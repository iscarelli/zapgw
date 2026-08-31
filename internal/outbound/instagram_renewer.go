// The renewal loop for the Instagram long-lived token (T-098).
//
// WHY THIS EXISTS: an Instagram token is valid for 60 days. Without this
// loop, Instagram DM stops working 60 days after connecting, IN SILENCE —
// the symptom arrives as "consumer-b isn't answering Direct anymore", two
// months later, disconnected from the cause. It's the same family of defect
// as this project's silent `503`: something COULD observe and doesn't.
//
// Source, checked at developers.facebook.com on 2026-07-30 (the `Source:`
// block of T-098, docs/TASKS.md, and the endpoint's reference page cited in
// internal/meta/instagram.go):
//
//   - the long-lived token is valid for 60 days;
//   - it renews with a GET to graph.instagram.com/refresh_access_token,
//     valid starting from the moment the renewed token has AT LEAST 24h of
//     life;
//   - past 60 days without renewing, it expires and CANNOT be renewed
//     anymore — only a manual login on Meta.
//
// OWNER'S DECISION, 2026-07-30: this loop does NOT alarm through its own
// channel (no notification, no escalation, no queue) — "the consumer is the
// one who'll alarm, because they already have a channel to talk to me."
// This task's main product is the STATE in GET /v1/estado (state.go),
// honest FROM THE FIRST failure: verdict, falhando_desde, when the token
// expires, and how many days are left. The `ALARME` line in the journal
// STAYS — and ONLY that: one log line, by this project's usual convention
// (ALARME = needs a person), with no escalation threshold, no second
// mechanism.
//
// SAME MOLD AS Watchdog (watchdog.go): a timer measures/acts per instance,
// always, independent of traffic and of anyone watching; reading the state
// (FailingSince) answers ONLY from the in-memory cache, never triggers a
// call to Meta.
package outbound

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// InstagramTokenValidity is the TOTAL validity of an Instagram long-lived
// token, counted from when it was set (creation, registration, rotation, or
// renewal itself — see internal/config/store.go,
// Instance.TokenSetAt). Source in this file's header.
const InstagramTokenValidity = 60 * 24 * time.Hour

// MinAgeToRenewIGToken is Meta's pre-condition: a token with LESS
// life than this is refused if we try to renew it. Source in the header.
//
// IN PRACTICE THIS GUARD IS DOMINATED by DaysToRenewIGToken (below,
// which is worth MUCH more than 24h) — a token only gets close to the
// renewal loop after having dozens of days of life. It stays here,
// explicit, because the pre-condition is META's, not a decision of ours: if
// DaysToRenewIGToken ever changes to a small number, this guard is what
// keeps the loop from hitting a predictable refusal from Meta. Proved in
// isolation in TestDecideIGTokenRenewal (pure function, without
// depending on the other threshold to be exercised).
const MinAgeToRenewIGToken = 24 * time.Hour

// DaysToRenewIGToken is the minimum margin of REMAINING life before the
// loop starts TRYING to renew — from the moment the token reaches this age
// (in days), every tick tries.
//
// WHY 30, AND NOT AT THE LIMIT (day 59): renewing early is FREE — the
// validity goes back to a full 60 days from the renewal, so renewing early
// wastes nothing, it just costs one extra HTTP call per month. What this
// number really defines isn't WHEN it renews, it's HOW MUCH MARGIN is LEFT
// if the renewal fails: renewing at 30 days of life leaves 30 days of
// margin for someone to act before the final expiry (after which no more
// renewal is possible — only manual login). And the margin exists to cover
// an UNSEEN failure, not just a technical one: in this project a missed
// alarm is the common failure mode (three of the four monitors lied on a
// single day, 2026-07-30 — docs/ARMADILHAS.md). That's why it doesn't go
// below 30.
//
// EXPORTED and NAMED in a SINGLE place on purpose (owner's explicit
// request, 2026-07-30): the number is revisable — he himself considered 45
// before settling on 30 — and a literal scattered across the loop and the
// tests would force finding every occurrence at the time of a change. The
// boundary tests, despite this, use a HAND-WRITTEN LITERAL DURATION (never
// this constant) — a test that derives the expected value from the SAME
// constant it's supposed to watch proves nothing (docs/ARMADILHAS.md,
// "Testes", found in T-095).
const DaysToRenewIGToken = 30

// igTokenRenewalDecision is what decideIGTokenRenewal concludes from a
// token's AGE — FOUR distinct outcomes, not two, because "don't try" has
// two VERY different causes (token too young vs. not yet time) that a
// `bool` would collapse.
type igTokenRenewalDecision int

const (
	// decisionWait: valid token, still far from the threshold — nothing
	// to do, and SILENTLY (not even a log): it's the normal state for most
	// of the token's life.
	decisionWait igTokenRenewalDecision = iota
	// decisionTokenTooYoung: the token has less than
	// MinAgeToRenewIGToken of life — Meta would refuse, and the
	// loop doesn't even try.
	decisionTokenTooYoung
	// decisionRenew: within the window of the last DaysToRenewIGToken
	// days of validity — tries to renew.
	decisionRenew
	// decisionExpired: past InstagramTokenValidity without renewing. No
	// renewal is possible (source in the header) — the loop does NOT try,
	// only records.
	decisionExpired
)

// decideIGTokenRenewal is a PURE FUNCTION on purpose: it doesn't talk to
// Meta, doesn't read the store, and doesn't read the clock — it only
// classifies an AGE already computed. That makes it testable in ISOLATION,
// including case (c) of the T-098 Verify (token with less than 24h), which
// in practice never shows up past the decisionWait guard (a token with
// less than 24h of life ALWAYS also has less than DaysToRenewIGToken
// days) — without this separate function, case (c) would only be
// observable end-to-end, and a future DaysToRenewIGToken would never
// let the test reach that branch.
func decideIGTokenRenewal(age time.Duration) igTokenRenewalDecision {
	switch {
	case age >= InstagramTokenValidity:
		return decisionExpired
	case age < MinAgeToRenewIGToken:
		return decisionTokenTooYoung
	case age >= time.Duration(DaysToRenewIGToken)*24*time.Hour:
		return decisionRenew
	default:
		return decisionWait
	}
}

// igRenewerInterval is how often each tipo=instagram instance is
// checked — SAME order of magnitude as the purge loops (cmd/zapgw/main.go,
// startPeriodicPurge): the number has no measurement behind it, just the
// need to be MUCH smaller than the 30-day window in which the loop tries,
// so a transient failure (network, a `5xx` from Meta) has several chances
// to correct itself before expiry.
const igRenewerInterval = time.Hour

// InstagramRenewer measures and renews, at its own pace, the token of
// each tipo=instagram instance — this project's counterpart to Watchdog
// (watchdog.go), but for the INSTAGRAM token instead of the WhatsApp
// acceptance verdict.
type InstagramRenewer struct {
	store       *config.Store
	client      *meta.Client
	renewalBase string

	interval time.Duration
	// now is injectable only for testing — without it, proving a token's
	// AGE would require waiting real days. SAME role as the field of the
	// same name in Watchdog.
	now func() time.Time
	// persist is the ONLY write path used by checkOne — by DEFAULT
	// store.RenewInstagramTokenAt, swappable only in tests. It exists as
	// a field, and not as a direct call to the store, so the T-098 Verify
	// (d) (write fails after Meta returns a new token) can simulate the
	// failure WITHOUT corrupting a real test database — the same injection
	// technique that `now`, above, already uses for the clock.
	persist func(slug, newToken string, now time.Time) error

	// mu protects falhas: the timer writes in one goroutine, and GET
	// /v1/estado reads in another (one per request) — SAME discipline as
	// Watchdog.measurements.
	mu sync.Mutex
	// falhas holds, per slug, the FIRST attempt of the CURRENT run of
	// failures — zero (absent from the map) when the last attempt
	// succeeded, or there was never an attempt. It's what GET /v1/estado
	// shows as `falhando_desde`, honestly, FROM the first failure — there's
	// no threshold that delays this information (owner's decision: the
	// loop doesn't escalate, it only records).
	failures map[string]time.Time

	// work is what Start calls on every tick — BY DEFAULT rv.Check
	// (see NewInstagramRenewer), swappable only in tests (T-115). SAME
	// role as the field of the same name in Watchdog, and for the same
	// reason: proving that Start's recover lets the SECOND round happen
	// after a panic, without having to make Check itself panic.
	work func(context.Context)
}

// NewInstagramRenewer builds the renewer INERT — it only starts
// measuring in Start. SAME separation as NewWatchdog, and for the SAME
// reason: the test runs one tick at a time, without racing against a real
// timer.
//
// `renewalBase` is the root of the renewal endpoint (production:
// meta.DefaultInstagramRenewalBase) — injectable to point at a fake server
// in tests, NEVER at the real Meta (the same rule as meta.NewClient and
// graphBase, cmd/zapgw/main.go).
func NewInstagramRenewer(store *config.Store, client *meta.Client, renewalBase string) *InstagramRenewer {
	rv := &InstagramRenewer{
		store:       store,
		client:      client,
		renewalBase: renewalBase,
		interval:    igRenewerInterval,
		now:         time.Now,
		failures:    map[string]time.Time{},
	}
	rv.persist = store.RenewInstagramTokenAt
	rv.work = rv.Check
	return rv
}

// Start brings up the timer goroutine — SAME design as Watchdog.Start,
// including the recover: without it, a panic inside the loop kills the
// goroutine and the renewer stops measuring IN SILENCE for the rest of the
// process's life, and an Instagram instance's token would expire without
// anything trying to fix it.
func (rv *InstagramRenewer) Start() {
	go func() {
		for {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("zapgw: renovador do token instagram sofreu panico (recuperado): %v", rec)
					}
				}()
				rv.work(context.Background())
			}()
			time.Sleep(rv.interval)
		}
	}()
}

// Check runs ONE tick: for EACH tipo=instagram instance, decides
// whether it's time to renew and acts.
//
// DOES NOT FILTER BY ACTIVE, unlike Watchdog.Check — and the difference is
// deliberate, not an oversight. Pausing is reversible (this gateway's
// emergency button, PauseInstance); an Instagram token's expiry is NOT —
// an instance paused today can be reactivated tomorrow, and a token that
// expired IN SILENCE while it was paused would reproduce exactly the defect
// this task exists to close, only behind a state that looks "deliberate"
// and is therefore even harder to notice.
func (rv *InstagramRenewer) Check(ctx context.Context) {
	instances, err := rv.store.ListInstances()
	if err != nil {
		// Just logs. The renewer never brings anything down: it's
		// monitoring, and the next tick tries again — SAME rule as
		// Watchdog.Check.
		log.Printf("zapgw: renovador do token instagram nao conseguiu listar instancias: %v", err)
		return
	}
	for _, r := range instances {
		if r.Type != config.TypeInstagram {
			continue // T-098, Verify (f): a whatsapp instance NEVER enters this loop
		}
		rv.checkOne(ctx, r.Slug)
	}
}

// checkOne runs ONE tick for ONE instance.
func (rv *InstagramRenewer) checkOne(ctx context.Context, slug string) {
	inst, err := rv.store.FindInstance(slug)
	if err != nil {
		log.Printf("zapgw: renovador do token instagram nao conseguiu ler a instancia %q: %v", slug, err)
		return
	}

	// Second guard, not redundancy: Check already filters by type, but
	// checkOne can be called directly (a test, or a future second
	// caller) — the same defense-in-depth discipline as
	// Store.RenewInstagramTokenAt (`AND tipo = ?`).
	if inst.Type != config.TypeInstagram {
		return
	}

	setAt, errParse := time.Parse(time.RFC3339, inst.TokenSetAt)
	if errParse != nil {
		// Should only happen with a row corrupted by hand: every write of
		// token_envio (creation, registration, rotation, renewal) writes
		// token_definido_em in the SAME move (T-098). Without it there's no
		// way to compute the token's age, so the loop can't decide
		// anything — it records and waits for a person.
		log.Printf("ALARME zapgw: a instancia instagram %q nao tem token_definido_em legivel (%q) — "+
			"o gateway nao sabe a idade do token e nao pode decidir se e hora de renovar; confira manualmente",
			slug, inst.TokenSetAt)
		return
	}

	now := rv.now()
	age := now.Sub(setAt)
	daysLeft := int((InstagramTokenValidity - age).Hours() / 24)

	switch decideIGTokenRenewal(age) {
	case decisionWait, decisionTokenTooYoung:
		return // normal path — not even a log
	case decisionExpired:
		// DOES NOT TRY: the source (this file's header) is explicit that
		// past the deadline no renewal is possible, only manual login —
		// inventing an attempt here would just spend a call on the SAME
		// predictable refusal, and T-098 Do(6) forbids inventing
		// re-authentication.
		log.Printf("ALARME zapgw: o token do instagram da instancia %q EXPIROU ha %d dia(s) "+
			"(definido em %s) — passou de %s sem renovar, e a Meta NAO permite mais renovar; "+
			"e preciso um login manual na Meta, pelo dono da conta Instagram",
			slug, -daysLeft, setAt.UTC().Format(time.RFC3339), InstagramTokenValidity)
		return
	}

	// decisionRenew: tries.
	newToken, err := rv.client.RenewInstagramToken(ctx, rv.renewalBase, inst.SendToken)
	if err != nil {
		rv.markFailure(slug, now)
		log.Printf("ALARME zapgw: a Meta recusou renovar o token do instagram da instancia %q "+
			"(faltam %d dia(s) para expirar) — %v", slug, daysLeft, err)
		return
	}

	// 🔴 PERSISTS BEFORE CONSIDERING IT RENEWED (T-098, Do 4): if the write
	// fails here, the gateway KEEPS the old token (which Meta still
	// accepts — it was only judged mature enough to renew, not revoked)
	// and CANNOT mark it as renewed. The next tick tries again with the
	// SAME old token, because nothing in this branch overwrote it.
	if err := rv.persist(slug, newToken, now); err != nil {
		rv.markFailure(slug, now)
		log.Printf("ALARME zapgw: a Meta devolveu um token NOVO para a instancia %q mas a GRAVACAO falhou (%v) "+
			"— o gateway continua com o token antigo, que ainda vale por %d dia(s); tentara renovar de novo no proximo ciclo",
			slug, err, daysLeft)
		return
	}

	rv.clearFailure(slug)
	log.Printf("zapgw: token do instagram da instancia %q renovado — validade reiniciada por %s a partir de agora",
		slug, InstagramTokenValidity)
}

func (rv *InstagramRenewer) markFailure(slug string, now time.Time) {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	// Only the FIRST failure of the run gets recorded — later ones can't
	// push the date forward, otherwise `falhando_desde` would say "failing
	// for one tick" after days of failing. SAME rule as Watchdog.record.
	if rv.failures[slug].IsZero() {
		rv.failures[slug] = now
	}
}

func (rv *InstagramRenewer) clearFailure(slug string) {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	delete(rv.failures, slug)
}

// FailingSince returns the FIRST attempt of instance `slug`'s CURRENT run
// of failures — zero when the last attempt succeeded, or there was never an
// attempt. It's what GET /v1/estado (state.go, via
// IGRenewalFailureReader) publishes as `falhando_desde`, ALWAYS from the
// cache, never triggering a call to Meta — SAME discipline as Watchdog.Read.
func (rv *InstagramRenewer) FailingSince(slug string) time.Time {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	return rv.failures[slug]
}
