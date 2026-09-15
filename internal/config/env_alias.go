// env_alias.go — T-244 retires the "accept both, count the old one"
// migration idiom T-214 introduced for this project's OPERATOR-facing
// surface: the ZAPGW_* variables read from /etc/zapgw/env. (The CLI verbs
// are a separate, still-open decision — T-220.)
//
// 🔴 WHY AN OLD NAME IS REFUSED, NEVER SILENTLY IGNORED: a rename with no
// safeguard would make the gateway boot on the DEFAULT, in SILENCE — no
// crash, no warning, and whatever depended on the variable simply stops
// happening until someone notices its absence. The most dangerous instance
// of this is the encryption key: an operator (or an old /etc/zapgw/env on
// another clone) still exporting ZAPGW_CHAVE_CIFRA would make the gateway
// open an EMPTY database under a DIFFERENT key, with no error at all. That
// is why the OLD name is not simply dropped from the read: if it is set,
// the process REFUSES to start, naming the new name to use instead.
package config

import (
	"errors"
	"fmt"
)

// ErrObsoleteEnvVar is the sentinel every EnvRefusingOld refusal wraps.
// Callers that want to distinguish "operator config error" from other
// startup failures can `errors.Is(err, ErrObsoleteEnvVar)`.
var ErrObsoleteEnvVar = errors.New("obsolete environment variable name")

// EnvRefusingOld resolves ONE operator-facing variable that used to accept
// both an English (new) and a Portuguese (old) name (T-214). It reads ONLY
// newName. If the OLD name is set (non-empty), it returns "" and an error
// wrapping ErrObsoleteEnvVar — the old value is NEVER read, not even to
// fall back to it.
//
// A nil getenv (only ever passed by a test that does not care about the
// environment) resolves to "", nil — never a panic.
func EnvRefusingOld(getenv func(string) string, newName, oldName string) (value string, err error) {
	if getenv == nil {
		return "", nil
	}
	if v := getenv(oldName); v != "" {
		return "", fmt.Errorf("environment variable %s is no longer read -- rename it to %s (T-244): %w",
			oldName, newName, ErrObsoleteEnvVar)
	}
	return getenv(newName), nil
}
