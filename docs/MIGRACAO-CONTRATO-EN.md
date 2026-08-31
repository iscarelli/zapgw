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

*[Leia em português](MIGRACAO-CONTRATO-EN.pt-BR.md)*

# The contract's English migration table

✅ **Step 2 (T-203) is LIVE since 2026-08-31.** Every ENTRADA row below is now
also accepted under its English spelling, on input, at the SAME position the
table names — the gateway translates to the canonical (Portuguese) form
before validating and before hashing for idempotency, so a request written
in either language produces the same outcome and the same `wa_message_id`.
Output is untouched: this is step 2 of the four-step plan in `docs/TASKS.md`
(T-189), not step 4. The dictionaries live in
`internal/outbound/entrada_apelidos.go`; the old-name counter
(`config.CounterOldNameUsed`, exposed per instance in `GET /v1/estado`) is
what will authorize step 4.

This is the durable table behind T-189 step 4 (the day the gateway's output flips to English).
It exists because the table only lived inside a private per-consumer channel until now, and this
workspace's channel doctrine is explicit: *what holds for every consumer belongs in a durable
document, not in a channel*. This table holds for every consumer, including `consumer-a`, who
hasn't even started migrating yet.

🔴 **The ASSEMBLY of this table decided no name, on purpose** — it merges two sources that already
existed, one where the consumer and this gateway had agreed on a pair, one where the key had only
been measured against the code. **Inventing a name during assembly turns into a silent production
break on the consumer's side**, and it has already happened once: see section 5.

✅ **The 29 undecided pairs were filled in by the planner on 2026-08-31**, in a separate step, from
conventions already present in table A — and with a collision check against Meta's vocabulary.
**Section 7 records which conventions, and what was measured.** No cell says `A DECIDIR` any more.

## 1. Where each row comes from

- **Table A (90 rows)** — pairs already proposed to `consumer-b`, in the private per-consumer
  status channel (see the workspace's `CANAL-ENTRE-SESSOES.md` protocol), in the section dated
  **2026-08-30 23:05**. Copied verbatim from the Portuguese/English pair; the **Direction** column
  and the **file:line** pointer were **measured against the code for this document** — the channel
  message did not carry them.
- **Table B (29 rows)** — keys measured directly against the code (`docs/INVENTARIO-CHAVES.md`,
  T-198): read by `consumer-b` but missing from the proposed table. The English column was
  `A DECIDIR` on every row until 2026-08-31, when the planner filled all 29 — see section 7.
- **Total: 119 rows (90 + 29).** The task that created this document estimated 89 + 29 = 118 —
  see the count reconciliation below for why the measured number is 90, not 89.
- **Zero overlap between the two tables** — confirmed by a set comparison of both key lists.

## 2. Count reconciliation

The originating task estimated 89 keys for table A. Counting the actual proposed table
(`sed -n` over the channel section, 90 data rows between the header and the next section) gives
**90**, not 89, with no duplicate key. This document keeps the measured number rather than the
estimate — the estimate is dropped, not "fixed to match": the estimate was a rough count from
memory, the 90 is a row count against the actual source text.

## 3. The rule that governs every row

**Direction is mandatory on every row**, and it takes exactly one of four literal values:

- **SAIDA-EVENTO** — travels in the `POST` this gateway makes to the consumer's `callback_url`
  (the webhook envelope, `meta.Event`).
- **SAIDA-RESPOSTA** — travels in the body of an HTTP response this gateway returns.
- **ENTRADA** — travels in the body the consumer sends to this gateway.
- **A MEDIR** — measured against the code and still inconclusive, or the key does not exist as an
  actual contract field today. **Never a guess** — every `A MEDIR` row below names exactly what was
  found instead (a CLI flag, an internal test vector, a Meta API pagination wrapper).

**A key can carry more than one direction on the same row** — that means the same PT string is a
real field in more than one place in the contract, each independently. **21 of the 119 keys in
this document are multi-direction** — see section 6.

**Absence from this document does NOT mean a key is in Portuguese.** See section 7 — there are
already output keys in English today that this table does not rename, because they need no
renaming.

## 4. The collision rule, credited to `consumer-b`

*Only rename if the destination name doesn't already exist in that dictionary.* Our English for
`texto` is `text`, and `text` is **also Meta's own field name** inside a message object; the same
holds for `category`. On doubt, the translation does nothing — a `text` sitting next to a `texto`
belongs to Meta and stays untouched. This is the consumer's own rule, written by them, and it
governs how the table below is applied on their side.

## 5. The example that already broke, and why every multi-direction row matters

`internal/meta/types.go:543` emits **`midia_id`** in the webhook event — the only one of the three
occurrences still in Portuguese. The response of `POST /v1/media`
(`internal/outbound/media_handler.go:260`) and the **request body** the `/v1/messages` route also
accepts (`internal/outbound/mensagem.go:179` and `:626`) both already emit/accept **`media_id`**,
on purpose — the code comment says the name matches what `/v1/messages` expects back, with no
translation in between.

**Same concept, two names, two directions — today, before any migration runs.** An earlier version
of this table listed `midia_id -> media_id` without saying where it applied; the consumer's own
translator renamed the `/v1/media` response, and **media upload stopped working**. It was fixed on
their side by reading `midia_id`, which works on both. Row 53 below (`midia_id`) carries this exact
warning inline. **This is why every row states a direction, and why a key that already exists in
English gets its own section (7) instead of being left to be inferred as "not in the table, so it
must be Portuguese."**

## 6. Table A — pairs already proposed to `consumer-b` (90 rows)

| # | portuguese | english | direction | file:line |
|---|---|---|---|---|
| 1 | `aberta` | `open` | SAIDA-RESPOSTA | internal/outbound/cadastro_handler.go:120 |
| 2 | `alcance_externo` | `external_reach` | SAIDA-RESPOSTA | internal/outbound/estado.go:216 |
| 3 | `alerta_de_conta` | `account_alert` | SAIDA-EVENTO | internal/meta/types.go:639 |
| 4 | `assinatura_esperada` | `expected_signature` | A MEDIR | not a contract key today — only appears in an internal test vector (internal/inbound/deliver_test.go:245, testdata/assinatura-entrega.json) |
| 5 | `botao_payload` | `button_payload` | SAIDA-EVENTO | internal/meta/types.go:536 |
| 6 | `botao_texto` | `button_text` | SAIDA-EVENTO | internal/meta/types.go:537 |
| 7 | `botao_titulo` | `button_title` | ENTRADA | internal/outbound/mensagem.go:587 |
| 8 | `botao_url` | `button_url` | ENTRADA | internal/outbound/mensagem.go:588 |
| 9 | `botoes` | `buttons` | ENTRADA | internal/outbound/mensagem.go:579 |
| 10 | `botoes_template` | `template_buttons` | ENTRADA | internal/outbound/mensagem.go:559 |
| 11 | `botoes_url` | `url_buttons` | ENTRADA | internal/outbound/mensagem.go:549 (field kept only to be REFUSED, T-045) |
| 12 | `cabecalho` | `header` | ENTRADA | internal/outbound/mensagem.go:527 |
| 13 | `cabecalho_texto` | `header_text` | ENTRADA | internal/outbound/mensagem.go:622 |
| 14 | `carimbos_desde` | `stamps_since` | SAIDA-RESPOSTA | internal/outbound/estado.go:105 |
| 15 | `categoria` | `category` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:181,235 (event); internal/outbound/mensagem.go:627 (request); internal/outbound/templates_handler.go:363,390 (response) |
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
| 26 | `corpo` | `body` | A MEDIR | not a contract key today — only appears in an internal test vector (internal/inbound/deliver_test.go:244, testdata/assinatura-entrega.json) |
| 27 | `cru` | `raw` | SAIDA-EVENTO | internal/inbound/deliver.go:47 |
| 28 | `data` | `date` | A MEDIR | not a contract key today — the only "data" occurrences in the code are the Meta API's own pagination wrapper (e.g. internal/meta/perfil.go:151), never a field of ours |
| 29 | `de` | `from` | A MEDIR | not a contract key today — only `de_cru` and `de_canonico` exist, never a bare "de" |
| 30 | `de_canonico` | `from_canonical` | SAIDA-EVENTO | internal/meta/types.go:444 |
| 31 | `de_cru` | `from_raw` | SAIDA-EVENTO | internal/meta/types.go:443 |
| 32 | `desfecho` | `outcome` | SAIDA-RESPOSTA | internal/outbound/templates_handler.go:555 |
| 33 | `dia` | `day` | SAIDA-RESPOSTA | internal/outbound/estado.go:272 |
| 34 | `dia_utc` | `day_utc` | SAIDA-RESPOSTA | internal/outbound/estado.go:275 |
| 35 | `dias_restantes` | `days_left` | SAIDA-RESPOSTA | internal/outbound/estado.go:526 |
| 36 | `encaminhada` | `forwarded` | SAIDA-EVENTO | internal/meta/types.go:529 |
| 37 | `encaminhada_muitas_vezes` | `frequently_forwarded` | SAIDA-EVENTO | internal/meta/types.go:530 |
| 38 | `entrada` | `ingress` | SAIDA-RESPOSTA | internal/outbound/estado.go:207 |
| 39 | `erro` | `error` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:599 (event); internal/outbound/handler.go:302 (response, shared error) |
| 40 | `estado` | `state` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:242,352,398 (event); internal/outbound/estado.go:66 and others (response) |
| 41 | `eventos` | `events` | SAIDA-EVENTO | internal/inbound/deliver.go:48 |
| 42 | `expira_em` | `expires_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:360,519 |
| 43 | `falhas` | `failures` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:159 |
| 44 | `idioma` | `language` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:230,295 (event); internal/outbound/mensagem.go:522 (request); internal/outbound/templates_handler.go:364,543 (response) |
| 45 | `instancia` | `instance` | ENTRADA + SAIDA-RESPOSTA + SAIDA-EVENTO | internal/outbound/mensagem.go:512 (request); internal/outbound/estado.go:44 (response); internal/inbound/deliver.go:45 (event envelope) |
| 46 | `instancias` | `instances` | A MEDIR | not a JSON contract key today — only exists as a CLI flag (`--instancias`), outside HTTP (cmd/zapgw/provisionar.go:1476) |
| 47 | `janela_de_cadastro` | `registration_window` | SAIDA-RESPOSTA | internal/outbound/cadastro_handler.go:145 |
| 48 | `limite_anterior` | `previous_limit` | SAIDA-EVENTO | internal/meta/types.go:359 |
| 49 | `limite_atual` | `current_limit` | SAIDA-EVENTO | internal/meta/types.go:358 |
| 50 | `limite_de_mensagens` | `message_limit` | SAIDA-RESPOSTA | internal/outbound/estado.go:402 |
| 51 | `limite_diario_maximo` | `max_daily_limit` | SAIDA-EVENTO | internal/meta/types.go:366 |
| 52 | `mensagem` | `message` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:143 (event); internal/outbound/handler.go:279 (response, shared error) |
| 53 | `midia_id` | `media_id` | SAIDA-EVENTO | internal/meta/types.go:543 — 🔴 see section 5: collides with `media_id`, already ENTRADA + SAIDA-RESPOSTA in English |
| 54 | `midia_mime_payload` | `media_mime_payload` | SAIDA-EVENTO | internal/meta/types.go:547 |
| 55 | `motivo` | `reason` | SAIDA-EVENTO + SAIDA-RESPOSTA | internal/meta/types.go:251 (event); internal/outbound/lideranca.go:231 (response) |
| 56 | `nome` | `name` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:229,294 (event); internal/outbound/mensagem.go:330,485 (request); internal/outbound/templates_handler.go:362,552 (response) |
| 57 | `nome_arquivo` | `file_name` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:581 (event); internal/outbound/mensagem.go:181,629 (request) |
| 58 | `nome_contato` | `contact_name` | SAIDA-EVENTO | internal/meta/types.go:445 |
| 59 | `numero_exibido` | `display_number` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:345 (event); internal/outbound/cadastro_handler.go:96 (request); internal/outbound/saude_handler.go:84 (response) |
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
| 71 | `responder_a` | `reply_to` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:479 (event); internal/outbound/mensagem.go:515 (request) |
| 72 | `rodape` | `footer` | ENTRADA | internal/outbound/mensagem.go:623 |
| 73 | `segredo_entrega` | `delivery_secret` | A MEDIR | not a contract key today — only appears in an internal test vector (internal/inbound/deliver_test.go:242, testdata/assinatura-entrega.json) |
| 74 | `serie_7_dias` | `last_7_days_series` | SAIDA-RESPOSTA | internal/outbound/estado.go:129 |
| 75 | `serie_diaria` | `daily_series` | SAIDA-RESPOSTA | internal/outbound/estado.go:152 |
| 76 | `sub_tipo` | `sub_kind` | SAIDA-EVENTO | internal/meta/types.go:431 |
| 77 | `template` | `template` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:617 (event); internal/outbound/mensagem.go:521 (request) — same spelling in both languages, no rename needed |
| 78 | `template_categoria` | `template_category` | SAIDA-EVENTO | internal/meta/types.go:627 |
| 79 | `templates` | `templates` | SAIDA-RESPOSTA | internal/outbound/templates_handler.go:348 — same spelling; also listed under "What does NOT change" (section 7) |
| 80 | `texto` | `text` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:446 (event); internal/outbound/mensagem.go:177,239,518 (request) |
| 81 | `tipo` | `kind` | SAIDA-EVENTO + ENTRADA + SAIDA-RESPOSTA | internal/meta/types.go:413 (event); internal/outbound/mensagem.go:514 (request); internal/outbound/estado.go:51 (response) |
| 82 | `tipo_da_entidade` | `entity_kind` | SAIDA-EVENTO | internal/meta/types.go:391 |
| 83 | `token_envio` | `send_token` | ENTRADA | internal/outbound/cadastro_handler.go:98 |
| 84 | `total` | `total` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:172; internal/outbound/templates_handler.go:347 |
| 85 | `ultimo_em` | `last_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:241 |
| 86 | `ultimo_webhook_em` | `last_webhook_at` | SAIDA-RESPOSTA | internal/outbound/entrada.go:225 |
| 87 | `ultimos_7_dias` | `last_7_days` | SAIDA-RESPOSTA | internal/outbound/estado.go:237 |
| 88 | `variaveis` | `variables` | ENTRADA | internal/outbound/mensagem.go:523 |
| 89 | `versao` | `version` | SAIDA-RESPOSTA | internal/outbound/estado.go:82 |
| 90 | `via` | `via` | SAIDA-RESPOSTA | internal/outbound/entrada.go:212 — same spelling in both languages |

## 7. Table B — measured keys, pair not yet decided (29 rows)

Measured against the code for T-198 (`docs/INVENTARIO-CHAVES.md`): keys `consumer-b` reads that
were missing from table A above. **The English column was `A DECIDIR` on every row until 2026-08-31
(section 7) — the ASSEMBLY of this document did
not choose any of them.**

| # | portuguese | english | direction | file:line |
|---|---|---|---|---|
| 91 | `alvo` | `target` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:68 (event); internal/outbound/mensagem.go:309 (request) |
| 92 | `bloqueados` | `blocked` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:173 |
| 93 | `certificado_do_callback` | `callback_certificate` | SAIDA-RESPOSTA | internal/outbound/estado.go:174 |
| 94 | `checagem_falhando_desde` | `check_failing_since` | SAIDA-RESPOSTA | internal/outbound/vigia.go:145 |
| 95 | `classe` | `class` | SAIDA-RESPOSTA | internal/outbound/handler.go:277; internal/outbound/templates_handler.go:1205 |
| 96 | `codigo` | `code` | SAIDA-EVENTO | internal/meta/types.go:142 |
| 97 | `codigo_meta` | `meta_code` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:146; internal/outbound/handler.go:278; internal/outbound/templates_handler.go:1206 |
| 98 | `componentes` | `components` | SAIDA-RESPOSTA + ENTRADA | internal/meta/templates.go:99 (response, catalog); internal/outbound/templates_handler.go:365 (entrada, creation) |
| 99 | `detalhe_meta` | `meta_detail` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:148; internal/outbound/handler.go:287 |
| 100 | `detalhes` | `details` | SAIDA-EVENTO | internal/meta/types.go:159 |
| 101 | `emoji` | `emoji` (unchanged) | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:67 (event); internal/outbound/mensagem.go:313 (request) |
| 102 | `endereco` | `address` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:89 (event); internal/outbound/mensagem.go:331 (request) |
| 103 | `explicacao_meta` | `meta_explanation` | SAIDA-RESPOSTA | internal/outbound/handler.go:297 |
| 104 | `falhando_desde` | `failing_since` | SAIDA-RESPOSTA | internal/outbound/entrada.go:201; internal/outbound/estado.go:543 |
| 105 | `fonte` | `source` | SAIDA-RESPOSTA | internal/outbound/estado.go:443; internal/outbound/sonda_externa.go:162 |
| 106 | `gerado_em` | `generated_at` | SAIDA-RESPOSTA | internal/outbound/estado.go:86 |
| 107 | `instrucao` | `instruction` | SAIDA-RESPOSTA | internal/outbound/estado.go:547 |
| 108 | `legenda` | `caption` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:580 (event); internal/outbound/mensagem.go:628 (request) |
| 109 | `localizacao` | `location` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:608 (event); internal/outbound/mensagem.go:640 (request) |
| 110 | `medido_em` | `measured_at` | SAIDA-RESPOSTA | internal/outbound/entrada.go:194; internal/outbound/sonda_externa.go:159; internal/outbound/vigia.go:143 |
| 111 | `processados` | `processed` | SAIDA-RESPOSTA | internal/outbound/bloqueio_handler.go:158 |
| 112 | `rastro_meta` | `meta_trace` | SAIDA-RESPOSTA | internal/outbound/handler.go:301 |
| 113 | `reacao` | `reaction` | SAIDA-EVENTO + ENTRADA | internal/meta/types.go:541 (event); internal/outbound/mensagem.go:637 (request) |
| 114 | `subcodigo_meta` | `meta_subcode` | SAIDA-RESPOSTA | internal/outbound/handler.go:293 |
| 115 | `token_instagram` | `instagram_token` | SAIDA-RESPOSTA | internal/outbound/estado.go:192 |
| 116 | `token_meta` | `meta_token` | SAIDA-RESPOSTA | internal/outbound/estado.go:169 |
| 117 | `valor` | `value` | SAIDA-RESPOSTA | internal/outbound/estado.go:432 |
| 118 | `veredito` | `verdict` | SAIDA-RESPOSTA | internal/outbound/estado.go:513; internal/outbound/saude_handler.go:94; internal/outbound/sonda_externa.go:152; internal/outbound/vigia.go:142 |
| 119 | `voz` | `voice` | SAIDA-EVENTO | internal/meta/types.go:575 |

## 8. What does NOT change — output keys already in English

**Absence from tables A and B above does not mean a key is Portuguese.** These output keys
(`SAIDA-EVENTO` or `SAIDA-RESPOSTA`) already leave this gateway in English today, before any
migration step runs. None of them is renamed by step 4 of T-189 — they simply stay as they are.
Measured by sweeping every `json:"…"` tag on every output struct in `internal/meta` and
`internal/outbound`, T-198 (`docs/INVENTARIO-CHAVES.md`, item 4).

| english key | file:line | struct | direction | route(s) |
|---|---|---|---|---|
| `media_id` | `internal/outbound/media_handler.go:260` | (map literal, no named struct) | SAIDA-RESPOSTA | `POST /v1/media` — **this is the exact pair of `midia_id`, section 5 above** |
| `latitude` | `internal/meta/types.go:86` | `Location` (in `Event.localizacao`) | SAIDA-EVENTO | webhook |
| `longitude` | `internal/meta/types.go:87` | `Location` (in `Event.localizacao`) | SAIDA-EVENTO | webhook |
| `id` | `internal/meta/types.go:419` | `Event` | SAIDA-EVENTO | webhook |
| `phone_number_id` | `internal/meta/types.go:424` | `Event` | SAIDA-EVENTO | webhook |
| `waba_id` | `internal/meta/types.go:425` | `Event` | SAIDA-EVENTO | webhook |
| `timestamp` | `internal/meta/types.go:427` | `Event` | SAIDA-EVENTO | webhook |
| `wa_message_id` | `internal/meta/types.go:430` | `Event` | SAIDA-EVENTO | webhook |
| `status` | `internal/meta/types.go:584` | `Event` | SAIDA-EVENTO | webhook |
| `template` | `internal/meta/types.go:617` | `Event` (field key, holds `TemplateStatus`) | SAIDA-EVENTO | webhook |
| `ig_id` | `internal/outbound/estado.go:62` | `State` | SAIDA-RESPOSTA | `GET /v1/estado` |
| `wa_id` | `internal/outbound/bloqueio_handler.go:136` | `blockItemResponse` | SAIDA-RESPOSTA | `POST/DELETE /v1/bloqueios` |
| `wa_id` | `internal/outbound/bloqueio_handler.go:145` | `blockFailureResponse` | SAIDA-RESPOSTA | `POST/DELETE /v1/bloqueios` |
| `wa_id` | `internal/outbound/bloqueio_handler.go:166` | `blockListItem` | SAIDA-RESPOSTA | `GET /v1/bloqueios` |
| `templates` | `internal/outbound/templates_handler.go:348` | `templatesResponse` | SAIDA-RESPOSTA | `GET /v1/templates` |
| `id` | `internal/outbound/templates_handler.go:388` | `templateCreatedResponse` | SAIDA-RESPOSTA | `POST /v1/templates` |
| `status` | `internal/outbound/templates_handler.go:389` | `templateCreatedResponse` | SAIDA-RESPOSTA | `POST /v1/templates` |
| `id` | `internal/outbound/templates_handler.go:542` | `templateEntry` | SAIDA-RESPOSTA | `DELETE /v1/templates` (ambiguous outcome) |
| `status` | `internal/outbound/templates_handler.go:545` | `templateEntry` | SAIDA-RESPOSTA | `DELETE /v1/templates` (ambiguous outcome) |
| `ok` | `internal/outbound/saude_handler.go:83` | `healthResponse` | SAIDA-RESPOSTA | `GET /v1/instances/{slug}/health` |
| `wa_message_id` | `internal/outbound/fumaca_handler.go:131` | `SmokeResponse` | SAIDA-RESPOSTA | `POST /v1/fumaca` |
| `about`,`address`,`description`,`email`,`profile_picture_url`,`websites`,`vertical` | `internal/meta/perfil.go:65-71` | `Profile` | SAIDA-RESPOSTA | `GET /v1/perfil` |
| `about`,`address`,`description`,`email`,`websites`,`vertical`,`profile_picture_handle` | `internal/meta/perfil.go:92-103` | `ProfilePatch` (echoed in `profileWriteResponse.gravado`) | SAIDA-RESPOSTA **and** ENTRADA | `POST /v1/perfil` |

## 9. Multi-direction keys — the dangerous ones

**21 of the 119 keys above carry more than one direction.** Each is a `media_id` waiting to
happen: whoever renames one occurrence without checking the others breaks the sibling direction
silently, exactly like section 5. List, by table:

**Table A (14 keys):** `categoria`, `erro`, `estado`, `idioma`, `instancia`, `mensagem`, `motivo`,
`nome`, `nome_arquivo`, `numero_exibido`, `responder_a`, `template`, `texto`, `tipo`.

**Table B (7 keys):** `alvo`, `componentes`, `emoji`, `endereco`, `legenda`, `localizacao`,
`reacao`.

Six of the seven in table B (`alvo`, `emoji`, `endereco`, `legenda`, `localizacao`, `reacao`) share
the same reason: the reaction/location vocabulary is deliberately identical on send and receive
(`internal/outbound/mensagem.go:271-274`), so the same word is a real field in both an outgoing
`Event` and an incoming `Request`.

## 10. Rows where no measurable contract key exists (`A MEDIR`)

Six rows in table A are `A MEDIR` because the proposed Portuguese string does not correspond to
any actual JSON field in this contract today — never a guess, each one names what was found
instead:

- `assinatura_esperada`, `corpo`, `segredo_entrega` — only exist inside one internal test vector
  file (`testdata/assinatura-entrega.json`, read by `internal/inbound/deliver_test.go`), which pins
  down an HMAC signature computation, not the envelope's schema. No consumer ever sees these three
  keys.
- `data` — the only `"data"` occurrences in the code are the Meta Graph API's own pagination
  wrapper (`{"data": [...]}`), decoded internally and never re-exposed under that name.
- `de` — only `de_cru` and `de_canonico` exist; there is no bare `de` field.
- `instancias` — only exists as a CLI flag (`--instancias`, `cmd/zapgw/provisionar.go:1476`), never
  as an HTTP/JSON field.

## 7. The 29 names, decided on 2026-08-31 — and the collision check that came with them

Table B's English column was `A DECIDIR` until 2026-08-31. The planner filled it in on that date,
following the conventions **already present in table A**, not invented here:

- `conferido_em` -> `checked_at`, so `gerado_em` -> `generated_at` and `medido_em` -> `measured_at`.
- `carimbos_desde` -> `stamps_since`, so `falhando_desde` -> `failing_since`.
- `alerta_de_conta` -> `account_alert` (modifier first), so `token_meta` -> `meta_token`,
  `codigo_meta` -> `meta_code`, `certificado_do_callback` -> `callback_certificate`.
- `emoji` is the same word in both languages and **does not change** — like `payload`, `template`,
  `templates`, `total` and `via` already noted by the consumer.

🔴 **The collision check, because this is the failure mode the consumer named.** Their rule —
*only rename if the destination name does not already exist in that dictionary* — exists because our
English for `texto` is `text`, and `text` is also Meta's own key inside a message object. Several of
these 29 have English forms that Meta also uses: `components`, `location`, `caption`, `reaction`,
`voice`, `address`, `code`.

**Measured on 2026-08-31**, by reading the sibling `json` tags of every struct where these keys are
emitted (`internal/meta/types.go:67,89,141,158,540,574,579,607`, `internal/meta/templates.go:99`):
**every sibling in those objects is one of ours**, all still Portuguese. **No Meta key shares an
object with any of these 29** — the Meta vocabulary that passes through untouched lives in `cru` and
in the pass-through objects, which are not visited.

⚠️ **That measurement is a snapshot, not a guarantee.** If a future change puts a raw Meta object
alongside one of these fields, the collision becomes real and the consumer's rule is what saves it.
The rule is the mechanism; this measurement only says the mechanism has nothing to do today.

## 8. The VALUE vocabularies, decided on 2026-08-31

🔴 **Owner's decision, and it is the same one from 2026-08-30 — it was never narrower than this:**
*"o projeto precisa ser em ingles"*. The example that followed it (*"se a chave chama nome, tem que
passar a se chamar name"*) named a key because a key was what was in front of him; **it did not
narrow the rule to keys**. A Portuguese word in the contract is a Portuguese word in the contract,
whether it sits left or right of the colon. `{"kind": "texto"}` is not a migrated contract.

🔴 **`tipo` is FOUR vocabularies sharing one JSON key.** A global value map — *"wherever `texto`
appears, write `text`"* — would rewrite objects that are not in the conversation. **Every table below
is scoped to its own object**, and the consumer's rule (rename only if the destination does not
already exist in THAT dictionary) applies per object, not per key name.

`*` marks a value that is already the same word in both languages and **does not change**.

### 8.1 `tipo` — top-level message type (`Request.Type`, ENTRADA)

| pt | en | | pt | en |
|---|---|---|---|---|
| `texto` | `text` | | `reacao` | `reaction` |
| `template` | `template` * | | `localizacao` | `location` |
| `botoes` | `buttons` | | `contatos` | `contacts` |
| `cta_url` | `cta_url` * | | `flow` | `flow` * |
| `lista` | `list` | | `midia` | `media` |
| `pedir_localizacao` | `request_location` | | | |

### 8.2 `tipo` — outgoing event type (`meta.EventType`, SAIDA-EVENTO)

| pt | en |
|---|---|
| `mensagem` | `message` |
| `status` | `status` * |
| `template_status` | `template_status` * |
| `template_categoria` | `template_category` |
| `qualidade_do_numero` | `number_quality` |
| `alerta_de_conta` | `account_alert` — same pair the KEY table already uses |

### 8.3 `tipo` — template button type (inside `botoes_template[]`, ENTRADA)

| pt | en |
|---|---|
| `url` | `url` * |
| `resposta_rapida` | `quick_reply` |

### 8.4 `tipo` — instance type — **already English** (`whatsapp`, `instagram`). Nothing to do.

### 8.5 `categoria` — media category (`Request.Category`, ENTRADA)

| pt | en |
|---|---|
| `imagem` | `image` |
| `video` | `video` * |
| `audio` | `audio` * |
| `documento` | `document` |
| `sticker` | `sticker` * |

### 8.6 `classe` — error classification (`meta.ErrorClass`, SAIDA-RESPOSTA)

| pt | en |
|---|---|
| `retentavel` | `retryable` |
| `permanente` | `permanent` |
| `config` | `config` * |
| `desconhecido` | `unknown` |

### 8.7 `estado` — observation state (SAIDA-RESPOSTA, shared by several state blocks)

| pt | en |
|---|---|
| `nunca_observado` | `never_observed` |
| `observado` | `observed` |
| `nao_se_aplica` | `not_applicable` |
| `nao_configurado` | `not_configured` |
| `desconhecido` | `unknown` |
| `nao_consegui_verificar` | `could_not_verify` |

### 8.8 / 8.9 / 8.10 — the three `veredito` vocabularies (SAIDA-RESPOSTA)

They share a key name and **not** a word list, so each is decided on its own. Where the same
Portuguese word appears in more than one, **the English word is the same** — a value that translates
two different ways depending on the block would be a trap of our own making.

| pt | en | which verdict |
|---|---|---|
| `ok` | `ok` * | WhatsApp token, Instagram token |
| `recusado` | `refused` | WhatsApp token |
| `desconhecido` | `unknown` | WhatsApp token |
| `nao_se_aplica` | `not_applicable` | Instagram token, health |
| `aguardando` | `pending` | Instagram token |
| `falhando` | `failing` | Instagram token |
| `expirado` | `expired` | Instagram token |

### 8.11 `contadores` — the counter-name vocabulary

The counter names are values inside `contadores`, and the KEY table already carries their pairs.
**`nome_antigo_usado` is the one that was missing** — it was created by T-203/T-205 and never added
to any published table, by the exact failure this document exists to prevent. Its pair:

| pt | en |
|---|---|
| `nome_antigo_usado` | `old_name_used` |

