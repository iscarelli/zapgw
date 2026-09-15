package config

import (
	"errors"
	"strings"
	"testing"
)

// TestEnvRefusingOldThreeCases is T-244's core Verify, generic over any
// pair: (a) only the new name -> its value is read; (b) only the old name
// -> refused, the error names the new name, and the value is NOT read (no
// default kicks in either); (c) both set -> refused too — the old name
// being set is enough, there is no "new wins".
func TestEnvRefusingOldThreeCases(t *testing.T) {
	cases := []struct {
		name      string
		vars      map[string]string
		wantValue string
		wantErr   bool
	}{
		{"neither", map[string]string{}, "", false},
		{"(a) only the new one -> read", map[string]string{"NOVA": "v-nova"}, "v-nova", false},
		{"(b) only the old one -> refused", map[string]string{"VELHA": "v-velha"}, "", true},
		{"(c) both -> refused too", map[string]string{"NOVA": "v-nova", "VELHA": "v-velha"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			getenv := func(k string) string { return c.vars[k] }
			value, err := EnvRefusingOld(getenv, "NOVA", "VELHA")
			if c.wantErr {
				if err == nil {
					t.Fatalf("EnvRefusingOld = (%q, nil), want an error naming NOVA", value)
				}
				if !errors.Is(err, ErrObsoleteEnvVar) {
					t.Errorf("error does not wrap ErrObsoleteEnvVar: %v", err)
				}
				if value != "" {
					t.Errorf("value = %q on a refusal, want empty — the old value must never be read", value)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if value != c.wantValue {
				t.Errorf("value = %q, want %q", value, c.wantValue)
			}
		})
	}
}

// TestEnvRefusingOldMessageNamesTheNewName: the refusal message has to name
// the NEW name — the operator is looking at a startup failure with no code
// in front of them, and the message is the only place that says what to do.
func TestEnvRefusingOldMessageNamesTheNewName(t *testing.T) {
	getenv := func(k string) string {
		if k == "ZAPGW_VELHA" {
			return "v-velha"
		}
		return ""
	}
	_, err := EnvRefusingOld(getenv, "ZAPGW_NOVA", "ZAPGW_VELHA")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if got := err.Error(); !strings.Contains(got, "ZAPGW_NOVA") || !strings.Contains(got, "ZAPGW_VELHA") {
		t.Errorf("message does not name both variables: %q", got)
	}
}

// TestEnvRefusingOldNilGetenvNeverPanics: a nil getenv (a test that doesn't
// care about the environment) resolves to "", nil — not a panic.
func TestEnvRefusingOldNilGetenvNeverPanics(t *testing.T) {
	value, err := EnvRefusingOld(nil, "NOVA", "VELHA")
	if value != "" || err != nil {
		t.Errorf("EnvRefusingOld(nil, ...) = (%q, %v), want (\"\", nil)", value, err)
	}
}
