package alert

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// lossAggregationWindow: after the FIRST permanent-loss alert on an
	// instance, further losses on the SAME instance within this window are
	// buffered instead of each firing its own message (rule a). Chosen to
	// match the handler's own largeBodyWindow discipline: short enough that
	// "buffered" still means "happening right now."
	lossAggregationWindow = 10 * time.Minute

	// seriesFailureThreshold and seriesFailureWindow are the two
	// independent triggers for the "consumer failing in series" alert
	// (rule b) — whichever comes first.
	seriesFailureThreshold = 10
	seriesFailureWindow    = 30 * time.Minute

	// seriesReminderInterval: how often the series alert repeats while the
	// failure continues past its own alert (rule b).
	seriesReminderInterval = 6 * time.Hour
)

// Tracker is the in-memory, per-process decision maker behind the alerts:
// GIVEN one delivery outcome, it decides whether the operator needs a NEW
// message right now. It never touches the network — see Sender for that.
//
// NO PERSISTENCE, ON PURPOSE, the same discipline as internal/inbound's
// rejectionCounter: losing this state on a restart costs, at most, one
// delayed reminder or one re-sent "first loss" message — never a silently
// dropped alert and never state this gateway exists specifically not to
// have.
type Tracker struct {
	mu sync.Mutex
	// now is injectable so a test can move the clock without sleeping —
	// production always passes time.Now.
	now    func() time.Time
	loss   map[string]*lossWindow
	series map[string]*failureSeries
}

type lossWindow struct {
	start time.Time
	// correlations of the losses that arrived AFTER the first one (which
	// already alerted on its own) and BEFORE the window closed.
	correlations []string
}

type failureSeries struct {
	first      time.Time
	count      int
	alerted    bool
	lastAlert  time.Time
	lastReason string
}

// NewTracker builds a Tracker. now must not be nil in production; passing
// nil defaults to time.Now so a caller can never end up with a Tracker that
// panics on its first Observe.
func NewTracker(now func() time.Time) *Tracker {
	if now == nil {
		now = time.Now
	}
	return &Tracker{
		now:    now,
		loss:   map[string]*lossWindow{},
		series: map[string]*failureSeries{},
	}
}

// Observe records one delivery outcome for slug and returns the messages
// (zero, one, or occasionally two — a flushed aggregate plus a fresh alert)
// that now need to reach the operator.
//
// THE THREE INPUTS ARE EXACTLY mirror.go's OWN Verdict, so this type never
// re-derives "does this need a person" on its own — that would be a SECOND
// classification that could drift from ConsumerVerdict's, this project's
// mother-trap (see the comment on inbound.CounterKeys).
//
//   - alarm && statusForMeta == 200  -> rule a: PERMANENT loss (Meta never
//     redelivers this one again — mirror.go's own definition of Alarm+200).
//   - statusForMeta == 502 || 504    -> rule b/d: Meta WILL redeliver this
//     one, so it belongs in the "failing in series" count, never in the
//     loss alert — this is also where an Alarm+504 (bad certificate) lands,
//     because it is not a 200 (rule d).
//   - statusForMeta in 2xx           -> rule c: an actually accepted
//     delivery, the only thing that can end a series.
//
// Anything else (a definitive 4xx-style refusal, which mirror.go always
// maps to 200 before it reaches here, so this branch is defensive) neither
// starts nor resets anything.
func (t *Tracker) Observe(slug, correlation string, statusForMeta int, alarm bool, reason string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()

	var out []string
	// Opportunistic flush: an aggregation window closes on the NEXT
	// Observe call for the SAME instance after ten minutes have passed,
	// whatever that call's own outcome is. There is no background timer —
	// documented tradeoff (task T-253): if an instance goes silent right
	// after a burst of losses, the aggregate summary waits for its next
	// delivery instead of firing on its own. The FIRST loss of the burst
	// already alerted immediately, so silence never hides an outage.
	if msg, ok := t.flushLossWindowLocked(slug, now); ok {
		out = append(out, msg)
	}

	switch {
	case alarm && statusForMeta == http.StatusOK:
		if msg, ok := t.observeLossLocked(slug, correlation, reason, now); ok {
			out = append(out, msg)
		}
	case statusForMeta == http.StatusBadGateway || statusForMeta == http.StatusGatewayTimeout:
		if msg, ok := t.observeSeriesFailureLocked(slug, reason, now); ok {
			out = append(out, msg)
		}
	case statusForMeta >= 200 && statusForMeta < 300:
		if msg, ok := t.observeRecoveryLocked(slug, now); ok {
			out = append(out, msg)
		}
	}
	return out
}

func (t *Tracker) flushLossWindowLocked(slug string, now time.Time) (string, bool) {
	w := t.loss[slug]
	if w == nil || now.Sub(w.start) < lossAggregationWindow {
		return "", false
	}
	delete(t.loss, slug)
	if len(w.correlations) == 0 {
		return "", false
	}
	return fmt.Sprintf(
		"zapgw ALERT — instance %s: +%d more lost since %s UTC, correlations: %s",
		slug, len(w.correlations), w.start.UTC().Format("15:04"), strings.Join(w.correlations, ", "),
	), true
}

func (t *Tracker) observeLossLocked(slug, correlation, reason string, now time.Time) (string, bool) {
	// Any expired window for slug was already flushed and removed above, in
	// THIS SAME Observe call — so finding one here means it is still open.
	if w := t.loss[slug]; w != nil {
		w.correlations = append(w.correlations, correlation)
		return "", false
	}
	t.loss[slug] = &lossWindow{start: now}
	return fmt.Sprintf(
		"zapgw ALERT — instance %s: 1 event LOST FOR GOOD (%s). Meta will not redeliver; only a person can recover it. correlation %s",
		slug, reason, correlation,
	), true
}

func (t *Tracker) observeSeriesFailureLocked(slug, reason string, now time.Time) (string, bool) {
	s := t.series[slug]
	if s == nil {
		s = &failureSeries{first: now}
		t.series[slug] = s
	}
	s.count++
	s.lastReason = reason

	if !s.alerted {
		if s.count < seriesFailureThreshold && now.Sub(s.first) < seriesFailureWindow {
			return "", false
		}
		s.alerted = true
		s.lastAlert = now
		return fmt.Sprintf(
			"zapgw ALERT — instance %s: consumer failing for %s (%d deliveries in a row without 2xx, last: %s)."+
				" Meta redelivers for ~36 h; after that it is loss.",
			slug, formatDuration(now.Sub(s.first)), s.count, reason,
		), true
	}

	if now.Sub(s.lastAlert) < seriesReminderInterval {
		return "", false
	}
	s.lastAlert = now
	return fmt.Sprintf(
		"zapgw ALERT — instance %s: consumer still failing after %s (%d deliveries in a row without 2xx, last: %s).",
		slug, formatDuration(now.Sub(s.first)), s.count, reason,
	), true
}

func (t *Tracker) observeRecoveryLocked(slug string, now time.Time) (string, bool) {
	s := t.series[slug]
	if s == nil {
		return "", false
	}
	delete(t.series, slug)
	if !s.alerted {
		// Rule c: a series that never reached its own alert resets in
		// silence — there is nothing to say "back" about.
		return "", false
	}
	return fmt.Sprintf(
		"zapgw OK — instance %s: consumer back after %s (%d failed deliveries)",
		slug, formatDuration(now.Sub(s.first)), s.count,
	), true
}

func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}

// Sender wires a Tracker (WHAT to send) to a Notifier (HOW to send it): it
// exists so the delivery path — internal/inbound's handler — can call one
// nil-safe, non-blocking method and never know Telegram exists.
type Sender struct {
	tracker  *Tracker
	notifier Notifier
}

// NewSender builds a Sender. Both arguments are required; main.go only
// constructs one when both ZAPGW_ALERT_TELEGRAM_TOKEN and
// ZAPGW_ALERT_TELEGRAM_CHAT_ID are set.
func NewSender(tracker *Tracker, notifier Notifier) *Sender {
	return &Sender{tracker: tracker, notifier: notifier}
}

// Observe is the method internal/inbound's handler calls, through a LOCAL
// interface it declares itself (so that package never imports this one —
// same shape as config.Counter/config.Transit). It NEVER blocks the caller
// on the network and NEVER returns an error the caller could act on: every
// message this call produces goes out in its OWN goroutine with its OWN 10s
// timeout, and a delivery failure becomes exactly one log line.
func (s *Sender) Observe(slug, correlation string, statusForMeta int, alarm bool, reason string) {
	if s == nil || s.notifier == nil || s.tracker == nil {
		return
	}
	for _, msg := range s.tracker.Observe(slug, correlation, statusForMeta, alarm, reason) {
		go sendOne(s.notifier, msg)
	}
}

func sendOne(notifier Notifier, msg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := notifier.Notify(ctx, msg); err != nil {
		log.Printf("zapgw: operator alert not delivered: %v", err)
	}
}
