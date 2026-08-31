# Changelog

Uma linha por versao entregue, no mesmo commit do bump. A entrada diz o **efeito**, nao o diff.

## Nao lancado

- **A gate for customer names, and the tree it cleans** (T-193) — novo portao
  (`internal/config/nomes_allowlist_test.go`) sem allowlist e sem isencao por arquivo: qualquer
  agulha e reprovacao. A lista mora fora do repositorio (`ZAPGW_FORBIDDEN_NAMES` ou
  `~/.zapgw/forbidden-names.txt`); sem uma das duas, falha dizendo que nao conseguiu verificar,
  nunca verde. Reprovou contra a arvore suja (25 ocorrencias, 6 arquivos) antes da limpeza; depois
  da limpeza, `go test ./...` inteiro `ok`. `CLAUDE.md` e `docs/ARMADILHAS.md` (+ par pt-BR)
  atualizados. _Completed 2026-08-31 00:37._
- **A real customer name in a test argument becomes a synthetic one** (T-192) — trocado o valor de
  `cmd/zapgw/provisionar_test.go:2392`; varredura da agulha na arvore inteira (rastreados e
  nao-rastreados-nao-ignorados) nao achou mais nenhuma ocorrencia. _Completed 2026-08-31 00:15._
- **The personal-data gate sweeps the whole repository, minus a declared exclusion** (T-191) — o
  portao de telefone trocou a lista fixa de diretorios (`scannedTargets`) por `filesGitSeesFromRoot`:
  `git ls-files` + `git ls-files --others --exclude-standard`, o mesmo conjunto que um `git add -A`
  levaria. Controle positivo na raiz reprovou contra dado real; controle negativo (`*.local.md`)
  confirmou que o arquivo do canal continua fora da varredura. _Completed 2026-08-31 00:21._

## v0.60.1 — 2026-08-30

**O primeiro release deste repositorio, e o primeiro que existe fora do privado.** Nao ha mudanca de
comportamento em relacao a `v0.60.0`, que esta em producao desde 2026-08-29: o contrato com os
consumidores e' byte a byte o mesmo — 322 tags `json` e 2.081 literais de string de producao
identicos, medidos, nao afirmados. **Por isso PATCH e nao MINOR:** SemVer fala do contrato, e o
contrato nao se moveu, por maior que tenha sido a mudanca por dentro.

O que esta versao carrega:

- **O codigo inteiro em ingles.** 3.818 declaracoes em sete pacotes; sobra uma palavra portuguesa,
  num nome de teste que cita o nome de uma parte multipart **no fio** — o teste nomeia o contrato
  que guarda.
- **O portao de dado pessoal, com a lista de isencoes VAZIA.** Ele varre `cmd/`, `internal/`,
  `testdata/`, `docs/` e o `README.md`, decodificando o base64 de todo `wamid.` — porque o `wamid`
  carrega o telefone do destinatario dentro dele, e um `grep` pelo numero como um humano o escreve
  passa limpo por cima.
- **Texto de diagnostico que descreve comportamento em vez de nomear constante interna** — o
  ponteiro que nenhum compilador confere.

**Binarios:** `zapgw-linux-amd64` e `zapgw-linux-arm64`, os dois estaticos (`CGO_ENABLED=0`).
🔴 **O amd64 foi EXECUTADO** num host Linux real (`x86_64`): respondeu `0.60.1` e `ldd` devolveu
*"not a dynamic executable"*. **O arm64 apenas compilou e NAO foi executado em lugar nenhum** —
*compilar nao e' rodar*, e por isso ele vai marcado como nao testado em vez de anunciado como
suportado.
