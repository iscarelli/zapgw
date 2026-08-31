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
//
// 🔴 T-200: A FIRST PUSH OF A BRAND-NEW REF IS NOT A SPECIAL CASE THAT
// REFUSES — IT IS A DIFFERENT WAY TO COMPUTE THE SAME INTERVAL. T-199 made
// the hook refuse outright whenever the remote sha was git's all-zero sha
// (no remote ref to diff against), on the theory that there was "no safe
// base to compute a range from". Measured cost: that made EVERY first push
// of EVERY new branch fail, including a clean one — and the only remaining
// path was `git push --no-verify`, i.e. turning the gate off. A failure that
// makes the legitimate path impossible does not protect anything; it teaches
// the bypass.
//
// The fix does not guess a merge-base. `git rev-list <new-sha> --not
// --remotes` is exactly "every commit reachable from this ref that is not
// already reachable from ANY remote-tracking ref this repository knows
// about" — precisely what a first push of this ref would add to `origin`,
// computable with no assumption about which branch it forked from. See
// commitsIntroducedByNewRef below, and note its own comment about the case
// where the repository has no remote-tracking refs AT ALL: `--remotes` then
// matches nothing, `--not --remotes` excludes nothing, and the command
// naturally degenerates into "every commit reachable from new-sha" — the
// slower, safer sweep-everything fallback the task calls for, with no
// special-case code needed to produce it.
const zeroSha = "0000000000000000000000000000000000000000"

// prePushOldShaEnvVar and prePushNewShaEnvVar carry the pushed interval's
// two endpoints from the shell hook into this test. Both come from git's
// own pre-push stdin protocol, never guessed. oldSha may be zeroSha — the
// hook forwards it verbatim rather than pre-filtering it (T-200); see
// commitsForPushedInterval for what that value selects.
const (
	prePushOldShaEnvVar = "ZAPGW_PREPUSH_OLD_SHA"
	prePushNewShaEnvVar = "ZAPGW_PREPUSH_NEW_SHA"
)

// commitsIntroducedInRange lists, oldest first, every commit reachable from
// newSha but not from oldSha — the commits this push actually introduces,
// for the ORDINARY case where oldSha is a real commit already known to this
// repository (the ref being pushed already exists on the remote). `git
// rev-list` itself fails closed here: an invalid or unreachable oldSha makes
// the command exit non-zero, which this function turns into an error, never
// an empty-and-therefore-clean range.
//
// oldSha == zeroSha (a brand-new ref, nothing to diff against on the remote
// side) is NOT routed here — see commitsForPushedInterval, which sends that
// case to commitsIntroducedByNewRef instead.
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

// commitsIntroducedByNewRef lists, oldest first, every commit reachable from
// newSha that is NOT reachable from any remote-tracking ref this repository
// already knows about (`git rev-list --not --remotes`). This is the T-200
// replacement for refusing outright when there is no remote sha to diff
// against: it is exactly "what this push would add to origin", computed
// without guessing which branch the new ref forked from — a commit already
// public on origin/main (or any other already-pushed ref) is correctly
// excluded even though this branch is new, and a commit that exists ONLY on
// this new ref is correctly included.
//
// 🔴 If this repository has NO remote-tracking refs at all (a fresh clone
// that has never pushed anything, or a repo with no remote configured),
// `--remotes` matches zero refs, so `--not --remotes` excludes nothing and
// this returns EVERY commit reachable from newSha — the safe, slower
// fallback the task calls for ("varra todos os commits alcancaveis") instead
// of inventing a base. This is not a special case in the code below; it
// falls out of what `--not --remotes` means when the exclusion set is empty,
// and is covered by TestPrePushGateNewRefNoRemoteAtAllSweepsEverything.
//
// Any actual git failure (a corrupt ref, an unreadable object, newSha not
// resolvable) is returned as an error, and every caller in this file turns
// that into a hard failure — never into "could not compute, so let it
// through".
func commitsIntroducedByNewRef(root, newSha string) ([]string, error) {
	cmd := exec.Command("git", "rev-list", "--reverse", newSha, "--not", "--remotes")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-list %s --not --remotes: %w", newSha, err)
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// commitsForPushedInterval picks which of the two computations above answers
// "what does this push introduce", based on oldSha alone — the same
// distinction git itself uses in the pre-push protocol (zeroSha means "this
// ref does not exist on the remote yet"). This is the ONLY place that
// branches on zeroSha; both TestPrePushGate (driven by the real hook) and
// this file's own tests (driven by a disposable repo) go through it, so the
// two paths cannot drift apart.
func commitsForPushedInterval(root, oldSha, newSha string) ([]string, error) {
	if oldSha == zeroSha {
		return commitsIntroducedByNewRef(root, newSha)
	}
	return commitsIntroducedInRange(root, oldSha, newSha)
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

// sweepCommitsForPersonalData runs both existing sweeps — the same
// sweepPhoneNumbersOutsideTheAllowlist and sweepForbiddenNamesOutsideTheGate
// the tree-scanning gates use — against exactly the content EACH commit in
// `commits` introduces (materialized via writeCommitFileToTemp, never the
// final working tree). It stops at the first commit with a finding and
// returns which commit it was plus a message naming the needle and the
// file, so a caller can tell "the gate found something" apart from "the
// gate could not run" (T-200's Do item 5: a bare bloqueio does not prove the
// gate finds anything — it only proves it refuses).
//
// Shared by TestPrePushGate (the hook's real entry point, against this
// actual repository) and this file's own TestPrePushGateNewRef* tests
// (against a disposable temp repository) — extracted so the two codepaths
// run the identical sweep logic instead of one drifting from the other.
func sweepCommitsForPersonalData(t *testing.T, root string, commits []string, needles []string) (badCommit, message string, err error) {
	t.Helper()
	for _, commit := range commits {
		files, ferr := filesChangedInCommit(root, commit)
		if ferr != nil {
			return "", "", fmt.Errorf("listar os arquivos alterados pelo commit %s: %w", commit, ferr)
		}
		if len(files) == 0 {
			// So' delecoes (ou um commit de merge — ver o limite documentado
			// em filesChangedInCommit): nenhum conteudo novo para inspecionar.
			continue
		}

		tmp := t.TempDir()
		for _, f := range files {
			if werr := writeCommitFileToTemp(root, commit, f, tmp); werr != nil {
				return "", "", fmt.Errorf("materializar %s do commit %s: %w", f, commit, werr)
			}
		}

		phoneHits, _, perr := sweepPhoneNumbersOutsideTheAllowlist(tmp, files)
		if perr != nil {
			return "", "", fmt.Errorf("varredura de telefone no commit %s: %w", commit, perr)
		}
		if len(phoneHits) > 0 {
			return commit, fmt.Sprintf("o commit %s introduz telefone fora da allowlist "+
				"(mesmo que um commit posterior no mesmo push apague o arquivo):\n%s",
				commit, strings.Join(phoneHits, "\n")), nil
		}

		nameHits, _, nerr := sweepForbiddenNamesOutsideTheGate(tmp, files, needles)
		if nerr != nil {
			return "", "", fmt.Errorf("varredura de nome no commit %s: %w", commit, nerr)
		}
		if len(nameHits) > 0 {
			lines := make([]string, 0, len(nameHits))
			for _, f := range nameHits {
				lines = append(lines, fmt.Sprintf("%s:%d: %s", f.file, f.line, f.match))
			}
			return commit, fmt.Sprintf("o commit %s introduz nome fora do portao "+
				"(mesmo que um commit posterior no mesmo push apague o arquivo):\n%s",
				commit, strings.Join(lines, "\n")), nil
		}
	}
	return "", "", nil
}

// TestPrePushGate is T-199's (and now T-200's) gate. For every commit the
// pushed interval introduces — computed by commitsForPushedInterval, which
// picks between an ordinary range and the T-200 "--not --remotes" formula —
// it materializes exactly the files that commit changed and runs both
// sweeps via sweepCommitsForPersonalData. A number or name that a LATER
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
			"intervalo sendo empurrado — oldSha pode ser o sha zero de git, que significa \"ref nova, " +
			"sem base no remoto\"). Fora desse contexto (por exemplo 'go test ./...' comum, parte do " +
			"Verify deste projeto) nao ha intervalo a calcular — isto NAO e' um portao de dado pessoal " +
			"sendo pulado, e' orquestracao sem entrada: os dois portoes que realmente varrem dado " +
			"pessoal (TestNoPhoneNumberOutsideTheAllowlistInTheRepo, " +
			"TestNoCustomerNameOutsideTheGateInTheRepo) continuam rodando e falhando fechado sempre.")
	}

	root, err := moduleRootForTheAllowlist()
	if err != nil {
		t.Fatalf("localizar a raiz do modulo (falha fechada): %v", err)
	}

	commits, err := commitsForPushedInterval(root, oldSha, newSha)
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

	badCommit, message, err := sweepCommitsForPersonalData(t, root, commits, needles)
	if err != nil {
		t.Fatalf("%v (falha fechada)", err)
	}
	if badCommit != "" {
		t.Fatalf("BLOQUEADO: %s", message)
	}
}

// --- T-200's own tests, against a disposable repo (never this repository) ---

// mustGit runs a git command and fails the test loudly (with the command,
// its directory and its combined output) rather than swallowing the error —
// these helpers build the fixture the test reasons about, so a silent
// failure here would make the test's later assertions meaningless.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newDisposableRemoteAndClone builds a throwaway bare "origin" plus a clone
// of it, entirely inside t.TempDir() (cleaned up automatically, never
// touching this actual repository or its real remote). It pushes one base
// commit on `main` so the clone has a real remote-tracking ref
// (origin/main) to test the T-200 "--not --remotes" formula against — the
// same shape a real developer's clone has before creating a feature branch.
func newDisposableRemoteAndClone(t *testing.T) (cloneDir string) {
	t.Helper()
	work := t.TempDir()
	bareDir := filepath.Join(work, "origin.git")
	cloneDir = filepath.Join(work, "clone")

	mustGit(t, work, "init", "--bare", "-q", bareDir)
	mustGit(t, work, "clone", "-q", bareDir, cloneDir)
	mustGit(t, cloneDir, "config", "user.email", "prepush-gate-test@example.invalid")
	mustGit(t, cloneDir, "config", "user.name", "prepush gate test")
	mustGit(t, cloneDir, "checkout", "-q", "-b", "main")

	if err := os.WriteFile(filepath.Join(cloneDir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("escrever base.txt: %v", err)
	}
	mustGit(t, cloneDir, "add", "base.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "base commit")
	mustGit(t, cloneDir, "push", "-q", "origin", "main")
	return cloneDir
}

// TestPrePushGateNewRefCleanBranchPasses is T-200's Do item 3, first case:
// a brand-new branch with nothing but clean commits must be able to push at
// all. Before this task the hook refused every new ref outright regardless
// of content (falha_fechada on remote sha == zero); this proves the
// replacement formula (commitsIntroducedByNewRef, via
// commitsForPushedInterval) both finds the right commits AND lets a clean
// set through, using a needle that is guaranteed absent from the fixture.
func TestPrePushGateNewRefCleanBranchPasses(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)

	mustGit(t, cloneDir, "checkout", "-q", "-b", "feature-clean")
	if err := os.WriteFile(filepath.Join(cloneDir, "feature.txt"), []byte("nothing sensitive here\n"), 0o644); err != nil {
		t.Fatalf("escrever feature.txt: %v", err)
	}
	mustGit(t, cloneDir, "add", "feature.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "clean feature commit")
	newSha := mustGit(t, cloneDir, "rev-parse", "HEAD")

	commits, err := commitsForPushedInterval(cloneDir, zeroSha, newSha)
	if err != nil {
		t.Fatalf("commitsForPushedInterval: %v", err)
	}
	if len(commits) != 1 || commits[0] != newSha {
		t.Fatalf("esperava exatamente [%s] (so' o commit exclusivo da branch nova, sem o commit "+
			"'base' ja publicado em origin/main); obtive %v", newSha, commits)
	}

	needles := []string{"NomeQueNaoAparece1200"}
	badCommit, message, err := sweepCommitsForPersonalData(t, cloneDir, commits, needles)
	if err != nil {
		t.Fatalf("sweepCommitsForPersonalData: %v", err)
	}
	if badCommit != "" {
		t.Fatalf("branch limpa nao deveria bloquear, mas bloqueou no commit %s: %s", badCommit, message)
	}
}

// TestPrePushGateNewRefBlocksNeedleDeletedLater is T-200's Do item 3, second
// case, and it is also the fix for the method error the task's Do item 5
// flags: it does not just assert the gate blocks — it asserts WHICH commit
// and WHICH file the block names, so a green run here proves the gate found
// the needle, not merely that it refused for some other reason (an earlier
// manual check on this same mechanism passed for the wrong reason: sha-zero
// refusal, not a needle match — see docs/ARMADILHAS.md).
func TestPrePushGateNewRefBlocksNeedleDeletedLater(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)
	const needle = "AgulhaDeTesteQueSomeDepois"

	mustGit(t, cloneDir, "checkout", "-q", "-b", "feature-leak")
	if err := os.WriteFile(filepath.Join(cloneDir, "leak.txt"),
		[]byte("this file mentions "+needle+" right here\n"), 0o644); err != nil {
		t.Fatalf("escrever leak.txt: %v", err)
	}
	mustGit(t, cloneDir, "add", "leak.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "commit A: introduces the needle")
	commitA := mustGit(t, cloneDir, "rev-parse", "HEAD")

	mustGit(t, cloneDir, "rm", "-q", "leak.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "commit B: deletes the file again")
	newSha := mustGit(t, cloneDir, "rev-parse", "HEAD")

	commits, err := commitsForPushedInterval(cloneDir, zeroSha, newSha)
	if err != nil {
		t.Fatalf("commitsForPushedInterval: %v", err)
	}
	if len(commits) != 2 || commits[0] != commitA || commits[1] != newSha {
		t.Fatalf("esperava [%s %s] (commit A e depois B, nesta ordem, sem o commit 'base' ja "+
			"publicado); obtive %v", commitA, newSha, commits)
	}

	badCommit, message, err := sweepCommitsForPersonalData(t, cloneDir, commits, []string{needle})
	if err != nil {
		t.Fatalf("sweepCommitsForPersonalData: %v", err)
	}
	if badCommit == "" {
		t.Fatalf("esperava bloqueio (o commit B apaga o arquivo, mas o commit A ainda o introduz " +
			"para o remoto) — a arvore final esta limpa, e e' exatamente o buraco que T-199 fecha; " +
			"se isto passar em branco, o gate voltou a olhar so' a arvore final")
	}
	if badCommit != commitA {
		t.Fatalf("bloqueou no commit errado: esperava commit A (%s), bloqueou em %s — a mensagem "+
			"tem de citar a AGULHA e o commit que a introduziu, nao qualquer bloqueio", commitA, badCommit)
	}
	if !strings.Contains(message, needle) {
		t.Fatalf("a mensagem de bloqueio nao cita a agulha %q — isto e' \"nao consegui verificar\" "+
			"disfarcado de achado; mensagem: %s", needle, message)
	}
	if !strings.Contains(message, "leak.txt") {
		t.Fatalf("a mensagem de bloqueio nao cita o arquivo leak.txt; mensagem: %s", message)
	}
}

// TestPrePushGateNewRefNoRemoteAtAllSweepsEverything is T-200's Do item 2:
// when there is no remote-tracking ref to exclude against (no remote
// configured at all, or none fetched yet), commitsIntroducedByNewRef must
// NOT let the push through unchecked — it must fall back to sweeping every
// commit reachable from newSha. This proves that fallback by building a
// repository with NO remote whatsoever and confirming a needle several
// commits back from HEAD is still found.
func TestPrePushGateNewRefNoRemoteAtAllSweepsEverything(t *testing.T) {
	dir := t.TempDir()
	const needle = "AgulhaSemRemotoNenhum"

	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "prepush-gate-test@example.invalid")
	mustGit(t, dir, "config", "user.name", "prepush gate test")
	mustGit(t, dir, "checkout", "-q", "-b", "main")

	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("older commit, no needle\n"), 0o644); err != nil {
		t.Fatalf("escrever old.txt: %v", err)
	}
	mustGit(t, dir, "add", "old.txt")
	mustGit(t, dir, "commit", "-q", "-m", "older commit")

	if err := os.WriteFile(filepath.Join(dir, "leak.txt"), []byte("has "+needle+" inside\n"), 0o644); err != nil {
		t.Fatalf("escrever leak.txt: %v", err)
	}
	mustGit(t, dir, "add", "leak.txt")
	mustGit(t, dir, "commit", "-q", "-m", "commit with the needle")
	newSha := mustGit(t, dir, "rev-parse", "HEAD")

	if remotes := mustGit(t, dir, "remote"); remotes != "" {
		t.Fatalf("fixture invalida: esperava zero remotos, obtive %q", remotes)
	}

	commits, err := commitsForPushedInterval(dir, zeroSha, newSha)
	if err != nil {
		t.Fatalf("commitsForPushedInterval: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("sem remoto nenhum, esperava varrer os 2 commits alcancaveis (fallback seguro); "+
			"obtive %d: %v", len(commits), commits)
	}

	badCommit, message, err := sweepCommitsForPersonalData(t, dir, commits, []string{needle})
	if err != nil {
		t.Fatalf("sweepCommitsForPersonalData: %v", err)
	}
	if badCommit == "" {
		t.Fatalf("esperava bloqueio: sem remoto, o fallback tem de varrer todos os commits " +
			"alcancaveis, e um deles tem a agulha")
	}
	if !strings.Contains(message, needle) || !strings.Contains(message, "leak.txt") {
		t.Fatalf("mensagem de bloqueio nao cita a agulha e o arquivo esperados: %s", message)
	}
}
