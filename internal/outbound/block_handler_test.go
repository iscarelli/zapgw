package outbound

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// --- Fake Graph API, only for this route -----------------------------------
//
// The SAME idea as readGraph (reads_handler_test.go): records what it
// received instead of just answering 200, so the tests can assert the exact
// verb, path, body, and QUERY.

type blockGraph struct {
	srv        *httptest.Server
	calls      atomic.Int64
	method     string
	path       string
	query      string
	body       map[string]any
	authorizes string
}

func respondingBlockGraph(t *testing.T, status int, body string) *blockGraph {
	t.Helper()
	g := &blockGraph{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.calls.Add(1)
		g.method, g.path, g.query = r.Method, r.URL.Path, r.URL.RawQuery
		g.authorizes = r.Header.Get("Authorization")
		var read map[string]any
		if err := json.NewDecoder(r.Body).Decode(&read); err == nil {
			g.body = read
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func testBlock(t *testing.T, g *blockGraph) (http.Handler, *config.Store) {
	t.Helper()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewBlockHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)
	return h, store
}

func askBlock(t *testing.T, h http.Handler, method, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/bloqueios", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func listBlocks(t *testing.T, h http.Handler, token string, q url.Values) *httptest.ResponseRecorder {
	t.Helper()
	target := "/v1/bloqueios"
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- POST: body and canonicalization ----------------------------------------

// TestBlockPostBuildsTheBodyAndCanonicalizesThePhone: the phone WITHOUT the ninth
// digit ("551199990000") has to reach Meta ALREADY canonicalized
// ("5511999990000") — the SAME rule as sending. Sending without
// canonicalizing would silently block ANOTHER number.
func TestBlockPostBuildsTheBodyAndCanonicalizesThePhone(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK,
		`{"messaging_product":"whatsapp","block_users":{"added_users":[{"input":"5511999990000","wa_id":"5511999990000"}]}}`)
	h, _ := testBlock(t, g)

	body := `{"instancia":"lojinha","telefones":["551199990000"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if g.method != http.MethodPost {
		t.Errorf("metodo = %q, quero POST", g.method)
	}
	if g.path != "/P-lojinha/block_users" {
		t.Errorf("caminho = %q, quero /P-lojinha/block_users", g.path)
	}
	if g.authorizes != "Bearer t-lojinha" {
		t.Errorf("Authorization = %q, quero o token da instancia no HEADER", g.authorizes)
	}
	if g.body["messaging_product"] != "whatsapp" {
		t.Errorf(`corpo["messaging_product"] = %#v, quero "whatsapp"`, g.body["messaging_product"])
	}
	catalog, ok := g.body["block_users"].([]any)
	if !ok || len(catalog) != 1 {
		t.Fatalf("corpo[block_users] = %#v, quero uma lista com 1 item", g.body["block_users"])
	}
	item, ok := catalog[0].(map[string]any)
	if !ok || item["user"] != "5511999990000" {
		t.Fatalf("item = %#v, quero user=5511999990000 (CANONIZADO, com o nono digito)", catalog[0])
	}
}

// --- DELETE: same body, different verb ---------------------------------------

func TestBlockDeleteUsesTheDeleteMethodWithTheSameBody(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK,
		`{"messaging_product":"whatsapp","block_users":{"removed_users":[{"input":"5511999990000","wa_id":"5511999990000"}]}}`)
	h, _ := testBlock(t, g)

	body := `{"instancia":"lojinha","telefones":["5511999990000"]}`
	rec := askBlock(t, h, http.MethodDelete, "token-do-a", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if g.method != http.MethodDelete {
		t.Errorf("metodo = %q, quero DELETE", g.method)
	}
	if g.path != "/P-lojinha/block_users" {
		t.Errorf("caminho = %q, quero /P-lojinha/block_users (o MESMO do POST)", g.path)
	}
	catalog, ok := g.body["block_users"].([]any)
	if !ok || len(catalog) != 1 {
		t.Fatalf("corpo[block_users] = %#v", g.body["block_users"])
	}

	var resp blockOperationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if resp.Operation != "desbloquear" {
		t.Errorf("operacao = %q, quero desbloquear", resp.Operation)
	}
	if len(resp.Processed) != 1 || resp.Processed[0].Phone != "5511999990000" {
		t.Errorf("processados = %+v", resp.Processed)
	}
}

// --- THE MOST IMPORTANT TEST OF THIS TASK: partial success turns into a
// response PER NUMBER, never a plain 200. ------------------------------------

func TestBlockPartialSuccessBecomesPerNumberResponse(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{
		"messaging_product":"whatsapp",
		"block_users":{
			"added_users":[{"input":"5511999990000","wa_id":"5511999990000"}],
			"failed_users":[{"input":"5511999990001","wa_id":"5511999990001","errors":[
				{"message":"nao mandou mensagem nas ultimas 24h","code":139001,
				 "error_data":{"details":"janela de 24h fechada"}}
			]}]
		}
	}`)
	h, _ := testBlock(t, g)

	body := `{"instancia":"lojinha","telefones":["5511999990000","5511999990001"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)

	// The WHOLE CALL has to answer 200: Meta's envelope was 200, and a
	// number refused INSIDE it is not a failure of the CALL.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (a Meta respondeu 200 no envelope); corpo = %s", rec.Code, rec.Body.String())
	}

	var resp blockOperationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if resp.Operation != "bloquear" {
		t.Errorf("operacao = %q, quero bloquear", resp.Operation)
	}
	if len(resp.Processed) != 1 || resp.Processed[0].Phone != "5511999990000" {
		t.Fatalf("processados = %+v, quero exatamente 5511999990000", resp.Processed)
	}
	if len(resp.Failures) != 1 {
		t.Fatalf("falhas = %+v, quero exatamente uma", resp.Failures)
	}
	f := resp.Failures[0]
	if f.Phone != "5511999990001" {
		t.Errorf("falhas[0].telefone = %q, quero 5511999990001", f.Phone)
	}
	if f.MetaCode != 139001 {
		t.Errorf("falhas[0].codigo_meta = %d, quero 139001", f.MetaCode)
	}
	if f.Message != "nao mandou mensagem nas ultimas 24h" {
		t.Errorf("falhas[0].mensagem = %q", f.Message)
	}
	if f.MetaDetail != "janela de 24h fechada" {
		t.Errorf("falhas[0].detalhe_meta = %q", f.MetaDetail)
	}
}

// --- Entry ceiling: 1,000 per call --------------------------------------------

func TestBlockAboveTheCapIsRefusedAtTheDoor(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)

	phones := make([]string, 1001)
	for i := range phones {
		phones[i] = "551199990" + padZero(i, 4)
	}
	raw, err := json.Marshal(map[string]any{"instancia": "lojinha", "telefones": phones})
	if err != nil {
		t.Fatalf("montar corpo: %v", err)
	}

	rec := askBlock(t, h, http.MethodPost, "token-do-a", string(raw))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("a Meta foi chamada %d vez(es) com um pedido que ja devia ter sido recusado na entrada", n)
	}
	errBody := decodeErrorOrFail(t, rec)
	if !strings.Contains(errBody.Error.Message, "1001") || !strings.Contains(errBody.Error.Message, "1000") {
		t.Errorf("mensagem = %q, quero que diga quantos vieram (1001) e o maximo (1000)", errBody.Error.Message)
	}
}

func padZero(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// --- Instagram instance: refuses 400/config, T-097/T-111 ---------------------

func TestBlockRefusesInstagramInstanceWith400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	srv := uncallableMeta(t)
	h := NewBlockHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	body := `{"instancia":"insta-loja","telefones":["5511999990000"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("classe = %q, quero config", errBody.Error.Class)
	}
	if !strings.Contains(errBody.Error.Message, `"instagram"`) {
		t.Errorf("a mensagem nao nomeia o tipo recusado: %q", errBody.Error.Message)
	}
}

// GET also refuses Instagram, by the same type rule — WhatsAppOnly applies to
// the three routes of this handler.
func TestBlockListRefusesInstagramInstanceWith400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	srv := uncallableMeta(t)
	h := NewBlockHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	rec := listBlocks(t, h, "token-do-a", url.Values{"instancia": {"insta-loja"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("classe = %q, quero config", errBody.Error.Class)
	}
}

// --- GET: forwards the cursors -------------------------------------------------

func TestBlockListPassesThroughTheCursors(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{
		"data":[{"messaging_product":"whatsapp","wa_id":"5511999990000"}],
		"paging":{"cursors":{"after":"CURSOR_DEPOIS","before":"CURSOR_ANTES"}}
	}`)
	h, _ := testBlock(t, g)

	rec := listBlocks(t, h, "token-do-a", url.Values{
		"instancia": {"lojinha"}, "limit": {"50"}, "after": {"APOS"}, "before": {"ANTES"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if g.method != http.MethodGet {
		t.Errorf("metodo = %q, quero GET", g.method)
	}
	if g.path != "/P-lojinha/block_users" {
		t.Errorf("caminho = %q, quero /P-lojinha/block_users", g.path)
	}
	q, err := url.ParseQuery(g.query)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", g.query, err)
	}
	if q.Get("limit") != "50" || q.Get("after") != "APOS" || q.Get("before") != "ANTES" {
		t.Fatalf("query recebida pela Meta = %s, quero limit/after/before repassados", g.query)
	}

	var resp blockListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if resp.Total != 1 || len(resp.Blocked) != 1 || resp.Blocked[0].WaID != "5511999990000" {
		t.Errorf("bloqueados = %+v", resp.Blocked)
	}
	if resp.CursorAfter != "CURSOR_DEPOIS" || resp.CursorBefore != "CURSOR_ANTES" {
		t.Errorf("cursores = depois=%q antes=%q, quero os da resposta da Meta", resp.CursorAfter, resp.CursorBefore)
	}
}

// --- The usual guards -----------------------------------------------------------

func TestBlockRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)

	body := `{"instancia":"clinica","telefones":["5511999990000"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, quero 403", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("o gateway chamou a Meta %d vez(es) pela instancia de outro sistema", n)
	}
}

func TestBlockRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)

	body := `{"instancia":"lojinha","telefones":["5511999990000"]}`
	if rec := askBlock(t, h, http.MethodPost, "", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
	if rec := askBlock(t, h, http.MethodPost, "token-errado", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status = %d, quero 401", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("o gateway chamou a Meta %d vez(es) sem autenticar ninguem", n)
	}
}

func TestBlockRefusesInvalidBody(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)

	cases := []struct{ name, body string }{
		{"sem instancia", `{"telefones":["5511999990000"]}`},
		{"sem telefones", `{"instancia":"lojinha"}`},
		{"telefones vazio", `{"instancia":"lojinha","telefones":[]}`},
		{"telefone sem digito", `{"instancia":"lojinha","telefones":["abc"]}`},
		{"nao e JSON", `nao sou json`},
		{"corpo vazio", ``},
	}
	for _, c := range cases {
		rec := askBlock(t, h, http.MethodPost, "token-do-a", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quero 400 (corpo = %s)", c.name, rec.Code, rec.Body.String())
		}
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("o gateway chamou a Meta %d vez(es) com pedido invalido", n)
	}
}

func TestBlockPausedInstanceGives503(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	store, _ := storeWithConsumer(t) // without activateInstance: born paused
	h := NewBlockHandler(store, NewAuthenticator(store), meta.NewClient(g.srv.Client(), g.srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	body := `{"instancia":"lojinha","telefones":["5511999990000"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, quero 503", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("instancia pausada e o gateway chamou a Meta %d vez(es)", n)
	}
}

// --- Transport failure: repeating is safe (no side effect) -------------------

func TestBlockTransportFailureGives502TellingToRetry(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)
	g.srv.Close() // Meta disappears BEFORE the call

	body := `{"instancia":"lojinha","telefones":["5511999990000"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502; corpo = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "unknown" {
		t.Errorf("classe = %q, quero desconhecido", errBody.Error.Class)
	}
	if !strings.Contains(errBody.Error.Message, "repetir e seguro") {
		t.Errorf("mensagem = %q, quero que diga que repetir e seguro", errBody.Error.Message)
	}
}
