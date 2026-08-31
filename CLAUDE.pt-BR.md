# CLAUDE.md — zapgw

*[Read in English](CLAUDE.md)*

Gateway para as APIs de mensagem da Meta (WhatsApp Cloud API e Instagram DM) — multi-inquilino,
binario estatico.

Criado em 2026-08-20.

## O que este repositorio e — e a regra que ele existe para cumprir

**Ele nasceu vazio, de proposito, em 2026-08-20.** E o sucessor limpo de `iscarelli/zapgw-dev` — o
mesmo projeto, renomeado no mesmo dia, com 692 commits e 72 tags de historico. A decisao, as tres
alternativas e o custo medido de cada uma estao em
`zapgw-dev:docs/ESTUDO-ABERTURA-PUBLICA-2026-08-20.md` (Caminho A).

> **Nada que identifique uma pessoa ou um cliente real entra aqui.** Nem o telefone do dono, nem
> nome de inquilino, nem `phone_number_id` / `waba_id` / `ig_id` de terceiro, nem endereco do
> homelab. Exemplo e fixture usam valor **sintetico**.

*Por que isso e a primeira secao e nao uma nota de rodape:* o `zapgw-dev` carrega o telefone do dono
em **36 arquivos** e nomes de cliente real em **34** (recontado em 2026-08-20), espalhados por 692
commits — e **nao existe despublicar**. Foi esse custo que fez o projeto nascer de novo em vez de
reescrever o historico. Uma unica ocorrencia que entre aqui por descuido reproduz o problema inteiro,
e o repo perde a unica propriedade que justifica ele existir.

## As quatro decisoes da abertura publica — tomadas em 2026-08-20

Nenhuma delas e minha para reabrir. O raciocinio inteiro e o custo medido de cada alternativa estao
em `zapgw-dev:docs/DECISAO-ABERTURA-PUBLICA-2026-08-20.md`; aqui fica so o que vale como regra.

| | decisao | consequencia pratica |
|---|---|---|
| **Licenca** | **AGPL-3.0-or-later** (`LICENSE`) | o produto e servidor: a clausula de rede e o ponto. O dono e titular unico, entao relicenciar depois continua possivel — o caminho AGPL -> permissiva existe, o inverso nao. MIT foi recusado por ser irreversivel. |
| **Idioma** | o codigo vai para **ingles**; documentacao em **PT-BR e EN** | os documentos deste repositorio ganharam par `NOME.md` (EN) + `NOME.pt-BR.md` (PT) em 2026-08-30. ✅ **O código chegou em 2026-08-30, já em inglês** — 3818 declarações renomeadas numa única passada mecânica no repositório privado, ANTES de migrar, de modo que nenhum identificador em português chegou a ser commitado aqui. |
| **Terceiros** | **nada de consumidor** vai ao publico | e a razao de este repo existir em vez de o antigo virar publico. |
| **Nome** | **continua `zapgw`** | foi para isso que o repo antigo virou `zapgw-dev`: liberar o nome. |

🔴 **A ordem e obrigatoria, e o motivo e mecanico:** *nome fechado -> passada mecanica unica
(rename + ingles) -> tornar publico*. Depois de publicado, o nome esta no binario que o
`update_script` procura, no caminho de config e na unit — troca-lo **quebra a instalacao de todo
mundo, em silencio, no update**. O nome ja esta fechado; falta a passada.

✅ **2026-08-30 — o gatilho da virada publica esta definido, e e um EVENTO, nao uma data.** Decisao
do dono: *"trabalhar na migracao do codigo; quando subir o primeiro release la, transforma em
publico."* A arvore higienizada migra para ca **enquanto este repo ainda e privado**, o primeiro
release sai daqui, e **so entao** ele vira publico.
🔴 **A consequencia que manda em tudo:** o historico DESTE repositorio nao sera reescrito, e sera
publicado como esta. Entao **nenhum commit com nome real de cliente ou comentario em portugues pode
chegar aqui** — a higienizacao e a traducao acontecem no `zapgw-dev`, ANTES da migracao, e o
primeiro commit de codigo daqui ja nasce limpo. *Aqui nao existe "corrigir no proximo commit": o
commit errado fica.*

**Estado hoje, medido em 2026-08-31: o código está aqui, o repositório é PÚBLICO, a `v0.60.1` está
lançada, e a CI roda verde aqui.** A stack é Go (binário estático, `CGO_ENABLED=0`), herdada do
`zapgw-dev`.
🔴 **O histórico que você vê começa em 2026-08-31, e isso é proposital:** o primeiro histórico
publicado carregava o nome real de um cliente, então o repositório foi **apagado e recriado** em vez
de receber um `push --force` — apagar é a única coisa que leva junto também os objetos inalcançáveis.
O motivo está escrito no commit de gênese, não escondido.

## Regras duras — e o que faz cada uma FALHAR

Este repositório nasce depois de treze meses de projeto concentrados em um mês, e a razão de ele
existir separado é não repetir o que já custou caro no `zapgw-dev`. Então as regras abaixo não vêm de
princípio: **cada uma tem um custo real atrás, e cada uma tem uma coluna dizendo o que a faz falhar.**

🔴 **O critério que manda aqui, e ele é a lição mais cara de 2026-08-20:** *regra sem mecanismo é
decoração, e mecanismo que nunca reprovou é indistinguível de mecanismo que não olha.* Naquele dia,
**três medições foram feitas com "controle positivo" e duas estavam erradas** — as duas validavam o
instrumento em vez do dado. Quem pegou as duas foi um teste que reprovava de verdade.

**Portanto, para valer como mecanismo aqui, um teste precisa ter reprovado uma vez, contra dado
real, e isso precisa estar registrado.**

| regra | o que a faz falhar | estado |
|---|---|---|
| Nenhum dado que identifique pessoa real (telefone, nome de cliente, id da Meta de terceiro, endereço interno) | **dois portões de allowlist** que varrem **todo arquivo que o `git` enxerga** a partir da raiz do módulo (rastreados **mais** não-rastreados-e-não-ignorados, T-191), um para telefone — **decodificando o base64** dentro de todo `wamid.` — e outro para nome de cliente (T-193) | ✅ **aqui, e a lista de isenções de telefone está VAZIA** (`internal/config/telefones_allowlist_test.go`, `internal/config/nomes_allowlist_test.go`). O de telefone já reprovou contra dado real **três** vezes — duas em 2026-08-30 (número num arquivo temporário; outro escondido em base64 dentro de um bloco de código markdown) e uma em 2026-08-31 (arquivo criado na raiz do repositório, o controle positivo da T-191). O de nome nasceu reprovando: encontrou **25 ocorrências em 6 arquivos** na própria árvore. ⚠️ **Falta portão para id da Meta de terceiro** |
| TLS não tem modo desligado, em nenhum sentido | teste que varre toda a árvore atrás da opção, com a agulha montada por concatenação para não acusar a si mesmo | ✅ **aqui** (`internal/inbound/deliver_test.go`, 24 testes) e roda na CI em passo próprio (`portao de TLS`). O código migrou em 2026-08-30; esta célula dizia "migra com o código" até 2026-08-31, que é a família de afirmação envelhecida descrita no item 1 abaixo |
| Rota nova declara isolamento por inquilino | tabela de isolamento que lê os `mux.HandleFunc` do pacote e exige literal declarado | ✅ **aqui** — `internal/outbound/isolamento_test.go:407` lê por regexp os registros `mux.HandleFunc(...)` dos arquivos não-teste do pacote e exige que cada rota carregue um literal declarado |
| Handler declara quais tipos de instância aceita | parâmetro **posicional obrigatório**: omitir **não compila**, e o valor zero é o mais restritivo | ✅ **aqui** — `internal/outbound/tipos.go` (`AcceptedTypes`, T-111): todo construtor `outbound.New*Handler` recebe como ÚLTIMO argumento posicional (ex.: `NewReadsHandler`, `internal/outbound/leituras_handler.go:65`). O valor zero, `WhatsAppOnly` (`iota` = 0), rejeita Instagram em vez de aceitar tudo — um valor zerado por acidente falha fechado. **Provado em 2026-08-31:** removi `outbound.WhatsAppOnly` da chamada de `NewReadsHandler` em `cmd/zapgw/main.go:430` e rodei `go build ./...` — `not enough arguments in call to outbound.NewReadsHandler … want (…, outbound.AcceptedTypes)`; restaurado, `git diff` vazio. A T-197 resolveu isto: a varredura anterior que "não achou" olhou no lugar errado — o parâmetro mora em `internal/outbound/`, não num arquivo com o nome da regra |
| O verify roda antes de todo merge | **CI** com os quatro comandos em passos separados, `timeout-minutes: 10`, e nenhum passo usando cano para `grep` | ✅ **existe** (`.github/workflows/verify.yml`), carrega os dois portões irreversíveis, cada um em passo próprio, com `ZAPGW_FORBIDDEN_NAMES` declarado como `env:` no nível do JOB para que tanto o passo do portão quanto o `go test ./...` inteiro o enxerguem (T-195). 🔴 **E ela JÁ RODOU aqui, medido em 2026-08-31:** quatro execuções no repositório recriado — as três primeiras **falharam** no portão de nome ("não consegui verificar", sem fonte de agulha) e a quarta passou com os quatro portões verdes (run `33355320803`). ⚠️ **A história de que a cota de Actions estava esgotada até 2026-09-01 morreu:** as execuções aconteceram em 08-31 |
| Segredo nunca no repositório | `.gitignore` desde o commit inicial; hook por projeto | parcial — o `.gitignore` está aqui. **Agora existe um hook de pre-push** (`.githooks/pre-push`, T-199) — mas cobre algo mais estreito que "segredo em geral": ele bloqueia os dois portões de dado pessoal (telefone, nome de cliente) sobre o INTERVALO sendo empurrado, não só a árvore final, então um número que um commit posterior apaga ainda é pego antes de chegar ao `origin`. Ele **não** cobre um token ou chave de API num commit — isso continua sendo só trabalho do `.gitignore`. Ative com `git config core.hooksPath .githooks` (comando por clone — nada commita esse estado); confirme com `git config --get core.hooksPath` |
| Doc de subsistema começa com `Código:` | **nada** | 🔴 **não existe** |
| O estado publicado (versão no ar, o que está em voo) não mente | **nada** | 🔴 **não existe** — a linha equivalente no `zapgw-dev` mentiu **duas vezes em dezesseis horas**, a segunda depois do aviso escrito logo abaixo dela |

### As três fundações — duas delas já têm mecanismo, e uma continua sem

1. **CI — ela existe aqui, ela roda aqui, e já reprovou contra dado real.**
   Quatro execuções em 2026-08-31, no repositório recriado: as três primeiras **falharam** no portão
   de nome (*"não consegui verificar"* — nenhuma fonte de agulha chegou ao runner), e a quarta passou
   com os quatro portões verdes (run `33355320803`, ~1m30s). Essa ordem importa mais que o verde: um
   portão que nunca recusou nada é indistinguível de um portão que não olha, e o mesmo vale para a
   esteira que o carrega.
   Ela existiu primeiro no `zapgw-dev`, de 2026-08-21 a 2026-08-29 — quatro passos separados, a
   versão do Go lida do `go.mod`, e um passo de `gofmt` que transforma saída não-vazia em erro, já
   que sozinho o `gofmt -l` imprime e sai `0`. Voltou aqui restaurada de
   `git show 709e915:.github/workflows/verify.yml`, e ganhou os dois passos de portão.
   🔥 **Este parágrafo já mentiu três vezes, cada uma de um jeito, e é por isso que o registro fica.**
   (1) Disse *"feito no `zapgw-dev` e migra com o código"* depois de o arquivo já ter sido apagado
   lá — **afirmação sobre o estado de OUTRO repositório, que envelhece sem avisar ninguém**.
   (2) A correção trocou isso por *"repo privado paga Actions; regra do dono: CI só em projeto
   público"* — **causa inventada com fantasia de regra**, que ainda sustentou uma recomendação de
   antecipar a abertura pública para "ganhar CI de graça". O dono corrigiu em 2026-08-30: era cota
   estourada por outro projeto, com data de reset.
   (3) E essa data de reset foi escrita aqui como fato — *"volta em 2026-09-01"* — e repetida no
   `docs/TASKS.md` em 2026-08-31 como *"a cota só reseta em 09-01"*. **As execuções aconteceram em
   08-31.** Ninguém re-mediu a capacidade; a data veio da explicação do dono e endureceu em estado.
   ➡️ *A família é uma só: **afirmação sobre coisa que você não controla envelhece calada.** Estado de
   outro repositório, causa inventada, janela de cota — nenhuma delas falha alto quando deixa de ser
   verdade. Escreva o sintoma, a data, e como foi medido.*
   *Vale guardar, porque nunca foi explicado:* uma tentativa anterior de Actions nesta conta
   (2026-08-06) morreu duas vezes — job `cancelled`, campo `runner` vazio e **exatamente 905
   segundos** nas duas. As execuções de 2026-08-21 terminaram em ~2 minutos. 🔴 **O que destravou
   nunca foi descoberto; só foi medido que funcionou naquele dia.**
2. **Os portões de dado pessoal rodam na CI**, e não apenas como teste local — porque aqui o custo é
   irreversível: publicado uma vez, não existe despublicar. Cada um tem passo próprio
   (`portao de dado pessoal`, `portao de nome`), e as agulhas do portão de nome chegam ao runner como
   o segredo de repositório `ZAPGW_FORBIDDEN_NAMES`, declarado como `env:` no nível do **job** para
   que o `go test ./...` inteiro também o enxergue.
   ⚠️ **PR vindo de fork não recebe segredo**, então o portão de nome falha lá dizendo que não
   conseguiu verificar. Isso é proposital, e o workflow diz isso em comentário: transformar em skip
   compraria um sinal verde com exatamente a cegueira que o portão existe para impedir.
3. **Um estado que se prova.** 🔴 **Continua sem mecanismo.** Qualquer linha deste repositório que
   afirme "a versão X está no ar" ou tem de ser medida na hora, ou não deve ser escrita. *Bloco de
   retomada que mente é pior que bloco nenhum: é o primeiro texto que a próxima sessão lê.* O item
   acima é o argumento deste — três mentiras, todas em texto que parecia assentado.

### O que NÃO é regra dura, e por que dizer isso importa

Estilo, gosto e preferência não entram nesta lista. Ela fica curta de propósito: uma lista de regras
duras com trinta itens é uma lista que ninguém lê, e a primeira pessoa com pressa a abandona inteira.
**Se uma regra não tem custo real atrás nem mecanismo na frente, ela não é dura — é preferência, e o
lugar dela é na revisão, não aqui.**

## Verify commands

    CGO_ENABLED=0 go build ./...
    go test ./...
    go vet ./...
    gofmt -l cmd internal      # nao pode imprimir nada

**E' `gofmt -l cmd internal`, nao `gofmt -l .`** — e a diferenca nao e' estetica. Worktrees de
agentes implementadores ficam em `.claude/`, **dentro do repo**. O comando `go` ignora diretorio que
comeca com ponto; o `gofmt` **nao**. Com `-l .` ele lista arquivo meio-escrito de outro processo, e o
verify passa a acusar falso — que e' o jeito mais rapido de treinar alguem a ignorar a saida do
gofmt.

O `CGO_ENABLED=0` no build nao e' decorativo: o artefato deste projeto tem de ser **binario
estatico** (e' a propria razao de a stack ser Go), e sem fixar a variavel uma dependencia futura pode
ligar cgo em silencio, quebrando essa garantia sem que nenhum teste acuse.

**Rode o verify antes de commitar.** Se falhar, conserte ou nao commite — nunca commite com o verify
vermelho "para arrumar depois", e **nunca desabilite o teste que acusou**. Uma secao de verify que
ninguem executa e decoracao, e decoracao que descreve um mecanismo inexistente e doc falso.

### O que o verify NAO alcanca

Ele nao fala com a Meta e nao fala com consumidor nenhum. Toda garantia que dependa de rede real —
certificado valido no hostname publico, webhook chegando de fato, token aceito pela Graph API — e'
**estruturalmente inverificavel** por esta suite. **A suite verde nao substitui essas provas**, e a
unica coisa que as produz e' trafego real.

### 🔴 Os três testes que são um PORTÃO, e o que os torna diferentes dos outros

Eles não provam que o código funciona: provam que uma decisão irreversível não foi violada. Os três
**já reprovaram contra dado real**, e é por isso que valem como mecanismo.

- **`internal/config/telefones_allowlist_test.go`** — varre **todo arquivo que o `git` enxerga** a
  partir da raiz do módulo (todos os rastreados, mais todos os não-rastreados-e-não-ignorados —
  T-191, de modo que arquivo novo em qualquer lugar, inclusive na raiz do repositório, entra sem
  ninguém editar este teste) atrás de telefone não declarado, **decodificando o base64 de todo
  `wamid.`**, porque o `wamid` carrega o telefone do destinatário dentro dele e um `grep` pelo número
  como um humano o escreve passa limpo por cima.
  🔴 **A lista de isenções deste repositório está VAZIA, e tem de continuar assim.** No repositório
  privado de onde este código veio havia cinco, todas de documentos de registro que ficaram lá. Aqui
  não há caso combinado: uma isenção é um telefone real na internet, para sempre.
- **`internal/config/nomes_allowlist_test.go`** (T-193) — varre o mesmo conjunto de arquivos atrás de
  nome de cliente, sem distinção de caixa e em fronteira de palavra. Ele **não tem allowlist nem
  isenção por arquivo**: qualquer casamento é achado, ponto. A lista de agulhas **nunca mora no
  repositório** — este repositório é público, e escrever a lista de nomes proibidos dentro dele
  publicaria exatamente o que o portão existe para manter fora. Ela vem da variável de ambiente
  `ZAPGW_FORBIDDEN_NAMES`, ou de `~/.zapgw/forbidden-names.txt`, fora da árvore; se nenhuma das duas
  produzir ao menos uma agulha, o teste **falha dizendo que não conseguiu verificar** — mensagem
  deliberadamente diferente da de achado, para que os dois desfechos nunca virem a mesma cor.
- **`internal/inbound/deliver_test.go`** — varre a árvore atrás de qualquer forma de desligar a
  verificação de TLS, com a agulha montada por concatenação para o teste não acusar a si mesmo.

Duas propriedades que o verify precisa ter, e que so aparecem quando alguem tenta usar:

- **Nao pode mutar estado.** Rodar o verify duas vezes seguidas tem que dar o mesmo
  resultado e nao deixar rastro — sem subir versao, sem escrever no banco de producao,
  sem publicar nada. Se algum comando muta, diga isso na propria secao.
  Cuidado com o caso que se disfarca: um script que **confere e conserta** (repoe o cron
  que faltava, reenvia o alerta) nao e um verify, e uma rotina de remediacao. Rodar
  "so para checar" mexe em producao. Ou ele ganha um modo so-leitura
  (`--dry-run` / `NO_NOTIFY=1`) e esse modo vira o verify, ou a secao diz em letras
  garrafais o que ele altera a cada execucao.
- **Se o projeto tem deploy, o verify vai ate depois dele**: o que conferir para saber
  que subiu de verdade (status HTTP esperado, a versao aparecendo na pagina, e quanto
  tempo de erro e normal na janela de restart). Verify que para no build responde "o
  codigo compila", nao "a mudanca funciona".

## Regras de documentacao

Doc errado e pior que doc nenhum. Na duvida entre escrever algo que voce nao confirmou e
nao escrever nada: **nao escreva**.

- Todo doc de subsistema comeca com um cabecalho `Codigo:` listando os arquivos que
  descreve. Assim "que doc minha mudanca quebrou?" vira mecanico:
  `grep -rl "arquivo_que_toquei" docs/subsistemas/`
- Doc novo sem `Codigo:` no topo nao entra.
- **`Codigo:` aceita `host:caminho`, nao so caminho do repo.** Muita coisa que o sistema
  depende nao mora no repositorio: `o LXC do Traefik:/etc/traefik/traefik.yaml`,
  `host .16:/etc/cron.d/unifi-threats`. Sem essa forma, tudo que muda sem passar por
  commit fica invisivel a doc — e e justamente o que ninguem lembra de conferir. Quando
  nao houver codigo nenhum, escreva `Codigo: nenhum` com o motivo (ex.: "o inventario e
  estado vivo"), para a ausencia ter veredito.
- **Aposentar uma coisa e remover as DUAS pontas: o artefato e o gatilho.** Script
  apagado com o cron ainda apontando para ele, job removido com o alerta ainda
  configurado — o orfao sobrevive calado. Custo real ja pago neste workspace: um cron
  orfao por tres semanas.
- **Monitor cego que responde OK e pior que monitor nenhum.** Vale para qualquer
  verificacao: ela precisa distinguir *falhou* de *nao consegui verificar*. As duas
  viram "sem alarme" se ninguem separar, e a segunda e a que engana — voce acredita que
  esta coberto.
- `docs/SUBSISTEMAS.md`, quando existir, e leitura obrigatoria antes de grepar e antes de
  projetar — e essa ordem mora na PRIMEIRA linha do `CLAUDE.md`, nao no meio: so o
  `CLAUDE.md` entra sozinho no contexto do agente, o mapa nao entra.
- Um bug que uma armadilha teria evitado vira uma linha em `docs/ARMADILHAS.md`, **no
  mesmo commit do fix**, com o custo real que cobrou. E o unico momento em que o custo
  esta fresco.
- **Marque as que ja cobraram, separando-as das que so foram confirmadas.** Exigir custo
  real de toda armadilha tem um efeito perverso: quem encontrou o mecanismo mas nunca foi
  mordido por ele ou inventa um custo, ou nao escreve. Use uma marca visivel (🔥) para
  "ja custou, e o custo esta escrito" e deixe as demais sem marca, com o mecanismo
  confirmado no codigo e um "ainda nao cobrou". Assim as duas entram, e quem le sabe
  quais doem de verdade.
- **Ao registrar uma armadilha, procure as irmas antes de fechar.** Se o problema e um
  jeito de ler ou reescrever um dado (regex, replace, sed, parser), varra os outros
  lugares que tocam o MESMO dado e diga, na propria entrada, quais tem o buraco e quais
  nao tem — quem le a primeira vai perguntar isso de qualquer jeito. Um caso real: a
  armadilha era um `.replace()` que troca todas as ocorrencias; a varredura achou que o
  leitor da mesma constante usava `re.search` e devolvia a PRIMEIRA ocorrencia, um
  segundo buraco pelo mesmo motivo, e que o terceiro consumidor usava um `sed` ancorado
  o suficiente para estar a salvo. Uma armadilha documentada sozinha esconde as irmas.
- Doc que descreve o sistema nao tem data no nome. Data no nome autoriza o apodrecimento.
  ADRs e specs datadas sao historico: nao se atualizam.
- Confira cada afirmacao contra o codigo, nunca contra a doc antiga. Aponte `arquivo:linha`.
- Diga por que, nao so o que. Escreva os erros que aconteceram de verdade, com o prejuizo.

Para auditar/refazer a documentacao deste projeto, use o prompt de 5 fases em
`<raiz>/github/docs/PROMPT-auditoria-documentacao.md`. A doutrina resumida esta em
`<raiz>/github/docs/DOCUMENTACAO.md`.

**As regras acima sao um retrato da doutrina naquele arquivo, congelado na data de criacao
deste `CLAUDE.md` (no topo).** A fonte e la, nao aqui: em conflito, ela manda, e regra nova
chega por ela — este arquivo nao recebe atualizacao sozinho. Se o projeto viver sob
`C:\dev` / `~/dev`, o `CLAUDE.md` da raiz ja carrega as regras do workspace em todo sessao;
a copia acima existe para o caso de o projeto sair dessa arvore (entregue a outra pessoa,
clonado em outro lugar), quando ela e a unica coisa que sobra.

## Segredos — nunca no Git

Token, chave, senha, credencial ou certificado **nunca** entram no repositorio. Esta e a
unica regra aqui cuja violacao e irreversivel: commitado uma vez, o segredo vive no
historico para sempre, e a correcao nao e `git rm` — e **rotacionar o segredo**.

- Segredos moram em arquivos ignorados (`.env`, `secrets.*`, `*.key`, `.gh_token`),
  ignorados **desde o commit inicial**.
- O repositorio documenta, para CADA segredo, os tres: **qual** e, **onde mora** no disco,
  e **onde conseguir** um novo (painel tal, gerenciador de senhas, `secrets-transfer/`).
  Os dois primeiros sem o terceiro nao resgatam ninguem: quem clonou numa maquina nova
  descobre que falta um token e continua sem saber de onde tira-lo.
- O terceiro item muda de natureza conforme a origem, e confundir os dois trava gente a
  toa: **segredo emitido por terceiro** (painel do provedor) exige saber o caminho exato
  ate ele, porque so de la sai um valido; **segredo nosso, arbitrario** (um token que a
  gente inventou para servir de gate) nao tem origem a descobrir — o que ele precisa e da
  receita de troca: gere um novo, atualize aqui e aqui, reinicie o que le. Marcar um
  segredo nosso como "origem a confirmar" assusta sem motivo; o que faltava era a receita.
- Uma quarta coluna paga sozinha a tabela: **o que rotacionar QUEBRA** — o raio de
  explosao. Trocar um webhook secret sem re-registrar o webhook mata o vinculo em
  silencio; trocar a chave de assinatura torna lixo todo dado ja cifrado. Sem essa
  coluna, a rotacao parece uma operacao de um passo so, e nao e. Se o mesmo segredo
  aparece em varios lugares, diga em quantos e quais.
- Todo arquivo de segredo tem um companheiro `.example` versionado, com **placeholders
  literais** (`troque-pelo-token-do-x`), nunca valores reais nem parecidos com reais.
- Transporte entre maquinas e por `<raiz>/github/secrets-transfer/`, nao por commit, nao
  por chat.
- Se voce perceber que um segredo foi commitado: **pare tudo e avise o programador na
  hora**. Nao tente limpar o historico sozinho.

## Versionamento — SemVer

Este projeto usa **SemVer**: `MAJOR.MINOR.PATCH`.

- **MAJOR** — quebra compatibilidade. **Decisao de MAJOR sempre passa pelo programador
  antes de ser feita.** Nunca suba MAJOR por conta propria: pare, explique o que quebra e
  pergunte.
- **MINOR** — funcionalidade nova, compativel para tras.
- **PATCH** — correcao compativel para tras.

**Cada versao tem exatamente uma fonte.** Nao duplique o mesmo numero em dois arquivos —
duas fontes divergem. A fonte e um arquivo `VERSION` na raiz — o `go.mod` nao carrega versao do proprio modulo.

Isso NAO quer dizer que o projeto tenha um numero so. Se ele tem mais de uma **frente**
(superficies, apps ou servicos entregues separadamente), cada frente tem a **sua** versao,
independente — e a regra continua satisfeita, porque cada numero tem uma fonte. Ver
"Projeto com mais de uma frente" abaixo.

A tag do git acompanha: `vMAJOR.MINOR.PATCH`, criada no commit que sobe a versao.

O `docs/CHANGELOG.md` e agrupado por versao. Cada bump abre um cabecalho novo, e as
tarefas aposentadas entram como bullets sob a versao em que sairam:

```markdown
# Changelog

## v0.2.0 — 2026-07-19
- **Nome da tarefa** (T-002) — resultado em uma linha. _Completed 2026-07-19 14:30._
- **Outra tarefa** (T-003) — resultado em uma linha. _Completed 2026-07-19 16:05._

## v0.1.0 — 2026-07-18
- **Primeira tarefa** (T-001) — resultado em uma linha. _Completed 2026-07-18 09:12._
```

Versao mais recente no topo. Tarefas ja concluidas mas ainda nao lancadas ficam sob um
cabecalho `## Nao lancado` ate o proximo bump.

**O changelog nasce no momento em que a tarefa e aposentada — nunca reconstruido depois
do `git log`.** Um changelog gerado retroativamente e adivinhacao com aparencia de
registro: quem le confia, e ele nao sabe o que cada commit significou de verdade. Se o
arquivo divergir do que aconteceu, apague o trecho falso — nao remende.

Antes de 1.0.0 o projeto e instavel e MINOR pode quebrar; ao publicar/entregar para
alguem, va para 1.0.0.

### Projeto com mais de uma frente

Frente = cada superficie, app ou servico que e entregue e muda em ritmo proprio (um site,
um painel, uma API, um app de entrega). **Versoes separadas por frente sao a preferencia
deste workspace** — nao consolide num numero unico. O ganho e diagnostico: quando alguem
reporta um bug, o rodape daquela superficie diz exatamente o que a pessoa esta rodando.
Com numero unico, todo rodape mente sobre as outras frentes.

Se este projeto nascer com mais de uma frente (pergunte, se nao estiver claro), escreva
assim no CLAUDE.md:

- **Liste as frentes numa tabela** com quatro colunas: *frente · versao atual · fonte
  (arquivo:linha) · como bumpa (automatico no build / manual / script de deploy)*. A
  coluna "como bumpa" e a que ninguem lembra de escrever e a que mais importa — ver o
  ponto sobre bump automatico abaixo.
- **Se o bump do numero for automatico, a nota tem que ser automatica tambem** — ou
  assuma por escrito que as notas serao esparsas. Numero que sobe sozinho e nota que
  depende de disciplina humana e uma combinacao que sempre termina do mesmo jeito: um
  projeto real deste workspace tem 8 frentes com bump automatico e **7 delas com zero
  notas**, enquanto a unica de bump manual tem nota. Automatize os dois ou nenhum.
- **Constante de fallback protegida por `#ifndef` (ou equivalente) nao e uma segunda
  fonte** — e um default para quando a real nao foi injetada. Nao a trate como violacao
  da regra de fonte unica, e nao a "corrija".
- **As notas de versao ficam coladas no numero da frente**, nao num arquivo separado —
  uma linha por versao, ordem crescente, a mais recente junto da constante:
  `# <versao>: <o efeito da mudanca, com o [id] da tarefa quando houver>`
  Adjacencia e o que faz a nota ser escrita: e impossivel subir o numero sem ver a lista.
- **A nota entra no MESMO commit do bump.** Subir versao sem nota e bug de processo — e o
  unico momento em que se sabe o que mudou.
- **Nao crie um changelog unico misturando as frentes.** "## v2.3.6" de qual delas? E nao
  tenha os dois (nota junto do numero E arquivo por frente): dois registros divergem.
- **Tags:** em projeto multi-frente, uma tag unica de repo nao significa nada. Decida
  explicitamente entre tag por frente (`galeria-v1.10.1`) ou nenhuma tag, e **escreva a
  decisao**. Tag orfa e pior que tag nenhuma: um repo cheio de tags de versao implica que
  elas acompanham as versoes, e quem confiar erra.

Quando a historia de UMA frente passar de ~30 linhas, o arquivo de versoes deixa de ser
config e vira documento: nessa hora, pergunte ao programador antes de migrar aquela frente
para `docs/changelog/<frente>.md`. Nao migre por conta propria.

## Vikunja (MCP) — regras duras de uso

O MCP do Vikunja e so leitura/escrita de tarefas. Ele custa tokens; use com disciplina.
Estas regras sao obrigatorias:

- E PROIBIDO chamar `vikunja_list_projects` neste projeto. O ID e fixo: **33**. Use-o
  direto em toda chamada.
- Para ver pendencias use `vikunja_get_tasks(project_id=33, done=false)` — ele devolve so
  titulo/status. NAO puxe a descricao de varias tarefas "por precaucao".
- `vikunja_get_task(task_id)` so na tarefa em que voce vai trabalhar AGORA, e no maximo UMA
  vez por tarefa. E proibido re-buscar uma tarefa que ja esta no contexto.
- NUNCA use `vikunja_delete_task` para "fechar" uma tarefa. Concluida =
  `vikunja_update_task(task_id, done=true)`. Delete so quando a tarefa for lixo real e o
  usuario mandar.
- A descricao da tarefa e o registro do trabalho (spec + resultado): escreva la, mas NAO
  ecoe a descricao inteira de volta no chat — resuma em uma linha.
- Toda pendencia que surgir vira tarefa na hora; toda resolvida e marcada `done=true` na
  hora. Nao acumule pra depois.

## Fluxo de trabalho

O fluxo planner/implementer, o formato de `docs/TASKS.md` e a doutrina de documentacao
vem do `CLAUDE.md` da raiz (`C:\dev\CLAUDE.md`) e valem aqui sem repeticao. Este arquivo
so acrescenta o que e especifico deste projeto.
