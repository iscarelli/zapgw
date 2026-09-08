package inbound

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"

	_ "modernc.org/sqlite" // only to activate a test instance via direct UPDATE
)

const testCipherKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func signAsMeta(raw []byte, appSecret string) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(raw)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// builds a handler with a "lojinha" instance pointing at `callback`,
// PAUSED — the natural state in which the store creates every instance
// (config.CreateInstance writes ativo=0 fixed). Use activeTestHandler when
// the test expects the handler to actually process/deliver.
func testHandler(t *testing.T, callback string) http.Handler {
	t.Helper()
	h, _ := testHandlerAndDB(t, callback)
	return h
}

// like testHandler, but ACTIVATES the instance before returning the handler.
//
// The store doesn't expose activation (only the smoke test, a future plan,
// activates for real — through a dashboard). Here, for tests only, we open a
// SECOND sqlite connection to the SAME file and run the UPDATE directly.
// internal/config stays untouched.
func activeTestHandler(t *testing.T, callback string) http.Handler {
	t.Helper()
	return activeTestHandlerWithCap(t, callback, 1<<20)
}

// like activeTestHandler, but with the body cap chosen by the test — the
// way to exercise the 413 path without building a 1 MiB body.
func activeTestHandlerWithCap(t *testing.T, callback string, maxBytes int) http.Handler {
	t.Helper()
	h, path := testHandlerAndDBWithCap(t, callback, maxBytes)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to activate test instance: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE instancia SET ativo = 1 WHERE slug = ?`, "lojinha"); err != nil {
		t.Fatalf("activate test instance: %v", err)
	}
	return h
}

func testHandlerAndDB(t *testing.T, callback string) (http.Handler, string) {
	t.Helper()
	return testHandlerAndDBWithCap(t, callback, 1<<20)
}

func testHandlerAndDBWithCap(t *testing.T, callback string, maxBytes int) (http.Handler, string) {
	t.Helper()
	return testHandlerAndDBWithIdentity(t, callback, maxBytes, "WABA1", "PNID1")
}

// testHandlerAndDBWithIdentity is the one above with the instance's
// waba_id and phone_number_id chosen by the test.
//
// EXISTS FOR THE TESTS THAT FEED THE HANDLER WITH THE CORPUS
// (testdata/corpus, T-063): those payloads are real Meta captures and carry
// `WABA_TESTE`/`PNID_TESTE`, so either the test instance has those ids or
// the 5a/5b guard rejects the batch before any counting happens — and the
// test would end up proving isolation instead of what it claims to prove.
// Rewriting the payload to fit the instance would be worse: it would erase
// the capture's provenance, which is the only reason to use the corpus here
// at all.
func testHandlerAndDBWithIdentity(
	t *testing.T, callback string, maxBytes int, wabaID, phoneNumberID string,
) (http.Handler, string) {
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
		Slug: "lojinha", WabaID: wabaID, PhoneNumberID: phoneNumberID,
		AppSecret: "app-secret-de-teste", VerifyToken: "vt", SendToken: "te",
		CallbackURL: callback, DeliverySecret: "se", TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	return NewHandler(store, NewDeliverer(nil), maxBytes, config.NewCounter(store), config.NewTransit(store)), path
}

func testPayload() []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.A","timestamp":"1769000000",
	                "type":"text","text":{"body":"oi"}}]}}]}]}`)
}

func TestHandlerDeliversAndMirrors200(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	activeTestHandler(t, consumer.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandlerRejectsInvalidSignatureWithoutDeliveringAnything(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(testPayload())))
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()

	// Activate: the signature check comes AFTER the pause check, so the
	// instance needs to be active for this test to exercise the signature.
	activeTestHandler(t, consumer.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if delivered {
		t.Fatal("delivered to the consumer with an invalid signature")
	}
}

// TestHandlerRejectsUnknownSlugWith404 pins down the status AND the
// EXACT BODY of the 404. The body isn't a detail: it's the only thing that
// separates "the gateway answered" from "something in front of it
// answered" — Traefik also returns 404, also in text/plain, also written
// in Go.
//
// 🔴 THE "instancia desconhecida" MESSAGE LIVES IN THREE PLACES, and this
// test is what ties the three together (T-119):
//
//  1. internal/inbound/handler.go — the source (the two http.Error calls);
//  2. implanta/sonda-publica.sh   — const CORPO_DO_GATEWAY;
//  3. sonda-worker/src/index.js   — const CORPO_DO_GATEWAY.
//
// The last two are probes that run OUTSIDE this suite. Without this test,
// whoever changed the handler's message would find out from the probe
// turning red in production, with the public path intact — an expensive
// false alarm, disconnected from its cause. With it, it goes red HERE, and
// the list above says what else to change in the same commit.
//
// The GET is covered on purpose: it's the method both probes use.
func TestHandlerRejectsUnknownSlugWith404(t *testing.T) {
	// 23 bytes, counting the \n that http.Error appends. This number is
	// cited as proof in docs/IMPLANTACAO.md and in both probes.
	const gatewayBody = "instancia desconhecida\n"

	cases := []struct {
		name   string
		method string
		body   string
	}{
		{"POST — Meta delivering", http.MethodPost, "{}"},
		{"GET — what both probes ask", http.MethodGet, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "/v1/inbound/nao-existe", strings.NewReader(c.body))
			rec := httptest.NewRecorder()

			testHandler(t, "http://127.0.0.1:1").ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if got := rec.Body.String(); got != gatewayBody {
				t.Fatalf("body = %q, want %q — changing this message breaks implanta/sonda-publica.sh and sonda-worker/src/index.js; change all three in the same commit",
					got, gatewayBody)
			}
			if n := rec.Body.Len(); n != 23 {
				t.Fatalf("body has %d bytes, want 23 — that's the number the doc and the probes cite as proof that it was the gateway that answered", n)
			}
		})
	}
}

// INVARIANT 2: the parse is ENRICHMENT, never a precondition for delivery.
func TestHandlerDeliversTheRawEvenWithTheParseFailing(t *testing.T) {
	var bodyAtConsumer []byte
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyAtConsumer, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	raw := []byte(`null`) // valid JSON, not an object
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	activeTestHandler(t, consumer.URL).ServeHTTP(rec, req)

	if len(bodyAtConsumer) == 0 {
		t.Fatal("the parse failed and NOTHING was delivered — that's exactly the silent loss the gateway exists to end")
	}
	if !strings.Contains(string(bodyAtConsumer), `"parse_error"`) {
		t.Errorf("envelope with no parse_error: %s", bodyAtConsumer)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (the consumer stored the raw body)", rec.Code)
	}
}

// foreignNumberPayload builds a MESSAGE webhook whose
// metadata.phone_number_id is NOT "lojinha" instance's own ("PNID1"). The
// entry[].id stays "WABA1", its REAL waba — on purpose: this way whoever
// rejects it is necessarily the 5a guard (phone_number_id), never 5b
// (waba_id), and a counting test can't pass by coincidence by looking at
// the neighbor's rejection.
func foreignNumberPayload() []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID-DE-OUTRO"},
	   "messages":[{"from":"5511999990000","id":"wamid.A","timestamp":"1","type":"text","text":{"body":"oi"}}]}}]}]}`)
}

// The body's phone_number_id has to belong to the PATH instance. Without
// this, a valid signature from one App could carry another instance's number.
//
// T-047: the rejection also COUNTS, under its own key. Up to this point it
// counted nothing — the alarm was triggered with live traffic in T-042
// (2026-07-26), came out correctly in the journal, and the instance kept
// showing `recebidas 1` and `conta_descartada 0`: the isolation rejection
// was invisible in `zapgw estado`.
func TestHandlerRejectsPhoneNumberIDFromAnotherInstance(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	raw := foreignNumberPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if delivered {
		t.Fatal("delivered an event whose phone_number_id is not the path instance's")
	}
	// 200 + alarm: redelivering would repeat the same failure for 36h.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if n := directCount(t, path, "lojinha", config.CounterNumberDiscarded); n != 1 {
		t.Errorf("numero_descartado = %d, want 1 — the isolation rejection by phone_number_id"+
			" has to show up in `zapgw estado`, not just in the journal", n)
	}
	// NON-REGRESSION: conta_descartada remains the EXCLUSIVE key for 5b
	// (waba_id). If the new key were written to the wrong place — or if
	// someone "simplified" by merging the two into one —, this test flags it.
	if n := directCount(t, path, "lojinha", config.CounterAccountDiscarded); n != 0 {
		t.Errorf("conta_descartada = %d, want 0 — the phone_number_id guard (5a) is what rejected it, not the waba one (5b)", n)
	}
	// T-047's DECISION, written as a test: an early exit does NOT count
	// `recebidas`. Here `recebidas` means "webhook that made it through to
	// delivery," and it applies equally to 404, 503, 413, 403, 5a and 5b —
	// making only these two branches count would create a new asymmetry
	// among early exits, which is the exact shape of this project's
	// mother-trap.
	if n := directCount(t, path, "lojinha", config.CounterReceived); n != 0 {
		t.Errorf("recebidas = %d, want 0 — an early exit does not count recebidas (see the comment on step 5 in handler.go)", n)
	}
}

// T-047, the other side: a MATCHING phone_number_id can't count any
// rejection. Without this test, an inverted guard (counting when it
// matches) would pass green on the test above and would only show up in
// production, as a number that grows without any rejection having happened.
func TestHandlerDoesNotCountDiscardedNumberWhenPhoneNumberIDMatches(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	raw := testPayload() // phone_number_id "PNID1" — the instance's own
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if n := directCount(t, path, "lojinha", config.CounterNumberDiscarded); n != 0 {
		t.Errorf("numero_descartado = %d, want 0 (the number matches, it's not a discard)", n)
	}
	if n := directCount(t, path, "lojinha", config.CounterReceived); n != 1 {
		t.Errorf("recebidas = %d, want 1", n)
	}
}

// templateAccountPayload builds a real Meta ACCOUNT webhook
// (message_template_status_update — NOT message/status): no
// metadata.phone_number_id, only the waba_id from entry[].id as the routing
// key. Shape checked against
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/message_template_status_update/
// (read on 2026-07-26); there is no real-capture corpus for this case yet
// (no template of this gateway's has changed status in production).
func templateAccountPayload(wabaID string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"` + wabaID + `","time":1751247548,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"APPROVED","message_template_id":1689556908129832,
	    "message_template_name":"lembrete","message_template_language":"pt_BR",
	    "reason":"NONE","message_template_category":"UTILITY"}}]}]}`)
}

// T-038: an ACCOUNT webhook (template status) carries no phone_number_id —
// the ONLY routing key it brings is the waba_id from entry[].id. If the
// waba isn't the path instance's own, the gateway CANNOT deliver "to
// whoever shows up first": the consumer checks envelope["instancia"]
// against THEIR OWN slug, the old guard would pass (WE were the ones who
// put the path's slug in the envelope), and they would write to their
// database, permanently, the raw body of an event that could belong to
// another tenant.
//
// The fixture's waba_id is deliberately EQUAL to the instance's own
// phone_number_id ("PNID1") — NOT to its waba_id ("WABA1"). That's what
// makes the MUTATION the task requires (comparing entry[].id against the
// instance's phone_number_id instead of the waba_id) turn this test red:
// with the wrong comparison, "PNID1" would match inst.PhoneNumberID and the
// webhook would be delivered, when the correct behavior is to reject it
// (the real waba is "WABA1," not "PNID1").
func TestHandlerRejectsAccountWebhookFromAnotherWaba(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	raw := templateAccountPayload("PNID1")
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if delivered {
		t.Fatal("delivered an account webhook whose waba_id is not the path instance's")
	}
	// 200: redelivering would repeat the same configuration mismatch for 36h.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if n := directCount(t, path, "lojinha", config.CounterAccountDiscarded); n != 1 {
		t.Errorf("conta_descartada = %d, want 1", n)
	}
}

// T-068: an UNREADABLE waba_id does not become "passes." This is the
// task's decision — `entry.id` stopped being a `string` and became
// `json.RawMessage`, so a shape this parser doesn't know how to read no
// longer brings the entry down: it reaches the guard as "". And "" has to
// be treated as NOT-MATCHING.
//
// WHY THIS WAY OUT, and not "discard the entry with its own counter" (the
// other option the task allowed): guard 5b rejects the WHOLE BATCH
// precisely because the RAW body travels along in the delivery. Discarding
// only the EVENTS from that entry would still let that account's content
// reach the path slug's consumer, inside the `cru` — a defense that looks
// like a defense and isn't, which is the shape of this project's
// mother-trap ("the guarantee was INERT").
//
// The price is written down and accepted: an ACCOUNT webhook with a new
// `entry.id` shape is lost. But it's an ANNOUNCED loss — ALARME in the
// journal and `conta_descartada` in the `zapgw estado` table —, not a
// silent one. Wrong routing fixes itself by repointing; data from an
// account we can't confirm is ours, written into someone else's database,
// doesn't undo itself.
//
// MANDATORY MUTATION (done and reverted before the commit): removing the
// `waba == ""` from the guard makes this test go red — the delivery
// happens and conta_descartada stays at 0.
func TestHandlerRejectsAccountWebhookWithUnreadableWabaID(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	// `"id":42` — a number where Meta sends text. It must NOT become the
	// waba_id "42," and much less pass as no waba_id at all.
	raw := []byte(`{"object":"whatsapp_business_account","entry":[{"id":42,"time":1751247548,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"APPROVED","message_template_id":1689556908129832,
	    "message_template_name":"lembrete","message_template_language":"pt_BR",
	    "reason":"NONE","message_template_category":"UTILITY"}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if delivered {
		t.Fatal("delivered an account webhook whose waba_id cannot be read — there is no way to prove it belongs to this instance")
	}
	// 200 for the same reason as the other two isolation rejections:
	// redelivering would repeat the same mismatch for 36h, and the fix is
	// a person.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// The SAME key as 5b: whoever reads the table asks "was there an
	// account-level isolation rejection?", and the answer is the same in
	// both cases.
	if n := directCount(t, path, "lojinha", config.CounterAccountDiscarded); n != 1 {
		t.Errorf("conta_descartada = %d, want 1 — the rejection has to be VISIBLE in `zapgw estado`", n)
	}
}

// The test the MUTATION demanded, and it only exists because the mutation
// passed GREEN (T-068). Removing `waba == ""` from guard 5b did not make
// TestHandlerRejectsAccountWebhookWithUnreadableWabaID go red: that test's
// instance has waba_id "WABA1," so `"" != "WABA1"` rejects it the same way
// regardless. It's the same lesson already recorded in docs/ARMADILHAS.md
// about T-047's order mutation — **when a mutation passes green, ask what
// the real defense is**. Here it was "every instance has a filled-in
// waba_id," and that is true of TODAY'S PATH, not of the type: until T-074,
// `config.Store.CreateInstance` validated slug, callback_url and bundle_ca,
// and did NOT validate waba_id — the only thing that validated it was
// `zapgw provisionar instancia` (cmd/zapgw/provision.go), the FIRST
// creation path. A future seed or admin endpoint would be born without that
// check — the exact scenario CreateInstance's own comment raises.
//
// With an empty waba_id on BOTH sides, `"" != ""` is false and the guard
// would pass SILENTLY, delivering to the consumer an account webhook no one
// managed to attribute to anyone. That's the hole the explicit
// `waba == ""` closes, and this is the test that proves it: with the
// mutation applied, it goes red.
//
// WHY THE waba_id IS EMPTIED VIA SQL, rather than asked of CreateInstance
// (T-074): since T-074 the store REFUSES to create an instance without a
// waba_id (config.ValidateIdentification) — that was the fix chosen to close
// the hole AT THE SOURCE. That doesn't retire this test: validation at
// creation time doesn't reach a row that's already in the database (a
// database written by an earlier binary, a hand-typed UPDATE). This test
// went on to model exactly that row, which is the only way left for an
// instance with no waba_id to reach the handler — and it's still what goes
// red if someone removes the `waba == ""` from guard 5b.
func TestHandlerRejectsUnreadableWabaIDEvenIfInstanceHasNoWabaID(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

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
	if err := store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "app-secret-de-teste", VerifyToken: "vt", SendToken: "te",
		CallbackURL: consumer.URL, DeliverySecret: "se", TimeoutMs: 2000,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	// EMPTY waba_id on purpose, via direct SQL: the row the store no longer
	// lets get BORN (T-074) but also can't undo if it's already there.
	clearWabaIDInFile(t, path, "lojinha")
	h := NewHandler(store, NewDeliverer(nil), 1<<20, config.NewCounter(store), config.NewTransit(store))
	activateInstanceInFile(t, path, "lojinha")

	raw := []byte(`{"object":"whatsapp_business_account","entry":[{"id":42,"time":1751247548,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"APPROVED","message_template_id":1689556908129832,
	    "message_template_name":"lembrete","message_template_language":"pt_BR",
	    "reason":"NONE","message_template_category":"UTILITY"}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if delivered {
		t.Fatal("delivered: unreadable waba_id matched the instance's EMPTY waba_id — the guard turned into decoration")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if n := directCount(t, path, "lojinha", config.CounterAccountDiscarded); n != 1 {
		t.Errorf("conta_descartada = %d, want 1", n)
	}
}

// T-038, the other side of Verify (a)/(b): a MATCHING waba_id delivers the
// account webhook, byte for byte identical to the raw body Meta sent — the
// new guard can't block the legitimate case (the same WABA as the path
// instance's own).
func TestHandlerDeliversAccountWebhookWithMatchingWabaID(t *testing.T) {
	var receivedBody []byte
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	raw := templateAccountPayload("WABA1") // "WABA1" is "lojinha" instance's real waba_id
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(receivedBody) == 0 {
		t.Fatal("account webhook with a matching waba_id was NOT delivered")
	}
	var env Envelope
	if err := json.Unmarshal(receivedBody, &env); err != nil {
		t.Fatalf("delivered body is not the expected Envelope: %v", err)
	}
	if env.Raw != base64.StdEncoding.EncodeToString(raw) {
		t.Error("Envelope.Raw is not the EXACT raw body of the account webhook")
	}
	if n := directCount(t, path, "lojinha", config.CounterAccountDiscarded); n != 0 {
		t.Errorf("conta_descartada = %d, want 0 (the waba matches, it's not a discard)", n)
	}
}

// The Host header decides NOTHING. That's what lets the gateway serve each
// client's own domain (zapgw.clientea.com.br, zapgw.clienteb.com.br) with
// no new line of code — and it's safer: a forged Host doesn't change the
// destination.
func TestHandlerIgnoresTheHost(t *testing.T) {
	var howMany int
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		howMany++
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h := activeTestHandler(t, consumer.URL)
	raw := testPayload()

	for _, host := range []string{"zapgw.tenant-one.com.br", "zapgw.outrocliente.com.br", "127.0.0.1"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
		req.Host = host
		req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Host %q gave status %d, want 200", host, rec.Code)
		}
	}
	if howMany != 3 {
		t.Fatalf("deliveries = %d, want 3 — the Host changed the destination", howMany)
	}
}

func TestHandlerRejectsBodyAboveTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	vault, _ := config.NewVault(testCipherKey)
	store, _ := config.OpenStore(path, vault)
	defer store.Close()
	_ = store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "W", PhoneNumberID: "P", AppSecret: "a",
		VerifyToken: "v", SendToken: "t", CallbackURL: "http://127.0.0.1:1",
		DeliverySecret: "s", TimeoutMs: 100,
	})
	// The pause check comes BEFORE reading the body — this test needs the
	// instance active, otherwise the pause's 503 masks the 413 it's testing.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to activate test instance: %v", err)
	}
	if _, err := db.Exec(`UPDATE instancia SET ativo = 1 WHERE slug = ?`, "lojinha"); err != nil {
		t.Fatalf("activate test instance: %v", err)
	}
	db.Close()
	h := NewHandler(store, NewDeliverer(nil), 10, config.NewCounter(store), config.NewTransit(store)) // 10-byte cap

	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(strings.Repeat("x", 11)))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

// GET is Meta's webhook verification, and it uses a DIFFERENT secret (verify_token).
func TestHandlerAnswersTheVerificationChallenge(t *testing.T) {
	vault, _ := config.NewVault(testCipherKey)
	store, _ := config.OpenStore(filepath.Join(t.TempDir(), "t.db"), vault)
	defer store.Close()
	_ = store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "W", PhoneNumberID: "P", AppSecret: "a",
		VerifyToken: "meu-verify-token", SendToken: "t",
		CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 100,
	})
	h := NewHandler(store, NewDeliverer(nil), 1<<20, config.NewCounter(store), config.NewTransit(store))

	req := httptest.NewRequest(http.MethodGet,
		"/v1/inbound/lojinha?hub.mode=subscribe&hub.verify_token=meu-verify-token&hub.challenge=DESAFIO123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "DESAFIO123" {
		t.Fatalf("status=%d body=%q, want 200 and DESAFIO123", rec.Code, rec.Body.String())
	}

	reqWrong := httptest.NewRequest(http.MethodGet,
		"/v1/inbound/lojinha?hub.mode=subscribe&hub.verify_token=errado&hub.challenge=X", nil)
	recWrong := httptest.NewRecorder()
	h.ServeHTTP(recWrong, reqWrong)

	if recWrong.Code != http.StatusForbidden {
		t.Fatalf("wrong verify_token gave %d, want 403", recWrong.Code)
	}
}

func TestHandlerRejectsPausedInstance(t *testing.T) {
	// The instance is born paused by design. Without this guard in the
	// handler, that guarantee is decorative — the store pauses and the
	// webhook delivers anyway.
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h := testHandler(t, consumer.URL) // does NOT activate the instance
	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if delivered {
		t.Fatal("PAUSED instance delivered to the consumer")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — a 200 would discard the message permanently", rec.Code)
	}
}

// CRITICAL found in the adversarial re-review (F6). mirror.go only marks
// Alarm=true on branches that return StatusForMeta 200, and the handler
// only logged when StatusForMeta was NOT 2xx — the two conditions are
// mutually exclusive, so the ALARME prefix (which mirror.go promises for
// permanent loss) never fired. A consumer that returns 404 is exactly this
// case: we respond 200 to Meta, it never redelivers, and the log was the
// only safety net.
func TestHandlerLogsALARMEWhenConsumerRefusesTheDocument(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer consumer.Close()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr) // os.Stderr is the log package's default

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	activeTestHandler(t, consumer.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (Meta cannot redeliver a permanent refusal)", rec.Code)
	}
	if !strings.Contains(buf.String(), "ALARME") {
		t.Fatalf("log with no ALARME for a consumer that REFUSED (404) — permanent loss with no one knowing. log:\n%s", buf.String())
	}
}

// Symmetric case of the same finding: a consumer that returns 500 is a
// TRANSIENT failure, Meta redelivers on its own for up to 36h, and no one
// needs to act NOW. The log has to fire (the trace matters), but WITHOUT
// the ALARME prefix — alarming on what fixes itself trains whoever
// operates it to ignore the alarm.
func TestHandlerLogsWithoutALARMEWhenConsumerFailsTransiently(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer consumer.Close()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	activeTestHandler(t, consumer.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (Meta redelivers on its own)", rec.Code)
	}
	if !strings.Contains(buf.String(), "correlacao=") {
		t.Fatalf("transient failure logged nothing, and tracing matters here. log:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "ALARME") {
		t.Fatalf("log with ALARME for a TRANSIENT failure (500) — Meta already redelivers on its own, no one needs to act. log:\n%s", buf.String())
	}
}

// A body over the cap is a PERMANENT loss when it repeats: Meta redelivers
// the same body, gets 413 again, and within 36h gives up. The isolated 413
// does not alarm (Meta will still redeliver), but repetition needs a
// person — and until T-002 no ALARME ever came out of this path.
func TestHandlerAlarmsOnlyAfterLargeBodyThreshold(t *testing.T) {
	h := activeTestHandlerWithCap(t, "http://127.0.0.1:1", 10)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// Distinctive marker: the rejected body must NOT appear in the log (it
	// carries personal data). We look for the marker, not the whole entry.
	body := strings.Repeat("marca-do-corpo-recusado", 8)
	send := func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", rec.Code)
		}
	}

	for i := 1; i < largeBodyThreshold; i++ {
		send()
	}
	if strings.Contains(buf.String(), "ALARME") {
		t.Fatalf("alarmed before the threshold — an alarm that fires on the isolated event (which Meta will still redeliver) trains whoever operates it to ignore it. log:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "body above the cap") {
		t.Fatalf("a rejection below the threshold stopped leaving a trace in the log — tracing matters even without an alarm. log:\n%s", buf.String())
	}

	send() // the one that crosses the threshold
	if n := strings.Count(buf.String(), "ALARME"); n != 1 {
		t.Fatalf("ALARME fired %d time(s) when crossing the threshold, want exactly 1. log:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), `"lojinha"`) {
		t.Errorf("the ALARME does not say WHICH instance: log:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "ZAPGW_MAX_CORPO_BYTES") {
		t.Errorf("the ALARME does not say what the person needs to DO: log:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "marca-do-corpo-recusado") {
		t.Errorf("the rejected body leaked into the log: %s", buf.String())
	}

	send()
	send()
	if n := strings.Count(buf.String(), "ALARME"); n != 1 {
		t.Fatalf("ALARME repeated (%d) within the SAME window — one alarm per event turns into noise and disappears along with what matters. log:\n%s", n, buf.String())
	}
}

// The tests above prove the MECHANISM against the constants, so swapping
// the constants would slip through unnoticed — and two of those swaps
// aren't policy tuning, they're defects. This test only pins the limits at
// which the guarantee stops existing; the exact value within them stays free.
func TestLargeBodyAlarmConstantsKeepTheGuarantee(t *testing.T) {
	if largeBodyThreshold < 2 {
		t.Errorf("threshold = %d: with 1, the ISOLATED event alarms and the contract's 'and none before' half stops existing",
			largeBodyThreshold)
	}
	// Meta redelivers for up to 36h and then gives up (docs/ARMADILHAS.md).
	// A window longer than that turns the counter into a lifetime
	// accumulator: the alarm would arrive after the message was already
	// lost, which is the same as it never arriving.
	if largeBodyWindow <= 0 || largeBodyWindow > 36*time.Hour {
		t.Errorf("window = %s: outside (0, 36h] the alarm stops meaning 'this is happening now, there's time to act'",
			largeBodyWindow)
	}
}

// The window has to RESET. Without this the counter turns into a lifetime
// accumulator: three rejections spread over three months would alarm as if
// something were happening right now, and the alarm would stop meaning
// "act today."
func TestRejectionCounterResetsTheWindow(t *testing.T) {
	c := newRejectionCounter(3, time.Hour)
	clock := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }

	c.record("lojinha")
	if _, alarmed := c.record("lojinha"); alarmed {
		t.Fatal("alarmed on the second rejection, with threshold 3")
	}

	clock = clock.Add(time.Hour) // the window rolled over

	n, alarmed := c.record("lojinha")
	if n != 1 || alarmed {
		t.Fatalf("after the window rolled over, got n=%d alarmed=%v, want n=1 and no alarm — the count has to restart", n, alarmed)
	}
	c.record("lojinha")
	if _, alarmed := c.record("lojinha"); !alarmed {
		t.Fatal("the new window does not alarm at the threshold — resetting cannot turn off the alarm")
	}
}

// The count is PER INSTANCE. Adding everything into one global counter
// would make the alarm arise from the sum of healthy instances and point
// at the wrong one — and, worse, it would make a gateway with many clients
// alarm even though none of them has a problem.
func TestRejectionCounterCountsPerInstance(t *testing.T) {
	c := newRejectionCounter(3, time.Hour)

	c.record("lojinha")
	c.record("outra")
	c.record("outra")

	n, alarmed := c.record("lojinha")
	if n != 2 || alarmed {
		t.Fatalf("lojinha got n=%d alarmed=%v, want n=2 with no alarm — another instance's rejection cannot count here", n, alarmed)
	}
	if _, alarmed := c.record("lojinha"); !alarmed {
		t.Fatal("lojinha did not alarm on its own third rejection")
	}
}

// docs/ARMADILHAS.md: every HTTP Handler with mutable state needs a
// concurrent test run under -race — without it, -race is theater. The
// rejection counter is new mutable state, touched by requests http.Server
// serves in goroutines over the SAME handler. Run with -race.
func TestHandlerWithstandsConcurrentLargeBodies(t *testing.T) {
	h := activeTestHandlerWithCap(t, "http://127.0.0.1:1", 10)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	const goroutines = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha",
				strings.NewReader(strings.Repeat("x", 64)))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("status = %d, want 413", rec.Code)
			}
		}()
	}
	wg.Wait()

	// 200 rejections in the same window = ONE alarm. A count lost to a race
	// would skip the exact threshold and come out zero; a count counted
	// twice would come out more than one.
	if n := strings.Count(buf.String(), "ALARME"); n != 1 {
		t.Fatalf("ALARME fired %d time(s) across %d concurrent rejections, want exactly 1", n, goroutines)
	}
}

// CRITICAL found in the T11 review, proven with -race.
// http.Server serves every request in its own goroutine over the SAME
// handler. An unsynchronized counter is a data race — and NO sequential
// test reveals it: the whole suite passed green under -race because
// nothing here was concurrent. Run this file with -race.
func TestHandlerWithstandsConcurrentRequests(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h := activeTestHandler(t, consumer.URL)
	raw := testPayload()
	signature := signAsMeta(raw, "app-secret-de-teste")

	const goroutines = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
			req.Header.Set("X-Hub-Signature-256", signature)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
		}()
	}
	wg.Wait()
}

// --- Instance counters (T-035) -----------------------------------------------

// directCount reads the counter directly from the sqlite file, WITHOUT
// going through config.Store (the table stores no secret at all — just
// slug, day, key and n). It's the same pattern already used in this file
// to activate a test instance via a direct UPDATE.
func directCount(t *testing.T, path, slug, key string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to read counter: %v", err)
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`SELECT COALESCE(SUM(n), 0) FROM contador WHERE slug = ? AND chave = ?`,
		slug, key).Scan(&n)
	if err != nil {
		t.Fatalf("read counter %q/%q: %v", slug, key, err)
	}
	return n
}

// alwaysFailingCounter is a config.CounterStore that fails on EVERY call —
// the double that proves, against the REAL handler, that a counting failure
// never changes the status returned to Meta (Verify (c) of T-035).
type alwaysFailingCounter struct{ calls atomic.Int64 }

func (c *alwaysFailingCounter) IncrementCounter(slug, key string, when time.Time) error {
	c.calls.Add(1)
	return errors.New("alwaysFailingCounter: deliberate test failure")
}

// Verify (a): a successful delivery increments `recebidas` and `entregues`.
func TestHandlerCountsReceivedAndDeliveredOnSuccessfulDelivery(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if n := directCount(t, path, "lojinha", config.CounterReceived); n != 1 {
		t.Errorf("recebidas = %d, want 1", n)
	}
	if n := directCount(t, path, "lojinha", config.CounterDelivered); n != 1 {
		t.Errorf("entregues = %d, want 1", n)
	}
	if n := directCount(t, path, "lojinha", config.CounterRefusedByConsumer); n != 0 {
		t.Errorf("recusadas_pelo_consumidor = %d, want 0 (delivery was successful)", n)
	}
	if n := directCount(t, path, "lojinha", config.CounterDefinitiveLossAlarm); n != 0 {
		t.Errorf("alarme_perda_definitiva = %d, want 0", n)
	}
}

// Verify (b): a 4xx from the consumer increments `recusadas_pelo_consumidor`
// AND `alarme_perda_definitiva` — both, because it's the SAME event
// (permanent loss) seen from two angles: who rejected it, and that no one
// has been warned yet.
func TestHandlerCountsRefusedAndAlarmWhenConsumerRefuses(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (Meta cannot redeliver a permanent refusal)", rec.Code)
	}
	if n := directCount(t, path, "lojinha", config.CounterRefusedByConsumer); n != 1 {
		t.Errorf("recusadas_pelo_consumidor = %d, want 1", n)
	}
	if n := directCount(t, path, "lojinha", config.CounterDefinitiveLossAlarm); n != 1 {
		t.Errorf("alarme_perda_definitiva = %d, want 1", n)
	}
	if n := directCount(t, path, "lojinha", config.CounterDelivered); n != 0 {
		t.Errorf("entregues = %d, want 0 (the consumer REFUSED)", n)
	}
}

// Verify (c), AND IT IS T-035's CENTRAL CRITERION: a counter failure does
// NOT change the status returned to Meta. alwaysFailingCounter fails on every
// call; the verdict has to come out IDENTICAL to that of a working counter.
//
// MANDATORY MUTATION (done and reverted during development, not
// committed): moving the call to h.counter.Register to BEFORE
// w.WriteHeader(v.StatusForMeta) — and, critically, swapping the safe call
// for a DIRECT call to h.store.IncrementCounter with error handling that
// RESPONDS (http.Error) instead of just logging — makes this test go red:
// the status becomes the counter's error status, not the real verdict. It
// is exactly the design docs/ARMADILHAS.md's hard rule for T-035 forbids
// ("counting is monitoring, it can never bring down the response already
// written to Meta or to the consumer"), and the reason Counter.Register has no error
// at all to return.
func TestHandlerCounterFailureDoesNotChangeTheStatus(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, _ := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	_ = h // handler with a WORKING counter, only as a reference for the expected status

	// Builds a SECOND handler, over the SAME database, with a counter that
	// ALWAYS fails.
	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "t2.db")
	store, err := config.OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	if err := store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "app-secret-de-teste", VerifyToken: "vt", SendToken: "te",
		CallbackURL: consumer.URL, DeliverySecret: "se", TimeoutMs: 2000,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	activateInstanceInFile(t, path, "lojinha")

	fake := &alwaysFailingCounter{}
	hFailing := NewHandler(store, NewDeliverer(nil), 1<<20, config.NewCounterWithStore(fake), config.NewTransit(store))

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	hFailing.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a COUNTER failure cannot change the DELIVERY verdict", rec.Code)
	}
	if fake.calls.Load() == 0 {
		t.Fatal("the always-failing counter was never called — the test did not exercise the path it proves")
	}
}

// handlerWithAlwaysFailingCounter builds a PRODUCTION handler over its own
// database, but with a config.Counter whose store fails on EVERY write —
// the only way to prove, on the real handler, that the verdict written for
// Meta doesn't depend on the count having succeeded.
func handlerWithAlwaysFailingCounter(t *testing.T, callback string) (http.Handler, *alwaysFailingCounter) {
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
	if err := store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "app-secret-de-teste", VerifyToken: "vt", SendToken: "te",
		CallbackURL: callback, DeliverySecret: "se", TimeoutMs: 2000,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	activateInstanceInFile(t, path, "lojinha")

	fake := &alwaysFailingCounter{}
	return NewHandler(store, NewDeliverer(nil), 1<<20, config.NewCounterWithStore(fake), config.NewTransit(store)), fake
}

// T-047, and this is the test the task's MANDATORY MUTATION attacks:
// counting the phone_number_id rejection cannot change the 200 Meta already
// heard. It's the same T-035 rule, now on branch 5a — the counter runs
// AFTER w.WriteHeader, never before.
//
// MUTATION, done and reverted during development (not committed), in TWO
// stages, because the first stage's result is the information that matters:
//
//	(i)  moving h.counter.Register(slug, config.CounterNumberDiscarded) to
//	     BEFORE w.WriteHeader(http.StatusOK), just that: this test stays
//	     GREEN. It isn't that the test is weak — it's that
//	     Counter.Register RETURNS NOTHING (internal/config/counter.go),
//	     so there is no path by which a counting failure could reach the
//	     response. The defense is the SIGNATURE, not the order;
//	(ii) moving AND swapping the call for a variant that returns an error,
//	     handled with http.Error the way the rest of the project handles
//	     errors (`if err != nil { http.Error(w, …); return }`): this test
//	     goes RED, with a 500 in place of the 200 — Meta would start
//	     redelivering for 36h a webhook that had already been decided,
//	     because of a tracking failure.
//
// In other words: the order alone isn't enough and the signature alone
// isn't obviously enough either — it's the two together that close the
// hole, and (i) is the proof of which of the two is carrying the weight.
func TestHandlerCounterFailureDoesNotChangeStatusOfPhoneNumberIDRefusal(t *testing.T) {
	delivered := false
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, fake := handlerWithAlwaysFailingCounter(t, consumer.URL)

	raw := foreignNumberPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a COUNTER failure cannot change the isolation refusal already written to Meta", rec.Code)
	}
	if delivered {
		t.Fatal("delivered an event whose phone_number_id is not the path instance's")
	}
	if fake.calls.Load() == 0 {
		t.Fatal("the always-failing counter was never called — the test did not exercise the path it proves")
	}
}

// Verify (d): docs/ARMADILHAS.md, "Go / concurrency" — the counter is
// mutable state touched by concurrent goroutines over the SAME handler. Run
// with -race.
func TestHandlerCounterWithstandsConcurrentRequests(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")
	raw := testPayload()
	signature := signAsMeta(raw, "app-secret-de-teste")

	const goroutines = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
			req.Header.Set("X-Hub-Signature-256", signature)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
		}()
	}
	wg.Wait()

	if n := directCount(t, path, "lojinha", config.CounterReceived); n != goroutines {
		t.Fatalf("recebidas = %d, want %d — count lost under concurrency", n, goroutines)
	}
	if n := directCount(t, path, "lojinha", config.CounterDelivered); n != goroutines {
		t.Fatalf("entregues = %d, want %d", n, goroutines)
	}
}

// activateInstanceInFile is the same direct UPDATE activeTestHandler
// does, exposed for the counter tests that need the database's PATH (to
// read the counter afterward) instead of just the http.Handler.
func activateInstanceInFile(t *testing.T, path, slug string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to activate test instance: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE instancia SET ativo = 1 WHERE slug = ?`, slug); err != nil {
		t.Fatalf("activate test instance: %v", err)
	}
}

// clearWabaIDInFile sets an EMPTY waba_id on an instance ALREADY
// WRITTEN to the database.
//
// It exists because the store stopped accepting an instance like that at
// creation time (config.ValidateIdentification, T-074), and even so guard 5b
// needs to be exercised against that state: the validation applies to
// whoever is BORN from here on and doesn't reach a row that's already in
// the database. This UPDATE is the only honest way to produce the state the
// guard defends against — the alternative (relaxing validation "just for
// the test") would erase precisely the guarantee T-074 created.
func clearWabaIDInFile(t *testing.T, path, slug string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to clear waba_id: %v", err)
	}
	defer db.Close()
	res, err := db.Exec(`UPDATE instancia SET waba_id = '' WHERE slug = ?`, slug)
	if err != nil {
		t.Fatalf("clear waba_id: %v", err)
	}
	// An UPDATE that matches no row is NOT an error for SQLite: without this
	// check, a wrong slug would leave the test running against an instance
	// with a filled-in waba_id — exactly the false green T-068 found.
	if rows, err := res.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("clear waba_id: %d row(s) affected (err=%v), want 1", rows, err)
	}
}
