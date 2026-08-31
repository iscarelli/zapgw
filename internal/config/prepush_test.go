package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// T-199's GATE, and it is a different shape from the phone and name gates
// that already exist in this package (telefones_allowlist_test.go,
// nomes_allowlist_test.go). Those two scan the WORKING TREE — the state of
// the files on disk right now. That leaves a hole this repository's history
// is public and never gets rewritten: a commit A that adds a real phone
// number and a LATER commit B that deletes the file again leaves the FINAL
// tree clean — both tree-scanning gates pass — but the number still reaches
// `origin` the moment the branch is pushed, because commit A goes along
// with it. There is no despublicar.
//
// 🔴 THIS FILE DOES NOT REIMPLEMENT THE SCAN. It calls the exact same
// sweepPhoneNumbersOutsideTheAllowlist / sweepForbiddenNamesOutsideTheGate
// functions the tree-scanning gates use, against a temporary directory that
// holds the CONTENT INTRODUCED BY ONE COMMIT — never a second copy of the
// regexes, the allowlist, or the wamid decoder. If covering the interval
// ever required duplicating that logic, the right move would be to stop and
// say so, not to fork it (docs/TASKS.md, T-199's Do item 4).
//
// This test is driven by `.githooks/pre-push`, which computes the pushed
// interval from git's own pre-push protocol (stdin lines of
// "<local-ref> <local-sha> <remote-ref> <remote-sha>") and invokes:
//
//	go test ./internal/config/ -run TestPrePushGate -v
//
// with ZAPGW_PREPUSH_OLD_SHA/ZAPGW_PREPUSH_NEW_SHA set to the remote/local
// shas of ONE ref update. Outside that context (a plain `go test ./...`,
// which IS part of this project's Verify) there is no interval to check, so
// the test SKIPS rather than fails — this is not a personal-data gate
// being skipped (those two never skip, by design); it is orchestration with
// no input, the same way a test needing a live network address skips
// without one.

// prePushOldShaEnvVar and prePushNewShaEnvVar carry the pushed interval's
// two endpoints from the shell hook into this test. Both come from git's
// own pre-push stdin protocol, never guessed.
const (
	prePushOldShaEnvVar = "ZAPGW_PREPUSH_OLD_SHA"
	prePushNewShaEnvVar = "ZAPGW_PREPUSH_NEW_SHA"
)

// commitsIntroducedInRange lists, oldest first, every commit reachable from
// newSha but not from oldSha — the commits this push actually introduces.
// `git rev-list` itself fails closed here: an invalid or unreachable oldSha
// (including the all-zero sha of a brand-new ref, which `.githooks/pre-push`
// refuses before ever calling this test — see the comment there) makes the
// command exit non-zero, which this function turns into an error, never an
// empty-and-therefore-clean range.
func commitsIntroducedInRange(root, oldSha, newSha string) ([]string, error) {
	cmd := exec.Command("git", "rev-list", "--reverse", oldSha+".."+newSha)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-list %s..%s: %w", oldSha, newSha, err)
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// filesChangedInCommit lists the files a single commit introduces new
// content for — everything except pure deletions (`--diff-filter=d`), with
// rename detection explicitly OFF (`--no-renames`, so behavior does not
// depend on a `diff.renames` config a machine happens to have set): without
// -M, git already represents a rename as a delete of the old path plus an
// add of the new one, and the add alone is exactly the content this gate
// needs to inspect. `--root` makes this work for a commit with no parent
// too (the repository's own genesis commit, if it is ever inside a pushed
// range again).
//
// 📌 KNOWN BOUNDARY, said out loud instead of assumed clean: a MERGE commit
// shows an EMPTY diff here, because `git diff-tree` without `-m`/`-c` only
// diffs single-parent commits. Content that a merge's conflict resolution
// reintroduces (and that neither parent already carried past this gate)
// would not be inspected. T-199's positive control uses two ordinary
// sequential commits, which is the case this function does cover; a merge
// commit carrying new personal data in its resolution is a gap for a future
// task, not something this comment should claim is already handled.
func filesChangedInCommit(root, commit string) ([]string, error) {
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r",
		"--diff-filter=d", "--no-renames", "--root", commit)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff-tree %s: %w", commit, err)
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, filepath.ToSlash(line))
		}
	}
	return files, nil
}

// writeCommitFileToTemp materializes ONE file exactly as it existed at
// `commit` (via `git show <commit>:<path>`, not the working tree) into
// `tempRoot/<path>` — preserving the same relative path the tree-scanning
// gates use, so the sweep functions compute the SAME relative path they
// would from a real checkout and any (currently empty) per-file exemption
// still applies to the right file, not to a mangled temp-dir alias of it.
func writeCommitFileToTemp(root, commit, path, tempRoot string) error {
	if strings.Contains(path, "..") {
		// git does not allow ".." path components in a tree in the first
		// place, but this function is about to os.WriteFile a
		// git-controlled path onto disk — refusing it here costs one line
		// and removes any doubt.
		return fmt.Errorf("caminho suspeito (contem '..'), recusado por seguranca: %q", path)
	}
	dest := filepath.Join(tempRoot, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("MkdirAll %s: %w", filepath.Dir(dest), err)
	}
	cmd := exec.Command("git", "show", commit+":"+path)
	cmd.Dir = root
	content, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git show %s:%s: %w", commit, path, err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return fmt.Errorf("WriteFile %s: %w", dest, err)
	}
	return nil
}

// TestPrePushGate is T-199's gate. For every commit the pushed interval
// introduces, it materializes exactly the files that commit changed (as
// THAT commit left them, not as the final tree left them) into a temporary
// directory and runs both existing sweeps — the same
// sweepPhoneNumbersOutsideTheAllowlist and sweepForbiddenNamesOutsideTheGate
// the tree-scanning gates use — against it. A number or name that a LATER
// commit in the same push deletes still gets caught, because it is caught
// at the commit that introduced it, before any later commit had a chance to
// hide it from a tree scan.
//
// Fails CLOSED at every step: it needs BOTH env vars to even attempt
// anything (see the Skip below), and every git command or sweep error
// becomes t.Fatalf, never a quietly-shorter scan.
func TestPrePushGate(t *testing.T) {
	oldSha := strings.TrimSpace(os.Getenv(prePushOldShaEnvVar))
	newSha := strings.TrimSpace(os.Getenv(prePushNewShaEnvVar))
	if oldSha == "" || newSha == "" {
		t.Skip("TestPrePushGate so' verifica algo quando .githooks/pre-push a invoca, com " +
			prePushOldShaEnvVar + " e " + prePushNewShaEnvVar + " definidas (os dois extremos do " +
			"intervalo sendo empurrado). Fora desse contexto (por exemplo 'go test ./...' comum, " +
			"parte do Verify deste projeto) nao ha intervalo a calcular — isto NAO e' um portao de " +
			"dado pessoal sendo pulado, e' orquestracao sem entrada: os dois portoes que realmente " +
			"varrem dado pessoal (TestNoPhoneNumberOutsideTheAllowlistInTheRepo, " +
			"TestNoCustomerNameOutsideTheGateInTheRepo) continuam rodando e falhando fechado sempre.")
	}

	root, err := moduleRootForTheAllowlist()
	if err != nil {
		t.Fatalf("localizar a raiz do modulo (falha fechada): %v", err)
	}

	commits, err := commitsIntroducedInRange(root, oldSha, newSha)
	if err != nil {
		t.Fatalf("calcular os commits do intervalo %s..%s (falha fechada): %v", oldSha, newSha, err)
	}
	if len(commits) == 0 {
		t.Fatalf("o intervalo %s..%s nao contem nenhum commit novo (falha fechada) — um push com "+
			"algo a empurrar sempre tem ao menos um commit; zero e' sinal de que a MEDICAO esta "+
			"vazia, nunca tratado como \"nada para verificar\"", oldSha, newSha)
	}

	needles, source, err := loadForbiddenNames()
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("agulhas de nome carregadas de: %s (%d agulha(s))", source, len(needles))
	t.Logf("intervalo %s..%s tem %d commit(s) novo(s)", oldSha, newSha, len(commits))

	for _, commit := range commits {
		files, err := filesChangedInCommit(root, commit)
		if err != nil {
			t.Fatalf("listar os arquivos alterados pelo commit %s (falha fechada): %v", commit, err)
		}
		if len(files) == 0 {
			// So' delecoes (ou um commit de merge — ver o limite documentado
			// em filesChangedInCommit): nenhum conteudo novo para inspecionar.
			continue
		}

		tmp := t.TempDir()
		for _, f := range files {
			if err := writeCommitFileToTemp(root, commit, f, tmp); err != nil {
				t.Fatalf("materializar %s do commit %s (falha fechada): %v", f, commit, err)
			}
		}

		phoneHits, _, err := sweepPhoneNumbersOutsideTheAllowlist(tmp, files)
		if err != nil {
			t.Fatalf("varredura de telefone no commit %s (falha fechada): %v", commit, err)
		}
		if len(phoneHits) > 0 {
			t.Fatalf("BLOQUEADO: o commit %s introduz telefone fora da allowlist "+
				"(mesmo que um commit posterior no mesmo push apague o arquivo):\n%s",
				commit, strings.Join(phoneHits, "\n"))
		}

		nameHits, _, err := sweepForbiddenNamesOutsideTheGate(tmp, files, needles)
		if err != nil {
			t.Fatalf("varredura de nome no commit %s (falha fechada): %v", commit, err)
		}
		if len(nameHits) > 0 {
			lines := make([]string, 0, len(nameHits))
			for _, f := range nameHits {
				lines = append(lines, fmt.Sprintf("%s:%d: %s", f.file, f.line, f.match))
			}
			t.Fatalf("BLOQUEADO: o commit %s introduz nome fora do portao "+
				"(mesmo que um commit posterior no mesmo push apague o arquivo):\n%s",
				commit, strings.Join(lines, "\n"))
		}
	}
}
