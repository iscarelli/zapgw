#!/usr/bin/env bash
#
# deploy.sh — builds, ships, swaps and VERIFIES zapgw on the target container.
#
# The reason this script exists is not "copy a binary": it is to ABORT when
# the new binary does not respond. Without the /v1/health step, a broken
# binary goes up and the gateway is off the air without anyone knowing — Meta
# simply stops delivering, and from the outside it's indistinguishable from
# "no message arrived".
#
# It also ABORTS when the binary responds, but is NOT WHAT THIS DEPLOY
# BUILT (T-184). The version comes in via `-ldflags "-X main.version=..."`,
# and the Go linker SILENTLY ignores a symbol that doesn't exist: rename the
# variable in the code and the build keeps exiting 0, except the binary
# responds "development". Measured with a positive control on 2026-08-30.
# Without the check below, that deploy ends up GREEN while publishing a
# gateway that doesn't know its own version — and from there on every
# production diagnosis leans on a wrong number.
#
# It does NOT copy /etc/zapgw/env and does NOT touch ZAPGW_CHAVE_CIFRA. The
# key lives only on the CT; copying it would be creating a second copy to leak.
#
# It ALSO installs /etc/profile.d/zapgw.sh (T-090), which is what makes the
# `zapgw` command work for whoever enters via `pct enter`/`pct exec` —
# without it, a new (or rebuilt) CT is born with `command not found`. This
# step is NOT part of the rollback path and failing it does not abort the
# deploy — but it never fails silently either (an ALARM on stdout, never
# swallowed).
#
# Usage:
#   ZAPGW_DEPLOY_HOST=user@node ZAPGW_DEPLOY_VMID=100 \
#   ZAPGW_DEPLOY_SAUDE=http://<gateway-internal-ip>:8080/v1/health \
#   deploy/deploy.sh
#
# REQUIRED — no default, and the absence of a default IS the protection:
#   ZAPGW_DEPLOY_VMID      numeric id of the container on Proxmox
#   ZAPGW_DEPLOY_HOST      SSH destination of the node that hosts the container
#   ZAPGW_DEPLOY_SAUDE     URL of /v1/health, reachable FROM the node
#
# Optional:
#   ZAPGW_DEPLOY_CHAVE     (empty) — SSH key. Empty lets ssh resolve it the
#                          same way it would for any other command (agent,
#                          ~/.ssh/config, default key).
#   ZAPGW_DEPLOY_ESPERA_S  30
#   ZAPGW_DEPLOY_BINARIO   (empty) — path to an ALREADY-built binary, to skip
#                          the build. Exists to prove the rollback with a
#                          deliberately broken binary without dirtying the
#                          repo. With it there is NO build version to compare
#                          against, so the version check is SKIPPED (said out
#                          loud, never silently) — precisely so as not to
#                          invalidate this tool, whose binary diverges on
#                          purpose.
#
# Exit: 0 deploy ok; 1 deploy failed and WAS ROLLED BACK — either /v1/health
# did not respond, or it responded with a version different from what was
# built; 2 deploy failed and the rollback did NOT restore health — needs a
# human now; 3 required configuration missing — NOTHING was done and no
# network was touched.

set -euo pipefail

passo() { printf '\n== %s\n' "$*"; }
erro() { printf 'ERROR: %s\n' "$*" >&2; }

# --------------------------------------------------------- required config
#
# Until 2026-08-30 these variables had a default pointing at the node, the
# container and the IP of ONE house. In a public repository a default like
# that isn't just a leaked address: it's a script that, run by someone who
# didn't read it, tries to deploy to a host that isn't theirs. So the absence
# STOPS the script here — before the build and before any ssh —, naming the
# variable and the expected FORMAT, because "missing variable" alone doesn't
# say what to write.
faltando=0
exigir() { # $1 name  $2 expected format  $3 example
	if [ -z "${!1:-}" ]; then
		erro "missing environment variable $1 — $2"
		erro "       example: $1=$3"
		faltando=1
	fi
}

exigir ZAPGW_DEPLOY_VMID \
	"NUMERIC id of the container on Proxmox (the same one \`pct\` uses)" \
	"100"
exigir ZAPGW_DEPLOY_HOST \
	"SSH destination of the node that hosts the container, in user@host format" \
	"deploy@proxmox-node.example.internal"
exigir ZAPGW_DEPLOY_SAUDE \
	"URL of the gateway's /v1/health, reachable FROM the node (not from your terminal)" \
	"http://<gateway-internal-ip>:8080/v1/health"

if [ "$faltando" -ne 0 ]; then
	erro "nothing was done: the deploy stops before touching the network."
	exit 3
fi

case ${ZAPGW_DEPLOY_VMID} in
*[!0-9]*)
	erro "ZAPGW_DEPLOY_VMID=${ZAPGW_DEPLOY_VMID} is not a numeric container id"
	erro "nothing was done: the deploy stops before touching the network."
	exit 3
	;;
esac

VMID=$ZAPGW_DEPLOY_VMID
HOST=$ZAPGW_DEPLOY_HOST
URL_SAUDE=$ZAPGW_DEPLOY_SAUDE
CHAVE=${ZAPGW_DEPLOY_CHAVE:-}
ESPERA=${ZAPGW_DEPLOY_ESPERA_S:-30}
BIN_PRONTO=${ZAPGW_DEPLOY_BINARIO:-}

# Options common to ssh and scp. The `-i` only enters if a key was declared:
# the earlier default named ONE machine's key, and served no one else.
# Without it, ssh resolves the key the same way it would for any other command.
SSH_OPCOES=(-o BatchMode=yes -o ConnectTimeout=10)
if [ -n "$CHAVE" ]; then
	SSH_OPCOES+=(-i "$CHAVE")
fi

DESTINO=/usr/local/bin/zapgw
ANTERIOR=/usr/local/bin/zapgw.anterior
NOVO=/usr/local/bin/zapgw.novo
UNIT=/etc/systemd/system/zapgw.service
UNIT_ANTERIOR=/etc/systemd/system/zapgw.service.anterior
SNAP=pre-update

RAIZ=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# What the rollback has to undo. Declared here, not only where they get
# filled, because reverter() reads both and with "set -u" a failure before
# the swap would take down the rollback path itself.
UNIT_SALVA=
BIN_SALVO=

remoto() { ssh "${SSH_OPCOES[@]}" "$HOST" "$1"; }

# ct runs ONE command inside the CT. The command cannot contain single quotes:
# it travels inside a pair of them all the way to pct exec.
ct() { remoto "sudo /usr/sbin/pct exec $VMID -- /bin/sh -c '$1'"; }

# esperar_saude asks for /v1/health FROM INSIDE the network (from the Proxmox
# node, not the CT): responding on loopback doesn't prove Traefik can reach it.
# A single ssh does the whole loop — 30 connections would be slower than the
# very limit we're measuring.
esperar_saude() {
	ssh "${SSH_OPCOES[@]}" "$HOST" \
		bash -s "$URL_SAUDE" "$ESPERA" <<-'FIM'
		url=$1
		limite=$2
		i=0
		while [ "$i" -lt "$limite" ]; do
			corpo=$(curl -fsS -m 2 "$url" 2>/dev/null || true)
			# NOT a zapgw:log-coupling marker on purpose: this pattern matches
			# the RENDERED JSON key+value ("ok":true), not a literal that sits
			# in the Go source — the coupling is really to the `OK bool
			# json:"ok"` field staying true on success (cmd/zapgw/main.go),
			# which the marker-based gate (internal/config/shell_log_coupling_test.go)
			# has no mechanical way to check without producing false negatives
			# on the word "ok". Reviewed by hand for T-235: still matches today.
			case "$corpo" in
			*'"ok":true'*)
				echo "$corpo"
				exit 0
				;;
			esac
			i=$((i + 1))
			sleep 1
		done
		exit 1
	FIM
}

# extrair_versao_do_corpo prints the value of the "versao" field of the JSON
# /v1/health returned, and returns 1 (printing nothing) when the field is NOT
# there. That return is the boundary between "the version is wrong" and
# "I couldn't read the version" — the other two functions here rely on it so
# as not to confuse one with the other.
extrair_versao_do_corpo() {
	local linha valor
	# zapgw:log-coupling "versao"
	linha=$(printf '%s' "$1" | grep -o '"versao":"[^"]*"' || true)
	[ -n "$linha" ] || return 1
	valor=${linha#'"versao":"'}
	valor=${valor%'"'}
	printf '%s\n' "$valor"
}

# imprimir_versao_do_corpo prints a VERY visible line with the version the
# gateway responded with — the proof that the RIGHT binary went up cannot stay
# hidden inside the raw JSON (T-025: on 2026-07-25 a binary with a new
# contract went up while the latest tag was another one, and only someone
# remembering that avoided an investigation against the wrong thing).
#
# It only PRINTS, never returns an error, and is used where there's NOTHING
# to compare against: in the probe AFTER the rollback, where the version that
# responds is the PREVIOUS binary's and therefore diverges from
# VERSAO_DO_BUILD by construction. Comparing there would flag a failure
# exactly on the path that just saved the gateway.
imprimir_versao_do_corpo() {
	local valor
	if valor=$(extrair_versao_do_corpo "$1"); then
		echo "VERSION: $valor"
	else
		echo "VERSION: unknown — this binary predates T-025 and /v1/health has no \"versao\" field (this does not abort the deploy)"
	fi
}

# conferir_versao compares what the gateway RESPONDED with what this deploy
# BUILT, and returns THREE different outcomes — the distinction is the whole
# point of the function, not a detail of it (T-184):
#
#   0  matched — the deploy proceeds.
#   1  DIVERGED — the published binary is not the one that was built. Aborts
#      and rolls back. This really happens when the build's
#      `-X main.version=...` gets the symbol wrong: the Go linker silently
#      ignores it and the binary comes up responding "desenvolvimento", with a
#      green build and a green push.
#   2  COULD NOT CHECK — and this is NOT a failure. Two legitimate causes:
#      the binary predates T-025 and has no "versao" field; or the deploy used
#      ZAPGW_DEPLOY_BINARIO and there's no build version to compare against.
#
# Treating (2) as (1) would turn this script into a monitor that screams
# without knowing, and treating (1) as (2) is the defect T-184 came to fix.
# That's why the three come out separate here, and the caller decides — none
# of them stays silent: all of them print.
conferir_versao() {
	local corpo=$1 esperada=$2 respondida
	if [ -z "$esperada" ]; then
		echo "VERSION: not checked — the deploy used ZAPGW_DEPLOY_BINARIO (build skipped), so there is no build version to compare against (this does not abort the deploy)"
		return 2
	fi
	if ! respondida=$(extrair_versao_do_corpo "$corpo"); then
		echo "VERSION: not checked — this binary predates T-025 and /v1/health has no \"versao\" field (this does not abort the deploy)"
		return 2
	fi
	if [ "$respondida" = "$esperada" ]; then
		echo "VERSION MATCHES: $respondida (same as built)"
		return 0
	fi
	erro "VERSION DIVERGES: built=$esperada responded=$respondida"
	return 1
}

# avisos_nome_obsoleto reads the startup journal and shows, only on the
# SUCCESS path, the lines that internal/config/env_alias.go:WarnOldEnvVar
# emits when a ZAPGW_* variable with the old (PT) name was used instead of
# the new (EN) one — T-216. It's the only way the operator finds out they
# need to migrate /etc/zapgw/env without entering the CT by hand: the FAILURE
# path already dumps the whole journal on purpose and doesn't change here.
#
# Filters by the same substring the log emits ("is deprecated -- use"), never
# the whole journal: a dump becomes noise, and noise trains people to ignore
# the deploy's output — which is where the version proof lives (T-184).
#
# Three outcomes, and they have to be DISTINGUISHABLE (the same requirement
# T-184 set for the version check): there was a warning -> shows the lines;
# there wasn't -> says there wasn't; couldn't read the journal -> says so,
# never "there wasn't". Silence must never turn into "it was clean".
avisos_nome_obsoleto() {
	local jornal avisos
	if ! jornal=$(ct "journalctl -u zapgw -n 200 --no-pager" 2>&1); then
		erro "COULD NOT READ the journal to check for deprecated variable names"
		return
	fi
	# zapgw:log-coupling "is deprecated -- use"
	avisos=$(printf '%s\n' "$jornal" | grep -F 'is deprecated -- use' || true)
	if [ -n "$avisos" ]; then
		echo "WARNING: environment variable(s) with a deprecated name in use at startup:"
		printf '%s\n' "$avisos" | sed 's/^/  /'
	else
		echo "no variable with a deprecated name in use"
	fi
}

# reverter undoes the swap and returns the service to the previous binary.
#
# reset-failed is not decoration: with Restart=always, a binary that dies
# instantly blows through StartLimitBurst in seconds and systemd starts
# REFUSING to start it ("start request repeated too quickly"). Without
# clearing the state, the rollback's restart fails silently and the gateway
# is off the air exactly on the path that exists to prevent it.
reverter() {
	passo "ROLLING BACK"
	if [ -n "$UNIT_SALVA" ]; then
		ct "mv -f $UNIT_ANTERIOR $UNIT"
		ct "systemctl daemon-reload"
		echo "previous unit restored"
	fi
	if [ -n "$BIN_SALVO" ]; then
		ct "mv -f $ANTERIOR $DESTINO"
		echo "previous binary restored: $(ct "sha256sum $DESTINO")"
	else
		erro "ALARM: there was no previous binary to restore (first install)."
		erro "ALARM: stopping the service to avoid a restart loop. Human action needed."
		ct "systemctl stop zapgw" || true
		return
	fi
	ct "systemctl reset-failed zapgw" || true
	ct "systemctl restart zapgw" || erro "the rollback's restart failed"
}

# ---------------------------------------------------------------- checks

passo "checking access to $HOST and to CT $VMID"
remoto "sudo /usr/sbin/pct status $VMID" | grep -q "status: running" ||
	{ erro "CT $VMID is not running"; exit 1; }
echo "CT $VMID running"

# ---------------------------------------------------------------- binary

# Empty means "this deploy did not build anything, so there's nothing to
# compare against the version the gateway responds with". Declared here, not
# only where it's filled, because with "set -u" the ready-binary path would
# take down the check further below.
VERSAO_DO_BUILD=

if [ -n "$BIN_PRONTO" ]; then
	passo "USING A READY BINARY (build skipped): $BIN_PRONTO"
	[ -f "$BIN_PRONTO" ] || { erro "binary does not exist: $BIN_PRONTO"; exit 1; }
	cp "$BIN_PRONTO" "$TMP/zapgw"
else
	# The version comes from the VERSION file, and ONLY from there — never
	# typed by hand into the deploy. It enters via -ldflags, never read from
	# disk by the binary at runtime (T-025): VERSION does not go to the CT,
	# and a binary that read the version from a file would lie exactly when
	# it matters (an old file next to a new binary — the incident that
	# opened T-025).
	VERSAO_DO_BUILD=$(cat "$RAIZ/VERSION")
	passo "building (CGO_ENABLED=0 GOOS=linux GOARCH=amd64), version $VERSAO_DO_BUILD"
	(cd "$RAIZ" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags "-X main.version=$VERSAO_DO_BUILD" \
		-o "$TMP/zapgw" ./cmd/zapgw)
fi
echo "local: $(sha256sum "$TMP/zapgw")"

# ---------------------------------------------------------------- upload

passo "sending to the node and pushing into the CT as $NOVO"
scp "${SSH_OPCOES[@]}" -q "$TMP/zapgw" "$HOST:/tmp/zapgw.envio.$$"
remoto "sudo /usr/sbin/pct push $VMID /tmp/zapgw.envio.$$ $NOVO --perms 0755 --user 0 --group 0"
remoto "rm -f /tmp/zapgw.envio.$$"
echo "in the CT: $(ct "sha256sum $NOVO")"

# ---------------------------------------------------------------- profile

# T-090: /etc/profile.d/zapgw.sh is what makes `zapgw` work also for whoever
# enters via `pct enter`/`pct exec` (docs/ARMADILHAS.md, "`command not found`
# inside the CT does not mean the binary isn't there").
# This is NOT part of the rollback path: it isn't the binary, and failing
# here doesn't justify rolling back a healthy deploy. But it also can't fail
# SILENTLY because of the "set -e" at the top of the script — so each step is
# guarded with "||"/"if" and prints an ALARM before continuing, instead of
# aborting the whole deploy or swallowing the error.
passo "installing /etc/profile.d/zapgw.sh"
if scp "${SSH_OPCOES[@]}" -q "$RAIZ/deploy/profile-zapgw.sh" "$HOST:/tmp/zapgw.profile.$$" &&
	remoto "sudo /usr/sbin/pct push $VMID /tmp/zapgw.profile.$$ /etc/profile.d/zapgw.sh --perms 0644 --user 0 --group 0"; then
	echo "profile installed: $(ct "sha256sum /etc/profile.d/zapgw.sh")"
else
	erro "ALARM: failed to install /etc/profile.d/zapgw.sh — whoever enters via pct may end up without the zapgw command. Deploy continues."
fi
remoto "rm -f /tmp/zapgw.profile.$$" || true

# pct enter/exec does not read profile.d (it's an interactive shell, not a
# login one) — that's why /root/.bashrc needs to source it. Idempotent: only
# appends if the line isn't already there.
ct "grep -qF /etc/profile.d/zapgw.sh /root/.bashrc 2>/dev/null || echo . /etc/profile.d/zapgw.sh >> /root/.bashrc" ||
	erro "ALARM: failed to ensure /etc/profile.d/zapgw.sh is sourced in /root/.bashrc. Deploy continues."

# ---------------------------------------------------------------- snapshot

passo "snapshot $SNAP of CT $VMID"
# A snapshot with the same name already exists after the first deploy; pct
# refuses without deleting it first. Failing here is on purpose: without a
# point of return, no swap happens.
remoto "sudo /usr/sbin/pct delsnapshot $VMID $SNAP" >/dev/null 2>&1 || true
remoto "sudo /usr/sbin/pct snapshot $VMID $SNAP"

# ---------------------------------------------------------------- unit

passo "installing the systemd unit"
UNIT_SALVA=
if ct "test -f $UNIT"; then
	ct "cp -a $UNIT $UNIT_ANTERIOR"
	UNIT_SALVA=sim
fi
scp "${SSH_OPCOES[@]}" -q "$RAIZ/deploy/zapgw.service" "$HOST:/tmp/zapgw.service.$$"
remoto "sudo /usr/sbin/pct push $VMID /tmp/zapgw.service.$$ $UNIT --perms 0644 --user 0 --group 0"
remoto "rm -f /tmp/zapgw.service.$$"
ct "systemctl daemon-reload"

# ---------------------------------------------------------------- swap

passo "atomic binary swap"
BIN_SALVO=
if ct "test -f $DESTINO"; then
	ct "mv -f $DESTINO $ANTERIOR"
	BIN_SALVO=sim
fi
ct "mv -f $NOVO $DESTINO"
echo "in production now: $(ct "sha256sum $DESTINO")"

passo "restarting the service"
ct "systemctl reset-failed zapgw" || true
ct "systemctl enable zapgw" >/dev/null
ct "systemctl restart zapgw"

# ---------------------------------------------------------------- verdict

passo "waiting for $URL_SAUDE to respond (up to ${ESPERA}s)"
if corpo=$(esperar_saude); then
	echo "HEALTH OK: $corpo"

	# Responding isn't enough: it has to be the binary THIS DEPLOY BUILT.
	# The "|| veredito=$?" is required — without it the "set -e" at the top
	# would kill the script on return 1, skipping the rollback, which is
	# exactly the point.
	veredito=0
	conferir_versao "$corpo" "$VERSAO_DO_BUILD" || veredito=$?

	if [ "$veredito" -ne 1 ]; then
		avisos_nome_obsoleto
		passo "DEPLOY COMPLETE"
		ct "systemctl is-active zapgw"
		exit 0
	fi

	erro "the gateway responded, but it is NOT the binary this deploy built."
	erro "rolling back: publishing a binary that does not know its own version poisons"
	erro "every future diagnosis, and it does so with the deploy painted green."
else
	erro "/v1/health did NOT respond within ${ESPERA}s"
	ct "systemctl status zapgw --no-pager -l" 2>&1 | tail -20 || true
	ct "journalctl -u zapgw -n 20 --no-pager" 2>&1 | tail -20 || true
fi

reverter

passo "checking whether the previous binary is responding again"
if corpo=$(esperar_saude); then
	echo "HEALTH OK (previous binary): $corpo"
	imprimir_versao_do_corpo "$corpo"
	erro "DEPLOY ROLLED BACK — the new binary was rejected. Nothing stayed in production."
	exit 1
fi

erro "ALARM: the rollback ran and /v1/health is still silent. The gateway is DOWN."
erro "ALARM: snapshot $SNAP of CT $VMID is available for a manual rollback."
exit 2
