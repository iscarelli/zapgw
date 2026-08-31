// Tests for RenewInstagramToken (T-098) — the network call, isolated
// from the loop that decides WHEN to call it (that loop is
// internal/outbound/instagram_renewer.go and has its own tests). Here
// it's only the request's FORM and the response's reading.
package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestRenewInstagramTokenReturnsTheNewAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"IGQ-TOKEN-NOVO","token_type":"bearer","expires_in":5184000}`))
	}))
	defer srv.Close()

	fresh, err := testClient(srv).RenewInstagramToken(context.Background(), srv.URL, "token-atual")
	if err != nil {
		t.Fatalf("RenewInstagramToken: %v", err)
	}
	if fresh != "IGQ-TOKEN-NOVO" {
		t.Errorf("token novo = %q, quero IGQ-TOKEN-NOVO", fresh)
	}
}

// THE SAME discipline as TestSendMessageRefuses200WithoutAnID: a Meta 200 does
// NOT prove an access_token came with it.
func TestRenewInstagramTokenRefuses200WithoutAnAccessToken(t *testing.T) {
	cases := []string{
		`{}`,
		`{"access_token":""}`,
		`{"access_token":"   "}`,
		`{"access_token":123}`,
		`null`,
		``,
		`nao e json`,
	}
	for _, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))

		fresh, err := testClient(srv).RenewInstagramToken(context.Background(), srv.URL, "token-atual")
		srv.Close()

		if err == nil {
			t.Errorf("corpo %q devolveu SUCESSO com token %q", body, fresh)
			continue
		}
		if !errors.Is(err, ErrRenewalWithoutAccessToken) {
			t.Errorf("corpo %q: erro = %v, quero ErrRenewalWithoutAccessToken", body, err)
		}
		if fresh != "" {
			t.Errorf("corpo %q devolveu token %q junto com erro", body, fresh)
		}
	}
}

func TestRenewInstagramTokenClassifiesMetasError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"token com menos de 24h","code":190}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).RenewInstagramToken(context.Background(), srv.URL, "token-atual")

	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("erro = %v, quero *MetaError", err)
	}
	if me.MetaCode != 190 {
		t.Errorf("MetaCode = %d, quero 190", me.MetaCode)
	}
}

// The request has to go to /refresh_access_token, with grant_type and
// access_token in the QUERY STRING — the exact form Meta requires (see the
// Source at the top of instagram.go). The token goes in the QUERY here, as
// a deliberate and documented exception — see RenewInstagramToken's
// comment.
func TestRenewInstagramTokenBuildsTheExactRequest(t *testing.T) {
	var path, method string
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"X"}`))
	}))
	defer srv.Close()

	if _, err := testClient(srv).RenewInstagramToken(context.Background(), srv.URL, "token-atual-de-verdade"); err != nil {
		t.Fatalf("RenewInstagramToken: %v", err)
	}
	if method != http.MethodGet {
		t.Errorf("metodo = %q, quero GET", method)
	}
	if path != "/refresh_access_token" {
		t.Errorf("caminho = %q, quero /refresh_access_token", path)
	}
	if got := query.Get("grant_type"); got != "ig_refresh_token" {
		t.Errorf("grant_type = %q, quero ig_refresh_token", got)
	}
	if got := query.Get("access_token"); got != "token-atual-de-verdade" {
		t.Errorf("access_token = %q, quero token-atual-de-verdade", got)
	}
}

func TestRenewInstagramTokenTransportFailure(t *testing.T) {
	// Dead address (nothing listens on loopback port 1): the call never
	// gets a response, and the error has to come out as a TRANSPORT
	// failure — never classified as if Meta had responded.
	c := NewClient(http.DefaultClient, "http://127.0.0.1:1")
	_, err := c.RenewInstagramToken(context.Background(), "http://127.0.0.1:1", "token-atual")
	if err == nil {
		t.Fatal("quero erro de transporte contra um endereco morto")
	}
	var me *MetaError
	if errors.As(err, &me) {
		t.Fatalf("erro classificado como MetaError (%v) — falha de TRANSPORTE nao pode parecer resposta da Meta", me)
	}
}
