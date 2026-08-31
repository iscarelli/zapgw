// Tests for the T-111 mechanism — AcceptedTypes, knownType, aceita and
// checkType (tipos.go) — and for the FIVE routes that gained the check:
// health (saude_handler.go), POST /v1/leituras, POST /v1/media (upload),
// POST /v1/templates (create) and POST /v1/cadastro.
//
// (f) of the T-111 Verify — "the five stay identical on a WhatsApp
// instance" — deliberately has NO new test here: the proof is the EXISTING
// suite for each route (saude_handler_test.go, leituras_handler_test.go,
// media_handler_test.go, templates_handler_test.go, cadastro_handler_test.go)
// staying GREEN, with NO assertion changed, after this task. A new test
// would just repeat what those already prove.
package outbound

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// storeWithInstagramConsumerAndOutsider is storeWithInstagramConsumer
// (instagram_test.go) plus a SECOND consumer with no binding at all to
// "insta-loja" — the pair item (g) of the Verify requires: a THIRD-PARTY
// instance (and, along the same code path, a NONEXISTENT one — CanUse
// does not query the database, so both produce the SAME 403 without going
// anywhere near the type check).
func storeWithInstagramConsumerAndOutsider(t *testing.T) (*config.Store, string) {
	t.Helper()
	store, path := storeWithInstagramConsumer(t)
	if err := store.CreateConsumer("sistema-outsider", "token-outsider", nil); err != nil {
		t.Fatalf("CreateConsumer outsider: %v", err)
	}
	return store, path
}

func decodeErrorOrFail(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var errBody errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("corpo de erro nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	return errBody
}

// T-115 (2): the `default:` branch of accepts (tipos.go) — the fail-closed
// brake for a value OUTSIDE the known vocabulary (WhatsAppOnly, InstagramOnly,
// AllTypes). `go tool cover` showed this branch at 0% before this test:
// no caller in this package had ever built a AcceptedTypes outside the three
// declared constants. Without proof, a refactor that swapped the switch for
// a numeric comparison, or a future fourth constant forgotten in some
// `case`, would silently undo the protection T-111 built — and the symptom
// would be an unknown value being ACCEPTED by omission, the exact defect
// T-111 closed.
func TestAcceptedTypesUnknownValueRefusesBothTypes(t *testing.T) {
	unknown := AcceptedTypes(99)
	if unknown.accepts(config.TypeWhatsApp) {
		t.Error("AcceptedTypes(99).aceita(whatsapp) = true, quero false (fail-closed)")
	}
	if unknown.accepts(config.TypeInstagram) {
		t.Error("AcceptedTypes(99).aceita(instagram) = true, quero false (fail-closed)")
	}
}

// --- (a)-(d): the four write routes reject with 400/config on an Instagram
// instance, and NEVER touch Meta -------------------------------------------

// (a) POST /v1/leituras.
func TestReadsRefusesInstagramInstanceWith400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	srv := uncallableMeta(t)
	h := NewReadsHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	rec := markRead(t, h, "token-do-a", `{"instancia":"insta-loja","wamid":"wamid.ABC123"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("classe = %q, quero \"config\"", errBody.Error.Class)
	}
	if !strings.Contains(errBody.Error.Message, `"instagram"`) {
		t.Errorf("a mensagem nao diz o tipo recusado: %q", errBody.Error.Message)
	}
}

// (b) POST /v1/media (upload).
func TestMediaUploadRefusesInstagramInstanceWith400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	srv := uncallableMeta(t)
	h := NewMediaHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), config.NewCounter(store), WhatsAppOnly)

	rec := run(h, newUploadRequest(t, "insta-loja", "arquivo", "nota.ogg",
		"audio/ogg; codecs=opus", []byte("OggS-bytes-de-audio"), true))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("classe = %q, quero \"config\"", errBody.Error.Class)
	}
}

// (c) POST /v1/templates (create).
func TestTemplatesCreateRefusesInstagramInstanceWith400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	m := metaWithCatalogOf() // counts calls: any of them fails the test
	srv := m.server(t)
	h := NewTemplatesHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)

	body := `{"instancia":"insta-loja","nome":"t1","categoria":"UTILITY","idioma":"pt_BR",` +
		`"componentes":[{"type":"BODY","text":"oi"}]}`
	rec := createTemplate(t, h, "token-do-a", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	if n := m.calls.Load(); n != 0 {
		t.Errorf("a Meta foi chamada %d vez(es) numa recusa que tinha de acontecer ANTES do fio", n)
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("classe = %q, quero \"config\"", errBody.Error.Class)
	}
}

// (d) POST /v1/cadastro — the ONLY one of the four with a SPECIFIC
// ORIENTATION (item 4 of the T-111 Do): it says Instagram registration
// belongs to whoever operates the gateway.
func TestRegistrationRefusesInstagramInstanceWith400AndGuidance(t *testing.T) {
	store, _ := storeWithInstagramConsumer(t) // does NOT need to be active: cadastro does not require it
	h := newRegistrationHandlerAt(store, NewAuthenticator(store), 1<<20, config.NewCounter(store), time.Now, WhatsAppOnly)

	rec := register(t, h, "token-do-a", registrationBody("insta-loja", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("classe = %q, quero \"config\"", errBody.Error.Class)
	}
	if !strings.Contains(errBody.Error.Message, "quem opera o gateway") {
		t.Errorf("a mensagem nao orienta o consumidor: %q", errBody.Error.Message)
	}
	// And NOTHING was written — the rejection happened BEFORE any write.
	r, err := store.SummarizeInstance("insta-loja")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if r.RegisteredAt != "" {
		t.Errorf("a instancia recusada por TIPO foi gravada mesmo assim: cadastro_em = %q", r.RegisteredAt)
	}
}

// --- (e): health (AllTypes) handles it internally, without a 400 and
// without calling Meta -------------------------------------------------

// healthResponseWithVerdict is testHealthResponse (saude_handler_test.go)
// plus the `veredito` field, which only this task's test needs to read.
type healthResponseWithVerdict struct {
	OK         bool   `json:"ok"`
	VerifiedAt string `json:"verificado_em"`
	Verdict    string `json:"veredito"`
}

func TestHealthInstagramAnswersNotApplicableWithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	m := tokenAcceptingMeta() // if it's called, the test below catches it via the counter
	srv := m.server(t)
	h := NewHealthHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), AllTypes)

	rec := askHealth(t, h, "token-do-a", "insta-loja")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (rota de LEITURA nao recusa); corpo = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("o probe falou %d vez(es) com a Meta por uma instancia Instagram — "+
			"nao existe, em graph.instagram.com, equivalente ao GET /{phone_number_id} (T-104)", n)
	}
	var resp healthResponseWithVerdict
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if !resp.OK {
		t.Errorf("ok = %v, quero true — o gateway nao detectou problema porque nao perguntou", resp.OK)
	}
	if resp.Verdict != NotApplicable {
		t.Errorf("veredito = %q, quero %q", resp.Verdict, NotApplicable)
	}
}

// --- (g): the type check does NOT LEAK EXISTENCE — a third-party instance
// (and, along the same path, a nonexistent one) still gets 403, never the
// type 400 -------------------------------------------------------------

func TestFourWriteRoutesAndHealthStay403ForForeignInstagramBeforeTheType(t *testing.T) {
	store, path := storeWithInstagramConsumerAndOutsider(t)
	activateInstance(t, path, "insta-loja")

	cases := map[string]func(t *testing.T) *httptest.ResponseRecorder{
		"leituras": func(t *testing.T) *httptest.ResponseRecorder {
			srv := uncallableMeta(t)
			h := NewReadsHandler(store, NewAuthenticator(store),
				meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)
			return markRead(t, h, "token-outsider", `{"instancia":"insta-loja","wamid":"wamid.X"}`)
		},
		"media": func(t *testing.T) *httptest.ResponseRecorder {
			srv := uncallableMeta(t)
			h := NewMediaHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), config.NewCounter(store), WhatsAppOnly)
			req := newUploadRequest(t, "insta-loja", "arquivo", "nota.ogg",
				"audio/ogg; codecs=opus", []byte("bytes"), true)
			req.Header.Set("Authorization", "Bearer token-outsider")
			return run(h, req)
		},
		"templates": func(t *testing.T) *httptest.ResponseRecorder {
			m := metaWithCatalogOf()
			srv := m.server(t)
			h := NewTemplatesHandler(store, NewAuthenticator(store),
				meta.NewClient(srv.Client(), srv.URL), 1<<20, config.NewCounter(store), WhatsAppOnly)
			return createTemplate(t, h, "token-outsider",
				`{"instancia":"insta-loja","nome":"t1","categoria":"UTILITY","idioma":"pt_BR",`+
					`"componentes":[{"type":"BODY","text":"oi"}]}`)
		},
		"cadastro": func(t *testing.T) *httptest.ResponseRecorder {
			h := newRegistrationHandlerAt(store, NewAuthenticator(store), 1<<20, config.NewCounter(store), time.Now, WhatsAppOnly)
			return register(t, h, "token-outsider", registrationBody("insta-loja", nil))
		},
		"health": func(t *testing.T) *httptest.ResponseRecorder {
			m := tokenAcceptingMeta()
			srv := m.server(t)
			h := NewHealthHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), AllTypes)
			return askHealth(t, h, "token-outsider", "insta-loja")
		},
	}

	for name, ask := range cases {
		t.Run(name, func(t *testing.T) {
			rec := ask(t)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, quero 403 — a checagem de tipo nao pode vazar antes do vinculo; corpo = %s",
					rec.Code, rec.Body.String())
			}
		})
	}
}
