# Armadilhas

*[Read in English](ARMADILHAS.md)*

Uma linha por pegadinha, **com o custo real que ela cobrou**. Sem o custo, vira aviso genérico e
ninguém lê.

As entradas nasceram nos planos **1** (fundação e inbound) e **2** (envio), e o arquivo cresce a cada
conserto — entrada nova entra no mesmo commit do fix, que é o único momento em que o custo está
fresco. Cada uma foi **provada por teste antes do conserto**: nenhuma é hipotética.

---

## A armadilha-mãe deste projeto: "a regra vale num lugar e não vale no seguinte"

**Custo: quatro achados Critical, todos na mesma branch, todos com este formato.**

O plano 1 teve exatamente quatro defeitos Critical. Nenhum foi erro de digitação, nenhum foi
desconhecimento. Nos quatro, a regra certa **estava escrita** — e não fora aplicada um nível acima:

| # | A regra existia em | Não existia em | O que acontecia |
|---|---|---|---|
| 1 | isolamento por item (mensagens) | `entry` e `changes` | um campo com tipo errado apagava o lote **inteiro**, inclusive mensagens válidas de outra conta |
| 2 | cifra em repouso (`callback_url` no banco) | o log | o `Motivo` imprimia a URL completa, com query string |
| 3 | instância nasce pausada (o store forçava) | o handler | ninguém perguntava se estava ativa; a garantia era **inerte** |
| 4 | contador correto (sequencial) | sob concorrência | `seq++` num handler compartilhado = corrida de dados |

**A lição, e ela é a mais cara desta branch:** ao escrever uma garantia, **não pergunte "esta função
está certa?" — pergunte "onde mais essa mesma frase deveria ser verdadeira, e é?"**. A assimetria
entre dois lugares que resolvem o mesmo problema **é** o bug.

**E o corolário:** os quatro foram achados por **revisão adversarial**, não por leitura. Em três dos
quatro, a suíte inteira estava **verde** no momento em que o defeito existia.

**A forma mais cara deste padrão neste projeto: o gateway sabe RECEBER o que não sabe MANDAR.**
Formulação do `consumer-a` (2026-07-26), ao notar que já eram **três** casos, todos com as duas
metades escritas pelas mesmas mãos em momentos diferentes:

| Funcionalidade | A ENTRADA já fazia | A SAÍDA não fazia |
|---|---|---|
| botão de template | envelope entregava `botao_payload` desde sempre | envio não montava cabeçalho nem parâmetro de botão (até a T-021) |
| reação | envelope trazia `reacao` completa (T-023) | envio recusava remoção (até a T-027) |
| payload de botão | envelope traz `botao_payload` de `button` e de `button_reply` | envio só sabe `sub_type: "url"` (T-044) |
| confirmação de leitura | envelope entrega `status: "read"` desde sempre — sabemos que o cliente leu o que mandamos | não havia como dizer que NÓS lemos a mensagem dele; a conversa ficava em dois tiques cinzas para sempre (até a T-075) |

**O modo de descoberta é o que importa: nenhuma das quatro apareceu lendo o código do envio.** Todas
apareceram quando **alguém foi USAR** a metade de saída de algo que a entrada já fazia bem — e nos
quatro casos quem usou foi um consumidor, não quem escreveu.

**A quarta linha acrescenta duas coisas que as três primeiras não tinham, e as duas valem como
sinal:**

- **os DOIS consumidores pediram a mesma coisa, no mesmo dia, sem se falarem** (`consumer-a` às
  16:42, `consumer-b` às 16:58 de 2026-07-28). Convergência independente é sinal de que a metade
  faltante é real, não preferência de um deles — a mesma leitura que este arquivo já registra para o
  gatilho por *resultado do trabalho* a que os dois chegaram sozinhos;
- **a causa raiz não era nossa: era a MIGRAÇÃO.** Pelo Baileys o marcador de leitura saía **sozinho**
  ao receber; pela Cloud API ele exige chamada explícita. Ou seja, uma funcionalidade que a stack
  antiga fazia **implicitamente** vira ausência silenciosa na nova, e ninguém a lista como requisito
  porque ninguém a implementou da primeira vez. *A pergunta que generaliza, e ela é barata numa
  migração:* **"o que a stack antiga fazia sozinha, sem ninguém pedir?"** — é uma família inteira de
  requisito que não está escrito em lugar nenhum, e todo consumidor que vier do Baileys vai bater
  nela.
*Custo: baixo e medido pelo próprio consumidor — não afeta entrega, cobrança nem cota. O que ele
cobra é a cliente não saber se foi lida, que é o que produz o "oi, chegou?" com o operador já
trabalhando no pedido dela.*

*Prevenção, barata, sugerida por quem levou o susto: **ao acrescentar campo ao envelope, pergunte na
mesma tarefa "e o envio sabe produzir isto?"** — não para implementar junto, mas para **registrar a
assimetria em vez de descobri-la pelo consumidor**. Uma linha no `Why` da tarefa custa nada; um fluxo
central travado num consumidor em produção custa o dia dele.*

**O padrão atravessa repositório, linguagem e time — três instâncias num único dia (2026-07-26), e uma
quarta no mesmo dia, achada por um QUINTO jeito:**

| Onde | A regra existia em | Não existia em |
|---|---|---|
| aqui, Go | envelope do evento de **mensagem** (T-023) | evento de **status**, que perdia o motivo da falha (T-028) |
| consumidor, Python | `503` para credencial no canal bilateral | o **contrato**, que todo consumidor futuro leria (movido em PR #34) |
| consumidor, Python | incremento da série de idempotência no **worker** | o **botão "reenviar" do admin**, que regravava o telefone sem trocar a chave → `422` → cliente nunca recebia |
| aqui, Go | vocabulário fechado de contadores (`internal/config/counter.go`) | a impressão de `zapgw estado` (`cmd/zapgw/state.go`), que repetia a lista à mão e não sabia da chave nova (T-038/T-039) |

Nos três primeiros, quem escreveu a regra foi quem deixou o buraco, e nos três a suíte estava verde.
**O que achou os três foi a mesma pergunta, não a mesma pessoa** — o que sugere que ela vale como
passo de revisão, não como talento.
*Um detalhe do terceiro caso vale copiar: o conserto NÃO foi acrescentar o incremento nos dois
lugares — foi criar UMA função para a transição, com a regra na docstring. Enumerar call sites é o
que produz o furo; um terceiro caminho no futuro esbarra na frase em vez de esquecê-la.*
*E o aviso que destravou aquele caso apontava um eixo DIFERENTE (texto recomputado no envio), que
estava limpo. O valor não estava na hipótese estar certa: estava em ela obrigar alguém a enumerar os
caminhos. **Palpite bem-dirigido e errado ainda encontra o bug ao lado.***

**A quarta linha chegou por um jeito diferente dos quatro já registrados neste arquivo** (revisão
adversarial, captura real de tráfego, experimento com aparelho físico, reler o que já foi capturado
com a pergunta certa — ver as entradas "Meta / WhatsApp Cloud API", acima). A T-038 acrescentou
`config.CounterAccountDiscarded` ao vocabulário fechado e — sem nenhuma revisão externa, nenhum teste
novo, nenhum tráfego novo — o PRÓPRIO implementer da T-038 declarou, ao encerrar a tarefa, que
`cmd/zapgw/state.go` tinha uma lista própria e não sabia da chave nova. Não foi "onde mais essa regra
deveria valer?" perguntado por um revisor de fora: foi quem acabou de escrever o código notando, na
hora, que o problema que ele mesmo criava era uma instância do padrão deste arquivo, e registrando
isso como a próxima tarefa em vez de deixar para alguém achar depois.
*Custo: zero em produção até aqui — `CounterAccountDiscarded` nunca teve seu valor real olhado por
ninguém antes do conserto (T-039), mas também nunca deu resultado ERRADO, só invisível. Conserto:
`config.KeysInDisplayOrder` (`internal/config/counter.go`) vira a fonte única — tanto o
conjunto de validação (`counterKeys`, derivado dela) quanto a impressão de `cmd/zapgw/state.go`
(que passou a percorrê-la, sem lista própria) leem do MESMO lugar. Mutação obrigatória, feita e
revertida antes do commit: restaurar a lista antiga de `state.go` (sem tocar em `counter.go`) deixa
`TestStateCommandShowsEveryVocabularyKey` (`cmd/zapgw/state_test.go`) vermelho, faltando
`conta_descartada`; acrescentar uma chave nova só em `KeysInDisplayOrder` (sem tocar em
`state.go`) deixa o mesmo teste verde, com a chave nova aparecendo sozinha.* **A pergunta que
generaliza: quando você mesmo acaba de criar a assimetria "a regra vale aqui, não vale ali", o quinto
jeito de achá-la é simplesmente dizer isso em voz alta antes de encerrar a tarefa — não esperar que
uma revisão, uma captura ou um teste futuro a ache por você.**

**O SEXTO jeito: provocar de propósito um alarme que nunca disparou — e olhar o que está AO LADO
dele.** A T-042 (2026-07-26) exercitou com tráfego, no binário de produção do CT 125, a guarda do
passo 5 (`internal/inbound/handler.go`), que tinha teste unitário e
**nunca havia rodado com duas instâncias de verdade**: em toda a vida do serviço,
`journalctl -u zapgw | grep "que nao e dela"` dava **zero**. A hipótese que abriu a tarefa era que o
alarme estivesse quebrado. **Ele não estava** — provocado com um `phone_number_id` alheio, saiu na
hora e correto. O achado é o vizinho:

*(Esta entrada chamava a guarda do passo 5 de "isolamento entre inquilinos". O nome foi corrigido na
T-050 — ver a seção *Meta / WhatsApp Cloud API*: com um App por consumidor, ela é **conferência de
endereçamento** e quem separa inquilino é a assinatura do passo 3. O que a T-042 mediu não muda.)*

| | recusa por `phone_number_id` (5a, plano 1) | recusa por `waba_id` (5b, T-038) |
|---|---|---|
| responde `200` à Meta | sim | sim |
| escreve `ALARME` no journal | sim | sim |
| **contava alguma coisa** (até a T-047) | **não — nada, nem `recebidas`** | sim, `config.CounterAccountDiscarded` |
| conta hoje (T-047) | sim, `config.CounterNumberDiscarded` | sim, `config.CounterAccountDiscarded` |

Ou seja: uma recusa de isolamento pelo `phone_number_id` **era invisível em `zapgw estado`** (o
conserto é da T-047, mais abaixo); o único rastro era uma linha de journal, e este arquivo já
registra (seção *Erros e log*) que ninguém lê journal por hábito. Medido na própria T-042: depois de
provocar o alarme, a instância de teste
mostrava `recebidas 1` (só o evento aceito) e `conta_descartada 0` — o evento recusado não aparecia
em contador nenhum. É exatamente a forma desta seção: a mesma frase ("recusa de isolamento é
contada") vale no ramo escrito em 2026-07-26 e não vale no ramo escrito no plano 1, e as duas metades
foram escritas pelas mesmas mãos em momentos diferentes.

*Custo: zero — a recusa nunca aconteceu em produção, e o defeito viveu do plano 1 (2026-07-23) até a
T-047 (2026-07-26) sem nunca ter dado resultado ERRADO, só invisível. **Ficou ABERTA por um dia, e a
razão fica registrada porque ela é a decisão certa, não a preguiça:** a T-042 era tarefa de
EXERCITAR, com a lista de `Files:` fechada no comentário do passo 5 — consertar ali teria misturado,
no mesmo commit, o que foi medido com o que foi mudado. **Fechada na T-047:**
`config.CounterNumberDiscarded` (`numero_descartado`) entra no vocabulário fechado de
`internal/config/counter.go` e é registrado no passo 5a de `internal/inbound/handler.go`, **depois**
do `w.WriteHeader` como manda a T-035. Nenhuma linha de `cmd/zapgw/state.go` mudou — a fonte única
da T-039 fez a chave nova aparecer sozinha na tabela, que é a mesma garantia exercitada de novo, de
graça.*

*Por que a chave é NOVA e não `conta_descartada` reusada: elas respondem igual à pergunta "houve
recusa de isolamento?" e **diferente** à seguinte — "qual guarda recusou?" —, e é a segunda que
decide onde a pessoa vai procurar (Callback URL/override/`phone_number_id` cadastrado contra
WABA/App cadastrado). Um número que soma as duas manda conferir os dois lugares, todas as vezes; e
quem só tem o total não consegue separar de volta, enquanto quem tem as duas soma na tela.*

***A mutação obrigatória rendeu mais do que a prova que ela pedia, e o resultado do primeiro estágio
é a informação que vale.*** A tarefa mandava mover o `Registrar` para ANTES do `w.WriteHeader` e
provar que o teste de "contador não muda o status" fica vermelho.
**Mover sozinho deixa o teste VERDE** — e isso não é teste fraco: é `Contador.Record` **não
devolvendo nada** (ver a entrada "um método que PODE devolver erro…", seção *Erros e log*), de modo
que não existe caminho pelo qual a falha de contagem alcance a resposta. Só o estágio dois — mover
**e** trocar por uma variante que devolve erro, tratado com `http.Error` como o resto do projeto
trata erro — deixa `TestHandlerCounterFailureDoesNotChangeStatusOfPhoneNumberIDRefusal` vermelho, com
`500` no lugar do `200`. **A lição: quando uma mutação de ordem passa verde, pergunte se a defesa
real é a ordem ou a ASSINATURA** — aqui a ordem é disciplina (e continua valendo, porque a próxima
pessoa pode não ter a assinatura do lado dela), e a assinatura é a garantia.

**A pergunta que generaliza: ao finalmente exercitar uma guarda que nunca rodou, não pare em "ela
disparou?" — pergunte o que os ramos VIZINHOS fazem que ela não faz.** Alarme que nunca disparou não
é só alarme não testado: é um ramo inteiro de código cuja instrumentação ninguém teve motivo de olhar.

**A forma mais irônica dessa assimetria: o ramo que existe para dizer *"não sei o que aconteceu"* era
o ÚNICO que não guardava o que aconteceu.** Em 2026-07-28 o `consumer-b` criou o template
`pedido_avaliacao_v2` pela `POST /v1/templates` e levou `502 desconhecido` com a mensagem *"o template
PODE ter sido criado — confira o catálogo antes de tentar de novo"*. **O template tinha sido criado.**
No `default` de `respondCreationError` (`internal/outbound/templates_handler.go`), os ramos vizinhos
todos logavam — `ALARME … credencial recusada`, `waba_id invalido`, teto de páginas — e só aquele
descartava o `err`. Medido no CT de produção, no dia inteiro em que o `502` saiu de lá:
`journalctl -u zapgw | grep -ci template` = **0**. Quando o consumidor perguntou *"foi timeout ou
transporte?"*, a resposta não estava perdida no meio do journal: ela **nunca tinha sido escrita**.

*Custo: o consumidor gastou a segunda porta para descobrir a verdade — foi conferir o catálogo
**direto na Graph API**, acesso que o dono proibiu no MESMO dia (`CLAUDE.md`, "NINGUÉM fala direto com
a Meta"). Ou seja, o único caminho que salvou aquele caso deixou de existir horas depois, e o defeito
ficaria sem rede de proteção nenhuma. Do nosso lado, o preço foi um desfecho estruturalmente
indiagnosticável: nem contador, nem log, nem resposta com informação.*

**A pergunta que economiza a próxima, e ela é estreita: o ramo que classifica um desfecho como
DESCONHECIDO guarda o que ele não sabe?** "Desconhecido" é a única classe cujo valor está inteiro no
rastro — as outras já dizem tudo na própria resposta. Um `desconhecido` calado é o pior dos dois
mundos: quem chamou não fica sabendo e quem opera também não.

🔴 ***E a segunda metade é onde a T-078 quase errou pior do que o defeito original: "NÃO ACHEI" NÃO É
"NÃO EXISTE".*** O conserto óbvio — reler o catálogo e, não achando, responder *"não foi criado"* —
teria sido uma afirmação que o gateway **não tem como sustentar**. A pergunta foi feita à fonte:
a Meta documenta *read-after-write* para a **resposta do próprio `POST`** dessa edge
(`developers.facebook.com/docs/graph-api/reference/whats-app-business-account/message_templates/`) —
que é exatamente o que não chegou — e **não documenta, em nenhuma página que foi possível ler, que um
`GET /{waba}/message_templates` posterior já contém o template novo**. Nem o contrário. *Não
documentado nos dois sentidos* é resposta legítima, e obriga a resposta a ser **INCONCLUSIVO**.

*O que torna esse erro caro é a ASSIMETRIA, e ela vale como regra fora deste caso: dizer "não sei"
custa ao consumidor uma conferência; dizer "não foi criado" faz ele recriar — e `nome` + `idioma` são
únicos por conta, com a segunda criação voltando `code 100` / `subcode 2388024`
(`developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management`).
**O nome que ele escolheu fica inutilizável, para sempre.** Um erro custa um minuto; o outro custa um
nome.* **Quando os dois erros possíveis têm preços de ordens diferentes, a dúvida não se resolve pelo
que é mais provável — ela se resolve pelo lado barato.**

*Conserto (T-078): `respondAmbiguousOutcome` loga a causa real (slug, nome e idioma do template —
**nunca o corpo do pedido**, que carrega texto que vai para o cliente final), relê o catálogo com
**contexto novo** (o da criação pode estar morto de prazo esgotado, que é uma das causas de chegar
ali) e responde `201` se achou, `INCONCLUSIVO` se não achou, e `502` com **as duas falhas logadas** se
a releitura também caiu. A releitura é `GET` e só — `TestRereadDoesNOTCreateAgain` conta os POSTs que
chegam à Meta nos três desfechos e exige exatamente **um**. O `cmd/grafo-falso` ganhou
`--falha-de-template={criado,nao-criado,catalogo-tambem}`, que derruba a conexão do POST **sem
resposta** (`panic(http.ErrAbortHandler)`): um `500` seria RESPOSTA, e resposta a Meta classifica — o
desfecho `desconhecido` nasce da ausência dela.*

*Duas mutações, feitas e revertidas antes do commit, e a primeira é a que informa:* (1) trocar o
desfecho "não achei" por *"o template não foi criado… pode criar de novo"* deixa
`TestCreateTemplateAmbiguousNotFoundInTheCatalogIsINCONCLUSIVE` vermelho em **quatro asserções sobre o
TEXTO** — falta de `inconclusiv`, presença de `nao foi criado`, presença de `nao existe` e falta do
*"não repita"* —, além da classe. **A asserção é sobre o texto de propósito**: foi o texto, e não o
código HTTP, que fez o trabalho no caso real (`502` sozinho não teria dito nada a ninguém); (2)
remover o `log.Printf` da falha da criação deixa dois testes vermelhos, e o detalhe vale: as linhas
de **desfecho** continuam saindo com slug, nome e idioma, então o que se perde é exatamente a
**causa** (`inalcancavel` / `prazo esgotado`) — ou seja, a mutação reproduz com precisão a pergunta
que ficou sem resposta em produção, *"foi timeout ou transporte?"*.

**O OITAVO jeito, e é o mais barato de todos: alguém OPERANDO o sistema comparou um número da tela
com um fato que ele acabou de presenciar.** Em 2026-07-28, minutos depois de o `zapgw fumaca` ativar
a `tenant-two`, a mensagem de teste saiu (wamid devolvido, e o dono confirmou **no aparelho** que
ela chegou) e o `zapgw estado` seguinte dizia `enviadas hoje 0`. Não houve revisão, tráfego novo nem
teste: houve uma pessoa com a mensagem no celular olhando um contador que dizia que ela não existia.

A assimetria tem a forma exata desta seção — *"toda mensagem que sai conta"* valia no
`POST /v1/messages` e não no `fumaca` — e o mecanismo dela é o que vale copiar: o `fumaca`
compartilhava com produção **o código que MONTA o corpo** (`Validar` + `MetaBody`, deliberado —
caminho próprio provaria o caminho errado) e não o **handler**, que é onde o `Registrar` morava.
*Compartilhar a construção do efeito não compartilha a instrumentação dele*, e a revisão que
pergunta "os dois caminhos usam o mesmo código?" responde **sim** e passa reto.

*Custo: baixo em volume (uma mensagem por instância, na ativação) e alto em confiança — o zero caía
exatamente na instância recém-ativada, que é quando *"essa instância já mandou alguma coisa?"* mais é
perguntada, e a resposta certa a partir do log seria "procure no journal", o rastro que os contadores
existem para não ser o único. Conserto (T-054): o passo 3 do `cmd/zapgw/smoke.go` registra
`config.CounterSent` no sucesso e `config.CounterSendFailures` na falha, **pela mesma chave do envio
de produção** — chave própria para o fumaça obrigaria quem lê a somar duas colunas para responder uma
pergunta, e quem tem só o total nunca separa de volta.*

*Quatro mutações, feitas e revertidas antes do commit; a terceira é a que informa:* (1) tirar o
`Registrar(ContEnviadas)` deixa `TestSmokeCountsSENTUnderTheSAMEKeyAsTheProductionSend` vermelho
**reproduzindo o sintoma de produção na íntegra** — a tabela do `zapgw estado` sai com a instância
`ativa` e todas as chaves em zero; (2) tirar o `Registrar(ContFalhasDeEnvio)` deixa
`TestSmokeCountsSENDFAILURESWhenMetaRefusesTheSend` vermelho; (3) **mover o
`Registrar(ContEnviadas)` para DEPOIS do passo 4 (`ActivateInstance`) passa VERDE** — nada na suíte
faz aquele passo falhar, então "conte o passo 3 antes do passo 4" é **disciplina escrita no
comentário, não garantia provada**: se a ativação falhasse, a mensagem teria saído e o contador
diria zero de novo. É a mesma lição da mutação de ordem da T-047, uma superfície adiante — a defesa
que existe de fato é a ASSINATURA de `Registrar`, que não devolve nada e por isso não pode abortar o
`fumaca` depois de a mensagem ter saído; (4) trocar a chave por uma inventada (`enviadas_pelo_fumaca`)
deixa o mesmo teste de (1) vermelho **sem erro nenhum no comando** — o vocabulário fechado
(`internal/config/counter.go`) só loga e segue, que é o comportamento certo e também o motivo de
uma chave nova precisar de teste para não virar contagem silenciosamente descartada.

**A mesma armadilha na SUPERFÍCIE, e não no dado: o consumidor via quatro blocos que o operador com
SSH aberto não via.** A T-060 (rota `GET /v1/estado`) e a T-064 (`certificado_do_callback`) subiram
no mesmo dia, cada uma com a suíte verde, e nenhuma das duas tocou em `cmd/zapgw/state.go`: a rota
publicava `estado`/`pausada`, `versao`, `token_meta` e `certificado_do_callback`, e o `zapgw estado`
mostrava **só a tabela de contadores**. Não era divergência de dado — os números sempre vieram de
`config.SummarizeCounters`, fonte única desde a T-039, e o teste que compara as duas superfícies
número a número passava. Era assimetria de **superfície**, que é a forma mais cara desta seção: a
informação **existe, está correta, e só não aparece onde alguém está olhando**. E quem está olhando
pelo CLI está, quase por definição, no meio de um incidente — teria de sair do CT, achar um token de
consumidor e chamar a rota interna para perguntar ao binário na frente dele se a Meta ainda aceita o
token.
*Custo: zero medido — o comando nunca mostrou número ERRADO, só mostrou MENOS, e o buraco viveu
poucas horas (T-060 e T-064 em 2026-07-28, fechado na T-065 no mesmo dia). Conserto:
`internal/outbound/state.go` — **um lugar que monta o estado (`BuildState`), duas superfícies que
o apresentam**; a rota serializa o `Estado` em JSON e o CLI imprime `StateRows`, que sai por
**reflexão** sobre os campos do struct. Nenhuma das duas enumera campo, e é isso que impede a
recaída.*
**O modo de descoberta repete o quinto jeito, e vale registrar que ele não foi acidente:** quem achou
foi o implementador da **T-064**, que rodou
`grep -n "token_meta\|Veredito\|vigia" cmd/zapgw/state.go`, viu **zero linhas** e — em vez de
consertar calado fora do escopo, ou de deixar para alguém achar depois — **reportou como tarefa**.
*E a mutação obrigatória é a única prova que vale aqui: acrescentar um campo ao `Estado` tem de
aparecer nas DUAS telas sem editar nenhuma delas. Feita e revertida antes do commit
(`campo_de_mutacao`), com `git status` mostrando **um** arquivo modificado.*
✅ **A garantia se exerceu sozinha, com campo de verdade, na T-120 (2026-08-06):** o bloco `entrada`
entrou no struct `Estado` e apareceu no JSON da rota **e** na tela do `zapgw estado` — o CLI
(`cmd/zapgw/state.go`) não ganhou uma linha sobre ele, só a construção da fonte. *Não é prova nova, é
a mesma prova cobrada por um caso real em vez de um campo de mentira.*

***E o achado que só apareceu ao implementar: extrair a fonte comum NÃO BASTA quando a fonte é
MEMÓRIA de outro processo.*** O `token_meta` vem do cache da vigia (`internal/outbound/watchdog.go`),
que vive na memória do **servidor**. O `zapgw estado` é outro processo, e nasce com o cache **vazio**
— publicar o bloco lendo esse cache teria posto na tela `veredito: desconhecido`, com os dois
carimbos em branco, **para sempre**. Isso não é "menos informação": parece **vigia quebrada**, e
manda quem opera investigar um defeito que não existe, no meio do incidente. *Conserto: o CLI **mede**
antes de ler (`Vigia.CheckInstance`), pelo mesmo motivo que o probe (`health_handler.go`) não tem
cache — quem está na frente do terminal escolheu a frequência ao apertar Enter. Instância PAUSADA
continua não sendo medida, igual ao servidor, e o `pausada: sim` ao lado explica o `desconhecido`.*
**A pergunta que generaliza: ao unificar duas superfícies, pergunte de onde cada campo VEM em cada
processo — "a mesma função" não garante "o mesmo dado disponível", e o campo que some é justamente o
que só existe em memória.**

**A mesma armadilha no SORTEIO de segredo: sortear calado é CERTO para dois campos e ERRADO para os
outros dois, e o modo de falha é AUSÊNCIA de erro.** `zapgw provisionar instancia` sorteava os quatro
segredos da instância sem imprimir nenhum — política única, aplicada uniformemente, e por isso mesmo
errada na metade. Dois deles existem **só dentro do gateway** (`app_secret`, `token_envio`) e
imprimi-los é exposição gratuita. Os outros dois precisam existir **também fora**: o `verify_token`
vai no painel da Meta e o `segredo_entrega` vai no consumidor. Sorteados e não mostrados, eles nascem
**inutilizáveis** — e nada acusa. Não há erro, não há log, o comando termina com sucesso; a falha só
aparece muito depois, quando o desafio da Meta não passa ou quando o consumidor não consegue conferir
o HMAC, longe do comando que causou. *Custo: o `ONBOARDING-META.md` mandava apontar o Verify Token
"que você escolheu" — instrução impossível de cumprir, porque ninguém escolheu nada; o valor tinha
sido sorteado e descartado.* **Conserto (T-052): imprimir os dois compartilhados, com aviso, no mesmo
formato do token de consumidor.** Duas coisas valem mais que o conserto: (1) a saída aparentemente
mais segura — **recusar** de sortear e exigir que o humano forneça — é pior, porque não reduz
exposição humana (segredo que uma pessoa transporta uma pessoa vê), só move a **geração** para fora,
onde alguém com pressa digita `segredo123` como chave HMAC de toda entrega; e (2) o custo aceito está
**escrito junto da decisão**, no código: o valor passa pelo terminal, e terminal vira transcript — foi
assim que quatro segredos vazaram na manhã de 2026-07-28. *Dois testes antigos codificavam "nunca
imprima segredo" sem exceção. Foram **estreitados, não desabilitados**, com o motivo dentro e ponteiro
cruzado para o teste que agora exige o oposto — desabilitar teria apagado a regra junto com a
exceção.*

🔴 **E a MESMA política virou errada de novo dois dias depois, na outra metade — porque a premissa
dela caiu.** A T-052 escreveu, com todas as letras, que `app_secret` e `token_envio` "existem só
dentro do gateway" e por isso podem ser sorteados calados. Isso era verdade enquanto a conta Meta
fosse do dono. A **T-079** entregou o modelo em que a conta Meta é do **consumidor** — e nesse
caminho os dois deixaram de ser nossos: eles têm um valor certo, que vive no painel dele, e qualquer
outro é lixo. Sortear passou a **produzir uma afirmação falsa na única superfície de leitura que
existe**: nada é decifrado neste projeto (T-020), então `zapgw instancia mostrar` e a resposta do
cadastro dizem apenas `cadastrado: sim|não` — e com o sorteio elas diziam `app_secret=sim` sobre um
segredo que a Meta do consumidor nunca viu. A pergunta que o dono e o consumidor **conseguem** fazer
("ele já cadastrou?") ficava sem resposta, e a resposta errada era a tranquilizadora.

*Como apareceu, e é o quinto jeito do arquivo (quem escreve nota a assimetria que ele mesmo acabou de
criar): não foi revisão nem tráfego — foi um teste do `instancia mostrar`, escrito na própria T-079,
falhando em `app_secret=nao` e mostrando `app_secret=sim` numa instância recém-criada que ninguém
tinha cadastrado. **Custo: zero em produção** — a instância nasce PAUSADA e o `zapgw fumaca` é o
único caminho para `ativo = 1`, e ele exige uma mensagem que saiu de verdade, impossível com
`token_envio` sorteado; o estrago seria de DIAGNÓSTICO, não de tráfego. Conserto no mesmo commit:
`cmd/zapgw/provision.go` marca os dois como `fromConsumerMeta` e **não os sorteia** quando a
instância nasce sem identificação (o sinal de "a conta Meta é do consumidor"), imprimindo
`NAO sorteados, porque sao da conta Meta do CONSUMIDOR: …` para que o `nao` na tela não pareça
defeito. Nos dois campos do gateway (`verify_token`, `segredo_entrega`) o sorteio da T-052 continua
igual, e há teste para cada metade.*

**A pergunta que generaliza, e ela é mais estreita do que "revise as regras antigas": quando um
modelo novo muda QUEM é dono de um dado, vá reler toda decisão que foi justificada por "este valor é
nosso".** A regra não estava errada — a premissa dela mudou de dono, e política de valor default
("sorteie o que faltar") é o lugar onde isso vira mentira silenciosa, porque valor sorteado é
indistinguível de valor cadastrado para qualquer leitura que não decifre. *Corolário barato:
`grep` por "só dentro do gateway", "é nosso", "qualquer valor serve" rende a lista de candidatos.*

**A armadilha do CAMINHO QUE SÓ EXISTE NUM SENTIDO: o CLI sabia CRIAR consumidor e não sabia
REVOGAR.** É a irmã de *"sabe receber o que não sabe mandar"*, e ela se esconde melhor, porque o
caminho que existe funciona perfeitamente — ninguém percebe o que falta enquanto está tudo calmo. O
custo chega junto com o incidente, que é quando a pressa é máxima: em 2026-07-28, ~10:40, os quatro
segredos de uma instância vazaram no transcript de um consumidor. Três tinham comando. **O token de
consumidor não tinha nenhum** — e "não tem comando" durante incidente significa `UPDATE` na mão, em
produção, com o relógio correndo. *Custo real: a rotação saiu, mas as duas saídas erradas que a pressa
oferece tiveram de ser recusadas em tempo real — criar `<nome>-2` (o token exposto continua valendo) e
apagar/recriar (vínculos somem, e o consumidor leva um `403` que ninguém sabe explicar). Conserto
(T-055): `zapgw consumidor rotacionar` / `listar`, e as duas saídas erradas viraram as duas
mutações obrigatórias.* **A pergunta que generaliza, e ela cabe em toda superfície: para cada coisa
que este sistema sabe CRIAR, ele sabe DESFAZER? E a resposta tem de ser dada antes do incidente, porque
durante ele não há tempo de descobrir que a resposta é não.**

**Lista de tabelas escrita de memória apodreceu em UM dia.** A T-048 dizia que remover uma instância
"apaga a instância e as linhas de `contador` dela". Eram **quatro** tabelas com coluna `slug`, não
duas: a `certificado_do_callback` nasceu na T-064 **no dia seguinte** ao texto ser escrito, e a
`consumidor_instancia` nunca foi lembrada por ninguém. E o erro não deixaria só linha órfã — com o
PRAGMA de chave estrangeira ligado, o `DELETE FROM instancia` **nem executa** sem apagar a
`consumidor_instancia` antes. *Conserto (T-048), e ele não é "a lista certa":*
`TestRemoveInstanceCOVERSEveryTableWithASlugColumn` pergunta **ao próprio banco** quais tabelas têm
coluna `slug` e fica vermelho **nomeando** a que faltar. A lista continua explícita de propósito — uma
tabela futura pode dever **sobreviver** à instância, e isso é decisão, não varredura; a guarda existe
para que a decisão seja **tomada**, não para tomá-la sozinha. **A pergunta que generaliza: toda lista
escrita à mão sobre o ESQUEMA tem de ter, ao lado, algo que pergunte ao esquema.** Enumeração de
tabelas, de colunas, de chaves de contador, de campos de resposta — em todas, o que apodrece é a
lista, e o que não apodrece é a pergunta.

**O NONO jeito, e é o mais desconfortável dos nove: o defeito estava ESCRITO, com todas as letras,
dentro de um teste VERDE.** A T-043 (2026-07-26) entregou o aviso de reclassificação de categoria
lendo `message_template_category` de `message_template_status_update` — o webhook de
**aprovação/rejeição**, que traz a categoria como *atributo* do estado novo. O evento que a Meta
dedica à **mudança** de categoria é outro, `template_category_update`, e o App nem estava inscrito
nele. Consequência: a proteção **só disparava quando a Meta reaprovava o template no mesmo
movimento**; uma reclassificação de um template já aprovado passava em silêncio, com a descoberta
chegando na fatura — o modo de falha exato que a tarefa existia para evitar.

*O detalhe que faz esta entrada valer uma seção:* a mesma T-043 escreveu
`TestParseWebhookAnotherAccountFieldStaysOnlyInTheRawBody` (`internal/meta/parse_test.go`) para provar que
campo de conta não modelado continua só no `cru` — e **escolheu `template_category_update` como
exemplo**. Ou seja: o repositório continha um teste verde, com nome afirmativo, dizendo que o
gateway **não lê** justamente o canal que a proteção precisava ouvir. Ninguém leu aquela linha como
um achado por dois dias, porque um teste verde é lido como "isto está certo", não como "isto está
escrito".

*Custo: **desconhecido por construção**, e desta vez nem o silêncio é observável — não há evento
perdido para contar, há um evento que **nunca chegou** porque o campo estava desinscrito. Viveu de
2026-07-26 (T-043) a 2026-07-28 (T-057), com um único template no ar e nenhuma reclassificação
conhecida no período. Conserto: `fieldTemplateCategory` + `templateCategoryEvent`
(`internal/meta/parse.go`) e `TemplateCategory` (`internal/meta/types.go`), com a inscrição feita
pelo dono no painel no mesmo dia — as duas metades eram necessárias e nenhuma bastava.*

**A pergunta que generaliza, e ela é barata: ao escolher o exemplo de um teste NEGATIVO ("isto a
gente não lê", "isto não pode acontecer"), pergunte por que o exemplo é ESSE.** Um exemplo escolhido
sem pensar é inofensivo; um escolhido porque era o mais parecido com o que você acabou de
implementar é uma confissão. *Corolário para revisão: `grep` pelos nomes que aparecem em testes de
"não modelamos isto" rende uma lista curta de candidatos a "deveríamos".*

*E a segunda lição, sobre FIXTURE em vez de código:* o sample do painel da Meta traz
`previous_category` e `correct_category` com o **mesmo valor** (`"MARKETING"`). Congelar só ele
teria produzido um corpus em que trocar a leitura de um campo pelo outro passa **VERDE** — medido:
a mutação `t.Previous` → `t.Correct` deixa vermelho **apenas** o fixture sintético
(`categoria_de_template_sintetico.json`), e o derivado da doc continua verde com o `ID` idêntico. É
a mesma família de `botao_de_template.json` (`payload == text` na captura real), e a regra que sai
dela é a de sempre: **antes de congelar um payload, olhe se dois campos vizinhos têm o mesmo valor —
se têm, ele não distingue leitura trocada e precisa de um irmão que distinga.**

*A pergunta rendeu de novo no MESMO dia, na T-058, o que a promove de anedota a passo de revisão:* o
sample de `phone_number_quality_update` traz `current_limit` e
`max_daily_conversations_per_business` com o mesmo `"TIER_250"`. Mutação feita e revertida:
`q.CurrentLimit` → `q.MaxDailyLimit` deixa vermelho **só** o fixture sintético; o derivado da doc
segue verde com o `ID` idêntico. **E o valor do passo é que ele às vezes responde NÃO:** no sample de
`account_alerts` os cinco campos são distintos, então ele não ganhou irmão sintético — acrescentar um
"por simetria" com o vizinho seria cerimônia sem garantia (a mesma decisão de `botao_interativo.json`).
*Três instâncias, duas respostas diferentes, custo de perguntar: um `diff` visual de trinta segundos.*

---

**Medição LIMPA de uma coisa vira conclusão SUJA sobre outra — e o número junto é o que a torna
convincente.** Em 2026-07-28 o `consumer-a` afirmou, com confiança, que o corpus de payloads crus da
Meta deles *"congelou no dia da migração e não cresce mais"*. Eles tinham medido: `LIKE '%entry%'` vs
`LIKE '%instancia%'` nas linhas guardadas, e a fronteira caía exatamente no dia da T-250 deles. A
medição estava **certa** — o formato da linha guardada mudou mesmo naquele dia. A conclusão estava
errada: o envelope do zapgw carrega o corpo cru **dentro dele**, em base64 (`Envelope.Raw`,
`internal/inbound/deliver.go:44`), então o que mudou foi o **invólucro**, não o conteúdo. Eles tinham
267 crus, não 225, e o número **cresce**. Pior: o comentário do próprio código deles, no arquivo que
estavam com aberto, dizia *"não perdemos o cru"*.

A formulação é deles e é a melhor parte: ***"eu medi a forma do invólucro e concluí sobre a existência
do conteúdo. O antídoto não é medir mais — é NOMEAR O QUE A MEDIÇÃO RESPONDE antes de olhar o
número."***

*Custo real, medido dos dois lados: um documento de canal que teria mentido para a próxima pessoa, uma
lacuna de captura declarada maior do que era, e — o mais caro — uma **ausência provada com uma busca
incapaz de achar**: `forwarded = 0` medido por `LIKE` no texto do envelope, onde o base64 esconde a
palavra. O zero sobreviveu à correção do método, mas só se soube disso depois de refazer.*

**E a variante oposta aconteceu AQUI no mesmo dia, o que fecha a família:** o o runbook de implantacao (que fica no repositorio privado)
justifica o desenho de rede afirmando que há um túnel `cloudflared` na frente do Traefik. O planner
repetiu isso **duas vezes ao dono, com convicção, sem medir**; o dono mediu com `nslookup` e derrubou
(T-066). Lá, medição certa com conclusão errada; aqui, nenhuma medição e uma doc antiga tratada como
fato. **A doutrina — *confira contra o código, nunca contra a doc antiga* — estava sendo aplicada ao
parser o dia inteiro e não à topologia, porque doc de infra "parece" fato.**

**As duas perguntas que generalizam, e elas são distintas:** (1) *o que exatamente esta medição
responde?* — escreva a frase antes de olhar o número, porque depois o número já convenceu; e (2) *esta
busca conseguiria achar o que procura?* — ausência só vale como prova depois que o método foi
exercitado contra um positivo conhecido.

**E o fecho da família, achado ao CONSERTAR a doc acima (T-066): medição "de fora" feita de DENTRO da
LAN é hairpin NAT — e ela responde igualzinho ao que a internet responderia, até a hora em que não
responde.** A primeira tentativa da correção mediu `:443` e `:8443` contra o IP público
`186.236.224.2` **de uma máquina cujo próprio IP de saída é esse mesmo endereço**
(`curl https://api.ipify.org` → `186.236.224.2`). O roteador devolve a conexão para dentro; nada sai
para a internet. As três medições pareciam conclusivas e iam sustentar uma afirmação de
**segurança** — *"a porta de envio não é alcançável de fora"* — que por acaso estava **certa** e não
estava **provada**.
*Custo: zero em produção, e por sorte. **Conclusão certa obtida por método errado é a mais
duradoura de todas**, porque ninguém volta a conferir o que já deu o resultado esperado — ela teria
virado a justificativa escrita do desenho de rede, como a do túnel tinha virado. O que denunciou não
foi o raciocínio: foi um detalhe de leitura, um `200` obtido "de fora" batendo **byte a byte** com a
resposta da LAN.*
**Conserto: ponto de observação genuinamente fora da rede (nós do `check-host.net`, 12 países), com
CONTROLE POSITIVO obrigatório** — `:443` conecta de 8/8 nós (São Paulo 10 ms, Singapura 331 ms) e
`:8443` dá timeout em 12/12, **incluindo o mesmo nó de São Paulo que enxergou o `:443`**. Sem o
controle positivo, "timeout em todos" é indistinguível de "a ferramenta não alcança este endereço".
*E a ferramenta tem modo de falha próprio, que quase repôs o erro por outro caminho: um nó (`ua3`,
AS13335) relatou o `:8443` **aberto em 2,7 ms** a partir de Kiev — impossível para um destino no
Brasil, onde o melhor nó real fez 10 ms. **Um único falso positivo fisicamente impossível derruba a
conclusão certa se ninguém olhar a latência ao lado do veredito.***
**A terceira pergunta que generaliza, irmã das duas acima: de ONDE esta medição está sendo feita, e
esse ponto de observação é o mesmo de quem eu quero descrever?** Toda afirmação sobre *"o que a
internet alcança"* feita de dentro da própria rede é hairpin até prova em contrário.

**A quarta pergunta: o seu instrumento consegue enxergar o que você quer medir?** Em 05/08/2026,
o `consumer-b` mediu `GET /v1/templates` duas vezes, obteve 109 templates com `motivo` ausente em
todos, e reportou como evidência de que o gateway não devolvia o campo. A conclusão estava certa.
A evidência não era: o cliente Python montava um dicionário novo com seis chaves fixas e descartava
o resto — ele nunca conseguiria enxergar `motivo` nem se o campo estivesse chegando. *"Campo que
não existe no objeto"* é a forma do seu mapper, não a forma do dado. A-armadilha: o instrumento
filtra antes de você ver, e ausência vira indistinguível de ausência real. **A defesa é ler o JSON
cru** (curl, jq, `response.json()` sem mapeamento) antes de passar pelo cliente que você mantém.

**A mesma família, uma altura abaixo: `ModeCharDevice` responde *"é dispositivo de caractere?"*, não
*"tem gente na frente?"* — e `/dev/null` é dispositivo de caractere.** Achado ao implementar o menu
interativo (T-082, 2026-07-28), cuja regra é *"só abre sem argumento E com terminal dos dois lados"*.
O teste de terminal idiomático em Go — `f.Stat().Mode()&os.ModeCharDevice != 0` — responde **true**
para o dispositivo nulo, **medido nas duas plataformas** (Windows: `Dcrw-rw-rw-`, `char: true`; no
Linux `/dev/null` é `crw-rw-rw-` pela mesma razão). E `/dev/null` é exatamente o que o **systemd**
entrega como entrada padrão (`StandardInput=null`) e o que um script escreve quando não quer a saída.

*Custo: zero, e só porque a pergunta foi feita antes de commitar — o modo de falha seria caro e mudo:
`zapgw >/dev/null </dev/null` abriria o menu, leria EOF na primeira pergunta e o binário **sairia com
status 0 sem subir o servidor**. Um serviço que "iniciou com sucesso" e não está no ar é pior que um
que falhou. Conserto: `isTerminal` (`cmd/zapgw/menu.go`) faz **duas** perguntas — dispositivo de
caractere **e** `!os.SameFile(info, os.Stat(os.DevNull))`, que compara identidade de arquivo e por
isso é imune a redirecionamento. O caso está no teste (`TestWithoutTTYDoesNotOpenMenu`, os dois lados no
dispositivo nulo).*

**A pergunta que generaliza é a mesma do bloco acima, com o alvo trocado: escreva a frase que a
verificação responde de VERDADE, e compare com a frase que você queria.** Aqui a distância entre
*"char device"* e *"terminal"* cabe inteira dentro de `/dev/null` — e o teste que só exercita pipe e
arquivo regular passa verde sem nunca tocar nela.

---

**A armadilha-mãe aplicada ao CONSELHO que damos ao consumidor: a regra valeu quando eu a escrevi e
não valeu quatro horas depois, quando eu mesmo a violei.** Em 2026-07-28, 12:45, escrevemos ao
`consumer-b`: ***"não construa alarme em cima de contador absoluto"*** — porque contador absoluto num
sistema que já rodou traz história dentro, e um limiar `> 0` nasce disparado. Às 16:28 do mesmo dia,
anunciando a `v0.26.0`, escrevemos a ele: *"`conta_descartada` subindo é uma linha de alarme que você
pode ligar hoje"*. **É exatamente o que a regra proíbe, dita pela mesma pessoa, no mesmo canal, no
mesmo dia.**

Quem pegou foi ele, indo implementar e parando: a instância dele **já tinha** `conta_descartada: 1` e
`numero_descartado: 4` — dos **nossos próprios testes de virada**. Um alarme "maior que zero"
nasceria vermelho sobre eventos que ele já sabia o que eram, e *"alarme que nasce aceso é alarme que
se desliga na semana seguinte"* (formulação dele).

*Custo: zero, porque ele leu o próprio histórico antes de codificar em vez de obedecer. Se tivesse
obedecido, teríamos gasto o único alarme automático que ele tem naquele painel — e alarme desligado é
pior que alarme ausente, porque some com a lacuna junto.*

**Por que escapou, e é o que vale copiar:** a regra foi escrita numa mensagem de **projeto** ("como
construir alarme") e violada numa mensagem de **release** ("o que mudou nesta versão"). Contexto
diferente, mesmo assunto — e ninguém relê as regras que deu ao anunciar uma versão. **A pergunta que
economiza a próxima: antes de sugerir que alguém construa algo, procure o que você já disse a ELE
sobre construir aquilo.** O canal é o registro; ele estava lá, com data.

**E o corolário técnico, que sobrevive a este caso:** contador absoluto responde *"quanto já
aconteceu desde sempre"*, e alarme pergunta *"algo mudou agora?"*. As duas perguntas só coincidem num
sistema que nunca rodou. Alarme quer **delta** ou **carimbo** — e é por isso que a `GET /v1/estado`
publica `ultimo_em` junto de cada chave (T-060) e `carimbos_desde` no topo (T-070). *Quem exibir
contador absoluto, exiba-o como o `consumer-b` fez: em destaque e em vermelho acima de zero, com a
frase que diz qual é o valor esperado — visível para quem olha, sem tocar sino para quem não pediu.*

---
**"Recusado" e "INEXPRIMÍVEL" são defesas diferentes, e a mutação provou que a primeira não enxerga o
que a segunda impede.** O contrato de envio teve, por um período de migração, **dois** campos para
parâmetro de botão de template: `botoes_url` (antigo, só URL) e `botoes_template` (união discriminada
por tipo). O estado inválido — *o mesmo botão declarado duas vezes, em dois campos* — era **recusado**
por uma guarda de índice repetido. Recusado, e ainda assim **exprimível**.

*Custo real, e ele não é zero: duas tarefas (T-044 para criar o sucessor mantendo o antigo vivo, T-045
para remover) e uma janela de convivência que durou até os DOIS consumidores confirmarem por escrito
que haviam migrado — um deles com sete templates em produção usando o campo. **Tudo isso porque o
estado inválido nasceu exprimível em vez de impossível.** A alternativa descartada na época era pior:
manter os dois para sempre.*

**O que a mutação da T-045 mostrou, e é a parte que não se adivinha:** ao ressuscitar um segundo campo
de botão, a suíte **inteira ficou verde — inclusive a guarda de índice repetido**. A guarda não
consegue enxergar um segundo campo; ela só sabe comparar índices dentro do campo que conhece. **Só o
teste que pergunta AO TIPO** — varrendo a struct por reflexão, contando campos que são *lista cujo item
tem `Indice`*, e exigindo exatamente um — fica vermelho, **nomeando o campo novo**.

**A pergunta que economiza a próxima: este estado é recusado, ou é impossível de escrever?** Se for
recusado, alguém vai escrevê-lo — e a guarda só pega o caso que ela foi ensinada a ver. *Guarda protege
contra o erro que você imaginou; tipo protege contra o que você não imaginou.*

*(É a mesma forma da lição da T-048 — "toda lista escrita à mão sobre o esquema precisa de algo que
pergunte ao esquema" — aplicada ao struct em vez de ao banco. Quando a mesma frase aparece em duas
alturas diferentes do sistema, ela é regra e não coincidência.)*

---

**A regra "nunca guarde o valor em claro, guarde o HMAC" valeu para DOIS campos da mesma tabela e não
para o TERCEIRO, escrito na mesma tarefa, pela mesma mão.** A T-091 (log de TRÂNSITO, 2026-07-29)
aplicou HMAC-SHA256 com chave ao telefone (`hmac_contraparte`) e ao `wamid` (`hmac_wamid`) — os dois
com o comentário explicando por que hash sem chave não basta e por que `Cofre.Cifrar` não serve. A
COLUNA `correlacao`, ao lado, gravava a `Idempotency-Key` do consumidor **em claro**, no lado da
SAÍDA. É texto livre de origem EXTERNA: nada impede um consumidor razoável de usar
`pedido-5532999990001` ou `cliente-joao-silva-0912` como chave, e a tabela passaria a guardar essa
string por 30 dias — exatamente o que a lista de campos do próprio arquivo proibia ("nunca... nome").
O mesmo valor também saía no `log.Printf` de `Transito.Record`, um SEGUNDO lugar, com retenção do
journal, não da tabela.

*A ironia que vale registrar: a mesma implementação **acertou** a defesa vizinha — recusou
`err.Error()` no campo `desfecho` para não abrir vetor de vazamento por mensagem de erro da Meta, e
documentou o porquê. Abriu o vetor gêmeo um campo ao lado, na mesma struct, no mesmo commit.*

**Como apareceu:** revisão adversarial ANTES do merge (o planner, relendo o diff pronto), não teste
nem produção. Nenhum teste do próprio autor pegou, porque o teste de "não guarda conteúdo"
(`TestTransitoNaoGuardaConteudo`) só existia do lado da ENTRADA — onde a pergunta nem se aplica,
porque ali `correlacao` é um id opaco que o próprio gateway gera. O autor tinha, inclusive,
**sinalizado a própria decisão no relatório final** ("usei a Idempotency-Key como correlacao") — o
sinal estava visível, só não foi comparado contra a regra do arquivo que ele mesmo tinha acabado de
escrever.

*Custo: zero em produção — pego antes do merge, sem tag, sem deploy. O custo que teria cobrado se
passasse: 30 dias de string arbitrária de consumidor num log pensado para durar exatamente essa
janela, mais o mesmo valor no journal (retenção diferente, lida por monitor). Conserto:
`Store.HMACCorrelation` (`internal/config/crypto.go`/`transit.go`), MESMO mecanismo dos outros dois
campos — e um teste por PACOTE que varre TODAS as colunas atrás de uma sentinela plantada na
Idempotency-Key (`TestOutboundTransitDoesNotStoreTheIdempotencyKeyInTheClear`,
`internal/outbound/transit_test.go`), espelhando o que já existia do lado da entrada.*

**A pergunta que generaliza, e ela é a mesma da armadilha-mãe com o alvo em cima de UMA tabela em vez
de um sistema inteiro:** ao proteger um campo com HMAC/cifra, pergunte **"quais OUTROS campos desta
MESMA linha vêm de fora, e eles têm a mesma proteção?"** — não basta perguntar se a proteção está
certa onde ela está; é preciso perguntar onde mais, na mesma struct, a mesma pergunta cabe.

---


## A segunda armadilha deste projeto: "duas verificações que compartilham uma premissa não são duas verificações"

**Levantar defunto: encerramento que não é ESCRITO não encerra — ele vira resíduo que a próxima
leitura do histórico ressuscita.** Em 2026-07-28 o dono encerrou o assunto `consultorio` em conversa,
e ele voltou **duas vezes** no mesmo dia: uma como parágrafo *"o que sobra, e é pequeno"* numa seção de
canal escrita por mim às 09:36, e outra dez horas depois, quando o consumidor releu o histórico, achou
o resíduo e o devolveu — e eu o transformei em tarefa da fila. Palavras do dono ao ver a segunda
ressurreição: *"vcs têm a mania de ficar levantando defunto"*.

**A causa não foi ninguém desobedecer.** Foi que os dois lados fizeram o certo com o registro errado:
transformar pendência de canal em tarefa da fila **é** o procedimento correto — só que aquela pendência
já não existia, e nada no repositório dizia isso. *A dispensa vivia no chat, que morre com a sessão; o
resíduo vivia no doc, que sobrevive.*

*Custo: retrabalho e desgaste do dono, duas vezes. E o custo escondido é pior — cada ressurreição gasta
credibilidade da fila: quem vê item morto voltando começa a ignorar a fila inteira.*

**A regra, e ela é curta:** quando o dono encerrar um assunto, **escreva o encerramento no repositório,
com as palavras dele e a data** — no bloco de retomada, que é o primeiro texto que a sessão seguinte lê.
E escreva **por que** está encerrado: *"decisão do dono"* e *"não havia nada"* são coisas diferentes, e
quem lê daqui a três meses precisa saber qual das duas foi, senão reabre para "confirmar".

**A pergunta que economiza a próxima: eu estou registrando isto como ABERTO porque ele está aberto, ou
porque eu não perguntei se fechou?** Parágrafo do tipo *"o que sobra, e é pequeno"* é onde defunto se
esconde — o diminutivo dispensa a conferência.

---

**Controle de medição pode ter FONTE PERECÍVEL — e quem some é o lado que você não controla.** Em
2026-07-28, antes de provisionar a segunda instância, o mapeamento de colunas foi provado comparando a
linha da Evolution (o LXC da Evolution) com a instância do gateway: os dois `waba_id` e os dois `phone_number_id`
batiam. Controle bom, feito na hora certa. **À noite o dono desligou o o LXC da Evolution** (medido: `stopped`), e
o `consumer-a` avisou — *"a primeira linha não é mais refazível"*.

**O achado maior veio de ir conferir o aviso:** aquela medição **não estava em doc nenhum deste
repositório**. Ela só existiu no arquivo de canal, que é `*.local.md` e **ignorado pelo Git**. Não era
"número sem procedência" — era número sem casa, e sumiria com a sessão. *No mesmo cheque, a pendência
do `consultorio` também não estava na fila: virou a T-077 no mesmo commit.*

*Custo: zero, porque o resultado foi superado por evidência melhor — a instância está em produção
recebendo e entregando (`recebidas 72 = entregues 72` no dia), o que prova o mapeamento com mais força
que a comparação de colunas. **Não congelamos os hashes**, e a decisão é deliberada: preservar um
controle superado cria a ilusão de que ele ainda é a prova.*

**As duas perguntas que ficam:**

1. **Ao fazer um controle, pergunte quanto tempo a FONTE DE COMPARAÇÃO vai existir.** Se ela é uma
   máquina, um serviço de terceiro ou um sistema em desativação, o resultado tem prazo — e o momento de
   congelar (fixture, linha de doc com data e origem) é enquanto os dois lados ainda lembram de onde
   saiu. Depois vira número sem procedência, que é pior que número nenhum.
2. **Medição que só existe em canal não existe.** Canal é `.local.md`, ignorado, e morre com a sessão.
   *Se um número foi usado para DECIDIR alguma coisa, ele pertence a `docs/`, não ao canal.*

---

**A variante que mais me pegou hoje: afirmar sobre O OUTRO LADO do canal sem abrir o arquivo — três
vezes em uma tarde, todas apanhadas pelo próprio consumidor.** Não é a mesma coisa que a armadilha
acima; ali as duas fontes eram minhas e compartilhavam premissa. Aqui **não houve fonte nenhuma** — a
afirmação nasceu de memória sobre um sistema que não é meu para grepar, e por isso a revisão interna
**não tinha como** pegar.

| o que afirmei | como estava | quem derrubou |
|---|---|---|
| *"atrás do `cloudflared` o Traefik vê `127.0.0.1`"* | não há `cloudflared` | o dono, com `nslookup` |
| *"vocês já têm estado por mensagem para saber quais refazer"* | a mensagem de entrada nasce sem `status` lá | o `consumer-a`, citando o arquivo |
| *"os dois consumidores pediram `PUT`"* | só um pediu; o outro nunca citou verbo | o `consumer-a`, com `grep` no próprio canal |

**As três eram acessórias ao argumento** — nenhuma decisão mudou quando caíram. É isso que as torna
perigosas: **premissa que não sustenta a conclusão não recebe conferência**, e sai escrita com a mesma
confiança das que sustentam. Quem lê não tem como saber qual perna foi medida.

*Custo: zero em código, e alto em outra moeda — a terceira delas foi repetida no canal de um TERCEIRO
(*"o do outro também"*), espalhando a atribuição falsa para alguém que não tinha como conferir. Doc e
canal são registro; atribuição errada num registro vira história errada.*

**A regra, e ela é mais estreita e mais fácil de cumprir do que "confira tudo":** se a frase é sobre o
código, o pedido ou o comportamento **do outro lado**, ou você **abre o arquivo / cita a linha**, ou
não escreve a frase. Não existe meio-termo, porque não existe o `grep` que te salvaria depois. *E se a
premissa for acessória — se a conclusão sobrevive sem ela —, o barato não é conferir: é **cortar**.*

---

**O `wamid` CARREGA O TELEFONE DO DESTINATÁRIO DENTRO DELE, em base64 — mascarar `recipient_id` e
deixar o `wamid` produz um arquivo que PARECE mascarado e não está.** Levantado pelo implementador da
T-069 (2026-07-28) ao congelar capturas reais no corpus, e **conferido pelo planner** decodificando um
`wamid` de produção:

```
$ echo "wamid.<a parte depois do ponto, de um wamid REAL>" \
    | base64 -d | strings
55DD9NNNNNNNN           <- o telefone do destinatario, em texto claro
CB8A8835D1365DD0C3      <- o resto, esse sim opaco
```

> **Este bloco NÃO traz um `wamid` real, e a omissão é deliberada** — ele foi escrito em 2026-07-28
> com um valor de produção, e retirado no mesmo dia ao se notar a ironia: um exemplo que **prova**
> que `wamid` carrega telefone não pode carregar um. Para reproduzir, pegue um `wamid` do seu próprio
> journal. *Isto é instância da regra abaixo, cometida por quem a estava escrevendo.*

O número sai na forma canônica do WhatsApp (para o Brasil, **sem o nono dígito**), então uma busca
pelo número como o humano o escreve — `55329...` — **não acha nada**, e a revisão passa limpa. É a
mesma família de *"este método acharia um positivo?"* (ver a segunda armadilha deste projeto): quem
grepa o número na forma que conhece conclui "não vazou" sobre uma busca incapaz de achar.

*Custo: zero, e só porque o implementador abriu o `wamid` em vez de confiar no próprio mascaramento.
Se tivesse trocado só `recipient_id` e `display_phone_number`, o telefone teria entrado no Git dentro
do id — e **Git não esquece**: a correção não seria `git rm`, seria reescrever histórico ou aceitar o
vazamento.*

**A regra, e ela vale para todo dado que vira fixture:** ao mascarar, **decodifique todo campo que
possa ser identificador estruturado** antes de decidir que ele é opaco. `wamid` é base64; um id que
"parece aleatório" pode ser uma estrutura com dado dentro. **A pergunta que economiza a próxima: este
campo é opaco, ou eu só não tentei abrir?**

🔥 **2026-08-30 — DEIXOU DE SER HIPOTÉTICA: ela já tinha mordido, num repositório de CONSUMIDOR, e
ninguém sabia.** Num teste de canal com o `consumer-b`, eles mascararam o telefone do dono no corpo
do pedido (`553298463XXXX`) e coláram o `wa_message_id` **inteiro** duas linhas acima — cujo payload
em base64 é o mesmo número, completo. *O cuidado deles foi real e mirou o lugar certo; o que falhou
foi o modelo mental de que `wamid` é opaco.*

Avisamos pelo canal. **Eles foram procurar com o `grep` que decodifica — e acharam um `wamid`
COMMITADO no repositório deles, num teste de webhook, de semanas antes**, carregando o mesmo número.

*Custo real: um telefone pessoal dentro do histórico de outro repositório, descoberto por acaso
semanas depois — e a correção lá é a mesma que seria aqui: reescrever histórico ou aceitar. Zero
prejuízo imediato (repo privado), e nenhum crédito para nós: **o aviso não evitou, ele revelou**.*

**E o desfecho, medido no mesmo dia, é o melhor argumento deste arquivo inteiro:** eles escreveram a
própria guarda com decodificação e ela achou **quatro** ocorrências, não uma. Duas eram `wamid`
(*"o mesmo número em posição diferente do payload gera base64 diferente"* — então o `grep` pelo
fragmento do primeiro não achava o segundo); uma era o `de_cru` de **12 dígitos** em fixture, que a
busca deles nem procurava. **E as outras duas estavam dentro de textos que descreviam ESTA
armadilha** — um comentário de 28/07 que explicava *"dentro do `wamid` está o telefone em base64"* e
trazia o número em texto claro na linha seguinte, e a entrada do `ARMADILHAS.md` deles sobre o
assunto, escrita **uma hora antes** da guarda existir, ilustrada com o `wamid` real.

🔥 **Conheciam a armadilha havia um mês, escreveram sobre ela duas vezes, e vazaram nas duas —
inclusive dentro do texto que a descrevia.** *Saber não protege; a guarda protege.* É a mesma frase
do `CLAUDE.md` do repo público (*regra sem mecanismo é decoração*) com um custo real atrás, e a
melhor formulação da defesa é deles: **a guarda guarda só o SHA-256 dos números — uma guarda que
carrega o número que protege é a própria coisa que ela proíbe.**

🔴 **As duas lições, e a segunda é a que muda comportamento:**

1. **Aviso sobre formato viaja mal e chega tarde.** Este bloco existe desde 2026-07-28 e descreve
   exatamente o vazamento que aconteceu do outro lado. Ele não alcançou quem precisava porque **vive
   no nosso repositório** e o consumidor lê o `CONTRATO-CONSUMIDOR.md`. *Armadilha que só o mantenedor
   conhece protege só o mantenedor.* — quando um formato NOSSO carrega dado sensível, o aviso é
   obrigação do contrato, não nota interna.
2. **O portão que decodifica `wamid` só existe aqui.** `TestNoPhoneNumberOutsideTheAllowlistInTheRepo`
   varre `cmd/`, `internal/` e `testdata/` deste repositório. Nenhum consumidor tem equivalente, e
   **cada um deles guarda `wamid` de produção por desenho** — é o id que usamos para pedir que eles
   deduplifiquem. *Nós distribuímos o formato; o portão ficou em casa.*

---

🔥 **VARIANTE DA MESMA ARMADILHA, e ela pegou quem estava escrevendo sobre ela (2026-08-20): "decodifiquei os 49 `wamid` e nenhum carrega o telefone" — falso, e as três razões são independentes.**

O planner varreu a árvore decodificando `wamid`, reportou **zero** ocorrências do telefone, **e rodou
um controle positivo que passou**. Os dois fatos eram verdadeiros; a conclusão, falsa. O número real
de um terceiro estava num `wamid` de `internal/config/transit_test.go` o tempo todo, em base64, e só
apareceu quando o implementador da T-161 abriu aquele arquivo por outro motivo.

**Três defeitos, e cada um sozinho bastava para o "limpo" sair errado:**

1. **Falha de decodificação foi tratada como ausência.** O valor no arquivo é
   `wamid.` + prefixo + base64-do-telefone + metadados. Decodificar a captura inteira a partir do
   começo estoura `Incorrect padding`; o código pegava a exceção, seguia, e contava aquele `wamid`
   como limpo. *O pedaço certo, isolado, decodifica sem esforço nenhum.*
   **`não consegui abrir` ≠ `está limpo`** — é a regra do monitor cego, aplicada a um decodificador.
2. **O controle positivo tinha a forma do INSTRUMENTO, não a forma do DADO.** O `wamid` fabricado
   para o teste tinha o base64 começando no offset zero e com padding correto — exatamente o caso que
   o decodificador sabia tratar. **Controle assim prova que o instrumento roda, não que ele enxerga.**
3. **A varredura procurava o número ERRADO.** O número foi extraído do `README.md` por regex, e o
   `README` já continha o **sintético** — que casou primeiro. Mesmo um decodificador perfeito teria
   respondido, com precisão, sobre o número que não importava.

*Custo real: a medição foi entregue ao dono como fato e usada para ENCOLHER o escopo da T-159 —
"`wamid` não precisa ser regenerado". O telefone de um cliente do consumidor ficou na árvore, em 7
arquivos, **dois deles de produção**, até a T-161 achá-lo por outro caminho. Não chegou a público
porque o repositório ainda é privado; num repo aberto, seria irreversível.*

**As três regras que sobram, e a segunda é a que quase ninguém aplica:**

- **Falha de leitura conta como ACHADO, não como limpo.** Varredura que não conseguiu abrir um valor
  reporta aquele valor, para um humano olhar. O silêncio é reservado para o que foi lido e estava bom.
- **Controle positivo se faz com um exemplar REAL do dado, não com um fabricado.** Pegue um `wamid`
  do próprio corpus, esconda nele o valor que você procura, e prove que a varredura o acha *naquela
  forma*. Fabricar o controle a partir do que o seu código já trata é escrever a prova a favor do réu.
- **Confira QUAL valor você está procurando antes de concluir sobre ele.** Extrair "o número" por
  regex de um arquivo que contém vários devolve o primeiro, não o certo.

✅ **Buraco FECHADO pela T-162** (`internal/config/phones_allowlist_test.go`). O portão da T-161
casava só `\b55[0-9]{10,11}\b` — número literal; um telefone dentro do base64 de um `wamid` passava
por ele. Agora cada linha é varrida duas vezes: pelo literal (como antes) e por
`phoneNumbersInsideTheWamid`, que decodifica todo `wamid.<payload>` achado — tentando os DOIS únicos
comprimentos de janela que correspondem a 12 ou 13 dígitos ASCII (16 ou 18 caracteres de base64), em
todo deslocamento possível, nunca a captura inteira de uma vez (o que estourava padding). Os números
que saem de qualquer uma das duas frentes passam pela MESMA `syntheticPhoneAllowlist` — uma
lista só. Falha de decodificação (nenhuma janela produziu texto legível) é achado, não ausência: o
teste reprova pedindo olho humano, marcado `NAO DECODIFICOU`.

🔴 **E o mecanismo achou um segundo número real no mesmo movimento de construí-lo — não é hipótese, é
o que aconteceu.** Em `cmd/zapgw/transit_test.go`, uma tarefa anterior já tinha trocado o literal
`numero` pelo sintético `5511999990000`, mas a constante `wamid` na linha seguinte, com o MESMO
telefone embutido em base64, ficou intocada — porque nenhuma varredura até então olhava dentro do
base64. Decodificado: um número real de terceiro, DDD 32, terminado em `...10` (diferente do número
de `(32) 9xxxx-xx72` que a T-161 já tinha achado e trocado — este é OUTRO número, não o mesmo
reaparecendo). Corrigido no mesmo commit: o `wamid` foi regravado com o mesmo prefixo `HBgN` e o
mesmo sufixo de metadados do valor original, só trocando o trecho do telefone para bater com o
`numero` já sintético da linha acima. *Confirma a lição da variante 🔥 acima: "decodifiquei e não
achei nada" só vale para o que a decodificação realmente alcançou — o `numero` literal já limpo ao
lado de um `wamid` sujo é exatamente o tipo de vizinhança que engana quem só confere o campo óbvio.*

**Armadilha nova, encontrada ao construir o controle positivo:** decodificar em VÁRIOS comprimentos de
janela (não só um) produz um número "quase certo" que não é uma forma real do telefone — é o mesmo
número cortado no último dígito pela janela mais curta. A primeira versão desta função tentava
qualquer comprimento de 12 a 24 caracteres de base64 e, para o `wamid` sintético de
`internal/config/transit_test.go`, produzia DOIS achados: o número certo (13 dígitos) e um segundo
"número" que é só o mesmo com o último dígito cortado — nunca declarado em lugar nenhum porque nunca
existiu. A correção restringe a busca aos DOIS únicos comprimentos que correspondem a 12 ou 13 dígitos
ASCII (16 ou 18 caracteres de base64) e, em cada deslocamento, prefere o de 13 dígitos — só cai para o
de 12 se o de 13 não decodificar de forma legível ali. *Regra que generaliza: ao extrair um dado de
tamanho fixo por janela deslizante, testar comprimentos "próximos" do certo não é mais rigoroso — é o
jeito de inventar um achado que não existe.*

**O controle positivo usa um `wamid` REAL do corpus** (`internal/config/transit_test.go`,
`wamid.HBgNNTUzMjk5OTk5MDAwMBUCABIYFjNFQjBEO`, que decodifica para o sintético `5532999990000`, já
allowlisted) — troca só o trecho do telefone por um número fora da allowlist, preservando prefixo e
sufixo originais, e prova que a varredura o acha apontando arquivo e linha. Fabricar um `wamid` do
zero, com o base64 alinhado no offset zero, é exatamente o erro que produziu o "limpo" errado
anterior — ver a variante 🔥 acima.

**E o corolário que não é sobre fixture, e é o que mais vale:** o `wamid` viaja no envelope, nos logs
e no banco dos consumidores. Ele **não é anônimo** — tratá-lo como identificador neutro em log, ticket
ou mensagem de erro publica o telefone junto. *Quem cola um `wamid` num canal, num issue ou num print
está colando o número.*

🔴 ***E o custo deixou de ser hipotético no mesmo dia.*** O aviso foi mandado ao consumidor
`consumer-b` em 2026-07-28 17:42 como nota de rodapé (*"se você loga `wamid`, vale saber"*). Ele foi
conferir e **estava logando o `wamid` em TODA linha de evento processado** — o telefone de cada
cliente, em texto claro dentro do id, em produção. Corrigido e no ar em minutos (passou a logar a pk
interna; a rastreabilidade sobreviveu porque o id inteiro continua no banco).

***O que fez o aviso funcionar foi o detalhe que quase ficou de fora:*** a observação de que o número
sai **sem o nono dígito**. Palavras dele: *"sem isso eu teria grepado o número do jeito que a gente
escreve, não achado nada, e concluído que estava limpo"*. **Aviso resumido teria produzido uma busca
que não acha e uma conclusão errada** — é a terceira pergunta da segunda armadilha (*este método
acharia um positivo?*) aplicada por um terceiro, sobre o sistema dele. **Ao avisar de um vazamento,
mande o formato exato do que procurar, não só o nome do campo.**

---

> **A formulação é do consumidor `consumer-b`, 2026-07-28**, ao responder por que uma convergência o
> tinha enganado: ***"o que separa é a FRONTEIRA, não a quantidade."*** Ela unifica cinco episódios do
> mesmo dia que pareciam problemas diferentes, e é por isso que ganhou seção própria em vez de virar
> mais um parágrafo.

Conferir duas vezes só vale se as duas conferências **puderem discordar**. Duas checagens do mesmo
lado de uma fronteira são uma checagem com dois nomes — e são **piores** que uma, porque a
concordância entre elas produz confiança que nenhuma das duas merece sozinha.

Os cinco casos de 2026-07-28, todos com a mesma anatomia:

| o que foi conferido | as duas "fontes" | a premissa que as duas compartilhavam | como apareceu |
|---|---|---|---|
| o `app_secret` na virada | teste de fumaça + `/v1/health` | *"o segredo que EU tenho é o que a Meta usa"* | a Meta assinando com o antigo, 8 min de recusa |
| a virada ter dado certo (consumidor) | o banco dele + o log dele | ambos ficam **depois** da nossa entrega | *"duas fontes, uma cegueira só"* — ele só soube porque nós contamos |
| alarme visual cinza (consumidor) | atributo no DOM + teste de render | *"a variável CSS `--co-bad` existe"* | só a cor **computada** no navegador acusou |
| o corpus cru ter congelado (`consumer-a`) | `LIKE '%entry%'` vs `LIKE '%instancia%'` | *"a forma da linha guardada diz o que há dentro"* | o `cru` estava lá, em base64 |
| `:8443` fechado para a internet | dois `curl` "pelo IP público" | *"esta máquina está fora da rede"* — e não está | hairpin NAT; `api.ipify.org` devolveu o mesmo IP |

**Var CSS inexistente não quebra** — o navegador descarta a declaração e o número herda a cor normal.
**`LIKE` em base64 não acha a palavra.** **Hairpin não sai da rede.** Em todos, o instrumento respondeu
com confiança sobre uma pergunta que ele não era capaz de responder.

**As três perguntas que quebram o padrão, e elas são diferentes entre si:**

1. **"Estas duas fontes conseguiriam discordar?"** Se não, você tem uma. Conte fronteiras, não fontes.
2. **"Esta medição responde a pergunta que eu vou fazer com ela?"** — a formulação do `consumer-a`:
   *nomeie o que a medição responde ANTES de olhar o número*, porque depois o número já convenceu.
3. **"Este método acharia um positivo?"** Ausência só vale como prova depois que a busca foi
   **calibrada contra um positivo conhecido** — foi assim que a T-066 provou o `:8443` fechado (12 nós,
   com o `:443` como controle) e assim que o `consumer-a` refez o `forwarded = 0` decodificando o
   base64.

**O corolário que este projeto já usava sem ter nomeado: prova de verdade é a CONTRAPARTE.** Não é
"mais uma checagem" — é uma checagem **do outro lado da fronteira**, que é o único lugar de onde a
premissa compartilhada é visível. Por isso um `curl` de fora vale mais que dez de dentro, o relato do
consumidor vale mais que o nosso journal, e a liberação escrita **citando a linha** vale mais que a
suíte verde. *É também por isso que este projeto segura deploy esperando resposta de consumidor: o
custo é minutos, e é a única evidência que a suíte não sabe produzir.*

**E o caso em que a fronteira foi respeitada, para servir de modelo:** em 2026-07-28, o `consumer-b`
mediu `recebidas 56 = entregues 56` **na nossa produção**, do container dele; nós conferimos do lado do
gateway, com o journal e os contadores — duas leituras genuinamente de lados opostos, que **poderiam**
ter discordado e não discordaram. É isso que "conferir duas vezes" deveria significar sempre.

---

🔥 **O valor de um endpoint de LEITURA não é listar — é DISCORDAR. E foi um `GET` que ninguém achava
importante que achou a causa raiz, em quinze segundos.**

2026-08-20, no consumidor `consumer-b`, uma hora depois de ele ter escrito para nós a frase *"o
sintoma de um bloqueio que funcionou e o de um que falhou são o mesmo silêncio"*. Um `Enter` num campo
de texto submeteu o formulário **sem `submitter`**; o `onsubmit` fez `event.submitter.value`, **lançou**
— e `onsubmit` que lança **não cancela o envio**. O `confirm()` nunca apareceu, o campo `alcance` não
foi enviado, e o servidor tinha `request.POST.get("alcance") or LOCAL`: **ele escolheu por conta
própria**.

Resultado: pediu-se bloqueio na Meta, gravou-se local, a tela disse "bloqueado", **a auditoria gravou
"bloqueou"** — e o número seguiu escrevendo. *Nossa rota nunca foi chamada.* Nenhuma camada mentiu;
cada uma disse a verdade do seu pedaço.

🔑 **O que quebrou o silêncio foi o `GET /v1/bloqueios`**, que respondeu `{"total":0}` enquanto o banco
deles dizia "bloqueado". **Duas fontes que NÃO compartilham premissa**, discordando — é a única coisa
que transforma *"acho que não funcionou"* em causa raiz. Um endpoint que eles tinham catalogado como
*"auditoria, não render"* foi a primeira ferramenta que pegaram.

**A regra, e ela é o argumento para construir leitura mesmo quando parece inútil:** *o seu banco não
consegue te contradizer — ele só repete o que você escreveu nele.* Só uma leitura da **fonte externa**
pode discordar. Um `GET` que espelha a verdade do terceiro é barato, não tem efeito colateral, e é a
única testemunha independente que existe quando a escrita falha em silêncio.

*Este projeto já tinha o mesmo padrão escrito noutro lugar sem nomeá-lo: o contrato diz que
`GET /v1/templates` é "a porta de conferência quando o `POST` termina ambíguo". **É a mesma regra, e
agora ela tem dois casos.** Ao acrescentar qualquer escrita nova, pergunte: existe leitura capaz de
contradizê-la? Se não existir, o único detector de falha é alguém reclamar.*


## Go / concorrência

**`go test` NÃO detecta corrida se nenhum teste for concorrente — e a suíte fica verde.** O contador
de correlação do handler fazia `h.seq++` num `*Handler` compartilhado; o `http.Server` atende cada
requisição numa goroutine sobre o **mesmo** handler. `go test -race ./...` passava limpo, porque
**nenhum teste disparava requisições concorrentes**. Só apareceu quando o revisor escreveu um teste
com 200 goroutines. Corrigido com `atomic.Int64`.
*Custo: um Critical. O detector estava disponível o tempo todo e não tinha o que detectar.*

**Todo `Handler` HTTP com estado mutável precisa de um teste concorrente rodado sob `-race`.** Sem
ele, `-race` é teatro.

---

## Go / JSON

**`json.Unmarshal` de `null` num map NÃO devolve erro — deixa o map `nil`.** E `null`, `42`, `[]`,
`"texto"` e `true` são JSON **sintaticamente válidos**. O código seguinte, que assume dados, segue
adiante achando que tem. Tem de virar erro nomeado (`ErrBodyNotObject`) com uma checagem explícita
de `nil`.
*Custo: pego por teste escrito antes do código. O equivalente em Python (`json.loads("null")` não
levantar `ValueError`) já custou um Critical noutro projeto desta rede — a armadilha atravessou a
fronteira de linguagem mudando de mecanismo e mantendo o desfecho.*

**Um item `null` ou `{}` dentro de uma lista também não falha o `Unmarshal`** — vira struct zerada.
No parser, isso produzia um `Evento` com `ID == "msg:"`; **dois deles colidiam no dedup do
consumidor**, contradizendo a promessa de unicidade escrita no próprio tipo. Guarda: item sem `id`
da Meta não vira evento, é contado como ignorado.
*Custo: pego na revisão da T5, antes de qualquer consumidor existir.*

**Um único `Unmarshal` sobre uma árvore inteira falha por inteiro.** Se a estrutura tem níveis
(`entry` → `changes` → `messages`), cada nível precisa ser `json.RawMessage` e desserializado por
conta própria — senão um campo com tipo errado em qualquer folha apaga tudo.
*Custo: **o Critical nº 1**. A Meta batcha `entry` de contas diferentes na mesma chamada, então um
payload malformado de um cliente apagaria as mensagens válidas de outro.*

**E o mesmo raciocínio vale para um campo NOVO que a Meta acrescenta dentro de uma folha já
existente — não só para os níveis que já tinham `RawMessage`.** A T-028 (motivo de falha no status)
podia ter modelado `errors[]` como `[]erroMeta` direto dentro de `statusMeta`; um `code` chegando
como string (a Meta já fez isso em outro campo — ver `tolerantInt` em `errors.go`) faria o
`Unmarshal` do item **inteiro** de `statuses[]` falhar, e a guarda de `ignorados++` já existente
descartaria o status **inteiro** — id, status e timestamp junto, não só o motivo. Conserto
preventivo: `statusMeta.Errors` é `[]json.RawMessage`, e cada item é desserializado à parte; um item
de erro malformado deixa `Evento.Erro` nil (perde só o motivo) sem derrubar o resto do evento.
*Custo: zero — achado ao projetar a T-028, aplicando a mesma pergunta da armadilha-mãe ("onde mais
essa regra deveria valer?") a um campo que nem existia ainda quando a regra foi escrita. Provado por
`TestParseWebhookInAStatusErrorAMalformedItemDoesNotBringDownTheEvent`
(`internal/meta/parse_test.go`): revertendo `statusMeta.Errors` para `[]erroMeta` seria a
"simplificação" óbvia que reabre o buraco.*

**E o oposto também é verdade, e por pouco não virou uma linha de código falando um mecanismo que não
existia: um objeto ANINHADO que pode faltar não precisa de ponteiro só porque "pode faltar".** Ao
escrever a T-029 (`error_data.details` no motivo do status), o primeiro rascunho de `erroMeta.ErrorData`
foi `*errorDataMeta`, com um comentário afirmando que só o ponteiro distinguia "`error_data` ausente"
de "`error_data: {}`". A afirmação nunca foi confirmada contra o `encoding/json` — e é falsa: um teste
de experimento (`json.Unmarshal` de campo ausente, de `error_data: null` e de `error_data: {}` sobre
uma struct **plana**) mostrou os três casos como no-op idêntico, sem erro — o pacote documenta que
`null` sobre qualquer tipo que não seja ponteiro/interface/map/slice não tem efeito. Como a Detalhes
final também não distingue "objeto ausente" de "objeto presente sem `details`" (as duas viram `""`,
por decisão da própria tarefa), o ponteiro não protegia nada que a struct plana já não resolvesse;
ficaria um comentário afirmando um motivo que o código não tinha.
*Custo: zero — pego ANTES do commit, ao aplicar a doutrina deste projeto ("confira cada afirmação
contra o código, nunca contra a doc antiga") ao **próprio comentário que eu estava escrevendo**, não só
ao código alheio. Conserto: `erroMeta.ErrorData` é `errorDataMeta` (struct plana); só um `error_data`
de **tipo** errado (ex.: string) derruba o item inteiro, e esse caso já tinha guarda (item malformado →
`Evento.Erro` fica `nil`, mesma família da entrada acima). **A pergunta que generaliza: antes de dar a
um campo um ponteiro "porque pode faltar", pergunte que DIFERENÇA de comportamento o ponteiro
compraria — se a resposta é nenhuma, é cerimônia, não defesa.***

**As duas entradas acima parecem contraditórias (uma pede `RawMessage`, a outra pede struct plana) —
e a diferença é a PROFUNDIDADE do campo, não o campo em si.** A T-041 (`pricing` no webhook de
status, virando `cobranca` no envelope) precisou das duas regras ao mesmo tempo, uma em cada nível:
`statusMeta.Pricing` é `json.RawMessage` (regra do parágrafo de `errors[]`, acima) porque "pricing" é
um campo **irmão** de `id`/`status`/`timestamp` dentro de `statusMeta` — sem isolamento, um `pricing`
de tipo errado quebraria o `Unmarshal` do status **inteiro**, os mesmos campos que sobreviveriam a um
`errors[0]` malformado. Já dentro dele, `pricingMeta.Billable` continua uma struct plana (nem
`pricingMeta` inteiro é ponteiro) porque, uma vez isolado pelo `RawMessage` de fora, não há mais
vizinho para proteger — a mesma lógica de `errorDataMeta`. **A pergunta que decide qual das duas
regras vale para um campo novo: ele é irmão de campos que já funcionam hoje (RawMessage), ou está
aninhado DENTRO de algo que já isola (struct plana)?** Provado pelos dois lados:
`TestParseWebhookAMalformedStatusPricingDoesNotBringDownTheEvent` (`internal/meta/parse_test.go`) fica
vermelho se `Pricing` virasse `pricingMeta` direto; `TestParseWebhookAStatusWithNullPricingGetsNoBilling`
prova que `"pricing": null` não pode virar uma `Cobranca` zerada (mesmo raciocínio do "erro item
malformado", aplicado a null em vez de tipo errado).

**Tipagem defensiva aplicada num campo e não no vizinho: a regra da entrada acima existia, escrita e
provada, e o campo ao lado ficou sem ela por dois planos.** A T-043 (2026-07-26) pôs `json.RawMessage`
em `entry.time` *justamente* porque um tipo divergente ali derrubaria o lote todo — e no mesmo
arquivo, dois níveis abaixo, `mensagemMeta.Context` continuava struct **plana**, com a defesa
faltando exatamente onde o payload é do cliente. Consequência de um `"context"` (ou de qualquer
campo dentro dele) chegar com **tipo** diferente do esperado: o `Unmarshal` da **mensagem inteira**
falhava e ela virava `ignorados++` — some de `eventos`, que é a lista pela qual todo consumidor
deduplica e age. O `cru` ainda era entregue, com `parse_error` preenchido
(`internal/inbound/handler.go:194`), mas o contrato manda o consumidor agir por `eventos`: na
prática, a mensagem da cliente ficava invisível. **Sem `ALARME` e sem contador** — só uma linha de
journal, e este arquivo já registra que ninguém lê journal por hábito. `midiaMeta.Voice` (`*bool`)
tinha o mesmo formato frágil desde o **plano 1**, o que faz o par ser a assimetria clássica: a
mesma frase valia em três campos e não valia em dois, todos no mesmo arquivo.

*Custo: **desconhecido por construção, que é pior que "zero"** — o defeito não produz resultado
ERRADO, produz resultado AUSENTE, e nenhum dos dois consumidores tem como notar a falta de uma
mensagem que nunca chegou. Não há evidência de perda real, e também não haveria como haver. Ele
viveu de 2026-07-23 (`voice`) e 2026-07-26 (`context.id`, T-032) até 2026-07-28 (T-061).*

**O modo de descoberta é o que vale copiar, e é um SÉTIMO jeito:** ninguém revisou este código, e
nenhum tráfego novo chegou. O implementador da **T-059** foi acrescentar dois campos (`forwarded`,
`frequently_forwarded`) ao `contextMeta` e **recusou-se a proteger só os campos novos**, porque
isolar o novo deixando o antigo exposto seria a armadilha-mãe deste arquivo com o sinal trocado —
escreveu a recusa no comentário do próprio código e abriu a tarefa. Ou seja: **o gatilho foi
ACRESCENTAR campo a uma struct que já existia.** Vale como passo de revisão barato — *ao pendurar
um campo novo num struct de parse, pergunte que profundidade de isolamento os IRMÃOS dele têm, não
só o que o campo novo precisa.* É irmão do quinto jeito (dizer em voz alta a assimetria que você
mesmo acabou de criar), com a diferença de que aqui a assimetria **já estava lá** e a tarefa nova só
passou por cima dela.

*Conserto (T-061): `mensagemMeta.Context` e `midiaMeta.Voice` viram `json.RawMessage`, lidos por
`contextoDaMensagem` e `tolerantBool` (`internal/meta/parse.go`) — a primeira virou
`blocoDaMensagem[T]` na T-062, logo abaixo, e por isso não existe mais com esse nome; tipo
inesperado degrada **o bloco**, com a mensagem entregue. `contextMeta` **continua struct plana** — pela regra de
profundidade da entrada acima, agora que o campo de fora isola. **Isto muda comportamento
observável** (`responder_a` passa a faltar sozinho em vez de sumir junto com a mensagem) e por isso
está em `docs/CONTRATO-CONSUMIDOR.md`, "Mudanças que quebram".*

**Quatro mutações, feitas e revertidas antes do commit — e a terceira e a quarta valem mais que as
duas óbvias:** (1) voltar `Context` para `contextMeta` deixa
`TestParseWebhookAContextOfTheWrongTypeDeliversTheMessageWithoutCountingIgnored` e dois fixtures do corpus
vermelhos, com `ErrPartialParse` e `len(evs)` caindo; (2) voltar `Voice` para `*bool` faz o mesmo com
o fixture de áudio; (3) fazer `contextoDaMensagem` aproveitar o que o `encoding/json` **já tinha
decodificado antes do erro** (ele continua decodificando e só devolve o `UnmarshalTypeError` no fim
— confirmado por experimento, não suposto) deixa
`TestParseWebhookAnUnreadableContextDiscardsTheWHOLEBlockNotJustTheBadField` vermelho: **é a única prova de
que "o bloco degrada inteiro" é decisão, e não acidente**; (4) trocar o `var b *bool` de
`tolerantBool` por `var b bool` deixa `TestParseWebhookANullVoiceDoesNotBecomeFalse` vermelho, com
`voz: false` no lugar de ausente — `null` sobre um `bool` não dá erro e não é no-op quando o
destino é um valor, e essa é a mesma família da primeira entrada desta seção.

**A pergunta que teria pego, e ela é grátis:** ao escrever `json.RawMessage` num campo *"porque um
tipo inesperado aqui derrubaria o resto"*, olhe os **irmãos declarados na mesma struct** e pergunte
por que a frase não vale para eles. Se a resposta for "ninguém teve motivo de pensar nisso", você
achou o próximo.

**E a pergunta acima foi feita, respondida e ainda assim a classe ficou aberta por mais uma rodada —
porque perguntar identifica o próximo CAMPO, e o que estava faltando era mover a FRONTEIRA.** É a
entrada mais cara desta seção pelo que ela custou em *processo*, não em produção: **a mesma defesa
foi aplicada TRÊS VEZES, campo a campo, antes de alguém tratar a classe.**

| Rodada | Campo blindado | O que ficou aberto |
|---|---|---|
| T-043 (26/07) | `entry.time` | os 15 campos de `messageMeta`, dois níveis abaixo, no mesmo arquivo |
| T-061 (28/07) | `mensagemMeta.Context` e `midiaMeta.Voice` | os 13 irmãos restantes, na **mesma struct** |
| T-062 (28/07) | *a struct inteira* | os vizinhos de OUTRAS structs — medidos e listados abaixo |

**O custo, contado: duas tarefas inteiras e uma descoberta acidental.** A T-061 só existiu porque o
implementador da **T-059** foi pendurar dois campos em `contextMeta`, reparou que os irmãos estavam
desprotegidos e abriu a tarefa em vez de blindar só o que ele estava mexendo — ninguém tinha ido
procurar. A T-062 só existiu porque o implementador da **T-061**, ao terminar, mediu os vizinhos com
`ParseWebhook` e reportou `"text":"oi"` · `"audio":"x"` · `"interactive":"x"` · `"reaction":"x"` ·
`"button":"x"` → `len(evs) = 0` + `ErrPartialParse`, em vez de consertar calado ou deixar quieto.
**Nas duas vezes, o achado veio de alguém que estava de passagem pelo arquivo.** Nenhuma revisão,
nenhum teste e nenhum tráfego encontrou isso em cinco dias, e a suíte esteve verde o tempo todo.

*Custo em produção: **desconhecido por construção**, como na entrada acima — o defeito produz
resultado AUSENTE, não errado, e nenhum consumidor tem como notar a falta de uma mensagem que nunca
chegou. Ele viveu desde o plano 1 (2026-07-23) até 2026-07-28.*

**A lição que generaliza, e ela é diferente da pergunta grátis acima:** *"onde mais essa frase
deveria ser verdadeira?"* devolve uma **lista de campos**, e consertar uma lista deixa a lista de
amanhã aberta. Quando a resposta for *"em todos os irmãos desta struct"*, a correção não é blindar
os irmãos um a um — é **fazer a struct inteira ser a fronteira**, e deixar para trás algo que fique
VERMELHO quando o próximo campo nascer. A pergunta que fecha: **"o que aqui vai avisar a próxima
pessoa, sem depender de ela ter lido isto?"**

*Conserto (T-062): todo campo de `messageMeta` (`internal/meta/parse.go`) é `json.RawMessage`, sem
exceção — um `json.Unmarshal` cujos campos são todos `RawMessage` não tem como falhar sobre um
objeto JSON qualquer. Cada bloco é lido por `blocoDaMensagem[T]`, que é a `contextoDaMensagem` da
T-061 com o tipo aberto (**a mesma função, não um segundo mecanismo** — um helper por campo é
justamente o que faz a rodada seguinte blindar só o campo da vez). O que fica para a próxima pessoa
é `TestMessageMetaIsolatesEveryFieldByConstruction` (`internal/meta/parse_test.go`), que percorre a
struct por reflexão e falha citando o nome do campo no dia em que alguém pendurar ali um tipo
concreto; e `TestParseWebhookNoFieldOfTheWrongTypeErasesTheMessageNorItsSiblings`, que varre **as chaves
do próprio payload** em vez de uma lista escrita à mão.*

**Uma distinção nasceu daqui e vale por si: "bloco AUSENTE" e "bloco ILEGÍVEL" não são a mesma
coisa, e confundi-los apaga mensagem.** As duas produzem o mesmo envelope (o bloco não aparece), mas
dizem coisas diferentes sobre *quem* falhou: ausente é a **Meta** afirmando que não há bloco —
`type:"reaction"` sem alvo é payload que não fecha, e continua sendo descartado; ilegível é o
**nosso** parser não entendendo o que veio, e isso pode muito bem ser um formato **novo**, não um
payload quebrado. Apagar a mensagem porque não entendemos um bloco dela é cobrar do consumidor o
preço da nossa defasagem — com `200` respondido à Meta, que por isso nunca reenvia. `blockState`
(`internal/meta/parse.go`) tem três valores por causa disso, e `null` conta como **ausente**: é a
Meta dizendo com todas as letras que não há bloco.

**Os VIZINHOS que a T-062 deixou abertos, medidos com `ParseWebhook` no dia do conserto e escritos
aqui para não dependerem de alguém tropeçar neles.** A pergunta *"onde mais essa mesma frase deveria
ser verdadeira, e é?"* foi feita e a resposta é **em mais quatro lugares, e não é**:

| Struct | Campo com tipo inesperado | O que se perde | Raio |
|---|---|---|---|
| `valueMeta` | `metadata` ou `contacts` | **o `change` inteiro** — todas as mensagens E todos os status daquele lote | pior que o defeito que a T-062 consertou |
| `changeMeta` | `field` (string) | o `change` inteiro, idem | pior |
| `statusMeta` | `id`, `status`, `timestamp`, `recipient_id` | o evento de status | igual ao de mensagem, no irmão direto do mesmo laço |
| `entryMeta` | `id` (o `waba_id`) | o `entry` inteiro (as irmãs de outros `entry` seguem) | pior |
| `templateStatusMeta` | `message_template_name`, `reason`, `event`, ... | o evento de template | igual |

Medição literal (mensagem boa + irmã boa no mesmo lote, um campo trocado por vez):
`value.contacts` → `len(evs)=0`; `value.metadata` → `0`; `contacts[].profile` → `0`; `change.field`
→ `0`; `status.id`/`status.status`/`status.timestamp`/`status.recipient_id` → a irmã sobrevive e o
status some; `template.reason`/`template.name` → `0`. **`valueMeta` é o mais caro dos cinco: um
`"contacts":"x"` apaga o lote inteiro de mensagens de um cliente, que é exatamente o Critical nº 1
deste arquivo com outro nome.**

*Três mutações, feitas e revertidas antes do commit. **A terceira encontrou mais do que provava:**
(1) devolver `mensagemMeta.Text` ao tipo concreto deixa `texto_de_tipo_errado_sintetico.json` e o
sub-teste `text` da varredura vermelhos, com `len(evs)` caindo de 2 para 1, e o teste de reflexão
vermelho citando `mensagemMeta.Text`; (2) o mesmo com `Reaction` → `*reactionMeta`; (3) fazer
`messageBlock` tratar `null` como lido não deixou teste vermelho — **derrubou a suíte inteira com
um `panic` de nil dereference**, porque `*p` sobre um `null` não tem para onde apontar. O
comentário no topo de `parse.go` promete que este parser **nunca entra em pânico**, e o `p == nil`
era, sem ninguém ter notado, o que sustentava essa promessa além da semântica. **A lição: mutação
que dá pânico em vez de vermelho está dizendo que aquela linha segura duas garantias, não uma** —
vale perguntar qual é a segunda antes de reverter.*

**E os cinco vizinhos da tabela acima foram fechados na rodada seguinte (T-068) — a quarta da
família, e a primeira em que o achado NÃO custou uma tarefa para ser descoberto.** É a diferença que
vale registrar: a T-061 apareceu porque alguém foi pendurar um campo; a T-062 apareceu porque o
implementador da T-061 mediu os vizinhos ao terminar. A T-068 já estava **escrita, medida e com raio
calculado** nesta seção antes de existir — o implementador da T-062 mediu os cinco com `ParseWebhook`
e reportou em vez de consertar calado, e a tabela virou a tarefa. *A pergunta que generaliza: quando
você fechar uma classe, meça a classe VIZINHA no mesmo dia e escreva o número — a próxima pessoa
começa da medição, não da suspeita.*

*Conserto (T-068): **sete** structs de `internal/meta/parse.go` são a fronteira, não uma —
`envelopeMeta`, `entryMeta`, `changeMeta`, `valueMeta`, `messageMeta`, `statusMeta` e
`templateStatusMeta`, todas com TODO campo `json.RawMessage`. **As listas também**
(`entry`, `changes`, `messages`, `statuses`, `contacts`, `errors`) são `json.RawMessage` e não
`[]json.RawMessage`, pela razão que já estava escrita em `mensagemMeta.Errors`: isolamento por ITEM
não protege contra a LISTA inteira ter tipo errado. `metadataMeta` e `contactMeta` continuam structs
**planas**, pela regra de PROFUNDIDADE desta seção — um `profile` ilegível custa aquele contato, e o
nome do outro cliente da mesma leva continua chegando (`TestParseWebhookAnUnreadableProfileCostsOnlyThatContact`).*

*Duas coisas que o conserto trouxe além do isolamento, e as duas são de anti-divergência:*
*(a) **uma travessia só** (`forEachChange`) serve `ParseWebhook` e `AccountWabaIDsInPayload`.
Enquanto os níveis de cima eram tipos concretos, as duas funções podiam repetir três linhas de
`Unmarshal` sem risco; com leitura DEGRADADA em cada nível, duas cópias divergiriam — e a divergência
seria entre o que o parser ENTREGA e o que a guarda de isolamento CONFERE, que é a forma mais cara
desta seção. (b) `changeDeTemplateMeta` **deixou de existir**: com `changeMeta.Value` cru, as duas
formas de `value` são lidas do mesmo bloco, cada uma na sua struct, sem o segundo `Unmarshal` do
`change` inteiro.*

***A DECISÃO da tarefa, e ela não é do parser: `entry.id` ilegível NÃO vira "passa".*** O `waba_id` é
a única chave de roteamento de um webhook de conta, e a guarda 5b (`internal/inbound/handler.go`)
passou a tratar `""` — ausente **ou** ilegível — como não-casado, recusando o lote com `ALARME` e
`conta_descartada`. A outra saída que a tarefa permitia (descartar só o `entry`, com contador
próprio) **foi recusada porque é uma defesa que só parece defesa**: a guarda recusa o lote inteiro
justamente porque o corpo **cru** vai junto na entrega, então filtrar os `eventos` deixaria o
conteúdo daquela conta chegar assim mesmo. *E aqui, de propósito, "ausente" e "ilegível" viram a
mesma resposta — a distinção da entrada acima existe para decidir se DESCARTAMOS conteúdo do
consumidor, e a pergunta desta guarda é outra: "dá para PROVAR que este webhook é desta instância?".*

*Oito mutações, feitas e revertidas antes do commit. **As duas últimas renderam mais do que a prova
que pediam:*** (1)-(6) devolver `valueMeta.Metadata`, `valueMeta.Contacts`, `changeMeta.Field`,
`entryMeta.ID`, `statusMeta.Status` e `templateStatusMeta.Reason` ao tipo concreto deixa o fixture
daquela struct vermelho **com `len(evs)` caindo** (2→0, 2→0, 2→1, 2→1, 2→1, 2→1) e
`TestBoundaryStructsIsolateEveryFieldByConstruction` vermelho citando struct e campo — e a de
`entryMeta.ID` derruba junto `TestHandlerRecusaWebhookDeContaComWabaIDIlegivel`, com a mensagem
*"entregou webhook de conta cujo waba_id nao pode ser lido"*, que é **o defeito de produção
reproduzido na íntegra**; (7) devolver `envelopeMeta.Entry` a `[]json.RawMessage` mostrou um buraco
que eu mesmo estava criando: o `return 0` do caminho de erro de `forEachChange` fazia
`{"entry":"x"}` sair como `nil, nil` — *"nenhum evento, tudo bem"* — quando antes da tarefa saía com
erro. Virou `return 1`, com o porquê no código; (8) **tirar o `waba == ""` da guarda 5b passou
VERDE**, e é o achado que vale.

***Por que a mutação (8) passou verde, e o que isso ensinou:*** a instância do teste tem
`waba_id = "WABA1"`, então `"" != "WABA1"` recusa de qualquer jeito. A defesa real não era o `==""`,
era *"toda instância tem waba_id preenchido"* — e isso é verdade do **caminho de hoje**, não do tipo:
`config.Store.CreateInstance` valida slug, `callback_url` e `bundle_ca`, e **não** valida `waba_id`
(era verdade até a T-074, que fechou isso — ver a entrada seguinte);
quem valida é o `zapgw provisionar instancia`, que é só o PRIMEIRO caminho de criação (o próprio
comentário de `CreateInstance` levanta esse cenário: *"o próximo — um endpoint de administração, um
seed — nasceria sem eles"*). Com `waba_id` vazio dos dois lados, `"" != ""` é falso e a guarda passa
**calada**. O conserto não foi remover o `== ""`: foi escrever o teste que faltava
(`TestHandlerRecusaWabaIDIlegivelAindaQueAInstanciaNaoTenhaWabaID`), que cria a instância pelo store
com `waba_id` vazio e fica vermelho com a mutação. **É a terceira vez que "mutação passou verde"
rende mais que "mutação passou vermelha" neste arquivo** (T-047, ordem × assinatura; T-054, contar
antes × assinatura de `Registrar`; esta, comparação × invariante de quem cria o dado) — e as três têm
a mesma forma: *a garantia que você acha que está no código está, na verdade, no caminho que
alimenta o código.*

*Custo em produção: **desconhecido por construção**, pelo terceiro relatório seguido — resultado
AUSENTE, não errado. Vale um qualificador que as entradas anteriores não tinham: este defeito é
**mais raro e mais caro** que o da mensagem. Mais raro porque exige que a Meta mude o tipo de um
campo de ESTRUTURA (`contacts`, `metadata`, `entry.id`), não de conteúdo; mais caro porque quando
acontecesse levaria o lote de um cliente, não uma mensagem. Ele viveu do plano 1 (2026-07-23) até
2026-07-28.*

**E o fecho dessa mutação verde (T-074), com o achado que só apareceu ao IMPLEMENTAR: a saída
defensiva que a tarefa oferecia não tinha mais o que defender.** A T-068 deixou o buraco descrito
assim — *"`waba_id` vazio dos dois lados faz `"" != ""` ser falso, a guarda passa **calada**"* — e a
T-074 ofereceu duas saídas: **(a)** `CreateInstance` passar a validar `waba_id` e `phone_number_id`,
ou **(b)** a guarda 5b tratar `waba_id` vazio **da instância** como *"não sei conferir"* e recusar. A
(b) parece a mais defensiva das duas, e é a que a descrição do defeito puxa. **Ela já estava
implementada:** o `waba == ""` explícito que a própria T-068 escreveu (`internal/inbound/handler.go`)
recusa o webhook de conta ilegível, e um `waba_id` legível de outra WABA difere de `""` — então, com
a instância sem `waba_id`, **nenhum** webhook de conta passa. Medido antes de decidir, com um teste
descartável (instância com `waba_id` esvaziado + `payloadDeContaDeTemplate("WABA999")`): `entregou =
false`, `200`, e o alarme `... da waba_id "WABA999", que nao e a dela ("")`. Escolher a (b) teria
produzido um comentário afirmando uma proteção nova sobre um comportamento que já existia — a
"defesa que só parece defesa" que este arquivo já registra duas seções acima, agora na forma mais
cara, a de quem *acha que consertou*.

*O defeito residual, então, não é o que estava escrito: instância sem `waba_id`/`phone_number_id`
não vaza, ela nasce **MORTA**. A 5a recusa toda mensagem/status com `phone_number_id` não-vazio, a 5b
recusa todo webhook de conta, as duas respondendo `200` à Meta (que por isso nunca reenvia) e
deixando só uma linha de journal — e este arquivo já registra que ninguém lê journal por hábito. Uma
instância que recusa tudo em silêncio só é barata de pegar num lugar: **na criação**. Daí a (a):
`config.ValidateIdentification` (`internal/config/store.go`), chamada por `CreateInstanceAt` junto de
`ValidateSlug`/`ValidateCallbackURL`/`ValidateCABundle` — validar três dos cinco campos e não os outros
dois era a armadilha-mãe deste projeto dentro de uma função só.*

**Conferido ANTES de decidir, porque a tarefa mandava conferir e porque (a) quebraria criação que
funciona se a resposta fosse outra:** não existe caminho legítimo com esses campos vazios.
`zapgw provisionar instancia` (`cmd/zapgw/provision.go`) já exige as duas flags — inclusive para
instância **só de saída**, onde o opcional é a `callback_url` e não a identificação, e inclusive para
a de **laboratório**, que desde a T-071 nasce pelo mesmo comando. A suíte inteira concordou: a
validação nova derrubou **um** teste, e foi justamente o da T-068.

> ⚠️ **O parágrafo acima descreve 2026-07-28 e deixou de valer no MESMO dia: a T-079 tornou
> `--waba-id` e `--phone-number-id` OPCIONAIS na criação**, porque no modelo decidido
> (`docs/MODELO-DE-USO.md`) esses valores são do consumidor e o dono não os tem. *A exigência não
> sumiu — mudou de lugar:* `config.ValidateIdentification` continua existindo e passou a ser chamada
> por `RegisterMeta` (`POST /v1/cadastro`), e o teste da T-074 foi **movido**, não removido
> (`TestRegisterMetaRefusesEmptyIdentification`). O que a T-074 impedia — instância que recusa tudo em
> silêncio — continua impossível por outro caminho: instância sem cadastro nasce e permanece
> **PAUSADA** (responde 503, que é o estado que ela de fato tem) e só o `zapgw fumaca` a ativa,
> exigindo um envio que funcionou de verdade. *A lição de método fica inteira: a mutação que passou
> verde na T-068 continua sendo o motivo de a validação existir.*

*Custo: **zero em produção** — nenhuma instância nasceu sem `waba_id`, porque o único caminho de
criação sempre exigiu. O que se pagou foi de processo, e é o mesmo par de sempre: a garantia morava
no CHAMADOR (a T-068 achou), e a descrição do buraco envelheceu **entre a tarefa ser escrita e ser
feita** — poucas horas, porque quem fechou o vazamento foi a própria tarefa anterior.*

**A pergunta que generaliza, e ela é sobre o Why da tarefa, não sobre o código: o texto que justifica
uma tarefa é DOC, e vale a mesma regra — confira contra o código, nunca contra a descrição.** Antes
de escolher a saída "mais defensiva" que uma tarefa oferece, **meça se ela ainda tem o que defender**;
se o comportamento que ela promete já é o de hoje, a escolha certa é a outra, e o custo de não medir
é um commit que parece conserto e não muda nada.

*Duas mutações, feitas e revertidas antes do commit:* (1) tirar a chamada de `ValidateIdentification`
de `CreateInstanceAt` deixa `TestCriarInstanciaRecusaIdentificacaoVazia`
(`internal/config/store_test.go`) vermelho nos **cinco** casos — os dois campos ausentes, os dois só
com espaço em branco, e os dois juntos; (2) tirar o `waba == ""` da guarda 5b **continua** deixando
`TestHandlerRecusaWabaIDIlegivelAindaQueAInstanciaNaoTenhaWabaID` vermelho, e essa era a prova que
importava: aquele teste **teve de mudar de forma** (o `waba_id` agora é esvaziado por `UPDATE`
direto, porque o store deixou de aceitar criar assim) e a rede da T-068 tinha de sobreviver à
mudança. *Por que o teste continua existindo depois da (a): validação na criação não alcança linha
que já está no banco — banco escrito por binário anterior, `UPDATE` na mão. Uma impede a instância de
NASCER incompleta; a outra impede o handler de CONFIAR numa que já esteja assim.*

**Duas tags `json:"mesmoNome"` na MESMA struct não dão erro de compilação nem de execução — o
`encoding/json` ignora os DOIS campos em silêncio, tanto no `Marshal` quanto no `Unmarshal`.** A
T-044 (botão de template discriminado por `tipo`) partiu de um pedido escrito com o campo novo
literalmente chamado `botoes` — igual ao exemplo que a própria Meta usa. Só que `Pedido.Buttons`
(`internal/outbound/message.go`) já usava a tag `json:"botoes"` desde antes, para outra coisa
inteiramente diferente: o corpo de `"tipo": "botoes"` (mensagem interativa comum, `{id,titulo}`,
SEM template). As duas funcionalidades não têm nada em comum além do nome. Confirmado por
experimento antes de escrever qualquer código (`json.Marshal`/`Unmarshal` numa struct de teste com
dois campos `json:"botoes"`): nenhum erro em nenhuma das duas chamadas — os dois campos saem/ficam
vazios, como se nenhum dos dois existisse. Se o campo novo tivesse entrado com esse nome, um
consumidor mandando `"botoes"` para um pedido de TEMPLATE receberia `200` com o bloco de botão
**ausente**, e um consumidor mandando `"botoes"` para uma mensagem interativa comum perderia a
mensagem inteira — os dois ao mesmo tempo, e nenhum dos dois com qualquer sinal de erro.
*Custo: zero — achado pelo consumidor `consumer-b`, lendo o contrato ANTES de planejar em cima
dele, e não pela revisão daqui. Conserto: o campo novo se chama `botoes_template`
(`Pedido.TemplateButtons`), preservando `Pedido.Buttons` intocado; e `Validar()` ganhou uma guarda
NOMEADA nos dois sentidos — `botoes` num pedido de `tipo:"template"` e `botoes_template` num pedido
de `tipo:"botoes"` são, os dois, `ErrFieldForbidden` citando o nome certo do campo — para que
confundir os dois nomes parecidos produza um erro que aponta para onde ir, em vez de um descarte
silencioso. Provado por `TestValidateRefusesInteractiveButtonsInTemplateWithAnErrorPointingAtTemplateButtons`
e `TestValidateRefusesTemplateButtonsInTheInteractiveButtonsType`
(`internal/outbound/message_test.go`).*
**A pergunta que generaliza, e é irmã — não a mesma — da armadilha-mãe deste arquivo: aquela
pergunta "onde mais essa regra deveria valer, e vale?" descobre um nome que falta num segundo
lugar; esta aqui descobre um nome que já existe num PRIMEIRO lugar diferente. O mesmo nome para
duas coisas diferentes é pior que dois nomes diferentes para a mesma coisa** — o segundo o leitor
do contrato percebe (nomes diferentes convidam a perguntar "por quê?"); o primeiro passa
despercebido até o `encoding/json`, ou pior, até produção, decidir sozinho qual campo existe.
Antes de nomear um campo novo em qualquer struct compartilhada, `grep` pela tag JSON que você está
prestes a escrever — não só pelo nome do campo Go.

**Slice `nil` vira `null` no JSON, NÃO `[]` — e o contrato deste projeto prometeu `[]` por escrito,
em dez lugares, durante toda a vida do envelope.** `Envelope.Events` não tem `omitempty`
(`internal/inbound/deliver.go:45`), então o campo sai sempre; e `ParseWebhook` devolve
`var evs []Evento` **sem nenhum `append`** quando não há o que enriquecer
(`internal/meta/parse.go:479`) — webhook de conta não modelado, corpo sem `messages`/`statuses`,
parse que falhou por inteiro. O handler repassava essa slice sem normalizar
(`internal/inbound/handler.go:194`). Resultado no fio, de 2026-07-23 a 2026-07-28:
`{"…","eventos":null,"parse_error":""}` — **hoje o envelope normaliza para `[]`, ver o conserto no
fim desta entrada.** O `docs/CONTRATO-CONSUMIDOR.md`, o
`docs/META-CAMPOS-DE-WEBHOOK.md`, o changelog e as tarefas todos diziam **`eventos: []`**.

**Por que isso é doc falso caro e não detalhe cosmético:** o consumidor que segue o contrato ao pé da
letra escreve `for ev in envelope["eventos"]`, que em Python estoura `TypeError: 'NoneType' object is
not iterable`; e uma guarda `if eventos == []` **nunca casa**. Os dois quebram exatamente no caso que
o texto existia para descrever — o lote sem evento —, e não no caminho feliz que todo teste exercita.
*(No mesmo envelope, `parse_error` também não é `null` nem ausente: é `""`, pela mesma falta de
`omitempty` em `:46`. O exemplo do contrato mostrava `null`.)*

*Custo: desconhecido, e o jeito como o teste passou ao lado é o que assusta.
`TestHandlerDeliversTheRawEvenWithTheParseFailing` (`internal/inbound/handler_test.go`) **produz esse
envelope exato** — manda `null` como corpo, o parse falha, a entrega acontece — e **lê o corpo
serializado no consumidor**. Só que a única asserção sobre ele é `strings.Contains(…, "parse_error")`:
o teste tinha `"eventos":null` na mão e nunca olhou. Na data do achado,
`grep -rn '"eventos"' internal/` devolvia **uma linha só, a tag da struct** — nenhuma asserção, em
teste nenhum, sobre a forma desse campo no JSON (hoje devolve também as do teste da T-067).
E nenhum consumidor reportou. Os dois consumidores estão em produção
recebendo webhook de conta não modelado desde 2026-07-28 (o App ficou inscrito em dez campos e o
gateway modela um), então o caso ocorre de rotina hoje. Achado em 2026-07-28 ao escrever a T-056,
conferindo o formato contra o código em vez de contra o contrato — e provado com um `json.Marshal`
executado, não por memória do `encoding/json`. Conserto em DUAS etapas, e a ordem entre elas é a
parte que vale copiar: primeiro a **documentação** passou a dizer `null` e a recomendar
`envelope.get("eventos") or []` (T-056), e só **depois** o fio mudou (T-067).*

***A ordem "avise, espere a defesa, então mude" não é cerimônia — ela é o que transformou uma quebra
em não-evento.*** A mudança do fio (`[]` no lugar de `null`) **conserta** quem leu a doc antiga e
**quebraria** quem tivesse ramificado no `null` que o fio realmente mandava. Os dois consumidores
foram avisados por escrito nos canais em 2026-07-28 15:12, com a instrução explícita de se
defenderem ANTES; os dois responderam no mesmo dia confirmando a defesa no código deles. Um dos dois
escreveu a frase que resume o ganho: *"o `null` não me pegou, mas eu estava com sorte — não havia
teste, e a simplificação óbvia teria quebrado. O aviso na ordem certa transformou acidente em
decisão."* Inverter a ordem teria custado exatamente o oposto, e de graça.

**A regra que sai daqui, e ela decide sozinha se um campo vazio precisa de conserto: o campo cujo
vazio já é FALSY não precisa; o campo cujo vazio é `null` num tipo que se ITERA precisa.** No MESMO
envelope, `parse_error` também não tem `omitempty` e também sai sempre — mas sai `""`, que é falso em
Python, JS, Go e em qualquer linguagem que um consumidor vá usar, e ninguém itera sobre uma string de
erro. Ele está CERTO como está e não foi tocado na T-067. `eventos` saía `null` num tipo que todo
consumidor percorre, e `null` não é lista em lugar nenhum. Os dois campos vinham do mesmo descuido
(nenhum `omitempty`, nenhuma normalização), e só um era defeito. *A pergunta, antes de "consertar"
um campo vazio: **o vazio dele já é falso na linguagem de quem lê, e alguém ITERA sobre ele?** Se o
vazio é falsy, mexer é churn que ainda por cima entra em "Mudanças que quebram" à toa.*

*Conserto (T-067): `if evs == nil { evs = []meta.Evento{} }` na montagem do envelope
(`internal/inbound/deliver.go:257`), **um lugar só** — normalizar em cada chamador seria enumerar call
sites, que é a armadilha-mãe deste arquivo, e o próximo caminho de entrega nasceria mandando `null`
de novo.* **Mutação obrigatória, feita e revertida antes do commit, e ela mediu mais do que provava:**
tirar a normalização deixa vermelho **um único teste em toda a suíte** —
`TestEnvelopeWithNoEventGoesOutAsEmptyArrayOnTheWireNeverNull` (`internal/inbound/deliver_test.go`), citando
`"eventos":null` na mensagem —, e **nada mais pisca**. Ou seja: cinco dias depois de o defeito ser
conhecido e documentado, a suíte inteira continuava incapaz de vê-lo, porque **nenhum outro teste
olha os bytes do fio**. É a prova executável da pergunta que fecha esta entrada.

**A pergunta que generaliza, e é irmã de "o código VERIFICA ou GUARDA esse dado?" (seção *TLS*): o
exemplo do contrato foi COPIADO do código, ou escrito à mão a partir da struct?** Struct em Go não
mostra `nil` contra vazio, nem `omitempty` contra sempre-presente — só o `json.Marshal` executado
mostra. **Exemplo de formato que ninguém serializou é chute com aparência de referência**, que é a
mesma lição da entrada *"exemplo de doc é código que ninguém roda"* (seção *Documentação*), um nível
antes: lá o VALOR estava errado, aqui o TIPO.

**`encoding/json` sem tag casa por nome EXATO ou por diferença só de maiúscula/minúscula — nunca
`snake_case` → `CamelCase`.** Ao escrever `meta.InstagramAccount` (T-109, o diagnóstico do Instagram) o
campo `AccountType string` nasceu SEM `json:"account_type"`, contando (sem verificar) que o pacote
faria a conversão que ele faz para `id`→`ID` e `username`→`Username` — os dois batem por
case-insensitive puro, e mascararam que o terceiro campo não batia. O `Unmarshal` não retornava erro
nenhum: `AccountType` ficava silenciosamente `""`, e o comando imprimia "tipo (não informado)" mesmo
com a Meta respondendo `"account_type":"BUSINESS"` no corpo.
*Custo: zero — pego pelo teste escrito ANTES do commit
(`TestDiagnosticInstagramHealthyInstanceAnswersEveryQuestion`, `cmd/zapgw/diagnostics_test.go`),
que afirma o texto exato `"tipo BUSINESS"` na saída em vez de só conferir que a palavra "tipo"
apareceu. Um teste que só checasse "a pergunta 1 respondeu alguma coisa" teria passado do mesmo jeito
com o campo vazio.* **A pergunta que generaliza: todo struct que decodifica JSON da Meta (ou de
qualquer API alheia) tem CADA campo com tag explícita, mesmo quando o nome "parece" bater** — bater
por acidente hoje (dois campos de três) é pior que não bater nunca, porque esconde o terceiro atrás
dos dois que funcionam.

---

## Erros e log

**Erro de transporte em Go carrega a URL completa — host, path e query string.** `*url.Error.Error()`
inclui a URL da requisição. Interpolar o erro com `%v` numa mensagem que vai para o log **vaza a
`callback_url`**, que este projeto cifra em repouso justamente para que um backup roubado não revele
a topologia dos consumidores. Provado com uma callback contendo `?token=SEGREDO`: o token inteiro
apareceu no `Motivo`.
*Custo: **o Critical nº 2**. Conserto: classificar o erro (`errors.Is` contra sentinelas), nunca
interpolá-lo.*

**Cifrar um dado em repouso e imprimi-lo no log torna a cifra decorativa.** Ao decidir que um campo é
segredo, varra **todos** os lugares onde ele pode sair: banco, log, mensagem de erro, header,
resposta HTTP.

**"A mensagem de erro nomeia o campo, nunca o valor" era regra do PACOTE, não de TODA mensagem —
e só virou perigosa quando alguém tentou logá-la.** A T-037 (logar o motivo da recusa de
`POST /v1/messages`, `/v1/media` e `/v1/templates`) partiu da premissa escrita no próprio handler:
o erro de `Validar()` "nomeia o campo, nunca o valor" — e por isso seria seguro logar
`err.Error()`. Revisando `internal/outbound/message.go` campo a campo (não confiando na frase),
apareceram TRÊS exceções que citam de propósito o valor recusado, com `%q`, para orientar o
consumidor: `ErrUnknownType` (ecoa `p.Type`), `ErrUnknownCategory` (ecoa `p.Categoria`)
e `ErrUnknownHeaderType` (ecoa `c.Cabecalho.Type`). Nenhuma das três é bug na RESPOSTA HTTP
— o próprio consumidor que mandou o valor está lendo de volta —, mas as três seriam vazamento se
fossem para o LOG DO GATEWAY: um campo de texto livre (`tipo`) viraria canal para gravar, no
journal do gateway, qualquer string que um consumidor (malicioso ou só quebrado) decidisse pôr ali.
*Custo: zero — achado ANTES do commit, ao conferir cada mensagem de `Validar()` contra o código
(doutrina deste projeto), não ao assumir que a frase do comentário valia para as 39 mensagens de
uma vez. Conserto: `mensagemDeRecusaSegura(err)` em `message.go` troca as três por texto fixo
antes de logar; a resposta HTTP ao consumidor continua usando `err.Error()` cru, sem mudança
nenhuma. Provado por `TestHandlerLogsUnknownTypeWithoutLeakingTheRefusedValue`
(`internal/outbound/handler_test.go`), que manda um `tipo` sentinela e exige que ele apareça na
RESPOSTA e não apareça no LOG.* **A pergunta que generaliza, irmã de "onde mais essa frase deveria
ser verdadeira?": uma regra escrita para explicar UM caso concreto vale para os OUTROS 38 casos da
mesma família, ou só parece valer porque ninguém tinha motivo pra testar os outros?**

**"Deu erro" e "não aconteceu" são perguntas diferentes, e tratar as duas igual duplica mensagem.**
O envio liberava a chave de idempotência em **qualquer** erro da Meta. Mas erro de transporte, prazo
estourado e `2xx` sem id não provam que a mensagem não foi criada — só provam que não sabemos. A
chave liberada faz o retry legítimo do consumidor virar uma **segunda** mensagem no celular de um
cliente real. Só desfecho **conhecido-negativo** (a Meta respondeu com status de erro, ou o pedido
nem saiu daqui) devolve a chave; o desconhecido retém e responde a classe `desconhecido` (`502`), não
`retentavel` — chamar de `retentavel` um caso que vai dar `409` manda o consumidor fazer o oposto do
certo.
*Custo: pego na revisão final do plano 2, antes de ir para produção. Conserto: `errors.As` contra
`*meta.ErroMeta`.*
**A fronteira exata é uma escolha, não um fato — e a doc não pode fingir que é fato.** Um `5xx` da
Meta **também** não prova que a mensagem não foi criada; ainda assim ele **libera** a chave. Não é
incoerência: reter em `5xx` transformaria uma instabilidade da Meta num lote inteiro de mensagens
intransmissíveis por 72h — dano **certo** e proporcional ao tamanho do incidente —, contra o risco
**incerto e unitário** de uma duplicata. A primeira redação do contrato afirmava, como fato, que
"`502`/`503` da Meta significam que a mensagem não foi criada"; isso é afirmação sobre serviço externo
sem fonte, que este arquivo proíbe (ver *Documentação*, abaixo). O texto foi trocado por "a Meta
respondeu, mesmo que com erro" — verdadeiro e verificável do nosso lado.

**Chave de idempotência amarrada só ao consumidor engole mensagem em silêncio.** O contrato
recomenda usar o id da entidade como chave — e a mesma entidade manda várias mensagens (lembrete,
cobrança, desculpa). Sem comparar o **pedido**, a segunda recebia `200` com o `wa_message_id` da
primeira: o consumidor gravava "enviado" e a mensagem nunca saía. *Conserto: hash do pedido **já
normalizado** na tabela, e `422` quando difere. Hashear antes de normalizar seria pior que não ter a
guarda — `" 5511…"` e `"5511…"` colidiriam como pedidos diferentes e todo retry legítimo viraria um
`422` falso.*

**Um conserto que ACRESCENTA um caso ao log pode APAGAR o caso que importava — e o comentário dele
descrevia o defeito enquanto o recriava.** O log de veredito do inbound alarmava quando `v.Alarm`.
Um conserto do plano 1 quis acrescentar o log dos casos não-2xx e trocou a condição por
`status < 200 || status >= 300`. Mas `Alarme` é marcado **exatamente** nos ramos que respondem `200` à
Meta: as duas condições são mutuamente exclusivas, e o prefixo `ALARME` virou **código morto**. Ou
seja: o único aviso de perda definitiva — a Meta nunca mais reenvia depois de um `200` — ficou
desligado por três semanas.

O comentário do próprio conserto dizia que o log *"só aparecia quando `v.Alarm`, ou seja, nunca no
caso em que você precisaria dele"*. Ele diagnosticou o problema com precisão e o inverteu em vez de
resolvê-lo. **Substituir uma condição não é acrescentar um caso**: quando o objetivo é "passar a logar
também X", a forma segura é `condicaoAntiga || X`, e o teste tem de exigir que o caso ANTIGO continue
saindo.
*Custo: um Critical achado por execução (`log.SetOutput` num buffer), não por leitura — três revisões
leram esse bloco e nenhuma viu. Conserto: `v.Alarme || fora-de-2xx`, com teste que exige o prefixo
num consumidor `4xx` e a ausência dele num `5xx`.*

**Duas definições vivas para o mesmo prefixo de alarme treinam quem opera a ignorá-lo.** O inbound
definia `ALARME` = perda definitiva; o outbound já alarmava sobre idempotência não liberada, em que
nada se perde. Nenhum dos dois estava errado sozinho — o par é que estava. *Conserto: o critério
passou a ser **"precisa de gente"**, e perda definitiva virou um caso dele. Cada `ALARME` diz agora o
que a pessoa precisa FAZER, não só o que aconteceu.*

**Uma justificativa CERTA para o evento isolado vira desculpa para a repetição — e o comentário que a
escreve congela o buraco.** A recusa por corpo acima do teto (`413`) logava sem `ALARME`, com a
justificativa *"a Meta reenvia sozinha dentro da janela de 36h — ninguém precisa agir agora por causa
deste evento isolado"*. A frase é verdadeira para **um** evento e falsa para o segundo: o reenvio traz
o **mesmo** corpo, estoura o **mesmo** teto, e quando a janela expira a mensagem se perde em
definitivo — o desfecho mais caro deste projeto — **sem nenhum sinal**. Nada nesse ramo se conserta
sozinho; só parecia, porque a comparação foi feita com o caso vizinho (`5xx` do consumidor), em que o
reenvio de fato resolve.
**E os DOIS extremos estavam errados, o que é o motivo de a correção ser um limiar e não um `if`.** O
plano 1 escreveu essa linha alarmando em **toda** rejeição
(`docs/superpowers/plans/2026-07-23-fundacao-e-inbound.md:2846`); a implementação tirou o prefixo — e
com razão, porque alarme por evento vira ruído — mas trocou o ruído pelo **silêncio**, que é a falha
cara. O meio é contar: nenhum alarme abaixo do limiar, um só por janela acima dele.
*Custo: nenhum em produção — o gateway ainda não recebe tráfego real. O buraco existiu desde o
primeiro commit do inbound (`677ccd0`, 2026-07-23) e nenhuma revisão do plano 1 o pegou: todas leram
a justificativa e concordaram com ela, porque ela está certa para o caso que descreve. Conserto na
T-002: contador por instância em memória (3 recusas em 1h → um `ALARME` com a ação). **A pergunta que
teria pego: "esta justificativa continua verdadeira na SEGUNDA vez?"** — e ela é irmã da
armadilha-mãe deste arquivo, que pergunta "onde mais essa frase deveria ser verdadeira?".*

**Um erro de I/O virou "corpo grande demais" porque o código só olhou que houve erro.** `ReadRaw`
devolve dois erros diferentes; o handler mapeava os dois para `413 permanente`. Uma conexão que caiu
no meio do upload virava "encolha o corpo" (que estava perfeito) + "não tente de novo" (quando tentar
de novo era o certo). *Conserto: `errors.Is(err, httpx.ErrCorpoGrande)`, e o resto vira `400`
`retentavel`.*

**Um método que PODE devolver erro convida alguém a, um dia, tratar esse erro como fatal — a defesa
mais forte é a ASSINATURA, não a disciplina de quem chama.** A T-035 (contadores de instância) tem uma
regra dura: contar é acompanhamento, nunca pode derrubar a resposta já escrita à Meta ou ao
consumidor. A tentação óbvia era escrever `Registrar(slug, chave) error` e documentar "ignore o erro,
só logue" — mas isso deixaria a garantia na cabeça de quem escreve cada `handler.go`, e o primeiro que
movesse a chamada para ANTES do `w.WriteHeader` (um refactor plausível: "vamos contar assim que
sabemos o desfecho") teria, bem ao lado, o padrão que domina o resto deste projeto:
`if err != nil { http.Error(w, …); return }`. `Contador.Record` não tem NENHUM retorno: o erro é
lido, logado e descartado dentro do próprio método, e não existe caminho para o chamador propagar o
que nunca recebeu. *Custo: zero — a mutação de prova (mover a chamada para antes do `WriteHeader` E
trocar `Registrar` por uma variante que devolve erro, `RegistrarComErroTEMPORARIO`, feita e revertida
antes do commit) deixou `TestHandlerCounterFailureDoesNotChangeTheStatus` vermelho com `500` no lugar do
veredito real — a prova de que, com a assinatura "erro que volta", o bug estava a um refactor de
distância.* **A pergunta que generaliza, irmã da entrada sobre o ponteiro desnecessário (seção
"Go / JSON"): se a resposta certa a um erro é sempre "ignore", a assinatura do método pode garantir
isso — por que deixar para a disciplina de quem chama?**

**Monitor que compara resposta tem de provar PRIMEIRO que houve resposta — senão a queda da rede
vira o alarme mais grave que ele sabe dar.** Em 2026-07-29, 13:39, o monitor de inscrição do App
gritou `ALARME INSCRICAO: a Callback URL do App MUDOU` **carregando, no próprio texto do alarme, a
prova de que não tinha perguntado nada**: `curl: (28) Failed to connect to graph.facebook.com`. A
lógica era "a resposta contém a nossa URL? se não, alarme" — e uma string de erro de `curl` também
não contém a nossa URL. **Ele não distingue *não consegui perguntar* de *a resposta mudou*, e trata
as duas como o pior caso.** *Custo desta vez: uma investigação. O custo que ele cobra se repetir é
outro e é composto — **este é o alarme que, se for de verdade, significa que todo o tráfego dos
inquilinos foi desviado**, e é justamente o que menos pode se dar ao luxo de gritar à toa. Alarme
que mente é alarme que se aprende a ignorar, e este é o último que alguém deveria aprender a
ignorar.* **A forma certa:** exigir `200` **e** JSON legível antes de comparar; sem isso, o evento é
*"o monitor está cego"* — outra mensagem, outra urgência. *Irmã da regra do `grep` num pipeline
(seção "Ambiente"): nos dois casos o veredito foi lido de um lugar que **também** responde a mesma
coisa quando o passo anterior nem aconteceu.*

> **E a lição de diagnóstico, que vale além de monitor:** o falso positivo foi fechado com **prova
> positiva**, não com "o alarme parece bobo". A pergunta que resolveu não foi *"a URL mudou?"* (que
> exigiria o `app_secret`, e ele nem mora na máquina do gateway) e sim *"webhook ainda está
> chegando?"* — `recebidas` do `tenant-two` com 53 no dia e o último 15 min antes do alarme.
> **Tráfego chegando prova que a Callback URL é a nossa**, e é uma prova que se lê sem segredo
> nenhum. Quando a pergunta direta for cara, procure o fato que só é possível se a resposta for a
> que você espera.

---

## TLS

**Um `*http.Client` recebido de fora é a porta pela qual a escotilha entra — e ela não faz barulho
nenhum ao ser aberta.** O `NewDeliverer` recebia o cliente por parâmetro (para os testes usarem o
`srv.Client()`), e com isso *qualquer* chamador podia entregar um `tls.Config` sem verificação, uma
vez, para destravar uma demo. Desligar a verificação **não gera erro**: só remove uma proteção, em
silêncio, e a exigência de `https` na `callback_url` vira teatro — o esquema continua dizendo `https`
e não há mais garantia nenhuma por trás.
*Custo: nenhum ainda — o gateway não entrega para consumidor nenhum hoje (a instância em produção tem
`callback_url` vazia). Conserto na T-013: o entregador monta o próprio cliente, um por âncora de
confiança, e o parâmetro sumiu. A defesa é a opção **não existir**, mais dois testes que ficam
vermelhos se ela voltar: um de comportamento (servidor com cert autoassinado tem de FALHAR) e uma
varredura de todo `.go` do repo atrás de `InsecureSkipVerify` — a varredura cobre também o sentido
`gateway → Graph API`, que teste de entrega nenhum alcança.*

**Falha de certificado NÃO é "consumidor fora do ar", e tratar as duas igual apaga o único aviso.**
As duas dão erro de transporte e as duas respondem `504`. A diferença é que a queda **se conserta
sozinha** (o consumidor volta, a Meta reenvia dentro da janela) e o certificado **não**: cada reenvio
refaz o mesmo handshake e leva a mesma recusa, até a Meta desistir — e aí a perda é definitiva, sem
ninguém ter sido avisado. É por isso que TLS alarma e queda não.
*E é por isso que o alarme de TLS **não tem limiar**, ao contrário do `413` do handler: lá o evento
isolado ainda tinha chance sozinho e só a repetição virava perda, aqui a PRIMEIRA ocorrência já
precisa de gente. Adiar o aviso trocaria ruído por silêncio no único caso em que o silêncio custa
mensagem.*

**"O gateway já verifica o certificado, então a data está em mãos" — a primeira metade era verdade e
a segunda não, e as duas foram escritas na mesma frase.** É o texto com que o planner abriu a T-060
(2026-07-28). `internal/inbound/deliver.go` monta o `tls.Config` e a verificação é estrita desde a
T-013 — mas **verificar é uma pergunta que o `crypto/tls` responde com um sim/não e joga fora o
resto**: ninguém nunca tinha lido `resp.TLS.PeerCertificates`, e nenhuma data existia em lugar
nenhum do projeto. Quem pegou foi o **implementador da T-060**, que foi conferir antes de
implementar, não implementou o que não existia e — o passo que importa — **não escreveu nada sobre
isso no contrato**: doc que promete mecanismo inexistente é o erro que este projeto persegue.
*Custo: zero, e por pouco. A frase estava dentro de uma TAREFA, que é o gênero de texto que ninguém
trata como suspeito — spec e doc antiga a gente desconfia; ordem de serviço a gente executa. Fechado
na T-064: a entrega passou a capturar o `NotAfter` do certificado folha na conexão que já existia
(`observeCertificate`), com o instante da observação junto.*
**A pergunta que generaliza, e ela vale para toda garantia deste projeto: o código VERIFICA esse
dado ou ele GUARDA esse dado? São coisas diferentes, e a primeira não implica a segunda** — a
verificação consome a informação e a descarta, e quem lê "isto é verificado" conclui naturalmente
que alguém, em algum lugar, ficou com ela.

**Classificar erro de TLS por substring da mensagem é falso negativo esperando o dia da atualização.**
O texto de erro do `crypto/tls` e do `x509` muda entre versões de Go e entre sistemas operacionais —
no Windows a verificação passa pelo verificador da plataforma. A pergunta é feita ao **tipo**
(`errors.As` contra `*tls.CertificateVerificationError` e os três erros do `x509`), e a prova de que a
taxonomia casa com a realidade é um teste que classifica o erro de um **handshake de verdade**, nunca
um erro sintético montado à mão.

---

## Assinatura

**Um valor que viaja FORA da assinatura não é protegido por ela — e batizá-lo de "anti-replay" faz
todo mundo assumir que é.** O `X-Zapgw-Signature` cobria só o corpo; o `X-Zapgw-Timestamp` ia num
header ao lado, e o contrato mandava o consumidor "rejeitar timestamp fora de uma janela de
tolerância". A janela não protegia de nada: quem capturasse uma entrega reenviava com timestamp novo
e a assinatura continuava fechando, porque a assinatura nunca tinha visto timestamp nenhum. O
desfecho é o pior formato deste projeto — o consumidor implementa a checagem, marca o item como
resolvido, e fica exposto **achando que não está**.
*Custo: nenhum em produção, e por pouco. Quem pegou foi o **consumidor** (`consumer-a`, 2026-07-26),
lendo o contrato para escolher o tamanho da tolerância — ele ainda não tinha implementado a
verificação, e é só por isso que a correção coube num commit em vez de numa negociação de quebra. O
buraco existiu desde o primeiro commit do inbound; nenhuma revisão do plano 1 o viu, porque todas
leram o header e o nome dele já respondia a pergunta. Conserto na T-022: a assinatura passou a cobrir
`timestamp + "." + corpo`.*
**A pergunta que teria pego, e ela vale para qualquer header novo: este campo está DENTRO da conta?**
Se a resposta é não, ele é informativo — e o doc tem de dizer isso com essa palavra.

**Assinar um valor que o código calcula DUAS vezes é falha intermitente, e nenhum teste sequencial
a pega.** O `Entregar` lia `e.agora()` uma vez para o `recebido_em` e outra para o header do
timestamp. Enquanto o timestamp era enfeite isso não custava nada; no momento em que ele entrou na
assinatura, as duas leituras passaram a divergir sempre que a virada do segundo caísse no meio — uma
entrega em cada N recusada pelo consumidor como "assinatura inválida", sem nada acusando no gateway,
irreproduzível. *Custo: zero, porque a mesma tarefa que criou o risco fechou-o. A guarda é um relógio
de teste que ANDA um segundo a cada leitura (`TestDeliverSignsTheSAMEInstantThatGoesInTheHeader`): assim
qualquer caminho que leia duas vezes fica vermelho de forma determinística, em vez de uma vez a cada
mil execuções. **Valor assinado se calcula UMA vez e se passa adiante.***

**Concatenar dois campos para assinar sem separador cria dois pares com a mesma assinatura.**
`("1769000000", "0x")` e `("17690000000", "x")` produzem os mesmos bytes. Aqui não seria explorável —
o corpo é sempre um objeto JSON e portanto começa com `{`, nunca com dígito —, mas essa defesa mora
em **outro arquivo** (o `json.Marshal` do `Envelope`), e quem reimplementar a conta em Python ou
TypeScript lendo só o contrato não sabe que ela existe. *O separador (`.`) torna a fronteira
inequívoca por construção; sem teste que o exija, a primeira "simplificação" o remove — a mutação que
o apaga passava verde antes de `TestSignatureSeparatesTimestampFromBodyWithoutAmbiguity` existir.*

---

## Meta / WhatsApp Cloud API

🔴 **ATIVAR uma instância de INSTAGRAM tem um beco de ovo-e-galinha, e ele não existe no WhatsApp.**
Achado em 2026-07-30 lendo o código, **antes** de alguém bater nele — a primeira ativação real ainda
não tinha sido tentada. As três peças, cada uma correta sozinha:

1. instância nasce **pausada**, e enquanto está, o webhook responde `503` **antes de ler o corpo**
   (a guarda de `Ativo` vem antes do `httpx.ReadRaw`, em `internal/inbound/handler.go`);
2. o fumaça — o único caminho para `ativo = 1` — exige um **IGSID que tenha escrito nas últimas
   24 h**, porque a Meta só aceita **texto livre** dentro da janela e no Instagram **não existe
   template** para abrir conversa;
3. o IGSID **só aparece dentro do corpo do webhook**, que a peça 1 recusa sem ler.

**Sem o IGSID não ativa; sem ativar não se lê o IGSID.** A DM não se perde (a Meta reenvia por 36 h),
mas o valor necessário para destravar não chega a ninguém.
**A saída hoje é manual e de fora do gateway:** pegar o IGSID no painel/Graph API da conta
profissional e passá-lo ao fumaça. Um comando de leitura das conversas recentes resolveria de vez —
**está proposto e não decidido** (2026-07-30); enquanto não existir, quem for ativar Instagram
precisa saber disto de antemão, senão gasta uma rodada descobrindo.
*Por que a analogia com o WhatsApp engana: lá o fumaça manda para um telefone, que a pessoa sabe de
cor. O identificador do Instagram é **opaco e com escopo por App** — ninguém o "sabe", ele só chega
pelo webhook. **Toda vez que uma superfície nova troca o identificador por um opaco, todo passo que
dependia de a pessoa conhecer o valor precisa ser reexaminado.***

**Pedido de consumidor descreve o CASO DE USO com precisão e o PROTOCOLO de memória — confira o
verbo, o caminho e o corpo na fonte, mesmo quando dois consumidores concordam.** Ao pedir a marcação
de leitura (T-075), um deles citou `PUT /{phone-number-id}/messages`; a doc oficial da Meta, lida em
2026-07-28 nas **duas** páginas que descrevem a chamada
(`developers.facebook.com/docs/whatsapp/cloud-api/guides/mark-message-as-read` e
`developers.facebook.com/documentation/business-messaging/whatsapp/messages/mark-message-as-read`),
diz **`POST`** — no mesmo caminho do envio. Os dois consumidores descreveram o **mesmo corpo**
(`{"messaging_product":"whatsapp","status":"read","message_id":"wamid…"}`), e **só o verbo divergia**:
o acordo deles sobre o corpo é justamente o que faria alguém aceitar o verbo junto, no pacote.
*Custo: **não cobrou** — a divergência foi vista ao escrever a tarefa e resolvida na fonte antes de
existir código. O que ela cobraria: a suíte deste projeto **não fala com a Meta** (CLAUDE.md, "O que
o verify NÃO alcança"), então um `PUT` passaria verde aqui e falharia só contra a Graph API de
verdade — em produção, no dia da virada, com o sintoma apontando para o gateway.*
**A guarda que ficou:** `TestReadsSendsPOSTOnTheSendPathWithTheMetaBody`
(`internal/outbound/reads_handler_test.go`) afirma verbo, caminho e corpo inteiro contra uma Graph
API falsa que **grava o que recebeu** — um duplo que só respondesse `200` deixaria a correção sem
guarda nenhuma.

**E a mesma armadilha tem uma segunda forma, mais silenciosa que o verbo errado: o campo que o
consumidor cita ainda EXISTE, mas está DEPRECADO.** Ao pedir tier e qualidade do número (T-080), o
`consumer-b` citou `GET /{waba-id}/phone_numbers` com o campo **`messaging_limit_tier`** — que é o
que ele lia direto na Graph. A doc da Meta, lida em 2026-07-28
(`developers.facebook.com/documentation/business-messaging/whatsapp/messaging-limits`), diz
textualmente: *"The `messaging_limit_tier` field, which used to return a business phone number's
messaging limit, **has been deprecated**. Request the `whatsapp_business_manager_messaging_limit`
field instead."* A mesma página mostra a chamada certa —
`graph.facebook.com/v25.0/{phone-number-id}?fields=whatsapp_business_manager_messaging_limit` — e a
resposta `{"whatsapp_business_manager_messaging_limit": "TIER_250", ...}`.
*Custo: **não cobrou** — a conferência foi exigida pela tarefa e feita antes de escrever código. O
que ela cobraria é PIOR que o caso do verbo: um campo deprecado normalmente **ainda responde**, então
não haveria erro nenhum no dia 1. O gateway nasceria com uma dependência marcada para morrer, e a
falha chegaria meses depois, sem relação visível com nada que alguém tivesse mexido.*
**Por que o consumidor não estava mentindo:** ele lia aquele campo e funcionava. *Campo deprecado é
exatamente o caso em que a experiência de quem usa **não contradiz** a doc — e por isso a doc é a
única fonte que resolve.*
**A guarda que ficou:** `TestObserveNumberAsksForTheFieldsCheckedAgainstTheSource`
(`internal/meta/number_test.go`) exige os dois nomes atuais na URL **e fica vermelho se
`messaging_limit_tier` voltar a aparecer** — a asserção negativa é a metade que importa, porque a
positiva sozinha passaria verde com os dois campos pedidos juntos.

🔴 **EDITAR um template APROVADO o tira do ar por até 24 h, e todo envio nesse intervalo é recusado
com `132001`.** A Meta revisa de novo depois da edição, e enquanto revisa o template não presta —
mas o gateway (e o consumidor) só descobrem isso no erro de envio, mensagem a mensagem.
*Custo real, medido no `consumer-b` em **13/07/2026**, antes da migração para este gateway: **duas
clientes ficaram sem contrato**, com o sistema dizendo "enviada". Registrado aqui porque o custo é
deles e a armadilha é da Meta — ela vale para qualquer consumidor deste gateway.*
**O caminho certo é `_vN`:** o `v1` fica no ar enquanto o `v2` é revisado. Nunca editar o que está
em uso.

⚖️ **DECISÃO, e ela é para não ser re-litigada: este gateway NÃO tem rota de editar nem de remover
template.** Não é lacuna, é escolha — confirmada pelo dono do `consumer-b` em 2026-07-28 22:08
(*"editar é um problema **no sentido de não ter mesmo**"*), depois de ele mesmo ter dito o contrário
sete minutos antes e corrigir. Remover queima o nome na Meta; editar é a armadilha acima.
*Se alguém propuser a rota, a resposta é esta linha, não uma discussão nova.*

**Pedir campo novo em `fields=` pode DERRUBAR a leitura inteira, e um 400 desses parece "a Meta
recusou a credencial".** A Graph API responde a um campo que ela não conhece com **400 / `code` 100**
(*"Tried accessing nonexisting field"*), e não com um `200` sem o campo. Na taxonomia deste gateway
4xx é **classe permanente**, que a vigia trata como desfecho definitivo — ou seja, o dia em que a
Meta aposentar `whatsapp_business_manager_messaging_limit` (ela **já** aposentou o antecessor dele,
acima) pintaria `token_meta.veredito = "recusado"` em **toda instância ativa**, mandando gente
procurar credencial revogada que ninguém revogou.
*Custo: **não cobrou** — foi visto ao desenhar a T-080. O que ele cobraria: o alarme mais caro do
painel disparando por um motivo que não tem nada a ver com o que ele afirma.*
**A defesa que ficou** (`internal/outbound/watchdog.go`, `checkOne`): `recusado` **nunca** nasce de
uma chamada com `fields=`. Antes de declarar recusa, a vigia reconfirma com o `GET` limpo; se o limpo
passa, a credencial está boa e quem foi recusado foi o nosso pedido de campo — o veredito sai `ok` e
o defeito vira uma linha de log. Custo zero no caminho feliz. Guardas:
`TestWatchdogDoesNOTRefuseTheTokenWhenGraphRefusesOnlyTheFields` e
`TestWatchdogKEEPSRefusingWhenTheTokenIsReallyRefused` (`internal/outbound/number_test.go`), a
segunda porque sem ela apagar a checagem de credencial inteira passaria verde. No laboratório,
`grafo-falso --recusar-campos-do-numero` reproduz o desfecho com o binário de verdade.

**Marcar uma mensagem como lida marca também as ANTERIORES daquela conversa** — *"When you mark a
message as read, the API also marks earlier messages in the conversation as read"* (as duas páginas
acima, 2026-07-28). Não é detalhe de cor: o `consumer-a` mediu que **47% dos blocos de mensagens de
entrada seguidas têm mais de uma mensagem, e o maior tinha 13** — se a Meta marcasse só a citada, o
caso comum exigiria treze chamadas. **A resposta certa aqui não veio de analogia**: o mesmo
consumidor observou que *"o WhatsApp que eu uso como pessoa se comporta assim"* e ele próprio
descartou a observação — **"o app parece fazer isso" não é fonte**, e o app e a Cloud API são
implementações diferentes do mesmo produto. *O que a fonte NÃO diz, e por isso o contrato não
afirma: como isso se comporta em conversa de GRUPO. Ela fala de "the conversation" sem distinguir.*

**O `app_id` NÃO é segredo sozinho, e É segredo EM PAR com o `app_secret`.** Os dois juntos formam o
**app access token** (`app_id|app_secret`), e com ele se administram as **inscrições de webhook** do
App — `POST /{app-id}/subscriptions`, ou seja, **para onde o WhatsApp daquele número entrega**. A doc
é explícita: *"É necessário um token de acesso do app para adicionar novas assinaturas ao app"*
(`developers.facebook.com/docs/graph-api/reference/application/subscriptions/`).
*Custo real, 2026-07-28: num incidente de vazamento, o consumidor classificou o `app_id` como "não é
segredo" — verdade isolada — e concluiu que a exposição do `app_secret` "não piorava o estado atual",
porque hoje quem recebe é a Evolution e ela não confere assinatura. **O raciocínio olhava só o uso do
valor NO GATEWAY e esquecia o uso dele NA META.** A gravidade real estava um nível acima: quem tinha
o par podia reapontar a Callback URL do App e desviar todo o tráfego, hoje, independente de quem
confere assinatura. O risco chegou a ser formalmente "aceito" antes de alguém contestar.*
**A pergunta que pega esta classe:** *"este identificador completa alguma credencial quando combinado
com outro valor que também vazou?"* Classificar valor a valor é o que produz o erro.

**Não existe rotação programática do `app_secret`** — a doc diz com todas as letras: *"It is not
possible to programmatically rotate the app secret."* Mas **o campo do painel aceita valor colado**,
o que na prática resolve: gere o valor novo (32 hex), cole na Meta, e **só depois** rotacione o
gateway para o mesmo valor.
*Custo evitado no mesmo incidente: o consumidor afirmou de memória que havia "Gerar novamente" no
painel, o dono corrigiu com a tela na frente, e a conclusão virou "não dá para rotacionar, risco
aceito". Ninguém tinha tentado **colar**. Duas afirmações erradas seguidas — uma de memória, outra
por leitura parcial da doc — quase transformaram uma correção de dez minutos em dívida permanente.*
**A ordem importa e é segura nos dois pontos:** enquanto a Meta ainda entrega no endpoint antigo, o
valor pode divergir entre os dois lados sem quebrar nada. Depois da virada, a mesma troca custa
janela de indisponibilidade.

**`200` para a Meta é IRREVERSÍVEL.** Ela reenvia por até 36h se **não** receber 2xx, e para **para
sempre** se receber 200. A regra não é "nunca devolva 500" — é: **responda 200 quando o reenvio não
resolveria, e não-2xx quando resolveria.**
*Custo: ainda não cobrou **aqui**. Cobrou noutro projeto desta rede, onde a primeira redação da
política dizia "nunca 500" e **criou o bug que existia para impedir**: falha de banco (transitória)
devolvia 200, e a Meta nunca reenviava.*

**As 36h NÃO são rede de segurança para queda longa.** Cobrem um restart de segundos. Consumidor fora
do ar por horas perde evento, e o gateway **nem fica sabendo** — quando os reenvios expiram, a Meta
simplesmente para.

**A Meta NÃO publica o timeout do webhook, nem as retentativas, nem os intervalos.** Qualquer número
nesse lugar é nosso, não dela. Por isso o timeout é configurável por instância e **medido**, nunca
constante mágica.

**A mesma mídia tem mime DIFERENTE no payload da mensagem e no `GET /{media_id}`** —
`audio/ogg; codecs=opus` contra `audio/ogg`. É o `codecs=opus` que faz o WhatsApp renderizar **nota de
voz tocável**; reenviar com o outro entrega **anexo de arquivo**, sem erro nenhum. O parser reporta o
mime do payload **cru, com parâmetro** — normalizar destruiria exatamente o que precisa ser
preservado.
*Custo: cobrou em produção noutro projeto desta rede, num relay de áudio.*
**A defesa é estrutural nas DUAS direções (T-016): o `GET /v1/media/{id}` devolve os dois mimes em
cabeçalhos separados (`X-Zapgw-Mime-Do-Payload`, `X-Zapgw-Mime-Do-Get`) e o `Content-Type` da
resposta é neutro** — pôr um dos dois ali seria o gateway escolhendo, e quem lesse só o
`Content-Type` levaria a escolha errada sem nunca ver que havia duas. No upload
(`internal/meta/media.go`, `UploadMedia`) o mime declarado vai para o fio **com o parâmetro**, e a
tabela de categorias só lê o tipo **base** para decidir teto — nunca reescreve o valor que viaja.
*A mutação que prova: normalizar os dois para um só; ela fica vermelha em dois testes, um deles com
o custo de 2026-07-20 escrito no comentário.*

**Resposta a botão de TEMPLATE chega como `type: "button"`, com o payload em `message.button.payload`
— NÃO como `interactive.button_reply`.** E só o caminho de template funciona **fora** da janela de
24h, que é onde um lembrete de véspera é enviado.
*Custo: noutro projeto desta rede, um laço de confirmação inteiro foi construído — com 11 testes
verdes — sobre um payload que o sistema era **incapaz de produzir**.*

**Lista paginada da Graph API lida sem seguir `paging.next` devolve uma lista PLAUSÍVEL e curta, e
nada acusa.** O gateway antigo desta rede lia só a primeira página do catálogo de templates — **25**
de uma conta com **84**. Não havia erro, status estranho nem log: o sistema consumidor concluía que
o template "não existe" e a mensagem nunca saía. *Custo: foi o que tirou aquele gateway de produção.*
A guarda aqui é dupla e mora em `internal/meta/templates.go`: paginar até `paging.next` **sumir**, e
fazer o teto de páginas **ERRAR** quando estourado — devolver a lista parcial "porque já temos
bastante" é a mesma armadilha com outro número, agora com aparência de decisão deliberada. Pelo
mesmo motivo, item malformado no meio da página é **erro**, nunca pulo: pular é truncar com outro
nome. *A pergunta que pega esta família inteira: se o resultado vier menor do que deveria, alguma
coisa fica vermelha?*

**A MESMA armadilha, na versão "ferramenta de bancada": `len(data)` sem olhar `paging.next` não é
contagem, é tamanho de página — e imprimir isso como se fosse total é falsa precisão.** Medido em
produção em 2026-07-31, na primeira execução real de `zapgw diagnostico` (v0.42.0) contra a Meta de
verdade: `countInstagramConversations` (`internal/meta/instagram_diagnostics.go`) montava a query de
`GET /me/conversations` **sem `limit`**, e as CINCO chamadas (caixa padrão + quatro pastas) devolveram
exatamente **25** — o teto de página padrão da Graph API, não uma coincidência de dado real. O rótulo
*"conversas na caixa padrão: 25"* lia como contagem; era "pelo menos 25, primeira página". E pior: a
varredura de pastas existe para distinguir "não tem DM" de "a DM caiu noutra gaveta" (`requests` traz
DM de quem não segue a conta, e a caixa padrão não), e com o teto de página as quatro pastas saíam
**iguais** — resultado indistinguível de "a Meta ignorou o `folder`". T-112 trocou a comparação por
`paging.next` **presente/ausente** (o mesmo sinal que `templates.go` já usa), não por `N == limite
pedido`: comparar com o limite pedido assumiria que a Meta sempre o honra à risca, o que não foi
verificado nesta fonte. *Custo: nenhum ainda — pego na primeira leitura humana da saída, antes de
qualquer decisão ter sido tomada em cima do número errado. A pergunta que pega esta variante: o
número que a tela mostra tem como a Meta dizer "há mais", e a tela ESCUTA esse sinal?*

🔴 **E a variante que custou mais caro: uma proteção documentada em TRÊS lugares nunca foi exercitada
contra o caso que promete cobrir.** A varredura das quatro pastas extras (`other`, `page_done`,
`spam`, `requests`) existe para distinguir "não tem DM" de "a DM caiu noutra gaveta" — e essa
intenção foi escrita, com essas palavras, em **três** lugares diferentes: o `diag_instagram_meta.py`
doado pelo `consumer-b`, o comentário da T-109 quando o comando foi portado, e o comentário da
própria `InstagramMessagingPermission` (`internal/meta/instagram_diagnostics.go`). **Nenhum dos três
jamais mediu se o parâmetro `folder` realmente filtra alguma coisa.** Medido em produção pela T-113
(2026-07-31 15:31 -03, `v0.42.1`): as CINCO chamadas — caixa padrão + quatro pastas — devolveram
`≥ 50` **em todas**, mesmo pedindo `limit=100` (a Meta também não honra o limite pedido neste
endpoint). `spam` e `page_done` com exatamente o mesmo total que a caixa padrão é implausível o
bastante para desconfiar, mas **não prova sozinho** que o parâmetro é ignorado — o teto de página
(a armadilha logo acima) já tinha mascarado a mesma diferença uma vez, com um teto menor (25).
**A pergunta que faltava fazer desde o início:** um `folder` que a Meta NUNCA documentou
(`folder=zzz-nao-existe`) recebe erro ou a mesma lista? Só essa sonda separa "o parâmetro é ignorado"
de "o teto de página mascara a diferença" — e é isso que a T-113 acrescentou
(`ProbeInvalidInstagramFolder`), sem concluir qual das duas hipóteses vale, porque **medir** é
diferente de **ter três comentários dizendo que já está coberto**.
*Custo real: quatro requisições por chamada de diagnóstico, em produção, desde a T-109
(2026-07-30) até a T-113 (2026-07-31) — um dia inteiro gastando rede e imprimindo cinco linhas `[ok]`
que o operador lia como cinco medições independentes, sem nenhuma delas informar coisa nenhuma sobre
em que pasta uma DM está.*
**O que generaliza, e é o motivo de esta entrada existir:** código que "trata" um caso sem nunca ter
sido exercitado contra ele é **indistinguível** de código que trata — até alguém medir. Um comentário
convincente, repetido em três arquivos, tem exatamente a mesma aparência de uma proteção que funciona
e de uma que nunca funcionou. A única forma de saber a diferença é bater no caso de verdade, e isso
não aconteceu neste projeto até a segunda medição contra a Meta real (T-112 mediu o teto; T-113 mediu
o parâmetro).
**A defesa que ficou:** `internal/meta/instagram_diagnostics.go` deixou de afirmar que a varredura
funciona — `MeasuredFolderResult` (hoje `FolderUnknown`) é o ÚNICO ponto que decide o
comportamento, com o mecanismo de medição (`ProbeInvalidInstagramFolder`, ligado por
`ZAPGW_DIAGNOSTICO_SONDAR_FOLDER` sem precisar recompilar) pronto para a resposta que ainda falta.
Guardas: `TestInstagramMessagingPermissionSweepsTheFourFoldersWhenUnknown` e
`TestInstagramMessagingPermissionStopsSweepingWhenFolderIgnored`
(`internal/meta/instagram_diagnostics_test.go`) prova os dois lados do `if` HOJE, antes de a medição
real acontecer — sem elas, o interruptor só seria testado no dia em que alguém o virasse de verdade,
tarde demais para pegar um `if` invertido.

**E seguir o `paging.next` às cegas manda o token para onde a resposta mandar.** O `next` vem do
CORPO da Meta, e a requisição seguinte leva o `Authorization` da instância. A leitura recusa uma
página cuja origem (esquema + host) não seja a da Graph API configurada, e a URL recusada **não**
entra na mensagem de erro — ela pode carregar credencial na query, e esse texto sobe para log.
*Custo: nenhum ainda; provado por mutação na T-015 — sem a guarda, o cliente ia buscar o host
estranho.*

**Um `200` da Meta NÃO prova que veio id.** A mesma resposta pode trazer `{"messages": []}`, `{}` ou
um id de tipo errado. Devolver isso como sucesso grava id vazio no consumidor e o defeito aparece
muito longe da origem.

**O `verify_token` só é usado no `GET` de verificação, nunca no `POST`.** Trocar o valor sem
re-registrar o webhook na Meta **não quebra tráfego nenhum**: tudo segue normal por semanas, até
alguém re-salvar a URL de callback no painel e a recusa aparecer desconectada da troca antiga.

**Status de template e webhooks de conta NÃO suportam override de webhook** e chegam sempre no
endpoint principal — sem `metadata.phone_number_id`. Confirmado em
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/override/ (lido em
2026-07-26): a página lista os campos de template
(`message_template_status_update`/`message_template_quality_update`/`message_template_components_update`/`template_category_update`)
e de conta (`account_update`/`account_review_update`/`account_alerts`) como não suportando override, e
diz que a Meta os entrega sempre na *"app's default callback URL"*. Consequência para o isolamento
entre inquilinos: uma guarda que só compare `phone_number_id` **não os cobre** — era exatamente essa
lacuna que a T-038 fechou (ver a entrada logo abaixo, "Webhook de CONTA sem roteamento por waba_id
retinha dado de outro inquilino").

**Webhook de CONTA sem roteamento por `waba_id` retinha dado de outro inquilino — não era só
roteamento errado.** A guarda de isolamento entre inquilinos (`internal/inbound/handler.go`, passo 5,
desde o plano 1) comparava só `phone_number_id`. Webhook de conta não tem esse campo (entrada acima) —
então a guarda varria zero eventos, nada era recusado, e o `cru` era entregue ao consumidor do slug do
PATH mesmo quando pertencia a outra WABA. O enquadramento certo, e ele veio do consumidor
(2026-07-26), não deste projeto: o consumidor confere `envelope["instancia"]` contra o slug
**configurado nele**, e a guarda **passava** — porque é o próprio gateway quem põe o slug do caminho
no envelope. Resultado: o consumidor A **grava em banco, em definitivo**, o cru de um evento que podia
ser do inquilino B. Roteamento errado se conserta reapontando; dado gravado no banco de terceiro não
se desfaz.

**A fonte para "entry[].id É o WABA ID" foi conferida antes de rotear por ele, não suposta.** A
tarefa (T-038) recomendava rotear por `waba_id`, mas exigia confirmar a fonte antes — o
`docs/ARMADILHAS.md` deste projeto já registra cinco casos, num único dia, em que uma afirmação não
conferida caiu. Duas páginas oficiais da Meta, lidas em 2026-07-26, confirmam: a tabela de parâmetros
de developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
descreve `<WHATSAPP_BUSINESS_ACCOUNT_ID>` (o valor de `entry[].id` no exemplo) como *"WhatsApp
Business Account ID."*; o exemplo de
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/message_template_status_update/
mostra o mesmo campo `"id"` no nível de `entry`, fora de `changes`/`value`, com uma descrição idêntica.
As duas fontes bastaram — path (a) da tarefa (rotear por `waba_id`) ficou confirmado, path (b)
(descartar webhook de conta sem entregar) não foi necessário.

**A guarda NÃO reaponta a entrega pelo `waba_id` do corpo — ela só valida o `waba_id` da instância que
o PATH já escolheu.** A tentação óbvia, lendo "rotear por waba_id", seria usar
`config.Store.InstanceByWabaID` (que já existia, sem chamador, desde antes desta tarefa) para
descobrir a instância DONA daquele `waba_id` e entregar a ELA — mas isso quebraria a invariante escrita
no topo do próprio arquivo: *"a instância vem do CAMINHO, nunca do corpo"* (você não pode saber de quem
é o corpo antes de confiar nele, e confiar exige já saber qual segredo usar). O conserto certo é
simétrico ao do `phone_number_id`, que já existia: comparar o `waba_id` do corpo contra o `WabaID` **da
instância já resolvida pelo path** — se bater, entrega; se não bater, descarta com `200` + `ALARME` +
contador (`config.CounterAccountDiscarded`), nunca re-roteia para a instância certa. `InstanceByWabaID`
segue sem chamador em produção; existe hoje só para o teste que prova a chave composta
(`TestStoreFindsByPhoneNumberIDAndByWabaID`, `internal/config/store_test.go`).

**Enumerar os nomes dos campos de conta (`message_template_status_update` etc.) na guarda travaria a
defesa no vocabulário de HOJE.** A Meta pode acrescentar um campo de conta novo sem avisar (mesma
armadilha genérica, seção *Meta / WhatsApp Cloud API*, acima) — se a guarda checasse uma lista fechada
de nomes, um campo de conta futuro que a lista não citasse passaria batido, sem checagem nenhuma. A
guarda em `internal/meta/parse.go` (`AccountWabaIDsInPayload`) usa o critério estrutural oposto:
`field != "messages"` — `"messages"` é o ÚNICO campo que este gateway sabe rotear por
`phone_number_id`; qualquer outro cai no fallback por `waba_id`, que é a única chave que toda mudança
de conta carrega, documentada ou não.

**Mutação obrigatória, feita e revertida antes do commit:** trocar a comparação de
`waba != inst.WabaID` para `waba != inst.PhoneNumberID` em `internal/inbound/handler.go` deixa
`TestHandlerRejectsAccountWebhookFromAnotherWaba` E `TestHandlerDeliversAccountWebhookWithMatchingWabaID`
vermelhos (`internal/inbound/handler_test.go`) — o fixture de prova usa de propósito um `waba_id` IGUAL
ao `phone_number_id` da instância (`"PNID1"`, não `"WABA1"`), exatamente para que comparar contra o
campo errado produza o resultado errado, e não passe por coincidência.
*Custo: zero em produção — há uma única instância hoje, e a tarefa (T-038) entrou na fila exatamente
para fechar isto ANTES do segundo inquilino existir, não depois de um vazamento real.*

**Guarda que responde `200` DESCARTANDO não é fronteira entre inquilinos — a fronteira é a que
responde `403`, e a doutrina deste projeto chamou as duas pelo mesmo nome por dois planos.** As
guardas 5a (`phone_number_id`) e 5b (`waba_id`), em `internal/inbound/handler.go`, vinham
sendo descritas — aqui, no contrato e nas tarefas — como *"a guarda de isolamento entre inquilinos"*.
Elas não são isso. Elas conferem se o **endereçamento** do que chegou bate com o que está cadastrado
naquele caminho, e respondem `200` jogando o lote fora. **Quem separa inquilino é a assinatura por
instância, no passo 3 (`:183-189`), conferida com o `app_secret` DAQUELA instância, e ela responde
`403` — nada chega ao passo 4 nem ao 5 sem passar por ela** (exercitado com tráfego na T-042: os
mesmos bytes, com a mesma assinatura, no caminho da outra instância → `403`).

**A diferença não é semântica, e o corolário é o que ninguém deduz sozinho: as guardas ficam MUDAS
quando dois Apps compartilham número e WABA.** Nesse caso o `phone_number_id` bate, o `waba_id` bate,
nenhum `ALARME` sai e **nenhum contador se move** — `numero_descartado` e `conta_descartada` seguem
em zero porque, para elas, está tudo certo. Quem tratar esse silêncio como *"não há convivência neste
número"* conclui errado. O que continua garantido é a outra camada: o outro App tem outro
`app_secret`, então o webhook dele leva `403`. **Nenhuma contramedida nova foi criada de propósito**
— guarda extra para "ids iguais" seria complexidade sem ameaça, já que a camada que decide é outra.

*Custo: um alarme falso mandado a um consumidor e retirado 11 minutos depois. Em 2026-07-28 medi 16
`InboundEvent` de 12–15/07 no banco de produção com os ids de roteamento batendo, e às 09:25 escrevi
no canal do `consumer-a` uma leitura daquele tráfego — "conteúdo de mensagem incluído" — que estava*
**correta sobre o formato e enganosa sobre o significado**, *porque o enquadramento insinuava um
segundo negócio plugado no número de produção. Retirada às 09:36, com a explicação do dono: ele usou
o mesmo número para validar, e o App do outro negócio está parado. Não havia convivência nenhuma.*
**Perguntar ao dono custava uma linha e veio DEPOIS do alarme.**

**⚠️ E a frase "a assinatura é a fronteira" tem uma CONDIÇÃO — escrevê-la sem ela seria trocar um doc
errado por outro, que é o modo de falha típico de auditoria (ver *Documentação*, "a caça a doc falso
produz doc falso").** Ela vale porque **cada consumidor usa um App próprio** (decisão do dono,
2026-07-26, registrada no comentário do passo 5 de `internal/inbound/handler.go`), e App próprio
implica `app_secret` próprio. **Nada no gateway impede duas instâncias no MESMO App:** a coluna
`app_secret` de `instancia` é `TEXT NOT NULL`, sem `UNIQUE` (`internal/config/store.go:306`). Nessa configuração
as duas instâncias compartilham o segredo, a assinatura **para de distinguir** uma da outra, e 5a/5b
passam a ser **a única separação** — que é precisamente a configuração para a qual elas foram
escritas (o override de webhook por número, citado no comentário do passo 5, é o caminho de quem
puser dois números no mesmo App; e o 5b nasceu na T-038 justamente para o App com mais de uma WABA).
**Então o que esta entrada corrige é o NOME, não o valor das guardas:** hoje elas conferem
endereçamento; num App compartilhado elas seriam a fronteira. Doc que afirme qualquer um dos dois
como incondicional está errado metade do tempo.

**Duas lições, e a segunda é a que sobrevive à correção:** (1) **medição não traz contexto junto** —
eu tinha os hashes e não tinha a história, e os hashes sozinhos sustentavam uma conclusão pior que a
verdade; (2) o alarme caiu, mas **a doutrina errada não caía com ele**: chamar de "isolamento" uma
guarda que responde `200` treina o leitor a ler o contador dela como medida de invasão. **A pergunta
que separa as duas classes, e é grátis: esta guarda RECUSA o pedido ou CONFERE o endereço? Se a
resposta que ela dá é `200`, ela é a segunda — e o zero dela não prova o que você quer que prove.**

**Reação SEM o campo `emoji` é a Meta dizendo "removi a reação" — não é payload malformado.**
A T-023 (`docs/TASKS.md`) foi escrita achando o contrário: o próprio texto da tarefa pedia para
tratar "reação sem emoji no payload" como erro de parse contado. A doc oficial da Meta
(developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/reaction/,
lida em 2026-07-26) diz o oposto: *"When an end user removes a reaction emoji, a webhook without the
'emoji' field will be sent."* Seguir a tarefa ao pé da letra teria produzido exatamente o defeito que
a T-023 inteira existe para fechar — um evento legítimo (a remoção) contado como `parse_error` e
descartado do `eventos`, silenciosamente indistinguível de um payload de verdade corrompido.
*Custo: zero — pego ANTES de virar bug, ao verificar a doc da Meta antes de escrever o teste que a
tarefa pedia (doutrina deste projeto: "confira cada afirmação contra o código/fonte, nunca contra a
doc antiga" — aqui a "doc antiga" era a própria tarefa, escrita sem consultar a fonte). Conserto:
`internal/meta/parse.go` só conta como malformada a reação SEM `message_id` (o alvo, que a Meta
sempre manda); `emoji` ausente vira um `Reacao{Alvo: …}` sem a chave `emoji`, repassando a mesma
ausência que a Meta usa. Provado por
`TestParseWebhookAReactionWithoutAnEmojiIsAValidRemovalNotAParseError`, que fica vermelho se alguém "corrigir"
essa guarda de volta para o texto literal da tarefa.* **A pergunta que teria evitado escrever a
tarefa errada: "esta afirmação sobre o formato da Meta foi conferida na fonte, ou é dedução de quem
escreveu a spec?"**

**No ENVIO a regra é a OPOSTA do recebimento, e uma busca de IA teria feito escrever o código
errado.** A T-024 pediu para descobrir como se remove uma reação pelo ENVIO. Uma busca web
resumiu (2026-07-26) que "emoji vazio remove a reação" seria comportamento da WhatsApp Cloud
API — mas a fonte por trás do resumo era um agregador terceiro (não a Meta), e a mesma busca
trouxe de brinde um "unreact" que é de **outro produto da Meta** (Messenger Platform,
`sender_action: "unreact"`), não da WhatsApp Cloud API. O fetch direto da página oficial
(developers.facebook.com/docs/whatsapp/cloud-api/messages/reaction-messages, lida em
2026-07-26) contradisse os dois: lista `emoji` como *"Required"* e não menciona remoção em
lugar nenhum. Ou seja, no RECEBIMENTO ausência de `emoji` É a remoção (ver a entrada acima);
no ENVIO a mesma ausência **não tem semântica documentada nenhuma** — as duas metades da mesma
funcionalidade não são espelhadas, e tratar como se fossem teria produzido um envio que a Meta
aceita com `200` sem remover nada.
*Custo: zero — pego ANTES de escrever código, seguindo a instrução da própria tarefa de não
adivinhar sem fonte. Conserto (T-024): `internal/outbound/message.go`, `validateReaction` recusa
`emoji` vazio/ausente no envio com `ErrRemocaoDeReacaoNaoSuportada`, e só o caso de ADICIONAR é
suportado. **A pergunta que teria pego mais cedo: um resumo de busca sobre uma API está citando
a doc da API certa, ou a de um produto vizinho da mesma empresa?** — buscas por "reaction" e
"unreact" da Meta cruzam Messenger Platform, Instagram e WhatsApp Cloud API livremente, e eles
não compartilham semântica.*
**Atualização (T-027): a recusa acima foi REVERTIDA — `emoji` vazio hoje remove a reação.** A
razão de a recusa ter sido a decisão certa NA ÉPOCA continua verdadeira (nenhuma fonte
confiável sustentava remoção); o que mudou é que uma fonte passou a existir (ver a entrada
abaixo). `ErrRemocaoDeReacaoNaoSuportada` foi removido do código — este parágrafo fica como
registro de COMO se chegou à recusa original, não como descrição do comportamento atual.

**Sucesso da API não é sucesso do efeito; quando a única testemunha é o aparelho do cliente,
"documentar" significa ir olhar.** (Formulação do consumer-a, 2026-07-26 — mais precisa que
qualquer coisa que este projeto tivesse escrito sozinho.) A T-027 finalmente teve fonte para a
remoção de reação pelo envio: o consumer-a fez o experimento com APARELHO em 2026-07-26
(10:15 -03), com o dono olhando a tela, porque nenhuma doc — nem a oficial, nem um agregador —
descrevia o caminho (ver a entrada acima). Dois envios pela Graph API direta, mesmo corpo, só o
`emoji` mudando: `{"emoji":"👍"}` fez a reação APARECER no aparelho; o MESMO corpo com
`{"emoji":""}` fez a reação SUMIR. **O detalhe que carrega toda a armadilha: nos dois envios a
Meta respondeu `200` com um `wa_message_id` NOVO.** Se a reação NÃO tivesse sumido no segundo
envio, a resposta teria sido byte a byte a mesma — o `200` prova que a Meta ACEITOU o pedido,
nunca que o EFEITO aconteceu. Um teste automatizado, um `curl` local, uma leitura de doc: nenhum
desses métodos teria distinguido os dois desfechos, porque os dois produzem a mesma resposta.
Só o aparelho do dono, olhado ao vivo, decidiu qual dos dois é verdade.
*Custo: nenhum em produção — mas o custo em TEMPO foi real: a tarefa ficou parada de 2026-07-24
até o experimento em 2026-07-26 porque nenhuma fonte de escritório (doc oficial, agregador,
busca) bastava, e adivinhar teria arriscado uma remoção que a Meta aceita e não executa, sem
NENHUM sinal de erro em lugar nenhum. Conserto (T-027): `internal/outbound/message.go`,
`ReacaoPedido.Emoji` virou `*string` (nil = chave ausente = erro; aponta para `""` = remoção;
aponta para valor = adiciona) e `validateReaction` aceita emoji vazio; `internal/outbound/body.go`
manda a chave `"emoji"` SEMPRE, mesmo vazia — um `omitempty` (ou um `if != "" { ... }`)
apagaria a chave e transformaria toda remoção num pedido sem efeito, com a Meta respondendo
`200` do mesmo jeito. Provado por mutação: `TestReactionBodyWithEmptyEmojiSendsTheKeyEmpty`
fica vermelho se a chave virar condicional. **A pergunta que generaliza: para este efeito, existe
ALGUMA forma de a resposta da API divergir entre "aconteceu" e "não aconteceu"? Se não, a única
prova é foto/vídeo/testemunha de fora do sistema — e "documentar sem fonte" vira "documentar sem
checar", que é pior.***

**Um fixture "correto pela doc" pode estar testando o caso RARO, e só a captura real revela
qual caso é o comum.** A T-026 trocou `testdata/corpus/localizacao.json` (derivado da doc) por
captura real do consumer-a (2026-07-26). A doc da Meta mostra um exemplo de `location` com `name`
e `address` preenchidos (pin de estabelecimento), e o fixture antigo copiou esse exemplo — os dois
campos são tecnicamente opcionais segundo a mesma doc, mas nada nela diz qual dos dois casos é
mais frequente. A captura real mostrou o oposto do que o fixture testava: o pin SOLTO — sem `name`
e sem `address` — é o que a Meta manda quando alguém compartilha localização pelo app normalmente;
o pin com nome/endereço é o caso do botão "compartilhar um local" (estabelecimento). Um fixture que
só testa o caso raro deixa o caminho comum (os dois campos ausentes) sem cobertura nenhuma — e é
exatamente o caminho que todo consumidor real vai bater primeiro.
*Custo: zero — a divergência não quebrou nada porque `Localizacao.Name`/`Localizacao.Endereco` já
usam `omitempty` e o parser já lê os dois como opcionais desde a T-023; o achado é sobre COBERTURA
de teste, não sobre o código. Conserto: `localizacao.json` passou a ser o pin solto (captura real),
e o caso com nome/endereço ganhou um teste sintético próprio
(`TestParseWebhookReadsALocationWithNameAndAddress`, `internal/meta/parse_test.go`) para não perder
cobertura do caminho documentado. **A pergunta que generaliza: um exemplo de doc que "bate" com o
código prova que o CAMINHO é aceito, nunca que é o caminho USUAL** — e corpus que só existe para
provar aceitação, sem nunca ter visto tráfego real, não sabe dizer qual ramo importa mais.*

**Um corpus com um único emoji de UM codepoint não prova nada sobre emoji de VÁRIOS.** O fixture
antigo de `reacao.json` (derivado da doc) usava `"👍"` — um único codepoint. A captura real do
consumer-a (2026-07-26) trouxe `"❤️"`, que é **dois** codepoints (`U+2764` HEAVY BLACK HEART +
`U+FE0F` VARIATION SELECTOR-16) — o emoji mais comum do WhatsApp é exatamente desse tipo composto
(coração, várias faces, bandeiras), então um corpus que só testa emoji de um codepoint nunca
exercita o caminho onde truncamento por "pega o primeiro caractere" quebraria em silêncio.
*Custo: zero — nenhum código deste projeto trunca emoji hoje (`Reacao.Emoji` é `string` e passa
direto), mas a mutação provou o risco: substituindo `Emoji: m.Reaction.Emoji` por uma versão que
mantém só o primeiro rune, `TestCorpusInteiro/reacao.json` e `TestParseWebhookReadsAReaction` ficam
vermelhos — e só ficam porque o fixture agora tem um emoji de dois codepoints. Com o `"👍"` antigo
(um codepoint só) a mesma mutação passaria despercebida. **A pergunta que generaliza: este valor de
teste tem a MESMA forma (contagem de bytes/runes/níveis) do valor mais comum em produção, ou só a
mesma aparência ao olho?***

**O `id` do evento de reação e o `message_id` do alvo são dois campos DIFERENTES, e só a captura
real prova a ordem certa.** A T-026 pediu para conferir se `messageEvent`
(`internal/meta/parse.go:750`) usa o id do EVENTO (`m.ID`) para `Evento.ID` e o `message_id` do
alvo (`m.Reaction.MessageID`) para `Reacao.Target` — não o contrário. Conferido: **estava certo**, e
a captura do consumer-a (2026-07-26, dois eventos com `id` de evento e `reaction.message_id`
sempre diferentes um do outro) confirma por observação que os dois valores nunca coincidem em
produção. Não é achado (não havia bug), mas a mutação que prova que o teste pegaria o contrário
está registrada: trocar `Alvo: m.Reaction.MessageID` por `Alvo: m.ID` deixa
`TestCorpusInteiro/reacao.json`, `TestCorpusInteiro/reacao_removida.json`,
`TestParseWebhookReadsAReaction` e `TestParseWebhookAReactionWithoutAnEmojiIsAValidRemovalNotAParseError`
vermelhos — os quatro comparam contra `wamid.TESTE001`, que só aparece como alvo no fixture, nunca
como id de evento.

**`errors[]` existe DENTRO de `messages[]`, não só dentro de `statuses[]` — e é a MESMA armadilha-mãe
deste arquivo, na terceira vez.** A T-023 consertou o envelope de mensagem para reação (chegava
rotulada e sem conteúdo) e esqueceu o evento de status; a T-028 consertou o evento de status para
`errors[]` (motivo de falha) e ninguém perguntou se a Meta também manda `errors[]` dentro de uma
MENSAGEM. Manda: quando ela recebe algo que a Cloud API não sabe representar, o `sub_tipo` chega
`"unsupported"` com `errors[]` ao lado de `from`/`id`/`timestamp` — formato confirmado em
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/
(lido em 2026-07-26), código `131051`, `"Message type unknown"`. `messageMeta` não tinha campo
`Errors` até a T-033: o evento saía com um id e um rótulo, e nada mais — indistinguível de "mensagem
vazia" para o consumidor, quando a verdade era "a Meta recebeu algo e não conseguiu decodificar".
*Custo: zero em produção — nenhum consumidor tinha recebido `unsupported` ainda. Achado pelo
consumer-a auditando os **218 payloads crus** que já tinha gravado (contando as chaves que a Meta
manda contra as que o código lê), não por um teste novo nem por tráfego novo — é um QUARTO jeito de
achar esta família de defeito, ao lado de "revisão adversarial" (T-023/T-028) e "captura real"
(T-026): **reler o que já foi capturado com a pergunta certa.** Conserto: `mensagemMeta.Errors
[]json.RawMessage`, reusando o MESMO `StatusError` e o MESMO campo `Evento.Erro` que o status já
usava — não um tipo irmão com nome parecido, porque o formato do item é idêntico nos dois lugares e
só o SIGNIFICADO muda com o `tipo` do evento (documentado em `internal/meta/types.go` e
`docs/CONTRATO-CONSUMIDOR.md`, para não ficar só na cabeça de quem consertou).* **A pergunta que
continua sendo a mesma, e vale repetir pela terceira vez: "onde mais esta mesma frase deveria ser
verdadeira, e é?"** — desta vez a resposta era "no MESMO arquivo, um nível ao lado", não noutro
repositório nem noutra linguagem.

**Um `200` no ENVIO tem a MESMA armadilha que um `200` no recebimento, e por pouco não virou o
QUINTO caso da armadilha-mãe.** `idDaResposta` (hoje `sendResponse`,
`internal/meta/client.go`) lia só `messages[0].id` da resposta de `POST /{phone_number_id}/messages`
— o mesmo objeto também traz, às vezes, `message_status`. Antes da T-034, um envio de template sob
pacing que a Meta segurasse (`held_for_quality_assessment`) ou recusasse depois
(`paused`/dropped por feedback negativo) devolvia o **mesmo** `200`+`wamid` de um envio normal — o
consumidor gravava "enviado" para uma mensagem que podia nunca chegar. É a MESMA frase da entrada
"Um `200` da Meta NÃO prova que veio id" (mais acima, sobre id ausente/vazio/tipo errado), aplicada
a um campo VIZINHO no mesmo objeto que ninguém tinha olhado ainda.
*Custo: zero em produção — nenhum template deste gateway tinha entrado em pacing ainda. Achado ao
pesquisar (a pedido do dono, T-034) se havia alternativa a um aparelho físico para confirmar o
desfecho de uma reação (T-027) — a resposta a essa pergunta foi "não há", e o motivo documentado é
justamente que `accepted` só significa "a Meta aceitou o pedido", nunca "o efeito aconteceu"; ao
verificar a fonte dessa frase, apareceu o campo que o gateway não lia.*
**A fonte tem uma contradição interna que vale registrar, para quem for mexer aqui de novo:** a
página de referência da API
(`developers.facebook.com/docs/whatsapp/cloud-api/reference/messages`, lida em 2026-07-26) lista
`paused` como um dos TRÊS valores do próprio `message_status` ("paused: the message delivery has
been paused" — leitura: da MENSAGEM). Já a página de pacing de template
(`developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-pacing`,
mesma data) descreve "paused" como o `status` do PRÓPRIO TEMPLATE virando `PAUSED` — um campo
DIFERENTE, do TEMPLATE. As duas são páginas oficiais da Meta e discordam uma da outra sobre o que
"paused" é. Conserto: `RespostaEnvio.MessageStatus` repassa o valor CRU, sem decidir qual das duas
leituras vale — inventar uma tradução aqui seria a mesma armadilha de "Duas fontes independentes que
descem do mesmo nada" (ver *Documentação*, abaixo), só que com fontes OFICIAIS discordando entre si
em vez de fontes não-oficiais concordando por acidente.
**E a mutação que prova a outra metade:** o campo vem ausente para todo envio que não seja template
sob pacing (confirmado pela própria página de referência: *"only included in responses when sending
a template message that uses a template that is being paced"*) — ausência e `"accepted"` são
desfechos que o CONSUMIDOR trata igual (a Meta aceitou), mas o CÓDIGO não pode fabricar `"accepted"`
para a ausência, porque isso afirmaria um valor que a Meta nunca mandou. Provado por mutação em
`TestSendMessageAnAbsentMessageStatusStaysEmptyNotAccepted`
(`internal/meta/client_test.go`): trocar o `MessageStatus` ausente por um `"accepted"` fabricado
deixa esse teste vermelho, mesmo sem mudar nada visível no corpo HTTP (a distinção só importa
internamente — ver `sendResponse`).

**Classificar erro de terceiro pelo ENVELOPE (status HTTP) em vez de pelo CONTEÚDO (código) funciona
até o terceiro mudar o envelope — e a mudança não gera erro nenhum, só uma classificação errada em
silêncio.** `classOfStatus` (`internal/meta/errors.go`) decidia `retentavel` × `permanente` × `config`
olhando só o status HTTP; nenhum código de erro da Meta era consultado no caminho de envio. A doc
oficial de códigos de erro (`developers.facebook.com/docs/whatsapp/cloud-api/support/error-codes`,
seção *Throttling errors*, lida em 2026-08-20) **não declara** com que status a família de limite de
taxa chega, e a Marketing Messages API mostra um erro do mesmo feitio chegando como `400`. Se um
throttling chegasse embrulhado em `400`, caía no default (`permanente`) e o consumidor parava de
tentar — exatamente quando esperar e tentar de novo era a solução.
*Este projeto já tinha visto o mesmo formato de defeito, no MESMO arquivo: `classOfStatus` trata
`408` e `425` como retentáveis "por definição do HTTP" precisamente porque deixá-los cair no default
faria o consumidor desistir de algo recuperável — o comentário ali já registrava o risco de status
inesperado carregando um significado diferente do que o código presume. A tabela de throttling
(T-142) é o mesmo raciocínio aplicado ao lado do CÓDIGO da Meta, não do status HTTP: o código não
tem a ambiguidade de envelope que o status tem.*
*Custo: **não cobrou** — achado em auditoria (T-142), lendo o contrato contra o código, antes de
qualquer consumidor receber um throttling embrulhado em `400`.*
**O conserto:** uma tabela nossa e conservadora de códigos de throttling
(`retryableCodesByNature`, `internal/meta/errors.go`) que só PROMOVE um status que caiu no
default (`ClassPermanent`) para `ClassRetryable` — nunca rebaixa `ClassConfig` nem o que o
status já classificou como `ClassRetryable`. Guardas em `internal/meta/errors_test.go`, entre elas
`TestClassifyAThrottlingCodePromotesPermanentToRetryable` (o caso central) e
`TestClassifyAThrottlingCodeDoesNotDowngradeClassConfig` (a ordem importa: a tabela é uma segunda
chance, não um segundo juiz).

---

### "Ainda no catálogo" NÃO é "não foi apagado" — a Meta deixa o template visível em `PENDING_DELETION` (2026-08-28)

Apagar um template **nem sempre** o faz sumir do catálogo. Verbatim da Meta, lido em 2026-08-28 em
developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management:

> *"If you delete a template that has been sent in a template message but has yet to be delivered
> (for example, because the WhatsApp user's phone is turned off), the template's status is set to
> `PENDING_DELETION` and WhatsApp attempts delivery for 30 days."*

**A armadilha é para quem confirma exclusão relendo o catálogo** — que é exatamente o que este
gateway faz quando a chamada termina sem resposta da Meta (T-173, `deletionAccepted` em
`internal/outbound/templates_handler.go`). A regra ingênua *"sumiu = apagado; ainda lá =
inconclusivo"* reporta **dúvida em cima de sucesso**, e numa limpeza de dezenas isso vira item na
pilha de "não sei" que estava certo o tempo todo. A regra certa: ausente **ou** todas as linhas
restantes em `PENDING_DELETION` → apagado; qualquer linha viva sob outro status → inconclusivo.

*Não cobrou nada, e só porque a página foi aberta.* O desenho errado esteve escrito e enviado a um
consumidor por oito minutos, em 2026-08-28: a regra ingênua foi mandada pelo canal às 11:18 e
corrigida às 11:26, **depois** de um `GET` na documentação que tinha sido disparado por outro
motivo — confirmar um prazo de 30 dias que o consumidor havia citado. **O achado não foi procurado:
ele estava na mesma página.** É o argumento mais barato que existe a favor de abrir a fonte em vez de
aceitar a citação: você paga um `GET` e leva o parágrafo vizinho de graça.

**Corolário que vale além da Meta:** quando um sistema de terceiro tem estado de transição
(`PENDING_*`, `DELETING`, `DRAINING`), a sua releitura de confirmação precisa saber **quais estados
contam como sucesso**. Tratar "ainda existe" como falha é o modo de falha padrão de toda confirmação
por releitura, e ele produz retrabalho, não erro — por isso ninguém o vê.

📊 **Atualização do mesmo dia, e ela NÃO cancela a entrada: em 29 exclusões reais, o
`PENDING_DELETION` não apareceu nenhuma vez** (primeira limpeza pela rota, 2026-08-28; os 29 já
tinham sumido da listagem 1 a 3 minutos depois, com o conjunto de status do consumidor fechando em
`APPROVED`/`REMOVIDO_DA_META` e nada em "outros"). **Nenhum dos 29 tinha envio recente**, e é
plausível — não provado — que ter mensagem em voo seja o gatilho. *Registrado aqui para que ninguém
leia a armadilha como "isto acontece toda hora" e, principalmente, para que ninguém a leia ao
contrário: **caso raro que o código não trata é exatamente o que falha no dia em que acontece**, e
neste o custo é o consumidor refazer trabalho já feito.*

## Telefone brasileiro

**`==` entre dois telefones "normalizados" NÃO prova que são a mesma pessoa.** A Meta **não garante**
a mesma grafia que você cadastrou: o mesmo assinante chega como `5532999990000` e `553299990000`.
*Custo: cobrou em produção noutro projeto desta rede — um E2E em que o webhook gravou, respondeu
`200`, o `==` não casou nada e **nada foi enviado de volta**. De fora era indistinguível de sucesso.*

**A guarda que separa o conserto do estrago é o QUINTO dígito.** Fixo é `55` + DDD + 8 dígitos —
**também 12** —, mas o assinante de celular começa em 6–9 e o de fixo em 2–5. Um
`if len == 12 { insere o 9 }` produziria números **inexistentes** para todo telefone fixo do país.

**`sha256(telefone)` NÃO anonimiza — é a APARÊNCIA de anonimização, e o espaço de busca é pequeno
demais para esconder nada.** Ao desenhar o log de TRÂNSITO (T-091, "esta mensagem passou por aqui?"),
a tentação óbvia era indexar por hash simples do telefone. Um celular brasileiro tem espaço de busca
pequeno e muito estruturado — `55` fixo, DDD de duas letras dentre ~67 válidos, prefixo `9` do
celular, e só 8 dígitos livres —, então enumerar e hashear **todo** número possível de um DDD e
comparar contra a tabela quebra por força bruta em segundos num notebook comum. Um hash sem chave
não é segredo: é só uma tabela-verdade publicada com outra roupa.
*O que anonimiza de fato é HMAC-SHA256 com CHAVE, e a chave tem de morar FORA do que pode vazar
junto com o hash — aqui, derivada da `ZAPGW_CHAVE_CIFRA`, que já vive fora do banco (é o que torna o
backup do SQLite seguro de existir). Com chave, quem rouba o banco não consegue enumerar os números
que falaram com o gateway; sem chave, o "anonimizado" se abre com um script.* Ver
`internal/config/crypto.go` (`Cofre.DeterministicHMAC`) e `internal/config/transit.go`.

---

## Testes

🔥 **`-ldflags -X` PARA SÍMBOLO QUE NÃO EXISTE É IGNORADO EM SILÊNCIO — e é o deploy inteiro mentindo
com push verde (2026-08-30, T-172 leva 5).** `implanta/deploy.sh:286` injeta a versão no binário com
`-ldflags "-X main.version=$VERSAO_DO_BUILD"`. O rename da camada 2 mudou aquela variável de
`versao` para `version`. **Se o implementador tivesse renomeado só o código:**

- o `go build` sairia **`0`** — o linker não reclama de `-X` para símbolo inexistente;
- o binário publicado responderia `versao: "desenvolvimento"` no `/v1/health`;
- e o `deploy.sh` **não abortaria**, porque `imprimir_versao_do_corpo` (linha 187) só **imprime** —
  há um comentário no próprio script dizendo que ela nunca retorna erro.

*Controle positivo medido, não deduzido:* `go build -ldflags "-X main.versaoQueNaoExiste=9.9.9"`
saiu `0` e o binário respondeu `desenvolvimento`; com `-X main.version=1.2.3-teste` respondeu
`1.2.3-teste`. **Custo real: zero — o implementador mudou os dois lados no mesmo commit e disse
isso no relatório.** Não houve mecanismo nenhum protegendo: foi atenção de quem estava lá.

🔴 **A família, e ela é maior que Go:** *contrato entre um arquivo e outro, escrito em texto que
nenhum compilador lê.* `-X main.X` na linha do build, nome de coluna em SQL, chave de variável de
ambiente, nome de campo em `json:"…"`, caminho num `ExecStart`. **A camada 2 inteira se apoiou na
frase "o compilador prova que o rename está completo" — e esta é exatamente a fronteira onde ela
deixa de valer.**

✅ **FECHADO em 2026-08-30 pela T-184**, no mesmo dia: o `deploy.sh` passou a **comparar** a versao que o gateway respondeu com a que ele acabou de construir, e a **abortar e reverter** quando divergem — distinguindo isso de *"nao consegui ler a versao"*, que continua nao sendo falha (binario anterior a T-025, ou deploy com `ZAPGW_DEPLOY_BINARIO`). ✅ **PROVADO EM PRODUCAO em 2026-08-30 23:45**, no CT real, com o consumidor avisado e a janela combinada: um build com o simbolo do `-X` trocado de proposito publicou um binario que respondia `versao: "desenvolvimento"`. **O `/v1/health` devolveu `{"ok":true,...}` — ou seja, a logica ANTIGA teria aprovado.** A conferencia nova acusou, reverteu sozinha, o binario anterior voltou (`d8df2bf6…`), o gateway respondeu `0.60.0` e o script saiu **`1`**. *O mecanismo reprovou uma vez, contra dado real, em producao.*

**O que fica em aberto, dito com nome:** o `deploy.sh` **não confere** se a versão que subiu é a que
ele acabou de construir. Ele imprime e segue. É a **T-184**.

🔥 **GUARDA QUE CASA NOME DE MÉTODO COMO *STRING* FICA VERDE CASANDO NADA (2026-08-30, T-172 leva 1).**
`cmd/zapgw/menu_test.go` percorre a AST procurando chamadas por **nome literal** — `"ListarInstancias"`,
`"Fechar"`, mais uma lista de métodos proibidos. Na passada de rename para inglês, o código mudou de
nome e **a string não**: a guarda continuaria **passando**, porque ela não encontra o que procura e
"não encontrei nada proibido" é exatamente o resultado verde dela.

*Custo: zero, e só porque a suíte quebrou por OUTRO motivo no mesmo commit e o implementador foi
olhar. Se o rename tivesse sido só nos métodos que a lista de proibidos cita, ele passaria limpo — e
a guarda de isolamento estaria desligada em silêncio, com o teste verde ao lado.*

**A família é a do monitor cego**, e o traço que a identifica é este: *a guarda distingue "achei e
está errado" de "achei e está certo", mas NÃO distingue nenhum dos dois de "não achei nada".* Toda
varredura que casa identificador por string tem esse buraco — e ela é invisível ao compilador, que é
justamente a prova em que a camada 2 se apoia.

**O que fazer com uma dessas:** ou ela **falha quando não encontra o alvo** (contagem esperada > 0,
declarada no próprio teste), ou o nome sai da string e vira referência que o compilador enxerga.
*Varredura por nome sem piso de contagem é um teste que se aposenta sozinho no primeiro refactor.*
**As outras duas varreduras por texto deste repositório foram conferidas na mesma hora** e não têm o
defeito: a de TLS casa `InsecureSkipVerify` (nome de terceiro, não renomeável por nós) e a de
telefone casa dígito.

🔥 **Instrumento que não sabe ler uma coisa devolve uma lista APARENTEMENTE COMPLETA, não um aviso.**
Em 2026-08-20, provando a `v0.50.0` em aparelho, sete mensagens foram enviadas e o dono colou a
**exportação em texto** da conversa do WhatsApp como prova de entrega. Vieram quatro das cinco que a
Meta aceitou; a que faltava era justamente a única `cta_url` — e eu concluí, com o intervalo de
horário fechando certinho, que o gateway tinha recebido `200` **sem entregar**. Cheguei a publicar a
suspeita no canal do consumidor.
**A mensagem tinha chegado.** A captura de tela mostrou as cinco. **A exportação do WhatsApp não lista
mensagem `cta_url`** — e não diz que não lista: ela devolve o resto como se fosse tudo.
*Custo real: um alarme falso publicado a um consumidor, e o desmentido três minutos depois.* Barato
só porque a suspeita foi rotulada como suspeita; apresentada como fato, teria tirado do ar um caminho
que funciona.
**A regra, e ela é a do monitor cego com o alvo trocado:** *o instrumento também tem ponto cego, e o
ponto cego se PARECE com o defeito que você procura.* Antes de concluir "não chegou" a partir de uma
ferramenta de leitura, pergunte **se aquela ferramenta enxerga esse tipo de coisa** — e prefira a
fonte que não interpreta (a tela, o status da Meta) à que resume. *Irmã da regra de que verificação
precisa distinguir **falhou** de **não consegui verificar**: aqui o instrumento não tinha o segundo
estado, então a ausência virou negativa.*

🔴 **Teste que afirma usando a CONSTANTE de produção não testa nada — ele concorda com o código, seja
o código qual for.** Em 2026-07-30, na T-095, a asserção de que as colunas do `zapgw log` ficam
separadas era `strings.HasPrefix(resto, separadorColunaLog)` — comparando a saída contra a **mesma
constante** que a produziu. Com `separadorColunaLog = ""`, `HasPrefix(x, "")` é **sempre verdadeiro**:
zerar o separador (o defeito exato que o teste existia para pegar) deixava o teste **verde**.
*Custo: zero — quem escreveu o teste o pegou antes do commit, ao perguntar "o que faz este teste ficar
vermelho?". Conserto: literal `"  "` fixo dentro do teste, independente da produção; conferido que com
a constante zerada ele fica vermelho e com `"  "` fica verde.*
**A pergunta que generaliza, e vale para toda asserção:** *que mudança no código faz este teste
falhar?* Se a resposta for "nenhuma, porque o valor esperado vem do próprio código", o teste é
**tautologia com aparência de prova** — e é pior que teste nenhum, porque ocupa a vaga do que
protegeria. O valor esperado de um teste é **escrito à mão**, não importado de quem ele deveria
vigiar. *Vale igual para tabela de casos que deriva o esperado com a mesma função que está sendo
testada — mesma tautologia, outra roupa.*

**Tamanho de corpo igual é CORRELAÇÃO; `wamid` e `timestamp` dentro do payload são IDENTIDADE.**
Investigando se a mensagem presa de 2026-07-25 23:40 tinha finalmente sido entregue, o log do proxy
mostrava sete `504` e um `200`, **todos com `RequestContentSize=549`** — tamanho que não aparecia em
mais nenhuma requisição do dia. Parecia conclusivo: mesma mensagem, sequência de falhas terminando em
sucesso. Foi declarado como fato, em resposta ao dono e num parágrafo de `docs/TASKS.md`.
**Era falso.** O consumidor tinha o dado que decide — o `timestamp` **dentro** do payload da Meta,
que diz quando o USUÁRIO enviou — e ele apontava uma mensagem nova (`10:06:30 UTC`), não a presa
(`02:40 UTC`). Duas mensagens de texto curtas colidem em tamanho com facilidade: o envelope da Meta é
quase todo estrutura fixa, e o conteúdo mal move a agulha.
*Custo: uma afirmação errada dita com manchete e escrita no repositório, retratada no mesmo dia
porque o consumidor disse "segue sem chegar" e isso obrigou a reconferir.*
**A regra: o log do proxy responde "quantas e com que status", nunca "qual".** Para identidade, o
dado tem de vir de dentro do payload — e ele existia, bastava pedir. Correlação forte com o dado
errado é mais perigosa que dado nenhum, porque ela convence.

**Para provar que uma dependência SAIU, rode a suíte com ela DESINSTALADA — não com ela presente e
sem uso aparente.** Suíte verde com a biblioteca instalada não distingue "não uso mais" de "uso sem
perceber": um `import` esquecido num caminho pouco exercitado passa despercebido, e o dia da
descoberta é o do primeiro deploy que não a tem.
*Método observado no consumidor (`consumer-a`, 2026-07-26) ao remover a lib privada que motivou
metade das tarefas desta semana: **303 testes com a lib, 303 sem a lib**, mais o build rodando **sem
agente SSH nenhum** — porque a lib vinha de repo privado por SSH, e um build que ainda a puxasse
falharia ali. Duas provas de AUSÊNCIA, não de presença.*
**A generalização vale para este repo:** ao tirar qualquer dependência daqui, a prova não é o teste
passar — é o teste passar **num ambiente onde a dependência não pode ser resolvida**. Vale para
biblioteca, para variável de ambiente e para credencial: o jeito de provar que um `META_GRAPH_TOKEN`
não é mais usado é rodar **sem ele**, não grepar por ele.

**Um `400` com corpo `text/plain` NÃO é o gateway recusando — é o Go recusando antes do handler, e
os dois são indistinguíveis se você só olhar o status.** Uma prova de produção mandou quatro pedidos
inválidos, recebeu `HTTP 400` nos quatro e foi dada como passada. Nenhum dos quatro chegou a ser
validado: o token vinha de
`cat /var/lib/zapgw/consumidor-consumer-a.txt`, o arquivo tem **três linhas de prosa em volta do
valor**, e um header `Authorization` com quebra de linha faz o `net/http` responder
`400 Bad Request` em `text/plain` com `Connection: close`, sem nunca chamar o handler. O resultado
tinha exatamente a forma do sucesso esperado.

Duas regras saem disso, e a segunda é a que salva:

1. **Erro do gateway é `Content-Type: application/json` com `{"erro":{…}}`.** `text/plain` +
   `Connection: close` é o servidor HTTP, não o código. Olhe o corpo, nunca só o status.
2. **Toda prova precisa de um caso de CONTROLE que só passa se a credencial e a rota estiverem
   certas** — um pedido que deve falhar de um jeito *nomeado e diferente*. Sem ele, "recusou" e
   "nem chegou" produzem a mesma saída, e a prova mede o próprio arnês. O controle aqui foi
   `tipo: "nao-existe"`, que devolveu `tipo de mensagem desconhecido` em JSON — e só então os
   quatro `400` passaram a significar algo.

*Custo: uma prova falsa relatada como verdadeira, corrigida no mesmo turno. O sinal que denunciou
foi um `wc -c` no token: 222 bytes onde cabiam 64.*

**Uma guarda que varre um diretório e não acha nada passa VERDE sem ter verificado nada.** Toda
guarda precisa de `assert itens_verificados > 0` **e** de ser vista falhando ao menos uma vez.
*Custo: ainda não cobrou aqui — a guarda do corpus nasceu com as três travas e foi provada falhando
nas três. Cobrou noutro projeto desta rede, onde um teste de arquitetura varria caminhos relativos
ao diretório de trabalho, era rodado de outro lugar e passava sem escanear nada.*

🔥 **Um portão que varre uma LISTA de caminhos só protege o que alguém lembrou de listar — e a RAIZ do
repositório é o caminho que todo mundo esquece, porque nada mora lá no dia em que a lista é escrita
(2026-08-30/31, T-191).** O portão de telefone de `internal/config/phones_allowlist_test.go` varria
`scannedTargets`, uma fatia fixa (`cmd`, `internal`, `testdata`, `docs`, `implanta`, `README.md`) que já
cresceu duas vezes (T-159, T-185) porque um lugar novo carregava um número real. A raiz do repositório
nunca esteve nela. O arquivo do canal com o consumidor — telefone real e `wamid` de produção, num
repositório PÚBLICO — foi criado **na raiz**, ficou **untracked**, e o portão não acusou nada: não porque
olhou e passou, mas porque nunca olhou para lá. Ninguém foi avisado, porque o próprio VERDE do teste é a
cara de "nada errado" e de "nada varrido" ao mesmo tempo.
*Custo real: quem percebeu foi o consumidor, indo escrever naquele mesmo caminho. Nenhum `git add -A`
rodou naquela janela — o repositório ficou limpo por sorte, não pelo desenho do portão.*
**O conserto (T-191) inverte a direção da enumeração**, de "quais caminhos lembramos de acrescentar" para
"quais arquivos um `git add -A` pegaria agora": `git ls-files` (rastreados) mais `git ls-files --others
--exclude-standard` (novos e não ignorados) — exatamente o conjunto que é o risco pelo qual este portão
existe. Um arquivo novo em qualquer lugar, inclusive na raiz, entra nesse conjunto no instante em que
existe; ninguém edita mais este teste para acrescentar um diretório. **O lado ignorado continua
deliberadamente fora** — `*.local.md`, o mesmo arquivo do canal, jamais pode ser varrido, ou o verify
ficaria vermelho para sempre por causa de algo que existe de propósito para escapar do commit — então
`git ls-files --others --exclude-standard` (que já exclui o que o `.gitignore` exclui) é exatamente a
metade certa, não um descuido.
**O irmão, conferido antes de fechar, como a doutrina manda:** o portão de TLS
(`internal/inbound/deliver_test.go`, `TestNoSourceInTheRepoTurnsOffTLSVerification`) **não tem** esse
buraco — ele já faz `filepath.WalkDir` na raiz do módulo INTEIRA (filtrando por extensão `.go` e pulando
diretórios ocultos), nunca uma lista fixa de subdiretórios. Um caminho novo acrescentado ao repositório
já entra na varredura dele automaticamente; só o portão de telefone precisava do conserto.

**Um teste de vazamento passa VERDE quando a fixture apaga o ramo que vazaria.** O teste que exige
"nenhum dos quatro segredos aparece na saída de `provisionar instancia`" preenchia os quatro pelo
ambiente — e é justamente aí que o comando **não imprime** a linha que nomeia os segredos sorteados.
A asserção rodava; o código que ela mira, não. Uma mutação que concatenava os quatro valores em claro
naquela linha **passou despercebida**.
*Custo: pego por teste de mutação durante a T-005, antes do commit. Conserto: o caso dos segredos
**sorteados** lê os valores de volta do banco e os procura na saída — é o único jeito de cobrir um
ramo cujos valores o teste não conhece de antemão. Regra geral: o caso de teste tem de fazer o ramo
suspeito EXECUTAR; escolher a fixture mais confortável costuma ser escolher o ramo que não vaza.*

**Um teste de recusa que vira verde porque a PRECONDIÇÃO mudou não prova nada.** Ao trocar a fábrica
de um teste (por exemplo, passar a criar a instância já ativa), confira caso a caso se cada teste
ainda alcança o código que ele testa — ou se uma guarda anterior passou a mascará-lo.
*Cobrou de novo na T-014, na direção oposta: o teste de `403` do probe de saúde rodava sobre a
instância do outro consumidor **pausada**, então com a guarda de vínculo desligada a recusa vinha da
pausa (`503`) e a asserção de status ficava vermelha pela razão errada — a mutação parecia "pega",
mas o `403` nunca tinha sido exercitado. Só depois de ativar a instância do outro consumidor a
mutação revelou o que realmente acontecia: `200`, com o **número do outro inquilino** no corpo. Custo:
uma rodada de mutação, antes do commit. **A fábrica do teste tem de deixar a guarda que ele mira ser
a PRIMEIRA a falar** — por isso o helper passou a receber quais instâncias ficam ativas, em vez de um
booleano.*

**Erro de "vermelho" também tem qualidade.** Um teste que falha por **erro de compilação** prova
menos que um que falha por asserção: o primeiro só mostra que o símbolo não existe, o segundo mostra
que o comportamento está errado. Quando o vermelho vier de compilação, diga isso no relatório.

**Uma asserção "isto não pode vazar" que procura a ENTRADA inteira casa com o texto de AJUDA da
própria mensagem de erro.** A guarda de `callback_url` recusa a URL e explica a regra: *"tem de ser
https:// …"*. O teste exigia que o erro **não contivesse** a URL recusada — e um dos casos recusados
é exatamente `"https://"` (esquema sem host). O erro ficou vermelho sem que nada estivesse errado no
código. *Custo: uma rodada de vermelho falso na T-010, sem chegar ao commit. Conserto: cada caso da
tabela carrega uma `marca` — o pedaço que **identifica o consumidor** (`consumidor.interno`,
`<ip-interno-do-conector>`) — e é ela que é procurada, não a entrada inteira. Casos sem marca (`"https://"`,
`"   "`) não têm o que vazar e não fazem a asserção. A regra geral: procure o **segredo**, não o
input; se os dois se confundem, o caso de teste está mal escolhido.*

**Todos os `httptest.NewTLSServer` do processo usam o MESMO certificado — então "a CA de um não vale
para o outro" passa verde sem testar nada.** O pacote embute um par fixo (válido até 2084). O teste
que exigia que a âncora de uma instância não validasse o consumidor de outra subia dois servidores e
comparava dois certificados **idênticos**: a asserção rodava e não podia falhar por conta do que ela
mira. Só apareceu porque o teste foi escrito antes da implementação e ficou **verde cedo demais** —
`a CA da instancia A validou o certificado do consumidor de B` apareceu na primeira rodada, e a
causa era a fixture, não o código.
*Custo: uma rodada de investigação na T-013, sem chegar ao commit. Conserto: forjar um certificado
autoassinado por servidor (`selfSignedCertificate`, em `internal/inbound/deliver_test.go`) — que é
também o único jeito de testar certificado **vencido**, já que o do `httptest` vale por 58 anos.*

**Cache por chave esconde vazamento entre inquilinos, e o teste "óbvio" não pega.** Duas mutações
diferentes — chave de cache constante e pool de CAs que **acumula** — sobreviveram ao teste
"a CA de A não vale para B": na primeira, o cliente de A ficava cacheado e recusava B pelo motivo
errado; na segunda, o vazamento só se manifesta **depois** de as duas CAs terem passado por lá.
*Conserto: o teste virou uma SEQUÊNCIA com quatro asserções — A passa, B com a CA de A falha, B com a
CA de B passa, e **B com a CA de A falha de novo no fim**. É a última que mata o pool que acumula, e
ela é literalmente a repetição de uma chamada anterior: o que se testa é a resposta MUDAR conforme o
que aconteceu no meio. Regra geral: teste de isolamento entre inquilinos precisa exercitar os dois na
ordem, e voltar.*

**Teste de mutação é a única prova de que um teste testa alguma coisa.** Ao revisar um conjunto que
protege uma ordem (índices de um slice, posições de campos), **quebre-a de propósito** e confirme que
a suíte acusa. Foi assim que a troca de posição entre credenciais foi provada impossível de passar
despercebida.

**Um teste que só olha o valor DEFAULT passa verde mesmo com o mecanismo de INJEÇÃO inteiramente
quebrado — porque o default de sucesso e o default do bug são o MESMO valor.** A T-025 injeta a
versão do binário por `-ldflags "-X main.versao=…"`; sem a flag, o valor é `"desenvolvimento"` nos
dois caminhos (`zapgw versao` e `GET /v1/health`). Um handler que **esquecesse** de ler a variável
`versao` no `/v1/health` e devolvesse `"desenvolvimento"` a dedo continuaria passando nos dois testes
que só conferem o comportamento sem `-ldflags` — os dois concordam com o hardcode por acidente, não
porque provam a leitura da variável. Só um teste que **compila o binário de verdade com
`-X main.versao=9.9.9` e roda os dois caminhos** pega a divergência.
*Custo: zero — provado por mutação antes do commit
(`TestVersionInjectedByLdflagsPropagatesToBothPaths`, `cmd/zapgw/main_test.go`): trocando
`Versao: versao` por `Versao: "desenvolvimento"` no handler de `/v1/health`, os dois testes do
default continuam VERDES e só o teste que compila com a flag acusa. **A pergunta que generaliza:
"este teste ainda falharia se o código sob teste fosse substituído por uma constante igual ao
default?"** — se a resposta for não, o teste só prova o default, nunca o mecanismo.*

**Remover a rota EXATA de mídia (`/v1/media`) não devolve `404` — devolve `307` para a rota de
SUBÁRVORE (`/v1/media/`), e um cliente que segue redirect automaticamente nem percebe.** O
`http.ServeMux` do Go redireciona quando um padrão de subárvore está registrado e falta o exato:
a requisição não chega ao handler, mas o status é `307`, não `404` — quem escrevesse o teste
esperando `404` (o padrão do resto deste arquivo) teria escrito uma asserção que também dá
vermelho, só que pela razão errada. Mais sério: como `307` preserva método e corpo, um consumidor
cujo cliente HTTP segue redirect por padrão (a maioria) reenvia o `POST` para `/v1/media/`, que
ainda casa com o padrão de subárvore registrado, e a mensagem chega ao mesmo handler — o defeito
se autocura PARA ESSE consumidor e falha calado só para quem trata `3xx` como erro (comum em
clientes de API programáticos). Por isso `TestRoutesRegistersTheMediaRoutes`
(`cmd/zapgw/main_test.go`) não confere status: confere que a requisição CHEGOU ao handler, por
efeito colateral (o caminho recebido) — a única asserção que não depende de saber de antemão qual
`3xx` ou `4xx` o ServeMux escolhe.
*Custo: zero — achado durante a mutação obrigatória da T-018, antes do commit.*

🔴 **Teste que aponta `c.base` do CLIENTE e o `base` do PARÂMETRO para o MESMO servidor de mentira
nunca exercita qual dos dois o código realmente usa.** A T-097 escreveu `SendInstagramMessage`
montando a URL sobre `c.base` (o host do WhatsApp, `graph.facebook.com/vNN`) em vez de receber a
base por parâmetro como `RenewInstagramToken` já fazia — e todo teste da época
(`internal/outbound/instagram_test.go`) usava `meta.NovoClient(metaSrv.Client(), metaSrv.URL)`, o
MESMO `metaSrv` para tudo. Com um host só no teste, "usei `c.base`" e "usei a base certa" produzem a MESMA requisição —
a suíte ficava verde nos dois casos. **O defeito só apareceu contra a Meta real**, na primeira
ativação de verdade da `tenant-two-ig` (2026-07-31 00:24 UTC = 2026-07-30 21:24 -03): `Invalid OAuth access token - Cannot
parse access token`, porque um token de Instagram Login não é parseável em `graph.facebook.com`. A
própria T-097 já tinha registrado isso como "limite honesto" no relatório — *os testes provam o
mecanismo, não que a Meta aceita* — e o limite cobrou na primeira instância que chegou a produção
(T-104).
*Custo: uma instância de produção (`tenant-two-ig`) ficou pausada esperando o conserto entre a
primeira ativação de verdade e o fix — a mensagem já tinha sido "provada" fora do gateway (medida
contra a Meta real, `POST https://graph.instagram.com/{ig_id}/messages` → `200`), então o atraso
foi só do gateway não montar a MESMA chamada.*
**A regra que generaliza, além de "não use a constante de produção como esperado" (a entrada logo
acima):** quando o código fala com **hosts diferentes por caminho** (WhatsApp num host, Instagram
noutro), a suíte só prova a seleção de host se o teste usar **servidores diferentes** para cada um —
um teste com um host só prova forma (corpo, método, headers), nunca ROTEAMENTO. O conserto da T-104
(`internal/meta/instagram_test.go`, `internal/outbound/instagram_test.go`) passou a usar dois
`httptest.Server` por teste — um que NUNCA deveria ser chamado, outro que DEVE — e provou, por
mutação manual (religar `c.base` no lugar do parâmetro), que os testes antigos continuavam verdes
enquanto os novos ficavam vermelhos.

---

### O `Verify` de uma tarefa disse `PASS` sem rodar nenhum dos testes que ele existia para provar — `-run` casa por REGEXP, não por assunto (2026-08-28)

A T-173 (rota `DELETE /v1/templates`) foi escrita com este `Verify`, para provar que a rota nova
entrou na tabela de isolamento:

```
go test ./internal/outbound/ -run 'Isolamento' -v   # e a rota tem de aparecer na saída
```

Ele **passa**, e não prova nada. `-run 'Isolamento'` casa só
`TestIsolationTableCOVERSEveryRouteRegisteredInThePackage`; os três testes que de fato exercem a tabela
chamam-se `Test*RotaDeSaida*` / `Test*RotasDeSaida*` e **não rodam**. Medido depois:
`-run 'Isolamento'` → **0** ocorrências da rota na saída; `-run 'Isolamento|RotaDeSaida|RotasDeSaida'`
→ 6.

*Custo real: zero, e por um motivo que não se repete sozinho — **o implementador não obedeceu ao
`Verify`, foi conferir se ele media alguma coisa, e relatou**. O comando saiu `PASS` com a rota
ausente da saída, que é a forma exata do "monitor cego que responde OK".*

**O que o autor do `Verify` fez de errado, e é sutil:** a metade *"e a rota tem de aparecer na
saída"* estava certa e era a prova real. O `-run` foi escrito **pelo assunto** ("os testes de
isolamento"), supondo que o nome do teste contivesse a palavra do assunto. **Nome de teste não é
tema, é identificador** — e a regexp casa o identificador.

**A regra, e ela é barata:** quem escreve um `Verify` que filtra por nome **roda o filtro antes de
escrever a tarefa** e confere que a saída contém o que a prova exige. Um `grep -c` no que deveria
aparecer resolve em segundos. *Mesma família do "selecionar alvo por PREFIXO DE NOME" na seção de
infra: prefixo e regexp escolhem por acidente de nomenclatura, e o acerto de hoje é coincidência de
como alguém batizou as coisas.*

## Ambiente

🔴 **`go test ./... | grep …` dentro de um `&&` ESCONDE a suíte vermelha, e foi assim que um vermelho
chegou ao `main`.** O status de saída de um pipeline é o do **último** comando: o `grep` sai `0`
porque *achou linhas*, o `&&` segue em frente, e o `echo "VERIFY OK"` imprime por cima de uma suíte
que falhou.
*Custo, em 2026-07-28: o planner comitou e empurrou para o `main` um merge com
`internal/outbound` vermelho, escrevendo "VERIFY OK" na própria saída. Produção não foi afetada só
porque nenhum deploy estava pendente — o `CLAUDE.md` proíbe exatamente isso ("se falhar, conserte ou
não commite"), e a proibição não bastou porque o comando **mentiu**.*
**A forma segura, e a razão de o filtro ser tentador:** a saída de teste deste projeto é enorme
(handlers logam de propósito), então filtrar é legítimo — só não pode ser na mesma expressão que
decide se deu certo.

🔴 **Um `cd` para o worktree de um agente PERSISTE entre chamadas de ferramenta, e a partir dali todo
`git` responde sobre o repositório ERRADO — com os sintomas exatos do desastre de contaminação.** Em
2026-07-29 o planner deu um `cd` num worktree de agente para ler uma linha de código. Três comandos
depois, ainda sem perceber, ele rodou `git log`/`git status`/`git branch --show-current` e leu:
`HEAD` no commit do agente, **os seus próprios commits ausentes do histórico**, e o branch do agente
em check-out. Concluiu que a árvore principal tinha sido contaminada e **avisou o dono com um alarme
vermelho**. Não havia contaminação nenhuma: `main` estava intacto, igual a `origin/main`, e os três
commits do planner eram ancestrais dele. **O diretório era outro.**
*Custo: um alarme falso do tipo mais caro — o que diz ao dono que o repositório dele quebrou. Minutos
de diagnóstico e uma correção pública.*
**Por que é armadilha e não descuido:** a regra do workspace manda conferir `git branch --show-current`
antes de comitar, e essa checagem **dispara igual** nos dois casos — ela distingue "branch errada" de
"branch certa", **não** "repositório errado" de "repositório certo". A pergunta que falta é *onde eu
estou*:

    git rev-parse --show-toplevel   # ANTES de acreditar em qualquer git log/status/branch
    git -C /c/dev/zapgw <comando>   # ou, melhor: nunca depender do diretório corrente

**E a regra de leitura que generaliza:** ao concluir que o estado do repositório está estranho,
**confirme primeiro que você está olhando o repositório que pensa**, antes de acreditar na conclusão.
Commit "desaparecido" é quase sempre pergunta feita no lugar errado — e é barato conferir: se
`origin/main` tem o SHA e `git merge-base --is-ancestor` confirma, nada foi perdido.

🔴 **Backup à mão em `/tmp` dentro de um repo git: o `cp` de restauração aceita LIXO DE OUTRA SESSÃO
sem um erro sequer.** Em 2026-07-29 um implementador quis guardar `cmd/zapgw/provision.go` antes de
uma mutação e escolheu `/tmp/provisionar.go.bak`. **Já existia um arquivo com esse nome**, de sessão
anterior e muito mais antigo. O `cp` de volta funcionou perfeitamente — exit `0`, nenhuma mensagem — e
**sobrescreveu o arquivo real por uma versão ~751 linhas mais curta**. O único sinal foi
`git diff --stat` mostrando `-751`.
*Custo: o conserto foi `git checkout --`, que também descartou o edit legítimo ainda não commitado —
então a mudança teve de ser reaplicada à mão. Nada quebrado chegou ao commit, e só porque ele olhou o
`--stat`.*
**A regra, e ela é mais forte que "escolha outro nome":** **em repo git, não role o seu próprio
backup.** O repositório já é o backup, atômico e por projeto — `git stash`, `git checkout --
<arquivo>`, ou um commit temporário fazem o trabalho e **não têm como trazer conteúdo de outra
sessão**. Se ainda assim precisar de arquivo solto, use o **diretório de scratchpad da sessão** (que
existe exatamente para isso), nunca `/tmp`: `/tmp` é compartilhado entre sessões e entre agentes, e
todo nome fixo lá é uma colisão esperando acontecer.
**O que faz esta armadilha valer uma entrada em vez de um "preste atenção":** o modo de falha é
*silencioso e invertido* — a ferramenta que você chamou **para proteger** o arquivo foi a que o
destruiu, e ela reportou sucesso. É a mesma família do `git push` que não empurra nada e do
`grep` num pipeline: **exit `0` não é prova de que a coisa certa aconteceu.**

```sh
go test ./... > /tmp/t.txt 2>&1; echo "EXIT=$?"   # o status vem antes do filtro
grep -E "^(FAIL|ok)" /tmp/t.txt                    # o filtro vem depois, e não decide nada
```

*A regra que generaliza, e vale para qualquer verify: **nunca canalize o comando cujo status você
está usando para decidir.** `set -o pipefail` resolve em `bash`, mas depender dele é apostar em qual
shell rodou; capturar o status explicitamente funciona em todos.*

**O Go instalado pelo `winget` NÃO entra no `PATH` das sessões de shell já abertas.** `go version`
devolve *command not found*, e quem estiver verificando conclui que "não dá para verificar" — e
aprova por leitura.
*Custo: uma revisão inteira aprovada sem rodar o verify. Só apareceu porque o PATH foi conferido à
mão depois. Solução: usar o caminho completo (`/c/Program Files/Go/bin/go.exe`).*

**Uma verificação que silenciosamente não acontece volta como "aprovado".** É o mesmo formato das
falhas de produto que este projeto persegue, aplicado ao processo de revisão.

**`* text=auto` no `.gitattributes` faz o checkout no Windows converter para CRLF, e o `gofmt` exige
LF.** Resultado: `gofmt -l .` — que é um passo do verify — passou a listar **os 22 arquivos `.go` do
repo, sempre. Um verify que acusa sempre é um verify que todo mundo aprende a ignorar**, e aí o dia
em que ele acusa de verdade ninguém olha. É o mesmo defeito que este projeto persegue no código,
aplicado à ferramenta.
*Custo: apareceu no `git checkout main` depois de um merge — durante o desenvolvimento os arquivos
nasceram escritos em LF e nunca tinham sido re-checkoutados, então o problema não existia. O índice
já estava em LF (`git ls-files --eol` → `i/lf w/crlf`): só o working tree convertia. Conserto:
`*.go text eol=lf` no `.gitattributes`.*

**E `*.go` não é o fim da lista: todo arquivo LIDO POR UM LINUX tem a mesma armadilha.** Com CRLF, o
`deploy.sh` morre em `$'\r': command not found` e a unit do systemd leva o `\r` junto no valor de
`ExecStart` — nenhum dos dois diz que o problema é fim de linha. *Custo: nenhum, e só por sorte de
cronologia — os arquivos da T-008 nasceram em LF e foram rodados antes de qualquer re-checkout,
exatamente como os `.go` acima. Fechado preventivamente com `*.sh text eol=lf` e
`*.service text eol=lf`. Ao acrescentar um tipo novo de arquivo de sistema (`.conf`, `.timer`,
`.env`), acrescente a linha junto.*

---

## Infra (não cobrou ainda — registrada antes)

### 🔥 O renovador de credencial não renovava nada — e respondia verde (2026-08-17)

`implanta/cf-renova-tokens.sh` nasceu com um nome que dizia **renova** e um filtro que selecionava só
tokens com `expires_on` **nulo**. Fazia sentido no dia em que foi escrito: ele existia para *pôr*
validade em quem não tinha. **No instante em que os três tokens ganharam prazo — que foi o próprio
trabalho dele — o filtro deixou de casar com qualquer coisa, e o script virou inerte.** O `--dry-run`
passou a imprimir a seção *"O QUE ESTE SCRIPT VAI ALTERAR"* **sem uma linha de alvo** e a sair `0`.

**O sintoma não existe, e é isso que torna esta cara:** o script é preventivo, para uma renovação em
**fevereiro de 2027**. Alguém rodaria sob pressão, veria tudo verde, e os tokens venceriam do mesmo
jeito — a queda chegaria depois, sem ninguém ligar uma coisa à outra.

**Por que passou:** o `Verify` da tarefa que o criou (T-123) checava **sintaxe e texto** —
`bash -n`, `grep` por frases no cabeçalho. Nenhuma checagem **exercitava comportamento**, e um script
que não tem alvo nenhum passa em todas elas. *Verify que só lê o arquivo não distingue "funciona" de
"não faz nada".*

**As três regras que ficam:**
1. **Ausência de alvo é FALHA, nunca verde.** Lista vazia agora é `exit 1` com o motivo. Um laço que
   não itera é o silêncio mais barato de produzir e o mais caro de descobrir.
2. **Um critério de seleção escrito no dia zero descreve o mundo do dia zero.** Se o script muda
   exatamente o campo pelo qual ele filtra, o filtro se auto-anula na primeira execução bem-sucedida.
   Pergunte sempre: *o que este script faz com o dado que ele usa para se decidir?*
3. **Script preventivo precisa de um exercício que possa falhar** — aqui, uma API falsa alimentando o
   laço e uma prova do controle positivo com token inventado. É a mesma pergunta da armadilha do
   certificado, logo abaixo: *o meu teste é capaz de falhar?*

*Custo real: nenhum incidente — o defeito viveu uma hora (script entregue às 06:54 de 2026-08-17,
achado e consertado às 08:00 do mesmo dia). O que se pagou foi uma tarefa inteira (T-123)
entregue como garantia e valendo zero, mais a T-124 para refazê-la. **O custo evitado era em fevereiro
de 2027, e teria sido a expiração das três credenciais de conta da Cloudflare** — zona, túnel e Worker
da sonda.*

⚠️ **No mesmo arquivo, dois defeitos irmãos, achados no mesmo dia e consertados junto:** o controle
positivo escrevia o **valor do token em claro** num `mktemp` e só apagava **depois** do `curl` — com
`set -euo pipefail`, um `curl` que falhasse (o caminho de falha mais provável do script) matava tudo
**entre as duas linhas** e deixava a credencial no `$TMPDIR` por tempo indeterminado. O conserto não
foi um `trap`: foi **eliminar o arquivo**, mandando a config pelo stdin (`curl -K -`). *Remendar a
limpeza mantém a janela; tirar o disco do caminho fecha a classe inteira.* E o cabeçalho prometia
**modo 0600** que o script não impunha — o modo vinha do `mktemp`, e no Windows quem manda é a ACL da
pasta temporária. **Promessa não cumprida sobre credencial é bug**: a frase foi apagada, não cumprida.

### 🔥 Selecionar alvo por PREFIXO DE NOME é adotar o que não é seu — e quase estendeu um token de 24 h para sete meses (2026-08-17)

Consertado o defeito acima, `implanta/cf-renova-tokens.sh` passou a selecionar alvo por
`startswith("zapgw")`. Parecia o oposto de um filtro estreito demais, e por isso ninguém olhou de novo.

**O `--dry-run` contra a conta de verdade mostrou o que a lógica dizia:**

    ALVO: zapgw-teste-tokenwrite-24h  expira=2026-08-17T23:59:59Z -> 2027-03-31T23:59:59Z

Aquele era um token de **teste, de 24 horas**, criado naquela manhã para provar que o próprio script
funcionava — e com `API Tokens Write`, ou seja, **a credencial mais poderosa da conta: ela cria
qualquer outro token**. A ferramenta que existe para *melhorar* higiene de credencial ia estender a
pior credencial possível de 24 horas para sete meses, em silêncio e com aparência de sucesso. A
execução real foi abortada por causa dessa linha, antes do `PUT`.

**A regra: nome é campo livre, e critério de seleção não pode ser um campo que qualquer um escreve.**
Quem cria um token escolhe o nome; um `zapgw-*` na conta não é prova de que ele é nosso, de que é
permanente, ou de que alguém quer que ele dure. O alvo passou a ser uma **lista explícita** dentro do
script (`CONHECIDOS`), que é a **mesma** lista dos arquivos de prova — de propósito: *"o que eu renovo"
e "o que eu sei provar depois" têm de ser o mesmo conjunto.*

**E o desconhecido não é ignorado: é RELATADO** (bloco `NAO SAO MEUS, e nao vou tocar`, com nome, id e
validade). As duas saídas fáceis eram erradas em direções opostas — **adotar** causa o dano de hoje,
**calar** faz um token temporário passar despercebido no futuro. Relatar é a única que não escolhe.

**Defeito irmão, da mesma raiz, consertado junto:** o mapa nome→arquivo de prova conhecia três nomes,
então um alvo adotado por prefixo não tinha arquivo, e o `provar()` matava o script — **depois** de já
ter feito `PUT` nos anteriores. *Uma ferramenta de credencial que descobre que não consegue validar
**depois** de mutar deixa aplicação parcial e um operador sem saber onde ela parou.* Agora **todos** os
arquivos de prova são resolvidos **antes do primeiro `PUT`**; faltando um, sai `≠ 0` sem ter alterado
nada.

🔴 **E a lição sobre o VERIFY, que é a que se repete neste arquivo:** este defeito atravessou **duas**
rodadas de verificação verdes — `bash -n`, greps por frase, e até um exercício de comportamento com
`cf()` esbulhado. **O exercício não tinha um intruso no JSON de mentira**, então não havia como ele
distinguir "seleciona certo" de "seleciona tudo". *Prova que não contém o caso perigoso não é prova.*
E ao provar que **nada** foi mutado, o "zero PUTs" só vale acompanhado de um controle em que o mesmo
contador **move** — senão é contador cego respondendo o que se quer ouvir.

*Custo real: nenhum incidente — pego pelo `--dry-run`, que é para isso mesmo. O que se pagou foi a
rodada de validação real inteira abortada e a T-125 para refazer o seletor. **O custo evitado seria
um token com `API Tokens Write` vivo por sete meses sem ninguém saber**, criado como descartável de
24 horas.*

### 🔥 "Medido em `<data>`" que o registro do próprio dia não sustenta (2026-08-20)

O cabeçalho de `implanta/cf-renova-tokens.sh` afirmava, na seção *A CHAVE GLOBAL NAO E' NECESSARIA*:
*"Medido em 2026-08-17, com `PUT` real seguido de conferência das permissões."* A frase **não tinha
medição atrás**. O registro do mesmo 17/08, duas armadilhas acima, conta que a execução real daquele
dia foi **abortada antes do `PUT`** pelo seletor por prefixo, e as duas rodadas usaram a **chave
global**.

**A conclusão era verdadeira; a procedência é que era falsa.** Isso é o que torna o defeito difícil:
não há sintoma. Nada quebra, ninguém tropeça, e a frase sobrevive a toda revisão porque *está certa*.

**E ela se espalhou, como afirmação boa costuma se espalhar:** do comentário do script para
o runbook de implantacao (que fica no repositorio privado), e de lá para `C:\dev\github\docs\CREDENCIAIS-DE-API.md` — que serve a
**todos** os projetos do workspace. Na terceira cópia ela tinha ganhado uma perna a mais: *"cria e
renova tokens"*. **Criar (`POST /user/tokens`) nunca foi medido por ninguém**, nem em 17/08 nem em
20/08. Uma afirmação sem procedência cresce quando é copiada, porque cada cópia herda a autoridade e
nenhuma herda a evidência.

**Medido de verdade em 2026-08-20**, e o roteiro está escrito nos três textos justamente para poder
ser repetido: `GET /user/tokens` → `GET /user/tokens/<id>` → `PUT` empurrando a validade → **releitura
independente** (o `PUT` responder `200` não prova) → comparação dos `permission_groups` antes × depois
→ restauração da data original. Alvo único: `zapgw_conf_tunnel`, o único `conf` sem consumidor
automatizado.

**A regra, que vale muito além de credencial:** *afirmação de medição carrega **o que foi
exercitado**, não só a data.* Se o leitor não consegue repetir a medição pelo que está escrito, não
escreva "medido" — escreva o que você de fato viu, ou não escreva. Está na doutrina do workspace
(`C:\dev\github\docs\DOCUMENTACAO.md`, *Regras de escrita*).

*Custo real: três dias com um "medido" inauditável, a medição refeita do zero em 20/08, e uma
afirmação falsa por procedência publicada no doc que serve a todos os projetos. Barato aqui porque a
conclusão calhou de estar certa — **e é exatamente isso que o torna perigoso**: o mesmo defeito num
limite de API, num número de capacidade ou numa janela de reenvio é decisão tomada em cima de nada.*

### 🔥 DOIS defeitos que teriam quebrado TODA renovação de certificado — achados só porque a emissão foi FORÇADA (2026-08-06)

Na migração da zona `tenant-one.com.br` para a Cloudflare, o Traefik reiniciou **ativo, sem um único
erro no log**, e todos os sites responderam. Parecia pronto. **Estava quebrado em dois lugares, e o
sintoma só chegaria em novembro**, quando o certificado curinga vencesse — três meses depois da causa.

**Os dois só apareceram porque a regra era "reiniciar não prova emissão".** A prova: um router
descartável para um hostname sem certificado, forçando uma emissão de verdade no mesmo dia.

**Defeito 1 — o curinga CNAME envenenava o desafio ACME.**
`*.tenant-one.com.br` era `CNAME → casa`. **Curinga de DNS casa em QUALQUER profundidade** — logo,
`_acme-challenge.<qualquer coisa>.tenant-one.com.br` também devolvia aquele CNAME. O lego seguia
para `casa` e escrevia o desafio no lugar errado.
🔴 *O erro de raciocínio que atrasou o diagnóstico: eu afirmei que "curinga cobre um nível". **Isso é
regra de certificado TLS, não de DNS.** No DNS, `*.exemplo.com` casa `a.b.c.exemplo.com`.*
**Conserto:** curinga vira registro **A** para o mesmo IP. Resolve idêntico e não é CNAME.

**Defeito 2 — o lego conferia a propagação no resolvedor da LAN.**
`NS <resolvedor-da-rede>:53 did not return the expected TXT record`. O resolvedor da máquina é interno e não
enxerga a zona pública (split-horizon), então o TXT **estava certo na Cloudflare** e a checagem dizia
que não. **Conserto:** `resolvers: ["1.1.1.1:53","8.8.8.8:53"]` no `dnsChallenge`.
🔴 *E a resposta já estava no MESMO arquivo: o resolvedor `cloudflare` tinha essas linhas; o
`letsencrypt` não. **Alguém já tinha batido nisto e consertado de um lado só** — e a assimetria entre
dois lugares que resolvem o mesmo problema É o defeito, como este documento já dizia.*

**Depois dos dois: emissão em 15 segundos** (`trying to solve` → `The server validated our request` →
`Server responded with a certificate`).

*Custo: nenhum incidente — a migração inteira foi feita com os dois defeitos ativos e ninguém teria
notado até novembro. **O que os pegou não foi olhar o log: foi exigir uma emissão que pudesse
falhar.***

⚠️ **E o primeiro teste não valia:** usei um hostname de UM nível, já coberto pelo certificado
curinga. O Traefik corretamente não emitiu nada, e eu quase li isso como falha. **Teste que não pode
falhar não prova nada** — foi preciso um nome de DOIS níveis para obrigar uma emissão real.

**As três perguntas que generalizam:**
1. *O meu teste é capaz de falhar?* Se o sistema puder atendê-lo com o que já tem, ele não testa nada.
2. *Existe um segundo lugar que resolve este mesmo problema?* Se existe e está diferente, a diferença
   é o defeito — não a coincidência.
3. *Qual é o intervalo entre a causa e o sintoma?* Quando ele é medido em meses, "não deu erro agora"
   não é informação nenhuma.


### 🔥 A escotilha de TLS que ficou ligada para sempre — medida em 2026-08-06

**O `CLAUDE.md` deste projeto proíbe qualquer caminho com opção de não verificar certificado, e diz
por quê: *"no dia em que a opção existir, alguém liga para destravar uma demo ou um teste, e ela fica
ligada para sempre — em silêncio, porque desligar a verificação não gera erro nenhum"*. Em
2026-08-06 essa frase foi encontrada acontecendo, nesta rede, havia meses.**

O túnel do `consumer-b` entregava em `https://127.0.0.1:443` com **`noTLSVerify: true` nas SETE
regras de ingress**. O motivo de ter sido ligado é óbvio e legítimo: `127.0.0.1` não bate com o nome
do certificado. O motivo de ter ficado é o da regra: **nada falha quando a verificação está
desligada.**

⚠️ **E havia um agravante que quase me fez consertar a coisa errada:** o dono autorizou mudar o modo
SSL da zona para `strict`. **Isso teria sido teatro** — nesses hostnames o tráfego vai
*borda → túnel → cloudflared → origem*, e quem manda na verificação da última perna é o **ingress do
túnel**, não a configuração da zona. `strict` na zona não tocaria no `noTLSVerify`.

**O conserto certo é `originServerName`**, que mantém o destino em `127.0.0.1` e valida o certificado
contra o nome real. *Provado ANTES de mexer, de dentro do o LXC do Traefik:*
`curl --resolve <host>:443:127.0.0.1` devolveu **`ssl_verify_result = 0` nos sete** — ou seja, o
Traefik já servia certificado válido e confiável, e a escotilha era **puro resíduo**.

*Custo: nenhum incidente — mas durante meses a garantia de TLS naquele salto foi **teatro**, e
ninguém tinha como saber. Aplicado nas sete regras, uma primeiro e as outras depois da prova externa;
os sete hostnames responderam igual antes e depois, e a zona foi para `strict` só DEPOIS de a
verificação ser real.*

**As duas perguntas que generalizam:**
1. *Onde termina o TLS no meu caminho, e QUEM decide se ele é verificado nessa perna?* — a resposta
   quase nunca é onde se olha primeiro.
2. *Existe alguma escotilha ligada aqui que nunca vai falhar sozinha?* — se existe, ninguém vai
   descobri-la por acidente. Só medindo de propósito.


**⚠️ Esta entrada foi ESTREITADA em 2026-07-28 (T-066): ela descrevia uma topologia que NÃO é a
deste gateway.** O texto afirmava, como fato sobre o zapgw, que o `cloudflared` entrega no Traefik
pelo loopback e por isso o Traefik vê todo request como `127.0.0.1`. **Não há `cloudflared` no
caminho público do `zapgw.tenant-one.com.br`** — o nome resolve para o IP da casa e chega por
encaminhamento de porta (medido; ver o runbook de implantacao (que fica no repositorio privado)).

O que continua valendo, e é por isso que a linha foi estreitada em vez de apagada: **onde há entrega
por túnel nesta rede**, o IP real vem **só** do `CF-Connecting-IP`, e confiar em `X-Forwarded-For`
por faixas de IP da Cloudflare — a receita usual de quem está atrás de proxy reverso — **está
errado**. *Custo: cobrou noutro projeto desta rede (quebrou anti-brute-force e log de origem de OTP).
Atinge qualquer allowlist de IP da Meta e qualquer limite por origem no gateway.*
**Qual IP o Traefik enxerga no caminho público do zapgw NÃO foi medido** (exige shell no o LXC do Traefik, nó
`<no-proxmox>`) — não assuma nenhum dos dois. Hoje nada no binário depende disso:
`grep -rn "RemoteAddr\|X-Forwarded-For\|CF-Connecting-IP" cmd internal` devolve zero linhas.
*A lição que esta entrada passou a carregar é sobre DOC, não sobre rede: **conclusão importada de
outro sistema desta mesma rede parece confirmada — ela tem ADR, tem custo real e tem nome de
mecanismo — e mesmo assim é chute sobre a topologia daqui até alguém medir.***

**Rota que o binário serve mas o proxy não roteia responde `404` com tudo certo do nosso lado — e
nenhum teste deste repo alcança isso.** O roteamento no Traefik é por `PathPrefix`, e ele mora no
campo Notes do LXC, **fora do repositório**: uma rota nova (`/v1/instances/…` na T-014, e as de
mídia e templates nas próximas) fica verde na suíte, verde no `curl` contra `127.0.0.1:8080`, e
`404` para o consumidor. O `404` é indistinguível de "essa rota não existe nesta versão", então o
consumidor conclui que o gateway está velho e ninguém procura no proxy.
*Custo: nenhum ainda — a lacuna foi registrada em o runbook de implantacao (que fica no repositorio privado) no mesmo commit que criou a
rota. **Ao acrescentar rota nova, a regra do router entra na mesma tarefa**, e a conferência é
`curl` pelo hostname e pela porta reais, nunca pelo `127.0.0.1` do LXC.*

**Certificado que não sai faz o webhook falhar CALADO dos dois lados.** `tenant-one.com.br` tem
wildcard no entrypoint (subdomínio novo não emite nada); `consumer-b.com.br` **não tem**, e exige
`certresolver=cloudflare`. Resolver errado no label = cert nunca sai, Traefik responde com o default,
e a Meta simplesmente não entrega — sem erro em lugar nenhum nosso.
*Validação de domínio novo é **conferir o emissor do certificado de fora da rede**, não "responde
200".*

**A sonda existir não é a mesma coisa que a sonda RODAR — e a diferença calou o alarme por uma queda
inteira.** `implanta/sonda-publica.sh` foi escrito na T-073 (2026-07-28) e ficou nove dias sem lugar
para ser executado a partir de fora da LAN. Em 2026-08-06 o link de IP fixo da casa caiu, o caminho
público ficou fora do ar por ~9 minutos, e os quatro monitores que existiam (journal, inscrição na
Meta, contadores, `/v1/health`) ficaram todos verdes durante a queda inteira — exatamente como
o runbook de implantacao (que fica no repositorio privado) já previa por escrito desde a T-073. **Quem avisou foi o consumidor.** *Custo:
nenhuma perda de mensagem provada (a Meta reenfileira por 36 h), mas nove dias em que o único
instrumento capaz de flagrar isso existia no repositório e não rodava em lugar nenhum.* Corrigido na
T-117 pôs o script no GitHub Actions — **e a casa não pegou**: dois disparos, os dois `cancelled`,
`runner` vazio, ~15 min cravados nos dois, com a cota medida e descartada como causa. A T-119
(2026-08-06) mudou a casa para um Cloudflare Worker com Cron Trigger (`sonda-worker/`). **A lição que
generaliza: um instrumento de detecção sem um lugar programado para executar é documentação, não
proteção — e a lacuna não aparece em teste nenhum, porque a suíte também roda de dentro.**
🔴 **E a segunda volta da mesma lição, dentro da própria T-119:** o Worker foi publicado com sucesso
na conta e **o Cron Trigger não entrou** (a conta não tinha subdomínio `workers.dev`; a API recusa com
`code: 10063`). *`wrangler deploy` disse `Uploaded` — a linha que falhou vinha depois.* **Worker
publicado sem gatilho é exatamente o mesmo defeito com outra fantasia: código no lugar certo e nada
executando.** A pendência está escrita em o runbook de implantacao (que fica no repositorio privado), seção *Onde ela roda*. *Confira o
gatilho, não o upload.*

**"Sem resposta" classificado como MONITOR CEGO faria a sonda mentir no dia exato para o qual ela foi
feita.** Ao portar a sonda para o Worker (T-119), a primeira versão seguia a leitura óbvia — *o
`fetch` lançou, logo eu estou cego*. Mas a queda de 2026-08-06 (link fora, `:443` recusando conexão)
produz **exatamente** um `fetch` que lança: o alarme tocaria dizendo *"isto não prova que o gateway
caiu"* com o gateway fora. **A fronteira certa já estava no `implanta/sonda-publica.sh` desde a
T-073** — lá, ausência de resposta é saída `1`, VERMELHO —, e ela é *"EU não consegui medir"*, não *"o
alvo não respondeu"*. O Worker separa as duas com um **controle independente**: alvo mudo + controle
respondendo = VERMELHO; os dois mudos = CEGO. *Custo: nenhum — pego na revisão do próprio porte, antes
de o cron existir. **A lição: ao portar um instrumento, porte a SEMÂNTICA, não o formato.** Duas
implementações do mesmo monitor que classificam o mesmo evento de formas diferentes são pior que uma,
porque quem lê a segunda acredita ter lido a primeira.*

**`cacheTtl: 0` NÃO desliga o cache da Cloudflare — ele manda cachear e expirar na hora.** A doc
(`developers.cloudflare.com/workers/runtime-apis/request/`, lida em 2026-08-06) é explícita: `cacheTtl`
*"forces Cloudflare to cache the response for this request, regardless of what headers are seen on the
response"*, e `0` significa *"the cache asset expires immediately"*. Quem desliga é
`cacheTtlByStatus` com valor **negativo** — *"Any negative value instructs Cloudflare not to cache at
all."* *Nunca cobrou aqui: a defesa que realmente segura o falso verde da sonda é **estrutural** — o
slug varia a cada execução, e URL que nunca existiu não está em cache nenhum. Registrada porque a
tentação é escrever só o `cacheTtl: 0` e achar que está coberto, e o sintoma seria a sonda ficar VERDE
sem tocar no gateway — depois da migração de DNS, quando o hostname passar a ser proxiado pela mesma
conta.*

---

## systemd

**`systemctl restart` devolve 0 com o serviço morrendo logo depois.** Com `Type=simple` o systemd só
promete que executou o `ExecStart`; ele não espera nada. No teste de reversão da T-008 o `restart`
saiu **0** enquanto o binário morria em 5 ms, e o systemd o reergueu 13 vezes em 30 s — tudo isso com
o deploy "bem-sucedido" do ponto de vista do `systemctl`. **É por isso que o veredito do deploy é o
`/v1/health`, e não o código de saída do `restart`.** *Custo: zero, porque a exigência do health
existia desde o primeiro deploy. Sem ela, um binário quebrado teria ficado em produção com o gateway
mudo e ninguém saberia — a Meta simplesmente para de entregar, e de fora é indistinguível de "não
chegou mensagem".*

**`Restart=always` + `StartLimitBurst` pode fazer o `restart` da REVERSÃO ser recusado.** Um binário
que morre na hora estoura o limite em segundos, e a partir daí o systemd responde *"start request
repeated too quickly"* — quem falha é justamente o caminho que existe para consertar. Por isso o
`deploy.sh` roda `systemctl reset-failed zapgw` antes de cada restart. *Custo: nenhum ainda — na
prova da T-008 o contador chegou a 13 sem estourar (`RestartSec=2s` contra a janela padrão de 10 s
faz o caso ficar na fronteira, e a fronteira depende do tempo de arranque). Uma guarda que só falha
"às vezes" é pior que uma que sempre falha: ela some do teste e aparece no incidente.*

**`command not found` dentro do CT não quer dizer que o binário não está lá — quer dizer que você
entrou por `pct`.** `pct enter` / `pct exec` dão `PATH=/sbin:/bin:/usr/sbin:/usr/bin`, **sem
`/usr/local/bin`**, que é onde o `deploy.sh` instala o `zapgw`. Só sessão de *login* (ssh, `su -`)
carrega o `/etc/profile` que completa o `PATH`. **E o env do systemd também não vem junto**: um
shell interativo não tem `ZAPGW_BANCO` nem `ZAPGW_CHAVE_CIFRA`, então o menu abre e imprime
`resumo indisponivel:` enquanto todo subcomando falha ao abrir o banco — dois sintomas diferentes da
mesma causa, e nenhum deles diz "carregue o env". *Medido em 2026-07-29, quando o dono digitou
`zapgw menu` no CT e levou `command not found` com a `v0.31.0` instalada e saudável. Custo: uma ida
ao chat. O que a torna cara se repetir é o disfarce — `command not found` manda procurar deploy que
falhou, e o deploy estava perfeito.*

✅ **Consertado no mesmo dia** por `o LXC do gateway:/etc/profile.d/zapgw.sh` (mais o `source` no
`/root/.bashrc`, porque `pct enter` não lê `profile.d`), que define `zapgw` como **função**. **A
lição que sobrevive ao conserto é a de desenho, não a do comando:** arrumar só o `PATH` teria
consertado a primeira falta e deixado a segunda — **trocar `command not found` por
`resumo indisponivel:` é trocar um erro claro por um obscuro**, e teria parecido conserto. *Duas
faltas com a mesma causa precisam do conserto que pega as duas, senão a segunda só aparece depois,
sem o contexto que a explicava.* O porquê de cada linha está em o runbook de implantacao (que fica no repositorio privado), *O que faz
`zapgw` funcionar venha você de onde vier*. ✅ A **T-090** versionou o arquivo em
`implanta/profile-zapgw.sh` e o `deploy.sh` passou a instalá-lo a cada implantação — CT novo, ou
reconstruído do zero, já nasce com o problema resolvido.

---

## SQLite

**`REFERENCES` no esquema é DECORATIVO sem `PRAGMA foreign_keys = ON`.** O SQLite aceita a cláusula
e não a aplica. A tabela de vínculo consumidor↔instância declarava duas chaves estrangeiras e
aceitava `CriarConsumidor("fantasma", token, []string{"slug-que-nao-existe"})` sem erro — um typo no
provisionamento viraria vínculo órfão autorizando uma instância que ninguém cadastrou.
*Custo: um Critical. É doc falsa em forma de schema — a declaração promete integridade que o banco
não entrega.*

**E o pragma vai na DSN, NUNCA num `db.Exec` depois de abrir.** O `database/sql` mantém um **pool**:
um `PRAGMA` executado por `Exec` vale só para a conexão que o atendeu. Provado por mutação —
revertendo para `db.Exec`, de **30 goroutines** concorrentes tentando o vínculo órfão, **só uma
passou**: as outras 29 pegaram, por acaso, a conexão que tinha rodado o `Exec`.
*Ele não falha — funciona ERRADO, dependendo de qual conexão o pool entregar. Teste sequencial
jamais pega; em produção aparece só sob carga, intermitente.*

**Sem `busy_timeout`, o SQLite devolve `SQLITE_BUSY` NA HORA em vez de esperar o lock.** Sob 60
requisições concorrentes na mesma chave de idempotência, a garantia central se sustentava (uma só
reservava) mas **58 voltavam com erro de banco** — um **quarto desfecho** que o contrato de três
casos não previa, aparecendo justamente no cenário que a idempotência existe para tratar: rajada de
retries simultâneos.
*Custo: um Important, achado antes de o handler HTTP ser escrito contra o contrato errado. Conserto:
`busy_timeout` e `journal_mode(WAL)` na DSN.*

**E `busy_timeout` NÃO cobre o caso em que a transação já LEU antes de tentar ESCREVER — só cobre
espera por trava.** `RemoveInstance` (`internal/config/store.go:1490`), `RegisterMeta`
(`internal/config/store.go:1072`) e `ClearTransitByPhone` (`internal/config/transit.go:360`)
abrem uma transação `deferred`, LEEM (`SELECT`/`SELECT ... GROUP BY`) e só depois ESCREVEM na mesma
transação. Se outra conexão comitar uma escrita real no meio desse intervalo, o instantâneo (snapshot)
de leitura que a transação já tem fica velho, e a tentativa de virar escritora aborta — e isso
acontece na hora, não depois de esperar os 5000 ms configurados: reproduzido de forma determinística
(uma conexão pinada com `db.Conn`, leitura, escrita comitada por OUTRA conexão, e então a escrita da
primeira) o erro chega em **0 s** com `Code()=517` (`SQLITE_BUSY_SNAPSHOT`), mensagem literal
`database is locked (517)`. Sob concorrência realista (12 goroutines chamando `RegisterMeta` ao mesmo
tempo) o mesmo aborto aparece também como `Code()=5` (`SQLITE_BUSY` simples, sem sub-código) em
11–23 ms — a mesma família de erro (a transação já tinha lido e perdeu a corrida para virar
escritora), só que o SQLite nem sempre chega a marcar o sub-código de snapshot. Nos dois casos o
`busy_timeout` foi ignorado: SQLite documenta que este caminho **não invoca o busy-handler**, porque
reesperar não resolveria — quem perdeu teria de reiniciar a transação inteira, e isso o driver não faz
sozinho. *Consequência medida: barulhento, não silencioso — a chamada volta com "database is locked"
em vez de ficar pendurada ou de corromper algo.*
*Custo: nenhum em produção ainda — achado como efeito colateral da T-131 (2026-08-18) e confirmado por
diagnóstico dedicado na T-132, não por incidente. **A hipótese mais grave que motivou a T-132 — que
esse erro "envenenaria" a conexão do pool, fazendo toda chamada seguinte falhar para sempre — foi
TESTADA e NÃO reproduzida:** nem numa conexão pinada isolada (`db.Conn`) reusada diretamente depois do
erro (8 leituras + 1 escrita nova, todas com sucesso), nem no pool real do `*Store` sob rajada pesada
(até 10 de 12 chamadas simultâneas falhando) seguida de 40 chamadas sequenciais no mesmo `*Store` — as
40 tiveram sucesso, em cinco repetições da rajada. O driver `modernc.org/sqlite` não marca a conexão
como inválida nesse caminho (`ResetSession`/`IsValid` só checam `sqlite3_is_interrupted`, não o estado
de transação), e o `Rollback()` da transação que abortou fecha a transação SQLite de verdade (conferido
sem erro em todas as repetições). **Nenhum conserto foi feito nesta tarefa** — retry, `BEGIN IMMEDIATE`
ou mudança de pool são decisão de projeto, e ficam para uma tarefa própria se o dono decidir que o
"barulhento" acima vale a pena resolver.*

🚫 **DECISÃO (2026-08-18): avaliado e RECUSADO — não haverá retry. Isto não é esquecimento.** A
T-133 existiu para decidir exatamente isto, e o desfecho foi *não fazer*. Fica escrito aqui porque
uma tarefa que some da fila sem rastro é indistinguível de uma tarefa perdida — e daqui a três meses
alguém reabriria o assunto do zero. Os motivos, na ordem em que pesam:

1. **Onde há instrumento, nunca aconteceu.** O journal do CT 125 cobre **24 dias** (desde 25/07) com
   **zero** `database is locked` — e zero `erro de store` de qualquer tipo. `RegisterMeta` é a única
   das três que corre dentro do daemon e loga erro de store
   (`internal/outbound/registration_handler.go:334`), então **para ela o zero é prova**.
2. ⚠️ **Para as outras duas o zero NÃO é prova — e isso fica escrito de propósito.**
   `RemoveInstance` e `ClearTransitByPhone` correm no processo do **CLI**, que morre em
   `log.Fatalf` contra o terminal de quem digitou (`cmd/zapgw/main.go:241`). O erro **nunca chega ao
   journal**. Ali zero é *ausência de instrumento*, não evidência — é a armadilha do monitor cego,
   com outra roupa. O que fecha a lacuna é a resposta do dono, perguntado diretamente em 2026-08-18:
   **nunca viu** `database is locked` rodando `zapgw instancia remover` nem `zapgw log clear
   --telefone`. *Isso é testemunho, não medição, e está anotado como testemunho.*
3. **A falha é barulhenta, e o preço dela é uma repetição manual.** O chamador recebe erro; nada fica
   pela metade (o `Rollback` foi conferido). Nos comandos de CLI há uma pessoa na frente, que é
   justamente quem consegue rodar de novo. Já `POST /v1/cadastro` responde `503 retentável` — o
   consumidor **já** repete por contrato.
4. **A hipótese que justificaria fazer mesmo sem ocorrência foi derrubada.** Era o envenenamento de
   conexão do parágrafo acima: se um aborto travasse o pool para sempre, "raro" não bastaria como
   defesa. Foi testado e não se reproduziu.
5. **Retry tem custo próprio, e ele não é zero.** Um helper que repete transação inteira é um padrão
   que o próximo leitor copia — e ele só é seguro *aqui*, porque estas três abortam sem efeito
   parcial. Copiado para um sítio com efeito parcial, duplica o efeito. Introduzir o padrão para um
   problema que não se manifesta é criar a armadilha antes de ter o benefício.

**O gatilho que reabre, e ele é objetivo:** a primeira ocorrência de `database is locked` observada —
no journal do daemon, ou relatada por quem rodou um comando de CLI. A tarefa então não precisa ser
reescrita: o `Do` e o `Verify` da T-133 já estavam completos (helper casando por `Code()` 5 ou 517,
**nunca** por texto; teto pequeno de tentativas; e o teste de controle exigindo que
`ErrInstanceActive` volte na **primeira** tentativa). Estão no histórico do git, no commit que a
aposentou.

🚫 **DECISÃO IRMÃ, no mesmo dia: o pool do `database/sql` fica SEM limite explícito — por escolha
agora, não mais por omissão.** `OpenStore` não chama `SetMaxOpenConns` nem `SetMaxIdleConns`
(`internal/config/store.go`, ver o bloco de pragmas). Para SQLite, mais conexões concorrentes não dão
mais vazão de escrita — dão mais contenção, que é a causa dos abortos acima. Mesmo assim **não
limitamos**, por dois motivos:

- **A medição não pede.** `lsof` mostrou o daemon com duas conexões no banco em produção, e o default
  do `database/sql` para `MaxIdleConns` já é **2** — ou seja, em repouso o pool já se comporta como
  limitado. O que "ilimitado" descreve é só o pico sob rajada, e a rajada real medida é pequena
  (a Meta reenvia 5 vezes em 9 s).
- 🔴 **E o conserto trocaria um erro BARULHENTO por uma espera SILENCIOSA.** Com `SetMaxOpenConns`
  baixo, quem não acha conexão livre **bloqueia** esperando uma — sem prazo, se o chamador não trouxer
  `context` com deadline. Isto é exatamente a direção contrária à que esta casa escolhe: o modo de
  falha que já custou caro aqui é o silêncio, não o erro na cara.

**O gatilho que reabre esta também:** (a) `database is locked` observado, como acima; ou (b) **o porte
para Postgres**, onde a conta é outra e inverte — lá o servidor tem `max_connections` global, pool sem
teto por processo é um risco real, e definir o teto passa a ser obrigatório e não opcional.

**O WAL cria arquivos vizinhos que o `.gitignore` não pega.** `*.db` não cobre `zapgw.db-wal` nem
`zapgw.db-shm`.

**Coluna nova dentro de `CREATE TABLE IF NOT EXISTS` NÃO chega a banco que já existe — e o `IF NOT
EXISTS` esconde isso.** A coluna `hash_pedido` foi acrescentada ao `CREATE TABLE` da idempotência.
Num banco cuja tabela já existia, a abertura do store passa limpa (a tabela existe, então nada roda) e
**todo envio** passa a devolver `503` com `table idempotencia has no column named hash_pedido`. Falha
total, silenciosa na subida, e visível só no primeiro envio.
*Custo: nenhum em produção — nenhum banco de v0.2.0 tinha a tabela `idempotencia`, então o `CREATE
TABLE` a criou já correta. Consertado na `T-001` com migração versionada por `PRAGMA user_version`
(`internal/config/store.go`, `migrar`), e o teste que a exige falhou **por asserção**, não por
compilação: a coluna realmente não chegava ao banco que já existia.*

**E o próprio mecanismo de migração traz duas armadilhas, as duas provadas por mutação:**

- **Migração que roda `ALTER TABLE` às cegas quebra exatamente nos bancos que ela existe para
  salvar.** Todo banco de v0.3.0 está em `user_version = 0` **e já tem** a coluna (ela nasceu dentro
  do `CREATE TABLE`). Sem consultar `pragma_table_info` antes, a primeira subida com migração morre
  com `duplicate column name` — e morre só nos bancos reais, nunca num banco de teste criado do zero.
- **Migração fora de transação deixa MEIO esquema, que é pior que nenhum.** Trocando o
  `BEGIN IMMEDIATE` por um no-op, a tabela criada pelo passo 1 sobrevive à falha do passo 2 e o banco
  fica num estado que versão nenhuma conhece — a subida passa e o erro aparece na primeira escrita.
  E o `BEGIN`/`COMMIT` tem de correr numa **única** `sql.Conn`: pelo pool do `database/sql` o `BEGIN`
  cai numa conexão e o `COMMIT` pode cair noutra. É a mesma armadilha do `PRAGMA` acima, com outra
  roupa.
- **Banco mais novo que o binário tem de RECUSAR subir** (`ErrSchemaFromTheFuture`). Um binário velho não
  conhece o que o novo criou: ele subiria sem erro nenhum e escreveria por cima com as regras
  antigas. É o formato preferido de estrago deste projeto — nada acusa na hora.

**Coluna CIFRADA nova derruba toda linha que já existia, porque o `DEFAULT` de um `ALTER TABLE` é a
string vazia — e string vazia não é um cifrado válido.** A migração 3 acrescentou `instancia.bundle_ca`
ao conjunto cifrado. Nas linhas já gravadas o valor vira `''`, e `Decifrar("")` falha em
`cifrado curto demais` — então `FindInstance` passaria a devolver erro para **toda** instância
anterior à migração. Como toda requisição começa buscando a instância, o webhook e o envio morreriam
juntos, na primeira chamada depois da atualização, com o `OpenStore` tendo passado limpo.
*Custo: nenhum — pego por teste (banco de v0.4.0 com uma instância inserida à mão, reaberto depois da
migração) e confirmado por mutação: decifrando às cegas, o teste acusa
`config: decifrar bundle de CA: config: cifrado curto demais`. Conserto: `decryptOptional`, que trata
coluna literalmente vazia como "nunca cadastrado" — a distinção é sem ambiguidade porque
`CreateInstance` grava o **cifrado de `""`**, que não é `""`. **A pergunta que economiza a próxima:
"o `DEFAULT` desta coluna é um valor válido para quem vai LER a coluna?"** — para coluna em claro é
sempre sim, para coluna cifrada é sempre não.*

**E o contrário também vale: `coluna != ''` NÃO é "cadastrado" numa coluna cifrada — o cifrado de
`""` não é `""`.** A T-020 nasceu com a instrução, escrita na própria tarefa, de que presença seria
`coluna != ''`. Mas `CreateInstance` cifra os **seis** campos, inclusive os que vieram vazios: a
`callback_url` de uma instância só de saída é um cifrado perfeitamente válido de string vazia. Com a
regra literal, a `tenant-one` — a única instância em produção — apareceria com `callback_url`
**cadastrada**, ou seja, entregando para um consumidor que não existe, e alguém iria procurar o
defeito na entrega em vez de na tela. E as **duas** formas de vazio convivem no mesmo banco: essa, e
a coluna literalmente `''` que o `ALTER TABLE` da migração 3 deixou em `bundle_ca`.
*Custo: nenhum — pego por mutação antes do commit (trocando a checagem por `!= ""`, cinco testes
ficam vermelhos). Conserto: perguntar ao **tamanho** do cifrado (base64 de `nonce+overhead` é
exatamente o cifrado de vazio), que responde sem decifrar e sem depender da chave. **A pergunta que
economiza a próxima: "o valor 'vazio' desta coluna é escrito de quantos jeitos diferentes?"** — e ela
é irmã da entrada acima, que pergunta se o DEFAULT é válido para quem vai LER.*

**E o ramo de compatibilidade que "trata o registro antigo" era código morto com comentário falso.**
O código tratava hash gravado vazio como igual, "para não quebrar registro anterior à coluna" — mas,
na época, sem migração, esse registro **não podia existir**, e o handler nunca passava hash vazio. O
comentário afirmava um mecanismo inexistente, e um teste verde afirmava cobrir o cenário: ele
exercitava uma chamada que nenhum caminho de produção faz.
*Compatibilidade que se escreve "por precaução", sem o caminho que a produz, é doc falsa dentro do
fonte — e ainda ganha um teste que dá a ela aparência de provada.*

**A terceira forma da mesma família, e a mais fácil de não ver: o `DEFAULT` do `ALTER TABLE` é um
valor VÁLIDO para quem lê, e mesmo assim é a resposta errada.** As duas entradas acima perguntam se o
default *quebra* a leitura (coluna cifrada) ou se ele *se confunde* com um valor legítimo (as duas
formas de vazio). A T-070 acrescentou `instancia.carimbos_desde` — o instante a partir do qual aquela
instância grava carimbo de contador —, e ali o `DEFAULT ''` não quebra nada: ele desserializa, viaja
no JSON e chega ao painel do consumidor como **campo vazio onde ele espera um instante**. Nada acusa
em lugar nenhum, e o campo nasce inútil justamente nas instâncias que ele existe para explicar: as
que já existiam antes de o carimbo existir. *Conserto: a migração faz o `ALTER TABLE` **e** um
`UPDATE ... WHERE carimbos_desde = ''` com o instante em que ela roda — e o `WHERE` é o que a deixa
segura de rodar de novo. Provado por mutação (só o `ALTER`, sem o `UPDATE`):
`TestTheMigrationFillsStampsSinceOnAPreExistingInstance` (`internal/config/store_test.go`) fica
vermelho com o campo vazio.* **A pergunta que fecha as três: além de "o `DEFAULT` é legível?" e "ele
se confunde com valor real?", pergunte "ele é uma RESPOSTA?"** — coluna que existe para responder uma
pergunta precisa de migração de DADO, não só de esquema.

*E o valor escolhido para as linhas antigas é decisão, não detalhe: elas recebem **o instante em que
a migração roda**, que pode ser mais tarde que a verdade (a instância talvez já carimbasse há dias).
Errar para mais tarde é o lado seguro — quem lê passa a tratar como "não sei" uma faixa em que talvez
houvesse carimbo, e nunca o contrário. O erro oposto (afirmar cobertura que não houve) é exatamente o
defeito que o campo existe para fechar.*

**E a metade que o teste quase não prova: um campo "por instância" com UMA instância no teste é
indistinguível de uma constante global.** `carimbos_desde` sai em RFC3339 **sem fração de segundo**,
então duas instâncias criadas com `time.Now()` no mesmo teste recebem o **mesmo texto** — e o teste
passa verde sobre uma implementação que devolvesse uma constante compilada para todo mundo, que é
literalmente o defeito. *Conserto: `CriarInstanciaEm(i, agora)` existe para o instante entrar por
parâmetro, e `TestStampsSinceIsPerInstanceNotAConstant` cria duas com 72 h de distância.
Mutação: gravar uma constante no lugar de `carimboDe(agora)` deixa o teste vermelho dizendo "as duas
instâncias respondem X — isto é uma constante global".* **A pergunta que generaliza, e ela é a mesma
que a T-072 pagou no mesmo dia (seção *Relógio e carimbo*): teste que depende de dois valores serem
DIFERENTES tem de forçar a diferença na granularidade em que o dado é GRAVADO.**

**A `T-001` inverteu esse fato, e é por isso que o conserto de um lugar obriga a reler o outro:** a
migração acrescenta `hash_pedido` com `DEFAULT` vazio às linhas **já reservadas**, então o registro
com hash vazio **passou a existir de verdade**. A comparação continua crua de propósito
(`internal/config/store.go`, `ReserveIdempotency`): um retry dessas linhas, feito depois da
atualização e dentro do TTL de 72h, recebe um `422` falso. A alternativa seria deixar **qualquer**
pedido diferente escapar da guarda sempre que o hash gravado fosse vazio — o desfecho **silencioso**
que ela existe para impedir, e que não expira em 72h.

---

## Validação

**Presença não é conteúdo.** Este plano teve **quatro** ocorrências do mesmo defeito, em arquivos
diferentes, todas na forma `!= ""` ou `len() == 0`:

| Onde | O que passava | Consequência |
|---|---|---|
| id devolvido pela Meta | `"   "` | `wa_message_id` inútil gravado num registro que **parece** enviado |
| `responder_a` | `"   "` | `context` com `message_id` em branco — citação para um id que não existe |
| botão | `{ID: "", Titulo: "Sim"}` | `reply` quebrado que a Meta rejeitaria, descoberto em produção |
| os campos irmãos de `Validar` | `para`/`texto`/`template`/`idioma`/`botao_*` só com espaço | pedido em branco aceito e mandado à Meta |

**Apare antes de decidir, e devolva o valor aparado** — senão os espaços viajam para a Meta.
*Custo: um Important e três lacunas, todas achadas por revisão adversarial, nenhuma por leitura.*

**Campo NOVO no `Pedido` sem `omitempty` muda o hash de TODO pedido antigo — inclusive dos que não
usam o campo.** `RequestHash` serializa a struct inteira, e o hash está **gravado no banco de
idempotência com TTL de 72h**. Um campo sem `omitempty` acrescenta `"botoes_url":null` ao JSON de
qualquer pedido, então todo retry legítimo de um pedido já reservado passa a bater contra um hash
diferente e recebe um **`422` falso** — o desfecho que a seção acima chama de "pior que não ter a
guarda", agora disparado por uma tag de struct. Nada acusa na hora: a suíte fica verde, o build
passa, e o estrago só aparece nos retries dos pedidos que já estavam no banco quando a versão subiu.
*Custo: nenhum — a T-021 nasceu com os três campos novos em `omitempty` e com um golden do hash
capturado da implementação anterior. Provado por mutação: tirando o `omitempty` de `botoes_url`, o
hash de um template que **só usa `variaveis`** muda de `c27bcc65…` para `d14dd34d…` e o teste fica
vermelho. **A pergunta que economiza a próxima: "este campo entra no `RequestHash` de quem não o
usa?"** — e a resposta só é "não" enquanto o `omitempty` estiver lá.*

**Valide com a MESMA função que envia, senão a validação e o envio discordam.** `Validar` chamava
`meta.Canonizar(p.Para)` para **decidir** se o número tinha dígito e **jogava o resultado fora**. O
envio canonizava de novo, então `"+55 (32) 99999-0000"` e `"5532999990000"` saíam idênticos no fio —
e produziam **hashes de pedido diferentes**, porque o hash é sobre o pedido validado. Um retry
legítimo cuja formatação do telefone variou receberia um `422` falso, que é justamente o desfecho que
a entrada acima chama de "pior que não ter a guarda".
*Custo: pego na re-revisão, antes do merge. Conserto: **atribuir**, não só comparar
(`p.Para = meta.Canonizar(p.Para)`). Conferir com a função que envia fecha a assimetria por
construção; conferir com uma regra própria ("tem que ter N dígitos") a reabre no dia em que uma das
duas mudar.*

**Uma guarda tem DOIS lados, e consertar só o que doeu abre o outro.** A recusa de base64 foi
consertada **três vezes**:

1. `HasPrefix(texto, "data:")` — recusava `"data: 23/07"`, data por extenso em português;
2. `+ Contains(";base64,")` — fechou o falso positivo e **abriu o falso negativo**: sensível a caixa
   e exigindo posição zero, deixava passar `DATA:...;base64,` e `"veja: data:...;base64,"`;
3. busca insensível a caixa, em qualquer posição, exigindo `;base64,` **depois** do `data:`.

*Custo: um Critical na terceira rodada. **Guarda mais estreita deixa passar o envio que falha calado;
mais ampla quebra conversa legítima — e as duas falhas caem no cliente final**, não em quem escreveu
a guarda. Escreva os testes em PAR: um exigindo recusa, outro exigindo aceitação.*

🔥 **Quando o erro que o gateway REPASSAVA não nomeava o campo, o teto tinha de ser conferido na
ENTRADA — o diagnóstico que chegava até nós não servia.** `botao_titulo` do `cta_url` (o `display_text`
do botão na Cloud API) não tinha teto nenhum em `Validar()` — só a guarda de vazio já existente. Em
18/08/2026 o consumidor `consumer-b` mandou um `botao_titulo` com mais de 20 caracteres e a Meta
respondeu `(#131009) Parameter value is not valid`, e o que chegou ao consumidor foi **só isso — sem
dizer qual dos parâmetros do pedido era o culpado.**
A mensagem saiu **sem o botão** para o cliente final, e o diagnóstico só fechou por **bissecção manual
num número de teste** (17 passou, 21 falhou, depois 19 e 20 confirmaram o teto exato). A T-139 moveu
o teto para a entrada (`limiteBotaoTituloCTAURL`, `internal/outbound/message.go`) — mas o número não
é documentação: a referência oficial da Cloud API é **omissa** sobre esse limite, e o comentário no
código diz isso de propósito em vez de fingir certeza sobre um valor que só um aparelho de terceiro
mediu.

🔴 **Correção (T-141, 2026-08-20): a formulação acima, escrita nesta mesma entrada, tratava "sem dizer
qual campo" como fato sobre a Meta — e isso nunca foi conferido.** O que era verdade é mais estreito: o
gateway (`ClassifyResponse`, `internal/meta/errors.go`) só lia `error.message` e `error.code` do
corpo de erro da Graph API, de propósito (o resto do corpo, `error_data`, pode ecoar telefone e texto
de mensagem do pedido enviado). O campo que faltava era da NOSSA leitura, não necessariamente da
resposta da Meta. Há evidência de que ela nomeia o campo e o teto em `error_data.details`: o mesmo
código `131009`, achado pelo consumidor no próprio banco em **18/07/2026**, por OUTRO transporte
(quando eles ainda falavam com a Meta via Evolution), veio como `"(#131009) Parameter value is not
valid — Button title length invalid. Min length: 1, Max length: 20"` — campo nomeado, teto declarado.
A T-141 passou a ler `error_data.details` (só essa chave, truncada em 500 runas) e repassá-lo em
`detalhe_meta`, campo separado no corpo de erro. **O que continua em aberto:** se a Meta ainda manda
esse campo pelo caminho de hoje (a chamada direta da Cloud API que o gateway faz), ou se aquele texto
de julho era peculiaridade do transporte antigo. Isso só fecha com medição contra a produção real,
depois do deploy — a suíte de testes da T-141 prova o repasse contra corpo sintético, nada além disso.
*Custo real: uma mensagem de cliente entregue sem o botão de call-to-action (18/08/2026), uma rodada
de bissecção manual num aparelho para descobrir um teto que o erro que chegava não nomeava, e — só
descoberto depois — um diagnóstico que já estava a um SELECT de distância desde julho.*

**O mesmo `(#131009)` anônimo vale para o vizinho.** Em 2026-08-20, medição nossa (não mais de
terceiro) contra a produção real da Meta, na instância `tenant-one`, com mensagens enviadas de
verdade: `titulo` de `botoes[]` (o `reply.title` do botão de resposta rápida) tem o **mesmo teto,
20**, e a contagem também é por **RUNA**, não byte — 20 caracteres acentuados (`ç`, 40 bytes)
passaram, 21 caracteres simples (21 bytes) foram recusados com o mesmo erro anônimo. A T-140 moveu
esse teto para a entrada (`quickReplyButtonTitleLimit`, `internal/outbound/message.go`, em
constante própria — os dois campos são endpoints diferentes da Cloud API e a Meta pode mudar um sem
o outro). O consumidor `consumer-b` tem 7 rótulos aprovados no catálogo entre 21 e 25 caracteres
que hoje falhariam em texto livre sem essa guarda.

---

## Contrato — obrigações que o consumidor cumpre errado por ler certo

**Guarda escrita sobre a FORMA do dado quebra quando a forma muda; guarda escrita sobre o RESULTADO
DO TRABALHO não quebra — e dois consumidores independentes chegaram nisso sozinhos, o que é o que
torna a regra confiável.**

Em 2026-07-28 o gateway normalizou `eventos` de `null` para `[]` (T-067). Antes de subir, o
implementador foi ler o canal do consumidor e viu `isinstance(eventos, list)`: com `null` isso é
**falso** e o lote caía no ramo que **grava o `cru`**; com `[]` viraria **verdadeiro** e entraria no
`for`, que roda zero vezes. Se aquele ramo não gravasse, o webhook de conta deixaria de deixar rastro
— **silêncio, não erro**, e funcionando hoje *por causa* do defeito que estávamos consertando.

Perguntamos aos dois consumidores e seguramos o deploy. As duas respostas vieram citando linha, não
memória, e **as duas descreviam a mesma solução com nomes diferentes**:

| consumidor | a guarda | a pergunta que ela faz |
|---|---|---|
| `consumer-b` | `if not itens:` | *"sobrou algum evento que eu consiga identificar?"* |
| `consumer-a` | `if not algum_evento_valido:` | *"eu consegui endereçar alguma coisa?"* |

Nenhuma das duas lê o **tipo** de `eventos` para decidir ramo. As duas decidem sobre o que o laço
**produziu**. E as duas cobrem um caso que a nossa própria sugestão inicial (`if not eventos:`) **não
cobria**: lote com eventos, todos com `id` vazio ou grande demais — lista não-vazia, zero endereçável,
guarda dispara do mesmo jeito.

*Custo: zero, e é o ponto. O deploy ficou 40 minutos parado e a resposta foi "está tudo bem". **A
pergunta valeu com a resposta sendo essa** — sem ela ninguém saberia se estava certo por desenho ou
por sorte, e a diferença entre as duas só aparece na próxima mudança de formato.*

**A hierarquia, do pior para o melhor**, porque as três aparecem neste projeto:

1. **por TIPO** (`isinstance(x, list)`, `x is None`) — amarra o código à representação, que é
   exatamente o que muda quando o produtor evolui;
2. **por CONTEÚDO** (`if not x:`) — melhor, cobre `None`/`[]`/ausente de uma vez, mas ainda pergunta
   sobre o **recipiente**;
3. **por RESULTADO DO TRABALHO** (`if not itens:`) — pergunta o que o código realmente precisa saber:
   *"consegui fazer alguma coisa útil com isto?"*. Sobrevive a formato novo, a item malformado no meio
   e a campo que a Meta invente amanhã.

**Sobre a convergência valer como prova — ela vale MENOS do que este arquivo chegou a afirmar, e quem
derrubou foi um dos dois convergentes.** A versão original desta entrada dizia que *"duas
implementações independentes chegando à mesma forma é a forma certa, e vale como evidência do jeito
que revisão interna nunca vale"*. O `consumer-b` respondeu, no mesmo dia, com a ressalva que faltava:
**os dois consumidores são Python e leram o MESMO contrato** — nosso. **Causa comum não é
independência**, e a chance de convergirem por terem lido o mesmo texto não é pequena. A frase
original superestimava a própria evidência, o que é a versão elegante de doc errado.

***O que carrega a prova de verdade é o TESTE, não a coincidência:*** os dois casos que
`if not eventos:` deixaria passar (`[{"foo":1}]` e `["texto", 3]`) estão cobertos dos dois lados, e o
resultado é linha gravada com o `cru`. **A pergunta que fica, e ela é reutilizável: antes de tratar
convergência como evidência, pergunte o que as duas partes têm em comum a montante** — mesma
linguagem, mesma doc, mesmo autor de contrato. O que sobra depois de descontar a causa comum é a
evidência real, e em geral é bem menos do que parecia.

*Crédito dos dois consumidores; a formulação "conteúdo ÚTIL, não conteúdo qualquer" é do
`consumer-b`, o contraexemplo do `id` grande demais é do `consumer-a`, e a ressalva sobre a causa
comum é do `consumer-b` derrubando a nossa própria conclusão.*

---

**O MESMO nome para coisas diferentes é pior que nomes diferentes para a mesma coisa.** Este projeto
tinha a segunda regra escrita e vigiada (*"enviar e receber com nomes diferentes para a mesma coisa é
o começo de dois vocabulários"*, T-024) — e por isso não viu a primeira chegando.
*A T-044 ia criar `botoes` para parâmetro de botão de template, forma `{indice, tipo, payload}`.
`Botoes []Botao` com tag `json:"botoes"` **já existia** (`message.go:233`), forma `{id, titulo}`,
para a variante de mensagem interativa. Mesma chave, duas formas, desambiguadas só pelo `tipo` da
mensagem.*
**E não era só cosmético:** duas tags `json:"botoes"` no mesmo nível fazem o `encoding/json`
**ignorar as duas**.
*Custo: zero — pego pelo consumidor `consumer-b` lendo o contrato antes de planejar em cima, com a
tarefa já em execução. Não foi revisão daqui, e a revisão daqui tinha lido aquele trecho do contrato
dezenas de vezes hoje.*
**Por que o primeiro caso escapa e o segundo não:** nomes diferentes para a mesma coisa **incomodam
quem lê** — a pessoa vê os dois e pergunta qual usar. O mesmo nome para coisas diferentes **não
incomoda ninguém**: cada leitor encontra um dos dois, entende, e segue. A colisão só aparece para
quem lê o contrato inteiro de uma vez, que é raro — ou para o compilador, tarde demais.
*Regra prática: antes de nomear campo novo, `grep` da chave JSON no contrato E no código. Custa dez
segundos e é a única defesa contra a classe inteira.*

**Aviso no contrato não viaja com o dado; NOME de campo viaja.** O `serie_7_dias[].dia` sempre foi UTC
— e a nossa defesa contra o consumidor ler no fuso errado era um parágrafo no contrato pedindo que ele
*"escrevesse UTC na tela dele"*. O `consumer-a` discordou do **remédio**, não do diagnóstico
(2026-07-28, com a rota tendo UM dia de vida): *"ele põe a guarda na intenção do consumidor, e
consumidor novo não lê o canal. Um nome de campo viaja com o dado até dentro do `console.log` de quem
estiver depurando às duas da manhã."*
*E eles provaram com o próprio bug que consertavam naquele dia: alguém escreveu na docstring "NÃO
mexa neste campo, é o ponto mais fácil de errar", a guarda ficou no aviso, e o `default=` da coluna
gravou errado assim mesmo — **custou orçamento não entregue e dois reenvios queimados**. É a
armadilha-mãe deste arquivo com outra roupa: a regra existia escrita, e não existia no lugar onde o
dado passa.*
*Custo aqui: **zero**, e só por causa da janela — a rota tinha um dia, nenhum dos dois consumidores
tinha leitor em produção, e por isso o conserto coube como ADITIVO (`dia_utc` ao lado de `dia`, mesmo
valor) em vez de quebra de contrato. Uma semana depois teria sido a segunda coisa, e "renomear" é
decisão do dono, não nossa. **A janela para trocar um nome é enquanto ninguém lê**, e ela fecha
sozinha.*
**A pergunta que generaliza: quando você escrever um aviso no contrato sobre COMO ler um campo,
pergunte se o aviso cabe DENTRO do nome.** Unidade, fuso, escala e moeda cabem quase sempre — e o
nome é a única parte da documentação que o consumidor não consegue não ler.

**"Deduplique pelo `id`" produz `SELECT`-depois-`INSERT`, e isso não é dedup.** O contrato mandava
deduplicar por evento e **não dizia como**.
*Achado em 2026-07-26 perguntando a um sistema desta rede que ainda NÃO integrou com o gateway
(`consumer-b`) como ele deduplica hoje, antes de ligar um buffer de reentrega. Resposta, conferida
por ele no schema de produção e não no ORM: `if exists(): return` — read-then-write, sem transação,
sob 3 workers `sync`, sem `UNIQUE` no banco.*
**E o fato de ele NUNCA ter lido o nosso contrato é o que torna o achado forte, não fraco:** essa é a
forma que alguém escreve sozinho, integrando direto com a Meta, sem ninguém instruir. O consumidor
que JÁ integrou fez com `UNIQUE` — então a frase do contrato não induz o erro, mas também **não
protege dele**, e a leitura mais natural do problema chega no padrão quebrado por conta própria.
**E não é risco só do buffer:** a própria Meta reentrega **5 vezes em 9 segundos** quando não recebe
`200` a tempo (medido no nosso access log). Processamento mais lento que o intervalo entre tentativas
já produz entregas simultâneas do mesmo evento **hoje**, sem buffer nenhum.
*O custo de uma duplicata não é uma linha repetida: os efeitos colaterais rodam de novo. No caso
medido, sairia outra resposta automática para a cliente e mais cota da Meta queimada — o estrago
aparece no celular de uma pessoa.*
**A regra que ficou no contrato: obrigação que depende de atomicidade tem de DIZER "atômico" e
mostrar a forma certa.** Quem escreve o contrato conhece o modo de falha; quem lê, não — e a leitura
mais natural é a que quebra.

**Janela de anti-repetição por tempo parece dedup e cobre a faixa errada.** O mesmo consumidor tinha
uma guarda de 60 s (mesma mensagem, mesmo destinatário) nascida de um incidente real. Ela absorveria
a rajada de 9 s da Meta — **e não a reentrega de +5 min nem a de +1 h35**, que é exatamente quando o
evento volta. Guarda por tempo protege do acidente humano (seis cliques no botão); ela não protege da
reentrega de um sistema que espera em escala logarítmica.
## Relógio e carimbo — nove variantes do MESMO erro, e cada regra pegou só a anterior

Não é preciosismo de formatação. Carimbo errado num canal entre sessões é o que decide, meses depois,
se *"o evento X causou Y"* ou a ordem inversa — e ele sobrevive à revisão melhor que qualquer defeito
de código, porque **ninguém desconfia de número redondo**. As três aconteceram entre 2026-07-26 e
2026-07-28, cada uma **depois** da regra criada para evitar a anterior.

**A regra e a tabela das três variantes NÃO moram aqui** — elas valem para qualquer par de sessões,
inclusive as que ainda não existem, e por isso foram versionadas em
`C:\dev\github\docs\CANAL-ENTRE-SESSOES.md`, seção 3 (commit `df0d5a2` do repo `github`, movidas para
lá pelo `consumer-a` em 2026-07-28). **Vá ler lá antes de carimbar qualquer coisa.** O que fica nesta
seção é só o que é nosso: o custo que a variante 3 cobrou aqui, e a pergunta que ela generaliza.

**A variante 3 é a mais difícil e foi a nossa**, em 2026-07-28: o planner rodou `date` às 15:12,
carimbou aquela seção com o resultado, e para as seções seguintes **extrapolou o número** — reusou o
relógio de vinte minutos antes com um acréscimo estimado, mantendo o `%z` corretamente copiado. Duas
seções saíram com `15:34` quando o relógio real era `15:26`.

*Custo: sete minutos de erro em dois canais, corrigidos e não apagados. O prejuízo evitado é o que
importa — o `consumer-a` correlacionaria essas seções com linhas de log do gateway, e sete minutos
inverte ordem de eventos.*

**Quem pegou, e o método está no doc versionado:** o `consumer-a` não comparou carimbo com carimbo —
foi buscar o `mtime`, **o número que nenhum dos dois agentes escreve**. Carimbo contra carimbo é um
agente contra o outro, e **nenhum dos dois é testemunha**. É a mesma disciplina do *"prova de verdade
é tráfego da contraparte"* (bloco de retomada, `docs/TASKS.md`), aplicada a relógio.

**E a pergunta que generaliza, que é o que torna esta seção útil fora de carimbo:** quando você criar
uma regra para impedir um erro, pergunte **qual PARTE do dado ela blinda** — porque a próxima
ocorrência vai ser na parte que sobrou. Aqui a família tem três partes (número, rótulo, e o ato de
medir), e cada regra cobriu uma. É a armadilha-mãe deste projeto (*a regra vale num lugar e não vale
no seguinte*) aplicada às **próprias regras**.

**A variante 4 chegou no dia seguinte e é a parte que faltava: o carimbo estava CERTO e o
REFERENCIAL contra o qual ele é lido estava errado.** O `zapgw estado` da `v0.25.0` imprimia, contra
a produção (medido no CT 125 em 2026-07-28 18:22, minutos depois de a versão subir):

```
gerado_em:      2026-07-28T18:22:31Z (ha 0s)
token_meta:
  medido_em:    2026-07-28T18:22:32Z (daqui a 0s)
  conferido_em: 2026-07-28T18:22:33Z (daqui a 1s)
```

Nenhum dos três carimbos está errado, e nenhum foi extrapolado: os três são o instante real do que
descrevem. O erro é a palavra **"daqui a"** — futuro — sobre uma medição **já acontecida**. A causa é
que o CLI tinha UM `agora` na mão (o que carimbou o `gerado_em`) e o usou para as duas perguntas
diferentes que a tela faz: *"de quando é este retrato?"* (conteúdo, instante escolhido e
compartilhado entre as duas superfícies) e *"isto é fresco?"* (distância, que é sempre contra o agora
de quem está lendo). Entre um e outro o CLI **mede o token na Graph API** — ele mede antes de ler,
porque o cache da vigia vive no processo do servidor (T-065) —, então `medido_em` é legitimamente
posterior ao `gerado_em` e a conta dá positivo.

*Custo: nenhum número errado e nenhum minuto perdido — e ainda assim vale a entrada, porque o
prejuízo é de CONFIANÇA e cresce com a lentidão da Meta, que é exatamente quando alguém está olhando
esta tela: duas instâncias com a Graph API a 4s cada e o operador lê `(daqui a 8s)` no meio de um
incidente. Uma tela que anuncia o futuro sobre algo que já aconteceu treina a desconfiar do resto
dela — e ela é o único instrumento que a pessoa tem naquele momento. Conserto (T-072):
`outbound.printClock`, lido DENTRO de `StateRows`/`ReadableStamp`; o instante deixou de
ser parâmetro dessas duas, porque o chamador tem na mão justamente a resposta errada e vai passá-la —
foi o que aconteceu.*

**O conserto que NÃO serve, e ele é tentador: `if futuro { imprime "ha 0s" }`.** Carimbo genuinamente
futuro existe e é legítimo — o `expira_em` do certificado sai `(daqui a 54d)` e está certo. A regra é
outra: **"daqui a" é para o que ainda não aconteceu; carimbo de OBSERVAÇÃO nunca está no futuro, e se
estiver, o referencial é que está errado.** Esconder o sintoma apagaria o único sinal de que o
referencial errou.

*Duas mutações, feitas e revertidas antes do commit, **e a segunda encontrou mais do que provava**:*
(1) voltar o referencial para o `gerado_em` deixa
`TestStateRowsMeasureTheDistanceAgainstThePrintNowNotAgainstGeneratedAt`
(`internal/outbound/state_test.go`) vermelho **na palavra**, com `medido_em` saindo
`"(daqui a 1s)"` — a asserção é sobre o TEXTO impresso, não sobre a duração calculada, porque foi o
texto que enganou quem leu; (2) o teste de ponta a ponta no comando de verdade
(`TestStateCommandDoesNotAnnounceAsFutureAMeasurementThatALREADYHAPPENED`, `cmd/zapgw/state_test.go`) **passou
VERDE com o defeito reposto** enquanto a Graph API falsa demorava 50 ms: **todo carimbo deste projeto
é RFC3339 SEM fração de segundo**, e os dois instantes caíam no mesmo segundo. Só com 1 s de atraso —
o que garante que a medição caia num segundo *posterior* — o teste reproduz a produção byte a byte.
**A lição: teste que depende de dois instantes SEREM diferentes tem de forçar a diferença na
granularidade em que o dado é GRAVADO, não na do relógio** — senão ele passa verde na sua máquina,
falha um dia por acaso e ninguém sabe por quê.

**A pergunta que generaliza, e ela é irmã de "de onde cada campo VEM em cada processo?" (T-065):**
quando um mesmo `agora` alimenta duas coisas numa mesma função, pergunte se as duas respondem à
MESMA pergunta. *"Quando isto foi montado?"* e *"isto é fresco AGORA?"* parecem a mesma leitura do
relógio, e não são — a primeira precisa de um instante compartilhado, a segunda não tem com quem
compartilhar.

**A variante 5 é a primeira que mora dentro de um TESTE, não de uma tela ou de um canal: comparar
struct que carrega um carimbo de relógio faz o teste falhar por PASSAGEM DO TEMPO, e o sintoma se
disfarça de "flake sem causa".** `TestMenuDoesEXACTLYWhatTheCommandLineDoes`
(`cmd/zapgw/menu_test.go`) compara o `InstanceSummary` inteiro criado pelo menu com o criado pela
linha de comando — de propósito, porque é a comparação de struct inteiro que impede um campo novo
de divergir calado (a mesma disciplina da T-045, seção acima). Um dos campos, `StampsSince`, é
carimbado pelo relógio dentro de `store.CreateInstance` no instante exato da escrita — e o teste cria
as duas instâncias em duas chamadas sequenciais. Quando o segundo do relógio vira entre as duas, os
dois carimbos RFC3339 (sem fração de segundo) saem diferentes por um segundo, e o teste falha
apontando um campo que não tem nada a ver com o que ele existe para provar. **Medido no `main` em
2026-07-29: `-count=150` deu 5 falhas (~3,3%).**

*Por que não deu para injetar relógio fixo (a saída preferida desta família, ver T-072 acima): os
dois caminhos — menu e linha de comando — passam pelo MESMO `provisionInstance`
(`cmd/zapgw/provision.go`), que chama `store.CreateInstance` (não a variante `…Em`, que recebe o
instante por parâmetro) e por isso não recebe a hora de fora. Mudar isso seria mexer em
`provision.go`, fora do escopo da T-092. Conserto (T-092): zerar `StampsSince` NOS DOIS LADOS
antes de comparar, com comentário nomeando o porquê — e só esse campo; o resto do struct continua
comparado por inteiro, para não reabrir a armadilha que a comparação total existe para fechar.*

*Custo: o minuto perdido em si é pequeno, mas o hábito que ele ensina não é — um verify vermelho a
cada ~30 execuções treina quem lê a suíte a dizer "esse é o de sempre", e este projeto já deixou um
`main` vermelho DE VERDADE chegar à produção porque a saída do verify foi lida errado (2026-07-28,
seção *Ambiente*). Teste que falha "às vezes" é pior que teste que sempre falha: ele some da suíte e
reaparece no incidente.*

**Mutação obrigatória, feita e revertida antes do commit:** trocar um valor da entrada do menu (a
`--callback-url`, omitida em vez de enviada) faz o menu montar `args` diferentes da linha de comando
— e o teste fica vermelho, mostrando o campo `callback_url` divergente (`Cadastrado:true` contra
`Cadastrado:false`) enquanto `StampsSince` continua zerado e mudo nos dois lados. **A zeragem do
campo do relógio não mascarou a divergência real** — prova que o teste continua sendo o mesmo teste,
só sem o ruído do segundo que vira.

**A pergunta que generaliza, e ela é a mesma da T-072 com o alvo trocado para dentro da suíte: ao
comparar dois structs criados em momentos diferentes, algum campo é carimbado pelo relógio da
ESCRITA?** Se for, ele não prova nada sobre o que o teste quer provar — ele só prova que o tempo
passa. Neutralizar esse campo especificamente é diferente de parar de comparar o struct inteiro:
a primeira remove ruído, a segunda reabre a armadilha-mãe deste projeto.

**A variante 6 é a mesma T-092 batendo de novo, em MENOS DE 24 HORAS, por um campo NOVO — e prova a
frase acima ao pé da letra.** A T-098 (2026-07-30) acrescentou `TokenSetAt`, outro carimbo do
relógio no mesmo `store.CreateInstance`, e o mesmo teste voltou a falhar por virada de segundo,
agora nos dois campos:

```
menu_test.go:319: a instancia criada pelo menu difere da criada pela linha de comando:
  linha: … TokenDefinidoEm:2026-07-30T22:19:05Z …
  menu:  … TokenDefinidoEm:2026-07-30T22:19:06Z …
```

**Zerar `TokenSetAt` ao lado de `StampsSince` teria consertado hoje e reaberto amanhã** — é
exatamente a lista escrita à mão sobre o esquema que a T-092 já tinha pago o custo de identificar
e não pôde evitar por escopo. A T-100 tomou o caminho que a T-092 apontou como o certo e deixou de
fora: `store.CreateInstanceAt` já existia, recebendo o instante por parâmetro; faltava
`provisionInstance` (`cmd/zapgw/provision.go`) usá-la em vez de `CreateInstance`. O instante
passou a vir de uma var de pacote, `relogioDeCriacao = time.Now` (MESMO padrão de
`outbound.printClock`, T-072 acima), que o teste sobrescreve para um instante FIXO antes das
duas chamadas (linha e menu) e devolve com `t.Cleanup`. Com o relógio congelado, os dois carimbos
nascem IGUAIS nos dois caminhos — a zeragem de `StampsSince` saiu do teste, e a comparação do
struct inteiro voltou a valer sem UMA exceção sequer.

**A prova de que curou, e não só remendou com outro nome:** um campo de carimbo TEMPORÁRIO foi
acrescentado a `Instancia`/`InstanceSummary`, escrito com `carimboDe(agora)` no mesmo INSERT (a
migração e a coluna existiram só durante a prova, revertidas antes do commit) — simulando exatamente
o que a T-098 fez: um carimbo novo, no caminho certo de criação. `go test -run
TestMenuFazEXATAMENTEOQueALinhaDeComandoFaz -count=300` continuou verde. É a diferença entre uma
correção que blinda o CAMPO de hoje e uma que blinda o MECANISMO: o próximo carimbo que alguém
acrescentar ao `INSERT INTO instancia` — desde que nasça do parâmetro `agora`, como todos os outros —
já sai coberto, sem ninguém precisar lembrar de tocar no teste.

**A pergunta que esta variante acrescenta à de cima:** quando a correção for "zerar/ignorar o campo
X antes de comparar", pergunte se dá para, em vez disso, fazer as DUAS escritas compartilharem o
MESMO instante. Comparar struct inteiro sem exceção nenhuma é mais forte do que comparar struct
inteiro menos uma lista de campos — e só a primeira sobrevive ao próximo campo.

**A variante 7 é a primeira que sujou o REGISTRO PERMANENTE, e ela entra por medição em produção.**
Achada em 2026-07-30 22:12 -03: `docs/CHANGELOG.md` abria com `## v0.37.2 — 2026-07-31` e
`## v0.37.1 — 2026-07-31`, **duas versões datadas num dia que localmente ainda não tinha começado**.
Não houve erro de digitação nem de relógio: quem escreveu mediu em produção, e o instante medido
(`00:24`, `00:50`) veio do journal do CT, **em UTC**. Às 21:24 de Brasília já é `2026-07-31` em UTC —
então o carimbo estava certo *no fuso de onde foi lido* e errado no arquivo onde foi colado, porque
**todas as outras entradas do changelog usam local (-03)**.

**O custo é pequeno hoje e cresce sozinho:** os dois arquivos de canal, o `docs/TASKS.md` e o
`_Completed …_` de cada tarefa são local; um leitor futuro correlacionando o changelog com qualquer
um deles vê duas entregas no "dia seguinte" ao dia em que o resto do dia aconteceu. É exatamente o
defeito da variante 3 — ordem de eventos entre fontes —, só que agora dentro do registro que este
projeto trata como **permanente e não reconstruível**.

**A regra, e ela vale para toda medição feita numa máquina que não é esta:** *carimbo lido de outra
máquina não se cola cru.* Converta para o fuso do arquivo, **ou** escreva o fuso ao lado
(`00:50 UTC = 21:50 -03`) — foi o conserto aplicado. E, se você é implementador medindo em produção:
o `date` da SUA máquina e o `journalctl` do CT **não estão no mesmo fuso**, e nada no meio avisa.

---

🔥 **A nona variante, no MESMO dia da oitava e com duas horas de diferença: os implementadores
carimbam o CHANGELOG de cabeça, e o changelog é o registro permanente.**

Em 2026-08-20, duas tarefas seguidas (T-145 e T-149) foram aposentadas com
`_Completed 2026-08-20 22:40._` e `_Completed ... 23:15._`. **Eram 13:26 e 13:31.** Nove horas à
frente, num arquivo que este projeto trata como fonte da verdade — é nele que se grepa o **próximo id
livre de tarefa**, e é dele que sai a linha de cada versão.

**O árbitro era grátis e estava ali:** `git log --date=format:"%H:%M"`. Os oito carimbos anteriores do
mesmo dia batiam com a hora do commit dentro de um ou dois minutos; os dois últimos destoavam em nove
horas. *Nenhum agente escreve a hora do commit — é o mesmo tipo de testemunha que o `stat -c '%y'` é
para o canal.*

🔴 **A causa não foi o agente: foi a MINHA instrução.** O prompt de despacho dizia *"com a hora
real"*, e "real" é adjetivo — não é comando. **A ordem tem de ser `rode `date` e use a saída`**, do
mesmo jeito que a regra do canal exige `date "+%Y-%m-%d %H:%M %z"` em vez de "carimbe a hora".
*Instrução que pede uma qualidade em vez de um procedimento é atendida pela aparência da qualidade.*

*Custo real: zero em produção — mas eu deixei passar o PRIMEIRO e só vi no segundo, o que significa
que a revisão não estava olhando. Se tivesse parado no primeiro, o changelog teria uma linha mentindo
para sempre, e a mentira estaria no arquivo que a próxima sessão usa para não repetir id.*

**A regra que fecha, e ela generaliza para todo trabalho delegado:** *o subordinado herda as suas
regras, não os seus instrumentos.* Ele não tem o seu relógio, o seu `date`, o seu contexto de quando
a sessão começou — então tudo que for medição precisa vir com o COMANDO, não com o adjetivo. E
qualquer número que um delegado escreva num registro permanente **precisa de um árbitro na revisão**,
porque ele parece tão plausível quanto o certo.

🔥 **A oitava variante, 2026-08-20, e ela é a mais desconfortável: os DOIS lados do canal erraram o
carimbo com cinco minutos de diferença — no dia em que passaram a manhã achando instrumento que
mente.**

O consumidor `consumer-b` **extrapolou** um número (carimbou `13:06`, o `date` dizia `13:05`). Eu
fiz a outra metade: o `ATUALIZADO_EM` da minha seção saiu **medido** (`$(date)`, `13:06`) e o
**título** da mesma seção saiu **da cabeça** (`13:08`). Rótulo medido, número inventado — a mesma
família da variante do `-03` colado em número UTC, com os papéis trocados de lugar dentro do mesmo
arquivo.

**Quem provou não foi nenhum dos dois agentes**, e é por isso que a regra do árbitro existe:

```
stat -c '%y' consumer-b-STATUS.local.md   →   2026-08-20 13:06:46 -0300
```

*Custo real: nenhum evento foi correlacionado errado — pego em minutos, pelo outro lado do canal. O
que ele cobrou foi a ilusão de estar coberto.*

🔴 **A lição não é sobre minutos, e é o motivo de esta linha existir:** naquela manhã os dois lados
acharam **cinco** defeitos que eram todos instrumento mentindo — mensagem de erro truncada no
repasse, sentinela nomeando a unidade errada, exportação de conversa que não exporta um tipo de
mensagem, e um aviso enterrado dentro de uma seção sobre outro assunto. **Estávamos afiados em
desconfiar do instrumento do outro e do próprio código, e nenhum dos dois desconfiou do próprio
relógio.**

**A regra que generaliza:** *o instrumento em que você mais confia é aquele que você nunca pensou em
chamar de instrumento.* Relógio, exportação, mensagem de erro e título de seção não parecem
medições — parecem fatos. Quando estiver caçando instrumento mentiroso, comece pela lista do que você
não classificaria como instrumento.

🔴 **Adendo do mesmo dia, e ele é o que fecha esta seção: escrever a armadilha NÃO impediu de
cometê-la.** Depois de registrar a oitava variante e a nona, o planner errou o carimbo do título de
seção **uma terceira vez**, na mesma sessão. As três têm a mesma mecânica, e ela é de PROCEDIMENTO,
não de atenção: *o rodapé (`ATUALIZADO_EM`) era interpolado de `$(date)`; o título era digitado à
mão.* Um vinha de medição, o outro da cabeça — no mesmo arquivo, com dois minutos de diferença.

**O conserto que funcionou não foi lembrar melhor: foi interpolar o título também.** Enquanto um
número for digitado, ele vai errar, e a taxa de erro não cai por o autor saber que ele erra.

*Vale para além de carimbo: se você documentou uma armadilha e continua caindo nela, o problema não é
a documentação — é que o passo que produz o erro continua sendo manual. Automatize o passo ou aceite
que a linha do ARMADILHAS é só um lugar para registrar as recaídas.*


## Documentação

🔥 **INCIDENTE COM DATA DE RESET REGISTRADO COMO REGRA PERMANENTE (2026-08-29 → corrigido em
2026-08-30).** A CI deste repositório foi removida porque a **franquia mensal de minutos de Actions
da conta** tinha sido consumida — pela CI de **outro projeto privado** do dono, e não pelos `push`
daqui. O que ficou escrito, em três arquivos, foi outra coisa: *"repo privado paga Actions"* e
**"regra do dono: CI do GitHub só em projeto público"**. A primeira frase é falsa (repo privado tem
franquia grátis); a segunda nunca foi dita.

**O custo, e ele é de decisão, não de código:** a regra inventada viajou de `CLAUDE.md` daqui para o
`CLAUDE.md` do repo público (`C:\dev\zapgw`), onde virou **linha de tabela de regras duras** — e no
dia seguinte uma sessão minha recomendou ao dono **antecipar a abertura pública do repositório**
para "recuperar a CI de graça". *A recomendação era coerente, bem fundamentada e inteiramente
construída sobre uma causa que ninguém tinha medido.* Quem derrubou foi o dono, em uma frase:
*"estourou a quota, só volta dia 01/09"*.

**A regra que fica:** *falha com data de reset não vira doutrina.* Quando a causa de uma
indisponibilidade não estiver medida, escreva **o sintoma e o prazo** — nunca a regra geral que
"explicaria" o sintoma. Regra sem custo medido atrás é palpite promovido, e palpite promovido é
exatamente o que o leitor seguinte trata como fato.

**E a segunda, mais específica:** *afirmação sobre o estado de OUTRO repositório envelhece sem
avisar ninguém.* A mesma linha do `zapgw` já tinha mentido antes por esse motivo — dizia que a CI
"existe no `zapgw-dev` e migra com o código" um dia depois de o arquivo ter sido apagado de lá.
Quando um doc precisar afirmar o estado de outro repo, ele diz **como medir** (`gh repo view`,
`git show <sha>:<caminho>`), não o resultado da medição.

🔥 **PONTEIRO TRADUZIDO NÃO ACHA NADA (2026-08-21).** Na passada do código para inglês, vários
comentários traduziram o **título da seção** que citavam — `"TLS — não existe modo desligado"` virou
`"TLS — there is no off mode"`, `"Testes"` virou `"Tests"`. Os documentos apontados **continuam em
português**, então quem grepa a citação não encontra a seção. Nove ocorrências chegaram ao `main`
antes de alguém notar.

*Como apareceu, e vale mais que o conserto:* **um implementador viu dois forks dele fazendo isso,
corrigiu, e no relatório observou que o piloto já mesclado tinha feito o CONTRÁRIO com os títulos do
`CLAUDE.md`.* Foi a **contradição entre dois trabalhos** que revelou o defeito — nenhum dos dois,
sozinho, parecia errado.

**A regra que fica, e ela é mais larga que tradução:** *string que existe para ser **procurada** não
se traduz nem se reescreve* — título de seção, nome de arquivo, nome de campo da API, valor que sai
no log. A prosa ao redor é livre; a âncora, não. **E a conferição é mecânica:** toda citação entre
aspas ao lado de um `docs/*.md` tem de ser achada por `grep -F` no arquivo apontado.

**Backtick dentro de `git commit -m "…"` EXECUTA, e o commit sai com sucesso mesmo assim.**

2026-08-20: uma mensagem de commit deste repositório perdeu a frase
*`` `request.POST.get("alcance") or LOCAL` ``* — o shell tratou o trecho entre backticks como
substituição de comando, tentou executá-lo, falhou, e substituiu por **string vazia**. A linha ficou
*"o servidor tinha : escolheu por conta propria"*.

**Nada falhou:** o `git commit` retornou zero, o push passou, e o único sinal foi um aviso de sintaxe
no meio da saída, entre outras linhas. *O arquivo versionado estava certo; quem perdeu conteúdo foi o
registro permanente.*

**O conserto é de procedimento, não de atenção:** mensagem longa vai por **heredoc**
(`git commit -F -` com `<<'FIM'`), nunca por `-m "…"`. Heredoc entre aspas simples não interpreta
nada — nem backtick, nem `$`, nem `!`. *Escapar backtick a backtick funciona até você esquecer um, e
esquecer um não gera erro.*

*Custo real: uma frase técnica perdida num commit já empurrado. Corrigir exigiria reescrever
histórico publicado, que custa mais do que a frase vale — então ficou, e virou esta linha.*



### 🔥 O registro de execução anotou a INTENÇÃO do plano como se fosse o RESULTADO — e a credencial ficou sem prazo por onze dias (2026-08-17)

**Custo: um token de API com escopo de conta inteira — `DNS Write`, `Zone WAF Write`, `SSL and
Certificates Write`, `Page Rules Write`, `Cloudflare Tunnel Write` — vivo e sem validade nenhuma
desde 06/08. Ele não foi achado por nós: foi um time de outro projeto que auditou a conta e avisou
pelo canal. E, ao responder a eles, eu repeti a afirmação falsa como se fosse tranquilizadora.**

O plano de migração (`docs/superpowers/plans/2026-08-06-tunel-cloudflare-e-migracao-de-zona.md`)
dizia, na seção de preparação: *"Prazo de validade: 30 dias"*. Era a **intenção**. O REGISTRO DE
EXECUÇÃO do mesmo arquivo, escrito depois, anotou na tabela de "Feito e conferido":

> *"Token escopado criado — `zapgw-migracao-cloudflare`, id `2ee4c9cf…`, **expira 2026-09-05**.
> Guardado em … (fora de repositório), valor nunca impresso"* — coluna **Prova**:
> `/user/tokens/verify` → `active`

🔴 **A "prova" provava outra coisa.** `/user/tokens/verify` responde `active` — ele **não devolve
`expires_on`**. A linha juntou um fato medido (o token funciona) com um fato não medido (a data),
numa tabela cujo título é *"Feito e conferido"*. Medido em 2026-08-17 pelo `GET /user/tokens`: o
campo `expires_on` estava **ausente**. Não havia prazo. O token não ia morrer nunca.

**As três lições, e a terceira é a que generaliza:**

1. **Campo que a sua prova não devolve, você não provou.** Se a afirmação é sobre `expires_on`,
   a medição tem de trazer `expires_on` na tela. `active` não é sobre isso.
2. **Intenção do plano e resultado da execução são seções diferentes por um motivo.** Copiar o
   número da primeira para a segunda transforma "eu queria 30 dias" em "tem 30 dias", e as duas
   frases ficam idênticas no papel.
3. **Prazo que ninguém mediu é prazo que não existe — e ele é pior que não ter prazo nenhum**,
   porque desliga a vigilância. Foi exatamente o efeito: por onze dias a credencial mais larga da
   conta não incomodou ninguém, inclusive eu, *porque estava escrito que ela morria sozinha*.

*É a mesma família do "veredito bonito não é medição" que este projeto já pagou duas vezes (o
`DIVERGE em 12 de 12` do extrator quebrado, no mesmo plano; e o `readyConnections` decidido pelo
corpo e não pelo status). Aqui o veredito bonito estava numa tabela de documentação, que é onde ele
sobrevive mais tempo sem ninguém conferir.*

**Estado hoje** (2026-08-17, medido em `GET /user/tokens`, não deduzido — é a lição 1 aplicada a
esta própria linha): o `zapgw-migracao-cloudflare` foi revogado, e os três tokens `zapgw*` que
sobraram expiram em **2027-02-17**:

| Token | Nome em 2026-08-17 | Escopo | Expira |
|---|---|---|---|
| `zapgw_conf_dns` | `zapgw-dns-tenant-one` | `DNS Write`, **só** na zona `tenant-one.com.br` | 2027-02-17 |
| `zapgw_conf_tunnel` | `zapgw-tunel` | `Cloudflare Tunnel Write` + `Account Settings Read`, conta | 2027-02-17 |
| `zapgw_conf_worker` | `zapgw-sonda-worker` | Workers Scripts Write/Read, Tail Read, Observability Read, Account Settings Read | 2027-02-17 |

*A coluna do meio é o nome **antigo**, e ela existe porque o histórico da conta e os commits até
2026-08-17 só conhecem aquele. Os três viraram `<projeto>_<tipo>_<função>` (T-126): `conf` porque
**configuram** — nenhum está no caminho de execução, então todos podem vencer sem derrubar nada. A
doutrina está em `C:\dev\github\docs\CREDENCIAIS-DE-API.md`, seção 2. Os arquivos em `~/.secrets/`
**não** acompanharam o nome novo, de propósito: quem os lê é o `sonda-worker/deploy.sh`.*

*O prazo foi aplicado por `PUT /user/tokens/<id>` — não por recriação —, então nenhum valor mudou e
nada em `~/.secrets/` precisou ser regravado. Depois do `PUT`, cada token foi provado duas vezes:
`/user/tokens/verify` → `active` **e** a listagem dos `permission_groups`, porque `active` diz que o
token vale e **não** diz que ele manteve as permissões. Essa é a lição 1 outra vez, e ela quase
escapou: a primeira execução deixou o controle positivo sem rodar e ainda assim saiu com código 0.*

🔴 **O que NÃO tem prazo, e é de propósito:** o **token do conector** do túnel, que o
`cloudflared-zapgw.service` usa no o LXC do Traefik (`EnvironmentFile` modo `600`, `TUNNEL_TOKEN=`). Ele não
é user API token, não aparece em `/user/tokens` e nenhuma operação de API de token o alcança. *Vale
saber antes de mexer em qualquer coisa chamada "túnel": são duas credenciais diferentes, e só uma
delas mantém a produção de pé.* **Elas tinham o mesmo apelido até 2026-08-17** — `zapgw-tunel` era o
token de API, quase idêntico ao nome do conector —, e foi exatamente isso que obrigou o dono a parar
uma operação em andamento para avisar *"cuidado com o token do túnel"*. **Só a documentação separava
os dois.** Hoje o nome separa: `zapgw_conf_tunnel` diz `conf`, e `conf` pode vencer.

---

**A tarefa mediu o custo numa superfície e listou `Files:` de outra — o conserto não alcançou o
sintoma.** A T-106 (2026-07-30) nasceu de uma medição precisa: o **monitor de falhas** disparava em
tráfego normal de Instagram, oito vezes em quarenta segundos. O `Why:` dizia isso com todas as
letras. Mas o `Files:` listava **só `internal/meta/`**, e o `Do:` mandava separar as categorias de
erro — que era metade do problema. O implementador fez **exatamente** o que foi pedido, e fez bem.

**No dia seguinte, com a `v0.40.0` no ar, a linha ficou assim:**

```
zapgw: parse falhou na instancia "tenant-two-ig": meta: item legitimo que esta fatia do instagram
nao modela: 1 item(ns)
```

**O texto do erro está certo; o prefixo continua chamando de falha.** `internal/inbound/handler.go`
imprime a mesma frase para qualquer erro de parse, sem olhar qual é — e o monitor, que casa pela
linha, **continuou disparando**. O custo medido não se moveu um milímetro. (É a T-110.)

🔴 **A regra, e o erro é de quem escreve a tarefa, não de quem a executa:** *se o `Why:` mede o custo
numa superfície — journal, monitor, tela, resposta HTTP —, o `Files:` tem de incluir essa superfície,
ou o `Do:` tem de dizer explicitamente por que ela fica de fora.* Separar a causa na camada de baixo
é pré-requisito, não conserto: **quem sente o defeito é quem lê a linha.**

**O sintoma de que isso aconteceu é característico e vale reconhecer:** a tarefa fecha com verify
verde, mutação confirmada e relatório impecável — **e o alarme continua tocando.** Quando isso
acontecer, não desconfie do implementador; releia o `Files:` da tarefa.

**Carimbo INVENTADO num canal entre times — e ele inverte a ordem de leitura do arquivo.** Em
2026-07-30 o planner escreveu quatro seções seguidas no canal do `consumer-b` e carimbou três delas
com `23:05`, `23:20` e `23:30` **sem rodar `date`** — incrementos plausíveis, escritos enquanto
redigia. A hora real quando a última subiu era **23:05**. Resultado: num arquivo cuja convenção é
*"seção nova entra no TOPO"*, a seção **mais nova** ficou com o número **menor** que as três de baixo.
Quem lê de fora não tem como saber qual veio antes — e a leitura errada aqui não é acadêmica: uma das
seções **retirava** a anterior, e trocar a ordem inverte o significado das duas.

**Por que isto é diferente da variante 7** (aquela era UTC colado num arquivo em -03, um erro de
**conversão**): aqui não houve medição nenhuma para converter. *Chutar um carimbo é mais barato e
mais tentador que convertê-lo errado — e produz um número que parece medido, porque nada no texto
distingue os dois.*

**A regra:** `date "+%Y-%m-%d %H:%M %z"` **antes de cada seção**, e cole a saída no cabeçalho, como
o `ATUALIZADO_EM` deste projeto já exige. Uma chamada por seção, não uma por sessão. **E não
reescreva carimbo errado depois** — anote acima que ele não foi medido e que a ordem verdadeira é a
posição no arquivo; reescrever apaga a prova de que houve erro e não devolve a hora certa.

**"Virou o padrão" NÃO é "virou obrigatório" — e a diferença entre as duas fez um consumidor cancelar
um experimento válido.** Em 2026-07-30, 23:20, o planner escreveu no canal do `consumer-b` que o
`allow_category_change` *"deixou de comprar recusa"* e que o experimento deles *"não pode dar certo"*.
**As duas frases eram inferência apresentada como fato.** O que a Meta documenta é só isto: desde
2025-04-09, a recategorização automática que o parâmetro habilitava **é o comportamento padrão**.
**O que ela não diz em lugar nenhum é o que `false` faz hoje** — a página de criação de template nem
menciona o parâmetro. *"Padrão" é, na leitura óbvia, o valor que se sobrescreve;* o planner leu como
"obrigatório" e publicou a leitura como se fosse a frase da fonte.

**O custo, e ele foi imediato:** o consumidor cancelou o `_v3` e encerrou o assunto — uma decisão
tomada sobre uma certeza que não existia. Dez minutos depois a seção teve de ser retirada por
inteiro. *Quem lê um canal não distingue "eles conferiram" de "eles concluíram"; quem escreve, sim —
e por isso o ônus é de quem escreve.*

🔴 **A armadilha secundária, que é a mesma do `ig_id` com o alvo trocado:** havia uma medição real na
mesa (o consumidor pediu `UTILITY` e a Meta gravou `MARKETING`) e ela **parecia** confirmar a tese.
Não confirmava nada: o gateway **nunca enviava o campo**, então aquele caso mediu o **caminho
padrão**, não o `false`. *Medição impecável de uma pergunta que não era a que estava em jogo — e é
justamente esse tipo que convence, porque o número é verdadeiro.*

**As duas regras:**

1. **Ao afirmar sobre a Meta, cite a frase dela ou marque como inferência.** Este projeto já exige
   isso para o próprio código (`arquivo:linha`); a fonte externa não é diferente, e é **pior**,
   porque o leitor não tem como grepar para conferir.
2. **Ausência de documentação não é documentação de ausência.** "A doc não diz que `false` funciona"
   e "a doc diz que `false` não funciona" são afirmações diferentes, e só a primeira era verdadeira.
   Quando a pesquisa volta sem posição, a saída barata é **fazer o mecanismo existir e medir** — foi a
   decisão do dono aqui: expor o campo, repassá-lo verbatim e **não prometer efeito nenhum** no
   contrato.

**Reverter em PRODUÇÃO não reverte a doc — e a doc sobrevivente manda alguém desfazer a reversão.**
Em 2026-07-30, o `ig_id` da `tenant-two-ig` foi trocado para `27807047495582675` (o id no **escopo
do App**, o que `GET /me` devolve em `graph.instagram.com`), **4 eventos foram descartados** pela
guarda de roteamento, e a troca foi **revertida** para o `entry[].id` do webhook,
`17841403678746353`. O comando de reversão levou dois segundos. **A doc não voltou junto**: um dia
depois, o valor errado ainda estava em **quatro** arquivos — `docs/CHANGELOG.md` (chamando-o de *"o
id certo, medido duas vezes de forma independente"*), `docs/TASKS.md`, o runbook de implantacao (que fica no repositorio privado) e
`README.md`, os dois últimos como **exemplo de comando pronto para copiar**.

🔴 **O que quase aconteceu, e é o custo de verdade:** a T-103 (a tarefa que deu ao operador a tela
para conferir o `ig_id`) trazia, no próprio `Verify:`, *"tem de exibir `ig_id: 27807047495582675`"*.
**A ferramenta nova de conferir nasceu apontando para o valor quebrado.** Quem rodasse a conferência
e confiasse no texto "consertaria" produção de volta para o estado que descarta todo o tráfego —
e o sintoma disso é silêncio: `200` para a Meta, nada para o consumidor, nenhum erro em lugar nenhum.

**Só apareceu porque a conferência foi feita contra a MÁQUINA**, não contra o texto: `zapgw instancia
mostrar` imprimiu `17841403678746353`, e o contador `conta_descartada` (4, último em `00:48:47 UTC`,
zero depois da reversão, com `recebidas 16 / entregues 16`) fechou a prova pelo comportamento, sem
depender de qual endpoint devolve qual id.

**As duas regras que saem daí:**

1. **Reversão é mudança, e mudança quebra doc.** Ao desfazer algo em produção, rode o mesmo
   `grep -rn "<valor antigo>" docs/ *.md` que você rodaria ao introduzir. Reverter parece "voltar ao
   que estava" e por isso escapa da checagem — mas o que estava, a doc já não descrevia.
2. **Valor real dentro de exemplo de comando é dívida.** `README.md` e o runbook de implantacao (que fica no repositorio privado) traziam o
   id como argumento literal, prontos para copiar e colar em produção. Passaram a trazer
   `<entry[].id do webhook>` — um placeholder que **obriga a pensar** e não pode ficar obsoleto.

**"Não tem tarefa" respondido como se fosse "não tem o campo" — e a fila é a fonte errada.** Em
2026-07-28, 21:27, o planner escreveu no canal do consumidor que o `id` do template na
`GET /v1/templates` *"ainda não virou tarefa"*. **O campo já era entregue** — `meta.Template.ID`
(`internal/meta/templates.go:76`), serializado direto pelo handler (`templates_handler.go:198-204`).
Ele tinha entrado na **T-078**, tag `v0.29.0` às **19:56:23**, *onze minutos depois de o consumidor
pedir às 19:45*, por outro motivo (a releitura do catálogo precisava dizer "é este"). Ninguém
percebeu que o pedido fora atendido de lado.
*Custo: uma afirmação falsa no canal, e o consumidor teria continuado com um contorno desnecessário.
Quem pegou foi o dono, perguntando **"por que você decidiu não fazer?"** sobre uma linha em que
estava escrito "sem tarefa" — a pergunta era sobre prioridade, e a resposta foi que a premissa
estava errada.*

**Duas perguntas diferentes, e a fila só responde uma:**

| Pergunta | Fonte que responde |
|---|---|
| *planejamos fazer isto?* | `docs/TASKS.md` |
| *isto existe?* | **o código, e só ele** |
| *isto NÃO existe — foi esquecimento ou decisão?* | **o `CHANGELOG.md`, onde as decisões moram** |

🔴 **A terceira linha entrou em 2026-07-29, e ela custou o trabalho inteiro de um implementador.** O
dono digitou `zapgw menu` no CT e levou `subcomando desconhecido`. O planner viu a ausência, chamou de
lacuna e escreveu a **T-089** mandando criar o subcomando. **A ausência era decisão**, registrada com
todas as letras no changelog da T-082 (2026-07-28 20:56): *"NÃO existe `zapgw menu` como subcomando
explícito — um nome invocável pode ser posto num script e travar esperando entrada… a guarda deixaria
de ser estrutural."* A tarefa foi despachada, **implementada com guarda de TTY correta e mutação
provando que sem ela o teste pendura**, e o dono **recusou de todo modo**: *o que se protege por
impossibilidade não se troca pelo que se protege por conferência.*
*Custo: uma execução completa de implementador jogada fora, dois docs que passaram algumas horas
prometendo o contrário da decisão, e a branch apagada de propósito — porque branch com decisão
revertida dentro é tentação de merge para a próxima sessão.*
**O que a torna traiçoeira:** o código responde *"não existe"* com perfeição e **não diz por quê**. O
grep confirma a ausência e a ausência parece bug — o único lugar que distingue as duas coisas é o
registro do dia em que alguém decidiu. **Antes de escrever tarefa que ACRESCENTA algo que falta,
grepe o changelog pelo nome da coisa.** Dez segundos, e a alternativa é reverter trabalho pronto.
*Irmã da regra do `consultorio`, com o sinal trocado: lá o encerramento não foi escrito e o assunto
ressuscitou; aqui foi escrito, e não foi lido.*

É a mesma armadilha de *"não afirme sobre o sistema do outro lado sem abrir o arquivo"*, **virada
para o próprio sistema — e por isso menos desculpável, não mais**: aqui o código está na sua mão e
grepá-lo custa dez segundos. Ela é mais provável em projeto com fila madura, porque a fila **parece**
autoritativa: ela é registro de intenção, e trabalho entregue **sai** dela.

🔴 **O gatilho que generaliza, e ele vale para toda entrega:** uma tarefa que resolve um pedido **como
efeito colateral** não deixa rastro no nome de ninguém. Quando uma tarefa tocar um campo, uma rota ou
um erro que **algum consumidor já pediu**, diga isso no changelog dela — senão o pedido continua
"aberto" na cabeça de todo mundo enquanto está pronto no ar.
*Consequência do achado: nada segurava esse campo. Nenhum teste conferia o `id` na LISTAGEM, e com
`omitempty` ele sumiria sem erro e sem falha de suíte — virou a T-085. A mutação da T-085 confirmou
o diagnóstico: apagar o campo do struct só ficava vermelho pelos testes da **criação** e da releitura
ambígua; a listagem inteira passava.*

**A MESMA FALHA, TRÊS HORAS DEPOIS, CONTRA O MEU PRÓPRIO REGISTRO ESCRITO.** Ainda em 2026-07-28, o
dono perguntou *"temos vazamento ou não?"*. Medi produção e achei `numero_descartado: 4` e
`conta_descartada: 1` na instância do consumidor, com cinco `ALARME` no journal carregando
`phone_number_id "000000000000000"` e `waba_id "0"`. Inferi — e marquei como inferência, o que salvou
o relatório de ser falso — que seria o disparo de amostra do painel da Meta durante a configuração do
webhook, e disse que ia **perguntar ao consumidor**.
**Eram os meus próprios testes de mão**, feitos durante a rotação do `app_secret` daquele dia. E a
explicação estava **escrita por mim, no arquivo de canal, horas antes, com os mesmos dois números**:
*"a sua instância já tem `conta_descartada: 1` e `numero_descartado: 4` — dos meus próprios testes de
virada"*. Quem corrigiu foi o dono.
*Custo: um relatório com causa errada, e eu a um passo de perguntar ao consumidor sobre tráfego meu —
o que teria sido a quarta vez no mesmo dia de afirmar sobre um sistema sem abrir o arquivo.*
🔴 **A regra que sai daí é sobre QUAL fonte se consulta, e é a mesma do quadro acima com uma linha a
mais:** *"de onde veio este tráfego?"* não se responde só com contador e journal — eles registram o
**efeito**, nunca a **intenção**. Quem sabe a intenção é o registro de quem agiu, e **neste projeto
esse registro é o arquivo de canal**. Antes de inferir a origem de qualquer evento de produção,
**grepe o canal pelo número**. É mais barato que a inferência, e nesta ocorrência teria respondido em
dez segundos.
⚠️ **Consequência operacional, e ela é a que morde de verdade:** o `numero_descartado` é **visível ao
consumidor** no `GET /v1/estado`, e a esse consumidor foi dito por escrito que ele significa *"algo
está batendo na porta errada"*. **Prova manual em produção vai na instância de teste** — foi o que a
T-042 fez, com `teste-isolamento-t042` —, nunca na instância de um consumidor: o contador dele passa
a mentir, e ele não tem como saber que o ruído é nosso.

🔴 **E o agravante, que fecha o caso contra o remédio óbvio: o contrato JÁ documentava o campo** —
`docs/CONTRATO-CONSUMIDOR.md`, seção *Ler o catálogo de templates*, escrito na própria T-078, com a ressalva certa sobre a
ausência não ser erro. **O campo existia, a doc existia, estava certa, e ainda assim o pedido do
consumidor seguiu "aberto" na cabeça de todo mundo.** Então o remédio não é "documentar melhor": é o
cruzamento explícito no changelog da tarefa que entregou. *Doc responde a quem foi procurar; ninguém
foi procurar, porque ninguém sabia que havia o que procurar.*

**A disciplina foi aplicada ao artefato que é auditado, e não ao que é copiado.** O contrato traz o
exemplo de `reacao` correto — `{"reacao": {"alvo": …, "emoji": …}}` — porque exemplo de contrato é
**executado** antes de entrar, por regra. Mas o anúncio da versão escrito **no canal com o
consumidor** foi digitado à mão, e saiu com `alvo` e `emoji` na **raiz** do corpo. O consumidor
copiou dali (é o texto que estava na frente dele, recém-escrito, sobre a funcionalidade que ele
queria usar) e levou `400 campo obrigatorio ausente: reacao`.
*Custo: dois minutos do consumidor, e a única razão de não ter sido mais é o contrato existir e estar
certo.*
**A lição não é "revise mais": é notar QUAL artefato as pessoas copiam.** Doc é o que se audita;
mensagem é o que se usa às pressas. A regra "exemplo sai de fonte consultável, nunca da cabeça" valia
para os dois e foi aplicada só ao primeiro. **Onde não der para executar o exemplo, aponte para o
doc em vez de reescrevê-lo** — um link não diverge da implementação; uma cópia diverge.

**Guarda local que replica regra de sistema remoto envelhece no relógio DELE, não no seu.** Do lado
do consumidor (registrado por ele em 2026-07-26): o cliente recusava `emoji` vazio localmente, sem
bater na rede, porque "o resultado seria o mesmo `400`". Estava **correto quando foi escrito** — e
ficou errado **na mesma tarde**, quando o gateway passou a aceitar. A mensagem que o operador via
("ainda não suportado") virou mentira sem nada mudar do lado dele.
*Regra: onde der, deixe o dono da regra recusar e traduza a recusa. Duplicar a validação para
"economizar uma chamada" compra latência com uma cópia que ninguém lembra de atualizar — e o dia da
divergência é escolhido pelo outro time.*

**Exemplo de doc é código que ninguém roda — e por isso é onde o valor errado sobrevive mais tempo.**
Três exemplos do `CONTRATO-CONSUMIDOR.md` traziam `"instancia": "consumer-a"`, que é o nome do
**consumidor**, não o slug da instância (`tenant-one`). Os exemplos foram *executados* antes de
irem para o doc — desserializados e validados — e passaram: a validação de esquema não conhece os
slugs que existem, então um slug plausível e inexistente é indistinguível de um certo. **Executar o
exemplo prova a forma, nunca o valor.** Valor de exemplo que nomeia uma coisa real (slug, id,
telefone) tem de ser conferido contra a coisa real — aqui, `zapgw instancia listar`.
*Custo: zero, porque o consumidor leu o exemplo antes de usá-lo e perguntou. Se não tivesse: a
`callback_url` seria cadastrada com o slug errado, a guarda de multi-inquilino do consumidor
responderia `503` a toda entrega, a Meta reenfileiraria por 36 h — e um `503` teimoso **parece
problema de assinatura**, que é onde a investigação começaria. Corrigido em 2026-07-26, junto com um
aviso no próprio contrato de que `instancia` é o número e não o consumidor.*

*E o slug era o menor dos erros — ele só apareceu porque alguém perguntou. Conferindo os MESMOS três
exemplos contra `GET /v1/templates` depois, **todo valor específico estava errado**: `venda_confirmada`
tem três variáveis de corpo e o exemplo mandava duas; `equipamento_enviado` tem quatro e o exemplo
mandava duas; `orcamento_disponivel` tem **um** botão e o exemplo mandava parâmetro para dois
índices. A regra que sai daí é mais dura que "confira o valor": **o catálogo/schema da coisa real é
a fonte, e ele é consultável — `GET /v1/templates` responde em um comando.** Exemplo cujo valor não
foi lido de uma fonte consultável é chute com aparência de referência, e o leitor não tem como
distinguir.*

**A caça a doc falso produz doc falso.** Não é ironia, é o padrão — e aconteceu nesta branch, em
duas rodadas seguidas:

1. Um comentário afirmava que a versão `v21.0` da Graph API "já tinha expirado". **Verificado na
   fonte:** a página de versionamento da Meta confirma que a atual é a `v25.0`, mas diz apenas que
   cada versão vale por **pelo menos dois anos** e **não lista** expirações. A parte da expiração
   viajou de carona no fato verdadeiro.
2. O conserto disso citou `/docs/graph-api/changelog` como fonte — URL que **respondeu 404** na
   mesma data.

*O mecanismo: durante uma revisão você escreve rápido e com confiança sobre algo que acabou de
entender, e **descrever o que a fonte deveria dizer é mais fluente que descrever o que ela diz**.
Trate o texto que você acabou de produzir como a parte menos auditada do repo — porque é
exatamente o que ele é.*

**Afirmação sobre serviço externo exige ponteiro verificado NO MOMENTO da escrita**, e o que não foi
verificado é **marcado como não verificado**, não omitido nem suavizado.

**A prova de que o vazamento fechou REABRE o vazamento.** O `verify_token` vazava na query string do
access log do Traefik (T-019). Rotacionado, o passo seguinte foi provar que o token antigo passara a
ser recusado e que o novo funcionava — batendo na **URL pública**, que atravessa o Traefik. As três
requisições da prova gravaram o **token novo** no mesmo log que a rotação existia para limpar. O
sintoma foi numérico e só apareceu porque alguém contou: `grep -c hub.verify_token` saiu de **19**
para **24**, e três das cinco linhas novas eram do próprio teste.
*Custo: uma segunda rotação, e o valor curto entre as duas esteve exposto. Conserto: provar contra o
gateway DIRETO (`<ip-interno-do-gateway>:8080`), que não passa por proxy nenhum e não tem access log — o caminho
público só deve ser exercitado pela Meta.*
**A regra geral, que vale além deste caso: antes de testar um segredo, pergunte por onde a
requisição do TESTE passa.** Um teste que reproduz o caminho de produção reproduz também os pontos
onde produção grava coisas — e o log de um proxy não distingue "requisição real" de "verificação de
que o vazamento fechou".

**Duas fontes independentes que descem do mesmo nada parecem exatamente com corroboração.**
Sobre "como se remove uma reação pelo ENVIO", em 2026-07-26, havia duas fontes que pareciam
concordar: um resumo de busca dizendo que `emoji: ""` remove, e o **código em produção do
consumidor**, que manda `emoji: ""` nesse caso. Duas origens diferentes, mesma resposta — o formato
de evidência mais convincente que existe. Rastreando cada uma: o resumo vinha de um **agregador
não-oficial**, e o código do consumidor vinha da **docstring de uma lib** que afirmava *"é assim que
a Meta define"* **sem citar a Meta**. Nenhuma das duas tinha visto a coisa acontecer. A página
oficial, lida no mesmo dia, marca `emoji` como **obrigatório** no envio e não descreve remoção
nenhuma. *(Brinde do mesmo rastreio: um terceiro mecanismo que a busca oferecia, "unreact", é de
OUTRO produto da Meta — Messenger Platform, `sender_action`. Resumo de busca mistura produtos da
mesma empresa com uma facilidade que não perdoa.)*
**A pergunta que separa as duas coisas: "quem, na cadeia desta afirmação, VIU acontecer?"** Se a
resposta for ninguém, duas fontes valem o mesmo que zero. Concordância entre elos que não observaram
nada é só o mesmo erro copiado.
*Custo: zero, e por pouco. O implementer rastreou a primeira e o consumidor rastreou a própria — se
qualquer um dos dois tivesse respondido a pergunta em vez de investigar a origem, o gateway teria
ganhado um caminho de remoção que **falha calado com `200`**.*

**"Provado na entrada" não é "provado na saída" — é a mesma palavra, não é a mesma garantia.**
O consumidor observou em produção (2026-07-20) que a Meta **omite** a chave `emoji` no WEBHOOK
quando o usuário remove a reação. Isso é fato, e o envelope depende dele. Mas ele não diz **nada**
sobre o que a Meta ACEITA no envio: são dois lados de uma API com regras próprias, e a simetria é
uma suposição confortável, não uma propriedade. Este documento chegou a ser escrito tratando os dois
como um fato só, e a correção veio do consumidor, sobre o dado dele mesmo.
*Regra: ao citar evidência de comportamento de um serviço externo, escreva a DIREÇÃO junto com o
fato. "A Meta omite `emoji`" é ambíguo; "a Meta omite `emoji` no webhook de remoção" não é.*

**O `verify_token` vai na query string, e o Traefik loga a URL inteira.**
Quem escolhe o formato do `GET` de verificação é a Meta: `?hub.mode=…&hub.verify_token=…`. O
`accessLog` do Traefik grava `RequestPath` completo, então o token fica legível em
`o LXC do Traefik:/var/log/traefik/traefik-access.log`. Observado em 2026-07-25, na verificação real da
`tenant-one`. **Custo até agora: zero** — e vale dizer por quê, senão isto vira alarme falso: o
`verify_token` só é usado no `GET`, nunca no `POST`. Quem o rouba não recebe mensagem nem forja
entrega (isso é o `app_secret`). Registrado como T-019.

**A Meta manda os parâmetros de verificação DUPLICADOS.**
No `GET` real ela envia `hub.mode`, `hub.challenge` e `hub.verify_token` **e também**
`hub_mode`, `hub_challenge`, `hub_verify_token` (com underscore). O gateway lê os com ponto e
funciona. Quem for mexer em `verificar()` não deve "consertar" para underscore achando que a doc
mudou: **os dois vêm juntos**, e trocar quebraria sem motivo.

**Doc que descreve uma limitação inexistente manda gente para o caminho perigoso — e ninguém
confere limitação, só instrução.** o runbook de implantacao (que fica no repositorio privado) afirmava que ativar instância de
laboratório "continua sem caminho de CLI: só `zapgw fumaca` ativa, **e ele fala com a Graph API de
verdade**", e por isso prescrevia `UPDATE instancia SET ativo = 1` digitado à mão no banco de
**produção**. A segunda metade da frase era falsa desde que o `fumaca` nasceu: ele chama
`graphBase` (`cmd/zapgw/main.go`), que lê `ZAPGW_GRAPH_BASE` — apontar para outra ponta sempre foi
possível, e é exatamente o que a suíte faz em `cmd/zapgw/smoke_test.go` desde o primeiro dia.
*Custo: um `UPDATE` na mão no SQLite de produção com o serviço no ar (T-042, e a receita ficou
escrita como procedimento por dois dias), mais duas tarefas (T-048 e T-071) gastas decidindo se valia
abrir uma **segunda porta** para `ativo = 1` — uma decisão de arquitetura para um problema que já
tinha saída.*
**O mecanismo, que é o que vale levar: o falso que o operador precisava estava preso no `_test.go`.**
Quando um teste precisa de um servidor de mentira para provar um caminho, **quem opera precisa do
mesmo servidor para exercitar esse caminho** — e um `grafoFalso` que só existe em arquivo de teste é
invisível para quem está no CT às 18h. Conserto: `cmd/grafo-falso/` (binário à parte; `deploy.sh`
compila só `./cmd/zapgw`) e a receita em o runbook de implantacao (que fica no repositorio privado). **Ao escrever "não tem como", diga
por que não tem — a frase completa é o que se consegue conferir.**

### 🔥 Critério de aceitação de um projeto mora no TEMPLATE DE PR, não no CONTRIBUTING (2026-08-20)

Ao estudar se o `zapgw` poderia entrar no catálogo do **community-scripts**, li quatro documentos
para levantar as regras: `CONTRIBUTING.md`, `AGENTS.md` (1.083 linhas), `docs/guides/source-origin.md`
e a página de contribuição do site. Levantei estrutura de arquivo, anti-padrões, funções de helper,
formato do JSON — e concluí, por escrito, que **não havia critério de elegibilidade**.

Havia. E é eliminatório:

> - [ ] The application is **at least 6 months old**
> - [ ] The application has **600+ GitHub stars**

Está na **checklist do `.github/pull_request_template.md`** do ProxmoxVED. Nenhum dos quatro
documentos de prosa a menciona. Quem me corrigiu foi o dono.

**A regra que generaliza, e vale para qualquer projeto de terceiro:** *documento de prosa ensina
**como escrever**; o template de PR e o de issue dizem **quem entra**.* A checklist que o mantenedor
marca é o critério real. **Leia `.github/` ANTES do `CONTRIBUTING.md`** — é mais curto e é o que
decide.

🔴 **E o número depende da PORTA.** O mesmo projeto pede **600** estrelas de quem submete o próprio
script (PR no ProxmoxVED) e **1.000** de quem pede que alguém faça (discussão no ProxmoxVE). Ler só
um dos dois produz resposta confiante e errada — foi o que eu fiz ao "corrigir" o 600 do dono para
1.000, tendo lido só o segundo. *Quando houver mais de um caminho de entrada, cada um tem a sua
régua; ache a régua do caminho que é o seu.*

*Custo real: um estudo inteiro cuja fase final — submeter ao catálogo — era inelegível por dois
critérios duros, e cuja ordem de trabalho colocava a submissão como destino em vez de portão. O
conserto mudou o Veredito, a Parte II e as duas últimas fases, e revelou de quebra um custo que
ninguém tinha visto: repositório novo **zera o relógio dos 6 meses**. Nada disso teria aparecido se
o dono não soubesse o número de cabeça.*

### 🔥 Número que chega pronto não traz o comando que o produziu — publiquei "18 eventos" que eram 35, porque a origem leu a saída de um `tail -40` (2026-08-28)

Um consumidor mediu, no banco dele, quantas vezes a Meta reclassificou a categoria de um template, e
mandou pelo canal: *18 eventos, cinco rebaixamentos, janelas de 14h a 22h*. O número entrou em
`docs/CONTRATO-CONSUMIDOR.md` no mesmo dia, com atribuição, com a ressalva de amostra pequena — e
**commitado e empurrado**.

Estava errado. A consulta tinha rodado com `| tail -40`, a saída tinha mais linhas que isso, e quem
mediu **contou o que sobrou como se fosse o total**. Os números certos: **35 eventos**, **16**
mudanças para MARKETING, e janelas indo a **512 h**. *Um `tail` é um filtro; foi tratado como
relatório.*

*Custo real: um contrato de consumidor com número falso, no `origin`, por 27 minutos — e a correção
veio da própria origem, não de nós. Se ela não tivesse voltado a conferir, o número estaria lá até
hoje, com cara de medição.*

**O que faltou do lado de quem PUBLICOU, que é a metade acionável:** a doutrina do canal já manda
*"meça, não estime — e diga o comando"*, e ela foi lida como uma obrigação de **quem mede**. É
também de **quem publica**: chegou número sem o comando ao lado, e ninguém pediu o comando. **Pedir
custa uma linha; o `| tail -40` estaria visível nela.**

**Regra:** número de terceiro que vira documento durável entra com o **comando que o produziu**
anexado — ou não entra. E quando o comando não puder vir, o documento diz *"reportado por X, sem
verificação independente"*, que é uma afirmação diferente e honesta.

⚠️ **A parte que quase passou batido, e é a mais cara:** os números errados eram os que **favoreciam
menos** a conclusão. A correção *fortaleceu* o argumento (16 rebaixamentos em 25 dias, não 5 num
mês), e um erro que empurra na direção que você já queria é o que menos convida à conferência. *A
revisão de número não pode depender de o número incomodar.*

🔥 **2026-08-29, MESMA série, MESMA origem: a correção de 28/08 arrumou a CONTAGEM e deixou a
CONCLUSÃO em pé — e a conclusão é que estava errada.** A origem voltou ao `payload_cru` dos mesmos 35
eventos e mediu que os rebaixamentos acontecem **de 1 a 13 minutos depois da CRIAÇÃO** do template, e
que **nenhuma** volta para `UTILITY` partiu da Meta — todas são levas de um humano pedindo pelo menu
do WhatsApp Manager. Ou seja: o mecanismo que o `CONTRATO-CONSUMIDOR.md` descrevia — *a Meta rebaixa
template já aprovado por conta própria* — nunca existiu, e o bloco ainda oferecia quatro janelas
(14,8 h / 14,9 h / 22,1 h / 22,2 h) como sendo **"o número da Meta"**.

*Custo: um mês com o mecanismo errado no documento que os consumidores leem — atravessando uma
correção que passou ao lado dele.*

**A parte acionável, que é diferente da regra acima:** quando um número de terceiro é corrigido, a
revisão tem de subir do número para **a frase que ele sustentava**. Corrigir `18 → 35` e reimprimir a
mesma conclusão dá ao texto errado uma **segunda assinatura de conferido** — e a segunda é mais
difícil de derrubar que a primeira, porque agora parece revisada.

⚠️ **E um segundo custo, no mesmo dia e menor, mas da mesma família:** o commit `ee4f0c7` anunciou na
mensagem que *"ARMADILHAS ganha o custo novo"*. **Não ganhou** — o script que faria a inserção abortou
por âncora inexistente (a frase quebrava em duas linhas), o `git add` do arquivo intacto não somou
nada, e o commit saiu assim mesmo. **Mensagem de commit é documento**, e essa mentiu por um push.
*Quando um commit promete tocar dois arquivos, confira `git show --stat` antes de empurrar — o
`git status` limpo depois do commit não distingue "gravei" de "não havia o que gravar".*


### 🔥 O planner enfileirou uma tarefa enquanto um implementador estava em voo — e o commit da tarefa ANTERIOR apagou a nova, pela mão do próprio planner (2026-08-28)

Sequência real, em sete minutos:

1. `11:36` — o planner acrescenta a **T-174** a `docs/TASKS.md` e commita (`b0ce5e7`). Havia um
   implementador rodando a **T-173** na mesma árvore desde `11:20`.
2. `11:43` — o implementador termina, e **retira a T-173** do `docs/TASKS.md` — a partir da cópia que
   ele tinha lido **antes** das `11:36`. A gravação dele leva junto a T-174.
3. O planner confere o diff, vê o que esperava (a T-173 saindo, o bloco de retomada atualizado),
   commita (`ff1da91`) e **empurra**.

O `git show ff1da91 -- docs/TASKS.md | grep '^-## \[ \]'` mostra **duas** tarefas removidas. Só uma
tinha sido feita.

*Custo real: quase zero, e por um motivo que não se repete sozinho — **o implementador seguinte foi
procurar a spec que não estava lá**, recuperou-a de `git show b0ce5e7` e executou a tarefa inteira,
em vez de responder "não há tarefa na fila". Sem essa iniciativa, a T-174 teria evaporado com o
`git status` limpo, o push verde e a fila parecendo correta.*

**Por que o diff não salvou:** o planner leu o diff de `docs/TASKS.md` procurando **o que esperava
encontrar** — a tarefa concluída saindo — e achou. Uma remoção a mais, num arquivo de centenas de
linhas onde remover tarefa é o comportamento normal, não chama atenção nenhuma. *Revisão que
confirma a hipótese não é revisão.*

**As duas regras, e a segunda é a que fecha o buraco:**

1. **Com implementador em voo, o planner NÃO edita `docs/TASKS.md`.** É o arquivo que o implementador
   vai reescrever. Enfileirar pode esperar o relatório dele; se não puder, o planner escreve a spec
   em outro lugar e a move para a fila **depois** do commit.
2. **Depois do commit do implementador, `grep` a fila pelo que você acrescentou.** Um
   `grep -c 'T-174' docs/TASKS.md` custa segundos e é o único passo que enxerga uma remoção que
   ninguém pediu.

➡️ **A generalização, e é a parte que vale para além da fila:** a doutrina desta casa já dizia que
*dois implementadores na mesma árvore se atropelam*. Este caso mostra que **o planner é um escritor
como outro qualquer** — ele não estava "só documentando", estava editando um arquivo que outro
processo tinha aberto. *Todo arquivo que um agente em voo vai reescrever é território dele até o
relatório chegar.*

---

### 🔥 Um portão que cobre um tipo de dado pessoal parece cobrir todos — e o buraco declarado foi por onde o vazamento passou (2026-08-31)

O portão de telefone (`internal/config/phones_allowlist_test.go`, T-161/T-162/T-191) é o mecanismo mais forte
deste repositório: uma allowlist que falha fechada, decodifica base64, e já pegou dado real mais de uma vez. A
presença dele na tabela de regras duras do `CLAUDE.md`, bem ao lado da linha "nenhum dado que identifique pessoa
real", lê como se a categoria inteira estivesse coberta. **Não estava.** O próprio texto da linha dizia isso, por
escrito, desde que a linha foi escrita: *"cobre telefone apenas — nome de cliente … ainda não tem portão."*

**O buraco mordeu mesmo assim.** O nome real de um cliente chegou a este repositório público — em dois documentos
(com seus pares pt-BR) e duas fixtures de teste Go — e nada falhou, porque nada estava procurando aquele formato de
dado. O que achou não foi mecanismo: foi um `git grep` manual por um nome que alguém já sabia procurar — exatamente
o modo de falha contra o qual o próprio comentário do portão de telefone avisa (*"procurar o número que você já
conhece só acha o número que você já conhece"*). Os seis arquivos afetados estão listados no registro da T-193
(`docs/CHANGELOG.md`), não repetidos aqui.

*Custo: um nome real num repositório que não tem "despublicar" — exatamente o dano que a regra de abertura deste
projeto (`CLAUDE.md`, "What this repository is") existe para evitar. Custo adicional zero só porque foi pego antes
do apaga-e-recria planejado do repositório, não porque o portão pegou.*

**Por que o buraco declarado não foi fechado antes:** nomear uma lacuna num comentário lê como diligência — parece
que o risco foi contabilizado. Não foi: um buraco escrito continua sendo um buraco. Ninguém precisou esquecer nada
para isto acontecer; a linha estava certa o tempo todo, e estar certa não é o mesmo que estar coberta.

**O conserto (T-193) deliberadamente não estende o modelo de allowlist do portão de telefone.** Um telefone pode
ser legitimamente sintético, então declará-lo e seguir em frente faz sentido. Um nome de cliente aparecendo num
repositório público nunca é legítimo, então `internal/config/names_allowlist_test.go` não tem allowlist nem isenção
por arquivo nenhuma — qualquer achado é reprovação, ponto — e a lista de agulhas mora FORA do repositório
(`ZAPGW_FORBIDDEN_NAMES` ou `~/.zapgw/forbidden-names.txt`), porque escrever os nomes proibidos dentro de um
repositório público publicaria exatamente o que o portão existe para impedir.

**A regra que generaliza:** uma tabela de regras com uma linha por tipo de dado convida a ler "a linha existe" como
"o tipo está coberto", quando a própria linha pode dizer o contrário na última cláusula. **Leia a célula inteira,
não só o visto** — e quando uma doc declara uma lacuna, trate essa declaração como uma tarefa esperando ser aberta,
não como permissão para deixá-la aberta.

---

### 🔥 Portão que falha fechado e torna o caminho legítimo impossível não protege — ensina o desvio (2026-08-31)

O hook de pre-push da T-199 (`.githooks/pre-push`) foi construído para fechar um buraco real: um telefone ou nome
de cliente introduzido no commit A e apagado de novo no commit B deixa a árvore final limpa, mas o commit A ainda
chega ao `origin` no instante em que a branch é empurrada — não existe "despublicar" num repositório público. O
conserto calculava o intervalo empurrado como `oldSha..newSha` e, quando o protocolo de pre-push do git reportava
o sha remoto como zero (nenhuma ref no remoto ainda), recusava de saída: *"nao ha base segura para calcular o
intervalo introduzido"*.

**Isso cobria todo caso, exceto o que acontece em TODA branch nova.** O primeiro push de QUALQUER ref nova —
inclusive uma so' com commits limpos — reporta sha remoto zerado, porque ainda não existe ref remota. O portão
bloqueava todas, sem exceção, sem forma de satisfazê-lo: o push que criaria a ref é o mesmo push sendo recusado.
**O único caminho restante era `git push --no-verify`** — o próprio texto do hook chama essa flag de "a única
coisa que desliga este portão". Um portão cujo único modo de falha ensina o desvio não eleva a régua; abaixa,
porque quem aprende a digitar `--no-verify` para uma branch limpa hoje digita de novo no dia em que existe mesmo
uma agulha no push.

**Medido pelo planner em 2026-08-31, com um push de verdade contra um repo bare descartável** (nunca o `origin`):
uma branch nova, com um único commit limpo, foi recusada. A mensagem de recusa dizia *"sha remoto = zeros — nao
ha base segura"* — correta como descrição do que aconteceu, inútil como descrição do que a branch continha.

**O erro de método que veio junto, e merece linha própria:** o primeiro controle do planner sobre este mecanismo
"passou" no sentido de que o push foi bloqueado — mas pelo motivo errado. Recusa por sha zerado não é evidência de
que o portão acha uma agulha; é só evidência de que ele recusa. *Um bloqueio que não distingue "achei a agulha" de
"não consegui verificar" prova que o instrumento se recusa, não que ele olha.* É a mesma confusão contra a qual os
portões de telefone/nome se protegem do lado do dado (`docs/ARMADILHAS.md`, "não consegui verificar" vs. "não
achei nada") — desta vez aparecendo no autoteste do próprio portão.

**O conserto (T-200) não adivinha merge-base nem relaxa o modo de falha — troca a fórmula que calcula o
intervalo.** `git rev-list <sha-novo> --not --remotes` é exatamente "todo commit alcançável por esta ref que
nenhuma ref de rastreamento remoto que este repositório já conhece alcança" — computável sem supor de qual branch
a ref nova saiu, e se reduz sozinho a "varra todo commit alcançável a partir do `HEAD`" quando o repositório não
tem ref de rastreamento remoto nenhuma (`--remotes` então não casa nada, então `--not --remotes` não exclui nada)
— o fallback seguro e mais lento que a tarefa exigia, em vez de tratar "não consigo calcular o intervalo esperto"
como "deixa passar". Provado contra dado real, não afirmado: uma branch nova e limpa agora empurra; uma branch cujo
commit A introduz uma agulha e o commit B apaga o arquivo de novo continua bloqueando, e a mensagem cita o commit A
e o arquivo, não só "bloqueado" (`internal/config/prepush_test.go`,
`TestPrePushGateNewRefCleanBranchPasses` / `TestPrePushGateNewRefBlocksNeedleDeletedLater` /
`TestPrePushGateNewRefNoRemoteAtAllSweepsEverything`).

**A regra que generaliza:** quando a única saída de um portão que falha fechado é "desligar o portão", o portão
está mal dimensionado, não apenas rigoroso — rigor sem caminho legítimo nenhum é, na prática, indistinguível de
portão nenhum, porque a disciplina de usar a saída de emergência apodrece no instante em que ela vira rotina.

---

### 🔥 Zero commits nem sempre é "não consegui medir" — pode ser a resposta certa, e o portão não sabia disso (2026-08-31)

A T-200 trocou uma recusa geral em toda ref nova por uma fórmula (`git rev-list <sha-novo> --not --remotes`) que
calcula corretamente o que um primeiro push realmente introduz. O que ela não mudou foi o que `TestPrePushGate`
fazia com um resultado VAZIO: `t.Fatalf`, sem condição, na teoria de que "um push com algo a empurrar sempre tem
ao menos um commit; zero é sinal de que a medição está vazia." Essa teoria é falsa exatamente no caso de que este
projeto depende em TODO lançamento: **empurrar uma tag anotada sobre um commit que já chegou ao `origin`** — o
fluxo comum de mesclar no `main` e só depois marcar aquele mesmo commit com uma tag. A ref da tag é nova (sha
remoto zero), então `commitsIntroducedByNewRef` descasca a tag e corretamente não acha nada NOVO — o push
acrescenta um ponteiro, não um commit. O portão não conseguia distinguir isso de "eu não sei o que este push
carrega", e recusava os dois igualmente.

**Medido direto, não inferido:** o planner marcou `v0.61.0` sobre um commit já mesclado no `main` e empurrou. O
hook recusou com *"o intervalo 0000...\<sha\> nao contem nenhum commit novo (falha fechada)"* — correto como
descrição do número, errado como veredito, porque este projeto **lança por tag**. Sem outro caminho para
publicar um release, a única saída era `git push --no-verify` — exatamente o desvio que o próprio post-mortem da
T-200 (a entrada acima desta) já tinha nomeado como o modo de falha a evitar. Segunda vez em 24 horas que este
mesmo defeito aparece com outra roupa de ref.

**O conserto (T-204) não afrouxa o modo de falha — ele faz a pergunta certa quando a contagem é zero.**
`objectAlreadyReachableFromRemotes` responde "o objeto empurrado (descascado de qualquer tag) já é ancestral de
alguma ref de rastreamento remoto que este repositório conhece?" — o que, para a fórmula
`commitsIntroducedByNewRef`, é matematicamente a MESMA condição que produziu a lista vazia em primeiro lugar
(`git rev-list X --not --remotes` fica vazio se e somente se X é alcançável a partir de `--remotes`). Isso não é
um segundo checagem, mais frouxa, colada ao lado da primeira; é a mesma pergunta, feita numa forma sobre a qual o
teste pode decidir em vez de só falhar. Uma lista de commits zero que volta "não alcançável" — combinação que
nunca deveria surgir legitimamente — continua falhando fechado exatamente como antes.

**A parte que ninguém pensou em checar, e que o Verify desta tarefa exigiu como controle positivo:** um objeto de
tag carrega sua própria MENSAGEM em texto livre, escrita por um humano, e esse texto chega ao `origin` num push de
tag independente de a tag trazer algum commit. Antes da T-204 nada olhava para ela — o portão varria arquivos,
nunca o próprio objeto de uma tag. `isAnnotatedTagObject` / `annotatedTagMessage` / `sweepTagMessage` agora varrem
esse texto sem condição, com as mesmas duas funções usadas no resto deste arquivo, e `annotatedTagMessage`
deliberadamente descarta o bloco de cabeçalho do `git cat-file -p` (que sempre inclui uma linha real `tagger Nome
<email>`) antes de varrer — varrer o cabeçalho cru teria transformado toda tag que o próprio mantenedor deste
projeto cria num falso positivo garantido no portão de nome.

Provado contra dado real, nas duas direções, na mesma sessão: `git push` da tag `v0.61.0` de verdade contra um
repo bare descartável, e depois contra o `origin`, os dois com sucesso e o portão registrando "isto e' legitimo"
em vez de recusar; uma segunda tag anotada cuja MENSAGEM (não um arquivo, não um commit) carregava uma agulha,
empurrada para o mesmo bare descartável, foi BLOQUEADA citando a mensagem da tag e a agulha exata
(`internal/config/prepush_test.go`, `TestPrePushGateAnnotatedTagOnPublishedCommitPushesClean` /
`TestPrePushGateBlocksNeedleInTagMessageEvenWithZeroCommits` /
`TestObjectAlreadyReachableFromRemotesFalseForUnpublishedCommit`).

**A regra que generaliza, e é a regra da T-200 de novo, mais afiada:** um portão que falha fechado e trata "zero"
como uma única falha indiferenciada está medindo menos do que pensa. Antes de escrever `if contagem == 0 {
falha }`, pergunte se zero tem mais de uma causa legítima — e se tiver, o portão precisa de um segundo sinal para
separá-las, não de uma definição mais larga de "vazio".

---

### 🔥 Um portão que lê o diff de um commit pode ficar cego para uma classe inteira de commit — e ficou, para merges (2026-08-31)

O portão de pre-push da T-199 materializa exatamente o que CADA commit no intervalo empurrado introduz, via `git
diff-tree`, e varre esse conteúdo atrás de telefone ou nome de cliente. O comentário da própria função já
nomeava o limite: `git diff-tree` sem `-m`/`-c` só calcula diff para commit de UM pai — passe um commit de merge
e ele devolve diff VAZIO, incondicionalmente, independente do que a árvore daquele merge realmente contém.
**Conteúdo que existe só na resolução de conflito de um merge — presente em nenhum dos dois pais — atravessava
o portão completamente sem ser olhado**, porque a função que lista "o que este commit mudou" reportava que nada
tinha mudado.

**Declarar o buraco num comentário não é o mesmo que fechá-lo**, e este projeto já tinha pago exatamente por
isso uma vez (o portão de telefone da T-193 cobria `docs/` no nome, mas não no código que decidia quais arquivos
varrer — ver a entrada acima desta). Escrever "esta é uma limitação conhecida" lê como diligência devida até o
momento em que alguém resolve um conflito colando um valor direto de um chamado de suporte — e é exatamente
nesse momento que um merge existe.

**Medido antes de escolher entre `-m` e `-c` (T-201, item 2 do Do), contra um merge REAL construído num clone
descartável deste repositório, nunca o `origin`:**

- Um merge limpo (duas branches tocando arquivos disjuntos, git resolve sozinho, sem conflito): `git diff-tree
  -m` reportou **12 arquivos** — a concatenação de tudo que QUALQUER uma das duas branches tocou, porque `-m`
  compara o commit de merge com cada pai SEPARADAMENTE e inclui os dois resultados. `git diff-tree -c` (diff
  combinado) reportou **0 arquivos** — corretamente, porque o conteúdo final de cada arquivo bate trivialmente
  com um dos dois pais, e o commit que produziu aquele pai já está em `commits` e é varrido por conta própria.
- Um merge que resolve um conflito DE VERDADE (as duas branches editam a mesma linha do mesmo arquivo; o texto
  da resolução não existe em NENHUM dos dois pais): tanto `-m` quanto `-c` reportaram o mesmo **1 arquivo** — o
  único que realmente precisava de resolução.

**`-m` não cobre mais do que `-c` aqui — ele reinspeciona conteúdo que a varredura por commit já tinha olhado,
e o trabalho redundante escala com o TAMANHO das duas branches, não com o tamanho da resolução.** A regra do
`-c` para omitir um arquivo ("idêntico a um dos pais, logo trivial") não consegue esconder uma agulha deste
portão: um arquivo que o `-c` pula porque bate com o pai P foi introduzido por qualquer commit que construiu a
árvore de P, e esse commit ou já está na lista varrida (como mais ele seria pai dentro deste push?) ou já
estava público antes deste push existir. De um jeito ou de outro, algo já olhou para ele — o `-c` nunca remove
o ÚNICO olhar, só um segundo olhar redundante.

**O conserto (T-201) faz `filesChangedInCommit` decidir pelo número de pais**: 0 ou 1 pai mantém o diff
original de pai único (com `--root` para o commit genesis); 2+ pais muda para `git diff-tree -c`. Provado contra
dado real, não afirmado: `TestPrePushGateBlocksNeedleOnlyInMergeResolution` constrói um merge com conflito de
verdade, com uma agulha que provadamente não existe em nenhum dos dois pais, e o bloqueio nomeia o próprio
commit de merge e o arquivo — não um commit que só "parece bloqueado", do jeito que o post-mortem da própria
T-200 (a entrada acima desta) alertou. `TestPrePushGateCleanMergeOnMainPasses` é o controle negativo
correspondente: um merge limpo continua passando, em bem menos de um segundo.

**A regra que generaliza:** um comentário que nomeia um buraco com precisão não é uma mitigação — é uma
descrição de exposição sem data de vencimento marcada por ninguém. O buraco fecha quando o código muda, não
quando o risco é escrito; e quando duas flags do git poderiam responder "o que este commit introduziu",
meça as duas contra um fixture construído a partir de uma operação REAL (um `git merge` de verdade, não um diff
sintético) antes de escolher a que parece mais barata.

