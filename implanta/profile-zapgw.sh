# /etc/profile.d/zapgw.sh — faz `zapgw` funcionar tambem para quem entra por `pct`.
# Ver docs/ARMADILHAS.md, "`command not found` dentro do CT nao quer dizer que o
# binario nao esta la". Instalado em 2026-07-29.
#
# O PROBLEMA QUE ELE RESOLVE, e sao dois com sintomas diferentes:
#   1. `pct enter` / `pct exec` dao PATH=/sbin:/bin:/usr/sbin:/usr/bin — sem
#      /usr/local/bin, que e onde o deploy.sh instala o binario. Sintoma:
#      `command not found`, que manda procurar deploy quebrado.
#   2. shell interativo nao herda o env do systemd, entao faltam ZAPGW_BANCO e
#      ZAPGW_CHAVE_CIFRA. Sintoma: o menu abre e diz `resumo indisponivel:`,
#      e todo subcomando falha ao abrir o banco.
#
# POR QUE UMA FUNCAO, e nao so um PATH: so o PATH consertaria (1) e deixaria (2)
# de pe — trocar um erro claro por um erro obscuro e pior que nao consertar.
#
# POR QUE O CORPO E `( ... )` E NAO `{ ... }`: a subshell mantem o env — inclusive
# ZAPGW_CHAVE_CIFRA — vivo so durante a chamada. Com chaves, a chave de cifra
# ficaria no ambiente do shell e seria herdada por todo processo iniciado dali,
# legivel em /proc/<pid>/environ. Uma conveniencia nao paga com o segredo.
#
# `zapgw` sozinho, num terminal, abre o menu — a funcao preserva stdin/stdout.
zapgw() (
	set -a
	. /etc/zapgw/env
	set +a
	exec /usr/local/bin/zapgw "$@"
)
