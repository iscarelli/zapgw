package outbound

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// assembles a handler whose "Meta" is the given test server.
func testHandler(t *testing.T, metaSrv *httptest.Server) (http.Handler, *config.Store) {
	t.Helper()
	store, path := storeWithConsumer(t)
	// The instance needs to be ACTIVE to send.
	activateInstance(t, path, "lojinha")

	h := NewHandler(store, NewAuthenticator(store),
		meta.NewClient(metaSrv.Client(), metaSrv.URL), 1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)
	return h, store
}

func acceptingMeta(id string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"id":"` + id + `"}]}`))
	}))
}

func ask(t *testing.T, h http.Handler, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const textBody = `{"instancia":"lojinha","para":"5511999990000","tipo":"texto","texto":"oi"}`

// textBodyHash is the hash of the SAME request that textBody carries (already
// normalized — textBody has no leading/trailing space in any field), for the
// A-1 tests to check the key's state by calling ReserveIdempotency
// directly, with the (consumer, key) pair and the hash the handler would have calculated.
var textBodyHash = RequestHash(Request{
	Instance: "lojinha", To: "5511999990000", Type: "texto", Text: "oi"})

func TestHandlerSendsAndReturnsTheID(t *testing.T) {
	srv := acceptingMeta("wamid.OK")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k1", textBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.WaMessageID != "wamid.OK" {
		t.Fatalf("wa_message_id = %q", resp.WaMessageID)
	}
}

func TestHandlerRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	srv := acceptingMeta("wamid.X")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	if rec := ask(t, h, "", "k1", textBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	if rec := ask(t, h, "token-errado", "k1", textBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", rec.Code)
	}
}

// REQUIREMENT 3, end to end: system A's token does not send through system B's number.
func TestHandlerRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	var calledMeta bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledMeta = true
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.X"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	body := `{"instancia":"clinica","para":"5511999990000","tipo":"texto","texto":"oi"}`
	rec := ask(t, h, "token-do-a", "k1", body)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if calledMeta {
		t.Fatal("the gateway CALLED META for another system's instance")
	}
}

func TestHandlerRequiresIdempotencyKey(t *testing.T) {
	srv := acceptingMeta("wamid.X")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "", textBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — without the key there is no way to prevent a duplicate", rec.Code)
	}
}

// The central promise of Idempotency-Key: same key twice, ONE message on the
// customer's phone.
func TestHandlerDoesNotSendTwiceWithTheSameKey(t *testing.T) {
	var sends int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sends++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.UNICO"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	first := ask(t, h, "token-do-a", "mesma-chave", textBody)
	second := ask(t, h, "token-do-a", "mesma-chave", textBody)

	if sends != 1 {
		t.Fatalf("Meta received %d sends, want 1", sends)
	}
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d and %d, want 200 on both", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("different responses:\n1: %s\n2: %s", first.Body, second.Body)
	}
}

func TestHandlerReleasesTheKeyWhenMetaRefuses(t *testing.T) {
	// If the key didn't become valid again, the consumer would lose the message
	// forever for having tried once.
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"tente depois","code":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.SEGUNDA"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	first := ask(t, h, "token-do-a", "k1", textBody)
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("1st: status = %d, want 503 (retryable)", first.Code)
	}

	second := ask(t, h, "token-do-a", "k1", textBody)
	if second.Code != http.StatusOK {
		t.Fatalf("2nd: status = %d — the key did not become valid again", second.Code)
	}
}

func TestHandlerTranslatesTheErrorClassIntoAStatus(t *testing.T) {
	cases := []struct {
		statusFromMeta int
		wantStatus     int
		wantClass      string
	}{
		{http.StatusServiceUnavailable, http.StatusServiceUnavailable, "retryable"},
		{http.StatusBadRequest, http.StatusBadRequest, "permanent"},
		{http.StatusUnauthorized, http.StatusBadGateway, "config"},
	}

	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.statusFromMeta)
			_, _ = w.Write([]byte(`{"error":{"message":"x","code":1}}`))
		}))
		h, _ := testHandler(t, srv)

		rec := ask(t, h, "token-do-a", "k-"+c.wantClass, textBody)
		srv.Close()

		if rec.Code != c.wantStatus {
			t.Errorf("Meta %d -> %d, want %d", c.statusFromMeta, rec.Code, c.wantStatus)
		}
		var resp struct {
			Error struct {
				Class string `json:"class"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Class != c.wantClass {
			t.Errorf("Meta %d -> class %q, want %q", c.statusFromMeta, resp.Error.Class, c.wantClass)
		}
	}
}

func TestHandlerRefusesInvalidBodyWithASchemaError(t *testing.T) {
	srv := acceptingMeta("wamid.X")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	cases := []string{
		`{"instancia":"lojinha","para":"5511999990000","tipo":"template","template":"t","idioma":"pt_BR","responder_a":"wamid.A"}`,
		`{"instancia":"lojinha","para":"5511999990000","tipo":"botoes","texto":"?","botoes":[{"id":"S","titulo":"S"}],"botao_url":"https://x"}`,
		`{"instancia":"lojinha","para":"5511999990000","tipo":"inventado","texto":"oi"}`,
		`nao e json`,
	}

	for _, body := range cases {
		rec := ask(t, h, "token-do-a", "k-esquema", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %.40s… -> status %d, want 400", body, rec.Code)
		}
	}
}

// (b) of T-045's Verify, on the WHOLE PATH: a consumer who still sends
// `botoes_url` gets a 400, with the instruction inside the response body, and META
// IS NOT CALLED.
//
// The count of calls to Meta is what separates this test from Validate's: if the
// field were silently ignored, the request would move forward and the message
// WOULD GO OUT — template without the button, 200 for the consumer, a billed conversation
// burned, and the discovery happening on his customer's phone. `chamadas`
// has to stay at ZERO.
func TestHandlerRefusesURLButtonsWithoutCallingMeta(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.X"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	body := `{"instancia":"lojinha","para":"5511999990000","tipo":"template",` +
		`"template":"equipamento_enviado","idioma":"pt_BR",` +
		`"botoes_url":[{"indice":0,"texto":"BR123456789BR"}]}`
	rec := ask(t, h, "token-do-a", "k-botoes-url", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if calls != 0 {
		t.Errorf("Meta was called %d time(s) — a request with botoes_url has to die "+
			"in validation, never go out without the button", calls)
	}
	// The consumer reads the instruction in the response BODY; a mute 400 would send them
	// to open the contract to find the new field's name.
	if !strings.Contains(rec.Body.String(), "botoes_template") {
		t.Errorf("the response does not point to the successor: %s", rec.Body.String())
	}
}

func TestHandlerNeverLeaksACredentialInTheResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid OAuth access token","code":190}}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k1", textBody)
	body := rec.Body.String()

	for _, forbidden := range []string{"t-lojinha", "token-do-a", srv.URL} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the response leaked %q: %s", forbidden, body)
		}
	}
}

// A-1: an UNKNOWN outcome (the message may have gone out) must not release the
// key — otherwise a legitimate retry sends a real SECOND message.
//
// The status and class are 502/desconhecido, NOT 503/retentavel: 503 would instruct
// the consumer to try again, and trying again with this key can only give a
// 409 — the key is held on purpose. See ClassUnknown in
// internal/meta/errors.go.
func TestHandlerDoesNotReleaseTheKeyWhenTransportFails(t *testing.T) {
	srv := acceptingMeta("wamid.NUNCA-CHAMADO")
	srv.Close() // closed BEFORE the request: every connection attempt fails

	h, store := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-transporte", textBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (unknown)", rec.Code)
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "unknown" {
		t.Fatalf("class = %q, want unknown", resp.Error.Class)
	}

	_, reserved, err := store.ReserveIdempotency("sistema-a", "k-transporte", textBodyHash)
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if reserved {
		t.Fatal("the key was released after a TRANSPORT failure — unknown outcome")
	}
}

// A-1: a 2xx with no id is also an unknown outcome (Meta received it and may have
// processed it; it just didn't answer as agreed).
func TestHandlerDoesNotReleaseTheKeyWhenTheAnswerHasNoID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`)) // 2xx, no messages[0].id
	}))
	defer srv.Close()
	h, store := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-sem-id", textBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (unknown)", rec.Code)
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "unknown" {
		t.Fatalf("class = %q, want unknown", resp.Error.Class)
	}

	_, reserved, err := store.ReserveIdempotency("sistema-a", "k-sem-id", textBodyHash)
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if reserved {
		t.Fatal("the key was released after ErrResponseWithoutID — unknown outcome")
	}
}

// A-1: a KNOWN-NEGATIVE outcome (Meta responded with an error; the message was NOT
// created) still releases — the retry has to be able to actually send.
func TestHandlerReleasesTheKeyOnAPermanentMetaRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"parametro invalido","code":100}}`))
	}))
	defer srv.Close()
	h, store := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-400", textBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	_, reserved, err := store.ReserveIdempotency("sistema-a", "k-400", "")
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if !reserved {
		t.Fatal("the key was NOT released after a known-negative outcome (400 permanent)")
	}
}

// T-141: when Meta sends error_data.details, the gateway passes that text through
// in detalhe_meta — a field SEPARATE from mensagem, never concatenated into it.
// THIS TEST DOES NOT PROVE THAT META STILL SENDS error_data.details TODAY: the
// body below is SYNTHETIC, shaped in the format that the consumer's 2026-07-18
// record (via another transport) showed. See docs/ARMADILHAS.md.
func TestHandlerPassesThroughMetaDetailWhenMetaSendsErrorDataDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Parameter value is not valid","code":131009,` +
			`"error_data":{"details":"Button title length invalid. Min length: 1, Max length: 20"}}}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-detalhe-meta", textBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	want := "Button title length invalid. Min length: 1, Max length: 20"
	if resp.Error.MetaDetail != want {
		t.Errorf("meta_detail = %q, want %q", resp.Error.MetaDetail, want)
	}
	if resp.Error.Message != "Parameter value is not valid" {
		t.Errorf("message = %q — cannot change just because meta_detail appeared", resp.Error.Message)
	}
	// The message NEVER carries the detail concatenated: whoever matches on
	// `mensagem` today must not break because of this new field.
	if strings.Contains(resp.Error.Message, "Button title") {
		t.Errorf("message = %q — meta_detail leaked into the message", resp.Error.Message)
	}
}

// (b) NON-REGRESSION: without error_data.details, the error body remains
// IDENTICAL, byte for byte, to what it was before this task — detalhe_meta stays
// ABSENT from the JSON (omitempty), never present as "".
func TestHandlerWithoutErrorDataKeepsTodaysErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"parametro invalido","code":100}}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-sem-detalhe", textBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "meta_detail") {
		t.Fatalf("body = %q — meta_detail CANNOT appear when Meta did not send error_data.details",
			rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if resp.Error.Class != "permanent" || resp.Error.MetaCode != 100 || resp.Error.Message != "parametro invalido" {
		t.Fatalf("body changed without error_data: %+v", resp.Error)
	}
}

// T-141 item 5: detalhe_meta NEVER goes into the transit log (T-091) — it is
// persistent and read by whoever operates it; the response goes only to whoever sent the
// payload and therefore already has its content. recordTransit writes only the
// error CLASS (sendErrorClass), never em.Message or em.Detail — if
// that ever changes, this test flags it.
func TestHandlerMetaDetailDoesNotLeakIntoTheTransitLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Parameter value is not valid","code":131009,` +
			`"error_data":{"details":"segredo do payload, nao pode ir pro log de transito"}}}`))
	}))
	defer srv.Close()
	h, store := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-detalhe-sem-log", textBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var outcome string
	if err := store.DB().QueryRow(
		`SELECT desfecho FROM transito WHERE slug = 'lojinha'`,
	).Scan(&outcome); err != nil {
		t.Fatalf("read the transit log: %v", err)
	}
	if strings.Contains(outcome, "segredo do payload") {
		t.Fatalf("transit log outcome = %q — meta_detail leaked into the persistent log", outcome)
	}
	if outcome != "permanent" {
		t.Errorf("outcome = %q, want the error's CLASS (permanent), not the detail", outcome)
	}
}

// T-153: when Meta sends error_subcode, error_user_title/error_user_msg, and
// fbtrace_id, the gateway passes the three through in SEPARATE fields — subcodigo_meta,
// explicacao_meta, and rastro_meta — never concatenated into mensagem.
func TestHandlerPassesThroughSubcodeExplanationAndTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"An unknown error has occurred","code":2,` +
			`"error_subcode":2494055,"error_user_title":"Erro temporario",` +
			`"error_user_msg":"Tente novamente em alguns instantes","fbtrace_id":"AbCdEfGhIjKlMnOp"}}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-subcodigo", textBody)
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body is not valid JSON: %v (%s)", err, rec.Body.String())
	}
	if resp.Error.MetaSubcode != 2494055 {
		t.Errorf("meta_subcode = %d, want 2494055", resp.Error.MetaSubcode)
	}
	want := "Erro temporario: Tente novamente em alguns instantes"
	if resp.Error.MetaExplanation != want {
		t.Errorf("meta_explanation = %q, want %q", resp.Error.MetaExplanation, want)
	}
	if resp.Error.MetaTrace != "AbCdEfGhIjKlMnOp" {
		t.Errorf("meta_trace = %q, want %q", resp.Error.MetaTrace, "AbCdEfGhIjKlMnOp")
	}
	if resp.Error.Message != "An unknown error has occurred" {
		t.Errorf("message = %q — cannot change just because the new fields appeared", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "Erro temporario") || strings.Contains(resp.Error.Message, "AbCdEfGhIjKlMnOp") {
		t.Errorf("message = %q — one of the new fields leaked into the message", resp.Error.Message)
	}
}

// (b) NON-REGRESSION: without Meta's four new fields, the error body
// remains IDENTICAL, byte for byte, to before this task — the three new
// fields stay ABSENT from the JSON (omitempty), never present as "" or 0.
func TestHandlerWithoutTheNewFieldsKeepsTodaysErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"parametro invalido","code":100}}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-sem-campos-novos", textBody)
	body := rec.Body.String()
	for _, field := range []string{"meta_subcode", "meta_explanation", "meta_trace"} {
		if strings.Contains(body, field) {
			t.Fatalf("body = %q — %s CANNOT appear when Meta did not send the source field", body, field)
		}
	}
}

// T-153 item 5: rastro_meta (fbtrace_id) is NOT a secret, but even so it does NOT
// go into the transit log — the log has a fixed-column shape (T-091) and giving
// it a new column is a decision outside this task (see the comment in
// respondSendError). Same guarantee as TestHandlerMetaDetailDoesNotLeakIntoTheTransitLog,
// for the new fields.
func TestHandlerMetaTraceDoesNotLeakIntoTheTransitLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"algo","code":2,` +
			`"error_subcode":2494055,"fbtrace_id":"AbCdEfGhIjKlMnOp"}}`))
	}))
	defer srv.Close()
	h, store := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-rastro-sem-log", textBody)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var outcome string
	if err := store.DB().QueryRow(
		`SELECT desfecho FROM transito WHERE slug = 'lojinha'`,
	).Scan(&outcome); err != nil {
		t.Fatalf("read the transit log: %v", err)
	}
	if strings.Contains(outcome, "AbCdEfGhIjKlMnOp") || strings.Contains(outcome, "2494055") {
		t.Fatalf("transit log outcome = %q — a new field leaked into the persistent log", outcome)
	}
}

// A-2: the instance's TimeoutMs has to actually hold. Without it, a destination that
// hangs holds the goroutine (and the idempotency slot) indefinitely.
// Fix E: an invalid phone_number_id is KNOWN-NEGATIVE (the request doesn't even leave
// here — see meta.ErrInvalidPhoneNumberID in internal/meta/client.go), not
// "unknown". Without its own branch it fell into respondSendError's default
// and lied twice: it said to check with Meta (the message never got there) and
// not to resend (the key WAS released three lines earlier, in handler.go).
func TestHandlerAnswersConfigWhenPhoneNumberIDIsInvalid(t *testing.T) {
	srv := acceptingMeta("wamid.NUNCA-CHAMADO")
	defer srv.Close()
	h, store := testHandler(t, srv)

	if _, err := store.DB().Exec(
		`UPDATE instancia SET phone_number_id = ? WHERE slug = ?`, "id/invalido", "lojinha"); err != nil {
		t.Fatalf("corrupt test phone_number_id: %v", err)
	}

	rec := ask(t, h, "token-do-a", "k-phone-invalido", textBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s — want 502", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "config" {
		t.Fatalf("class = %q, want config — the instance is misconfigured, not an unknown outcome",
			resp.Error.Class)
	}
	for _, forbidden := range []string{"confira se a mensagem chegou", "gerenciador", "nao reenvie"} {
		if strings.Contains(resp.Error.Message, forbidden) {
			t.Errorf("message %q contains %q — tells to check/not resend, but the request never went out and the key was released",
				resp.Error.Message, forbidden)
		}
	}

	// The key has to have been RELEASED: a legitimate retry, after someone
	// fixes the phone_number_id, has to be able to actually send.
	_, reserved, err := store.ReserveIdempotency("sistema-a", "k-phone-invalido", textBodyHash)
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if !reserved {
		t.Fatal("the key was NOT released after an invalid phone_number_id — the outcome is KNOWN-NEGATIVE")
	}
}

// T-211: this test USED TO measure wall-clock elapsed time against a tight
// (250ms) bound, and that is exactly what made it flaky — CI run 33418293310
// failed with an observed 294ms on a loaded runner, on the SAME commit that
// passed both right before (as a tag push) and right after. A loaded
// scheduler can delay a goroutine by hundreds of milliseconds without the
// deadline logic being wrong at all.
//
// The first replacement tried here read the deadline off a REAL mock
// server's r.Context() — and that does not work AT ALL: context.WithTimeout
// is a purely local, client-side Go value. It is never serialized onto the
// wire, so the server side can never see it; that version deadlocked
// (proven with `go test -c` + `-test.timeout=10s`, goroutine dump showed the
// server handler stuck forever on `<-r.Context().Done()` because that
// context had no deadline to ever expire).
//
// The value is only observable in ONE place, in-process: the
// http.RoundTripper the client is about to invoke. So this test swaps in a
// fake RoundTripper that touches NO network at all — it just records
// req.Context().Deadline() and returns an error — and asserts that recorded
// deadline falls inside [before+50ms, after+50ms]. That window is exact
// regardless of runner speed, because before/after bracket the very call
// that produced it, and it only holds if TimeoutMs=50 genuinely flowed from
// the instance's row, through InstanceDeadline, into context.WithTimeout,
// into the request the client was about to send. Zero sleeping, zero clock
// racing, zero network.
func TestHandlerRespectsTheInstanceTimeoutMs(t *testing.T) {
	const timeoutMs = 50

	rt := &deadlineCapturingTransport{}
	client := &http.Client{Transport: rt}

	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	if _, err := store.DB().Exec(
		`UPDATE instancia SET timeout_ms = ? WHERE slug = ?`, timeoutMs, "lojinha"); err != nil {
		t.Fatalf("adjust test timeout_ms: %v", err)
	}

	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(client, "http://meta.invalid"), 1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)

	before := time.Now()
	rec := ask(t, h, "token-do-a", "k-timeout", textBody)
	after := time.Now()

	if !rt.hasDeadline {
		t.Fatal("the call to the HTTP client went out WITHOUT a deadline in the context — TimeoutMs did not reach the client")
	}
	// The deadline the TRANSPORT SAW has to land inside [before+timeoutMs,
	// after+timeoutMs] — the proof it came FROM TimeoutMs=50ms and not from
	// defaultTimeout (30s) nor from no deadline at all.
	wantMin := before.Add(timeoutMs * time.Millisecond)
	wantMax := after.Add(timeoutMs * time.Millisecond)
	if rt.deadline.Before(wantMin) || rt.deadline.After(wantMax) {
		t.Fatalf("context deadline = %v, expected between %v and %v (TimeoutMs=%dms was not respected)",
			rt.deadline, wantMin, wantMax, timeoutMs)
	}

	// 502/desconhecido, not 503/retentavel: the transport failed without a
	// response from Meta, so the message's outcome is UNKNOWN — the same
	// treatment as a real timeout blowing up mid-call.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s — want 502: transport failure (simulated) with no response from Meta",
			rec.Code, rec.Body.String())
	}
}

// deadlineCapturingTransport never touches the network: it exists only so a
// test can read the deadline the CALLER attached to the request's context —
// the one signal that proves TimeoutMs reached the HTTP client, without
// needing the deadline to actually fire and without any real I/O.
type deadlineCapturingTransport struct {
	deadline    time.Time
	hasDeadline bool
}

func (rt *deadlineCapturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.deadline, rt.hasDeadline = req.Context().Deadline()
	return nil, errors.New("deadlineCapturingTransport: proposital — este teste nunca faz I/O de rede de verdade")
}

// A-4: the deadline for how long to wait for Meta belongs to the INSTANCE, not to the HTTP
// client of whoever called the gateway. The typical consumer uses a 3-5s timeout, of the same
// order as the default TimeoutMs (5000ms, see internal/config/store.go); if the
// gateway derived the deadline from r.Context() without letting go of the cancellation, the
// impatient consumer would abort the gateway's OWN call to Meta midway — the worst
// possible moment, because the key has already been reserved and the outcome would become
// unknown for up to 72h. This test would have failed before the fix because
// net/http cancels r.Context() when the consumer gives up, and that
// cancellation propagated into the call to Meta.
func TestHandlerIgnoresConsumerCancellationAndFinishesTheSend(t *testing.T) {
	reachedMeta := make(chan struct{})
	releaseMeta := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(reachedMeta) // proves the call to Meta LEFT here before the cancellation
		<-releaseMeta
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.SOBREVIVEU-AO-CANCELAMENTO"}]}`))
	}))
	defer srv.Close()

	h, store := testHandler(t, srv)
	// The test instance's TimeoutMs is 2000ms (see storeWithConsumer) — well
	// longer than this test's wait. What would have killed the send isn't the
	// instance's deadline, it's the CONSUMER's cancellation.

	ctxConsumer, cancelConsumer := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(textBody)).
		WithContext(ctxConsumer)
	req.Header.Set("Authorization", "Bearer token-do-a")
	req.Header.Set("Idempotency-Key", "k-cancelado")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	made := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(made)
	}()

	<-reachedMeta // the call to Meta is genuinely already in flight

	cancelConsumer() // simulates the consumer's client timeout
	// gives the cancellation time to fully propagate BEFORE letting Meta
	// respond — without this slack, a cancellation that (absent the fix)
	// aborts the call could lose the race against the response and the test
	// would pass by accident.
	time.Sleep(30 * time.Millisecond)
	close(releaseMeta) // ONLY THEN does Meta respond

	<-made

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s — the CONSUMER's cancellation aborted the send",
			rec.Code, rec.Body.String())
	}
	var resp struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.WaMessageID != "wamid.SOBREVIVEU-AO-CANCELAMENTO" {
		t.Fatalf("wa_message_id = %q — the call to Meta did not reach completion", resp.WaMessageID)
	}

	// The key has to be CONFIRMED with the sent id — not held as an
	// unknown outcome. ReserveIdempotency returns the id in alreadySent
	// when the key has already been confirmed.
	alreadySent, _, err := store.ReserveIdempotency("sistema-a", "k-cancelado", textBodyHash)
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if alreadySent != "wamid.SOBREVIVEU-AO-CANCELAMENTO" {
		t.Fatalf("alreadySent = %q — the key was not confirmed; the send did not really complete", alreadySent)
	}
}

// failingReader returns a chunk of valid JSON and then an error — simulates a
// consumer connection that dropped MIDWAY through the upload (it didn't blow any cap).
type failingReader struct {
	delivered bool
}

func (l *failingReader) Read(p []byte) (int, error) {
	if !l.delivered {
		l.delivered = true
		n := copy(p, []byte(`{"instancia":`))
		return n, nil
	}
	return 0, errors.New("conexao caiu no meio do upload")
}

// A-3: a body I/O error is NOT "body too large". Saying that would send the
// consumer to shrink a body that was perfectly fine, and "permanent" would tell it
// NOT to try again — when trying again is exactly right.
func TestHandlerRefusesAReadErrorAsRetryableAndNot413(t *testing.T) {
	srv := acceptingMeta("wamid.X")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", io.NopCloser(&failingReader{}))
	req.Header.Set("Authorization", "Bearer token-do-a")
	req.Header.Set("Idempotency-Key", "k-io")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s — want 400 (the gateway is standing; what fell was the consumer's connection)",
			rec.Code, rec.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "retryable" {
		t.Fatalf("class = %q, want retryable — retrying solves it, the body did not blow any ceiling", resp.Error.Class)
	}
}

// A-3, the path that already worked: a body ABOVE the cap remains a 413
// permanent (retrying with the SAME body repeats the SAME overflow).
func TestHandlerRefusesLargeBodyWith413Permanent(t *testing.T) {
	srv := acceptingMeta("wamid.X")
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 10, config.NewCounter(store), config.NewTransit(store), AllTypes)

	rec := ask(t, h, "token-do-a", "k-grande", textBody)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s — want 413", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "permanent" {
		t.Fatalf("class = %q, want permanent", resp.Error.Class)
	}
}

// A-5: the idempotency key has to be tied TO THE REQUEST. Without that, the
// SECOND message (reminder, billing, apology — same entity, key
// reused) gets a 200 with the FIRST one's id, the consumer records
// "sent", and it never goes out — a silent and costly failure.
func TestHandlerRefusesASecondRequestWithTheSameKey(t *testing.T) {
	var sends int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sends++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.PRIMEIRA"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	bodyA := textBody
	bodyB := `{"instancia":"lojinha","para":"5511999990000","tipo":"texto","texto":"cobranca"}`

	first := ask(t, h, "token-do-a", "mesma-chave", bodyA)
	if first.Code != http.StatusOK {
		t.Fatalf("1st call: status = %d, body = %s", first.Code, first.Body.String())
	}

	second := ask(t, h, "token-do-a", "mesma-chave", bodyB)
	if second.Code != http.StatusUnprocessableEntity {
		t.Fatalf("2nd call: status = %d, body = %s — want 422 (key used with ANOTHER request)",
			second.Code, second.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(second.Body.Bytes(), &resp)
	if resp.Error.Class != "permanent" {
		t.Fatalf("class = %q, want permanent — retrying with this key will NEVER work", resp.Error.Class)
	}

	mu.Lock()
	n := sends
	mu.Unlock()
	if n != 1 {
		t.Fatalf("Meta received %d calls, want 1 — the second message could NOT go out", n)
	}
}

// F4 — the symmetric pair of TestRequestHashIsEqualForTwoSpellingsOfTheSamePhone,
// but going through the real handler: same request, same Idempotency-Key,
// phone written in two different forms. Without the fix, the hash calculated
// for the second spelling diverges from the one recorded for the first, and the handler returns 422
// (ErrKeyWithDifferentRequest) for what is, on Meta, the SAME repeated message.
func TestHandlerAcceptsTwoSpellingsOfTheSamePhoneWithTheSameKey(t *testing.T) {
	var sends int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sends++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.UNICO"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	formattedBody := `{"instancia":"lojinha","para":"+55 11 99999-0000","tipo":"texto","texto":"oi"}`

	first := ask(t, h, "token-do-a", "mesma-chave-telefone", textBody)
	if first.Code != http.StatusOK {
		t.Fatalf("1st call (canonical): status = %d, body = %s", first.Code, first.Body.String())
	}

	second := ask(t, h, "token-do-a", "mesma-chave-telefone", formattedBody)
	if second.Code != http.StatusOK {
		t.Fatalf("2nd call (formatted): status = %d, body = %s — want 200 with the SAME id, not 422", second.Code, second.Body.String())
	}

	var resp1, resp2 struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &resp1)
	_ = json.Unmarshal(second.Body.Bytes(), &resp2)
	if resp1.WaMessageID != resp2.WaMessageID || resp2.WaMessageID == "" {
		t.Fatalf("wa_message_id diverged between spellings: %q and %q", resp1.WaMessageID, resp2.WaMessageID)
	}

	mu.Lock()
	n := sends
	mu.Unlock()
	if n != 1 {
		t.Fatalf("Meta received %d calls, want 1 — the second spelling could not resend", n)
	}
}

func TestHandlerRefusesPausedInstance(t *testing.T) {
	srv := acceptingMeta("wamid.X")
	defer srv.Close()
	store, _ := storeWithConsumer(t) // does NOT activate
	h := NewHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)

	rec := ask(t, h, "token-do-a", "k1", textBody)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — a paused instance does not send", rec.Code)
	}
}

// T-034 — Meta can respond 200 with a message_status different from
// "accepted" (held_for_quality_assessment, paused), and until now the gateway
// returned the SAME 200+wamid as a normal send: the consumer would record
// "sent" for a message that may never arrive. See
// docs/CONTRATO-CONSUMIDOR.md and docs/ARMADILHAS.md.

// (a) NON-REGRESSION: message_status "accepted" produces the success body
// BYTE FOR BYTE identical to before this task — no consumer that only reads
// wa_message_id can see a difference.
func TestHandlerMessageStatusAcceptedKeepsTodaysBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.OK","message_status":"accepted"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-status-accepted", textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	want := "{\"wa_message_id\":\"wamid.OK\"}\n"
	if rec.Body.String() != want {
		t.Fatalf("body = %q, want %q — message_status accepted CANNOT appear in the body", rec.Body.String(), want)
	}
}

// (b) NON-REGRESSION, the absent case: it's what happens today for all traffic
// that isn't a template under pacing (text, media, interactive, reaction,
// location) — without the field, the body remains IDENTICAL.
func TestHandlerMessageStatusAbsentKeepsTodaysBody(t *testing.T) {
	srv := acceptingMeta("wamid.OK") // no message_status
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-status-ausente", textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	want := "{\"wa_message_id\":\"wamid.OK\"}\n"
	if rec.Body.String() != want {
		t.Fatalf("body = %q, want %q — absence of message_status CANNOT turn into any field", rec.Body.String(), want)
	}
}

// (c) a value DIFFERENT from "accepted" stays VISIBLE in the body and ALARMS the
// log — it's the case where the 200 doesn't guarantee delivery (see the source cited in
// internal/meta/client.go, sendResponse).
func TestHandlerMessageStatusOtherThanAcceptedShowsInTheBodyAndAlarms(t *testing.T) {
	for _, status := range []string{"held_for_quality_assessment", "paused"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.RETIDO","message_status":"` + status + `"}]}`))
		}))
		h, _ := testHandler(t, srv)

		var logBuf bytes.Buffer
		log.SetOutput(&logBuf)
		rec := ask(t, h, "token-do-a", "k-status-"+status, textBody)
		log.SetOutput(logStdout)
		srv.Close()

		if rec.Code != http.StatusOK {
			t.Fatalf("status %q: HTTP = %d, body = %s — Meta accepted the request, the gateway cannot refuse",
				status, rec.Code, rec.Body.String())
		}
		var resp struct {
			WaMessageID   string `json:"wa_message_id"`
			MessageStatus string `json:"message_status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("status %q: body is not JSON: %s", status, rec.Body.String())
		}
		if resp.WaMessageID != "wamid.RETIDO" {
			t.Errorf("status %q: wa_message_id = %q", status, resp.WaMessageID)
		}
		if resp.MessageStatus != status {
			t.Errorf("status %q: message_status in the body = %q, want the raw value", status, resp.MessageStatus)
		}
		if !strings.Contains(logBuf.String(), "ALARME") {
			t.Errorf("status %q: nothing in the log warned — the 200 does not guarantee delivery and nobody would know", status)
		}
	}
}

// logStdout restores the log package's destination after a test that
// redirects it to a buffer — without this, a test that runs BEFORE in the same
// suite would leave the log mute for the ones that come after.
var logStdout = log.Writer()

// --- Instance counters (T-035) -----------------------------------------

func TestHandlerCountsSentOnSuccess(t *testing.T) {
	srv := acceptingMeta("wamid.OK")
	defer srv.Close()
	h, store := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-enviadas", textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	m, err := store.CountersBetween("lojinha", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[config.CounterSent] != 1 {
		t.Errorf("sent = %d, want 1", m[config.CounterSent])
	}
	if m[config.CounterSendFailures] != 0 {
		t.Errorf("send_failures = %d, want 0", m[config.CounterSendFailures])
	}
}

func TestHandlerCountsSendFailuresWhenMetaRefuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"parametro invalido","code":100}}`))
	}))
	defer srv.Close()
	h, store := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k-falha", textBody)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want an error (Meta refused)", rec.Code)
	}

	m, err := store.CountersBetween("lojinha", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[config.CounterSendFailures] != 1 {
		t.Errorf("send_failures = %d, want 1", m[config.CounterSendFailures])
	}
	if m[config.CounterSent] != 0 {
		t.Errorf("sent = %d, want 0 (Meta refused)", m[config.CounterSent])
	}
}

// alwaysFailingOutboundCounter is the double of config.CounterStore that fails on
// EVERY call — proves, against the REAL send handler, that a counting
// failure never changes the status returned to the consumer (T-035, Verify (c)).
// It's the same role as internal/inbound's contadorSempreErra; one type per
// package because config.CounterStore is a small interface and each test
// package already has its own set of helpers.
type alwaysFailingOutboundCounter struct{}

func (alwaysFailingOutboundCounter) IncrementCounter(slug, key string, when time.Time) error {
	return errors.New("alwaysFailingOutboundCounter: falha proposital de teste")
}

func TestHandlerCounterFailureDoesNotChangeTheStatus(t *testing.T) {
	srv := acceptingMeta("wamid.OK")
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20,
		config.NewCounterWithStore(alwaysFailingOutboundCounter{}), config.NewTransit(store), AllTypes)

	rec := ask(t, h, "token-do-a", "k-conta-falha", textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s — a COUNTER failure cannot change the SEND response",
			rec.Code, rec.Body.String())
	}
}

// docs/ARMADILHAS.md, "Go / concurrency": the counter is mutable state touched
// by requests that http.Server serves in goroutines over the SAME
// handler. Run with -race. Each goroutine uses a DIFFERENT Idempotency-Key —
// otherwise the idempotency guard (not the counting) would be what decides the
// result.
func TestHandlerCounterWithstandsConcurrentRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.CONC"}]}`))
	}))
	defer srv.Close()
	h, store := testHandler(t, srv)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := "k-conc-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
			rec := ask(t, h, "token-do-a", key, textBody)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d", rec.Code)
			}
		}()
	}
	wg.Wait()

	m, err := store.CountersBetween("lojinha", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[config.CounterSent] != goroutines {
		t.Fatalf("sent = %d, want %d — count lost under concurrency", m[config.CounterSent], goroutines)
	}
}

// --- Validation-refusal log (T-037) -------------------------------------
//
// On 2026-07-26 10:41:06 a consumer got a 400 on POST /v1/messages and the
// gateway's journal was clean that minute — the ONLY copy of the reason
// had gone to the consumer (docs/TASKS.md, T-037). These tests prove the
// task's Verify: (a) one line per refusal, with slug and the named field;
// (b) never the VALUE of a field nor the Idempotency-Key; (c) 401 never logs;
// (d) the status returned to the consumer doesn't change (already covered by the
// status assertions in each test below and by the whole suite staying green).

// (a) and (b): an invalid request produces ONE line citing the slug and the
// refused field ("para"), and that line does NOT contain the value of ANY field
// of the request nor the Idempotency-Key — even when both carry a
// SENTINEL string. The assertion is by ABSENCE of the specific value, not by format
// (docs/ARMADILHAS.md, "Testes": look for the secret, not the whole input).
func TestHandlerLogsValidationRejectionWithoutLeakingValueNorIdempotencyKey(t *testing.T) {
	srv := acceptingMeta("wamid.NAO-DEVE-SER-CHAMADO")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	const textSentinel = "SENTINELA-VALOR-DO-CAMPO-789"
	const keySentinel = "IDEMP-SENTINELA-CHAVE-001"
	// an empty `para` is the field that fails; `texto` carries the sentinel and does NOT
	// participate in the error (the `para` error comes before the type switch, in
	// message.go) — if the log leaked ANY value from the body, it would be this one.
	body := `{"instancia":"lojinha","para":"","tipo":"texto","texto":"` + textSentinel + `"}`

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	rec := ask(t, h, "token-do-a", keySentinel, body)
	log.SetOutput(logStdout)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s — want 400 (para missing)", rec.Code, rec.Body.String())
	}

	output := logBuf.String()
	if n := strings.Count(strings.TrimRight(output, "\n"), "\n") + 1; strings.TrimSpace(output) == "" || n != 1 {
		t.Fatalf("log has %d line(s), want exactly 1: %q", n, output)
	}
	if !strings.Contains(output, "lojinha") {
		t.Errorf("log does not cite the instance slug: %q", output)
	}
	if !strings.Contains(output, "para") {
		t.Errorf("log does not name the refused field (para): %q", output)
	}
	if strings.Contains(output, textSentinel) {
		t.Errorf("the log LEAKED the value of a request field: %q", output)
	}
	if strings.Contains(output, keySentinel) {
		t.Errorf("the log LEAKED the Idempotency-Key: %q", output)
	}
}

// The same proof (b), but for the THREE points in message.go that deliberately
// quote the refused value in the RESPONSE body (ErrUnknownType,
// ErrUnknownCategory) — found while writing this task (see
// safeRejectionMessage in message.go). Without that function, this test would be the
// one to catch the raw message leaking the made-up `tipo` into the log.
func TestHandlerLogsUnknownTypeWithoutLeakingTheRefusedValue(t *testing.T) {
	srv := acceptingMeta("wamid.NAO-DEVE-SER-CHAMADO")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	const sentinelType = "SENTINELA-TIPO-INVENTADO"
	body := `{"instancia":"lojinha","para":"5511999990000","tipo":"` + sentinelType + `"}`

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	rec := ask(t, h, "token-do-a", "k-tipo-desconhecido", body)
	log.SetOutput(logStdout)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s — want 400 (unknown tipo)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), sentinelType) {
		t.Fatalf("the RESPONSE to the consumer did not cite the type it sent itself — badly built test")
	}
	if strings.Contains(logBuf.String(), sentinelType) {
		t.Errorf("the log LEAKED the value of `tipo`, which should only come back in the response to the consumer itself: %q",
			logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "lojinha") {
		t.Errorf("log does not cite the instance slug: %q", logBuf.String())
	}
}

// (c): 401 is internet scan noise — never logs.
func TestHandler401NeverGeneratesARejectionLog(t *testing.T) {
	srv := acceptingMeta("wamid.NAO-DEVE-SER-CHAMADO")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	rec := ask(t, h, "token-completamente-errado", "k-401", textBody)
	log.SetOutput(logStdout)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if logBuf.Len() != 0 {
		t.Errorf("401 generated a log — an invalid token is scan noise, it must never log: %q", logBuf.String())
	}
}

// A 404 for the instance DOES log (T-037): whoever got here has already authenticated and is already
// authorized for the slug — the instance just no longer exists in the store, and that is
// someone's misconfiguration, which is worth investigating (unlike 401).
//
// The scenario is simulated by deleting the `instancia` row via DIRECT SQL, the SAME
// TECHNIQUE as activateInstance (auth_test.go): CreateConsumer has a FOREIGN KEY
// against `instancia`, so there's no way to register via the API a consumer
// authorized for a slug that never existed — but an admin deleting the
// instance outside the Store (and forgetting the link) is exactly the
// misalignment this 404 exists to flag.
func TestHandlerLogsUnknownInstanceAs404(t *testing.T) {
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

	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(http.DefaultClient, "http://127.0.0.1:1"),
		1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)

	body := `{"instancia":"clinica","para":"5511999990000","tipo":"texto","texto":"oi"}`

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	rec := ask(t, h, "token-do-c", "k-clinica-sumida", body)
	log.SetOutput(logStdout)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s — want 404 (unknown instance)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "clinica") {
		t.Errorf("404 for an unknown instance did not log — it should (T-037): %q", logBuf.String())
	}
}

// throttleLog is mutable state touched by every request over the SAME
// Handler (docs/ARMADILHAS.md, "Go / concurrency" — the same file already had
// a race Critical exactly like this, `h.seq++`). Run with -race: each
// goroutine deliberately sends the SAME invalid request (same consumer, same reason),
// to hit the SAME throttle key at the same time.
func TestHandlerLogThrottleWithstandsConcurrentRequests(t *testing.T) {
	srv := acceptingMeta("wamid.NAO-DEVE-SER-CHAMADO")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// Without Idempotency-Key: validation refusal, same throttle
			// key (route+consumer) for the 100 calls.
			rec := ask(t, h, "token-do-a", "", textBody)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		}()
	}
	wg.Wait()
}
