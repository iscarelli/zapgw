// Tests for the `alcance_externo` block of GET /v1/estado (T-121).
//
// WHAT THEY PROTECT, in the same shape as entrada_test.go: the DISTINCTION
// between "I measured and the external probe said down" and "I couldn't
// ask the external probe". Reusing `down` for the second case would have
// the consumer coding an alarm on top of data that never existed; and a
// handler that did I/O at request time would turn "external probe down"
// into "GET /v1/estado slow", which is exactly what T-121 exists to not
// have.
package outbound

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// fakeExternalProbe returns the URL of a server that answers
// `{"status": veredito}` — the format documented in
// docs/CONTRATO-CONSUMIDOR.md (the dead-man's-switch JSON badge).
func fakeExternalProbe(t *testing.T, verdict string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": verdict})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/b/2/status.json"
}

// deadExternalAddress is the EQUIVALENT of deadAddress (entrada_test.go),
// copied here instead of reused on purpose: the two test DIFFERENT probes,
// and a shared helper would hide which one broke if they ever diverge.
func deadExternalAddress(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/status"
	srv.Close()
	return url
}

// hangingAddress returns the URL of a server that ACCEPTS the connection
// and NEVER responds — no body, no error, no close. It's the only way to
// prove that SOMEONE is making a real network call: a destination that
// fails right away (deadExternalAddress) comes back too fast to
// distinguish "didn't try" from "tried and failed fast".
//
// It also returns `accepted`, a counter incremented the instant the
// listener accepts a connection. T-215's
// TestStateRouteDoesNotHangWithTheExternalProbeStuck reads it instead of
// racing a wall-clock deadline: if the route ever dials this address, the
// counter moves off zero — deterministically, with no dependence on how
// fast or slow the runner is.
func hangingAddress(t *testing.T) (string, *int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var accepted int64
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&accepted, 1)
			// Accepts and NEVER writes nor closes — on purpose.
			_ = conn
		}
	}()
	return "http://" + ln.Addr().String() + "/status", &accepted
}

// --- (a) URL comes from the environment, with the space trimmed -----------

func TestExternalProbeURLTrimsSurroundingSpace(t *testing.T) {
	cases := map[string]string{
		"":                           "",
		"  ":                         "",
		" https://x.example/y.json ": "https://x.example/y.json",
	}
	for ingress, want := range cases {
		if got := ExternalProbeURL(func(string) string { return ingress }); got != want {
			t.Errorf("ExternalProbeURL(%q) = %q, quero %q", ingress, got, want)
		}
	}
	if got := ExternalProbeURL(nil); got != "" {
		t.Errorf("ExternalProbeURL(nil) = %q, quero vazio", got)
	}
}

// --- (b) good response: the literal travels WITHOUT TRANSLATION -----------

func TestExternalProbePublishesTheVerdictTheURLAnswered(t *testing.T) {
	for _, verdict := range []string{"up", "down"} {
		t.Run(verdict, func(t *testing.T) {
			s := NewExternalProbe(fakeExternalProbe(t, verdict))
			s.Measure(context.Background())

			r := s.Read()
			if r.State != ReachStateObserved {
				t.Fatalf("estado = %q, quero %q", r.State, ReachStateObserved)
			}
			// 🔴 THE CENTRAL POINT: a genuinely measured `down` is
			// `observado` with `veredito: "down"` — NEVER its own state,
			// and never confused with "couldn't ask".
			if r.Verdict == nil || *r.Verdict != verdict {
				t.Errorf("veredito = %v, quero %q", r.Verdict, verdict)
			}
			if r.MeasuredAt == nil {
				t.Error("medido_em nulo depois de a sonda externa responder")
			}
			if r.Source == nil || *r.Source != SourceExternalProbe {
				t.Errorf("fonte = %v, quero %q", r.Source, SourceExternalProbe)
			}
		})
	}
}

// --- (c) external probe down -------------------------------------------

// 🔴 THE TEST T-121 EXISTS TO HAVE: an external probe that's down CANNOT
// turn into `down`, nor into silence. `nao_consegui_verificar` is the ONLY
// honest answer — a `down` here would be indistinguishable from the
// legitimate measurement in the test above, and would trigger the
// consumer's automatic alarm over a fact nobody measured.
func TestExternalProbeDownComesOutCouldNotVerifyAndNeverDown(t *testing.T) {
	s := NewExternalProbe(deadExternalAddress(t))
	s.Measure(context.Background())

	r := s.Read()
	if r.State != ReachStateCouldNotVerify {
		t.Fatalf("estado = %q, quero %q", r.State, ReachStateCouldNotVerify)
	}
	if r.Verdict != nil {
		t.Errorf("veredito = %q numa medicao que NAO ACONTECEU — isso e' um veredito inventado", *r.Verdict)
	}
	if r.MeasuredAt != nil {
		t.Errorf("medido_em = %v sem a sonda externa nunca ter respondido", *r.MeasuredAt)
	}
	if r.Source != nil {
		t.Errorf("fonte = %q sem medicao nenhuma", *r.Source)
	}
}

// A response without the `status` field (a proxy in the middle, an error
// page, JSON in a different shape) CANNOT silently decode to empty. Same
// defect as the test above, different clothes.
func TestExternalProbeRefusesAnswerWithoutTheStatusField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ping":"pong"}`))
	}))
	t.Cleanup(srv.Close)

	s := NewExternalProbe(srv.URL + "/status")
	s.Measure(context.Background())

	r := s.Read()
	if r.State != ReachStateCouldNotVerify || r.Verdict != nil {
		t.Fatalf("estado = %q, veredito = %v; quero %q com null",
			r.State, r.Verdict, ReachStateCouldNotVerify)
	}
}

// A response that isn't readable JSON — HTML from an error page, for
// example — is the SAME defect, tested from the other side of the parser.
func TestExternalProbeRefusesUnreadableJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>404</body></html>`))
	}))
	t.Cleanup(srv.Close)

	s := NewExternalProbe(srv.URL + "/status")
	s.Measure(context.Background())

	if r := s.Read(); r.State != ReachStateCouldNotVerify {
		t.Fatalf("estado = %q, quero %q", r.State, ReachStateCouldNotVerify)
	}
}

// After ONE good measurement, the next failure erases the VERDICT but not
// the TIMESTAMP: `medido_em` keeps saying how long the gateway hasn't heard
// from the external probe — information that zeroing it would destroy (same
// rule as MetaToken and ConnectorInState).
func TestExternalProbeKeepsTheStampOfTheLastGoodAnswerWhenItFails(t *testing.T) {
	s := NewExternalProbe(fakeExternalProbe(t, "up"))
	clock := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.Measure(context.Background())

	good := s.Read()
	if good.MeasuredAt == nil {
		t.Fatal("medido_em nulo depois da medicao boa")
	}

	s.url = deadExternalAddress(t)
	clock = clock.Add(time.Minute)
	s.Measure(context.Background())
	firstFailure := clock
	clock = clock.Add(time.Minute)
	s.Measure(context.Background())

	r := s.Read()
	if r.State != ReachStateCouldNotVerify {
		t.Fatalf("estado = %q, quero %q depois de duas falhas seguidas", r.State, ReachStateCouldNotVerify)
	}
	if r.Verdict != nil {
		t.Errorf("veredito = %q depois de a medicao parar de voltar", *r.Verdict)
	}
	if r.MeasuredAt == nil || *r.MeasuredAt != *good.MeasuredAt {
		t.Errorf("medido_em = %v, quero o carimbo da ultima RESPOSTA (%v)", r.MeasuredAt, *good.MeasuredAt)
	}
	_ = firstFailure // T-121 does not publish falhando_desde (only 4 fields in the contract)
}

// A good measurement that ages out degrades to `nao_consegui_verificar`:
// a cache that never expires is a lie with a timestamp.
func TestExternalProbeDegradesStaleMeasurementToCouldNotVerify(t *testing.T) {
	s := NewExternalProbe(fakeExternalProbe(t, "up"))
	clock := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.Measure(context.Background())

	if r := s.Read(); r.State != ReachStateObserved {
		t.Fatalf("estado logo depois de medir = %q, quero %q", r.State, ReachStateObserved)
	}

	clock = clock.Add(externalMeasurementValidity + time.Second)
	r := s.Read()
	if r.State != ReachStateCouldNotVerify || r.Verdict != nil {
		t.Errorf("estado = %q, veredito = %v depois de a medicao vencer; quero %q com null",
			r.State, r.Verdict, ReachStateCouldNotVerify)
	}
	if r.MeasuredAt == nil {
		t.Error("medido_em some ao vencer — e' ele que diz ha quanto tempo o gateway nao ouve a sonda externa")
	}
}

// --- (d) no URL: `nao_configurado`, and the process boots normally --------

func TestExternalProbeWithoutURLComesOutNotConfiguredAndTheProcessStartsNormally(t *testing.T) {
	for name, s := range map[string]*ExternalProbe{
		"url vazia":     NewExternalProbe(""),
		"sonda ausente": nil,
	} {
		// Measure and Start on a probe with no URL cannot panic — it's the
		// normal configuration of an installation that doesn't yet have
		// ZAPGW_SONDA_EXTERNA_URL (same pattern as `conector` in T-120).
		s.Measure(context.Background())
		s.Start()
		r := s.Read()
		if r.State != ReachStateNotConfigured {
			t.Errorf("%s: estado = %q, quero %q", name, r.State, ReachStateNotConfigured)
		}
		if r.Verdict != nil || r.MeasuredAt != nil || r.Source != nil {
			t.Errorf("%s: bloco nao configurado veio com valor: %+v", name, r)
		}
	}
}

// --- concurrency ------------------------------------------------------------

// SHARED state: the timer goroutine writes while every GET /v1/estado
// request reads. Without this test, `-race` is theater (docs/ARMADILHAS.md,
// "Go / concorrência" — the `seq++` trap).
func TestExternalProbeSupportsConcurrentReadAndWrite(t *testing.T) {
	s := NewExternalProbe(fakeExternalProbe(t, "up"))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.Measure(context.Background()) }()
		go func() { defer wg.Done(); _ = s.Read() }()
	}
	wg.Wait()
}

// --- the block on the route ---------------------------------------------------------

type testExternalReach struct {
	State      string  `json:"state"`
	Verdict    *string `json:"verdict"`
	MeasuredAt *string `json:"measured_at"`
	Source     *string `json:"source"`
}

func readExternalReach(t *testing.T, rec *httptest.ResponseRecorder) testExternalReach {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	var r struct {
		ExternalReach testExternalReach `json:"external_reach"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	return r.ExternalReach
}

// stateRouteWithReach is the EQUIVALENT of stateRouteWithIngress
// (entrada_test.go) for the `alcance_externo` field.
func stateRouteWithReach(t *testing.T, reach *ExternalProbe) (http.Handler, *config.Store) {
	t.Helper()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		IngressSource{}, reach, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)
	return h, store
}

// ⚠️ A FIELD THAT DISAPPEARS BREAKS A STRICT PARSER (the same defect
// `token_instagram` already paid for in v0.37.x). The assertion is about the
// PRESENCE OF THE KEY in the raw JSON: an absent field deserializes to the
// same zero value as a field that's present and empty.
func TestStateRouteNeverOmitsTheExternalReachBlockWithoutAConfiguredURL(t *testing.T) {
	h, _ := stateRouteWithReach(t, NewExternalProbe(""))

	rec := askState(t, h, "token-do-a", "lojinha")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("corpo nao desserializa: %v", err)
	}
	rawReach, has := raw["external_reach"]
	if !has {
		t.Fatalf("a chave `alcance_externo` NAO esta no JSON: %s", rec.Body.String())
	}
	var blocks map[string]json.RawMessage
	if err := json.Unmarshal(rawReach, &blocks); err != nil {
		t.Fatalf("`alcance_externo` nao desserializa: %v", err)
	}
	for _, key := range []string{"state", "verdict", "measured_at", "source"} {
		if _, has := blocks[key]; !has {
			t.Errorf("a chave `alcance_externo.%s` NAO esta no JSON: %s", key, rawReach)
		}
	}

	e := readExternalReach(t, rec)
	if e.State != ReachStateNotConfigured {
		t.Errorf("estado = %q, quero %q", e.State, ReachStateNotConfigured)
	}
}

// The route publishes the measured verdict, byte for byte with what the
// external probe answered.
func TestStateRoutePublishesTheObservedExternalReach(t *testing.T) {
	probe := NewExternalProbe(fakeExternalProbe(t, "up"))
	probe.Measure(context.Background())
	h, _ := stateRouteWithReach(t, probe)

	e := readExternalReach(t, askState(t, h, "token-do-a", "lojinha"))
	if e.State != ReachStateObserved {
		t.Errorf("estado = %q, quero %q", e.State, ReachStateObserved)
	}
	if e.Verdict == nil || *e.Verdict != "up" {
		t.Errorf("veredito = %v, quero \"up\"", e.Verdict)
	}
}

// 🔴 THE CENTRAL VERIFY OF T-121: the handler CANNOT DO NETWORK I/O. With
// the external probe HANGING (accepts the connection and never responds),
// GET /v1/estado has to respond WITHOUT ever dialing it — proving the
// request reads ONLY the cache.
//
// T-215: this test USED TO assert `elapsed > 500ms`, a hard wall-clock
// ceiling on a shared runner — generous, but still a race against the
// clock instead of a proof about the code path. The claim underneath it
// was never about milliseconds: it's that ExternalProbe.Read
// (sonda_externa.go) only reads an in-memory, mutex-protected struct — no
// network call, sync or async. So the mechanism to observe is not "how
// long did it take" but "did anyone ever dial the hanging listener at
// all". `hangingAddress` now hands back a counter of accepted connections;
// a handler that DID contact the probe synchronously would have to
// complete that dial (or die trying) before askState could return, so by
// the time askState is back, the dial would already have happened if it
// were ever going to. Checking the counter is zero is deterministic — it
// does not depend on runner speed at all.
func TestStateRouteDoesNotHangWithTheExternalProbeStuck(t *testing.T) {
	addr, accepted := hangingAddress(t)
	externalProbe := NewExternalProbe(addr)
	// No Measure, no Start: not EVEN one tick happened. If the handler had
	// any path that talked to the probe at request time, this call would
	// hang.
	h, _ := stateRouteWithReach(t, externalProbe)

	rec := askState(t, h, "token-do-a", "lojinha")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt64(accepted); got != 0 {
		t.Fatalf("a sonda externa travada recebeu %d conexao(oes) — o handler fez I/O de rede na "+
			"hora do request (proibido pela T-121)", got)
	}
	e := readExternalReach(t, rec)
	if e.State != ReachStateCouldNotVerify {
		t.Errorf("estado = %q, quero %q (nunca houve tique)", e.State, ReachStateCouldNotVerify)
	}
}
