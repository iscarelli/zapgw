Código: internal/config/contador.go, internal/config/store.go, internal/meta/types.go,
internal/meta/media.go, internal/meta/errors.go, internal/outbound/mensagem.go,
internal/outbound/estado.go, internal/outbound/estado_handler.go, internal/outbound/entrada.go,
internal/outbound/sonda_externa.go, internal/outbound/lideranca.go, internal/outbound/vigia.go,
internal/outbound/saude_handler.go, internal/outbound/handler.go,
internal/outbound/bloqueio_handler.go, internal/outbound/media_handler.go,
internal/outbound/templates_handler.go, internal/outbound/perfil_handler.go

# T-206 — what a `json:"…"` sweep cannot see

This is a MEASUREMENT, not the migration table. **No English name is decided here** — every
"english" column below reads `A DECIDIR`. That choice belongs to T-189 step 4 and is the
planner's, not this task's.

`docs/MIGRACAO-CONTRATO-EN.md` (119 rows) and `docs/INVENTARIO-CHAVES.md` (29 rows) inventory
**keys** — every one of them found by sweeping `json:"…"` struct tags. This document inventories
four things that sweep cannot see, because none of them is a struct tag:

1. Closed vocabularies of **literal values** — a `tipo` of `"texto"` is a VALUE, and no `json:`
   sweep of the code finds it. Found by reading `const`/`iota` blocks, `switch` statements over a
   string field, and comparisons against a literal.
2. Four **ENTRADA keys** that exist in the contract today and are missing from both tables above.
3. **Query parameters** — a fourth direction, `ENTRADA-QUERY`, that the two tables above never
   defined (their "ENTRADA" is explicitly "the body the consumer sends").
4. A **count of every counter that exists in the code today**, against the closed vocabulary of
   counter constants (`internal/config/contador.go`).

## 1. Closed vocabularies that travel as VALUE

**11 vocabularies found, one per section below.** Value count per section: 1.1=11, 1.2=6, 1.3=2,
1.4=2, 1.5=5, 1.6=4, 1.7=6, 1.8=3, 1.9=5, 1.10=1, 1.11=19 — **64 value occurrences in total**. Two
values (`desconhecido` and `nao_se_aplica`) are shared, by aliasing, across more than one of these
sections (noted inline in 1.7-1.10); this count is per-section, not a deduplicated global count of
distinct strings, because a translator working section by section needs to know how many times each
occurrence must be handled, not how many unique words exist.

🔴 **The single most valuable finding of this task, flagged as instructed:** the `Why` that opened
this task named ONE `tipo` vocabulary with six example values (`texto`, `botoes`, `cta_url`,
`midia`, `reacao`, `template`). **The code carries not one but FOUR separate closed vocabularies
under the same JSON key `tipo`, in four different objects** (1.1-1.4 below) — the top-level
message type (11 values, not 6), the outgoing event type, the template-button type, and the
instance type. A translator that renames `"tipo":"texto"` -> `"kind":"text"` assuming a single
`tipo` vocabulary would be reading the wrong dictionary for three of the four objects the moment it
runs.

### 1.1 `tipo` — top-level message type (`Request.Type`)

Direction: **ENTRADA**. Field: `tipo`, `internal/outbound/mensagem.go:514` (json tag, see
`docs/MIGRACAO-CONTRATO-EN.md` row 81). Values enforced by the `switch` in `Request.Validate`:

| value | arquivo:linha | english |
|---|---|---|
| `texto` | internal/outbound/mensagem.go:749 | A DECIDIR |
| `template` | internal/outbound/mensagem.go:753 | A DECIDIR |
| `botoes` | internal/outbound/mensagem.go:774 | A DECIDIR |
| `cta_url` | internal/outbound/mensagem.go:809 | A DECIDIR |
| `lista` | internal/outbound/mensagem.go:825 | A DECIDIR |
| `pedir_localizacao` | internal/outbound/mensagem.go:838 | A DECIDIR |
| `midia` | internal/outbound/mensagem.go:868 | A DECIDIR |
| `reacao` | internal/outbound/mensagem.go:897 | A DECIDIR |
| `localizacao` | internal/outbound/mensagem.go:901 | A DECIDIR |
| `contatos` | internal/outbound/mensagem.go:905 | A DECIDIR |
| `flow` | internal/outbound/mensagem.go:909 | A DECIDIR |

**11 values.** Any string outside this list is refused (`ErrUnknownType`,
`internal/outbound/mensagem.go:938`).

### 1.2 `tipo` — outgoing event type (`Event.Type` / `meta.EventType`)

Direction: **SAIDA-EVENTO**. Field: `tipo`, `internal/meta/types.go:413` (json tag, matches
`docs/MIGRACAO-CONTRATO-EN.md` row 81's SAIDA-EVENTO occurrence). Values, `const` block:

| value | arquivo:linha | english |
|---|---|---|
| `mensagem` | internal/meta/types.go:9 | A DECIDIR |
| `status` | internal/meta/types.go:10 | A DECIDIR |
| `template_status` | internal/meta/types.go:11 | A DECIDIR |
| `template_categoria` | internal/meta/types.go:21 | A DECIDIR |
| `qualidade_do_numero` | internal/meta/types.go:26 | A DECIDIR |
| `alerta_de_conta` | internal/meta/types.go:31 | A DECIDIR |

**6 values.** `status` is already the same spelling in both languages — no rename needed, like
`template`/`via`/`total` noted in section 4 of `docs/MIGRACAO-CONTRATO-EN.md`.

### 1.3 `tipo` — template button type (`TemplateButtonUnion.Type`, inside `botoes_template[]`)

Direction: **ENTRADA**. Field: `tipo`, `internal/outbound/mensagem.go:237` (json tag, nested
inside each item of `botoes_template[]`, itself `internal/outbound/mensagem.go:559`). Values,
`switch` in `validateTemplateButtons`:

| value | arquivo:linha | english |
|---|---|---|
| `url` | internal/outbound/mensagem.go:1182 | A DECIDIR |
| `resposta_rapida` | internal/outbound/mensagem.go:1192 | A DECIDIR |

**2 values.** Any other string is refused (`ErrUnknownTemplateButtonType`,
`internal/outbound/mensagem.go:1208`).

### 1.4 `tipo` — instance type (`State.Type`)

Direction: **SAIDA-RESPOSTA**. Field: `tipo`, `internal/outbound/estado.go:51` (json tag, matches
`docs/MIGRACAO-CONTRATO-EN.md` row 81's SAIDA-RESPOSTA occurrence). Values, `const` block:

| value | arquivo:linha | english |
|---|---|---|
| `whatsapp` | internal/config/store.go:65 | (unchanged — already English) |
| `instagram` | internal/config/store.go:66 | (unchanged — already English) |

**2 values, and both already English** — like `via`/`total`/`emoji`, this vocabulary needs no rename
at all. Included for completeness: its absence from this document would otherwise read as "still
Portuguese," which is wrong.

### 1.5 `categoria` — media category (`Request.Category`)

Direction: **ENTRADA**. Field: `categoria`, `internal/outbound/mensagem.go:627` — the `midia`
section of `Request`, **not** the same `categoria` occurrence in
`docs/MIGRACAO-CONTRATO-EN.md` row 15, which is the message-TEMPLATE category
(`internal/meta/types.go:181,235`, `internal/outbound/templates_handler.go:363,390`). **Same JSON
key name, two structurally different closed vocabularies, in different objects** — the template
one is Meta's own literal (`UTILITY`/`MARKETING`/`AUTHENTICATION`, excluded from this document per
the rule in `internal/config/contador.go:148-156`: Meta's own values are never translated). The
media category below is OURS. Values, `const` block:

| value | arquivo:linha | english |
|---|---|---|
| `imagem` | internal/meta/media.go:53 | A DECIDIR |
| `video` | internal/meta/media.go:54 | (unchanged — already English) |
| `audio` | internal/meta/media.go:55 | (unchanged — already English) |
| `documento` | internal/meta/media.go:56 | A DECIDIR |
| `sticker` | internal/meta/media.go:57 | (unchanged — already English) |

**5 values, 2 in Portuguese.** The comment at `internal/meta/media.go:48` states this vocabulary
IS the consumer contract (`"categoria": "audio"`), by design.

**Reused, restricted, for the template header's own `tipo` field**
(`TemplateHeader.Type`, json `cabecalho.tipo`, `internal/outbound/mensagem.go:175`, ENTRADA): the
comment at `internal/outbound/mensagem.go:164-166` states this is deliberately the SAME table as
`categoria` above — but restricted to the three categories in the `mediaHeaders` map
(`internal/outbound/mensagem.go:1061-1065`: `imagem`, `video`, `documento` — `audio` and `sticker`
excluded, comment explains why at lines 1057-1060) — **plus one literal not from this vocabulary
at all**, `"texto"` (`internal/outbound/mensagem.go:1085`, hardcoded, checked before the category
table is consulted). So `cabecalho.tipo` accepts exactly `{texto, imagem, video, documento}` — 4
of the 6 possible tokens across the two sources, not a fifth vocabulary of its own.

### 1.6 `classe` — error classification (`meta.ErrorClass`)

Direction: **SAIDA-RESPOSTA**. Field: `classe`, `internal/outbound/handler.go:277`
(`errorResponse.Error.Class`, the shared-error struct that
`docs/INVENTARIO-CHAVES.md` row 5 already lists as a KEY — this is its VALUE vocabulary, which
that document does not cover). Values, `const` block:

| value | arquivo:linha | english |
|---|---|---|
| `retentavel` | internal/meta/errors.go:29 | A DECIDIR |
| `permanente` | internal/meta/errors.go:31 | A DECIDIR |
| `config` | internal/meta/errors.go:33 | (unchanged — already English) |
| `desconhecido` | internal/meta/errors.go:46 | A DECIDIR |

**4 values.** This is the closed vocabulary the `Why` of this task called out by name: *"decide se
eles reenviam"* — every consumer that reads `classe` to decide whether to retry reads one of these
four strings. `desconhecido` (`meta.ClassUnknown`) is reused, by aliasing, in three of the
vocabularies below (1.8, 1.9) — see the cross-reference in each.

### 1.7 `estado` — observation-state family (shared across several state blocks)

Direction: **SAIDA-RESPOSTA**, always inside `GET /v1/estado`. Field: `estado`, used by
`CertificateInState.State` (`internal/outbound/estado.go:356`), `ObservedValue.State`
(`internal/outbound/estado.go:430`), `LeadershipInState.State` (`internal/outbound/lideranca.go:220`),
`ConnectorInState.State` (`internal/outbound/entrada.go:179`) and `ExternalReachInState.State`
(`internal/outbound/sonda_externa.go:145`) — five different blocks of the same response,
deliberately sharing one word list (comment at `internal/outbound/lideranca.go:216-219`: *"uses the
SAME vocabulary this package already uses… new vocabulary would force the consumer to learn a
second table"*). No single block uses all six values — each block uses its own subset; the table
below is the union, with the canonical definition of each literal:

| value | arquivo:linha (canonical definition) | used by (alias, if any) | english |
|---|---|---|---|
| `nunca_observado` | internal/outbound/estado.go:293 (`CertNeverObserved`) | — | A DECIDIR |
| `observado` | internal/outbound/estado.go:295 (`CertObserved`) | `ConnectorObserved` (internal/outbound/entrada.go:155), `ReachStateObserved` (internal/outbound/sonda_externa.go:100) | A DECIDIR |
| `nao_se_aplica` | internal/outbound/estado.go:316 (`NotApplicable`) | `VerdictIGTokenNotApplicable` (internal/outbound/estado.go:474) | A DECIDIR |
| `nao_configurado` | internal/outbound/entrada.go:170 (`ConnectorNotConfigured`) | `ReachStateNotConfigured` (internal/outbound/sonda_externa.go:106) | A DECIDIR |
| `desconhecido` | internal/meta/errors.go:46 (`meta.ClassUnknown`, see 1.6) | `VerdictUnknown` (internal/outbound/vigia.go:70), `ConnectorUnknown` (internal/outbound/entrada.go:160) | A DECIDIR |
| `nao_consegui_verificar` | internal/outbound/sonda_externa.go:115 (`ReachStateCouldNotVerify`) | — | A DECIDIR |

**6 values.** `desconhecido` is the SAME literal as 1.6's `classe` value, reused on purpose
(comment at `internal/outbound/vigia.go:52-55`: *"not new vocabulary… a second vocabulary for the
same concept would force the consumer to learn two tables"*) — a translation of `classe` that
doesn't also touch every `estado`/`veredito` occurrence of `desconhecido` breaks the "same word,
same meaning" guarantee the comment describes.

### 1.8 `veredito` — WhatsApp token verdict (`MetaToken.Verdict`, `token_meta` block)

Direction: **SAIDA-RESPOSTA**. Field: `veredito`, `internal/outbound/vigia.go:142`. Values,
`const` block:

| value | arquivo:linha | english |
|---|---|---|
| `ok` | internal/outbound/vigia.go:60 | (unchanged — already English) |
| `recusado` | internal/outbound/vigia.go:66 | A DECIDIR |
| `desconhecido` | internal/outbound/vigia.go:70 (`= string(meta.ClassUnknown)`, canonical definition at internal/meta/errors.go:46) | A DECIDIR |

**3 values** — a DIFFERENT closed vocabulary than 1.9 below, even though both live under the same
JSON key `veredito`: comment at `internal/outbound/estado.go:459-462` states explicitly *"they are
their OWN words, not a reuse of VerdictOK/VerdictRefused… forcing the same vocabulary would hide
[the] difference"*.

### 1.9 `veredito` — Instagram token verdict (`InstagramTokenInState.Verdict`, `token_instagram` block)

Direction: **SAIDA-RESPOSTA**. Field: `veredito`, `internal/outbound/estado.go:513`. Values,
`const` block:

| value | arquivo:linha | english |
|---|---|---|
| `nao_se_aplica` | internal/outbound/estado.go:474 (`= NotApplicable`, canonical definition at internal/outbound/estado.go:316) | A DECIDIR |
| `aguardando` | internal/outbound/estado.go:478 | A DECIDIR |
| `ok` | internal/outbound/estado.go:485 | (unchanged — already English) |
| `falhando` | internal/outbound/estado.go:492 | A DECIDIR |
| `expirado` | internal/outbound/estado.go:495 | A DECIDIR |

**5 values.** A translator that renames `veredito` values globally (rather than per block) would
turn `token_meta.veredito:"ok"` and `token_instagram.veredito:"ok"` into the same output either
way — harmless here, by coincidence — but would also try to rename `token_meta`'s three-value set
using this five-value dictionary, silently mismatching `recusado` (1.8, has no counterpart here) and
`aguardando`/`falhando`/`expirado` (here, have no counterpart in 1.8).

### 1.10 `veredito` — health verdict (`healthResponse.Verdict`, `GET /v1/instances/{slug}/health`)

Direction: **SAIDA-RESPOSTA**. Field: `veredito,omitempty`, `internal/outbound/saude_handler.go:94`.
Measured: the field is assigned exactly ONCE in the whole package, always the same literal:

| value | arquivo:linha | english |
|---|---|---|
| `nao_se_aplica` | internal/outbound/saude_handler.go:155 (`= NotApplicable`, internal/outbound/estado.go:316) | A DECIDIR |

**1 value, `omitempty` on every other outcome** (the field is absent from the JSON unless the
instance's type makes it not-applicable — comment at `internal/outbound/saude_handler.go:86-90`).
Listed on its own because it shares the JSON key `veredito` with 1.8 and 1.9 but is neither: not a
subset, a single fixed value.

### 1.11 `contadores` — the counter-name vocabulary

Direction: **SAIDA-RESPOSTA**, `contadores` object of `GET /v1/estado`
(`internal/outbound/estado.go:111,276`, `docs/MIGRACAO-CONTRATO-EN.md` row 25). Each entry below is
a MAP KEY inside `contadores`, written from a Go `const`, never a struct `json:` tag — this is
exactly why the migration table's sweep never found it. Full list, `KeysInDisplayOrder`
(`internal/config/contador.go:278-298`), in that order:

| # | value | arquivo:linha | english |
|---|---|---|---|
| 1 | `recebidas` | internal/config/contador.go:32 | A DECIDIR |
| 2 | `entregues` | internal/config/contador.go:33 | A DECIDIR |
| 3 | `recusadas_pelo_consumidor` | internal/config/contador.go:34 | A DECIDIR |
| 4 | `numero_descartado` | internal/config/contador.go:76 | A DECIDIR |
| 5 | `conta_descartada` | internal/config/contador.go:45 | A DECIDIR |
| 6 | `alarme_perda_definitiva` | internal/config/contador.go:35 | A DECIDIR |
| 7 | `enviadas` | internal/config/contador.go:36 | A DECIDIR |
| 8 | `falhas_de_envio` | internal/config/contador.go:37 | A DECIDIR |
| 9 | `leituras_marcadas` | internal/config/contador.go:100 | A DECIDIR |
| 10 | `falhas_de_leitura` | internal/config/contador.go:101 | A DECIDIR |
| 11 | `templates_apagados` | internal/config/contador.go:121 | A DECIDIR |
| 12 | `nome_antigo_usado` | internal/config/contador.go:136 | A DECIDIR |
| 13 | `cobranca_marketing` | internal/config/contador.go:167 | A DECIDIR (prefix only — suffix is Meta's literal, see below) |
| 14 | `cobranca_utility` | internal/config/contador.go:168 | A DECIDIR (prefix only) |
| 15 | `cobranca_authentication` | internal/config/contador.go:169 | A DECIDIR (prefix only) |
| 16 | `cobranca_service` | internal/config/contador.go:170 | A DECIDIR (prefix only) |
| 17 | `cobranca_outra` | internal/config/contador.go:199 | A DECIDIR |
| 18 | `cobranca_ausente` | internal/config/contador.go:215 | A DECIDIR |
| 19 | `cobranca_cobravel` | internal/config/contador.go:232 | A DECIDIR |

**19 values — see section 4 below for the reconciliation against "the table with 19" the `Why`
referred to.** Counters 13-16 are a `cobranca_` (ours) + Meta's own category name (`marketing`,
`utility`, `authentication`, `service`) glued together (comment at
`internal/config/contador.go:148-156`) — only the `cobranca_` prefix is a candidate for renaming;
the suffix must stay byte-for-byte equal to what Meta sends, or the two vocabularies (ours and
Meta's, sitting inside the same string) silently diverge the day Meta adds a category.

## 2. ENTRADA keys missing from both existing tables

None of these four is a `json:"…"` tag that a struct-tag sweep would find in the KEY position
`docs/MIGRACAO-CONTRATO-EN.md` and `docs/INVENTARIO-CHAVES.md` already cover for `botao_titulo`,
`botoes`, `botoes_template` and the bodies of the other routes — these four are separate, nested or
out-of-band fields those sweeps did not visit.

| # | key | arquivo:linha | field it travels in | direction |
|---|---|---|---|---|
| 1 | `titulo` | internal/outbound/mensagem.go:153 | `Button.Title`, one item of `botoes[]` (`Request.Buttons`, `internal/outbound/mensagem.go:579`) — **different field than `botao_titulo`** (`docs/MIGRACAO-CONTRATO-EN.md` row 7, `internal/outbound/mensagem.go:587`, the `cta_url`/`lista`/`flow` single-button title) | ENTRADA |
| 2 | `indice` | internal/outbound/mensagem.go:235 | `TemplateButtonUnion.Index`, one item of `botoes_template[]` | ENTRADA |
| 3 | `telefones` | internal/outbound/bloqueio_handler.go:102 | `BlockRequest.Phones`, body of `POST /v1/bloqueios` and `DELETE /v1/bloqueios` | ENTRADA |
| 4 | `arquivo` | internal/outbound/media_handler.go:74 | **not a `json:"…"` tag at all** — `const partName = "arquivo"`, the multipart FIELD NAME required in the `multipart/form-data` body of `POST /v1/media` (matched at `internal/outbound/media_handler.go:273`, `part.FormName() == partName`) | ENTRADA |

## 3. `ENTRADA-QUERY` — query parameters

The two existing tables define ENTRADA as *"the body the consumer sends"* — query parameters are
neither a body key nor a JSON key of any kind, so they are invisible to both. This is a fourth
direction this document introduces: **`ENTRADA-QUERY`**.

**Measured by sweeping every `r.URL.Query().Get(…)` call in `internal/`, excluding `_test.go`
files** (those exercise this gateway's OWN client code against a fake Meta server, or this
gateway's own handler tests — neither is the consumer-facing contract). **9 distinct parameter
names, 13 call sites, across 6 routes** — more than the 3 routes the `Why` named for `instancia`
alone; `instancia` itself appears in 5 of the 6.

| # | param | route(s) | arquivo:linha | english |
|---|---|---|---|---|
| 1 | `instancia` | `POST /v1/media`, `GET /v1/media/{id}` (shared `instanceAuthorized`) | internal/outbound/media_handler.go:128 | A DECIDIR |
| 2 | `mime_do_payload` | `GET /v1/media/{id}` | internal/outbound/media_handler.go:296 | A DECIDIR |
| 3 | `instancia` | `GET /v1/estado` | internal/outbound/estado_handler.go:192 | A DECIDIR |
| 4 | `serie_dias` | `GET /v1/estado` | internal/outbound/estado_handler.go:219 | A DECIDIR |
| 5 | `instancia` | `GET /v1/bloqueios` | internal/outbound/bloqueio_handler.go:376 | A DECIDIR |
| 6 | `limit` | `GET /v1/bloqueios` | internal/outbound/bloqueio_handler.go:410 | (unchanged — already English) |
| 7 | `after` | `GET /v1/bloqueios` | internal/outbound/bloqueio_handler.go:419 | (unchanged — already English) |
| 8 | `before` | `GET /v1/bloqueios` | internal/outbound/bloqueio_handler.go:420 | (unchanged — already English) |
| 9 | `instancia` | `GET /v1/perfil` | internal/outbound/perfil_handler.go:123 | A DECIDIR |
| 10 | `instancia` | `GET /v1/templates` | internal/outbound/templates_handler.go:416 | A DECIDIR |
| 11 | `status` | `GET /v1/templates` | internal/outbound/templates_handler.go:436 | (unchanged — already English) |
| 12 | `instancia` | `DELETE /v1/templates` | internal/outbound/templates_handler.go:598 | A DECIDIR |
| 13 | `nome` | `DELETE /v1/templates` | internal/outbound/templates_handler.go:606 | A DECIDIR |

**Note on `POST /v1/perfil`:** it also carries an `instancia` value, but as a BODY key
(`ProfileRequest.Instance`, `internal/outbound/perfil_handler.go:175`), not a query parameter — it
is `ENTRADA`, already inside the `instancia`/`instance` occurrence of
`docs/MIGRACAO-CONTRATO-EN.md` row 45, not a fifth row here.

## 4. The counter reconciliation

**19 counters exist in the code today** — every one of them listed in section 1.11 above, and every
one of them present in `internal/config/contador.go:278-298` (`KeysInDisplayOrder`), the single
source `buildCounterKeys()` derives the closed vocabulary from
(`internal/config/contador.go:306-312`). **No orphan counter exists**: a repo-wide sweep for
`IncrementCounter`/counter-writing call sites (`internal/outbound/*_test.go`,
`internal/inbound/*_test.go`, `internal/config/contador.go`, `internal/inbound/handler.go`) turned
up no literal counter key outside this list.

`nome_antigo_usado` (`CounterOldNameUsed`, `internal/config/contador.go:136`) **is one of these 19**
— it was added by T-203 on 2026-08-31, the same date this task's `Why` was written, and it IS
counted in the 19 above. What is missing is not the counter itself but its **English pair**: no
durable document in this repository lists an English name for any of the 19 counters (the private,
per-consumer table the `Why` refers to as "the table with 19" lives outside this repository, in the
gitignored `*.local.md` channel files — see `docs/TASKS.md`'s T-189 entry — and this task does not
read or copy that private table; section 1.11 above is this repository's first DURABLE record of
the full counter vocabulary, with every value's `english` column left `A DECIDIR`).
