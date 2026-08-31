# Changelog

Uma linha por versao entregue, no mesmo commit do bump. A entrada diz o **efeito**, nao o diff.

## Nao lancado

- **Settle whether the instance-type gate exists, and make the row say what is true** (T-197) — o
  mecanismo EXISTE: `internal/outbound/tipos.go` (`AcceptedTypes`, T-111), parametro posicional
  obrigatorio no ultimo lugar de todo construtor `outbound.New*Handler`. Provado removendo
  `outbound.WhatsAppOnly` da chamada de `NewReadsHandler` em `cmd/zapgw/main.go:430` e rodando
  `go build ./...`: `not enough arguments in call to outbound.NewReadsHandler`; restaurado, `git
  diff` vazio. A varredura anterior (T-196) tinha olhado no lugar errado. `CLAUDE.md` e
  `CLAUDE.pt-BR.md` corrigidos juntos, com ponteiro e evidencia. _Completed 2026-08-31 01:07._
- **The PT-BR pair of CLAUDE.md stops describing a repository that no longer exists** (T-196) —
  o par em portugues voltou a bater com o `CLAUDE.md`: a tabela de regras duras (os dois portoes de
  dado pessoal, TLS, isolamento de rota), a secao das tres fundacoes reescrita, a dos portoes
  passando de dois para tres, e o "Estado hoje" que ainda dizia *"so o scaffold, sem codigo, e ainda
  privado"*. **Tres afirmacoes falsas foram achadas TAMBEM no lado ingles** ao conferir contra o
  codigo — tres portoes marcados como "existe no zapgw-dev, migra com o codigo" depois de o codigo
  ter migrado. Dois deles foram localizados aqui e ganharam ponteiro; o terceiro nao foi encontrado
  e virou a T-197 em vez de virar afirmacao. _Completed 2026-08-31 01:03._

- **CI carries the name gate, and says so when it cannot verify** (T-195) — `ZAPGW_FORBIDDEN_NAMES`
  entregue como `env:` no nivel do JOB em `.github/workflows/verify.yml`, alcancando tanto o
  `go test ./...` quanto um passo proprio novo (`-run TestNoCustomerNameOutsideTheGateInTheRepo`),
  espelhando o portao de telefone. Comentario no workflow documenta que PR de fork falha fechado
  ("nao consegui verificar") de proposito. `CLAUDE.md` corrigido: a CI ja existe, nao "volta em
  2026-09-01". _Completed 2026-08-31 00:50._
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
