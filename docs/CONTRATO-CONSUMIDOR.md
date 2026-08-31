# Consumer contract

*[Leia em português](CONTRATO-CONSUMIDOR.pt-BR.md)*

**Code:** `internal/inbound/deliver.go`, `internal/inbound/handler.go`,
`internal/inbound/mirror.go`, `internal/inbound/testdata/assinatura-entrega.json`,
`internal/meta/types.go`, `internal/meta/parse.go`, `internal/meta/client.go`,
`internal/meta/errors.go`, `internal/meta/media.go`, `internal/meta/templates.go`,
`internal/meta/leitura.go`, `internal/meta/numero.go`, `internal/meta/instagram.go`,
`internal/meta/bloqueio.go`,
`internal/outbound/handler.go`,
`internal/outbound/mensagem.go`, `internal/outbound/corpo.go`, `internal/outbound/saude_handler.go`,
`internal/outbound/media_handler.go`, `internal/outbound/templates_handler.go`,
`internal/outbound/leituras_handler.go`, `internal/outbound/bloqueio_handler.go`,
`internal/outbound/estado_handler.go`,
`internal/outbound/estado.go`, `internal/outbound/renovador_instagram.go`,
`internal/outbound/sonda_externa.go`,
`internal/outbound/cadastro_handler.go`, `internal/outbound/fumaca.go`,
`internal/outbound/fumaca_handler.go`, `internal/outbound/pausa_handler.go`,
`internal/config/store.go`, `internal/config/contador.go`, `cmd/zapgw/provisionar.go`,
`cmd/zapgw/main.go`.

*(That block is gateway maintenance metadata — it answers "which doc did this code change break?".
**You don't need it**, and nothing below depends on you having those files.)*

This document is what a consumer system needs in order to talk to zapgw — **in any language**. It
describes the current state; when the code changes, this file changes in the same commit.

**It was written to be sufficient on its own.** Everything you need to integrate and operate is
here: the bodies, the errors, the guarantees, the limits, and the three proofs you can run without
us (the signature test vector, the smoke test, and the state read).

## 🔴 Read these four things before anything else

### 1. There is no support channel — this document is the support

There is no e-mail address, ticket system, chat, or announcement list. **Nothing in this file asks
you to "let us know", "ask for" or "check with us"**: wherever there is a decision to make, it is
written here in a form you can execute on your own.

There is **one** person on the other side, and their reach is narrow: **whoever handed you the
slug**. They resolve what only they can resolve — reopening the 24 h registration window, rotating a
leaked consumer token, or telling you what happened to an instance that vanished. They are **not** a
channel for questions, for requesting a new field, or for traffic investigations.

**A breaking change is announced in one place only: the *Breaking changes* section at the end of
this file.** It is versioned in git alongside the code, and the way not to be surprised is to reread
it before rolling out a new integration. There is no notice through any other channel, and there is
no endpoint that declares a format version.

### 2. Today only the webhook is reachable from outside the gateway's network

**This is a DECISION by whoever operates the gateway, not unfinished work — and it decides how you
start.** Of all the routes described here, **only `POST /v1/inbound/{slug}`** (the webhook that
**Meta** calls) is published on the internet. Everything else — registration, sending, smoke test,
pause, read receipts, state, templates, media, health — answers **only on the gateway's LAN**, on an
internal entrypoint, **and stays that way until there is a need that justifies changing it**. There
is no date, and there will be no announcement of one.

Practical consequence: **an integrator who is not inside the gateway's network cannot reach
`POST /v1/cadastro`**, which is where **their own** `app_secret` and `token_envio` go through.
Whoever registers is whoever reaches the network — in practice, you hand your values to whoever gave
you the slug and that person runs the registration. **The rest of the flow stays yours**, from the
smoke test onward.

This document describes the routes as they are, not as they would be if they were published.

### 3. The conventions used in the examples, so you don't mistake an example for a real value

Every example in this file uses **the same fictitious markers**, and none of them exist:

| Marker | Value used | What it is |
|---|---|---|
| instance slug | `lojinha` | the **fictitious** slug of the examples. Yours comes in the delivery package, and it is a different one |
| phone (canonical form) | `5511999990000` | a **synthetic** number, with the 9th digit |
| phone (as Meta sometimes sends it) | `551199990000` | the **same** number without the 9th digit — this is the pair the `de_cru`/`de_canonico` section explains |
| Meta ids | `PNID_TESTE`, `WABA_TESTE`, `wamid.TESTE001`… | test identifiers, never real ones |
| base of the URLs **you** call | `https://zapgw.exemplo.com.br:8443` | **replace it with the registration URL you received** (item 2 of the package); the port is part of it |
| URL **Meta** calls | `https://zapgw.exemplo.com.br/v1/inbound/lojinha` | this is item 3 of the package, and the only one published on the internet — port 443, no explicit port |

**If an example value looks real, it isn't.** Slug, phone and id come from your delivery package and
from your Meta account — never from here.

### 4. How this document marks what is measured and what is assumed

Where a statement comes from **real traffic**, the text says the size of the measurement ("measured
across 267 real payloads"). Where it comes from **Meta's documentation**, it says the page and the
date it was read. Where it **was not verified**, it says so out loud instead of softening it. Treat
the three as different things: the first you can use as fact, the second as a third party's declared
intent, the third as a warning that there you are the one who will find out.

---

## What you receive when you are provisioned

The instance is created **manually** by whoever operates the gateway, and they supply the **slug** —
it is immutable and becomes a URL path. That is all: they **do not know, do not store and do not
ask for** your Meta account's data. You are the one who registers it, through the route in the next
section.

Check that you received **all five items**. The conversation happens once and the gateway has no
channel to notify anyone — a missing item here becomes stalled work on your side:

| # | What | What it's for |
|---|---|---|
| 1 | your instance's **slug** | it identifies the instance in **every** request body (`"instancia": "<slug>"`) |
| 2 | the **registration** URL (`POST …/v1/cadastro`) | this is where you register your Meta account |
| 3 | the **webhook** URL (`…/v1/inbound/<slug>`) | this is what **you** paste into *Callback URL* in Meta's panel **for your account** |
| 4 | `verify_token` and `segredo_entrega` | you type the first into *Verify Token* in Meta's panel; the second verifies the HMAC of every delivery the gateway makes to you (see obligation 3) |
| 5 | the **consumer token** | it is the `Authorization: Bearer` of every call you make |

The two values in item 4 are **drawn at random by the gateway and shown once** — it stores only the
encrypted form and never shows them again. If you did not receive them, **go back to whoever handed
you the slug before you start** (it is one of the few matters only they can resolve): without them
the webhook verification at Meta will not pass, and the delivery signature becomes impossible to
verify.

**Nothing else is missing.** These five items are the whole package — there is no sixth value that
someone forgot to send you, and no section of this document will ask for one. Anything not on the
list is either **yours** (your Meta account's data, which you register yourself in the next section)
or is written here.

**What you do NOT receive, and should not ask for:** anything from a third party's Meta account, and
no value back from what you register yourself. A secret goes in and does not come back — see the
registration section.

> ℹ️ **One value that does NOT travel in the package and that you will want to know: your instance's
> `timeout_ms`.** It is the deadline the gateway gives itself to talk to Meta during one of your
> calls, stored per instance. **It is not exposed on any route today** — not even in
> `GET /v1/estado` — and the default is **5000 ms**. Since the rule that matters is *"your HTTP
> client's timeout must be LONGER than the gateway's"*, you don't need the exact number: use a
> generous deadline (30 s is comfortable) and you get the gateway's answer instead of an error of
> your own. The case where the difference shows up is described in *Did it fail: does your key
> become valid again, or not?*.

## What each secret lets whoever steals it do

Written because protection on your side is your problem, and to size it you need to know what each
value is worth in someone else's hands:

| Secret | Whoever has it can |
|---|---|
| **consumer token** | 🔴 send messages through your instance **and RECONFIGURE IT** — see the warning below |
| `segredo_entrega` | forge a delivery your system will accept as coming from the gateway |
| `verify_token` | re-verify a webhook at Meta; on its own, it moves no traffic |
| `app_secret`, `token_envio` | they are **yours**, from your Meta account: whoever has them talks to Meta as you, inside and outside the gateway |

🔴 **The consumer token is no longer just "sends messages".** With the registration route, whoever
steals it **replaces the credentials and points your instance at their own Meta** — messages would
start going out through their number, and deliveries would go to their `callback_url`. This is an
accepted consequence of the model (it is what lets you sort things out on your own, without asking
us anything), and what limits it **is not permission, it is time**: once the 24 h window of the next
section has passed, a stolen token is back to just "sends messages", which is the risk that always
existed.

**What that demands of you:** treat the consumer token as an administration credential while the
window is open — do not leave it in a repository, in a log, in a terminal transcript, or in the
browser of a public dashboard. If it leaks, **go back to whoever handed you the slug immediately** —
that is the second of the matters only they can resolve: there is a rotation command on the gateway
side, and the previous token stops authenticating at that same instant.

## Register YOUR Meta account — `POST /v1/cadastro`

**The direction is you → gateway, and it is write-only.** The gateway does not return configuration:
it receives it.

```
POST /v1/cadastro
Authorization: Bearer <consumer token>
Content-Type: application/json
```

```jsonc
{
  "instancia":       "lojinha",          // the slug you received
  "waba_id":         "…",                // Business Settings → WhatsApp accounts
  "phone_number_id": "…",                // the id of the NUMBER in the Graph API (not the phone)
  "numero_exibido":  "5511999990000",    // the phone as it appears to the customer
  "app_secret":      "…",                // App → Settings → Basic → App Secret
  "token_envio":     "…",                // PERMANENT System User token
  "callback_url":    "https://…",        // where we deliver whatever Meta sends
  "bundle_ca":       ""                  // optional: PEM of YOUR CA, if you don't use a public CA
}
```

**Registration REPLACES: always send the complete set.** An omitted field counts as **empty**, not as
"leave it alone". This is deliberate — a partial registration would require you to know what is
already stored, and that is exactly what the gateway never returns. Re-registering is your
**rotation** path: changed the `app_secret` in Meta's panel, send the whole set again.

Two fields may be sent **empty**, and empty is a legitimate state:

- empty `callback_url` = **outbound-only instance**: the gateway delivers whatever Meta sends to
  nobody. This is not an error, it is a choice;
- empty `bundle_ca` = delivery uses the system CA store (the normal case). Fill it in only if your
  endpoint uses its **own CA** — that swaps the trust anchor, and does **not** turn off any
  verification. There is no mode for not verifying a certificate in this gateway, on any path.

`callback_url` must be `https://` for the reason in the section *Your `callback_url` must be
`https://`*, further below.

### The `200`: what was stored, without returning anything you sent

```jsonc
{
  "instancia": "lojinha",
  "estado": "pausada",
  "pausada": true,
  "janela_de_cadastro": {
    "aberta": true,
    "primeira_insercao_em": "2026-07-20T09:00:00Z",
    "fecha_em": "2026-07-21T09:00:00Z"
  },
  "cifrados": [
    { "campo": "app_secret",      "cadastrado": true  },
    { "campo": "verify_token",    "cadastrado": true  },
    { "campo": "token_envio",     "cadastrado": true  },
    { "campo": "callback_url",    "cadastrado": true  },
    { "campo": "segredo_entrega", "cadastrado": true  },
    { "campo": "bundle_ca",       "cadastrado": false }
  ],
  "proximo_passo": "esta instancia continua PAUSADA…"
}
```

🔴 **A secret goes in and does not come back — and this route makes no exception.** The response says
only **whether** each field is registered, never the value: not whole, not truncated, not hashed. An
endpoint that returned a credential would turn a leaked consumer token into theft of **your** Meta
account. Use `cifrados` to check that the set arrived complete — in particular that `callback_url`
is `true` if you expect to receive deliveries.

🔴 **Registering does NOT activate.** The instance stays `pausada`, and while it is, the webhook
answers `503` and so does sending. Only a **successful send** activates it (the smoke test): call
`POST /v1/fumaca` (next section) with this instance and the destination that should **receive** the
test message. Registering proves nothing — sending proves it. If registering activated, a wrong
credential would become an "active" instance that refuses everything.

### The 24 h window — what it is, and how not to lose it

You may write your instance's configuration for **24 h counted from the FIRST time you registered
anything** — not from the instance's creation (you may take five days to start, and the clock only
starts when you write) and **not restarting on each change** (otherwise anyone touching it daily
would keep the window open forever). During it, re-register as much as you like: this is how you
test, get it wrong and fix it on your own.

A **refused** registration (`400`) does **not** open the window — getting the body wrong on the first
try does not cost you your 24 h.

After the window, the `POST` answers **`409`** and nothing is stored; the configuration already there
remains in force. **The way out is human, it exists, and it is the third of the matters that only
whoever handed you the slug can resolve:** ask them to reopen the window — there is a command for
that on the gateway side. Reopening gives the deadline back and **does not alter the configuration**
(you are still the one who registers), and the new clock only starts when you write again.

> ⚠️ **Plan your use of the window, because it is the only period in which you fix things on your
> own.** The case that hits it most: a wrong `token_envio`, or one without permission, discovered in
> `POST /v1/fumaca` **after** the window has closed. While it is open, the cycle *register → smoke →
> fix → re-register* is entirely yours; after it, every credential fix costs a trip to another
> person. **Run the smoke test right after the first registration**, not the next day.

### Errors

| HTTP | `classe` | When |
|---|---|---|
| `400` | `permanente` | the body is not JSON, `instancia` is missing, a required field is missing, `callback_url` is not `https`, `bundle_ca` has no certificate. The message **names the field** and never echoes the value |
| `401` | `config` | no `Authorization`, or a token nobody recognizes |
| `403` | `config` | the token is valid, but that instance **is not yours** |
| `404` | `config` | the instance no longer exists in the gateway (talk to whoever handed you the slug) |
| `409` | `config` | **the registration window has closed**; nothing was stored |
| `413` | `permanente` | body above the ceiling (**1 MiB** by default — see *Known limits*) |
| `503` | `retentavel` | the gateway could not talk to its own database; retrying is safe |

## Prove the channel — `POST /v1/fumaca` (2026-07-28)

This is the step that **activates** your instance. It sends a real TEST message to a number you
choose, and only activates if Meta accepts the send — registering never activates (previous section).

```
POST /v1/fumaca
Authorization: Bearer <consumer token>
Content-Type: application/json
```

```jsonc
{
  "instancia": "lojinha",
  "destino":   "5511999990000"    // number that will RECEIVE the test message, in E.164
}
```

🔴 **`destino` has no default value, on purpose.** A default here would send the test message to a
number chosen by us, not by you — and a sent message cannot be undone.

### The `200`

```jsonc
{
  "instancia": "lojinha",
  "estado": "ativa",
  "pausada": false,
  "ja_estava_ativa": false,
  "wa_message_id": "wamid.HBgL…",
  "ativa_desde": "2026-07-28T21:40:00Z"
}
```

🔴 **`ativo = 1` is ALWAYS a consequence of Meta having accepted the send, never of you having called
the route.** If Meta refuses (wrong credential, invalid number, whatever it is), the instance
**stays paused** and the call returns an error — see the error table below. There is no way, on this
route or any other, to activate without a test message actually having gone out.

🔴 **Instance already active: this route does NOT send a message.** The response comes with
`ja_estava_ativa: true`, no `wa_message_id`, and `ativa_desde` saying since when it has been active
(the timestamp of the test message that activated it). *Calling `POST /v1/fumaca` again on an already
active instance is safe and cheap — it never spends a second paid message just because you called the
route again.* If you really need a fresh proof (for example after changing the `token_envio`), pause
first (`POST /v1/pausa`, below) and run the smoke test again.

### Errors

| HTTP | `classe` | When |
|---|---|---|
| `400` | `permanente` | the body is not JSON, `instancia` or `destino` is missing |
| `401` | `config` | no `Authorization`, or a token nobody recognizes |
| `403` | `config` | the token is valid, but that instance **is not yours** |
| `404` | `config` | the instance no longer exists in the gateway (talk to whoever handed you the slug) |
| `502` | `config` \| `permanente` \| `retentavel` \| `desconhecido` | Meta refused the send or did not answer — the instance stays PAUSED. The message says why, using the same classification as `POST /v1/messages` (section *What each error means*) |
| `413` | `permanente` | body above the ceiling (**1 MiB** by default — see *Known limits*) |

## Pause the channel — `POST /v1/pausa` (2026-07-28)

The safe direction: takes your instance off the air without deleting anything, without demanding any
proof.

```
POST /v1/pausa
Authorization: Bearer <consumer token>
Content-Type: application/json
```

```jsonc
{ "instancia": "lojinha" }
```

```jsonc
// 200
{ "instancia": "lojinha", "estado": "pausada", "pausada": true }
```

While paused, the webhook answers `503` and so does sending — Meta re-queues whatever arrives and
retries for up to 36 h. **Coming back requires a new smoke test** (previous section): there is no
other path to reactivate.

### Errors

| HTTP | `classe` | When |
|---|---|---|
| `400` | `permanente` | the body is not JSON, or `instancia` is missing |
| `401` | `config` | no `Authorization`, or a token nobody recognizes |
| `403` | `config` | the token is valid, but that instance **is not yours** |
| `404` | `config` | the instance no longer exists in the gateway (talk to whoever handed you the slug) |
| `413` | `permanente` | body above the ceiling (**1 MiB** by default — see *Known limits*) |
| `503` | `retentavel` | the gateway could not talk to its own database; retrying is safe |

---

## What you receive

zapgw makes a `POST` to your instance's `callback_url`:

```
Content-Type: application/json
X-Zapgw-Signature:      sha256=<hex>    HMAC-SHA256 of the TIMESTAMP + the body — see obligation 3
X-Zapgw-Timestamp:      <unix>          seconds; goes into the signature, and is what gives anti-replay
X-Zapgw-Event-Id:       <id>            id of the FIRST event in the batch — read the caveat
X-Zapgw-Correlation-Id: <id>            crosses both sides; quote it when reporting a problem
X-Hub-Signature-256:    sha256=<hex>    Meta's ORIGINAL signature, passed through
```

```jsonc
{
  "instancia": "lojinha",
  "recebido_em": "2026-07-23T14:05:00Z",
  "cru": "<the EXACT bytes Meta sent, in base64>",
  "eventos": [
    { "tipo": "mensagem",
      "id": "msg:wamid.ABC",
      "phone_number_id": "…", "waba_id": "…",
      "wa_message_id": "wamid.ABC",
      "sub_tipo": "text",
      "de_cru": "551199990000",
      "de_canonico": "5511999990000",
      "nome_contato": "…",
      "texto": "…",
      "responder_a": "wamid…",
      "encaminhada": true, "encaminhada_muitas_vezes": true,
      "botao_payload": "…", "botao_texto": "…",
      "reacao": { "emoji": "👍", "alvo": "wamid…" },
      "midia_id": "…", "midia_mime_payload": "audio/ogg; codecs=opus",
      "voz": true,
      "legenda": "…", "nome_arquivo": "…",
      "localizacao": { "latitude": 37.44, "longitude": -122.16, "nome": "…", "endereco": "…" },
      "timestamp": 1769000000 }
  ],
  "parse_error": ""
}
```

> **Two fields in this example have already lied, and both break code, not just reading.**
> `parse_error` is the **empty** string when there was no error, never `null` and never absent — it is
> **not** omitted, and **that never changed**. And `eventos`, when the gateway enriched nothing, is
> today **`[]`** — an empty array, never `null`.
>
> ⚠️ **Until 2026-07-28 the wire sent `"eventos": null` in this case** — since the gateway's very
> first day (2026-07-23). If you wrote `envelope.get("eventos") or []` (or `?? []`) because of that
> warning, **keep it**: the defense is still correct and still necessary, because `[]` is also falsy
> in Python and iterates zero times in any language. See *ACCOUNT webhook*, further below, and the
> 2026-07-28 entry in *Breaking changes*.

**Which fields always come, and which only appear when there is something to say.** The rule that
applies to you is "treat everything as optional", but it has named exceptions, and hiding them would
be trading one inaccuracy for another:

| Level | **ALWAYS** present, even when empty | Everything else |
|---|---|---|
| envelope | `instancia`, `recebido_em`, `cru`, `eventos`, `parse_error` | — |
| an item of `eventos` | `tipo` and `id` | omitted when there is no value |
| inside the nested blocks | `reacao.alvo`; `localizacao.latitude` and `.longitude`; `erro.codigo` and `.mensagem`; `cobranca.categoria`; `template.estado`; `template_categoria.categoria_nova` | omitted when there is no value |

Outside those rows, **every field is omitted when empty**: a plain text message does not get
`reacao`, `voz`, `legenda`, `nome_arquivo`, `localizacao`, `responder_a`,
`encaminhada`/`encaminhada_muitas_vezes` (since 2026-07-28), nor `erro` (since 2026-07-26). That is
what the "the envelope only grows" guarantee (below) demands, and there is a regression test pinned
to the current fields.

> ⚠️ **The fields in the first column always come because their absence would be ambiguous or
> fatal** — `id` is your dedup key, `latitude: 0` is a valid coordinate, `erro.codigo: 0` would have
> to be distinguishable from "no code". **This is not a licence for you to require presence**: a
> block that Meta sends in an unreadable format is discarded whole, and then the event arrives
> without it (see the 2026-07-28 entries in *Breaking changes*). The table says what the gateway
> guarantees **when it produces the field**, not that the field exists in every scenario.

### `responder_a` — which message this one answers

`responder_a` carries the `wamid` of the **quoted** message, when the sender replied by holding down
another message's bubble (Meta sends this in `messages[].context.id`, in **any** `sub_tipo`, not
only in `text`). **SAME NAME as the equivalent field when sending** (`responder_a` in
`POST /v1/messages`, see further below) — the referent is identical in both directions: the `wamid`
of the quoted message. Sending and receiving with different names for the same thing would be the
start of two vocabularies.

**Meta also sends `context.from` (the number of the BUSINESS that sent the original message), and the
gateway does not pass that field through.** This is not an oversight: a field that looks like "from
whom" and is "to whom" is an invitation to a bug, and nobody has asked for that value to date. If
someday somebody needs it, it comes in with a name that says what it is — never by reusing
`responder_a` for two meanings.

Absent when the message answers nothing — the key disappears from the JSON, never
`"responder_a": ""`.

> ⚠️ **Absent does NOT mean "it isn't a reply". It means "Meta did not send a link".**
> Observed on 2026-07-26 (two real payloads from the same conversation, 3 minutes apart): someone who
> replies by **holding the bubble** generates `context` and you receive `responder_a`; someone who
> replies by **typing into the phone's notification** — an *inline* reply — generates a payload with
> **no `context` at all**, and therefore no `responder_a`, even though it is a genuine reply to the
> person who wrote.
>
> This is not the gateway omitting it nor your side losing it: **Meta does not send the link in that
> case.** And replying from the notification is the fastest way on a phone, so the absence is
> probably the majority of the traffic, not the exception.
>
> **Practical consequence:** do not treat a missing `responder_a` as an anomaly, do not alarm because
> of it, and do not build anything that depends on every reply carrying the link. If someday somebody
> opens an investigation into "`responder_a` missing on a real payload", the answer is this line.
>
> **Since 2026-07-28 there is a third reason for the absence, and it is ours, not Meta's:** if the
> `context` block arrives with a **type** the gateway cannot read, it discards the whole block and
> delivers the message without `responder_a`, `encaminhada` and `encaminhada_muitas_vezes` — instead
> of losing the message, which is what happened before. See *Breaking changes*, at the end of this
> file. For you the effect is indistinguishable from the other two cases, and the guidance above does
> not change.

**Executed example** (the gateway's parser over a **real capture** payload, 2026-07-26: someone
replied quoting an earlier message; `context.from` and `context.id` came out different from each
other in the real payload, confirming that it is the `id` that becomes `responder_a`, not the
`from`):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE015",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000013,
  "wa_message_id": "wamid.TESTE015",
  "sub_tipo": "text",
  "de_cru": "551199990000",
  "de_canonico": "5511999990000",
  "nome_contato": "Fulana de Teste",
  "texto": "Recebido",
  "responder_a": "wamid.TESTE001"
}
```

### `encaminhada` and `encaminhada_muitas_vezes` — the message was not written now, for you

Both come from `messages[].context` at Meta (`forwarded` and `frequently_forwarded`) and arrive in
**any** `sub_tipo`, not only in `text`.

- **`encaminhada`** — the person passed a message along instead of writing it. A weak, common signal:
  a customer forwarding a reference photo, a price list, a screenshot of a conversation. It is
  **context**, not an alarm.
- **`encaminhada_muitas_vezes`** — this is WhatsApp's **chain-message** signal (the "forwarded many
  times" mark the app shows). **This is the one that changes an automated decision.** If your side
  answers on its own — appointment detection, auto-reply, relay — a chain message is today treated as
  if the customer had written it. This field is what lets you **not** fire a business flow on top of
  it.

**They are plain booleans, and absence means `false` — unlike `voz`.** The distinction exists and is
deliberate, so it is worth writing down what it is: in `voz`, "Meta said it is not a voice note" and
"Meta said nothing" lead you to do **different** things when re-sending the audio, and that is why
absent ≠ `false` there. Here there is no third action: "it was not forwarded" and "Meta did not say
it was" lead to the same place — treating the message as written by the person. That is why **the key
only appears when it is `true`**; you will never receive `"encaminhada": false`, and you should not
treat the absence as unknown.

> ⚠️ **There is no real capture of these fields as of 2026-07-28.** None of the payloads the consumers
> kept carries `forwarded`, and Meta's public documentation, searched on that date, no longer has a
> page describing the fields of `context`. The gateway reads both fields where Meta has historically
> placed them, and the fixture proving the read is **synthetic** (marked as synthetic in the corpus
> inventory).
>
> **Practical consequence, and it is the reason this warning exists:** treat the **presence** of the
> fields as good information, and the **absence** as "we did not receive the signal" — not as proof
> that the message is original. **The proof that counts for you is your own**: if you observe a
> genuinely forwarded message arriving with `encaminhada: true`, it is confirmed in your traffic, and
> that is what decides what your code may assume. Until you observe it, do not build anything that
> depends on the presence.

**`referred_product`, the third field of `context`, remains outside the envelope.** Nobody asked for
it, and the envelope only grows — adding later is free, removing later is a break.

**Forwarding is not quoting.** A forwarded message that answers nothing arrives with a `context`
**without `id`**, that is, with `encaminhada` and **without** `responder_a`. The two cases are
independent and can appear together.

**Executed example** (the gateway's parser over a **synthetic** payload — see the warning above):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE018",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000021,
  "wa_message_id": "wamid.TESTE018",
  "sub_tipo": "text",
  "de_cru": "551199990000",
  "de_canonico": "5511999990000",
  "nome_contato": "Fulana de Teste",
  "texto": "Tabela de precos 2026",
  "encaminhada": true
}
```

Note that `encaminhada_muitas_vezes` **does not appear**: in this payload it is `false`, and `false`
disappears from the envelope.

### `reacao`, `voz`, `legenda`/`nome_arquivo`, `localizacao`, `erro` — what each one means

These fields exist so that you **do not have to re-parse the `cru`** for reaction, location, caption,
file name, voice note and — since 2026-07-26 — the reason Meta could not represent a message. Before
them, the `sub_tipo` arrived labelled (`"reaction"`, `"location"`, `"unsupported"`) but without the
event's CONTENT, and the only way to recover the data was to reimplement Meta's parser yourself.

- **`reacao` (`sub_tipo: "reaction"`)** — `emoji` and `alvo` (the `wamid` of the message reacted to).
  **A missing `emoji` is a removal, not an error**: when someone takes back a reaction they had
  placed, Meta itself sends the event without the `emoji` key — that is how it distinguishes "I
  reacted" from "I removed the reaction". The gateway passes that absence through exactly as it came;
  `alvo`, by contrast, is always mandatory: a reaction event without it is counted as a parse error
  (`parse_error`) and does not become a `reacao`.
- **`voz` (`sub_tipo: "audio"`)** — `true`/`false`/**absent**, in that order of meaning. It is the
  playable voice note (obligation 5 of the contract, further below) versus an ordinary audio
  attachment. **Absent is not `false`**: Meta sometimes does not send the `voice` field in the
  payload, and treating that as `false` by default would invent data you do not have. If `voz` does
  not appear, the gateway does not know.
- **`legenda` and `nome_arquivo` (media with `caption`/`filename` in Meta's payload)** — the caption
  exists for image, video and document; the file name only for document. Both refer to the same
  media as the `midia_id` in the same event.
- **`localizacao` (`sub_tipo: "location"`)** — `latitude`, `longitude` (always present, even when
  `0` — the crossing of the Greenwich meridian with the equator is a valid coordinate) and, when the
  sender sent them, `nome` and `endereco`.
- **`erro` (`sub_tipo: "unsupported"`, 2026-07-26)** — the **SAME field and SAME shape** as the `erro`
  of the `status` event (described just below: `codigo`, `mensagem`, `detalhes`), but with a
  **different MEANING**, and the difference matters: in the status event, `erro` means "the delivery
  failed"; here it means **"Meta received something the Cloud API cannot represent"** — Meta did not
  fail to deliver anything, it delivered and could not decode the content (the observed case is code
  `131051`, `"Message type unknown"`). Without this field, an `unsupported` message arrived with a
  `sub_tipo` and an `id`, and **nothing else** — indistinguishable from "empty message" for anyone
  who only looks at the envelope. Absent when Meta did not send `errors[]` in the message — omitted,
  never `{"codigo": 0, "mensagem": ""}` (same rule as on the status side, see below).

**Executed examples** (deserialized and revalidated against the parser before going in here — the
gateway's parser over the corpus payloads):

Reaction added (**real capture**, 2026-07-26; the emoji `❤️` has two codepoints, `U+2764` +
`U+FE0F` variation selector, on purpose: a single-codepoint emoji would not prove that the parser
preserves the pair):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE006",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000005,
  "wa_message_id": "wamid.TESTE006",
  "sub_tipo": "reaction",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "reacao": { "emoji": "❤️", "alvo": "wamid.TESTE001" }
}
```

Reaction **removed** (**real capture**, 2026-07-26: the same reaction as the example above, undone
20 seconds later, same target) — note the absence of the `emoji` key inside `reacao`, not an empty
string:

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE007",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000006,
  "wa_message_id": "wamid.TESTE007",
  "sub_tipo": "reaction",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "reacao": { "alvo": "wamid.TESTE001" }
}
```

Location (**real capture**, 2026-07-26; coordinates rounded on purpose in the capture, so as not to
expose the real location). This is the **bare pin** — no `nome`/`endereco` — which is the case
observed as common; Meta also accepts a business pin with both fields filled in (see the note above
about `nome`/`endereco` being optional), but that is not what most users send:

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE008",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000007,
  "wa_message_id": "wamid.TESTE008",
  "sub_tipo": "location",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "localizacao": {
    "latitude": -21.229,
    "longitude": -43.7892
  }
}
```

Document with caption and file name:

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE009",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000008,
  "wa_message_id": "wamid.TESTE009",
  "sub_tipo": "document",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "midia_id": "MEDIA_TESTE2",
  "midia_mime_payload": "application/pdf",
  "legenda": "meu recibo",
  "nome_arquivo": "recibo-teste.pdf"
}
```

Voice note — `voz: true` (the same payload as the two-mimes trap cited in obligation 5):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE004",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000003,
  "wa_message_id": "wamid.TESTE004",
  "sub_tipo": "audio",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "midia_id": "MEDIA_TESTE",
  "midia_mime_payload": "audio/ogg; codecs=opus",
  "voz": true
}
```

`sub_tipo: "unsupported"` with the reason (2026-07-26) — Meta received something the Cloud API
cannot represent (the example below is synthetic, with the values from Meta's official example:
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/,
read on 2026-07-26; there is no real-capture corpus for this case yet):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE016",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000014,
  "wa_message_id": "wamid.TESTE016",
  "sub_tipo": "unsupported",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "erro": {
    "codigo": 131051,
    "mensagem": "Message type unknown",
    "detalhes": "Message type is currently not supported."
  }
}
```

Plain text message, for comparison — **none of the five new fields appears** (real capture):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE001",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000000,
  "wa_message_id": "wamid.TESTE001",
  "sub_tipo": "text",
  "de_cru": "551199990000",
  "de_canonico": "5511999990000",
  "nome_contato": "Fulana de Teste",
  "texto": "mensagem de teste"
}
```

### `status` (`tipo: "status"`) — what each field means

The status event confirms the fate of a message **you** sent: `sent` → `delivered` → `read`, or
`failed`. It has no `sub_tipo`; the fields that matter are these:

| Field | What it is |
|---|---|
| `status` | `sent`, `delivered`, `read` or `failed` — the vocabulary is Meta's, passed through untranslated |
| `wa_message_id` | the id of the message you sent (the same one that came back in the `POST /v1/messages` response) |
| `para_cru` / `para_canonico` | the recipient, in both forms — same reason as the message's `de_cru`/`de_canonico`: Meta does not guarantee the same spelling you registered |
| `erro` | **only on `failed`, and only when Meta sent the reason** — see below |
| `cobranca` | **when Meta sent `pricing`** — under which category it charged for this delivery, see below |

> 🔴 **DO NOT ORDER A MESSAGE'S HISTORY BY `timestamp` — the `sent` and the `delivered` of the SAME
> message arrive with the SAME timestamp from Meta.** This is not a hypothesis: it was **measured in
> real traffic** by a consumer on 2026-07-28, and it is frozen in this repository's corpus — the two
> frozen fixtures of that pair are the same `wa_message_id` with `timestamp` `1785072102` on **both**.
>
> The envelope's `timestamp` is **Meta's** timestamp (`statuses[].timestamp`), passed through
> untranslated. It says *when the event happened from its point of view*, and it can date two
> different states within the same second. A screen that orders by it shows `delivered` before `sent`
> half the time, with no error anywhere.
>
> **What separates the two is ARRIVAL ORDER** — the instant the `POST` hit your endpoint, which is
> your data and not ours. Record it (obligation 1 already requires recording the `cru` with the
> moment of receipt) and order by that, with Meta's `timestamp` as a tiebreaker or as displayed
> information, never as a sort key.
>
> **Each state's identity remains intact:** the event's `id` is `status:{wamid}:{status}`, and `sent`
> and `delivered` of the same send have **different** ids. Your dedup does not merge the two — what
> the identical timestamp breaks is ORDER, not uniqueness.

**`erro` (2026-07-26; `detalhes` added days later, in the same month)** — `codigo` (integer),
`mensagem` (text) and `detalhes` (text, **optional**). It exists because `failed` on its own does not
say **why**: without the reason, the human operator that a system like yours notifies when a delivery
fails (the original trigger for this task was a real failure, code `131026`, which ended up only
recorded in the database because nobody saw it) has nothing to show.

> ⚠️ **`erro` is the SAME field, with the SAME shape, in TWO different events — and the meaning is
> NOT the same.** Since 2026-07-26, `erro` also appears on the **message** event
> (`sub_tipo: "unsupported"` — see the section above). Here (status) it means "the delivery failed";
> there (message) it means "Meta received something and could not represent it" — the message was
> delivered, it did not fail. Everything this block describes about `codigo`/`mensagem`/`detalhes`,
> about the `errors[]` list and about "absence is absence, never zero" applies identically to both;
> **only what `erro` is SAYING about the event changes**, and it is the event's `tipo`/`sub_tipo`
> that says which of the two it is.

Format confirmed at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
(read on 2026-07-26): Meta sends `errors[]` — a list — with `code`, `title`, `message`,
`error_data.details` and `href` per item. **The gateway passes through `codigo` (from `code`),
`mensagem` (from `title`) and `detalhes` (from `error_data.details`)** — not `message` (identical to
`title` in the doc's example), not `href`. There is no translation into Portuguese: translating a
third party's code is your decision, and a table of ours would rot the day Meta added a new code.

**Why `message` and `href` are left out and `error_data.details` is not.** The question "does any cut
field get missed?" was put to a real consumer (2026-07-26, who checked their own error translator
before answering): Meta sends `title` and `message` **identical** — sending both would repeat the
sentence — and `href` has no consumer. `error_data.details` is different from the other two: it is
the **only** part of the message that **adds** information instead of repeating the title. Without
it, the operator's alert is poor precisely in the cases where the generic code explains nothing —
which are the cases where the message is missed.

**`detalhes` is optional and can be missing even inside an `erro` that is present.** Meta only sends
`error_data` for some codes; when it is missing, `codigo` and `mensagem` still come out normally —
the absence of the nested object does not bring down the rest of the reason. When it is missing, the
`detalhes` key disappears from the JSON (never `"detalhes": ""`) — same reason the whole `erro` is
omitted instead of zeroed, see below.

**`errors[]` can carry more than one item; the gateway keeps only the FIRST.** This is not a silent
discard — it is the choice recorded here: a single reason is already enough for an operator's alert,
and to date there is no observed case of conflicting items that would justify exposing the whole
list. If that changes, it is a contract change, not a detail adjustment.

**Absence is absence, never zero.** A `failed` without `errors[]` in Meta's payload — or with an item
the gateway could not interpret — does not get an `erro`: the field is **omitted**, never
`{"codigo": 0, "mensagem": ""}`. Code `0` is not a real Meta code; if the gateway invented it, you
would have no way to distinguish "no reason reported" from "a genuine error with code zero".

### `cobranca` — under which category Meta charged (2026-07-26)

**This is not accounting curiosity — it is the only way to know, on the FIRST message, that Meta
reclassified a template.** Editing a template can make Meta reclassify `UTILITY` → `MARKETING`, which
changes price and sending rules. Without this field, that would only show up on the **invoice**,
weeks later: `pricing.category` is Meta saying, **on each delivery**, under which category it
charged, and that is why the reclassification appears on the first message delivered after the
change, not at the end of the month.

Requested by a consumer (2026-07-26), who checked before asking: `pricing` did not exist in this
gateway's contract or code, but it **does arrive** — it is in 145 of the 148 statuses they have
recorded. The gateway translates it to `cobranca`, in our vocabulary — Meta's format dies at the
gateway, as in all the rest of the envelope, so that no consumer needs to know Meta's format in order
to know how much of their traffic is charged.

| Field of `cobranca` | What it is |
|---|---|
| `categoria` | Meta's `pricing.category`, passed through untranslated: `utility`, `marketing`, `authentication`, `service`, among others Meta defines |
| `cobravel` | Meta's `pricing.billable` — `true`/`false`/**absent** (see the note below) |

**Only `categoria` and `cobravel` are modelled.** `pricing_model` and `type` — the other two fields
Meta sends inside `pricing` — stay out until someone says what they would do with them: the envelope
only grows, so adding later is free, removing later is a contract break.

> ⚠️ **An absent `cobravel` and `cobravel: false` are NOT the same information — and here the
> difference is about MONEY.** Same rule as `voz` (see above), with a bigger consequence: "Meta said
> it does not charge" (`false`) and "Meta said nothing" (absent, the key disappears from the JSON)
> are different things, and inventing `false` by default would hide exactly the case where nobody
> knows whether it was charged.

**The whole `cobranca` is absent when Meta did not send `pricing` in the status** — the field
disappears from the JSON, never `{"categoria": "", "cobravel": false}`. Do not invent a value.

**And the absence has an address: it is on `sent`.** A consumer's measurement over 225 raw Meta
payloads (2026-07-28): of the **53 `sent`**, **49 came with `pricing` and 4 without** (~7.5%); of the
**49 `delivered`**, **49 came with it** (100%). A `sent` without `cobranca` **is neither a failure nor
a truncated payload — it is a normal case**, and code that treats `cobranca` as guaranteed on `sent`
breaks about one in thirteen times. **Both forms are frozen side by side**, both backed by real
traffic, and both appear as examples just below.

**Executed example** (the gateway's parser over a **real (partial) capture** payload, 2026-07-26: the
`status`/`pricing` pair is literal, exactly as it arrived; it needed an envelope around it to become
a fixture):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE017:read",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000015,
  "wa_message_id": "wamid.TESTE017",
  "status": "read",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "cobranca": {
    "categoria": "utility",
    "cobravel": true
  }
}
```

**Executed examples** (the gateway's parser over the corpus payloads):

Send accepted **without** `cobranca` (**real capture**, 2026-07-28: this is one of the 4 `sent` out
of 53 that came without `pricing`):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE041:sent",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1785073298,
  "wa_message_id": "wamid.TESTE041",
  "status": "sent",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000"
}
```

Send accepted **with** `cobranca` (**real capture**, 2026-07-28):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE042:sent",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1785072102,
  "wa_message_id": "wamid.TESTE042",
  "status": "sent",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "cobranca": {
    "categoria": "service",
    "cobravel": false
  }
}
```

Delivery confirmed (**real capture**, 2026-07-28) — no `erro`. **It is the SAME send as the example
above**: note the identical `wa_message_id`, the **identical** `timestamp`, and the **different**
event `id` — this is exactly the case of the 🔴 warning at the start of this section:

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE042:delivered",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1785072102,
  "wa_message_id": "wamid.TESTE042",
  "status": "delivered",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "cobranca": {
    "categoria": "service",
    "cobravel": false
  }
}
```

Failure, with the reason (**real capture**, 2026-07-26: this is the real failure of 2026-07-20 that
motivated this whole section of the contract; before, it was derived from the doc's generic example):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE010:failed",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000009,
  "wa_message_id": "wamid.TESTE010",
  "status": "failed",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "erro": {
    "codigo": 131026,
    "mensagem": "Message undeliverable",
    "detalhes": "Message Undeliverable."
  }
}
```

### `template_status` (`tipo: "template_status"`) — Meta approved, rejected or paused a template (2026-07-26)

This event **is not about a message** and has no recipient: it is Meta warning that the state of a
**template** in your account has changed. It arrives in the same `POST` as always, inside `eventos`.

**Why it matters more than it looks: the `categoria` comes in the event.** The `UTILITY` →
`MARKETING` reclassification — which changes **price and sending rules** — appears here **before any
message is sent**. It is an earlier signal than the status's `cobranca` (above), which only arrives
after a delivery. The two complement each other: this one warns that it **changed**, that one
confirms **what was charged**.

| Field | What it is |
|---|---|
| `template.nome` / `template.idioma` | the pair that identifies the template **when sending** (`template` and `idioma` of `POST /v1/messages`) — it is how you link this notice to what you send |
| `template.categoria` | `UTILITY`, `MARKETING`, `AUTHENTICATION` — the vocabulary is Meta's, passed through **untranslated** |
| `template.estado` | `APPROVED`, `REJECTED`, `PENDING`, `PAUSED`, `DISABLED`… — also Meta's, untranslated |
| `template.motivo` | Meta's `reason`, **as it came** — including the string `"NONE"`, see below |
| `waba_id` | the **only** routing key of this event (see the next section) |
| `timestamp` | the batch's timestamp (Meta's `entry.time`) — this webhook **has no timestamp of its own** |

> ⚠️ **`"NONE"` is the NORMAL value of `motivo`, not an error nor an "empty".** Meta sends the literal
> string `"NONE"` when there is no reason — not absent, not `null`. The gateway passes it through as
> it came, and **does not translate `"NONE"` into empty**: "Meta said NONE" and "Meta did not send the
> field" are different facts, and the second may appear in a type of event we have not seen yet. If
> `motivo` disappears from the JSON, it is because Meta really did not send the field.
>
> **The same field, with the same doctrine, is also in the catalogue** — `GET /v1/templates` (section
> "Read the template catalogue", further below) returns `templates[].motivo` for when you read the
> catalogue for another reason, or missed this webhook. The difference is timing: this one warns **on
> its own**, as soon as Meta decides; the catalogue's requires you to **ask**.

**`phone_number_id` does not appear in this event** — the template webhook carries no `metadata` at
all (confirmed across the 21 real specimens that founded this section). If your code requires
`phone_number_id` in every event, it breaks here.

> ⚠️ **This event's `id` includes TIME, and that is on purpose — do not "simplify" it to
> `template_status:{id}:{event}`.** For message status the key is `status:{wamid}:{status}` and that
> is enough, because `sent`/`delivered`/`read` are **distinct** states: the same pair never repeats.
> Here it is not so: **the same template can be `APPROVED` more than once** — approved, edited, back
> to pending, approved again. Without time in the key, the **second approval would have the id of the
> first** and your dedup (obligation 2, further below) would throw it away: you would never find out.
> The time comes from Meta's `entry.time`, because this webhook's `value` has no timestamp of its own.
>
> The key remains **deterministic**: a redelivery of the SAME event (Meta retries for up to 36 h)
> carries the same `entry.time` and therefore the same `id`, and your dedup works as with any other
> event.

**Executed example** (the gateway's parser over a **real (partial) capture** payload, 2026-07-26: the
`change` is literal, one of the 21 specimens kept on disk since before the migration; the `entry`
level is the corpus's standard envelope):

```json
{
  "tipo": "template_status",
  "id": "template_status:1384121316897444:APPROVED:1769000020",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000020,
  "template": {
    "nome": "aguardando_peca_v2",
    "idioma": "pt_BR",
    "categoria": "UTILITY",
    "estado": "APPROVED",
    "motivo": "NONE"
  }
}
```

> 🔴 **This event is NOT the category reclassification notice, and for three days it was treated as if
> it were (2026-07-28).** It carries `template.categoria` because the category is an **attribute** of
> the new state — that is, you learn about the reclassification **if, and only if,** Meta re-approves
> the template in the same movement. When it reclassifies an **already approved** template without
> touching the state, this event **does not arrive**, and the failure mode is the worst possible:
> silence, with the discovery arriving on the invoice. The dedicated event is `template_categoria`,
> just below.

**The other account webhooks:** see the section *ACCOUNT webhook*, further below, for the complete
list of what is modelled and what arrives with `eventos: []` on purpose.

### `template_categoria` (`tipo: "template_categoria"`) — Meta RECLASSIFIED a template's category (2026-07-28)

**This is the event that warns that the category changed.** It comes from Meta's
`template_category_update` webhook, and it exists alongside the `template_status` above — the two
speak of the same template and are **not** the same fact. A template can be reclassified **and**
re-approved, and in that case both arrive; deduplicating one against the other erases information.

**Why it matters: reclassification to `MARKETING` makes every send more expensive.** An appeal
exists, but what tells you it exists is Meta's panel, **not this event** — see the red warning just
below the table. What it gives that `template_status` does not:

| Field | What it is |
|---|---|
| `template_categoria.categoria_anterior` / `categoria_nova` | the **direction**. `UTILITY → MARKETING` raises the price; `MARKETING → UTILITY` lowers it. `template_status` only says "today it is MARKETING", and without state stored on your side the two cases are indistinguishable |
| `template_categoria.status_do_recurso` | Meta's `category_appeal_status` — `"ELIGIBLE"` would mean it **can be appealed**. ⚠️ **Never observed in real traffic** — see below |
| `template_categoria.categoria_correta` | the `correct_category`: which category Meta considers correct for this template. ⚠️ **Never observed in real traffic** — see below |
| `template_categoria.nome` / `idioma` | the pair that identifies the template **when sending** (`template` and `idioma` of `POST /v1/messages`) |
| `waba_id` | the **only** routing key (account webhook — there is no `phone_number_id`) |
| `timestamp` | the batch's timestamp (`entry.time`) — this webhook **has no timestamp of its own** |

> 🔴 **The two appeal fields are DOCUMENTED by Meta and have NEVER arrived in measured traffic. Do not
> build anything that depends on them without confirming it in your own account.** The table rows
> above describe what Meta's documentation promises; what was measured says something else:
>
> - **35 real captures**, from 30/07 to 23/08/2026, in an account in normal use (measurement by
>   consumer `consumer-b`, a full field inventory via `set(v.keys())`): the field set is **closed in
>   two formats**, and neither contains `category_appeal_status` or `correct_category`. The fields
>   seen are `message_template_id`, `message_template_name`, `message_template_language`,
>   `new_category` and — in 34 of the 35 — `previous_category`.
> - **This includes templates that went through a category review request**, which is precisely the
>   scenario where a `category_appeal_status` would make sense to appear.
> - The three captures frozen in `testdata/corpus/` agree, and there is a test that goes **red** the
>   day a capture with those fields enters the corpus — the statement corrects itself instead of
>   ageing in silence.
>
> **What does NOT follow from that:** that Meta does not send them. One account, one month, and the
> fields may depend on a path nobody walked — a formal appeal opened through the panel, for example,
> is not the same thing as "requesting a category review". **What is asserted is the paragraph above,
> and only it.**
>
> ➡️ **Practical consequence:** treat `status_do_recurso` and `categoria_correta` as **absent by
> default**. Anyone wanting to know whether a downgrade can be appealed finds out through Meta's
> panel, not through this event. **If they show up in your account, send the capture through the
> channel** — it becomes a fixture the same day and this block changes.

> ⚠️ **`categoria_nova` is the only field in this block WITHOUT `omitempty`: it always comes.** A
> `template_category_update` without `new_category` (or without `message_template_id`) **does not
> become an event** — the dedup key would collide with any other empty change in the same batch. You
> still receive the `cru`, and the envelope's `parse_error` reports it. The other fields may be
> missing individually: a field Meta sends in a format we do not yet know how to read costs **that
> field**, never the event.

> ⚠️ **`status_do_recurso` is TEXT, and does not become a boolean.** `"ELIGIBLE"` arrives as
> `"ELIGIBLE"`. *"Can it be appealed?"* looks like a yes/no question, and Meta answers with a
> vocabulary nobody here has enumerated — translating it into `true`/`false` would force the gateway
> to decide today what to do with a value that only appears tomorrow. Same rule as `template.estado`,
> `template.categoria` and `cobranca.categoria`: **Meta's format dies at the gateway; its vocabulary
> of values does not.**

**Example** (the gateway's parser over the sample Meta publishes as this webhook's example):

```json
{
  "tipo": "template_categoria",
  "id": "template_categoria:12345678:MARKETING:UTILITY:1769000070",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000070,
  "template_categoria": {
    "nome": "my_message_template",
    "idioma": "en-US",
    "categoria_anterior": "MARKETING",
    "categoria_nova": "UTILITY",
    "categoria_correta": "MARKETING",
    "status_do_recurso": "ELIGIBLE"
  }
}
```

> ✅ **This event's fixture has been a REAL TRAFFIC CAPTURE since 2026-08-28** (T-174; three raw payloads
> supplied by consumer `consumer-b` and frozen in `testdata/corpus/`: the `UTILITY → MARKETING` /
> `MARKETING → UTILITY` pair of the **same** template, and a third that arrived **without
> `previous_category`**). Until then it was derived from the sample in Meta's panel, and the warning
> that stood here said why. ⚠️ **What the capture showed, and it matters for your mapping:** in all
> three, `correct_category` and `category_appeal_status` **did not come** — the two fields in the
> table above come from what Meta documents, and the traffic observed so far has not corroborated
> them. These are three events from one account: do not read it as "Meta does not send them", read it
> as "do not count on them being there".

> 🔴 **BLOCK CONTESTED BY THE SOURCE ITSELF ON 2026-08-29 — read this before using any number from
> here.** Consumer `consumer-b`, who produced this measurement, went back to the `payload_cru` of the
> SAME 35 events and reported through the channel that **two of the statements below do not hold**:
>
> - **the downgrade does not hit an already approved template in use.** The 15 downgrades happened
>   **1 to 13 MINUTES after the template's CREATION**, without exception. What this series measures is
>   the category the template COMES OUT of classification with — not Meta changing its mind later.
> - **no return to `UTILITY` came from Meta** — they are all batches of a human asking through the
>   WhatsApp Manager menu. This specifically knocks down the sentence, further below, that offers
>   *"14.8 h, 14.9 h, 22.1 h and 22.2 h"* as **Meta's number**: in this series there is no Meta number.
>
> **What STILL STANDS, and it is the reason the event exists:** the template spends time in
> `MARKETING`, and during that time sending is more expensive and subject to opt-out. The practical
> consequence does not depend on who initiated the change — only the explanation of the mechanism
> changes.
>
> **Why the new number did NOT replace the old one:** this document has already published a number
> from this same source that was wrong — see `ARMADILHAS.md`, *"Número que chega pronto não traz o
> comando que o produziu"* — and the rule that came out of that cost is **a third party's number goes
> in with the command that produced it, or it does not go in**. The command was requested through the
> channel on 2026-08-29 and has not arrived yet. Until it does, the block below stands as **reported
> on 28/08 and contested on 29/08**, never as a current measurement.

> 📊 **And it is not a hypothesis: 16 downgrades in 25 days, with the template spending from 15 hours
> to three weeks in the wrong category.** Measurement by consumer `consumer-b` in their own account,
> reported on 2026-08-28: **35** `template_categoria` events between **30/07 and 23/08/2026**, counted
> one by one over the `value` objects inside `entry[].changes[]` (without truncating the output, and
> separating by `message_template_id` — counting by name would mix versions and give the wrong
> conclusion). Of those, **16 changes to `MARKETING`** and 19 to `UTILITY`; no other category.
>
> **Eleven came back, and the time in `MARKETING` varies by two orders of magnitude: from 14.8 h to
> 512.8 h.** 🔴 **Do not read the upper end as slowness by Meta.** The 400-500 h windows all end in
> the same batch of 21/08 06:48-06:52 — the hour when a human requested a review of several templates
> at once. They measure **how long it took until someone asked**, not how long Meta took to answer.
> 🔴 **(SENTENCE KNOCKED DOWN BY THE SOURCE ON 29/08 — see the warning at the top of the block.)**
> Anyone wanting Meta's number should look at the four where the request was immediate: **14.8 h,
> 14.9 h, 22.1 h and 22.2 h**.
>
> ⚠️ **And five did NOT come back** — one of them is still in `MARKETING` today. Adding that to the
> paragraph above: the floor of the window is ~15 h, and **the ceiling is "until somebody notices"**.
> That is why consuming this event is not optional: without it, the window's clock only starts running
> when someone finds the invoice odd.
>
> **What that forces you to decide, and it is the reason this event exists:** during that window,
> every send from that family goes out as `MARKETING` — more expensive and subject to promotional
> opt-out, in a notice that is transactional to your customer. Whoever keeps the immediately preceding
> `UTILITY` version approved has somewhere to fall back to while the appeal runs; whoever only has the
> live version does not. **Keeping the previous one is a rollback for THIS window** — it is not
> insurance against Meta "changing its mind after the appeal", which is a different question.
>
> **The caveat, and it is about what was NOT counted:** "does the category move again after the
> appeal?" was explicitly checked over **four** restorations, with 6 to 7 days of observation, and
> none recurred. That is **evidence in favour**, with a small `n` — and it was not recounted over the
> eleven restorations of the complete series. **Do not read it as confirmation.**
>
> 🔁 **Review trigger, and it is a request to you:** this is **Meta's** behaviour measured in **one**
> account, over a 25-day interval. A second account knocks it down easily. **If your account sees
> something else** — a different frequency, a downgrade without restoration, recurrence after the
> appeal, a window of another order of magnitude — say so: this block changes, and whoever measured
> first explicitly asked to know.

### `qualidade_do_numero` (`tipo: "qualidade_do_numero"`) — the number's daily QUOTA and quality (2026-07-28)

Comes from the `phone_number_quality_update` webhook. **This is the only channel through which a
quota downgrade arrives before it hurts.** Without it, the first news that the ceiling dropped is
sending starting to fail on a limit — a symptom that points to the wrong place (everyone will look at
the gateway, the token and the network) and that only appears after messages have already been
refused.

| Field | What it is |
|---|---|
| `qualidade_do_numero.numero_exibido` | the `display_phone_number` — which number the notice is about. It is a **label**, not a `phone_number_id`: this webhook has no `metadata` at all |
| `qualidade_do_numero.estado` | Meta's `event`: `ONBOARDING`, `FLAGGED`, `UNFLAGGED`… — untranslated |
| `qualidade_do_numero.limite_anterior` / `limite_atual` | `old_limit` → `current_limit`. These two give the **direction**: `TIER_1K → TIER_50` is a downgrade, `TIER_NOT_SET → TIER_250` is the account being born. `limite_atual` alone does not distinguish the two |
| `qualidade_do_numero.limite_diario_maximo` | the `max_daily_conversations_per_business` |
| `waba_id` | the **only** routing key |
| `timestamp` | `entry.time` — this webhook has no timestamp of its own |

> 🔴 **The limits are literal TEXT. `"TIER_250"` arrives as `"TIER_250"`, not as `250`.** That is a
> decision, not laziness: converting requires a translation table, and it gets things wrong the day
> Meta invents a new tier — wrong in the worst possible way, returning a **plausible number** for a
> value nobody verified. If you need the number for a progress bar, build the map on your side and
> **treat the unknown value as unknown**, never as zero.

> ⚠️ **This event has no Meta id**, so the key is assembled from what it carries — number, state and
> the limit transition, plus the `entry.time`. A payload in which **none of that** is readable does
> not become an event (the `cru` arrives all the same, and `parse_error` reports it); any surviving
> piece is already enough for the event to come out.

> **Since 2026-07-28 this event also FEEDS `GET /v1/estado`.** The `limite_atual` (`current_limit`) is
> stored in the `numero_na_meta.limite_de_mensagens` block, with `fonte: "webhook"`. If you already
> react to this event, you need change nothing — the state simply becomes a second place, **queryable
> at any time**, where the same number appears. `limite_anterior` and `limite_diario_maximo` are
> **not** stored there: the state answers *"which tier is the number in NOW"*, and the direction of
> the change already travels whole here.

### `alerta_de_conta` (`tipo: "alerta_de_conta"`) — Meta warning about a problem, with SEVERITY (2026-07-28)

Comes from the `account_alerts` webhook. **The field that justifies the type existing is
`severidade`:** Meta's example carries `INFORMATIONAL`, and the existence of a level called
"informational" implies there are levels above it — those are the ones that decide anything. Without
modelling, a serious alert and a routine notice arrived identical: a raw line nobody reads.

| Field | What it is |
|---|---|
| `alerta_de_conta.severidade` | the `alert_severity` — **the field that decides** |
| `alerta_de_conta.tipo` | the `alert_type` (e.g.: `OBA_APPROVED`) — what happened |
| `alerta_de_conta.estado` | the `alert_status` (e.g.: `NONE`) |
| `alerta_de_conta.tipo_da_entidade` / `id_da_entidade` | `entity_type` / `entity_id` — what the alert is about. The `id` is **text**, and Meta sends it as a number |
| `alerta_de_conta.descricao` | the `alert_description`, free text in English |
| `waba_id` · `timestamp` | as in the other account webhooks |

> 🔴 **The gateway does NOT rank severities, and the absence is deliberate.** There is no
> `grave: true` here derived from `severidade`: ranking a third party's vocabulary requires knowing
> the whole list, and nobody here knows it. You are the one who decides what is serious, because you
> have the business context — the gateway hands the label over intact so that decision is possible.

> ⚠️ **Do not write a rule on top of `descricao`.** It is the only field of this event that is not
> closed vocabulary: it is an English sentence written by Meta. Matching a third party's free text is
> the fastest way to build an alarm that dies the day they rewrite the sentence. Use `severidade`,
> `tipo` and `estado`.

> ⚠️ **This event's key includes `severidade` and `estado`, not just the `tipo`.** The **same**
> `alert_type` can come back with a different severity (a problem that **escalates**) or with a
> different `alert_status` (a problem that is **resolved**). With a key carrying only the type, the
> escalation notice would be deduplicated against the original alert — and it is the only one of the
> two that requires action.

> 🔴 **The fixtures for these two events are DERIVED FROM THE DOCUMENTATION** (in this gateway's
> corpus, files with `_derivado_da_doc` in the name), for the same reason as `template_categoria`:
> they are samples from the panel's *Test* button, and there is no real capture — an account webhook
> is rare by nature. Testing your mapping only against them proves that you agree with Meta's
> **documentation**, not with what it **sends**.

### ACCOUNT webhook (template status, number quality, alerts) — 2026-07-26

Meta also sends webhooks that are not about a message nor about a recipient: template approval or
rejection (`message_template_status_update`), template quality
(`message_template_quality_update`), category change (`template_category_update`) and account alerts
(`account_update`, `account_review_update`, `account_alerts`).

**Four of them become events today**, and the rest arrive **with no event at all**. The table below is
the complete list, and it exists so you do not treat "an event that becomes nothing" as a parse
failure:

| Meta `field` | Becomes an event? | What you receive |
|---|---|---|
| `message_template_status_update` | ✅ `tipo: "template_status"` | see the event's section |
| `template_category_update` | ✅ `tipo: "template_categoria"` | see the event's section |
| `phone_number_quality_update` | ✅ `tipo: "qualidade_do_numero"` | see the event's section |
| `account_alerts` | ✅ `tipo: "alerta_de_conta"` | see the event's section |
| `message_template_quality_update` | ❌ **on purpose** | `cru` + `"eventos": []` |
| `message_template_components_update` | ❌ **on purpose** | `cru` + `"eventos": []` |
| `account_update` | ❌ **on purpose** | `cru` + `"eventos": []` |
| `account_review_update` | ❌ **on purpose** | `cru` + `"eventos": []` |
| `security` | ❌ **on purpose** | `cru` + `"eventos": []` |
| `phone_number_name_update` | ❌ **on purpose** | `cru` + `"eventos": []` |
| `calls` | ❌ **not yet** — see below | `cru` + `"eventos": []` |
| any `field` Meta invents tomorrow | ❌ | `cru` + `"eventos": []` |

**"On purpose" means exactly this: nobody asked.** The envelope only **grows** — adding a field later
is free, removing later is a contract break — so a new `tipo` with no interested consumer would be
dead vocabulary the gateway owes forever.

🔴 **If any of these is useful to you, you do NOT go without it: the `cru` arrives whole.** The
webhook is delivered normally, with Meta's exact bytes in base64 — just without enrichment. Parse the
`cru` on your side and you have the same data; the modelled `tipo` is convenience, not access.
**Deduplicate that batch by the rule in the section *And when the batch has no event at all*** (hash
of the `cru`), which is the right key precisely for this case. If one of these is ever modelled, it
is born with its own `tipo`, and that is **additive**: your parsing of the `cru` keeps working.

And it is not a gap out of forgetfulness or laziness about parsing: each one's `value` has different
keys, and interpreting them with another's parser would produce an **invented** event, which is worse
than none.

> ⚠️ **`calls` is the only one on the list that is different, and it is worth knowing why.** It is
> **not** an account webhook: it has `metadata.phone_number_id`, so it goes through guard **5a**, and
> on a real call the id **matches** — that is, it **is delivered to you**, with `"eventos": []`, and
> its `cru` carries **personal data** (`contacts[].profile.name`, `wa_id`) on a line nobody reads.
> That enters your retention accounting **today**, even without an event. Modelling it depends on a
> business decision that is not technical (does the number accept calls, and is there somebody to
> answer?), and that is why it is out of this round.

> 🔴 **The `template_category_update` row arrived late, and the delay has a name.** From 2026-07-26 to
> 2026-07-28 the gateway read the category from the **neighbour**
> (`message_template_status_update`) and called that a reclassification notice. If you built a cost
> alarm on top of `template.categoria`, it still holds — but it **only fires when Meta re-approves the
> template at the same time**. The complete signal is `template_categoria`.

> 🔴 **An unmodelled account webhook reaches you WITH NO EVENT AT ALL, and that is NORMAL — never a
> parse failure (2026-07-28).** The parser did not fail: it simply found no `messages` and no
> `statuses` to enrich. Store the `cru` (obligation 1) and answer `200`. **Do not alarm, do not treat
> it as an error, do not return `5xx`** — a `5xx` here would make Meta resend the same payload for
> 36 h for a batch that had nothing to process.
>
> ✅ **The field comes as `[]` — an empty array, never `null`.** The body is literally:
>
> ```json
> {"instancia":"…","recebido_em":"…","cru":"…","eventos":[],"parse_error":""}
> ```
>
> Normalization happens in one place only, when the envelope is assembled, and it is locked by a test
> that asserts on the **bytes on the wire** — not on the in-memory structure, which is what let the
> defect below go unnoticed for five days.
>
> ⚠️ **FROM 2026-07-23 TO 2026-07-28 THIS FIELD WAS `null` ON THE WIRE, and the defense you wrote
> because of it still holds.** The parser produced a **nil** list when there was no event at all, the
> envelope passed it through without normalizing, and serializing a nil list in Go produces `null`.
> Anyone following this contract to the letter wrote `for ev in envelope["eventos"]` and got
> `TypeError: 'NoneType' object is not iterable` in Python; an `if eventos == []` **never** matched.
>
> **Do not undo that defense.** `for ev in (envelope.get("eventos") or [])` works the same with `[]`,
> is still the recommended form, and is what protects you from any future in which the field goes
> missing again. The direction of the change was chosen precisely for that: whoever tolerated `null`
> keeps working, and whoever broke started working.
>
> ⚠️ **If you are writing the defense today, write `or []`, NEVER `get("eventos", [])`** — and the
> difference is not style. In Python the `get` default only applies when the **key does not exist**;
> it always exists here, so a `null` (from the history, from an old envelope you kept, or from any
> other source) sails past the default and you get `None` back. `envelope.get("eventos", [])` **looks
> like** the clean version of the line above and is the "simplification" any reviewer would suggest.
> *Raised by a consumer on 2026-07-28, after checking their own code: it did not break, but by
> accident, and the trap they named is this one — it is not just the `for` blowing up, it is the
> **natural fix** that brings the bug back.* The equivalent in JS (`??`, not `||` — although both work
> here) and in Go (a nil list already iterates zero times) does not have this catch; it is Python's,
> and that is why it is written with the exact name of the error.
>
> **`parse_error` is neither `null` nor absent: it is the empty string (`""`)** when there was no
> error, and **that never changed and never will**. The field is not omitted and therefore comes
> **always**. Test by content (`if parse_error:`), never by the presence of the key. *Why it was not
> part of the `eventos` fix: its empty value is already **falsy** in every language, and nobody
> iterates over an error string. `""` breaks nothing; `null` in a field that is ITERATED does. That is
> the rule.*
>
> **How to tell the two cases apart, which is the only thing you need to know here:**
>
> | What arrived | `eventos` | `parse_error` | What it means |
> |---|---|---|---|
> | unmodelled account webhook | `[]` | `""` | normal — store the `cru` and move on |
> | payload the gateway could not interpret | `[]` (or partial) | **error text** | the `cru` is there and it is the source: store it, and treat the batch by the `cru` dedup key |
>
> 🔴 **Why this warning exists, and why it probably applies to YOU too:** when saving a new Callback
> URL, Meta's panel subscribes the App to a **default set of fields all at once** — ten, in the
> 2026-07-28 measurement — and the gateway models only some of them (the table above says **which**,
> and it is the table that counts, not a count). That is, **a batch with no event is not a rare
> exception: it is routine traffic from your instance's very first day**, and "an event that becomes
> nothing" is easy to confuse with "a parse failure". Check, in your App's panel, which fields it is
> subscribed to — that is where how much of that traffic you receive gets decided.

**Meta does not let you have one webhook URL per instance for these.** Confirmed at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/override/ (read on
2026-07-26): message and status support a URL override per number/WABA; the template and account
fields above do **not** — they always arrive at the App's main URL at Meta, never at your per-instance
`callback_url`.

**That means the gateway has to decide, on its own, whether an account webhook that arrived is yours**
— and the key it uses to decide is the `waba_id` (`entry[].id` in Meta's payload, confirmed as
*"WhatsApp Business Account ID"* on the official pages cited above and at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/,
same date). If the webhook's `waba_id` matches your instance's, you receive the `cru` normally,
through the same `POST` to your `callback_url`. **If it does not match, you receive nothing** — the
gateway discards it and answers `200` to Meta, without trying to guess which other instance that
belonged to.

> ⚠️ **Until 2026-07-26, an account webhook arriving at the main URL was delivered to the consumer
> configured there even if it belonged to another WABA — and it was not just wrong routing.** The
> tenant isolation guard only compared `phone_number_id`, and an account webhook has no such field. If
> you had an `if` checking `envelope["instancia"]` against the configured slug (recommended behaviour,
> see *The wrong instance*, further below), it **passed** — because the gateway itself puts the path's
> slug into the envelope — and another tenant's account webhook could end up stored in your database
> as if it were yours. That is closed now: an account webhook only reaches you if the `waba_id` really
> is your instance's.

**It stopped being just theory on 2026-07-26:** a second test instance was created in the production
gateway, exercised with signed traffic and removed the same day. A signature valid for one instance,
sent on the other's path, got `403`; and the alarm for someone else's `phone_number_id`, which had
never fired, fired when provoked. In that exercise the traffic's origin was the gateway itself being
tested, not Meta.

> **A sentence was REMOVED from here on 2026-07-28 instead of updated, and the reason is worth more
> than the sentence:** this paragraph asserted *"today there is a single instance registered in this
> gateway"*. It was true when written and aged **in silence** — a production count changes without the
> contract changing with it, and an out-of-date number in a doc is worse than no number, because the
> reader trusts it. **This document does not answer how many instances exist, and never will.** What
> it promises is the **guarantee**, which does not depend on the count.

### What separates you from the other tenant is the SIGNATURE, not the id guard (2026-07-28)

**This project's isolation doctrine had been describing the `phone_number_id` and `waba_id` guards
(the two sections above) as what separates one tenant from another. It is not.** The correction is
here because a consumer who reads the sections above and stops there concludes that "a discard
counter at zero" means "no foreign traffic came anywhere near" — and that is false.

There are **two layers**, with different purposes, HTTP responses and reach:

| | **The boundary** | **The addressing check** |
|---|---|---|
| What it is | the **HMAC signature from Meta, verified with THAT instance's `app_secret`** | guards 5a (`phone_number_id`) and 5b (`waba_id`) |
| Where | in step 3 of webhook processing | in steps 5a and 5b, after the parse |
| What it answers | **`403`, and processing stops there** — nothing reaches step 4 (parse) or step 5 | **`200`, discarding the batch** |
| What it protects against | **another App** — that is, another tenant, and anyone who discovers your URL | **your own configuration error**: a Callback URL pointed at the wrong path, a missing webhook override, a WABA with more than one number |
| Proof | exercised with real traffic on 2026-07-26: the **same bytes** with the **same signature**, sent on the other instance's path, got `403` | provoked on purpose in the same task: `200` + `ALARME` + counter |

**Guards 5a/5b exist to catch a configuration error, not an intruder.** An intruder does not get the
`phone_number_id` wrong — they never even reach step 5, because they do not have the `app_secret` to
sign with.

> 🔴 **The corollary, which nobody deduces on their own from the sections above: the guards go MUTE
> when two Apps share the same number and the same WABA.** In that scenario the body's
> `phone_number_id` **matches** the registered one and the `waba_id` **matches** too — both guards see
> everything as fine, no `ALARME` comes out, and **no counter moves** (`numero_descartado` and
> `conta_descartada` stay at zero). **Silence from the guards is not proof that there is no
> co-tenancy**; it is proof that the addressing is coherent, which is a different question. Anyone
> treating a zero there as "nobody else is plugged into this number" will conclude wrongly — and this
> project concluded wrongly exactly like that, on 2026-07-28, before asking the number's owner.
>
> **What remains guaranteed in that scenario, and it is what matters:** the other App has **another
> `app_secret`**, so its webhook does not close your instance's signature and gets `403` at step 3.
> The boundary still stands — it is just not the id guard holding it up. **That is why there is no new
> countermeasure here**: one more guard for "identical ids" would be complexity without a threat,
> since the layer that decides is a different one.

**The sentence above has a condition, and hiding it would be trading one wrong doc for another:** "the
signature is the boundary" holds because **each consumer uses their own App** at Meta (a project
decision, 2026-07-26), and one's own App means one's own `app_secret`. **Two instances in the SAME
App would have the same `app_secret`** — and nothing in the gateway prevents registering the same
value twice — and in that configuration the signature **stops distinguishing** the two: one's webhook
closes the other's accounting, and what separates them becomes exactly 5a/5b. **The two guards exist
for that configuration**, and that is why they are not ceremony. What this section changes is the
**name**: today, with one App per consumer, they check addressing; the day two numbers share an App,
they become the only separation — and then their zero starts meaning something quite different.

### Your `callback_url` must be `https://`

The gateway **refuses to create** an instance with an `http://` `callback_url` at creation time. This
is not a preference: the `POST` above carries the message's **raw body** — what the customer wrote,
their phone number, their name. Over `http://` that crosses the network readable by anyone on the
path, and `X-Zapgw-Signature` does not help: **a signature proves integrity, not confidentiality.**

Two exceptions, and only two:

- **An empty `callback_url` is valid** — that is the outbound-only instance, which sends and does not
  receive. No delivery is not delivery in the clear.
- **`http://127.0.0.1` and `http://localhost`** (with any port and path), for development on your own
  machine. The check is about the **host** the URL resolves to, so `http://127.0.0.1.exemplo.com` and
  `http://127.0.0.1@exemplo.com` are refused — they start with the permitted text and deliver
  outward.

### And your certificate is verified — there is no way to turn that off

Requiring `https://` without verifying the certificate would be theatre: the URL would still say
`https` and there would be no guarantee behind it. So verification on delivery is **strict** and
**there is no option to turn it off** — no flag, no environment variable, no configuration field, no
"only in development". This is a project rule, not a preference of whoever implemented it: turning off
verification generates no error at all, it only removes a protection, and that is why the option stays
on forever the day it exists.

In practice, what this requires of you:

- **A valid certificate, within its validity period, for your `callback_url`'s hostname.**
  Self-signed with nothing else does not pass.
- **If you use your own CA** (an internal consumer, without a public CA certificate), that is the way
  out, and it exists precisely so the escape hatch does not have to: send the **CA** to whoever
  operates the gateway, and it is registered **on that instance** (`--bundle-ca`, a PEM file, kept
  encrypted at rest). A CA registered by one instance does **not** apply to any other. It is still
  verification: chain, validity and hostname are still checked.
- **An expired certificate is the most expensive case, and you are the one who sees it first.** On the
  gateway's side the delivery fails, it answers `504` to Meta (keeping the retry window open, which is
  what gives someone time to fix it) and writes an `ALARME` line saying it is a certificate and what to
  do, in the service log. But **no retry fixes a certificate**: when Meta gives up, the messages from
  that period are lost for good. Renew beforehand.

The instance's **slug** is also validated at creation, and is **immutable** afterwards: lowercase
`a-z`, digits and hyphen, no hyphen at the ends, 3 to 40 characters. It becomes `/v1/inbound/{slug}`,
and a character outside that shape produces a URL that Meta accepts pasting and can never verify —
with **its** error message pointing at the wrong place.

---

## Sending a message

**`POST https://zapgw.exemplo.com.br:8443/v1/messages`** · `Authorization: Bearer <your token>`
· `Idempotency-Key: <your key>`

**The port is not a detail — it is where sending exists, and it only answers on the LAN** (the
limitation announced at the start of this document). The gateway's public address serves **only**
`/v1/inbound`; sending lives on an internal entrypoint, and from the internet that port **does not
exist** — checked on 2026-07-28 from 12 points outside the network, all timing out, with the public
path as a positive control. Calling `/v1/messages` on the public address (port 443) returns **`404`**,
on purpose.

> ⚠️ **That `404` is the most misleading symptom in this document.** It is indistinguishable from
> "wrong path" or "this version of the gateway does not have that route", and it sends you
> investigating deployment and code when the problem is **which address you hit**. If you got a `404`
> on a route that exists on this page, check the URL's base and port **before** anything else.

The certificate is valid for the hostname you received — **do not disable TLS verification** in your
HTTP client. What travels there is your token, and it is exactly so that it does not go across the
network in the clear that this path is `https`.

The `Idempotency-Key` is **mandatory**. Without it the request is refused with `400` — not because we
are strict, but because without it one retry of yours becomes **two messages on your customer's
phone**, and we have no way of telling.

Choose a key that identifies the *intent*, not the attempt: your queue row's id works; a fresh UUID
per retry is useless.

> ⚠️ **The key goes into the gateway's log** when something goes wrong with it (it is the only way
> anyone can unstick it). Do not put personal data in the key — no phone number, national id or
> e-mail. An internal id of yours is not personal data to whoever reads the log; a phone number is.

> 🔴 **The key travels in an HTTP HEADER: any non-ASCII character in it dies in YOUR client, before
> leaving.** Emoji, accent, `ç` — the HTTP client raises an exception and **no request reaches the
> gateway**. Do not look in our journal: there is nothing there. From the operator's side, the symptom
> is a `500` in your system and absolute silence in ours.
>
> *Raised by a consumer on 2026-07-28, with the exact error:*
> `UnicodeEncodeError('ascii', 'reacao:<id>:❤', 'ordinal not in range(128)')`. *They had put the emoji
> in the key on purpose — to distinguish 👍 from ❤️ as two intents, which is right (see `reacao`,
> below). The defect was only the transport.*
>
> **Fix it by ESCAPING, not by sanitizing** — and the difference is the trap: `quote`/percent-encoding
> is **injective** (👍 and ❤️ still generate different keys); *removing* the non-ASCII characters
> **would collapse both into the same key**, and then the second request would get `422` or, worse,
> the intent would stay frozen on the first. Trading a noisy error for a silent bug is the worst deal
> available. **Escape in one place only**, at the edge that assembles the header.
>
> *This applies to any key carrying human text: an alert title in pt-BR has accents, and the same shot
> is loaded there.*

**One key serves ONE request.** The same key with a different body is refused with `422`, and that is
not strictness: the contract recommends using your entity's id as the key, and the same entity usually
sends several messages (a reminder, a bill, an apology). Without that refusal, the second one would
get `200` with the **first one's** `wa_message_id`, you would record "sent", and the message **would
never go out**. The gateway compares a hash of the already normalized request — a space more or less
does not count as a different request.

```jsonc
{ "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "texto" | "template" | "botoes" | "cta_url" | "lista" | "midia" | "reacao" | "localizacao" | "contatos" | "pedir_localizacao" | "flow",
  "responder_a": "wamid…",   // optional; FORBIDDEN in template

  "texto": "…",                                   // texto, botoes, cta_url, lista, pedir_localizacao, flow
  "template": "lembrete", "idioma": "pt_BR",      // template
  "variaveis": ["Maria", "19h"],                  // template, optional (the template's body)
  "cabecalho": {"tipo": "documento", "media_id": "…", "nome_arquivo": "recibo.pdf"}, // template, optional
  "botoes_template": [{"indice": 0, "tipo": "url", "texto": "…"}, // template, optional
                       {"indice": 1, "tipo": "resposta_rapida", "payload": "…"}],
  "botoes": [{"id":"SIM","titulo":"Sim"}],        // botoes (interactive message — NOT a template) — titulo: max 20 characters, list: max 3 items
  "botao_titulo": "Abrir", "botao_url": "https://…", // cta_url — botao_titulo: max 20 characters, botao_url required only in cta_url
  "secoes": [{"titulo": "…", "itens": [{"id":"…","titulo":"…","descricao":"…"}]}], // lista, required — botao_titulo (reused from cta_url) is the label of the button that opens the list; see its own section for the ceilings
  "fluxo": {"id": "…", "token": "…", "acao": "navigate", "tela": "…"}, // flow, required — id OR nome (never both), token required; botao_titulo (reused, own ceiling) is the flow_cta; see its own section
  "cabecalho_texto": "…", "rodape": "…",          // botoes, cta_url, lista — optional

  "media_id": "…",                                // midia, required (from POST /v1/media)
  "categoria": "imagem"|"video"|"audio"|"documento"|"sticker", // midia, required — PTT is also "audio"
  "legenda": "…",          // midia: only imagem, video and documento
  "nome_arquivo": "nota.pdf", // midia: only documento

  "reacao": {"alvo": "wamid…", "emoji": "👍"},     // reacao, both required — see its own section
  "localizacao": {"latitude": 37.44, "longitude": -122.16, "nome": "…", "endereco": "…"}, // localizacao; nome/endereco optional
  "contatos": [{"name": {"formatted_name": "João Vendedor"}, "phones": [{"phone": "5511999990000"}]}] // contatos, required — only name.formatted_name is demanded; the card's interior uses the Cloud API's own field names (name, phones[].phone, org.company…), in English, because they are ~25 nested vCard fields and a translation table would diverge on Meta's first new field
}
```

> ⚠️ **`botoes` and `botoes_template` are TWO DIFFERENT THINGS, despite the similar name.**
> `botoes` is the whole body of an ordinary interactive message (`"tipo": "botoes"`, no template),
> with the shape `{"id": "…", "titulo": "…"}`. `botoes_template` is a **template** parameter
> (`"tipo": "template"`), with the shape `{"indice": …, "tipo": "url"|"resposta_rapida", …}` — see the
> section *Template: header and button*, below. Confusing the two is a `400`, a named error pointing at
> the right field: the gateway does not silently discard the wrong field, but if you do not read the
> error message you may think "botoes" also works for a template. **It does not.**

> ⚠️ **`instancia` is the NUMBER, it is not you.** It is the **slug** of the WhatsApp number that
> sends — item 1 of your delivery package. Who *you* are already comes in the `Authorization: Bearer`;
> there is no field for that in the body, and putting **your** name in `instancia` is the easiest
> mistake to make here, because both are names of things of ours and look interchangeable.
>
> **The symptom, if you get it wrong, does not point at the mistake.** On sending it is
> `404 instancia desconhecida` or `403 instancia nao autorizada para este consumidor` — that one is
> still readable. On **delivery** it is worse: a consumer who checks `envelope["instancia"]` against
> the configured value (recommended behaviour, see *The wrong instance*) answers `503` to every
> delivery, Meta re-queues for 36 h, and the stubborn `503` **looks like a signature problem**. Three
> examples in this very document used to carry, in `instancia`, the name of a **consumer** instead of
> the slug — corrected on 2026-07-26, after someone read the example before using it. That is why the
> conventions section, at the top, says that **every** example value here is fictitious.

Success returns `200` and `{"wa_message_id": "wamid…"}`. **Repeating the same key returns the same
id, without sending again — but only within the idempotency record's retention window.**

> 🔴 **The `wa_message_id` IS NOT OPAQUE: it carries the recipient's phone number inside it, in
> base64.** That is not our choice — it is Meta's format — but it is our duty to warn you, because we
> are the ones asking you to **store** that id for deduplication.
>
> ```
> $ echo "<the part after 'wamid.'>" | base64 -d | strings
> ```
>
> The number comes out in **WhatsApp's canonical form**, which for Brazil is **without the ninth
> digit**. That is the part that deceives: a search for the phone number as a human writes it — with
> the `9` — **sails right past** and returns "found nothing", about a search incapable of finding.
>
> **The three practical consequences for you:**
>
> 1. **A `wamid` does not become a fixture.** If you record a test case from real traffic, masking
>    `recipient_id` and `de_cru` **is not enough** — the phone number is still whole inside the `wamid`
>    in the same file, and the result *looks* masked. For a fixture, generate a synthetic `wamid`.
> 2. **Your phone-number sweep needs to DECODE.** A `grep` for the number does not find what is in
>    base64. Ours decodes every `wamid.<payload>` in the tree before comparing, precisely for this
>    reason.
> 3. **The same applies to the envelope's `cru` on the inbound side** (the receiving section): it is
>    the exact body Meta sent, so it has phone numbers in text **and** inside any `wamid` it carries.
>
> ⚠️ **This is not theory.** On 2026-08-30 a consumer masked the phone number in a request body and
> pasted the whole `wa_message_id` next to it, in the same report; searching with decoding, they found
> **another `wamid`, already committed in their repository for weeks**, with the same number inside.
> A private repository, zero damage — and the fix, had it been public, would not have been `git rm`.
>
> *This warning lived for two months only in the gateway's internal documentation, which you do not
> read. It is here now because the trap belongs to the format **we** distribute.*

> ⚠️ **What the `200` means, and what it does NOT mean.** `200` + `wa_message_id` means **"Meta
> accepted the request"** — never **"the message arrived"**, nor **"it will arrive"**. That was always
> true (the actual delivery confirmation is the webhook's `status` event — see the receiving section),
> but now there is a concrete case in which the distance between the two becomes visible in the send's
> own response:
>
> For messages with `"tipo": "template"` whose template is under **pacing** (a Meta mechanism that
> holds part of a mass send back to collect feedback before releasing the rest — Meta's decision about
> your template's health, not something you choose in the request), the response may carry an extra
> field: `{"wa_message_id": "wamid…", "message_status": "held_for_quality_assessment"}`. When that
> happens, the `200` **does not guarantee delivery**: Meta may release the message later (positive
> feedback) or **drop it without ever delivering it** (negative feedback) — both remain `200` for the
> gateway, because the request really was accepted.
>
> **`message_status` only appears in the body when the value is different from `"accepted"`.** That is
> deliberate, not byte thrift: `"accepted"` and the **absent** field (today's case, for every send that
> is not a template under pacing — text, media, interactive, reaction, location, and even a template
> outside pacing) mean exactly the same thing from the point of view of someone who only reads
> `wa_message_id`, and that is why both produce the **same body as always**. Only a value that changes
> what the `200` promises — today, `held_for_quality_assessment` or `paused` — appears.
>
> **An example of the format, with the two values Meta documents for that case** — the key/value pair
> is exactly what comes out (spacing here is only for readability), and both values are pinned by
> tests:
>
> ```json
> {"wa_message_id": "wamid.RETIDO", "message_status": "held_for_quality_assessment"}
> ```
> ```json
> {"wa_message_id": "wamid.RETIDO", "message_status": "paused"}
> ```
>
> **The gateway does not translate that value, neither into Portuguese nor into the send's error
> classes** (`retentavel`/`permanente`/`config` — see the errors section). It passes on what Meta sent,
> raw. If you receive a `message_status` that is neither `held_for_quality_assessment` nor `paused`,
> treat it as "Meta is saying something we have not documented yet" — not as silent success.
>
> **Source, read on 2026-07-26:**
> `developers.facebook.com/documentation/business-messaging/whatsapp/messages/send-messages` — the
> field "is included in the response only when the send is of a template message that uses a template
> under pacing"; and
> `developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-pacing` — on
> the outcome of `held_for_quality_assessment`: *"If the feedback is positive... the held messages will
> be released and sent normally"*, but *"If the feedback is negative... Each held message will be
> dropped"*. The two official pages **disagree** on whether `paused` is a value of the message's
> `message_status` or of the template's `status` — the gateway does not decide which of the two is
> right, it only passes the raw value through.

The record is purged after a TTL (default **72h**, configurable via `ZAPGW_TTL_IDEMPOTENCIA_HORAS` on
the operator's side). Past that window, the same key is no longer recognized: the gateway treats it as
a new send and **sends the message again** — exactly what this feature exists to prevent. The
guarantee is not "forever"; it is "for as long as the record exists". If your retry process can sit
idle for longer than the TTL (a stuck queue, a slow deploy, an incident), do not trust the key alone
after that — check first whether the message already went out by some other means (e.g. by checking
whether the `wa_message_id` has already been received on your side).

### Header and footer in interactive messages (`botoes` and `cta_url`) (2026-08-20)

An interactive message accepts a **free-text header and footer**, and they go through no approval at
all: they are valid inside the 24 h window, like the rest of the message.

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "botoes",
  "cabecalho_texto": "Racer Autopeças",        // optional, max 60
  "texto": "Confirma o orçamento #4471?",
  "rodape": "Responda até as 18h",             // optional, max 60
  "botoes": [{"id": "SIM", "titulo": "Confirmar"}, {"id": "NAO", "titulo": "Cancelar"}] }
```

Both fields are valid **only** in `tipo: "botoes"` and `tipo: "cta_url"`. Sent in any other type, they
are a `400` naming the field — never silently discarded.

**The ceiling is 60 characters in each, counted in CHARACTERS and not in bytes** — an accent or an
emoji costs one, as you would expect. *(Source: Meta's documentation,
`developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-list-messages`, read on
2026-08-20. If it squeezes anyone in practice, say so: in the catalogue of today's largest consumer,
measured on 2026-08-20, the longest footer in use has 33 characters and the longest header 35 — 60 is
comfortably enough, but that is one catalogue's measurement, not a guarantee about yours.)*

> ℹ️ **`cabecalho_texto` is TEXT only.** The Cloud API also accepts a **media** header in an
> interactive message (image, video, document); **this gateway does not assemble that today**, and
> there is no field to ask for it. It is not a project refusal — it is the absence of a use case. If
> you have one, ask: the field name was chosen with the `_texto` suffix precisely so a
> `cabecalho_midia` can sit beside it without renaming anything that already exists.

#### 🔴 This does NOT exist in a template, and the difference is not the gateway's — it is Meta's

The question always comes up, and the answer saves an afternoon: **in a template, header and footer
are fixed at registration.**

| | in the template (approved by Meta) | in the interactive message (`botoes`, `cta_url`) |
|---|---|---|
| **header** | fixed text with **at most 1 variable**, or media | free text, yours, on each send |
| **footer** | fixed text, **with no variable at all** | free text, yours, on each send |

*(Source: `developers.facebook.com/docs/whatsapp/business-management-api/message-templates/components`,
read on 2026-08-20 — "Text headers support 1 parameter"; the footer is described as a text component
and admits no parameter.)*

Practical consequence: **there is no way to vary a template's footer at send time.** If the text needs
to change per message, either it becomes an interactive message (inside the 24 h window), or it goes
into the template's **body** as a variable. Sending `rodape` or `cabecalho_texto` in a
`tipo: "template"` is a `400`, with the message pointing at `cabecalho` — which is the **template**
header, a different field and a different shape.

> ⚠️ **And the gateway will not paste the footer text at the end of the body to simulate a footer.** It
> would look similar on screen, and it would be content **we** wrote inside **your** message, without
> you seeing it. If you want that sentence in the body, write it yourself — then it is yours, reviewed,
> and you control it.

#### `botao_titulo` of `cta_url`: maximum 20 characters, and this number has a different origin

The link button's label is refused above **20 characters**, with a `400` naming `botao_titulo`.

**This ceiling does not come from documentation, and the document will not pretend it does.** The
Cloud API's official reference (`developers.facebook.com/docs/whatsapp/cloud-api/reference/messages`,
read on 2026-08-20) **declares no limit at all** for that field. The 20 is a **measurement on a
device**, made by consumer `consumer-b` on 18/08/2026 by bisection: 17 passed, 21 failed, 19 and 20
passed.

**Why the gateway refuses at the input instead of letting Meta refuse:** because what the gateway
PASSED ON from its refusal arrived anonymous. Meta answers `(#131009) Parameter value is not valid`,
and until T-141 the gateway only read `message` and `code` from the error body — so it was **that**,
without the parameter, that reached you. The cost of this has already been paid — a customer's message
went out **without the button**, and finding out why required manual bisection on a test number.

🔴 **Correction: "Meta does not say which parameter" was never verified — what was missing was our own
reading.** There is evidence that it names the field and the ceiling in `error_data.details` (a record
from 18/07/2026, found by the consumer through another transport, came with "Button title length
invalid. Min length: 1, Max length: 20"). Since T-141 the gateway reads **only** that key and passes it
on in `detalhe_meta` (see the section *What each error means*, below) — but whether Meta still sends
that detail through today's path is an **open** question until measured against production; do not
treat `detalhe_meta` as guaranteed on every refusal. That is why the ceiling stays at the input: a
guard that validates before calling Meta does not depend on Meta sending any detail.

> ⚠️ **Refusing is all the gateway does here.** It does not shorten the label, does not switch to a
> template and does not choose another path for you — that is orchestration, and the context to decide
> is on your side, not ours. If your label can exceed 20, check it **before** asking.

> 🔎 **The neighbour has the same ceiling, and this time the measurement is OURS.** The `titulo` of
> each item of `botoes` (the quick-reply button) is also refused above **20 characters**, with a `400`
> naming `botoes[N].titulo` — the index of the wrong button, not just the field. Measured on
> 2026-08-20, against Meta's real production (instance `tenant-one`, messages actually sent): 20
> characters passes, 21 fails with the same anonymous `(#131009)`, and a label of 20 **accented**
> characters (40 bytes) also passes — the count is per character, not per byte. Like the ceiling, it is
> not documented by Meta: if they change the value, only a new measurement on a device corrects it.

#### `botoes[]`: maximum 3 items (2026-08-20)

The `botoes` list (interactive message `tipo: "botoes"`, quick-reply button) accepts **1 to 3** items.
With 4 or more, it is a `400` naming `botoes` with how many came and the maximum.

**Measured in production, against the real Meta, on 2026-08-20**, with `v0.52.0` already live — four
buttons actually sent by instance `tenant-one`:

```
HTTP 400
codigo_meta  131009
mensagem     (#131009) Parameter value is not valid
detalhe_meta Invalid buttons count. Min allowed buttons: 1, Max allowed buttons: 3
```

This `detalhe_meta` is T-141's pass-through (see *What each error means*, below) working for the first
time against the real Meta — it is what revealed this limit. Unlike the neighbour's 20-character
ceiling above (found by manual bisection), this number **came read from the error's own text**.

> 🔴 **The gateway will not mirror Meta's entire limits table.** This 3-item ceiling went into the
> input because it is a **structural** constant of the block the gateway itself assembles (the button
> list of the JSON that goes out of here) — not because "every limit should become a validation". A
> mirror of Meta's limits table would age and start lying; from T-141 on you already receive the right
> field and number in `detalhe_meta` when Meta refuses, which covers most cases without demanding one
> more constant to keep up to date here.

### Template: header and button

A template can have three places where data of yours goes in, and the gateway assembles all three:

| Request field | Becomes, at Meta | What for |
|---|---|---|
| `variaveis` | `components[].type = "body"` | the body's positional variables (`{{1}}`, `{{2}}`…) |
| `cabecalho` | `components[].type = "header"` | the template's header: text, image, video or document |
| `botoes_template` | `components[].type = "button"`, `sub_type: "url"` or `"quick_reply"` | the dynamic parameter of a template button — a URL suffix (tracking token) **or** a quick-reply payload (a booking id, etc.) |

> 🔴 **`botoes_url` WAS REMOVED on 2026-07-28.** `botoes_url` was the name of the FIELD, not of the
> feature: it was the URL button field, succeeded by `botoes_template`, and it lived alongside its
> successor for two days. The URL button went nowhere — it is `{"tipo": "url"}` inside
> `botoes_template`. A request that still sends `botoes_url` receives a **`400` with a named error and
> the translation ready** — it is never ignored. See *Breaking changes*, at the end of this document.

All three are optional and independent. **If you send none of them, the request goes out without the
`components` key** — and that is not a detail: Meta refuses `components: []` on some templates, and
the difference only appears on a real send.

**You do NOT send a ready-made `components`.** Sending the Graph API's format is refused with `400`
and the named error `components cru da Graph API nao e aceito`. The reason is the reason this gateway
exists: the schema here makes what Meta rejects *inexpressible*, and a raw pass-through would hand
that entire class of error back to your production — you would assemble it wrong, Meta would refuse,
and the gateway would have protected nobody.

**`cabecalho`** — `tipo` is `"texto"`, `"imagem"`, `"video"` or `"documento"` (the same vocabulary as
`categoria`; `audio` and `sticker` are not valid as a header). In `"texto"` send `texto`; in the
others send `media_id`. `nome_arquivo` only exists in `"documento"`. A field on the wrong type is a
`400` — it would disappear silently, and you would see `200` with the header missing on the customer's
phone.

> ⚠️ **A media header is by `media_id`, never by URL.** There is no URL field, and a `media_id` shaped
> like a URL is refused with `400`. A raw URL makes **Meta go and fetch** the file, and when it does
> not fetch — host down, TLS, `404` — **there is no error anywhere**: the template arrives without the
> document. That is exactly the silent failure that made `POST /v1/media` exist. Upload the bytes
> there and use the id it returns.

> ℹ️ **A voice note (PTT) has no category of its own: send `categoria: "audio"`.**
> On the **Cloud API** channel, a voice note and ordinary audio go through the **same** path —
> `tipo: midia` with `categoria: audio`. There is no separate category for PTT.
>
> **Anyone coming from a Baileys-based system tends to look for the equivalent of `sendWhatsAppAudio`,
> and the risk is not getting an error — it is the opposite: the send is accepted and does not
> deliver.** A silent failure, of the kind that only shows up when someone on the other side says "I
> didn't get it".
>
> *Origin, and it matters because it changes how much this is worth:* the formulation above comes from
> a consumer (2026-07-28), and the **"accepts and does not deliver" is knowledge INHERITED** from their
> history with Evolution/Baileys — it is **not** a measurement against this gateway. What was measured
> here, on 2026-07-28, is the path that **works**: `voice` collapsed into `audio`, sent through
> `POST /v1/messages`, delivered and listened to on the device.
>
> **Open, and written as a question on purpose:** WhatsApp distinguishes PTT from ordinary audio in the
> **rendering** (waveform bubble vs. file). Nobody checked how the device rendered what arrived in that
> test — only that it arrived and was listened to. If you need PTT rendering specifically, **this is
> not answered**, and answering by analogy here would be inventing.

**`botoes_template`** — a **union discriminated by `tipo`**, in the same pattern as `cabecalho`: each
item is `{"indice": …, "tipo": "url"|"resposta_rapida", …}`, and the field that comes after `tipo`
changes with it.

```jsonc
// the URL button — the same component the removed field `botoes_url` produced
"botoes_template": [ {"indice": 0, "tipo": "url", "texto": "BR123456789BR"} ]

// and the one that only exists here: the quick-reply button, with a payload of yours
"botoes_template": [ {"indice": 1, "tipo": "resposta_rapida", "payload": "confirma:41"} ]
```

> ⚠️ **Meta calls this `button`, and this task's original request asked for the name `botoes` — but
> the real field is called `botoes_template`.** `Pedido.Buttons` (json `"botoes"`) already existed
> before, for `"tipo": "botoes"` (an ordinary interactive message, `{"id","titulo"}`) — and two Go
> structs with the SAME JSON tag at the same level are **silently ignored** by both `Marshal` and
> `Unmarshal` (confirmed by experiment before writing this field: no error, both fields end up empty).
> Reusing `"botoes"` here would erase both features at once, with no signal at all — this project's
> own mother-trap (*"the rule holds here, it does not hold there"*), this time inside `encoding/json`.
> Hence: **use `botoes_template`** in your new requests.

- `"tipo": "url"` — carries `texto`; produces **exactly** the same component the removed field
  `botoes_url` produced (same block shape, same `sub_type`, frozen byte by byte in a test). Migrating
  a URL button changes nothing Meta can see.
- `"tipo": "resposta_rapida"` — carries `payload` instead of `texto`; becomes
  `sub_type: "quick_reply"` with the parameter `{"type": "payload", "payload": …}`. **It is the path
  that was missing**: without it, a click on a template's button comes back as the button's **text**
  ("Sim"), not an id your system recognizes — and a flow expecting `confirma:41` finds nothing to
  match.

> ⚠️ **`"resposta_rapida"` has not yet been through real Meta traffic** (situation as of 2026-07-28,
> revisited when `botoes_url` was removed). It is proven by the suite and by mutation — the test
> distinguishes `type:"payload"` from `type:"text"`, not just the parameter's presence. But *API
> success ≠ effect success* is a recorded trap of this project, and no template with a quick-reply
> button **and a dynamic payload** has been sent through this path so far: the consumer who came
> closest has a **static** quick reply (no parameter, therefore no component emitted).
>
> **The proof is yours, and it fits in five minutes:** send a template with `resposta_rapida` to a
> number of your own, **click the button on the device** and look at the inbound event. If
> `botao_payload` comes with your payload, it is proven for your case; if it comes with the button's
> **text**, the path does not do what you need and you found that out before a customer did. Run that
> test **before** wiring up any flow that matches by payload — this document cannot confirm on your
> behalf what nobody has observed yet.
>
> **The `"url"` half, on the other hand, does have device proof:** on 2026-07-28, while confirming that
> `botoes_url` could be dropped, a consumer sent a template through `botoes_template` and **clicked the
> button on the phone**, with the portal opening. They measured alongside that the 24 h window was
> **closed** (57 h since the last inbound) — without that control, the message could have gone out as
> free text and the test would have proven something else.

> **A single list, with `tipo` per item, even if your catalogue today does not need it.** A consumer
> pulled their real catalogue (2026-07-26, straight from Meta): **90 approved templates, 38 with a
> button, 51 `QUICK_REPLY` buttons and 17 `URL` — and no template mixing the two types.** That is: if
> you "simplify" this one day by splitting it into fields, the current catalogue **will not contradict
> you**. Nor does it authorize you: that is an observation about the templates that exist today, **it
> is not a Meta guarantee** — nothing prevents the next approved template from being mixed. The
> discriminated union covers that day without anyone remembering anything; two fields with a
> cross-check depend on someone remembering. That is why the list is a single one.

`indice` is the button's position **in the template**, starting at `0`, in the order the buttons were
declared at Meta — not the position in the list you send. A negative index is a `400`. **Two parameters
for the same index is also a `400`**: it is the same button declared twice, Meta would silently discard
one of the two parameters, and we would not be the ones choosing which.

> ℹ️ **Until 2026-07-28 that check had to cross two fields** (`botoes_template` and the old
> `botoes_url`), because both wrote into the same index space. With `botoes_url` removed, it is **one
> list, one index space** — there is no longer any way to spread the same template's buttons across two
> places, not even by mistake.

`payload` is **opaque** to the gateway — it can carry an internal id of yours (e.g.: `"confirma:41"`).
It **never appears in a log**: the error message names the field, never the value.

> ⚠️ **A wrong index produces no error at all.** The token goes to the wrong button, the customer
> clicks and lands in the wrong place. Check the button order in the catalogue (`GET /v1/templates`)
> before fixing the number.

> ⚠️ **The button's numbering is its own, not the body's.** In a template whose registered URL is
> `https://cliente.exemplo.com.br/{{1}}` **and** whose body also uses `{{1}}`, the two `{{1}}` are
> different things. The button's `texto` fills the URL's `{{1}}`; `variaveis[0]` fills the body's
> `{{1}}`. Confusing the two sends the customer's name inside the link.

The examples below have the **shape** of genuinely approved templates: each one's variable count was
checked against `GET /v1/templates` on a real account on 2026-07-26, so they are not invented shapes.
**The template names are examples** — yours are the ones in your catalogue, and it is your instance's
`GET /v1/templates` that says how many variables and how many buttons each one has.

Example — **document header** (`venda_confirmada`: `HEADER DOCUMENT`, three body variables, no
buttons):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "venda_confirmada",
  "idioma": "pt_BR",
  "cabecalho": {
    "tipo": "documento",
    "media_id": "1234567890123456",
    "nome_arquivo": "comprovante-4210.pdf"
  },
  "variaveis": ["Maria", "4210", "R$ 349,90"]
}
```

Example — **URL button** (`equipamento_enviado`: four body variables and **one** URL button, at index
`0`):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "equipamento_enviado",
  "idioma": "pt_BR",
  "variaveis": ["Maria", "OS-1187", "Correios", "BR123456789BR"],
  "botoes_template": [
    {"indice": 0, "tipo": "url", "texto": "BR123456789BR"}
  ]
}
```

Example — **quick-reply button** (a booking confirmation template with a `quick_reply` button at index
`0`, whose click comes back with the `payload` — not the text "Sim" — in the inbound event):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "confirma_agendamento",
  "idioma": "pt_BR",
  "variaveis": ["Maria", "terça, 14h"],
  "botoes_template": [
    {"indice": 0, "tipo": "resposta_rapida", "payload": "confirma:41"}
  ]
}
```

Example — **body + button** (`orcamento_disponivel`: one body variable and the portal button at index
`0`):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "orcamento_disponivel",
  "idioma": "pt_BR",
  "variaveis": ["Maria"],
  "botoes_template": [
    {"indice": 0, "tipo": "url", "texto": "tok-portal-9f2"}
  ]
}
```

> **The gateway accepts the three blocks together** — the `header → body → button` order is implemented
> and tested. But if you are writing `botoes_template` with **two items**, check the catalogue first: a
> template with more than one button is rare, and two items against a single button would both land in
> the same place.

A template with only `variaveis` keeps working exactly as before: the body that goes to Meta is byte
for byte the same, and so is its idempotency hash (both are pinned by regression tests). You need
change nothing in what you already send.

### `contatos` — and the field that decides whether the card IS USEFUL (2026-08-20)

A contact card has **one** mandatory field, `name.formatted_name`. But there is an optional field that
changes what the customer can do with the card, and **Meta's documentation does not say so**: it lists
`wa_id` as just another field among others.

| what you send | what appears on the device |
|---|---|
| `phones: [{"phone": "+55 (32) 99999-0000", "type": "WORK"}]` | an **"Invite to WhatsApp"** button, generic photo |
| the same, plus `"wa_id": "5532999990000"` | a **"Message"** button, and the contact's **profile photo** |

*Measured on a device on 2026-08-20, with the two cards identical except for that field.*

🔴 **Without `wa_id`, the card does not serve the main use case.** Passing a salesperson's contact so
the customer can talk to them turns into an invitation to install WhatsApp — the customer **is
already** on WhatsApp, reading your message. Whoever fills in only `phone` thinks they sent a contact
and sent an invitation.

> ℹ️ **The `wa_id` is the number in Meta's canonical format, without `+`, without spaces and without
> punctuation** — `5532999990000`. It is the same value that reaches you in the `de` of a received
> message. The `phone` beside it can be written however you like (it is what the customer sees on the
> card); the `wa_id` is what Meta uses to find the account.

### Reaction and location

The vocabulary of these two fields is **the same as on the inbound side** (`reacao {emoji, alvo}`,
`localizacao {latitude, longitude, nome, endereco}` — see *What you receive*, above): sending and
receiving with different names for the same thing would be the start of two vocabularies.

**`tipo: "reacao"`** — applies an emoji to a message you received earlier.

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "reacao",
  "reacao": { "alvo": "wamid…", "emoji": "👍" } }
```

`alvo` is the `wamid` of the message reacted to (the `wa_message_id` that came in the event). `alvo`
is always **mandatory**. `emoji` is also mandatory **as a key** — but its **value** may be empty, and
the two cases mean different things:

- **`emoji` with a value** — adds (or replaces) the reaction.
- **`emoji: ""`** (empty string, key present) — **removes** the reaction.
- **`emoji` absent** (key not sent) — `400`, a named required-field error. It is **not** treated as a
  removal.

```jsonc
// removes the reaction
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "reacao",
  "reacao": { "alvo": "wamid…", "emoji": "" } }
```

> ⚠️ **Why an empty `emoji` removes but an absent `emoji` does not — the asymmetry with RECEIVING is
> deliberate, not a bug.** When RECEIVING (see *`reacao`, `voz`, ...* above) it is Meta itself that
> assembles the event, and it *omits* the `emoji` key to say "the user removed it". When SENDING, the
> one assembling the request is **a program of yours**, and "I forgot to send the field" is the most
> common programming error there is — an absent key is indistinguishable from carelessness. An empty
> string is different: it is a choice someone typed on purpose. That is why only the empty string
> removes; the absent key stays a `400`.
>
> **The source for the removal is not Meta's documentation — it is an experiment with a device.** The
> official doc (developers.facebook.com/docs/whatsapp/cloud-api/messages/reaction-messages, read on
> 2026-07-26) lists `<EMOJI>` as *"Required"* and describes no removal format. A consumer ran the
> experiment on 2026-07-26 (10:15 -03), with someone watching the device: two sends through the direct
> Graph API, same body, only the `emoji` changing — `"👍"` made the reaction **appear**; `""` made the
> reaction **disappear**. In both cases Meta answered `200` with a **new** `wa_message_id` — if the
> reaction had not disappeared on the second send, the response would have been identical. The `200`
> proves Meta accepted the request, never that the effect happened; the only possible witness was the
> device. If you have a better way to confirm removal that is not looking at the device, it does not
> exist today in this gateway nor in Meta's doc.

**`tipo: "localizacao"`** — shares a point with the recipient.

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "localizacao",
  "localizacao": { "latitude": 37.44, "longitude": -122.16, "nome": "…", "endereco": "…" } }
```

`latitude` and `longitude` are **mandatory**; `nome` and `endereco` are optional. The two field names
in the Graph API (`name`, `address`) are translated by this gateway — you do not use them.

> ⚠️ **`0` is a valid coordinate** (the crossing of the Greenwich meridian with the equator), and it
> DOES go out in the body — it is never omitted for looking "empty". What is refused with `400` is the
> *absence* of `latitude`/`longitude`, not the value `0`.

**`tipo: "pedir_localizacao"`** — shows a text with a button that opens WhatsApp's location-sharing
screen (the Cloud API's `location_request_message`).

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "pedir_localizacao",
  "texto": "Pode compartilhar sua localizacao para a entrega?" }
```

The whole shape is just this — `texto` is the only field, and it becomes `body.text`. **This type has
no header and no footer**: `cabecalho_texto` and `rodape` are a `400` here, the same forbidden-field
rule as the other types that do not use them — the Cloud API documents neither of them for this
object. When the customer replies by tapping the button, the shared location reaches you through the
**same route as always**: the `localizacao` event in the webhook (see *What you receive*, above) — it
is what closes the loop and what makes it worth asking through this type instead of through free text.

**`tipo: "flow"`** — opens a WhatsApp Flow (a native form inside WhatsApp) for the recipient to fill
in (T-154).

🟢 **SHAPE CONFIRMED AGAINST META'S PARSER ON 2026-08-20 (T-156)** — but read carefully what that
covers, because this project confused the two things three times on that day alone. We sent a
`tipo:"flow"` with a **deliberately invented** `flow_id`, on both `acao` branches (`navigate` with
`tela`, and `data_exchange` without). On both, Meta answered:

```
400  codigo_meta 131009
detalhe_meta: Parameter "flow_id" is invalid. Please check if the flow associated to
              this id belongs to your WhatsApp Business Account, and it's in a valid state.
```

That is: it **parsed the entire payload** and only stopped at the single field that was false on
purpose. `flow_message_version`, `flow_token`, `flow_cta`, `flow_action` and `flow_action_payload`
(with `screen` and `data`) **passed through its parser** — if any of them were wrong or misnamed, it
would have complained about that one before reaching `flow_id`.

🔴 **What this does NOT prove: the RENDERING.** The official Flows page
(`developers.facebook.com/docs/whatsapp/flows/...`) assembles the content **on the client**, via
JavaScript, and no Flow was ever published in this WABA — so no screen ever opened on the recipient's
side. "Meta accepted the payload" and "the Flow rendered on the client" are **different** proofs, and
only the first happened. Do not read this section as "proven" without that distinction.

And the **parameters** still come from somewhere else: the structure of the fields below (names,
requiredness, the XOR of `id`/`nome`) came from **BSP documentation (360dialog) and a third-party SDK
(whatsapp-api-js)**, read on 2026-08-20 — Meta did not confirm that source is official, only that the
**combination** we assembled from it passes its parser. These are two things this contract cannot
merge: the provenance of the **parameters** (third-hand, as it always was) and the confirmation of the
**shape** (now real).

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "flow",
  "texto": "Preencha seus dados para agendar",
  "botao_titulo": "Agendar",           // flow_cta — reused from cta_url/lista, max 20 (a THIRD-HAND ceiling, see above)
  "fluxo": {
    "id": "123456789",                 // OR "nome" — never both, and one of the two is required
    "token": "agendamento-4471",       // required: identifies the Flow's answer for you
    "acao": "navigate",                // "navigate" | "data_exchange" — optional, default "navigate"
    "tela": "TELA_INICIAL",            // required when acao is "navigate"
    "dados": {"nome_cliente": "Maria"} // optional
  } }
```

`fluxo.id` and `fluxo.nome` are **mutually exclusive** — send one or the other, never both, and at
least one is mandatory; the `400` names the field both when the two come together and when both are
missing. `fluxo.token` is **mandatory**: it is the value you generate that matches the Flow's answer
(see below) to what you were doing when you opened it — without it the answer comes back and there is
no way to know whose it is. `fluxo.tela` is mandatory **only** when `fluxo.acao` is `"navigate"` (the
default); in `"data_exchange"` it is optional.

The 20-character ceiling of `botao_titulo` (`flow_cta`) here is a **constant of its own**, not shared
with `cta_url`'s (`display_text`) nor with `lista`'s (`action.button`) — they are **three different
fields** of the Cloud API that happen to coincide in value today, and Meta can change any of the three
without changing the others (a rule fixed by T-149, extended here by T-154). **This number is still
third-hand** — T-156's call confirmed that the `flow_cta` *field* passes Meta's parser, not that the
20-character *limit* is right (the value sent in the test never came near the boundary). Only a
bisection measurement, like `cta_url`'s above, corrects this number if it is wrong.

> ⚠️ **This type does NOT accept `cabecalho_texto` or `rodape`.** The refusal is for **LACK OF
> CONFIRMATION**, not because Meta forbids it — the difference matters to whoever reopens the subject
> later: third-party sources disagree about whether a Flow supports a header/footer, and none of them
> was confirmed in this reading. Refusing now is **additive later**, if someone ever confirms Meta
> accepts it; accepting now and being wrong would be a **contract break** later.

> ℹ️ **The Flow's answer arrives through the webhook as an `interactive` of subtype `nfm_reply`, and
> this gateway does NOT enrich it yet.** It arrives with a raw `sub_tipo` (`"interactive"`), without
> `botao_payload`/`botao_texto` filled in — the same treatment any `interactive` subtype the parser
> does not model yet receives today (see *What you receive*, above). There is no enrichment at all: if
> you need the answer's content before the gateway starts modelling it, read the webhook's raw body.

**Executed examples** (deserialized with `json.Unmarshal` into a `Pedido`, validated with `Validar()`
and re-serialized; a test pins that the assembled body has exactly this shape):

Reaction:

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "reacao",
  "reacao": {
    "alvo": "wamid.TESTE001",
    "emoji": "👍"
  }
}
```

becomes, in the body for the Graph API:

```json
{
  "messaging_product": "whatsapp",
  "reaction": {
    "emoji": "👍",
    "message_id": "wamid.TESTE001"
  },
  "recipient_type": "individual",
  "to": "5511999990000",
  "type": "reaction"
}
```

Reaction **removed** (same `alvo`, empty `emoji`):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "reacao",
  "reacao": {
    "alvo": "wamid.TESTE001",
    "emoji": ""
  }
}
```

becomes, in the body for the Graph API — note that the `"emoji"` key **goes out empty, never
omitted**:

```json
{
  "messaging_product": "whatsapp",
  "reaction": {
    "emoji": "",
    "message_id": "wamid.TESTE001"
  },
  "recipient_type": "individual",
  "to": "5511999990000",
  "type": "reaction"
}
```

Location:

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "localizacao",
  "localizacao": {
    "latitude": 37.44221496582,
    "longitude": -122.16165924072,
    "nome": "Cafe de Teste",
    "endereco": "Rua de Teste, 101"
  }
}
```

becomes, in the body for the Graph API:

```json
{
  "location": {
    "address": "Rua de Teste, 101",
    "latitude": 37.44221496582,
    "longitude": -122.16165924072,
    "name": "Cafe de Teste"
  },
  "messaging_product": "whatsapp",
  "recipient_type": "individual",
  "to": "5511999990000",
  "type": "location"
}
```

### Three things the contract refuses on purpose

The three guards below come from real incidents on this network — the cost was already paid in another
system — not from a reading of Meta's documentation. They were not checked against Meta's source; the
label on each says so on purpose, so they do not become "documented by Meta" without having been.

- **`responder_a` in a `template`** → `400`. *Observed in production on this network; not checked in
  Meta's doc:* Meta accepts it and answers `200`, and the quote bubble never renders. There is no error
  anywhere; the only evidence would be your customer seeing a reply floating loose.
- **A reply button together with a link button** → `400`. *Observed in production on this network; not
  checked in Meta's doc:* in the Cloud API the two do not coexist in the same interactive; without this
  guard it would be a `400` from Meta discovered in production.
- **Content in base64** → `400`, with the path in the error. *Observed in production on this network;
  not checked in Meta's doc:* the Cloud API does not accept it; a system on this network discovered
  that with a send that **failed silently**.

### 🔴 What is STABLE in this error body, and what is not (2026-08-20, extended by T-153)

An error body has seven fields, and they **do not carry the same commitment**. Matching on the wrong
field is the kind of coupling that does not fail the day it is written — it fails months later, in a
deploy of ours that broke nothing for anybody else.

| field | commitment |
|---|---|
| `classe` | **STABLE.** It is the closed vocabulary (`retentavel`, `permanente`, `config`, `desconhecido`), and it is what decides whether you retry |
| `codigo_meta` | **STABLE** for as long as Meta keeps it — it is theirs, and we pass it through raw |
| `mensagem` | ❌ **NOT STABLE.** Text for a human. The wording changes without notice and **without a MAJOR bump** |
| `detalhe_meta` | ❌ **NOT STABLE, twice over** — it is a third party's text, raw from Meta, and it may not come at all |
| `subcodigo_meta` | **STABLE** for as long as Meta keeps it — it is their `error.error_subcode`, raw, same commitment as `codigo_meta` |
| `explicacao_meta` | ❌ **NOT STABLE.** It is `error.error_user_msg` (with `error.error_user_title` as a prefix, when it comes) — Meta's human text, it may be reworded or stop coming without notice |
| `rastro_meta` | **OPAQUE and STABLE for as long as the call lasts** — it is the `fbtrace_id`, the ONLY thing in this body that Meta's support accepts for opening a ticket about a specific call, and **it DOES NOT COME BACK after this response**. Do not decide anything by it; **keep it when an error matters to you** |

**Decide by `classe`; when you need granularity, by `codigo_meta` or `subcodigo_meta`. Never by a
sentence — neither `mensagem` nor `explicacao_meta`.**

🔑 **An empty field here is DATA, not a hole — and the distinction has only existed since T-153.**
Measured by `consumer-b` on 2026-08-20: a `503 codigo_meta 2` on template creation came with
`subcodigo_meta` and `explicacao_meta` **empty**, and `rastro_meta` present. That is not the gateway
losing a field: it is Meta not having sent either `error_subcode` or `error_user_msg`.

*Why it is worth recording:* before T-153 the gateway discarded those fields, so "Meta said nothing"
and "we ate the field" were **indistinguishable** from the outside. Now a generic `2` with no subcode
means exactly *Meta failed internally and cannot say what* — and then the only path is the
`rastro_meta`, which is what their support accepts.

*This is not hypothetical: on 2026-08-20 a limit error's message changed from `"campo acima do limite
de caracteres"` to `"campo acima do limite"`, because the old one said "characters" while counting
list items — it told you to fix the wrong thing. It was a correction, it shipped in a MINOR, and it
will happen again whenever a message is wrong. **An error message is meant to be fixed; pinning it in
a contract is choosing to keep the bad text.***

> ℹ️ **If you need to react to a specific case, ask for a CODE or a FIELD — not a sentence.** The
> formulation is consumer `consumer-b`'s (2026-08-20), and it is right: a sentence that became an
> interface is a sentence nobody can improve any more. A request of that kind is welcome and gets a
> quick answer.

### What each error means

```jsonc
{ "erro": { "classe": "retentavel" | "permanente" | "config" | "desconhecido",
            "codigo_meta": 131047, "mensagem": "…", "detalhe_meta": "…" } }
```

**Decide by the `classe`, never by the HTTP status.** The status is a transport hint, not the
contract: the same class comes out under more than one status, because each guard in the sending chain
(authentication, the link to the instance, the request body, idempotency, the call to Meta) returns
the status that makes sense *at that point* — there is no fixed status per class.

🔴 **`detalhe_meta` (T-141, `POST /v1/messages`) is a RAW passthrough of what Meta sends in
`error.error_data.details` — read this before using it.** It only appears (the field is absent from
the JSON when there is no detail, never `""`) when the error came from a direct Meta response to this
send, and:

- **it is a third party's text, not ours** — it may echo a piece of your own payload (a phone number,
  the message's text) the same way Meta's `error_data` can; therefore it **should not be logged or
  shown to a third party without care** on your side, for the same reason this gateway does not keep
  that field in its own transit log;
- **truncated at 500 runes**, with the suffix ` …[truncado]` when that happens;
- **it is not guaranteed on every refusal by Meta.** Whether it still sends `error_data.details`
  through today's path is an open question until measured against production — treat the field's
  absence as "it did not come this time", never as "Meta would not have said it".

| Status | Class | When it happens |
|---|---|---|
| `400` | `permanente` | the body is missing, the body is not JSON, or schema validation fails; **or** Meta refused the request with an error a retry would not solve |
| `400` | `retentavel` | the body did not arrive whole (your connection dropped mid-upload). Same status, different class — that is exactly why you decide by the `classe` |
| `401` | `config` | your `Authorization` is missing or invalid |
| `403` | `config` | the instance requested in the body is not yours |
| `404` | `config` | the requested instance does not exist |
| `409` | `retentavel` | another send of yours with the **same** `Idempotency-Key` is in flight — or a previous attempt with it ended in `desconhecido` and the key was held (see below) |
| `413` | `permanente` | the body exceeded the accepted size limit (**1 MiB** by default — see *Known limits*) |
| `422` | `permanente` | this `Idempotency-Key` has already been used for a **different** request. Changing the key is the fix; repeating will never work |
| `502` | `config` | the configuration stored for that instance is invalid — Meta refused the credential (token/permission), **or** the registered `phone_number_id` has an invalid shape and the request never even went out. It is not the `Authorization` you sent, and resending does not solve it. **You are the one who fixes it**, see below |
| `502` | `desconhecido` | the gateway did not get a usable answer from Meta (transport, the instance's deadline exceeded, or a `2xx` with no id): **it is not known** whether the message went out. See the section below before resending |
| `503` | `retentavel` | the gateway could not talk to its own storage, the instance is paused, **this gateway instance does not hold the pair's leadership** (v0.47.0 — see below), **or** Meta returned an error classified as retryable (5xx, timeout, or throttling — see the note below) |

`retentavel`: re-queue and try again later. `permanente`: **do not try again** — fix the request.
`desconhecido`: **do not resend automatically** — the same key will give `409`, and changing it may
duplicate; read the next section.

> **How Meta's throttling becomes `retentavel`, and the limit of that (T-142):** Meta **does not
> document** which HTTP status the rate-limit error arrives with — the official error-code page lists
> the throttling family with no status column, and the Marketing Messages API shows an error of the
> same shape as `400`. That is why the gateway does not trust the status to recognize throttling: it
> recognizes it by **Meta's error code**, against a list **of ours, conservative**, of codes verified
> in the official documentation. A throttling code Meta invents tomorrow falls into `permanente` until
> somebody adds it to that list — it is not a guarantee of full coverage, it is what is verified today.

🔎 **And `GET /v1/estado` publishes the `lideranca` block (v0.49.0)**, with four fields: `armada`
(there is a guard configured), `estado` (`observado` when armed, `nao_se_aplica` when not), `titular`
(`true`/`false`, and **`null` when disarmed** — a single node won no election) and `motivo` (only when
`titular: false`). ⚠️ **This block is worth less to you than the first version of this section
claimed, and the correction is ours:** traffic arrives through a VIP, and whoever answers on the VIP is
**by construction** the node holding the leadership. So in practice you will almost always see
`titular: true` — the node that is **not** the holder does not hold the VIP and therefore does not
even receive your requests. *The useful reading for you is the short window between the VIP migrating
and the lease being acquired: there you may receive `503 retentavel` and this block explains why.*
**Checking whether the whole pair is protected is the duty of whoever operates the gateway, not
yours** — it requires talking to each node at its own address, which the VIP does not allow. Like
every field on this route, it is **additive**: the format only grows.

> 🔴 **One wrong reading nullifies the whole block, so it is written down here:** asking *"is the
> gateway armed?"* only works **with the gateway up**. If the route does not answer — timeout,
> connection refused, `5xx` — that is **"I could not verify"**, and never `armada: false` nor
> "everything is fine". The two outcomes produce the same absence of an answer, and treating them
> alike turns a safety indicator into silence. *Rule brought by `consumer-b`'s team on 2026-08-19,
> while designing their alarm on top of this field.*

🔵 **The leadership `503` (v0.47.0) — what it is, and why you need do nothing different.** The gateway
is being prepared to run as an **active-passive pair**. In that arrangement, only the instance holding
the leadership may **send**; if the one that served you is not the holder, it answers `503 retentavel`
with a message saying so, **instead of sending the message**.

**Why that protects you rather than getting in your way:** the pair's risk is not one message fewer —
it is the **same message twice** on your customer's phone, with the 250/day quota burning double, if
both instances send. Refusing is the safe side.

**What you do:** exactly what you already do with any `retentavel` — repeat. When you repeat, the
address will already have migrated to whoever holds the leadership, and the send goes out normally.
**Do not change the `Idempotency-Key`**: the message was not sent, so repeating with the SAME key is
the correct behaviour and does not duplicate.

*Today this guard is **disarmed** (the gateway runs on a single node), so this response **does not
happen** — it is documented now so that, the day the pair exists, it does not arrive as a surprise. If
you want to treat it differently from other `503`s, the error's message is recognizable: it says "não
detém a liderança do par". But deciding by the `classe`, as always, remains sufficient.*

🔴 **`config` does not mean "wait for someone to fix it", and that is the most important correction in
this table.** The word is a leftover from when credentials were registered by whoever operates the
gateway. **Today you are the one who registers them**, so `config` splits in two, with different
owners:

| What came | Whose fix it is | What to do |
|---|---|---|
| `502 config` — Meta refused the `token_envio`, or the registered `phone_number_id` has an invalid shape | **yours**: they are values you sent in `POST /v1/cadastro` | fix it in your Meta account and **re-register** the complete set. If the 24 h window has already closed, the `POST` answers `409` and then, yes, you need to ask whoever handed you the slug to reopen it |
| `401 config` — `Authorization` missing or unknown | **yours**: it is the consumer token from item 5 of the package | check the header. If the token was rotated, use the new one |
| `403 config` — the instance is not yours · `404 config` — the instance does not exist | **whoever operates the gateway's**: it is the consumer↔instance link | first check that you did not write **your own** name in `instancia` instead of the slug — it is the most common mistake. If the slug is right, this is the only case in this table you cannot resolve on your own |

*That is: of the four `config` responses in the table above, **two are entirely yours** (`401` and
`502`) and the other two still call for a check of yours before you conclude the problem is ours.
Stopping and waiting without looking at your own configuration is the wrong reaction in most cases.*

For errors born **before** talking to Meta (authentication, the link to the instance, the request
body, idempotency), the classification is decided by this gateway, not by Meta, and `codigo_meta`
comes as `0`. `desconhecido` is also born on this side (`codigo_meta` `0`): by definition, Meta
answered nothing classifiable. Only when the error **comes** from Meta (the `502 config` row, the part
of the `503` that is theirs, and the part of the `400 permanente`) is the class derived from the HTTP
status Meta returned — and `codigo_meta` travels along for anyone with a rule of their own; we decide
nothing by it, because inventing meaning for an unverified code would be worse than not sending it.

### Did it fail: does your key become valid again, or not?

It depends on whether the gateway **knows** if the message was created on Meta's side, and the error's
`classe` tells you which of the two worlds you are in. It is not the same question as "did it fail?".

- **Meta answered** that it did not work — `classe` `permanente`, `config` or `retentavel` coming from
  it (`400`, `502 config`, the part of the `503` that is theirs): the message was **not** created. The
  key **becomes valid again** immediately, and a retry with it really does send. This holds even for
  Meta's `5xx`: it answered, even if with an error.
- **Meta answered nothing usable** — `classe` `desconhecido` (`502`): the transport dropped, the
  **instance's** deadline was exceeded, or a `2xx` came **without an id**. The message **may have gone
  out**. The key is **held**, and a new send with it receives `409` until the TTL expires.

The hold is deliberate and you need to know how to live with it: releasing the key in that case would
make your legitimate retry create a **second** message on a real customer's phone — the very damage
the whole of idempotency exists to prevent. A `409` that requires someone looking costs less than a
duplicate nobody sees.

#### 🔴 What to do with `desconhecido` (or with a `409` that will not clear) — the whole procedure

**Do not swap the key for a new one just to unstick it: that resends blindly**, and this is
precisely the case where the message may already be on someone's phone. The key is held until the
idempotency TTL expires (**default 72 h**), and every send with it receives `409` during that period.
This is the worst dead end in this contract, so here is the way out, step by step, **without depending
on anyone**:

**Step 1 — look at your own webhook, which is where the answer is.** If Meta created the message, it
sends a `status` event with `status: "sent"` to your `callback_url`, typically within seconds. Look,
in your base of received events, for a `status` with:

- `para_canonico` equal to that send's destination, **and**
- `timestamp` (or your own moment of receipt) inside the window of the send that came back
  `desconhecido`.

**Found it → the message WENT OUT.** Record that event's `wa_message_id` as your send's id (it is the
same one the `200` would have returned) and **do not repeat**. The `409` stops bothering you when the
record expires; until then, it is right.

**Step 2 — did not find it after a few minutes?** Then the message probably was not created — but
"probably" is the best there is here, and the gateway will not pretend otherwise. The decision is
yours and depends on what costs more in your business: **a duplicated message** or **a message that
did not go out**. If you choose to resend, do it with a new key and **know that you are accepting the
risk of a duplicate** — it is not the gateway unsticking things for you.

> ⚠️ **Step 1 requires a registered `callback_url`.** On an **outbound-only** instance (empty
> callback) there is no signal at all to observe, and you land straight on step 2. If that ambiguity
> is expensive for you, register a `callback_url` — even if it only records the `status` events and
> discards the rest: it is what turns "I don't know" into "I know".

> ℹ️ **`GET /v1/estado` does NOT answer this question, and it is worth saying so you do not waste time
> there.** The `enviadas` counter only goes up on a successful send; a `desconhecido` outcome counts
> in `falhas_de_envio` even if the message went out. The counters measure what the **gateway** knew,
> not what Meta did.

On the operator's side, this case writes an `ALARME` with the `(consumer, key)` pair in the service
log. That is an instrument of **theirs**, not a promise that somebody will come looking for you.

**And the deadline that decides whether you even get to see the answer is the instance's**
(`timeout_ms`, stored per number, default 5000 ms), not your HTTP client's. If you give up on the
request before the gateway finishes, **the send continues** — your cancellation does not abort the
call to Meta nor hold the key, and you are left not knowing the outcome of a send that happened.
**Give your client a timeout comfortably above 5 s** (30 s is comfortable) and this entire class of
ambiguity disappears.

---

## Mark a received message as READ — `POST /v1/leituras` (2026-07-28)

**`POST https://zapgw.exemplo.com.br:8443/v1/leituras`** · `Authorization: Bearer <your token>`
· **no** `Idempotency-Key`

Knowing that the customer read what **you** sent already worked: it is the `status: "read"` that
arrives in the webhook. Telling the customer that **you** read **their** message did not exist — they
sent it and saw two grey ticks forever, even with the operator already working on the order. That is
what produces the *"hi, did it arrive?"*.

**The root cause is not a gateway regression, it is a consequence of the migration, and every consumer
coming from Baileys will hit it:** there, the read marker went out **on its own** when the message was
received. In the Cloud API it requires an explicit call, and without it nobody marks anything.

Same rules as `/v1/messages`: a **LAN** route, on the internal entrypoint (`:8443`), and **only the
instances linked to you answer** — `403` for the others, with the same error body. **There is no new
authorization model:** it is the same consumer↔instance link.

```jsonc
// request
{ "instancia": "lojinha",
  "wamid": "wamid…",     // the wamid of the message YOU RECEIVED
  "digitando": true }    // OPTIONAL — turns on the "typing…" indicator in the same call

// response 200
{ "ok": true }
```

**`digitando` is optional and by default absent/`false`.** ⚠️ **Two restrictions you will discover the
hard way if you do not read them here:** (a) it requires the `wamid` of a message **you received** —
there is no loose "typing", outside a reply to something; (b) the indicator **falls on its own after
25 seconds**, or when you reply, whichever comes first — there is no way to keep it on beyond that,
and anyone expecting it to stay lit will think the gateway failed.

### Why `digitando` is a field here, and not a `/v1/digitando` route

The Cloud API has **no endpoint of its own** for the "typing…" indicator: it merges the two into the
**same** `POST` as the read receipt, only adding `typing_indicator: {"type":"text"}` to the usual body
(source: `developers.facebook.com/docs/whatsapp/cloud-api/typing-indicators`, read 2026-08-20). A
separate route would make the gateway emit **two** `POST`s for what Meta does in one, and would force
you to choose between "mark as read" and "mark as read and typing" when that difference does not exist
on the other side.

**The response has no `wa_message_id`, and that is deliberate:** marking as read **creates no
message**. Inventing an id to "keep the shape" of a send would be lying in the contract to save a line
of documentation.

### Why it is a route of its own, and not an eighth `tipo` of `POST /v1/messages`

The seven send types have `para`, have content and return a `wa_message_id`. Marking as read has
**none of the three**: the target is a *message* and not a person, there is no body, and nothing is
born. Stuffing this into the send envelope would turn three contract fields into *"optional depending
on the type"* — the same ambiguity that already cost dearly in the `botoes`/`botoes_template` pair.
*(The argument is a consumer's, and was adopted because it is right.)*

### Repeating is safe — and that is why there is **no** `Idempotency-Key` here

**Marking the same message twice is harmless:** no message is born, there is no charge, and the final
state is the same. The idempotency key exists in `/v1/messages` because there a retry becomes **two
messages on your customer's phone** — here there is no possible duplicate to prevent.

So let it be written, so nobody builds the control "just in case" six months from now: **you do not
need to keep "already marked" state.** The operator opens the same conversation ten times a day; send
all ten. An "already marked" control on your side would only cost (a table, a held key, a `409` to
unstick) and would buy no defect that exists.

### One `wamid` per call — there is no list, and the reason is not laziness

**The argument is partial failure.** If the route accepted a list and 5 out of 13 failed, the gateway
would have to invent a response for "partial success", and **every** possible response is bad: a
`200` lying, a `500` telling you to repeat what already worked, or a body with a per-item result —
which is a new API inside the route. With one target per call, **partial failure does not exist as a
concept**: each call is a fact.

Accepting a list later is additive; removing the list later would be a break — that is why the design
starts narrow.

**And the loop you will write is short, not long:** by the guarantee in the next section, marking a
conversation's **most recent** message marks its earlier ones too. So the real volume is *one call per
open conversation*, not one per message — it is the difference between 1 and 13 in the case measured
just below.

### What Meta does with the conversation's EARLIER messages

**Marking one message as read also marks the earlier ones in that conversation.** This is a statement
from the official documentation, read on 2026-07-28 on the two pages describing the call:

> *"When you mark a message as read, the API also marks earlier messages in the conversation as
> read."*
> — `developers.facebook.com/docs/whatsapp/cloud-api/guides/mark-message-as-read` and
> `developers.facebook.com/documentation/business-messaging/whatsapp/messages/mark-message-as-read`

**The practical consequence is big for you:** when opening a conversation with several unread
messages, **one call with the `wamid` of the MOST RECENT one is enough** — you do not need to iterate.
A consumer measured that 47% of the blocks of consecutive inbound messages have more than one message
(the largest had 13), and it is exactly that case the guarantee above solves with a single call.

**Two honest caveats about the reach of that reading:**

- both pages speak of *"the conversation"* without distinguishing an individual conversation from a
  **group** — the behaviour in a group **was not checked**, and this gateway does not assert it;
- the same source says *"Mark incoming messages as read within 30 days of receipt"*. A message older
  than that tends to be refused (`400 permanente`), and there is nothing to do beyond giving up on
  that marking.

### Errors

The same error body and the same taxonomy as sending — **decide by the `classe`, never by the
status**:

| Status | Class | When it happens |
|---|---|---|
| `400` | `permanente` | `instancia` or `wamid` is missing, the body is not JSON; **or** Meta refused the `wamid` (it returns `codigo_meta` `131009` for *"Parameter value is not valid"*, the case of an invalid or too-old wamid) |
| `400` | `retentavel` | the body did not arrive whole (your connection dropped midway) |
| `401` | `config` | your `Authorization` is missing or invalid |
| `403` | `config` | the requested instance is not yours |
| `404` | `config` | the requested instance does not exist |
| `413` | `permanente` | the body exceeded the accepted size limit (**1 MiB** by default — see *Known limits*) |
| `502` | `config` | the credential the **gateway** keeps for that instance was refused by Meta, or the registered `phone_number_id` is invalid. It is not your token; it needs an admin |
| `502` | `desconhecido` | the gateway got no usable answer from Meta (transport, the instance's deadline exceeded) |
| `503` | `retentavel` | the instance is paused, the gateway did not talk to its own storage, **or** Meta returned a retryable error (5xx, timeout, or throttling recognized by Meta's **code** — not by the status; see the note in *POST /v1/messages → Errors*) |

⚠️ **The `desconhecido` (`502`) here says the OPPOSITE of what it says when sending, and it is the
difference that matters most on this page.** In `/v1/messages`, "I do not know whether Meta created
the message" means *do not resend* — a blind retry would duplicate a real message. Here **there is no
possible duplicate**: if the marking may not have happened, **repeat**. The error message itself says
so, in those words. Applying the sending rule here would leave the conversation on two grey ticks out
of fear of damage that does not exist.

### This route's numbers appear in `GET /v1/estado`, with keys of their OWN

Two new keys in the vocabulary: **`leituras_marcadas`** and **`falhas_de_leitura`** (a failure to
*mark* a read, never "a failure to read something"). They arrive on their own in your reading of
`contadores`, by the promise in the section *A promise of ours about the vocabulary*.

🔴 **They do NOT go into `enviadas`, and that is a guarantee, not an implementation detail.** Marking
as read produces no conversation and Meta does not charge for it. If that count fell into `enviadas`,
your cost projection (volume × rate-card price) would start including ten "sends" for each
conversation the operator opened ten times — **an inflated number with the face of a measurement**.
Anyone wanting a total adds the columns on screen; anyone who only had the total could not separate
them back out.

---

## Block and unblock users — `POST /v1/bloqueios`, `DELETE /v1/bloqueios`, `GET /v1/bloqueios` (2026-08-20)

**`POST/DELETE/GET https://zapgw.exemplo.com.br:8443/v1/bloqueios`** ·
`Authorization: Bearer <your token>`

The Cloud API has an endpoint of its own for your business to stop RECEIVING from a number — this is
the route that reaches it. Same rules as the other instance routes: a **LAN** route, and only the
instances linked to you answer (`403` for the others). WhatsApp-only — see *Routes that refuse with
`400` on an Instagram instance*, further below.

### Block — `POST /v1/bloqueios`

```jsonc
// request
{ "instancia": "lojinha",
  "telefones": ["5511999990000", "5511999990001"] }   // up to 1,000 per call

// response 200
{ "instancia": "lojinha",
  "operacao": "bloquear",
  "processados": [ {"telefone": "5511999990000", "wa_id": "5511999990000"} ],
  "falhas": [
    { "telefone": "5511999990001", "wa_id": "5511999990001",
      "codigo_meta": 139001, "mensagem": "…", "detalhe_meta": "…" }
  ] }
```

🔴 **The restriction that changes the design, spelled out:** you can only block someone who sent you a
message in the **LAST 24 HOURS**. Blocking is **REACTIVE**, never preventive — there is no
pre-blocking a number before it writes. It is also not possible to block another business account.
**Both restrictions are META'S, not the gateway's**: it does not check the window on its own — the one
who decides, PER NUMBER, is Meta, and the result arrives in `falhas[]`, in the format of the next
paragraph.

🔴 **PARTIAL success is not a rare case on this route, and the body is designed for it:** Meta answers
`200` on the ENVELOPE and reports errors PER NUMBER inside it — 1,000 numbers can become 998 blocked
and 2 refused, all under the same `200`. That is why this route **never** returns a plain success:
every call comes back with `processados` **and** `falhas` together, even when one of the two is empty.
**Check `falhas` on every call** — the `200` status alone does not prove all the numbers were
processed.

### Unblock — `DELETE /v1/bloqueios`

**SAME body** as the `POST`, **SAME response format** — only the method changes, and `"operacao"`
comes as `"desbloquear"`.

### List who is blocked — `GET /v1/bloqueios`

```
GET /v1/bloqueios?instancia=lojinha&limit=100&after=<cursor>&before=<cursor>
```

`limit`, `after` and `before` are OPTIONAL and passed straight through to Meta's pagination.

```jsonc
// response 200
{ "instancia": "lojinha",
  "total": 1,
  "bloqueados": [ {"wa_id": "5511999990000"} ],
  "cursor_antes": "…",
  "cursor_depois": "…" }
```

Meta does not return the phone number in the clear in this listing — only the `wa_id`.
`cursor_antes`/`cursor_depois` only appear when Meta sent them; use them in the next call's
`before`/`after` to paginate.

🔑 **What this route is really for: to DISAGREE with you.** Measured by `consumer-b` on 2026-08-20,
and the lesson is theirs: their database said "blocked" and this `GET` answered
`{"total":0,"bloqueados":[]}`. **Two sources disagreeing within fifteen seconds** turned an *"I think
it didn't work"* into a root cause — it was an `Enter` in their form submitting without a `submitter`,
and therefore without the field that chose between blocking here and blocking at Meta.

*Why this belongs in the contract and not only in the changelog:* they had catalogued this route as
*"auditing, not rendering"* — useful, but low priority. **Its value is not listing; it is being a
second source that can contradict yours.** Blocking is exactly the case where success and failure have
the same symptom: silence. Nobody investigates someone who stopped writing.

### Limits

- **1,000 phone numbers per `POST`/`DELETE` call** — above that the gateway refuses at the INPUT
  (`400 permanente`), saying how many came and the maximum accepted. Meta is not even called.
- **64,000 blocked users in total, per account** — a META limit, not mirrored here: the one who knows
  the total is Meta, and the error arrives alongside the number that exceeded it (`falhas[]`) or, if
  the whole call is refused for that, in `erro.detalhe_meta`.
  ⚠️ **We have never seen this error happen** (checked on 2026-08-20: no occurrence in our code, tests
  or records). So **we do not know its `codigo_meta`** — and we are not going to invent one. Whoever
  hits it first, send the code through this channel and it goes in here.
- A phone number is **CANONIZED** as when sending — sending it without the ninth digit is not an
  error, but neither does it block the number you think: the gateway inserts the digit before talking
  to Meta.

### Errors

| Status | Class | When it happens |
|---|---|---|
| `400` | `permanente` | `instancia` missing, `telefones` absent/empty, more than 1,000 phone numbers, or the body is not JSON |
| `400` | `retentavel` | the body did not arrive whole (your connection dropped midway) |
| `401` | `config` | your `Authorization` is missing or invalid |
| `403` | `config` | the requested instance is not yours |
| `404` | `config` | the requested instance does not exist |
| `503` | `retentavel` | the instance is paused, or the gateway did not talk to its own storage |
| `502` | `config` | the credential the **gateway** keeps for that instance was refused by Meta, or the registered `phone_number_id` is invalid |
| `502` | `desconhecido` | the gateway got no usable answer from Meta for the **WHOLE CALL** — no number was processed; repeating is safe (blocking/unblocking has no side effect by itself) |

⚠️ **A `200` is never an "error" on this route — even if ALL the numbers ended up in `falhas[]`.** The
envelope answered; the per-number verdict is in the body. The table above describes only the failure
of the WHOLE CALL (no number processed) — do not confuse it with the partial success of the previous
paragraph.

---

## Knowing whether a channel is still fit to send

**`GET /v1/instances/{slug}/health`** · `Authorization: Bearer <your token>`

Same rules as `/v1/messages`: a **LAN** route, on the internal entrypoint (`:8443`), and only the
instances linked to you answer — `403` for the others, without the gateway even talking to Meta.

It exists because `/v1/health` **does not answer this question**: that `200` says the gateway's
process is up, and it comes out identical with **all** the tokens revoked. A token revoked by the
customer at Meta is the failure that dies in silence — nothing changes in the gateway, and the first
to know would be your end customer who did not receive anything. This endpoint asks Meta, with that
instance's token, whether it still accepts it (`GET /{phone_number_id}`, which creates nothing on the
other side).

> **`GET /v1/health` answers a different and simpler question: which binary is this?**
> The **shape** is `{"ok": true, "versao": "<the version of the binary that is live>"}` — for example
> `{"ok":true,"versao":"0.30.0"}`. **The number in the example is illustrative and ages**; what the
> contract guarantees are the two keys, not the value. `ok` still exists and is still always `true`
> (it is this gateway's public guarantee; the format only grows, so anyone who only reads `ok` does
> not break). `versao` is the binary's identity, injected at **build** time (never read from disk in
> production); without injection the value is `"desenvolvimento"`, never a plausible number. Before
> that, the only way to know what was running was to compare the binary's sha256 or to believe the tag
> — and the tag can diverge from what is live (it happened on 2026-07-25, see the BREAKING CHANGE note
> further below).

Fit to send → `200`:

```jsonc
{ "ok": true,
  "numero_exibido": "5511999990000",   // comes from the gateway's registration, not from Meta's response
  "verificado_em": "2026-07-25T12:00:00Z" }
```

Any other outcome → **`503`**, with the same error body as sending
(`{"erro":{"classe":…,"codigo_meta":…,"mensagem":…}}`). The status is always `503` on purpose: a probe
answers **one** question — is this channel fit to send right now? — and forcing your monitor to learn
the whole status table just to decide "red or green" would turn a signal into an interpretation. **It
is the `classe` that says what to do:** `config` (token refused, invalid `phone_number_id`) is *call a
human, this does not fix itself*; `retentavel` is *wait* (instance paused, `5xx`, timeout, or Meta
throttling recognized by the **code** — not by the status; see the note in *POST /v1/messages →
Errors*); `desconhecido` is *the gateway could not talk to Meta, so it does not know*.

**There is no cache, and that is the feature.** Every call talks to Meta. A probe with a cache lies for
exactly the duration of the cache — which is the window in which it matters most — and the failure it
exists to report would go back to being silent. In exchange, **the frequency is yours**: each call
costs one trip to the Graph API on that instance's account, so do not put it in a tight loop. The
`verificado_em` travels precisely so that nobody in between can re-present an old answer as new.

What it does **not** prove: that the message reaches somebody's phone. An accepted token is not a
delivered message — for that there is **`POST /v1/fumaca`**, which sends a real message to a number
you choose.

---

## Two different questions — and the second one has its OWN source, which survives your outage (2026-08-06, updated 2026-08-07)

When a consumer wants to "know the status", it is almost always **two** questions, and they have
different sources. Confusing the two is how an entire day of work on this project was spent.

| Your question | Where to answer it | Why |
|---|---|---|
| **"how is the gateway?"** — how many messages, is the token valid, when did the last one arrive | `GET /v1/estado`, just below | the gateway knows that, and knows it honestly |
| **"are you reaching me?"** | an **external source**, in this section — and a convenience mirror of it in `GET /v1/estado` (`alcance_externo`, 2026-08-07) | 🔴 the gateway **cannot measure this on its own**; it can only REPEAT what a probe running outside our network has already measured |

🔴 **THE MIRROR DOES NOT REPLACE THE EXTERNAL SOURCE, and that is a decision, not a gap.** The case
where the probe matters most is a **silent gateway** — and that is exactly when asking the gateway
returns nothing. A status that shares a failure domain with what it monitors is not a status. Use
`alcance_externo` when `GET /v1/estado` is already on your screen (it avoids a second call); use this
section's URL when you suspect the whole gateway is down — it is the only one of the two that survives
our outage.

### Why the gateway cannot MEASURE this on its own

**A request that does not arrive leaves no trace.** If the public path goes down, the process stays
alive, `GET /v1/health` stays `200`, the counters simply stop rising and nothing in the journal records
a delivery that never happened. It was measured on 2026-08-06: the link went down, and **the four
internal instruments stayed green throughout the entire outage**.

And there is an even simpler reason: **any answer served by the gateway is unavailable exactly when the
answer would be "no".** An `alcancavel: true` computed from inside would be true whenever you managed
to read it — which makes it no information at all. That is why `alcance_externo` is never a measurement
of its own: it is the gateway **asking the same external probe you could ask directly** and returning
what it answered, by the same discipline of "nobody talks to Meta directly" applied here to a third
party that is not Meta — an explicit request from the owner, so that you only need to talk to the
gateway when it is up.

### The external source

A probe runs **outside our network**, every 5 minutes, and does exactly what Meta does: a `GET` on the
public inbound path, checking **the response body**, not just the status. Its verdict is public and
**requires no authentication**:

```
GET https://healthchecks.io/b/2/c6f700a6-1982-408d-a2c0-d2f959c38da6.json
    → {"status": "up", "total": 1, "grace": 0, "down": 0}      (measured on 2026-08-13 17:50 -03)
```

**Read `status`, and ONLY it.** `total`, `grace` and `down` are the badge's internal counters (how many
checks it covers, how many are in grace, how many are down) — this badge covers **1** check, and
deriving anything from them is coupling yourself to a third party's implementation detail. *This line
exists because the example here once showed only `{"status": "up"}`, abbreviated — and an abbreviated
example that looks complete is how somebody comes to write a parser by exact equality.*

| Value | What it means |
|---|---|
| `up` | in the last few minutes, a machine outside our network asked the public path **and the gateway answered** |
| `down` | **either** the path went down, **or** the probe itself stopped measuring |
| any other literal | **"I could not verify"** — never green, never an outage — **and tell us**: the vocabulary grew and this contract needs to change |

**`status` is an open string, not an enum.** We have only measured `up` and `down` to date; treating the
field as a closed vocabulary would return something plausible for a value nobody checked — the same
reason `alcance_externo.veredito` travels **untranslated**.

⚠️ **`GET`, and only on this URL.** healthchecks has a second address, the **ping** one (a write),
which is what the probe uses to say "I measured and it is up". It is secret, lives only here, and
**cannot be derived from the badge's UUID**. A `POST` from outside at that address would falsify the
measurement: the monitor would go green without anyone having measured anything.

🔴 **The external source does NOT have a third state, and will not have one.** "I could not verify" is
the outcome of YOUR READING — I could not reach it, HTTP ≠ 200, a body that is not JSON, JSON without
`status`, an unknown literal — **never** a verdict it returns. Publishing "I could not measure" as a
state the consumer treats as not-an-outage would turn the **probe's death** into a non-alarm, which is
the very hole it exists to close. It is the same treatment the gateway gives this URL
(`internal/outbound/sonda_externa.go:286-316`): none of those failures invents a verdict.

**The body carries no timestamp** — you cannot compute age from it, and you do not need to: stale data
here does not stay silently green, **it turns into `down` on its own** after the period plus the grace.
It is the difference between a dead-man switch and a cache: the cache serves the last green forever,
this one rots to red.

🔴 **`down` does NOT distinguish the two causes, and that is deliberate.** The rule is *"no positive
signal, assume it is stopped"* — because a monitor that dies in silence is indistinguishable from
"everything is fine", and that is exactly the defect this probe exists to close. **Both causes require
a human; neither of them is "everything is fine".**

⚠️ **Three limits, so you do not draw one conclusion too many:**

- **It is per GATEWAY, not per instance.** It says the inbound path is up — it says nothing about
  *your* instance, your token or your quota. That is `GET /v1/estado`.
- **`down` takes up to ~20 min to appear** (a 5 min period plus 15 of grace). It is not for detecting
  hiccups of seconds, and it does not need to be: Meta re-queues for **36 h**.
- **It does not replace your own silence detection.** You know what your normal volume is; we do not.

## Read this instance's numbers — `GET /v1/estado` (2026-07-28)

**`GET /v1/estado?instancia={slug}`** · `Authorization: Bearer <your token>`
**Optional parameter:** `&serie_dias={1..90}` — the size of the daily series (default 7); see the
section *`serie_diaria` + `?serie_dias=`*, below.

Same rules as `/v1/messages`: a **LAN** route, on the internal entrypoint (`:8443`), and **only the
instances linked to you answer** — `403` for the others, with the same error body as sending. There is
no new authorization model here: it is the same consumer↔instance link.

**It exists for you to ALARM, not for you to draw.** The gateway has no dashboard and will not have
one; the number it promises comes out here, and whoever draws is whoever already has a web front end.
The real gain: you already have a warning system — with these fields,
`alarme_perda_definitiva.ultimos_7_dias > 0` becomes an automatic alert **in a system that already
knows how to alert**, instead of a number only someone with server access can see.

**These are the SAME numbers whoever operates the gateway sees on their screen**, read from the same
function at the same instant — not a second count that agrees by coincidence. The `contadores` keys
come from the gateway's closed vocabulary; when a new key is born, it appears here **on its own**,
without a contract release. Treat an unknown key as a number, never as an error.

**The read NEVER talks to Meta.** Call it as often as your dashboard likes: everything here comes from
the database and from a cache the gateway updates at its own pace. A consequence worth knowing: Meta
being down does **not** bring this route down — it shows up in `token_meta`, which is where it should.

### Executed example

Captured by running the real handler (not typed): an active instance, 13 received and 13 delivered in
the week (4 of them today), 2 sent, 1 send failure, 3 read receipts marked, the token's verdict
measured at the instant of capture and the callback's certificate observed 37 minutes earlier. `versao`
here is the value the suite injects; in production it is the **same** value as `GET /v1/health`. The
series were shortened with `…` **only in the paste**, to fit — the response always carries the whole
window, and every day with every key.

*(Recaptured on 2026-07-29, first when the `carimbos_desde` and `serie_7_dias[].dia_utc` fields went
in, again when the `leituras_marcadas` and `falhas_de_leitura` keys went in, and again when
`serie_diaria` went in — the VALUES come from the handler, never typed.
**One exception, written so nobody confuses it with the rest:** the seven `cobranca_*` keys **were not
recaptured** — they went into this paste at zero, because the captured scenario had no `sent` with a
charge and zero is exactly what the guarantee above produces in that case (every key of the vocabulary
appears, even with no event). Zero here is the guaranteed filling, not an invented number.
**`carimbos_desde` appears here equal to `gerado_em` because this capture's instance had just been
created**; on an instance that already existed it is the instant the migration ran, and on one created
last month it is its birth.
**SECOND EXCEPTION (T-098, 2026-07-30):** the `token_instagram` block was added BY HAND to this paste —
the original scenario is WhatsApp and did not have that field when it was captured. The SEVEN values
(`nao_se_aplica` and the six `null`) are exactly what
`TestGETStateWhatsappInstagramTokenIsNotApplicableInTheJSON`
(`internal/outbound/estado_instagram_test.go`) proves against the real handler — the guarantee is
mechanical, only the paste here is manual.
**THIRD EXCEPTION (T-107, 2026-07-30):** the `tipo` and `ig_id` fields were also added BY HAND — the
original scenario predates both. The values (`"whatsapp"` and `"nao_se_aplica"`) are exactly what
`TestGETStateWhatsappExposesTypeAndIgIDAsNotApplicableInTheJSON`
(`internal/outbound/estado_instagram_test.go`) proves against the real handler.)*

```jsonc
{
  "instancia": "lojinha",
  "tipo": "whatsapp",
  "ig_id": "nao_se_aplica",
  "estado": "ativa",
  "pausada": false,
  "versao": "9.9.9-teste",
  "gerado_em": "2026-07-29T00:00:02Z",
  "carimbos_desde": "2026-07-29T00:00:02Z",
  "contadores": {
    "alarme_perda_definitiva":   { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_ausente":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_authentication":   { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_cobravel":         { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_marketing":        { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_outra":            { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_service":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_utility":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "conta_descartada":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "entregues":                 { "hoje": 4, "ultimos_7_dias": 13, "ultimo_em": "2026-07-29T00:00:02Z" },
    "enviadas":                  { "hoje": 2, "ultimos_7_dias": 2,  "ultimo_em": "2026-07-29T00:00:02Z" },
    "falhas_de_envio":           { "hoje": 1, "ultimos_7_dias": 1,  "ultimo_em": "2026-07-29T00:00:02Z" },
    "falhas_de_leitura":         { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "leituras_marcadas":         { "hoje": 3, "ultimos_7_dias": 3,  "ultimo_em": "2026-07-29T00:00:02Z" },
    "numero_descartado":         { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "recebidas":                 { "hoje": 4, "ultimos_7_dias": 13, "ultimo_em": "2026-07-29T00:00:02Z" },
    "recusadas_pelo_consumidor": { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null }
  },
  "serie_7_dias": [
    { "dia": "2026-07-23", "dia_utc": "2026-07-23", "contadores": { "alarme_perda_definitiva": 0, /* the 7 cobranca_* keys come here, all 0 in this capture — omitted only in this paste */ "conta_descartada": 0, "entregues": 0, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 0, "recusadas_pelo_consumidor": 0 } },
    // … 2026-07-24 to 2026-07-27, all zeroed in this capture …
    { "dia": "2026-07-28", "dia_utc": "2026-07-28", "contadores": { "alarme_perda_definitiva": 0, /* the 7 cobranca_* keys come here, all 0 in this capture — omitted only in this paste */ "conta_descartada": 0, "entregues": 9, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 9, "recusadas_pelo_consumidor": 0 } },
    { "dia": "2026-07-29", "dia_utc": "2026-07-29", "contadores": { "alarme_perda_definitiva": 0, /* the 7 cobranca_* keys come here, all 0 in this capture — omitted only in this paste */ "conta_descartada": 0, "entregues": 4, "enviadas": 2, "falhas_de_envio": 1, "falhas_de_leitura": 0, "leituras_marcadas": 3, "numero_descartado": 0, "recebidas": 4, "recusadas_pelo_consumidor": 0 } }
  ],
  // Without `?serie_dias=`, this is the SAME 7-day window as above, day for day.
  // With `?serie_dias=30`, it has 30 entries and `serie_7_dias` still has 7.
  "serie_diaria": [
    { "dia": "2026-07-23", "dia_utc": "2026-07-23", "contadores": { "alarme_perda_definitiva": 0, /* the 7 cobranca_* keys come here, all 0 in this capture — omitted only in this paste */ "conta_descartada": 0, "entregues": 0, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 0, "recusadas_pelo_consumidor": 0 } },
    // … 2026-07-24 to 2026-07-27, all zeroed in this capture …
    { "dia": "2026-07-28", "dia_utc": "2026-07-28", "contadores": { "alarme_perda_definitiva": 0, /* the 7 cobranca_* keys come here, all 0 in this capture — omitted only in this paste */ "conta_descartada": 0, "entregues": 9, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 9, "recusadas_pelo_consumidor": 0 } },
    { "dia": "2026-07-29", "dia_utc": "2026-07-29", "contadores": { "alarme_perda_definitiva": 0, /* the 7 cobranca_* keys come here, all 0 in this capture — omitted only in this paste */ "conta_descartada": 0, "entregues": 4, "enviadas": 2, "falhas_de_envio": 1, "falhas_de_leitura": 0, "leituras_marcadas": 3, "numero_descartado": 0, "recebidas": 4, "recusadas_pelo_consumidor": 0 } }
  ],
  "token_meta": {
    "veredito": "ok",
    "medido_em": "2026-07-29T00:00:02Z",
    "conferido_em": "2026-07-29T00:00:02Z",
    "checagem_falhando_desde": null
  },
  "certificado_do_callback": {
    "estado": "observado",
    "expira_em": "2026-10-21T00:00:02Z",
    "observado_em": "2026-07-28T23:23:02Z"
  },
  "numero_na_meta": {
    "qualidade": {
      "estado": "observado",
      "valor": "GREEN",
      "observado_em": "2026-07-29T00:00:02Z",
      "fonte": "medicao"
    },
    "limite_de_mensagens": {
      "estado": "observado",
      "valor": "TIER_1K",
      "observado_em": "2026-07-29T00:00:02Z",
      "fonte": "medicao"
    },
    "conferido_em": "2026-07-29T00:00:02Z"
  },
  "token_instagram": {
    "veredito": "nao_se_aplica",
    "definido_em": null,
    "expira_em": null,
    "dias_restantes": null,
    "renovado_em": null,
    "falhando_desde": null,
    "instrucao": null
  },
  "entrada": {
    "via": "tunel",
    "conector": {
      "estado": "observado",
      "conexoes_prontas": 4,
      "medido_em": "2026-07-29T00:00:02Z",
      "falhando_desde": null
    },
    "ultimo_webhook_em": "2026-07-29T00:00:02Z"
  }
}
```

*(`token_instagram` comes out as `nao_se_aplica` in this capture because `lojinha` is WhatsApp — see
the block's own section, further below, for the example on an Instagram instance.)*

*(**FOURTH EXCEPTION (T-120, 2026-08-06):** the `entrada` block was added BY HAND to this paste — the
original scenario predates it. The shape and the states are exactly the ones
`internal/outbound/entrada_test.go` proves against the real handler; the values shown are those of an
installation that comes in through a tunnel with the connector answering.)*

### `ultimo_em` — the field that answers what the counter does not

**A stopped counter is ambiguous between "it failed" and "nobody wrote" — both are the same number.**
That cost dearly on 2026-07-28, 11:07: delivery stopped and `recebidas` stayed stuck at 4. The outage
only came to light because a log monitor had been armed by hand that morning.

The timestamp undoes the ambiguity because it **ages**. With it, your alarm rule is
*"`entregues.ultimo_em` is more than N minutes old"* — and it works **without you knowing anything
about our normal volume**, which is the only rule that works for someone who does not know the other
side's traffic.

- It is the instant of the **last** event for that key, in UTC, RFC3339;
- `null` means **it never happened** within the counters' retention (90 days by default);
- it is **not** cut off by the 7-day window: an event from 20 days ago appears with its own date.
  Cutting it off would make "old" and "never" the same thing, which is the defect it exists to cure.

The four that matter most: `recebidas.ultimo_em`, `entregues.ultimo_em`, `enviadas.ultimo_em` and
`falhas_de_envio.ultimo_em`.

### `carimbos_desde` — since when this instance timestamps (2026-07-28)

**`ultimo_em: null` still hides TWO states**, and the rule above describes only one of them. What is
left is *"it never happened"* (normal) and *"it happened **before** the timestamp was recorded"* —
because the timestamp was born in `v0.23.0`, and what came before left no instant at all. For an alarm
these are different things: in the first case there is nothing to know; in the second there is a blind
spot.

`carimbos_desde` is the instant (UTC/RFC3339, **at the top of the response**, outside `contadores`)
from which **that instance** records timestamps. The complete reading becomes:

```
ultimo_em != null                             -> the date is the answer
ultimo_em == null and carimbos_desde is OLD    -> it really never happened
ultimo_em == null and carimbos_desde is RECENT -> it may have happened before, and there is no way to know
```

**It is per INSTANCE and comes from the database — it is not the date of a version of ours.** An
instance created today timestamps from today; an instance that already existed when the field was born
received the instant the migration ran. That value can be **later** than the truth (perhaps it had
been timestamping for days), and the error leans that way on purpose: it makes you treat as *"I don't
know"* a range in which there might have been timestamps, never the other way around.

> **Why it exists instead of you noting down `v0.23.0`'s date:** that was exactly the alternative, and
> it is a constant written by hand in each consumer's code — which **rots on the first new instance**,
> because that one does not timestamp from `v0.23.0`, but from when it was born. The request came from
> a consumer, and the argument is the same one that already made us put **two** timestamps in
> `token_meta` and in `certificado_do_callback`: the reader needs to know the age of the
> **instrument**, not just that of the data.

#### 🔴 The rule above is INCOMPLETE without a reference window

*"N minutes without a delivery"*, on its own, **measures your customer's sleep, not the gateway's
health.** Raised by a consumer on 2026-07-28, a few hours after rolling out the rule exactly as this
document recommended it:

> *"It was going to fire tonight, and every night. The customer **doesn't write at 3 a.m.**"*

And there is a **second bite**, which only appears in whoever fixes the first one badly: gating only on
*"are we in business hours?"* makes the alarm fire **at opening, every day** — at 08:05 the last
delivery is last night's, 11 h old, old by definition and with nothing wrong.

**The complete rule has two conditions:**

```
alarm if:  <key>.ultimo_em is older than N minutes
       AND today's traffic window has been open for at least N minutes
```

Checked with the clock moved forward, against production:

```
03:05 -> ok      (small hours: silence is expected)
08:05 -> ok      (window just opened; the old delivery is yesterday's)
12:05 -> ALARM   "no delivery for 1330 min"
20:05 -> ok      (window closed)
```

The window is **yours**, not ours — the consumer who raised this uses 8–20h on weekdays and 10–22h at
the weekend, the same one that decides sending on their side. Your case will be different; the defect
is the same.

> **Why this rule looks right when you write it, and that is what makes it dangerous:** *"anyone with
> 24/7 machine traffic does not feel this"* (their words). A consumer with **human** traffic only finds
> out on the first night — and an alarm that fires every night for no reason is how you teach a team to
> ignore alarms, which is the opposite of what this field exists to do.

### `pausada` — before alarming on silence, look at this field

A paused instance answers `503` on sending, the volume goes to zero and the timestamp ages —
**exactly like an outage**. Without this field your alarm would say *"no delivery for 200 minutes"*
when the cause is a deliberate pause. **Rule: `pausada == true` suppresses the silence alarm** (and,
if you like, raises one of its own, because an instance paused by mistake is also a problem). `estado`
is the same information in words (`"ativa"` / `"pausada"`), for a human-facing screen.

### `serie_7_dias` — the day by day of the same window

> ⚠️ **OBSOLETE since 2026-07-29, and still working byte for byte.** The successor is `serie_diaria`,
> which accepts whatever window you ask for (30 days, for example) — the section just below.
> Everything described here applies identically to both.

Always **7 entries**, from the oldest day to the newest, each with **all** the vocabulary's keys. A day
without traffic comes present and zeroed — never absent: a variable-length series would make your
chart change shape according to traffic, and a zeroed day would disappear instead of appearing as
zero.

**Each entry carries TWO keys with the SAME value: `dia_utc` (the right name) and `dia` (obsolete).**
Use `dia_utc`. The date is in **UTC**, the same time zone in which the counters are recorded. That is
why the sum of the seven days matches `ultimos_7_dias` exactly; if you convert to the local time zone
before summing, it stops matching — and that is not the gateway's fault.

> 🛑 **DECIDED ON 2026-07-31: the day stays UTC, and there will be NO time zone parameter.** Do not ask
> for `?tz=`; it will not exist. *This is written so you can take the item off your queue, not so you
> can wait.*
>
> **Why not, and the reason is technical, not one of priority:** the day is decided on **WRITE**, not
> on read. The counter aggregates in `INSERT INTO contador (slug, dia, …)` with the date already in
> UTC (`internal/config/contador.go`, the `dayOf` function). **Once aggregated, the information about
> which LOCAL day each event fell on no longer exists** — only the instant of the bucket's last event
> survives. No read parameter recovers that: a `?tz=` would return a **wrong** number with the face of
> a right one, which is worse than not having it.
>
> **The practical consequence you need to know, measured by a consumer on 2026-07-28:** the day here
> turns over at **21:00 Brasília time**, in the middle of the operating night. A dashboard showing
> "received today" by reading `dia_utc` shows `0` at 21:01 even with activity. **If your "today" has to
> be the local one, the arithmetic is yours and the source is yours** — the gateway answers in UTC,
> accurately, and does not pretend otherwise.
>
> *A label saying "(UTC)" next to a number the operator reads as "today" **does not solve it** — that
> was the consumer's first attempt, and their owner cut it: "it's obvious that the data has to show
> reality". We agree; that is why the gateway will not pretend to know your time zone.*

> **Why the new name, and why it is worth the repeated field (a consumer's request).** We had "solved"
> the time zone by writing the warning below and asking you to write *"UTC"* on your screen. They
> disagreed with the **remedy**, not with the diagnosis: *"it puts the guard in the consumer's
> intention, and a new integrator reads no warning at all. A field name travels with the data right
> into the `console.log` of whoever is debugging at two in the morning."* And they proved it with the
> very bug they were fixing that day: someone wrote in the docstring *"do NOT touch this field, it is
> the easiest point to get wrong"*, the guard stayed in the warning, and the column's `default=` stored
> the wrong thing anyway — it cost an undelivered quote and two burned resends.

**`dia` is OBSOLETE and still works, byte for byte.** Renaming would be a contract break, and the
window to grow without breaking was that one — the route was **one day old** when this went in.

**Marked obsolete on 2026-07-28, therefore removable from 2027-01-28** (by the *Deprecation policy*,
at the end of this document). Until then it comes out in the response, with the same value as
`dia_utc`. **Migrate to `dia_utc` without hurry and without waiting for any notice**: the date is
written, and it is the notice.

> ⚠️ **The symptom this produces on the screen of someone reading in UTC−3**, raised by a consumer on
> 2026-07-28 while building their dashboard: **everything that goes out after 21:00 local falls on the
> NEXT day of the list.** It is not a counting error — it is the day boundary in UTC — but anyone
> looking without knowing swears it is, and will go looking for a counter bug where there is only a
> time zone.
> **Write that on your screen**, not just in your code. Whoever reads the dashboard does not read the
> contract.

### `serie_diaria` + `?serie_dias=` — the window **you** ask for (2026-07-29)

**`GET /v1/estado?instancia={slug}&serie_dias=30`**

`serie_7_dias` answers *"is it delivering?"* — the operational question, and seven days are enough for
it. **It does not answer *"how much am I going to spend this month"***, which is a 30-day chart. That
chart existed on a consumer's dashboard, fed by the WABA's `analytics`, **straight from the Graph**,
and the rule *nobody talks to Meta directly* closed that path: this window is the replacement, and it
is **debt the rule created**, not convenience.

- `serie_diaria` has **exactly** `serie_dias` entries, from the oldest day to the newest, with the same
  shape as `serie_7_dias` (`dia_utc`, obsolete `dia`, and **all** the keys on **every** day, including
  days without traffic);
- **without the parameter, it is 7 days** — the same content as `serie_7_dias`, day for day. The
  default did not grow so as not to inflate thirteenfold the response of someone who never asked for
  anything;
- the data was **already in the database**: the gateway keeps daily counters for **90 days** (the
  retention this document has always declared), and the cut at 7 was the route's, never the storage's.
  No new storage was needed for this to work.

#### 🔴 There is a ceiling, and asking beyond it is a `400` — never a short series in silence

```jsonc
// GET /v1/estado?instancia=lojinha&serie_dias=91   -> 400
{ "erro": { "classe": "permanente",
            "mensagem": "`serie_dias` = 91, mas este gateway guarda contador por 90 dias — a serie mais longa possivel tem 90 entradas, e as mais velhas de uma janela maior sairiam zeradas sem terem sido medidas" } }
```

**The ceiling is the retention itself**, and the message quotes the number **in force on this
installation** (the operator can shorten it via the `ZAPGW_TTL_CONTADORES_DIAS` variable). A
`serie_dias` that is not an integer ≥ 1 gets the same `400`.

**Why an error and not a shortened series**, which would be the kinder option: the days the purge has already
deleted would come back **zeroed**, and a zero from a purged day is **indistinguishable from a zero
from a day without traffic** — exactly at the oldest end of the chart, the one nobody checks. You would
add up the month and the total would come out smaller with nothing to report it. It is the same rule as
the truncated template catalogue: **incomplete is an error, never a `200`**.

#### The second limit is the INSTANCE's age, and it is already in the response

Retention is not the only boundary: an instance created three days ago has no thirty days of history to
tell, and the series comes back with legitimate zeros on the days **it did not exist**. What answers
that is **`carimbos_desde`** at the top of the same response — the instrument's age. The reading is:

```
a series day BEFORE carimbos_desde -> absence of instrument, not absence of traffic
a series day AFTER carimbos_desde  -> a zero is a real zero
```

*This matters a lot for anyone plotting cost: a monthly average computed over the days before the
instance's birth comes out diluted, and the number looks reasonable — which is the worst way to be
wrong.*

#### `serie_7_dias` still works, byte for byte, and is OBSOLETE

It **never changes shape**: with `serie_dias=30`, `serie_diaria` has 30 entries and `serie_7_dias`
still has 7 — and both come from the **same read of the database**, so one's 7-day suffix equals the
other, day for day and number for number (there is a test guarding this).

**Migrate to `serie_diaria`** (with or without the parameter: without it, it is the same 7 days). The
name `serie_7_dias` carries a number, and so it could never grow — a field with that name and 30
entries would lie about itself inside the `console.log` of whoever is debugging at two in the morning,
which is exactly the argument that produced `dia_utc`.

**Marked obsolete on 2026-07-29, therefore removable from 2027-01-29** (by the *Deprecation policy*,
at the end of this document) — the same shape as `dia`/`dia_utc`. Until then it comes out in the
response, with the same shape as always.

### A promise of ours about the vocabulary, and what it demands of you

`contadores` carries **every** key of the vocabulary, always, even zeroed — and that is guaranteed by a
test on our side, not by convention. **The good consequence:** a new key we create appears on your
screen without a release of yours.

**The consequence that demands a commitment:** if one day we remove a key, a consumer relying on its
presence breaks in silence. So let it be written: **removing a key from the vocabulary is a breaking
change** — it goes through the *Deprecation policy* (below) and into *Breaking changes*, which is the
only place the notice exists. A new key does not — it is additive, and you get it for free.

*(A consumer asked for exactly this on 2026-07-28, after pinning by test, on their side, that their
read does not filter by a fixed list: **their test still passes if our guarantee falls**, and then
their screen would lie. The guarantee is ours; verifying it is impossible from your side — that is why
it is written here as a commitment, and why removal has a minimum notice period instead of "whenever".)*

### The BILLING keys — the cost projection becomes a measurement (2026-07-28)

**A consumer's request on 2026-07-28.** Their Consumption dashboard multiplies volume by price, with
the volume **estimated on their side**. Meta sends, in each status webhook, under which category it
charged that delivery — the same `cobranca` that already travels in the envelope. These seven keys are
that data counted, and it is the house rule applied to money: **a number the gateway promises has to
come from the gateway.**

| key | what it counts |
|---|---|
| `cobranca_marketing` | deliveries charged under `marketing` |
| `cobranca_utility` | ditto, `utility` |
| `cobranca_authentication` | ditto, `authentication` |
| `cobranca_service` | ditto, `service` |
| `cobranca_outra` | Meta charged under a category the gateway **does not yet know** |
| `cobranca_ausente` | the `sent` arrived **without** the billing block — not an error, see below |
| `cobranca_cobravel` | of the above, those where Meta said `billable: true` |

**The names are Meta's LITERAL values** (`marketing`, `utility`, `authentication`, `service`), with the
`cobranca_` prefix only to group them. We do not translate their vocabulary anywhere — it is the same
rule as `"TIER_250"` not becoming `250`.

#### 🔴 Only the `sent` counts — and that is what stops the number from being up to 3x larger

The **same** send appears in `sent`, `delivered` and `read`, and all three can carry the billing block
(measured: in our corpus, the captured `sent` and `delivered` are the same `wamid`, with the same
category; and there is a `read` with a block of its own). Counting every status with a charge would
multiply the measured invoice by up to three — and an inflated number is **worse** than an estimate,
because it looks like a measurement.

`sent` is the only state every charged message passes through: a message can be sent and never
delivered (a phone switched off for days), and Meta has already charged.

#### `cobranca_ausente` is the size of what you still have to estimate

**~7.5% of `sent` arrive without a billing block** — 4 out of 53 raw `sent`, measured by a consumer
over 267 real payloads on 2026-07-28. **That is not a broken payload, it is routine.** The key exists
so that blind spot is a number instead of a silence:

    cobranca_marketing + cobranca_utility + cobranca_authentication
      + cobranca_service + cobranca_outra + cobranca_ausente   =   `sent` counted

Without it, the measurement would come out **smaller** than reality with nothing to report it. With
it, you know exactly how much volume you are still estimating.

#### `cobranca_outra` — Meta can invent a category tomorrow

When it does, the event **counts** in `cobranca_outra` and the gateway records the **literal value**
that arrived in its log. Discarding it silently would be losing money without knowing; creating a new
key from the received value would let **Meta** choose the keys of your screen and of your response.

**What that demands of you, and it is one line:** treat `cobranca_outra` as volume **charged and not
yet classified** — add it to the total when projecting cost, and do not discard it for not knowing the
category. If it rises consistently, the missing data is the **literal value** Meta sent; the gateway
records it in its own log, and on your side the available reading is that of the `status` event
itself, whose `cobranca.categoria` field arrives **raw** in the envelope. That is: **the new category
is in your `cru` before it is in any counter of ours** — if you need it today, it is there.

#### `cobranca_cobravel` counts only the `true`

`billable: false` and an absent `billable` are different facts, but **neither of them is "charged"**,
and inventing a charge out of an absence errs in the expensive direction. Consequence:
`cobranca_cobravel` is always **less than or equal to** the sum of the categories, and the difference
is *"not charged"* plus *"Meta did not say"*. The `service` we captured in real traffic, for example,
came with `billable: false`.

#### What these numbers are NOT

They count **accepted `sent` webhooks**, not unique messages. The gateway does not keep per-message
state — it is the same boundary that makes it a gateway and not a queue — so a redelivery by Meta
after a `200` would count again. It is the same caveat `recebidas` and `entregues` always had.

**The common redelivery case is covered:** counting only happens when the answer to Meta was `2xx`. If
your callback is down and we answer `5xx`, Meta resends — and nothing is counted until the webhook is
actually accepted. Without that rule, an incident on your side would inflate exactly the number you
look at most during it.

### `token_meta` — the live check, with TWO timestamps and THREE states

It answers *"does Meta still accept this instance's token?"* — the same question as
`GET /v1/instances/{slug}/health`, but **without a per-call cost**: here you read what the gateway has
already measured.

| field | what it is |
|---|---|
| `veredito` | `"ok"` · `"recusado"` · `"desconhecido"` |
| `medido_em` | when Meta last **answered** (`null` = never) |
| `conferido_em` | the last **attempt**, successful or not (`null` = never) |
| `checagem_falhando_desde` | the start of the current run of check failures (`null` = not failing) |

**Why two timestamps, and not one.** `{"veredito":"ok","medido_em":"15:20"}` on its own is **ambiguous
between two opposite states**: *"I checked at 15:20 and did not need to check again"* and *"I checked
at 15:20, and every attempt since then has failed"*. In the second case your dashboard would paint
green with Meta down. **`medido_em` and `conferido_em` diverging is the signal that the check is
failing** — visible without you knowing anything about our implementation.

**An old `ok` EXPIRES.** After 15 minutes without Meta answering, the verdict degrades to
`desconhecido` instead of staying `ok`: a cache that never expires is a lie with a timestamp.
`medido_em` still points at the last real answer — it is what says how long the gateway has gone
without hearing from Meta.

**`desconhecido` is not new vocabulary:** it is the same word as the send's error `classe`, with the
same meaning — *we do not know*. Disguising "we do not know" as either of the other two is what causes
damage.

- `recusado` = Meta refused the credential, **or** the registered `phone_number_id` is invalid and the
  call never even left the gateway. In both, **only a human fixes it** — the action is the same, hence
  the same word;
- `desconhecido` = nobody has measured yet (a freshly started gateway, a paused instance) or the
  measurement aged out.

**The one who measures is a timer of ours, running per ACTIVE instance every 5 minutes**, regardless
of whether there is traffic and whether anyone is looking. That is deliberate: if the verdict depended
on traffic, *absence of traffic* would be indistinguishable from *broken system* — a token revoked at
2 a.m. would only show up at 8 a.m., in front of the first customer. **A paused instance is not
measured** (it does not send), and that is why its verdict ages into `desconhecido`; use `pausada` so
you do not confuse the two.

**The two alarm rules this gives you, and neither requires knowing our innards:** `veredito != "ok"`,
or an aged `conferido_em`.

### `certificado_do_callback` — the validity of **your** certificate, as the gateway saw it

The sibling of `token_meta` on the other side: that one answers *"does Meta still accept this
instance's token?"*; this one answers *"will your callback's certificate still be valid next week?"*.
**A consumer's certificate expiring brings down the whole delivery**, and the symptom arrives as a TLS
failure in the small hours — knowing days in advance turns an incident into maintenance. Automatic
renewal exists precisely for that, but **automation fails silently**.

| field | what it is |
|---|---|
| `estado` | `"observado"` · `"nunca_observado"` |
| `expira_em` | the certificate's `NotAfter`, UTC/RFC3339 (`null` in `nunca_observado`) |
| `observado_em` | when the gateway **saw** that certificate, UTC/RFC3339 (`null` in `nunca_observado`) |

**It is not a probe: it is an observation.** The gateway opens no connection to look at your
certificate — it reads what the **delivery's** handshake already carries, on the same connection that
was going to happen anyway. Two consequences you need to know, and the second is the one that decides
your alarm rule:

- **without a delivery, there is no new observation.** The data ages when traffic stops (or when the
  instance is paused). That is why `observado_em` travels alongside: a certificate observed three
  weeks ago **is not current information**, and the gateway cannot pretend it is;
- **it is the LEAF certificate** (yours), not the whole chain. It is the one that renews every ~90 days
  and the one that breaks when renewal fails. An intermediate of your CA expiring does not appear here.

**`nunca_observado` is a state with a name, and that is deliberate — treat it as "no information",
never as "expired".** It means that **no delivery from this instance has completed a handshake**: a
freshly created instance, or a consumer that has not received anything yet. It is not a failure, and it
is not on its own a reason to alarm.

> **Why the word, and not just `null`.** This route already paid that bill once: it went live with
> `ultimo_em: null` on counters that **did** have history (the timestamp had only started being
> recorded at that moment), and anyone treating `null` as "very old" would have started with a false
> positive on everything. Here the case is permanent — an instance without deliveries will never have a
> date — so the difference between *"never saw it"* and *"saw it and it is bad"* is **unambiguous in
> its form**: a word that does not look like a date and does not compare with any date. The field also
> **does not disappear** from the response: an absent field would force you to distinguish "absent"
> from "null" in order to answer the same question.

**The gateway does not say "expired" nor "expires in N days", and that too is a decision.** It would be
a judgement about an observation that may be old — the certificate may have been renewed after it.
With the two timestamps in hand you do the arithmetic better than we would, including deciding how
much observation age you tolerate.

**The suggested alarm rule**, and it uses both fields on purpose:

```
estado == "observado"
  AND expira_em - now < 14 days
  AND now - observado_em < 24 h      # otherwise you are alarming about old information
```

And, separately from that: `estado == "observado"` with a very old `observado_em` **on an active
instance with traffic** means delivery has stopped — but for that `entregues.ultimo_em` answers
better, because it exists for exactly that question.

Example of the block on an instance that has not delivered anything yet (**pasted from the same run**
as the example above, before the observation):

```json
"certificado_do_callback": {
  "estado": "nunca_observado",
  "expira_em": null,
  "observado_em": null
}
```

### `numero_na_meta` — your number's **quality** and **messaging limit** (2026-07-28)

The third sibling of `token_meta` and `certificado_do_callback`. The first two answer *"does this
credential still work?"*; this one answers the neighbouring and equally expensive question: **"can this
number still send the volume I planned?"**

**It exists because you lost it.** Until 2026-07-28 those two values were read straight from the Graph
API with your token. The gateway's rule — *nobody talks to Meta directly* — closed that path, and this
is the door that replaces it. What each one decides on your side:

- **`limite_de_mensagens`** is the *tier* — the daily ceiling of initiated conversations. It **changes
  on its own**: the account matures and it goes up, or it is downgraded and it falls. Planning the
  month with the old tier is planning wrong;
- **`qualidade`** is the **early** warning that the account is heading for a restriction. Finding that
  out through the block is finding out late.

```jsonc
"numero_na_meta": {
  "qualidade": {
    "estado": "observado",
    "valor": "GREEN",
    "observado_em": "2026-07-28T20:36:58Z",
    "fonte": "medicao"
  },
  "limite_de_mensagens": {
    "estado": "observado",
    "valor": "TIER_50",          // downgraded, and the notice arrived PUSHED
    "observado_em": "2026-07-28T20:40:03Z",
    "fonte": "webhook"
  },
  "conferido_em": "2026-07-28T20:36:58Z"
}
```

*(Both examples in this block — this one and the `nunca_observado` one below — are **pasted from a run
of the real handler**, never typed.)*

| field | what it is |
|---|---|
| `<value>.estado` | `"observado"` · `"nunca_observado"` — the **same** words as `certificado_do_callback`, because the question is the same |
| `<value>.valor` | Meta's literal (`null` in `nunca_observado`) |
| `<value>.observado_em` | when the **gateway** learned that value, UTC/RFC3339 (`null` in `nunca_observado`) |
| `<value>.fonte` | `"medicao"` · `"webhook"` (`null` in `nunca_observado`) |
| `conferido_em` | the last time the gateway **tried to measure**, UTC/RFC3339 (`null` if it never tried) |

#### 🔴 The values are Meta's LITERALS — `"TIER_250"` does not become `250`

`"TIER_250"`, `"TIER_1K"`, `"GREEN"`, `"UNKNOWN"` arrive **exactly** as Meta sends them. The gateway
does not translate them into a number, does not rank the qualities and does not derive "bad" from any
word. Translating would require a table of ours, and it would be wrong the day Meta invented a new
value — wrong in the worst way: returning a plausible number for something nobody checked. **If you
need a number, do the conversion on your side, and treat an unknown value as unknown.**

It is the same rule as the `qualidade_do_numero` event, and by construction: both carry the same
literal.

#### The TWO sources, and who wins when they disagree

`limite_de_mensagens` arrives by two paths, and the `fonte` field says which one produced the value
you are reading:

- **`medicao`** — the gateway asks the Graph API, per **active** instance, in the same cycle in which
  it already checks the token. It guarantees a new instance has a value even if nothing ever changes;
- **`webhook`** — Meta **pushes** `phone_number_quality_update` when the limit changes. It is the only
  path that arrives within seconds, and it is the one that warns of a **downgrade before sending
  fails**.

**The most RECENT observation wins, whatever the source.** There is no preferred source: "the webhook
always wins" would let a Meta redelivery (it retries for up to 36 h) regress a value measured later;
"the measurement always wins" would throw away exactly the pushed warning.

**`qualidade` has only one source (`medicao`)**, and that is not a gap: the
`phone_number_quality_update` webhook **does not carry a quality rating** — it carries an `event`
(`ONBOARDING`/`FLAGGED`/`UNFLAGGED`), which is a different fact. Inventing an equivalence between them
would be asserting a translation Meta's documentation does not support.

#### `observado_em` is the GATEWAY's clock, not Meta's

The timestamp says **when the gateway learned**, not when Meta recorded the change. It is the same
definition as `certificado_do_callback.observado_em`. The reason is that the alternative would compare
two clocks nobody synchronized — and a drift of minutes would decide, silently, which source wins.

#### The TWO timestamps, and what divergence between them means

`conferido_em` moving while the `observado_em`s stand still means **"the gateway is measuring and
coming back without the data"** — Meta stopped sending the fields, or the field request was refused. In
that state the value you read is still true *for its date*, and `observado_em` is what tells you how
old it is.

**`conferido_em: null` on a `pausada` instance is expected**, not a failure: a paused instance is not
measured on purpose — it does not send, and spending a call on it would be measuring a channel that
cannot fail.

#### `nunca_observado` is a state with a name — treat it as "no information", never as "bad"

The state is **per value**, not per block, because the mixed case genuinely exists: a limit webhook can
arrive before the first measurement, and then the limit is observed and the quality is not.

```json
"numero_na_meta": {
  "qualidade": {
    "estado": "nunca_observado",
    "valor": null,
    "observado_em": null,
    "fonte": null
  },
  "limite_de_mensagens": {
    "estado": "nunca_observado",
    "valor": null,
    "observado_em": null,
    "fonte": null
  },
  "conferido_em": null
}
```

**The suggested alarm rule** — and it is yours, because the gateway publishes no judgement here:

```
limite_de_mensagens.estado == "observado"
  AND limite_de_mensagens.valor != <the tier you planned for>     # it went up or down without you knowing
qualidade.estado == "observado" AND qualidade.valor != "GREEN"    # and treat an UNKNOWN value as unknown
```

#### 🔴 On an Instagram instance, this block (and `token_meta`) say `nao_se_aplica` (T-099)

**Quality and messaging tier are WhatsApp Business Number concepts — Instagram does not have them, and
never will.** Until v0.36.0 this block came out as `nunca_observado` on an Instagram instance (measured
in production, `tenant-two-ig`, 2026-07-30 21:11), which is the WRONG answer: `nunca_observado` says
*"we have not measured yet, wait"*; the right answer is `nao_se_aplica`, which says *"it will never
exist here, do not look"*. If you read `nunca_observado` in a field that will never be filled, you
either wait forever or alarm about something that does not exist — it is the same problem
`token_instagram` already solves on the WhatsApp side (next section), and the two directions now use
the **same word**.

```json
"numero_na_meta": {
  "qualidade": { "estado": "nao_se_aplica", "valor": null, "observado_em": null, "fonte": null },
  "limite_de_mensagens": { "estado": "nao_se_aplica", "valor": null, "observado_em": null, "fonte": null },
  "conferido_em": null
}
```

**And the same applies to `token_meta.veredito`, which also comes out as `"nao_se_aplica"` on an
Instagram instance.** The reason is subtler than "the field does not apply by definition": the live
check (`vigia.go`) measures by calling `GET /{phone_number_id}` on the Graph API, and an Instagram
instance **never has** a `phone_number_id` (registration refuses it if it comes filled in). Without
this handling, the watcher would measure with the field empty, the Graph would refuse the call locally
(there would not even be a network request), and the gateway would classify that as a **refused
credential** — a **permanent and false** `veredito: "recusado"` on every healthy Instagram instance,
because the check was never designed to measure anything over there. That is why the gateway does not
let that result leak: `token_meta` also becomes `nao_se_aplica`.

### `token_instagram` — the validity of Instagram's long-lived token (2026-07-30)

It answers *"will this Instagram channel keep working?"* — from the **token**'s side, which here has a
hard difference from WhatsApp: it **expires in 60 days**, always, and past that deadline **there is no
possible renewal** — only a manual login at Meta. The gateway tries to renew on its own, with margin,
but this is the only block in the whole of `GET /v1/estado` in which "we did nothing" has a
**definitive** outcome, not a grey zone.

🔴 **This field appears on EVERY instance, even WhatsApp — the absence is ASSERTED, never inferred.**
`GET /v1/estado?instancia=<slug>` is **a single endpoint, one call per instance** (the
consumer→instance link decides which). On a **WhatsApp** instance this block comes out like this,
always:

```json
"token_instagram": {
  "veredito": "nao_se_aplica",
  "definido_em": null,
  "expira_em": null,
  "dias_restantes": null,
  "renovado_em": null,
  "falhando_desde": null,
  "instrucao": null
}
```

**This is NOT the block broken — it is the block telling the truth.** The System User token WhatsApp
uses has no 60-day deadline (it is the same reason `numero_na_meta` and `token_meta` always come out
`nao_se_aplica` on the Instagram side — see the corresponding subsection, above — and
`token_instagram` always comes out `nao_se_aplica` on the WhatsApp side: each Meta product has the
credential it has, and the block ALWAYS exists in the response, saying which of the two is your case).
A field that simply disappeared, or came with the numbers zeroed instead of `null`, would make you
think automatic renewal is broken when it never existed there.

| field | what it is |
|---|---|
| `veredito` | `"nao_se_aplica"` · `"aguardando"` · `"ok"` · `"falhando"` · `"expirado"` |
| `definido_em` | when the CURRENT token was set — creation, your registration, a rotation by the owner, or the last automatic renewal (`null` in `nao_se_aplica`) |
| `expira_em` | `definido_em` + 60 days (`null` in `nao_se_aplica`) |
| `dias_restantes` | can be **negative** (expired N days ago) (`null` in `nao_se_aplica`) |
| `renovado_em` | the last time the **automatic loop** renewed this token successfully — `null` until the first real renewal, even if the original token still has days of life |
| `falhando_desde` | the start of the current run of renewal failures (`null` = not failing) |
| `instrucao` | text explaining what to do — only present when `veredito` is `falhando` or `expirado` |

**The five verdicts, and what each asks of you:**

- **`aguardando`** — a valid token, still far from the renewal threshold (from 30 days of age).
  Normal, no action at all;
- **`ok`** — the automatic loop **has already renewed this token successfully at least once**. It is
  the answer to *"does the mechanism really work?"* — and that is why it is a verdict of its OWN, and
  not the same as `aguardando`: a token that never needed renewing has proven nothing about the
  automation yet;
- **`falhando`** — the most recent renewal attempt did not work (Meta refused, or storing the new
  token failed) and the token **has not expired yet**. `falhando_desde` shows the HONEST first failure
  — there is no delay and no threshold here: if the gateway has been failing for 10 minutes, that is
  what the response has been saying for 10 minutes;
- **`expirado`** — more than 60 days went by without renewal. **There is no automatic renewal possible
  any more.**

#### 🔴 You are the one who alarms — the gateway only records

**Owner's decision, 2026-07-30.** The gateway does **not** send a notification, does not escalate and
does not open a ticket when renewal fails — it writes an `ALARME` line in its own log (operational, it
does not reach you) and leaves the STATE honest for you to read. **You already have a channel to talk
to whoever operates this gateway; building a second channel here would be worse than what already
exists on your side.**

🔴 **And the reason this matters more here than in any other block: you cannot fix it yourself.** The
token is not in your hands, by this gateway's design (nobody talks to Meta directly). That is why
`instrucao` is not cosmetic — it is the only thing separating `veredito: "falhando"` from a dead end
for someone with no access to the problem.

**A practical alarm rule, yours:**

```
veredito == "expirado"                                   # stop everything, it is manual
  OR (veredito == "falhando" AND falhando_desde is more than a few days old)
```

If `falhando_desde` goes beyond a few days, get hold of the owner of the Instagram account at Meta —
the resolution is **manual** and is not on the gateway's side. A freshly appeared `falhando` normally
resolves itself on the next cycle (an unstable network, a passing `5xx` from Meta); it is the
PERSISTENCE of the failure that calls for a human, not the first occurrence.

Example of an Instagram instance failing (pasted from a run of the real handler):

```json
"token_instagram": {
  "veredito": "falhando",
  "definido_em": "2026-06-15T00:00:00Z",
  "expira_em": "2026-08-14T00:00:00Z",
  "dias_restantes": 12,
  "renovado_em": null,
  "falhando_desde": "2026-08-01T09:00:00Z",
  "instrucao": "a renovacao automatica esta falhando; a resolucao e MANUAL, do lado de quem opera o gateway ou e dono da conta Instagram na Meta — o token nao esta ao alcance deste consumidor"
}
```

### `entrada` — WHERE the inbound path is published, and whether the connector is up (2026-08-06)

🔴 **READ THIS SENTENCE BEFORE USING THE BLOCK, because it is what prevents the expensive
misunderstanding:** `via` and `conector` describe **where the inbound path is published** and
**whether the connector is up** — they do **NOT** promise that Meta is managing to deliver. **What
answers that question is a probe, measuring from OUTSIDE.**

**Why the gateway cannot answer that, and it is not for lack of will:** a request that does not arrive
leaves no trace at all in here. If the public path goes down, the journal does not record what did not
arrive, the subscription at Meta stays correct, the counters merely stop rising and `/v1/health` stays
`200`. On **2026-08-06** the link went down for ~9 minutes with every monitor green, and the one who
warned us was the consumer. **An `alcancavel: true` field would be exactly that blind monitor** — that
is why it does not exist here, and will not come to exist.

**What you can do with the block, then:**

| field | is | what it is for |
|---|---|---|
| `via` | **configuration**, not measurement — `tunel`, `encaminhamento_de_porta` or `desconhecido` | knowing where the inbound path should be arriving through when you report an outage |
| `conector` | a **measurement** of the `/ready` of the connector that publishes the route | telling "the tunnel went down" apart from "the gateway is quiet" |
| `ultimo_webhook_em` | the **same** value as `contadores.recebidas.ultimo_em` | concluding **silence** on your own, without reading the counters table |

**`conector.estado` has THREE values, and the difference between two of them is the point of the
block:**

- **`observado`** — the gateway asked and the connector answered. `conexoes_prontas` carries the
  number, and it **can be `0`**: zero is a legitimate measurement ("the connector is up and there is
  no tunnel established"), the strongest signal this block can give. `falhando_desde` comes `null`;
- **`desconhecido`** — **I could not measure**. `conexoes_prontas` comes **always `null`**, never a
  zero that looks like a verdict; `falhando_desde` says since when the question has not been coming
  back (`null` if there was never an attempt), and `medido_em` still points at the **last real
  answer**, which is what says how long the gateway has gone without hearing from the connector;
- **`nao_configurado`** — nobody told the gateway whom to ask (an installation without a tunnel). All
  three fields come `null`.

⚠️ **`observado` is NOT a health verdict.** The gateway publishes what it measured and when it
measured it; the one who judges is you. It is the same rule as `certificado_do_callback`, which also
has no "expired" state.

⚠️ **The block comes ALWAYS, with all its keys, on every instance** — including `nao_configurado` and
including on an Instagram instance. A field that disappears breaks a strict parser, and this contract
already paid for that with `token_instagram`.

ℹ️ **`via` and `conector` are the GATEWAY's, not the instance's:** two instances of the same gateway
read exactly the same values. Only `ultimo_webhook_em` is per instance.

**The alarm rule this gives you:** `conector.estado == "observado" && conexoes_prontas == 0` is *"the
tunnel went down"* — act. `conector.estado == "desconhecido"` is *"the gateway is not managing to
measure"* — a different urgency, a different place to look, and **never** the same alarm.

### `alcance_externo` — the public probe's verdict, mirrored here (2026-08-07)

🔴 **READ THE SECTION "Two different questions" (above) BEFORE USING THIS BLOCK.** It is
**convenience**, not a second source: when the gateway is silent, this block is too — it is the
probe's public URL (the section above) that survives our outage, never this field.

```jsonc
"alcance_externo": { "estado": "observado", "veredito": "up", "medido_em": "2026-08-07T13:05:00Z",
                      "fonte": "sonda_externa" }
```

| field | is |
|---|---|
| `estado` | `observado`, `nao_configurado` or `nao_consegui_verificar` — see below |
| `veredito` | the literal the external probe answered (today, `"up"` or `"down"`), **untranslated** — `null` outside `observado` |
| `medido_em` | the last time the external probe actually ANSWERED — it keeps pointing at that answer even after the state degrades, for the same reason as `token_meta.medido_em` |
| `fonte` | today always `"sonda_externa"` when `observado`; it exists for the day a second mechanism comes in, without forcing you to reinterpret the contract |

**`estado` has THREE values, and the distinction between the last two is the point of the block:**

- **`observado`** — the gateway asked the external probe and it answered. `veredito` carries its
  literal, **including `"down"`** — a MEASURED down is `observado` with `veredito: "down"`, not a
  separate state;
- 🔴 **`nao_consegui_verificar`** — the gateway's LAST attempt to ask the external probe did not come
  back (no answer, no readable JSON, or no expected field), OR the last good answer is past its
  validity. **This is NEVER `down`, and never the field being absent.** It is a word of its own,
  different from the `desconhecido` used in `token_meta`/`conector` — because the decision you are
  going to automate on top of it is different: "I could not ask" is not "you are down";
- **`nao_configurado`** — this gateway does not have `ZAPGW_SONDA_EXTERNA_URL` configured yet.
  `veredito`, `medido_em` and `fonte` come `null`.

⚠️ **The block comes ALWAYS, with all four keys, on every instance** — the same rule as `entrada` and
`token_instagram`: a field that disappears breaks a strict parser.

**The alarm rule this gives you:** `alcance_externo.estado == "observado" && veredito == "down"` is
*"the public inbound path went down"* — act, with the SAME urgency as a direct read of the probe.
`alcance_externo.estado == "nao_consegui_verificar"` is *"the gateway could not ask the probe right
now"* — it is no sign of an outage at all; if you want to know anyway, use this section's direct URL,
which does not depend on the gateway answering.

### Errors

| status | when | body |
|---|---|---|
| `400` | the `instancia` parameter is missing | `{"erro":{"classe":"permanente","mensagem":"parametro de consulta \`instancia\` e obrigatorio"}}` |
| `401` | no token, or a token nobody recognizes | the standard error body |
| `403` | the instance is not yours | `{"erro":{"classe":"config","mensagem":"instancia nao autorizada para este consumidor"}}` |
| `404` | a slug that does not exist | `{"erro":{"classe":"config","mensagem":"instancia desconhecida"}}` |
| `503` | the gateway could not read its own database | `{"erro":{"classe":"retentavel","mensagem":"indisponivel"}}` |

The `400` and `403` bodies above are **pasted from a run**, not typed.

> **A missing parameter is `400`, not `403`.** A `403` there would send you checking your link, which
> is correct — the defect would be hidden in the wrong place.

### What this route deliberately IS NOT

- **It is not Prometheus.** The audience is you, not a scraper. A scraping format would invite
  infrastructure this project does not have and that the premise "run outside the homelab one day"
  makes uncertain;
- **it does not accept an arbitrary date filter**, nor per-event history. The windows are the two the
  gateway already shows to whoever operates it. A surface that grows without a consumer asking is a
  dead surface that reads as current;
- **it does not count by billing category.** That is a new counter on the inbound path, not a response
  format — it is recorded as a task of its own.

---

## Read the template catalogue

**`GET /v1/templates?instancia={slug}`** · `Authorization: Bearer <your token>` ·
optional: `&status=APPROVED`

Same rules as `/v1/messages`: a **LAN** route, on the internal entrypoint (`:8443`), and only the
instances linked to you answer — `403` for the others, without the gateway even talking to Meta. The
catalogue describes the tenant's business (campaign names, collection wording), so the separation
holds here for the same reason as for sending.

```jsonc
{ "instancia": "lojinha",
  "total": 84,
  "templates": [
    { "id": "1234567890",
      "nome": "lembrete_consulta",
      "status": "APPROVED",
      "categoria": "UTILITY",
      "idioma": "pt_BR",
      "componentes": [ /* exactly as Meta returns them, without rewriting */ ] },
    { "id": "1234567891",
      "nome": "acessar_galeria",
      "status": "REJECTED",
      "categoria": "UTILITY",
      "idioma": "pt_BR",
      "motivo": "INCORRECT_CATEGORY",
      "componentes": [ /* … */ ] }
  ] }
```

> **`id` is the template's id at Meta — the same one `POST /v1/templates` returns.** It came in on
> 2026-07-28 and **can be missing**: the gateway does not require the field, because it was not
> verified at Meta's source that every page of the catalogue carries it, and bringing down the whole
> catalogue read because of it would be trading a useful catalogue for none. When it is missing, the
> key simply does not come. **Keep identifying a template by the pair `nome` + `idioma`** — that is
> what goes in the send, and that is what Meta treats as unique within the account.

> **`motivo` is Meta's `rejected_reason`, raw — it came in on 2026-08-04 (T-116) to answer "why was it
> rejected?" without requiring a manual read of the WhatsApp Manager.** The real cost that motivated
> the task: `consumer-b` had `acessar_galeria` rejected, formed a blind hypothesis, created
> `acessar_galeria_v2`, rejected again — two attempts, zero new information, and each attempt **burns
> a template name forever** (Meta does not release a rejected template's name for immediate reuse).
>
> - **`"NONE"` is a normal value, not an absence** — the same doctrine as the `template.motivo` of the
>   `template_status` webhook (section above): Meta sends the literal string `"NONE"` when there is no
>   reason, and the gateway does not translate it into empty. If the `motivo` key disappears from the
>   JSON, it is because Meta really did not send `rejected_reason` on that item — "said NONE" and "did
>   not send the field" are different facts.
> - **The prose text from the WhatsApp Manager does NOT come, and will not come while Meta does not
>   expose a field for it.** `rejected_reason` is a short enum (`ABUSIVE_CONTENT`, `INVALID_FORMAT`,
>   `NONE`, `PROMOTIONAL`, `SCAM`, `TAG_CONTENT_MISMATCH`, `INCORRECT_CATEGORY` observed in the
>   webhooks) — the explanatory paragraph that appears in the panel has no documented field in the
>   Graph API, and the gateway does not synthesize text from the enum: that would be a false doc in
>   the shape of a field.
> - **The same reason already arrives through the `template_status` webhook** (`template.motivo`,
>   section above) **at the instant Meta decides** — without you having to ask. This field here is for
>   when you read the catalogue for another reason (or missed the webhook); the webhook is the path
>   that warns on its own.

**The list comes WHOLE, or an error comes — never a short list.** This is the reason the endpoint
exists: this network's old gateway returned only the **first 25** templates of an account with **84**,
and that is what took it out of production. The truncation gave no error at all, so from the outside it
was indistinguishable from the truth: the consumer system concluded the template "does not exist" and
the message simply never went out. Here the gateway follows Meta's pagination **until it ends**, and if
one day the catalogue does not fit within its pagination limit (**50 pages of 100**, ~5000 templates),
you receive a **`502` with `classe: config`** and **no list** — on purpose.

**If that happens, repeating does not solve it — and the only way out in your hands is `&status=`.**
The parameter is passed on to Meta in the query (besides being reapplied here), so, **if it honours
it**, the read walks fewer pages and fits again. It is a palliative, and it is written as a palliative:
if Meta ignores the filter, the `502` remains. It is worth trying before anything else, because it
costs a query string, and approved templates are in any case the subset that matters for sending.

`status` filters (`APPROVED`, `PENDING`, `REJECTED`, …). The filter is applied **on the gateway's side
too**, so `status=APPROVED` really means approved — without depending on Meta honouring the parameter.

**There is no cache**, and that is deliberate: a template changes status at Meta **without telling the
gateway**, and a stored catalogue would answer `APPROVED` about something that has just been rejected.
In exchange, each call costs a trip to the Graph API on that instance's account — read the catalogue
when you need it, not in a tight loop.

`componentes` travels **raw, as Meta returns it**. The gateway neither rewrites nor validates that
part: describing that format would freeze a shape that is theirs, not ours.

## Create a template

**`POST /v1/templates`** · `Authorization: Bearer <your token>`

Same rules as its sibling `GET` and as `/v1/messages`: a **LAN** route, on the internal entrypoint
(`:8443`), and only the instances linked to you answer — `403` for the others, without the gateway
even talking to Meta. **It is the same path and the same port as `GET /v1/templates`**; the only
difference is the verb.

```jsonc
{ "instancia": "lojinha",
  "nome": "lembrete_consulta",
  "categoria": "UTILITY",
  "idioma": "pt_BR",
  "componentes": [ { "type": "BODY", "text": "Olá {{1}}, sua consulta é amanhã." } ] }
```

`allow_category_change` is **optional** and, if you send it, travels **verbatim** to Meta's
`allow_category_change` — the gateway does not validate, interpret or translate the value; the one
who decides the effect is Meta. If you do not send the field, the gateway **sends nothing** about it
to Meta (there is no implicit `false` going out silently on your behalf).

> ⚠️ **What is known about this parameter, and what is NOT — read before relying on it.** Meta
> documents (pages read on 2026-07-30:
> [`new-template-guidelines`](https://developers.facebook.com/docs/whatsapp/updates-to-pricing/new-template-guidelines/)
> and
> [`template-categorization`](https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-categorization))
> that, since **2025-04-09**, automatic recategorization **became the default behaviour**: asking for
> `UTILITY` on content it classifies as `MARKETING` results in *"the template is approved as
> `MARKETING`"* — the behaviour `allow_category_change: true` used to turn on before that date.
>
> **What is NOT confirmed, and the gateway does not promise:** whether sending
> `allow_category_change: false` still makes Meta **refuse** creation instead of recategorizing. The
> documentation does not state that — it only describes what `true` did before it became the default.
> **The gateway merely passes the field through**; it guarantees no effect on the outcome, and there
> is no way to guarantee something the source does not document.
>
> **Recategorization cannot be undone through this API, with or without `allow_category_change`.** The
> path that exists is the *category review request*, available **only through the WhatsApp Manager**
> (Business Support Home → *Template Category Updates*), for a `MARKETING` template with status
> `APPROVED`, or `UTILITY`/`MARKETING` with status `REJECTED`, within **60 days** of the category
> change. It is a human action by the account's owner — **the gateway has no route for it and will not
> have one, because there is no endpoint** for that review in the Graph API.

> ⚠️ **Here `componentes` is Meta's RAW format — and that is the OPPOSITE of what applies when
> sending.** In `POST /v1/messages` a raw `components` is **refused** (`ErrRawComponents`): there the
> gateway models `cabecalho` and `botoes_template` and assembles the block, to make what Meta rejects
> inexpressible. Here it passes the list on as it came, and the only guard is that it be a **real JSON
> list** (`null` and `{}` do not fail the `Unmarshal` and would travel as a template with no body).
>
> **The asymmetry is deliberate, and the criterion is the failure mode:**
>
> | | If you get the format wrong |
> |---|---|
> | **sending** | Meta accepts and the message arrives **wrong on the customer's phone** — a silent failure |
> | **creating** | Meta **refuses on the spot**, with its own message, and nothing was sent to anyone |
>
> Where getting it wrong is cheap and Meta's vocabulary is large and changing, modelling would cost
> more than it protects. Where getting it wrong is expensive and silent, the gateway models.
>
> **The consequence for you: whoever CREATES a template needs to know Meta's format; whoever SENDS
> does not.** Use Meta's template components reference to assemble this list.

Success → **`201`**:

```jsonc
{ "id": "1234567890",
  "status": "PENDING",
  "categoria": "UTILITY",
  "categoria_pedida": "UTILITY",
  "aviso": "template recem-criado NAO pode ser usado na hora: ele nasce PENDING e so vale depois de aprovado pela Meta. …" }
```

**A created template CANNOT be used right away.** It is born `PENDING` and only becomes valid when
Meta approves it — that is why the `aviso` comes with every successful creation. Anyone trying to send
right after creating gets an error from Meta and cannot explain it, because the creation answered
success. Check the status in `GET /v1/templates` before sending.

`categoria_pedida` is the **echo of what you asked for**, always present (it is a required field of
the request). `categoria` is what **Meta stored** — and the two can be different.

> 🔴 **Meta can STORE a category different from the one you ASKED FOR, with no error and no warning of
> its own.** A field report from `consumer-b` (2026-07-30): they submitted `instagram_continuar` as
> `UTILITY` and Meta stored `MARKETING` — they only found out on rereading the catalogue afterwards.
> The category decides **billing** (`MARKETING` and `UTILITY` have different prices) and whether the
> message needs opt-in — a silent swap is money and compliance, not aesthetics.
>
> When `categoria` (what Meta stored) is **different** from `categoria_pedida` (what you sent, a
> comparison that ignores case and whitespace), the `aviso` gains a second passage spelling out both
> categories: that the swap is **NOT an error** — Meta can recategorize a template at creation itself —
> and that the gateway **does NOT undo** the swap. This holds on both success paths: the normal
> creation and the one reconstructed by rereading the catalogue (next section).

The error body is the same as for sending, with the same classes.

### When creation ends **without an answer from Meta**

There is an outcome that is neither success nor refusal: the request left here and **no verdict came
back** (transport down, deadline exceeded, or a `2xx` without an `id`). In that case the gateway
**does not hand you back a question** — it **rereads the catalogue** (`GET`, and only `GET`) and
answers with what it found. There are three possible answers, and each wants a different reaction from
you:

| What the gateway found on the reread | What you receive | What to do |
|---|---|---|
| **found the template** | **`201`**, just like a normal success, with `id`, `status` and `categoria` coming from the catalogue. The `aviso` says the creation ended without an answer and that the reread **confirmed** the template exists | nothing. **It was created.** Only the response was lost |
| **did not find it** | **`502`**, class `desconhecido`, and the message contains the word **INCONCLUSIVO** | **do not recreate blindly.** Query `GET /v1/templates` in a few minutes and decide with the result |
| **the reread also failed** | **`502`**, class `desconhecido`, saying the reread did not work either | wait and query `GET /v1/templates` |

> 🔴 **"I did not find it" does NOT mean "it was not created", and the gateway will never say it
> does.** Meta documents *read-after-write* for the **`POST`'s own response** on that edge — which is
> precisely what did not arrive — and **documents nothing** about a later `GET` already containing the
> freshly created template (checked at the source on 2026-07-28; it does not document the opposite
> either). Without that guarantee, a catalogue that does not show the template is doubt, not a verdict.
>
> **The error is asymmetric, and that is why the word is that one.** If we say "I don't know" and the
> template does not exist, you lose one check. If we say "it was not created" and it does exist, you
> recreate it — and `nome` + `idioma` are **unique per account**: the second creation comes back
> `code 100` / `subcode 2388024`, and **the name you chose becomes unusable**. One error costs a
> minute; the other costs a name.

**The reread is a `GET` and nothing else.** The gateway **never** tries to create again on its own, in
any of the three outcomes — the one who decides to repeat is you, with the fact in hand.

> **The reread tries more than once, spaced out, before declaring "not found" (T-101).** The MOST
> LIKELY case on this path is the template having been created and Meta's catalogue taking a few
> seconds to propagate — and a single immediate reread gave that no time to happen, coming out with
> the SAME FACE as the rare outcome ("it really was not created"). Now, if the first reread (immediate,
> no wait) does not find it, the gateway tries again after pauses of **2 s, 5 s and 10 s** (up to 4
> attempts in total) before giving up. Found on any of them → the outcome is the success one in the
> table above, without exception. Not found on any → the **INCONCLUSIVO** `502` comes out with the
> **same text as always** — the warning does not get weaker, only rarer.
>
> The response body (success or error) carries `releituras` (how many attempts happened) and
> `espera_segundos` (how long the gateway stayed paused between them), so you can calibrate the
> timeout on your side with a fact instead of an estimate.
>
> 🔴 **The ceiling of PURE waiting on this path is 17 s (2+5+10), and it counts against your
> "comfortable" 30 s deadline** (see above). Add to that ceiling the network time of the original
> creation attempt and of each reread — none of them is instantaneous. If your client uses a tight
> timeout, it may give up BEFORE the gateway has finished waiting, and you are back in the same limbo,
> only without our message.

**The cost you should know:** a request that falls on this path can take up to **two** instance
deadlines (the one for the creation that died, plus the one for the reread) **plus up to 17 s** of the
spaced pauses between rereads (zero if the first one already finds it — the common path does not get
slower). It is the price of receiving a fact instead of a question.

> **Why this changed:** until 2026-07-28 the answer was a `502` with *"the template MAY have been
> created — check the catalogue"*. On 2026-07-28 a consumer hit exactly that while creating a template
> that **had been created**, and only found out because they still had direct Graph API access to
> check. That access no longer exists — **nobody talks to Meta directly** — so telling you to check
> stopped being an answer. The one who checks now is the gateway.
>
> **And why the reread started insisting:** on 2026-07-30 `consumer-b` got the INCONCLUSIVO `502` when
> creating `selecao_provas_novas` — and less than a minute later `GET /v1/templates` already carried
> the template. The creation had worked; only the reread was too early for the catalogue's
> propagation. The warning's text was still right (it prevented a recreation that would have burned
> the name), but the common outcome (created and propagated in seconds) came out with the same face as
> the rare one — and that is what the spaced pauses solve.

---

## Delete a template — `DELETE /v1/templates` (2026-08-28)

Deletes **ONE** template, **by name**, from the instance's WABA. It is the only route in this gateway
that **destroys** anything on Meta's side, and its entire design follows from that.

```
DELETE /v1/templates?instancia=<slug>&nome=<name>
Authorization: Bearer <your key>
```

Both parameters are mandatory. There is no body.

> 🔴 **Meta deletes in ALL LANGUAGES of that name, at once.** Verbatim from the `message_templates`
> edge reference: *"Name of template to be deleted. Deletes templates matching the name in all
> languages"*. If the same name exists in `pt_BR` and in another language, **one call takes both** —
> that is why the response returns the list of what went, and not an "ok".

> 🔴 **There is no wildcard, there is no batch, and that is not policy — it is construction.** The
> `nome` must match `^[a-z0-9_]{1,512}$`, checked **before** any call to Meta: `*`, `%`, a space, a dot
> and an uppercase letter are refused with `400` without leaving here. Meta offers batch deletion
> (`hsm_ids`) and this gateway **does not use it anywhere**. One name per call, always. Deletion has no
> undo, and a wildcard Meta accepted would erase a whole family.

### The three outcomes, and why they do not collapse into a `200 {}`

A cleanup of dozens of templates **will** be interrupted and resumed. If "I deleted it just now" and
"it was already gone" come back identical, you cannot report what you did — and the outcome that ruins
the report is not the error, it is the "I don't know". That is why there are three, and the `desfecho`
field separates them:

| `desfecho` | HTTP | what happened |
|---|---|---|
| `apagado` | `200` | the template existed and Meta accepted the deletion |
| `ja_nao_existia` | `200` | the name **was not** in the catalogue. **Nothing was asked of Meta** — it is what makes resuming genuinely idempotent |
| *(inconclusive)* | `502`, class `desconhecido` | the call went out and **no verdict came back**. See below |

**Success** (the first two share the same body):

```json
{
  "instancia": "seu-slug",
  "nome": "galeria_atualizada_v6",
  "desfecho": "apagado",
  "entradas": [
    {"id": "1563912508540305", "idioma": "pt_BR", "categoria": "UTILITY", "status": "APPROVED"}
  ],
  "aviso": "a exclusao apaga o template em TODOS os idiomas, e a Meta NAO aceita criar um template com o MESMO nome por 30 dias. ..."
}
```

`entradas` is what **was** deleted, one item per language, read from the catalogue before the
deletion. In `ja_nao_existia` it is `[]` — **never `null`**, so you do not have to handle two different
kinds of empty.

### The 30 days are Meta's, and the warning only travels in `apagado`

> *"If you delete an approved template, you cannot create a new template with the same name for 30
> days."*
> — developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management
> (read on 2026-08-28)

**Treat the name as burned.** If you need to recreate it before that, choose another name — the
creation will fail, and the error comes from Meta, not from here.

The warning does **not** accompany `ja_nao_existia`: there the gateway deleted nothing and does not
know whether that name ever existed. Saying that a name you never used is burned would be the gateway
inventing a restriction — and this warning's entire value is being a fact with a source.

### ⚠️ "Still in the catalogue" is NOT "it was not deleted"

The same Meta page:

> *"If you delete a template that has been sent in a template message but has yet to be delivered
> (for example, because the WhatsApp user's phone is turned off), the template's status is set to
> `PENDING_DELETION` and WhatsApp attempts delivery for 30 days."*

That is: you will delete, open `GET /v1/templates` and **see the template there**. The natural reading
("it didn't work") is wrong, and acting on it costs repeated work or a ticket about a defect that does
not exist.

That is why the gateway treats `PENDING_DELETION` as an **accepted deletion**, and the response says so
out loud, in a second warning glued to the first: *"este template CONTINUA aparecendo no catalogo com
status PENDING_DELETION … A exclusao FOI aceita; ele sai do catalogo sozinho."*

> 📊 **Documented by Meta, and NOT yet observed in traffic: across 29 real deletions,
> `PENDING_DELETION` did not appear once.** The first cleanup done through this route (consumer
> `consumer-b`, 2026-08-28, `v0.60.0`): 29 names, 29 `apagado` outcomes, and all 29 had already
> **disappeared from the listing** when the catalogue was reread, 1 to 3 minutes later. The account
> balancing is what makes the measurement trustworthy: the consumer's sync copies the `status`
> **verbatim, with no list of known values**, and the set of statuses after the cleanup came out
> `APPROVED: 108`, `REMOVIDO_DA_META: 37`, **others: empty** — a `PENDING_DELETION` would be in
> "others". And 137 − 29 = 108.
>
> **The caveat is worth more than the number, and it comes from whoever measured:** it is not known
> whether `PENDING_DELETION` depends on there being a message in flight with that template. **None of
> the 29 had a recent send, and several had zero messages in their whole life** — it is plausible that
> this is precisely the trigger, and it is the kind of confident explanation nobody here will state as
> fact.
>
> ➡️ **Therefore: keep handling the case.** The behaviour is the one Meta documents, and your code
> cannot assume the template always disappears immediately. What this paragraph adds is the realistic
> expectation — **the common path is disappearing from the listing within a few minutes** — so you do
> not design your cleanup as if `PENDING_DELETION` were the rule.

### The inconclusive `502`, and why it does not say "it was not deleted"

If the call ends **without an answer** from Meta, the gateway rereads the catalogue — immediately and
then in spaced pauses — and reconstructs the outcome: name absent **or** all remaining rows in
`PENDING_DELETION` → `apagado`, with `releituras` and `espera_segundos` in the body saying what it took
to find out. **Only if the template is still there, alive, under another status** does the inconclusive
`502` come out — with the word *inconclusivo*, never "it was not deleted".

The asymmetry is the point: *"I did not see it happen"* is not *"it did not happen"*, and declaring the
second makes you repeat a call that may already have worked. It is the same doctrine, the same word and
the same reread loop as the ambiguous creation, just above.

### How to run a batch cleanup — from someone who has run one

A recommendation from consumer `consumer-b`, who deleted 29 templates through this route on
2026-08-28, on its first real use. None of these is a gateway requirement; all of them cost minutes and
are worth the price in an operation with no undo.

- **Delete ONE first, check the live response, and only then the rest.** Until the first real call,
  your code has only been exercised against the **stub** you wrote yourself — and a stub proves no
  field name. That is how they confirmed that `desfecho`, `entradas[].idioma` and `aviso` arrive with
  the names their command reads. *Choose a harmless template for that first one: the one they used had
  zero messages in its whole life and was six versions behind.*
- **The list comes from a file, and the `desfecho` is what checks your editing.** Their second batch
  started from a list **edited by hand** (removing the name already deleted). If the edit had come out
  wrong, what would have said so was a `ja_nao_existia` — not a silent `200`. That is what the two
  successes have different names for.
- **Stop on the first error, and treat `inconclusivo` as neither an error nor a success.** It is the
  only outcome that calls for a human eye: report the name and move on with the list **without**
  repeating the call.
- **Check the result from YOUR side, not from the command's output.** They reread the catalogue
  afterwards and verified that no family had lost its live version — the command's own output is no
  proof of that.

### What this gateway deliberately does NOT do here

- **There is no daily deletion ceiling.** A ceiling high enough to let a legitimate cleanup of dozens
  through does not hold back a runaway loop that would eat the whole catalogue: it would be calibrated
  by the operator's comfort, not by the failure mode. The brake that works is on your side (a checked
  list, `--dry-run`, confirmation, stopping on the first error), because that is where it is known what
  is in use.
- **There is no boolean confirmation field** (`eu_sei_que_isso_e_irreversivel` and relatives). A
  boolean brake becomes `true` in the caller once and is never thought about again — a brake that only
  brakes on opening night is not a brake.
- **There is no status validation before calling.** Meta documents that *"Templates that are in a
  disabled status cannot be deleted"*; if that is the case, **its** error comes up translated, with a
  subcode, an explanation and an `fbtrace_id`. Guessing Meta's rule in here would trade the real reason
  for a guess of ours.
- **Every deletion is counted** (`templates_apagados`, visible in `GET /v1/estado`) and logged, one
  line per call. An unexpected burst becomes a number, not silence.

## Send and download media

Two routes, both on the **LAN** (the same `:8443` entrypoint as sending) and both restricted to the
instances linked to you.

### Upload bytes: `POST /v1/media?instancia={slug}`

`multipart/form-data` with **one part called `arquivo`**, and the **part's `Content-Type`** declaring
the mime. Success returns `200` and `{"media_id": "…"}` — that is the id that goes into
`POST /v1/messages` with `"tipo": "midia"`.

```
curl -H "Authorization: Bearer <your token>" \
     -F 'arquivo=@nota.ogg;type=audio/ogg; codecs=opus' \
     'https://zapgw.exemplo.com.br:8443/v1/media?instancia=lojinha'
```

**Why this route exists:** without it, whoever has bytes must host a public URL just for Meta to
fetch — and when it does not fetch, the send fails **silently**. That is what a system on this network
had to build before.

The gateway **does not store the bytes**: they cross through by streaming to Meta and what is left is
the `media_id`, which is theirs. No media goes to disk or to a log here.

**The list of accepted mimes and the ceilings, so you do not find out by `415`/`413`.** Both are **the
gateway's, not Meta's** — Meta accepts more than this; we are conservative on purpose, because sending
40 MB to find out it did not fit costs the whole upload. The **category** you declare in
`POST /v1/messages` is the same one that decides the ceiling here, and the part's mime is what decides
the category:

| Category | Accepted mimes | Ceiling |
|---|---|---|
| imagem | `image/jpeg`, `image/png` | 5 MiB |
| video | `video/mp4`, `video/3gpp` | 16 MiB |
| audio (including voice notes) | `audio/aac`, `audio/amr`, `audio/mpeg`, `audio/mp4`, `audio/ogg` | 16 MiB |
| documento | `application/pdf`, `application/msword`, `application/vnd.openxmlformats-officedocument.wordprocessingml.document`, `application/vnd.ms-excel`, `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, `application/vnd.ms-powerpoint`, `application/vnd.openxmlformats-officedocument.presentationml.presentation`, `text/plain`, `text/csv` | 32 MiB |
| sticker | `image/webp` | 500 KiB |

> ℹ️ **The mime is read by its BASE part, and the parameters survive.** `audio/ogg; codecs=opus`
> matches the table's `audio/ogg` row — and the `; codecs=opus` is **neither normalized nor
> discarded**, because it is exactly what makes WhatsApp render a voice note (see obligation 5).
>
> **`image/webp` lives under `sticker`, not under `imagem`** — on WhatsApp, webp is a sticker.
>
> **If the mime you need is not in the table, the way forward is to convert the file** to one of the
> accepted ones before uploading. There is no parameter, header or query that loosens the list.

Refusals that happen **before** any call to Meta:

| Status | Class | When |
|---|---|---|
| `415` | `permanente` | the part's mime is not in the table above |
| `413` | `permanente` | above the **category's** ceiling. The error message says the ceiling in force and says it is the gateway's |
| `400` | `permanente` | the `arquivo` part did not come, or the body is not multipart |

`401`, `403`, `404` and `503` follow the same table as sending.

### Download bytes: `GET /v1/media/{id}?instancia={slug}&mime_do_payload=…`

Returns the **bytes** in the body and **both mimes** in headers:

```
X-Zapgw-Mime-Do-Payload: audio/ogg; codecs=opus   echoed from what YOU sent in the query
X-Zapgw-Mime-Do-Get:     audio/ogg                what Meta's GET /{media_id} reports
Content-Type:            application/octet-stream always — read the two above
```

`mime_do_payload` is optional and comes **from you**, not from a record of ours: the gateway does not
store messages, so whoever has that value is whoever received the event (`midia_mime_payload`). If you
do not send it, the header comes **absent** — the gateway does not copy the other one into its place,
because that would be inventing data with the face of truth. A `mime_do_payload` that is not a valid
mime is refused with `400`.

The `Content-Type` is `application/octet-stream` **on purpose**: putting one of the two mimes there
would be the gateway choosing for you, and anyone reading only the `Content-Type` would take the wrong
choice without ever seeing there were two. The choice is yours, and the next section says why.

#### Download errors

Same taxonomy as sending — **decide by the `classe`, never by the status**. This route talks to Meta
twice (describe the media, then fetch the bytes), and either of the two can fail:

| Status | Class | When |
|---|---|---|
| `400` | `permanente` | a `mime_do_payload` that is not a valid mime (send the `midia_mime_payload` **exactly** as it came in the event); **or** the media's `{id}` has an invalid shape and the request never left the gateway; **or** Meta refused the id — media that does not exist, that is not from your account, or that has already expired |
| `401` | `config` | your `Authorization` is missing or invalid |
| `403` | `config` | the requested instance is not yours |
| `404` | `config` | the **instance** does not exist. Note: a non-existent media id does **not** land here, it lands in the `400` above, because the one refusing it is Meta |
| `502` | `desconhecido` | Meta did not return a usable address for that media, or the gateway could not talk to it |
| `503` | `retentavel` | the instance is paused, the gateway did not read its own storage, **or** Meta returned a retryable error (5xx, timeout, or throttling recognized by Meta's **code** — not by the status; see the note in *POST /v1/messages → Errors*) |

> ⚠️ **The error can arrive in the middle of the bytes, and then there is no error body at all.** The
> bytes travel by streaming: if the connection to Meta drops **after** the `200` and the headers have
> already gone out, the status was already `200` and there is no error JSON for you to classify — the
> response simply **ends early**. The response carries no `Content-Length` (it is `chunked`), so a
> correct HTTP client reports an incomplete read; **do not swallow that exception**, and do not store
> the partial file as if it were whole. Repeating the `GET` is safe: downloading media changes nothing
> on Meta's side.

---

## Business profile — `GET/POST /v1/perfil` (2026-08-20)

**`GET/POST https://zapgw.exemplo.com.br:8443/v1/perfil`** · `Authorization: Bearer <your token>`

It is what your customer sees on tapping the **business name**, inside WhatsApp: description,
address, e-mail, website, vertical. Until this route, that could only be changed through Meta's panel,
by hand. A **LAN** route, like the other instance ones — and WhatsApp-only, see *Routes that refuse
with `400` on an Instagram instance*, further below.

### Read — `GET /v1/perfil?instancia=lojinha`

```jsonc
// response 200
{ "instancia": "lojinha",
  "about": "Loja de roupas femininas",
  "vertical": "RETAIL" }
```

**The gateway returns exactly what Meta sent — it never invents an absent field.** If `email` or
`websites` were never configured, they **do not appear** in the body, instead of coming as `""` or
`[]` — an absent field and an empty field are different facts, and this route does not erase that
difference. `websites` is a list (up to 2 addresses, according to Meta); the rest are plain text.

### Write — `POST /v1/perfil`

```jsonc
// request — ONLY the fields you want to change
{ "instancia": "lojinha",
  "about": "Nova descricao curta" }

// response 200
{ "instancia": "lojinha",
  "gravado": { "about": "Nova descricao curta" } }
```

🔴 **A FIELD ABSENT FROM THE REQUEST IS NOT AN EMPTY FIELD — it is the explicit instruction "do not
touch".** The `POST` sends Meta **only** the fields that came in your body; a field you did not send
keeps the value that was already there. Sending `"description": ""` on purpose **erases** the
description — that is different from not sending `description`, which preserves it. If you want to
erase a value, send the field with `""` (or `"websites": []` to clear the list of sites) — it is a
valid request and goes out exactly like that to Meta; the only thing this gateway never does is invent
that swap on its own.

The accepted fields: `about` (up to 139 characters, according to Meta), `description` (up to 512),
`address`, `email`, `websites` (up to 2), `vertical`, and `profile_picture_handle` (the `media_id`
that `POST /v1/media` returned, to change the profile picture to that upload's content).

⚠️ **This gateway does not check the character ceilings nor the number of sites.** The one who
validates is Meta, and it **explains** what it refused — the same `explicacao_meta`/`rastro_meta` of
T-153, in the next section. Duplicating the number here would only create a second source that would
diverge from Meta the day it changes its own limit.

### Errors

Same taxonomy as the other instance routes — **decide by the `classe`, never by the status**:

| Status | Class | When |
|---|---|---|
| `400` | `permanente` | `instancia` missing in the `GET`, `instancia` missing in the `POST` body, or the body is not JSON |
| `400` | `retentavel` | the `POST` body did not arrive whole (your connection dropped midway) |
| `401` | `config` | your `Authorization` is missing or invalid |
| `403` | `config` | the requested instance is not yours |
| `404` | `config` | the requested instance does not exist |
| `503` | `retentavel` | the instance is paused, or the gateway did not talk to its own storage |
| `502` | `config` | the credential the **gateway** keeps for that instance was refused by Meta |
| `400`/`502` | comes from Meta | Meta refused a field (a character ceiling, an unknown `vertical`, a `profile_picture_handle` that is not an image…) — the response carries `mensagem`, and when Meta sends them, `detalhe_meta`/`explicacao_meta`/`rastro_meta` (T-141/T-153) |
| `502` | `desconhecido` | the gateway got no usable answer from Meta; repeating is safe (reading and writing the profile have no side effect by themselves beyond the requested field) |

✅ **VERIFIED AGAINST THE REAL META (T-157, 2026-08-20, `v0.59.0` live):** the identifier of your
instance that this Graph API endpoint uses is `phone_number_id` — confirmed by a real call
(`GET /v1/perfil?instancia=tenant-one` returned `200` with the complete profile; the wrong identifier
would have returned `404`). That was a doubt in this route's first version (the third-party references
consulted disagreed between `phone_number_id` and `waba_id`) and no longer is — nothing in the
behaviour changed, only the degree of certainty about the node.

---

## Instagram — the first slice (2026-07-30)

Everything in this document, up to here, describes a **WhatsApp** instance. The gateway also serves
**Instagram**, on an instance of **another type** — and the two never mix: an instance is WhatsApp
*or* Instagram, never both, and you know which yours is because you (or the owner) chose the type at
creation.

**What exists today, and nothing beyond it:**

- **Receiving and replying to text messages.** No media, no template, no button, no reaction, no story
  reply, no read receipt. `POST /v1/messages` on an Instagram instance accepts only `tipo:"texto"` —
  any other type gets a `400`.
- **No `POST /v1/cadastro` for Instagram.** A WhatsApp instance can be born with just the slug and be
  configured later, by API, by the consumer (section *Register YOUR Meta account*, above) — Instagram
  **does not have that path** in this slice. Every Instagram instance is created **manually and
  complete** by the owner, with the identification already in place. If you have not received one yet,
  it does not exist.

### What changes in the addressing

An Instagram instance has no `waba_id`, no `phone_number_id` and no `numero_exibido` — they do not
exist on Instagram. In their place:

| WhatsApp | Instagram | What it is |
|---|---|---|
| `waba_id` + `phone_number_id` | **IG ID** (`ig_id`) | identifies the ACCOUNT — who received the webhook, who sends the message |
| a phone number | **IGSID** (Instagram-scoped ID) | identifies WHOM YOU ARE TALKING TO — it is never a phone number, never try to format it as one |

The `IGSID` is the value you receive in `eventos[].de_canonico` (and in `eventos[].de_cru` — the two
come **identical**: an IGSID does not go through the Brazilian phone normalization WhatsApp uses,
because it is not a phone number) and it is the same value you send back in `para`, when sending.

### Receiving a message

The event arrives in the **same format** as a WhatsApp message (`"tipo":"mensagem"`) — you do not have
to learn a new vocabulary. The difference is what comes filled in:

```json
{
  "tipo": "mensagem",
  "id": "msg:IGMID...",
  "wa_message_id": "IGMID...",
  "sub_tipo": "text",
  "de_cru": "179...IGSID",
  "de_canonico": "179...IGSID",
  "texto": "oi, vocês entregam hoje?"
}
```

`wa_message_id` carries the id Meta gave the message on Instagram (the `mid`) — the field name is
inherited from WhatsApp on purpose: both are "the identifier Meta gave this message", and you do not
need one field name per product to recognize that.

This slice **only models text messages**. A message with an attachment (image, audio, story reply,
reaction) arrives in the `cru`, but **does not become an event in `eventos`** — the envelope's
`parse_error` reports that something was left out. If you need those types, ask — the envelope only
grows.

🔴 **The message YOU send also comes back through the webhook — as an ECHO — and it NEVER becomes an
event in `eventos` (T-105).** Meta resends, in the same `messaging[]`, a notification of every message
your own business sends (`message.is_echo: true`,
[developers.facebook.com/docs/messenger-platform/instagram/features/webhook/](https://developers.facebook.com/docs/messenger-platform/instagram/features/webhook/)),
with the sender being **your own `ig_id`**, not a customer's IGSID. The gateway filters that item: it
**does not become a `tipo:"mensagem"`**, but it stays present in the `cru` (base64), whole, like all
the rest of the batch. This exists because, without the filter, an automated system that answers every
received message ends up answering **its own reply** — Meta refuses that send (it does not deliver a
business's message to itself), and each of your replies becomes a send attempt doomed to fail. You **do
not need to filter that on your side**: if a batch carried only echoes, `eventos` arrives empty (`[]`),
never with the echo disguised as a customer message.

### Sending a message

`POST /v1/messages`, exactly as on WhatsApp, with `instancia` pointing to your Instagram instance and
`para` = the IGSID of the recipient:

```json
{"instancia":"<seu-slug-instagram>","para":"<IGSID>","tipo":"texto","texto":"Oi! Fechamos às 18h."}
```

The success response has the same shape as WhatsApp's — `{"wa_message_id":"<id>"}` — for the same
reason as the inbound field: one name for "Meta accepted this and gave it an id", across both products.

🔴 **The 24-hour window — and it is NARROWER than on WhatsApp.** You can only message someone who
wrote to you **in the last 24 hours** (extended to **7 days** if you use the `human_agent` tag — this
gateway does not assemble that tag for you in this slice). On WhatsApp, outside the window you can
still start a conversation with an approved **template**; **Instagram has no templates**, so outside
the window **there is nothing to do** but wait for the customer to write again. If your send is refused
because of the window, Meta answers with a `permanente` or `config` error — the gateway passes its
message on as it came, without inventing a translation that was not checked at the source.

### Prove the channel — `POST /v1/fumaca` / `zapgw fumaca`

It works as on WhatsApp — the instance is born **paused** and only an accepted test send activates it —
but with two honest differences.

**First: the smoke test's `destino` must be an IGSID that has messaged you in the last 24 hours.** The
gateway does not check that before trying (there is no way, short of guessing what Meta will answer);
it attempts the real send and lets Meta decide. If Meta refuses because of the closed window, the
returned error says so explicitly alongside what Meta answered — you are not left with just a generic
error code to decipher.

**Second (T-104): the step "the Graph API accepts the token", which on WhatsApp runs BEFORE the test
send, does not exist for Instagram.** On WhatsApp that step is a `GET` that confirms the token without
spending the test send; for Instagram there is no **measured** equivalent call on that host that asks
the same question without a side effect — inventing one would risk an answer that deceives. This means
that, on an Instagram instance, a revoked token is only reported **in the test send itself** (the same
`erro.classe = "config"` you were already expecting, just one step later) — it never changes what the
final answer says, only when Meta is consulted.

### What does NOT exist, and is not a gap to ask about

- Templates, buttons, media, reactions, location, story replies, read receipts.
- Number quality / messaging tier — a WhatsApp concept, with no equivalent modelled here.
- `GET /v1/estado` still works (it is generic, by slug), but the WhatsApp-specific blocks
  (`token_meta`, `numero_na_meta`) always answer `nao_se_aplica` — **explicitly**, never empty and
  never absent (T-099) — on an Instagram instance. See the table just below.
- 🔴 **The EXCEPTION is `token_instagram` (T-098): it is the block that ONLY exists on the Instagram
  side** — the long-lived token expires in 60 days, with no WhatsApp equivalent (a System User token
  does not expire like that). That is where you watch whether this channel's token will still work
  tomorrow — see the block's own section, above.

### Routes that refuse with `400` on an Instagram instance (T-111)

Six routes use a field that only exists on a **WhatsApp** instance — `phone_number_id` or `waba_id` —
and therefore **refuse with `400`, class `config`**, on an Instagram instance, before attempting
anything with Meta. The message always names the refused type:

| Route | Why it does not apply to Instagram |
|---|---|
| `POST /v1/leituras` | uses `phone_number_id` to mark the message as read — no equivalent modelled here |
| `POST /v1/media` (upload **and** download) | ditto — and this Instagram slice sends no media at all (see above) |
| `POST /v1/templates` **and** `GET /v1/templates` | uses `waba_id` — Instagram has no templates in this slice |
| `POST /v1/cadastro` | stores `waba_id`/`phone_number_id`/`numero_exibido` — **an Instagram instance is configured by whoever operates the gateway**, not by this route (it is the same rule as the section "No `POST /v1/cadastro` for Instagram", above) |
| `POST /v1/bloqueios`, `DELETE /v1/bloqueios` **and** `GET /v1/bloqueios` | uses `phone_number_id` — blocking users is exclusive to WhatsApp's Cloud API (2026-08-20) |
| `GET /v1/perfil` **and** `POST /v1/perfil` | uses `phone_number_id` (confirmed by measurement — T-157, 2026-08-20) — the business profile is exclusive to WhatsApp's Cloud API (2026-08-20) |

`GET /v1/instances/{slug}/health` is the **only exception**, and by deliberate decision: being a
**read** route, it never refuses — a `400` there would break the polling watch of whoever monitors the
channel. On an Instagram instance it answers `200` with `"veredito":"nao_se_aplica"` and **without
calling Meta**: there is no equivalent, on `graph.instagram.com`, to `GET /{phone_number_id}` that
confirms the token without sending a message — inventing one by analogy would risk an answer that
deceives.

#### Which `GET /v1/estado` block applies to which instance type

| block | WhatsApp | Instagram |
|---|---|---|
| `estado` / `pausada` / `versao` / `gerado_em` / `carimbos_desde` / `contadores` / `serie_7_dias` / `serie_diaria` | yes | yes — generic, independent of the Meta product |
| `tipo` | yes — always `"whatsapp"` | yes — always `"instagram"` |
| `ig_id` | **always `nao_se_aplica`** (T-107) — an Instagram identifier, WhatsApp does not have one | yes — this instance's Instagram-scoped Business Account ID, the same value as `zapgw instancia mostrar` |
| `certificado_do_callback` | yes | yes — it is **your** endpoint's TLS, the same in both products |
| `token_meta` | yes, measured every 5 min | **always `nao_se_aplica`** (T-099) — the check measures by `phone_number_id`, which Instagram never has |
| `numero_na_meta` (`qualidade`, `limite_de_mensagens`) | yes, measured/pushed | **always `nao_se_aplica`** (T-099) — quality and tier are WhatsApp Business Number concepts |
| `token_instagram` | **always `nao_se_aplica`** (T-098) | yes — 60-day expiry, see its own section |
| `entrada` (`via`, `conector`, `ultimo_webhook_em`) | yes | yes — it belongs to the **gateway**, not the Meta product: the first two fields are identical on every instance of this gateway (T-120) |

🔴 **`tipo` and `ig_id` came in with T-107 (2026-07-30) and appear ALWAYS, in both products** — the
same blindness T-103 had already fixed in `zapgw instancia mostrar`/`listar` persisted here: without
`tipo`, you had to deduce the product from the absence of the other blocks (`token_instagram
nao_se_aplica` etc.), which is guesswork; and without `ig_id` you saw a healthy `token_instagram`
block **without being able to confirm which Instagram account it speaks of** — it was exactly a wrong
`ig_id` that caused the defect described in the instance rotation section (T-102). `ig_id` is an
identifier, not a secret (the same decision as T-102): the value comes out, never a boolean
`cadastrado: yes/no`.

⚠️ **Not checked in this slice:** whether Meta requires App Review/advanced access for an App to act
on behalf of third-party accounts. That is onboarding for whoever brings the account (the owner,
today) — it does not block your use of the API, but do not assert that Meta will never ask for it.

---

## The consumer's five obligations

zapgw **does not keep message state**. These five things it cannot do for you.

### 1. Store the `cru` BEFORE looking at the `eventos`, and answer afterwards

`cru` is the exact bytes Meta sent. `eventos` is **enrichment** — it can come empty, or partial, with
`parse_error` filled in.

If you process `eventos` before storing, and the processing fails, you lose a message you already had
in hand. It is the defect that has cost most on this network.

### 2. Deduplicate **PER EVENT**, by the `id` field inside the body

> ⚠️ **Do not deduplicate by the `X-Zapgw-Event-Id` header.** It carries the id of the **first** event
> of the batch, as a tracing convenience. A batch whose first event you have already seen, but with
> new events after it, would be **discarded whole** — silently.

The `id` is **deterministic**, derived from the content:

| Event | `id` format |
|---|---|
| mensagem | `msg:{wa_message_id}` |
| status | `status:{wa_message_id}:{status}` |
| template_status | `template_status:{message_template_id}:{event}:{entry.time}` |
| template_categoria | `template_categoria:{message_template_id}:{previous_category}:{new_category}:{entry.time}` |
| qualidade_do_numero | `qualidade_do_numero:{display_phone_number}:{event}:{old_limit}:{current_limit}:{entry.time}` |
| alerta_de_conta | `alerta_de_conta:{entity_id}:{alert_type}:{alert_severity}:{alert_status}:{entry.time}` |

The status key is composite **on purpose**: `sent`, `delivered` and `read` arrive with the **same**
`wa_message_id`. Deduplicating by `wa_message_id` alone would discard two of the three.

**`template_status`'s includes TIME for the opposite reason, and that one is less obvious:** there the
states **repeat** — the same template can be `APPROVED` several times over its life (approved, edited,
back to pending, approved again). Without the time, the second approval would have the id of the first
and your dedup would discard it. See the event's section, further above.

**`template_categoria`'s carries the time for the same reason and the TRANSITION for one more:** a
template can **go and come back** between categories. `UTILITY → MARKETING` and `MARKETING → UTILITY`
are opposite facts (one raises the price, the other lowers it) and without the direction in the key
they would be the same `id`; and the **third** transition (`UTILITY → MARKETING` again) has the same
direction as the first, so only the time separates them — and it is precisely that one that reopens
the appeal window.

**The last two follow the same logic, and exist because those webhooks HAVE NO ID.** There is no
`{wamid}` and no `{message_template_id}` to anchor the key, so it is assembled from whatever
distinguishes one event from its neighbour: in `qualidade_do_numero`, the number + the `event` + the
limit transition; in `alerta_de_conta`, the entity + the type + the severity + the state. **A payload
in which NONE of that is readable does not become an event** — the key would collide with any other
equally empty one in the same batch, and your dedup would erase both. The `cru` arrives all the same,
and `parse_error` reports it.

**Treat the `id` as opaque. Do not parse it.** The format above is informative; the guarantee is that
the same event always produces the same `id`, and different events produce different `id`s.

Determinism is what makes Meta's legitimate redelivery and a malicious resend fall into the **same**
dedup.

> ⚠️ **`SELECT` and then `INSERT` is NOT deduplication. The guarantee has to be ATOMIC.**
>
> ```python
> if Mensagem.objects.filter(wa_message_id=wa_id).exists():   # ← is NOT dedup
>     return
> ```
>
> That pattern is what almost everybody writes on reading the phrase "deduplicate by the `id`", and it
> **fails exactly when it is needed**: two deliveries of the SAME event arriving at the same time both
> pass the `exists()`, and both insert.
>
> **And simultaneous delivery of the same event is not a hypothesis — it is Meta's normal behaviour.**
> Measured in our access log, it redelivers **5 times in 9 seconds** when it does not receive a `200`
> in time: `:02 · :04 · :05 · :07 · :11`. If your processing takes longer than the interval between two
> attempts, they overlap. With more than one worker (process, thread or pod), both run in parallel.
>
> **The right way is to have the database refuse:**
>
> ```sql
> CREATE UNIQUE INDEX CONCURRENTLY uniq_evento_id ON eventos (evento_id);
> ```
>
> …and to treat the uniqueness violation as **success** (`200`), not as an error — it is the second
> deliverer discovering the first one won. An `INSERT … ON CONFLICT DO NOTHING` (or your database's
> equivalent) solves it in the same statement.
>
> **Why this matters more than a repeated row:** the duplicate does not cost one extra row — it
> **re-executes the side effects**. On a real consumer of this network (measured on 2026-07-26), a
> duplicated event would create the message again on screen, forward it again to Telegram and **fire
> another automatic reply to the customer** — besides burning Meta quota, which is limited. The cost
> shows up on a person's phone, not in the database.
>
> **A time-based anti-repeat window does not replace this.** A system on this network had a 60 s guard
> (same message, same recipient) that would absorb Meta's 9 s burst — **but not the +5 min redelivery
> nor the +1 h35 one**, which is precisely when the event comes back.

#### And when the batch has no event at all (`"eventos": []`)? — the key is a DIFFERENT one (2026-07-28)

Obligation 1 requires storing the `cru` **always**, including when the batch arrives with no event at
all — `"eventos": []` on the wire since 2026-07-28 (it was `null` before; see *ACCOUNT webhook*, which
also carries the safe way to iterate). It happens with an unmodelled account webhook, with
`parse_error` filled in, and with any batch the gateway delivered without managing to enrich. Except
that then there is no `eventos[].id`, and the paragraph above does not say which key to store it by.
**This section exists because the intuitive answer is wrong**, and the wrong guidance was actually
given in writing, in the channel, by whoever maintains this gateway — the consumer did not follow it,
went to the code and knocked it down.

**There are two different questions, with OPPOSITE answers, and gluing one onto the other is what
produces the bug:**

| Question | About which bytes | Why |
|---|---|---|
| **Verifying the signature** | the bytes **of the POST body, exact, as they arrived on the wire** | it is over them that the HMAC was computed — see obligation 3, just below. **Nothing here changes that.** |
| **Deduplicating a batch with no usable event** | the **`cru`**, decoded from inside the envelope | the envelope's body **changes between redeliveries of the same event**; the `cru` does not |

**Never hash the envelope's body to deduplicate.** The envelope is **reassembled on each delivery**:
`recebido_em` comes from a clock read made during **that** delivery, and the JSON is serialized right
after. Two deliveries of the **same** Meta event therefore produce **different** bodies — a different
hash, a different `UNIQUE`, a **duplicated row**: exactly what dedup exists to prevent. This is neither
intermittent nor rare; it fails **by construction**, on every redelivery.

**Do this:** decode the `cru` field's base64 and use the hash of those bytes as the unique key
(`sha256` will do). Pick one form — decoded bytes or the base64 string as it came — and **do not
change it later**, or the same delivery gets two keys. Treat the uniqueness violation as success, by
the same rule as the block above.

> ⚠️ **The `X-Zapgw-Event-Id` header is no plan B here: it only exists when there is an event.** The
> gateway only writes it when the batch produced at least one event. In a batch with no event the
> header simply **does not come** — and that is precisely why this case needs a key of its own. (Even
> when it does come, do not deduplicate by it: see the warning at the start of this obligation.)

**The premise that remains, said out loud instead of hidden:** hashing the `cru` assumes Meta
**redelivers the same bytes**. That is **not a guarantee published by them** — this project forbids
asserting as fact what was not checked at the source, and this was not. It is the best option
available, and it is **strictly better** than the envelope: the `cru` only fails if Meta changes the
bytes between redeliveries (not observed); the envelope fails **always**. If a redelivery with
different bytes ever shows up, the symptom is a duplicated raw row in your database — cheap, and the
opposite of what happens today with the envelope, which is already a guaranteed duplicate.

### 3. Verify the signature, and verify the timestamp

**The signature covers the timestamp AND the body.** The computation, literally:

```
signed_message = <X-Zapgw-Timestamp in ASCII> + "." + <POST body, exact bytes>

X-Zapgw-Signature = "sha256=" + lowercase_hex(
                        HMAC_SHA256(key  = your instance's delivery secret,
                                    data = signed_message))
```

Three details that decide whether your computation will match:

- The **body is what arrived on the wire, byte for byte** — never re-serialized JSON. A `json.dumps` in
  the middle reorders keys and changes whitespace, and the signature stops matching with nothing to
  explain why. Read the raw bytes before any parsing.
- The **timestamp is the header's exact string**, not a re-formatted integer. Today it is a unix time
  in seconds, decimal, unsigned and without leading zeros, but concatenate **the text that came** —
  that way your computation keeps matching even if the format changes.
- The separator is a **dot** (`.`), between the two. It exists so that the boundary between timestamp
  and body is unambiguous: in raw concatenation, `("1769000000", "0x")` and `("17690000000", "x")`
  produce the same signed bytes, and therefore the same signature.

#### The test vector — check your implementation against it BEFORE turning anything on

This is the gateway's **frozen** vector, reproduced here in full so you need nothing beyond this page.
**If your implementation produces this signature with these values, it is right; if it does not, the
problem is its own and not the gateway's.**

```
segredo_entrega = segredo-de-brinquedo-do-vetor-de-teste-NAO-E-CREDENCIAL
timestamp       = 1769000000
body (129 bytes, exactly these, with no trailing newline):
{"instancia":"lojinha","recebido_em":"2026-07-26T12:00:00Z","texto":"Olá, João — 50% de ação no caminho C:\\tmp\\nota.pdf"}

expected signature:
sha256=f685419474eed78cfd0458e16057a70317556c154d1d78f06d28f47c87fe35d3
```

🔴 **Read the body as BYTES, not as JSON.** The backslashes there are **two real ones** (`C:` + `\` +
`\` + `tmp`), just as they appear on the wire; if your language requires escaping them again to write
the string literal, escape them — what goes into the HMAC is the 129 bytes above. This vector's
`segredo_entrega` is a **toy**: an invented, public value that opens nothing anywhere. The real one is
drawn at random per instance and is item 4 of your delivery package.

**The body deliberately carries an accent, an em dash (3 bytes in UTF-8) and the backslashes** — that
is exactly where a reimplementation breaks (UTF-8 encoding and JSON escaping), and an ASCII-only vector
would pass green on a wrong implementation. It is an **opaque byte string**, not the envelope's schema:
a new field in the envelope does not change the vector, and the vector does not describe the delivery's
format.

Compare in **constant time** (`hmac.Equal`, `hash_equals`, `crypto.timingSafeEqual`), never with `==`.

Reject an `X-Zapgw-Timestamp` outside a tolerance window of yours (a few minutes is usual, and the size
is your decision — the gateway imposes none). **Now that rejection is worth something:** since the
timestamp is inside the signature, whoever captures a delivery cannot resend it with a new timestamp
without invalidating it, and without your delivery secret cannot re-sign it.

> ⚠️ **BREAKING CHANGE (2026-07-26).** Until then the signature covered **only the body**, and the
> timestamp travelled outside it — the "tolerance" recommended above protected against nothing, because
> it was enough to resend the captured delivery with a new timestamp. Anyone already validating the old
> way (`HMAC(body)`) **starts receiving an invalid signature** and must adopt the formula above.
> **It has been live since `v0.6.0` (2026-07-26). Implement the formula above and only it** — the old
> one no longer exists in any gateway in operation.
> *Until 2026-07-28 this paragraph said that the change "has not yet shipped in a numbered version" and
> that the latest published one was **v0.5.0**, which signed only the body — and told you to check
> against the running binary. **It was false**, and of the worst kind: anyone writing the verifier from
> it would implement `HMAC(body)`, every delivery would fail verification, the contract's own rule says
> to answer `5xx` in that case, and Meta would redeliver for 36 h until **definitive loss** — with
> nobody to ask. Found by an audit conducted under the criterion "can a third party with no channel
> integrate?".*
> The reference implementation, if you want to check yours against it, is `SignDelivery` / the vector
> above itself.

**End-to-end proof, optional:** `X-Hub-Signature-256` carries Meta's original signature over the `cru`
(after decoding the base64). If you have the `app_secret`, you can recompute it and prove the origin
without depending on the gateway. The gateway has already verified it — this pass-through exists for
anyone who wants the guarantee without an intermediary.

### 4. Guard against status regression

`sent` → `delivered` → `read` arrive separately and **can arrive out of order**. The gateway keeps no
state and does not know what has already passed. **Never let an earlier state overwrite a later one.**

🔴 **And do NOT order by Meta's `timestamp` — order by the state's PRECEDENCE** (`sent` < `delivered`
< `read` < `failed`), keeping the most advanced one that has arrived.

*This obligation said "use the event's `timestamp` to order" until 2026-07-28, and **flatly
contradicted the 🔴 warning in this same document** (see the status section): `sent` and `delivered` of
the same message arrive with the **same** `timestamp` — measured in real traffic, with both fixtures
frozen (`status_sent_com_pricing.json` and `status_delivered.json`, same `wa_message_id`, `timestamp`
`1785072102` on both). Anyone following this section would build exactly the defect the warning exists
to prevent, and it is **invisible**: no error anywhere, just the screen showing `delivered` before
`sent` half the time. The contradiction was in the NORMATIVE section, which is where an integrator
takes their checklist from — that is why it weighed more than the warning.*

### 5. When RE-SENDING media, use the `mime_do_payload` — never the `mime_do_get`

**This is your obligation, and the gateway cannot take it on.** Meta reports the **same** media with
two different mimes:

| Where | Example |
|---|---|
| in the message's event (`midia_mime_payload`) | `audio/ogg; codecs=opus` |
| in `GET /{media_id}` (`X-Zapgw-Mime-Do-Get`) | `audio/ogg` |

It is the `; codecs=opus` that makes WhatsApp render a **playable voice note**. Re-sending with the
other one delivers a **file attachment** — and the message **arrives**, with no error anywhere: not in
Meta's response, not in the status webhook. Only the end customer sees the difference, and they are not
going to tell you it was supposed to be playable audio. **Cost paid in production on this network on
2026-07-20.**

That is why the gateway returns **both, named, normalizing neither and choosing neither**: normalizing
would destroy exactly what needs to be preserved, and choosing would take from you the one decision
only you can make. Keep the `midia_mime_payload` that came in the event; when uploading back, declare
it **whole** in the `Content-Type` of the `arquivo` part.

---

## What you answer, and what that causes

**zapgw mirrors your status back to Meta.** The rule it applies:

| You answer | Meta hears | What happens |
|---|---|---|
| **2xx** | `200` | Done. **Meta never resends this event.** |
| **5xx** | `502` | Meta resends. Use it when a resend **would** solve it (your database went down). |
| **4xx** | `200` + alarm | Meta does **not** resend. Use it when a resend would **not** solve it. |
| no answer / too slow | `504` | Meta resends, if you come back in time. |
| TLS certificate refused | `504` + alarm | Meta resends — but the resend gets the **same** refusal. Only a human fixes it, and the alarm says so. |

### ⚠️ A credential or configuration error answers **5xx**, never 4xx

This is the rule that costs most to find out on your own, and the table above explains why.

**An invalid signature, a missing secret, the wrong instance, an unconfigured key — answer `5xx`.**
The `4xx` is reserved for **a defect of form that resending does not fix**: a body that is too large
(`413`), invalid JSON or base64 (`400`).

The reason is counter-intuitive and worth writing out in full. When you answer `4xx`, the gateway says
`200` to Meta — and **it never resends**. If the cause was a divergence of secret or of formula
between you and the gateway, **you have just destroyed a real customer's message** over a configuration
problem that would have been fixed in minutes.

And you **do not find out on your own**: on your side the log says *"I refused a request with an
invalid signature"*, which looks exactly like the correct behaviour. The symptom appears weeks later,
as "that customer never replied".

*"But isn't an invalid signature a malicious request?"* — almost never, and the asymmetry decides:

- A `POST` **forged by a third party** does not reach the mirror. The answer goes to the attacker, and
  Meta does not even take part. You lose nothing by answering `5xx` to them.
- A **real** delivery only fails verification through a divergence between the two sides. There the
  `5xx` keeps the 36 h window open and gives someone time to fix it.

That is: `5xx` costs **zero** against the attacker and **saves** the legitimate message. `4xx` does not
hinder the attacker and **burns** the legitimate one.

> **The wrong instance is the family's most serious case.** If another tenant's `callback_url` points
> at you by mistake, answering `4xx` destroys **a third party's data** for good, and answering `2xx`
> tells Meta that a message that is not yours is settled — its owner never receives it. Check the
> envelope's `instancia` field against the instance you expect, and answer `5xx` if it diverges.

**Answering 2xx without having stored it is definitive loss.** There is no second chance, no
reprocessing, and the gateway keeps no copy.

**Meta's 36 h of resends are not a safety net for a long outage** — they cover a restart of seconds. If
you are down for hours, the event is lost and the gateway **does not even find out**.

Answer quickly: the delivery deadline is configurable per instance, and exceeding it counts as "did not
answer".

---

## Known limits, so they are not discovered in production

- **Each instance needs its own webhook URL at Meta.** If one App delivers, in a single `POST`, events
  from numbers belonging to different instances, the whole batch is refused (`200` + alarm). Meta
  supports a webhook override per WABA and per number.
- **Template status and account webhooks are not routed by number** — Meta does not support an override
  for them and does not send `phone_number_id`. They are routed **by `waba_id`**, and that guard exists
  and is live: an account webhook whose `waba_id` is not your instance's **does not reach you** (see
  *ACCOUNT webhook*). What remains a limit is the origin: since Meta delivers all of them to the App's
  main URL, it is the gateway that has to decide whose each one is.
- **A body above the ceiling is refused with `413`, and the ceiling is the SAME in both directions.** A
  single limit (`ZAPGW_MAX_CORPO_BYTES`, **default 1 MiB**, adjustable by whoever operates) applies to
  the body **Meta** sends in the webhook **and** to the body **you** send in `POST /v1/messages`,
  `/v1/cadastro`, `/v1/leituras`, `/v1/templates`, `/v1/fumaca`, `/v1/pausa` and `/v1/bloqueios`
  (`POST`/`DELETE`). Above it the response is `413` class `permanente`, and repeating never solves it —
  shrink the request.
  **Media does not use this ceiling**: the upload has per-category ceilings (the table in *Upload
  bytes*), which are larger.
  🔴 **In the INBOUND direction the `413` costs a message.** The gateway answers `413` to Meta and
  **delivers nothing**; it resends the same body, gets `413` again, and when it gives up the event is
  over — there is no copy and no reprocessing. On your side the only symptom is **silence**: a message
  that never arrives. Repeated, that is not an accident, it is the ceiling being too low for what that
  instance receives; from **3 refusals on the same instance within 1 h** the gateway writes an `ALARME`
  for whoever operates it (at most one per hour, so it does not become noise).
- **The template catalogue has a pagination limit, and it is an ERROR, not a cut.** The gateway reads
  up to **50 pages of 100** (~5000 templates); the real WABA checked on 2026-07-25 has 39. If that
  limit is ever reached, you receive a `502` and **no list** — never a short list with a `200`. A
  silent short list is the defect that took the old gateway out of production.
- **`midia_mime_payload` comes raw, with its parameter.** It is the `; codecs=opus` that makes WhatsApp
  render a voice note; the mime from `GET /{media_id}` is different and poorer. Do not normalize it —
  and see obligation 5, which is where that difference charges.
- **The mime list and the upload ceilings are the GATEWAY's, not Meta's.** A file refused with `415` or
  `413` here may be perfectly acceptable to them: we chose to be conservative because sending and
  finding out afterwards costs the whole upload. The table is in *Upload bytes*, and the way forward
  for a mime that is not in it is to **convert the file**.
- **Your instance's `timeout_ms` is not exposed on any route.** Default 5000 ms. The practical rule is
  in *What you receive when you are provisioned*: give your HTTP client a deadline comfortably above
  it.
- **`de_cru` is what Meta sent; `de_canonico` went through the ninth-digit canonization.** Always
  compare by the canonical one — Meta does not guarantee the same spelling you registered.
- **The gateway retains your counterparties' phone numbers for up to 30 days, in the clear (owner's
  decision, 2026-07-30).** It is the internal transit log (`zapgw transito` / `zapgw log`, with no HTTP
  route — you have no access to it), used only to answer "did this message pass through here?" when a
  specific message must be identified. The deadline is `ZAPGW_TTL_TRANSITO_DIAS` (default 30 days).
  **What is stored:** the timestamp, the instance, the counterparty's phone number, the direction, the
  event type, the outcome, the **`wamid`** (the id Meta gave the message) and an **HMAC of your
  `Idempotency-Key`** — the key in the clear never enters the database. **What is NOT:** content —
  neither text, nor name, nor caption, nor the raw body.
  ⚠️ **The `wamid` carries the recipient's phone number encoded inside it.** That is, your customer's
  phone number is in this table in **two** places, not one — both under the same purge deadline.
  *(This sentence was added on 2026-08-18: both columns had existed since 2026-07-30 and the list above
  did not mention them. The behaviour did not change; it was the description that was incomplete — and
  in a paragraph about personal data retention, incomplete is wrong.)*
  ⚠️ **These are YOUR customers' phone numbers, on a machine that is not yours.** If your operation has
  its own retention or erasure obligation over that data, factor in this extra window on the gateway's
  side when sizing it.

---

## Deprecation policy — by DEADLINE, not by consensus

When a field is succeeded by another, the old one is marked **OBSOLETE** in the section that describes
it, **with the date of the marking**. From then on a single rule applies:

> 🔴 **A field marked obsolete keeps working for at least SIX MONTHS from the date of the marking.**
> The removal, when it happens, becomes an entry in *Breaking changes*, with a date and migration
> instructions.

**Six months, and the number is written on purpose.** "Some time" is not a deadline: it leaves you
reading "OBSOLETE" without knowing whether you can keep using it tomorrow. Six months is longer than
any integration cycle this gateway has observed, and short enough not to become "compatible forever" —
which is the fate of every field whose removal never has a date.

**The minimum date is written next to each obsolete field.** Today there are two:

| Field | Where | Marked obsolete on | Removable from | Successor |
|---|---|---|---|---|
| `dia` | each entry of `serie_7_dias` and `serie_diaria` | 2026-07-28 | **2027-01-28** | `dia_utc` |
| `serie_7_dias` | top level of `GET /v1/estado` | 2026-07-29 | **2027-01-29** | `serie_diaria` |

⚠️ **"Removable from" is not "will be removed on that date".** It is the **floor**: before it the
removal does not happen, after it it can happen at any moment and is announced only here. Migrate
before the floor and the date stops mattering to you.

**Why a deadline and not the old form.** Until 2026-07-28 the rule was *"remove when all the consumers
confirm in writing"* — and it worked once, with `botoes_url`, because there were **two** consumers,
both known and both in the same conversation. With N outside integrators, that condition **never
closes**: someone is always missing, and the obsolete field becomes permanent while the reader is left
not knowing what to do. A deadline has the opposite defect, which is the cheap one: it may remove
something someone was still using — but that person had six months and a written date.

**What does NOT go through this policy:** a **new** field. It is additive, it appears on its own and it
demands nothing of you.

---

## Breaking changes

**This section is the mechanism promised in place of versioning the format.**

The normal guarantee is that **the envelope only grows**: a field never disappears, is never reused
with another meaning, and every new field is omitted when empty. That is what makes a version number
unnecessary — and it is stronger than a number, because a version number invites the consumer to branch
(`if version >= 3`), and then the gateway's format becomes part of their logic.

**But sometimes it breaks.** When it breaks, it appears here: **date, what changed, what to do.** There
is no notice through any other channel, there is no announcement list and there is no endpoint
declaring a version — this file is versioned in git and has a history, and it is what you read.

🔴 **That gives you one obligation, and it is a small one: reread this section before rolling out a new
integration, and periodically thereafter.** There is nobody to come looking for you. The entries stay
here forever, in date order, and the one at the top is the oldest — glance at the date of the last one
you had already read.

### 2026-07-26 · The delivery signature started covering the TIMESTAMP

**What changed:** `X-Zapgw-Signature` was `HMAC(secret, body)`. It became
`HMAC(secret, timestamp + "." + body)`.

**Why it broke instead of growing:** the timestamp travelled in a header **outside** the signature, so
the tolerance window this contract requires you to implement protected against nothing — it was enough
to resend the captured delivery with a new timestamp. There was no way to fix that additively: either
the computation covers the timestamp, or it does not.

**What to do:** redo the computation as in the section *Verify the signature, and verify the
timestamp*. The frozen test vector is reproduced in obligation 3 — redo it in your language before
trusting your implementation.

**When:** done on the same day the only consumer of the time was implementing the verification, on
purpose. The window to fix it for free would not come back.

### 2026-07-28 · An unreadable block stops bringing down the message — and `responder_a` starts going missing on its own

**What changed:** when `messages[].context` (or `voice`, inside a media block) arrives from Meta with a
**type** different from the expected one — text where an object is expected, a number where text is
expected, text where a boolean is expected — the gateway now **discards only that block** and delivers
the message. In practice: the message starts arriving in `eventos` **without** `responder_a`,
`encaminhada`, `encaminhada_muitas_vezes` (the `context` block) or **without** `voz` (the media block).

**What happened before:** the **whole** message was discarded from the `eventos` list and counted as a
parse error. You still received the `cru` and the filled-in `parse_error` — but this contract tells you
to act on `eventos` and deduplicate by `eventos[].id`, so, for any consumer following the contract,
**the customer's message simply did not exist**. There was no alarm and no counter: only a line in the
gateway's journal.

**Why this is a "break" and not growth:** the envelope neither gained nor lost a field — what changed
is **when** a field appears. A consumer who wrote, even without noticing, "if the message arrived in
`eventos` and is a reply, then it has `responder_a`" now sees a new case: message present, field
absent. It could not be done additively — either the message survives the unreadable block, or it does
not.

**What to do:** nothing, if you already treat `responder_a` and `voz` as optional — and this contract
always required treating them so (an absent `responder_a` is the **normal** case, see its section; an
absent `voz` means "Meta did not say", never `false`). If any point of your code assumes presence, that
is where the change shows up. **It cannot be observed with a well-formed payload**: no normal traffic
changes behaviour.

**Why it is the right change even being a break:** a message delivered without an accessory field is
infinitely better than a lost message — and loss here is **definitive**, because the gateway answers
`200` to Meta and it does not resend.

**When:** done before any observed loss. The defect was invisible by construction (it produces absence,
not an error), so waiting for evidence was waiting for nothing.

### 2026-07-28 · The rule above starts applying to EVERY block of a message, not just `context` and `voz`

**What changed:** the previous entry describes the same behaviour applied to **two** fields. It now
applies to **all the blocks** of a message — `text`, `button`, `interactive`, `audio`, `image`,
`video`, `document`, `sticker`, `reaction`, `location`, `errors`, `context`, plus `from`, `type` and
`timestamp`. A block arriving from Meta with a **type** different from the expected one is discarded on
its own, and the message arrives in `eventos` without it.

**What happened before:** exactly the same defect as the previous entry, on the other thirteen fields —
measured, not assumed: `"text":"oi"`, `"audio":"x"`, `"interactive":"x"`, `"reaction":"x"` and
`"button":"x"` made the **whole** message vanish from `eventos`. **`text` is the case that matters to
you:** it is the most common message type of all, and a `text` of an unexpected shape erased the most
ordinary message in the system.

**Why it is a break and not growth:** the same reason as the previous entry — the envelope neither
gains nor loses a field, what changes is **when** a field appears. New cases you may observe and did
not before: a message with `sub_tipo: "text"` and **without** `texto`; with `sub_tipo: "audio"` and
**without** `midia_id`; with `sub_tipo: "reaction"` and **without** `reacao`; with
`sub_tipo: "button"` and **without** `botao_payload`.

**What to do:** treat every envelope field as optional, including those that "always come" for a given
`sub_tipo`. If your code does `evento["texto"]` without checking, that is where the change shows up.
**No well-formed payload changes behaviour** — as in the previous entry, this is not observable in
normal traffic.

**What did NOT change, and it is the only exception:** a message **without a readable `id`** still does
not become an event (it comes in the `cru`, with `parse_error`). Without the `wamid` there is no dedup
key, and an `id` that arrives as a number is **not** converted into text — inventing a wamid would send
you replying to a message that does not exist. Also still discarded are the `reaction` Meta sends
**without a target** and the `location` it sends **without the object** (or with it `null`): there it is
Meta asserting that the block does not exist, which is different from the gateway not knowing how to
read it.

**When:** the same day as the previous entry, and that is precisely the point. The first round closed
two fields; this one closes the class, and leaves a test that goes red the day a new field is born
unprotected. Recorded with its cost in the gateway's trap log.

### 2026-07-28 · `eventos` started coming out as `[]` on the wire — before it was `null` when the batch had no event

**What changed:** a batch with no event at all came with `"eventos": null`. It started coming with
`"eventos": []`. The field is still always present, and nothing else in the envelope changed.

**What happened before, and for how long:** from the gateway's very first day (2026-07-23) until
2026-07-28, every envelope with no event carried `null`. And the case was not rare: the App was
subscribed to ten webhook fields and the gateway modelled only some of them, so **every unmodelled
account webhook** arrived that way, routinely. This contract promised `[]` in writing the whole time,
in five places — anyone who followed the text to the letter wrote `for ev in envelope["eventos"]`,
which blows up with `TypeError: 'NoneType' object is not iterable` in Python, or `if eventos == []`,
which never matches.

**Why it is a "break" and not growth:** the envelope neither gained nor lost a field — the **type** of
the value changed in one case. There was no way to do it additively: either the field is an array, or
it is `null`. A second field (`eventos_sempre_array`) would be two ways of saying the same thing, which
is the debt this project had just decided not to let be born in another field (see `botoes_url`).

**And it is the safest possible break, which is what decided the direction:** code that tolerated
`null` (`envelope.get("eventos") or []`, `?? []`) keeps working with `[]`, because `[]` is also falsy
in Python and iterates zero times in any language. Code that broke started working. The only pattern
that switches branch is an explicit `if eventos is None`, and the new branch iterates zero times.
**There is no consumer to whom `[]` costs anything** — the perverse detail is the opposite: the change
fixes whoever read the old contract and does not bother whoever read the wire.

**What to do:** nothing, and **do not undo the defense you already have**. `or []` / `?? []` is still
the recommended form here.

> 🔴 **One case deserves a check, and it is not hypothetical — it is the code of a real consumer on
> this network.** If you BRANCH by type instead of iterating — `if isinstance(eventos, list): …` /
> `Array.isArray(eventos)` — the batch with no event **switches branch**: before, the `null` fell into
> the "unusable, store the `cru` under a key derived from it" branch; now `[]` is a list and falls into
> the `for` branch, which runs zero times. **If the `for` branch does not store the `cru`, the batch
> now leaves no trace at all** — which is exactly the silent loss obligation 1 exists to prevent, with
> the sign flipped. Check that "empty list" and "no usable events" lead to the same destination in your
> code, and pin it with a test. A guard that treats `null` as a special case (a log, an alarm, a
> metric) also stops firing — the desired result, but worth knowing before the silence surprises you.

**When:** the two consumers that existed at the time were warned in writing on **2026-07-28 15:12**,
with the instruction to defend themselves **before** the wire changed; both answered the same day
confirming the defense in their code, and only then was the change made. The order was that way on
purpose: changing first and warning afterwards would have turned a safe fix into a real break for
anyone who had branched on `is None`. *(Direct warning like that is only possible while the readers fit
in one conversation. Do not count on it: the mechanism that applies to you is this section.)*

### 2026-07-28 · The same rule rises from the MESSAGE to the BATCH — and an account webhook without a readable `waba_id` starts being refused

**What changed, and there are two things with opposite directions:**

**(1) The batch stops dying along with it.** The two entries above apply within a message. They now
also apply at the levels that **contain** the message: `entry`, `changes`, `value` (`metadata`,
`contacts`, `messages`, `statuses`), the change's `field`, the **status** event and the **template
status** event. A field of unexpected type at any of them degrades only what it describes.

**What happened before** — measured with the parser, not assumed: a `"contacts":"x"` or a
`"metadata":"x"` erased **all the messages AND all the statuses of that `change`**; a `field` of
unexpected type, ditto; an `entry.id` of unexpected type erased the whole `entry`; and a single odd
field in one item of `statuses[]` or in the `value` of a `message_template_status_update` erased that
event. You received the `cru` and the `parse_error`, and `eventos` came empty or short.

**New cases you may observe:** a message event **without** `phone_number_id` (unreadable `metadata`
block); **without** `nome_contato` (unreadable `contacts` block); a status event **without** `status`
or **without** `para`/`para_canonico`; a template event **without** `nome`/`motivo`. All with the rest
of the event intact, including the dedup `id`.

**(2) An ACCOUNT webhook without a readable `waba_id` stops being delivered.** If `entry.id` arrives in
a format the gateway cannot read, an account webhook (`message_template_status_update` and the other
`field`s that are not `messages`) is **refused whole** — no `eventos`, no `cru`. Before, it was
delivered. On your side, that is indistinguishable from "the webhook did not arrive"; on ours, an
`ALARME` comes out in the journal and the `conta_descartada` counter rises (visible in
`GET /v1/estado`).

**Why it could not grow instead of breaking:** the `waba_id` is the **only** routing key an account
webhook carries, and it is by it that the gateway proves that webhook belongs to your instance. Without
it readable there is no proof. Delivering anyway would put, in your database and for good, the content
of an account nobody could attribute — and the `cru` body travels along in the delivery, so filtering
only the `eventos` would not solve it: it would be a defense that only looks like one. Losing an
account notice is recoverable and is **announced**; storing someone else's data in your database is
neither.

**What to do:** nothing, in both cases. **(1)** is already the rule this contract has repeated since
the previous entry — treat every field as optional, including in a status event. **(2)** has no action
on your side: no well-formed payload is affected, and if it happens, we are the ones who act.

**When:** the same day as the two entries above. The three are the same sentence applied at three
heights of the tree, and the third only existed because the second measured the neighbours instead of
declaring victory. Recorded with its cost in the gateway's trap log.

### 2026-07-28 · The `botoes_url` field was REMOVED — use `botoes_template`

**What changed:** `botoes_url`, the parameter field for a template's URL button, **no longer exists**.
A request that still sends it receives a `400` with the named error `botoes_url foi removido; use
botoes_template` and the translation inside the message itself. **The URL button went nowhere** — it is
`{"tipo": "url"}` inside `botoes_template`, and the component that goes out to Meta is the same byte for
byte (frozen in a test).

The translation is mechanical and is the whole migration:

```jsonc
// before
"botoes_url":      [ {"indice": 0,               "texto": "BR123456789BR"} ]
// after
"botoes_template": [ {"indice": 0, "tipo":"url", "texto": "BR123456789BR"} ]
```

**Why it could not grow instead of breaking — and here the honest answer is that it COULD, and we chose
not to.** Keeping both fields cost little in code and a lot in contract: two ways of saying the same
thing, and an **invalid state remaining expressible** — the same button declared in both fields, with
the same index. It was refused by a cross-check, but *refused is not inexpressible*: the guard depended
on whoever touched that code remembering it, and this project uses a discriminated union precisely so
as not to depend on that. With a single field, the index became **structural** again — one list, one
index space, nothing to remember.
**And the other side of the accounting is the date:** the field was **two days old**, and the cost of
coordinating a removal with two known consumers will never be lower than it was that day. Leaving it
for later would have turned "compatible for now" into "compatible forever" — the formulation is from
the very consumer who volunteered to leave early.

**Why `400` and not silence, which is the decision that matters most to you:** a removed field that
were simply ignored by the deserializer would send the template **without the button**, with `200` in
the response. You would have no signal at all — the one who would find out would be your customer, on
their phone, with an incomplete template, and the charged conversation would already have been burned.
The two possible errors have prices of different orders: ignoring costs a wrong delivery and your trust
in the number on your screen; refusing costs a deploy. **When the asymmetry is that, the doubt is
resolved on the cheap side.**

**What to do:** change the field's name and add `"tipo": "url"` to each item, as in the block above.
Nothing else changes — not the index, not the text, not the component Meta receives, not the
**idempotency hash** (the field was `omitempty`, so anyone already using `botoes_template` has the same
hash as before and no retry within the 72 h TTL turns into a false `422`).

**When:** the removal had **no deadline**. The field was two days old and there were two consumers, both
known, and it stayed blocked all day waiting for both to confirm **in writing** that they no longer used
the field — each citing the line in their own code, not their memory. One of them closed after three
conditions demanded in this order: PR merged, deploy **verified** (not presumed) and **button clicked on
the device** with the portal opening; the other had zero usage, because their client was born on
`botoes_template`.

> 🔴 **That form is NO LONGER the policy, and the difference affects you.** Waiting for everyone's
> confirmation only closes when "everyone" is a short, known list — with N outside integrators it never
> closes, and you would be left reading "OBSOLETE" without knowing whether you can keep using it. **The
> current policy is a deadline**, and it is written in *Deprecation policy*, further above. This
> paragraph is a record of what happened that day, not an instruction.

> ⚠️ **A warning that came from a consumer and applies to anyone checking their own code:**
> `botoes_url` may be an **internal name of yours**, with no relation to this field. That is what
> happened with one of them: a `grep` for the name in their repository found **8 occurrences** of a
> local helper, and anyone stopping there would conclude they used our field — they never did. **What
> decides is not the `grep` for the name: it is what comes out in the request's JSON.** Same family as
> `wa_message_id` × `wamid`.

---

**Format for the next entry:** date · what changed · **why it could not grow instead of breaking** ·
what to do · when. The second is the one that matters: if it can grow, grow it — breaking is always the
second option, and this section exists to make that choice visible instead of convenient.
