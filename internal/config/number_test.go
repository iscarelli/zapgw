package config

import (
	"errors"
	"testing"
	"time"
)

// The two reference instants for the tiebreak tests. They are EXPLICIT and
// far apart from each other on purpose: a test that wrote both sources at
// the same instant wouldn't distinguish "the most recent wins" from "the
// last to write wins", which are different rules and only one is
// implemented.
var (
	before = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	after  = time.Date(2026, 7, 28, 12, 30, 0, 0, time.UTC)
)

func testNumber(t *testing.T) *Store {
	t.Helper()
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	return s
}

func update(t *testing.T, s *Store, a NumberUpdate) {
	t.Helper()
	if err := s.UpdateNumberAtMeta("lojinha", a); err != nil {
		t.Fatalf("UpdateNumberAtMeta(%+v): %v", a, err)
	}
}

func readNumber(t *testing.T, s *Store) NumberAtMeta {
	t.Helper()
	n, err := s.NumberAtMeta("lojinha")
	if err != nil {
		t.Fatalf("NumberAtMeta: %v", err)
	}
	return n
}

// An instance nobody observed responds ZEROED and nil error. "Never
// observed" is an answer, not a failure — and it's the normal state of
// every freshly created instance.
func TestANumberNeverObservedIsNotAnError(t *testing.T) {
	s := testNumber(t)

	n := readNumber(t, s)
	if n.Quality.Observed() || n.Limit.Observed() {
		t.Errorf("n = %+v, quero tudo nao-observado", n)
	}
	if !n.CheckedAt.IsZero() {
		t.Errorf("CheckedAt = %v, quero zero — ninguem tentou medir ainda", n.CheckedAt)
	}
}

// 🔴 T-080's MANDATORY MUTATION (the second one): making the webhook NOT
// overwrite an OLDER measurement turns this test red.
//
// The two sources come in at DIFFERENT INSTANTS — without that the test
// wouldn't distinguish anything, and that was the requirement written into
// the task.
func TestTheNEWERWebhookBeatsTheOLDERMeasurement(t *testing.T) {
	s := testNumber(t)

	update(t, s, NumberUpdate{Limit: "TIER_1K", Source: SourceMeasurement, When: before})
	update(t, s, NumberUpdate{Limit: "TIER_50", Source: SourceWebhook, When: after})

	n := readNumber(t, s)
	if n.Limit.Value != "TIER_50" {
		t.Errorf("Limit = %q, quero TIER_50 — o webhook chegou DEPOIS da medicao e vence. "+
			"Um rebaixamento de tier que o gateway ignora e' o aviso que so chega quando o envio ja falhou",
			n.Limit.Value)
	}
	if n.Limit.Source != SourceWebhook {
		t.Errorf("Source = %q, quero %q — o consumidor precisa saber de onde veio o numero que mudou",
			n.Limit.Source, SourceWebhook)
	}
	if !n.Limit.ObservedAt.Equal(after) {
		t.Errorf("ObservedAt = %v, quero %v", n.Limit.ObservedAt, after)
	}
}

// The SYMMETRIC case, and it's half the rule: a newer measurement beats an
// older webhook. Without this test, "webhook always wins" would pass green
// on the test above — and a Meta redelivery (it redelivers for up to 36h)
// would roll back a value already measured since.
func TestTheNEWERMeasurementBeatsTheOLDERWebhook(t *testing.T) {
	s := testNumber(t)

	update(t, s, NumberUpdate{Limit: "TIER_50", Source: SourceWebhook, When: before})
	update(t, s, NumberUpdate{Limit: "TIER_1K", Source: SourceMeasurement, When: after})

	n := readNumber(t, s)
	if n.Limit.Value != "TIER_1K" || n.Limit.Source != SourceMeasurement {
		t.Errorf("(%q, %q), quero (TIER_1K, %q) — nao ha fonte preferida, vence a mais recente",
			n.Limit.Value, n.Limit.Source, SourceMeasurement)
	}
}

// THE CASE THE RULE EXISTS TO COVER: a DELAYED webhook (Meta redelivery, or
// out-of-order delivery) CANNOT roll back a newer value.
func TestALATEWebhookDoesNotRollBackANewerValue(t *testing.T) {
	s := testNumber(t)

	update(t, s, NumberUpdate{Limit: "TIER_1K", Source: SourceMeasurement, When: after})
	update(t, s, NumberUpdate{Limit: "TIER_50", Source: SourceWebhook, When: before})

	if n := readNumber(t, s); n.Limit.Value != "TIER_1K" {
		t.Errorf("Limit = %q, quero TIER_1K — observacao ATRASADA nao pode desfazer uma mais nova",
			n.Limit.Value)
	}
}

// Only MEASUREMENT stamps `conferido_em`. A webhook is not a check: we
// didn't ask anything, it just arrived. If it pushed the stamp, "the
// measurement is healthy" would show green on a gateway that has lost read
// access to the Graph API.
func TestAWebhookDoesNOTStampCheckedAt(t *testing.T) {
	s := testNumber(t)

	update(t, s, NumberUpdate{Limit: "TIER_250", Source: SourceWebhook, When: after})

	n := readNumber(t, s)
	if !n.CheckedAt.IsZero() {
		t.Errorf("CheckedAt = %v depois de SO um webhook, quero zero", n.CheckedAt)
	}
	if !n.Limit.Observed() {
		t.Error("o webhook nao gravou o limite")
	}
}

// A measurement attempt stamps `conferido_em` even when it brings no value
// at all — that's what makes the DIVERGENCE between the two stamps expose
// "the measurement is going back and forth empty", instead of leaving it
// indistinguishable from "nobody measured".
func TestAnEMPTYMeasurementStampsTheCheckWithoutErasingAnything(t *testing.T) {
	s := testNumber(t)

	update(t, s, NumberUpdate{Quality: "GREEN", Limit: "TIER_250", Source: SourceMeasurement, When: before})
	update(t, s, NumberUpdate{Source: SourceMeasurement, When: after})

	n := readNumber(t, s)
	if n.Quality.Value != "GREEN" || n.Limit.Value != "TIER_250" {
		t.Errorf("(%q, %q), quero (GREEN, TIER_250) — medicao sem os campos NAO apaga o que ja se sabia",
			n.Quality.Value, n.Limit.Value)
	}
	if !n.CheckedAt.Equal(after) {
		t.Errorf("CheckedAt = %v, quero %v — a TENTATIVA aconteceu e tem de aparecer", n.CheckedAt, after)
	}
	if !n.Quality.ObservedAt.Equal(before) {
		t.Errorf("ObservedAt da qualidade = %v, quero %v — o valor e' velho e o carimbo dele tem de dizer isso",
			n.Quality.ObservedAt, before)
	}
}

// The webhook doesn't speak to quality and cannot erase it. (It carries an
// `event` — ONBOARDING/FLAGGED/UNFLAGGED —, which is a different fact;
// inventing that it equals a rating would be asserting a translation that
// Meta's source doesn't support.)
func TestAWebhookDoesNOTTouchTheQuality(t *testing.T) {
	s := testNumber(t)

	update(t, s, NumberUpdate{Quality: "YELLOW", Source: SourceMeasurement, When: before})
	update(t, s, NumberUpdate{Limit: "TIER_50", Source: SourceWebhook, When: after})

	n := readNumber(t, s)
	if n.Quality.Value != "YELLOW" || n.Quality.Source != SourceMeasurement {
		t.Errorf("qualidade = (%q, %q), quero (YELLOW, %q)", n.Quality.Value, n.Quality.Source, SourceMeasurement)
	}
}

// A source that doesn't exist is REFUSED, not written. The source travels
// to the consumer: a typo written here would show up on their dashboard as
// if it were our vocabulary.
func TestAnUnknownSourceIsRefused(t *testing.T) {
	s := testNumber(t)

	err := s.UpdateNumberAtMeta("lojinha", NumberUpdate{
		Limit: "TIER_250", Source: "palpite", When: before})
	if !errors.Is(err, ErrUnknownNumberSource) {
		t.Fatalf("err = %v, quero ErrUnknownNumberSource", err)
	}
	if n := readNumber(t, s); n.Limit.Observed() {
		t.Errorf("gravou assim mesmo: %+v", n.Limit)
	}
}

// Record does NOT return an error by signature, and a store that always
// fails cannot bring anything down or panic — the guarantee lives in the
// SIGNATURE, not in the discipline of whoever calls it.
func TestTheNumberObserverOnlyLogsWhenTheStoreFails(t *testing.T) {
	o := NewNumberObserverWithStore(failingNumberStore{})
	o.Record("lojinha", NumberUpdate{Limit: "TIER_250", Source: SourceWebhook, When: before})

	// A nil receiver is a deliberate no-op: tests that have nothing to do
	// with this pass nil, and none of them can panic because of tracking.
	var nilObserver *NumberObserver
	nilObserver.Record("lojinha", NumberUpdate{Limit: "TIER_250", Source: SourceWebhook, When: before})
}

type failingNumberStore struct{}

func (failingNumberStore) UpdateNumberAtMeta(string, NumberUpdate) error {
	return errors.New("banco fora do ar")
}
