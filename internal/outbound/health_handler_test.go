package outbound

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/meta"
)

// fakeHealthMeta is the fake Graph API that the probe queries.
//
// Mutable state under mutex and an atomic counter because httptest.Server
// serves each request on a goroutine: a raw counter here is a data race, and
// this project already paid a Critical for exactly that (docs/ARMADILHAS.md,
// "Go / concurrency").
type fakeHealthMeta struct {
	mu     sync.Mutex
	status int
	body   string

	// urls and authorizations record what arrived, to prove that the token
	// goes in the HEADER and never in the URL.
	urls           []string
	authorizations []string

	gets atomic.Int64
}

func (m *fakeHealthMeta) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.gets.Add(1)
		m.mu.Lock()
		m.urls = append(m.urls, r.URL.String())
		m.authorizations = append(m.authorizations, r.Header.Get("Authorization"))
		status, body := m.status, m.body
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

// responder swaps what the fake Graph API returns FROM HERE ON — it's how
// the test simulates the client revoking the token mid-way through the
// process's life.
func (m *fakeHealthMeta) respond(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status, m.body = status, body
}

func (m *fakeHealthMeta) seen() (urls, authorizations []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.urls...), append([]string(nil), m.authorizations...)
}

func tokenAcceptingMeta() *fakeHealthMeta {
	m := &fakeHealthMeta{}
	// The display_phone_number here is DIFFERENT from the number registered
	// in the store on purpose: the probe doesn't read Meta's success body,
	// and test (a) proves it by requiring the response to carry the number
	// that came from the STORE.
	m.respond(http.StatusOK, `{"id":"P-lojinha","display_phone_number":"+55 11 90000-0000"}`)
	return m
}

const revokedTokenBody = `{"error":{"message":"Invalid OAuth access token","code":190}}`

// testHealthHandler takes WHICH instances are activated, instead of a
// boolean: each test needs the guard it targets to be the FIRST to speak. A
// 403 test running against a paused instance would go green even with the
// bond guard removed — the refusal would come from the pause, and the
// assertion would prove something else (docs/ARMADILHAS.md, "Testes": a
// refusal test that goes green because the PRECONDITION changed proves
// nothing).
func testHealthHandler(t *testing.T, m *fakeHealthMeta, active ...string) http.Handler {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	srv := m.server(t)
	return NewHealthHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), AllTypes)
}

func askHealth(t *testing.T, h http.Handler, token, slug string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/"+slug+"/health", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type testHealthResponse struct {
	OK            bool   `json:"ok"`
	DisplayNumber string `json:"display_number"`
	VerifiedAt    string `json:"verificado_em"`
}

// (a) Meta accepts the token -> ok:true. And the probe MUST have talked to
// it: a probe that doesn't talk to Meta only says the process is standing,
// which is exactly what /v1/health already said — and it happily responds
// 200 with ALL tokens revoked.
func TestHealthAnswersOKWhenMetaAcceptsTheToken(t *testing.T) {
	m := tokenAcceptingMeta()
	h := testHealthHandler(t, m, "lojinha")

	rec := askHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 1 {
		t.Fatalf("Graph API calls = %d, want 1 — a probe that does not ask does not flag a revoked token", n)
	}

	var resp testHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if !resp.OK {
		t.Errorf("ok = %v, want true", resp.OK)
	}
	if want := testDisplayNumbers["lojinha"]; resp.DisplayNumber != want {
		t.Errorf("display_number = %q, want %q (the number comes from the STORE, not from Meta's body)",
			resp.DisplayNumber, want)
	}
	when, err := time.Parse(time.RFC3339, resp.VerifiedAt)
	if err != nil {
		t.Fatalf("verificado_em = %q is not RFC3339: %v", resp.VerifiedAt, err)
	}
	// Without cache, the timestamp is always the instant of THIS call. If
	// it comes back stale, someone is serving a cached result.
	if age := time.Since(when); age > time.Minute || age < -time.Minute {
		t.Errorf("verificado_em = %q is %v from now — the probe answered with a stale result",
			resp.VerifiedAt, age)
	}
}

// (b) Meta refuses -> 503, and the response does NOT carry the token nor
// its raw body. Meta's `error_data` can echo the payload sent (phone
// number, message text); that's why only error.message and error.code are
// read (internal/meta/errors.go).
func TestHealthWith401FromMetaAnswers503WithoutLeakingTokenNorMetaBody(t *testing.T) {
	m := tokenAcceptingMeta()
	m.respond(http.StatusUnauthorized, `{"error":{"message":"Invalid OAuth access token","code":190,`+
		`"error_data":{"details":"payload-do-cliente-nao-pode-vazar"}},`+
		`"access_token":"t-lojinha","eco":"t-lojinha"}`)
	h := testHealthHandler(t, m, "lojinha")

	rec := askHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	// "t-lojinha" is the sending token encrypted in the store (see
	// storeWithConsumer): we search for the SECRET, not the entry.
	for _, mustNotLeak := range []string{
		"t-lojinha",
		"payload-do-cliente-nao-pode-vazar",
		"error_data",
		"access_token",
	} {
		if strings.Contains(body, mustNotLeak) {
			t.Errorf("the probe's response leaked %q:\n%s", mustNotLeak, body)
		}
	}

	var errBody errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("error body does not deserialize: %v (body = %q)", err, body)
	}
	// Refused token is `config`: no retry fixes it, only a human. Calling it
	// `retryable` would send the operator to wait for something that
	// doesn't change.
	if errBody.Error.Class != string(meta.ClassConfig) {
		t.Errorf("class = %q, want %q", errBody.Error.Class, meta.ClassConfig)
	}
	if errBody.Error.MetaCode != 190 {
		t.Errorf("meta_code = %d, want 190", errBody.Error.MetaCode)
	}
}

// (c) A paused instance responds 503, like the rest of the gateway — and
// without spending a call to Meta for a channel that can't send anyway.
func TestHealthWithPausedInstanceAnswers503WithoutCallingMeta(t *testing.T) {
	m := tokenAcceptingMeta()
	h := testHealthHandler(t, m) // does NOT activate: instance is born paused

	rec := askHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("the probe talked to Meta %d time(s) for a PAUSED instance", n)
	}
	if !strings.Contains(rec.Body.String(), "pausada") {
		t.Errorf("the response does not say the instance is paused: %s", rec.Body.String())
	}
}

// NO CACHE. A cached probe lies for exactly the duration of the cache,
// which is precisely when it matters most: the client revokes the token on
// Meta and the gateway keeps saying `ok:true` until the cache expires — the
// failure this endpoint exists to expose goes back to dying silently.
// Whoever calls a probe chooses the frequency.
func TestHealthHasNoCache(t *testing.T) {
	m := tokenAcceptingMeta()
	h := testHealthHandler(t, m, "lojinha")

	if rec := askHealth(t, h, "token-do-a", "lojinha"); rec.Code != http.StatusOK {
		t.Fatalf("first call: status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// The client revokes the token on Meta between one probe and the next.
	m.respond(http.StatusUnauthorized, revokedTokenBody)

	rec := askHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("after Meta started refusing the token the probe answered %d, want 503 — "+
			"it is serving a cached result; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 2 {
		t.Errorf("Graph API calls = %d, want 2 (one per probe, always)", n)
	}
}

// REQUIREMENT 3 also applies to the probe: system A's token doesn't query
// B's instance — and doesn't even spend a call to Meta finding that out.
func TestHealthRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	m := tokenAcceptingMeta()
	// "clinica" ACTIVE on purpose: if it were paused, this test would go
	// green with the bond guard removed — the refusal would come from the
	// pause, and the 403 would never have been proven. Proven by mutation:
	// with `CanUse` disabled and clinica paused, the response was 503
	// "instancia pausada".
	h := testHealthHandler(t, m, "lojinha", "clinica")

	rec := askHealth(t, h, "token-do-a", "clinica")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("the probe talked to Meta %d time(s) for another system's instance", n)
	}
}

func TestHealthRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	m := tokenAcceptingMeta()
	h := testHealthHandler(t, m, "lojinha")

	if rec := askHealth(t, h, "", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	if rec := askHealth(t, h, "token-errado", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", rec.Code)
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("the probe talked to Meta %d time(s) without an authenticated consumer", n)
	}
}

// The sending token goes in the HEADER, never in the URL: a token in a
// query string leaks into proxy, server, and CDN logs — and the URL also
// travels inside *url.Error when the transport fails.
func TestHealthSendsTheTokenInTheHeaderNeverInTheURL(t *testing.T) {
	m := tokenAcceptingMeta()
	h := testHealthHandler(t, m, "lojinha")

	askHealth(t, h, "token-do-a", "lojinha")

	urls, authorizations := m.seen()
	if len(urls) != 1 {
		t.Fatalf("requests seen = %d, want 1", len(urls))
	}
	if strings.Contains(urls[0], "t-lojinha") {
		t.Errorf("the send token went into the URL: %q", urls[0])
	}
	if !strings.Contains(urls[0], "P-lojinha") {
		t.Errorf("the probe did not hit the instance's phone_number_id: %q", urls[0])
	}
	if want := "Bearer t-lojinha"; authorizations[0] != want {
		t.Errorf("Authorization = %q, want %q", authorizations[0], want)
	}
}

// Every HTTP handler in this project serves each request on a goroutine
// over the SAME handler; without a concurrent test, `-race` has nothing to
// detect (it already cost a Critical, see docs/ARMADILHAS.md). Here it has a
// second role: a cache introduced later would almost certainly be a shared
// map, and this test catches it both ways — by the race and by the count.
func TestHealthConcurrentDoesNotShareState(t *testing.T) {
	const calls = 50
	m := tokenAcceptingMeta()
	h := testHealthHandler(t, m, "lojinha")

	var wg sync.WaitGroup
	codes := make([]int, calls)
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = askHealth(t, h, "token-do-a", "lojinha").Code
		}()
	}
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200", i, c)
		}
	}
	if n := m.gets.Load(); n != calls {
		t.Errorf("Graph API calls = %d, want %d — one per probe, no cache", n, calls)
	}
}
