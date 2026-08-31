// `zapgw transito` — answers "did this message pass through here?" (T-091, T-094).
//
// WHY THIS EXISTS: the owner's request, 2026-07-29, after needing to
// identify a message from a specific number and the gateway having no way
// to answer. The counters (`zapgw estado`) say "how many today?"; no field
// of this gateway said "did this message pass?". See
// internal/config/transit.go for the full design (retention, the exact
// fields, what stays HMAC).
//
// WHY IT IS CLI AND NOT AN HTTP ROUTE: the consumer already has the `cru`
// through obligation 1 of the contract — a route would give them something
// WORSE than what they already have (an index by third parties' phone
// numbers), and the gateway would then have to maintain that route
// forever. The question this command answers is the OPERATOR's ("did it
// pass through here?"), not the integrator's.
//
// THE OUTPUT STILL DOES NOT SHOW THE PHONE NUMBER NOR THE Idempotency-Key —
// and that did NOT change with T-094: whoever calls `--telefone` already
// has the number IN HAND, so echoing it back adds no information
// (`zapgw log`, which answers "what's going through?" WITHOUT any number in
// hand, is the one that got the `contraparte` column). What DID change is
// how the `--telefone` search finds the row: until T-091 it was HMAC of the
// CANONICAL number (only the exact form found it); since T-094 (the
// owner's decision, "you can put the number in, it's not a secret") it is
// by the LAST EIGHT DIGITS in plain text (meta.LastEightDigits) — the
// four spellings of the same subscriber find the SAME row.
package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// parseSince accepts RFC3339 (the same format this gateway uses
// everywhere, docs/CONTRATO-CONSUMIDOR.md) OR just the date
// (`2006-01-02`), for whoever is typing at the terminal and is not going
// to write hour and timezone by hand. Empty returns the ZERO time.Time,
// which SearchTransit reads as "since forever".
func parseSince(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("zapgw: --desde %q nao e RFC3339 nem AAAA-MM-DD", raw)
}

func transitCommand(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("transito", flag.ContinueOnError)
	fs.SetOutput(out)
	instance := fs.String("instancia", "", "slug da instancia (obrigatorio)")
	phone := fs.String("telefone", "", "o numero a procurar, em qualquer grafia (com --chave, escolha um dos dois)")
	rawKey := fs.String("chave", "", "a Idempotency-Key do ENVIO a procurar (com --telefone, escolha um dos dois)")
	rawSince := fs.String("desde", "", "so linhas a partir daqui — RFC3339 ou AAAA-MM-DD (default: desde sempre)")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	slug := strings.TrimSpace(*instance)
	if slug == "" {
		return fmt.Errorf("zapgw: --instancia e obrigatorio")
	}
	number := strings.TrimSpace(*phone)
	key := strings.TrimSpace(*rawKey)
	// EXACTLY ONE of the two, never neither and never both: they are two
	// DIFFERENT indexes on the same table (counterparty and correlation),
	// and allowing both together would force deciding which prevails — a
	// decision no one asked for and that would only confuse whoever reads
	// the output.
	if number == "" && key == "" {
		return fmt.Errorf("zapgw: informe --telefone ou --chave")
	}
	if number != "" && key != "" {
		return fmt.Errorf("zapgw: --telefone e --chave sao alternativas — escolha um dos dois")
	}
	since, err := parseSince(*rawSince)
	if err != nil {
		return err
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	var lines []config.TransitLine
	if number != "" {
		// THE LAST EIGHT DIGITS (internal/meta/phone.go), no longer HMAC of
		// the canonical number (T-091): whoever asks can type the number in
		// ANY spelling — with or without "55", with or without the ninth
		// digit — and all four have to find the SAME row — see Verify (a)
		// of T-094.
		lastEight := meta.LastEightDigits(number)
		if lastEight == "" {
			return fmt.Errorf("zapgw: --telefone %q tem menos de 8 digitos", number)
		}
		lines, err = store.SearchTransit(slug, lastEight, since)
		if err != nil {
			return fmt.Errorf("zapgw: buscar transito por telefone: %w", err)
		}
	} else {
		// --key: the search on the OUTBOUND side — the Idempotency-Key
		// the consumer used in POST /v1/messages. Store.HMACCorrelation
		// stays HMAC even after T-094 (the owner's decision was about the
		// PHONE NUMBER, not about this field): whoever knows the key
		// computes the same HMAC and finds it; the plain value never enters
		// the database nor leaves this screen.
		hmac := store.HMACCorrelation(key)
		lines, err = store.SearchTransitByCorrelation(slug, hmac, since)
		if err != nil {
			return fmt.Errorf("zapgw: buscar transito por chave: %w", err)
		}
	}

	if len(lines) == 0 {
		fmt.Fprintf(out, "nada encontrado na instancia %q.\n", slug)
		return nil
	}

	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tab, "carimbo\tdirecao\ttipo\twamid\tdesfecho\n")
	for _, l := range lines {
		kind := l.Type
		if kind == "" {
			kind = "(nenhum evento modelado)"
		}
		// "—" for an empty wamid: inbound, a send that failed before Meta
		// answered, or a row predating T-094 are the cases — the same
		// treatment cmd/zapgw/log.go already gives an empty contraparte
		// (T-128).
		wamid := l.Wamid
		if wamid == "" {
			wamid = "—"
		}
		fmt.Fprintf(tab, "%s\t%s\t%s\t%s\t%s\n", l.Stamp.Format(time.RFC3339), l.Direction, kind, wamid, l.Outcome)
	}
	return tab.Flush()
}
