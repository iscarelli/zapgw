// `zapgw perdidas` — the failover post-mortem: what the node that fell had
// and never replicated.
//
// WHEN THIS IS USED: right after a failover of the high-availability pair.
// The supervisor, on taking over, keeps the old database aside instead of
// deleting it (docs/IMPLANTACAO.md); this command compares the two and
// answers "which sends were left at risk?".
//
// WHY IT EXISTS, and it is not convenience: the decision to stay on SQLite
// (docs/DECISAO-MODELO-DE-ALTA-DISPONIBILIDADE-2026-08-18.md) accepted a
// loss window. What made that acceptable was it being AUDITABLE — and an
// audit that depends on someone opening two SQLite files by hand, in the
// middle of an incident, is not auditable: it is theoretical.
package main

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

func lostCommand(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("perdidas", flag.ContinueOnError)
	fs.SetOutput(out)
	old := fs.String("antigo", "", "banco do no que CAIU, guardado de lado pelo supervisor (obrigatorio)")
	current := fs.String("atual", "", "banco em uso agora; vazio usa o mesmo que o gateway abriria")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}
	if *old == "" {
		return fmt.Errorf("zapgw: perdidas: informe --antigo <caminho do banco do no que caiu>.\n" +
			"  Ele e' a copia que o supervisor guarda ANTES de restaurar. Se ela nao existe,\n" +
			"  nao ha pericia possivel — e isso e' o que a instrucao de guardar o arquivo evita")
	}
	currentPath := *current
	if currentPath == "" {
		currentPath = env("ZAPGW_BANCO")
		if currentPath == "" {
			currentPath = "zapgw.db"
		}
	}

	c, err := config.CompareFailover(*old, currentPath)
	if err != nil {
		return fmt.Errorf("zapgw: perdidas: %w", err)
	}

	fmt.Fprintf(out, "comparando\n  antigo: %s (%d reservas)\n  atual:  %s (%d reservas)\n\n",
		*old, c.ReadInOld, currentPath, c.ReadInCurrent)

	// The size of what was compared is ALWAYS printed, and before the
	// verdict, because "nothing lost" over an empty old database is not
	// good news — it is a comparison that compared nothing. Whoever reads
	// it has to be able to tell the two readings apart without going to
	// check the file.
	if c.ReadInOld == 0 {
		fmt.Fprintf(out, "ATENCAO: o banco antigo nao tem NENHUMA reserva de idempotencia.\n"+
			"  Isso pode ser verdade (nenhum envio na retencao) ou pode ser o arquivo errado.\n"+
			"  Confira antes de concluir que nada se perdeu.\n\n")
	}

	if !c.Lost() {
		fmt.Fprintf(out, "NADA EM RISCO: toda reserva do banco antigo esta no atual.\n")
		return nil
	}

	if len(c.Confirmed) > 0 {
		fmt.Fprintf(out, "🔴 CONFIRMADAS PERDIDAS — %d. A mensagem CHEGOU a Meta e o gateway esqueceu.\n"+
			"   Um retry do consumidor com a mesma chave envia DE NOVO: duplicata no aparelho da cliente.\n\n",
			len(c.Confirmed))
		fmt.Fprintf(out, "  %-20s  %-38s  %-22s  %s\n", "consumidor", "chave", "reservada em (UTC)", "wamid")
		for _, e := range c.Confirmed {
			fmt.Fprintf(out, "  %-20s  %-38s  %-22s  %s\n",
				e.Consumer, e.Key, time.Unix(e.CreatedAt, 0).UTC().Format(time.RFC3339), e.Wamid)
		}
		fmt.Fprintln(out)
	}

	if len(c.Open) > 0 {
		fmt.Fprintf(out, "🟡 EM ABERTO PERDIDAS — %d. Reservaram e NAO confirmaram.\n"+
			"   Pode nunca ter saido, ou ter saido e o confirm nao ter replicado. NAO da para saber daqui:\n"+
			"   so' a Meta (ou o consumidor) responde. Estas NAO sao contadas como duplicata provavel.\n\n",
			len(c.Open))
		fmt.Fprintf(out, "  %-20s  %-38s  %s\n", "consumidor", "chave", "reservada em (UTC)")
		for _, e := range c.Open {
			fmt.Fprintf(out, "  %-20s  %-38s  %s\n",
				e.Consumer, e.Key, time.Unix(e.CreatedAt, 0).UTC().Format(time.RFC3339))
		}
		fmt.Fprintln(out)
	}

	// An error, not just text: whoever runs this in a script needs "there
	// was a loss" to be a status != 0. `PRECISA DE GENTE` ("NEEDS A HUMAN")
	// is the SAME marker the handler's alarms use, so whoever operates it
	// does not have to learn a second vocabulary.
	return fmt.Errorf("PRECISA DE GENTE: %d confirmada(s) perdida(s) e %d em aberto",
		len(c.Confirmed), len(c.Open))
}
