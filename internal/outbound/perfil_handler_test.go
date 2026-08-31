package outbound

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iscarelli/zapgw/internal/meta"
)

func testProfileHandler(t *testing.T, srv *httptest.Server, active ...string) (http.Handler, string) {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	h := NewProfileHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, WhatsAppOnly)
	return h, path
}

func readProfile(t *testing.T, h http.Handler, token, slug string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/perfil?instancia="+slug, nil)
	return askWithToken(t, h, req, token)
}

func writeProfile(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/perfil", strings.NewReader(body))
	return askWithToken(t, h, req, token)
}

// TestProfileReadBuildsTheRightPathAndPassesThroughWhatMetaReturned proves the GET:
// the path uses the NODE CHOSEN by profileNode (today PhoneNumberID —
// "P-lojinha", see storeWithConsumer in auth_test.go) and the returned body
// is what Meta sent, with nothing invented.
func TestProfileReadBuildsTheRightPathAndPassesThroughWhatMetaReturned(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"about":"Sobre a lojinha","vertical":"RETAIL"}]}`))
	}))
	defer srv.Close()

	h, _ := testProfileHandler(t, srv, "lojinha")
	rec := readProfile(t, h, "token-do-a", "lojinha")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	if seenPath != "/P-lojinha/whatsapp_business_profile" {
		t.Fatalf("caminho = %q, quero /P-lojinha/whatsapp_business_profile (profileNode usa PhoneNumberID hoje)",
			seenPath)
	}

	var resp profileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if resp.Instance != "lojinha" {
		t.Errorf("instancia = %q, quero \"lojinha\"", resp.Instance)
	}
	if resp.About != "Sobre a lojinha" || resp.Vertical != "RETAIL" {
		t.Errorf("perfil = %+v", resp.Profile)
	}
	// NOTHING INVENTED: description/address/email/websites didn't come from
	// Meta and must come out OMITTED from the body (omitempty), not
	// asserted as an empty string/list.
	if strings.Contains(rec.Body.String(), `"description"`) {
		t.Errorf("corpo inventou o campo description, ausente na resposta da Meta: %s", rec.Body.String())
	}
}

// TestProfileWriteSendsOnlyThePresentFieldsAndDoesNotZeroTheOthers IS THE MOST
// IMPORTANT TEST OF THIS TASK (item 4): a POST that sends ONLY `about` must
// not send `description` (not even `""`) to Meta — sending "" would erase
// the value already there.
func TestProfileWriteSendsOnlyThePresentFieldsAndDoesNotZeroTheOthers(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	h, _ := testProfileHandler(t, srv, "lojinha")
	rec := writeProfile(t, h, "token-do-a", `{"instancia":"lojinha","about":"Nova sobre"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(receivedBody, "description") || strings.Contains(receivedBody, "address") ||
		strings.Contains(receivedBody, "email") || strings.Contains(receivedBody, "websites") ||
		strings.Contains(receivedBody, "vertical") || strings.Contains(receivedBody, "profile_picture_handle") {
		t.Fatalf("o gateway mandou a Meta um campo que o consumidor NAO pediu: %s — isso apagaria o "+
			"valor gravado na Meta para esse campo", receivedBody)
	}
	const want = `{"messaging_product":"whatsapp","about":"Nova sobre"}`
	if receivedBody != want {
		t.Fatalf("corpo mandado a Meta = %s\nquero                 = %s", receivedBody, want)
	}

	var resp profileWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo de resposta nao desserializa: %v", err)
	}
	if resp.Saved.About == nil || *resp.Saved.About != "Nova sobre" {
		t.Errorf("resposta nao ecoa o about gravado: %+v", resp.Saved)
	}
	if resp.Saved.Description != nil {
		t.Errorf("resposta ecoa description que nunca foi mandado: %+v", resp.Saved)
	}
}

// TestProfileGetInstagramRefuses400WithoutCallingMeta and TestProfilePostInstagramRefuses400WithoutCallingMeta
// are the (T-111) of this task: business profile is exclusive to the
// WhatsApp Cloud API.
func TestProfileGetInstagramRefuses400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	srv := uncallableMeta(t)
	h := NewProfileHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, WhatsAppOnly)

	rec := readProfile(t, h, "token-do-a", "insta-loja")

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

func TestProfilePostInstagramRefuses400WithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	srv := uncallableMeta(t)
	h := NewProfileHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, WhatsAppOnly)

	rec := writeProfile(t, h, "token-do-a", `{"instancia":"insta-loja","about":"x"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.Class != "config" {
		t.Errorf("classe = %q, quero \"config\"", errBody.Error.Class)
	}
}

// TestProfilePassesThroughMetaExplanationAndTrace is T-153: the new error fields
// have to cross this route too, not just sending and templates.
func TestProfilePassesThroughMetaExplanationAndTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid parameter","code":100,"error_subcode":2494055,` +
			`"error_user_title":"Erro temporario","error_user_msg":"Tente novamente em alguns instantes",` +
			`"fbtrace_id":"AbCdEfGhIjKlMnOp"}}`))
	}))
	defer srv.Close()

	h, _ := testProfileHandler(t, srv, "lojinha")
	rec := writeProfile(t, h, "token-do-a", `{"instancia":"lojinha","about":"x"}`)

	errBody := decodeErrorOrFail(t, rec)
	if errBody.Error.MetaSubcode != 2494055 {
		t.Errorf("subcodigo_meta = %d, quero 2494055", errBody.Error.MetaSubcode)
	}
	if errBody.Error.MetaExplanation != "Erro temporario: Tente novamente em alguns instantes" {
		t.Errorf("explicacao_meta = %q", errBody.Error.MetaExplanation)
	}
	if errBody.Error.MetaTrace != "AbCdEfGhIjKlMnOp" {
		t.Errorf("rastro_meta = %q", errBody.Error.MetaTrace)
	}
}

// TestProfileWithoutInstanceRefuses400 checks the validation of the GET's
// required parameter.
func TestProfileWithoutInstanceRefuses400(t *testing.T) {
	srv := uncallableMeta(t)
	h, _ := testProfileHandler(t, srv, "lojinha")

	req := httptest.NewRequest(http.MethodGet, "/v1/perfil", nil)
	rec := askWithToken(t, h, req, "token-do-a")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
}

// TestProfileForeignInstanceRefuses403OnBothRoutes checks the bond — an
// instance that isn't the consumer's gets 403 on both GET and POST.
func TestProfileForeignInstanceRefuses403OnBothRoutes(t *testing.T) {
	srv := uncallableMeta(t)
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "clinica")
	if err := store.CreateConsumer("sistema-b", "token-do-b", []string{"clinica"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	h := NewProfileHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20, WhatsAppOnly)

	if rec := readProfile(t, h, "token-do-b", "lojinha"); rec.Code != http.StatusForbidden {
		t.Errorf("GET: status = %d, quero 403; corpo = %s", rec.Code, rec.Body.String())
	}
	if rec := writeProfile(t, h, "token-do-b", `{"instancia":"lojinha","about":"x"}`); rec.Code != http.StatusForbidden {
		t.Errorf("POST: status = %d, quero 403; corpo = %s", rec.Code, rec.Body.String())
	}
}

func TestProfileWithoutTokenRefuses401(t *testing.T) {
	srv := uncallableMeta(t)
	h, _ := testProfileHandler(t, srv, "lojinha")

	req := httptest.NewRequest(http.MethodGet, "/v1/perfil?instancia=lojinha", nil)
	rec := run(h, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, quero 401; corpo = %s", rec.Code, rec.Body.String())
	}
}
