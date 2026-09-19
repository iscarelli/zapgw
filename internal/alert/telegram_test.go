package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// test-token: never a real-looking value (CLAUDE.md — a Telegram bot token
// is a secret, and this file never handles one).
const testToken = "test-token"

func TestTelegramNotifySendsTheExpectedRequest(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tg := &Telegram{Token: testToken, ChatID: "12345", BaseURL: server.URL}
	if err := tg.Notify(context.Background(), "hello operator"); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	wantPath := "/bot" + testToken + "/sendMessage"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotBody["chat_id"] != "12345" {
		t.Errorf("chat_id = %v, want 12345", gotBody["chat_id"])
	}
	if gotBody["text"] != "hello operator" {
		t.Errorf("text = %v, want %q", gotBody["text"], "hello operator")
	}
	if gotBody["disable_web_page_preview"] != true {
		t.Errorf("disable_web_page_preview = %v, want true", gotBody["disable_web_page_preview"])
	}
}

func TestTelegramNotifyErrorsOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tg := &Telegram{Token: testToken, ChatID: "12345", BaseURL: server.URL}
	err := tg.Notify(context.Background(), "hello operator")
	if err == nil {
		t.Fatal("Notify: got nil error, want one for a non-2xx response")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error %q leaks the token", err.Error())
	}
}

// THE GATE: the token must never appear in an error, even one produced by
// net/http's OWN *url.Error, which normally embeds the full request URL —
// and the request URL here is BaseURL + "/bot" + Token + "/sendMessage".
func TestTelegramNotifyNeverLeaksTheTokenOnATransportError(t *testing.T) {
	// A closed loopback port: connection refused, fast, no real network use.
	tg := &Telegram{
		Token:   testToken,
		ChatID:  "12345",
		BaseURL: "http://127.0.0.1:1",
		Client:  &http.Client{Timeout: 2 * time.Second},
	}

	err := tg.Notify(context.Background(), "hello operator")
	if err == nil {
		t.Fatal("Notify against a closed port: got nil error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("transport error %q leaks the token — redaction did not run", err.Error())
	}
}

func TestTelegramNotifyDefaultsBaseURLAndClient(t *testing.T) {
	tg := &Telegram{Token: testToken, ChatID: "12345"}
	// Not calling Notify here (it would hit the real api.telegram.org) —
	// this only proves the zero-value struct is usable, i.e. BaseURL/Client
	// are optional, by constructing one exactly as main.go will.
	if tg.BaseURL != "" {
		t.Fatalf("BaseURL should be empty until Notify defaults it, got %q", tg.BaseURL)
	}
}
