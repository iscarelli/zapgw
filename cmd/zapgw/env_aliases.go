// env_aliases.go — T-214 (CAMADA 4 of this project's "accept both, count
// the old one" migration idiom — see internal/outbound/input_aliases.go for
// the same idiom applied to the API contract, CAMADA 3, and
// internal/config/env_alias.go for the shared resolver). Here it covers the
// ZAPGW_* variables that have NO exported constant of their own elsewhere in
// this repository (the ones config/ and internal/outbound own already carry
// their own New/Old pair next to their existing constant), plus the CLI
// VERBS themselves.
//
// 🔴 WHY THIS LAYER IS THE DANGEROUS ONE: a rename here does NOT reach
// /etc/zapgw/env, which lives on the production machine, outside this
// repository. Renaming with no alias would make the gateway boot on the
// DEFAULT, in SILENCE. Every pair below is therefore ADDITIVE ONLY — the OLD
// (Portuguese) name is never removed; that is a separate, owner-only
// decision (docs/TASKS.md, T-214's Do item 4).
package main

import (
	"fmt"
	"io"

	"github.com/iscarelli/zapgw/internal/config"
)

// The env-variable pairs this file owns (new, old):
const (
	envDatabaseNew = "ZAPGW_DATABASE"
	envDatabaseOld = "ZAPGW_BANCO"

	envEncryptionKeyNew = "ZAPGW_ENCRYPTION_KEY"
	envEncryptionKeyOld = "ZAPGW_CHAVE_CIFRA"

	envAddressNew = "ZAPGW_ADDRESS"
	envAddressOld = "ZAPGW_ENDERECO"

	envMaxBodyBytesNew = "ZAPGW_MAX_BODY_BYTES"
	envMaxBodyBytesOld = "ZAPGW_MAX_CORPO_BYTES"

	envIdempotencyTTLHoursNew = "ZAPGW_TTL_IDEMPOTENCY_HOURS"
	envIdempotencyTTLHoursOld = "ZAPGW_TTL_IDEMPOTENCIA_HORAS"

	envDiagnosticProbeFolderNew = "ZAPGW_DIAGNOSTIC_PROBE_FOLDER"
	envDiagnosticProbeFolderOld = "ZAPGW_DIAGNOSTICO_SONDAR_FOLDER"

	envDeliverySecretNew = "ZAPGW_DELIVERY_SECRET"
	envDeliverySecretOld = "ZAPGW_SEGREDO_ENTREGA"

	envSendTokenNew = "ZAPGW_SEND_TOKEN"
	envSendTokenOld = "ZAPGW_TOKEN_ENVIO"

	envPublicURLNew = "ZAPGW_PUBLIC_URL"
	envPublicURLOld = "ZAPGW_URL_PUBLICA"
)

// databasePath resolves the database file path — ZAPGW_DATABASE, falling
// back to ZAPGW_BANCO — with the "zapgw.db" default applied here. THE ONE
// PLACE both openStore and `zapgw perdidas` (lost.go) get this default from,
// so the two can never diverge on which file "no variable at all" opens —
// exactly the divergence class CounterRetentionDays's own header warns
// against.
func databasePath(env environment) (path string, oldNameUsed bool) {
	path, oldNameUsed = config.EnvOrOld(env, envDatabaseNew, envDatabaseOld)
	if path == "" {
		path = "zapgw.db"
	}
	return path, oldNameUsed
}

// warnOldVerb prints the T-214 CLI-verb warning: the OLD (Portuguese) verb
// still works exactly as before, but the operator should move to the new
// (English) spelling — Do item 3, applied to a verb instead of a variable. It
// writes to `out`, the SAME stream every other on-screen warning in this
// command already uses (menu.go's secretsWarning, provision.go's
// verify_token notice), not the log package: a `zapgw <verbo>` invocation IS
// its own "arranque" — the operator is looking at `out` right there, and
// there is no separate startup phase to defer to.
func warnOldVerb(out io.Writer, oldVerb, newVerb string) {
	fmt.Fprintf(out, "zapgw: o subcomando %q esta obsoleto -- use %q no lugar (T-214)\n", oldVerb, newVerb)
}
