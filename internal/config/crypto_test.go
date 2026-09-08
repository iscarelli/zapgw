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

	plaintext := "EAAG...test-token-not-real"
	ciphertext, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	roundTrip, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if roundTrip != plaintext {
		t.Fatalf("round trip = %q, want %q", roundTrip, plaintext)
	}
}

func TestVaultNeverKeepsThePlaintextInTheCiphertext(t *testing.T) {
	c, _ := NewVault(testKey)

	ciphertext, _ := c.Encrypt("visible-secret-token")
	if strings.Contains(ciphertext, "visible-secret-token") {
		t.Fatal("the plaintext shows up in the ciphertext")
	}
}

func TestVaultProducesADifferentCiphertextEveryTime(t *testing.T) {
	// Nonce per operation. Deterministic ciphertext would leak that two
	// fields share the same value — useful for whoever only reads the
	// backup file.
	c, _ := NewVault(testKey)

	a, _ := c.Encrypt("same-value")
	b, _ := c.Encrypt("same-value")
	if a == b {
		t.Fatal("two Encrypt calls on the same value came out identical — missing nonce")
	}
}

func TestVaultRefusesTamperedCiphertext(t *testing.T) {
	// AES-GCM is authenticated: one swapped byte has to FAIL, not return garbage.
	c, _ := NewVault(testKey)

	ciphertext, _ := c.Encrypt("value")
	tampered := []byte(ciphertext)
	tampered[len(tampered)-1] ^= 'x'

	if _, err := c.Decrypt(string(tampered)); err == nil {
		t.Fatal("tampered ciphertext was accepted")
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
		t.Fatalf("DeterministicHMAC(x) twice gave %q and %q — it isn't deterministic, and the transit log search breaks", a, b)
	}
	if a == "" {
		t.Fatal("DeterministicHMAC returned empty for a non-empty input")
	}

	// The PROOF that Encrypt CANNOT replace DeterministicHMAC here: two
	// calls of Encrypt on the SAME value give DIFFERENT outputs (fresh
	// nonce every time), so using Encrypt instead of the HMAC would make
	// every "did this number pass through?" search always fail, even
	// with the right number.
	x, _ := c.Encrypt("5511999990000")
	y, _ := c.Encrypt("5511999990000")
	if x == y {
		t.Fatal("Encrypt stopped drawing a nonce — the premise that justifies NOT using it for the transit log just fell apart")
	}
}

func TestNewVaultRefusesAnInvalidKey(t *testing.T) {
	// The key lives OUTSIDE the database, in an environment variable.
	// Refusing early and loud is what keeps the service from coming up
	// "working" with no encryption at all.
	cases := []struct{ name, key string }{
		{"empty", ""},
		{"too short", "00010203"},
		{"not hex", "zzzz02030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"},
		{"31 bytes", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e"},
	}

	for _, c := range cases {
		if _, err := NewVault(c.key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("key %s: err = %v, want ErrInvalidKey", c.name, err)
		}
	}
}
