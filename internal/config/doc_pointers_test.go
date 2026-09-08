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
// (and T-234's Do item, which widens what it has to cover) asks for. Every
// key is the EXACT matched pointer text — never a substring or a bare word.
// Two different reasons put a pointer here, and each entry says which:
//
//  1. NOT A REAL POINTER — the matched text only looks like a repository
//     path. A Go subtest name embeds a fixture's basename after a '/'
//     (`TestCorpusInteiro/reacao.json`); a lesson about ANOTHER project
//     cites that project's own file structure in prose with no repo prefix
//     to mark it as external (ProxmoxVED's `.github/pull_request_template.md`);
//     a rename is documented by quoting the PRE-rename name on purpose
//     (`internal/inbound/testdata/assinatura-entrega.json`, T-234's Do
//     item (d)). None of these name a file this repository's tests could
//     ever find, by construction — widening the extension list did not
//     create these cases, it only made them visible for the first time.
//  2. KNOWN PRE-EXISTING BUG, DEFERRED — the pointer IS wrong (the doc
//     really should say something else) but T-234 is scoped to the gate
//     itself, not to editing docs/ (three other implementers are editing
//     docs/*.md and docs/CHANGELOG.md in this same round; fixing a doc here
//     risks clobbering their in-flight edit). Each such entry is also
//     called out in T-234's report for the planner to fix for real. An
//     entry in this bucket is a debt, not a false positive — removing it
//     without fixing the doc's pointer would silently reopen a real gap.
var deadDocPointerExceptions = map[string]string{
	// --- category 1: not a real pointer ---

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

	"internal/inbound/testdata/assinatura-entrega.json": "intentional historical citation: " +
		"T-228 renamed this fixture to delivery-signature.json, and docs/CHANGELOG.md:30 " +
		"documents that rename by quoting the PRE-rename name in an old-arrow-new pair — " +
		"T-234's Do item (d), the same category as a `.bak` filename kept as history",

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

	"docs/MIGRACAO-CONTRATO-EN.pt-BR.md": "this pt-BR mirror was deliberately DELETED on " +
		"2026-09-07 (CLAUDE.md's own decisions table records the seven mirrors cut, this " +
		"one among them); docs/CHANGELOG.md:454 documents the mirror's creation on " +
		"2026-08-31 and correctly names it as it existed THEN — a historical citation of a " +
		"file retired on purpose, T-234's Do item (d)",
	"CONTROLE-T199-AGULHA.md:1": "throwaway positive-control fixture created and deleted " +
		"within a single test run (T-199, docs/CHANGELOG.md:514) — never meant to persist, " +
		"the same category as a `.bak` filename kept as history",
	"testdata/corpus/categoria_de_template_derivado_da_doc.json": "docs/META-CAMPOS-DE-" +
		"WEBHOOK.md:137 says explicitly, two paragraphs later, that \"the derived fixture " +
		"is gone (T-174, 2026-08-28)\" — deleted on purpose once a real Meta capture " +
		"replaced it, a historical citation of a file retired intentionally",

	// --- category 2: known pre-existing bug, deferred to the planner (see
	// T-234's report) ---

	"testdata/delivery-signature.json": "KNOWN PRE-EXISTING BUG, not introduced by T-234: " +
		"the real file is internal/inbound/testdata/delivery-signature.json. " +
		"docs/CONTRATO-CONSUMIDOR.md, its pt-BR mirror, and docs/MIGRACAO-CONTRATO-EN.md " +
		"cite it without the internal/inbound/ directory (4 occurrences total). Not fixed " +
		"here: docs/CONTRATO-CONSUMIDOR.md is being edited by another implementer in this " +
		"same round (per T-234's dispatch instructions) and editing docs/ is out of this " +
		"task's scope regardless — flagged in T-234's report for the planner",
	"testdata/corpus/categoria_de_template_rebaixamento.json": "KNOWN PRE-EXISTING BUG, " +
		"exactly the T-228-shaped miss this gate was widened to catch: T-228 renamed this " +
		"fixture to template_category_downgrade.json, and docs/META-CAMPOS-DE-WEBHOOK.md:142 " +
		"still cites the pre-rename Portuguese name. Not fixed here (doc edit out of T-234's " +
		"scope) — flagged in T-234's report for the planner",
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
// pointer (contains '/') or a BARE pointer (no '/', only counted when it
// carries a `:line` — an unadorned filename in prose is not a structured
// pointer, it's just naming the file), and checks existence: a path
// pointer is checked with os.Stat against root; a bare pointer is checked
// against repoBasenames.
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
					if !strings.Contains(pointer, ":") {
						// Bare filename, no line number: not a
						// structured pointer per the task's own format
						// definition ("caminho/arquivo.go" e
						// "arquivo.go:linha") — just prose naming a
						// file, too ambiguous to verify on its own
						// (which of possibly several same-named files
						// under different directories is meant?).
						howManyPointersSeen-- // doesn't count as a pointer examined
						continue
					}
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

// TestDocPointersHaveNoDeadTarget is T-217's gate, widened by T-234: every
// pointer written in docs/*.md, for each extension in docPointerExtensions
// (both `path/to/file.ext` and the bare `file.ext:linha` form), has to name
// a file that actually exists in this tree, or be a declared exception / a
// recognized external-repo citation / a structurally-recognized out-of-repo
// absolute path. A rename that forgets the docs now fails HERE instead of
// being found by whoever goes looking next and doesn't.
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
// # What this gate does NOT cover — T-234's Do item, and the reason the
// limit is written HERE rather than only in a task spec or an armadilha:
// the whole point of this widening is that a gate's width is invisible
// from outside it, so the boundary has to travel with the code that draws
// it, not live in a document someone has to remember to open.
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
//   - A bare filename with no `:line` (`zapgw.service`, `CONTRIBUTING.md`)
//     is never checked, structurally, the same as it always was for `.go`
//     — see the comment on the bare-pointer branch in
//     sweepDeadDocPointers. A doc that renames such a file and never
//     updates the bare mention of it will not be caught here.
//   - A cross-repository citation is only recognized in two shapes: the
//     `zapgw-dev:` prefix (externalRepoPrefixes) and a path that is
//     syntactically absolute (isOutsideRepoAbsolutePath). A THIRD shape —
//     a relative-looking path belonging to some OTHER project, cited in
//     prose with neither marker (ProxmoxVED's own
//     `.github/pull_request_template.md`, found while widening this gate)
//     — is invisible to both mechanisms and can only be handled by naming
//     it in deadDocPointerExceptions. Nothing stops a NEW such citation
//     from silently passing as "the file doesn't exist here" would read
//     as "dead", not as "it's someone else's repo" — the false positive
//     it produces (a t.Fatalf naming a real, working doc) is at least
//     loud, unlike a false negative would be.
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
