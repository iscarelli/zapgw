// The state of ONE instance: one place that BUILDS it, two surfaces that
// PRESENT it (T-065).
//
// WHY THIS FILE EXISTS, and the cost it closes: until T-064 the state was
// built inside the `GET /v1/estado` handler, and `zapgw estado`
// (cmd/zapgw/state.go) showed only the counter table. Four blocks the
// CONSUMER saw and the OPERATOR didn't: `estado`/`pausada`, `versao`,
// `token_meta` and `certificado_do_callback`. It wasn't DATA divergence (the
// counters always came from config.SummarizeCounters, single source since
// T-039) — it was SURFACE asymmetry, which is this project's mother-pitfall in
// its most expensive form: the information exists and just doesn't appear
// where someone is looking.
//
// And whoever is looking through the CLI is, almost by definition, IN THE
// MIDDLE OF AN INCIDENT — with SSH open on the CT, and no time to leave it,
// find a consumer token and call the internal route to ask the binary in
// front of them whether Meta still accepts the token and when the consumer's
// certificate expires.
//
// THE SPLIT, and it is this file's only rule: CONTENT is shared
// (BuildState), SCREEN FORMAT belongs to each surface — the route serializes
// the State to JSON, the CLI prints the StateRows in a table. Neither of
// the two enumerates fields: adding a field to State here makes it appear in
// BOTH without editing either. That is T-065's mandatory mutation, made and
// reverted before the commit, and it is the only proof that the source really
// is a single one — its cost is in docs/ARMADILHAS.md.
package outbound

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// State is the published state of an instance — the body of `200` from
// `GET /v1/estado` and the content of `zapgw estado`.
//
// THE FORMAT ONLY GROWS, like /v1/health's does: a consumer reading
// `contadores.recebidas.hoje` today must not break when a new field arrives.
type State struct {
	Instance string `json:"instance"`
	// Type is config.TypeWhatsApp or config.TypeInstagram (T-097/T-098), always
	// present (T-107). Until now this route had the SAME blindness that T-103
	// fixed in `zapgw instancia mostrar`/`listar`: without this field the
	// consumer would have to DEDUCE the type from the absence of other blocks
	// (token_instagram nao_se_aplica, numero_na_meta nao_se_aplica...), which is
	// guessing, never reading.
	Type string `json:"kind"`
	// IgID is the Instagram-scoped Business Account ID (T-102) — it ONLY has a
	// value when Type == config.TypeInstagram. On a WhatsApp instance it holds
	// NotApplicable, NEVER an absent field nor an empty string (T-107): an
	// `ig_id: ""` would look like forgotten configuration, and the consumer
	// would have no way to distinguish "this type doesn't use ig_id" from
	// "nobody registered one". It IS an IDENTIFIER, not a secret — same decision
	// as T-102 (`WabaID`, `PhoneNumberID`) — so the real VALUE goes out, never
	// just a boolean. It was a wrong `ig_id` that caused T-102's defect; this
	// route is the one the consumer reads, and it needs to be able to confirm
	// the right account.
	IgID string `json:"ig_id"`
	// State is the SAME word from `zapgw instancia listar` ("ativa"/"pausada"),
	// via the SAME function (config.StateOf): two words for the same state
	// would force whoever operates it to translate between their screen and ours.
	State string `json:"state"`
	// Paused is the SAME fact as `State`, in the form that an alarm rule
	// consumes without comparing strings. Both come out of the SAME boolean, in
	// the SAME literal — there is no path by which they could diverge.
	//
	// WHY IT'S WORTH THE EXTRA FIELD (consumer request, 2026-07-28): a paused
	// instance answers 503, volume goes to zero and the timestamp ages —
	// INDISTINGUISHABLE from "nobody wrote anything". Without this field the
	// consumer's alarm says "no delivery in 200 minutes" when the cause is
	// "it's paused". It's the same disease the timestamp cures, one level up:
	// the symptom doesn't point to the cause.
	Paused bool `json:"paused"`
	// Version is the binary that answered — the SAME value from /v1/health. It
	// keeps "which version was live when this happened?" from turning into
	// deploy archaeology when the consumer keeps this response alongside their
	// incident.
	Version string `json:"version"`
	// GeneratedAt is the instant THIS state was built — not the age of any
	// measurement (that age lives in the timestamps). It travels so that a
	// dashboard or proxy that saves this response cannot present it as fresh.
	GeneratedAt string `json:"generated_at"`
	// StampsSince is the instant from which THIS instance records counter
	// timestamps — the age of the INSTRUMENT, not of the data (T-070, requested
	// by `consumer-a`).
	//
	// WHY IT SITS AT THE TOP AND NOT INSIDE `contadores`: it doesn't belong to
	// one key, it belongs to all of them — repeating it per key would be the
	// same answer eight times, and putting it in just ONE of them would make the
	// others look different.
	//
	// WHAT IT DISAMBIGUATES: `ultimo_em: null` hides TWO states — "never
	// happened" (normal) and "happened before the timestamp existed" (blind
	// spot: the timestamp was born in v0.23.0). With this field the consumer
	// decides on their own, and without it EVERY consumer would hardcode the
	// v0.23.0 date — a constant that rots on the first new instance, because an
	// instance created today timestamps from today. It comes from the DATABASE,
	// per instance (config.InstanceSummary.StampsSince); which value a
	// pre-existing instance receives, and why, is in the
	// "instancia.carimbos_desde" migration (internal/config/store.go).
	StampsSince string `json:"stamps_since"`
	// The `cli:"tabela"` tag says the CLI shows this field in the counters
	// TABLE, not in the label/value list — see StateRows. It lives here, in
	// the declaration, and not in an exceptions list inside the CLI, for exactly
	// this task's reason: an exceptions list on one surface is the seed of the
	// next divergence.
	Counters map[string]CounterInState `json:"counters" cli:"tabela"`
	// Series7Days is the day-by-day of the SAME window that `ultimos_7_dias`
	// summarizes: ALWAYS 7 entries, oldest to newest, with days that had no
	// traffic present and zeroed. The total alone doesn't distinguish "four
	// messages today" from "four messages on Monday and nothing since" — and
	// it's the second shape that matters to whoever is drawing a chart.
	//
	// THE CLI DOES NOT PRINT IT (and that's why it's also `cli:"tabela"`): seven
	// days times the whole vocabulary is dozens of lines per instance, and
	// whoever is at the terminal wants the summary — the series exists for a
	// chart, and a chart is for whoever has the web.
	//
	// 🔴 IT IS A LIVE CONTRACT AND DOES NOT CHANGE SHAPE (T-081). Two consumers
	// read it today; it keeps 7 entries even when the requested window is 30
	// days, and in that case it is literally the SUFFIX of `serie_diaria` (both
	// come from the same read, see config.CounterSummary). The name carries
	// a number and so it cannot grow: `serie_7_dias` with 30 entries would be a
	// field lying about itself in the `console.log` of whoever is debugging.
	Series7Days []DayInState `json:"last_7_days_series" cli:"tabela"`
	// DailySeries is the SAME series over the window the consumer ASKED FOR
	// (`?serie_dias=`, default ShortSeriesDays) — the new field from T-081.
	//
	// WHY A NEW FIELD, AND NOT `serie_7_dias` growing: it's T-044's form B, the
	// same one the `dia`/`dia_utc` pair uses just below, and this project's only
	// deprecation form. A new field comes in ADDITIVELY, the old one keeps
	// working byte for byte, and removal is FUTURE and CONDITIONED on both
	// consumers confirming in writing that they no longer read it. Never a
	// deadline.
	//
	// WHY IT EXISTS, AND IT IS DEBT: `consumer-b`'s dashboard has a 30-day chart
	// ("how much will I spend this month") that used to be fed by the WABA's
	// `analytics`, DIRECTLY on the Graph. The owner's rule — *nobody talks
	// directly to Meta* — closed that path, and the route only delivered 7 days,
	// which answer a different question ("is it delivering?"). Same family as
	// T-080.
	//
	// IT ALWAYS APPEARS, even when nobody asked for any window (in which case
	// ShortSeriesDays applies and it repeats `serie_7_dias`): a field that only
	// sometimes appears forces the consumer to distinguish "absent" from "empty"
	// to answer the same question, and this file has already refused that twice
	// (see LastAt and CertificateInState).
	DailySeries []DayInState `json:"daily_series" cli:"tabela"`
	// MetaToken is the watchdog's live check (watchdog.go) — "does Meta still accept
	// this instance's token?".
	//
	// ONLY MEANINGFUL ON WHATSAPP (T-099). The watchdog measures by calling
	// GET /{phone_number_id} (ObserveNumber/CheckCredential), and Instagram
	// NEVER has a phone_number_id — ValidateInstanceType refuses the
	// registration if it comes filled in (store.go). Without the override
	// below, the watchdog would call the Graph with an EMPTY phone_number_id,
	// meta.PhoneNumberIDValid would return ErrInvalidPhoneNumberID, and
	// definitiveOutcome (watchdog.go) treats that error as CREDENTIAL REFUSED — a
	// PERMANENT and FALSE `veredito: "recusado"` on every Instagram instance,
	// never a real measurement. That is what reading watchdog.go for this task
	// found: the timer RUNS for Instagram instances (Check's loop doesn't
	// filter by type), but what it measures MAKES NO SENSE there. That's why
	// metaTokenInState, below, publishes NotApplicable instead of what the watchdog
	// read, for the SAME reason as NumberAtMeta.
	MetaToken MetaToken `json:"meta_token"`
	// CallbackCertificate is the validity of the CONSUMER's certificate,
	// alongside token_meta and for the same reason: both answer "will this
	// credential still work tomorrow?", one for Meta's side and the other for
	// theirs.
	CallbackCertificate CertificateInState `json:"callback_certificate"`
	// NumberAtMeta is the number's quality and message limit (T-080), next to
	// the two blocks above because it answers the SAME family of question:
	// "will this channel keep working?" — now from the QUOTA side instead of
	// the credential side.
	//
	// ONLY EXISTS ON WHATSAPP (T-099): message quality and tier are WhatsApp
	// Business Number concepts, and Instagram does NOT have them — never will,
	// it's not "haven't measured yet". On a tipo=instagram instance both values
	// come out as NotApplicable, never CertNeverObserved: they are DIFFERENT
	// answers (measured in production, tenant-two-ig, v0.36.0, 2026-07-30 — the
	// block came out `nunca_observado`, which says "wait", when the correct
	// answer is "will never exist here, don't look").
	NumberAtMeta NumberAtMeta `json:"number_at_meta"`
	// InstagramToken is the validity of Instagram's long-lived token (T-098),
	// next to the three blocks above for the SAME family of question — "will
	// this channel keep working?", now from the token side that ONLY Instagram
	// has a fixed expiry with no automatic re-authentication possible.
	InstagramToken InstagramTokenInState `json:"instagram_token"`
	// Ingress is WHERE this gateway's ingress is published, and whether the
	// connector that publishes it is up (T-120, owner's request).
	//
	// ⚠️ IT DOES NOT SAY, AND MUST NOT COME TO SAY, THAT THE GATEWAY IS
	// REACHABLE FROM OUTSIDE — a request that never arrives leaves no trace in
	// here. The whole reason, with the cost it has already charged, is in
	// ingress.go's header.
	//
	// IT IS THE ONLY BLOCK IN THIS RESPONSE THAT DOESN'T BELONG TO THE
	// INSTANCE, BUT TO THE PROCESS: two instances of the same gateway read the
	// same `via` and the same `conector` (only `ultimo_webhook_em` is per
	// instance). It travels here, and not in a new route, because the question
	// it answers — "is anything still coming in, and through where?" — is asked
	// together with the counters, never alone.
	Ingress IngressInState `json:"ingress"`
	// ExternalReach is the verdict of the probe that measures ingress FROM
	// OUTSIDE our network (T-121) — brought into this same response at the
	// owner's request, so the consumer only ever needs to talk to the gateway.
	//
	// ⚠️ IT DOES NOT REPLACE THE PROBE'S PUBLIC URL
	// (docs/CONTRATO-CONSUMIDOR.md): in the case where it matters most — a
	// SILENT GATEWAY — asking here returns nothing. The whole reason is in
	// external_probe.go's header.
	ExternalReach ExternalReachInState `json:"external_reach"`
	// Leadership says whether the send singleton guard exists in this
	// installation and whether THIS machine holds it — see LeadershipInState
	// (leadership.go).
	Leadership LeadershipInState `json:"lideranca"`
}

// CounterInState is one key of the vocabulary: the TWO windows that
// `zapgw estado` shows, plus the timestamp.
//
// THE TIMESTAMP IS THIS BLOCK'S MOST VALUABLE ITEM, and the cost that
// motivated it is dated: on 2026-07-28, 11:07, delivery stopped and
// `recebidas` sat stuck at 4 — "stuck from failure" and "stuck because
// nobody wrote anything" are the SAME number. The drop only showed up
// because a journal monitor had been armed by hand that morning. A stuck
// counter is ambiguous; a timestamp AGES. With it the consumer can alarm on
// "last delivered more than N minutes ago" WITHOUT knowing anything about
// our normal volume — which is the only rule that works for someone who
// doesn't know the other side's traffic.
type CounterInState struct {
	Today     int `json:"hoje"`
	Last7Days int `json:"last_7_days"`
	// LastAt is `null` when that key was never counted within the counters'
	// retention. Pointer without omitempty: the key ALWAYS exists, so the
	// dashboard doesn't have to distinguish "absent" from "null".
	LastAt *string `json:"last_at"`
}

// DayInState is one day of the series. The date is in UTC, the SAME
// timezone the counters are recorded in (config.dayOf) — converting to the
// reader's timezone would make the sum of the seven days stop matching
// `ultimos_7_dias`.
//
// TWO FIELDS WITH THE SAME VALUE, and this is the transition from one name
// to the other (T-070, requested by `consumer-a` on 2026-07-28, with the
// route being ONE day old). Their argument holds and is worth copying:
// putting the "it's UTC" warning in the contract **puts the guard in the
// consumer's intention, and a new consumer doesn't read the contract**; a
// field NAME travels with the data all the way into the `console.log` of
// whoever is debugging at two in the morning. They proved it with the very
// bug they were fixing that day: a docstring saying "do NOT touch this
// field" did not stop `default=` from writing it wrong — it cost undelivered
// budget and two burned resends.
//
// ADDITIVE, NEVER RENAMED: renaming is a contract break, and a contract
// break is the owner's decision; additive is ours. Removing `dia` is FUTURE
// and CONDITIONED on both consumers confirming in writing that they no
// longer read it — T-044's form B (`botoes_url`), which is this project's
// precedent. Do not invent a second deprecation pattern.
//
// AND THAT PRECEDENT ALREADY RAN TO THE END: `botoes_url` left in T-045
// (2026-07-28), after both consumers confirmed in writing, each one citing
// the line of their own code. There was never a deadline. What's left of the
// field is a RECUSA (refusal) named in message.go — not a working field.
type DayInState struct {
	// Day is OBSOLETE: same value as DayUTC, kept for whoever already reads it.
	Day string `json:"day"`
	// DayUTC is the right name — it states the timezone, and that's why it
	// survives being copied outside this document.
	DayUTC   string         `json:"day_utc"`
	Counters map[string]int `json:"counters"`
}

// dayInState builds the pair from ONE date. It's what keeps `dia` and
// `dia_utc` from diverging: there is no path that fills one without the
// other, and on the day `dia` leaves (see DayInState) there's simply one
// less assignment here, not a divergence loose in the code.
func dayInState(day string, counters map[string]int) DayInState {
	return DayInState{Day: day, DayUTC: day, Counters: counters}
}

// The TWO states of the observed certificate. They are words, not the
// field's absence, and the decision is written in CertificateInState.
const (
	// CertNeverObserved: no delivery from this instance has ever completed
	// a handshake — a newly-created instance, or a consumer that never
	// received anything.
	CertNeverObserved = "never_observed"
	// CertObserved: there is a date, and there is the instant it was seen.
	CertObserved = "observed"
)

// NotApplicable is the ONLY literal in this file for "this instance doesn't
// have this data, by TYPE design" — never "haven't measured yet" (that
// answer is CertNeverObserved, a DIFFERENT idea: here there will NEVER be a
// measurement, there one may still happen).
//
// WHY A SINGLE CONSTANT, SHARED BY THE THREE BLOCKS THAT NEED IT
// (token_instagram on a WhatsApp instance — T-098 — and numero_na_meta and
// token_meta on an Instagram instance — T-099): it's this project's
// mother-pitfall in its narrowest form — "the rule holds in one place and
// doesn't hold in the next" — applied to a SINGLE word instead of a whole
// mechanism. T-098 wrote VerdictIGTokenNotApplicable with the literal loose;
// T-099 measured in production (`tenant-two-ig`, v0.36.0, 2026-07-30 21:11)
// that the inverse side said `nunca_observado` — the WRONG answer, because
// quality and tier ARE WhatsApp concepts and will NEVER exist on an
// Instagram instance, which is exactly what NotApplicable states and
// CertNeverObserved does not. Two constants with the SAME text would
// diverge on the day someone edited only one — that's why there is a single
// one, and the blocks that need it read this one.
const NotApplicable = "not_applicable"

// CertificateInState is the `certificado_do_callback` block (T-064).
//
// TWO TIMESTAMPS, THE SAME DISCIPLINE AS token_meta: `expira_em` alone
// doesn't say whether it's information from now or from three weeks ago, and
// the gateway only updates it on DELIVERY (there's no probe). A certificate
// observed three weeks ago may already have been renewed — or may already
// have expired. With `observado_em` alongside it, the reader decides;
// without it, the gateway would be asserting as current something it never
// measured.
//
// WHY A NAMED STATE, AND NOT THE ABSENT FIELD NOR JUST `null`. This is the
// point T-060 paid to learn: it went live with `ultimo_em: null` on a
// counter that HAD history (the timestamp only started being recorded at
// that moment), and a consumer that treats `null` as "very old" starts with
// a false positive on everything. The same thing happens here by
// construction — an instance that never delivered has no observed
// certificate. The three possible shapes, and why this one:
//
//   - AN ABSENT FIELD forces the consumer to distinguish "absent" from
//     "null" to answer the SAME question, and this file has already refused
//     that once (see LastAt, above). Worse: it disappears in exactly the
//     case it needs to handle;
//   - JUST `expira_em: null`, with no word at all, is ambiguous with
//     "couldn't read it" and invites the wrong reading "no date = expired";
//   - A NAMED STATE has no plausible wrong reading: `nunca_observado`
//     doesn't look like any date and doesn't compare against any date. It's
//     the same device `token_meta.veredito` already uses for the same
//     problem ("desconhecido"), so there's no new vocabulary to learn.
//
// WHAT THE GATEWAY DOES NOT SAY, AND IT'S NOT AN OVERSIGHT: there is no
// "expired" state nor "expires in N days". That is a JUDGMENT over an
// observation that may be stale, and whoever has both timestamps does that
// math better than we do — including discounting for the observation's age,
// which only they know whether to tolerate. (The CLI shows the distance in
// days next to the date, but that is a READING of the same date, done on the
// screen of someone who already has both timestamps in front of them — not
// a published judgment.)
type CertificateInState struct {
	State string `json:"state"`
	// ExpiresAt is the NotAfter of the callback's LEAF certificate, in
	// UTC/RFC3339. `null` ONLY in the nunca_observado state, never a made-up
	// date.
	ExpiresAt *string `json:"expires_at"`
	// ObservedAt is when the gateway saw that certificate — the instant of
	// delivery, not of now.
	ObservedAt *string `json:"observed_at"`
}

// NumberAtMeta is the `numero_na_meta` block (T-080): what Meta says about
// this instance's NUMBER.
//
// THE `estado` WORDS ARE THE SAME AS THE CERTIFICATE'S (CertNeverObserved
// and CertObserved, not new constants with the same values): the problem is
// the same — "is there an observation or not?" — and a second vocabulary for
// it would force the consumer to learn two tables for the same question. Two
// constants with the same text would also diverge on the day someone edited
// just one.
//
// WHY IT EXISTS, AND IT IS DEBT: until 2026-07-28 the consumer read
// `quality_rating` and the tier DIRECTLY on the Graph API. The owner's rule
// — *nobody talks directly to Meta* — closed that path with no substitute,
// and their screen started saying "awaiting a gateway route". Each of the
// two values decides something over there: the TIER adjusts the daily limit
// in the dashboard on its own (the operator planning the month with the old
// tier plans wrong), and the QUALITY is the EARLY warning of a restriction —
// finding out through the restriction is finding out late.
//
// ONE TIMESTAMP PER VALUE, NOT ONE FOR THE BLOCK, and that is NOT ceremony:
// the two values arrive through different paths, at different instants (see
// config/number.go). A single `medido_em` for both would be true about one
// and false about the other every time a webhook updated just the limit.
type NumberAtMeta struct {
	// Quality is Meta's `quality_rating`, LITERAL ("GREEN", "YELLOW"...).
	// It only ever comes from a measurement — the quality webhook doesn't
	// carry any rating (see config.NumberAtMeta).
	Quality ObservedValue `json:"quality"`
	// MessageLimit is the tier, LITERAL ("TIER_250", "TIER_1K"...) —
	// NEVER 250 nor 1000. Translating it would hide whatever tier Meta
	// invents tomorrow, returning a plausible number for a value nobody
	// checked. Same rule as T-058.
	//
	// IT HAS TWO SOURCES and the `fonte` field says which one won — the
	// tie-break rule (the most recent observation wins, not the "preferred"
	// source) lives in config.UpdateNumberAtMeta, in one single place.
	MessageLimit ObservedValue `json:"message_limit"`
	// CheckedAt is the last time the gateway TRIED to measure — not the
	// last time it learned something. The divergence between it and the
	// `observado_em` above is the signal, exactly as with token_meta: it
	// moving while the others stay still means the measurement is going back
	// and forth without bringing the fields back.
	//
	// `null` means there was never an attempt — a PAUSED instance is never
	// measured (the watchdog skips it on purpose), so that `null` is the right
	// answer, not a failure.
	CheckedAt *string `json:"checked_at"`
}

// ObservedValue is ONE value from Meta with provenance: what, when, and
// from where.
//
// WHY A NAMED STATE, AND NOT JUST `null` ON THE FIELDS — it's the lesson
// T-064 paid for and this task had to repeat: a consumer that treats `null`
// as "very old" starts with a false positive on EVERY new instance. `null`
// alone is also ambiguous with "couldn't read it" and invites the wrong
// reading "no value = bad". `nunca_observado` doesn't look like any value
// and doesn't compare against any value.
//
// AND WHY THE STATE IS PER VALUE, NOT PER BLOCK: the mixed state genuinely
// exists — a limit webhook arrives before the first measurement, and then
// the limit is observed and the quality is not. A single state would have
// to lie about one of the two.
type ObservedValue struct {
	State string `json:"state"`
	// Value is Meta's literal, untranslated. `null` ONLY in nunca_observado.
	Value *string `json:"value"`
	// ObservedAt is when the GATEWAY learned this value — not Meta's
	// timestamp. The why (two clocks nobody synchronized) is in
	// config/number.go.
	ObservedAt *string `json:"observed_at"`
	// Source is `measurement` or `webhook` — config.SourceMeasurement/SourceWebhook.
	//
	// IT TRAVELS ALL THE WAY TO THE CONSUMER ON PURPOSE: without it, two
	// reads with different values don't say whether the gateway measured or
	// Meta notified, and that's the first question of whoever is looking at
	// a number that changed.
	Source *string `json:"source"`
}

// VerdictReader is what BuildState needs from the token's watchdog.
//
// IT IS AN INTERFACE, not *Watchdog, because of the two surfaces: the route
// reads the watchdog running in the SERVER's process, with a history of several
// ticks; `zapgw estado` runs in a process that just started and whose watchdog
// is empty — it does ONE tick before reading (see cmd/zapgw/state.go). The
// narrow type makes this explicit: what the state needs is "someone who
// knows the verdict", not "the timer".
type VerdictReader interface {
	Read(slug string) MetaToken
}

// The FIVE verdicts of InstagramTokenInState. They are their OWN words,
// not a reuse of VerdictOK/VerdictRefused (watchdog.go): the Instagram token
// has a lifecycle the WhatsApp verdict does not have (age, renewal,
// definitive expiry) — forcing the same vocabulary would hide that
// difference behind a coincidence of names.
const (
	// VerdictIGTokenNotApplicable: the instance is NOT tipo=instagram.
	// ABSENCE HAS TO BE ASSERTED, never inferred from an empty field — the
	// SAME doctrine that NumberAtMeta and CallbackCertificate already
	// follow in this file: a WhatsApp consumer seeing this block zeroed
	// could think renewal is BROKEN, when it simply doesn't exist on their
	// side (a System User token doesn't expire in 60 days).
	//
	// = NotApplicable, and not its own literal (T-099): it's the SAME answer
	// numero_na_meta and token_meta give on the Instagram side, and both
	// directions have to use the SAME word.
	VerdictIGTokenNotApplicable = NotApplicable
	// VerdictIGTokenWaiting: valid token, not yet time to try renewing
	// (age < DaysToRenewIGToken days). Normal state — not a problem, and
	// the `instrucao` field stays absent so it doesn't look like one.
	VerdictIGTokenWaiting = "pending"
	// VerdictIGTokenOK: the automatic loop has ALREADY renewed this token
	// successfully at least once (RenewedAt != nil) and the most recent
	// attempt isn't failing. It's the answer to "did it manage to validate?"
	// — it ONLY appears after a real renewal, never just because the
	// original token is still within its validity (that is
	// VerdictIGTokenWaiting).
	VerdictIGTokenOK = "ok"
	// VerdictIGTokenFailing: the most recent renewal attempt failed
	// (RenewInstagramToken refused by Meta, or writing the new token
	// failed) and the token has NOT yet expired. `FailingSince` tracks it,
	// honest SINCE the first failure — this project does not escalate to an
	// ALARM on its own (owner's decision, 2026-07-30): whoever has a channel
	// with the person who fixes it is the CONSUMER, not the gateway.
	VerdictIGTokenFailing = "failing"
	// VerdictIGTokenExpired: past InstagramTokenValidity without renewing.
	// No automatic renewal is possible — only a manual login on Meta.
	VerdictIGTokenExpired = "expired"
)

// InstagramTokenInState is the `token_instagram` block (T-098): when the
// current token was set, when it expires, how many days are left, and — the
// part that makes it USEFUL for whoever doesn't have access to the token —
// whether the automatic renewal mechanism has already proven it works, and
// what to do when it doesn't.
//
// 🔴 THE CONSUMER CANNOT FIX IT ON THEIR OWN — the token isn't in their
// hands, by this gateway's design (NINGUÉM fala direto com a Meta,
// CLAUDE.md). That's why `Instruction`, below, is not cosmetic: a
// `veredito: "falhando"` without saying WHAT TO DO is a dead end for
// whoever has no support channel of their own — and this project's doctrine
// is explicit about it (CLAUDE.md, section "NINGUÉM fala direto com a Meta":
// "se o consumidor não pode contornar, o gateway tem de resolver ou
// reportar com precisão").
type InstagramTokenInState struct {
	Verdict string `json:"verdict"`
	// SetAt and ExpiresAt are `null` ONLY when Verdict == nao_se_aplica
	// — on every tipo=instagram instance they ALWAYS appear, even while
	// aguardando (it's the consumer's request, said out loud: "we want to
	// know so we can at least watch the date").
	SetAt     *string `json:"definido_em"`
	ExpiresAt *string `json:"expires_at"`
	// DaysLeft tracks ExpiresAt under the SAME condition — it exists
	// because doing the subtraction against ExpiresAt in your head, in the
	// middle of an incident, is exactly the mistake ReadableDistance (further
	// below in this file) already exists to spare the OPERATOR from; here
	// the same holds for the CONSUMER, who only reads the JSON. It can be
	// NEGATIVE (expired N days ago).
	DaysLeft *int `json:"days_left"`
	// RenewedAt is the last time the AUTOMATIC LOOP renewed this token
	// successfully — never a manual rotation by the owner
	// (RotateInstance never writes token_renovado_em, only
	// token_definido_em: see the migration comment in
	// internal/config/store.go). `null` until the FIRST successful automatic
	// renewal, even if the token itself already has days of life — that's
	// the distinction that answers "has the mechanism already proven it
	// works?".
	RenewedAt *string `json:"renewed_at"`
	// FailingSince is the FIRST attempt of the CURRENT run of failures —
	// `null` when the last attempt succeeded, or there was never an attempt.
	// Honest SINCE THE FIRST FAILURE: there is no threshold here that delays
	// this information (owner's decision, 2026-07-30 — the loop tries again
	// every tick with no escalation alarm at all, only the ALARME line in
	// the gateway's journal, which is operational and never reaches the
	// consumer).
	FailingSince *string `json:"failing_since"`
	// Instruction ONLY appears when Verdict is falhando or expirado — and
	// says, in Portuguese, THAT THE FIX IS MANUAL AND IS NOT THE CONSUMER'S.
	// See the type's comment, above.
	Instruction *string `json:"instruction"`
}

// InstructionIGTokenFailing and InstructionIGTokenExpired are the TWO
// possible texts of InstagramTokenInState.Instruction — CONSTANTS, not
// literals written twice (here and in the test that proves the field
// carries the instruction), so the two ends never diverge.
const (
	InstructionIGTokenFailing = "a renovacao automatica esta falhando; a resolucao e MANUAL, do lado de quem " +
		"opera o gateway ou e dono da conta Instagram na Meta — o token nao esta ao alcance deste consumidor"
	InstructionIGTokenExpired = "o token venceu e nao ha renovacao automatica possivel; e preciso um login " +
		"manual na Meta pelo dono da conta Instagram — o token nao esta ao alcance deste consumidor"
)

// IGRenewalFailureReader is what BuildState needs from the
// Instagram token renewer — the SAME structural reason as
// VerdictReader: the route reads the renewer running in the SERVER's
// process, with a history of several ticks.
//
// CAN BE nil: `zapgw estado` (cmd/zapgw/state.go) has no way to give the
// renewer a safe tick before reading — unlike the token's watchdog
// (CheckCredential is READ-only), a renewal attempt MUTATES the
// credential, and firing that as a side effect of a STATUS command would be
// a mutating surface hidden behind a read command. BuildStateWithSeries
// treats nil as "no known failure" — the CLI shows the token honestly
// through the DATABASE's timestamps (definido_em/expira_em/renovado_em,
// which are true in any process), just without a live `falhando_desde`.
type IGRenewalFailureReader interface {
	FailingSince(slug string) time.Time
}

// BuildState reads everything an instance's state contains — and it is
// the ONLY place that builds it.
//
// `now` comes in as a PARAMETER, and not from time.Now() in here, for the
// same reason as config.SummarizeCounters: both surfaces must be able to
// prove the same read at the SAME instant.
//
// A READ ERROR PROPAGATES, it never becomes a zeroed state: a
// "nunca_observado" or a zero counter returned because the database failed
// would be a lie wearing a fact's face — and it would land exactly on the
// value whoever reads it uses to NOT alarm. The caller distinguishes
// config.ErrInstanceNotFound (404 on the route, message with
// `zapgw instancia listar` on the CLI) from the rest (503 / error).
func BuildState(store *config.Store, watchdog VerdictReader, renewer IGRenewalFailureReader, ingress IngressSource, reach *ExternalProbe, leadership *Leadership, version, slug string, now time.Time) (State, error) {
	return BuildStateWithSeries(store, watchdog, renewer, ingress, reach, leadership, version, slug, now, config.ShortSeriesDays)
}

// BuildStateWithSeries is BuildState with the daily series size chosen by
// whoever is asking (T-081) — today only the route, which reads
// `?serie_dias=`.
//
// `zapgw estado` keeps calling BuildState and doesn't need to know a
// window exists: its screen doesn't print any series (see Series7Days).
// That's why the window came in through a NEW function instead of one more
// parameter on the old one — the alternative would force the CLI to choose,
// on every call, a number that isn't its business.
//
// WHOEVER CALLS IS WHOEVER VALIDATES: `days` outside the accepted range
// becomes `400` on the route, which is the one holding the consumer's
// request and knowing the ceiling in effect. Here an absurd value just
// produces a large series.
func BuildStateWithSeries(store *config.Store, watchdog VerdictReader, renewer IGRenewalFailureReader, ingress IngressSource, reach *ExternalProbe, leadership *Leadership, version, slug string, now time.Time, days int) (State, error) {
	// SummarizeInstance, not FindInstance: the state doesn't need any
	// secret, and InstanceSummary doesn't carry the plaintext fields.
	// Reading the whole instance would put the business's credentials in the
	// memory of a read that has no use for them.
	inst, err := store.SummarizeInstance(slug)
	if err != nil {
		return State{}, fmt.Errorf("resumir a instancia %q: %w", slug, err)
	}
	summary, err := store.SummarizeCountersWithSeries(slug, now, days)
	if err != nil {
		return State{}, fmt.Errorf("ler os contadores da instancia %q: %w", slug, err)
	}
	cert, err := store.CallbackCertificate(slug)
	if err != nil {
		return State{}, fmt.Errorf("ler o certificado do callback da instancia %q: %w", slug, err)
	}
	number, err := store.NumberAtMeta(slug)
	if err != nil {
		return State{}, fmt.Errorf("ler o numero na meta da instancia %q: %w", slug, err)
	}

	// failingSince ONLY exists when `renewer` is available (see the comment
	// on IGRenewalFailureReader: `zapgw estado` passes nil on purpose).
	var failingSince time.Time
	if renewer != nil {
		failingSince = renewer.FailingSince(slug)
	}

	e := State{
		Instance:            inst.Slug,
		Type:                inst.Type,
		IgID:                igIDInState(inst),
		State:               config.StateOf(inst),
		Paused:              !inst.Active,
		Version:             version,
		GeneratedAt:         now.UTC().Format(time.RFC3339),
		StampsSince:         inst.StampsSince,
		Counters:            make(map[string]CounterInState, len(config.KeysInDisplayOrder)),
		Series7Days:         daysInState(summary.ShortSeries),
		DailySeries:         daysInState(summary.Series),
		MetaToken:           metaTokenInState(inst.Type, watchdog.Read(slug)),
		CallbackCertificate: certificateInState(cert),
		NumberAtMeta:        numberAtMeta(number, inst.Type),
		InstagramToken:      instagramTokenInState(inst, failingSince, now),
		Ingress:             ingress.inState(),
		// reach.Read() is nil-safe (see the comment on ExternalProbe.Read): a
		// caller that didn't build the probe publishes `nao_configurado`,
		// never a made-up measurement.
		ExternalReach: reach.Read(),
		Leadership:    leadership.inState(),
	}
	// THE SINGLE SOURCE (T-039), iterated — not a list written here. Every
	// key of the vocabulary ALWAYS appears, even zeroed: a dashboard that
	// only received keys with traffic would have to guess whether the
	// absence is zero or a new field it doesn't know.
	for _, key := range config.KeysInDisplayOrder {
		c := CounterInState{
			Today:     summary.Today[key],
			Last7Days: summary.Last7Days[key],
		}
		if when, had := summary.LastEvent[key]; had {
			c.LastAt = stamp(when)
		}
		e.Counters[key] = c
	}
	// `entrada.ultimo_webhook_em` IS THE SAME TIMESTAMP as
	// `recebidas.ultimo_em`, copied AFTER the loop above and from the SAME
	// read — never a second timestamp with its own source (T-120 was
	// explicit: reuse, don't create). Two sources for the same fact diverge
	// on the first change, and this one would diverge on exactly the field
	// the consumer uses to conclude silence.
	e.Ingress.LastWebhookAt = e.Counters[config.CounterReceived].LastAt
	return e, nil
}

// daysInState translates a series from the database into the published
// series.
//
// IT IS A SINGLE FUNCTION FOR BOTH SERIES (T-081), not two loops: a loop per
// series would be the same rule's second copy, and the first change (a new
// key, a new field on the day) would land in one and not the other — which
// is literally the defect T-039 paid to fix in the counters.
//
// THE SERIES USES THE SAME KEY SOURCE as the `contadores` block, for the
// same reason: a new key has to appear in both places without anyone
// touching this. Every key of the vocabulary appears on EVERY day, even
// zeroed.
func daysInState(series []config.CounterDay) []DayInState {
	days := make([]DayInState, 0, len(series))
	for _, d := range series {
		n := make(map[string]int, len(config.KeysInDisplayOrder))
		for _, key := range config.KeysInDisplayOrder {
			n[key] = d.N[key]
		}
		days = append(days, dayInState(d.Day, n))
	}
	return days
}

// certificateInState translates the database's observation into the
// published block.
//
// THE TRANSLATION IS TOTAL, and it's what keeps an intermediate state from
// existing: either there are BOTH timestamps, or there is `nunca_observado`
// with both `null`. There is no path that produces "observado" without a
// date, nor a date without "observado" — the state isn't a field someone
// fills in on the side, it's a FUNCTION of what was observed.
func certificateInState(o config.CertificateObservation) CertificateInState {
	if !o.Observed() {
		return CertificateInState{State: CertNeverObserved}
	}
	return CertificateInState{
		State:      CertObserved,
		ExpiresAt:  stamp(o.ExpiresAt),
		ObservedAt: stamp(o.ObservedAt),
	}
}

// numberAtMeta translates what the database stores into the published
// block.
//
// THE TRANSLATION IS TOTAL, like the certificate's, and it's what keeps an
// intermediate state from existing: either there is a value WITH a
// timestamp AND WITH a source, or there is `nunca_observado` with all three
// `null`. The state isn't a field someone fills in on the side — it's a
// FUNCTION of what was observed, and that's why there's no path that
// produces "observado" without a value nor a value without "observado".
//
// TYPE COMES IN HERE, and not just in the caller (T-099): an `instagram`
// instance NEVER has quality nor tier — the watchdog doesn't even try to
// measure (it would have no phone_number_id to ask about), so there is no
// "not yet observed" that turns into "observado" one day. It's NotApplicable
// on both values, with `conferido_em` also null: asserting an attempt that
// never happened would be the same lie this file already refuses in other
// timestamps.
func numberAtMeta(n config.NumberAtMeta, kind string) NumberAtMeta {
	if kind == config.TypeInstagram {
		return NumberAtMeta{
			Quality:      ObservedValue{State: NotApplicable},
			MessageLimit: ObservedValue{State: NotApplicable},
		}
	}
	return NumberAtMeta{
		Quality:      observedValue(n.Quality),
		MessageLimit: observedValue(n.Limit),
		CheckedAt:    stamp(n.CheckedAt),
	}
}

// metaTokenInState decides what to publish in `token_meta` — what the
// watchdog measured, OR NotApplicable when the type makes its measurement
// meaningless (T-099, see the comment on State.MetaToken for the exact
// why).
func metaTokenInState(kind string, read MetaToken) MetaToken {
	if kind == config.TypeInstagram {
		return MetaToken{Verdict: NotApplicable}
	}
	return read
}

// igIDInState decides what to publish in `ig_id` — the recorded value,
// when the instance is Instagram, or NotApplicable, for the SAME reason as the
// other blocks in this file that depend on the type (numberAtMeta,
// metaTokenInState, instagramTokenInState): T-107.
func igIDInState(inst config.InstanceSummary) string {
	if inst.Type != config.TypeInstagram {
		return NotApplicable
	}
	return inst.IgID
}

func observedValue(v config.NumberValue) ObservedValue {
	if !v.Observed() {
		return ObservedValue{State: CertNeverObserved}
	}
	value, source := v.Value, v.Source
	return ObservedValue{
		State:      CertObserved,
		Value:      &value,
		ObservedAt: stamp(v.ObservedAt),
		Source:     &source,
	}
}

// instagramTokenInState builds the `token_instagram` block (T-098) from
// what the database stores (inst.TokenSetAt/TokenRenewedAt — see
// config.InstanceSummary) and what the renewer knows RIGHT NOW about the
// most recent attempt (failingSince, zero when there's no failure in
// progress).
//
// THE TRANSLATION IS TOTAL, like certificateInState/numberAtMeta: there is
// no path that produces a Verdict without the fields it implies, because
// the state isn't a field someone fills in on the side — it's a FUNCTION of
// what was observed.
func instagramTokenInState(inst config.InstanceSummary, failingSince, now time.Time) InstagramTokenInState {
	if inst.Type != config.TypeInstagram {
		// ABSENCE ASSERTED, never inferred: a WHATSAPP instance doesn't have
		// this deadline (a System User token doesn't expire in 60 days) — see
		// the comment on VerdictIGTokenNotApplicable.
		return InstagramTokenInState{Verdict: VerdictIGTokenNotApplicable}
	}

	setAt, err := time.Parse(time.RFC3339, inst.TokenSetAt)
	if err != nil {
		// Should only happen on a row corrupted by hand — see the equivalent
		// comment in instagram_renewer.go.checkOne. Without SetAt
		// there is no ExpiresAt nor DaysLeft to calculate; show what's
		// known (nothing) instead of inventing a date.
		return InstagramTokenInState{Verdict: VerdictIGTokenWaiting}
	}

	expiresAt := setAt.Add(InstagramTokenValidity)
	daysLeft := int(expiresAt.Sub(now).Hours() / 24)

	t := InstagramTokenInState{
		SetAt:     stamp(setAt),
		ExpiresAt: stamp(expiresAt),
		DaysLeft:  &daysLeft,
	}
	if inst.TokenRenewedAt != "" {
		if renewedAt, err := time.Parse(time.RFC3339, inst.TokenRenewedAt); err == nil {
			t.RenewedAt = stamp(renewedAt)
		}
	}
	if !failingSince.IsZero() {
		t.FailingSince = stamp(failingSince)
	}

	switch {
	case !now.Before(expiresAt):
		t.Verdict = VerdictIGTokenExpired
		instruction := InstructionIGTokenExpired
		t.Instruction = &instruction
	case !failingSince.IsZero():
		t.Verdict = VerdictIGTokenFailing
		instruction := InstructionIGTokenFailing
		t.Instruction = &instruction
	case t.RenewedAt != nil:
		t.Verdict = VerdictIGTokenOK
	default:
		t.Verdict = VerdictIGTokenWaiting
	}
	return t
}

// --- Text presentation ---------------------------------------------------

// tableTag marks the field the CLI shows in its own TABLE, outside the
// label/value list. See State.Counters.
const tableTag = "tabela"

// NoValue is what the CLI prints in place of a `null`/empty field.
//
// A DASH, AND NOT THE LINE OMITTED: a field missing from the screen is
// indistinguishable from "this version of the binary doesn't even have this
// field", and that's exactly the confusion T-065 exists to end. It's the
// same decision the JSON makes with an explicit `null` (see
// CounterInState.LastAt).
const NoValue = "—"

// StateRow is one line of the text presentation: a label, a value and
// the depth (0 = a field of State, 1 = a field of a nested block).
//
// An empty Value means "block header" — the `token_meta:` that precedes its
// fields.
type StateRow struct {
	Label string
	Value string
	Level int
}

// StateRows flattens State into label/value lines, in the ORDER the
// fields are declared.
//
// WHY REFLECTION, and it's this task's whole point: a field list written by
// hand in the CLI would be the SECOND copy — exactly the shape of defect
// T-039 paid to fix in the counters and T-065 paid to fix in the blocks.
// With reflection, adding a field to State makes it appear in the route's
// JSON AND on the CLI screen without editing either surface. That's the
// task's mandatory mutation, and it's the reason this loop has no `switch`
// by field name.
//
// THE LABEL IS THE JSON TAG, not the Go field name: this way the operator on
// the CT and the consumer reading the JSON say the SAME word — `token_meta`,
// not `MetaToken` — and whoever reads one's alarm doesn't need to translate
// it.
//
// THE CLOCK DOES NOT COME IN AS A PARAMETER HERE, and that's the opposite of
// what BuildState does — see printClock for the cost that bought
// the difference.
func StateRows(e State) []StateRow {
	return structRows(reflect.ValueOf(e), 0, printClock())
}

// printClock is the instant against which EVERY distance on this
// screen is measured: the now of WHOEVER IS PRINTING, never the instant
// that built the State.
//
// WHY IT IS NOT A PARAMETER, given that `now` is a parameter in
// BuildState and in config.SummarizeCounters. Because the two questions
// are different, and the caller has precisely the WRONG answer in hand:
//
//   - the CONTENT needs a chosen, shared instant ("both surfaces prove the
//     same read at the SAME instant"), and that's why it comes in as a
//     parameter;
//   - the DISTANCE answers "is this fresh?" for whoever is reading the
//     screen RIGHT NOW. It has no instant to share with anyone.
//
// `zapgw estado` had `now` in hand (the one that stamped `gerado_em`) and
// passed it in here — the most natural thing in the world, and wrong:
// between one and the other the CLI MEASURES the token on the Graph API (it
// measures before reading, because the watchdog's cache lives in the server's
// process), so `medido_em` is legitimately LATER than `gerado_em` and the
// screen would print "in" about a fact from the past. Measured against
// production on 2026-07-28 18:22, minutes after v0.25.0 went live:
//
//	gerado_em:      2026-07-28T18:22:31Z (ha 0s)
//	token_meta:
//	  medido_em:    2026-07-28T18:22:32Z (daqui a 0s)
//	  conferido_em: 2026-07-28T18:22:33Z (daqui a 1s)
//
// At 1s this is ugly; the number GROWS with how slow Meta is, which is
// exactly when someone is looking at this screen. A screen that announces
// the future about something that already happened trains you to distrust
// the rest of it — and it's the only instrument the person has at that
// moment.
//
// THE FIX IS NOT "if it comes out future, print 0s ago": a genuinely future
// timestamp exists and is legitimate (the certificate's `expira_em` comes
// out `(daqui a 54d)` and that's correct). The rule is different: **"in" is
// for what hasn't happened yet; an OBSERVATION timestamp is never in the
// future, and if it is, the reference clock is the one that's wrong.**
//
// It's a `var` only so the test can stop the clock (the same role as the
// `now func() time.Time` field on Watchdog and Entregador); production never
// swaps it.
var printClock = time.Now

func structRows(v reflect.Value, level int, now time.Time) []StateRow {
	var rows []StateRow
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() || field.Tag.Get("cli") == tableTag {
			continue
		}
		label := fieldLabel(field)
		value := v.Field(i)
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				rows = append(rows, StateRow{Label: label, Value: NoValue, Level: level})
				continue
			}
			value = value.Elem()
		}
		if value.Kind() == reflect.Struct {
			rows = append(rows, StateRow{Label: label, Level: level})
			rows = append(rows, structRows(value, level+1, now)...)
			continue
		}
		rows = append(rows, StateRow{Label: label, Value: ReadableValue(value, now), Level: level})
	}
	return rows
}

func fieldLabel(field reflect.StructField) string {
	tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if tag == "" || tag == "-" {
		return field.Name
	}
	return tag
}

// ReadableValue formats ONE value for the screen.
//
// EVERY TIMESTAMP COMES OUT IN UTC, RAW, with the time distance in
// parentheses. Both halves are a decision, and both cost something if
// swapped:
//
//   - RAW UTC because whoever is reading has `journalctl` open on the other
//     half of the screen, and the CT's journal is in UTC. Translating to
//     the house's timezone (-03) would force doing the math in your head, in
//     the middle of an incident, against every log line — and would make
//     the operator and the consumer (who reads the JSON, always UTC) cite
//     DIFFERENT CLOCKS about the same event, which is the same two-truths
//     disease this task exists to cure. The string is byte for byte the one
//     the route returns: you can copy it from one screen and paste it into
//     the other.
//   - THE DISTANCE ALONGSIDE it because the real question is never "what
//     time was it" — it's "is this fresh?" / "how much is left?".
//     `2026-07-28T14:03:11Z` alone doesn't answer either without a mental
//     subtraction, and a future date (the certificate's `expira_em`) is the
//     case where the mental subtraction is most likely to get ugly.
//
// Detection is by PARSE, not by field name: a new timestamp in State gets
// the distance for free, without anyone editing this function — the same
// rule that makes a new field appear on the screen by itself.
func ReadableValue(v reflect.Value, now time.Time) string {
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if s == "" {
			return NoValue
		}
		return textWithDistance(s, now)
	case reflect.Bool:
		// "sim"/"nao", not "true"/"false": the screen is for people, and
		// the JSON keeps the boolean for whoever alarms by rule.
		if v.Bool() {
			return "sim"
		}
		return "nao"
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// ReadableStamp formats a state timestamp (`*string`, `null` = never) the
// same way ReadableValue formats the ones inside State.
//
// It exists for the CLI's counters TABLE, which reads `ultimo_em` from
// inside the map and therefore doesn't go through reflection. Without this
// function, the CLI would have a SECOND timestamp-formatting rule — and "two
// rules for the same data" is literally the defect T-065 closed.
//
// It reads the SAME printClock as StateRows, and for the same
// reason: the table's `ultimo_em` is also an OBSERVATION timestamp, and the
// line above (`token_meta`) and the line below (`recebidas`) measuring
// distance against different reference clocks would be the same screen with
// two clocks.
func ReadableStamp(c *string) string {
	if c == nil || *c == "" {
		return NoValue
	}
	return textWithDistance(*c, printClock())
}

// textWithDistance appends the time distance to a text THAT IS an RFC3339
// timestamp; whatever isn't comes back untouched. The question is put to the
// PARSE, not to the field name, so a new timestamp gets the distance on its
// own.
func textWithDistance(s string, now time.Time) string {
	when, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return s + " (" + ReadableDistance(when, now) + ")"
}

// ReadableDistance says, in words, how much time separates `when` from
// `now` — backward ("ha 3min") or forward ("daqui a 63d").
//
// PRECISION DROPS WITH DISTANCE on purpose: "ha 3min" and "daqui a 63d"
// answer the question; "ha 3min 12s" and "daqui a 63d 4h 11min" make the
// reader spend attention on a digit that changes no decision at all.
func ReadableDistance(when, now time.Time) string {
	d := now.Sub(when)
	if d < 0 {
		return "daqui a " + readableDuration(-d)
	}
	return "ha " + readableDuration(d)
}

func readableDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dmin", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
