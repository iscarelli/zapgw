package outbound

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// --- T-203 (step 2 of T-189): English aliases on ENTRADA --------------------

// enTextBody is textBody (handler_test.go), same request, English key names.
// The VALUE of `kind` stays "texto" — this migration step renames JSON
// KEYS, never the discriminator VALUES (docs/MIGRACAO-CONTRATO-EN.md only
// has rows for keys).
const enTextBody = `{"instance":"lojinha","to":"5511999990000","kind":"texto","text":"oi"}`

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
	h := NewBlockHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, WhatsAppOnly)

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
// request still uses the Portuguese spelling, (b) NOT go up when it does
// not, and (c) actually surface in GET /v1/estado — the same store, read
// through the same route the consumer will watch.
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

	// A request in the OLD (Portuguese) spelling counts.
	pt := ask(t, send, "token-do-a", "chave-pt-contador", textBody)
	if pt.Code != http.StatusOK {
		t.Fatalf("PT: status = %d, corpo = %s", pt.Code, pt.Body)
	}
	// The SAME request, fully in English, must NOT count again.
	en := ask(t, send, "token-do-a", "chave-en-contador", enTextBody)
	if en.Code != http.StatusOK {
		t.Fatalf("EN: status = %d, corpo = %s", en.Code, en.Body)
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
	if c.Today != 1 {
		t.Errorf("contadores[%q].hoje = %d, quero 1 (so o pedido em portugues conta)",
			config.CounterOldNameUsed, c.Today)
	}
}
