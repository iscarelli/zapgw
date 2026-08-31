# Onboarding a number at Meta (WhatsApp Cloud API)

*[Leia em português](ONBOARDING-META.pt-BR.md)*

**Code:** the values collected here become a row in the `instancia` table — the fields are in
`internal/config/store.go` (`type Instancia`). This doc describes **how to obtain them**, not the
schema.

> **Doctrine warning.** Meta changes this process often, and this project forbids stating as fact
> what has not been checked at the source. The **steps** and the **dependencies** below are stable
> and are what matters for planning. The **specific numbers** (tier limits, exact deadlines, whether
> App Review is required) are marked *[CONFERIR na fonte]* — confirm at
> `developers.facebook.com/docs/whatsapp` and in Business Manager **on the day**, not from this page.

## What verification blocks, and what it does NOT block

**Business verification is NOT a gate to start sending.** There is an **unverified** tier: a
registered number sends right away, limited to roughly **250 business-initiated conversations per
24 h** *[CONFERIR o número vigente e o que conta como "conversa"]*. **Business Verification** (plus an
approved display name and quality kept up) **unlocks larger tiers** (1k, 10k, 100k…) — it is a gate
to **scale**, not to **launch**.

Consequence for planning — and it is the opposite of what an earlier version of this doc said: **the
critical path to the first message is NOT Meta; it is us.** At the low volume `consumer-c` starts
with, 250/24 h is roomy, so what stands between the gateway and working is the **deploy** (plan 4)
and the **number registration** — both ours. Verification runs in parallel, as an "unlock scale"
track, and only becomes a bottleneck when volume approaches the unverified ceiling.

Three points that decide how roomy the 250 ceiling is, and that this doc does **not** assert without
checking:
- is it only for **business-initiated** conversations (template/marketing)? Replying to someone who
  messaged first, within the service window, may **not** count — which would matter a lot for a
  mostly transactional gateway. *[CONFERIR]*
- does the **display name** need approval for the number to leave its initial state, or only to look
  good? *[CONFERIR]*
- the exact number and the window (rolling 24 h? per number? per WABA?). *[CONFERIR]*

## Who fills in each field: you, or the consumer (T-079, 2026-07-28)

**This document describes onboarding a number in the Meta account OF WHOEVER OPERATES THE GATEWAY.**
When the Meta account belongs to a **third party** — the case the gateway started supporting in
T-079 — the phases below still hold, but the one carrying them out is **them**, in their account, and
the one typing the values into the gateway is also them:

| Path | Who creates the instance | Who fills in `waba_id`, `phone_number_id`, the number, `app_secret`, `token_envio`, `callback_url` |
|---|---|---|
| **owner's** Meta account (own production, lab) | the owner, with `--waba-id …` and the other flags | the owner, in the same command (or via `zapgw instancia rotacionar`) |
| **consumer's** Meta account (third party) | the owner, **with `--slug` only** | **the consumer**, via `POST /v1/cadastro`, within the 24 h window |

On the second path the owner **does not know, does not store and does not ask for** those values —
and the command **does not draw** an `app_secret` or a `token_envio` (drawing one would make
`zapgw instancia mostrar` say `app_secret=sim` about a value the consumer's Meta has never seen; see
`docs/ARMADILHAS.md`). What the owner hands over is in `docs/CONTRATO-CONSUMIDOR.md`, *"O que você
recebe ao ser provisionado"*.

**`verify_token` and `segredo_entrega` are still drawn and printed by the gateway on both paths** —
they belong to the gateway, not to anyone's Meta. On the third-party path, they are part of the
delivery bundle.

## What each instance field needs, and where it comes from

| Field (`Instancia`) | What it is | Where it comes from in onboarding |
|---|---|---|
| `PhoneNumberID` | the number's id in the Graph API (**not** the phone number) | appears in the WhatsApp panel after the number is registered |
| `WabaID` | id of the WhatsApp Business Account | Business Settings → WhatsApp accounts |
| `DisplayNumber` | the phone number itself, as displayed | the number you register |
| `AppSecret` | the Meta App's secret; **verifies the HMAC** of the inbound webhook | App → Settings → Basic → App Secret. **Third-party account: they register it themselves, via `POST /v1/cadastro`** |
| `VerifyToken` | arbitrary string; answers the webhook verification GET | **the CLI draws and prints it** (or you pass it in `ZAPGW_VERIFY_TOKEN`), and you repeat the value in the webhook config at Meta |
| `SendToken` | **permanent** System User token; talks to the Graph API | Business Settings → System users (see Phase 4). **Third-party account: they register it themselves, via `POST /v1/cadastro`** |
| `CallbackURL` | where zapgw delivers to the consumer | **not** Meta's — it is the consumer system's |
| `DeliverySecret` | signs zapgw's POST to the consumer | **the CLI draws and prints it** (or you pass it in `ZAPGW_SEGREDO_ENTREGA`), and the consumer puts the value in their `.env` |

The first three are **identifiers** (not secrets). `AppSecret` and `SendToken` are **secrets** and
follow the project rule: never in Git, transported via `C:\dev\github\secrets-transfer\`.

### Two of these secrets are SHARED, and that is why the CLI shows them (T-052)

`zapgw provisionar instancia` draws every secret that does not arrive via an environment variable.
For `app_secret` and `token_envio` it says only **which** ones it drew, never the value — nobody
needs to read them back.

`verify_token` and `segredo_entrega` are different: provisioning **only finishes when the value
reaches a person** (you type the first into Meta's panel; the consumer puts the second in their
`.env`). So **when they are drawn, the command prints them once**, with the warning that the gateway
keeps only the encrypted form and does not show them again:

    GUARDE AGORA os valores abaixo — o gateway guarda so o cifrado e NAO os mostra de novo.
    verify_token: <64 hex>
    segredo_entrega: <64 hex>

*(That is the emitted output, in Portuguese: "SAVE THE VALUES BELOW NOW — the gateway keeps only the
encrypted form and does NOT show them again.")*

**What used to happen, and it is the reason this section exists:** both were drawn silently. The
instance was born, `zapgw instancia mostrar` said `verify_token=sim segredo_entrega=sim` — it looked
complete — and it was **impossible to finish provisioning**, because nothing is decrypted back by any
command. With no error pointing at the cause: the symptom showed up days later, in Meta's panel, as
*"a verificação recusa"* ("the verification is refused"), which sends you looking in the wrong place.
It cost one extra rotation in T-046 (2026-07-28).

**If you pass the value through the environment, nothing is printed** — you already have it. That is
the production path.

**If you lost the value**, there is no recovery: generate another one and swap it with
`ZAPGW_VERIFY_TOKEN=<new> zapgw instancia rotacionar --slug <slug>` (`rotacionar` does **not** draw —
the value comes from the environment, precisely so whoever rotates knows what it is).

## The phases, in order, with what blocks what

### Phase 1 — Account and App *(fast, depends on nothing of ours)*
1. Have a **Meta Business account** (`business.facebook.com`).
2. Create a Business-type **Meta App** (`developers.facebook.com`) and add the **WhatsApp** product.
   That already grants Cloud API access on the unverified tier.

> **No step of this phase needs zapgw ready or up.** It is fast — it is not the long stage.

### Phase 1b — Business verification *(long, PARALLEL, does not block launch)*
3. Submit **Business Verification**: Meta checks the business's legal existence (company document,
   address, verifiable phone number). It is the longest stage, but it **unlocks scale**, it does not
   enable sending — sending already works on the unverified tier without it.
   *[CONFERIR: documentos exigidos e prazo — variam por país e mudam.]*

> Start this one in parallel, but do **not** wait for it to put the gateway up at 250/24 h.

### Phase 2 — Number and display name *(fast registration; name review separate)*
4. Add a **phone number** to the WABA. The number **cannot** be active on a regular WhatsApp or on
   the WhatsApp Business app — if it is, it has to be unlinked first. It needs to receive an
   SMS/call for the verification code.
5. Set the **display name**, which goes through **Meta review** and can be **refused** on policy
   grounds. *[CONFERIR a política de nome vigente antes de submeter — recusa recomeça a espera.]*
6. **Payment method** on the WABA: the Cloud API charges per conversation above the free tier.
   *[CONFERIR a faixa/tarifa vigente.]*

### Phase 3 — Permanent production token *(fast, but easy to get wrong)*
7. The token the panel gives you up front is **temporary (expires in ~24 h)** *[CONFERIR]* and is **no
   good** for production. In **Business Settings → System users**, create a **System User**, give it
   access to the WABA and to the App, and **generate a permanent token** with the
   `whatsapp_business_messaging` and `whatsapp_business_management` scopes *[CONFERIR os escopos
   exatos]*. That is the `SendToken`.
8. Save the **App Secret** (App → Settings → Basic). That is the `AppSecret`, used to verify the HMAC
   of inbound webhooks.

### Phase 4 — Webhook *(the ONLY phase that depends on zapgw being deployed — plan 4)*
9. In the App's WhatsApp config, point the **Callback URL** at
   `https://zapgw.<domain>/v1/inbound/<slug>` and the **Verify Token** at the value
   `zapgw provisionar instancia` printed (or the one you passed in `ZAPGW_VERIFY_TOKEN`) — see *"Two
   of these secrets are SHARED"*, above. Meta makes a challenge `GET` right then — zapgw already
   answers it (`GET /v1/inbound/{slug}`).
10. **Saving the Callback URL already subscribes a SET of fields — your job here is to review, not to
    subscribe.** And, if you route per number, configure the **per-number/WABA webhook override** —
    without it, an App with several numbers delivers everything to a single endpoint, and zapgw
    refuses the mixed batch (see `docs/ARMADILHAS.md` and the contract).

    > **This step used to say "subscribe to the `messages` field", as if it were a single manual
    > action. It is not** — corrected on 2026-07-28 (T-056) against observed behavior. On saving the
    > new Callback URL, the App was subscribed all at once to a default set of fields, **without
    > anyone choosing**: **ten**, in that day's measurement, against the **one** this doc told you to
    > subscribe. Anyone following the old sentence would expect one field and would have ten switched
    > on without knowing.

    **How to know what is really subscribed** — it is the only way, and the panel does not show it in
    any obvious manner:

    ```
    GET https://graph.facebook.com/v25.0/{app-id}/subscriptions?access_token={app-id}|{app-secret}
    ```

    The token is the **app access token**, which is literally `app_id|app_secret` with the pipe in the
    middle. **It is a credential for administering the App's subscriptions** — whoever holds it
    repoints the Callback URL and diverts all the traffic — so treat it as a secret and do not let it
    into a log, shell history or transcript (see `docs/ARMADILHAS.md`, *Meta / WhatsApp Cloud API*).
    The `GET` is **a read and changes nothing**; the `v25.0` version is the same one the gateway uses
    by default (`graphBase`, `cmd/zapgw/main.go`).

    > 🔴 **Where the `app_secret` does NOT come from: the machine running the gateway.** Measured on
    > 2026-07-28 (T-057): it is **not** in `/etc/zapgw/env` on CT 125 — that file has
    > `ZAPGW_CHAVE_CIFRA`, `ZAPGW_BANCO` and `ZAPGW_ENDERECO`. The `app_secret` lives **encrypted in
    > the database, per instance**, and **no CLI command decrypts it back** — which is a decision, not
    > a gap: T-052 chose to print only the two secrets that need to exist OUTSIDE the gateway
    > (`verify_token` and `segredo_entrega`), and this is not one of them. Practical consequence:
    > **whoever is going to run this `GET` needs the value from another source**, and there is no
    > point looking for it on the CT. If you do not have the value, **say you did not measure** — do
    > not deduce the subscription list from what the doc said yesterday.

    **What to do with the result:** check it against `docs/META-CAMPOS-DE-WEBHOOK.md`, which carries
    the inventory measured on 2026-07-28 — which fields were subscribed, which ones the gateway
    models, and the payload shape of each. **The number is neither stable nor the same for every
    App**: only the `GET` answers for *your* App, today.

    **Pruning is the business owner's decision, not this doc's and not the deployer's.** A subscribed
    and unmodeled field **is not an error**: it arrives, passes the guards and is delivered to the
    consumer with no event at all (`"eventos": []` — see `docs/CONTRATO-CONSUMIDOR.md`), becoming a
    raw row in their database — volume and data stored with no use, not risk. And **unsubscribing has
    an asymmetric cost**: the field nobody reads today may be the one that would warn of a quality
    drop tomorrow. Survey the list, show it to the owner, and **do not unsubscribe anything on your
    own** — least of all on the same day as a cutover, when adding a variable to what has just
    stabilized is the worst deal available.

    **ADDING a field later is a panel action, not an API one — and the distinction is not a matter of
    taste** (T-057, 2026-07-28, when `template_category_update` was subscribed). The
    `POST /{app-id}/subscriptions` route exists and the credential above works for it, but it takes
    `callback_url` **and** `verify_token` **in the same request**: getting one character wrong there
    repoints the delivery of all the traffic. The panel has a **per-field toggle, which does not touch
    the Callback URL**. In a production installation, the safe path is worth more than the automatable
    one — and the `GET` above checks afterwards, because it changes nothing.
11. This step requires `zapgw.<domain>` to **already be up with a valid TLS certificate on the public
    hostname** — which is plan 4. Before that, webhook verification fails **silently** on both sides
    (see the certificate trap). That is why it is the last phase, and the only one that waits on us.

## The sequencing that matters

```
Phase 1 (App) + Phase 2 (number) + Phase 3 (token)  ─┐  fast, Meta's side
                                                      ├─► Phase 4 (webhook) ─► LIVE at 250/24h
zapgw plans 3–4 (code + DEPLOY)  ─────────────────────┘  ← this is the critical path

Phase 1b (business verification, ~20–40 days) ────────► unlocks tiers above 250 (parallel)
```

**Prioritize the deploy; start verification in parallel so it does not become a bottleneck
*later*, when volume goes up.**

## What only the owner can do (it is not code, I do not delegate it)

Business verification, choosing and verifying the number, the display name, the payment method and
creating the System User are acts of the **business's identity** at Meta — they require the owner's
documents and credentials. My role is to prepare zapgw to receive the values those stages produce
(the administration panel, plan 4) and to check the flow with the smoke test. **Starting the
onboarding is your action.**
