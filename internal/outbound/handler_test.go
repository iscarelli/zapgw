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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
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
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
	if rec := ask(t, h, "token-errado", "k1", textBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status = %d, quero 401", rec.Code)
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
		t.Errorf("status = %d, quero 403", rec.Code)
	}
	if calledMeta {
		t.Fatal("o gateway CHAMOU A META pela instancia de outro sistema")
	}
}

func TestHandlerRequiresIdempotencyKey(t *testing.T) {
	srv := acceptingMeta("wamid.X")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "", textBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 — sem a chave nao ha como impedir duplicata", rec.Code)
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
		t.Fatalf("a Meta recebeu %d envios, quero 1", sends)
	}
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d e %d, quero 200 nos dois", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("respostas diferentes:\n1: %s\n2: %s", first.Body, second.Body)
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
		t.Fatalf("1a: status = %d, quero 503 (retentavel)", first.Code)
	}

	second := ask(t, h, "token-do-a", "k1", textBody)
	if second.Code != http.StatusOK {
		t.Fatalf("2a: status = %d — a chave nao voltou a valer", second.Code)
	}
}

func TestHandlerTranslatesTheErrorClassIntoAStatus(t *testing.T) {
	cases := []struct {
		statusFromMeta int
		wantStatus     int
		wantClass      string
	}{
		{http.StatusServiceUnavailable, http.StatusServiceUnavailable, "retentavel"},
		{http.StatusBadRequest, http.StatusBadRequest, "permanente"},
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
			t.Errorf("Meta %d -> %d, quero %d", c.statusFromMeta, rec.Code, c.wantStatus)
		}
		var resp struct {
			Error struct {
				Class string `json:"classe"`
			} `json:"erro"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Class != c.wantClass {
			t.Errorf("Meta %d -> classe %q, quero %q", c.statusFromMeta, resp.Error.Class, c.wantClass)
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
			t.Errorf("corpo %.40s… -> status %d, quero 400", body, rec.Code)
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
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	if calls != 0 {
		t.Errorf("a Meta foi chamada %d vez(es) — o pedido com botoes_url tem de morrer "+
			"na validacao, nunca sair sem o botao", calls)
	}
	// The consumer reads the instruction in the response BODY; a mute 400 would send them
	// to open the contract to find the new field's name.
	if !strings.Contains(rec.Body.String(), "botoes_template") {
		t.Errorf("a resposta nao aponta o sucessor: %s", rec.Body.String())
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
			t.Errorf("a resposta vazou %q: %s", forbidden, body)
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
		t.Fatalf("status = %d, quero 502 (desconhecido)", rec.Code)
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "desconhecido" {
		t.Fatalf("classe = %q, quero desconhecido", resp.Error.Class)
	}

	_, reserved, err := store.ReserveIdempotency("sistema-a", "k-transporte", textBodyHash)
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if reserved {
		t.Fatal("a chave foi liberada apos falha de TRANSPORTE — desfecho desconhecido")
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
		t.Fatalf("status = %d, quero 502 (desconhecido)", rec.Code)
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "desconhecido" {
		t.Fatalf("classe = %q, quero desconhecido", resp.Error.Class)
	}

	_, reserved, err := store.ReserveIdempotency("sistema-a", "k-sem-id", textBodyHash)
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if reserved {
		t.Fatal("a chave foi liberada apos ErrResponseWithoutID — desfecho desconhecido")
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
		t.Fatalf("status = %d, quero 400", rec.Code)
	}

	_, reserved, err := store.ReserveIdempotency("sistema-a", "k-400", "")
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if !reserved {
		t.Fatal("a chave NAO foi liberada apos desfecho conhecido-negativo (400 permanente)")
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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao e JSON valido: %v", err)
	}
	want := "Button title length invalid. Min length: 1, Max length: 20"
	if resp.Error.MetaDetail != want {
		t.Errorf("detalhe_meta = %q, quero %q", resp.Error.MetaDetail, want)
	}
	if resp.Error.Message != "Parameter value is not valid" {
		t.Errorf("mensagem = %q — nao pode mudar so' porque detalhe_meta apareceu", resp.Error.Message)
	}
	// The message NEVER carries the detail concatenated: whoever matches on
	// `mensagem` today must not break because of this new field.
	if strings.Contains(resp.Error.Message, "Button title") {
		t.Errorf("mensagem = %q — detalhe_meta vazou para dentro de mensagem", resp.Error.Message)
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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "detalhe_meta") {
		t.Fatalf("corpo = %q — detalhe_meta NAO pode aparecer quando a Meta nao mandou error_data.details",
			rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao e JSON valido: %v", err)
	}
	if resp.Error.Class != "permanente" || resp.Error.MetaCode != 100 || resp.Error.Message != "parametro invalido" {
		t.Fatalf("corpo mudou sem error_data: %+v", resp.Error)
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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	var outcome string
	if err := store.DB().QueryRow(
		`SELECT desfecho FROM transito WHERE slug = 'lojinha'`,
	).Scan(&outcome); err != nil {
		t.Fatalf("ler o log de transito: %v", err)
	}
	if strings.Contains(outcome, "segredo do payload") {
		t.Fatalf("desfecho do log de transito = %q — detalhe_meta vazou para o log persistente", outcome)
	}
	if outcome != "permanente" {
		t.Errorf("desfecho = %q, quero a CLASSE do erro (permanente), nao o detalhe", outcome)
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
		t.Fatalf("corpo nao e JSON valido: %v (%s)", err, rec.Body.String())
	}
	if resp.Error.MetaSubcode != 2494055 {
		t.Errorf("subcodigo_meta = %d, quero 2494055", resp.Error.MetaSubcode)
	}
	want := "Erro temporario: Tente novamente em alguns instantes"
	if resp.Error.MetaExplanation != want {
		t.Errorf("explicacao_meta = %q, quero %q", resp.Error.MetaExplanation, want)
	}
	if resp.Error.MetaTrace != "AbCdEfGhIjKlMnOp" {
		t.Errorf("rastro_meta = %q, quero %q", resp.Error.MetaTrace, "AbCdEfGhIjKlMnOp")
	}
	if resp.Error.Message != "An unknown error has occurred" {
		t.Errorf("mensagem = %q — nao pode mudar so' porque os campos novos apareceram", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "Erro temporario") || strings.Contains(resp.Error.Message, "AbCdEfGhIjKlMnOp") {
		t.Errorf("mensagem = %q — um dos campos novos vazou para dentro de mensagem", resp.Error.Message)
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
	for _, field := range []string{"subcodigo_meta", "explicacao_meta", "rastro_meta"} {
		if strings.Contains(body, field) {
			t.Fatalf("corpo = %q — %s NAO pode aparecer quando a Meta nao mandou o campo de origem", body, field)
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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	var outcome string
	if err := store.DB().QueryRow(
		`SELECT desfecho FROM transito WHERE slug = 'lojinha'`,
	).Scan(&outcome); err != nil {
		t.Fatalf("ler o log de transito: %v", err)
	}
	if strings.Contains(outcome, "AbCdEfGhIjKlMnOp") || strings.Contains(outcome, "2494055") {
		t.Fatalf("desfecho do log de transito = %q — um campo novo vazou para o log persistente", outcome)
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
		t.Fatalf("corromper phone_number_id de teste: %v", err)
	}

	rec := ask(t, h, "token-do-a", "k-phone-invalido", textBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, corpo = %s — quero 502", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "config" {
		t.Fatalf("classe = %q, quero config — a instancia esta mal configurada, nao e desfecho desconhecido",
			resp.Error.Class)
	}
	for _, forbidden := range []string{"confira se a mensagem chegou", "gerenciador", "nao reenvie"} {
		if strings.Contains(resp.Error.Message, forbidden) {
			t.Errorf("mensagem %q contem %q — manda conferir/nao reenviar, mas o pedido nunca saiu e a chave foi liberada",
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
		t.Fatal("a chave NAO foi liberada apos phone_number_id invalido — desfecho e CONHECIDO-NEGATIVO")
	}
}

func TestHandlerRespectsTheInstanceTimeoutMs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond) // more than the TimeoutMs=50 configured below
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.TARDE-DEMAIS"}]}`))
	}))
	defer srv.Close()

	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	if _, err := store.DB().Exec(
		`UPDATE instancia SET timeout_ms = ? WHERE slug = ?`, 50, "lojinha"); err != nil {
		t.Fatalf("ajustar timeout_ms de teste: %v", err)
	}

	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)

	start := time.Now()
	rec := ask(t, h, "token-do-a", "k-timeout", textBody)
	elapsed := time.Since(start)

	// 502/desconhecido, not 503/retentavel: the deadline blew up in the MIDDLE of the
	// call, so the message may have gone out — it's the same transport
	// outcome, just by timeout instead of a refused connection.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, corpo = %s — quero 502: o TimeoutMs=50ms da instancia nao foi respeitado (esperou %v)",
			rec.Code, rec.Body.String(), elapsed)
	}
	if elapsed >= 250*time.Millisecond {
		t.Fatalf("o handler esperou %v — mais que o TimeoutMs=50ms configurado", elapsed)
	}
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
		t.Fatalf("status = %d, corpo = %s — o cancelamento do CONSUMIDOR abortou o envio",
			rec.Code, rec.Body.String())
	}
	var resp struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.WaMessageID != "wamid.SOBREVIVEU-AO-CANCELAMENTO" {
		t.Fatalf("wa_message_id = %q — a chamada a Meta nao chegou ao fim", resp.WaMessageID)
	}

	// The key has to be CONFIRMED with the sent id — not held as an
	// unknown outcome. ReserveIdempotency returns the id in alreadySent
	// when the key has already been confirmed.
	alreadySent, _, err := store.ReserveIdempotency("sistema-a", "k-cancelado", textBodyHash)
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if alreadySent != "wamid.SOBREVIVEU-AO-CANCELAMENTO" {
		t.Fatalf("alreadySent = %q — a chave nao foi confirmada; o envio nao completou de verdade", alreadySent)
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
// consumer to shrink a body that was perfectly fine, and "permanente" would tell it
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
		t.Fatalf("status = %d, corpo = %s — quero 400 (o gateway esta de pe; quem caiu foi a conexao do consumidor)",
			rec.Code, rec.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "retentavel" {
		t.Fatalf("classe = %q, quero retentavel — repetir resolve, o corpo nao estourou teto nenhum", resp.Error.Class)
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
		t.Fatalf("status = %d, corpo = %s — quero 413", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Class != "permanente" {
		t.Fatalf("classe = %q, quero permanente", resp.Error.Class)
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
		t.Fatalf("1a chamada: status = %d, corpo = %s", first.Code, first.Body.String())
	}

	second := ask(t, h, "token-do-a", "mesma-chave", bodyB)
	if second.Code != http.StatusUnprocessableEntity {
		t.Fatalf("2a chamada: status = %d, corpo = %s — quero 422 (chave usada com OUTRO pedido)",
			second.Code, second.Body.String())
	}
	var resp errorResponse
	_ = json.Unmarshal(second.Body.Bytes(), &resp)
	if resp.Error.Class != "permanente" {
		t.Fatalf("classe = %q, quero permanente — repetir com esta chave NUNCA vai funcionar", resp.Error.Class)
	}

	mu.Lock()
	n := sends
	mu.Unlock()
	if n != 1 {
		t.Fatalf("a Meta recebeu %d chamadas, quero 1 — a segunda mensagem NAO podia sair", n)
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
		t.Fatalf("1a chamada (canonico): status = %d, corpo = %s", first.Code, first.Body.String())
	}

	second := ask(t, h, "token-do-a", "mesma-chave-telefone", formattedBody)
	if second.Code != http.StatusOK {
		t.Fatalf("2a chamada (formatado): status = %d, corpo = %s — quero 200 com o MESMO id, nao 422", second.Code, second.Body.String())
	}

	var resp1, resp2 struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &resp1)
	_ = json.Unmarshal(second.Body.Bytes(), &resp2)
	if resp1.WaMessageID != resp2.WaMessageID || resp2.WaMessageID == "" {
		t.Fatalf("wa_message_id divergiu entre as grafias: %q e %q", resp1.WaMessageID, resp2.WaMessageID)
	}

	mu.Lock()
	n := sends
	mu.Unlock()
	if n != 1 {
		t.Fatalf("a Meta recebeu %d chamadas, quero 1 — a segunda grafia nao podia reenviar", n)
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
		t.Fatalf("status = %d, quero 503 — instancia pausada nao envia", rec.Code)
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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	want := "{\"wa_message_id\":\"wamid.OK\"}\n"
	if rec.Body.String() != want {
		t.Fatalf("corpo = %q, quero %q — message_status accepted NAO pode aparecer no corpo", rec.Body.String(), want)
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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	want := "{\"wa_message_id\":\"wamid.OK\"}\n"
	if rec.Body.String() != want {
		t.Fatalf("corpo = %q, quero %q — ausencia de message_status NAO pode virar campo nenhum", rec.Body.String(), want)
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
			t.Fatalf("status %q: HTTP = %d, corpo = %s — a Meta aceitou o pedido, o gateway nao pode recusar",
				status, rec.Code, rec.Body.String())
		}
		var resp struct {
			WaMessageID   string `json:"wa_message_id"`
			MessageStatus string `json:"message_status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("status %q: corpo nao e JSON: %s", status, rec.Body.String())
		}
		if resp.WaMessageID != "wamid.RETIDO" {
			t.Errorf("status %q: wa_message_id = %q", status, resp.WaMessageID)
		}
		if resp.MessageStatus != status {
			t.Errorf("status %q: message_status no corpo = %q, quero o valor cru", status, resp.MessageStatus)
		}
		if !strings.Contains(logBuf.String(), "ALARME") {
			t.Errorf("status %q: nada no log avisou — o 200 nao garante entrega e ninguem saberia", status)
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
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	m, err := store.CountersBetween("lojinha", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[config.CounterSent] != 1 {
		t.Errorf("enviadas = %d, quero 1", m[config.CounterSent])
	}
	if m[config.CounterSendFailures] != 0 {
		t.Errorf("falhas_de_envio = %d, quero 0", m[config.CounterSendFailures])
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
		t.Fatalf("status = %d, quero um erro (a Meta recusou)", rec.Code)
	}

	m, err := store.CountersBetween("lojinha", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[config.CounterSendFailures] != 1 {
		t.Errorf("falhas_de_envio = %d, quero 1", m[config.CounterSendFailures])
	}
	if m[config.CounterSent] != 0 {
		t.Errorf("enviadas = %d, quero 0 (a Meta recusou)", m[config.CounterSent])
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
		t.Fatalf("status = %d, corpo = %s — falha do CONTADOR nao pode mudar a resposta do ENVIO",
			rec.Code, rec.Body.String())
	}
}

// docs/ARMADILHAS.md, "Go / concorrência": the counter is mutable state touched
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
		t.Fatalf("enviadas = %d, quero %d — contagem perdida sob concorrencia", m[config.CounterSent], goroutines)
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
	// mensagem.go) — if the log leaked ANY value from the body, it would be this one.
	body := `{"instancia":"lojinha","para":"","tipo":"texto","texto":"` + textSentinel + `"}`

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	rec := ask(t, h, "token-do-a", keySentinel, body)
	log.SetOutput(logStdout)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, corpo = %s — quero 400 (para ausente)", rec.Code, rec.Body.String())
	}

	output := logBuf.String()
	if n := strings.Count(strings.TrimRight(output, "\n"), "\n") + 1; strings.TrimSpace(output) == "" || n != 1 {
		t.Fatalf("log tem %d linha(s), quero exatamente 1: %q", n, output)
	}
	if !strings.Contains(output, "lojinha") {
		t.Errorf("log nao cita o slug da instancia: %q", output)
	}
	if !strings.Contains(output, "para") {
		t.Errorf("log nao nomeia o campo recusado (para): %q", output)
	}
	if strings.Contains(output, textSentinel) {
		t.Errorf("o log VAZOU o valor de um campo do pedido: %q", output)
	}
	if strings.Contains(output, keySentinel) {
		t.Errorf("o log VAZOU a Idempotency-Key: %q", output)
	}
}

// The same proof (b), but for the THREE points in mensagem.go that deliberately
// quote the refused value in the RESPONSE body (ErrUnknownType,
// ErrUnknownCategory) — found while writing this task (see
// safeRejectionMessage in mensagem.go). Without that function, this test would be the
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
		t.Fatalf("status = %d, corpo = %s — quero 400 (tipo desconhecido)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), sentinelType) {
		t.Fatalf("a RESPOSTA ao consumidor nao citou o tipo que ele mesmo mandou — teste mal montado")
	}
	if strings.Contains(logBuf.String(), sentinelType) {
		t.Errorf("o log VAZOU o valor de `tipo` que so deveria voltar na resposta ao proprio consumidor: %q",
			logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "lojinha") {
		t.Errorf("log nao cita o slug da instancia: %q", logBuf.String())
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
		t.Fatalf("status = %d, quero 401", rec.Code)
	}
	if logBuf.Len() != 0 {
		t.Errorf("401 gerou log — token invalido e ruido de varredura, nunca deve logar: %q", logBuf.String())
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
		t.Fatalf("abrir banco para apagar a instancia: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM instancia WHERE slug = 'clinica'`); err != nil {
		t.Fatalf("apagar instancia clinica: %v", err)
	}

	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(http.DefaultClient, "http://127.0.0.1:1"),
		1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)

	body := `{"instancia":"clinica","para":"5511999990000","tipo":"texto","texto":"oi"}`

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	rec := ask(t, h, "token-do-c", "k-clinica-sumida", body)
	log.SetOutput(logStdout)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, corpo = %s — quero 404 (instancia desconhecida)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "clinica") {
		t.Errorf("404 de instancia desconhecida nao logou — deveria (T-037): %q", logBuf.String())
	}
}

// throttleLog is mutable state touched by every request over the SAME
// Handler (docs/ARMADILHAS.md, "Go / concorrência" — the same file already had
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
				t.Errorf("status = %d, quero 400", rec.Code)
			}
		}()
	}
	wg.Wait()
}
