# Changelog

Uma linha por versao entregue, no mesmo commit do bump. A entrada diz o **efeito**, nao o diff.

## Nao lancado

- **T-211 — The CI is flaky on a wall-clock test** — `TestHandlerRespectsTheInstanceTimeoutMs`
  media agora a PASSAGEM do valor, nao a duracao: um `http.RoundTripper` falso que nunca toca a
  rede captura `req.Context().Deadline()` e confere que ela cai na janela `[antes+50ms,
  depois+50ms]`. A primeira tentativa (ler o deadline no `r.Context()` de um servidor mock real)
  nao funciona — `context.WithTimeout` e' um valor local do cliente, nunca vai para a rede — e
  travou (`go test -c` + `-test.timeout=10s` mostrou o goroutine do servidor preso para sempre em
  `<-r.Context().Done()`). Verde 20/20 em ~2.5s, sem sono e sem corrida de relogio. Achados dois
  irmaos do mesmo risco (medidos, nao consertados — fora do escopo desta tarefa):
  `TestWaitWithContextStopsEarlyIfTheContextIsCancelled`
  (`internal/outbound/templates_handler_test.go:1362`, margem de 135ms) e
  `TestStateRouteDoesNotHangWithTheExternalProbeStuck`
  (`internal/outbound/sonda_externa_test.go:368`, margem de 500ms). _Completed 2026-08-31 14:50._

## v0.63.0 — 2026-08-31

- **O contrato passa a falar ingles — leitor tolerante do lado deles, apelido so na ENTRADA** (T-189)
  — a migracao inteira do contrato, em quatro passos e sem uma mensagem perdida: leitores tolerantes
  do lado do consumidor, o gateway aceitando os dois idiomas na entrada (chave e valor), os
  escritores do consumidor em ingles, e a virada da saida. **Provada contra producao pelo
  consumidor:** `"observed"` chega e vira `"observado"` nos 7 pontos de leitura dele **sem uma linha
  de codigo dele ter mudado**, 80 templates lidos, zero evento preso. Ficam de fora, nomeados: o
  apelido de entrada (que continua no ar, e cuja remocao e decisao do dono) e os 18 nomes de
  contador (sem par decidido — o doc que dizia o contrario era falso). _Completed 2026-08-31 14:16._

- **T-210 — The output sweep is BLIND to the webhook event — fix it before v0.63.0 ships** — root
  cause found: `TestOutputContractHasNoPortugueseKeyOrValue` checks `forbiddenOutputTokens` with a
  flat substring search over the whole marshaled blob, which has no notion of WHERE a key sits; to
  avoid a false positive on `AccountAlert.Type` (legitimately tagged `tipo,omitempty`, left alone
  by T-209 on purpose), the word `"tipo"` was excluded from the list ENTIRELY — which also waived
  `Event.Type`'s own top-level key, the field the table actually renamed (`kind`, row 81). Fix:
  `walkForbiddenKeys` parses each instance's JSON and walks it with a full parent path, so `"tipo"`
  is now checked structurally against `forbiddenKeyExceptions` (a single path-scoped waiver at
  `account_alert.tipo`) instead of being banned everywhere or nowhere.
  **Verify — four mutations, each reproved and reverted:** (a) `internal/meta/types.go:413`
  `Event.Type` `json:"kind"` -> `json:"tipo"` (the exact positive control that found the bug) now
  fails with `a chave "tipo" aparece em tipo — regressao do Event.Type…`, once per event type
  (6 failures); (b) `NumberQuality.State` (nested inside `Event.number_quality`) `state` ->
  `estado` fails with the pre-existing flat check; (c) `EventTypeStatus` value `"status"` ->
  `"estado"` fails the same way; (d) `healthResponse.DisplayNumber` (an HTTP response key, no
  event involved) `display_number` -> `numero_exibido` still fails, proving the fix didn't regress
  response-body coverage. `go test -count=1 ./...` green with the tree fully restored.
  Not a new finding, but independently reconfirmed by probing the marshaled output: the Portuguese
  leftovers T-209's own changelog entry (above) already flagged and left untouched on purpose
  (`AccountAlert.tipo/severidade/id_da_entidade/descricao`, `Billing.cobravel`,
  `status_do_recurso`, `saude_handler.go`'s `verificado_em`, …) really do appear in the marshaled
  JSON, exactly as that entry says — they remain OUT of scope for this task (instrument, not
  contract) and are the planner's call, not re-litigated here.
  _Completed 2026-08-31 14:09._

🔴 **THE CONTRACT'S OUTPUT CHANGES: `zapgw` now speaks English on every SAIDA-EVENTO key/value it
delivers to a consumer's `callback_url`, and on every SAIDA-RESPOSTA key/value it returns from an
HTTP route.** Input keeps accepting BOTH languages, unchanged since T-203/T-207/T-208 — this is
step 4 of T-189, and step 5 (removing the input alias) is NOT part of this task and stays a
future, dono-authorized decision.

- **THE FLIP: the gateway's output speaks English — keys and values, in one commit** (T-209) —
  renamed every SAIDA-EVENTO/SAIDA-RESPOSTA key and value the migration table
  (`docs/MIGRACAO-CONTRATO-EN.md`, sections 6–8) lists, and only those: `meta.Event` and its
  nested types (`Reaction`, `Location`, `StatusError`, `TemplateStatus`, `TemplateCategory`,
  `NumberQuality`, `AccountAlert`, `Billing`), `meta.Template`, and every response struct in
  `internal/outbound` (`State` and its whole tree, `RegistrationResponse`, `blockOperationResponse`/
  `blockListResponse`, `templatesResponse`/`templateCreatedResponse`/`templateDeletedResponse`,
  `healthResponse`, `SmokeResponse`, `profileResponse`/`profileWriteResponse`, `PauseResponse`,
  the shared `errorResponse`). Values: the `meta.EventType`/`meta.ErrorClass` constants, the
  observation-state vocabulary (`CertNeverObserved`/`CertObserved`/`NotApplicable`/
  `ConnectorNotConfigured`/`ReachStateCouldNotVerify`), the three `veredito` vocabularies
  (`VerdictRefused`, `VerdictIGTokenWaiting`/`Failing`/`Expired`), the ~50 `respondError` call
  sites that passed `"retentavel"`/`"permanente"` as literals, and `config.CounterOldNameUsed`
  (`nome_antigo_usado` -> `old_name_used`).
  🔴 **The table is the only source: what it does not list did not change.** Confirmed by hand
  against `docs/contrato-chaves-que-nao-mudam.txt` (the 23 keys already in English) and flagged,
  never touched, for every Portuguese-looking field the table is silent on — `AccountAlert`'s
  `tipo`/`severidade`/`id_da_entidade`/`descricao`, `Billing.cobravel`,
  `TemplateStatus.status_do_recurso`, `FieldInRegistration`'s `campo`/`cadastrado`,
  `WindowInRegistration`'s `primeira_insercao_em`/`fecha_em`, `RegistrationResponse.proximo_passo`,
  `blockItemResponse`/`blockFailureResponse.telefone`, `blockOperationResponse.operacao`,
  `blockListResponse`'s `cursor_antes`/`cursor_depois`, `SmokeResponse`'s `ja_estava_ativa`/
  `ativa_desde`, `saude_handler.go`'s `verificado_em`, `State`'s `lideranca`/`hoje`/`definido_em`,
  `templateDeletedResponse`'s `entradas`/`aviso`/`releituras`/`espera_segundos`,
  `LeadershipInState`'s `armada`/`titular`, `entrada.go`'s `ViaTunnel`/`ViaPortForwarding` values
  (`tunel`/`encaminhamento_de_porta`), `leituras_handler.go`'s `wamid`/`digitando`, and every
  counter name besides `old_name_used` (`recebidas`, `entregues`, `enviadas`, the whole
  `cobranca_*` family, …) — none of these appear in the migration table, so none of them moved.
  🔴 **`cru`/`raw` and the byte-exact content are two different things, and only the KEY moved:**
  `cru` -> `raw` per the table's own row (SAIDA-EVENTO), but the base64 VALUE it carries is still
  the untouched exact bytes from Meta — `TestDeliverSendsTheRawAndTheEventsTogether` proves this
  byte-for-byte via `received.Raw` (a Go field, blind to the JSON tag) and needed no edit.
  **Verify (the gate, not a sample):** `TestOutputContractHasNoPortugueseKeyOrValue`
  (`internal/outbound/contrato_ingles_test.go`) reflectively fills one maximal instance of every
  SAIDA-EVENTO event type and every SAIDA-RESPOSTA body, marshals them, and fails on any of the
  108 Portuguese tokens this task retired — proven against real data by breaking `State.State`'s
  tag back to `estado` and watching it fail, then reverting.
  `TestFrozenKeysStayIdenticalInSource` sweeps `internal/meta`/`internal/outbound` for every key
  in `docs/contrato-chaves-que-nao-mudam.txt` and fails if one goes missing — proven the same way,
  against `profile_picture_handle`. The 36 `TestEntrada*` tests (T-203/T-207/T-208) passed
  UNCHANGED, zero edits to `entrada_apelidos.go`'s dictionaries or `entrada_apelidos_test.go`.
  `entrada_test.go` (T-120's ingress-health block, a same-named but unrelated file) DID change —
  its SAIDA-RESPOSTA mirror structs, not the entrada mechanism.

## v0.62.1 — 2026-08-31

- **Teach the counter to see the thirteen keys it was blind to** (T-208) — `nome_antigo_usado`
  only counted a key that had a PUBLISHED pair; `consumer-b` proved the blind spot against
  production by sending `titulo` (inside `botoes[]`) and the counter never moved. Published the
  pair AND wired the counter for the 13 rows `docs/MIGRACAO-CONTRATO-EN.md` section 9 names:
  4 body/multipart keys (`titulo`->`title` inside each `botoes[]` item — NOT `botao_titulo`,
  already aliased; `indice`->`index` inside each `botoes_template[]` item; `telefones`->`phones`,
  body of `POST/DELETE /v1/bloqueios`, which no longer shares `instanceOnlyAlias` with
  pausa/leituras/fumaca; `arquivo`->`file`, the multipart FIELD NAME of `POST /v1/media` — not a
  `json:"…"` tag at all, so it goes through a separate mechanism, `filePart`, not
  `translateAliasesInPlace`) and 9 `ENTRADA-QUERY` call sites across 6 routes
  (`instancia`->`instance` on `GET /v1/media/{id}` + `POST /v1/media` (shared), `GET /v1/estado`,
  `GET /v1/bloqueios`, `GET /v1/perfil`, `GET /v1/templates`, `DELETE /v1/templates`;
  `mime_do_payload`->`payload_mime` on `GET /v1/media/{id}`; `serie_dias`->`series_days` on
  `GET /v1/estado`; `nome`->`name` on `DELETE /v1/templates` — new `queryAlias`/`queryAliasRaw`
  helpers in `entrada_apelidos.go`, the same "novo or velho" principle as the body but a
  DIFFERENT point in the code, since a query parameter is never a JSON key). `MediaHandler`,
  `StateHandler` and `ProfileHandler` gained a POSITIONAL AND MANDATORY `counter *config.Counter`
  (same discipline T-205 used for bloqueio/cadastro/pausa) — none of the three had one before,
  because none of their ENTRADA points were JSON keys. One `Record` call per REQUEST, combining
  every old name that request carried, never one per key (media's `instanceAuthorized` returns its
  flag instead of recording, so `upload`/`download` can combine it with their own second flag).
  14 new tests, one per key plus the exact control: a request with every key in English except
  `titulo` inside `botoes[]` now moves the counter by exactly +1
  (`TestEntradaConsumerScenarioTitleInPortugueseMovesTheCounter`) — before this task it did not
  move at all. Output is untouched. Two existing test helpers
  (`askStateWithWindow`/`cmd/zapgw/estado_test.go`'s state-route test) switched their OWN query
  spelling from `instancia`/`serie_dias` to `instance`/`series_days`, since `GET /v1/estado` now
  self-counts on the old spelling and those helpers back nearly every state-reading test in the
  suite, unrelated to this migration. `CGO_ENABLED=0 go build ./...`, `go test ./...`,
  `go vet ./...`, `gofmt -l cmd internal` clean. _Completed 2026-08-31 13:09._

## v0.62.0 — 2026-08-31

- **Step 2 of the ENTRADA migration, for VALUES this time: the gateway accepts the English value on
  input too** (T-207) — the three ENTRADA value vocabularies of
  `docs/MIGRACAO-CONTRATO-EN.md` section 8 (8.1 `Request.Type`, 11 values; 8.3
  `TemplateButtonUnion.Type` inside `botoes_template[]`, 2 values; 8.5 `Request.Category`, 5
  values — 18 in total) now also accept their English spelling on input, translated to the
  canonical Portuguese value BEFORE `json.Unmarshal` and BEFORE `RequestHash`, same ordering
  requirement and same reason as the key alias: hashing before translation would make
  `{"tipo":"texto"}` and `{"tipo":"text"}` — the same request — hash differently, and the same
  message would go out twice to the customer
  (`TestEntradaValueIdempotencyCrossesLanguages`, green). Output is untouched: the eight SAIDA
  value vocabularies (8.2, 8.6, 8.7, 8.8-8.10, 8.11) were not touched. `tipo` is FOUR
  vocabularies sharing one JSON key — `requestTypeValueAlias`, `templateButtonTypeValueAlias` and
  `requestCategoryValueAlias` are three SEPARATE dicts, each scoped to the one object section 8
  names for it, proved by `TestEntradaValueAliasIsScopedPerObject` (a valid top-level value used
  inside `botoes_template`, and a valid button value used at the top level, both stay `400`). A
  value has no conflict case — no `ErrConflictingAlias` equivalent was added. An invented value
  keeps being refused with today's exact message (`TestEntradaInventedValueStillRejected`).
  `config.CounterOldNameUsed` now also counts an old VALUE, not only an old KEY — a request whose
  key is already English but whose value is still Portuguese counts too, the exact scenario the
  task's own Why names; the counter stays a SINGLE one, and an old-value marker is formatted
  `"valor:<field>=<value>"` (an old-key marker stays the bare field name) so the two are
  distinguishable without a second counter. 11 of the 18 values needed an alias (`template`,
  `cta_url`, `flow`, `url`, `video`, `audio`, `sticker` are the 7 that are the same word in both
  languages already). `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`,
  `gofmt -l cmd internal` clean. _Completed 2026-08-31 11:44._
- **The migration table is INCOMPLETE for step 4 — inventory what a `json:` sweep cannot see**
  (T-206) — new `docs/INVENTARIO-VALORES.md`: 11 closed value-vocabularies (`tipo` alone is FOUR
  separate vocabularies under the same JSON key, 11+6+2+2 values — the `Why`'s six examples covered
  only one of them), the 4 ENTRADA keys missing from the existing tables (`titulo`, `indice`,
  `telefones`, and `arquivo` — a multipart field name, not a JSON tag), a new `ENTRADA-QUERY`
  direction (13 query-param call sites, 9 distinct names, 6 routes — more than the 3 routes
  measured before), and confirmation that all 19 counters in the code (including
  `nome_antigo_usado`) are accounted for. No English name decided — every row is `A DECIDIR`.
  _Completed 2026-08-31 11:12._

## v0.61.1 — 2026-08-31

- **The old-name counter now covers every route that accepts an alias, and a structural guard makes
  the omission fail the build's own test suite** (T-205) — T-203 wired
  `config.CounterOldNameUsed` on 4 of the 7 alias-accepting routes (send, templates, leituras,
  fumaca) and left `POST /v1/cadastro`, `POST /v1/pausa` and `POST/DELETE /v1/bloqueios` accepting
  the English alias WITHOUT counting it. All three now record the counter the same way the other
  four already did. `counter *config.Counter` is a POSITIONAL, MANDATORY constructor parameter on
  `NewRegistrationHandler`, `NewPauseHandler` and `NewBlockHandler` (same discipline as
  `AcceptedTypes`, T-111) — proved not to compile without it (removed the argument from
  `cmd/zapgw/main.go`, `CGO_ENABLED=0 go build ./...` failed naming the missing parameter, restored).
  The heart of the task is the STRUCTURAL GUARD (`TestOldNameCounterGuardCoversEveryAliasRoute`,
  `internal/outbound/entrada_apelidos_test.go`): it does not enumerate the 7 routes by hand — it
  walks the package's AST for every call site of `translateEntradaOrReject` and requires the
  enclosing function to both capture `oldNames` and reference `config.CounterOldNameUsed`, so a
  route born tomorrow that forgets the counter fails this test by name, with zero edits to the
  guard. Proved against REAL production code, not only a synthetic fixture: temporarily reverted
  `pausa_handler.go`'s wiring back to discarding `oldNames`, the guard failed citing
  `pauseRoute (chamada em pausa_handler.go:101:23)`, then the wiring was restored and the guard went
  green again. `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`,
  `gofmt -l cmd internal` clean. _Completed 2026-08-31 10:54._
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
