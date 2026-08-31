package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// respondingGraph builds a fake Graph API that ALWAYS returns the same body
// and captures the URL and Authorization it received.
func respondingGraph(t *testing.T, status int, body string) (*Client, *[]string, *[]string) {
	t.Helper()
	var urls, authorizations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urls = append(urls, r.URL.String())
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.Client(), srv.URL), &urls, &authorizations
}

// The URL requests the RIGHT fields — and the test names both, because the
// consumer cited a DEPRECATED name (`messaging_limit_tier`) and implementing
// it would have been born dead. See the top of number.go for the Meta doc
// quote.
func TestObserveNumberAsksForTheFieldsCheckedAgainstTheSource(t *testing.T) {
	c, urls, authorizations := respondingGraph(t, http.StatusOK, `{"id":"PNID1"}`)

	if _, err := c.ObserveNumber(context.Background(), "PNID1", "token-secreto"); err != nil {
		t.Fatalf("ObserveNumber: %v", err)
	}
	if len(*urls) != 1 {
		t.Fatalf("chamadas = %d, quero 1", len(*urls))
	}
	url := (*urls)[0]
	for _, field := range []string{"quality_rating", "whatsapp_business_manager_messaging_limit"} {
		if !strings.Contains(url, field) {
			t.Errorf("a URL %q nao pede %q", url, field)
		}
	}
	// THE DEPRECATED NAME CANNOT COME BACK. Meta wrote, on the messaging
	// limits page: "The messaging_limit_tier field ... has been deprecated.
	// Request the whatsapp_business_manager_messaging_limit field instead."
	if strings.Contains(url, "messaging_limit_tier") {
		t.Errorf("a URL %q pede `messaging_limit_tier`, que a Meta DEPRECOU", url)
	}
	// The token goes in the HEADER, never in the URL: a query string leaks
	// into proxy, server, and CDN logs.
	if strings.Contains(url, "token-secreto") {
		t.Errorf("o token vazou para a URL: %q", url)
	}
	if (*authorizations)[0] != "Bearer token-secreto" {
		t.Errorf("Authorization = %q", (*authorizations)[0])
	}
}

// 🔴 MANDATORY MUTATION OF T-080: translating "TIER_250" to 250 (or "GREEN"
// to anything else) leaves this test RED, and the message cites the expected
// LITERAL. Translating requires a table of our own, and it breaks the day
// Meta invents a new tier — breaking in the worst way, returning a plausible
// number for a value no one checked.
func TestObserveNumberKeepsTheLITERALValues(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK,
		`{"id":"PNID1","quality_rating":"GREEN","whatsapp_business_manager_messaging_limit":"TIER_250"}`)

	obs, err := c.ObserveNumber(context.Background(), "PNID1", "t")
	if err != nil {
		t.Fatalf("ObserveNumber: %v", err)
	}
	if obs.Limit != "TIER_250" {
		t.Errorf("Limit = %q, quero o LITERAL %q — nunca 250, nunca traduzido", obs.Limit, "TIER_250")
	}
	if obs.Quality != "GREEN" {
		t.Errorf("Quality = %q, quero o LITERAL %q — nunca numero, nunca booleano", obs.Quality, "GREEN")
	}
	if obs.Empty() {
		t.Error("Empty() = true com os dois campos preenchidos")
	}
}

// Meta sending ONE field in an unexpected type cannot take the other one
// down with it. It already sends a number where the doc shows text (the
// `entity_id` in account_alerts), and with `string` directly in the struct
// the Unmarshal of the WHOLE ENVELOPE would fail.
func TestObserveNumberLosesONLYTheUnreadableField(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK,
		`{"quality_rating":{"nota":"GREEN"},"whatsapp_business_manager_messaging_limit":"TIER_1K"}`)

	obs, err := c.ObserveNumber(context.Background(), "PNID1", "t")
	if err != nil {
		t.Fatalf("ObserveNumber: %v", err)
	}
	if obs.Limit != "TIER_1K" {
		t.Errorf("Limit = %q, quero TIER_1K — um campo ilegivel nao pode derrubar o outro", obs.Limit)
	}
	if obs.Quality != "" {
		t.Errorf("Quality = %q, quero \"\" — valor que nao da para ler vira ausencia, nunca invencao", obs.Quality)
	}
}

// A 2xx response WITHOUT the fields is an EMPTY observation, never an
// invented value — and whoever writes it uses that to avoid erasing what it
// already knew.
func TestObserveNumberWithoutTheFieldsIsEmpty(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK, `{"id":"PNID1","display_phone_number":"+55 11 90000-0000"}`)

	obs, err := c.ObserveNumber(context.Background(), "PNID1", "t")
	if err != nil {
		t.Fatalf("ObserveNumber: %v", err)
	}
	if !obs.Empty() {
		t.Errorf("obs = %+v, quero vazia", obs)
	}
}

// The error comes back ALREADY CLASSIFIED, same as CheckCredential: that's
// what lets the watchdog decide a verdict with the usual taxonomy.
func TestObserveNumberReturnsAClassifiedError(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusUnauthorized,
		`{"error":{"message":"Invalid OAuth access token","code":190}}`)

	_, err := c.ObserveNumber(context.Background(), "PNID1", "t")
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, quero *MetaError", err)
	}
	if me.Class != ClassConfig {
		t.Errorf("classe = %q, quero %q", me.Class, ClassConfig)
	}
}

// THE SAME guard as sending and checking, through the same function: an id
// with `../` would escape the Graph API's version prefix and point to
// another endpoint.
func TestObserveNumberRefusesAnInvalidPhoneNumberID(t *testing.T) {
	c, urls, _ := respondingGraph(t, http.StatusOK, `{}`)

	_, err := c.ObserveNumber(context.Background(), "../../me", "t")
	if !errors.Is(err, ErrInvalidPhoneNumberID) {
		t.Fatalf("err = %v, quero ErrInvalidPhoneNumberID", err)
	}
	if len(*urls) != 0 {
		t.Errorf("a chamada SAIU mesmo com id invalido: %v", *urls)
	}
}
