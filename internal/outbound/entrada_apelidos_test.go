package outbound

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// --- T-203 (step 2 of T-189): English aliases on ENTRADA --------------------

// enTextBody is textBody (handler_test.go), same request, English key
// names. The VALUE of `kind` stays "texto" ON PURPOSE — this const exists
// to test the KEY alias in isolation. Since T-207 (step 2 of T-189, for
// VALUES) that no longer makes this body "fully in English": `kind` is
// English but its value is still the OLD Portuguese spelling, so this body
// now counts on config.CounterOldNameUsed too — see
// TestEntradaOldNameCounterCountsAndAppearsInEstado, updated by T-207, and
// enTextBodyFullyEnglish below for the body that carries NEITHER old
// spelling.
const enTextBody = `{"instance":"lojinha","to":"5511999990000","kind":"texto","text":"oi"}`

// enTextBodyFullyEnglish is enTextBody with the discriminator VALUE also
// translated ("texto" -> "text", section 8.1) — T-207's addition. This is
// the body that must NOT count on config.CounterOldNameUsed: neither its
// keys nor this one value carry an old spelling.
const enTextBodyFullyEnglish = `{"instance":"lojinha","to":"5511999990000","kind":"text","text":"oi"}`

// TestEntradaAcceptsEnglishAliasWithIdenticalResponse is the FIRST Verify
// item: the same request, written once in Portuguese and once in English,
// answers with the SAME body.
func TestEntradaAcceptsEnglishAliasWithIdenticalResponse(t *testing.T) {
	srv := acceptingMeta("wamid.PARIDADE")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	pt := ask(t, h, "token-do-a", "chave-pt-paridade", textBody)
	en := ask(t, h, "token-do-a", "chave-en-paridade", enTextBody)

	if pt.Code != http.StatusOK || en.Code != http.StatusOK {
		t.Fatalf("status PT=%d EN=%d — corpo PT=%s EN=%s", pt.Code, en.Code, pt.Body, en.Body)
	}
	if pt.Body.String() != en.Body.String() {
		t.Errorf("respostas diferentes:\nPT: %s\nEN: %s", pt.Body, en.Body)
	}
}

// TestEntradaCreateTemplateAcceptsEnglishAliasWithIdenticalResponse: the
// SAME parity proof on a second route (POST /v1/templates), because the
// Verify item asks for "each route that accepts input", not only the send.
func TestEntradaCreateTemplateAcceptsEnglishAliasWithIdenticalResponse(t *testing.T) {
	m := &fakeTemplateMeta{createStatus: http.StatusOK, createBody: `{"id":"1234","status":"PENDING","category":"UTILITY"}`}
	h := testTemplatesHandler(t, m, "lojinha")

	pt := `{"instancia":"lojinha","nome":"promo","categoria":"UTILITY","idioma":"pt_BR","componentes":[]}`
	en := `{"instance":"lojinha","name":"promo","category":"UTILITY","language":"pt_BR","components":[]}`

	rPT := createTemplate(t, h, "token-do-a", pt)
	rEN := createTemplate(t, h, "token-do-a", en)

	if rPT.Code != rEN.Code {
		t.Fatalf("status PT=%d EN=%d — corpo PT=%s EN=%s", rPT.Code, rEN.Code, rPT.Body, rEN.Body)
	}
	if rPT.Body.String() != rEN.Body.String() {
		t.Errorf("respostas diferentes:\nPT: %s\nEN: %s", rPT.Body, rEN.Body)
	}
}

// TestEntradaIdempotencyCrossesLanguages is T-203's central test — Do item
// 5 and the Why line say it plainly: if idempotency hashed the RAW body
// instead of the CANONICAL form, the SAME request written once in
// Portuguese and once in English would hash differently, and the SAME
// message would go out TWICE to the customer's phone. This test sends the
// identical request in both languages, under the SAME Idempotency-Key, and
// requires Meta to have been called exactly once.
func TestEntradaIdempotencyCrossesLanguages(t *testing.T) {
	var sends int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sends++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.IDIOMAS"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	first := ask(t, h, "token-do-a", "mesma-chave-dois-idiomas", textBody)
	second := ask(t, h, "token-do-a", "mesma-chave-dois-idiomas", enTextBody)

	if sends != 1 {
		t.Fatalf("a Meta recebeu %d envios para o MESMO pedido em dois idiomas sob a MESMA "+
			"Idempotency-Key — quero 1: acima disso e a mesma mensagem saindo duas vezes "+
			"para a cliente", sends)
	}
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d (PT) e %d (EN), quero 200 nos dois — corpo PT=%s EN=%s",
			first.Code, second.Code, first.Body, second.Body)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("respostas diferentes entre PT e EN sob a mesma chave:\nPT: %s\nEN: %s",
			first.Body, second.Body)
	}
	var r1, r2 struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &r1)
	_ = json.Unmarshal(second.Body.Bytes(), &r2)
	if r1.WaMessageID != "wamid.IDIOMAS" || r2.WaMessageID != "wamid.IDIOMAS" {
		t.Fatalf("wa_message_id PT=%q EN=%q, quero os dois iguais a wamid.IDIOMAS",
			r1.WaMessageID, r2.WaMessageID)
	}
}

// --- Conflict: both spellings of the same key in the same request -----------

// requestConflictBody builds a minimal POST /v1/messages body carrying BOTH
// spellings of ONE field — translateRequestBody runs BEFORE any shape
// validation, so the rest of the body does not need to be a valid message.
func requestConflictBody(pt, en string) string {
	if pt == "instancia" {
		// `instancia` is already present as the base identifier of every
		// other case; carrying it twice under the SAME literal key would
		// test a JSON duplicate key, not a PT/EN conflict.
		return `{"instancia":"x","instance":"y"}`
	}
	return `{"instancia":"lojinha","` + pt + `":"x","` + en + `":"y"}`
}

func assertConflictRejected(t *testing.T, ask func(body string) *httptest.ResponseRecorder, pt, en string) {
	t.Helper()
	rec := ask(requestConflictBody(pt, en))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo: %s)", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, pt) || !strings.Contains(body, en) {
		t.Errorf("a mensagem de erro nao nomeia as duas chaves (%q e %q): %s", pt, en, body)
	}
}

// TestEntradaConflictingAliasIsRejectedForEveryRequestTopLevelKey is the
// Verify item "Conflito devolve 400 nomeando a chave. Teste por chave, nao
// so uma amostra" — applied to EVERY top-level ENTRADA key of Request
// (mensagem.go), not a sample of them.
func TestEntradaConflictingAliasIsRejectedForEveryRequestTopLevelKey(t *testing.T) {
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a Meta foi CHAMADA com um pedido em conflito de apelido")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.NUNCA"}]}`))
	}))
	defer metaSrv.Close()
	h, _ := testHandler(t, metaSrv)

	i := 0
	for en, pt := range requestAliasAtTopLevel {
		en, pt := en, pt
		i++
		t.Run(pt, func(t *testing.T) {
			ask := func(body string) *httptest.ResponseRecorder {
				return ask(t, h, "token-do-a", "conflito-top-"+pt, body)
			}
			assertConflictRejected(t, ask, pt, en)
		})
	}
	if i != len(requestAliasAtTopLevel) {
		t.Fatalf("percorreu %d chaves, esperava %d", i, len(requestAliasAtTopLevel))
	}
}

// TestEntradaConflictingAliasIsRejectedForEveryNestedRequestKey covers the
// four nested objects Request carries an ENTRADA alias into: `cabecalho`,
// `reacao`, `localizacao` and `fluxo`. THE ALIAS IS POSITIONAL (T-203 Do
// item 2) — this is the test that would fail if translation ever became a
// generic top-of-tree rename.
func TestEntradaConflictingAliasIsRejectedForEveryNestedRequestKey(t *testing.T) {
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a Meta foi CHAMADA com um pedido em conflito de apelido")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.NUNCA"}]}`))
	}))
	defer metaSrv.Close()
	h, _ := testHandler(t, metaSrv)

	cases := []struct {
		outer string
		dict  map[string]string
	}{
		{"cabecalho", templateHeaderAlias},
		{"reacao", reactionAlias},
		{"localizacao", locationAlias},
		{"fluxo", flowAlias},
	}
	for _, c := range cases {
		for en, pt := range c.dict {
			outer, en, pt := c.outer, en, pt
			t.Run(outer+"."+pt, func(t *testing.T) {
				body := `{"instancia":"lojinha","` + outer + `":{"` + pt + `":"x","` + en + `":"y"}}`
				rec := ask(t, h, "token-do-a", "conflito-"+outer+"-"+pt, body)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, quero 400 (corpo: %s)", rec.Code, rec.Body)
				}
				b := rec.Body.String()
				if !strings.Contains(b, pt) || !strings.Contains(b, en) {
					t.Errorf("a mensagem de erro nao nomeia as duas chaves (%q e %q): %s", pt, en, b)
				}
			})
		}
	}
}

// TestEntradaConflictingAliasIsRejectedInsideEachTemplateButton covers
// `botoes_template[i]`, a LIST of TemplateButtonUnion — the alias applies
// to EACH ITEM, positionally, never to the list's own key.
func TestEntradaConflictingAliasIsRejectedInsideEachTemplateButton(t *testing.T) {
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a Meta foi CHAMADA com um pedido em conflito de apelido")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.NUNCA"}]}`))
	}))
	defer metaSrv.Close()
	h, _ := testHandler(t, metaSrv)

	for en, pt := range templateButtonAlias {
		en, pt := en, pt
		t.Run("botoes_template."+pt, func(t *testing.T) {
			body := `{"instancia":"lojinha","botoes_template":[{"` + pt + `":"x","` + en + `":"y"}]}`
			rec := ask(t, h, "token-do-a", "conflito-botao-"+pt, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, quero 400 (corpo: %s)", rec.Code, rec.Body)
			}
			b := rec.Body.String()
			if !strings.Contains(b, pt) || !strings.Contains(b, en) {
				t.Errorf("a mensagem de erro nao nomeia as duas chaves (%q e %q): %s", pt, en, b)
			}
		})
	}
}

// TestEntradaConflictingAliasIsRejectedOnCreateTemplate is the same
// coverage on POST /v1/templates (createTemplateAlias).
func TestEntradaConflictingAliasIsRejectedOnCreateTemplate(t *testing.T) {
	m := &fakeTemplateMeta{createStatus: http.StatusOK, createBody: `{"id":"1234","status":"PENDING","category":"UTILITY"}`}
	h := testTemplatesHandler(t, m, "lojinha")

	for en, pt := range createTemplateAlias {
		en, pt := en, pt
		t.Run(pt, func(t *testing.T) {
			var body string
			if pt == "instancia" {
				body = `{"instancia":"x","instance":"y"}`
			} else {
				body = `{"instancia":"lojinha","` + pt + `":"x","` + en + `":"y"}`
			}
			rec := createTemplate(t, h, "token-do-a", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, quero 400 (corpo: %s)", rec.Code, rec.Body)
			}
			b := rec.Body.String()
			if !strings.Contains(b, pt) || !strings.Contains(b, en) {
				t.Errorf("a mensagem de erro nao nomeia as duas chaves (%q e %q): %s", pt, en, b)
			}
		})
	}
}

// TestEntradaConflictingAliasIsRejectedOnRegistration is the same coverage
// on POST /v1/cadastro (registrationAlias) — and it doubles as the proof
// that the conflict path never echoes a VALUE: this body's siblings carry
// app_secret and token_envio, and the assertion only ever checks for the
// two KEY names.
func TestEntradaConflictingAliasIsRejectedOnRegistration(t *testing.T) {
	h, _, _, _ := testRegistration(t)

	for en, pt := range registrationAlias {
		en, pt := en, pt
		t.Run(pt, func(t *testing.T) {
			var body string
			if pt == "instancia" {
				body = `{"instancia":"x","instance":"y"}`
			} else {
				body = `{"instancia":"terceiro","` + pt + `":"x","` + en + `":"y"}`
			}
			rec := register(t, h, "token-do-a", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, quero 400 (corpo: %s)", rec.Code, rec.Body)
			}
			b := rec.Body.String()
			if !strings.Contains(b, pt) || !strings.Contains(b, en) {
				t.Errorf("a mensagem de erro nao nomeia as duas chaves (%q e %q): %s", pt, en, b)
			}
		})
	}
}

// TestEntradaConflictingAliasIsRejectedOnInstanceOnlyRoutes covers
// instanceOnlyAlias (POST /v1/pausa, POST/DELETE /v1/bloqueios,
// POST /v1/leituras, POST /v1/fumaca all share this ONE-key dictionary) —
// exercised once, via /v1/pausa, since the dictionary itself has a single
// entry and the OTHER three routes wire the exact same translateEntradaOrReject
// call with the exact same dict (proven by the full route-level tests
// below, which exercise the ACCEPT side on each of the four routes).
func TestEntradaConflictingAliasIsRejectedOnInstanceOnlyRoutes(t *testing.T) {
	h, _ := testPause(t)

	for en, pt := range instanceOnlyAlias {
		rec := askPause(t, h, "token-do-a", `{"`+pt+`":"x","`+en+`":"y"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, quero 400 (corpo: %s)", rec.Code, rec.Body)
		}
		b := rec.Body.String()
		if !strings.Contains(b, pt) || !strings.Contains(b, en) {
			t.Errorf("a mensagem de erro nao nomeia as duas chaves (%q e %q): %s", pt, en, b)
		}
	}
}

// --- The English alias is ACCEPTED (not just: PT/EN conflict refused) on
// every one of the four "instance-only" routes. ---------------------------

func TestEntradaAcceptsEnglishInstanceAliasOnPause(t *testing.T) {
	h, store := testPause(t)
	if err := store.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}
	rec := askPause(t, h, "token-do-a", `{"instance":"lojinha"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body)
	}
}

func TestEntradaAcceptsEnglishInstanceAliasOnBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewBlockHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	req := httptest.NewRequest(http.MethodPost, "/v1/bloqueios",
		strings.NewReader(`{"instance":"lojinha","telefones":["5511999990000"]}`))
	req.Header.Set("Authorization", "Bearer token-do-a")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body)
	}
}

func TestEntradaAcceptsEnglishInstanceAliasOnReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewReadsHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), AllTypes)

	req := httptest.NewRequest(http.MethodPost, "/v1/leituras",
		strings.NewReader(`{"instance":"lojinha","wamid":"wamid.ABC"}`))
	req.Header.Set("Authorization", "Bearer token-do-a")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body)
	}
}

func TestEntradaAcceptsEnglishInstanceAliasOnSmoke(t *testing.T) {
	srv := acceptingMeta("wamid.FUMACA")
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewSmokeHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), config.NewCounter(store), 1<<20, AllTypes)

	req := httptest.NewRequest(http.MethodPost, "/v1/fumaca",
		strings.NewReader(`{"instance":"lojinha","destino":"5511999990000"}`))
	req.Header.Set("Authorization", "Bearer token-do-a")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body)
	}
}

// --- T-203 Do item 6: the old-name counter, per instance, in /v1/estado ----

// TestEntradaOldNameCounterCountsAndAppearsInEstado is the last mandatory
// Verify item: the counter that authorizes step 4 has to (a) go up when a
// request still uses the Portuguese spelling — of a KEY, or (T-207) of a
// VALUE — (b) NOT go up when NEITHER is Portuguese anymore, and (c)
// actually surface in GET /v1/estado — the same store, read through the
// same route the consumer will watch.
//
// 🔴 T-207 changed the expected count from 1 to 2, and that is the whole
// point of Do item 5's own justification example: "a consumer sending
// `kind` (English key) with `tipo` still `\"texto\"` (Portuguese VALUE)
// would read as zero if the counter only looked at keys". enTextBody IS
// exactly that consumer — English key, Portuguese value — so it now counts
// too. enTextBodyFullyEnglish (T-207) is what proves the counter still
// stops incrementing once BOTH the key and the value are migrated.
func TestEntradaOldNameCounterCountsAndAppearsInEstado(t *testing.T) {
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	sendSrv := acceptingMeta("wamid.CONTADOR")
	defer sendSrv.Close()
	send := NewHandler(store, NewAuthenticator(store),
		meta.NewClient(sendSrv.Client(), sendSrv.URL), 1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)

	healthMeta := tokenAcceptingMeta()
	healthSrv := healthMeta.server(t)
	watchdog := NewWatchdog(store, meta.NewClient(healthSrv.Client(), healthSrv.URL))
	state := NewStateHandler(store, NewAuthenticator(store), watchdog, nil, IngressSource{}, nil, nil,
		testVersion, config.DefaultRetentionDays, AllTypes)

	// A request in the OLD (Portuguese) spelling — key AND value — counts.
	pt := ask(t, send, "token-do-a", "chave-pt-contador", textBody)
	if pt.Code != http.StatusOK {
		t.Fatalf("PT: status = %d, corpo = %s", pt.Code, pt.Body)
	}
	// English KEY, but the discriminator VALUE is still Portuguese — this
	// is the scenario Do item 5 names by hand, and it counts too (T-207).
	en := ask(t, send, "token-do-a", "chave-en-contador", enTextBody)
	if en.Code != http.StatusOK {
		t.Fatalf("EN (chave em ingles, valor em portugues): status = %d, corpo = %s", en.Code, en.Body)
	}
	// English key AND English value — fully migrated — must NOT count.
	enFull := ask(t, send, "token-do-a", "chave-en-completo-contador", enTextBodyFullyEnglish)
	if enFull.Code != http.StatusOK {
		t.Fatalf("EN completo: status = %d, corpo = %s", enFull.Code, enFull.Body)
	}

	recState := askState(t, state, "token-do-a", "lojinha")
	if recState.Code != http.StatusOK {
		t.Fatalf("GET /v1/estado: status = %d, corpo = %s", recState.Code, recState.Body)
	}
	var r testStateResponse
	if err := json.Unmarshal(recState.Body.Bytes(), &r); err != nil {
		t.Fatalf("decodificar /v1/estado: %v", err)
	}
	c, has := r.Counters[config.CounterOldNameUsed]
	if !has {
		t.Fatalf("contador %q nao aparece em /v1/estado", config.CounterOldNameUsed)
	}
	if c.Today != 2 {
		t.Errorf("contadores[%q].hoje = %d, quero 2 (PT conta, EN-chave/PT-valor conta, "+
			"EN completo NAO conta)", config.CounterOldNameUsed, c.Today)
	}
}

// --- T-205: the three routes T-203 left uncounted --------------------------
//
// T-203 wired config.CounterOldNameUsed on send, templates, leituras and
// fumaca — and left /v1/cadastro, /v1/pausa and /v1/bloqueios ACCEPTING the
// English alias without counting it (docs/TASKS.md, T-205's Why). Each test
// below proves, per route: the OLD spelling counts, the SAME request in the
// NEW spelling does not count again, and the number surfaces on
// GET /v1/estado — the same three requirements
// TestEntradaOldNameCounterCountsAndAppearsInEstado already proves for the
// send route, above.

// stateHandlerFor builds a minimal GET /v1/estado handler over an EXISTING
// store, for tests that only care whether a counter surfaces there — not
// about health, which needs its own Meta fixture per test.
func stateHandlerFor(t *testing.T, store *config.Store) http.Handler {
	t.Helper()
	m := tokenAcceptingMeta()
	srv := m.server(t)
	watchdog := NewWatchdog(store, meta.NewClient(srv.Client(), srv.URL))
	return NewStateHandler(store, NewAuthenticator(store), watchdog, nil, IngressSource{}, nil, nil,
		testVersion, config.DefaultRetentionDays, AllTypes)
}

// oldNameCounterTodayInEstado reads config.CounterOldNameUsed for `slug`
// through the REAL GET /v1/estado route — never a direct store read — so
// the test proves the SAME thing a consumer watching that endpoint would
// see, not just that a row exists in the counter table.
func oldNameCounterTodayInEstado(t *testing.T, store *config.Store, slug string) int {
	t.Helper()
	rec := askState(t, stateHandlerFor(t, store), "token-do-a", slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/estado: status = %d, corpo = %s", rec.Code, rec.Body)
	}
	var r testStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decodificar /v1/estado: %v", err)
	}
	return r.Counters[config.CounterOldNameUsed].Today
}

func TestEntradaOldNameCounterOnRegistration(t *testing.T) {
	h, store, _, _ := testRegistration(t)

	// registrationBody (cadastro_handler_test.go) writes `instancia`,
	// `numero_exibido` and `token_envio` — all THREE are the OLD spelling
	// of registrationAlias's keys.
	pt := register(t, h, "token-do-a", registrationBody("terceiro", nil))
	if pt.Code != http.StatusOK {
		t.Fatalf("PT: status = %d, corpo = %s", pt.Code, pt.Body)
	}
	if got := oldNameCounterTodayInEstado(t, store, "terceiro"); got != 1 {
		t.Fatalf("apos o pedido em PORTUGUES: contador = %d, quero 1", got)
	}

	// The SAME re-registration, fully in English (T-079: re-registering
	// REPLACES, it does not conflict with the first call).
	enBody := `{"instance":"terceiro","waba_id":"WABA-DO-CONSUMIDOR","phone_number_id":"PNID-DO-CONSUMIDOR",` +
		`"display_number":"5511999990000","app_secret":"` + testEncryptedValue["app_secret"] + `",` +
		`"send_token":"` + testEncryptedValue["token_envio"] + `","callback_url":"` + testEncryptedValue["callback_url"] + `"}`
	en := register(t, h, "token-do-a", enBody)
	if en.Code != http.StatusOK {
		t.Fatalf("EN: status = %d, corpo = %s", en.Code, en.Body)
	}
	if got := oldNameCounterTodayInEstado(t, store, "terceiro"); got != 1 {
		t.Fatalf("apos o pedido em INGLES: contador = %d, quero 1 (nao pode subir de novo)", got)
	}
}

func TestEntradaOldNameCounterOnPause(t *testing.T) {
	h, store := testPause(t)
	if err := store.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}

	pt := askPause(t, h, "token-do-a", `{"instancia":"lojinha"}`)
	if pt.Code != http.StatusOK {
		t.Fatalf("PT: status = %d, corpo = %s", pt.Code, pt.Body)
	}
	if got := oldNameCounterTodayInEstado(t, store, "lojinha"); got != 1 {
		t.Fatalf("apos o pedido em PORTUGUES: contador = %d, quero 1", got)
	}

	en := askPause(t, h, "token-do-a", `{"instance":"lojinha"}`)
	if en.Code != http.StatusOK {
		t.Fatalf("EN: status = %d, corpo = %s", en.Code, en.Body)
	}
	if got := oldNameCounterTodayInEstado(t, store, "lojinha"); got != 1 {
		t.Fatalf("apos o pedido em INGLES: contador = %d, quero 1 (nao pode subir de novo)", got)
	}
}

func TestEntradaOldNameCounterOnBlock(t *testing.T) {
	g := respondingBlockGraph(t, http.StatusOK, `{"messaging_product":"whatsapp","block_users":{"added_users":[{"input":"5511999990000","wa_id":"5511999990000"}]}}`)
	h, store := testBlock(t, g)

	pt := askBlock(t, h, http.MethodPost, "token-do-a", `{"instancia":"lojinha","telefones":["5511999990000"]}`)
	if pt.Code != http.StatusOK {
		t.Fatalf("PT: status = %d, corpo = %s", pt.Code, pt.Body)
	}
	if got := oldNameCounterTodayInEstado(t, store, "lojinha"); got != 1 {
		t.Fatalf("apos o pedido em PORTUGUES: contador = %d, quero 1", got)
	}

	en := askBlock(t, h, http.MethodPost, "token-do-a", `{"instance":"lojinha","telefones":["5511999990000"]}`)
	if en.Code != http.StatusOK {
		t.Fatalf("EN: status = %d, corpo = %s", en.Code, en.Body)
	}
	if got := oldNameCounterTodayInEstado(t, store, "lojinha"); got != 1 {
		t.Fatalf("apos o pedido em INGLES: contador = %d, quero 1 (nao pode subir de novo)", got)
	}
}

// --- T-205 Do item 3: the STRUCTURAL GUARD ----------------------------------
//
// T-205 exists because T-203 wired the counter on 4 of 7 routes and nobody
// noticed the other 3 — a hand-written list of "the 7 routes that accept an
// alias" would have the EXACT SAME blind spot on route #8: enumeration
// forgets the new item, and the forgotten one does not fail, it just
// answers with a number that looks complete (docs/TASKS.md, T-205's Why).
//
// So this guard does not enumerate routes. It reads every call site of
// translateEntradaOrReject in the outbound package's SOURCE — the one
// function every alias-decoding route goes through (see its doc comment,
// above in this file) — and requires, for the enclosing function, BOTH:
//
//  1. the returned `oldNames` is CAPTURED (not discarded as `_`);
//  2. that function's body references config.CounterOldNameUsed somewhere.
//
// A route born tomorrow that calls translateEntradaOrReject and discards
// (or never records) its oldNames fails THIS test, NAMING the route — with
// zero edits to this file. That is what makes it a guard instead of a
// snapshot of today's 7 routes.

// oldNameCounterViolation is one call site of translateEntradaOrReject
// whose enclosing function does not wire config.CounterOldNameUsed.
type oldNameCounterViolation struct {
	route string // the `route` argument's source text (identifier or literal)
	pos   string // file:line of the call, for a human to go straight to it
}

// findOldNameCounterViolations is the guard's MECHANISM — see the section
// header above for why it walks call sites instead of a route list.
func findOldNameCounterViolations(fset *token.FileSet, files []*ast.File) []oldNameCounterViolation {
	var out []oldNameCounterViolation
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			out = append(out, oldNameCounterViolationsInFunc(fset, fn)...)
		}
	}
	return out
}

func oldNameCounterViolationsInFunc(fset *token.FileSet, fn *ast.FuncDecl) []oldNameCounterViolation {
	// Does THIS function reference config.CounterOldNameUsed anywhere in its
	// body? A route may call translateEntradaOrReject and record the
	// counter through a shared helper it calls (like process(), in
	// bloqueio_handler.go, which serves both block AND unblock) — as long
	// as the reference lives in the SAME function that runs the
	// translation, which is the case for every route in this package today.
	recordsCounter := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if ok && pkgIdent.Name == "config" && sel.Sel.Name == "CounterOldNameUsed" {
			recordsCounter = true
		}
		return true
	})

	var out []oldNameCounterViolation
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		callee, ok := call.Fun.(*ast.Ident)
		if !ok || callee.Name != "translateEntradaOrReject" {
			return true
		}

		route := "?"
		if len(call.Args) > 2 {
			route = types.ExprString(call.Args[2])
		}

		// translateEntradaOrReject returns (translated, oldNames, ok) —
		// discarding the SECOND value (`_`) means this call site never
		// even LOOKS at whether an old name arrived.
		discarded := len(assign.Lhs) < 2
		if !discarded {
			if id, ok := assign.Lhs[1].(*ast.Ident); ok && id.Name == "_" {
				discarded = true
			}
		}

		if discarded || !recordsCounter {
			out = append(out, oldNameCounterViolation{
				route: route,
				pos:   fset.Position(call.Pos()).String(),
			})
		}
		return true
	})
	return out
}

// parseOutboundPackageForGuard parses every non-test .go file in THIS
// package (the test runs with its working directory set to the package
// directory, like every Go test) — production source only, so the guard
// checks what actually ships, not the tests that exercise it.
func parseOutboundPackageForGuard(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parser.ParseDir(\".\"): %v", err)
	}
	pkg, ok := pkgs["outbound"]
	if !ok {
		names := make([]string, 0, len(pkgs))
		for name := range pkgs {
			names = append(names, name)
		}
		t.Fatalf("pacote \"outbound\" nao encontrado em . — pacotes vistos: %v", names)
	}
	files := make([]*ast.File, 0, len(pkg.Files))
	for _, f := range pkg.Files {
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("nenhum arquivo .go de producao encontrado — este teste nao verificaria nada")
	}
	return fset, files
}

// TestOldNameCounterGuardCoversEveryAliasRoute is T-205 Do item 3: the
// structural guard. It fails NAMING the route(s) that accept an alias
// without counting it — see the section header above.
func TestOldNameCounterGuardCoversEveryAliasRoute(t *testing.T) {
	fset, files := parseOutboundPackageForGuard(t)
	violations := findOldNameCounterViolations(fset, files)
	if len(violations) == 0 {
		return
	}
	msgs := make([]string, 0, len(violations))
	for _, v := range violations {
		msgs = append(msgs, fmt.Sprintf("%s (chamada em %s)", v.route, v.pos))
	}
	t.Fatalf("rota(s) aceitam apelido de entrada e NAO contam config.CounterOldNameUsed: %s",
		strings.Join(msgs, "; "))
}

// TestOldNameCounterGuardCatchesAForgottenRoute is the proof the doctrine
// demands (docs/ARMADILHAS.md, "guarda que nunca reprovou nada e
// indistinguivel de guarda que nao olha"): a guard that could never fail is
// not a mechanism. This feeds the exact shape of the T-205 defect — a
// route that decodes an alias and discards oldNames — into the SAME
// analysis the real guard runs, as a SYNTHETIC source file (never a change
// to a real production file), and requires it to fail, NAMING the route.
func TestOldNameCounterGuardCatchesAForgottenRoute(t *testing.T) {
	const forgotten = `package outbound

import "net/http"

func (h *FakeHandler) fake(w http.ResponseWriter, r *http.Request) {
	translated, _, ok := translateEntradaOrReject(
		w, h.throttleLog, "POST /v1/rota-nova-esquecida", consumerName, raw, someAlias)
	if !ok {
		return
	}
	_ = translated
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "sintetico_esquecida.go", forgotten, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile do fixture sintetico: %v", err)
	}
	violations := findOldNameCounterViolations(fset, []*ast.File{f})
	if len(violations) != 1 {
		t.Fatalf("a guarda nao pegou a rota sintetica sem contador: %d violacoes, queria exatamente 1", len(violations))
	}
	if !strings.Contains(violations[0].route, "rota-nova-esquecida") {
		t.Errorf("a guarda pegou a violacao mas NAO NOMEIA a rota: %q", violations[0].route)
	}
	t.Logf("guarda reprovou como esperado, nomeando a rota: %q (em %s)", violations[0].route, violations[0].pos)

	// A SECOND fixture, same shape, but capturing oldNames WITHOUT ever
	// referencing config.CounterOldNameUsed — the other half of Do item 3
	// ("le as rotas... exige que cada uma tenha contador", not just
	// "captura o retorno").
	const capturedButNeverRecorded = `package outbound

import "net/http"

func (h *FakeHandler) fake(w http.ResponseWriter, r *http.Request) {
	translated, oldNames, ok := translateEntradaOrReject(
		w, h.throttleLog, "POST /v1/rota-capturada-sem-contar", consumerName, raw, someAlias)
	if !ok {
		return
	}
	_ = translated
	_ = oldNames // captured, but never turned into a counter
}
`
	f2, err := parser.ParseFile(fset, "sintetico_capturada.go", capturedButNeverRecorded, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile do segundo fixture sintetico: %v", err)
	}
	v2 := findOldNameCounterViolations(fset, []*ast.File{f2})
	if len(v2) != 1 {
		t.Fatalf("a guarda nao pegou a rota que CAPTURA oldNames mas nunca conta: %d violacoes, queria exatamente 1", len(v2))
	}
	if !strings.Contains(v2[0].route, "rota-capturada-sem-contar") {
		t.Errorf("a guarda pegou a violacao mas NAO NOMEIA a rota: %q", v2[0].route)
	}
}

// TestOldNameCounterGuardAcceptsAWiredRoute is the guard's OWN negative
// control: the same shape as the two fixtures above, but wired correctly,
// must produce ZERO violations — otherwise the guard would be flagging
// every route regardless of whether it counts, which is as useless as
// never failing.
func TestOldNameCounterGuardAcceptsAWiredRoute(t *testing.T) {
	const wired = `package outbound

import "net/http"

func (h *FakeHandler) fake(w http.ResponseWriter, r *http.Request) {
	translated, oldNames, ok := translateEntradaOrReject(
		w, h.throttleLog, "POST /v1/rota-nova-correta", consumerName, raw, someAlias)
	if !ok {
		return
	}
	_ = translated
	if len(oldNames) > 0 {
		h.counter.Record(p.Instance, config.CounterOldNameUsed)
	}
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "sintetico_correta.go", wired, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile do fixture sintetico: %v", err)
	}
	if v := findOldNameCounterViolations(fset, []*ast.File{f}); len(v) != 0 {
		t.Fatalf("a guarda reprovou uma rota CORRETAMENTE ligada ao contador: %+v", v)
	}
}

// --- T-207 (step 2 of T-189, for VALUES) ------------------------------------
//
// docs/MIGRACAO-CONTRATO-EN.md section 8 — the three ENTRADA value
// vocabularies: 8.1 (Request.Type, top level), 8.3 (TemplateButtonUnion.Type,
// inside botoes_template[]) and 8.5 (Request.Category, top level).

// topLevelValueBody builds a minimal POST /v1/messages body carrying ONLY
// `tipo` at the value under test. Validate() will very likely still reject
// it - a real "texto" message also needs `texto`, a real "midia" needs
// `media_id`, and so on - and that is fine: the parity this proves is "PT
// and EN produce the SAME response", never "the message is deliverable".
// After translateValueAliasInPlace runs, the PT and EN bodies below become
// byte-for-byte the SAME map before json.Unmarshal - identical output is
// what correct translation predicts, whatever that output turns out to be.
func topLevelValueBody(value string) string {
	return `{"instancia":"lojinha","para":"5511999990000","tipo":"` + value + `"}`
}

// categoryValueBody needs `tipo` to resolve to "midia" to reach Category
// validation at all (mensagem.go, case "midia") and a non-empty `media_id`
// so the switch reaches `meta.KnownCategory(p.Category)` instead of
// stopping one line earlier on the media_id check (mensagem.go:869-882).
// EVERY key here (`instance`, `to`, `kind`, `category`, `media_id`) is
// written in ENGLISH, and `kind` is set to "media" (the ENGLISH spelling
// of `tipo`) — the only thing this body leaves in Portuguese, deliberately,
// is the `category` VALUE under test. Any Portuguese KEY (including
// "categoria" itself — it is an aliasable KEY, not just the name of the
// field carrying an aliasable VALUE) or any other old-spelling value in
// this body would count on config.CounterOldNameUsed for a reason
// unrelated to the category value, and defeat the isolation
// TestEntradaOldNameCounterCountsOldValueUsage relies on.
func categoryValueBody(value string) string {
	return `{"instance":"lojinha","to":"5511999990000","kind":"media","media_id":"m1",` +
		`"category":"` + value + `"}`
}

// templateButtonValueBody needs `tipo:"template"` plus `template`/`idioma`
// (case "template" requires both, mensagem.go:753-759) to reach
// validateTemplateButtons at all.
func templateButtonValueBody(value string) string {
	return `{"instancia":"lojinha","para":"5511999990000","tipo":"template","template":"promo",` +
		`"idioma":"pt_BR","botoes_template":[{"indice":0,"tipo":"` + value + `"}]}`
}

// valueAliasCase is one (pt, en) pair from one of the three ENTRADA value
// vocabularies, plus enough of a request body to reach the field that
// carries it.
type valueAliasCase struct {
	name   string
	ptBody string
	enBody string
}

// allValueAliasCases is EVERY value of the three ENTRADA vocabularies - 18
// in total (11 + 2 + 5, docs/MIGRACAO-CONTRATO-EN.md section 8.1/8.3/8.5)
// - including the ones already spelled the same word in both languages
// (`template`, `cta_url`, `flow`, `url`, `video`, `audio`, `sticker`):
// Verify asks for "valor por valor, nao por amostra", and for a same-word
// value the parity is trivial but still MEASURED, not assumed.
func allValueAliasCases() []valueAliasCase {
	var cases []valueAliasCase
	add := func(vocab string, bodyFn func(string) string, pt, en string) {
		cases = append(cases, valueAliasCase{
			name:   vocab + "-" + pt,
			ptBody: bodyFn(pt),
			enBody: bodyFn(en),
		})
	}
	// 8.1 - Request.Type, top level (11 values).
	top := map[string]string{
		"texto":             "text",
		"template":          "template",
		"botoes":            "buttons",
		"cta_url":           "cta_url",
		"lista":             "list",
		"pedir_localizacao": "request_location",
		"reacao":            "reaction",
		"localizacao":       "location",
		"contatos":          "contacts",
		"flow":              "flow",
		"midia":             "media",
	}
	for pt, en := range top {
		add("8.1-tipo", topLevelValueBody, pt, en)
	}
	// 8.3 - TemplateButtonUnion.Type, inside botoes_template[] (2 values).
	btn := map[string]string{
		"url":             "url",
		"resposta_rapida": "quick_reply",
	}
	for pt, en := range btn {
		add("8.3-tipo-botao", templateButtonValueBody, pt, en)
	}
	// 8.5 - Request.Category, top level (5 values).
	cat := map[string]string{
		"imagem":    "image",
		"video":     "video",
		"audio":     "audio",
		"documento": "document",
		"sticker":   "sticker",
	}
	for pt, en := range cat {
		add("8.5-categoria", categoryValueBody, pt, en)
	}
	return cases
}

// TestEntradaValueAliasWithIdenticalResponse is Verify item 1: PT and EN
// spellings of EVERY value of the three ENTRADA vocabularies produce an
// IDENTICAL response - value by value, not by sample (18 cases).
func TestEntradaValueAliasWithIdenticalResponse(t *testing.T) {
	srv := acceptingMeta("wamid.VALORPARIDADE")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	cases := allValueAliasCases()
	if len(cases) != 18 {
		t.Fatalf("montei %d casos, queria 18 (11 + 2 + 5, secao 8.1/8.3/8.5)", len(cases))
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pt := ask(t, h, "token-do-a", "valor-pt-"+c.name, c.ptBody)
			en := ask(t, h, "token-do-a", "valor-en-"+c.name, c.enBody)
			if pt.Code != en.Code {
				t.Fatalf("status PT=%d EN=%d - corpo PT=%s EN=%s", pt.Code, en.Code, pt.Body, en.Body)
			}
			if pt.Body.String() != en.Body.String() {
				t.Errorf("respostas diferentes:\nPT: %s\nEN: %s", pt.Body, en.Body)
			}
		})
	}
}

// TestEntradaValueIdempotencyCrossesLanguages is T-207's central test - Do
// item 5's cross-language idempotency rule, spelled out in the Why line:
// mirrors TestEntradaIdempotencyCrossesLanguages, but the ONLY thing that
// differs between the two requests here is the VALUE of `tipo` ("texto" vs
// "text"), never a key. If the hash were computed BEFORE value
// translation, this would look like two DIFFERENT requests under the SAME
// Idempotency-Key - and the SAME message would go out TWICE to the
// customer. "Se so' um teste desta tarefa puder existir, e' este" (Do item
// 5's own words).
func TestEntradaValueIdempotencyCrossesLanguages(t *testing.T) {
	var sends int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sends++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.VALORIDIOMAS"}]}`))
	}))
	defer srv.Close()
	h, _ := testHandler(t, srv)

	enValueBody := `{"instancia":"lojinha","para":"5511999990000","tipo":"text","texto":"oi"}`

	first := ask(t, h, "token-do-a", "mesma-chave-valor-dois-idiomas", textBody)
	second := ask(t, h, "token-do-a", "mesma-chave-valor-dois-idiomas", enValueBody)

	if sends != 1 {
		t.Fatalf("a Meta recebeu %d envios para o MESMO pedido com o MESMO valor escrito em dois "+
			"idiomas sob a MESMA Idempotency-Key - quero 1: acima disso e a mesma mensagem saindo "+
			"duas vezes para a cliente", sends)
	}
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d (PT) e %d (EN), quero 200 nos dois - corpo PT=%s EN=%s",
			first.Code, second.Code, first.Body, second.Body)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("respostas diferentes entre PT e EN sob a mesma chave:\nPT: %s\nEN: %s",
			first.Body, second.Body)
	}
	var r1, r2 struct {
		WaMessageID string `json:"wa_message_id"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &r1)
	_ = json.Unmarshal(second.Body.Bytes(), &r2)
	if r1.WaMessageID != "wamid.VALORIDIOMAS" || r2.WaMessageID != "wamid.VALORIDIOMAS" {
		t.Fatalf("wa_message_id PT=%q EN=%q, quero os dois iguais a wamid.VALORIDIOMAS",
			r1.WaMessageID, r2.WaMessageID)
	}
}

// TestEntradaValueAliasIsScopedPerObject is Verify item 3 ("escopo por
// objeto, provado"): a valid EN value of the TOP-LEVEL `tipo` vocabulary
// used inside `botoes_template` stays REJECTED, and a valid EN value of
// the BUTTON `tipo` vocabulary used at the top level stays REJECTED too -
// proof the alias never leaked from one dictionary into the other's
// object, even though both share the JSON key name "tipo".
func TestEntradaValueAliasIsScopedPerObject(t *testing.T) {
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a Meta foi CHAMADA com um pedido que deveria ter sido recusado")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.NUNCA"}]}`))
	}))
	defer metaSrv.Close()
	h, _ := testHandler(t, metaSrv)

	// "media" is a VALID top-level value (8.1: midia -> media). Used
	// inside a template button, it means nothing:
	// templateButtonTypeValueAlias has no "media" entry, and
	// TemplateButtonUnion's own vocabulary ("url"/"resposta_rapida")
	// doesn't know it either.
	rec := ask(t, h, "token-do-a", "escopo-media-no-botao", templateButtonValueBody("media"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (\"media\" do vocabulario do topo nao deveria valer "+
			"dentro de botoes_template) - corpo: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "media") {
		t.Errorf("a mensagem de erro deveria citar o valor recusado \"media\": %s", rec.Body)
	}

	// "quick_reply" is a VALID button-scope value (8.3:
	// resposta_rapida -> quick_reply). Used as the TOP-LEVEL `tipo`, it
	// means nothing: requestTypeValueAlias has no "quick_reply" entry.
	rec2 := ask(t, h, "token-do-a", "escopo-quickreply-no-topo", topLevelValueBody("quick_reply"))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (\"quick_reply\" do vocabulario do botao nao deveria "+
			"valer no topo) - corpo: %s", rec2.Code, rec2.Body)
	}
	if !strings.Contains(rec2.Body.String(), "quick_reply") {
		t.Errorf("a mensagem de erro deveria citar o valor recusado \"quick_reply\": %s", rec2.Body)
	}
}

// TestEntradaInventedValueStillRejected is Verify item 4 (Do item 4): the
// value alias ADDS spellings, it never loosens validation - a value nobody
// published (not the Portuguese one, not its English alias) is rejected
// with the EXACT SAME message today's (pre-T-207) gateway already gives.
func TestEntradaInventedValueStillRejected(t *testing.T) {
	srv := acceptingMeta("wamid.NUNCA")
	defer srv.Close()
	h, _ := testHandler(t, srv)

	rec := ask(t, h, "token-do-a", "valor-inventado", topLevelValueBody("bugigangue"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 - corpo: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), ErrUnknownType.Error()) {
		t.Errorf("a mensagem de erro nao e a de ErrUnknownType (%q): %s", ErrUnknownType.Error(), rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "bugigangue") {
		t.Errorf("a mensagem de erro deveria citar o valor recusado: %s", rec.Body)
	}
}

// TestEntradaOldNameCounterCountsOldValueUsage is Do item 5, isolated from
// the key alias entirely: a request whose KEYS are already the CANONICAL
// Portuguese ones (nothing to translate there) but whose `categoria` VALUE
// is still Portuguese, with a "categoria" key that has an English
// alternative (`imagem` -> `image`), counts on config.CounterOldNameUsed -
// and the SAME request with the value already in English does not count
// again.
func TestEntradaOldNameCounterCountsOldValueUsage(t *testing.T) {
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	sendSrv := acceptingMeta("wamid.VALORCONTADOR")
	defer sendSrv.Close()
	send := NewHandler(store, NewAuthenticator(store),
		meta.NewClient(sendSrv.Client(), sendSrv.URL), 1<<20, config.NewCounter(store), config.NewTransit(store), AllTypes)

	oldValue := ask(t, send, "token-do-a", "valor-velho-contador", categoryValueBody("imagem"))
	if oldValue.Code != http.StatusOK {
		t.Fatalf("valor velho: status = %d, corpo = %s", oldValue.Code, oldValue.Body)
	}
	newValue := ask(t, send, "token-do-a", "valor-novo-contador", categoryValueBody("image"))
	if newValue.Code != http.StatusOK {
		t.Fatalf("valor novo: status = %d, corpo = %s", newValue.Code, newValue.Body)
	}

	if got := oldNameCounterTodayInEstado(t, store, "lojinha"); got != 1 {
		t.Errorf("contadores[%q].hoje = %d, quero 1 (so o valor velho conta)",
			config.CounterOldNameUsed, got)
	}
}
