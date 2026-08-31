Código: internal/meta/types.go, internal/meta/templates.go, internal/meta/parse.go, internal/meta/perfil.go,
internal/outbound/handler.go, internal/outbound/mensagem.go, internal/outbound/estado.go,
internal/outbound/vigia.go, internal/outbound/sonda_externa.go, internal/outbound/entrada.go,
internal/outbound/bloqueio_handler.go, internal/outbound/templates_handler.go,
internal/outbound/saude_handler.go, internal/outbound/fumaca_handler.go,
internal/outbound/media_handler.go, internal/outbound/perfil_handler.go, internal/inbound/deliver.go

# Inventário das 29 chaves (T-198)

Isto é uma MEDIÇÃO, não a tabela do contrato. Nenhum nome é decidido aqui — essa escolha
pertence a T-189 e é do planner. Cada linha abaixo foi conferida contra o código, não contra
`docs/CONTRATO-CONSUMIDOR.md` nem contra a T-189.

**Direção:**
- **SAIDA-EVENTO** — vai no `POST` que este gateway faz ao `callback_url` do consumidor
  (`internal/inbound/deliver.go` entrega `meta.Event`, definido em `internal/meta/types.go`).
- **SAIDA-RESPOSTA** — vai no corpo de resposta de uma rota HTTP nossa.
- **ENTRADA** — o consumidor manda para nós (corpo de requisição).

**Nota sobre o erro compartilhado:** `errorResponse` (`internal/outbound/handler.go:275-303`) é
escrito por `respondError`/`respondErrorWithDetail`/`respondMetaError`, chamadas de
`bloqueio_handler.go`, `cadastro_handler.go`, `estado_handler.go`, `fumaca_handler.go`,
`handler.go`, `leituras_handler.go`, `lideranca.go`, `media_handler.go`, `pausa_handler.go`,
`perfil_handler.go`, `saude_handler.go`, `templates_handler.go`, `tipos.go` — ou seja, é o corpo
de erro de **praticamente toda rota do pacote `outbound`**: `POST /v1/messages`,
`POST|DELETE|GET /v1/bloqueios`, `GET /v1/estado`, `GET /v1/instances/{slug}/health`,
`GET|POST|DELETE /v1/templates`, `POST /v1/media`, `GET /v1/media/{id}`, `POST /v1/pausa`,
`GET|POST /v1/perfil`, `POST /v1/leituras`, `POST /v1/cadastro`, `POST /v1/fumaca`. Abaixo, as
linhas desse struct dizem só "erro compartilhado (ver nota acima)" em vez de repetir a lista de
rotas em cada uma.

## Tabela

| # | chave | arquivo:linha | struct | direção | rota(s) |
|---|---|---|---|---|---|
| 1 | `alvo` | `internal/meta/types.go:68` | `Reaction` (em `Event.reacao`) | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 1 | `alvo` | `internal/outbound/mensagem.go:309` | `ReactionRequest` (em `Request.reacao`) | ENTRADA | `POST /v1/messages` |
| 2 | `bloqueados` | `internal/outbound/bloqueio_handler.go:173` | `blockListResponse` | SAIDA-RESPOSTA | `GET /v1/bloqueios` |
| 3 | `certificado_do_callback` | `internal/outbound/estado.go:174` | `State` | SAIDA-RESPOSTA | `GET /v1/estado` |
| 4 | `checagem_falhando_desde` | `internal/outbound/vigia.go:145` | `MetaToken` (em `State.token_meta`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 5 | `classe` | `internal/outbound/handler.go:277` | `errorResponse.Error` | SAIDA-RESPOSTA | erro compartilhado (ver nota acima) |
| 5 | `classe` | `internal/outbound/templates_handler.go:1205` | `errorResponseWithReread.Error` | SAIDA-RESPOSTA | `POST/DELETE /v1/templates` (desfecho ambíguo) |
| 6 | `codigo` | `internal/meta/types.go:142` | `StatusError` (em `Event.erro`) | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 7 | `codigo_meta` | `internal/outbound/bloqueio_handler.go:146` | `blockFailureResponse` | SAIDA-RESPOSTA | `POST/DELETE /v1/bloqueios` |
| 7 | `codigo_meta` | `internal/outbound/handler.go:278` | `errorResponse.Error` | SAIDA-RESPOSTA | erro compartilhado (ver nota acima) |
| 7 | `codigo_meta` | `internal/outbound/templates_handler.go:1206` | `errorResponseWithReread.Error` | SAIDA-RESPOSTA | `POST/DELETE /v1/templates` (desfecho ambíguo) |
| 8 | `componentes` | `internal/meta/templates.go:99` | `Template` | SAIDA-RESPOSTA | `GET /v1/templates` |
| 8 | `componentes` | `internal/outbound/templates_handler.go:365` | `CreateTemplateRequest` | ENTRADA | `POST /v1/templates` |
| 9 | `detalhe_meta` | `internal/outbound/bloqueio_handler.go:148` | `blockFailureResponse` | SAIDA-RESPOSTA | `POST/DELETE /v1/bloqueios` |
| 9 | `detalhe_meta` | `internal/outbound/handler.go:287` | `errorResponse.Error` | SAIDA-RESPOSTA | erro compartilhado (ver nota acima) |
| 10 | `detalhes` | `internal/meta/types.go:159` | `StatusError` (em `Event.erro`) | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 11 | `emoji` | `internal/meta/types.go:67` | `Reaction` (em `Event.reacao`) | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 11 | `emoji` | `internal/outbound/mensagem.go:313` | `ReactionRequest` (em `Request.reacao`) | ENTRADA | `POST /v1/messages` |
| 12 | `endereco` | `internal/meta/types.go:89` | `Location` (em `Event.localizacao`) | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 12 | `endereco` | `internal/outbound/mensagem.go:331` | `LocationRequest` (em `Request.localizacao`) | ENTRADA | `POST /v1/messages` |
| 13 | `explicacao_meta` | `internal/outbound/handler.go:297` | `errorResponse.Error` | SAIDA-RESPOSTA | erro compartilhado (ver nota acima) |
| 14 | `falhando_desde` | `internal/outbound/entrada.go:201` | `ConnectorInState` (em `State.entrada.conector`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 14 | `falhando_desde` | `internal/outbound/estado.go:543` | `InstagramTokenInState` (em `State.token_instagram`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 15 | `fonte` | `internal/outbound/estado.go:443` | `ObservedValue` (em `State.numero_na_meta`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 15 | `fonte` | `internal/outbound/sonda_externa.go:162` | `ExternalReachInState` (em `State.alcance_externo`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 16 | `gerado_em` | `internal/outbound/estado.go:86` | `State` | SAIDA-RESPOSTA | `GET /v1/estado` |
| 17 | `instrucao` | `internal/outbound/estado.go:547` | `InstagramTokenInState` (em `State.token_instagram`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 18 | `legenda` | `internal/meta/types.go:580` | `Event` | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 18 | `legenda` | `internal/outbound/mensagem.go:628` | `Request` | ENTRADA | `POST /v1/messages` |
| 19 | `localizacao` | `internal/meta/types.go:608` | `Event` | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 19 | `localizacao` | `internal/outbound/mensagem.go:640` | `Request` | ENTRADA | `POST /v1/messages` |
| 20 | `medido_em` | `internal/outbound/entrada.go:194` | `ConnectorInState` (em `State.entrada.conector`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 20 | `medido_em` | `internal/outbound/sonda_externa.go:159` | `ExternalReachInState` (em `State.alcance_externo`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 20 | `medido_em` | `internal/outbound/vigia.go:143` | `MetaToken` (em `State.token_meta`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 21 | `processados` | `internal/outbound/bloqueio_handler.go:158` | `blockOperationResponse` | SAIDA-RESPOSTA | `POST/DELETE /v1/bloqueios` |
| 22 | `rastro_meta` | `internal/outbound/handler.go:301` | `errorResponse.Error` | SAIDA-RESPOSTA | erro compartilhado (ver nota acima) |
| 23 | `reacao` | `internal/meta/types.go:541` | `Event` | SAIDA-EVENTO | webhook (`POST callback_url`) |
| 23 | `reacao` | `internal/outbound/mensagem.go:637` | `Request` | ENTRADA | `POST /v1/messages` |
| 24 | `subcodigo_meta` | `internal/outbound/handler.go:293` | `errorResponse.Error` | SAIDA-RESPOSTA | erro compartilhado (ver nota acima) |
| 25 | `token_instagram` | `internal/outbound/estado.go:192` | `State` | SAIDA-RESPOSTA | `GET /v1/estado` |
| 26 | `token_meta` | `internal/outbound/estado.go:169` | `State` | SAIDA-RESPOSTA | `GET /v1/estado` |
| 27 | `valor` | `internal/outbound/estado.go:432` | `ObservedValue` (em `State.numero_na_meta`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 28 | `veredito` | `internal/outbound/estado.go:513` | `InstagramTokenInState` (em `State.token_instagram`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 28 | `veredito` | `internal/outbound/saude_handler.go:94` | `healthResponse` | SAIDA-RESPOSTA | `GET /v1/instances/{slug}/health` |
| 28 | `veredito` | `internal/outbound/sonda_externa.go:152` | `ExternalReachInState` (em `State.alcance_externo`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 28 | `veredito` | `internal/outbound/vigia.go:142` | `MetaToken` (em `State.token_meta`) | SAIDA-RESPOSTA | `GET /v1/estado` |
| 29 | `voz` | `internal/meta/types.go:575` | `Event` | SAIDA-EVENTO | webhook (`POST callback_url`) |

**Reconciliação do total:** 29 chaves pedidas → 47 linhas na tabela. A diferença (18 linhas a
mais) é toda explicada por chave repetida em mais de um ponto de emissão — nenhuma chave ficou
de fora e nenhuma linha é invenção:
- 6 chaves aparecem em SAIDA-EVENTO **e** ENTRADA com o mesmo nome, por desenho (`alvo`, `emoji`,
  `endereco`, `legenda`, `localizacao`, `reacao` — o vocabulário de reação/localização é
  deliberadamente o mesmo dos dois lados, ver comentário em `mensagem.go:271-274`).
- 1 chave aparece em SAIDA-RESPOSTA **e** ENTRADA (`componentes` — corpo do catálogo de
  templates de saída e corpo de criação de template de entrada).
- As demais repetições são a MESMA chave em pontos SAIDA-RESPOSTA diferentes dentro de
  `GET /v1/estado` ou do erro compartilhado (`classe`, `codigo_meta`, `detalhe_meta`,
  `falhando_desde`, `fonte`, `medido_em`, `veredito`).

**Nenhuma das 29 chaves está ausente do código, e nenhuma delas já está em inglês** — todas as
29 são, hoje, nomes em português nos estruturas Go listadas acima.

## Item 4 — o caminho inverso: chaves de SAÍDA que já estão em inglês

Varredura de `json:"…"` em todo struct de SAIDA (SAIDA-EVENTO ou SAIDA-RESPOSTA) dos pacotes
`internal/meta` e `internal/outbound`, excluindo:
- structs internos que decodificam o formato CRU da Meta e morrem por dentro (nunca chegam ao
  consumidor) — ex.: `internal/meta/parse.go` inteiro, `reactionMeta`/`locationMeta`
  (`parse.go:361-376`), os decoders de `bloqueio.go`, `instagram.go`, `client.go`, `numero.go`,
  `diagnostico_instagram.go`;
- `internal/outbound/entrada.go:431` (`readyConnections`, camelCase) — não é nosso contrato: é
  o gateway LENDO a resposta do `/ready` de um conector de terceiro, não uma emissão nossa.

| chave inglesa | arquivo:linha | struct | direção | rota(s) |
|---|---|---|---|---|
| `media_id` | `internal/outbound/media_handler.go:260` | (map literal, sem struct nomeado) | SAIDA-RESPOSTA | `POST /v1/media` — **este é o par exato do `midia_id` citado no `Why` da tarefa** |
| `latitude` | `internal/meta/types.go:86` | `Location` (em `Event.localizacao`) | SAIDA-EVENTO | webhook |
| `longitude` | `internal/meta/types.go:87` | `Location` (em `Event.localizacao`) | SAIDA-EVENTO | webhook |
| `id` | `internal/meta/types.go:419` | `Event` | SAIDA-EVENTO | webhook |
| `phone_number_id` | `internal/meta/types.go:424` | `Event` | SAIDA-EVENTO | webhook |
| `waba_id` | `internal/meta/types.go:425` | `Event` | SAIDA-EVENTO | webhook |
| `timestamp` | `internal/meta/types.go:427` | `Event` | SAIDA-EVENTO | webhook |
| `wa_message_id` | `internal/meta/types.go:430` | `Event` | SAIDA-EVENTO | webhook |
| `status` | `internal/meta/types.go:584` | `Event` | SAIDA-EVENTO | webhook |
| `template` | `internal/meta/types.go:617` | `Event` (chave do campo, guarda `TemplateStatus`) | SAIDA-EVENTO | webhook |
| `ig_id` | `internal/outbound/estado.go:62` | `State` | SAIDA-RESPOSTA | `GET /v1/estado` |
| `wa_id` | `internal/outbound/bloqueio_handler.go:136` | `blockItemResponse` | SAIDA-RESPOSTA | `POST/DELETE /v1/bloqueios` |
| `wa_id` | `internal/outbound/bloqueio_handler.go:145` | `blockFailureResponse` | SAIDA-RESPOSTA | `POST/DELETE /v1/bloqueios` |
| `wa_id` | `internal/outbound/bloqueio_handler.go:166` | `blockListItem` | SAIDA-RESPOSTA | `GET /v1/bloqueios` |
| `templates` | `internal/outbound/templates_handler.go:348` | `templatesResponse` | SAIDA-RESPOSTA | `GET /v1/templates` |
| `id` | `internal/outbound/templates_handler.go:388` | `templateCreatedResponse` | SAIDA-RESPOSTA | `POST /v1/templates` |
| `status` | `internal/outbound/templates_handler.go:389` | `templateCreatedResponse` | SAIDA-RESPOSTA | `POST /v1/templates` |
| `id` | `internal/outbound/templates_handler.go:542` | `templateEntry` | SAIDA-RESPOSTA | `DELETE /v1/templates` (desfecho ambíguo) |
| `status` | `internal/outbound/templates_handler.go:545` | `templateEntry` | SAIDA-RESPOSTA | `DELETE /v1/templates` (desfecho ambíguo) |
| `ok` | `internal/outbound/saude_handler.go:83` | `healthResponse` | SAIDA-RESPOSTA | `GET /v1/instances/{slug}/health` |
| `wa_message_id` | `internal/outbound/fumaca_handler.go:131` | `SmokeResponse` | SAIDA-RESPOSTA | `POST /v1/fumaca` |
| `about`,`address`,`description`,`email`,`profile_picture_url`,`websites`,`vertical` | `internal/meta/perfil.go:65-71` | `Profile` | SAIDA-RESPOSTA | `GET /v1/perfil` |
| `about`,`address`,`description`,`email`,`websites`,`vertical`,`profile_picture_handle` | `internal/meta/perfil.go:92-103` | `ProfilePatch` (ecoado em `profileWriteResponse.gravado`) | SAIDA-RESPOSTA **e** ENTRADA | `POST /v1/perfil` |

**Observação sobre `id`/`status`/`ok`/`timestamp`/`template`:** são palavras genéricas ou
identificadores técnicos (não o "vocabulário fechado de contadores" que a T-189 mede à parte),
mas contam como chave de saída já em inglês pelo mesmo critério que pegou `media_id` — se a
T-189 tratar "tudo que não está na lista de 89 é português" como premissa, cada uma destas é
outro `media_id` esperando para quebrar.
