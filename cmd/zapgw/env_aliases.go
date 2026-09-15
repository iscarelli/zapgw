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
// DEFAULT, in SILENCE. Every ZAPGW_* pair below is therefore ADDITIVE
// ONLY — the OLD (Portuguese) name is never removed here (T-244 later made
// it a hard refusal instead, in internal/config/env_alias.go). The CLI
// verbs are a separate lifecycle: five of them (provisionar, fumaca,
// diagnostico, instancia, consumidor) had their Portuguese spelling
// REMOVED by T-220, once every in-repo caller was migrated — see
// oldVerbRefused below. T-245 removed the next batch the same way:
// "estado" and the eight sub-verbs (listar, mostrar, rotacionar,
// reabrir-cadastro, pausar, remover, registrar, desregistrar). T-246
// closed the last three top-level holdouts (transito, perdidas, versao),
// which had never had an English spelling at all until that task.
package main

import (
	"fmt"

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

// databasePath resolves the database file path — ZAPGW_DATABASE, with the
// "zapgw.db" default applied here. THE ONE PLACE both openStore and `zapgw
// lost` (lost.go) get this default from, so the two can never diverge
// on which file "no variable at all" opens — exactly the divergence class
// CounterRetentionDays's own header warns against.
//
// T-244: the OLD name (ZAPGW_BANCO) is no longer read — if it is set, this
// returns an error wrapping config.ErrObsoleteEnvVar instead of a path, and
// the caller has to bring the startup down.
func databasePath(env environment) (path string, err error) {
	path, err = config.EnvRefusingOld(env, envDatabaseNew, envDatabaseOld)
	if err != nil {
		return "", err
	}
	if path == "" {
		path = "zapgw.db"
	}
	return path, nil
}

// oldVerbRefused is T-220's helper for a subcommand whose Portuguese
// spelling no longer dispatches to anything -- it REFUSES, naming the
// English verb to use instead, the same "refuse, don't silently degrade"
// shape config.EnvRefusingOld already uses for ZAPGW_* (T-244).
// "Silently ignored" is the failure mode this guards against: an
// unknown-subcommand error that did not name the new verb would leave
// whoever typed the old one guessing.
//
// T-220 used it for the five top-level verbs (provisionar, fumaca,
// diagnostico, instancia, consumidor). T-245 moved the next holdouts onto
// this same helper too -- "estado" and the eight sub-verbs (listar,
// mostrar, rotacionar, reabrir-cadastro, pausar, remover, registrar,
// desregistrar) -- which is why the older warn-and-still-dispatch helper
// that used to sit above this one is gone. T-246 closed the last three
// (transito, perdidas, versao).
func oldVerbRefused(oldVerb, newVerb string) error {
	return fmt.Errorf("zapgw: subcommand %q no longer exists -- use %q instead", oldVerb, newVerb)
}
