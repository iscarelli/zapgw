// Tests for T-097 (Instagram, first slice) on the OUTBOUND SIDE — the
// task's mandatory Verify item (d), the OUTBOUND half of (e) (the INBOUND
// half is in internal/inbound/instagram_test.go), and an extra proof that
// `zapgw fumaca` also knows how to activate an Instagram instance.
//
// ALL SERVERS ARE LOCAL httptest.NewServer — the SAME technique
// TestHandlerSendsAndReturnsTheID and this package's other WhatsApp tests already
// use (handler_test.go). The gateway NEVER talks to the real Meta in this
// file.
package outbound

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// storeWithInstagramConsumer is storeWithConsumer's counterpart, for
// Instagram (auth_test.go): an INSTAGRAM instance "insta-loja" (ig_id
// "IGID1") and a "sistema-a" consumer authorized on it.
func storeWithInstagramConsumer(t *testing.T) (*config.Store, string) {
	t.Helper()
	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := config.OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.CreateInstance(config.Instance{
		Slug: "insta-loja", Type: config.TypeInstagram, IgID: "IGID1",
		AppSecret: "a", VerifyToken: "v", SendToken: "t-insta-loja",
		CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 2000,
	}); err != nil {
		t.Fatalf("CreateInstance (instagram): %v", err)
	}
	if err := s.CreateConsumer("sistema-a", "token-do-a", []string{"insta-loja"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	return s, path
}

// testInstagramHandler points the Instagram send at `metaSrv` on BOTH
// SIDES (the client's c.base AND the handler's baseInstagram) — enough for
// tests that only prove SHAPE (body, response). The test that proves HOST
// SELECTION (TestHandlerInstagramNuncaChamaCBaseDoCliente, below) uses two
// different servers on purpose.
func testInstagramHandler(t *testing.T, metaSrv *httptest.Server) (http.Handler, *config.Store) {
	t.Helper()
	return testInstagramHandlerWithBases(t, metaSrv, metaSrv)
}

// testInstagramHandlerWithBases is the full version: `cBase` is the server
// behind `c.base` (the Meta client) and `baseInstagram` is the server
// injected as the root of the graph.instagram.com host (T-104) — the two CAN
// be different servers, and that is how TestHandlerInstagramNuncaChamaCBaseDoCliente
// proves which of the two the handler actually calls.
func testInstagramHandlerWithBases(t *testing.T, cBase, baseInstagram *httptest.Server) (http.Handler, *config.Store) {
	t.Helper()
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")

	h := NewHandlerWithInstagramBase(store, NewAuthenticator(store),
		meta.NewClient(cBase.Client(), cBase.URL), 1<<20, config.NewCounter(store), config.NewTransit(store),
		baseInstagram.URL, AllTypes)
	return h, store
}

// Verify item (d): the text send assembles EXACTLY `POST /<IG_ID>/messages`
// with `{"recipient":{"id":...},"message":{"text":...}}` — against the fake
// Graph, never against the real Meta.
func TestHandlerInstagramSendTextBuildsTheExactRequest(t *testing.T) {
	var method, path string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		body, _ = readAllForTest(t, r)
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID_SINTETICO_1","message_id":"IG-TESTE-1"}`))
	}))
	defer srv.Close()
	h, _ := testInstagramHandler(t, srv)

	request := `{"instancia":"insta-loja","para":"IGSID_SINTETICO_1","tipo":"texto","texto":"oi"}`
	rec := ask(t, h, "token-do-a", "k1", request)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if method != http.MethodPost {
		t.Errorf("metodo = %q, quero POST", method)
	}
	if path != "/IGID1/messages" {
		t.Errorf("caminho = %q, quero /IGID1/messages", path)
	}
	var sent struct {
		Recipient struct {
			ID string `json:"id"`
		} `json:"recipient"`
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("corpo enviado a Meta nao e JSON: %s", body)
	}
	if sent.Recipient.ID != "IGSID_SINTETICO_1" {
		t.Errorf("recipient.id = %q, quero IGSID_SINTETICO_1", sent.Recipient.ID)
	}
	if sent.Message.Text != "oi" {
		t.Errorf("message.text = %q, quero \"oi\"", sent.Message.Text)
	}
	// The body must have NOTHING of WhatsApp (messaging_product, recipient_type,
	// type, to) — that would prove SendInstagramMessage assembles the WRONG shape.
	for _, whatsappField := range []string{"messaging_product", "recipient_type", `"to"`, `"type"`} {
		if strings.Contains(string(body), whatsappField) {
			t.Errorf("corpo enviado ao Instagram traz campo de WHATSAPP %q: %s", whatsappField, body)
		}
	}

	var resp struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.WaMessageID != "IG-TESTE-1" {
		t.Errorf("wa_message_id = %q, quero IG-TESTE-1 (o message_id que a Meta devolveu)", resp.WaMessageID)
	}
}

// (e), OUTBOUND half: the transit log records the counterpart as the IGSID —
// INTACT, never passed through meta.Canonicalize (which exists for a Brazilian
// phone number and would corrupt an IGSID that happened to have the SHAPE of
// a 12-digit number starting with "55").
//
// "551987654321" (12 digits, prefix "55", 5th digit '8' in [6-9]) is
// EXACTLY the format meta.Canonicalize would mutate by inserting a "9th digit" —
// if this test used p.To (canonicalized) instead of rawTo for the
// counterpart, it would catch the mutation. See the comment on Request in
// mensagem.go and on rawTo in handler.go.
func TestHandlerInstagramTransitWritesTheIGSIDIntactEvenWhenPhoneShaped(t *testing.T) {
	igsidShapedLikePhone := "551987654321"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"recipient_id":"` + igsidShapedLikePhone + `","message_id":"IG-TESTE-2"}`))
	}))
	defer srv.Close()
	h, store := testInstagramHandler(t, srv)

	request := `{"instancia":"insta-loja","para":"` + igsidShapedLikePhone + `","tipo":"texto","texto":"oi"}`
	rec := ask(t, h, "token-do-a", "k1", request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	var counterpart string
	if err := store.DB().QueryRow(
		`SELECT contraparte FROM transito WHERE slug = 'insta-loja' ORDER BY carimbo DESC LIMIT 1`).
		Scan(&counterpart); err != nil {
		t.Fatalf("ler linha de transito: %v", err)
	}
	if counterpart != igsidShapedLikePhone {
		t.Errorf("transito.contraparte = %q, quero %q INTACTO (Canonicalize teria inserido um digito)",
			counterpart, igsidShapedLikePhone)
	}
}

// Narrow NON-REGRESSION: a WHATSAPP instance still uses SendMessage (the
// MetaBody path), never SendInstagramMessage — TestHandlerSendsAndReturnsTheID
// (handler_test.go) already proves that by the response format
// (wa_message_id comes from messages[0].id); this test checks the REQUEST
// SIDE, which that one does not look at.
func TestHandlerWhatsAppKeepsBuildingTheWhatsAppBody(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = readAllForTest(t, r)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.OK"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "k1", textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(string(body), `"messaging_product":"whatsapp"`) {
		t.Errorf("corpo enviado a Meta nao tem messaging_product=whatsapp: %s", body)
	}
	if strings.Contains(string(body), `"recipient":`) {
		t.Errorf("corpo enviado a Meta tem `recipient`, forma de INSTAGRAM: %s", body)
	}
}

func readAllForTest(t *testing.T, r *http.Request) ([]byte, error) {
	t.Helper()
	return io.ReadAll(r.Body)
}

// --- T-104: the destination host the code chooses, PER INSTANCE TYPE --
//
// WHY THIS TEST EXISTS: TestHandlerInstagramSendTextBuildsTheExactRequest
// (above) and every Instagram test in this file, UNTIL T-104, pointed
// `c.base` and Instagram's base at the SAME httptest.Server — exactly the
// "honest limitation" T-097 recorded and that T-104 collected on: the
// production defect (SendInstagramMessage building the URL over `c.base`,
// WhatsApp's host) would NEVER show up against a server that answers both
// hosts the same way. This test uses TWO DIFFERENT servers — one ONLY for
// `c.base` (WhatsApp), another ONLY for `baseInstagram` — and proves, by
// CONSEQUENCE (the wrong server would have answered differently, or would not
// have been called), which of the two each instance TYPE actually uses.
func TestHandlerPicksTheHostByInstanceType(t *testing.T) {
	var clientBaseCalled, instagramBaseCalled bool

	srvClient := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientBaseCalled = true
		// Answers in WhatsApp's FORMAT — if the Instagram send accidentally
		// stopped here, reading the response (WRONG format) would also flag
		// it, but the assertion that matters is the count below.
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.NAO-DEVERIA-SER-USADO"}]}`))
	}))
	defer srvClient.Close()

	srvInstagram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instagramBaseCalled = true
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID_SINTETICO_1","message_id":"IG-HOST-CERTO"}`))
	}))
	defer srvInstagram.Close()

	h, _ := testInstagramHandlerWithBases(t, srvClient, srvInstagram)

	request := `{"instancia":"insta-loja","para":"IGSID_SINTETICO_1","tipo":"texto","texto":"oi"}`
	rec := ask(t, h, "token-do-a", "k1", request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	if clientBaseCalled {
		t.Error("uma instancia INSTAGRAM chamou o servidor de c.base (o host do WhatsApp) — " +
			"e exatamente o defeito que a T-104 corrigiu")
	}
	if !instagramBaseCalled {
		t.Error("uma instancia INSTAGRAM NUNCA chamou o servidor de baseInstagram (graph.instagram.com)")
	}

	var resp struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.WaMessageID != "IG-HOST-CERTO" {
		t.Errorf("wa_message_id = %q, quero IG-HOST-CERTO (o id que o servidor CERTO devolveu)", resp.WaMessageID)
	}
}

// The COUNTERPART of the test above, on WhatsApp's side: a WhatsApp instance
// still goes to `c.base`, EVEN WHEN `baseInstagram` points at another server
// — non-regression required by T-104's Do ("a WhatsApp instance cannot
// change at all").
func TestHandlerWhatsAppKeepsUsingCBaseEvenWithADifferentInstagramBase(t *testing.T) {
	var clientBaseCalled, instagramBaseCalled bool

	srvClient := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientBaseCalled = true
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.OK"}]}`))
	}))
	defer srvClient.Close()

	srvInstagram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instagramBaseCalled = true
		_, _ = w.Write([]byte(`{"recipient_id":"X","message_id":"NAO-DEVERIA-SER-USADO"}`))
	}))
	defer srvInstagram.Close()

	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewHandlerWithInstagramBase(store, NewAuthenticator(store),
		meta.NewClient(srvClient.Client(), srvClient.URL), 1<<20, config.NewCounter(store), config.NewTransit(store),
		srvInstagram.URL, AllTypes)

	rec := ask(t, h, "token-do-a", "k1", textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if !clientBaseCalled {
		t.Error("uma instancia WHATSAPP NUNCA chamou o servidor de c.base")
	}
	if instagramBaseCalled {
		t.Error("uma instancia WHATSAPP chamou o servidor de baseInstagram — nao deveria nem saber que ele existe")
	}
}

// --- T-097's Do item 6: the smoke test also knows how to activate Instagram --
//
// IT IS NOT in T-097's `Files:` list, and the decision to extend it is
// recorded in that task's final report: without this, an Instagram instance
// would NEVER leave PAUSED (config.CreateInstance always forces ativo=0, and
// config.ActivateInstance is only called by outbound.SmokeWithInstagramBase —
// see both headers). Reuses fumaca_handler_test.go/smokeGraph (the SAME
// two-route fake the WhatsApp smoke test already uses), just with postBody
// in Instagram's format.
//
// 🔴 T-104: g.gets.Load() NOW has to be ZERO. Until T-104 this test required
// step 2 (GET, CheckCredential) to hit the Graph API for Instagram — and
// that requirement would NEVER have caught the real defect (the call was
// going to the wrong host AND STILL counted as "hit", because g.srv answered
// both methods). This task's decision was to SKIP step 2 for Instagram (see
// the comment in fumaca.go) instead of forcing a call with no measured
// source — this test now proves the opposite of what it proved before, on
// purpose.
func TestSmokeRouteActivatesInstagramInstanceOnlyAfterSendingAMessage(t *testing.T) {
	g := workingSmokeGraph(t)
	g.postBody = `{"recipient_id":"IGSID_SINTETICO_1","message_id":"IG-FUMACA-1"}`
	store, _ := storeWithInstagramConsumer(t)
	h := NewSmokeHandlerWithInstagramBase(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), config.NewCounter(store), 1<<20, g.srv.URL, AllTypes)

	rec := askSmoke(t, h, "token-do-a", smokeBody("insta-loja", "IGSID_SINTETICO_1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if g.gets.Load() != 0 {
		t.Errorf("gets = %d, quero 0 — o passo 2 (CheckCredential) e PULADO para Instagram (T-104)", g.gets.Load())
	}
	if g.posts.Load() != 1 {
		t.Errorf("mensagens enviadas = %d, quero 1", g.posts.Load())
	}

	inst, err := store.FindInstance("insta-loja")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if !inst.Active {
		t.Error("instancia Instagram continua PAUSADA depois de um envio de teste aceito")
	}
}
