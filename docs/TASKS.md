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

## [ ] T-201  The pre-push gate looks inside merge commits too
Why:    A T-199 declarou o buraco no proprio codigo (`filesChangedInCommit`): **`git diff-tree` sem
        `-m`/`-c` devolve diff VAZIO para commit de merge**, entao conteudo introduzido **apenas numa
        resolucao de merge** atravessa o portao sem ser olhado.
🔴      **Buraco declarado nao e buraco coberto** — esta e' a terceira vez em 24 h que essa frase
        aparece aqui, e nas duas anteriores ela cobrou: o nome de cliente foi para o repositorio
        publico com o aviso escrito ao lado, e o portao de branch nova nasceu impedindo o caminho
        legitimo. **Declarar honestamente e' o comeco do conserto, nao o conserto.**
        E o caso nao e' teorico: resolver conflito e' exatamente o momento em que alguem cola um
        trecho "so' para funcionar" e esquece.
Files:  internal/config/prepush_test.go
        .githooks/pre-push  (se precisar)
        docs/ARMADILHAS.md, docs/ARMADILHAS.pt-BR.md

Do:
  1. **Faca o portao enxergar o conteudo introduzido por um merge.** `git diff-tree -m` (contra cada
     pai) ou `-c`/`--cc` (so' o que difere de TODOS os pais) sao os caminhos; **`-c` e' o que responde
     a pergunta certa** — "o que este merge introduziu que nao vinha de nenhum pai" —, porque com `-m`
     um merge limpo repete todo o conteudo do outro ramo e a varredura explode de tamanho.
  2. **Meça antes de escolher:** rode os dois contra um merge real deste repositorio e diga quantos
     arquivos cada um devolve. **Escolha com o numero na mao**, e escreva o numero no comentario.
  3. **Se `-c` puder esconder algo** (conteudo que veio de um pai mas que aquele pai ja tinha sido
     varrido... ou nao), diga isso em voz alta em vez de assumir. **O portao pode varrer a mais; nunca
     a menos.**
  4. **Atualize o comentario que declara o buraco** — ele passa a descrever o que o portao faz agora.
     Comentario que descreve limitacao ja consertada e' doc falso.
  5. **Entrada em `docs/ARMADILHAS.md` + par pt-BR** so' se a entrada existente da T-200 nao cobrir;
     se cobrir, **acrescente uma linha** a ela em vez de criar entrada nova.

Verify:
  - 🔴 **CONTROLE COM MERGE, e a saida tem de citar a agulha:** num bare descartavel, crie dois ramos,
    ponha a agulha **apenas na resolucao do merge** (nao existe em nenhum dos dois pais), e confirme
    que o push **BLOQUEIA nomeando o commit de merge e o arquivo**. Cole a saida.
    *Se a mensagem disser "nao consegui verificar", o controle nao provou nada — foi esse exato erro
    que o planner cometeu na T-199.*
  - **Controle negativo:** um merge limpo (sem agulha) **passa**, e em quanto tempo. Se `-m` fizer o
    tempo explodir, esse numero e' a justificativa da escolha do item 2.
  - **Nunca empurre branch de controle para o `origin`; nunca use `--no-verify`.** Apague os ramos e o
    bare no fim; `git branch` sem sobra e `git status --short` limpo.
  - `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` limpos.

## [ ] T-202  The migration table becomes a versioned document, with DIRECTION and what does NOT change
After:  T-201
Why:    A tabela de migracao do contrato existe **so' dentro do canal** com um consumidor. A doutrina
        do canal diz o contrario: *o que vale para TODOS sai do canal para o documento duravel* — e
        esta tabela vale para todo consumidor, inclusive o `consumer-a`, que ainda nem comecou.
🔴      **E ela ja quebrou um consumidor por omissao:** listava `midia_id -> media_id` sem dizer em que
        DIRECAO valia. O tradutor do consumidor renomeou a resposta do `POST /v1/media` — que ja estava
        em ingles — e o upload de midia parou. *Tabela sem direcao mente por omissao em toda chave que
        aparece nos dois sentidos.*
        **E a ausencia tambem mente:** quem le uma tabela que so' lista renomeacoes conclui que o que
        nao esta nela e' portugues. Ha pelo menos 25 chaves nossas de SAIDA **ja em ingles** hoje
        (`docs/INVENTARIO-CHAVES.md`), e cada uma e' outro `media_id` esperando.
Files:  docs/MIGRACAO-CONTRATO-EN.md  (novo)
        docs/MIGRACAO-CONTRATO-EN.pt-BR.md  (novo, o par)

Do:
  🔴 **Esta tarefa NAO decide nome nenhum.** Ela MONTA o documento a partir de duas fontes que ja
  existem. Onde o par em ingles ainda nao foi decidido, escreva literalmente `A DECIDIR` — o planner
  preenche depois. **Inventar um nome aqui vira quebra em producao do lado do consumidor.**

  1. **Fonte A — as 89 chaves ja decididas:** estao na secao de `2026-08-30 23:05` do arquivo
     `C:/dev/<consumidor-b>/zapgw-STATUS.local.md` (resolva o diretorio com
     `ls C:/dev/*/zapgw-STATUS.local.md` — o nome real nao entra neste arquivo, que e publico).
     Procure a secao cujo titulo comeca com `# 🔤🔴` e a data `2026-08-30 23:05`. Copie os
     pares **verbatim**. 🔴 Esse arquivo carrega telefone real e nome de cliente: **copie apenas as
     linhas da tabela de chaves**, nada de prosa, nada de exemplo de payload.
  2. **Fonte B — as 29 chaves medidas:** `docs/INVENTARIO-CHAVES.md`, com `arquivo:linha` e direcao.
     Coluna de ingles = `A DECIDIR`.
  3. **Coluna DIRECAO obrigatoria em toda linha** — `SAIDA-EVENTO`, `SAIDA-RESPOSTA`, `ENTRADA`, ou
     mais de uma. Para as 89, se a direcao nao estiver na fonte, **meça no codigo**; se nao conseguir,
     escreva `A MEDIR` — nunca chute.
  4. **Secao propria e destacada: "O que NAO muda"**, com as chaves de saida que ja estao em ingles
     (a lista do item 4 do inventario), cada uma com `arquivo:linha`. Com uma frase dizendo que a
     ausencia de uma chave desta tabela **nao** significa que ela vira ingles.
  5. **Traga a regra de colisao do consumidor, com credito:** *so' renomeia se o nome de destino ainda
     nao existir naquele dicionario* — porque o nosso ingles para `texto` e' `text`, e `text` tambem e'
     nome da Meta dentro de um objeto de mensagem; idem `category`.
  6. **Cabecalho `Código:` no topo dos dois arquivos**, como toda doc de subsistema deste repo.
  7. Os dois arquivos (EN e pt-BR) nascem **juntos**.

Verify:
  - Contagem: 89 + 29 = 118 linhas de chave (ou a diferenca explicada, chave por chave — pode haver
    sobreposicao entre as fontes, e nesse caso a linha e' UMA, com as duas origens citadas).
  - **Nenhuma linha sem direcao.** `grep` por linhas de tabela sem uma das tres palavras: zero.
  - Amostra de 5 `arquivo:linha` conferida com `sed -n`.
  - 🔴 `go test ./...` verde — os dois portoes varrem `docs/`, entao **se voce copiar qualquer coisa
    identificavel do canal, eles reprovam**. Isso e' a rede, nao a permissao para copiar sem olhar.
  - `gofmt -l cmd internal` sem saida.

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
