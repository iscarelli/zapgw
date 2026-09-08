package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
	"github.com/iscarelli/zapgw/internal/outbound"
)

// rowValues finds the row that starts (after trimming spaces) with
// `chave` and returns the "today" and "last 7 days" columns. The
// tabwriter aligns with spaces, not literal tabs, so the test cannot
// search for "\t" in the final text — it has to split by FIELDS.
//
// `len(fields) >= 3`, and not `== 3`, since T-065: the table gained the
// `ultimo em` column, whose value has spaces ("2026-07-28T17:57:33Z (ha
// 3min)"). Locked to the exact field count, this helper would say "the
// key didn't appear" for a row that is right there on screen — the worst
// shape of test failure, because it sends you looking for the defect in
// the wrong place.
func rowValues(t *testing.T, text, key string) (today, week int) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == key {
			h, err := strconv.Atoi(fields[1])
			if err != nil {
				t.Fatalf("column 'today' of %q is not a number: %q", key, fields[1])
			}
			s, err := strconv.Atoi(fields[2])
			if err != nil {
				t.Fatalf("column 'last 7 days' of %q is not a number: %q", key, fields[2])
			}
			return h, s
		}
	}
	t.Fatalf("key %q did not appear in the output:\n%s", key, text)
	return 0, 0
}

// valueFromState returns the value of the `rotulo:` row from `zapgw
// estado`'s screen — either at the top (`block` empty) or INSIDE a nested
// block (`token_meta`, `certificado_do_callback`).
//
// WHY IT NEEDS TO KNOW THE BLOCK: `estado:` appears TWICE on screen, once
// at the top ("ativa"/"pausada") and once inside
// `certificado_do_callback` ("nunca_observado"). A helper that returned
// the first occurrence would pass green while comparing the wrong field —
// and a test that compares the wrong field is worse than no test. The
// separation is by INDENTATION, which is what the screen uses.
func valueFromState(t *testing.T, text, block, label string) string {
	t.Helper()
	inside := block == ""
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block != "" {
			if indent <= 2 {
				inside = fields[0] == block+":"
				continue
			}
		} else if indent > 2 {
			continue
		}
		if !inside || fields[0] != label+":" {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), label+":"))
	}
	t.Fatalf("the line %q (block %q) did not appear in the output:\n%s", label, block, text)
	return ""
}

// activeInstanceWithFakeMeta provisions `lojinha`, ACTIVATES the
// instance, and points the Graph API at a fake that accepts the
// credential.
//
// The instance needs to be ACTIVE because a paused instance is not
// checked — neither by the server's watcher nor by `zapgw estado` (see
// TestStateCommandDoesNotSpendACallOnMetaForAPausedInstance). With it
// paused, every verdict test would pass green while measuring
// `desconhecido`, which is the value that comes out even when nothing
// works.
func activeInstanceWithFakeMeta(t *testing.T, g *fakeGraph) map[string]string {
	t.Helper()
	vars := testEnvironment(t)
	vars["ZAPGW_GRAPH_BASE"] = g.server(t).URL

	var junk bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &junk, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}
	if err := storeFromEnvironment(t, vars).ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}
	return vars
}

// SURFACE PARITY (T-065): the four blocks only the consumer used to
// see — `estado`/`pausada`, `versao`, `token_meta` and
// `certificado_do_callback` — appear on the screen of whoever has SSH
// open on the CT.
//
// WHY THIS IS A TEST AND NOT "you can see it by running it": the defect
// this task fixed did not produce a WRONG result, it produced an ABSENT
// one — the information existed, assembled and correct, and simply did
// not show up on this screen. Absence draws no one's attention: T-060 and
// T-064 shipped, each with the suite green, leaving one block missing
// here each time, and no one noticed until someone went looking.
func TestStateCommandShowsTheFourBlocksOnlyTheConsumerSaw(t *testing.T) {
	vars := activeInstanceWithFakeMeta(t, workingGraph())

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}
	text := out.String()

	if want, has := "ativa", valueFromState(t, text, "", "state"); has != want {
		t.Errorf("state = %q, want %q. output:\n%s", has, want, text)
	}
	if want, has := "nao", valueFromState(t, text, "", "paused"); has != want {
		t.Errorf("pausada = %q, want %q. output:\n%s", has, want, text)
	}
	if want, has := version, valueFromState(t, text, "", "version"); has != want {
		t.Errorf("versao = %q, want %q — without it, 'which binary was running?' becomes deploy archaeology", has, want)
	}
	// The verdict and BOTH timestamps: they are what answer "does Meta
	// still accept this token?", which is the question of whoever is in
	// the middle of the incident.
	if want, has := outbound.VerdictOK, valueFromState(t, text, "meta_token", "verdict"); has != want {
		t.Errorf("token_meta.veredito = %q, want %q. output:\n%s", has, want, text)
	}
	for _, label := range []string{"measured_at", "checked_at"} {
		if v := valueFromState(t, text, "meta_token", label); v == "—" {
			t.Errorf("token_meta.%s came out empty after a successful measurement. output:\n%s", label, text)
		}
	}
	if want, has := outbound.CertNeverObserved, valueFromState(t, text, "callback_certificate", "state"); has != want {
		t.Errorf("certificado_do_callback.estado = %q, want %q — an instance that never delivered has no observed certificate",
			has, want)
	}
}

// THE TIMESTAMP FORMAT DECISION (T-065, item 3): the CLI shows RAW UTC,
// byte for byte the same as what the route returns, with the time
// distance next to it.
//
// Translating to the house's timezone (-03) would force whoever operates
// it to do the math in their head against every `journalctl` line (the
// CT's journal is in UTC) and would have the operator and the consumer
// citing DIFFERENT CLOCKS about the same event — the same two-truths
// disease this task exists to cure. The distance exists because the real
// question is never "what time was it", it's "is this fresh?".
func TestStateCommandShowsStampInUTCWithTheDistanceBeside(t *testing.T) {
	vars := testEnvironment(t)
	var buf bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &buf, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	store := storeFromEnvironment(t, vars)
	// Three days ago: far enough for the distance to be unmistakable, and
	// within the seven-day window.
	when := time.Now().Add(-72 * time.Hour)
	if err := store.IncrementCounter("lojinha", config.CounterReceived, when); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}
	text := out.String()

	expected := when.UTC().Format(time.RFC3339)
	if !strings.Contains(text, expected) {
		t.Errorf("the `recebidas` timestamp did not come out in UTC/RFC3339 (%s). output:\n%s", expected, text)
	}
	if !strings.Contains(text, "(ha 3d)") {
		t.Errorf("the time distance did not appear next to the timestamp. output:\n%s", text)
	}
	// gerado_em is also a timestamp, and it proves the rule holds for
	// EVERY time field in the state — the formatting asks the PARSE, not
	// the field's name.
	if v := valueFromState(t, text, "", "generated_at"); !strings.HasSuffix(v, "(ha 0s)") {
		t.Errorf("gerado_em = %q, want ending in \"(ha 0s)\"", v)
	}
}

// THE REPRODUCTION OF THE PRODUCTION DEFECT (T-072), in the real
// command: the token measurement happens AFTER `gerado_em` is stamped,
// and the screen announced that past fact as a future one.
//
// Real output from CT 125, 2026-07-28 18:22, minutes after v0.25.0 shipped:
//
//	gerado_em:      2026-07-28T18:22:31Z (ha 0s)
//	medido_em:      2026-07-28T18:22:32Z (daqui a 0s)
//	conferido_em:   2026-07-28T18:22:33Z (daqui a 1s)
//
// The fake Graph API responds with a delay on purpose, and the SIZE of
// the delay is this test's only delicate detail — measured, not chosen:
// at 50ms it passed GREEN even with the defect put back, because every
// timestamp here is RFC3339 WITHOUT a fractional second
// (config.stampOf, internal/outbound/carimbo) and the two instants
// landed in the same second. A 1s delay guarantees the measurement's
// timestamp lands in a second LATER than `gerado_em`'s, which is the
// condition where the screen lied. That is how it happened in
// production: Meta took a while, and the operator read "(daqui a 1s)".
func TestStateCommandDoesNotAnnounceAsFutureAMeasurementThatALREADYHAPPENED(t *testing.T) {
	g := workingGraph()
	g.delay = 1050 * time.Millisecond
	vars := activeInstanceWithFakeMeta(t, g)

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}
	text := out.String()

	for _, label := range []string{"measured_at", "checked_at"} {
		v := valueFromState(t, text, "meta_token", label)
		if strings.Contains(v, "daqui a") {
			t.Errorf("token_meta.%s = %q — the screen announces as FUTURE a measurement that already happened.\noutput:\n%s",
				label, v, text)
		}
		if !strings.Contains(v, "(ha ") {
			t.Errorf("token_meta.%s = %q, want the distance in the past (\"(ha ...)\").\noutput:\n%s",
				label, v, text)
		}
	}
}

// T-065'S GUARANTEE EXERCISED AGAIN, with the field T-070 added:
// `carimbos_desde` was born in `State` (internal/outbound/state.go) and
// shows up on this screen **without a single line of this file having
// been written for it**. If someone ever swaps
// outbound.StateRows's reflection for a field list, it is this test
// that goes red — not the consumer, months later, noticing the operator
// sees less than they do.
func TestStateCommandShowsStampsSinceWithoutFieldListInTheCLI(t *testing.T) {
	vars := testEnvironment(t)
	var buf bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &buf, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}
	text := out.String()

	v := valueFromState(t, text, "", "stamps_since")
	if v == outbound.NoValue {
		t.Fatalf("carimbos_desde = %q on screen — a freshly created instance stamps from the moment it was born.\noutput:\n%s", v, text)
	}
	// The value comes out like every timestamp on this screen: raw UTC
	// with the distance next to it.
	if _, err := time.Parse(time.RFC3339, strings.Fields(v)[0]); err != nil {
		t.Errorf("carimbos_desde = %q, whose first word is not RFC3339: %v", v, err)
	}
}

// A PAUSED instance is not checked — the SAME rule as the server's
// watcher (internal/outbound/watchdog.go), and not a new CLI decision: it
// does not send, so spending a Graph API call on it would mean measuring
// a channel that cannot fail. And the `pausada: sim` on screen already
// explains the `desconhecido` next to it.
func TestStateCommandDoesNotSpendACallOnMetaForAPausedInstance(t *testing.T) {
	g := workingGraph()
	vars := testEnvironment(t)
	vars["ZAPGW_GRAPH_BASE"] = g.server(t).URL

	var buf bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &buf, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}
	if n := g.gets.Load(); n != 0 {
		t.Errorf("the command hit the Graph API %d time(s) for a PAUSED instance", n)
	}
	text := out.String()
	if want, has := outbound.VerdictUnknown, valueFromState(t, text, "meta_token", "verdict"); has != want {
		t.Errorf("token_meta.veredito = %q, want %q for a paused instance. output:\n%s", has, want, text)
	}
}

// Verify (f) of T-035: an instance WITH NO traffic prints zeros, not an
// error.
func TestStateCommandWithoutTrafficPrintsZerosNotError(t *testing.T) {
	vars := testEnvironment(t)
	var provisionOut bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &provisionOut, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}

	text := out.String()
	if strings.Contains(text, "ALARM") {
		t.Errorf("an instance with no traffic must not alarm. output:\n%s", text)
	}
	if !strings.Contains(text, `instance "lojinha"`) {
		t.Errorf("the instance did not appear in the output:\n%s", text)
	}
	for _, key := range config.KeysInDisplayOrder {
		today, week := rowValues(t, text, key)
		if today != 0 || week != 0 {
			t.Errorf("key %q = (today=%d, 7days=%d), want (0, 0)", key, today, week)
		}
	}
}

// T-039: `zapgw estado` had a SECOND key list, hand-written in
// cmd/zapgw/state.go, separate from internal/config/counter.go's
// vocabulary — T-038 added config.CounterAccountDiscarded to the vocabulary
// and no one remembered to add it to the second list, so the counter kept
// incrementing in production and never showed up here. This test iterates
// over config.KeysInDisplayOrder (the REAL vocabulary, not a list
// written in this file — if it were, the test would be the THIRD copy)
// and proves that EVERY key in the vocabulary appears in the output with
// the right value, including the one T-038 added.
func TestStateCommandShowsEveryVocabularyKey(t *testing.T) {
	vars := testEnvironment(t)
	var provisionOut bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &provisionOut, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	store := storeFromEnvironment(t, vars)
	now := time.Now()
	// Increments EACH key in the vocabulary a DIFFERENT number of times
	// (its position in the list + 1), so no reading can "get it right by
	// coincidence" by reading another key's value.
	for i, key := range config.KeysInDisplayOrder {
		for n := 0; n < i+1; n++ {
			if err := store.IncrementCounter("lojinha", key, now); err != nil {
				t.Fatalf("IncrementCounter(%q): %v", key, err)
			}
		}
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}

	text := out.String()
	for i, key := range config.KeysInDisplayOrder {
		want := i + 1
		today, week := rowValues(t, text, key)
		if today != want || week != want {
			t.Errorf("key %q = (today=%d, 7days=%d), want (%d, %d)", key, today, week, want, want)
		}
	}
}

// THE PROOF THAT THE SOURCE IS ONE (T-060, Verify): the GET /v1/estado
// route returns the SAME numbers as `zapgw estado` for the same instance,
// key by key — and, since T-065, the SAME BLOCKS.
//
// WHY IT LIVES HERE, and not in the handler's package: it is the only
// place that sees BOTH surfaces. A test on each side would only prove
// each one agrees with itself — which is exactly what was green when
// `cmd/zapgw/state.go` had a second key list and did not show
// `conta_descartada` (T-038/T-039), and again when it showed none of the
// four blocks the route published (T-060/T-064).
//
// MUTATION THAT PROVES IT: making either side read the counter on its
// own (a second 7-day window, for instance) leaves this test red; and
// adding a field to outbound.State has to show up in BOTH without
// editing either surface.
func TestStateRouteReturnsTheSameNumbersAsTheStateCommand(t *testing.T) {
	// The fake Meta is the SAME for both sides: the route reads the
	// server's watcher cache and the CLI measures on the spot (its
	// process is born with no cache), so only by measuring against the
	// same Meta is the verdict comparable.
	g := workingGraph()
	vars := activeInstanceWithFakeMeta(t, g)
	var buf bytes.Buffer
	if err := dispatch([]string{
		"provisionar", "consumidor", "--nome", "consumer-a", "--instancias", "lojinha",
	}, &buf, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision consumer: %v", err)
	}
	token := tokenFromOutput(t, buf.String())

	store := storeFromEnvironment(t, vars)
	now := time.Now()
	// Each key a different number of times, and one of them outside
	// today: only this way do the two windows end up with DIFFERENT
	// values from each other, and a side that swapped "today" for "7
	// days" (or vice versa) would not pass by coincidence.
	for i, key := range config.KeysInDisplayOrder {
		for n := 0; n <= i; n++ {
			if err := store.IncrementCounter("lojinha", key, now); err != nil {
				t.Fatalf("IncrementCounter(%q): %v", key, err)
			}
		}
		if err := store.IncrementCounter("lojinha", key, now.AddDate(0, 0, -2)); err != nil {
			t.Fatalf("IncrementCounter(%q, -2d): %v", key, err)
		}
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}

	// The ROUTE's watcher is the server's: it measures on an explicit
	// tick, and the consumer's read answers ONLY from the cache.
	watchdog := outbound.NewWatchdog(store, meta.NewClient(nil, graphBase(fakeEnvironment(vars))))
	watchdog.Check(context.Background())
	// The last parameter is the ceiling of the `serie_dias` window
	// (T-081), which in production comes resolved from the environment a
	// single time in `main`.
	// nil in place of the Instagram renewer: this test has no instagram
	// instance, and the `token_instagram` block comes out `nao_se_aplica`
	// either way (see the same decision in cmd/zapgw/state.go).
	// IngressSource{} (T-120): this test does not exercise the `entrada`
	// block, and the zero value is the honest one — `via: desconhecido`,
	// `conector: nao_configurado`.
	h := outbound.NewStateHandler(store, outbound.NewAuthenticator(store), watchdog, nil,
		outbound.IngressSource{}, nil, nil, version,
		config.CounterRetentionDays(fakeEnvironment(vars)), config.NewCounter(store), outbound.AllTypes)

	// T-208: `instance` (English), not `instancia` — this route now records
	// config.CounterOldNameUsed on the OLD spelling (GET /v1/estado's own
	// ENTRADA-QUERY, docs/MIGRACAO-CONTRATO-EN.md section 9.2), and this
	// test compares the route's counters against a snapshot the CLI
	// command already took — using the old spelling here would make the
	// route's OWN read self-increment the very counter it's about to
	// report, diverging from the earlier snapshot for a reason unrelated
	// to what this test checks.
	req := httptest.NewRequest(http.MethodGet, "/v1/estado?instance=lojinha", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	// The route NEVER talks to Meta on a read (watchdog.go): the fake
	// Meta's counter must not move because of a request. Before, a dead
	// address was enough to prove this; now that the watcher measures
	// against the fake Meta, what proves it is the count around
	// ServeHTTP.
	before := g.gets.Load()
	h.ServeHTTP(rec, req)
	if after := g.gets.Load(); after != before {
		t.Errorf("GET /v1/estado bateu %d vez(es) na Graph API — a leitura tem de sair do cache da vigia",
			after-before)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		State    string `json:"state"`
		Paused   bool   `json:"paused"`
		Version  string `json:"version"`
		Counters map[string]struct {
			Today     int `json:"hoje"`
			Last7Days int `json:"last_7_days"`
		} `json:"counters"`
		MetaToken struct {
			Verdict string `json:"verdict"`
		} `json:"meta_token"`
		CallbackCertificate struct {
			State string `json:"state"`
		} `json:"callback_certificate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}

	text := out.String()
	for _, key := range config.KeysInDisplayOrder {
		today, week := rowValues(t, text, key)
		fromAPI, has := resp.Counters[key]
		if !has {
			t.Errorf("the route did not return the key %q that the command shows", key)
			continue
		}
		if fromAPI.Today != today || fromAPI.Last7Days != week {
			t.Errorf("key %q: route = (today=%d, 7days=%d), command = (today=%d, 7days=%d) — "+
				"both surfaces HAVE to read the same source",
				key, fromAPI.Today, fromAPI.Last7Days, today, week)
		}
	}

	// THE FOUR BLOCKS (T-065), field by field. `pausada` compares the
	// route's BOOLEAN with the CLI's WORD on purpose: the screen format
	// belongs to each surface, the fact is a single one.
	pausedInCLI := map[bool]string{true: "sim", false: "nao"}[resp.Paused]
	blocks := []struct {
		block, label, fromRoute string
	}{
		{"", "state", resp.State},
		{"", "paused", pausedInCLI},
		{"", "version", resp.Version},
		{"meta_token", "verdict", resp.MetaToken.Verdict},
		{"callback_certificate", "state", resp.CallbackCertificate.State},
	}
	for _, c := range blocks {
		if fromCLI := valueFromState(t, text, c.block, c.label); fromCLI != c.fromRoute {
			t.Errorf("%s%s: route = %q, command = %q — the two build the SAME state",
				c.block+".", c.label, c.fromRoute, fromCLI)
		}
	}
	// AND the verdict has to be the MEASURED one, not the `desconhecido`
	// that comes out when nothing works: without this line, both sides
	// could agree on knowing nothing and the test would pass green.
	if resp.MetaToken.Verdict != outbound.VerdictOK {
		t.Errorf("token_meta.veredito = %q, want %q — with the fake Meta accepting, both sides HAVE to measure",
			resp.MetaToken.Verdict, outbound.VerdictOK)
	}
}

func TestStateCommandFlagsUnknownSlug(t *testing.T) {
	vars := testEnvironment(t)
	var provisionOut bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &provisionOut, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	var out bytes.Buffer
	err := dispatch([]string{"estado", "--slug", "nao-existe"}, &out, fakeEnvironment(vars))
	if !errors.Is(err, config.ErrInstanceNotFound) {
		t.Fatalf("error = %v, want ErrInstanceNotFound", err)
	}
}

// THE ALARM is the FIRST visible thing, never a line in the middle of a
// table (T-035, Do item 5): this test checks the ORDER of the text, not
// just its presence.
func TestStateCommandShowsTheAlarmBEFORETheTable(t *testing.T) {
	vars := testEnvironment(t)
	var provisionOut bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &provisionOut, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	store := storeFromEnvironment(t, vars)
	if err := store.IncrementCounter("lojinha", config.CounterDefinitiveLossAlarm, time.Now()); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}
	if err := store.IncrementCounter("lojinha", config.CounterRefusedByConsumer, time.Now()); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}

	text := out.String()
	alarmPos := strings.Index(text, "ALARM")
	tablePos := strings.Index(text, `instance "lojinha"`)
	if alarmPos == -1 {
		t.Fatalf("no ALARM in the output, with 1 definitive-loss event recorded:\n%s", text)
	}
	if tablePos == -1 {
		t.Fatalf("the instance table did not appear:\n%s", text)
	}
	if alarmPos > tablePos {
		t.Errorf("ALARM appeared AFTER the table — it has to be the first visible thing. output:\n%s", text)
	}
	if !strings.Contains(text, "lojinha") {
		t.Errorf("the ALARM does not say WHICH instance:\n%s", text)
	}
}

func TestStateCommandFiltersBySlug(t *testing.T) {
	vars := testEnvironment(t)
	var buf bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &buf, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision lojinha: %v", err)
	}
	buf.Reset()
	if err := dispatch(instanceArgsWith("clinica", ""), &buf, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision clinica: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `instance "lojinha"`) {
		t.Errorf("lojinha did not appear:\n%s", text)
	}
	if strings.Contains(text, `instance "clinica"`) {
		t.Errorf("clinica appeared despite the --slug lojinha filter:\n%s", text)
	}
}

// The sum of "today" and "last 7 days" cannot get confused: an event
// from 3 days ago enters the weekly summary but NOT today's.
func TestStateCommandSumsTodayAndLast7DaysSeparately(t *testing.T) {
	vars := testEnvironment(t)
	var buf bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &buf, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provision instance: %v", err)
	}

	store := storeFromEnvironment(t, vars)
	now := time.Now()
	if err := store.IncrementCounter("lojinha", config.CounterReceived, now); err != nil {
		t.Fatalf("IncrementCounter (today): %v", err)
	}
	if err := store.IncrementCounter("lojinha", config.CounterReceived, now.AddDate(0, 0, -3)); err != nil {
		t.Fatalf("IncrementCounter (3 days ago): %v", err)
	}
	if err := store.IncrementCounter("lojinha", config.CounterReceived, now.AddDate(0, 0, -10)); err != nil {
		t.Fatalf("IncrementCounter (10 days ago, outside the window): %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"estado"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}

	text := out.String()
	today, week := rowValues(t, text, config.CounterReceived)
	if today != 1 || week != 2 {
		t.Errorf("recebidas = (today=%d, 7days=%d), want (1, 2) — the event from 10 days ago falls OUTSIDE the window. output:\n%s",
			today, week, text)
	}
}

// --- T-120: which path serves inbound traffic ----------------------------------

// 🔴 AN UNKNOWN VALUE IN ZAPGW_ENTRADA_VIA FAILS CLOSED — this is test
// (a) of T-120, at the BINARY level. The server makes the SAME call
// (outbound.IngressVia) before opening the database and before listening
// on any port, and does not start up; here the command returns an error
// instead of printing a screen with a made-up inbound path.
//
// WHY THIS IS TOUGHER THAN IT LOOKS: `via` goes into the CONTRACT, and a
// `tunnel` (with two `n`s) silently accepted produces no error at all —
// it produces a field every consumer reads as checked configuration.
func TestStateCommandRefusesUnknownInboundPath(t *testing.T) {
	vars := testEnvironment(t)
	vars["ZAPGW_ENTRADA_VIA"] = "tunnel"

	var out bytes.Buffer
	err := dispatch([]string{"estado"}, &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatalf("the command should have REFUSED %q; output:\n%s", vars["ZAPGW_ENTRADA_VIA"], out.String())
	}
	if !strings.Contains(err.Error(), "ZAPGW_ENTRADA_VIA") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// T-065'S SURFACE PARITY, checked on the new block: `entrada` shows up
// on the screen of whoever has SSH open on the CT without anyone having
// edited the CLI — the rows come from outbound.StateRows, by
// reflection.
//
// And the operator needs it as much as the consumer does: when nothing
// is coming in, the first question is "which path was this supposed to
// come in through?".
func TestStateCommandShowsTheInboundBlock(t *testing.T) {
	vars := activeInstanceWithFakeMeta(t, workingGraph())
	vars["ZAPGW_ENTRADA_VIA"] = outbound.ViaTunnel

	var out bytes.Buffer
	if err := dispatch([]string{"estado", "--slug", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("estado: %v", err)
	}
	text := out.String()

	if want, has := outbound.ViaTunnel, valueFromState(t, text, "ingress", "via"); has != want {
		t.Errorf("entrada.via = %q, want %q. output:\n%s", has, want, text)
	}
	// Without ZAPGW_CONECTOR_READY the block stays on screen, saying no
	// one said who to ask — never a missing line, for the same reason the
	// JSON never omits the field.
	if !strings.Contains(text, outbound.ConnectorNotConfigured) {
		t.Errorf("the screen does not show the connector as %q. output:\n%s", outbound.ConnectorNotConfigured, text)
	}
}
