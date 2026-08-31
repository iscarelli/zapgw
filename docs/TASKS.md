# Tasks

## Em voo — retomar por aqui

> Bloco de retomada. **Cada item sai daqui quando for resolvido**; ele nao e' historico.
> Escrito ao fim de 2026-08-30, o dia em que o repositorio virou publico. Bloco de retomada
> mentindo e' pior que bloco nenhum: e' o primeiro texto que a proxima sessao le.

🔴 **DECIDIDO PELO DONO (2026-08-31): o repositorio publico vai ser APAGADO E RECRIADO, e a arvore
tem de estar limpa ANTES.** Ele lembrou, com razao, que ja tinha pedido cuidado com dado de cliente
varias vezes — isto nao era menu aberto.
- **Por que apagar e nao reescrever:** so o apagar leva embora tambem os objetos soltos, que um
  `push --force` deixa alcancaveis por SHA. Publico ha ~1 dia, **0 forks, 0 estrelas** — nao ha
  colateral de terceiro.
- **A ORDEM importa, e o repositorio novo nasce de UM commit:** o que nao estiver limpo na hora fica.
  1. **T-193** constroi o portao de NOME e limpa a arvore no mesmo commit.
  2. **T-194** poda de snapshot.
  3. **So entao** apagar e recriar o repositorio.
- 🔴 **O que a recriacao leva junto, e nao volta:** o release `v0.60.1` e os binarios anexados, as
  tags, as issues, e **os segredos de repositorio da CI**. O segredo `ZAPGW_FORBIDDEN_NAMES` tem de
  ser recriado DEPOIS, no repositorio novo — antes disso a CI nao consegue rodar o portao de nome, e
  o portao **falha dizendo que nao conseguiu verificar**, que e o comportamento certo.
- **Portao de nome: opcao (a), decidida pelo dono.** A agulha mora fora do repositorio, em
  `~/.zapgw/forbidden-names.txt` (ja criado e preenchido) ou na variavel `ZAPGW_FORBIDDEN_NAMES`.
  Sem uma das duas, o teste **falha** — nunca pula.
- ⚠️ **Medido em 2026-08-31: a arvore NAO estava limpa** mesmo depois da T-192. Nome de cliente
  seguia em `docs/META-CAMPOS-DE-WEBHOOK.md` (+ par), `docs/ARMADILHAS.md` (+ par),
  `cmd/zapgw/perdidas_test.go` e `internal/config/forense_test.go`. **O que achou foi um `git grep`
  manual, nao um teste** — e e por isso que a T-193 existe.


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

📌 **Snapshot `pre-update` acumula no CT a cada deploy** (um por deploy, visto em `pct listsnapshot`).
Ninguem poda. Vale virar tarefa antes de virar problema de disco.

## Active

> A fila do periodo privado esta em `iscarelli/zapgw-dev`, congelada. Tarefa nova nasce aqui.

## [ ] T-194  The deploy prunes its own pre-update snapshots
Why:    **Decisao do dono, 2026-08-31:** manter os 3 ultimos e podar no proprio `deploy.sh`. Hoje um
        snapshot `pre-update` se acumula no CT a cada deploy e ninguem poda — vira problema de disco
        sem avisar ninguem.
Files:  implanta/deploy.sh

Do:
  - Depois de o deploy **ter dado certo** (nunca antes, nunca se ele reverteu), remover os snapshots
    `pre-update` mais antigos, mantendo os **3 mais recentes**.
  - **Apagar snapshot e irreversivel: case o filtro no PREFIXO exato** dos snapshots que o proprio
    script cria, e nunca num padrao amplo. Snapshot criado por um humano, com outro nome, **nao pode**
    entrar na poda.
  - Se a listagem falhar ou devolver algo inesperado, **nao apague nada** e diga isso na saida — poda
    as cegas e pior que nao podar.
  - Registrar na saida do deploy quantos foram removidos e quais ficaram.

Verify:
  - **Ensaio sem apagar:** rode a parte de poda em modo de listagem contra a lista real de snapshots
    do CT e confirme que ela **seleciona exatamente os que sobram do 3-mais-recentes** e nenhum
    outro. Cole a lista de entrada e a selecao.
  - Confirme que um snapshot com nome fora do prefixo **nao** aparece na selecao.
  - `bash -n implanta/deploy.sh` sem erro.

## [ ] T-189  O contrato passa a falar ingles — leitor tolerante do lado deles, apelido so' na ENTRADA
🔴      **BLOQUEADA por terceiro:** nada aqui comeca antes do passo 1 do consumidor (leitores
        tolerantes). Ver o bloco de retomada no topo deste arquivo.
Why:    **decisao do dono, 2026-08-30:** *"o projeto precisa ser em ingles, ter feito em portugues foi
        errado. Se a chave chama nome, tem que passar a se chamar name."*
        **Medido no mesmo dia:** 89 chaves `json` nossas com termo em portugues (de 299), **7 rotas**
        e **18 nomes do vocabulario fechado de contadores**.
🔴      **Os 18 contadores sao VALORES no JSON, nao tags** — nenhuma varredura por `json:"` os acha.

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
