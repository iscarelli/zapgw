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

// T-235's GATE, and the reason it exists: `deploy/check-leadership.sh` and
// `deploy/deploy.sh` decide what to report by grepping the gateway's OWN
// log output. That makes a `log.Printf` string (or a JSON struct tag) a
// CONTRACT between a Go file and a shell file — and `go test ./...`, this
// project's entire automated safety net, does not read `.sh` files at all.
// Neither does `gofmt`, `go vet`, or CI's other steps.
//
// Two of these seams went stale, SILENTLY, on 2026-09-07 — one from T-219
// (the CLI's English pass), one from T-224 (`internal/config`). Neither
// task's Verify could have seen it, because both verifies are `go test`
// shaped and the broken half is shell. The dangerous one (deploy.sh:260,
// `deploy/deploy.sh`) has a failure mode that IS silence: a `grep` that
// stops matching returns nothing, and "nothing" is exactly what a healthy
// deploy looks like. Full account: docs/ARMADILHAS.md, "The deploy scripts
// GREP the gateway's log".
//
// HOW A COUPLING GETS MARKED, and why a marker instead of parsing every
// `grep`/`case` in the tree: a script has plenty of `grep`/`case` uses that
// have NOTHING to do with the gateway's output (pct/CT status, bashrc
// contents, a VMID's shape — see the sweep in T-235's report). Guessing
// which ones are a Go-coupling from the shell syntax alone is exactly the
// kind of "would need to guess" the allowlist tests in this package already
// reject (see the comment on syntheticPhoneAllowlist). So the choice is
// explicit and lives IN THE SCRIPT: a line
//
//	# zapgw:log-coupling "EXACT LITERAL AS IT APPEARS IN GO"
//
// placed next to (immediately before, or on) the grep/case it documents.
// This test extracts every such literal and requires it to still appear,
// verbatim, somewhere under cmd/ or internal/. Two couplings are
// DELIBERATELY left unmarked, with the reason written at the site instead:
//   - deploy/deploy.sh's `case "$corpo" in *'"ok":true'*)` matches a
//     RENDERED JSON key+value, not a literal that sits in the Go source
//     (the real coupling is to the `OK bool json:"ok"` field staying `true`
//     on success) — matching on the bare word "ok" would produce constant
//     false negatives, since "ok" appears throughout the codebase for
//     unrelated reasons.
//   - deploy/deploy.sh's `case ${ZAPGW_DEPLOY_VMID} in`, the `pct status`
//     grep, and the `/root/.bashrc` grep check shell input, a third-party
//     tool's output, and a file this same script writes — none of them are
//     the gateway's own log output.
//
// 🔴 REPROVOU vs NAO CONSEGUI VERIFICAR: if the extraction finds ZERO
// markers, that is not "nothing to check" — it is indistinguishable from
// the extraction itself having broken (a renamed marker, a moved file), and
// treating it as "clean" is the exact blind monitor docs/ARMADILHAS.md
// warns about ("Um monitor cego que responde OK e pior que monitor
// nenhum"). It FAILS, naming what happened, same as the name gate
// (names_allowlist_test.go) fails when it can't load a needle source.
var shellLogCouplingMarker = regexp.MustCompile(`#\s*zapgw:log-coupling\s+"([^"]*)"`)

// shellLogCoupling is one `# zapgw:log-coupling "..."` marker found in a
// script, with the file:line it was found at (the marker's own line, which
// sits right next to the grep/case it documents) for the failure message.
type shellLogCoupling struct {
	file    string // relative to the module root, e.g. "deploy/deploy.sh"
	line    int    // 1-based
	literal string
}

// extractShellLogCouplings scans every deploy/*.sh file among `targets`
// (expected to come from filesGitSeesFromRoot, same convention T-191 set
// for the phone/name gates — a new script under deploy/ is covered
// without anyone editing this test) for the marker comment and returns
// every one found, in file order.
func extractShellLogCouplings(root string, targets []string) ([]shellLogCoupling, error) {
	var found []shellLogCoupling
	for _, rel := range targets {
		relSlash := filepath.ToSlash(rel)
		if !strings.HasPrefix(relSlash, "deploy/") || !strings.HasSuffix(relSlash, ".sh") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relSlash)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relSlash, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			m := shellLogCouplingMarker.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			found = append(found, shellLogCoupling{file: relSlash, line: i + 1, literal: m[1]})
		}
	}
	return found, nil
}

// goSourceContains reports whether `literal` appears verbatim, as a plain
// substring, in any .go file under cmd/ or internal/ within root. Test
// files count too: a message that only lives in a `_test.go` still proves
// the string is real Go text, and this gate isn't about production-only
// wiring, it's about "did the WORDING drift out from under the shell
// script that greps for it".
//
// Fails CLOSED on any read/walk error — never silently treats an unreadable
// tree as "not found".
func goSourceContains(root, literal string) (bool, error) {
	for _, sub := range []string{"cmd", "internal"} {
		base := filepath.Join(root, sub)
		found := false
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			if d.IsDir() {
				// Same reason as the phone/TLS scans: a dot-directory
				// under the module root is another implementer agent's
				// worktree (.claude/), not this commit's code.
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			if strings.Contains(string(content), literal) {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// TestShellScriptsLogCouplingsExistInGoSource is T-235's gate: every
// `# zapgw:log-coupling "..."` literal declared in deploy/*.sh must still
// appear somewhere under cmd/ or internal/. When it doesn't, the failure
// names the exact pair (`file:line` greps X, X not found) instead of
// leaving the drift to be found by a human reading a deploy transcript.
func TestShellScriptsLogCouplingsExistInGoSource(t *testing.T) {
	root, err := moduleRootForTheAllowlist()
	if err != nil {
		t.Fatalf("locate the module root (closed failure): %v", err)
	}

	targets, err := filesGitSeesFromRoot(root)
	if err != nil {
		t.Fatalf("enumerate the files git sees (closed failure): %v", err)
	}

	couplings, err := extractShellLogCouplings(root, targets)
	if err != nil {
		t.Fatalf("read deploy/*.sh (closed failure): %v", err)
	}

	if len(couplings) == 0 {
		t.Fatalf("COULD NOT VERIFY: found zero \"# zapgw:log-coupling\" markers in deploy/*.sh. " +
			"This is deliberately NOT treated as \"nothing to check\" — it is indistinguishable " +
			"from the extraction itself having broken (a renamed marker, a moved/deleted script), " +
			"and passing on that would be exactly the blind monitor docs/ARMADILHAS.md warns " +
			"about. If deploy/ genuinely lost every coupling, remove this test instead of " +
			"letting it pass silently.")
	}

	var broken []string
	for _, c := range couplings {
		ok, err := goSourceContains(root, c.literal)
		if err != nil {
			t.Fatalf("search cmd/ and internal/ for %q (closed failure): %v", c.literal, err)
		}
		if !ok {
			broken = append(broken, fmt.Sprintf(
				"%s:%d greps %q, which no longer appears anywhere in cmd/ or internal/",
				c.file, c.line, c.literal))
		}
	}

	if len(broken) > 0 {
		sort.Strings(broken)
		t.Fatalf("shell/Go log coupling(s) broken:\n%s\n\n"+
			"Either the Go side reworded the message/tag and the shell script's grep (and its "+
			"\"# zapgw:log-coupling\" marker) need to be updated to match, or the marker is "+
			"stale and should be deleted along with the check it described. See "+
			"docs/ARMADILHAS.md, \"The deploy scripts GREP the gateway's log\".",
			strings.Join(broken, "\n"))
	}

	// Printed on every run, green included — same convention as the phone
	// and name gates: a mechanism nobody can see running is a mechanism
	// nobody trusts.
	t.Logf("checked %d shell/Go log coupling(s):", len(couplings))
	for _, c := range couplings {
		t.Logf("  %s:%d -> %q", c.file, c.line, c.literal)
	}
}

// TestShellLogCouplingGateCatchesAStaleLiteral is T-235's POSITIVE CONTROL.
//
// This repository's hard rule (CLAUDE.md, "a mechanism that has never
// failed anything is indistinguishable from a mechanism that does not
// look") applies here exactly as it does to the phone and name gates: this
// test proves, on every run, that goSourceContains actually returns "not
// found" when the Go side changes wording out from under a marked literal —
// not just "not found (yet)" because nobody tried.
//
// It builds a throwaway tree (a fake deploy/sample.sh and a fake
// cmd/zapgw/x.go) instead of mutating the real repository files, so this
// control itself never needs a manual break/revert to run in CI.
func TestShellLogCouplingGateCatchesAStaleLiteral(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatalf("MkdirAll deploy: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "zapgw"), 0o755); err != nil {
		t.Fatalf("MkdirAll cmd/zapgw: %v", err)
	}
	// goSourceContains walks both cmd/ and internal/ — the real module
	// always has both, so an empty internal/ here keeps this control's tree
	// shaped like the real one instead of tripping over a missing directory.
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatalf("MkdirAll internal: %v", err)
	}

	const literal = "leadership guard ARMED"
	script := "#!/usr/bin/env bash\n" +
		"# zapgw:log-coupling \"" + literal + "\"\n" +
		"grep -q \"" + literal + "\" out.log\n"
	if err := os.WriteFile(filepath.Join(root, "deploy", "sample.sh"), []byte(script), 0o644); err != nil {
		t.Fatalf("WriteFile sample.sh: %v", err)
	}

	goFileWithTheString := "package main\n\nfunc x() { println(\"" + literal + "\") }\n"
	goFilePath := filepath.Join(root, "cmd", "zapgw", "x.go")
	if err := os.WriteFile(goFilePath, []byte(goFileWithTheString), 0o644); err != nil {
		t.Fatalf("WriteFile x.go: %v", err)
	}

	couplings, err := extractShellLogCouplings(root, []string{"deploy/sample.sh"})
	if err != nil {
		t.Fatalf("extractShellLogCouplings: %v", err)
	}
	if len(couplings) != 1 || couplings[0].literal != literal {
		t.Fatalf("expected exactly one coupling with literal %q, got: %+v", literal, couplings)
	}

	t.Run("still_present_passes", func(t *testing.T) {
		ok, err := goSourceContains(root, couplings[0].literal)
		if err != nil {
			t.Fatalf("goSourceContains: %v", err)
		}
		if !ok {
			t.Fatalf("expected the literal to be found while it is still present in the Go source")
		}
	})

	// The Go side reworks the wording — the exact shape of T-219 breaking
	// deploy/check-leadership.sh's ARMED/DISARMED markers on 2026-09-06.
	goFileRenamed := "package main\n\nfunc x() { println(\"renamed guard status\") }\n"
	if err := os.WriteFile(goFilePath, []byte(goFileRenamed), 0o644); err != nil {
		t.Fatalf("WriteFile x.go (renamed): %v", err)
	}

	t.Run("goes_stale_and_the_gate_notices", func(t *testing.T) {
		ok, err := goSourceContains(root, couplings[0].literal)
		if err != nil {
			t.Fatalf("goSourceContains: %v", err)
		}
		if ok {
			t.Fatalf("expected the literal to be reported MISSING after the Go source stopped " +
				"saying it — a gate that still says \"found\" here is the exact silent-failure " +
				"mode T-235 exists to close")
		}
	})
}
