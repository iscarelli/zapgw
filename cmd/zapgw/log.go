// `zapgw log` — the last 200 transit rows, then it keeps printing what
// arrives, until the operator quits with Ctrl-C (T-093).
//
// WHY THIS EXISTS, and why it is different from `zapgw transito`:
// `transito` answers "did this message pass through here?" — a question
// with a number or a key in hand. The normal question from whoever opens a
// log is different: "what's going through?" — no number at all, just
// wanting to see the traffic. The owner's request, 2026-07-30, in these
// words: "I just want a `zapgw log`, it lists the last 200 rows and keeps
// showing what shows up until I leave the log." NO REQUIRED FLAG AT ALL,
// on purpose — an earlier version of this task had `--instancia`,
// `--telefone`/`--key` as required, and the owner cut them.
//
// 🔴 SHOWS THE COUNTERPARTY — FLIPPED ON THE SAME DAY (T-094). Written
// first: "NEVER PRINTS the HMAC nor invents a phone column... the log
// shows the MOVEMENT, never WHO." Minutes later the owner decided the
// opposite: "you can put the number in, it's not a secret." The
// `contraparte` column (T-094) shows the phone number ALREADY CANONICAL,
// IN PLAIN TEXT — never the HMAC (it no longer exists for this field, see
// internal/config/transit.go). A row recorded BEFORE T-094 prints "—":
// HMAC is one-way, and there is no way to recover the phone number from
// the hash T-091 recorded — it is not a bug, it is the known price of the
// earlier safeguard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// defaultLogRows is the "last 200" from the owner's request — the
// default for `--n`, never required.
const defaultLogRows = 200

// parseLogFlags isolates `zapgw log`'s parsing from any database
// access or the follow context — it is what lets test (c) prove "no flag
// is required" and the `--n`/`--instancia` values WITHOUT opening the
// store or entering the loop that only ends with Ctrl-C.
func parseLogFlags(args []string, out io.Writer) (n int, slug string, keepGoing bool, err error) {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(out)
	nFlag := fs.Int("n", defaultLogRows, "quantas linhas ja gravadas mostrar antes de seguir")
	instance := fs.String("instancia", "", "so uma instancia (default: todas)")
	keepGoing, err = parseFlags(fs, args)
	if err != nil || !keepGoing {
		return 0, "", keepGoing, err
	}
	if *nFlag <= 0 {
		return 0, "", false, fmt.Errorf("zapgw: --n tem de ser maior que zero")
	}
	return *nFlag, strings.TrimSpace(*instance), true, nil
}

func logCommand(args []string, out io.Writer, env environment) error {
	// "clear" is POSITIONAL, right after "log" (the owner's request,
	// 2026-07-30, T-096) — it is not a flag, so it cannot be confused with
	// the READ flags (--n, --instancia) that `zapgw log` without "clear"
	// already had.
	if len(args) > 0 && args[0] == "clear" {
		return logClearCommand(args[1:], out, env)
	}

	n, slug, keepGoing, err := parseLogFlags(args, out)
	if err != nil || !keepGoing {
		return err
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// Ctrl-C exits CLEANLY: no panic, no stack trace, no half-printed
	// line, status 0. signal.NotifyContext cancels the context on the
	// first os.Interrupt and restores the signal's default behavior if a
	// SECOND one arrives (whoever insists with Ctrl-C can still kill the
	// process).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return runLog(ctx, store, slug, n, out, time.Second)
}

// FIXED widths for `zapgw log`'s columns (T-095). NEVER tabwriter: it
// aligns by BATCH — it can only align what it has already seen BEFORE the
// Flush. This command has always printed in three independent batches
// (the header alone, the initial block of N rows, and each follow
// round — in practice, a single row), so with tabwriter the header never
// matched the body and **every new follow row would misalign from the
// previous ones**: it was not a one-off header bug, it was the wrong tool
// for a log that follows. A fixed width solves it because every row uses
// the SAME constant — it does not need to have seen the other rows to
// align with them.
const (
	logStampWidth = 20 // RFC3339 in UTC — internal/config forces .UTC() on every timestamp read, so it always ends in "Z": constant size, no slack.
	// instancia at 24 is the OWNER'S REQUEST, 2026-07-30: today the
	// longest slug is "tenant-one" (13 characters); he wants slack for
	// a longer name.
	logInstanceWidth    = 24
	logCounterpartWidth = 15 // E.164 with the "55" fits in 13 digits — 2 of slack.
	logDirectionWidth   = 8  // "entrada" (7) is the longest value today — 1 of slack.
	logTypeWidth        = 10 // "mensagem" (8) is the longest value today, but "template" (8) is nearly as long — 2 of slack, no more.
	// "desfecho" is the LAST column and takes NO fixed width — see the
	// comment in printLogRows about why nothing here truncates.
)

// logColumnSeparator is a literal BETWEEN columns, outside any of their
// widths — the owner's adjustment on top of this task's first version,
// which only gave the timestamp a width (20) and left the next column
// glued on when the value filled the entire width (RFC3339 always fills
// the timestamp's 20, so it ALWAYS glued).
//
// 🔴 WHY A SEPARATOR AND NOT "width+1", which looks like the obvious fix:
// with width+1 the problem just moves, it does not disappear — it comes
// back on its own the day another field also fills its entire column. And
// that is not hypothetical, it is already close to happening today:
// `tipo` has width 10 and "template" (8) already uses almost all of it;
// `contraparte` has 15 and an E.164 with "55" reaches 13; and the owner
// himself asked for 24 on `instancia` knowing a slug can grow. With a
// separator, gluing is IMPOSSIBLE by construction, because the separator
// does not depend on whether the value fits the width or not — it is
// printed always, unconditionally, between one column and the next. With
// width+1, gluing only becomes unlikely UNTIL someone writes a value the
// exact size of the new width — the same defect, deferred.
const logColumnSeparator = "  "

// logRowFormat is the ONLY fmt used both by the header
// (printLogHeader) and by each data row (printLogRows) —
// header and body CANNOT have their own format, otherwise the problem
// this task solved comes back. The separator goes between EVERY pair of
// columns, including before the last one (desfecho) — it also needs 2
// spaces of distance from the previous field, it just carries no fixed
// width OF ITS OWN because it is the last one.
const logRowFormat = "%-*s" + logColumnSeparator +
	"%-*s" + logColumnSeparator +
	"%-*s" + logColumnSeparator +
	"%-*s" + logColumnSeparator +
	"%-*s" + logColumnSeparator +
	"%s\n"

// printLogHeader writes the header using the SAME widths and the
// SAME format as the body (logRowFormat) — it is what guarantees
// each field's column starts at the same position in the header and in
// every data row, even having been written at different moments (and even
// different calls).
func printLogHeader(out io.Writer) error {
	_, err := fmt.Fprintf(out, logRowFormat,
		logStampWidth, "carimbo",
		logInstanceWidth, "instancia",
		logCounterpartWidth, "contraparte",
		logDirectionWidth, "direcao",
		logTypeWidth, "tipo",
		"desfecho")
	return err
}

// runLog prints the last `n` recorded rows and keeps printing what
// arrives, until `ctx` is canceled. It receives the loop interval as a
// parameter so the follow test (b) does not depend on a wall-clock
// second.
func runLog(ctx context.Context, store *config.Store, slug string, n int, out io.Writer, interval time.Duration) error {
	if err := printLogHeader(out); err != nil {
		return err
	}

	initial, err := store.LastLogLines(slug, n)
	if err != nil {
		return fmt.Errorf("zapgw: log: %w", err)
	}
	var lastRowid int64
	if len(initial) > 0 {
		if err := printLogRows(out, initial); err != nil {
			return err
		}
		lastRowid = initial[len(initial)-1].Rowid
	}

	// FOLLOW: a SHORT read loop (WHERE rowid > ?), with no open
	// transaction. The database is in WAL (internal/config/store.go,
	// OpenStore), so reading concurrently with the service is safe —
	// `zapgw estado` already does this.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// 🔴 T-096: `log clear` may have deleted the highest rowid —
			// SQLite REUSES rowids (the `transito` table does not declare
			// AUTOINCREMENT, see transitSchema in
			// internal/config/transit.go), so the next row recorded can
			// be born with rowid <= lastRowid. Without this reset,
			// `LogLinesSince` (which filters `rowid > lastRowid`)
			// would never find anything again and the follow would go
			// BLIND FOREVER, silently.
			//
			// `largest-1`, NEVER `largest`: between `HighestLogRowid` and
			// `LogLinesSince` below there is no transaction — the new
			// row itself may have already been recorded by the time we
			// read `largest` (the clear's return and the following write
			// can land on the SAME tick). If `largest` already IS the rowid
			// of that new row and the cursor were set to exactly `largest`,
			// the next filter (`rowid > cursor`) would exclude that VERY
			// SAME row — the defect this fix exists to close, just
			// deferred by one tick. `largest-1` keeps the comparison
			// inclusive on the side that matters (`rowid > largest-1` <=>
			// `rowid >= largest`), while never returning a negative rowid
			// (SQLite never uses rowid <= 0).
			//
			// 🔴 SIDE EFFECT ACCEPTED, DELIBERATELY: `largest-1` can REPEAT a
			// row already printed. Scenario: rowids 1..10 already shown
			// (lastRowid=10), a `log clear --telefone` deletes 8, 9 and
			// 10 (the most recent) without touching earlier ones —
			// `largest` becomes 7, the cursor goes back to 6, and the next
			// `rowid > 6` returns row 7 AGAIN, which had already appeared
			// on screen. This is NOT a bug: it is this fix's trade-off,
			// and it is the right one. Silently losing a row is the
			// defect this log cannot have (it is the reason this function
			// exists); repeating a row is cosmetic, limited to ONE row,
			// ONE time, and only after a clear that removed precisely the
			// most recent ones. "Fixing" this by switching back to
			// `largest` reopens the blindness this block exists to close —
			// do not switch it back.
			largest, err := store.HighestLogRowid(slug)
			if err != nil {
				return fmt.Errorf("zapgw: log: %w", err)
			}
			if largest < lastRowid {
				lastRowid = largest - 1
			}
			newOnes, err := store.LogLinesSince(slug, lastRowid)
			if err != nil {
				return fmt.Errorf("zapgw: log: %w", err)
			}
			if len(newOnes) == 0 {
				continue
			}
			if err := printLogRows(out, newOnes); err != nil {
				return err
			}
			lastRowid = newOnes[len(newOnes)-1].Rowid
		}
	}
}

// printLogRows is the ONLY function that formats a batch of
// config.LogLine for the screen — used both in the initial dump and in
// every follow round, so the two outputs never diverge in format. Writes
// straight to the io.Writer (no buffer of its own, unlike the tabwriter
// this command used before): a fixed width does not need to accumulate
// rows to compute a column, so each row appears on screen as soon as it
// arrives — which is what "keeps showing what arrives" requires.
func printLogRows(out io.Writer, lines []config.LogLine) error {
	for _, l := range lines {
		kind := l.Type
		if kind == "" {
			kind = "(nenhum evento modelado)"
		}
		// "—" for an empty counterparty: an ACCOUNT webhook (never had a
		// counterparty) and a row recorded BEFORE T-094 (one-way HMAC,
		// cannot be recovered) are the two cases — neither one is "no
		// sender" in the sense of an accidental empty value, so they do
		// not show up as a blank field.
		counterpart := l.Counterparty
		if counterpart == "" {
			counterpart = "—"
		}
		// 🔴 DOES NOT TRUNCATE. %-*s NEVER cuts a value wider than the
		// requested width — it only pads with spaces UP TO the width when
		// the value is smaller. A 30-character slug, a longer E.164, or a
		// long desfecho only PUSH that entire row to the right; no other
		// row is affected. Truncating a slug or a phone number would lose
		// the identity of what is being read, and in a log the
		// information is worth more than the alignment (the owner's
		// request, T-095) — if someone wants to "fix" this to avoid
		// misalignment, they are hoping for the wrong answer: the wide row
		// is the accepted price, not a defect.
		if _, err := fmt.Fprintf(out, logRowFormat,
			logStampWidth, l.Stamp.Format(time.RFC3339),
			logInstanceWidth, l.Slug,
			logCounterpartWidth, counterpart,
			logDirectionWidth, l.Direction,
			logTypeWidth, kind,
			l.Outcome); err != nil {
			return err
		}
	}
	return nil
}

// logClearCommand is `zapgw log clear` (T-096, the owner's request,
// 2026-07-30): deletes the TRANSIT log of an entire instance or of a
// phone number — always EXACTLY one of the two, never neither, never
// both.
//
// DELETING IS IRREVERSIBLE, and the two halves have DIFFERENT defenses
// because the BLAST RADIUS is different:
//
//   - `--instancia` deletes an instance's entire log — the same pattern as
//     `instancia remover` (provision.go): `--confirmo <slug>` with the
//     slug RETYPED, never a `-y`. The asymmetry with `instancia pausar`
//     (which confirms nothing) is the information: pausing has an undo,
//     this does not.
//   - `--telefone` requires the SAME pattern (T-114): `--confirmo <number>`
//     with the number RETYPED, IDENTICAL to `--telefone`. Until T-114 this
//     half asked for no confirmation at all — typing the number was
//     already the target —, and that was the INCONSISTENCY, not an
//     isolated absence: the rest of the surface (`instancia remover`,
//     `instancia reabrir-cadastro`, `log clear --instancia`) trains the
//     operator to expect the question, and the one path without it was
//     exactly the one easiest to get wrong by a typo — one wrong number
//     typed once deletes ANOTHER PERSON's history with no chance to check
//     first. The LAST-EIGHT-DIGITS guard (below) covers COLLISION (two
//     subscribers sharing the same ending); `--confirmo` covers the most
//     common error, which is typing the WRONG number from the start —
//     both are necessary, neither replaces the other.
func logClearCommand(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("log clear", flag.ContinueOnError)
	fs.SetOutput(out)
	instance := fs.String("instancia", "", "apaga TODO o log de transito desta instancia. IRREVERSIVEL — exige --confirmo")
	phone := fs.String("telefone", "", "apaga o log de transito deste telefone, em TODAS as instancias. IRREVERSIVEL — exige --confirmo")
	confirm := fs.String("confirmo", "", "digite --instancia (o slug) ou --telefone (o numero) DE NOVO para confirmar. Nao existe -y")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	slug := strings.TrimSpace(*instance)
	number := strings.TrimSpace(*phone)
	if slug == "" && number == "" {
		return errors.New("zapgw: log clear: informe --instancia OU --telefone (exatamente um dos dois)")
	}
	if slug != "" && number != "" {
		return errors.New("zapgw: log clear: --instancia e --telefone sao alternativas — escolha um dos dois")
	}

	// THE CONFIRMATION IS CHECKED BEFORE OPENING THE DATABASE, same
	// pattern as `removeInstance` (provision.go): reject early,
	// without touching anything. Both halves require the SAME pattern
	// since T-114 — neither has a `-y`.
	if slug != "" && strings.TrimSpace(*confirm) != slug {
		return fmt.Errorf("zapgw: apagar o log de transito da instancia %q e IRREVERSIVEL — repita o slug em --confirmo:"+
			"  zapgw log clear --instancia %s --confirmo %s", slug, slug, slug)
	}
	if number != "" && strings.TrimSpace(*confirm) != number {
		return fmt.Errorf("zapgw: apagar o log de transito do telefone %q e IRREVERSIVEL — repita o numero em --confirmo:"+
			"  zapgw log clear --telefone %s --confirmo %s", number, number, number)
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if slug != "" {
		return logClearByInstance(store, slug, out)
	}
	return logClearByPhone(store, number, out)
}

// logClearByInstance deletes an instance's ENTIRE transit log — only
// the log, never the instance nor any other table (that is `instancia
// remover`, provision.go).
func logClearByInstance(store *config.Store, slug string, out io.Writer) error {
	deleted, err := store.ClearInstanceTransit(slug)
	if err != nil {
		return fmt.Errorf("zapgw: log clear --instancia %q: %w", slug, err)
	}
	fmt.Fprintf(out, "log de transito da instancia %q APAGADO (irreversivel): %d linha(s).\n", slug, deleted)
	return nil
}

// logClearByPhone is the half of the task with the trap: before
// deleting anything, it COUNTS how many DISTINCT numbers match the last
// eight digits of what was typed. More than one, it REJECTS — deleting by
// that key would delete another person's history (T-094 matches by last
// eight, and two subscribers with different area codes can share them).
func logClearByPhone(store *config.Store, number string, out io.Writer) error {
	lastEight := meta.LastEightDigits(number)
	if lastEight == "" {
		return fmt.Errorf("zapgw: --telefone %q tem menos de 8 digitos", number)
	}

	numbers, err := store.NumbersForLastEight(lastEight)
	if err != nil {
		return fmt.Errorf("zapgw: log clear --telefone: buscar numeros: %w", err)
	}
	if len(numbers) == 0 {
		fmt.Fprintf(out, "nada encontrado para --telefone %q — nenhuma linha apagada.\n", number)
		return nil
	}
	if len(numbers) > 1 {
		return fmt.Errorf("zapgw: --telefone %q casa com %d numeros DISTINTOS pelos ultimos oito digitos —"+
			" apagar apagaria o historico de mais de uma pessoa, e por isso NADA foi apagado."+
			" digite a forma COMPLETA (com DDI e DDD) para escolher qual: %s",
			number, len(numbers), strings.Join(numbers, ", "))
	}

	deleted, err := store.ClearTransitByPhone(numbers[0])
	if err != nil {
		return fmt.Errorf("zapgw: log clear --telefone %q: %w", number, err)
	}
	if len(deleted) == 0 {
		fmt.Fprintf(out, "nada encontrado para --telefone %q — nenhuma linha apagada.\n", number)
		return nil
	}

	var total int64
	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, a := range deleted {
		fmt.Fprintf(tab, "  %s\t%d\n", a.Slug, a.Rows)
		total += a.Rows
	}
	if err := tab.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "log de transito do telefone %q APAGADO (irreversivel) em %d instancia(s), %d linha(s) no total.\n",
		number, len(deleted), total)
	return nil
}
