package outbound

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// ---------------------------------------------------------------------------
// The fake Graph API — the three legs of the Resumable Upload API.
// ---------------------------------------------------------------------------

type fakeUploadsMeta struct {
	mu sync.Mutex

	appIDStatus   int
	appIDResponse string

	sessionStatus   int
	sessionResponse string
	sessionQuery    string

	byteStatus    int
	byteResponse  string
	byteAuth      string
	receivedBytes []byte
}

func newFakeUploadsMeta() *fakeUploadsMeta {
	return &fakeUploadsMeta{
		appIDStatus:     http.StatusOK,
		appIDResponse:   `{"id":"APP-777"}`,
		sessionStatus:   http.StatusOK,
		sessionResponse: `{"id":"upload:SESSAO-1"}`,
		byteStatus:      http.StatusOK,
		byteResponse:    `{"h":"HANDLE-777"}`,
	}
}

func (m *fakeUploadsMeta) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/app"):
			w.WriteHeader(m.appIDStatus)
			_, _ = io.WriteString(w, m.appIDResponse)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploads"):
			m.sessionQuery = r.URL.RawQuery
			w.WriteHeader(m.sessionStatus)
			_, _ = io.WriteString(w, m.sessionResponse)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "upload:"):
			m.byteAuth = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			m.receivedBytes = body
			w.WriteHeader(m.byteStatus)
			_, _ = io.WriteString(w, m.byteResponse)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func testUploadsHandler(t *testing.T, srv *httptest.Server, active ...string) http.Handler {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	return NewUploadsHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), config.NewCounter(store), WhatsAppOnly)
}

// withoutContentLength hides *bytes.Reader from httptest.NewRequest so it
// cannot deduce Content-Length on its own — the same technique media's
// tests use for the "consumer sent chunked" case (withoutDeclaredSize,
// media_handler_test.go).
type withoutContentLength struct{ io.Reader }

func newUploadsRequest(slug, mimeType string, content []byte, declareLength bool) *http.Request {
	var body io.Reader = bytes.NewReader(content)
	if !declareLength {
		body = withoutContentLength{bytes.NewReader(content)}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads?instancia="+slug, body)
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("Authorization", "Bearer token-do-a")
	if declareLength {
		// httptest.NewRequest sets req.ContentLength from the *bytes.Reader
		// automatically, but NOT the Header text (it builds the request
		// from a headerless request line) — a real HTTP transport sends
		// both together, so the test has to set the header explicitly to
		// match production.
		req.Header.Set("Content-Length", strconv.Itoa(len(content)))
	}
	return req
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestUploadsReturnsHandle(t *testing.T) {
	m := newFakeUploadsMeta()
	h := testUploadsHandler(t, m.server(t), "lojinha")

	content := []byte("synthetic-png-bytes-for-a-template-header")
	rec := run(h, newUploadsRequest("lojinha", "image/png", content, true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body does not deserialize as JSON: %v (body = %q)", err, rec.Body.String())
	}
	if body["handle"] != "HANDLE-777" {
		t.Fatalf(`body["handle"] = %q, want "HANDLE-777"`, body["handle"])
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if string(m.receivedBytes) != string(content) {
		t.Errorf("bytes on the wire = %q, want %q", m.receivedBytes, content)
	}
	if m.byteAuth != "OAuth t-lojinha" {
		t.Errorf("byte-upload Authorization = %q, want the OAuth scheme with the instance's SendToken", m.byteAuth)
	}
}

// TestUploadsDefaultsTheFileName proves the `header` + mime-derived
// extension default when `?file_name=` is absent — the field has no
// observable effect on the outcome, but Meta requires it.
func TestUploadsDefaultsTheFileName(t *testing.T) {
	for mimeType, wantName := range map[string]string{
		"image/jpeg":      "header.jpg",
		"image/png":       "header.png",
		"video/mp4":       "header.mp4",
		"application/pdf": "header.pdf",
	} {
		m := newFakeUploadsMeta()
		h := testUploadsHandler(t, m.server(t), "lojinha")

		rec := run(h, newUploadsRequest("lojinha", mimeType, []byte("x"), true))
		if rec.Code != http.StatusOK {
			t.Fatalf("mime %q: status = %d, want 200; body = %s", mimeType, rec.Code, rec.Body.String())
		}
		q, err := url.ParseQuery(m.sessionQuery)
		if err != nil {
			t.Fatalf("mime %q: parse session query %q: %v", mimeType, m.sessionQuery, err)
		}
		if got := q.Get("file_name"); got != wantName {
			t.Errorf("mime %q: file_name = %q, want %q", mimeType, got, wantName)
		}
	}
}

// TestUploadsUsesTheDeclaredFileName proves `?file_name=` overrides the
// default when the consumer supplies one.
func TestUploadsUsesTheDeclaredFileName(t *testing.T) {
	m := newFakeUploadsMeta()
	h := testUploadsHandler(t, m.server(t), "lojinha")

	req := newUploadsRequest("lojinha", "image/png", []byte("x"), true)
	req.URL.RawQuery += "&file_name=cartao-presente.png"
	rec := run(h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	q, err := url.ParseQuery(m.sessionQuery)
	if err != nil {
		t.Fatalf("parse session query %q: %v", m.sessionQuery, err)
	}
	if got := q.Get("file_name"); got != "cartao-presente.png" {
		t.Errorf("file_name = %q, want the consumer's own %q", got, "cartao-presente.png")
	}
}

// ---------------------------------------------------------------------------
// 401 / 403 / 404 / 503 — the same matrix as media's
// ---------------------------------------------------------------------------

func TestUploadsRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	// "clinica" ACTIVE on purpose: with it paused this test would pass
	// even with the bond guard erased (docs/ARMADILHAS.md, "Testes").
	h := testUploadsHandler(t, uncallableMeta(t), "lojinha", "clinica")

	rec := run(h, newUploadsRequest("clinica", "image/png", []byte("x"), true))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
}

func TestUploadsRefusesUnknownInstance(t *testing.T) {
	// CreateConsumer requires the instance to ALREADY EXIST (foreign key),
	// so an "unknown instance" can only be reached the way
	// TestHandlerLogsUnknownInstanceAs404 (handler_test.go) does it: link
	// the consumer to a real instance, then delete the row underneath —
	// the same drift a deprovisioned instance would leave behind.
	store, path := storeWithConsumer(t)
	if err := store.CreateConsumer("sistema-c", "token-do-c", []string{"clinica"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to delete the instance: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM instancia WHERE slug = 'clinica'`); err != nil {
		t.Fatalf("delete instance clinica: %v", err)
	}

	srv := uncallableMeta(t)
	h := NewUploadsHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), config.NewCounter(store), WhatsAppOnly)

	req := newUploadsRequest("clinica", "image/png", []byte("x"), true)
	req.Header.Set("Authorization", "Bearer token-do-c")
	rec := run(h, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestUploadsRefusesPausedInstanceAndWithoutToken(t *testing.T) {
	h := testUploadsHandler(t, uncallableMeta(t)) // none active

	rec := run(h, newUploadsRequest("lojinha", "image/png", []byte("x"), true))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("paused: status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}

	req := newUploadsRequest("lojinha", "image/png", []byte("x"), true)
	req.Header.Del("Authorization")
	if rec := run(h, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 411 / 415 / 413
// ---------------------------------------------------------------------------

func TestUploadsRequiresContentLength(t *testing.T) {
	h := testUploadsHandler(t, uncallableMeta(t), "lojinha")

	rec := run(h, newUploadsRequest("lojinha", "image/png", []byte("x"), false))
	if rec.Code != http.StatusLengthRequired {
		t.Fatalf("status = %d, want 411; body = %s", rec.Code, rec.Body.String())
	}
}

func TestUploadsRefusesMimeOutsideTheAcceptedList(t *testing.T) {
	// image/jpg (no `e`) is deliberately absent — same line as /v1/media.
	for _, bad := range []string{"image/webp", "audio/ogg", "image/jpg", "text/plain", ""} {
		h := testUploadsHandler(t, uncallableMeta(t), "lojinha")

		rec := run(h, newUploadsRequest("lojinha", bad, []byte("x"), true))
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("mime %q: status = %d, want 415; body = %s", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestUploadsRefusesAboveTheCategoryCapWithoutCallingMeta(t *testing.T) {
	big := bytes.Repeat([]byte("w"), int(meta.CategoryCap(meta.CategoryImage))+1)

	h := testUploadsHandler(t, uncallableMeta(t), "lojinha")
	rec := run(h, newUploadsRequest("lojinha", "image/png", big, true))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Step attribution — the addendum: the consumer must be able to tell the
// three Meta-side failures apart WITHOUT depending on Message's prose.
// ---------------------------------------------------------------------------

type uploadErrorBody struct {
	Error struct {
		Class   string `json:"class"`
		Message string `json:"message"`
		Step    string `json:"step"`
	} `json:"error"`
}

func decodeUploadError(t *testing.T, rec *httptest.ResponseRecorder) uploadErrorBody {
	t.Helper()
	var body uploadErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body does not deserialize as JSON: %v (body = %q)", err, rec.Body.String())
	}
	return body
}

// TestUploadsNamesTheAppIDStepOnFailure proves a GET /app failure carries
// `"step":"app_id"` — the ONE call in this route never exercised against
// the real Meta.
func TestUploadsNamesTheAppIDStepOnFailure(t *testing.T) {
	m := newFakeUploadsMeta()
	m.appIDStatus = http.StatusUnauthorized
	m.appIDResponse = `{"error":{"message":"invalid token","code":190}}`
	h := testUploadsHandler(t, m.server(t), "lojinha")

	rec := run(h, newUploadsRequest("lojinha", "image/png", []byte("x"), true))
	body := decodeUploadError(t, rec)
	if body.Error.Step != "app_id" {
		t.Fatalf(`error.step = %q, want "app_id"; body = %s`, body.Error.Step, rec.Body.String())
	}
	if !strings.Contains(body.Error.Message, "App ID") {
		t.Errorf("error.message = %q, want it to name the app id step for a human too", body.Error.Message)
	}
}

// TestUploadsNamesTheSessionStepOnFailure proves a session-creation
// failure carries `"step":"session"`.
func TestUploadsNamesTheSessionStepOnFailure(t *testing.T) {
	m := newFakeUploadsMeta()
	m.sessionStatus = http.StatusBadRequest
	m.sessionResponse = `{"error":{"message":"invalid file_type","code":100}}`
	h := testUploadsHandler(t, m.server(t), "lojinha")

	rec := run(h, newUploadsRequest("lojinha", "image/png", []byte("x"), true))
	body := decodeUploadError(t, rec)
	if body.Error.Step != "session" {
		t.Fatalf(`error.step = %q, want "session"; body = %s`, body.Error.Step, rec.Body.String())
	}
}

// TestUploadsNamesTheUploadStepOnFailure proves a byte-upload failure
// carries `"step":"upload"`.
func TestUploadsNamesTheUploadStepOnFailure(t *testing.T) {
	m := newFakeUploadsMeta()
	m.byteStatus = http.StatusBadRequest
	m.byteResponse = `{"error":{"message":"invalid file","code":101}}`
	h := testUploadsHandler(t, m.server(t), "lojinha")

	rec := run(h, newUploadsRequest("lojinha", "image/png", []byte("x"), true))
	body := decodeUploadError(t, rec)
	if body.Error.Step != "upload" {
		t.Fatalf(`error.step = %q, want "upload"; body = %s`, body.Error.Step, rec.Body.String())
	}
}

// TestUploadsPreWireErrorsCarryNoStep proves `step` is a Meta-side
// discriminator ONLY: a rejection that happens before any byte crosses
// (411/415/413, and the 401/403/404/503 guards) must NOT carry it — `step`
// would otherwise claim a Meta call happened when none did.
func TestUploadsPreWireErrorsCarryNoStep(t *testing.T) {
	h := testUploadsHandler(t, uncallableMeta(t), "lojinha")

	rec := run(h, newUploadsRequest("lojinha", "image/gif", []byte("x"), true))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeUploadError(t, rec)
	if body.Error.Step != "" {
		t.Fatalf("error.step = %q on a pre-wire rejection, want empty", body.Error.Step)
	}
}

// ---------------------------------------------------------------------------
// Leak guard — response and log carry NEITHER the token, NOR the
// phone_number_id, NOR the app id.
// ---------------------------------------------------------------------------

func TestUploadsDoesNotLeakTokenPhoneNumberIDOrAppID(t *testing.T) {
	m := newFakeUploadsMeta()
	h := testUploadsHandler(t, m.server(t), "lojinha")

	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(io.Discard) })

	rec := run(h, newUploadsRequest("lojinha", "image/png", []byte("bytes-sinteticos"), true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// The field names that exist TODAY (storeWithConsumer, auth_test.go):
	// SendToken is "t-lojinha", PhoneNumberID is "P-lojinha", and the app
	// id this fixture's fake Meta hands out is "APP-777".
	forbidden := []string{"t-lojinha", "P-lojinha", "APP-777", "token-do-a"}
	for _, mustNotLeak := range forbidden {
		if strings.Contains(rec.Body.String(), mustNotLeak) {
			t.Errorf("the response body leaked %q: %s", mustNotLeak, rec.Body.String())
		}
		if strings.Contains(output.String(), mustNotLeak) {
			t.Errorf("the log leaked %q:\n%s", mustNotLeak, output.String())
		}
	}
}

// The same guard, on the FAILURE path — a Meta 4xx at any step must not
// echo the token or the app id into the consumer-facing message.
func TestUploadsErrorResponseDoesNotLeakTokenOrAppID(t *testing.T) {
	m := newFakeUploadsMeta()
	m.byteStatus = http.StatusBadRequest
	m.byteResponse = `{"error":{"message":"invalid file","code":101}}`
	h := testUploadsHandler(t, m.server(t), "lojinha")

	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(io.Discard) })

	rec := run(h, newUploadsRequest("lojinha", "image/png", []byte("x"), true))

	for _, mustNotLeak := range []string{"t-lojinha", "P-lojinha", "APP-777", "token-do-a"} {
		if strings.Contains(rec.Body.String(), mustNotLeak) {
			t.Errorf("the error response leaked %q: %s", mustNotLeak, rec.Body.String())
		}
		if strings.Contains(output.String(), mustNotLeak) {
			t.Errorf("the log leaked %q:\n%s", mustNotLeak, output.String())
		}
	}
}
