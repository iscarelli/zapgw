package meta

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRegistrationGraph builds a fake Graph API that ALWAYS returns the same
// status/body, and captures path, received body, and Authorization for each
// call — the SAME pattern as respondingGraph (number_test.go), with the body
// captured on top because registrar/pin NEED to be checked by what they
// send, not just by the URL.
func fakeRegistrationGraph(t *testing.T, status int, body string) (*Client, *[]string, *[][]byte, *[]string) {
	t.Helper()
	var paths []string
	var bodies [][]byte
	var authorizations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		paths = append(paths, r.URL.String())
		bodies = append(bodies, raw)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.Client(), srv.URL), &paths, &bodies, &authorizations
}

func TestPinValid(t *testing.T) {
	cases := map[string]bool{
		"123456":  true,
		"000000":  true,
		"":        false,
		"12345":   false, // too short
		"1234567": false, // too long
		"12345a":  false, // not numeric
		" 23456":  false, // a space isn't a digit
	}
	for pin, want := range cases {
		if got := PinValid(pin); got != want {
			t.Errorf("PinValid(%q) = %v, want %v", pin, got, want)
		}
	}
}

func TestRegisterBuildsTheRightBodyAndPath(t *testing.T) {
	c, paths, bodies, authorizations := fakeRegistrationGraph(t, http.StatusOK, `{"success":true}`)

	if err := c.Register(context.Background(), "PNID1", "token-secreto", "123456"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(*paths) != 1 {
		t.Fatalf("calls = %d, want 1", len(*paths))
	}
	if !strings.HasSuffix((*paths)[0], "/PNID1/register") {
		t.Errorf("path = %q, want it to end in /PNID1/register", (*paths)[0])
	}

	var body map[string]any
	if err := json.Unmarshal((*bodies)[0], &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, (*bodies)[0])
	}
	if body["messaging_product"] != "whatsapp" {
		t.Errorf("messaging_product = %v, want \"whatsapp\"", body["messaging_product"])
	}
	if body["pin"] != "123456" {
		t.Errorf("pin = %v, want \"123456\"", body["pin"])
	}

	if (*authorizations)[0] != "Bearer token-secreto" {
		t.Errorf("Authorization = %q", (*authorizations)[0])
	}
	// The PIN and the TOKEN can never leak into the URL: a query string
	// leaks into proxy, server, and CDN logs.
	if strings.Contains((*paths)[0], "123456") || strings.Contains((*paths)[0], "token-secreto") {
		t.Errorf("secret leaked into the URL: %q", (*paths)[0])
	}
}

func TestRegisterRefusesAPinOfTheWrongShapeWithoutTouchingTheNetwork(t *testing.T) {
	c, paths, _, _ := fakeRegistrationGraph(t, http.StatusOK, `{"success":true}`)

	err := c.Register(context.Background(), "PNID1", "token-secreto", "12")
	if !errors.Is(err, ErrInvalidPin) {
		t.Fatalf("err = %v, want ErrInvalidPin", err)
	}
	if len(*paths) != 0 {
		t.Errorf("Register with an invalid pin TOUCHED the network (%d call(s))", len(*paths))
	}
}

func TestDeregisterBuildsTheRightBodyAndPath(t *testing.T) {
	c, paths, bodies, _ := fakeRegistrationGraph(t, http.StatusOK, `{"success":true}`)

	if err := c.Deregister(context.Background(), "PNID1", "token-secreto"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if len(*paths) != 1 {
		t.Fatalf("calls = %d, want 1", len(*paths))
	}
	if !strings.HasSuffix((*paths)[0], "/PNID1/deregister") {
		t.Errorf("path = %q, want it to end in /PNID1/deregister", (*paths)[0])
	}
	var body map[string]any
	if err := json.Unmarshal((*bodies)[0], &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, (*bodies)[0])
	}
	if body["messaging_product"] != "whatsapp" {
		t.Errorf("messaging_product = %v, want \"whatsapp\"", body["messaging_product"])
	}
}

func TestSetPinPostsToTheNumbersPathWithoutASuffix(t *testing.T) {
	c, paths, bodies, _ := fakeRegistrationGraph(t, http.StatusOK, `{"success":true}`)

	if err := c.SetPin(context.Background(), "PNID1", "token-secreto", "654321"); err != nil {
		t.Fatalf("SetPin: %v", err)
	}
	if len(*paths) != 1 {
		t.Fatalf("calls = %d, want 1", len(*paths))
	}
	// No /register or /deregister suffix: it's the SAME path that
	// ObserveNumber reads, just as a POST.
	url := (*paths)[0]
	if !strings.HasSuffix(url, "/PNID1") {
		t.Errorf("path = %q, want it to end in /PNID1 (no /register or /deregister suffix)", url)
	}

	var body map[string]any
	if err := json.Unmarshal((*bodies)[0], &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, (*bodies)[0])
	}
	if body["pin"] != "654321" {
		t.Errorf("pin = %v, want \"654321\"", body["pin"])
	}
	// SetPin does NOT send messaging_product — only the pin, per the
	// source cited at the top of registration.go.
	if _, has := body["messaging_product"]; has {
		t.Errorf("body has messaging_product, and SetPin should only send {\"pin\":...}: %v", body)
	}
}

func TestSetPinRefusesAPinOfTheWrongShapeWithoutTouchingTheNetwork(t *testing.T) {
	c, paths, _, _ := fakeRegistrationGraph(t, http.StatusOK, `{"success":true}`)

	err := c.SetPin(context.Background(), "PNID1", "token-secreto", "abcdef")
	if !errors.Is(err, ErrInvalidPin) {
		t.Fatalf("err = %v, want ErrInvalidPin", err)
	}
	if len(*paths) != 0 {
		t.Errorf("SetPin with an invalid pin TOUCHED the network (%d call(s))", len(*paths))
	}
}

func TestRegistrationRefusesAnInvalidPhoneNumberIDWithoutTouchingTheNetwork(t *testing.T) {
	c, paths, _, _ := fakeRegistrationGraph(t, http.StatusOK, `{"success":true}`)

	cases := []func() error{
		func() error { return c.Register(context.Background(), "../etc", "token", "123456") },
		func() error { return c.Deregister(context.Background(), "../etc", "token") },
		func() error { return c.SetPin(context.Background(), "../etc", "token", "123456") },
	}
	for i, call := range cases {
		if err := call(); !errors.Is(err, ErrInvalidPhoneNumberID) {
			t.Errorf("case %d: err = %v, want ErrInvalidPhoneNumberID", i, err)
		}
	}
	if len(*paths) != 0 {
		t.Errorf("invalid phone_number_id TOUCHED the network (%d call(s))", len(*paths))
	}
}

func TestRegisterPassesOnMetasClassifiedError(t *testing.T) {
	// 401 is ClassConfig — wrong or revoked token. The test checks that the
	// Meta error arrives classified, not just "there was an error".
	c, _, _, _ := fakeRegistrationGraph(t, http.StatusUnauthorized,
		`{"error":{"message":"token invalido","code":190}}`)

	err := c.Register(context.Background(), "PNID1", "token-velho", "123456")
	var metaError *MetaError
	if !errors.As(err, &metaError) {
		t.Fatalf("err = %v (%T), want *MetaError", err, err)
	}
	if metaError.Class != ClassConfig {
		t.Errorf("Class = %q, want %q", metaError.Class, ClassConfig)
	}
}
