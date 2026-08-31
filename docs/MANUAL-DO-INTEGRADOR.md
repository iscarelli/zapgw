# Integrator's manual — zapgw

*[Leia em português](MANUAL-DO-INTEGRADOR.pt-BR.md)*

**Code:** `internal/outbound/cadastro_handler.go`, `internal/outbound/fumaca.go`,
`internal/outbound/fumaca_handler.go`, `internal/outbound/pausa_handler.go`,
`internal/inbound/handler.go`, `cmd/zapgw/provisionar.go`, `internal/config/store.go`,
`internal/meta/instagram.go`, `internal/outbound/renovador_instagram.go`.

*(That block is gateway maintenance metadata — it answers "which doc did this code change break?".
**You do not need it**, and nothing below depends on you having those files.)*

---

This is the **deployment** manual: *what you do, in what order, to go from zero to operating*. It is
short on purpose.

**It does not describe route behavior.** That is in
[`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md) — bodies, fields, errors, guarantees and limits.
They are two documents because they are two different questions, and **two copies of the same thing
diverge**. The sequence here; the behavior there. When this manual needs a route detail, it points
there instead of repeating it.

**Who it is for:** a programmer with **their own Meta account**, who received a slug from whoever
operates the gateway and **has no channel to ask anything**. If you are reading this, assume nobody
will answer an email: what you need to know is written here or in the contract.

🔴 **And it covers WhatsApp. If your slug is an INSTAGRAM one, stop here and read the paragraph
below.** An Instagram instance exists (since 2026-07-30, in production), but the **sequence is
different** and much of this manual **does not apply to it**:

- **there is no `POST /v1/cadastro` for Instagram**, and therefore no 24 h window. The account
  identification (`ig_id`) only goes in **at instance creation**, done by whoever operates the
  gateway — so Steps 2, 3 and 4 of this manual, which are about you preparing and registering your
  account, **are carried out by that person**, not by you;
- the `ig_id` that counts is the **`entry[].id` that arrives in the webhook** — **not** the one
  `GET /me` returns, which is a different id space. *Confusing the two has already made this gateway
  discard real traffic;*
- sending is **text** only in this first slice, and the token is valid for 60 days, renewed
  automatically by the gateway.

**What holds for you, if your slug is an Instagram one:** route behavior is in the section
*Instagram — a primeira fatia* of [`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md), and Step 5
(prove the channel with the smoke test) and Step 6 (operate) of this manual still hold. **Steps 2, 3
and 4 do not.**

*This is stated here, and not omitted, because a manual that describes only one of the two paths
sends whoever is on the other one down a sequence that will fail — and that person has nobody to
ask.*

---

## Index

- [🔴 First of all: registration is NOT reachable from outside, and that is a DECISION](#-first-of-all-registration-is-not-reachable-from-outside-and-that-is-a-decision)
- [The map, on one screen](#the-map-on-one-screen)
- [Step 1 — Check what you received](#step-1--check-what-you-received)
- [Step 2 — Prepare YOUR Meta account](#step-2--prepare-your-meta-account)
- [Step 3 — Register your Meta with the gateway](#step-3--register-your-meta-with-the-gateway)
  - [🔴 The 24 h window — the costliest mistake in this manual](#-the-24-h-window--the-costliest-mistake-in-this-manual)
- [Step 4 — Point the webhook in YOUR Meta panel](#step-4--point-the-webhook-in-your-meta-panel)
- [Step 5 — Prove the channel (and only then the instance activates)](#step-5--prove-the-channel-and-only-then-the-instance-activates)
- [Step 6 — Operate](#step-6--operate)
- [When something goes wrong, look in this order](#when-something-goes-wrong-look-in-this-order)
- [The three things only the other person can solve](#the-three-things-only-the-other-person-can-solve)

---

## 🔴 First of all: registration is NOT reachable from outside, and that is a DECISION

**Read this before booking team time.** Of all the gateway's routes, **only the webhook**
(`POST /v1/inbound/{slug}`, which **Meta** is the one to call) is published on the internet.
Everything else — **including `POST /v1/cadastro`** — answers only on the gateway's internal network.

**This is not unfinished work nor a gap waiting for a fix: it is the gateway owner's choice, and it
stays that way until there is a need that justifies otherwise.** Do not plan against a date, because
there is none.

The direct consequence, and it is the one you have to accommodate: **Step 3 of this manual — where
your Meta account's `app_secret` and `token_envio` go in — is carried out by whoever can reach the
gateway's network.** In practice, you hand your values to that person and they register on your
behalf. All the rest of the flow is still yours.

---

## The map, on one screen

| # | Who does it | What | Depends on |
|---|---|---|---|
| 1 | **whoever operates the gateway** | creates your instance and hands you **5 values** | nothing |
| 2 | **you** | prepare **your** Meta account (App, number, permanent token) | nothing of ours |
| 3 | **you** | `POST /v1/cadastro` — writes your Meta into the gateway | 1 and 2 |
| 4 | **you** | point the webhook in **your** Meta panel | 1 |
| 5 | **you** | `POST /v1/fumaca` — proves the channel. **Only this activates the instance** | 3 |
| 6 | **you** | operate: send, receive, read state | 5 |

Steps 2 and 4 happen in the Meta panel, in **your** account. Steps 3, 5 and 6 are HTTP calls to the
gateway. **Step 1 happens once and does not repeat.**

⏱ **How long it takes:** steps 3 to 5 take minutes. Step 2 takes hours to days, and it is the one
that rules the calendar — the slow part is Meta, not us.

---

## Step 1 — Check what you received

The instance is created **manually** by whoever operates the gateway, and they are the one who
chooses the **slug** (it is immutable and becomes a URL path). They **do not know, do not store and
do not ask for** your Meta account's data.

You receive **five items**, in a conversation that happens once:

| # | What | What it is for |
|---|---|---|
| 1 | the **slug** | identifies the instance in **every** request body (`"instancia": "<slug>"`) |
| 2 | the **registration** URL | the base of the routes **you** call |
| 3 | the **webhook** URL (`…/v1/inbound/<slug>`) | you paste it into *Callback URL* in your Meta panel |
| 4 | `verify_token` **and** `segredo_entrega` | the first you type into *Verify Token* at Meta; the second checks the signature of every delivery the gateway makes to you |
| 5 | the **consumer token** | it is the `Authorization: Bearer` of every call you make |

🔴 **Check all five NOW, before starting.** The two values in item 4 are drawn by the gateway and
**shown only once** — it keeps only the encrypted form and does not show them again. A missing item
means work stopped on your side, and the only way out is to go back to whoever handed you the slug.

**Nothing else is missing.** There is no sixth value someone forgot to send. Everything not on that
list is either **yours** (you register it yourself, in Step 3) or is written in the contract.

> 🔴 **The consumer token (item 5) is an administration credential, not just a sending one.** Whoever
> steals it **reconfigures your instance** and points your traffic at their own Meta — while the 24 h
> window of Step 3 is open. Do not leave it in a repository, in a log or in a terminal transcript.
> Details and what each secret is worth in someone else's hands: contract, *"O que cada segredo
> permite a quem o roubar"*.

---

## Step 2 — Prepare YOUR Meta account

Everything here happens in **your** account, and none of it depends on the gateway. At the end you
have **four values**, which are what Step 3 asks for:

| Value | What it is | Where |
|---|---|---|
| `waba_id` | id of the WhatsApp Business Account | Business Settings → WhatsApp accounts |
| `phone_number_id` | id of the **number** in the Graph API (**not** the phone number) | WhatsApp panel, after the number is registered |
| `app_secret` | the App's secret key | App → Settings → Basic → App Secret |
| `token_envio` | **permanent** System User token | Business Settings → System users |

The order that works:

1. **Business account + App.** Create the Meta Business account, create a Business-type App and add
   the **WhatsApp** product. Fast, and it already grants access to the Cloud API.
2. **Number.** Add a number to the WABA. It **cannot** be active on a regular WhatsApp or on WhatsApp
   Business — if it is, unlink it first. It needs to receive an SMS/call for the code.
3. **Display name.** It goes through Meta review and **can be refused** on policy grounds. A refusal
   restarts the wait, so read the policy in force before submitting.
4. **Payment method** on the WABA. The Cloud API charges per conversation above the free tier.
5. **Permanent token.** 🔴 **The token the panel hands you up front is temporary and is no good.** In
   *Business Settings → System users*, create a **System User**, give it access to the WABA **and** to
   the App, and generate a permanent token with messaging and WhatsApp Business management scopes.
   That is the `token_envio`.
6. **App Secret.** Save the value from App → Settings → Basic. That is the `app_secret`.

> ⚠️ **Meta changes this process often, and this manual does not state as fact what it has not
> checked.** The **steps** and the **dependencies** above are stable and are what matters for
> planning. The **numbers** and the **menu names** — deadlines, tier limits, exact scopes, whether
> your case requires App Review — check at `developers.facebook.com/docs/whatsapp` **on the day**,
> not from this page.

**Business Verification is NOT a gate to start.** There is an unverified tier that sends right away,
with a cap on business-initiated conversations per 24 h. Verification **unlocks volume**, it does not
enable sending: start it in parallel, but do not wait for it to integrate. *(The cap in force and
what counts as a "conversation" change — check at the source.)*

**One gateway instance = one Meta App.** What separates you from another tenant is the **signature**
of each webhook, and it only distinguishes when the `app_secret` values differ. With your own App
that is guaranteed by construction — but do not try to hang two numbers from different Apps on the
same slug.

---

## Step 3 — Register your Meta with the gateway

**The direction is you → gateway, and it is a write.** The gateway receives configuration; it never
hands it back.

```
POST /v1/cadastro
Authorization: Bearer <consumer token>
Content-Type: application/json
```

```jsonc
{
  "instancia":       "<your slug>",
  "waba_id":         "…",
  "phone_number_id": "…",
  "numero_exibido":  "…",           // the phone number as the customer sees it
  "app_secret":      "…",
  "token_envio":     "…",
  "callback_url":    "https://…",   // where we deliver what Meta sends
  "bundle_ca":       ""             // optional: PEM of YOUR CA, if you do not use a public CA
}
```

Three things that decide whether this works the first time:

- **Registration REPLACES: always send the complete set.** An omitted field counts as **empty**, not
  as "leave it alone". That is deliberate — a partial registration would require you to know what is
  already stored, and that is precisely what the gateway never hands back. **Re-registering is your
  rotation path:** changed the `app_secret` at Meta, send the whole set again.
- **The `callback_url` must be `https://`, and its certificate is verified.** There is no
  no-verification mode, on any path of this gateway. If your endpoint uses **its own CA**, send its
  PEM in `bundle_ca` — that swaps the trust anchor, it does not turn verification off.
- **An empty `callback_url` is a legitimate choice**: it means "outbound-only instance", and nothing
  is delivered to you. Just do not leave it empty by accident — check in the `200`, which says
  `cifrados: [{campo: "callback_url", cadastrado: true|false}]`.

**The response returns nothing of what you sent** — only *whether* each field is registered. A secret
goes in and does not come back, and this route makes no exception.

### 🔴 The 24 h window — the costliest mistake in this manual

You can write your instance's configuration for **24 h counted from the FIRST time you registered
something successfully**. Not from instance creation (you can take five days to start, and the clock
only starts when you write), and **not restarting on each change**. A registration refused with `400`
does not open the window — getting the body wrong on the first attempt does not cost you your 24 h.

After it, the `POST` answers `409`, **nothing is written**, and the old configuration stays in force.

> ⚠️ **Run Step 5 (smoke test) RIGHT AFTER the first registration — not the next day.** The case that
> hits this window most is a wrong `token_envio`, or one without permission, discovered **after** the
> window has closed. While it is open, the *register → smoke test → fix → re-register* cycle is all
> yours and costs nothing. After it, **every credential fix costs a trip to another person** — only
> whoever handed you the slug can reopen the window.

**Plan the 24 h as one continuous block of work.** Register when you already have the number working
and someone available to receive the test message.

Errors from this route (and from all the others): contract, the corresponding route's section.

---

## Step 4 — Point the webhook in YOUR Meta panel

In your App's WhatsApp configuration:

- **Callback URL** = item 3 of your bundle (`https://…/v1/inbound/<slug>`);
- **Verify Token** = the `verify_token` from item 4.

Meta makes a challenge `GET` when you save; the gateway answers it on its own. **That challenge works
even with the instance still paused** — you can do this step before or after Step 5, and messages
arriving while it is paused get `503`, which makes Meta **resend** for up to 36 h instead of
discarding.

Two details that catch people doing this for the first time:

- 🔴 **Saving the Callback URL subscribes a SET of fields at once** — you do not pick them one by one.
  Your job here is to **review what ended up subscribed**, not to subscribe a field. The only way to
  know what is really subscribed is to ask the Graph API for your App's *subscriptions*; the panel
  does not show it in any obvious way.
- **If your App has more than one number**, configure the **per-number/WABA webhook override**.
  Without it, an App with several numbers delivers everything to a single endpoint, and the gateway
  **discards the batch that does not match** the registered `phone_number_id`/`waba_id` — answering
  `200`, so Meta does not keep resending, and moving a counter you read in `GET /v1/estado`. *Those
  guards exist to catch configuration mistakes of yours; what separates you from another tenant is
  the **signature**, not them (contract, "O que separa você do outro inquilino").*

A subscribed field the gateway does not model **is not an error**: it arrives, passes the guards and
is delivered to you with no event at all (`"eventos": []`), becoming a raw row in your database.

---

## Step 5 — Prove the channel (and only then the instance activates)

```
POST /v1/fumaca
Authorization: Bearer <consumer token>
Content-Type: application/json
```

```jsonc
{ "instancia": "<your slug>", "destino": "<number that will RECEIVE, in E.164>" }
```

**What this route does, in this order:** checks that the instance exists → checks that the Graph API
accepts your `token_envio` → **sends a real message** → and only then activates. It aborts at the
first step that fails, and failing at any of them leaves the instance **paused**.

🔴 **`ativo` is always a consequence of Meta having accepted the send, never of you having called the
route.** There is no force flag, not here and not anywhere. If registering activated, a wrong
credential would become an "active" instance that refuses everything — and you would find out on the
first real customer.

Three practical things:

- **`destino` has no default, on purpose.** A real message is going out to that number, and a sent
  message cannot be undone. The text identifies itself in its own words ("teste de fumaça… não é
  preciso responder"), because a person is the one receiving it.
- ⚠️ **Pick a destination that has messaged your number recently.** The smoke test sends **free-form
  text**, and Meta restricts free-form text to people who have talked to you recently — outside that,
  it requires a template. *(Their rule, not ours, and not measured by this project: check at the
  source if you want the exact deadline.)* In practice: send a "hi" from the test phone to your
  business number **before** running the smoke test.
- **Calling it again on an already active instance is safe and cheap:** it does **not** send a second
  message, and answers `ja_estava_ativa: true`. To force a new proof (after changing the
  `token_envio`, for example), **pause first** with `POST /v1/pausa` — coming back requires a new
  smoke test.

**Got a `502`?** Meta refused. The error message says why, with the same classification as a normal
send (contract, *"O que cada erro quer dizer"*). The instance stays paused, and **while the 24 h
window is open you fix and re-register on your own**.

---

## Step 6 — Operate

From here on the contract is the document. What exists:

| What you want | Route |
|---|---|
| send a message (text, template, media, reaction, location…) | `POST /v1/messages` |
| mark a received message as read | `POST /v1/leituras` |
| read your instance's numbers (counters, daily series, token health, your certificate's validity, number quality) | `GET /v1/estado` |
| know whether the channel is still able to send | `GET /v1/instances/{slug}/health` |
| list and create templates | `GET` / `POST /v1/templates` |
| upload and download media | `POST /v1/media`, `GET /v1/media/{id}` |
| take the instance off the air without erasing anything | `POST /v1/pausa` |

**And five obligations that are yours, not ours.** They are detailed in the contract, under *"As
cinco obrigações do consumidor"*, and ignoring any of them produces a defect that only shows up in
production:

1. **Store the `cru` BEFORE looking at the `eventos`**, and answer afterwards.
2. **Deduplicate per EVENT**, by the `id` field inside the body — and **atomically**.
3. **Verify the `X-Zapgw-Signature` and the timestamp** of every delivery. The `segredo_entrega` is
   what separates a delivery of ours from a forged one.
4. **Guard against status regression** — statuses arrive out of order.
5. **When re-sending media, use the `mime_do_payload`**, never the `mime_do_get`.

🔴 **A credential or configuration error on YOUR endpoint answers `5xx`, never `4xx`.** A `4xx` tells
the gateway "this delivery is never going to work, do not insist" — and what was a missing
environment variable becomes a permanently lost message.

---

## When something goes wrong, look in this order

| Symptom | The first place to look |
|---|---|
| Meta refuses the webhook verification | is the `verify_token` you typed exactly item 4 of the bundle? |
| nothing arrives at your endpoint | is the instance **active**? (`GET /v1/estado`, field `pausada`). A paused instance answers `503` and Meta holds the resend for 36 h |
| it reaches the gateway but not you | is the `callback_url` registered? (the `200` from registration, `cifrados`). Is its certificate valid? (`GET /v1/estado`, `certificado_do_callback`) |
| sending fails with a credential error | `GET /v1/estado`, `token_meta` — it says whether the token is still valid, stamped with when it was checked |
| registration answers `409` | the 24 h window closed. Only whoever handed you the slug can reopen it |
| the smoke test answers `502` | Meta refused. The message says why; if it is a credential, fix it **before** the window closes |
| sending stopped going out, with no obvious error | `GET /v1/estado`, `numero_na_meta` — your number's quality and daily quota come from there |

**Before raising an alarm over silence, look at `pausada`.** It is the most common mistake: a paused
instance has no defect at all — it is waiting for a smoke test.

---

## The three things only the other person can solve

There is no support channel, and this manual is the support. But there are exactly **three** matters
you cannot solve on your own, and all of them are with **whoever handed you the slug**:

1. **reopening the registration window** after it has closed;
2. **rotating the consumer token** if it leaks (the previous one stops being valid at that same
   instant);
3. **saying what happened to an instance that vanished** (the routes started answering `404`).

That person is **not** a channel for questions, for requesting a new field or for investigating
traffic.

**A breaking change is announced in one place only:** the section *Mudanças que quebram*, at the end
of the contract. There is no notice through another channel and there is no endpoint that declares a
format version — re-read that section before shipping a new integration.
