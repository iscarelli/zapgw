// Tests for the Instagram insights calls (T-271). Synthetic ids only.
package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstagramInsightsCallsRefuseABadIDBeforeCallingMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Meta was called")
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), "http://127.0.0.1:1")
	ctx := context.Background()
	if _, err := c.InstagramAccountInsights(ctx, srv.URL, "../x", "tok", 1, 2); !errors.Is(err, ErrInvalidPhoneNumberID) {
		t.Errorf("account: err = %v", err)
	}
	if _, err := c.InstagramMediaList(ctx, srv.URL, "../x", "tok", 10, ""); !errors.Is(err, ErrInvalidPhoneNumberID) {
		t.Errorf("list: err = %v", err)
	}
	if _, err := c.InstagramMediaInsights(ctx, srv.URL, "../x", "tok"); !errors.Is(err, ErrInvalidPhoneNumberID) {
		t.Errorf("media insights: err = %v", err)
	}
}

func TestParseInsightsDataAcceptsBothShapesAndNullsTheRest(t *testing.T) {
	got, err := parseInsightsData([]byte(`{"data":[
		{"name":"reach","total_value":{"value":7}},
		{"name":"saved","values":[{"value":3}]},
		{"name":"views","values":[]},
		{"name":"shares","total_value":{"value":"n/a"}},
		{"name":"other","total_value":{"value":1}}]}`),
		[]string{"reach", "saved", "views", "shares", "profile_visits"})
	if err != nil {
		t.Fatal(err)
	}
	if got["reach"] == nil || *got["reach"] != 7 || got["saved"] == nil || *got["saved"] != 3 {
		t.Errorf("got = %v", got)
	}
	for _, name := range []string{"views", "shares", "profile_visits"} {
		if v, present := got[name]; !present || v != nil {
			t.Errorf("%s = %v (present=%v), want nil", name, v, present)
		}
	}
	if _, extra := got["other"]; extra {
		t.Error("unasked metric kept")
	}
	if _, err := parseInsightsData([]byte(`not json`), nil); !errors.Is(err, ErrInstagramInsightsNotUnderstood) {
		t.Errorf("err = %v", err)
	}
}
