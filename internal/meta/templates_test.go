package meta

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeTemplateGraph is the fake Graph API that paginates.
//
// Mutable state under a mutex because httptest.Server serves each request
// in its own goroutine — this project already paid a Critical for a raw
// counter in a shared handler (docs/ARMADILHAS.md, "Go / concorrência").
type fakeTemplateGraph struct {
	mu sync.Mutex

	// paginas[i] are page i's raw JSON items. The cursor is the `pagina`
	// query param, which is ours and not Meta's: the client CANNOT know
	// about it, it only follows the `paging.next` that comes in the
	// body.
	pages [][]string

	// alwaysNext makes EVERY page announce a next one. It's the
	// catalog that never ends — the only way to exercise the ceiling
	// without depending on 5000 fixture items.
	alwaysNext bool

	// forcedNext replaces `paging.next` with a URL chosen by the
	// test (used for the "Meta points to another origin" case).
	forcedNext string

	base           string
	urls           []string
	authorizations []string
}

func (g *fakeTemplateGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(g.serve))
	g.mu.Lock()
	g.base = s.URL
	g.mu.Unlock()
	t.Cleanup(s.Close)
	return s
}

func (g *fakeTemplateGraph) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.urls = append(g.urls, r.URL.String())
	g.authorizations = append(g.authorizations, r.Header.Get("Authorization"))
	pages, always, forced, base := g.pages, g.alwaysNext, g.forcedNext, g.base
	g.mu.Unlock()

	i, _ := strconv.Atoi(r.URL.Query().Get("pagina"))

	var items []string
	if i < len(pages) {
		items = pages[i]
	} else if always {
		items = []string{`{"name":"t-extra","status":"APPROVED","category":"UTILITY","language":"pt_BR"}`}
	}

	next := ""
	if always || i+1 < len(pages) {
		q := r.URL.Query()
		q.Set("pagina", strconv.Itoa(i+1))
		next = base + r.URL.Path + "?" + q.Encode()
	}
	if forced != "" {
		next = forced
	}

	body := `{"data":[` + strings.Join(items, ",") + `]`
	if next != "" {
		body += `,"paging":{"cursors":{"after":"CURSOR"},"next":` + strconv.Quote(next) + `}`
	}
	body += `}`

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func (g *fakeTemplateGraph) seen() (urls, authorizations []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.urls...), append([]string(nil), g.authorizations...)
}

func rawTemplate(name, status string) string {
	return `{"id":` + strconv.Quote("id-de-"+name) + `,"name":` + strconv.Quote(name) +
		`,"status":` + strconv.Quote(status) +
		`,"category":"UTILITY","language":"pt_BR","components":[{"type":"BODY","text":"oi"}]}`
}

// The catalog item's `id` was added in T-078: without it, re-reading after
// an ambiguous creation knows how to say "it exists" and doesn't know how
// to say "it's this one".
func TestListTemplatesBringsEachItemsId(t *testing.T) {
	g := &fakeTemplateGraph{pages: [][]string{{rawTemplate("lembrete_consulta", "APPROVED")}}}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 1 || list[0].ID != "id-de-lembrete_consulta" {
		t.Fatalf("lista = %+v, queria o id do item", list)
	}
}

// And the id's ABSENCE does NOT take the catalog down, unlike the name's
// absence.
//
// The reason is in the `ID` field's comment in templates.go: it wasn't
// verified against Meta's source that every catalog page carries `id`, and
// trading a useful catalog for NO catalog — which is what an error here
// would do — is the expensive version of the mistake. Whoever depends on
// the id handles the empty case explicitly.
func TestListTemplatesWithoutAnIdKeepsReadingTheCatalog(t *testing.T) {
	withoutID := `{"name":"lembrete_consulta","status":"APPROVED","category":"UTILITY","language":"pt_BR"}`
	g := &fakeTemplateGraph{pages: [][]string{{withoutID}}}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v — item sem `id` nao pode derrubar a leitura do catalogo", err)
	}
	if len(list) != 1 || list[0].Name != "lembrete_consulta" || list[0].ID != "" {
		t.Fatalf("lista = %+v, queria o template com id vazio", list)
	}
}

// T-116: `motivo` arrives intact when Meta sends `rejected_reason`, and
// "NONE" (the normal value when there's no reason, see Template.Reason's
// comment) survives without becoming empty — the same doctrine as
// TestParseWebhookTemplateStatusReasonNONEAndAbsenceAreDifferentThings, on
// the webhook side.
func TestListTemplatesBringsTheRejectionReasonWhenMetaSendsIt(t *testing.T) {
	rejected := `{"name":"acessar_galeria","status":"REJECTED","category":"UTILITY",` +
		`"language":"pt_BR","rejected_reason":"INCORRECT_CATEGORY"}`
	withNone := `{"name":"lembrete_consulta","status":"APPROVED","category":"UTILITY",` +
		`"language":"pt_BR","rejected_reason":"NONE"}`
	g := &fakeTemplateGraph{pages: [][]string{{rejected, withNone}}}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(lista) = %d, quero 2", len(list))
	}
	if list[0].Reason != "INCORRECT_CATEGORY" {
		t.Errorf("Reason = %q, quero INCORRECT_CATEGORY", list[0].Reason)
	}
	if list[1].Reason != "NONE" {
		t.Errorf("Reason = %q, quero a string NONE — a Meta a manda literalmente, nao e ausencia", list[1].Reason)
	}
}

// And the field's ABSENCE (no consumer should see "motivo" in the JSON when
// Meta didn't send `rejected_reason`) is different from "NONE" — the same
// guard as `ID`: an accessory field not confirmed against the source cannot
// take down the catalog read, and "absent" cannot become a visible "" in
// the contract.
func TestListTemplatesWithoutRejectedReasonShowsNoReasonInTheJSON(t *testing.T) {
	withoutReason := `{"name":"lembrete_consulta","status":"APPROVED","category":"UTILITY","language":"pt_BR"}`
	g := &fakeTemplateGraph{pages: [][]string{{withoutReason}}}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v — item sem rejected_reason nao pode derrubar a leitura do catalogo", err)
	}
	if len(list) != 1 || list[0].Reason != "" {
		t.Fatalf("lista = %+v, queria motivo vazio", list)
	}
	b, err := json.Marshal(list[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "motivo") {
		t.Errorf("a chave \"motivo\" apareceu num item sem rejected_reason: %s", b)
	}
}

// TRAP, and it has already cost something: the old gateway only returned
// the FIRST 25 templates, and a system on this network has 84. That's what
// took it out of production. The truncation gives no error — it returns a
// plausible, short list, and the consumer concludes the template "doesn't
// exist" and never sends it. Here, three pages of two items have to become
// SIX.
func TestListTemplatesJoinsEveryPage(t *testing.T) {
	g := &fakeTemplateGraph{pages: [][]string{
		{rawTemplate("t1", "APPROVED"), rawTemplate("t2", "APPROVED")},
		{rawTemplate("t3", "APPROVED"), rawTemplate("t4", "APPROVED")},
		{rawTemplate("t5", "APPROVED"), rawTemplate("t6", "APPROVED")},
	}}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 6 {
		t.Fatalf("templates = %d, quero 6 — pagina que para na primeira e o truncamento em 25 com outro numero", len(list))
	}
	for i, want := range []string{"t1", "t2", "t3", "t4", "t5", "t6"} {
		if list[i].Name != want {
			t.Errorf("lista[%d].Name = %q, quero %q", i, list[i].Name, want)
		}
	}
	if list[0].Category != "UTILITY" || list[0].Language != "pt_BR" || list[0].Status != "APPROVED" {
		t.Errorf("campos do primeiro item = %+v", list[0])
	}
	var components []map[string]any
	if err := json.Unmarshal(list[0].Components, &components); err != nil {
		t.Errorf("componentes nao viajaram: %v (%s)", err, list[0].Components)
	}

	urls, _ := g.seen()
	if len(urls) != 3 {
		t.Errorf("paginas buscadas = %d, quero 3", len(urls))
	}
}

// EXCEEDING THE CEILING IS AN ERROR. Returning the partial list "because we
// already have plenty" would be reinventing the 25-item truncation with a
// different number — and this time with the excuse of having been
// deliberate. The ceiling exists only so it doesn't spin forever.
func TestListTemplatesBlowingTheCapGivesAnErrorNotAPartialList(t *testing.T) {
	g := &fakeTemplateGraph{alwaysNext: true}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
	if !errors.Is(err, ErrIncompleteCatalog) {
		t.Fatalf("err = %v, quero ErrIncompleteCatalog", err)
	}
	if list != nil {
		t.Fatalf("devolveu %d templates junto com o erro — lista parcial e a armadilha, nao o conserto", len(list))
	}

	urls, _ := g.seen()
	if len(urls) != pageCap {
		t.Errorf("paginas buscadas = %d, quero %d (o maxBytes)", len(urls), pageCap)
	}
}

// The status filter HAS to apply, and it applies even when Meta ignores
// the parameter: promising "APPROVED" and returning the whole catalog
// makes the consumer send a REJECTED template and take an error to the end
// customer's face.
//
// The fake server here IGNORES `status` on purpose — that's how this test
// proves the filter on our side, and not the fake server's good manners.
func TestListTemplatesFiltersByStatusEvenIfMetaIgnoresIt(t *testing.T) {
	g := &fakeTemplateGraph{pages: [][]string{
		{rawTemplate("aprovado1", "APPROVED"), rawTemplate("pendente", "PENDING")},
		{rawTemplate("rejeitado", "REJECTED"), rawTemplate("aprovado2", "APPROVED")},
	}}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "APPROVED")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("templates = %d, quero 2 (so os APPROVED); veio %+v", len(list), list)
	}
	for _, tpl := range list {
		if tpl.Status != "APPROVED" {
			t.Errorf("template %q com status %q escapou do filtro", tpl.Name, tpl.Status)
		}
	}

	// The parameter also GOES to Meta: filtering only here would force
	// pulling the whole catalog every time.
	urls, authorizations := g.seen()
	if !strings.Contains(urls[0], "status=APPROVED") {
		t.Errorf("a primeira URL nao levou o filtro: %q", urls[0])
	}
	if !strings.Contains(urls[0], "limit=") {
		t.Errorf("a primeira URL nao pediu limite de pagina: %q", urls[0])
	}
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	for i, u := range urls {
		if strings.Contains(u, "token") {
			t.Errorf("url[%d] carrega o token: %q", i, u)
		}
	}
	if authorizations[0] != "Bearer token" {
		t.Errorf("Authorization = %q", authorizations[0])
	}
}

// `paging.next` comes from the response BODY. Following it blindly sends
// the instance's send token to whatever host shows up there. Pagination
// stops, with a named error — and the token doesn't go out.
func TestListTemplatesRefusesANextFromAnotherOrigin(t *testing.T) {
	g := &fakeTemplateGraph{
		pages:      [][]string{{rawTemplate("t1", "APPROVED")}},
		forcedNext: "https://exemplo-invasor.invalido/v25.0/WABA1/message_templates?after=X",
	}
	srv := g.server(t)

	list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
	if !errors.Is(err, ErrPageFromAnotherOrigin) {
		t.Fatalf("err = %v, quero ErrPageFromAnotherOrigin", err)
	}
	if list != nil {
		t.Errorf("devolveu lista parcial junto com o erro: %d itens", len(list))
	}
	// The rejected URL does NOT go into the message: it can carry a
	// credential in the query, and this text goes up to the log.
	if strings.Contains(err.Error(), "exemplo-invasor") {
		t.Errorf("a mensagem de erro carrega a URL recusada: %v", err)
	}
}

// A malformed item is NOT skipped: skipping is truncation with a different
// name, and the outcome is the same as the 25-item trap — a short,
// plausible list, no error. A `null` item doesn't fail the Unmarshal
// (docs/ARMADILHAS.md, "Go / JSON"): it becomes a zeroed struct, and
// without the name check it would become a ghost template.
func TestListTemplatesRefusesAMalformedItemInsteadOfSkipping(t *testing.T) {
	cases := []string{
		`null`,
		`{}`,
		`{"name":""}`,
		`{"name":"   "}`,
		`{"name":123}`,
		`"nao e objeto"`,
	}
	for _, item := range cases {
		g := &fakeTemplateGraph{pages: [][]string{{rawTemplate("t1", "APPROVED"), item}}}
		srv := g.server(t)

		list, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
		if err == nil {
			t.Errorf("item %s passou como catalogo bom: %+v", item, list)
		}
		// T-115 (5): "there was an error" and "there was THIS error" are
		// different claims. ErrCatalogNotUnderstood's three siblings
		// (ErrInvalidWabaID, ErrIncompleteCatalog, ErrPageFromAnotherOrigin)
		// already have `errors.Is` in their tests — this was the only
		// one that just checked `err != nil`, and it's the error
		// internal/outbound/templates_handler.go uses to decide
		// 503/retryable (see the handler test that exercises that
		// branch, TestTemplatesListReturns503RetryableWhenTheCatalogIsNotUnderstood).
		if !errors.Is(err, ErrCatalogNotUnderstood) {
			t.Errorf("item %s: erro = %v, quero ErrCatalogNotUnderstood", item, err)
		}
		if list != nil {
			t.Errorf("item %s devolveu lista de %d itens junto com o erro", item, len(list))
		}
	}
}

// THE SAME guard as sending, for the SAME reason: url.JoinPath resolves
// `..` like path.Join, so an id with `../` escapes the Graph API's version
// prefix and points to another endpoint.
func TestListTemplatesRefusesAnInvalidWabaID(t *testing.T) {
	g := &fakeTemplateGraph{pages: [][]string{{rawTemplate("t1", "APPROVED")}}}
	srv := g.server(t)

	for _, id := range []string{"", "../me", "WABA 1", "WABA/1"} {
		_, err := testClient(srv).ListTemplates(context.Background(), id, "token", "")
		if !errors.Is(err, ErrInvalidWabaID) {
			t.Errorf("waba_id %q: err = %v, quero ErrInvalidWabaID", id, err)
		}
	}
	if urls, _ := g.seen(); len(urls) != 0 {
		t.Errorf("falou %d vez(es) com a Meta com waba_id invalido", len(urls))
	}
}

// The classification is STRUCTURAL (HTTP status), the same as sending —
// there's no Meta code table anywhere in this project.
func TestListTemplatesClassifiesTheResponse(t *testing.T) {
	cases := []struct {
		status int
		class_ ErrorClass
	}{
		{http.StatusUnauthorized, ClassConfig},
		{http.StatusTooManyRequests, ClassRetryable},
		{http.StatusInternalServerError, ClassRetryable},
		{http.StatusBadRequest, ClassPermanent},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(`{"error":{"message":"nao","code":190}}`))
		}))

		_, err := testClient(srv).ListTemplates(context.Background(), "WABA1", "token", "")
		srv.Close()

		var me *MetaError
		if !errors.As(err, &me) {
			t.Fatalf("status %d: err = %v, quero *MetaError", c.status, err)
		}
		if me.Class != c.class_ {
			t.Errorf("status %d: classe = %q, quero %q", c.status, me.Class, c.class_)
		}
	}
}

func TestCreateTemplateReturnsMetasIDAndStatus(t *testing.T) {
	var mu sync.Mutex
	var received []byte
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received, authorization = body, r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1234","status":"PENDING","category":"UTILITY"}`))
	}))
	defer srv.Close()

	created, err := testClient(srv).CreateTemplate(context.Background(), "WABA1", "token", TemplateRequest{
		Name:       "lembrete_consulta",
		Category:   "UTILITY",
		Language:   "pt_BR",
		Components: json.RawMessage(`[{"type":"BODY","text":"oi"}]`),
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if created.ID != "1234" {
		t.Errorf("ID = %q, quero 1234", created.ID)
	}
	// The status comes from META, not from a constant of ours: if it
	// ever answers something else, the consumer has to see what it
	// said.
	if created.Status != "PENDING" {
		t.Errorf("Status = %q, quero PENDING", created.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(received, &body); err != nil {
		t.Fatalf("corpo enviado a Meta nao e JSON: %v (%s)", err, received)
	}
	for key, want := range map[string]string{
		"name":       `"lembrete_consulta"`,
		"category":   `"UTILITY"`,
		"language":   `"pt_BR"`,
		"components": `[{"type":"BODY","text":"oi"}]`,
	} {
		if string(body[key]) != want {
			t.Errorf("corpo[%q] = %s, quero %s", key, body[key], want)
		}
	}
	if authorization != "Bearer token" {
		t.Errorf("Authorization = %q", authorization)
	}
}

// A Meta `2xx` does NOT prove an id came with it — the same trap as
// sending (ErrResponseWithoutID), in different clothes. Returning an empty id
// as success makes the consumer store a record that LOOKS created.
func TestCreateTemplateRefuses2xxWithoutAnID(t *testing.T) {
	cases := []string{`{}`, `{"id":""}`, `{"id":"   "}`, `{"id":123}`, `null`, ``, `nao e json`}
	for _, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))

		created, err := testClient(srv).CreateTemplate(context.Background(), "WABA1", "token", TemplateRequest{
			Name: "n", Category: "UTILITY", Language: "pt_BR",
			Components: json.RawMessage(`[]`),
		})
		srv.Close()

		if !errors.Is(err, ErrTemplateWithoutID) {
			t.Errorf("corpo %q: err = %v, quero ErrTemplateWithoutID (criado = %+v)", body, err, created)
		}
	}
}

func TestCreateTemplateClassifiesMetasRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"template name is invalid","code":100}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).CreateTemplate(context.Background(), "WABA1", "token", TemplateRequest{
		Name: "n", Category: "UTILITY", Language: "pt_BR", Components: json.RawMessage(`[]`),
	})
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, quero *MetaError", err)
	}
	if me.Class != ClassPermanent {
		t.Errorf("classe = %q, quero %q", me.Class, ClassPermanent)
	}
	if me.MetaCode != 100 {
		t.Errorf("codigo_meta = %d, quero 100", me.MetaCode)
	}
}

// DeleteTemplate has to send exactly ONE shape: DELETE on the WABA's
// message_templates edge, with the name in the QUERY and the token in the
// HEADER.
//
// The three assertions are three different guarantees, and none of them is
// cosmetic: the METHOD (a POST here would create instead of delete), the
// `name` parameter (the only supported form — never `hsm_id`/`hsm_ids`, see
// the function's comment), and the token OUT of the URL (a token in a query
// string leaks into proxy, server, and CDN logs).
func TestDeleteTemplateSendsDeleteWithTheNameInTheQueryAndTheTokenInTheHeader(t *testing.T) {
	var mu sync.Mutex
	var method, lookup, path, authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, lookup, path = r.Method, r.URL.RawQuery, r.URL.Path
		authorization = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	if err := testClient(srv).DeleteTemplate(context.Background(),
		"WABA1", "token", "promo_julho"); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if method != http.MethodDelete {
		t.Errorf("metodo = %q, quero DELETE", method)
	}
	if !strings.HasSuffix(path, "/WABA1/message_templates") {
		t.Errorf("caminho = %q", path)
	}
	if lookup != "name=promo_julho" {
		t.Errorf("query = %q, quero name=promo_julho (nunca hsm_id nem hsm_ids)", lookup)
	}
	if authorization != "Bearer token" {
		t.Errorf("Authorization = %q", authorization)
	}
	if strings.Contains(lookup, "token") {
		t.Errorf("o token viajou na query: %q", lookup)
	}
}

// A Meta `2xx` does NOT prove the deletion happened — the same trap as
// ErrTemplateWithoutID on the creation, on the other verb. Reporting "deleted"
// over a body that did not say so makes the consumer cross the name off its
// cleanup list while the template stays on the account.
//
// `{}` and `null` are in the list because they do NOT fail the Unmarshal
// (docs/ARMADILHAS.md, "Go / JSON"): they become a zeroed struct, which is
// exactly why Success is a POINTER.
func TestDeleteTemplateRefuses2xxWithoutSuccessTrue(t *testing.T) {
	cases := []string{`{}`, `{"success":false}`, `null`, ``, `nao e json`, `{"success":"true"}`}
	for _, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))

		err := testClient(srv).DeleteTemplate(context.Background(), "WABA1", "token", "promo_julho")
		srv.Close()

		if !errors.Is(err, ErrDeletionNotConfirmed) {
			t.Errorf("corpo %q: err = %v, quero ErrDeletionNotConfirmed", body, err)
		}
	}
}

func TestDeleteTemplateAcceptsSuccessTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	if err := testClient(srv).DeleteTemplate(context.Background(),
		"WABA1", "token", "promo_julho"); err != nil {
		t.Errorf("DeleteTemplate com success:true: %v", err)
	}
}

func TestDeleteTemplateClassifiesMetasRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"template is disabled","code":100}}`))
	}))
	defer srv.Close()

	err := testClient(srv).DeleteTemplate(context.Background(), "WABA1", "token", "promo_julho")
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, quero *MetaError", err)
	}
	if me.Class != ClassPermanent {
		t.Errorf("classe = %q, quero %q", me.Class, ClassPermanent)
	}
	if me.MetaCode != 100 {
		t.Errorf("codigo_meta = %d, quero 100", me.MetaCode)
	}
}

// The SAME guard as the two sisters, and the same function: url.JoinPath
// resolves `..`, so a waba_id with `../` would escape the version prefix and
// point the DELETE at another endpoint.
func TestDeleteTemplateRefusesAnInvalidWabaID(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	err := testClient(srv).DeleteTemplate(context.Background(), "../me", "token", "promo_julho")
	if !errors.Is(err, ErrInvalidWabaID) {
		t.Fatalf("err = %v, quero ErrInvalidWabaID", err)
	}
	if called {
		t.Errorf("falou com a Meta com waba_id invalido")
	}
}

func TestCreateTemplateRefusesAnInvalidWabaID(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	_, err := testClient(srv).CreateTemplate(context.Background(), "../me", "token", TemplateRequest{
		Name: "n", Category: "UTILITY", Language: "pt_BR", Components: json.RawMessage(`[]`),
	})
	if !errors.Is(err, ErrInvalidWabaID) {
		t.Fatalf("err = %v, quero ErrInvalidWabaID", err)
	}
	if called {
		t.Errorf("falou com a Meta com waba_id invalido")
	}
}
