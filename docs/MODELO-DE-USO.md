# The zapgw usage model — who does what

*[Leia em português](MODELO-DE-USO.pt-BR.md)*

**Code:** `internal/outbound/cadastro_handler.go` (step 3, `POST /v1/cadastro`),
`internal/outbound/fumaca.go` (the single path through the four smoke-test steps, called by BOTH
facades), `internal/outbound/fumaca_handler.go` (`POST /v1/fumaca`, step 4 over the API),
`internal/outbound/pausa_handler.go` (`POST /v1/pausa`), `cmd/zapgw/fumaca.go` (the command-line
facade over the same path), `internal/config/store.go` (`RegisterMeta`, `RegistrationWindow`,
`ReopenRegistrationWindow`, the `instancia.cadastro_em` migration, and
`ActivateInstance`/`PauseInstance` — the only paths to `ativo`), `cmd/zapgw/provisionar.go`
(creation with `--slug` only, the delivery bundle and `zapgw instancia reabrir-cadastro`),
`cmd/zapgw/estado.go` (the line that says where the daily series lives), `internal/meta/instagram.go`
(Instagram sending and parsing). Steps 1, 2, 3, 4 and item 8 shipped in T-079; item 1 (manual
creation) and item 7 (registering does not activate) predate it and still hold. Step 4 over the API
(item 7 of the audit, below) shipped in T-084; items 2, 3, 4 and 5 of the audit shipped in T-083.
The *Instagram* section shipped in T-097.

This document is the **source of the design**; the tasks and the manual derive from it. Decided by
the owner on 2026-07-28. **If this file diverges from a task, it wins and the task is wrong.**

It exists because the design was decided in a quick conversation and the planner reconstructed it
wrong three times in a row — including inverting the direction of an entire API. **A design that
only exists in chat does not exist.**

## The target

**THIRD-PARTY programmers**, with their own Meta account, **out of the owner's reach and with no
channel to ask questions**. This is not "another system of mine": it is people who receive a
credential once and get by with the documentation.

> *"Ideal é ser o B, posso não conseguir subir esse canal sempre."*
> ("Ideally it's B, I may not always be able to bring that channel up.")

## The flow, and who does each step

| # | Who | What |
|---|---|---|
| 1 | **Owner** | Creates the instance. **Manual, always.** Supplies the **slug** — *"se não, usuário faz aberração"* ("otherwise the user makes a monstrosity"). The slug is immutable and becomes a URL path. |
| 2 | **Owner** | Hands the consumer **the minimum it needs to talk to the gateway**. Nothing more. |
| 3 | **Consumer** | Registers **its own Meta** with the gateway, **over the API**, from its own panel. |
| 4 | **Consumer** | Proves the channel (`fumaca`) — only that activates the instance. |
| 5 | **Consumer** | Operates: sends, receives, reads state — **all through the gateway**. |

**The direction of step 3 is CONSUMER → GATEWAY.** It is a write, not a read. *(The planner designed
a self-description route — the gateway handing data back — twice, and was wrong both times.)*

## What is DECIDED

1. **Creation is manual and always will be.** No auto-provisioning, no sandbox, no secret derivable
   from the slug name. Considered and discarded.
2. **The slug belongs to the owner.** It is not the integrator's choice.
3. **The consumer owns its Meta.** The owner does not know, does not store and does not broker
   `waba_id`, `phone_number_id`, `app_secret` or `token_envio` — **that is the consumer's data**.
4. **The consumer has its own panel, and its security is its own problem.** We do not build a panel.
   *"E continuamos sobrevivendo sem painel."* ("And we keep surviving without a panel.")
5. **Nobody talks to Meta directly** — not even to read. See `CLAUDE.md`.
6. **A secret goes in and does not come back.** Stored encrypted; no surface decrypts it for display.
7. **Registering does not activate.** The instance is born paused; only a successful send activates it.
8. **The registration window is 24 h, counted from the consumer's FIRST insertion.** After that the
   configuration **locks**, and changing it requires human intervention by the owner.
   > *"24h da primeira inserção: eu criar a instância hoje, daqui 5 dias o consumidor insere algo,
   > começa a contar ali."* ("24h from the first insertion: I create the instance today, 5 days from
   > now the consumer inserts something, it starts counting there.")

   **It does not count from instance creation** — a slow consumer would lose the window before
   starting. And it **does not restart on every change** — if it did, whoever touched it daily would
   keep the window open forever, and the rule would become decorative.
   **What it solves, and it is elegant:** the consumer token is powerful **for a while**, not by
   permission. During the window the consumer tests, errs and fixes things on its own; after it, a
   stolen token is worth only "send a message" again, which is the risk that already existed.
   *Limiting in time instead of limiting by permission was the owner's decision, and it is better
   than what the planner had proposed.*
9. **The owner has a command to REOPEN the window.** A consumer locking itself out with a wrong
   credential is going to happen, and without the command the way out would be hand-written SQL in
   production — which is exactly what T-048 existed to kill.

## What that IMPLIES, and it is not optional

- **The consumer token stops being "send a message" and becomes "reconfigure the instance".**
  Whoever steals it replaces the credentials and points the instance at their own Meta. It is an
  accepted consequence of the model — and **the consumer has to know**, because protection on its
  side is its own and it needs to size it.
- **The requirement for `waba_id`/`phone_number_id` moves, it does not disappear.** T-074 started
  requiring them at creation; under this model they become required **at registration**. Its test is
  adjusted, not removed.
- **One instance per Meta App.** The real separation between tenants is the **per-instance
  signature**, and it only distinguishes when the `app_secret` values differ. With third parties
  bringing their own App that is guaranteed by construction — two numbers under the same App would
  bring the guarantee down.
- **With no channel, the documentation and the error messages ARE the support.** Every ambiguous
  terminal error is a dead end. This is what turned T-078 from "fix an ugly case" into a standard.

## What was DECIDED on 2026-07-28, about the contract audit

The first five were open and the owner decided all of them in the same conversation.

1. ✅ **EXPOSE the outbound routes — but NOT today, and on a DIFFERENT URL.**
   > *"Vamos expor, mas não hoje. Com URL diferente."* ("We will expose them, but not today. On a
   > different URL.")

   **It is T-053** (separate public from internal by **hostname**, not by port), which goes from
   "evaluate" to **approved in principle, no date**. *The port separation that exists today protects
   by accident of topology, not by design — and this gateway is built to run outside the homelab one
   day.*
   ⚠️ **Until that happens, the third-party model DOES NOT WORK**, and the contract has to say so out
   loud instead of describing as public a surface that is not. The costliest case is
   `POST /v1/cadastro`: the consumer's `app_secret` and `token_envio` go through it, and it is
   unreachable — a third party cannot even start.
2. ✅ **The INTERNAL contract becomes publishable — a single document, with no derived version.**
   Two documents diverge, and that is what this project fights hardest. Out go the `T-0xx`
   references and the `file:line` pointers: the audit showed that **none** of them add information a
   third party can use (the date is already alongside, and they do not have the code).
3. ✅ **There is NO contact address, and the contract says so once, at the top.**
   The nine implicit promises (*"ask"*, *"let us know"*, *"say so here"*) become instructions that
   resolve themselves. *Adding a channel later is easy; taking away one people started using is not.*
4. ✅ **Deprecation by DEADLINE, not by consensus.** *"Campo marcado obsoleto sai no mínimo N meses
   depois, anunciado em Mudanças que quebram."* ("A field marked obsolete goes out at least N months
   later, announced in Mudanças que quebram.") The old condition — *"os dois consumidores
   confirmarem por escrito"* ("both consumers confirming in writing") — never closes with N third
   parties, and leaves the integrator reading "OBSOLETO" without knowing whether it can be used.
   **N = 6 months**, chosen and written in T-083 (`docs/CONTRATO-CONSUMIDOR.md`, section *Política de
   depreciação*). If the owner wants another number, that is where it changes — and the table of
   minimum dates (`dia`, `serie_7_dias`) changes with it.
5. ✅ **Real numbers come out of the examples**, replaced by a declared convention. It fixes at the
   same time what the audit found: the examples alternate between two slugs and **one of them is the
   real one** — for someone who cannot ask, a placeholder that looks real is a coin flip.
6. ✅ **`serie_diaria` stays OUT of `zapgw estado`** (owner's decision: *"mantenha fora"* — "keep it
   out"). Dozens of lines per instance would make the terminal useless, and the short series stays on
   screen, so the operator is not blind. **T-083 added two lines to `zapgw estado` saying that it
   exists and where it comes out** — omitting without saying where it lives was T-065's defect with
   the sign flipped.

**Items 2, 3, 4 and 5 shipped in T-083** (`docs/CONTRATO-CONSUMIDOR.md`, and `cmd/zapgw/estado.go`
for the complement to item 6).

7. ✅ **Step 4 becomes executable by the consumer: the SMOKE TEST gets a route, and so does PAUSE.**
   Decided by the owner on 2026-07-28, 21:21, about the hole raised while implementing T-079 —
   `zapgw fumaca` is a **command line**, a third party has no shell on the gateway machine, and there
   was no channel even to tell us it had registered. **Implemented in T-084**
   (`internal/outbound/fumaca.go`, `fumaca_handler.go`, `pausa_handler.go`).

   **The verb is what makes the decision work, and it is not "activate":**

   | Route | What it does | Why it may exist |
   |---|---|---|
   | runs the **smoke test** | really sends to Meta and, **if Meta accepts**, activates | `ativo = 1` remains a **consequence of the proof**, not of the request |
   | **pause** | goes back to `ativo = 0` | safe direction (fail-closed); coming back **requires a new smoke test** |

   🔴 **What still does not exist, not even as a route: turning OFF the smoke-test REQUIREMENT.** That
   is the force flag under another name. `internal/config/store.go` (comment on `ActivateInstance`)
   (*"AtivarInstancia e o UNICO caminho para `ativo = 1` neste projeto"*) and
   `internal/outbound/fumaca.go` (*"NAO EXISTE FLAG DE FORCA"*) exist because a path to `ativo = 1`
   without real traffic would let the consumer register the wrong credential, press the button and
   find out on the first real customer. **The route also does not send a message when the instance is
   already active** — otherwise it would become the only way to burn paid messages in a loop, since it
   is the only gateway route that sends without the consumer having asked for a send.

   ⚠️ **This does NOT unlock the model on its own:** the route is born next to the other outbound
   routes, so it stays unreachable to a third party **until T-053** — exactly like item 1.

## Instagram (T-097, 2026-07-30) — the SAME target, a DIFFERENT model

The instance gains a **type**: `whatsapp` (the default — everything above describes IT, unchanged)
or `instagram`. Instagram uses the **OLD** model, from before T-079 — items 1 (manual creation) and
7 (registering does not activate) of this page, without items 2–6 (the consumer's API registration,
`POST /v1/cadastro`).

**Why, and why this is not a regression:** item 3 of this page ("the consumer owns its Meta") remains
true — whoever brings the `ig_id` and the credentials is the owner of the channel, not the owner of
the gateway. What changes is the **CHANNEL** those credentials take to reach the gateway: for
WhatsApp it is an HTTP call (`POST /v1/cadastro`) the consumer makes after the instance exists; for
Instagram in this slice it is a command-line flag (`zapgw provisionar instancia --tipo instagram
--ig-id <IGID>`) the OWNER types, with the credentials arriving from the consumer out of band (the
same human channel that delivers the provisioning bundle today).

We did not write an equivalent `RegisterMeta` for Instagram — not because the need is different, but
because this is the **first slice**, and replicating the whole model (24 h window,
`ReopenRegistrationWindow`, identity validation over the API) without a second consumer asking for it
would be building for a hypothetical demand. If a real third party needs to self-register on an
Instagram instance, that is the next task — and it would extend `MetaRegistration` and
`internal/outbound/cadastro_handler.go` for the new type, not create a parallel path.

**What does NOT change, and it is what this document exists to protect:** the instance is born
**paused** (`CreateInstance` writes `ativo = 0` for any type — the check is structural, not
per-field), and only a test send **actually accepted by Meta** activates it (`zapgw fumaca` /
`POST /v1/fumaca`, extended to call `SendInstagramMessage` when the type asks for it —
`internal/outbound/fumaca.go`). There is not, and cannot come to be, a force flag for either type.

Protocol details (IGSID, the 24 h/7-day window, what the send route accepts) are in
`docs/CONTRATO-CONSUMIDOR.md`, section *Instagram — a primeira fatia* — this document describes
**who does what**, not the shape of the bodies.

## What REMAINS open

Nothing in the WhatsApp design. What is missing there is network reach (item 1 → T-053).

On the Instagram side: an equivalent `POST /v1/cadastro` (if a real third party comes to need it),
media/template/reaction/read receipt (out of scope for this slice, not decided as "never"), and
network reach — the same T-053, because the route is the same door.
