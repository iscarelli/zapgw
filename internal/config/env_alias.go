// env_alias.go — T-214 (CAMADA 4 of this project's "accept both, count the
// old one" migration idiom — see internal/outbound/input_aliases.go for the
// same idiom applied to the API contract, CAMADA 3). Here it applies to the
// OPERATOR's surface: the ZAPGW_* variables read from /etc/zapgw/env and the
// CLI verbs.
//
// 🔴 WHY THIS LAYER IS THE DANGEROUS ONE, and CLAUDE.md already marked it
// out of T-189's scope for the reason: a rename here does NOT reach
// /etc/zapgw/env, which lives on the production machine, outside this
// repository. A rename with no alias would make the gateway boot on the
// DEFAULT, in SILENCE — no crash, no warning, and whatever depended on the
// variable simply stops happening until someone notices its absence. That is
// why every pair this task touches is ADDITIVE ONLY: the OLD (Portuguese)
// name is never removed here — removing it is a separate, owner-only
// decision (docs/TASKS.md, T-214's Do item 4).
package config

import "log"

// EnvOrOld resolves ONE operator-facing variable that has both an English
// (new) and a Portuguese (old) name. THE NEW NAME WINS when both are set —
// the same precedence rule internal/outbound's queryAlias already applies to
// the API contract, so an operator migrating /etc/zapgw/env one line at a
// time is never surprised by an old value winning over a new one they just
// added.
//
// A nil getenv (only ever passed by a test that does not care about the
// environment) resolves to "", false — never a panic.
func EnvOrOld(getenv func(string) string, newName, oldName string) (value string, oldNameUsed bool) {
	if getenv == nil {
		return "", false
	}
	if v := getenv(newName); v != "" {
		return v, false
	}
	if v := getenv(oldName); v != "" {
		return v, true
	}
	return "", false
}

// WarnOldEnvVar prints, via the standard logger, that an operator's
// environment used the OLD (Portuguese) name of a ZAPGW_* variable instead
// of the new English one — Do item 3 of T-214: "if the old name is used, SAY
// SO at startup — the operator needs to see it, and startup is the only
// place they look." A no-op when oldNameUsed is false, which is what keeps a
// fully-migrated /etc/zapgw/env printing NOTHING extra — the same silence
// Verify checks for.
//
// Goes through `log`, not a return value collected somewhere: this is the
// SAME channel main.go already uses for every other startup line ("guarda de
// lideranca ARMADA", …), and it is also what a `zapgw <verbo>` run from a
// terminal already has on stderr — a CLI invocation IS its own "arranque"
// for this purpose, just like the server's boot is.
func WarnOldEnvVar(oldNameUsed bool, oldName, newName string) {
	if !oldNameUsed {
		return
	}
	log.Printf("zapgw: variavel de ambiente %s esta obsoleta -- use %s no lugar (T-214)", oldName, newName)
}
