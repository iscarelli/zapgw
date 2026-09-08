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
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if g.method != http.MethodPost {
		t.Errorf("method = %q, want POST", g.method)
	}
	if g.path != "/P-lojinha/block_users" {
		t.Errorf("path = %q, want /P-lojinha/block_users", g.path)
	}
	if g.authorizes != "Bearer t-lojinha" {
		t.Errorf("Authorization = %q, want the instance token in the HEADER", g.authorizes)
	}
	if g.body["messaging_product"] != "whatsapp" {
		t.Errorf(`body["messaging_product"] = %#v, want "whatsapp"`, g.body["messaging_product"])
	}
	catalog, ok := g.body["block_users"].([]any)
	if !ok || len(catalog) != 1 {
		t.Fatalf("body[block_users] = %#v, want a list with 1 item", g.body["block_users"])
	}
	item, ok := catalog[0].(map[string]any)
	if !ok || item["user"] != "5511999990000" {
		t.Fatalf("item = %#v, want user=5511999990000 (CANONICALIZED, with the ninth digit)", catalog[0])
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
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if g.method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", g.method)
	}
	if g.path != "/P-lojinha/block_users" {
		t.Errorf("path = %q, want /P-lojinha/block_users (the SAME as POST)", g.path)
	}
	catalog, ok := g.body["block_users"].([]any)
	if !ok || len(catalog) != 1 {
		t.Fatalf("body[block_users] = %#v", g.body["block_users"])
	}

	var resp blockOperationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.Operation != "desbloquear" {
		t.Errorf("operation = %q, want desbloquear", resp.Operation)
	}
	if len(resp.Processed) != 1 || resp.Processed[0].Phone != "5511999990000" {
		t.Errorf("processed = %+v", resp.Processed)
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
		t.Fatalf("status = %d, want 200 (Meta answered 200 in the envelope); body = %s", rec.Code, rec.Body.String())
	}

	var resp blockOperationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.Operation != "bloquear" {
		t.Errorf("operation = %q, want bloquear", resp.Operation)
	}
	if len(resp.Processed) != 1 || resp.Processed[0].Phone != "5511999990000" {
		t.Fatalf("processed = %+v, want exactly 5511999990000", resp.Processed)
	}
	if len(resp.Failures) != 1 {
		t.Fatalf("failures = %+v, want exactly one", resp.Failures)
	}
	f := resp.Failures[0]
	if f.Phone != "5511999990001" {
		t.Errorf("failures[0].phone = %q, want 5511999990001", f.Phone)
	}
	if f.MetaCode != 139001 {
		t.Errorf("failures[0].meta_code = %d, want 139001", f.MetaCode)
	}
	if f.Message != "nao mandou mensagem nas ultimas 24h" {
		t.Errorf("failures[0].message = %q", f.Message)
	}
	if f.MetaDetail != "janela de 24h fechada" {
		t.Errorf("failures[0].meta_detail = %q", f.MetaDetail)
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
		t.Fatalf("build body: %v", err)
	}

	rec := askBlock(t, h, http.MethodPost, "token-do-a", string(raw))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("Meta was called %d time(s) with a request that should already have been refused at the door", n)
	}
	errBody := decodeErrorOrFail(t, rec)
	if !strings.Contains(errBody.Error.Message, "1001") || !strings.Contains(errBody.Error.Message, "1000") {
		t.Errorf("message = %q, want it to say how many came (1001) and the maximum (1000)", errBody.Error.Message)
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
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("class = %q, want config", errBody.Error.Class)
	}
	if !strings.Contains(errBody.Error.Message, `"instagram"`) {
		t.Errorf("the message does not name the refused type: %q", errBody.Error.Message)
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
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("class = %q, want config", errBody.Error.Class)
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
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if g.method != http.MethodGet {
		t.Errorf("method = %q, want GET", g.method)
	}
	if g.path != "/P-lojinha/block_users" {
		t.Errorf("path = %q, want /P-lojinha/block_users", g.path)
	}
	q, err := url.ParseQuery(g.query)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", g.query, err)
	}
	if q.Get("limit") != "50" || q.Get("after") != "APOS" || q.Get("before") != "ANTES" {
		t.Fatalf("query received by Meta = %s, want limit/after/before passed through", g.query)
	}

	var resp blockListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.Total != 1 || len(resp.Blocked) != 1 || resp.Blocked[0].WaID != "5511999990000" {
		t.Errorf("blocked = %+v", resp.Blocked)
	}
	if resp.CursorAfter != "CURSOR_DEPOIS" || resp.CursorBefore != "CURSOR_ANTES" {
		t.Errorf("cursors = after=%q before=%q, want the cursors returned by Meta", resp.CursorAfter, resp.CursorBefore)
	}
}

// --- The usual guards -----------------------------------------------------------

func TestBlockRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)

	body := `{"instancia":"clinica","telefones":["5511999990000"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("the gateway called Meta %d time(s) for an instance of another system", n)
	}
}

func TestBlockRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)

	body := `{"instancia":"lojinha","telefones":["5511999990000"]}`
	if rec := askBlock(t, h, http.MethodPost, "", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	if rec := askBlock(t, h, http.MethodPost, "token-errado", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("the gateway called Meta %d time(s) without authenticating anyone", n)
	}
}

func TestBlockRefusesInvalidBody(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	h, _ := testBlock(t, g)

	cases := []struct{ name, body string }{
		{"missing instancia", `{"telefones":["5511999990000"]}`},
		{"missing telefones", `{"instancia":"lojinha"}`},
		{"empty telefones", `{"instancia":"lojinha","telefones":[]}`},
		{"phone without a digit", `{"instancia":"lojinha","telefones":["abc"]}`},
		{"not JSON", `not json`},
		{"empty body", ``},
	}
	for _, c := range cases {
		rec := askBlock(t, h, http.MethodPost, "token-do-a", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body = %s)", c.name, rec.Code, rec.Body.String())
		}
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("the gateway called Meta %d time(s) with an invalid request", n)
	}
}

func TestBlockPausedInstanceGives503(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{}}`)
	store, _ := storeWithConsumer(t) // without activateInstance: born paused
	h := NewBlockHandler(store, NewAuthenticator(store), meta.NewClient(g.srv.Client(), g.srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	body := `{"instancia":"lojinha","telefones":["5511999990000"]}`
	rec := askBlock(t, h, http.MethodPost, "token-do-a", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("instance paused and the gateway called Meta %d time(s)", n)
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
		t.Fatalf("status = %d, want 502; body = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "unknown" {
		t.Errorf("class = %q, want unknown", errBody.Error.Class)
	}
	if !strings.Contains(errBody.Error.Message, "repetir e seguro") {
		t.Errorf("message = %q, want it to say that retrying is safe", errBody.Error.Message)
	}
}
