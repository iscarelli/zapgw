package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T-199's GATE, and it is a different shape from the phone and name gates
// that already exist in this package (phones_allowlist_test.go,
// names_allowlist_test.go). Those two scan the WORKING TREE — the state of
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
//
// 🔴 T-204: A ZERO-COMMIT INTERVAL IS NOT ALWAYS "COULD NOT MEASURE" — AND
// THIS PROJECT LAUNCHES BY TAG. Pushing an annotated tag that points at a
// commit already on `origin` (the ordinary case: tag the release commit
// AFTER it merged) reports a zero remote sha for the tag ref (it is a brand
// new ref) and, once peeled, `commitsIntroducedByNewRef` legitimately
// returns an EMPTY list — the push adds a POINTER, not a commit. Before this
// task TestPrePushGate treated ANY empty commit list as "the measurement is
// empty, fail closed", which is correct for "I could not compute the
// interval" and wrong for "I computed it, and it is genuinely zero". Same
// defect as T-200, different disguise: a fail-closed gate whose only escape
// hatch for a legitimate push is `--no-verify` trains the bypass instead of
// protecting anything. See docs/ARMADILHAS.md.
//
// The signal that tells the two apart is reachability, not emptiness:
// objectAlreadyReachableFromRemotes asks whether the pushed object (peeled
// past any tag) is already an ancestor of some remote-tracking ref. For the
// commitsIntroducedByNewRef formula this is mathematically the SAME
// condition that produced the empty list in the first place (`git rev-list
// X --not --remotes` is empty if and only if X is reachable from
// `--remotes`) — so this is not a second, looser check bolted on next to the
// first; it is asking the exact same question the empty list already
// answered, in a form the test can act on instead of just fail on. A zero
// list this function calls "not reachable" is still a hard failure — that
// combination should not arise from either formula above, and if it ever
// does, refusing is the only defensible move.
//
// 🔴 An empty interval is NOT the whole story for a tag push. The tag
// object's own MESSAGE — free text a human wrote — travels to `origin` too,
// whether or not the tag introduces any new commit. isAnnotatedTagObject /
// annotatedTagMessage exist so TestPrePushGate sweeps that text with the
// same two functions used everywhere else in this file, never a
// re-implementation. This is the part of a tag push this gate actually has
// work to do on, and it is the part nobody thinks about.
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

// objectAlreadyReachableFromRemotes is T-204's answer to "is a zero-commit
// interval a legitimate zero, or a sign the measurement did not run?" — sha
// (peeled past any tag object automatically by `git rev-list`) is already
// reachable from some remote-tracking ref this repository knows about if and
// only if `git rev-list sha --not --remotes` finds nothing: every ancestor
// of sha, including sha itself, is excluded because it is already reachable
// from `--remotes`. This is deliberately the SAME formula
// commitsIntroducedByNewRef already runs (here with `-n 1`, since only
// emptiness matters, not the full list) — not a second, looser check: the
// zero-commit case this function is asked about is usually the direct
// output of that exact command, so this re-asks the identical question in a
// form the caller can branch on.
//
// Any git failure (sha does not resolve, corrupt object) is returned as an
// error and never silently turned into "not reachable" — a caller that
// cannot tell "confirmed reachable" apart from "could not check" must fail
// closed, the same rule every other function in this file follows.
func objectAlreadyReachableFromRemotes(root, sha string) (bool, error) {
	cmd := exec.Command("git", "rev-list", "-n", "1", sha, "--not", "--remotes")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git rev-list -n 1 %s --not --remotes: %w", sha, err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// isAnnotatedTagObject reports whether sha names a git TAG object — the kind
// `git tag -a` creates, which carries its own free-text message — as opposed
// to a lightweight tag (whose ref points straight at a commit, with no
// separate object or message) or an ordinary branch/commit push. `git
// cat-file -t` prints the object's type ("tag", "commit", "tree", "blob");
// only "tag" means there is a message for annotatedTagMessage to read.
func isAnnotatedTagObject(root, sha string) (bool, error) {
	cmd := exec.Command("git", "cat-file", "-t", sha)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git cat-file -t %s: %w", sha, err)
	}
	return strings.TrimSpace(string(out)) == "tag", nil
}

// annotatedTagMessage returns the free-text message of the annotated tag
// object at sha — the part a human actually typed, with the object/type/
// tag/tagger header lines stripped off. `git cat-file -p` prints the header
// block, a single blank line, then the message; splitting on the FIRST blank
// line separates the two without parsing the header fields at all. If the
// object has no blank line (malformed, or an empty message with no trailing
// newline separator), the full output is swept instead of nothing — this
// function never turns "could not find the boundary" into "there is no
// message to check".
func annotatedTagMessage(root, sha string) (string, error) {
	cmd := exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git cat-file -p %s: %w", sha, err)
	}
	if idx := strings.Index(string(out), "\n\n"); idx != -1 {
		return string(out)[idx+2:], nil
	}
	return string(out), nil
}

// commitParentCount reports how many parents `commit` has (0 for the
// repository's own genesis commit, 1 for an ordinary commit, 2+ for a
// merge) — the branch filesChangedInCommit needs to pick between the
// single-parent diff and the merge-aware combined diff below.
func commitParentCount(root, commit string) (int, error) {
	cmd := exec.Command("git", "rev-list", "--parents", "-n", "1", commit)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("git rev-list --parents -n 1 %s: %w", commit, err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return 0, fmt.Errorf("git rev-list --parents -n 1 %s: saida vazia", commit)
	}
	return len(fields) - 1, nil // fields[0] is the commit itself
}

// filesChangedInCommit lists the files a single commit introduces new
// content for — everything except pure deletions (`--diff-filter=d`), with
// rename detection explicitly OFF (`--no-renames`, so behavior does not
// depend on a `diff.renames` config a machine happens to have set): without
// -M, git already represents a rename as a delete of the old path plus an
// add of the new one, and the add alone is exactly the content this gate
// needs to inspect.
//
// For an ORDINARY commit (0 or 1 parent) this runs the plain diff-tree
// against that parent, with `--root` covering the repository's own genesis
// commit.
//
// 🔴 T-201: FOR A MERGE COMMIT (2+ parents) THIS BRANCHES TO `-c`
// (combined diff), NOT `-m`, and the choice was MEASURED, not assumed —
// against a real merge built in a disposable clone of this repository
// (never `origin`; the fixture and commands are recorded in this task's
// report, not reproduced as code here to avoid a third copy of the scan
// logic drifting from the other two).
//
//   - A CLEAN merge (two branches touching disjoint files, git auto-merges,
//     no conflict) measured `git diff-tree -m` at **12 files** — the full
//     union of both branches' changes, because `-m` diffs the merge
//     separately against EACH parent and concatenates. Every one of those
//     12 files already belongs to a commit that `commitsForPushedInterval`
//     put in the scanned list on its own (that is how those files got INTO
//     the merge in the first place) — `-m` re-inspects content this gate
//     already looked at, and the redundant work grows without bound as a
//     branch's own history grows. The SAME merge measured `git diff-tree -c`
//     at **0 files** — correctly nothing, because every file's final
//     content trivially matches one parent or the other; there is no
//     resolution content to inspect.
//   - A merge that resolves a REAL conflict (both branches edit the same
//     line of the same file, requiring a human/tool to write new text) —
//     with the resolution text present in NEITHER parent's version —
//     measured **1 file** for BOTH `-m` and `-c`: the one conflicted file,
//     found either way. This is exactly the content commit-level scanning
//     cannot see (it exists only in the merge commit's own tree), and `-c`
//     catches it at zero extra cost over the clean case.
//
// **The gate must sweep at least as much as before, never less** — so the
// question item 3 of T-201 demands an answer to is whether `-c`'s
// "TREESAME to one parent, so omit it" rule can hide content this gate
// still needed to see. It cannot: a file `-c` omits because it trivially
// equals parent P was not introduced by the resolution — it was introduced
// by whatever commit made parent P's tree look that way, and that commit is
// either already in `commits` (scanned on its own, per the point above) or
// it was already public on some remote-tracking ref before this push
// existed (already past this gate when IT was pushed). Either way the
// content already went through a sweep; `-c` only removes the REDUNDANT
// second look `-m` would perform, never the only look.
func filesChangedInCommit(root, commit string) ([]string, error) {
	parents, err := commitParentCount(root, commit)
	if err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if parents >= 2 {
		cmd = exec.Command("git", "diff-tree", "-c", "--no-commit-id", "--name-only", "-r",
			"--diff-filter=d", "--no-renames", commit)
	} else {
		cmd = exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r",
			"--diff-filter=d", "--no-renames", "--root", commit)
	}
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
			// So' delecoes, OU um merge cujo `-c` nao achou nenhum arquivo que
			// difira de TODOS os pais (merge limpo, sem resolucao de
			// conflito — ver filesChangedInCommit): nenhum conteudo novo
			// para inspecionar.
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

// sweepTagMessage is T-204's tag-message check, extracted into its own
// function (instead of living inline inside TestPrePushGate) for the same
// reason sweepCommitsForPersonalData is: so this file's own tests can drive
// it directly against a disposable clone, instead of only exercising it
// through TestPrePushGate — which is pinned to THIS repository via
// moduleRootForTheAllowlist and cannot run against a throwaway fixture.
//
// sha names an ordinary commit, a lightweight tag, or an annotated tag
// object. For anything other than an annotated tag there is no separate
// message to sweep — matched comes back false with no error, the same
// "nothing here" shape sweepCommitsForPersonalData uses for a commit with no
// changed files. For an annotated tag, its message is materialized into a
// temp file and swept with the exact same two functions used everywhere
// else in this file — never a re-implementation of the phone or name check.
func sweepTagMessage(t *testing.T, root, sha string, needles []string) (matched bool, message string, err error) {
	t.Helper()

	isTag, terr := isAnnotatedTagObject(root, sha)
	if terr != nil {
		return false, "", fmt.Errorf("determinar se %s e' uma tag anotada: %w", sha, terr)
	}
	if !isTag {
		return false, "", nil
	}

	tagMessage, merr := annotatedTagMessage(root, sha)
	if merr != nil {
		return false, "", fmt.Errorf("ler a mensagem da tag %s: %w", sha, merr)
	}

	const tagMessageRelativePath = "tag-message.txt"
	tmp := t.TempDir()
	if werr := os.WriteFile(filepath.Join(tmp, tagMessageRelativePath), []byte(tagMessage), 0o644); werr != nil {
		return false, "", fmt.Errorf("escrever a mensagem da tag %s em arquivo temporario: %w", sha, werr)
	}

	phoneHits, _, perr := sweepPhoneNumbersOutsideTheAllowlist(tmp, []string{tagMessageRelativePath})
	if perr != nil {
		return false, "", fmt.Errorf("varredura de telefone na mensagem da tag %s: %w", sha, perr)
	}
	if len(phoneHits) > 0 {
		return true, fmt.Sprintf("a MENSAGEM da tag %s introduz telefone fora da allowlist "+
			"(mesmo quando a tag nao acrescenta nenhum commit novo):\n%s",
			sha, strings.Join(phoneHits, "\n")), nil
	}

	nameHits, _, nerr := sweepForbiddenNamesOutsideTheGate(tmp, []string{tagMessageRelativePath}, needles)
	if nerr != nil {
		return false, "", fmt.Errorf("varredura de nome na mensagem da tag %s: %w", sha, nerr)
	}
	if len(nameHits) > 0 {
		lines := make([]string, 0, len(nameHits))
		for _, f := range nameHits {
			lines = append(lines, fmt.Sprintf("mensagem da tag, linha %d: %s", f.line, f.match))
		}
		return true, fmt.Sprintf("a MENSAGEM da tag %s introduz nome fora do portao "+
			"(mesmo quando a tag nao acrescenta nenhum commit novo):\n%s",
			sha, strings.Join(lines, "\n")), nil
	}

	return false, "", nil
}

// TestPrePushGate is T-199's (and now T-200's and T-204's) gate. For every
// commit the pushed interval introduces — computed by
// commitsForPushedInterval, which picks between an ordinary range and the
// T-200 "--not --remotes" formula — it materializes exactly the files that
// commit changed and runs both sweeps via sweepCommitsForPersonalData. A
// number or name that a LATER commit in the same push deletes still gets
// caught, because it is caught at the commit that introduced it, before any
// later commit had a chance to hide it from a tree scan.
//
// T-204: an EMPTY commit list is no longer an automatic failure. It is
// checked against objectAlreadyReachableFromRemotes first — if the pushed
// object (peeled past any tag) is already an ancestor of some
// remote-tracking ref, zero is the correct answer (a tag pointing at a
// commit already on `origin` adds a pointer, not a commit), and the test
// logs that and moves on instead of refusing a push that adds nothing new
// to `origin`. If it is NOT reachable, the original fail-closed refusal
// still applies — that combination means the measurement itself could not
// be trusted, not that there is nothing to check.
//
// T-204 also sweeps the pushed object's own tag message when it is an
// annotated tag, REGARDLESS of whether the commit list is empty — a tag
// that introduces real commits can still carry a message with a needle in
// it, and that text reaches `origin` exactly as much as any file does.
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
		// T-204: zero commits is not automatically "could not measure" —
		// it is the correct, legitimate answer for a tag pushed after its
		// commit already reached `origin` (the push adds a pointer, not a
		// commit). The signal that tells the two apart: is the pushed
		// object (peeled past any tag) already reachable from some
		// remote-tracking ref this repository knows about?
		reachable, rerr := objectAlreadyReachableFromRemotes(root, newSha)
		if rerr != nil {
			t.Fatalf("o intervalo %s..%s nao contem nenhum commit novo, e nao consegui confirmar se "+
				"%s ja esta alcancavel a partir de alguma ref de rastreamento remoto (falha fechada): %v",
				oldSha, newSha, newSha, rerr)
		}
		if !reachable {
			t.Fatalf("o intervalo %s..%s nao contem nenhum commit novo, e %s NAO esta alcancavel a "+
				"partir de nenhuma ref de rastreamento remoto (falha fechada) — um push com algo a "+
				"empurrar sempre tem ao menos um commit novo OU aponta para algo ja publicado; nenhum "+
				"dos dois e' o caso aqui, entao a MEDICAO nao pode ser confiada, nunca tratada como "+
				"\"nada para verificar\"", oldSha, newSha, newSha)
		}
		t.Logf("intervalo %s..%s nao acrescenta nenhum commit novo ao remoto, e isto e' legitimo: %s "+
			"ja esta alcancavel a partir de uma ref de rastreamento remoto (ex.: uma tag apontando "+
			"para um commit ja publicado). Continuando so' com a varredura da propria ref abaixo.",
			oldSha, newSha, newSha)
	} else {
		t.Logf("intervalo %s..%s tem %d commit(s) novo(s)", oldSha, newSha, len(commits))
	}

	needles, source, err := loadForbiddenNames()
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("agulhas de nome carregadas de: %s (%d agulha(s))", source, len(needles))

	// T-204: the tag object's own MESSAGE travels to `origin` on a tag
	// push, whether or not the tag introduces any new commit — swept here,
	// unconditionally, with the exact same two functions used everywhere
	// else in this file.
	tagBlocked, tagMessageResult, err := sweepTagMessage(t, root, newSha, needles)
	if err != nil {
		t.Fatalf("%v (falha fechada)", err)
	}
	if tagBlocked {
		t.Fatalf("BLOQUEADO: %s", tagMessageResult)
	}

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

// --- T-201's own tests: the pre-push gate's blindness to merge commits ---

// TestPrePushGateCleanMergeOnMainPasses is T-201's negative control: two
// branches that touch DISJOINT files, merged with no conflict, must push —
// and fast. This is also what the report's measurement (12 files via `-m`,
// 0 via `-c`, see filesChangedInCommit) is proving in test form: `-c`
// finding zero files for a clean merge is not "the gate got quieter", it is
// "there was nothing here that a per-commit scan had not already seen".
func TestPrePushGateCleanMergeOnMainPasses(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)

	mustGit(t, cloneDir, "checkout", "-q", "-b", "clean-a")
	if err := os.WriteFile(filepath.Join(cloneDir, "clean_a.txt"), []byte("branch A file, no needle\n"), 0o644); err != nil {
		t.Fatalf("escrever clean_a.txt: %v", err)
	}
	mustGit(t, cloneDir, "add", "clean_a.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "branch A: adds its own file")

	mustGit(t, cloneDir, "checkout", "-q", "main")
	mustGit(t, cloneDir, "checkout", "-q", "-b", "clean-b")
	if err := os.WriteFile(filepath.Join(cloneDir, "clean_b.txt"), []byte("branch B file, no needle\n"), 0o644); err != nil {
		t.Fatalf("escrever clean_b.txt: %v", err)
	}
	mustGit(t, cloneDir, "add", "clean_b.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "branch B: adds a different file")

	mustGit(t, cloneDir, "checkout", "-q", "clean-a")
	mustGit(t, cloneDir, "merge", "-q", "--no-edit", "clean-b")
	mergeSha := mustGit(t, cloneDir, "rev-parse", "HEAD")

	commits, err := commitsForPushedInterval(cloneDir, zeroSha, mergeSha)
	if err != nil {
		t.Fatalf("commitsForPushedInterval: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("esperava 3 commits novos (A, B e o merge; 'base' ja esta em origin/main), obtive %d: %v",
			len(commits), commits)
	}

	start := time.Now()
	badCommit, message, err := sweepCommitsForPersonalData(t, cloneDir, commits, []string{"NomeQueNaoAparece1201"})
	elapsed := time.Since(start)
	t.Logf("merge limpo (3 commits, sem conflito): sweep levou %s", elapsed)
	if err != nil {
		t.Fatalf("sweepCommitsForPersonalData: %v", err)
	}
	if badCommit != "" {
		t.Fatalf("merge limpo nao deveria bloquear, mas bloqueou no commit %s: %s", badCommit, message)
	}
}

// TestPrePushGateBlocksNeedleOnlyInMergeResolution is T-201's positive
// control, and the one the task's Verify step demands in exactly this
// shape: a needle that exists in NEITHER parent of a merge, only in the
// text a human/tool wrote while resolving a REAL conflict (both branches
// edit the same line of the same file). Before this task filesChangedInCommit
// returned an EMPTY diff for any merge commit (no `-m`/`-c`), so this needle
// would have crossed the gate unseen even though it is INSIDE the pushed
// range. A green run here proves the opposite: the block names the merge
// commit itself and the file, not a commit that merely "looks blocked".
func TestPrePushGateBlocksNeedleOnlyInMergeResolution(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)
	const needle = "AgulhaDeMergeQueSoExisteNaResolucao1201"

	// A shared file both branches will edit on the SAME line, guaranteeing
	// a real conflict instead of git auto-merging disjoint hunks.
	if err := os.WriteFile(filepath.Join(cloneDir, "shared.txt"), []byte("linha original\n"), 0o644); err != nil {
		t.Fatalf("escrever shared.txt: %v", err)
	}
	mustGit(t, cloneDir, "add", "shared.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "adds shared.txt")
	mustGit(t, cloneDir, "push", "-q", "origin", "main")

	mustGit(t, cloneDir, "checkout", "-q", "-b", "conflict-a")
	if err := os.WriteFile(filepath.Join(cloneDir, "shared.txt"), []byte("linha da branch A\n"), 0o644); err != nil {
		t.Fatalf("escrever shared.txt (A): %v", err)
	}
	mustGit(t, cloneDir, "add", "shared.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "conflict-a: edita shared.txt")
	commitA := mustGit(t, cloneDir, "rev-parse", "HEAD")

	mustGit(t, cloneDir, "checkout", "-q", "main")
	mustGit(t, cloneDir, "checkout", "-q", "-b", "conflict-b")
	if err := os.WriteFile(filepath.Join(cloneDir, "shared.txt"), []byte("linha da branch B\n"), 0o644); err != nil {
		t.Fatalf("escrever shared.txt (B): %v", err)
	}
	mustGit(t, cloneDir, "add", "shared.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "conflict-b: edita shared.txt (diferente)")
	commitB := mustGit(t, cloneDir, "rev-parse", "HEAD")

	mustGit(t, cloneDir, "checkout", "-q", "conflict-a")
	mergeCmd := exec.Command("git", "merge", "--no-edit", "conflict-b")
	mergeCmd.Dir = cloneDir
	_ = mergeCmd.Run() // esperado sair com erro: conflito de verdade em shared.txt

	if !strings.Contains(mustGit(t, cloneDir, "status", "--short"), "UU shared.txt") {
		t.Fatalf("fixture invalida: esperava conflito UU em shared.txt, git status: %s",
			mustGit(t, cloneDir, "status", "--short"))
	}

	// Resolve o conflito escrevendo um texto que NAO existe em nenhum dos
	// dois pais — a agulha so' passa a existir na resolucao do merge.
	resolved := "resolvido no merge, contem a agulha: " + needle + "\n"
	if err := os.WriteFile(filepath.Join(cloneDir, "shared.txt"), []byte(resolved), 0o644); err != nil {
		t.Fatalf("escrever a resolucao: %v", err)
	}
	mustGit(t, cloneDir, "add", "shared.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "resolve o conflito (introduz a agulha so' aqui)")
	mergeSha := mustGit(t, cloneDir, "rev-parse", "HEAD")

	// Confirma a premissa do teste: a agulha nao esta em nenhum dos pais.
	forA := mustGitAllowingContent(t, cloneDir, "show", commitA+":shared.txt")
	forB := mustGitAllowingContent(t, cloneDir, "show", commitB+":shared.txt")
	if strings.Contains(forA, needle) || strings.Contains(forB, needle) {
		t.Fatalf("fixture invalida: a agulha ja aparece num dos pais (A=%q B=%q)", forA, forB)
	}

	commits, err := commitsForPushedInterval(cloneDir, zeroSha, mergeSha)
	if err != nil {
		t.Fatalf("commitsForPushedInterval: %v", err)
	}
	// commitA, commitB, e o commit de merge — nao o "adds shared.txt", que
	// ja foi empurrado para origin/main acima.
	if len(commits) != 3 {
		t.Fatalf("esperava 3 commits novos (A, B, merge), obtive %d: %v", len(commits), commits)
	}

	badCommit, message, err := sweepCommitsForPersonalData(t, cloneDir, commits, []string{needle})
	if err != nil {
		t.Fatalf("sweepCommitsForPersonalData: %v", err)
	}
	if badCommit == "" {
		t.Fatalf("esperava bloqueio: a agulha so' existe na resolucao do merge, e antes do T-201 " +
			"filesChangedInCommit devolvia diff VAZIO para commit de merge — exatamente o buraco que " +
			"esta tarefa fecha; se isto passar em branco, o gate voltou a nao olhar dentro de merges")
	}
	if badCommit != mergeSha {
		t.Fatalf("bloqueou no commit errado: esperava o commit de MERGE (%s) — a agulha nao esta em "+
			"nenhum dos pais (%s, %s) —, bloqueou em %s", mergeSha, commitA, commitB, badCommit)
	}
	if !strings.Contains(message, needle) {
		t.Fatalf("a mensagem de bloqueio nao cita a agulha %q: %s", needle, message)
	}
	if !strings.Contains(message, "shared.txt") {
		t.Fatalf("a mensagem de bloqueio nao cita o arquivo shared.txt: %s", message)
	}
	if !strings.Contains(message, mergeSha) {
		t.Fatalf("a mensagem de bloqueio nao cita o commit de merge %s: %s", mergeSha, message)
	}
}

// mustGitAllowingContent is mustGit's twin for reading blob content: same
// loud-failure contract, but it does not TrimSpace the output — a fixture
// file's exact bytes (including its trailing newline) matter when the
// assertion is "does this needle appear in this parent's version", not
// "what did this git subcommand print".
func mustGitAllowingContent(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// --- T-204's own tests: a tag pointing at an already-published commit, and its message ---

// TestPrePushGateAnnotatedTagOnPublishedCommitPushesClean is T-204's central
// case, and the one that was measured refusing a real push before this task:
// an annotated tag created AFTER its commit already reached `origin` (the
// ordinary release flow — merge to main, then `git tag -a` on that same
// commit) reports a zero remote sha (the tag ref itself is brand new) and,
// once commitsForPushedInterval peels the tag, a genuinely EMPTY commit
// list — the push adds a pointer, not a commit. Before this task any empty
// list was an automatic t.Fatalf; this proves the replacement signal
// (objectAlreadyReachableFromRemotes) correctly calls this case legitimate,
// and that a CLEAN tag message does not block either.
func TestPrePushGateAnnotatedTagOnPublishedCommitPushesClean(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)
	baseSha := mustGit(t, cloneDir, "rev-parse", "HEAD") // the "base commit", already on origin/main

	mustGit(t, cloneDir, "tag", "-a", "v0.204.0", "-m", "clean release note, no needle here")
	tagSha := mustGit(t, cloneDir, "rev-parse", "v0.204.0")

	commits, err := commitsForPushedInterval(cloneDir, zeroSha, tagSha)
	if err != nil {
		t.Fatalf("commitsForPushedInterval: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("uma tag no commit base (ja publicado em origin/main) nao deveria acrescentar "+
			"nenhum commit novo; obtive %v", commits)
	}

	reachable, rerr := objectAlreadyReachableFromRemotes(cloneDir, tagSha)
	if rerr != nil {
		t.Fatalf("objectAlreadyReachableFromRemotes: %v", rerr)
	}
	if !reachable {
		t.Fatalf("esperava que %s (tag sobre o commit base %s, ja em origin/main) fosse alcancavel "+
			"a partir de uma ref de rastreamento remoto — esta e' exatamente a distincao que T-204 "+
			"introduz entre \"zero porque nada foi medido\" e \"zero porque e' legitimo\"", tagSha, baseSha)
	}

	needles := []string{"NomeQueNaoAparece1204Limpo"}
	matched, message, serr := sweepTagMessage(t, cloneDir, tagSha, needles)
	if serr != nil {
		t.Fatalf("sweepTagMessage: %v", serr)
	}
	if matched {
		t.Fatalf("mensagem de tag limpa nao deveria bloquear, mas bloqueou: %s", message)
	}
}

// TestPrePushGateBlocksNeedleInTagMessageEvenWithZeroCommits is T-204's Do
// item 3, and the Verify step's positive control that must not regress: a
// needle living ONLY in the tag's own MESSAGE — never in any file, never in
// any commit — has to block the push, even though (especially though) the
// commit interval is legitimately empty. Before this task the zero-commit
// case never reached the point of even looking at the tag object; a needle
// here would have crossed the gate unseen the moment T-204's OTHER fix (not
// failing on empty intervals) shipped alone, without this check alongside
// it.
func TestPrePushGateBlocksNeedleInTagMessageEvenWithZeroCommits(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)
	const needle = "AgulhaDeMensagemDeTag1204"

	mustGit(t, cloneDir, "tag", "-a", "v0.204.1", "-m",
		"release note that accidentally mentions "+needle+" right here")
	tagSha := mustGit(t, cloneDir, "rev-parse", "v0.204.1")

	commits, err := commitsForPushedInterval(cloneDir, zeroSha, tagSha)
	if err != nil {
		t.Fatalf("commitsForPushedInterval: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("fixture invalida: esperava intervalo vazio (tag sobre o commit base ja publicado), "+
			"obtive %v", commits)
	}
	reachable, rerr := objectAlreadyReachableFromRemotes(cloneDir, tagSha)
	if rerr != nil {
		t.Fatalf("objectAlreadyReachableFromRemotes: %v", rerr)
	}
	if !reachable {
		t.Fatalf("fixture invalida: esperava %s alcancavel a partir de origin/main", tagSha)
	}

	matched, message, serr := sweepTagMessage(t, cloneDir, tagSha, []string{needle})
	if serr != nil {
		t.Fatalf("sweepTagMessage: %v", serr)
	}
	if !matched {
		t.Fatalf("esperava bloqueio: a agulha so' existe na MENSAGEM da tag, e o intervalo de commits " +
			"e' vazio — exatamente o caso que T-204 introduz; se isto passar em branco, o portao voltou " +
			"a nao olhar para a mensagem da tag")
	}
	if !strings.Contains(message, needle) {
		t.Fatalf("a mensagem de bloqueio nao cita a agulha %q: %s", needle, message)
	}
	if !strings.Contains(message, tagSha) {
		t.Fatalf("a mensagem de bloqueio nao cita a tag %s: %s", tagSha, message)
	}
}

// TestObjectAlreadyReachableFromRemotesFalseForUnpublishedCommit is the
// contrast case that proves objectAlreadyReachableFromRemotes actually
// discriminates instead of always answering true: a brand-new commit that
// was never pushed anywhere must come back NOT reachable, matching the
// non-empty commit list commitsForPushedInterval reports for the exact same
// sha (already covered by TestPrePushGateNewRefCleanBranchPasses) — the two
// signals have to agree in both directions, not just the "empty" one.
func TestObjectAlreadyReachableFromRemotesFalseForUnpublishedCommit(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)

	mustGit(t, cloneDir, "checkout", "-q", "-b", "feature-unpublished")
	if err := os.WriteFile(filepath.Join(cloneDir, "unpublished.txt"), []byte("never pushed\n"), 0o644); err != nil {
		t.Fatalf("escrever unpublished.txt: %v", err)
	}
	mustGit(t, cloneDir, "add", "unpublished.txt")
	mustGit(t, cloneDir, "commit", "-q", "-m", "commit that never reached origin")
	newSha := mustGit(t, cloneDir, "rev-parse", "HEAD")

	reachable, err := objectAlreadyReachableFromRemotes(cloneDir, newSha)
	if err != nil {
		t.Fatalf("objectAlreadyReachableFromRemotes: %v", err)
	}
	if reachable {
		t.Fatalf("um commit nunca empurrado nao deveria estar alcancavel a partir de nenhuma ref de " +
			"rastreamento remoto, mas o teste disse que estava")
	}
}

// TestSweepTagMessageSkipsLightweightTagsAndPlainCommits confirms
// sweepTagMessage's "nothing to check" shape for the two ref kinds that are
// NOT annotated tag objects: a lightweight tag (its ref points straight at
// the commit, no separate tag object) and an ordinary commit sha (a plain
// branch push). Both must come back matched=false with no error — there is
// no message object to read, not an error reading one.
func TestSweepTagMessageSkipsLightweightTagsAndPlainCommits(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)
	commitSha := mustGit(t, cloneDir, "rev-parse", "HEAD")

	mustGit(t, cloneDir, "tag", "v0.204.2-lightweight") // no -a, no -m: not a tag OBJECT
	lightweightSha := mustGit(t, cloneDir, "rev-parse", "v0.204.2-lightweight")
	if lightweightSha != commitSha {
		t.Fatalf("fixture invalida: tag leve deveria apontar direto para o commit %s, apontou para %s",
			commitSha, lightweightSha)
	}

	needles := []string{"AgulhaQueNaoDeveriaImportarAqui1204"}

	for _, sha := range []string{commitSha, lightweightSha} {
		matched, message, err := sweepTagMessage(t, cloneDir, sha, needles)
		if err != nil {
			t.Fatalf("sweepTagMessage(%s): %v", sha, err)
		}
		if matched {
			t.Fatalf("sweepTagMessage(%s) bloqueou, mas nao ha objeto de tag anotada aqui: %s", sha, message)
		}
	}
}

// TestAnnotatedTagMessageStripsHeaderLines guards against a false positive
// that would be easy to introduce by sweeping `git cat-file -p`'s raw
// output: the header block includes a "tagger Name <email> ..." line, which
// carries a real name every time (whoever ran `git tag -a`). Sweeping the
// FULL cat-file output against the name gate would make every annotated tag
// this repository's own maintainer creates a false positive. This proves
// annotatedTagMessage returns ONLY the text after the header's blank line.
func TestAnnotatedTagMessageStripsHeaderLines(t *testing.T) {
	cloneDir := newDisposableRemoteAndClone(t)
	mustGit(t, cloneDir, "tag", "-a", "v0.204.3", "-m", "so' isto deveria sobrar")
	tagSha := mustGit(t, cloneDir, "rev-parse", "v0.204.3")

	msg, err := annotatedTagMessage(cloneDir, tagSha)
	if err != nil {
		t.Fatalf("annotatedTagMessage: %v", err)
	}
	if strings.Contains(msg, "tagger ") || strings.Contains(msg, "object ") || strings.Contains(msg, "type commit") {
		t.Fatalf("annotatedTagMessage vazou linhas de cabecalho, deveria conter so' a mensagem: %q", msg)
	}
	if !strings.Contains(msg, "so' isto deveria sobrar") {
		t.Fatalf("annotatedTagMessage nao contem o texto esperado da mensagem: %q", msg)
	}
}
