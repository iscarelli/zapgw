# Changelog

Uma linha por versao entregue, no mesmo commit do bump. A entrada diz o **efeito**, nao o diff.

## Nao lancado

- **The pre-push gate must not refuse a TAG that points at an already-pushed commit** (T-204) — an
  empty pushed interval is no longer an automatic `t.Fatalf`: `objectAlreadyReachableFromRemotes`
  checks whether the pushed object (peeled past any tag) is already an ancestor of some
  remote-tracking ref, and a legitimate zero (a release tag on a commit already merged to `main`,
  the ordinary flow) now logs and continues instead of blocking. A genuinely unmeasured zero still
  fails closed exactly as before. Also new: the annotated tag object's own free-text MESSAGE is now
  swept unconditionally (`isAnnotatedTagObject` / `annotatedTagMessage` / `sweepTagMessage`, header
  lines stripped so a real `tagger Name <email>` never false-positives the name gate), because that
  text reaches `origin` on a tag push whether or not the tag carries any new commit. Proved against
  real data both ways in the same session: `v0.61.0` (which existed locally, unpushed, exactly
  because the old gate refused it) pushed clean to a disposable bare repo and then to `origin`; a
  second annotated tag whose message alone carried a needle was BLOCKED citing the tag message, on
  the same disposable remote. T-200/T-201's own tests
  (`TestPrePushGateNewRefCleanBranchPasses`/`...NewRefBlocksNeedleDeletedLater`/
  `...NewRefNoRemoteAtAllSweepsEverything`/`...CleanMergeOnMainPasses`/
  `...BlocksNeedleOnlyInMergeResolution`) stayed green, untouched. `CGO_ENABLED=0 go build ./...`,
  `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` clean; `--no-verify` never used.
  _Completed 2026-08-31 09:41._

## v0.61.0 — 2026-08-31

- **The gateway accepts English key names on ENTRADA input, and counts the old ones** (T-203, passo
  2 de 4 da T-189) — as 30 chaves de direcao ENTRADA de `docs/MIGRACAO-CONTRATO-EN.md` ganharam
  apelido em ingles, POR POSICAO (`internal/outbound/entrada_apelidos.go`), nas 7 rotas que
  decodificam corpo: `POST /v1/messages` (com descida nos 4 objetos aninhados —
  `cabecalho`/`reacao`/`localizacao`/`fluxo` — e em cada item de `botoes_template`),
  `POST /v1/templates`, `POST /v1/cadastro`, `POST /v1/pausa`, `POST/DELETE /v1/bloqueios`,
  `POST /v1/leituras`, `POST /v1/fumaca`. A saida NAO muda. A traducao roda ANTES do
  `json.Unmarshal` e ANTES do `RequestHash`, entao o hash de idempotencia ve a forma CANONICA —
  provado com um teste que manda o MESMO pedido em PT e em EN sob a MESMA `Idempotency-Key` e exige
  UM envio a Meta com o MESMO `wa_message_id` (`TestEntradaIdempotencyCrossesLanguages`). Os dois
  nomes juntos no mesmo pedido viram `400` nomeando as duas chaves — testado chave por chave, nao
  por amostra, nas 30 linhas ENTRADA (incluindo os 4 objetos aninhados e o item de
  `botoes_template`). Nenhuma chave de `docs/contrato-chaves-que-nao-mudam.txt` ganhou apelido, e
  nenhuma chave fora da tabela foi inventada. Contador `config.CounterOldNameUsed`
  (`nome_antigo_usado`) sobe por instancia quando o pedido ainda usa a grafia velha e aparece em
  `GET /v1/estado` — e' o numero que vai autorizar o passo 4. ⚠️ **Nao esta em toda rota:**
  `/v1/cadastro`, `/v1/pausa` e `POST/DELETE /v1/bloqueios` aceitam o apelido em ingles mas ainda
  NAO tem `*config.Counter` plugado (mudanca estrutural maior, fora do escopo desta tarefa) — nelas
  o apelido funciona e o contador fica de fora por enquanto. Verify: `CGO_ENABLED=0 go build ./...`,
  `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` limpos; suite inteira (`internal/inbound`
  incluido, onde vive o teste do `cru` byte a byte) verde sem alteracao nenhuma la.
  _Completed 2026-08-31 09:17._
- **The migration table becomes a versioned document, with DIRECTION and what does NOT change**
  (T-202) — `docs/MIGRACAO-CONTRATO-EN.md` (par `docs/MIGRACAO-CONTRATO-EN.pt-BR.md`) nasceram
  juntos, montando 119 linhas de chave a partir de duas fontes ja existentes: 90 pares ja propostos
  ao `consumer-b` no canal privado (Fonte A — medido, nao os 89 estimados na spec: sem duplicata,
  contagem batida com `sed -n` linha a linha), mais 29 chaves medidas contra o codigo sem par
  decidido (Fonte B, `docs/INVENTARIO-CHAVES.md`, inglês = `A DECIDIR`), sem sobreposicao entre as
  duas. **Toda linha tem direcao** (`SAIDA-EVENTO`/`SAIDA-RESPOSTA`/`ENTRADA`, `A MEDIR` quando
  medido e inconclusivo) — 21 chaves sao multi-direcao (14 na Tabela A, 7 na B), e 6 da Tabela A sao
  `A MEDIR` porque a string proposta nao corresponde a nenhum campo real do contrato hoje (3 so
  existem num vetor de teste interno, uma e' o envelope de paginacao da propria Meta, uma e' flag de
  CLI, uma nao existe). Secao "O que NAO muda" trouxe as 23 linhas ja-em-ingles do item 4 do
  inventario, com a regra de colisao do `consumer-b` creditada. Nenhum nome foi decidido pela
  tarefa. Todos os 119 ponteiros `arquivo:linha` foram conferidos mecanicamente contra o codigo
  (script Python de verificacao, nao so amostragem); um erro de arquivo achado nessa conferencia
  (`conector` apontava para `estado.go` em vez de `entrada.go`) foi corrigido antes do commit.
  Zero dado identificavel copiado do canal privado (so as linhas de tabela chave/valor). Verify:
  `CGO_ENABLED=0 go build ./...`, `go test ./...` (os dois portoes de dado pessoal varreram `docs/`
  e passaram), `go vet ./...`, `gofmt -l cmd internal` limpos. _Completed 2026-08-31 08:18._
- **The pre-push gate looks inside merge commits too** (T-201) — `filesChangedInCommit`
  (`internal/config/prepush_test.go`) passou a decidir pelo numero de pais do commit: 0/1 pai
  mantem o diff de sempre (com `--root` para o genesis), 2+ pais (merge) muda para
  `git diff-tree -c`. Escolhido com numero na mao, nao por suposicao: contra um merge real
  construido num clone descartavel deste repositorio (nunca `origin`), um merge LIMPO mediu **12
  arquivos com `-m`** (reinspeciona tudo que as duas branches ja tinham, redundante com o que a
  varredura por commit ja olhou) contra **0 com `-c`** (nada de novo — tudo bate trivialmente com
  um dos pais); um merge com CONFLITO DE VERDADE (agulha so' existe na resolucao, em nenhum dos
  dois pais) mediu **1 arquivo em ambos** — `-c` acha tudo que `-m` acha, ao custo de zero
  redundancia no caso limpo. Prova de que `-c` nao esconde nada: um arquivo que ele omite bate
  com um pai cujo commit ja esta na lista varrida (ou ja estava publico antes deste push), entao
  o conteudo ja passou por um olhar — `-c` so' remove o SEGUNDO olhar redundante, nunca o unico.
  Dois testes novos contra repo descartavel: `TestPrePushGateBlocksNeedleOnlyInMergeResolution`
  (agulha so' na resolucao de um conflito real; bloqueio nomeia o commit de MERGE e o arquivo, nao
  "nao consegui verificar") e `TestPrePushGateCleanMergeOnMainPasses` (merge limpo passa, sweep em
  ~160ms). Entrada em `docs/ARMADILHAS.md` (par pt-BR): o comentario que declarava o buraco em
  `filesChangedInCommit` foi atualizado — descrevia limitacao ja consertada, doc falso. Verify:
  `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` limpos.
  _Completed 2026-08-31 07:55._
- **The pre-push gate must not make the legitimate path impossible** (T-200) — o primeiro push de
  QUALQUER ref nova (sha remoto = zeros) parava de ser recusado de saida e passou a ter o intervalo
  calculado por `git rev-list <sha-novo> --not --remotes` (`commitsForPushedInterval`, em
  `internal/config/prepush_test.go`) — exatamente "o que este push acrescenta ao `origin`", sem
  adivinhar merge-base; sem remoto nenhum a formula se reduz sozinha a varrer todos os commits
  alcancaveis (fallback seguro, sem codigo especial). Provado contra dado real, nao afirmado: push
  de branch nova e limpa passa; push de branch cujo commit A introduz uma agulha e o commit B apaga
  o arquivo de novo continua bloqueando, citando o commit A e o arquivo (nao "nao consegui
  verificar") — tres testes novos (`TestPrePushGateNewRefCleanBranchPasses`,
  `TestPrePushGateNewRefBlocksNeedleDeletedLater`, `TestPrePushGateNewRefNoRemoteAtAllSweepsEverything`)
  e um ensaio manual contra um repo bare descartavel. Entrada em `docs/ARMADILHAS.md` (par pt-BR),
  marcada com o fogo: falha fechada que torna o caminho legitimo impossivel nao protege, ensina o
  desvio — e o primeiro controle do planner tinha "passado" pelo motivo errado (sha zerado, nao
  agulha achada). Verify: `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l
  cmd internal` limpos. _Completed 2026-08-31 06:41._
- **A pre-push gate: nothing personal leaves this machine, not even in a commit that a later
  commit fixes** (T-199) — `.githooks/pre-push` (ativado por clone com `git config core.hooksPath
  .githooks`) roda `internal/config/prepush_test.go`'s `TestPrePushGate` para cada ref sendo
  empurrada: materializa o que CADA commit do intervalo introduziu (nao a arvore final) e reusa,
  sem duplicar, `sweepPhoneNumbersOutsideTheAllowlist` e `sweepForbiddenNamesOutsideTheGate` — as
  mesmas funcoes dos dois portoes de arvore ja existentes. Controle positivo de intervalo provado:
  dois commits descartaveis (um acrescenta um numero sintetico fora da allowlist — nao repetido
  aqui, ver o proprio portao de telefone para o porque de nao crescer a allowlist por um valor
  de controle descartado — o outro apaga o arquivo, arvore final limpa) tiveram o push BLOQUEADO,
  nomeando o commit e o arquivo (`CONTROLE-T199-AGULHA.md:1`). Controle de "nao consegui verificar" provado escondendo
  `~/.zapgw/forbidden-names.txt` (push bloqueado com a mensagem propria; arquivo devolvido, 17
  linhas confirmadas). Push legitimo mede ~1.7s. Falha fechada em todos os caminhos: `go` ausente,
  lista de agulhas ausente, intervalo incalculavel, e primeiro push de ref nova (sha remoto todo
  zeros, sem base segura para calcular o intervalo) bloqueiam, nunca liberam. Limite documentado no
  proprio codigo: um commit de MERGE mostra diff vazio para `git diff-tree` sem `-m`/`-c`, entao
  conteudo reintroduzido so' numa resolucao de merge nao e' inspecionado por este gate. Verify:
  `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` limpos.
  _Completed 2026-08-31 06:29._
- **Inventory every contract key the consumer reads, with its direction and file:line** (T-198) —
  `docs/INVENTARIO-CHAVES.md` criado: 47 pontos de emissao medidos para as 29 chaves pedidas (18
  chaves repetem por aparecerem em mais de uma direcao/rota), zero ausentes do codigo, zero ja em
  ingles entre as 29. A varredura inversa (item 4) achou 20+ chaves de SAIDA ja em ingles
  (`media_id`, `wa_id`, `id`, `status`, `ok`, os campos de `meta.Profile`/`ProfilePatch` etc.),
  incluindo a localizacao exata e corrigida do exemplo do `Why` — a resposta de `POST /v1/media`
  emite `media_id` em `internal/outbound/media_handler.go:260` (nao em `mensagem.go:179,626`, que
  sao usos de ENTRADA do mesmo nome). Nao decide nome nenhum, nao edita tabela de contrato. Verify:
  `go test ./...`, `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `gofmt -l cmd internal` limpos;
  amostra de 7 `arquivo:linha` conferida com `sed -n`. _Completed 2026-08-31 06:02._
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
