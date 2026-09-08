package outbound

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// --- fake Graph API, only for this route ---------------------------------------
//
// It's the SAME idea as `cmd/grafo-falso` (T-071) — a fake Graph API that
// cannot produce a false positive —, here in-process because that binary is
// `package main` and can't be imported. It RECORDS what it received (verb,
// path, body) instead of just answering 200: this task's central finding was
// that the verb the consumer requested (`PUT`) is not the one in Meta's doc
// (`POST`), and a double that accepted any verb would leave that correction
// with no guard at all — it would only reappear against the real Meta, which
// this project's suite does not reach (see CLAUDE.md, "What the verify does NOT
// reach").

type readGraph struct {
	srv   *httptest.Server
	calls atomic.Int64
	// What the LAST call brought.
	method     string
	path       string
	body       map[string]any
	authorizes string
}

// readAcceptingGraph responds the way Meta responds to a read receipt:
// `{"success": true}` (developers.facebook.com/documentation/
// business-messaging/whatsapp/messages/mark-message-as-read, read 2026-07-28).
func readAcceptingGraph(t *testing.T) *readGraph {
	t.Helper()
	return respondingGraph(t, http.StatusOK, `{"success":true}`)
}

func respondingGraph(t *testing.T, status int, body string) *readGraph {
	t.Helper()
	g := &readGraph{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.calls.Add(1)
		g.method, g.path = r.Method, r.URL.Path
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

func testReads(t *testing.T, g *readGraph) (http.Handler, *config.Store) {
	t.Helper()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewReadsHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)
	return h, store
}

// The tests' wamid is SYNTHETIC (CLAUDE.md: a new fixture does not use the
// real phone number, and the wamid carries the phone number inside it).
const testWamid = "wamid.TESTE-LEITURA-001"

func readBody(slug, wamid string) string {
	return `{"instancia":"` + slug + `","wamid":"` + wamid + `"}`
}

func readBodyWithTyping(slug, wamid string) string {
	return `{"instancia":"` + slug + `","wamid":"` + wamid + `","digitando":true}`
}

func markRead(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/leituras", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func todaysCounters(t *testing.T, store *config.Store, slug string) map[string]int {
	t.Helper()
	m, err := store.CountersBetween(slug, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	return m
}

// --- The happy path, and the VERB ------------------------------------------------

// The test that locks in the divergence that opened the task: a consumer
// requested `PUT /{phone-number-id}/messages`, Meta's doc says `POST`. Both
// official pages were read on 2026-07-28 (the URLs are in
// internal/meta/read.go). This test asserts the verb, the path, and the
// ENTIRE body — changing any of the three turns red here instead of only
// against the real Meta.
func TestReadsSendsPOSTOnTheSendPathWithTheMetaBody(t *testing.T) {
	g := readAcceptingGraph(t)
	h, _ := testReads(t, g)

	rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if g.method != http.MethodPost {
		t.Errorf("verb = %q, want POST — Meta's doc says POST; the consumer's request PUT is wrong", g.method)
	}
	if g.path != "/P-lojinha/messages" {
		t.Errorf("path = %q, want /P-lojinha/messages (the SAME as sending)", g.path)
	}
	if g.authorizes != "Bearer t-lojinha" {
		t.Errorf("Authorization = %q, want the instance token in the HEADER", g.authorizes)
	}
	want := map[string]any{"messaging_product": "whatsapp", "status": "read", "message_id": testWamid}
	for key, value := range want {
		if g.body[key] != value {
			t.Errorf("body[%q] = %#v, want %#v", key, g.body[key], value)
		}
	}
	if len(g.body) != len(want) {
		t.Errorf("the body has %d fields (%v), want exactly %d", len(g.body), g.body, len(want))
	}
}

// The success response does NOT have `wa_message_id`, and that's contract: no
// message was born, and inventing an id to "keep the shape" of the send would
// be lying to save a line of doc.
func TestReadsAnswersOKWithoutInventingAMessageID(t *testing.T) {
	g := readAcceptingGraph(t)
	h, _ := testReads(t, g)

	rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Errorf(`corpo["ok"] = %#v, quero true`, body["ok"])
	}
	if _, has := body["wa_message_id"]; has {
		t.Errorf("the response brought wa_message_id = %#v — marking as read does NOT create a message", body["wa_message_id"])
	}
}

// --- T-147: the `digitando` field -------------------------------------------------

// TestReadsWithTypingCarriesTheTypingIndicator is the positive case: the
// OPTIONAL `digitando:true` field adds `typing_indicator.type == "text"` to
// the SAME POST as the read receipt — the Cloud API has no dedicated endpoint
// for the indicator (see the comment on MarkAsRead in
// internal/meta/read.go).
func TestReadsWithTypingCarriesTheTypingIndicator(t *testing.T) {
	g := readAcceptingGraph(t)
	h, _ := testReads(t, g)

	rec := markRead(t, h, "token-do-a", readBodyWithTyping("lojinha", testWamid))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	ti, ok := g.body["typing_indicator"].(map[string]any)
	if !ok {
		t.Fatalf("body[%q] = %#v, want a typing_indicator object", "typing_indicator", g.body["typing_indicator"])
	}
	if ti["type"] != "text" {
		t.Errorf(`typing_indicator.type = %#v, quero "text"`, ti["type"])
	}
}

// TestReadsWithoutTypingDoesNotCarryTheTypingIndicator is the NON-REGRESSION:
// the body WITHOUT `digitando` (neither present nor `false`) has to come out
// identical to always — without the `typing_indicator` key, because this path
// has already been in production since T-075.
func TestReadsWithoutTypingDoesNotCarryTheTypingIndicator(t *testing.T) {
	cases := []string{
		readBody("lojinha", testWamid),                                          // without the field
		`{"instancia":"lojinha","wamid":"` + testWamid + `","digitando":false}`, // explicit false
	}
	for _, body := range cases {
		g := readAcceptingGraph(t)
		h, _ := testReads(t, g)

		rec := markRead(t, h, "token-do-a", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("body %q: status = %d, want 200", body, rec.Code)
		}
		if _, has := g.body["typing_indicator"]; has {
			t.Errorf("body %q: Meta received typing_indicator = %#v, want absent", body, g.body["typing_indicator"])
		}
	}
}

// TestReadsRefusesTypingWithoutWamid: there is no standalone "digitando",
// outside a response to a RECEIVED message — `wamid` remains mandatory even
// with `digitando:true`, and Meta never gets called at all.
func TestReadsRefusesTypingWithoutWamid(t *testing.T) {
	g := readAcceptingGraph(t)
	h, _ := testReads(t, g)

	rec := markRead(t, h, "token-do-a", `{"instancia":"lojinha","digitando":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body = %s)", rec.Code, rec.Body.String())
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("the gateway called Meta %d time(s) without a wamid", n)
	}
}

// --- MANDATORY MUTATION 1: the counter key ---------------------------------

// 🔴 THE MOST IMPORTANT ITEM OF T-075. Marking as read does not produce a
// conversation; if it counts as `enviadas`, it inflates the cost projection of
// BOTH consumers and T-063 (billing-category count) is born lying.
//
// This test asserts WHICH key went up, not that "some counter went up" — a
// total test doesn't distinguish the defect, and the defect is exactly that.
// Swapping config.CounterReadsMarked for config.CounterSent in
// reads_handler.go turns this test RED on both assertions.
func TestReadsCountsItsOwnKeyAndNeverSent(t *testing.T) {
	g := readAcceptingGraph(t)
	h, store := testReads(t, g)

	if rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	m := todaysCounters(t, store, "lojinha")
	if m[config.CounterReadsMarked] != 1 {
		t.Errorf("reads_marked = %d, want 1", m[config.CounterReadsMarked])
	}
	if m[config.CounterSent] != 0 {
		t.Errorf("sent = %d, want 0 — marking as read is NOT sending, and counting it here inflates "+
			"the consumers' cost projection", m[config.CounterSent])
	}
	if m[config.CounterSendFailures] != 0 {
		t.Errorf("send_failures = %d, want 0 — this route never touches the send keys",
			m[config.CounterSendFailures])
	}
	if m[config.CounterReceived] != 0 || m[config.CounterDelivered] != 0 {
		t.Errorf("received = %d, delivered = %d, want 0 and 0 — nothing on this route can be read "+
			"as 'there was inbound'", m[config.CounterReceived], m[config.CounterDelivered])
	}
}

// The failure also has its own key, for the same symmetry-with-send reason:
// `falhas_de_envio` means "a SEND attempt failed", and a marking that Meta
// rejected is not that.
func TestReadsCountsFailureWithItsOwnKeyWhenMetaRefuses(t *testing.T) {
	g := respondingGraph(t, http.StatusBadRequest, `{"error":{"message":"Parameter value is not valid","code":131009}}`)
	h, store := testReads(t, g)

	if rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid)); rec.Code == http.StatusOK {
		t.Fatalf("status = 200, want an error (Meta refused)")
	}

	m := todaysCounters(t, store, "lojinha")
	if m[config.CounterReadFailures] != 1 {
		t.Errorf("read_failures = %d, want 1", m[config.CounterReadFailures])
	}
	if m[config.CounterReadsMarked] != 0 {
		t.Errorf("reads_marked = %d, want 0 (Meta refused)", m[config.CounterReadsMarked])
	}
	if m[config.CounterSendFailures] != 0 {
		t.Errorf("send_failures = %d, want 0", m[config.CounterSendFailures])
	}
}

// --- MANDATORY MUTATION 2: there is no Idempotency-Key -----------------------------

// Marking twice is harmless, and both consumers asked to not have to keep
// "already marked" state — the operator opens the same conversation ten times
// a day. Requiring `Idempotency-Key` in the handler turns this test RED (both
// calls become 400), and that's the mutation the task asks for.
//
// Both calls go WITHOUT any idempotency header, on purpose.
func TestReadsDoesNotRequireIdempotencyKeyAndMarkingTwiceWorksBothTimes(t *testing.T) {
	g := readAcceptingGraph(t)
	h, store := testReads(t, g)

	for i, turn := range []string{"first", "second"} {
		rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s mark: status = %d, body = %s — repeating the SAME mark has to succeed, "+
				"with no idempotency key", turn, rec.Code, rec.Body.String())
		}
		if n := g.calls.Load(); n != int64(i+1) {
			t.Fatalf("%s mark: Meta received %d call(s), want %d — the gateway does not keep "+
				"'already marked' state", turn, n, i+1)
		}
	}

	if m := todaysCounters(t, store, "lojinha"); m[config.CounterReadsMarked] != 2 {
		t.Errorf("reads_marked = %d, want 2 (two marks, two facts)", m[config.CounterReadsMarked])
	}
}

// --- The guards ----------------------------------------------------------------

// REQUIREMENT 3, end to end, on this route: system A's token does not mark a
// read through B's number — and Meta doesn't even get called.
func TestReadsRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	g := readAcceptingGraph(t)
	h, _ := testReads(t, g)

	rec := markRead(t, h, "token-do-a", readBody("clinica", testWamid))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("the gateway CALLED META (%d time(s)) for another system's instance", n)
	}
}

func TestReadsRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	g := readAcceptingGraph(t)
	h, _ := testReads(t, g)

	if rec := markRead(t, h, "", readBody("lojinha", testWamid)); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	if rec := markRead(t, h, "token-errado", readBody("lojinha", testWamid)); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("the gateway called Meta %d time(s) without authenticating anyone", n)
	}
}

func TestReadsRefusesIncompleteBody(t *testing.T) {
	g := readAcceptingGraph(t)
	h, _ := testReads(t, g)

	cases := []struct{ name, body string }{
		{"sem instancia", `{"wamid":"` + testWamid + `"}`},
		{"sem wamid", `{"instancia":"lojinha"}`},
		{"wamid so com espaco", `{"instancia":"lojinha","wamid":"   "}`},
		{"nao e JSON", `nao sou json`},
		{"corpo vazio", ``},
	}
	for _, c := range cases {
		rec := markRead(t, h, "token-do-a", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body = %s)", c.name, rec.Code, rec.Body.String())
		}
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("the gateway called Meta %d time(s) with an invalid request", n)
	}
}

// 404 for the instance, not 403: whoever got here has ALREADY authenticated
// and ALREADY passed the link check — what's left is misalignment in our own
// configuration, which is worth investigating.
//
// The scenario is built by deleting the `instancia` row via DIRECT SQL, the
// SAME technique as TestHandlerLogsUnknownInstanceAs404
// (handler_test.go): CreateConsumer has a FOREIGN KEY against `instancia`, so
// there is no way to link a consumer to a slug that never existed.
func TestReadsUnknownInstanceGives404(t *testing.T) {
	g := readAcceptingGraph(t)
	store, path := storeWithConsumer(t)
	if err := store.CreateConsumer("sistema-c", "token-do-c", []string{"clinica"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to delete the instance: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM instancia WHERE slug = 'clinica'`); err != nil {
		t.Fatalf("delete instance clinica: %v", err)
	}

	h := NewReadsHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	rec := markRead(t, h, "token-do-c", readBody("clinica", testWamid))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body = %s)", rec.Code, rec.Body.String())
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("nonexistent instance and the gateway called Meta %d time(s)", n)
	}
}

// A paused instance does not talk to Meta: retryable 503, the SAME outcome as
// send — whoever paused it will unpause it.
func TestReadsPausedInstanceGives503(t *testing.T) {
	g := readAcceptingGraph(t)
	store, _ := storeWithConsumer(t) // without activateInstance: it is BORN paused
	h := NewReadsHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("paused instance and the gateway called Meta %d time(s)", n)
	}
}

// --- The error table the contract promises ------------------------------------

func TestReadsTranslatesTheMetaErrorIntoTheContractStatus(t *testing.T) {
	cases := []struct {
		name       string
		metaStatus int
		metaBody   string
		wantStatus int
		wantClass  string
		wantCode   int
	}{
		{
			// PERMANENT error: invalid wamid, or older than the 30 days Meta
			// accepts. Retrying repeats the error.
			name: "permanent", metaStatus: http.StatusBadRequest,
			metaBody:   `{"error":{"message":"Parameter value is not valid","code":131009}}`,
			wantStatus: http.StatusBadRequest, wantClass: "permanent", wantCode: 131009,
		},
		{
			// RETRYABLE error: Meta went down. Re-queue.
			name: "retentavel (5xx da Meta)", metaStatus: http.StatusBadGateway,
			metaBody:   `{"error":{"message":"Service temporarily unavailable","code":2}}`,
			wantStatus: http.StatusServiceUnavailable, wantClass: "retryable", wantCode: 2,
		},
		{
			// Rate limit is also retryable — and it's the case T-075 asked to
			// measure whether thirteen markings in a row would hit it.
			name: "retentavel (limite de taxa)", metaStatus: http.StatusTooManyRequests,
			metaBody:   `{"error":{"message":"Too many requests","code":130429}}`,
			wantStatus: http.StatusServiceUnavailable, wantClass: "retryable", wantCode: 130429,
		},
		{
			// Credential: not the consumer's token, it's the one the GATEWAY
			// stores — that's why it's 502 config, not 401 (which would send
			// them to check their own token, which is correct).
			name: "config (token da instancia recusado)", metaStatus: http.StatusUnauthorized,
			metaBody:   `{"error":{"message":"Invalid OAuth access token","code":190}}`,
			wantStatus: http.StatusBadGateway, wantClass: "config", wantCode: 190,
		},
	}

	for _, c := range cases {
		g := respondingGraph(t, c.metaStatus, c.metaBody)
		h, _ := testReads(t, g)

		var logBuf bytes.Buffer
		log.SetOutput(&logBuf)
		rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
		log.SetOutput(logStdout)

		if rec.Code != c.wantStatus {
			t.Errorf("%s: status = %d, want %d (body = %s)", c.name, rec.Code, c.wantStatus, rec.Body.String())
		}
		var resp errorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: body does not deserialize: %v (body = %q)", c.name, err, rec.Body.String())
		}
		if resp.Error.Class != c.wantClass {
			t.Errorf("%s: class = %q, want %q", c.name, resp.Error.Class, c.wantClass)
		}
		if resp.Error.MetaCode != c.wantCode {
			t.Errorf("%s: meta_code = %d, want %d (it travels to whoever has their own rule)",
				c.name, resp.Error.MetaCode, c.wantCode)
		}
		if c.wantClass == "config" && !strings.Contains(logBuf.String(), "ALARME") {
			t.Errorf("%s: credential refused and nothing alarmed in the log — only a person fixes this", c.name)
		}
	}
}

// A TRANSPORT failure (Meta didn't respond at all) becomes 502 `unknown`,
// like on send — but with the OPPOSITE instruction in the text: here retrying
// is safe, because marking twice has no side effect. A consumer who reads the
// send message ("don't resend") and applies it here would leave the
// conversation with two gray checkmarks forever, out of fear of a duplicate
// that doesn't exist.
func TestReadsTransportFailureGives502TellingToRetry(t *testing.T) {
	g := readAcceptingGraph(t)
	h, store := testReads(t, g)
	g.srv.Close() // Meta disappears BEFORE the call

	rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body = %s)", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v", err)
	}
	if resp.Error.Class != "unknown" {
		t.Errorf("class = %q, want unknown", resp.Error.Class)
	}
	if !strings.Contains(resp.Error.Message, "repetir e seguro") {
		t.Errorf("mensagem = %q — ela precisa dizer que repetir e seguro, senao o consumidor "+
			"aplica aqui a regra do ENVIO ('nao reenvie') e a conversa nunca e marcada", resp.Error.Message)
	}
	if m := todaysCounters(t, store, "lojinha"); m[config.CounterReadFailures] != 1 {
		t.Errorf("read_failures = %d, want 1", m[config.CounterReadFailures])
	}
}

// --- Secrets and personal data in the log ---------------------------------------------

// The `wamid` CARRIES THE OTHER SIDE'S PHONE NUMBER encoded inside it
// (docs/ARMADILHAS.md, "the wamid carries the recipient's phone number
// inside it"). No log line on this route can contain it — not even on the rejection
// path, which is where the temptation to "log the whole request to debug" lives.
func TestReadsNeverLogsTheWamidNorTheToken(t *testing.T) {
	g := respondingGraph(t, http.StatusBadRequest, `{"error":{"message":"Parameter value is not valid","code":131009}}`)
	h, _ := testReads(t, g)

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	// A request that passes through ALL the guards and still gets rejected by
	// Meta, and one that dies at the link guard: both log paths.
	markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
	markRead(t, h, "token-do-a", readBody("clinica", testWamid))
	log.SetOutput(logStdout)

	if strings.Contains(logBuf.String(), testWamid) {
		t.Errorf("o wamid apareceu no log — ele carrega o telefone do cliente:\n%s", logBuf.String())
	}
	if strings.Contains(logBuf.String(), "token-do-a") || strings.Contains(logBuf.String(), "t-lojinha") {
		t.Errorf("um token apareceu no log:\n%s", logBuf.String())
	}
}

// --- The counter never changes the response (T-035) -------------------------------------

func TestReadsCounterFailureDoesNotChangeTheStatus(t *testing.T) {
	g := readAcceptingGraph(t)
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	h := NewReadsHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), 1<<20,
		config.NewCounterWithStore(alwaysFailingOutboundCounter{}), WhatsAppOnly)

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	rec := markRead(t, h, "token-do-a", readBody("lojinha", testWamid))
	log.SetOutput(logStdout)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s — a COUNTER failure cannot change the mark response",
			rec.Code, rec.Body.String())
	}
}
