// Tests for the `ingress` block of GET /v1/estado (T-120).
//
// WHAT THEY PROTECT, and it's one thing said in several ways: the DISTINCTION
// between "I measured and it's bad" and "I couldn't measure". The two send
// whoever reads them to opposite places, and both become "no alarm" if no one
// tells them apart — with the second one being the deceptive one, because the
// person believes they're covered.
package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// --- (a) `via` comes from the environment and an unknown value does NOT start ---------------

func TestIngressViaAcceptsTheKnownPathsAndRefusesTheRest(t *testing.T) {
	cases := []struct {
		env     string
		want    string
		wantErr bool
	}{
		// EMPTY IS NOT AN ERROR: the binary starts up and publishes "we don't
		// know". An error here would make this version's first deployment bring
		// the gateway down just because /etc/zapgw/env didn't have the new line
		// yet.
		{env: "", want: ViaUnknown},
		{env: ViaTunnel, want: ViaTunnel},
		{env: ViaPortForwarding, want: ViaPortForwarding},
		// The four typos someone actually makes when editing `env`. None of
		// them can become a contract field.
		{env: "tunnel", wantErr: true},
		{env: "TUNEL", wantErr: true},
		{env: " tunel", wantErr: true},
		{env: "porta", wantErr: true},
	}

	for _, c := range cases {
		via, err := IngressVia(func(string) string { return c.env })
		if c.wantErr {
			if err == nil {
				t.Errorf("%s=%q returned via=%q with no error; the binary HAS to refuse to start",
					VarIngressVia, c.env, via)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s=%q: unexpected error: %v", VarIngressVia, c.env, err)
			continue
		}
		if via != c.want {
			t.Errorf("%s=%q gave via=%q, want %q", VarIngressVia, c.env, via, c.want)
		}
	}
}

// The error HAS to cite the variable and the accepted values: whoever reads it
// is on the console of a machine that just failed to start, without the code
// in front of them.
func TestIngressViaSaysWhatToDoInTheError(t *testing.T) {
	_, err := IngressVia(func(string) string { return "tunnel" })
	if err == nil {
		t.Fatal("unknown value has to give an error")
	}
	for _, want := range []string{VarIngressVia, "tunnel", ViaTunnel, ViaPortForwarding} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not cite %q: %v", want, err)
		}
	}
}

// THE SPACE AROUND THE URL IS TRIMMED — and ` tunel` keeps being rejected. The
// asymmetry is deliberate and lives in ConnectorAddress: here, a line break
// coming from a heredoc in `env` would make the probe publish `unknown`
// with `failing_since` rising over a healthy connector, which is an alarm LYING.
func TestConnectorAddressTrimsSurroundingSpace(t *testing.T) {
	cases := map[string]string{
		"  http://10.0.0.19:60125/ready\n": "http://10.0.0.19:60125/ready",
		"":                                 "",
		"   ":                              "",
	}
	for raw, want := range cases {
		if has := ConnectorAddress(func(string) string { return raw }); has != want {
			t.Errorf("ConnectorAddress(%q) = %q, want %q", raw, has, want)
		}
	}
	if has := ConnectorAddress(nil); has != "" {
		t.Errorf("ConnectorAddress(nil) = %q, want empty", has)
	}
}

// --- (b) connector responding ---------------------------------------------

// fakeReady is a fake `/ready`. `status` is the HTTP code it returns, and it
// is SEPARATE from `conexoes` on purpose: cloudflared responds 503 when there
// is no ready connection, and the test needs to be able to reproduce that.
func fakeReady(t *testing.T, status, connections int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           status,
			"readyConnections": connections,
			"connectorId":      "0499d32e-0000-0000-0000-000000000000",
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/ready"
}

func TestConnectorProbePublishesTheConnectionsThatReadyAnswered(t *testing.T) {
	s := NewConnectorProbe(fakeReady(t, http.StatusOK, 4))
	s.Measure(context.Background())

	c := s.Read()
	if c.State != ConnectorObserved {
		t.Fatalf("state = %q, want %q", c.State, ConnectorObserved)
	}
	if c.ReadyConnections == nil || *c.ReadyConnections != 4 {
		t.Errorf("ready_connections = %v, want 4 (the /ready's readyConnections)", c.ReadyConnections)
	}
	if c.MeasuredAt == nil {
		t.Error("measured_at null after the connector answered")
	}
	if c.FailingSince != nil {
		t.Errorf("failing_since = %v on a measurement that succeeded", *c.FailingSince)
	}
}

// 🔴 ZERO CONNECTIONS IS A MEASUREMENT, NOT A MEASUREMENT FAILURE — and it
// cannot depend on the HTTP STATUS. What status cloudflared responds with
// when there is no ready connection is NOT confirmed (only a healthy `/ready`
// has been seen), so tying the measurement to `200` would be betting on an
// unconfirmed third-party detail — and losing the tunnel drop in the most
// expensive direction: it would turn into "I couldn't measure", hiding the
// drop behind silence. The test uses `503` on purpose, being the case where
// that bet would break.
func TestConnectorProbeTreatsZeroConnectionsAsAMeasurementWhateverTheHTTPStatus(t *testing.T) {
	s := NewConnectorProbe(fakeReady(t, http.StatusServiceUnavailable, 0))
	s.Measure(context.Background())

	c := s.Read()
	if c.State != ConnectorObserved {
		t.Fatalf("state = %q, want %q — zero connections is a MEASUREMENT", c.State, ConnectorObserved)
	}
	if c.ReadyConnections == nil || *c.ReadyConnections != 0 {
		t.Errorf("ready_connections = %v, want 0", c.ReadyConnections)
	}
	if c.FailingSince != nil {
		t.Errorf("failing_since = %v: the question CAME BACK, what failed was the tunnel", *c.FailingSince)
	}
}

// --- (c) connector unreachable ----------------------------------------------

// deadAddress is a URL nobody answers: httptest starts up and closes, so the
// port is real and is closed — more faithful than a hand-picked port.
func deadAddress(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/ready"
	srv.Close()
	return url
}

// 🔴 THE TEST THE MANDATORY MUTATION OF T-120 PROTECTS: connector unreachable
// CANNOT turn into a number. `unknown` + `failing_since` is the only
// honest answer — a `conexoes_prontas: 0` here would be indistinguishable from
// the legitimate measurement of the test above, and would send someone looking
// for the defect in the tunnel when the defect is on the path to the connector
// (or in the dead connector).
func TestConnectorProbeDownComesOutUnknownWithFailingSinceAndNeverZero(t *testing.T) {
	s := NewConnectorProbe(deadAddress(t))
	s.Measure(context.Background())

	c := s.Read()
	if c.State != ConnectorUnknown {
		t.Fatalf("state = %q, want %q", c.State, ConnectorUnknown)
	}
	if c.ReadyConnections != nil {
		t.Errorf("ready_connections = %d on a measurement that did NOT HAPPEN — this is a made-up verdict",
			*c.ReadyConnections)
	}
	if c.FailingSince == nil {
		t.Error("failing_since null: without it, `unknown` does not distinguish 'never asked' from 'ask and it does not come back'")
	}
	if c.MeasuredAt != nil {
		t.Errorf("measured_at = %v without the connector ever having answered", *c.MeasuredAt)
	}
}

// A response that doesn't carry `readyConnections` (proxy in the middle,
// address pointed at the wrong service, error HTML) CANNOT silently decode to
// zero. It's the same defect from the test above wearing different clothes.
func TestConnectorProbeRefusesAnswerWithoutTheFieldInsteadOfReadingZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":200,"connectorId":"x"}`))
	}))
	t.Cleanup(srv.Close)

	s := NewConnectorProbe(srv.URL + "/ready")
	s.Measure(context.Background())

	c := s.Read()
	if c.State != ConnectorUnknown || c.ReadyConnections != nil {
		t.Fatalf("state = %q, ready_connections = %v; want %q with null",
			c.State, c.ReadyConnections, ConnectorUnknown)
	}
}

// After ONE good measurement, the next failure erases the NUMBER but not the
// TIMESTAMP: `medido_em` keeps saying how long the gateway hasn't heard from
// the connector — information that zeroing it out would destroy (same rule as
// MetaToken). And `failing_since` marks the FIRST failure of the sequence,
// never the last.
func TestConnectorProbeKeepsTheStampOfTheLastAnswerAndANCHORSFailingSince(t *testing.T) {
	s := NewConnectorProbe(fakeReady(t, http.StatusOK, 4))
	clock := time.Date(2026, 8, 6, 19, 20, 44, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.Measure(context.Background())

	good := s.Read()
	if good.MeasuredAt == nil {
		t.Fatal("measured_at null after the good measurement")
	}

	// The connector drops. Two attempts, one minute apart.
	s.url = deadAddress(t)
	clock = clock.Add(time.Minute)
	s.Measure(context.Background())
	firstFailure := clock
	clock = clock.Add(time.Minute)
	s.Measure(context.Background())

	c := s.Read()
	if c.ReadyConnections != nil {
		t.Errorf("ready_connections = %d after the measurement stopped coming back", *c.ReadyConnections)
	}
	if c.MeasuredAt == nil || *c.MeasuredAt != *good.MeasuredAt {
		t.Errorf("measured_at = %v, want the timestamp of the last RESPONSE (%v)", c.MeasuredAt, *good.MeasuredAt)
	}
	if c.FailingSince == nil || *c.FailingSince != *stamp(firstFailure) {
		t.Errorf("failing_since = %v, want the FIRST failure of the streak (%v)",
			c.FailingSince, *stamp(firstFailure))
	}
}

// A good measurement that ages degrades to `unknown`: a cache that never
// expires is a lie with a timestamp. If the probe's goroutine dies, a frozen
// `observed` would paint "connector standing" forever.
func TestConnectorProbeDegradesStaleMeasurementToUnknown(t *testing.T) {
	s := NewConnectorProbe(fakeReady(t, http.StatusOK, 4))
	clock := time.Date(2026, 8, 6, 19, 20, 44, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.Measure(context.Background())

	if c := s.Read(); c.State != ConnectorObserved {
		t.Fatalf("state right after measuring = %q, want %q", c.State, ConnectorObserved)
	}

	clock = clock.Add(connectorMeasurementValidity + time.Second)
	c := s.Read()
	if c.State != ConnectorUnknown || c.ReadyConnections != nil {
		t.Errorf("state = %q, ready_connections = %v after the measurement expired; want %q with null",
			c.State, c.ReadyConnections, ConnectorUnknown)
	}
	if c.MeasuredAt == nil {
		t.Error("measured_at disappears on expiry — it is what says how long the gateway has not heard from the connector")
	}
}

// --- (d) no address: `not_configured`, present in the JSON -----------------

func TestConnectorProbeWithoutAddressComesOutNotConfigured(t *testing.T) {
	for name, s := range map[string]*ConnectorProbe{
		"url vazia":     NewConnectorProbe(""),
		"sonda ausente": nil,
	} {
		// Measure and Start on a probe with no address cannot panic: it is the
		// configuration of an installation without a tunnel.
		s.Measure(context.Background())
		s.Start()
		c := s.Read()
		if c.State != ConnectorNotConfigured {
			t.Errorf("%s: state = %q, want %q", name, c.State, ConnectorNotConfigured)
		}
		if c.ReadyConnections != nil || c.MeasuredAt != nil || c.FailingSince != nil {
			t.Errorf("%s: unconfigured block came with a value: %+v", name, c)
		}
	}
}

// --- concurrency ---------------------------------------------------------

// The probe is SHARED state: the timer's goroutine writes while every GET
// /v1/estado request reads. Without this test `-race` is theater — this
// project's `seq++` pitfall (docs/ARMADILHAS.md).
func TestConnectorProbeSupportsConcurrentReadAndWrite(t *testing.T) {
	s := NewConnectorProbe(fakeReady(t, http.StatusOK, 4))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.Measure(context.Background()) }()
		go func() { defer wg.Done(); _ = s.Read() }()
	}
	wg.Wait()
}

// --- the block on the route ------------------------------------------------------

// testIngress is the `ingress` block from the point of view of WHOEVER
// CONSUMES it — a deliberate copy of the format, like testStateResponse:
// if someone renames a field, this test turns red instead of the consumer
// finding out in production.
type testIngress struct {
	Via       string `json:"via"`
	Connector struct {
		State            string  `json:"state"`
		ReadyConnections *int    `json:"ready_connections"`
		MeasuredAt       *string `json:"measured_at"`
		FailingSince     *string `json:"failing_since"`
	} `json:"connector"`
	LastWebhookAt *string `json:"last_webhook_at"`
}

func readIngress(t *testing.T, rec *httptest.ResponseRecorder) testIngress {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var r struct {
		Ingress testIngress `json:"ingress"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	return r.Ingress
}

func stateRouteWithIngress(t *testing.T, ingress IngressSource) (http.Handler, *config.Store) {
	t.Helper()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		ingress, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)
	return h, store
}

// 🔴 THE ROUTE STAYS `200` WITH THE CONNECTOR UNREACHABLE. Same discipline as
// the token watchdog: a third party being unreachable cannot break the
// consumer's read — if it did, their panel would say "gateway has a problem"
// about a healthy gateway, and they would also lose the counters, which is the
// information they came for.
func TestStateRouteStays200WithTheConnectorDownAndPublishesUnknown(t *testing.T) {
	probe := NewConnectorProbe(deadAddress(t))
	probe.Measure(context.Background())
	h, _ := stateRouteWithIngress(t, IngressSource{Via: ViaTunnel, Connector: probe})

	e := readIngress(t, askState(t, h, "token-do-a", "lojinha"))
	if e.Via != ViaTunnel {
		t.Errorf("via = %q, want %q", e.Via, ViaTunnel)
	}
	if e.Connector.State != ConnectorUnknown {
		t.Errorf("connector.state = %q, want %q", e.Connector.State, ConnectorUnknown)
	}
	if e.Connector.ReadyConnections != nil {
		t.Errorf("connector.ready_connections = %d on a measurement that did not happen", *e.Connector.ReadyConnections)
	}
	if e.Connector.FailingSince == nil {
		t.Error("connector.failing_since null with the probe failing")
	}
}

// ⚠️ A FIELD THAT DISAPPEARS BREAKS A STRICT PARSER, and this project already
// paid for it (`instagram_token`, which came to be sent ALWAYS). The
// assertion is about the KEY's PRESENCE in the raw JSON, not about the
// deserialized value: an absent field deserializes to the same zero value as
// a present-but-empty field, so a test that only looked at the struct would
// pass green over the omission.
func TestStateRouteNeverOMITSTheConnectorBlockWhenNoAddressIsConfigured(t *testing.T) {
	h, _ := stateRouteWithIngress(t, IngressSource{Connector: NewConnectorProbe("")})

	rec := askState(t, h, "token-do-a", "lojinha")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body does not deserialize: %v", err)
	}
	rawIngress, has := raw["ingress"]
	if !has {
		t.Fatalf("the `ingress` key is NOT in the JSON: %s", rec.Body.String())
	}
	var blocks map[string]json.RawMessage
	if err := json.Unmarshal(rawIngress, &blocks); err != nil {
		t.Fatalf("`ingress` nao desserializa: %v", err)
	}
	for _, key := range []string{"via", "connector", "last_webhook_at"} {
		if _, has := blocks[key]; !has {
			t.Errorf("the `ingress.%s` key is NOT in the JSON: %s", key, rawIngress)
		}
	}

	e := readIngress(t, rec)
	if e.Connector.State != ConnectorNotConfigured {
		t.Errorf("connector.state = %q, want %q", e.Connector.State, ConnectorNotConfigured)
	}
	// With no one configuring `via`, the gateway says it doesn't know — never a
	// plausible guess.
	if e.Via != ViaUnknown {
		t.Errorf("via = %q, want %q", e.Via, ViaUnknown)
	}
}

// `ultimo_webhook_em` IS `recebidas.ultimo_em`, byte for byte — reuse, not a
// second timestamp. Two timestamps with their own source would diverge on the
// first change, and this one would diverge exactly on the field the consumer
// uses to conclude SILENCE.
func TestIngressLastWebhookAtIsTheSameStampAsReceived(t *testing.T) {
	h, store := stateRouteWithIngress(t, IngressSource{Via: ViaTunnel})

	when := time.Now().Add(-3 * time.Minute)
	if err := store.IncrementCounter("lojinha", config.CounterReceived, when); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	rec := askState(t, h, "token-do-a", "lojinha")
	r := readState(t, rec)
	e := readIngress(t, rec)

	received := r.Counters[config.CounterReceived].LastAt
	if received == nil {
		t.Fatal("received.last_at null after counting a received")
	}
	if e.LastWebhookAt == nil || *e.LastWebhookAt != *received {
		t.Errorf("ingress.last_webhook_at = %v, want the SAME as received.last_at (%q)",
			e.LastWebhookAt, *received)
	}
}

// An instance with no traffic at all: `ultimo_webhook_em` is explicit `null`,
// never an absent field and never a made-up date.
func TestIngressLastWebhookAtIsNullWithoutTraffic(t *testing.T) {
	h, _ := stateRouteWithIngress(t, IngressSource{Via: ViaTunnel})
	if e := readIngress(t, askState(t, h, "token-do-a", "lojinha")); e.LastWebhookAt != nil {
		t.Errorf("last_webhook_at = %q on an instance that never received anything", *e.LastWebhookAt)
	}
}

// 🔴 THE TEST THAT GUARDS THE DOCTRINE, and it doesn't measure any behavior:
// THERE IS NO field, and none may come to exist, that claims the gateway is
// reachable from outside. A request that never arrives leaves no trace in
// here — an `alcancavel: true` would be the blind monitor that answers OK,
// published in the contract and multiplied across every consumer. See the
// header of ingress.go.
func TestStatePublishesNoFieldThatASSERTSReachability(t *testing.T) {
	body, err := json.Marshal(State{})
	if err != nil {
		t.Fatalf("serialize the State: %v", err)
	}
	for _, forbidden := range []string{"alcancavel", "alcancavel_de_fora", "acessivel", "publico_ok"} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("the State publishes a field %q — the gateway CANNOT know that; "+
				"the one who answers is the probe, from outside (ingress.go)", forbidden)
		}
	}
}

// The block ALSO has to appear on the `zapgw estado` screen, without anyone
// editing the CLI — the T-065 guarantee, checked on the new field.
func TestTheIngressBlockAppearsOnTheCLIScreen(t *testing.T) {
	withPrintClock(t, time.Now())
	n := 4
	e := State{Ingress: IngressInState{
		Via:       ViaTunnel,
		Connector: ConnectorInState{State: ConnectorObserved, ReadyConnections: &n},
	}}

	rows := StateRows(e)
	if v := rowValue(t, rows, "via"); v != ViaTunnel {
		t.Errorf("line `via` = %q, want %q", v, ViaTunnel)
	}
	if v := rowValue(t, rows, "ready_connections"); v != "4" {
		t.Errorf("line `ready_connections` = %q, want \"4\"", v)
	}
}

// --- T-214: VarIngressViaNew/VarConnectorReadyNew, the English pair --------

// TestIngressViaAcceptsTheNewNameAndItWins is T-214's Verify for
// ZAPGW_INGRESS_VIA/ZAPGW_ENTRADA_VIA: both work alone, and the NEW one
// wins when both are set.
func TestIngressViaAcceptsTheNewNameAndItWins(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"so a nova", map[string]string{VarIngressViaNew: ViaTunnel}, ViaTunnel},
		{"so a velha", map[string]string{VarIngressVia: ViaPortForwarding}, ViaPortForwarding},
		{"as duas: a NOVA vence", map[string]string{
			VarIngressViaNew: ViaTunnel, VarIngressVia: ViaPortForwarding,
		}, ViaTunnel},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			via, err := IngressVia(func(k string) string { return c.vars[k] })
			if err != nil {
				t.Fatalf("IngressVia: %v", err)
			}
			if via != c.want {
				t.Errorf("via = %q, want %q", via, c.want)
			}
		})
	}
}

// TestIngressViaWarnsOnlyWhenOldNameWins is T-214 Do item 3.
func TestIngressViaWarnsOnlyWhenOldNameWins(t *testing.T) {
	cases := []struct {
		name     string
		vars     map[string]string
		wantWarn bool
	}{
		{"only the old one: warns", map[string]string{VarIngressVia: ViaTunnel}, true},
		{"so a nova: fica calado", map[string]string{VarIngressViaNew: ViaTunnel}, false},
		{"nenhuma: fica calado", map[string]string{}, false},
	}
	original := log.Writer()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			if _, err := IngressVia(func(k string) string { return c.vars[k] }); err != nil {
				log.SetOutput(original)
				t.Fatalf("IngressVia: %v", err)
			}
			log.SetOutput(original)
			warned := strings.Contains(buf.String(), VarIngressVia) && strings.Contains(buf.String(), "deprecated")
			if warned != c.wantWarn {
				t.Errorf("warning = %v (log: %q), want %v", warned, buf.String(), c.wantWarn)
			}
		})
	}
}

// TestConnectorAddressAcceptsTheNewNameAndItWins mirrors
// TestIngressViaAcceptsTheNewNameAndItWins for
// ZAPGW_CONNECTOR_READY/ZAPGW_CONECTOR_READY.
func TestConnectorAddressAcceptsTheNewNameAndItWins(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"so a nova", map[string]string{VarConnectorReadyNew: "http://novo/ready"}, "http://novo/ready"},
		{"so a velha", map[string]string{VarConnectorReady: "http://velho/ready"}, "http://velho/ready"},
		{"as duas: a NOVA vence", map[string]string{
			VarConnectorReadyNew: "http://novo/ready", VarConnectorReady: "http://velho/ready",
		}, "http://novo/ready"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if has := ConnectorAddress(func(k string) string { return c.vars[k] }); has != c.want {
				t.Errorf("ConnectorAddress = %q, want %q", has, c.want)
			}
		})
	}
}

// TestConnectorAddressWarnsOnlyWhenOldNameWins is T-214 Do item 3.
func TestConnectorAddressWarnsOnlyWhenOldNameWins(t *testing.T) {
	cases := []struct {
		name     string
		vars     map[string]string
		wantWarn bool
	}{
		{"only the old one: warns", map[string]string{VarConnectorReady: "http://velho/ready"}, true},
		{"so a nova: fica calado", map[string]string{VarConnectorReadyNew: "http://novo/ready"}, false},
		{"nenhuma: fica calado", map[string]string{}, false},
	}
	original := log.Writer()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			ConnectorAddress(func(k string) string { return c.vars[k] })
			log.SetOutput(original)
			warned := strings.Contains(buf.String(), VarConnectorReady) && strings.Contains(buf.String(), "deprecated")
			if warned != c.wantWarn {
				t.Errorf("warning = %v (log: %q), want %v", warned, buf.String(), c.wantWarn)
			}
		})
	}
}
