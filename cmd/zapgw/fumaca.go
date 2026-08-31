// `zapgw fumaca` — the COMMAND-LINE facade of the smoke test.
//
// THE WHOLE PATH (the four steps, and the guarantee that `ativo = 1` only
// happens after a real send) lives in outbound.SmokeWithInstagramBase
// (internal/outbound/fumaca.go) — the SAME function that `POST /v1/fumaca`
// calls (internal/outbound/fumaca_handler.go, T-084). This file only does
// flags, opens the store and prints progress; no business rule lives here.
//
// WHY THERE WAS A SINGLE PATH UNTIL T-084, AND WHY THERE ARE NOW TWO
// FACADES: until this task, `zapgw fumaca` was a command line and nothing
// else — a third party with no shell on the gateway machine had no way to
// prove their own channel, and step 4 of the model (docs/MODELO-DE-USO.md)
// went unexecuted on its own. The new route is not a second implementation:
// it is the same logic, called from the other side. Two copies would
// diverge, and the one that diverged would be the one no one runs by hand.
//
// --- T-071: how a LAB instance gets activated ---------------------
//
// THE PROBLEM: a lab instance has no Meta number, so step 3 could never
// pass — and until 2026-07-28 docs/IMPLANTACAO.md prescribed closing that
// gap with `UPDATE instancia SET ativo = 1` typed by hand into the
// PRODUCTION database.
//
// THE TASK HAD TWO WAYS OUT. The one rejected was an `instancia ativar
// --sem-prova`: it would open a SECOND door to `ativo = 1`
// (config.ActivateInstance documents being the only one) and, worse, it
// would exercise a path production never uses — a lab proving what no one
// runs.
//
// THE ONE CHOSEN opens no door at all: the lab points the Graph API at
// ANOTHER ENDPOINT (ZAPGW_GRAPH_BASE, which main.go already read —
// `graphBase`) and runs THIS SAME command, with all four steps and the
// proof requirement intact. The fake lives in cmd/grafo-falso/ (a separate
// binary: implanta/deploy.sh only builds ./cmd/zapgw) and the recipe is in
// docs/IMPLANTACAO.md.
//
// For whoever wants proof the requirement survived:
// TestSmokeWithSendFailureLEAVESTheInstancePAUSED (fumaca_test.go) —
// pointing at a server that refuses the send leaves the instance PAUSED,
// and the fake has `--recusar-envio` for the operator to see that with the
// real binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
	"github.com/iscarelli/zapgw/internal/outbound"
)

func smoke(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("fumaca", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a provar e ativar")
	// NO DEFAULT, on purpose: a default here sends a message to the wrong
	// number, and a sent message cannot be undone.
	destination := fs.String("destino", "", "quem vai RECEBER a mensagem de teste — numero em E.164 (WhatsApp) ou IGSID (Instagram, "+
		"e so' funciona se ele tiver mandado mensagem nas ultimas 24h). OBRIGATORIO")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio")
	}
	toWhom := strings.TrimSpace(*destination)
	if toWhom == "" {
		return errors.New("zapgw: --destino e obrigatorio e nao tem default — o teste de fumaca manda uma mensagem DE VERDADE")
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// The SAME counter from production sending (T-054) and the SAME Graph
	// API client the server would use — neither one is exclusive to this
	// command.
	counter := config.NewCounter(store)
	client := meta.NewClient(&http.Client{}, graphBase(env))

	// T-104: the SAME environment resolution that already fed the
	// Instagram token renewer (main.go) — step 3 of the smoke test, for an
	// Instagram instance, calls SendInstagramMessage for real, and it
	// needs the graph.instagram.com host, not graphBase.
	result, err := outbound.SmokeWithInstagramBase(context.Background(), store, client, counter, who, toWhom,
		instagramRenewalBase(env), func(line string) { fmt.Fprintln(out, line) })
	if err != nil {
		return fmt.Errorf("zapgw: %w", err)
	}

	if result.AlreadyActive {
		fmt.Fprintf(out, "instancia %q ja estava ATIVA — nenhuma mensagem foi enviada. "+
			"para provar de novo, pause primeiro (`zapgw instancia pausar`).\n", result.Instance.Slug)
		return nil
	}

	fmt.Fprintf(out, "confirme com quem recebeu que a mensagem chegou — a Meta aceitar nao e a mensagem aparecer no celular.\n")
	return nil
}
