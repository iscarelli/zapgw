package main

import (
	"bytes"
	"strings"
	"testing"
)

// T-262: the CLI shares the registration's numero_exibido rule (when given).
func TestProvisionInstanceRefusesDigitsOnlyDisplayNumber(t *testing.T) {
	vars := testEnvironment(t)
	var out bytes.Buffer

	err := dispatch(fullInstanceArgs("lojinha", "P1", "W1", "551155555555", "https://consumidor.interno/hook"),
		&out, fakeEnvironment(vars))

	if err == nil || !strings.Contains(err.Error(), "numero_exibido") {
		t.Fatalf("error = %v, want one naming numero_exibido", err)
	}
	if n := countInstancesFromEnvironment(t, vars); n != 0 {
		t.Errorf("%d instance(s) written despite the refusal", n)
	}
}
