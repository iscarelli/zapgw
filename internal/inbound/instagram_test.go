// T-097 (Instagram, first slice) tests, INPUT SIDE — the mandatory (b) and
// (c) of the task's Verify, plus an extra proof that the message event
// arrives with the counterpart as an IGSID (the input half of (e) — the
// OUTPUT half is in internal/outbound).
//
// DOES NOT TOUCH any WhatsApp test in this package — every helper here is
// NEW, and the only path shared with them is NewHandler itself, whose
// signature didn't change.
package inbound

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
)

// testHandlerAndDBInstagram builds a PAUSED INSTAGRAM instance — the
// same starting point testHandlerAndDB gives for WhatsApp.
func testHandlerAndDBInstagram(t *testing.T, callback, igID string) (http.Handler, string) {
	t.Helper()
	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "t.db")
	store, err := config.OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	err = store.CreateInstance(config.Instance{
		Slug: "insta-loja", Type: config.TypeInstagram, IgID: igID,
		AppSecret: "app-secret-de-teste", VerifyToken: "vt", SendToken: "te",
		CallbackURL: callback, DeliverySecret: "se", TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatalf("CreateInstance (instagram): %v", err)
	}

	return NewHandler(store, NewDeliverer(nil), 1<<20, config.NewCounter(store), config.NewTransit(store)), path
}

func activateInstagramInstance(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco para ativar: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE instancia SET ativo = 1 WHERE slug = 'insta-loja'`); err != nil {
		t.Fatalf("ativar instancia de teste: %v", err)
	}
}

// testInstagramPayload is the shape checked against T-097's Source
// (developers.facebook.com/docs/instagram-platform/webhooks/examples/ and
// developers.facebook.com/docs/messenger-platform/instagram/features/webhook/,
// read on 2026-07-30). SYNTHETIC IGID/IGSID, under CLAUDE.md's hard rule
// about real third-party data in the repository.
func testInstagramPayload(igID, igsid string) []byte {
	return []byte(`{"object":"instagram","entry":[{"id":"` + igID + `","time":1769000000,
	  "messaging":[{"sender":{"id":"` + igsid + `"},"recipient":{"id":"` + igID + `"},
	                "timestamp":1769000000123,"message":{"mid":"IGMID.TESTE","text":"oi"}}]}]}`)
}

// Verify (b): a wrong signature gets 403 and NEVER reaches the parser.
func TestInstagramHandlerRejectsInvalidSignatureWithoutDeliveringAnything(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBInstagram(t, consumer.URL, "IGID1")
	activateInstagramInstance(t, path)

	raw := testInstagramPayload("IGID1", "IGSID_SINTETICO_1")
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/insta-loja", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, quero 403", rec.Code)
	}
	if delivered {
		t.Fatal("entregou ao consumidor com assinatura invalida — o parser de Instagram nunca deveria ter rodado")
	}
}

// The POSITIVE PROOF sibling of the test above: the SAME signature
// (HMAC-SHA256 over the raw bytes, with the instance's app_secret —
// meta.SignatureValid, signature.go) that already serves WhatsApp also
// serves Instagram, with no new verification path at all. See the header of
// internal/meta/instagram.go for the source confirming Meta signs both
// products with the SAME Webhooks infrastructure.
func TestInstagramHandlerAcceptsValidSignatureAndDelivers(t *testing.T) {
	var bodyAtConsumer []byte
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(rawBody)
		bodyAtConsumer = rawBody
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBInstagram(t, consumer.URL, "IGID1")
	activateInstagramInstance(t, path)

	raw := testInstagramPayload("IGID1", "IGSID_SINTETICO_1")
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/insta-loja", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if len(bodyAtConsumer) == 0 {
		t.Fatal("nada foi entregue ao consumidor com assinatura valida")
	}
	// The proof that the event arrived typed, with the counterpart as an
	// IGSID — never "canonicalized" as a phone number (the INPUT half of
	// test (e)).
	if !strings.Contains(string(bodyAtConsumer), `"from_canonical":"IGSID_SINTETICO_1"`) {
		t.Errorf("corpo entregue nao traz from_canonical=IGSID_SINTETICO_1 intacto: %s", bodyAtConsumer)
	}
	if !strings.Contains(string(bodyAtConsumer), `"text":"oi"`) {
		t.Errorf("corpo entregue nao traz o texto da mensagem: %s", bodyAtConsumer)
	}
}

// Verify (c): an entry[].id that isn't the instance's own -> 200, batch
// discarded, counter goes up. SAME discipline as WhatsApp's 5b
// (TestHandlerRejectsAccountWebhookFromAnotherWaba, handler_test.go) — reuses
// the SAME counter key (config.CounterAccountDiscarded), a decision documented
// in handler.go itself.
func TestInstagramHandlerRejectsEntryIDFromAnotherInstance(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBInstagram(t, consumer.URL, "IGID1")
	activateInstagramInstance(t, path)

	// The payload arrives on "insta-loja"'s path (ig_id IGID1) but claims
	// to belong to IGID_ALHEIO.
	raw := testInstagramPayload("IGID_ALHEIO", "IGSID_SINTETICO_1")
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/insta-loja", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if delivered {
		t.Fatal("entregou webhook de Instagram cujo entry[].id nao e da instancia do path")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, quero 200 — reenviar repetiria a mesma divergencia por 36h", rec.Code)
	}
	if n := directCount(t, path, "insta-loja", config.CounterAccountDiscarded); n != 1 {
		t.Errorf("conta_descartada = %d, quero 1", n)
	}
}

// Sibling of the test above, in T-068's shape (unreadable waba_id): a
// MISSING entry[].id also has to be rejected, never treated as "" == "" of
// an instance with no ig_id (which ValidateInstanceType already prevents
// from existing).
func TestInstagramHandlerRejectsEntryWithMissingID(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBInstagram(t, consumer.URL, "IGID1")
	activateInstagramInstance(t, path)

	raw := []byte(`{"object":"instagram","entry":[{"time":1769000000,
	  "messaging":[{"sender":{"id":"IGSID_SINTETICO_1"},"recipient":{"id":"IGID1"},
	                "timestamp":1769000000123,"message":{"mid":"IGMID.TESTE","text":"oi"}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/insta-loja", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if delivered {
		t.Fatal("entregou webhook de Instagram sem entry[].id legivel — nao da para provar que e' desta instancia")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, quero 200", rec.Code)
	}
	if n := directCount(t, path, "insta-loja", config.CounterAccountDiscarded); n != 1 {
		t.Errorf("conta_descartada = %d, quero 1", n)
	}
}

// NARROW NON-REGRESSION: a WHATSAPP instance (Type == "", the default)
// NEVER goes through the `meta.ParseInstagramWebhook`/`meta.IgIDsInPayload`
// branch — it keeps falling into the handler's `else`, byte for byte.
// Proven by mutation: swapping `inst.Type == config.TypeInstagram` for
// `true` in the handler makes ALL of this package's WhatsApp isolation
// tests (TestHandlerRecusaWebhook*) go red, because
// `meta.IgIDsInPayload` doesn't know how to read
// `"object":"whatsapp_business_account"` and returns an empty list — the
// WhatsApp payload would silently pass the Instagram guard. This test only
// confirms the happy path stays the SAME:
// TestHandlerDeliversAndMirrors200 (handler_test.go) is already that proof for
// the common case.
func TestHandlerDefaultWhatsAppInstanceStaysOnTheOldBranch(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	// testHandlerAndDB (handler_test.go) creates the instance WITH NO
	// Type — the zero value of string, which ValidateInstanceType
	// normalizes to config.TypeWhatsApp on write.
	h, path := testHandlerAndDB(t, consumer.URL)
	activateInstanceInFile(t, path, "lojinha")

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
}

// --- T-105: the ECHO never becomes an event, but the raw body goes WHOLE ---
//
// The proof in internal/meta/instagram_test.go already covers the isolated
// PARSER; the two tests below prove the same thing at the HANDLER level,
// where the guarantee the task requires actually lives: the `cru` that
// reaches the consumer does NOT CHANGE because of the filter (deliver.go
// always sends the whole raw body, the event is a separate thing).

// testInstagramPayloadWithEcho is a batch with TWO messaging[] items: the
// CUSTOMER's message and the ECHO of the business's reply — the SAME
// sequence measured in production in T-105 (customer writes, business
// replies, Meta echoes the reply back). SYNTHETIC ids.
func testInstagramPayloadWithEcho(igID, igsidCustomer string) []byte {
	return []byte(`{"object":"instagram","entry":[{"id":"` + igID + `","time":1769000000,
	  "messaging":[
	    {"sender":{"id":"` + igsidCustomer + `"},"recipient":{"id":"` + igID + `"},
	     "timestamp":1769000000123,"message":{"mid":"IGMID.CLIENTE1","text":"oi, preciso de ajuda"}},
	    {"sender":{"id":"` + igID + `"},"recipient":{"id":"` + igsidCustomer + `"},
	     "timestamp":1769000000456,"message":{"mid":"IGMID.ECO1","text":"claro, um momento","is_echo":true}}
	  ]}]}`)
}

// T-105's Verify (a), in the shape of a batch with ONLY an echo: an empty
// events list and the unchanged raw body (the echo's own body, byte for
// byte) reach the consumer.
func TestInstagramHandlerEchoOnlyBatchDeliversNoEventButDeliversWholeRaw(t *testing.T) {
	var bodyAtConsumer []byte
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(rawBody)
		bodyAtConsumer = rawBody
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBInstagram(t, consumer.URL, "IGID1")
	activateInstagramInstance(t, path)

	raw := []byte(`{"object":"instagram","entry":[{"id":"IGID1","time":1769000000,
	  "messaging":[{"sender":{"id":"IGID1"},"recipient":{"id":"IGSID_CLIENTE_SINTETICO"},
	                "timestamp":1769000000456,"message":{"mid":"IGMID.ECO1","text":"claro, um momento","is_echo":true}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/insta-loja", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(bodyAtConsumer, &env); err != nil {
		t.Fatalf("envelope entregue nao e JSON: %s", bodyAtConsumer)
	}
	if len(env.Events) != 0 {
		t.Errorf("eventos = %d, quero 0 (o eco virou evento?): %+v", len(env.Events), env.Events)
	}
	// `Raw` is base64 of the EXACT bytes from Meta (deliver.go) — the
	// filter is only on the MODELED event, never on what's delivered:
	// decoded, `cru` has to carry the whole echo, is_echo and all.
	rawBody, err := base64.StdEncoding.DecodeString(env.Raw)
	if err != nil {
		t.Fatalf("env.Raw nao e base64: %v", err)
	}
	if !strings.Contains(string(rawBody), `"is_echo":true`) {
		t.Errorf("cru decodificado nao traz o payload original com is_echo:true: %s", rawBody)
	}
}

// T-105's Verify (b): a batch with a CUSTOMER message and an ECHO -> only
// the customer's message becomes an event. This is the test that proves the
// RIGHT item was filtered (not the whole batch) — the task's hardest
// guarantee.
func TestInstagramHandlerFiltersEchoFromBatchAndDeliversOnlyCustomerMessage(t *testing.T) {
	var bodyAtConsumer []byte
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(rawBody)
		bodyAtConsumer = rawBody
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBInstagram(t, consumer.URL, "IGID1")
	activateInstagramInstance(t, path)

	raw := testInstagramPayloadWithEcho("IGID1", "IGSID_CLIENTE_SINTETICO")
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/insta-loja", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(bodyAtConsumer, &env); err != nil {
		t.Fatalf("envelope entregue nao e JSON: %s", bodyAtConsumer)
	}
	// Only the CUSTOMER's event shows up in the `eventos` array — this is
	// the proof that the RIGHT item was filtered, not the whole batch.
	if len(env.Events) != 1 {
		t.Fatalf("eventos = %d, quero 1 (so a mensagem do cliente): %+v", len(env.Events), env.Events)
	}
	ev := env.Events[0]
	if ev.WaMessageID != "IGMID.CLIENTE1" {
		t.Errorf("WaMessageID = %q, quero IGMID.CLIENTE1 (o eco IGMID.ECO1 nao pode aparecer como evento)", ev.WaMessageID)
	}
	if ev.FromCanonical != "IGSID_CLIENTE_SINTETICO" {
		t.Errorf("FromCanonical = %q, quero o IGSID do CLIENTE", ev.FromCanonical)
	}
	// The RAW body stays WHOLE, with the echo also present (byte for byte,
	// the consumer stores everything before looking at `eventos`).
	rawBody, err := base64.StdEncoding.DecodeString(env.Raw)
	if err != nil {
		t.Fatalf("env.Raw nao e base64: %v", err)
	}
	if !strings.Contains(string(rawBody), `"is_echo":true`) {
		t.Errorf("cru decodificado nao traz o eco original (is_echo:true): %s", rawBody)
	}
	if !strings.Contains(string(rawBody), `IGMID.ECO1`) {
		t.Errorf("cru decodificado nao traz o mid do eco (IGMID.ECO1) — o cru nao pode ter sido filtrado: %s", rawBody)
	}
}

// --- T-110: the journal distinguishes UNREADABLE from UNMODELED -----------
//
// T-106 (2026-07-30) split the two apart in internal/meta, but
// internal/inbound/handler.go kept calling both of them "parse failed" —
// measured in production on v0.40.0 (2026-07-31 06:52 -03), firing the
// failure monitor on perfectly normal Instagram traffic. The four tests
// below are the task's Verify (a)-(d).

// testInstagramPayloadUnreadableItem has ONE messaging[] item whose
// `message` DID arrive but in a shape instagramMessageBodyMeta doesn't
// know how to read (a number instead of an object) — the ONLY shape in
// which "cannot be read" is true (blockUnreadable, meta/instagram.go). Counts
// toward meta.ErrPartialParse.
func testInstagramPayloadUnreadableItem(igID string) []byte {
	return []byte(`{"object":"instagram","entry":[{"id":"` + igID + `","time":1769000000,
	  "messaging":[{"sender":{"id":"IGSID_ILEGIVEL"},"recipient":{"id":"` + igID + `"},
	                "timestamp":1769000000123,"message":42}]}]}`)
}

// testInstagramPayloadUnmodeledItem has ONE messaging[] item with NO
// `message` block at all — a read receipt, a reaction to a story, a
// postback: read successfully, legitimate, this slice (T-097) just doesn't
// model it (blockAbsent). Counts toward meta.ErrUnmodeledItems.
func testInstagramPayloadUnmodeledItem(igID string) []byte {
	return []byte(`{"object":"instagram","entry":[{"id":"` + igID + `","time":1769000000,
	  "messaging":[{"sender":{"id":"IGSID_NAO_MODELADO"},"recipient":{"id":"` + igID + `"},
	                "timestamp":1769000000123}]}]}`)
}

// testInstagramPayloadMixed has BOTH in the SAME batch — the case the
// task flags as the one a careless test lets slip through: if the handler's
// check tests ErrUnmodeledItems first, with an else, the unreadable item
// disappears from the journal.
func testInstagramPayloadMixed(igID string) []byte {
	return []byte(`{"object":"instagram","entry":[{"id":"` + igID + `","time":1769000000,
	  "messaging":[
	    {"sender":{"id":"IGSID_ILEGIVEL"},"recipient":{"id":"` + igID + `"},
	     "timestamp":1769000000123,"message":42},
	    {"sender":{"id":"IGSID_NAO_MODELADO"},"recipient":{"id":"` + igID + `"},
	     "timestamp":1769000000456}
	  ]}]}`)
}

// sendToInstagramHandlerWithCapturedLog builds the instance, activates it,
// sends the `cru` and returns the log content captured during ServeHTTP.
// Helper shared by the four tests (a)-(d) below.
func sendToInstagramHandlerWithCapturedLog(t *testing.T, raw []byte) (rec *httptest.ResponseRecorder, logged string) {
	t.Helper()
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBInstagram(t, consumer.URL, "IGID1")
	activateInstagramInstance(t, path)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/insta-loja", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec = httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	return rec, buf.String()
}

// (a) a batch with ONLY an echo -> no unreadable item and no unmodeled one,
// the parse returns no error at all -> no journal line about the parse.
func TestInstagramHandlerEchoOnlyBatchLogsNothingAboutParse(t *testing.T) {
	raw := []byte(`{"object":"instagram","entry":[{"id":"IGID1","time":1769000000,
	  "messaging":[{"sender":{"id":"IGID1"},"recipient":{"id":"IGSID_CLIENTE"},
	                "timestamp":1769000000456,"message":{"mid":"IGMID.ECO1","text":"ok","is_echo":true}}]}]}`)

	rec, logged := sendToInstagramHandlerWithCapturedLog(t, raw)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if logged != "" {
		t.Fatalf("lote so com eco nao deveria deixar rastro nenhum de parse no journal: %q", logged)
	}
}

// (b) a batch with an UNMODELED item -> an informational line, with no
// "falhou" and no "erro" — it isn't a failure, and the failure monitor
// can't fire because of it.
func TestInstagramHandlerUnmodeledItemLogsInformationalWithoutFailed(t *testing.T) {
	raw := testInstagramPayloadUnmodeledItem("IGID1")

	rec, logged := sendToInstagramHandlerWithCapturedLog(t, raw)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logged, `instancia "insta-loja"`) {
		t.Fatalf("journal sem mencionar a instancia: %q", logged)
	}
	if !strings.Contains(logged, "item legitimo que esta fatia do instagram nao modela") {
		t.Fatalf("journal nao traz o texto do sentinela ErrUnmodeledItems: %q", logged)
	}
	if strings.Contains(logged, "falhou") {
		t.Fatalf("item nao modelado nao e falha — a palavra 'falhou' nao pode aparecer: %q", logged)
	}
	if strings.Contains(logged, "erro") {
		t.Fatalf("item nao modelado nao e erro — a palavra 'erro' nao pode aparecer: %q", logged)
	}
}

// (c) a batch with an UNREADABLE item -> TODAY's line, with "parse
// falhou," checked against the exact text (a constant, not a visual
// inspection).
func TestInstagramHandlerUnreadableItemLogsParseFailedWithExactText(t *testing.T) {
	const exactText = `zapgw: parse falhou na instancia "insta-loja": meta: parte do payload nao pode ser lida: 1 item(ns) ignorado(s)`

	raw := testInstagramPayloadUnreadableItem("IGID1")

	rec, logged := sendToInstagramHandlerWithCapturedLog(t, raw)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logged, exactText) {
		t.Fatalf("journal nao contem o texto exato de hoje.\nquero conter: %s\njournal: %q", exactText, logged)
	}
}

// (d) 🔴 THE CASE THAT DECIDES THE TASK: a MIXED batch (unreadable +
// unmodeled) has to come out as a FAILURE — never as informational only.
// It's the proof that the handler's errors.Is order is correct
// (ErrPartialParse checked BEFORE ErrUnmodeledItems). The Verify's
// mandatory mutation (reversing the order, with an else) has to make this
// test go red.
func TestInstagramHandlerMixedBatchLogsFailureNeverOnlyInformational(t *testing.T) {
	const exactText = `zapgw: parse falhou na instancia "insta-loja": meta: parte do payload nao pode ser lida: 1 item(ns) ignorado(s)`

	raw := testInstagramPayloadMixed("IGID1")

	rec, logged := sendToInstagramHandlerWithCapturedLog(t, raw)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logged, exactText) {
		t.Fatalf("lote misto tem de sair com a linha de FALHA (o item ilegivel nao pode sumir).\nquero conter: %s\njournal: %q", exactText, logged)
	}
	if !strings.Contains(logged, "item legitimo que esta fatia do instagram nao modela") {
		t.Fatalf("lote misto tambem deveria carregar a informacao do item nao modelado (errors.Join): %q", logged)
	}
}
