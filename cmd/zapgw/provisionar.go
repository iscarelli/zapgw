// Subcommands that WRITE configuration: `zapgw provisionar instancia`,
// `zapgw provisionar consumidor` and `zapgw instancia rotacionar` (which
// swaps an existing instance's secrets, without recreating it — the slug
// is immutable).
//
// WHY THIS EXISTS INSTEAD OF A RAW INSERT: the instance's six sensitive
// fields are ENCRYPTED at rest (internal/config/crypto.go). Writing
// directly to SQLite records plain text where the gateway expects
// encrypted data, and the error does not show up at startup — it shows
// up at send time, in production, with the customer on the other end.
//
// SECRETS DO NOT COME THROUGH A FLAG. A flag shows up in any machine
// user's `ps` and stays in the shell history; an environment variable
// does not. Whichever are missing are generated here, and the command
// says WHICH ones it generated — never the value, with TWO named
// exceptions: a randomly generated `verify_token` and `segredo_entrega`
// are printed, because they are SHARED and without them the instance is
// born impossible to finish setting up. The whole reason is in
// `printSharedSecrets` (T-052).
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
	"github.com/iscarelli/zapgw/internal/outbound"
)

// environment is the environment-variable read, injectable for testing. In
// production it is always os.Getenv.
type environment func(name string) string

// dispatch runs the subcommand requested in `args` (which is
// os.Args[1:]).
//
// An UNKNOWN subcommand is an error, never "start the server anyway": a
// typo in `zapgw provisonar` that started the gateway would leave whoever
// typed it thinking they had provisioned something. With NO argument at
// all the binary starts the server — or opens the menu, if there is a
// terminal on both sides (menu.go, T-082) —, and that is decided in
// main(), not here.
func dispatch(args []string, out io.Writer, env environment) error {
	if len(args) == 0 {
		return errors.New("zapgw: falta o subcomando (provisionar | fumaca | diagnostico | instancia | consumidor | estado | template | transito | log | perdidas | versao)")
	}
	switch args[0] {
	case "provisionar":
		return provision(args[1:], out, env)
	case "fumaca":
		return smoke(args[1:], out, env)
	case "diagnostico":
		// T-109: READ-ONLY — does not send, does not activate, does not
		// write to the database. See diagnostico.go for why it is not
		// the same path as fumaca.
		return diagnose(args[1:], out, env)
	case "instancia":
		return instanceCommand(args[1:], out, env)
	case "consumidor":
		// T-055: rotating a consumer's token and seeing who has access
		// to what. Until this task, revoking a consumer's token required
		// a DELETE by hand in the production SQLite — and the gap only
		// showed up during a leak incident.
		return consumerCommand(args[1:], out, env)
	case "estado":
		// T-035: state (active/paused) + per-instance counters, with the
		// permanent-loss alarm highlighted. See estado.go.
		return stateCommand(args[1:], out, env)
	case "template":
		// T-036: registering a template through the local store, without
		// requiring a consumer token or assembling JSON on the command
		// line. See template.go.
		return templateCommand(args[1:], out, env)
	case "transito":
		// T-091: "did this message pass through here?" without storing
		// the phone number in plain text nor the content. See
		// transito.go.
		return transitCommand(args[1:], out, env)
	case "perdidas":
		// The failover post-mortem: what the node that fell had and
		// never replicated. Read-only, and it deliberately does NOT use
		// OpenStore — see internal/config/forense.go,
		// openReadOnly.
		return lostCommand(args[1:], out, env)
	case "log":
		// T-093: the last 200 transit rows, then it keeps showing what
		// arrives, until you quit with Ctrl-C. See log.go.
		return logCommand(args[1:], out, env)
	case "versao":
		// Does not use `env`: the version comes from the BUILD
		// (-ldflags), never from the environment nor the database. See
		// the `versao` var in main.go.
		return versionCommand(out)
	case "menu":
		// T-089 (the owner's decision, 2026-07-29, reaffirming T-082):
		// "menu" is NOT a subcommand and never will be. This `case`
		// exists ONLY to TEACH the error — it does NOT call menu(...)
		// and does NOT go into the "conheco" list of the `default` right
		// below, because it is not a real subcommand.
		//
		// WHY THERE IS NO SECOND PATH TO THE SCREEN: an invocable name
		// can be put in a script and lock up waiting for input -- and it
		// is the ONLY way to punch through the TTY guard
		// (shouldOpenMenu, menu.go), which would stop being structural
		// (guaranteed by impossibility) and would start depending on
		// diligence (someone remembering not to call `zapgw menu` in a
		// script). An earlier version of this task created the
		// subcommand with that guard working and it was REJECTED anyway
		// -- see docs/IMPLANTACAO.md, "O menu" section, and T-082's
		// changelog entry (2026-07-28 20:56). Do not reopen this.
		return fmt.Errorf("zapgw: nao existe subcomando \"menu\" -- a tela interativa abre com zapgw" +
			" sem argumento nenhum, num terminal. nao existe subcomando \"menu\" porque um nome" +
			" invocavel poderia ser posto num script e travar esperando entrada, furando a guarda" +
			" que so deixa o menu abrir sem argumento e com terminal dos dois lados")
	default:
		return fmt.Errorf("zapgw: subcomando desconhecido %q (conheco: provisionar, fumaca, diagnostico, instancia, consumidor, estado, template, transito, log, versao)", args[0])
	}
}

func instanceCommand(args []string, out io.Writer, env environment) error {
	if len(args) == 0 {
		return errors.New("zapgw: instancia o que? (listar | mostrar | rotacionar | reabrir-cadastro | pausar | remover | registrar | desregistrar | pin)")
	}
	switch args[0] {
	case "listar":
		return listInstances(args[1:], out, env)
	case "mostrar":
		return showInstance(args[1:], out, env)
	case "rotacionar":
		return rotateInstance(args[1:], out, env)
	case "reabrir-cadastro":
		// T-079: giving the consumer back the right to write their own
		// configuration. Without this command, a consumer stuck with the
		// wrong credential = an UPDATE by hand in the production SQLite.
		return reopenEnrollment(args[1:], out, env)
	case "pausar":
		// T-048: bringing it down without deleting it. It is the
		// mandatory step before removing, and the only one of the two
		// that has an undo.
		return pauseInstance(args[1:], out, env)
	case "remover":
		return removeInstance(args[1:], out, env)
	case "registrar":
		// T-151: turns on the number's two-step verification with Meta.
		// Provisioning only — see the header of
		// internal/meta/registro.go.
		return registerInstance(args[1:], out, env)
	case "desregistrar":
		// T-151: takes the number OFF the air in production, at Meta.
		// The same confirmation pattern as `remover`.
		return deregisterInstance(args[1:], out, env)
	case "pin":
		// T-151: swaps the two-step verification PIN of a number ALREADY
		// registered.
		return changeInstancePin(args[1:], out, env)
	default:
		return fmt.Errorf("zapgw: nao sei fazer %q com uma instancia (conheco: listar, mostrar, rotacionar, reabrir-cadastro, pausar, remover, registrar, desregistrar, pin)", args[0])
	}
}

// consumerCommand is `instanceCommand`'s pair for this gateway's OTHER
// credential: the token a consumer system uses to call /v1/messages.
//
// WHY IT EXISTS (T-055): until 2026-07-28 the CLI only knew how to
// CREATE a consumer. On the day a consumer token leaked, the two possible
// ways out were creating a second consumer while leaving the exposed
// token valid, or deleting the row by hand with sqlite3 inside the
// production CT — which is what happened, under time pressure, and
// sqlite3 wasn't even installed there.
func consumerCommand(args []string, out io.Writer, env environment) error {
	if len(args) == 0 {
		return errors.New("zapgw: consumidor o que? (listar | rotacionar)")
	}
	switch args[0] {
	case "listar":
		return listConsumers(args[1:], out, env)
	case "rotacionar":
		return rotateConsumer(args[1:], out, env)
	default:
		return fmt.Errorf("zapgw: nao sei fazer %q com um consumidor (conheco: listar, rotacionar)", args[0])
	}
}

// --- Reading: seeing the state without opening SQLite by hand ---------------------
//
// NOTHING IS DECRYPTED in these two commands. Of the six encrypted
// fields, only whether they are recorded comes out, and the question is
// answered against the recorded column (config.Store.ListInstances) —
// decrypting only to throw it away would create a window with the secret
// in plain text in a read command's memory, with no use for it at all.
//
// The side effect is what matters most during an incident: both keep
// working with the WRONG encryption key, which is exactly when seeing the
// system's state matters most.

// stateOf is the same word across EVERY surface of this gateway: "ativa"
// and "pausada" are what whoever operates it looks for, and two spellings
// would make a `grep pausada` lie.
//
// The word lives in config.StateOf since T-060, no longer here: the GET
// /v1/estado route (internal/outbound/estado_handler.go) publishes the
// SAME state to the consumer, and a copy of the function there would
// start lying the day either one gained a third state.
func stateOf(r config.InstanceSummary) string {
	return config.StateOf(r)
}

// presence writes the six fields as `name=sim|nao`.
//
// The name=value pair, and not a bare "sim" column, because this output
// is read by people in a hurry and by `grep`: `instancia listar | grep
// callback_url=nao` answers "who doesn't deliver?" without counting
// columns. And every token of the same field has the same width, so the
// rows stay aligned.
func presence(r config.InstanceSummary) []string {
	var parts []string
	for _, c := range r.Encrypted {
		value := "nao"
		if c.Registered {
			value = "sim"
		}
		parts = append(parts, c.Name+"="+value)
	}
	return parts
}

// windowAsText describes, for whoever OPERATES it, the window in which
// the consumer can register their Meta credentials (T-079).
//
// THE VERDICT COMES FROM THE SAME FUNCTION THAT DECIDES (config.WindowFrom
// / Janela.Open), never from a computation written here: two
// computations would diverge, and the symptom would be this screen saying
// "open" about a window that is rejecting writes — the worst possible
// outcome, because it sends the owner to look for the defect in the
// consumer.
//
// AND IT SAYS THE ENTIRE COMMAND when it is closed: whoever reads this
// line is helping a stuck consumer, and "reopen the window" with no
// command next to it is exactly how the answer turns into SQL by hand
// (what T-048 existed to kill).
func windowAsText(r config.InstanceSummary, now time.Time) string {
	j := config.WindowFrom(r.RegisteredAt)
	if j.OpenedAt.IsZero() {
		return "ABERTA — o consumidor ainda nao cadastrou nada;" +
			" as " + config.RegistrationWindow.String() + " comecam na PRIMEIRA insercao dele, nao agora"
	}
	if j.IsOpen(now) {
		return "ABERTA ate " + j.ClosesAt.UTC().Format(time.RFC3339) +
			" (primeira insercao do consumidor em " + j.OpenedAt.UTC().Format(time.RFC3339) + ")"
	}
	return "FECHADA desde " + j.ClosesAt.UTC().Format(time.RFC3339) +
		" — o consumidor recebe 409 ao cadastrar. Para reabrir:" +
		"  zapgw instancia reabrir-cadastro --slug " + r.Slug + " --confirmo " + r.Slug
}

// absenceNote explains the TWO fields whose emptiness is a legitimate
// state, and not a gap. Without the note, a "nao" column on screen looks
// like a defect and someone is going to "fix" an instance that is
// correct.
var absenceNote = map[string]string{
	"callback_url": "instancia so de SAIDA: o gateway nao entrega a ninguem o que a Meta mandar aqui",
	"bundle_ca":    "sem ancora propria: a entrega usa a store de CAs do sistema, e a verificacao continua ESTRITA",
	// The two below entered in T-079, and their note matters more than
	// the other two: since the instance started being born with ONLY THE
	// SLUG, `nao` here is the NORMAL state of a freshly created
	// instance — the consumer hasn't registered yet. Without this line,
	// the owner reads "app_secret=nao" as a defect and goes fix an
	// instance that is correct (or, worse, types in the consumer's
	// credential, which he does not have and should not have).
	"app_secret":  "o CONSUMIDOR ainda nao cadastrou a Meta dele (POST /v1/cadastro) — normal em instancia recem-criada",
	"token_envio": "o CONSUMIDOR ainda nao cadastrou a Meta dele (POST /v1/cadastro) — normal em instancia recem-criada",
}

func listInstances(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia listar", flag.ContinueOnError)
	fs.SetOutput(out)
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	list, err := store.ListInstances()
	if err != nil {
		return fmt.Errorf("zapgw: listar instancias: %w", err)
	}
	if len(list) == 0 {
		// An empty output cannot be told apart from "the command didn't
		// run", and on a freshly created database that doubt costs a
		// pointless investigation.
		fmt.Fprintf(out, "nenhuma instancia cadastrada neste banco.\n")
		return nil
	}

	// tabwriter because the aligned column is what makes the list
	// readable at a glance — and "at a glance" is the only way this
	// output will ever be read.
	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	// T-103: TIPO goes right next to ESTADO, not at the end — without
	// it, an instagram instance shows up with
	// NUMERO/PHONE_NUMBER_ID/WABA_ID empty and nothing in the row
	// explains why; whoever reads it goes looking for missing
	// configuration where nothing is missing.
	fmt.Fprintf(tab, "SLUG\tESTADO\tTIPO\tNUMERO\tPHONE_NUMBER_ID\tWABA_ID\tTIMEOUT\tCADASTRADO? (nada e decifrado, valor nenhum e mostrado)\n")
	for _, r := range list {
		fmt.Fprintf(tab, "%s\t%s\t%s\t%s\t%s\t%s\t%dms\t%s\n",
			r.Slug, stateOf(r), r.Type, r.DisplayNumber, r.PhoneNumberID, r.WabaID, r.TimeoutMs,
			strings.Join(presence(r), " "))
	}
	return tab.Flush()
}

func showInstance(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia mostrar", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a mostrar")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio (use `zapgw instancia listar` para ver os slugs)")
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	r, err := store.SummarizeInstance(who)
	if err != nil {
		return fmt.Errorf("zapgw: mostrar a instancia %q: %w", who, err)
	}

	// T-103: on WhatsApp the three fields below (numero_exibido/phone_number_id/
	// waba_id) are the real identifiers; on Instagram they NEVER exist
	// (ValidateInstanceType rejects registration if they arrive
	// filled in, see internal/config/store.go) — showing them empty
	// next to filled-in fields sends whoever operates it looking for
	// missing configuration where nothing is missing, the same disease
	// T-099 already fixed in GET /v1/estado. Marked with
	// outbound.NotApplicable (the SAME vocabulary), not omitted: omitting
	// them would change the table's SHAPE depending on the type, and
	// whoever greps a row by name (`grep waba_id`) would stop finding it
	// in half the instances.
	displayedNumber, phoneNumberID, wabaID := r.DisplayNumber, r.PhoneNumberID, r.WabaID
	if r.Type == config.TypeInstagram {
		displayedNumber, phoneNumberID, wabaID = outbound.NotApplicable, outbound.NotApplicable, outbound.NotApplicable
	}

	lines := [][2]string{
		{"slug", r.Slug},
		{"estado", stateOf(r)},
		{"tipo", r.Type},
		{"numero_exibido", displayedNumber},
		{"phone_number_id", phoneNumberID},
		{"waba_id", wabaID},
	}
	if r.Type == config.TypeInstagram {
		// ig_id is an IDENTIFIER, not a secret (the same class as
		// waba_id and phone_number_id) — prints the VALUE. Until this
		// task there was no screen at all that confirmed the value
		// recorded by `instancia rotacionar --ig-id` (T-102): fixing the
		// field with no way to check the fix took.
		lines = append(lines, [2]string{"ig_id", r.IgID})
	}
	lines = append(lines,
		[2]string{"timeout_ms", fmt.Sprintf("%d", r.TimeoutMs)},
		[2]string{"janela_de_cadastro", windowAsText(r, time.Now())},
	)

	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, line := range lines {
		fmt.Fprintf(tab, "%s:\t%s\n", line[0], line[1])
	}
	if err := tab.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(out, "campos cifrados — cadastrado? (nada e decifrado, e valor nenhum e mostrado):\n")
	fields := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	pairs := presence(r)
	for n, c := range r.Encrypted {
		// With no note, the row ends at the pair: a "\t%s" with an empty
		// note would leave trailing space on almost every row, and
		// invisible trailing space gets in the way of whoever copies the
		// output or compares it with yesterday's.
		if text, has := absenceNote[c.Name]; has && !c.Registered {
			fmt.Fprintf(fields, "  %s\t— %s\n", pairs[n], text)
			continue
		}
		fmt.Fprintf(fields, "  %s\n", pairs[n])
	}
	return fields.Flush()
}

// rotateInstance swaps the secrets of an instance that ALREADY
// EXISTS.
//
// WHY IT EXISTS: until T-017, an existing instance's `app_secret`,
// `verify_token`, `token_envio`, `segredo_entrega` and `callback_url` had
// no write path at all — and spec §7.3 described the app_secret's
// rotation as if it were executable. A doc promising a nonexistent
// mechanism is an error this project has already paid for.
//
// SECRETS COME THROUGH THE ENVIRONMENT, through the SAME variables as
// provisioning (a flag shows up in `ps` and in the shell history).
// Whatever doesn't arrive stays INTACT: PARTIAL rotation — swapping only
// the app_secret — is the normal case, and silently zeroing the rest
// would wipe out the token_envio of an instance that is sending right
// now.
//
// AND NOTHING IS RANDOMLY GENERATED HERE. In provisioning, generating a
// missing secret is a convenience; in rotation it would mean swapping a
// secret in use for a value no one knows.
func rotateInstance(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia rotacionar", flag.ContinueOnError)
	fs.SetOutput(out)
	// There is NO rename flag, and the absence is the guarantee: the
	// slug becomes /v1/inbound/{slug} and is already pasted into Meta's
	// panel. It says WHICH instance to swap, never what to swap it into.
	slug := fs.String("slug", "", "instancia cujos segredos serao trocados. O slug e IMUTAVEL: ele diz QUAL instancia, nunca vira um valor novo")
	callbackURL := fs.String("callback-url", "", "nova callback_url; https:// obrigatorio. So e trocada se a flag VIER; passe vazia para apagar (instancia so de saida)")
	// T-102: ig_id is an IDENTIFIER, not a secret (the same class as
	// waba_id and phone_number_id) — that is why it is a FLAG, like in
	// `provisionar instancia`, and not an environment variable. Only
	// applies to a --tipo instagram instance: the write is rejected with
	// an error on a whatsapp instance (see the validation right below,
	// which REUSES config.ValidateInstanceType).
	igID := fs.String("ig-id", "", "novo ig_id (Instagram-scoped Business Account ID) da instancia; identificador, nao e segredo. So se aplica a instancia --tipo instagram — recusado com erro numa instancia whatsapp")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio")
	}

	// The SAME order and the SAME variables as provisioning: two name
	// lists would diverge, and the symptom would be a secret that
	// "doesn't swap" with nothing flagging it.
	var r config.Rotation
	secrets := []struct {
		variable    string
		destination **string
	}{
		{"ZAPGW_APP_SECRET", &r.AppSecret},
		{"ZAPGW_VERIFY_TOKEN", &r.VerifyToken},
		{"ZAPGW_TOKEN_ENVIO", &r.SendToken},
		{"ZAPGW_SEGREDO_ENTREGA", &r.DeliverySecret},
	}
	var swapped []string
	for _, s := range secrets {
		value := strings.TrimSpace(env(s.variable))
		if value == "" {
			continue // DO NOT TOUCH
		}
		*s.destination = &value
		swapped = append(swapped, s.variable)
	}

	// The TYPED flag is what distinguishes "clear the callback" from
	// "leave it alone": --callback-url's default value is empty, and
	// treating that as a swap request would make every app_secret
	// rotation silently wipe out delivery. fs.Visit only sees the flags
	// that appeared on the command line.
	fs.Visit(func(f *flag.Flag) {
		if f.Name != "callback-url" {
			return
		}
		newOne := strings.TrimSpace(*callbackURL)
		r.CallbackURL = &newOne
		swapped = append(swapped, "--callback-url")
	})

	// ig_id has NO legitimate value to "clear" (unlike callback_url,
	// which can become an outbound-only instance): an instagram instance
	// with no ig_id is impossible to route, and a whatsapp instance
	// should never receive the flag. That is why the criterion is the
	// SAME as the four secrets above — a non-empty value after TrimSpace
	// — and not callback_url's fs.Visit.
	if value := strings.TrimSpace(*igID); value != "" {
		r.IgID = &value
		swapped = append(swapped, "--ig-id")
	}

	if len(swapped) == 0 {
		return errors.New("zapgw: nada para trocar — defina ZAPGW_APP_SECRET, ZAPGW_VERIFY_TOKEN," +
			" ZAPGW_TOKEN_ENVIO ou ZAPGW_SEGREDO_ENTREGA no ambiente, e/ou passe --callback-url ou --ig-id." +
			" Rotacionar nada e imprimir sucesso deixaria voce achando que o segredo real ja esta no gateway")
	}
	// The SAME function the store calls — not a second rule (two rules
	// diverge; a function called from two places doesn't). Here it only
	// moves the rejection to before opening the database, with the
	// message pointing at the flag.
	if r.CallbackURL != nil {
		if err := config.ValidateCallbackURL(*r.CallbackURL); err != nil {
			return fmt.Errorf("zapgw: --callback-url: %w", err)
		}
	}
	// Validating --ig-id against the instance's TYPE (whatsapp doesn't
	// have this field) CANNOT happen here: it depends on the type
	// ALREADY RECORDED in the database, which this command hasn't opened
	// yet. store.RotateInstance does that check, reusing
	// config.ValidateInstanceType, before writing any column.

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if err := store.RotateInstance(who, r); err != nil {
		return fmt.Errorf("zapgw: rotacionar a instancia %q: %w", who, err)
	}

	// SAY WHICH ONES, never the values — neither the new one nor the
	// old one. The old one is still valid at Meta until the swap over
	// there, so printing it leaks a LIVE secret.
	fmt.Fprintf(out, "instancia %q: trocado(s) %s (o valor NAO e mostrado).\n",
		who, strings.Join(swapped, ", "))
	fmt.Fprintf(out, "o que nao esta nessa lista ficou INTACTO.\n")
	// THE ORDER IS THE GUARANTEE: reversed, every delivery between the
	// two steps fails verification — no loss (Meta re-queues for 36h),
	// but with an alarm and pointless noise.
	fmt.Fprintf(out, "agora atualize a Meta, e so DEPOIS do gateway — nunca antes.\n")
	if r.VerifyToken != nil {
		// The warning spec §7.4 requires ON SCREEN: the verify_token
		// trap is sneaky because it is only used on the verification
		// GET.
		fmt.Fprintf(out, "atencao ao verify_token: trocar aqui NAO quebra trafego nenhum hoje — ele so e usado no GET de verificacao."+
			" A recusa aparece semanas depois, na primeira vez que alguem re-salvar a URL de callback no painel da Meta com o valor antigo.\n")
	}
	return nil
}

// --- T-048: pausing and removing -------------------------------------------------
//
// The two operations that only used to exist as SQL typed by hand into
// the PRODUCTION database. The cost was never typing SQL: it was WHERE
// it was typed — a `DELETE ... WHERE slug = '…'` with the wrong slug, at
// 6pm, deletes a real consumer's instance, and the database holds the
// six encrypted fields no one has memorized. The CLI exists so the risky
// operation has a NAME, CONFIRMATION, and a TEST.
//
// THERE IS NO `instancia ativar`, and the absence is a decision, not an
// oversight. T-048 left `--sem-prova` out because the decision wasn't
// its to make; T-071 made the decision and it was to NOT OPEN the second
// door: `config.ActivateInstance` remains the only path to `ativo = 1`,
// and `zapgw fumaca` remains the only one that calls it. A lab instance
// activates through that SAME fumaca, with the Graph API pointed at the
// fake in cmd/grafo-falso/ (ZAPGW_GRAPH_BASE) — the requirement of a
// successful send stays whole. The full reasoning is in
// cmd/zapgw/fumaca.go and the recipe in docs/IMPLANTACAO.md.

// pauseInstance brings the instance down without deleting anything.
//
// NO CONFIRMATION, on purpose, and the asymmetry with `remover` is the
// information: pausing has an undo (`zapgw fumaca` turns it back on),
// removing has none. Asking for confirmation on both would train whoever
// operates it to type "yes" on autopilot, and the confirmation that
// matters would lose its effect.
func pauseInstance(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia pausar", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a tirar do ar")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio (use `zapgw instancia listar` para ver os slugs)")
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if err := store.PauseInstance(who); err != nil {
		return fmt.Errorf("zapgw: pausar a instancia %q: %w", who, err)
	}

	fmt.Fprintf(out, "instancia %q PAUSADA: o webhook responde 503 e o envio tambem.\n", who)
	// The consequence that decides whether pausing was the right thing
	// to do: 503 is not 200, so Meta RE-QUEUES and re-sends for up to
	// 36h. A short pause loses no event; a long pause does, with no
	// warning.
	fmt.Fprintf(out, "a Meta reenfileira o que chegar (503 nao e 200) e reenvia por ate 36h — depois disso, perde.\n")
	fmt.Fprintf(out, "para religar:  zapgw fumaca --slug %s --destino <numero em E.164>\n", who)
	return nil
}

// removeInstance deletes the instance and EVERY row that belongs to
// it.
//
// THE CONFIRMATION IS RETYPING THE SLUG, never a `-y`: a `-y` is one
// key, and whoever is on the wrong command hits the same key. Retyping
// the slug forces the person to look at the name of what they are
// deleting — and the flag still has to MATCH `--slug`, otherwise the
// rejection comes before any write.
func removeInstance(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia remover", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a apagar. IRREVERSIVEL")
	confirm := fs.String("confirmo", "", "digite o slug DE NOVO para confirmar. Nao existe -y: apagar a instancia errada nao tem desfazer")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio (use `zapgw instancia listar` para ver os slugs)")
	}
	if strings.TrimSpace(*confirm) != who {
		return fmt.Errorf("zapgw: remover a instancia %q e IRREVERSIVEL — repita o slug em --confirmo:"+
			"  zapgw instancia remover --slug %s --confirmo %s", who, who, who)
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// WHO LOSES ACCESS, read BEFORE deleting: after the commit the link
	// no longer exists and the question goes unanswered. A consumer who
	// loses the link starts receiving 403, and this is the only moment
	// where it's possible to warn them before they find out on their
	// own.
	var orphans []string
	if consumers, err := store.ListConsumers(); err == nil {
		for _, c := range consumers {
			for _, s := range c.Instances {
				if s == who {
					orphans = append(orphans, c.Name)
				}
			}
		}
	}

	deleted, err := store.RemoveInstance(who)
	if err != nil {
		if errors.Is(err, config.ErrInstanceActive) {
			return fmt.Errorf("zapgw: a instancia %q esta ATIVA e nada foi apagado."+
				" Pause primeiro (`zapgw instancia pausar --slug %s`), confira que nada quebrou, e so entao remova —"+
				" pausar tem desfazer, remover nao: %w", who, who, err)
		}
		return fmt.Errorf("zapgw: remover a instancia %q: %w", who, err)
	}

	fmt.Fprintf(out, "instancia %q REMOVIDA (irreversivel). Linhas apagadas, numa transacao so:\n", who)
	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, a := range deleted {
		fmt.Fprintf(tab, "  %s\t%d\n", a.Table, a.Rows)
	}
	if err := tab.Flush(); err != nil {
		return err
	}
	if len(orphans) > 0 {
		fmt.Fprintf(out, "PERDERAM ACESSO a esta instancia: %s — as chamadas deles para ela passam a receber 403.\n",
			strings.Join(orphans, ", "))
	}
	// What the gateway CANNOT delete, and therefore has to say: the
	// subscription on Meta's side. As long as it exists, traffic keeps
	// arriving at /v1/inbound/{slug} and the gateway answers 404
	// (internal/inbound/handler.go), with one journal line per batch —
	// and no one reads the journal out of habit.
	fmt.Fprintf(out, "a Meta NAO sabe disso: enquanto a Callback URL apontar para /v1/inbound/%s, ela recebe 404 a cada entrega.\n", who)
	fmt.Fprintf(out, "tire a inscricao no painel da Meta, ou reaponte a Callback URL.\n")
	return nil
}

// --- T-151: number registration and two-step PIN -------------------------
//
// PROVISIONING ONLY (the owner's decision, 2026-08-20: "provisioning
// only"): the three operations below touch a number that is ALREADY
// LIVE at Meta and are not consumer surface — see the header of
// internal/meta/registro.go for the source and for the warning that the
// PIN operation is ONE-WAY: there is no way to turn off two-step
// verification through the API, only through the WhatsApp Manager.
//
// 🔴 THE PIN IS A SECRET AND DOES NOT COME THROUGH A FLAG. A hard rule
// of this house, with five leaks in its history (see CLAUDE.md, the
// secrets section): a command-line argument is read by any local process
// in `ps` and shows up in every configuration dump. THERE IS NO `--pin`
// HERE, on purpose — see TestNoSubcommandHasAFlagCalledPin in
// provisionar_test.go, which goes red if that flag is ever created by
// mistake. The value comes in through an environment variable (ZAPGW_PIN,
// read by the SAME `env` the rest of the file already uses) or through a
// file pointed at by `--pin-file`, which carries the PATH, never the
// value — the same decision as `--bundle-ca` in provisionInstance,
// above.

// readPin returns the PIN from `--pin-file` (when the flag came in) or
// from ZAPGW_PIN — in that order, because the flag is what the person
// just typed and an old environment variable should not win over a
// choice made right now.
//
// THE FILE IS READ HERE, and the content is not kept: like the PEM from
// --bundle-ca, the gateway cannot depend on a file continuing to exist
// (and with the same content) after this call.
func readPin(env environment, file string) (string, error) {
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("zapgw: --pin-arquivo: ler %s: %w", file, err)
		}
		pin := strings.TrimSpace(string(raw))
		if pin == "" {
			return "", fmt.Errorf("zapgw: --pin-arquivo: %s esta vazio", file)
		}
		return pin, nil
	}
	pin := strings.TrimSpace(env("ZAPGW_PIN"))
	if pin == "" {
		return "", errors.New("zapgw: o pin e obrigatorio — defina ZAPGW_PIN no ambiente ou passe --pin-arquivo" +
			" com o CAMINHO de um arquivo que contenha o pin (NUNCA --pin: argumento de linha de comando aparece" +
			" no `ps` de qualquer processo local e em todo dump de configuracao)")
	}
	return pin, nil
}

// instanceForTalkingToMeta opens the store, looks up `who`, and builds
// the Graph API client — the SAME sequence registerInstance,
// deregisterInstance, and changeInstancePin all need before calling
// Meta. Returns the store ALREADY CLOSED: none of the three functions
// write to the database (whoever records TokenSetAt and the rest is
// the owner's rotation, not this command), so there is no reason to hold
// the connection open during the network call.
func instanceForTalkingToMeta(env environment, who string) (config.Instance, *meta.Client, error) {
	store, err := openStore(env)
	if err != nil {
		return config.Instance{}, nil, err
	}
	defer func() { _ = store.Close() }()

	inst, err := store.FindInstance(who)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			return config.Instance{}, nil, fmt.Errorf("zapgw: instancia %q nao existe (use `zapgw instancia listar` para ver os slugs): %w", who, err)
		}
		return config.Instance{}, nil, fmt.Errorf("zapgw: buscar instancia %q: %w", who, err)
	}
	return inst, meta.NewClient(&http.Client{}, graphBase(env)), nil
}

// registerInstance turns on the number's two-step verification with
// Meta.
//
// The PIN is never printed — not the value, not a prefix, not even the
// length — in the output nor in any error: see the header of
// internal/meta/registro.go, sendRegistration, which guarantees the same on
// the HTTP client side.
func registerInstance(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia registrar", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a registrar na Meta (liga a verificacao em duas etapas). OBRIGATORIO")
	pinFile := fs.String("pin-arquivo", "", "arquivo com o pin de 6 digitos (o CAMINHO, nunca o valor)."+
		" Alternativa: variavel de ambiente ZAPGW_PIN. NAO existe --pin")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio (use `zapgw instancia listar` para ver os slugs)")
	}
	pin, err := readPin(env, strings.TrimSpace(*pinFile))
	if err != nil {
		return err
	}

	inst, client, err := instanceForTalkingToMeta(env, who)
	if err != nil {
		return err
	}

	if err := client.Register(context.Background(), inst.PhoneNumberID, inst.SendToken, pin); err != nil {
		return fmt.Errorf("zapgw: registrar %q na Meta: %w", who, err)
	}

	fmt.Fprintf(out, "instancia %q REGISTRADA na Meta — verificacao em duas etapas ligada com o pin fornecido.\n", who)
	fmt.Fprintln(out, "isto e' DE MAO UNICA: nao ha endpoint da Cloud API para DESATIVAR a verificacao em duas")
	fmt.Fprintln(out, "etapas — so o WhatsApp Manager, fora desta API, consegue.")
	return nil
}

// deregisterInstance takes the number OFF the air in production, at
// Meta: after it, no message goes in or out through this phone_number_id
// until a new `instancia record`.
//
// THE SAME PATTERN AS `instancia remover`: `--confirmo <slug>`, the slug
// retyped, no `-y` — and the check happens BEFORE opening the store or
// touching the network, so typing the command with no `--confirmo` fails
// fast and with no effect at all.
func deregisterInstance(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia desregistrar", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a tirar do ar NA META. OBRIGATORIO")
	confirm := fs.String("confirmo", "", "digite o slug DE NOVO para confirmar. Nao existe -y")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio (use `zapgw instancia listar` para ver os slugs)")
	}
	if strings.TrimSpace(*confirm) != who {
		return fmt.Errorf("zapgw: desregistrar %q TIRA O NUMERO DE PRODUCAO DO AR na Meta — nenhuma mensagem"+
			" entra nem sai por este phone_number_id ate um novo `zapgw instancia registrar`. Repita o slug em"+
			" --confirmo:  zapgw instancia desregistrar --slug %s --confirmo %s", who, who, who)
	}

	inst, client, err := instanceForTalkingToMeta(env, who)
	if err != nil {
		return err
	}

	if err := client.Deregister(context.Background(), inst.PhoneNumberID, inst.SendToken); err != nil {
		return fmt.Errorf("zapgw: desregistrar %q na Meta: %w", who, err)
	}

	fmt.Fprintf(out, "instancia %q DESREGISTRADA na Meta — o numero SAIU DO AR: nenhuma mensagem entra nem sai"+
		" por ele agora.\n", who)
	fmt.Fprintf(out, "isto NAO apagou nada aqui no gateway: a linha continua no banco. Para religar, registre"+
		" de novo com um pin:  zapgw instancia registrar --slug %s\n", who)
	return nil
}

// changeInstancePin swaps the two-step verification PIN of an instance
// that is ALREADY REGISTERED. Does NOT turn verification on or off — only
// swaps the key; see `registerInstance` to turn it on the first time,
// and this section's header for why the reverse does NOT EXIST.
func changeInstancePin(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia pin", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia cujo pin de verificacao em duas etapas sera trocado. OBRIGATORIO")
	pinFile := fs.String("pin-arquivo", "", "arquivo com o NOVO pin de 6 digitos (o CAMINHO, nunca o valor)."+
		" Alternativa: variavel de ambiente ZAPGW_PIN. NAO existe --pin")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio (use `zapgw instancia listar` para ver os slugs)")
	}
	pin, err := readPin(env, strings.TrimSpace(*pinFile))
	if err != nil {
		return err
	}

	inst, client, err := instanceForTalkingToMeta(env, who)
	if err != nil {
		return err
	}

	if err := client.SetPin(context.Background(), inst.PhoneNumberID, inst.SendToken, pin); err != nil {
		return fmt.Errorf("zapgw: trocar o pin de %q na Meta: %w", who, err)
	}

	fmt.Fprintf(out, "instancia %q: pin de verificacao em duas etapas TROCADO na Meta (o valor NAO e mostrado).\n", who)
	return nil
}

func provision(args []string, out io.Writer, env environment) error {
	if len(args) == 0 {
		return errors.New("zapgw: provisionar o que? (instancia | consumidor)")
	}
	switch args[0] {
	case "instancia":
		return provisionInstance(args[1:], out, env)
	case "consumidor":
		return provisionConsumer(args[1:], out, env)
	default:
		return fmt.Errorf("zapgw: nao sei provisionar %q (conheco: instancia, consumidor)", args[0])
	}
}

// creationClock is the instant provisionInstance passes to
// config.CreateInstanceAt — the SAME pattern as
// internal/outbound/estado.go (printClock): a package var with
// time.Now as the default, which the test swaps for a fixed instant and
// restores with t.Cleanup.
//
// WHY IT EXISTS (T-100): `dispatch` and `menu` are the SAME entry point
// (menu.go calls dispatch internally), so
// TestMenuDoesEXACTLYWhatTheCommandLineDoes invokes them in SEQUENCE —
// two real calls, each capturing the clock at its own instant. Before
// this variable, CreateInstance called time.Now() internally in each
// one, and a clock tick between the two made `carimbos_desde` (T-092) and
// later `token_definido_em` (T-098) diverge by a second — a struct
// equality test becoming flaky on every NEW timestamp field, not just
// those two.
//
// THE FIX IS NOT ZEROING FIELD BY FIELD (that has already failed twice:
// T-092 zeroed StampsSince, and T-098 reopened the same flaw with
// TokenSetAt in under 24h — it is this project's mother trap, a
// hand-written list on top of the schema). The fix is having the test
// inject ONE fixed clock for BOTH calls: with the same instant, both
// timestamps are born IDENTICAL on both paths, and comparing the ENTIRE
// struct works again with no exception at all — not for today's two
// fields, not for the next timestamp someone adds.
var creationClock = time.Now

// randomSecret returns 32 bytes in hex.
//
// crypto/rand, never math/rand: a predictable app_secret lets anyone
// forge the signature inbound checks.
func randomSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("zapgw: sortear segredo: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// parseFlags runs fs.Parse and says whether the command should CONTINUE.
//
// `-h` returns (false, nil): asking for help is not a failure — exiting
// != 0 because of it would make a deploy script abort by mistake —, but
// it also does not authorize the command to run with flags that didn't
// come in.
func parseFlags(fs *flag.FlagSet, args []string) (keepGoing bool, err error) {
	err = fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("zapgw: %s: %w", fs.Name(), err)
	}
	return true, nil
}

// provisionInstance creates the instance. SINCE T-079, `--slug` is the
// ONLY required flag.
//
// WHY (the owner's decision, docs/MODELO-DE-USO.md): `waba_id`,
// `phone_number_id`, the number, and the App's secrets are the
// CONSUMER's data — the owner does not have them, does not keep them,
// and should not broker them. Requiring them here forced a five-value
// conversation with someone he cannot reach. The owner supplies the SLUG
// (which is his because it is immutable and becomes a URL path — "if
// not, users go create abominations"), delivers the minimal package, and
// the consumer registers their own Meta credentials via `POST
// /v1/cadastro`.
//
// THE FLAGS STILL EXIST, and remain the path for a LAB instance
// (docs/IMPLANTACAO.md) and for any instance whose Meta account belongs
// to the owner himself: whoever HAS the values doesn't need a second HTTP
// call to type them in. What changed is that their absence stopped being
// an error.
//
// T-074'S REQUIREMENT DID NOT DISAPPEAR, it moved: `waba_id` and
// `phone_number_id` remain required — at REGISTRATION time
// (config.ValidateMetaRegistration). What T-074 prevented was an ACTIVE and
// incomplete instance; that stays impossible, because only `zapgw fumaca`
// activates one and it requires a message that actually went out.
func provisionInstance(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("provisionar instancia", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "identificador da instancia; vira /v1/inbound/{slug}. IMUTAVEL. minusculas, digitos e hifen, 3 a 40. E a UNICA flag obrigatoria")
	kind := fs.String("tipo", config.TypeWhatsApp, "\"whatsapp\" (default) ou \"instagram\" (T-097). Instancia Instagram NAO usa --waba-id/--phone-number-id/--numero-exibido — usa --ig-id, e exige-o JA NESTA CRIACAO: esta fatia nao tem cadastro por API para Instagram")
	igID := fs.String("ig-id", "", "Instagram-scoped Business Account ID (identificador, nao e segredo). OBRIGATORIO quando --tipo=instagram; nao se aplica a --tipo=whatsapp")
	wabaID := fs.String("waba-id", "", "WABA ID da conta na Meta (identificador, nao e segredo). So em --tipo=whatsapp. OPCIONAL: se a conta Meta for do CONSUMIDOR, deixe vazio — ele cadastra por POST /v1/cadastro")
	phoneNumberID := fs.String("phone-number-id", "", "Phone Number ID do numero na Meta (identificador, nao e segredo). So em --tipo=whatsapp. OPCIONAL, pelo mesmo motivo do --waba-id")
	displayedNumber := fs.String("numero-exibido", "", "o numero como ele aparece para o cliente. So em --tipo=whatsapp. OPCIONAL, pelo mesmo motivo do --waba-id")
	timeoutMs := fs.Int("timeout-ms", 5000, "prazo de cada chamada a Graph API, em ms")
	callbackURL := fs.String("callback-url", "", "para onde o gateway entrega o que a Meta mandar; https:// obrigatorio, vazio = instancia so de saida")
	caBundle := fs.String("bundle-ca", "", "arquivo PEM com a CA do consumidor, quando ele nao usa CA publica; vazio = store de CAs do sistema. NAO desliga a verificacao: troca a ancora de confianca, so para esta instancia")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	// The PATH comes through a flag, the CONTENT does not: an entire
	// PEM in a flag would show up in `ps` and in the shell history. And
	// the file is read HERE, and not kept as a path, because the gateway
	// cannot depend on a file continuing to exist (and with the same
	// content) months later, on the right machine.
	var caPEM string
	if path := strings.TrimSpace(*caBundle); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("zapgw: --bundle-ca: ler %s: %w", path, err)
		}
		caPEM = string(raw)
		// An EMPTY file does not become "no bundle": whoever passed the
		// flag meant to register an anchor, and silently falling back to
		// the system store would deliver exactly to the consumer the
		// custom CA exists to cover — with the failure only showing up
		// on the first delivery. A truncated copy is the common way this
		// happens.
		if caPEM == "" {
			return fmt.Errorf("zapgw: --bundle-ca: %s esta vazio; para usar a store de CAs do sistema, OMITA a flag: %w",
				path, config.ErrInvalidCABundle)
		}
	}

	inst := config.Instance{
		Slug:          strings.TrimSpace(*slug),
		Type:          strings.TrimSpace(*kind),
		IgID:          strings.TrimSpace(*igID),
		WabaID:        strings.TrimSpace(*wabaID),
		PhoneNumberID: strings.TrimSpace(*phoneNumberID),
		DisplayNumber: strings.TrimSpace(*displayedNumber),
		CallbackURL:   strings.TrimSpace(*callbackURL),
		CABundle:      caPEM,
		TimeoutMs:     *timeoutMs,
	}
	// Trims BEFORE deciding: presence is not content, and a --slug of
	// only spaces would become a /v1/inbound/  route that Meta would
	// never hit correctly.
	//
	// A LIST OF ONE, and it remains a list on purpose: the other three
	// flags left this list in T-079 because their values belong to the
	// CONSUMER (see the header), not because they stopped being
	// necessary. They started being required at registration time
	// instead.
	mandatory := []struct {
		flag  string
		value string
	}{
		{"--slug", inst.Slug},
	}
	for _, o := range mandatory {
		if o.value == "" {
			return fmt.Errorf("zapgw: %s e obrigatorio", o.flag)
		}
	}
	// BOTH OR NEITHER. Half identification is the one state that serves
	// no purpose: with waba_id and no phone_number_id the instance does
	// not send, and with phone_number_id and no waba_id it rejects every
	// account webhook with an ALARM. Whoever typed one of the two has
	// both on screen — and having neither is the normal case, where the
	// consumer registers both at once (or the instance is Instagram,
	// which never uses either — see ValidateInstanceType right below).
	if (inst.WabaID == "") != (inst.PhoneNumberID == "") {
		return errors.New("zapgw: --waba-id e --phone-number-id andam JUNTOS: passe os dois, ou nenhum" +
			" (nenhum = a conta Meta e do consumidor, e ele cadastra por POST /v1/cadastro)")
	}
	if inst.TimeoutMs <= 0 {
		return errors.New("zapgw: --timeout-ms tem de ser maior que zero")
	}
	// THE SAME functions CreateInstance calls — not a second rule (two
	// rules diverge; a function called from two places doesn't). Calling
	// them here only moves the rejection to BEFORE generating a secret
	// and opening the database, and leaves the message pointing at the
	// flag the person typed.
	if err := config.ValidateSlug(inst.Slug); err != nil {
		return fmt.Errorf("zapgw: --slug: %w", err)
	}
	if err := config.ValidateCallbackURL(inst.CallbackURL); err != nil {
		return fmt.Errorf("zapgw: --callback-url: %w", err)
	}
	if err := config.ValidateCABundle(inst.CABundle); err != nil {
		return fmt.Errorf("zapgw: --bundle-ca: %w", err)
	}
	// T-097: the SAME function CreateInstance calls to normalize/check
	// the type — moves the rejection of an Instagram instance with
	// --waba-id (or with no --ig-id) to BEFORE generating a secret and
	// opening the database.
	normalizedType, err := config.ValidateInstanceType(
		inst.Type, inst.WabaID, inst.PhoneNumberID, inst.DisplayNumber, inst.IgID)
	if err != nil {
		return fmt.Errorf("zapgw: --tipo/--ig-id: %w", err)
	}
	inst.Type = normalizedType

	// T-114, item 2: --tipo instagram with NO ZAPGW_APP_SECRET or NO
	// ZAPGW_TOKEN_ENVIO in the environment is REJECTED here — before
	// generating a secret and before opening the database. Instagram
	// NEVER falls into the `consumerMeta` branch (below: this slice
	// has no equivalent POST /v1/cadastro for Instagram, T-111 declared
	// it WhatsAppOnly), so without this rejection both secrets were
	// RANDOMLY GENERATED and the command finished successfully —
	// README.md:118-120 says creation "requires" both variables, and
	// until now that was false. The randomly generated app_secret is
	// especially serious: it rejects EVERY webhook by HMAC (the signature
	// Meta sends uses the REAL app_secret, which the generated one never
	// matches), and nothing in the command warned about it — the operator
	// only found out at the smoke test or on the first webhook, hours
	// later.
	if inst.Type == config.TypeInstagram {
		var missing []string
		if strings.TrimSpace(env("ZAPGW_APP_SECRET")) == "" {
			missing = append(missing, "ZAPGW_APP_SECRET")
		}
		if strings.TrimSpace(env("ZAPGW_TOKEN_ENVIO")) == "" {
			missing = append(missing, "ZAPGW_TOKEN_ENVIO")
		}
		if len(missing) > 0 {
			return fmt.Errorf("zapgw: --tipo instagram exige %s no ambiente — esta fatia nao tem "+
				"POST /v1/cadastro equivalente para Instagram (T-111), entao um valor sorteado aqui "+
				"nunca seria substituido por um real: a instancia nasceria e nunca funcionaria (o "+
				"app_secret sorteado rejeita todo webhook por HMAC, em silencio)",
				strings.Join(missing, " e "))
		}
	}

	// This list's order matches the documented variables; each one
	// points at the field it fills, so "where does this secret come
	// from?" has a single answer.
	//
	// THE META ACCOUNT BELONGS TO THE CONSUMER when the instance is
	// WhatsApp and it brought no identification at all — and that is the
	// same question that decides the delivery package, further below.
	// INSTAGRAM NEVER FALLS HERE: this slice has no equivalent
	// RegisterMeta for Instagram (it is not in T-097's Files), so
	// `app_secret`/`token_envio` for an Instagram instance HAS to come
	// FROM THIS CALL — via env, and now (T-114) ONLY via env: the check
	// right above REJECTS creation if either one is missing, so the
	// generation below never runs for Instagram (it gets there with both
	// already guaranteed present). Before this check, `inst.WabaID == ""`
	// (always true for an Instagram instance, which NEVER has a waba_id —
	// see ValidateInstanceType) made an Instagram instance fall into
	// the "toRegister" branch and be born with app_secret/token_envio
	// LITERALLY EMPTY, with no generation at all. 🔴 The sentence that
	// used to be here — "impossible to fix without SQL by hand" — was
	// FALSE (T-114): `zapgw instancia rotacionar --slug <slug>` swaps
	// either one without touching SQL. The real problem was never
	// "impossible to fix": it was the instance being born BROKEN and the
	// command finishing successfully with no warning — that is what the
	// check above closes, at the source.
	consumerMeta := inst.Type != config.TypeInstagram && inst.WabaID == ""

	secrets := []struct {
		variable    string
		field       string
		destination *string
		// shared flags the secret whose value has to reach
		// someone OUTSIDE this gateway for provisioning to FINISH. See
		// the T-052 block below: it is the difference that decides
		// whether generating it silently is a convenience or a
		// stillborn instance.
		shared bool
		// fromConsumerMeta flags the secret that is NOT OURS: it
		// exists in someone else's Meta account and the gateway only
		// stores it. See the T-079 block right below.
		fromConsumerMeta bool
	}{
		{"ZAPGW_APP_SECRET", "app_secret", &inst.AppSecret, false, true},
		{"ZAPGW_VERIFY_TOKEN", "verify_token", &inst.VerifyToken, true, false},
		{"ZAPGW_TOKEN_ENVIO", "token_envio", &inst.SendToken, false, true},
		{"ZAPGW_SEGREDO_ENTREGA", "segredo_entrega", &inst.DeliverySecret, true, false},
	}
	var generated []string
	var toRegister []string
	var drawnShared [][2]string
	for _, s := range secrets {
		if value := strings.TrimSpace(env(s.variable)); value != "" {
			*s.destination = value
			continue
		}
		// --- T-079: DO NOT GENERATE A SECRET THAT BELONGS TO SOMEONE ELSE ---------------
		//
		// Generating every missing secret was correct as long as
		// `app_secret` and `token_envio` could be ANY value — they only
		// existed inside the gateway, and a generated value no one used
		// was harmless (the rule and the whole reasoning are in
		// `printSharedSecrets`, T-052).
		//
		// WITH THE CONSUMER'S META ACCOUNT THAT STOPPED BEING TRUE, and
		// the damage lands on the ONLY read surface that exists:
		// `instancia mostrar` and the registration's response only say
		// `cadastrado: sim|nao` (nothing is decrypted, since T-020). A
		// generated value makes both say `app_secret=sim` about a secret
		// the consumer's Meta account never saw — meaning the gateway
		// starts CLAIMING it is configured when it isn't, and the one
		// question the owner and the consumer can ask loses its answer.
		// Leaving it EMPTY is what keeps `sim` meaning "they registered
		// it".
		//
		// And the risk of being born "with no secret" does not exist:
		// the instance is born PAUSED, `zapgw fumaca` is the only path to
		// activate it, and it requires a message that actually went out
		// — impossible without a real token_envio.
		if s.fromConsumerMeta && consumerMeta {
			toRegister = append(toRegister, s.field)
			continue
		}
		value, err := randomSecret()
		if err != nil {
			return err
		}
		*s.destination = value
		if s.shared {
			drawnShared = append(drawnShared, [2]string{s.field, value})
			continue
		}
		generated = append(generated, s.variable)
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if err := store.CreateInstanceAt(inst, creationClock()); err != nil {
		return fmt.Errorf("zapgw: criar instancia %q: %w", inst.Slug, err)
	}

	fmt.Fprintf(out, "instancia %q criada.\n", inst.Slug)
	if len(generated) > 0 {
		// SAY WHICH ONES, never the value: without this line the
		// operator walks away thinking their own app_secret was used
		// when it was actually generated, and inbound would reject
		// Meta's HMAC with no one understanding why.
		fmt.Fprintf(out, "segredos sorteados aqui (o valor NAO e mostrado): %s\n",
			strings.Join(generated, ", "))
	}
	if len(toRegister) > 0 {
		// SAY WE DID NOT GENERATE THEM, and why: without this line the
		// owner sees `app_secret=nao` in `instancia mostrar` and treats
		// it as a defect.
		fmt.Fprintf(out, "NAO sorteados, porque sao da conta Meta do CONSUMIDOR: %s — ele os cadastra por POST /v1/cadastro.\n",
			strings.Join(toRegister, ", "))
	}
	printSharedSecrets(out, inst.Slug, drawnShared)
	if inst.CABundle != "" {
		// SAY the anchor changed, and say it loosens nothing: without
		// this line, "I registered the CA" and "I turned off
		// verification" become the same thing in whoever operated it's
		// head.
		fmt.Fprintf(out, "bundle de CA proprio cadastrado para esta instancia: a verificacao do certificado continua ESTRITA, so a ancora de confianca muda.\n")
	}
	fmt.Fprintf(out, "webhook para colar na Meta: %s\n", webhookURL(env, inst.Slug))
	fmt.Fprintf(out, "a instancia nasceu PAUSADA: enquanto ativo = 0, o webhook responde 503 e o envio tambem.\n")
	if inst.Type == config.TypeInstagram {
		fmt.Fprintf(out, "so o teste de fumaca ativa:  zapgw fumaca --slug %s --destino <IGSID que te mandou mensagem nas ultimas 24h>\n", inst.Slug)
	} else {
		fmt.Fprintf(out, "so o teste de fumaca ativa:  zapgw fumaca --slug %s --destino <numero em E.164>\n", inst.Slug)
	}
	// THE DELIVERY PACKAGE only applies to the THIRD-PARTY model with
	// their own Meta account (T-079, docs/MODELO-DE-USO.md) — which is a
	// WhatsApp-ONLY model in this slice (Instagram has no equivalent
	// POST /v1/cadastro). Without this `&&`, an Instagram instance
	// (which is also born with WabaID = "", by definition —
	// ValidateInstanceType) would print a package pointing at a
	// registration route it will never be able to use.
	if inst.Type != config.TypeInstagram && inst.WabaID == "" {
		printDeliveryPackage(out, env, inst.Slug)
	}
	return nil
}

// printDeliveryPackage lists what the owner delivers to the
// consumer — and nothing beyond that (T-079).
//
// WHY IT EXISTS, and why it is a LIST and not a sentence: the
// conversation with a third party happens ONCE, and the owner has no
// channel for "oh, I forgot to send you X". One item forgotten here turns
// into a stuck consumer with no one to ask — and this project's doctrine
// is that, with no channel, the documentation and the message ARE the
// support.
//
// ONLY SHOWS UP WHEN THE INSTANCE IS BORN WITH NO IDENTIFICATION, which
// is the case for a third party with their own Meta account. Whoever
// passed `--waba-id` is creating an instance from THEIR OWN account (a
// lab, or their own business) and is not going to deliver any package to
// anyone.
//
// THE CONSUMER TOKEN IS NOT PRINTED HERE, and the absence is honest: it
// is born in a different command (`zapgw provisionar consumidor`), which
// requires the instance to already exist. Printing a placeholder in its
// place would be worse than pointing at the command — the owner would
// copy the entire list thinking it is complete.
func printDeliveryPackage(out io.Writer, env environment, slug string) {
	fmt.Fprintf(out, "\nPACOTE DE ENTREGA — o que o CONSUMIDOR precisa receber, e nada alem disto:\n")
	fmt.Fprintf(out, "  1. o slug:                     %s\n", slug)
	fmt.Fprintf(out, "  2. a URL de cadastro (POST):   %s\n", enrollmentURL(env))
	fmt.Fprintf(out, "  3. a URL do webhook, que ELE cola no painel da Meta DELE:\n")
	fmt.Fprintf(out, "                                 %s\n", webhookURL(env, slug))
	fmt.Fprintf(out, "  4. o verify_token e o segredo_entrega impressos acima\n")
	fmt.Fprintf(out, "  5. o token de consumidor, que sai do proximo comando:\n")
	fmt.Fprintf(out, "     zapgw provisionar consumidor --nome <nome-dele> --instancias %s\n", slug)
	fmt.Fprintf(out, "quem cadastra waba_id, phone_number_id, numero, app_secret, token_envio e callback_url e ELE,\n")
	fmt.Fprintf(out, "por POST /v1/cadastro — voce nao precisa desses valores e nao deve pedi-los.\n")
	// THE WINDOW, told to the owner at creation time, because he is the
	// one who has the command to reopen it and he is the one who will
	// receive the request when it closes.
	fmt.Fprintf(out, "ele tem %s para cadastrar, contados da PRIMEIRA insercao dele (nao de agora).\n",
		config.RegistrationWindow)
	fmt.Fprintf(out, "se ele travar depois disso:  zapgw instancia reabrir-cadastro --slug %s --confirmo %s\n", slug, slug)
}

// enrollmentURL assembles the POST /v1/cadastro URL through the SAME
// path as webhookURL — and with the same honesty: without
// ZAPGW_URL_PUBLICA it prints a VISIBLE placeholder, never a guessed
// domain.
//
// ⚠️ THIS URL IS ON THE LAN TODAY (docs/IMPLANTACAO.md: :8443 matches by
// EXCLUDING /v1/inbound). A real third party cannot reach it from the
// internet, and that is an owner decision still OPEN
// (docs/MODELO-DE-USO.md). Until it's decided, whoever registers is
// whoever has access to the gateway's network.
func enrollmentURL(env environment) string {
	base := strings.TrimRight(strings.TrimSpace(env("ZAPGW_URL_PUBLICA")), "/")
	if base == "" {
		base = "https://<defina ZAPGW_URL_PUBLICA>"
	}
	return base + "/v1/cadastro"
}

// reopenEnrollment gives the consumer back the right to write their own
// configuration — and ONLY that.
//
// THE CONFIRMATION IS RETYPING THE SLUG, like in `instancia remover`,
// and the reason here is different from there: reopening deletes
// nothing, but reopening the WRONG slug means silently giving one
// consumer 24h of write access to another's instance — nothing fails,
// nothing flags it, and the owner walks away thinking he unblocked the
// consumer who complained (who is still stuck).
func reopenEnrollment(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("instancia reabrir-cadastro", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia cuja janela de cadastro sera reaberta")
	confirm := fs.String("confirmo", "", "digite o slug DE NOVO para confirmar. Reabrir no slug errado da 24h de escrita sobre a instancia de outro consumidor, sem nada acusar")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio (use `zapgw instancia listar` para ver os slugs)")
	}
	if strings.TrimSpace(*confirm) != who {
		return fmt.Errorf("zapgw: reabrir a janela de %q da ao consumidor dela 24h de escrita — repita o slug em --confirmo:"+
			"  zapgw instancia reabrir-cadastro --slug %s --confirmo %s", who, who, who)
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if err := store.ReopenRegistrationWindow(who); err != nil {
		return fmt.Errorf("zapgw: reabrir a janela de cadastro da instancia %q: %w", who, err)
	}

	fmt.Fprintf(out, "instancia %q: janela de cadastro REABERTA.\n", who)
	// WHAT CHANGED AND WHAT DIDN'T, in this order: the expensive
	// confusion here would be the owner thinking he fixed the consumer's
	// configuration.
	fmt.Fprintf(out, "nenhum campo da configuracao foi tocado — quem cadastra continua sendo o CONSUMIDOR, por POST /v1/cadastro.\n")
	fmt.Fprintf(out, "o relogio NAO comeca agora: as %s contam da proxima vez que ele cadastrar algo.\n",
		config.RegistrationWindow)
	fmt.Fprintf(out, "avise-o — o gateway nao tem como avisar ninguem.\n")
	return nil
}

// printSharedSecrets shows, ONCE, the generated secrets that
// someone OUTSIDE this gateway needs to type somewhere else.
//
// --- T-052: why these TWO are shown and the other two aren't ------------
//
// The CLI generates every missing secret and says WHICH ones it
// generated, never the value. For `app_secret` and `token_envio` that is
// correct and remains so: the real value is chosen by Meta and registered
// here; no one needs to read back what the gateway generated (and if the
// generated one ever gets used, inbound rejects the HMAC and the error
// shows up).
//
// The other two are SHARED by definition — provisioning only FINISHES
// when their value reaches a person:
//   - `verify_token`   — the owner types it into Meta's panel;
//   - `segredo_entrega`— the consumer puts it in their `.env` to check
//     the delivery's signature.
//
// Generated and never shown, both would stay ILLEGIBLE FOREVER (the
// store keeps them encrypted and nothing is ever decrypted back in any
// command): the instance is born, `instancia mostrar` says
// `verify_token=sim segredo_entrega=sim` — looks complete — and it is
// IMPOSSIBLE to finish provisioning, with no error pointing at the cause.
// The symptom shows up days later, in Meta's panel, as "verification is
// rejecting", which sends you looking in the wrong place. It cost an
// extra rotation in T-046 (2026-07-28).
//
// THE TASK HAD TWO WAYS OUT — reject creation without them, or generate
// and print only these two. THIS IMPLEMENTATION PRINTS, and why:
//
//  1. a secret whose job is to be CARRIED by a person is going to be
//     seen by a person. Refusing to show it does not reduce human
//     exposure — it only moves the value's GENERATION outside, where it
//     is worse;
//  2. and it is worse in a measurable way: a human in a hurry makes up
//     `segredo123`. `segredo_entrega` is the HMAC key for EVERY delivery
//     to the consumer; it is exactly the value that cannot depend on
//     inspiration. Here it comes from crypto/rand, 32 bytes
//     (`randomSecret`);
//  3. refusing would have a legitimate false positive: an OUTBOUND-ONLY
//     instance (`callback_url` empty — a normal state, see
//     `absenceNote`) never uses `segredo_entrega`, and requiring it
//     would block a case this project deliberately supports;
//  4. it invents no new mechanism: it is the SAME warning and the SAME
//     format as the consumer token (`printConsumerToken`), which
//     already existed.
//
// THE ACCEPTED COST, written down because it is real: the value passes
// through the terminal, and a terminal becomes a transcript — that is how
// four secrets leaked on 2026-07-28. It is accepted because (a) it is the
// same cost the consumer token has always paid, (b) the value has to be
// copied one way or another, and (c) a cheap way out exists if it leaks:
// `zapgw instancia rotacionar`. The alternative did not eliminate the
// exposure, it only moved it to a weaker value.
//
// WHOEVER PASSED THE VALUE THROUGH THE ENVIRONMENT sees nothing here:
// they already have the value, and printing it again would be exposing
// it for free.
func printSharedSecrets(out io.Writer, slug string, pairs [][2]string) {
	if len(pairs) == 0 {
		return
	}
	fmt.Fprintf(out, "GUARDE AGORA os valores abaixo — o gateway guarda so o cifrado e NAO os mostra de novo.\n")
	fmt.Fprintf(out, "eles foram sorteados aqui e sao COMPARTILHADOS: sem eles nas maos de quem configura, o provisionamento nao termina.\n")
	for _, p := range pairs {
		fmt.Fprintf(out, "%s: %s\n", p[0], p[1])
		switch p[0] {
		case "verify_token":
			fmt.Fprintf(out, "  ^ este e o valor que voce digita em Verify Token no painel da Meta.\n")
		case "segredo_entrega":
			fmt.Fprintf(out, "  ^ este e o valor que o CONSUMIDOR poe no .env dele para conferir a assinatura da entrega.\n")
		}
	}
	// The way out for whoever closes the terminal before copying.
	// Without this line, the only apparent way out becomes recreating
	// the instance — and the slug is IMMUTABLE, so recreating isn't even
	// possible. `rotacionar` does NOT generate: the new value comes from
	// the environment, so whoever rotates it already knows what it is.
	fmt.Fprintf(out, "perdeu? nao ha como recuperar — gere um valor novo e ponha no ambiente:\n")
	fmt.Fprintf(out, "  ZAPGW_VERIFY_TOKEN=<novo> zapgw instancia rotacionar --slug %s\n", slug)
}

// webhookURL assembles the URL to paste into Meta's panel.
//
// The public host is NOT guessed: it comes from ZAPGW_URL_PUBLICA.
// Without it the command prints a VISIBLE placeholder instead of guessing
// a domain — a wrong URL pasted into Meta makes the webhook fail silently
// on both sides, which is exactly the failure mode recorded in
// docs/ARMADILHAS.md.
func webhookURL(env environment, slug string) string {
	base := strings.TrimRight(strings.TrimSpace(env("ZAPGW_URL_PUBLICA")), "/")
	if base == "" {
		base = "https://<defina ZAPGW_URL_PUBLICA>"
	}
	return base + "/v1/inbound/" + slug
}

func provisionConsumer(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("provisionar consumidor", flag.ContinueOnError)
	fs.SetOutput(out)
	name := fs.String("nome", "", "nome do sistema consumidor")
	list := fs.String("instancias", "", "slugs que ele pode usar, separados por virgula")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	whoIs := strings.TrimSpace(*name)
	if whoIs == "" {
		return errors.New("zapgw: --nome e obrigatorio")
	}

	var slugs []string
	for _, s := range strings.Split(*list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			slugs = append(slugs, s)
		}
	}
	if len(slugs) == 0 {
		// A consumer with no link at all authenticates and receives 403
		// on everything — a symptom that doesn't look like the cause.
		return errors.New("zapgw: --instancias e obrigatorio (pelo menos um slug)")
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// CHECK BEFORE CREATING. The foreign_keys PRAGMA would also block
	// the orphan link, but with a database error that doesn't say which
	// slug was wrong; and that is exactly what whoever typed it cares
	// about. Checking here also guarantees nothing is created when the
	// ERROR is in the last slug of the list.
	for _, slug := range slugs {
		if _, err := store.FindInstance(slug); err != nil {
			if errors.Is(err, config.ErrInstanceNotFound) {
				return fmt.Errorf("zapgw: instancia %q nao existe — vincular a um slug inexistente vira um 403 futuro que ninguem sabe explicar: %w", slug, err)
			}
			return fmt.Errorf("zapgw: conferir instancia %q: %w", slug, err)
		}
	}

	token, err := randomSecret()
	if err != nil {
		return err
	}
	if err := store.CreateConsumer(whoIs, token, slugs); err != nil {
		return fmt.Errorf("zapgw: criar consumidor %q: %w", whoIs, err)
	}

	fmt.Fprintf(out, "consumidor %q criado, com acesso a: %s\n", whoIs, strings.Join(slugs, ", "))
	printConsumerToken(out, token)
	return nil
}

// printConsumerToken shows the token ONCE, with the warning that
// it does not come back.
//
// IT IS A FUNCTION, and not the same text written in two commands,
// because creation and rotation (T-055) have exactly the same
// consequence — the store only keeps the hash (config.HashToken), so this
// is the only instant the value exists — and two copies of the warning
// diverge the first time someone improves one of them. The "token:
// <value>" format is also a contract with whoever operates it and with
// the tests.
func printConsumerToken(out io.Writer, token string) {
	fmt.Fprintf(out, "GUARDE AGORA o token abaixo — o gateway guarda so o hash dele e nao ha como recupera-lo.\n")
	fmt.Fprintf(out, "token: %s\n", token)
}

// rotateConsumer generates a new token for a consumer that
// ALREADY EXISTS and REVOKES the previous one at the same instant.
//
// THERE IS NO FLAG TO KEEP BOTH VALID, and the absence is the guarantee:
// whoever rotates it is reacting to an exposed token, and a rotation
// that leaves the old one working revokes nothing — it only creates the
// feeling of having revoked it. Two tokens coexisting, if ever needed for
// a no-downtime cutover, is a NEW consumer with the same links, deleted
// afterward; not a "--keep".
//
// THE NAME DOES NOT CHANGE and the LINKS DO NOT MOVE (see
// config.RotateConsumer): the name is the primary key and the
// instances it can use live in a different table, which this command
// does not touch.
func rotateConsumer(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("consumidor rotacionar", flag.ContinueOnError)
	fs.SetOutput(out)
	name := fs.String("nome", "", "consumidor cujo token sera trocado. O nome nao muda: ele diz QUEM, nunca o que trocar")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	whoIs := strings.TrimSpace(*name)
	if whoIs == "" {
		return errors.New("zapgw: --nome e obrigatorio (use `zapgw consumidor listar` para ver os nomes)")
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	token, err := randomSecret()
	if err != nil {
		return err
	}
	if err := store.RotateConsumer(whoIs, token); err != nil {
		if errors.Is(err, config.ErrConsumerNotFound) {
			return fmt.Errorf("zapgw: consumidor %q nao existe — nada foi rotacionado, e o token que voce quer revogar CONTINUA valendo: %w", whoIs, err)
		}
		return fmt.Errorf("zapgw: rotacionar o consumidor %q: %w", whoIs, err)
	}

	fmt.Fprintf(out, "consumidor %q: token trocado. O ANTERIOR nao autentica mais — a partir de agora ele recebe 401.\n", whoIs)
	fmt.Fprintf(out, "os vinculos de instancia NAO foram tocados: o que ele podia usar antes, continua podendo.\n")
	printConsumerToken(out, token)
	// Without this line, whoever rotated it walks away thinking they're
	// done, and the consumer finds out about the swap through a 401 in
	// production. The gateway has no channel to warn anyone — whoever
	// operates it does.
	fmt.Fprintf(out, "avise o consumidor AGORA: as chamadas dele falham com 401 ate ele trocar o token do lado dele.\n")
	return nil
}

// listConsumers answers "who has access to this instance?" without
// opening the database by hand — a question that, until T-055, only had
// an answer through SQL.
func listConsumers(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("consumidor listar", flag.ContinueOnError)
	fs.SetOutput(out)
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	list, err := store.ListConsumers()
	if err != nil {
		return fmt.Errorf("zapgw: listar consumidores: %w", err)
	}
	if len(list) == 0 {
		// An empty output cannot be told apart from "the command didn't
		// run" — same reason as `instancia listar`.
		fmt.Fprintf(out, "nenhum consumidor cadastrado neste banco.\n")
		return nil
	}

	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tab, "NOME\tINSTANCIAS QUE ELE PODE USAR (nenhum token, nem o hash, e mostrado)\n")
	for _, c := range list {
		// A consumer with no link authenticates and receives 403 on
		// EVERYTHING, and a blank row wouldn't say that — the text does,
		// because the symptom doesn't look like the cause.
		which := "(nenhuma — ele autentica e recebe 403 em tudo)"
		if len(c.Instances) > 0 {
			which = strings.Join(c.Instances, ", ")
		}
		fmt.Fprintf(tab, "%s\t%s\n", c.Name, which)
	}
	return tab.Flush()
}
