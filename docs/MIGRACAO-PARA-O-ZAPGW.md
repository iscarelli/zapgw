# Migrating a consumer to zapgw

*[Leia em português](MIGRACAO-PARA-O-ZAPGW.pt-BR.md)*

**Code:** [`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md) (what the consumer has to comply with) ·
the inbound-arming runbook (kept in the private repository — it is house operations, not product) ·
the deployment runbook (kept in the private repository) (where the gateway runs) · `internal/inbound/deliver.go` ·
`internal/outbound/handler.go`

> **Scope.** This document describes what zapgw **requires and guarantees**, and in what order the
> switch has to happen. It does **not** describe changes to the consumer's code: each repository is
> read by whoever works on it. Here it is the contract and the sequence.

---

## Two very different migrations use this document

| | Consumer **in testing** | Consumer **in production** |
|---|---|---|
| Downtime window | not needed | **must not exist** |
| Both paths coexisting | dispensable | **mandatory** |
| Rollback rehearsal | dispensable | **mandatory** |
| Order of the steps | advice | **structure** |
| Consumer-side dedup | "should have" | **blocking prerequisite** |

The first consumer (`consumer-a`, 2026-07-26) was **in testing**, and that is what made its migration
cheap — it fit in a day because being wrong was reversible. **That discount does not repeat.** If
your system serves people right now, read the right-hand column and treat each of its lines as a
requirement, not as diligence.

---

## What the migration IS, and what it is NOT

**It is changing who talks to Meta.** The consumer stops calling the Cloud API (directly or through a
layer such as Evolution) and starts calling the gateway, which calls Meta.

**It is NOT entering the official ecosystem.** If your number today is on an **unofficial** connection
(WhatsApp Web, Baileys and the like), this is not for you: you first need to register the number on
the Cloud API, submit templates for approval (days), and accept the 24 h window and the per-conversation
cost — **and the number cannot be active in both worlds at the same time**, which rules out coexistence.
In that case, "it cannot stop" becomes the hardest requirement of the project.

> **Confirm this before anything else.** A payload shaped like the Cloud API does not prove an
> official connection: intermediate layers normalize the format. Ask whoever administers the number,
> or look in Business Manager to see whether the number appears as registered on the Cloud API.

---

## Phase 0 — the gates. Nothing is scheduled before this.

**No cutover date is set while any item here is open.**

### 0.1 — Has the gateway already served two instances? (gateway side) — **YES, in the lab**

If you are the **second** consumer, your migration is the first time the gateway serves two
consumers **for real** at the same time. What has already been done, and what remains open, is the
distinction that matters here.

With **one App per consumer** (this house's standard), inbound isolation is the **URL path** plus
**that** instance's `app_secret`: each instance receives at `/v1/inbound/{slug}`, and
`app_secret`/`verify_token` belong to different Apps.

**Exercised with traffic on 2026-07-26 (T-042)**, on CT 125's production binary, with a second test
instance created and removed on the same day:

- a signed POST on the test instance's path → `200`, and the envelope arrived **only** at the test
  collector. `tenant-one`'s counters did not move (24/24 before and after);
- **the same bytes, with the same valid signature, on the other instance's path → `403`
  `assinatura invalida`.** It is this half that proves the secret is per instance; the first would
  only prove routing;
- the same with the `verify_token` on the verification `GET`: `200` + challenge on its own path,
  `403` on the other one's path;
- the alarm for a foreign `phone_number_id` — which had **never fired in the service's life** — was
  provoked on purpose and **fired**.

**What this does NOT prove, and must not be read as if it did: Meta actually delivering on the second
instance's path.** The traffic above originated from us, signing with a secret we chose ourselves —
it is the same binary and the same code, not the same origin. That proof is about the real network
and lives in gate 0.4 (`zapgw fumaca` of the real instance), not here.

### 0.2 — Is your deduplication ATOMIC? (consumer side — **blocking**)

```python
if Evento.objects.filter(evento_id=eid).exists():   # ← NOT dedup
    return
```

If your dedup looks like this, **stop here**. It fails under concurrency, and concurrency is not
hypothetical: Meta re-delivers **5 times in 9 seconds** when it does not get a `200` in time. Two
deliveries of the same event both pass the `exists()` and both insert.

And the cost is not one repeated row — **the side effects run again**: a duplicated message on the
screen, a duplicated forward, an **automatic reply sent twice to the same person**, and Meta quota
burned.

What solves it: a database constraint, with the violation treated as **success**.

```sql
CREATE UNIQUE INDEX CONCURRENTLY uniq_evento_id ON eventos (evento_id) WHERE evento_id <> '';
```

Details and the reasoning in [`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md), section *Deduplique
POR EVENTO*.

### 0.3 — Separate LIVE code from the rest of an abandoned integration

A project that has already tried another path leaves artifacts that **read as current**: endpoints,
parsers, clients, configuration. **A conclusion drawn from dead code looks well-founded** — it has
`file:line` — and it is not.

It happened on 2026-07-26: a consumer concluded *"meu rollback pode não existir"* ("my rollback may
not exist") from reading an endpoint that was the remains of an interrupted study, not the live path.

**Prove it with traffic, not with reading** — and the test that separates live from dead is **data
only the live path produces**: a row in the database, a counter, an observable effect. It does not
have to be a new log. The consumer above settled the question with a read query in production — *795
messages received, the last one three hours ago, and all created exclusively by that endpoint* —, and
a temporary `print` would have cost a deploy to prove what the database already said. **A new log is
the resort for when the path leaves no trace**; if it does leave one, the trace is already the proof.

The same survey found the residue that reading does not find: **three periodic tasks from the
abandoned study running in the worker, every 60 s and 300 s, against empty tables.** They intercepted
nothing — and that is why nobody would have looked. Enumerate the residue even after the suspicion
falls apart.

### 0.4 — The instance exists, is paused, and the smoke test passed

An instance is born **paused** (`ativo=0`) on purpose. Only `zapgw fumaca` activates it, and it sends
a real message — which proves the token, the number and connectivity **before** any consumer depends
on it.

### 0.5 — The consumer's receiving endpoint is up and proven

Answering `200` is not enough. It has to fulfill the contract's obligations — and the costliest one
is: **answer `200` only after having stored.** The consumer's `200` makes the gateway tell Meta the
matter is settled, and **Meta never resends again**.

Prove it with an unsigned request: it must give `5xx` (not `403` — see the credential-error section
of the contract).

---

## Phase 1 — SENDING. Reversible at any moment.

The consumer starts calling `POST /v1/messages` instead of Meta (or the old layer).

**Why this phase comes first:** it is **entirely reversible and on the consumer's side**. There is no
configuration change at Meta, no step of no return. If something goes wrong, the switch is an
environment variable.

Recommended: **a switch that, when empty, keeps the old path**. That way the new code goes to
production inert, and the cutover is one line of `.env` — with someone watching.

Start with the cheapest type to get wrong: a test notice, to the number of whoever is watching. **If
it fails, it fails for a person who is watching, not for a customer.**

Three things that bite on the first integration:

1. **Port `8443` is not a detail.** Sending only exists on the internal entrypoint. On `:443` the same
   URL returns `404`, **on purpose**.
2. **Do not disable TLS verification.** What travels there is your token.
3. **`Idempotency-Key` identifies the INTENT, not the attempt.** Your queue row's id works; a new UUID
   on every retry is worth nothing. And **the body has to be byte-for-byte stable across attempts with
   the same key** — if any part of the text is computed from the clock ("due tomorrow"), the retry
   becomes a `422`.
   **Freeze the text at the FIRST DISPATCH, persist the frozen value, and only then call
   `POST /v1/messages` — never recompute if a persisted value already exists.** In this order, and all
   three parts have a reason:
   - **at the first dispatch, not at the birth of the queue row** — this doc said "at birth" and was
     wrong. A consumer knocked the recommendation down with its own case: the system has a **sending
     window** (outside 8h–20h the message is deferred to the next window, and that is the normal path,
     not the edge case). Frozen at birth, a 21:00 dispatch arrives at 08:00 saying *"Boa noite"*
     ("Good evening"). The `422` is a **conditional** risk — it only bites if there is a retry crossing
     the boundary; the wrong greeting is a **certain** error, visible to the customer, on every
     deferred message;
   - **persist it** — otherwise a reconstruction (worker restarted, row re-queued) recomputes and the
     `422` comes back through the back door;
   - **persist BEFORE the HTTP call** — it is the same obligation as on inbound (*store the raw body
     before answering*). Persisting afterwards leaves the window in which the gateway already has
     `key → body` and you do not have the body: the retry recomputes and takes a `422`. It is exactly
     the hole that freezing exists to close.

   > **The case nobody audits is the greeting, not the date.** A consumer went looking for whether
   > this warning hit them and found `"Bom dia"/"Boa tarde"/"Boa noite"` ("Good morning/afternoon/
   > evening") — derived from `now().hour` — filling a variable in **almost all** of their templates.
   > The body changes on its own at 12:00 and at 18:00: an attempt at 11:59 + a retry at 12:01 = same
   > key, different body = a permanent `422`, and the message does not go out. "Due tomorrow" at least
   > **looks** like transactional content and gets checked; a greeting looks like decoration. And the
   > timing conspires: a retry happens when something is slow — which is when the chance of crossing
   > the boundary is highest. Look for **everything** that reads the clock, not just dates.

---

## Phase 2 — RECEIVING. The only step with no immediate way back.

```
1. register the callback_url on the instance      ← zapgw instancia rotacionar --callback-url
2. rotate the real app_secret in the gateway      ← needs the value from YOUR App
3. point YOUR App's Callback URL here             ← IRREVERSIBLE the instant you save
4. test message from the handset, full cycle      ← the proof
```

**Steps 1 and 2 come before 3, always.** The other way around, Meta starts delivering to a gateway
that does not yet know how to check the signature: every delivery is refused, it re-queues for 36 h
and the alarm fires for nothing. There is no loss — there is noise and a useless investigation.

**There is no coexistence on receiving.** The webhook is **per App**: the second the URL is saved,
that number delivers here and nowhere else. This is not a gateway limitation; it is Meta's.

### Rolling back

**Point your App's Callback URL back at the previous endpoint.** That is the only thing to undo.

For that to work, **the old endpoint has to stay up** — do not switch it off in the same move. Keep it
alive until the cycle is proven and you have slept on it.

**Rehearse it beforehand.** Know in advance how many clicks and how long it takes to repoint, and who
has the access to do it. A rollback nobody rehearsed is a rollback that is slow on exactly the day
haste matters.

> **Write down the previous endpoint's exact URL BEFORE the cutover**, along with the App ID, somewhere
> you can find in ten seconds. A consumer who prepared its own migration put it this way:
> *"rollback cuja primeira etapa é **achar o valor que eu preciso digitar de volta** é rollback lento,
> e a lentidão chega no pior dia."* ("a rollback whose first step is **finding the value I need to
> type back in** is a slow rollback, and the slowness arrives on the worst day.")
> You are going to replace that value in the panel — and the panel does not keep the previous one.

**Find out where Meta delivers today without depending on the panel.** If your current endpoint
requires something Meta does not send — an `Authorization: Bearer`, for example — then Meta does
**not** deliver to it directly: it delivers to the intermediate layer, which forwards. A consumer
proved exactly that, by `file:line`, with no access to the panel: *"se o Callback URL apontasse para o
meu Django, toda entrega levaria `401`, e o sistema funciona há meses"* ("if the Callback URL pointed
at my Django, every delivery would take a `401`, and the system has been working for months"). A live
constraint proves topology better than reading configuration.

To stop receiving **without** touching Meta: pause the instance. It starts answering `503`, Meta
resends for 36 h, and a short pause **loses no message**. A long pause does lose, permanently and
silently.

---

## What the consumer gains, and it is not obvious

- **`GET /v1/instances/{slug}/health`** catches a token revoked at Meta — the failure that dies
  silently and whose first news is usually the customer not receiving anything.
- **`GET /v1/templates`** returns the **whole** catalog, untruncated.
- **`POST /v1/media`** removes the need to host a public URL for Meta to fetch.
- **`cobranca` in the status event** says under which category Meta charged — the `UTILITY` →
  `MARKETING` reclassification shows up in the first message, not in the invoice.
- **The media's two mime types arrive separately.** Whoever re-sends audio needs the
  `mime_do_payload` — with the other one, WhatsApp delivers an **attachment** instead of a voice note,
  with no error at all. It cost dearly on this network on 2026-07-20.

## What this document does NOT guarantee

- **That your current path is the official Cloud API.** Confirm it (see above). If it is not, this is
  the wrong document.
- **That your current layer only passed messages along.** If Evolution (or an equivalent) also stored
  media, managed sessions or did anything beyond forwarding, that **does not come along** — survey what
  it does today before taking it out of the path.
- **That no other system depends on the same number.** If one does, Phase 2 affects that system at the
  same instant, and nobody has checked.
