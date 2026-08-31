// `zapgw estado` — the operator's dashboard, on the command line (T-035).
//
// WHY THIS EXISTS: this gateway's most expensive signal —
// "ALARME zapgw: instancia recusou; evento perdido em definitivo"
// (internal/inbound/mirror.go) — until now only existed as a line in the
// journal. No one reads the journal out of habit, and by the time someone
// does, the message has already been lost. This command reads the SAME
// count the handler records (internal/config/contador.go) and prints the
// alarm first — never a line in the middle of a table.
//
// IT SHOWS THE SAME STATE AS THE `GET /v1/estado` ROUTE, assembled by the
// SAME function (outbound.BuildState) — T-065. Until then, this screen
// only had the counters table: `estado`/`pausada`, `versao`, `token_meta`
// and `certificado_do_callback` were four blocks the CONSUMER saw and the
// OPERATOR didn't. And the operator with SSH open on the CT is, almost by
// definition, in the middle of an incident — and that is exactly the
// person who most needs "does Meta still accept this token?" and "when
// does the consumer's certificate expire?". Before T-065 they would have
// had to leave the CT, find a consumer token, and call the internal route
// to learn what the binary in front of them already knew.
//
// WHAT BELONGS TO THIS FILE IS ONLY THE SCREEN FORMAT: tabwriter, words,
// terminal width. NO state field is enumerated here — the rows come from
// outbound.StateRows, and the table iterates over
// config.KeysInDisplayOrder. A new state field appears on this screen
// without anyone editing this file, and that is what keeps the defect from
// being born again.
//
// NOT AN HTTP ENDPOINT ON PURPOSE: the shape of a viewing surface (should
// one ever exist) is the owner's decision; this task only delivers the
// table, the counting and this command.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
	"github.com/iscarelli/zapgw/internal/outbound"
)

func stateCommand(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("estado", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a mostrar; vazio = TODAS as instancias cadastradas")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	// The SAME resolution the server does on startup (T-120), and it fails
	// here for the SAME reason: `zapgw estado` is the screen the operator
	// opens in the middle of an incident, and a screen that publishes a
	// wrong inbound path is worse than one screen fewer. Empty is not an
	// error — it publishes `desconhecido`.
	via, err := outbound.IngressVia(env)
	if err != nil {
		return err
	}

	// The SAME discipline as IngressVia above, and for the SAME reason:
	// this is the screen the operator opens in the middle of an incident.
	// An unreadable leadership configuration brings down the command
	// instead of painting a `lideranca` block that no one checked — a
	// screen that lies about who is sending is worse than no screen at
	// all.
	leadership, err := outbound.NewLeadership(env)
	if err != nil {
		return err
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	var instances []config.InstanceSummary
	if who := strings.TrimSpace(*slug); who != "" {
		r, err := store.SummarizeInstance(who)
		if err != nil {
			if errors.Is(err, config.ErrInstanceNotFound) {
				return fmt.Errorf("zapgw: instancia %q nao existe (use `zapgw instancia listar` para ver os slugs): %w", who, err)
			}
			return fmt.Errorf("zapgw: buscar instancia %q: %w", who, err)
		}
		instances = []config.InstanceSummary{r}
	} else {
		instances, err = store.ListInstances()
		if err != nil {
			return fmt.Errorf("zapgw: listar instancias: %w", err)
		}
	}
	if len(instances) == 0 {
		// Empty cannot become an error (Verify (f)): a freshly created
		// database, or a slug filter that exists but never received
		// traffic, are the two normal cases for this command, not a
		// failure.
		fmt.Fprintf(out, "nenhuma instancia cadastrada neste banco.\n")
		return nil
	}

	// This `now` is the SNAPSHOT instant: it stamps `gerado_em` and
	// decides which days enter the counters' window. It is NOT the
	// reference point for the printed distances — those are measured
	// against the printing-time now, further down (T-072).
	now := time.Now()

	// THIS PROCESS'S WATCHER IS BORN EMPTY, and that is why it MEASURES
	// before reading. The verdict cache lives in the SERVER process's
	// memory (vigia.go); this binary just started and has no history at
	// all, so reading without measuring would give `desconhecido` for
	// everything, always — worse than not showing the block, because it
	// looks like a broken watcher. See Watchdog.CheckInstance.
	//
	// WHAT THIS COSTS, and it is written down because it changes the
	// command's behavior: `zapgw estado` now talks to the Graph API (one
	// READ call per active instance shown). It still does NOT fail because
	// of that — an attempt with no response does not become a rejection,
	// it becomes `desconhecido` with `checagem_falhando_desde` filled in,
	// which is the same rule as the watcher — and the deadline is the
	// instance's, so Meta being down delays the screen by the timeout, not
	// the lock.
	//
	// AND SINCE T-080 THIS COMMAND ALSO WRITES TO THE DATABASE, and the
	// change deserves to be written down: the same tick saves the number's
	// quality and messaging limit (the `numero_na_meta` block), which live
	// in the DATABASE and not in the watcher's memory. Without recording
	// it, this process would measure and the screen would show
	// `nunca_observado` right after — and the operator would read "the
	// gateway doesn't know" about data the binary in front of them just
	// fetched. The write never brings anything down
	// (config.NumberObserver.Register returns no error) and the
	// database is in WAL with busy_timeout, so it coexists with the server
	// writing alongside it.
	watchdog := outbound.NewWatchdog(store, meta.NewClient(nil, graphBase(env)))
	for _, inst := range instances {
		// A PAUSED instance is not measured, exactly like on the server: it
		// does not send, and spending a call on it would mean measuring a
		// channel that cannot fail. The `pausada: sim` on screen already
		// explains the `desconhecido` next to it.
		if !inst.Active {
			continue
		}
		watchdog.CheckInstance(context.Background(), inst.Slug)
	}

	// THIS PROCESS'S PROBE IS ALSO BORN EMPTY, and that is why it measures
	// ONCE before reading — same reason as the paragraph above about the
	// watcher, and the same cost (smaller here: the connector's `/ready`
	// is local and spends no one's quota). Without this tick the screen
	// would always say `desconhecido`, which is exactly the shape of lie
	// T-120 exists to not have.
	//
	// ASKING `/ready` IS PURE READING, so a STATUS command can do it —
	// unlike the Instagram token renewer, which MUTATES the credential and
	// therefore still gets passed `nil` below.
	connector := outbound.NewConnectorProbe(outbound.ConnectorAddress(env))
	connector.Measure(context.Background())
	in := outbound.IngressSource{Via: via, Connector: connector}

	// THE EXTERNAL PROBE (T-121) IS BORN EMPTY FOR THE SAME REASON AS THE
	// CONNECTOR, two paragraphs above: this process just started and has
	// no history. Asking is PURE READING (the external probe changes
	// nothing, it just answers "up"/"down"), so the status command can do
	// it directly.
	externalProbe := outbound.NewExternalProbe(outbound.ExternalProbeURL(env))
	externalProbe.Measure(context.Background())

	// A single reading per instance, stored here — the screen BELOW reads
	// from the same state as the alarm check, so the two can never diverge
	// from reading the database at different instants.
	//
	// THE ASSEMBLY BELONGS TO outbound.BuildState, not to this command:
	// the GET /v1/estado route (T-060) shows the CONSUMER the SAME state,
	// and two assemblies would diverge on the first change — the operator
	// and the consumer disagreeing about the same instance (T-065).
	states := make(map[string]outbound.State, len(instances))
	var instancesWithAlarm []string
	for _, inst := range instances {
		// nil in place of the renewer (T-098): ticking it before reading
		// would MUTATE the credential (renewing is not reading, unlike
		// what the watcher's CheckInstance does) — wrong as a side
		// effect of a STATUS command. `token_instagram.falhando_desde`
		// comes out empty on this screen for that reason; the block's
		// other fields (definido_em/expira_em/renovado_em) come from the
		// DATABASE and stay correct in any process.
		e, err := outbound.BuildState(store, watchdog, nil, in, externalProbe, leadership, version, inst.Slug, now)
		if err != nil {
			return fmt.Errorf("zapgw: estado da instancia %q: %w", inst.Slug, err)
		}
		states[inst.Slug] = e
		if e.Counters[config.CounterDefinitiveLossAlarm].Last7Days > 0 {
			instancesWithAlarm = append(instancesWithAlarm, inst.Slug)
		}
	}

	// THE ALARM IS THE FIRST VISIBLE THING, never a line in the middle of
	// a table (T-035, Do item 5) — and that is why this block prints
	// BEFORE any per-instance table, even if that means repeating the
	// number below.
	if len(instancesWithAlarm) > 0 {
		fmt.Fprintf(out, "======================================================================\n")
		fmt.Fprintf(out, "ALARME: %d instancia(s) com PERDA DEFINITIVA nos ultimos 7 dias: %s\n",
			len(instancesWithAlarm), strings.Join(instancesWithAlarm, ", "))
		fmt.Fprintf(out, "A Meta ja respondeu 200 para essas mensagens — ela NAO reenvia mais.\n")
		fmt.Fprintf(out, "Procure \"ALARME zapgw\" no log do servico para a correlacao de cada evento.\n")
		fmt.Fprintf(out, "======================================================================\n\n")
	}

	for _, inst := range instances {
		e := states[inst.Slug]

		fmt.Fprintf(out, "instancia %q\n", inst.Slug)

		// TWO tabwriters, not one: the label/value block has two columns
		// and the counters table has four. On the same writer, the
		// tabwriter would align one's columns by the other's and the
		// screen would come out crooked.
		blocks := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		// NO field list here: the rows come from the state, in the order
		// it declares them. This is what makes a new field appear on this
		// screen without anyone editing this file (T-065).
		//
		// AND NO `now` EITHER (T-072): each timestamp's distance is
		// measured against the PRINTING-time now, which outbound reads on
		// its own. Passing `now` from here used to be the defect — it
		// stamped `gerado_em` BEFORE measuring the token against the Graph
		// API, so `medido_em` came out "in 1s", a future over a past fact.
		// See outbound.printClock.
		for _, l := range outbound.StateRows(e) {
			fmt.Fprintf(blocks, "  %s%s:\t%s\n", strings.Repeat("  ", l.Level), l.Label, l.Value)
		}
		if err := blocks.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(out)

		tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tab, "  chave\thoje\tultimos 7 dias\tultimo em\n")
		// Iterates over config.KeysInDisplayOrder — the SINGLE SOURCE
		// of the vocabulary (T-039). There is no separate list here: a new
		// key only needs to enter there to appear in this table.
		for _, key := range config.KeysInDisplayOrder {
			c := e.Counters[key]
			fmt.Fprintf(tab, "  %s\t%d\t%d\t%s\n",
				key, c.Today, c.Last7Days, outbound.ReadableStamp(c.LastAt))
		}
		if err := tab.Flush(); err != nil {
			return err
		}

		// THE DAILY SERIES STAYS OUT OF THIS SCREEN BY THE OWNER'S
		// DECISION (dozens of lines per instance would make the terminal
		// useless), but its ABSENCE needs to be stated — otherwise the
		// operator does not know it exists.
		//
		// WHY THIS LINE IS WORTH THE SPACE IT TAKES (T-083): the defect
		// T-065 fixed was not "the information is somewhere else", it was
		// "no one knew it existed" — four blocks the consumer saw and the
		// operator didn't. Omitting the series WITHOUT saying where it
		// lives would reproduce exactly that defect, just in the opposite
		// direction.
		fmt.Fprintf(out, "  a serie DIARIA (dia a dia, ate %d dias) nao cabe nesta tela e sai por\n",
			config.CounterRetentionDays(env))
		fmt.Fprintf(out, "  GET /v1/estado?instancia=%s&serie_dias=N — os MESMOS numeros, por dia.\n",
			inst.Slug)

		fmt.Fprintln(out)
	}
	return nil
}
