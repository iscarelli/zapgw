# Pitfalls

*[Leia em português](ARMADILHAS.pt-BR.md)*

One line per pitfall, **with the real cost it charged**. Without the cost it becomes a generic
warning and nobody reads it.

The entries were born in plans **1** (foundation and inbound) and **2** (sending), and the file grows
with every fix — a new entry goes in the same commit as the fix, which is the only moment when the
cost is fresh. Each one was **proven by a test before the fix**: none is hypothetical.

---

## This project's mother pitfall: "the rule holds in one place and not in the next"

**Cost: four Critical findings, all on the same branch, all with this shape.**

Plan 1 had exactly four Critical defects. None was a typo, none was ignorance. In all four the right
rule **was already written** — and had not been applied one level up:

| # | The rule existed in | It did not exist in | What happened |
|---|---|---|---|
| 1 | per-item isolation (messages) | `entry` and `changes` | one field with the wrong type wiped out the **entire** batch, including valid messages from another account |
| 2 | encryption at rest (`callback_url` in the database) | the log | `Motivo` printed the full URL, query string included |
| 3 | an instance is born paused (the store enforced it) | the handler | nobody asked whether it was active; the guarantee was **inert** |
| 4 | correct counter (sequential) | under concurrency | `seq++` in a shared handler = data race |

**The lesson, and it is the most expensive one on this branch:** when you write a guarantee, **do not
ask "is this function right?" — ask "where else should that same sentence be true, and is it?"**. The
asymmetry between two places that solve the same problem **is** the bug.

**And the corollary:** all four were found by **adversarial review**, not by reading. In three of the
four, the whole suite was **green** at the moment the defect existed.

**The most expensive form of this pattern in this project: the gateway knows how to RECEIVE what it
does not know how to SEND.** Formulated by `consumer-a` (2026-07-26), on noticing that there were
already **three** cases, all with both halves written by the same hands at different moments:

| Feature | The INBOUND side already did | The OUTBOUND side did not do |
|---|---|---|
| template button | the envelope delivered `botao_payload` from the start | sending built neither the header nor the button parameter (until T-021) |
| reaction | the envelope carried a full `reacao` (T-023) | sending refused removal (until T-027) |
| button payload | the envelope carries `botao_payload` from `button` and from `button_reply` | sending only knows `sub_type: "url"` (T-044) |
| read receipt | the envelope delivers `status: "read"` from the start — we know the customer read what we sent | there was no way to say that WE read their message; the conversation stayed on two grey ticks forever (until T-075) |

**The discovery mode is what matters: none of the four showed up by reading the sending code.** All of
them showed up when **somebody went to USE** the outbound half of something the inbound half already
did well — and in all four cases the one who went to use it was a consumer, not the author.

**The fourth row adds two things the first three did not have, and both count as signal:**

- **BOTH consumers asked for the same thing, on the same day, without talking to each other**
  (`consumer-a` at 16:42, `consumer-b` at 16:58 on 2026-07-28). Independent convergence is a sign that
  the missing half is real, not one consumer's preference — the same reading this file already records
  for the *work-result* trigger both of them arrived at on their own;
- **the root cause was not ours: it was the MIGRATION.** Under Baileys the read marker went out **by
  itself** on receipt; under the Cloud API it requires an explicit call. That is, a feature the old
  stack did **implicitly** becomes a silent absence in the new one, and nobody lists it as a
  requirement because nobody implemented it the first time. *The question that generalizes, and it is
  cheap during a migration:* **"what did the old stack do by itself, without anyone asking?"** — that
  is an entire family of requirement written down nowhere, and every consumer coming from Baileys will
  hit it.
*Cost: low, and measured by the consumer itself — it affects neither delivery, nor billing, nor quota.
What it costs is the customer not knowing whether she was read, which is what produces the "hi, did it
arrive?" while the operator is already working on her order.*

*Prevention, cheap, suggested by the one who got the scare: **when you add a field to the envelope, ask
in the same task "and does sending know how to produce this?"** — not to implement it at the same time,
but to **record the asymmetry instead of discovering it through the consumer**. One line in the task's
`Why` costs nothing; a central flow stuck on a consumer in production costs them a day.*

**The pattern crosses repository, language and team — three instances in a single day (2026-07-26), and
a fourth on the same day, found by a FIFTH way:**

| Where | The rule existed in | It did not exist in |
|---|---|---|
| here, Go | envelope of the **message** event (T-023) | the **status** event, which lost the failure reason (T-028) |
| consumer, Python | `503` for a credential on the bilateral channel | the **contract**, which every future consumer would read (moved in PR #34) |
| consumer, Python | incrementing the idempotency series in the **worker** | the admin's **"resend" button**, which rewrote the phone number without changing the key → `422` → the customer never received it |
| here, Go | closed counter vocabulary (`internal/config/contador.go`) | the printout of `zapgw estado` (`cmd/zapgw/estado.go`), which repeated the list by hand and did not know about the new key (T-038/T-039) |

In the first three, whoever wrote the rule was the one who left the hole, and in all three the suite was
green. **What found the three was the same question, not the same person** — which suggests it works as
a review step, not as a talent.
*One detail of the third case is worth copying: the fix was NOT to add the increment in both places —
it was to create ONE function for the transition, with the rule in the docstring. Enumerating call
sites is what produces the gap; a third path in the future runs into the sentence instead of forgetting
it.*
*And the warning that unblocked that case pointed at a DIFFERENT axis (text recomputed on send), which
was clean. The value was not in the hypothesis being right: it was in it forcing somebody to enumerate
the paths. **A well-aimed and wrong guess still finds the bug next door.***

**The fourth row arrived by a way different from the four already recorded in this file** (adversarial
review, real traffic capture, experiment with a physical device, rereading what had already been
captured with the right question — see the "Meta / WhatsApp Cloud API" entries, above). T-038 added
`config.CounterAccountDiscarded` to the closed vocabulary and — with no external review, no new test, no
new traffic — the T-038 implementer THEMSELVES declared, on closing the task, that `cmd/zapgw/estado.go`
had a list of its own and did not know about the new key. It was not "where else should this rule hold?"
asked by an outside reviewer: it was the person who had just written the code noticing, right then, that
the problem they were creating was an instance of this file's pattern, and recording it as the next task
instead of leaving it for someone to find later.
*Cost: zero in production so far — `CounterAccountDiscarded` never had its real value looked at by anyone
before the fix (T-039), but it also never gave a WRONG result, only an invisible one. Fix:
`config.KeysInDisplayOrder` (`internal/config/contador.go`) becomes the single source — both the
validation set (`counterKeys`, derived from it) and the printout in `cmd/zapgw/estado.go` (which now
walks it, with no list of its own) read from the SAME place. Mandatory mutation, done and reverted before
the commit: restoring the old list in `estado.go` (without touching `contador.go`) leaves
`TestStateCommandShowsEveryVocabularyKey` (`cmd/zapgw/estado_test.go`) red, missing `conta_descartada`;
adding a new key only to `KeysInDisplayOrder` (without touching `estado.go`) leaves the same test green,
with the new key showing up on its own.* **The question that generalizes: when you yourself have just
created the "the rule holds here, not there" asymmetry, the fifth way to find it is simply to say so out
loud before closing the task — not to wait for a review, a capture or a future test to find it for you.**

**The SIXTH way: deliberately trigger an alarm that has never fired — and look at what is NEXT TO it.**
T-042 (2026-07-26) exercised, with traffic, on the production binary of CT 125, the step-5 guard
(`internal/inbound/handler.go`), which had a unit test and **had never run with two real instances**: over
the entire life of the service, `journalctl -u zapgw | grep "que nao e dela"` returned **zero**. The
hypothesis that opened the task was that the alarm was broken. **It was not** — triggered with somebody
else's `phone_number_id`, it came out immediately and correct. The finding is its neighbour:

*(This entry used to call the step-5 guard "tenant isolation". The name was corrected in T-050 — see the
*Meta / WhatsApp Cloud API* section: with one App per consumer, it is **addressing verification**, and
what separates tenants is the step-3 signature. What T-042 measured does not change.)*

| | rejection by `phone_number_id` (5a, plan 1) | rejection by `waba_id` (5b, T-038) |
|---|---|---|
| answers `200` to Meta | yes | yes |
| writes `ALARME` to the journal | yes | yes |
| **counted anything** (until T-047) | **no — nothing, not even `recebidas`** | yes, `config.CounterAccountDiscarded` |
| counts today (T-047) | yes, `config.CounterNumberDiscarded` | yes, `config.CounterAccountDiscarded` |

That is: an isolation rejection by `phone_number_id` **was invisible in `zapgw estado`** (the fix is
T-047, further down); the only trace was one journal line, and this file already records (section *Errors
and logging*) that nobody reads the journal out of habit. Measured in T-042 itself: after triggering the
alarm, the test instance showed `recebidas 1` (only the accepted event) and `conta_descartada 0` — the
rejected event appeared in no counter at all. It is exactly the shape of this section: the same sentence
("an isolation rejection is counted") holds in the branch written on 2026-07-26 and does not hold in the
branch written in plan 1, and both halves were written by the same hands at different moments.

*Cost: zero — the rejection never happened in production, and the defect lived from plan 1
(2026-07-23) until T-047 (2026-07-26) without ever giving a WRONG result, only an invisible one. **It
stayed OPEN for a day, and the reason is recorded because it is the right decision, not laziness:**
T-042 was a task to EXERCISE, with the `Files:` list closed on the step-5 comment — fixing it there
would have mixed, in the same commit, what was measured with what was changed. **Closed in T-047:**
`config.CounterNumberDiscarded` (`numero_descartado`) joins the closed vocabulary in
`internal/config/contador.go` and is recorded in step 5a of `internal/inbound/handler.go`, **after**
the `w.WriteHeader` as T-035 requires. Not one line of `cmd/zapgw/estado.go` changed — T-039's single
source made the new key appear in the table on its own, which is the same guarantee exercised again,
for free.*

*Why the key is NEW and not `conta_descartada` reused: they answer the question "was there an isolation
rejection?" the same way and answer the next one — "which guard rejected it?" — **differently**, and it
is the second that decides where the person will go looking (Callback URL/override/registered
`phone_number_id` versus registered WABA/App). A number that sums the two orders you to check both
places, every time; and whoever has only the total cannot split it back apart, while whoever has both
adds them up on screen.*

***The mandatory mutation yielded more than the proof it asked for, and the result of the first stage is
the information that counts.*** The task ordered moving the `Registrar` to BEFORE the `w.WriteHeader`
and proving that the "the counter does not change the status" test goes red.
**Moving it alone leaves the test GREEN** — and that is not a weak test: it is `Contador.Record`
**returning nothing** (see the entry "A method that CAN return an error…", section *Errors and logging*),
so that there is no path by which a counting failure can reach the response. Only stage two — moving it
**and** swapping in a variant that returns an error, handled with `http.Error` the way the rest of the
project handles errors — leaves `TestHandlerCounterFailureDoesNotChangeStatusOfPhoneNumberIDRefusal` red,
with `500` in place of the `200`. **The lesson: when an ordering mutation comes out green, ask whether
the real defence is the order or the SIGNATURE** — here the order is discipline (and still holds, because
the next person may not have the signature on their side), and the signature is the guarantee.

**The question that generalizes: when you finally exercise a guard that has never run, do not stop at
"did it fire?" — ask what the NEIGHBOURING branches do that it does not.** An alarm that has never fired
is not just an untested alarm: it is a whole branch of code whose instrumentation nobody had a reason to
look at.

**The most ironic form of this asymmetry: the branch that exists to say *"I do not know what
happened"* was the ONLY one that did not keep what happened.** On 2026-07-28 `consumer-b` created the
template `pedido_avaliacao_v2` through `POST /v1/templates` and got `502 desconhecido` with the message
*"o template PODE ter sido criado — confira o catálogo antes de tentar de novo"*. **The template had been
created.** In the `default` of `respondCreationError` (`internal/outbound/templates_handler.go`), the
neighbouring branches all logged — `ALARME … credencial recusada`, `waba_id invalido`, page ceiling — and
only that one discarded the `err`. Measured on the production CT, over the whole day the `502` came out
of it: `journalctl -u zapgw | grep -ci template` = **0**. When the consumer asked *"foi timeout ou
transporte?"*, the answer was not lost in the middle of the journal: it **had never been written**.

*Cost: the consumer spent the second door to find out the truth — they went and checked the catalogue
**straight on the Graph API**, an access the owner forbade on the SAME day (`CLAUDE.md`, "NINGUÉM fala
direto com a Meta"). That is, the only path that saved that case stopped existing hours later, and the
defect would have been left with no safety net at all. On our side, the price was a structurally
undiagnosable outcome: no counter, no log, no response carrying information.*

**The question that saves the next one, and it is narrow: does the branch that classifies an outcome as
UNKNOWN keep what it does not know?** "Unknown" is the only class whose value lies entirely in the trace
— the others already say everything in the response itself. A silent `desconhecido` is the worst of both
worlds: the caller does not find out and neither does the operator.

🔴 ***And the second half is where T-078 nearly got it worse than the original defect: "I DID NOT FIND
IT" IS NOT "IT DOES NOT EXIST".*** The obvious fix — reread the catalogue and, on not finding it, answer
*"it was not created"* — would have been an assertion the gateway **has no way to sustain**. The question
was put to the source: Meta documents *read-after-write* for the **response of the `POST` itself** on that
edge (`developers.facebook.com/docs/graph-api/reference/whats-app-business-account/message_templates/`) —
which is exactly what did not arrive — and **does not document, on any page it was possible to read, that
a later `GET /{waba}/message_templates` already contains the new template**. Nor the opposite. *Not
documented in either direction* is a legitimate answer, and it forces the response to be **INCONCLUSIVE**.

*What makes that error expensive is the ASYMMETRY, and it holds as a rule beyond this case: saying "I do
not know" costs the consumer one check; saying "it was not created" makes them recreate it — and `nome` +
`idioma` are unique per account, with the second creation returning `code 100` / `subcode 2388024`
(`developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management`).
**The name they chose becomes unusable, forever.** One error costs a minute; the other costs a name.*
**When the two possible errors have prices of different orders of magnitude, the doubt is not resolved by
what is more likely — it is resolved by the cheap side.**

*Fix (T-078): `respondAmbiguousOutcome` logs the real cause (slug, template name and language — **never
the request body**, which carries text that goes to the end customer), rereads the catalogue with a **new
context** (the creation one may be dead from an exhausted deadline, which is one of the causes of getting
there) and answers `201` if it found it, `INCONCLUSIVO` if it did not, and `502` with **both failures
logged** if the reread also fell over. The reread is a `GET` and nothing else —
`TestRereadDoesNOTCreateAgain` counts the POSTs reaching Meta across the three outcomes and requires
exactly **one**. `cmd/grafo-falso` gained `--falha-de-template={criado,nao-criado,catalogo-tambem}`, which
drops the POST connection **without a response** (`panic(http.ErrAbortHandler)`): a `500` would be a
RESPONSE, and a response is something Meta classifies — the `desconhecido` outcome is born from its
absence.*

*Two mutations, done and reverted before the commit, and the first is the informative one:* (1) swapping
the "did not find it" outcome for *"the template was not created… you may create it again"* leaves
`TestCreateTemplateAmbiguousNotFoundInTheCatalogIsINCONCLUSIVE` red on **four assertions about the TEXT**
— missing `inconclusiv`, presence of `nao foi criado`, presence of `nao existe`, and the missing *"do not
repeat"* — besides the class. **The assertion is about the text on purpose**: it was the text, not the
HTTP code, that did the work in the real case (a bare `502` would have told nobody anything); (2) removing
the `log.Printf` from the creation failure leaves two tests red, and the detail matters: the **outcome**
lines still come out with slug, name and language, so what is lost is exactly the **cause**
(`inalcancavel` / `prazo esgotado`) — that is, the mutation reproduces precisely the question that went
unanswered in production, *"foi timeout ou transporte?"*.

**The EIGHTH way, and it is the cheapest of all: somebody OPERATING the system compared a number on the
screen with a fact they had just witnessed.** On 2026-07-28, minutes after `zapgw fumaca` activated
`tenant-two`, the test message went out (wamid returned, and the owner confirmed **on the handset** that it
arrived) and the next `zapgw estado` said `enviadas hoje 0`. There was no review, no new traffic and no
test: there was a person with the message on their phone looking at a counter saying it did not exist.

The asymmetry has the exact shape of this section — *"every message that goes out is counted"* held in
`POST /v1/messages` and not in `fumaca` — and its mechanism is what is worth copying: `fumaca` shared with
production **the code that BUILDS the body** (`Validar` + `MetaBody`, deliberately — a path of its own
would prove the wrong path) and not the **handler**, which is where the `Registrar` lived. *Sharing the
construction of the effect does not share its instrumentation*, and the review that asks "do both paths use
the same code?" answers **yes** and walks straight past.

*Cost: low in volume (one message per instance, at activation) and high in confidence — the zero landed
exactly on the freshly activated instance, which is when *"has this instance sent anything yet?"* is asked
most, and the right answer from the log would be "go look in the journal", the very trace the counters exist
so as not to be the only one. Fix (T-054): step 3 of `cmd/zapgw/fumaca.go` records `config.CounterSent` on
success and `config.CounterSendFailures` on failure, **under the same key as the production send** — a key
of its own for the smoke test would force the reader to add two columns to answer one question, and whoever
has only the total never splits it back apart.*

*Four mutations, done and reverted before the commit; the third is the informative one:* (1) removing the
`Registrar(ContEnviadas)` leaves `TestSmokeCountsSENTUnderTheSAMEKeyAsTheProductionSend` red **reproducing
the production symptom in full** — the `zapgw estado` table comes out with the instance `ativa` and every
key at zero; (2) removing the `Registrar(ContFalhasDeEnvio)` leaves
`TestSmokeCountsSENDFAILURESWhenMetaRefusesTheSend` red; (3) **moving the `Registrar(ContEnviadas)` to
AFTER step 4 (`ActivateInstance`) passes GREEN** — nothing in the suite makes that step fail, so "count
step 3 before step 4" is **discipline written in a comment, not a proven guarantee**: if activation failed,
the message would have gone out and the counter would say zero again. It is the same lesson as T-047's
ordering mutation, one surface further along — the defence that actually exists is the SIGNATURE of
`Registrar`, which returns nothing and therefore cannot abort `fumaca` after the message has gone out; (4)
swapping the key for an invented one (`enviadas_pelo_fumaca`) leaves the same test from (1) red **with no
error at all from the command** — the closed vocabulary (`internal/config/contador.go`) only logs and moves
on, which is the right behaviour and also the reason a new key needs a test so it does not become silently
discarded counting.

**The same pitfall on the SURFACE, not on the data: the consumer saw four blocks the operator with an SSH
session open did not see.** T-060 (route `GET /v1/estado`) and T-064 (`certificado_do_callback`) shipped on
the same day, each with a green suite, and neither touched `cmd/zapgw/estado.go`: the route published
`estado`/`pausada`, `versao`, `token_meta` and `certificado_do_callback`, and `zapgw estado` showed **only
the counter table**. It was not a data divergence — the numbers always came from `config.SummarizeCounters`,
the single source since T-039, and the test that compares the two surfaces number by number passed. It was a
**surface** asymmetry, which is the most expensive form in this section: the information **exists, is
correct, and simply does not appear where somebody is looking**. And whoever is looking through the CLI is,
almost by definition, in the middle of an incident — they would have to leave the CT, find a consumer token
and call the internal route to ask the binary in front of them whether Meta still accepts the token.
*Cost: zero measured — the command never showed a WRONG number, only showed LESS, and the hole lived a few
hours (T-060 and T-064 on 2026-07-28, closed in T-065 the same day). Fix: `internal/outbound/estado.go` —
**one place that builds the state (`BuildState`), two surfaces that present it**; the route serializes the
`Estado` as JSON and the CLI prints `StateRows`, which comes out by **reflection** over the struct's fields.
Neither of the two enumerates a field, and that is what prevents the relapse.*
**The discovery mode repeats the fifth way, and it is worth recording that it was not an accident:** the
finder was the **T-064** implementer, who ran `grep -n "token_meta\|Veredito\|vigia" cmd/zapgw/estado.go`,
saw **zero lines** and — instead of quietly fixing it outside scope, or leaving it for someone to find later
— **reported it as a task**.

*And the mandatory mutation is the only proof that counts here: adding a field to `Estado` has to show
up on BOTH screens without editing either of them. Done and reverted before the commit
(`campo_de_mutacao`), with `git status` showing **one** modified file.*
✅ **The guarantee exercised itself, with a real field, in T-120 (2026-08-06):** the `entrada` block
entered the `Estado` struct and appeared in the route's JSON **and** on the `zapgw estado` screen — the
CLI (`cmd/zapgw/estado.go`) did not gain a single line about it, only the source's construction. *It is
not a new proof, it is the same proof charged by a real case instead of a made-up field.*

***And the finding that only showed up on implementation: extracting the common source IS NOT ENOUGH when
the source is another process's MEMORY.*** The `token_meta` comes from the watcher's cache
(`internal/outbound/vigia.go`), which lives in the **server's** memory. `zapgw estado` is another process,
and is born with an **empty** cache — publishing the block by reading that cache would have put
`veredito: desconhecido` on the screen, with both stamps blank, **forever**. That is not "less
information": it looks like a **broken watcher**, and it sends the operator to investigate a defect that
does not exist, in the middle of the incident. *Fix: the CLI **measures** before reading
(`Vigia.CheckInstance`), for the same reason the probe (`saude_handler.go`) has no cache — whoever is at
the terminal chose the frequency by pressing Enter. A PAUSED instance still is not measured, same as on the
server, and the `pausada: sim` next to it explains the `desconhecido`.*
**The question that generalizes: when you unify two surfaces, ask where each field COMES FROM in each
process — "the same function" does not guarantee "the same data available", and the field that vanishes is
precisely the one that only exists in memory.**

**The same pitfall in secret GENERATION: generating silently is RIGHT for two fields and WRONG for the
other two, and the failure mode is the ABSENCE of an error.** `zapgw provisionar instancia` generated the
instance's four secrets without printing any of them — a single policy, applied uniformly, and wrong on
half of them for exactly that reason. Two of them exist **only inside the gateway** (`app_secret`,
`token_envio`) and printing them is gratuitous exposure. The other two need to exist **outside as well**:
the `verify_token` goes in Meta's panel and the `segredo_entrega` goes to the consumer. Generated and not
shown, they are born **unusable** — and nothing flags it. There is no error, there is no log, the command
finishes successfully; the failure only shows up much later, when Meta's challenge does not pass or when
the consumer cannot verify the HMAC, far from the command that caused it. *Cost: `ONBOARDING-META.md`
instructed pointing at the Verify Token "that you chose" — an impossible instruction to follow, because
nobody chose anything; the value had been generated and thrown away.* **Fix (T-052): print the two shared
ones, with a warning, in the same format as the consumer token.** Two things are worth more than the fix:
(1) the seemingly safer way out — **refusing** to generate and requiring the human to supply the value — is
worse, because it does not reduce human exposure (a secret a person carries is a secret a person sees), it
only moves **generation** outside, where somebody in a hurry types `segredo123` as the HMAC key for every
delivery; and (2) the accepted cost is **written next to the decision**, in the code: the value passes
through the terminal, and a terminal becomes a transcript — that is how four secrets leaked on the morning
of 2026-07-28. *Two old tests encoded "never print a secret" with no exception. They were **narrowed, not
disabled**, with the reason inside and a cross-pointer to the test that now requires the opposite —
disabling would have erased the rule along with the exception.*

🔴 **And the SAME policy became wrong again two days later, on the other half — because its premise fell.**
T-052 wrote, in so many words, that `app_secret` and `token_envio` "exist only inside the gateway" and can
therefore be generated silently. That was true while the Meta account belonged to the owner. **T-079**
delivered the model in which the Meta account belongs to the **consumer** — and on that path the two stopped
being ours: they have one right value, which lives in the consumer's panel, and any other is garbage.
Generating them started to **produce a false statement on the only reading surface that exists**: nothing is
decrypted in this project (T-020), so `zapgw instancia mostrar` and the registration response say only
`cadastrado: sim|não` — and with generation they said `app_secret=sim` about a secret the consumer's Meta had
never seen. The question the owner and the consumer **can** ask ("has he registered it yet?") went
unanswered, and the wrong answer was the reassuring one.

*How it showed up, and it is the file's fifth way (the author notices the asymmetry they have just created):
it was not review nor traffic — it was an `instancia mostrar` test, written in T-079 itself, failing on
`app_secret=nao` and showing `app_secret=sim` on a freshly created instance nobody had registered. **Cost:
zero in production** — the instance is born PAUSED and `zapgw fumaca` is the only path to `ativo = 1`, and it
requires a message that actually went out, impossible with a generated `token_envio`; the damage would be to
DIAGNOSIS, not to traffic. Fix in the same commit: `cmd/zapgw/provisionar.go` marks the two as
`fromConsumerMeta` and **does not generate them** when the instance is born without identification (the
signal for "the Meta account is the consumer's"), printing `NAO sorteados, porque sao da conta Meta do
CONSUMIDOR: …` so that the `nao` on the screen does not look like a defect. On the gateway's two fields
(`verify_token`, `segredo_entrega`) T-052's generation stays as it was, and there is a test for each half.*

**The question that generalizes, and it is narrower than "review the old rules": when a new model changes WHO
owns a piece of data, go and reread every decision that was justified with "this value is ours".** The rule
was not wrong — its premise changed owner, and a default-value policy ("generate whatever is missing") is
where that turns into a silent lie, because a generated value is indistinguishable from a registered one for
any reading that does not decrypt. *Cheap corollary: `grep` for "só dentro do gateway", "é nosso", "qualquer
valor serve" yields the list of candidates.*

**The pitfall of the PATH THAT ONLY EXISTS IN ONE DIRECTION: the CLI knew how to CREATE a consumer and did
not know how to REVOKE one.** It is the sister of *"it knows how to receive what it does not know how to
send"*, and it hides better, because the path that exists works perfectly — nobody notices what is missing
while everything is calm. The cost arrives together with the incident, which is when the hurry is at its
peak: on 2026-07-28, ~10:40, an instance's four secrets leaked into a consumer's transcript. Three had a
command. **The consumer token had none** — and "there is no command" during an incident means a hand-written
`UPDATE`, in production, with the clock running. *Real cost: the rotation went through, but the two wrong
ways out that haste offers had to be refused in real time — creating `<nome>-2` (the exposed token stays
valid) and deleting/recreating (the links disappear, and the consumer takes a `403` nobody can explain). Fix
(T-055): `zapgw consumidor rotacionar` / `listar`, and the two wrong ways out became the two mandatory
mutations.* **The question that generalizes, and it fits every surface: for everything this system knows how
to CREATE, does it know how to UNDO? And the answer has to be given before the incident, because during it
there is no time to discover that the answer is no.**

**A table list written from memory rotted in ONE day.** T-048 said that removing an instance "deletes the
instance and its `contador` rows". There were **four** tables with a `slug` column, not two: the
`certificado_do_callback` was born in T-064 **the day after** the text was written, and
`consumidor_instancia` was never remembered by anyone. And the error would not leave only an orphan row —
with the foreign-key PRAGMA on, the `DELETE FROM instancia` **does not even run** without deleting the
`consumidor_instancia` first. *Fix (T-048), and it is not "the right list":*
`TestRemoveInstanceCOVERSEveryTableWithASlugColumn` asks **the database itself** which tables have a `slug`
column and goes red **naming** the missing one. The list stays explicit on purpose — a future table may be
supposed to **survive** the instance, and that is a decision, not a sweep; the guard exists so that the
decision gets **made**, not to make it by itself. **The question that generalizes: every hand-written list
about the SCHEMA has to have, next to it, something that asks the schema.** Enumerations of tables, of
columns, of counter keys, of response fields — in all of them, what rots is the list, and what does not rot
is the question.

**The NINTH way, and it is the most uncomfortable of the nine: the defect was WRITTEN, in so many words,
inside a GREEN test.** T-043 (2026-07-26) delivered the category-reclassification warning by reading
`message_template_category` from `message_template_status_update` — the **approval/rejection** webhook,
which carries the category as an *attribute* of the new state. The event Meta dedicates to a category
**change** is another one, `template_category_update`, and the App was not even subscribed to it.
Consequence: the protection **only fired when Meta reapproved the template in the same movement**; a
reclassification of an already approved template went by in silence, with the discovery arriving on the
invoice — the exact failure mode the task existed to prevent.

*The detail that makes this entry worth a section:* the same T-043 wrote
`TestParseWebhookAnotherAccountFieldStaysOnlyInTheRawBody` (`internal/meta/parse_test.go`) to prove that an
unmodelled account field stays only in the `cru` — and **chose `template_category_update` as its example**.
That is: the repository contained a green test, with an affirmative name, saying that the gateway **does not
read** precisely the channel the protection needed to listen to. Nobody read that line as a finding for two
days, because a green test is read as "this is right", not as "this is written".

*Cost: **unknown by construction**, and this time not even the silence is observable — there is no lost event
to count, there is an event that **never arrived** because the field was unsubscribed. It lived from
2026-07-26 (T-043) to 2026-07-28 (T-057), with a single template live and no known reclassification in that
window. Fix: `fieldTemplateCategory` + `templateCategoryEvent` (`internal/meta/parse.go`) and
`TemplateCategory` (`internal/meta/types.go`), with the subscription made by the owner in the panel the same
day — both halves were necessary and neither sufficed.*

**The question that generalizes, and it is cheap: when you choose the example for a NEGATIVE test ("this we
do not read", "this cannot happen"), ask why the example is THAT one.** An example chosen without thinking
is harmless; one chosen because it was the closest thing to what you have just implemented is a confession.
*Corollary for review: `grep` for the names appearing in "we do not model this" tests yields a short list of
candidates for "we should".*

*And the second lesson, about the FIXTURE rather than the code:* Meta's panel sample carries
`previous_category` and `correct_category` with the **same value** (`"MARKETING"`). Freezing only that one
would have produced a corpus in which swapping the reading of one field for the other passes **GREEN** —
measured: the mutation `t.Previous` → `t.Correct` leaves **only** the synthetic fixture
(`categoria_de_template_sintetico.json`) red, and the doc-derived one stays green with an identical `ID`. It
is the same family as `botao_de_template.json` (`payload == text` in the real capture), and the rule that
comes out of it is the usual one: **before freezing a payload, look at whether two neighbouring fields have
the same value — if they do, it does not distinguish a swapped read and it needs a sibling that does.**

*The question paid off again on the SAME day, in T-058, which promotes it from anecdote to review step:*
the `phone_number_quality_update` sample carries `current_limit` and
`max_daily_conversations_per_business` with the same `"TIER_250"`. Mutation done and reverted:
`q.CurrentLimit` → `q.MaxDailyLimit` leaves **only** the synthetic fixture red; the doc-derived one stays
green with an identical `ID`. **And the step's value is that it sometimes answers NO:** in the
`account_alerts` sample the five fields are distinct, so it did not get a synthetic sibling — adding one
"for symmetry" with its neighbour would be ceremony without a guarantee (the same decision as
`botao_interativo.json`).
*Three instances, two different answers, cost of asking: a thirty-second visual `diff`.*

---

**A CLEAN measurement of one thing becomes a DIRTY conclusion about another — and the number attached is
what makes it convincing.** On 2026-07-28 `consumer-a` stated, confidently, that their corpus of Meta raw
payloads *"froze on migration day and does not grow any more"*. They had measured it: `LIKE '%entry%'` vs
`LIKE '%instancia%'` over the stored rows, and the boundary fell exactly on the day of their T-250. The
measurement was **right** — the shape of the stored row did change that day. The conclusion was wrong: the
zapgw envelope carries the raw body **inside it**, in base64 (`Envelope.Raw`,
`internal/inbound/deliver.go:44`), so what changed was the **wrapper**, not the content. They had 267 raws,
not 225, and the number **grows**. Worse: their own code comment, in the file they had open, said *"não
perdemos o cru"* ("we do not lose the raw").

The formulation is theirs and is the best part: ***"eu medi a forma do invólucro e concluí sobre a
existência do conteúdo. O antídoto não é medir mais — é NOMEAR O QUE A MEDIÇÃO RESPONDE antes de olhar o
número."*** ("I measured the shape of the wrapper and concluded about the existence of the content. The
antidote is not to measure more — it is to NAME WHAT THE MEASUREMENT ANSWERS before looking at the
number.")

*Real cost, measured on both sides: a channel document that would have lied to the next person, a capture
gap declared larger than it was, and — the most expensive — an **absence proven with a search incapable of
finding**: `forwarded = 0` measured with `LIKE` over the envelope text, where base64 hides the word. The
zero survived the correction of the method, but nobody knew that until it was redone.*

**And the opposite variant happened HERE on the same day, which closes the family:** the deployment runbook (kept in the private repository)
justifies the network design by asserting that there is a `cloudflared` tunnel in front of Traefik. The
planner repeated that **twice to the owner, with conviction, without measuring**; the owner measured with
`nslookup` and knocked it down (T-066). There, a right measurement with a wrong conclusion; here, no
measurement and an old doc treated as fact. **The doctrine — *check against the code, never against the old
doc* — was being applied to the parser all day and not to the topology, because infra docs "look like"
fact.**

**The two questions that generalize, and they are distinct:** (1) *what exactly does this measurement
answer?* — write the sentence before looking at the number, because afterwards the number has already
convinced you; and (2) *would this search be able to find what it is looking for?* — an absence only counts
as proof after the method has been exercised against a known positive.

**And the family's closing piece, found while FIXING the doc above (T-066): a "from outside" measurement made
from INSIDE the LAN is hairpin NAT — and it answers exactly like the internet would, right up to the moment
it does not.** The first attempt at the correction measured `:443` and `:8443` against the public IP
`186.236.224.2` **from a machine whose own egress IP is that same address** (`curl https://api.ipify.org` →
`186.236.224.2`). The router returns the connection inwards; nothing goes out to the internet. The three
measurements looked conclusive and were going to sustain a **security** claim — *"the sending port is not
reachable from outside"* — which happened to be **right** and was not **proven**.
*Cost: zero in production, and by luck. **A right conclusion obtained by a wrong method is the most
durable of all**, because nobody goes back to check what already gave the expected result — it would have
become the written justification for the network design, as the tunnel one had. What gave it away was not
the reasoning: it was a reading detail, a `200` obtained "from outside" matching the LAN response **byte for
byte**.*
**Fix: a genuinely off-network observation point (`check-host.net` nodes, 12 countries), with a mandatory
POSITIVE CONTROL** — `:443` connects from 8/8 nodes (São Paulo 10 ms, Singapore 331 ms) and `:8443` times out
on 12/12, **including the same São Paulo node that saw `:443`**. Without the positive control, "timeout
everywhere" is indistinguishable from "the tool cannot reach this address".
*And the tool has a failure mode of its own, which nearly restored the error by another route: one node
(`ua3`, AS13335) reported `:8443` **open in 2.7 ms** from Kyiv — impossible for a destination in Brazil,
where the best real node took 10 ms. **A single physically impossible false positive knocks down the right
conclusion if nobody looks at the latency next to the verdict.***
**The third question that generalizes, sister to the two above: from WHERE is this measurement being made,
and is that observation point the same as the one I want to describe?** Every claim about *"what the internet
can reach"* made from inside your own network is hairpin until proven otherwise.

**The fourth question: can your instrument see what you want to measure?** On 05/08/2026, `consumer-b`
measured `GET /v1/templates` twice, got 109 templates with `motivo` absent in all of them, and reported it as
evidence that the gateway was not returning the field. The conclusion was right. The evidence was not: the
Python client built a new dictionary with six fixed keys and discarded the rest — it could never have seen
`motivo` even if the field had been arriving. *"A field that does not exist on the object"* is the shape of
your mapper, not the shape of the data. The pitfall: the instrument filters before you see, and absence
becomes indistinguishable from real absence. **The defence is to read the raw JSON** (curl, jq,
`response.json()` without mapping) before it goes through the client you maintain.

**The same family, one level down: `ModeCharDevice` answers *"is it a character device?"*, not *"is there a
person in front of it?"* — and `/dev/null` is a character device.** Found while implementing the interactive
menu (T-082, 2026-07-28), whose rule is *"only open with no argument AND with a terminal on both sides"*. The
idiomatic terminal test in Go — `f.Stat().Mode()&os.ModeCharDevice != 0` — answers **true** for the null
device, **measured on both platforms** (Windows: `Dcrw-rw-rw-`, `char: true`; on Linux `/dev/null` is
`crw-rw-rw-` for the same reason). And `/dev/null` is exactly what **systemd** hands over as standard input
(`StandardInput=null`) and what a script writes when it does not want the output.

*Cost: zero, and only because the question was asked before committing — the failure mode would be expensive
and mute: `zapgw >/dev/null </dev/null` would open the menu, read EOF on the first question and the binary
would **exit with status 0 without bringing up the server**. A service that "started successfully" and is not
up is worse than one that failed. Fix: `isTerminal` (`cmd/zapgw/menu.go`) asks **two** questions — character
device **and** `!os.SameFile(info, os.Stat(os.DevNull))`, which compares file identity and is therefore immune
to redirection. The case is in the test (`TestWithoutTTYDoesNotOpenMenu`, both sides on the null device).*

**The question that generalizes is the same as the block above, with the target swapped: write the sentence
the check REALLY answers, and compare it with the sentence you wanted.** Here the distance between *"char
device"* and *"terminal"* fits entirely inside `/dev/null` — and the test that only exercises a pipe and a
regular file passes green without ever touching it.

---

**The mother pitfall applied to the ADVICE we give the consumer: the rule held when I wrote it and did not
hold four hours later, when I broke it myself.** On 2026-07-28, 12:45, we wrote to `consumer-b`: ***"do not
build an alarm on top of an absolute counter"*** — because an absolute counter in a system that has already
run carries history inside it, and a `> 0` threshold is born firing. At 16:28 the same day, announcing
`v0.26.0`, we wrote to them: *"`conta_descartada` going up is an alarm line you can turn on today"*. **That is
exactly what the rule forbids, said by the same person, on the same channel, on the same day.**

The one who caught it was them, going to implement and stopping: their instance **already had**
`conta_descartada: 1` and `numero_descartado: 4` — from **our own cutover tests**. A "greater than zero" alarm
would be born red over events they already knew the meaning of, and *"alarme que nasce aceso é alarme que se
desliga na semana seguinte"* ("an alarm born lit is an alarm that gets switched off the following week", their
formulation).

*Cost: zero, because they read their own history before coding instead of obeying. Had they obeyed, we would
have spent the only automatic alarm they have on that dashboard — and a switched-off alarm is worse than a
missing one, because it takes the gap away with it.*

**Why it slipped through, and this is what is worth copying:** the rule was written in a **project** message
("how to build an alarm") and broken in a **release** message ("what changed in this version"). Different
context, same subject — and nobody rereads the rules they gave while announcing a version. **The question that
saves the next one: before suggesting somebody build something, go find what you have already told THEM about
building that.** The channel is the record; it was there, with a date.

**And the technical corollary, which survives this case:** an absolute counter answers *"how much has happened
since forever"*, and an alarm asks *"did something change just now?"*. The two questions only coincide in a
system that has never run. An alarm wants a **delta** or a **stamp** — and that is why `GET /v1/estado`
publishes `ultimo_em` next to each key (T-060) and `carimbos_desde` at the top (T-070). *Whoever displays an
absolute counter, display it the way `consumer-b` did: highlighted and in red above zero, with the sentence
saying what the expected value is — visible to whoever looks, without ringing a bell for whoever did not ask.*

---

**"Refused" and "INEXPRESSIBLE" are different defences, and the mutation proved the first does not see what
the second prevents.** The sending contract had, for a migration period, **two** fields for a template button
parameter: `botoes_url` (old, URL only) and `botoes_template` (a union discriminated by type). The invalid
state — *the same button declared twice, in two fields* — was **refused** by a repeated-index guard. Refused,
and yet **expressible**.

*Real cost, and it is not zero: two tasks (T-044 to create the successor while keeping the old one alive,
T-045 to remove it) and a coexistence window that lasted until BOTH consumers confirmed in writing that they
had migrated — one of them with seven templates in production using the field. **All of that because the
invalid state was born expressible instead of impossible.** The alternative discarded at the time was worse:
keeping both forever.*

**What T-045's mutation showed, and it is the part you cannot guess:** on resurrecting a second button
field, the **entire** suite stayed green — **including the repeated-index guard**. The guard cannot see a
second field; it only knows how to compare indices inside the field it knows about. **Only the test that asks
THE TYPE** — sweeping the struct by reflection, counting fields that are *a list whose item has `Indice`*, and
requiring exactly one — goes red, **naming the new field**.

**The question that saves the next one: is this state refused, or is it impossible to write?** If it is
refused, somebody will write it — and the guard only catches the case it was taught to see. *A guard protects
against the error you imagined; a type protects against the one you did not.*

*(It is the same shape as T-048's lesson — "every hand-written list about the schema needs something next to it
that asks the schema" — applied to the struct instead of the database. When the same sentence shows up at two
different heights of the system, it is a rule and not a coincidence.)*

---

**The rule "never store the value in the clear, store the HMAC" held for TWO columns of the same table and not
for the THIRD, written in the same task, by the same hand.** T-091 (TRANSIT log, 2026-07-29) applied keyed
HMAC-SHA256 to the phone number (`hmac_contraparte`) and to the `wamid` (`hmac_wamid`) — both with a comment
explaining why an unkeyed hash is not enough and why `Cofre.Cifrar` does not fit. The `correlacao` COLUMN, right
next to them, wrote the consumer's `Idempotency-Key` **in the clear**, on the OUTBOUND side. It is free text of
EXTERNAL origin: nothing stops a reasonable consumer from using `pedido-5532999990001` or
`cliente-joao-silva-0912` as the key, and the table would then keep that string for 30 days — exactly what the
file's own field list forbade ("never... a name"). The same value also came out in `Transito.Record`'s
`log.Printf`, a SECOND place, with the journal's retention, not the table's.

*The irony worth recording: the same implementation **got the neighbouring defence right** — it refused
`err.Error()` in the `desfecho` field so as not to open a leak vector through a Meta error message, and
documented why. It opened the twin vector one field over, in the same struct, in the same commit.*

**How it showed up:** adversarial review BEFORE the merge (the planner, rereading the finished diff), not a test
and not production. No test by the author caught it, because the "does not store content" test
(`TestTransitoNaoGuardaConteudo`) only existed on the INBOUND side — where the question does not even apply,
because there `correlacao` is an opaque id the gateway generates itself. The author had even **flagged their own
decision in the final report** ("I used the Idempotency-Key as correlacao") — the signal was visible, it just
was not compared against the rule of the file they had themselves just written.

*Cost: zero in production — caught before the merge, no tag, no deploy. The cost it would have charged had it
gone through: 30 days of an arbitrary consumer string in a log designed to last exactly that window, plus the
same value in the journal (different retention, read by a monitor). Fix: `Store.HMACCorrelation`
(`internal/config/crypto.go`/`transito.go`), the SAME mechanism as the other two fields — and one test per
PACKAGE sweeping ALL columns for a sentinel planted in the Idempotency-Key
(`TestOutboundTransitDoesNotStoreTheIdempotencyKeyInTheClear`, `internal/outbound/transito_test.go`), mirroring
what already existed on the inbound side.*

**The question that generalizes, and it is the mother pitfall's with the target on ONE table instead of a whole
system:** when you protect a field with HMAC/encryption, ask **"which OTHER fields of this SAME row come from
outside, and do they have the same protection?"** — it is not enough to ask whether the protection is right
where it is; you have to ask where else, in the same struct, the same question fits.

---


## This project's second pitfall: "two checks that share a premise are not two checks"

**Raising the dead: a closure that is not WRITTEN does not close — it becomes residue that the next reading of
the history resurrects.** On 2026-07-28 the owner closed the `consultorio` subject in conversation, and it came
back **twice** the same day: once as a *"what is left over, and it is small"* paragraph in a channel section
written by me at 09:36, and again ten hours later, when the consumer reread the history, found the residue and
handed it back — and I turned it into a queue task. The owner's words on seeing the second resurrection: *"vcs têm
a mania de ficar levantando defunto"* ("you people have this habit of raising the dead").

**The cause was not anyone disobeying.** It was that both sides did the right thing with the wrong record: turning
a channel pending item into a queue task **is** the correct procedure — except that this pending item no longer
existed, and nothing in the repository said so. *The dismissal lived in the chat, which dies with the session; the
residue lived in the doc, which survives.*

*Cost: rework and wear on the owner, twice. And the hidden cost is worse — each resurrection spends the queue's
credibility: whoever sees a dead item coming back starts ignoring the whole queue.*

**The rule, and it is short:** when the owner closes a subject, **write the closure into the repository, with his
words and the date** — in the resumption block, which is the first text the next session reads. And write **why**
it is closed: *"the owner's decision"* and *"there was nothing there"* are different things, and whoever reads
three months from now needs to know which of the two it was, otherwise they reopen it "to confirm".

**The question that saves the next one: am I recording this as OPEN because it is open, or because I did not ask
whether it closed?** A paragraph of the *"what is left over, and it is small"* kind is where the dead hide — the
diminutive waives the check.

---

**A measurement control can have a PERISHABLE SOURCE — and the one that disappears is the side you do not
control.** On 2026-07-28, before provisioning the second instance, the column mapping was proven by comparing the
Evolution row (the Evolution LXC) with the gateway instance: both `waba_id`s and both `phone_number_id`s matched.
A good control, made at the right time. **That night the owner shut down the Evolution LXC** (measured: `stopped`),
and `consumer-a` warned — *"the first row cannot be redone any more"*.

**The bigger finding came from going to check the warning:** that measurement **was in no document of this
repository**. It only existed in the channel file, which is `*.local.md` and **ignored by Git**. It was not a
"number without provenance" — it was a number without a home, and it would vanish with the session. *In the same
check, the `consultorio` pending item was not in the queue either: it became T-077 in the same commit.*

*Cost: zero, because the result was superseded by better evidence — the instance is in production receiving and
delivering (`recebidas 72 = entregues 72` on the day), which proves the mapping more strongly than the column
comparison. **We did not freeze the hashes**, and the decision is deliberate: preserving a superseded control
creates the illusion that it is still the proof.*

**The two questions that remain:**

1. **When you make a control, ask how long the COMPARISON SOURCE is going to exist.** If it is a machine, a
   third-party service or a system being decommissioned, the result has an expiry date — and the moment to freeze
   it (a fixture, a doc line with date and origin) is while both sides still remember where it came from.
   Afterwards it becomes a number without provenance, which is worse than no number.
2. **A measurement that only exists in a channel does not exist.** A channel is `.local.md`, ignored, and dies with
   the session. *If a number was used to DECIDE something, it belongs in `docs/`, not in the channel.*

---

**The variant that caught me most today: asserting about THE OTHER SIDE of the channel without opening the file —
three times in one afternoon, all of them caught by the consumer.** It is not the same as the pitfall above; there
both sources were mine and shared a premise. Here **there was no source at all** — the assertion was born from
memory about a system that is not mine to grep, and for that reason internal review **had no way** to catch it.

| what I asserted | how it actually was | who knocked it down |
|---|---|---|
| *"behind `cloudflared`, Traefik sees `127.0.0.1`"* | there is no `cloudflared` | the owner, with `nslookup` |
| *"you already have per-message state to know which ones to redo"* | the inbound message is born without `status` over there | `consumer-a`, citing the file |
| *"both consumers asked for `PUT`"* | only one asked; the other never mentioned a verb | `consumer-a`, with `grep` on the channel itself |

**All three were incidental to the argument** — no decision changed when they fell. That is what makes them
dangerous: **a premise that does not sustain the conclusion gets no checking**, and it comes out written with the
same confidence as the ones that do. The reader has no way of knowing which leg was measured.

*Cost: zero in code, and high in another currency — the third of them was repeated on a THIRD party's channel
(*"the other one's too"*), spreading the false attribution to somebody who had no way to check. Docs and channels
are records; a wrong attribution in a record becomes wrong history.*

**The rule, and it is narrower and easier to follow than "check everything":** if the sentence is about the
code, the request or the behaviour **of the other side**, either you **open the file / cite the line**, or you
do not write the sentence. There is no middle ground, because there is no `grep` that would save you later.
*And if the premise is incidental — if the conclusion survives without it — the cheap move is not to check: it
is to **cut**.*

---

**The `wamid` CARRIES THE RECIPIENT'S PHONE NUMBER INSIDE IT, in base64 — masking `recipient_id` and leaving
the `wamid` produces a file that LOOKS masked and is not.** Raised by the T-069 implementer (2026-07-28) while
freezing real captures into the corpus, and **checked by the planner** by decoding a production `wamid`:

```
$ echo "wamid.<the part after the dot, from a REAL wamid>" \
    | base64 -d | strings
55DD9NNNNNNNN           <- the recipient's phone number, in clear text
CB8A8835D1365DD0C3      <- the rest, that one really is opaque
```

> **This block does NOT carry a real `wamid`, and the omission is deliberate** — it was written on 2026-07-28
> with a production value, and removed the same day on noticing the irony: an example that **proves** a
> `wamid` carries a phone number cannot carry one. To reproduce, take a `wamid` from your own journal. *This
> is an instance of the rule below, committed by the person who was writing it.*

The number comes out in WhatsApp's canonical form (for Brazil, **without the ninth digit**), so a search for the
number as a human writes it — `55329...` — **finds nothing**, and the review passes clean. It is the same family
as *"would this method find a positive?"* (see this project's second pitfall): whoever greps the number in the
form they know concludes "it did not leak" from a search incapable of finding it.

*Cost: zero, and only because the implementer opened the `wamid` instead of trusting their own masking. Had they
swapped only `recipient_id` and `display_phone_number`, the phone number would have entered Git inside the id —
and **Git does not forget**: the fix would not be `git rm`, it would be rewriting history or accepting the leak.*

**The rule, and it holds for every piece of data that becomes a fixture:** when masking, **decode every field
that could be a structured identifier** before deciding it is opaque. `wamid` is base64; an id that "looks
random" may be a structure with data inside. **The question that saves the next one: is this field opaque, or did
I just not try to open it?**

🔥 **2026-08-30 — IT STOPPED BEING HYPOTHETICAL: it had already bitten, in a CONSUMER's repository, and nobody
knew.** In a channel test with `consumer-b`, they masked the owner's phone number in the request body
(`553298463XXXX`) and pasted the **whole** `wa_message_id` two lines above — whose base64 payload is the same
number, complete. *Their care was real and aimed at the right place; what failed was the mental model that
`wamid` is opaque.*

We warned them on the channel. **They went looking with the `grep` that decodes — and found a `wamid` COMMITTED
in their repository, in a webhook test, from weeks earlier**, carrying the same number.

*Real cost: a personal phone number inside another repository's history, discovered by chance weeks later — and
the correction there is the same it would be here: rewrite history or accept it. Zero immediate damage (private
repo), and no credit for us: **the warning did not prevent it, it revealed it**.*

**And the outcome, measured the same day, is the best argument in this whole file:** they wrote their own guard
with decoding and it found **four** occurrences, not one. Two were `wamid`s (*"the same number in a different
position of the payload produces different base64"* — so a `grep` for the first one's fragment did not find the
second); one was a **12-digit** `de_cru` in a fixture, which their search was not even looking for. **And the
other two were inside texts describing THIS pitfall** — a comment from 28/07 explaining *"inside the `wamid` is
the phone number in base64"* and carrying the number in clear text on the next line, and their own
`ARMADILHAS.md` entry on the subject, written **an hour before** the guard existed, illustrated with the real
`wamid`.

🔥 **They had known the pitfall for a month, wrote about it twice, and leaked both times — including inside the
text that described it.** *Knowing does not protect; the guard protects.* It is the same sentence from the public
repo's `CLAUDE.md` (*a rule without a mechanism is decoration*) with a real cost behind it, and the best
formulation of the defence is theirs: **the guard keeps only the SHA-256 of the numbers — a guard that carries
the number it protects is the very thing it forbids.**

🔴 **The two lessons, and the second is the one that changes behaviour:**

1. **A warning about a format travels badly and arrives late.** This block has existed since 2026-07-28 and
   describes exactly the leak that happened on the other side. It did not reach the person who needed it because
   it **lives in our repository** and the consumer reads `CONTRATO-CONSUMIDOR.md`. *A pitfall only the maintainer
   knows protects only the maintainer.* — when a format of OURS carries sensitive data, the warning is a contract
   obligation, not an internal note.
2. **The gate that decodes `wamid` only exists here.** `TestNoPhoneNumberOutsideTheAllowlistInTheRepo` sweeps
   `cmd/`, `internal/` and `testdata/` of this repository. No consumer has an equivalent, and **each of them
   stores production `wamid`s by design** — it is the id we use to ask them to deduplicate. *We distributed the
   format; the gate stayed home.*

---

🔥 **VARIANT OF THE SAME PITFALL, and it caught the person writing about it (2026-08-20): "I decoded the 49 `wamid`s and none carries the phone number" — false, and the three reasons are independent.**

The planner swept the tree decoding `wamid`s, reported **zero** occurrences of the phone number, **and ran a
positive control that passed**. Both facts were true; the conclusion was false. A third party's real number was in
a `wamid` in `internal/config/transito_test.go` the whole time, in base64, and only showed up when the T-161
implementer opened that file for another reason.

**Three defects, and each one alone was enough for the "clean" verdict to come out wrong:**

1. **A decoding failure was treated as an absence.** The value in the file is `wamid.` + prefix +
   base64-of-the-phone-number + metadata. Decoding the whole capture from the start blows up with
   `Incorrect padding`; the code caught the exception, moved on, and counted that `wamid` as clean. *The right
   chunk, isolated, decodes with no effort at all.*
   **`I could not open it` ≠ `it is clean`** — it is the blind-monitor rule, applied to a decoder.
2. **The positive control had the shape of the INSTRUMENT, not the shape of the DATA.** The `wamid` fabricated for
   the test had its base64 starting at offset zero and with correct padding — exactly the case the decoder knew how
   to handle. **A control like that proves the instrument runs, not that it sees.**
3. **The sweep was looking for the WRONG number.** The number was extracted from `README.md` by regex, and the
   `README` already contained the **synthetic** one — which matched first. Even a perfect decoder would have
   answered, precisely, about the number that did not matter.

*Real cost: the measurement was handed to the owner as fact and used to SHRINK T-159's scope — "`wamid` does not
need regenerating". A consumer's customer's phone number stayed in the tree, in 7 files, **two of them
production**, until T-161 found it by another route. It did not reach the public because the repository is still
private; in an open repo, it would be irreversible.*

**The three rules that remain, and the second is the one almost nobody applies:**

- **A read failure counts as a FINDING, not as clean.** A sweep that could not open a value reports that value,
  for a human to look at. Silence is reserved for what was read and was fine.
- **A positive control is made with a REAL specimen of the data, not a fabricated one.** Take a `wamid` from the
  corpus itself, hide in it the value you are looking for, and prove that the sweep finds it *in that form*.
  Fabricating the control out of what your code already handles is writing the proof for the defence.
- **Check WHICH value you are looking for before concluding about it.** Extracting "the number" by regex from a
  file that contains several returns the first one, not the right one.

✅ **Hole CLOSED by T-162** (`internal/config/telefones_allowlist_test.go`). T-161's gate matched only
`\b55[0-9]{10,11}\b` — a literal number; a phone number inside a `wamid`'s base64 walked past it. Now each line is
swept twice: by the literal (as before) and by `phoneNumbersInsideTheWamid`, which decodes every `wamid.<payload>`
found — trying the TWO possible window lengths that correspond to 12 or 13 ASCII digits (16 or 18 base64
characters), at every possible offset, never the whole capture at once (which blew up padding). The numbers coming
out of either front go through the SAME `syntheticPhoneAllowlist` — one list only. A decoding failure (no window
produced legible text) is a finding, not an absence: the test fails asking for a human eye, marked
`NAO DECODIFICOU`.

🔴 **And the mechanism found a second real number in the very movement of building it — it is not a hypothesis,
it is what happened.** In `cmd/zapgw/transito_test.go`, an earlier task had already swapped the literal `numero`
for the synthetic `5511999990000`, but the `wamid` constant on the next line, with the SAME phone number embedded
in base64, was left untouched — because no sweep until then looked inside the base64. Decoded: a real third-party
number, area code 32, ending in `...10` (different from the `(32) 9xxxx-xx72` number T-161 had already found and
swapped — this is ANOTHER number, not the same one reappearing). Fixed in the same commit: the `wamid` was
rewritten with the same `HBgN` prefix and the same metadata suffix as the original value, changing only the phone
number stretch to match the already-synthetic `numero` on the line above. *It confirms the lesson of the 🔥 variant
above: "I decoded and found nothing" only holds for what the decoding actually reached — the already-clean literal
`numero` next to a dirty `wamid` is exactly the kind of neighbourhood that fools whoever only checks the obvious
field.*

**A new pitfall, found while building the positive control:** decoding at SEVERAL window lengths (not just one)
produces an "almost right" number that is not a real form of the phone number — it is the same number cut at the
last digit by the shorter window. The first version of this function tried any length from 12 to 24 base64
characters and, for the synthetic `wamid` in `internal/config/transito_test.go`, produced TWO findings: the right
number (13 digits) and a second "number" that is just the same one with the last digit cut off — never declared
anywhere because it never existed. The correction restricts the search to the TWO lengths that correspond to 12 or
13 ASCII digits (16 or 18 base64 characters) and, at each offset, prefers the 13-digit one — it only falls back to
the 12-digit one if the 13-digit one does not decode legibly there. *Rule that generalizes: when extracting a
fixed-size datum with a sliding window, testing lengths "close to" the right one is not more rigorous — it is the
way to invent a finding that does not exist.*

**The positive control uses a REAL `wamid` from the corpus** (`internal/config/transito_test.go`,
`wamid.HBgNNTUzMjk5OTk5MDAwMBUCABIYFjNFQjBEO`, which decodes to the synthetic `5532999990000`, already
allowlisted) — it swaps only the phone-number stretch for a number outside the allowlist, preserving the original
prefix and suffix, and proves that the sweep finds it, pointing at file and line. Fabricating a `wamid` from
scratch, with the base64 aligned at offset zero, is exactly the error that produced the earlier wrong "clean" — see
the 🔥 variant above.

**And the corollary that is not about fixtures, and is the most valuable part:** the `wamid` travels in the
envelope, in the logs and in the consumers' databases. It is **not anonymous** — treating it as a neutral
identifier in a log, a ticket or an error message publishes the phone number along with it. *Whoever pastes a
`wamid` into a channel, an issue or a screenshot is pasting the number.*

🔴 ***And the cost stopped being hypothetical the same day.*** The warning was sent to consumer `consumer-b` on
2026-07-28 17:42 as a footnote (*"if you log `wamid`, worth knowing"*). They went to check and **were logging the
`wamid` on EVERY processed-event line** — every customer's phone number, in clear text inside the id, in
production. Fixed and live within minutes (they switched to logging the internal pk; traceability survived because
the whole id is still in the database).

***What made the warning work was the detail that nearly got left out:*** the observation that the number comes out
**without the ninth digit**. Their words: *"sem isso eu teria grepado o número do jeito que a gente escreve, não
achado nada, e concluído que estava limpo"* ("without that I would have grepped the number the way we write it,
found nothing, and concluded it was clean"). **A shortened warning would have produced a search that does not find
and a wrong conclusion** — it is the second pitfall's third question (*would this method find a positive?*) applied
by a third party, about their own system. **When you warn about a leak, send the exact format of what to look for,
not just the field name.**

---

> **The formulation is consumer `consumer-b`'s, 2026-07-28**, answering why a convergence had fooled them:
> ***"o que separa é a FRONTEIRA, não a quantidade."*** ("What separates is the BOUNDARY, not the quantity.") It
> unifies five episodes of the same day that looked like different problems, and that is why it earned a section of
> its own instead of becoming one more paragraph.

Checking twice is only worth something if the two checks **can disagree**. Two checks on the same side of a
boundary are one check with two names — and they are **worse** than one, because the agreement between them
produces confidence that neither deserves on its own.

The five cases from 2026-07-28, all with the same anatomy:

| what was checked | the two "sources" | the premise both shared | how it showed up |
|---|---|---|---|
| the `app_secret` at cutover | smoke test + `/v1/health` | *"the secret I hold is the one Meta uses"* | Meta signing with the old one, 8 min of rejections |
| the cutover having worked (consumer) | their database + their log | both sit **after** our delivery | *"two sources, one blindness"* — they only knew because we told them |
| grey visual alarm (consumer) | DOM attribute + render test | *"the CSS variable `--co-bad` exists"* | only the **computed** colour in the browser gave it away |
| the raw corpus having frozen (`consumer-a`) | `LIKE '%entry%'` vs `LIKE '%instancia%'` | *"the shape of the stored row says what is inside"* | the `cru` was there, in base64 |
| `:8443` closed to the internet | two `curl`s "over the public IP" | *"this machine is outside the network"* — and it is not | hairpin NAT; `api.ipify.org` returned the same IP |

**A non-existent CSS var does not break** — the browser discards the declaration and the number inherits the normal
colour. **`LIKE` over base64 does not find the word.** **Hairpin does not leave the network.** In all of them the
instrument answered confidently a question it was not capable of answering.

**The three questions that break the pattern, and they differ from each other:**

1. **"Could these two sources disagree?"** If not, you have one. Count boundaries, not sources.
2. **"Does this measurement answer the question I am going to use it for?"** — `consumer-a`'s formulation: *name
   what the measurement answers BEFORE looking at the number*, because afterwards the number has already convinced
   you.
3. **"Would this method find a positive?"** An absence only counts as proof after the search has been **calibrated
   against a known positive** — that is how T-066 proved `:8443` closed (12 nodes, with `:443` as the control) and
   how `consumer-a` redid the `forwarded = 0` by decoding the base64.

**The corollary this project was already using without having named it: real proof is the COUNTERPARTY.** It is not
"one more check" — it is a check **on the other side of the boundary**, which is the only place from which the
shared premise is visible. That is why one `curl` from outside is worth more than ten from inside, the consumer's
report is worth more than our journal, and a written clearance **citing the line** is worth more than a green
suite. *It is also why this project holds a deploy waiting for a consumer's answer: the cost is minutes, and it is
the only evidence the suite cannot produce.*

**And the case where the boundary was respected, to serve as a model:** on 2026-07-28, `consumer-b` measured
`recebidas 56 = entregues 56` **on our production**, from their container; we checked from the gateway side, with
the journal and the counters — two readings genuinely from opposite sides, which **could** have disagreed and did
not. That is what "checking twice" should always mean.

---

🔥 **The value of a READ endpoint is not to list — it is to DISAGREE. And it was a `GET` nobody thought important
that found the root cause, in fifteen seconds.**

2026-08-20, at consumer `consumer-b`, an hour after they had written to us the sentence *"the symptom of a block
that worked and of one that failed are the same silence"*. An `Enter` in a text field submitted the form **without a
`submitter`**; the `onsubmit` did `event.submitter.value`, **threw** — and an `onsubmit` that throws **does not
cancel the submission**. The `confirm()` never appeared, the `alcance` field was not sent, and the server had
`request.POST.get("alcance") or LOCAL`: **it chose on its own**.

Result: a block was requested at Meta, a local one was written, the screen said "blocked", **the audit log recorded
"blocked"** — and the number went on writing. *Our route was never called.* No layer lied; each one told the truth
about its own piece.

🔑 **What broke the silence was `GET /v1/bloqueios`**, which answered `{"total":0}` while their database said
"blocked". **Two sources that do NOT share a premise**, disagreeing — it is the only thing that turns *"I think it
did not work"* into a root cause. An endpoint they had catalogued as *"audit, not render"* was the first tool they
reached for.

**The rule, and it is the argument for building reads even when they look useless:** *your database cannot
contradict you — it only repeats what you wrote into it.* Only a read of the **external source** can disagree. A
`GET` that mirrors the third party's truth is cheap, has no side effects, and is the only independent witness that
exists when a write fails silently.

*This project already had the same pattern written elsewhere without naming it: the contract says
`GET /v1/templates` is "a porta de conferência quando o `POST` termina ambíguo". **It is the same rule, and now it
has two cases.** When you add any new write, ask: is there a read capable of contradicting it? If there is not, the
only failure detector is somebody complaining.*

## Go / concurrency

**`go test` does NOT detect a race if no test is concurrent — and the suite stays green.** The handler's
correlation counter did `h.seq++` on a shared `*Handler`; `http.Server` serves each request in a goroutine over
the **same** handler. `go test -race ./...` passed clean, because **no test fired concurrent requests**. It only
showed up when the reviewer wrote a test with 200 goroutines. Fixed with `atomic.Int64`.
*Cost: one Critical. The detector was available the whole time and had nothing to detect.*

**Every HTTP `Handler` with mutable state needs a concurrent test run under `-race`.** Without one, `-race` is
theatre.

---

## Go / JSON

**`json.Unmarshal` of `null` into a map does NOT return an error — it leaves the map `nil`.** And `null`, `42`,
`[]`, `"text"` and `true` are **syntactically valid** JSON. The code that follows, assuming data, moves on
believing it has some. It has to become a named error (`ErrBodyNotObject`) with an explicit `nil` check.
*Cost: caught by a test written before the code. The Python equivalent (`json.loads("null")` not raising
`ValueError`) has already cost a Critical in another project of this network — the pitfall crossed the language
boundary changing mechanism and keeping the outcome.*

**A `null` or `{}` item inside a list also does not fail the `Unmarshal`** — it becomes a zeroed struct. In the
parser that produced an `Evento` with `ID == "msg:"`; **two of them collided in the consumer's dedup**,
contradicting the uniqueness promise written on the type itself. Guard: an item without Meta's `id` does not
become an event, it is counted as ignored.
*Cost: caught in the T5 review, before any consumer existed.*

**A single `Unmarshal` over an entire tree fails entirely.** If the structure has levels (`entry` → `changes` →
`messages`), each level needs to be a `json.RawMessage` and deserialized on its own — otherwise one field with the
wrong type in any leaf wipes everything.
*Cost: **Critical no. 1**. Meta batches `entry` from different accounts in the same call, so one malformed payload
from one customer would wipe another's valid messages.*

**And the same reasoning holds for a NEW field Meta adds inside an already existing leaf — not only for the levels
that already had `RawMessage`.** T-028 (failure reason in the status) could have modelled `errors[]` as
`[]erroMeta` directly inside `statusMeta`; a `code` arriving as a string (Meta has already done that in another
field — see `tolerantInt` in `errors.go`) would make the `Unmarshal` of the **whole** `statuses[]` item fail, and
the already existing `ignorados++` guard would discard the **whole** status — id, status and timestamp along with
it, not just the reason. Preventive fix: `statusMeta.Errors` is `[]json.RawMessage`, and each item is deserialized
separately; a malformed error item leaves `Evento.Erro` nil (losing only the reason) without bringing down the rest
of the event.
*Cost: zero — found while designing T-028, applying the mother pitfall's question ("where else should this rule
hold?") to a field that did not even exist when the rule was written. Proven by
`TestParseWebhookInAStatusErrorAMalformedItemDoesNotBringDownTheEvent` (`internal/meta/parse_test.go`): reverting
`statusMeta.Errors` to `[]erroMeta` would be the obvious "simplification" that reopens the hole.*

**And the opposite is true too, and it nearly became a line of code claiming a mechanism that did not exist: a
NESTED object that may be missing does not need a pointer just because "it may be missing".** While writing T-029
(`error_data.details` in the status reason), the first draft of `erroMeta.ErrorData` was `*errorDataMeta`, with a
comment asserting that only the pointer distinguished "`error_data` absent" from "`error_data: {}`". The assertion
was never confirmed against `encoding/json` — and it is false: an experiment test (`json.Unmarshal` of a missing
field, of `error_data: null` and of `error_data: {}` over a **flat** struct) showed all three cases as an identical
no-op, with no error — the package documents that `null` over any type that is not a pointer/interface/map/slice
has no effect. Since the final `Detalhes` also does not distinguish "object absent" from "object present without
`details`" (both become `""`, by the task's own decision), the pointer protected nothing the flat struct did not
already handle; what would remain is a comment asserting a reason the code did not have.
*Cost: zero — caught BEFORE the commit, by applying this project's doctrine ("check every assertion against the
code, never against the old doc") to **the very comment I was writing**, not only to somebody else's code. Fix:
`erroMeta.ErrorData` is `errorDataMeta` (a flat struct); only an `error_data` of the wrong **type** (e.g. a string)
brings down the whole item, and that case already had a guard (malformed item → `Evento.Erro` becomes `nil`, the
same family as the entry above). **The question that generalizes: before giving a field a pointer "because it may
be missing", ask what behavioural DIFFERENCE the pointer would buy — if the answer is none, it is ceremony, not
defence.***

**The two entries above look contradictory (one asks for `RawMessage`, the other for a flat struct) — and the
difference is the field's DEPTH, not the field itself.** T-041 (`pricing` in the status webhook, becoming
`cobranca` in the envelope) needed both rules at once, one at each level: `statusMeta.Pricing` is
`json.RawMessage` (the `errors[]` paragraph's rule, above) because "pricing" is a **sibling** of
`id`/`status`/`timestamp` inside `statusMeta` — without isolation, a `pricing` of the wrong type would break the
`Unmarshal` of the **whole** status, the same fields that would survive a malformed `errors[0]`. Inside it,
`pricingMeta.Billable` remains a flat struct (not even the whole `pricingMeta` is a pointer) because, once isolated
by the outer `RawMessage`, there is no neighbour left to protect — the same logic as `errorDataMeta`. **The
question that decides which of the two rules applies to a new field: is it a sibling of fields that already work
today (RawMessage), or is it nested INSIDE something that already isolates (flat struct)?** Proven from both sides:
`TestParseWebhookAMalformedStatusPricingDoesNotBringDownTheEvent` (`internal/meta/parse_test.go`) goes red if
`Pricing` became `pricingMeta` directly; `TestParseWebhookAStatusWithNullPricingGetsNoBilling` proves that
`"pricing": null` must not become a zeroed `Cobranca` (the same reasoning as "malformed error item", applied to
null instead of the wrong type).

**Defensive typing applied to one field and not to its neighbour: the rule of the entry above existed, written and
proven, and the field next to it went without it for two plans.** T-043 (2026-07-26) put `json.RawMessage` on
`entry.time` *precisely* because a divergent type there would bring down the whole batch — and in the same file,
two levels down, `mensagemMeta.Context` was still a **flat** struct, with the defence missing exactly where the
payload is the customer's. Consequence of a `"context"` (or any field inside it) arriving with a **type** different
from the expected: the `Unmarshal` of the **whole message** failed and it became `ignorados++` — gone from
`eventos`, which is the list every consumer deduplicates and acts on. The `cru` was still delivered, with
`parse_error` filled in (`internal/inbound/handler.go:194`), but the contract tells the consumer to act on
`eventos`: in practice, the customer's message became invisible. **With no `ALARME` and no counter** — only a
journal line, and this file already records that nobody reads the journal out of habit. `midiaMeta.Voice`
(`*bool`) had the same fragile shape since **plan 1**, which makes the pair the classic asymmetry: the same
sentence held in three fields and did not hold in two, all in the same file.

*Cost: **unknown by construction, which is worse than "zero"** — the defect does not produce a WRONG result, it
produces an ABSENT one, and neither consumer has any way to notice a missing message that never arrived. There is
no evidence of real loss, and there could not be. It lived from 2026-07-23 (`voice`) and 2026-07-26 (`context.id`,
T-032) until 2026-07-28 (T-061).*

**The discovery mode is what is worth copying, and it is a SEVENTH way:** nobody reviewed this code, and no new
traffic arrived. The **T-059** implementer went to add two fields (`forwarded`, `frequently_forwarded`) to
`contextMeta` and **refused to protect only the new fields**, because isolating the new one while leaving the old
exposed would be this file's mother pitfall with the sign flipped — they wrote the refusal in the code's own
comment and opened the task. That is: **the trigger was ADDING a field to a struct that already existed.** It works
as a cheap review step — *when you hang a new field on a parse struct, ask what depth of isolation its SIBLINGS
have, not just what the new field needs.* It is a sibling of the fifth way (saying out loud the asymmetry you have
just created), with the difference that here the asymmetry **was already there** and the new task merely walked
over it.

*Fix (T-061): `mensagemMeta.Context` and `midiaMeta.Voice` become `json.RawMessage`, read by `contextoDaMensagem`
and `tolerantBool` (`internal/meta/parse.go`) — the first became `blocoDaMensagem[T]` in T-062, just below, and
therefore no longer exists under that name; an unexpected type degrades **the block**, with the message still
delivered. `contextMeta` **remains a flat struct** — by the depth rule of the entry above, now that the outer field
isolates. **This changes observable behaviour** (`responder_a` now goes missing on its own instead of vanishing
along with the message) and is therefore in `docs/CONTRATO-CONSUMIDOR.md`, "Mudanças que quebram".*

**Four mutations, done and reverted before the commit — and the third and fourth are worth more than the two
obvious ones:** (1) reverting `Context` to `contextMeta` leaves
`TestParseWebhookAContextOfTheWrongTypeDeliversTheMessageWithoutCountingIgnored` and two corpus fixtures red, with
`ErrPartialParse` and `len(evs)` dropping; (2) reverting `Voice` to `*bool` does the same with the audio fixture;
(3) making `contextoDaMensagem` reuse what `encoding/json` **had already decoded before the error** (it keeps
decoding and only returns the `UnmarshalTypeError` at the end — confirmed by experiment, not assumed) leaves
`TestParseWebhookAnUnreadableContextDiscardsTheWHOLEBlockNotJustTheBadField` red: **it is the only proof that "the
block degrades as a whole" is a decision and not an accident**; (4) swapping `tolerantBool`'s `var b *bool` for
`var b bool` leaves `TestParseWebhookANullVoiceDoesNotBecomeFalse` red, with `voz: false` in place of absent —
`null` over a `bool` does not error and is not a no-op when the destination is a value, and that is the same family
as the first entry in this section.

**The question that would have caught it, and it is free:** when you write `json.RawMessage` on a field *"because
an unexpected type here would bring down the rest"*, look at the **siblings declared in the same struct** and ask
why the sentence does not hold for them. If the answer is "nobody had a reason to think about it", you have found
the next one.

**And the question above was asked, answered, and still the class stayed open for one more round — because asking
identifies the next FIELD, and what was missing was moving the BOUNDARY.** It is the most expensive entry in this
section for what it cost in *process*, not in production: **the same defence was applied THREE TIMES, field by
field, before anyone treated the class.**

| Round | Field hardened | What was left open |
|---|---|---|
| T-043 (26/07) | `entry.time` | the 15 fields of `messageMeta`, two levels down, in the same file |
| T-061 (28/07) | `mensagemMeta.Context` and `midiaMeta.Voice` | the remaining 13 siblings, in the **same struct** |
| T-062 (28/07) | *the whole struct* | the neighbours in OTHER structs — measured and listed below |

**The cost, counted: two whole tasks and one accidental discovery.** T-061 only existed because the **T-059**
implementer went to hang two fields on `contextMeta`, noticed the siblings were unprotected and opened the task
instead of hardening only what they were touching — nobody had gone looking. T-062 only existed because the
**T-061** implementer, on finishing, measured the neighbours with `ParseWebhook` and reported `"text":"oi"` ·
`"audio":"x"` · `"interactive":"x"` · `"reaction":"x"` · `"button":"x"` → `len(evs) = 0` + `ErrPartialParse`,
instead of quietly fixing it or saying nothing. **Both times, the finding came from somebody who was passing
through the file.** No review, no test and no traffic found this in five days, and the suite was green the whole
time.

*Cost in production: **unknown by construction**, as in the entry above — the defect produces an ABSENT result, not
a wrong one, and no consumer has any way of noticing a missing message that never arrived. It lived from plan 1
(2026-07-23) until 2026-07-28.*

**The lesson that generalizes, and it is different from the free question above:** *"where else should this
sentence be true?"* returns a **list of fields**, and fixing a list leaves tomorrow's list open. When the answer is
*"in every sibling of this struct"*, the correction is not to harden the siblings one by one — it is to **make the
whole struct the boundary**, and to leave behind something that goes RED when the next field is born. The closing
question: **"what here is going to warn the next person, without depending on them having read this?"**

*Fix (T-062): every field of `messageMeta` (`internal/meta/parse.go`) is a `json.RawMessage`, with no exception —
a `json.Unmarshal` whose fields are all `RawMessage` cannot fail over any JSON object. Each block is read by
`blocoDaMensagem[T]`, which is T-061's `contextoDaMensagem` with the type opened up (**the same function, not a
second mechanism** — one helper per field is precisely what makes the next round harden only that round's field).
What is left for the next person is `TestMessageMetaIsolatesEveryFieldByConstruction`
(`internal/meta/parse_test.go`), which walks the struct by reflection and fails naming the field on the day
somebody hangs a concrete type there; and `TestParseWebhookNoFieldOfTheWrongTypeErasesTheMessageNorItsSiblings`,
which sweeps **the payload's own keys** instead of a hand-written list.*

**A distinction was born here and stands on its own: "block ABSENT" and "block UNREADABLE" are not the same thing,
and confusing them erases messages.** Both produce the same envelope (the block does not appear), but they say
different things about *who* failed: absent is **Meta** stating that there is no block — a `type:"reaction"`
without a target is a payload that does not close, and is still discarded; unreadable is **our** parser not
understanding what arrived, and that may very well be a **new** format, not a broken payload. Erasing the message
because we did not understand one of its blocks is charging the consumer the price of our lag — with a `200`
answered to Meta, which therefore never resends. `blockState` (`internal/meta/parse.go`) has three values because
of this, and `null` counts as **absent**: it is Meta saying in so many words that there is no block.

**The NEIGHBOURS T-062 left open, measured with `ParseWebhook` on the day of the fix and written here so as not to
depend on somebody tripping over them.** The question *"where else should this same sentence be true, and is it?"*
was asked and the answer is **in four more places, and it is not**:

| Struct | Field with an unexpected type | What is lost | Radius |
|---|---|---|---|
| `valueMeta` | `metadata` or `contacts` | **the whole `change`** — all the messages AND all the statuses of that batch | worse than the defect T-062 fixed |
| `changeMeta` | `field` (string) | the whole `change`, same as above | worse |
| `statusMeta` | `id`, `status`, `timestamp`, `recipient_id` | the status event | same as the message one, in the direct sibling of the same loop |
| `entryMeta` | `id` (the `waba_id`) | the whole `entry` (the sisters from other `entry`s go on) | worse |
| `templateStatusMeta` | `message_template_name`, `reason`, `event`, ... | the template event | same |

Literal measurement (a good message + a good sister in the same batch, one field swapped at a time):
`value.contacts` → `len(evs)=0`; `value.metadata` → `0`; `contacts[].profile` → `0`; `change.field` → `0`;
`status.id`/`status.status`/`status.timestamp`/`status.recipient_id` → the sister survives and the status
disappears; `template.reason`/`template.name` → `0`. **`valueMeta` is the most expensive of the five: a
`"contacts":"x"` wipes out a customer's entire batch of messages, which is exactly this file's Critical no. 1 under
another name.**

*Three mutations, done and reverted before the commit. **The third found more than it proved:** (1) returning
`mensagemMeta.Text` to the concrete type leaves `texto_de_tipo_errado_sintetico.json` and the sweep's `text`
sub-test red, with `len(evs)` dropping from 2 to 1, and the reflection test red naming `mensagemMeta.Text`; (2) the
same with `Reaction` → `*reactionMeta`; (3) making `messageBlock` treat `null` as read left no test red — **it
brought down the whole suite with a nil-dereference `panic`**, because `*p` over a `null` has nowhere to point.
The comment at the top of `parse.go` promises that this parser **never panics**, and the `p == nil` was, without
anyone having noticed, what sustained that promise beyond the semantics. **The lesson: a mutation that panics
instead of going red is telling you that line holds two guarantees, not one** — worth asking what the second one is
before reverting.*

**And the five neighbours in the table above were closed in the following round (T-068) — the family's fourth, and
the first in which the finding did NOT cost a task to be discovered.** That is the difference worth recording:
T-061 showed up because somebody went to hang a field; T-062 showed up because the T-061 implementer measured the
neighbours on finishing. T-068 was already **written, measured and with its radius calculated** in this section
before it existed — the T-062 implementer measured the five with `ParseWebhook` and reported instead of quietly
fixing, and the table became the task. *The question that generalizes: when you close a class, measure the
NEIGHBOURING class the same day and write down the number — the next person starts from the measurement, not from
the suspicion.*

*Fix (T-068): **seven** structs in `internal/meta/parse.go` are the boundary, not one — `envelopeMeta`,
`entryMeta`, `changeMeta`, `valueMeta`, `messageMeta`, `statusMeta` and `templateStatusMeta`, all with EVERY field
a `json.RawMessage`. **The lists too** (`entry`, `changes`, `messages`, `statuses`, `contacts`, `errors`) are
`json.RawMessage` and not `[]json.RawMessage`, for the reason already written in `mensagemMeta.Errors`: per-ITEM
isolation does not protect against the whole LIST having the wrong type. `metadataMeta` and `contactMeta` remain
**flat** structs, by this section's DEPTH rule — an unreadable `profile` costs that contact, and the other
customer's name from the same batch still arrives
(`TestParseWebhookAnUnreadableProfileCostsOnlyThatContact`).*

*Two things the fix brought beyond the isolation, and both are anti-divergence:*
*(a) **a single traversal** (`forEachChange`) serves `ParseWebhook` and `AccountWabaIDsInPayload`. While the upper
levels were concrete types, the two functions could repeat three lines of `Unmarshal` at no risk; with DEGRADED
reading at every level, two copies would diverge — and the divergence would be between what the parser DELIVERS and
what the isolation guard CHECKS, which is this section's most expensive shape. (b) `changeDeTemplateMeta` **stopped
existing**: with `changeMeta.Value` raw, both shapes of `value` are read from the same block, each into its own
struct, without the second `Unmarshal` of the whole `change`.*

***The task's DECISION, and it is not the parser's: an unreadable `entry.id` does NOT become "pass".*** The
`waba_id` is the only routing key of an account webhook, and guard 5b (`internal/inbound/handler.go`) now treats
`""` — absent **or** unreadable — as unmatched, refusing the batch with `ALARME` and `conta_descartada`. The other
way out the task allowed (discarding only the `entry`, with a counter of its own) **was refused because it is a
defence that only looks like one**: the guard refuses the whole batch precisely because the **raw** body goes along
in the delivery, so filtering the `eventos` would let that account's content arrive anyway. *And here, on purpose,
"absent" and "unreadable" get the same answer — the distinction in the entry above exists to decide whether WE
DISCARD the consumer's content, and this guard's question is another one: "can it be PROVEN that this webhook
belongs to this instance?".*

*Eight mutations, done and reverted before the commit. **The last two yielded more than the proof they asked
for:*** (1)-(6) returning `valueMeta.Metadata`, `valueMeta.Contacts`, `changeMeta.Field`, `entryMeta.ID`,
`statusMeta.Status` and `templateStatusMeta.Reason` to the concrete type leaves that struct's fixture red **with
`len(evs)` dropping** (2→0, 2→0, 2→1, 2→1, 2→1, 2→1) and `TestBoundaryStructsIsolateEveryFieldByConstruction` red
naming struct and field — and the `entryMeta.ID` one also brings down
`TestHandlerRecusaWebhookDeContaComWabaIDIlegivel`, with the message *"entregou webhook de conta cujo waba_id nao
pode ser lido"*, which is **the production defect reproduced in full**; (7) returning `envelopeMeta.Entry` to
`[]json.RawMessage` revealed a hole I was creating myself: the `return 0` on `forEachChange`'s error path made
`{"entry":"x"}` come out as `nil, nil` — *"no events, all fine"* — when before the task it came out with an error.
It became `return 1`, with the reason in the code; (8) **removing the `waba == ""` from guard 5b passed GREEN**, and
that is the finding that counts.

***Why mutation (8) passed green, and what that taught:*** the test instance has `waba_id = "WABA1"`, so
`"" != "WABA1"` refuses anyway. The real defence was not the `==""`, it was *"every instance has `waba_id`
filled in"* — and that is true of **today's path**, not of the type: `config.Store.CreateInstance` validates
slug, `callback_url` and `bundle_ca`, and does **not** validate `waba_id` (true until T-074, which closed it —
see the following entry); the one that validates is `zapgw provisionar instancia`, which is only the FIRST
creation path (`CreateInstance`'s own comment raises this scenario: *"o próximo — um endpoint de administração,
um seed — nasceria sem eles"*). With `waba_id` empty on both sides, `"" != ""` is false and the guard passes
**silently**. The fix was not removing the `== ""`: it was writing the missing test
(`TestHandlerRecusaWabaIDIlegivelAindaQueAInstanciaNaoTenhaWabaID`), which creates the instance through the store
with an empty `waba_id` and goes red under the mutation. **It is the third time "a mutation passed green" yields
more than "a mutation passed red" in this file** (T-047, order × signature; T-054, counting before × `Registrar`'s
signature; this one, comparison × the invariant of whoever creates the data) — and all three have the same shape:
*the guarantee you think is in the code is, in fact, in the path that feeds the code.*

*Cost in production: **unknown by construction**, for the third report running — an ABSENT result, not a wrong one.
It deserves a qualifier the previous entries did not have: this defect is **rarer and more expensive** than the
message one. Rarer because it requires Meta to change the type of a STRUCTURAL field (`contacts`, `metadata`,
`entry.id`), not a content one; more expensive because when it happened it would take a customer's whole batch, not
one message. It lived from plan 1 (2026-07-23) until 2026-07-28.*

**And the closing of that green mutation (T-074), with the finding that only showed up on IMPLEMENTING: the
defensive way out the task offered had nothing left to defend.** T-068 left the hole described like this — *"an
empty `waba_id` on both sides makes `"" != ""` false, and the guard passes **silently**"* — and T-074 offered two
ways out: **(a)** `CreateInstance` validating `waba_id` and `phone_number_id`, or **(b)** guard 5b treating an
empty `waba_id` **on the instance** as *"I cannot check"* and refusing. (b) looks the more defensive of the two,
and is the one the defect description pulls towards. **It was already implemented:** the explicit `waba == ""`
that T-068 itself wrote (`internal/inbound/handler.go`) refuses the unreadable account webhook, and a readable
`waba_id` from another WABA differs from `""` — so, with the instance having no `waba_id`, **no** account webhook
passes. Measured before deciding, with a throwaway test (an instance with an emptied `waba_id` +
`payloadDeContaDeTemplate("WABA999")`): `entregou = false`, `200`, and the alarm
`... da waba_id "WABA999", que nao e a dela ("")`. Choosing (b) would have produced a comment asserting a new
protection over behaviour that already existed — the "defence that only looks like a defence" this file already
records two sections above, now in its most expensive form, that of someone who *believes they fixed it*.

*The residual defect, then, is not what was written: an instance without `waba_id`/`phone_number_id` does not leak,
it is born **DEAD**. 5a refuses every message/status with a non-empty `phone_number_id`, 5b refuses every account
webhook, both answering `200` to Meta (which therefore never resends) and leaving only a journal line — and this
file already records that nobody reads the journal out of habit. An instance that silently refuses everything is
cheap to catch in exactly one place: **at creation**. Hence (a): `config.ValidateIdentification`
(`internal/config/store.go`), called by `CreateInstanceAt` alongside
`ValidateSlug`/`ValidateCallbackURL`/`ValidateCABundle` — validating three of the five fields and not the other two
was this project's mother pitfall inside a single function.*

**Checked BEFORE deciding, because the task ordered checking and because (a) would break working creation if the
answer were otherwise:** there is no legitimate path with those fields empty. `zapgw provisionar instancia`
(`cmd/zapgw/provisionar.go`) already requires both flags — including for a **send-only** instance, where what is
optional is the `callback_url` and not the identification, and including for the **laboratory** one, which since
T-071 is born from the same command. The whole suite agreed: the new validation brought down **one** test, and it
was precisely T-068's.

> ⚠️ **The paragraph above describes 2026-07-28 and stopped holding on the SAME day: T-079 made `--waba-id` and
> `--phone-number-id` OPTIONAL at creation**, because in the decided model (`docs/MODELO-DE-USO.md`) those values
> are the consumer's and the owner does not have them. *The requirement did not disappear — it moved:*
> `config.ValidateIdentification` still exists and is now called by `RegisterMeta` (`POST /v1/cadastro`), and
> T-074's test was **moved**, not removed (`TestRegisterMetaRefusesEmptyIdentification`). What T-074 prevented — an
> instance that silently refuses everything — remains impossible by another route: an instance without registration
> is born and stays **PAUSED** (answering 503, which is the state it actually has) and only `zapgw fumaca`
> activates it, requiring a send that really worked. *The methodological lesson survives intact: the mutation that
> passed green in T-068 is still the reason the validation exists.*

*Cost: **zero in production** — no instance was ever born without a `waba_id`, because the only creation path
always required it. What was paid was process, and it is the usual pair: the guarantee lived in the CALLER (T-068
found that), and the description of the hole aged **between the task being written and being done** — a few hours,
because the one who closed the leak was the previous task itself.*

**The question that generalizes, and it is about the task's Why, not about the code: the text that justifies a task
is DOC, and the same rule applies — check it against the code, never against the description.** Before choosing the
"more defensive" way out a task offers, **measure whether it still has anything to defend**; if the behaviour it
promises is already today's, the right choice is the other one, and the cost of not measuring is a commit that
looks like a fix and changes nothing.

*Two mutations, done and reverted before the commit:* (1) removing the `ValidateIdentification` call from
`CreateInstanceAt` leaves `TestCriarInstanciaRecusaIdentificacaoVazia` (`internal/config/store_test.go`) red in all
**five** cases — the two fields absent, the two with whitespace only, and the two together; (2) removing the
`waba == ""` from guard 5b **still** leaves `TestHandlerRecusaWabaIDIlegivelAindaQueAInstanciaNaoTenhaWabaID` red,
and that was the proof that mattered: that test **had to change shape** (the `waba_id` is now emptied by a direct
`UPDATE`, because the store stopped accepting creation that way) and T-068's net had to survive the change. *Why
the test still exists after (a): validation at creation does not reach a row already in the database — a database
written by an earlier binary, a hand-written `UPDATE`. One prevents the instance from BEING BORN incomplete; the
other prevents the handler from TRUSTING one that already is.*

**Two `json:"sameName"` tags in the SAME struct produce neither a compile error nor a runtime error — `encoding/json`
silently ignores BOTH fields, in `Marshal` as well as in `Unmarshal`.** T-044 (a template button discriminated by
`tipo`) started from a request written with the new field literally called `botoes` — the same as the example Meta
itself uses. Except that `Pedido.Buttons` (`internal/outbound/mensagem.go`) had used the tag `json:"botoes"` since
before, for something entirely different: the body of `"tipo": "botoes"` (an ordinary interactive message,
`{id,titulo}`, WITHOUT a template). The two features have nothing in common beyond the name. Confirmed by
experiment before writing any code (`json.Marshal`/`Unmarshal` on a test struct with two `json:"botoes"` fields):
no error in either call — both fields come out/stay empty, as if neither existed. Had the new field gone in under
that name, a consumer sending `"botoes"` for a TEMPLATE request would get `200` with the button block **absent**,
and a consumer sending `"botoes"` for an ordinary interactive message would lose the whole message — both at once,
and neither with any error signal.
*Cost: zero — found by consumer `consumer-b`, reading the contract BEFORE planning on top of it, and not by review
here. Fix: the new field is called `botoes_template` (`Pedido.TemplateButtons`), leaving `Pedido.Buttons`
untouched; and `Validar()` gained a NAMED guard in both directions — `botoes` in a `tipo:"template"` request and
`botoes_template` in a `tipo:"botoes"` request are both `ErrFieldForbidden` citing the right field name — so that
confusing the two similar names produces an error pointing where to go, instead of a silent discard. Proven by
`TestValidateRefusesInteractiveButtonsInTemplateWithAnErrorPointingAtTemplateButtons` and
`TestValidateRefusesTemplateButtonsInTheInteractiveButtonsType` (`internal/outbound/mensagem_test.go`).*
**The question that generalizes, and it is a sibling — not the same — of this file's mother pitfall: that question,
"where else should this rule hold, and does it?", uncovers a name that is missing in a second place; this one
uncovers a name that already exists in a different FIRST place. The same name for two different things is worse
than two different names for the same thing** — the second one the contract reader notices (different names invite
the question "why?"); the first goes unnoticed until `encoding/json`, or worse, until production, decides on its
own which field exists. Before naming a new field in any shared struct, `grep` for the JSON tag you are about to
write — not just for the Go field name.

**A `nil` slice becomes `null` in JSON, NOT `[]` — and this project's contract promised `[]` in writing, in ten
places, for the whole life of the envelope.** `Envelope.Events` has no `omitempty`
(`internal/inbound/deliver.go:45`), so the field always comes out; and `ParseWebhook` returns `var evs []Evento`
**with no `append` at all** when there is nothing to enrich (`internal/meta/parse.go:479`) — an unmodelled account
webhook, a body without `messages`/`statuses`, a parse that failed entirely. The handler passed that slice on
without normalizing (`internal/inbound/handler.go:194`). Result on the wire, from 2026-07-23 to 2026-07-28:
`{"…","eventos":null,"parse_error":""}` — **today the envelope normalizes to `[]`, see the fix at the end of this
entry.** `docs/CONTRATO-CONSUMIDOR.md`, `docs/META-CAMPOS-DE-WEBHOOK.md`, the changelog and the tasks all said
**`eventos: []`**.

**Why that is expensive false doc and not a cosmetic detail:** a consumer following the contract to the letter
writes `for ev in envelope["eventos"]`, which in Python blows up with `TypeError: 'NoneType' object is not
iterable`; and a guard `if eventos == []` **never matches**. Both break exactly in the case the text existed to
describe — the batch with no event — and not on the happy path every test exercises.
*(In the same envelope, `parse_error` is also neither `null` nor absent: it is `""`, from the same missing
`omitempty` at `:46`. The contract example showed `null`.)*

*Cost: unknown, and the way the test walked past it is what is frightening.
`TestHandlerDeliversTheRawEvenWithTheParseFailing` (`internal/inbound/handler_test.go`) **produces that exact
envelope** — it sends `null` as the body, the parse fails, the delivery happens — and **reads the serialized body
at the consumer**. Except that its only assertion about it is `strings.Contains(…, "parse_error")`: the test had
`"eventos":null` in its hand and never looked. On the day of the finding, `grep -rn '"eventos"' internal/` returned
**one line, the struct tag** — no assertion, in any test, about the shape of that field in the JSON (today it also
returns T-067's test lines). And no consumer reported it. Both consumers are in production receiving unmodelled
account webhooks since 2026-07-28 (the App stayed subscribed to ten fields and the gateway models one), so the case
occurs routinely today. Found on 2026-07-28 while writing T-056, checking the format against the code instead of
against the contract — and proven with an executed `json.Marshal`, not from memory of `encoding/json`. Fix in TWO
stages, and the order between them is the part worth copying: first the **documentation** started saying `null` and
recommending `envelope.get("eventos") or []` (T-056), and only **afterwards** did the wire change (T-067).*

***The order "warn, wait for the defence, then change" is not ceremony — it is what turned a breakage into a
non-event.*** The wire change (`[]` in place of `null`) **fixes** whoever read the old doc and **would break**
whoever had branched on the `null` the wire actually sent. Both consumers were warned in writing on the channels on
2026-07-28 15:12, with the explicit instruction to defend themselves FIRST; both answered the same day confirming
the defence in their code. One of them wrote the sentence that sums up the gain: *"o `null` não me pegou, mas eu
estava com sorte — não havia teste, e a simplificação óbvia teria quebrado. O aviso na ordem certa transformou
acidente em decisão."* ("The `null` did not catch me, but I was lucky — there was no test, and the obvious
simplification would have broken. The warning in the right order turned an accident into a decision.") Reversing the
order would have cost exactly the opposite, and for free.

**The rule that comes out of here, and it decides on its own whether an empty field needs fixing: the field whose
empty value is already FALSY does not; the field whose empty value is `null` in a type that gets ITERATED does.**
In the SAME envelope, `parse_error` also has no `omitempty` and also always comes out — but it comes out as `""`,
which is false in Python, JS, Go and in any language a consumer will use, and nobody iterates over an error string.
It is RIGHT as it is and was not touched in T-067. `eventos` came out as `null` in a type every consumer walks, and
`null` is not a list anywhere. Both fields came from the same oversight (no `omitempty`, no normalization), and only
one was a defect. *The question, before "fixing" an empty field: **is its empty value already false in the language
of whoever reads it, and does anybody ITERATE over it?** If the empty value is falsy, touching it is churn that on
top of everything lands in "Mudanças que quebram" for nothing.*

*Fix (T-067): `if evs == nil { evs = []meta.Evento{} }` in the envelope assembly (`internal/inbound/deliver.go:257`),
**in one place only** — normalizing in each caller would be enumerating call sites, which is this file's mother
pitfall, and the next delivery path would be born sending `null` again.* **Mandatory mutation, done and reverted
before the commit, and it measured more than it proved:** removing the normalization leaves **one single test in the
whole suite** red — `TestEnvelopeWithNoEventGoesOutAsEmptyArrayOnTheWireNeverNull`
(`internal/inbound/deliver_test.go`), citing `"eventos":null` in the message — and **nothing else blinks**. That is:
five days after the defect was known and documented, the whole suite was still incapable of seeing it, because **no
other test looks at the bytes on the wire**. It is the executable proof of the question that closes this entry.

**The question that generalizes, and it is a sibling of "does the code VERIFY or STORE this datum?" (section
*TLS*): was the contract's example COPIED from the code, or written by hand from the struct?** A Go struct does not
show `nil` versus empty, nor `omitempty` versus always-present — only an executed `json.Marshal` shows that. **A
format example nobody serialized is a guess wearing the costume of a reference**, which is the same lesson as the
entry *"A doc example is code nobody runs"* (section *Documentation*), one level earlier: there the VALUE
was wrong, here the TYPE.

**`encoding/json` without a tag matches by EXACT name or by a case-only difference — never `snake_case` →
`CamelCase`.** When writing `meta.InstagramAccount` (T-109, the Instagram diagnostic) the field `AccountType string`
was born WITHOUT `json:"account_type"`, counting (without checking) on the package doing the conversion it does for
`id`→`ID` and `username`→`Username` — those two match by pure case-insensitivity, and masked the fact that the third
field did not. The `Unmarshal` returned no error at all: `AccountType` was silently left `""`, and the command
printed "tipo (não informado)" even with Meta answering `"account_type":"BUSINESS"` in the body.
*Cost: zero — caught by the test written BEFORE the commit
(`TestDiagnosticInstagramHealthyInstanceAnswersEveryQuestion`, `cmd/zapgw/diagnostico_test.go`), which asserts the
exact text `"tipo BUSINESS"` in the output instead of merely checking that the word "tipo" appeared. A test that
only checked "question 1 answered something" would have passed just the same with the field empty.* **The question
that generalizes: does every struct that decodes JSON from Meta (or from any third-party API) have an explicit tag
on EVERY field, even when the name "looks like" it matches** — matching by accident today (two fields out of three)
is worse than never matching, because it hides the third behind the two that work.

---

## Errors and logging

**A transport error in Go carries the full URL — host, path and query string.** `*url.Error.Error()` includes the
request's URL. Interpolating the error with `%v` into a message that goes to the log **leaks the `callback_url`**,
which this project encrypts at rest precisely so that a stolen backup does not reveal the consumers' topology.
Proven with a callback containing `?token=SEGREDO`: the whole token showed up in `Motivo`.
*Cost: **Critical no. 2**. Fix: classify the error (`errors.Is` against sentinels), never interpolate it.*

**Encrypting a datum at rest and printing it in the log makes the encryption decorative.** When you decide a field
is a secret, sweep **every** place it can come out: database, log, error message, header, HTTP response.

**"The error message names the field, never the value" was a rule of the PACKAGE, not of EVERY message — and it
only became dangerous when somebody tried to log it.** T-037 (logging the reason for a `POST /v1/messages`,
`/v1/media` and `/v1/templates` refusal) started from the premise written in the handler itself: `Validar()`'s error
"names the field, never the value" — and therefore logging `err.Error()` would be safe. Reviewing
`internal/outbound/mensagem.go` field by field (not trusting the sentence), THREE exceptions showed up that
deliberately cite the refused value, with `%q`, to guide the consumer: `ErrUnknownType` (echoes `p.Type`),
`ErrUnknownCategory` (echoes `p.Categoria`) and `ErrUnknownHeaderType` (echoes `c.Cabecalho.Type`). None of the
three is a bug in the HTTP RESPONSE — the very consumer who sent the value is reading it back — but all three would
be a leak if they went into the GATEWAY'S LOG: a free-text field (`tipo`) would become a channel for writing, into
the gateway's journal, any string a consumer (malicious or merely broken) decided to put there.
*Cost: zero — found BEFORE the commit, by checking each `Validar()` message against the code (this project's
doctrine), not by assuming the comment's sentence held for all 39 messages at once. Fix:
`mensagemDeRecusaSegura(err)` in `mensagem.go` swaps the three for fixed text before logging; the HTTP response to
the consumer keeps using raw `err.Error()`, with no change at all. Proven by
`TestHandlerLogsUnknownTypeWithoutLeakingTheRefusedValue` (`internal/outbound/handler_test.go`), which sends a
sentinel `tipo` and requires it to appear in the RESPONSE and not appear in the LOG.* **The question that
generalizes, sibling of "where else should this sentence be true?": does a rule written to explain ONE concrete case
hold for the OTHER 38 cases of the same family, or does it only look like it does because nobody had a reason to test
the others?**

**"It errored" and "it did not happen" are different questions, and treating them the same duplicates messages.**
Sending released the idempotency key on **any** error from Meta. But a transport error, an exhausted deadline and a
`2xx` without an id do not prove the message was not created — they only prove that we do not know. A released key
turns the consumer's legitimate retry into a **second** message on a real customer's phone. Only a **known-negative**
outcome (Meta answered with an error status, or the request never left here) gives the key back; an unknown one
retains it and answers with the class `desconhecido` (`502`), not `retentavel` — calling `retentavel` a case that
will produce a `409` tells the consumer to do the opposite of the right thing.
*Cost: caught in plan 2's final review, before going to production. Fix: `errors.As` against `*meta.ErroMeta`.*
**The exact boundary is a choice, not a fact — and the doc cannot pretend it is a fact.** A `5xx` from Meta **also**
does not prove the message was not created; even so it **releases** the key. It is not incoherence: retaining on
`5xx` would turn a Meta instability into a whole batch of untransmittable messages for 72h — **certain** damage,
proportional to the size of the incident — against the **uncertain and unitary** risk of a duplicate. The contract's
first wording asserted, as fact, that "Meta's `502`/`503` mean the message was not created"; that is an assertion
about an external service with no source, which this file forbids (see *Documentation*, below). The text was swapped
for "Meta answered, even if with an error" — true and verifiable on our side.

**An idempotency key tied only to the consumer swallows messages in silence.** The contract recommends using the
entity's id as the key — and the same entity sends several messages (reminder, invoice, apology). Without comparing
the **request**, the second one got a `200` with the first one's `wa_message_id`: the consumer recorded "sent" and
the message never went out. *Fix: a hash of the **already normalized** request in the table, and `422` when it
differs. Hashing before normalizing would be worse than not having the guard — `" 5511…"` and `"5511…"` would
collide as different requests and every legitimate retry would become a false `422`.*

**A fix that ADDS a case to the log can ERASE the case that mattered — and its own comment described the defect
while recreating it.** The inbound verdict log alarmed when `v.Alarm`. A plan-1 fix wanted to add logging of the
non-2xx cases and swapped the condition for `status < 200 || status >= 300`. But `Alarme` is set **exactly** on the
branches that answer `200` to Meta: the two conditions are mutually exclusive, and the `ALARME` prefix became **dead
code**. That is: the only warning of definitive loss — Meta never resends after a `200` — was switched off for three
weeks.

The fix's own comment said that the log *"only appeared when `v.Alarm`, that is, never in the case where you would
need it"*. It diagnosed the problem precisely and inverted it instead of solving it. **Replacing a condition is not
adding a case**: when the goal is "start logging X as well", the safe form is `oldCondition || X`, and the test has
to require the OLD case to keep coming out.
*Cost: one Critical found by execution (`log.SetOutput` into a buffer), not by reading — three reviews read that
block and none saw it. Fix: `v.Alarme || outside-2xx`, with a test requiring the prefix on a consumer `4xx` and its
absence on a `5xx`.*

**Two live definitions for the same alarm prefix train the operator to ignore it.** Inbound defined `ALARME` =
definitive loss; outbound already alarmed on an unreleased idempotency key, where nothing is lost. Neither was wrong
on its own — the pair was. *Fix: the criterion became **"needs a person"**, and definitive loss became one case of
it. Every `ALARME` now says what the person needs to DO, not just what happened.*

**A justification that is RIGHT for the isolated event becomes an excuse for the repetition — and the comment that
writes it freezes the hole.** The refusal for a body above the ceiling (`413`) logged without `ALARME`, with the
justification *"Meta resends by itself within the 36h window — nobody needs to act now because of this isolated
event"*. The sentence is true for **one** event and false for the second: the resend brings the **same** body, blows
the **same** ceiling, and when the window expires the message is definitively lost — this project's most expensive
outcome — **with no signal at all**. Nothing on that branch fixes itself; it only looked like it did, because the
comparison was made with the neighbouring case (the consumer's `5xx`), where the resend really does resolve it.
**And BOTH extremes were wrong, which is why the correction is a threshold and not an `if`.** Plan 1 wrote that line
alarming on **every** rejection (`docs/superpowers/plans/2026-07-23-fundacao-e-inbound.md:2846`); the implementation
removed the prefix — and rightly, because an alarm per event becomes noise — but swapped the noise for **silence**,
which is the expensive failure. The middle ground is counting: no alarm below the threshold, one only per window
above it.
*Cost: none in production — the gateway does not receive real traffic yet. The hole existed since the inbound's
first commit (`677ccd0`, 2026-07-23) and no plan-1 review caught it: all of them read the justification and agreed
with it, because it is right for the case it describes. Fixed in T-002: an in-memory per-instance counter (3
refusals in 1h → one `ALARME` with the action). **The question that would have caught it: "is this justification
still true the SECOND time?"** — and it is a sibling of this file's mother pitfall, which asks "where else should
this sentence be true?".*

**An I/O error became "body too large" because the code only looked at the fact that there was an error.** `ReadRaw`
returns two different errors; the handler mapped both to `413 permanente`. A connection that dropped mid-upload
became "shrink the body" (which was perfect) + "do not try again" (when trying again was the right thing). *Fix:
`errors.Is(err, httpx.ErrCorpoGrande)`, and the rest becomes `400` `retentavel`.*

**A method that CAN return an error invites somebody, one day, to treat that error as fatal — the strongest defence
is the SIGNATURE, not the caller's discipline.** T-035 (instance counters) has a hard rule: counting is monitoring,
it can never bring down the response already written to Meta or to the consumer. The obvious temptation was to write
`Registrar(slug, chave) error` and document "ignore the error, just log it" — but that would leave the guarantee in
the head of whoever writes each `handler.go`, and the first person to move the call to BEFORE the `w.WriteHeader` (a
plausible refactor: "let us count as soon as we know the outcome") would have, right next to it, the pattern that
dominates the rest of this project: `if err != nil { http.Error(w, …); return }`. `Contador.Record` has NO return at
all: the error is read, logged and discarded inside the method itself, and there is no path for the caller to
propagate what it never received. *Cost: zero — the proof mutation (moving the call to before the `WriteHeader` AND
swapping `Registrar` for a variant that returns an error, `RegistrarComErroTEMPORARIO`, done and reverted before the
commit) left `TestHandlerCounterFailureDoesNotChangeTheStatus` red with a `500` in place of the real verdict — proof
that, with an "error comes back" signature, the bug was one refactor away.* **The question that generalizes, sibling
of the entry about the unnecessary pointer (section "Go / JSON"): if the right answer to an error is always "ignore
it", the method's signature can guarantee that — why leave it to the caller's discipline?**

**A monitor that compares a response has to prove FIRST that there was a response — otherwise a network outage
becomes the gravest alarm it knows how to raise.** On 2026-07-29, 13:39, the App subscription monitor shouted
`ALARME INSCRICAO: a Callback URL do App MUDOU` **carrying, in the alarm text itself, the proof that it had not asked
anything**: `curl: (28) Failed to connect to graph.facebook.com`. The logic was "does the response contain our URL?
if not, alarm" — and a `curl` error string also does not contain our URL. **It does not distinguish *I could not
ask* from *the response changed*, and treats both as the worst case.** *Cost this time: one investigation. The cost
it charges if it recurs is different and compound — **this is the alarm that, if real, means all the tenants'
traffic has been diverted**, and it is precisely the one that can least afford to cry wolf. An alarm that lies is an
alarm people learn to ignore, and this is the last one anybody should learn to ignore.* **The right form:** require
a `200` **and** readable JSON before comparing; without that, the event is *"the monitor is blind"* — a different
message, a different urgency. *Sibling of the `grep`-in-a-pipeline rule (section "Environment"): in both cases the
verdict was read from a place that **also** answers the same thing when the previous step did not even happen.*

> **And the diagnostic lesson, which holds beyond monitors:** the false positive was closed with **positive
> proof**, not with "the alarm looks silly". The question that resolved it was not *"did the URL change?"* (which
> would require the `app_secret`, and it does not even live on the gateway's machine) but *"is the webhook still
> arriving?"* — `tenant-two`'s `recebidas` at 53 for the day and the last one 15 min before the alarm. **Traffic
> arriving proves the Callback URL is ours**, and it is a proof you can read with no secret at all. When the direct
> question is expensive, look for the fact that is only possible if the answer is the one you expect.

---

## TLS

**An `*http.Client` received from outside is the door through which the escape hatch enters — and it makes no noise
at all as it opens.** `NewDeliverer` received the client as a parameter (so that tests could use `srv.Client()`), and
with that *any* caller could hand over a `tls.Config` with no verification, just once, to unblock a demo. Switching
verification off **generates no error**: it merely removes a protection, silently, and the `https` requirement on
the `callback_url` becomes theatre — the scheme still says `https` and there is no guarantee behind it any more.
*Cost: none yet — the gateway delivers to no consumer today (the production instance has an empty `callback_url`).
Fixed in T-013: the deliverer builds its own client, one per trust anchor, and the parameter is gone. The defence is
for the option **not to exist**, plus two tests that go red if it comes back: a behavioural one (a server with a
self-signed cert must FAIL) and a sweep of every `.go` in the repo for `InsecureSkipVerify` — the sweep also covers
the `gateway → Graph API` direction, which no delivery test reaches.*

**A certificate failure is NOT "the consumer is down", and treating the two the same erases the only warning.** Both
give a transport error and both answer `504`. The difference is that the outage **fixes itself** (the consumer comes
back, Meta resends within the window) and the certificate **does not**: every resend redoes the same handshake and
takes the same rejection, until Meta gives up — and then the loss is definitive, with nobody having been warned. That
is why TLS alarms and an outage does not.
*And that is why the TLS alarm **has no threshold**, unlike the handler's `413`: there the isolated event still had a
chance on its own and only repetition became a loss; here the FIRST occurrence already needs a person. Delaying the
warning would trade noise for silence in the one case where silence costs a message.*

**"The gateway already verifies the certificate, so the date is in hand" — the first half was true and the second was
not, and both were written in the same sentence.** It is the text with which the planner opened T-060 (2026-07-28).
`internal/inbound/deliver.go` builds the `tls.Config` and verification has been strict since T-013 — but **verifying
is a question that `crypto/tls` answers with a yes/no and throws the rest away**: nobody had ever read
`resp.TLS.PeerCertificates`, and no date existed anywhere in the project. The one who caught it was the **T-060
implementer**, who went to check before implementing, did not implement what did not exist and — the step that
matters — **wrote nothing about it into the contract**: a doc that promises a non-existent mechanism is the error
this project hunts.
*Cost: zero, and only just. The sentence was inside a TASK, which is the genre of text nobody treats as suspect — a
spec and an old doc we distrust; a work order we execute. Closed in T-064: delivery started capturing the leaf
certificate's `NotAfter` on the connection that already existed (`observeCertificate`), with the moment of
observation alongside.*
**The question that generalizes, and it holds for every guarantee in this project: does the code VERIFY this datum or
does it STORE this datum? They are different things, and the first does not imply the second** — verification
consumes the information and discards it, and whoever reads "this is verified" naturally concludes that somebody,
somewhere, kept it.

**Classifying a TLS error by a substring of the message is a false negative waiting for update day.** The error text
of `crypto/tls` and `x509` changes between Go versions and between operating systems — on Windows verification goes
through the platform verifier. The question is asked of the **type** (`errors.As` against
`*tls.CertificateVerificationError` and the three `x509` errors), and the proof that the taxonomy matches reality is a
test that classifies the error of a **real handshake**, never a synthetic error assembled by hand.

## Signature

**A value that travels OUTSIDE the signature is not protected by it — and naming it "anti-replay" makes everybody
assume it is.** The `X-Zapgw-Signature` covered only the body; the `X-Zapgw-Timestamp` went in a header next to it,
and the contract told the consumer to "reject a timestamp outside a tolerance window". The window protected nothing:
whoever captured a delivery resent it with a new timestamp and the signature still closed, because the signature had
never seen any timestamp. The outcome is this project's worst shape — the consumer implements the check, marks the
item resolved, and stays exposed **believing they are not**.
*Cost: none in production, and only just. The one who caught it was the **consumer** (`consumer-a`, 2026-07-26),
reading the contract to choose the size of the tolerance — they had not implemented the verification yet, and that
is the only reason the correction fitted in a commit instead of in a breakage negotiation. The hole existed since
the inbound's first commit; no plan-1 review saw it, because they all read the header and its name already answered
the question. Fixed in T-022: the signature now covers `timestamp + "." + body`.*
**The question that would have caught it, and it holds for any new header: is this field INSIDE the computation?**
If the answer is no, it is informative — and the doc has to say so with that word.

**Signing a value the code computes TWICE is an intermittent failure, and no sequential test catches it.**
`Entregar` read `e.agora()` once for the `recebido_em` and again for the timestamp header. While the timestamp was
decoration that cost nothing; the moment it entered the signature, the two reads started to diverge whenever the
second rolled over in between — one delivery in every N refused by the consumer as an "invalid signature", with
nothing flagging it at the gateway, unreproducible. *Cost: zero, because the same task that created the risk closed
it. The guard is a test clock that ADVANCES one second on every read
(`TestDeliverSignsTheSAMEInstantThatGoesInTheHeader`): that way any path that reads twice goes red
deterministically, instead of once every thousand runs. **A signed value is computed ONCE and passed along.***

**Concatenating two fields for signing without a separator creates two pairs with the same signature.**
`("1769000000", "0x")` and `("17690000000", "x")` produce the same bytes. Here it would not be exploitable — the
body is always a JSON object and therefore starts with `{`, never with a digit — but that defence lives in **another
file** (the `Envelope`'s `json.Marshal`), and whoever reimplements the computation in Python or TypeScript reading
only the contract does not know it exists. *The separator (`.`) makes the boundary unambiguous by construction;
without a test requiring it, the first "simplification" removes it — the mutation that erases it passed green before
`TestSignatureSeparatesTimestampFromBodyWithoutAmbiguity` existed.*

---

## Meta / WhatsApp Cloud API

🔴 **ACTIVATING an INSTAGRAM instance has a chicken-and-egg dead end, and it does not exist on WhatsApp.** Found on
2026-07-30 by reading the code, **before** anybody hit it — the first real activation had not been attempted yet.
The three pieces, each correct on its own:

1. an instance is born **paused**, and while it is, the webhook answers `503` **before reading the body** (the
   `Ativo` guard comes before the `httpx.ReadRaw`, in `internal/inbound/handler.go`);
2. the smoke test — the only path to `ativo = 1` — requires an **IGSID that has written in the last 24 h**, because
   Meta only accepts **free text** inside the window and on Instagram **there is no template** to open a
   conversation;
3. the IGSID **only appears inside the webhook body**, which piece 1 refuses without reading.

**Without the IGSID it does not activate; without activating you do not read the IGSID.** The DM is not lost (Meta
resends for 36 h), but the value needed to unblock reaches nobody.
**Today's way out is manual and from outside the gateway:** take the IGSID from the professional account's
panel/Graph API and pass it to the smoke test. A command that reads recent conversations would resolve it for good —
**it is proposed and not decided** (2026-07-30); while it does not exist, whoever activates Instagram needs to know
this beforehand, otherwise they spend one round finding out.
*Why the analogy with WhatsApp misleads: there the smoke test sends to a phone number, which a person knows by
heart. The Instagram identifier is **opaque and App-scoped** — nobody "knows" it, it only arrives through the
webhook. **Every time a new surface swaps the identifier for an opaque one, every step that depended on a person
knowing the value has to be re-examined.***

**A consumer's request describes the USE CASE precisely and the PROTOCOL from memory — check the verb, the path and
the body against the source, even when two consumers agree.** In asking for the read receipt (T-075), one of them
cited `PUT /{phone-number-id}/messages`; Meta's official doc, read on 2026-07-28 on **both** pages that describe the
call (`developers.facebook.com/docs/whatsapp/cloud-api/guides/mark-message-as-read` and
`developers.facebook.com/documentation/business-messaging/whatsapp/messages/mark-message-as-read`), says **`POST`**
— on the same path as sending. Both consumers described the **same body**
(`{"messaging_product":"whatsapp","status":"read","message_id":"wamid…"}`), and **only the verb diverged**: their
agreement about the body is precisely what would make somebody accept the verb along with it, in the package.
*Cost: **it did not charge** — the divergence was seen while writing the task and resolved at the source before any
code existed. What it would have charged: this project's suite **does not talk to Meta** (CLAUDE.md, "O que o verify
NÃO alcança"), so a `PUT` would pass green here and only fail against the real Graph API — in production, on cutover
day, with the symptom pointing at the gateway.*
**The guard that remained:** `TestReadsSendsPOSTOnTheSendPathWithTheMetaBody`
(`internal/outbound/leituras_handler_test.go`) asserts the verb, the path and the whole body against a fake Graph
API that **records what it received** — a double that only answered `200` would leave the correction with no guard
at all.

**And the same pitfall has a second form, quieter than the wrong verb: the field the consumer cites still EXISTS,
but is DEPRECATED.** In asking for the number's tier and quality (T-080), `consumer-b` cited
`GET /{waba-id}/phone_numbers` with the field **`messaging_limit_tier`** — which is what they read directly on the
Graph. Meta's doc, read on 2026-07-28
(`developers.facebook.com/documentation/business-messaging/whatsapp/messaging-limits`), says verbatim: *"The
`messaging_limit_tier` field, which used to return a business phone number's messaging limit, **has been
deprecated**. Request the `whatsapp_business_manager_messaging_limit` field instead."* The same page shows the right
call — `graph.facebook.com/v25.0/{phone-number-id}?fields=whatsapp_business_manager_messaging_limit` — and the
response `{"whatsapp_business_manager_messaging_limit": "TIER_250", ...}`.
*Cost: **it did not charge** — the check was required by the task and done before writing code. What it would have
charged is WORSE than the verb case: a deprecated field normally **still answers**, so there would be no error at
all on day 1. The gateway would be born with a dependency marked for death, and the failure would arrive months
later, with no visible connection to anything anybody had touched.*
**Why the consumer was not lying:** they were reading that field and it worked. *A deprecated field is exactly the
case in which the user's experience **does not contradict** the doc — and that is why the doc is the only source
that settles it.*
**The guard that remained:** `TestObserveNumberAsksForTheFieldsCheckedAgainstTheSource`
(`internal/meta/numero_test.go`) requires both current names in the URL **and goes red if `messaging_limit_tier`
shows up again** — the negative assertion is the half that matters, because the positive one alone would pass green
with both fields requested together.

🔴 **EDITING an APPROVED template takes it off the air for up to 24 h, and every send in that interval is refused
with `132001`.** Meta reviews it again after the edit, and while it reviews the template does not work — but the
gateway (and the consumer) only find that out from the send error, message by message.
*Real cost, measured at `consumer-b` on **13/07/2026**, before the migration to this gateway: **two customers were
left without a contract**, with the system saying "sent". Recorded here because the cost is theirs and the pitfall is
Meta's — it holds for any consumer of this gateway.*
**The right path is `_vN`:** `v1` stays live while `v2` is reviewed. Never edit what is in use.

⚖️ **DECISION, and it is not to be re-litigated: this gateway has NO route to edit and none to delete a template.**
It is not a gap, it is a choice — confirmed by `consumer-b`'s owner on 2026-07-28 22:08 (*"editar é um problema **no
sentido de não ter mesmo**"*), after he himself had said the opposite seven minutes earlier and corrected it.
Deleting burns the name at Meta; editing is the pitfall above.
*If somebody proposes the route, the answer is this line, not a fresh discussion.*

**Asking for a new field in `fields=` can BRING DOWN the whole read, and a 400 like that looks like "Meta refused
the credential".** The Graph API answers a field it does not know with **400 / `code` 100** (*"Tried accessing
nonexisting field"*), and not with a `200` without the field. In this gateway's taxonomy a 4xx is a **permanent
class**, which the watcher treats as a definitive outcome — that is, the day Meta retires
`whatsapp_business_manager_messaging_limit` (it has **already** retired its predecessor, above) it would paint
`token_meta.veredito = "recusado"` on **every active instance**, sending people looking for a revoked credential
nobody revoked.
*Cost: **it did not charge** — it was seen while designing T-080. What it would have charged: the most expensive
alarm on the dashboard firing for a reason that has nothing to do with what it asserts.*
**The defence that remained** (`internal/outbound/vigia.go`, `checkOne`): `recusado` **never** comes from a call with
`fields=`. Before declaring a refusal, the watcher reconfirms with the clean `GET`; if the clean one passes, the
credential is fine and what was refused was our field request — the verdict comes out `ok` and the defect becomes a
log line. Zero cost on the happy path. Guards:
`TestWatchdogDoesNOTRefuseTheTokenWhenGraphRefusesOnlyTheFields` and
`TestWatchdogKEEPSRefusingWhenTheTokenIsReallyRefused` (`internal/outbound/numero_test.go`), the second because
without it, deleting the credential check entirely would pass green. In the laboratory,
`grafo-falso --recusar-campos-do-numero` reproduces the outcome with the real binary.

**Marking a message as read also marks the PREVIOUS ones in that conversation** — *"When you mark a message as read,
the API also marks earlier messages in the conversation as read"* (both pages above, 2026-07-28). It is not a
cosmetic detail: `consumer-a` measured that **47% of blocks of consecutive inbound messages have more than one
message, and the largest had 13** — if Meta marked only the cited one, the common case would require thirteen calls.
**The right answer here did not come from analogy**: the same consumer observed that *"the WhatsApp I use as a person
behaves like this"* and discarded the observation themselves — **"the app seems to do this" is not a source**, and
the app and the Cloud API are different implementations of the same product. *What the source does NOT say, and
therefore the contract does not assert: how this behaves in a GROUP conversation. It speaks of "the conversation"
without distinguishing.*

**The `app_id` is NOT a secret on its own, and IS a secret PAIRED with the `app_secret`.** Together they form the
**app access token** (`app_id|app_secret`), and with it one administers the App's **webhook subscriptions** —
`POST /{app-id}/subscriptions`, that is, **where that number's WhatsApp delivers to**. The doc is explicit: *"É
necessário um token de acesso do app para adicionar novas assinaturas ao app"*
(`developers.facebook.com/docs/graph-api/reference/application/subscriptions/`).
*Real cost, 2026-07-28: in a leak incident, the consumer classified the `app_id` as "not a secret" — true in
isolation — and concluded that the `app_secret`'s exposure "did not worsen the current state", because today it is
Evolution that receives and it does not check the signature. **The reasoning looked only at the use of the value IN
THE GATEWAY and forgot its use AT META.** The real severity was one level up: whoever had the pair could repoint the
App's Callback URL and divert all the traffic, today, regardless of who checks signatures. The risk was formally
"accepted" before somebody contested it.*
**The question that catches this class:** *"does this identifier complete some credential when combined with another
value that also leaked?"* Classifying value by value is what produces the error.

**There is no programmatic rotation of the `app_secret`** — the doc says so in so many words: *"It is not possible to
programmatically rotate the app secret."* But **the panel field accepts a pasted value**, which in practice solves
it: generate the new value (32 hex), paste it at Meta, and **only afterwards** rotate the gateway to the same value.
*Cost avoided in the same incident: the consumer stated from memory that there was a "Regenerate" in the panel, the
owner corrected them with the screen in front of him, and the conclusion became "it cannot be rotated, risk
accepted". Nobody had tried **pasting**. Two wrong statements in a row — one from memory, the other from a partial
reading of the doc — nearly turned a ten-minute correction into permanent debt.*
**The order matters and is safe at both points:** while Meta still delivers to the old endpoint, the value can
diverge between the two sides without breaking anything. After the cutover, the same swap costs a window of
unavailability.

**A `200` to Meta is IRREVERSIBLE.** It resends for up to 36h if it does **not** get a 2xx, and stops **forever** if
it gets a 200. The rule is not "never return a 500" — it is: **answer 200 when a resend would not help, and non-2xx
when it would.**
*Cost: it has not charged **here** yet. It charged in another project of this network, where the policy's first
wording said "never 500" and **created the bug it existed to prevent**: a (transient) database failure returned 200,
and Meta never resent.*

**The 36h are NOT a safety net for a long outage.** They cover a restart of seconds. A consumer down for hours loses
events, and the gateway **does not even find out** — when the resends expire, Meta simply stops.

**Meta does NOT publish the webhook timeout, nor the retries, nor the intervals.** Any number in that place is ours,
not theirs. That is why the timeout is per-instance configurable and **measured**, never a magic constant.

**The same media has a DIFFERENT mime in the message payload and in `GET /{media_id}`** — `audio/ogg; codecs=opus`
against `audio/ogg`. It is the `codecs=opus` that makes WhatsApp render a **playable voice note**; resending with the
other delivers a **file attachment**, with no error at all. The parser reports the payload's mime **raw, with the
parameter** — normalizing would destroy exactly what needs preserving.
*Cost: it charged in production in another project of this network, in an audio relay.*
**The defence is structural in BOTH directions (T-016): `GET /v1/media/{id}` returns both mimes in separate headers
(`X-Zapgw-Mime-Do-Payload`, `X-Zapgw-Mime-Do-Get`) and the response's `Content-Type` is neutral** — putting one of
the two there would be the gateway choosing, and whoever read only the `Content-Type` would take the wrong choice
without ever seeing there were two. On upload (`internal/meta/media.go`, `UploadMedia`) the declared mime goes on the
wire **with the parameter**, and the category table reads only the **base** type to decide the ceiling — it never
rewrites the value that travels.
*The mutation that proves it: normalizing both to one; it goes red in two tests, one of them with the cost of
2026-07-20 written in the comment.*

**A reply to a TEMPLATE button arrives as `type: "button"`, with the payload in `message.button.payload` — NOT as
`interactive.button_reply`.** And only the template path works **outside** the 24h window, which is where an
eve-of-appointment reminder is sent.
*Cost: in another project of this network, an entire confirmation loop was built — with 11 green tests — on top of a
payload the system was **incapable of producing**.*

**A paginated Graph API list read without following `paging.next` returns a PLAUSIBLE, short list, and nothing flags
it.** This network's old gateway read only the first page of the template catalogue — **25** from an account with
**84**. There was no error, no odd status and no log: the consuming system concluded the template "does not exist"
and the message never went out. *Cost: it is what took that gateway out of production.*
The guard here is twofold and lives in `internal/meta/templates.go`: paginate until `paging.next` **disappears**, and
make the page ceiling **ERROR** when blown — returning the partial list "because we already have enough" is the same
pitfall with another number, now wearing the look of a deliberate decision. For the same reason, a malformed item in
the middle of a page is an **error**, never a skip: skipping is truncating under another name. *The question that
catches this whole family: if the result comes back smaller than it should, does anything go red?*

**The SAME pitfall, in the "bench tool" version: `len(data)` without looking at `paging.next` is not a count, it is a
page size — and printing it as if it were a total is false precision.** Measured in production on 2026-07-31, on the
first real run of `zapgw diagnostico` (v0.42.0) against the real Meta: `countInstagramConversations`
(`internal/meta/diagnostico_instagram.go`) built the `GET /me/conversations` query **without `limit`**, and the FIVE
calls (default inbox + four folders) returned exactly **25** — the Graph API's default page ceiling, not a
coincidence of real data. The label *"conversas na caixa padrão: 25"* read like a count; it was "at least 25, first
page". And worse: the folder sweep exists to distinguish "there is no DM" from "the DM landed in another drawer"
(`requests` carries DMs from people who do not follow the account, and the default inbox does not), and with the page
ceiling the four folders came out **identical** — a result indistinguishable from "Meta ignored the `folder`". T-112
swapped the comparison for `paging.next` **present/absent** (the same signal `templates.go` already uses), not for
`N == requested limit`: comparing against the requested limit would assume Meta always honours it to the letter,
which has not been verified with this source. *Cost: none yet — caught on the first human reading of the output,
before any decision had been taken on top of the wrong number. The question that catches this variant: does the
number the screen shows have a way for Meta to say "there is more", and does the screen LISTEN to that signal?*

🔴 **And the variant that cost most: a protection documented in THREE places was never exercised against the case it
promises to cover.** The sweep of the four extra folders (`other`, `page_done`, `spam`, `requests`) exists to
distinguish "there is no DM" from "the DM landed in another drawer" — and that intent was written, in those words, in
**three** different places: the `diag_instagram_meta.py` donated by `consumer-b`, T-109's comment when the command was
ported, and the comment of `InstagramMessagingPermission` itself (`internal/meta/diagnostico_instagram.go`). **None
of the three ever measured whether the `folder` parameter really filters anything.** Measured in production by T-113
(2026-07-31 15:31 -03, `v0.42.1`): the FIVE calls — default inbox + four folders — returned `≥ 50` in **all of them**,
even asking for `limit=100` (Meta also does not honour the requested limit on this endpoint). `spam` and `page_done`
with exactly the same total as the default inbox is implausible enough to raise suspicion, but **does not prove on its
own** that the parameter is ignored — the page ceiling (the pitfall just above) had already masked the same
difference once, with a smaller ceiling (25).
**The question that should have been asked from the start:** does a `folder` Meta NEVER documented
(`folder=zzz-nao-existe`) get an error or the same list? Only that probe separates "the parameter is ignored" from
"the page ceiling masks the difference" — and that is what T-113 added (`ProbeInvalidInstagramFolder`), without
concluding which of the two hypotheses holds, because **measuring** is different from **having three comments saying
it is already covered**.
*Real cost: four requests per diagnostic call, in production, from T-109 (2026-07-30) to T-113 (2026-07-31) — a whole
day spending network and printing five `[ok]` lines that the operator read as five independent measurements, without
any of them saying anything about which folder a DM is in.*
**What generalizes, and it is why this entry exists:** code that "handles" a case without ever having been exercised
against it is **indistinguishable** from code that handles it — until somebody measures. A convincing comment,
repeated in three files, looks exactly like a protection that works and like one that never worked. The only way to
know the difference is to hit the real case, and that did not happen in this project until the second measurement
against the real Meta (T-112 measured the ceiling; T-113 measured the parameter).
**The defence that remained:** `internal/meta/diagnostico_instagram.go` stopped asserting that the sweep works —
`MeasuredFolderResult` (today `FolderUnknown`) is the ONLY point that decides the behaviour, with the measurement
mechanism (`ProbeInvalidInstagramFolder`, switched on by `ZAPGW_DIAGNOSTICO_SONDAR_FOLDER` without needing a
recompile) ready for the answer that is still missing. Guards:
`TestInstagramMessagingPermissionSweepsTheFourFoldersWhenUnknown` and
`TestInstagramMessagingPermissionStopsSweepingWhenFolderIgnored`
(`internal/meta/diagnostico_instagram_test.go`) prove both sides of the `if` TODAY, before the real measurement
happens — without them, the switch would only be tested on the day somebody actually flipped it, too late to catch an
inverted `if`.

**And following `paging.next` blindly sends the token wherever the response says.** The `next` comes from Meta's
BODY, and the following request carries the instance's `Authorization`. The read refuses a page whose origin (scheme
+ host) is not that of the configured Graph API, and the refused URL does **not** go into the error message — it may
carry a credential in the query, and that text goes up to the log.
*Cost: none yet; proven by mutation in T-015 — without the guard, the client went off to fetch the strange host.*

**A `200` from Meta does NOT prove an id came back.** The same response can carry `{"messages": []}`, `{}` or an id
of the wrong type. Returning that as success writes an empty id at the consumer and the defect shows up a long way
from its origin.

**The `verify_token` is only used in the verification `GET`, never in the `POST`.** Changing the value without
re-registering the webhook at Meta **breaks no traffic at all**: everything runs normally for weeks, until somebody
re-saves the callback URL in the panel and the rejection shows up disconnected from the old change.

**Template status and account webhooks do NOT support webhook override** and always arrive at the main endpoint —
without `metadata.phone_number_id`. Confirmed at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/override/ (read on 2026-07-26): the page
lists the template fields
(`message_template_status_update`/`message_template_quality_update`/`message_template_components_update`/`template_category_update`)
and the account ones (`account_update`/`account_review_update`/`account_alerts`) as not supporting override, and says
Meta always delivers them to the *"app's default callback URL"*. Consequence for tenant isolation: a guard that only
compares `phone_number_id` **does not cover them** — that was exactly the gap T-038 closed (see the entry just below,
"An ACCOUNT webhook without `waba_id` routing retained another tenant's data").

**An ACCOUNT webhook without `waba_id` routing retained another tenant's data — it was not just wrong routing.** The
tenant isolation guard (`internal/inbound/handler.go`, step 5, since plan 1) compared only `phone_number_id`. An
account webhook does not have that field (entry above) — so the guard swept zero events, nothing was refused, and the
`cru` was delivered to the consumer of the PATH's slug even when it belonged to another WABA. The right framing, and
it came from the consumer (2026-07-26), not from this project: the consumer checks `envelope["instancia"]` against
the slug **configured on their side**, and the guard **passed** — because it is the gateway itself that puts the
path's slug into the envelope. Result: consumer A **writes to their database, definitively**, the raw body of an
event that could belong to tenant B. Wrong routing is fixed by repointing; data written into a third party's database
cannot be undone.

**The source for "entry[].id IS the WABA ID" was checked before routing by it, not assumed.** The task (T-038)
recommended routing by `waba_id`, but required confirming the source first — this project's `docs/ARMADILHAS.md`
already records five cases, in a single day, in which an unchecked assertion fell. Two official Meta pages, read on
2026-07-26, confirm it: the parameter table at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/ describes
`<WHATSAPP_BUSINESS_ACCOUNT_ID>` (the value of `entry[].id` in the example) as *"WhatsApp Business Account ID."*; the
example at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/message_template_status_update/
shows the same `"id"` field at the `entry` level, outside `changes`/`value`, with an identical description. The two
sources sufficed — path (a) of the task (route by `waba_id`) was confirmed, path (b) (discard the account webhook
without delivering) was not needed.

**The guard does NOT repoint delivery by the body's `waba_id` — it only validates the `waba_id` of the instance the
PATH already chose.** The obvious temptation, reading "route by waba_id", would be to use
`config.Store.InstanceByWabaID` (which already existed, with no caller, from before this task) to find out the
instance that OWNS that `waba_id` and deliver to IT — but that would break the invariant written at the top of the
file itself: *"a instância vem do CAMINHO, nunca do corpo"* (you cannot know whose the body is before trusting it,
and trusting requires already knowing which secret to use). The right fix is symmetric to the `phone_number_id` one,
which already existed: compare the body's `waba_id` against the `WabaID` **of the instance already resolved by the
path** — if it matches, deliver; if it does not, discard with `200` + `ALARME` + counter
(`config.CounterAccountDiscarded`), never re-route to the right instance. `InstanceByWabaID` still has no caller in
production; it exists today only for the test that proves the composite key
(`TestStoreFindsByPhoneNumberIDAndByWabaID`, `internal/config/store_test.go`).

**Enumerating the names of account fields (`message_template_status_update` etc.) in the guard would freeze the
defence in TODAY's vocabulary.** Meta may add a new account field without warning (the same generic pitfall, section
*Meta / WhatsApp Cloud API*, above) — if the guard checked a closed list of names, a future account field the list did
not cite would slip through, with no check at all. The guard in `internal/meta/parse.go` (`AccountWabaIDsInPayload`)
uses the opposite, structural criterion: `field != "messages"` — `"messages"` is the ONLY field this gateway knows how
to route by `phone_number_id`; anything else falls into the `waba_id` fallback, which is the only key every account
change carries, documented or not.

**Mandatory mutation, done and reverted before the commit:** swapping the comparison from `waba != inst.WabaID` to
`waba != inst.PhoneNumberID` in `internal/inbound/handler.go` leaves `TestHandlerRejectsAccountWebhookFromAnotherWaba`
AND `TestHandlerDeliversAccountWebhookWithMatchingWabaID` red (`internal/inbound/handler_test.go`) — the proof fixture
deliberately uses a `waba_id` EQUAL to the instance's `phone_number_id` (`"PNID1"`, not `"WABA1"`), exactly so that
comparing against the wrong field produces the wrong result, and does not pass by coincidence.
*Cost: zero in production — there is a single instance today, and the task (T-038) entered the queue precisely to
close this BEFORE the second tenant existed, not after a real leak.*

**A guard that answers `200` while DISCARDING is not a boundary between tenants — the boundary is the one that
answers `403`, and this project's doctrine called both by the same name for two plans.** Guards 5a
(`phone_number_id`) and 5b (`waba_id`), in `internal/inbound/handler.go`, had been described — here, in the contract
and in the tasks — as *"the tenant isolation guard"*. They are not that. They check whether the **addressing** of what
arrived matches what is registered on that path, and they answer `200` throwing the batch away. **What separates
tenants is the per-instance signature, at step 3 (`:183-189`), checked with THAT instance's `app_secret`, and it
answers `403` — nothing reaches step 4 or step 5 without passing through it** (exercised with traffic in T-042: the
same bytes, with the same signature, on the other instance's path → `403`).

**The difference is not semantic, and the corollary is what nobody deduces on their own: the guards go MUTE when two
Apps share a number and a WABA.** In that case the `phone_number_id` matches, the `waba_id` matches, no `ALARME` comes
out and **no counter moves** — `numero_descartado` and `conta_descartada` stay at zero because, as far as they are
concerned, everything is fine. Whoever treats that silence as *"there is no coexistence on this number"* concludes
wrongly. What remains guaranteed is the other layer: the other App has another `app_secret`, so its webhook takes a
`403`. **No new countermeasure was created, on purpose** — an extra guard for "identical ids" would be complexity
without a threat, since the layer that decides is another one.

*Cost: one false alarm sent to a consumer and withdrawn 11 minutes later. On 2026-07-28 I measured 16 `InboundEvent`
from 12–15/07 in the production database with the routing ids matching, and at 09:25 I wrote on `consumer-a`'s
channel a reading of that traffic — "message content included" — which was* **right about the format and misleading
about the meaning**, *because the framing insinuated a second business plugged into the production number. Withdrawn
at 09:36, with the owner's explanation: he used the same number to validate, and the other business's App is idle.
There was no coexistence at all.*
**Asking the owner cost one line and came AFTER the alarm.**

**⚠️ And the sentence "the signature is the boundary" has a CONDITION — writing it without the
condition would be trading one wrong doc for another, which is the typical failure mode of an audit
(see *Documentation*, "Hunting for false docs produces false docs").** It holds because **each consumer uses its own App** (the owner's decision, 2026-07-26,
recorded in the comment of step 5 of `internal/inbound/handler.go`), and an App of its own implies an `app_secret` of
its own. **Nothing in the gateway prevents two instances in the SAME App:** the `app_secret` column of `instancia` is
`TEXT NOT NULL`, with no `UNIQUE` (`internal/config/store.go:306`). In that configuration the two instances share the
secret, the signature **stops distinguishing** one from the other, and 5a/5b become **the only separation** — which is
precisely the configuration they were written for (the per-number webhook override, cited in step 5's comment, is the
path for whoever puts two numbers in the same App; and 5b was born in T-038 exactly for an App with more than one
WABA). **So what this entry corrects is the NAME, not the value of the guards:** today they check addressing; in a
shared App they would be the boundary. A doc that asserts either of the two unconditionally is wrong half the time.

**Two lessons, and the second is the one that survives the correction:** (1) **a measurement does not bring context
with it** — I had the hashes and did not have the story, and the hashes alone sustained a conclusion worse than the
truth; (2) the alarm fell, but **the wrong doctrine did not fall with it**: calling a guard that answers `200`
"isolation" trains the reader to read its counter as a measure of intrusion. **The question that separates the two
classes, and it is free: does this guard REFUSE the request or CHECK the address? If the answer it gives is `200`, it
is the second — and its zero does not prove what you want it to prove.**

**A reaction WITHOUT the `emoji` field is Meta saying "I removed the reaction" — it is not a malformed payload.**
T-023 (`docs/TASKS.md`) was written believing the opposite: the task's own text asked for "a reaction without an emoji
in the payload" to be treated as a counted parse error. Meta's official doc
(developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/reaction/, read on
2026-07-26) says the opposite: *"When an end user removes a reaction emoji, a webhook without the 'emoji' field will
be sent."* Following the task to the letter would have produced exactly the defect the whole of T-023 exists to close
— a legitimate event (the removal) counted as a `parse_error` and discarded from `eventos`, silently
indistinguishable from a genuinely corrupted payload.
*Cost: zero — caught BEFORE it became a bug, by verifying Meta's doc before writing the test the task asked for (this
project's doctrine: "check every assertion against the code/source, never against the old doc" — here the "old doc"
was the task itself, written without consulting the source). Fix: `internal/meta/parse.go` only counts as malformed a
reaction WITHOUT `message_id` (the target, which Meta always sends); a missing `emoji` becomes a `Reacao{Alvo: …}`
without the `emoji` key, passing on the same absence Meta uses. Proven by
`TestParseWebhookAReactionWithoutAnEmojiIsAValidRemovalNotAParseError`, which goes red if somebody "corrects" that
guard back to the task's literal text.* **The question that would have avoided writing the wrong task: "was this
assertion about Meta's format checked at the source, or is it a deduction by whoever wrote the spec?"**

**On SENDING the rule is the OPPOSITE of receiving, and an AI search would have made someone write the wrong code.**
T-024 asked how a reaction is removed on the SENDING side. A web search summarized (2026-07-26) that "an empty emoji
removes the reaction" was WhatsApp Cloud API behaviour — but the source behind the summary was a third-party
aggregator (not Meta), and the same search threw in an "unreact" that belongs to **another Meta product** (Messenger
Platform, `sender_action: "unreact"`), not to the WhatsApp Cloud API. Fetching the official page directly
(developers.facebook.com/docs/whatsapp/cloud-api/messages/reaction-messages, read on 2026-07-26) contradicted both:
it lists `emoji` as *"Required"* and does not mention removal anywhere. That is, on RECEIVING a missing `emoji` IS
the removal (see the entry above); on SENDING the same absence **has no documented semantics at all** — the two
halves of the same feature are not mirrored, and treating them as if they were would have produced a send that Meta
accepts with `200` without removing anything.
*Cost: zero — caught BEFORE writing code, following the task's own instruction not to guess without a source. Fix
(T-024): `internal/outbound/mensagem.go`, `validateReaction` refuses an empty/missing `emoji` on send with
`ErrRemocaoDeReacaoNaoSuportada`, and only the ADD case is supported. **The question that would have caught it
earlier: is a search summary about an API citing the doc of the right API, or that of a neighbouring product from the
same company?** — Meta searches for "reaction" and "unreact" cross Messenger Platform, Instagram and WhatsApp Cloud
API freely, and they do not share semantics.*
**Update (T-027): the refusal above was REVERTED — an empty `emoji` today removes the reaction.** The reason the
refusal was the right decision AT THE TIME remains true (no reliable source sustained removal); what changed is that
a source came to exist (see the entry below). `ErrRemocaoDeReacaoNaoSuportada` was removed from the code — this
paragraph stays as a record of HOW the original refusal was reached, not as a description of current behaviour.

**API success is not effect success; when the only witness is the customer's handset, "documenting" means going and
looking.** (Formulation by `consumer-a`, 2026-07-26 — more precise than anything this project would have written on
its own.) T-027 finally had a source for reaction removal on the sending side: `consumer-a` ran the experiment with a
HANDSET on 2026-07-26 (10:15 -03), with the owner watching the screen, because no doc — neither the official one nor
an aggregator — described the path (see the entry above). Two sends through the direct Graph API, same body, only the
`emoji` changing: `{"emoji":"👍"}` made the reaction APPEAR on the handset; the SAME body with `{"emoji":""}` made
the reaction DISAPPEAR. **The detail that carries the whole pitfall: on both sends Meta answered `200` with a NEW
`wa_message_id`.** If the reaction had NOT disappeared on the second send, the response would have been byte for byte
the same — the `200` proves Meta ACCEPTED the request, never that the EFFECT happened. An automated test, a local
`curl`, a doc reading: none of those methods would have distinguished the two outcomes, because both produce the same
response. Only the owner's handset, watched live, decided which of the two is true.
*Cost: none in production — but the cost in TIME was real: the task sat still from 2026-07-24 until the experiment on
2026-07-26 because no desk source (official doc, aggregator, search) sufficed, and guessing would have risked a
removal that Meta accepts and does not execute, with NO error signal anywhere. Fix (T-027):
`internal/outbound/mensagem.go`, `ReacaoPedido.Emoji` became a `*string` (nil = key absent = error; pointing at `""`
= removal; pointing at a value = add) and `validateReaction` accepts an empty emoji; `internal/outbound/corpo.go`
sends the `"emoji"` key ALWAYS, even empty — an `omitempty` (or an `if != "" { ... }`) would erase the key and turn
every removal into a request with no effect, with Meta answering `200` all the same. Proven by mutation:
`TestReactionBodyWithEmptyEmojiSendsTheKeyEmpty` goes red if the key becomes conditional. **The question that
generalizes: for this effect, is there ANY way for the API's response to differ between "it happened" and "it did not
happen"? If not, the only proof is a photo/video/witness from outside the system — and "documenting without a source"
becomes "documenting without checking", which is worse.***

**A fixture "correct by the doc" may be testing the RARE case, and only a real capture reveals which case is the
common one.** T-026 swapped `testdata/corpus/localizacao.json` (derived from the doc) for a real capture from
`consumer-a` (2026-07-26). Meta's doc shows a `location` example with `name` and `address` filled in (a business pin),
and the old fixture copied that example — both fields are technically optional according to the same doc, but nothing
in it says which of the two cases is more frequent. The real capture showed the opposite of what the fixture tested:
the BARE pin — without `name` and without `address` — is what Meta sends when somebody shares a location through the
app normally; the pin with name/address is the "share a place" button case (a business). A fixture that only tests
the rare case leaves the common path (both fields absent) with no coverage at all — and it is exactly the path every
real consumer will hit first.
*Cost: zero — the divergence broke nothing because `Localizacao.Name`/`Localizacao.Endereco` already use `omitempty`
and the parser already reads both as optional since T-023; the finding is about test COVERAGE, not about the code.
Fix: `localizacao.json` became the bare pin (real capture), and the case with name/address got a synthetic test of
its own (`TestParseWebhookReadsALocationWithNameAndAddress`, `internal/meta/parse_test.go`) so as not to lose coverage
of the documented path. **The question that generalizes: a doc example that "matches" the code proves the PATH is
accepted, never that it is the USUAL path** — and a corpus that exists only to prove acceptance, having never seen
real traffic, cannot say which branch matters more.*

**A corpus with a single ONE-codepoint emoji proves nothing about MULTI-codepoint emoji.** The old `reacao.json`
fixture (derived from the doc) used `"👍"` — a single codepoint. `consumer-a`'s real capture (2026-07-26) brought
`"❤️"`, which is **two** codepoints (`U+2764` HEAVY BLACK HEART + `U+FE0F` VARIATION SELECTOR-16) — WhatsApp's most
common emoji are exactly of that composite kind (hearts, several faces, flags), so a corpus that only tests
one-codepoint emoji never exercises the path where truncation by "take the first character" would break silently.
*Cost: zero — no code in this project truncates emoji today (`Reacao.Emoji` is a `string` and passes straight
through), but the mutation proved the risk: replacing `Emoji: m.Reaction.Emoji` with a version that keeps only the
first rune leaves `TestCorpusInteiro/reacao.json` and `TestParseWebhookReadsAReaction` red — and they only go red
because the fixture now has a two-codepoint emoji. With the old `"👍"` (a single codepoint) the same mutation would
go unnoticed. **The question that generalizes: does this test value have the SAME shape (count of bytes/runes/levels)
as the most common value in production, or only the same appearance to the eye?***

**The reaction event's `id` and the target's `message_id` are two DIFFERENT fields, and only a real capture proves
the right order.** T-026 asked to check whether `messageEvent` (`internal/meta/parse.go:750`) uses the EVENT's id
(`m.ID`) for `Evento.ID` and the target's `message_id` (`m.Reaction.MessageID`) for `Reacao.Target` — not the other
way round. Checked: **it was right**, and `consumer-a`'s capture (2026-07-26, two events with the event `id` and
`reaction.message_id` always different from each other) confirms by observation that the two values never coincide in
production. It is not a finding (there was no bug), but the mutation proving the test would catch the opposite is
recorded: swapping `Alvo: m.Reaction.MessageID` for `Alvo: m.ID` leaves `TestCorpusInteiro/reacao.json`,
`TestCorpusInteiro/reacao_removida.json`, `TestParseWebhookReadsAReaction` and
`TestParseWebhookAReactionWithoutAnEmojiIsAValidRemovalNotAParseError` red — all four compare against
`wamid.TESTE001`, which only appears as a target in the fixture, never as an event id.

**`errors[]` exists INSIDE `messages[]`, not only inside `statuses[]` — and it is the SAME mother pitfall of this
file, for the third time.** T-023 fixed the message envelope for reactions (they arrived labelled and with no
content) and forgot the status event; T-028 fixed the status event for `errors[]` (failure reason) and nobody asked
whether Meta also sends `errors[]` inside a MESSAGE. It does: when it receives something the Cloud API cannot
represent, the `sub_tipo` arrives as `"unsupported"` with `errors[]` next to `from`/`id`/`timestamp` — format
confirmed at
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/ (read on
2026-07-26), code `131051`, `"Message type unknown"`. `messageMeta` had no `Errors` field until T-033: the event came
out with an id and a label, and nothing else — indistinguishable from "empty message" for the consumer, when the
truth was "Meta received something and could not decode it".
*Cost: zero in production — no consumer had received an `unsupported` yet. Found by `consumer-a` auditing the **218
raw payloads** they had already stored (counting the keys Meta sends against the ones the code reads), not by a new
test nor by new traffic — it is a FOURTH way to find this family of defect, alongside "adversarial review"
(T-023/T-028) and "real capture" (T-026): **rereading what has already been captured with the right question.** Fix:
`mensagemMeta.Errors []json.RawMessage`, reusing the SAME `StatusError` and the SAME `Evento.Erro` field the status
already used — not a sibling type with a similar name, because the item's format is identical in both places and only
the MEANING changes with the event's `tipo` (documented in `internal/meta/types.go` and
`docs/CONTRATO-CONSUMIDOR.md`, so as not to live only in the head of whoever fixed it).* **The question that is still
the same, and is worth repeating for the third time: "where else should this same sentence be true, and is it?"** —
this time the answer was "in the SAME file, one level over", not in another repository nor in another language.

**A `200` on SENDING has the SAME pitfall as a `200` on receiving, and it nearly became the FIFTH case of the mother
pitfall.** `idDaResposta` (today `sendResponse`, `internal/meta/client.go`) read only `messages[0].id` from the
`POST /{phone_number_id}/messages` response — the same object also carries, sometimes, `message_status`. Before
T-034, a template send under pacing that Meta held (`held_for_quality_assessment`) or refused later (`paused`/dropped
for negative feedback) returned the **same** `200`+`wamid` as a normal send — the consumer recorded "sent" for a
message that might never arrive. It is the SAME sentence as the entry "A `200` from Meta does NOT prove an id came
back" (further up, about a missing/empty/wrong-typed id), applied to a NEIGHBOURING field in the same object nobody
had looked at yet.
*Cost: zero in production — no template of this gateway had entered pacing yet. Found while researching (at the
owner's request, T-034) whether there was an alternative to a physical handset for confirming the outcome of a
reaction (T-027) — the answer to that question was "there is none", and the documented reason is precisely that
`accepted` only means "Meta accepted the request", never "the effect happened"; on verifying the source of that
sentence, the field the gateway did not read showed up.*
**The source has an internal contradiction worth recording, for whoever touches this again:** the API reference page
(`developers.facebook.com/docs/whatsapp/cloud-api/reference/messages`, read on 2026-07-26) lists `paused` as one of
the THREE values of `message_status` itself ("paused: the message delivery has been paused" — reading: of the
MESSAGE). The template pacing page
(`developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-pacing`, same date) describes
"paused" as the status of the TEMPLATE ITSELF turning `PAUSED` — a DIFFERENT field, of the TEMPLATE. Both are
official Meta pages and they disagree with each other about what "paused" is. Fix:
`RespostaEnvio.MessageStatus` passes the RAW value along, without deciding which of the two readings holds —
inventing a translation here would be the same pitfall as "Duas fontes independentes que descem do mesmo nada" (see
*Documentation*, below), only with OFFICIAL sources disagreeing with each other instead of unofficial ones agreeing
by accident.

**And the mutation that proves the other half:** the field arrives absent for every send that is not a template under
pacing (confirmed by the reference page itself: *"only included in responses when sending a template message that
uses a template that is being paced"*) — absence and `"accepted"` are outcomes the CONSUMER treats the same (Meta
accepted), but the CODE cannot fabricate `"accepted"` for the absence, because that would assert a value Meta never
sent. Proven by mutation in `TestSendMessageAnAbsentMessageStatusStaysEmptyNotAccepted`
(`internal/meta/client_test.go`): swapping the absent `MessageStatus` for a fabricated `"accepted"` leaves that test
red, even without changing anything visible in the HTTP body (the distinction only matters internally — see
`sendResponse`).

**Classifying a third party's error by the ENVELOPE (HTTP status) instead of by the CONTENT (code) works until the
third party changes the envelope — and the change generates no error at all, only a silently wrong
classification.** `classOfStatus` (`internal/meta/errors.go`) decided `retentavel` × `permanente` × `config` looking
only at the HTTP status; no Meta error code was consulted on the sending path. The official error-code doc
(`developers.facebook.com/docs/whatsapp/cloud-api/support/error-codes`, section *Throttling errors*, read on
2026-08-20) **does not declare** with which status the rate-limit family arrives, and the Marketing Messages API
shows an error of the same shape arriving as a `400`. If a throttling error arrived wrapped in a `400`, it fell into
the default (`permanente`) and the consumer stopped trying — exactly when waiting and trying again was the solution.
*This project had already seen the same defect shape, in the SAME file: `classOfStatus` treats `408` and `425` as
retryable "by HTTP definition" precisely because letting them fall into the default would make the consumer give up
on something recoverable — the comment there already recorded the risk of an unexpected status carrying a meaning
different from the one the code presumes. The throttling table (T-142) is the same reasoning applied on the side of
Meta's CODE, not of the HTTP status: the code does not have the envelope ambiguity the status has.*
*Cost: **it did not charge** — found in an audit (T-142), reading the contract against the code, before any consumer
received a throttling error wrapped in a `400`.*
**The fix:** a conservative table of ours of throttling codes (`retryableCodesByNature`, `internal/meta/errors.go`)
that only PROMOTES a status that fell into the default (`ClassPermanent`) to `ClassRetryable` — it never downgrades
`ClassConfig` nor what the status already classified as `ClassRetryable`. Guards in `internal/meta/errors_test.go`,
among them `TestClassifyAThrottlingCodePromotesPermanentToRetryable` (the central case) and
`TestClassifyAThrottlingCodeDoesNotDowngradeClassConfig` (order matters: the table is a second chance, not a second
judge).

---

### "Still in the catalogue" is NOT "it was not deleted" — Meta leaves the template visible in `PENDING_DELETION` (2026-08-28)

Deleting a template does **not always** make it disappear from the catalogue. Verbatim from Meta, read on 2026-08-28
at developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management:

> *"If you delete a template that has been sent in a template message but has yet to be delivered
> (for example, because the WhatsApp user's phone is turned off), the template's status is set to
> `PENDING_DELETION` and WhatsApp attempts delivery for 30 days."*

**The pitfall is for whoever confirms a deletion by rereading the catalogue** — which is exactly what this gateway
does when the call ends with no response from Meta (T-173, `deletionAccepted` in
`internal/outbound/templates_handler.go`). The naive rule *"gone = deleted; still there = inconclusive"* reports
**doubt on top of success**, and in a cleanup of dozens that becomes an item on the "I do not know" pile that was
right all along. The right rule: absent **or** all remaining rows in `PENDING_DELETION` → deleted; any live row under
another status → inconclusive.

*It charged nothing, and only because the page was opened.* The wrong design was written and sent to a consumer for
eight minutes, on 2026-08-28: the naive rule was sent on the channel at 11:18 and corrected at 11:26, **after** a
`GET` on the documentation that had been fired for another reason — confirming a 30-day deadline the consumer had
cited. **The finding was not looked for: it was on the same page.** It is the cheapest argument there is in favour of
opening the source instead of accepting the quotation: you pay one `GET` and get the neighbouring paragraph for free.

**A corollary that holds beyond Meta:** when a third-party system has a transition state (`PENDING_*`, `DELETING`,
`DRAINING`), your confirmation reread has to know **which states count as success**. Treating "it still exists" as a
failure is the default failure mode of every confirmation-by-reread, and it produces rework, not an error — which is
why nobody sees it.

📊 **Same-day update, and it does NOT cancel the entry: in 29 real deletions, `PENDING_DELETION` did not appear
once** (first cleanup through the route, 2026-08-28; the 29 had already vanished from the listing 1 to 3 minutes
later, with the consumer's status set closing on `APPROVED`/`REMOVIDO_DA_META` and nothing under "others").
**None of the 29 had a recent send**, and it is plausible — not proven — that having a message in flight is the
trigger. *Recorded here so that nobody reads the pitfall as "this happens all the time" and, above all, so that
nobody reads it backwards: **a rare case the code does not handle is exactly what fails on the day it happens**, and
here the cost is the consumer redoing work already done.*

## Brazilian phone numbers

**`==` between two "normalized" phone numbers does NOT prove they are the same person.** Meta does **not guarantee**
the same spelling you registered: the same subscriber arrives as `5532999990000` and `553299990000`.
*Cost: it charged in production in another project of this network — an E2E in which the webhook stored, answered
`200`, the `==` matched nothing and **nothing was sent back**. From outside it was indistinguishable from success.*

**The guard that separates the fix from the damage is the FIFTH digit.** A landline is `55` + area code + 8 digits —
**also 12** — but a mobile subscriber number starts with 6–9 and a landline one with 2–5. An
`if len == 12 { insert the 9 }` would produce **non-existent** numbers for every landline in the country.

**`sha256(phone)` does NOT anonymize — it is the APPEARANCE of anonymization, and the search space is far too small
to hide anything.** While designing the TRANSIT log (T-091, "did this message pass through here?"), the obvious
temptation was to index by a plain hash of the phone number. A Brazilian mobile number has a small and highly
structured search space — a fixed `55`, a two-digit area code out of ~67 valid ones, the mobile's `9` prefix, and only
8 free digits — so enumerating and hashing **every** possible number of one area code and comparing against the table
breaks it by brute force in seconds on an ordinary laptop. An unkeyed hash is not a secret: it is just a truth table
published in different clothes.
*What actually anonymizes is HMAC-SHA256 with a KEY, and the key has to live OUTSIDE what can leak along with the
hash — here, derived from `ZAPGW_CHAVE_CIFRA`, which already lives outside the database (it is what makes a SQLite
backup safe to exist). With a key, whoever steals the database cannot enumerate the numbers that talked to the
gateway; without a key, the "anonymized" data opens with a script.* See `internal/config/crypto.go`
(`Cofre.DeterministicHMAC`) and `internal/config/transito.go`.

---

## Tests

🔥 **`-ldflags -X` FOR A SYMBOL THAT DOES NOT EXIST IS SILENTLY IGNORED — and it is the whole deploy lying with a
green push (2026-08-30, T-172 batch 5).** `implanta/deploy.sh:286` injects the version into the binary with
`-ldflags "-X main.version=$VERSAO_DO_BUILD"`. The layer-2 rename changed that variable from `versao` to `version`.
**Had the implementer renamed only the code:**

- `go build` would exit **`0`** — the linker does not complain about `-X` for a non-existent symbol;
- the published binary would answer `versao: "desenvolvimento"` on `/v1/health`;
- and `deploy.sh` **would not abort**, because `imprimir_versao_do_corpo` (line 187) only **prints** — there is a
  comment in the script itself saying it never returns an error.

*Positive control measured, not deduced:* `go build -ldflags "-X main.versaoQueNaoExiste=9.9.9"` exited `0` and the
binary answered `desenvolvimento`; with `-X main.version=1.2.3-teste` it answered `1.2.3-teste`. **Real cost: zero —
the implementer changed both sides in the same commit and said so in the report.** No mechanism protected anything:
it was the attention of the person who was there.

🔴 **The family, and it is bigger than Go:** *a contract between one file and another, written in text no compiler
reads.* `-X main.X` on the build line, a column name in SQL, an environment variable key, a field name in
`json:"…"`, a path in an `ExecStart`. **The whole of layer 2 leaned on the sentence "the compiler proves the rename
is complete" — and this is exactly the boundary where it stops holding.**

✅ **FECHADO em 2026-08-30 pela T-184**, no mesmo dia: o `deploy.sh` passou a **comparar** a versao que o gateway respondeu com a que ele acabou de construir, e a **abortar e reverter** quando divergem — distinguindo isso de *"nao consegui ler a versao"*, que continua nao sendo falha (binario anterior a T-025, ou deploy com `ZAPGW_DEPLOY_BINARIO`). ✅ **PROVADO EM PRODUCAO em 2026-08-30 23:45**, no CT real, com o consumidor avisado e a janela combinada: um build com o simbolo do `-X` trocado de proposito publicou um binario que respondia `versao: "desenvolvimento"`. **O `/v1/health` devolveu `{"ok":true,...}` — ou seja, a logica ANTIGA teria aprovado.** A conferencia nova acusou (`VERSAO DIVERGE: construida=0.60.1 respondida=desenvolvimento`), reverteu sozinha, o binario anterior voltou (`d8df2bf6…`), o gateway respondeu `0.60.0` e o script saiu **`1`**. *O mecanismo reprovou uma vez, contra dado real, em producao — que e' o que esta casa exige antes de chamar qualquer coisa de mecanismo.*

**What is left open, said by name:** `deploy.sh` **does not check** whether the version that went up is the one it
has just built. It prints and moves on. It is **T-184**.

🔥 **A GUARD THAT MATCHES A METHOD NAME AS A *STRING* STAYS GREEN MATCHING NOTHING (2026-08-30, T-172 batch 1).**
`cmd/zapgw/menu_test.go` walks the AST looking for calls by **literal name** — `"ListarInstancias"`, `"Fechar"`, plus
a list of forbidden methods. In the rename-to-English pass, the code changed name and **the string did not**: the
guard would go on **passing**, because it does not find what it is looking for and "I found nothing forbidden" is
exactly its green result.

*Cost: zero, and only because the suite broke for ANOTHER reason in the same commit and the implementer went to look.
Had the rename touched only the methods the forbidden list cites, it would have passed clean — and the isolation
guard would be silently switched off, with the test green beside it.*

**The family is the blind monitor's**, and the trait that identifies it is this: *the guard distinguishes "I found it
and it is wrong" from "I found it and it is right", but does NOT distinguish either of them from "I found
nothing".* Every sweep that matches an identifier by string has that hole — and it is invisible to the compiler,
which is precisely the proof layer 2 leans on.

**What to do with one of those:** either it **fails when it does not find its target** (an expected count > 0,
declared in the test itself), or the name comes out of the string and becomes a reference the compiler can see.
*A sweep by name with no count floor is a test that retires itself at the first refactor.*
**The repository's other two text sweeps were checked at the same moment** and do not have the defect: the TLS one
matches `InsecureSkipVerify` (a third party's name, not renameable by us) and the phone one matches digits.

🔥 **An instrument that cannot read something returns an APPARENTLY COMPLETE list, not a warning.** On 2026-08-20,
proving `v0.50.0` on a handset, seven messages were sent and the owner pasted the WhatsApp conversation's **text
export** as proof of delivery. Four of the five Meta accepted came through; the missing one was precisely the only
`cta_url` — and I concluded, with the time window fitting neatly, that the gateway had received a `200` **without
delivering**. I went as far as publishing the suspicion on the consumer's channel.
**The message had arrived.** The screenshot showed all five. **The WhatsApp export does not list a `cta_url`
message** — and it does not say it does not: it returns the rest as if that were everything.
*Real cost: one false alarm published to a consumer, and the retraction three minutes later.* Cheap only because the
suspicion was labelled as a suspicion; presented as fact, it would have taken down a path that works.
**The rule, and it is the blind monitor's with the target swapped:** *the instrument has a blind spot too, and the
blind spot LOOKS like the defect you are looking for.* Before concluding "it did not arrive" from a reading tool, ask
**whether that tool can see this kind of thing** — and prefer the source that does not interpret (the screen, Meta's
status) to the one that summarizes. *Sibling of the rule that a check must distinguish **it failed** from **I could
not check**: here the instrument did not have the second state, so absence became a negative.*

🔴 **A test that asserts using production's CONSTANT tests nothing — it agrees with the code, whatever the code
is.** On 2026-07-30, in T-095, the assertion that `zapgw log`'s columns stay separated was
`strings.HasPrefix(resto, separadorColunaLog)` — comparing the output against the **same constant** that produced it.
With `separadorColunaLog = ""`, `HasPrefix(x, "")` is **always true**: zeroing the separator (the exact defect the
test existed to catch) left the test **green**.
*Cost: zero — the person who wrote the test caught it before the commit, by asking "what makes this test go red?".
Fix: a fixed `"  "` literal inside the test, independent of production; checked that with the constant zeroed it goes
red and with `"  "` it goes green.*
**The question that generalizes, and it holds for every assertion:** *what change to the code makes this test fail?*
If the answer is "none, because the expected value comes from the code itself", the test is a **tautology wearing the
look of a proof** — and it is worse than no test, because it occupies the slot of the one that would protect. A
test's expected value is **written by hand**, not imported from what it is supposed to be watching. *It holds equally
for a table of cases that derives the expected value with the same function under test — same tautology, different
clothes.*

**An equal body size is CORRELATION; the `wamid` and the `timestamp` inside the payload are IDENTITY.**
Investigating whether the message stuck since 2026-07-25 23:40 had finally been delivered, the proxy log showed seven
`504`s and one `200`, **all with `RequestContentSize=549`** — a size that appeared in no other request that day. It
looked conclusive: same message, a sequence of failures ending in success. It was declared as fact, in an answer to
the owner and in a paragraph of `docs/TASKS.md`.
**It was false.** The consumer had the datum that decides — the `timestamp` **inside** Meta's payload, which says when
the USER sent it — and it pointed at a new message (`10:06:30 UTC`), not the stuck one (`02:40 UTC`). Two short text
messages collide on size easily: Meta's envelope is almost all fixed structure, and the content barely moves the
needle.
*Cost: a wrong statement said as a headline and written into the repository, retracted the same day because the
consumer said "it still has not arrived" and that forced a recheck.*
**The rule: the proxy log answers "how many and with what status", never "which one".** For identity, the datum has to
come from inside the payload — and it existed, you only had to ask. A strong correlation with the wrong datum is more
dangerous than no datum, because it convinces.

**To prove that a dependency is GONE, run the suite with it UNINSTALLED — not with it present and apparently
unused.** A green suite with the library installed does not distinguish "I no longer use it" from "I use it without
noticing": a forgotten `import` on a rarely exercised path goes unnoticed, and the day of discovery is the first
deploy that does not have it.
*Method observed at the consumer (`consumer-a`, 2026-07-26) while removing the private lib that motivated half of
this week's tasks: **303 tests with the lib, 303 without the lib**, plus the build running **with no SSH agent at
all** — because the lib came from a private repo over SSH, and a build that still pulled it would fail right there.
Two proofs of ABSENCE, not of presence.*
**The generalization holds for this repo:** when you remove any dependency from here, the proof is not the test
passing — it is the test passing **in an environment where the dependency cannot be resolved**. It holds for a
library, for an environment variable and for a credential: the way to prove a `META_GRAPH_TOKEN` is no longer used is
to run **without it**, not to grep for it.

**A `400` with a `text/plain` body is NOT the gateway refusing — it is Go refusing before the handler, and the two
are indistinguishable if you only look at the status.** A production proof sent four invalid requests, got
`HTTP 400` on all four and was declared passed. None of the four ever got validated: the token came from
`cat /var/lib/zapgw/consumidor-consumer-a.txt`, the file has **three lines of prose around the value**, and an
`Authorization` header with a line break makes `net/http` answer `400 Bad Request` in `text/plain` with
`Connection: close`, without ever calling the handler. The result had exactly the shape of the expected success.

Two rules come out of this, and the second is the one that saves you:

1. **A gateway error is `Content-Type: application/json` with `{"erro":{…}}`.** `text/plain` + `Connection: close` is
   the HTTP server, not the code. Look at the body, never just the status.
2. **Every proof needs a CONTROL case that only passes if the credential and the route are right** — a request that
   must fail in a *named and different* way. Without it, "it refused" and "it never arrived" produce the same output,
   and the proof measures its own harness. The control here was `tipo: "nao-existe"`, which returned
   `tipo de mensagem desconhecido` in JSON — and only then did the four `400`s start to mean something.

*Cost: a false proof reported as true, corrected in the same turn. The signal that gave it away was a `wc -c` on the
token: 222 bytes where 64 fitted.*

**A guard that sweeps a directory and finds nothing passes GREEN without having verified anything.** Every guard needs
`assert items_verified > 0` **and** to have been seen failing at least once.
*Cost: it has not charged here yet — the corpus guard was born with all three locks and was proven failing on all
three. It charged in another project of this network, where an architecture test swept paths relative to the working
directory, was run from somewhere else and passed without scanning anything.*

🔥 **A gate that sweeps a LIST of paths only protects what someone remembered to list — and the repository ROOT is
the path everybody forgets, because nothing lives there yet the day the list is written (2026-08-30/31, T-191).**
`internal/config/telefones_allowlist_test.go`'s phone-number gate scanned `scannedTargets`, a fixed slice
(`cmd`, `internal`, `testdata`, `docs`, `implanta`, `README.md`) grown twice already (T-159, T-185) as new places
turned out to carry a real number. The repository root was never on it. The consumer channel file — a real phone
number and a production `wamid`, in a repository that is PUBLIC — was created **at the root**, sat **untracked**,
and the gate said nothing: not because it looked and passed, but because it never looked there at all. Nobody was
warned, because the test's own green run is what "nothing wrong" and "nothing scanned" look like from the outside.
*Real cost: the consumer noticed it first, going to write to that same path. No `git add -A` ran in that window —
the repository stayed clean by luck, not by the gate's design.*
**The fix (T-191) inverts the direction of the enumeration**, from "which paths did we remember to add" to "which
files would `git add -A` pick up right now": `git ls-files` (tracked) plus `git ls-files --others
--exclude-standard` (new and not ignored) — the exact set the risk this gate exists for is made of. A new file
anywhere, including the root, is in that set the instant it exists; nobody edits this test to add a directory
again. **The ignored side stays deliberately uncovered** — `*.local.md`, the same channel file, must never be
swept, or the verify would be red forever for something it exists on purpose to hide from commits — so `git
ls-files --others --exclude-standard` (which already excludes what `.gitignore` excludes) is exactly the right
half, not an oversight.
**The sibling checked before closing, as the doctrine requires:** the TLS gate (`internal/inbound/deliver_test.go`,
`TestNoSourceInTheRepoTurnsOffTLSVerification`) does **not** have this hole — it already `filepath.WalkDir`s the
WHOLE module root (filtering by `.go` extension and skipping hidden directories), never a fixed list of
subdirectories. A path added to the repository is in its sweep automatically; only the phone gate needed the fix.

**A leak test passes GREEN when the fixture erases the branch that would leak.** The test requiring "none of the four
secrets appears in the output of `provisionar instancia`" filled all four from the environment — and that is
precisely where the command **does not print** the line naming the generated secrets. The assertion ran; the code it
aims at did not. A mutation that concatenated the four values in the clear on that line **went unnoticed**.
*Cost: caught by mutation testing during T-005, before the commit. Fix: the **generated** secrets case reads the
values back from the database and looks for them in the output — it is the only way to cover a branch whose values the
test does not know in advance. General rule: the test case has to make the suspect branch EXECUTE; choosing the most
comfortable fixture usually means choosing the branch that does not leak.*

**A refusal test that turns green because the PRECONDITION changed proves nothing.** When you change a test's factory
(for example, starting to create the instance already active), check case by case whether each test still reaches the
code it tests — or whether an earlier guard has started masking it.
*It charged again in T-014, in the opposite direction: the health probe's `403` test ran over the other consumer's
instance while it was **paused**, so with the link guard switched off the refusal came from the pause (`503`) and the
status assertion went red for the wrong reason — the mutation looked "caught", but the `403` had never been
exercised. Only after activating the other consumer's instance did the mutation reveal what was really happening:
`200`, with the **other tenant's number** in the body. Cost: one round of mutation, before the commit. **A test's
factory has to let the guard it aims at be the FIRST to speak** — which is why the helper now receives which instances
stay active, instead of a boolean.*

**A "red" also has quality.** A test that fails with a **compile error** proves less than one that fails on an
assertion: the first only shows the symbol does not exist, the second shows the behaviour is wrong. When the red comes
from compilation, say so in the report.

**An assertion "this must not leak" that looks for the whole INPUT matches the HELP text of the error message
itself.** The `callback_url` guard refuses the URL and explains the rule: *"tem de ser https:// …"*. The test required
the error **not to contain** the refused URL — and one of the refused cases is exactly `"https://"` (scheme without
host). It went red without anything being wrong in the code. *Cost: one round of false red in T-010, without reaching
the commit. Fix: each table case carries a `marca` — the piece that **identifies the consumer**
(`consumidor.interno`, `<ip-interno-do-conector>`) — and that is what is searched for, not the whole input. Cases with
no mark (`"https://"`, `"   "`) have nothing to leak and do not make the assertion. The general rule: look for the
**secret**, not the input; if the two get confused, the test case is badly chosen.*

**Every `httptest.NewTLSServer` in the process uses the SAME certificate — so "one's CA is not valid for the other"
passes green without testing anything.** The package embeds a fixed pair (valid until 2084). The test requiring one
instance's anchor not to validate another's consumer brought up two servers and compared two **identical**
certificates: the assertion ran and could not fail because of what it aims at. It only showed up because the test was
written before the implementation and went **green too early** — `a CA da instancia A validou o certificado do
consumidor de B` appeared on the first run, and the cause was the fixture, not the code.

*Cost: one investigation round in T-013, without reaching the commit. Fix: forge a self-signed certificate per server
(`selfSignedCertificate`, in `internal/inbound/deliver_test.go`) — which is also the only way to test an **expired**
certificate, since `httptest`'s is valid for 58 years.*

**A per-key cache hides a leak between tenants, and the "obvious" test does not catch it.** Two different mutations —
a constant cache key and a CA pool that **accumulates** — survived the "A's CA is not valid for B" test: in the first,
A's client stayed cached and refused B for the wrong reason; in the second, the leak only manifests **after** both CAs
have been through there.
*Fix: the test became a SEQUENCE with four assertions — A passes, B with A's CA fails, B with B's CA passes, and **B
with A's CA fails again at the end**. It is the last one that kills the accumulating pool, and it is literally the
repetition of an earlier call: what is tested is the response CHANGING according to what happened in between. General
rule: a tenant isolation test needs to exercise both in order, and come back.*

**Mutation testing is the only proof that a test tests anything.** When reviewing a set that protects an ordering
(slice indices, field positions), **break it deliberately** and confirm the suite flags it. That is how the swap of
positions between credentials was proven impossible to slip through.

**A test that only looks at the DEFAULT value passes green even with the INJECTION mechanism entirely broken —
because the success default and the bug default are the SAME value.** T-025 injects the binary's version with
`-ldflags "-X main.versao=…"`; without the flag, the value is `"desenvolvimento"` on both paths (`zapgw versao` and
`GET /v1/health`). A handler that **forgot** to read the `versao` variable in `/v1/health` and returned
`"desenvolvimento"` by hand would keep passing both tests that only check the behaviour without `-ldflags` — the two
agree with the hardcoded value by accident, not because they prove the variable is read. Only a test that **compiles
the real binary with `-X main.versao=9.9.9` and runs both paths** catches the divergence.
*Cost: zero — proven by mutation before the commit (`TestVersionInjectedByLdflagsPropagatesToBothPaths`,
`cmd/zapgw/main_test.go`): swapping `Versao: versao` for `Versao: "desenvolvimento"` in the `/v1/health` handler, both
default tests stay GREEN and only the test that compiles with the flag flags it. **The question that generalizes:
"would this test still fail if the code under test were replaced by a constant equal to the default?"** — if the
answer is no, the test only proves the default, never the mechanism.*

**Removing the EXACT media route (`/v1/media`) does not return `404` — it returns `307` to the SUBTREE route
(`/v1/media/`), and a client that follows redirects automatically does not even notice.** Go's `http.ServeMux`
redirects when a subtree pattern is registered and the exact one is missing: the request does not reach the handler,
but the status is `307`, not `404` — whoever wrote the test expecting `404` (the standard in the rest of this file)
would have written an assertion that also goes red, only for the wrong reason. More seriously: since `307` preserves
the method and the body, a consumer whose HTTP client follows redirects by default (most do) resends the `POST` to
`/v1/media/`, which still matches the registered subtree pattern, and the message reaches the same handler — the
defect self-heals FOR THAT CONSUMER and fails silently only for whoever treats `3xx` as an error (common in
programmatic API clients). That is why `TestRoutesRegistersTheMediaRoutes` (`cmd/zapgw/main_test.go`) does not check
the status: it checks that the request REACHED the handler, by side effect (the received path) — the only assertion
that does not depend on knowing in advance which `3xx` or `4xx` the ServeMux will choose.
*Cost: zero — found during T-018's mandatory mutation, before the commit.*

🔴 **A test that points the CLIENT's `c.base` and the PARAMETER's `base` at the SAME fake server never exercises
which of the two the code actually uses.** T-097 wrote `SendInstagramMessage` building the URL over `c.base` (the
WhatsApp host, `graph.facebook.com/vNN`) instead of receiving the base as a parameter as `RenewInstagramToken`
already did — and every test at the time (`internal/outbound/instagram_test.go`) used
`meta.NovoClient(metaSrv.Client(), metaSrv.URL)`, the SAME `metaSrv` for everything. With a single host in the test,
"I used `c.base`" and "I used the right base" produce the SAME request — the suite went green in both cases. **The
defect only showed up against the real Meta**, on the first real activation of `tenant-two-ig` (2026-07-31 00:24 UTC
= 2026-07-30 21:24 -03): `Invalid OAuth access token - Cannot parse access token`, because an Instagram Login token is
not parseable at `graph.facebook.com`. T-097 itself had already recorded this as an "honest limit" in its report —
*the tests prove the mechanism, not that Meta accepts it* — and the limit charged on the first instance that reached
production (T-104).
*Cost: a production instance (`tenant-two-ig`) sat paused waiting for the fix between the first real activation and
the fix — the message had already been "proven" outside the gateway (measured against the real Meta,
`POST https://graph.instagram.com/{ig_id}/messages` → `200`), so the delay was purely the gateway not building the
SAME call.*
**The rule that generalizes, beyond "do not use production's constant as the expected value" (the entry just
above):** when the code talks to **different hosts per path** (WhatsApp on one host, Instagram on another), the suite
only proves host selection if the test uses **different servers** for each one — a test with a single host proves
shape (body, method, headers), never ROUTING. T-104's fix (`internal/meta/instagram_test.go`,
`internal/outbound/instagram_test.go`) started using two `httptest.Server`s per test — one that should NEVER be
called, another that MUST be — and proved, by manual mutation (wiring `c.base` back in place of the parameter), that
the old tests stayed green while the new ones went red.

---

### A task's `Verify` said `PASS` without running any of the tests it existed to prove — `-run` matches by REGEXP, not by subject (2026-08-28)

T-173 (route `DELETE /v1/templates`) was written with this `Verify`, to prove the new route entered the isolation
table:

```
go test ./internal/outbound/ -run 'Isolamento' -v   # and the route has to appear in the output
```

It **passes**, and proves nothing. `-run 'Isolamento'` matches only
`TestIsolationTableCOVERSEveryRouteRegisteredInThePackage`; the three tests that actually exercise the table are
called `Test*RotaDeSaida*` / `Test*RotasDeSaida*` and **do not run**. Measured afterwards: `-run 'Isolamento'` →
**0** occurrences of the route in the output; `-run 'Isolamento|RotaDeSaida|RotasDeSaida'` → 6.

*Real cost: zero, and for a reason that does not repeat itself — **the implementer did not obey the `Verify`, went to
check whether it measured anything, and reported it**. The command exited `PASS` with the route absent from the
output, which is the exact shape of the "blind monitor that answers OK".*

**What the `Verify`'s author did wrong, and it is subtle:** the half *"and the route has to appear in the output"* was
right and was the real proof. The `-run` was written **by subject** ("the isolation tests"), assuming the test's name
would contain the subject's word. **A test name is not a theme, it is an identifier** — and the regexp matches the
identifier.

**The rule, and it is cheap:** whoever writes a `Verify` that filters by name **runs the filter before writing the
task** and checks that the output contains what the proof requires. A `grep -c` on what should appear settles it in
seconds. *Same family as "selecting a target by NAME PREFIX" in the infra section: prefixes and regexps choose by
accident of naming, and today's hit is a coincidence of how somebody named things.*

## Environment

🔴 **`go test ./... | grep …` inside an `&&` HIDES a red suite, and that is how a red one reached `main`.** A
pipeline's exit status is that of the **last** command: the `grep` exits `0` because *it found lines*, the `&&` moves
on, and the `echo "VERIFY OK"` prints over a suite that failed.
*Cost, on 2026-07-28: the planner committed and pushed to `main` a merge with `internal/outbound` red, writing "VERIFY
OK" in its own output. Production was not affected only because no deploy was pending — `CLAUDE.md` forbids exactly
this ("if it fails, fix it or do not commit"), and the prohibition was not enough because the command **lied**.*
**The safe form, and the reason the filter is tempting:** this project's test output is enormous (handlers log on
purpose), so filtering is legitimate — it just cannot be in the same expression that decides whether it worked.

🔴 **A `cd` into an agent's worktree PERSISTS between tool calls, and from then on every `git` answers about the WRONG
repository — with the exact symptoms of the contamination disaster.** On 2026-07-29 the planner did a `cd` into an
agent's worktree to read one line of code. Three commands later, still without noticing, they ran
`git log`/`git status`/`git branch --show-current` and read: `HEAD` at the agent's commit, **their own commits absent
from the history**, and the agent's branch checked out. They concluded the main tree had been contaminated and
**warned the owner with a red alarm**. There was no contamination at all: `main` was intact, identical to
`origin/main`, and the planner's three commits were its ancestors. **The directory was another one.**
*Cost: a false alarm of the most expensive kind — the one that tells the owner their repository is broken. Minutes of
diagnosis and a public correction.*
**Why it is a pitfall and not carelessness:** the workspace rule says to check `git branch --show-current` before
committing, and that check **fires identically** in both cases — it distinguishes "wrong branch" from "right branch",
**not** "wrong repository" from "right repository". The missing question is *where am I*:

    git rev-parse --show-toplevel   # BEFORE believing any git log/status/branch
    git -C /c/dev/zapgw <command>   # or, better: never depend on the current directory

**And the reading rule that generalizes:** when you conclude that the repository's state is strange, **confirm first
that you are looking at the repository you think you are**, before believing the conclusion.

A "disappeared" commit is almost always a question asked in the wrong place — and it is cheap to check: if
`origin/main` has the SHA and `git merge-base --is-ancestor` confirms, nothing was lost.

🔴 **A hand-made backup in `/tmp` inside a git repo: the restoring `cp` accepts GARBAGE FROM ANOTHER SESSION without
a single error.** On 2026-07-29 an implementer wanted to save `cmd/zapgw/provisionar.go` before a mutation and chose
`/tmp/provisionar.go.bak`. **A file with that name already existed**, from an earlier session and much older. The `cp`
back worked perfectly — exit `0`, no message — and **overwrote the real file with a version ~751 lines shorter**. The
only signal was `git diff --stat` showing `-751`.
*Cost: the fix was `git checkout --`, which also discarded the legitimate, not-yet-committed edit — so the change had
to be reapplied by hand. Nothing broken reached the commit, and only because they looked at the `--stat`.*
**The rule, and it is stronger than "choose another name":** **in a git repo, do not roll your own backup.** The
repository is already the backup, atomic and per project — `git stash`, `git checkout -- <file>`, or a temporary
commit do the job and **have no way of bringing in content from another session**. If you still need a loose file, use
the **session's scratchpad directory** (which exists exactly for that), never `/tmp`: `/tmp` is shared between
sessions and between agents, and every fixed name there is a collision waiting to happen.
**What makes this pitfall worth an entry instead of a "pay attention":** the failure mode is *silent and inverted* —
the tool you called **to protect** the file was the one that destroyed it, and it reported success. It is the same
family as the `git push` that pushes nothing and the `grep` in a pipeline: **exit `0` is not proof that the right
thing happened.**

```sh
go test ./... > /tmp/t.txt 2>&1; echo "EXIT=$?"   # the status comes before the filter
grep -E "^(FAIL|ok)" /tmp/t.txt                    # the filter comes afterwards, and decides nothing
```

*The rule that generalizes, and it holds for any verify: **never pipe the command whose status you are using to
decide.** `set -o pipefail` solves it in `bash`, but depending on it is betting on which shell ran; capturing the
status explicitly works in all of them.*

**The Go installed by `winget` does NOT enter the `PATH` of already-open shell sessions.** `go version` returns
*command not found*, and whoever is verifying concludes that "it cannot be verified" — and approves by reading.
*Cost: an entire review approved without running the verify. It only showed up because the PATH was checked by hand
afterwards. Solution: use the full path (`/c/Program Files/Go/bin/go.exe`).*

**A check that silently does not happen comes back as "approved".** It is the same shape as the product failures this
project hunts, applied to the review process.

**`* text=auto` in `.gitattributes` makes the checkout on Windows convert to CRLF, and `gofmt` requires LF.** Result:
`gofmt -l .` — which is a verify step — started listing **all 22 `.go` files in the repo, always. A verify that always
flags is a verify everybody learns to ignore**, and then the day it flags for real nobody looks. It is the same defect
this project hunts in code, applied to the tooling.
*Cost: it showed up on `git checkout main` after a merge — during development the files were born written in LF and
had never been re-checked-out, so the problem did not exist. The index was already in LF (`git ls-files --eol` →
`i/lf w/crlf`): only the working tree converted. Fix: `*.go text eol=lf` in `.gitattributes`.*

**And `*.go` is not the end of the list: every file READ BY A LINUX has the same pitfall.** With CRLF, `deploy.sh`
dies with `$'\r': command not found` and the systemd unit carries the `\r` into the `ExecStart` value — neither of them
says the problem is line endings. *Cost: none, and only by chronological luck — T-008's files were born in LF and were
run before any re-checkout, exactly like the `.go` ones above. Closed preventively with `*.sh text eol=lf` and
`*.service text eol=lf`. When you add a new type of system file (`.conf`, `.timer`, `.env`), add the line along with
it.*

---

## Infra (has not charged yet — recorded beforehand)

### 🔥 The credential renewer renewed nothing — and answered green (2026-08-17)

`implanta/cf-renova-tokens.sh` was born with a name that said **renew** and a filter that selected only tokens with a
**null** `expires_on`. That made sense on the day it was written: it existed to *put* an expiry on those that had
none. **The moment the three tokens got a deadline — which was its own work — the filter stopped matching anything,
and the script became inert.** The `--dry-run` started printing the section *"O QUE ESTE SCRIPT VAI ALTERAR"* **without
a single target line** and exiting `0`.

**The symptom does not exist, and that is what makes this one expensive:** the script is preventive, for a renewal in
**February 2027**. Somebody would run it under pressure, see all green, and the tokens would expire anyway — the outage
would arrive later, with nobody connecting the two.

**Why it passed:** the `Verify` of the task that created it (T-123) checked **syntax and text** — `bash -n`, `grep`
for phrases in the header. No check **exercised behaviour**, and a script with no target at all passes all of them.
*A verify that only reads the file does not distinguish "it works" from "it does nothing".*

**The three rules that remain:**
1. **An absence of targets is a FAILURE, never green.** An empty list is now `exit 1` with the reason. A loop that does
   not iterate is the cheapest silence to produce and the most expensive to discover.
2. **A selection criterion written on day zero describes day zero's world.** If the script changes exactly the field it
   filters by, the filter cancels itself out on the first successful run. Always ask: *what does this script do with the
   datum it uses to make up its mind?*
3. **A preventive script needs an exercise that can fail** — here, a fake API feeding the loop and a proof of the
   positive control with an invented token. It is the same question as the certificate pitfall, just below: *is my test
   capable of failing?*

*Real cost: no incident — the defect lived one hour (script delivered at 06:54 on 2026-08-17, found and fixed at 08:00
the same day). What was paid was a whole task (T-123) delivered as a guarantee and worth zero, plus T-124 to redo it.
**The avoided cost was in February 2027, and it would have been the expiry of the three Cloudflare account
credentials** — zone, tunnel and probe Worker.*

⚠️ **In the same file, two sibling defects, found the same day and fixed together:** the positive control wrote the
**token value in the clear** into a `mktemp` and only deleted it **after** the `curl` — with `set -euo pipefail`, a
`curl` that failed (the script's most likely failure path) killed everything **between the two lines** and left the
credential in `$TMPDIR` for an indeterminate time. The fix was not a `trap`: it was **eliminating the file**, sending
the config through stdin (`curl -K -`). *Patching the cleanup keeps the window; taking the disk out of the path closes
the whole class.* And the header promised **mode 0600** the script did not enforce — the mode came from `mktemp`, and
on Windows what rules is the temporary folder's ACL. **An unkept promise about a credential is a bug**: the sentence
was deleted, not fulfilled.

### 🔥 Selecting a target by NAME PREFIX is adopting what is not yours — and it nearly extended a 24 h token to seven months (2026-08-17)

With the defect above fixed, `implanta/cf-renova-tokens.sh` started selecting targets by `startswith("zapgw")`. It
looked like the opposite of a too-narrow filter, and for that reason nobody looked again.

**The `--dry-run` against the real account showed what the logic said:**

    ALVO: zapgw-teste-tokenwrite-24h  expira=2026-08-17T23:59:59Z -> 2027-03-31T23:59:59Z

That was a **test** token, of **24 hours**, created that morning to prove the script itself worked — and with
`API Tokens Write`, that is, **the most powerful credential in the account: it creates any other token**. The tool that
exists to *improve* credential hygiene was going to extend the worst possible credential from 24 hours to seven
months, silently and with the appearance of success. The real run was aborted because of that line, before the `PUT`.

**The rule: a name is a free-text field, and a selection criterion cannot be a field anybody writes.** Whoever creates
a token chooses the name; a `zapgw-*` in the account is not proof that it is ours, that it is permanent, or that
anybody wants it to last. The target became an **explicit list** inside the script (`CONHECIDOS`), which is the **same**
list as the proof files — on purpose: *"what I renew" and "what I can prove afterwards" have to be the same set.*

**And the unknown is not ignored: it is REPORTED** (block `NAO SAO MEUS, e nao vou tocar`, with name, id and expiry).
The two easy ways out were wrong in opposite directions — **adopting** causes today's damage, **staying silent** lets a
temporary token go unnoticed in the future. Reporting is the only one that does not choose.

**A sibling defect, from the same root, fixed together:** the name→proof-file map knew three names, so a target adopted
by prefix had no file, and `provar()` killed the script — **after** it had already done `PUT`s on the previous ones. *A
credential tool that discovers it cannot validate **after** mutating leaves a partial application and an operator with
no idea where it stopped.* Now **all** the proof files are resolved **before the first `PUT`**; if one is missing, it
exits `≠ 0` without having changed anything.

🔴 **And the lesson about the VERIFY, which is the one that repeats throughout this file:** this defect went through
**two** green verification rounds — `bash -n`, greps for phrases, and even a behavioural exercise with a stubbed
`cf()`. **The exercise did not have an intruder in the fake JSON**, so there was no way for it to distinguish "selects
correctly" from "selects everything". *A proof that does not contain the dangerous case is not a proof.* And in proving
that **nothing** was mutated, the "zero PUTs" is only worth something alongside a control in which the same counter
**moves** — otherwise it is a blind counter answering what one wants to hear.

*Real cost: no incident — caught by the `--dry-run`, which is exactly what it is for. What was paid was the whole real
validation round aborted and T-125 to redo the selector. **The avoided cost would have been a token with
`API Tokens Write` alive for seven months without anybody knowing**, created as a 24-hour throwaway.*

### 🔥 "Measured on `<date>`" that the record of that very day does not support (2026-08-20)

The header of `implanta/cf-renova-tokens.sh` asserted, in the section *A CHAVE GLOBAL NAO E' NECESSARIA*:
*"Measured on 2026-08-17, with a real `PUT` followed by a check of the permissions."* The sentence had **no
measurement behind it**. The record of that same 17/08, two pitfalls above, says the real run of that day was
**aborted before the `PUT`** by the prefix selector, and both rounds used the **global key**.

**The conclusion was true; it was the provenance that was false.** That is what makes the defect hard: there is no
symptom. Nothing breaks, nobody trips, and the sentence survives every review because *it is right*.

**And it spread, the way a good assertion tends to spread:** from the script's comment into the deployment runbook (kept in the private repository), and
from there into `C:\dev\github\docs\CREDENCIAIS-DE-API.md` — which serves **every** project in the workspace. By the
third copy it had grown an extra leg: *"creates and renews tokens"*. **Creating (`POST /user/tokens`) was never
measured by anybody**, neither on 17/08 nor on 20/08. An assertion without provenance grows as it is copied, because
each copy inherits the authority and none inherits the evidence.

**Measured for real on 2026-08-20**, and the script is written into all three texts precisely so it can be repeated:
`GET /user/tokens` → `GET /user/tokens/<id>` → `PUT` pushing the expiry out → **an independent reread** (the `PUT`
answering `200` proves nothing) → comparison of the `permission_groups` before × after → restoration of the original
date. Single target: `zapgw_conf_tunnel`, the only `conf` with no automated consumer.

**The rule, which holds well beyond credentials:** *a measurement claim carries **what was exercised**, not just the
date.* If the reader cannot repeat the measurement from what is written, do not write "measured" — write what you
actually saw, or write nothing. It is in the workspace doctrine (`C:\dev\github\docs\DOCUMENTACAO.md`, *Regras de
escrita*).

*Real cost: three days with an unauditable "measured", the measurement redone from scratch on 20/08, and an assertion
false by provenance published in the doc that serves every project. Cheap here because the conclusion happened to be
right — **and that is exactly what makes it dangerous**: the same defect on an API limit, a capacity number or a
resend window is a decision taken on top of nothing.*

### 🔥 TWO defects that would have broken EVERY certificate renewal — found only because issuance was FORCED (2026-08-06)

In migrating the `tenant-one.com.br` zone to Cloudflare, Traefik restarted **active, without a single error in the
log**, and every site answered. It looked done. **It was broken in two places, and the symptom would only arrive in
November**, when the wildcard certificate expired — three months after the cause.

**Both only showed up because the rule was "restarting does not prove issuance".** The proof: a throwaway router for a
hostname with no certificate, forcing a real issuance on the same day.

**Defect 1 — the wildcard CNAME poisoned the ACME challenge.**
`*.tenant-one.com.br` was a `CNAME → casa`. **A DNS wildcard matches at ANY depth** — therefore
`_acme-challenge.<anything>.tenant-one.com.br` also returned that CNAME. lego followed it to `casa` and wrote the
challenge in the wrong place.
🔴 *The reasoning error that delayed the diagnosis: I asserted that "a wildcard covers one level". **That is a TLS
certificate rule, not a DNS one.** In DNS, `*.exemplo.com` matches `a.b.c.exemplo.com`.*
**Fix:** the wildcard becomes an **A** record for the same IP. It resolves identically and is not a CNAME.

**Defect 2 — lego checked propagation against the LAN's resolver.**
`NS <resolvedor-da-rede>:53 did not return the expected TXT record`. The machine's resolver is internal and cannot see
the public zone (split-horizon), so the TXT **was right at Cloudflare** and the check said it was not. **Fix:**
`resolvers: ["1.1.1.1:53","8.8.8.8:53"]` in the `dnsChallenge`.
🔴 *And the answer was already in the SAME file: the `cloudflare` resolver had those lines; `letsencrypt` did not.
**Somebody had already hit this and fixed it on one side only** — and the asymmetry between two places that solve the
same problem IS the defect, as this document already said.*

**After the two: issuance in 15 seconds** (`trying to solve` → `The server validated our request` → `Server responded
with a certificate`).

*Cost: no incident — the entire migration was done with both defects active and nobody would have noticed until
November. **What caught them was not looking at the log: it was demanding an issuance that could fail.***

⚠️ **And the first test was worthless:** I used a ONE-level hostname, already covered by the wildcard certificate.
Traefik correctly issued nothing, and I nearly read that as a failure. **A test that cannot fail proves nothing** — it
took a TWO-level name to force a real issuance.

**The three questions that generalize:**
1. *Is my test capable of failing?* If the system can satisfy it with what it already has, it tests nothing.
2. *Is there a second place that solves this same problem?* If there is and it is different, the difference is the
   defect — not a coincidence.
3. *What is the interval between cause and symptom?* When it is measured in months, "no error right now" is no
   information at all.


### 🔥 The TLS escape hatch that stayed on forever — measured on 2026-08-06

**This project's `CLAUDE.md` forbids any path with an option not to verify certificates, and says why: *"the day the
option exists, somebody turns it on to unblock a demo or a test, and it stays on forever — silently, because switching
verification off generates no error at all"*. On 2026-08-06 that sentence was found happening, in this network, for
months.**

`consumer-b`'s tunnel delivered to `https://127.0.0.1:443` with **`noTLSVerify: true` on all SEVEN ingress rules**. The
reason it was turned on is obvious and legitimate: `127.0.0.1` does not match the certificate's name. The reason it
stayed is the rule's: **nothing fails when verification is off.**

⚠️ **And there was an aggravating factor that nearly made me fix the wrong thing:** the owner authorized changing the
zone's SSL mode to `strict`. **That would have been theatre** — on those hostnames the traffic goes
*edge → tunnel → cloudflared → origin*, and what rules verification on the last leg is the **tunnel's ingress**, not
the zone's configuration. `strict` on the zone would not touch the `noTLSVerify`.

**The right fix is `originServerName`**, which keeps the destination at `127.0.0.1` and validates the certificate
against the real name. *Proven BEFORE touching anything, from inside the Traefik LXC:*
`curl --resolve <host>:443:127.0.0.1` returned **`ssl_verify_result = 0` on all seven** — that is, Traefik was already
serving a valid and trusted certificate, and the escape hatch was **pure residue**.

*Cost: no incident — but for months the TLS guarantee on that hop was **theatre**, and nobody had any way of knowing.
Applied to all seven rules, one first and the others after the external proof; the seven hostnames answered the same
before and after, and the zone went to `strict` only AFTER the verification was real.*

**The two questions that generalize:**
1. *Where does TLS terminate on my path, and WHO decides whether it is verified on that leg?* — the answer is almost
   never where you look first.
2. *Is there any escape hatch switched on here that will never fail on its own?* — if there is, nobody will find it by
   accident. Only by measuring on purpose.


**⚠️ This entry was NARROWED on 2026-07-28 (T-066): it described a topology that is NOT this gateway's.** The text
asserted, as fact about zapgw, that `cloudflared` delivers to Traefik over loopback and that Traefik therefore sees
every request as `127.0.0.1`. **There is no `cloudflared` on the public path of `zapgw.tenant-one.com.br`** — the name
resolves to the house's IP and arrives by port forwarding (measured; see the deployment runbook (kept in the private repository)).

What still holds, and is why the line was narrowed instead of deleted: **where there is tunnel delivery in this
network**, the real IP comes **only** from `CF-Connecting-IP`, and trusting `X-Forwarded-For` by Cloudflare IP ranges
— the usual recipe for anyone behind a reverse proxy — **is wrong**. *Cost: it charged in another project of this
network (it broke anti-brute-force and the OTP origin log). It affects any Meta IP allowlist and any per-origin limit
at the gateway.*
**Which IP Traefik sees on zapgw's public path was NOT measured** (it requires a shell on the Traefik LXC, node
`<no-proxmox>`) — do not assume either of them. Today nothing in the binary depends on it:
`grep -rn "RemoteAddr\|X-Forwarded-For\|CF-Connecting-IP" cmd internal` returns zero lines.
*The lesson this entry now carries is about DOCS, not about networks: **a conclusion imported from another system in
this same network looks confirmed — it has an ADR, it has a real cost and it has the name of a mechanism — and it is
still a guess about the topology here until somebody measures.***

**A route the binary serves but the proxy does not route answers `404` with everything right on our side — and no test
in this repo reaches that.** Routing in Traefik is by `PathPrefix`, and it lives in the LXC's Notes field, **outside
the repository**: a new route (`/v1/instances/…` in T-014, and the media and template ones in the next tasks) is green
in the suite, green on a `curl` against `127.0.0.1:8080`, and `404` for the consumer. The `404` is indistinguishable
from "this route does not exist in this version", so the consumer concludes the gateway is old and nobody looks at the
proxy.
*Cost: none yet — the gap was recorded in the deployment runbook (kept in the private repository) in the same commit that created the route. **When you
add a new route, the router rule goes into the same task**, and the check is a `curl` on the real hostname and port,
never on the LXC's `127.0.0.1`.*

**A certificate that does not get issued makes the webhook fail SILENTLY on both sides.** `tenant-one.com.br` has a
wildcard on the entrypoint (a new subdomain issues nothing); `consumer-b.com.br` **does not**, and requires
`certresolver=cloudflare`. The wrong resolver in the label = the cert is never issued, Traefik answers with the
default, and Meta simply does not deliver — with no error anywhere on our side.
*Validating a new domain means **checking the certificate's issuer from outside the network**, not "it answers 200".*

**The probe existing is not the same as the probe RUNNING — and the difference silenced the alarm through an entire
outage.** `implanta/sonda-publica.sh` was written in T-073 (2026-07-28) and spent nine days with nowhere to be run
from outside the LAN. On 2026-08-06 the house's fixed-IP link went down, the public path was off the air for ~9
minutes, and the four monitors that existed (journal, Meta subscription, counters, `/v1/health`) all stayed green
through the whole outage — exactly as the deployment runbook (kept in the private repository) had predicted in writing since T-073. **The one who
warned us was the consumer.** *Cost: no proven message loss (Meta requeues for 36 h), but nine days in which the only
instrument capable of flagging this existed in the repository and ran nowhere.* The fix in T-117 put the script on
GitHub Actions — **and the home did not stick**: two triggers, both `cancelled`, an empty `runner`, ~15 min flat on
both, with the quota measured and discarded as the cause. T-119 (2026-08-06) moved the home to a Cloudflare Worker
with a Cron Trigger (`sonda-worker/`). **The lesson that generalizes: a detection instrument with no scheduled place
to run is documentation, not protection — and the gap shows up in no test, because the suite also runs from the
inside.**
🔴 **And the second lap of the same lesson, inside T-119 itself:** the Worker was published successfully to the
account and **the Cron Trigger did not go in** (the account had no `workers.dev` subdomain; the API refuses with
`code: 10063`). *`wrangler deploy` said `Uploaded` — the line that failed came afterwards.* **A published Worker with
no trigger is exactly the same defect in another costume: code in the right place and nothing executing.** The pending
item is written in the deployment runbook (kept in the private repository), section *Onde ela roda*. *Check the trigger, not the upload.*

**"No response" classified as a BLIND MONITOR would make the probe lie on the exact day it was made for.** When
porting the probe to the Worker (T-119), the first version followed the obvious reading — *the `fetch` threw,
therefore I am blind*. But the 2026-08-06 outage (link down, `:443` refusing connections) produces **exactly** a
`fetch` that throws: the alarm would ring saying *"this does not prove the gateway is down"* with the gateway down.
**The right boundary was already in `implanta/sonda-publica.sh` since T-073** — there, an absence of response is exit
`1`, RED — and it means *"I could not measure"*, not *"the target did not answer"*. The Worker separates the two with
an **independent control**: mute target + control answering = RED; both mute = BLIND. *Cost: none — caught in the
review of the port itself, before the cron existed. **The lesson: when porting an instrument, port the SEMANTICS, not
the format.** Two implementations of the same monitor that classify the same event differently are worse than one,
because whoever reads the second believes they have read the first.*

**`cacheTtl: 0` does NOT switch off Cloudflare's cache — it orders caching and immediate expiry.** The doc
(`developers.cloudflare.com/workers/runtime-apis/request/`, read on 2026-08-06) is explicit: `cacheTtl` *"forces
Cloudflare to cache the response for this request, regardless of what headers are seen on the response"*, and `0`
means *"the cache asset expires immediately"*. What switches it off is `cacheTtlByStatus` with a **negative** value —
*"Any negative value instructs Cloudflare not to cache at all."* *It never charged here: the defence that actually
holds the probe's false green is **structural** — the slug varies on every run, and a URL that never existed is in no
cache. Recorded because the temptation is to write only `cacheTtl: 0` and think you are covered, and the symptom
would be the probe going GREEN without touching the gateway — after the DNS migration, when the hostname is proxied by
the same account.*

---

## systemd

**`systemctl restart` returns 0 with the service dying right afterwards.** With `Type=simple` systemd only promises it
executed the `ExecStart`; it waits for nothing. In T-008's rollback test the `restart` exited **0** while the binary
died in 5 ms, and systemd raised it 13 times in 30 s — all of that with the deploy "successful" from `systemctl`'s
point of view. **That is why the deploy's verdict is `/v1/health`, not the `restart`'s exit code.** *Cost: zero,
because the health requirement existed from the first deploy. Without it, a broken binary would have stayed in
production with the gateway mute and nobody would know — Meta simply stops delivering, and from outside that is
indistinguishable from "no message arrived".*

**`Restart=always` + `StartLimitBurst` can make the ROLLBACK's `restart` be refused.** A binary that dies immediately
blows the limit in seconds, and from then on systemd answers *"start request repeated too quickly"* — what fails is
precisely the path that exists to fix things. That is why `deploy.sh` runs `systemctl reset-failed zapgw` before every
restart. *Cost: none yet — in T-008's proof the counter reached 13 without blowing (`RestartSec=2s` against the
default 10 s window puts the case right on the boundary, and the boundary depends on start-up time). A guard that
only fails "sometimes" is worse than one that always fails: it vanishes from the test and shows up in the incident.*

**`command not found` inside the CT does not mean the binary is not there — it means you came in through `pct`.**
`pct enter` / `pct exec` give `PATH=/sbin:/bin:/usr/sbin:/usr/bin`, **without `/usr/local/bin`**, which is where
`deploy.sh` installs `zapgw`. Only a *login* session (ssh, `su -`) loads the `/etc/profile` that completes the `PATH`.
**And systemd's env does not come along either**: an interactive shell has neither `ZAPGW_BANCO` nor
`ZAPGW_CHAVE_CIFRA`, so the menu opens and prints `resumo indisponivel:` while every subcommand fails to open the
database — two different symptoms of the same cause, and neither of them says "load the env". *Measured on
2026-07-29, when the owner typed `zapgw menu` in the CT and took a `command not found` with `v0.31.0` installed and
healthy. Cost: one trip to the chat. What makes it expensive if it recurs is the disguise — `command not found` sends
you looking for a failed deploy, and the deploy was perfect.*

✅ **Fixed the same day** by `<gateway LXC>:/etc/profile.d/zapgw.sh` (plus the `source` in `/root/.bashrc`, because
`pct enter` does not read `profile.d`), which defines `zapgw` as a **function**. **The lesson that survives the fix is
about design, not about the command:** fixing only the `PATH` would have fixed the first absence and left the second —
**trading `command not found` for `resumo indisponivel:` is trading a clear error for an obscure one**, and it would
have looked like a fix. *Two absences with the same cause need the fix that catches both, otherwise the second only
shows up later, without the context that explained it.* The reason for each line is in the deployment runbook (kept in the private repository), *O que
faz `zapgw` funcionar venha você de onde vier*. ✅ **T-090** versioned the file at `implanta/profile-zapgw.sh` and
`deploy.sh` started installing it on every deployment — a new CT, or one rebuilt from scratch, is born with the
problem already solved.

---

## SQLite

**`REFERENCES` in the schema is DECORATIVE without `PRAGMA foreign_keys = ON`.** SQLite accepts the clause and does
not enforce it. The consumer↔instance link table declared two foreign keys and accepted
`CriarConsumidor("fantasma", token, []string{"slug-que-nao-existe"})` without an error — a typo in provisioning would
become an orphan link authorizing an instance nobody registered.
*Cost: one Critical. It is false doc in the shape of a schema — the declaration promises integrity the database does
not deliver.*

**And the pragma goes in the DSN, NEVER in a `db.Exec` after opening.** `database/sql` keeps a **pool**: a `PRAGMA`
executed by `Exec` holds only for the connection that served it. Proven by mutation — reverting to `db.Exec`, out of
**30 concurrent goroutines** attempting the orphan link, **only one passed**: the other 29 happened to get the
connection that had run the `Exec`.
*It does not fail — it works WRONGLY, depending on which connection the pool hands over. A sequential test never
catches it; in production it shows up only under load, intermittently.*

**Without `busy_timeout`, SQLite returns `SQLITE_BUSY` IMMEDIATELY instead of waiting for the lock.** Under 60
concurrent requests on the same idempotency key, the central guarantee held (only one reserved) but **58 came back
with a database error** — a **fourth outcome** the three-case contract did not foresee, appearing precisely in the
scenario idempotency exists to handle: a burst of simultaneous retries.
*Cost: one Important, found before the HTTP handler was written against the wrong contract. Fix: `busy_timeout` and
`journal_mode(WAL)` in the DSN.*

**And `busy_timeout` does NOT cover the case where the transaction has already READ before trying to WRITE — it only
covers waiting for a lock.** `RemoveInstance` (`internal/config/store.go:1490`), `RegisterMeta`
(`internal/config/store.go:1072`) and `ClearTransitByPhone` (`internal/config/transito.go:360`) open a `deferred`
transaction, READ (`SELECT`/`SELECT ... GROUP BY`) and only then WRITE within the same transaction. If another
connection commits a real write in that interval, the read snapshot the transaction already holds goes stale, and the
attempt to become a writer aborts — and that happens immediately, not after waiting the configured 5000 ms:
reproduced deterministically (a connection pinned with `db.Conn`, a read, a write committed by ANOTHER connection, and
then the first one's write) the error arrives in **0 s** with `Code()=517` (`SQLITE_BUSY_SNAPSHOT`), literal message
`database is locked (517)`. Under realistic concurrency (12 goroutines calling `RegisterMeta` at the same time) the
same abort also appears as `Code()=5` (plain `SQLITE_BUSY`, with no sub-code) in 11–23 ms — the same error family (the
transaction had already read and lost the race to become a writer), except that SQLite does not always get as far as
marking the snapshot sub-code. In both cases the `busy_timeout` was ignored: SQLite documents that this path **does
not invoke the busy handler**, because waiting again would not help — whoever lost would have to restart the whole
transaction, and the driver does not do that by itself. *Measured consequence: noisy, not silent — the call comes back
with "database is locked" instead of hanging or corrupting something.*

*Cost: none in production yet — found as a side effect of T-131 (2026-08-18) and confirmed by a dedicated diagnostic
in T-132, not by an incident. **The gravest hypothesis that motivated T-132 — that this error would "poison" the
pool's connection, making every subsequent call fail forever — was TESTED and NOT reproduced:** neither on an isolated
pinned connection (`db.Conn`) reused directly after the error (8 reads + 1 new write, all successful), nor on the
`*Store`'s real pool under a heavy burst (up to 10 out of 12 simultaneous calls failing) followed by 40 sequential
calls on the same `*Store` — all 40 succeeded, across five repetitions of the burst. The `modernc.org/sqlite` driver
does not mark the connection as invalid on this path (`ResetSession`/`IsValid` only check `sqlite3_is_interrupted`,
not the transaction state), and the aborted transaction's `Rollback()` really does close the SQLite transaction
(checked without error in every repetition). **No fix was made in this task** — retry, `BEGIN IMMEDIATE` or a pool
change are design decisions, and are left for a task of their own if the owner decides the "noisy" behaviour above is
worth resolving.*

🚫 **DECISION (2026-08-18): evaluated and REFUSED — there will be no retry. This is not an oversight.** T-133 existed
to decide exactly this, and the outcome was *do not do it*. It is written here because a task that vanishes from the
queue without a trace is indistinguishable from a lost task — and three months from now somebody would reopen the
subject from scratch. The reasons, in order of weight:

1. **Where there is an instrument, it never happened.** CT 125's journal covers **24 days** (since 25/07) with **zero**
   `database is locked` — and zero `erro de store` of any kind. `RegisterMeta` is the only one of the three that runs
   inside the daemon and logs store errors (`internal/outbound/cadastro_handler.go:334`), so **for it the zero is
   proof**.
2. ⚠️ **For the other two the zero is NOT proof — and that is written here on purpose.** `RemoveInstance` and
   `ClearTransitByPhone` run in the **CLI**'s process, which dies with a `log.Fatalf` against the terminal of whoever
   typed it (`cmd/zapgw/main.go:241`). The error **never reaches the journal**. There, zero is *an absence of
   instrument*, not evidence — it is the blind-monitor pitfall in different clothes. What closes the gap is the
   owner's answer, asked directly on 2026-08-18: he has **never seen** `database is locked` running
   `zapgw instancia remover` nor `zapgw log clear --telefone`. *That is testimony, not measurement, and it is noted as
   testimony.*
3. **The failure is noisy, and its price is one manual repetition.** The caller gets an error; nothing is left
   half-done (the `Rollback` was checked). In the CLI commands there is a person in front, who is exactly the one able
   to run it again. And `POST /v1/cadastro` answers `503 retentável` — the consumer **already** retries by contract.
4. **The hypothesis that would justify doing it even without an occurrence was knocked down.** It was the connection
   poisoning of the paragraph above: if an abort locked the pool forever, "rare" would not be enough as a defence. It
   was tested and did not reproduce.
5. **Retry has a cost of its own, and it is not zero.** A helper that repeats a whole transaction is a pattern the next
   reader copies — and it is only safe *here*, because these three abort with no partial effect. Copied into a place
   with a partial effect, it duplicates the effect. Introducing the pattern for a problem that does not manifest is
   creating the pitfall before having the benefit.

**The trigger that reopens it, and it is objective:** the first observed occurrence of `database is locked` — in the
daemon's journal, or reported by whoever ran a CLI command. The task then does not need rewriting: T-133's `Do` and
`Verify` were already complete (a helper matching by `Code()` 5 or 517, **never** by text; a small retry ceiling; and
the control test requiring `ErrInstanceActive` to come back on the **first** attempt). They are in git's history, in
the commit that retired it.

🚫 **SIBLING DECISION, the same day: the `database/sql` pool stays WITHOUT an explicit limit — by choice now, no longer
by omission.** `OpenStore` calls neither `SetMaxOpenConns` nor `SetMaxIdleConns` (`internal/config/store.go`, see the
pragma block). For SQLite, more concurrent connections do not give more write throughput — they give more contention,
which is the cause of the aborts above. Even so we **do not limit**, for two reasons:

- **The measurement does not call for it.** `lsof` showed the daemon with two connections to the database in
  production, and `database/sql`'s default for `MaxIdleConns` is already **2** — that is, at rest the pool already
  behaves as if limited. What "unlimited" describes is only the peak under a burst, and the real measured burst is
  small (Meta resends 5 times in 9 s).
- 🔴 **And the fix would trade a NOISY error for a SILENT wait.** With a low `SetMaxOpenConns`, whoever finds no free
  connection **blocks** waiting for one — with no deadline, if the caller does not bring a `context` with one. This is
  exactly the opposite direction from the one this house chooses: the failure mode that has already cost dearly here
  is silence, not an error in your face.

**The trigger that reopens this one too:** (a) an observed `database is locked`, as above; or (b) **the port to
Postgres**, where the arithmetic is different and inverts — there the server has a global `max_connections`, an
uncapped pool per process is a real risk, and setting the cap becomes mandatory rather than optional.

**WAL creates neighbouring files that `.gitignore` does not catch.** `*.db` does not cover `zapgw.db-wal` nor
`zapgw.db-shm`.

**A new column inside a `CREATE TABLE IF NOT EXISTS` does NOT reach a database that already exists — and the
`IF NOT EXISTS` hides that.** The `hash_pedido` column was added to idempotency's `CREATE TABLE`. In a database whose
table already existed, opening the store passes clean (the table exists, so nothing runs) and **every send** starts
returning `503` with `table idempotencia has no column named hash_pedido`. Total failure, silent at start-up, and
visible only on the first send.
*Cost: none in production — no v0.2.0 database had the `idempotencia` table, so the `CREATE TABLE` created it already
correct. Fixed in `T-001` with a migration versioned by `PRAGMA user_version` (`internal/config/store.go`, `migrar`),
and the test that requires it failed **on an assertion**, not on compilation: the column really did not reach the
database that already existed.*

**And the migration mechanism itself brings two pitfalls, both proven by mutation:**

- **A migration that runs `ALTER TABLE` blindly breaks exactly the databases it exists to save.** Every v0.3.0
  database is at `user_version = 0` **and already has** the column (it was born inside the `CREATE TABLE`). Without
  consulting `pragma_table_info` first, the first start-up with the migration dies with `duplicate column name` — and
  it dies only in real databases, never in a test database created from scratch.
- **A migration outside a transaction leaves HALF a schema, which is worse than none.** Swapping the `BEGIN IMMEDIATE`
  for a no-op, the table created by step 1 survives step 2's failure and the database ends up in a state no version
  knows — start-up passes and the error shows up on the first write. And the `BEGIN`/`COMMIT` has to run on a
  **single** `sql.Conn`: through `database/sql`'s pool the `BEGIN` lands on one connection and the `COMMIT` may land
  on another. It is the same pitfall as the `PRAGMA` above, in different clothes.
- **A database newer than the binary has to REFUSE to start** (`ErrSchemaFromTheFuture`). An old binary does not know
  what the new one created: it would start with no error at all and write over it with the old rules. It is this
  project's favourite shape of damage — nothing flags it at the time.

**A new ENCRYPTED column brings down every row that already existed, because an `ALTER TABLE`'s `DEFAULT` is the empty
string — and an empty string is not a valid ciphertext.** Migration 3 added `instancia.bundle_ca` to the encrypted
set. In the rows already stored the value becomes `''`, and `Decifrar("")` fails with `cifrado curto demais` — so
`FindInstance` would start returning an error for **every** instance predating the migration. Since every request
begins by looking up the instance, the webhook and sending would die together, on the first call after the update,
with `OpenStore` having passed clean.
*Cost: none — caught by a test (a v0.4.0 database with an instance inserted by hand, reopened after the migration) and
confirmed by mutation: decrypting blindly, the test flags
`config: decifrar bundle de CA: config: cifrado curto demais`. Fix: `decryptOptional`, which treats a literally empty
column as "never registered" — the distinction is unambiguous because `CreateInstance` stores the **ciphertext of
`""`**, which is not `""`. **The question that saves the next one: "is this column's `DEFAULT` a valid value for
whoever is going to READ the column?"** — for a plaintext column it is always yes, for an encrypted one always no.*

**And the opposite holds too: `column != ''` is NOT "registered" in an encrypted column — the ciphertext of `""` is not
`""`.** T-020 was born with the instruction, written into the task itself, that presence would be `column != ''`. But
`CreateInstance` encrypts all **six** fields, including those that arrived empty: the `callback_url` of a send-only
instance is a perfectly valid ciphertext of an empty string. Under the literal rule, `tenant-one` — the only instance
in production — would show up with a **registered** `callback_url`, that is, delivering to a consumer that does not
exist, and somebody would go looking for the defect in delivery instead of on the screen. And **both** forms of empty
coexist in the same database: that one, and the literally `''` column that migration 3's `ALTER TABLE` left in
`bundle_ca`.
*Cost: none — caught by mutation before the commit (swapping the check for `!= ""`, five tests go red). Fix: ask the
ciphertext's **length** (base64 of `nonce+overhead` is exactly the ciphertext of empty), which answers without
decrypting and without depending on the key. **The question that saves the next one: "in how many different ways is
this column's 'empty' value written?"** — and it is a sibling of the entry above, which asks whether the DEFAULT is
valid for whoever is going to READ.*

**And the compatibility branch that "handles the old record" was dead code with a false comment.** The code treated a
stored empty hash as equal, "so as not to break a record predating the column" — but at the time, with no migration,
that record **could not exist**, and the handler never passed an empty hash. The comment asserted a non-existent
mechanism, and a green test asserted it covered the scenario: it exercised a call no production path makes.
*Compatibility written "just in case", without the path that produces it, is false doc inside the source — and it even
gets a test that gives it the appearance of being proven.*

**The third form of the same family, and the easiest not to see: the `ALTER TABLE`'s `DEFAULT` is a VALID value for
whoever reads it, and it is still the wrong answer.** The two entries above ask whether the default *breaks* the read
(an encrypted column) or whether it *gets confused* with a legitimate value (the two forms of empty). T-070 added
`instancia.carimbos_desde` — the instant from which that instance stores counter stamps — and there the `DEFAULT ''`
breaks nothing: it deserializes, travels in the JSON and reaches the consumer's dashboard as an **empty field where
they expect an instant**. Nothing flags it anywhere, and the field is born useless precisely in the instances it
exists to explain: the ones that already existed before the stamp existed. *Fix: the migration does the `ALTER TABLE`
**and** an `UPDATE ... WHERE carimbos_desde = ''` with the instant it runs — and the `WHERE` is what makes it safe to
run again. Proven by mutation (only the `ALTER`, without the `UPDATE`):
`TestTheMigrationFillsStampsSinceOnAPreExistingInstance` (`internal/config/store_test.go`) goes red with the field
empty.* **The question that closes all three: besides "is the `DEFAULT` readable?" and "does it get confused with a
real value?", ask "is it an ANSWER?"** — a column that exists to answer a question needs a DATA migration, not just a
schema one.

*And the value chosen for the old rows is a decision, not a detail: they receive **the instant the migration runs**,
which may be later than the truth (the instance may have been stamping for days). Erring towards later is the safe
side — the reader starts treating as "I do not know" a range in which there may have been stamps, and never the
opposite. The opposite error (asserting coverage that did not exist) is exactly the defect the field exists to
close.*

**And the half the test almost fails to prove: a "per-instance" field with ONE instance in the test is
indistinguishable from a global constant.** `carimbos_desde` comes out in RFC3339 **without fractional seconds**, so
two instances created with `time.Now()` in the same test receive the **same text** — and the test passes green over an
implementation that returned a compiled constant for everybody, which is literally the defect. *Fix:
`CriarInstanciaEm(i, agora)` exists so the instant can enter by parameter, and
`TestStampsSinceIsPerInstanceNotAConstant` creates two 72 h apart. Mutation: storing a constant in place of
`carimboDe(agora)` leaves the test red saying "both instances answer X — this is a global constant".* **The question
that generalizes, and it is the same one T-072 paid for on the same day (section *Clocks and stamps*): a test that
depends on two values being DIFFERENT has to force the difference at the granularity at which the datum is STORED.**

**`T-001` inverted that fact, and that is why fixing one place forces you to reread the other:** the migration adds
`hash_pedido` with an empty `DEFAULT` to rows **already reserved**, so the record with an empty hash **came to
actually exist**. The comparison stays raw on purpose (`internal/config/store.go`, `ReserveIdempotency`): a retry of
those rows, made after the update and within the 72h TTL, gets a false `422`. The alternative would be to let **any**
different request escape the guard whenever the stored hash was empty — the **silent** outcome it exists to prevent,
and one that does not expire in 72h.

---

## Validation

**Presence is not content.** This plan had **four** occurrences of the same defect, in different files, all in the
form `!= ""` or `len() == 0`:

| Where | What got through | Consequence |
|---|---|---|
| the id returned by Meta | `"   "` | a useless `wa_message_id` stored in a record that **looks** sent |
| `responder_a` | `"   "` | a `context` with a blank `message_id` — a quotation of an id that does not exist |
| button | `{ID: "", Titulo: "Sim"}` | a broken `reply` that Meta would reject, discovered in production |
| the sibling fields of `Validar` | `para`/`texto`/`template`/`idioma`/`botao_*` with whitespace only | a blank request accepted and sent to Meta |

**Trim before deciding, and return the trimmed value** — otherwise the spaces travel to Meta.
*Cost: one Important and three gaps, all found by adversarial review, none by reading.*

**A NEW field in `Pedido` without `omitempty` changes the hash of EVERY old request — including those that do not use
the field.** `RequestHash` serializes the whole struct, and the hash is **stored in the idempotency database with a
72h TTL**. A field without `omitempty` adds `"botoes_url":null` to any request's JSON, so every legitimate retry of an
already reserved request starts matching against a different hash and gets a **false `422`** — the outcome the section
above calls "worse than not having the guard", now triggered by a struct tag. Nothing flags it at the time: the suite
stays green, the build passes, and the damage only shows up in the retries of requests that were already in the
database when the version shipped.
*Cost: none — T-021 was born with the three new fields in `omitempty` and with a golden hash captured from the
previous implementation. Proven by mutation: removing the `omitempty` from `botoes_url`, the hash of a template that
**only uses `variaveis`** changes from `c27bcc65…` to `d14dd34d…` and the test goes red. **The question that saves the
next one: "does this field enter the `RequestHash` of those who do not use it?"** — and the answer is only "no" while
the `omitempty` is there.*

**Validate with the SAME function that sends, otherwise validation and sending disagree.** `Validar` called
`meta.Canonizar(p.Para)` to **decide** whether the number had the digit and **threw the result away**. Sending
canonicalized again, so `"+55 (32) 99999-0000"` and `"5532999990000"` went out identical on the wire — and produced
**different request hashes**, because the hash is over the validated request. A legitimate retry whose phone
formatting varied would get a false `422`, which is precisely the outcome the entry above calls "worse than not having
the guard".
*Cost: caught in the re-review, before the merge. Fix: **assign**, not just compare (`p.Para = meta.Canonizar(p.Para)`).
Checking with the function that sends closes the asymmetry by construction; checking with a rule of your own ("it must
have N digits") reopens it the day one of the two changes.*

**A guard has TWO sides, and fixing only the one that hurt opens the other.** The base64 refusal was fixed **three
times**:

1. `HasPrefix(texto, "data:")` — refused `"data: 23/07"`, a spelled-out date in Portuguese;
2. `+ Contains(";base64,")` — closed the false positive and **opened the false negative**: case-sensitive and
   requiring position zero, it let `DATA:...;base64,` and `"veja: data:...;base64,"` through;
3. a case-insensitive search, at any position, requiring `;base64,` **after** the `data:`.

*Cost: one Critical on the third round. **A narrower guard lets through the send that fails silently; a wider one
breaks legitimate conversation — and both failures land on the end customer**, not on whoever wrote the guard. Write
the tests in PAIRS: one requiring refusal, one requiring acceptance.*

🔥 **When the error the gateway RELAYED did not name the field, the ceiling had to be checked at the INPUT — the
diagnosis that reached us was of no use.** The `botao_titulo` of `cta_url` (the button's `display_text` in the Cloud
API) had no ceiling at all in `Validar()` — only the already existing empty guard. On 18/08/2026 consumer `consumer-b`
sent a `botao_titulo` with more than 20 characters and Meta answered `(#131009) Parameter value is not valid`, and
what reached the consumer was **only that — without saying which of the request's parameters was the culprit.**
The message went out **without the button** to the end customer, and the diagnosis only closed by **manual bisection
on a test number** (17 passed, 21 failed, then 19 and 20 confirmed the exact ceiling). T-139 moved the ceiling to the
input (`limiteBotaoTituloCTAURL`, `internal/outbound/mensagem.go`) — but the number is not documentation: the official
Cloud API reference is **silent** about that limit, and the code comment says so on purpose instead of feigning
certainty about a value only a third party's handset measured.

🔴 **Correction (T-141, 2026-08-20): the formulation above, written in this very entry, treated "without saying which
field" as a fact about Meta — and that was never checked.** What was true is narrower: the gateway
(`ClassifyResponse`, `internal/meta/errors.go`) read only `error.message` and `error.code` from the Graph API's error
body, on purpose (the rest of the body, `error_data`, can echo the phone number and message text of the sent
request). The missing field was in OUR reading, not necessarily in Meta's response. There is evidence that it names
the field and the ceiling in `error_data.details`: the same code `131009`, found by the consumer in their own database
on **18/07/2026**, over ANOTHER transport (when they still talked to Meta through Evolution), came as
`"(#131009) Parameter value is not valid — Button title length invalid. Min length: 1, Max length: 20"` — field named,
ceiling declared. T-141 started reading `error_data.details` (only that key, truncated at 500 runes) and relaying it
in `detalhe_meta`, a separate field in the error body. **What remains open:** whether Meta still sends that field over
today's path (the direct Cloud API call the gateway makes), or whether that July text was a peculiarity of the old
transport. That only closes with a measurement against real production, after the deploy — T-141's test suite proves
the relaying against a synthetic body, nothing beyond that.
*Real cost: one customer message delivered without its call-to-action button (18/08/2026), one round of manual
bisection on a handset to find a ceiling the arriving error did not name, and — only discovered afterwards — a
diagnosis that had been one SELECT away since July.*

**The same anonymous `(#131009)` holds for the neighbour.** On 2026-08-20, a measurement of ours (no longer a third
party's) against Meta's real production, on the `tenant-one` instance, with messages actually sent: the `titulo` of
`botoes[]` (the quick-reply button's `reply.title`) has the **same ceiling, 20**, and the count is also per **RUNE**,
not byte — 20 accented characters (`ç`, 40 bytes) passed, 21 plain characters (21 bytes) were refused with the same
anonymous error. T-140 moved that ceiling to the input (`quickReplyButtonTitleLimit`,
`internal/outbound/mensagem.go`, in a constant of its own — the two fields are different Cloud API endpoints and Meta
may change one without the other). Consumer `consumer-b` has 7 approved labels in the catalogue between 21 and 25
characters that today would fail in free text without this guard.

---

## Contract — obligations the consumer fulfils wrongly by reading correctly

**A guard written over the SHAPE of the data breaks when the shape changes; a guard written over the RESULT OF THE
WORK does not — and two independent consumers arrived at that on their own, which is what makes the rule
trustworthy.**

On 2026-07-28 the gateway normalized `eventos` from `null` to `[]` (T-067). Before shipping, the implementer went to
read the consumer's channel and saw `isinstance(eventos, list)`: with `null` that is **false** and the batch fell into
the branch that **stores the `cru`**; with `[]` it would become **true** and enter the `for`, which runs zero times.
If that branch did not store, the account webhook would stop leaving a trace — **silence, not an error**, and working
today *because of* the defect we were fixing.

We asked both consumers and held the deploy. Both answers came citing a line, not memory, and **both described the
same solution with different names**:

| consumer | the guard | the question it asks |
|---|---|---|
| `consumer-b` | `if not itens:` | *"is there any event left that I can identify?"* |
| `consumer-a` | `if not algum_evento_valido:` | *"did I manage to address anything?"* |

Neither of them reads the **type** of `eventos` to decide the branch. Both decide on what the loop **produced**. And
both cover a case our own initial suggestion (`if not eventos:`) **did not** cover: a batch with events, all with an
empty or oversized `id` — a non-empty list, zero addressable, and the guard fires just the same.

*Cost: zero, and that is the point. The deploy sat still for 40 minutes and the answer was "everything is fine". **The
question was worth it with that being the answer** — without it nobody would know whether it was right by design or by
luck, and the difference between the two only shows up at the next format change.*

**The hierarchy, from worst to best**, because all three appear in this project:

1. **by TYPE** (`isinstance(x, list)`, `x is None`) — ties the code to the representation, which is exactly what
   changes when the producer evolves;
2. **by CONTENT** (`if not x:`) — better, covers `None`/`[]`/absent at once, but still asks about the **container**;
3. **by RESULT OF THE WORK** (`if not itens:`) — asks what the code really needs to know: *"did I manage to do
   anything useful with this?"*. It survives a new format, a malformed item in the middle and a field Meta invents
   tomorrow.

**On convergence counting as proof — it counts for LESS than this file once asserted, and the one who knocked it down
was one of the two convergers.** The original version of this entry said that *"two independent implementations
arriving at the same shape is the right shape, and it counts as evidence the way internal review never does"*.
`consumer-b` replied, the same day, with the missing caveat: **both consumers are Python and read the SAME contract**
— ours. **A common cause is not independence**, and the chance of their converging because they read the same text is
not small. The original sentence overestimated its own evidence, which is the elegant version of a wrong doc.

***What actually carries the proof is the TEST, not the coincidence:*** the two cases `if not eventos:` would let
through (`[{"foo":1}]` and `["texto", 3]`) are covered on both sides, and the result is a stored row with the `cru`.
**The question that remains, and it is reusable: before treating convergence as evidence, ask what the two parties
have in common upstream** — same language, same doc, same contract author. What is left after discounting the common
cause is the real evidence, and it is usually much less than it looked.

*Credit to both consumers; the formulation "USEFUL content, not just any content" is `consumer-b`'s, the oversized
`id` counterexample is `consumer-a`'s, and the caveat about the common cause is `consumer-b` knocking down our own
conclusion.*

---

**The SAME name for different things is worse than different names for the same thing.** This project had the second
rule written and policed (*"sending and receiving with different names for the same thing is the beginning of two
vocabularies"*, T-024) — and for that reason did not see the first one coming.
*T-044 was going to create `botoes` for a template button parameter, shape `{indice, tipo, payload}`. `Botoes []Botao`
with the tag `json:"botoes"` **already existed** (`mensagem.go:233`), shape `{id, titulo}`, for the interactive
message variant. Same key, two shapes, disambiguated only by the message's `tipo`.*
**And it was not merely cosmetic:** two `json:"botoes"` tags at the same level make `encoding/json` **ignore both**.
*Cost: zero — caught by consumer `consumer-b` reading the contract before planning on top of it, with the task already
in progress. It was not review from here, and review from here had read that stretch of the contract dozens of times
that day.*
**Why the first case slips through and the second does not:** different names for the same thing **bother the
reader** — the person sees both and asks which to use. The same name for different things **bothers nobody**: each
reader finds one of the two, understands it, and moves on. The collision only shows up for whoever reads the whole
contract at once, which is rare — or for the compiler, too late.
*Practical rule: before naming a new field, `grep` the JSON key in the contract AND in the code. It costs ten seconds
and is the only defence against the whole class.*

**A warning in the contract does not travel with the data; a field NAME does.** `serie_7_dias[].dia` was always UTC —
and our defence against the consumer reading it in the wrong timezone was a paragraph in the contract asking them to
*"write UTC on their screen"*. `consumer-a` disagreed with the **remedy**, not with the diagnosis (2026-07-28, with
the route ONE day old): *"it puts the guard in the consumer's intention, and a new consumer does not read the channel.
A field name travels with the data all the way into the `console.log` of whoever is debugging at two in the morning."*
*And they proved it with the very bug they were fixing that day: somebody wrote in the docstring "do NOT touch this
field, it is the easiest point to get wrong", the guard stayed in the warning, and the column's `default=` stored the
wrong value anyway — **it cost undelivered budget and two burnt resends**. It is this file's mother pitfall in
different clothes: the rule existed in writing, and did not exist in the place the data passes through.*
*Cost here: **zero**, and only because of the window — the route was one day old, neither consumer had a reader in
production, and that is why the fix fitted as an ADDITION (`dia_utc` next to `dia`, same value) instead of a contract
break. A week later it would have been the second thing, and "renaming" is the owner's decision, not ours. **The
window to change a name is while nobody reads it**, and it closes by itself.*
**The question that generalizes: when you write a warning in the contract about HOW to read a field, ask whether the
warning fits INSIDE the name.** Unit, timezone, scale and currency almost always fit — and the name is the only part
of the documentation the consumer cannot avoid reading.

**"Deduplicate by the `id`" produces `SELECT`-then-`INSERT`, and that is not dedup.** The contract required
deduplicating per event and **did not say how**.
*Found on 2026-07-26 by asking a system in this network that had NOT yet integrated with the gateway (`consumer-b`)
how it deduplicates today, before wiring up a redelivery buffer. Answer, checked by them against the production
schema and not the ORM: `if exists(): return` — read-then-write, no transaction, under 3 `sync` workers, with no
`UNIQUE` in the database.*
**And the fact that they had NEVER read our contract is what makes the finding strong, not weak:** that is the shape
somebody writes on their own, integrating directly with Meta, with nobody instructing them. The consumer who HAD
already integrated did it with `UNIQUE` — so the contract's sentence does not induce the error, but it also **does not
protect against it**, and the most natural reading of the problem arrives at the broken pattern by itself.
**And it is not only a buffer risk:** Meta itself redelivers **5 times in 9 seconds** when it does not get a `200` in
time (measured in our access log). Processing slower than the interval between attempts already produces simultaneous
deliveries of the same event **today**, with no buffer at all.
*The cost of a duplicate is not a repeated row: the side effects run again. In the measured case, another automatic
reply would go out to the customer and more Meta quota would burn — the damage shows up on a person's phone.*
**The rule that stayed in the contract: an obligation that depends on atomicity has to SAY "atomic" and show the right
shape.** Whoever writes the contract knows the failure mode; whoever reads it does not — and the most natural reading
is the one that breaks.

**An anti-repetition window by time looks like dedup and covers the wrong range.** The same consumer had a 60 s guard
(same message, same recipient) born from a real incident. It would absorb Meta's 9 s burst — **and not the +5 min
redelivery nor the +1 h35 one**, which is exactly when the event comes back. A guard by time protects against human
accident (six clicks on the button); it does not protect against the redelivery of a system that waits on a
logarithmic scale.

## Clocks and stamps — nine variants of the SAME error, and each rule caught only the previous one

It is not formatting fussiness. A wrong stamp on a channel between sessions is what decides, months later, whether
*"event X caused Y"* or the reverse order — and it survives review better than any code defect, because **nobody
distrusts a round number**. The three happened between 2026-07-26 and 2026-07-28, each one **after** the rule created
to prevent the previous.

**The rule and the table of the three variants do NOT live here** — they hold for any pair of sessions, including
those that do not exist yet, and were therefore versioned in `C:\dev\github\docs\CANAL-ENTRE-SESSOES.md`, section 3
(commit `df0d5a2` of the `github` repo, moved there by `consumer-a` on 2026-07-28). **Go read it there before stamping
anything.** What stays in this section is only what is ours: the cost variant 3 charged here, and the question it
generalizes.

**Variant 3 is the hardest and was ours**, on 2026-07-28: the planner ran `date` at 15:12, stamped that section with
the result, and for the following sections **extrapolated the number** — reused the clock from twenty minutes earlier
with an estimated increment, keeping the `%z` correctly copied. Two sections came out with `15:34` when the real clock
was `15:26`.

*Cost: seven minutes of error on two channels, corrected and not deleted. The avoided damage is what matters —
`consumer-a` would correlate those sections with the gateway's log lines, and seven minutes inverts the order of
events.*

**Who caught it, and the method is in the versioned doc:** `consumer-a` did not compare stamp with stamp — they went
for the `mtime`, **the number neither agent writes**. Stamp against stamp is one agent against the other, and
**neither of them is a witness**. It is the same discipline as *"real proof is the counterparty's traffic"* (the
resumption block, `docs/TASKS.md`), applied to clocks.

**And the question that generalizes, which is what makes this section useful beyond stamps:** when you create a rule
to prevent an error, ask **which PART of the datum it hardens** — because the next occurrence will be in the part that
was left over. Here the family has three parts (the number, the label, and the act of measuring), and each rule
covered one. It is this project's mother pitfall (*the rule holds in one place and not in the next*) applied to the
**rules themselves**.

**Variant 4 arrived the next day and is the missing piece: the stamp was RIGHT and the REFERENCE FRAME against which
it is read was wrong.** `zapgw estado` in `v0.25.0` printed, against production (measured on CT 125 on 2026-07-28
18:22, minutes after the version shipped):

```
gerado_em:      2026-07-28T18:22:31Z (ha 0s)
token_meta:
  medido_em:    2026-07-28T18:22:32Z (daqui a 0s)
  conferido_em: 2026-07-28T18:22:33Z (daqui a 1s)
```

None of the three stamps is wrong, and none was extrapolated: all three are the real instant of what they describe.
The error is the words **"daqui a"** ("in", future tense) about a measurement that **already happened**. The cause is
that the CLI had ONE `agora` in hand (the one that stamped the `gerado_em`) and used it for the two different
questions the screen asks: *"when is this snapshot from?"* (content, an instant chosen and shared between the two
surfaces) and *"is this fresh?"* (distance, which is always against the now of whoever is reading). Between the two
the CLI **measures the token on the Graph API** — it measures before reading, because the watcher's cache lives in
the server's process (T-065) — so `medido_em` is legitimately later than `gerado_em` and the arithmetic comes out
positive.

*Cost: no wrong number and no minute lost — and it still deserves an entry, because the damage is to CONFIDENCE and
grows with Meta's slowness, which is exactly when somebody is looking at this screen: two instances with the Graph API
at 4s each and the operator reads `(daqui a 8s)` in the middle of an incident. A screen that announces the future
about something that already happened trains you to distrust the rest of it — and it is the only instrument the person
has at that moment. Fix (T-072): `outbound.printClock`, read INSIDE `StateRows`/`ReadableStamp`; the instant stopped
being a parameter of those two, because the caller has in hand precisely the wrong answer and will pass it — which is
what happened.*

**The fix that does NOT work, and it is tempting: `if future { print "ha 0s" }`.** A genuinely future stamp exists and
is legitimate — the certificate's `expira_em` comes out `(daqui a 54d)` and is right. The rule is another: **"in" is
for what has not happened yet; an OBSERVATION stamp is never in the future, and if it is, it is the reference frame
that is wrong.** Hiding the symptom would erase the only signal that the reference frame went wrong.

*Two mutations, done and reverted before the commit, **and the second found more than it proved**:* (1) reverting the
reference frame to `gerado_em` leaves `TestStateRowsMeasureTheDistanceAgainstThePrintNowNotAgainstGeneratedAt`
(`internal/outbound/estado_test.go`) red **on the word**, with `medido_em` coming out `"(daqui a 1s)"` — the assertion
is about the PRINTED text, not about the computed duration, because it was the text that misled the reader; (2) the
end-to-end test on the real command
(`TestStateCommandDoesNotAnnounceAsFutureAMeasurementThatALREADYHAPPENED`, `cmd/zapgw/estado_test.go`) **passed GREEN
with the defect restored** while the fake Graph API took 50 ms: **every stamp in this project is RFC3339 WITHOUT
fractional seconds**, and the two instants fell in the same second. Only with a 1 s delay — which guarantees the
measurement lands in a *later* second — does the test reproduce production byte for byte. **The lesson: a test that
depends on two instants BEING different has to force the difference at the granularity at which the datum is STORED,
not at the clock's** — otherwise it passes green on your machine, fails one day by accident and nobody knows why.

**The question that generalizes, and it is a sibling of "where does each field COME FROM in each process?" (T-065):**
when the same `agora` feeds two things in the same function, ask whether the two answer the SAME question. *"When was
this assembled?"* and *"is this fresh NOW?"* look like the same reading of the clock, and they are not — the first
needs a shared instant, the second has nobody to share with.

**Variant 5 is the first that lives inside a TEST, not a screen or a channel: comparing a struct that carries a clock
stamp makes the test fail from the PASSAGE OF TIME, and the symptom disguises itself as "a flake with no cause".**
`TestMenuDoesEXACTLYWhatTheCommandLineDoes` (`cmd/zapgw/menu_test.go`) compares the whole `InstanceSummary` created by
the menu with the one created by the command line — on purpose, because comparing the whole struct is what stops a new
field from diverging silently (the same discipline as T-045, in the section above). One of the fields, `StampsSince`,
is stamped by the clock inside `store.CreateInstance` at the exact instant of the write — and the test creates the two
instances in two sequential calls. When the clock's second rolls over between them, the two RFC3339 stamps (without
fractional seconds) come out one second apart, and the test fails pointing at a field that has nothing to do with what
it exists to prove. **Measured on `main` on 2026-07-29: `-count=150` gave 5 failures (~3.3%).**

*Why a fixed clock could not be injected (the preferred way out of this family, see T-072 above): both paths — menu
and command line — go through the SAME `provisionInstance` (`cmd/zapgw/provisionar.go`), which calls
`store.CreateInstance` (not the `…Em` variant, which receives the instant as a parameter) and therefore does not
receive the time from outside. Changing that would mean touching `provisionar.go`, outside T-092's scope. Fix (T-092):
zero out `StampsSince` ON BOTH SIDES before comparing, with a comment naming the reason — and only that field; the rest
of the struct is still compared in full, so as not to reopen the pitfall the total comparison exists to close.*

*Cost: the lost minute itself is small, but the habit it teaches is not — a red verify every ~30 runs trains whoever
reads the suite to say "that is the usual one", and this project has already let a REALLY red `main` reach production
because the verify's output was read wrongly (2026-07-28, section *Environment*). A test that fails "sometimes" is
worse than a test that always fails: it vanishes from the suite and reappears in the incident.*

**Mandatory mutation, done and reverted before the commit:** changing one value of the menu's input (the
`--callback-url`, omitted instead of sent) makes the menu assemble different `args` from the command line — and the
test goes red, showing the divergent `callback_url` field (`Cadastrado:true` against `Cadastrado:false`) while
`StampsSince` stays zeroed and mute on both sides. **Zeroing the clock field did not mask the real divergence** —
proof that the test is still the same test, only without the noise of the rolling second.

**The question that generalizes, and it is T-072's with the target moved inside the suite: when comparing two structs
created at different moments, is any field stamped by the WRITE's clock?** If so, it proves nothing about what the
test wants to prove — it only proves that time passes. Neutralizing that specific field is different from ceasing to
compare the whole struct: the first removes noise, the second reopens this project's mother pitfall.

**Variant 6 is the same T-092 hitting again, in LESS THAN 24 HOURS, through a NEW field — and it proves the sentence
above to the letter.** T-098 (2026-07-30) added `TokenSetAt`, another clock stamp in the same `store.CreateInstance`,
and the same test started failing on a rolling second again, now on both fields:

```
menu_test.go:319: a instancia criada pelo menu difere da criada pela linha de comando:
  linha: … TokenDefinidoEm:2026-07-30T22:19:05Z …
  menu:  … TokenDefinidoEm:2026-07-30T22:19:06Z …
```

**Zeroing `TokenSetAt` alongside `StampsSince` would have fixed today and reopened tomorrow** — it is exactly the
hand-written list about the schema that T-092 had already paid the cost of identifying and could not avoid because of
scope. T-100 took the path T-092 pointed at as the right one and left out: `store.CreateInstanceAt` already existed,
receiving the instant as a parameter; what was missing was `provisionInstance` (`cmd/zapgw/provisionar.go`) using it
instead of `CreateInstance`. The instant now comes from a package var, `relogioDeCriacao = time.Now` (the SAME pattern
as `outbound.printClock`, T-072 above), which the test overrides to a FIXED instant before both calls (command line
and menu) and restores with `t.Cleanup`. With the clock frozen, both stamps are born IDENTICAL on both paths — the
zeroing of `StampsSince` left the test, and the whole-struct comparison became valid again without a SINGLE exception.

**The proof that it cured rather than merely patching under another name:** a TEMPORARY stamp field was added to
`Instancia`/`InstanceSummary`, written with `carimboDe(agora)` in the same INSERT (the migration and the column existed
only during the proof, reverted before the commit) — simulating exactly what T-098 did: a new stamp, on the right
creation path. `go test -run TestMenuFazEXATAMENTEOQueALinhaDeComandoFaz -count=300` stayed green. It is the difference
between a correction that hardens today's FIELD and one that hardens the MECHANISM: the next stamp somebody adds to
the `INSERT INTO instancia` — as long as it is born from the `agora` parameter, like all the others — comes out
covered, with nobody needing to remember to touch the test.

**The question this variant adds to the one above:** when the fix is "zero out/ignore field X before comparing", ask
whether instead you can make BOTH writes share the SAME instant. Comparing the whole struct with no exception at all is
stronger than comparing the whole struct minus a list of fields — and only the first survives the next field.

**Variant 7 is the first that dirtied the PERMANENT RECORD, and it enters through a measurement in production.**
Found on 2026-07-30 22:12 -03: `docs/CHANGELOG.md` opened with `## v0.37.2 — 2026-07-31` and
`## v0.37.1 — 2026-07-31`, **two versions dated on a day that locally had not started yet**. There was no typo and no
clock error: whoever wrote it measured in production, and the measured instant (`00:24`, `00:50`) came from the CT's
journal, **in UTC**. At 21:24 Brasília time it is already `2026-07-31` in UTC — so the stamp was right *in the
timezone it was read from* and wrong in the file it was pasted into, because **every other changelog entry uses local
time (-03)**.

**The cost is small today and grows by itself:** the two channel files, `docs/TASKS.md` and each task's
`_Completed …_` are local; a future reader correlating the changelog with any of them sees two deliveries on the "next
day" from the day the rest of the day happened. It is exactly variant 3's defect — the order of events between sources
— only now inside the record this project treats as **permanent and not reconstructible**.

**The rule, and it holds for every measurement made on a machine that is not this one:** *a stamp read from another
machine is not pasted raw.* Convert it to the file's timezone, **or** write the timezone alongside
(`00:50 UTC = 21:50 -03`) — that was the fix applied. And, if you are an implementer measuring in production: YOUR
machine's `date` and the CT's `journalctl` **are not in the same timezone**, and nothing in between warns you.

---

🔥 **The ninth variant, on the SAME day as the eighth and two hours apart: implementers stamp the CHANGELOG from
memory, and the changelog is the permanent record.**

On 2026-08-20, two consecutive tasks (T-145 and T-149) were retired with `_Completed 2026-08-20 22:40._` and
`_Completed ... 23:15._`. **It was 13:26 and 13:31.** Nine hours ahead, in a file this project treats as the source of
truth — it is where the **next free task id** is grepped for, and it is where each version's line comes from.

**The arbiter was free and right there:** `git log --date=format:"%H:%M"`. The eight earlier stamps from the same day
matched the commit's time within a minute or two; the last two were off by nine hours. *No agent writes the commit's
time — it is the same kind of witness that `stat -c '%y'` is for the channel.*

🔴 **The cause was not the agent: it was MY instruction.** The dispatch prompt said *"with the real time"*, and "real"
is an adjective — it is not a command. **The order has to be `run `date` and use the output`**, the same way the
channel rule requires `date "+%Y-%m-%d %H:%M %z"` instead of "stamp the time". *An instruction that asks for a quality
instead of a procedure is satisfied by the appearance of the quality.*

*Real cost: zero in production — but I let the FIRST one through and only saw it on the second, which means the review
was not looking. Had it stopped at the first, the changelog would have a line lying forever, and the lie would be in
the file the next session uses so as not to reuse an id.*

**The closing rule, and it generalizes to all delegated work:** *the subordinate inherits your rules, not your
instruments.* They do not have your clock, your `date`, your context of when the session started — so anything that is
a measurement has to come with the COMMAND, not with the adjective. And any number a delegate writes into a permanent
record **needs an arbiter at review time**, because it looks just as plausible as the right one.

🔥 **The eighth variant, 2026-08-20, and it is the most uncomfortable: BOTH sides of the channel got the stamp wrong
five minutes apart — on the day they spent the morning finding instruments that lie.**

Consumer `consumer-b` **extrapolated** a number (stamped `13:06`, `date` said `13:05`). I did the other half: my
section's `ATUALIZADO_EM` came out **measured** (`$(date)`, `13:06`) and the **title** of the same section came out
**from memory** (`13:08`). Label measured, number invented — the same family as the variant of the `-03` pasted onto a
UTC number, with the roles swapped around inside the same file.

**The one who proved it was neither of the two agents**, and that is why the arbiter rule exists:

```
stat -c '%y' consumer-b-STATUS.local.md   →   2026-08-20 13:06:46 -0300
```

*Real cost: no event was correlated wrongly — caught within minutes, by the other side of the channel. What it charged
was the illusion of being covered.*

🔴 **The lesson is not about minutes, and it is why this line exists:** that morning both sides found **five** defects
that were all instruments lying — an error message truncated in the relay, a sentinel naming the wrong unit, a
conversation export that does not export one type of message, and a warning buried inside a section about another
subject. **We were sharp at distrusting the other's instrument and our own code, and neither of us distrusted our own
clock.**

**The rule that generalizes:** *the instrument you trust most is the one you never thought of calling an instrument.*
A clock, an export, an error message and a section title do not look like measurements — they look like facts. When
you are hunting for a lying instrument, start with the list of things you would not classify as an instrument.

🔴 **Addendum from the same day, and it is what closes this section: writing the pitfall down did NOT prevent
committing it.** After recording the eighth and the ninth variants, the planner got a section title's stamp wrong **a
third time**, in the same session. All three have the same mechanics, and it is one of PROCEDURE, not of attention:
*the footer (`ATUALIZADO_EM`) was interpolated from `$(date)`; the title was typed by hand.* One came from a
measurement, the other from memory — in the same file, two minutes apart.

**The fix that worked was not remembering better: it was interpolating the title as well.** As long as a number is
typed, it will be wrong, and the error rate does not fall because the author knows they get it wrong.

*It holds beyond stamps: if you have documented a pitfall and keep falling into it, the problem is not the
documentation — it is that the step that produces the error is still manual. Automate the step or accept that the
ARMADILHAS line is just a place to record the relapses.*


## Documentation

🔥 **AN INCIDENT WITH A RESET DATE RECORDED AS A PERMANENT RULE (2026-08-29 → corrected on 2026-08-30).** This
repository's CI was removed because the **account's monthly Actions minute allowance** had been consumed — by
**another private project** of the owner's CI, and not by pushes from here. What got written down, in three files, was
something else: *"a private repo pays for Actions"* and **"the owner's rule: GitHub CI only on public projects"**. The
first sentence is false (a private repo has a free allowance); the second was never said.

**The cost, and it is one of decision, not of code:** the invented rule travelled from `CLAUDE.md` here to the public
repo's `CLAUDE.md` (`C:\dev\zapgw`), where it became a **row in the hard-rules table** — and the next day a session of
mine recommended to the owner that he **bring forward the repository's public opening** to "get the CI back for
free". *The recommendation was coherent, well argued and built entirely on a cause nobody had measured.* The owner
knocked it down in one sentence: *"estourou a quota, só volta dia 01/09"* ("the quota blew, it only comes back on
01/09").

**The rule that remains:** *a failure with a reset date does not become doctrine.* When the cause of an outage is not
measured, write **the symptom and the deadline** — never the general rule that "would explain" the symptom. A rule with
no measured cost behind it is a promoted guess, and a promoted guess is exactly what the next reader treats as fact.

**And the second, more specific one:** *an assertion about ANOTHER repository's state ages without warning anybody.*
The same `zapgw` line had already lied for that reason — it said the CI "exists in `zapgw-dev` and migrates with the
code" one day after the file had been deleted from there. When a doc needs to assert another repo's state, it says
**how to measure** (`gh repo view`, `git show <sha>:<path>`), not the measurement's result.

🔥 **A TRANSLATED POINTER FINDS NOTHING (2026-08-21).** In the pass moving the code to English, several comments
translated the **section title** they cited — `"TLS — não existe modo desligado"` became
`"TLS — there is no off mode"`, `"Testes"` became `"Tests"`. The documents pointed at **are still in Portuguese**, so
whoever greps the citation does not find the section. Nine occurrences reached `main` before somebody noticed.

*How it showed up, and it is worth more than the fix:* **an implementer saw two of their own forks doing it, corrected
it, and in the report observed that the already-merged pilot had done the OPPOSITE with `CLAUDE.md`'s titles.* It was
the **contradiction between two pieces of work** that revealed the defect — neither of them, on its own, looked wrong.

**The rule that remains, and it is wider than translation:** *a string that exists to be **searched for** is neither
translated nor rewritten* — a section title, a file name, an API field name, a value that comes out in the log. The
surrounding prose is free; the anchor is not. **And the check is mechanical:** every quoted citation next to a
`docs/*.md` has to be found by `grep -F` in the file it points at.

**A backtick inside `git commit -m "…"` EXECUTES, and the commit succeeds anyway.**

2026-08-20: a commit message in this repository lost the phrase *`` `request.POST.get("alcance") or LOCAL` ``* — the
shell treated the backticked stretch as a command substitution, tried to execute it, failed, and replaced it with an
**empty string**. The line ended up as *"o servidor tinha : escolheu por conta propria"*.

**Nothing failed:** `git commit` returned zero, the push went through, and the only signal was a syntax warning in the
middle of the output, among other lines. *The versioned file was right; what lost content was the permanent record.*

**The fix is procedural, not a matter of attention:** a long message goes through a **heredoc** (`git commit -F -`
with `<<'FIM'`), never through `-m "…"`. A single-quoted heredoc interprets nothing — not a backtick, not `$`, not
`!`. *Escaping backtick by backtick works until you forget one, and forgetting one produces no error.*

*Real cost: one technical sentence lost in an already pushed commit. Correcting it would require rewriting published
history, which costs more than the sentence is worth — so it stayed, and became this line.*


### 🔥 The execution record noted the plan's INTENT as if it were the RESULT — and the credential had no expiry for eleven days (2026-08-17)

**Cost: an API token with account-wide scope — `DNS Write`, `Zone WAF Write`, `SSL and Certificates Write`,
`Page Rules Write`, `Cloudflare Tunnel Write` — alive and with no expiry at all since 06/08. It was not found by us:
it was a team from another project auditing the account that warned us on the channel. And, in answering them, I
repeated the false assertion as if it were reassuring.**

The migration plan (`docs/superpowers/plans/2026-08-06-tunel-cloudflare-e-migracao-de-zona.md`) said, in the
preparation section: *"Expiry: 30 days"*. That was the **intent**. The EXECUTION RECORD of the same file, written
afterwards, noted in the "Done and checked" table:

> *"Scoped token created — `zapgw-migracao-cloudflare`, id `2ee4c9cf…`, **expires 2026-09-05**. Stored in … (outside
> the repository), value never printed"* — **Proof** column: `/user/tokens/verify` → `active`

🔴 **The "proof" proved something else.** `/user/tokens/verify` answers `active` — it **does not return
`expires_on`**. The line joined a measured fact (the token works) with an unmeasured one (the date), in a table titled
*"Done and checked"*. Measured on 2026-08-17 with `GET /user/tokens`: the `expires_on` field was **absent**. There was
no expiry. The token was never going to die.

**The three lessons, and the third is the one that generalizes:**

1. **A field your proof does not return, you did not prove.** If the assertion is about `expires_on`, the measurement
   has to bring `expires_on` onto the screen. `active` is not about that.
2. **The plan's intent and the execution's result are separate sections for a reason.** Copying the number from the
   first into the second turns "I wanted 30 days" into "it has 30 days", and the two sentences become identical on
   paper.
3. **An expiry nobody measured is an expiry that does not exist — and it is worse than having no expiry at all**,
   because it switches off vigilance. That was exactly the effect: for eleven days the account's broadest credential
   bothered nobody, myself included, *because it was written that it died on its own*.

*It is the same family as "a pretty verdict is not a measurement", which this project has already paid for twice (the
`DIVERGE em 12 de 12` of the broken extractor, in the same plan; and the `readyConnections` decided by the body rather
than by the status). Here the pretty verdict was in a documentation table, which is where it survives longest without
anybody checking.*

**State today** (2026-08-17, measured with `GET /user/tokens`, not deduced — it is lesson 1 applied to this very
line): `zapgw-migracao-cloudflare` was revoked, and the three remaining `zapgw*` tokens expire on **2027-02-17**:

| Token | Name on 2026-08-17 | Scope | Expires |
|---|---|---|---|
| `zapgw_conf_dns` | `zapgw-dns-tenant-one` | `DNS Write`, **only** on the `tenant-one.com.br` zone | 2027-02-17 |
| `zapgw_conf_tunnel` | `zapgw-tunel` | `Cloudflare Tunnel Write` + `Account Settings Read`, account-wide | 2027-02-17 |
| `zapgw_conf_worker` | `zapgw-sonda-worker` | Workers Scripts Write/Read, Tail Read, Observability Read, Account Settings Read | 2027-02-17 |

*The middle column is the **old** name, and it exists because the account's history and the commits up to 2026-08-17
only know that one. All three became `<project>_<type>_<function>` (T-126): `conf` because they **configure** — none is
on the execution path, so all of them can expire without bringing anything down. The doctrine is in
`C:\dev\github\docs\CREDENCIAIS-DE-API.md`, section 2. The files in `~/.secrets/` did **not** follow the new name, on
purpose: what reads them is `sonda-worker/deploy.sh`.*

*The expiry was applied with `PUT /user/tokens/<id>` — not by recreation — so no value changed and nothing in
`~/.secrets/` had to be rewritten. After the `PUT`, each token was proven twice: `/user/tokens/verify` → `active`
**and** the listing of the `permission_groups`, because `active` says the token is valid and does **not** say it kept
its permissions. That is lesson 1 again, and it nearly slipped: the first run left the positive control unexecuted and
still exited with code 0.*

🔴 **What has NO expiry, and it is deliberate:** the tunnel's **connector token**, which
`cloudflared-zapgw.service` uses on the Traefik LXC (`EnvironmentFile` mode `600`, `TUNNEL_TOKEN=`). It is not a user
API token, it does not appear in `/user/tokens` and no token API operation reaches it. *Worth knowing before touching
anything called "tunnel": they are two different credentials, and only one of them keeps production standing.* **They
had the same nickname until 2026-08-17** — `zapgw-tunel` was the API token, almost identical to the connector's name —
and that is exactly what forced the owner to stop an operation in progress to warn *"careful with the tunnel token"*.
**Only the documentation separated the two.** Today the name separates them: `zapgw_conf_tunnel` says `conf`, and
`conf` may expire.

---

**The task measured the cost on one surface and listed `Files:` from another — the fix did not reach the symptom.**
T-106 (2026-07-30) was born from a precise measurement: the **failure monitor** fired on normal Instagram traffic,
eight times in forty seconds. The `Why:` said so in so many words. But the `Files:` listed **only `internal/meta/`**,
and the `Do:` ordered separating the error categories — which was half the problem. The implementer did **exactly**
what was asked, and did it well.

**The next day, with `v0.40.0` live, the line looked like this:**

```
zapgw: parse falhou na instancia "tenant-two-ig": meta: item legitimo que esta fatia do instagram
nao modela: 1 item(ns)
```

**The error's text is right; the prefix still calls it a failure.** `internal/inbound/handler.go` prints the same
sentence for any parse error, without looking at which one it is — and the monitor, which matches on the line,
**kept firing**. The measured cost did not move a millimetre. (It is T-110.)

🔴 **The rule, and the error is the task author's, not the executor's:** *if the `Why:` measures the cost on a surface
— journal, monitor, screen, HTTP response — the `Files:` has to include that surface, or the `Do:` has to say
explicitly why it is left out.* Separating the cause in the lower layer is a prerequisite, not a fix: **whoever feels
the defect is whoever reads the line.**

**The symptom that this happened is characteristic and worth recognizing:** the task closes with a green verify, a
confirmed mutation and an impeccable report — **and the alarm keeps ringing.** When that happens, do not distrust the
implementer; reread the task's `Files:`.

**An INVENTED stamp on a channel between teams — and it inverts the file's reading order.** On 2026-07-30 the planner
wrote four consecutive sections on `consumer-b`'s channel and stamped three of them `23:05`, `23:20` and `23:30`
**without running `date`** — plausible increments, written while drafting. The real time when the last one went up was
**23:05**. Result: in a file whose convention is *"a new section goes on TOP"*, the **newest** section ended up with a
**smaller** number than the three below it. Whoever reads from outside has no way of knowing which came first — and
the wrong reading here is not academic: one of the sections **withdrew** the previous one, and swapping the order
inverts the meaning of both.

**Why this is different from variant 7** (that one was UTC pasted into a file in -03, an error of **conversion**):
here there was no measurement at all to convert. *Guessing a stamp is cheaper and more tempting than converting one
wrongly — and it produces a number that looks measured, because nothing in the text distinguishes the two.*

**The rule:** `date "+%Y-%m-%d %H:%M %z"` **before each section**, and paste the output into the header, as this
project's `ATUALIZADO_EM` already requires. One call per section, not one per session. **And do not rewrite a wrong
stamp afterwards** — note above it that it was not measured and that the true order is the position in the file;
rewriting erases the proof that there was an error and does not give back the right time.

**"It became the default" is NOT "it became mandatory" — and the difference between the two made a consumer cancel a
valid experiment.** On 2026-07-30, 23:20, the planner wrote on `consumer-b`'s channel that `allow_category_change`
*"stopped buying refusals"* and that their experiment *"cannot work"*. **Both sentences were inference presented as
fact.** What Meta documents is only this: since 2025-04-09, the automatic recategorization the parameter enabled **is
the default behaviour**. **What it does not say anywhere is what `false` does today** — the template creation page does
not even mention the parameter. *"Default" is, on the obvious reading, the value you override;* the planner read it as
"mandatory" and published the reading as if it were the source's own sentence.

**The cost, and it was immediate:** the consumer cancelled the `_v3` and closed the subject — a decision taken on top
of a certainty that did not exist. Ten minutes later the section had to be withdrawn entirely. *Whoever reads a
channel cannot distinguish "they checked" from "they concluded"; whoever writes can — and that is why the burden is on
the writer.*

🔴 **The secondary pitfall, which is the `ig_id` one with the target swapped:** there was a real measurement on the
table (the consumer asked for `UTILITY` and Meta stored `MARKETING`) and it **looked** like it confirmed the thesis.
It confirmed nothing: the gateway **never sent the field**, so that case measured the **default path**, not `false`.
*An impeccable measurement of a question that was not the one at stake — and it is exactly that kind that convinces,
because the number is true.*

**The two rules:**

1. **When asserting about Meta, quote their sentence or mark it as inference.** This project already requires that for
   its own code (`file:line`); an external source is no different, and it is **worse**, because the reader cannot grep
   to check.
2. **An absence of documentation is not documentation of absence.** "The doc does not say `false` works" and "the doc
   says `false` does not work" are different assertions, and only the first was true. When research comes back with no
   position, the cheap way out is to **make the mechanism exist and measure** — which was the owner's decision here:
   expose the field, relay it verbatim and **promise no effect at all** in the contract.

**Reverting in PRODUCTION does not revert the doc — and the surviving doc tells somebody to undo the reversion.** On
2026-07-30, `tenant-two-ig`'s `ig_id` was changed to `27807047495582675` (the id in the **App's scope**, the one
`GET /me` returns on `graph.instagram.com`), **4 events were discarded** by the routing guard, and the change was
**reverted** to the webhook's `entry[].id`, `17841403678746353`. The reversion command took two seconds. **The doc did
not come back with it**: a day later, the wrong value was still in **four** files — `docs/CHANGELOG.md` (calling it
*"the right id, measured twice independently"*), `docs/TASKS.md`, the deployment runbook (kept in the private repository) and `README.md`, the last two
as a **ready-to-copy command example**.

🔴 **What nearly happened, and it is the real cost:** T-103 (the task that gave the operator the screen for checking
the `ig_id`) carried, in its own `Verify:`, *"it has to display `ig_id: 27807047495582675`"*. **The new checking tool
was born pointing at the broken value.** Whoever ran the check and trusted the text would "fix" production back into
the state that discards all the traffic — and the symptom of that is silence: `200` to Meta, nothing to the consumer,
no error anywhere.

**It only showed up because the check was made against the MACHINE**, not against the text: `zapgw instancia mostrar`
printed `17841403678746353`, and the `conta_descartada` counter (4, last at `00:48:47 UTC`, zero after the reversion,
with `recebidas 16 / entregues 16`) closed the proof by behaviour, without depending on which endpoint returns which
id.

**The two rules that come out of it:**

1. **A reversion is a change, and a change breaks docs.** When you undo something in production, run the same
   `grep -rn "<old value>" docs/ *.md` you would run when introducing it. Reverting looks like "going back to what was
   there" and therefore escapes the check — but what was there, the doc no longer described.
2. **A real value inside a command example is debt.** `README.md` and the deployment runbook (kept in the private repository) carried the id as a
   literal argument, ready to copy and paste into production. They now carry `<entry[].id do webhook>` — a placeholder
   that **forces you to think** and cannot go stale.

**"There is no task" answered as if it meant "there is no field" — and the queue is the wrong source.** On
2026-07-28, 21:27, the planner wrote on the consumer's channel that the template's `id` in `GET /v1/templates` *"has
not become a task yet"*. **The field was already being delivered** — `meta.Template.ID`
(`internal/meta/templates.go:76`), serialized straight by the handler (`templates_handler.go:198-204`). It had gone in
with **T-078**, tag `v0.29.0` at **19:56:23**, *eleven minutes after the consumer asked at 19:45*, for another reason
(the catalogue reread needed to say "this is the one"). Nobody noticed the request had been satisfied sideways.
*Cost: a false statement on the channel, and the consumer would have carried on with an unnecessary workaround. The
one who caught it was the owner, asking **"why did you decide not to do it?"** about a line that said "no task" — the
question was about priority, and the answer was that the premise was wrong.*

**Two different questions, and the queue only answers one:**

| Question | The source that answers |
|---|---|
| *do we plan to do this?* | `docs/TASKS.md` |
| *does this exist?* | **the code, and only the code** |
| *this does NOT exist — was it an oversight or a decision?* | **`CHANGELOG.md`, where the decisions live** |

🔴 **The third row entered on 2026-07-29, and it cost an implementer's entire work.** The owner typed `zapgw menu` in
the CT and got `subcomando desconhecido`. The planner saw the absence, called it a gap and wrote **T-089** ordering
the subcommand to be created. **The absence was a decision**, recorded in so many words in T-082's changelog
(2026-07-28 20:56): *"there is NO `zapgw menu` as an explicit subcommand — an invocable name can be put in a script
and hang waiting for input… the guard would stop being structural."* The task was dispatched, **implemented with a
correct TTY guard and a mutation proving that without it the test hangs**, and the owner **refused it anyway**: *what
is protected by impossibility is not traded for what is protected by checking.*
*Cost: a complete implementer run thrown away, two docs that spent a few hours promising the opposite of the decision,
and the branch deliberately deleted — because a branch with a reverted decision inside is a merge temptation for the
next session.*
**What makes it treacherous:** the code answers *"it does not exist"* perfectly and **does not say why**. The grep
confirms the absence and the absence looks like a bug — the only place that distinguishes the two is the record of the
day somebody decided. **Before writing a task that ADDS something missing, grep the changelog for the thing's name.**
Ten seconds, and the alternative is reverting finished work.
*Sibling of the `consultorio` rule, with the sign flipped: there the closure was not written and the subject
resurrected; here it was written, and it was not read.*

It is the same pitfall as *"do not assert about the other side's system without opening the file"*, **turned on your
own system — and therefore less excusable, not more**: here the code is in your hand and grepping it costs ten
seconds. It is more likely in a project with a mature queue, because the queue **looks** authoritative: it is a record
of intent, and delivered work **leaves** it.

🔴 **The trigger that generalizes, and it holds for every delivery:** a task that resolves a request **as a side
effect** leaves no trace under anybody's name. When a task touches a field, a route or an error that **some consumer
has already asked for**, say so in its changelog entry — otherwise the request stays "open" in everybody's head while
it is finished and live.
*Consequence of the finding: nothing held that field. No test checked the `id` in the LISTING, and with `omitempty` it
would vanish with no error and no suite failure — it became T-085. T-085's mutation confirmed the diagnosis: deleting
the field from the struct only went red through the **creation** tests and the ambiguous reread; the whole listing
passed.*

**THE SAME FAILURE, THREE HOURS LATER, AGAINST MY OWN WRITTEN RECORD.** Still on 2026-07-28, the owner asked *"do we
have a leak or not?"*. I measured production and found `numero_descartado: 4` and `conta_descartada: 1` on the
consumer's instance, with five `ALARME`s in the journal carrying `phone_number_id "000000000000000"` and
`waba_id "0"`. I inferred — and marked it as inference, which saved the report from being false — that it would be
Meta's panel sample trigger during the webhook configuration, and said I would **ask the consumer**.
**They were my own hand tests**, made during that day's `app_secret` rotation. And the explanation was **written by
me, in the channel file, hours earlier, with the same two numbers**: *"your instance already has
`conta_descartada: 1` and `numero_descartado: 4` — from my own cutover tests"*. The one who corrected it was the
owner.
*Cost: a report with the wrong cause, and me one step away from asking the consumer about my own traffic — which would
have been the fourth time in the same day asserting about a system without opening the file.*
🔴 **The rule that comes out of it is about WHICH source you consult, and it is the same as the table above with one
extra row:** *"where did this traffic come from?"* is not answered by counters and the journal alone — they record the
**effect**, never the **intent**. Who knows the intent is the record of whoever acted, and **in this project that
record is the channel file**. Before inferring the origin of any production event, **grep the channel for the
number**. It is cheaper than the inference, and on this occasion it would have answered in ten seconds.
⚠️ **Operational consequence, and it is the one that really bites:** `numero_descartado` is **visible to the
consumer** in `GET /v1/estado`, and that consumer was told in writing that it means *"something is knocking on the
wrong door"*. **A manual proof in production goes on the test instance** — which is what T-042 did, with
`teste-isolamento-t042` — never on a consumer's instance: their counter starts lying, and they have no way of knowing
the noise is ours.

🔴 **And the aggravating factor, which closes the case against the obvious remedy: the contract ALREADY documented the
field** — `docs/CONTRATO-CONSUMIDOR.md`, section *Ler o catálogo de templates*, written in T-078 itself, with the
right caveat about the absence not being an error. **The field existed, the doc existed, it was right, and even so the
consumer's request stayed "open" in everybody's head.** So the remedy is not "document better": it is the explicit
cross-reference in the changelog of the task that delivered it. *A doc answers whoever went looking; nobody went
looking, because nobody knew there was anything to look for.*

**The discipline was applied to the artefact that is audited, and not to the one that is copied.** The contract
carries the correct `reacao` example — `{"reacao": {"alvo": …, "emoji": …}}` — because a contract example is
**executed** before it goes in, by rule. But the version announcement written **on the channel with the consumer** was
typed by hand, and came out with `alvo` and `emoji` at the **root** of the body. The consumer copied from there (it is
the text that was in front of them, freshly written, about the feature they wanted to use) and got
`400 campo obrigatorio ausente: reacao`.

*Cost: two minutes of the consumer's time, and the only reason it was not more is that the contract exists and is
right.*
**The lesson is not "review more": it is to notice WHICH artefact people copy.** A doc is what gets audited; a message
is what gets used in a hurry. The rule "an example comes from a consultable source, never from memory" held for both
and was applied only to the first. **Where you cannot execute the example, point at the doc instead of rewriting it**
— a link does not diverge from the implementation; a copy does.

**A local guard that replicates a remote system's rule ages on THEIR clock, not yours.** On the consumer's side
(recorded by them on 2026-07-26): the client refused an empty `emoji` locally, without hitting the network, because
"the result would be the same `400`". It was **correct when it was written** — and became wrong **that same
afternoon**, when the gateway started accepting it. The message the operator saw ("not supported yet") became a lie
with nothing changing on their side.
*Rule: where you can, let the rule's owner refuse and translate the refusal. Duplicating the validation to "save a
call" buys latency with a copy nobody remembers to update — and the day of divergence is chosen by the other team.*

**A doc example is code nobody runs — and that is why it is where a wrong value survives longest.** Three examples in
`CONTRATO-CONSUMIDOR.md` carried `"instancia": "consumer-a"`, which is the **consumer's** name, not the instance's
slug (`tenant-one`). The examples were *executed* before going into the doc — deserialized and validated — and passed:
schema validation does not know which slugs exist, so a plausible non-existent slug is indistinguishable from a right
one. **Executing the example proves the shape, never the value.** An example value that names something real (a slug,
an id, a phone number) has to be checked against the real thing — here, `zapgw instancia listar`.
*Cost: zero, because the consumer read the example before using it and asked. Had they not: the `callback_url` would
be registered with the wrong slug, the consumer's multi-tenant guard would answer `503` to every delivery, Meta would
requeue for 36 h — and a stubborn `503` **looks like a signature problem**, which is where the investigation would
start. Corrected on 2026-07-26, along with a warning in the contract itself that `instancia` is the number and not the
consumer.*

*And the slug was the smallest of the errors — it only showed up because somebody asked. Checking the SAME three
examples against `GET /v1/templates` afterwards, **every specific value was wrong**: `venda_confirmada` has three body
variables and the example sent two; `equipamento_enviado` has four and the example sent two; `orcamento_disponivel`
has **one** button and the example sent a parameter for two indices. The rule that comes out of it is harsher than
"check the value": **the real thing's catalogue/schema is the source, and it is consultable — `GET /v1/templates`
answers in one command.** An example whose value was not read from a consultable source is a guess wearing the costume
of a reference, and the reader has no way of telling.*

**Hunting for false docs produces false docs.** It is not irony, it is the pattern — and it happened on this branch,
in two consecutive rounds:

1. A comment asserted that Graph API version `v21.0` "had already expired". **Verified at the source:** Meta's
   versioning page confirms the current one is `v25.0`, but says only that each version lasts **at least two years**
   and **does not list** expirations. The expiration part hitched a ride on the true fact.
2. The fix for that cited `/docs/graph-api/changelog` as its source — a URL that **answered 404** on the same date.

*The mechanism: during a review you write fast and confidently about something you have just understood, and
**describing what the source ought to say is more fluent than describing what it says**. Treat the text you have just
produced as the least audited part of the repo — because that is exactly what it is.*

**An assertion about an external service requires a pointer verified AT THE MOMENT of writing**, and what was not
verified is **marked as unverified**, not omitted nor softened.

**The proof that the leak was closed REOPENS the leak.** The `verify_token` leaked in the query string of Traefik's
access log (T-019). Once rotated, the next step was to prove the old token was now refused and the new one worked — by
hitting the **public URL**, which crosses Traefik. The proof's three requests wrote the **new token** into the same
log the rotation existed to clean. The symptom was numerical and only showed up because somebody counted:
`grep -c hub.verify_token` went from **19** to **24**, and three of the five new lines were the test's own.
*Cost: a second rotation, and the value was briefly exposed between the two. Fix: prove against the gateway DIRECTLY
(`<ip-interno-do-gateway>:8080`), which goes through no proxy and has no access log — the public path should only be
exercised by Meta.*
**The general rule, which holds beyond this case: before testing a secret, ask where the TEST's request goes
through.** A test that reproduces production's path also reproduces the points where production writes things down —
and a proxy's log does not distinguish "a real request" from "a check that the leak was closed".

**Two independent sources descending from the same nothing look exactly like corroboration.** About "how a reaction is
removed on SENDING", on 2026-07-26, there were two sources that seemed to agree: a search summary saying `emoji: ""`
removes it, and the **consumer's production code**, which sends `emoji: ""` in that case. Two different origins, the
same answer — the most convincing form of evidence there is. Tracing each: the summary came from an **unofficial
aggregator**, and the consumer's code came from the **docstring of a library** that asserted *"this is how Meta
defines it"* **without citing Meta**. Neither of them had seen the thing happen. The official page, read the same day,
marks `emoji` as **required** on sending and describes no removal at all. *(Bonus from the same trace: a third
mechanism the search offered, "unreact", belongs to ANOTHER Meta product — Messenger Platform, `sender_action`. A
search summary mixes products from the same company with an ease that does not forgive.)*
**The question that separates the two things: "who, in the chain of this assertion, SAW it happen?"** If the answer is
nobody, two sources are worth the same as zero. Agreement between links that observed nothing is just the same error
copied.
*Cost: zero, and only just. The implementer traced the first and the consumer traced their own — if either of them had
answered the question instead of investigating the origin, the gateway would have gained a removal path that **fails
silently with `200`**.*

**"Proven on the inbound side" is not "proven on the outbound side" — it is the same word, it is not the same
guarantee.** The consumer observed in production (2026-07-20) that Meta **omits** the `emoji` key in the WEBHOOK when
the user removes the reaction. That is a fact, and the envelope depends on it. But it says **nothing** about what Meta
ACCEPTS on sending: they are two sides of an API with rules of their own, and symmetry is a comfortable assumption,
not a property. This document was at one point written treating the two as a single fact, and the correction came from
the consumer, about their own data.
*Rule: when citing evidence of an external service's behaviour, write the DIRECTION along with the fact. "Meta omits
`emoji`" is ambiguous; "Meta omits `emoji` in the removal webhook" is not.*

**The `verify_token` goes in the query string, and Traefik logs the whole URL.**
Meta chooses the verification `GET`'s format: `?hub.mode=…&hub.verify_token=…`. Traefik's `accessLog` records the full
`RequestPath`, so the token stays legible in `<Traefik LXC>:/var/log/traefik/traefik-access.log`. Observed on
2026-07-25, in `tenant-one`'s real verification. **Cost so far: zero** — and it is worth saying why, otherwise this
becomes a false alarm: the `verify_token` is only used in the `GET`, never in the `POST`. Whoever steals it receives no
message and forges no delivery (that is the `app_secret`). Recorded as T-019.

**Meta sends the verification parameters DUPLICATED.**
In the real `GET` it sends `hub.mode`, `hub.challenge` and `hub.verify_token` **and also** `hub_mode`,
`hub_challenge`, `hub_verify_token` (with underscores). The gateway reads the dotted ones and works. Whoever touches
`verificar()` must not "fix" it to underscores thinking the doc changed: **both come together**, and switching would
break it for no reason.

**A doc that describes a non-existent limitation sends people down the dangerous path — and nobody checks a
limitation, only an instruction.** the deployment runbook (kept in the private repository) asserted that activating a laboratory instance "still has no
CLI path: only `zapgw fumaca` activates, **and it talks to the real Graph API**", and therefore prescribed an
`UPDATE instancia SET ativo = 1` typed by hand into the **production** database. The sentence's second half had been
false since `fumaca` was born: it calls `graphBase` (`cmd/zapgw/main.go`), which reads `ZAPGW_GRAPH_BASE` — pointing at
another endpoint was always possible, and it is exactly what the suite does in `cmd/zapgw/fumaca_test.go` from day
one.
*Cost: a hand-written `UPDATE` on production's SQLite with the service live (T-042, and the recipe stayed written as a
procedure for two days), plus two tasks (T-048 and T-071) spent deciding whether it was worth opening a **second
door** to `ativo = 1` — an architecture decision for a problem that already had a way out.*
**The mechanism, which is what to take away: the fake the operator needed was locked inside `_test.go`.** When a test
needs a fake server to prove a path, **whoever operates needs the same server to exercise that path** — and a
`grafoFalso` that only exists in a test file is invisible to the person in the CT at six in the evening. Fix:
`cmd/grafo-falso/` (a separate binary; `deploy.sh` compiles only `./cmd/zapgw`) and the recipe in
the deployment runbook (kept in the private repository). **When you write "there is no way", say why there is not — the complete sentence is what can be
checked.**

### 🔥 A project's acceptance criteria live in the PR TEMPLATE, not in the CONTRIBUTING (2026-08-20)

While studying whether `zapgw` could enter the **community-scripts** catalogue, I read four documents to gather the
rules: `CONTRIBUTING.md`, `AGENTS.md` (1,083 lines), `docs/guides/source-origin.md` and the site's contribution page.
I gathered the file structure, the anti-patterns, the helper functions, the JSON format — and concluded, in writing,
that **there were no eligibility criteria**.

There were. And they are disqualifying:

> - [ ] The application is **at least 6 months old**
> - [ ] The application has **600+ GitHub stars**

It is in ProxmoxVED's **`.github/pull_request_template.md` checklist**. None of the four prose documents mentions it.
The one who corrected me was the owner.

**The rule that generalizes, and it holds for any third-party project:** *a prose document teaches **how to write**;
the PR and issue templates say **who gets in**.* The checklist the maintainer ticks is the real criterion. **Read
`.github/` BEFORE `CONTRIBUTING.md`** — it is shorter and it is what decides.

🔴 **And the number depends on the DOOR.** The same project asks for **600** stars from whoever submits their own
script (a PR on ProxmoxVED) and **1,000** from whoever asks somebody else to make one (a discussion on ProxmoxVE).
Reading only one of the two produces a confident and wrong answer — which is what I did when I "corrected" the owner's
600 to 1,000, having read only the second. *When there is more than one entry path, each has its own ruler; find the
ruler of the path that is yours.*

*Real cost: an entire study whose final phase — submitting to the catalogue — was ineligible on two hard criteria, and
whose work order placed the submission as a destination instead of a gate. The fix changed the Verdict, Part II and
the last two phases, and incidentally revealed a cost nobody had seen: a new repository **resets the 6-month clock**.
None of that would have surfaced if the owner had not known the number by heart.*

### 🔥 A number that arrives ready-made does not bring the command that produced it — I published "18 events" that were 35, because the source read the output of a `tail -40` (2026-08-28)

A consumer measured, in their database, how many times Meta had reclassified a template's category, and sent it on the
channel: *18 events, five downgrades, windows of 14h to 22h*. The number entered
`docs/CONTRATO-CONSUMIDOR.md` the same day, with attribution, with the caveat about a small sample — and **committed
and pushed**.

It was wrong. The query had run with `| tail -40`, the output had more lines than that, and whoever measured
**counted what was left as if it were the total**. The right numbers: **35 events**, **16** changes to MARKETING, and
windows reaching **512 h**. *A `tail` is a filter; it was treated as a report.*

*Real cost: a consumer contract with a false number, on `origin`, for 27 minutes — and the correction came from the
source itself, not from us. Had they not gone back to check, the number would still be there today, looking like a
measurement.*

**What was missing on the PUBLISHER's side, which is the actionable half:** the channel doctrine already says
*"measure, do not estimate — and give the command"*, and it was read as an obligation of **whoever measures**. It is
also **whoever publishes**: a number arrived without the command beside it, and nobody asked for the command.
**Asking costs one line; the `| tail -40` would have been visible in it.**

**Rule:** a third party's number that becomes a durable document goes in with the **command that produced it**
attached — or it does not go in. And when the command cannot come, the document says *"reported by X, without
independent verification"*, which is a different and honest assertion.

⚠️ **The part that nearly slipped by, and it is the most expensive:** the wrong numbers were the ones that **favoured
the conclusion less**. The correction *strengthened* the argument (16 downgrades in 25 days, not 5 in a month), and an
error that pushes in the direction you already wanted is the one that least invites checking. *Reviewing a number
cannot depend on the number bothering you.*

🔥 **2026-08-29, SAME series, SAME source: the 28/08 correction fixed the COUNT and left the CONCLUSION standing — and
it was the conclusion that was wrong.** The source went back to the `payload_cru` of the same 35 events and measured
that the downgrades happen **1 to 13 minutes after the template's CREATION**, and that **no** return to `UTILITY` came
from Meta — all of them are batches of a human asking through the WhatsApp Manager menu. That is: the mechanism
`CONTRATO-CONSUMIDOR.md` described — *Meta downgrades an already approved template on its own* — never existed, and the
block still offered four windows (14.8 h / 14.9 h / 22.1 h / 22.2 h) as being **"Meta's number"**.

*Cost: a month with the wrong mechanism in the document consumers read — surviving a correction that walked right past
it.*

**The actionable part, which is different from the rule above:** when a third party's number is corrected, the review
has to climb from the number to **the sentence it sustained**. Correcting `18 → 35` and reprinting the same conclusion
gives the wrong text a **second signature of "checked"** — and the second is harder to knock down than the first,
because now it looks reviewed.

⚠️ **And a second cost, the same day and smaller, but of the same family:** commit `ee4f0c7` announced in its message
that *"ARMADILHAS gains the new cost"*. **It did not** — the script that would do the insertion aborted on a
non-existent anchor (the sentence broke across two lines), the `git add` of the untouched file added nothing, and the
commit went out anyway. **A commit message is a document**, and that one lied for one push.
*When a commit promises to touch two files, check `git show --stat` before pushing — a clean `git status` after the
commit does not distinguish "I wrote it" from "there was nothing to write".*


### 🔥 The planner queued a task while an implementer was in flight — and the PREVIOUS task's commit deleted the new one, by the planner's own hand (2026-08-28)

The real sequence, in seven minutes:

1. `11:36` — the planner adds **T-174** to `docs/TASKS.md` and commits (`b0ce5e7`). There was an implementer running
   **T-173** in the same tree since `11:20`.
2. `11:43` — the implementer finishes, and **removes T-173** from `docs/TASKS.md` — from the copy they had read
   **before** `11:36`. Their write takes T-174 along with it.
3. The planner checks the diff, sees what they expected (T-173 leaving, the resumption block updated), commits
   (`ff1da91`) and **pushes**.

`git show ff1da91 -- docs/TASKS.md | grep '^-## \[ \]'` shows **two** tasks removed. Only one had been done.

*Real cost: almost zero, and for a reason that does not repeat itself — **the next implementer went looking for the
spec that was not there**, recovered it from `git show b0ce5e7` and executed the whole task, instead of answering
"there is no task in the queue". Without that initiative, T-174 would have evaporated with a clean `git status`, a
green push and a queue that looked correct.*

**Why the diff did not save it:** the planner read `docs/TASKS.md`'s diff looking for **what they expected to find** —
the finished task leaving — and found it. One extra removal, in a file of hundreds of lines where removing a task is
normal behaviour, draws no attention at all. *A review that confirms the hypothesis is not a review.*

**The two rules, and the second is the one that closes the hole:**

1. **With an implementer in flight, the planner does NOT edit `docs/TASKS.md`.** It is the file the implementer is
   going to rewrite. Queuing can wait for their report; if it cannot, the planner writes the spec somewhere else and
   moves it into the queue **after** the commit.
2. **After the implementer's commit, `grep` the queue for what you added.** A `grep -c 'T-174' docs/TASKS.md` costs
   seconds and is the only step that sees a removal nobody asked for.

➡️ **The generalization, and it is the part that holds beyond the queue:** this house's doctrine already said that
*two implementers in the same tree run each other over*. This case shows that **the planner is a writer like any
other** — they were not "just documenting", they were editing a file another process had open. *Every file an
in-flight agent is going to rewrite is their territory until the report arrives.*

---

### 🔥 A gate that covers one kind of personal data reads as covering all of them — and the declared hole is where the leak went through (2026-08-31)

The phone-number gate (`internal/config/telefones_allowlist_test.go`, T-161/T-162/T-191) is this repository's
strongest mechanism: an allowlist that fails closed, decodes base64, and has already caught real data more than once.
Its presence in `CLAUDE.md`'s rules table, right next to the row for "no data identifying a real person," reads as
if the whole category were covered. **It was not.** The row's own text said so, in writing, since the row was first
written: *"it covers phone numbers only — a customer name … still has no gate."*

**The hole bit anyway.** A real customer's name reached this public repository — in two documentation files (with
their PT-BR pair) and two Go test fixtures — and nothing failed, because nothing was looking for that shape of data.
What found it was not a mechanism: it was a manual `git grep` for a name someone already knew to look for, which is
exactly the failure mode the phone gate's own header comment warns against (*"searching for the number you know only
finds the number you already know"*). The six affected files are listed in T-193's task record
(`docs/CHANGELOG.md`), not repeated here.

*Cost: a real name in a repository that has no unpublishing — the exact harm this project's opening rule
(`CLAUDE.md`, "What this repository is") exists to prevent. Zero further cost only because it was caught before the
repository's planned delete-and-recreate, not because the gate caught it.*

**Why the declared hole did not get closed sooner:** naming a gap in a comment reads as due diligence — it looks
like the risk has been accounted for. It has not: a hole that is written down is still a hole. Nobody had to forget
anything for this to happen; the row was accurate the whole time, and accurate is not the same as covered.

**The fix (T-193) deliberately does not extend the phone gate's allowlist model.** A phone number can be
legitimately synthetic, so declaring one and moving on makes sense. A customer's name showing up in a public
repository is never legitimate, so `internal/config/nomes_allowlist_test.go` has no allowlist and no per-file
exemption at all — any match is a finding, full stop — and its needle list lives OUTSIDE the repository
(`ZAPGW_FORBIDDEN_NAMES` or `~/.zapgw/forbidden-names.txt`), because writing the forbidden names inside a public
repository would publish exactly what the gate exists to keep out.

**The rule that generalizes:** a rules table with one row per data type invites reading "the row exists" as "the
kind is covered," when the row itself may say otherwise in its last clause. **Read the whole cell, not just its
checkmark** — and when a doc declares a gap, treat that declaration as a task waiting to be filed, not as
permission to leave it open.

---

### 🔥 A fail-closed gate that makes the legitimate path impossible does not protect — it trains the bypass (2026-08-31)

T-199's pre-push hook (`.githooks/pre-push`) was built to close a real hole: a phone number or customer name
introduced in commit A and deleted again in commit B leaves the final tree clean, but commit A still reaches
`origin` the moment the branch is pushed — there is no despublicar in a public repository. The fix computed the
pushed range as `oldSha..newSha` and, when git's pre-push protocol reported the remote sha as all zeros (no ref
on the remote yet), refused outright: *"nao ha base segura para calcular o intervalo introduzido"*.

**That covered every case except the one that happens on every single new branch.** The first push of ANY new
ref — including one with nothing but clean commits — reports a zero remote sha, because there is no remote ref
yet. The gate blocked all of them, unconditionally, with no way to satisfy it: the push that would create the
ref is the same push being refused. **The only remaining path was `git push --no-verify`** — the hook's own text
names that flag as "the only thing that disables this gate." A gate whose sole failure mode teaches the bypass
does not raise the bar; it lowers it, because the person who learns to type `--no-verify` for a clean branch
today types it again on the day there really is a needle in the push.

**Measured by the planner on 2026-08-31, with a real push against a disposable bare repo** (never `origin`): a
brand-new branch with one clean commit was refused. The refusal message read *"sha remoto = zeros — nao ha base
segura"* — correct as a description of what happened, useless as a description of what the branch contained.

**The method error that rode along with it, and is worth its own line:** the planner's first control on this
mechanism "passed" in the sense that the push was blocked — but for the wrong reason. Sha-zero refusal is not
evidence the gate can find a needle; it is only evidence the gate refuses. *A block that does not distinguish
"I found the needle" from "I could not verify" proves the instrument recuses, not that it looks.* The same
confusion the phone/name gates guard against on the data side (`docs/ARMADILHAS.md`, "could not verify" vs.
"found nothing") showed up on the gate's own self-test.

**The fix (T-200) does not guess a merge-base and does not relax the failure mode — it changes which formula
computes the interval.** `git rev-list <new-sha> --not --remotes` is exactly "every commit reachable from this
ref that no remote-tracking ref this repository already knows about can reach" — computable without assuming
which branch the new ref forked from, and it naturally reduces to "sweep every commit reachable from `HEAD`" when
the repository has no remote-tracking ref at all (`--remotes` then matches nothing, so `--not --remotes` excludes
nothing) — the safe, slower fallback the task required instead of ever treating "cannot compute the smart
interval" as "let it through." Verified against real data, not asserted: a clean new branch now pushes; a branch
whose commit A introduces a needle and commit B deletes the file again still blocks, and the message names commit
A and the file, not just "blocked" (`internal/config/prepush_test.go`,
`TestPrePushGateNewRefCleanBranchPasses` / `TestPrePushGateNewRefBlocksNeedleDeletedLater` /
`TestPrePushGateNewRefNoRemoteAtAllSweepsEverything`).

**The rule that generalizes:** when a fail-closed gate's only escape hatch is "turn the gate off," the gate is
mis-scoped, not merely strict — rigor that has no legitimate path left is indistinguishable, in practice, from no
gate at all, because the discipline of using the escape hatch decays the moment it becomes routine.

---

### 🔥 A gate that reads a commit's diff can be blind to a whole class of commit — and it was, for merges (2026-08-31)

T-199's pre-push gate materializes exactly what EACH commit in the pushed range introduces, via `git diff-tree`,
and sweeps that content for a phone number or a customer name. The function's own comment already named the
limit: `git diff-tree` with no `-m`/`-c` flag only computes a diff for a SINGLE-parent commit — pass it a merge
commit and it returns an EMPTY diff, unconditionally, regardless of what that merge's tree actually contains.
**Content that exists only in a merge's conflict resolution — present in neither parent — crossed the gate
completely unseen**, because the function that lists "what this commit changed" reported nothing changed at all.

**Declaring the hole in a comment is not the same as closing it**, and this project had already paid for that
exact gap once (T-193's phone gate covered `docs/` in name but not in the code that decided which files to
scan — see the entry above this one). Writing "this is a known limitation" reads as due diligence right up until
someone resolves a conflict by pasting a value straight from a support ticket, which is precisely the moment a
merge exists.

**Measured before choosing between `-m` and `-c` (T-201's Do item 2), against a REAL merge built in a disposable
clone of this repository, never `origin`:**

- A clean merge (two branches touching disjoint files, git auto-merges without a conflict): `git diff-tree -m`
  reported **12 files** — the concatenation of every file EITHER branch touched, because `-m` diffs the merge
  commit against each parent SEPARATELY and includes both results. `git diff-tree -c` (combined diff) reported
  **0 files** — correctly, because every file's final content trivially equals one parent or the other, and
  whatever commit produced that parent already sits in `commits` and gets scanned on its own.
- A merge that resolves a REAL conflict (both branches edit the same line of the same file; the resolution text
  exists in NEITHER parent): both `-m` and `-c` reported the same **1 file** — the one that actually needed
  resolving.

**`-m` does not cover more than `-c` here — it re-inspects content the per-commit scan already looked at, and
the redundant work scales with the SIZE of both branches, not with the size of the resolution.** `-c`'s rule for
omitting a file ("identical to one parent, so trivial") cannot hide a needle from this gate: a file `-c` skips
because it matches parent P was introduced by whatever commit built P's tree, and that commit is either already
in the scanned list (how else would it be a parent inside this push) or was already public before this push
existed. Either way something already looked at it — `-c` never removes the ONLY look, only a redundant second
one.

**The fix (T-201) branches `filesChangedInCommit` on parent count**: 0 or 1 parent keeps the original
single-parent diff (with `--root` for the genesis commit); 2+ parents switches to `git diff-tree -c`. Proven
against real data, not asserted: `TestPrePushGateBlocksNeedleOnlyInMergeResolution` builds an actual conflicting
merge with a needle that provably exists in NEITHER parent, and the block names the merge commit itself and the
file — not a commit that merely "looks blocked" the way T-200's own postmortem (the entry above this one) warned
against. `TestPrePushGateCleanMergeOnMainPasses` is the matching negative control: a clean merge still pushes,
in well under a second.

**The rule that generalizes:** a comment that names a gap accurately is not a mitigation — it is a description of
exposure with a due date nobody set. The gap closes when the code changes, not when the risk is written down; and
when two git flags could plausibly answer "what did this commit introduce," measure both against a fixture built
from a REAL operation (a real `git merge`, not a synthetic diff) before picking the cheaper-looking one.
