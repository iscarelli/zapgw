package outbound

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// numberGraph is a fake Graph API that TREATS `fields=` AS A DIFFERENT
// QUESTION — like the real one and like cmd/grafo-falso/. A fake that
// answered the two the same way would hide exactly the dangerous case this
// task is about.
type numberGraph struct {
	// refuseFields makes the GET WITH `fields=` respond 400/code 100 (what
	// Meta responds to a field it doesn't know) while the CLEAN GET keeps
	// answering 200.
	refuseFields   bool
	bodyWithFields string

	withFields atomic.Int64
	clean      atomic.Int64
}

func (g *numberGraph) client(t *testing.T) *meta.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("fields") == "" {
			g.clean.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"P-lojinha"}`))
			return
		}
		g.withFields.Add(1)
		if g.refuseFields {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"(#100) Tried accessing nonexisting field","code":100}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(g.bodyWithFields))
	}))
	t.Cleanup(srv.Close)
	return meta.NewClient(srv.Client(), srv.URL)
}

func watchdogWithNumberGraph(t *testing.T, g *numberGraph, active ...string) (*Watchdog, *config.Store) {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	return NewWatchdog(store, g.client(t)), store
}

func storeNumber(t *testing.T, s *config.Store, slug string) config.NumberAtMeta {
	t.Helper()
	n, err := s.NumberAtMeta(slug)
	if err != nil {
		t.Fatalf("NumberAtMeta(%q): %v", slug, err)
	}
	return n
}

// The measurement happens in the SAME TICK as the credential, in a single
// call — the task forbade a new polling cycle, and "one call per active
// instance per tick" is the mechanical proof there isn't a second one.
//
// And the values reach the database LITERAL: this is where "TIER_250
// became 250" would show up, even if the read in internal/meta were
// correct.
func TestWatchdogObservesTheNumberOnTheSAMETickAndWithLITERALValues(t *testing.T) {
	g := &numberGraph{bodyWithFields: `{"quality_rating":"GREEN",` +
		`"whatsapp_business_manager_messaging_limit":"TIER_250"}`}
	v, store := watchdogWithNumberGraph(t, g, "lojinha")

	v.Check(context.Background())

	if n := g.withFields.Load() + g.clean.Load(); n != 1 {
		t.Errorf("chamadas a Graph API num tique = %d, quero 1 — observar o numero nao pode "+
			"criar um segundo caminho de rede nem um ciclo novo", n)
	}
	if r := v.Read("lojinha"); r.Verdict != VerdictOK {
		t.Errorf("veredito = %q, quero %q — a mesma chamada continua conferindo a credencial", r.Verdict, VerdictOK)
	}

	n := storeNumber(t, store, "lojinha")
	if n.Limit.Value != "TIER_250" {
		t.Errorf("limite gravado = %q, quero o LITERAL %q", n.Limit.Value, "TIER_250")
	}
	if n.Quality.Value != "GREEN" {
		t.Errorf("qualidade gravada = %q, quero o LITERAL %q", n.Quality.Value, "GREEN")
	}
	if n.Limit.Source != config.SourceMeasurement || n.Quality.Source != config.SourceMeasurement {
		t.Errorf("fontes = (%q, %q), quero as duas %q", n.Limit.Source, n.Quality.Source, config.SourceMeasurement)
	}
	if n.CheckedAt.IsZero() {
		t.Error("conferido_em ficou zerado depois de uma medicao que deu certo")
	}
}

// 🔴 THE GUARD THAT PAYS FOR THE DESIGN. A `fields=` that the Graph API
// doesn't know answers 400 — PERMANENT class in our taxonomy,
// indistinguishable from "Meta rejected it for good". Without the
// reconfirmation with the clean GET, the day Meta retires
// `whatsapp_business_manager_messaging_limit` (it ALREADY retired the
// earlier `messaging_limit_tier`) would paint the token of EVERY active
// instance RED, and would send people looking for a revoked credential
// that nobody revoked.
func TestWatchdogDoesNOTRefuseTheTokenWhenGraphRefusesOnlyTheFields(t *testing.T) {
	g := &numberGraph{refuseFields: true}
	v, store := watchdogWithNumberGraph(t, g, "lojinha")

	v.Check(context.Background())

	if r := v.Read("lojinha"); r.Verdict != VerdictOK {
		t.Fatalf("veredito = %q, quero %q — a credencial foi ACEITA no GET limpo; "+
			"quem foi recusado foi o nosso pedido de campo", r.Verdict, VerdictOK)
	}
	if g.clean.Load() != 1 {
		t.Errorf("reconfirmacoes com o GET limpo = %d, quero 1", g.clean.Load())
	}
	// What's lost is ONLY the number observation, and it stays at
	// `nunca_observado` — a NAMED, visible state, never a made-up value.
	if n := storeNumber(t, store, "lojinha"); n.Limit.Observed() || n.Quality.Observed() {
		t.Errorf("gravou observacao a partir de uma chamada que falhou: %+v", n)
	}
}

// A token that's ACTUALLY revoked still turns into `recusado` — the guard
// above can't have turned every rejection into `ok`. Without this test,
// deleting the credential check would pass green.
func TestWatchdogKEEPSRefusingWhenTheTokenIsReallyRefused(t *testing.T) {
	g := &numberGraphRefusingAll{}
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	v := NewWatchdog(store, meta.NewClient(nil, g.server(t)))

	v.Check(context.Background())

	if r := v.Read("lojinha"); r.Verdict != VerdictRefused {
		t.Fatalf("veredito = %q, quero %q", r.Verdict, VerdictRefused)
	}
}

type numberGraphRefusingAll struct{}

func (numberGraphRefusingAll) server(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid OAuth access token","code":190}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// A PAUSED instance is not observed, exactly like it's not checked: it
// doesn't send, and spending a call for it would be measuring a channel
// that can't fail.
func TestWatchdogDoesNotObserveTheNumberOfAPausedInstance(t *testing.T) {
	g := &numberGraph{bodyWithFields: `{"quality_rating":"GREEN"}`}
	v, store := watchdogWithNumberGraph(t, g) // none activated

	v.Check(context.Background())

	if n := g.withFields.Load() + g.clean.Load(); n != 0 {
		t.Errorf("falou %d vez(es) com a Meta por instancia PAUSADA", n)
	}
	if n := storeNumber(t, store, "lojinha"); n.Quality.Observed() || !n.CheckedAt.IsZero() {
		t.Errorf("gravou observacao de instancia pausada: %+v", n)
	}
}

// An instance never observed responds with a NAMED state on BOTH values,
// not a bare `null`. This is the lesson from T-064: whoever treats `null`
// as "too old" starts with a false positive on EVERY new instance.
func TestNumberAtMetaNeverObservedHasANAMEDState(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	raw := askState(t, h, "token-do-a", "lojinha").Body.Bytes()
	// The format is written here from the point of view of THE CONSUMER,
	// like in testStateResponse: renaming a field in the block turns
	// this red instead of the consumer finding out in production.
	var body struct {
		NumberAtMeta struct {
			Quality      map[string]any `json:"quality"`
			MessageLimit map[string]any `json:"message_limit"`
			CheckedAt    *string        `json:"checked_at"`
		} `json:"number_at_meta"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("ler o corpo: %v\n%s", err, raw)
	}
	n := body.NumberAtMeta
	for name, block := range map[string]map[string]any{
		"quality": n.Quality, "message_limit": n.MessageLimit,
	} {
		if block["state"] != CertNeverObserved {
			t.Errorf("%s.estado = %v, quero %q — estado NOMEADO, nunca so `null`",
				name, block["state"], CertNeverObserved)
		}
		for _, field := range []string{"value", "observed_at", "source"} {
			v, exists := block[field]
			if !exists {
				t.Errorf("%s.%s SUMIU do JSON — a chave existe SEMPRE, para o painel nao ter de "+
					"distinguir `ausente` de `nulo`", name, field)
			}
			if v != nil {
				t.Errorf("%s.%s = %v, quero null em nunca_observado", name, field, v)
			}
		}
	}
	if n.CheckedAt != nil {
		t.Errorf("conferido_em = %v, quero null — ninguem tentou medir ainda", *n.CheckedAt)
	}
}

// THE SINGLE SOURCE (T-039/T-065), checked and not assumed: the new block
// appears in the ROUTE and in the CLI SCREEN without either surface
// enumerating a field. This test is the one that would turn red if someone,
// one day, decided to list fields in either of them.
func TestNumberAtMetaAppearsOnBOTHSurfacesWithoutAnyoneEnumerating(t *testing.T) {
	g := &numberGraph{bodyWithFields: `{"quality_rating":"YELLOW",` +
		`"whatsapp_business_manager_messaging_limit":"TIER_1K"}`}
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	watchdog := NewWatchdog(store, g.client(t))
	watchdog.Check(context.Background())

	h := NewStateHandler(store, NewAuthenticator(store), watchdog, nil, IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	// 1. THE ROUTE, which serializes the State to JSON without naming a
	// single field.
	onRoute := askState(t, h, "token-do-a", "lojinha").Body.String()
	for _, chunk := range []string{`"number_at_meta"`, `"message_limit"`, `"TIER_1K"`, `"YELLOW"`} {
		if !strings.Contains(onRoute, chunk) {
			t.Errorf("a rota nao trouxe %s:\n%s", chunk, onRoute)
		}
	}

	// 2. THE CLI SCREEN, which walks the StateRows by reflection.
	e, err := BuildState(store, watchdog, nil, IngressSource{}, nil, nil, testVersion, "lojinha", time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	var screen strings.Builder
	for _, l := range StateRows(e) {
		screen.WriteString(l.Label + " " + l.Value + "\n")
	}
	for _, chunk := range []string{"number_at_meta", "message_limit", "TIER_1K", "YELLOW", config.SourceMeasurement} {
		if !strings.Contains(screen.String(), chunk) {
			t.Errorf("a tela do CLI nao trouxe %q — campo novo tem de aparecer nas DUAS "+
				"superficies sem ninguem editar nenhuma:\n%s", chunk, screen.String())
		}
	}
}
