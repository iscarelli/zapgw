package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// fakeTemplateMeta is the fake Graph API: paginates on GET and answers
// with whatever the test tells it to on POST.
//
// Mutable state under mutex and an atomic counter because httptest.Server
// serves each request in its own goroutine — a raw counter here is a data
// race, and this project has already paid a Critical for exactly that
// (docs/ARMADILHAS.md).
type fakeTemplateMeta struct {
	mu sync.Mutex

	pages        [][]string
	alwaysNext   bool
	createStatus int
	createBody   string

	// abortCreate drops the POST's connection with NO response at all —
	// the TRANSPORT failure from 2026-07-28, reproduced. `panic(http.ErrAbortHandler)`
	// is how the test server closes the connection midway: a `500` would BE
	// a response, and a response is precisely what did not happen that day.
	abortCreate bool
	// createEvenWhenAborting reproduces the REAL outcome: the template WAS
	// created and only the response did not arrive. Without this half, the
	// test would prove a case that did not happen.
	createEvenWhenAborting bool
	// abortRead drops the GET too — the third outcome, where not even
	// the re-read resolves it.
	abortRead bool

	// --- DELETION (T-173) ---
	//
	// deleteStatus/deleteBody answer the DELETE; zero values mean the happy
	// path (`200 {"success":true}`), so a test that does not care about the
	// deletion's response does not have to spell it out.
	deleteStatus int
	deleteBody   string
	// abortDelete drops the DELETE's connection with NO response at all
	// — the ambiguous outcome, the same shape as abortCreate.
	abortDelete bool
	// deleteEffect says what the DELETE does to the FAKE CATALOG, and it
	// is the axis the three outcomes turn on: it is the catalog re-read, not
	// the DELETE's own response, that decides `apagado` vs `inconclusivo`.
	deleteEffect deletionEffect

	// appearsInRound, when > 0, simulates the DELAYED PROPAGATION of Meta's
	// catalog (T-101, field report from consumer-b): laggingTemplate only
	// enters page 0 of the catalog starting from the indicated GET round —
	// round 1 is the FIRST re-read (immediate), round 2 is the next one
	// (after the first spaced pause), and so on. "Round" is a NEW call to
	// ListTemplates (starts from scratch, `pagina` absent/0), not each of
	// its pages.
	appearsInRound  int
	laggingTemplate string
	laggingLanguage string
	currentRound    int

	base           string
	urls           []string
	receivedBodies []string

	calls atomic.Int64
}

func (m *fakeTemplateMeta) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	m.mu.Lock()
	m.base = s.URL
	m.mu.Unlock()
	t.Cleanup(s.Close)
	return s
}

// deletionEffect is what the fake Meta does to its own catalog when the
// DELETE arrives.
//
// The THREE values are the three real behaviors of the source, and the middle
// one is the one that is easy to miss: Meta documents that deleting a
// template with delivery still in flight leaves it in the catalog under
// PENDING_DELETION. Without exercising that, the suite would go green over a
// gateway that reported `inconclusivo` for a deletion that worked.
type deletionEffect int

const (
	// deletionNoEffect: the catalog does not change — the template stays
	// there, alive. It is the `inconclusivo` scenario when the DELETE also
	// ends with no verdict.
	deletionNoEffect deletionEffect = iota
	// deletionRemoves: the lines with that name leave the catalog.
	deletionRemoves
	// deletionMarksPending: the lines stay, with status PENDING_DELETION.
	deletionMarksPending
)

func (m *fakeTemplateMeta) serve(w http.ResponseWriter, r *http.Request) {
	m.calls.Add(1)

	if r.Method == http.MethodDelete {
		m.mu.Lock()
		m.urls = append(m.urls, r.URL.String())
		status, response := m.deleteStatus, m.deleteBody
		if status == 0 {
			status = http.StatusOK
		}
		if response == "" {
			response = `{"success":true}`
		}
		abort := m.abortDelete
		switch m.deleteEffect {
		case deletionRemoves:
			m.removeFromCatalog(r.URL.Query().Get("name"))
		case deletionMarksPending:
			m.markPendingDeletion(r.URL.Query().Get("name"))
		}
		m.mu.Unlock()

		if abort {
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
		return
	}

	if r.Method == http.MethodPost {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.urls = append(m.urls, r.URL.String())
		m.receivedBodies = append(m.receivedBodies, string(body))
		status, response := m.createStatus, m.createBody
		abort := m.abortCreate
		if m.createEvenWhenAborting {
			var p struct {
				Name     string `json:"name"`
				Language string `json:"language"`
			}
			_ = json.Unmarshal(body, &p)
			if len(m.pages) == 0 {
				m.pages = append(m.pages, nil)
			}
			m.pages[0] = append(m.pages[0], rawTemplateWithLanguage(p.Name, "PENDING", p.Language))
		}
		m.mu.Unlock()

		if abort {
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
		return
	}

	i, _ := strconv.Atoi(r.URL.Query().Get("pagina"))

	m.mu.Lock()
	m.urls = append(m.urls, r.URL.String())
	pages, always, base := m.pages, m.alwaysNext, m.base
	abortRead := m.abortRead
	if i == 0 {
		// Page 0 marks the START of a new ListTemplates — that's what
		// counts as a "round" for appearsInRound.
		m.currentRound++
	}
	round := m.currentRound
	appearsInRound, laggingTemplate, laggingLanguage := m.appearsInRound, m.laggingTemplate, m.laggingLanguage
	m.mu.Unlock()

	if abortRead {
		panic(http.ErrAbortHandler)
	}

	var items []string
	if i < len(pages) {
		items = pages[i]
	} else if always {
		items = []string{testRawTemplate("t-extra", "APPROVED")}
	}
	if i == 0 && appearsInRound > 0 && round >= appearsInRound {
		items = append(items, rawTemplateWithLanguage(laggingTemplate, "PENDING", laggingLanguage))
	}

	nextOne := ""
	if always || i+1 < len(pages) {
		q := r.URL.Query()
		q.Set("pagina", strconv.Itoa(i+1))
		nextOne = base + r.URL.Path + "?" + q.Encode()
	}

	body := `{"data":[` + strings.Join(items, ",") + `]`
	if nextOne != "" {
		body += `,"paging":{"next":` + strconv.Quote(nextOne) + `}`
	}
	body += `}`

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func (m *fakeTemplateMeta) seen() (urls, bodies []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.urls...), append([]string(nil), m.receivedBodies...)
}

// removeFromCatalog and markPendingDeletion are called WITH THE LOCK
// HELD (see serve): httptest serves each request in its own goroutine, and
// this project has already paid a Critical for a raw mutation under
// concurrency (docs/ARMADILHAS.md).
//
// They walk EVERY page, not just the first: deleting by name deletes every
// language, and the languages can be split across pages.
func (m *fakeTemplateMeta) removeFromCatalog(name string) {
	for i, page := range m.pages {
		kept := make([]string, 0, len(page))
		for _, raw := range page {
			if rawItemName(raw) == name {
				continue
			}
			kept = append(kept, raw)
		}
		m.pages[i] = kept
	}
}

func (m *fakeTemplateMeta) markPendingDeletion(name string) {
	for i, page := range m.pages {
		for j, raw := range page {
			if rawItemName(raw) == name {
				m.pages[i][j] = withRawStatus(raw, StatusPendingDeletion)
			}
		}
	}
}

func rawItemName(raw string) string {
	var t struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal([]byte(raw), &t)
	return t.Name
}

// withRawStatus swaps ONLY the `status` field, keeping everything else byte
// for byte: rebuilding the item from scratch would silently drop the `id` and
// the `components`, and the response the gateway sends back carries them.
func withRawStatus(raw, status string) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return raw
	}
	fields["status"] = json.RawMessage(strconv.Quote(status))
	fresh, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return string(fresh)
}

func testRawTemplate(name, status string) string {
	return rawTemplateWithLanguage(name, status, "pt_BR")
}

// rawTemplateWithLanguage assembles a catalog item WITH `id`, which is what the
// T-078 re-read needs to return to the consumer. The id derives from the
// name on purpose: a fixed id would pass just the same today and would mask
// a re-read that found the WRONG template.
func rawTemplateWithLanguage(name, status, language string) string {
	return `{"id":` + strconv.Quote("id-de-"+name) + `,"name":` + strconv.Quote(name) +
		`,"status":` + strconv.Quote(status) + `,"category":"UTILITY","language":` + strconv.Quote(language) +
		`,"components":[{"type":"BODY","text":"oi"}]}`
}

// metaWithCatalogOf assembles the fake Graph API with one page per list of
// names.
func metaWithCatalogOf(pages ...[]string) *fakeTemplateMeta {
	m := &fakeTemplateMeta{createStatus: http.StatusOK, createBody: `{"id":"1234","status":"PENDING","category":"UTILITY"}`}
	for _, names := range pages {
		var items []string
		for _, n := range names {
			items = append(items, testRawTemplate(n, "APPROVED"))
		}
		m.pages = append(m.pages, items)
	}
	return m
}

// testTemplatesHandler takes WHICH instances become active, instead of a
// boolean: each test needs the guard it is targeting to be the FIRST to
// speak. A 403 test on a paused instance would pass green even with the
// binding guard erased (docs/ARMADILHAS.md, "Testes").
func testTemplatesHandler(t *testing.T, m *fakeTemplateMeta, active ...string) http.Handler {
	t.Helper()
	h, _ := templatesHandlerWithStore(t, m, active...)
	return h
}

// templatesHandlerWithStore is the same assembly, ALSO returning the store —
// the deletion counter (T-173) is only readable through it, and a test that
// could not read it would prove the route answers and not that the number moved.
func templatesHandlerWithStore(
	t *testing.T, m *fakeTemplateMeta, active ...string,
) (http.Handler, *config.Store) {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	srv := m.server(t)
	return NewTemplatesHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly), store
}

func callDeleteTemplate(t *testing.T, h http.Handler, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/v1/templates"+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func askTemplates(t *testing.T, h http.Handler, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/templates"+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func createTemplate(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/templates", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type testTemplatesResponse struct {
	Instance  string `json:"instance"`
	Total     int    `json:"total"`
	Templates []struct {
		ID         string          `json:"id"`
		Name       string          `json:"name"`
		Status     string          `json:"status"`
		Category   string          `json:"category"`
		Language   string          `json:"language"`
		Components json.RawMessage `json:"components"`
		Reason     string          `json:"reason"`
	} `json:"templates"`
}

// TRAP, and it has already taken the old gateway out of production: it
// returned only the first 25 templates, and a system on this network has 84.
// The consumer concluded the template "does not exist." Three pages of two
// have to become SIX here, crossing the whole handler — not just the client.
func TestTemplatesReturnsTheWholeCatalogWithoutTruncating(t *testing.T) {
	m := metaWithCatalogOf([]string{"t1", "t2"}, []string{"t3", "t4"}, []string{"t5", "t6"})
	h := testTemplatesHandler(t, m, "lojinha")

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}

	var resp testTemplatesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if len(resp.Templates) != 6 {
		t.Fatalf("templates = %d, quero 6 — lista curta e o defeito que este endpoint existe para nao repetir", len(resp.Templates))
	}
	if resp.Total != 6 {
		t.Errorf("total = %d, quero 6", resp.Total)
	}
	if resp.Instance != "lojinha" {
		t.Errorf("instancia = %q, quero lojinha", resp.Instance)
	}
	names := make([]string, 0, len(resp.Templates))
	for _, tpl := range resp.Templates {
		names = append(names, tpl.Name)
	}
	if want := "t1 t2 t3 t4 t5 t6"; strings.Join(names, " ") != want {
		t.Errorf("nomes = %q, quero %q", strings.Join(names, " "), want)
	}
	if tpl := resp.Templates[0]; tpl.Category != "UTILITY" || tpl.Language != "pt_BR" || tpl.Status != "APPROVED" {
		t.Errorf("campos do primeiro template = %+v", tpl)
	}
	if !strings.Contains(string(resp.Templates[0].Components), "BODY") {
		t.Errorf("componentes nao chegaram ao consumidor: %s", resp.Templates[0].Components)
	}
}

// T-085: NOTHING pinned the `id` in the LISTING's response. The field went
// in at T-078 only for the re-read of an ambiguous creation (`hit.ID` in
// templates_handler.go), and plain `GET /v1/templates` never had a guard of
// its own — with `omitempty` on the struct, the field could disappear the
// day someone touches `meta.Template` without any error catching it.
// consumer-b uses the `id` to distinguish "was on Meta and disappeared" from
// "local draft that never went up"; without it that distinction becomes
// impossible again.
func TestTemplatesListingReturnsTheIdOfEachItem(t *testing.T) {
	m := metaWithCatalogOf([]string{"t1", "t2"}, []string{"t3"})
	h := testTemplatesHandler(t, m, "lojinha")

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}

	var resp testTemplatesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if len(resp.Templates) != 3 {
		t.Fatalf("templates = %d, quero 3", len(resp.Templates))
	}
	for _, tpl := range resp.Templates {
		// testRawTemplate (via metaWithCatalogOf) emits "id-de-<nome>" —
		// that is the value the fake Meta returned, not an id invented here.
		if want := "id-de-" + tpl.Name; tpl.ID != want {
			t.Errorf("id do template %q = %q, quero %q — sem o id o consumidor nao distingue "+
				"'esteve na Meta e sumiu' de 'rascunho local que nunca subiu'", tpl.Name, tpl.ID, want)
		}
	}
}

// T-116, case (d) of Verify — end to end through the handler: the `motivo`
// of a rejection (Meta's raw `rejected_reason`) arrives inside
// `templates[]`, not only in meta.Client tested in isolation in
// internal/meta/templates_test.go. Real cost that triggered the task:
// consumer-b got rejected on "acessar_galeria," formed a blind hypothesis,
// created "acessar_galeria_v2," rejected again — two attempts, zero
// information, and each one burns a template name forever.
func TestTemplatesListingReturnsTheRejectionReason(t *testing.T) {
	rejected := `{"id":"id-de-acessar_galeria","name":"acessar_galeria","status":"REJECTED",` +
		`"category":"UTILITY","language":"pt_BR","rejected_reason":"INCORRECT_CATEGORY"}`
	m := &fakeTemplateMeta{
		createStatus: http.StatusOK, createBody: `{"id":"1234","status":"PENDING","category":"UTILITY"}`,
		pages: [][]string{{rejected}},
	}
	h := testTemplatesHandler(t, m, "lojinha")

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}

	var resp testTemplatesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if len(resp.Templates) != 1 || resp.Templates[0].Reason != "INCORRECT_CATEGORY" {
		t.Fatalf("templates = %+v, queria motivo = INCORRECT_CATEGORY", resp.Templates)
	}
}

// HITTING THE CEILING IS AN ERROR. A partial list returned with `200` would
// be the 25-item truncation all over again, now with a bigger number and the
// look of being deliberate — the consumer has no way of knowing something is
// missing.
func TestTemplatesCapExceededIsAnErrorAndNotAPartialList(t *testing.T) {
	m := metaWithCatalogOf([]string{"t1", "t2"})
	m.alwaysNext = true
	h := testTemplatesHandler(t, m, "lojinha")

	var record bytes.Buffer
	log.SetOutput(&record)
	defer log.SetOutput(os.Stderr) // os.Stderr is the log package's default

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502; corpo = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "\"templates\"") {
		t.Errorf("a resposta de erro carrega uma lista de templates: %s", rec.Body.String())
	}

	var errBody errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("corpo de erro nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if errBody.Error.Class != string(meta.ClassConfig) {
		t.Errorf("classe = %q, quero %q — so gente conserta um catalogo que nao cabe no teto", errBody.Error.Class, meta.ClassConfig)
	}
	if !strings.Contains(record.String(), "ALARME") {
		t.Errorf("o teto estourou sem ALARME; log = %q", record.String())
	}
}

// Status filter: promising APPROVED and returning the whole catalog makes
// the consumer send a REJECTED template and get an error in the end
// customer's face. The fake server IGNORES the parameter on purpose.
func TestTemplatesFiltersByStatus(t *testing.T) {
	m := &fakeTemplateMeta{
		createStatus: http.StatusOK,
		pages: [][]string{
			{testRawTemplate("aprovado1", "APPROVED"), testRawTemplate("pendente", "PENDING")},
			{testRawTemplate("rejeitado", "REJECTED"), testRawTemplate("aprovado2", "APPROVED")},
		},
	}
	h := testTemplatesHandler(t, m, "lojinha")

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha&status=APPROVED")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}

	var resp testTemplatesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v", err)
	}
	if len(resp.Templates) != 2 {
		t.Fatalf("templates = %d, quero 2 (so APPROVED); veio %s", len(resp.Templates), rec.Body.String())
	}
	for _, tpl := range resp.Templates {
		if tpl.Status != "APPROVED" {
			t.Errorf("template %q com status %q escapou do filtro", tpl.Name, tpl.Status)
		}
	}

	urls, _ := m.seen()
	if !strings.Contains(urls[0], "status=APPROVED") {
		t.Errorf("o filtro nao foi repassado a Meta: %q", urls[0])
	}
}

// Empty catalog is `[]`, never `null`: a `null` forces every consumer to
// handle two different empties, and whoever forgets breaks on the first
// client with no template at all.
func TestTemplatesWithNoTemplateAtAllReturnsAnEmptyList(t *testing.T) {
	m := metaWithCatalogOf([]string{})
	h := testTemplatesHandler(t, m, "lojinha")

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"templates":[]`) {
		t.Errorf("catalogo vazio nao veio como lista vazia: %s", rec.Body.String())
	}
}

func TestTemplatesRequiresTheInstance(t *testing.T) {
	m := metaWithCatalogOf([]string{"t1"})
	h := testTemplatesHandler(t, m, "lojinha")

	rec := askTemplates(t, h, "token-do-a", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	if n := m.calls.Load(); n != 0 {
		t.Errorf("falou %d vez(es) com a Meta sem instancia no pedido", n)
	}
}

// REQUIREMENT 3 here too: system A's token does not read B's catalog — a
// template catalog DESCRIBES THE OTHER'S BUSINESS (campaign names, billing
// text). "clinica" ACTIVE on purpose: paused, this test would pass green
// even with the binding guard erased, and the refusal would come from the
// pause instead.
func TestTemplatesRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	m := metaWithCatalogOf([]string{"t1"})
	h := testTemplatesHandler(t, m, "lojinha", "clinica")

	rec := askTemplates(t, h, "token-do-a", "?instancia=clinica")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quero 403; corpo = %s", rec.Code, rec.Body.String())
	}
	if n := m.calls.Load(); n != 0 {
		t.Errorf("falou %d vez(es) com a Meta pela instancia de outro sistema", n)
	}
}

func TestTemplatesWithPausedInstanceAnswers503WithoutCallingMeta(t *testing.T) {
	m := metaWithCatalogOf([]string{"t1"})
	h := testTemplatesHandler(t, m) // does NOT activate: instance is born paused

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503; corpo = %s", rec.Code, rec.Body.String())
	}
	if n := m.calls.Load(); n != 0 {
		t.Errorf("falou %d vez(es) com a Meta por uma instancia PAUSADA", n)
	}
}

// T-115 (5): forces the meta.ErrCatalogNotUnderstood branch in
// respondCatalogError (templates_handler.go:547-549) — the ONLY one of
// the four catalog-error branches that decides 503/retentavel instead of
// 502/config, and that no HANDLER test reached before this task (the
// internal/meta suite already proved the error at the SOURCE, never the
// translation into an HTTP response).
func TestTemplatesListReturns503RetryableWhenTheCatalogIsNotUnderstood(t *testing.T) {
	// Item with no "name": the SAME shape that
	// TestListTemplatesRefusesAMalformedItemInsteadOfSkipping (internal/meta)
	// proves produces meta.ErrCatalogNotUnderstood.
	m := &fakeTemplateMeta{pages: [][]string{{`{}`}}}
	h := testTemplatesHandler(t, m, "lojinha")

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503; corpo = %s", rec.Code, rec.Body.String())
	}
	var errBody errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("corpo de erro nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if errBody.Error.Class != string(meta.ClassRetryable) {
		t.Errorf("classe = %q, quero %q", errBody.Error.Class, meta.ClassRetryable)
	}
}

func TestTemplatesRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	m := metaWithCatalogOf([]string{"t1"})
	h := testTemplatesHandler(t, m, "lojinha")

	if rec := askTemplates(t, h, "", "?instancia=lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
	if rec := askTemplates(t, h, "token-errado", "?instancia=lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status = %d, quero 401", rec.Code)
	}
	if rec := createTemplate(t, h, "", `{"instancia":"lojinha"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST sem token: status = %d, quero 401", rec.Code)
	}
	if n := m.calls.Load(); n != 0 {
		t.Errorf("falou %d vez(es) com a Meta sem consumidor autenticado", n)
	}
}

// Meta's refusal turns into the right class, and the status follows the
// SAME table as sending — the consumer does not learn two taxonomies.
func TestTemplatesTranslatesTheMetaError(t *testing.T) {
	cases := []struct {
		metaStatus int
		want       int
		class      meta.ErrorClass
	}{
		{http.StatusUnauthorized, http.StatusBadGateway, meta.ClassConfig},
		{http.StatusTooManyRequests, http.StatusServiceUnavailable, meta.ClassRetryable},
		{http.StatusBadRequest, http.StatusBadRequest, meta.ClassPermanent},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.metaStatus)
			_, _ = w.Write([]byte(`{"error":{"message":"nao","code":190}}`))
		}))
		store, path := storeWithConsumer(t)
		activateInstance(t, path, "lojinha")
		h := NewTemplatesHandler(store, NewAuthenticator(store),
			meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

		rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
		srv.Close()

		if rec.Code != c.want {
			t.Errorf("Meta %d: status = %d, quero %d; corpo = %s", c.metaStatus, rec.Code, c.want, rec.Body.String())
		}
		var errBody errorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
		if errBody.Error.Class != string(c.class) {
			t.Errorf("Meta %d: classe = %q, quero %q", c.metaStatus, errBody.Error.Class, c.class)
		}
	}
}

// A created template is BORN PENDING, and the response has to say so:
// without that warning the consumer tries to use the template right away and
// gets an error from Meta, which is exactly what it cannot explain to the
// end customer.
func TestCreateTemplateSaysItWasBornPending(t *testing.T) {
	m := metaWithCatalogOf()
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"lembrete_consulta",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201; corpo = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Warning string `json:"aviso"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if resp.ID != "1234" {
		t.Errorf("id = %q, quero 1234", resp.ID)
	}
	if resp.Status != "PENDING" {
		t.Errorf("status = %q, quero PENDING (o que a Meta respondeu)", resp.Status)
	}
	if !strings.Contains(resp.Warning, "PENDING") || !strings.Contains(strings.ToLower(resp.Warning), "aprovad") {
		t.Errorf("o aviso nao diz que o template nasce pendente de aprovacao: %q", resp.Warning)
	}

	// The body that went to Meta uses ITS names, and the components travel
	// with no rewriting.
	_, bodies := m.seen()
	if len(bodies) != 1 {
		t.Fatalf("corpos enviados a Meta = %d, quero 1", len(bodies))
	}
	for _, chunk := range []string{`"name":"lembrete_consulta"`, `"category":"UTILITY"`, `"language":"pt_BR"`, `"type":"BODY"`} {
		if !strings.Contains(bodies[0], chunk) {
			t.Errorf("o corpo enviado a Meta nao contem %s: %s", chunk, bodies[0])
		}
	}
}

// An invalid body is rejected BEFORE touching the wire: sending Meta a
// request already known to be broken spends quota and returns a message
// that is not ours.
func TestCreateTemplateRefusesInvalidBodyWithoutCallingMeta(t *testing.T) {
	cases := []string{
		`{"instancia":"lojinha","categoria":"UTILITY","idioma":"pt_BR","componentes":[]}`,                 // no name
		`{"instancia":"lojinha","nome":"   ","categoria":"UTILITY","idioma":"pt_BR","componentes":[]}`,    // name is only spaces
		`{"instancia":"lojinha","nome":"n","idioma":"pt_BR","componentes":[]}`,                            // no category
		`{"instancia":"lojinha","nome":"n","categoria":"UTILITY","componentes":[]}`,                       // no language
		`{"instancia":"lojinha","nome":"n","categoria":"UTILITY","idioma":"pt_BR"}`,                       // no components
		`{"instancia":"lojinha","nome":"n","categoria":"UTILITY","idioma":"pt_BR","componentes":null}`,    // null does NOT fail the Unmarshal
		`{"instancia":"lojinha","nome":"n","categoria":"UTILITY","idioma":"pt_BR","componentes":{"a":1}}`, // object, not a list
		`{"nome":"n","categoria":"UTILITY","idioma":"pt_BR","componentes":[]}`,                            // no instance
		`nao e json`,
	}
	m := metaWithCatalogOf()
	h := testTemplatesHandler(t, m, "lojinha")

	for _, body := range cases {
		rec := createTemplate(t, h, "token-do-a", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("corpo %s: status = %d, quero 400 (corpo da resposta = %s)", body, rec.Code, rec.Body.String())
		}
	}
	if n := m.calls.Load(); n != 0 {
		t.Errorf("falou %d vez(es) com a Meta por pedido invalido", n)
	}
}

// Creation has no idempotency, so the transport outcome is truly UNKNOWN:
// the template may have been created. Calling this `retentavel` would send
// the consumer to retry blindly.
//
// With the whole destination down, this test exercises the THIRD outcome of
// T-078 — the catalog re-read also fails —, and that's why the `502`
// remains.
func TestCreateTemplateWithTransportFailureAnswers502Unknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // the destination no longer exists: the call never ends in a response

	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewTemplatesHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"n",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502; corpo = %s", rec.Code, rec.Body.String())
	}
	var errBody errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error.Class != string(meta.ClassUnknown) {
		t.Errorf("classe = %q, quero %q", errBody.Error.Class, meta.ClassUnknown)
	}
	// Neither the URL nor the token can travel in the message: *url.Error
	// carries the full URL, and this text goes to the consumer and to the
	// log.
	if strings.Contains(rec.Body.String(), "t-lojinha") || strings.Contains(rec.Body.String(), "127.0.0.1") {
		t.Errorf("a resposta vazou destino ou token: %s", rec.Body.String())
	}
}

// fakeWaits replaces waitReread (templates_handler.go) with a
// spy that does NOT actually sleep — it only records the requested pause and
// returns right away. A test that really slept until the 17s ceiling
// (RereadWaitCap) would stall the whole suite, and this project
// forbids a test that actually sleeps (docs/ARMADILHAS.md).
func fakeWaits(t *testing.T) *[]time.Duration {
	t.Helper()
	seenSet := []time.Duration{}
	original := waitReread
	waitReread = func(ctx context.Context, d time.Duration) {
		seenSet = append(seenSet, d)
	}
	t.Cleanup(func() { waitReread = original })
	return &seenSet
}

// createTemplateWithAmbiguousOutcome assembles the 2026-07-28 scenario: the
// creation's POST DIES with no response. `didCreate` says whether the template
// comes to exist on Meta's side (that's what really happened) and
// `catalogAlsoFails` drops the re-read too.
//
// Also returns what went to the log (half of what T-078 fixes IS the log:
// before it, `journalctl -u zapgw | grep -ci template` came back ZERO on the
// day the `502` went out to production) and the pauses waitReread saw
// — the function installs fakeWaits itself, so NO caller actually
// sleeps even if the scenario exhausts the three pauses of
// RereadWaits (T-101).
func createTemplateWithAmbiguousOutcome(
	t *testing.T, name, language string, didCreate, catalogAlsoFails bool,
) (*httptest.ResponseRecorder, *fakeTemplateMeta, string, *[]time.Duration) {
	t.Helper()
	seenSet := fakeWaits(t)
	m := metaWithCatalogOf()
	m.abortCreate = true
	m.createEvenWhenAborting = didCreate
	m.abortRead = catalogAlsoFails
	h := testTemplatesHandler(t, m, "lojinha")

	var record bytes.Buffer
	log.SetOutput(&record)
	defer log.SetOutput(os.Stderr) // os.Stderr is the log package's default

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":`+strconv.Quote(name)+`,`+
		`"categoria":"UTILITY","idioma":`+strconv.Quote(language)+`,`+
		`"componentes":[{"type":"BODY","text":"Ola {{1}}, sua consulta e amanha."}]}`)
	return rec, m, record.String(), seenSet
}

// THE REAL OUTCOME OF 2026-07-28: `pedido_avaliacao_v2` WAS created and the
// response did not arrive. The consumer got `502 desconhecido` and only
// found out the truth because it still had direct access to the Graph API —
// access the "NINGUÉM fala direto com a Meta" rule has just forbidden. Now
// who checks is the gateway, and the consumer gets the `201` the first call
// would have returned.
func TestCreateTemplateAmbiguousREREADSTheCatalogAndConfirmsTheCreation(t *testing.T) {
	rec, m, record, seenSet := createTemplateWithAmbiguousOutcome(t, "pedido_avaliacao_v2", "pt_BR", true, false)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 — o template EXISTE, e responder erro sobre algo que existe "+
			"e o defeito da T-078; corpo = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		Category    string `json:"category"`
		Warning     string `json:"aviso"`
		Rereads     int    `json:"releituras"`
		WaitSeconds int    `json:"espera_segundos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if resp.ID != "id-de-pedido_avaliacao_v2" {
		t.Errorf("id = %q — a releitura tem de devolver o id do template ACHADO", resp.ID)
	}
	if resp.Status != "PENDING" || resp.Category != "UTILITY" {
		t.Errorf("status/categoria = %q/%q, queria os do catalogo (PENDING/UTILITY)", resp.Status, resp.Category)
	}
	// The consumer has to know its call failed midway: that's what explains
	// the delay and what it will find in its own log.
	if !strings.Contains(resp.Warning, WarningCreationConfirmedByReread) {
		t.Errorf("o aviso nao conta que a criacao foi confirmada por releitura: %q", resp.Warning)
	}
	if !strings.Contains(resp.Warning, "PENDING") {
		t.Errorf("o aviso perdeu a instrucao de que o template nasce pendente: %q", resp.Warning)
	}
	if !strings.Contains(record, "pedido_avaliacao_v2") || !strings.Contains(record, "ACHOU") {
		t.Errorf("o desfecho nao ficou no log; log = %q", record)
	}
	if urls, _ := m.seen(); len(urls) < 2 {
		t.Errorf("a releitura nem aconteceu: urls = %v", urls)
	}
	// T-101's VERIFY (c): found on the FIRST attempt — the common path
	// cannot get slower because of the rare case. No spaced pause can have
	// happened, and the body has to count 1 attempt/0s of wait.
	if resp.Rereads != 1 {
		t.Errorf("releituras = %d, quero 1 — achou de primeira, nenhuma retentativa deveria ter acontecido", resp.Rereads)
	}
	if resp.WaitSeconds != 0 {
		t.Errorf("espera_segundos = %d, quero 0 — achar de primeira nao pode custar espera nenhuma", resp.WaitSeconds)
	}
	if len(*seenSet) != 0 {
		t.Errorf("waitReread foi chamado %d vez(es) mesmo achando na 1a tentativa: %v — "+
			"o caminho comum ficou mais lento", len(*seenSet), *seenSet)
	}
}

// 🔴 THE JUDGMENT CALL OF T-078. "Didn't find it" does NOT authorize saying
// "it was not created": Meta documents read-after-write for the RESPONSE OF
// THIS POST edge and does NOT document anything about the following `GET`
// already containing the template (checked on 2026-07-28). The error is
// asymmetric — saying "I don't know" costs one check; saying "it wasn't"
// makes the consumer repeat and burn the name, which Meta does not reaccept
// (`code 100` / `subcode 2388024`).
//
// THE ASSERTION IS ABOUT THE TEXT THE CONSUMER READS, not the HTTP code: it
// was the text ("the template MAY have been created — check the catalog")
// that did the work in the real case, and it's the text this task's mutation
// attacks.
func TestCreateTemplateAmbiguousNotFoundInTheCatalogIsINCONCLUSIVE(t *testing.T) {
	rec, _, record, seenSet := createTemplateWithAmbiguousOutcome(t, "lembrete_consulta", "pt_BR", false, false)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502; corpo = %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Error struct {
			Class       string `json:"class"`
			Message     string `json:"message"`
			Rereads     int    `json:"releituras"`
			WaitSeconds int    `json:"espera_segundos"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("corpo de erro nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if errBody.Error.Class != string(meta.ClassUnknown) {
		t.Errorf("classe = %q, quero %q", errBody.Error.Class, meta.ClassUnknown)
	}

	// T-101, VERIFY (b): with the spaced attempts exhausted, the `502` comes
	// out with the SAME TEXT as always — compared by EQUALITY, not just
	// `Contains`, so the text can't drift without anyone noticing (the
	// warning does not loosen, it only gets rarer).
	if errBody.Error.Message != MessageInconclusiveOutcome {
		t.Errorf("mensagem = %q, quero EXATAMENTE a constante MessageInconclusiveOutcome — o texto do "+
			"desfecho raro nao pode mudar so porque ele ficou mais raro", errBody.Error.Message)
	}
	lower := strings.ToLower(errBody.Error.Message)
	if !strings.Contains(lower, "inconclusiv") {
		t.Errorf("a mensagem nao diz ao consumidor que o desfecho e INCONCLUSIVO — sem essa palavra ele "+
			"trata a resposta como veredito e repete a criacao; mensagem = %q", errBody.Error.Message)
	}
	// The phrase that CANNOT exist. A "was not created" here is a claim the
	// gateway has no way of backing up, and its price is the template's
	// name.
	for _, banned := range []string{"nao foi criado", "não foi criado", "nao existe", "não existe"} {
		if strings.Contains(lower, banned) {
			t.Errorf("a mensagem afirma %q sobre um catalogo que pode nao estar atualizado: %q",
				banned, errBody.Error.Message)
		}
	}
	if !strings.Contains(lower, "nao repita") {
		t.Errorf("a mensagem nao avisa para NAO repetir as cegas: %q", errBody.Error.Message)
	}
	if !strings.Contains(record, "INCONCLUSIVO") || !strings.Contains(record, "lembrete_consulta") {
		t.Errorf("o desfecho inconclusivo nao ficou no log; log = %q", record)
	}

	// Exhausted the entire RereadWaits (1 immediate attempt + 3
	// spaced) before declaring it inconclusive, and the sum of the waits
	// never goes past the ceiling declared in the contract (VERIFY (d)).
	if errBody.Error.Rereads != len(RereadWaits)+1 {
		t.Errorf("releituras = %d, quero %d (a imediata + as %d espacadas)",
			errBody.Error.Rereads, len(RereadWaits)+1, len(RereadWaits))
	}
	if want := int(RereadWaitCap.Seconds()); errBody.Error.WaitSeconds != want {
		t.Errorf("espera_segundos = %d, quero %d (o teto declarado no contrato)", errBody.Error.WaitSeconds, want)
	}
	if len(*seenSet) != len(RereadWaits) {
		t.Errorf("waitReread foi chamado %d vez(es), quero %d — uma por pausa de RereadWaits: %v",
			len(*seenSet), len(RereadWaits), *seenSet)
	} else {
		for i, expected := range RereadWaits {
			if (*seenSet)[i] != expected {
				t.Errorf("pausa %d = %v, quero %v", i, (*seenSet)[i], expected)
			}
		}
	}
	if sum := sumOfDurations(*seenSet); sum > RereadWaitCap {
		t.Errorf("soma das esperas = %v, estourou o teto declarado no contrato (%v)", sum, RereadWaitCap)
	}
}

// THE CASE THAT OPENED T-101: field report from consumer-b (2026-07-30) —
// the template WAS created and Meta's catalog took a while to propagate.
// Here the FIRST re-read (immediate) does NOT find it, and only the SECOND
// (after the 2s pause) finds it — the delayed propagation simulated by
// fakeTemplateMeta.appearsInRound. T-101's VERIFY (a).
func TestCreateTemplateAmbiguousMissingOnTheFirstRereadAppearsOnTheSecond(t *testing.T) {
	seenSet := fakeWaits(t)
	m := metaWithCatalogOf()
	m.abortCreate = true
	m.appearsInRound = 2
	m.laggingTemplate = "selecao_provas_novas"
	m.laggingLanguage = "pt_BR"
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"selecao_provas_novas",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 — a 2a releitura achou o template; corpo = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID          string `json:"id"`
		Warning     string `json:"aviso"`
		Rereads     int    `json:"releituras"`
		WaitSeconds int    `json:"espera_segundos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if resp.ID != "id-de-selecao_provas_novas" {
		t.Errorf("id = %q — a releitura tem de devolver o id do template ACHADO", resp.ID)
	}
	if !strings.Contains(resp.Warning, WarningCreationConfirmedByReread) {
		t.Errorf("o aviso nao conta que a criacao foi confirmada por releitura: %q", resp.Warning)
	}
	// Disappeared on the 1st, found on the 2nd: exactly TWO attempts and the
	// FIRST pause (2s) — never the 5s/10s pauses, which would only kick in
	// if the 2nd had also failed.
	if resp.Rereads != 2 {
		t.Errorf("releituras = %d, quero 2 (sumiu na 1a, achou na 2a)", resp.Rereads)
	}
	if resp.WaitSeconds != 2 {
		t.Errorf("espera_segundos = %d, quero 2 (so a primeira pausa)", resp.WaitSeconds)
	}
	if want := []time.Duration{2 * time.Second}; len(*seenSet) != len(want) || (*seenSet)[0] != want[0] {
		t.Errorf("esperas vistas = %v, queria exatamente %v", *seenSet, want)
	}
}

// Third outcome: the re-read also failed. Here the `502 desconhecido`
// remains the right response — and BOTH failures have to be in the log,
// which is what makes the next occurrence diagnosable.
func TestCreateTemplateAmbiguousWithARereadThatAlsoFailsLOGSBOTH(t *testing.T) {
	rec, _, record, seenSet := createTemplateWithAmbiguousOutcome(t, "lembrete_consulta", "pt_BR", true, true)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502; corpo = %s", rec.Code, rec.Body.String())
	}
	var errBody errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error.Class != string(meta.ClassUnknown) {
		t.Errorf("classe = %q, quero %q", errBody.Error.Class, meta.ClassUnknown)
	}
	if !strings.Contains(record, "SEM VEREDITO") {
		t.Errorf("a falha da CRIACAO nao foi logada; log = %q", record)
	}
	if !strings.Contains(record, "TAMBEM falhou") {
		t.Errorf("a falha da RELEITURA nao foi logada — sem ela ninguem sabe se o gateway chegou a "+
			"conferir; log = %q", record)
	}
	// Transport failed ON THE FIRST attempt (abortRead drops EVERY
	// GET): no point insisting against a transport that does not respond,
	// so no spaced pause can have happened.
	if len(*seenSet) != 0 {
		t.Errorf("waitReread foi chamado %d vez(es) apos falha de TRANSPORTE: %v — "+
			"insistir contra um transporte que nao responde so atrasa o desfecho desconhecido", len(*seenSet), *seenSet)
	}
}

// THE T-078 MUTATION, as a test: removing the log.Printf from the ambiguous
// outcome has to leave this red. The defect was born silent — on the day of
// the incident, `journalctl -u zapgw | grep -ci template` came back ZERO
// with the `502` having gone out to production —, and an outcome that stores
// nothing is the worst of both worlds: the consumer loses the second door
// and we gain no information.
func TestCreateTemplateAmbiguousLOGSTheRealErrorWithoutLeakingTheRequestBody(t *testing.T) {
	rec, _, record, _ := createTemplateWithAmbiguousOutcome(t, "lembrete_consulta", "pt_BR", false, false)
	if rec.Code == 0 {
		t.Fatal("nenhuma resposta")
	}

	for _, chunk := range []string{"lojinha", "lembrete_consulta", "pt_BR"} {
		if !strings.Contains(record, chunk) {
			t.Errorf("o log nao diz %q — sem slug, nome e idioma ninguem procura o template no catalogo; "+
				"log = %q", chunk, record)
		}
	}
	// "Was it a timeout or transport?" has to be answerable from the log.
	// Without the cause, the outcome remains structurally undiagnosable.
	if !strings.Contains(record, "inalcancavel") && !strings.Contains(record, "prazo esgotado") {
		t.Errorf("o log nao carrega a causa real da falha; log = %q", record)
	}

	// 🔴 THE REQUEST BODY CANNOT ENTER THE LOG: `componentes` is text that
	// goes to the tenant's end customer.
	for _, forbidden := range []string{"sua consulta e amanha", "BODY", "{{1}}"} {
		if strings.Contains(record, forbidden) {
			t.Errorf("o corpo do pedido vazou para o log (%q): %q", forbidden, record)
		}
	}
	// And neither token nor destination, by the same rule as the rest of
	// the project.
	if strings.Contains(record, "t-lojinha") {
		t.Errorf("o token vazou para o log: %q", record)
	}
}

// 🔴 THE RE-READ IS A `GET`, AND ONLY THAT. If it ever turns into "try
// creating it again," the template's name burns: name+language are unique
// per WABA and the second creation comes back `code 100` / `subcode
// 2388024`. This test counts the POSTs that reached Meta in the THREE
// outcomes — and the number is always ONE.
func TestRereadDoesNOTCreateAgain(t *testing.T) {
	cases := []struct {
		name             string
		didCreate        bool
		catalogAlsoFails bool
	}{
		{"achou no catalogo", true, false},
		{"nao achou no catalogo", false, false},
		{"releitura tambem falhou", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, m, _, _ := createTemplateWithAmbiguousOutcome(t, "lembrete_consulta", "pt_BR", c.didCreate, c.catalogAlsoFails)
			_, bodies := m.seen()
			if len(bodies) != 1 {
				t.Fatalf("chegaram %d POSTs a Meta, quero exatamente 1 — uma segunda criacao QUEIMA o nome "+
					"do template, e a Meta nao o reaceita", len(bodies))
			}
		})
	}
}

// The re-read matches by the pair (name, language), which is the template's
// identity within a WABA — Meta allows the SAME name in different languages.
// Finding the sibling of another language and answering `201` would tell the
// consumer its template exists when what exists is a different one.
func TestRereadDoesNotConfuseATemplateOfAnotherLANGUAGE(t *testing.T) {
	fakeWaits(t) // pt_BR never shows up: exhausts the entire RereadWaits.
	m := metaWithCatalogOf()
	m.pages = [][]string{{rawTemplateWithLanguage("lembrete_consulta", "APPROVED", "en_US")}}
	m.abortCreate = true
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"lembrete_consulta",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 — o pt_BR nao esta no catalogo; corpo = %s", rec.Code, rec.Body.String())
	}
	var errBody errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if !strings.Contains(strings.ToLower(errBody.Error.Message), "inconclusiv") {
		t.Errorf("mensagem = %q, queria o desfecho inconclusivo", errBody.Error.Message)
	}
}

// `2xx` WITH NO ID is ambiguous for the same reason as transport — Meta may
// have created it and just not said the id —, and that's why it falls into
// the SAME handling. Leaving this branch out would be this project's mother
// trap: "the rule applies in one branch and not in its neighbor"
// (docs/ARMADILHAS.md).
func TestCreateTemplateWith2xxAndNoIdAlsoREREADSTheCatalog(t *testing.T) {
	m := metaWithCatalogOf()
	m.createBody = `{"status":"PENDING","category":"UTILITY"}` // 200, and with no `id`
	m.pages = [][]string{{testRawTemplate("lembrete_consulta", "PENDING")}}
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"lembrete_consulta",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 — a releitura achou o template; corpo = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID != "id-de-lembrete_consulta" {
		t.Errorf("id = %q — a releitura tem de suprir o id que a Meta nao mandou", resp.ID)
	}
}

// Meta RESPONDED: there is no ambiguity to resolve, and re-reading the
// catalog here would only spend the instance's quota. A re-read here would
// also confuse the log, which would start saying "no verdict" about a
// verdict that arrived.
func TestCreateTemplateRefusedByMetaDoesNotREREADTheCatalog(t *testing.T) {
	m := metaWithCatalogOf()
	m.createStatus = http.StatusBadRequest
	m.createBody = `{"error":{"message":"nome invalido","code":100}}`
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"n",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	if n := m.calls.Load(); n != 1 {
		t.Errorf("houve %d chamadas a Meta, quero 1 — a recusa dela nao e desfecho ambiguo", n)
	}
}

// T-153: the HEART of the task. The case that triggered it was exactly
// this — a consumer got a deterministic 503 (meta 2) on the CREATION of a
// template, with no error_data, and asked in writing for the fields T-141's
// cut did not catch. Before this task, this route (respondCreationError)
// called respondError (without the new fields) even holding a complete
// *meta.MetaError in hand — if this test only passed for sending and not
// here, the case that opened the task would remain unanswered.
func TestCreateTemplateRefusedByMetaPassesThroughSubcodeExplanationAndTrace(t *testing.T) {
	m := metaWithCatalogOf()
	m.createStatus = http.StatusServiceUnavailable
	m.createBody = `{"error":{"message":"An unknown error has occurred","code":2,` +
		`"error_subcode":2494055,"error_user_title":"Erro temporario",` +
		`"error_user_msg":"Tente novamente em alguns instantes","fbtrace_id":"AbCdEfGhIjKlMnOp"}}`
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"n",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503; corpo = %s", rec.Code, rec.Body.String())
	}

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
	if n := m.calls.Load(); n != 1 {
		t.Errorf("houve %d chamadas a Meta, quero 1 — a recusa dela nao e desfecho ambiguo", n)
	}
}

// The SAME guarantee above, for READING (GET /v1/templates,
// respondCatalogError) — the other half of the templates route that also
// has its own error body.
func TestListTemplatesWithMetaErrorPassesThroughSubcodeExplanationAndTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"An unknown error has occurred","code":2,` +
			`"error_subcode":2494055,"error_user_title":"Erro temporario",` +
			`"error_user_msg":"Tente novamente em alguns instantes","fbtrace_id":"AbCdEfGhIjKlMnOp"}}`))
	}))
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewTemplatesHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503; corpo = %s", rec.Code, rec.Body.String())
	}

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
}

// NO REGRESSION on the templates route: without the new fields, the error
// body stays without them — omitempty holds here just as much as on the
// sending route.
func TestCreateTemplateRefusedByMetaWithoutTheNewFieldsDoesNotInventThem(t *testing.T) {
	m := metaWithCatalogOf()
	m.createStatus = http.StatusBadRequest
	m.createBody = `{"error":{"message":"nome invalido","code":100}}`
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"n",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	body := rec.Body.String()
	for _, field := range []string{"subcodigo_meta", "explicacao_meta", "rastro_meta"} {
		if strings.Contains(body, field) {
			t.Fatalf("corpo = %q — %s NAO pode aparecer quando a Meta nao mandou o campo de origem", body, field)
		}
	}
}

// Every HTTP handler in this project serves each request in its own
// goroutine over the SAME handler; without a concurrent test, `-race` has
// nothing to detect (it has already cost a Critical, see docs/ARMADILHAS.md).
func TestTemplatesConcurrentDoesNotShareState(t *testing.T) {
	const calls = 30
	m := metaWithCatalogOf([]string{"t1", "t2"}, []string{"t3", "t4"})
	h := testTemplatesHandler(t, m, "lojinha")

	var wg sync.WaitGroup
	codes := make([]int, calls)
	totals := make([]int, calls)
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := askTemplates(t, h, "token-do-a", "?instancia=lojinha")
			codes[i] = rec.Code
			var resp testTemplatesResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			totals[i] = len(resp.Templates)
		}()
	}
	wg.Wait()

	for i := range calls {
		if codes[i] != http.StatusOK {
			t.Fatalf("chamada %d: status = %d, quero 200", i, codes[i])
		}
		if totals[i] != 4 {
			t.Fatalf("chamada %d: templates = %d, quero 4", i, totals[i])
		}
	}
}

// The CEILING that goes into the CONTRACT, in writing, in seconds (T-101,
// item 2): the sum of RereadWaits's pauses. Fixed number on purpose —
// it's the number docs/CONTRATO-CONSUMIDOR.md promises for the consumer to
// size its client's timeout with FACT instead of estimate; if it changes
// here without changing there, the doc lies.
func TestRereadWaitCapIs17Seconds(t *testing.T) {
	if RereadWaitCap != 17*time.Second {
		t.Fatalf("RereadWaitCap = %v, quero 17s (2+5+10) — este numero e o que o contrato promete",
			RereadWaitCap)
	}
}

// waitWithContext is the REAL function behind waitReread (the tests
// above swap it for fakeWaits, which never sleeps). Here the target
// is the PRIMITIVE itself: it has to respect the request's context, never
// sleep blindly with time.Sleep. Small durations (tens of ms) on purpose —
// this test really DOES sleep, but for a short enough time to not weigh down
// the suite.
func TestWaitWithContextStopsEarlyIfTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()

	start := time.Now()
	waitWithContext(ctx, 300*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed >= 150*time.Millisecond {
		t.Fatalf("esperou %v com o context cancelado aos 15ms — nao esta parando por causa do ctx.Done(); "+
			"se o consumidor desistir da requisicao, o gateway NAO pode continuar dormindo ate o fim da pausa",
			elapsed)
	}
}

func TestWaitWithContextWaitsTheRequestedTimeWithoutCancellation(t *testing.T) {
	start := time.Now()
	waitWithContext(context.Background(), 20*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < 20*time.Millisecond {
		t.Fatalf("esperou so %v, quero pelo menos 20ms — sem cancelamento a pausa tem de acontecer inteira", elapsed)
	}
}

// testTemplateCreatedResponse mirrors templateCreatedResponse for T-108's
// tests to read `categoria_pedida` and `categoria` alongside `aviso`.
type testTemplateCreatedResponse struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	Category          string `json:"category"`
	RequestedCategory string `json:"requested_category"`
	Warning           string `json:"aviso"`
}

// T-108, VERIFY (a): REQUESTED category and RECORDED category equal — no
// new warning can appear, and the warning has to be EXACTLY today's
// (compared by equality, not Contains, so the mutation of removing the
// comparison does not slip through by accident when the two sides are
// already equal).
func TestCreateTemplateEqualCategoryDoesNotWarnOfAChange(t *testing.T) {
	m := metaWithCatalogOf()
	m.createBody = `{"id":"1234","status":"PENDING","category":"UTILITY"}`
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"lembrete_consulta",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201; corpo = %s", rec.Code, rec.Body.String())
	}
	var resp testTemplateCreatedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if resp.RequestedCategory != "UTILITY" {
		t.Errorf("categoria_pedida = %q, quero UTILITY (o que foi pedido)", resp.RequestedCategory)
	}
	if resp.Warning != WarningTemplatePending {
		t.Errorf("aviso = %q, quero EXATAMENTE WarningTemplatePending — categories iguais nao podem "+
			"acrescentar nada ao aviso de hoje", resp.Warning)
	}
}

// T-108, VERIFY (b): Meta RECORDS a category different from the one
// REQUESTED — the real case from consumer-b (2026-07-30): they submitted
// UTILITY and Meta recorded MARKETING, no error, no warning from Meta. The
// new warning has to cite BOTH categories, and `categoria_pedida` has to
// carry the REQUESTED one (not the recorded one).
func TestCreateTemplateChangedCategoryWarns(t *testing.T) {
	m := metaWithCatalogOf()
	m.createBody = `{"id":"1234","status":"PENDING","category":"MARKETING"}`
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"instagram_continuar",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201; corpo = %s", rec.Code, rec.Body.String())
	}
	var resp testTemplateCreatedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if resp.RequestedCategory != "UTILITY" {
		t.Errorf("categoria_pedida = %q, quero UTILITY (o que o consumidor pediu, nao o que a Meta gravou)",
			resp.RequestedCategory)
	}
	if resp.Category != "MARKETING" {
		t.Errorf("categoria = %q, quero MARKETING (o que a Meta gravou)", resp.Category)
	}
	if !strings.Contains(resp.Warning, "UTILITY") || !strings.Contains(resp.Warning, "MARKETING") {
		t.Errorf("o aviso nao cita as duas categories (pedida e gravada): %q", resp.Warning)
	}
	lower := strings.ToLower(resp.Warning)
	if !strings.Contains(lower, "nao e erro") {
		t.Errorf("o aviso nao diz que a troca NAO e erro: %q", resp.Warning)
	}
	if !strings.Contains(lower, "nao desfaz") {
		t.Errorf("o aviso nao diz que o gateway NAO desfaz a troca: %q", resp.Warning)
	}
	if resp.Warning == WarningTemplatePending {
		t.Errorf("o aviso ficou EXATAMENTE igual ao de sempre — a troca de categoria tem de acrescentar texto")
	}
}

// T-108, VERIFY (c): the category swap also has to warn on the RE-READ path
// (T-101/T-078) — and that is precisely where the consumer has the least
// chance of noticing, because it is already reading a warning about the
// ambiguous creation. rawTemplateWithLanguage (used by
// createTemplateWithAmbiguousOutcome) always returns "category":"UTILITY" in
// the catalog, so requesting MARKETING guarantees the difference.
func TestCreateTemplateAmbiguousRereadWithChangedCategoryWarns(t *testing.T) {
	seenSet := fakeWaits(t)
	m := metaWithCatalogOf()
	m.abortCreate = true
	m.createEvenWhenAborting = true
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"pedido_avaliacao_v2",`+
		`"categoria":"MARKETING","idioma":"pt_BR",`+
		`"componentes":[{"type":"BODY","text":"Ola {{1}}, sua consulta e amanha."}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 — a releitura achou o template; corpo = %s", rec.Code, rec.Body.String())
	}
	if len(*seenSet) != 0 {
		t.Fatalf("waitReread foi chamado mesmo achando na 1a tentativa: %v", *seenSet)
	}
	var resp testTemplateCreatedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if resp.RequestedCategory != "MARKETING" {
		t.Errorf("categoria_pedida = %q, quero MARKETING (o que foi pedido)", resp.RequestedCategory)
	}
	if resp.Category != "UTILITY" {
		t.Errorf("categoria = %q, quero UTILITY (o que o catalogo tem)", resp.Category)
	}
	if !strings.Contains(resp.Warning, "MARKETING") || !strings.Contains(resp.Warning, "UTILITY") {
		t.Errorf("o aviso da releitura nao cita as duas categories: %q", resp.Warning)
	}
	if !strings.Contains(resp.Warning, WarningCreationConfirmedByReread) {
		t.Errorf("o aviso da releitura perdeu o aviso de sempre sobre a criacao confirmada: %q", resp.Warning)
	}
}

// T-108, VERIFY (d): a difference of ONLY case/spacing is not a category
// swap — "utility" requested and "UTILITY" recorded are the SAME category,
// and the warning CANNOT grow because of spelling.
func TestCreateTemplateCategoryDifferingOnlyInCaseDoesNotWarnOfAChange(t *testing.T) {
	m := metaWithCatalogOf()
	m.createBody = `{"id":"1234","status":"PENDING","category":"UTILITY"}`
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"lembrete_consulta",`+
		`"categoria":"  utility  ","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201; corpo = %s", rec.Code, rec.Body.String())
	}
	var resp testTemplateCreatedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%q)", err, rec.Body.String())
	}
	if resp.Warning != WarningTemplatePending {
		t.Errorf("aviso = %q, quero EXATAMENTE WarningTemplatePending — diferenca de caixa/espaco nao e "+
			"troca de categoria", resp.Warning)
	}
}

// T-108, VERIFY (e): whoever does NOT send `allow_category_change` in the
// request has to see the body that goes out to Meta EXACTLY as it was before
// this task — without the key. This is the test the `*bool` -> `bool`
// mutation has to kill: with a plain `bool`, absence and `false` become the
// SAME value, and the gateway's only way to know nobody asked for anything
// stops existing.
func TestCreateTemplateWithoutAllowCategoryChangeSendsNoSuchField(t *testing.T) {
	m := metaWithCatalogOf()
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"lembrete_consulta",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201; corpo = %s", rec.Code, rec.Body.String())
	}
	_, bodies := m.seen()
	if len(bodies) != 1 {
		t.Fatalf("corpos enviados a Meta = %d, quero 1", len(bodies))
	}
	if strings.Contains(bodies[0], "allow_category_change") {
		t.Errorf("o corpo enviado a Meta contem allow_category_change sem o consumidor ter pedido nada: %s",
			bodies[0])
	}
}

// T-108, VERIFY (f): `allow_category_change` is PASSED THROUGH VERBATIM —
// the gateway does not validate it, does not interpret it, does not
// translate it.
func TestCreateTemplateWithAllowCategoryChangePassesItThroughVerbatim(t *testing.T) {
	cases := []struct {
		value string
		wait  string
	}{
		{"false", `"allow_category_change":false`},
		{"true", `"allow_category_change":true`},
	}
	for _, c := range cases {
		m := metaWithCatalogOf()
		h := testTemplatesHandler(t, m, "lojinha")

		rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"lembrete_consulta",`+
			`"categoria":"UTILITY","idioma":"pt_BR","allow_category_change":`+c.value+`,`+
			`"componentes":[{"type":"BODY","text":"oi"}]}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("valor %s: status = %d, quero 201; corpo = %s", c.value, rec.Code, rec.Body.String())
		}
		_, bodies := m.seen()
		if len(bodies) != 1 {
			t.Fatalf("valor %s: corpos enviados a Meta = %d, quero 1", c.value, len(bodies))
		}
		if !strings.Contains(bodies[0], c.wait) {
			t.Errorf("valor %s: o corpo enviado a Meta nao contem %s: %s", c.value, c.wait, bodies[0])
		}
	}
}

// ============================================================================
// DELETE /v1/templates (T-173)
// ============================================================================

// testDeletionResponse mirrors the WIRE names, not the package struct, on
// purpose: the JSON field names ARE the contract with the consumer, and a
// test that reused templateDeletedResponse would rename them along with the
// code and never notice.
type testDeletionResponse struct {
	Instance string `json:"instance"`
	Name     string `json:"name"`
	Outcome  string `json:"outcome"`
	Entries  []struct {
		ID       string `json:"id"`
		Language string `json:"language"`
		Category string `json:"category"`
		Status   string `json:"status"`
	} `json:"entradas"`
	Warning     string `json:"aviso"`
	Rereads     int    `json:"releituras"`
	WaitSeconds int    `json:"espera_segundos"`
}

func readDeletion(t *testing.T, rec *httptest.ResponseRecorder) testDeletionResponse {
	t.Helper()
	var r testDeletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	return r
}

func counterOf(t *testing.T, store *config.Store, slug, key string) int {
	t.Helper()
	now := time.Now()
	n, err := store.CountersBetween(slug, now, now)
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	return n[key]
}

// OUTCOME 1 of 3: the template was there and Meta accepted the deletion.
//
// Checks the three things the consumer's cleanup report depends on: the WORD
// (`apagado`, not a bare `200 {}`), the LANGUAGES that went out with the name
// (Meta deletes by name in ALL languages — one line here would under-report
// what the call did), and the fact that the DELETE really carried the name.
func TestDeleteTemplateThatExistsAnswersDeletedWithTheLanguages(t *testing.T) {
	m := metaWithCatalogOf([]string{"outro"}, []string{"promo_julho"})
	// A SECOND language for the same name, on ANOTHER page: that is what the
	// "all languages" warning is talking about, and a handler that stopped at
	// the first match would pass without it.
	m.pages[0] = append(m.pages[0], rawTemplateWithLanguage("promo_julho", "APPROVED", "en_US"))
	m.deleteEffect = deletionRemoves
	h, store := templatesHandlerWithStore(t, m, "lojinha")

	rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=lojinha&nome=promo_julho")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	r := readDeletion(t, rec)
	if r.Outcome != OutcomeDeleted {
		t.Errorf("desfecho = %q, quero %q", r.Outcome, OutcomeDeleted)
	}
	if r.Name != "promo_julho" || r.Instance != "lojinha" {
		t.Errorf("nome = %q e instancia = %q", r.Name, r.Instance)
	}
	if len(r.Entries) != 2 {
		t.Fatalf("entradas = %d, quero 2 (pt_BR e en_US) — a Meta apaga o nome em TODOS os idiomas,"+
			" e listar so um deixa o relatorio do consumidor curto: %s", len(r.Entries), rec.Body.String())
	}
	languages := map[string]bool{}
	for _, e := range r.Entries {
		languages[e.Language] = true
		if e.ID == "" {
			t.Errorf("entrada %+v sem id — e o que o consumidor tinha guardado", e)
		}
	}
	if !languages["pt_BR"] || !languages["en_US"] {
		t.Errorf("idiomas devolvidos = %v, quero pt_BR e en_US", languages)
	}
	if !strings.Contains(r.Warning, "30 dias") {
		t.Errorf("o aviso nao fala dos 30 dias em que a Meta nao reaceita o nome: %q", r.Warning)
	}
	if strings.Contains(r.Warning, StatusPendingDeletion) {
		t.Errorf("o aviso de %s viajou numa exclusao que SUMIU do catalogo: %q", StatusPendingDeletion, r.Warning)
	}

	urls, _ := m.seen()
	var deleted bool
	for _, u := range urls {
		if strings.Contains(u, "name=promo_julho") {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("o DELETE nao levou o nome na query: %v", urls)
	}
	if n := counterOf(t, store, "lojinha", config.CounterTemplatesDeleted); n != 1 {
		t.Errorf("%s = %d, quero 1 — sem o contador, uma limpeza em serie e invisivel no /v1/estado",
			config.CounterTemplatesDeleted, n)
	}
}

// OUTCOME 2 of 3: the name is not in the catalog.
//
// TWO guarantees, and the second is what makes the route idempotent IN FACT:
// the word `ja_nao_existia` (a cleanup gets interrupted and resumed, and has
// to tell "I deleted it now" from "it was already gone") and NO CALL to Meta
// — asking it to delete something that is not there spends the instance's
// quota to receive an error about something already correct.
func TestDeleteTemplateThatDoesNotExistAnswersDidNotExistWithoutCallingDelete(t *testing.T) {
	m := metaWithCatalogOf([]string{"outro_qualquer"})
	h, store := templatesHandlerWithStore(t, m, "lojinha")

	rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=lojinha&nome=nunca_existiu")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	r := readDeletion(t, rec)
	if r.Outcome != OutcomeDidNotExist {
		t.Errorf("desfecho = %q, quero %q", r.Outcome, OutcomeDidNotExist)
	}
	if !strings.Contains(rec.Body.String(), `"entradas":[]`) {
		t.Errorf("entradas tem de sair como `[]`, nunca `null` — dois vazios diferentes quebram o parser"+
			" do consumidor: %s", rec.Body.String())
	}
	if len(r.Entries) != 0 {
		t.Errorf("entradas = %+v, quero vazio", r.Entries)
	}
	// The 30-day warning does NOT travel here: the gateway deleted nothing
	// and does not know whether the name ever existed. Saying it is burned
	// would be inventing a restriction.
	if strings.Contains(r.Warning, "30 dias") {
		t.Errorf("o aviso de nome queimado viajou num %s, onde nada foi apagado: %q", OutcomeDidNotExist, r.Warning)
	}

	urls, _ := m.seen()
	for _, u := range urls {
		if strings.Contains(u, "name=") {
			t.Errorf("houve DELETE para um nome que nao estava no catalogo: %v", urls)
		}
	}
	if n := counterOf(t, store, "lojinha", config.CounterTemplatesDeleted); n != 0 {
		t.Errorf("%s = %d, quero 0 — nada foi apagado", config.CounterTemplatesDeleted, n)
	}
}

// OUTCOME 3 of 3: the DELETE died with NO response and the catalog re-read
// STILL shows the template, alive.
//
// The word is INCONCLUSIVO, with the same `502` and the same `desconhecido`
// class as the ambiguous creation (T-078/T-101). "I didn't see it happen" is
// not "it didn't happen".
func TestDeleteTemplateWithoutVerdictAndTemplateStillAliveAnswers502Inconclusive(t *testing.T) {
	seenSet := fakeWaits(t)
	m := metaWithCatalogOf([]string{"promo_julho"})
	m.abortDelete = true
	m.deleteEffect = deletionNoEffect
	h, store := templatesHandlerWithStore(t, m, "lojinha")

	rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=lojinha&nome=promo_julho")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502; corpo = %s", rec.Code, rec.Body.String())
	}
	var errBody errorResponseWithReread
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("corpo nao desserializa: %v (%s)", err, rec.Body.String())
	}
	if errBody.Error.Class != string(meta.ClassUnknown) {
		t.Errorf("classe = %q, quero %q", errBody.Error.Class, meta.ClassUnknown)
	}
	if !strings.Contains(errBody.Error.Message, "INCONCLUSIVO") {
		t.Errorf("a mensagem nao usa a palavra INCONCLUSIVO: %q", errBody.Error.Message)
	}
	if errBody.Error.Rereads != len(RereadWaits)+1 {
		t.Errorf("releituras = %d, quero %d — uma imediata mais uma por pausa",
			errBody.Error.Rereads, len(RereadWaits)+1)
	}
	if len(*seenSet) != len(RereadWaits) {
		t.Errorf("pausas = %v, quero %v", *seenSet, RereadWaits)
	}
	if n := counterOf(t, store, "lojinha", config.CounterTemplatesDeleted); n != 0 {
		t.Errorf("%s = %d, quero 0 — desfecho inconclusivo nao conta como apagado",
			config.CounterTemplatesDeleted, n)
	}
}

// 🔴 THE CORRECTION THAT KEEPS `inconclusivo` FROM LYING: still in the
// catalog is NOT "it was not deleted".
//
// Verbatim from the source (read on 2026-08-28): "If you delete a template
// that has been sent in a template message but has yet to be delivered [...]
// the template's status is set to PENDING_DELETION and WhatsApp attempts
// delivery for 30 days."
// https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management
//
// Without this test the obvious rule ("still there = doubt") passes green and
// puts a correctly deleted template into the consumer's "I don't know" pile —
// which on a 61-name cleanup is the pile it has to work through by hand.
func TestDeleteTemplateWithoutVerdictButPendingDeletionAnswersDeleted(t *testing.T) {
	fakeWaits(t)
	m := metaWithCatalogOf([]string{"promo_julho"})
	m.abortDelete = true
	m.deleteEffect = deletionMarksPending
	h, store := templatesHandlerWithStore(t, m, "lojinha")

	rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=lojinha&nome=promo_julho")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	r := readDeletion(t, rec)
	if r.Outcome != OutcomeDeleted {
		t.Fatalf("desfecho = %q, quero %q — %s e exclusao ACEITA pela Meta, nao duvida; corpo = %s",
			r.Outcome, OutcomeDeleted, StatusPendingDeletion, rec.Body.String())
	}
	// The consumer is going to SEE the template in its own catalog. If the
	// response does not say why, the only reading left to it is "it did not
	// work".
	if !strings.Contains(r.Warning, StatusPendingDeletion) {
		t.Errorf("o aviso nao explica por que o template continua no catalogo: %q", r.Warning)
	}
	if r.Rereads < 1 {
		t.Errorf("releituras = %d, quero >= 1 — o desfecho foi reconstruido pela releitura", r.Rereads)
	}
	if n := counterOf(t, store, "lojinha", config.CounterTemplatesDeleted); n != 1 {
		t.Errorf("%s = %d, quero 1", config.CounterTemplatesDeleted, n)
	}
}

// 🔴 THE GUARD THAT MAKES A WILDCARD IMPOSSIBLE, and the assertion that
// matters is the SECOND one: Meta is never called.
//
// The deletion is by name and has no undo. If Meta ever accepted a pattern,
// ONE call would take out a whole family of templates and nothing on either
// side of this gateway could put them back.
func TestDeleteTemplateRefusesNameWithWildcardBEFORECallingMeta(t *testing.T) {
	invalid := []string{"*", "promo_*", "promo%", "promo julho", "Promo_Julho", "promo.julho",
		"promo-julho", "../outro", strings.Repeat("a", 513)}
	for _, name := range invalid {
		m := metaWithCatalogOf([]string{"promo_julho"})
		h := testTemplatesHandler(t, m, "lojinha")

		rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=lojinha&nome="+url.QueryEscape(name))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("nome %q: status = %d, quero 400; corpo = %s", name, rec.Code, rec.Body.String())
		}
		if n := m.calls.Load(); n != 0 {
			t.Errorf("nome %q: a Meta foi chamada %d vez(es) numa recusa que tem de acontecer ANTES do fio",
				name, n)
		}
	}
}

func TestDeleteTemplateRequiresInstanceAndName(t *testing.T) {
	cases := []struct{ name, query string }{
		{"sem instancia", "?nome=promo_julho"},
		{"sem nome", "?instancia=lojinha"},
		{"nome vazio", "?instancia=lojinha&nome="},
		{"nome so com espaco", "?instancia=lojinha&nome=%20%20"},
	}
	for _, c := range cases {
		m := metaWithCatalogOf([]string{"promo_julho"})
		h := testTemplatesHandler(t, m, "lojinha")

		rec := callDeleteTemplate(t, h, "token-do-a", c.query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quero 400; corpo = %s", c.name, rec.Code, rec.Body.String())
		}
		if n := m.calls.Load(); n != 0 {
			t.Errorf("%s: a Meta foi chamada %d vez(es) num pedido malformado", c.name, n)
		}
	}
}

// A `2xx` from Meta does NOT prove the deletion happened: the documented body
// is `{"success": bool}`, and anything that is not an explicit `true` is a
// refusal, never a silent success. It falls into the SAME ambiguous handling
// as a dead transport (the same rule ErrTemplateWithoutID follows on the
// creation), so with the template still alive in the catalog the outcome is
// the inconclusive `502` — and, above all, NOT a `200 apagado`.
func TestDeleteTemplateWithSuccessFalseDoesNotAnswerDeleted(t *testing.T) {
	for _, body := range []string{`{"success":false}`, `{}`, `null`, `nao e json`} {
		fakeWaits(t)
		m := metaWithCatalogOf([]string{"promo_julho"})
		m.deleteBody = body
		m.deleteEffect = deletionNoEffect
		h, store := templatesHandlerWithStore(t, m, "lojinha")

		rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=lojinha&nome=promo_julho")
		if rec.Code == http.StatusOK {
			t.Errorf("corpo %q: status 200 — `success` que nao e `true` virou sucesso silencioso: %s",
				body, rec.Body.String())
			continue
		}
		if rec.Code != http.StatusBadGateway {
			t.Errorf("corpo %q: status = %d, quero 502; corpo = %s", body, rec.Code, rec.Body.String())
		}
		if n := counterOf(t, store, "lojinha", config.CounterTemplatesDeleted); n != 0 {
			t.Errorf("corpo %q: %s = %d, quero 0", body, config.CounterTemplatesDeleted, n)
		}
	}
}

// Meta RESPONDED with an error: there is no ambiguity to resolve, and the
// class it sent is what reaches the consumer. This is also the branch that
// carries the refusals documented for this edge (a disabled template cannot
// be deleted) — the gateway does NOT pre-validate status for them, so what
// the consumer reads is what Meta said, not our guess at what it would say.
func TestDeleteTemplateWithMetaErrorPassesThroughTheClass(t *testing.T) {
	m := metaWithCatalogOf([]string{"promo_julho"})
	m.deleteStatus = http.StatusBadRequest
	m.deleteBody = `{"error":{"message":"template is disabled","code":100}}`
	h := testTemplatesHandler(t, m, "lojinha")

	rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=lojinha&nome=promo_julho")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	var errBody errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error.Class != string(meta.ClassPermanent) {
		t.Errorf("classe = %q, quero %q", errBody.Error.Class, meta.ClassPermanent)
	}
}

// T-111: an Instagram instance has no WabaID, so it has no catalog to delete
// from. The refusal comes from WhatsAppOnly, BEFORE any call to Meta.
func TestDeleteTemplateRefusesInstagramInstanceWith400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	m := metaWithCatalogOf() // counts calls: any of them fails the test
	srv := m.server(t)
	h := NewTemplatesHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	rec := callDeleteTemplate(t, h, "token-do-a", "?instancia=insta-loja&nome=promo_julho")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	if n := m.calls.Load(); n != 0 {
		t.Errorf("a Meta foi chamada %d vez(es) numa recusa que tinha de acontecer ANTES do fio", n)
	}
}
