# /etc/profile.d/zapgw.sh — makes `zapgw` work also for whoever enters via `pct`.
# See docs/ARMADILHAS.md, "`command not found` inside the CT does not mean the
# binary isn't there". Installed on 2026-07-29.
#
# THE PROBLEM IT SOLVES, and there are two, with different symptoms:
#   1. `pct enter` / `pct exec` give PATH=/sbin:/bin:/usr/sbin:/usr/bin — without
#      /usr/local/bin, which is where deploy.sh installs the binary. Symptom:
#      `command not found`, which sends you looking for a broken deploy.
#   2. an interactive shell does not inherit systemd's env, so ZAPGW_BANCO and
#      ZAPGW_CHAVE_CIFRA are missing. Symptom: the menu opens and prints
#      `summary unavailable:`, and every subcommand fails to open the database.
#
# WHY A FUNCTION, and not just a PATH: a PATH alone would fix (1) and leave (2)
# standing — trading a clear error for an obscure one is worse than not fixing it.
#
# WHY THE BODY IS `( ... )` AND NOT `{ ... }`: the subshell keeps the env —
# including ZAPGW_CHAVE_CIFRA — alive only for the duration of the call. With
# braces, the encryption key would stay in the shell's environment and would be
# inherited by every process started from there, readable in /proc/<pid>/environ.
# A convenience is not worth paying for with the secret.
#
# `zapgw` alone, in a terminal, opens the menu — the function preserves
# stdin/stdout.
zapgw() (
	set -a
	. /etc/zapgw/env
	set +a
	exec /usr/local/bin/zapgw "$@"
)
