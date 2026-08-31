package config

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestEnvOrOldPrecedence is T-214's core Verify: the new name wins when
// both are set, either name works alone, and neither present resolves to
// "".
func TestEnvOrOldPrecedence(t *testing.T) {
	cases := []struct {
		name        string
		vars        map[string]string
		wantValue   string
		wantOldUsed bool
	}{
		{"nenhuma", map[string]string{}, "", false},
		{"so a nova", map[string]string{"NOVA": "v-nova"}, "v-nova", false},
		{"so a velha", map[string]string{"VELHA": "v-velha"}, "v-velha", true},
		{"as duas: a NOVA vence", map[string]string{"NOVA": "v-nova", "VELHA": "v-velha"}, "v-nova", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			getenv := func(k string) string { return c.vars[k] }
			value, oldUsed := EnvOrOld(getenv, "NOVA", "VELHA")
			if value != c.wantValue || oldUsed != c.wantOldUsed {
				t.Errorf("EnvOrOld = (%q, %v), quero (%q, %v)", value, oldUsed, c.wantValue, c.wantOldUsed)
			}
		})
	}
}

// TestEnvOrOldNilGetenvNeverPanics: a nil getenv (a test that doesn't care
// about the environment) resolves to "", false — not a panic.
func TestEnvOrOldNilGetenvNeverPanics(t *testing.T) {
	value, oldUsed := EnvOrOld(nil, "NOVA", "VELHA")
	if value != "" || oldUsed {
		t.Errorf("EnvOrOld(nil, ...) = (%q, %v), quero (\"\", false)", value, oldUsed)
	}
}

// TestWarnOldEnvVarOnlyPrintsWhenOldNameUsed is T-214 Do item 3's contract
// at the shared-helper level: silent when oldNameUsed is false, one line
// naming both the old and the new variable when it's true.
func TestWarnOldEnvVarOnlyPrintsWhenOldNameUsed(t *testing.T) {
	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })

	var buf bytes.Buffer
	log.SetOutput(&buf)
	WarnOldEnvVar(false, "ZAPGW_VELHA", "ZAPGW_NOVA")
	if buf.Len() != 0 {
		t.Fatalf("oldNameUsed=false imprimiu algo: %q", buf.String())
	}

	buf.Reset()
	WarnOldEnvVar(true, "ZAPGW_VELHA", "ZAPGW_NOVA")
	got := buf.String()
	if !strings.Contains(got, "ZAPGW_VELHA") || !strings.Contains(got, "ZAPGW_NOVA") {
		t.Errorf("aviso nao cita as duas variaveis: %q", got)
	}
	if strings.Count(strings.TrimRight(got, "\n"), "\n")+1 != 1 {
		t.Errorf("aviso tem mais de uma linha: %q", got)
	}
}
