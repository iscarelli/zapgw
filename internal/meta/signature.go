package meta

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const signaturePrefix = "sha256="

// SignatureValid says whether `cru` was signed with `appSecret` by Meta.
//
// Hard contract: NEVER panics, for any header value. A panic here becomes a
// 500, and a 500 makes Meta redeliver the same payload for 36h.
//
// Receives the RAW BYTES. Passing reserialized JSON here breaks the
// verification silently — that's why the body comes in through httpx.ReadRaw
// and is not touched before this.
func SignatureValid(payload []byte, header, appSecret string) bool {
	if !strings.HasPrefix(header, signaturePrefix) {
		return false
	}

	// hex.DecodeString is what rejects a non-ASCII header: a byte outside
	// [0-9a-fA-F] becomes an error, never a panic.
	expected, err := hex.DecodeString(header[len(signaturePrefix):])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(payload)

	return hmac.Equal(expected, mac.Sum(nil))
}
