package outbound

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// testSmokeDestination is SYNTHETIC, on purpose (CLAUDE.md: a new fixture
// does not use the owner's real phone number).
const testSmokeDestination = "5532999990099"

// --- Fake Graph API, with the TWO routes fumaca uses --------------------
//
// The SAME idea as cmd/zapgw/fumaca_test.go (grafoFalso), here in-process
// because this package cannot import `package main`. GET answers step 2
// (checking the token); POST answers step 3 (sending).
type smokeGraph struct {
	srv        *httptest.Server
	getStatus  int
	getBody    string
	postStatus int
	postBody   string

	gets  atomic.Int64
	posts atomic.Int64
}

func workingSmokeGraph(t *testing.T) *smokeGraph {
	t.Helper()
	g := &smokeGraph{
		getStatus:  http.StatusOK,
		getBody:    `{"id":"P-lojinha","display_phone_number":"+55 32 99999-0000"}`,
		postStatus: http.StatusOK,
		postBody:   `{"messages":[{"id":"wamid.FUMACA-ROTA-TESTE"}]}`,
	}
	g.start(t)
	return g
}

func (g *smokeGraph) start(t *testing.T) {
	t.Helper()
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("chamada sem Authorization em %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			g.posts.Add(1)
			w.WriteHeader(g.postStatus)
			_, _ = w.Write([]byte(g.postBody))
			return
		}
		g.gets.Add(1)
		w.WriteHeader(g.getStatus)
		_, _ = w.Write([]byte(g.getBody))
	}))
	t.Cleanup(g.srv.Close)
}

// testSmoke builds the handler over a PAUSED instance (the birth state)
// linked to "sistema-a"/"token-do-a" — the SAME storeWithConsumer base the
// rest of the package uses.
func testSmoke(t *testing.T, g *smokeGraph) (http.Handler, *config.Store) {
	t.Helper()
	store, _ := storeWithConsumer(t)
	h := NewSmokeHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), config.NewCounter(store), 1<<20, AllTypes)
	return h, store
}

func smokeBody(slug, destination string) string {
	return `{"instancia":"` + slug + `","destino":"` + destination + `"}`
}

func askSmoke(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/fumaca", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- Verify (b): Meta accepts → ativo = 1 -----------------------------------

func TestSmokeRouteActivatesTheInstanceOnlyAfterSendingAMessage(t *testing.T) {
	g := workingSmokeGraph(t)
	h, store := testSmoke(t, g)

	rec := askSmoke(t, h, "token-do-a", smokeBody("lojinha", testSmokeDestination))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if g.gets.Load() == 0 {
		t.Error("o passo 2 nao bateu na Graph API — token revogado pelo cliente passaria despercebido")
	}
	if g.posts.Load() != 1 {
		t.Errorf("mensagens enviadas = %d, quero 1", g.posts.Load())
	}

	var resp SmokeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if resp.State != "ativa" || resp.Paused {
		t.Errorf("estado = %q pausada = %t, quero ativa/false", resp.State, resp.Paused)
	}
	if resp.AlreadyActive {
		t.Error("ja_estava_ativa = true numa instancia que NASCEU pausada")
	}
	if resp.WaMessageID != "wamid.FUMACA-ROTA-TESTE" {
		t.Errorf("wa_message_id = %q, quero o id devolvido pela Meta", resp.WaMessageID)
	}

	i, err := store.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if !i.Active {
		t.Fatal("a instancia continua pausada depois de um fumaca que passou")
	}
}

// --- Verify (a): Meta refuses → stays PAUSED and the route returns an error -----
//
// THIS IS THE TEST THAT PROTECTS THE WHOLE RULE (T-084, item 5 of Do). If the
// route activated even with Meta refusing, a consumer would register the
// wrong credential, call the route, and find out with the first real client.
func TestSmokeRouteWithSendFailureLeavesTheInstancePausedAndRefusesWithError(t *testing.T) {
	g := workingSmokeGraph(t)
	g.postStatus = http.StatusBadRequest
	g.postBody = `{"error":{"message":"numero invalido","code":131000}}`
	h, store := testSmoke(t, g)

	rec := askSmoke(t, h, "token-do-a", smokeBody("lojinha", testSmokeDestination))
	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200, quero erro (a Meta recusou o envio)")
	}

	i, err := store.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Active {
		t.Fatal("a instancia foi ATIVADA mesmo com a Meta recusando o envio")
	}
}

func TestSmokeRouteWithRefusedTokenSendsNoMessageAndDoesNotActivate(t *testing.T) {
	g := workingSmokeGraph(t)
	g.getStatus = http.StatusUnauthorized
	g.getBody = `{"error":{"message":"Invalid OAuth access token","code":190}}`
	h, store := testSmoke(t, g)

	rec := askSmoke(t, h, "token-do-a", smokeBody("lojinha", testSmokeDestination))
	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200, quero erro (a Graph API recusou o token)")
	}
	if g.posts.Load() != 0 {
		t.Errorf("mandou %d mensagem(ns) depois de o token ser recusado — o passo 2 nao abortou", g.posts.Load())
	}

	i, err := store.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Active {
		t.Fatal("a instancia foi ATIVADA com o token recusado")
	}
}

// --- Verify (c): instance already active → no send call at all -------------
//
// The assertion that CATCHES the defect is the COUNT of calls to the fake
// client — an assertion only on the response (for example
// "ja_estava_ativa == true") would pass even if the handler had sent another
// message anyway.
func TestSmokeRouteAlreadyActiveInstanceSendsNoMessageAtAll(t *testing.T) {
	g := workingSmokeGraph(t)
	h, store := testSmoke(t, g)
	if err := store.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance (preparo do teste): %v", err)
	}

	rec := askSmoke(t, h, "token-do-a", smokeBody("lojinha", testSmokeDestination))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	// THE REAL PROOF: count the calls to the fake client.
	if g.gets.Load() != 0 {
		t.Errorf("a Graph API foi consultada (GET) %d vez(es) numa instancia JA ATIVA — "+
			"nao deveria nem checar o token de novo", g.gets.Load())
	}
	if g.posts.Load() != 0 {
		t.Errorf("a Graph API recebeu %d mensagem(ns) numa instancia JA ATIVA — "+
			"esta rota gastaria mensagem paga em loop", g.posts.Load())
	}

	var resp SmokeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if !resp.AlreadyActive {
		t.Error("ja_estava_ativa = false numa instancia que ja estava ativa")
	}
	if resp.WaMessageID != "" {
		t.Errorf("wa_message_id = %q, quero vazio — nenhuma mensagem foi enviada nesta chamada", resp.WaMessageID)
	}
}

// --- The guards ---------------------------------------------------------------

func TestSmokeRouteRefusesInstanceNotOwnedByConsumerBefore404(t *testing.T) {
	g := workingSmokeGraph(t)
	h, _ := testSmoke(t, g)

	// "clinica" exists in the store (storeWithConsumer creates both), but is
	// not linked to "sistema-a" — the test proves 403, not 404, otherwise the
	// route turns into an oracle for "does this slug exist?".
	rec := askSmoke(t, h, "token-do-a", smokeBody("clinica", testSmokeDestination))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, quero 403", rec.Code)
	}
	if g.gets.Load() != 0 || g.posts.Load() != 0 {
		t.Fatalf("o gateway falou com a Meta (gets=%d posts=%d) pela instancia de outro sistema",
			g.gets.Load(), g.posts.Load())
	}
}

func TestSmokeRouteRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	g := workingSmokeGraph(t)
	h, _ := testSmoke(t, g)

	if rec := askSmoke(t, h, "", smokeBody("lojinha", testSmokeDestination)); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
	if rec := askSmoke(t, h, "token-errado", smokeBody("lojinha", testSmokeDestination)); rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status = %d, quero 401", rec.Code)
	}
	if g.gets.Load() != 0 || g.posts.Load() != 0 {
		t.Fatalf("o gateway falou com a Meta sem autenticar ninguem")
	}
}

func TestSmokeRouteRefusesIncompleteBody(t *testing.T) {
	g := workingSmokeGraph(t)
	h, _ := testSmoke(t, g)

	cases := []struct{ name, body string }{
		{"sem instancia", `{"destino":"` + testSmokeDestination + `"}`},
		{"sem destino", `{"instancia":"lojinha"}`},
		{"destino so com espaco", `{"instancia":"lojinha","destino":"   "}`},
		{"nao e JSON", `nao sou json`},
		{"corpo vazio", ``},
	}
	for _, c := range cases {
		rec := askSmoke(t, h, "token-do-a", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quero 400 (corpo = %s)", c.name, rec.Code, rec.Body.String())
		}
	}
	if g.gets.Load() != 0 || g.posts.Load() != 0 {
		t.Fatalf("o gateway falou com a Meta com pedido invalido")
	}
}

func TestSmokeRouteUnknownInstanceGives404(t *testing.T) {
	g := workingSmokeGraph(t)
	store, path := storeWithConsumer(t)
	if err := store.CreateConsumer("sistema-c", "token-do-c", []string{"clinica"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	// "clinica" really exists in the store; to prove 404 (and not 403) the
	// technique is the SAME as handler_test.go (TestHandlerLogsUnknownInstanceAs404):
	// delete the row via direct SQL after the consumer's link already exists.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco para apagar a instancia: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM instancia WHERE slug = 'clinica'`); err != nil {
		t.Fatalf("apagar instancia clinica: %v", err)
	}

	h := NewSmokeHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), config.NewCounter(store), 1<<20, AllTypes)

	rec := askSmoke(t, h, "token-do-c", smokeBody("clinica", testSmokeDestination))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quero 404 (corpo = %s)", rec.Code, rec.Body.String())
	}
}

// --- Secret never appears --------------------------------------------------

func TestSmokeRouteDoesNotReturnTheSendTokenNorAnySecret(t *testing.T) {
	g := workingSmokeGraph(t)
	h, _ := testSmoke(t, g)

	rec := askSmoke(t, h, "token-do-a", smokeBody("lojinha", testSmokeDestination))
	if strings.Contains(rec.Body.String(), "t-lojinha") {
		t.Errorf("o token de envio apareceu na resposta:\n%s", rec.Body.String())
	}
}
