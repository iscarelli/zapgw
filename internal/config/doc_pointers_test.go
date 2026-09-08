package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// T-217's GATE, widened by T-234. T-212 (CAMADA 1) renamed 86 `.go` files
// from Portuguese to English identifiers. Nobody told the docs: a sweep
// right after found 113 distinct `.go` pointers in docs/*.md, and 50 of
// them named a file that no longer existed — the SAME failure mode as
// T-190 (28 dead pointers, one day earlier), just triggered by a different
// rename. A dead doc pointer does not fail, does not warn, and is only
// found by someone who went looking and didn't — the worst possible
// moment. This is the mechanism CLAUDE.md's `Código:` header promises
// ("which doc did my change break?" becomes mechanical) and that, before
// T-217, nothing enforced.
//
// T-217's gate only ever looked at paths ending in ".go". T-228 (2026-09-07)
// renamed 40 test fixtures — none of them ".go" — and `go test ./...` came
// back green on all seven packages while FOUR docs were left pointing at a
// filename that no longer existed, including the `Código:` header of
// docs/CONTRATO-CONSUMIDOR.md itself: the exact mechanism this gate exists
// to guarantee, broken in the direction that fails silently (see
// docs/ARMADILHAS.md, "The doc-pointer gate only sees `.go`"). T-234 widens
// docPointerExtensions past ".go" so the SAME class of rename is caught for
// every extension this repository actually cites in prose.

// docPointerExtensions is every file extension this gate treats as a
// repository-file pointer when it appears in prose. It is deliberately NOT
// "any extension" — the task that widened this gate (T-234) measured what
// docs/*.md actually cites (grepping for `\.[A-Za-z0-9]+$` on every
// path-shaped token in docs/) and started from there, because a wide-open
// class invites exactly the false positives this gate cannot afford: a Go
// struct field like `.Errorf`, a version fragment like `.0`, a MIME type
// like `application/…spreadsheetml.sheet`. Add an extension here only after
// checking, the same way, that docs/ actually cites a repo file with it.
var docPointerExtensions = []string{"go", "json", "sh", "md", "yml", "service", "txt"}

// docPointerPattern finds a repository-file pointer written in prose: a run
// of path/filename characters ending in one of docPointerExtensions,
// optionally followed by a line number or a line RANGE (both forms are used
// throughout docs/, e.g. `internal/meta/profile.go:65-71`).
//
// It deliberately does NOT use \b or lookaround (Go's RE2 engine doesn't
// support lookaround at all, and \b doesn't fit a character class built
// mostly from non-word runes like '/' and '.'). Instead:
//   - the LEFT edge takes care of itself: '/' and '.' are in the class, so
//     a match started anywhere is already extended as far left as the run
//     of path characters goes — Go's regexp finds the leftmost starting
//     position for which a match exists, and any earlier path character
//     would already have been swept in.
//   - the RIGHT edge needs an explicit check in code (see
//     rejectFalseEndBoundary below): the pattern alone would happily accept
//     "provisionar.go" out of "provisionar.go.bak" (a real string in
//     docs/ARMADILHAS.md, describing a temp backup file, not a repo
//     pointer) because nothing here stops ".go" from being a false ending
//     shaved off a longer run. The same mechanism, unchanged by the
//     widening, also rejects "…spreadsheetml.sh" being sheared off
//     "…spreadsheetml.sheet" (a real MIME type string in docs/): the next
//     character is 'e', which fails the boundary check below.
var docPointerPattern = regexp.MustCompile(
	`[A-Za-z0-9_./-]+\.(` + strings.Join(docPointerExtensions, "|") + `)(:[0-9]+(-[0-9]+)?)?`,
)

// docsFilesToSweep is every markdown file under docs/ EXCEPT the ones named
// here, each with its reason spelled out — the "exemption by FULL PATH,
// never by word" rule applies to what the sweep skips, not only to what it
// forgives once inside a file.
//
// docs/TASKS.md is the only exclusion, and it earns it structurally, not by
// convenience: this very task's own spec, while active, contains the
// literal strings "arquivo.go" and "caminho/arquivo.go" as EXAMPLE FORMAT
// TEXT (see the Do section of T-217, and again T-234, in git history) — not
// pointers to any real file. TASKS.md is also not a "subsystem doc" in
// CLAUDE.md's sense (it is the work queue, retired items leave it), so the
// `Código:` contract this gate enforces was never made for it.
var docsFilesExcludedFromTheSweep = map[string]string{
	"docs/TASKS.md": "the work queue: task specs use \"arquivo.go\"/\"caminho/arquivo.go\" " +
		"as literal example format text, not pointers to real files",

	// T-238's Do item 0 — the fix that un-breaks the gate itself. The
	// changelog is a RECORD: docs/CHANGELOG.md cites `implanta/deploy.sh`
	// in four lines because that path was true on the day each entry was
	// written, and T-230 later renamed the directory to `deploy/`.
	// "Correcting" the record would be inventing history it never had,
	// and adding one exception per future rename makes this list grow
	// forever. Excluding the whole file once, with the reason on record,
	// is the rule this project already applies to docs/TASKS.md above —
	// the changelog is equally not a "subsystem doc" in CLAUDE.md's
	// `Código:` sense (it never claims to describe current code, only to
	// record what happened when).
	"docs/CHANGELOG.md": "a historical record, not a subsystem doc: its entries cite the " +
		"path that was true on the day they were written (e.g. `implanta/deploy.sh`, " +
		"renamed to `deploy/` by T-230) and are never rewritten to match a later rename " +
		"— \"fixing\" them would fabricate history that never existed",
}

// externalRepoPrefixes are the ONLY "repo:path" prefixes this gate treats as
// a pointer into ANOTHER repository, hence exempt from an existence check
// against THIS tree. This mirrors the citation style CLAUDE.md itself uses
// (`zapgw-dev:docs/ESTUDO-ABERTURA-PUBLICA-2026-08-20.md`). It is a named
// allowlist, not "anything before a colon" — a wildcard there would let any
// doc dodge the gate by prefixing a made-up word.
var externalRepoPrefixes = []string{
	"zapgw-dev:",
}

// isOutsideRepoAbsolutePath reports whether pathPart names a location that
// cannot possibly be inside this checkout, because it is an ABSOLUTE
// filesystem path on some OTHER host. CLAUDE.md itself authorizes citing
// such paths in a `Código:` header (`o LXC do Traefik:/etc/traefik/…`,
// `host .16:/etc/cron.d/…`) — this is T-234's Do item (a). The same shape
// also covers item (b), a path outside the repository written without a
// "host:" prefix at all (`/root/…`, `/etc/…`, `/var/lib/…`), and — as a
// side effect of the character class not including '~' — a home-relative
// path like `~/.zapgw/forbidden-names.txt` (CLAUDE.md, CLAUDE.md), whose
// match starts right after the stripped '~' at the leading '/'. A full URL
// (`https://healthchecks.io/…`) is covered the same way: ':' is not in the
// class, so the match starts at the "//" right after "https:".
//
// This is checked structurally (a syntactic rule), not by naming every such
// path as an exception, because there is no bound on how many absolute
// paths a doc might cite — CLAUDE.md's whole point in authorizing the
// "host:path" form is that a lot of what the system depends on does not
// live in this repository.
func isOutsideRepoAbsolutePath(pathPart string) bool {
	return strings.HasPrefix(pathPart, "/")
}

// deadDocPointerExceptions is the full-path exception list T-217's Do item 5
// (T-234's Do item, which widens what it has to cover, and T-238's, which
// widens it again to bare filenames with no `:line`) asks for. Every key is
// the EXACT matched pointer text — never a substring or a bare word. Every
// remaining entry is a NOT A REAL POINTER: the matched text only looks like
// a repository path. A Go subtest name embeds a fixture's basename after a
// '/' (`TestCorpusInteiro/reacao.json`); a lesson about ANOTHER project
// cites that project's own file structure in prose with no repo prefix to
// mark it as external (ProxmoxVED's `.github/pull_request_template.md`); a
// rename is documented by quoting the PRE-rename name on purpose
// (`localizacao.json`, T-238's own worked example); a citation of this
// WORKSPACE's own `github` repo is written as a Windows absolute path
// (`C:\dev\github\docs\CREDENCIAIS-DE-API.md`) whose backslashes fall
// outside docPointerPattern's character class, so only the trailing
// basename is ever seen. None of these name a file this repository's tests
// could ever find, by construction — widening the extension list, and later
// the bare-filename check, did not create these cases, it only made them
// visible for the first time.
//
// 🔴 There used to be a second category here, for a pointer T-234 found
// genuinely wrong but deferred to the planner instead of fixing, because
// editing docs/ was out of that task's scope: two entries sat in this list
// marked as debt rather than false positive. T-236 (2026-09-08) fixed both
// real doc pointers (docs/META-CAMPOS-DE-WEBHOOK.md,
// docs/MIGRACAO-CONTRATO-EN.md) and DELETED the two entries — on purpose,
// per this project's own rule: an exception marked as a deferred bug is
// honest today, while the comment is fresh, and is just another allowlist
// line in three months. The gate has to go green because the bug is gone,
// never because it was listed. If a future rename creates a new dead
// pointer, fix the doc; do not reopen this category.
var deadDocPointerExceptions = map[string]string{
	// --- not a real pointer ---

	"TestCorpusInteiro/reacao.json": "Go subtest identifier (TestName/fixture-basename), " +
		"not a filesystem path — docs/ARMADILHAS.md:2035 names the SUBTEST that goes red, " +
		"not a file; the corpus test names each case after its fixture's basename",
	"TestCorpusInteiro/reaction_removed.json": "same as reacao.json above — Go subtest name, " +
		"not a path (docs/ARMADILHAS.md:2046-2047); note this fixture DOES exist at " +
		"testdata/corpus/reaction_removed.json, so an existence check against the literal " +
		"matched text would be wrong either way this comes out",

	".github/pull_request_template.md": "belongs to ProxmoxVED/community-scripts, a " +
		"third-party project studied in a lesson (docs/ARMADILHAS.md:4070) — not a path in " +
		"this repository, and there is no established prefix convention (like `zapgw-dev:`) " +
		"for citing an arbitrary third party's file structure in prose",
	"docs/guides/source-origin.md": "same ProxmoxVED/community-scripts citation as the " +
		"pull_request_template.md entry above (docs/ARMADILHAS.md:4061) — not a path here",

	// T-238's Do item 1: these two are the BARE (no-directory) form of the
	// SAME sentence in docs/ARMADILHAS.md:2563 — "T-228 renamed 40 test
	// fixtures to English (`localizacao.json` → `location.json`,
	// `assinatura-entrega.json` → `delivery-signature.json`, …)" — a doc
	// NARRATING the T-228 rename on purpose, quoting the PRE-rename name
	// as the whole point of the sentence. This is the exact case T-238's
	// own spec names as the one that stays (distinguish narrative from
	// pointer): fixing it to the post-rename name would erase the history
	// the sentence exists to record. The full-path form of the
	// assinatura-entrega.json citation that used to live here (T-234's Do
	// item (d)) cited docs/CHANGELOG.md:30, which T-238's Do item 0 now
	// excludes from the sweep structurally — that entry is gone, not
	// because the citation stopped existing, but because the file it
	// lived in is no longer swept at all.
	"localizacao.json": "docs/ARMADILHAS.md:2563 narrates the T-228 rename by quoting the " +
		"PRE-rename name on purpose, in an old→new pair — the exact shape T-238's own spec " +
		"names as the case that stays",
	"assinatura-entrega.json": "same sentence, same reason as localizacao.json above " +
		"(docs/ARMADILHAS.md:2563) — the bare form of the pre-rename name T-228 replaced " +
		"with delivery-signature.json",

	"docs/superpowers/plans/2026-07-23-fundacao-e-inbound.md:2846": "plan doc kept in the " +
		"private zapgw-dev repository's superpowers/plans/ directory, never migrated here; " +
		"the citation is missing the `zapgw-dev:` prefix that would exempt it structurally " +
		"— flagged for the planner in T-234's report, not fixed here because this task's " +
		"scope is the gate, not doc content",
	"docs/superpowers/plans/2026-08-06-tunel-cloudflare-e-migracao-de-zona.md": "same gap " +
		"as the 2026-07-23 plan doc above (docs/ARMADILHAS.md:3696) — missing `zapgw-dev:` " +
		"prefix, flagged for the planner, not fixed here",

	"implanta/cf-renova-tokens.sh": "script that lived in the private zapgw-dev repository " +
		"before the 2026-08-20 public split; docs/ARMADILHAS.md's infra pitfalls narrate " +
		"pre-split incidents and were carried over as history, but the script itself was " +
		"never part of this public repo's implanta/ (which only has deploy.sh, " +
		"profile-zapgw.sh and valida-lideranca.sh) — missing `zapgw-dev:` prefix, flagged " +
		"for the planner, not fixed here",
	"implanta/sonda-publica.sh": "same as cf-renova-tokens.sh above — private-repo script " +
		"narrated as history in docs/ARMADILHAS.md, never part of this public repo",
	"sonda-worker/deploy.sh": "same as cf-renova-tokens.sh above — a whole directory " +
		"(sonda-worker/) that exists only in the private repository, narrated as history",

	// The two entries that used to sit here (docs/MIGRACAO-CONTRATO-EN.pt-BR.md
	// and CONTROLE-T199-AGULHA.md:1) are GONE, not fixed: both citations
	// only ever occurred in docs/CHANGELOG.md, which T-238's Do item 0 now
	// excludes from the sweep structurally. Same reasoning as the
	// assinatura-entrega.json cleanup above — an exception whose only
	// citation lived in a file no longer swept is dead weight, not a
	// finding to carry forward.
	"testdata/corpus/categoria_de_template_derivado_da_doc.json": "docs/META-CAMPOS-DE-" +
		"WEBHOOK.md:137 says explicitly, two paragraphs later, that \"the derived fixture " +
		"is gone (T-174, 2026-08-28)\" — deleted on purpose once a real Meta capture " +
		"replaced it, a historical citation of a file retired intentionally",

	// --- T-238's Do item 2: widening the gate to bare filenames with no
	// `:line` made every one of the entries below visible for the first
	// time. None of them is a real pointer into this repository; each is
	// checked against real data below, the same way every entry above it
	// was.

	".local.md": "a NAMING CONVENTION, not a file: docs/ARMADILHAS.md:2336, :660 and :675 and " +
		"docs/INVENTARIO-VALORES.md:349 all cite the glob `*.local.md` for the gitignored " +
		"per-consumer channel files this project's own doctrine says must stay uncovered " +
		"(docs/ARMADILHAS.md:2336-2338) — the '*' is outside docPointerPattern's character " +
		"class, so only the suffix after it is ever matched, and no concrete file named " +
		"exactly `.local.md` is ever meant",
	"consumer-b-STATUS.local.md": "docs/ARMADILHAS.md:3622 quotes a real `stat` command run " +
		"against a real channel file, as evidence in a lesson about timestamp provenance — " +
		"but that file is gitignored by the same `*.local.md` convention as the entry above, " +
		"so it was never meant to exist in this checkout",
	"_test.go": "a SUFFIX, not a filename: docs/ARMADILHAS.md:2532 and :4069, docs/" +
		"INVENTARIO-STRINGS.md:13, and docs/INVENTARIO-VALORES.md:307, :340 and :341 all use " +
		"`_test.go` (or a glob like `*_test.go`) to mean \"any Go test file\", never one " +
		"specific file — the '*' in the glob form is outside docPointerPattern's character " +
		"class, so only the suffix survives the match",
	"response.json": "docs/ARMADILHAS.md:504 writes the Python method call `response.json()` " +
		"— rejectFalseEndBoundary does not catch this false ending because '(' is not a " +
		"letter, digit, underscore or dot, so nothing after the match signals it continues; " +
		"the fix is this exception, not widening rejectFalseEndBoundary, because '(' really " +
		"does end a path-shaped token everywhere else in docs/",

	// This workspace's own `github` repository (distinct from zapgw-dev,
	// which already has the `zapgw-dev:` prefix convention) has no such
	// prefix. Two of the three citations below are written as a Windows
	// absolute path (`C:\dev\github\docs\…`), whose backslashes fall
	// outside docPointerPattern's character class — isOutsideRepoAbsolutePath
	// only recognizes a leading '/', so the drive-letter form slips past it
	// and only the trailing basename is ever matched. The third
	// (CANAL-ENTRE-SESSOES.md, in docs/MIGRACAO-CONTRATO-EN.md:55) has no
	// path at all, just prose ("the workspace's CANAL-ENTRE-SESSOES.md
	// protocol") — the same third shape as the ProxmoxVED citations above,
	// just pointing at this workspace's own sibling repo instead of a
	// stranger's.
	"CREDENCIAIS-DE-API.md": "docs/ARMADILHAS.md:2804 cites " +
		"`C:\\dev\\github\\docs\\CREDENCIAIS-DE-API.md` — a file in the `github` workspace " +
		"repo, not this one; the backslashes are outside docPointerPattern's character " +
		"class, so only the basename after the last one is ever matched",
	"DOCUMENTACAO.md": "docs/ARMADILHAS.md:2816 cites `C:\\dev\\github\\docs\\DOCUMENTACAO.md` " +
		"— same workspace-repo gap as CREDENCIAIS-DE-API.md above",
	"CANAL-ENTRE-SESSOES.md": "docs/ARMADILHAS.md:3427 cites " +
		"`C:\\dev\\github\\docs\\CANAL-ENTRE-SESSOES.md` (same gap as the two entries above) " +
		"and docs/MIGRACAO-CONTRATO-EN.md:55 cites the same file with no path at all, just " +
		"prose (\"the workspace's `CANAL-ENTRE-SESSOES.md` protocol\") — the same missing-" +
		"prefix shape the ProxmoxVED entries above already cover, for this workspace's own " +
		"sibling repo instead of a stranger's",

	"cloudflared-zapgw.service": "docs/ARMADILHAS.md:3764 names a systemd unit that runs on " +
		"the Traefik LXC, a remote host — never part of this repository — cited in prose " +
		"with no `host:path` prefix (CLAUDE.md authorizes the form, this sentence does not " +
		"use it)",

	"AGENTS.md": "docs/ARMADILHAS.md:4079 studies ProxmoxVED/community-scripts (the same " +
		"third-party project as the .github/pull_request_template.md and " +
		"docs/guides/source-origin.md entries above) and names its AGENTS.md alongside them " +
		"— not a path in this repository",
	"CONTRIBUTING.md": "docs/ARMADILHAS.md:4079 and :4093 name ProxmoxVED/community-scripts's " +
		"CONTRIBUTING.md, same citation as AGENTS.md above — not a path in this repository",
}

// rejectFalseEndBoundary reports whether the character right after a match
// means the match actually ended too early — i.e. it is a PREFIX of a
// longer token, not the whole thing. "provisionar.go" out of
// "provisionar.go.bak" is exactly this: the next byte is '.', which would
// continue a filename/extension, so the ".go" found is not really where the
// token ends. A letter, digit or underscore right after is the same
// problem in principle — and, after T-234's widening, a REAL case: a MIME
// type string in docs/ (`application/vnd.openxmlformats-officedocument.
// spreadsheetml.sheet`) contains "…spreadsheetml.sh" as a prefix of
// "…spreadsheetml.sheet"; the next character is 'e', so this check rejects
// it. This function is extension-agnostic on purpose — it only looks at the
// byte after the match, so it needed no change when the extension list
// grew.
func rejectFalseEndBoundary(line string, matchEnd int) bool {
	if matchEnd >= len(line) {
		return false
	}
	c := line[matchEnd]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.'
}

// stripLineSuffix removes a trailing `:123` or `:123-456` from a matched
// pointer, returning just the path/filename part that has to exist on
// disk.
func stripLineSuffix(pointer string) string {
	if i := strings.IndexByte(pointer, ':'); i >= 0 {
		return pointer[:i]
	}
	return pointer
}

// hasRecognizedDocPointerExtension reports whether name ends in one of
// docPointerExtensions, used by buildRepoBasenameIndex to decide which
// files to index — kept as a single small helper so the walk and the regex
// can never drift apart on what counts as "a pointer-worthy extension".
func hasRecognizedDocPointerExtension(name string) bool {
	for _, ext := range docPointerExtensions {
		if strings.HasSuffix(name, "."+ext) {
			return true
		}
	}
	return false
}

// buildRepoBasenameIndex walks root looking for every file whose extension
// is in docPointerExtensions (skipping hidden directories — .claude/ holds
// OTHER implementer agents' worktrees, not this commit's code, same
// reasoning as the phone and TLS gates) and returns the set of basenames
// found. It backs the BARE-pointer format (`arquivo.go:linha`, no
// directory): the task's own Do section names this as one of the two
// formats to catch, and a bare pointer's only checkable claim is "a file
// with this exact name exists somewhere in the tree".
//
// Renamed from buildGoBasenameIndex by T-234: it now indexes every
// recognized extension, not only ".go".
func buildRepoBasenameIndex(root string) (map[string]bool, error) {
	basenames := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if d.IsDir() {
			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if hasRecognizedDocPointerExtension(d.Name()) {
			basenames[d.Name()] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(basenames) == 0 {
		return nil, fmt.Errorf("zero recognized-extension files found under %s — closed-failure "+
			"guard: a real checkout always has some, so an empty index means the walk is broken, "+
			"not that the tree is empty", root)
	}
	return basenames, nil
}

// docPointerFinding is one dead pointer, already formatted the way the test
// failure names it: which doc, which line, which pointer.
type docPointerFinding struct {
	doc     string
	lineNo  int
	pointer string
}

func (f docPointerFinding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.doc, f.lineNo, f.pointer)
}

// sweepDeadDocPointers scans every file in docFiles (paths relative to
// root) for a docPointerPattern match, classifies each match as a PATH
// pointer (contains '/') or a BARE pointer (no '/'), and checks existence:
// a path pointer is checked with os.Stat against root; a bare pointer —
// WITH or WITHOUT a `:line` suffix, since T-238 — is checked against
// repoBasenames. T-234 only checked a bare pointer when it carried a line
// number, on the theory that an unadorned filename in prose was too
// ambiguous to verify; T-238 measured that theory against real data
// (seven dead pointers named a renamed fixture with no directory and no
// line number) and it was wrong, so the restriction is gone.
//
// Returns the dead findings, plus howManyPointersSeen — the raw count of
// EVERY pointer examined (dead or alive, exceptions and external-repo
// citations included) so the caller can fail closed if the sweep somehow
// looked at nothing.
func sweepDeadDocPointers(root string, docFiles []string, repoBasenames map[string]bool) (dead []docPointerFinding, howManyPointersSeen int, err error) {
	for _, relDoc := range docFiles {
		fullPath := filepath.Join(root, relDoc)
		content, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			return nil, 0, fmt.Errorf("read %s: %w", fullPath, readErr)
		}
		for i, line := range strings.Split(string(content), "\n") {
			lineNo := i + 1
			for _, idx := range docPointerPattern.FindAllStringIndex(line, -1) {
				start, end := idx[0], idx[1]
				if rejectFalseEndBoundary(line, end) {
					continue
				}
				pointer := line[start:end]
				howManyPointersSeen++

				// External-repo citation ("zapgw-dev:internal/x.go"): the
				// prefix lives in the line just before the match, since
				// ':' is not in docPointerPattern's character class and so
				// never became part of the match itself.
				isExternal := false
				for _, prefix := range externalRepoPrefixes {
					if start >= len(prefix) && line[start-len(prefix):start] == prefix {
						isExternal = true
						break
					}
				}
				if isExternal {
					continue
				}

				if reason, exempt := deadDocPointerExceptions[pointer]; exempt {
					_ = reason // logged by the caller (t.Logf), not here
					continue
				}

				pathPart := stripLineSuffix(pointer)

				// T-234's Do items (a) and (b): a "host:path" citation
				// CLAUDE.md authorizes, or any other path outside this
				// repository, is never checkable against this tree. Both
				// shapes reduce to the same syntactic test once the ':'
				// prefix (never part of the match) is out of the picture:
				// the remaining path part starts with '/'.
				if isOutsideRepoAbsolutePath(pathPart) {
					continue
				}

				var exists bool
				if strings.Contains(pathPart, "/") {
					_, statErr := os.Stat(filepath.Join(root, pathPart))
					exists = statErr == nil
				} else {
					// Bare filename (no directory component), with or
					// without a `:line` suffix: T-234 only checked this
					// shape when it carried a line number, on the theory
					// that an unadorned filename in prose was "just
					// naming the file, too ambiguous to verify". T-238
					// measured that theory against real data and it was
					// wrong — the loose form is exactly where a rename
					// hides, because nobody expects the gate to be
					// looking there. Checking a bare name against the
					// basename index does NOT need a line number: the
					// question "does a file with this exact basename
					// exist anywhere in the tree" is answerable either
					// way, and the ambiguity concern (which of possibly
					// several same-named files is meant) was never about
					// EXISTENCE, only about which directory to point at
					// — a question this gate never answered for the
					// `:line` form either (see "Existence is the only
					// thing checked" below).
					exists = repoBasenames[pathPart]
				}
				if !exists {
					dead = append(dead, docPointerFinding{doc: relDoc, lineNo: lineNo, pointer: pointer})
				}
			}
		}
	}
	return dead, howManyPointersSeen, nil
}

// zeroPointersError is the CLOSED-FAILURE check itself, pulled out of
// TestDocPointersHaveNoDeadTarget so
// TestDocPointerGateFailsClosedOnZeroPointers can call the EXACT same code
// the real gate runs — proving the mechanism against real inputs, not a
// parallel reimplementation that could drift from what production actually
// checks. Returns nil when seen > 0.
func zeroPointersError(seen, docCount int) error {
	if seen > 0 {
		return nil
	}
	return fmt.Errorf("swept %d markdown file(s) and found ZERO pointers — the pattern "+
		"stopped matching (or every doc changed shape), not that docs/ has no "+
		"pointers left. Treat this as a failure to verify, never as clean.", docCount)
}

// listMarkdownDocsToSweep enumerates docs/*.md relative to root, minus
// docsFilesExcludedFromTheSweep. Built from a directory read (not a
// hand-maintained list) so a new doc is swept the moment it exists — same
// reasoning T-191 used to replace phones_allowlist_test.go's fixed target
// list with filesGitSeesFromRoot.
func listMarkdownDocsToSweep(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		return nil, fmt.Errorf("read docs/: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := "docs/" + e.Name()
		if _, excluded := docsFilesExcludedFromTheSweep[rel]; excluded {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("zero markdown files found under docs/ — closed-failure guard")
	}
	return out, nil
}

// TestDocPointersHaveNoDeadTarget is T-217's gate, widened by T-234 and
// again by T-238: every pointer written in docs/*.md, for each extension in
// docPointerExtensions (both `path/to/file.ext` and the bare `file.ext` or
// `file.ext:linha` form), has to name a file that actually exists in this
// tree, or be a declared exception / a recognized external-repo citation /
// a structurally-recognized out-of-repo absolute path. A rename that
// forgets the docs now fails HERE instead of being found by whoever goes
// looking next and doesn't.
//
// Fails CLOSED in two independent ways, per the task's own Do item 3:
//   - zero markdown files enumerated, or zero recognized-extension files
//     found to check bare pointers against, are both errors (t.Fatalf),
//     never "nothing to check";
//   - zero POINTERS actually examined across every doc is ALSO a failure —
//     113 ".go" pointers alone existed in this repository as of T-217's own
//     measurement, before the extension list even grew; a run that finds
//     none means the pattern stopped matching (a markdown syntax change, a
//     doc rewritten as something other than prose+backticks, …), not that
//     docs/ went clean.
//
// # What this gate does NOT cover — T-234's Do item, carried forward and
// updated by T-238's, and the reason the limit is written HERE rather than
// only in a task spec or an armadilha: the whole point of this widening is
// that a gate's width is invisible from outside it, so the boundary has to
// travel with the code that draws it, not live in a document someone has
// to remember to open.
//
//   - Only the extensions in docPointerExtensions are checked. A repo file
//     cited with an extension NOT in that list (".py", ".env.example",
//     ".yaml", a bare ".githooks/pre-push" with no dot-suffix at all) is
//     as invisible to this gate today as every non-".go" extension was
//     before T-234 — this is the SAME hole, one extension narrower.
//     docs/ARMADILHAS.md's own accompanying case (`diag_instagram_meta.py`,
//     donated by a consumer, never part of this repo) was checked by hand
//     while widening this gate and deliberately left OFF the list — adding
//     ".py" today would flag it as dead for being outside the repo, not
//     for being renamed, which is a worse failure mode than not checking
//     it at all.
//   - ✅ RETIRED by T-238: a bare filename with no `:line` used to be
//     skipped entirely, structurally, the same as it always was for `.go`
//     — and that was the hole: seven dead pointers named a T-228-renamed
//     fixture with no directory and no line number, measured on
//     2026-09-08 by grepping docs/*.md for a backtick-quoted `name.json`
//     and checking each against disk. A bare filename with a recognized extension is
//     now checked against the basename index regardless of a `:line`
//     suffix (see the bare-pointer branch in sweepDeadDocPointers) — the
//     ambiguity T-234 worried about (which of possibly several
//     same-named files is meant) was never about EXISTENCE, only about
//     which directory to point at, a question this gate never answered
//     for the `:line` form either (see "Existence is the only thing
//     checked" below).
//   - ⚠️ NEW, surfaced by the widening above: a bare filename is checked
//     for existence ANYWHERE in the tree by basename alone, with no
//     notion of directory — so two files that share a basename in
//     different directories are indistinguishable to this check (the
//     same limit the `:line` form already had, now reachable far more
//     often since no line number is required to trigger it). It also
//     surfaced a class of false positive the `:line` requirement used to
//     filter out for free: a Python method call written as
//     `response.json()` reads as a pointer to `response.json` once the
//     trailing `()` is dropped by rejectFalseEndBoundary's own rules (see
//     the `response.json` entry in deadDocPointerExceptions) — and a
//     glob pattern like `*.local.md` or `*_test.go`, whose leading `*`
//     falls outside docPointerPattern's character class, reads as a
//     pointer to the literal basename `.local.md` or `_test.go` (see
//     those entries in the same map). None of these are extension-list
//     or boundary bugs to fix; each is a real string in docs/ that only
//     LOOKS like a pointer once bare names are in scope.
//   - A cross-repository citation is only recognized in two shapes: the
//     `zapgw-dev:` prefix (externalRepoPrefixes) and a path that is
//     syntactically absolute with a leading '/' (isOutsideRepoAbsolutePath).
//     Two more shapes are invisible to both and can only be handled by
//     naming them in deadDocPointerExceptions: a relative-looking path
//     belonging to some OTHER project, cited in prose with neither marker
//     (ProxmoxVED's own `.github/pull_request_template.md`, `AGENTS.md`
//     and `CONTRIBUTING.md`, found while widening this gate); and —
//     surfaced only now that bare names are checked — a Windows absolute
//     path to THIS WORKSPACE's own `github` sibling repo
//     (`C:\dev\github\docs\CREDENCIAIS-DE-API.md`), whose backslashes
//     fall outside the character class just as thoroughly as a leading
//     '/' would be caught by isOutsideRepoAbsolutePath if it were a
//     forward slash — so only the trailing basename is ever matched, and
//     it is indistinguishable from a same-named file that really is
//     missing from this repo. Nothing stops a NEW such citation from
//     silently passing as "the file doesn't exist here" would read as
//     "dead", not as "it's someone else's repo" — the false positive it
//     produces (a t.Fatalf naming a real, working doc) is at least loud,
//     unlike a false negative would be.
//   - Existence is the only thing checked, never correctness of a line
//     number or line range: `file.go:9999` against a 20-line file passes
//     as long as `file.go` exists.
func TestDocPointersHaveNoDeadTarget(t *testing.T) {
	root, err := moduleRootForTheAllowlist()
	if err != nil {
		t.Fatalf("locate module root (closed failure): %v", err)
	}

	docFiles, err := listMarkdownDocsToSweep(root)
	if err != nil {
		t.Fatalf("enumerate docs/*.md (closed failure): %v", err)
	}

	repoBasenames, err := buildRepoBasenameIndex(root)
	if err != nil {
		t.Fatalf("index basenames (closed failure): %v", err)
	}

	dead, seen, err := sweepDeadDocPointers(root, docFiles, repoBasenames)
	if err != nil {
		t.Fatalf("sweep docs/ (closed failure): %v", err)
	}

	// Closed-failure guard against the scan silently seeing nothing (the
	// same failure class the phone gate's file-coverage check guards
	// against): a repository with 22+ doc files and, as of T-217's own
	// measurement, well over a hundred real ".go" pointers alone cannot
	// legitimately produce zero. Pulled into zeroPointersError so the
	// closed-failure control test below
	// (TestDocPointerGateFailsClosedOnZeroPointers) exercises this EXACT
	// branch, not a reimplementation of it.
	if err := zeroPointersError(seen, len(docFiles)); err != nil {
		t.Fatalf("%v", err)
	}

	if len(dead) > 0 {
		sort.Slice(dead, func(i, j int) bool { return dead[i].String() < dead[j].String() })
		var b strings.Builder
		for _, f := range dead {
			b.WriteString(f.String())
			b.WriteString("\n")
		}
		t.Fatalf("dead pointer(s) in docs/ (file no longer exists — check where it "+
			"was renamed to with `git log --follow`, never by guessing from a similar "+
			"name):\n%s", b.String())
	}

	t.Logf("swept %d doc file(s), %d pointer(s) examined (extensions: %s), 0 dead",
		len(docFiles), seen, strings.Join(docPointerExtensions, ", "))
}

// TestDocPointerGateFailsOnAMutatedPointer is T-217's positive control
// (Verify item 4: "point a doc at a nonexistent file, confirm the test
// fails naming the doc, line and pointer"), extended by T-234 to prove the
// same mechanism against real data for a WIDENED extension (".json", not
// ".go") — the exact class of file T-228 renamed without the old gate
// noticing. It runs the SAME sweepDeadDocPointers function the real gate
// uses, against a temporary tree, so this control can't drift from what
// actually runs in production.
func TestDocPointerGateFailsOnAMutatedPointer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("MkdirAll docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "outbound"), 0o755); err != nil {
		t.Fatalf("MkdirAll internal/outbound: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "testdata", "corpus"), 0o755); err != nil {
		t.Fatalf("MkdirAll testdata/corpus: %v", err)
	}
	// Two real files, so the basename index and the "seen" counter aren't
	// hollow: one ".go" (the pre-T-234 case) and one ".json" (the T-228
	// case this task exists to catch).
	if err := os.WriteFile(filepath.Join(root, "internal", "outbound", "message.go"),
		[]byte("package outbound\n"), 0o644); err != nil {
		t.Fatalf("WriteFile message.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "testdata", "corpus", "location.json"),
		[]byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile location.json: %v", err)
	}

	const mutatedDoc = "docs/EXAMPLE.md"
	// Line 3 and line 4 are where the two dead pointers live — checked
	// below. This is T-234's real-data proof: renaming a fixture (as
	// T-228 did) and leaving the doc pointed at the OLD name is exactly
	// what this gate now has to catch for a non-".go" extension.
	content := "# example\n\n" +
		"dead .go pointer: `internal/outbound/old_message_that_does_not_exist.go`\n" +
		"dead .json pointer, T-228-shaped: `testdata/corpus/localizacao_de_negocios.json`\n"
	if err := os.WriteFile(filepath.Join(root, mutatedDoc), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", mutatedDoc, err)
	}

	repoBasenames, err := buildRepoBasenameIndex(root)
	if err != nil {
		t.Fatalf("index basenames: %v", err)
	}

	dead, seen, err := sweepDeadDocPointers(root, []string{mutatedDoc}, repoBasenames)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if seen != 2 {
		t.Fatalf("expected exactly 2 pointers examined, got %d", seen)
	}
	if len(dead) != 2 {
		t.Fatalf("expected exactly 2 dead pointers, got %d: %v", len(dead), dead)
	}
	sort.Slice(dead, func(i, j int) bool { return dead[i].String() < dead[j].String() })
	wantGo := mutatedDoc + ":3: internal/outbound/old_message_that_does_not_exist.go"
	wantJSON := mutatedDoc + ":4: testdata/corpus/localizacao_de_negocios.json"
	if got := dead[0].String(); got != wantGo {
		t.Fatalf("dead .go pointer finding mismatch:\n got:  %s\n want: %s", got, wantGo)
	}
	if got := dead[1].String(); got != wantJSON {
		t.Fatalf("dead .json pointer finding mismatch:\n got:  %s\n want: %s", got, wantJSON)
	}
	t.Logf("gate correctly failed on both extensions, naming doc/line/pointer:\n%s\n%s",
		dead[0].String(), dead[1].String())
}

// TestDocPointerGateFailsClosedOnZeroPointers is T-217's Verify item 3: a
// sweep that finds NO pointer at all reports it can't verify, instead of
// reporting "clean". It exercises the same seen==0 branch
// TestDocPointersHaveNoDeadTarget relies on, against a doc that
// legitimately has no recognized pointer in it (prose only).
func TestDocPointerGateFailsClosedOnZeroPointers(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("MkdirAll docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatalf("MkdirAll internal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "placeholder.go"),
		[]byte("package internal\n"), 0o644); err != nil {
		t.Fatalf("WriteFile placeholder.go: %v", err)
	}
	const emptyDoc = "docs/NO-POINTER.md"
	if err := os.WriteFile(filepath.Join(root, emptyDoc),
		[]byte("# nothing here points to code\n\njust text.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", emptyDoc, err)
	}

	repoBasenames, err := buildRepoBasenameIndex(root)
	if err != nil {
		t.Fatalf("index basenames: %v", err)
	}

	dead, seen, err := sweepDeadDocPointers(root, []string{emptyDoc}, repoBasenames)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(dead) != 0 {
		t.Fatalf("expected zero dead findings from a doc with no pointer, got: %v", dead)
	}
	if seen != 0 {
		t.Fatalf("expected seen == 0 (this is the control for the closed-failure branch), got %d", seen)
	}

	// This calls the EXACT function TestDocPointersHaveNoDeadTarget uses to
	// turn seen == 0 into a t.Fatalf — not a reimplementation of the check
	// — so the error text below is what a real run would actually print.
	gateErr := zeroPointersError(seen, 1)
	if gateErr == nil {
		t.Fatalf("zeroPointersError(0, 1) returned nil — the closed-failure gate would have " +
			"passed a doc with no pointers as if it were clean")
	}
	t.Logf("closed-failure gate correctly refuses to call this \"clean\": %v", gateErr)
}
