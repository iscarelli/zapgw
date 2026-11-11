package config

import (
	"strings"
	"testing"
	"time"
)

// T-262: numero_exibido has ONE accepted shape and callback_url is mandatory.
// Reject, never rewrite: a refused registration writes nothing.
func TestRegisterMetaDisplayNumberShapeAndMandatoryCallback(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*MetaRegistration)
		wantErr  bool
		wantName string
	}{
		{"landline accepted", func(c *MetaRegistration) { c.DisplayNumber = "+55 11 5555-5555" }, false, ""},
		{"mobile accepted", func(c *MetaRegistration) { c.DisplayNumber = "+55 11 99999-0000" }, false, ""},
		{"digits only refused", func(c *MetaRegistration) { c.DisplayNumber = "5511999990000" }, true, "numero_exibido"},
		{"5-digit block not starting with 9 refused", func(c *MetaRegistration) { c.DisplayNumber = "+55 11 88888-0000" }, true, "numero_exibido"},
		{"empty callback_url refused", func(c *MetaRegistration) { c.CallbackURL = "" }, true, "callback_url"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
				t.Fatalf("CreateInstance: %v", err)
			}
			reg := testRegistration()
			reg.DisplayNumber = "+55 11 99999-0000"
			c.mutate(&reg)

			_, err := s.RegisterMeta("terceiro", reg, time.Now())

			if !c.wantErr {
				if err != nil {
					t.Fatalf("RegisterMeta = %v, want accepted", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantName) {
				t.Fatalf("RegisterMeta = %v, want an error naming %q", err, c.wantName)
			}
		})
	}
}
