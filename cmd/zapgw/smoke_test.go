package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// fakeGraph is a fake Graph API with the two routes the smoke test uses.
// The counters are atomic because httptest.Server serves each request in
// its own goroutine: a raw counter here is a data race, and this project
// already paid a Critical for exactly that.
type fakeGraph struct {
	statusGET  int
	getBody    string
	statusPOST int
	postBody   string
	// delay is how long the fake Graph API takes to respond. Zero (the
	// normal case) responds right away; the one that uses it is the T-072
	// test, which needs the token measurement to land MEASURABLY after
	// `gerado_em` — a slow Meta is the condition where that task's defect
	// showed up large.
	delay time.Duration

	gets  atomic.Int64
	posts atomic.Int64
}

func (g *fakeGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The token goes in the header, never in the URL. If it stops
		// going there, step 2 stops proving what it exists to prove.
		if r.Header.Get("Authorization") == "" {
			t.Errorf("call without Authorization on %s %s", r.Method, r.URL.Path)
		}
		time.Sleep(g.delay)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			g.posts.Add(1)
			w.WriteHeader(g.statusPOST)
			_, _ = w.Write([]byte(g.postBody))
			return
		}
		g.gets.Add(1)
		w.WriteHeader(g.statusGET)
		_, _ = w.Write([]byte(g.getBody))
	}))
	t.Cleanup(s.Close)
	return s
}

func workingGraph() *fakeGraph {
	return &fakeGraph{
		statusGET:  http.StatusOK,
		getBody:    `{"id":"PNID1","display_phone_number":"+55 32 99999-0000"}`,
		statusPOST: http.StatusOK,
		postBody:   `{"messages":[{"id":"wamid.TESTE"}]}`,
	}
}

// smokeScenario provisions an instance and points the Graph API at the fake.
func smokeScenario(t *testing.T, g *fakeGraph) map[string]string {
	t.Helper()
	vars := testEnvironment(t)
	vars["ZAPGW_GRAPH_BASE"] = g.server(t).URL

	var junk bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &junk, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}
	return vars
}

func smokeArgs() []string {
	return []string{"fumaca", "--slug", "lojinha", "--destino", "5511999990000"}
}

func TestSmokeActivatesTheInstanceOnlyAFTERSendingAMessage(t *testing.T) {
	g := workingGraph()
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	if err := dispatch(smokeArgs(), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}

	if g.gets.Load() == 0 {
		t.Error("step 2 did not hit the Graph API — a token revoked by the client would go unnoticed")
	}
	if g.posts.Load() != 1 {
		t.Errorf("messages sent = %d, want 1", g.posts.Load())
	}
	i, err := storeFromEnvironment(t, vars).FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if !i.Active {
		t.Fatal("the instance is still paused after a smoke test that passed")
	}
}

func TestSmokeWithSendFailureLEAVESTheInstancePAUSED(t *testing.T) {
	// THIS IS THE TEST THAT PROTECTS THE ENTIRE RULE. The guarantee
	// "instance is born paused and only the smoke test activates it" is
	// worthless if the smoke test activates when the send failed: a
	// misconfigured instance would go into production "working" and the
	// defect would show up on the customer's side.
	g := workingGraph()
	g.statusPOST = http.StatusBadRequest
	g.postBody = `{"error":{"message":"numero invalido","code":131000}}`
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	err := dispatch(smokeArgs(), &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("the command returned success while Meta refused the send")
	}

	i, lookupErr := storeFromEnvironment(t, vars).FindInstance("lojinha")
	if lookupErr != nil {
		t.Fatalf("FindInstance: %v", lookupErr)
	}
	if i.Active {
		t.Fatal("the instance was ACTIVATED even with the send failing")
	}
}

func TestSmokeWithRefusedTokenSendsNOMessageAtAll(t *testing.T) {
	// Step 2 exists to abort BEFORE step 3. Without it, a revoked token
	// would only show up as a failed send — and every run of the smoke
	// test would try to send a real message to a real number.
	g := workingGraph()
	g.statusGET = http.StatusUnauthorized
	g.getBody = `{"error":{"message":"Invalid OAuth access token","code":190}}`
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	err := dispatch(smokeArgs(), &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("the command returned success while the Graph API refused the token")
	}
	if g.posts.Load() != 0 {
		t.Errorf("sent %d message(s) after the token was refused — step 2 did not abort", g.posts.Load())
	}

	i, lookupErr := storeFromEnvironment(t, vars).FindInstance("lojinha")
	if lookupErr != nil {
		t.Fatalf("FindInstance: %v", lookupErr)
	}
	if i.Active {
		t.Fatal("the instance was ACTIVATED with the token refused")
	}
}

func TestSmokeAbortsWhenTheInstanceDoesNotExist(t *testing.T) {
	g := workingGraph()
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	err := dispatch([]string{"fumaca", "--slug", "nao-existe", "--destino", "5511999990000"},
		&out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("the command accepted a nonexistent slug")
	}
	if g.gets.Load() != 0 || g.posts.Load() != 0 {
		t.Errorf("talked to the Graph API about an instance that does not exist (gets=%d posts=%d)",
			g.gets.Load(), g.posts.Load())
	}
}

func TestSmokeRequiresDestinationWithNoDEFAULT(t *testing.T) {
	// --destino has no default ON PURPOSE: a default here sends a message
	// to the wrong number, and a sent message cannot be undone.
	g := workingGraph()
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	if err := dispatch([]string{"fumaca", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("the command accepted running with no --destino")
	}
	if g.posts.Load() != 0 {
		t.Errorf("sent %d message(s) with no destination given", g.posts.Load())
	}
}

func TestSmokeTakesAnswerWithoutIDAsFAILURE(t *testing.T) {
	// A 200 from Meta does NOT prove an id came back: the same response
	// can carry {"messages":[]}. Activating on that would mean activating
	// a channel that never proved it delivers.
	g := workingGraph()
	g.postBody = `{"messages":[]}`
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	if err := dispatch(smokeArgs(), &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("the command accepted a 200 with no wa_message_id")
	}

	i, err := storeFromEnvironment(t, vars).FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Active {
		t.Fatal("the instance was ACTIVATED on a 200 with no id")
	}
}

// --- T-054: the smoke test's message COUNTS -------------------------------------
//
// On 2026-07-28 the smoke test activated `tenant-two` with a real message
// (confirmed on the device by the owner) and the following `zapgw estado`
// said `enviadas hoje 0`. A message went out and the counter said zero —
// and whoever reads that number to answer "has this instance sent
// anything yet?" received a LYING zero, precisely on the just-activated
// instance.

// todaysCounters reads the instance's day counters FROM DISK, with the
// same care as storeFromEnvironment: a NEW store over the same file, after the
// command has closed its own — what is checked is what got WRITTEN.
func todayCounters(t *testing.T, vars map[string]string, slug string) map[string]int {
	t.Helper()
	now := time.Now()
	m, err := storeFromEnvironment(t, vars).CountersBetween(slug, now, now)
	if err != nil {
		t.Fatalf("CountersBetween(%q): %v", slug, err)
	}
	return m
}

func TestSmokeCountsSENTUnderTheSAMEKeyAsTheProductionSend(t *testing.T) {
	g := workingGraph()
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	if err := dispatch(smokeArgs(), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}

	m := todayCounters(t, vars, "lojinha")
	if m[config.CounterSent] != 1 {
		t.Errorf("sent = %d, want 1 — the message WENT OUT (Meta returned a wamid) and the counter did not see it",
			m[config.CounterSent])
	}
	if m[config.CounterSendFailures] != 0 {
		t.Errorf("falhas_de_envio = %d, want 0 — the send succeeded", m[config.CounterSendFailures])
	}

	// AND THE NUMBER HAS TO SHOW UP WHERE THE QUESTION IS ASKED. This is
	// the closing of the 2026-07-28 report: what was actually looked at
	// was `zapgw estado`, not the database table. Counting into a made-up
	// key would pass the block above and still be invisible here, because
	// `estado` iterates over config.KeysInDisplayOrder (the CLOSED
	// vocabulary, T-039) and not a separate list.
	var state bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &state, fakeEnvironment(vars)); err != nil {
		t.Fatalf("zapgw estado: %v\n%s", err, state.String())
	}
	if today, _ := rowValues(t, state.String(), config.CounterSent); today != 1 {
		t.Errorf("`zapgw estado` shows sent today = %d, want 1:\n%s", today, state.String())
	}
}

func TestSmokeCountsSENDFAILURESWhenMetaRefusesTheSend(t *testing.T) {
	// The symmetry with the test above is the point: counting only
	// success would leave a FAILED send attempt invisible in `zapgw
	// estado` — the same asymmetry this task fixes, with the sign
	// flipped.
	g := workingGraph()
	g.statusPOST = http.StatusBadRequest
	g.postBody = `{"error":{"message":"numero invalido","code":131000}}`
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	if err := dispatch(smokeArgs(), &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("the command returned success while Meta refused the send")
	}

	m := todayCounters(t, vars, "lojinha")
	if m[config.CounterSendFailures] != 1 {
		t.Errorf("falhas_de_envio = %d, want 1", m[config.CounterSendFailures])
	}
	if m[config.CounterSent] != 0 {
		t.Errorf("sent = %d, want 0 (Meta refused)", m[config.CounterSent])
	}
}

func TestSmokeWithRefusedTokenDoesNotTOUCHTheSendCounter(t *testing.T) {
	// THE KEY'S BOUNDARY, and it is the same one as production sending:
	// step 2 aborts BEFORE any message byte goes out to Meta.
	// `falhas_de_envio` means "a send attempt failed", not "the smoke test
	// failed" — counting here would make the key say two different
	// things, and whoever read the number would not know whether a
	// message was involved.
	g := workingGraph()
	g.statusGET = http.StatusUnauthorized
	g.getBody = `{"error":{"message":"Invalid OAuth access token","code":190}}`
	vars := smokeScenario(t, g)

	var out bytes.Buffer
	if err := dispatch(smokeArgs(), &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("the command returned success while the Graph API refused the token")
	}

	m := todayCounters(t, vars, "lojinha")
	if m[config.CounterSent] != 0 || m[config.CounterSendFailures] != 0 {
		t.Errorf("sent = %d, falhas_de_envio = %d, want 0 and 0 — no message was even attempted",
			m[config.CounterSent], m[config.CounterSendFailures])
	}
}

func TestSmokeDoesNotPrintTheSendToken(t *testing.T) {
	g := workingGraph()
	vars := testEnvironment(t)
	vars["ZAPGW_GRAPH_BASE"] = g.server(t).URL
	vars["ZAPGW_TOKEN_ENVIO"] = "token-de-envio-nao-pode-vazar"

	var out bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}
	out.Reset()

	if err := dispatch(smokeArgs(), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "token-de-envio-nao-pode-vazar") {
		t.Errorf("the send token appeared in the smoke test's output:\n%s", out.String())
	}
}
