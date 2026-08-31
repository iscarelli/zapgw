package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// --- The menu is NOT a second path -----------------------------------------
//
// The four tests in this section attack the same question from different
// angles, on purpose: it is the project's mother trap
// (docs/ARMADILHAS.md), and a menu that accepts what the command rejects
// is its most expensive form.
//
//	(1) TestMenuBUILDSTheSubcommandArgs       — what the menu produces is `args`, only;
//	(2) TestMenuOnlyAsksForFlagsTheSubcommandHAS   — asked of the REAL SUBCOMMAND;
//	(3) TestMenuDoesNOTValidateDoesNOTConfirmDoesNOTWrite  — asks menu.go's CODE;
//	(4) TestMenuDoesEXACTLYWhatTheCommandLineDoes — same output, same database.

// answers assembles the operator's input: one line per Enter.
func answers(lines ...string) io.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

// menuWithSpy runs the menu with a FAKE dispatcher that only stores
// the `args`. Nothing is executed — that is the point: it proves the
// menu's exit to the world is a command line, and only that.
func menuWithSpy(t *testing.T, in io.Reader) ([][]string, string) {
	t.Helper()
	var captured [][]string
	var out bytes.Buffer
	spy := func(args []string, _ io.Writer, _ environment) error {
		captured = append(captured, append([]string(nil), args...))
		return nil
	}
	if err := runMenu(in, &out, fakeEnvironment(testEnvironment(t)), spy); err != nil {
		t.Fatalf("runMenu: %v", err)
	}
	return captured, out.String()
}

func TestMenuBUILDSTheSubcommandArgs(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		expected []string
	}{
		{
			name:     "estado sem slug: a flag NAO vai, e quem decide 'todas' e o subcomando",
			in:       []string{"1", ""},
			expected: []string{"estado"},
		},
		{
			name:     "estado com slug",
			in:       []string{"1", "lojinha"},
			expected: []string{"estado", "--slug", "lojinha"},
		},
		{
			name:     "instancia listar nao pergunta nada",
			in:       []string{"2"},
			expected: []string{"instancia", "listar"},
		},
		{
			name:     "instancia mostrar",
			in:       []string{"3", "lojinha"},
			expected: []string{"instancia", "mostrar", "--slug", "lojinha"},
		},
		{
			name:     "consumidor listar",
			in:       []string{"4"},
			expected: []string{"consumidor", "listar"},
		},
		{
			name: "provisionar instancia: o que ficou em branco nao vira flag",
			in: []string{"5", "lojinha", "WABA1", "PNID1", "5532999990000",
				"", "https://consumidor.interno/hook", ""},
			expected: []string{"provisionar", "instancia",
				"--slug", "lojinha", "--waba-id", "WABA1", "--phone-number-id", "PNID1",
				"--numero-exibido", "5532999990000", "--callback-url", "https://consumidor.interno/hook"},
		},
		{
			name:     "provisionar consumidor",
			in:       []string{"6", "consumer-a", "lojinha,outra"},
			expected: []string{"provisionar", "consumidor", "--nome", "consumer-a", "--instancias", "lojinha,outra"},
		},
		{
			name:     "rotacionar instancia sem callback: a flag NAO vai (ver o teste dedicado abaixo)",
			in:       []string{"7", "lojinha", ""},
			expected: []string{"instancia", "rotacionar", "--slug", "lojinha"},
		},
		{
			name:     "consumidor rotacionar",
			in:       []string{"8", "consumer-a"},
			expected: []string{"consumidor", "rotacionar", "--nome", "consumer-a"},
		},
		{
			name: "template criar",
			in:   []string{"9", "lojinha", "boas_vindas", "UTILITY", "pt_BR", "comp.json"},
			expected: []string{"template", "criar", "--slug", "lojinha", "--nome", "boas_vindas",
				"--categoria", "UTILITY", "--idioma", "pt_BR", "--componentes", "comp.json"},
		},
		{
			name:     "fumaca",
			in:       []string{"10", "lojinha", "5532999990000"},
			expected: []string{"fumaca", "--slug", "lojinha", "--destino", "5532999990000"},
		},
		{
			name:     "pausar",
			in:       []string{"11", "lojinha"},
			expected: []string{"instancia", "pausar", "--slug", "lojinha"},
		},
		{
			name:     "remover: o slug redigitado vai VERBATIM em --confirmo",
			in:       []string{"99", "lojinha", "lojinha"},
			expected: []string{"instancia", "remover", "--slug", "lojinha", "--confirmo", "lojinha"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			captured, _ := menuWithSpy(t, answers(append(c.in, "0")...))
			if len(captured) != 1 {
				t.Fatalf("o menu despachou %d comando(s), quero exatamente 1: %v", len(captured), captured)
			}
			if got := strings.Join(captured[0], " "); got != strings.Join(c.expected, " ") {
				t.Fatalf("args = %q\nquero    = %q", got, strings.Join(c.expected, " "))
			}
		})
	}
}

// The flag the menu asks for has to EXIST on the subcommand — and who
// answers which ones exist is the subcommand itself, via `-h` (parseFlags()
// returns the help without opening the database or touching the
// network).
//
// This is the test that stops the menu from inventing surface: a flag
// only the menu knows about is a second path starting.
func TestMenuOnlyAsksForFlagsTheSubcommandHAS(t *testing.T) {
	env := fakeEnvironment(testEnvironment(t))
	for _, g := range zapgwMenu {
		for _, i := range g.items {
			var help bytes.Buffer
			if err := dispatch(append(append([]string(nil), i.args...), "-h"), &help, env); err != nil {
				t.Fatalf("%s: `zapgw %s -h`: %v", i.key, strings.Join(i.args, " "), err)
			}
			text := help.String()
			if strings.Contains(text, "desconhecido") {
				t.Fatalf("%s: `zapgw %s` nao e um subcomando de verdade: %s", i.key, strings.Join(i.args, " "), text)
			}
			for _, c := range i.fields {
				name := strings.TrimLeft(c.flag, "-")
				found := regexp.MustCompile(`(?m)^\s*-` + regexp.QuoteMeta(name) + `\b`).MatchString(text)
				if !found {
					t.Errorf("item %s (%s) pergunta %s, que `zapgw %s` NAO tem.\najuda do subcomando:\n%s",
						i.key, i.label, c.flag, strings.Join(i.args, " "), text)
				}
			}
		}
	}
}

// Asks the CODE: menu.go cannot call a command, a validator, or a write.
//
// The two tests above prove today's BEHAVIOR; this one blocks tomorrow's
// deviation — the day someone "quickly fixes" a special case by calling
// `removeInstance` directly, or checking the slug here. The forbidden
// list is explicit on purpose (the same decision as T-048: the guard
// exists so the decision gets MADE, not to make it on its own).
func TestMenuDoesNOTValidateDoesNOTConfirmDoesNOTWrite(t *testing.T) {
	forbiddenNames := map[string]string{
		// Running a subcommand WITHOUT going through dispatch would be the fork.
		"provision":         "o menu monta args e delega; chamar o subcomando direto pula dispatch",
		"provisionInstance": "idem",
		"provisionConsumer": "idem",
		"instanceCommand":   "idem",
		"consumerCommand":   "idem",
		"stateCommand":      "idem",
		"templateCommand":   "idem",
		"listInstances":     "idem",
		"showInstance":      "idem",
		"rotateInstance":    "idem",
		"rotateConsumer":    "idem",
		"listConsumers":     "idem",
		"pauseInstance":     "idem",
		"removeInstance":    "idem",
		"templateCreate":    "idem",
		"smoke":             "idem",
		// Validation belongs to the subcommand/store, never here.
		"ValidateSlug":        "validacao duplicada diverge; o menu nao valida",
		"ValidateCallbackURL": "idem",
		"ValidateCABundle":    "idem",
		// Secrets and writes do not pass through here.
		"randomSecret":     "o menu nao sorteia segredo",
		"CreateInstance":   "o menu nao grava",
		"RemoveInstance":   "o menu nao grava",
		"PauseInstance":    "o menu nao grava",
		"ActivateInstance": "o menu nao grava",
		"RotateInstance":   "o menu nao grava",
		"CreateConsumer":   "o menu nao grava",
		"RotateConsumer":   "o menu nao grava",
	}

	file := filepath.Join(".", "menu.go")
	fs := token.NewFileSet()
	tree, err := parser.ParseFile(fs, file, nil, 0)
	if err != nil {
		t.Fatalf("parse de %s: %v", file, err)
	}

	ast.Inspect(tree, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch f := call.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
			// The ONLY database read allowed here is the summary count.
			// Any other method on `store` is the menu turning into a
			// second path to the data.
			if x, ok := f.X.(*ast.Ident); ok && x.Name == "store" {
				if f.Sel.Name != "ListInstances" && f.Sel.Name != "Close" {
					t.Errorf("%s chama store.%s — o menu so pode CONTAR (ListInstances) e fechar",
						fs.Position(call.Pos()), f.Sel.Name)
				}
			}
		default:
			return true
		}
		if reason, forbidden := forbiddenNames[name]; forbidden {
			t.Errorf("%s chama %s: %s", fs.Position(call.Pos()), name, reason)
		}
		return true
	})
}

// The test that closes the section: the menu's effect and the command
// line's effect, BYTE FOR BYTE in the output and row for row in the
// database.
//
// It runs the real menu (`menu`, the same one main() calls, with the real
// `dispatch`) against one database, and the equivalent command against
// another. Secrets come from the environment on both so nothing is
// randomly generated — with random generation the output would have
// different values by construction and the comparison would say nothing.
func TestMenuDoesEXACTLYWhatTheCommandLineDoes(t *testing.T) {
	withSecrets := func() map[string]string {
		vars := testEnvironment(t)
		vars["ZAPGW_BANCO"] = filepath.Join(t.TempDir(), "zapgw.db")
		vars["ZAPGW_APP_SECRET"] = "app-secret-de-teste"
		vars["ZAPGW_VERIFY_TOKEN"] = "verify-token-de-teste"
		vars["ZAPGW_TOKEN_ENVIO"] = "token-envio-de-teste"
		vars["ZAPGW_SEGREDO_ENTREGA"] = "segredo-entrega-de-teste"
		return vars
	}

	viaCommandLine := withSecrets()
	viaMenu := withSecrets()

	// The two creations below are TWO real calls in sequence — without
	// freezing the clock, the second could tick over between them, and
	// carimbos_desde/token_definido_em (both come from the CLOCK at the
	// moment of creation, internal/config/store.go CreateInstanceAt) would
	// diverge over a field that proves NOTHING about "the menu does the
	// same as the command line" (docs/ARMADILHAS.md, "Relógio e
	// carimbo"; T-100). Freezing creationClock (provision.go) for
	// both calls is what makes both timestamps come out IDENTICAL, and it
	// returns the comparison to the ENTIRE struct — with no field
	// exception at all, not for today's fields nor for the next timestamp
	// someone adds.
	previous := creationClock
	frozen := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	creationClock = func() time.Time { return frozen }
	t.Cleanup(func() { creationClock = previous })

	args := []string{"provisionar", "instancia",
		"--slug", "lojinha", "--waba-id", "WABA1", "--phone-number-id", "PNID1",
		"--numero-exibido", "5532999990000", "--callback-url", "https://consumidor.interno/hook"}

	var commandLineOut bytes.Buffer
	if err := dispatch(args, &commandLineOut, fakeEnvironment(viaCommandLine)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	var menuOut bytes.Buffer
	in := answers("5", "lojinha", "WABA1", "PNID1", "5532999990000", "",
		"https://consumidor.interno/hook", "", "0")
	if err := menu(in, &menuOut, fakeEnvironment(viaMenu)); err != nil {
		t.Fatalf("menu: %v", err)
	}

	// The subcommand's output comes out WHOLE and INTACT inside the
	// menu's screen: the menu does not rewrite, does not summarize, and
	// does not add any line to it.
	if !strings.Contains(menuOut.String(), commandLineOut.String()) {
		t.Fatalf("a saida do menu nao contem, palavra por palavra, a do comando.\nmenu:\n%s\ncomando:\n%s",
			menuOut.String(), commandLineOut.String())
	}

	// AND THE DATABASE: both paths recorded the same instance.
	fromCommandLine, err := storeFromEnvironment(t, viaCommandLine).SummarizeInstance("lojinha")
	if err != nil {
		t.Fatalf("SummarizeInstance (linha de comando): %v", err)
	}
	fromMenu, err := storeFromEnvironment(t, viaMenu).SummarizeInstance("lojinha")
	if err != nil {
		t.Fatalf("SummarizeInstance (menu): %v", err)
	}

	// No field is zeroed before this comparison: creationClock was
	// frozen ABOVE for both calls, so carimbos_desde and token_definido_em
	// are born IDENTICAL on both paths, and the ENTIRE struct is compared
	// with no exception — including the next timestamp someone adds
	// (T-100; the previous version of this test zeroed StampsSince
	// field by field, and T-098 reopened the same flaw with
	// TokenSetAt in under 24h — see docs/ARMADILHAS.md, "Relógio e
	// carimbo").
	if fmt.Sprintf("%+v", fromCommandLine) != fmt.Sprintf("%+v", fromMenu) {
		t.Fatalf("a instancia criada pelo menu difere da criada pela linha de comando:\nlinha: %+v\nmenu:  %+v",
			fromCommandLine, fromMenu)
	}
}

// --- The confirmations remain what they were -------------------------------

// The assertion is "the menu REQUIRES THE RETYPED SLUG", not "the menu
// asks for confirmation": a `[y/N]` would pass the second one, and that
// is exactly the end of the protection.
//
// MANDATORY MUTATION (done and reverted): making the menu accept `y` —
// sending the slug itself in `--confirmo` when the answer is "y" — leaves
// this test red in the first half, with the instance deleted.
func TestMenuRemoveREQUIRESTheSlugRetyped(t *testing.T) {
	vars := testEnvironment(t)
	if err := dispatch(instanceArgs("lojinha"), io.Discard, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provisionar: %v", err)
	}

	// (a) "y" is NOT confirmation.
	var out bytes.Buffer
	if err := menu(answers("99", "lojinha", "s", "0"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("menu: %v", err)
	}
	if _, err := storeFromEnvironment(t, vars).FindInstance("lojinha"); err != nil {
		t.Fatalf("a instancia foi APAGADA respondendo \"s\" — a confirmacao virou uma tecla: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "--confirmo") {
		t.Fatalf("a recusa nao aponta o --confirmo (quem recusa e o subcomando, e a mensagem dele tem de chegar):\n%s", out.String())
	}

	// (b) the retyped slug, that does remove — otherwise the test above
	// would pass with a menu that simply removes nothing.
	var second bytes.Buffer
	if err := menu(answers("99", "lojinha", "lojinha", "0"), &second, fakeEnvironment(vars)); err != nil {
		t.Fatalf("menu: %v", err)
	}
	if _, err := storeFromEnvironment(t, vars).FindInstance("lojinha"); err == nil {
		t.Fatalf("a instancia continua no banco depois de o slug ser redigitado:\n%s", second.String())
	}
}

// The other side of the asymmetry: `pausar` does NOT ask for
// confirmation, and that is deliberate (pausing has an undo; confirming
// everything trains people to hit "yes" on autopilot). A menu that
// "protects" pausar ruins removing's confirmation.
func TestMenuPauseDoesNOTAskForConfirmation(t *testing.T) {
	item, found := findItem("11")
	if !found {
		t.Fatal("o item de pausar sumiu do menu")
	}
	if len(item.fields) != 1 || item.fields[0].flag != "--slug" {
		t.Fatalf("pausar pergunta %+v — a unica pergunta e o slug", item.fields)
	}

	vars := testEnvironment(t)
	if err := dispatch(instanceArgs("lojinha"), io.Discard, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provisionar: %v", err)
	}
	store := storeFromEnvironment(t, vars)
	if err := store.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}

	// ONE answer (the slug) and the instance goes down.
	var out bytes.Buffer
	if err := menu(answers("11", "lojinha", "0"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("menu: %v", err)
	}
	r, err := store.SummarizeInstance("lojinha")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if config.StateOf(r) != "pausada" {
		t.Fatalf("estado = %q, quero pausada — o menu pediu algo a mais?\n%s", config.StateOf(r), out.String())
	}
}

// --- Non-interactive mode must not break -----------------------------------

// MANDATORY MUTATION (done and reverted): making the menu show up with no
// TTY (`shouldOpenMenu` returning true just because there is no argument)
// leaves this test red in three cases, and the one below — the real
// binary — right along with it.
func TestWithoutTTYDoesNotOpenMenu(t *testing.T) {
	regular, err := os.Create(filepath.Join(t.TempDir(), "saida.log"))
	if err != nil {
		t.Fatalf("criar arquivo: %v", err)
	}
	defer func() { _ = regular.Close() }()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = read.Close(); _ = write.Close() }()

	nullDev, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("abrir %s: %v", os.DevNull, err)
	}
	defer func() { _ = nullDev.Close() }()

	cases := []struct {
		name    string
		args    []string
		in, out *os.File
	}{
		// The implanta/deploy.sh case: called with an argument. Never a
		// menu, not even when both sides are a terminal.
		{"com argumento nunca abre", []string{"provisionar", "instancia"}, os.Stdin, os.Stdout},
		{"saida em arquivo", nil, read, regular},
		{"saida em pipe", nil, read, write},
		// What systemd delivers: StandardInput=null. `/dev/null` IS a
		// character device — if `isTerminal` stopped at ModeCharDevice,
		// this case would open the menu, read EOF, and the binary would
		// exit with 0 without starting the server.
		{"entrada no dispositivo nulo", nil, nullDev, regular},
		{"os dois no dispositivo nulo", nil, nullDev, nullDev},
	}
	for _, c := range cases {
		if shouldOpenMenu(c.args, c.in, c.out) {
			t.Errorf("%s: shouldOpenMenu = true, quero false", c.name)
		}
	}

	if isTerminal(nullDev) {
		t.Errorf("isTerminal(%s) = true — o dispositivo nulo nao e terminal de ninguem", os.DevNull)
	}
}

// The proof in the REAL BINARY, which is the one `implanta/deploy.sh`
// depends on: with no argument and no terminal, the process starts the
// SERVER, as it always has. If the menu showed up here, it would read EOF
// and the process would die without ever answering /v1/health — which is
// exactly how a deploy locks up/rolls back.
func TestScriptWithoutTTYStillBringsUpTheServer(t *testing.T) {
	bin := buildWithVersion(t, "menu-sem-tty")
	body := startServerAndGetHealth(t, bin)
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("/v1/health respondeu %s", body)
	}
}

// --- Secrets, and what the menu CANNOT add ------------------------------

// Whoever passes the value through the environment still sees nothing
// (T-052) — and the menu asks for no secret at all, so it has no way to
// put one on screen.
func TestMenuDoesNotPrintASecretTheCommandDoesNotPrint(t *testing.T) {
	vars := testEnvironment(t)
	secrets := map[string]string{
		"ZAPGW_APP_SECRET":      "app-secret-de-teste",
		"ZAPGW_VERIFY_TOKEN":    "verify-token-de-teste",
		"ZAPGW_TOKEN_ENVIO":     "token-envio-de-teste",
		"ZAPGW_SEGREDO_ENTREGA": "segredo-entrega-de-teste",
	}
	for k, v := range secrets {
		vars[k] = v
	}

	var out bytes.Buffer
	in := answers("5", "lojinha", "WABA1", "PNID1", "5532999990000", "", "", "", "0")
	if err := menu(in, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("menu: %v", err)
	}
	for k, v := range secrets {
		if strings.Contains(out.String(), v) {
			t.Errorf("o valor de %s apareceu na tela do menu:\n%s", k, out.String())
		}
	}

	// And no menu field asks for a secret: the list of asked flags is
	// checked against the environment variables that carry a secret.
	for _, g := range zapgwMenu {
		for _, i := range g.items {
			for _, c := range i.fields {
				name := strings.ToLower(strings.TrimLeft(c.flag, "-"))
				for _, forbidden := range []string{"secret", "token", "segredo", "senha"} {
					if strings.Contains(name, forbidden) {
						t.Errorf("item %s pergunta %s — segredo vem do ambiente, nunca da tela", i.key, c.flag)
					}
				}
			}
		}
	}
}

// ENTER on `--callback-url` has to OMIT the flag. Passing the flag empty
// would wipe out delivery for an instance that is receiving traffic — the
// TYPED flag is what distinguishes "clear it" from "leave it alone"
// (fs.Visit, provision.go).
func TestMenuDoesNotClearCallbackWhenTheOperatorJustPressesEnter(t *testing.T) {
	vars := testEnvironment(t)
	if err := dispatch(instanceArgs("lojinha"), io.Discard, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provisionar: %v", err)
	}
	vars["ZAPGW_APP_SECRET"] = "outro-app-secret"

	var out bytes.Buffer
	if err := menu(answers("7", "lojinha", "", "0"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("menu: %v", err)
	}
	i, err := storeFromEnvironment(t, vars).FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.CallbackURL == "" {
		t.Fatalf("a callback_url foi APAGADA por um ENTER:\n%s", out.String())
	}
	if i.AppSecret != "outro-app-secret" {
		t.Fatalf("app_secret = %q — a rotacao pedida nao aconteceu", i.AppSecret)
	}
}

// --- The screen: summary on open, and destructive far from reading -------------------

func TestMenuShowsStateSummaryOnOpening(t *testing.T) {
	vars := testEnvironment(t)
	for _, slug := range []string{"lojinha", "outra"} {
		if err := dispatch(instanceArgs(slug), io.Discard, fakeEnvironment(vars)); err != nil {
			t.Fatalf("provisionar %s: %v", slug, err)
		}
	}
	if err := storeFromEnvironment(t, vars).ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}

	var out bytes.Buffer
	if err := menu(answers("0"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("menu: %v", err)
	}
	for _, snippet := range []string{"resumo: 2 instancia(s)", "1 ativa", "1 pausada"} {
		if !strings.Contains(out.String(), snippet) {
			t.Errorf("o resumo nao traz %q:\n%s", snippet, out.String())
		}
	}
}

// A database that won't even open is one of the reasons someone opens
// the menu. It cannot die because of the summary.
func TestMenuOpensEvenWithTheDatabaseUnreachable(t *testing.T) {
	var out bytes.Buffer
	// With no ZAPGW_CHAVE_CIFRA at all: openStore rejects it.
	if err := menu(answers("0"), &out, fakeEnvironment(map[string]string{})); err != nil {
		t.Fatalf("menu: %v", err)
	}
	if !strings.Contains(out.String(), "resumo indisponivel") {
		t.Fatalf("o menu nao explicou por que nao ha resumo:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "instancia listar") {
		t.Fatalf("o menu nao chegou a ser mostrado:\n%s", out.String())
	}
}

// An irreversible operation cannot be a neighbor of a harmless one —
// neither in the group, nor in the number the finger types.
func TestMenuSeparatesTheIrreversibleFromTheRead(t *testing.T) {
	seen := map[string]bool{}
	var irreversibles []menuItem
	for _, g := range zapgwMenu {
		var reads, writesSeen int
		for _, i := range g.items {
			if seen[i.key] {
				t.Errorf("a chave %q aparece duas vezes no menu", i.key)
			}
			seen[i.key] = true
			if i.writes {
				writesSeen++
			} else {
				reads++
			}
		}
		if reads > 0 && writesSeen > 0 {
			t.Errorf("o grupo %q mistura leitura e escrita — `remover` logo abaixo de `listar` e o que esta secao existe para impedir", g.title)
		}
		if strings.Contains(g.title, "IRREVERSIVEL") {
			irreversibles = append(irreversibles, g.items...)
		}
	}
	if len(irreversibles) != 1 || irreversibles[0].args[len(irreversibles[0].args)-1] != "remover" {
		t.Fatalf("o grupo irreversivel tem de ter exatamente o `instancia remover`: %+v", irreversibles)
	}

	// And its key cannot be a neighbor of any other: an adjacent number
	// turns a wrong finger into a deleted instance.
	target, err := strconv.Atoi(irreversibles[0].key)
	if err != nil {
		t.Fatalf("chave %q nao e numero: %v", irreversibles[0].key, err)
	}
	for key := range seen {
		if key == irreversibles[0].key {
			continue
		}
		n, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		if n >= target-1 && n <= target+1 {
			t.Errorf("a chave %q e vizinha da chave de `instancia remover` (%q)", key, irreversibles[0].key)
		}
	}
}
