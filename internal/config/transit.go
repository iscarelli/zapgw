// TRANSIT log (T-091, T-094): answers "did this message pass through
// here?" without storing CONTENT — but, since T-094, WITH the phone number
// and the wamid in the CLEAR.
//
// THE QUESTION THIS CLOSES, and why it had no answer until now (owner's
// request, 2026-07-29): the counters (counter.go) answer "how many
// today?"; neither one answers "did this specific message pass through?" —
// and to know that, the gateway depended on asking the CONSUMER, who might
// be exactly the problem.
//
// 🔴 UNTIL v0.32.0 (T-091) THE PHONE NUMBER AND THE WAMID WERE INDEXED ONLY
// BY HMAC, so nobody could ENUMERATE the numbers that had talked to the
// gateway. On 2026-07-30 the owner reverted that choice, with these words:
// *"you can put the number in, it's not a secret."* Said after the planner
// explained that `zapgw log` (T-093) would show MOVEMENT but not WHO. The
// decision is the owner's, not the planner's — the same discipline as
// CLAUDE.md ("O gateway é DAQUI").
//
// WHAT THE REVERSAL DOESN'T REACH, AND THIS IS A DECISION, NOT AN
// OVERSIGHT: `correlacao` stays HMAC. On OUTBOUND it's the consumer's
// `Idempotency-Key` — free text from an EXTERNAL ORIGIN, which can contain
// anything (an order id, a customer's name). The owner decided about the
// PHONE NUMBER, not about a field whose content the consumer chooses — see
// the comment on TransitRecord.Correlation.
//
// THE GAIN THAT ISN'T OBVIOUS, and which motivated the decision: with
// HMAC, only the EXACT form that generated the hash found the row —
// getting the spelling wrong (with/without "55", with/without the ninth
// digit) returned "nothing found", indistinguishable from "this person
// never sent anything". In the CLEAR, the search matches on the LAST EIGHT
// DIGITS (meta.LastEightDigits) — and that key survives the four
// spellings of the same subscriber, because neither the country code, nor
// the area code, nor the ninth digit are part of it.
//
// A ROW WRITTEN BEFORE T-094 KEEPS contraparte/wamid EMPTY FOREVER: HMAC
// is ONE-WAY, there is no way to recover the phone number from the hash
// T-091 wrote. NOT A BUG — it's the known price of the previous guard (see
// the "transito.contraparte-e-wamid-em-claro" migration in store.go).
// `zapgw transito`/`zapgw log` print "—" on those rows (cmd/zapgw/log.go,
// printLogRows), never an empty value that would look like "no sender".
//
// FIELDS, AND NOTHING ELSE (the list is the guarantee, not loose
// documentation): slug, direcao, contraparte, wamid, tipo, correlacao,
// stamp and desfecho. NEVER text, body, raw, name, caption, filename, or
// the Idempotency-Key in the clear — see transitSchema and the migration
// in store.go, which is where that list becomes impossible to violate by
// accident (a new column would require touching here, and touching here is
// the only place that needs review).
//
// 🔴 `correlacao` IS NOT ALWAYS SAFE TO WRITE IN THE CLEAR, and that's the
// lesson from a review: the first version wrote the RAW `Idempotency-Key`
// there on the OUTBOUND side — free text the CONSUMER chooses, with
// nothing stopping them from putting an order id or a name in it. See the
// comment on TransitRecord.Correlation for the right shape (HMAC on
// outbound, an opaque gateway id on inbound).
package config

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"
)

// DirectionInbound and DirectionOutbound are the only two valid values of the
// `direcao` column — a webhook received from Meta, or a send requested by a consumer.
const (
	DirectionInbound  = "entrada"
	DirectionOutbound = "saida"
)

// ErrInvalidTransitDirection: someone tried to write a direction outside
// the two known ones. It's an error, not a silent write — the same reason
// as ErrUnknownCounterKey (counter.go): a value outside the
// closed vocabulary is exactly the kind of thing nobody reviews afterward.
var ErrInvalidTransitDirection = errors.New("config: direcao de transito invalida (quero entrada ou saida)")

// transitSchema is T-091's migration, FROZEN — like baseSchema
// (store.go), it no longer gets edited: it SHIPPED in v0.32.0 (2026-07-29
// 22:32) and real databases have already run it. A new schema goes in as a
// NEW migration at the end of the list, NEVER here — which is exactly what
// the "transito.contraparte-e-wamid-em-claro" migration (store.go, T-094)
// does: adds `contraparte`/`wamid`, drops `hmac_contraparte`/`hmac_wamid`
// and the old index, and recreates the new index. This block STILL creates
// the HMAC columns for whoever is opening a database FROM SCRATCH — the
// following migration removes them in the same movement, and it's
// redundant on purpose: editing here would rewrite the past of databases
// that already ran this migration (see the "Schema and migrations" header
// in store.go).
//
// NO PRIMARY KEY besides the implicit rowid: unlike the counter (which
// AGGREGATES by slug+dia+key), here each row is a distinct EVENT or
// ATTEMPT, and two identical rows (same instant, same everything) are two
// different facts, not one counted twice.
const transitSchema = `
CREATE TABLE IF NOT EXISTS transito (
  slug             TEXT NOT NULL,
  direcao          TEXT NOT NULL,
  hmac_contraparte TEXT NOT NULL DEFAULT '',
  hmac_wamid       TEXT NOT NULL DEFAULT '',
  tipo             TEXT NOT NULL DEFAULT '',
  correlacao       TEXT NOT NULL DEFAULT '',
  carimbo          INTEGER NOT NULL,
  desfecho         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_transito_busca ON transito(slug, hmac_contraparte, carimbo);
CREATE INDEX IF NOT EXISTS idx_transito_correlacao ON transito(slug, correlacao);
CREATE INDEX IF NOT EXISTS idx_transito_carimbo ON transito(carimbo);
`

// DefaultTransitRetentionDays is how many days this installation keeps
// the transit log when nobody configures anything — the purge deadline
// (PurgeTransit). SAME REASONING as counter retention
// (DefaultRetentionDays): without purging, the table grows forever, and
// here that would be exactly the pile of personal data CLAUDE.md's HARD
// RULE exists to keep from existing. Since T-094 the phone number and the
// wamid go in the CLEAR (owner's decision: "it's not a secret") — the
// purge remains mandatory for the same reason as any third party's
// personal data the gateway retains: retaining it for less time is always
// safer than retaining it longer, whether or not that's the owner's decision.
const DefaultTransitRetentionDays = 30

// TransitRetentionEnvVar is the environment variable that changes the
// deadline above. Documented in docs/IMPLANTACAO.md.
//
// This is the OLD (Portuguese) name. T-214 (2026-08-31) added
// TransitRetentionEnvVarNew as the English pair — this constant stays,
// unchanged and still read, because it is the ONLY name an already-deployed
// /etc/zapgw/env has; see TransitRetentionDays.
const TransitRetentionEnvVar = "ZAPGW_TTL_TRANSITO_DIAS"

// TransitRetentionEnvVarNew is the English name of TransitRetentionEnvVar
// (T-214). The NEW name wins when both are set — see config.EnvOrOld.
const TransitRetentionEnvVarNew = "ZAPGW_TTL_TRANSIT_DAYS"

// TransitRetentionDays resolves the transit log's retention deadline,
// in DAYS, from the environment — the SAME MOLD as
// CounterRetentionDays (counter.go): read ONCE, in `main`, and the
// number flows down as a parameter. An invalid value (non-numeric, zero,
// or negative) falls back to the default, without an error.
//
// T-214: accepts TransitRetentionEnvVarNew in addition to the old name (new
// wins if both are set), and logs once (WarnOldEnvVar) when the value that
// won came from the OLD name.
func TransitRetentionDays(getenv func(string) string) int {
	if getenv == nil {
		return DefaultTransitRetentionDays
	}
	v, oldUsed := EnvOrOld(getenv, TransitRetentionEnvVarNew, TransitRetentionEnvVar)
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		WarnOldEnvVar(oldUsed, TransitRetentionEnvVar, TransitRetentionEnvVarNew)
		return n
	}
	return DefaultTransitRetentionDays
}

// HMACContraparte and HMACWamid EXISTED UNTIL T-094 — removed along with
// the hmac_contraparte/hmac_wamid columns (see this file's header and the
// "transito.contraparte-e-wamid-em-claro" migration in store.go). Two
// representations of the same fact diverge, and that's this project's
// mother-of-all-traps: don't leave the HMAC columns dead alongside the
// clear ones, nor the methods that only served to write to them.

// HMACCorrelation is the SAME mechanism as before, now restricted to
// `correlacao` — the ONLY field that stays HMAC-indexed after T-094, for
// the SEND's `Idempotency-Key`.
//
// 🔴 POST-REVIEW FIX (T-091): the first version wrote the RAW key into the
// `correlacao` column on the OUTBOUND side. That is free text from an
// EXTERNAL ORIGIN — the consumer chooses the content, and nothing
// restricts it. A reasonable consumer uses `pedido-5532999990000` or
// `cliente-joao-silva-0912`: the table would start keeping, for 30 days,
// an arbitrary third-party string — exactly what this file's field list
// forbids ("never... a name"). And the same value was also going out in
// Transit.Record's `log.Printf`, another place, another retention.
// SAME rule as the phone number and the wamid: HMAC, never the value. An
// empty key returns an empty HMAC.
func (s *Store) HMACCorrelation(key string) string {
	if key == "" {
		return ""
	}
	return s.vault.DeterministicHMAC("correlacao:" + key)
}

// TransitRecord is ONE row of the transit log — the fields, and nothing
// else (see this file's header).
type TransitRecord struct {
	Slug      string
	Direction string // DirectionInbound or DirectionOutbound
	// Counterparty is the ALREADY-CANONICAL phone number of whoever wrote
	// (inbound) or was sent to (outbound), in the CLEAR since T-094
	// (owner's decision, 2026-07-30: "you can put the number in, it's not
	// a secret"). "" when the event has no counterpart (an account webhook).
	Counterparty string
	// Wamid gets the SAME treatment, in the CLEAR since T-094 — "" when
	// there is no wamid to tie it to. It carries the recipient's phone
	// number in base64 (docs/ARMADILHAS.md), so writing the wamid in the
	// clear is already a consequence of the SAME decision that took the
	// phone number out of the Counterparty column's HMAC.
	Wamid string
	Type  string // the event/request's type; "" when the batch carried no modeled event
	// Correlation has TWO SHAPES, and the difference is who chooses the
	// value:
	//   - ON INBOUND: the OPAQUE id the gateway itself generates
	//     (internal/inbound/handler.go, proximaCorrelacao) — never seen by
	//     anyone outside before it exists, so writing it in the clear
	//     leaks nothing; it's the same value that goes into the journal
	//     alongside the verdict, and serves to cross-reference the TWO
	//     sources (the journal and the transit log) of the SAME event.
	//   - ON OUTBOUND: Store.HMACCorrelation(idempotencyKey) — the
	//     Idempotency-Key comes from the CONSUMER, and they can put any
	//     free text there (an order id, a customer's name). Writing the
	//     raw value would turn this field into exactly the leak T-094 took
	//     out of the Counterparty column but did NOT take out of here — the
	//     owner decided about the PHONE NUMBER, not about a free-content
	//     field chosen by the consumer. The HMAC preserves the
	//     cross-referencing property: when the consumer says "my key was
	//     X", the investigator computes HMACCorrelation(X) and finds the SAME row.
	Correlation string
	Outcome     string // the status to the consumer/to Meta, or a short reason
}

// WriteTransit inserts ONE row of the transit log.
//
// Returns a REAL error because its caller (Transit, further below) needs
// to know if there is something to log — the SAME division of
// responsibility as IncrementCounter vs. Counter (counter.go).
func (s *Store) WriteTransit(r TransitRecord, when time.Time) error {
	if r.Direction != DirectionInbound && r.Direction != DirectionOutbound {
		return fmt.Errorf("%w: %q", ErrInvalidTransitDirection, r.Direction)
	}
	_, err := s.db.Exec(`
		INSERT INTO transito (slug, direcao, contraparte, wamid, tipo, correlacao, carimbo, desfecho)
		VALUES (?,?,?,?,?,?,?,?)`,
		r.Slug, r.Direction, r.Counterparty, r.Wamid, r.Type, r.Correlation, when.Unix(), r.Outcome)
	if err != nil {
		return fmt.Errorf("config: gravar transito: %w", err)
	}
	return nil
}

// PurgeTransit deletes rows older than `before` and returns how many.
//
// SAME REASONING as PurgeIdempotency and PurgeCounters: without
// purging, the table grows forever — and here that would be exactly the
// pile of personal data CLAUDE.md's hard rule exists to keep from existing.
func (s *Store) PurgeTransit(before time.Time) (int, error) {
	res, err := s.db.Exec(`DELETE FROM transito WHERE carimbo < ?`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("config: purgar transito: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("config: linhas purgadas de transito: %w", err)
	}
	return int(n), nil
}

// TransitLine is what `zapgw transito` shows — the search already
// requires a number or a key IN HAND from whoever asks
// (cmd/zapgw/transit.go), so echoing the counterpart back adds no
// information; `zapgw log` (LogLine, further below) is what shows the
// counterpart to whoever has NO number in hand at all.
type TransitLine struct {
	Stamp     time.Time
	Direction string
	// Wamid is Meta's `wa_message_id`, in the CLEAR since T-094 — "" when
	// there is no wamid to tie it to (inbound, a send that failed before
	// Meta responded, or a row written BEFORE T-094). It's the missing
	// piece for the two `ALARME ... PRECISA DE GENTE` in
	// internal/outbound/handler.go (T-128): they tell the operator to
	// write down the wa_message_id by hand without saying where to get it
	// from, and this field is where. cmd/zapgw/transit.go prints "—" for
	// empty, the SAME treatment cmd/zapgw/log.go already gives an empty
	// Counterparty, and for the same reason: empty looks like "a field that
	// doesn't exist", the dash says "there wasn't one".
	//
	// 🔴 The wamid carries the recipient's phone number inside it
	// (docs/ARMADILHAS.md). This is already accepted ON THIS SCREEN — the
	// phone number goes in the clear in the --telefone search since T-094,
	// owner's decision — but do NOT leak this field into any OTHER output
	// (error log, contract, etc.).
	Wamid   string
	Type    string
	Outcome string
}

// SearchTransit returns an instance's rows whose `contraparte`'s LAST
// EIGHT DIGITS match the requested ones (meta.LastEightDigits), from
// the MOST RECENT to the OLDEST, with a stamp >= `desde`.
//
// LAST EIGHT DIGITS, NOT EQUALITY — owner's decision, T-094: it's what
// makes the FOUR spellings of the same subscriber (with/without "55",
// with/without the ninth digit) find the SAME row, because none of them
// falls within the last eight. The column stores the CANONICAL form
// (WriteTransit), and the index (idx_transito_busca, store.go) is on the
// EXPRESSION substr(contraparte, -8), not on the whole column.
//
// AN EMPTY `lastEight` NEVER MATCHES: a number with fewer than eight
// digits returns "" from meta.LastEightDigits, and a `contraparte = ”`
// (an ACCOUNT webhook, with no counterpart at all) would also produce
// substr(...) = "" — without this guard, searching for an invalid number
// would return every account webhook of the instance as if they were "matches".
func (s *Store) SearchTransit(slug, lastEight string, since time.Time) ([]TransitLine, error) {
	if lastEight == "" {
		return nil, nil
	}
	return s.queryTransit(
		`SELECT carimbo, direcao, wamid, tipo, desfecho FROM transito
		  WHERE slug = ? AND substr(contraparte, -8) = ? AND carimbo >= ?
		  ORDER BY carimbo DESC`,
		slug, lastEight, since.Unix())
}

// NumbersForLastEight returns the DISTINCT numbers (in the CLEAR, the
// CANONICAL form WriteTransit writes) whose LAST EIGHT DIGITS match
// `lastEight` — across ANY instance, because `zapgw log clear --telefone`
// (T-096) deletes across ALL of them.
//
// 🔴 EXISTS FOR THE AMBIGUITY GUARD, and that's why this function exists
// separate from SearchTransit: searching by the last eight digits (T-094)
// is CONVENIENT for ASKING "did this message pass through?", because two
// subscribers rarely share the same final eight digits — but rarely isn't
// never. `5511999990000` and `5532999990000` have the SAME last eight
// (only the country+area code changes, and it falls OUTSIDE the last
// eight). For SEARCHING that's convenience; for DELETING it's deleting
// someone else's history. The caller (logClearCommand, cmd/zapgw/log.go)
// uses this to COUNT how many distinct numbers matched BEFORE deciding
// whether to delete — more than one, it refuses.
func (s *Store) NumbersForLastEight(lastEight string) ([]string, error) {
	if lastEight == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT DISTINCT contraparte FROM transito
		 WHERE substr(contraparte, -8) = ? AND contraparte != ''
		 ORDER BY contraparte`, lastEight)
	if err != nil {
		return nil, fmt.Errorf("config: buscar numeros por ultimos oito digitos: %w", err)
	}
	defer rows.Close()

	var numbers []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("config: ler numero por ultimos oito digitos: %w", err)
		}
		numbers = append(numbers, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar numeros por ultimos oito digitos: %w", err)
	}
	return numbers, nil
}

// ClearInstanceTransit deletes the ENTIRE transit log of ONE instance
// — returns how many rows came out. It's `zapgw log clear --instancia` (T-096).
//
// DIFFERENT from RemoveInstance (store.go): this function does NOT
// delete the instance nor any other table — only the transit log, and
// only for that slug. Whoever wants to delete the whole instance uses
// `instancia remover`.
func (s *Store) ClearInstanceTransit(slug string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM transito WHERE slug = ?`, slug)
	if err != nil {
		return 0, fmt.Errorf("config: limpar transito da instancia %q: %w", slug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("config: linhas apagadas de transito da instancia %q: %w", slug, err)
	}
	return n, nil
}

// TransitRowsDeleted is how many transit rows a by-phone-number
// cleanup (T-096) deleted from ONE instance. SAME reasoning as
// RowsDeleted (store.go, RemoveInstance): the total alone would hide
// whether the cleanup had reached the wrong instance, and deleting without
// saying WHERE is deleting silently.
type TransitRowsDeleted struct {
	Slug string
	Rows int64
}

// ClearTransitByPhone deletes every transit row, across ALL
// instances, whose `contraparte` is EXACTLY `number` — the COMPLETE form
// already resolved by NumbersForLastEight. The CALLER is who guarantees
// only ONE candidate number exists (the ambiguity guard lives in
// logClearCommand, cmd/zapgw/log.go): this function never decides on its
// own what to do with more than one number, it only deletes what it received.
//
// COUNTS BEFORE DELETING, in the SAME transaction: counting afterward
// would count zero, and the per-instance count is the only proof, on
// screen, that the right thing was deleted (same reasoning as RemoveInstance).
func (s *Store) ClearTransitByPhone(number string) ([]TransitRowsDeleted, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("config: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`
		SELECT slug, COUNT(*) FROM transito WHERE contraparte = ? GROUP BY slug ORDER BY slug`, number)
	if err != nil {
		return nil, fmt.Errorf("config: contar transito do telefone antes de apagar: %w", err)
	}
	var deleted []TransitRowsDeleted
	for rows.Next() {
		var a TransitRowsDeleted
		if err := rows.Scan(&a.Slug, &a.Rows); err != nil {
			rows.Close()
			return nil, fmt.Errorf("config: ler contagem de transito do telefone: %w", err)
		}
		deleted = append(deleted, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("config: iterar contagem de transito do telefone: %w", err)
	}
	rows.Close()

	if _, err := tx.Exec(`DELETE FROM transito WHERE contraparte = ?`, number); err != nil {
		return nil, fmt.Errorf("config: apagar transito do telefone: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("config: commit da limpeza por telefone: %w", err)
	}
	return deleted, nil
}

// HighestLogRowid returns the highest ROWID currently in the `transito`
// table — across ALL instances if `slug` is empty, or just the instance's
// if not — and 0 if there is no row at all.
//
// 🔴 EXISTS SO `zapgw log`'s FOLLOW SURVIVES `log clear` (T-096): the
// `transito` table doesn't declare AUTOINCREMENT (see transitSchema,
// above) and that's why SQLite REUSES a rowid when the row with the
// highest rowid is deleted — the next row written is born with rowid =
// MAX(remaining rowid)+1, which can be LESS THAN OR EQUAL TO the cursor
// the follow had already seen (if the table became entirely empty, the
// next INSERT gets rowid 1). Without this, `LogLinesSince(slug,
// cursor)` — which filters `rowid > cursor` — would never find the new row
// again, and the follow would go BLIND FOREVER, silently. The caller
// (runLog, cmd/zapgw/log.go) uses this method to ROLL BACK the cursor
// when it becomes greater than the highest rowid that actually exists.
func (s *Store) HighestLogRowid(slug string) (int64, error) {
	var highest sql.NullInt64
	var err error
	if slug == "" {
		err = s.db.QueryRow(`SELECT MAX(rowid) FROM transito`).Scan(&highest)
	} else {
		err = s.db.QueryRow(`SELECT MAX(rowid) FROM transito WHERE slug = ?`, slug).Scan(&highest)
	}
	if err != nil {
		return 0, fmt.Errorf("config: maior rowid de transito: %w", err)
	}
	if !highest.Valid {
		return 0, nil
	}
	return highest.Int64, nil
}

// SearchTransitByCorrelation is SearchTransit's counterpart for the
// SEND's `Idempotency-Key`: `hmacCorrelation` is Store.HMACCorrelation(key),
// and the search is by direct EQUALITY on the `correlacao` column — which
// on OUTBOUND already stores the HMAC, never the key in the clear (see the
// comment on TransitRecord.Correlation).
//
// ONLY FINDS OUTBOUND ROWS: an INBOUND correlation is the gateway's opaque
// id, not an HMAC, so it never accidentally matches HMACCorrelation(algo) —
// the two value spaces don't cross in practice (one is
// `<nanotime>-<seq>` in base36, the other is hex of SHA-256), and even if
// they collided one day, showing an inbound row here leaks nothing (that
// field is already safe to show in the clear).
func (s *Store) SearchTransitByCorrelation(slug, hmacCorrelation string, since time.Time) ([]TransitLine, error) {
	return s.queryTransit(
		`SELECT carimbo, direcao, wamid, tipo, desfecho FROM transito
		  WHERE slug = ? AND correlacao = ? AND carimbo >= ?
		  ORDER BY carimbo DESC`,
		slug, hmacCorrelation, since.Unix())
}

// queryTransit is the SHARED read between SearchTransit and
// SearchTransitByCorrelation — SAME reason as readSummary (store.go): both
// searches read the SAME columns in the SAME order, and two copies would
// diverge on the first schema change.
func (s *Store) queryTransit(query string, args ...any) ([]TransitLine, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("config: buscar transito: %w", err)
	}
	defer rows.Close()

	var found []TransitLine
	for rows.Next() {
		var stamp int64
		var l TransitLine
		if err := rows.Scan(&stamp, &l.Direction, &l.Wamid, &l.Type, &l.Outcome); err != nil {
			return nil, fmt.Errorf("config: ler linha de transito: %w", err)
		}
		l.Stamp = time.Unix(stamp, 0).UTC()
		found = append(found, l)
	}
	// rows.Err() is NOT optional (docs/ARMADILHAS.md, "Meta"): an error
	// midway through iteration would end the loop as if the data had run
	// out, and a search shorter than reality is this project's most
	// expensive failure shape.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar transito: %w", err)
	}
	return found, nil
}

// LogLine is ONE line of what `zapgw log` shows (T-093) — the sibling
// of TransitLine that carries the SLUG (the log crosses instances, the
// search by phone/key is always within ONE) and the ROWID, which is the
// follow's CURSOR.
//
// 🔴 THE ROWID, NEVER THE STAMP: `stamp` is a whole second, and two rows
// in the same second (a burst — the counters show it happens) would make a
// `stamp > ultimo` filter silently lose one of the two, which is the
// worst possible defect in a log. `rowid` is SQLite's implicit
// identifier, monotonic per INSERT, and the `transito` table declares no
// PRIMARY KEY of its own (see the comment on transitSchema) precisely so
// it can serve this purpose.
type LogLine struct {
	Rowid     int64
	Stamp     time.Time
	Slug      string
	Direction string
	// Counterparty is the phone number (T-094, in the CLEAR) — "" on a row
	// written BEFORE the migration (HMAC is one-way, there is no way to
	// recover it) or on an ACCOUNT webhook, which has no counterpart at
	// all. cmd/zapgw/log.go prints "—" in both cases, never an empty value
	// that would look like "no sender".
	Counterparty string
	Type         string
	Outcome      string
}

// LastLogLines returns the LAST `n` transit rows — across ALL
// instances if `slug` is empty, or just the instance's if not — in
// CHRONOLOGICAL order (the oldest first, to read like a log). The ROWID of
// the last row returned (or 0 if there is none) is the cursor the caller
// starts the follow with, without skipping or reprinting the same row.
func (s *Store) LastLogLines(slug string, n int) ([]LogLine, error) {
	var (
		rows []LogLine
		err  error
	)
	if slug == "" {
		rows, err = s.queryLog(`
			SELECT rowid, carimbo, slug, direcao, contraparte, tipo, desfecho FROM transito
			ORDER BY rowid DESC LIMIT ?`, n)
	} else {
		rows, err = s.queryLog(`
			SELECT rowid, carimbo, slug, direcao, contraparte, tipo, desfecho FROM transito
			WHERE slug = ?
			ORDER BY rowid DESC LIMIT ?`, slug, n)
	}
	if err != nil {
		return nil, err
	}
	// the query came back in DESCENDING order (that's what makes LIMIT
	// grab the LAST n); a log is read from oldest to newest, so it's
	// reversed here, a single time, instead of asking the caller to
	// remember to do it.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows, nil
}

// LogLinesSince returns every transit row with a ROWID greater than
// `rowid` — across ALL instances if `slug` is empty, or just the
// instance's if not —, in ASCENDING order. It's the query `zapgw log`'s
// follow repeats on every turn of the loop (T-093): a SHORT read loop,
// with no open transaction, safe against the service because the database
// is in WAL mode (OpenStore, `journal_mode(WAL)`) — the same reasoning
// that already lets `zapgw estado` read concurrently with the writing process.
func (s *Store) LogLinesSince(slug string, rowid int64) ([]LogLine, error) {
	if slug == "" {
		return s.queryLog(`
			SELECT rowid, carimbo, slug, direcao, contraparte, tipo, desfecho FROM transito
			WHERE rowid > ?
			ORDER BY rowid ASC`, rowid)
	}
	return s.queryLog(`
		SELECT rowid, carimbo, slug, direcao, contraparte, tipo, desfecho FROM transito
		WHERE slug = ? AND rowid > ?
		ORDER BY rowid ASC`, slug, rowid)
}

// queryLog is the SHARED read between LastLogLines and
// LogLinesSince — SAME reason as queryTransit, right above: both
// read the SAME columns in the SAME order, and two copies would diverge on
// the first schema change.
func (s *Store) queryLog(query string, args ...any) ([]LogLine, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("config: buscar log de transito: %w", err)
	}
	defer rows.Close()

	var found []LogLine
	for rows.Next() {
		var stamp int64
		var l LogLine
		if err := rows.Scan(&l.Rowid, &stamp, &l.Slug, &l.Direction, &l.Counterparty, &l.Type, &l.Outcome); err != nil {
			return nil, fmt.Errorf("config: ler linha de log: %w", err)
		}
		l.Stamp = time.Unix(stamp, 0).UTC()
		found = append(found, l)
	}
	// rows.Err() is NOT optional (docs/ARMADILHAS.md, "Meta"): see
	// queryTransit, above, for the reason.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar log de transito: %w", err)
	}
	return found, nil
}

// TransitStore is what Transit needs from the Store. EXPORTED on
// purpose: tests in OTHER packages (internal/inbound, internal/outbound)
// use an implementation that ALWAYS fails to prove, on the real HTTP
// handler, that Record never propagates that error into the response
// already written to Meta or the consumer (T-091, Verify (c)) — the SAME
// technique as CounterStore (counter.go).
type TransitStore interface {
	WriteTransit(r TransitRecord, when time.Time) error
}

// Transit is the WRITER of the transit log.
//
// The SAME guarantee as Counter (counter.go), and for the SAME reason:
// Record RETURNS NOTHING, because the "write failure only logs"
// guarantee has to live in the method's SIGNATURE, not in the discipline
// of whoever writes each handler.go. A method that CAN return an error
// invites the caller to, one day, treat that error as fatal — exactly the
// outcome this subsystem exists to prevent (docs/ARMADILHAS.md, "Erros e log").
type Transit struct {
	store TransitStore
}

// NewTransit wraps the store with the guarantee above. It is the
// PRODUCTION constructor.
func NewTransit(store *Store) *Transit {
	return &Transit{store: store}
}

// NewTransitWithStore is like NewTransit, but accepts any TransitStore
// — exists for testing, the same reason as NewCounterWithStore.
func NewTransitWithStore(store TransitStore) *Transit {
	return &Transit{store: store}
}

// Record writes a transit log row.
//
// Calling this is always safe AFTER w.WriteHeader: even if the write fails
// (database down, disk full, an invalid direction from a programming
// mistake), nothing here returns an error or can change what was already
// answered to Meta or the consumer — only a log line is left behind.
func (t *Transit) Record(r TransitRecord) {
	if err := t.store.WriteTransit(r, time.Now()); err != nil {
		log.Printf("zapgw: falha ao gravar log de transito (slug=%q direcao=%q correlacao=%q): %v",
			r.Slug, r.Direction, r.Correlation, err)
	}
}
