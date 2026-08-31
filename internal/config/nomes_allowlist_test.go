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

// T-193's GATE. The phone-number gate (telefones_allowlist_test.go) only
// ever covered ONE kind of personal data — CLAUDE.md said so, in writing,
// in the rules table. The hole it declared bit: a real customer's name
// reached this PUBLIC repository anyway, in docs and in test fixtures. This
// file closes that specific hole the same way T-161/T-162/T-191 closed the
// phone one: fail CLOSED when the needle list can't be loaded, and fail
// LOUD (never t.Skip) when a needle shows up.
//
// 🔴 UNLIKE THE PHONE GATE, THIS ONE HAS NO ALLOWLIST OF VALUES AND NO
// PER-FILE EXEMPTION. A phone number can be legitimately synthetic (declare
// it and move on); a customer's name showing up in this repository is never
// legitimate — there is no synthetic form of "insert a real name here" that
// makes sense to pre-approve. Any match is a finding, full stop. If a
// finding turns out to need discussion, that discussion is the owner's, not
// a map in this file.
//
// 🔴 THE NEEDLE LIST DOES NOT LIVE HERE, OR ANYWHERE IN THIS REPOSITORY.
// Writing the list of forbidden names INSIDE a public repository publishes
// exactly what the gate exists to keep out. It comes from OUTSIDE the tree,
// in this order: the ZAPGW_FORBIDDEN_NAMES environment variable first, then
// ~/.zapgw/forbidden-names.txt. If neither produces at least one needle,
// the test FAILS saying it could not verify — that failure is a different
// message from a finding, on purpose (see loadForbiddenNames): the two
// outcomes collapse into the same color if nothing separates them, and
// "could not verify" reading as "clean" is exactly the deception this
// project's documentation rules warn about.

// forbiddenNamesEnvVar is read first. One needle per line — see
// parseForbiddenNamesLines for the exact format.
const forbiddenNamesEnvVar = "ZAPGW_FORBIDDEN_NAMES"

// forbiddenNamesFileRelativeToHome is where the needle list lives on disk
// when the environment variable isn't set. Deliberately outside the
// repository (see the file-level comment above).
const forbiddenNamesFileRelativeToHome = ".zapgw/forbidden-names.txt"

// parseForbiddenNamesLines turns raw text (the env var's value, or the
// file's content) into needles: one per non-blank, non-comment line,
// trimmed. Shared by both sources so the format only has to be defined
// once.
func parseForbiddenNamesLines(raw string) []string {
	var needles []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		needles = append(needles, line)
	}
	return needles
}

// loadForbiddenNames tries ZAPGW_FORBIDDEN_NAMES first, then
// ~/.zapgw/forbidden-names.txt, in that order (T-193's Do (2)). It returns
// the needles plus where they came from (logged on every run, findings or
// not — same reasoning as the phone gate logging its exemptions), or an
// error naming BOTH attempts when neither produced anything usable.
//
// This is the function that makes "could not verify" and "found nothing"
// two different code paths: a missing or empty source returns an error
// HERE, before the sweep ever runs, and the caller turns that into
// t.Fatalf — never into a silent, empty-findings pass. An env var that is
// SET but empty (or only comments/blank lines) still falls through to the
// file, exactly like an unset one: "declared but useless" and "not
// declared" both mean "try the next source," and if both fail the error
// says so about each.
func loadForbiddenNames() (needles []string, source string, err error) {
	var envAttempt, fileAttempt string

	if raw, present := os.LookupEnv(forbiddenNamesEnvVar); present {
		parsed := parseForbiddenNamesLines(raw)
		if len(parsed) > 0 {
			return parsed, "variavel de ambiente " + forbiddenNamesEnvVar, nil
		}
		envAttempt = fmt.Sprintf("variavel de ambiente %s esta definida mas nao produziu "+
			"nenhuma agulha (vazia, ou so' linhas em branco/comentario)", forbiddenNamesEnvVar)
	} else {
		envAttempt = fmt.Sprintf("variavel de ambiente %s nao esta definida", forbiddenNamesEnvVar)
	}

	home, herr := os.UserHomeDir()
	if herr != nil {
		fileAttempt = fmt.Sprintf("nao foi possivel localizar o diretorio home: %v", herr)
	} else {
		path := filepath.Join(home, filepath.FromSlash(forbiddenNamesFileRelativeToHome))
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			fileAttempt = fmt.Sprintf("%s: %v", path, rerr)
		} else {
			parsed := parseForbiddenNamesLines(string(content))
			if len(parsed) > 0 {
				return parsed, path, nil
			}
			fileAttempt = fmt.Sprintf("%s existe mas nao produziu nenhuma agulha "+
				"(vazio, ou so' linhas em branco/comentario)", path)
		}
	}

	return nil, "", fmt.Errorf("NAO CONSEGUI VERIFICAR (falha fechada, isto NAO e' \"esta limpo\"): "+
		"nenhuma fonte de agulhas disponivel — %s; %s", envAttempt, fileAttempt)
}

// forbiddenNameFinding is one line where a needle showed up.
type forbiddenNameFinding struct {
	file  string
	line  int
	match string
}

// sweepForbiddenNamesOutsideTheGate scans root/<target> for each target in
// targets looking for any needle in `needles` — case-insensitive, at a word
// boundary on both sides (T-193's Do (4)): without the boundary, a short
// first name among the needles matches inside an ordinary word and the gate
// turns into noise nobody reads. There is deliberately no allowlist and no
// per-file exemption here — see the file-level comment.
func sweepForbiddenNamesOutsideTheGate(root string, targets []string, needles []string) (findings []forbiddenNameFinding, seen map[string]bool, err error) {
	seen = map[string]bool{}

	patterns := make([]*regexp.Regexp, len(needles))
	for i, needle := range needles {
		patterns[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(needle) + `\b`)
	}

	scanFile := func(path string) error {
		relative, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return fmt.Errorf("relativizar %s: %w", path, rerr)
		}
		relative = filepath.ToSlash(relative)
		seen[relative] = true

		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("ler %s: %w", path, rerr)
		}
		for i, line := range strings.Split(string(content), "\n") {
			for _, pattern := range patterns {
				if m := pattern.FindString(line); m != "" {
					findings = append(findings, forbiddenNameFinding{file: relative, line: i + 1, match: m})
				}
			}
		}
		return nil
	}

	for _, target := range targets {
		base := filepath.Join(root, target)
		info, serr := os.Stat(base)
		if serr != nil {
			return nil, nil, fmt.Errorf("ler %s: %w", base, serr)
		}
		if !info.IsDir() {
			if err := scanFile(base); err != nil {
				return nil, nil, fmt.Errorf("varrer %s: %w", base, err)
			}
			continue
		}
		werr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("ler %s: %w", path, err)
			}
			if d.IsDir() {
				// Same reason as the phone gate and the TLS gate: .claude/
				// holds other implementer agents' worktrees, not this
				// commit's code. Any other hidden directory isn't either.
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			return scanFile(path)
		})
		if werr != nil {
			return nil, nil, fmt.Errorf("varrer %s: %w", base, werr)
		}
	}
	return findings, seen, nil
}

// TestNoCustomerNameOutsideTheGateInTheRepo is T-193's gate: it loads the
// needle list from OUTSIDE the repository (loadForbiddenNames), enumerates
// every file `git` sees from the module root — the same enumeration the
// phone gate uses (filesGitSeesFromRoot, T-191) — and fails if any needle
// shows up anywhere in it, case-insensitive, at a word boundary.
//
// Fails CLOSED at every step: no needles loadable, no module root, no file
// enumeration, or a read error while sweeping all turn into t.Fatalf, never
// into a pass with fewer files checked than the caller believes.
func TestNoCustomerNameOutsideTheGateInTheRepo(t *testing.T) {
	needles, source, err := loadForbiddenNames()
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("agulhas carregadas de: %s (%d agulha(s))", source, len(needles))

	root, err := moduleRootForTheAllowlist()
	if err != nil {
		t.Fatalf("localizar a raiz do modulo (falha fechada): %v", err)
	}

	targets, err := filesGitSeesFromRoot(root)
	if err != nil {
		t.Fatalf("enumerar os arquivos que o git ve (falha fechada): %v", err)
	}

	findings, seen, err := sweepForbiddenNamesOutsideTheGate(root, targets, needles)
	if err != nil {
		t.Fatalf("varrer a arvore (falha fechada): %v", err)
	}
	if len(seen) == 0 {
		t.Fatalf("a varredura nao alcancou nenhum arquivo — falha fechada, nunca tratado como limpo")
	}

	if len(findings) > 0 {
		sort.Slice(findings, func(i, j int) bool {
			if findings[i].file != findings[j].file {
				return findings[i].file < findings[j].file
			}
			return findings[i].line < findings[j].line
		})
		var lines []string
		for _, f := range findings {
			lines = append(lines, fmt.Sprintf("%s:%d: %s", f.file, f.line, f.match))
		}
		t.Fatalf("nome fora do portao encontrado (%d ocorrencia(s)):\n%s\n\n"+
			"Cada linha acima e' onde uma agulha apareceu. Este portao NAO TEM lista de "+
			"isencoes: se o achado for legitimo, PARE e leve para o dono; se for um "+
			"exemplo, troque por um valor sintetico que preserve o formato.",
			len(findings), strings.Join(lines, "\n"))
	}
}
