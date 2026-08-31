#!/usr/bin/env bash
#
# deploy.sh — compila, envia, troca e VERIFICA o zapgw no container de destino.
#
# A razao de existir deste script nao e "copiar um binario": e ABORTAR quando o
# binario novo nao responde. Sem o passo do /v1/health, um binario quebrado sobe
# e o gateway fica fora do ar sem ninguem saber — a Meta simplesmente para de
# entregar, e do lado de fora e indistinguivel de "nao chegou mensagem".
#
# E ABORTAR TAMBEM quando o binario responde, mas NAO E O QUE ESTE DEPLOY
# CONSTRUIU (T-184). A versao entra por `-ldflags "-X main.version=..."`, e o
# linker Go ignora EM SILENCIO um simbolo que nao existe: renomeie a variavel no
# codigo e o build continua saindo 0, so que o binario responde
# "desenvolvimento". Medido com controle positivo em 2026-08-30. Sem a conferencia
# abaixo, esse deploy termina VERDE publicando um gateway que nao sabe que versao
# e — e a partir dai todo diagnostico de producao se apoia num numero errado.
#
# Ele NAO copia /etc/zapgw/env e NAO toca em ZAPGW_CHAVE_CIFRA. A chave mora so
# no CT; copia-la seria criar uma segunda copia para vazar.
#
# Ele TAMBEM instala /etc/profile.d/zapgw.sh (T-090), que e o que faz o comando
# `zapgw` funcionar para quem entra por `pct enter`/`pct exec` — sem ele, CT
# novo (ou reconstruido) nasce com `command not found`. Esse passo NAO entra no
# caminho de reversao e falhar nele nao aborta o deploy — mas fica visivel
# (ALARME no stdout), nunca engolido.
#
# Uso:
#   ZAPGW_DEPLOY_HOST=usuario@no ZAPGW_DEPLOY_VMID=100 \
#   ZAPGW_DEPLOY_SAUDE=http://<ip-interno-do-gateway>:8080/v1/health \
#   implanta/deploy.sh
#
# OBRIGATORIAS — nao tem default, e a ausencia de default e a propria protecao:
#   ZAPGW_DEPLOY_VMID      id numerico do container no Proxmox
#   ZAPGW_DEPLOY_HOST      destino SSH do no que hospeda o container
#   ZAPGW_DEPLOY_SAUDE     URL do /v1/health, alcancavel A PARTIR do no
#
# Opcionais:
#   ZAPGW_DEPLOY_CHAVE     (vazio) — chave SSH. Vazio deixa o ssh resolver como
#                          resolveria em qualquer outro comando (agente,
#                          ~/.ssh/config, chave padrao).
#   ZAPGW_DEPLOY_ESPERA_S  30
#   ZAPGW_DEPLOY_BINARIO   (vazio) — caminho de um binario JA compilado, para
#                          pular o build. Existe para provar a reversao com um
#                          binario propositalmente quebrado sem sujar o repo.
#                          Com ele NAO ha versao de build para comparar, entao a
#                          conferencia de versao e PULADA (dita em voz alta, nao
#                          calada) — justamente para nao invalidar essa
#                          ferramenta, cujo binario diverge de proposito.
#
# Saida: 0 deploy ok; 1 deploy falhou e FOI REVERTIDO — ou o /v1/health nao
# respondeu, ou respondeu uma versao diferente da que foi construida; 2 deploy
# falhou e a reversao NAO restaurou a saude — precisa de gente agora; 3
# configuracao obrigatoria ausente — NADA foi feito e nenhuma rede foi tocada.

set -euo pipefail

passo() { printf '\n== %s\n' "$*"; }
erro() { printf 'ERRO: %s\n' "$*" >&2; }

# --------------------------------------------------- configuracao obrigatoria
#
# Ate 2026-08-30 estas variaveis tinham default apontando para o no, o container
# e o IP de UMA casa. Num repositorio publico um default assim nao e so um
# endereco vazado: e um script que, rodado por quem nao leu, tenta implantar num
# host que nao e dele. Entao a falta PARA o script aqui — antes do build e antes
# de qualquer ssh —, dizendo o NOME da variavel e o FORMATO esperado, porque
# "faltou variavel" sozinho nao diz o que escrever.
faltando=0
exigir() { # $1 nome  $2 formato esperado  $3 exemplo
	if [ -z "${!1:-}" ]; then
		erro "falta a variavel de ambiente $1 — $2"
		erro "       exemplo: $1=$3"
		faltando=1
	fi
}

exigir ZAPGW_DEPLOY_VMID \
	"id NUMERICO do container no Proxmox (o mesmo que o \`pct\` usa)" \
	"100"
exigir ZAPGW_DEPLOY_HOST \
	"destino SSH do no que hospeda o container, no formato usuario@host" \
	"deploy@no-proxmox.exemplo.internal"
exigir ZAPGW_DEPLOY_SAUDE \
	"URL do /v1/health do gateway, alcancavel A PARTIR do no (nao do seu terminal)" \
	"http://<ip-interno-do-gateway>:8080/v1/health"

if [ "$faltando" -ne 0 ]; then
	erro "nada foi feito: o deploy para antes de tocar em rede."
	exit 3
fi

case ${ZAPGW_DEPLOY_VMID} in
*[!0-9]*)
	erro "ZAPGW_DEPLOY_VMID=${ZAPGW_DEPLOY_VMID} nao e um id numerico de container"
	erro "nada foi feito: o deploy para antes de tocar em rede."
	exit 3
	;;
esac

VMID=$ZAPGW_DEPLOY_VMID
HOST=$ZAPGW_DEPLOY_HOST
URL_SAUDE=$ZAPGW_DEPLOY_SAUDE
CHAVE=${ZAPGW_DEPLOY_CHAVE:-}
ESPERA=${ZAPGW_DEPLOY_ESPERA_S:-30}
BIN_PRONTO=${ZAPGW_DEPLOY_BINARIO:-}

# Opcoes comuns a ssh e scp. O `-i` so entra se houver chave declarada: o
# default anterior nomeava a chave de UMA maquina, e nao servia a mais ninguem.
# Sem ela, o ssh resolve a chave como faria em qualquer outro comando.
SSH_OPCOES=(-o BatchMode=yes -o ConnectTimeout=10)
if [ -n "$CHAVE" ]; then
	SSH_OPCOES+=(-i "$CHAVE")
fi

DESTINO=/usr/local/bin/zapgw
ANTERIOR=/usr/local/bin/zapgw.anterior
NOVO=/usr/local/bin/zapgw.novo
UNIT=/etc/systemd/system/zapgw.service
UNIT_ANTERIOR=/etc/systemd/system/zapgw.service.anterior

# T-194: cada deploy cria um snapshot com nome PROPRIO (prefixo + timestamp),
# em vez de reaproveitar sempre o mesmo nome "pre-update". So assim faz sentido
# "manter os 3 ultimos" — um nome fixo reescrito a cada deploy nunca teria mais
# de um para escolher. O prefixo e o padrao abaixo sao usados tambem pela poda
# (podar_snapshots, mais abaixo): SO um nome que bate no padrao EXATO entra na
# selecao — um snapshot criado por humano, com outro nome, fica de fora sempre.
SNAP_PREFIXO=pre-update
SNAP_PADRAO="^${SNAP_PREFIXO}-[0-9]{14}\$"
SNAP_MANTER=3
SNAP="${SNAP_PREFIXO}-$(date -u +%Y%m%d%H%M%S)"

RAIZ=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# O que a reversao tem para desfazer. Declarados aqui, e nao so onde sao
# preenchidos, porque reverter() le os dois e com "set -u" uma falha antes da
# troca derrubaria o proprio caminho de reversao.
UNIT_SALVA=
BIN_SALVO=

remoto() { ssh "${SSH_OPCOES[@]}" "$HOST" "$1"; }

# ct roda UM comando dentro do CT. O comando nao pode conter aspas simples: ele
# viaja dentro de um par delas ate o pct exec.
ct() { remoto "sudo /usr/sbin/pct exec $VMID -- /bin/sh -c '$1'"; }

# esperar_saude pergunta pelo /v1/health de DENTRO da rede (do no Proxmox, nao
# do CT): responder no loopback nao prova que o Traefik consegue chegar. Um
# unico ssh faz o laco inteiro — 30 conexoes seriam mais lentas que o proprio
# limite que estamos medindo.
esperar_saude() {
	ssh "${SSH_OPCOES[@]}" "$HOST" \
		bash -s "$URL_SAUDE" "$ESPERA" <<-'FIM'
		url=$1
		limite=$2
		i=0
		while [ "$i" -lt "$limite" ]; do
			corpo=$(curl -fsS -m 2 "$url" 2>/dev/null || true)
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

# extrair_versao_do_corpo imprime o valor do campo "versao" do JSON que o
# /v1/health devolveu, e retorna 1 (sem imprimir nada) quando o campo NAO esta
# la. Esse retorno e a fronteira entre "a versao esta errada" e "nao consegui
# ler a versao" — as duas outras funcoes daqui se apoiam nele para nao
# confundir uma coisa com a outra.
extrair_versao_do_corpo() {
	local linha valor
	linha=$(printf '%s' "$1" | grep -o '"versao":"[^"]*"' || true)
	[ -n "$linha" ] || return 1
	valor=${linha#'"versao":"'}
	valor=${valor%'"'}
	printf '%s\n' "$valor"
}

# imprimir_versao_do_corpo imprime uma linha BEM visivel com a versao que o
# gateway respondeu — a prova de que subiu o binario CERTO nao pode ficar
# escondida dentro do JSON cru (T-025: em 2026-07-25 um binario com contrato
# novo subiu enquanto a tag mais recente era outra, e so alguem lembrando disso
# evitou uma investigacao contra a coisa errada).
#
# Ela so IMPRIME, nunca retorna erro, e e usada onde NAO ha o que comparar: no
# probe DEPOIS da reversao, onde a versao que responde e a do binario ANTERIOR
# e portanto diverge de VERSAO_DO_BUILD por construcao. Comparar ali acusaria
# falha justamente no caminho que acabou de salvar o gateway.
imprimir_versao_do_corpo() {
	local valor
	if valor=$(extrair_versao_do_corpo "$1"); then
		echo "VERSAO: $valor"
	else
		echo "VERSAO: desconhecida — este binario e anterior a T-025 e o /v1/health nao tem o campo \"versao\" (isso nao aborta o deploy)"
	fi
}

# conferir_versao compara o que o gateway RESPONDEU com o que este deploy
# CONSTRUIU, e devolve TRES desfechos diferentes — a distincao e o ponto da
# funcao, nao um detalhe dela (T-184):
#
#   0  bateu — segue o deploy.
#   1  DIVERGIU — o binario publicado nao e o que foi construido. Aborta e
#      reverte. Acontece de verdade quando o `-X main.version=...` do build erra
#      o simbolo: o linker Go ignora em silencio e o binario sobe respondendo
#      "desenvolvimento", com build verde e push verde.
#   2  NAO DEU PARA CONFERIR — e isso NAO E FALHA. Duas causas legitimas: o
#      binario e anterior a T-025 e nao tem o campo "versao"; ou o deploy usou
#      ZAPGW_DEPLOY_BINARIO e nao ha versao de build para comparar.
#
# Tratar (2) como (1) transformaria este script num monitor que grita sem saber,
# e tratar (1) como (2) e o defeito que a T-184 veio corrigir. Por isso os tres
# saem separados daqui, e quem chama decide — nenhuma delas silencia:
# todas imprimem.
conferir_versao() {
	local corpo=$1 esperada=$2 respondida
	if [ -z "$esperada" ]; then
		echo "VERSAO: nao conferida — o deploy usou ZAPGW_DEPLOY_BINARIO (build pulado), entao nao ha versao de build para comparar (isso nao aborta o deploy)"
		return 2
	fi
	if ! respondida=$(extrair_versao_do_corpo "$corpo"); then
		echo "VERSAO: nao conferida — este binario e anterior a T-025 e o /v1/health nao tem o campo \"versao\" (isso nao aborta o deploy)"
		return 2
	fi
	if [ "$respondida" = "$esperada" ]; then
		echo "VERSAO CONFERE: $respondida (igual a construida)"
		return 0
	fi
	erro "VERSAO DIVERGE: construida=$esperada respondida=$respondida"
	return 1
}

# reverter desfaz a troca e devolve o servico ao binario anterior.
#
# O reset-failed nao e enfeite: com Restart=always, um binario que morre na hora
# estoura o StartLimitBurst em segundos e o systemd passa a RECUSAR o start
# ("start request repeated too quickly"). Sem limpar o estado, o restart da
# reversao falha calado e o gateway fica fora do ar justamente no caminho que
# existe para evita-lo.
reverter() {
	passo "REVERTENDO"
	if [ -n "$UNIT_SALVA" ]; then
		ct "mv -f $UNIT_ANTERIOR $UNIT"
		ct "systemctl daemon-reload"
		echo "unit anterior restaurada"
	fi
	if [ -n "$BIN_SALVO" ]; then
		ct "mv -f $ANTERIOR $DESTINO"
		echo "binario anterior restaurado: $(ct "sha256sum $DESTINO")"
	else
		erro "ALARME: nao havia binario anterior para restaurar (primeira instalacao)."
		erro "ALARME: parando o servico para nao deixar laco de restart. Acao humana necessaria."
		ct "systemctl stop zapgw" || true
		return
	fi
	ct "systemctl reset-failed zapgw" || true
	ct "systemctl restart zapgw" || erro "o restart da reversao falhou"
}

# selecionar_poda e PURA: le por stdin uma lista de nomes de snapshot (um por
# linha, sem mais nada em cada linha) e imprime os que sobram fora do "$SNAP_MANTER
# mais recentes" — ou seja, os que a poda removeria. Nao toca em rede, nao apaga
# nada. So considera nomes que batem no SNAP_PADRAO exato; qualquer outro nome
# (snapshot manual, com outro texto) e ignorado por inteiro, nunca contado nem
# selecionado. E a funcao que o modo de ensaio do Verify chama diretamente,
# contra uma lista real de nomes, sem exigir acesso a producao para provar a
# selecao (T-194).
selecionar_poda() {
	local batem total excedente
	batem=$(grep -E "$SNAP_PADRAO" | sort || true)
	[ -n "$batem" ] || return 0
	total=$(printf '%s\n' "$batem" | grep -c .)
	excedente=$((total - SNAP_MANTER))
	[ "$excedente" -gt 0 ] || return 0
	printf '%s\n' "$batem" | sed -n "1,${excedente}p"
}

# podar_snapshots so roda DEPOIS de o deploy ter dado certo (nunca antes, nunca
# se reverteu — quem chama decide isso, esta funcao nao verifica). Le a lista
# REAL de snapshots do CT, entrega so os nomes ao selecionar_poda() e apaga so
# o que ela selecionar. Se a listagem falhar ou vier vazia/sem nada que bata no
# padrao, NAO apaga nada — poda as cegas e pior que nao podar.
podar_snapshots() {
	passo "podando snapshots $SNAP_PREFIXO antigos (mantendo os $SNAP_MANTER mais recentes)"
	local bruto nomes selecao removidos=0 restantes
	if ! bruto=$(remoto "sudo /usr/sbin/pct listsnapshot $VMID" 2>&1); then
		erro "poda pulada: nao consegui listar os snapshots do CT $VMID"
		erro "$bruto"
		return 0
	fi
	# Tokeniza por espaco (o "pct listsnapshot" desenha uma arvore com
	# `->  antes do nome) e testa cada TOKEN INTEIRO contra SNAP_PADRAO — nunca
	# um grep de substring. Substring pegaria um nome humano que so CONTEM o
	# padrao, tipo "pre-update-20260830090000-testedomeuadm"; o token inteiro
	# desse nome nao bate no padrao ancorado (^...$), entao fica de fora.
	nomes=$(printf '%s\n' "$bruto" | tr -s '[:space:]' '\n' | grep -E "$SNAP_PADRAO" | sort -u || true)
	if [ -z "$nomes" ]; then
		echo "poda: nenhum snapshot com o prefixo $SNAP_PREFIXO- (nome deste script) encontrado — nada a fazer"
		return 0
	fi
	selecao=$(printf '%s\n' "$nomes" | selecionar_poda)
	if [ -z "$selecao" ]; then
		echo "poda: $(printf '%s\n' "$nomes" | grep -c .) snapshot(s) dentro do limite de $SNAP_MANTER, nada a remover"
		return 0
	fi
	while IFS= read -r nome; do
		[ -n "$nome" ] || continue
		if remoto "sudo /usr/sbin/pct delsnapshot $VMID $nome"; then
			removidos=$((removidos + 1))
			echo "poda: removido $nome"
		else
			erro "poda: falha ao remover $nome (deploy ja concluido, nao aborta por isso)"
		fi
	done <<<"$selecao"
	restantes=$(comm -23 <(printf '%s\n' "$nomes") <(printf '%s\n' "$selecao") | tr '\n' ' ')
	echo "poda: $removidos removido(s); ficaram: $restantes"
}

# ---------------------------------------------------------------- checagens

passo "checando acesso a $HOST e ao CT $VMID"
remoto "sudo /usr/sbin/pct status $VMID" | grep -q "status: running" ||
	{ erro "CT $VMID nao esta running"; exit 1; }
echo "CT $VMID running"

# ---------------------------------------------------------------- binario

# Vazia quer dizer "este deploy nao construiu nada, entao nao ha com o que
# comparar a versao que o gateway responder". Declarada aqui, e nao so onde e
# preenchida, porque com "set -u" o caminho do binario pronto derrubaria a
# conferencia la embaixo.
VERSAO_DO_BUILD=

if [ -n "$BIN_PRONTO" ]; then
	passo "USANDO BINARIO PRONTO (build pulado): $BIN_PRONTO"
	[ -f "$BIN_PRONTO" ] || { erro "binario nao existe: $BIN_PRONTO"; exit 1; }
	cp "$BIN_PRONTO" "$TMP/zapgw"
else
	# A versao vem do arquivo VERSION, e SO daqui — nunca digitada a mao no
	# deploy. Ela entra por -ldflags, nunca lida de disco pelo binario em
	# tempo de execucao (T-025): o VERSION nao vai para o CT, e um binario
	# que lesse versao de arquivo mentiria exatamente quando importa
	# (arquivo velho ao lado de binario novo — o incidente que abriu a T-025).
	VERSAO_DO_BUILD=$(cat "$RAIZ/VERSION")
	passo "compilando (CGO_ENABLED=0 GOOS=linux GOARCH=amd64), versao $VERSAO_DO_BUILD"
	(cd "$RAIZ" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags "-X main.version=$VERSAO_DO_BUILD" \
		-o "$TMP/zapgw" ./cmd/zapgw)
fi
echo "local: $(sha256sum "$TMP/zapgw")"

# ---------------------------------------------------------------- envio

passo "enviando para o no e empurrando para o CT como $NOVO"
scp "${SSH_OPCOES[@]}" -q "$TMP/zapgw" "$HOST:/tmp/zapgw.envio.$$"
remoto "sudo /usr/sbin/pct push $VMID /tmp/zapgw.envio.$$ $NOVO --perms 0755 --user 0 --group 0"
remoto "rm -f /tmp/zapgw.envio.$$"
echo "no CT: $(ct "sha256sum $NOVO")"

# ---------------------------------------------------------------- perfil

# T-090: /etc/profile.d/zapgw.sh e o que faz `zapgw` funcionar tambem para quem
# entra por `pct enter`/`pct exec` (docs/ARMADILHAS.md, "`command not found`
# dentro do CT nao quer dizer que o binario nao esta la").
# Isto NAO entra no caminho de reversao: nao e o binario, e falhar aqui nao
# justifica reverter um deploy saudavel. Mas tambem nao pode falhar CALADO por
# causa do "set -e" do topo do script — por isso cada passo e guardado com
# "||"/"if" e imprime ALARME antes de seguir, em vez de abortar o deploy
# inteiro ou engolir o erro.
passo "instalando /etc/profile.d/zapgw.sh"
if scp "${SSH_OPCOES[@]}" -q "$RAIZ/implanta/profile-zapgw.sh" "$HOST:/tmp/zapgw.profile.$$" &&
	remoto "sudo /usr/sbin/pct push $VMID /tmp/zapgw.profile.$$ /etc/profile.d/zapgw.sh --perms 0644 --user 0 --group 0"; then
	echo "perfil instalado: $(ct "sha256sum /etc/profile.d/zapgw.sh")"
else
	erro "ALARME: falha ao instalar /etc/profile.d/zapgw.sh — quem entrar por pct pode continuar sem o comando zapgw. Deploy segue."
fi
remoto "rm -f /tmp/zapgw.profile.$$" || true

# pct enter/exec nao le profile.d (e shell interativo, nao de login) — por isso
# o /root/.bashrc precisa dar source nele. Idempotente: so acrescenta se a
# linha ainda nao existir.
ct "grep -qF /etc/profile.d/zapgw.sh /root/.bashrc 2>/dev/null || echo . /etc/profile.d/zapgw.sh >> /root/.bashrc" ||
	erro "ALARME: falha ao garantir o source de /etc/profile.d/zapgw.sh em /root/.bashrc. Deploy segue."

# ---------------------------------------------------------------- snapshot

passo "snapshot $SNAP do CT $VMID"
# Nome unico por deploy (T-194): nao ha o que apagar antes de criar. Falhar
# aqui e de proposito: sem ponto de retorno, nao troca.
remoto "sudo /usr/sbin/pct snapshot $VMID $SNAP"

# ---------------------------------------------------------------- unit

passo "instalando a unit do systemd"
UNIT_SALVA=
if ct "test -f $UNIT"; then
	ct "cp -a $UNIT $UNIT_ANTERIOR"
	UNIT_SALVA=sim
fi
scp "${SSH_OPCOES[@]}" -q "$RAIZ/implanta/zapgw.service" "$HOST:/tmp/zapgw.service.$$"
remoto "sudo /usr/sbin/pct push $VMID /tmp/zapgw.service.$$ $UNIT --perms 0644 --user 0 --group 0"
remoto "rm -f /tmp/zapgw.service.$$"
ct "systemctl daemon-reload"

# ---------------------------------------------------------------- troca

passo "troca atomica do binario"
BIN_SALVO=
if ct "test -f $DESTINO"; then
	ct "mv -f $DESTINO $ANTERIOR"
	BIN_SALVO=sim
fi
ct "mv -f $NOVO $DESTINO"
echo "em producao agora: $(ct "sha256sum $DESTINO")"

passo "reiniciando o servico"
ct "systemctl reset-failed zapgw" || true
ct "systemctl enable zapgw" >/dev/null
ct "systemctl restart zapgw"

# ---------------------------------------------------------------- veredito

passo "esperando $URL_SAUDE responder (ate ${ESPERA}s)"
if corpo=$(esperar_saude); then
	echo "SAUDE OK: $corpo"

	# Responder nao basta: tem de ser o binario QUE ESTE DEPLOY CONSTRUIU.
	# O "|| veredito=$?" e obrigatorio — sem ele o "set -e" do topo mataria o
	# script no retorno 1, pulando a reversao que e justamente o ponto.
	veredito=0
	conferir_versao "$corpo" "$VERSAO_DO_BUILD" || veredito=$?

	if [ "$veredito" -ne 1 ]; then
		passo "DEPLOY CONCLUIDO"
		ct "systemctl is-active zapgw"
		# So poda com o deploy JA dado certo — nunca antes, nunca no caminho de
		# reversao (T-194). Falha na poda nao desfaz um deploy que ja subiu.
		podar_snapshots
		exit 0
	fi

	erro "o gateway respondeu, mas NAO e o binario que este deploy construiu."
	erro "revertendo: publicar um binario que nao sabe a propria versao envenena"
	erro "todo diagnostico futuro, e faz isso com o deploy pintado de verde."
else
	erro "o /v1/health NAO respondeu em ${ESPERA}s"
	ct "systemctl status zapgw --no-pager -l" 2>&1 | tail -20 || true
	ct "journalctl -u zapgw -n 20 --no-pager" 2>&1 | tail -20 || true
fi

reverter

passo "conferindo se o binario anterior voltou a responder"
if corpo=$(esperar_saude); then
	echo "SAUDE OK (binario anterior): $corpo"
	imprimir_versao_do_corpo "$corpo"
	erro "DEPLOY REVERTIDO — o binario novo foi recusado. Nada ficou em producao."
	exit 1
fi

erro "ALARME: a reversao rodou e o /v1/health continua mudo. O gateway esta FORA."
erro "ALARME: snapshot $SNAP do CT $VMID esta disponivel para rollback manual."
exit 2
