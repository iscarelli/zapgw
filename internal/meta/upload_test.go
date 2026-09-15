package meta

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeUploadMeta serves the THREE legs of the Resumable Upload API:
//
//	GET  /app                -> app id discovery
//	POST /{app-id}/uploads   -> session creation
//	POST /upload:XXXX        -> the bytes
type fakeUploadMeta struct {
	mu sync.Mutex

	appIDStatus   int
	appIDResponse string
	appIDAuth     string
	appIDQuery    string

	sessionStatus        int
	sessionResponse      string
	sessionAuth          string
	sessionQuery         string
	sessionPathSeenAppID string

	byteStatus        int
	byteResponse      string
	byteAuth          string
	byteOffsetHeader  string
	byteURL           string
	byteContentLength int64
	receivedContent   []byte
}

func newFakeUploadMeta() *fakeUploadMeta {
	return &fakeUploadMeta{
		appIDStatus:     http.StatusOK,
		appIDResponse:   `{"id":"APP-1"}`,
		sessionStatus:   http.StatusOK,
		sessionResponse: `{"id":"upload:XYZ"}`,
		byteStatus:      http.StatusOK,
		byteResponse:    `{"h":"HANDLE-ABC"}`,
	}
}

func (m *fakeUploadMeta) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/app"):
			m.appIDAuth = r.Header.Get("Authorization")
			m.appIDQuery = r.URL.RawQuery
			w.WriteHeader(m.appIDStatus)
			_, _ = io.WriteString(w, m.appIDResponse)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploads"):
			m.sessionAuth = r.Header.Get("Authorization")
			m.sessionQuery = r.URL.RawQuery
			m.sessionPathSeenAppID = r.URL.Path
			w.WriteHeader(m.sessionStatus)
			_, _ = io.WriteString(w, m.sessionResponse)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "upload:"):
			m.byteAuth = r.Header.Get("Authorization")
			m.byteOffsetHeader = r.Header.Get("file_offset")
			m.byteURL = r.URL.String()
			m.byteContentLength = r.ContentLength
			body, _ := io.ReadAll(r.Body)
			m.receivedContent = body
			w.WriteHeader(m.byteStatus)
			_, _ = io.WriteString(w, m.byteResponse)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// TestAppIDHappyPath proves the token goes in the header (never the query)
// and that a well-formed `{"id": ...}` comes back as the app id.
func TestAppIDHappyPath(t *testing.T) {
	m := newFakeUploadMeta()
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	id, err := c.AppID(context.Background(), "token-abc")
	if err != nil {
		t.Fatalf("AppID: %v", err)
	}
	if id != "APP-1" {
		t.Fatalf("id = %q, want APP-1", id)
	}
	if m.appIDAuth != "Bearer token-abc" {
		t.Fatalf("Authorization = %q, want %q", m.appIDAuth, "Bearer token-abc")
	}
	if strings.Contains(m.appIDQuery, "token-abc") {
		t.Fatalf("the token leaked into the query string: %q", m.appIDQuery)
	}
	q, err := url.ParseQuery(m.appIDQuery)
	if err != nil {
		t.Fatalf("parse query %q: %v", m.appIDQuery, err)
	}
	if got := q.Get("fields"); got != "id" {
		t.Fatalf("fields = %q, want %q", got, "id")
	}
}

// TestAppIDWithoutID proves a 2xx with no `id` field is NOT success — the
// same trap as media.go's uploadID, generalized (T-241's requiredStringField).
func TestAppIDWithoutID(t *testing.T) {
	m := newFakeUploadMeta()
	m.appIDResponse = `{}`
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	_, err := c.AppID(context.Background(), "token")
	if !errors.Is(err, ErrAppIDWithoutID) {
		t.Fatalf("err = %v, want ErrAppIDWithoutID", err)
	}
}

// TestAppIDClassifiesMeta4xx proves a Meta error response comes back as a
// classified *MetaError, same taxonomy as every other call in this client.
func TestAppIDClassifiesMeta4xx(t *testing.T) {
	m := newFakeUploadMeta()
	m.appIDStatus = http.StatusUnauthorized
	m.appIDResponse = `{"error":{"message":"invalid token","code":190}}`
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	_, err := c.AppID(context.Background(), "expired-token")
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want *MetaError", err)
	}
	if me.MetaCode != 190 {
		t.Fatalf("MetaCode = %d, want 190", me.MetaCode)
	}
	if me.Class != ClassConfig {
		t.Fatalf("Class = %q, want %q (401)", me.Class, ClassConfig)
	}
}

// TestCreateUploadSessionAndCompleteUploadHappyPath is the caminho feliz
// the task's Verify note asks for: the session request carries
// file_length/file_type/file_name in the query, the byte request carries
// file_offset: 0 and the OAuth spelling, the bytes arrive intact, and the
// token never touches a URL.
//
// The two calls are SEPARATE (T-241 addendum): the outbound handler
// sequences them itself so it can attribute a failure to the right step.
func TestCreateUploadSessionAndCompleteUploadHappyPath(t *testing.T) {
	m := newFakeUploadMeta()
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	content := "the-raw-png-bytes-of-a-synthetic-fixture"
	sessionID, err := c.CreateUploadSession(context.Background(), "APP-1", "token-xyz",
		"image/png", "header.png", int64(len(content)))
	if err != nil {
		t.Fatalf("CreateUploadSession: %v", err)
	}
	if sessionID != "upload:XYZ" {
		t.Fatalf("sessionID = %q, want upload:XYZ", sessionID)
	}

	if !strings.HasSuffix(m.sessionPathSeenAppID, "/APP-1/uploads") {
		t.Fatalf("session request path = %q, want it to end in /APP-1/uploads", m.sessionPathSeenAppID)
	}
	q, err := url.ParseQuery(m.sessionQuery)
	if err != nil {
		t.Fatalf("parse session query %q: %v", m.sessionQuery, err)
	}
	if got := q.Get("file_length"); got != strconv.Itoa(len(content)) {
		t.Fatalf("file_length = %q, want %d", got, len(content))
	}
	if got := q.Get("file_type"); got != "image/png" {
		t.Fatalf("file_type = %q, want image/png", got)
	}
	if got := q.Get("file_name"); got != "header.png" {
		t.Fatalf("file_name = %q, want header.png", got)
	}
	if m.sessionAuth != "Bearer token-xyz" {
		t.Fatalf("session Authorization = %q, want Bearer token-xyz", m.sessionAuth)
	}

	handle, err := c.CompleteUpload(context.Background(), sessionID, "token-xyz", int64(len(content)), strings.NewReader(content))
	if err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	if handle != "HANDLE-ABC" {
		t.Fatalf("handle = %q, want HANDLE-ABC", handle)
	}
	if m.byteOffsetHeader != "0" {
		t.Fatalf("file_offset header = %q, want %q", m.byteOffsetHeader, "0")
	}
	// The doc's own spelling for THIS endpoint — every other call in the
	// client uses Bearer.
	if m.byteAuth != "OAuth token-xyz" {
		t.Fatalf("byte-upload Authorization = %q, want %q", m.byteAuth, "OAuth token-xyz")
	}
	if strings.Contains(m.byteURL, "token-xyz") {
		t.Fatalf("the token leaked into the byte-upload URL: %q", m.byteURL)
	}
	if string(m.receivedContent) != content {
		t.Fatalf("bytes received = %q, want %q (streaming must not truncate or alter them)", m.receivedContent, content)
	}
	if m.byteContentLength != int64(len(content)) {
		t.Fatalf("byte-upload Content-Length = %d, want %d", m.byteContentLength, len(content))
	}
}

// TestCreateUploadSessionWithoutID proves a session response with no `id`
// is NOT success.
func TestCreateUploadSessionWithoutID(t *testing.T) {
	m := newFakeUploadMeta()
	m.sessionResponse = `{}`
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	_, err := c.CreateUploadSession(context.Background(), "APP-1", "t", "image/png", "x.png", 3)
	if !errors.Is(err, ErrUploadSessionWithoutID) {
		t.Fatalf("err = %v, want ErrUploadSessionWithoutID", err)
	}
}

// TestCompleteUploadWithoutHandle proves a byte-upload response with no
// `h` is NOT success — the SEPARATE sentinel from the session's missing
// `id`, so a log naming this error tells the two failures apart.
func TestCompleteUploadWithoutHandle(t *testing.T) {
	m := newFakeUploadMeta()
	m.byteResponse = `{}`
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	_, err := c.CompleteUpload(context.Background(), "upload:XYZ", "t", 3, strings.NewReader("abc"))
	if !errors.Is(err, ErrUploadWithoutHandle) {
		t.Fatalf("err = %v, want ErrUploadWithoutHandle", err)
	}
}

// TestCreateUploadSessionClassifiesMeta4xx proves a Meta error at the
// session step comes back classified, same as every other call in this
// client.
func TestCreateUploadSessionClassifiesMeta4xx(t *testing.T) {
	m := newFakeUploadMeta()
	m.sessionStatus = http.StatusBadRequest
	m.sessionResponse = `{"error":{"message":"invalid file_type","code":100}}`
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	_, err := c.CreateUploadSession(context.Background(), "APP-1", "t", "image/png", "x.png", 3)
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want *MetaError", err)
	}
	if me.MetaCode != 100 {
		t.Fatalf("MetaCode = %d, want 100", me.MetaCode)
	}
	if me.Class != ClassPermanent {
		t.Fatalf("Class = %q, want %q (400)", me.Class, ClassPermanent)
	}
}

// TestCompleteUploadClassifiesMeta4xx proves the SAME taxonomy applies to
// the byte-upload step.
func TestCompleteUploadClassifiesMeta4xx(t *testing.T) {
	m := newFakeUploadMeta()
	m.byteStatus = http.StatusBadRequest
	m.byteResponse = `{"error":{"message":"invalid file","code":101}}`
	srv := m.server(t)
	c := NewClient(srv.Client(), srv.URL)

	_, err := c.CompleteUpload(context.Background(), "upload:XYZ", "t", 3, strings.NewReader("abc"))
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want *MetaError", err)
	}
	if me.MetaCode != 101 {
		t.Fatalf("MetaCode = %d, want 101", me.MetaCode)
	}
}

// TestCreateUploadSessionRejectsInvalidAppID proves the SAME guard as
// every other id-into-URL-segment call in this client — an appID with
// `../` must not reach url.JoinPath.
func TestCreateUploadSessionRejectsInvalidAppID(t *testing.T) {
	c := NewClient(http.DefaultClient, "https://graph.example.invalid")

	_, err := c.CreateUploadSession(context.Background(), "../etc/passwd", "t", "image/png", "x.png", 3)
	if !errors.Is(err, ErrInvalidPhoneNumberID) {
		t.Fatalf("err = %v, want ErrInvalidPhoneNumberID", err)
	}
}
