// Encryption of credentials at rest.
//
// THE KEY LIVES OUTSIDE THE DATABASE (environment variable ZAPGW_CHAVE_CIFRA).
// That is what makes it safe for the SQLite file backup to exist: whoever
// grabs the file without the key opens nothing. If the key went inside the
// database, the backup would start carrying N businesses' credentials in
// the clear.
package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrInvalidKey: the encryption key doesn't work. The service must NOT come up like this.
var ErrInvalidKey = errors.New("config: chave de cifra invalida (quero 32 bytes em hex)")

type Vault struct {
	aead cipher.AEAD
	// hmacKey is USED ONLY by the TRANSIT log (T-091, transit.go). It
	// comes from the SAME encryption key (ZAPGW_CHAVE_CIFRA) but with DOMAIN
	// SEPARATION (the fixed prefix below): never the same byte sequence that
	// goes into AES, so that using this key elsewhere doesn't hand over the
	// encryption key for free.
	hmacKey []byte
}

// NewVault builds the vault from 32 bytes in hex (AES-256).
func NewVault(keyHex string) (*Vault, error) {
	raw, err := hex.DecodeString(keyHex)
	if err != nil || len(raw) != 32 {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("config: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("config: gcm: %w", err)
	}
	sum := sha256.Sum256(append([]byte("zapgw:chave-hmac-de-transito:v1:"), raw...))
	return &Vault{aead: aead, hmacKey: sum[:]}, nil
}

// Encrypt returns base64(nonce || ciphertext). Fresh nonce on every call:
// deterministic ciphertext would leak that two fields share the same value.
func (c *Vault) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("config: nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt undoes Encrypt. AES-GCM is authenticated: tampered ciphertext FAILS
// instead of returning garbage that would look like a credential.
func (c *Vault) Decrypt(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("config: base64: %w", err)
	}
	n := c.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("config: cifrado curto demais")
	}
	plaintext, err := c.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", fmt.Errorf("config: abrir: %w", err)
	}
	return string(plaintext), nil
}

// DeterministicHMAC is the opposite of Encrypt ON PURPOSE: the SAME input
// ALWAYS produces the SAME output (hex of HMAC-SHA256), because that is how
// the TRANSIT log (T-091, transit.go) can answer "did this Idempotency-Key
// pass through here?" without storing the value in the clear — whoever asks
// computes the SAME HMAC and searches by equality (Store.HMACCorrelation).
//
// 🔴 UNTIL T-094 (2026-07-30) THIS FUNCTION ALSO INDEXED THE PHONE NUMBER AND
// THE WAMID — the owner's decision reverted only that part ("you can put the
// number in, it's not a secret"): both now go in the CLEAR in the table, and
// DeterministicHMAC stayed restricted to the one field whose content is
// chosen by the CONSUMER (the Idempotency-Key, via HMACCorrelation).
//
// 🔴 WHY NOT Encrypt: Encrypt draws a fresh nonce ON EVERY CALL, ON PURPOSE
// (see its comment) — which is why the same encrypted key twice produces
// two DIFFERENT values, useless for an equality search. If someone
// "simplifies" this function to call Encrypt, the transit log search stops
// finding anything, and nothing in the type flags it — only the test
// (TestDeterministicHMACIsNotEncrypt, crypto_test.go) proves that the two
// functions have opposite properties.
func (c *Vault) DeterministicHMAC(plaintext string) string {
	mac := hmac.New(sha256.New, c.hmacKey)
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}
