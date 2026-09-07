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
	old := fs.String("antigo", "", "database of the node that FELL, kept aside by the supervisor (required)")
	current := fs.String("atual", "", "database in use now; empty uses the same one the gateway would open")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}
	if *old == "" {
		return fmt.Errorf("zapgw: perdidas: provide --antigo <path to the fallen node's database>.\n" +
			"  It is the copy the supervisor keeps BEFORE restoring. If it does not exist,\n" +
			"  no forensics is possible — and that is exactly what the instruction to keep the file avoids")
	}
	currentPath := *current
	if currentPath == "" {
		var oldUsed bool
		currentPath, oldUsed = databasePath(env)
		config.WarnOldEnvVar(oldUsed, envDatabaseOld, envDatabaseNew)
	}

	c, err := config.CompareFailover(*old, currentPath)
	if err != nil {
		return fmt.Errorf("zapgw: perdidas: %w", err)
	}

	fmt.Fprintf(out, "comparing\n  old:     %s (%d reservations)\n  current: %s (%d reservations)\n\n",
		*old, c.ReadInOld, currentPath, c.ReadInCurrent)

	// The size of what was compared is ALWAYS printed, and before the
	// verdict, because "nothing lost" over an empty old database is not
	// good news — it is a comparison that compared nothing. Whoever reads
	// it has to be able to tell the two readings apart without going to
	// check the file.
	if c.ReadInOld == 0 {
		fmt.Fprintf(out, "WARNING: the old database has NO idempotency reservation at all.\n"+
			"  This may be true (no send in the retention window) or it may be the wrong file.\n"+
			"  Check before concluding that nothing was lost.\n\n")
	}

	if !c.Lost() {
		fmt.Fprintf(out, "NOTHING AT RISK: every reservation in the old database is in the current one.\n")
		return nil
	}

	if len(c.Confirmed) > 0 {
		fmt.Fprintf(out, "🔴 CONFIRMED LOST — %d. The message REACHED Meta and the gateway forgot it.\n"+
			"   A consumer retry with the same key sends AGAIN: a duplicate on the customer's device.\n\n",
			len(c.Confirmed))
		fmt.Fprintf(out, "  %-20s  %-38s  %-22s  %s\n", "consumer", "key", "reserved at (UTC)", "wamid")
		for _, e := range c.Confirmed {
			fmt.Fprintf(out, "  %-20s  %-38s  %-22s  %s\n",
				e.Consumer, e.Key, time.Unix(e.CreatedAt, 0).UTC().Format(time.RFC3339), e.Wamid)
		}
		fmt.Fprintln(out)
	}

	if len(c.Open) > 0 {
		fmt.Fprintf(out, "🟡 OPEN LOST — %d. They reserved and did NOT confirm.\n"+
			"   They may never have gone out, or gone out with the confirm not replicated. There is NO way to tell from here:\n"+
			"   only Meta (or the consumer) can answer. These are NOT counted as a probable duplicate.\n\n",
			len(c.Open))
		fmt.Fprintf(out, "  %-20s  %-38s  %s\n", "consumer", "key", "reserved at (UTC)")
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
