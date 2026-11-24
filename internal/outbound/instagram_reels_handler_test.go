// Tests for T-273: POST /v1/instances/{slug}/instagram-reels.
//
// Fake Graph and real SQLite store; every id and URL is synthetic. The fake
// is the one of instagram_publish_handler_test.go.
package outbound

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/meta"
)

const oneReel = `{"video_url":"https://vid.example.invalid/a.mp4","caption":"hello"}`

func postReel(t *testing.T, h http.Handler, slug, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/instances/"+slug+"/instagram-reels", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token-do-a")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func countPath(f *fakePublishGraph, suffix string) int {
	n := 0
	for _, c := range f.snapshot() {
		if strings.HasSuffix(c.path, suffix) {
			n++
		}
	}
	return n
}

func TestReelFreshPostInProgressIs202WithRetryAfter(t *testing.T) {
	fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS"}}
	h := publishRoute(t, fake.server(t))

	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}
	m := decodeMap(t, rec)
	if m["container_id"] != "c-1" || m["status"] != "IN_PROGRESS" || m["retry_after_s"] != float64(60) {
		t.Errorf("body = %v", m)
	}
	if n := countPath(fake, "/media_publish"); n != 0 {
		t.Errorf("media_publish called %d times on a container still in progress", n)
	}
	// The container call carries the Reels params and no unset field.
	first := fake.snapshot()[0]
	if first.body["media_type"] != "REELS" || first.body["video_url"] != "https://vid.example.invalid/a.mp4" ||
		first.body["caption"] != "hello" {
		t.Errorf("container body = %v", first.body)
	}
	for _, absent := range []string{"cover_url", "thumb_offset", "share_to_feed"} {
		if _, ok := first.body[absent]; ok {
			t.Errorf("%s was sent although omitted", absent)
		}
	}
	if !strings.HasPrefix(first.auth, "Bearer ") || strings.Contains(first.url, "token") {
		t.Errorf("auth = %q, url = %q", first.auth, first.url)
	}
}

func TestReelOptionalFieldsReachMeta(t *testing.T) {
	fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS"}}
	h := publishRoute(t, fake.server(t))

	body := `{"video_url":"https://vid.example.invalid/a.mp4","thumb_offset_ms":1500,"share_to_feed":false}`
	if rec := postReel(t, h, "insta-loja", "k1", body); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	b := fake.snapshot()[0].body
	if b["thumb_offset"] != float64(1500) || b["share_to_feed"] != false {
		t.Errorf("container body = %v", b)
	}

	fake2 := &fakePublishGraph{statuses: []string{"IN_PROGRESS"}}
	h2 := publishRoute(t, fake2.server(t))
	body = `{"video_url":"https://vid.example.invalid/a.mp4","cover_url":"https://img.example.invalid/c.jpg"}`
	if rec := postReel(t, h2, "insta-loja", "k1", body); rec.Code != http.StatusAccepted {
		t.Fatalf("cover: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if b := fake2.snapshot()[0].body; b["cover_url"] != "https://img.example.invalid/c.jpg" {
		t.Errorf("container body = %v", b)
	}
}

func TestReelRePostFinishedPublishesExactlyOnce(t *testing.T) {
	fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS", "FINISHED"}}
	h := publishRoute(t, fake.server(t))

	if rec := postReel(t, h, "insta-loja", "k1", oneReel); rec.Code != http.StatusAccepted {
		t.Fatalf("first: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-POST: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	if m["media_id"] != "90001" || m["container_id"] != "c-1" {
		t.Errorf("body = %v", m)
	}
	if n := countPath(fake, "/media_publish"); n != 1 {
		t.Errorf("media_publish called %d times, want 1", n)
	}
}

func TestReelFinishedOnTheFirstStatusCheckPublishesInOneRequest(t *testing.T) {
	fake := &fakePublishGraph{} // default status FINISHED
	h := publishRoute(t, fake.server(t))

	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["media_id"] != "90001" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestReelReplayAfter200MakesZeroMetaCalls(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))
	postReel(t, h, "insta-loja", "k1", oneReel)
	before := len(fake.snapshot())

	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body = %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	if m["media_id"] != "90001" || m["container_id"] != "c-1" {
		t.Errorf("replay body = %v", m)
	}
	if after := len(fake.snapshot()); after != before {
		t.Errorf("replay made %d Meta calls", after-before)
	}
}

func TestReelErrorStatusReleasesTheKeyAndNothingIsPublished(t *testing.T) {
	for _, terminal := range []string{"ERROR", "EXPIRED"} {
		t.Run(terminal, func(t *testing.T) {
			fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS", terminal, "IN_PROGRESS"}}
			h := publishRoute(t, fake.server(t))

			postReel(t, h, "insta-loja", "k1", oneReel)
			rec := postReel(t, h, "insta-loja", "k1", oneReel)
			e := decodeMap(t, rec)["error"].(map[string]any)
			if rec.Code != http.StatusBadGateway || e["step"] != "status" || e["status_code"] != terminal {
				t.Fatalf("status = %d, error = %v", rec.Code, e)
			}
			if n := countPath(fake, "/media_publish"); n != 0 {
				t.Errorf("media_publish called %d times", n)
			}
			// The key was released: the same key creates a NEW container.
			rec = postReel(t, h, "insta-loja", "k1", oneReel)
			if rec.Code != http.StatusAccepted || decodeMap(t, rec)["container_id"] != "c-2" {
				t.Errorf("after release: status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestReelStatusCallFailureKeepsTheRow(t *testing.T) {
	var mu sync.Mutex
	failStatus := false
	publishCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/media_publish"):
			publishCalls++
			_, _ = w.Write([]byte(`{"id":"90001"}`))
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"c-1"}`))
		case failStatus:
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		default:
			_, _ = w.Write([]byte(`{"status_code":"IN_PROGRESS"}`))
		}
	}))
	t.Cleanup(srv.Close)
	h := publishRoute(t, srv)

	if rec := postReel(t, h, "insta-loja", "k1", oneReel); rec.Code != http.StatusAccepted {
		t.Fatalf("first: status = %d", rec.Code)
	}
	mu.Lock()
	failStatus = true
	mu.Unlock()
	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	e := decodeMap(t, rec)["error"].(map[string]any)
	if rec.Code < 500 || e["step"] != "status" || e["class"] != "retryable" {
		t.Fatalf("status = %d, error = %v", rec.Code, e)
	}
	mu.Lock()
	failStatus = false
	mu.Unlock()
	// The row was kept: the same key still resumes the SAME container.
	rec = postReel(t, h, "insta-loja", "k1", oneReel)
	if rec.Code != http.StatusAccepted || decodeMap(t, rec)["container_id"] != "c-1" {
		t.Errorf("after the failure: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if publishCalls != 0 {
		t.Errorf("media_publish called %d times", publishCalls)
	}
}

func TestReelContainerFailureCarriesStepContainerAndReleasesTheKey(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid parameter","type":"OAuthException","code":100,"error_subcode":2207026}}`))
			return
		}
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"c-9"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status_code":"IN_PROGRESS"}`))
	}))
	t.Cleanup(srv.Close)
	h := publishRoute(t, srv)

	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	e := decodeMap(t, rec)["error"].(map[string]any)
	if rec.Code < 400 || e["step"] != "container" {
		t.Fatalf("status = %d, error = %v", rec.Code, e)
	}
	// Released: the same key works again.
	if rec := postReel(t, h, "insta-loja", "k1", oneReel); rec.Code != http.StatusAccepted {
		t.Errorf("retry status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestReelPublishMetaRefusalKeepsTheContainerAndALaterRePostPublishes(t *testing.T) {
	fake := &fakePublishGraph{publishStatus: 400,
		publishBody: `{"error":{"message":"The media is not ready","type":"OAuthException","code":9007,"error_subcode":2207027}}`}
	h := publishRoute(t, fake.server(t))

	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	e := decodeMap(t, rec)["error"].(map[string]any)
	if rec.Code < 400 || e["step"] != "publish" {
		t.Fatalf("status = %d, error = %v", rec.Code, e)
	}
	fake.mu.Lock()
	fake.publishStatus, fake.publishBody = 0, ""
	fake.mu.Unlock()

	rec = postReel(t, h, "insta-loja", "k1", oneReel)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["container_id"] != "c-1" {
		t.Fatalf("re-POST: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if n := countPath(fake, "/media_publish"); n != 2 {
		t.Errorf("media_publish calls = %d, want 2 (one refused, one accepted)", n)
	}
	if n := countPath(fake, "/media"); n != 1 {
		t.Errorf("containers created = %d, want 1", n)
	}
}

func TestReelPublishNetworkErrorThenRePostIs409AndNeverPublishesTwice(t *testing.T) {
	var mu sync.Mutex
	publishCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/media_publish"):
			mu.Lock()
			publishCalls++
			mu.Unlock()
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

	rec := postReel(t, h, "insta-loja", "k1", oneReel)
	e := decodeMap(t, rec)["error"].(map[string]any)
	if rec.Code != http.StatusBadGateway || e["step"] != "publish" || e["class"] != "unknown" {
		t.Fatalf("status = %d, error = %v", rec.Code, e)
	}
	rec = postReel(t, h, "insta-loja", "k1", oneReel)
	e = decodeMap(t, rec)["error"].(map[string]any)
	if rec.Code != http.StatusConflict || e["class"] != "permanent" {
		t.Fatalf("re-POST: status = %d, error = %v", rec.Code, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if publishCalls != 1 {
		t.Errorf("media_publish called %d times, want exactly 1", publishCalls)
	}
}

func TestReelConcurrentRePostsOnAFinishedContainerPublishOnce(t *testing.T) {
	fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS", "FINISHED"}}
	h := publishRoute(t, fake.server(t))
	if rec := postReel(t, h, "insta-loja", "k1", oneReel); rec.Code != http.StatusAccepted {
		t.Fatalf("first: status = %d", rec.Code)
	}

	const n = 12
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = postReel(t, h, "insta-loja", "k1", oneReel).Code
		}(i)
	}
	close(start)
	wg.Wait()

	if got := countPath(fake, "/media_publish"); got != 1 {
		t.Errorf("media_publish called %d times by %d concurrent re-POSTs, want exactly 1", got, n)
	}
	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		} else if c != http.StatusConflict {
			t.Errorf("unexpected status %d", c)
		}
	}
	if ok == 0 {
		t.Error("no re-POST got 200")
	}
}

func TestReelValidationRefusesBeforeMeta(t *testing.T) {
	cases := map[string]string{
		"no video_url":      `{}`,
		"http video_url":    `{"video_url":"http://vid.example.invalid/a.mp4"}`,
		"relative url":      `{"video_url":"/a.mp4"}`,
		"http cover":        `{"video_url":"https://vid.example.invalid/a.mp4","cover_url":"http://x.example.invalid/c.jpg"}`,
		"negative offset":   `{"video_url":"https://vid.example.invalid/a.mp4","thumb_offset_ms":-1}`,
		"fractional offset": `{"video_url":"https://vid.example.invalid/a.mp4","thumb_offset_ms":1.5}`,
		"cover and offset":  `{"video_url":"https://vid.example.invalid/a.mp4","cover_url":"https://x.example.invalid/c.jpg","thumb_offset_ms":0}`,
		"long caption":      `{"video_url":"https://vid.example.invalid/a.mp4","caption":"` + strings.Repeat("a", 2201) + `"}`,
		"not json":          `nope`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakePublishGraph{}
			h := publishRoute(t, fake.server(t))
			rec := postReel(t, h, "insta-loja", "k1", body)
			e := decodeMap(t, rec)["error"].(map[string]any)
			if rec.Code != http.StatusBadRequest || e["class"] != "permanent" {
				t.Errorf("status = %d, error = %v", rec.Code, e)
			}
			if n := len(fake.snapshot()); n != 0 {
				t.Errorf("Meta was called %d times", n)
			}
		})
	}
}

func TestReelCoverAndOffsetTogetherSaysWhy(t *testing.T) {
	h := publishRoute(t, (&fakePublishGraph{}).server(t))
	rec := postReel(t, h, "insta-loja", "k1",
		`{"video_url":"https://vid.example.invalid/a.mp4","cover_url":"https://x.example.invalid/c.jpg","thumb_offset_ms":0}`)
	if !strings.Contains(rec.Body.String(), "send cover_url or thumb_offset_ms, not both") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestReelRequiresIdempotencyKey(t *testing.T) {
	fake := &fakePublishGraph{}
	h := publishRoute(t, fake.server(t))
	if rec := postReel(t, h, "insta-loja", "", oneReel); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times", n)
	}
}

func TestReelSameKeyDifferentBodyIs422(t *testing.T) {
	fake := &fakePublishGraph{statuses: []string{"IN_PROGRESS"}}
	h := publishRoute(t, fake.server(t))
	postReel(t, h, "insta-loja", "k1", oneReel)
	before := len(fake.snapshot())

	rec := postReel(t, h, "insta-loja", "k1", `{"video_url":"https://vid.example.invalid/a.mp4","caption":"other"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if after := len(fake.snapshot()); after != before {
		t.Errorf("Meta was called %d extra times", after-before)
	}
}

func TestReelWhatsAppInstanceIs400Config(t *testing.T) {
	fake := &fakePublishGraph{}
	srv := fake.server(t)
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewInstagramPublishHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), srv.URL, 1<<20, meta.InstagramPublishTimings{}, InstagramOnly)

	rec := postReel(t, h, "lojinha", "k1", oneReel)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"class":"config"`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times for a WhatsApp instance", n)
	}
}
