// What the gateway knows about the NUMBER at Meta: quality and messaging
// limit (T-080).
//
// # TWO SOURCES, AND THIS FILE IS THE ONLY PLACE THAT DECIDES WHO WINS
//
// The same data arrives through two paths, and both are legitimate:
//
//   - MEASUREMENT — the watcher (internal/outbound/vigia.go) already hits
//     `GET /{phone-number-id}` per ACTIVE instance on every tick; it now also
//     reads `quality_rating` and `whatsapp_business_manager_messaging_limit`
//     (meta.ObserveNumber). It is the only source of QUALITY.
//   - WEBHOOK — `phone_number_quality_update` (modeled in T-058) arrives
//     PUSHED when the limit changes, and carries `current_limit`. It is the
//     only source that is FRESH at the instant of the change, without us
//     asking.
//
// Together they give freshness without aggressive polling: the measurement
// guarantees a freshly created instance has a value even if nothing ever
// changes, and the webhook guarantees a change shows up in seconds instead
// of up to one tick.
//
// # THE TIEBREAK RULE, WRITTEN IN CODE AND NOT IN ANYONE'S HEAD
//
// **The MOST RECENT observation wins, whatever the source.** There is no
// source priority, and the absence is deliberate: "webhook always wins"
// would let a Meta redelivery (it redelivers for up to 36h) roll back a
// value that has already been measured since; "measurement always wins"
// would throw away exactly the pushed alert, which is the only one that
// arrives before it hurts.
//
// **The stamp compared is OUR clock — the instant the gateway learned it
// —, never Meta's `entry.time`.** This is a choice, and the reason is that
// the alternative compares two clocks nobody synchronized: a drift of
// minutes between Meta's clock and the CT's would silently decide which
// source wins. "When the gateway learned it" is also exactly the question
// the consumer needs to answer to judge freshness, and it's the SAME
// definition `certificado_do_callback.observado_em` already uses.
//
// *What this choice accepts, and it's written down so it isn't discovered
// as a surprise: a Meta redelivery of an OLD event arrives "now" by our
// clock and can reassert a limit that has already been superseded. In
// practice the redelivery only happens when we DIDN'T respond 200, and a
// redelivery of the same event carries the same value — the bad case
// requires a real change within the redelivery window.*
//
// # THE TWO STAMPS, THE SAME DISCIPLINE AS token_meta AND THE CERTIFICATE
//
// `observado_em` (per value) says when that value arrived; `conferido_em`
// says when the gateway last TRIED to MEASURE. The DIVERGENCE between the
// two is the signal: `conferido_em` moving while `observado_em` stays put
// means the measurement is going back and forth without the field — and the
// consumer sees this without knowing anything about our implementation.
//
// **ONLY MEASUREMENT stamps `conferido_em`.** A webhook is not a check: we
// didn't ask anything, it just arrived. Letting the webhook push that stamp
// would make "the measurement is healthy" show green on a gateway that has
// lost read access to the Graph API.
package config

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// The two possible sources of a value. They travel ALL THE WAY TO THE
// CONSUMER (the state's `numero_na_meta` block publishes `fonte` alongside
// each value), and that's why they are stable words, not internal detail:
// without them, a consumer seeing two disagreeing values between two reads
// has no way to know whether the gateway measured it or Meta announced it.
const (
	// SourceMeasurement: the gateway asked the Graph API (the watcher).
	SourceMeasurement = "medicao"
	// SourceWebhook: Meta pushed it (`phone_number_quality_update`).
	SourceWebhook = "webhook"
)

// NumberValue is ONE observed value, with where it came from and when.
//
// THE THREE FIELDS GO TOGETHER OR THEY'RE WORTHLESS, for the same reason as
// CertificateObservation: "TIER_250" alone doesn't say whether it's
// current information or three weeks old, and doesn't say whether it was
// measured or pushed. Whoever reads it decides with all three in hand.
type NumberValue struct {
	// Value is Meta's literal ("GREEN", "TIER_250"). "" is "never observed" —
	// there is no path that writes an empty value (see UpdateNumberAtMeta).
	Value string
	// ObservedAt is when the GATEWAY learned this value. See the header for
	// why it isn't Meta's stamp.
	ObservedAt time.Time
	// Source is SourceMeasurement or SourceWebhook.
	Source string
}

// Observed tells whether there is a value. `false` is "never observed",
// which is legitimate information — not an error.
func (v NumberValue) Observed() bool { return v.Value != "" }

// NumberAtMeta is everything the gateway knows about ONE instance's number.
type NumberAtMeta struct {
	// Quality comes ONLY from SourceMeasurement. The
	// `phone_number_quality_update` webhook does NOT carry a quality
	// rating: it carries an `event` (ONBOARDING/FLAGGED/UNFLAGGED), which
	// is a different fact — and inventing that "FLAGGED" equals some
	// rating would be asserting a translation that Meta's source doesn't
	// support. So there is no possible conflict here.
	Quality NumberValue
	// Limit comes from BOTH sources, and it's what the header's tiebreak
	// rule is about.
	Limit NumberValue
	// CheckedAt is the last measurement ATTEMPT — zero when there never
	// was one.
	CheckedAt time.Time
}

// NumberUpdate is ONE update coming from ONE source.
//
// An empty field means "this source doesn't speak to this" and does NOT
// erase what was already there: the webhook doesn't speak to quality, and a
// measurement where Meta omitted one of the fields cannot zero out that
// field's previous observation.
type NumberUpdate struct {
	Quality string
	Limit   string
	// Source is SourceMeasurement or SourceWebhook. Only SourceMeasurement stamps
	// `conferido_em` — see the header.
	Source string
	// When is the instant on OUR clock when the gateway learned this.
	When time.Time
}

// ErrUnknownNumberSource: someone called with a source that doesn't exist.
//
// REFUSE, DON'T WRITE IT ANYWAY: the source travels to the consumer. A
// typo'd string written here would show up on their dashboard as if it were
// our vocabulary, and no one would know where it came from.
var ErrUnknownNumberSource = errors.New("config: fonte de observacao do numero desconhecida")

// UpdateNumberAtMeta applies ONE update.
//
// THE FRESHNESS COMPARISON IS DONE IN SQL, in a single UPSERT, and that's a
// decision: a read-decide-write in Go would lose the race between the
// server (which measures and receives webhooks) and the `zapgw estado`
// process (which also measures), because they are DIFFERENT processes and
// one's mutex doesn't reach the other.
//
// COMPARING THE STAMP AS TEXT IS SAFE HERE, and only here: every write goes
// through stampOf, which produces RFC3339 in UTC, always the same width
// and always with `Z` — in that shape, lexicographic order IS chronological
// order. The column with the EMPTY string (the initial state) sorts before
// any date, so the first observation always goes in.
func (s *Store) UpdateNumberAtMeta(slug string, a NumberUpdate) error {
	if a.Source != SourceMeasurement && a.Source != SourceWebhook {
		return fmt.Errorf("%w: %q", ErrUnknownNumberSource, a.Source)
	}
	when := stampOf(a.When)
	// Only measurement stamps `conferido_em`. See the header.
	checked := ""
	if a.Source == SourceMeasurement {
		checked = when
	}
	// The stamp for an absent value stays empty so the CASE below (`<> ''`)
	// discards it by VALUE, never by stamp — so "the source didn't speak
	// to this" and "the source spoke and it was empty" don't need two
	// separate paths.
	stampIfAny := func(value string) string {
		if value == "" {
			return ""
		}
		return when
	}

	_, err := s.db.Exec(`
		INSERT INTO numero_na_meta
		  (slug, qualidade, qualidade_em, qualidade_fonte, limite, limite_em, limite_fonte, conferido_em)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (slug) DO UPDATE SET
		  qualidade = CASE WHEN excluded.qualidade <> ''
		                    AND excluded.qualidade_em >= numero_na_meta.qualidade_em
		                   THEN excluded.qualidade ELSE numero_na_meta.qualidade END,
		  qualidade_em = CASE WHEN excluded.qualidade <> ''
		                       AND excluded.qualidade_em >= numero_na_meta.qualidade_em
		                      THEN excluded.qualidade_em ELSE numero_na_meta.qualidade_em END,
		  qualidade_fonte = CASE WHEN excluded.qualidade <> ''
		                          AND excluded.qualidade_em >= numero_na_meta.qualidade_em
		                         THEN excluded.qualidade_fonte ELSE numero_na_meta.qualidade_fonte END,
		  limite = CASE WHEN excluded.limite <> ''
		                 AND excluded.limite_em >= numero_na_meta.limite_em
		                THEN excluded.limite ELSE numero_na_meta.limite END,
		  limite_em = CASE WHEN excluded.limite <> ''
		                    AND excluded.limite_em >= numero_na_meta.limite_em
		                   THEN excluded.limite_em ELSE numero_na_meta.limite_em END,
		  limite_fonte = CASE WHEN excluded.limite <> ''
		                       AND excluded.limite_em >= numero_na_meta.limite_em
		                      THEN excluded.limite_fonte ELSE numero_na_meta.limite_fonte END,
		  conferido_em = CASE WHEN excluded.conferido_em <> ''
		                       AND excluded.conferido_em >= numero_na_meta.conferido_em
		                      THEN excluded.conferido_em ELSE numero_na_meta.conferido_em END`,
		slug,
		a.Quality, stampIfAny(a.Quality), sourceIfAny(a.Quality, a.Source),
		a.Limit, stampIfAny(a.Limit), sourceIfAny(a.Limit, a.Source),
		checked)
	if err != nil {
		return fmt.Errorf("config: atualizar o numero na meta: %w", err)
	}
	return nil
}

func sourceIfAny(value, source string) string {
	if value == "" {
		return ""
	}
	return source
}

// NumberAtMeta reads what the gateway knows about an instance's number. An
// instance with no row gets back everything zeroed and a nil error —
// "never observed" is an answer, not a failure.
//
// A stamp that fails to decode IS AN ERROR, never a zeroed observation —
// the SAME decision (and the same reason) as CallbackCertificate: the
// column is written by one function, in one format, so an unreadable value
// there means someone edited the database by hand. Returning "never
// observed" in that case would turn a tampered database into plausible
// silence.
func (s *Store) NumberAtMeta(slug string) (NumberAtMeta, error) {
	var quality, qualityAt, qualitySource, limit, limitAt, limitSource, checked string
	err := s.db.QueryRow(`
		SELECT qualidade, qualidade_em, qualidade_fonte, limite, limite_em, limite_fonte, conferido_em
		  FROM numero_na_meta WHERE slug = ?`, slug,
	).Scan(&quality, &qualityAt, &qualitySource, &limit, &limitAt, &limitSource, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return NumberAtMeta{}, nil
	}
	if err != nil {
		return NumberAtMeta{}, fmt.Errorf("config: ler o numero na meta: %w", err)
	}

	n := NumberAtMeta{
		Quality: NumberValue{Value: quality, Source: qualitySource},
		Limit:   NumberValue{Value: limit, Source: limitSource},
	}
	if n.Quality.ObservedAt, err = parseStamp(qualityAt, "qualidade_em", slug); err != nil {
		return NumberAtMeta{}, err
	}
	if n.Limit.ObservedAt, err = parseStamp(limitAt, "limite_em", slug); err != nil {
		return NumberAtMeta{}, err
	}
	if n.CheckedAt, err = parseStamp(checked, "conferido_em", slug); err != nil {
		return NumberAtMeta{}, err
	}
	return n, nil
}

// parseStamp converts the column to an instant. An EMPTY column is
// "never", not an error: it's the initial state of every row (and the
// CREATE TABLE's DEFAULT).
func parseStamp(raw, column, slug string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"config: %s do numero na meta (slug=%q) nao e RFC3339: %w", column, slug, err)
	}
	return t.UTC(), nil
}

// NumberObserverStore is what NumberObserver needs from the Store.
// EXPORTED for the same reason as ObserverStore and CounterStore: a test
// needs an implementation that ALWAYS fails, to prove that a write failure
// doesn't change any outcome.
type NumberObserverStore interface {
	UpdateNumberAtMeta(slug string, a NumberUpdate) error
}

// NumberObserver is the WRITER of number observations — the same design
// (and the same two reasons) as CertificateObserver and Counter.
//
// The MUTEX serializes the write within this process: two paths write here
// (the watcher, on a timer goroutine, and the webhook handler, on a
// per-request goroutine), and this project has already paid a Critical for
// shared mutable state (docs/ARMADILHAS.md, "Go / concorrência"). *It
// doesn't reach the OTHER process (`zapgw estado` also measures) — what
// guarantees order between processes is the UPSERT with stamp comparison,
// not this lock.*
//
// REGISTRAR RETURNS NOTHING, and the guarantee lives in the SIGNATURE: both
// callers are on paths that CANNOT fail because of tracking — the watcher
// (which never brings anything down) and the webhook handler AFTER the
// response to Meta has already been written. A method that CAN return an
// error invites someone, one day, to treat that error as fatal
// (docs/ARMADILHAS.md).
type NumberObserver struct {
	mu    sync.Mutex
	store NumberObserverStore
}

// NewNumberObserver is the PRODUCTION constructor.
func NewNumberObserver(store *Store) *NumberObserver {
	return &NumberObserver{store: store}
}

// NewNumberObserverWithStore accepts any NumberObserverStore. It
// exists for testing — including to prove that a write that ALWAYS fails
// doesn't change any outcome.
func NewNumberObserverWithStore(store NumberObserverStore) *NumberObserver {
	return &NumberObserver{store: store}
}

// Record applies an update.
//
// NIL RECEIVER IS A DELIBERATE NO-OP, as in CertificateObserver: tests
// that have nothing to do with this pass nil, and none of them can panic
// because of a tracking subsystem. In production, a missing observer
// doesn't produce a WRONG result — it produces `nunca_observado` in the
// state, which is a NAMED and visible state.
func (o *NumberObserver) Record(slug string, a NumberUpdate) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.store.UpdateNumberAtMeta(slug, a); err != nil {
		log.Printf("zapgw: falha ao gravar a observacao do numero (slug=%q, fonte=%q): %v", slug, a.Source, err)
	}
}
