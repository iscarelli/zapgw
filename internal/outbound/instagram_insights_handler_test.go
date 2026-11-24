// Tests for T-271: GET /v1/instances/{slug}/instagram-insights and
// GET /v1/instances/{slug}/instagram-media.
//
// Every server is a local httptest fake Graph; the gateway never talks to the
// real Meta here. All ids and values are synthetic.
package outbound

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

type insightsCall struct {
	path, rawQuery, auth, url string
}

// fakeInsightsGraph answers the insights/media/me calls. Each body can be
// overridden; `mediaFn` lets a test answer the media list per fields value
// and `mediaInsights` per media id.
type fakeInsightsGraph struct {
	mu             sync.Mutex
	calls          []insightsCall
	accountBody    string
	meBody         string
	mediaFn        func(fields string) (int, string)
	mediaInsightFn func(id string) (int, string)
	// mediaInsightQFn, when set, sees the metric list of each call too.
	mediaInsightQFn func(id, metric string) (int, string)
}

func (f *fakeInsightsGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, insightsCall{r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.URL.String()})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		write := func(status int, body string) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
		switch {
		case r.URL.Path == "/IGID1/insights":
			if f.accountBody != "" {
				write(200, f.accountBody)
				return
			}
			write(200, `{"data":[
				{"name":"reach","total_value":{"value":1200}},
				{"name":"accounts_engaged","total_value":{"value":80}},
				{"name":"views","total_value":{"value":5000}},
				{"name":"total_interactions","total_value":{"value":310}},
				{"name":"follows_and_unfollows","total_value":{"value":-3}},
				{"name":"profile_links_taps","total_value":{"value":12}}]}`)
		case r.URL.Path == "/me":
			if f.meBody != "" {
				write(200, f.meBody)
				return
			}
			write(200, `{"followers_count":4321,"media_count":98}`)
		case r.URL.Path == "/IGID1/media":
			fields := r.URL.Query().Get("fields")
			if f.mediaFn != nil {
				st, b := f.mediaFn(fields)
				write(st, b)
				return
			}
			write(200, `{"data":[
				{"id":"m1","media_type":"IMAGE","timestamp":"2026-11-20T10:00:00+0000","permalink":"https://www.example.invalid/p/m1/","caption":"first","like_count":5,"comments_count":1},
				{"id":"m2","media_type":"CAROUSEL_ALBUM","timestamp":"2026-11-19T10:00:00+0000","permalink":"https://www.example.invalid/p/m2/"}],
				"paging":{"cursors":{"before":"B","after":"CUR2"},"next":"https://graph.example.invalid/next"}}`)
		case strings.HasSuffix(r.URL.Path, "/insights"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/insights")
			if f.mediaInsightQFn != nil {
				st, b := f.mediaInsightQFn(id, r.URL.Query().Get("metric"))
				write(st, b)
				return
			}
			if f.mediaInsightFn != nil {
				st, b := f.mediaInsightFn(id)
				write(st, b)
				return
			}
			write(200, `{"data":[
				{"name":"reach","values":[{"value":100}]},
				{"name":"saved","values":[{"value":4}]},
				{"name":"shares","values":[{"value":2}]},
				{"name":"views","values":[{"value":150}]},
				{"name":"total_interactions","values":[{"value":20}]}]}`)
		default:
			write(404, `{"error":{"message":"unexpected path in the fake","code":100}}`)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (f *fakeInsightsGraph) snapshot() []insightsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]insightsCall(nil), f.calls...)
}

func insightsRoute(t *testing.T, srv *httptest.Server) http.Handler {
	t.Helper()
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	return NewInstagramInsightsHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), "http://127.0.0.1:1"), srv.URL, InstagramOnly)
}

func getInsights(t *testing.T, h http.Handler, slug, route, query string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/v1/instances/" + slug + "/" + route
	if query != "" {
		target += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer token-do-a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestInstagramInsightsSendsTheExactQueryAndMapsTheNumbers(t *testing.T) {
	fake := &fakeInsightsGraph{}
	h := insightsRoute(t, fake.server(t))

	rec := getInsights(t, h, "insta-loja", "instagram-insights", "since=1700000000&until=1700086400")
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["since"] != float64(1700000000) || got["until"] != float64(1700086400) ||
		got["followers_count"] != float64(4321) || got["media_count"] != float64(98) {
		t.Errorf("response = %v", got)
	}
	metrics := got["metrics"].(map[string]any)
	if metrics["reach"] != float64(1200) || metrics["follows_and_unfollows"] != float64(-3) || len(metrics) != 6 {
		t.Errorf("metrics = %v", metrics)
	}

	calls := fake.snapshot()
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want 2", calls)
	}
	wantQuery := "metric=reach%2Caccounts_engaged%2Cviews%2Ctotal_interactions%2Cfollows_and_unfollows%2Cprofile_links_taps" +
		"&metric_type=total_value&period=day&since=1700000000&until=1700086400"
	if calls[0].path != "/IGID1/insights" || calls[0].rawQuery != wantQuery {
		t.Errorf("insights call = %s ?%s\nwant ?%s", calls[0].path, calls[0].rawQuery, wantQuery)
	}
	if calls[1].path != "/me" || calls[1].rawQuery != "fields=followers_count%2Cmedia_count" {
		t.Errorf("me call = %s ?%s", calls[1].path, calls[1].rawQuery)
	}
	for _, c := range calls {
		if c.auth != "Bearer t-insta-loja" {
			t.Errorf("%s: Authorization = %q", c.path, c.auth)
		}
	}
}

func TestInstagramInsightsMissingMetricIsNullNeverZero(t *testing.T) {
	fake := &fakeInsightsGraph{
		accountBody: `{"data":[{"name":"reach","total_value":{"value":0}},{"name":"views"},{"name":"unasked","total_value":{"value":9}}]}`,
		meBody:      `{"media_count":5}`,
	}
	h := insightsRoute(t, fake.server(t))

	rec := getInsights(t, h, "insta-loja", "instagram-insights", "since=10&until=20")
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	metrics := got["metrics"].(map[string]any)
	if v, present := metrics["reach"]; !present || v != float64(0) {
		t.Errorf("reach = %v (present=%v), want a real 0", v, present)
	}
	for _, name := range []string{"accounts_engaged", "views", "total_interactions", "follows_and_unfollows", "profile_links_taps"} {
		if v, present := metrics[name]; !present || v != nil {
			t.Errorf("%s = %v (present=%v), want null", name, v, present)
		}
	}
	if _, extra := metrics["unasked"]; extra {
		t.Errorf("a metric nobody asked for leaked: %v", metrics)
	}
	if v, present := got["followers_count"]; !present || v != nil {
		t.Errorf("followers_count = %v (present=%v), want null", v, present)
	}
}

func TestInstagramInsightsBadWindowIs400WithZeroMetaCalls(t *testing.T) {
	fake := &fakeInsightsGraph{}
	h := insightsRoute(t, fake.server(t))
	for _, q := range []string{
		"", "since=10", "until=20", "since=abc&until=20", "since=10&until=-5", "since=-1&until=20",
		"since=20&until=10", "since=10&until=10", "since=10&until=99999999999999999999", "since=+5&until=20",
	} {
		if rec := getInsights(t, h, "insta-loja", "instagram-insights", q); rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", q, rec.Code)
		}
	}
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times for refused requests, want 0", n)
	}
}

func TestInstagramInsightsMetaRefusalPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"bad metric","code":100,"fbtrace_id":"TRACE-SYNTH-3"}}`))
	}))
	t.Cleanup(srv.Close)
	h := insightsRoute(t, srv)
	rec := getInsights(t, h, "insta-loja", "instagram-insights", "since=10&until=20")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bad metric") {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
}

func TestInstagramMediaPagingPassesLimitAfterAndReturnsNextCursor(t *testing.T) {
	fake := &fakeInsightsGraph{}
	h := insightsRoute(t, fake.server(t))

	rec := getInsights(t, h, "insta-loja", "instagram-media", "limit=2&after=CUR1")
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["next_cursor"] != "CUR2" {
		t.Errorf("next_cursor = %v", got["next_cursor"])
	}
	items := got["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	first := items[0].(map[string]any)
	if first["id"] != "m1" || first["caption"] != "first" || first["like_count"] != float64(5) ||
		first["media_type"] != "IMAGE" || first["permalink"] == "" {
		t.Errorf("first item = %v", first)
	}
	ins := first["insights"].(map[string]any)
	if ins["reach"] != float64(100) || ins["profile_visits"] != nil || len(ins) != 6 {
		t.Errorf("first insights = %v (profile_visits must be null: Meta did not send it)", ins)
	}
	second := items[1].(map[string]any)
	if second["caption"] != nil || second["like_count"] != nil || second["comments_count"] != nil {
		t.Errorf("second item = %v, want nulls", second)
	}

	var list insightsCall
	for _, c := range fake.snapshot() {
		if c.path == "/IGID1/media" {
			list = c
		}
	}
	q := httptest.NewRequest(http.MethodGet, "/?"+list.rawQuery, nil).URL.Query()
	if q.Get("limit") != "2" || q.Get("after") != "CUR1" ||
		q.Get("fields") != "id,media_type,timestamp,permalink,caption,like_count,comments_count" {
		t.Errorf("list query = %v", q)
	}
	var perItem []insightsCall
	for _, c := range fake.snapshot() {
		if strings.HasSuffix(c.path, "/insights") {
			perItem = append(perItem, c)
		}
	}
	if len(perItem) != 2 {
		t.Fatalf("per-item insights calls = %v", perItem)
	}
	for _, c := range perItem {
		if c.rawQuery != "metric=reach%2Csaved%2Cshares%2Cviews%2Ctotal_interactions%2Cprofile_visits" {
			t.Errorf("%s ?%s", c.path, c.rawQuery)
		}
	}
}

func TestInstagramMediaDefaultLimitIs10AndLastPageHasNullCursor(t *testing.T) {
	fake := &fakeInsightsGraph{mediaFn: func(string) (int, string) {
		return 200, `{"data":[{"id":"m1","media_type":"IMAGE","timestamp":"t","permalink":"p"}],"paging":{"cursors":{"after":"LAST"}}}`
	}}
	h := insightsRoute(t, fake.server(t))
	rec := getInsights(t, h, "insta-loja", "instagram-media", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if v, present := decodeMap(t, rec)["next_cursor"]; !present || v != nil {
		t.Errorf("next_cursor = %v (present=%v), want null on the last page", v, present)
	}
	for _, c := range fake.snapshot() {
		if c.path == "/IGID1/media" && !strings.Contains(c.rawQuery, "limit=10") {
			t.Errorf("list query = %s, want limit=10", c.rawQuery)
		}
	}
}

func TestInstagramMediaBadLimitIs400WithZeroMetaCalls(t *testing.T) {
	fake := &fakeInsightsGraph{}
	h := insightsRoute(t, fake.server(t))
	for _, q := range []string{"limit=0", "limit=26", "limit=abc", "limit=-1", "limit=", "limit=1.5"} {
		if rec := getInsights(t, h, "insta-loja", "instagram-media", q); rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", q, rec.Code)
		}
	}
	for _, q := range []string{"limit=1", "limit=25"} {
		if rec := getInsights(t, h, "insta-loja", "instagram-media", q); rec.Code != 200 {
			t.Errorf("query %q: status = %d, want 200", q, rec.Code)
		}
	}
	for _, c := range fake.snapshot() {
		if strings.Contains(c.rawQuery, "limit=0") || strings.Contains(c.rawQuery, "limit=26") {
			t.Errorf("a refused limit reached Meta: %s", c.rawQuery)
		}
	}
}

func TestInstagramMediaOneItemInsightsFailureDoesNotFailThePage(t *testing.T) {
	fake := &fakeInsightsGraph{mediaInsightFn: func(id string) (int, string) {
		if id == "m2" {
			return 400, `{"error":{"message":"media posted before business account conversion","code":100,"error_subcode":2108006,"fbtrace_id":"TRACE-SYNTH-4"}}`
		}
		return 200, `{"data":[{"name":"reach","values":[{"value":100}]}]}`
	}}
	h := insightsRoute(t, fake.server(t))
	rec := getInsights(t, h, "insta-loja", "instagram-media", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	items := decodeMap(t, rec)["items"].([]any)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	if first["insights"] == nil || first["insights_error"] != nil {
		t.Errorf("first item should be intact: %v", first)
	}
	if v, present := second["insights"]; !present || v != nil {
		t.Errorf("second insights = %v (present=%v), want null", v, present)
	}
	e, _ := second["insights_error"].(map[string]any)
	if e == nil || e["meta_code"] != float64(100) || e["meta_subcode"] != float64(2108006) ||
		!strings.Contains(e["message"].(string), "business account conversion") {
		t.Errorf("insights_error = %v", second["insights_error"])
	}
}

const (
	withVisits     = "reach,saved,shares,views,total_interactions,profile_visits"
	withoutVisits  = "reach,saved,shares,views,total_interactions"
	rejectedVisits = `{"error":{"message":"The Media Insights API does not support the profile_visits metric for this media product type.","code":100,"fbtrace_id":"TRACE-SYNTH-5"}}`
)

func metricQueries(calls []insightsCall, id string) []string {
	var out []string
	for _, c := range calls {
		if c.path == "/"+id+"/insights" {
			out = append(out, c.rawQuery)
		}
	}
	return out
}

// Meta rejects the whole request for one unsupported metric (Reels): the item
// retries once without profile_visits and keeps the rest.
func TestInstagramMediaInsightsCode100RetriesWithoutProfileVisits(t *testing.T) {
	fake := &fakeInsightsGraph{mediaInsightQFn: func(id, metric string) (int, string) {
		if id == "m2" && metric == withVisits {
			return 400, rejectedVisits
		}
		return 200, `{"data":[{"name":"reach","values":[{"value":100}]},{"name":"saved","values":[{"value":4}]}]}`
	}}
	h := insightsRoute(t, fake.server(t))
	rec := getInsights(t, h, "insta-loja", "instagram-media", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	second := decodeMap(t, rec)["items"].([]any)[1].(map[string]any)
	ins, _ := second["insights"].(map[string]any)
	if ins == nil || ins["reach"] != float64(100) || ins["saved"] != float64(4) {
		t.Fatalf("insights = %v", second["insights"])
	}
	if v, present := ins["profile_visits"]; !present || v != nil {
		t.Errorf("profile_visits = %v (present=%v), want null", v, present)
	}
	if second["insights_error"] != nil {
		t.Errorf("insights_error = %v, want none", second["insights_error"])
	}
	enc := func(s string) string { return "metric=" + strings.ReplaceAll(s, ",", "%2C") }
	got := metricQueries(fake.snapshot(), "m2")
	if len(got) != 2 || got[0] != enc(withVisits) || got[1] != enc(withoutVisits) {
		t.Errorf("m2 queries = %v", got)
	}
	if g := metricQueries(fake.snapshot(), "m1"); len(g) != 1 || g[0] != enc(withVisits) {
		t.Errorf("m1 (IMAGE path) queries = %v, want exactly one with profile_visits", g)
	}
}

// A non-100 error is not retried.
func TestInstagramMediaInsightsNon100IsNotRetried(t *testing.T) {
	fake := &fakeInsightsGraph{mediaInsightQFn: func(id, metric string) (int, string) {
		if id == "m2" {
			return 500, `{"error":{"message":"transient","code":2,"fbtrace_id":"TRACE-SYNTH-6"}}`
		}
		return 200, `{"data":[{"name":"reach","values":[{"value":100}]}]}`
	}}
	h := insightsRoute(t, fake.server(t))
	rec := getInsights(t, h, "insta-loja", "instagram-media", "")
	second := decodeMap(t, rec)["items"].([]any)[1].(map[string]any)
	if second["insights_error"] == nil || second["insights"] != nil {
		t.Errorf("second = %v", second)
	}
	if got := metricQueries(fake.snapshot(), "m2"); len(got) != 1 {
		t.Errorf("m2 calls = %v, want exactly one", got)
	}
}

// If the retry fails too, the item carries the RETRY's error.
func TestInstagramMediaInsightsRetryFailingCarriesTheRetrysError(t *testing.T) {
	fake := &fakeInsightsGraph{mediaInsightQFn: func(id, metric string) (int, string) {
		if id == "m2" && metric == withVisits {
			return 400, rejectedVisits
		}
		if id == "m2" {
			return 400, `{"error":{"message":"second refusal","code":100,"error_subcode":2108006,"fbtrace_id":"TRACE-SYNTH-7"}}`
		}
		return 200, `{"data":[{"name":"reach","values":[{"value":100}]}]}`
	}}
	h := insightsRoute(t, fake.server(t))
	rec := getInsights(t, h, "insta-loja", "instagram-media", "")
	second := decodeMap(t, rec)["items"].([]any)[1].(map[string]any)
	e, _ := second["insights_error"].(map[string]any)
	if e == nil || !strings.Contains(e["message"].(string), "second refusal") || second["insights"] != nil {
		t.Errorf("second = %v", second)
	}
	if got := metricQueries(fake.snapshot(), "m2"); len(got) != 2 {
		t.Errorf("m2 calls = %v, want exactly two", got)
	}
}

// The docs flag `caption` as Facebook-Login-only. If Meta refuses the field
// list that has it, the call retries ONCE without it and the caption is null.
func TestInstagramMediaCaptionRejectedRetriesOnceWithoutIt(t *testing.T) {
	var lists []string
	fake := &fakeInsightsGraph{}
	fake.mediaFn = func(fields string) (int, string) {
		fake.mu.Lock()
		lists = append(lists, fields)
		fake.mu.Unlock()
		if strings.Contains(fields, "caption") {
			return 400, `{"error":{"message":"(#100) caption is not a valid field","code":100}}`
		}
		return 200, `{"data":[{"id":"m1","media_type":"IMAGE","timestamp":"t","permalink":"p","like_count":1}]}`
	}
	h := insightsRoute(t, fake.server(t))
	rec := getInsights(t, h, "insta-loja", "instagram-media", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if len(lists) != 2 || !strings.Contains(lists[0], "caption") || strings.Contains(lists[1], "caption") {
		t.Fatalf("field lists = %v, want [with caption, without]", lists)
	}
	item := decodeMap(t, rec)["items"].([]any)[0].(map[string]any)
	if v, present := item["caption"]; !present || v != nil {
		t.Errorf("caption = %v (present=%v), want null", v, present)
	}
}

func TestInstagramMediaListRefusedTwiceIsAMetaError(t *testing.T) {
	fake := &fakeInsightsGraph{mediaFn: func(string) (int, string) {
		return 400, `{"error":{"message":"nope","code":100}}`
	}}
	h := insightsRoute(t, fake.server(t))
	rec := getInsights(t, h, "insta-loja", "instagram-media", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	n := 0
	for _, c := range fake.snapshot() {
		if c.path == "/IGID1/media" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("list calls = %d, want exactly 2 (one retry)", n)
	}
}

func TestInstagramInsightsWhatsAppInstanceIs400Config(t *testing.T) {
	fake := &fakeInsightsGraph{}
	srv := fake.server(t)
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewInstagramInsightsHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), srv.URL, InstagramOnly)

	for _, route := range []string{"instagram-insights", "instagram-media"} {
		rec := getInsights(t, h, "lojinha", route, "since=10&until=20")
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"class":"config"`) {
			t.Errorf("%s: status = %d, body = %s", route, rec.Code, rec.Body.String())
		}
	}
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times for a WhatsApp instance", n)
	}
}

func TestInstagramInsightsOtherConsumersInstanceIs403(t *testing.T) {
	fake := &fakeInsightsGraph{}
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
	h := NewInstagramInsightsHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), srv.URL, InstagramOnly)

	for _, route := range []string{"instagram-insights", "instagram-media"} {
		if rec := getInsights(t, h, "insta-outra", route, "since=10&until=20"); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", route, rec.Code)
		}
	}
	if n := len(fake.snapshot()); n != 0 {
		t.Errorf("Meta was called %d times for someone else's instance", n)
	}
}

func TestInstagramInsightsNeverPutTheTokenInAURLOrALogLine(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	fake := &fakeInsightsGraph{mediaInsightFn: func(string) (int, string) {
		return 400, `{"error":{"message":"nope","code":100}}`
	}}
	h := insightsRoute(t, fake.server(t))
	getInsights(t, h, "insta-loja", "instagram-insights", "since=10&until=20")
	getInsights(t, h, "insta-loja", "instagram-media", "")

	calls := fake.snapshot()
	if len(calls) == 0 {
		t.Fatal("no calls recorded")
	}
	for _, c := range calls {
		if strings.Contains(c.url, "t-insta-loja") {
			t.Errorf("token in URL: %s", c.url)
		}
	}
	if strings.Contains(logs.String(), "t-insta-loja") {
		t.Errorf("token in a log line: %s", logs.String())
	}
}
