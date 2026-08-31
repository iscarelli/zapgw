package outbound

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/meta"
)

// ---------------------------------------------------------------------------
// The fake Graph API
// ---------------------------------------------------------------------------

type fakeFileMeta struct {
	mu            sync.Mutex
	receivedType  string // the multipart `type` field, as it arrived
	partMime      string
	receivedBytes []byte

	getMime string
	content string

	calls atomic.Int64

	// gotStart closes when the FIRST bytes of the file reach the fake
	// Meta. It's what unblocks the producer in the streaming test: if the
	// gateway holds the bytes until the end, this never happens.
	gotStart    chan struct{}
	warnedStart sync.Once
}

func newFakeFileMeta() *fakeFileMeta {
	return &fakeFileMeta{
		getMime: "audio/ogg", content: "OggS-bytes-de-audio",
		gotStart: make(chan struct{}),
	}
}

// servidorDeArquivos is TLS because the media download carries the
// instance token in the URL Meta returned — and Meta's client rejects
// http:// there.
func (m *fakeFileMeta) server(t *testing.T) *httptest.Server {
	t.Helper()
	var base string
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.calls.Add(1)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media"):
			parts, err := r.MultipartReader()
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for {
				part, err := parts.NextPart()
				if err != nil {
					break
				}
				if part.FormName() != "file" {
					value, _ := io.ReadAll(part)
					m.mu.Lock()
					if part.FormName() == "type" {
						m.receivedType = string(value)
					}
					m.mu.Unlock()
					continue
				}
				// Reads ONE chunk, signals that bytes have started
				// arriving, and only then consumes the rest — that's the
				// signal that unblocks the producer in the streaming test.
				start := make([]byte, 4)
				n, _ := part.Read(start)
				m.warnedStart.Do(func() { close(m.gotStart) })
				rest, _ := io.ReadAll(part)
				m.mu.Lock()
				m.partMime = part.Header.Get("Content-Type")
				m.receivedBytes = append(append([]byte(nil), start[:n]...), rest...)
				m.mu.Unlock()
			}
			_, _ = io.WriteString(w, `{"id":"MEDIA-777"}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/arquivo"):
			m.mu.Lock()
			body := m.content
			m.mu.Unlock()
			_, _ = io.WriteString(w, body)
		default:
			m.mu.Lock()
			mime := m.getMime
			m.mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"url":%q,"mime_type":%q,"sha256":"abc","file_size":19}`,
				base+"/arquivo", mime)
		}
	}))
	base = s.URL
	t.Cleanup(s.Close)
	return s
}

// uncallableMeta fails the test if it receives any request. It's
// the ONLY way to PROVE "rejected before touching the wire": a counter at
// zero would also pass if the handler had returned an error for another
// reason, but this server flags it on the spot and says which route was
// touched.
func uncallableMeta(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a Meta foi chamada (%s %s) numa recusa que tinha de acontecer ANTES do fio",
			r.Method, r.URL.Path)
	}))
	t.Cleanup(s.Close)
	return s
}

func testMediaHandler(t *testing.T, srv *httptest.Server, active ...string) http.Handler {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	return NewMediaHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), WhatsAppOnly)
}

// ---------------------------------------------------------------------------
// Upload
// ---------------------------------------------------------------------------

type withoutDeclaredSize struct{ io.Reader }

// multipartBody returns the body bytes AND the Content-Type with the boundary.
func multipartBody(t *testing.T, field, filename, mimeType string, content []byte) ([]byte, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition",
		fmt.Sprintf("form-data; name=%q; filename=%q", field, filename))
	if mimeType != "" {
		header.Set("Content-Type", mimeType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("escrever a parte: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("fechar multipart: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func newUploadRequest(t *testing.T, slug, field, filename, mimeType string,
	content []byte, declareSize bool,
) *http.Request {
	t.Helper()
	raw, contentType := multipartBody(t, field, filename, mimeType, content)
	body := bytes.NewReader(raw)

	var reader io.Reader = body
	if !declareSize {
		// Hides the *bytes.Reader so httptest doesn't deduce
		// Content-Length: this is the case of a consumer sending chunked,
		// where the cap can only be enforced MID-STREAM.
		reader = withoutDeclaredSize{body}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/media?instancia="+slug, reader)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer token-do-a")
	return req
}

// twoStepReader delivers the first half, WAITS for the signal, and
// only then delivers the second. If the signal doesn't arrive in time, it
// flags the stall and returns an error — the test fails by ASSERTION,
// never by hanging the suite.
type twoStepReader struct {
	first, second []byte
	pos           int
	released      bool
	release       <-chan struct{}
	deadline      time.Duration
	flaggedStall  *atomic.Bool
}

func (l *twoStepReader) Read(p []byte) (int, error) {
	if l.pos < len(l.first) {
		n := copy(p, l.first[l.pos:])
		l.pos += n
		return n, nil
	}
	if !l.released {
		select {
		case <-l.release:
			l.released = true
		case <-time.After(l.deadline):
			l.flaggedStall.Store(true)
			return 0, errors.New("a Meta nao recebeu nada: o gateway bufferizou")
		}
	}
	rest := l.pos - len(l.first)
	if rest >= len(l.second) {
		return 0, io.EOF
	}
	n := copy(p, l.second[rest:])
	l.pos += n
	return n, nil
}

// THE BYTES CROSS THE GATEWAY, they don't stop in it. The consumer
// delivers the start of the body and only continues once the fake Meta
// signals it received something: a handler that reads the whole part into
// memory (or to disk, which is what `ParseMultipartForm` does above a
// threshold) hangs here, the signal never arrives, and the assertion
// catches it.
//
// Without this test, "streaming" would just be a word in a comment: no
// other assertion in this suite distinguishes passing-through from
// store-and-forward.
func TestUploadPassesThroughStreamingWithoutBufferingInTheHandler(t *testing.T) {
	m := newFakeFileMeta()
	h := testMediaHandler(t, m.server(t), "lojinha")

	content := bytes.Repeat([]byte("A"), 8<<10)
	raw, contentType := multipartBody(t, "arquivo", "nota.ogg", "audio/ogg", content)
	// The cut falls AFTER the part header (a few hundred bytes) and inside
	// the file: this way the gateway already has enough mime and bytes to
	// start forwarding, and all that's missing is more content.
	const cut = 1024

	var buffered atomic.Bool
	body := &twoStepReader{
		first: raw[:cut], second: raw[cut:],
		release: m.gotStart, deadline: 5 * time.Second, flaggedStall: &buffered,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/media?instancia=lojinha", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer token-do-a")

	rec := run(h, req)
	if buffered.Load() {
		t.Fatal("o gateway segurou os bytes: a Meta nao viu nada enquanto o consumidor ainda enviava — " +
			"a midia foi bufferizada em vez de atravessar em streaming")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !bytes.Equal(m.receivedBytes, content) {
		t.Errorf("a Meta recebeu %d bytes, quero %d", len(m.receivedBytes), len(content))
	}
}

func run(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// THE UPLOAD EXISTS FOR A SILENT FAILURE: without it, whoever has the
// bytes needs to host a public URL just for Meta to fetch — and when it
// doesn't fetch, the send fails silently. Here the gateway uploads the
// bytes and returns the media_id.
//
// And the mime goes to the wire WITH THE PARAMETER: it's the
// `; codecs=opus` that makes the voice note exist (docs/ARMADILHAS.md,
// cost paid on 2026-07-20).
func TestUploadReturnsMediaIDAndPreservesTheMime(t *testing.T) {
	m := newFakeFileMeta()
	h := testMediaHandler(t, m.server(t), "lojinha")

	rec := run(h, newUploadRequest(t, "lojinha", "arquivo", "nota.ogg",
		"audio/ogg; codecs=opus", []byte("OggS-bytes-de-audio"), true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "MEDIA-777") {
		t.Errorf("a resposta nao traz o media_id: %s", rec.Body.String())
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.receivedType != "audio/ogg; codecs=opus" {
		t.Errorf("campo `type` no fio = %q, quero %q — o parametro foi cortado no caminho",
			m.receivedType, "audio/ogg; codecs=opus")
	}
	if string(m.receivedBytes) != "OggS-bytes-de-audio" {
		t.Errorf("bytes no fio = %q", m.receivedBytes)
	}
}

// A MIME OUTSIDE THE CATEGORY IS REJECTED WITHOUT TOUCHING THE WIRE.
// Sending and hoping costs bandwidth on both sides and the verdict only
// arrives AFTER the whole upload.
func TestUploadRefusesMimeOutsideTheCategoryWithoutCallingMeta(t *testing.T) {
	for _, bad := range []string{"application/x-msdownload", "text/html", ""} {
		h := testMediaHandler(t, uncallableMeta(t), "lojinha")

		rec := run(h, newUploadRequest(t, "lojinha", "arquivo", "x.bin", bad,
			[]byte("qualquer coisa"), true))
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("mime %q: status = %d, quero 415; corpo = %s", bad, rec.Code, rec.Body.String())
		}
	}
}

// ABOVE THE CATEGORY CAP, SAME THING — and the cap is PER CATEGORY: the
// same size that passes as audio is rejected as sticker. A single cap
// would only be written in the comment.
func TestUploadRefusesAboveTheCategoryCapWithoutCallingMeta(t *testing.T) {
	big := bytes.Repeat([]byte("w"), int(meta.CategoryCap(meta.CategorySticker))+1)

	h := testMediaHandler(t, uncallableMeta(t), "lojinha")
	rec := run(h, newUploadRequest(t, "lojinha", "arquivo", "fig.webp", "image/webp",
		big, true))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, quero 413; corpo = %s", rec.Code, rec.Body.String())
	}

	// The SAME size, as audio, passes: proof that the rejection came from
	// the CATEGORY cap and not from some global cap.
	m := newFakeFileMeta()
	h2 := testMediaHandler(t, m.server(t), "lojinha")
	rec = run(h2, newUploadRequest(t, "lojinha", "arquivo", "nota.ogg", "audio/ogg",
		big, true))
	if rec.Code != http.StatusOK {
		t.Fatalf("o mesmo tamanho como audio deu %d, quero 200 — o teto nao e por categoria; corpo = %s",
			rec.Code, rec.Body.String())
	}
}

// Without a declared Content-Length (chunked), the cap can only be
// enforced MID-STREAM — and it HAS to be enforced, otherwise the guard
// only applies to whoever declares the size, which is exactly who doesn't
// need it.
func TestUploadRefusesAboveTheCapAlsoWithoutADeclaredSize(t *testing.T) {
	m := newFakeFileMeta()
	h := testMediaHandler(t, m.server(t), "lojinha")
	big := bytes.Repeat([]byte("w"), int(meta.CategoryCap(meta.CategorySticker))+1)

	rec := run(h, newUploadRequest(t, "lojinha", "arquivo", "fig.webp", "image/webp",
		big, false))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, quero 413; corpo = %s", rec.Code, rec.Body.String())
	}
}

func TestUploadRequiresThePartNamedArquivo(t *testing.T) {
	h := testMediaHandler(t, uncallableMeta(t), "lojinha")

	rec := run(h, newUploadRequest(t, "lojinha", "outra-coisa", "x.png", "image/png",
		[]byte("x"), true))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "arquivo") {
		t.Errorf("o erro nao diz qual e o nome esperado da parte: %s", rec.Body.String())
	}
}

// REQUIREMENT 3 also for media: system A's token can't upload a file
// through B's instance — and doesn't spend a call to Meta finding that out.
func TestUploadRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	// "clinica" ACTIVE on purpose: with it paused this test would pass
	// even with the bond guard erased (docs/ARMADILHAS.md, "Testes").
	h := testMediaHandler(t, uncallableMeta(t), "lojinha", "clinica")

	rec := run(h, newUploadRequest(t, "clinica", "arquivo", "x.png", "image/png",
		[]byte("x"), true))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quero 403; corpo = %s", rec.Code, rec.Body.String())
	}
}

func TestUploadRefusesPausedInstanceAndWithoutToken(t *testing.T) {
	h := testMediaHandler(t, uncallableMeta(t)) // none active

	rec := run(h, newUploadRequest(t, "lojinha", "arquivo", "x.png", "image/png",
		[]byte("x"), true))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("pausada: status = %d, quero 503; corpo = %s", rec.Code, rec.Body.String())
	}

	req := newUploadRequest(t, "lojinha", "arquivo", "x.png", "image/png", []byte("x"), true)
	req.Header.Del("Authorization")
	if rec := run(h, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
}

func TestUploadRequiresTheInstanceInTheQuery(t *testing.T) {
	h := testMediaHandler(t, uncallableMeta(t), "lojinha")

	req := newUploadRequest(t, "", "arquivo", "x.png", "image/png", []byte("x"), true)
	if rec := run(h, req); rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
		t.Fatalf("sem instancia: status = %d, quero 400 ou 403; corpo = %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Download — THE TWO MIMES
// ---------------------------------------------------------------------------

func askMedia(t *testing.T, h http.Handler, slug, id, payloadMime, token string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/v1/media/" + id + "?instancia=" + slug
	if payloadMime != "" {
		target += "&mime_do_payload=" + url.QueryEscape(payloadMime)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return run(h, req)
}

// THE CENTRAL TEST OF THIS TASK, and the cost is measured in production:
// on 2026-07-20, on this network, an audio relay resent a voice note with
// the `GET` mime ("audio/ogg") instead of the payload mime
// ("audio/ogg; codecs=opus"). The message ARRIVED — as a FILE ATTACHMENT,
// not as a playable voice note — and there was no error anywhere: not in
// Meta's response, not in the status webhook.
//
// That's why the gateway returns BOTH, named, and does NOT CHOOSE either
// one: choosing here would take from the consumer the one decision only
// they can make. A test that saw both as EQUAL would be proving the bug,
// not the fix.
func TestDownloadReturnsBothDistinctMimesWithoutNormalizingEither(t *testing.T) {
	const fromPayload = "audio/ogg; codecs=opus" // came in the message event
	const fromGet = "audio/ogg"                  // comes from GET /{media_id}

	m := newFakeFileMeta()
	m.getMime = fromGet
	h := testMediaHandler(t, m.server(t), "lojinha")

	rec := askMedia(t, h, "lojinha", "MEDIA-777", fromPayload, "token-do-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}

	gotPayload := rec.Header().Get("X-Zapgw-Mime-Do-Payload")
	gotGet := rec.Header().Get("X-Zapgw-Mime-Do-Get")
	if gotPayload != fromPayload {
		t.Errorf("mime_do_payload = %q, quero %q — normalizar destroi o que precisa ser preservado",
			gotPayload, fromPayload)
	}
	if gotGet != fromGet {
		t.Errorf("mime_do_get = %q, quero %q", gotGet, fromGet)
	}
	if gotPayload == gotGet {
		t.Errorf("os dois mimes vieram IGUAIS (%q): o gateway normalizou ou escolheu um — "+
			"e quem reenviar audio com o mime errado entrega anexo em vez de nota de voz", gotPayload)
	}
	// The Content-Type is deliberately NEITHER of the two: choosing one
	// there would be the gateway deciding, and a consumer who read only
	// Content-Type would end up with the wrong choice without ever seeing
	// the difference.
	if ct := rec.Header().Get("Content-Type"); ct == fromPayload || ct == fromGet {
		t.Errorf("Content-Type = %q — o gateway escolheu um dos dois mimes", ct)
	}
	if rec.Body.String() != "OggS-bytes-de-audio" {
		t.Errorf("bytes = %q", rec.Body.String())
	}
}

// WITHOUT the payload mime, the gateway does NOT invent one. Copying the
// `GET` mime into the payload field would be lying with the appearance of
// data — the consumer would resend it thinking they have the right mime.
// Absent stays absent.
func TestDownloadWithoutPayloadMimeInventsNone(t *testing.T) {
	m := newFakeFileMeta()
	h := testMediaHandler(t, m.server(t), "lojinha")

	rec := askMedia(t, h, "lojinha", "MEDIA-777", "", "token-do-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Zapgw-Mime-Do-Payload"); got != "" {
		t.Errorf("mime_do_payload = %q sem o consumidor ter mandado — o gateway inventou", got)
	}
	if got := rec.Header().Get("X-Zapgw-Mime-Do-Get"); got != "audio/ogg" {
		t.Errorf("mime_do_get = %q, quero \"audio/ogg\"", got)
	}
}

// A broken payload mime doesn't become a header: a header value with
// control characters is injection, and a mime that doesn't parse doesn't
// describe any media. The guard only CHECKS — the value that goes up is
// the original, not the parsed one, otherwise the check would turn into
// normalization through the back door.
func TestDownloadRefusesBrokenPayloadMime(t *testing.T) {
	h := testMediaHandler(t, uncallableMeta(t), "lojinha")

	for _, bad := range []string{"nao-e-mime", "audio/ogg\r\nX-Injetado: 1", "   "} {
		rec := askMedia(t, h, "lojinha", "MEDIA-777", bad, "token-do-a")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("mime_do_payload %q: status = %d, quero 400", bad, rec.Code)
		}
		if got := rec.Header().Get("X-Injetado"); got != "" {
			t.Errorf("header injetado pelo valor do mime: %q", got)
		}
	}
}

// The payload mime goes up EXACTLY AS IT ARRIVED, with original casing and
// spacing: the gateway checks, it never rewrites.
func TestDownloadDoesNotRewriteThePayloadMimeItReceives(t *testing.T) {
	const original = "AUDIO/OGG;codecs=opus"
	m := newFakeFileMeta()
	h := testMediaHandler(t, m.server(t), "lojinha")

	rec := askMedia(t, h, "lojinha", "MEDIA-777", original, "token-do-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Zapgw-Mime-Do-Payload"); got != original {
		t.Errorf("mime_do_payload = %q, quero %q (como veio)", got, original)
	}
}

func TestDownloadRefusesInstanceNotOwnedByConsumerAndWithoutToken(t *testing.T) {
	h := testMediaHandler(t, uncallableMeta(t), "lojinha", "clinica")

	if rec := askMedia(t, h, "clinica", "MEDIA-777", "", "token-do-a"); rec.Code != http.StatusForbidden {
		t.Errorf("instancia de outro: status = %d, quero 403", rec.Code)
	}
	if rec := askMedia(t, h, "lojinha", "MEDIA-777", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
}

func TestDownloadWithMetaErrorReturnsTheRightClass(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"Invalid OAuth access token","code":190}}`)
	}))
	t.Cleanup(s.Close)
	h := testMediaHandler(t, s, "lojinha")

	rec := askMedia(t, h, "lojinha", "MEDIA-777", "", "token-do-a")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502; corpo = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), string(meta.ClassConfig)) {
		t.Errorf("a resposta nao traz a classe %q: %s", meta.ClassConfig, rec.Body.String())
	}
}

// THE BYTES DO NOT GO TO THE LOG. Media is content, and this project's
// line is "configuration yes, message never": a client's audio in the
// systemd journal is a leak that no encryption at rest undoes.
func TestMediaDoesNotLogTheBytesNorTheSecrets(t *testing.T) {
	const marker = "SEGREDO-DO-CLIENTE-NO-CONTEUDO"

	m := newFakeFileMeta()
	m.content = marker
	h := testMediaHandler(t, m.server(t), "lojinha")

	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(io.Discard) })

	if rec := run(h, newUploadRequest(t, "lojinha", "arquivo", "nota.ogg", "audio/ogg",
		[]byte(marker), true)); rec.Code != http.StatusOK {
		t.Fatalf("upload: status = %d; corpo = %s", rec.Code, rec.Body.String())
	}
	if rec := askMedia(t, h, "lojinha", "MEDIA-777", "audio/ogg; codecs=opus",
		"token-do-a"); rec.Code != http.StatusOK {
		t.Fatalf("download: status = %d; corpo = %s", rec.Code, rec.Body.String())
	}

	for _, mustNotLeak := range []string{marker, "t-lojinha", "token-do-a"} {
		if strings.Contains(output.String(), mustNotLeak) {
			t.Errorf("o log vazou %q:\n%s", mustNotLeak, output.String())
		}
	}
}

// Every handler in this project serves each request in a goroutine over
// the SAME handler; without a concurrent test, `-race` has nothing to
// detect — it already cost a Critical here (docs/ARMADILHAS.md,
// "Go / concorrência").
func TestMediaConcurrentDoesNotShareState(t *testing.T) {
	const calls = 30
	m := newFakeFileMeta()
	h := testMediaHandler(t, m.server(t), "lojinha")

	var wg sync.WaitGroup
	codes := make([]int, calls)
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				codes[i] = run(h, newUploadRequest(t, "lojinha", "arquivo", "n.ogg",
					"audio/ogg", []byte("bytes"), true)).Code
				return
			}
			codes[i] = askMedia(t, h, "lojinha", "MEDIA-777", "audio/ogg; codecs=opus",
				"token-do-a").Code
		}()
	}
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("chamada %d: status = %d, quero 200", i, c)
		}
	}
}
