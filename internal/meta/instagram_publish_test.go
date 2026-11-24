// Tests for PublishInstagramPost / InstagramPublishingLimit (T-270).
// Synthetic ids only.
package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublishInstagramPostRefusesABadIGIDBeforeCallingMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Meta was called")
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), "http://127.0.0.1:1")
	_, err := c.PublishInstagramPost(context.Background(), srv.URL, "../x", "tok",
		[]InstagramPublishImage{{URL: "https://img.example.invalid/a.jpg"}}, "", InstagramPublishTimings{})
	var pe *InstagramPublishError
	if !errors.As(err, &pe) || !errors.Is(err, ErrInvalidPhoneNumberID) {
		t.Fatalf("err = %v", err)
	}
}

func TestPublishInstagramPostContextCancelStopsThePoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"c-1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status_code":"IN_PROGRESS"}`))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), "http://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.PublishInstagramPost(ctx, srv.URL, "IGID1", "tok",
		[]InstagramPublishImage{{URL: "https://img.example.invalid/a.jpg"}}, "",
		InstagramPublishTimings{PollInterval: 10 * time.Millisecond, MaxWait: time.Minute})
	var pe *InstagramPublishError
	if !errors.As(err, &pe) || pe.Step != PublishStepStatus {
		t.Fatalf("err = %v, want step status", err)
	}
}

func TestInstagramPublishingLimitRefusesABodyWithoutTheNumbers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"quota_usage":3}]}`))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), "http://127.0.0.1:1")
	_, err := c.InstagramPublishingLimit(context.Background(), srv.URL, "IGID1", "tok")
	if !errors.Is(err, ErrInstagramPublishNotUnderstood) {
		t.Fatalf("err = %v", err)
	}
}
