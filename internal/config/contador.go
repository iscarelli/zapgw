// Instance counters — the definitive-loss alarm (T-035) stops being just a
// log line, and the count is tracking, NOT the product.
//
// THE CRITERION THAT DECIDES ANY DESIGN DOUBT HERE: a counter that brings
// delivery down is infinitely worse than no counter at all. The mirror
// rule (internal/inbound/mirror.go) requires responding to Meta fast and
// correctly; nothing in this file can delay or change that response. Hence:
//
//   - Contar() is meant to be called AFTER w.WriteHeader, never before;
//   - a write failure only LOGS and moves on — and that guarantee lives in
//     the method's SIGNATURE (Record returns nothing), not in the
//     discipline of whoever calls it. A method that CAN return an error
//     invites the caller to, one day, treat that error as fatal — exactly
//     the outcome this subsystem exists to prevent. If the right answer is
//     always "ignore", the method's signature guarantees that; discipline
//     does not.
package config

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"
)

// The CLOSED vocabulary of counter keys (T-035, Do item 1). Don't invent a
// metric nobody is going to look at: start from this set and only add when
// someone will actually look at the new number.
const (
	CounterReceived            = "recebidas"
	CounterDelivered           = "entregues"
	CounterRefusedByConsumer   = "recusadas_pelo_consumidor"
	CounterDefinitiveLossAlarm = "alarme_perda_definitiva"
	CounterSent                = "enviadas"
	CounterSendFailures        = "falhas_de_envio"

	// CounterAccountDiscarded (T-038, 2026-07-26): an ACCOUNT webhook (template
	// status, number quality, alerts — fields Meta does NOT let you point
	// to a per-instance URL, see internal/inbound/handler.go) arrived with
	// a waba_id (entry[].id) that is NOT this instance's. The handler
	// discards the batch and responds 200 — never delivers "to whoever
	// shows up first" — and this counter is the only trace left behind.
	CounterAccountDiscarded = "conta_descartada"

	// CounterNumberDiscarded (T-047, 2026-07-26): a MESSAGE/STATUS webhook
	// arrived with a metadata.phone_number_id that is NOT this instance's
	// (internal/inbound/handler.go, step 5a). Same outcome as its sibling
	// above — batch discarded, 200 to Meta, ALARM in the log.
	//
	// WHY A NEW KEY, AND NOT REUSING CounterAccountDiscarded. Both count
	// "tenant-isolation refusal", and the temptation to sum everything into
	// one number is real. Three reasons, in order of weight:
	//
	//  1. THEY ARE DIFFERENT GUARDS, ON DIFFERENT WEBHOOK FORMATS, WITH
	//     DIFFERENT FIXES. `conta_descartada` says "the WABA in the body
	//     isn't this instance's" — the investigator will look at the WABA's
	//     registration and the App. `numero_descartado` says "a phone
	//     number that isn't this instance's arrived at this slug" — the
	//     investigator will look at the Callback URL, the webhook override,
	//     and the registered phone_number_id. A number that SUMS the two
	//     sends the person to check both places, every time.
	//  2. "WHICH GUARD REFUSED?" IS THE FIRST QUESTION of whoever opens
	//     `zapgw estado` and sees an isolation refusal. A summed key
	//     doesn't answer; it forces a trip to the journal — which is
	//     exactly the trace this counter exists to not be the only one of
	//     (docs/ARMADILHAS.md, "ninguém lê journal por hábito").
	//  3. THE NAME WOULD LIE. `conta_descartada` has, right above, a
	//     comment describing ACCOUNT webhooks; stuffing number-based
	//     refusals in there would force loosening the comment to fit both,
	//     and a comment loosened to fit is the start of a doc that lies.
	//
	// And the cost of not summing is low: whoever wants the total sums the
	// two columns on screen; whoever only has the total CANNOT split it back apart.
	CounterNumberDiscarded = "numero_descartado"

	// CounterReadsMarked and CounterReadFailures (T-075, 2026-07-28):
	// POST /v1/leituras marked (or failed to mark) a RECEIVED message as
	// read on Meta — the blue check in the direction that was missing.
	//
	// 🔴 WHY THIS ISN'T CounterSent, AND THIS IS T-075'S MOST IMPORTANT
	// POINT. Marking as read produces NO conversation: no message is born,
	// there is no new recipient, Meta doesn't return a wa_message_id, and
	// there is no billing. Adding this into `enviadas` would inflate the
	// number BOTH consumers use to project cost (consumer-b's Consumption
	// panel multiplies volume by the rate card's tariffs), and T-063 —
	// which swaps projection for measurement — would be born on top of an
	// already-lying number. An operator who opens the same conversation ten
	// times a day would add up ten "sends" that never happened.
	//
	// The same criterion as the CounterNumberDiscarded/CounterAccountDiscarded
	// pair above, applied to money instead of isolation: whoever has both
	// columns sums them whenever they want; whoever only has the total
	// CANNOT split it back apart.
	//
	// `falhas_de_leitura` is a READ-MARKING failure, never "failed to read
	// something" — the pair lives glued to the POST /v1/leituras route
	// (internal/outbound/leituras_handler.go).
	CounterReadsMarked  = "leituras_marcadas"
	CounterReadFailures = "falhas_de_leitura"

	// CounterTemplatesDeleted (T-173, 2026-08-28): DELETE /v1/templates took a
	// template off the WABA — the `apagado` outcome only, never
	// `ja_nao_existia`, which deleted nothing.
	//
	// WHY THIS ONE EARNS A KEY, when the rule right at the top of this file
	// is "don't invent a metric nobody is going to look at": the deletion is
	// the only route here that destroys something on Meta's side, and it is
	// meant to run in BURSTS (consumer-b has 61 approved templates to take
	// off the account). Without the number, a WABA that lost dozens of
	// templates has nothing in the gateway saying it was us — and the route
	// runs so rarely that the journal will have rotated long before anyone
	// asks. It is the same reasoning as CounterAccountDiscarded: the counter is
	// the trace that outlives the log.
	//
	// There is no `falhas_de_exclusao` sibling, unlike the read pair above,
	// and that is deliberate: a failed deletion answers the consumer with an
	// error IT reads and acts on, in the same request. What nobody can
	// reconstruct afterward is how many actually went through.
	CounterTemplatesDeleted = "templates_apagados"

	// --- BILLING (T-063, 2026-07-28) ---
	//
	// Meta says, in the status webhook, under which CATEGORY it billed that
	// delivery (`pricing.category`, which becomes meta.Billing —
	// internal/meta/types.go). These keys turn the consumer's cost
	// PROJECTION into MEASUREMENT: they already multiply volume by tariff,
	// just with volume estimated on their side. It's the owner's rule
	// applied to money — "a number the gateway promises has to come from
	// the gateway."
	//
	// 🔴 THE NAMES ARE META'S LITERAL VALUES, and that's a decision:
	// `service`, `utility`, `marketing` and `authentication` are exactly
	// the strings observed in `pricing.category`, with no translation into
	// Portuguese. Same rule as Billing.Category, TemplateStatus.State
	// and NumberQuality.CurrentLimit (internal/meta/types.go): a
	// translation table of our own rots the day Meta adds a new value, and
	// the translated name would make the screen's key not match the field
	// in the envelope the same consumer reads. The `cobranca_` prefix is
	// ours and exists only to group; the suffix is theirs, byte for byte.
	//
	// 🔴 ONLY `sent` COUNTS, AND ONLY IT — internal/inbound/cobranca.go. The
	// reason is written there and is worth repeating here because it's what
	// makes these numbers mean money: the SAME wamid shows up in `sent`,
	// `delivered` and `read`, and all three can carry `pricing` (measured:
	// the corpus pair status_sent_com_pricing.json /
	// status_delivered.json shares wamid and timestamp). Counting every
	// status that carries `pricing` would multiply the invoice by up to
	// three — exactly the defect the comment on CounterReadsMarked, right
	// above, existed to keep this task from inheriting.
	CounterBillingMarketing      = "cobranca_marketing"
	CounterBillingUtility        = "cobranca_utility"
	CounterBillingAuthentication = "cobranca_authentication"
	CounterBillingService        = "cobranca_service"

	// CounterBillingOther: Meta billed under a category this vocabulary does
	// NOT know — including the case where it sent `pricing` with no
	// `category`.
	//
	// 🔴 WHY A FIXED KEY AND NOT A KEY PER RECEIVED VALUE. Meta may invent
	// a new category tomorrow, and the two obvious outs are bad:
	//
	//   - SILENTLY DISCARDING means losing money without knowing it. The
	//     sum of categories would stop matching volume and nobody would
	//     have a way to suspect it — this project's most expensive failure shape.
	//   - CREATING A DYNAMIC KEY from the received value makes the
	//     vocabulary GROW WITH OUTSIDE DATA. Three costs, in order of
	//     weight: (1) the vocabulary is CLOSED on purpose and
	//     IncrementCounter REFUSES any key outside it, so a dynamic key
	//     would require tearing down that guard; (2) GET /v1/estado's
	//     response grows with the product of days x vocabulary — the same
	//     product T-081 capped with the window ceiling —, and Meta would
	//     become the one choosing its size; (3) a key that only exists
	//     after the event arrives never shows up zeroed on screen, so the
	//     question "has this ever happened?" goes back to having no answer.
	//
	// The way out is counting here AND LOGGING THE LITERAL VALUE (ALARM,
	// once per slug+category per process — see internal/inbound/cobranca.go).
	// The counter says HOW MUCH; the log says WHICH. It's the same lesson
	// docs/ARMADILHAS.md records about the `desconhecido` outcome that
	// discarded the error: "unknown" is the one class whose value is
	// entirely in the trace.
	CounterBillingOther = "cobranca_outra"

	// CounterBillingAbsent: a `sent` arrived WITHOUT the `pricing` block.
	//
	// NOT AN ERROR, A NORMAL CASE — measured by consumer-a across 267 real
	// payloads (T-069): 4 of 53 raw `sent` came without the block (~7.5%),
	// while all 49 `delivered` came with it. See
	// testdata/corpus/status_sent_sem_pricing.json.
	//
	// IT EXISTS SO THE BLIND SPOT IS A NUMBER, NOT SILENCE. Without this
	// key, ~7.5% of volume simply wouldn't show up anywhere and the
	// measurement would come out SMALLER than reality with nothing flagging
	// it. With it, the math closes: the sum of the categories (the four,
	// plus `cobranca_outra`) plus `cobranca_ausente` is the total of `sent`
	// counted, and the consumer knows exactly the size of what they still
	// need to estimate.
	CounterBillingAbsent = "cobranca_ausente"

	// CounterBillingBillable: of the `sent` above, those where Meta said
	// `billable: true`.
	//
	// IT'S THE NUMBER THAT MULTIPLIES THE TARIFF. The categories say WHICH
	// price; this one says HOW MANY deliveries were actually billed — and
	// the two aren't the same question: the real `service` from the
	// capture came with `billable:false`.
	//
	// ONLY `true` COUNTS. `billable:false` and `billable` absent are
	// different things (that's why Billing.Billable is *bool), but
	// neither one is "billed", and inventing a charge out of absence would
	// be the expensive mistake in the expensive sense. Accepted and
	// documented consequence in the contract: this key is always <= the
	// sum of the categories, and the difference is "not billed" plus "Meta
	// didn't say".
	CounterBillingBillable = "cobranca_cobravel"
)

// KeysInDisplayOrder is the closed vocabulary above, in the ONE order
// it should be shown to the operator in (T-039). The order tells the STORY
// of an event's path — recebidas -> entregues -> recusadas -> descartadas
// -> enviadas -> falhas, with the definitive-loss alarm interleaved right
// after "descartadas" — and NOT the order the constants were declared in above.
//
// The TWO "discarded" keys sit SIDE BY SIDE, and in this order (number,
// then account), which is the order the guards run in
// internal/inbound/handler.go (5a before 5b). Neighbors on screen because
// they answer the SAME question ("was there an isolation refusal?");
// separate because they answer the next question ("which guard?")
// differently — see the comment on CounterNumberDiscarded.
//
// The READ pair comes LAST, after enviadas/falhas_de_envio, and not
// interleaved with them: the distance on screen is what keeps someone from
// eyeballing the two columns as if they were the same family (see
// CounterReadsMarked — marking as read produces no conversation and
// doesn't enter into cost projection).
//
// `templates_apagados` comes right after that pair and BEFORE the billing
// block: like marking as read, it is an action the gateway took on Meta's
// side that produces no conversation and costs nothing — so it belongs on
// the traffic side of the divide, and never inside the money block, whose
// lines all answer "how much was billed?".
//
// The BILLING BLOCK (T-063) comes after everything, together, and for the
// SAME reason: it answers a MONEY question, not a traffic one, and its
// lines don't sum with the ones above (a `sent` counted here was already
// counted as `recebidas` up there — they're counts of the same webhook
// along different axes). Inside the block the order also tells a story:
// the four observed categories, then the two holes (`outra` = don't know
// how to classify, `ausente` = Meta didn't say), and last
// `cobranca_cobravel`, which is a CROSS-CUT of the lines above and
// therefore can't sit in the middle of them.
//
// This is the SINGLE SOURCE: `counterKeys` (below) is DERIVED from
// this list, and `cmd/zapgw/estado.go` WALKS it, never a copy of its own.
// Before T-039, `cmd/zapgw/estado.go` repeated this list by hand — T-038
// added CounterAccountDiscarded here and nobody remembered to add it there, and
// the counter kept incrementing in production without `zapgw estado` ever
// showing it. A new key goes in ONLY here: the `estado` command (and any
// other future consumer of the vocabulary) starts showing it without
// needing any other edit, because there is no longer a second list to forget.
var KeysInDisplayOrder = []string{
	CounterReceived,
	CounterDelivered,
	CounterRefusedByConsumer,
	CounterNumberDiscarded,
	CounterAccountDiscarded,
	CounterDefinitiveLossAlarm,
	CounterSent,
	CounterSendFailures,
	CounterReadsMarked,
	CounterReadFailures,
	CounterTemplatesDeleted,
	CounterBillingMarketing,
	CounterBillingUtility,
	CounterBillingAuthentication,
	CounterBillingService,
	CounterBillingOther,
	CounterBillingAbsent,
	CounterBillingBillable,
}

// counterKeys is the closed vocabulary above, as a set — used to
// refuse any key not on this list. DERIVED from KeysInDisplayOrder:
// there is no way for a key to exist here without also being in the
// display order, which is the very guarantee T-039 asks for.
var counterKeys = buildCounterKeys()

func buildCounterKeys() map[string]bool {
	m := make(map[string]bool, len(KeysInDisplayOrder))
	for _, key := range KeysInDisplayOrder {
		m[key] = true
	}
	return m
}

// ErrUnknownCounterKey: someone tried to write a key outside the
// closed vocabulary. It's an error, not a silent write, because an
// unreviewed new key is exactly the metric nobody is going to look at.
var ErrUnknownCounterKey = errors.New("config: chave de contador fora do vocabulario fechado")

// ShortSeriesDays is the size of `serie_7_dias` — 7 entries, TODAY included.
//
// IT BECAME A NAMED CONSTANT IN T-081, and the reason is the name itself:
// until then 7 was THE series size; now it's ONE size — the one two
// consumers already read and that cannot change shape. A loose `7`
// scattered across the code would stop saying WHICH of the two numbers it is.
const ShortSeriesDays = 7

// SevenDayWindow is the size of the "last 7 days" summary — INCLUDES
// today (today + 6 previous days), for the same reason as any window in
// this project: a number meant to mean "happening right now" without being
// so short it zeroes out between two spaced-out events.
//
// DERIVED from ShortSeriesDays, not a hand-written `6 * 24h`:
// `ultimos_7_dias` is the SUM of `serie_7_dias`, and the contract promises
// the two accounts match. Two independent numbers would make that promise
// depend on nobody touching one without touching the other.
//
// LIVES HERE, ALONGSIDE THE VOCABULARY, AND NOT WITH WHOEVER DISPLAYS IT:
// it was born in cmd/zapgw/estado.go, and T-060 added a SECOND reader (the
// GET /v1/estado route). Two "7 days" constants would diverge on the first
// change, and the symptom would be the consumer and the operator
// disagreeing about the same number on the same instance — this project's
// mother-of-all-traps (docs/ARMADILHAS.md).
const SevenDayWindow = (ShortSeriesDays - 1) * 24 * time.Hour

// DefaultRetentionDays is how many days this installation keeps a daily
// counter when nobody configures anything — the purge deadline
// (PurgeCounters), not a documentation number.
//
// 🔴 IT IS ALSO THE SERIES WINDOW'S CEILING (T-081), and that equality is
// the whole task. An N-day series can only be MEASUREMENT up to where the
// data exists; past the purge deadline, the older days would come out
// filled with zero — indistinguishable from "there was no traffic". Asking
// for 120 days on a database that keeps 90 doesn't return 120 days: it
// returns 30 days of lies wearing the face of fact. That's why the route
// REFUSES a window larger than this number instead of silently shortening
// the response, the same rule as the truncated template catalog — an
// error, never `200`.
//
// The SECOND reason the ceiling exists is more mundane and equally real: a
// series with no ceiling is an invitation for someone to ask for 10 years,
// and the response grows with the product (days x vocabulary) until it
// brings down the dashboard that asked for it.
const DefaultRetentionDays = 90

// CounterRetentionEnvVar is the environment variable that changes the
// deadline above. Documented in docs/IMPLANTACAO.md.
const CounterRetentionEnvVar = "ZAPGW_TTL_CONTADORES_DIAS"

// CounterRetentionDays resolves the counters' retention deadline, in
// DAYS, from the environment.
//
// WHY IT EXISTS, and it's not decoration over three lines of
// `strconv.Atoi`: the deadline used to be resolved ONLY in
// cmd/zapgw/main.go, where the purge lives. T-081 gave this same number a
// SECOND reader — the GET /v1/estado route, which uses it as the window's
// ceiling and cites it in the error. Two resolutions of the same deadline
// diverge on the first change, and the divergence comes out exactly as the
// defect the ceiling exists to prevent: the route accepting 30 days over a
// database that keeps 15. The environment is read ONCE, in `main`, and the
// number flows down as a parameter — no package under internal/ reads an
// environment variable.
//
// AN INVALID VALUE (non-numeric, zero, or negative) FALLS BACK TO THE
// DEFAULT, without an error: it's the behavior the purge already had, and
// changing this here would make an `env` with a wrong digit bring the
// server down on startup instead of keeping counters for longer than requested.
func CounterRetentionDays(getenv func(string) string) int {
	if getenv == nil {
		return DefaultRetentionDays
	}
	if v := getenv(CounterRetentionEnvVar); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultRetentionDays
}

// dayOf formats the instant as the counter's DAY, always in UTC — one
// clock, so "today" and "the last 7 days" mean the same day on every read
// and write, regardless of the timezone the binary runs in.
func dayOf(when time.Time) string {
	return when.UTC().Format("2006-01-02")
}

// stampOf formats the last event's INSTANT, always in UTC and always in
// RFC3339 without a fractional second — FIXED width on purpose: it's what
// makes SQLite's `MAX(ultimo)` (text comparison) actually return the most
// recent one. A variable-width format (or one with a local timezone) would
// make the lexicographic comparison lie with nothing flagging it.
func stampOf(when time.Time) string {
	return when.UTC().Format(time.RFC3339)
}

// IncrementCounter adds 1 to the (slug, dia, key) counter, creating
// the row if it's the first event of that day.
//
// DON'T CALL THIS DIRECTLY FROM THE PATH THAT RESPONDS TO META OR THE
// CONSUMER: that's why Counter exists (below), wrapping this call with
// the "error only logs" guarantee. This method returns a real error
// because its caller (the serialized writer) needs to know if there is
// something to log.
func (s *Store) IncrementCounter(slug, key string, when time.Time) error {
	if !counterKeys[key] {
		return fmt.Errorf("%w: %q", ErrUnknownCounterKey, key)
	}
	// `ultimo` is overwritten on every increment (T-060): the row keeps
	// HOW MANY events there were that day and WHEN the last one was.
	// Counting and stamping in the SAME UPSERT is what keeps the two from
	// diverging — there is no path that increments without stamping.
	_, err := s.db.Exec(`
		INSERT INTO contador (slug, dia, chave, n, ultimo) VALUES (?, ?, ?, 1, ?)
		ON CONFLICT (slug, dia, chave) DO UPDATE SET n = n + 1, ultimo = excluded.ultimo`,
		slug, dayOf(when), key, stampOf(when))
	if err != nil {
		return fmt.Errorf("config: incrementar contador: %w", err)
	}
	return nil
}

// PurgeCounters deletes whole days older than `before` and returns how
// many rows (slug+dia+key) came out.
//
// SAME REASONING AS IDEMPOTENCY (PurgeIdempotency): without purging, the
// table grows forever in a binary meant to run for years. Here the cut is
// by DAY, not by exact timestamp — a counter that exists to answer "how
// many today?" and "how many per day in the requested window?" doesn't
// need finer-than-a-day granularity to decide what's old.
//
// IT IS THE LIMIT OF HOW FAR THE SERIES CAN REACH (T-081): what this
// function deleted doesn't come back as an honest zero — it comes back as
// a zero indistinguishable from "there was no traffic". That's why the
// route refuses a window larger than this purge's deadline
// (CounterRetentionDays) instead of answering a series that only
// looks complete.
func (s *Store) PurgeCounters(before time.Time) (int, error) {
	res, err := s.db.Exec(`DELETE FROM contador WHERE dia < ?`, dayOf(before))
	if err != nil {
		return 0, fmt.Errorf("config: purgar contadores: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("config: linhas purgadas de contador: %w", err)
	}
	return int(n), nil
}

// CountersBetween sums, per key, an instance's counters between `desde`
// and `ate` — the DAYS of both instants, inclusive on both ends.
//
// A key with no event in the period simply does NOT appear in the map;
// whoever reads it decides whether absence means "zero" (and the `zapgw
// estado` command decides yes, so it never prints an error on an instance
// with no traffic — T-035's Verify (f)).
func (s *Store) CountersBetween(slug string, since, until time.Time) (map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT chave, SUM(n) FROM contador
		 WHERE slug = ? AND dia BETWEEN ? AND ?
		 GROUP BY chave`,
		slug, dayOf(since), dayOf(until))
	if err != nil {
		return nil, fmt.Errorf("config: somar contadores: %w", err)
	}
	defer rows.Close()

	m := map[string]int{}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, fmt.Errorf("config: ler contador: %w", err)
		}
		m[key] = n
	}
	// rows.Err() is NOT optional (docs/ARMADILHAS.md, "Meta"): an error
	// midway through iteration would end the loop as if the data had run
	// out, and a summary shorter than reality is this project's most
	// expensive failure shape.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar contadores: %w", err)
	}
	return m, nil
}

// LastEventPerKey returns, per key, WHEN that instance's last event
// was — T-060's stamp.
//
// WHY IT HAS NO WINDOW, unlike CountersBetween: the question it answers is
// "how long ago?", and cutting at 7 days would make a stamp from 20 days
// ago look IDENTICAL to "never happened" — exactly the ambiguity the stamp
// exists to undo. The only cut is the TTL purge (PurgeCounters, 90 days
// by default), and it's written into the contract.
//
// A key with no event at all simply does NOT appear in the map: absence is
// "never", and a zeroed time.Time in its place would be a stamp of January
// 1st, year 1 — a plausible-looking number for someone who only glances at
// the field, which is worse than declared emptiness.
//
// A stamp that fails to decode IS AN ERROR, never a skipped row: the
// column is written by ONE function, in ONE format (stampOf), so an
// unreadable value there means someone edited the database by hand.
// Skipping the row would return a summary SHORTER than reality with
// nothing flagging it — this project's most expensive failure shape
// (docs/ARMADILHAS.md, "Meta").
func (s *Store) LastEventPerKey(slug string) (map[string]time.Time, error) {
	rows, err := s.db.Query(`
		SELECT chave, MAX(ultimo) FROM contador
		 WHERE slug = ? AND ultimo <> ''
		 GROUP BY chave`, slug)
	if err != nil {
		return nil, fmt.Errorf("config: ultimo evento por chave: %w", err)
	}
	defer rows.Close()

	m := map[string]time.Time{}
	for rows.Next() {
		var key, stamp string
		if err := rows.Scan(&key, &stamp); err != nil {
			return nil, fmt.Errorf("config: ler carimbo de contador: %w", err)
		}
		when, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			return nil, fmt.Errorf("config: carimbo de contador (slug=%q chave=%q) nao e RFC3339: %w",
				slug, key, err)
		}
		m[key] = when.UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar carimbos de contador: %w", err)
	}
	return m, nil
}

// CountersPerDay returns, per DAY and per key, an instance's counters
// between the days of `desde` and `ate` (inclusive on both ends).
//
// A DAY WITH NO EVENT AT ALL DOES NOT APPEAR in the outer map — whoever
// builds the series is the one who fills the gaps with zero (see
// dailySeries), because only they know HOW MANY days the series should
// have. Returning an empty day here would be inventing data the table doesn't have.
func (s *Store) CountersPerDay(slug string, since, until time.Time) (map[string]map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT dia, chave, SUM(n) FROM contador
		 WHERE slug = ? AND dia BETWEEN ? AND ?
		 GROUP BY dia, chave`,
		slug, dayOf(since), dayOf(until))
	if err != nil {
		return nil, fmt.Errorf("config: contadores por dia: %w", err)
	}
	defer rows.Close()

	m := map[string]map[string]int{}
	for rows.Next() {
		var day, key string
		var n int
		if err := rows.Scan(&day, &key, &n); err != nil {
			return nil, fmt.Errorf("config: ler contador por dia: %w", err)
		}
		if m[day] == nil {
			m[day] = map[string]int{}
		}
		m[day][key] = n
	}
	// The usual reason: an error midway through iteration would end the
	// loop as if the data had run out, and a series shorter than reality
	// is this project's most expensive failure shape.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config: iterar contadores por dia: %w", err)
	}
	return m, nil
}

// CounterDay is ONE day of the series: the day in UTC (`2006-01-02`)
// and what was counted on it.
type CounterDay struct {
	Day string
	// N only has the keys that had an event that day. Whoever displays it
	// fills the rest with zero by walking KeysInDisplayOrder — never
	// a list of its own.
	N map[string]int
}

// CounterSummary is what an instance has to say about its own traffic:
// the TWO windows this project shows, the day-by-day series, and the
// stamp of each key's last event.
type CounterSummary struct {
	Today     map[string]int
	Last7Days map[string]int
	// Series is the REQUESTED window, OLDEST to newest, with exactly as
	// many entries as days were requested — including days with no
	// traffic (with empty N, which whoever displays it reads as zero).
	// Fixed length on purpose: a series that omitted empty days would make
	// "nothing happened this day" and "this day doesn't exist in the
	// response" become the same thing on the consumer's chart — the same
	// ambiguity the stamp exists to undo, one level up.
	Series []CounterDay
	// ShortSeries ALWAYS has ShortSeriesDays entries — it's the
	// `serie_7_dias` two consumers already read, and it cannot change
	// shape even when the requested window is different.
	//
	// THE TWO ARE SLICES OF THE SAME SERIES, and that's the guarantee
	// (T-081): when the requested window is 30 days, `ShortSeries` is
	// literally the 7-day SUFFIX of `Series` — same days, same numbers, no
	// second database query. Two independent reads could diverge (all it
	// takes is midnight falling between them), and the consumer would see
	// two charts disagreeing about the same day of the same instance —
	// this project's mother-of-all-traps.
	ShortSeries []CounterDay
	// LastEvent only has the keys that have ever happened (within
	// retention). Absence = never.
	LastEvent map[string]time.Time
}

// SummarizeCounters builds an instance's CounterSummary at one instant.
//
// IT IS THE SINGLE SOURCE OF THE NUMBERS THAT COME OUT OF HERE: `zapgw
// estado` (cmd/zapgw/estado.go) and the `GET /v1/estado` route
// (internal/outbound/estado_handler.go) call THIS function, never a copy
// of the calculation. T-060 requires the two to return the same numbers
// for the same instance at the same instant; with two calculations that
// would be coincidence the first change would undo, and here it's true by
// construction.
//
// `now` comes in as a parameter, not from time.Now() in here, so the two
// readers can prove the same read at the SAME instant.
func (s *Store) SummarizeCounters(slug string, now time.Time) (CounterSummary, error) {
	return s.SummarizeCountersWithSeries(slug, now, ShortSeriesDays)
}

// SummarizeCountersWithSeries is SummarizeCounters with the daily series
// size chosen by whoever is asking (T-081).
//
// WHY A SECOND FUNCTION, AND NOT ONE MORE PARAMETER ON THE FIRST: the two
// readers want different things and neither should carry the other's
// decision. `zapgw estado` always shows the short window (its table
// doesn't print any series), and only the route has a consumer with a
// 30-day chart. A mandatory parameter would make the CLI choose, on every
// call, a number that isn't its business.
//
// 🔴 THE 30-DAY DATA WAS ALREADY IN THE DATABASE — and confirming that was
// step (1) of T-081, before any code. The `counter` table keeps one row
// per (slug, dia, key) and the only thing that deletes a row is the
// age-based purge (PurgeCounters, DefaultRetentionDays). The 7-day cut
// was never about storage: it was the interval THIS function asked the
// database for. Building new aggregation to "keep 30 days" would have been
// work on data that already existed — and nobody undoes redundant storage
// once it exists.
//
// `seriesDays` less than 1 FALLS BACK to the short window instead of
// blowing up the slice. The real validation (and the `400` refusal) lives
// in the route, which is the one holding the consumer's request; this
// guard exists only so a programming mistake in another caller doesn't
// turn into a panic in the middle of a response.
func (s *Store) SummarizeCountersWithSeries(slug string, now time.Time, seriesDays int) (CounterSummary, error) {
	if seriesDays < 1 {
		seriesDays = ShortSeriesDays
	}
	today, err := s.CountersBetween(slug, now, now)
	if err != nil {
		return CounterSummary{}, err
	}
	seven, err := s.CountersBetween(slug, now.Add(-SevenDayWindow), now)
	if err != nil {
		return CounterSummary{}, err
	}
	// ONE read, at the LARGER of the two sizes, and both series come out
	// of it as slices. Reading twice (once for the requested window, once
	// for the 7 days) would open the door for the two to disagree — all it
	// takes is midnight falling between the queries — and the consumer
	// would see `serie_diaria` and `serie_7_dias` counting the same day differently.
	days := max(seriesDays, ShortSeriesDays)
	perDay, err := s.CountersPerDay(slug, now.Add(-time.Duration(days-1)*24*time.Hour), now)
	if err != nil {
		return CounterSummary{}, err
	}
	last, err := s.LastEventPerKey(slug)
	if err != nil {
		return CounterSummary{}, err
	}
	series := dailySeries(perDay, now, days)
	return CounterSummary{
		Today:       today,
		Last7Days:   seven,
		Series:      series[days-seriesDays:],
		ShortSeries: series[days-ShortSeriesDays:],
		LastEvent:   last,
	}, nil
}

// dailySeries enumerates the window's days — OLDEST to newest — and
// matches each one with what was counted on it.
//
// THE ENUMERATION IS FROM THE CALENDAR, NOT FROM THE TABLE: that's what
// guarantees the `days` entries even on an instance that received nothing
// all week. Walking the database rows would give a variable-length series,
// and the consumer's chart would change shape depending on traffic —
// worse, a zeroed day would disappear instead of showing up as zero.
func dailySeries(perDay map[string]map[string]int, now time.Time, days int) []CounterDay {
	series := make([]CounterDay, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := dayOf(now.Add(-time.Duration(i) * 24 * time.Hour))
		n := perDay[day]
		if n == nil {
			n = map[string]int{}
		}
		series = append(series, CounterDay{Day: day, N: n})
	}
	return series
}

// Counter is the SINGLE WRITER of instance counters.
//
// WHY A SEPARATE TYPE, AND NOT JUST CALLING Store.IncrementCounter
// DIRECTLY FROM THE HANDLER: http.Server serves each request in a
// goroutine over the SAME handler — this project has already had a
// Critical over exactly this (`seq++`, docs/ARMADILHAS.md, "Go /
// concorrência"). The mutex serializes the write, and — more important
// than the mutex — Record RETURNS NOTHING: there is no error for the
// caller to propagate, because no error comes out of this method at all.
// The "counting failure only logs" guarantee lives in the signature,
// never in the memory of whoever writes the handler.
// CounterStore is what Counter needs from the Store. EXPORTED on
// purpose: tests in OTHER packages (internal/inbound, internal/outbound)
// use an implementation that ALWAYS fails to prove, on the real HTTP
// handler, that Record never propagates that error into the response
// already written to Meta (T-035, Verify (c) — "counter failure doesn't
// change the status returned to Meta").
type CounterStore interface {
	IncrementCounter(slug, key string, when time.Time) error
}

type Counter struct {
	mu    sync.Mutex
	store CounterStore
}

// NewCounter wraps the store with the guarantee of serialized writes and
// no error propagation. It is the PRODUCTION constructor.
func NewCounter(store *Store) *Counter {
	return &Counter{store: store}
}

// NewCounterWithStore is like NewCounter, but accepts any CounterStore.
// It exists for testing: it's what lets a handler in another package
// prove, against an implementation that always fails, that counting never
// changes the response.
func NewCounterWithStore(store CounterStore) *Counter {
	return &Counter{store: store}
}

// Record adds 1 to today's (slug, key) counter.
//
// Calling this is always safe after w.WriteHeader: even if the write fails
// (database down, disk full, a key outside the vocabulary from a
// programming mistake), nothing here returns an error or can change what
// was already answered to Meta or the consumer — only a log line is left behind.
func (c *Counter) Record(slug, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.store.IncrementCounter(slug, key, time.Now()); err != nil {
		log.Printf("zapgw: falha ao gravar contador (slug=%q chave=%q): %v", slug, key, err)
	}
}
