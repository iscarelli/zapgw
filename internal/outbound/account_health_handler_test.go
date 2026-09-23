package outbound

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/iscarelli/zapgw/internal/meta"
)

// fakeAccountHealthMeta answers the TWO calls the route makes — one to the
// WABA id, one to the phone_number_id — with INDEPENDENT bodies, because
// test (a) below needs the two to DISAGREE (WABA LIMITED, number AVAILABLE)
// to prove the route picks the WORSE of the two, not just one of them.
//
// Routed by WHICH id is in the path, the SAME way production tells the two
// calls apart: storeWithConsumer (auth_test.go) gives the WABA id the "W-"
// prefix and the phone number id the "P-" prefix.
type fakeAccountHealthMeta struct {
	mu                                    sync.Mutex
	wabaStatus, phoneStatus               int
	wabaBody, phoneBody                   string
	wabaTransportFail, phoneTransportFail bool

	urls           []string
	authorizations []string
	gets           atomic.Int64
}

func (m *fakeAccountHealthMeta) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.gets.Add(1)
		m.mu.Lock()
		m.urls = append(m.urls, r.URL.String())
		m.authorizations = append(m.authorizations, r.Header.Get("Authorization"))
		isWABA := strings.HasPrefix(r.URL.Path, "/W-")
		status, body, transportFail := m.phoneStatus, m.phoneBody, m.phoneTransportFail
		if isWABA {
			status, body, transportFail = m.wabaStatus, m.wabaBody, m.wabaTransportFail
		}
		m.mu.Unlock()

		if transportFail {
			// Close the connection without a response — the deterministic
			// way to force a TRANSPORT error client-side, without sleeping
			// past a deadline.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("ResponseWriter does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (m *fakeAccountHealthMeta) respondWABA(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wabaStatus, m.wabaBody, m.wabaTransportFail = status, body, false
}

func (m *fakeAccountHealthMeta) respondPhone(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phoneStatus, m.phoneBody, m.phoneTransportFail = status, body, false
}

func (m *fakeAccountHealthMeta) failWABATransport() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wabaTransportFail = true
}

// healthyAccountMeta answers both calls with an AVAILABLE, error-free,
// funding-absent WABA and phone number — the baseline every test starts
// from and overrides one call away from.
func healthyAccountMeta() *fakeAccountHealthMeta {
	m := &fakeAccountHealthMeta{}
	m.respondWABA(http.StatusOK, `{"health_status":{"can_send_message":"AVAILABLE",`+
		`"entities":[{"entity_type":"WABA","id":"waba-real-id","can_send_message":"AVAILABLE"}]}}`)
	m.respondPhone(http.StatusOK, `{"health_status":{"can_send_message":"AVAILABLE",`+
		`"entities":[{"entity_type":"PHONE_NUMBER","id":"phone-real-id","can_send_message":"AVAILABLE"}]}}`)
	return m
}

func testAccountHealthHandler(t *testing.T, m *fakeAccountHealthMeta, active ...string) http.Handler {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	srv := m.server(t)
	return NewAccountHealthHandler(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), AllTypes)
}

func askAccountHealth(t *testing.T, h http.Handler, token, slug string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/"+slug+"/account-health", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type testAccountHealthErrorEntry struct {
	Code             int    `json:"code"`
	Description      string `json:"description"`
	PossibleSolution string `json:"possible_solution"`
}

type testAccountHealthEntity struct {
	EntityType     string                        `json:"entity_type"`
	CanSendMessage string                        `json:"can_send_message"`
	Errors         []testAccountHealthErrorEntry `json:"errors"`
	AdditionalInfo []string                      `json:"additional_info"`
}

type testAccountHealthResponse struct {
	CanSendMessage   string                    `json:"can_send_message"`
	HasPaymentMethod bool                      `json:"has_payment_method"`
	Entities         []testAccountHealthEntity `json:"entities"`
	CheckedAt        string                    `json:"checked_at"`
}

// (a) WABA LIMITED + number AVAILABLE -> the ACCOUNT's overall
// can_send_message is LIMITED (the worse of the two), and both entities
// (with the WABA's error) survive in the response — but NEITHER entity's
// real Meta id does.
func TestAccountHealthPicksTheWorseOfWABAAndNumber(t *testing.T) {
	m := healthyAccountMeta()
	m.respondWABA(http.StatusOK, `{"health_status":{"can_send_message":"LIMITED","entities":[`+
		`{"entity_type":"WABA","id":"waba-real-id","can_send_message":"LIMITED",`+
		`"errors":[{"error_code":139012,"error_description":"quality dropped","possible_solution":"wait it out"}],`+
		`"additional_info":["informational note"]}]}}`)
	h := testAccountHealthHandler(t, m, "lojinha")

	rec := askAccountHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 2 {
		t.Fatalf("Graph API calls = %d, want 2 (one to the WABA, one to the number)", n)
	}

	var resp testAccountHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.CanSendMessage != "LIMITED" {
		t.Errorf("can_send_message = %q, want %q (the WORSE of WABA=LIMITED and number=AVAILABLE)",
			resp.CanSendMessage, "LIMITED")
	}
	if len(resp.Entities) != 2 {
		t.Fatalf("entities = %d, want 2 (WABA + number)", len(resp.Entities))
	}
	var waba *testAccountHealthEntity
	for i := range resp.Entities {
		if resp.Entities[i].EntityType == "WABA" {
			waba = &resp.Entities[i]
		}
	}
	if waba == nil {
		t.Fatal("no WABA entity in the response")
	}
	if len(waba.Errors) != 1 || waba.Errors[0].Code != 139012 ||
		waba.Errors[0].Description != "quality dropped" || waba.Errors[0].PossibleSolution != "wait it out" {
		t.Errorf("WABA entity errors = %+v, want the LITERAL Meta fields", waba.Errors)
	}
	if len(waba.AdditionalInfo) != 1 || waba.AdditionalInfo[0] != "informational note" {
		t.Errorf("WABA entity additional_info = %v", waba.AdditionalInfo)
	}
	if body := rec.Body.String(); strings.Contains(body, "waba-real-id") || strings.Contains(body, "phone-real-id") {
		t.Errorf("the response leaked a Meta entity id: %s", body)
	}
}

// (b) has_payment_method mirrors primary_funding_id's PRESENCE, and the
// VALUE itself never appears anywhere in the body.
func TestAccountHealthPaymentMethodPresenceWithoutLeakingTheValue(t *testing.T) {
	absent := healthyAccountMeta() // baseline never sends primary_funding_id
	h := testAccountHealthHandler(t, absent, "lojinha")
	rec := askAccountHealth(t, h, "token-do-a", "lojinha")
	var resp testAccountHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.HasPaymentMethod {
		t.Error("has_payment_method = true with primary_funding_id absent, want false")
	}

	present := healthyAccountMeta()
	present.respondWABA(http.StatusOK, `{"health_status":{"can_send_message":"AVAILABLE",`+
		`"entities":[{"entity_type":"WABA","can_send_message":"AVAILABLE"}]},`+
		`"primary_funding_id":"1234567890123"}`)
	h2 := testAccountHealthHandler(t, present, "lojinha")
	rec2 := askAccountHealth(t, h2, "token-do-a", "lojinha")
	var resp2 testAccountHealthResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec2.Body.String())
	}
	if !resp2.HasPaymentMethod {
		t.Error("has_payment_method = false with primary_funding_id present, want true")
	}
	if strings.Contains(rec2.Body.String(), "1234567890123") {
		t.Errorf("the primary_funding_id VALUE leaked into the response: %s", rec2.Body.String())
	}
}

// (c) Meta failing EITHER call — a 5xx, a refused token, or a transport
// failure — never becomes 200. respondUnhealthy's classification (same
// function health_handler.go uses) applies unchanged.
func TestAccountHealthNeverAnswers200WhenMetaFailsEitherCall(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(m *fakeAccountHealthMeta)
	}{
		{"waba 5xx", func(m *fakeAccountHealthMeta) {
			m.respondWABA(http.StatusInternalServerError, `{"error":{"message":"boom","code":1}}`)
		}},
		{"phone invalid token", func(m *fakeAccountHealthMeta) {
			m.respondPhone(http.StatusUnauthorized, `{"error":{"message":"Invalid OAuth access token","code":190}}`)
		}},
		{"waba transport failure", func(m *fakeAccountHealthMeta) {
			m.failWABATransport()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := healthyAccountMeta()
			c.break_(m)
			h := testAccountHealthHandler(t, m, "lojinha")
			rec := askAccountHealth(t, h, "token-do-a", "lojinha")
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// (d) A 200 without a RECOGNIZED can_send_message — absent, or an
// unexpected literal — never becomes AVAILABLE by omission: it is 503,
// class `unknown`, same as "the gateway could not talk to Meta at all".
func TestAccountHealthAnswers503UnknownWhenMetaOmitsOrGarblesHealthStatus(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no health_status at all", `{"id":"waba-real-id"}`},
		{"unrecognized can_send_message", `{"health_status":{"can_send_message":"SOMETHING_NEW","entities":[]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := healthyAccountMeta()
			m.respondWABA(http.StatusOK, c.body)
			h := testAccountHealthHandler(t, m, "lojinha")
			rec := askAccountHealth(t, h, "token-do-a", "lojinha")
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
			}
			var errBody errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
				t.Fatalf("error body does not deserialize: %v (body = %q)", err, rec.Body.String())
			}
			if errBody.Error.Class != string(meta.ClassUnknown) {
				t.Errorf("class = %q, want %q — an unreadable health_status must never be mistaken for AVAILABLE",
					errBody.Error.Class, meta.ClassUnknown)
			}
		})
	}
}

// (e) An Instagram instance answers 200 NotApplicable and makes ZERO calls
// to Meta — health_status has no documented equivalent on
// graph.instagram.com (same absence T-104 already measured for the sibling
// probe's CheckCredential).
func TestAccountHealthInstagramAnswersNotApplicableWithoutCallingMeta(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	m := healthyAccountMeta()
	srv := m.server(t)
	h := NewAccountHealthHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), AllTypes)

	rec := askAccountHealth(t, h, "token-do-a", "insta-loja")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a READ route does not refuse); body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("the probe talked to Meta %d time(s) for an Instagram instance", n)
	}
	var resp struct {
		Verdict   string `json:"verdict"`
		CheckedAt string `json:"checked_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.Verdict != NotApplicable {
		t.Errorf("verdict = %q, want %q", resp.Verdict, NotApplicable)
	}
	if resp.CheckedAt == "" {
		t.Error("checked_at is empty")
	}
}

// A paused instance answers 503 without spending EITHER call to Meta — same
// reasoning as the sibling health probe.
func TestAccountHealthWithPausedInstanceAnswers503WithoutCallingMeta(t *testing.T) {
	m := healthyAccountMeta()
	h := testAccountHealthHandler(t, m) // does NOT activate: instance is born paused

	rec := askAccountHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("the probe talked to Meta %d time(s) for a PAUSED instance", n)
	}
}

func TestAccountHealthRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	m := healthyAccountMeta()
	h := testAccountHealthHandler(t, m, "lojinha")

	if rec := askAccountHealth(t, h, "", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	if rec := askAccountHealth(t, h, "token-errado", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", rec.Code)
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("the probe talked to Meta %d time(s) without an authenticated consumer", n)
	}
}

// REQUIREMENT 3 also applies here: system A's token does not query B's
// instance, and does not spend a call to Meta finding that out.
func TestAccountHealthRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	m := healthyAccountMeta()
	h := testAccountHealthHandler(t, m, "lojinha", "clinica")

	rec := askAccountHealth(t, h, "token-do-a", "clinica")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 0 {
		t.Errorf("the probe talked to Meta %d time(s) for another system's instance", n)
	}
}

// The sending token goes in the HEADER, never in the URL, for BOTH calls.
func TestAccountHealthSendsTheTokenInTheHeaderNeverInTheURL(t *testing.T) {
	m := healthyAccountMeta()
	h := testAccountHealthHandler(t, m, "lojinha")

	askAccountHealth(t, h, "token-do-a", "lojinha")

	urls, authorizations := func() ([]string, []string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return append([]string(nil), m.urls...), append([]string(nil), m.authorizations...)
	}()
	if len(urls) != 2 {
		t.Fatalf("requests seen = %d, want 2", len(urls))
	}
	for i, u := range urls {
		if strings.Contains(u, "t-lojinha") {
			t.Errorf("request %d: the send token went into the URL: %q", i, u)
		}
		if authorizations[i] != "Bearer t-lojinha" {
			t.Errorf("request %d: Authorization = %q, want %q", i, authorizations[i], "Bearer t-lojinha")
		}
	}
	if !strings.Contains(urls[0]+urls[1], "W-lojinha") || !strings.Contains(urls[0]+urls[1], "P-lojinha") {
		t.Errorf("the two calls did not hit both the WABA id and the phone_number_id: %v", urls)
	}
}
