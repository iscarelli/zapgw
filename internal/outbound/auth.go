// Consumer authentication.
//
// Two separate things, and confusing them is the defect this file exists to
// prevent:
//
//  1. WHO you are — the Bearer token says the consumer;
//  2. WHAT you can use — the consumer->instances link says the numbers.
//
// Authenticating without checking the link would let a token leaked from
// system A send a message through system B's number. This is project
// requirement 3: speaking on behalf of N businesses WITHOUT confusing one
// with another.
package outbound

import (
	"errors"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
)

var (
	// ErrNoToken: no Authorization came, or it did not come in the Bearer scheme.
	ErrNoToken = errors.New("outbound: sem token Bearer")
	// ErrInvalidToken: a token came, but no consumer recognizes it.
	ErrInvalidToken = errors.New("outbound: token invalido")
)

const bearerScheme = "bearer "

type Authenticator struct {
	store *config.Store
}

func NewAuthenticator(store *config.Store) *Authenticator {
	return &Authenticator{store: store}
}

// TokenFromHeader extracts the token from an Authorization header, or "" if there is none.
//
// The scheme's case is ignored because HTTP clients vary ("Bearer", "bearer");
// the TOKEN itself is always compared exactly.
func TokenFromHeader(header string) string {
	if len(header) < len(bearerScheme) {
		return ""
	}
	if !strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
		return ""
	}
	return strings.TrimSpace(header[len(bearerScheme):])
}

// Authenticate says WHO the caller is. It does NOT say what they can use — for
// that CanUse exists, and forgetting to call it is the hole this package names.
func (a *Authenticator) Authenticate(header string) (config.Consumer, error) {
	token := TokenFromHeader(header)
	if token == "" {
		return config.Consumer{}, ErrNoToken
	}

	c, err := a.store.ConsumerByToken(token)
	if errors.Is(err, config.ErrConsumerNotFound) {
		return config.Consumer{}, ErrInvalidToken
	}
	if err != nil {
		return config.Consumer{}, err // database error: transient, propagates as-is
	}
	return c, nil
}

// CanUse says whether this consumer can send through instance `slug`.
//
// EXACT comparison, never prefix or "contains": the slug is the instance's
// identity, and matching by prefix would let "lojinha" authorize "lojinha-teste".
func CanUse(c config.Consumer, slug string) bool {
	if slug == "" {
		return false
	}
	for _, allowed := range c.Instances {
		if allowed == slug {
			return true
		}
	}
	return false
}
