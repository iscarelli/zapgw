// Store: the gateway's configuration in SQLite.
//
// A LINE THAT DOESN'T CROSS: this is where CONFIGURATION lives (instances,
// keys, callbacks, limits) and NEVER a message. No conversation, history,
// queue, or redelivery buffer. Storing messages would turn the transport
// into a second place holding personal data, with its own retention and a
// second truth about what was sent.
//
// TWO NAMED EXCEPTIONS, and both for a reason that is not "message":
//
//   - IDEMPOTENCY (further below): keeps only (consumidor, key) ->
//     wa_message_id, with a short TTL — a DELIVERY record, not a message.
//   - TRANSIT (transit.go, T-091): records that a message PASSED through
//     here — instance, direction, HMAC of the phone number and the wamid
//     (never the value in the clear), type, correlation, stamp and
//     outcome —, with its own TTL (DefaultTransitRetentionDays). It is
//     TRANSIT METADATA, not a message: there is no content whatsoever in
//     the table, and the phone number never exists in the clear — only the
//     HMAC, which cannot be reversed back to it.
package config

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go driver: no cgo, the binary is static
)

var ErrInstanceNotFound = errors.New("config: instancia nao encontrada")

// ErrInvalidSlug: the proposed slug doesn't fit in a webhook URL.
var ErrInvalidSlug = errors.New("config: slug invalido")

// ErrInsecureCallback: the callback_url would deliver the raw body outside an encrypted channel.
var ErrInsecureCallback = errors.New("config: callback_url insegura")

// ErrInvalidCABundle: the registered CA bundle contains no certificate at all.
var ErrInvalidCABundle = errors.New("config: bundle de CA invalido")

// ErrIncompleteIdentification: the registration would leave the instance
// without a waba_id or without a phone_number_id — the two keys by which a
// webhook is attributed to it.
var ErrIncompleteIdentification = errors.New("config: instancia sem identificacao completa")

// --- T-097: Instagram, the first slice ---------------------------------------
//
// TypeWhatsApp and TypeInstagram are the ONLY TWO valid values of
// Instance.Type. TypeWhatsApp is the DEFAULT — literally, and not just as
// documentation: every row written before this task has the `tipo` column
// filled in with it by the migration (see migracoes, below), and every bit
// of code that asks "is it Instagram?" asks `Type == TypeInstagram`, never
// `Type != TypeWhatsApp` — the difference matters because a future third
// type has to fall into the WhatsApp branch only if SOMEONE decides that,
// never by omission.
const (
	TypeWhatsApp  = "whatsapp"
	TypeInstagram = "instagram"
)

// ErrUnknownInstanceType: `--tipo` (or the Type field) is neither
// TypeWhatsApp nor TypeInstagram.
var ErrUnknownInstanceType = errors.New("config: tipo de instancia desconhecido")

// ErrFieldDoesNotApplyToType: a field that only exists on the OTHER type came
// filled in — e.g. `ig_id` on a `whatsapp` instance, or `waba_id` on an
// `instagram` one.
//
// WHY REFUSE INSTEAD OF IGNORING: an `instagram` instance with `waba_id`
// filled in by mistake (copied from another row, an old script) would look
// like it has double identification, and no code in this gateway reads
// `waba_id` on an Instagram instance — the value would sit dead in the
// database, looking like it means something. See the "half identification"
// entry in WhatsApp provisioning (further below): the same discipline, now
// between TWO types instead of two fields of the same type.
var ErrFieldDoesNotApplyToType = errors.New("config: campo nao se aplica a este tipo de instancia")

// ValidateInstanceType normalizes `tipo` (empty -> TypeWhatsApp, the same
// reading every row prior to this task already has in the database) and
// checks that the identification fields match the chosen type.
//
// THE RULE IS MUTUAL EXCLUSION, not "what's missing": an `instagram`
// instance with `ig_id` filled in AND `waba_id` filled in isn't "more
// complete" — it's a state no code in this gateway knows how to interpret
// (the inbound addressing guard only looks at ONE of the two, depending on
// the type), and letting it be written would hide a copy-paste mistake
// instead of refusing it right away.
//
// INSTAGRAM REQUIRES ig_id ALREADY AT CREATION, unlike WhatsApp (which can
// be born with just the slug — docs/MODELO-DE-USO.md, T-079): this slice
// didn't create an equivalent RegisterMeta for Instagram (it's not in
// T-097's Files, and the reason is written in its changelog), so the ONLY
// entry point for identification is creation. An Instagram instance without
// ig_id would be born impossible to route — the inbound addressing guard
// would never have anything to compare `entry[].id` against.
func ValidateInstanceType(typ, wabaID, phoneNumberID, displayNumber, igID string) (string, error) {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		typ = TypeWhatsApp
	}
	switch typ {
	case TypeWhatsApp:
		if strings.TrimSpace(igID) != "" {
			return typ, fmt.Errorf("%w: ig_id numa instancia %s", ErrFieldDoesNotApplyToType, TypeWhatsApp)
		}
	case TypeInstagram:
		for _, c := range []struct{ name, value string }{
			{"waba_id", wabaID}, {"phone_number_id", phoneNumberID}, {"numero_exibido", displayNumber},
		} {
			if strings.TrimSpace(c.value) != "" {
				return typ, fmt.Errorf("%w: %s numa instancia %s — quem endereça uma conta Instagram e o ig_id, nunca telefone",
					ErrFieldDoesNotApplyToType, c.name, TypeInstagram)
			}
		}
		if strings.TrimSpace(igID) == "" {
			return typ, fmt.Errorf("%w: ig_id vazio — esta fatia nao tem cadastro por API para Instagram, "+
				"entao a identificacao so pode chegar na criacao", ErrIncompleteIdentification)
		}
	default:
		return typ, fmt.Errorf("%w: %q (conheco %q e %q)", ErrUnknownInstanceType, typ, TypeWhatsApp, TypeInstagram)
	}
	return typ, nil
}

// ValidateIdentification requires waba_id and phone_number_id to be filled in.
//
// WHY IT EXISTS (T-074), and the finding that called for it: mutating guard
// 5b in internal/inbound/handler.go (removing the `waba == ""`), the suite
// passed GREEN. The real defense there wasn't the type nor the comparison —
// it was the fact that EVERY instance had waba_id filled in. And that was
// true of TODAY'S PATH (at the time, `zapgw provisionar instancia` required
// both flags), not a guarantee: boundary validation covered slug,
// callback_url and bundle_ca, and left out precisely the two identifiers.
// Validating three fields and not the other two is this project's
// mother-of-all-traps ("the rule holds in one place and not in the next")
// inside a single function.
//
// WHERE IT'S CALLED CHANGED IN T-079, AND THE REQUIREMENT DIDN'T DISAPPEAR —
// IT MOVED. Until T-079, whoever filled in waba_id and phone_number_id was
// the OWNER, at creation, which is why the check lived in CreateInstance. In
// the decided model (docs/MODELO-DE-USO.md) those values belong to the
// CONSUMER and the owner doesn't have them: the instance is born with just
// the slug, and the identifiers arrive via REGISTRATION (RegisterMeta,
// further below). Requiring them at creation became impossible to fulfill;
// requiring them at registration is the SAME rule in the one place it fits.
//
// AND THE INCOMPLETE INSTANCE ISN'T BORN DEAD BECAUSE OF THIS, which was
// T-074's fear: it's born PAUSED (CreateInstance forces ativo = 0) and only
// `zapgw fumaca` activates it, after a message has actually gone out — which
// is impossible without phone_number_id and token_envio registered. In
// other words, an unregistered instance doesn't refuse traffic with an
// ALARM: it responds 503 PAUSED, which is the state it actually has, and
// never gets to receive any webhook.
//
// GUARD 5b STILL HAS THE `waba == ""`, and the two aren't redundant:
// boundary validation doesn't reach a row that's ALREADY in the database
// (neither a hand-typed UPDATE, nor a database written by a binary older
// than this version). One keeps registration from writing incomplete
// identification; the other keeps the handler from TRUSTING one that's
// already like that. With T-079 that second one now applies to a REAL case
// instead of a hypothetical one — every freshly created instance has an
// empty waba_id until the consumer registers.
//
// BLANK COUNTS AS EMPTY, by the same criterion as ValidateSlug: "   " is what
// is left over from an environment variable nobody exported, and a
// whitespace waba_id would never match anything — it would be the same dead
// instance, with a value in place of empty to disguise it.
//
// The two are NOT secret (see the comment on Instance), so they can appear
// in the error message — and it says WHICH of the two is missing, otherwise
// whoever typed it is left choosing between two fields.
func ValidateIdentification(wabaID, phoneNumberID string) error {
	for _, c := range []struct{ field, value string }{
		{"waba_id", wabaID},
		{"phone_number_id", phoneNumberID},
	} {
		if strings.TrimSpace(c.value) == "" {
			return fmt.Errorf("%w: %s esta vazio — sem ele o webhook desta instancia nao tem como ser atribuido a ela, e o gateway recusa o lote inteiro com ALARME",
				ErrIncompleteIdentification, c.field)
		}
	}
	return nil
}

// ValidateSlug tells whether the slug can become `/v1/inbound/{slug}`.
//
// The shape is the narrowest one that works: lowercase a-z, digits and
// hyphen, no hyphen at either end, 3 to 40 characters. Why so narrow:
//
//   - a SLASH creates a second path segment — `/v1/inbound/loja/racer`
//     doesn't match the route, or worse, matches another one;
//   - `?` and `#` cut off the path, so the slug that reaches the server
//     isn't what was typed; a space doesn't even survive being pasted into
//     Meta's panel;
//   - UPPERCASE: the path is compared byte for byte, so `/Lojinha` and
//     `/lojinha` are different routes — and whoever pasted it into Meta has
//     no way to suspect it;
//   - non-ASCII turns into percent-encoding in the middle of the path.
//
// And it's checked at CREATION because the slug is IMMUTABLE (there is no
// editing path) and because the damage doesn't show up here: it shows up
// when the URL is pasted into Meta and verification fails, with ITS error
// message pointing at the wrong place.
//
// The message says WHICH rule broke: a bare "invalid slug" makes whoever
// typed it guess among five rules. The slug is not a secret, so it can appear.
func ValidateSlug(slug string) error {
	// The charset comes first on purpose: after it the slug is pure ASCII,
	// so len() in bytes is the same as counting characters.
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("%w: %q tem o caractere %q — so valem minusculas de a a z, digitos e hifen",
			ErrInvalidSlug, slug, r)
	}
	const minLen, maxLen = 3, 40
	if len(slug) < minLen || len(slug) > maxLen {
		return fmt.Errorf("%w: %q tem %d caractere(s) — o tamanho tem de ficar entre %d e %d",
			ErrInvalidSlug, slug, len(slug), minLen, maxLen)
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return fmt.Errorf("%w: %q comeca ou termina em hifen", ErrInvalidSlug, slug)
	}
	return nil
}

// ValidateCallbackURL requires an encrypted channel for delivery to the consumer.
//
// WHY https IS MANDATORY: the delivery carries the message's RAW BODY —
// personal data from whoever wrote to the business. It's already signed by
// HMAC (internal/inbound/deliver.go), but a signature proves INTEGRITY, not
// confidentiality: over `http://` the body crosses the network readable to
// anyone on the path.
//
// EMPTY IS STILL VALID, and that's not a relaxation: an outbound-only
// instance (its webhook lives in another system) delivers nothing, and
// absence of delivery is not delivery in the clear. That's the case of the
// instance running in production today.
//
// THE EXCEPTION is hand-written and narrow — `http://127.0.0.1` and
// `http://localhost`, for local testing — and it is checked against the
// HOST that net/url resolves, NEVER by text prefix:
// `http://127.0.0.1.consumidor.example` and
// `http://127.0.0.1@consumidor.example` start with the allowed text and
// deliver outside the machine. A prefix guard has already cost this project
// a Critical (docs/ARMADILHAS.md, "Validação").
//
// THE URL DOES NOT go into the error message: it is encrypted at rest
// precisely so a stolen backup doesn't reveal the consumers' topology, and
// the error goes to the log and the terminal.
func ValidateCallbackURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err == nil {
		if u.Scheme == "https" && u.Host != "" {
			return nil
		}
		if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
			return nil
		}
	}
	// net/url's error carries the whole URL; that's why it is NOT wrapped.
	return fmt.Errorf("%w: a entrega leva o corpo cru da mensagem, entao a callback_url tem de ser https:// — a unica excecao e http://127.0.0.1 ou http://localhost, para teste local (a URL nao e repetida aqui porque e cifrada em repouso)",
		ErrInsecureCallback)
}

// ValidateCABundle checks that the instance's CA bundle actually contains a
// certificate.
//
// WHY THIS FIELD EXISTS, and why it isn't a boolean: certificate
// verification on delivery is STRICT and cannot be switched off (CLAUDE.md,
// "TLS — não existe modo desligado"). The legitimate case that makes someone want
// the escape hatch — a consumer with its own CA, without a public CA
// certificate — exits through here: THEIR CA becomes the trust anchor for
// that instance, and only it. This is still verification; a global
// `insecure_skip_verify` is not the same thing, because it verifies nothing
// and doesn't warn that it stopped verifying.
//
// EMPTY IS VALID and means "use the system's CA store" — the normal case,
// and the one for every instance that exists today.
//
// BUT BLANK IS NOT EMPTY: a bundle with just whitespace, or with PEM that
// carries no certificate at all, is REFUSED instead of treated as absence.
// Whoever typed it meant to register an anchor; falling silently back to
// the system store would deliver to a consumer that CA doesn't cover, and
// nobody would understand why.
//
// The bundle's content does NOT go into the error message: it is encrypted
// at rest (along with the callback_url) because it reveals the consumer's
// topology, and the error goes to the log.
func ValidateCABundle(bundle string) error {
	if bundle == "" {
		return nil
	}
	// AppendCertsFromPEM returns true only when at least one CERTIFICATE
	// block was decoded AND parsed as X.509 — it's the same function the
	// deliverer uses to build its pool, not a second rule that could
	// diverge from it.
	if !x509.NewCertPool().AppendCertsFromPEM([]byte(bundle)) {
		return fmt.Errorf("%w: nenhum certificado X.509 foi lido do PEM (o conteudo nao e repetido aqui porque e cifrado em repouso)."+
			" Para usar a store de CAs do sistema, deixe o bundle VAZIO — branco nao vale como vazio", ErrInvalidCABundle)
	}
	return nil
}

// Instance is a configured WhatsApp channel.
//
// The six fields AppSecret, VerifyToken, SendToken, CallbackURL,
// DeliverySecret and CABundle are ENCRYPTED at rest and never come back out
// through the administration API — write-only. An endpoint that read them
// back would turn a leaked provisioning token into theft of a Meta
// credential; in the case of CallbackURL and CABundle, a stolen backup file
// also must not reveal the consumers' topology (a CA's certificate is not a
// secret — it travels in the clear on every handshake —, but SAYING WHO the
// consumer of that instance is is the same information the callback_url is
// already encrypted to hide).
type Instance struct {
	Slug          string // becomes /v1/inbound/{slug}. IMMUTABLE after creation
	WabaID        string // identifier, NOT a secret. Only in TypeWhatsApp
	PhoneNumberID string // identifier, NOT a secret. Only in TypeWhatsApp
	DisplayNumber string // Only in TypeWhatsApp

	// Type is TypeWhatsApp (default, "" also reads as it) or TypeInstagram
	// (T-097). See ValidateInstanceType for the mutual exclusion with the
	// identification fields.
	Type string
	// IgID is the Instagram-scoped Business Account ID — the `entry[].id`
	// Meta sends in the webhook and the URL segment of
	// `POST /<IG_ID>/messages`. Identifier, NOT a secret (same class as
	// WabaID/PhoneNumberID). Only in TypeInstagram.
	IgID string
	// TokenSetAt is when SendToken, ABOVE, was LAST written — by any
	// path (creation, consumer registration, owner rotation, or Instagram's
	// automatic renewal, T-098). NOT a secret (it's just a stamp), but only
	// matters today for tipo=instagram: it's what
	// internal/outbound/instagram_renewer.go uses to know the token's AGE
	// and decide whether it's already time to renew. RFC3339 UTC, or ""
	// when the row was born before this column existed and was never
	// rewritten (see the migration
	// "instancia.token_definido_em-e-token_renovado_em").
	TokenSetAt string

	AppSecret      string // inbound: verifies Meta's HMAC
	VerifyToken    string // inbound: answers the verification GET
	SendToken      string // outbound: talks to the Graph API
	CallbackURL    string // encrypted: doesn't reveal the consumers' topology
	DeliverySecret string // signs the POST to the consumer

	CABundle string // PEM: this delivery's OWN trust anchor (optional)

	TimeoutMs int
	Active    bool
}

type Store struct {
	db    *sql.DB
	vault *Vault
}

// --- Schema and migrations -----------------------------------------------------
//
// MIGRATIONS ARE HISTORY: the SQL of a migration that has already shipped
// does NOT get edited. Editing it rewrites the past of databases that have
// already run it — they don't run it again and silently start diverging
// from new databases. A new schema goes in as a NEW migration at the end of
// the list.
//
// That's why there is NO "current schema" anywhere here: the schema is the
// sum of the migrations. Keeping both things would mean keeping two
// truths, and it was exactly one of them lying (a column added inside a
// CREATE TABLE IF NOT EXISTS, which never reaches a database that already
// exists) that created this task.

// ErrSchemaFromTheFuture: the file was written by a newer binary.
//
// Coming up like this would be worse than failing: the old binary doesn't
// know the columns and tables the new one created, would write over them
// with the old rules and silently corrupt what the new binary writes.
var ErrSchemaFromTheFuture = errors.New("config: esquema do banco e mais novo que o binario")

// migracao is a schema step. The INDEX in the list is the version: applying
// the migration at index i takes the database from user_version i to i+1.
// So user_version == len(migracoes) means "up to date".
type migration struct {
	name  string
	apply func(context.Context, *sql.Conn) error
}

var migrations = []migration{
	{"esquema base", func(ctx context.Context, c *sql.Conn) error {
		_, err := c.ExecContext(ctx, baseSchema)
		return err
	}},
	{"idempotencia.hash_pedido", func(ctx context.Context, c *sql.Conn) error {
		return addColumn(ctx, c, "idempotencia", "hash_pedido", "TEXT NOT NULL DEFAULT ''")
	}},
	// The '' DEFAULT for rows that already exist is NOT a valid ciphertext —
	// that's why reads treat the empty column as "never registered" instead
	// of decrypting blind (see decryptOptional).
	{"instancia.bundle_ca", func(ctx context.Context, c *sql.Conn) error {
		return addColumn(ctx, c, "instancia", "bundle_ca", "TEXT NOT NULL DEFAULT ''")
	}},
	// T-035: instance counters. Read/write in internal/config/counter.go.
	{"contador", func(ctx context.Context, c *sql.Conn) error {
		_, err := c.ExecContext(ctx, counterSchema)
		return err
	}},
	// T-060: the STAMP of the last event for each (slug, dia, key).
	//
	// WHY A COLUMN ON THE COUNTER, AND NOT A NEW TABLE: a stalled counter is
	// AMBIGUOUS between "failed" and "nobody wrote" — the two are the same
	// number (real cost measured on 2026-07-28, see docs/TASKS.md T-060).
	// The stamp resolves the ambiguity because it AGES. Living on the SAME
	// row as the counter, it's born for every key of the closed vocabulary
	// at once: there is no second list of "which keys have a stamp" for
	// someone to forget to update — which is this project's
	// mother-of-all-traps.
	//
	// The '' DEFAULT for rows that already exist means "counted before the
	// column existed": the read treats it as ABSENCE of a stamp, never as a
	// zero date (see LastEventPerKey in counter.go).
	{"contador.ultimo", func(ctx context.Context, c *sql.Conn) error {
		return addColumn(ctx, c, "contador", "ultimo", "TEXT NOT NULL DEFAULT ''")
	}},
	// T-064: the callback certificate's validity, as the DELIVERY saw it.
	// Read/write in internal/config/certificate.go.
	{"certificado do callback", func(ctx context.Context, c *sql.Conn) error {
		_, err := c.ExecContext(ctx, callbackCertificateSchema)
		return err
	}},
	// T-070: since when THIS instance has been stamping counters.
	//
	// WHY IT EXISTS (requested by `consumer-a`, 2026-07-28): `ultimo_em:
	// null` hides TWO states — "never happened" (normal) and "happened
	// before the stamp was written" (blind spot, because the stamp was born
	// in v0.23.0). For a dashboard these are different things, and without
	// a field saying the age of the INSTRUMENT every consumer hardcodes the
	// v0.23.0 date by hand — a constant that rots on the first new instance.
	//
	// WHY DATA IN THE DATABASE AND NOT A COMPILED CONSTANT: the answer is
	// PER INSTANCE. An instance created today starts stamping today, and a
	// constant would tell it the v0.23.0 date too — lying precisely in the
	// dangerous direction (asserting coverage that never happened).
	//
	// WHAT A PRE-EXISTING INSTANCE RECEIVES, AND WHY (the one part with
	// judgment): the instant THIS migration runs. Not the v0.23.0 date,
	// which would be a compiled constant under another name; and not the
	// instance's creation date, which the database doesn't even keep. The
	// migration's instant is the latest we can PROVE, and it may be later
	// than the truth — the instance might have been stamping for days
	// already. Erring LATER is the safe side: whoever reads it now treats
	// as "don't know" a range where there might have been a stamp, and
	// never the opposite. The opposite error (claiming it stamps since
	// earlier) would make the consumer read `ultimo_em: null` as "never
	// happened" over a blind window — precisely the defect this field
	// exists to close.
	{"instancia.carimbos_desde", func(ctx context.Context, c *sql.Conn) error {
		if err := addColumn(ctx, c, "instancia", "carimbos_desde", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		// The ALTER TABLE's '' DEFAULT doesn't work as an answer: '' isn't
		// any date and would show up on the consumer's screen as an empty
		// field. Rows that already existed receive this migration's
		// instant — and only them, via the `WHERE`, so running this again
		// (a database that lost its user_version) doesn't overwrite what
		// already has a value.
		_, err := c.ExecContext(ctx,
			`UPDATE instancia SET carimbos_desde = ? WHERE carimbos_desde = ''`,
			stampOf(time.Now()))
		return err
	}},
	// T-080: the number's quality and messaging limit, as Meta reports them.
	// Read/write in internal/config/number.go.
	{"numero na meta", func(ctx context.Context, c *sql.Conn) error {
		_, err := c.ExecContext(ctx, numberAtMetaSchema)
		return err
	}},
	// T-079: the instant of the CONSUMER's FIRST insert on this instance.
	//
	// It's what opens the 24h window in which the consumer can register and
	// correct their Meta setup (see RegisterMeta and RegistrationWindow).
	// Owner's decision, with both halves written down because each one
	// rules out a design that looks reasonable and isn't:
	//
	//   - It does NOT count from the instance's CREATION: "I create the
	//     instance today, in 5 days the consumer inserts something, the
	//     count starts there". A slow consumer would lose the window before
	//     starting, and their first contact with the gateway would be an
	//     error they didn't cause;
	//   - It does NOT reset on every change: whoever touched it every day
	//     would keep the window open forever, and the rule would become
	//     decorative.
	//
	// The ALTER TABLE's '' DEFAULT means "the consumer never inserted
	// anything", and here that is the CORRECT answer for instances that
	// already existed: none of them was registered by the consumer (the
	// owner typed everything at creation), and their clock starts the first
	// time they write. There is no value to backfill retroactively —
	// unlike carimbos_desde, here the absence is a real state and not a hole.
	//
	// And '' is also what REOPENING the window writes (ReopenRegistrationWindow):
	// the owner doesn't grant "24 more hours from now", they ERASE the
	// first insert, and the count restarts when the consumer writes again.
	{"instancia.cadastro_em", func(ctx context.Context, c *sql.Conn) error {
		return addColumn(ctx, c, "instancia", "cadastro_em", "TEXT NOT NULL DEFAULT ''")
	}},
	// T-091: TRANSIT log — "did this message pass through here?" without
	// storing content or the phone number in the clear. Read/write in
	// internal/config/transit.go.
	{"transito", func(ctx context.Context, c *sql.Conn) error {
		_, err := c.ExecContext(ctx, transitSchema)
		return err
	}},
	// T-094: the transit log's phone number and wamid now go in the CLEAR —
	// owner's decision, 2026-07-30 ("you can put the number in, it's not a
	// secret"), which reverts, ONLY on those two fields, T-091's HMAC design
	// (`correlacao` stays HMAC — see internal/config/transit.go).
	//
	// A ROW WRITTEN BEFORE THIS MIGRATION KEEPS contraparte/wamid EMPTY
	// FOREVER: HMAC is ONE-WAY, there is no way to recover the phone number
	// from the hash T-091 wrote. NOT A BUG — cmd/zapgw/log.go prints "—" on
	// those rows, never an empty value that would look like "no sender".
	{"transito.contraparte-e-wamid-em-claro", func(ctx context.Context, c *sql.Conn) error {
		if err := addColumn(ctx, c, "transito", "contraparte", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if err := addColumn(ctx, c, "transito", "wamid", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		// The old index (idx_transito_busca) uses hmac_contraparte, which
		// this step drops right below — it has to fall BEFORE the DROP
		// COLUMN, or SQLite refuses to drop an indexed column.
		if _, err := c.ExecContext(ctx, `DROP INDEX IF EXISTS idx_transito_busca`); err != nil {
			return fmt.Errorf("derrubar idx_transito_busca: %w", err)
		}
		// Two representations of the same fact diverge (docs/ARMADILHAS.md,
		// "a armadilha-mãe deste projeto") — that's why the HMAC
		// columns are DROPPED, not left dead alongside the clear ones.
		for _, column := range []string{"hmac_contraparte", "hmac_wamid"} {
			if _, err := c.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE transito DROP COLUMN %s`, column)); err != nil {
				return fmt.Errorf("derrubar transito.%s: %w", column, err)
			}
		}
		// The new index matches on the EXPRESSION substr(contraparte, -8) —
		// the LAST EIGHT DIGITS, not the whole column. It's the same key
		// Store.SearchTransit uses (internal/config/transit.go), and it's
		// what makes the four spellings of the same subscriber find the
		// SAME row (T-094's Verify (a)).
		_, err := c.ExecContext(ctx,
			`CREATE INDEX IF NOT EXISTS idx_transito_busca ON transito(slug, substr(contraparte, -8), carimbo)`)
		return err
	}},
	// T-097: Instagram, the first slice. `tipo` is born with the EXPLICIT
	// 'whatsapp' DEFAULT on the column — not '', and the difference
	// matters: a mistaken read of `Type == ""` (instead of
	// `!= TypeInstagram`) stays correct with the literal DEFAULT, and not
	// merely by coincidence of the two empty strings being equal.
	{"instancia.tipo-e-ig_id", func(ctx context.Context, c *sql.Conn) error {
		if err := addColumn(ctx, c, "instancia", "tipo", "TEXT NOT NULL DEFAULT 'whatsapp'"); err != nil {
			return err
		}
		return addColumn(ctx, c, "instancia", "ig_id", "TEXT NOT NULL DEFAULT ''")
	}},
	// T-098: renewal of Instagram's long-lived token (60 days, no renewal
	// possible once expired — see internal/outbound/instagram_renewer.go).
	//
	// TWO COLUMNS, NOT ONE, and the difference matters: token_definido_em is
	// the validity IN FORCE (every path that writes token_envio updates it —
	// creation, consumer registration, owner rotation, and the renewal
	// itself); token_renovado_em is written ONLY by the automatic loop, and
	// it answers a different and harder-to-fake question: "has the renewal
	// mechanism worked at least once, and when?". If the two were a single
	// column, a manual rotation by the owner (which proves nothing about the
	// automatic loop) would look like a successful renewal.
	{"instancia.token_definido_em-e-token_renovado_em", func(ctx context.Context, c *sql.Conn) error {
		if err := addColumn(ctx, c, "instancia", "token_definido_em", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if err := addColumn(ctx, c, "instancia", "token_renovado_em", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		// Backfill ONLY of token_definido_em, for the SAME reason and the
		// SAME choice as the "instancia.carimbos_desde" migration above:
		// this migration's instant is the latest thing we can PROVE. In
		// practice this only matters for a pre-existing tipo=instagram
		// instance — a rare case here, because `tipo`/`ig_id` were born
		// TOGETHER in the previous migration (T-097) and the ONLY entry
		// point for tipo=instagram is CLI CREATION
		// (`zapgw provisionar instancia --tipo instagram`), always manual.
		// Even so the backfill runs on ALL rows (not just tipo=instagram),
		// for the usual reason: a `WHERE tipo = ...` condition would
		// duplicate the same question ValidateInstanceType already
		// answers, and would diverge from it on the first change.
		//
		// token_renovado_em does NOT get a backfill — '' is the CORRECT
		// value for "the automatic loop has never successfully renewed this
		// token", and that is true for EVERY row that existed before this
		// task, without exception.
		_, err := c.ExecContext(ctx,
			`UPDATE instancia SET token_definido_em = ? WHERE token_definido_em = ''`,
			stampOf(time.Now()))
		return err
	}},
}

// numberAtMetaSchema is T-080's migration.
//
// ONE ROW PER INSTANCE (slug is the PRIMARY KEY), and not a history — the
// SAME decision as certificado_do_callback and for the same reason: the
// question is "what does the gateway know about the number NOW?", and for
// that only the latest observation matters. A tier history would be data
// that grows without anyone asking for it, and without purging.
//
// EVERY COLUMN IS BORN WITH THE EMPTY STRING because the row is truly born
// PARTIAL: the first limit webhook arrives before the first measurement (or
// the other way around), and there is no instant at which both sides are
// filled in by construction. The empty string is "never observed" — it's
// the only value UpdateNumberAtMeta's UPSERT treats as "any stamp is
// newer than this".
//
// THERE IS NO "estado" COLUMN: it's a FUNCTION of what was observed (empty
// value = never observed), and one more column would only create the
// chance for it to disagree with the values. The translation to the NAMED
// state the consumer reads happens in outbound.numberAtMeta.
const numberAtMetaSchema = `
CREATE TABLE IF NOT EXISTS numero_na_meta (
  slug            TEXT PRIMARY KEY,
  qualidade       TEXT NOT NULL DEFAULT '',
  qualidade_em    TEXT NOT NULL DEFAULT '',
  qualidade_fonte TEXT NOT NULL DEFAULT '',
  limite          TEXT NOT NULL DEFAULT '',
  limite_em       TEXT NOT NULL DEFAULT '',
  limite_fonte    TEXT NOT NULL DEFAULT '',
  conferido_em    TEXT NOT NULL DEFAULT ''
);
`

// callbackCertificateSchema is T-064's migration.
//
// ONE ROW PER INSTANCE (slug is the PRIMARY KEY), and not a history: the
// question this table answers is "what does the gateway know about the
// certificate NOW?", and for that only the latest observation matters. A
// certificate history would be data that grows without anyone asking for
// it — and without purging, unlike the counter, which has a TTL precisely
// because it accumulates a row per day.
//
// THERE IS NO "estado" COLUMN. "Never observed" is the ABSENCE of the row:
// a stored state would need to be written by someone at provisioning time,
// and an instance created before this table existed would be left without
// it — the missing row already says the same thing, without depending on
// anyone remembering.
const callbackCertificateSchema = `
CREATE TABLE IF NOT EXISTS certificado_do_callback (
  slug         TEXT PRIMARY KEY,
  expira_em    TEXT NOT NULL,
  observado_em TEXT NOT NULL
);
`

// counterSchema is T-035's migration, separate from baseSchema (FROZEN)
// for the same reason as the others: a new schema goes in as a NEW step at
// the end of the list, never inside a migration that has already shipped.
//
// PRIMARY KEY (slug, dia, key), no auto-increment id: it's what makes
// `INSERT ... ON CONFLICT DO UPDATE` (IncrementCounter) a truly atomic
// UPSERT — without the composite key, two rows for the same day, instance
// and key could coexist, and "how many messages arrived today?" would stop
// having a single answer.
const counterSchema = `
CREATE TABLE IF NOT EXISTS contador (
  slug  TEXT NOT NULL,
  dia   TEXT NOT NULL,
  chave TEXT NOT NULL,
  n     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (slug, dia, chave)
);
CREATE INDEX IF NOT EXISTS idx_contador_dia ON contador(dia);
`

// baseSchema is migration 1, FROZEN: it's the schema as it was born,
// without hash_pedido. Don't add a column here — see the block above.
const baseSchema = `
CREATE TABLE IF NOT EXISTS instancia (
  slug             TEXT PRIMARY KEY,
  waba_id          TEXT NOT NULL,
  phone_number_id  TEXT NOT NULL,
  numero_exibido   TEXT NOT NULL DEFAULT '',
  app_secret       TEXT NOT NULL,
  verify_token     TEXT NOT NULL,
  token_envio      TEXT NOT NULL,
  callback_url     TEXT NOT NULL,
  segredo_entrega  TEXT NOT NULL,
  timeout_ms       INTEGER NOT NULL DEFAULT 5000,
  ativo            INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_instancia_pnid ON instancia(phone_number_id);
CREATE INDEX IF NOT EXISTS idx_instancia_waba ON instancia(waba_id);
CREATE TABLE IF NOT EXISTS consumidor (
  nome       TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS consumidor_instancia (
  consumidor TEXT NOT NULL REFERENCES consumidor(nome),
  slug       TEXT NOT NULL REFERENCES instancia(slug),
  PRIMARY KEY (consumidor, slug)
);
CREATE INDEX IF NOT EXISTS idx_consumidor_token ON consumidor(token_hash);
CREATE TABLE IF NOT EXISTS idempotencia (
  consumidor      TEXT NOT NULL,
  chave           TEXT NOT NULL,
  wa_message_id   TEXT NOT NULL DEFAULT '',
  criado_em       INTEGER NOT NULL,
  PRIMARY KEY (consumidor, chave)
);
CREATE INDEX IF NOT EXISTS idx_idempotencia_idade ON idempotencia(criado_em);
`

func OpenStore(path string, vault *Vault) (*Store, error) {
	// PRAGMAS in the DSN, never via db.Exec: database/sql keeps a POOL, and
	// a PRAGMA per Exec only holds for the connection that served it.
	//
	//   foreign_keys  — without it the schema's REFERENCES clauses are
	//                   DECORATIVE: SQLite accepts them and doesn't enforce them.
	//
	//   busy_timeout  — without it SQLite returns SQLITE_BUSY RIGHT AWAY when
	//                   the database is locked, instead of waiting. That
	//                   broke ReserveIdempotency's contract: under a burst
	//                   of simultaneous retries (the scenario idempotency
	//                   exists to handle), 58 of 60 concurrent calls came
	//                   back with a database error instead of the
	//                   "in progress" outcome.
	//
	//   journal_mode  — WAL lets readers and one writer coexist instead of
	//                   excluding each other. Reduces the contention
	//                   busy_timeout would otherwise only mask with waiting.
	//
	// THE POOL HAS NO EXPLICIT CEILING — BY CHOICE, NOT BY OMISSION. There is
	// no SetMaxOpenConns nor SetMaxIdleConns here, and this was DECIDED on
	// 2026-08-18: `lsof` measured 2 connections in production (database/sql's
	// MaxIdleConns default is already 2), and limiting it would trade a
	// LOUD error for an UNBOUNDED block waiting for a free connection — the
	// opposite direction from this house, where what costs dearly is silence.
	//
	// 🔴 DON'T ADD A LIMIT THINKING IT FIXES AN OVERSIGHT. The whole
	// reasoning, with the two triggers that REOPEN the decision (the first
	// observed occurrence of `database is locked`, or the port to Postgres —
	// there max_connections is a server-wide setting and limiting becomes
	// mandatory), is in docs/ARMADILHAS.md, section SQLite, paragraph
	// "DECISÃO IRMÃ, no mesmo dia: o pool do `database/sql` fica SEM
	// limite explícito". It is NOT repeated here on purpose: two copies
	// of the same decision is one that rots.
	dsn := path + "?_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("config: abrir banco: %w", err)
	}
	if err := migrate(context.Background(), db, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, vault: vault}, nil
}

// migrar takes the database from the version it's at to the one this binary
// knows, applying the missing migrations IN ORDER and in a single
// transaction: either the database ends up entirely on the new version, or
// it stays entirely on the old one. A half-migrated schema is the worst
// outcome — it passes on startup and fails on the first write.
//
// Everything runs on a SINGLE connection (sql.Conn) because database/sql
// keeps a POOL: a BEGIN per db.Exec could land on one connection and the
// COMMIT, possibly, on another. It's the same trap that already cost this
// project a Critical with the PRAGMAs.
//
// The list of steps comes in as a parameter (in production it's always
// `migracoes`) so the test can prove, with a migration that fails on
// purpose, that nothing that came before it survives.
func migrate(ctx context.Context, db *sql.DB, steps []migration) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("config: conexao para migrar: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// IMMEDIATE, not the default (deferred) BEGIN: the transaction is born
	// already requesting the write lock. With deferred, two processes
	// starting up together would both read the same old user_version and
	// only discover the conflict when writing.
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("config: abrir transacao de migracao: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	var version int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("config: ler versao do esquema: %w", err)
	}
	if version > len(steps) {
		return fmt.Errorf("%w: banco na versao %d, este binario conhece ate a %d",
			ErrSchemaFromTheFuture, version, len(steps))
	}

	for i := version; i < len(steps); i++ {
		if err := steps[i].apply(ctx, conn); err != nil {
			return fmt.Errorf("config: migracao %d (%s): %w", i+1, steps[i].name, err)
		}
	}

	// PRAGMA doesn't accept a bound parameter; the value is the length of a
	// list from the code itself, never outside input.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, len(steps))); err != nil {
		return fmt.Errorf("config: gravar versao do esquema: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("config: commit da migracao: %w", err)
	}
	committed = true
	return nil
}

// addColumn is the ALTER TABLE that doesn't blow up if the column
// is already there.
//
// This is NOT excessive caution: every database created before this
// mechanism existed is at user_version 0 with today's schema already
// complete (the column was born inside the CREATE TABLE). Without the
// check, the binary's first startup with migrations would die with
// "duplicate column name" on precisely the databases that exist.
func addColumn(ctx context.Context, c *sql.Conn, table, column, typ string) error {
	var howMany int
	if err := c.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&howMany); err != nil {
		return fmt.Errorf("consultar colunas de %s: %w", table, err)
	}
	if howMany > 0 {
		return nil
	}
	// Names come from constants in this file, never from outside — ALTER
	// TABLE doesn't accept a bound parameter for an identifier.
	if _, err := c.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, typ)); err != nil {
		return fmt.Errorf("acrescentar %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the connection for TESTING — in particular to check pragmas,
// which have no other way of being verified from outside. Don't use it in
// production code: every write goes through the Store's methods, which is
// where the guarantees live.
func (s *Store) DB() *sql.DB { return s.db }

// CreateInstance writes the instance with the six sensitive fields encrypted.
//
// The instance is ALWAYS born paused, even if the caller asks for active:
// only the smoke test activates it. Otherwise a misconfigured instance
// enters production "working" and the defect shows up with the customer on
// the other end.
//
// THE SLUG IS MANDATORY AND IT'S THE ONLY ONE THAT IS (T-079).
// Identification (waba_id and phone_number_id) and the Meta credentials are
// the CONSUMER's data: the owner creates the instance with just the slug —
// which is theirs because it's immutable and becomes a URL path — and the
// consumer registers their own Meta setup via API (RegisterMeta). T-074's
// requirement didn't disappear, it moved: ValidateIdentification is now
// called at registration, and why it no longer fits here is written there.
//
// The slug, the callback_url and the bundle_ca are still validated HERE,
// and not only in the subcommand that types them in: the subcommand is
// just the first creation path, and the next one (an administration
// endpoint, a seed) would be born without them — this project's
// mother-of-all-traps is "the rule holds in one place and not in the next".
func (s *Store) CreateInstance(i Instance) error {
	return s.CreateInstanceAt(i, time.Now())
}

// CreateInstanceAt is CreateInstance with the birth instant made explicit —
// which becomes the instance's `carimbos_desde` (T-070).
//
// WHY IT EXISTS, and it's not convenience: `carimbos_desde` comes out in
// RFC3339 WITHOUT a fractional second, so two instances created in the
// same second get the SAME text. A test that created both with time.Now()
// wouldn't distinguish "one value per instance" from "an equal constant for
// all" — precisely the defect this field exists to not have. The instant
// comes in as a parameter so that distinction is provable.
func (s *Store) CreateInstanceAt(i Instance, now time.Time) error {
	if err := ValidateSlug(i.Slug); err != nil {
		return err
	}
	if err := ValidateCallbackURL(i.CallbackURL); err != nil {
		return err
	}
	if err := ValidateCABundle(i.CABundle); err != nil {
		return err
	}
	// T-097: normalize and check the type BEFORE encrypting/writing
	// anything — same discipline as the validations above (refuse early,
	// without touching the database).
	typ, err := ValidateInstanceType(i.Type, i.WabaID, i.PhoneNumberID, i.DisplayNumber, i.IgID)
	if err != nil {
		return err
	}
	i.Type = typ
	// callback_url and bundle_ca go into the encrypted set together with
	// the secrets: a stolen backup file must not reveal either the
	// credentials or the consumers' topology.
	secrets := make([]string, 6)
	for n, plaintext := range []string{i.AppSecret, i.VerifyToken, i.SendToken, i.CallbackURL, i.DeliverySecret, i.CABundle} {
		ciphertext, err := s.vault.Encrypt(plaintext)
		if err != nil {
			return fmt.Errorf("config: cifrar: %w", err)
		}
		secrets[n] = ciphertext
	}

	// carimbos_desde is written HERE, and doesn't come from the caller via
	// Instance: the field says since when this instance stamps the
	// counter, and the answer is "since it exists" — making it fillable
	// from outside would create an instance able to lie about its own age,
	// and a new caller who forgot it would create one with no answer at all.
	// token_definido_em is born with the SAME instant as carimbos_desde,
	// for the same reason: the token that was just written was defined
	// NOW, and making it fillable from outside would create an instance
	// able to lie about its own age (T-098).
	_, err = s.db.Exec(`
		INSERT INTO instancia (slug, waba_id, phone_number_id, numero_exibido,
		    app_secret, verify_token, token_envio, callback_url, segredo_entrega,
		    bundle_ca, timeout_ms, ativo, carimbos_desde, tipo, ig_id, token_definido_em)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?)`,
		i.Slug, i.WabaID, i.PhoneNumberID, i.DisplayNumber,
		secrets[0], secrets[1], secrets[2], secrets[3], secrets[4], secrets[5],
		i.TimeoutMs, stampOf(now), i.Type, i.IgID, stampOf(now))
	if err != nil {
		return fmt.Errorf("config: inserir instancia: %w", err)
	}
	return nil
}

// --- T-079: the CONSUMER registers THEIR OWN Meta setup ---------------------------------
//
// THE DIRECTION IS CONSUMER -> GATEWAY, and it's a WRITE. Nothing here
// returns configuration: what this block does is receive the consumer's
// values, encrypt the ones that are secret, and write them. The whole model
// is in docs/MODELO-DE-USO.md, and it beats anything written here.

// RegistrationWindow is for how long the consumer can write their own
// configuration, counted from the FIRST time they wrote.
//
// WHAT IT SOLVES, and why limiting by TIME is better than limiting by
// PERMISSION (owner's decision): with this route, the consumer token stops
// being "send a message" and becomes "reconfigure the instance" — whoever
// steals it can replace the credentials and point the instance at their own
// Meta setup. During the window they test, err, and fix it themselves,
// which is exactly what lets a third party without a channel manage on
// their own; after it, a stolen token goes back to only being worth "send a
// message", which is the risk that already existed before this task.
const RegistrationWindow = 24 * time.Hour

// ErrRegistrationWindowClosed: more than RegistrationWindow has passed since
// the consumer's FIRST insert, and the configuration is locked.
//
// With no channel to ask, the error message IS the support: whoever wraps
// this error has to say WHY it closed (when it opened, when it closed) and
// WHAT TO DO (talk to the owner, who reopens it with `zapgw instancia
// reabrir-cadastro`).
var ErrRegistrationWindowClosed = errors.New("config: janela de cadastro fechada")

// ErrIncompleteRegistration: a field is missing without which the registration
// is useless. The error NAMES the field and never carries the VALUE — two
// of this type's fields are secret, and the error goes to the log and the
// response.
var ErrIncompleteRegistration = errors.New("config: cadastro incompleto")

// MetaRegistration is what the CONSUMER sends: their entire Meta account.
//
// REPLACES, DOESN'T PATCH — that's why the fields are string and not
// pointer, unlike Rotation. The two operations look like the same thing and
// aren't: Rotation is the OWNER changing one secret of an instance in
// production, where "don't touch the others" is the guarantee that
// matters. Here whoever writes has the whole configuration on their panel
// screen and sends it again; a partial registration would force them to
// know what's already stored — and the one thing the gateway NEVER returns
// is exactly that.
//
// VerifyToken and DeliverySecret are NOT here, and the absence is
// deliberate: both are randomly generated at creation and delivered to the
// consumer in the delivery package (T-052 — they type `verify_token` into
// their Meta panel, and put `segredo_entrega` in their `.env`). Letting
// them change `segredo_entrega` through here would swap the HMAC key of a
// delivery in progress without the other side knowing; changing
// `verify_token` would break webhook re-verification with no immediate
// symptom. Both swaps exist, and they are `zapgw instancia rotacionar`, on
// the owner's side.
type MetaRegistration struct {
	WabaID        string // identifier, NOT a secret
	PhoneNumberID string // identifier, NOT a secret
	DisplayNumber string

	AppSecret string // encrypted: verifies the HMAC Meta signs
	SendToken string // encrypted: talks to the Graph API

	// CallbackURL EMPTY is a legitimate state (outbound-only instance), as
	// at creation — see ValidateCallbackURL.
	CallbackURL string
	// CABundle EMPTY is the normal case: uses the system's CA store. It
	// exists for the consumer with their own CA, and does NOT switch off
	// any verification (CLAUDE.md, "TLS — não existe modo desligado").
	CABundle string
}

// ValidateMetaRegistration refuses a registration that would leave the
// instance useless.
//
// MESSAGE NAMES THE FIELD AND NEVER THE VALUE: `app_secret` and
// `token_envio` are secret, and this error goes to the gateway's log and
// the HTTP response. Same criterion as ValidateCallbackURL, which doesn't
// repeat the URL.
//
// BLANK COUNTS AS EMPTY, for the same reason as ValidateIdentification: "   "
// is what's left over from a field the consumer's panel sent blank, and a
// whitespace token_envio would only be discovered on Meta's first refusal.
func ValidateMetaRegistration(c MetaRegistration) error {
	if err := ValidateIdentification(c.WabaID, c.PhoneNumberID); err != nil {
		return err
	}
	// Field and explanation go TOGETHER: each line says what breaks without
	// it, because whoever reads this message has no channel to ask.
	for _, field := range []struct{ name, value, why string }{
		{"numero_exibido", c.DisplayNumber,
			"e o numero como ele aparece para o cliente; sem ele o gateway nao consegue dizer de qual numero esta instancia fala"},
		{"app_secret", c.AppSecret,
			"e o segredo do seu App na Meta; sem ele o gateway nao consegue verificar a assinatura dos webhooks que ela manda, e recusa todos"},
		{"token_envio", c.SendToken,
			"e o token permanente do System User; sem ele nenhuma mensagem sai"},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s esta vazio — %s", ErrIncompleteRegistration, field.name, field.why)
		}
	}
	if err := ValidateCallbackURL(c.CallbackURL); err != nil {
		return err
	}
	return ValidateCABundle(c.CABundle)
}

// Window says where an instance's registration window stands.
//
// TWO INSTANTS, and not "how much is left": a relative deadline computed
// here would become a stale number by the instant the response left the
// process. Whoever reads it subtracts using their own clock.
type Window struct {
	// OpenedAt is the consumer's FIRST insert. Zero means they never
	// inserted anything — the window hasn't started counting yet.
	OpenedAt time.Time
	// ClosesAt is OpenedAt + RegistrationWindow. Zero while OpenedAt is zero.
	ClosesAt time.Time
}

// IsOpen says whether the window accepts writes NOW.
//
// A window that never started is OPEN — and that's the owner's whole
// decision: the clock starts on the first insert, not on the instance's
// creation. "I create the instance today, in 5 days the consumer inserts
// something, the count starts there."
func (j Window) IsOpen(now time.Time) bool {
	if j.OpenedAt.IsZero() {
		return true
	}
	return !now.After(j.ClosesAt)
}

// WindowFrom builds the window from the stored stamp.
//
// AN UNREADABLE STAMP COUNTS AS CLOSED, and the choice is on the safe side:
// the two possible wrong readings are "treat as never inserted" (the window
// reopens on its own, and the rule becomes decorative precisely on the row
// someone edited by hand) and "treat as closed" (the consumer hits an
// error that says to talk to the owner, and the owner reopens it with a
// command that exists). The second costs a message; the first costs the
// guarantee.
func WindowFrom(stamp string) Window {
	if stamp == "" {
		return Window{}
	}
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		// An instant impossible to reach: any `now` is after it, so
		// IsOpen() answers false without needing a third state.
		return Window{OpenedAt: time.Unix(0, 0).UTC(), ClosesAt: time.Unix(0, 0).UTC()}
	}
	return Window{OpenedAt: when, ClosesAt: when.Add(RegistrationWindow)}
}

// RegisterMeta writes the consumer's Meta account onto an instance that
// ALREADY EXISTS.
//
// 🔴 THERE IS NO PATH BACK. The fields encrypted here follow the rule that
// has held since T-020: they go in and don't come back out. No surface
// decrypts them for display, and this route makes no exception — whoever
// wants to know if they're registered reads SummarizeInstance, which
// answers YES/NO without opening anything.
//
// 🔴 DOESN'T ACTIVATE THE INSTANCE, and the guarantee is STRUCTURAL: the
// `ativo` column doesn't appear in the UPDATE below. Registering proves
// nothing; SENDING proves. If registration activated it, a wrong
// credential would turn into an "active" instance that refuses everything
// — the defect T-074 found. The only path to `ativo = 1` remains
// ActivateInstance, called only by `zapgw fumaca`.
//
// EVERYTHING IN A SINGLE TRANSACTION because the window is read and written
// in the same movement: between a loose "is it still open?" and the
// UPDATE, another request from the same consumer could fit in, and both
// would write the first insert with different instants.
//
// Returns the window AS IT ENDED UP, so the caller can tell the consumer
// how much time they still have — the information they have no way of
// deducing on their own, and which decides whether they fix it today or
// tomorrow.
func (s *Store) RegisterMeta(slug string, c MetaRegistration, now time.Time) (Window, error) {
	if err := ValidateMetaRegistration(c); err != nil {
		return Window{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Window{}, fmt.Errorf("config: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var stamp string
	err = tx.QueryRow(`SELECT cadastro_em FROM instancia WHERE slug = ?`, slug).Scan(&stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return Window{}, ErrInstanceNotFound
	}
	if err != nil {
		return Window{}, fmt.Errorf("config: ler a janela de cadastro: %w", err)
	}

	window := WindowFrom(stamp)
	if !window.IsOpen(now) {
		return window, fmt.Errorf("%w: a primeira insercao foi em %s e a janela de %s fechou em %s",
			ErrRegistrationWindowClosed, stampOf(window.OpenedAt), RegistrationWindow, stampOf(window.ClosesAt))
	}
	// The FIRST insert is the one that starts the clock; the following ones
	// rewrite the SAME stamp. Swapping `janela.OpenedAt` for `now` here
	// would make the window restart on every change — and whoever touched
	// it every day would keep it open forever, which is half the owner's
	// decision (the other half is not counting from the instance's
	// creation). See the "instancia.cadastro_em" migration.
	if window.OpenedAt.IsZero() {
		window = Window{OpenedAt: now, ClosesAt: now.Add(RegistrationWindow)}
	}

	// Column and value go TOGETHER in this list, and not in two parallel
	// lists: that's how a positional swap between credentials becomes
	// impossible by construction (docs/ARMADILHAS.md, "Testes").
	encrypted := []struct {
		column    string
		plaintext string
	}{
		{"app_secret", c.AppSecret},
		{"token_envio", c.SendToken},
		{"callback_url", c.CallbackURL},
		{"bundle_ca", c.CABundle},
	}
	parts := []string{"waba_id = ?", "phone_number_id = ?", "numero_exibido = ?"}
	args := []any{strings.TrimSpace(c.WabaID), strings.TrimSpace(c.PhoneNumberID), strings.TrimSpace(c.DisplayNumber)}
	for _, field := range encrypted {
		ciphertext, err := s.vault.Encrypt(field.plaintext)
		if err != nil {
			return Window{}, fmt.Errorf("config: cifrar: %w", err)
		}
		parts = append(parts, field.column+" = ?")
		args = append(args, ciphertext)
	}
	parts = append(parts, "cadastro_em = ?")
	args = append(args, stampOf(window.OpenedAt))
	// token_envio ALWAYS comes filled in here (ValidateMetaRegistration
	// requires it — see above), so token_definido_em ALWAYS restarts along
	// with it, at the same instant: every path that writes the token also
	// records when it was defined (T-098), and this is one of them.
	// Restarting even when the value written is EQUAL to the previous one
	// is the safe side: at worst it moves the next renewal check up a
	// little, never delays it.
	parts = append(parts, "token_definido_em = ?")
	args = append(args, stampOf(now), slug)

	// Column names come from the constants above, never from outside; only
	// the VALUES are bound parameters. And `ativo` is NOT in the list —
	// see the header.
	if _, err := tx.Exec(
		`UPDATE instancia SET `+strings.Join(parts, ", ")+` WHERE slug = ?`, args...); err != nil {
		return Window{}, fmt.Errorf("config: gravar o cadastro da instancia: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Window{}, fmt.Errorf("config: commit do cadastro: %w", err)
	}
	return window, nil
}

// ReopenRegistrationWindow gives the consumer back the right to write — and
// ONLY that: it doesn't touch a single configuration field.
//
// WHY IT EXISTS (T-079, owner's decision): a consumer getting locked out
// with a wrong credential WILL happen, and without a command the way out
// would be `UPDATE` typed by hand into the production SQLite — precisely
// what T-048 existed to kill. Whoever registers stays the consumer; the
// owner only reopens the door.
//
// ERASES THE FIRST INSERT instead of granting "24 more hours from now",
// and the difference matters: the owner reopens it when they manage to
// talk to the consumer, and the consumer gets back to work when they can. A
// deadline counted from the REOPENING would consume the window while the
// two were still exchanging messages — the same defect as counting from
// the instance's creation.
func (s *Store) ReopenRegistrationWindow(slug string) error {
	res, err := s.db.Exec(`UPDATE instancia SET cadastro_em = '' WHERE slug = ?`, slug)
	if err != nil {
		return fmt.Errorf("config: reabrir a janela de cadastro: %w", err)
	}
	// RowsAffected is the only proof the slug existed: an UPDATE that
	// matches no row is NOT an error to SQLite. Without this, reopening a
	// mistyped slug would print success, the owner would leave thinking
	// they unlocked it, and the consumer would keep hitting the same error.
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("config: linhas afetadas: %w", err)
	}
	if rows == 0 {
		return ErrInstanceNotFound
	}
	return nil
}

// ActivateInstance is the ONLY path to `ativo = 1` in this project.
//
// WHO CALLS IT: only the smoke test (cmd/zapgw/smoke.go), and only after
// having proven, against the real Meta, that the token is accepted and that
// a message goes out with an id. The guarantee "instance is born paused"
// (see CreateInstance) only holds as long as this remains the only door: a
// parallel activation path — a `--forcar` flag, an administration
// endpoint, a "just for testing" shortcut — undoes the whole guarantee,
// because a misconfigured instance regains the ability to enter production
// "working" and the defect reappears with the customer on the other end.
//
// There is NO silent success: a nonexistent slug returns
// ErrInstanceNotFound. Without this, the smoke test would print
// "instance activated" over a mistyped slug, whoever operated it would
// leave thinking they turned the channel on, and the real instance would
// stay paused — the defect would only show up on the first real send, far
// from the cause.
func (s *Store) ActivateInstance(slug string) error {
	return s.setActive(slug, true)
}

// PauseInstance takes the instance offline without deleting anything:
// while `ativo = 0`, the webhook responds 503 and so does sending.
//
// It's the emergency button — a channel with a revoked or misconfigured
// token leaves production through here, and only comes back via the smoke
// test. It flags a nonexistent slug for the same reason as ActivateInstance:
// pausing a mistyped slug "successfully" leaves running exactly the
// instance someone wanted to take offline.
func (s *Store) PauseInstance(slug string) error {
	return s.setActive(slug, false)
}

func (s *Store) setActive(slug string, active bool) error {
	value := 0
	if active {
		value = 1
	}
	res, err := s.db.Exec(`UPDATE instancia SET ativo = ? WHERE slug = ?`, value, slug)
	if err != nil {
		return fmt.Errorf("config: mudar estado da instancia: %w", err)
	}
	// RowsAffected is the only proof the slug existed: an UPDATE that
	// matches no row is NOT an error to SQLite.
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("config: linhas afetadas: %w", err)
	}
	if rows == 0 {
		return ErrInstanceNotFound
	}
	return nil
}

// Rotation is a request to swap secrets of an instance that ALREADY EXISTS.
//
// POINTER, not string, because the difference between "swap for empty" and
// "don't touch" is the difference between a rotation and a disaster: nil
// means DON'T TOUCH. A PARTIAL rotation is the normal case — the spec
// §7.3 procedure swaps only app_secret —, and silently zeroing out the rest
// would erase, for example, the token_envio of an instance that is
// actually sending right now.
//
// THE SLUG IS NOT HERE, and the absence is the guarantee: it is IMMUTABLE
// (it becomes /v1/inbound/{slug}, already pasted into Meta's panel), so it
// only enters RotateInstance as WHO is swapping, never as what to swap.
//
// CABundle isn't here either: T-017 didn't ask for it and no instance has a
// bundle registered today. When it becomes necessary to swap one, the field
// goes into THIS type and passes through ValidateCABundle — not into a
// second command with its own rule.
//
// IgID (T-102) IS NOT A SECRET — it's an identifier, like
// WabaID/PhoneNumberID on Instance — but it lives here the same way as the
// five fields above: the pointer distinguishes "don't touch" from "swap for
// this", and without this field the only way to fix a wrong ig_id in
// production would be to delete and recreate the instance (loses the
// consumer's link) or hand-typed SQL — the dead end T-097 opened by letting
// CREATE take --ig-id without letting anything CORRECT it.
type Rotation struct {
	AppSecret      *string
	VerifyToken    *string
	SendToken      *string
	DeliverySecret *string
	CallbackURL    *string
	IgID           *string
}

// ErrEmptyRotation: the rotation didn't ask to swap any field.
//
// It's an error instead of a no-op because the dangerous outcome is the
// SILENT one: whoever forgot to export the variable in the shell would see
// "rotated" and leave thinking the real secret is on the gateway.
var ErrEmptyRotation = errors.New("config: rotacao sem campo nenhum para trocar")

// RotateInstance swaps, on an instance that ALREADY EXISTS, only the
// fields filled in on r — through the same encrypted paths as provisioning.
//
// The callback_url is validated HERE, and not only in the subcommand that
// types it in, for the same reason as CreateInstance: the subcommand is
// just the first write path, and this project's mother-of-all-traps is
// "the rule holds in one place and not in the next". A rotation path that
// accepted an external http:// would make the raw body's delivery cross the
// network readable — exactly what creation closes off.
//
// DOESN'T RANDOMLY GENERATE ANYTHING. In provisioning, generating a missing
// secret is convenience; here it would mean swapping a secret IN USE for a
// value nobody knows.
//
// IgID (T-102) IS VALIDATED AGAINST THE TYPE ALREADY STORED, and that's why
// it needs a read before the UPDATE — unlike the five encrypted fields
// above, which don't depend on anything already in the database. The
// function that does the check is the SAME ONE creation calls
// (ValidateInstanceType): two rules for the same invariant ("ig_id only
// exists on tipo=instagram") would diverge, and that's this project's
// mother-of-all-traps.
func (s *Store) RotateInstance(slug string, r Rotation) error {
	if r.CallbackURL != nil {
		if err := ValidateCallbackURL(*r.CallbackURL); err != nil {
			return err
		}
	}
	if r.IgID != nil {
		// Only the type matters here: waba_id/phone_number_id/numero_exibido
		// don't change in this rotation, and ValidateInstanceType only
		// checks them to refuse DOUBLE identification on an instagram
		// instance — passing "" for all three is safe because the schema
		// itself already guarantees an instagram instance has them empty
		// (the same function would have refused them at creation if they weren't).
		var currentType string
		if err := s.db.QueryRow(`SELECT tipo FROM instancia WHERE slug = ?`, slug).Scan(&currentType); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInstanceNotFound
			}
			return fmt.Errorf("config: ler tipo da instancia para validar --ig-id: %w", err)
		}
		if _, err := ValidateInstanceType(currentType, "", "", "", *r.IgID); err != nil {
			return err
		}
	}

	// Column and value go TOGETHER in this list, and not in two parallel
	// lists: that's how a positional swap between credentials becomes
	// impossible by construction (docs/ARMADILHAS.md, "Testes").
	fields := []struct {
		column   string
		newValue *string
	}{
		{"app_secret", r.AppSecret},
		{"verify_token", r.VerifyToken},
		{"token_envio", r.SendToken},
		{"segredo_entrega", r.DeliverySecret},
		{"callback_url", r.CallbackURL},
	}
	var parts []string
	var args []any
	for _, c := range fields {
		if c.newValue == nil {
			continue // DON'T TOUCH
		}
		ciphertext, err := s.vault.Encrypt(*c.newValue)
		if err != nil {
			return fmt.Errorf("config: cifrar: %w", err)
		}
		parts = append(parts, c.column+" = ?")
		args = append(args, ciphertext)
	}
	// ig_id is NOT ENCRYPTED (identifier, not a secret — same criterion as
	// WabaID/PhoneNumberID on Instance), so it stays OUTSIDE the loop
	// above instead of going through s.vault.Encrypt.
	if r.IgID != nil {
		parts = append(parts, "ig_id = ?")
		args = append(args, *r.IgID)
	}
	if len(parts) == 0 {
		return ErrEmptyRotation
	}
	// token_definido_em restarts TOGETHER with token_envio, for the same
	// reason as creation and registration (T-098): every path that writes
	// the token also records when it was defined, otherwise Instagram's
	// renewal loop would measure the age of a token that was actually just
	// swapped by the owner.
	if r.SendToken != nil {
		parts = append(parts, "token_definido_em = ?")
		args = append(args, stampOf(time.Now()))
	}
	args = append(args, slug)

	// Column names come from the constants above, never from outside; only
	// the VALUES are bound parameters.
	res, err := s.db.Exec(
		`UPDATE instancia SET `+strings.Join(parts, ", ")+` WHERE slug = ?`, args...)
	if err != nil {
		return fmt.Errorf("config: rotacionar segredos da instancia: %w", err)
	}
	// RowsAffected is the only proof the slug existed: an UPDATE that
	// matches no row is NOT an error to SQLite. Without this, rotating a
	// mistyped slug would print success and the real instance would keep
	// the old secret — and that would only show up once Meta started delivering.
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("config: linhas afetadas: %w", err)
	}
	if rows == 0 {
		return ErrInstanceNotFound
	}
	return nil
}

// RenewInstagramTokenAt writes the NEW TOKEN Meta returned for an
// Instagram renewal, and restarts the validity from `now` — the ONLY
// write path used by the automatic loop
// (internal/outbound/instagram_renewer.go, T-098).
//
// SEPARATE FROM RotateInstance ON PURPOSE, and it's not duplication:
// the decision of WHICH field to touch is AUTOMATIC here (always
// token_envio + token_definido_em + token_renovado_em, never the other four
// secrets Rotation covers) — mixing the two routes would leave a loop
// without human supervision able to touch a field only the owner should
// choose to touch.
//
// `AND tipo = ?` IN THE WHERE CLAUSE IS DEFENSE IN DEPTH, not just
// documentation: even if a bug in the caller one day sends a WHATSAPP
// instance's slug here, the UPDATE matches no row and returns
// ErrInstanceNotFound instead of writing a "token_envio" that nothing
// else in the gateway expects to see change on its own.
//
// TOKEN_RENOVADO_EM AND TOKEN_DEFINIDO_EM RECEIVE THE SAME STAMP, but they
// answer different questions (see the comment on the migration
// "instancia.token_definido_em-e-token_renovado_em"): only the automatic
// loop writes the first one, and that's why it's proof it has already
// worked at least once — a manual rotation by the owner
// (RotateInstance) NEVER touches token_renovado_em.
func (s *Store) RenewInstagramTokenAt(slug, newToken string, now time.Time) error {
	ciphertext, err := s.vault.Encrypt(newToken)
	if err != nil {
		return fmt.Errorf("config: cifrar: %w", err)
	}
	stamp := stampOf(now)
	res, err := s.db.Exec(
		`UPDATE instancia SET token_envio = ?, token_definido_em = ?, token_renovado_em = ?
		   WHERE slug = ? AND tipo = ?`,
		ciphertext, stamp, stamp, slug, TypeInstagram)
	if err != nil {
		return fmt.Errorf("config: renovar o token do instagram: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("config: linhas afetadas: %w", err)
	}
	if rows == 0 {
		return ErrInstanceNotFound
	}
	return nil
}

// ErrInstanceActive: the instance is ACTIVE and therefore cannot be removed.
//
// Whoever really wants to remove it pauses it first, and pausing is
// REVERSIBLE — removing is not. The order "pause, look, then delete" is
// cheap; the reverse order has no undo, because the six encrypted fields
// don't exist anywhere outside the database (and the encryption key alone
// doesn't recreate them).
var ErrInstanceActive = errors.New("config: instancia ativa nao pode ser removida")

// tablesWithSlug is EVERY table whose row belongs to ONE instance, in the
// order they have to be deleted (children before `instancia`, because
// `consumidor_instancia.slug` references `instancia.slug` and the
// foreign_keys PRAGMA is ON).
//
// THE LIST IS EXPLICIT ON PURPOSE, and not discovered at runtime: a removal
// that swept, on its own, every table with a `slug` column would also
// delete a future table meant to SURVIVE the instance (an audit record, for
// example) without anyone having decided that.
//
// AND THAT'S WHY IT HAS A MECHANICAL GUARD:
// TestRemoveInstanceCOVERSEveryTableWithASlugColumn asks the database itself
// which tables have a `slug` column and turns RED if any isn't here.
// Enumerating from memory is what leaves the next table silently orphaned —
// it has already happened: `certificado_do_callback` was born in T-064, one
// day after T-048 was written listing two tables.
var tablesWithSlug = []string{
	"contador",
	"certificado_do_callback",
	"numero_na_meta",
	"transito",
	"consumidor_instancia",
	"instancia",
}

// RowsDeleted is how many rows came out of each table in a removal.
//
// The number goes back to the caller because the removal is irreversible
// and without confirmation: printing "deleted 3 consumer links" on screen
// is the only chance for someone to notice, BEFORE closing the terminal,
// that they deleted the wrong instance.
type RowsDeleted struct {
	Table string
	Rows  int64
}

// RemoveInstance deletes the instance and EVERY row that belongs to it,
// in a single transaction.
//
// WHY THIS EXISTS (T-048): until 2026-07-28 there was no `zapgw instancia
// remover`, and deleting a lab instance meant a `DELETE` typed by hand into
// the PRODUCTION SQLite. The cost isn't typing SQL — it's WHERE it's
// typed: a `DELETE ... WHERE slug = '…'` with the wrong slug, or without
// the `WHERE`, deletes a real consumer's instance, and the database holds
// the six encrypted fields nobody has memorized.
//
// IN A SINGLE TRANSACTION because half-deleted is the worst outcome: an
// `instancia` removed with `counter` left over leaves rows `zapgw estado`
// doesn't show (it walks the registered instances) and that only disappear
// on the 90-day TTL — invisible and alive. And if the slug gets reused,
// they show back up, with numbers from another life.
//
// REFUSES AN ACTIVE INSTANCE, and the refusal lives HERE and not only in
// the subcommand: the subcommand is just the first removal path. The check
// is done INSIDE the transaction, otherwise between "is it paused?" and the
// `DELETE` a `fumaca` from another session could fit in and activate the
// instance.
func (s *Store) RemoveInstance(slug string) ([]RowsDeleted, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("config: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var active int
	err = tx.QueryRow(`SELECT ativo FROM instancia WHERE slug = ?`, slug).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInstanceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("config: ler estado da instancia: %w", err)
	}
	if active != 0 {
		return nil, ErrInstanceActive
	}

	var deleted []RowsDeleted
	for _, table := range tablesWithSlug {
		// The table name comes from this file's list, never from outside;
		// only the slug is a bound parameter. And the `WHERE slug = ?` is
		// what separates "delete this instance" from "empty the table" —
		// see T-048's mandatory mutation.
		res, err := tx.Exec(`DELETE FROM `+table+` WHERE slug = ?`, slug)
		if err != nil {
			return nil, fmt.Errorf("config: apagar %s da instancia %q: %w", table, slug, err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("config: linhas afetadas em %s: %w", table, err)
		}
		deleted = append(deleted, RowsDeleted{Table: table, Rows: rows})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("config: commit da remocao: %w", err)
	}
	return deleted, nil
}

func (s *Store) FindInstance(slug string) (Instance, error) {
	return s.findBy("slug = ?", slug)
}

func (s *Store) InstanceByPhoneNumberID(pnid string) (Instance, error) {
	return s.findBy("phone_number_id = ?", pnid)
}

func (s *Store) InstanceByWabaID(waba string) (Instance, error) {
	return s.findBy("waba_id = ?", waba)
}

func (s *Store) findBy(where string, arg string) (Instance, error) {
	var i Instance
	var appSecret, verifyToken, sendToken, callbackURL, deliverySecret, caBundle string
	var active int

	err := s.db.QueryRow(`
		SELECT slug, waba_id, phone_number_id, numero_exibido,
		       app_secret, verify_token, token_envio, callback_url,
		       segredo_entrega, bundle_ca, timeout_ms, ativo, tipo, ig_id, token_definido_em
		  FROM instancia WHERE `+where, arg).
		Scan(&i.Slug, &i.WabaID, &i.PhoneNumberID, &i.DisplayNumber,
			&appSecret, &verifyToken, &sendToken, &callbackURL,
			&deliverySecret, &caBundle, &i.TimeoutMs, &active, &i.Type, &i.IgID, &i.TokenSetAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, ErrInstanceNotFound
	}
	if err != nil {
		return Instance{}, fmt.Errorf("config: buscar instancia: %w", err)
	}
	i.Active = active != 0

	targets := []*string{&i.AppSecret, &i.VerifyToken, &i.SendToken, &i.CallbackURL, &i.DeliverySecret}
	for n, ciphertext := range []string{appSecret, verifyToken, sendToken, callbackURL, deliverySecret} {
		plaintext, err := s.vault.Decrypt(ciphertext)
		if err != nil {
			return Instance{}, fmt.Errorf("config: decifrar: %w", err)
		}
		*targets[n] = plaintext
	}
	if i.CABundle, err = s.decryptOptional(caBundle); err != nil {
		return Instance{}, fmt.Errorf("config: decifrar bundle de CA: %w", err)
	}
	return i, nil
}

// --- Reading for operations: what can be known WITHOUT DECRYPTING anything --------------

// EncryptedField tells, for one of the six encrypted fields, only WHETHER it
// is registered — never the value.
//
// Name and state go TOGETHER in a single record, and not in two parallel
// lists: that's how a positional swap between fields becomes impossible by
// construction. A line saying "callback_url: yes" about app_secret's state
// is worse than not having the line (docs/ARMADILHAS.md, "Testes").
type EncryptedField struct {
	Name       string // the COLUMN's name, which is what the command and the docs call it
	Registered bool
}

// InstanceSummary is everything that can be said about an instance without
// opening any secret: the identifiers (which aren't secret), the state, and
// the PRESENCE of each encrypted field.
//
// It's a type separate from Instance on purpose. Instance carries the
// six fields IN THE CLEAR, and returning it to someone who only wants to
// know "which ones exist?" would put N businesses' credentials into the
// memory of a read command, with no use for them.
type InstanceSummary struct {
	Slug          string
	DisplayNumber string
	PhoneNumberID string
	WabaID        string
	TimeoutMs     int
	Active        bool
	// StampsSince is the instant (UTC/RFC3339) from which this instance
	// has been stamping the counter — the INSTRUMENT's age, not the
	// data's. An instance created after T-070's migration carries its own
	// birth; an instance that already existed carries the instant the
	// migration ran. See the "instancia.carimbos_desde" migration for the
	// reasoning behind each choice.
	StampsSince string
	// RegisteredAt is the stamp of the consumer's FIRST insert (T-079), or ""
	// if they never wrote anything. It comes out RAW, not already
	// converted into "open"/"closed", because the answer depends on the
	// clock of whoever is asking — whoever wants the verdict goes through
	// WindowFrom(...).IsOpen(now), the same function registration uses to
	// DECIDE. Two separate accounts would diverge, and the symptom would
	// be the screen saying "open" about a window that refuses writes.
	RegisteredAt string

	// Type is TypeWhatsApp or TypeInstagram (T-097/T-098) — it needs to be
	// in the summary (and not only on Instance) because GET /v1/estado
	// (a read WITHOUT secrets) decides, based on it, whether to show the
	// Instagram token renewal block or "nao_se_aplica".
	Type string
	// IgID is the Instagram-scoped Business Account ID (T-102) — only
	// filled in on TypeInstagram. It needs to be in the summary (and not
	// only on Instance, which carries the six fields IN THE CLEAR) for
	// the SAME reason as Type: it's an IDENTIFIER, not a secret, and a
	// secret-free read command has to be able to show it (T-103 — until
	// then `zapgw instancia mostrar` had no way to confirm the stored
	// value, not even after fixing it).
	IgID string
	// TokenSetAt and TokenRenewedAt answer, without decrypting
	// anything, "when was the current token defined" and "when did the
	// automatic loop last successfully renew it" (T-098) — see the comment
	// on Instance.TokenSetAt and on the migration
	// "instancia.token_definido_em-e-token_renovado_em" for the difference
	// between the two. "" means "never", in both fields.
	TokenSetAt     string
	TokenRenewedAt string

	Encrypted []EncryptedField // the six, always in the same order
}

// StateOf is the word that describes an instance's state — "ativa" or
// "pausada" — for EVERY surface of this gateway: the command line
// (cmd/zapgw/provision.go, cmd/zapgw/state.go) and the GET /v1/estado
// route (internal/outbound/state_handler.go).
//
// ONE function, and not the same condition written in each place: two
// spellings would make a `grep pausada` lie, and on the day a third state
// exists (draining? migrating?) one of the copies would keep saying
// "active" for it.
func StateOf(r InstanceSummary) string {
	if r.Active {
		return "ativa"
	}
	return "pausada"
}

// summaryColumns is the column list for BOTH summary queries. A single
// constant because two lists would diverge, and the divergence would show
// up as a swapped field on the operator's screen.
const summaryColumns = `slug, numero_exibido, phone_number_id, waba_id, timeout_ms, ativo,
	       carimbos_desde, cadastro_em, tipo, ig_id, token_definido_em, token_renovado_em,
	       app_secret, verify_token, token_envio, callback_url, segredo_entrega, bundle_ca`

// ListInstances returns the summary of ALL instances, in slug order.
//
// FIXED ORDER on purpose: without ORDER BY, SQLite can return the same list
// in another order on each call, and "what changed since yesterday?" stops
// having a comparable answer.
func (s *Store) ListInstances() ([]InstanceSummary, error) {
	rows, err := s.db.Query(`SELECT ` + summaryColumns + ` FROM instancia ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("config: listar instancias: %w", err)
	}
	defer rows.Close()

	var list []InstanceSummary
	for rows.Next() {
		r, err := s.readSummary(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("config: ler instancia da lista: %w", err)
		}
		list = append(list, r)
	}
	// rows.Err() is NOT optional: an error midway through iteration ends
	// the loop as if the list had finished. Without this check, the list
	// comes out SHORT and nothing flags it — and an instance list shorter
	// than reality is this project's most expensive failure shape
	// (docs/ARMADILHAS.md, "Meta").
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar instancias: %w", err)
	}
	return list, nil
}

// SummarizeInstance returns the summary of ONE instance.
//
// A nonexistent slug is ErrInstanceNotFound, never a zeroed summary:
// the empty summary would look like a paused instance with no secret at
// all, and whoever typed the wrong slug would go fix the wrong instance.
func (s *Store) SummarizeInstance(slug string) (InstanceSummary, error) {
	r, err := s.readSummary(s.db.QueryRow(
		`SELECT `+summaryColumns+` FROM instancia WHERE slug = ?`, slug).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return InstanceSummary{}, ErrInstanceNotFound
	}
	if err != nil {
		return InstanceSummary{}, fmt.Errorf("config: resumir instancia: %w", err)
	}
	return r, nil
}

// readSummary builds the summary from an already-selected row. It receives
// the Scan (from *sql.Row or from *sql.Rows) so BOTH reads read the same
// columns in the same order — two copies would diverge, and the symptom
// would be `listar` and `mostrar` disagreeing about the same instance.
func (s *Store) readSummary(scan func(...any) error) (InstanceSummary, error) {
	var r InstanceSummary
	var active int
	var appSecret, verifyToken, sendToken, callbackURL, deliverySecret, caBundle string
	if err := scan(&r.Slug, &r.DisplayNumber, &r.PhoneNumberID, &r.WabaID, &r.TimeoutMs, &active,
		&r.StampsSince, &r.RegisteredAt, &r.Type, &r.IgID, &r.TokenSetAt, &r.TokenRenewedAt,
		&appSecret, &verifyToken, &sendToken, &callbackURL, &deliverySecret, &caBundle); err != nil {
		return InstanceSummary{}, err
	}
	r.Active = active != 0

	// Name and ciphertext go TOGETHER in this list, not in two parallel
	// lists: that's how "callback_url: yes" said about app_secret's state
	// becomes impossible by construction.
	for _, c := range []struct{ name, ciphertext string }{
		{"app_secret", appSecret},
		{"verify_token", verifyToken},
		{"token_envio", sendToken},
		{"callback_url", callbackURL},
		{"segredo_entrega", deliverySecret},
		{"bundle_ca", caBundle},
	} {
		r.Encrypted = append(r.Encrypted, EncryptedField{Name: c.name, Registered: s.isRegistered(c.ciphertext)})
	}
	return r, nil
}

// cadastrado tells whether the encrypted column holds any value — WITHOUT DECRYPTING.
//
// WHY NOT `cifrado != ""`: there are TWO forms of "never registered", and
// both coexist in the database running today.
//
//   - Column literally EMPTY: the empty string an ALTER TABLE sets as
//     DEFAULT. That's how bundle_ca reached every row before migration 3.
//   - THE CIPHERTEXT OF "": CreateInstance encrypts the six fields,
//     including the ones that came empty. The callback_url of an
//     outbound-only instance — the tenant-one case — is a perfectly valid
//     ciphertext of the empty string, which is why `!= ""` would mark it
//     as REGISTERED. A screen saying the instance delivers to a consumer
//     that doesn't exist sends the operator looking for a defect that
//     isn't there.
//
// HOW IT'S KNOWN WITHOUT OPENING IT: Encrypt writes base64(nonce ||
// sealed), and AES-GCM's sealed output is exactly len(claro)+Overhead()
// bytes. So the ciphertext of "" has, decoded, NonceSize()+Overhead()
// bytes, and any non-empty value has more. It's a question about SIZE, not
// about content: no secret byte is exposed, and the math doesn't depend on
// the key — which is what keeps this read answering correctly with the
// WRONG encryption key, precisely the incident where seeing the system's
// state is worth the most.
//
// A ciphertext that fails to decode from base64 counts as REGISTERED: it
// wasn't written by this binary, but there is content there — saying
// "empty" would hide a corrupted row instead of showing it.
func (s *Store) isRegistered(ciphertext string) bool {
	if ciphertext == "" {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return true
	}
	return len(raw) != s.vault.aead.NonceSize()+s.vault.aead.Overhead()
}

// decryptOptional treats the EMPTY column as "never registered".
//
// WITHOUT THIS BRANCH THE MIGRATION BRINGS DOWN WHAT WAS ALREADY RUNNING:
// bundle_ca arrived via ALTER TABLE, and an ALTER TABLE's DEFAULT is the
// empty string — which is not a valid ciphertext (Decrypt expects base64
// of nonce+sealed). Every instance prior to the migration would start
// returning "config: decifrar", and since EVERY request starts by looking
// up the instance, the webhook and sending would die together on the first
// call after the update.
//
// The distinction is unambiguous: a row written by CreateInstance stores
// the ciphertext of "" (which is NOT ""), so a literally empty column can
// only have come from the migration's DEFAULT.
func (s *Store) decryptOptional(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	return s.vault.Decrypt(ciphertext)
}

// ErrConsumerNotFound: no consumer has that token (authentication)
// or that name (rotation, T-055). It's the SAME error on purpose: both
// cases say "that consumer doesn't exist here", and the caller
// distinguishes by what it asked.
var ErrConsumerNotFound = errors.New("config: consumidor nao encontrado")

// Consumer is who can call the gateway, and the instances they can use.
//
// The consumidor->instances link is the project's requirement 3: send on
// behalf of N businesses WITHOUT confusing one with another. It is CHECKED
// on every call, never assumed — a token leaked from system A doesn't send
// through system B's number.
type Consumer struct {
	Name      string
	Instances []string
}

// HashToken returns the hash the token is stored under.
//
// WHY HASH AND NOT ENCRYPTION: there is no use case that needs the token
// back — we only need to PROVE that whoever arrived knows it. Encryption
// stores for reading later; a hash doesn't come back. If the file leaks, an
// encrypted token still only depends on the key being alongside it; a hash
// doesn't return the token even with the key.
//
// No salt, on purpose: a salt would prevent SEARCHING by token (we'd have
// to scan every consumer and compare one by one). The token is generated
// by us, long and random — it's not a human password, so a dictionary
// attack doesn't apply.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateConsumer writes the consumer and the link to the instances they
// can use. The token is stored as a HASH — from here on it doesn't exist
// anywhere else, and there is no way to recover it. Lost it, generate
// another.
func (s *Store) CreateConsumer(name, token string, instances []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("config: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`INSERT INTO consumidor (nome, token_hash) VALUES (?,?)`,
		name, HashToken(token)); err != nil {
		return fmt.Errorf("config: inserir consumidor: %w", err)
	}
	for _, slug := range instances {
		if _, err := tx.Exec(
			`INSERT INTO consumidor_instancia (consumidor, slug) VALUES (?,?)`,
			name, slug); err != nil {
			return fmt.Errorf("config: vincular instancia %q: %w", slug, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("config: commit: %w", err)
	}
	return nil
}

// RotateConsumer swaps the token of a consumer that ALREADY EXISTS,
// and the old one stops working at the same instant.
//
// WHY AN UPDATE, AND NOT DELETE AND RECREATE (T-055): the `consumidor` row
// is referenced by `consumidor_instancia`, which is WHO can use WHICH
// instance. Deleting and recreating would bring the links down with it,
// and a lost link doesn't turn into a rotation error — it turns into a 403
// on the consumer's next send, far from the cause and with no one
// remembering a rotation happened. The UPDATE doesn't touch the other table.
//
// A SINGLE STATEMENT IS THE TRANSACTION: there is no window where both
// tokens work. `token_hash` is UNIQUE and authentication looks up by it
// (ConsumerByToken), so overwriting the column IS the revocation —
// there is no second place keeping the old hash, and that's why there is
// no "flag to keep the previous one valid": it would need a second column
// that doesn't exist.
//
// DOESN'T GENERATE ANYTHING HERE, for the same reason as
// RotateInstance: the value has to go back to the caller, because
// this is the ONLY instant it exists outside the hash — the store keeps
// only HashToken(token), and a hash doesn't come back.
func (s *Store) RotateConsumer(name, token string) error {
	res, err := s.db.Exec(
		`UPDATE consumidor SET token_hash = ? WHERE nome = ?`, HashToken(token), name)
	if err != nil {
		return fmt.Errorf("config: rotacionar token do consumidor: %w", err)
	}
	// RowsAffected is the only proof the name existed: an UPDATE that
	// matches no row is NOT an error to SQLite. Without this, rotating a
	// mistyped name would print a new token looking like success, and the
	// exposed token — the very thing the rotation exists to revoke — would
	// keep working.
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("config: linhas afetadas: %w", err)
	}
	if rows == 0 {
		return ErrConsumerNotFound
	}
	return nil
}

// ListConsumers returns every consumer with the instances they can
// use, in stable order.
//
// NO TOKEN comes out of here, not even the hash: the question this method
// answers is "who has access to this instance?", and it's answered without
// the secret. The hash on screen would be useless and would still give a
// terminal backup a target for an offline comparison.
func (s *Store) ListConsumers() ([]Consumer, error) {
	// LEFT JOIN, and not JOIN: a consumer with NO link at all is exactly
	// the case that needs to show up in the list — they authenticate and
	// get 403 on everything, a symptom that doesn't look like the cause. A
	// JOIN would hide it.
	rows, err := s.db.Query(`
		SELECT c.nome, COALESCE(ci.slug, '')
		FROM consumidor c
		LEFT JOIN consumidor_instancia ci ON ci.consumidor = c.nome
		ORDER BY c.nome, ci.slug`)
	if err != nil {
		return nil, fmt.Errorf("config: listar consumidores: %w", err)
	}
	defer rows.Close()

	var list []Consumer
	for rows.Next() {
		var name, slug string
		if err := rows.Scan(&name, &slug); err != nil {
			return nil, fmt.Errorf("config: ler consumidor: %w", err)
		}
		if len(list) == 0 || list[len(list)-1].Name != name {
			list = append(list, Consumer{Name: name})
		}
		if slug != "" {
			current := &list[len(list)-1]
			current.Instances = append(current.Instances, slug)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar consumidores: %w", err)
	}
	return list, nil
}

// ConsumerByToken finds the consumer by the hash of the presented token.
func (s *Store) ConsumerByToken(token string) (Consumer, error) {
	var c Consumer
	err := s.db.QueryRow(
		`SELECT nome FROM consumidor WHERE token_hash = ?`, HashToken(token)).Scan(&c.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Consumer{}, ErrConsumerNotFound
	}
	if err != nil {
		return Consumer{}, fmt.Errorf("config: buscar consumidor: %w", err)
	}

	rows, err := s.db.Query(
		`SELECT slug FROM consumidor_instancia WHERE consumidor = ? ORDER BY slug`, c.Name)
	if err != nil {
		return Consumer{}, fmt.Errorf("config: instancias do consumidor: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return Consumer{}, fmt.Errorf("config: ler instancia: %w", err)
		}
		c.Instances = append(c.Instances, slug)
	}
	if err := rows.Err(); err != nil {
		return Consumer{}, fmt.Errorf("config: iterar instancias: %w", err)
	}
	return c, nil
}

// --- Idempotency -----------------------------------------------------------
//
// THE NAMED EXCEPTION to the "configuration yes, message never" rule.
//
// Keeps only (consumidor, key) -> wa_message_id. There is NO content,
// recipient, or conversation: it's a DELIVERY record, with a short TTL.
// Without it, the Idempotency-Key the contract promises is fake, and a
// retry duplicates a message on a real customer's phone.
//
// IF SOMEONE PROPOSES storing the message body here "to resend later",
// that's the QUEUE — and the queue belongs to the consumer. This exception
// stops right here.
//
// The key is per CONSUMER: it's chosen by them, and two systems can choose
// the same one without knowing it.

// ErrKeyWithDifferentRequest: the key was already used, but for a DIFFERENT request.
//
// Without this guard the outcome is silent and costly: the gateway would
// return 200 with the FIRST send's id, the consumer would record "sent",
// and the second message would never go out. The end customer would never
// know — and neither would the consumer.
//
// It happens because the contract recommends using the entity's id as the
// key, and the same entity usually sends several messages (reminder,
// billing, apology).
var ErrKeyWithDifferentRequest = errors.New("config: chave de idempotencia ja usada com outro pedido")

// ErrIdempotencyVanished: the key didn't exist at confirmation time.
//
// Means the message WENT OUT and the record was lost (a purge in the
// middle, or an out-of-order call). A consumer retry with the same key
// will reserve again and RESEND. It's the only path where idempotency
// fails without anything else flagging it, and that's why it needs its own
// error.
var ErrIdempotencyVanished = errors.New("config: registro de idempotencia sumiu antes da confirmacao")

// ReserveIdempotency tries to take ownership of (consumidor, key) for
// the request identified by requestHash.
//
// Returns:
//   - (id, false, nil)                    — already sent before (same
//     request); use that id, don't send again;
//   - ("", false, nil)                    — another send with that key is
//     IN PROGRESS (same request);
//   - ("", true,  nil)                    — ownership is yours; send, then
//     Confirmar or Liberar;
//   - ("", false, ErrKeyWithDifferentRequest) — the key was already used for a
//     DIFFERENT request; doesn't reserve, doesn't return another message's id.
func (s *Store) ReserveIdempotency(consumer, key, requestHash string) (string, bool, error) {
	res, err := s.db.Exec(`
		INSERT INTO idempotencia (consumidor, chave, wa_message_id, hash_pedido, criado_em)
		VALUES (?,?,'',?,?)
		ON CONFLICT (consumidor, chave) DO NOTHING`,
		consumer, key, requestHash, time.Now().Unix())
	if err != nil {
		return "", false, fmt.Errorf("config: reservar idempotencia: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return "", false, fmt.Errorf("config: linhas afetadas: %w", err)
	}
	if rows == 1 {
		return "", true, nil // ownership is ours
	}

	// Someone got here first. Either they already finished (has an id), or
	// they're still sending.
	var id, storedHash string
	err = s.db.QueryRow(
		`SELECT wa_message_id, hash_pedido FROM idempotencia WHERE consumidor = ? AND chave = ?`,
		consumer, key).Scan(&id, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		// A race with a purge between the INSERT and the SELECT. Treating
		// it as "in progress" is the conservative choice: never duplicates.
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("config: ler idempotencia: %w", err)
	}

	// An EMPTY stored hash doesn't count as "equal to anything" — the
	// comparison is deliberately raw.
	//
	// Since the schema migration, this record REALLY EXISTS: migration 2
	// adds hash_pedido with an empty DEFAULT to rows reserved by an older
	// binary. A retry of those rows, done after the update and within the
	// 72h TTL, lands here and gets ErrKeyWithDifferentRequest — a false 422.
	//
	// It's the deliberate choice between a loud 422 in a narrow window (the
	// keys in flight at the moment of the update, which expire on their
	// own in 72h) and letting ANY different request slip past the guard
	// whenever the stored hash is empty. The second is the SILENT outcome
	// ErrKeyWithDifferentRequest exists to prevent: 200 with the first
	// message's id and the second one never goes out.
	if storedHash != requestHash {
		return "", false, ErrKeyWithDifferentRequest
	}
	return id, false, nil
}

// ConfirmIdempotency writes the id Meta returned.
func (s *Store) ConfirmIdempotency(consumer, key, waMessageID string) error {
	res, err := s.db.Exec(
		`UPDATE idempotencia SET wa_message_id = ? WHERE consumidor = ? AND chave = ?`,
		waMessageID, consumer, key)
	if err != nil {
		return fmt.Errorf("config: confirmar idempotencia: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("config: linhas afetadas: %w", err)
	}
	if rows == 0 {
		return ErrIdempotencyVanished
	}
	return nil
}

// ReleaseIdempotency returns the key to the consumer when the send FAILED.
//
// Without this, an attempt Meta refused would burn the key forever — and
// the consumer would lose the message for having tried once.
func (s *Store) ReleaseIdempotency(consumer, key string) error {
	_, err := s.db.Exec(
		`DELETE FROM idempotencia WHERE consumidor = ? AND chave = ?`, consumer, key)
	if err != nil {
		return fmt.Errorf("config: liberar idempotencia: %w", err)
	}
	return nil
}

// PurgeIdempotency deletes records older than `before` and returns how many.
//
// A short TTL is what keeps the exception from turning into history.
// Without purging, the table grows forever and becomes exactly the "store
// the message" the project forbids.
func (s *Store) PurgeIdempotency(before time.Time) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM idempotencia WHERE criado_em < ?`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("config: purgar idempotencia: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("config: linhas purgadas: %w", err)
	}
	return int(n), nil
}
