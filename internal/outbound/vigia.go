// The watcher for the Meta token — the SENSOR behind the verdict that
// GET /v1/estado publishes (T-060).
//
// WHY A TIMER, AND NOT "check when someone asks" NOR "derive from
// traffic". Both alternatives are cheaper and both are blind exactly when
// it matters:
//
//   - DERIVING FROM TRAFFIC (each successful send proves the token is
//     valid) makes "absence of traffic" indistinguishable from "system
//     broken" — and it's in silence that a break goes unnoticed the
//     longest. Token revoked at 2am, nobody sends anything, and the
//     discovery happens at 8am in front of the first client. The owner's
//     line (2026-07-28): "we can't depend on a message, otherwise it dies
//     silently".
//   - CHECKING ON READ (lazy) has the same flaw in different clothes: if
//     nobody opens the dashboard, nobody checks, and the system goes blind
//     exactly when it's quiet. Plus it hangs the consumer's dashboard on
//     Meta's latency and uptime, and makes "Meta is down" look like
//     "gateway has a problem".
//
// That's why the order is: the timer measures, always, per ACTIVE
// instance, independent of traffic and of anyone watching; the consumer's
// read answers ONLY from cache and never triggers a call to Meta.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO, and why: real traffic (an
// accepted send, a checked signature) COULD refresh the verdict for free
// and skip the next tick. That is CALL REDUCTION, never a source of truth
// — and it wasn't implemented because, with the timer already up, the gain
// is one read call every five minutes per instance, against the cost of
// spreading the watcher through the send path. If this is ever done, it
// has to be ONLY in the "make it fresher" direction: nothing in traffic
// can indefinitely postpone a tick, otherwise the first paragraph applies
// again.
//
// THIS FILE IS NOT THE PROBE. GET /v1/instances/{slug}/health
// (saude_handler.go) stays uncached, on purpose: whoever calls a probe
// chooses the frequency and wants the truth NOW. Here it's the opposite —
// the consumer paints the dashboard at their own frequency and we talk to
// Meta at ours.
package outbound

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// The THREE verdicts. `desconhecido` is NOT new vocabulary: it's the same
// word, with the same meaning, from the send error taxonomy
// (meta.ClassUnknown) — "we don't know" is an answer, and disguising
// it as either of the other two is what causes harm. A second vocabulary
// for the same concept would force the consumer to learn two tables.
const (
	// VerdictOK: Meta responded accepting this instance's credential.
	VerdictOK = "ok"
	// VerdictRefused: the credential does NOT work and only a human can
	// fix it. Covers two definitive outcomes, because the ACTION for
	// whoever reads it is the same in both: Meta responded rejecting it
	// (401/403, or a permanent 4xx), or the registered phone_number_id is
	// invalid and the call never left here.
	VerdictRefused = "recusado"
	// VerdictUnknown: there is no valid measurement right now —
	// either there never was one, or the last one aged out (see
	// verdictValidity).
	VerdictUnknown = string(meta.ClassUnknown)
)

// watchdogInterval is how often each ACTIVE instance is checked.
//
// THE NUMBER HAS NO MEASUREMENT BEHIND IT and is revisable with the first
// real observation; what's not negotiable is the timer existing. Five
// minutes per active instance gives ~288 calls/day each — irrelevant to
// the Graph API. `GET /{phone-number-id}` is a READ: it doesn't create a
// conversation and doesn't consume the 250-conversations/day quota (which
// counts conversations started with unique users, a different mechanism
// from the call rate limit). That endpoint's rate-limit threshold was NOT
// verified at the source, so no number is asserted here about it.
const watchdogInterval = 5 * time.Minute

// verdictValidity is how long a measured verdict keeps being presented.
// Past that it degrades to `desconhecido`.
//
// WHY IT EXPIRES: a cache that never expires is a lie with a timestamp on
// it. A `{"veredito":"ok","medido_em":"15:20"}` that never ages is
// AMBIGUOUS between two opposite states — "I checked at 15:20 and didn't
// need to check again" and "I checked at 15:20 and EVERY attempt since
// then has failed". In the second case the consumer's dashboard paints
// green while Meta is down, which is exactly the opposite of what this
// route exists to give.
//
// THREE TICKS, and the choice is deliberate: one missed tick is normal
// noise (a ten-second network blip shouldn't change the token's state and
// shouldn't paint the dashboard yellow); three ticks failing in a row is
// already a real problem. The number is ours and has no measurement behind
// it — but its relationship to the interval does: SMALLER than the
// interval, every verdict would be born expired; EQUAL to it, any one-tick
// delay would turn into an alarm.
const verdictValidity = 3 * watchdogInterval

// measurement is the state stored per instance. It doesn't leave this
// package: whoever reads it from outside gets a MetaToken, with expiration
// already applied.
type measurement struct {
	verdict string
	// measuredAt is the last time Meta RESPONDED (accepting or rejecting) —
	// zero when it never responded.
	measuredAt time.Time
	// checkedAt is the last ATTEMPT, successful or not.
	checkedAt time.Time
	// failingSince is the FIRST attempt of the current failure streak —
	// zero when the last attempt produced a verdict.
	failingSince time.Time
}

// MetaToken is the block published in GET /v1/estado.
//
// TWO TIMESTAMPS, AND THEY ANSWER DIFFERENT QUESTIONS — and the divergence
// between them IS the signal:
//
//   - MeasuredAt     — when Meta last responded;
//   - CheckedAt  — when we last tried.
//
// Equal (within a tick), the check is healthy. DIVERGING, the check is
// failing — and the consumer sees that without knowing anything about our
// implementation.
//
// EVERY time field is a pointer, with no omitempty, on purpose: an
// explicit `null` says "never happened", and the key ALWAYS exists. A
// field that disappears from the JSON would force the consumer's
// dashboard to distinguish "absent" from "null" to answer the same
// question.
//
// THE ALARM RULE THIS GIVES THE CONSUMER, and it doesn't require knowing
// our volume or our internals: `veredito` different from `ok`, or
// `conferido_em` gone stale.
type MetaToken struct {
	Verdict           string  `json:"veredito"`
	MeasuredAt        *string `json:"medido_em"`
	CheckedAt         *string `json:"conferido_em"`
	CheckFailingSince *string `json:"checagem_falhando_desde"`
}

// Watchdog measures the token verdict of each active instance, at its own pace.
type Watchdog struct {
	store  *config.Store
	client *meta.Client
	// number is the writer for the NUMBER observation (quality and
	// messaging limit, T-080). It is NOT a second cycle: the read happens
	// on the SAME tick, for the SAME active instance, inside checkOne —
	// the task forbade creating a new polling loop, and creating one would
	// mean paying twice for the same discipline (paused instance,
	// per-instance deadline, recover).
	number *config.NumberObserver

	interval time.Duration
	validity time.Duration
	// now is injectable only for tests — without it, proving that a
	// stale `ok` expires would require sleeping fifteen minutes.
	now func() time.Time

	// mu protects measurements: the timer writes from one goroutine and the
	// HTTP handler reads from another, one per request. This project
	// already paid a Critical for an unlocked counter in a shared handler
	// (docs/ARMADILHAS.md, "Go / concorrência").
	mu           sync.Mutex
	measurements map[string]measurement

	// work is what Start calls on every tick — BY DEFAULT v.Check
	// (see NewWatchdog), swappable only in tests (T-115). Without this
	// field, proving that Start's recover really lets the SECOND round
	// happen after a panic would require making the real Check panic —
	// contaminating the rest of the suite just to reach a recovery branch.
	// Same role as `now`, above: injection only for tests, production
	// always uses the default.
	work func(context.Context)
}

// NewWatchdog builds the watcher INERT: it only starts measuring in
// Start. Separating the two is what lets the test run one tick at a
// time, instead of racing against a real clock.
func NewWatchdog(store *config.Store, client *meta.Client) *Watchdog {
	v := &Watchdog{
		store:        store,
		client:       client,
		number:       config.NewNumberObserver(store),
		interval:     watchdogInterval,
		validity:     verdictValidity,
		now:          time.Now,
		measurements: map[string]measurement{},
	}
	v.work = v.Check
	return v
}

// Start starts the timer goroutine: one tick RIGHT AWAY (so the
// dashboard is worth something seconds after boot, not only five minutes
// later) and one every interval, forever.
//
// The recover is NOT ceremony: without it, a panic inside the loop kills
// the goroutine and the watcher stops measuring IN SILENCE for the rest of
// the process's life — and the consumer's dashboard would keep showing the
// last verdict until it expires, with nobody knowing why. It's the same
// mold as startPeriodicPurge (cmd/zapgw/main.go), for the same reason.
func (v *Watchdog) Start() {
	go func() {
		for {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("zapgw: vigia do token sofreu panico (recuperado): %v", rec)
					}
				}()
				v.work(context.Background())
			}()
			time.Sleep(v.interval)
		}
	}()
}

// Check runs ONE tick: it asks Meta, for each ACTIVE instance, whether
// it still accepts that instance's token.
//
// A PAUSED instance is not checked — it doesn't send, so spending a call
// for it would be measuring a channel that can't fail. Its verdict expires
// on its own to `desconhecido`, which is the truth: nobody is measuring it.
func (v *Watchdog) Check(ctx context.Context) {
	instances, err := v.store.ListInstances()
	if err != nil {
		// Just logs. The watcher never brings anything down: it's
		// monitoring, and the next tick tries again.
		log.Printf("zapgw: vigia do token nao conseguiu listar instancias: %v", err)
		return
	}
	for _, r := range instances {
		if !r.Active {
			continue
		}
		v.checkOne(ctx, r.Slug)
	}
}

// CheckInstance runs ONE tick for ONE instance.
//
// WHO NEEDS THIS IS `zapgw estado` (cmd/zapgw/estado.go), and the reason
// is structural: the watcher keeps its measurements in MEMORY, in the
// server's process — a command-line process that just started has an
// EMPTY cache, and would read `desconhecido` for everything, always.
// "Unknown forever" on the screen of someone in the middle of an incident
// is worse than not showing the block at all: it looks like a broken
// watcher. The CLI therefore MEASURES before reading, with this method,
// for the same reason the probe (saude_handler.go) has no cache: whoever
// is in front of the terminal wants the truth NOW, and chose the
// frequency by pressing Enter.
//
// ONLY ONE INSTANCE, and not Check(): with `--slug`, spending a Graph
// API call on the OTHER instances would mean consuming one tenant's quota
// to answer a question about another.
func (v *Watchdog) CheckInstance(ctx context.Context, slug string) {
	v.checkOne(ctx, slug)
}

func (v *Watchdog) checkOne(ctx context.Context, slug string) {
	inst, err := v.store.FindInstance(slug)
	if err != nil {
		log.Printf("zapgw: vigia do token nao conseguiu ler a instancia %q: %v", slug, err)
		v.record(slug, err)
		return
	}
	// The deadline is the INSTANCE's, the same account as sending and the
	// probe: without it a destination that hangs would hold the watcher's
	// goroutine forever, and the watcher is ONE for all instances — one
	// stuck would stop the others from being measured.
	ctx, cancel := context.WithTimeout(ctx, InstanceDeadline(inst))
	defer cancel()

	// ONE call on the happy path. It asks, in the same GET that always
	// checked the credential, for the two number fields (T-080) — that's
	// why it REPLACES CheckCredential here instead of adding to it: a
	// second call per tick would double the read traffic forever just to
	// avoid thinking through this paragraph.
	obs, err := v.client.ObserveNumber(ctx, inst.PhoneNumberID, inst.SendToken)

	// 🔴 THE GUARD THAT PAYS FOR THE DESIGN: `recusado` NEVER comes out of
	// a call that used `fields=`.
	//
	// A `fields=` with a field the Graph API doesn't know answers
	// 400/code 100 — and in our taxonomy 4xx is ClassPermanent, which
	// definitiveOutcome treats as "only a human can fix it". Without this
	// guard, the day Meta retires
	// `whatsapp_business_manager_messaging_limit` (it ALREADY retired the
	// earlier `messaging_limit_tier` — see internal/meta/numero.go) would
	// paint the token of every active instance RED, and the consumer's
	// dashboard would send people looking for a revoked credential that
	// was never revoked.
	//
	// Before declaring `recusado`, the watcher RECONFIRMS with the clean
	// GET, without `fields=` — the same thing it always did. If the clean
	// one passes, the credential is fine and what got rejected was our
	// field request; the verdict comes out `ok` and the defect becomes a
	// log line instead of a false alarm.
	//
	// THE COST IS ZERO ON THE HAPPY PATH (the reconfirmation only runs
	// after a definitive failure already happened) and one extra call per
	// tick on an instance that's already broken — which is exactly where
	// it's worth it.
	if err != nil && definitiveOutcome(err) {
		if clean := v.client.CheckCredential(ctx, inst.PhoneNumberID, inst.SendToken); clean == nil {
			log.Printf("ALARME zapgw: a Graph API recusou a leitura dos campos do numero da instancia %q "+
				"(%v), mas ACEITOU a credencial — o gateway parou de saber qualidade e limite de mensagens; "+
				"confira os nomes dos campos em internal/meta/numero.go contra a doc da Meta", slug, err)
			err = nil
		}
	}

	v.record(slug, err)

	// THE OBSERVATION IS MONITORING AND NEVER CHANGES THE VERDICT: it's
	// written AFTER record, and Register returns no error by signature
	// (config.NumberObserver) — the same discipline as the counter in
	// inbound.
	//
	// THE ATTEMPT IS ALWAYS STAMPED, with a value or without — when the
	// call fails, `obs` is empty and the UPSERT only moves
	// `conferido_em`. That's what makes `conferido_em` advance while
	// `observado_em` stays put, which is the signal for "the measurement
	// is going back and forth without bringing anything back". Registering
	// only on success would make "the measurement is broken"
	// indistinguishable from "nobody measured", which is exactly the
	// ambiguity the two timestamps exist to close — and it's the same rule
	// token_meta.conferido_em already follows.
	v.number.Record(slug, config.NumberUpdate{
		Quality: obs.Quality,
		Limit:   obs.Limit,
		Source:  config.SourceMeasurement,
		When:    v.now(),
	})
}

// record applies the result of ONE attempt.
//
// THE ASYMMETRY, and it's the same as sending
// (internal/outbound/handler.go): only a DEFINITIVE outcome becomes a
// verdict. "Meta didn't respond" is not a rejection — it's a check
// failure, and treating it as a rejection would turn a network blip into a
// credential alarm, sending people to look for a defect that isn't there.
func (v *Watchdog) record(slug string, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.now()
	m := v.measurements[slug]
	m.checkedAt = now

	switch {
	case err == nil:
		m.verdict, m.measuredAt, m.failingSince = VerdictOK, now, time.Time{}
	case definitiveOutcome(err):
		m.verdict, m.measuredAt, m.failingSince = VerdictRefused, now, time.Time{}
	default:
		// Attempt that produced no verdict: the previous one stays in
		// place (and ages on its own in Read), and the failure streak
		// starts counting. Only the FIRST failure of the streak writes
		// failingSince — the following ones can't push the date forward,
		// otherwise it would say "failing for a minute" after an hour of
		// failing.
		if m.failingSince.IsZero() {
			m.failingSince = now
		}
	}
	v.measurements[slug] = m
}

// definitiveOutcome reports whether the error PROVES the credential
// doesn't work.
//
//   - *meta.MetaError of class config (401/403) — Meta rejected the token;
//   - *meta.MetaError of class permanente (remaining 4xx) — it responded
//     and retrying repeats the same error; in both cases only a human can
//     fix it;
//   - ErrInvalidPhoneNumberID — the call never left here, and no send
//     from this instance works until an admin fixes the registration.
//
// RETENTAVEL class (429, Meta's 5xx) is deliberately left OUT: it says the
// problem is Meta's or the rate's, not the credential's.
func definitiveOutcome(err error) bool {
	if errors.Is(err, meta.ErrInvalidPhoneNumberID) {
		return true
	}
	var me *meta.MetaError
	if errors.As(err, &me) {
		return me.Class == meta.ClassConfig || me.Class == meta.ClassPermanent
	}
	return false
}

// Read returns the `token_meta` block for an instance — ALWAYS from cache,
// never talking to Meta.
//
// This is where a stale verdict EXPIRES. Expiration applies to `ok` and to
// `recusado` for the same reason: the verdict describes NOW, and an old
// measurement doesn't describe now. In practice it almost only ever bites
// `ok`, because a rejected instance keeps being checked every tick and
// `recusado` arrives fresh every time — and both sides alarm the same way
// for the consumer (`veredito != "ok"`).
//
// `medido_em` keeps pointing to Meta's last REAL response even after the
// verdict expires: that's what tells the consumer how long the gateway
// hasn't heard from Meta, information that zeroing the field would destroy.
func (v *Watchdog) Read(slug string) MetaToken {
	v.mu.Lock()
	m, exists := v.measurements[slug]
	v.mu.Unlock()

	r := MetaToken{Verdict: VerdictUnknown}
	if !exists {
		return r
	}
	r.MeasuredAt = stamp(m.measuredAt)
	r.CheckedAt = stamp(m.checkedAt)
	r.CheckFailingSince = stamp(m.failingSince)

	if !m.measuredAt.IsZero() && v.now().Sub(m.measuredAt) <= v.validity {
		r.Verdict = m.verdict
	}
	return r
}

// stamp formats an instant in RFC3339 UTC, or returns nil for the zero
// value — "never happened" is `null`, never a year-1 date (which a
// dashboard would print as if it were a real measurement).
func stamp(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
