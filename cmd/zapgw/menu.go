// The interactive MENU: operate the gateway without memorizing a command
// (T-082).
//
// WHY IT EXISTS, and the reason is NOT convenience: whoever doesn't
// remember the command invents SQL. This project has already paid for
// that twice — docs/IMPLANTACAO.md went as far as prescribing `UPDATE
// instancia SET ativo = 1` on the PRODUCTION database (T-071), and T-048
// was born from "Instância de laboratório exige SQL na mão". Every operation that
// only exists as a hand-typed flag is an invitation to open sqlite3.
//
// --- THIS FILE'S THREE GUARANTEES -----------------------------------------
//
// (1) IT IS NOT A SECOND PATH.
// The menu only does two things: ASK and ASSEMBLE `args`. What executes
// is `dispatch` — the SAME entry point as the command line
// (provision.go). There is no validation, confirmation, secret
// generation, or database write here; one more `if` in this file would be
// this project's mother trap ("a regra vale num lugar e não vale no
// seguinte", docs/ARMADILHAS.md) in its most expensive form: a menu that
// accepts what the command rejects.
//
// The design consequence that sustains this: A BLANK ANSWER OMITS THE
// FLAG. The menu never invents a value and never passes an empty string —
// what the operator did not type simply does not appear in `args`, and
// whoever rejects it (or falls back to the default) is the subcommand.
// See `buildArgs`.
//
// The side effect is deliberate: the menu can do LESS than the command
// line, never more. The concrete case is in `instancia rotacionar` —
// clearing the `callback_url` requires the flag TYPED with an empty
// value, and the menu has no way to express that. The way out is to state
// the command, not to open a second rule here.
//
// (2) IT DOES NOT WEAKEN ANY CONFIRMATION.
// `instancia remover` requires retyping the slug and `instancia pausar`
// requires nothing — the asymmetry is deliberate (confirming everything
// trains people to hit "yes" on autopilot, provision.go). The menu is
// precisely where confirmation turns into a reflex, so this file HAS NO
// CONFIRMATION CODE: `--confirmo` is a field like any other, the typed
// text goes VERBATIM to `dispatch`, and whoever compares it with
// `--slug` is still `removeInstance`. A `[y/N]` here would not be "a
// simpler confirmation": it would be the protection ending.
//
// (3) IT DOES NOT SHOW UP FOR A SCRIPT.
// `implanta/deploy.sh` and systemd call this binary with no one at the
// terminal; a menu waiting for input that never comes LOCKS UP the
// deploy. That is why the menu only opens when there is NO argument AND
// BOTH sides (input and output) are a terminal — see `shouldOpenMenu`. With
// no TTY, the behavior is the usual one: start the server.
//
// SECRETS ARE NOT ASKED HERE. The instance's four secrets still come from
// the environment (ZAPGW_APP_SECRET, ZAPGW_VERIFY_TOKEN, ZAPGW_SEND_TOKEN
// [old name ZAPGW_TOKEN_ENVIO — T-214], ZAPGW_DELIVERY_SECRET [old name
// ZAPGW_SEGREDO_ENTREGA]), as provision.go requires — asking on screen
// would put the value in the scrollback and in the transcript, which is
// exactly how four secrets leaked on 2026-07-28 (docs/ARMADILHAS.md). The
// menu also stores nothing to disk: there is no history, no session file,
// and what it prints is only what the subcommand printed.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
)

// dispatcher is `dispatch`'s signature. It exists as a TYPE so the test
// can capture the assembled `args` without executing anything — and to
// make it obvious, in the code itself, that the menu has ONE exit to the
// world.
type dispatcher func(args []string, out io.Writer, env environment) error

// menuField is a question that becomes ONE flag.
//
// There is no "required" field nor "validate", and the absence is
// guarantee (1): who decides what is required is the subcommand, with its
// own message.
type menuField struct {
	flag     string // how it is typed on the command line: "--slug"
	question string
	// note prints BEFORE the question. It exists for the consequence the
	// question alone does not convey — "ENTER = leave it alone" is the
	// example that matters most.
	note string
}

// menuItem is one menu line: a fixed subcommand and the questions that
// complete its arguments.
type menuItem struct {
	key   string // what the operator types to choose it
	label string
	// args is the subcommand EXACTLY as it is typed on the command line.
	// It is printed on screen before running, on purpose: whoever uses
	// the menu today learns the command for tomorrow.
	args     []string
	fields   []menuField
	warnings []string
	// writes flags the item that CHANGES something. It is only used for
	// grouping (and for the test that requires reading and destruction to
	// never be neighbors) — no execution decision depends on it.
	writes bool
}

// menuGroup separates by CONSEQUENCE, not by subject.
//
// The separation is not cosmetic: `remover` right below `listar` would
// turn a typo into an irreversible loss. That is why the irreversible one
// lives alone at the end, with a key far from the others (see
// `zapgwMenu`).
type menuGroup struct {
	title string
	items []menuItem
}

// secretsWarning is the same text in the two commands that read a
// secret from the environment. One variable, not the text written twice:
// two copies diverge the first time someone improves one of them.
const secretsWarning = "segredo NAO se digita aqui: ele vem do ambiente (ZAPGW_APP_SECRET, " +
	"ZAPGW_VERIFY_TOKEN, " + envSendTokenNew + ", " + envDeliverySecretNew + ")."

// zapgwMenu is the entire screen, as DATA.
//
// As data and not as a `switch` because the test needs to iterate item by
// item: that is how `TestMenuOnlyAsksForFlagsTheSubcommandHAS` manages to ask
// EACH real subcommand which flags it accepts, and compare them with the
// ones the menu asks.
var zapgwMenu = []menuGroup{
	{
		title: "LER — nao mudam nada",
		items: []menuItem{
			{
				key:   "1",
				label: "estado — contadores, vigia do token e certificado do callback",
				args:  []string{"estado"},
				fields: []menuField{
					{flag: "--slug", question: "slug", note: "ENTER = TODAS as instancias"},
				},
			},
			{
				key:   "2",
				label: "instancia listar — o que existe neste banco",
				args:  []string{"instancia", "listar"},
			},
			{
				key:    "3",
				label:  "instancia mostrar — uma instancia em detalhe",
				args:   []string{"instancia", "mostrar"},
				fields: []menuField{{flag: "--slug", question: "slug"}},
			},
			{
				key:   "4",
				label: "consumidor listar — quem tem acesso a que",
				args:  []string{"consumidor", "listar"},
			},
		},
	},
	{
		title: "CRIAR e TROCAR — mudam configuracao",
		items: []menuItem{
			{
				key:      "5",
				label:    "provisionar instancia — cria (ela nasce PAUSADA)",
				args:     []string{"provisionar", "instancia"},
				writes:   true,
				warnings: []string{secretsWarning, "o que faltar no ambiente e sorteado pelo comando."},
				fields: []menuField{
					{flag: "--slug", question: "slug (IMUTAVEL, vira /v1/inbound/{slug})"},
					{flag: "--waba-id", question: "waba_id"},
					{flag: "--phone-number-id", question: "phone_number_id"},
					{flag: "--numero-exibido", question: "numero exibido"},
					{flag: "--timeout-ms", question: "timeout em ms", note: "ENTER = o default do comando"},
					{flag: "--callback-url", question: "callback_url (https://)", note: "ENTER = instancia so de SAIDA"},
					{flag: "--bundle-ca", question: "arquivo PEM com a CA do consumidor", note: "ENTER = store de CAs do sistema; a verificacao continua ESTRITA nos dois casos"},
				},
			},
			{
				key:    "6",
				label:  "provisionar consumidor — cria e imprime o token UMA vez",
				args:   []string{"provisionar", "consumidor"},
				writes: true,
				fields: []menuField{
					{flag: "--nome", question: "nome do consumidor"},
					{flag: "--instancias", question: "slugs que ele pode usar, separados por virgula"},
				},
			},
			{
				key:    "7",
				label:  "instancia rotacionar — troca segredo de quem ja existe",
				args:   []string{"instancia", "rotacionar"},
				writes: true,
				warnings: []string{
					secretsWarning,
					"o que nao vier no ambiente fica INTACTO — nada e sorteado na rotacao.",
					"para APAGAR a callback_url (instancia so de saida) o menu nao serve: " +
						"e `zapgw instancia rotacionar --slug <slug> --callback-url ''` na linha de comando.",
				},
				fields: []menuField{
					{flag: "--slug", question: "slug"},
					{flag: "--callback-url", question: "nova callback_url", note: "ENTER = NAO MEXE na callback"},
				},
			},
			{
				key:    "8",
				label:  "consumidor rotacionar — token novo, o anterior morre na hora",
				args:   []string{"consumidor", "rotacionar"},
				writes: true,
				fields: []menuField{{flag: "--nome", question: "nome do consumidor"}},
			},
			{
				key:    "9",
				label:  "template criar — cadastra template na Meta",
				args:   []string{"template", "criar"},
				writes: true,
				fields: []menuField{
					{flag: "--slug", question: "slug da instancia dona do template"},
					{flag: "--nome", question: "nome do template"},
					{flag: "--categoria", question: "categoria (MARKETING, UTILITY, AUTHENTICATION)"},
					{flag: "--idioma", question: "idioma (ex.: pt_BR)"},
					{flag: "--componentes", question: "caminho do ARQUIVO com a lista JSON de componentes"},
				},
			},
			{
				key:    "10",
				label:  "fumaca — prova o canal e ATIVA a instancia",
				args:   []string{"fumaca"},
				writes: true,
				warnings: []string{
					"ele MANDA UMA MENSAGEM DE VERDADE para o numero digitado — mensagem enviada nao se desfaz.",
					"e o UNICO caminho que ativa uma instancia; falhou em qualquer passo, ela continua pausada.",
				},
				fields: []menuField{
					{flag: "--slug", question: "slug"},
					{flag: "--destino", question: "numero que vai RECEBER a mensagem, em E.164"},
				},
			},
		},
	},
	{
		title: "TIRAR DO AR — tem desfazer",
		items: []menuItem{
			{
				key:    "11",
				label:  "instancia pausar — webhook e envio respondem 503",
				args:   []string{"instancia", "pausar"},
				writes: true,
				warnings: []string{
					"NAO pede confirmacao, de proposito: pausar tem desfazer (`zapgw fumaca` religa).",
					"a Meta reenfileira o que chegar e reenvia por ate 36h — depois disso, perde.",
				},
				fields: []menuField{{flag: "--slug", question: "slug"}},
			},
		},
	},
	{
		// ALONE, and at the end. See menuGroup: the neighbor of an
		// irreversible operation is part of its protection.
		title: "IRREVERSIVEL — nao tem desfazer",
		items: []menuItem{
			{
				// The key is DISTANT from the others on purpose: a
				// neighboring number would turn a wrong finger into a
				// deleted instance.
				key:    "99",
				label:  "instancia remover — apaga a instancia e TODA linha dela",
				args:   []string{"instancia", "remover"},
				writes: true,
				warnings: []string{
					"IRREVERSIVEL. Instancia ATIVA e recusada: pause antes, confira que nada quebrou, e so entao remova.",
					"a Meta NAO fica sabendo: enquanto a Callback URL apontar para /v1/inbound/{slug}, ela recebe 404 a cada entrega.",
				},
				fields: []menuField{
					{flag: "--slug", question: "slug da instancia a APAGAR"},
					{
						flag: "--confirmo",
						// The question says what to do; WHO CHECKS IT is
						// the subcommand (removeInstance). This file
						// compares nothing — see guarantee (2) at the
						// top.
						question: "digite o SLUG DE NOVO para confirmar",
						note:     "nao existe s/N aqui: uma tecla e uma tecla, e quem esta no comando errado aperta a mesma",
					},
				},
			},
		},
	},
}

// isTerminal answers whether the file is a real terminal.
//
// The question is asked of the SYSTEM, never of an environment variable:
// `TERM` survives a `|` and a redirection, and it is exactly in the
// deploy pipe that the answer needs to be "no".
//
// THERE ARE TWO QUESTIONS, and the second is the one that can't be
// guessed: "is it a character device?" IS NOT ENOUGH, because
// `/dev/null` (and Windows' `NUL`) is ALSO one — measured on both
// platforms. And `/dev/null` is exactly what systemd delivers as standard
// input (`StandardInput=null`) and what a script writes when it doesn't
// want the output. Without the second question,
// `zapgw >/dev/null </dev/null` would open the menu, read EOF on the
// first question, and the binary would EXIT WITH 0 without starting the
// server — a silent failure, which is the worst possible outcome here.
//
// `os.SameFile` answers the second: it compares the file's identity
// (device/inode on Unix, the same pair on Windows), not the name — which
// is what makes it immune to redirection.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		// Don't know = not a terminal. The cheap error is not opening the
		// menu for someone who could have seen it; the expensive one is
		// opening it for a script.
		return false
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	nullDev, err := os.Stat(os.DevNull)
	if err != nil {
		// With no way to check the null device, the weak criterion
		// applies. This is the only branch here that errs on the side of
		// opening the menu, and it only happens on a machine where
		// /dev/null cannot be inspected.
		return true
	}
	return !os.SameFile(info, nullDev)
}

// shouldOpenMenu decides, and it is the entire guarantee (3) in four
// lines.
//
// WITH AN ARGUMENT it never opens — `implanta/deploy.sh` and every script
// call the binary this way, and a menu in place of a subcommand locks up
// the deploy waiting for input that never comes.
//
// WITH NO ARGUMENT, it requires BOTH sides to be a terminal. The output
// alone is not enough: `zapgw </dev/null` in a terminal (or inside a
// `cron` that inherited the tty) would have the output on the terminal
// and the input at EOF — the menu would open, read end-of-file on the
// first question, and the binary would EXIT without starting the server,
// which is the failure mode this function exists to not have. Requiring
// both also gives the documented way out for whoever wants the server in
// a terminal: `zapgw </dev/null`.
func shouldOpenMenu(args []string, in, out *os.File) bool {
	if len(args) > 0 {
		return false
	}
	return isTerminal(in) && isTerminal(out)
}

// menu runs the menu until the operator quits. It is the only entry
// point used by main(); `runMenu` exists separately only so the test
// can swap out the dispatcher.
func menu(in io.Reader, out io.Writer, env environment) error {
	return runMenu(in, out, env, dispatch)
}

func runMenu(in io.Reader, out io.Writer, env environment, run dispatcher) error {
	reader := bufio.NewScanner(in)

	fmt.Fprintf(out, "\nzapgw %s — menu\n", version)
	// THE SUMMARY FIRST: whoever opens the menu almost always wants to
	// know this before choosing anything.
	printSummary(out, env)

	for {
		printMenu(out)
		choice, ok := ask(reader, out, "escolha")
		if !ok {
			// EOF (Ctrl-D, or the input ended). Quitting silently is
			// correct: there was no error at all.
			fmt.Fprintln(out)
			return nil
		}
		if choice == "" {
			continue
		}
		if choice == "0" || choice == "q" {
			fmt.Fprintln(out, "ate mais.")
			return nil
		}
		item, found := findItem(choice)
		if !found {
			fmt.Fprintf(out, "nao conheco a opcao %q. Digite o numero de uma das linhas acima, ou 0 para sair.\n", choice)
			continue
		}

		fmt.Fprintf(out, "\n[%s]\n", item.label)
		for _, warning := range item.warnings {
			fmt.Fprintf(out, "  ! %s\n", warning)
		}
		args, ok := buildArgs(item, reader, out)
		if !ok {
			fmt.Fprintln(out)
			return nil
		}

		// THE EQUIVALENT COMMAND, ON SCREEN, BEFORE RUNNING. Two reasons:
		// whoever uses the menu today learns the command line for
		// tomorrow (which is the path scripts and the deploy use), and
		// the operator sees what is about to happen with what they typed.
		// No flag from this menu carries a secret — secrets come from the
		// environment —, so printing the line exposes nothing the command
		// wouldn't already expose.
		fmt.Fprintf(out, "\n> zapgw %s\n\n", commandLine(args))

		// THE ONLY EXIT TO THE WORLD. An error does not bring the menu
		// down: whoever is here almost always wants to fix it and try
		// again, and a `log.Fatal` in the middle of an incident closes
		// the tool in the face of whoever is using it.
		if err := run(args, out, env); err != nil {
			fmt.Fprintf(out, "\nERRO: %v\n", err)
		}
		fmt.Fprintln(out)
	}
}

// printSummary is the only thing this file reads from the database, and
// it only COUNTS.
//
// Why read from here instead of running `instancia listar`: the summary
// has to fit in one line (the entire table at the top of the menu is not
// a summary, and pushes the menu off the screen). Nothing here validates,
// decides, or writes — the count comes from the SAME
// `store.ListInstances` that `instancia listar` uses, and each state's
// word comes from `config.StateOf`, which is the single source of the
// vocabulary (no hand-written comparison with "ativa": the day a third
// state exists, it shows up here on its own).
//
// A FAILURE DOES NOT BLOCK THE MENU: a wrong encryption key or a missing
// database is exactly the problem someone may have opened the menu to
// fix.
func printSummary(out io.Writer, env environment) {
	store, err := openStore(env)
	if err != nil {
		fmt.Fprintf(out, "resumo indisponivel: %v\n", err)
		return
	}
	defer func() { _ = store.Close() }()

	list, err := store.ListInstances()
	if err != nil {
		fmt.Fprintf(out, "resumo indisponivel: %v\n", err)
		return
	}
	if len(list) == 0 {
		fmt.Fprintf(out, "resumo: nenhuma instancia cadastrada neste banco.\n")
		return
	}
	byState := map[string]int{}
	for _, r := range list {
		byState[config.StateOf(r)]++
	}
	states := make([]string, 0, len(byState))
	for e := range byState {
		states = append(states, e)
	}
	sort.Strings(states)
	var parts []string
	for _, e := range states {
		parts = append(parts, fmt.Sprintf("%d %s", byState[e], e))
	}
	fmt.Fprintf(out, "resumo: %d instancia(s) — %s\n", len(list), strings.Join(parts, ", "))
}

func printMenu(out io.Writer) {
	for _, g := range zapgwMenu {
		fmt.Fprintf(out, "\n%s\n", g.title)
		for _, i := range g.items {
			fmt.Fprintf(out, " %3s) %s\n", i.key, i.label)
		}
	}
	fmt.Fprintf(out, "\n   0) sair\n\n")
}

func findItem(key string) (menuItem, bool) {
	for _, g := range zapgwMenu {
		for _, i := range g.items {
			if i.key == key {
				return i, true
			}
		}
	}
	return menuItem{}, false
}

// buildArgs asks the item's fields and returns the command line.
//
// A BLANK ANSWER OMITS THE FLAG — and this is the rule that keeps the
// menu from becoming a second path:
//
//   - what is required is still rejected by the SUBCOMMAND, with its own
//     message (the menu does not know, and should not know, what is
//     required);
//   - what has a default still picks up the subcommand's default, a
//     single place;
//   - and the case that costs the most: in `instancia rotacionar`, the
//     TYPED flag is what distinguishes "clear the callback" from "leave
//     it alone" (fs.Visit, see provision.go). Passing `--callback-url
//     ""` just because the operator hit ENTER would silently wipe out
//     delivery for an instance that is receiving traffic.
//
// The value goes VERBATIM, and there is no risk of it turning into
// another flag: the `flag` package consumes the next argument as the
// VALUE of a string flag, even if it starts with `-`.
func buildArgs(item menuItem, reader *bufio.Scanner, out io.Writer) ([]string, bool) {
	args := append([]string(nil), item.args...)
	for _, c := range item.fields {
		if c.note != "" {
			fmt.Fprintf(out, "  (%s)\n", c.note)
		}
		value, ok := ask(reader, out, c.question)
		if !ok {
			return nil, false
		}
		if value == "" {
			continue
		}
		args = append(args, c.flag, value)
	}
	return args, true
}

// ask reads one line. `false` is EOF — never "blank answer": the
// two things are different and confusing them would make the menu run a
// command on its own upon reaching the end of a redirected input.
func ask(reader *bufio.Scanner, out io.Writer, question string) (string, bool) {
	fmt.Fprintf(out, "%s: ", question)
	if !reader.Scan() {
		return "", false
	}
	return strings.TrimSpace(reader.Text()), true
}

// commandLine assembles what shows up on screen before running. A
// value with a space prints inside quotes so the printed line can be
// COPIED and work — a line that only looks copyable is worse than none.
func commandLine(args []string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"") {
			parts = append(parts, fmt.Sprintf("%q", a))
			continue
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
