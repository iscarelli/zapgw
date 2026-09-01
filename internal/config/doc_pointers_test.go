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

// T-217's GATE. T-212 (CAMADA 1) renamed 86 `.go` files from Portuguese to
// English identifiers. Nobody told the docs: a sweep right after found 113
// distinct `.go` pointers in docs/*.md, and 50 of them named a file that no
// longer existed — the SAME failure mode as T-190 (28 dead pointers, one day
// earlier), just triggered by a different rename. A dead doc pointer does not
// fail, does not warn, and is only found by someone who went looking and
// didn't — the worst possible moment. This is the mechanism CLAUDE.md's
// `Código:` header promises ("qual doc a minha mudanca quebrou?" becomes
// mechanical) and that, before this task, nothing enforced.

// docPointerPattern finds a Go-source pointer written in prose: a run of
// path/filename characters ending in literal ".go", optionally followed by
// a line number or a line RANGE (both forms are used throughout docs/, e.g.
// `internal/meta/profile.go:65-71`).
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
//     shaved off a longer run.
var docPointerPattern = regexp.MustCompile(`[A-Za-z0-9_./-]+\.go(:[0-9]+(-[0-9]+)?)?`)

// docsFilesToSweep is every markdown file under docs/ EXCEPT the ones named
// here, each with its reason spelled out — the "por CAMINHO COMPLETO, nunca
// por palavra" rule applies to what the sweep skips, not only to what it
// forgives once inside a file.
//
// docs/TASKS.md is the only exclusion, and it earns it structurally, not by
// convenience: this very task's own spec, while active, contains the
// literal strings "arquivo.go" and "caminho/arquivo.go" as EXAMPLE FORMAT
// TEXT (see the Do section of T-217 in git history) — not pointers to any
// real file. TASKS.md is also not a "subsystem doc" in CLAUDE.md's sense
// (it is the work queue, retired items leave it), so the `Código:` contract
// this gate enforces was never made for it.
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

// deadDocPointerExceptions is the full-path exception list the task's Do
// item 5 asks for. It is empty on purpose, same as
// filesExemptFromThePhoneScan in phones_allowlist_test.go: as of this task
// there is no legitimate ".go" pointer in docs/ that both (a) looks like a
// real path and (b) does not resolve — every real case found (a `.bak`
// filename, this file's own example text) is handled structurally above,
// by construction, not by naming an exception. If one shows up later
// (a genuinely illustrative "path/to/file.go" in prose, for instance), it
// goes here keyed by the EXACT matched pointer text — never by a
// substring or a bare word — with the reason written down.
var deadDocPointerExceptions = map[string]string{
	// (empty)
}

// rejectFalseEndBoundary reports whether the character right after a match
// means the match actually ended too early — i.e. it is a PREFIX of a
// longer token, not the whole thing. "provisionar.go" out of
// "provisionar.go.bak" is exactly this: the next byte is '.', which would
// continue a filename/extension, so the ".go" found is not really where the
// token ends. A letter, digit or underscore right after is the same
// problem in principle (nothing produces it today — verified by sweeping
// docs/ for `\.go[A-Za-z0-9_]` before writing this gate — but the check
// stays because a future doc could).
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

// buildGoBasenameIndex walks root looking for every ".go" file (skipping
// hidden directories — .claude/ holds OTHER implementer agents' worktrees,
// not this commit's code, same reasoning as the phone and TLS gates) and
// returns the set of basenames found. It backs the BARE-pointer format
// (`arquivo.go:linha`, no directory): the task's own Do section names this
// as one of the two formats to catch, and a bare pointer's only checkable
// claim is "a file with this exact name exists somewhere in the tree".
func buildGoBasenameIndex(root string) (map[string]bool, error) {
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
		if strings.HasSuffix(d.Name(), ".go") {
			basenames[d.Name()] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(basenames) == 0 {
		return nil, fmt.Errorf("zero .go files found under %s — closed-failure guard: "+
			"a real checkout always has some, so an empty index means the walk is broken, "+
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
// against goBasenames.
//
// Returns the dead findings, plus howManyPointersSeen — the raw count of
// EVERY pointer examined (dead or alive, exceptions and external-repo
// citations included) so the caller can fail closed if the sweep somehow
// looked at nothing.
func sweepDeadDocPointers(root string, docFiles []string, goBasenames map[string]bool) (dead []docPointerFinding, howManyPointersSeen int, err error) {
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
					exists = goBasenames[pathPart]
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
// TestNoDeadGoPointerInDocs so TestDeadDocPointerGateFailsClosedOnZeroPointers
// can call the EXACT same code the real gate runs — proving the mechanism
// against real inputs, not a parallel reimplementation that could drift
// from what production actually checks. Returns nil when seen > 0.
func zeroPointersError(seen, docCount int) error {
	if seen > 0 {
		return nil
	}
	return fmt.Errorf("swept %d markdown file(s) and found ZERO .go pointers — the pattern "+
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

// TestNoDeadGoPointerInDocs is T-217's gate: every ".go" pointer written in
// docs/*.md (both `path/to/file.go` and the bare `file.go:linha` form) has
// to name a file that actually exists in this tree, or be a declared
// exception / a recognized external-repo citation. A rename that forgets
// the docs now fails HERE instead of being found by whoever goes looking
// next and doesn't.
//
// Fails CLOSED in two independent ways, per the task's own Do item 3:
//   - zero markdown files enumerated, or zero .go files found to check
//     bare pointers against, are both errors (t.Fatalf), never "nothing to
//     check";
//   - zero POINTERS actually examined across every doc is ALSO a failure —
//     113 pointers exist in this repository today (T-217's own
//     measurement); a run that finds none means the pattern stopped
//     matching (a markdown syntax change, a doc rewritten as something
//     other than prose+backticks, …), not that docs/ went clean.
func TestNoDeadGoPointerInDocs(t *testing.T) {
	root, err := moduleRootForTheAllowlist()
	if err != nil {
		t.Fatalf("locate module root (closed failure): %v", err)
	}

	docFiles, err := listMarkdownDocsToSweep(root)
	if err != nil {
		t.Fatalf("enumerate docs/*.md (closed failure): %v", err)
	}

	goBasenames, err := buildGoBasenameIndex(root)
	if err != nil {
		t.Fatalf("index .go basenames (closed failure): %v", err)
	}

	dead, seen, err := sweepDeadDocPointers(root, docFiles, goBasenames)
	if err != nil {
		t.Fatalf("sweep docs/ (closed failure): %v", err)
	}

	// Closed-failure guard against the scan silently seeing nothing (the
	// same failure class the phone gate's file-coverage check guards
	// against): a repository with 22+ doc files and, as of this task,
	// well over a hundred real pointers cannot legitimately produce zero.
	// Pulled into zeroPointersError so the closed-failure control test
	// below (TestDeadDocPointerGateFailsClosedOnZeroPointers) exercises
	// this EXACT branch, not a reimplementation of it.
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
		t.Fatalf("dead .go pointer(s) in docs/ (file no longer exists — check where it "+
			"was renamed to with `git log --follow`, never by guessing from a similar "+
			"name):\n%s", b.String())
	}

	t.Logf("swept %d doc file(s), %d .go pointer(s) examined, 0 dead", len(docFiles), seen)
}

// TestDeadDocPointerGateFailsOnAMutatedPointer is T-217's positive control
// (Verify item 4: "aponte um doc para um arquivo inexistente, confirme que
// o teste reprova nomeando doc, linha e ponteiro"). It runs the SAME
// sweepDeadDocPointers function the real gate uses, against a temporary
// tree, so this control can't drift from what actually runs in production.
func TestDeadDocPointerGateFailsOnAMutatedPointer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("MkdirAll docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "outbound"), 0o755); err != nil {
		t.Fatalf("MkdirAll internal/outbound: %v", err)
	}
	// One real file, so the basename index and the "seen" counter aren't
	// hollow.
	if err := os.WriteFile(filepath.Join(root, "internal", "outbound", "message.go"),
		[]byte("package outbound\n"), 0o644); err != nil {
		t.Fatalf("WriteFile message.go: %v", err)
	}

	const mutatedDoc = "docs/EXEMPLO.md"
	// Line 3 is where the dead pointer lives — checked below.
	content := "# exemplo\n\nponteiro morto: `internal/outbound/mensagem_velha_que_nao_existe.go`\n"
	if err := os.WriteFile(filepath.Join(root, mutatedDoc), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", mutatedDoc, err)
	}

	goBasenames, err := buildGoBasenameIndex(root)
	if err != nil {
		t.Fatalf("index .go basenames: %v", err)
	}

	dead, seen, err := sweepDeadDocPointers(root, []string{mutatedDoc}, goBasenames)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if seen != 1 {
		t.Fatalf("expected exactly 1 pointer examined, got %d", seen)
	}
	if len(dead) != 1 {
		t.Fatalf("expected exactly 1 dead pointer, got %d: %v", len(dead), dead)
	}
	got := dead[0].String()
	want := mutatedDoc + ":3: internal/outbound/mensagem_velha_que_nao_existe.go"
	if got != want {
		t.Fatalf("dead pointer finding mismatch:\n got:  %s\n want: %s", got, want)
	}
	t.Logf("gate correctly failed, naming doc/line/pointer: %s", got)
}

// TestDeadDocPointerGateFailsClosedOnZeroPointers is T-217's Verify item 3:
// a sweep that finds NO pointer at all reports it can't verify, instead of
// reporting "clean". It exercises the same seen==0 branch
// TestNoDeadGoPointerInDocs relies on, against a doc that legitimately has
// no ".go" pointer in it (prose only).
func TestDeadDocPointerGateFailsClosedOnZeroPointers(t *testing.T) {
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
	const emptyDoc = "docs/SEM-PONTEIRO.md"
	if err := os.WriteFile(filepath.Join(root, emptyDoc),
		[]byte("# nada aqui aponta para codigo\n\nso' texto.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", emptyDoc, err)
	}

	goBasenames, err := buildGoBasenameIndex(root)
	if err != nil {
		t.Fatalf("index .go basenames: %v", err)
	}

	dead, seen, err := sweepDeadDocPointers(root, []string{emptyDoc}, goBasenames)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(dead) != 0 {
		t.Fatalf("expected zero dead findings from a doc with no pointer, got: %v", dead)
	}
	if seen != 0 {
		t.Fatalf("expected seen == 0 (this is the control for the closed-failure branch), got %d", seen)
	}

	// This calls the EXACT function TestNoDeadGoPointerInDocs uses to turn
	// seen == 0 into a t.Fatalf — not a reimplementation of the check —
	// so the error text below is what a real run would actually print.
	gateErr := zeroPointersError(seen, 1)
	if gateErr == nil {
		t.Fatalf("zeroPointersError(0, 1) returned nil — the closed-failure gate would have " +
			"passed a doc with no pointers as if it were clean")
	}
	t.Logf("closed-failure gate correctly refuses to call this \"clean\": %v", gateErr)
}
