Código: internal/meta/types.go, internal/meta/templates.go, internal/meta/perfil.go,
internal/outbound/mensagem.go, internal/outbound/handler.go, internal/outbound/estado.go,
internal/outbound/vigia.go, internal/outbound/sonda_externa.go, internal/outbound/entrada.go,
internal/outbound/entrada_apelidos.go, internal/outbound/bloqueio_handler.go,
internal/outbound/templates_handler.go, internal/outbound/saude_handler.go,
internal/outbound/fumaca_handler.go, internal/outbound/media_handler.go,
internal/outbound/perfil_handler.go, internal/outbound/pausa_handler.go,
internal/outbound/cadastro_handler.go, internal/outbound/leituras_handler.go,
internal/outbound/lideranca.go, internal/config/contador.go,
internal/inbound/deliver.go, internal/inbound/deliver_test.go, cmd/zapgw/provisionar.go,
docs/INVENTARIO-CHAVES.md

*[Read in English](MIGRACAO-CONTRATO-EN.md)*

# A tabela de migração do contrato para inglês

✅ **O passo 2 (T-203) está NO AR desde 2026-08-31.** Toda linha ENTRADA abaixo agora também é
aceita na grafia em inglês, na entrada, na MESMA posição que a tabela nomeia — o gateway traduz
para a forma canônica (português) antes de validar e antes de calcular o hash de idempotência, de
modo que o mesmo pedido escrito nos dois idiomas produz o mesmo resultado e o mesmo
`wa_message_id`. A saída não muda: isto é o passo 2 do plano de quatro (T-189, `docs/TASKS.md`),
não o passo 4. Os dicionários moram em `internal/outbound/entrada_apelidos.go`; o contador do nome
velho (`config.CounterOldNameUsed`, exposto por instância em `GET /v1/estado`) é o que vai
autorizar o passo 4.

Esta é a tabela duradoura por trás do passo 4 da T-189 (o dia em que a saída do gateway vira
inglês). Ela existe porque a tabela só vivia dentro de um canal privado por consumidor até agora, e
a doutrina de canal deste workspace é explícita: *o que vale para TODOS sai do canal para o
documento duradouro*. Esta tabela vale para todo consumidor, inclusive o `consumer-a`, que ainda
nem começou a migrar.

🔴 **A MONTAGEM desta tabela não decidiu nome nenhum, de propósito** — ela junta duas fontes que já
existiam, uma em que o consumidor e este gateway já tinham combinado um par, outra em que a chave só
tinha sido medida contra o código. **Inventar um nome durante a montagem vira quebra silenciosa em
produção do lado do consumidor**, e isso já aconteceu uma vez: ver seção 5.

✅ **Os 29 pares que faltavam foram decididos pelo planner em 2026-08-31**, num passo separado, a
partir de convenções que já estavam na tabela A — e com uma conferência de colisão contra o
vocabulário da Meta. **A seção 7 registra quais convenções, e o que foi medido.** Nenhuma célula diz
`A DECIDIR` mais.

## 1. De onde vem cada linha

- **Tabela A (90 linhas)** — pares já propostos ao `consumer-b`, no canal privado de status por
  consumidor (ver o protocolo `CANAL-ENTRE-SESSOES.md` do workspace), na seção datada de
  **2026-08-30 23:05**. Copiados verbatim quanto ao par português/inglês; a coluna **Direção** e o
  ponteiro **arquivo:linha** foram **medidos contra o código para este documento** — a mensagem do
  canal não trazia nenhum dos dois.
- **Tabela B (29 linhas)** — chaves medidas diretamente contra o código (`docs/INVENTARIO-CHAVES.md`,
  T-198): lidas pelo `consumer-b` mas ausentes da tabela proposta. A coluna de inglês era
  `A DECIDIR` em toda linha até 2026-08-31, quando o planner preencheu as 29 — ver seção 7.
- **Total: 119 linhas (90 + 29).** A tarefa que originou este documento estimava 89 + 29 = 118 —
  ver a reconciliação de contagem abaixo para o motivo do número medido ser 90, não 89.
- **Zero sobreposição entre as duas tabelas** — confirmado por comparação de conjuntos entre as
  duas listas de chaves.

## 2. Reconciliação da contagem

A tarefa que originou este documento estimava 89 chaves para a tabela A. Contando a tabela proposta
de verdade (`sed -n` sobre a seção do canal, 90 linhas de dado entre o cabeçalho e a próxima seção)
dá **90**, não 89, sem nenhuma chave duplicada. Este documento mantém o número medido em vez da
estimativa — a estimativa é descartada, não "ajustada para bater": a estimativa era uma contagem
de memória, o 90 é uma contagem de linhas contra o texto-fonte de verdade.

## 3. A regra que vale para toda linha

**Direção é obrigatória em toda linha**, e ela assume exatamente um de quatro valores literais:

- **SAIDA-EVENTO** — viaja no `POST` que este gateway faz no `callback_url` do consumidor (o
  envelope do webhook, `meta.Event`).
- **SAIDA-RESPOSTA** — viaja no corpo de uma resposta HTTP que este gateway devolve.
- **ENTRADA** — viaja no corpo que o consumidor manda para este gateway.
- **A MEDIR** — medida contra o código e ainda inconclusiva, ou a chave não existe como campo real
  do contrato hoje. **Nunca um chute** — toda linha `A MEDIR` abaixo diz exatamente o que foi
  encontrado no lugar (uma flag de CLI, um vetor de teste interno, o envelope de paginação da API
  da Meta).

**Uma chave pode carregar mais de uma direção na mesma linha** — isso significa que a mesma string
em português é um campo real em mais de um lugar do contrato, cada um independente. **21 das 119
chaves deste documento são multi-direção** — ver seção 6.

**Ausência deste documento NÃO significa que uma chave está em português.** Ver seção 7 — já
existem chaves de saída em inglês hoje que esta tabela não renomeia, porque não precisam de
renomeação.

## 4. A regra de colisão, com crédito ao `consumer-b`

*Só renomeia se o nome de destino ainda não existir naquele dicionário.* O nosso inglês para
`texto` é `text`, e `text` é **também o nome que a própria Meta usa** dentro de um objeto de
mensagem; o mesmo vale para `category`. Na dúvida, a tradução não faz nada — um `text` ao lado de
um `texto` é da Meta e fica intocado. Esta é a regra do próprio consumidor, escrita por eles, e ela
rege como a tabela abaixo é aplicada do lado deles.

## 5. O exemplo que já quebrou, e por que toda linha multi-direção importa

`internal/meta/types.go:543` emite **`midia_id`** no evento de webhook — o único dos três pontos de
emissão ainda em português. A resposta do `POST /v1/media`
(`internal/outbound/media_handler.go:260`) e o **corpo de entrada** que a rota `/v1/messages`
também aceita (`internal/outbound/mensagem.go:179` e `:626`) já emitem/aceitam **`media_id`**, de
propósito — o comentário no código diz que o nome bate com o que `/v1/messages` espera de volta,
sem tradução no meio.

**Mesmo conceito, dois nomes, duas direções — hoje, antes de qualquer migração.** Uma versão
anterior desta tabela listava `midia_id -> media_id` sem dizer onde valia; o tradutor do próprio
consumidor renomeou a resposta do `/v1/media`, e **o upload de mídia parou de funcionar**. Foi
consertado do lado deles lendo `midia_id`, que funciona nos dois. A linha 53 abaixo (`midia_id`)
carrega esse aviso embutido. **É por isso que toda linha declara uma direção, e por que uma chave
que já existe em inglês ganha sua própria seção (7) em vez de ficar a cargo de inferência — "não
está na tabela, então deve ser português" é exatamente onde se erra.**

## 6. Tabela A — pares já propostos ao `consumer-b` (90 linhas)

| # | português | inglês | direção | arquivo:linha |
|---|---|---|---|---|
| 1 | `aberta` | `open` | SAIDA-RESPOSTA | internal/outbound/cadastro_handler.go:120 |
| 2 | `alcance_externo` | `external_reach` | SAIDA-RESPOSTA | internal/outbound/estado.go:216 |
| 3 | `alerta_de_conta` | `account_alert` | SAIDA-EVENTO | internal/meta/types.go:639 |
| 4 | `assinatura_esperada` | `expected_signature` | A MEDIR | não é chave do contrato hoje — só aparece em vetor de teste interno (internal/inbound/deliver_test.go:245, testdata/assinatura-entrega.json) |
| 5 | `botao_payload` | `button_payload` | SAIDA-EVENTO | internal/meta/types.go:536 |
| 6 | `botao_texto` | `button_text` | SAIDA-EVENTO | internal/meta/types.go:537 |
| 7 | `botao_titulo` | `button_title` | ENTRADA | internal/outbound/mensagem.go:587 |
| 8 | `botao_url` | `button_url` | ENTRADA | internal/outbound/mensagem.go:588 |
| 9 | `botoes` | `buttons` | ENTRADA | internal/outbound/mensagem.go:579 |
| 10 | `botoes_template` | `template_buttons` | ENTRADA | internal/outbound/mensagem.go:559 |
| 11 | `botoes_url` | `url_buttons` | ENTRADA | internal/outbound/mensagem.go:549 (campo mantido só para ser RECUSADO, T-045) |
| 12 | `cabecalho` | `header` | ENTRADA | internal/outbound/mensagem.go:527 |
| 13 | `cabecalho_texto` | `header_text` | ENTRADA | internal/outbound/mensagem.go:622 |
| 14 | `carimbos_desde` | `stamps_since` | SAIDA-RESPOSTA | internal/outbound/estado.go:105 |
| 15 | `categoria` | `category` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:181,235 (evento); internal/outbound/mensagem.go:627 (entrada); internal/outbound/templates_handler.go:363,390 (resposta) |
| 16 | `categoria_anterior` | `previous_category` | SAIDA-EVENTO | internal/meta/types.go:304 |
| 17 | `categoria_correta` | `correct_category` | SAIDA-EVENTO | internal/meta/types.go:312 |
| 18 | `categoria_nova` | `new_category` | SAIDA-EVENTO | internal/meta/types.go:305 |
| 19 | `categoria_pedida` | `requested_category` | SAIDA-RESPOSTA | internal/outbound/templates_handler.go:399 |
| 20 | `cifrados` | `encrypted` | SAIDA-RESPOSTA | internal/outbound/cadastro_handler.go:146 |
| 21 | `cobranca` | `pricing` | SAIDA-EVENTO | internal/meta/types.go:605 |
| 22 | `conector` | `connector` | SAIDA-RESPOSTA | internal/outbound/entrada.go:213 |
| 23 | `conexoes_prontas` | `ready_connections` | SAIDA-RESPOSTA | internal/outbound/entrada.go:188 |
| 24 | `conferido_em` | `checked_at` | SAIDA-RESPOSTA | internal/outbound/vigia.go:144; internal/outbound/estado.go:412 |
| 25 | `contadores` | `counters` | SAIDA-RESPOSTA | internal/outbound/estado.go:111,276 |
| 26 | `corpo` | `body` | A MEDIR | não é chave do contrato hoje — só aparece em vetor de teste interno (internal/inbound/deliver_test.go:244, testdata/assinatura-entrega.json) |
| 27 | `cru` | `raw` | SAIDA-EVENTO | internal/inbound/deliver.go:47 |
| 28 | `data` | `date` | A MEDIR | não é chave do contrato hoje — as únicas ocorrências de "data" no código são o envelope de paginação da própria API da Meta (ex.: internal/meta/perfil.go:151), nunca uma chave nossa |
| 29 | `de` | `from` | A MEDIR | não é chave do contrato hoje — só existem `de_cru` e `de_canonico`, nunca um "de" isolado |
| 30 | `de_canonico` | `from_canonical` | SAIDA-EVENTO | internal/meta/types.go:444 |
| 31 | `de_cru` | `from_raw` | SAIDA-EVENTO | internal/meta/types.go:443 |
| 32 | `desfecho` | `outcome` | SAIDA-RESPOSTA | internal/outbound/templates_handler.go:555 |
| 33 | `dia` | `day` | SAIDA-RESPOSTA | internal/outbound/estado.go:272 |
| 34 | `dia_utc` | `day_utc` | SAIDA-RESPOSTA | internal/outbound/estado.go:275 |
| 35 | `dias_restantes` | `days_left` | SAIDA-RESPOSTA | internal/outbound/estado.go:526 |
| 36 | `encaminhada` | `forwarded` | SAIDA-EVENTO | internal/meta/types.go:529 |
| 37 | `encaminhada_muitas_vezes` | `frequently_forwarded` | SAIDA-EVENTO | internal/meta/types.go:530 |
| 38 | `entrada` | `ingress` | SAIDA-RESPOSTA | internal/outbound/estado.go:207 |
| 39 | `erro` | `error` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:599 (evento); internal/outbound/handler.go:302 (resposta, erro compartilhado) |
| 40 | `estado` | `state` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:242,352,398 (evento); internal/outbound/estado.go:66 e outros (resposta) |
| 41 | `eventos` | `events` | SAIDA-EVENTO | internal/inbound/deliver.go:48 |
| 42 | `expira_em` | `expires_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:360,519 |
| 43 | `falhas` | `failures` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:159 |
| 44 | `idioma` | `language` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:230,295 (evento); internal/outbound/mensagem.go:522 (entrada); internal/outbound/templates_handler.go:364,543 (resposta) |
| 45 | `instancia` | `instance` | ENTRADA + SAIDA-RESPOSTA + SAIDA-EVENTO | internal/outbound/mensagem.go:512 (entrada); internal/outbound/estado.go:44 (resposta); internal/inbound/deliver.go:45 (evento, envelope) |
| 46 | `instancias` | `instances` | A MEDIR | não é chave JSON do contrato hoje — só existe como flag de CLI (`--instancias`), fora do HTTP (cmd/zapgw/provisionar.go:1476) |
| 47 | `janela_de_cadastro` | `registration_window` | SAIDA-RESPOSTA | internal/outbound/cadastro_handler.go:145 |
| 48 | `limite_anterior` | `previous_limit` | SAIDA-EVENTO | internal/meta/types.go:359 |
| 49 | `limite_atual` | `current_limit` | SAIDA-EVENTO | internal/meta/types.go:358 |
| 50 | `limite_de_mensagens` | `message_limit` | SAIDA-RESPOSTA | internal/outbound/estado.go:402 |
| 51 | `limite_diario_maximo` | `max_daily_limit` | SAIDA-EVENTO | internal/meta/types.go:366 |
| 52 | `mensagem` | `message` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:143 (evento); internal/outbound/handler.go:279 (resposta, erro compartilhado) |
| 53 | `midia_id` | `media_id` | SAIDA-EVENTO | internal/meta/types.go:543 — 🔴 ver seção 5: colide com `media_id`, já ENTRADA + SAIDA-RESPOSTA em inglês |
| 54 | `midia_mime_payload` | `media_mime_payload` | SAIDA-EVENTO | internal/meta/types.go:547 |
| 55 | `motivo` | `reason` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:251 (evento); internal/outbound/lideranca.go:231 (resposta) |
| 56 | `nome` | `name` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:229,294 (evento); internal/outbound/mensagem.go:330,485 (entrada); internal/outbound/templates_handler.go:362,552 (resposta) |
| 57 | `nome_arquivo` | `file_name` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:581 (evento); internal/outbound/mensagem.go:181,629 (entrada) |
| 58 | `nome_contato` | `contact_name` | SAIDA-EVENTO | internal/meta/types.go:445 |
| 59 | `numero_exibido` | `display_number` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:345 (evento); internal/outbound/cadastro_handler.go:96 (entrada); internal/outbound/saude_handler.go:84 (resposta) |
| 60 | `numero_na_meta` | `number_at_meta` | SAIDA-RESPOSTA | internal/outbound/estado.go:187 |
| 61 | `observado_em` | `observed_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:363,436 |
| 62 | `para` | `to` | ENTRADA | internal/outbound/mensagem.go:513 |
| 63 | `para_canonico` | `to_canonical` | SAIDA-EVENTO | internal/meta/types.go:586 |
| 64 | `para_cru` | `to_raw` | SAIDA-EVENTO | internal/meta/types.go:585 |
| 65 | `pausada` | `paused` | SAIDA-RESPOSTA | internal/outbound/estado.go:77; internal/outbound/pausa_handler.go:64 |
| 66 | `payload` | `payload` | ENTRADA | internal/outbound/mensagem.go:241 |
| 67 | `qualidade` | `quality` | SAIDA-RESPOSTA | internal/outbound/estado.go:393 |
| 68 | `qualidade_do_numero` | `number_quality` | SAIDA-EVENTO | internal/meta/types.go:636 |
| 69 | `recebido_em` | `received_at` | SAIDA-EVENTO | internal/inbound/deliver.go:46 |
| 70 | `renovado_em` | `renewed_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:535 |
| 71 | `responder_a` | `reply_to` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:479 (evento); internal/outbound/mensagem.go:515 (entrada) |
| 72 | `rodape` | `footer` | ENTRADA | internal/outbound/mensagem.go:623 |
| 73 | `segredo_entrega` | `delivery_secret` | A MEDIR | não é chave do contrato hoje — só aparece em vetor de teste interno (internal/inbound/deliver_test.go:242, testdata/assinatura-entrega.json) |
| 74 | `serie_7_dias` | `last_7_days_series` | SAIDA-RESPOSTA | internal/outbound/estado.go:129 |
| 75 | `serie_diaria` | `daily_series` | SAIDA-RESPOSTA | internal/outbound/estado.go:152 |
| 76 | `sub_tipo` | `sub_kind` | SAIDA-EVENTO | internal/meta/types.go:431 |
| 77 | `template` | `template` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:617 (evento); internal/outbound/mensagem.go:521 (entrada) — mesma grafia nos dois idiomas, sem renomeação |
| 78 | `template_categoria` | `template_category` | SAIDA-EVENTO | internal/meta/types.go:627 |
| 79 | `templates` | `templates` | SAIDA-RESPOSTA | internal/outbound/templates_handler.go:348 — mesma grafia; ver também "O que NÃO muda" (seção 7) |
| 80 | `texto` | `text` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:446 (evento); internal/outbound/mensagem.go:177,239,518 (entrada) |
| 81 | `tipo` | `kind` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:413 (evento); internal/outbound/mensagem.go:514 (entrada); internal/outbound/estado.go:51 (resposta) |
| 82 | `tipo_da_entidade` | `entity_kind` | SAIDA-EVENTO | internal/meta/types.go:391 |
| 83 | `token_envio` | `send_token` | ENTRADA | internal/outbound/cadastro_handler.go:98 |
| 84 | `total` | `total` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:172; internal/outbound/templates_handler.go:347 |
| 85 | `ultimo_em` | `last_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:241 |
| 86 | `ultimo_webhook_em` | `last_webhook_at` | SAIDA-RESPOSTA | internal/outbound/entrada.go:225 |
| 87 | `ultimos_7_dias` | `last_7_days` | SAIDA-RESPOSTA | internal/outbound/estado.go:237 |
| 88 | `variaveis` | `variables` | ENTRADA | internal/outbound/mensagem.go:523 |
| 89 | `versao` | `version` | SAIDA-RESPOSTA | internal/outbound/estado.go:82 |
| 90 | `via` | `via` | SAIDA-RESPOSTA | internal/outbound/entrada.go:212 — mesma grafia nos dois idiomas |

## 7. Tabela B — chaves medidas, par ainda não decidido (29 linhas)

Medidas diretamente contra o código para a T-198 (`docs/INVENTARIO-CHAVES.md`): chaves que o
`consumer-b` lê e que estavam ausentes da tabela proposta. **A coluna de inglês era `A DECIDIR` em
toda linha até 2026-08-31 (seção 7) — a MONTAGEM deste documento não escolheu nenhuma delas.**

| # | português | inglês | direção | arquivo:linha |
|---|---|---|---|---|
| 91 | `alvo` | `target` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:68 (evento); internal/outbound/mensagem.go:309 (entrada) |
| 92 | `bloqueados` | `blocked` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:173 |
| 93 | `certificado_do_callback` | `callback_certificate` | SAIDA-RESPOSTA | internal/outbound/estado.go:174 |
| 94 | `checagem_falhando_desde` | `check_failing_since` | SAIDA-RESPOSTA | internal/outbound/vigia.go:145 |
| 95 | `classe` | `class` | SAIDA-RESPOSTA | internal/outbound/handler.go:277; internal/outbound/templates_handler.go:1205 |
| 96 | `codigo` | `code` | SAIDA-EVENTO | internal/meta/types.go:142 |
| 97 | `codigo_meta` | `meta_code` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:146; internal/outbound/handler.go:278; internal/outbound/templates_handler.go:1206 |
| 98 | `componentes` | `components` | SAIDA-RESPOSTA + ENTRADA | internal/meta/templates.go:99 (resposta, catálogo); internal/outbound/templates_handler.go:365 (entrada, criação) |
| 99 | `detalhe_meta` | `meta_detail` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:148; internal/outbound/handler.go:287 |
| 100 | `detalhes` | `details` | SAIDA-EVENTO | internal/meta/types.go:159 |
| 101 | `emoji` | `emoji` (não muda) | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:67 (evento); internal/outbound/mensagem.go:313 (entrada) |
| 102 | `endereco` | `address` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:89 (evento); internal/outbound/mensagem.go:331 (entrada) |
| 103 | `explicacao_meta` | `meta_explanation` | SAIDA-RESPOSTA | internal/outbound/handler.go:297 |
| 104 | `falhando_desde` | `failing_since` | SAIDA-RESPOSTA | internal/outbound/entrada.go:201; internal/outbound/estado.go:543 |
| 105 | `fonte` | `source` | SAIDA-RESPOSTA | internal/outbound/estado.go:443; internal/outbound/sonda_externa.go:162 |
| 106 | `gerado_em` | `generated_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:86 |
| 107 | `instrucao` | `instruction` | SAIDA-RESPOSTA | internal/outbound/estado.go:547 |
| 108 | `legenda` | `caption` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:580 (evento); internal/outbound/mensagem.go:628 (entrada) |
| 109 | `localizacao` | `location` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:608 (evento); internal/outbound/mensagem.go:640 (entrada) |
| 110 | `medido_em` | `measured_at` | SAIDA-RESPOSTA | internal/outbound/entrada.go:194; internal/outbound/sonda_externa.go:159; internal/outbound/vigia.go:143 |
| 111 | `processados` | `processed` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:158 |
| 112 | `rastro_meta` | `meta_trace` | SAIDA-RESPOSTA | internal/outbound/handler.go:301 |
| 113 | `reacao` | `reaction` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:541 (evento); internal/outbound/mensagem.go:637 (entrada) |
| 114 | `subcodigo_meta` | `meta_subcode` | SAIDA-RESPOSTA | internal/outbound/handler.go:293 |
| 115 | `token_instagram` | `instagram_token` | SAIDA-RESPOSTA | internal/outbound/estado.go:192 |
| 116 | `token_meta` | `meta_token` | SAIDA-RESPOSTA | internal/outbound/estado.go:169 |
| 117 | `valor` | `value` | SAIDA-RESPOSTA | internal/outbound/estado.go:432 |
| 118 | `veredito` | `verdict` | SAIDA-RESPOSTA | internal/outbound/estado.go:513; internal/outbound/saude_handler.go:94; internal/outbound/sonda_externa.go:152; internal/outbound/vigia.go:142 |
| 119 | `voz` | `voice` | SAIDA-EVENTO | internal/meta/types.go:575 |

## 8. O que NÃO muda — chaves de saída que já estão em inglês

**A ausência de uma chave nas tabelas A e B acima NÃO significa que ela está em português.** Estas
chaves de saída (`SAIDA-EVENTO` ou `SAIDA-RESPOSTA`) já saem deste gateway em inglês hoje, antes de
qualquer passo de migração rodar. Nenhuma delas é renomeada pelo passo 4 da T-189 — elas
simplesmente continuam como estão. Medido varrendo toda tag `json:"…"` de todo struct de saída em
`internal/meta` e `internal/outbound`, T-198 (`docs/INVENTARIO-CHAVES.md`, item 4).

| chave inglesa | arquivo:linha | struct | direção | rota(s) |
|---|---|---|---|---|
| `media_id` | `internal/outbound/media_handler.go:260` | (map literal, sem struct nomeado) | SAIDA-RESPOSTA | `POST /v1/media` — **este é o par exato do `midia_id`, seção 5 acima** |
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

## 9. Chaves multi-direção — as perigosas

**21 das 119 chaves acima carregam mais de uma direção.** Cada uma é um `media_id` esperando para
acontecer: quem renomeia uma ocorrência sem checar as outras quebra a direção irmã em silêncio,
exatamente como na seção 5. Lista, por tabela:

**Tabela A (14 chaves):** `categoria`, `erro`, `estado`, `idioma`, `instancia`, `mensagem`,
`motivo`, `nome`, `nome_arquivo`, `numero_exibido`, `responder_a`, `template`, `texto`, `tipo`.

**Tabela B (7 chaves):** `alvo`, `componentes`, `emoji`, `endereco`, `legenda`, `localizacao`,
`reacao`.

Seis das sete da tabela B (`alvo`, `emoji`, `endereco`, `legenda`, `localizacao`, `reacao`)
compartilham o mesmo motivo: o vocabulário de reação/localização é deliberadamente idêntico no
envio e no recebimento (`internal/outbound/mensagem.go:271-274`), então a mesma palavra é um campo
real tanto num `Event` de saída quanto num `Request` de entrada.

## 10. Linhas onde não existe chave de contrato mensurável (`A MEDIR`)

Seis linhas da tabela A são `A MEDIR` porque a string em português proposta não corresponde a
nenhum campo JSON real deste contrato hoje — nunca um chute, cada uma nomeia o que foi encontrado
no lugar:

- `assinatura_esperada`, `corpo`, `segredo_entrega` — só existem dentro de um único arquivo de vetor
  de teste interno (`testdata/assinatura-entrega.json`, lido por `internal/inbound/deliver_test.go`),
  que fixa o cálculo de uma assinatura HMAC, não o formato do envelope. Nenhum consumidor jamais vê
  essas três chaves.
- `data` — as únicas ocorrências de `"data"` no código são o envelope de paginação da própria API
  do Graph da Meta (`{"data": [...]}`), decodificado internamente e nunca reexposto sob esse nome.
- `de` — só existem `de_cru` e `de_canonico`; não há um campo `de` isolado.
- `instancias` — só existe como flag de CLI (`--instancias`, `cmd/zapgw/provisionar.go:1476`),
  nunca como campo HTTP/JSON.

## 7. Os 29 nomes, decididos em 2026-08-31 — e a conferência de colisão que veio junto

A coluna de inglês da tabela B era `A DECIDIR` até 2026-08-31. O planner preencheu nessa data,
seguindo as convenções **que já estavam na tabela A**, não inventadas aqui:

- `conferido_em` -> `checked_at`, então `gerado_em` -> `generated_at` e `medido_em` -> `measured_at`.
- `carimbos_desde` -> `stamps_since`, então `falhando_desde` -> `failing_since`.
- `alerta_de_conta` -> `account_alert` (modificador primeiro), então `token_meta` -> `meta_token`,
  `codigo_meta` -> `meta_code`, `certificado_do_callback` -> `callback_certificate`.
- `emoji` é a mesma palavra nos dois idiomas e **não muda** — como `payload`, `template`,
  `templates`, `total` e `via`, que o próprio consumidor já tinha apontado.

🔴 **A conferência de colisão, porque este é o modo de falha que o consumidor nomeou.** A regra dele
— *só renomeia se o nome de destino ainda não existir naquele dicionário* — existe porque o nosso
inglês para `texto` é `text`, e `text` é também nome da **Meta** dentro de um objeto de mensagem.
Vários destes 29 têm forma inglesa que a Meta também usa: `components`, `location`, `caption`,
`reaction`, `voice`, `address`, `code`.

**Medido em 2026-08-31**, lendo as tags `json` irmãs de cada struct onde essas chaves são emitidas
(`internal/meta/types.go:67,89,141,158,540,574,579,607`, `internal/meta/templates.go:99`): **todo
irmão nesses objetos é nosso**, todos ainda em português. **Nenhuma chave da Meta divide objeto com
qualquer uma destas 29** — o vocabulário da Meta que passa intacto vive no `cru` e nos objetos de
passagem, que não são visitados.

⚠️ **Essa medição é uma fotografia, não uma garantia.** Se uma mudança futura puser um objeto cru da
Meta ao lado de um destes campos, a colisão passa a ser real e a regra do consumidor é o que salva.
**A regra é o mecanismo; esta medição só diz que hoje ele não tem o que fazer.**

## 8. Os vocabularios de VALOR, decididos em 2026-08-31

🔴 **Decisao do dono, e ela e a MESMA de 2026-08-30 — nunca foi mais estreita que isto:**
*"o projeto precisa ser em ingles"*. O exemplo que veio depois (*"se a chave chama nome, tem que
passar a se chamar name"*) citou uma chave porque chave era o que estava na frente dele; **ele nao
estreitou a regra para chaves**. Palavra em portugues no contrato e palavra em portugues no
contrato, esteja ela a esquerda ou a direita dos dois pontos. `{"kind": "texto"}` nao e um contrato
migrado.

🔴 **`tipo` sao QUATRO vocabularios dividindo uma chave JSON.** Um mapa de valores global —
*"onde aparecer `texto`, escreva `text`"* — reescreveria objetos que nem estao na conversa. **Cada
tabela abaixo tem escopo no proprio objeto**, e a regra do consumidor (so renomeia se o destino ainda
nao existir NAQUELE dicionario) vale por objeto, nao por nome de chave.

`*` marca valor que ja e a mesma palavra nos dois idiomas e **nao muda**.

**As tabelas sao identicas as da versao em ingles (secao 8), porque sao identificadores de
contrato — traduzir a tabela seria criar uma segunda fonte que diverge.** Ver
[`MIGRACAO-CONTRATO-EN.md`](MIGRACAO-CONTRATO-EN.md), secao 8: 8.1 `tipo` de mensagem (11 valores),
8.2 `tipo` de evento (6), 8.3 `tipo` de botao de template (2), 8.4 `tipo` de instancia (ja em
ingles), 8.5 `categoria` de midia (5), 8.6 `classe` de erro (4), 8.7 `estado` de observacao (6),
8.8-8.10 os tres `veredito`, 8.11 o contador `nome_antigo_usado` -> `old_name_used`.

🔴 **A regra que atravessa as tres tabelas de `veredito`:** onde a mesma palavra em portugues
aparece em mais de um vocabulario, **a palavra em ingles e a mesma**. Valor que traduz de dois
jeitos conforme o bloco seria uma armadilha criada por nos.
