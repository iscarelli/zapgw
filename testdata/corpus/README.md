# Meta payload corpus

**Real** payloads, with numbers, ids and names swapped for test values. The **format** is
preserved byte for byte — that's what's being tested, not the content.

**Never put personal data here.** When adding a new payload: swap `wa_id`, `from`,
`recipient_id`, `user_id`/`from_user_id`/`recipient_user_id`, `id` (wamid) and `profile.name` for
fictitious values, and check that no real message text survived.

> 🔴 **`wamid` is mandatory in the list above, and not as a precaution: it CARRIES the recipient's
> phone number.** `wamid.` is followed by base64, and `base64 -d` on what comes after the dot
> returns the number in plain text. Swapping `recipient_id` and leaving the `wamid` leaks the
> number the same way, and the file looks masked. **An opaque field is not a field without
> content — decode it before letting it through.** (Measured while masking the T-069 capture; see
> the dedicated note at the end of this file.)

Each file carries, in its name, what it proves. A file with no test consuming it shouldn't exist:
`corpus_test.go` fails if any `.json` here isn't exercised.

## Origin of each file

The marking is **per file**, not per batch — a mixed batch (this README's earlier form) turns
into "they're all real" in a fast reader's head. Every file below is one of these three:

- **capture**: came from real Meta traffic, observed by a consumer in production
  (`consumer-a`), with sensitive values masked on purpose (rounded coordinates, media
  `id`/`sha256`/`url` replaced by a placeholder). The **shape** is literal, byte for byte; the
  **values** that would point to a real person are not.
- **derived from the doc**: no real capture was available; the shape was copied from the examples
  published by Meta's official documentation, with the ids swapped for this corpus's fictitious
  pattern (`WABA_TESTE`, `PNID_TESTE`, `wamid.TESTEnnn`).
- **synthetic**: not a payload Meta sent nor one the doc described — written by hand to exercise
  a path that neither a capture nor the doc covers alone (e.g.: `body_null.json` proves the guard
  on `json.Unmarshal("null", &map)`; `template_button_synthetic.json` exists because the real
  capture has `payload == text` and wouldn't catch a swapped field read on its own).

## Backing per STATUS — all four have a real capture since T-069 (2026-07-28)

**Read this before using the corpus to validate a status mapping.** The "Origin" column of the
table below is per FILE; it answers *"where did this file come from?"* and does **not** answer
the question the integrator actually asks, which is *"what am I testing my `sent` against?"*.
This section answers that one.

| Status | File in the corpus | Backing |
|---|---|---|
| `sent` | `status_sent_with_pricing.json` and `status_sent_without_pricing.json` | ✅ **capture** — consumer-a, 2026-07-28 (T-069). There are **two** files because the measurement found **two shapes**: 49 of 53 raw `sent` had `pricing`, **4 didn't** |
| `delivered` | `status_delivered.json` | ✅ **capture** — consumer-a, 2026-07-28 (T-069). 49 raw `delivered`, **49 with `pricing`** |
| `read` | `status_read_with_billing.json` | ✅ real capture (partial) — consumer-a, 2026-07-26 |
| `failed` | `status_failed.json` | ✅ real capture — consumer-a, 2026-07-26 |

**Until 2026-07-28 this section said the opposite, and what it said is worth keeping as a
warning**, because the same trap comes back with every new type: `sent` had no fixture at all
(`grep -rln '"sent"'` on the corpus came back **empty**) and `delivered` had a **derived-from-doc**
one. Whoever tested against that `delivered` proved they agree with Meta's **documentation**, not
with what Meta **sends** — it's the *"a doc example is code nobody runs"* family
(`docs/ARMADILHAS.md`) one level up: there the example became a **fixture**, and a green fixture
**looks like proof**.

**The capture confirmed the suspicion was worth it: the doc-derived `delivered` was wrong on two
observable points.** It didn't carry `pricing` (the real one does, 49 of 49) and it carried a
`conversation` block that **none of the three real captured payloads has**. Neither changes
behavior — `conversation` was never read by the parser, and the absence of `pricing` was handled
— and that's exactly why the case is instructive: a fixture can be wrong about the shape without
any test going red.

**Why the `sent` gap survived so long unnoticed — and the structural lesson STILL HOLDS, because
nothing changed in the mechanism:** `TestTheWholeCorpus` (`internal/meta/corpus_test.go`) sweeps
the files that **exist** and requires each one to have a test. Nothing requires the opposite —
**that every status have a file**. The guard protects against an orphan file and is blind to a
missing status. If Meta creates a new state tomorrow, it vanishes from the corpus exactly the way
`sent` did.

**The four names aren't a closed vocabulary in the code.** The parser passes `status` through as
it came (`internal/meta/parse.go:statusEvent`, and the value enters the event's key in the same
function); the list `sent, delivered, read, failed` exists **only as a comment** in
`internal/meta/types.go`. In other words: there is, today, no place in the code that could go red
because of a new status or a status with no fixture.

| File | Origin |
|---|---|
| `text_message.json` | **capture** — consumer-a, 2026-07-26 (T-031). Confirms `from_user_id` and `contacts[].user_id` (format `BR.<digits>`) in real traffic — fields that do NOT appear in the doc's classic examples |
| `template_button.json` | **capture** — consumer-a, 2026-07-26 (T-031). A real template quick-reply, tapped on the device: `type: "button"`, and `payload`/`text` came **equal** (`"Falar com a gente"`) — see `template_button_synthetic.json` below |
| `template_button_synthetic.json` | **synthetic** (T-031). Same shape as the capture above, but with `payload` and `text` DIFFERENT on purpose — it's what catches a swapped field read, which the capture (equal values) doesn't catch on its own |
| `interactive_button.json` | **capture** — consumer-a, 2026-07-26 (T-033). `button_reply.id` (`"confirmar"`) and `button_reply.title` (`"Confirmar"`) come DIFFERENT — enough to tell a swapped field read apart on its own; **has no synthetic sibling** (see note below) |
| `reaction.json` | **capture** — consumer-a, 2026-07-26 (T-026). Real reaction with emoji `❤️` (`U+2764 U+FE0F`, two code points — see note below) |
| `reaction_removed.json` | **capture** — consumer-a, 2026-07-26 (T-026). The observed pair of the line above: same reaction (same target), undone 20s later. The `emoji` key doesn't exist in the payload — not `""`, not `null` |
| `location.json` | **capture** — consumer-a, 2026-07-26 (T-026). A dropped pin: **without** `name`/`address` — the common case observed, unlike the earlier fixture (derived from the doc), which had both and tested the rare case |
| `audio_voice_note.json` | **capture** — consumer-a, 2026-07-26 (T-026). `"voice": true` and `mime_type: "audio/ogg; codecs=opus"` (with the parameter) confirmed in the real payload |
| `image.json` | **capture** — consumer-a, 2026-07-26 (T-026) |
| `video.json` | **capture** — consumer-a, 2026-07-26 (T-026) |
| `document_with_caption.json` | **capture** — consumer-a, 2026-07-26 (T-030). `caption` and `filename` both come, side by side, and the `filename` is the customer's real (long, with hyphens and numbers) file name |
| `forwarded_message_synthetic.json` | **synthetic** (T-059, 2026-07-28). No real capture of these fields exists — `grep -rl forwarded testdata/corpus/` found nothing before this file. `context.forwarded` and `context.frequently_forwarded` come with DIFFERENT values from each other (`true`/`false`), for the same reason `template_button_synthetic.json` exists; and the `context` **has no `id`**, because forwarding isn't quoting |
| `message_reply.json` | **capture** — consumer-a, 2026-07-26 (T-032). A text message replying to (quoting) another: carries `context.id` with the quoted message's `wamid`, alongside `context.from` (the BUSINESS's number — not modeled, see `types.go`) |
| `status_sent_without_pricing.json` | **capture** — consumer-a, 2026-07-28 (T-069). The `sent` **without** the `pricing` block: 4 of 53 raw `sent` measured (~7.5%). It's the file that proves `pricing` is optional; see the dedicated note below |
| `status_sent_with_pricing.json` | **capture** — consumer-a, 2026-07-28 (T-069). The common shape of `sent` (49 of 53), with `pricing` `{"billable":false,...,"category":"service"}`. Same `wamid` and same `timestamp` as `status_delivered.json` — on purpose, see the dedicated note below |
| `status_delivered.json` | **capture** — consumer-a, 2026-07-28 (T-069). **Replaced** the doc-derived fixture that lived here (doesn't coexist with it). Comes with `pricing` (49 of 49 in their corpus) and **without** the `conversation` block the derived one had |
| `status_failed.json` | **capture** — consumer-a, 2026-07-26 (T-033). It's the real failure from 2026-07-20 (ticket LR-00014, `code 131026`, `"Message undeliverable"`) that triggered the operator alert that gave rise to T-028; it used to be derived from the doc's generic example (`code 131049`) |
| `status_read_with_billing.json` | **capture (partial)** — consumer-a, 2026-07-26 (T-041), pasted in the bilateral channel (`consumer-a-STATUS.local.md`, gitignored). The excerpt `{"status":"read","pricing":{"billable":true,"pricing_model":"PMP","category":"utility","type":"regular"}}` is literal, exactly as they pasted it; wrapped here in this corpus's standard envelope (`WABA_TESTE`/`PNID_TESTE`/`wamid.TESTE017`) because what was pasted didn't include the `entry`/`changes`/`metadata` level |
| `template_status.json` | **capture (partial)** — consumer-a, 2026-07-26 (T-043). The whole `change` (`field` + `value`) is literal: one of 21 samples they had kept on disk since before the migration. The `entry` level (`id`/`time`) is this corpus's standard envelope, because what was delivered didn't include that level — and `time` **is read** by the parser (it enters the event's key), so it isn't decoration: see the dedicated note below |
| `context_wrong_type_synthetic.json` | **synthetic** (T-061, 2026-07-28). `context` comes as a **string** where an object is expected. Nobody observed Meta doing this; the file describes what the gateway has to WITHSTAND, not what it sends — see the dedicated note below |
| `context_field_wrong_type_synthetic.json` | **synthetic** (T-061, 2026-07-28). `context` is an object, but `id` comes as a **number** where a string is expected and `forwarded` comes as a **string** where a boolean is expected. It's the case the file above doesn't cover: there the parser doesn't even enter the block |
| `audio_voice_wrong_type_synthetic.json` | **synthetic** (T-061, 2026-07-28). Audio with `voice` as a **string** where a boolean is expected — the older sibling of `context`'s same defect (the fragile shape of `voice` came from stage 1) |
| `text_wrong_type_synthetic.json` | **synthetic** (T-062, 2026-07-28). `"text":"oi"` — a string where an object is expected, on the most common message type of all. Carries a **sane sister** in the same batch; see the dedicated note below |
| `audio_wrong_type_synthetic.json` | **synthetic** (T-062, 2026-07-28). The **whole media block** with the wrong type (`"audio":"MEDIA_TESTE10"`), one level above the `voice` that T-061 closed. Sane sister in the same batch |
| `interactive_wrong_type_synthetic.json` | **synthetic** (T-062, 2026-07-28). `"interactive":"button_reply"` — a string where an object is expected. Sane sister in the same batch |
| `reaction_wrong_type_synthetic.json` | **synthetic** (T-062, 2026-07-28). `"reaction":"wamid.TESTE001"` — a string where an object is expected. It's the file that separates a **missing block** from an **unreadable block**: the "a reaction with no target is malformed" guard still holds, and it doesn't reach this case. Sane sister in the same batch |
| `button_wrong_type_synthetic.json` | **synthetic** (T-062, 2026-07-28). `"button":"Falar com a gente"` — a string where an object is expected, in a reply to a template button (the path that works outside the 24h window). Sane sister in the same batch |
| `metadata_wrong_type_synthetic.json` | **synthetic** (T-068, 2026-07-28). `"metadata":"PNID_TESTE"` — a string where an object is expected, one level ABOVE the message. Carries a message **and** a status in the same `change`, because it was the WHOLE `change` that died |
| `contacts_wrong_type_synthetic.json` | **synthetic** (T-068, 2026-07-28). `"contacts":"Fulana de Teste"` — the most expensive of the five measured: it erased a **customer's whole batch of messages**. Message and status in the same `change` |
| `field_wrong_type_synthetic.json` | **synthetic** (T-068, 2026-07-28). `"field":42` in the first `change`, with a second, sane `change`. It's the **only one of the six that still returns `ErrPartialParse`** — see the dedicated note below |
| `entry_id_wrong_type_synthetic.json` | **synthetic** (T-068, 2026-07-28). `"id":42` in the first `entry` (the `waba_id`), with a SECOND, sane `entry` — which is how Meta batches different accounts in the same call |
| `status_wrong_type_synthetic.json` | **synthetic** (T-068, 2026-07-28). `status`, `recipient_id` and `timestamp` of an unexpected type in the first status, with a sane sibling status. The numeric `timestamp` **survives** (it's the tolerant exception); the other two degrade to empty |
| `template_wrong_type_synthetic.json` | **synthetic** (T-068, 2026-07-28). `message_template_name` a number and `reason` an object, with a second, sane template `change`. The `event` and the `message_template_category` — what makes the event worth having — survive |
| `template_category_downgrade.json` | **capture** (T-174, 2026-08-28, provided by consumer `consumer-b`). A real `template_category_update`: `UTILITY → MARKETING` on `instrucoes_download_app_v6`, `entry.time` 1787252135 (2026-08-20 18:55:35 UTC = 15:55:35 -03). The whole body, unreformatted; the only swap at the source was `waba_id` for `WABA_TESTE`. See the dedicated note below |
| `template_category_restore.json` | **capture** (T-174, 2026-08-28, same origin). The **return** of the SAME `message_template_id`: `MARKETING → UTILITY`, `entry.time` 1787305767 (2026-08-21 09:49:27 UTC = 06:49:27 -03). Only the pair proves the round trip goes out with different dedup keys |
| `template_category_no_previous.json` | **capture** (T-174, 2026-08-28, same origin). Arrives **without `previous_category`** (`teste_sonda_503_20ago`, `new_category: MARKETING`, `entry.time` 1787244576). One in 18 events the consumer kept; it's the first real evidence of the case `parse.go` handled by a project decision |
| `template_category_synthetic.json` | **synthetic** (T-057, 2026-07-28). Same shape, with `previous_category`, `new_category` and `correct_category` DIFFERENT from each other — in the panel's sample, `previous` and `correct` come **equal** (`MARKETING`), so it alone doesn't catch a swapped field read. Same reason `template_button_synthetic.json` exists. **It survived the captures arriving (T-174), and not out of habit:** none of the three carries `correct_category` or `category_appeal_status`, so it's the only file that still exercises those two fields |
| `number_quality_derived_from_doc.json` | **derived from the doc** (T-058, 2026-07-28). Sample from the panel's *Test* button for `phone_number_quality_update`, frozen byte for byte. The `display_phone_number` (`16505551111`) is Meta's **own fictitious number**, preserved from the sample — it's nobody's number, and there is no real phone number in this file |
| `number_quality_synthetic.json` | **synthetic** (T-058, 2026-07-28). The THREE limits different from each other (in the sample `current_limit` == `max_daily_conversations_per_business`), and the **expensive** direction: `TIER_1K → TIER_50`, a downgrade. See the dedicated note below |
| `account_alert_derived_from_doc.json` | **derived from the doc** (T-058, 2026-07-28). Sample from the panel's *Test* button for `account_alerts`. **Has no synthetic sibling**, and the absence is a decision: the fields that enter the key already come with different values from each other in the sample, so it alone catches a swapped field read (same decision as `interactive_button.json`) |
| `body_null.json` | synthetic (not a Meta payload — proves the guard on `json.Unmarshal("null", &map)`) |

## About the files DERIVED from the documentation — **two remain, and their name says so**

> 🔴 **This section said "none remain" until 2026-07-28.** T-057 and T-058 added
> `template_category_derived_from_doc.json`, `number_quality_derived_from_doc.json` and
> `account_alert_derived_from_doc.json`, and all three were derived **for lack of an
> alternative** — they are ACCOUNT webhooks whose traffic is rare by nature (a category
> reclassification, a tier downgrade and an account alert don't happen every week), and
> `template_category_update` was even **unsubscribed** in the App. No consumer had a sample kept.
>
> ✅ **One of the three is already gone: T-174 (2026-08-28) deleted
> `template_category_derived_from_doc.json`** and put three real captures in its place
> (`..._downgrade`, `..._restore`, `..._no_previous`), provided by consumer `consumer-b`. **It
> doesn't survive alongside them** — that's the rule right below, applied. Still derived:
> `number_quality_derived_from_doc.json` and `account_alert_derived_from_doc.json`.
>
> **The file name carries the marking.** The origin table is the record, but whoever opens the
> directory sees the file before opening this README, and a derived fixture that looks like a
> capture is exactly what this section exists to keep from happening again.
>
> **The replacement rule holds for the two that remain:** when a real capture shows up, it
> **replaces** the file — the two don't coexist.


For T-023 (reaction, location, caption/file name) and T-028 (failure reason) no real payload was
available when those tasks were done — no consumer had exercised those paths against the real
Meta yet. Those files **weren't real captures**: the shape was copied from the examples published
by Meta's official documentation, with the ids swapped for the rest of the corpus's same
fictitious pattern, and location name/address (when present in an example) invented.

They kept getting replaced by captures as real traffic showed up:
`interactive_button.json` and `status_failed.json` in T-033 (2026-07-26), **`status_delivered.json`,
the last of that batch, in T-069 (2026-07-28)**, and `template_category_derived_from_doc.json` in
T-174 (2026-08-28). Counting today, **two files in the table above are derived from the doc**
(`number_quality_derived_from_doc.json` and `account_alert_derived_from_doc.json` — T-058); all
the others are *capture* or *synthetic*.

> **The justification that propped up the derived `delivered` was comfort, not argument — and the
> capture proved the doubters right.** It said there would be nothing to confirm in a real
> `delivered`, because *"`delivered` with no reason is exactly the happy case"*. The sentence
> reasons about the `errors[]` field, which indeed wouldn't show up — and concludes, with no
> basis, about the **whole payload**. T-051 (2026-07-28) had already flagged it as fragile; T-069,
> hours later, showed the derived one got the shape wrong in two spots (`pricing` missing,
> `conversation` present). **"Nothing to confirm" is only known after capturing** — and every
> finding in this corpus is something nobody could have predicted by reading the doc:
> `from_user_id`/`user_id` in `text_message.json`, `reason: "NONE"` and the absence of `metadata`
> in `template_status.json`, the `emoji` key **absent** in `reaction_removed.json`, and now the
> optional `pricing` on `sent`.

If a derived file is ever born here again (a new type with no capture), the rule holds: when the
real one shows up, **replace** the file and update the table above — don't let the two coexist.

## About the real CAPTURE files (T-026, 2026-07-26)

`consumer-a` captured a batch of real messages sent by the test number's owner: text, emoji,
location, image, audio and video — and, in a separate capture the same day, a reaction and its
removal 20 seconds later. Sensitive values were masked **by them**, before pasting into the
bilateral channel (`consumer-a-STATUS.local.md`, gitignored): rounded coordinates, media
`id`/`sha256`/`url` replaced by a placeholder — because the real location points to a person's
home, and `lookaside.fbsbx.com` URLs are temporary file-access credentials. The shape (which keys
exist, at what level, with what type) is literal.

**`reaction_removed.json` is the most important file in the batch**: it's the only case where the
event's whole meaning is in the ABSENCE of a key, and the only fact from this task that couldn't
be confirmed by reading the doc — only by someone watching the reaction disappear and capturing
the payload.

## About `document_with_caption.json` (T-030, 2026-07-26)

It was the last message-with-media fixture still marked "derived from the doc" — no consumer had
sent a document with a caption until then. `consumer-a` captured one off the wire and pasted it
into the bilateral channel (`consumer-a-STATUS.local.md`, gitignored), with the same masking as
the T-026 batch: `sha256`/`id`/`url` replaced by a placeholder (`lookaside.fbsbx.com` URLs are
temporary file-access credentials). The shape — which keys exist, at what level, with what type —
is literal; `caption` and `filename` are the real observed text.

The capture confirms the same structure the doc already predicted (`caption` and `filename` side
by side), so it isn't a shape finding — but it replaces a weak test value with a realistic one:
the doc-derived `filename` was `recibo-teste.pdf` (short, no hyphen, no number); the captured one
is `515642-9741-manual-forno-gourmet-grill-rev-43.pdf`, the customer's real file name. A fixture
with a short name would never have exercised a long one, with hyphens and digits.

**This sentence was corrected on 2026-07-26 (T-031) after coming out wrong once.** An earlier
version of this paragraph said "no message fixture describes the doc" as if it were absolute —
the real qualifier, said by consumer-a, was "the last derived one **that matters**", and the swap
to an absolute was made without counting a single file. Counting at the time (T-031): of the
MESSAGE fixtures, only `interactive_button.json` was still "derived from the doc"
(`text_message.json` and `template_button.json` had become captures in the same task);
`status_failed.json` was left out of the count for being a STATUS fixture, not a message one.
**T-033 (2026-07-26) closed the two that remained** — see the dedicated note further below — so,
from then on, no MESSAGE fixture in the corpus is derived from the doc; `status_delivered.json`
(a STATUS fixture) was the last of all, and it fell in T-069 (2026-07-28).

## About `text_message.json` and `template_button.json` (T-031, 2026-07-26)

consumer-a captured two fixtures that were still "derived from the doc" and both brought
something the derived one didn't have:

**`text_message.json`** — the real payload arrived with `contacts[].user_id` and
`messages[].from_user_id`, format `BR.<digits>`, a user identity. No classic example in Meta's doc
shows these fields, and nobody knows since when they exist in real traffic. This breaks nothing
today — `encoding/json` silently ignores an unknown field — but until this task the corpus **had
never exercised** that path: a corpus that only contains already-known fields can't fail on a new
field, which is exactly the scenario it exists for. The guarantee is now proven by test
(`TestParseWebhookAnUnknownFieldDoesNotBringDownTheParse`, `internal/meta/parse_test.go`), not
just by `encoding/json`'s accident.

**Explicit decision: `user_id`/`from_user_id` do NOT enter the envelope** (`Event`, in
`types.go`). It's personal identity data, no consumer asked for it, and the envelope only grows —
adding it later is free, removing it later breaks the contract. The same test that proves the
field doesn't bring down the parse also proves it doesn't leak into the envelope.

**`template_button.json`** — the real template quick-reply brought `payload` and `text` **equal**
(`"Falar com a gente"`). A fixture like this passes green even if the parser reads the wrong field
(the two values coincide) — it's the same family as the "leak test whose fixture erased the
branch that would leak" (`docs/ARMADILHAS.md`). That's why `template_button_synthetic.json`
exists: same shape, `payload` and `text` DIFFERENT on purpose. Proven by mutation: swapping
`e.ButtonPayload = button.Payload` for `button.Text` in `internal/meta/parse.go` leaves
`TestParseWebhookASyntheticTemplateButtonDistinguishesPayloadFromText` red while the capture's test
(`TestParseWebhookATemplateButtonCaptureHasPayloadEqualToText`) stays green — exactly because the
capture's values are equal.

**Two non-findings, checked before writing the task (not assumed):** the parser doesn't read
`context` (present in the button's real payload, but not modeled in `messageMeta`), so the
original message pointed to by `context.id`/`context.from` doesn't affect anything here; and
`type: "button"` was already handled as its own path before this task (`internal/meta/parse.go`),
with a comment already saying a template quick-reply doesn't arrive as
`interactive.button_reply`. Neither became a finding because neither diverged from what was
expected.

**This note went stale on 2026-07-26 (T-032): the parser started reading `context`.**
`consumer-a` asked for the field — it's the last thing they needed to remove the private library
that existed only because of the incomplete envelope. `template_button.json` (above) had already
carried `context` since T-031 and had never been read; now it is: `context.id` becomes
`Event.ReplyTo` (`reply_to` in the JSON), with the SAME field name as the equivalent field on
send (`Request.ReplyTo`) — reasoning in T-024. `context.from` stays out, and now by explicit
decision, not by accident: it's the BUSINESS's number, not the customer's, and a field that looks
like "who from" and is "who to" is an invitation to bugs.

## About `message_reply.json` (T-032, 2026-07-26)

> **The absence of `reply_to` is the NORMAL case — including in a real reply.** Observed on
> 2026-07-26 (consumer-a, two payloads from the same conversation, 3 min apart): replying by
> **holding the bubble** generates `context`; replying by **typing in the notification** (inline)
> generates a payload **without `context`**. Meta doesn't send the link in that case. Since
> replying from the notification is the fastest path on a phone, the absence is probably the
> majority of the traffic.
>
> **This fixture only covers the case WITH a quote.** There's no capture of the inline case here
> — whoever writes an absence test today should use a synthetic payload and mark it as such. If a
> capture of the inline case shows up, it goes in and this note becomes a pointer to the file.

Real capture from consumer-a: the owner replied by quoting an earlier message (holding the bubble
and typing "Recebido"). The `cru` carried `context.from` (the business's number) and `context.id`
(the quoted message's `wamid`) inside `messages[0]`. The two values are DIFFERENT — which makes
this fixture also the proof of T-032's mandatory mutation: if the parser read `context.from`
instead of `context.id`, `Event.ReplyTo` would come out with the business's number
(`5532999990000`) instead of the expected `wamid` (`wamid.TESTE001`), and the test comparing the
exact value (not just the field's presence) goes red. The same distinction already existed,
unused, in `template_button.json` (`context.from` = `5532999990000`, `context.id` =
`wamid.TESTE013`).

## About `interactive_button.json` and `status_failed.json` (T-033, 2026-07-26)

The last two message/status fixtures still marked "derived from the doc" fell in the same task:
consumer-a audited the 218 raw payloads they have recorded (counting the keys Meta sends against
the ones our code reads — the same "count, don't estimate" discipline as this network) and found
real captures for both.

**`interactive_button.json`** — consumer-a had said, in an earlier cycle, that this traffic didn't
exist (a non-template interactive message had never been sent). The count proved otherwise: there
are **three**. The captured `button_reply` has `id` (`"confirmar"`) DIFFERENT from `title`
(`"Confirmar"`) — the two only differ in capitalization, but that's already enough for a Go string
comparison (which is case-sensitive) to tell a swapped field read apart on its own. **That's why
this fixture did NOT get a synthetic sibling** the way `template_button_synthetic.json` did: there
the capture had `payload == text` byte for byte, so it didn't catch the mutation on its own; here
it does. Adding a synthetic one "for symmetry" with the template one would be ceremony with no
guarantee — the same question from `docs/ARMADILHAS.md`: "does this field/file buy any behavior
difference, or does it just look like it should because a similar one needed it?". The capture
also brought `context` (not tested by this fixture — `message_reply.json`, above, is already what
proves reading `context.id`).

**`status_failed.json`** — it's the real failure from 2026-07-20 (ticket LR-00014, `code 131026`,
`"Message undeliverable"`) that triggered the operator alert that gave rise to all of T-028; it
used to be derived from the official doc's generic example (`code 131049`, a different code from
the incident the task existed to solve). The capture confirms field by field what T-028/T-029
already modeled: `code`, `title`, `message` and `error_data.details` exist, and **`title` and
`message` come with the SAME value** (`"Message undeliverable"`) — the cut of keeping only
`code`/`message` (not both) is still right, and `details` (`"Message Undeliverable."`) is still
the only part that adds information beyond the title.

**What this task added to the PARSER, not just the corpus:** Meta also sends `errors[]` INSIDE
`messages[]` (not just in `statuses[]`), under the `"unsupported"` sub-type — Meta received
something the Cloud API can't represent (`code 131051`, `"Message type unknown"`, shape confirmed
at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/,
read on 2026-07-26). Before T-033, `messageMeta` had no `Errors` field, so an `unsupported`
message arrived with a sub-type and an id, and nothing else — indistinguishable from "empty
message" to the consumer. The gateway reuses the SAME `StatusError` and the SAME `Event.Error`
field the status event already uses (see `internal/meta/types.go`), because the `errors[]` item's
shape is identical in both places — only the MEANING changes with the event's type, and that
difference is documented in the code and in `docs/CONTRATO-CONSUMIDOR.md`. There's no corpus
fixture for this case (no real capture has arrived yet); the tests covering `sub_tipo:
"unsupported"` in `internal/meta/parse_test.go` use a synthetic payload, with the official Meta
example's values quoted above.

## About `forwarded_message_synthetic.json` (T-059, 2026-07-28)

It's the only message fixture in the corpus with **no backing at all in observed traffic** —
neither a capture, nor a doc example. Both were searched before writing it: none of the payloads
kept by the consumers carries `forwarded` (`grep -rl forwarded testdata/corpus/` comes back empty
without this file), and Meta's public documentation, searched on 2026-07-28, **no longer has a
page describing the `context` fields** — the webhook reference pages that exist today don't
mention them. It describes, therefore, the shape the gateway **decided to read**, not an observed
shape.

**A consequence worth taking seriously, and the reason this note exists:** a consumer testing the
forwarding mapping only against this file proves they agree with **our own assumption**, not with
what Meta sends. It's the same family as T-051 (`sent` with no capture, `delivered` derived from
the doc), one degree worse. When a real capture of a forwarded message shows up — both consumers
are in production receiving traffic, and forwarded messages are common —, it **replaces** this
file, it doesn't coexist with it.

**Both fields come with different values from each other on purpose** (`forwarded: true`,
`frequently_forwarded: false`): with the two equal, swapping the read of one for the other would
pass green. Proven by mutation (T-059): swapping `m.Context.Forwarded` for
`m.Context.FrequentlyForwarded` in `internal/meta/parse.go` leaves this fixture RED on both
assertions at once, while `TestParseWebhookAChainMessageMarksBothForwardingFields` (a synthetic
payload with both `true`) stays green — exactly the asymmetry that justifies both tests existing.

This file's `context` **has no `id`**: forwarding isn't quoting, and the two cases are independent
on Meta's side. That's why the corpus test also requires `reply_to` to be absent here.

## About the three WRONG-TYPE files (T-061, 2026-07-28)

They're the only files in the corpus **malformed on purpose** — and the distinction matters when
reading them: the others describe what Meta *sends*; these describe what the gateway *has to
withstand*. Neither consumer observed Meta sending the wrong type in these fields, and nothing
here claims it does.

**What they prove is a single line, and it was inverted until 2026-07-28:** `err == nil` and
`len(evs) == 1`. Before T-061, a `context` (or a `voice`) of an unexpected type brought down the
**whole message's** `json.Unmarshal` — it turned into `ignored++` and vanished from the `events`
list, with no alarm and no counter, with `200` answered to Meta (which therefore never resends).
In other words: a green test here isn't "the parser read a weird field", it's **"the customer's
message got through"**.

The three are separated on purpose, one path per file: an entirely unreadable block (`context` as
a string), an unreadable field **inside** a readable block (`id` a number, `forwarded` a string)
and the twin at another level of the tree (`voice` a string, inside media). Merging two into one
file would keep a red test from saying which path broke.

**Mandatory mutation (T-061), made and reverted before the commit:** reverting `messageMeta.Context`
to a flat struct leaves the first two red; reverting `mediaMeta.Voice` to `*bool` leaves the third
one. Details of the four mutations — including the two that prove decisions no malformed payload
would catch on its own — are in `docs/ARMADILHAS.md`, "Go / JSON" section.

## About the five WRONG-TYPE-BY-MESSAGE-KIND files (T-062, 2026-07-28)

They're the direct continuation of the three above, and the difference between the two batches is
the task's lesson: T-061's cover **two fields**; these cover **one class**. Measured with
`ParseWebhook` before T-062, an unexpected type value in **any** field of `messageMeta` — not
only the five that give these files their name — turned the message into `ignored++` and made it
**vanish from `events`**. `"text":"oi"` is the scary case: it's the most common type of all.

**Each file has TWO messages, and the second is what the T-061 batch didn't have: the sane
sister.** The assertion that holds across the five is `len(evs) == 2` — the broken one degrades
and gets through, the sister arrives intact. One file per type (not a single batch with all five)
because a red test has to say **which** type broke.

**What these files do NOT prove, and it's on purpose:** they cover five fields, not the whole
struct. The class-level guarantee isn't here — it's in two tests in
`internal/meta/parse_test.go`: `TestParseWebhookNoFieldOfTheWrongTypeErasesTheMessageNorItsSiblings`
(sweeps **the payload's own keys**, type by type, with two mutants each) and
`TestMessageMetaIsolatesEveryFieldByConstruction` (walks the struct by reflection and goes red the
day someone hangs a field there that isn't `json.RawMessage`). A fixture covers the case someone
thought of; those two cover what nobody has written yet.

**One exception still exists and is written down:** an `id` of an unexpected type **still**
erases the message (`TestParseWebhookAnIdOfTheWrongTypeStillErasesTheMessage`). Without a wamid
there's no dedup key, and `42` doesn't become the wamid `"42"` — inventing a wamid would make the
consumer reply to a message that doesn't exist.

## About the six WRONG-TYPE-AT-LEVELS-ABOVE-THE-MESSAGE files (T-068, 2026-07-28)

Third batch in the same family, and its difference from the previous two is the **radius**. T-061's
and T-062's are all inside `messages[]`: what got lost was one message. These sit at the levels
that **contain** the message, and there what got lost was the whole batch — measured with
`ParseWebhook` before the task, one field swapped at a time, with a good message + good sister in
the same batch:

| File | Field swapped | Before T-068 |
|---|---|---|
| `metadata_wrong_type_synthetic.json` | `value.metadata` | `len(evs) = 0` — the whole `change`, messages **and** status |
| `contacts_wrong_type_synthetic.json` | `value.contacts` | `len(evs) = 0` — same |
| `field_wrong_type_synthetic.json` | `change.field` | `len(evs) = 0` — the whole `change` |
| `entry_id_wrong_type_synthetic.json` | `entry.id` (`waba_id`) | the whole `entry` vanished (siblings from OTHER `entry`s survived) |
| `status_wrong_type_synthetic.json` | `status.status`/`recipient_id`/`timestamp` | the status vanished (the sibling survived) |
| `template_wrong_type_synthetic.json` | `template.message_template_name`/`reason` | `len(evs) = 0` — the template event |

**`contacts` is the worst, and worse than the defect T-062 had just fixed:** a `"contacts":"x"`
Meta might send in a new format erased a customer's whole batch of messages, silently, with `200`
answered to Meta — which is `docs/ARMADILHAS.md`'s Critical #1 under another name.

**Two of these files claim more than "didn't vanish", and that's why they exist separately:**

- **`field_wrong_type_synthetic.json` is the only one that still returns `ErrPartialParse`**, on
  purpose. `field` is the field that **classifies** the `change` — without it there's no way to
  know whether that `value` was a template webhook we stopped modeling. The messages get through
  (best effort, the `change` is read as if it were `messages`), and the envelope's `parse_error`
  says something couldn't be classified. The general rule, inherited from T-062: **`ignored` is
  counted when an EVENT stops existing, never when a block inside a delivered event is lost** —
  that's why unreadable `metadata` and `contacts` do NOT count.
- **`entry_id_wrong_type_synthetic.json` documents a decision that isn't the parser's**, but the
  isolation guard's: an unreadable `entry.id` becomes `""`, and guard 5b in
  `internal/inbound/handler.go` treats `""` as **not matching**, refusing the batch with `ALARME`
  and `conta_descartada`. The alternative (discarding just the `entry` and delivering the rest) was
  rejected because the **raw** body travels along with the delivery: filtering events wouldn't
  keep that account's content from reaching the consumer. Proven by
  `TestHandlerRejectsAccountWebhookWithUnreadableWabaID` (`internal/inbound/handler_test.go`).

**The CLASS guarantee, like in the T-062 batch, isn't in these files** — it's in
`TestBoundaryStructsIsolateEveryFieldByConstruction` (reflection over the seven boundary structs)
and in `TestParseWebhookNoFieldAtAnyLevelSilencesTheBatch`, which sweeps **every key at every
level** of the payload itself and requires the witness message from another `entry` to always get
through.

## About `status_read_with_billing.json` (T-041, 2026-07-26)

consumer-a asked (2026-07-26) for the `pricing` field Meta sends in the status webhook — present
in 145 of the 148 statuses they have recorded — because the billing category it carries is the
only way to know, in the FIRST message, that Meta reclassified a template (`UTILITY` →
`MARKETING`, which changes price and sending rules); without it, that would only show up in the
invoice, weeks later. The excerpt they pasted in the bilateral channel was just the
`status`/`pricing` pair, with no envelope — unlike this corpus's other capture fixtures, which came
from the whole payload. This file wraps that literal excerpt in the same standard envelope
(`WABA_TESTE`/`PNID_TESTE`) as the other status fixtures, because `TestTheWholeCorpus`
(`internal/meta/corpus_test.go`) exercises `ParseWebhook` over the whole payload, not over a loose
fragment.

**Why the field is called `billing` in the envelope, not `pricing`:** Meta's shape dies in
`parse.go`, like the rest of the envelope — it's the same reason `reacao`/`localizacao` aren't
called `reaction`/`location` (contract keys still pending translation, out of this task's scope).
Only `category` (from `category`) and `billable` (from `billable`) are modeled; `pricing_model`
and `type` stay out until someone needs them.

**`billable` is a pointer, not a plain `bool`** — the same reasoning as `voice` (`Event.Voice`),
with a bigger consequence: here the difference between "Meta said it doesn't bill" (`false`) and
"Meta said nothing" (absent) is about MONEY. Proven by mutation: changing `Billing.Billable` from
`*bool` to `bool` doesn't just leave one test red — it breaks the COMPILATION of
`internal/meta/corpus_test.go` and `internal/meta/parse_test.go` (`invalid operation: ... == nil
(mismatched types bool and untyped nil)`), because the tests require comparing the field against
`nil` to prove that absence and `false` produce different results.

## About `template_status.json` (T-043, 2026-07-26)

It's the first fixture in the corpus that **isn't about a message or a recipient**: it's an
ACCOUNT webhook (`field: "message_template_status_update"`), Meta announcing that a template was
approved, rejected or paused. Until T-043 this payload arrived, `ParseWebhook` found neither
`messages` nor `statuses`, and the envelope came out with no event at all and only the `cru` —
back then literally `"eventos": null` on the wire, not `[]`, which was the defect fixed in T-067
(2026-07-28).

**Two facts from the real payload weren't deducible from the doc, and both are in this file on
purpose:**

- **`reason` comes as the string `"NONE"`** when there's no reason — not absent, not `null`.
  Whoever treats it as an optional field gets it wrong. The gateway passes `"NONE"` through as it
  came; only the REAL absence disappears from the JSON (`internal/meta/types.go`,
  `TemplateStatus.Reason`).
- **there's no `metadata` nor `phone_number_id` anywhere in the payload** — confirms with data the
  gap T-038 had closed by reading the code alone: the only routing key for an account webhook is
  the `waba_id` from `entry[].id`.

**The `message_template_id` and the `message_template_name` were NOT swapped for fictitious
values**, unlike `wa_id`/`from`/`recipient_id`/`wamid`/`profile.name`, which the rule at the top of
this file requires masking. It isn't an oversight: neither is personal data (one is a template's
id in Meta's panel, the other is the name the business itself gave it), and the literal id has
**16 digits** — swapping it for a short test id would stop exercising the path that matters (it
doesn't fit in `int32`, which is why `templateStatusMeta.TemplateID` is `json.RawMessage` and
becomes text, never an integer).

**This file's `entry.time` (`1769000020`) comes from the standard envelope, not the capture — and
it's still read.** This webhook's `value` **has no timestamp of its own**; the only time available
is in `entry`, and it enters the event's KEY (`template_status:{id}:{event}:{time}`) because the
same template can be `APPROVED` more than once (approved → edited → pending → approved again).
Without the time in the key, the second approval would be deduplicated by the consumer and
vanish. See `internal/meta/parse.go`, `templateStatusEvent`, and the test that proves it
(`TestParseWebhookTemplateStatusTwoApprovalsAtDifferentInstantsHaveDifferentIds`).

## About the three real `sent`/`delivered` files (T-069, 2026-07-28)

`status_sent_with_pricing.json`, `status_sent_without_pricing.json` and `status_delivered.json`.

**The sample, because it's what justifies the fixtures existing.** `consumer-a` measured their
whole corpus — **267 stored payloads**, of which **225 are Meta's raw body** (the remaining 42 are
the gateway's envelope, which carries the raw body in base64 inside itself). Within the 225:

| Measurement | Number |
|---|---|
| `sent` | **53** — **49 with `pricing`**, **4 without** (~7.5%) |
| `delivered` | **49** — **49 with `pricing`** (100%) |
| `recipient_user_id` present | **152** of 225 |
| `contacts[].user_id` present | **203** of 225 |

These aren't three hand-picked examples: they're three payloads from a measurement over the whole
corpus, and **the proportion is what says which shapes need to exist here**. "4 in 53" is the
reason there are TWO `sent` fixtures; a single one would have frozen the common shape and left the
normal case out.

### What these three show, and no hand-written fixture would show

1. **`pricing` is OPTIONAL on `sent`.** A `sent` with no billing isn't a broken payload, it's
   routine in ~7.5% of the measured traffic. Whoever counts by billing category needs to know this
   **before** writing the counter. Proven by mutation: making the parse require `pricing` leaves
   `status_sent_without_pricing.json` red.
2. **`recipient_user_id` exists on the status, format `BR.<digits>`** — and `contacts[].user_id`
   alongside it. Neither is modeled by `statusMeta`/`contactMeta`, and since T-062/T-068 that's a
   decision, not an accident. These fixtures **prove** what was previously assumed: an unknown key
   in the status doesn't bring down the parse. Proven by mutation: turning on
   `DisallowUnknownFields` on `statusMeta`'s `Unmarshal` leaves all three red.
3. **The `sent` and the `delivered` of the SAME `wamid` came with the SAME `timestamp` from
   Meta.** It's the most valuable finding, because the consequence is the **consumer's**. Whoever
   sorts history by the sender's clock doesn't tell the two states apart. It's preserved here on
   purpose — the two files share `wamid.TESTE042` and `timestamp` `1785072102` — and locked down by
   `TestCorpusSentAndDeliveredOfTheSameWamidHaveTheSameTimestamp`. The warning to the consumer is
   in `docs/CONTRATO-CONSUMIDOR.md`; a README only we read doesn't protect whoever builds history
   from it.

### How they were masked

The captures came verbatim from the consumer's `psql`, **with a real customer number and a real
`wamid`**. The masking is **consistent** — the same real value always becomes the same fake value,
across the three files —, because without that the `sent`→`delivered` pair would stop being the
same send and the correlation test wouldn't prove anything:

| Field | Became |
|---|---|
| `entry[].id` (WABA) | `WABA_TESTE` |
| `metadata.phone_number_id` | `PNID_TESTE` |
| `metadata.display_phone_number` | `5532999990000` (this corpus's default business number) |
| `contacts[].wa_id` and `statuses[].recipient_id` | `553288888888` |
| `contacts[].user_id` and `statuses[].recipient_user_id` | `BR.20000000000000000` |
| `statuses[].id` (the `wamid`) | `wamid.TESTE041` (the `sent` without pricing) and `wamid.TESTE042` (the `sent` with pricing **and** the `delivered`) |

**The real `wamid` had to come out whole, not just "swap the digits that look like a number".**
The Cloud API's wamid is `wamid.` followed by **base64**, and that base64 carries the recipient's
phone number **in plain text inside it** — `base64 -d` on what comes after the dot returns the
number. A masking pass that swapped `recipient_id` and left the `wamid` would have leaked the
number the same way, and nobody would have seen it by looking at the file. **The rule that
stands: an opaque field is not a field with no content — before letting an identifier through as
"doesn't look like personal data", decode it.**

**The `timestamp`s were NOT masked, on purpose** (`1785073298` and `1785072102`): they identify
nobody, and the second is literally the fact the capture proved. Swapping them for standard-envelope
values would erase finding #3.

**The `conversation` block doesn't exist in any of the three** — the doc-derived fixture this
batch replaced had one. The parser never read that block (`statusMeta` has no such field), so the
difference doesn't change behavior; it only shows that a fixture can get the shape wrong with no
test going red.

> ⚠️ **`553288888888` is a NEW recipient in this corpus — the other fixtures use a different
> number, and the mismatch is deliberate.** While masking this capture, decoding the real
> `wamid`'s base64 showed that the real traffic's recipient was **the same number** this corpus had
> been treating as fictitious since stage 1: it was the owner's personal phone number, not an
> invented value. **Resolved by T-159 (2026-08-20):** every fixture using that number switched to
> `5511999990000` (the convention `docs/CONTRATO-CONSUMIDOR.md` already uses), the same pattern
> adopted hours earlier by T-138 for the same kind of leak. The mismatch with `553288888888` still
> exists — they're two different synthetic values, one from masking real traffic, the other from
> T-159's replacement — but neither is the real phone number anymore. Unifying them, if it ever
> makes sense, is a decision outside T-159's scope.

## About the four `template_category_update` files (T-057, 2026-07-28; T-174, 2026-08-28)

Three captures — `template_category_downgrade.json`, `..._restore.json` and
`..._no_previous.json` — plus `template_category_synthetic.json`. The doc-derived file that used
to open this list **was deleted by T-174**; the section on derived files, above, explains why it
doesn't coexist with the captures.

**None of the four contains a phone number, `wa_id`, `wamid` or a person's name** — this webhook's
`value` has nothing like that. It was the first group in the corpus where the masking rule at the
top of this file had nothing to mask, and it's worth saying so instead of leaving the reader to
check field by field. In the three captures the only swap made at the source was `waba_id` for
`WABA_TESTE`; `message_template_id` stayed as it came, on purpose, because it's what proves the
pair is of the **same** template.

**Why the synthetic one still exists after the captures — and the reason CHANGED.** It was born
because the panel's sample carried `previous_category: "MARKETING"` and `correct_category:
"MARKETING"` — the **same value** —, and a parser reading one in place of the other would pass
**green**. Today the reason is stronger: **none of the three captures carries `correct_category`
or `category_appeal_status`**, so the synthetic one is the only file in the corpus that still
exercises those two fields. It's the same family as `template_button.json` (`payload == text` in
the real capture) and the *"leak test whose fixture erased the branch that would leak"* entry in
`docs/ARMADILHAS.md`.

*Measured, not assumed (T-057): the mutation that swaps `t.Previous` for `t.Correct` in
`templateCategoryEvent` (`internal/meta/parse.go`) left **only** the synthetic one red — the
doc-derived one stayed green, with the same event `ID`. Made and reverted before the commit.*

**The EXPENSIVE direction now has a capture.** The panel's sample showed `MARKETING → UTILITY`,
which *gets cheaper*, and that's why the synthetic one was made with `UTILITY → MARKETING`, which
**makes every send more expensive**. T-174 froze both directions in real traffic, from the **same**
`message_template_id`, ~14.9 h apart — something the sample could never show.

**`category_appeal_status` only exists in the synthetic one** (`NOT_ELIGIBLE`), and it's passed
through as **text**. A boolean derived from it ("can it be appealed?") would force the gateway to
decide today what to do with a value Meta only invents tomorrow. ⚠️ **That it hasn't shown up in
any of the three captures is a measurement, not a conclusion:** these are three events from one
account, and the doc lists the field. The
`TestTemplateCategoryNoRealCaptureBroughtAppealNorCorrectCategory` test freezes what's been
observed and goes red the day a capture with the field arrives — which is the day to update the
measurement and the contract's table together, not to delete the assertion.

**The synthetic one's `message_template_id` has 16 digits** (`9900000000000002`), like the one in
`template_status.json`'s capture: it doesn't fit in `int32`, which is why
`templateCategoryMeta.TemplateID` is `json.RawMessage` read as text, never an integer. The derived
one's is the sample's `12345678`, preserved byte for byte — freezing the sample is what that file
does.

## About the three ACCOUNT webhook files from T-058 (2026-07-28)

`number_quality_derived_from_doc.json`, `number_quality_synthetic.json` and
`account_alert_derived_from_doc.json`.

**None contains a customer phone number, `wamid` or a person's name.** The derived one's
`display_phone_number` is `16505551111` — the fictitious number Meta itself uses in panel samples,
preserved byte for byte; the synthetic one's is `5532999990000`, this corpus's default business
number. The masking rule at the top of this file has nothing to mask here.

### Why quality has a synthetic sibling and the alert doesn't

**It's the same question in both cases, and it got different answers** — which is exactly why the
question is worth asking: *do two neighboring fields in this payload have the same value?*

- **quality: yes.** The sample carries `current_limit: "TIER_250"` and
  `max_daily_conversations_per_business: "TIER_250"` — **the same value**. Freezing just that one
  would produce a corpus where swapping the read of one for the other passes **GREEN**. *Measured:
  the mutation `q.CurrentLimit` → `q.MaxDailyLimit` in `numberQualityEvent`
  (`internal/meta/parse.go`) leaves **only** `number_quality_synthetic.json` red; the doc-derived
  one stays green, with the same event `ID`. Made and reverted before the commit.*
- **alert: no.** `entity_type`, `entity_id`, `alert_severity`, `alert_status` and `alert_type` all
  come with different values in the sample, so it alone tells a swapped read apart. Adding a
  synthetic one "for symmetry" would be ceremony with no guarantee — the same decision, and the
  same question, as `interactive_button.json`.

**It's the third time this question has paid off in this corpus** (`template_button.json` with
`payload == text`; `template_category_derived_from_doc.json` with `previous == correct` — a file
deleted by T-174, but the finding is what survives from it; and now `current_limit ==
max_daily`). It's free and worth keeping as a fixed step: **before freezing a payload, check
whether two neighboring fields have the same value.**

### The synthetic one also freezes the direction that HURTS

The panel's sample shows `TIER_NOT_SET → TIER_250` with `event: "ONBOARDING"` — the one quota
transition nobody worries about. The synthetic one shows `TIER_1K → TIER_50` with `event:
"FLAGGED"`: a **downgrade**, exactly the case this event exists to warn about before sending starts
failing on the limit. A corpus with only the sample would freeze the good news.

### The limits are TEXT, and the fixtures prove it

`"TIER_250"` doesn't become `250`, `"TIER_NOT_SET"` doesn't become `0` or empty, and `TIER_10K`
exists in the synthetic one so nobody feels like doing arithmetic on the suffix. Meta can invent a
new tier tomorrow, and a translation table of our own would fail in the worst way: returning a
plausible number for a value nobody checked. See `NumberQuality`, in `internal/meta/types.go`.

### `entity_id` comes as a NUMBER and goes out as TEXT

The sample sends `"entity_id": 123456` (a JSON number), and the gateway passes it through as the
string `"123456"` — the same tolerance (and the same reason) as `message_template_id`, which in
the real capture has 16 digits and doesn't fit in `int32`. The derived fixture already exercises
this path, so there's no synthetic one for it.
