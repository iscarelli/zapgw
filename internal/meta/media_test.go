package meta

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMediaMeta serves the THREE media legs of the Graph API:
//
//	POST /{phone_number_id}/media  -> upload
//	GET  /{media_id}               -> description (mime, size, temporary url)
//	GET  /arquivo                  -> the bytes
//
// It's a TLS server because the second leg carries the instance's Bearer to
// a URL that came FROM OUTSIDE (Meta's response): sending that token over
// http would hand it over in the clear to anyone on the path.
type fakeMediaMeta struct {
	mu sync.Mutex

	// what the upload saw arrive
	declaredType        string
	product             string
	filename            string
	partMime            string
	receivedContent     []byte
	authorizationUpload string
	uploadURL           string

	// what to answer with
	uploadResponse   string
	uploadStatus     int
	getMime          string
	describeStatus   int
	describeBody     string // if != "", replaces the built response
	fileBytes        string
	authorizationGet string

	// urlScheme allows forging a download url as http:// — the case
	// that would hand over the token in the clear.
	urlScheme string

	uploads     atomic.Int64
	description atomic.Int64
	downloads   atomic.Int64

	// gotFileStart closes as soon as the file's first bytes
	// reach the server. It's what proves streaming: with a buffered
	// upload the channel never closes, because the client would be
	// holding the bytes.
	gotFileStart chan struct{}
	closeOnce    sync.Once
}

func newFakeMediaMeta() *fakeMediaMeta {
	return &fakeMediaMeta{
		uploadStatus:   http.StatusOK,
		uploadResponse: `{"id":"MEDIA-123"}`,
		describeStatus: http.StatusOK,
		getMime:        "audio/ogg",
		fileBytes:      "OggS-bytes-de-audio",
		gotFileStart:   make(chan struct{}),
	}
}

func (m *fakeMediaMeta) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media"):
			m.serveUpload(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/arquivo"):
			m.downloads.Add(1)
			m.mu.Lock()
			m.authorizationGet = r.Header.Get("Authorization")
			body := m.fileBytes
			m.mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodGet:
			m.serveDescribe(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	m.mu.Lock()
	if m.urlScheme == "" {
		m.urlScheme = s.URL
	}
	m.mu.Unlock()
	return s
}

func (m *fakeMediaMeta) serveUpload(w http.ResponseWriter, r *http.Request) {
	m.uploads.Add(1)
	m.mu.Lock()
	m.uploadURL = r.URL.String()
	m.authorizationUpload = r.Header.Get("Authorization")
	m.mu.Unlock()

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
		switch part.FormName() {
		case "messaging_product":
			value, _ := io.ReadAll(part)
			m.mu.Lock()
			m.product = string(value)
			m.mu.Unlock()
		case "type":
			value, _ := io.ReadAll(part)
			m.mu.Lock()
			m.declaredType = string(value)
			m.mu.Unlock()
		case "file":
			m.mu.Lock()
			m.filename = part.FileName()
			m.partMime = part.Header.Get("Content-Type")
			m.mu.Unlock()
			// Reads one chunk, signals that the bytes HAVE STARTED
			// arriving, and only then consumes the rest: that's how the
			// streaming test unblocks the producer on the other side.
			start := make([]byte, 4)
			n, _ := io.ReadFull(part, start)
			m.closeOnce.Do(func() { close(m.gotFileStart) })
			rest, _ := io.ReadAll(part)
			m.mu.Lock()
			m.receivedContent = append(append([]byte(nil), start[:n]...), rest...)
			m.mu.Unlock()
		}
	}

	m.mu.Lock()
	status, body := m.uploadStatus, m.uploadResponse
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func (m *fakeMediaMeta) serveDescribe(w http.ResponseWriter, r *http.Request) {
	m.description.Add(1)
	m.mu.Lock()
	status, body := m.describeStatus, m.describeBody
	if body == "" {
		// Actually serialized, not concatenated: a mime with quotes in
		// the parameter (`name="x"`) would break the hand-built JSON,
		// and the test would fail because of ITS OWN defect instead of
		// proving anything.
		raw, _ := json.Marshal(map[string]any{
			"url": m.urlScheme + "/arquivo", "mime_type": m.getMime,
			"sha256": "abc", "file_size": 19, "id": "MEDIA-123",
		})
		body = string(raw)
	}
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func (m *fakeMediaMeta) seen() fakeMediaMeta {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fakeMediaMeta{
		declaredType: m.declaredType, product: m.product, filename: m.filename,
		partMime: m.partMime, receivedContent: append([]byte(nil), m.receivedContent...),
		authorizationUpload: m.authorizationUpload, uploadURL: m.uploadURL,
		authorizationGet: m.authorizationGet,
	}
}

func mediaClient(t *testing.T, m *fakeMediaMeta) *Client {
	t.Helper()
	s := m.server(t)
	return NewClient(s.Client(), s.URL)
}

// THE MIME PARAMETER SURVIVES ALL THE WAY TO THE WIRE. This is the same
// 2026-07-20 cost (docs/ARMADILHAS.md) seen from the send side: it's the
// `; codecs=opus` that makes WhatsApp render a PLAYABLE voice note. If the
// upload normalized the mime to "audio/ogg" before sending, Meta would
// store the media as a plain file and resending it would deliver an
// ATTACHMENT — with no error at all, nowhere.
func TestUploadMediaKeepsTheMimesParameterOnTheWire(t *testing.T) {
	m := newFakeMediaMeta()
	c := mediaClient(t, m)

	id, err := c.UploadMedia(context.Background(), "P-lojinha", "t-lojinha",
		"audio/ogg; codecs=opus", "nota.ogg", strings.NewReader("OggS-bytes-de-audio"))
	if err != nil {
		t.Fatalf("UploadMedia: %v", err)
	}
	if id != "MEDIA-123" {
		t.Errorf("media_id = %q, quero %q", id, "MEDIA-123")
	}

	v := m.seen()
	if v.declaredType != "audio/ogg; codecs=opus" {
		t.Errorf("campo `type` no fio = %q, quero %q — o parametro do mime foi cortado, "+
			"e e ele que faz a nota de voz existir", v.declaredType, "audio/ogg; codecs=opus")
	}
	if v.partMime != "audio/ogg; codecs=opus" {
		t.Errorf("Content-Type da parte = %q, quero %q", v.partMime, "audio/ogg; codecs=opus")
	}
	if v.product != "whatsapp" {
		t.Errorf("messaging_product = %q, quero \"whatsapp\"", v.product)
	}
	if v.filename != "nota.ogg" {
		t.Errorf("filename = %q, quero \"nota.ogg\"", v.filename)
	}
	if string(v.receivedContent) != "OggS-bytes-de-audio" {
		t.Errorf("bytes recebidos = %q, quero %q", v.receivedContent, "OggS-bytes-de-audio")
	}
	if v.authorizationUpload != "Bearer t-lojinha" {
		t.Errorf("Authorization = %q, quero \"Bearer t-lojinha\"", v.authorizationUpload)
	}
	if strings.Contains(v.uploadURL, "t-lojinha") {
		t.Errorf("o token foi para a URL: %q", v.uploadURL)
	}
}

// STREAMING, and the test fails by ASSERTION if the upload buffers.
//
// The producer delivers 4 bytes and then WAITS for the server to signal it
// received them. An upload that reads the whole io.Reader into memory (or
// into a file) before opening the connection hangs right here: the server
// never receives anything, the signal never arrives, and the producer
// returns an error after the deadline. Media is content, and this
// project's line is configuration yes, message never — message bytes
// cannot land on disk or in a log.
func TestUploadMediaStreamsWithoutBufferingEverything(t *testing.T) {
	m := newFakeMediaMeta()
	c := mediaClient(t, m)

	var buffered atomic.Bool
	producer := &twoStepReader{
		first:        []byte("OggS"),
		second:       []byte("-resto-do-audio"),
		release:      m.gotFileStart,
		deadline:     3 * time.Second,
		flaggedStall: &buffered,
	}

	_, err := c.UploadMedia(context.Background(), "P-lojinha", "t-lojinha",
		"audio/ogg; codecs=opus", "nota.ogg", producer)
	if buffered.Load() {
		t.Fatalf("o upload segurou os bytes: o servidor nao viu nada enquanto o produtor esperava — " +
			"os bytes foram bufferizados em vez de atravessarem em streaming")
	}
	if err != nil {
		t.Fatalf("UploadMedia: %v", err)
	}
	if got := string(m.seen().receivedContent); got != "OggS-resto-do-audio" {
		t.Errorf("bytes recebidos = %q, quero %q", got, "OggS-resto-do-audio")
	}
}

// twoStepReader delivers the first half, waits for the signal,
// delivers the second. If the signal doesn't arrive within the deadline, it
// flags the hang and returns an error — failure by ASSERTION in the test,
// never by hanging the whole suite.
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
			return 0, errors.New("o servidor nao recebeu nada: upload bufferizado")
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

// THE SAME trap as sending, in media: a Meta 2xx does NOT prove an id came
// with it. Returning "" as success would make the consumer store an empty
// media_id and the defect would show up far from here.
func TestUploadMediaWith2xxAndNoIDReturnsANamedError(t *testing.T) {
	for _, body := range []string{`{}`, `{"id":""}`, `{"id":"   "}`, `{"id":123}`, `nao e json`} {
		m := newFakeMediaMeta()
		m.uploadResponse = body
		c := mediaClient(t, m)

		id, err := c.UploadMedia(context.Background(), "P-lojinha", "t", "image/png", "a.png",
			strings.NewReader("x"))
		if !errors.Is(err, ErrUploadWithoutID) {
			t.Errorf("corpo %q: erro = %v, quero ErrUploadWithoutID (id devolvido = %q)", body, err, id)
		}
	}
}

func TestUploadMediaWithAMetaErrorReturnsAClassifiedError(t *testing.T) {
	m := newFakeMediaMeta()
	m.uploadStatus = http.StatusUnauthorized
	m.uploadResponse = `{"error":{"message":"Invalid OAuth access token","code":190}}`
	c := mediaClient(t, m)

	_, err := c.UploadMedia(context.Background(), "P-lojinha", "t", "image/png", "a.png",
		strings.NewReader("x"))
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("erro = %v, quero *MetaError", err)
	}
	if me.Class != ClassConfig {
		t.Errorf("classe = %q, quero %q", me.Class, ClassConfig)
	}
}

// THE GET'S MIME COMES RAW. It's the OTHER half of the 2026-07-20 pair:
// Meta reports the same media as "audio/ogg; codecs=opus" in the message
// payload and as "audio/ogg" here. The gateway passes through whatever it
// says, untouched — the consumer is the one who chooses, and they can only
// choose if they receive both.
func TestDescribeMediaReturnsTheGetsMimeWithoutNormalizing(t *testing.T) {
	for _, metaMime := range []string{
		"audio/ogg",              // the 2026-07-20 case: poorer than the payload's
		"audio/ogg; codecs=opus", // and if it sends the parameter, it ALSO survives
		"application/octet-stream; name=\"x\"",
	} {
		m := newFakeMediaMeta()
		m.getMime = metaMime
		c := mediaClient(t, m)

		media, err := c.DescribeMedia(context.Background(), "MEDIA-123", "t-lojinha")
		if err != nil {
			t.Fatalf("DescribeMedia: %v", err)
		}
		if media.MimeFromGet != metaMime {
			t.Errorf("MimeFromGet = %q, quero %q — o gateway nao normaliza mime nenhum",
				media.MimeFromGet, metaMime)
		}
	}
}

func TestDownloadMediaSendsTheTokenInTheHeaderAndReturnsTheBytes(t *testing.T) {
	m := newFakeMediaMeta()
	c := mediaClient(t, m)

	media, err := c.DescribeMedia(context.Background(), "MEDIA-123", "t-lojinha")
	if err != nil {
		t.Fatalf("DescribeMedia: %v", err)
	}
	body, err := c.OpenMedia(context.Background(), media, "t-lojinha")
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer body.Close()

	bytes, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ler os bytes: %v", err)
	}
	if string(bytes) != "OggS-bytes-de-audio" {
		t.Errorf("bytes = %q, quero %q", bytes, "OggS-bytes-de-audio")
	}
	if v := m.seen(); v.authorizationGet != "Bearer t-lojinha" {
		t.Errorf("Authorization no download = %q, quero \"Bearer t-lojinha\"", v.authorizationGet)
	}
}

// The download URL comes FROM OUTSIDE (from Meta's response body) and
// carries the instance's token in the header. Accepting http:// there would
// hand that token over in the clear to anyone on the path — and this
// project's rule is that there is no TLS off mode, in BOTH directions
// (CLAUDE.md).
func TestOpenMediaRefusesAURLThatIsNotHTTPS(t *testing.T) {
	for _, bad := range []string{
		"http://lookaside.exemplo/arquivo",
		"ftp://lookaside.exemplo/arquivo",
		"",
		"   ",
		"lookaside.exemplo/arquivo",
	} {
		m := newFakeMediaMeta()
		c := mediaClient(t, m)

		_, err := c.OpenMedia(context.Background(), Media{URL: bad, MimeFromGet: "audio/ogg"}, "t-lojinha")
		if !errors.Is(err, ErrBadMediaURL) {
			t.Errorf("url %q: erro = %v, quero ErrBadMediaURL", bad, err)
		}
		if n := m.downloads.Load(); n != 0 {
			t.Errorf("url %q: houve %d download — o token saiu antes da guarda", bad, n)
		}
	}
}

// THE SAME guard as phone_number_id (client.go), and the SAME function:
// url.JoinPath resolves `..` like path.Join, so an id with `../` escapes
// the Graph API's version prefix and points to another endpoint — with the
// token along for the ride.
func TestDescribeMediaRefusesAMediaIDOfInvalidShape(t *testing.T) {
	for _, bad := range []string{"", "../outra-coisa", "MEDIA 123", "a/b", "id?x=1"} {
		m := newFakeMediaMeta()
		c := mediaClient(t, m)

		_, err := c.DescribeMedia(context.Background(), bad, "t-lojinha")
		if !errors.Is(err, ErrInvalidMediaID) {
			t.Errorf("media_id %q: erro = %v, quero ErrInvalidMediaID", bad, err)
		}
		if n := m.description.Load(); n != 0 {
			t.Errorf("media_id %q: o gateway chamou a Meta %d vez(es) antes da guarda", bad, n)
		}
	}
}

// THE CATEGORY COMES FROM THE BASE MIME, and the FULL mime is never
// rewritten because of it. Searching a table for a raw "audio/ogg;
// codecs=opus" would find nothing and would reject precisely the voice
// note.
func TestCategoryOfMimeIgnoresTheParameterWithoutRewritingTheName(t *testing.T) {
	cases := []struct {
		mimeType string
		want     Category
	}{
		{"audio/ogg; codecs=opus", CategoryAudio},
		{"audio/ogg", CategoryAudio},
		{"AUDIO/OGG", CategoryAudio}, // mime is case-insensitive by definition
		{"image/jpeg", CategoryImage},
		{"image/png", CategoryImage},
		{"image/webp", CategorySticker},
		{"video/mp4", CategoryVideo},
		{"application/pdf", CategoryDocument},
	}
	for _, c := range cases {
		got, err := CategoryOfMime(c.mimeType)
		if err != nil {
			t.Errorf("CategoryOfMime(%q): %v", c.mimeType, err)
			continue
		}
		if got != c.want {
			t.Errorf("CategoryOfMime(%q) = %q, quero %q", c.mimeType, got, c.want)
		}
	}
}

func TestCategoryOfMimeRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, bad := range []string{
		"application/x-msdownload",
		"text/html",
		"",
		"nao-e-mime",
		"application/octet-stream", // the default for a multipart part with no Content-Type
	} {
		if cat, err := CategoryOfMime(bad); !errors.Is(err, ErrUnsupportedMime) {
			t.Errorf("CategoryOfMime(%q) = (%q, %v), quero ErrUnsupportedMime", bad, cat, err)
		}
	}
}

// The table is the SINGLE SOURCE: ceiling, Graph API name, and which fields
// the category accepts all come from it. Two tables for the same question
// would diverge at the first change — this project's mother trap.
func TestTheCategoryTableIsComplete(t *testing.T) {
	allOf := []Category{
		CategoryImage, CategoryVideo, CategoryAudio, CategoryDocument, CategorySticker,
	}
	for _, cat := range allOf {
		if CategoryCap(cat) <= 0 {
			t.Errorf("CategoryCap(%q) = %d, quero > 0", cat, CategoryCap(cat))
		}
		if GraphAPIType(cat) == "" {
			t.Errorf("GraphAPIType(%q) esta vazio", cat)
		}
		if markedRead, ok := KnownCategory(string(cat)); !ok || markedRead != cat {
			t.Errorf("KnownCategory(%q) = (%q, %v), quero (%q, true)", cat, markedRead, ok, cat)
		}
	}
	if _, ok := KnownCategory("audiozinho"); ok {
		t.Error("KnownCategory aceitou uma categoria que nao existe")
	}
	// audio and sticker have no caption in the Graph API body, and only
	// document has a file name. Whoever validates (mensagem.go) and
	// whoever builds (corpo.go) read FROM HERE: if they read their own
	// rules, one would accept the field the other throws away — and the
	// consumer would see the text vanish with no error at all.
	if AcceptsCaption(CategoryAudio) || AcceptsCaption(CategorySticker) {
		t.Error("audio/sticker nao tem legenda no corpo montado; aceitar o campo o descartaria em silencio")
	}
	if !AcceptsCaption(CategoryImage) || !AcceptsCaption(CategoryVideo) || !AcceptsCaption(CategoryDocument) {
		t.Error("imagem, video e documento tem legenda")
	}
	if !AcceptsFilename(CategoryDocument) {
		t.Error("documento tem nome de arquivo")
	}
	for _, cat := range []Category{CategoryImage, CategoryVideo, CategoryAudio, CategorySticker} {
		if AcceptsFilename(cat) {
			t.Errorf("%q nao tem nome de arquivo no corpo montado", cat)
		}
	}
}

// The sticker has the smallest ceiling and the document the largest: if
// the ceilings were all the same, "limit PER CATEGORY" would just be a
// phrase in the comment.
func TestCapsDifferPerCategory(t *testing.T) {
	if CategoryCap(CategorySticker) >= CategoryCap(CategoryVideo) {
		t.Errorf("maxBytes de sticker (%d) >= maxBytes de video (%d) — o limite nao e por categoria",
			CategoryCap(CategorySticker), CategoryCap(CategoryVideo))
	}
}
