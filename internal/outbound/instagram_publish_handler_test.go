// Tests for T-270: POST /v1/instances/{slug}/instagram-posts and
// GET /v1/instances/{slug}/instagram-publishing-limit.
//
// Every server is a local httptest fake Graph; the gateway never talks to the
// real Meta here. All ids and URLs are synthetic.
package outbound

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

type recordedCall struct {
	method, path, rawQuery, auth, url string
	body                              map[string]any
}

// fakePublishGraph answers the whole publish flow. Container ids are
// c-1, c-2, ... in creation order; `statuses` is consumed one per status GET
// (the last one repeats); `publishStatus/publishBody` override media_publish.
type fakePublishGraph struct {
	mu            sync.Mutex
	calls         []recordedCall
	n             int
	statuses      []string
	statusIdx     int
	publishStatus int
	publishBody   string
	limitBody     string
}

func (f *fakePublishGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.calls = append(f.calls, recordedCall{r.Method, r.URL.Path, r.URL.RawQuery,
			r.Header.Get("Authorization"), r.URL.String(), body})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media_publish"):
			status := f.publishStatus
			if status == 0 {
				status = 200
			}
			resp := f.publishBody
			if resp == "" {
				resp = `{"id":"90001"}`
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(resp))
		case r.Method == http.MethodPost:
			f.mu.Lock()
			f.n++
			id := f.n
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"c-` + string(rune('0'+id)) + `"}`))
		case strings.HasSuffix(r.URL.Path, "/content_publishing_limit"):
			resp := f.limitBody
			if resp == "" {
				resp = `{"data":[{"quota_usage":7,"config":{"quota_total":40,"quota_duration":86400}}]}`
			}
			_, _ = w.Write([]byte(resp))
		default: // status GET
			f.mu.Lock()
			st := "FINISHED"
			if len(f.statuses) > 0 {
				i := f.statusIdx
				if i >= len(f.statuses) {
					i = len(f.statuses) - 1
				}
				st = f.statuses[i]
				f.statusIdx++
			}
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"status_code":"` + st + `","id":"x"}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (f *fakePublishGraph) snapshot() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedCall(nil), f.calls...)
}

func (f *fakePublishGraph) describe() []string {
	var out []string
	for _, c := range f.snapshot() {
		out = append(out, c.method+" "+c.path)
	}
	return out
}

func publishRoute(t *testing.T, srv *httptest.Server) http.Handler {
	t.Helper()
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	return NewInstagramPublishHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), "http://127.0.0.1:1"), srv.URL, 1<<20,
		meta.InstagramPublishTimings{PollInterval: 5 * time.Millisecond, MaxWait: 200 * time.Millisecond},
		InstagramOnly)
}

func postInstagramPost(t *testing.T, h http.Handler, slug, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/instances/"+slug+"/instagram-posts", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token-do-a")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const oneImage = `{"images":[{"url":"https://img.example.invalid/a.jpg","alt_text":"a red mug"}],"caption":"hello"}`

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	return m
}

func TestInstagramPostSingleImageFlowAndParams(t *testing.T) {
	fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS", "FINISHED"}}
	h := publishRoute(t, fake.server(t))

	rec := postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["media_id"] != "90001" || got["container_id"] != "c-1" || got["kind"] != "image" {
		t.Errorf("response = %v", got)
	}
	want := []string{"POST /IGID1/media", "GET /c-1", "GET /c-1", "POST /IGID1/media_publish"}
	if d := fake.describe(); strings.Join(d, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v (IN_PROGRESS must be waited on)", d, want)
	}
	calls := fake.snapshot()
	if calls[0].body["image_url"] != "https://img.example.invalid/a.jpg" ||
		calls[0].body["caption"] != "hello" || calls[0].body["alt_text"] != "a red mug" {
		t.Errorf("container body = %v", calls[0].body)
	}
	if calls[1].rawQuery != "fields=status_code" {
		t.Errorf("status query = %q", calls[1].rawQuery)
	}
	if calls[3].body["creation_id"] != "c-1" {
		t.Errorf("publish body = %v", calls[3].body)
	}
	for _, c := range calls {
		if c.auth != "Bearer t-insta-loja" {
			t.Errorf("%s %s: Authorization = %q", c.method, c.path, c.auth)
		}
	}
}

func TestInstagramPostCarouselOrderAndCaptionOnParentOnly(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))

	rec := postInstagramPost(t, h, "insta-loja", "k1", `{"images":[
		{"url":"https://img.example.invalid/1.jpg","alt_text":"one"},
		{"url":"https://img.example.invalid/2.jpg"},
		{"url":"https://img.example.invalid/3.jpg"}],"caption":"set of three"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["kind"] != "carousel" || got["container_id"] != "c-4" {
		t.Errorf("response = %v", got)
	}
	want := []string{"POST /IGID1/media", "POST /IGID1/media", "POST /IGID1/media", "POST /IGID1/media",
		"GET /c-4", "POST /IGID1/media_publish"}
	if d := fake.describe(); strings.Join(d, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v", d, want)
	}
	calls := fake.snapshot()
	for i := 0; i < 3; i++ {
		if calls[i].body["is_carousel_item"] != true {
			t.Errorf("child %d: is_carousel_item = %v", i, calls[i].body["is_carousel_item"])
		}
		if _, has := calls[i].body["caption"]; has {
			t.Errorf("child %d carries a caption: %v", i, calls[i].body)
		}
	}
	if calls[0].body["alt_text"] != "one" {
		t.Errorf("child 0 alt_text = %v", calls[0].body["alt_text"])
	}
	parent := calls[3].body
	if parent["media_type"] != "CAROUSEL" || parent["children"] != "c-1,c-2,c-3" || parent["caption"] != "set of three" {
		t.Errorf("parent body = %v", parent)
	}
}

func TestInstagramPostContainerErrorStatusIs502AndNeverPublishes(t *testing.T) {
	for _, st := range []string{"ERROR", "EXPIRED"} {
		fake := &fakePublishGraph{statuses: []string{st}}
		h := publishRoute(t, fake.server(t))

		rec := postInstagramPost(t, h, "insta-loja", "k1", oneImage)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("%s: status = %d, want 502; body = %s", st, rec.Code, rec.Body.String())
		}
		e := decodeMap(t, rec)["error"].(map[string]any)
		if e["step"] != "status" || e["status_code"] != st {
			t.Errorf("%s: error = %v, want step status and status_code", st, e)
		}
		for _, d := range fake.describe() {
			if strings.HasSuffix(d, "/media_publish") {
				t.Errorf("%s: media_publish was called", st)
			}
		}
	}
}

func TestInstagramPostPollTimeoutIs502StatusStep(t *testing.T) {
	fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS"}}
	h := publishRoute(t, fake.server(t))

	rec := postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", rec.Code, rec.Body.String())
	}
	e := decodeMap(t, rec)["error"].(map[string]any)
	if e["step"] != "status" || e["status_code"] != "IN_PROGRESS" {
		t.Errorf("error = %v", e)
	}
	for _, d := range fake.describe() {
		if strings.HasSuffix(d, "/media_publish") {
			t.Error("media_publish was called after a poll timeout")
		}
	}
}

func TestInstagramPostMetaErrorAtPublishCarriesStepPublishAndReleasesTheKey(t *testing.T) {
	fake := &fakePublishGraph{publishStatus: 400,
		publishBody: `{"error":{"message":"Media ID is not available","code":9007,"fbtrace_id":"TRACE-SYNTH-2"}}`}
	h := publishRoute(t, fake.server(t))

	rec := postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	if rec.Code == 200 {
		t.Fatalf("status = 200, want an error")
	}
	e := decodeMap(t, rec)["error"].(map[string]any)
	if e["step"] != "publish" || e["meta_code"] != float64(9007) || e["meta_trace"] != "TRACE-SYNTH-2" {
		t.Errorf("error = %v", e)
	}
	// Meta refused: known-negative, so the same key can try again (not 409).
	fake.mu.Lock()
	fake.publishStatus, fake.publishBody = 0, ""
	fake.mu.Unlock()
	if rec2 := postInstagramPost(t, h, "insta-loja", "k1", oneImage); rec2.Code != 200 {
		t.Errorf("retry after a Meta refusal: status = %d, want 200 (key released); body = %s", rec2.Code, rec2.Body.String())
	}
}

func TestInstagramPostMetaErrorAtContainerCarriesStepContainer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid image","code":36003}}`))
	}))
	t.Cleanup(srv.Close)
	h := publishRoute(t, srv)

	rec := postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	e := decodeMap(t, rec)["error"].(map[string]any)
	if rec.Code != 400 || e["step"] != "container" {
		t.Errorf("status = %d, error = %v", rec.Code, e)
	}
}

func TestInstagramPostSameKeySameBodyReplaysWithoutCallingMeta(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))

	first := postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	hits := len(fake.snapshot())
	second := postInstagramPost(t, h, "insta-loja", "k1", oneImage)

	if second.Code != 200 {
		t.Fatalf("second status = %d; body = %s", second.Code, second.Body.String())
	}
	if len(fake.snapshot()) != hits {
		t.Errorf("the replay called Meta (%d -> %d calls)", hits, len(fake.snapshot()))
	}
	a, b := decodeMap(t, first), decodeMap(t, second)
	if a["media_id"] != b["media_id"] || a["container_id"] != b["container_id"] || a["kind"] != b["kind"] {
		t.Errorf("replay differs: %v vs %v", a, b)
	}
}

func TestInstagramPostSameKeyDifferentBodyIs422WithoutCallingMeta(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))

	postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	hits := len(fake.snapshot())
	rec := postInstagramPost(t, h, "insta-loja", "k1",
		`{"images":[{"url":"https://img.example.invalid/other.jpg"}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	if len(fake.snapshot()) != hits {
		t.Error("Meta was called for a key reused with a different body")
	}
}

func TestInstagramPostValidationRefusesBeforeMeta(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))

	var many []string
	for i := 0; i < 21; i++ {
		many = append(many, `{"url":"https://img.example.invalid/x.jpg"}`)
	}
	cases := map[string]struct{ key, body string }{
		"missing key":      {"", oneImage},
		"zero images":      {"k", `{"images":[]}`},
		"21 images":        {"k", `{"images":[` + strings.Join(many, ",") + `]}`},
		"http url":         {"k", `{"images":[{"url":"http://img.example.invalid/a.jpg"}]}`},
		"relative url":     {"k", `{"images":[{"url":"/a.jpg"}]}`},
		"caption too long": {"k", `{"images":[{"url":"https://img.example.invalid/a.jpg"}],"caption":"` + strings.Repeat("a", 2201) + `"}`},
		"not json":         {"k", `nope`},
	}
	for name, c := range cases {
		rec := postInstagramPost(t, h, "insta-loja", c.key, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body = %s", name, rec.Code, rec.Body.String())
		}
	}
	// Boundaries that must pass validation.
	ok := postInstagramPost(t, h, "insta-loja", "k-edge",
		`{"images":[{"url":"https://img.example.invalid/a.jpg"}],"caption":"`+strings.Repeat("a", 2200)+`"}`)
	if ok.Code != 200 {
		t.Errorf("2200-char caption: status = %d, want 200", ok.Code)
	}
}

func TestInstagramPostRefusedRequestsMakeZeroMetaCalls(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))
	postInstagramPost(t, h, "insta-loja", "", oneImage)
	postInstagramPost(t, h, "insta-loja", "k", `{"images":[]}`)
	postInstagramPost(t, h, "insta-loja", "k", `{"images":[{"url":"http://x.example.invalid/a.jpg"}]}`)
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times for refused requests, want 0", n)
	}
}

func TestInstagramPostWhatsAppInstanceIs400Config(t *testing.T) {
	fake := &fakePublishGraph{}
	srv := fake.server(t)
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewInstagramPublishHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), srv.URL, 1<<20, meta.InstagramPublishTimings{}, InstagramOnly)

	rec := postInstagramPost(t, h, "lojinha", "k1", oneImage)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"class":"config"`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times for a WhatsApp instance", n)
	}
}

func TestInstagramPostOtherConsumersInstanceIs403(t *testing.T) {
	fake := &fakePublishGraph{}
	srv := fake.server(t)
	store, path := storeWithInstagramConsumer(t)
	if err := store.CreateInstance(config.Instance{
		Slug: "insta-outra", Type: config.TypeInstagram, IgID: "IGID2",
		AppSecret: "a", VerifyToken: "v", SendToken: "t-insta-outra",
		CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 2000,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	activateInstance(t, path, "insta-outra")
	h := NewInstagramPublishHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), srv.URL, 1<<20, meta.InstagramPublishTimings{}, InstagramOnly)

	if rec := postInstagramPost(t, h, "insta-outra", "k1", oneImage); rec.Code != http.StatusForbidden {
		t.Fatalf("post: status = %d, want 403", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/insta-outra/instagram-publishing-limit", nil)
	req.Header.Set("Authorization", "Bearer token-do-a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("limit: status = %d, want 403", rec.Code)
	}
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times for someone else's instance", n)
	}
}

func TestInstagramPublishingLimitMapsTheThreeNumbers(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))

	req := httptest.NewRequest(http.MethodGet, "/v1/instances/insta-loja/instagram-publishing-limit", nil)
	req.Header.Set("Authorization", "Bearer token-do-a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["quota_usage"] != float64(7) || got["quota_total"] != float64(40) || got["quota_duration_s"] != float64(86400) {
		t.Errorf("response = %v", got)
	}
	c := fake.snapshot()[0]
	if c.path != "/IGID1/content_publishing_limit" || c.rawQuery != "fields=config%2Cquota_usage" {
		t.Errorf("call = %s ?%s", c.path, c.rawQuery)
	}
}

func TestInstagramPublishNeverPutsTheTokenInAURLOrALogLine(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	fake := &fakePublishGraph{publishStatus: 400,
		publishBody: `{"error":{"message":"nope","code":100}}`}
	h := publishRoute(t, fake.server(t))
	postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/insta-loja/instagram-publishing-limit", nil)
	req.Header.Set("Authorization", "Bearer token-do-a")
	h.ServeHTTP(httptest.NewRecorder(), req)

	for _, c := range fake.snapshot() {
		if strings.Contains(c.url, "t-insta-loja") {
			t.Errorf("token in URL: %s", c.url)
		}
	}
	if strings.Contains(logs.String(), "t-insta-loja") {
		t.Errorf("token in a log line: %s", logs.String())
	}
}

// A transport failure DURING media_publish is an unknown outcome: the post
// may be live, so the key stays held (409 on retry) instead of being released
// into a possible duplicate.
func TestInstagramPostTransportFailureAtPublishHoldsTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/media_publish"):
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"c-1"}`))
		default:
			_, _ = w.Write([]byte(`{"status_code":"FINISHED"}`))
		}
	}))
	t.Cleanup(srv.Close)
	h := publishRoute(t, srv)

	rec := postInstagramPost(t, h, "insta-loja", "k1", oneImage)
	e := decodeMap(t, rec)["error"].(map[string]any)
	if rec.Code != http.StatusBadGateway || e["step"] != "publish" || e["class"] != "unknown" {
		t.Fatalf("status = %d, error = %v", rec.Code, e)
	}
	if rec2 := postInstagramPost(t, h, "insta-loja", "k1", oneImage); rec2.Code != http.StatusConflict {
		t.Errorf("retry status = %d, want 409 (key held)", rec2.Code)
	}
}
