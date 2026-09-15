# Tasks

## Em voo — retomar por aqui

> Bloco de retomada. **Cada item sai daqui quando for resolvido**; ele nao e' historico.
> Escrito ao fim de 2026-08-30, o dia em que o repositorio virou publico. Bloco de retomada
> mentindo e' pior que bloco nenhum: e' o primeiro texto que a proxima sessao le.

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
  nesse arquivo). Os ALIASES continuam no codigo (T-214 item 4, decisao do dono); a T-243 limpa
  os comentarios/docs que ainda citam os nomes velhos como vivos.
- 🧹 **21 worktrees de implementador acumuladas em `.claude/worktrees/`** (medido por
  `git worktree list` em 2026-09-15). So' uma tem trabalho nao mesclado conhecido: a da T-231
  (`agent-af905375f3c5ebcb1`, commit `8d3fd43`, ver abaixo). As outras sao lixo de tarefas ja
  aposentadas — `git worktree remove` uma a uma, conferindo `git log main..<branch>` vazio antes.

#### 🔴 A FILA agora — TRES tarefas, nesta ordem: T-240, T-231, T-220 (detalhe no bloco de 09-08 abaixo)

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

🚦 **T-220 e' a proxima e PAROU DE PROPOSITO esperando o dono.** Ela remove as grafias
portuguesas dos verbos, e so' e' segura se **tres passos acontecerem no mesmo movimento**:
mesclar -> deployar -> atualizar `/root/rotaciona-token.sh` no CT 125, que usa `zapgw instancia
listar`. Foi exatamente esse descompasso que quebrou o script em 06/09 00:36. *`main` nao e' o
implantado.*

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

## [ ] T-243  Comments, examples and docs still name the OLD `ZAPGW_*` variables as the live ones
Vikunja: 1618
Why:     Em 2026-09-15 01:32 o dono renomeou, no CT 125, os seis nomes obsoletos de `/etc/zapgw/env`
         para os ingleses (`ZAPGW_ENCRYPTION_KEY`, `ZAPGW_DATABASE`, `ZAPGW_ADDRESS`,
         `ZAPGW_INGRESS_VIA`, `ZAPGW_CONNECTOR_READY`, `ZAPGW_EXTERNAL_PROBE_URL`) — saude ok na
         `v0.66.1`, zero avisos `deprecated`. A partir dai, todo comentario, `.env.example` e doc que
         diz "a chave vive em `ZAPGW_CHAVE_CIFRA`" descreve um estado que nao existe mais: doc falso,
         no sentido de `docs/DOCUMENTACAO.md`. Medido com `grep` em 2026-09-15: 15 arquivos, 34
         ocorrencias fora dos aliases, dos testes e do historico.
Files:   .env.example, README.md, README.pt-BR.md, cmd/zapgw/main.go, deploy/check-leadership.sh,
         deploy/deploy.sh, deploy/profile-zapgw.sh, deploy/zapgw.service, docs/CONTRATO-CONSUMIDOR.md,
         docs/CONTRATO-CONSUMIDOR.pt-BR.md, docs/META-CAMPOS-DE-WEBHOOK.md, docs/ONBOARDING-META.md,
         internal/config/crypto.go, internal/outbound/external_probe.go, internal/outbound/ingress.go,
         docs/CHANGELOG.md
Do:      Comando de partida (rode e cole no relatorio):
         `grep -rn "ZAPGW_CHAVE_CIFRA\|ZAPGW_BANCO\|ZAPGW_ENDERECO\|ZAPGW_ENTRADA_VIA\|ZAPGW_CONECTOR_READY\|ZAPGW_SONDA_EXTERNA_URL" --include="*.go" --include="*.sh" --include="*.service" --include="*.md" --include="*.example" . | grep -v "_test.go\|^./.claude\|env_aliases.go\|CHANGELOG\|TASKS.md\|ARMADILHAS"`
         Em cada ocorrencia decida, e liste no relatorio com o caso:
         (a) descreve o estado VIVO (comentario "a chave vive em X", `.env.example`, README, unit,
             doc de operacao) -> troque pelo nome novo;
         (b) e' o PAR de migracao citado de proposito ("aceita X ou Y", tabela de aliases, T-214,
             `config.EnvOrOld(..., new, old)`) -> NAO toque: o alias continua existindo no codigo;
         (c) `deploy/check-leadership.sh:69-71` exporta os nomes velhos para um binario de TESTE ->
             troque pelos novos (o binario aceita os dois; o script deve falar a lingua atual).
         🔴 SO' COMENTARIO, EXEMPLO E DOC. Nenhuma linha de codigo executavel muda de comportamento:
         `env_aliases.go` e os pares `New/Old` de `internal/config` e `internal/outbound` FICAM —
         apagar o alias e' decisao do dono (T-214, Do item 4), nao desta tarefa. Se o `grep` de
         partida apontar linha executavel que voce acha que deveria mudar, pare e relate.
         `.env.example`: os placeholders continuam literais (`troque-pelo-...`), so' o nome muda.
Verify:  CGO_ENABLED=0 go build ./... && go test ./... && go vet ./... && gofmt -l cmd internal
         `bash -n deploy/*.sh`. E o `grep` de partida rodado de novo: o que sobrar tem de ser so'
         caso (b), listado um a um no relatorio.

## [ ] T-244  Retire the old `ZAPGW_*` env-var names: an old name set at startup is REFUSED, never read
Vikunja: 1619
After:   T-243
Why:     Decisao do dono em 2026-09-15 ("pode fazer uma de uma vez"), depois de o CT 125 ter sido
         renomeado para os nomes ingleses (zero avisos `deprecated` na `v0.66.1`) e de a medicao
         mostrar que nenhum chamador restante usa nome velho (`/root/rotaciona-token.sh` ja le
         `ZAPGW_DATABASE`/`ZAPGW_SEND_TOKEN`, o profile so' faz `source`). Fecha o item 4 da T-214,
         que estava reservado ao dono. 🔴 **O modo de falha desta mudanca e' "sobe no DEFAULT em
         silencio"** (e' o aviso escrito no topo de `cmd/zapgw/env_aliases.go`): um `/etc/zapgw/env`
         antigo em outro clone, ou um operador que ainda exporta `ZAPGW_CHAVE_CIFRA`, faria o
         gateway abrir um banco vazio com outra chave, sem erro. Por isso o nome velho nao e'
         simplesmente ignorado: **se estiver definido, o processo RECUSA subir**, nomeando o novo.
Files:   internal/config/env_alias.go, internal/config/env_alias_test.go, cmd/zapgw/env_aliases.go,
         cmd/zapgw/main.go, cmd/zapgw/diagnostics.go, cmd/zapgw/lost.go (se usar databasePath),
         internal/config/counter.go, internal/config/transit.go, internal/outbound/external_probe.go,
         internal/outbound/ingress.go, internal/outbound/leadership.go, os `_test.go` de cada um,
         deploy/check-leadership.sh (so' se a T-243 nao tiver trocado os nomes la),
         docs/ARMADILHAS.md, docs/CHANGELOG.md
Do:      Inventario de partida (rode e cole no relatorio):
         `grep -rn "EnvOrOld\|WarnOldEnvVar" --include="*.go" cmd internal | grep -v _test.go`
         Sao ~14 chamadas de `config.EnvOrOld(getenv, new, old)` mais os `WarnOldEnvVar` que as
         acompanham. A mudanca e' UMA, no resolvedor, e mecanica em cada chamador:
         1. `internal/config/env_alias.go`: substitua `EnvOrOld` por
            `EnvRefusingOld(getenv func(string) string, newName, oldName string) (string, error)`:
            le SO' `newName`; se `getenv(oldName) != ""`, devolve `""` e um erro que embrulha
            `ErrObsoleteEnvVar` (sentinela novo) com a mensagem
            `environment variable ZAPGW_X is no longer read -- rename it to ZAPGW_Y (T-244)`.
            Apague `WarnOldEnvVar` (vira morto) e o `oldNameUsed bool` de todo lugar.
            🔴 Nao deixe `EnvOrOld` viva "por compatibilidade": duas funcoes com a mesma pergunta
            e respostas diferentes e' a armadilha-mae.
         2. Em cada chamador: erro -> falha de ARRANQUE (`log.Fatalf`/retorno de erro ate o `main`,
            o que cada sitio ja usa para config invalida — siga o padrao vizinho, nao invente).
            Nos verbos da CLI (`diagnostics.go`, `lost.go`), o erro sai em `stderr` com saida != 0.
            Os pares de constantes `New`/`Old` FICAM (o `Old` e' o que a recusa nomeia); apague so'
            o que nao for mais lido.
         3. Teste, por chamador, tres casos: (a) so' o nome novo -> valor lido; (b) so' o velho ->
            RECUSA com mensagem que contem o nome novo, e o valor NAO foi lido (o default nao
            entra); (c) os dois -> recusa (o velho definido basta; nao ha "o novo vence" aqui).
            Onde ja existe teste do par (T-214 escreveu varios), converta — nao duplique.
            🔴 **O teste (b) tem de existir para a chave de cifra** (`cmd/zapgw/main.go:182`): e' o
            caso que abriria banco vazio. Rode-o ANTES de mexer no resolvedor e cole a falha.
         4. `deploy/check-leadership.sh:69-71`: confira que exporta os nomes NOVOS (a T-243 deve ter
            trocado); se ainda exporta os velhos, troque — com a T-244 o binario de teste recusaria.
         5. `docs/ARMADILHAS.md`: uma linha na entrada da T-214 (ou nova, se nao houver), sem 🔥:
            *alias de variavel de ambiente se aposenta RECUSANDO o nome velho, nunca ignorando —
            ignorar e' subir no default em silencio.* Ainda nao cobrou; diga isso.
         6. `CLAUDE.md`/docs que digam "aceita os dois nomes" ficam falsos: `grep -rn "T-214" docs
            README.md CLAUDE.md` e corrija o que descreve estado vivo; registro historico fica.
         🔴 NAO toque nos verbos da CLI (`warnOldVerb`, dispatch em `main.go`) — e' a T-220, logo
         abaixo. NAO bumpe `VERSION`.
Verify:  CGO_ENABLED=0 go build ./... && go test ./... && go vet ./... && gofmt -l cmd internal
         `bash -n deploy/*.sh`. `grep -rn "EnvOrOld\|WarnOldEnvVar" cmd internal` vazio.
         E a prova manual, colada: `ZAPGW_CHAVE_CIFRA=x ZAPGW_DATABASE=/tmp/t.db ./zapgw` (ou o
         verbo mais barato) sai != 0 com a mensagem nomeando `ZAPGW_ENCRYPTION_KEY`.

## [ ] T-220  Remove the Portuguese spellings of the CLI verbs
After:   T-244 — e o movimento sincronizado (mesclar + deployar + atualizar
         /root/rotaciona-token.sh no CT 125, que hoje usa `zapgw instancia listar` e
         `zapgw instancia rotacionar`) e do PLANNER, nao do implementador. Decisao do dono em
         2026-09-15: "pode fazer uma de uma vez" — esta tarefa, a T-243 e a T-244 vao juntas.
Why:     o projeto e' publico e a decisao de 2026-08-30 e' codigo em INGLES. A T-218 fez a ponte
         (o ingles passou a funcionar); manter a grafia portuguesa para sempre transforma a ponte em
         destino. O portao do contador NAO se aplica aqui: ele existe para o apelido de ENTRADA, que
         tem um terceiro do outro lado. A CLI tem um operador so', e o dono confirmou em 2026-09-06
         que NAO tem nada dele rodando por CLI nem por cron — e' tudo por API, que ja esta em ingles.
🔥 O PERIGO REAL NAO E' "chamador desconhecido", E' `main` != IMPLANTADO. Custo medido em
         2026-09-06 00:36, minutos depois desta tarefa ser escrita: migrei o /root/rotaciona-token.sh
         para `zapgw instance list` porque a T-218 estava no main — e o binario no CT era a v0.64.0,
         que responde `nao sei fazer "list" com uma instancia`. O script quebrou na hora. A grafia
         nova so' existe onde o binario novo esta.
Files:   cmd/zapgw/provision.go, cmd/zapgw/menu.go, cmd/zapgw/env_aliases.go (warnOldVerb),
         cmd/zapgw/*_test.go, deploy/deploy.sh, deploy/profile-zapgw.sh, docs/*.md (os que mostram
         comandos)
Do:      🔴 A ORDEM E' A GARANTIA, e o inverso quebra calado — so' falha na proxima vez que alguem
         rodar o script. Faca nesta ordem:
         1. VARRA todos os chamadores dentro do repo e liste-os no relatorio:
            `grep -rn "zapgw \(instancia\|consumidor\|provisionar\|fumaca\|diagnostico\)" --include="*.sh" --include="*.md" --include="*.go" .`
            Inclua `deploy/`, `.github/`, os docs, e o menu interativo.
         2. ATUALIZE cada chamador para a grafia inglesa.
         3. SO' ENTAO remova as grafias portuguesas do dispatch e o `warnOldVerb` que virou morto.
         4. Atualize as mensagens que ENUMERAM os verbos, de novo — elas mentem se ficarem com os dois.
         Se encontrar chamador que voce nao pode alcancar (fora do repo), NAO remova o verbo que ele
         usa: pare, liste no relatorio, e deixe esse par para o planner.
Verify:  CGO_ENABLED=0 go build ./... && go test ./... && go vet ./... && gofmt -l cmd internal
         E um teste que prove que a grafia portuguesa agora e' RECUSADA com erro que nomeia a
         inglesa — "some silenciosamente" e' o modo de falha desta mudanca.
         E a varredura do passo 1 rodada de novo deve vir vazia.
🙋 PARTE QUE O IMPLEMENTADOR NAO ALCANCA, e o planner faz: `/root/rotaciona-token.sh` dentro do CT
   125 usa `zapgw instancia listar`. Ele vive fora do repositorio e tem de ser atualizado no mesmo
   dia, ou quebra na proxima rotacao de token.


## [ ] T-240  The `GET /v1/estado` blocks the contract still names in Portuguese
After:   T-239
Why:     A T-237 consertou quatro familias do `docs/CONTRATO-CONSUMIDOR.md` e, no caminho, achou uma
         QUINTA — os NOMES DOS BLOCOS do `GET /v1/estado`, nao so' o vocabulario dentro deles.
         **Re-medido por mim contra o codigo em 2026-09-08**, chave por chave:
         | o codigo emite | o doc ainda diz | onde |
         |---|---|---|
         | `instance` | `instancia` | `internal/outbound/state.go:44` |
         | `kind` | `tipo` | `internal/outbound/state.go:51` |
         | `meta_token` | `token_meta` | `internal/outbound/state.go:169` |
         | `ingress` | `entrada` | `internal/outbound/state.go:207` |
         | `ready_connections` | `conexoes_prontas` | `internal/outbound/ingress.go:217` |
         | `measured_at` | `medido_em` | `internal/outbound/ingress.go:223` |
         | `failing_since` | `falhando_desde` | `internal/outbound/ingress.go:230` |
         E a lista de bloqueios: `GET /v1/bloqueios` emite `instance`/`total`/`blocked`
         (`internal/outbound/block_handler.go`), e o doc mostra `instancia`/`bloqueados`.
Files:   docs/CONTRATO-CONSUMIDOR.md, docs/CONTRATO-CONSUMIDOR.pt-BR.md
Do:      Mesmo metodo da T-237, que funcionou: familia por familia, prova contra o codigo antes e o
         `grep` depois. Os tres casos continuam valendo — (a) descreve o que sai hoje, corrija;
         (b) registro/tabela de migracao citando a forma velha de proposito, nao toque; (c) prosa
         portuguesa no `.pt-BR.md` usando a palavra como palavra, nao toque.
         🔴 **Confira os blocos vizinhos que a T-237 declarou fora de escopo e que ninguem mediu
         ainda:** `certificado_do_callback`, `numero_na_meta`, `token_instagram`, `alcance_externo`.
         Eles usam o mesmo vocabulario (`observado`, `nao_configurado`) mas vem de OUTROS arquivos
         (`state.go`, `instagram_renewer.go`, `external_probe.go`). Meça cada um contra o seu proprio
         arquivo antes de mexer — **nao presuma que seguem o mesmo padrao dos outros dois.**
         🔴 NAO MEXA EM CODIGO. Se achar chave que parece errada no codigo, pare e relate.
Verify:  Por familia, a prova contra o codigo antes e o `grep` depois, coladas no relatorio.
         E a lista, uma a uma, das ocorrencias que voce deixou, com o caso (b)/(c) de cada.
         `CGO_ENABLED=0 go build ./... && go test ./...` (so' garantia).

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

