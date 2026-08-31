package httpx

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReadRawReturnsTheExactBytes(t *testing.T) {
	// Byte for byte: Meta's HMAC is over what arrived, not over reserialized JSON.
	original := []byte("{\"a\": 1,   \"b\":\t2}\n")

	got, err := ReadRaw(bytes.NewReader(original), 1024)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("bytes diferentes:\n got = %q\nquero = %q", got, original)
	}
}

func TestReadRawAcceptsExactlyTheCeiling(t *testing.T) {
	body := []byte(strings.Repeat("x", 10))

	got, err := ReadRaw(bytes.NewReader(body), 10)
	if err != nil {
		t.Fatalf("corpo do tamanho exato do teto deve passar, veio: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("len = %d, quero 10", len(got))
	}
}

func TestReadRawRejectsOneByteOverTheCeiling(t *testing.T) {
	body := []byte(strings.Repeat("x", 11))

	_, err := ReadRaw(bytes.NewReader(body), 10)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("erro = %v, quero ErrBodyTooLarge", err)
	}
}

func TestReadRawAcceptsAnEmptyBody(t *testing.T) {
	// An empty body is not an error HERE. It fails at the signature, which is the
	// right place: what decides whether the body is any good is the HMAC, not the
	// byte reader.
	got, err := ReadRaw(bytes.NewReader(nil), 10)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, quero 0", len(got))
	}
}
