// Tests for T-269: GET /v1/instances/{slug}/instagram-users/{igsid}.
//
// Every server is a local httptest fake; the gateway never talks to the real
// Meta here. All ids and names are synthetic.
package outbound

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

const testIGSID = "17841400000000001"

type fakeInstagramGraph struct {
	mu            sync.Mutex
	hits          atomic.Int64
	status        int
	body          string
	paths, fields []string
	auths, urls   []string
}

func (f *fakeInstagramGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.fields = append(f.fields, r.URL.Query().Get("fields"))
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.urls = append(f.urls, r.URL.String())
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(s.Close)
	return s
}

func instagramUserRoute(t *testing.T, srv *httptest.Server) http.Handler {
	t.Helper()
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	return NewInstagramUserHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), "http://127.0.0.1:1"), srv.URL, InstagramOnly)
}

func getInstagramUser(t *testing.T, h http.Handler, slug, igsid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/"+slug+"/instagram-users/"+igsid, nil)
	req.Header.Set("Authorization", "Bearer token-do-a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestInstagramUserReturnsLowercasedUsernameWithoutAt(t *testing.T) {
	fake := &fakeInstagramGraph{status: 200, body: `{"username":"@Synthetic.User","name":"Synthetic Person","id":"` + testIGSID + `"}`}
	h := instagramUserRoute(t, fake.server(t))

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rec := getInstagramUser(t, h, "insta-loja", testIGSID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if got["igsid"] != testIGSID || got["username"] != "synthetic.user" || got["name"] != "Synthetic Person" {
		t.Errorf("response = %v", got)
	}

	if len(fake.paths) != 1 || fake.paths[0] != "/"+testIGSID {
		t.Errorf("path seen by Meta = %v, want [/%s]", fake.paths, testIGSID)
	}
	if !strings.Contains(fake.fields[0], "username") || !strings.Contains(fake.fields[0], "name") {
		t.Errorf("fields = %q, want username and name", fake.fields[0])
	}
	if fake.auths[0] != "Bearer t-insta-loja" {
		t.Errorf("Authorization = %q, want the instance's send token as Bearer", fake.auths[0])
	}
	if strings.Contains(fake.urls[0], "t-insta-loja") {
		t.Errorf("the token is in the request URL: %s", fake.urls[0])
	}
	if strings.Contains(logs.String(), "t-insta-loja") {
		t.Errorf("the token appeared in a log line: %s", logs.String())
	}
}

func TestInstagramUserNullNamePassesThroughAsNull(t *testing.T) {
	fake := &fakeInstagramGraph{status: 200, body: `{"username":"synthetic.user","name":null}`}
	h := instagramUserRoute(t, fake.server(t))

	rec := getInstagramUser(t, h, "insta-loja", testIGSID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if string(got["name"]) != "null" {
		t.Errorf("name = %s, want null (key present)", got["name"])
	}
}

func TestInstagramUserConsentErrorCarriesMetaCodeAndMessage(t *testing.T) {
	fake := &fakeInstagramGraph{status: 400, body: `{"error":{"message":"User consent is required to access user profile.","type":"IGApiException","code":230,"fbtrace_id":"TRACE-SYNTH-1"}}`}
	h := instagramUserRoute(t, fake.server(t))

	rec := getInstagramUser(t, h, "insta-loja", testIGSID)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200, want an error; body = %s", rec.Body.String())
	}
	var got struct {
		Error struct {
			Class    string `json:"class"`
			Message  string `json:"message"`
			MetaCode int    `json:"meta_code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if got.Error.MetaCode != 230 {
		t.Errorf("meta_code = %d, want 230", got.Error.MetaCode)
	}
	if !strings.Contains(got.Error.Message, "User consent is required") {
		t.Errorf("message = %q, want Meta's text passed through", got.Error.Message)
	}
	if !strings.Contains(rec.Body.String(), "TRACE-SYNTH-1") {
		t.Errorf("fbtrace_id not passed through: %s", rec.Body.String())
	}
}

func TestInstagramUserNonDigitIGSIDIs400WithoutCallingMeta(t *testing.T) {
	fake := &fakeInstagramGraph{status: 200, body: `{"username":"x"}`}
	h := instagramUserRoute(t, fake.server(t))

	for _, bad := range []string{"abc", "123abc", "12%2F34", "1234%20"} {
		rec := getInstagramUser(t, h, "insta-loja", bad)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("igsid %q: status = %d, want 400", bad, rec.Code)
		}
	}
	if n := fake.hits.Load(); n != 0 {
		t.Errorf("Meta was called %d times for non-digit igsids, want 0", n)
	}
}

func TestInstagramUserWhatsAppInstanceIs400Config(t *testing.T) {
	fake := &fakeInstagramGraph{status: 200, body: `{"username":"x"}`}
	srv := fake.server(t)
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")
	h := NewInstagramUserHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), srv.URL, InstagramOnly)

	rec := getInstagramUser(t, h, "lojinha", testIGSID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"class":"config"`) {
		t.Errorf("body = %s, want class config", rec.Body.String())
	}
	if n := fake.hits.Load(); n != 0 {
		t.Errorf("Meta was called %d times for a WhatsApp instance, want 0", n)
	}
}

func TestInstagramUserOtherConsumersInstanceIs403(t *testing.T) {
	fake := &fakeInstagramGraph{status: 200, body: `{"username":"x"}`}
	srv := fake.server(t)
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	if err := store.CreateInstance(config.Instance{
		Slug: "insta-outra", Type: config.TypeInstagram, IgID: "IGID2",
		AppSecret: "a", VerifyToken: "v", SendToken: "t-insta-outra",
		CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 2000,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	activateInstance(t, path, "insta-outra")
	h := NewInstagramUserHandlerWithBase(store, NewAuthenticator(store),
		meta.NewClient(srv.Client(), srv.URL), srv.URL, InstagramOnly)

	rec := getInstagramUser(t, h, "insta-outra", testIGSID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if n := fake.hits.Load(); n != 0 {
		t.Errorf("Meta was called %d times for someone else's instance, want 0", n)
	}
}
