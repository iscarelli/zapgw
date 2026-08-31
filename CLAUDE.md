# CLAUDE.md — zapgw

*[Leia em português](CLAUDE.pt-BR.md)*

A gateway for Meta's messaging APIs (WhatsApp Cloud API and Instagram DM) — multi-tenant, static
binary.

Created on 2026-08-20.

## What this repository is — and the rule it exists to enforce

**It was born empty, on purpose, on 2026-08-20.** It is the clean successor to `iscarelli/zapgw-dev`
— the same project, renamed on the same day, with 692 commits and 72 tags of history. The decision,
the three alternatives and the measured cost of each are in
`zapgw-dev:docs/ESTUDO-ABERTURA-PUBLICA-2026-08-20.md` (Path A).

> **Nothing that identifies a real person or customer goes in here.** Not the owner's phone number,
> not a tenant's name, not a third party's `phone_number_id` / `waba_id` / `ig_id`, not the homelab's
> address. Examples and fixtures use **synthetic** values.

*Why this is the first section and not a footnote:* `zapgw-dev` carries the owner's phone number in
**36 files** and real customer names in **34** (recounted on 2026-08-20), spread over 692 commits —
and **there is no unpublishing**. It was that cost that made the project be born again instead of
rewriting the history. A single occurrence that gets in here by carelessness reproduces the whole
problem, and the repo loses the only property that justifies its existence.

## The four decisions of going public — taken on 2026-08-20

None of them is mine to reopen. The full reasoning and the measured cost of each alternative are in
`zapgw-dev:docs/DECISAO-ABERTURA-PUBLICA-2026-08-20.md`; only what counts as a rule stays here.

| | decision | practical consequence |
|---|---|---|
| **License** | **AGPL-3.0-or-later** (`LICENSE`) | the product is a server: the network clause is the point. The owner is the sole holder, so relicensing later remains possible — the AGPL -> permissive path exists, the reverse does not. MIT was refused for being irreversible. |
| **Language** | the code goes to **English**; documentation in **PT-BR and EN** | this repository's documents got their `NAME.md` (EN) + `NAME.pt-BR.md` (PT) pair on 2026-08-30. The code has not arrived yet: translating it goes in the same single mechanical pass as the first code, never after publication. |
| **Third parties** | **nothing from a consumer** goes public | it is the reason this repo exists instead of the old one being made public. |
| **Name** | **stays `zapgw`** | that is what the old repo became `zapgw-dev` for: to free up the name. |

🔴 **The order is mandatory, and the reason is mechanical:** *name settled -> single mechanical pass
(rename + English) -> make public*. After publication, the name is in the binary the `update_script`
looks for, in the config path and in the unit — changing it **breaks everyone's installation, in
silence, on update**. The name is already settled; the pass is what is missing.

✅ **2026-08-30 — the trigger for going public is defined, and it is an EVENT, not a date.** Owner's
decision: *"trabalhar na migracao do codigo; quando subir o primeiro release la, transforma em
publico."* ("work on migrating the code; when the first release ships there, make it public.") The
sanitized tree migrates here **while this repo is still private**, the first release ships from here,
and **only then** does it become public.
🔴 **The consequence that rules everything:** THIS repository's history will not be rewritten, and
will be published as it stands. So **no commit with a real customer name or a Portuguese comment can
arrive here** — the sanitization and the translation happen in `zapgw-dev`, BEFORE the migration, and
the first code commit here is born clean. *There is no "fix it in the next commit" here: the wrong
commit stays.*

**State today: the code is here, the repository is PUBLIC, and `v0.60.1` is released.** The chosen stack is Go (static
binary, `CGO_ENABLED=0`), inherited from `zapgw-dev`; the verify commands go into the section below
**together with the first code**, not before.

## Hard rules — and what makes each one FAIL

This repository is born after thirteen months of project concentrated into one month, and the reason
it exists separately is not to repeat what already cost dearly in `zapgw-dev`. So the rules below do
not come from principle: **each one has a real cost behind it, and each one has a column saying what
makes it fail.**

🔴 **The criterion that rules here, and it is the most expensive lesson of 2026-08-20:** *a rule with
no mechanism is decoration, and a mechanism that has never failed anything is indistinguishable from
a mechanism that does not look.* On that day, **three measurements were made with a "positive
control" and two of them were wrong** — the two validated the instrument instead of the data. What
caught the two was a test that really did fail.

**Therefore, to count as a mechanism here, a test has to have failed once, against real data, and
that has to be on the record.**

| rule | what makes it fail | state |
|---|---|---|
| No data identifying a real person (phone number, customer name, third party's Meta id, internal address) | a **phone-number allowlist** test (`internal/config/telefones_allowlist_test.go`, T-191) plus a **customer-name deny-scan** (`internal/config/nomes_allowlist_test.go`, T-193) — both sweep every file `git` sees from the module root (tracked + untracked-and-not-ignored) | ✅ **the phone gate's exemption list is EMPTY**. It has failed against real data three times — twice on 2026-08-30 (a number in a temporary file, one hidden in base64 inside a markdown code block) and once on 2026-08-31 (a number in a file created at the repository root, T-191's positive control). ✅ **the name gate has NO allowlist at all** — its needle list lives OUTSIDE the repository (`ZAPGW_FORBIDDEN_NAMES` or `~/.zapgw/forbidden-names.txt`), and it FAILS (never skips) if neither source is loadable. It has already failed against real data once, on 2026-08-31, against the tree this same commit cleans (T-193). ⚠️ **A third party's Meta id (`waba_id`, `ig_id`, `phone_number_id`) still has no gate** |
| TLS has no off switch, in either direction | a test that sweeps the whole tree for the option, with the needle assembled by concatenation so it does not flag itself | exists in `zapgw-dev` (`internal/inbound/deliver_test.go`) — **migrates with the code** |
| A new route declares per-tenant isolation | an isolation table that reads the package's `mux.HandleFunc` calls and requires a declared literal | exists in `zapgw-dev` — **migrates with the code** |
| A handler declares which instance types it accepts | a **mandatory positional** parameter: omitting it **does not compile**, and the zero value is the most restrictive one | exists in `zapgw-dev` — **migrates with the code** |
| The verify runs before every merge | **CI** with the four commands in separate steps, `timeout-minutes: 10`, and no step piping into `grep` | ✅ **exists** (`.github/workflows/verify.yml`), born together with the first code — carries both irreversible gates, phone and name, each in its own step, with `ZAPGW_FORBIDDEN_NAMES` declared as an `env:` at the JOB level so both the name gate's own step and the whole-package `go test ./...` see it (T-195). 🔴 **And it has RUN here, measured on 2026-08-31:** four runs on the recreated repository — the first three **failed** on the name gate ("could not verify", no needle source) and the fourth passed with all four gates green (run `33355320803`). So the CI itself has now failed against real data before being trusted. ⚠️ **The story that the Actions quota was exhausted until 2026-09-01 is dead:** the runs executed on 08-31. That account had already been wrong twice; this is the third correction, and the lesson repeats — *a claim about capacity that nobody re-measured is a claim, not a state* |
| A secret never in the repository | `.gitignore` since the initial commit; a per-project hook | partial — the `.gitignore` is here; the hook is not |
| A subsystem doc starts with `Código:` | **nothing** | 🔴 **does not exist** |
| The published state (version live, what is in flight) does not lie | **nothing** | 🔴 **does not exist** — the equivalent line in `zapgw-dev` lied **twice in sixteen hours**, the second time after the warning written just below it |

### The three foundations — two of them now have a mechanism, and one still does not

1. **CI — it exists here, it runs here, and it has already failed against real data.**
   Four runs on 2026-08-31, on the recreated repository: the first three **failed** on the name gate
   (*"could not verify"* — no needle source reached the runner), and the fourth passed with all four
   gates green (run `33355320803`, ~1m30s). That order matters more than the green: a gate that has
   never rejected anything is indistinguishable from a gate that does not look, and the same is true
   of the pipeline that carries it.
   It first existed in `zapgw-dev` from 2026-08-21 to 2026-08-29 — four separate steps, the Go
   version read from `go.mod`, and a `gofmt` step that turns non-empty output into an error, since on
   its own `gofmt -l` prints and exits `0`. It came back here restored from
   `git show 709e915:.github/workflows/verify.yml`, and grew the two gate steps.
   🔥 **This paragraph has now lied three times, each in a different way, and that is why the record
   stays.**
   (1) It said *"done in `zapgw-dev` and migrates with the code"* after the file had already been
   deleted there — **a statement about ANOTHER repository's state, which goes stale without telling
   anyone**.
   (2) The correction swapped that for *"a private repo pays for Actions; owner's rule: CI only on a
   public project"* — **an invented cause wearing the costume of a rule**, which propped up a
   recommendation to bring the public opening forward to "get CI for free". The owner corrected it on
   2026-08-30: a quota blown by another project, with a reset date.
   (3) And that reset date was written here as fact — *"comes back on 2026-09-01"* — and repeated into
   `docs/TASKS.md` on 2026-08-31 as *"the quota only resets on 09-01"*. **The runs executed on
   08-31.** Nobody re-measured the capacity; the date was carried forward from the owner's
   explanation and hardened into a state.
   ➡️ *The family is one: **a claim about something you do not control goes stale silently.** Another
   repository's state, an invented cause, a quota window — none of them fail loudly when they stop
   being true. Write the symptom, the date, and how it was measured.*
   *Worth keeping, because it was never explained:* an earlier Actions attempt on this account
   (2026-08-06) died twice — job `cancelled`, empty `runner` field, and **exactly 905 seconds** both
   times. The runs of 2026-08-21 finished in ~2 minutes. 🔴 **What unblocked it was never discovered;
   it was only measured that it worked that day.**
2. **The personal-data gates run in CI**, not only as local tests — because the cost here is
   irreversible: published once, there is no unpublishing. Both have their own step
   (`portao de dado pessoal`, `portao de nome`), and the name gate's needles reach the runner as the
   repository secret `ZAPGW_FORBIDDEN_NAMES`, declared as an `env:` at the **job** level so the
   whole-package `go test ./...` sees it too.
   ⚠️ **A PR from a fork gets no secret**, so the name gate fails there saying it could not verify.
   That is deliberate, and the workflow says so in a comment: turning it into a skip would buy a
   green tick with the exact blindness the gate exists to prevent.
3. **A state that proves itself.** 🔴 **Still no mechanism.** Any line in this repository claiming
   "version X is live" either has to be measured on the spot, or not be written. *A resumption block
   that lies is worse than no block: it is the first text the next session reads.* The item above is
   the argument for this one — three lies, all in text that looked settled.

### What is NOT a hard rule, and why saying so matters

Style, taste and preference do not go on this list. It is kept short on purpose: a list of hard rules
with thirty items is a list nobody reads, and the first person in a hurry abandons it entirely. **If a
rule has no real cost behind it and no mechanism in front of it, it is not hard — it is a preference,
and its place is in review, not here.**

## Verify commands

    CGO_ENABLED=0 go build ./...
    go test ./...
    go vet ./...
    gofmt -l cmd internal      # must not print anything

**It is `gofmt -l cmd internal`, not `gofmt -l .`** — and the difference is not cosmetic. Implementer
agents' worktrees live in `.claude/`, **inside the repo**. The `go` command ignores directories
starting with a dot; `gofmt` does **not**. With `-l .` it lists a half-written file from another
process, and the verify starts crying wolf — which is the fastest way to train someone to ignore
gofmt's output.

The `CGO_ENABLED=0` in the build is not decorative: this project's artifact has to be a **static
binary** (that is the very reason the stack is Go), and without pinning the variable a future
dependency can turn cgo on silently, breaking that guarantee without any test flagging it.

**Run the verify before committing.** If it fails, fix it or do not commit — never commit with the
verify red "to sort out later", and **never disable the test that flagged it**. A verify section
nobody runs is decoration, and decoration describing a nonexistent mechanism is a false doc.

### What the verify does NOT reach

It does not talk to Meta and does not talk to any consumer. Every guarantee that depends on the real
network — a valid certificate on the public hostname, a webhook actually arriving, a token accepted by
the Graph API — is **structurally unverifiable** by this suite. **A green suite does not replace those
proofs**, and the only thing that produces them is real traffic.

### 🔴 The three tests that are a GATE, and what makes them different from the others

They do not prove the code works: they prove an irreversible decision was not violated. All three
**have already failed against real data**, and that is why they count as mechanisms.

- **`internal/config/telefones_allowlist_test.go`** — sweeps every file `git` sees from the module
  root (every tracked file, plus every untracked-and-not-ignored one — T-191, so a new file anywhere,
  including the repository root, is covered without anyone editing this test) for an undeclared phone
  number, **decoding the base64 of every `wamid.`**, because the `wamid` carries the recipient's phone
  number inside it and a `grep` for the number as a human writes it passes straight over them.
  🔴 **This repository's exemption list is EMPTY, and it has to stay that way.** In the private
  repository this code came from there were five, all from registration documents that stayed there.
  Here there is no agreed case: an exemption is a real phone number on the internet, forever.
- **`internal/config/nomes_allowlist_test.go`** (T-193) — sweeps the same set of files for a customer
  name, case-insensitive, at a word boundary. It has **no allowlist and no per-file exemption at
  all**: any match is a finding, full stop. The needle list never lives in the repository — this is a
  public repository, and writing the list of forbidden names inside it would publish exactly what the
  gate exists to keep out. It comes from the `ZAPGW_FORBIDDEN_NAMES` environment variable, or from
  `~/.zapgw/forbidden-names.txt` outside the tree; if neither produces at least one needle, the test
  **fails saying it could not verify** — a message deliberately different from a finding, so the two
  outcomes never collapse into the same color.
- **`internal/inbound/deliver_test.go`** — sweeps the tree for any way of turning TLS verification
  off, with the needle assembled by concatenation so the test does not flag itself.

Two properties the verify needs to have, and which only show up when someone tries to use it:

- **It must not mutate state.** Running the verify twice in a row has to give the same result and
  leave no trace — no version bump, no writing to the production database, no publishing anything. If
  some command does mutate, say so in the section itself.
  Watch out for the case that disguises itself: a script that **checks and fixes** (restores the
  missing cron, resends the alert) is not a verify, it is a remediation routine. Running it "just to
  check" touches production. Either it gets a read-only mode (`--dry-run` / `NO_NOTIFY=1`) and that
  mode becomes the verify, or the section says in capital letters what it changes on every run.
- **If the project has a deploy, the verify goes past it too**: what to check to know it really went
  up (expected HTTP status, the version showing on the page, and how much error time is normal in the
  restart window). A verify that stops at the build answers "the code compiles", not "the change
  works".

## Documentation rules

A wrong doc is worse than no doc. When in doubt between writing something you have not confirmed and
writing nothing: **do not write**.

- Every subsystem doc starts with a `Código:` header listing the files it describes. That way "which
  doc did my change break?" becomes mechanical:
  `grep -rl "file_i_touched" docs/subsistemas/`
- A new doc without `Código:` at the top does not get in.
- **`Código:` accepts `host:path`, not just a repo path.** A lot of what the system depends on does
  not live in the repository: `o LXC do Traefik:/etc/traefik/traefik.yaml`,
  `host .16:/etc/cron.d/unifi-threats`. Without that form, everything that changes without going
  through a commit is invisible to the doc — and that is exactly what nobody remembers to check. When
  there is no code at all, write `Código: nenhum` with the reason (e.g. "the inventory is live
  state"), so the absence has a verdict.
- **Retiring something means removing BOTH ends: the artifact and the trigger.** A script deleted with
  the cron still pointing at it, a job removed with the alert still configured — the orphan survives
  silently. Real cost already paid in this workspace: an orphan cron for three weeks.
- **A blind monitor that answers OK is worse than no monitor.** It holds for any check: it has to
  distinguish *failed* from *could not verify*. Both become "no alarm" if nobody separates them, and
  the second is the one that deceives — you believe you are covered.
- `docs/SUBSISTEMAS.md`, once it exists, is mandatory reading before grepping and before designing —
  and that order lives on the FIRST line of `CLAUDE.md`, not in the middle: only `CLAUDE.md` enters
  the agent's context on its own, the map does not.
- A bug a trap would have avoided becomes a line in `docs/ARMADILHAS.md`, **in the same commit as the
  fix**, with the real cost it charged. It is the only moment when the cost is fresh.
- **Mark the ones that have already charged, separating them from the ones merely confirmed.**
  Requiring a real cost from every trap has a perverse effect: whoever found the mechanism but was
  never bitten by it either invents a cost or does not write. Use a visible mark (🔥) for "it has
  already cost, and the cost is written down" and leave the rest unmarked, with the mechanism
  confirmed in the code and a "has not charged yet". That way both get in, and the reader knows which
  ones really hurt.
- **When recording a trap, look for its siblings before closing.** If the problem is a way of reading
  or rewriting a piece of data (regex, replace, sed, parser), sweep the other places that touch the
  SAME data and say, in the entry itself, which ones have the hole and which do not — whoever reads
  the first one is going to ask that anyway. A real case: the trap was a `.replace()` that swaps every
  occurrence; the sweep found that the reader of the same constant used `re.search` and returned the
  FIRST occurrence, a second hole for the same reason, and that the third consumer used a `sed`
  anchored enough to be safe. A trap documented on its own hides its siblings.
- A doc that describes the system does not have a date in its name. A date in the name authorizes
  rot. ADRs and dated specs are history: they are not updated.
- Check every statement against the code, never against the old doc. Point at `file:line`.
- Say why, not just what. Write the mistakes that actually happened, with the damage.

To audit/redo this project's documentation, use the 5-phase prompt in
`<root>/github/docs/PROMPT-auditoria-documentacao.md`. The doctrine in summary is in
`<root>/github/docs/DOCUMENTACAO.md`.

**The rules above are a snapshot of the doctrine in that file, frozen at this `CLAUDE.md`'s creation
date (at the top).** The source is there, not here: in a conflict, it rules, and a new rule arrives
through it — this file does not get updated on its own. If the project lives under `C:\dev` / `~/dev`,
the root `CLAUDE.md` already loads the workspace rules in every session; the copy above exists for the
case of the project leaving that tree (handed to another person, cloned elsewhere), when it is the
only thing left.

## Secrets — never in Git

A token, key, password, credential or certificate **never** goes into the repository. This is the only
rule here whose violation is irreversible: committed once, the secret lives in the history forever,
and the fix is not `git rm` — it is **rotating the secret**.

- Secrets live in ignored files (`.env`, `secrets.*`, `*.key`, `.gh_token`), ignored **since the
  initial commit**.
- The repository documents, for EACH secret, all three: **which** one it is, **where it lives** on
  disk, and **where to get** a new one (which panel, password manager, `secrets-transfer/`). The first
  two without the third rescue nobody: whoever cloned on a new machine finds out a token is missing
  and still does not know where to get it.
- The third item changes nature depending on the origin, and confusing the two blocks people for
  nothing: a **secret issued by a third party** (the provider's panel) requires knowing the exact path
  to it, because only from there does a valid one come; a **secret of ours, arbitrary** (a token we
  made up to serve as a gate) has no origin to discover — what it needs is the swap recipe: generate a
  new one, update it here and here, restart what reads it. Marking a secret of ours as "origin to be
  confirmed" scares people for no reason; what was missing was the recipe.
- A fourth column pays for the table on its own: **what rotating BREAKS** — the blast radius. Swapping
  a webhook secret without re-registering the webhook kills the link silently; swapping the signing key
  turns all already-encrypted data into garbage. Without that column, rotation looks like a
  single-step operation, and it is not. If the same secret appears in several places, say in how many
  and which.
- Every secret file has a versioned `.example` companion, with **literal placeholders**
  (`troque-pelo-token-do-x`), never real values nor anything resembling real ones.
- Transport between machines is via `<root>/github/secrets-transfer/`, not by commit, not by chat.
- If you notice a secret has been committed: **stop everything and tell the programmer immediately**.
  Do not try to clean the history on your own.

## Versioning — SemVer

This project uses **SemVer**: `MAJOR.MINOR.PATCH`.

- **MAJOR** — breaks compatibility. **A MAJOR decision always goes through the programmer before it is
  made.** Never bump MAJOR on your own: stop, explain what breaks and ask.
- **MINOR** — new functionality, backward compatible.
- **PATCH** — backward-compatible fix.

**Each version has exactly one source.** Do not duplicate the same number in two files — two sources
diverge. The source is a `VERSION` file at the root — `go.mod` does not carry the module's own version.

That does NOT mean the project has a single number. If it has more than one **front** (surfaces, apps
or services delivered separately), each front has **its own** version, independent — and the rule is
still satisfied, because each number has one source. See "A project with more than one front" below.

The git tag follows: `vMAJOR.MINOR.PATCH`, created on the commit that bumps the version.

`docs/CHANGELOG.md` is grouped by version. Each bump opens a new header, and retired tasks go in as
bullets under the version they shipped in:

```markdown
# Changelog

## v0.2.0 — 2026-07-19
- **Task name** (T-002) — result in one line. _Completed 2026-07-19 14:30._
- **Another task** (T-003) — result in one line. _Completed 2026-07-19 16:05._

## v0.1.0 — 2026-07-18
- **First task** (T-001) — result in one line. _Completed 2026-07-18 09:12._
```

Most recent version at the top. Tasks already finished but not yet released sit under a
`## Nao lancado` header until the next bump.

**The changelog is born the moment the task is retired — never reconstructed later from `git log`.** A
retroactively generated changelog is guesswork wearing the costume of a record: the reader trusts it,
and it does not know what each commit actually meant. If the file diverges from what happened, delete
the false part — do not patch it.

Before 1.0.0 the project is unstable and MINOR may break; when publishing/delivering to someone, go to
1.0.0.

### A project with more than one front

A front = each surface, app or service that is delivered and changes at its own pace (a site, a
dashboard, an API, a delivery app). **Separate versions per front are this workspace's preference** —
do not consolidate into a single number. The gain is diagnostic: when someone reports a bug, that
surface's footer says exactly what the person is running. With a single number, every footer lies
about the other fronts.

If this project is born with more than one front (ask, if it is not clear), write it in CLAUDE.md like
this:

- **List the fronts in a table** with four columns: *front · current version · source (file:line) ·
  how it bumps (automatic on build / manual / deploy script)*. The "how it bumps" column is the one
  nobody remembers to write and the one that matters most — see the point about automatic bumping
  below.
- **If the number's bump is automatic, the note has to be automatic too** — or assume in writing that
  the notes will be sparse. A number that goes up on its own plus a note that depends on human
  discipline is a combination that always ends the same way: a real project in this workspace has 8
  fronts with automatic bumping and **7 of them with zero notes**, while the only one with a manual
  bump has notes. Automate both or neither.
- **A fallback constant protected by `#ifndef` (or equivalent) is not a second source** — it is a
  default for when the real one was not injected. Do not treat it as a violation of the single-source
  rule, and do not "fix" it.
- **The release notes stay glued to the front's number**, not in a separate file — one line per
  version, ascending order, the most recent next to the constant:
  `# <version>: <the effect of the change, with the task's [id] when there is one>`
  Adjacency is what makes the note get written: it is impossible to bump the number without seeing the
  list.
- **The note goes in the SAME commit as the bump.** Bumping a version without a note is a process bug
  — it is the only moment when what changed is known.
- **Do not create a single changelog mixing the fronts.** "## v2.3.6" of which one? And do not have
  both (a note next to the number AND a per-front file): two records diverge.
- **Tags:** in a multi-front project, a single repo tag means nothing. Decide explicitly between a tag
  per front (`galeria-v1.10.1`) or no tags at all, and **write the decision down**. An orphan tag is
  worse than no tag: a repo full of version tags implies they track the versions, and whoever trusts
  that gets it wrong.

When ONE front's history goes past ~30 lines, the versions file stops being config and becomes a
document: at that point, ask the programmer before migrating that front to
`docs/changelog/<front>.md`. Do not migrate on your own.

## Vikunja (MCP) — hard rules of use

The Vikunja MCP is only for reading/writing tasks. It costs tokens; use it with discipline. These
rules are mandatory:

- Calling `vikunja_list_projects` is FORBIDDEN in this project. The ID is fixed: **33**. Use it
  directly in every call.
- To see pending items use `vikunja_get_tasks(project_id=33, done=false)` — it returns only
  title/status. Do NOT pull the description of several tasks "just in case".
- `vikunja_get_task(task_id)` only on the task you are going to work on NOW, and at most ONCE per
  task. Re-fetching a task that is already in context is forbidden.
- NEVER use `vikunja_delete_task` to "close" a task. Done =
  `vikunja_update_task(task_id, done=true)`. Delete only when the task is real garbage and the user
  says so.
- The task's description is the record of the work (spec + result): write there, but do NOT echo the
  whole description back into the chat — summarize it in one line.
- Every pending item that comes up becomes a task right away; every resolved one is marked
  `done=true` right away. Do not pile them up for later.

## Workflow

The planner/implementer flow, the format of `docs/TASKS.md` and the documentation doctrine come from
the root `CLAUDE.md` (`C:\dev\CLAUDE.md`) and hold here without repetition. This file only adds what
is specific to this project.
