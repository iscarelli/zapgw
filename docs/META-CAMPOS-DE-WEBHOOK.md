# Meta webhook fields — what exists, what we subscribe to, what we model

*[Leia em português](META-CAMPOS-DE-WEBHOOK.pt-BR.md)*

**Code:** `internal/meta/parse.go` (where Meta's format becomes the gateway's vocabulary),
`internal/meta/types.go` (the vocabulary), `internal/inbound/handler.go` (guards 5a/5b, which decide
whether the event reaches the consumer), `testdata/corpus/` (the frozen payloads).

**What this doc is:** the inventory of the App's webhook fields, with the shape of each payload.
**What it is NOT:** a traffic capture. Unless stated otherwise, every example below is the **sample
from Meta's panel** (each field's *Test* button, which shows the body **without sending anything**),
collected on **2026-07-28**. The provenance distinction in the table of `testdata/corpus/README.md`
holds: **a panel sample is "derived from the docs"** — the shape is official, the values are
fictitious, and none of them was observed in this account's real traffic.

**Why this doc exists:** on 2026-07-28, when saving the new Callback URL, the App was subscribed to
**ten** fields at once, without anyone choosing. `ONBOARDING-META.md` said "subscribe to the
`messages` field", as if it were a single manual action. Finding out what was on — and what was
**not** — changed two technical decisions on the same day (see T-057 and T-058).

---

## How to read the "reaches the consumer?" column

The gateway models few fields. **An unmodeled field is not discarded**: it passes the guards, is
delivered **with no event at all**, and the consumer stores the `cru`. That is correct behavior, not
a parse failure — it is in the contract, and every consumer needs to know it, otherwise it treats an
empty event as an error.

> **On the wire the field comes as `"eventos": []`** — an empty array, always present. The
> normalization is done when the envelope is assembled (`internal/inbound/deliver.go`, the
> `if evs == nil` in the envelope assembly) and locked down by a test over the bytes
> (`TestEnvelopeWithNoEventGoesOutAsEmptyArrayOnTheWireNeverNull`).
>
> ⚠️ **From 2026-07-23 to 2026-07-28 it was `null` (T-067), and this doc said `[]` — a wrong doc, not
> a cosmetic detail.** `ParseWebhook` returns a **nil** slice when it enriches nothing
> (`internal/meta/parse.go`, at the end of `ParseWebhook`) and `json.Marshal` of a nil slice produces
> `null`; on the consumer's side, `for ev in envelope["eventos"]` blew up in Python and
> `if eventos == []` never matched. See `docs/CONTRATO-CONSUMIDOR.md`, sections *Webhook de CONTA* and
> *Mudanças que quebram*.

Two guards decide whether the event arrives (`internal/inbound/handler.go`, the blocks commented as
*guarda 5a* and *guarda 5b*):

- **5a — `phone_number_id`**: applies to payloads with `metadata.phone_number_id` (`messages`,
  `calls`).
- **5b — `waba_id`**: applies to **account** webhooks, which carry the WABA id in `entry[].id`.

*Consequence measured on 2026-07-28: the panel's **Test** button sends a correctly signed payload,
but with sample ids (`waba_id: "0"`). It passes the signature and **dies at guard 5b**, with
`conta_descartada`. In other words: **the panel's Test is no good for discovering the format through
the gateway** — it is good for proving the guard works. To see the shape, use the sample the button
itself displays, which is what is in this doc.*

---

## Subscribed

> ⚠️ **The list below has NO total count, and the omission is deliberate.** It was surveyed on
> 2026-07-28 with **ten** fields subscribed; at the end of that same day the owner subscribed
> `template_category_update` in the panel (reported in writing, **not measured from here** — see the
> note at the end of this section). Any count written here is a snapshot of a panel someone can touch
> without telling this file. **Only one thing answers "how many and which are subscribed NOW":**
>
> ```
> GET https://graph.facebook.com/v25.0/{app-id}/subscriptions?access_token={app-id}|{app-secret}
> ```
>
> It is a read and changes nothing. The `app-id` is `869733115682937`.
>
> 🔴 **And it is not easy to run from here, which is part of the finding (T-057, 2026-07-28).** The
> `app_secret` is **not** in `/etc/zapgw/env` on CT 125 — that file has `ZAPGW_CHAVE_CIFRA`,
> `ZAPGW_BANCO` and `ZAPGW_ENDERECO`. It lives **encrypted in the database, per instance**, and no CLI
> command decrypts it back — on purpose (T-052: of the instance's four secrets, only `verify_token`
> and `segredo_entrega` are printed, because they need to exist outside the gateway). Whoever measures
> it needs the value from another source; **measured on 2026-07-28 that it is not in the service's
> environment.**

| Field | Modeled? | Reaches the consumer |
|---|---|---|
| `messages` | ✅ yes | message and status events |
| `message_template_status_update` | ✅ yes (T-043) | `tipo: "template_status"` |
| `template_category_update` | ✅ yes (T-057) | `tipo: "template_categoria"` — **subscribed by the owner on 2026-07-28**, after the survey above |
| `phone_number_quality_update` | ✅ yes (T-058) | `tipo: "qualidade_do_numero"` |
| `account_alerts` | ✅ yes (T-058) | `tipo: "alerta_de_conta"` |
| `account_update` | ❌ no | `"eventos": []` |
| `account_review_update` | ❌ no | `"eventos": []` |
| `security` | ❌ no | `"eventos": []` |
| `phone_number_name_update` | ❌ no | `"eventos": []` |
| `message_template_quality_update` | ❌ no | `"eventos": []` |
| `calls` | ❌ no | `eventos: []` — **but it is delivered**, see below |

> **The five `❌` are not a queue, they are a decision — and the decision has an open-ended deadline
> on purpose** (T-058, 2026-07-28). The envelope only **GROWS**: adding a `tipo` later is free,
> removing one later is a contract break. A modeled field with no interested consumer becomes **dead
> vocabulary** that the gateway owes forever, and nobody ever reads. **The order is: first a consumer
> shows up, then the `tipo` is born.** That is written in the contract so the consumer knows it can
> ask.
>
> The modeling order of this round was **by cost**, not by completeness:
> `template_category_update` (immediate money and an appeal window), `phone_number_quality_update`
> (quota — a send failure that points to the wrong place), `account_alerts` (severity). The remaining
> five have neither. **`calls` had an owner's question in front of it; it was answered and the matter
> is CLOSED — see its section below.** One-line summary: none of the gateway's numbers accepts calls,
> enabling requires a limit ≥ 2000 and both are on `TIER_250`, so the event **has no way to arrive**.

## Unsubscribed ones that matter

| Field | Why it matters |
|---|---|
| `message_template_components_update` | warns of a component change, which breaks the assumption about the buttons' `indice`. **Approved by the owner on 2026-07-28** (*"sim, vamos tratar"* — "yes, let's handle it"), together with `template_category_update`; its subscription was **not confirmed** here |

---

## `template_category_update` — SUBSCRIBED on 2026-07-28, and it is what T-043 should be listening to

```json
{"message_template_id": 12345678, "message_template_name": "my_message_template",
 "message_template_language": "en-US",
 "previous_category": "MARKETING", "new_category": "UTILITY",
 "correct_category": "MARKETING", "category_appeal_status": "ELIGIBLE"}
```

T-043 warns about the `UTILITY` → `MARKETING` reclassification by reading the category from
`message_template_status_update`, which is the **approval/rejection** event with the category as an
attribute. **This is the event dedicated to the change**, and it gives what the other one does not:
the **direction** (`previous_` → `new_`) and the **`category_appeal_status`** — the reclassification
is **appealable**, and without receiving the event nobody knows an appeal window exists.
Reclassification to `MARKETING` makes every send more expensive.

**Closed in T-057 (2026-07-28), on two sides that both needed each other:**

- **outside the repository** — the owner subscribed the field in Meta's panel, field by field, with
  the toggle that **does not touch** the Callback URL. This is the owner's statement, **not a
  measurement by this session**: the `GET /{app-id}/subscriptions` query was not made, for the reason
  written in the note of the *Subscribed* section above;
- **inside** — `internal/meta/parse.go` (`templateCategoryEvent`) and `internal/meta/types.go`
  (`TemplateCategory`) model the `value`, and the event goes out as `tipo: "template_categoria"`. The
  sample above was frozen in `testdata/corpus/categoria_de_template_derivado_da_doc.json`, **marked
  as derived from the docs in the file name itself**, until the real capture showed up.

✅ **It showed up, and the derived fixture is gone (T-174, 2026-08-28).** The consumer `consumer-b`
handed over three raw payloads through the channel, frozen in
`testdata/corpus/categoria_de_template_rebaixamento.json`, `..._restauracao.json` and
`..._sem_anterior.json`. **Two findings the panel sample would not have given:** the pair goes and
comes back on the **same** `message_template_id` (`UTILITY → MARKETING` and, ~14.9 h later, the way
back), and **one of the three arrived with no `previous_category`** — which was the case handled by
design decision, with no observation at all. ⚠️ **And none of the three brought `correct_category` or
`category_appeal_status`**, the two fields the paragraph above describes from the documentation;
these are three events from one account, so that is a measurement, not a conclusion about Meta's
behavior.

**Consequence of both halves arriving on the same day:** from 2026-07-28 on, this webhook really
arrives in production, so the model has real traffic to validate it. Until the binary with the model
is deployed, it arrives as an unmodeled account webhook — `cru` delivered, `"eventos": []`, which is
correct and documented behavior.

## `calls` — subscribed, NOT an account webhook, and it IS delivered

```json
{"messaging_product": "whatsapp",
 "metadata": {"display_phone_number": "16505551111", "phone_number_id": "123456123"},
 "calls": [{"id": "ABGGFlA5Fpa", "to": "18005551180", "from": "16315551181",
            "timestamp": 1504902988, "event": "connect"}],
 "contacts": [{"profile": {"name": "test user name"}, "wa_id": "16315551181"}]}
```

It has `metadata.phone_number_id`, so it goes through guard **5a** — and on a real call the id
**matches**, so the event **reaches the consumer** (with no event at all, `"eventos": []`, until it is
modeled). Two things no other field on this list has:

- **it carries personal data** (`contacts[].profile.name`, `wa_id`) in a raw row nobody reads — it
  counts toward retention;
- **`calls` is a LIST and one call generates several events** (`connect` and others). Whoever
  responds to each event sends several messages to the same customer. The trigger has to be **one**
  chosen event, with the rest explicitly ignored.

## 🛑 CLOSED on 2026-07-30 — we are NOT going to model `calls`, and the reason is that it cannot happen

**Owner's decision, 2026-07-30:** the task to model `calls` (T-076) was **closed as done**, with no
code. It is not a deferral by priority: it is that **the event has no way to arrive**.

**The three measured facts that close the matter:**

1. **None of the gateway's numbers accepts calls.** Measured by the owner on the handset, 2026-07-30:
   *"nem o número da Padaria e nem o número da Lojinha tem como fazer ligação para eles."* ("neither
   Padaria's number nor Lojinha's number can be called.")
   Subscribing does **not** generate an event — the call does. With calling disabled, the `calls`
   webhook **never fires**.
2. **It cannot be enabled even "just to document it".** Enabling is a single call and is reversible
   (`POST /<PHONE_NUMBER_ID>/settings` with `{"calling":{"status":"ENABLED"}}`; `DISABLED` undoes it),
   with no review and no approval — **but it requires a messaging limit ≥ 2000**:
   > *"Calling is not enabled by default on a business phone number. To enable calling, you must have
   > a messaging limit of 2000 or above."*
   Source: <https://developers.facebook.com/documentation/business-messaging/whatsapp/calling/call-settings>,
   read on 2026-07-30.
3. **Both numbers are on `TIER_250`** — measured in production on 2026-07-31 02:22 UTC
   (= 2026-07-30 23:22 -03), `zapgw estado --slug …`, `numero_na_meta.limite_de_mensagens`,
   `estado: observado`, `fonte: medicao`. `tenant-two` and `tenant-one`, both `TIER_250` and both
   `GREEN` on quality. **A quarter of the required minimum.**

*And even if it qualified it would not be a quick test: the same page warns that* **"WhatsApp users
may take up to 7 days to reflect those changes"** *— the call button takes a while to appear for
whoever is going to call.*

**What does NOT change with the closure:** the field **stays subscribed** (it costs nothing and there
is no reason to touch it), and the owner's decision of 2026-07-28 — *"call a gente pode assinar e
passar a tratar respondendo com mensagens"* ("we can subscribe to call and start handling it by
replying with messages") — **was not reversed**. It became **unreachable**, which is different: the
day a call can arrive, that is what counts as the starting point.

**What reopens the matter — it needs BOTH:** (a) the number reaching a limit ≥ 2000, which goes
through business verification (**T-003**); and (b) the product decision to **answer** calls, because
enabling means someone — person or automation — starts needing to respond.

**What is already settled and should not be redone on D-day:**

- ✅ **The question that was blocking the design is ANSWERED at the source:** an incoming call
  **opens** the 24 h window and **renews** it on every new call —
  > *"When a WhatsApp user messages you or calls you, a 24-hour timer called a customer service
  > window starts. If the user messages or calls you again before the timer expires, the timer resets
  > to 24 hours."*
  (<https://developers.facebook.com/documentation/business-messaging/whatsapp/messages/send-messages>,
  read on 2026-07-30.) **So replying by message does not require a template.**
- 🔴 **But replying is NOT free**, and the task claimed it was: the same page says *"Service messages
  are billed under the SERVICE pricing category"*. **An open window waives the TEMPLATE, not the
  CHARGE.** The price itself was not checked and should not be written down without checking the
  pricing page.
- ⚠️ **The payload example above was NOT confirmed against real traffic.** It only shows
  `event: "connect"`, and a call generates several events whose names are not listed on any page that
  was opened. **On D-day, the first real payload is worth more than this example**, and it is on that
  payload that the dedup key gets designed.

*Why this closure is written here and does not vanish along with the task: an item that merely
disappears is resurrected by the next reading of the history — which is what happened with
`consultorio`, twice. A closure that is not written down closes nothing.*

**It was NOT modeled in T-058, and the reason is on the record because the task and this doc
diverged.** T-058 said *"antes de modelar, a pergunta é do dono: o número aceita ligação, e existe
alguém para atender? **Não modele antes dessa resposta**"* ("before modeling, the question is the
owner's: does the number accept calls, and is there someone to answer? **Do not model before that
answer**") — and this section already carried the answer, given on the same day: yes, and the answer
is a message. **The task was out of date relative to the doc, not the other way around.** Even so it
was left out, for two reasons that survive the correction:

1. **the question that decides the DESIGN is still open** (the 24 h window). Modeling the event
   without it is modeling half of it: the trigger would exist and nobody would know whether the reply
   can be free-form text;
2. **it is not an account webhook and the work is of a different nature** — guard 5a instead of 5b, a
   LIST of events per call (replying to each one sends several messages to the same customer), and
   personal data in the `cru`. It is a task of its own, not an item in a round of account webhooks.

*Meanwhile it keeps reaching the consumer with `"eventos": []` and with `contacts[].profile.name` and
`wa_id` inside the `cru` — and that already counts toward both consumers' retention, today. It is
stated in the contract, in the account-webhook table.*

## `phone_number_quality_update` — subscribed and MODELED (T-058), it is the number's QUOTA

```json
{"display_phone_number": "16505551111", "event": "ONBOARDING",
 "current_limit": "TIER_250", "old_limit": "TIER_NOT_SET",
 "max_daily_conversations_per_business": "TIER_250"}
```

`current_limit`/`old_limit` give the daily ceiling **and the direction**. A tier downgrade or a
flagged number arrive through here. Without modeling it, a downgrade only shows up when sending
starts failing on a limit — a symptom that points to the wrong place. **The values are preserved
literally** (`"TIER_250"`, not converted to a number: Meta may create a new tier).

**Modeled in T-058** as `tipo: "qualidade_do_numero"` — see `NumberQuality`
(`internal/meta/types.go`), `numberQualityEvent` (`internal/meta/parse.go`) and the event's section in
`docs/CONTRATO-CONSUMIDOR.md`.

**And since T-080 (2026-07-28) it is the SECOND SOURCE of one block of the state.** The
`current_limit` is written to `numero_na_meta.limite_de_mensagens` with `fonte: "webhook"`
(`internal/inbound/handler.go`, `recordNumberLimit`); the other source is the watcher's measurement
(`internal/outbound/watchdog.go`). The tiebreaker is `config.UpdateNumberAtMeta`
(`internal/config/number.go`), by the rule **"the most recent observation wins, whatever the
source"** — and the stamp compared is **our** clock, not `entry.time`, because comparing two
unsynchronized clocks would decide the tiebreak in silence.

> ⚠️ **`current_limit` ≠ `max_daily_conversations_per_business`.** The state stores the **first**,
> which is the same quantity the measurement reads in
> `whatsapp_business_manager_messaging_limit`. It is exactly the swap that this section's synthetic
> fixture exists to catch.

> ⚠️ **In this sample, `current_limit` and `max_daily_conversations_per_business` have the SAME
> value** (`"TIER_250"`). Freezing it as the only fixture would produce a corpus in which swapping
> the reading of one field for the other passes **green** — measured. That is why the corpus also has
> `qualidade_do_numero_sintetico.json`, with the three limits different and a **downgrade**
> (`TIER_1K → TIER_50`), which is the direction the sample does not exercise. See
> `testdata/corpus/README.md`.

## `message_template_quality_update` — subscribed

```json
{"previous_quality_score": "GREEN", "new_quality_score": "YELLOW",
 "message_template_id": 12345678, "message_template_name": "my_message_template",
 "message_template_language": "pt-BR"}
```

A drop in a template's quality precedes it being paused by Meta. It has direction
(`previous_` → `new_`), like the category one.

## `message_template_components_update` — UNSUBSCRIBED

```json
{"message_template_id": 12345678, "message_template_name": "my_message_template",
 "message_template_language": "en-US",
 "message_template_title": "message header", "message_template_element": "message body",
 "message_template_footer": "message footer",
 "message_template_buttons": [{"message_template_button_type": "URL",
                               "message_template_button_text": "button text",
                               "message_template_button_url": "https://example.com",
                               "message_template_button_phone_number": "12342342345"}]}
```

It brings the template's **list of buttons**. It is exactly what would warn that the buttons' position
changed — and the `indice` the consumer sends in `botoes_template` is the position **in the
template**. Today the mapping "matches by convention, not by construction" (recorded by both consumers
on 2026-07-26); this event is what would turn convention into a warning.

## `account_alerts` — subscribed and MODELED (T-058)

```json
{"entity_type": "WABA", "entity_id": 123456, "alert_severity": "INFORMATIONAL",
 "alert_status": "NONE", "alert_type": "OBA_APPROVED",
 "alert_description": "Sample alert description, informational in nature with no status"}
```

The existence of `alert_severity` implies severities above `INFORMATIONAL` — and those are the ones
that matter.

**Modeled in T-058** as `tipo: "alerta_de_conta"` — see `AccountAlert` (`internal/meta/types.go`) and
`accountAlertEvent` (`internal/meta/parse.go`). `alert_severity`, `alert_type` and `alert_status` are
passed through **as they came**: the gateway does not order severities and does not derive "serious"
from anything, because ordering a third party's vocabulary requires knowing the whole list and nobody
here knows it.

**This sample got NO synthetic sibling**, and the decision is the opposite of the one for
`phone_number_quality_update` just above, from the same question: the fields that go into the key
(`entity_id`, `alert_type`, `alert_severity`, `alert_status`) already come with values **different
from each other** here, so it alone catches a swapped field read. A synthetic one "for symmetry"
would be ceremony with no guarantee.

⚠️ **`entity_id` comes as a NUMBER** (`123456`, unquoted) and leaves the gateway as **text** — the
same tolerance as `message_template_id`, which in the real capture has 16 digits and does not fit in
`int32`.

## `account_update` — subscribed

```json
{"phone_number": "16505551111", "event": "VERIFIED_ACCOUNT"}
```

## `history` — UNSUBSCRIBED, and probably does not apply here

It delivers **old conversations in chunks** (`metadata.phase`, `chunk_order`, `progress`), with
`threads[].messages[]` and `history_context.from_me`. It is the history import for whoever migrates
**from the WhatsApp Business app** to the Cloud API. `consumer-b`'s migration came from Evolution, not
from the app, so it does not apply — it is on the record for the day someone migrates a number that
has history on the handset.

---

## Fields that exist and have never been looked at

From the panel, on 2026-07-28, all **unsubscribed**: `account_settings_update`, `automatic_events`,
`business_capability_update`, `business_status_update`, `business_username_updates`, `flows`,
`group_lifecycle_update`, `group_participants_update`, `group_settings_update`,
`group_status_update`, `message_echoes`, `messaging_handovers`, `partner_solutions`,
`payment_configuration_update`, `smb_app_state_sync`, `smb_message_echoes`, `standby`,
`template_correct_category_detection`, `tracking_events`, `user_preferences`.

**Do not subscribe to them out of curiosity.** A subscribed and unmodeled field becomes a raw row in
a consumer's production database — volume and data stored with no use. Subscribe when there is
someone to consume it.
