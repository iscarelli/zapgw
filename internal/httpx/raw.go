// Package httpx: reading the raw body of a request, with a ceiling.
//
// It exists as its own package because Meta's HMAC is computed over THE BYTES
// RECEIVED. Any path that deserializes and reserializes the body before
// verification breaks the signature silently. This is the service's only entry
// door for bytes.
package httpx

import (
	"errors"
	"io"
)

// ErrBodyTooLarge: the body went past the configured ceiling.
var ErrBodyTooLarge = errors.New("httpx: corpo acima do teto")

// ReadRaw returns the exact bytes of the body, or ErrBodyTooLarge if it goes past
// maxBytes.
//
// It reads maxBytes+1 on purpose: that is how "fits exactly at the ceiling" is
// told apart from "overflowed", without allocating an attacker's whole body.
func ReadRaw(body io.Reader, maxBytes int) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(body, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBytes {
		return nil, ErrBodyTooLarge
	}
	return b, nil
}
