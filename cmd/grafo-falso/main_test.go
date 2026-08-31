// What these tests guard: that the fake speaks the language the PRODUCTION code
// understands.
//
// They do not exercise the gateway — that is cmd/zapgw/fumaca_test.go, and in
// particular TestSmokeWithSendFailureLEAVESTheInstancePAUSED, which is what
// proves the proof requirement survives the lab. Here the question is another
// one, and narrower: does the real client (internal/meta) accept the success and
// classify the refusal?
//
// Why that is worth a test: a fake that answers crooked would only show up at lab
// time, with whoever is operating in the middle, and the symptom ("the smoke test
// failed") points at the gateway — not at the toy.
package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iscarelli/zapgw/internal/meta"
)

func fakeServer(t *testing.T, g *fakeGraph) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(g.routes())
	t.Cleanup(s.Close)
	return s
}

func testBody() map[string]any {
	return map[string]any{
		"messaging_product": "whatsapp",
		"to":                "5511999990000",
		"type":              "text",
		"text":              map[string]any{"body": "laboratorio"},
	}
}

func TestFakeGraphSpeaksTheLanguageOfThePRODUCTIONClient(t *testing.T) {
	s := fakeServer(t, &fakeGraph{})
	c := meta.NewClient(s.Client(), s.URL)

	if err := c.CheckCredential(context.Background(), "PNID1", "token"); err != nil {
		t.Fatalf("CheckCredential: %v — o passo 2 do fumaca abortaria no laboratorio", err)
	}

	resp, err := c.SendMessage(context.Background(), "PNID1", "token", testBody())
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.ID == "" {
		// meta.ErrResponseWithoutID would have come out above; this line covers the day
		// that guarantee moves elsewhere. An empty id would activate the instance
		// over a channel that never proved it delivers.
		t.Fatal("a resposta veio sem wa_message_id")
	}

	second, err := c.SendMessage(context.Background(), "PNID1", "token", testBody())
	if err != nil {
		t.Fatalf("SendMessage (segunda): %v", err)
	}
	if second.ID == resp.ID {
		t.Errorf("os dois envios devolveram o mesmo id (%q) — o falso nao distingue mensagens", resp.ID)
	}
}

// SEND and READ RECEIPT share the same verb and the same path on the Graph API
// (`POST /{phone_number_id}/messages`); only the body separates them. A fake that
// did not tell them apart would return `messages[].id` to a read receipt — and
// the lab would hide precisely the difference T-075 exists to implement.
func TestFakeGraphTellsReadReceiptApartFromSend(t *testing.T) {
	g := &fakeGraph{}
	s := fakeServer(t, g)
	c := meta.NewClient(s.Client(), s.URL)

	if err := c.MarkAsRead(context.Background(), "PNID1", "token", "wamid.LABORATORIO", false); err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}
	if n := g.reads.Load(); n != 1 {
		t.Errorf("leituras = %d, quero 1", n)
	}
	if n := g.sent.Load(); n != 0 {
		t.Errorf("enviadas = %d, quero 0 — marcar como lida NAO e envio", n)
	}

	// And the send goes on being a send, on the same path.
	if _, err := c.SendMessage(context.Background(), "PNID1", "token", testBody()); err != nil {
		t.Fatalf("SendMessage depois da marcacao: %v", err)
	}
	if n := g.sent.Load(); n != 1 {
		t.Errorf("enviadas = %d, quero 1", n)
	}
}

func TestFakeGraphRefuseSendBecomesACLASSIFIEDError(t *testing.T) {
	// This is the half of the lab that cannot be missing: the operator has to be
	// able to see, with the real binary, that a refused send does NOT activate the
	// instance. For that, the refusal has to reach the gateway as a refusal from
	// Meta, and not as anything else.
	s := fakeServer(t, &fakeGraph{refuseSend: true})
	c := meta.NewClient(s.Client(), s.URL)

	_, err := c.SendMessage(context.Background(), "PNID1", "token", testBody())
	var metaError *meta.MetaError
	if !errors.As(err, &metaError) {
		t.Fatalf("SendMessage devolveu %v, quero um *meta.MetaError", err)
	}
	if metaError.Class != meta.ClassPermanent {
		t.Errorf("classe = %q, quero %q (400 da Meta)", metaError.Class, meta.ClassPermanent)
	}
	if metaError.MetaCode == 0 {
		t.Error("o codigo da Meta nao chegou — o corpo de erro do falso nao tem o formato da Meta")
	}
}

func TestFakeGraphRefuseTokenFailsAtSTEP2(t *testing.T) {
	s := fakeServer(t, &fakeGraph{refuseToken: true})
	c := meta.NewClient(s.Client(), s.URL)

	err := c.CheckCredential(context.Background(), "PNID1", "token")
	var metaError *meta.MetaError
	if !errors.As(err, &metaError) {
		t.Fatalf("CheckCredential devolveu %v, quero um *meta.MetaError", err)
	}
	if metaError.Class != meta.ClassConfig {
		t.Errorf("classe = %q, quero %q (401 e credencial)", metaError.Class, meta.ClassConfig)
	}
}

func testComponents() []byte {
	return []byte(`[{"type":"BODY","text":"Ola {{1}}"}]`)
}

func createOnFake(t *testing.T, c *meta.Client, name string) (meta.CreatedTemplate, error) {
	t.Helper()
	return c.CreateTemplate(context.Background(), "WABA1", "token", meta.TemplateRequest{
		Name: name, Category: "UTILITY", Language: "pt_BR", Components: testComponents(),
	})
}

// The HAPPY path of the catalog: without it, the three failure modes below could
// be passed by a fake that simply does not know how to create any template.
func TestFakeGraphCreatesTemplateAndShowsItInTheCatalog(t *testing.T) {
	g := &fakeGraph{}
	s := fakeServer(t, g)
	c := meta.NewClient(s.Client(), s.URL)

	created, err := createOnFake(t, c, "lembrete_consulta")
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if created.ID == "" {
		t.Fatal("a criacao veio sem id — meta.ErrTemplateWithoutID sairia acima")
	}

	list, err := c.ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 1 || list[0].Name != "lembrete_consulta" {
		t.Fatalf("catalogo = %+v, queria o template recem-criado", list)
	}
	if list[0].ID == "" {
		t.Error("o item do catalogo veio sem id — a releitura da T-078 nao teria id para devolver")
	}
}

// THE MODE THAT REPRODUCES 2026-07-28: the POST dies without an answer AND the
// template exists. It is the scenario in which the gateway has to answer `201`
// after re-reading.
func TestFakeGraphTemplateFailureCREATEDDiesWithoutAnswerAndLeavesTheTemplateInTheCatalog(t *testing.T) {
	g := &fakeGraph{templateFailure: failTemplateCreated}
	s := fakeServer(t, g)
	c := meta.NewClient(s.Client(), s.URL)

	_, err := createOnFake(t, c, "pedido_avaliacao_v2")
	if err == nil {
		t.Fatal("CreateTemplate devolveu sucesso; o POST tinha de morrer sem resposta")
	}
	// It HAS to be a TRANSPORT failure, not a refusal from Meta: it is the absence
	// of an answer that produces the `desconhecido` outcome in the gateway.
	var metaError *meta.MetaError
	if errors.As(err, &metaError) {
		t.Fatalf("veio um *meta.MetaError (%v) — isso e RESPOSTA da Meta, e o desfecho ambiguo nasce da "+
			"AUSENCIA dela", metaError)
	}

	list, err := c.ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates depois da criacao ambigua: %v — a leitura e caminho DIFERENTE e "+
			"tem de continuar funcionando", err)
	}
	if len(list) != 1 || list[0].Name != "pedido_avaliacao_v2" {
		t.Fatalf("catalogo = %+v, queria o template que FOI criado antes de a conexao cair", list)
	}
}

func TestFakeGraphTemplateFailureNOTCREATEDLeavesTheCatalogEmpty(t *testing.T) {
	g := &fakeGraph{templateFailure: failTemplateNotCreated}
	s := fakeServer(t, g)
	c := meta.NewClient(s.Client(), s.URL)

	if _, err := createOnFake(t, c, "lembrete_consulta"); err == nil {
		t.Fatal("CreateTemplate devolveu sucesso; o POST tinha de morrer sem resposta")
	}
	list, err := c.ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("catalogo = %+v, queria vazio", list)
	}
}

func TestFakeGraphTemplateFailureCATALOGTOODropsTheReread(t *testing.T) {
	g := &fakeGraph{templateFailure: failTemplateCatalogToo}
	s := fakeServer(t, g)
	c := meta.NewClient(s.Client(), s.URL)

	if _, err := createOnFake(t, c, "lembrete_consulta"); err == nil {
		t.Fatal("CreateTemplate devolveu sucesso; o POST tinha de morrer sem resposta")
	}
	if _, err := c.ListTemplates(context.Background(), "WABA1", "token", ""); err == nil {
		t.Fatal("ListTemplates funcionou; neste modo o GET tambem tem de cair")
	}
}

func TestFakeGraphRefusesCallWithoutAuthorization(t *testing.T) {
	// The token goes in the HEADER, never in the URL. If the gateway stopped
	// sending it, the lab has to flag it — otherwise the fake would be more
	// permissive than Meta, and the defect would only show up against her.
	s := fakeServer(t, &fakeGraph{})

	resp, err := s.Client().Get(s.URL + "/PNID1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d sem Authorization, quero %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestFakeGraphRefusesRouteTheGraphAPIDoesNotHave(t *testing.T) {
	// A generous fake (200 for everything) would hide a call to the wrong path — a
	// defect that would only show up against the real Meta.
	s := fakeServer(t, &fakeGraph{})

	req, err := http.NewRequest(http.MethodDelete, s.URL+"/PNID1/messages", nil)
	if err != nil {
		t.Fatalf("montar requisicao: %v", err)
	}
	req.Header.Set("Authorization", "Bearer token")
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d numa rota que a Graph API nao tem, quero %d", resp.StatusCode, http.StatusNotFound)
	}
}
