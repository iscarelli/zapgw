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

// fakeAccountHealthMeta answers the THREE calls the route makes (T-255) —
// phone_health_status (to the phone_number_id), waba_health_status and
// waba_funding (both to the waba_id, told apart by the `fields=` query
// parameter) — with INDEPENDENT bodies, because several tests below need
// them to DISAGREE (one query fails while the others succeed) to prove a
// single refused query degrades the response instead of erasing it.
//
// Routed by WHICH id is in the path, the SAME way production tells the
// calls apart: storeWithConsumer (auth_test.go) gives the WABA id the "W-"
// prefix and the phone number id the "P-" prefix.
type fakeAccountHealthMeta struct {
	mu sync.Mutex

	phoneStatus, wabaHealthStatus, wabaFundingStatus                      int
	phoneBody, wabaHealthBody, wabaFundingBody                            string
	phoneTransportFail, wabaHealthTransportFail, wabaFundingTransportFail bool

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
		fields := r.URL.Query().Get("fields")

		var status int
		var body string
		var transportFail bool
		switch {
		case !isWABA:
			status, body, transportFail = m.phoneStatus, m.phoneBody, m.phoneTransportFail
		case fields == "primary_funding_id":
			status, body, transportFail = m.wabaFundingStatus, m.wabaFundingBody, m.wabaFundingTransportFail
		default:
			status, body, transportFail = m.wabaHealthStatus, m.wabaHealthBody, m.wabaHealthTransportFail
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

func (m *fakeAccountHealthMeta) respondPhone(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phoneStatus, m.phoneBody, m.phoneTransportFail = status, body, false
}

func (m *fakeAccountHealthMeta) respondWABAHealth(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wabaHealthStatus, m.wabaHealthBody, m.wabaHealthTransportFail = status, body, false
}

func (m *fakeAccountHealthMeta) respondWABAFunding(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wabaFundingStatus, m.wabaFundingBody, m.wabaFundingTransportFail = status, body, false
}

func (m *fakeAccountHealthMeta) failPhoneTransport() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phoneTransportFail = true
}

func (m *fakeAccountHealthMeta) failWABAHealthTransport() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wabaHealthTransportFail = true
}

func (m *fakeAccountHealthMeta) failWABAFundingTransport() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wabaFundingTransportFail = true
}

// healthyAccountMeta answers all THREE calls with an AVAILABLE, error-free,
// funding-absent baseline — every test starts here and overrides one call
// away from it.
func healthyAccountMeta() *fakeAccountHealthMeta {
	m := &fakeAccountHealthMeta{}
	m.respondPhone(http.StatusOK, `{"health_status":{"can_send_message":"AVAILABLE",`+
		`"entities":[{"entity_type":"PHONE_NUMBER","id":"phone-real-id","can_send_message":"AVAILABLE"}]}}`)
	m.respondWABAHealth(http.StatusOK, `{"health_status":{"can_send_message":"AVAILABLE",`+
		`"entities":[{"entity_type":"WABA","id":"waba-real-id","can_send_message":"AVAILABLE"}]}}`)
	m.respondWABAFunding(http.StatusOK, `{}`)
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

type testAccountHealthUnavailableEntry struct {
	Query    string `json:"query"`
	Class    string `json:"class"`
	MetaCode int    `json:"meta_code"`
	Message  string `json:"message"`
}

type testAccountHealthResponse struct {
	CanSendMessage string                              `json:"can_send_message"`
	PaymentMethod  string                              `json:"payment_method"`
	Entities       []testAccountHealthEntity           `json:"entities"`
	Unavailable    []testAccountHealthUnavailableEntry `json:"unavailable"`
	CheckedAt      string                              `json:"checked_at"`
}

// WABA health LIMITED + number AVAILABLE -> the ACCOUNT's overall
// can_send_message is LIMITED (the worse of the two), and both entities
// (with the WABA's error) survive in the response — but NEITHER entity's
// real Meta id does.
func TestAccountHealthPicksTheWorseOfWABAAndNumber(t *testing.T) {
	m := healthyAccountMeta()
	m.respondWABAHealth(http.StatusOK, `{"health_status":{"can_send_message":"LIMITED","entities":[`+
		`{"entity_type":"WABA","id":"waba-real-id","can_send_message":"LIMITED",`+
		`"errors":[{"error_code":139012,"error_description":"quality dropped","possible_solution":"wait it out"}],`+
		`"additional_info":["informational note"]}]}}`)
	h := testAccountHealthHandler(t, m, "lojinha")

	rec := askAccountHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if n := m.gets.Load(); n != 3 {
		t.Fatalf("Graph API calls = %d, want 3 (phone_health_status, waba_health_status, waba_funding)", n)
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
	if len(resp.Unavailable) != 0 {
		t.Errorf("unavailable = %+v, want empty — all three queries answered", resp.Unavailable)
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

// (d of T-255's Verify) payment_method mirrors primary_funding_id's
// PRESENCE ("present"/"absent"), and the VALUE itself never appears
// anywhere in the body.
func TestAccountHealthPaymentMethodPresentOrAbsentWithoutLeakingTheValue(t *testing.T) {
	absent := healthyAccountMeta() // baseline: waba_funding answers without the field
	h := testAccountHealthHandler(t, absent, "lojinha")
	rec := askAccountHealth(t, h, "token-do-a", "lojinha")
	var resp testAccountHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.PaymentMethod != "absent" {
		t.Errorf("payment_method = %q, want %q — waba_funding answered without primary_funding_id", resp.PaymentMethod, "absent")
	}

	present := healthyAccountMeta()
	present.respondWABAFunding(http.StatusOK, `{"primary_funding_id":"1234567890123"}`)
	h2 := testAccountHealthHandler(t, present, "lojinha")
	rec2 := askAccountHealth(t, h2, "token-do-a", "lojinha")
	var resp2 testAccountHealthResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec2.Body.String())
	}
	if resp2.PaymentMethod != "present" {
		t.Errorf("payment_method = %q, want %q — primary_funding_id came back non-empty", resp2.PaymentMethod, "present")
	}
	if strings.Contains(rec2.Body.String(), "1234567890123") {
		t.Errorf("the primary_funding_id VALUE leaked into the response: %s", rec2.Body.String())
	}
}

// (a of T-255's Verify) waba_funding failing (Meta error code 10, the real
// refusal measured 2026-09-22) never becomes `503`: it degrades
// payment_method to "unavailable" and the failure lands in `unavailable`,
// while can_send_message and entities — which do NOT depend on
// waba_funding — come through untouched.
func TestAccountHealthFundingRefusalDegradesInsteadOfBlinding(t *testing.T) {
	m := healthyAccountMeta()
	m.respondWABAFunding(http.StatusBadRequest,
		`{"error":{"message":"(#10) requires that the Business that owns this App is a Business Solution Provider","code":10}}`)
	h := testAccountHealthHandler(t, m, "lojinha")

	rec := askAccountHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp testAccountHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.CanSendMessage != "AVAILABLE" {
		t.Errorf("can_send_message = %q, want %q — unaffected by waba_funding failing", resp.CanSendMessage, "AVAILABLE")
	}
	if resp.PaymentMethod != "unavailable" {
		t.Errorf("payment_method = %q, want %q", resp.PaymentMethod, "unavailable")
	}
	if len(resp.Unavailable) != 1 {
		t.Fatalf("unavailable = %+v, want exactly 1 entry", resp.Unavailable)
	}
	entry := resp.Unavailable[0]
	if entry.Query != "waba_funding" {
		t.Errorf("unavailable[0].query = %q, want %q", entry.Query, "waba_funding")
	}
	if entry.MetaCode != 10 {
		t.Errorf("unavailable[0].meta_code = %d, want 10", entry.MetaCode)
	}
}

// (b of T-255's Verify) waba_health_status failing — a Meta error OR a
// transport failure — while phone_health_status answers -> `200`,
// can_send_message comes ONLY from phone_health_status, and the failure
// lands in `unavailable` with query waba_health_status. waba_funding
// failing the same way, independently, is this test's mirror for
// PaymentMethod.
func TestAccountHealthWABAHealthRefusalDegradesInsteadOfBlinding(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(m *fakeAccountHealthMeta)
	}{
		{"waba_health_status 5xx", func(m *fakeAccountHealthMeta) {
			m.respondWABAHealth(http.StatusInternalServerError, `{"error":{"message":"boom","code":1}}`)
		}},
		{"waba_health_status transport failure", func(m *fakeAccountHealthMeta) {
			m.failWABAHealthTransport()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := healthyAccountMeta()
			c.break_(m)
			h := testAccountHealthHandler(t, m, "lojinha")

			rec := askAccountHealth(t, h, "token-do-a", "lojinha")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			var resp testAccountHealthResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
			}
			if resp.CanSendMessage != "AVAILABLE" {
				t.Errorf("can_send_message = %q, want %q — must come from phone_health_status alone", resp.CanSendMessage, "AVAILABLE")
			}
			if len(resp.Entities) != 1 || resp.Entities[0].EntityType != "PHONE_NUMBER" {
				t.Errorf("entities = %+v, want only the PHONE_NUMBER entity", resp.Entities)
			}
			if len(resp.Unavailable) != 1 || resp.Unavailable[0].Query != "waba_health_status" {
				t.Fatalf("unavailable = %+v, want exactly 1 entry for waba_health_status", resp.Unavailable)
			}
		})
	}
}

// waba_funding failing on a TRANSPORT error (not a Meta-classified one)
// degrades the same way as the Meta-code-10 case above — the failure mode
// does not change the outcome, only classifyHealthProbeError's default
// "could not talk to Meta" (class unknown) applies.
func TestAccountHealthFundingTransportFailureDegradesInsteadOfBlinding(t *testing.T) {
	m := healthyAccountMeta()
	m.failWABAFundingTransport()
	h := testAccountHealthHandler(t, m, "lojinha")

	rec := askAccountHealth(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp testAccountHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	if resp.PaymentMethod != "unavailable" {
		t.Errorf("payment_method = %q, want %q", resp.PaymentMethod, "unavailable")
	}
	if len(resp.Unavailable) != 1 || resp.Unavailable[0].Query != "waba_funding" ||
		resp.Unavailable[0].Class != string(meta.ClassUnknown) {
		t.Fatalf("unavailable = %+v, want exactly 1 entry, query waba_funding, class unknown", resp.Unavailable)
	}
}

// (c of T-255's Verify) phone_health_status failing is the ONLY failure
// that is still `503` — without it there is no signal at all — and its
// message is prefixed with the query's name so the 503 says WHO was
// refused.
func TestAccountHealthPhoneFailureIsAlways503(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(m *fakeAccountHealthMeta)
	}{
		{"phone 5xx", func(m *fakeAccountHealthMeta) {
			m.respondPhone(http.StatusInternalServerError, `{"error":{"message":"boom","code":1}}`)
		}},
		{"phone invalid token", func(m *fakeAccountHealthMeta) {
			m.respondPhone(http.StatusUnauthorized, `{"error":{"message":"Invalid OAuth access token","code":190}}`)
		}},
		{"phone transport failure", func(m *fakeAccountHealthMeta) {
			m.failPhoneTransport()
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
			var errBody errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
				t.Fatalf("error body does not deserialize: %v (body = %q)", err, rec.Body.String())
			}
			if !strings.HasPrefix(errBody.Error.Message, "phone_health_status:") {
				t.Errorf("message = %q, want it to start with %q", errBody.Error.Message, "phone_health_status:")
			}
			// waba_health_status and waba_funding must not even be reached:
			// phone_health_status failing ends the request before either.
			if n := m.gets.Load(); n != 1 {
				t.Errorf("Graph API calls = %d, want 1 (phone_health_status only)", n)
			}
		})
	}
}

// A `200` WITHOUT a RECOGNIZED can_send_message on phone_health_status —
// absent, or an unexpected literal — never becomes AVAILABLE by omission:
// it is `503`, class `unknown`, same as "the gateway could not talk to Meta
// at all". This is phone_health_status's own version of the same rule
// waba_health_status has below.
func TestAccountHealthAnswers503UnknownWhenPhoneOmitsOrGarblesHealthStatus(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no health_status at all", `{"id":"phone-real-id"}`},
		{"unrecognized can_send_message", `{"health_status":{"can_send_message":"SOMETHING_NEW","entities":[]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := healthyAccountMeta()
			m.respondPhone(http.StatusOK, c.body)
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
			if !strings.HasPrefix(errBody.Error.Message, "phone_health_status:") {
				t.Errorf("message = %q, want it to start with %q", errBody.Error.Message, "phone_health_status:")
			}
		})
	}
}

// The SAME "unrecognized health_status" condition on waba_health_status —
// unlike on phone_health_status above — degrades the response instead of
// replacing it: it lands in `unavailable` with class `unknown`, and
// can_send_message/entities still come through from phone_health_status.
func TestAccountHealthWABAUnrecognizedHealthStatusDegradesInsteadOfBlinding(t *testing.T) {
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
			m.respondWABAHealth(http.StatusOK, c.body)
			h := testAccountHealthHandler(t, m, "lojinha")
			rec := askAccountHealth(t, h, "token-do-a", "lojinha")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			var resp testAccountHealthResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
			}
			if resp.CanSendMessage != "AVAILABLE" {
				t.Errorf("can_send_message = %q, want %q", resp.CanSendMessage, "AVAILABLE")
			}
			if len(resp.Unavailable) != 1 || resp.Unavailable[0].Query != "waba_health_status" ||
				resp.Unavailable[0].Class != string(meta.ClassUnknown) {
				t.Fatalf("unavailable = %+v, want exactly 1 entry, query waba_health_status, class unknown", resp.Unavailable)
			}
		})
	}
}

// An Instagram instance answers 200 NotApplicable and makes ZERO calls to
// Meta — health_status has no documented equivalent on
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

// A paused instance answers 503 without spending ANY of the three calls to
// Meta — same reasoning as the sibling health probe.
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

// The sending token goes in the HEADER, never in the URL, for ALL THREE
// calls.
func TestAccountHealthSendsTheTokenInTheHeaderNeverInTheURL(t *testing.T) {
	m := healthyAccountMeta()
	h := testAccountHealthHandler(t, m, "lojinha")

	askAccountHealth(t, h, "token-do-a", "lojinha")

	urls, authorizations := func() ([]string, []string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return append([]string(nil), m.urls...), append([]string(nil), m.authorizations...)
	}()
	if len(urls) != 3 {
		t.Fatalf("requests seen = %d, want 3", len(urls))
	}
	for i, u := range urls {
		if strings.Contains(u, "t-lojinha") {
			t.Errorf("request %d: the send token went into the URL: %q", i, u)
		}
		if authorizations[i] != "Bearer t-lojinha" {
			t.Errorf("request %d: Authorization = %q, want %q", i, authorizations[i], "Bearer t-lojinha")
		}
	}
	joined := strings.Join(urls, " ")
	if !strings.Contains(joined, "P-lojinha") {
		t.Errorf("no call hit the phone_number_id: %v", urls)
	}
	if strings.Count(joined, "W-lojinha") != 2 {
		t.Errorf("want exactly 2 calls to the waba_id (health + funding): %v", urls)
	}
	if !strings.Contains(joined, "fields=primary_funding_id") {
		t.Errorf("no call requested primary_funding_id: %v", urls)
	}
}
