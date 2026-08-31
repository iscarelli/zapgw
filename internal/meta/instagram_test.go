// Tests for SendInstagramMessage (T-104) — the network call for sending
// an Instagram DM, ISOLATED from the instance and from the handler.
// Complements instagram_renewal_test.go (RenewInstagramToken) and
// client_test.go (SendMessage, WhatsApp), which already prove the same
// discipline for the sibling functions.
//
// WHY THIS FILE EXISTS: until T-104, no test in this package exercised the
// HOST SendInstagramMessage builds — client_test.go uses
// testClient(srv), which points `c.base` at the httptest.Server itself,
// and until T-104 SendInstagramMessage built the URL over `c.base`. A
// test like that would NEVER have caught the production defect (the real
// call went to graph.facebook.com/vNN instead of graph.instagram.com): the
// test's `c.base` and the real `base` coincided by construction. The tests
// below use TWO DIFFERENT servers — one at `c.base` that must NEVER be
// called, another passed as `base` (the parameter) that MUST receive the
// call — so that a regression to `c.base` turns RED instead of staying
// green by coincidence.
package meta

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSendInstagramMessageUsesTheParametersBaseNeverCBase is the test that
// proves T-104's fix: `c.base` points at a dead address (nothing listens
// there) and `base` (the parameter) points at the fake server. If
// SendInstagramMessage regressed to using `c.base`, this call would fail
// by transport (dead address) instead of returning success — the test
// distinguishes the two hosts by CONSEQUENCE, not just by inspecting the
// URL.
func TestSendInstagramMessageUsesTheParametersBaseNeverCBase(t *testing.T) {
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID1","message_id":"IG-TESTE-1"}`))
	}))
	defer srv.Close()

	// `c.base` is NOT the fake server: it's an address that listens
	// nowhere (loopback port 1, the SAME technique as
	// TestRenewInstagramTokenTransportFailure). If the call used
	// `c.base`, it would die by transport right here.
	c := NewClient(http.DefaultClient, "http://127.0.0.1:1")

	resp, err := c.SendInstagramMessage(context.Background(), srv.URL, "IGID1", "token", "IGSID1", "oi")
	if err != nil {
		t.Fatalf("SendInstagramMessage: %v (c.base=%q base=%q) — a chamada foi para o host ERRADO", err, "http://127.0.0.1:1", srv.URL)
	}
	if resp.ID != "IG-TESTE-1" {
		t.Errorf("id = %q, quero IG-TESTE-1", resp.ID)
	}
	if requestedPath != "/IGID1/messages" {
		t.Errorf("caminho = %q, quero /IGID1/messages", requestedPath)
	}
}

// NARROW NON-REGRESSION on the same proof: the call has to go to `base`
// even when `c.base` IS a server that RESPONDS (and would respond with
// success, if called) — without this, a regression to `c.base` would only
// show up against a dead address, and a `c.base` "that works" but points to
// the WRONG Meta host (facebook instead of instagram) would go unnoticed.
func TestSendInstagramMessageNeverCallsTheServerOfCBase(t *testing.T) {
	var baseCalled, calledParam bool

	srvBase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseCalled = true
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID1","message_id":"NUNCA-DEVERIA-APARECER"}`))
	}))
	defer srvBase.Close()

	srvParam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledParam = true
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID1","message_id":"IG-TESTE-2"}`))
	}))
	defer srvParam.Close()

	c := NewClient(srvBase.Client(), srvBase.URL)
	resp, err := c.SendInstagramMessage(context.Background(), srvParam.URL, "IGID1", "token", "IGSID1", "oi")
	if err != nil {
		t.Fatalf("SendInstagramMessage: %v", err)
	}
	if baseCalled {
		t.Error("o servidor de c.base foi chamado — SendInstagramMessage regrediu para c.base")
	}
	if !calledParam {
		t.Error("o servidor de `base` (o parametro) NUNCA foi chamado")
	}
	if resp.ID != "IG-TESTE-2" {
		t.Errorf("id = %q, quero IG-TESTE-2 (o que o servidor de `base` devolveu)", resp.ID)
	}
}

// (d) of T-097's Verify, rechecked here at the meta.Client level (the same
// shape is already proven in internal/outbound/instagram_test.go, at the
// Handler level — this test covers the isolated function, without the
// handler's indirection).
func TestSendInstagramMessageBuildsTheExactRequest(t *testing.T) {
	var method, authorization string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		authorization = r.Header.Get("Authorization")
		body, _ = readBodyForTest(t, r)
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID1","message_id":"IG-TESTE-3"}`))
	}))
	defer srv.Close()

	if _, err := testClient(srv).SendInstagramMessage(
		context.Background(), srv.URL, "IGID1", "token-secreto", "IGSID1", "oi, tudo bem?"); err != nil {
		t.Fatalf("SendInstagramMessage: %v", err)
	}

	if method != http.MethodPost {
		t.Errorf("metodo = %q, quero POST", method)
	}
	if authorization != "Bearer token-secreto" {
		t.Errorf("Authorization = %q", authorization)
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
		t.Fatalf("corpo enviado nao e JSON: %s", body)
	}
	if sent.Recipient.ID != "IGSID1" || sent.Message.Text != "oi, tudo bem?" {
		t.Errorf("corpo = %s", body)
	}
	if strings.Contains(string(body), "messaging_product") {
		t.Errorf("corpo tem campo de WHATSAPP: %s", body)
	}
}

// THE SAME guard as SendMessage (TestSendMessageRefusesAPhoneNumberIDThatWouldEscape):
// url.JoinPath resolves `..`, so a dirty ig_id would escape the intended
// host. The check happens BEFORE building the URL — the value of `base`
// doesn't matter.
func TestSendInstagramMessageRefusesAnIgIDThatWouldEscape(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID1","message_id":"X"}`))
	}))
	defer srv.Close()

	for _, dirty := range []string{"../outro", "a/b", "", "IGID?x=1"} {
		_, err := testClient(srv).SendInstagramMessage(
			context.Background(), srv.URL, dirty, "token", "IGSID1", "oi")
		if err == nil {
			t.Errorf("ig_id %q: aceito sem erro", dirty)
		}
	}
	if called {
		t.Fatal("o cliente CHAMOU a rede com um ig_id invalido")
	}
}

// T-115 (1): THE SAME trap as the WhatsApp sibling
// (TestSendMessageRefuses200WithoutAnID, client_test.go) — a Meta `200` does
// NOT prove a message_id came with it. Until this task no test in this
// package reached instagramSendResponse's error branch: every fake
// Instagram server used by the tests above returned a filled-in
// message_id. `go tool cover` confirmed 0% on this branch before this
// test.
func TestSendInstagramMessageRefuses200WithoutAnID(t *testing.T) {
	cases := []string{
		`{"recipient_id":"IGSID1"}`,
		`{}`,
		`{"recipient_id":"IGSID1","message_id":""}`,
		`{"recipient_id":"IGSID1","message_id":"   "}`,
		`{"recipient_id":"IGSID1","message_id":123}`,
		`null`,
		``,
		`nao e json`,
	}

	for _, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))

		resp, err := testClient(srv).SendInstagramMessage(
			context.Background(), srv.URL, "IGID1", "token", "IGSID1", "oi")
		srv.Close()

		if err == nil {
			t.Errorf("corpo %q devolveu SUCESSO com id %q", body, resp.ID)
			continue
		}
		if !errors.Is(err, ErrResponseWithoutMessageID) {
			t.Errorf("corpo %q: erro = %v, quero ErrResponseWithoutMessageID", body, err)
		}
		if resp.ID != "" {
			t.Errorf("corpo %q devolveu id %q junto com erro", body, resp.ID)
		}
	}
}

func readBodyForTest(t *testing.T, r *http.Request) ([]byte, error) {
	t.Helper()
	return io.ReadAll(r.Body)
}

// --- T-105: ParseInstagramWebhook and `is_echo` -------------------------------
//
// The message the business itself sends is ECHOED back by Meta in the SAME
// `messaging[]` array that carries customer messages — see the Source in
// IsEcho's comment (instagramMessageBodyMeta, instagram.go). The tests
// below are the task's Verify's three. SYNTHETIC IDs (CLAUDE.md's hard rule
// about real third-party data in the repository).

// instagramPayloadWithItems builds an Instagram payload with N ready
// `messaging[]` items (each `item` is already the object's full JSON).
func instagramPayloadWithItems(igID string, items ...string) []byte {
	return []byte(`{"object":"instagram","entry":[{"id":"` + igID + `","time":1769000000,` +
		`"messaging":[` + strings.Join(items, ",") + `]}]}`)
}

func testCustomerMessageItem(customerIGSID, igID, mid, text string) string {
	return `{"sender":{"id":"` + customerIGSID + `"},"recipient":{"id":"` + igID + `"},` +
		`"timestamp":1769000000123,"message":{"mid":"` + mid + `","text":"` + text + `"}}`
}

func testEchoItem(igID, customerIGSID, mid, text string) string {
	// Echo: the SENDER is the business itself (igID) and the RECIPIENT is
	// the customer — the INVERSE of a received message. It's exactly this
	// inversion that made the consumer read "a customer message arrived"
	// when it was actually the business itself (see this section's top
	// and the Source in instagram.go).
	return `{"sender":{"id":"` + igID + `"},"recipient":{"id":"` + customerIGSID + `"},` +
		`"timestamp":1769000000456,"message":{"mid":"` + mid + `","text":"` + text + `","is_echo":true}}`
}

// TestIsEchoAgreesWithSenderEqualToTheEntryID: DOUBLE-CHECK proof, requested
// after this task was already implemented. Consumer consumer-b reached the
// SAME echo diagnosis by looking at THEIR OWN data, in the same minute, and
// put up their OWN guard in production — but through a path different from
// ours: they read from the `cru` (not the normalized event, which does NOT
// carry the recipient) and use `sender.id == entry.id` ("the account
// talking to itself"), fail-closed (unreadable cru or no entry.id counts as
// echo — their reasoning: "erring toward ignoring costs one DM; the other
// way costs a flood on a customer's phone"). They will KEEP that guard even
// after this fix, as a seatbelt.
//
// This gateway uses `is_echo` (the marker Meta DOCUMENTS — see the Source
// in IsEcho's comment, instagram.go) as the PRODUCTION CRITERION, and that
// remains the right choice: it's the field Meta itself designed for this.
// This test does NOT swap the criterion — it only PROVES that, on the SAME
// realistic payload, the two criteria agree. If they ever diverge (Meta
// changes the format, for example), it's better to find out here, through a
// RED test, than through a customer complaining again.
func TestIsEchoAgreesWithSenderEqualToTheEntryID(t *testing.T) {
	const igID = "IGID_NEGOCIO_SINTETICO"
	const customerIGSID = "IGSID_CLIENTE_SINTETICO"

	cases := []struct {
		name     string
		item     string
		wantEcho bool
	}{
		{"mensagem de cliente", testCustomerMessageItem(customerIGSID, igID, "IGMID.CLI1", "oi"), false},
		{"eco", testEchoItem(igID, customerIGSID, "IGMID.ECO1", "resposta"), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload := instagramPayloadWithItems(igID, c.item)

			// Reads the SAME cru through the TWO paths, with the SAME
			// types the production parser uses (instagram.go) — so this
			// test exercises exactly what Meta sends, with no third way
			// of interpreting the payload.
			var env instagramEnvelopeMeta
			if err := json.Unmarshal(payload, &env); err != nil {
				t.Fatalf("envelope: %v", err)
			}
			entries, _ := messageBlock[[]json.RawMessage](env.Entry)
			if len(entries) != 1 {
				t.Fatalf("entradas = %d, quero 1", len(entries))
			}
			var ent instagramEntryMeta
			if err := json.Unmarshal(entries[0], &ent); err != nil {
				t.Fatalf("entry: %v", err)
			}
			entryID, _ := messageBlock[string](ent.ID)

			items, _ := messageBlock[[]json.RawMessage](ent.Messaging)
			if len(items) != 1 {
				t.Fatalf("itens de messaging = %d, quero 1", len(items))
			}
			var m messagingItemMeta
			if err := json.Unmarshal(items[0], &m); err != nil {
				t.Fatalf("item de messaging: %v", err)
			}
			msg, msgState := messageBlock[instagramMessageBodyMeta](m.Message)
			if msgState != blockRead {
				t.Fatalf("message: estado = %v", msgState)
			}
			sender, _ := messageBlock[instagramParticipantMeta](m.Sender)

			// Criterion A (the PRODUCTION one): is_echo.
			echoByIsEcho := msg.IsEcho
			// Criterion B (the consumer's guard): sender.id == entry.id.
			echoBySenderEqualsEntry := sender.ID == entryID

			if echoByIsEcho != c.wantEcho {
				t.Fatalf("is_echo = %v, quero %v", echoByIsEcho, c.wantEcho)
			}
			if echoBySenderEqualsEntry != c.wantEcho {
				t.Fatalf("sender.id==entry.id = %v, quero %v", echoBySenderEqualsEntry, c.wantEcho)
			}
			if echoByIsEcho != echoBySenderEqualsEntry {
				t.Errorf("os dois criterios DIVERGIRAM neste payload: is_echo=%v, sender.id==entry.id=%v — "+
					"isso e' sinal de que a Meta mudou algo no formato; ver o comentario desta funcao",
					echoByIsEcho, echoBySenderEqualsEntry)
			}
		})
	}
}

// (a) a batch with ONLY an echo -> empty events.
//
// T-106 CHANGED this test's assertion: until T-106, the echo was counted
// as an ignored item and err carried ErrPartialParse. That was exactly the
// defect measured in production (docs/TASKS.md, T-106) — starting from the
// echo fix (T-105), EVERY reply the business sends generates an echo, and
// each one became a "parse failed" line saying "part of the payload can't
// be read" about an item read and recognized on purpose. Now the echo is
// SILENT (itemEcho, instagram.go): it doesn't count anywhere.
func TestParseInstagramWebhookAnEchoDoesNotBecomeAnEvent(t *testing.T) {
	payload := instagramPayloadWithItems("IGID_NEGOCIO_SINTETICO",
		testEchoItem("IGID_NEGOCIO_SINTETICO", "IGSID_CLIENTE_SINTETICO", "IGMID.ECO1", "de volta pra voce"))

	evs, err := ParseInstagramWebhook(payload)

	if len(evs) != 0 {
		t.Fatalf("eventos = %d, quero 0 (eco nao pode virar evento modelado): %+v", len(evs), evs)
	}
	if err != nil {
		t.Errorf("err = %v, quero nil (eco e SILENCIOSO — T-106)", err)
	}
}

// (b) a batch with a CUSTOMER message and an echo -> only the customer's
// becomes an event. It's this test that proves the RIGHT item was
// filtered, not the whole batch — exactly the sequence measured in
// production (T-105): the customer's message first, the echo of the
// business's reply right after, in the SAME minute.
//
// T-106 CHANGED err's assertion — see the test above's comment.
func TestParseInstagramWebhookFiltersTheEchoAndKeepsTheCustomersMessage(t *testing.T) {
	payload := instagramPayloadWithItems("IGID_NEGOCIO_SINTETICO",
		testCustomerMessageItem("IGSID_CLIENTE_SINTETICO", "IGID_NEGOCIO_SINTETICO", "IGMID.CLIENTE1", "oi, preciso de ajuda"),
		testEchoItem("IGID_NEGOCIO_SINTETICO", "IGSID_CLIENTE_SINTETICO", "IGMID.ECO1", "claro, um momento"),
	)

	evs, err := ParseInstagramWebhook(payload)

	if len(evs) != 1 {
		t.Fatalf("eventos = %d, quero 1 (so a mensagem do cliente): %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Type != EventTypeMessage {
		t.Errorf("Type = %q, quero %q", ev.Type, EventTypeMessage)
	}
	if ev.WaMessageID != "IGMID.CLIENTE1" {
		t.Errorf("WaMessageID = %q, quero IGMID.CLIENTE1 (o eco IGMID.ECO1 nao pode aparecer)", ev.WaMessageID)
	}
	if ev.FromCanonical != "IGSID_CLIENTE_SINTETICO" {
		t.Errorf("FromCanonical = %q, quero o IGSID do CLIENTE, nunca o IGID do negocio", ev.FromCanonical)
	}
	if ev.Text != "oi, preciso de ajuda" {
		t.Errorf("Text = %q", ev.Text)
	}
	// The echo is SILENT (T-106): the customer message parsed
	// successfully and the echo doesn't count anywhere, so err has to be
	// nil.
	if err != nil {
		t.Errorf("err = %v, quero nil (eco filtrado e SILENCIOSO — T-106)", err)
	}
}

// --- T-106: separating UNREADABLE from NOT-MODELED from ECHO -------------------------------
//
// The four tests below are (a)-(d) of T-106's Verify. SYNTHETIC IDs
// (CLAUDE.md's hard rule).

// (a) of T-106's Verify (repeated here under the name the task requires): a
// batch with ONLY an echo -> err == nil and empty evs. Same test as
// TestParseInstagramWebhookAnEchoDoesNotBecomeAnEvent, above; kept as a separate
// function because it's the PRODUCTION case T-106 measured (two batches
// like this per reply sent), and it's worth having its own name in the
// test report.
func TestParseInstagramWebhookABatchOfEchoesOnlyGivesNilErrAndNoEvents(t *testing.T) {
	payload := instagramPayloadWithItems("IGID_NEGOCIO_SINTETICO",
		testEchoItem("IGID_NEGOCIO_SINTETICO", "IGSID_CLIENTE_SINTETICO", "IGMID.ECO2", "de volta pra voce"))

	evs, err := ParseInstagramWebhook(payload)

	if err != nil {
		t.Errorf("err = %v, quero nil", err)
	}
	if len(evs) != 0 {
		t.Errorf("eventos = %d, quero 0", len(evs))
	}
}

// testItemWithoutMessage builds a `messaging[]` item WITHOUT any `message`
// block at all — the real shape of a read/delivery receipt, postback, or
// story reaction (Meta sends all of these in the SAME `messaging` array,
// only swapping what accompanies `sender`/`recipient`/`timestamp`; this
// test doesn't need to replicate each one's real field, just prove that
// `message`'s ABSENCE falls into the right category).
func testItemWithoutMessage(customerIGSID, igID string) string {
	return `{"sender":{"id":"` + customerIGSID + `"},"recipient":{"id":"` + igID + `"},"timestamp":1769000000789}`
}

// (b) of T-106's Verify: a batch with a messaging[] item WITHOUT `message`
// (a read receipt) -> does NOT produce the "can't be read" error
// (ErrPartialParse); if it produces a signal, it's category 2's
// (ErrUnmodeledItems).
func TestParseInstagramWebhookAnItemWithoutMessageDoesNotLieThatItCouldNotBeRead(t *testing.T) {
	payload := instagramPayloadWithItems("IGID_NEGOCIO_SINTETICO",
		testItemWithoutMessage("IGSID_CLIENTE_SINTETICO", "IGID_NEGOCIO_SINTETICO"))

	evs, err := ParseInstagramWebhook(payload)

	if len(evs) != 0 {
		t.Fatalf("eventos = %d, quero 0: %+v", len(evs), evs)
	}
	if errors.Is(err, ErrPartialParse) {
		t.Errorf("err = %v — um item SEM message (recibo/postback/reacao) foi LIDO, "+
			"nunca pode virar ErrPartialParse ('nao pode ser lida')", err)
	}
	if err != nil && !errors.Is(err, ErrUnmodeledItems) {
		t.Errorf("err = %v, se produzir sinal tem de ser ErrUnmodeledItems", err)
	}
}

// (c) of T-106's Verify: a batch with genuinely unreadable JSON -> stays
// ErrPartialParse with TODAY's TEXT, compared AGAINST THE CONSTANT (so the
// text doesn't drift without anyone noticing).
func TestParseInstagramWebhookAnUnreadableItemIsStillErrPartialParse(t *testing.T) {
	// message comes as a STRING, not an object — messageBlock[T]
	// returns blockUnreadable for a type that doesn't match (the same
	// technique as parse_test.go for WhatsApp).
	payload := []byte(`{"object":"instagram","entry":[{"id":"IGID_NEGOCIO_SINTETICO","time":1769000000,` +
		`"messaging":[{"sender":{"id":"IGSID_CLIENTE_SINTETICO"},"recipient":{"id":"IGID_NEGOCIO_SINTETICO"},` +
		`"timestamp":1769000000999,"message":"isto deveria ser um objeto"}]}]}`)

	evs, err := ParseInstagramWebhook(payload)

	if len(evs) != 0 {
		t.Fatalf("eventos = %d, quero 0: %+v", len(evs), evs)
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Fatalf("err = %v, quero ErrPartialParse (message ilegivel de verdade)", err)
	}
	if !strings.Contains(err.Error(), ErrPartialParse.Error()) {
		t.Errorf("err = %v, o TEXTO de ErrPartialParse mudou sem ninguem notar (constante: %q)", err, ErrPartialParse.Error())
	}
	if errors.Is(err, ErrUnmodeledItems) {
		t.Errorf("err = %v, um item ilegivel nao pode contar como ErrUnmodeledItems tambem", err)
	}
}

// (d) of T-106's Verify: a MIXED batch (one good message + one echo + one
// unreadable) -> the good event comes out in the batch AND the unreadable
// one is still reported. This is the proof that the separation (and the
// silent echo) didn't swallow the real signal.
func TestParseInstagramWebhookAMixedBatchKeepsTheGoodAndReportsTheUnreadable(t *testing.T) {
	rawUnreadableItem := `{"sender":{"id":"IGSID_CLIENTE_SINTETICO"},"recipient":{"id":"IGID_NEGOCIO_SINTETICO"},` +
		`"timestamp":1769000001111,"message":"isto deveria ser um objeto"}`
	payload := instagramPayloadWithItems("IGID_NEGOCIO_SINTETICO",
		testCustomerMessageItem("IGSID_CLIENTE_SINTETICO", "IGID_NEGOCIO_SINTETICO", "IGMID.BOA1", "mensagem de verdade"),
		testEchoItem("IGID_NEGOCIO_SINTETICO", "IGSID_CLIENTE_SINTETICO", "IGMID.ECO3", "resposta"),
		rawUnreadableItem,
	)

	evs, err := ParseInstagramWebhook(payload)

	if len(evs) != 1 {
		t.Fatalf("eventos = %d, quero 1 (so a mensagem boa; eco e ilegivel nao viram Event): %+v", len(evs), evs)
	}
	if evs[0].WaMessageID != "IGMID.BOA1" {
		t.Errorf("WaMessageID = %q, quero IGMID.BOA1", evs[0].WaMessageID)
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Fatalf("err = %v, quero um erro que envolve ErrPartialParse (o item ilegivel)", err)
	}
	if errors.Is(err, ErrUnmodeledItems) {
		t.Errorf("err = %v, este lote nao tem item nao-modelado, so ilegivel — o eco e silencioso", err)
	}
}

// MANDATORY MUTATION of T-106's Verify (done and reverted before the
// commit): merging the UNREADABLE and NOT-MODELED categories back into a
// single counter (swapping `unmodeled++` for `unreadable++` in
// instagramParseErrors/ParseInstagramWebhook) left
// TestParseInstagramWebhookAnItemWithoutMessageDoesNotLieThatItCouldNotBeRead RED —
// the item without `message` started producing ErrPartialParse, which the
// test explicitly rejects. Reverted before the commit; there is no
// automated "mutation" test in this repository (the same practice as the
// rest of the package — see parse_test.go).

// (c) NON-REGRESSION: a normal message (without `is_echo`, the case that
// already had a test in internal/inbound/instagram_test.go via the
// handler) still parses exactly as before this task when called in
// isolation here at the parser level. Absent is_echo = zero value bool
// (false) — the same data every production payload before T-105 always
// had.
func TestParseInstagramWebhookAMessageWithoutIsEchoBehavesAsBefore(t *testing.T) {
	payload := instagramPayloadWithItems("IGID_NEGOCIO_SINTETICO",
		testCustomerMessageItem("IGSID_CLIENTE_SINTETICO", "IGID_NEGOCIO_SINTETICO", "IGMID.NORMAL1", "mensagem normal"))

	evs, err := ParseInstagramWebhook(payload)

	if err != nil {
		t.Fatalf("err = %v, quero nil (nenhum item ignorado)", err)
	}
	if len(evs) != 1 {
		t.Fatalf("eventos = %d, quero 1: %+v", len(evs), evs)
	}
	if evs[0].WaMessageID != "IGMID.NORMAL1" || evs[0].Text != "mensagem normal" {
		t.Errorf("evento = %+v", evs[0])
	}
}
