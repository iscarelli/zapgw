package outbound

import (
	"net/http"
	"strings"
	"testing"
)

// T-262: the cadastro contract is explicit about numero_exibido's shape and
// about callback_url being mandatory.
func TestRegistrationDisplayNumberShapeAndMandatoryCallback(t *testing.T) {
	cases := map[string]struct {
		changes map[string]string
		field   string // empty = accepted
	}{
		"landline accepted":           {map[string]string{"numero_exibido": "+55 11 5555-5555"}, ""},
		"mobile accepted":             {map[string]string{"numero_exibido": "+55 11 99999-0000"}, ""},
		"digits only refused":         {map[string]string{"numero_exibido": "5511999990000"}, "numero_exibido"},
		"5-digit block not 9 refused": {map[string]string{"numero_exibido": "+55 11 88888-0000"}, "numero_exibido"},
		"empty callback_url refused":  {map[string]string{"numero_exibido": "+55 11 99999-0000", "callback_url": ""}, "callback_url"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, _, _, _ := testRegistration(t)

			rec := register(t, h, "token-do-a", registrationBody("terceiro", c.changes))

			if c.field == "" {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
				}
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400. body: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.field) {
				t.Errorf("the error does not name %q: %s", c.field, rec.Body.String())
			}
		})
	}
}
