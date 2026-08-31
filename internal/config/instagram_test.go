// Tests for T-097 (Instagram, first slice) on the config.Instance side:
// ValidateInstanceType and CreateInstance/FindInstance storing and
// returning the new fields.
//
// NO WHATSAPP TEST IN THIS PACKAGE WAS TOUCHED — testInstance()
// (store_test.go) still has no Type/IgID, and ValidateInstanceType
// normalizes "" to TypeWhatsApp, which is exactly what every row written
// before this task already has in the database (migration
// "instancia.tipo-e-ig_id").
package config

import (
	"errors"
	"testing"
	"time"
)

func testInstagramInstance() Instance {
	return Instance{
		Slug:           "insta-loja",
		Type:           TypeInstagram,
		IgID:           "IGID_SINTETICO_1",
		AppSecret:      "app-secret-de-teste",
		VerifyToken:    "verify-token-de-teste",
		SendToken:      "token-envio-de-teste",
		CallbackURL:    "https://consumidor.interno/webhooks/zapgw",
		DeliverySecret: "segredo-entrega-de-teste",
		TimeoutMs:      5000,
	}
}

func TestCreateInstagramInstanceKeepsAndReturnsIgID(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstagramInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	i, err := s.FindInstance("insta-loja")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Type != TypeInstagram {
		t.Errorf("Type = %q, quero %q", i.Type, TypeInstagram)
	}
	if i.IgID != "IGID_SINTETICO_1" {
		t.Errorf("IgID = %q, quero IGID_SINTETICO_1", i.IgID)
	}
	if i.WabaID != "" || i.PhoneNumberID != "" {
		t.Errorf("instancia Instagram nao deveria ter waba_id/phone_number_id: %q/%q", i.WabaID, i.PhoneNumberID)
	}
}

// NON-REGRESSION: an instance created the way it ALWAYS was (without
// touching Type or IgID at all) is still born TypeWhatsApp, and Type == ""
// never reaches the database — the migration writes 'whatsapp' as a
// literal DEFAULT, so a pre-T-097 instance and a post-T-097 one are
// INDISTINGUISHABLE by this field.
func TestCreateWhatsAppInstanceWithNoTypeNormalizesToWhatsApp(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	i, err := s.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Type != TypeWhatsApp {
		t.Errorf("Type = %q, quero %q (normalizado a partir de \"\")", i.Type, TypeWhatsApp)
	}
	if i.IgID != "" {
		t.Errorf("IgID = %q, quero vazio numa instancia WhatsApp", i.IgID)
	}
}

// --- T-098: long-lived token renewal ------------------------------

// TestCreateInstagramInstanceWritesTokenSetAt proves the half that
// TestCreateInstagramInstanceKeepsAndReturnsIgID doesn't cover: the field the
// renewal loop (internal/outbound/instagram_renewer.go) uses to compute
// the token's AGE is born filled in, never empty.
func TestCreateInstagramInstanceWritesTokenSetAt(t *testing.T) {
	s := testStore(t)
	birth := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	if err := s.CreateInstanceAt(testInstagramInstance(), birth); err != nil {
		t.Fatalf("CreateInstanceAt: %v", err)
	}
	i, err := s.FindInstance("insta-loja")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if want := birth.Format(time.RFC3339); i.TokenSetAt != want {
		t.Errorf("TokenSetAt = %q, quero %q", i.TokenSetAt, want)
	}
}

func TestRenewInstagramTokenAtWritesTheNewTokenAndRestartsTheValidity(t *testing.T) {
	s := testStore(t)
	birth := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := s.CreateInstanceAt(testInstagramInstance(), birth); err != nil {
		t.Fatalf("CreateInstanceAt: %v", err)
	}

	renewedAt := birth.Add(35 * 24 * time.Hour)
	if err := s.RenewInstagramTokenAt("insta-loja", "token-novo-da-meta", renewedAt); err != nil {
		t.Fatalf("RenewInstagramTokenAt: %v", err)
	}

	i, err := s.FindInstance("insta-loja")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.SendToken != "token-novo-da-meta" {
		t.Errorf("SendToken = %q, quero o token novo", i.SendToken)
	}
	if want := renewedAt.Format(time.RFC3339); i.TokenSetAt != want {
		t.Errorf("TokenSetAt = %q, quero %q (reiniciado pela renovacao)", i.TokenSetAt, want)
	}

	r, err := s.SummarizeInstance("insta-loja")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if want := renewedAt.Format(time.RFC3339); r.TokenRenewedAt != want {
		t.Errorf("TokenRenewedAt = %q, quero %q", r.TokenRenewedAt, want)
	}
}

// TestRenewInstagramTokenAtNeverTouchesAWhatsappInstance is the defense in
// depth of `AND tipo = ?`: even asking for the right slug, if the instance
// is NOT tipo=instagram the write has to refuse as "not found", and never
// change token_envio on a WhatsApp channel in production.
func TestRenewInstagramTokenAtNeverTouchesAWhatsappInstance(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	err := s.RenewInstagramTokenAt("lojinha", "token-que-nao-deveria-entrar", time.Now())
	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("erro = %v, quero ErrInstanceNotFound", err)
	}

	i, err := s.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.SendToken != "token-envio-de-teste" {
		t.Errorf("SendToken da instancia WHATSAPP mudou para %q — RenewInstagramTokenAt vazou a guarda de tipo", i.SendToken)
	}
}

func TestValidateInstanceType(t *testing.T) {
	cases := []struct {
		name                                     string
		typ, wabaID, phoneNumberID, number, igID string
		want                                     string // expected normalized type, "" if an error is expected
	}{
		{"vazio normaliza para whatsapp", "", "", "", "", "", TypeWhatsApp},
		{"whatsapp explicito passa", TypeWhatsApp, "W1", "P1", "5532999990000", "", TypeWhatsApp},
		{"whatsapp com ig_id e recusado", TypeWhatsApp, "", "", "", "IG1", ""},
		{"instagram valido passa", TypeInstagram, "", "", "", "IG1", TypeInstagram},
		{"instagram sem ig_id e recusado", TypeInstagram, "", "", "", "", ""},
		{"instagram com waba_id e recusado", TypeInstagram, "W1", "", "", "IG1", ""},
		{"instagram com phone_number_id e recusado", TypeInstagram, "", "P1", "", "IG1", ""},
		{"instagram com numero_exibido e recusado", TypeInstagram, "", "", "5532999990000", "IG1", ""},
		{"tipo desconhecido e recusado", "telegram", "", "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ, err := ValidateInstanceType(c.typ, c.wabaID, c.phoneNumberID, c.number, c.igID)
			if c.want == "" {
				if err == nil {
					t.Fatalf("quero erro, tipo normalizado saiu %q", typ)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if typ != c.want {
				t.Errorf("tipo normalizado = %q, quero %q", typ, c.want)
			}
		})
	}
}
