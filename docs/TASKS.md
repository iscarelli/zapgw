# Tasks

## Em voo — retomar por aqui

> Bloco de retomada. **Cada item sai daqui quando for resolvido**; ele nao e' historico.
> Escrito ao fim de 2026-08-30, o dia em que o repositorio virou publico. Bloco de retomada
> mentindo e' pior que bloco nenhum: e' o primeiro texto que a proxima sessao le.

### 📌 2026-09-15 03:40 — `v0.67.0` EM PRODUCAO: nomes velhos de env e verbos portugueses da CLI RECUSAM. Aguarda validacao do consumidor.

**Medido pelo deploy, nao afirmado:** `HEALTH OK: {"ok":true,"versao":"0.67.0"}` +
`VERSION MATCHES: 0.67.0 (same as built)` as ~03:31. `main` = `e205487`, tag `v0.67.0`, os dois no
`origin` (`git ls-remote --tags`). Verify de repo inteiro verde nos 7 pacotes em CADA merge.

- **Quatro tarefas fechadas num lote** (dono: "pode fazer uma de uma vez"): T-243 (docs/comentarios),
  T-244 (env velho -> RECUSA, `EnvOrOld`/`WarnOldEnvVar` apagadas; `ZAPGW_CHAVE_CIFRA=x ./zapgw`
  sai 1 nomeando `ZAPGW_ENCRYPTION_KEY`), T-220 (cinco verbos de topo), T-245 (`estado` + oito
  sub-verbos; `warnOldVerb` apagada). Provas re-rodadas no `main` mesclado, nao tiradas do relatorio.
- ✅ **`/root/rotaciona-token.sh` no CT 125 migrado as ~03:33** (`instancia listar` -> `instance list`,
  `instancia rotacionar` -> `instance rotate`), backup `/root/rotaciona-token.sh.antes-da-T-220`,
  `bash -n` ok, `zapgw instance list` respondendo pelo binario novo via a funcao do profile. Janela de
  quebra: ~2 min entre o swap e o sed. ⚠️ O script `~/.zapgw/migra-verbos-rotaciona.sh` primeiro
  FALHOU chamando `/usr/local/bin/zapgw` direto (sem o env): a conferencia certa e' pela funcao
  `zapgw` de `/etc/profile.d/zapgw.sh`, que carrega o env em subshell.
- ✅ **Validado pelo consumidor as 03:34** (`STATUS: PRONTO` no arquivo dele): `GET /v1/estado`
  com `version 0.67.0`, `POST /v1/uploads` `200` em 1,5 s com handle de 218 chars, texto `200` com
  `wamid`, template gravado como enviado — "diferencas em relacao a v0.66.1: nenhuma". Lote FECHADO.
  Dois residuos foram para a fila: T-240 ganhou a lista de chaves medida; T-246 (`transito`,
  `perdidas`, `versao` sem par ingles). O pedido, como foi feito:
  Secao de 03:32 no canal (`STATUS: AGUARDANDO_VALIDACAO`)
  pede: `GET /v1/estado` com `versao 0.67.0`, o fluxo do vale-presente sem mudar o script, um envio
  de texto e um de template, e qualquer diferenca vs a `v0.66.1`. `SendMessage` "va ler" enviado a
  sessao do consumidor (endereco esta no cabecalho do arquivo dele no canal). Nenhuma rota HTTP mudou nesta versao — a validacao e' de trafego real,
  que o verify nao alcanca. Quando responderem `PRONTO`, fechar o lote com o dono.
- 🧹 Quatro worktrees novas desta sessao (`agent-a12eac…`, `aeebd5…`, `a24372…`, `aa86d2…`) estao
  MESCLADAS; entram na limpeza das 21 anteriores.

### 📌 2026-09-15 01:10 — `v0.66.1` EM PRODUCAO e MEDIDA pelo consumidor: `POST /v1/uploads` fechado

**Atualiza o bloco logo abaixo (00:50), que ficou stale em 20 minutos:** a primeira chamada real do
consumidor na `v0.66.0` FALHOU (`400`, `step: "upload"`) — o id de sessao da Meta e'
`upload:…?sig=…` e o `url.JoinPath` escapava o `?`. **T-242** consertou (concatenacao + validacao
de forma; teste com o valor real reprovou antes e passou depois; entrada 🔥 em `ARMADILHAS.md`).
`v0.66.1` implantada as 01:06 (`HEALTH OK` + `VERSION MATCHES`), e as 01:07 o consumidor mediu:
`200` com handle, template `HEADER IMAGE` criado (`201`/`PENDING`). **A marca "nao medido" saiu
do contrato (`6c6733b`) citando essa medicao**, com a fronteira: um mime, um consumidor. O `GET
/app` com token de System User FUNCIONA — o plano B do App ID morreu. `main` = `6c6733b`, tags
`v0.66.0` e `v0.66.1` no `origin`. Canal `PRONTO` dos dois lados, nada pendente.

### 📌 2026-09-15 00:50 — `v0.66.0` EM PRODUCAO: a rota do consumidor subiu e levou a traducao junto

**Medido pelo proprio deploy, nao afirmado:** `HEALTH OK: {"ok":true,"versao":"0.66.0"}` seguido de
`VERSION MATCHES: 0.66.0 (same as built)`. Tag `v0.66.0` no `origin`, `main` = `37bc11b`, verify
de repo inteiro verde nos 7 pacotes antes do bump.

- **T-241 fechada** (`POST /v1/uploads`, o `example.header_handle` via Resumable Upload) — pedido
  do `consumer-b` em 2026-09-15, respondido no canal com formato, versao e o que falta medir.
  🔴 **Zero byte foi para a Meta real**: a rota foi provada contra Graph falso. O contrato marca a
  rota como *assumida / nao medida*, e o passo `GET /app?fields=id` (descoberta do App ID a partir
  do `token_envio`, porque a instancia NAO guarda App ID) em particular. **A medicao e' a primeira
  chamada do consumidor**, combinada no canal: `200` + template `PENDING` -> tirar a marca do
  contrato citando o arquivo deles; erro com `step: "app_id"` -> plano B ja decidido: aceitar o App
  ID vindo do consumidor (identificador, nao segredo). Nao corre sozinho — espera o canal.
- **As 17 tarefas da traducao (T-222..T-239) subiram nesta versao.** O aviso de "main NAO esta
  implantado" do bloco anterior morreu aqui.
- **A unit do systemd do CT 125 NAO cita `implanta/`** — medido em 2026-09-15 00:20 por
  `systemctl cat zapgw`: so' `Documentation=` (URL do GitHub) e `ExecStart=/usr/local/bin/zapgw`.
  A pergunta "ABERTA" do bloco anterior fecha.
- ✅ **As seis `ZAPGW_*` obsoletas do CT foram renomeadas em 2026-09-15 01:32** ([1468] fechado):
  so' o NOME, valor intacto (317 -> 324 bytes = soma das diferencas de nome), backup
  `/etc/zapgw/env.antes-do-rename-20260915`, saude ok na `v0.66.1`, **zero** avisos `deprecated`.
  Rodado pelo dono via `~/.zapgw/rename-env-obsoleto.sh` (o classificador barra escrita remota
  nesse arquivo). A T-243 limpou os comentarios/docs que ainda citavam os nomes velhos como vivos,
  e a T-244 fechou o item 4 da T-214: os nomes velhos nao sao mais lidos — se estiverem
  definidos, o processo RECUSA subir, nomeando o novo.
- 🧹 **21 worktrees de implementador acumuladas em `.claude/worktrees/`** (medido por
  `git worktree list` em 2026-09-15). So' uma tem trabalho nao mesclado conhecido: a da T-231
  (`agent-af905375f3c5ebcb1`, commit `8d3fd43`, ver abaixo). As outras sao lixo de tarefas ja
  aposentadas — `git worktree remove` uma a uma, conferindo `git log main..<branch>` vazio antes.

#### 🔴 A FILA agora — DUAS tarefas, nesta ordem: T-240, T-231 (detalhe no bloco de 09-08 abaixo).
T-220 e T-245 (mescladas e implantadas na `v0.67.0`, bloco de 03:40) sairam da fila.

### 📌 2026-09-08 06:35 — FIM DA SESSAO DA TRADUCAO. Leia este bloco inteiro antes de tocar em nada.

**A sessao parou por orcamento do dono, nao por problema.** `main` = `a0b0d52`, empurrado, e o
**verify de repo inteiro estava VERDE nos 7 pacotes no ultimo merge** — medido, nao afirmado.

**17 tarefas aposentadas nesta sessao:** T-222, T-223, T-224, T-225, T-226, T-227, T-228, T-229,
T-230, T-232, T-233, T-234, T-235, T-236, T-237, T-238, T-239. Todas com entrada no
`docs/CHANGELOG.md`, sob `## Unreleased`.

#### O que foi medido, com a mesma varredura antes e depois

- **Codigo Go: 5.023 -> 2.262 linhas** carregando palavra portuguesa (−55%). O resto e' quase todo
  deliberado: tag `json:` de contrato, vocabulario que o consumidor enxerga, prefixo `ALARME`.
- `testdata/corpus/README.md`: 378 -> 2. `docs/CHANGELOG.md`: 220 -> 51. `deploy/deploy.sh`: 162 -> 55.
- **ZERO nome de arquivo ou diretorio em portugues.** `implanta/` -> `deploy/`,
  `cmd/grafo-falso/` -> `cmd/fakegraph/`, `valida-lideranca.sh` -> `check-leadership.sh`, 40 fixtures.
- **Espelho pt-BR cortado de 11 para 4 docs** (−6.113 linhas). Criterio no `CLAUDE.md`.

#### 🔴 A FILA — TRES tarefas, nesta ordem

1. **T-240** — a quinta familia do contrato do consumidor. **Nao chegou a escrever nada**; a worktree
   dela ja foi limpa. Comece do zero.
2. **T-231** — o lado ingles dos docs. ⚠️ **TEM TRABALHO PARCIAL SALVO, NAO REVISADO:** commit
   `8d3fd43` na branch `worktree-agent-af905375f3c5ebcb1`, um arquivo
   (`docs/INVENTARIO-STRINGS.md`) traduzido pela metade. **Nao mescle sem revisar.** O agente tinha
   reportado que `docs/ARMADILHAS.md` ja esta integralmente traduzido (o portugues que sobra la e'
   literal legitimo) — **isso e' afirmacao dele, nao medicao minha; confira.**
3. **T-220** — os verbos da CLI. E' a unica que termina em PRODUCAO, e a ordem e' a garantia:
   mesclar -> deployar -> atualizar `/root/rotaciona-token.sh` no CT 125, no mesmo movimento.

#### ~~🔴 ANTES DE QUALQUER DEPLOY — duas coisas medidas hoje~~ (RESOLVIDO em 2026-09-15: `v0.66.0` implantada, `VERSION` bumpado — ver bloco acima)

- **`main` NAO esta implantado.** O CT roda a `v0.65.0`; tudo desta sessao esta no `main` e nao subiu.
- **O `VERSION` continua `0.65.0` e nao foi bumpado.** As 17 entradas estao sob `## Unreleased`.
  **Deployar sem bumpar faz o `/v1/health` responder `0.65.0` para um binario que nao e' a `0.65.0`** —
  e a prova do proprio deploy (`VERSAO CONFERE`) passaria mentindo. **Bumpe antes.**

#### Fora do repositorio: uma ponta fechada, uma ABERTA

- ✅ **Fechada:** `~/.zapgw/deploy-zapgw.sh` (maquina do dono) passou a ler `deploy/deploy.sh`. Tres
  linhas. A linha 25, que aponta para o repo PRIVADO antigo (`/c/dev/zapgw-dev/implanta/deploy.sh`),
  **nao foi tocada de proposito** — aquele repo nao foi renomeado, e um `sed` global quebraria ali.
  Backup em `~/.zapgw/deploy-zapgw.sh.antes-do-rename-2026-09-08`. `bash -n` limpo.
- ✅ **Fechada em 2026-09-15:** a unit do systemd no CT 125 NAO cita `implanta/` (medido por
  `systemctl cat zapgw` antes do deploy da `v0.66.0`).

#### Tres mecanismos novos, todos com prova contra dado real

- **Portao de acoplamento shell/Go** (`internal/config/shell_log_coupling_test.go`, T-235) — virou
  linha propria na tabela de regras duras do `CLAUDE.md`. Reprovou duas vezes: uma pelo implementador,
  uma re-provada pelo planner em vez de aceita do relatorio.
- **Portao de ponteiro de doc alargado** para alem de `.go` (T-234) e depois para nome de arquivo
  solto (T-238). Hoje reporta o proprio alcance: *"swept 15 doc file(s), 1115 pointer(s) examined, 0
  dead"*. **Zero excecoes pendentes.**
- **Quatro guardas vacuosas consertadas**, duas delas guardando contra vazamento de campo interno da
  Meta no corpo da resposta — passariam num vazamento real.

#### 🙋 A METADE QUE E' DECISAO DO DONO, e nao corre sozinha

Ficou **fora** desta sessao de proposito, e ele pediu reavaliacao quando a primeira metade terminar:
tags `json:` portuguesas, os **18 nomes de contador** (o consumidor alarma em 8), os verbos e flags da
CLI, e as seis `ZAPGW_*` obsoletas do CT.
📌 **Uma inconsistencia de fio medida hoje e nao tocada:** `internal/outbound/block_handler.go:166`
emite `json:"operacao"` no meio de `instance`/`processed`/`failures`. Tres inglesas e uma portuguesa
no mesmo objeto. **Nao mude sem ele** — tem consumidor do outro lado.

### 📌 2026-09-07/08 — a traducao do codigo para ingles: 4 pacotes fechados, so' o que foi medido

✅ **`internal/meta`, `internal/config`, `internal/inbound` e `internal/outbound` estao traduzidos e
mesclados** (T-223, T-224, T-225, T-226), mais a T-233 que consertou o efeito colateral.
**Medida do progresso, contada pela mesma varredura antes e depois:** linhas `.go` carregando palavra
portuguesa cairam de **5.023** (`8997609`) para **2.635**. O que sobra e' quase todo deliberado —
literal de fio, vocabulario de contrato — mais `cmd/`, que e' a T-227.
**Verify de repo inteiro verde nos 7 pacotes** depois do ultimo merge.

🔥 **A licao que custou, e ela e' sobre o RECORTE das tarefas, nao sobre os implementadores.** Eu
dividi a traducao por pacote. A T-224 traduziu `config.WarnOldEnvVar` — certo, era o pedido — e
quebrou **8 testes em `cmd/zapgw` e `internal/outbound`**, que prendiam o substring `obsoleta`.
**Cada implementador rodou o verify do proprio pacote e passou verde**, porque a quebra mora fora
dele. So' o `go test ./...` enxergou.
➡️ *A fronteira que voce desenhou em volta da tarefa nao e' a fronteira do efeito.* Quando a tarefa
mexe em algo que OUTROS pacotes observam, o `Verify` dela tem de ser de repo inteiro. Escrito em
`docs/ARMADILHAS.md` com o custo.

🔥 **Duas guardas de vazamento passavam vacuamente ha muito tempo**, achadas pela leitura linha a
linha da T-226: `handler_test.go` e `templates_handler_test.go` checavam a ausencia de
`subcodigo_meta`/`explicacao_meta`/`rastro_meta`/`detalhe_meta`, campos renomeados para
`meta_subcode` e companhia. A guarda passava sempre — **e passaria num vazamento real**. Consertadas.
Tambem em `docs/ARMADILHAS.md`.

⚠️ **ARMADILHA DE PROCESSO, medida DUAS vezes hoje e ainda sem mecanismo:** worktree de implementador
**nasce na base em que a sessao estava quando o agente foi criado**, nao no `main` atual. Nas duas
levas os agentes nasceram em `8997609` e nao enxergavam o `docs/TASKS.md` com as proprias tarefas.
Contornado mandando cada um ler por `git show main:docs/TASKS.md`. **O sintoma e' o agente reportar
que a tarefa nao existe, ou pior, trabalhar com spec velho.** Antes de despachar, confira a base.

📉 **O espelho pt-BR foi cortado de 11 para 4 docs** (`44b8a93`), −6.113 linhas. Criterio, escrito no
`CLAUDE.md`: espelho existe quando um brasileiro que NAO le este codigo precisa do doc para AGIR, e
errar custa FORA deste repositorio. Ficaram `README`, `CONTRATO-CONSUMIDOR`, `MANUAL-DO-INTEGRADOR` e
`MIGRACAO-PARA-O-ZAPGW`. O maior ganho foi `ARMADILHAS` (4.490 linhas e o doc de maior ritmo de
escrita).

🙋 **DECISAO DO DONO, pendente e combinada:** a metade do CONTRATO ficou **fora** desta leva de
proposito — tags `json:` portuguesas, os 18 nomes de contador (o consumidor alarma em 8), os verbos e
as flags da CLI, e as seis `ZAPGW_*` obsoletas do CT. Ele pediu reavaliacao quando a primeira metade
terminar. *Nada disso corre sozinho.*

### 📌 2026-09-06 — o que fica para amanha (escrito no fim do dia, so' o que foi medido)

✅ **`v0.65.0` ESTA EM PRODUCAO**, provada pelo proprio deploy: `SAUDE OK:
{"ok":true,"versao":"0.65.0"}` seguido de `VERSAO CONFERE: 0.65.0 (igual a construida)`.
**O que ela leva:** T-221 (as tres chaves de topo em ingles) e T-218 (sub-verbos da CLI).
**O que ela NAO leva:** a T-219, que esta no `main` e ainda nao subiu.

✅ **T-221 fechada — o pedido inteiro passou a ter grafia inglesa.** `contacts`, `flow` e `sections`
entraram em `requestAliasAtTopLevel` (`internal/outbound/input_aliases.go:159-161`). Ate ontem um
consumidor 100% em ingles TINHA de mandar uma chave em portugues, e por isso um exemplo limpo do
contrato era impossivel de escrever — foi assim que um exemplo misturado chegou a um documento de
consumidor.
🔴 **O que vale mais que as tres linhas e' o portao invertido:**
`TestRequestTopLevelKeysAreAllAccountedFor` le as tags do `Request` e exige apelido ingles ou
presenca em `docs/contrato-chaves-que-nao-mudam.txt`. O teste que existia percorria a **propria
tabela** e por isso nao enxergava linha AUSENTE — foi assim que as tres sobreviveram a T-203 inteira.
**Reprovou contra dado real duas vezes** (o implementador tirando `secoes`; eu tirando `contacts` no
`main`), entao conta como mecanismo pelo criterio desta casa.

✅ **T-219 fechada e no `main` (`fe992e5`), NAO implantada.** Strings de `cmd/zapgw` e
`cmd/grafo-falso` em ingles. A varredura da propria tarefa **nao vem vazia**, e isso e' o esperado:
o que sobra sao as grafias dos VERBOS e os NOMES das flags (contrato de CLI, territorio da T-220) e
vocabulario compartilhado de proposito com os pacotes fora de escopo (`PRECISA DE GENTE`, `sim`/`nao`).
🔥 **O implementador RE-DELEGOU apesar da proibicao escrita** (abriu um fork para o `provision.go`) —
segunda vez, depois de 21/08. Contido: `git worktree list` deu tres arvores e nao quatro, o filho
dividiu a arvore do pai, e `HEAD` da worktree seguia em `cb7be5a` (ninguem commitou por conta
propria). *"Nao re-delegue" continua sendo pedido, nao mecanismo.*

✅ **T-220 implementada em 2026-09-15** — e MESCLADA + IMPLANTADA na `v0.67.0` (bloco de 03:40; o
resto deste paragrafo e' o estado de antes do merge, mantido como registro). Removeu a
grafia portuguesa de cinco verbos de topo (`provisionar`, `fumaca`, `diagnostico`, `instancia`,
`consumidor`) — RECUSA agora, nomeando o verbo ingles, em vez de aceitar em silencio; `estado` e os
oito sub-verbos (`rotacionar`, `listar`, `mostrar`, `pausar`, `remover`, `registrar`,
`desregistrar`, `reabrir-cadastro`) ficaram de fora de proposito (decisao separada, T-218). Verify
de repo inteiro verde. 🙋 **CONTINUA valendo so' ser segura se tres passos acontecerem no mesmo
movimento** (isso e' do PLANNER, nao do implementador): mesclar -> deployar -> atualizar
`/root/rotaciona-token.sh` no CT 125, que usa `zapgw instancia listar` (fora do repositorio, tem de
mudar para `zapgw instance list` no mesmo dia do deploy). Foi exatamente esse descompasso que
quebrou o script em 06/09 00:36. *`main` nao e' o implantado.*

📝 **O prompt para o consumidor esta ESCRITO e NAO publicado**, de proposito:
`prompt-consumidor-contato.local.md`, na raiz do repo (gitignorado por `*.local.md`).
Ele cobre o que mudou (as tres chaves + o portao) e como enviar cartao de contato, com o aviso do
`wa_id` — o campo que decide se o cartao chega com "Conversar" ou com "Convidar para o WhatsApp",
sendo que **nenhum dos dois da erro**.
🔥 **Duas licoes do dono, hoje:** (1) *"Quem mandou abrir canal?"* — pedido de PROMPT e' texto para
revisar, publicar espera palavra explicita; eu publiquei no canal deles e tive de reverter.
(2) *"Quando eu pedir prompt, escreva em portugues, o resto do projeto todo em ingles."*

🔴 **Tres pendencias medidas hoje que NAO estao na fila do repo:**
- **[1466] / T-222 (ja enfileirada):** `docs/CONTRATO-CONSUMIDOR.md` documenta `classe` com
  `permanente`/`retentavel`/`desconhecido`; o codigo emite `class` com
  `permanent`/`retryable`/`config`/`unknown` desde a T-209. Doc falso no documento que os
  consumidores leem para integrar.
- **[1467]:** o erro de contato cita `contatos[0].name.formatted_name` para quem mandou `contacts`
  (`internal/outbound/message.go:1303`). **Depende da pergunta de 01/09 ao consumidor**, ainda sem
  resposta: eles comparam o TEXTO de mensagem de erro em algum lugar?
- **[1468]:** seis `ZAPGW_*` com nome obsoleto ainda em uso no arranque do CT, gritadas a cada
  deploy. Uma delas e' a chave de cifra — trocar o nome sem levar o valor derruba o servico.


✅ **FEITO EM 2026-08-31 00:44 (-03; `gh repo view --json createdAt` = `03:44:28Z`): o repositorio
publico foi APAGADO E RECRIADO, e o historico comeca
num commit so.** Medido, nao afirmado:
- `git rev-list --count origin/main` = **1**. A arvore do commit genesis e' **identica** a que passou
  no verify (`git diff` entre a antiga HEAD e o genesis: vazio).
- **As 8 agulhas dao zero** na arvore e no `origin`. O portao de nome (T-193) e' o que mede isso
  agora — nao mais um `git grep` de quem lembrou.
- **Release `v0.60.1` reposto e PROVADO byte a byte:** baixei de volta do release novo e o `sha256`
  dos dois binarios bate com o do release original (`a48a031d…` amd64, `a32a78e4…` arm64).
- **Segredo `ZAPGW_FORBIDDEN_NAMES` criado** no repositorio novo, entregue por `stdin` a partir de
  `~/.zapgw/forbidden-names.txt` — nunca em linha de comando.
- **O que a recriacao levou e nao volta:** o historico anterior (15 commits), as issues e as
  estrelas (eram zero). Nada funcional apontava para o GitHub — varri `implanta/` e `.github/`: so' um
  `Documentation=` na unit do systemd, e a URL nao mudou.

✅ **FEITO (T-195): a CI recebe o segredo `ZAPGW_FORBIDDEN_NAMES` como `env:` de JOB e ganhou passo
proprio do portao de nome** (`.github/workflows/verify.yml`), espelhando o portao de telefone.
✅ **E ela RODOU: verde as 00:54, com os quatro portoes passando** (run `33355320803`). Os **tres
runs anteriores falharam** no portao de nome, por falta de agulha — entao a propria CI ja reprovou
contra dado real antes de ser confiada, que e' o criterio desta casa.
🔴 **E isso derrubou uma coisa que eu tinha acabado de escrever aqui:** *"a cota de Actions so' reseta
em 2026-09-01"*. Os runs executaram em **08-31**. A data veio da explicacao do dono e virou estado
sem ninguem re-medir. *Afirmacao sobre coisa que voce nao controla envelhece calada* — e esta e' a
**terceira** vez que este mesmo paragrafo mente, agora registrado no `CLAUDE.md`.
⚠️ **Decisao que eu tomei e que voce pode querer rever:** num PR vindo de **fork**, o GitHub nao
entrega segredo, entao o portao vai reprovar por "nao consegui verificar" — e eu escolhi manter
assim, falhando fechado, em vez de virar skip. Skip seria a cegueira que o portao existe para nao ter.
Documentado em comentario no proprio workflow.

✅ **A `v0.61.0` ESTA EM PRODUCAO desde 2026-08-31 10:19, e o consumidor foi liberado as 10:21.**
Prova medida, nao afirmada: `SAUDE OK: {"ok":true,"versao":"0.61.0"}` seguido de
`VERSAO CONFERE: 0.61.0 (igual a construida)`, uma troca de binario atomica com o mesmo `sha256`
conferido no no e dentro do container, saida `0`.
- **O passo 3 (escritores deles em ingles) esta liberado.** Nao ha janela para acertar: o gateway
  aceita os dois idiomas ao mesmo tempo, e vai aceitar ate o passo 4.
- 📌 **O deploy roda por `~/.zapgw/deploy-zapgw.sh`**, fora do repositorio. Ele LE os cinco valores de
  topologia do `deploy.sh` do repo privado antigo (`/c/dev/zapgw-dev/implanta/deploy.sh:53-57`), onde
  eles ficaram como default do alvo real — o publico passou a EXIGI-los porque endereco interno nao
  entra aqui. **Nenhum valor passa por chat, commit ou linha de comando.**
- ⚠️ **Licao do proprio deploy:** o runner extraia o valor da chave SSH com `sed`, entao o `$HOME`
  saia LITERAL — o ssh avisou 21 vezes que nao achava a chave, caiu no agente, **e o deploy funcionou
  assim mesmo**. *Falha que ainda entrega o resultado certo e' a que ninguem conserta*, e 21 avisos
  por execucao ensinam a ignorar a saida do deploy, que e' onde mora a prova. Consertado.
✅ **A TAG `v0.61.0` ESTA NO `origin`**, apontando para o commit do bump (`6f975f4`). O portao a
recusava por falso positivo — tag que aponta para commit ja publicado acrescenta um *ponteiro*, nao
commits, e ele lia zero como "medicao vazia". **T-204 consertou distinguindo as duas causas**, e
acrescentou o que faltava: a **MENSAGEM da tag anotada e' varrida**, sempre. Provado com agulha real
na mensagem — bloqueia citando `mensagem da tag, linha 1`.

📌 **O passo 4 e' MAJOR e PARA PARA PERGUNTAR AO DONO.** Ele vira a saida para ingles e depois apaga o
apelido de entrada. Nao acontece sozinho, aconteca o que acontecer com a fila.

✅ **FECHADO: o par ANTES/DEPOIS existe, e a `v0.60.1` passou.** Medicao do consumidor em
2026-08-31 00:28: **77 segundos contra 79 do ANTES**, mesmo roteiro e mesmo template,
`tentativas: 1` em tudo, nenhuma retentativa. A assimetria de status que ficou aberta no ANTES sumiu
— `sent`, `delivered` e `read` nos dois disparos —, **sem concluir que consertamos nada**: pode ser
ordem de chegada da Meta, e eles disseram isso em vez de creditar a versao.
⚠️ **Eles invalidaram um numero que eles mesmos tinham oferecido:** o par
`recebido_em`/`processado_em` nao se compara — o primeiro tem granularidade de SEGUNDO, o segundo tem
microssegundos, e a diferenca mede distancia da borda do segundo, nao latencia. **Sai das duas
medicoes.**
🔴 **O que quase custou isso:** a medicao foi pedida as 23:52 **no arquivo errado** (o deles), e
reenviada as 00:02 no certo. Depois eu escrevi as 00:49 **sem reler** e cobrei o que ja estava
entregue as 00:15 e as 00:28 — a resposta deles ficou **cinco horas** parada. As duas licoes estao em
`github/docs/CANAL-ENTRE-SESSOES.md`: *o seu arquivo e' o que mora no repositorio do OUTRO*, e
*"eu li" tem prazo de validade — releia no movimento de ESCREVER*.

✅ **FECHADO: o passo 1 da T-189 esta NO AR desde 2026-08-31 00:15** (BACKEND 3.236.0, 6.235 testes
verdes, 15 guardas novas). **A T-189 nao esta mais bloqueada.**
🔴 **E eles contradisseram a forma que a gente pediu, com razao medida:** em vez de `novo or velho`
em **55** leitores espalhados por 13 arquivos, traduzem **uma vez na porta** (10 pontos). Cinquenta e
cinco pontos de edicao sao cinquenta e cinco chances de esquecer um, e **o esquecido nao falha** —
`.get()` ausente vira `None`, vira string vazia, e a mensagem sai errada sem acordar ninguem.
*E' o mesmo argumento que usamos para inverter o portao de telefone na T-191: enumeracao esquece o
item novo, e o esquecido e' invisivel.* **A contradicao foi o produto do canal, nao o atrito.**

📌 **O canal sao DOIS arquivos, e confundi-los ja custou 32 minutos de silencio invisivel:**

| arquivo | quem ESCREVE | quem LE |
|---|---|---|
| `C:\dev\<consumidor-b>\zapgw-STATUS.local.md` | **nos** | eles |
| `C:\dev\zapgw\<consumidor-b>-STATUS.local.md` | **eles** | nos |

🔴 **`<consumidor-b>` e' pseudonimo de proposito: este repositorio e' PUBLICO e nome de cliente nao
entra.** Para resolver o nome na maquina, `ls *-STATUS.local.md` na raiz — o arquivo esta la, e esta
gitignorado. *Nao escreva o nome real aqui para "facilitar": e' irreversivel.*

O caminho antigo (`C:\dev\zapgw-dev`) esta morto nos dois sentidos, e as 7.418 linhas de historico ja
foram copiadas. **A seçao orfa das 23:52 fica no topo do arquivo deles** — nao se apaga arquivo do
outro, nem para desfazer bobagem propria.
🔴 **`*.local.md` esta no `.gitignore` desde 2026-08-30 e tem de continuar** — o canal carrega
telefone real e `wamid` de producao, e este repositorio e' publico.


🙋 **DUAS COISAS QUE SO' O DONO DECIDE, e nenhuma corre.** A migracao do contrato acabou (T-189,
`v0.63.0` em producao e provada pelo consumidor). Sobram estas, ambas com raio de alcance grande:

1. **Apagar o apelido de ENTRADA.** Hoje um pedido em portugues continua funcionando, e e' essa rede
   que segura o que ninguem previu. **A autorizacao de 31/08 foi para a VIRADA, nao para apagar a
   rede** — isto e' outra conversa. Quando for a hora, o portao e o mesmo: `nome_antigo_usado` em
   zero **e** volume subindo ao lado, agora com as treze chaves que ele passou a enxergar.
2. **Os pares dos 18 nomes de contador.** Eles continuam em portugues **de proposito** — o doc que
   dizia que a tabela ja os carregava era falso, e a correcao esta na secao 8.11.
   🔴 **O consumidor alarma em 8 dos 18**, entao renomear sem ele no circuito quebra alarme em
   silencio. Decidir os pares passa por ele antes.

📌 **Uma lacuna que o consumidor declarou e que so' o tempo fecha:** o valor `mensagem` -> `message`
do tipo de evento so' aparece quando **uma cliente escrever** — nao ha como fabricar. O mecanismo ja
esta provado num valor que muda (`observed` -> `observado`); falta a combinacao especifica. Se der
errado, o sintoma e' evento **preso com aviso**, nao evento sumido — por causa do conserto que eles
fizeram no `processado_em` hoje de manha.

🔴 **O AVISO DE "NAO RODE DEPLOY" MORREU EM 2026-09-06, POR DECISAO DO DONO — e o preco ja foi
pago.** Ele autorizou: *"Autorizado o deploy, amanha trabalhamos na lojinha."* O deploy da `v0.65.0`
rodou logo apos o commit do bump (`21d7d65`) e o `pct delsnapshot` apagou o `pre-update` de
31/08 23:09.
**Consequencia MEDIDA, nao temida:** aquele snapshot era a unica outra copia do `token_envio`
original da instancia restaurada; hoje existe **uma** copia, a que esta viva no banco, e ela
**continua sem prova de envio real**. Se a restauracao de 31/08 estiver errada, o caminho barato
acabou e sobra pedir token novo na conta Meta de terceiro (Vikunja [1449]).
⚠️ **O que fazer amanha, na ordem:** provar a instancia com um envio real ANTES de qualquer outro
deploy. Se o envio funcionar, o assunto encerra e [1449] fecha de verdade.

**O que aconteceu em 2026-09-05, medido:** um token permanente de System User da Meta, com acesso de
ADMIN do negocio, vazou em texto claro no prompt de uma rotina agendada e foi despejado em 62
transcripts de 7 projetos entre 13/08 e 05/09. Foi anulado no painel (o botao e' tudo-ou-nada) e as
instancias foram rotacionadas. Duas licoes que custaram na hora e valem alem deste incidente:

- 🔥 **Rotacao em LACO sobre todas as instancias alcancou tres quando o alvo era uma.** Slug por
  extenso, uma instancia por comando. A ferramenta que sobrou disso (`/root/rotaciona-token.sh`, no
  CT, fora do repo) impoe isso.
- 🔥 **O arquivo `.db` do SQLite NAO e' o estado completo.** As escritas recentes estavam no `-wal`
  (ultimo checkpoint 5h antes). Puxar so' o `.db`, escrever por cima e apagar o `-wal` desfez uma
  rotacao ja feita e ~5h de transito/idempotencia. Com o servico parado, `PRAGMA
  wal_checkpoint(TRUNCATE)` antes de copiar, ou leve `.db` + `-wal` + `-shm` juntos.
- 🔥 **`main` nao e' o implantado.** Um verbo novo mergeado no `main` nao existe no binario do CT ate
  o deploy. Migrar um chamador para a grafia nova antes do deploy quebra na hora — aconteceu com o
  script do CT dez minutos depois da T-220 ser escrita.

## Active

> A fila do periodo privado esta em `iscarelli/zapgw-dev`, congelada. Tarefa nova nasce aqui.

## [ ] T-246  Three top-level CLI verbs never got an English spelling: `transito`, `perdidas`, `versao`
Why:     A T-220/T-245 aposentaram todo verbo que TINHA par ingles. Sobraram tres sem par
         (`cmd/zapgw/provision.go:116,121,131`): `transito`, `perdidas`, `versao` — a CLI de um
         projeto publico em ingles ainda responde a tres verbos portugueses, e so' a eles. Achado ao
         conferir por que `/v1/health` responde `"versao"` (isso e' contrato HTTP, fica; a CLI nao).
Files:   cmd/zapgw/provision.go, cmd/zapgw/env_aliases.go (oldVerbRefused), cmd/zapgw/*_test.go,
         cmd/zapgw/menu.go (se enumerar), docs/*.md que mostrem os tres, docs/CHANGELOG.md
Do:      Os nomes ingleses seguem os ARQUIVOS que ja existem: `transit` (`transit.go`), `lost`
         (`lost.go`), `version`. Mesmo desenho da T-245: o verbo ingles despacha; o portugues cai
         em `oldVerbRefused(old, new)` (exit != 0 nomeando o ingles). Nomes de flag-set idem.
         Mensagens que enumeram verbos so' com o ingles. NAO toque no JSON de `/v1/health`
         (`cmd/zapgw/main.go:51`, `deploy/deploy.sh:183` grepa `"versao"` — portao T-235).
Verify:  CGO_ENABLED=0 go build ./... && go test ./... && go vet ./... && gofmt -l cmd internal
         `grep -n 'case "\(transito\|perdidas\|versao\)"' cmd/zapgw/provision.go` so' nas linhas
         que chamam `oldVerbRefused`. Prova manual: `./zapgw versao` sai != 0 nomeando `version`;
         `./zapgw version` imprime a versao.
After:   T-240

## [ ] T-247  The `GET /v1/estado` top-level scalars, the counters vocabulary and the health `verdict` in the contract are still Portuguese
Why:     A T-240 (d75b02a) consertou os NOMES DOS BLOCOS e os quatro vizinhos, e no caminho mediu uma
         familia que nao estava na tabela dela: o exemplo executado do `GET /v1/estado` e a prosa em
         volta ainda mostram `estado`/`pausada`/`versao`/`gerado_em`/`carimbos_desde`/`contadores`/
         `serie_7_dias`/`serie_diaria` e, dentro de cada contador, `ultimos_7_dias`/`ultimo_em` —
         e o codigo emite `state`/`paused`/`version`/`generated_at`/`stamps_since`/`counters`/
         `last_7_days_series`/`daily_series` (`internal/outbound/state.go:66-152`) e
         `last_7_days`/`last_at` (`state.go:237,241`). Foi exatamente `versao` que o consumidor leu
         como `None` em 2026-09-15 03:34. Mesma familia: `GET /v1/instances/{slug}/health` numa
         instancia Instagram responde `"verdict":"not_applicable"`
         (`internal/outbound/health_handler.go:94,155`) e o doc (~4746 EN / ~4693 pt-BR) diz
         `"veredito":"nao_se_aplica"`.
Files:   docs/CONTRATO-CONSUMIDOR.md, docs/CONTRATO-CONSUMIDOR.pt-BR.md
Do:      Mesmo metodo da T-240: prova contra o codigo antes (`grep -n 'json:"' internal/outbound/state.go`),
         doc depois, `grep` do doc no fim. Casos (a)/(b)/(c) iguais. 🔴 NAO MEXA EM CODIGO. As chaves
         que CONTINUAM em portugues no codigo ficam iguais no doc (`hoje`, `definido_em`,
         `cursor_antes`/`cursor_depois`, `lideranca`, literais `medicao`/`sonda_externa`) — sao a
         T-248, que espera decisao do dono. Tabela de migracao `dia`/`serie_7_dias` (~5166) e' caso (b).
Verify:  Prova por familia colada; `grep -n "\"estado\"\|\"pausada\"\|\"versao\"\|\"gerado_em\"\|carimbos_desde\|\"contadores\"\|serie_7_dias\|serie_diaria\|ultimos_7_dias\|ultimo_em\|veredito" docs/CONTRATO-CONSUMIDOR.md`
         so' com caso (b)/(c) listados um a um (o `"versao"` do `/v1/health` e' caso (a) correto: fica).
         `CGO_ENABLED=0 go build ./... && go test ./...` (garantia).
After:   T-246

## [ ] T-248  Seven response keys and literals still Portuguese in code, among English siblings
After:   DECISAO DO DONO — muda chave de RESPOSTA que o consumidor le hoje. Nao despache sem ele.
Why:     Medido pela T-240 contra o codigo (2026-09-15): `hoje` (`state.go:236`, irmaos `last_7_days`/
         `last_at`), `definido_em` (`state.go:518`, cinco irmaos em ingles), `cursor_antes`/
         `cursor_depois` (`block_handler.go:183-184`, irmaos `instance`/`total`/`blocked`),
         `lideranca` (`state.go:220`, bloco inteiro), literais `medicao` (`internal/config/number.go:76`)
         e `sonda_externa` (`external_probe.go:151`), e as frases de `instruction`
         (`state.go:555-558`). Sao restos do passe de 2026-08-30 — mas cada um e' contrato vivo:
         renomear quebra quem le (o `versao` -> `version` ja custou um `None` ao consumidor).
Files:   internal/outbound/state.go, internal/outbound/block_handler.go, internal/config/number.go,
         internal/outbound/external_probe.go, os `_test.go`, docs/CONTRATO-CONSUMIDOR.md (+pt-BR),
         docs/CHANGELOG.md
Do:      O que o dono decidir entre: (1) renomear de uma vez com bump MINOR e aviso no canal ANTES do
         deploy, com a lista velho->novo; (2) emitir os dois nomes por uma versao (`omitempty` no
         velho) e aposentar depois; (3) deixar. Escrever a decisao aqui antes de despachar.
Verify:  a definir com a decisao.

## [ ] T-231  Translate the English side of the docs that is still Portuguese
After:   T-230
Why:     desde 2026-09-07 a politica e' documentacao em INGLES por padrao, com espelho pt-BR so' nos
         QUATRO docs que um brasileiro precisa para AGIR (`README`, `CONTRATO-CONSUMIDOR`,
         `MANUAL-DO-INTEGRADOR`, `MIGRACAO-PARA-O-ZAPGW`) — os outros sete espelhos foram apagados.
         Isso torna o lado ingles a UNICA versao da maioria dos docs, e prosa portuguesa nele deixou
         de ser desleixo para virar o texto que o leitor recebe. Medido em 2026-09-07, o lado EN
         de varios ainda carrega prosa
         portuguesa: `CONTRATO-CONSUMIDOR.md` (330), `ARMADILHAS.md` (160), `MIGRACAO-CONTRATO-EN.md`
         (119), `INVENTARIO-STRINGS.md` (353), `INVENTARIO-CHAVES.md` (152), `INVENTARIO-VALORES.md`
         (62), `HANDOFF.md` (72). Doc bilingue com metade da metade em portugues nao e' bilingue.
Files:   docs/CONTRATO-CONSUMIDOR.md, docs/ARMADILHAS.md, docs/MIGRACAO-CONTRATO-EN.md,
         docs/INVENTARIO-CHAVES.md, docs/INVENTARIO-STRINGS.md, docs/INVENTARIO-VALORES.md,
         docs/MANUAL-DO-INTEGRADOR.md, docs/MODELO-DE-USO.md, docs/ONBOARDING-META.md,
         docs/META-CAMPOS-DE-WEBHOOK.md, docs/MIGRACAO-PARA-O-ZAPGW.md, README.md, CLAUDE.md,
         HANDOFF.md
Do:      🔴 Boa parte das ocorrencias e' LEGITIMA e tem de ficar: e' o NOME PORTUGUES DA CHAVE sendo
         citado (`instancia`, `token_envio`, `botoes_template`). Um inventario de chaves portuguesas
         escrito em ingles continua citando as chaves em portugues.
         Va arquivo por arquivo. Em cada ocorrencia decida:
         (a) PROSA portuguesa no arquivo EN -> traduza;
         (b) LITERAL citado (chave, valor, comando, nome de arquivo) -> NAO toque;
         (c) citacao textual do dono ou do consumidor -> NAO toque, e' fala de outra pessoa.
         NAO mexa em nenhum `.pt-BR.md`: os quatro que sobraram sao portugueses de proposito.
         Confira que o cabecalho `Código:` de cada doc ainda aponta para arquivo que existe.
Verify:  liste no relatorio, por arquivo, quantas ocorrencias voce traduziu e quantas deixou em (b)
         ou (c). `go test ./internal/config/ -run TestDoc` verde (os ponteiros dos docs).

