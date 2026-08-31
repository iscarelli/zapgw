package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// T-161's GATE. T-159 took the OWNER's phone number out of the code; when
// measuring what was left, something worse showed up: a REAL number
// belonging to a CUSTOMER of `consumer-b` — THIRD-PARTY data the owner
// never decided to publish — was in 7 files, two of them in PRODUCTION
// (internal/config/transit.go, internal/meta/phone.go). T-161 swapped
// that number for the synthetic one already in use and created this scan
// so the NEXT real number doesn't get in the same way.
//
// 🔴 WHY AN ALLOWLIST, AND NOT A DENY-LIST OF THE KNOWN NUMBER: it was
// exactly a deny-list that failed in T-159 — that task's scan searched for
// ONE number and there were 17 distinct area-code-32/19 numbers in the
// code. Searching for the number you know only finds the number you
// already know. The allowlist inverts this: a new number in the code ONLY
// gets in if someone declares it down below. Forgetting to declare it
// fails CLOSED (the scan fails), never open.
//
// CLASSIFICATION of the area-code-32/19 numbers found in this scan
// (T-161), each one checked against the file that uses it, never guessed:
//
//   - 5532999990000 (and the sequential variations 5532999990001/…0002/…0003/
//     …0099, 5532900000001, 5532900000009, and the form without the ninth
//     digit 553299990000) are the synthetic area-code-32 base in use since
//     T-138/T-159 — the pair of 5511999990000 (area code 11). Several are
//     labeled "// sintetico" right in the code (cmd/zapgw/log_test.go,
//     internal/config/transit_test.go).
//   - 553288888888 / 5532988888888: fixture from the synthetic corpus
//     (testdata/corpus/status_*.json), documented in
//     testdata/corpus/README.md as the corpus's NEW recipient, alongside
//     WABA_TESTE/PNID_TESTE/user_id BR.2000...
//   - 553234567890 / 553245678901: FIXED synthetic numbers (digit sequence
//     234567890 / 345678901) used in internal/meta/phone_test.go to prove
//     Canonicalize does NOT insert the ninth digit on a landline.
//   - 553284630011: synthetic, pair of 551199990000 in
//     internal/meta/parse_test.go — fictional contact names
//     ("Fulana"/"Sicrana"), with no relation to any real number.
//   - 551987654321: not an area code — it's an Instagram IGSID that
//     HAPPENS to have the SHAPE of a phone number
//     (internal/outbound/instagram_test.go), used on purpose to prove the
//     transit log doesn't canonicalize it.
//
// consumer-b's customer's REAL number ((32) 9xxxx-xx72, found in 7 files:
// cmd/zapgw/log_test.go, cmd/zapgw/transit_test.go,
// internal/config/transit.go, internal/config/transit_test.go,
// internal/meta/phone.go, internal/meta/phone_test.go,
// internal/outbound/transit_test.go) was swapped for 5532999990000 in
// this task and is NO LONGER in the allowlist — if it reappears, the scan
// fails just like any other undeclared number.
var syntheticPhoneAllowlist = map[string]bool{
	// Area code 11 — synthetic base adopted by T-138 and spread by T-159.
	"5511999990000": true,
	"551199990000":  true,
	"5511999990001": true,
	"5511987654321": true,
	"551187654321":  true,
	"5511900000000": true,
	"5511900000001": true,
	"5511900000002": true,
	"5511900000030": true,
	"5511900000040": true,
	"5511900000077": true,
	"5511900000099": true,
	"5511888880000": true,
	"5511777770000": true,
	// T-185, found when implanta/ entered the sweep: the `para` of the
	// POST that implanta/valida-lideranca.sh:93 fires against a gateway
	// it just started. All nines after the area code — synthetic by
	// construction, and it is never meant to reach the Meta.
	"5511999999999": true,
	"551155555555":  true, // synthetic landline, starts with 5
	"551123456789":  true, // synthetic landline, starts with 2

	// Area code 32 — the synthetic pair of area code 11, adopted by T-159
	// and used in this task (T-161) to replace the customer's real number.
	"5532999990000": true,
	"553299990000":  true,
	"5532999990001": true,
	"5532999990002": true,
	"5532999990003": true,
	"5532999990099": true,
	"5532900000001": true,
	"5532900000009": true,
	"553234567890":  true, // synthetic landline, starts with 3
	"553245678901":  true, // synthetic landline, starts with 4
	"553284630011":  true, // synthetic, pair of 551199990000 (parse_test.go)
	"553288888888":  true, // corpus fixture (testdata/corpus/README.md)
	"5532988888888": true, // same, form with the ninth digit

	// Area code 19 — actually an Instagram IGSID with the SHAPE of a
	// phone number (instagram_test.go), not a subscriber's number.
	"551987654321": true,
}

// filesExemptFromThePhoneScan is T-185's list of EXCEPTIONS, and it is
// deliberately BY FILE, never by directory.
//
// 🔴 WHY BY FILE: T-185 added docs/, implanta/ and README.md to the sweep
// precisely because the owner's real phone number that is left in this
// repository lives there (measured 2026-08-30). Exempting all of docs/
// would re-create, under another name, the exact hole the task exists to
// close: the next doc written would inherit the exemption without anyone
// deciding it. Each line below is a file someone NAMED, with the reason
// written down.
//
// 🔴 WHY THE EXEMPTION IS THE WHOLE FILE AND NOT "the owner's number in
// the allowlist": declaring that number in syntheticPhoneAllowlist would
// mean WRITING IT ONE MORE TIME, here in internal/ — and CLAUDE.md's hard
// rule ("abrir este repositorio tem um custo obrigatorio embutido") says
// every new occurrence raises the cost of the history rewrite that
// becomes mandatory the day this repository goes public. The allowlist is
// the wrong tool for a number that must not be typed again.
//
// The two files below are HISTORICAL RECORD that stays in this PRIVATE
// repository — docs/PSEUDONIMOS.md lists exactly what does not migrate,
// and names both of them ("Registro reescrito e registro falsificado").
// The repository that will be published is `zapgw`, not this one.
//
// The exemption fails closed in two ways: the reason is DATA, not a
// comment (an entry cannot be added without writing why), and the test
// below checks that every file named here was actually reached by the
// sweep — so a renamed or deleted file turns into a failure instead of a
// stale exception nobody notices.
//
// 🔴 THE ENTRIES MARKED "PROVISORIA" ARE NOT A DECISION, THEY ARE A
// PENDING ONE. T-185 measured the tree and found the owner's real number
// (and, in one file, a THIRD PARTY's) in docs that PSEUDONIMOS.md does
// NOT list as staying behind. Removing a number from a doc is the
// owner's call, not an implementer's, so the gate was made to pass while
// naming exactly which files are carrying the debt — instead of a green
// run that hides it. The test prints them on every run.
var filesExemptFromThePhoneScan = map[string]string{
	// 🔴 VAZIO, E ISSO E' A PROPRIEDADE QUE ESTE REPOSITORIO EXISTE PARA TER.
	//
	// No `zapgw-dev` (o repositorio privado de onde este codigo veio) havia
	// cinco isencoes: documentos de registro que carregam o telefone real do
	// dono e, num deles, o de um CLIENTE de consumidor. Eles ficaram la, por
	// decisao — sao historico, e historico reescrito e' historico falsificado.
	//
	// AQUI NENHUM ARQUIVO E' ISENTO, e nenhum deve passar a ser. Este
	// repositorio e' publicado: uma isencao aqui nao e' "um caso combinado",
	// e' um telefone real na internet, para sempre. Se algum dia um arquivo
	// PRECISAR de telefone real, ele nao pertence a este repositorio.
	//
	// A checagem abaixo continua valendo de qualquer forma: isencao cujo
	// arquivo sumiu reprova. Com o mapa vazio ela nao tem o que fazer — e
	// esse e' o estado desejado, nao um descuido.
}

// agulha is the same pattern used on both search fronts (literal and
// decoded): "55" followed by 10 or 11 digits, with a word boundary.
// Shared so there is ONE definition of what counts as a phone number, not
// two that could diverge.
var needle = regexp.MustCompile(`\b55[0-9]{10,11}\b`)

// wamidRegex finds the base64 payload of a `wamid.<payload>` identifier.
// Only the standard base64 alphabet (A-Za-z0-9+/=) goes in here —
// deliberately narrower than "anything between quotes": a wide alphabet
// (with hyphen/underscore, for example) would match hundreds of CamelCase
// Go identifiers from the repository itself (error names, function names,
// route names) that were never any base64 at all, and each one would
// become noise to investigate. `wamid.<...>` is the ONLY structured
// identifier with an embedded phone number documented so far
// (docs/ARMADILHAS.md, the variant of "o wamid carrega o telefone do
// destinatário dentro dele"); restricting to it is the minimum scope T-162 asks for,
// and it avoids turning every hash/UUID in the repo into a false positive.
var wamidRegex = regexp.MustCompile(`wamid\.([A-Za-z0-9+/=]+)`)

// minimumToHideAPhoneNumber is the smallest base64 payload capable of
// embedding the SMALLEST number the needle recognizes (12 ASCII
// characters: "55" + 10 digits). 12 bytes = 96 bits = exactly 16 base64
// characters (96/6), with no remainder — that's 4 complete groups of 3
// bytes, so any 12 bytes (the CONTENT doesn't matter for this math, only
// the COUNT) always encode into exactly 16 characters when embedded as a
// substring, with no padding. A payload shorter than this CANNOT FIT any
// 12+ digit number anywhere inside it, whatever the offset — it's math,
// not a guess. The 50 `wamid.A`/`wamid.TESTE001`/etc. in the test corpus
// (payload of 1 to ~15 characters) fall here and are skipped WITHOUT
// decoding, with the reason written down instead of silently assumed.
const minimumToHideAPhoneNumber = 16

// twelveDigitLength and thirteenDigitLength are the ONLY window
// lengths phoneNumbersInsideTheWamid tries — not a wide range. The needle only
// recognizes 12 or 13 ASCII characters ("55" + 10 or 11 digits), and each
// one corresponds to EXACTLY one base64 length (12 bytes -> 16 chars; 13
// bytes -> 18 chars, counting 4 complete groups plus 1 leftover byte
// encoded in 2 chars). Trying intermediate lengths (e.g. 14) would produce
// a window cut IN THE MIDDLE of a 13-digit number — an "almost right"
// number that would match the needle (12 of the 13 digits, with a word
// boundary at the end of the truncated window) but doesn't correspond to
// ANY real form of the phone number, only to an accident of where the
// window stopped. That's how this function's first version produced, from
// the synthetic wamid in internal/config/transit_test.go, a second
// number outside the allowlist — the same real number, just with the last
// digit cut off by the short window — as if it were one more phone number
// to declare, when there was only ONE number there.
const (
	twelveDigitLength   = 16
	thirteenDigitLength = 18
)

// legivel decides whether a decode produced text worth looking at:
// printable ASCII from start to end. The rest of a real `wamid`'s payload
// (after the phone number) is binary — it doesn't decode into text — and
// that's why it does NOT count as "managed to read something", which is
// exactly the desired behavior: only readable text proves that piece was actually read.
func readable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

// decodeBase64WithPadding decodes `s`, fixing the padding from the
// character count (standard base64 requires a multiple of 4; a remainder
// of 1 is never valid, not even with padding). It's the direct fix for
// the trap described in docs/ARMADILHAS.md: decoding a `wamid` capture
// WHOLE, from offset zero, blows past that padding before reaching the
// piece that matters; decoding only the PIECE of the right size, at the
// right position, doesn't trip over it.
func decodeBase64WithPadding(s string) ([]byte, bool) {
	rest := len(s) % 4
	if rest == 1 {
		return nil, false
	}
	filled := s + strings.Repeat("=", (4-rest)%4)
	decoded, err := base64.StdEncoding.DecodeString(filled)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

// phoneNumbersInsideTheWamid tries to decode, at EVERY possible offset in the
// payload, the two lengths that correspond to a 12- or 13-digit number
// (twelveDigitLength/thirteenDigitLength) — never the whole
// capture at once, which is what blew past padding and hid the phone
// number before T-162. At each offset, the 13-digit one is tried FIRST:
// if it decodes readably, the 12-digit one AT THE SAME POSITION is
// skipped, because it would just be the same number with the last digit
// cut off (see the comment on twelveDigitLength) — not a different number.
//
// Returns the phone numbers (in the needle's format) found in any
// readable window, and a bool saying whether ANY window decoded into
// readable text — if none decoded (not even into readable garbage, nor
// into the phone number), decodificavel comes back false and the caller
// treats that as a FINDING, never as "clean". It's T-162's requirement
// (c): a decoding failure is not absence.
func phoneNumbersInsideTheWamid(payload string) (hits []string, decodable bool) {
	hitSet := map[string]bool{}
	n := len(payload)
	for start := 0; start < n; start++ {
		foundHere := false
		if start+thirteenDigitLength <= n {
			chunk := payload[start : start+thirteenDigitLength]
			if decoded, ok := decodeBase64WithPadding(chunk); ok && readable(decoded) {
				foundHere = true
				decodable = true
				for _, number := range needle.FindAllString(string(decoded), -1) {
					hitSet[number] = true
				}
			}
		}
		if !foundHere && start+twelveDigitLength <= n {
			chunk := payload[start : start+twelveDigitLength]
			if decoded, ok := decodeBase64WithPadding(chunk); ok && readable(decoded) {
				decodable = true
				for _, number := range needle.FindAllString(string(decoded), -1) {
					hitSet[number] = true
				}
			}
		}
	}
	for number := range hitSet {
		hits = append(hits, number)
	}
	sort.Strings(hits)
	return hits, decodable
}

// sweepPhoneNumbersOutsideTheAllowlist scans `root`/<target> for each target in
// `targets` looking for phone numbers outside the allowlist — literal ones AND
// ones hidden in base64 inside `wamid.<payload>`. Pulled out into its own
// function (instead of living inside the test's body) so T-162's POSITIVE
// CONTROL (TestTheBase64GateIFoundAPhoneNumberInItsRealForm, below) runs the
// SAME logic against a temporary tree, instead of reimplementing a
// parallel version that could diverge from the one that runs in production.
//
// A target is a DIRECTORY (walked whole) or a SINGLE FILE — T-185 needed
// the root README.md, which is a file, not a sub-tree. The two cases are
// separated explicitly instead of leaning on filepath.WalkDir's behavior
// when handed a file: it does work, but silently, and a reader would have
// to know that detail to trust that README.md is really being read.
//
// Fails CLOSED, and that is the whole point of this function: an error
// reading any target comes back as an error, never as "found nothing".
// The os.Stat below is what turns a target that has been renamed or
// deleted into a failure instead of a sweep that quietly covers less than
// its caller believes.
//
// 📌 WHERE THIS GUARD STOPS, said out loud because "today it doesn't
// exist" is not a mechanism: the base64 front ONLY decodes what carries
// the literal `wamid.` prefix (wamidRegex). A phone number base64-encoded
// into ANY OTHER field — an opaque id, a token, a payload dumped into a
// doc, a JSON fixture value — goes through untouched. Measured
// 2026-08-30: zero such occurrences in testdata/. That measurement is the
// reason the hole was left open, not a reason to believe it is closed;
// the next reader has to know the boundary before trusting a green run.
// Widening the alphabet to "anything that looks like base64" was tried
// and rejected in T-162 (see the comment on wamidRegex): it turns every
// CamelCase Go identifier in the repository into a finding to
// investigate, and a gate that cries wolf gets ignored.
func sweepPhoneNumbersOutsideTheAllowlist(root string, targets []string) (outsideTheAllowlist []string, seen map[string]bool, finalError error) {
	seen = map[string]bool{}

	scanFile := func(path string) error {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativizar %s: %w", path, err)
		}
		relative = filepath.ToSlash(relative)
		seen[relative] = true

		if _, exempt := filesExemptFromThePhoneScan[relative]; exempt {
			// Named, file-by-file exception — see the comment on
			// filesExemptFromThePhoneScan for why the exemption is the
			// file and not an entry in the allowlist. It stays in `seen`
			// because it WAS reached: that is what lets the test below
			// notice an exception that has gone stale.
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("ler %s: %w", path, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			// Front 1: a literal number in the text — T-161's original gate.
			for _, number := range needle.FindAllString(line, -1) {
				if !syntheticPhoneAllowlist[number] {
					outsideTheAllowlist = append(outsideTheAllowlist,
						fmt.Sprintf("%s:%d: %s", relative, i+1, number))
				}
			}

			// Front 2 (T-162): a number hidden in base64 inside a
			// `wamid.<payload>`. It applies to MARKDOWN exactly as it
			// does to Go: T-185 brought docs/ into the sweep, and docs/
			// carries `wamid` both in prose and inside code fences. On
			// 2026-08-30 a consumer found, in their own repository, a
			// committed wamid with the owner's phone number inside it —
			// invisible to a grep for the number as a human writes it.
			for _, m := range wamidRegex.FindAllStringSubmatch(line, -1) {
				payload := m[1]
				if len(payload) < minimumToHideAPhoneNumber {
					// Mathematically too small to fit a 12+ digit
					// number in base64 — see the comment on
					// minimumToHideAPhoneNumber. Skipped WITHOUT
					// decoding, and the reason is written there, not
					// silently assumed.
					continue
				}
				hits, decodable := phoneNumbersInsideTheWamid(payload)
				if !decodable {
					// T-162's requirement (c): a decoding failure IS a
					// FINDING, never absence. No window of this
					// payload produced readable text — it could be a
					// wamid format this gate doesn't understand yet,
					// or a corrupted value. Either way, it fails,
					// asking for a human eye instead of silently passing.
					outsideTheAllowlist = append(outsideTheAllowlist,
						fmt.Sprintf("%s:%d: NAO DECODIFICOU (achado, precisa de olho humano): wamid.%s", relative, i+1, payload))
					continue
				}
				for _, number := range hits {
					if !syntheticPhoneAllowlist[number] {
						outsideTheAllowlist = append(outsideTheAllowlist,
							fmt.Sprintf("%s:%d: %s (decodificado de wamid.%s)", relative, i+1, number, payload))
					}
				}
			}
		}
		return nil
	}

	for _, target := range targets {
		base := filepath.Join(root, target)
		info, err := os.Stat(base)
		if err != nil {
			return nil, nil, fmt.Errorf("ler %s: %w", base, err)
		}
		if !info.IsDir() {
			// Single file (README.md). Same existence check as a
			// directory — a missing file is an error, never silence.
			if err := scanFile(base); err != nil {
				return nil, nil, fmt.Errorf("varrer %s: %w", base, err)
			}
			continue
		}
		err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("ler %s: %w", path, err)
			}
			if d.IsDir() {
				// Same reason as the TLS test: .claude/ holds other
				// implementer agents' worktrees, not this commit's code.
				// Any other hidden directory isn't either.
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			return scanFile(path)
		})
		if err != nil {
			return nil, nil, fmt.Errorf("varrer %s: %w", base, err)
		}
	}
	return outsideTheAllowlist, seen, nil
}

// filesGitSeesFromRoot is T-191's fix for the hole a fixed directory list
// always has: it enumerates EXACTLY the set a `git add -A` from `root`
// would pick up — every TRACKED file (`git ls-files`) plus every file that
// is new and NOT ignored (`git ls-files --others --exclude-standard`) —
// instead of a hand-maintained list of directories somebody has to
// remember to grow.
//
// 🔴 Real cost, 2026-08-30: the consumer channel file (real phone number,
// production `wamid`) was created at the REPOSITORY ROOT, which was not on
// scannedTargets (the fixed list this function replaces). It sat untracked
// in a PUBLIC repository and the gate said nothing — enumerating a list of
// paths forgets the path nobody added to the list. Enumerating what git
// sees does not have that failure mode: a new file anywhere, including the
// root, is in the set the moment it exists, with nobody having to edit
// this file.
//
// 🔴 An IGNORED file (`*.local.md`, the channel with a consumer — real
// phone number and production `wamid` by design) is deliberately NOT in
// this set. `git ls-files --others --exclude-standard` already excludes
// what `.gitignore` excludes; sweeping it anyway would turn this project's
// verify permanently red (docs/TASKS.md, T-191's Verify: the negative
// control), and a verify that always screams is a verify nobody reads.
// That is the same reason `*.local.md` exists as a `.gitignore` line in
// the first place (see the comment there) — this function just has to not
// go around it.
//
// Fails CLOSED: git missing from PATH, either command failing, or the
// combined result coming back with ZERO files are all treated as "could
// not verify" (t.Fatalf via the caller), never as "the repository is
// clean". A git checkout of this module always has at least go.mod,
// CLAUDE.md and .gitignore tracked — zero files means the measurement
// itself is broken, not that there is nothing to scan.
func filesGitSeesFromRoot(root string) ([]string, error) {
	tracked, err := gitLsFiles(root, "ls-files")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	untracked, err := gitLsFiles(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others --exclude-standard: %w", err)
	}

	seen := map[string]bool{}
	var all []string
	for _, f := range append(tracked, untracked...) {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		all = append(all, f)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("git nao devolveu nenhum arquivo a partir de %s "+
			"(rastreado + nao-rastreado-e-nao-ignorado) — falha fechada: nunca "+
			"tratado como \"repositorio vazio\"", root)
	}
	sort.Strings(all)
	return all, nil
}

// gitLsFiles runs `git <args...>` with its working directory set to root
// and returns the output split into lines. A missing git binary or a
// non-zero exit both come back as an error — the caller (filesGitSeesFromRoot)
// is what turns that into a failed-closed t.Fatalf.
func gitLsFiles(root string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// TestNoPhoneNumberOutsideTheAllowlistInTheRepo is T-161's gate: it scans
// every file `git` sees from the module root — every TRACKED file plus
// every untracked-and-not-ignored one, via filesGitSeesFromRoot (T-191) —
// looking for any "55" sequence followed by 10 or 11 digits (the shape of
// a Brazilian number with the country code, with or without the ninth
// digit) and fails if the found value isn't in the allowlist above. A new
// number only gets into the repo if someone declares it there — the list
// fails CLOSED.
//
// 🔴 T-191 replaced a fixed list of directories (cmd/, internal/,
// testdata/, docs/, implanta/, README.md) with this git-based enumeration
// precisely because the fixed list left the repository ROOT out, and a
// file created there (the consumer channel, real phone number) sat
// untracked in a public repository with the gate silent. Enumerating what
// git sees covers any new file anywhere — including the root — the moment
// it exists, with nobody having to edit this file to add it.
//
// Unlike the TLS scan (internal/inbound/deliver_test.go), the needle here
// does NOT need to be built by concatenation: the allowlist is what this
// test PERMITS, so the very numbers declared above showing up in the file
// (and being found by the scan) is the correct behavior, not a self-accusation.
//
// Fails CLOSED: any error locating the module root, enumerating the files
// git sees, or reading the tree turns into t.Fatalf, never a silent
// "found nothing".
//
// 🔴 T-162's EXTENSION: the original gate only matched the LITERAL number
// in the text. A phone number hidden in base64 inside a `wamid.<...>`
// passed clean — that's how it survived T-159 (docs/ARMADILHAS.md, the
// variant marked 🔥 of "wamid carrega o telefone"). Every line
// is now scanned TWICE: by the literal (as before) and by
// phoneNumbersInsideTheWamid on every `wamid.<payload>` found in it — and the
// numbers that come out of EITHER of the two fronts pass through the SAME
// syntheticPhoneAllowlist (a single list, T-162's requirement (d)).
func TestNoPhoneNumberOutsideTheAllowlistInTheRepo(t *testing.T) {
	root, err := moduleRootForTheAllowlist()
	if err != nil {
		t.Fatalf("localizar a raiz do modulo (falha fechada): %v", err)
	}

	targets, err := filesGitSeesFromRoot(root)
	if err != nil {
		t.Fatalf("enumerar os arquivos que o git ve (falha fechada): %v", err)
	}

	outsideTheAllowlist, seen, err := sweepPhoneNumbersOutsideTheAllowlist(root, targets)
	if err != nil {
		t.Fatalf("varrer a arvore (falha fechada): %v", err)
	}

	if len(outsideTheAllowlist) > 0 {
		sort.Strings(outsideTheAllowlist)
		t.Fatalf("numero(s) fora da allowlist encontrado(s):\n%s\n\n"+
			"Se e sintetico, declare-o em syntheticPhoneAllowlist "+
			"(internal/config/phones_allowlist_test.go) com o porque. "+
			"Se e real, ele NAO PODE entrar no repositorio — troque pelo "+
			"sintetico 5511999990000/5532999990000 (CLAUDE.md, secao do "+
			"telefone do dono). Se a linha diz 'NAO DECODIFICOU', o portao "+
			"nao conseguiu ler aquele valor e precisa de olho humano — "+
			"nunca trate isso como limpo.",
			strings.Join(outsideTheAllowlist, "\n"))
	}

	// A guard against a scan's worst outcome: passing GREEN without
	// having looked at anything (docs/ARMADILHAS.md, "Testes"). The
	// required files are the two PRODUCTION ones that motivated T-161
	// plus one from each scanned target, to prove every tree — including
	// the three T-185 added — was actually reached. None of them is on
	// filesExemptFromThePhoneScan on purpose: a required file that is
	// exempt would prove the walk arrived and NOT that anything was read.
	for _, required := range []string{
		"internal/config/transit.go", // production — had the real number
		"internal/meta/phone.go",     // production — had the real number
		"cmd/zapgw/log_test.go",
		"testdata/corpus/README.md",
		"docs/CONTRATO-CONSUMIDOR.md", // T-180: docs/ is swept, and no file here is exempt
		"README.md",                   // T-185: the target that is a FILE, not a directory
	} {
		if !seen[required] {
			t.Fatalf("a varredura nao alcancou %s — ela passou sem verificar o que existe para verificar (%d arquivos vistos)",
				required, len(seen))
		}
	}

	// The exception list fails closed too. A file named in
	// filesExemptFromThePhoneScan that the sweep never reached means the
	// exception is stale (file renamed, moved or deleted) — and a stale
	// exemption is an exemption nobody is deciding any more. It is the
	// "aposentar e' remover as DUAS pontas" rule applied here: the
	// exception dies with the file it exempts.
	exemptFiles := make([]string, 0, len(filesExemptFromThePhoneScan))
	for exempt := range filesExemptFromThePhoneScan {
		if !seen[exempt] {
			t.Fatalf("a excecao %s esta em filesExemptFromThePhoneScan mas a varredura nao "+
				"alcancou esse arquivo — o arquivo mudou de nome ou sumiu, e a excecao "+
				"virou letra morta. Remova-a ou corrija o caminho. Motivo registrado: %s",
				exempt, filesExemptFromThePhoneScan[exempt])
		}
		exemptFiles = append(exemptFiles, exempt)
	}

	// The exceptions are printed on EVERY run, green included. An
	// exemption that only shows up in the source is an exemption that
	// stops being read; the ones marked PROVISORIA are open debt waiting
	// on the owner, and debt that goes quiet is debt that gets shipped.
	sort.Strings(exemptFiles)
	for _, exempt := range exemptFiles {
		t.Logf("ISENTO da varredura: %s — %s", exempt, filesExemptFromThePhoneScan[exempt])
	}
}

// TestTheBase64GateIFoundAPhoneNumberInItsRealForm is T-162's POSITIVE CONTROL.
//
// 🔴 It starts from a REAL wamid from the corpus — the same one already
// used in internal/config/transit_test.go,
// `wamid.HBgNNTUzMjk5OTk5MDAwMBUCABIYFjNFQjBEO` (decodes to the synthetic
// 5532999990000, already allowlisted) — and only swaps the PHONE NUMBER
// SEGMENT, preserving the original `HBgN` prefix and metadata suffix. It's
// the distinction the 🔥 variant in docs/ARMADILHAS.md points to as the
// cause of the earlier wrong "clean": a wamid FABRICATED from scratch,
// with the base64 starting aligned at offset zero, proves the instrument
// RUNS — not that it SEES. Fabricating one here would turn this control
// into exactly the mistake T-162 exists to fix.
//
// Three sub-tests, one per T-162's Verify requirement:
//   - finds the number outside the allowlist hidden in the tampered wamid,
//     and points to file:line;
//   - with the SAME wamid without the tampering (the corpus original,
//     already synthetic), the scan comes back to ZERO findings;
//   - a corrupted wamid (one "=" out of place, in the middle of the
//     payload) doesn't decode in ANY window and fails via path (c) — a
//     finding marked "NAO DECODIFICOU", not silent absence.
func TestTheBase64GateIFoundAPhoneNumberInItsRealForm(t *testing.T) {
	const realWamidFromTheCorpus = "wamid.HBgNNTUzMjk5OTk5MDAwMBUCABIYFjNFQjBEO"

	// The three constants below are built by CONCATENATION on purpose —
	// same idiom as internal/inbound/deliver_test.go
	// (`agulha := "Insecure" + "SkipVerify"`). Written out whole, they
	// would be found by this package's OWN scan when it reads this file
	// (TestNoPhoneNumberOutsideTheAllowlistInTheRepo scans all of internal/,
	// including this file) — not because they're a real leak, but because
	// they are DELIBERATELY numbers outside the allowlist, which is
	// exactly this control's point. Concatenating avoids the
	// self-accusation without hiding the value from whoever reads the test.

	// Same prefix "HBgN" and same suffix "UCABIYFjNFQjBEO" as
	// realWamidFromTheCorpus above; only the phone number segment was swapped
	// for numberOutsideTheAllowlist, defined right below.
	tamperedWamid := "wamid." + "HBgNNTUxMTkxMjM0NTY3OAUCABIYFjNFQjBEO"
	numberOutsideTheAllowlist := "551191" + "2345678"
	// A single "=" out of place (in the middle of the phone number
	// segment) — base64 only accepts "=" as padding at the END, so no
	// window that crosses this position decodes, and the windows that
	// avoid it fall either outside the payload (too short) or into the
	// binary metadata (not readable).
	corruptedWamid := "wamid." + "HBgNNTUzMj=5OTk5MDAwMBUCABIYFjNFQjBEO"

	if syntheticPhoneAllowlist[numberOutsideTheAllowlist] {
		t.Fatalf("pre-condicao do controle quebrada: %s precisa estar FORA "+
			"da allowlist para este teste provar algo", numberOutsideTheAllowlist)
	}

	write := func(t *testing.T, literalWamid string) (root string) {
		t.Helper()
		root = t.TempDir()
		for _, sub := range []string{"cmd", "internal", "testdata"} {
			if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
				t.Fatalf("MkdirAll %s: %v", sub, err)
			}
		}
		// Line 3 is where the wamid constant lives — the tests below
		// check this in the finding's file:line.
		content := "package alvo\n\nconst wamid = \"" + literalWamid + "\"\n"
		path := filepath.Join(root, "internal", "alvo_test.go")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		return root
	}

	t.Run("acha_o_telefone_escondido_no_wamid_adulterado", func(t *testing.T) {
		root := write(t, tamperedWamid)
		hits, _, err := sweepPhoneNumbersOutsideTheAllowlist(root, []string{"cmd", "internal", "testdata"})
		if err != nil {
			t.Fatalf("varrer: %v", err)
		}
		var foundWithTheRightLine bool
		for _, a := range hits {
			if strings.Contains(a, numberOutsideTheAllowlist) && strings.HasPrefix(a, "internal/alvo_test.go:3:") {
				foundWithTheRightLine = true
			}
		}
		if !foundWithTheRightLine {
			t.Fatalf("esperava um achado citando %s em internal/alvo_test.go:3, achados: %v",
				numberOutsideTheAllowlist, hits)
		}
	})

	t.Run("wamid_original_sem_adulteracao_fica_em_zero", func(t *testing.T) {
		root := write(t, realWamidFromTheCorpus)
		hits, _, err := sweepPhoneNumbersOutsideTheAllowlist(root, []string{"cmd", "internal", "testdata"})
		if err != nil {
			t.Fatalf("varrer: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("o wamid original (telefone sintetico, ja allowlisted) "+
				"deveria dar ZERO achados; achou: %v", hits)
		}
	})

	t.Run("valor_corrompido_reprova_em_vez_de_passar_calado", func(t *testing.T) {
		root := write(t, corruptedWamid)
		hits, _, err := sweepPhoneNumbersOutsideTheAllowlist(root, []string{"cmd", "internal", "testdata"})
		if err != nil {
			t.Fatalf("varrer: %v", err)
		}
		var foundButDidNotDecode bool
		for _, a := range hits {
			if strings.Contains(a, "NAO DECODIFICOU") {
				foundButDidNotDecode = true
			}
		}
		if !foundButDidNotDecode {
			t.Fatalf("esperava um achado 'NAO DECODIFICOU' para o valor "+
				"corrompido (requisito (c) da T-162); achados: %v", hits)
		}
	})
}

// moduleRootForTheAllowlist locates the Go module's root (the directory
// with go.mod) by walking up from this test file's directory, instead of
// assuming a fixed depth that would silently break if the package moved.
// Same logic as raizDoModulo in internal/inbound/deliver_test.go,
// duplicated here because the two packages don't share a common test package.
func moduleRootForTheAllowlist() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) nao retornou o caminho deste arquivo")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod nao encontrado subindo a partir de %s", filepath.Dir(file))
		}
		dir = parent
	}
}
