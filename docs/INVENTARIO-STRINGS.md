Código: cmd/zapgw/, internal/outbound/, internal/config/, internal/meta/, internal/inbound/, cmd/grafo-falso/

# Inventário de strings de produção em português (T-213)

🔴 **Este documento MEDE. Não traduz nada.** Cada linha abaixo tem uma classificação obtida
seguindo o caminho real do valor até um `w.Write`/`json.Encode` (SAIDA-CONSUMIDOR) ou até um
`log.`/CLI stdout (LOG) — nunca por aparência da string. Onde o caminho não foi fechado, a linha
diz `A MEDIR` e o que faltou, em vez de chutar.

## Como este documento foi medido

1. **Busca por strings acentuadas** (`á à â ã é ê í ó ô õ ú ç` e maiúsculas) em todo `.go` de
   `cmd/` e `internal/`, excluindo `_test.go`. Deu 216 ocorrências brutas; **52 delas eram texto
   dentro de comentário** (ex.: `// "TLS — não existe modo desligado"`, citando o `CLAUDE.md`), não
   string de código — removidas depois de conferir cada uma a olho. Sobraram **164 literais de
   código reais**, cada uma rastreada individualmente até o sink.
2. **Achado central que muda a medição inteira: a maioria das strings de produção deste projeto
   NÃO usa acento** (`"invalido"`, `"obrigatorio"`, `"instancia desconhecida"`, `"corpo grande
   demais"` — tudo ASCII, de propósito, aparentemente). Uma contagem só por acento sub-mede
   pesadamente. Para cobrir isso, tracei TODOS os call sites de `respondError`,
   `respondErrorWithDetail`, `respondMetaError` e `logRejection` em `internal/outbound` (~226
   chamadas) e o corpo inteiro de `internal/outbound/message.go` (a função `Request.Validate()`,
   que sozinha tem **103 pontos distintos de `fmt.Errorf`/`errors.New` em português**, nenhum
   acentuado).
3. **cmd/zapgw e cmd/grafo-falso são um binário à parte do consumidor.** `cmd/zapgw/main.go`
   confirma por comentário: sem argumento o binário sobe o servidor HTTP; com argumento vira
   ferramenta de CLI, e o dispatch de CLI escreve em `out io.Writer` (stdout do operador) — nunca
   no `http.ResponseWriter` do consumidor. `cmd/grafo-falso` é uma Graph API FALSA de laboratório
   que "NÃO VAI PARA PRODUÇÃO" (comentário no topo do arquivo). Por isso **toda string desses dois
   binários é LOG (saída de operador/laboratório) por construção**, sem precisar rastrear cada
   uma — e nenhuma delas pode ser SAIDA-CONSUMIDOR.

## Números

| classificação | strings únicas (padrões de mensagem) | observação |
|---|---|---|
| **SAIDA-CONSUMIDOR** (só) | **~44** | ver tabela 2 |
| **AMBOS** | **~132** | ver tabela 3 — 103 delas vêm de UMA função (`message.go: Request.Validate()`) |
| **LOG** (internal, não-CLI) | **~40** | ver tabela 4 |
| **LOG** (CLI/laboratório — `cmd/zapgw`, `cmd/grafo-falso`) | **98 confirmadas com acento** + volume ASCII não contado (não afeta o número acima porque é estruturalmente impossível chegar ao consumidor — ver item 3) | ver tabela 5 |
| **Nunca impressa** (nem log, nem resposta — achado à parte) | 7 | ver seção própria |
| **A MEDIR** | ver seção própria | funções de erro específicas de alguns handlers não varridas linha a linha até o fim |

🔴 **O número que decide o tamanho real do problema é SAIDA-CONSUMIDOR + AMBOS: ~176 padrões de
mensagem que, se traduzidos sem cuidado, mudam um valor que o consumidor pode estar comparando.**
Isso é MAIOR que os 207 originalmente estimados — não menor —, e a explicação é o item 2 acima: os
207 pareciam contar por acento, e a maior fonte de strings deste projeto (`message.go`) não usa
acento nenhum. A reconciliação completa está na última seção.

---

## Tabela 1 — famílias grandes, para não repetir 108+ linhas quase idênticas

Estas mensagens se repetem, **literalmente**, em várias rotas (cada handler HTTP tem sua cópia da
mesma checagem de auth/store/corpo). Contá-las por ocorrência infla a tabela sem ajudar quem vai
decidir o que traduzir — a decisão é por TEXTO, não por lugar onde ele aparece.

| texto (literal) | classificação | ocorrências | arquivo:linha (primeira ocorrência) |
|---|---|---|---|
| `"indisponivel"` | SAIDA-CONSUMIDOR | 25 | `internal/outbound/block_handler.go:214` |
| `"token ausente ou invalido"` | SAIDA-CONSUMIDOR | 12 | `internal/outbound/block_handler.go:210` |
| `"instancia pausada"` | SAIDA-CONSUMIDOR | 8 | `internal/outbound/block_handler.go:281` |
| `"esta instancia nao existe mais no gateway"` | SAIDA-CONSUMIDOR | 2 | `internal/outbound/pause_handler.go:138` |
| `"parametro limit tem de ser um inteiro positivo"` | SAIDA-CONSUMIDOR | 1 | `internal/outbound/block_handler.go:424` |
| `"envio com esta chave em andamento"` | SAIDA-CONSUMIDOR | 1 | `internal/outbound/handler.go:535` |
| `"instancia desconhecida"` | 🔴 AMBOS | 9 | `internal/outbound/block_handler.go:273` (resp) + `handler.go:470` (log, mesmo texto) |
| `"corpo nao foi lido por inteiro"` | 🔴 AMBOS | 8 | `internal/outbound/block_handler.go:227,228` |
| `"corpo nao e JSON valido"` | 🔴 AMBOS | 8 | `internal/outbound/block_handler.go:244,245` |
| `"corpo grande demais"` | 🔴 AMBOS | 8 | `internal/outbound/handler.go:387,388` |
| `"parametro instancia e obrigatorio"` | 🔴 AMBOS | 2 | `internal/outbound/block_handler.go:382,383` |
| `"instancia nao autorizada para este consumidor"` | 🔴 AMBOS | 9 | resp em 9 handlers; log verbatim em `media_handler.go:159` |
| `"campo instancia e obrigatorio"` | 🔴 AMBOS | 1 | `internal/outbound/profile_handler.go:190,191` |
| `"header Idempotency-Key e obrigatorio"` | 🔴 AMBOS | 1 | `internal/outbound/handler.go:378,380` |

**Como AMBOS foi decidido, não suposto:** para cada linha marcada 🔴 AMBOS eu achei, com `grep`, um
`logRejection(...)` OU um `log.Printf(...)` em algum lugar do pacote passando **o mesmo texto
literal**, e um `respondError`/`respondErrorWithDetail` em outro (ou no mesmo) lugar passando
**o mesmo texto**. Onde só achei o texto em um dos dois lados, a linha ficou SAIDA-CONSUMIDOR ou
LOG (nunca "AMBOS por parecer perigoso").

## Tabela 2 — SAIDA-CONSUMIDOR (não repetidas na tabela 1)

Cada uma foi confirmada indo até um `respondError`/`respondErrorWithDetail`/`respondMetaError`
OU até um campo de struct com `json:"..."` que é escrito na resposta — e conferindo que **não**
existe um `log.`/`logRejection` em nenhum lugar do pacote com o mesmo texto literal.

| arquivo:linha | conteúdo (resumo) | onde entra na resposta |
|---|---|---|
| `internal/outbound/block_handler.go:359` | "falha ao falar com a Meta; a operacao pode nao ter acontecido..." | `respondError` |
| `internal/outbound/block_handler.go:340-341` | "a configuracao desta instancia no gateway esta invalida; o pedido nao foi enviado a Meta..." | `respondError` (`respondBlockError`) |
| `internal/outbound/handler.go:494-496` | "esta instancia e Instagram; nesta fase o gateway so envia tipo \"texto\"..." | `respondError` |
| `internal/outbound/leadership.go:266` | "esta instancia do gateway nao detem a lideranca do par..." | `respondError` |
| `internal/outbound/media_handler.go:436` | "a Meta respondeu sem media_id; a midia pode ter subido..." | `respondError` |
| `internal/outbound/media_handler.go:410-411` | "identificador com forma invalida; o pedido nem chegou a sair do gateway" | `respondError` (`respondMediaError`) |
| `internal/outbound/media_handler.go:418` | "a Meta nao devolveu um endereco utilizavel para esta midia" | `respondError` |
| `internal/outbound/media_handler.go:214,216` | "mime_do_payload nao e um mime valido; mande o valor..." | `respondError` |
| `internal/outbound/media_handler.go:332-333` | "acima do teto do gateway para a categoria ...; o teto e NOSSO, nao da Meta" | `respondError` (`respondCapError`) |
| `internal/outbound/profile_handler.go:288-289` | "a configuracao desta instancia no gateway esta invalida; o pedido nao chegou a Meta..." | `respondError` |
| `internal/outbound/profile_handler.go:292-293` | "a Meta respondeu sobre o perfil algo que o gateway nao entendeu; tente de novo" | `respondError` |
| `internal/outbound/profile_handler.go:307-308` | "nao foi possivel falar com a Meta sobre o perfil; tente de novo" | `respondError` |
| `internal/outbound/reads_handler.go:289` | "falha ao falar com a Meta; a marcacao pode nao ter acontecido..." | `respondError` |
| `internal/outbound/registration_handler.go:322` (campo `NextStep`) | "esta instancia continua PAUSADA: enquanto isso, o webhook responde 503..." | campo do corpo 200, `json:"proximo_passo"`-like — struct `RegistrationResponse` |
| `internal/outbound/registration_handler.go:346-351` | mensagem completa de janela de cadastro FECHADA | `respondError` (caso `ErrRegistrationWindowClosed`, log usa texto DIFERENTE) |
| `internal/outbound/smoke_handler.go:265-267` | "a mensagem de teste foi enviada e aceita pela Meta, mas o gateway falhou ao marcar..." | `respondError` |
| `internal/outbound/state.go:556-557` (const `InstructionIGTokenFailing`) | "a renovacao automatica esta falhando; a resolucao e MANUAL..." | campo de instrução no `/v1/estado` |
| `internal/outbound/state.go:558-559` (const `InstructionIGTokenExpired`) | "o token venceu e nao ha renovacao automatica possivel..." | idem |
| `internal/outbound/state_handler.go:179` | "`serie_dias` = %q: tem de ser um inteiro de 1 a %d" | `respondError` (via `err.Error()`, sem log pareado) |
| `internal/outbound/state_handler.go:184-186` | "`serie_dias` = %d, mas este gateway guarda contador por %d dias..." | idem |
| `internal/outbound/types.go:122-126` | "esta rota nao se aplica a instancias do tipo %q" + `guidance` | `respondError` (`checkType`, usado por vários handlers) |
| `internal/outbound/templates_handler.go:1023-1025` | "o catalogo desta instancia nao coube no limite de paginacao..." | `respondError` |
| `internal/outbound/templates_handler.go:1037-1039` | "a paginacao da Meta apontou para um destino inesperado..." | `respondError` |
| `internal/outbound/templates_handler.go:1040-1041` | "a Meta respondeu um catalogo que o gateway nao entendeu; nenhuma lista parcial e devolvida" | `respondError` |
| `internal/outbound/templates_handler.go:53-63` (const `WarningTemplatePending`) | "template recem-criado NAO pode ser usado na hora..." | campo `Warning` do 201 |
| `internal/outbound/templates_handler.go:64-72` (const `WarningCreationConfirmedByReread`) | "a criacao terminou SEM resposta da Meta, mas o gateway releu..." | campo `Warning` |
| `internal/outbound/templates_handler.go:96-102` (const `MessageInconclusiveOutcome`) | "a criacao do template terminou sem resposta da Meta..." | campo `Message` do erro 502 |
| `internal/outbound/templates_handler.go:103-109` (const `MessageUnknownOutcome`) | "falha ao falar com a Meta; o template PODE ter sido criado..." | idem |
| `internal/outbound/templates_handler.go:123-125` (const `WarningCategoryChangedFormat`) | "a categoria PEDIDA foi %q, mas a Meta GRAVOU %q..." | campo `Warning` |
| `internal/outbound/templates_handler.go:177-186` (const `WarningNameBurnedForThirtyDays`) | "a exclusao apaga o template em TODOS os idiomas..." | campo `Warning` da exclusão |
| `internal/outbound/templates_handler.go:187-198` (const `WarningStillInCatalogPendingDelivery`) | "este template CONTINUA aparecendo no catalogo com status..." | campo `Warning` |
| `internal/outbound/templates_handler.go:199-205` (const `MessageInconclusiveDeletion`) | "a exclusao do template terminou sem resposta da Meta..." | campo `Message` do erro |
| `internal/outbound/templates_handler.go:206-212` (const `MessageUnknownDeletion`) | "falha ao falar com a Meta; o template PODE ter sido apagado..." | idem |

**Achado que ninguém esperaria** (pedido explícito da tarefa): a `registration_handler.go:322`
(`NextStep`) e as nove `const Warning*`/`Message*` de `templates_handler.go` **não são mensagens de
erro** — são texto instrutivo dentro de um **corpo de SUCESSO** (200/201). Quem procura strings
para traduzir tende a olhar só `respondError`; essas ficam fora da varredura óbvia porque vivem em
`struct` literals e `const` no topo do arquivo, viajam por `resp.Warning`/`resp.Message`, e só
aparecem no bytes da resposta em caminhos de sucesso-com-ressalva (template criado mas com aviso,
instância registrada mas ainda pausada). São exatamente o tipo de string que "parece documentação"
e na verdade é contrato.

## Tabela 3 — AMBOS (fora da família da tabela 1)

| origem | quantas | como foi confirmado |
|---|---|---|
| `internal/outbound/message.go`, função `Request.Validate()` — **103 pontos distintos** de `fmt.Errorf`/`errors.New`, linhas 27-31,35,43,48,56,60,64,73,77,95,100,107,112,693-1756 (lista completa em `git grep -n "fmt.Errorf(\|errors.New(" internal/outbound/message.go`) | 103 | Confirmado em 4 call sites diferentes (`handler.go:432`, `block_handler.go:248`, `reads_handler.go:188`, `templates_handler.go:503`) que TODOS fazem `logRejection(..., err.Error())` seguido de `respondError(..., err.Error(), 0)` — o MESMO `err.Error()` cru nos dois lados. `handler.go` tem uma exceção PARCIAL: `safeRejectionMessage()` troca 4 dessas 103 (as que citam o valor recusado via `%q`) por um texto fixo mais curto ANTES de logar — mas só nesse UM caminho; `block_handler.go`/`reads_handler.go`/`templates_handler.go` logam o `err.Error()` cru mesmo para essas 4, então mesmo essas continuam AMBOS pelo menos por um caminho. |
| `internal/outbound/templates_handler.go`, `CreateTemplateRequest.Validate()`, linhas 921,923,925,927,935 | 5 | Mesmo padrão, confirmado em `templates_handler.go:503-508`. |
| `internal/outbound/block_handler.go`, `BlockRequest.Validate()` (`ErrBlockNoInstance`, `ErrBlockNoPhones`, + 2 `fmt.Errorf` de `telefones`), linhas 110,111,128,134 | 4 | Confirmado em `block_handler.go:248-250`. |
| `internal/outbound/reads_handler.go` (`ErrReadNoInstance`, `ErrReadNoWamid`), linhas 107,108 | 2 | Confirmado em `reads_handler.go:188-194`. |
| `internal/outbound/pause_handler.go` (`ErrPauseNoInstance`), linha 66 | 1 | Mesmo padrão (`pause_handler.go`, não relido linha a linha — ver A MEDIR). |
| `internal/outbound/smoke_handler.go` (`ErrSmokeRequestNoInstance`, `ErrSmokeRequestNoDestination`), linhas 101,102 | 2 | Confirmado em `smoke_handler.go:178-180`. |
| `internal/outbound/input_aliases.go` (`ErrConflictingAlias`), linha 48 | 1 | Confirmado em `handler.go:410-414`. |
| `internal/outbound/media_handler.go` (multipart: "o corpo precisa ser multipart/form-data..." e "nao veio a parte ... com os bytes"), linhas ~201,208 | 2 | Confirmado: `logRejection`/`respondError` lado a lado com o MESMO texto concatenado. |
| `internal/config/store.go`, `ValidateIdentification`, linha 186 | 1 | Só alcançável via `RegisterMeta` (consumidor, `POST /v1/cadastro`); `respondRegistrationError` loga E responde `err.Error()` no caso `ErrIncompleteIdentification` (`registration_handler.go:356-364`). |
| `internal/config/store.go`, `ValidateCallbackURL`, linha 274 | 1 | Alcançável por `RegisterMeta` (consumidor) E por `CreateInstanceAt`/`RotateInstance` (CLI) — a MESMA string aparece nos dois lados; do lado consumidor é AMBOS pelo mesmo mecanismo do item acima. |
| `internal/config/store.go`, `ValidateCABundle`, linha 312 | 1 | Mesmo caminho que `ValidateCallbackURL`. |
| `internal/config/store.go`, `ValidateMetaRegistration` (loop de `numero_exibido`/`app_secret`/`token_envio`), linha 1056 | 1 (3 instanciações) | Mesmo caminho — caso `ErrIncompleteRegistration` no switch de `registration_handler.go`. |

## Tabela 4 — LOG (dentro de `internal/`, não é CLI)

Confirmado que o texto só aparece em `log.Printf`/`log.Print`/`log.Fatalf` — nunca em
`respondError`/`respondErrorWithDetail`/`respondMetaError` nem em campo de struct de resposta.

| arquivo:linha | conteúdo (resumo) | por que é LOG |
|---|---|---|
| `internal/outbound/block_handler.go:339-341` (log) | "ALARME zapgw: phone_number_id invalido..." | log ALARME; a resposta ao consumidor usa texto FIXO diferente |
| `internal/outbound/handler.go:492-493` (log) | "instancia Instagram so aceita tipo texto nesta fase" | `logRejection`; resposta usa texto mais longo, diferente |
| `internal/outbound/handler.go:593-596` | "ALARME zapgw: idempotencia nao liberada apos falha..." | log ALARME |
| `internal/outbound/handler.go:666-669` | "ALARME zapgw: a Meta aceitou o envio... mas message_status..." | log ALARME |
| `internal/outbound/handler.go:688-691` | "ALARME zapgw: phone_number_id invalido..." | log ALARME |
| `internal/outbound/ingress.go:112-114` | "%s = %q nao e um caminho de entrada conhecido..." | erro de **arranque** — `log.Fatalf` em `main`, processo nem sobe |
| `internal/outbound/instagram_renewer.go:281-330` (5 pontos) | avisos de renovação de token do Instagram | todos em `log.Printf`, laço de fundo (watchdog), nunca respondidos |
| `internal/outbound/leadership.go:146-163` | erro de configuração da guarda de liderança | `NewLeadership` só é chamada em `cmd/zapgw/main.go:309` — arranque |
| `internal/outbound/leadership.go:194-196` | "a concessao de lideranca em %s esta velha..." (`reason`) | só chega a `logLeadershipRefusal`; a resposta ao consumidor usa texto fixo diferente |
| `internal/outbound/profile_handler.go:285-287` | "ALARME zapgw: identificador de perfil invalido..." | log ALARME |
| `internal/outbound/reads_handler.go:266-268` | "ALARME zapgw: phone_number_id invalido..." | log ALARME |
| `internal/outbound/smoke.go:168-169,201-205` | textos de progresso do fumaça ("passo N/4...") | o parâmetro `report` chega `nil` na rota HTTP (`smoke_handler.go:205`) — só existe quando chamado por `cmd/zapgw fumaca` (CLI) |
| `internal/outbound/smoke.go:65` (`ErrSmokeNoSlug`, `ErrSmokeNoDestination`) | | a rota HTTP valida `instancia`/`destino` ANTES de chamar `SmokeWithInstagramBase` (com sentinelas próprias, tabela 3) — este caminho só é alcançado pela CLI |
| `internal/outbound/smoke_handler.go:279-281` | "ALARME zapgw: phone_number_id invalido... ao rodar o fumaca" | log ALARME |
| `internal/outbound/state.go:616-628` (4 `fmt.Errorf`) | erros de montagem do `/v1/estado` | só chegam a `log.Printf("zapgw: erro ao montar o estado...")`, `state_handler.go:271`; a resposta usa `"indisponivel"` fixo |
| `internal/outbound/state.go:868` (const `NoValue = "—"`) | | usada só por `StateRow`/`StateRows`, que só `cmd/zapgw/state.go` (CLI) consome — não aparece no JSON de `/v1/estado` |
| `internal/outbound/templates_handler.go` (13 pontos: 665,706,810,867,875,886,894,1019,1028,1077,1145,1155,1165,1191) | logs de auditoria de criação/exclusão/releitura de template | todos `log.Printf`, sem `respondError` pareado |
| `internal/outbound/watchdog.go:313-316` | "ALARME zapgw: a Graph API recusou a leitura dos campos..." | log ALARME |
| `internal/inbound/billing.go:189-192` | "ALARME zapgw: instancia... recebeu cobranca da Meta na categoria..." | log ALARME |
| `internal/inbound/mirror.go:81-83` (campo `Reason` de `Verdict`) | | só vai para `log.Printf` e para o log de trânsito (`recordTransit`), lido por `zapgw transito`/`zapgw log` (CLI) — não há rota HTTP que exponha o log de trânsito ao consumidor |
| `internal/config/store.go:104-131` (`ValidateInstanceType`), linhas 119,124 | | só chamada por `CreateInstanceAt`/`RotateInstance`, ambos só usados por `cmd/zapgw/provision.go` (CLI) |
| `internal/config/store.go:215-233` (`ValidateSlug`), linhas 222,227 | | só chamada por `CreateInstanceAt` (CLI) e diretamente por `provision.go` |
| `internal/meta/registration.go:46` (`ErrInvalidPin`) | | `Register`/`SetPin` só chamados por `cmd/zapgw/provision.go` (CLI) |

## Tabela 5 — LOG (CLI/laboratório, `cmd/zapgw` e `cmd/grafo-falso`)

Estruturalmente impossível chegar ao consumidor (ver item 3 da metodologia): `cmd/zapgw` roteia
por `dispatch(args, out io.Writer, env)` (CLI) ou pelo servidor HTTP com NENHUM texto duro destes
arquivos — os textos abaixo vivem em `provision.go`, `menu.go`, `diagnostics.go`, `log.go`,
`transit.go`, `smoke.go`, `lost.go`, `state.go`, `template.go`, `main.go`, todos sob `cmd/zapgw/`, e
em `cmd/grafo-falso/main.go` (que o próprio arquivo documenta como "NÃO VAI PARA PRODUÇÃO").

| arquivo | strings com acento confirmadas (contagem exata) | strings ASCII (não contadas — ver A MEDIR) |
|---|---|---|
| `cmd/zapgw/provision.go` | 35 | não contado |
| `cmd/zapgw/menu.go` | 22 | não contado |
| `cmd/zapgw/diagnostics.go` | 16 | não contado |
| `cmd/zapgw/log.go` | 10 | não contado |
| `cmd/zapgw/transit.go` | 3 | não contado |
| `cmd/zapgw/smoke.go` | 3 | não contado |
| `cmd/zapgw/lost.go` | 3 | não contado |
| `cmd/zapgw/state.go` | 2 | não contado |
| `cmd/zapgw/main.go` | 1 | não contado |
| `cmd/zapgw/template.go` | 0 (na busca por acento) | não contado |
| `cmd/grafo-falso/main.go` | 3 | não contado |
| **total** | **98** | — |

Amostra conferida com `sed -n`: `cmd/zapgw/provision.go:1084` (mensagem de erro de tipo de
instância, chamada por `zapgw instancia registrar`) e `cmd/zapgw/menu.go:8` (comentário, excluído
da contagem) — ambos confirmados como saída de terminal, nunca HTTP.

## "Nunca impressa" — strings construídas que não chegam a lugar nenhum

Achado incidental da tarefa: 7 strings são montadas por `fmt.Errorf`/`errors.New` e o valor
**nunca é lido** — nem por log, nem por resposta. Não são perigosas de traduzir (ninguém compara
com elas), mas valem registro porque contradizem a suposição "toda `fmt.Errorf` vai para algum
lugar".

| arquivo:linha | texto | por que nunca aparece |
|---|---|---|
| `internal/outbound/auth.go:24` (`ErrNoToken`) | "outbound: sem token Bearer" | só usada em `errors.Is(err, ErrNoToken)`; a resposta usa sempre o texto fixo `"token ausente ou invalido"` |
| `internal/outbound/auth.go:26` (`ErrInvalidToken`) | "outbound: token invalido" | idem |
| `internal/outbound/media_handler.go:456` (`errAboveCap`) | "outbound: midia acima do teto da categoria" | só usada em `errors.Is`; `respondCapError` usa texto fixo diferente, sem ler `err.Error()` |
| `internal/outbound/external_probe.go:307` | "montar o pedido a sonda externa (%s): %w" | `ask()` retorna erro para `Measure()`, que chama `record(v, err)`, que **descarta** `err` (só usa para marcar `failingSince`) — nunca logado, nunca respondido |
| `internal/outbound/external_probe.go:311` | "perguntar a sonda externa (%s): %w" | idem |
| `internal/outbound/external_probe.go:326` | "ler a sonda externa (%s, HTTP %d): %w" | idem |
| `internal/outbound/external_probe.go:329` | "a sonda externa (%s) respondeu HTTP %d sem o campo `status`" | idem |

## A MEDIR — o que esta passada NÃO fechou

Sendo honesto sobre os limites do que foi rastreado nesta sessão, em vez de estender a tabela 2/3
por extrapolação:

- **Volume ASCII exato de `cmd/zapgw`/`cmd/grafo-falso`.** A tabela 5 conta só as strings com
  acento (98). O volume sem acento existe e é grande (o padrão se repete: mensagens de CLI também
  evitam acento), mas não foi contado linha a linha porque **não muda o número que importa**
  (SAIDA-CONSUMIDOR): está estruturalmente provado que nada desses dois binários alcança o
  consumidor. Falta só para fechar o total geral de "quantas strings em português existem no
  repositório", que não é o que a tarefa pede.
- **`internal/outbound/pause_handler.go`**: classifiquei `ErrPauseNoInstance` como AMBOS por
  analogia direta com `ErrBlockNoInstance`/`ErrReadNoInstance` (mesmo padrão, mesmo autor,
  confirmado em pelo menos 4 arquivos irmãos) mas **não abri o call site desta função
  especificamente** para confirmar linha a linha.
- **Funções `respond*Error` de `templates_handler.go` ainda não lidas por completo**:
  `respondDeletionError`/`respondAmbiguousDeletion` (a partir da linha 804) e
  `respondAmbiguousOutcome` (linha 1135) foram lidas o bastante para confirmar os `const`
  Warning/Message da tabela 2, mas branches menores dentro delas (ex.: variações de `me.Class`)
  podem conter mais 1-2 strings específicas não capturadas aqui.
- **`internal/meta/`**: só 3 strings de código reais (não-comentário) foram achadas na busca por
  acento (`errors.go:206`, `registration.go:46`), e o pacote não foi varrido por ASCII da mesma
  forma exaustiva que `internal/outbound`/`internal/config` — é plausível que existam mais
  mensagens ASCII em português ali (o pacote fala com a Graph API e tem muito parsing de erro).

Nenhuma dessas lacunas muda a ORDEM DE GRANDEZA do achado principal (a maior fonte de strings
consumer-facing é `message.go`, com 103 templates AMBOS) — mas cada uma é uma decisão real que o
próximo passo (tradução) vai precisar fechar antes de tocar nesses arquivos.

## Reconciliação com os 207 originalmente estimados

**Não bate, e a diferença tem explicação, não desculpa.** A estimativa de 207 foi escrita no commit
`9bc3136` (fila do dia 2026-08-31) sem metodologia registrada. Esta medição encontrou:

- **164 strings de código com acento** (de 216 ocorrências brutas — 52 eram comentário).
- **~132 padrões distintos AMBOS + ~44 padrões distintos SAIDA-CONSUMIDOR** entre strings
  ASCII e acentuadas de `internal/outbound`/`internal/config`, dos quais **103 vêm de um único
  arquivo** (`message.go`).
- **98 strings com acento** em `cmd/zapgw`/`cmd/grafo-falso` (LOG por construção).

Somando só as linhas de código reais rastreadas (sem contar ocorrências repetidas da tabela 1 mais
de uma vez): **164 (acentuadas) + ~132 ASCII não-acentuadas em `message.go`/`store.go`/handlers +
98 (CLI, com acento) ≈ 394 pontos de código**, um número **quase o dobro** de 207. A explicação mais
provável (não confirmada, por isso não vira afirmação): 207 media *strings distintas por texto*
usando busca só por acento, sem incluir `message.go` — que sozinho, por não usar acento em nenhum
dos 103 pontos, ficaria inteiramente fora de uma varredura assim.

**O que fica firme independente da reconciliação:** o número que decide o próximo passo —
**quantas strings, se traduzidas, mudam algo que o consumidor pode estar comparando** — é
**SAIDA-CONSUMIDOR (~44) + AMBOS (~132) = ~176 padrões de mensagem**, a maioria concentrada em UMA
função (`internal/outbound/message.go: Request.Validate()`, 103 delas) mais os `const`
Warning/Message de `templates_handler.go` (9) e o campo `NextStep` de `registration_handler.go`
(1) — estas últimas dez são as que "ninguém esperaria", porque vivem em corpo de SUCESSO, não de
erro.
