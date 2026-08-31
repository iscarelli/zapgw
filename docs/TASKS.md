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

⏳ **ESPERANDO O CONSUMIDOR: a medicao de DEPOIS da migracao.**
🔴 **Ela foi pedida as 23:52 NO ARQUIVO ERRADO** — a seção foi escrita em
`C:\dev\zapgw\<consumidor-b>-STATUS.local.md`, que e' o arquivo DELES. O monitor deles le o nosso.
**Ate 2026-08-31 00:02 o consumidor nao tinha o pedido**, e do lado dele nos tinhamos avisado que o
gateway ia piscar e sumido em seguida. **Reenviada as 00:02 no arquivo certo**
(`C:\dev\<consumidor-b>\zapgw-STATUS.local.md`), com o custo registrado em
`github/docs/CANAL-ENTRE-SESSOES.md`.
- E' o mesmo roteiro da linha de base de 2026-08-30 20:31 UTC — template `sistema_alerta` (UTILITY),
  depois texto livre, mais a conferencia da entrega no `callback_url` deles e o `status` do `wamid`.
- **A comparacao so' vale porque a metade de ANTES foi medida antes de qualquer coisa mudar**; se ela
  nao chegar, o par nao fecha e a migracao continua sem prova de trafego real.
- ⏰ **A janela de 24 h aberta as 20:32 UTC vence 2026-08-31 20:32 UTC (17:32 -03)** — depois disso o
  passo do texto livre precisa de template de novo, e o roteiro deixa de ser identico ao de ANTES.

⏳ **ESPERANDO O CONSUMIDOR: o passo 1 da T-189** (leitores tolerantes, `novo or velho`). Nada aqui
comeca antes disso — e' o unico passo que bloqueia o resto. Registrado la como **[1350]**, **ainda
nao comecado**: eles estao num desenho de cobranca que o dono deles priorizou. Nao ha nada a cobrar.

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

## [ ] T-200  The pre-push gate must not make the legitimate path impossible
Why:    **Medido pelo planner em 2026-08-31, com push de verdade contra um repo bare descartavel:** o
        hook da T-199 **bloqueia TODA primeira push de branch nova**, inclusive uma branch limpa, com
        *"sha remoto = zeros — nao ha base segura"*. Nao existe forma de estabelecer a base: a push
        que criaria a ref e' a mesma que e' recusada.
🔴      **O efeito nao e' "portao rigoroso", e' portao que FORCA o desvio.** Se o unico caminho para
        empurrar uma branch nova e' `git push --no-verify`, entao a pratica que o projeto ensina e'
        desligar o portao — e quem desliga uma vez desliga de novo, inclusive no dia em que havia
        agulha. *Falha fechada que torna o caminho legitimo impossivel nao protege: ela treina o
        bypass.*
        **A push do `main` funciona** (ref ja existe no `origin`, intervalo calculavel) — em 0,68 s.
        O defeito e' so' no caso da ref nova, e e' o caso de toda branch de trabalho.
Files:  .githooks/pre-push
        internal/config/prepush_test.go
        docs/ARMADILHAS.md, docs/ARMADILHAS.pt-BR.md

Do:
  1. **Quando o sha remoto for zeros, calcule a base sem adivinhar:** o conjunto introduzido por esta
     push e' `git rev-list <sha-local> --not --remotes` — os commits alcancaveis pela ref nova e **nao**
     alcancaveis por nenhuma ref de rastreamento remoto. E' exatamente "o que esta push acrescenta ao
     remoto", e' computavel, e nao depende de escolher um branch de referencia.
  2. 🔴 **Continua falhando fechado onde de fato nao da para saber.** Se `--not --remotes` nao puder
     ser calculado, ou se o repositorio nao tiver remoto nenhum, **varra todos os commits alcancaveis**
     — mais lento e seguro — em vez de liberar. **Nunca troque "nao consigo calcular" por "deixa passar".**
  3. **Teste em `internal/config/prepush_test.go`** cobrindo os dois casos novos:
     - branch nova e limpa: push **passa**;
     - branch nova com agulha num commit que um commit posterior apaga: push **bloqueia**, nomeando o
       commit e o arquivo.
  4. **Entrada nova em `docs/ARMADILHAS.md` + par pt-BR**, marcada com o fogo (ja cobrou), no mesmo
     commit: *falha fechada que impede o caminho legitimo nao e' rigor, e' um bypass ensinado.* Diga o
     custo real: o portao nasceu impedindo toda branch nova, e o unico caminho restante era o
     `--no-verify` que ele mesmo desaconselha.
  5. ⚠️ **Registre tambem, na mesma entrada, o erro de METODO que apareceu junto:** o primeiro controle
     positivo do planner "passou" — o push foi bloqueado — **mas pelo motivo errado** (sha zerado, nao
     agulha encontrada). Bloqueio observado nao prova que o portao ACHA: prova que ele recusou.
     *Controle que nao distingue POR QUE reprovou valida o instrumento, nao o dado.*

Verify:
  - **Branch nova e limpa empurra**, contra um repo bare descartavel — cole a saida e o tempo.
  - **Branch nova com a agulha em commit A e apagada em commit B: BLOQUEIA nomeando o commit A e o
    arquivo.** Cole a saida. 🔴 **Confirme que a mensagem cita a agulha encontrada, e nao "nao consegui
    verificar"** — e' a diferenca entre provar que o portao acha e provar que ele recusa.
  - **Sem remoto nenhum:** o hook varre tudo em vez de liberar. Cole a saida.
  - `git ls-remote` no bare descartavel confirmando que nada foi empurrado nos casos bloqueados.
  - 🔴 **Nunca empurre a branch de controle para o `origin`, e nunca use `--no-verify`.**
  - `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` limpos.

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
2. **Gateway passa a ACEITAR os dois nomes na entrada** e traduz as tags para ingles por dentro.
   Saida continua em portugues. *Aditivo: MINOR.*
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
