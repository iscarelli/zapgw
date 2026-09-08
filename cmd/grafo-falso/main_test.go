// What these tests guard: that the fake speaks the language the PRODUCTION code
// understands.
//
// They do not exercise the gateway — that is cmd/zapgw/smoke_test.go, and in
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
		t.Fatalf("CheckCredential: %v — step 2 of the smoke test would abort in the lab", err)
	}

	resp, err := c.SendMessage(context.Background(), "PNID1", "token", testBody())
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.ID == "" {
		// meta.ErrResponseWithoutID would have come out above; this line covers the day
		// that guarantee moves elsewhere. An empty id would activate the instance
		// over a channel that never proved it delivers.
		t.Fatal("the response came with no wa_message_id")
	}

	second, err := c.SendMessage(context.Background(), "PNID1", "token", testBody())
	if err != nil {
		t.Fatalf("SendMessage (second): %v", err)
	}
	if second.ID == resp.ID {
		t.Errorf("the two sends returned the same id (%q) — the fake does not tell messages apart", resp.ID)
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
		t.Errorf("reads = %d, want 1", n)
	}
	if n := g.sent.Load(); n != 0 {
		t.Errorf("sent = %d, want 0 — marking as read is NOT sending", n)
	}

	// And the send goes on being a send, on the same path.
	if _, err := c.SendMessage(context.Background(), "PNID1", "token", testBody()); err != nil {
		t.Fatalf("SendMessage after marking: %v", err)
	}
	if n := g.sent.Load(); n != 1 {
		t.Errorf("sent = %d, want 1", n)
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
		t.Fatalf("SendMessage returned %v, want a *meta.MetaError", err)
	}
	if metaError.Class != meta.ClassPermanent {
		t.Errorf("class = %q, want %q (400 from Meta)", metaError.Class, meta.ClassPermanent)
	}
	if metaError.MetaCode == 0 {
		t.Error("Meta's code did not come through — the fake's error body does not have Meta's format")
	}
}

func TestFakeGraphRefuseTokenFailsAtSTEP2(t *testing.T) {
	s := fakeServer(t, &fakeGraph{refuseToken: true})
	c := meta.NewClient(s.Client(), s.URL)

	err := c.CheckCredential(context.Background(), "PNID1", "token")
	var metaError *meta.MetaError
	if !errors.As(err, &metaError) {
		t.Fatalf("CheckCredential returned %v, want a *meta.MetaError", err)
	}
	if metaError.Class != meta.ClassConfig {
		t.Errorf("class = %q, want %q (401 is credential)", metaError.Class, meta.ClassConfig)
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
		t.Fatal("the creation came with no id — meta.ErrTemplateWithoutID would have come out above")
	}

	list, err := c.ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 1 || list[0].Name != "lembrete_consulta" {
		t.Fatalf("catalog = %+v, wanted the freshly created template", list)
	}
	if list[0].ID == "" {
		t.Error("the catalog item came with no id — T-078's reread would have no id to return")
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
		t.Fatal("CreateTemplate returned success; the POST had to die with no response")
	}
	// It HAS to be a TRANSPORT failure, not a refusal from Meta: it is the absence
	// of an answer that produces the `desconhecido` outcome in the gateway.
	var metaError *meta.MetaError
	if errors.As(err, &metaError) {
		t.Fatalf("got a *meta.MetaError (%v) — that is a RESPONSE from Meta, and the ambiguous outcome is born from "+
			"its ABSENCE", metaError)
	}

	list, err := c.ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates after the ambiguous creation: %v — the read is a DIFFERENT path and "+
			"has to keep working", err)
	}
	if len(list) != 1 || list[0].Name != "pedido_avaliacao_v2" {
		t.Fatalf("catalog = %+v, wanted the template that WAS created before the connection dropped", list)
	}
}

func TestFakeGraphTemplateFailureNOTCREATEDLeavesTheCatalogEmpty(t *testing.T) {
	g := &fakeGraph{templateFailure: failTemplateNotCreated}
	s := fakeServer(t, g)
	c := meta.NewClient(s.Client(), s.URL)

	if _, err := createOnFake(t, c, "lembrete_consulta"); err == nil {
		t.Fatal("CreateTemplate returned success; the POST had to die with no response")
	}
	list, err := c.ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("catalog = %+v, wanted empty", list)
	}
}

func TestFakeGraphTemplateFailureCATALOGTOODropsTheReread(t *testing.T) {
	g := &fakeGraph{templateFailure: failTemplateCatalogToo}
	s := fakeServer(t, g)
	c := meta.NewClient(s.Client(), s.URL)

	if _, err := createOnFake(t, c, "lembrete_consulta"); err == nil {
		t.Fatal("CreateTemplate returned success; the POST had to die with no response")
	}
	if _, err := c.ListTemplates(context.Background(), "WABA1", "token", ""); err == nil {
		t.Fatal("ListTemplates worked; in this mode the GET also has to fail")
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
		t.Errorf("status = %d with no Authorization, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestFakeGraphRefusesRouteTheGraphAPIDoesNotHave(t *testing.T) {
	// A generous fake (200 for everything) would hide a call to the wrong path — a
	// defect that would only show up against the real Meta.
	s := fakeServer(t, &fakeGraph{})

	req, err := http.NewRequest(http.MethodDelete, s.URL+"/PNID1/messages", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer token")
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d on a route the Graph API does not have, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
