#!/usr/bin/env bash
#
# valida-lideranca.sh — prova, com o BINARIO REAL subindo de verdade, que a
# guarda de singleton do envio (internal/outbound/lideranca.go) esta no caminho
# e obedece a configuracao.
#
# POR QUE ELE EXISTE, se ja ha 8 testes de unidade da guarda: os testes de
# unidade constroem a `Lideranca` a mao e chamam `Exigir` direto. Eles NUNCA
# exercitam a fiacao de cmd/zapgw/main.go — ou seja, nao provam que o embrulho
# foi mesmo aplicado a rota `POST /v1/messages`, nem que as variaveis de
# ambiente sao lidas como se espera. Um refactor que passasse o handler CRU
# para `rotas()` deixaria a suite inteira verde e a producao desprotegida.
#
# O QUE PROVA DE VERDADE E O PAR B x A, e nao a recusa sozinha: entre os dois
# casos a UNICA coisa que muda e o arquivo de concessao, e a resposta sai de
# 503 (guarda) para 401 (autenticacao). Isso mostra as tres coisas de uma vez:
# a guarda esta no caminho, ela roda ANTES da autenticacao, e ela ABRE quando a
# concessao existe. Um teste que so visse a recusa nao distinguiria "guarda
# funcionando" de "rota quebrada" — e os dois dao 503.
#
# E o caso D prova a propriedade que mais importa no dia a dia de hoje: com a
# guarda DESARMADA o comportamento e o de antes de ela existir. E a instalacao
# de no unico, que e a que roda em producao enquanto o par nao existe.
#
# NADA AQUI TOCA PRODUCAO. Banco temporario, porta alta em 127.0.0.1, e uma
# chave de cifra de teste (zeros — obviamente falsa de proposito, para ninguem
# a confundir com credencial). O diretorio inteiro e apagado na saida.
#
# Uso:
#   implanta/valida-lideranca.sh
#
# Ajustes por ambiente:
#   ZAPGW_VALIDA_PORTA     18099   (porta de loopback para o gateway de teste)
#   ZAPGW_VALIDA_BINARIO   (vazio) caminho de um binario JA compilado, para
#                          pular o build — mesmo padrao do deploy.sh.
#
# Saida:
#   0  TODOS OS CASOS PASSARAM
#   1  ALGUM CASO FALHOU — a guarda nao se comporta como especificado
#   2  INCONCLUSIVO — nao deu para medir (build falhou, porta ocupada, o
#      gateway nao subiu, o `touch` nao envelheceu o arquivo). NAO e verde e
#      NAO e prova de defeito. Saida 2 tratada como sucesso e exatamente o
#      monitor cego que este projeto documenta em docs/ARMADILHAS.md.
set -uo pipefail

PORTA=${ZAPGW_VALIDA_PORTA:-18099}
BASE="http://127.0.0.1:${PORTA}"

inconclusivo() { echo "INCONCLUSIVO: $*" >&2; exit 2; }

command -v curl >/dev/null 2>&1 || inconclusivo "curl nao esta no PATH"

RAIZ=$(mktemp -d) || inconclusivo "nao consegui criar diretorio temporario"
trap 'derrubar 2>/dev/null; rm -rf "$RAIZ"' EXIT

CONCESSAO="$RAIZ/lider"
BIN=${ZAPGW_VALIDA_BINARIO:-}
if [ -z "$BIN" ]; then
	BIN="$RAIZ/zapgw$(go env GOEXE)"
	echo "== compilando o binario de teste"
	CGO_ENABLED=0 go build -o "$BIN" ./cmd/zapgw || inconclusivo "o build falhou — sem binario nao ha o que medir"
fi
[ -x "$BIN" ] || inconclusivo "binario inexistente ou sem permissao de execucao: $BIN"

# A chave e' de teste e o banco e' descartavel. Nao ha segredo real neste
# arquivo, e nao pode passar a haver.
export ZAPGW_CHAVE_CIFRA=0000000000000000000000000000000000000000000000000000000000000000
export ZAPGW_BANCO="$RAIZ/teste.db"
export ZAPGW_ENDERECO="127.0.0.1:${PORTA}"

if curl -s -o /dev/null --max-time 2 "$BASE/v1/health"; then
	inconclusivo "ja ha alguem ouvindo em $BASE — escolha outra com ZAPGW_VALIDA_PORTA"
fi

PID=""
subir() { # $1 = valor de ZAPGW_LIDERANCA_ARQUIVO ("" desarma)
	if [ -n "$1" ]; then export ZAPGW_LIDERANCA_ARQUIVO="$1"; else unset ZAPGW_LIDERANCA_ARQUIVO; fi
	export ZAPGW_LIDERANCA_VALIDADE=5s
	"$BIN" > "$RAIZ/saida.log" 2>&1 &
	PID=$!
	for _ in $(seq 1 40); do
		curl -s -o /dev/null "$BASE/v1/health" && return 0
		sleep 0.25
	done
	echo "--- log do gateway ---" >&2; cat "$RAIZ/saida.log" >&2
	inconclusivo "o gateway nao subiu em 10 s"
}
derrubar() { [ -n "$PID" ] && kill "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; PID=""; }

enviar() {
	curl -s -o "$RAIZ/corpo.json" -w '%{http_code}' -X POST "$BASE/v1/messages" \
		-H 'Content-Type: application/json' \
		-d '{"instancia":"x","para":"5511999999999","tipo":"texto","texto":"oi"}'
}

falhou=0
checar() { # $1 nome  $2 esperado  $3 obtido  $4 trecho exigido no corpo ("" ignora)
	local veredito="OK    "
	if [ "$2" != "$3" ]; then veredito="FALHOU"; falhou=1; fi
	if [ -n "$4" ] && ! grep -qi "$4" "$RAIZ/corpo.json"; then veredito="FALHOU"; falhou=1; fi
	printf '%-56s esperado=%s obtido=%s  %s\n' "$1" "$2" "$3" "$veredito"
	if [ "$veredito" = "FALHOU" ]; then
		echo "   corpo: $(cat "$RAIZ/corpo.json")"
		echo "   log:   $(grep -i lideranca "$RAIZ/saida.log" | tail -1)"
	fi
}

echo "== A) guarda ARMADA, concessao AUSENTE -> tem de RECUSAR"
rm -f "$CONCESSAO"
subir "$CONCESSAO"
grep -q "guarda de lideranca ARMADA" "$RAIZ/saida.log" \
	|| { echo "FALHOU: a subida nao anunciou ARMADA"; falhou=1; }
checar "A) ausente recusa" 503 "$(enviar)" "lideranca"
derrubar

echo
echo "== B) guarda ARMADA, concessao FRESCA -> tem de ABRIR   [o par que prova]"
echo ok > "$CONCESSAO"
subir "$CONCESSAO"
# 401 = chegou na AUTENTICACAO. Nao e' "deu erro": e' a prova de que a guarda
# deixou passar e o pedido seguiu para o handler real, que recusa por falta de
# Authorization. Se aqui viesse 503, a guarda estaria presa fechada.
checar "B) fresca abre (chega ao 401 da autenticacao)" 401 "$(enviar)" ""

echo
echo "== C) MESMO processo, concessao ENVELHECIDA -> volta a RECUSAR"
# Sem reiniciar, de proposito: isto prova que a guarda confere A CADA
# REQUISICAO, e nao uma vez na subida. Uma implementacao que lesse o arquivo so
# no arranque passaria em A, B e D e falharia exatamente aqui — que e o caso
# real do titular que PERDE a concessao com o processo vivo.
if ! touch -d "@$(( $(date +%s) - 600 ))" "$CONCESSAO" 2>/dev/null; then
	derrubar
	inconclusivo "o \`touch -d\` desta maquina nao envelhece arquivo; nao da para medir o caso C"
fi
checar "C) vencida recusa, sem reiniciar" 503 "$(enviar)" "lideranca"
derrubar

echo
echo "== D) guarda DESARMADA -> comportamento identico ao de antes"
subir ""
grep -q "guarda de lideranca DESARMADA" "$RAIZ/saida.log" \
	|| { echo "FALHOU: a subida nao anunciou DESARMADA"; falhou=1; }
checar "D) desarmada abre (chega ao 401)" 401 "$(enviar)" ""
derrubar

echo
echo "== E) validade ILEGIVEL -> o gateway NAO PODE SUBIR"
export ZAPGW_LIDERANCA_ARQUIVO="$CONCESSAO"
export ZAPGW_LIDERANCA_VALIDADE="quinze"
if "$BIN" > "$RAIZ/saida2.log" 2>&1; then
	echo "E) FALHOU: subiu com validade ilegivel"
	falhou=1
elif grep -qi "nao e uma duracao valida" "$RAIZ/saida2.log"; then
	echo "E) OK     recusou subir: $(tail -1 "$RAIZ/saida2.log")"
else
	echo "E) FALHOU: saiu com erro, mas nao pela validade — $(tail -1 "$RAIZ/saida2.log")"
	falhou=1
fi

echo
echo "== F) ARMADA sem validade -> o gateway NAO PODE SUBIR"
# O default de validade foi REMOVIDO em 2026-08-18: o valor seguro depende do
# TTL da concessao no etcd, que este processo nao conhece, entao qualquer
# default e' um chute sobre configuracao alheia — e o chute errado deixa dois
# nos se acharem titulares. Este caso guarda a remocao.
export ZAPGW_LIDERANCA_ARQUIVO="$CONCESSAO"
unset ZAPGW_LIDERANCA_VALIDADE
if "$BIN" > "$RAIZ/saida3.log" 2>&1; then
	echo "F) FALHOU: subiu armada sem validade — um default silencioso voltou"
	falhou=1
elif grep -q "V + A < T" "$RAIZ/saida3.log"; then
	echo "F) OK     recusou subir, e a mensagem traz a formula"
else
	echo "F) FALHOU: recusou, mas sem a formula — quem arma precisa dela, nao de um \"faltou variavel\""
	falhou=1
fi

echo
if [ "$falhou" -eq 0 ]; then
	echo "TODOS OS CASOS PASSARAM"
else
	echo "HOUVE FALHA — a guarda nao se comporta como especificado"
fi
exit "$falhou"
