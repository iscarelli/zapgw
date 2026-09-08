package meta

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

const testSecret = "test-secret-not-real"

func sign(t *testing.T, payload []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestSignatureValidAcceptsACorrectSignature(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account"}`)

	if !SignatureValid(payload, sign(t, payload, testSecret), testSecret) {
		t.Fatal("the correct signature was refused")
	}
}

func TestSignatureValidRefusesAnAlteredBody(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account"}`)
	header := sign(t, payload, testSecret)

	if SignatureValid([]byte(`{"object":"outra_coisa"}`), header, testSecret) {
		t.Fatal("the altered body passed verification")
	}
}

func TestSignatureValidRefusesTheWrongSecret(t *testing.T) {
	payload := []byte(`{"a":1}`)

	if SignatureValid(payload, sign(t, payload, "another-secret"), testSecret) {
		t.Fatal("the wrong secret passed verification")
	}
}

func TestSignatureValidRefusesAnAbsentHeader(t *testing.T) {
	if SignatureValid([]byte(`{"a":1}`), "", testSecret) {
		t.Fatal("the empty header passed verification")
	}
}

func TestSignatureValidRefusesAHeaderWithoutThePrefix(t *testing.T) {
	payload := []byte(`{"a":1}`)
	withPrefix := sign(t, payload, testSecret)
	withoutPrefix := withPrefix[len("sha256="):]

	if SignatureValid(payload, withoutPrefix, testSecret) {
		t.Fatal("the header without the sha256= prefix passed verification")
	}
}

// TRAP — cost: an Important in this network's previous library.
// An X-Hub-Signature-256 header with a high byte (frameworks decode the
// header as latin-1) blew up INSIDE the security function, which promised
// "never raises" -> 500 -> 36h of Meta redelivery. In Go the mechanism moves
// (it's the hex decode that rejects it), but the outcome to avoid is the
// same: verification has to return FALSE, never panic.
func TestSignatureValidRefusesANonASCIIHeaderWithoutPanicking(t *testing.T) {
	cases := []string{
		"sha256=" + "\xc3\xa9" + "abcdef",
		"sha256=çççççç",
		"sha256=" + string(rune(0x80)),
		"\xff\xfe",
	}

	for _, header := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic with header %q: %v", header, r)
				}
			}()
			if SignatureValid([]byte(`{"a":1}`), header, testSecret) {
				t.Fatalf("non-ASCII header %q passed verification", header)
			}
		}()
	}
}

func TestSignatureValidRefusesHexOfTheWrongLength(t *testing.T) {
	// Valid hex, wrong size: hmac.Equal returns false without leaking timing.
	if SignatureValid([]byte(`{"a":1}`), "sha256=abcd", testSecret) {
		t.Fatal("short hex passed verification")
	}
}
