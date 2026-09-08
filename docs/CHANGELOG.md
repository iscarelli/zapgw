# Changelog

One line per version shipped, in the same commit as the bump. The entry states the **effect**, not the diff.

## Unreleased

- **T-237 — The consumer contract still describes four families of key the gateway stopped
  emitting** — the worst was the discriminator of **every** webhook event: the code emits `kind` with
  value `message`, the doc said `"tipo": "mensagem"`. The implementer widened past the 12 places the
  planner had measured, correctly: `tipo` is one field shared by all six event kinds, so fixing only
  `mensagem` would have left the doc contradicting itself. It also fixed the status error sub-object,
  the `GET /v1/estado` ingress/watchdog vocabulary, and three of the four keys in the `/v1/bloqueios`
  success body — leaving `operacao`, which really is Portuguese in the code. A global replace would
  have written a lie in exactly that cell. Surfaced a **fifth** family, re-measured by the planner
  and queued as T-240. _Completed 2026-09-08 04:41._


- **T-238 — Close the doc-pointer gate's bare-filename hole** — the gate required a directory
  component, so a fixture cited by bare name was invisible to it; seven dead pointers were hiding in
  exactly that gap. Step 0 excluded `docs/CHANGELOG.md` from the sweep structurally — a changelog is
  a record, and "fixing" it to please a gate fabricates history — which alone returned `main` to
  green. The seven were fixed against the T-228 rename commit rather than guessed, and two written in
  ellipsis shorthand were expanded to full paths. Widening surfaced **31** findings rather than 7,
  most of them legitimate false positives now carrying a written reason (glob patterns, a `.json()`
  method call, a gitignored channel file, workspace docs cited with backslashes, third-party repos).
  The limits block was updated in **both** directions: the retired limit removed, two new ones added.
  One real fix was deferred rather than written over a concurrent implementer's file — see T-239.
  _Completed 2026-09-08 04:19._


- **T-230 — Rename the Portuguese directories and script names** — `cmd/grafo-falso/` →
  `cmd/fakegraph/`, `implanta/` → `deploy/`, `valida-lideranca.sh` → `check-leadership.sh`, all by
  `git mv`, plus the binary's own log prefix. The referencer sweep ran BEFORE the rename, which is
  the whole guarantee: the inverse order breaks silently and only surfaces the next time somebody
  runs the script. It updated the T-235 coupling gate too — that gate reads the scripts **by path**,
  so renaming the directory without touching it would have made it answer *could not verify*; the
  positive control's synthetic tree needed the same fix or the control would have broken against the
  corrected logic. Left alone on purpose: citations of the **private** repo's `implanta/` (which was
  not renamed) and a few verbatim historical quotes reproducing output from before the rename.
  The owner's out-of-repo wrapper `~/.zapgw/deploy-zapgw.sh` was updated by the planner in the same
  movement — three lines, with the line pointing at the **old private repo** deliberately untouched.
  _Completed 2026-09-08 03:52._


- **T-236 — Fix the two dead doc pointers the widened gate found, and DELETE their exemptions** —
  the point was never the two edits: it was that both had been parked in
  `deadDocPointerExceptions` marked `KNOWN PRE-EXISTING BUG` so the gate would go green. An
  exemption like that is honest on the day it is written and indistinguishable from noise three
  months later. Both are now fixed and both exemptions are gone, proved by the inverted check
  (`grep -c 'KNOWN PRE-EXISTING BUG'` → 0) — the gate is green **because the bug is gone**, not
  because it was listed. It also fixed more than the gate had named: the line the gate flagged cited
  three renamed fixtures and the gate could only see one, because the other two were written
  abbreviated. _Completed 2026-09-08 03:34._


- **T-222 — Fix the error vocabulary the consumer contract documents** — the contract documented the
  error as `classe` with `permanente`/`retentavel`/`desconhecido`; the gateway has emitted `class`
  with `permanent`/`retryable`/`config`/`unknown` since T-209. Corrected across
  `docs/CONTRATO-CONSUMIDOR.md` (111 lines), its pt-BR mirror (114), `docs/INVENTARIO-VALORES.md`
  section 1.6 and two `docs/ARMADILHAS.md` entries that asserted the old vocabulary as **current**
  behaviour. The whole error body was wrong, not just those four words: `erro`, `codigo_meta`,
  `mensagem`, `detalhe_meta`, `subcodigo_meta`, `explicacao_meta` and `rastro_meta` were fixed too.
  `docs/MIGRACAO-CONTRATO-EN.md` was deliberately left alone — every occurrence there is a migration
  row citing the old form on purpose. The implementer caught and reverted two of its own over-broad
  replacements that had hit ordinary Portuguese prose. **Surfaced four further families of contract
  key the doc still gets wrong** — now T-237, with every pointer re-measured by the planner because
  one of the four was reported incorrectly. _Completed 2026-09-08 03:22._


- **T-234 — Widen the doc-pointer gate past `.go`** — the gate now covers `.json`, `.sh`, `.md`,
  `.yml`, `.service` and `.txt`, from a list derived by grepping what `docs/*.md` actually cites
  rather than "anything with a dot". False positives are handled **structurally** wherever possible —
  an absolute path is out-of-repo, which settles the `host:path` form CLAUDE.md authorises, `~/`,
  `/etc/`, and even URLs in one rule — with a per-case text exemption only for what is left. It
  writes its own limits into the test's doc comment, because the hole it fixes existed precisely
  because the gate's width was invisible from outside. Proved against real data: a fabricated pointer
  makes it fail naming `doc:line`. **It immediately found two genuine dead pointers** that the manual
  sweep after T-228 had missed — which is the whole argument for having it. _Completed 2026-09-08 03:04._


- **T-232 — Translate docs/CHANGELOG.md to English** — 24 blocks of Portuguese prose translated,
  including the `## Nao lancado` header, which became `## Unreleased`. Nothing any entry ASSERTS was
  changed: a changelog is a record, and correcting a record retroactively is inventing history.
  Invariance proved on both sides and re-checked by the planner: same bullet count and an identical
  multiset of task ids before and after. Everything that survives in Portuguese is a literal — the
  `sim`/`nao` contract values, quoted program output like `NAO CONSEGUI LER`, and the filename
  `contrato-chaves-que-nao-mudam.txt`. _Completed 2026-09-08 02:53._


- **T-235 — Fix the two shell/Go log couplings, and build the gate that has never existed** — fixed
  `valida-lideranca.sh` (grepped a leadership line the Go stopped emitting at T-219, producing a false
  `FAILED`) and `deploy.sh` (grepped the obsolete-env-var warning the Go stopped emitting at T-224,
  which had silently removed that warning from every deploy). **The product is the gate**:
  `internal/config/shell_log_coupling_test.go` reads every `implanta/*.sh`, extracts the literals
  marked `# zapgw:log-coupling "…"` and requires each to still exist under `cmd/` or `internal/`; zero
  markers is a hard *could not verify*, never a pass. The sibling sweep gave a verdict on all 13
  `grep`/`case` points — 8 real couplings, marked; 5 not couplings. Failed against real data twice
  (implementer, then re-proved by the planner), and carries a permanent positive control.
  _Completed 2026-09-08 02:41._


- **T-227 — Translate the Portuguese comments left in cmd/** — 12 files. T-219 had translated `cmd/`'s
  strings; this finished the comments and test messages. The CLI verb spellings, the flag names, the
  `ALARME`/`PRECISA DE GENTE` prefixes and the `sim`/`nao`/`ativa`/`pausada` contract values stayed,
  each checked against the package that emits it rather than from memory. 🔥 **Found two more vacuous
  guards** — `menu_test.go` searching `-h` output for `desconhecido` and `provision_test.go` searching
  for `o valor NAO e mostrado`, both translated by T-219 the day before, so neither check could fire.
  That makes **four in one day**, which is what turned the pitfall entry from "renames break guards"
  into "asserting on a string you do not own is a guard with an unwritten expiry date".
  _Completed 2026-09-08 02:11._


- **T-229 — Translate the deploy scripts, the CI and the env example** — 7 files: `implanta/deploy.sh`,
  `profile-zapgw.sh`, `valida-lideranca.sh`, `zapgw.service`, `.githooks/pre-push`,
  `.github/workflows/verify.yml`, `.env.example`. Comments and operator-facing output in English;
  every string compared by `case`, `grep` or equality stayed, because translating one in shell makes
  the branch stop matching and **nothing warns**. `deploy.sh` was never executed — the proof for this
  task is static by design. 🔥 **Found two real regressions and deliberately did not fix them** (out
  of its scope, now T-235): `valida-lideranca.sh` greps for a leadership log line the Go stopped
  emitting at T-219, and `deploy.sh` greps for the obsolete-env-var warning the Go stopped emitting at
  T-224 — the second one silently removed that warning from every deploy.
  _Completed 2026-09-08 01:38._

- **T-228 — Rename the Portuguese test fixtures to English** — 39 fixtures in `testdata/corpus/` plus
  `internal/inbound/testdata/assinatura-entrega.json` → `delivery-signature.json`, all by `git mv`
  (38 renames detected by git, history preserved). **Zero bytes of JSON payload changed** — the corpus
  is what Meta actually sends, and an edited value there is a corpus lying about its source. 8 Go
  readers updated and `testdata/corpus/README.md` translated (665 → 693 lines), including the Go
  identifiers it cites, which T-223..T-226 had renamed. Left four dead pointers in `docs/` that the
  doc-pointer gate could not see, because that gate only matches paths ending in `.go` — pointers
  fixed by hand in the same movement, gate widening queued as T-234.
  _Completed 2026-09-08 01:25._


- **T-233 — Re-align the tests that pin the old Portuguese deprecation warning** — realigned the 11
  assertions in `cmd/zapgw/env_aliases_test.go` (6), `internal/outbound/ingress_test.go` (2),
  `leadership_test.go` (2) and `external_probe_test.go` (1) that pinned the old Portuguese spelling of
  `config.WarnOldEnvVar`'s message, plus the Portuguese subtest names next to them. The translated
  message itself was NOT reverted — it was correct; the assertions were what had drifted. Returned
  `main` to green after T-224's merge left it deliberately red. _Completed 2026-09-08 01:02._

- **T-226 — Translate the remaining Portuguese in internal/outbound** — 52 files, +1926/−1924 lines,
  the largest package of the batch and the only one that touched nothing outside itself. Every wire
  literal stayed: the `respondError` messages, the error texts `Validate()` returns into the response
  body, both columns of every pair in `input_aliases.go` (only its comments were translated), the
  Portuguese `json:` tags, and the ~30 `ALARME`-prefixed lines that are shared grep vocabulary with
  `cmd/`. Carried two fixes that are not translation: **two leak guards that pinned field names
  renamed long ago and therefore passed vacuously**, and ~150 comments quoting output vocabulary the
  gateway stopped emitting at T-209. _Completed 2026-09-08 00:41._

- **T-225 — Translate the remaining Portuguese in internal/inbound** — 12 files, 390 lines. The TLS
  gate's needle, assembled by concatenation so the test does not flag itself, was not touched, and its
  24 tests stayed green. The HTTP error texts stayed in Portuguese with a measured reason: they are
  published in `docs/CONTRATO-CONSUMIDOR.md:3886` as the literal response body, so they are contract,
  not human text. _Completed 2026-09-07 23:24._

- **T-224 — Translate the remaining Portuguese in internal/config** — 21 files, 990 lines. The name
  gate still fails CLOSED and still distinguishes "failed" from "could not verify", now in English —
  proved against a fake `USERPROFILE` so the real needle file was never touched. SQLite table and
  column names, counter keys, the `entrada`/`saida` direction values and the HMAC domain-separation
  prefix stayed in Portuguese: changing the last one would invalidate HMACs already computed in
  production. Its merge deliberately left `main` red — see T-233 and the pitfall it produced.
  _Completed 2026-09-08 00:11._

- **T-223 — Translate the remaining Portuguese in internal/meta** — 29 of 31 files, 996 lines. The
  event-id prefixes (`template_categoria:`, `qualidade_do_numero:`, `alerta_de_conta:`), the media
  categories, the `[truncado]` suffix and the error texts other packages compare byte for byte all
  stayed, verified one by one against `docs/CONTRATO-CONSUMIDOR.md`. Also fixed two dead comments: a
  reference to an already-renamed function and a parameter name that no longer existed.
  _Completed 2026-09-07 23:57._


- **T-219 — Translate the operator-facing strings of the CLI** — translated to English the output and
  error strings of `cmd/zapgw/*.go` and `cmd/grafo-falso/*.go` (non-test), with the tests that match
  them adjusted together (13 production files + 9 test files). Confirmed before touching each file
  against `docs/INVENTARIO-STRINGS.md` (T-213): both binaries are LOG by construction, neither reaches
  the consumer. `internal/outbound/`, `internal/config/`, `internal/meta/` and `internal/inbound/` were
  NOT touched — that is where the messages the consumer may be comparing against live (owner's decision
  still pending). Left in Portuguese, deliberately, were the points that are vocabulary SHARED with
  those out-of-scope packages (`PRECISA DE GENTE`, `ALARME zapgw`, the values `sim`/`nao`,
  `nao_configurado`, `nao_se_aplica`) and the ones that are CLI CONTRACT, not human text: the spellings
  of the verbs and sub-verbs (`instancia`, `consumidor`, `provisionar`, `fumaca`, `diagnostico` and the
  like — T-220's territory) and the flag NAMES (`--instancia`, `--telefone`, `--confirmo`, `--tipo`
  etc.), plus the enum values of `grafo-falso`'s `--falha-de-template`, documented in
  `docs/ARMADILHAS.md`. The sweep `grep -rnE '(nao|voce|instancia|segredo|obrigatorio|invalido)'
  cmd/ --include=*.go | grep -v _test` did not come back empty, but everything left over falls into
  one of those two categories. Full verify (`go build`, `go test ./...`, `go vet`,
  `gofmt -l cmd internal`) green.
  _Completed 2026-09-06 21:18._

## v0.65.0 — 2026-09-06

- **T-221 — Accept the English spelling of every top-level request key** — `contacts`, `flow` and
  `sections` now exist; before, a 100%-English consumer still had to send one key in Portuguese, and
  a whole example of the contract in English was impossible to write. Nothing left the wire: the
  Portuguese spelling keeps working and sending both in the same request is still
  `ErrConflictingAlias`. What matters more than the three lines is the inverted gate
  (`TestRequestTopLevelKeysAreAllAccountedFor`): it reads `Request`'s tags and requires an English
  alias or a presence in `docs/contrato-chaves-que-nao-mudam.txt` — the old test walked the table
  itself and so could not see a MISSING row, which is how the three survived T-203. It failed
  against real data twice (removing `secoes`, and then `contacts` again on main).
  _Completed 2026-09-06 21:05._

- **T-218 — English aliases for the CLI sub-verbs** — additive, without removing any Portuguese
  spelling. T-214 had only migrated the 4 TOP-level verbs; this task goes one level down: 10
  sub-verb pairs now accept the English spelling alongside the Portuguese one, with the SAME
  mechanism (`warnOldVerb`) — `rotacionar`/`rotate`, `listar`/`list`, `mostrar`/`show`,
  `pausar`/`pause`, `remover`/`remove`, `registrar`/`register`, `desregistrar`/`deregister`,
  `reabrir-cadastro`/`reopen-enrollment`, `provisionar`/`provision`, `diagnostico`/`diagnostics`
  (`pin` was already English). The error messages that ENUMERATE the known sub-verbs (in
  `instanceCommand`, `consumerCommand` and the top-level `dispatch`) were updated together, or they
  would start lying about what the binary accepts. New test
  `TestDispatchAcceptsEnglishSubVerbsSilently` (`cmd/zapgw/provision_test.go`) proves, for EACH pair,
  BOTH halves: the English spelling dispatches to the same function (compared byte for byte after
  stripping the old-spelling warning from the output) and the Portuguese one keeps dispatching AND
  emitting the T-214 warning.
  _Completed 2026-09-05 21:10._

- **T-216 — The obsolete-name warning never reaches the operator — surface it on a SUCCESSFUL
  deploy** — `implanta/deploy.sh` only showed the boot journal when `/v1/health` FAILED; on the
  success path (the normal case) the T-214 warning about an environment variable with an old name
  stayed invisible. New function `avisos_nome_obsoleto()`, called right after `VERSAO CONFERE`,
  reads the journal and filters just for the warning line ("esta obsoleta -- use") — it never dumps
  the whole journal. Three distinguishable outputs: there was a warning -> shows the lines; there
  was none -> "nenhuma variavel com nome obsoleto em uso"; the journal could not be read ->
  "NAO CONSEGUI LER". Proved without production by extracting the function verbatim from
  `implanta/deploy.sh` (`sed -n '246,259p'`) and exercising it with a simulated `ct()` in the three
  cases (journal with a warning, clean journal, read failing). The FAILURE path was not touched.
  `bash -n implanta/deploy.sh` and the usual verify (`go build`, `go test -count=1 ./...`, `go vet`,
  `gofmt -l cmd internal`) clean. _Completed 2026-08-31 23:07._

- **T-217 — Half the doc pointers are dead after the rename — fix them, and build the gate that
  stops it recurring** — of the 113 `.go` pointers in `docs/*.md`, 50 pointed to a file T-212 had
  renamed; each mapping was confirmed by `git log --follow` (never by a similar-looking name)
  before the fix. New gate (`internal/config/doc_pointers_test.go`, `TestNoDeadGoPointerInDocs`)
  sweeps `docs/*.md` for every `.go` pointer (a path, and an isolated `file.go:line`) and fails,
  naming doc/line/pointer, when the file does not exist; fails closed if the sweep finds no pointer
  at all (`TestDeadDocPointerGateFailsClosedOnZeroPointers`). Proved by a real mutation against
  `docs/MODELO-DE-USO.md` (failed citing `docs/MODELO-DE-USO.md:7`, undone right after) and a
  permanent positive control in `TestDeadDocPointerGateFailsOnAMutatedPointer`. The full-path
  exception list starts empty. _Completed 2026-08-31 23:02._

## v0.64.0 — 2026-08-31

- **T-214 — LAYER 4: `ZAPGW_*` and the CLI accept both names, and count the old one** — additive,
  without removing any old name. **16 `ZAPGW_*` variables gained an English pair** (`BANCO`,
  `CHAVE_CIFRA`, `CONECTOR_READY`, `DIAGNOSTICO_SONDAR_FOLDER`, `ENDERECO`, `ENTRADA_VIA`,
  `LIDERANCA_ARQUIVO`, `LIDERANCA_VALIDADE`, `MAX_CORPO_BYTES`, `SEGREDO_ENTREGA`,
  `SONDA_EXTERNA_URL`, `TOKEN_ENVIO`, `TTL_CONTADORES_DIAS`, `TTL_IDEMPOTENCIA_HORAS`,
  `TTL_TRANSITO_DIAS`, `URL_PUBLICA`) and **4 CLI verbs** (`fumaca`/`smoke`, `instancia`/`instance`,
  `consumidor`/`consumer`, `estado`/`state`) — the other 8 `ZAPGW_*` variables found were already in
  English or are just for local tooling (`ZAPGW_APP_SECRET`, `ZAPGW_VERIFY_TOKEN`, `ZAPGW_PIN`,
  `ZAPGW_GRAPH_BASE`, `ZAPGW_INSTAGRAM_REFRESH_BASE`, `ZAPGW_FORBIDDEN_NAMES`,
  `ZAPGW_PREPUSH_NEW_SHA`, `ZAPGW_PREPUSH_OLD_SHA`), out of scope. The NEW name always wins when
  both are provided (tested pair by pair, including the two leadership-guard names resolved
  independently). The warning — "variavel de ambiente X esta obsoleta -- use Y no lugar (T-214)" —
  goes out through the standard `log`, once per variable per process; **proved with the real
  binary**: started with the 12 old names the server reads on boot, stderr cites the 12; started
  with the new pairs, it stays silent. No old name was removed. Verify clean as always (`go build`,
  `go test -count=1 ./...`, `go vet`, `gofmt -l cmd internal`), with new tests in
  `internal/config/env_alias_test.go`, `cmd/zapgw/env_aliases_test.go` and in the `outbound`/`config`
  packages that already had the variable. _Completed 2026-08-31 16:20._

- **T-213 — LAYER 3, first half: measure which Portuguese strings REACH the consumer** —
  measured, not translated, in `docs/INVENTARIO-STRINGS.md`. Searching only for accented characters
  (the method the original estimate of 207 likely used) found 164 real code strings — but this
  project's biggest source of strings does NOT use accented characters (`"invalido"`,
  `"obrigatorio"`, `"corpo grande demais"`), so I traced the ~226 call sites of
  `respondError`/`logRejection` in `internal/outbound` plus the whole body of `message.go`, which
  alone has **103 `fmt.Errorf`/`errors.New` points in Portuguese, none accented** — confirmed as
  BOTH (same text in the log AND in the response) via 4 different call sites that log a raw
  `err.Error()` before responding with it raw. **~44 CONSUMER-OUTPUT patterns + ~132 BOTH = ~176
  message patterns decide the next step**, more than the original 207 in total code points (~394,
  CLI included), but that (consumer-facing) is the number that matters. A finding nobody would
  expect: ten strings live in a SUCCESS body, not an error one — the `NextStep` field of
  `registration_handler.go` and nine `Warning*`/`Message*` `const`s of `templates_handler.go`, which
  escape a sweep that only looks at `respondError`. A separate finding: 7 strings are built by
  `fmt.Errorf` and never reach anywhere (not the log, not the response) — `auth.go`'s
  `ErrNoToken`/`ErrInvalidToken` and the 4 in `external_probe.go`, whose error is discarded by
  `record()`. Verify clean as always, including the phone gate (the new doc carries none).
  _Completed 2026-08-31 15:39._

- **T-212 — LAYER 1: file names and identifiers stop speaking Portuguese** —
  **86 `.go` files renamed** (`git mv`, history preserved — more than the 69 measured in the spec;
  the original measurement seems to have counted only a subset, and this task measured again by
  reading the current code) and **exactly 38 identifiers** fixed: `translateEntradaOrReject` ->
  `translateInputOrReject` and 37 `TestEntrada*`/`oldNameCounterTodayInEstado` ->
  `TestInput*`/`oldNameCounterTodayInState` in `internal/outbound/input_aliases_test.go`, the only
  place where the word "Entrada"/"Estado" survived in an identifier — the rest of the code was
  already in English before this task began (the number matches exactly the 38 in the spec). Every
  comment that pointed to the old file name was fixed in the same batch (a mechanical
  string-for-string substitution, never touching a `json` tag, a CLI verb, a `ZAPGW_*` name or a
  message). `git diff -U0 | grep 'json:"'` came back empty and
  `TestOutputContractHasNoPortugueseKeyOrValue` stayed green with no logic edit (only the
  `Código:` header of `internal/outbound/english_contract_test.go`, which pointed to two renamed
  files, was updated). Full verify (`build`, `test -count=1`, `vet`, `gofmt`) clean. Found and NOT
  touched, for being outside this layer: the `cmd/grafo-falso` directory (a Portuguese name,
  "grafo falso") — its `.go` files were already called `main.go`/`main_test.go` in English, the name
  only lives in the directory, and changing it would drag two big docs (`docs/ARMADILHAS.md`/
  `.pt-BR.md`) outside this task's "file and identifier" scope. _Completed 2026-08-31 15:11._

- **T-211 — The CI is flaky on a wall-clock test** — `TestHandlerRespectsTheInstanceTimeoutMs`
  now measures the PASSING of the value, not the duration: a fake `http.RoundTripper` that never
  touches the network captures `req.Context().Deadline()` and checks that it falls in the window
  `[before+50ms, after+50ms]`. The first attempt (reading the deadline on the `r.Context()` of a
  real mock server) does not work — `context.WithTimeout` is a client-side local value, it never
  goes over the network — and it hung (`go test -c` + `-test.timeout=10s` showed the server's
  goroutine stuck forever on `<-r.Context().Done()`). Green 20/20 in ~2.5s, no sleeping and no
  clock race. Found two siblings of the same risk (measured, not fixed — out of this task's scope):
  `TestWaitWithContextStopsEarlyIfTheContextIsCancelled`
  (`internal/outbound/templates_handler_test.go:1362`, a 135ms margin) and
  `TestStateRouteDoesNotHangWithTheExternalProbeStuck`
  (`internal/outbound/external_probe_test.go:368`, a 500ms margin). _Completed 2026-08-31 14:50._

- **T-215 — The two sibling flakes** — the two siblings T-211 pointed out and did not fix now prove
  the MECHANISM, not the clock. `TestWaitWithContextStopsEarlyIfTheContextIsCancelled`
  (`internal/outbound/templates_handler_test.go`) cancels the context BEFORE calling
  `waitWithContext` and passes `d=5s`: the internal `select` has only ONE ready path (`ctx.Done()`
  already closed; the 5s timer cannot have fired), so the choice is deterministic by Go's own
  semantics, not a race — the test's `time.After(500ms)` is a hang detector, not the assertion.
  `TestStateRouteDoesNotHangWithTheExternalProbeStuck`
  (`internal/outbound/external_probe_test.go`) swapped the `elapsed > 500ms` ceiling for an atomic
  count of connections accepted by the stuck listener: since `ExternalProbe.Read` only reads a
  struct in memory (no I/O at all), the counter has to stay at zero, and that check does not depend
  on the runner's speed. Green 20/20 on both, `go test -count=1 ./...` clean. Three wall-clock
  assertions remain in the package, reviewed and safe because none is a tight ceiling: a
  lower-bound-only limit (`TestWaitWithContextWaitsTheRequestedTimeWithoutCancellation`, a scheduler
  can only delay, never shorten), a symmetric 1-minute window (`health_handler_test.go:148`), and
  the VALUE (not duration) window that T-211 itself left in `handler_test.go`.
  _Completed 2026-08-31 14:57._

## v0.63.0 — 2026-08-31

- **The contract starts speaking English — a tolerant reader on their side, alias only on ENTRADA
  (input)** (T-189) — the whole contract migration, in four steps and without a single message lost:
  tolerant readers on the consumer's side, the gateway accepting both languages on input (key and
  value), the consumer's writers in English, and the output flip. **Proved against production by the
  consumer:** `"observed"` arrives and turns into `"observado"` at its 7 reading points **without a
  single line of its own code changing**, 80 templates read, zero event stuck. Left out, named: the
  input alias (which stays live, and whose removal is the owner's decision) and the 18 counter names
  (no pair decided — the doc that said otherwise was false). _Completed 2026-08-31 14:16._

- **T-210 — The output sweep is BLIND to the webhook event — fix it before v0.63.0 ships** — root
  cause found: `TestOutputContractHasNoPortugueseKeyOrValue` checks `forbiddenOutputTokens` with a
  flat substring search over the whole marshaled blob, which has no notion of WHERE a key sits; to
  avoid a false positive on `AccountAlert.Type` (legitimately tagged `tipo,omitempty`, left alone
  by T-209 on purpose), the word `"tipo"` was excluded from the list ENTIRELY — which also waived
  `Event.Type`'s own top-level key, the field the table actually renamed (`kind`, row 81). Fix:
  `walkForbiddenKeys` parses each instance's JSON and walks it with a full parent path, so `"tipo"`
  is now checked structurally against `forbiddenKeyExceptions` (a single path-scoped waiver at
  `account_alert.tipo`) instead of being banned everywhere or nowhere.
  **Verify — four mutations, each reproved and reverted:** (a) `internal/meta/types.go:413`
  `Event.Type` `json:"kind"` -> `json:"tipo"` (the exact positive control that found the bug) now
  fails with `a chave "tipo" aparece em tipo — regressao do Event.Type…`, once per event type
  (6 failures); (b) `NumberQuality.State` (nested inside `Event.number_quality`) `state` ->
  `estado` fails with the pre-existing flat check; (c) `EventTypeStatus` value `"status"` ->
  `"estado"` fails the same way; (d) `healthResponse.DisplayNumber` (an HTTP response key, no
  event involved) `display_number` -> `numero_exibido` still fails, proving the fix didn't regress
  response-body coverage. `go test -count=1 ./...` green with the tree fully restored.
  Not a new finding, but independently reconfirmed by probing the marshaled output: the Portuguese
  leftovers T-209's own changelog entry (above) already flagged and left untouched on purpose
  (`AccountAlert.tipo/severidade/id_da_entidade/descricao`, `Billing.cobravel`,
  `status_do_recurso`, `health_handler.go`'s `verificado_em`, …) really do appear in the marshaled
  JSON, exactly as that entry says — they remain OUT of scope for this task (instrument, not
  contract) and are the planner's call, not re-litigated here.
  _Completed 2026-08-31 14:09._

🔴 **THE CONTRACT'S OUTPUT CHANGES: `zapgw` now speaks English on every SAIDA-EVENTO key/value it
delivers to a consumer's `callback_url`, and on every SAIDA-RESPOSTA key/value it returns from an
HTTP route.** Input keeps accepting BOTH languages, unchanged since T-203/T-207/T-208 — this is
step 4 of T-189, and step 5 (removing the input alias) is NOT part of this task and stays a
future, dono-authorized decision.

- **THE FLIP: the gateway's output speaks English — keys and values, in one commit** (T-209) —
  renamed every SAIDA-EVENTO/SAIDA-RESPOSTA key and value the migration table
  (`docs/MIGRACAO-CONTRATO-EN.md`, sections 6–8) lists, and only those: `meta.Event` and its
  nested types (`Reaction`, `Location`, `StatusError`, `TemplateStatus`, `TemplateCategory`,
  `NumberQuality`, `AccountAlert`, `Billing`), `meta.Template`, and every response struct in
  `internal/outbound` (`State` and its whole tree, `RegistrationResponse`, `blockOperationResponse`/
  `blockListResponse`, `templatesResponse`/`templateCreatedResponse`/`templateDeletedResponse`,
  `healthResponse`, `SmokeResponse`, `profileResponse`/`profileWriteResponse`, `PauseResponse`,
  the shared `errorResponse`). Values: the `meta.EventType`/`meta.ErrorClass` constants, the
  observation-state vocabulary (`CertNeverObserved`/`CertObserved`/`NotApplicable`/
  `ConnectorNotConfigured`/`ReachStateCouldNotVerify`), the three `veredito` vocabularies
  (`VerdictRefused`, `VerdictIGTokenWaiting`/`Failing`/`Expired`), the ~50 `respondError` call
  sites that passed `"retentavel"`/`"permanente"` as literals, and `config.CounterOldNameUsed`
  (`nome_antigo_usado` -> `old_name_used`).
  🔴 **The table is the only source: what it does not list did not change.** Confirmed by hand
  against `docs/contrato-chaves-que-nao-mudam.txt` (the 23 keys already in English) and flagged,
  never touched, for every Portuguese-looking field the table is silent on — `AccountAlert`'s
  `tipo`/`severidade`/`id_da_entidade`/`descricao`, `Billing.cobravel`,
  `TemplateStatus.status_do_recurso`, `FieldInRegistration`'s `campo`/`cadastrado`,
  `WindowInRegistration`'s `primeira_insercao_em`/`fecha_em`, `RegistrationResponse.proximo_passo`,
  `blockItemResponse`/`blockFailureResponse.telefone`, `blockOperationResponse.operacao`,
  `blockListResponse`'s `cursor_antes`/`cursor_depois`, `SmokeResponse`'s `ja_estava_ativa`/
  `ativa_desde`, `health_handler.go`'s `verificado_em`, `State`'s `lideranca`/`hoje`/`definido_em`,
  `templateDeletedResponse`'s `entradas`/`aviso`/`releituras`/`espera_segundos`,
  `LeadershipInState`'s `armada`/`titular`, `ingress.go`'s `ViaTunnel`/`ViaPortForwarding` values
  (`tunel`/`encaminhamento_de_porta`), `reads_handler.go`'s `wamid`/`digitando`, and every
  counter name besides `old_name_used` (`recebidas`, `entregues`, `enviadas`, the whole
  `cobranca_*` family, …) — none of these appear in the migration table, so none of them moved.
  🔴 **`cru`/`raw` and the byte-exact content are two different things, and only the KEY moved:**
  `cru` -> `raw` per the table's own row (SAIDA-EVENTO), but the base64 VALUE it carries is still
  the untouched exact bytes from Meta — `TestDeliverSendsTheRawAndTheEventsTogether` proves this
  byte-for-byte via `received.Raw` (a Go field, blind to the JSON tag) and needed no edit.
  **Verify (the gate, not a sample):** `TestOutputContractHasNoPortugueseKeyOrValue`
  (`internal/outbound/english_contract_test.go`) reflectively fills one maximal instance of every
  SAIDA-EVENTO event type and every SAIDA-RESPOSTA body, marshals them, and fails on any of the
  108 Portuguese tokens this task retired — proven against real data by breaking `State.State`'s
  tag back to `estado` and watching it fail, then reverting.
  `TestFrozenKeysStayIdenticalInSource` sweeps `internal/meta`/`internal/outbound` for every key
  in `docs/contrato-chaves-que-nao-mudam.txt` and fails if one goes missing — proven the same way,
  against `profile_picture_handle`. The 36 `TestEntrada*` tests (T-203/T-207/T-208) passed
  UNCHANGED, zero edits to `input_aliases.go`'s dictionaries or `input_aliases_test.go`.
  `ingress_test.go` (T-120's ingress-health block, a same-named but unrelated file) DID change —
  its SAIDA-RESPOSTA mirror structs, not the entrada mechanism.

## v0.62.1 — 2026-08-31

- **Teach the counter to see the thirteen keys it was blind to** (T-208) — `nome_antigo_usado`
  only counted a key that had a PUBLISHED pair; `consumer-b` proved the blind spot against
  production by sending `titulo` (inside `botoes[]`) and the counter never moved. Published the
  pair AND wired the counter for the 13 rows `docs/MIGRACAO-CONTRATO-EN.md` section 9 names:
  4 body/multipart keys (`titulo`->`title` inside each `botoes[]` item — NOT `botao_titulo`,
  already aliased; `indice`->`index` inside each `botoes_template[]` item; `telefones`->`phones`,
  body of `POST/DELETE /v1/bloqueios`, which no longer shares `instanceOnlyAlias` with
  pausa/leituras/fumaca; `arquivo`->`file`, the multipart FIELD NAME of `POST /v1/media` — not a
  `json:"…"` tag at all, so it goes through a separate mechanism, `filePart`, not
  `translateAliasesInPlace`) and 9 `ENTRADA-QUERY` call sites across 6 routes
  (`instancia`->`instance` on `GET /v1/media/{id}` + `POST /v1/media` (shared), `GET /v1/estado`,
  `GET /v1/bloqueios`, `GET /v1/perfil`, `GET /v1/templates`, `DELETE /v1/templates`;
  `mime_do_payload`->`payload_mime` on `GET /v1/media/{id}`; `serie_dias`->`series_days` on
  `GET /v1/estado`; `nome`->`name` on `DELETE /v1/templates` — new `queryAlias`/`queryAliasRaw`
  helpers in `input_aliases.go`, the same "novo or velho" principle as the body but a
  DIFFERENT point in the code, since a query parameter is never a JSON key). `MediaHandler`,
  `StateHandler` and `ProfileHandler` gained a POSITIONAL AND MANDATORY `counter *config.Counter`
  (same discipline T-205 used for bloqueio/cadastro/pausa) — none of the three had one before,
  because none of their ENTRADA points were JSON keys. One `Record` call per REQUEST, combining
  every old name that request carried, never one per key (media's `instanceAuthorized` returns its
  flag instead of recording, so `upload`/`download` can combine it with their own second flag).
  14 new tests, one per key plus the exact control: a request with every key in English except
  `titulo` inside `botoes[]` now moves the counter by exactly +1
  (`TestEntradaConsumerScenarioTitleInPortugueseMovesTheCounter`) — before this task it did not
  move at all. Output is untouched. Two existing test helpers
  (`askStateWithWindow`/`cmd/zapgw/state_test.go`'s state-route test) switched their OWN query
  spelling from `instancia`/`serie_dias` to `instance`/`series_days`, since `GET /v1/estado` now
  self-counts on the old spelling and those helpers back nearly every state-reading test in the
  suite, unrelated to this migration. `CGO_ENABLED=0 go build ./...`, `go test ./...`,
  `go vet ./...`, `gofmt -l cmd internal` clean. _Completed 2026-08-31 13:09._

## v0.62.0 — 2026-08-31

- **Step 2 of the ENTRADA migration, for VALUES this time: the gateway accepts the English value on
  input too** (T-207) — the three ENTRADA value vocabularies of
  `docs/MIGRACAO-CONTRATO-EN.md` section 8 (8.1 `Request.Type`, 11 values; 8.3
  `TemplateButtonUnion.Type` inside `botoes_template[]`, 2 values; 8.5 `Request.Category`, 5
  values — 18 in total) now also accept their English spelling on input, translated to the
  canonical Portuguese value BEFORE `json.Unmarshal` and BEFORE `RequestHash`, same ordering
  requirement and same reason as the key alias: hashing before translation would make
  `{"tipo":"texto"}` and `{"tipo":"text"}` — the same request — hash differently, and the same
  message would go out twice to the customer
  (`TestEntradaValueIdempotencyCrossesLanguages`, green). Output is untouched: the eight SAIDA
  value vocabularies (8.2, 8.6, 8.7, 8.8-8.10, 8.11) were not touched. `tipo` is FOUR
  vocabularies sharing one JSON key — `requestTypeValueAlias`, `templateButtonTypeValueAlias` and
  `requestCategoryValueAlias` are three SEPARATE dicts, each scoped to the one object section 8
  names for it, proved by `TestEntradaValueAliasIsScopedPerObject` (a valid top-level value used
  inside `botoes_template`, and a valid button value used at the top level, both stay `400`). A
  value has no conflict case — no `ErrConflictingAlias` equivalent was added. An invented value
  keeps being refused with today's exact message (`TestEntradaInventedValueStillRejected`).
  `config.CounterOldNameUsed` now also counts an old VALUE, not only an old KEY — a request whose
  key is already English but whose value is still Portuguese counts too, the exact scenario the
  task's own Why names; the counter stays a SINGLE one, and an old-value marker is formatted
  `"valor:<field>=<value>"` (an old-key marker stays the bare field name) so the two are
  distinguishable without a second counter. 11 of the 18 values needed an alias (`template`,
  `cta_url`, `flow`, `url`, `video`, `audio`, `sticker` are the 7 that are the same word in both
  languages already). `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`,
  `gofmt -l cmd internal` clean. _Completed 2026-08-31 11:44._
- **The migration table is INCOMPLETE for step 4 — inventory what a `json:` sweep cannot see**
  (T-206) — new `docs/INVENTARIO-VALORES.md`: 11 closed value-vocabularies (`tipo` alone is FOUR
  separate vocabularies under the same JSON key, 11+6+2+2 values — the `Why`'s six examples covered
  only one of them), the 4 ENTRADA keys missing from the existing tables (`titulo`, `indice`,
  `telefones`, and `arquivo` — a multipart field name, not a JSON tag), a new `ENTRADA-QUERY`
  direction (13 query-param call sites, 9 distinct names, 6 routes — more than the 3 routes
  measured before), and confirmation that all 19 counters in the code (including
  `nome_antigo_usado`) are accounted for. No English name decided — every row is `A DECIDIR`.
  _Completed 2026-08-31 11:12._

## v0.61.1 — 2026-08-31

- **The old-name counter now covers every route that accepts an alias, and a structural guard makes
  the omission fail the build's own test suite** (T-205) — T-203 wired
  `config.CounterOldNameUsed` on 4 of the 7 alias-accepting routes (send, templates, leituras,
  fumaca) and left `POST /v1/cadastro`, `POST /v1/pausa` and `POST/DELETE /v1/bloqueios` accepting
  the English alias WITHOUT counting it. All three now record the counter the same way the other
  four already did. `counter *config.Counter` is a POSITIONAL, MANDATORY constructor parameter on
  `NewRegistrationHandler`, `NewPauseHandler` and `NewBlockHandler` (same discipline as
  `AcceptedTypes`, T-111) — proved not to compile without it (removed the argument from
  `cmd/zapgw/main.go`, `CGO_ENABLED=0 go build ./...` failed naming the missing parameter, restored).
  The heart of the task is the STRUCTURAL GUARD (`TestOldNameCounterGuardCoversEveryAliasRoute`,
  `internal/outbound/input_aliases_test.go`): it does not enumerate the 7 routes by hand — it
  walks the package's AST for every call site of `translateEntradaOrReject` and requires the
  enclosing function to both capture `oldNames` and reference `config.CounterOldNameUsed`, so a
  route born tomorrow that forgets the counter fails this test by name, with zero edits to the
  guard. Proved against REAL production code, not only a synthetic fixture: temporarily reverted
  `pause_handler.go`'s wiring back to discarding `oldNames`, the guard failed citing
  `pauseRoute (chamada em pause_handler.go:101:23)`, then the wiring was restored and the guard went
  green again. `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`,
  `gofmt -l cmd internal` clean. _Completed 2026-08-31 10:54._
- **The pre-push gate must not refuse a TAG that points at an already-pushed commit** (T-204) — an
  empty pushed interval is no longer an automatic `t.Fatalf`: `objectAlreadyReachableFromRemotes`
  checks whether the pushed object (peeled past any tag) is already an ancestor of some
  remote-tracking ref, and a legitimate zero (a release tag on a commit already merged to `main`,
  the ordinary flow) now logs and continues instead of blocking. A genuinely unmeasured zero still
  fails closed exactly as before. Also new: the annotated tag object's own free-text MESSAGE is now
  swept unconditionally (`isAnnotatedTagObject` / `annotatedTagMessage` / `sweepTagMessage`, header
  lines stripped so a real `tagger Name <email>` never false-positives the name gate), because that
  text reaches `origin` on a tag push whether or not the tag carries any new commit. Proved against
  real data both ways in the same session: `v0.61.0` (which existed locally, unpushed, exactly
  because the old gate refused it) pushed clean to a disposable bare repo and then to `origin`; a
  second annotated tag whose message alone carried a needle was BLOCKED citing the tag message, on
  the same disposable remote. T-200/T-201's own tests
  (`TestPrePushGateNewRefCleanBranchPasses`/`...NewRefBlocksNeedleDeletedLater`/
  `...NewRefNoRemoteAtAllSweepsEverything`/`...CleanMergeOnMainPasses`/
  `...BlocksNeedleOnlyInMergeResolution`) stayed green, untouched. `CGO_ENABLED=0 go build ./...`,
  `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` clean; `--no-verify` never used.
  _Completed 2026-08-31 09:41._

## v0.61.0 — 2026-08-31

- **The gateway accepts English key names on ENTRADA input, and counts the old ones** (T-203, step
  2 of 4 of T-189) — the 30 ENTRADA-direction keys of `docs/MIGRACAO-CONTRATO-EN.md` gained an
  English alias, BY POSITION (`internal/outbound/input_aliases.go`), on the 7 routes that decode a
  body: `POST /v1/messages` (descending into the 4 nested objects —
  `cabecalho`/`reacao`/`localizacao`/`fluxo` — and into each `botoes_template` item),
  `POST /v1/templates`, `POST /v1/cadastro`, `POST /v1/pausa`, `POST/DELETE /v1/bloqueios`,
  `POST /v1/leituras`, `POST /v1/fumaca`. Output does NOT change. The translation runs BEFORE
  `json.Unmarshal` and BEFORE `RequestHash`, so the idempotency hash sees the CANONICAL form —
  proved with a test that sends the SAME request in PT and in EN under the SAME `Idempotency-Key`
  and requires ONE send to Meta with the SAME `wa_message_id`
  (`TestEntradaIdempotencyCrossesLanguages`). The two names together in the same request become
  `400`, naming both keys — tested key by key, not by sample, across the 30 ENTRADA rows (including
  the 4 nested objects and the `botoes_template` item). No key from
  `docs/contrato-chaves-que-nao-mudam.txt` gained an alias, and no key outside the table was
  invented. Counter `config.CounterOldNameUsed` (`nome_antigo_usado`) goes up per instance when the
  request still uses the old spelling and shows up on `GET /v1/estado` — it is the number that will
  authorize step 4. ⚠️ **Not on every route:** `/v1/cadastro`, `/v1/pausa` and
  `POST/DELETE /v1/bloqueios` accept the English alias but do NOT yet have `*config.Counter` wired
  in (a bigger structural change, out of this task's scope) — on those the alias works and the
  counter stays out for now. Verify: `CGO_ENABLED=0 go build ./...`, `go test ./...`,
  `go vet ./...`, `gofmt -l cmd internal` clean; the whole suite (`internal/inbound` included, where
  the byte-for-byte `cru` test lives) green with no change there at all.
  _Completed 2026-08-31 09:17._
- **The migration table becomes a versioned document, with DIRECTION and what does NOT change**
  (T-202) — `docs/MIGRACAO-CONTRATO-EN.md` (paired with `docs/MIGRACAO-CONTRATO-EN.pt-BR.md`) were
  born together, assembling 119 key rows from two already-existing sources: 90 pairs already
  proposed to `consumer-b` on the private channel (Source A — measured, not the 89 estimated in the
  spec: no duplicate, count checked with `sed -n` line by line), plus 29 keys measured against the
  code with no pair decided (Source B, `docs/INVENTARIO-CHAVES.md`, English = `A DECIDIR`), with no
  overlap between the two. **Every row has a direction** (`SAIDA-EVENTO`/`SAIDA-RESPOSTA`/`ENTRADA`,
  `A MEDIR` when measured and inconclusive) — 21 keys are multi-direction (14 in Table A, 7 in B),
  and 6 in Table A are `A MEDIR` because the proposed string does not match any real field of
  today's contract (3 only exist in an internal test vector, one is Meta's own pagination envelope,
  one is a CLI flag, one does not exist). The "What does NOT change" section brought in the 23
  already-in-English rows from item 4 of the inventory, with `consumer-b`'s collision rule credited.
  No name was decided by this task. All 119 `file:line` pointers were checked mechanically against
  the code (a Python verification script, not just sampling); a file error found during that check
  (`conector` pointed to `state.go` instead of `ingress.go`) was fixed before the commit. Zero
  identifiable data copied from the private channel (only the key/value table rows). Verify:
  `CGO_ENABLED=0 go build ./...`, `go test ./...` (both personal-data gates swept `docs/` and
  passed), `go vet ./...`, `gofmt -l cmd internal` clean. _Completed 2026-08-31 08:18._
- **The pre-push gate looks inside merge commits too** (T-201) — `filesChangedInCommit`
  (`internal/config/prepush_test.go`) now decides by the commit's number of parents: 0/1 parent
  keeps the usual diff (with `--root` for the genesis commit), 2+ parents (a merge) switches to
  `git diff-tree -c`. Chosen with a measured number in hand, not by assumption: against a real merge
  built in a disposable clone of this repository (never `origin`), a CLEAN merge measured **12 files
  with `-m`** (re-inspects everything both branches already had, redundant with what the per-commit
  sweep already looked at) against **0 with `-c`** (nothing new — everything trivially matches one
  of the parents); a merge with a REAL CONFLICT (the needle only exists in the resolution, in
  neither parent) measured **1 file in both** — `-c` finds everything `-m` finds, at the cost of
  zero redundancy in the clean case. Proof that `-c` hides nothing: a file it omits matches a parent
  whose commit is already in the swept list (or was already public before this push), so the
  content already had a look — `-c` only removes the redundant SECOND look, never the only one. Two
  new tests against a disposable repo: `TestPrePushGateBlocksNeedleOnlyInMergeResolution` (the
  needle exists only in the resolution of a real conflict; the block names the MERGE commit and the
  file, not "could not verify") and `TestPrePushGateCleanMergeOnMainPasses` (a clean merge passes,
  sweep in ~160ms). Entry in `docs/ARMADILHAS.md` (pt-BR pair): the comment that declared the hole
  in `filesChangedInCommit` was updated — it described a limitation already fixed, a false doc.
  Verify: `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l cmd internal`
  clean. _Completed 2026-08-31 07:55._
- **The pre-push gate must not make the legitimate path impossible** (T-200) — the first push of
  ANY new ref (remote sha = zeros) stopped being refused outright and now has its interval computed
  by `git rev-list <new-sha> --not --remotes` (`commitsForPushedInterval`, in
  `internal/config/prepush_test.go`) — exactly "what this push adds to `origin`", without guessing a
  merge-base; with no remote at all the formula reduces on its own to sweeping every reachable
  commit (a safe fallback, no special-case code). Proved against real data, not just asserted: a
  push of a new, clean branch passes; a push of a branch whose commit A introduces a needle and
  whose commit B deletes the file again still blocks, citing commit A and the file (not "could not
  verify") — three new tests (`TestPrePushGateNewRefCleanBranchPasses`,
  `TestPrePushGateNewRefBlocksNeedleDeletedLater`,
  `TestPrePushGateNewRefNoRemoteAtAllSweepsEverything`) plus a manual trial against a disposable bare
  repo. Entry in `docs/ARMADILHAS.md` (pt-BR pair), marked with fire: a fail-closed gate that makes
  the legitimate path impossible does not protect, it teaches the workaround — and the planner's
  first check had "passed" for the wrong reason (a zeroed sha, not a needle found). Verify:
  `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` clean.
  _Completed 2026-08-31 06:41._
- **A pre-push gate: nothing personal leaves this machine, not even in a commit that a later
  commit fixes** (T-199) — `.githooks/pre-push` (activated per clone with `git config
  core.hooksPath .githooks`) runs `internal/config/prepush_test.go`'s `TestPrePushGate` for every
  ref being pushed: it materializes what EACH commit in the interval introduced (not the final
  tree) and reuses, without duplicating, `sweepPhoneNumbersOutsideTheAllowlist` and
  `sweepForbiddenNamesOutsideTheGate` — the same functions the two existing tree gates use. A
  positive interval control was proved: two disposable commits (one adds a synthetic number outside
  the allowlist — not repeated here, see the phone gate itself for why the allowlist should not
  grow by a discarded control value — the other deletes the file, leaving the final tree clean) had
  their push BLOCKED, naming the commit and the file (`CONTROLE-T199-AGULHA.md:1`). A "could not
  verify" control was proved by hiding `~/.zapgw/forbidden-names.txt` (push blocked with its own
  message; file restored, 17 lines confirmed). A legitimate push measures ~1.7s. Fail-closed on
  every path: `go` missing, the needle list missing, an uncomputable interval, and the first push of
  a new ref (remote sha all zeros, no safe base to compute the interval) all block, never let
  through. A limit is documented in the code itself: a MERGE commit shows an empty diff to
  `git diff-tree` without `-m`/`-c`, so content reintroduced only in a merge resolution is not
  inspected by this gate. Verify: `CGO_ENABLED=0 go build ./...`, `go test ./...`, `go vet ./...`,
  `gofmt -l cmd internal` clean. _Completed 2026-08-31 06:29._
- **Inventory every contract key the consumer reads, with its direction and file:line** (T-198) —
  `docs/INVENTARIO-CHAVES.md` created: 47 emission points measured for the 29 requested keys (18
  keys repeat because they appear in more than one direction/route), zero missing from the code,
  zero already in English among the 29. The reverse sweep (item 4) found 20+ OUTPUT keys already in
  English (`media_id`, `wa_id`, `id`, `status`, `ok`, `meta.Profile`/`ProfilePatch`'s fields, etc.),
  including the exact, corrected location of the `Why`'s example — `POST /v1/media`'s response
  emits `media_id` in `internal/outbound/media_handler.go:260` (not in `message.go:179,626`, which
  are ENTRADA/input uses of the same name). It decides no name and edits no contract table. Verify:
  `go test ./...`, `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `gofmt -l cmd internal` clean; a
  sample of 7 `file:line`s checked with `sed -n`. _Completed 2026-08-31 06:02._
- **Settle whether the instance-type gate exists, and make the row say what is true** (T-197) — the
  mechanism EXISTS: `internal/outbound/types.go` (`AcceptedTypes`, T-111), a mandatory positional
  parameter in the last position of every `outbound.New*Handler` constructor. Proved by removing
  `outbound.WhatsAppOnly` from the `NewReadsHandler` call in `cmd/zapgw/main.go:430` and running
  `go build ./...`: `not enough arguments in call to outbound.NewReadsHandler`; restored, `git diff`
  empty. The earlier sweep (T-196) had looked in the wrong place. `CLAUDE.md` and
  `CLAUDE.pt-BR.md` fixed together, with a pointer and the evidence. _Completed 2026-08-31 01:07._
- **The PT-BR pair of CLAUDE.md stops describing a repository that no longer exists** (T-196) —
  the Portuguese pair went back to matching `CLAUDE.md`: the hard-rules table (the two personal-data
  gates, TLS, route isolation), the three-foundations section rewritten, the gates section going
  from two to three, and the "State today" that still said *"just the scaffold, no code, and still
  private"*. **Three false statements were found ALSO on the English side** while checking against
  the code — three gates marked as "exists in zapgw-dev, migrates with the code" after the code had
  already migrated. Two of them were located here and got a pointer; the third was not found and
  became T-197 instead of becoming a statement. _Completed 2026-08-31 01:03._

- **CI carries the name gate, and says so when it cannot verify** (T-195) — `ZAPGW_FORBIDDEN_NAMES`
  delivered as a JOB-level `env:` in `.github/workflows/verify.yml`, reaching both `go test ./...`
  and a new step of its own (`-run TestNoCustomerNameOutsideTheGateInTheRepo`), mirroring the phone
  gate. A comment in the workflow documents that a fork PR fails closed ("could not verify") on
  purpose. `CLAUDE.md` fixed: the CI already exists, it does not "come back on 2026-09-01".
  _Completed 2026-08-31 00:50._
- **A gate for customer names, and the tree it cleans** (T-193) — new gate
  (`internal/config/names_allowlist_test.go`) with no allowlist and no per-file exemption: any
  needle is a failure. The list lives outside the repository (`ZAPGW_FORBIDDEN_NAMES` or
  `~/.zapgw/forbidden-names.txt`); without one of the two, it fails saying it could not verify,
  never green. It failed against the dirty tree (25 occurrences, 6 files) before the cleanup; after
  the cleanup, the whole `go test ./...` is `ok`. `CLAUDE.md` and `docs/ARMADILHAS.md` (+ pt-BR
  pair) updated. _Completed 2026-08-31 00:37._
- **A real customer name in a test argument becomes a synthetic one** (T-192) — swapped the value
  of `cmd/zapgw/provision_test.go:2392`; a sweep for the needle across the whole tree (tracked and
  untracked-and-not-ignored) found no more occurrences. _Completed 2026-08-31 00:15._
- **The personal-data gate sweeps the whole repository, minus a declared exclusion** (T-191) — the
  phone gate swapped its fixed list of directories (`scannedTargets`) for `filesGitSeesFromRoot`:
  `git ls-files` + `git ls-files --others --exclude-standard`, the same set a `git add -A` would
  take. A positive control at the root failed against real data; a negative control (`*.local.md`)
  confirmed the channel file stays outside the sweep. _Completed 2026-08-31 00:21._

## v0.60.1 — 2026-08-30

**This repository's first release, and the first one that exists outside the private repo.** There
is no behavior change relative to `v0.60.0`, which has been in production since 2026-08-29: the
contract with consumers is byte-for-byte the same — 322 `json` tags and 2,081 production string
literals identical, measured, not just asserted. **That is why it's PATCH and not MINOR:** SemVer
speaks about the contract, and the contract did not move, no matter how big the change was on the
inside.

What this version carries:

- **The entire codebase in English.** 3,818 declarations across seven packages; one Portuguese word
  is left, in a test name that cites the name of a multipart part **on the wire** — the test names
  the contract it guards.
- **The personal-data gate, with an EMPTY exemption list.** It sweeps `cmd/`, `internal/`,
  `testdata/`, `docs/` and `README.md`, decoding the base64 of every `wamid.` — because the `wamid`
  carries the recipient's phone number inside it, and a `grep` for the number the way a human writes
  it passes right over them.
- **Diagnostic text that describes behavior instead of naming an internal constant** — a pointer no
  compiler checks.

**Binaries:** `zapgw-linux-amd64` and `zapgw-linux-arm64`, both static (`CGO_ENABLED=0`).
🔴 **The amd64 was RUN** on a real Linux host (`x86_64`): it answered `0.60.1` and `ldd` returned
*"not a dynamic executable"*. **The arm64 only compiled and was NOT run anywhere** — *compiling is
not running*, and that's why it is marked untested rather than announced as supported.
