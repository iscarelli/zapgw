#!/usr/bin/env bash
#
# valida-lideranca.sh — proves, with the REAL BINARY actually coming up, that
# the sending singleton guard (internal/outbound/leadership.go) is in the
# path and obeys its configuration.
#
# WHY IT EXISTS, when there are already 8 unit tests for the guard: the unit
# tests build the `Leadership` by hand and call `Exigir` directly. They NEVER
# exercise cmd/zapgw/main.go's wiring — that is, they don't prove the wrapper
# was actually applied to the `POST /v1/messages` route, nor that the
# environment variables are read the way they're expected to be. A refactor
# that passed the RAW handler to `rotas()` would leave the whole suite green
# and production unprotected.
#
# WHAT REALLY PROVES IT IS THE B x A PAIR, not the refusal alone: between the
# two cases the ONLY thing that changes is the grant file, and the response
# goes from 503 (guard) to 401 (authentication). That shows three things at
# once: the guard is in the path, it runs BEFORE authentication, and it OPENS
# when the grant exists. A test that only saw the refusal wouldn't
# distinguish "guard working" from "route broken" — both give 503.
#
# And case D proves the property that matters most day to day today: with the
# guard DISARMED the behavior is the one from before it existed. That's the
# single-node install, which is the one running in production while the pair
# doesn't exist.
#
# NOTHING HERE TOUCHES PRODUCTION. A temporary database, a high port on
# 127.0.0.1, and a test encryption key (zeros — obviously fake on purpose, so
# no one mistakes it for a credential). The whole directory is deleted on exit.
#
# Usage:
#   implanta/valida-lideranca.sh
#
# Per-environment adjustments:
#   ZAPGW_VALIDA_PORTA     18099   (loopback port for the test gateway)
#   ZAPGW_VALIDA_BINARIO   (empty) path to an ALREADY-built binary, to skip
#                          the build — same pattern as deploy.sh.
#
# Exit:
#   0  ALL CASES PASSED
#   1  SOME CASE FAILED — the guard does not behave as specified
#   2  INCONCLUSIVE — could not measure it (build failed, port busy, the
#      gateway didn't come up, `touch` didn't age the file). This is NOT
#      green and NOT proof of a defect. Treating exit 2 as success is exactly
#      the blind monitor this project documents in docs/ARMADILHAS.md.
set -uo pipefail

PORTA=${ZAPGW_VALIDA_PORTA:-18099}
BASE="http://127.0.0.1:${PORTA}"

inconclusivo() { echo "INCONCLUSIVE: $*" >&2; exit 2; }

command -v curl >/dev/null 2>&1 || inconclusivo "curl is not in PATH"

RAIZ=$(mktemp -d) || inconclusivo "could not create a temporary directory"
trap 'derrubar 2>/dev/null; rm -rf "$RAIZ"' EXIT

CONCESSAO="$RAIZ/lider"
BIN=${ZAPGW_VALIDA_BINARIO:-}
if [ -z "$BIN" ]; then
	BIN="$RAIZ/zapgw$(go env GOEXE)"
	echo "== building the test binary"
	CGO_ENABLED=0 go build -o "$BIN" ./cmd/zapgw || inconclusivo "the build failed — with no binary there's nothing to measure"
fi
[ -x "$BIN" ] || inconclusivo "binary missing or not executable: $BIN"

# The key is a test one and the database is disposable. There is no real
# secret in this file, and there must never be one.
export ZAPGW_CHAVE_CIFRA=0000000000000000000000000000000000000000000000000000000000000000
export ZAPGW_BANCO="$RAIZ/teste.db"
export ZAPGW_ENDERECO="127.0.0.1:${PORTA}"

if curl -s -o /dev/null --max-time 2 "$BASE/v1/health"; then
	inconclusivo "something is already listening on $BASE — pick another one with ZAPGW_VALIDA_PORTA"
fi

PID=""
subir() { # $1 = value of ZAPGW_LIDERANCA_ARQUIVO ("" disarms)
	if [ -n "$1" ]; then export ZAPGW_LIDERANCA_ARQUIVO="$1"; else unset ZAPGW_LIDERANCA_ARQUIVO; fi
	export ZAPGW_LIDERANCA_VALIDADE=5s
	"$BIN" > "$RAIZ/saida.log" 2>&1 &
	PID=$!
	for _ in $(seq 1 40); do
		curl -s -o /dev/null "$BASE/v1/health" && return 0
		sleep 0.25
	done
	echo "--- gateway log ---" >&2; cat "$RAIZ/saida.log" >&2
	inconclusivo "the gateway did not come up within 10s"
}
derrubar() { [ -n "$PID" ] && kill "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; PID=""; }

enviar() {
	curl -s -o "$RAIZ/corpo.json" -w '%{http_code}' -X POST "$BASE/v1/messages" \
		-H 'Content-Type: application/json' \
		-d '{"instancia":"x","para":"5511999999999","tipo":"texto","texto":"oi"}'
}

falhou=0
checar() { # $1 name  $2 expected  $3 got  $4 substring required in the body ("" ignores it)
	local veredito="OK    "
	if [ "$2" != "$3" ]; then veredito="FAILED"; falhou=1; fi
	if [ -n "$4" ] && ! grep -qi "$4" "$RAIZ/corpo.json"; then veredito="FAILED"; falhou=1; fi
	printf '%-56s expected=%s got=%s  %s\n' "$1" "$2" "$3" "$veredito"
	if [ "$veredito" = "FAILED" ]; then
		echo "   body: $(cat "$RAIZ/corpo.json")"
		# zapgw:log-coupling "lideranca"
		echo "   log:  $(grep -i lideranca "$RAIZ/saida.log" | tail -1)"
	fi
}

echo "== A) guard ARMED, grant ABSENT -> must REFUSE"
rm -f "$CONCESSAO"
subir "$CONCESSAO"
# zapgw:log-coupling "leadership guard ARMED"
grep -q "leadership guard ARMED" "$RAIZ/saida.log" \
	|| { echo "FAILED: the startup did not announce ARMED"; falhou=1; }
# zapgw:log-coupling "lideranca"
checar "A) absent refuses" 503 "$(enviar)" "lideranca"
derrubar

echo
echo "== B) guard ARMED, grant FRESH -> must OPEN   [the pair that proves it]"
echo ok > "$CONCESSAO"
subir "$CONCESSAO"
# 401 = reached AUTHENTICATION. It's not "an error happened": it's the proof
# that the guard let it through and the request went on to the real handler,
# which refuses for lack of Authorization. If 503 came out here, the guard
# would be stuck shut.
checar "B) fresh opens (reaches the 401 from authentication)" 401 "$(enviar)" ""

echo
echo "== C) SAME process, grant AGED -> back to REFUSING"
# Without restarting, on purpose: this proves the guard checks on EVERY
# REQUEST, not once at startup. An implementation that only read the file at
# startup would pass A, B and D and would fail exactly here — which is the
# real-world case of a titular that LOSES the grant while the process is alive.
if ! touch -d "@$(( $(date +%s) - 600 ))" "$CONCESSAO" 2>/dev/null; then
	derrubar
	inconclusivo "this machine's \`touch -d\` does not age a file; case C cannot be measured"
fi
# zapgw:log-coupling "lideranca"
checar "C) expired refuses, without restarting" 503 "$(enviar)" "lideranca"
derrubar

echo
echo "== D) guard DISARMED -> behavior identical to before it existed"
subir ""
# zapgw:log-coupling "leadership guard DISARMED"
grep -q "leadership guard DISARMED" "$RAIZ/saida.log" \
	|| { echo "FAILED: the startup did not announce DISARMED"; falhou=1; }
checar "D) disarmed opens (reaches 401)" 401 "$(enviar)" ""
derrubar

echo
echo "== E) UNREADABLE validity -> the gateway MUST NOT COME UP"
export ZAPGW_LIDERANCA_ARQUIVO="$CONCESSAO"
export ZAPGW_LIDERANCA_VALIDADE="quinze"
if "$BIN" > "$RAIZ/saida2.log" 2>&1; then
	echo "E) FAILED: came up with an unreadable validity"
	falhou=1
# zapgw:log-coupling "nao e uma duracao valida"
elif grep -qi "nao e uma duracao valida" "$RAIZ/saida2.log"; then
	echo "E) OK     refused to come up: $(tail -1 "$RAIZ/saida2.log")"
else
	echo "E) FAILED: exited with an error, but not because of the validity — $(tail -1 "$RAIZ/saida2.log")"
	falhou=1
fi

echo
echo "== F) ARMED without validity -> the gateway MUST NOT COME UP"
# The validity default was REMOVED on 2026-08-18: the safe value depends on
# the grant's TTL in etcd, which this process doesn't know, so any default is
# a guess about someone else's configuration — and the wrong guess leaves two
# nodes thinking they're the titular. This case guards that removal.
export ZAPGW_LIDERANCA_ARQUIVO="$CONCESSAO"
unset ZAPGW_LIDERANCA_VALIDADE
if "$BIN" > "$RAIZ/saida3.log" 2>&1; then
	echo "F) FAILED: came up armed without validity — a silent default came back"
	falhou=1
# zapgw:log-coupling "V + A < T"
elif grep -q "V + A < T" "$RAIZ/saida3.log"; then
	echo "F) OK     refused to come up, and the message carries the formula"
else
	echo "F) FAILED: refused, but without the formula — whoever arms it needs it, not a \"missing variable\""
	falhou=1
fi

echo
if [ "$falhou" -eq 0 ]; then
	echo "ALL CASES PASSED"
else
	echo "THERE WAS A FAILURE — the guard does not behave as specified"
fi
exit "$falhou"
