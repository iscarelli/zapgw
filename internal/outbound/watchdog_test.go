package outbound

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/meta"
)

// testWatchdog builds the watcher with a STOPPED, injectable clock.
// Without this, proving that a stale `ok` expires would require sleeping
// fifteen minutes — and a test that sleeps is a test nobody runs.
func testWatchdog(t *testing.T, m *fakeHealthMeta, active ...string) (*Watchdog, *fakeClock) {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	srv := m.server(t)
	v := NewWatchdog(store, meta.NewClient(srv.Client(), srv.URL))
	clock := &fakeClock{now: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)}
	v.now = clock.read
	return v, clock
}

// fakeClock is a clock that only moves when the test tells it to.
// Protected by a mutex because the watcher reads the clock from inside
// record, which runs under its own lock — and a concurrent test would
// read both at the same time.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (t *fakeClock) read() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now
}

func (t *fakeClock) advance(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = t.now.Add(d)
}

// The happy path: Meta accepts the token, and BOTH timestamps come out
// equal — and the equality is the signal that "the check is healthy".
func TestWatchdogMeasuresOKWithBothStampsEqual(t *testing.T) {
	m := tokenAcceptingMeta()
	v, clock := testWatchdog(t, m, "lojinha")

	v.Check(context.Background())

	r := v.Read("lojinha")
	if r.Verdict != VerdictOK {
		t.Fatalf("verdict = %q, want %q", r.Verdict, VerdictOK)
	}
	want := clock.read().Format(time.RFC3339)
	if r.MeasuredAt == nil || *r.MeasuredAt != want {
		t.Errorf("measured_at = %v, want %q", r.MeasuredAt, want)
	}
	if r.CheckedAt == nil || *r.CheckedAt != want {
		t.Errorf("checked_at = %v, want %q", r.CheckedAt, want)
	}
	if r.CheckFailingSince != nil {
		t.Errorf("check_failing_since = %v, want null on a check that succeeded", *r.CheckFailingSince)
	}
}

// An instance the watcher never measured answers `desconhecido` with
// EVERYTHING null — not `ok` nor `recusado`. A verdict made up at boot
// would be worse than none: the dashboard would paint green before the
// gateway ever talked to Meta once.
func TestWatchdogNeverMeasuredIsUnknownWithNullStamps(t *testing.T) {
	m := tokenAcceptingMeta()
	v, _ := testWatchdog(t, m, "lojinha")

	r := v.Read("lojinha")
	if r.Verdict != VerdictUnknown {
		t.Fatalf("verdict = %q, want %q", r.Verdict, VerdictUnknown)
	}
	if r.MeasuredAt != nil || r.CheckedAt != nil || r.CheckFailingSince != nil {
		t.Errorf("stamps = (%v, %v, %v), want all three null", r.MeasuredAt, r.CheckedAt, r.CheckFailingSince)
	}
}

// Meta rejecting the token turns into `recusado`, not `desconhecido`: it
// RESPONDED, and that's a definitive outcome — only a human can fix it.
// Calling this "unknown" would hide the one case where the consumer needs
// to call someone.
func TestWatchdogMarksRefusedWhenMetaRefusesTheToken(t *testing.T) {
	m := tokenAcceptingMeta()
	m.respond(http.StatusUnauthorized, revokedTokenBody)
	v, _ := testWatchdog(t, m, "lojinha")

	v.Check(context.Background())

	if r := v.Read("lojinha"); r.Verdict != VerdictRefused {
		t.Fatalf("verdict = %q, want %q", r.Verdict, VerdictRefused)
	}
}

// The CHECK failing is NOT the credential failing. A 503 from Meta
// (retentavel class) says the problem is theirs, not the token's: the
// previous verdict stays in place and the DIVERGENCE between the two
// timestamps is what flags the failure — visible without the consumer
// knowing anything about our implementation.
func TestWatchdogWithFailingCheckKeepsTheVerdictAndDivergesTheStamps(t *testing.T) {
	m := tokenAcceptingMeta()
	v, clock := testWatchdog(t, m, "lojinha")

	v.Check(context.Background())
	measuredAt := clock.read().Format(time.RFC3339)

	// Meta goes down. Two attempts in a row fail.
	m.respond(http.StatusServiceUnavailable, `{"error":{"message":"try later","code":1}}`)
	clock.advance(time.Minute)
	failAt := clock.read().Format(time.RFC3339)
	v.Check(context.Background())
	clock.advance(time.Minute)
	v.Check(context.Background())

	r := v.Read("lojinha")
	if r.Verdict != VerdictOK {
		t.Errorf("verdict = %q, want %q — a 503 from Meta proves nothing about the credential", r.Verdict, VerdictOK)
	}
	if r.MeasuredAt == nil || *r.MeasuredAt != measuredAt {
		t.Errorf("measured_at = %v, want %q (Meta's last RESPONSE)", r.MeasuredAt, measuredAt)
	}
	if r.CheckedAt == nil || *r.CheckedAt == measuredAt {
		t.Errorf("checked_at = %v — it has to DIVERGE from measured_at when the check fails", r.CheckedAt)
	}
	// The FIRST failure of the streak, not the last: pushing the date on
	// every attempt would make it say "failing for a minute" after an
	// hour of failing.
	if r.CheckFailingSince == nil || *r.CheckFailingSince != failAt {
		t.Errorf("check_failing_since = %v, want %q (the FIRST failure of the streak)",
			r.CheckFailingSince, failAt)
	}
}

// A STALE `ok` EXPIRES. A cache that never expires is a lie with a
// timestamp: `{"veredito":"ok","medido_em":"15:20"}` that never ages is
// ambiguous between "I checked at 15:20 and didn't need to again" and "I
// checked at 15:20 and EVERYTHING since then has failed" — and in the
// second case the dashboard paints green while Meta is down.
//
// MUTATION THAT PROVES IT: deleting the comparison against v.validity in
// Read (always returning m.verdict) turns this test red, with `ok` after
// hours with no response at all from Meta.
func TestWatchdogExpiresTheStaleVerdictToUnknown(t *testing.T) {
	m := tokenAcceptingMeta()
	v, clock := testWatchdog(t, m, "lojinha")

	v.Check(context.Background())
	if r := v.Read("lojinha"); r.Verdict != VerdictOK {
		t.Fatalf("initial verdict = %q, want %q", r.Verdict, VerdictOK)
	}

	// Within the validity window, the verdict still holds — otherwise the
	// test above would pass under an "always expires", which would be
	// equally wrong.
	clock.advance(verdictValidity - time.Second)
	if r := v.Read("lojinha"); r.Verdict != VerdictOK {
		t.Fatalf("verdict BEFORE expiring = %q, want %q — it cannot expire too early",
			r.Verdict, VerdictOK)
	}

	clock.advance(2 * time.Second)
	r := v.Read("lojinha")
	if r.Verdict != VerdictUnknown {
		t.Errorf("verdict after %v = %q, want %q — a stale `ok` has to degrade",
			verdictValidity, r.Verdict, VerdictUnknown)
	}
	// The timestamp of the last real response SURVIVES expiration: it's
	// the one that says how long the gateway hasn't heard from Meta.
	if r.MeasuredAt == nil {
		t.Errorf("measured_at became null on expiry — the information of HOW LONG there has been no response disappears")
	}
}

// A PAUSED instance is not checked: it doesn't send, so spending a call
// for it would be measuring a channel that can't fail.
func TestWatchdogDoesNotCheckAPausedInstance(t *testing.T) {
	m := tokenAcceptingMeta()
	v, _ := testWatchdog(t, m) // none activated: instance is born paused

	v.Check(context.Background())

	if n := m.gets.Load(); n != 0 {
		t.Errorf("the watchdog talked to Meta %d time(s) for PAUSED instances", n)
	}
	if r := v.Read("lojinha"); r.Verdict != VerdictUnknown {
		t.Errorf("verdict of a paused instance = %q, want %q — nobody is measuring",
			r.Verdict, VerdictUnknown)
	}
}

// The watcher measures EVERY active instance, not just the first: a
// `break` in place of `continue` would leave the second instance blind
// forever, and nothing would flag it.
func TestWatchdogChecksEveryActiveInstance(t *testing.T) {
	m := tokenAcceptingMeta()
	v, _ := testWatchdog(t, m, "lojinha", "clinica")

	v.Check(context.Background())

	if n := m.gets.Load(); n != 2 {
		t.Errorf("Graph API calls = %d, want 2 (one per ACTIVE instance)", n)
	}
	for _, slug := range []string{"lojinha", "clinica"} {
		if r := v.Read(slug); r.Verdict != VerdictOK {
			t.Errorf("verdict of %q = %q, want %q", slug, r.Verdict, VerdictOK)
		}
	}
}

// T-115 (3): proves that Start's recover() really lets the SECOND round
// of the loop happen after a panic on the first — the guarantee Start's
// comment promises ("without it, the watcher stops measuring IN SILENCE
// for the rest of the process's life"), and that `go tool cover` showed at
// 0% before this test. `v.work` (injected only here) panics on the
// first call and signals a channel on the second; if the recover didn't
// exist, the goroutine would die on the panic and the channel would never
// receive anything — the test would fail by timeout.
func TestWatchdogStartSurvivesAPanicAndContinuesOnTheNextTick(t *testing.T) {
	store, _ := storeWithConsumer(t)
	v := NewWatchdog(store, meta.NewClient(http.DefaultClient, "http://127.0.0.1:1"))
	v.interval = time.Millisecond

	// Start's goroutine never stops (there's no Parar method), so it
	// keeps ticking after this test ends — that's why the channel close
	// only happens EXACTLY on the second call, never again, otherwise
	// closing an already-closed channel again would panic on every tick
	// for the rest of the test process's life.
	var calls int
	secondRound := make(chan struct{})
	v.work = func(ctx context.Context) {
		calls++
		switch calls {
		case 1:
			panic("test panic — first round")
		case 2:
			close(secondRound)
		}
	}

	v.Start()

	select {
	case <-secondRound:
		// the second round happened — the first round's panic did NOT kill the loop.
	case <-time.After(5 * time.Second):
		t.Fatal("the loop's second round never happened after the panic — recover did not protect the goroutine")
	}
}

// The watcher is read by the HTTP handler, which http.Server serves in
// one goroutine per request, while the timer writes from another. Without
// a concurrent test, -race has nothing to detect — and this project
// already paid a Critical for exactly this (docs/ARMADILHAS.md,
// "Go / concurrency").
func TestWatchdogConcurrentReadWithMeasurementDoesNotRace(t *testing.T) {
	m := tokenAcceptingMeta()
	v, _ := testWatchdog(t, m, "lojinha")

	var wg sync.WaitGroup
	for range 25 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			v.Check(context.Background())
		}()
		go func() {
			defer wg.Done()
			_ = v.Read("lojinha")
		}()
	}
	wg.Wait()
}
