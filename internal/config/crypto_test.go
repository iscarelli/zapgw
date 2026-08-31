package config

import (
	"errors"
	"strings"
	"testing"
)

// 32 bytes in hex = AES-256. TEST value, not a secret anywhere.
const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestVaultRoundTrips(t *testing.T) {
	c, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}

	plaintext := "EAAG...token-de-teste-nao-e-real"
	ciphertext, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Cifrar: %v", err)
	}
	roundTrip, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if roundTrip != plaintext {
		t.Fatalf("volta = %q, quero %q", roundTrip, plaintext)
	}
}

func TestVaultNeverKeepsThePlaintextInTheCiphertext(t *testing.T) {
	c, _ := NewVault(testKey)

	ciphertext, _ := c.Encrypt("token-secreto-visivel")
	if strings.Contains(ciphertext, "token-secreto-visivel") {
		t.Fatal("o texto claro aparece no cifrado")
	}
}

func TestVaultProducesADifferentCiphertextEveryTime(t *testing.T) {
	// Nonce per operation. Deterministic ciphertext would leak that two
	// fields share the same value — useful for whoever only reads the
	// backup file.
	c, _ := NewVault(testKey)

	a, _ := c.Encrypt("mesmo-valor")
	b, _ := c.Encrypt("mesmo-valor")
	if a == b {
		t.Fatal("dois Cifrar do mesmo valor deram identico — falta nonce")
	}
}

func TestVaultRefusesTamperedCiphertext(t *testing.T) {
	// AES-GCM is authenticated: one swapped byte has to FAIL, not return garbage.
	c, _ := NewVault(testKey)

	ciphertext, _ := c.Encrypt("valor")
	tampered := []byte(ciphertext)
	tampered[len(tampered)-1] ^= 'x'

	if _, err := c.Decrypt(string(tampered)); err == nil {
		t.Fatal("cifrado adulterado foi aceito")
	}
}

// TestDeterministicHMACIsNotEncrypt is the Verify (e) for T-091: the transit
// log can only SEARCH because DeterministicHMAC ALWAYS gives the same
// output for the same input — the exact opposite of Encrypt, which draws a
// nonce on every call on purpose (TestVaultProducesADifferentCiphertextEveryTime,
// above). If someone one day "simplifies" DeterministicHMAC to call
// Encrypt, this test is the only one that catches it: nothing in the TYPE
// prevents the swap, only behavior does.
func TestDeterministicHMACIsNotEncrypt(t *testing.T) {
	c, _ := NewVault(testKey)

	a := c.DeterministicHMAC("5511999990000")
	b := c.DeterministicHMAC("5511999990000")
	if a != b {
		t.Fatalf("DeterministicHMAC(x) duas vezes deu %q e %q — nao e deterministico, e a busca do log de transito quebra", a, b)
	}
	if a == "" {
		t.Fatal("DeterministicHMAC devolveu vazio para uma entrada nao vazia")
	}

	// The PROOF that Encrypt CANNOT replace DeterministicHMAC here: two
	// calls of Encrypt on the SAME value give DIFFERENT outputs (fresh
	// nonce every time), so using Encrypt instead of the HMAC would make
	// every "did this number pass through?" search always fail, even
	// with the right number.
	x, _ := c.Encrypt("5511999990000")
	y, _ := c.Encrypt("5511999990000")
	if x == y {
		t.Fatal("Cifrar deixou de sortear nonce — a premissa que justifica NAO usa-lo para o log de transito caiu")
	}
}

func TestNewVaultRefusesAnInvalidKey(t *testing.T) {
	// The key lives OUTSIDE the database, in an environment variable.
	// Refusing early and loud is what keeps the service from coming up
	// "working" with no encryption at all.
	cases := []struct{ name, key string }{
		{"vazia", ""},
		{"curta demais", "00010203"},
		{"nao e hex", "zzzz02030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"},
		{"31 bytes", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e"},
	}

	for _, c := range cases {
		if _, err := NewVault(c.key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("chave %s: erro = %v, quero ErrInvalidKey", c.name, err)
		}
	}
}
