package alert

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock lets a test move the Tracker's clock without sleeping — the
// same discipline as internal/inbound's rejectionCounter and Watchdog.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock {
	return &fakeClock{t: t}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var testBase = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

// Rule a, first half: the FIRST permanent loss on an instance alerts right away.
func TestTrackerDefinitiveLossAlertsImmediately(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	msgs := tr.Observe("acme", "corr-1", http.StatusOK, true, "consumer REFUSED (400); event lost for good")

	if len(msgs) != 1 {
		t.Fatalf("Observe returned %d messages, want 1: %v", len(msgs), msgs)
	}
	msg := msgs[0]
	for _, want := range []string{"acme", "corr-1", "1 event LOST FOR GOOD", "Meta will not redeliver", "only a person can recover it"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

// Rule a, second half: losses within the 10-minute window after the first
// one do NOT alert individually — they flush as a SINGLE aggregated message
// once the window closes. Documented choice: the flush happens on the NEXT
// Observe call for the same instance after the window has closed (no
// background timer) — see tracker.go.
func TestTrackerDefinitiveLossAggregatesWithinTheWindow(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	first := tr.Observe("acme", "corr-1", http.StatusOK, true, "consumer REFUSED (400); event lost for good")
	if len(first) != 1 {
		t.Fatalf("first loss: got %d messages, want 1: %v", len(first), first)
	}

	clock.advance(2 * time.Minute)
	second := tr.Observe("acme", "corr-2", http.StatusOK, true, "consumer REFUSED (400); event lost for good")
	if len(second) != 0 {
		t.Fatalf("second loss inside the window: got %d messages, want 0 (buffered): %v", len(second), second)
	}

	clock.advance(5 * time.Minute)
	third := tr.Observe("acme", "corr-3", http.StatusOK, true, "consumer REFUSED (400); event lost for good")
	if len(third) != 0 {
		t.Fatalf("third loss inside the window: got %d messages, want 0 (buffered): %v", len(third), third)
	}

	// The window opened at t0 and is 10 minutes long; t0+11min is past it.
	// A normal, successful delivery on the SAME instance is what triggers
	// the flush.
	clock.advance(4 * time.Minute)
	flushed := tr.Observe("acme", "corr-4", http.StatusOK, false, "consumer stored it (200)")
	if len(flushed) != 1 {
		t.Fatalf("flush after the window closed: got %d messages, want 1 (the aggregate): %v", len(flushed), flushed)
	}
	agg := flushed[0]
	if !strings.Contains(agg, "+2 more lost") {
		t.Errorf("aggregate message %q does not say +2 more lost", agg)
	}
	if strings.Contains(agg, "corr-1") {
		t.Errorf("aggregate message %q wrongly repeats the FIRST correlation, already alerted on its own", agg)
	}
	if !strings.Contains(agg, "corr-2") || !strings.Contains(agg, "corr-3") {
		t.Errorf("aggregate message %q is missing one of the buffered correlations", agg)
	}

	// And a loss AFTER the window closed starts a brand new window, with its
	// own immediate alert.
	fresh := tr.Observe("acme", "corr-5", http.StatusOK, true, "consumer REFUSED (400); event lost for good")
	if len(fresh) != 1 || !strings.Contains(fresh[0], "corr-5") {
		t.Fatalf("loss after the window closed: got %v, want a fresh immediate alert", fresh)
	}
}

// Rule b, first trigger: ten consecutive non-2xx-that-Meta-retries
// deliveries alert, and nothing alerts before the tenth.
func TestTrackerSeriesAlertsAfterTenConsecutiveFailures(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	for i := 1; i < 10; i++ {
		msgs := tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
		if len(msgs) != 0 {
			t.Fatalf("failure #%d: got %v, want no alert before the 10th", i, msgs)
		}
	}

	msgs := tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
	if len(msgs) != 1 {
		t.Fatalf("10th failure: got %d messages, want 1: %v", len(msgs), msgs)
	}
	msg := msgs[0]
	for _, want := range []string{"acme", "consumer failing for", "10 deliveries in a row without 2xx", "~36 h"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

// Rule b, second trigger: 30 minutes since the FIRST failure in the series
// alerts even with fewer than 10 failures — whichever comes first.
func TestTrackerSeriesAlertsAfterThirtyMinutesBelowTheCount(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	if msgs := tr.Observe("acme", "", http.StatusGatewayTimeout, false, "consumer unreachable"); len(msgs) != 0 {
		t.Fatalf("1st failure: got %v, want no alert", msgs)
	}

	clock.advance(31 * time.Minute)
	msgs := tr.Observe("acme", "", http.StatusGatewayTimeout, false, "consumer unreachable")
	if len(msgs) != 1 {
		t.Fatalf("2nd failure, 31 min later: got %d messages, want 1 (time trigger): %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "2 deliveries in a row") {
		t.Errorf("message %q should report 2 deliveries, not 10", msgs[0])
	}
}

// Rule b, reminder: while the series continues past its own alert, a
// reminder goes out every 6 hours — not more often.
func TestTrackerSeriesRemindsEverySixHours(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	for i := 0; i < 10; i++ {
		tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
	}

	// Just under 6h: no reminder yet.
	clock.advance(6*time.Hour - time.Second)
	if msgs := tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver"); len(msgs) != 0 {
		t.Fatalf("just under 6h: got %v, want no reminder yet", msgs)
	}

	// Now past 6h since the alert: the reminder fires.
	clock.advance(2 * time.Second)
	msgs := tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
	if len(msgs) != 1 {
		t.Fatalf("past 6h: got %d messages, want 1 (reminder): %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "12 deliveries in a row") {
		t.Errorf("reminder %q should carry the running total (12)", msgs[0])
	}
}

// Rule c: the first accepted (2xx) delivery after a series that had ALREADY
// alerted sends a recovery message and resets the series.
func TestTrackerRecoveryAfterAnAlertedSeries(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	for i := 0; i < 10; i++ {
		tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
	}

	clock.advance(3 * time.Minute)
	msgs := tr.Observe("acme", "", http.StatusOK, false, "consumer stored it (200)")
	if len(msgs) != 1 {
		t.Fatalf("recovery: got %d messages, want 1: %v", len(msgs), msgs)
	}
	msg := msgs[0]
	for _, want := range []string{"zapgw OK", "acme", "consumer back after", "10 failed deliveries"} {
		if !strings.Contains(msg, want) {
			t.Errorf("recovery message %q does not contain %q", msg, want)
		}
	}

	// The series reset: a single new failure does not immediately re-alert.
	if msgs := tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver"); len(msgs) != 0 {
		t.Fatalf("first failure after recovery: got %v, want no alert (fresh series)", msgs)
	}
}

// Rule c, second half: a series that never reached its own alert resets IN
// SILENCE — no "back" message for a thing the operator was never told about.
func TestTrackerSeriesThatNeverAlertedResetsSilently(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	for i := 0; i < 3; i++ {
		tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
	}

	if msgs := tr.Observe("acme", "", http.StatusOK, false, "consumer stored it (200)"); len(msgs) != 0 {
		t.Fatalf("silent reset: got %v, want no message", msgs)
	}
}

// Rule d, first half: an isolated failure — the single most common case —
// never alerts on its own.
func TestTrackerDoesNotAlertOnAnIsolatedFailure(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	if msgs := tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver"); len(msgs) != 0 {
		t.Fatalf("isolated failure: got %v, want no alert", msgs)
	}
}

// Rule d, second half: an alarm on a 504 (the certificate case, mirror.go)
// is NOT a definitive loss — StatusForMeta is 504, not 200 — and counts
// toward the series (rule b), never toward the loss alert (rule a).
func TestTrackerAlarmWith504CountsTowardTheSeriesNotTheLoss(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	var allMsgs []string
	for i := 0; i < 10; i++ {
		allMsgs = append(allMsgs, tr.Observe("acme", "corr-cert", http.StatusGatewayTimeout, true,
			"the consumer's CERTIFICATE was refused in TLS")...)
	}

	if len(allMsgs) != 1 {
		t.Fatalf("got %d messages across 10 calls, want exactly 1 (the series alert): %v", len(allMsgs), allMsgs)
	}
	if strings.Contains(allMsgs[0], "LOST FOR GOOD") {
		t.Fatalf("a 504 (Meta still retries) must never produce a LOST FOR GOOD message: %q", allMsgs[0])
	}
	if !strings.Contains(allMsgs[0], "consumer failing for") {
		t.Errorf("message %q should be the series alert", allMsgs[0])
	}
}

// Two instances are tracked completely independently.
func TestTrackerInstancesAreIndependent(t *testing.T) {
	clock := newFakeClock(testBase)
	tr := NewTracker(clock.now)

	msgsA := tr.Observe("acme", "corr-a", http.StatusOK, true, "consumer REFUSED (400); event lost for good")
	if len(msgsA) != 1 {
		t.Fatalf("instance acme: got %v, want 1 immediate alert", msgsA)
	}

	// A single failure on a DIFFERENT instance must not be influenced by
	// acme's state, and must not itself alert (isolated failure).
	msgsB := tr.Observe("beta", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
	if len(msgsB) != 0 {
		t.Fatalf("instance beta: got %v, want no alert (isolated, unrelated to acme)", msgsB)
	}
}

// The mutex is exercised the way http.Server actually calls this type: many
// goroutines, one Tracker. Run with -race.
func TestTrackerWithstandsConcurrentObserve(t *testing.T) {
	tr := NewTracker(time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tr.Observe("acme", "", http.StatusBadGateway, false, "consumer failed transiently (500); Meta will redeliver")
		}(i)
	}
	wg.Wait()
}
