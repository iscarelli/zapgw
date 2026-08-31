# Tasks

## Em voo — retomar por aqui

> Bloco de retomada. **Cada item sai daqui quando for resolvido**; ele nao e' historico.
> Escrito ao fim de 2026-08-30, o dia em que o repositorio virou publico. Bloco de retomada
> mentindo e' pior que bloco nenhum: e' o primeiro texto que a proxima sessao le.

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

🔴 **A `v0.61.0` ESTA NO `main` E NAO ESTA EM PRODUCAO. O consumidor foi avisado para NAO trocar os
escritores para ingles ate a confirmacao.** Se ele trocar antes, os envios dele tomam erro: o campo em
ingles chega e o `v0.60.1`, que e' o que atende hoje, nao reconhece.
- **O que falta e' so' o deploy**, e ele parou **antes de tocar em rede**: exige
  `ZAPGW_DEPLOY_VMID`, `ZAPGW_DEPLOY_HOST` e `ZAPGW_DEPLOY_SAUDE`, que sao topologia do homelab e
  moram com o dono — nao no repositorio, por desenho. **Nao ha nada a consertar no codigo.**
- 🙋 **O dono roda**, com as tres variaveis no ambiente. Depois do deploy, a confirmacao para o
  consumidor sai com a **versao medida no `/v1/health` na hora** — nunca com o numero que se acha que
  subiu, que e' a mentira que a T-184 existe para impedir.
- **O que a `v0.61.0` carrega:** as 30 chaves de direcao ENTRADA aceitam o nome em ingles, por
  POSICAO; a saida nao muda (o diff das tags `json` de producao entre os dois commits e' **vazio**);
  conflito PT+EN no mesmo pedido e' `400` nomeando a chave; e a **idempotencia atravessa idiomas** —
  traduz antes, calcula o hash depois, senao a mesma mensagem sai duas vezes para a cliente.
- ⚠️ **Lacuna declarada:** o contador do nome velho esta ligado em **4 das 7 rotas** (envio, criacao
  de template, leituras, fumaca). `/v1/cadastro`, `/v1/pausa` e `/v1/bloqueios` aceitam o apelido mas
  **nao contam** — e o contador e' o numero que autoriza o passo 4.

✅ **A TAG `v0.61.0` ESTA NO `origin`**, apontando para o commit do bump (`6f975f4`). O portao a
recusava por falso positivo — tag que aponta para commit ja publicado acrescenta um *ponteiro*, nao
commits, e ele lia zero como "medicao vazia". **T-204 consertou distinguindo as duas causas**, e
acrescentou o que faltava: a **MENSAGEM da tag anotada e' varrida**, sempre. Provado com agulha real
na mensagem — bloqueia citando `mensagem da tag, linha 1`.

📌 **O passo 4 e' MAJOR e PARA PARA PERGUNTAR AO DONO.** Ele vira a saida para ingles e depois apaga o
apelido de entrada. Nao acontece sozinho, aconteca o que acontecer com a fila.

🙋 **PARA O DONO — a premissa do snapshot era FALSA, e a fonte era eu. A T-194 foi revertida.**
Voce aprovou *"manter os 3 ultimos e podar no deploy"* com base numa linha que eu escrevi neste
bloco: *"um snapshot `pre-update` se acumula no CT a cada deploy e ninguem poda"*. **Medido no
`git show HEAD~1:implanta/deploy.sh:324-330`: e' falso.**
- O script usa nome **fixo** (`SNAP=pre-update`) e roda `pct delsnapshot` **antes** de criar o novo.
  O comentario no proprio codigo diz isso desde sempre. **Existe exatamente UM snapshot por vez** —
  nunca houve acumulo, e nao ha nada para podar.
- 🔴 **A implementacao, feita sobre a premissa errada, ia na direcao contraria:** trocava o nome fixo
  por nome com timestamp e passava a **guardar tres** — ou seja, **triplicava** o uso de disco que a
  tarefa dizia querer reduzir, e ainda acrescentava 79 linhas de remocao irreversivel a um script de
  producao. Revertido.
- ⚠️ **A remocao nunca rodou contra dado real** e nem podia: nao ha credencial de producao nesta
  sessao. O ensaio foi contra lista sintetica, e o implementador **declarou isso sem rodeio** em vez
  de chamar de prova.
- ✅ **O que vale guardar do trabalho perdido:** o ensaio achou que um `grep -oE` sem ancora casaria
  `pre-update-20260830090000-testedomeuadm` — prefixo **como substring** de um nome que um humano
  poderia criar. Se um dia existir poda de verdade, o filtro tem de ser tokenizado e ancorado
  (`^pre-update-[0-9]{14}$`), nunca substring. *Filtro de remocao irreversivel casa a linha inteira.*
- **A pergunta que sobra e' sua:** existe algum lugar onde snapshot realmente acumula (voce viu
  algo num `pct listsnapshot`?), ou a linha nasceu de leitura errada minha? Se nao acumula, **nao ha
  tarefa** — e a licao e' a de sempre: *eu escrevi um sintoma que nao medi, e ele virou decisao sua.*

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

## Active

> A fila do periodo privado esta em `iscarelli/zapgw-dev`, congelada. Tarefa nova nasce aqui.

## [ ] T-189  O contrato passa a falar ingles — leitor tolerante do lado deles, apelido so' na ENTRADA
Why:    **decisao do dono, 2026-08-30:** *"o projeto precisa ser em ingles, ter feito em portugues foi
        errado. Se a chave chama nome, tem que passar a se chamar name."*
        **Medido no mesmo dia:** 89 chaves `json` nossas com termo em portugues (de 299), **7 rotas**
        e **18 nomes do vocabulario fechado de contadores**.
🔴      **Os 18 contadores sao VALORES no JSON, nao tags** — nenhuma varredura por `json:"` os acha.

### ✅ O passo 1 FOI FEITO, e o consumidor contradisse a forma — a forma dele e' melhor

**No ar desde 2026-08-31 00:15** (BACKEND 3.236.0, 6.235 testes verdes, 15 guardas novas).
**A T-189 nao esta mais bloqueada.**

🔴 **Eles nao fizeram `novo or velho` em cada leitor, e o motivo e' medicao:** eram **55 chaves lidas
em 13 arquivos**. Cinquenta e cinco pontos de edicao sao cinquenta e cinco chances de esquecer um — e
o esquecido **nao falha**: `.get()` de chave ausente devolve `None`, vira string vazia, e a mensagem
sai errada sem acordar ninguem.
**O que fizeram: traduzir UMA VEZ na porta** (modulo `zapgw_idioma`, mapa ingles->portugues em **10
pontos** — o webhook e as respostas do cliente HTTP). Os 55 leitores nao mudaram uma linha, e quando
nos virarmos a saida no passo 4, **nada muda de novo do lado deles**.
*E' o mesmo argumento que nos usamos para inverter o portao de telefone (T-191): enumeracao esquece o
item novo, e o esquecido e' invisivel.*

**A regra de colisao que eles escreveram, e que vale para a nossa tabela:** *so renomeia se o nome de
destino ainda nao existir naquele dicionario.* O nosso ingles para `texto` e' `text`, e `text` e'
tambem nome da Meta dentro de um objeto de mensagem; idem `category`. Um `text` ao lado de um `texto`
e' da Meta e fica quieto. **Na duvida a traducao nao faz nada**, que e' o lado seguro.

### 🔴 O que MEDIRAM do nosso contrato, e muda o tamanho do passo 4

- **29 chaves que eles leem e a nossa tabela nao menciona** — a T-198 esta inventariando. **`classe`**
  (decide se eles reenviam; cai para `desconhecido` calado) e **`codigo_meta`** (`132001`, `131008`,
  `131047`) sao as duas que doem.
- **35 chaves que eles nunca leem**, o que encolhe o trabalho. ⚠️ **Nao e' garantia:** eles mesmos
  acharam `ultimos_7_dias` na lista de "nunca lidas" sendo lido por acesso dinamico
  (`_n(chave, "...")`), que uma busca por `.get("x")` nao ve. **Trate como "provavelmente nao leem".**
- **Rotas: usam 3 das 7** — `/v1/bloqueios`, `/v1/estado`, `/v1/leituras`. Nunca chamam
  `/v1/cadastro`, `/v1/fumaca`, `/v1/pausa`, `/v1/perfil`.
- **Contadores: nomeiam 8 dos 18.** Renomear um dos outros 10 nao os quebra; renomear um dos 8 quebra
  alarme — e os 8 ja estao no mapa deles.
- **Nenhuma contradicao de NOME na tabela.** Os pares que mandamos ficaram bons de ler no codigo deles.

### 🔴 E o defeito que a tabela JA causou: ela nao dizia a DIRECAO

Medido em 2026-08-31 (T-198): `internal/meta/types.go:543` emite **`midia_id`** no evento de webhook —
e e o UNICO dos tres em portugues. A resposta do `POST /v1/media` emite **`media_id`**
(`internal/outbound/media_handler.go:260`) e a **ENTRADA tambem aceita `media_id`** (`mensagem.go:179`
e `:626`) — **de proposito**: o comentario no codigo diz que o nome bate com o que o `/v1/messages`
espera de volta, sem traducao no meio.
**Mesmo conceito, dois nomes, duas direcoes — hoje, antes de qualquer migracao.**
A tabela listava `midia_id -> media_id` sem dizer onde valia; o tradutor deles renomeou a resposta da
rota e **o upload de midia parou**. Consertado do lado deles lendo `midia_id`, que funciona nos dois.
**Toda chave da tabela passa a declarar a direcao** — `SAIDA-EVENTO`, `SAIDA-RESPOSTA` ou `ENTRADA`.
*Tabela sem direcao mente por omissao em toda chave que aparece nos dois sentidos.*
No passo 4 isso se resolve sozinho: o evento passa a mandar `media_id`, e os dois lados do contrato
ficam com o mesmo nome pela primeira vez.

### 🔴 As MINAS: chaves de SAIDA que JA estao em ingles hoje

Inventario completo em **`docs/INVENTARIO-CHAVES.md`** (T-198). Das 29 chaves que o consumidor le e
que faltavam na tabela, **nenhuma** ja esta em ingles e **nenhuma** deixou de ser encontrada: 9 sao
`SAIDA-EVENTO`, 20 `SAIDA-RESPOSTA`, 7 `ENTRADA` (varias aparecem em mais de uma direcao), em 47
pontos de emissao.

🔴 **Mas a varredura inversa achou o que interessa — chaves nossas de SAIDA que JA saem em ingles:**

- `meta.Event` (`internal/meta/types.go`), **SAIDA-EVENTO**: `latitude`, `longitude`, `id`,
  `phone_number_id`, `waba_id`, `timestamp`, `wa_message_id`, `status`, `template`.
- `internal/outbound/estado.go:62` — `ig_id` (`GET /v1/estado`).
- `internal/outbound/bloqueio_handler.go:136,145,166` — `wa_id`.
- `internal/outbound/templates_handler.go:348,388,389,542,545` — `templates`, `id`, `status`.
- `internal/outbound/saude_handler.go:83` — `ok`.
- `internal/outbound/fumaca_handler.go:131` — `wa_message_id`.
- `internal/meta/perfil.go:65-71,92-103` — o objeto de perfil inteiro: `about`, `address`,
  `description`, `email`, `profile_picture_url`, `websites`, `vertical`,
  `profile_picture_handle`.

**Cada uma dessas e' um `media_id` esperando para quebrar outro leitor**, e todas quebram do mesmo
jeito: quem le a tabela conclui que "tudo que nao esta na lista de 89 termos e' portugues", monta um
tradutor sobre essa premissa, e o tradutor renomeia uma chave que ja estava certa.
🔴 **A tabela do passo 4 tem de listar tambem o que NAO muda** — a ausencia nao pode ser lida como
"vai virar ingles". *Tabela de renomeacao que so lista renomeacoes deixa o leitor inferir o resto, e
inferir e' onde ele erra.*

### 📏 A medicao de DEPOIS fechou, e a `v0.60.1` passou

**77 segundos contra 79 do ANTES**, mesmo roteiro e mesmo template, `tentativas: 1` em tudo, nenhuma
retentativa. A assimetria de status que ficou aberta no ANTES sumiu: `sent`, `delivered` e `read` nos
dois disparos — **sem concluir que consertamos nada**, porque pode ser ordem de chegada da Meta.
⚠️ **Eles invalidaram um numero que eles mesmos ofereceram:** o par `recebido_em`/`processado_em` nao
se compara — o primeiro tem granularidade de SEGUNDO, o segundo tem microssegundos, e a diferenca
mede distancia da borda do segundo, nao latencia. **Sai das duas medicoes.**

### O desenho mudou, e a ideia e' do dono

O primeiro desenho era um tradutor bidirecional com **saida duplicada**. O dono perguntou se dava
para migrar *"on the fly"*, sem tradutor. **Da quase** — e o "quase" e' a parte que importa:

🔴 **A ENTREGA e' ASSINCRONA, e por isso NENHUM corte simultaneo funciona.** O gateway faz `POST` no
`callback_url` do consumidor quando a Meta manda evento. Durante qualquer janela de virada existem
entregas **em voo** e, pior, **retentativas da Meta de ate 36 h** carregando o corpo do idioma
antigo. Nao ha instante em que os dois lados possam trocar juntos: **o consumidor TEM de aceitar os
dois nomes na leitura, aconteca o que acontecer.**

**E eles ja fazem isso.** Medido por eles proprios em 2026-08-30, em quatro lugares:
`str(ev.get("de_canonico") or ev.get("de_cru") or "")`. O padrao `novo or velho` ja e' o idioma da
casa deles.

🔴 **Escopo, decidido pelo dono em 2026-08-30: o consumidor desta migracao e' o `consumer-b`, so' ele.**
O `consumer-a` fica para depois — a tabela ja foi mandada a ele, mas **nada aqui espera por ele**, e o
contador que autoriza apagar o apelido conta o `consumer-b`. *Dois consumidores em migracao ao mesmo
tempo dobram a janela de convivencia sem dobrar o aprendizado.*

**Entao o plano, em quatro passos, e o gateway so' precisa de MEIO tradutor:**

1. **Consumidores tornam os leitores tolerantes** (`novo or velho`) — barato, e' o padrao que eles ja
   usam. Continuam **escrevendo** em portugues.
2. ✅ **FEITO (T-203, `v0.61.0`, 2026-08-31): gateway passa a ACEITAR os dois nomes na entrada** e
   traduz as tags para portugues por dentro, por POSICAO, nas 30 chaves de direcao ENTRADA de
   `docs/MIGRACAO-CONTRATO-EN.md` — Request (`POST /v1/messages`, com os 4 objetos aninhados e
   `botoes_template[]`), `POST /v1/templates`, `POST /v1/cadastro`, `POST /v1/pausa`,
   `POST/DELETE /v1/bloqueios`, `POST /v1/leituras`, `POST /v1/fumaca`. Saida continua em
   portugues. Idempotencia provada atravessando idiomas (`TestEntradaIdempotencyCrossesLanguages`).
   Contador `config.CounterOldNameUsed` no ar em `/v1/estado` — mas so' em 4 das 7 rotas
   (send/templates/leituras/fumaca ja tinham `*config.Counter` plugado; cadastro/pausa/bloqueio
   aceitam o apelido em ingles e ainda NAO contam, por falta desse fio — ver `docs/CHANGELOG.md`).
3. **Consumidores trocam os escritores para ingles.** Nada quebra: o gateway aceita os dois.
4. **Gateway vira a saida para ingles, num commit.** Nada quebra: os leitores sao tolerantes desde o
   passo 1. **Depois, apaga o apelido de entrada** — e ai sim e' MAJOR, que para e pergunta ao dono.

**O que isso economiza em relacao ao desenho anterior:** nao existe saida duplicada, nao existe corpo
inchado, nao existe regra de duplicacao em objeto aninhado. Sobra **uma tabela de apelidos so' na
entrada**, viva por dias, apagada num commit.

### 🔴 As armadilhas que continuam valendo

1. **Apelido por POSICAO, nao por nome global.** Um mapa `tipo -> kind` aplicado recursivamente
   tambem reescreve o `tipo` **dentro de um objeto de passagem da Meta**, que nao e' nosso.
2. **O `cru` nao se toca** — base64 dos bytes EXATOS da Meta, com teste provando saida byte a byte
   igual.
3. **Os dois nomes no MESMO pedido e' `400`**, nomeando o conflito. "O ultimo vence" e' o defeito que
   aparece em producao seis meses depois.
4. **A idempotencia e' calculada sobre a forma CANONICA.** Se o hash for do corpo cru, o mesmo pedido
   escrito em PT e em EN gera hashes diferentes — e a mesma mensagem sai **duas vezes** para a
   cliente. Traduza primeiro, calcule o hash depois.
5. **O contador do nome velho vive no apelido de entrada**, por consumidor. E' o numero que autoriza
   o passo 4 — *"'se estiver ok, remover' precisa de um numero, nao de uma impressao"* (dono,
   2026-08-20).

### Verify

- Pedido em portugues e o mesmo em ingles produzem **resposta identica**.
- `cru` byte a byte igual nos dois caminhos.
- Conflito devolve `400` nomeando a chave.
- **Idempotencia atravessa idiomas:** mesmo pedido em PT e EN, mesma `Idempotency-Key`, mesmo
  `wa_message_id`, **um** envio.
- Contador do nome velho sobe por consumidor e aparece no `/v1/estado`.

### Fora do escopo, de proposito

**CLI e `ZAPGW_*` sao camada 3 e quebram o OPERADOR** — e o risco nao e' o rename, e' a variavel no
`/etc/zapgw/env`, que o rename nao alcanca: o gateway sobe com o default, em silencio.
