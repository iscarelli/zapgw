// Tests for InstagramUserProfile (T-269). Synthetic ids only.
package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestInstagramUserProfileRefusesNonDigitIGSIDBeforeCallingMeta(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), "http://127.0.0.1:1")

	for _, bad := range []string{"", "..", "12/34", "12a", "../1"} {
		_, err := c.InstagramUserProfile(context.Background(), srv.URL, bad, "tok")
		if !errors.Is(err, ErrInvalidIGSID) {
			t.Errorf("igsid %q: err = %v, want ErrInvalidIGSID", bad, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("fake was hit %d times, want 0", hits.Load())
	}
}

func TestInstagramUserProfileUsesTheParametersBaseAndParsesTheBody(t *testing.T) {
	var path, fields, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, fields, auth = r.URL.Path, r.URL.Query().Get("fields"), r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"username":"Synthetic_User","name":"Synthetic Person"}`))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), "http://127.0.0.1:1") // c.base listens nowhere

	p, err := c.InstagramUserProfile(context.Background(), srv.URL, "17841400000000001", "tok")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if path != "/17841400000000001" || fields != "name,username" || auth != "Bearer tok" {
		t.Errorf("path=%q fields=%q auth=%q", path, fields, auth)
	}
	if p.Username != "synthetic_user" || p.Name == nil || *p.Name != "Synthetic Person" {
		t.Errorf("profile = %+v", p)
	}
}

func TestInstagramUserProfileRefuses200WithoutUsername(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"Synthetic Person"}`))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), "http://127.0.0.1:1")

	_, err := c.InstagramUserProfile(context.Background(), srv.URL, "17841400000000001", "tok")
	if !errors.Is(err, ErrInstagramProfileNotUnderstood) {
		t.Errorf("err = %v, want ErrInstagramProfileNotUnderstood", err)
	}
}
