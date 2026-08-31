package inbound

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// payloadWithTextNameAndCaption is T-091's Verify (b): ONE webhook carrying
// the THREE kinds of data the transit log must not store — message text,
// contact name (contacts[].profile.name) and media caption
// (document.caption). Each value is a UNIQUE sentinel, so the test's
// substring search can't collide with any other stored field (slug, tipo,
// desfecho, correlacao).
func payloadWithTextNameAndCaption() []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "contacts":[{"profile":{"name":"NOME-SENTINELA-DO-CONTATO"},"wa_id":"5511999990000"}],
	   "messages":[
	     {"from":"5511999990000","id":"wamid.TEXTO1","timestamp":"1769000000",
	      "type":"text","text":{"body":"TEXTO-SENTINELA-DA-MENSAGEM"}},
	     {"from":"5511999990000","id":"wamid.DOC1","timestamp":"1769000001",
	      "type":"document","document":{"id":"MEDIA1","mime_type":"application/pdf",
	      "sha256":"x","caption":"LEGENDA-SENTINELA-DO-DOCUMENTO","filename":"NOMEARQUIVO-SENTINELA.pdf"}}
	   ]}}]}]}`)
}

// TestTransitStoresNoContent is Verify (b): it sweeps EVERY column of
// EVERY `transito` row this webhook wrote and proves that NONE of the
// three sentinels shows up in ANY of them — not just in the columns the
// code declares today, because it's precisely a new, forgotten column that
// this test exists to catch.
//
// 🔴 NARROWED on 2026-07-30 (T-094): the PHONE NUMBER ("5511999990000," in
// `from`/`wa_id` above) and the WAMID ("wamid.TEXTO1," "wamid.DOC1") were
// NEVER in the `sentinels` list below — T-091 already protected them
// through a DIFFERENT mechanism (HMAC, not this sentinel sweep), and the
// phone number was never one of the four CONTENT types this test always
// forbade (message text, contact name, caption, filename). So the
// `sentinels` list hasn't changed a single line — what changed is that,
// since T-094 (the owner's decision: "the number can go in, it isn't a
// secret"), the phone number and the wamid NOW APPEAR in plaintext in the
// table's `contraparte`/`wamid` column, and the assertion below proves
// this on purpose, so the next reading of this file doesn't confuse "was
// never forbidden" with "doesn't appear."
func TestTransitStoresNoContent(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithCap(t, consumer.URL, 1<<20)
	activateInstanceInFile(t, path, "lojinha")

	raw := payloadWithTextNameAndCaption()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco para conferir: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT * FROM transito WHERE slug = 'lojinha'`)
	if err != nil {
		t.Fatalf("consultar transito: %v", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("listar colunas: %v", err)
	}

	sentinels := []string{
		"TEXTO-SENTINELA-DA-MENSAGEM",
		"NOME-SENTINELA-DO-CONTATO",
		"LEGENDA-SENTINELA-DO-DOCUMENTO",
		"NOMEARQUIVO-SENTINELA",
	}

	nRows := 0
	for rows.Next() {
		nRows++
		// GENERIC Scan: []any of *any, to sweep ANY column present in the
		// table today — including one this test doesn't know the name of,
		// if someone adds it without updating this file.
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("ler linha de transito: %v", err)
		}
		for i, v := range values {
			text := fmt.Sprintf("%v", v)
			for _, sentinel := range sentinels {
				if strings.Contains(text, sentinel) {
					t.Fatalf("coluna %q da linha de transito contem a sentinela %q — conteudo vazou para o log de transito",
						columns[i], sentinel)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar transito: %v", err)
	}
	if nRows == 0 {
		t.Fatal("nenhuma linha de transito foi gravada — o teste nao exercitou o caminho que ele prova")
	}

	// POSITIVE, on purpose (T-094): the payload's phone number and wamid
	// HAVE to show up in plaintext in `contraparte`/`wamid` — the total
	// absence of a sentinel above does NOT prove this by itself (the table
	// could have stayed entirely empty and the test would still be
	// "green"). Without this assertion, a future change that silently
	// reintroduced HMAC would sail right over the rest of the test without
	// flagging anything.
	var counterpart, wamid string
	if err := db.QueryRow(`SELECT contraparte, wamid FROM transito WHERE slug = 'lojinha' AND tipo = 'message'`).
		Scan(&counterpart, &wamid); err != nil {
		t.Fatalf("ler contraparte/wamid da linha de mensagem: %v", err)
	}
	if counterpart != "5511999990000" {
		t.Fatalf("contraparte = %q, quero o telefone em CLARO (decisao do dono, T-094)", counterpart)
	}
	if wamid != "wamid.TEXTO1" {
		t.Fatalf("wamid = %q, quero o wamid em CLARO (decisao do dono, T-094)", wamid)
	}
}

// alwaysFailingTransitStore is a config.TransitStore that fails on EVERY
// call — the double that proves, against the REAL handler, that a
// transit-log write failure never changes the status returned to Meta
// (Verify (c) of T-091). SAME technique as alwaysFailingCounter, above in
// this package.
type alwaysFailingTransitStore struct{ calls atomic.Int64 }

func (f *alwaysFailingTransitStore) WriteTransit(config.TransitRecord, time.Time) error {
	f.calls.Add(1)
	return errors.New("alwaysFailingTransitStore: falha proposital de teste")
}

// TestHandlerTransitFailureDoesNotChangeTheStatus is T-091's Verify (c).
//
// MANDATORY MUTATION (done and reverted during development, not
// committed): swapping h.recordTransit for a call that HANDLES
// WriteTransit's error with http.Error instead of log.Printf makes this
// test go red — the same proof T-035 required for the counter, applied to
// the new writer.
func TestHandlerTransitFailureDoesNotChangeTheStatus(t *testing.T) {
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	store, err := config.OpenStore(t.TempDir()+"/t.db", vault)
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
	if err := store.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}

	fake := &alwaysFailingTransitStore{}
	h := NewHandler(store, NewDeliverer(nil), 1<<20, config.NewCounter(store), config.NewTransitWithStore(fake))

	raw := testPayload()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 — falha do TRANSITO nao pode mudar o veredito da ENTREGA", rec.Code)
	}
	if fake.calls.Load() == 0 {
		t.Fatal("o transito que sempre erra nunca foi chamado — o teste nao exercitou o caminho que ele prova")
	}
}
