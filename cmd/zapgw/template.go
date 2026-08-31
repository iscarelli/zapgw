// `zapgw template criar` — registers a message template WITHOUT assembling
// a curl with a token by hand (T-036).
//
// WHY THIS EXISTS: `POST /v1/templates` (internal/outbound/templates_handler.go)
// already exists and works, but the only way to use it is to build a POST
// with `Authorization: Bearer <consumer token>` and a JSON body on the
// command line. For the operational case — the owner wanting to put up a
// template RIGHT NOW — that is pure friction, and escaping quotes by hand
// has already cost this project dearly: the `printf %s\n` through ssh+pct
// wrote a literal `n` inside the `verify_token`, and the error only
// surfaced as a 403 on a verification that should have passed
// (docs/ARMADILHAS.md). Every time an operation depends on assembling a
// command with nested quotes, that risk comes back.
//
// THREE DECISIONS, each with its reason:
//
//  1. The COMPONENTS come from a FILE, never from a flag. JSON on the
//     command line is where quotes break, and a template malformed by
//     escaping is indistinguishable from a genuinely wrong template.
//     Reading from a file eliminates the whole class instead of asking for
//     care.
//  2. This command talks to the LOCAL STORE, like the other subcommands —
//     NOT to POST /v1/templates over HTTP. It is an operator tool on the
//     machine, not a second client: this avoids requiring a consumer token
//     for an action that is already privileged by being inside the CT.
//  3. The body validation (the four required fields and the shape of
//     `componentes`) is the SAME as the HTTP route —
//     outbound.CreateTemplateRequest.Validate(), exported for this — not a
//     copy. Two rules diverge on the first change; it is this project's
//     mother trap (docs/ARMADILHAS.md).
//
// VALIDATION RUNS BEFORE ANY NETWORK CALL AND BEFORE OPENING THE STORE: an
// invalid components file cannot spend the instance's quota nor require an
// open database to be rejected.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
	"github.com/iscarelli/zapgw/internal/outbound"
)

func templateCommand(args []string, out io.Writer, env environment) error {
	if len(args) == 0 {
		return errors.New("zapgw: template o que? (criar)")
	}
	switch args[0] {
	case "criar":
		return templateCreate(args[1:], out, env)
	default:
		return fmt.Errorf("zapgw: nao sei fazer %q com um template (conheco: criar)", args[0])
	}
}

func templateCreate(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("template criar", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia dona do template")
	name := fs.String("nome", "", "nome do template, no formato que a Meta exige")
	category := fs.String("categoria", "", "categoria do template (ex.: MARKETING, UTILITY, AUTHENTICATION)")
	language := fs.String("idioma", "", "idioma do template, no formato da Meta (ex.: pt_BR)")
	componentsFileFlag := fs.String("componentes", "",
		"arquivo com a lista JSON de componentes do template. OBRIGATORIO — nao existe flag "+
			"para componentes em linha, porque escapar aspas na linha de comando ja produziu um "+
			"segredo corrompido neste projeto (docs/ARMADILHAS.md)")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	path := strings.TrimSpace(*componentsFileFlag)
	if path == "" {
		return errors.New("zapgw: --componentes e obrigatorio (caminho de um arquivo com a lista JSON de componentes)")
	}
	// A missing-file error NAMES THE PATH: an error that only says "not
	// found" forces whoever ran it to guess which of the several files
	// named in the command was missing.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("zapgw: --componentes: ler %s: %w", path, err)
	}

	// The SAME validation as the HTTP route
	// (internal/outbound/templates_handler.go), called from here — not
	// copied. It rejects an empty JSON list, `{}`, `null` or anything that
	// does not start with `[`, and trims the four text fields. RUNS BEFORE
	// opening the store and BEFORE any call to Meta: an invalid file cannot
	// spend the instance's quota.
	p := outbound.CreateTemplateRequest{
		Instance:   strings.TrimSpace(*slug),
		Name:       strings.TrimSpace(*name),
		Category:   strings.TrimSpace(*category),
		Language:   strings.TrimSpace(*language),
		Components: json.RawMessage(raw),
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("zapgw: %w", err)
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	inst, err := store.FindInstance(p.Instance)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			return fmt.Errorf("zapgw: instancia %q nao existe (use `zapgw instancia listar` para ver os slugs): %w",
				p.Instance, err)
		}
		return fmt.Errorf("zapgw: buscar instancia %q: %w", p.Instance, err)
	}

	// The SAME deadline as the HTTP route (outbound.InstanceDeadline,
	// exported for this): a homegrown one here would diverge from the
	// route on the first change to the default.
	ctx, cancel := context.WithTimeout(context.Background(), outbound.InstanceDeadline(inst))
	defer cancel()

	client := meta.NewClient(nil, graphBase(env))
	created, err := client.CreateTemplate(ctx, inst.WabaID, inst.SendToken, meta.TemplateRequest{
		Name:       p.Name,
		Category:   p.Category,
		Language:   p.Language,
		Components: p.Components,
	})
	if err != nil {
		return fmt.Errorf("zapgw: criar template %q na instancia %q: %w", p.Name, p.Instance, err)
	}

	fmt.Fprintf(out, "template %q criado na instancia %q (id %s", p.Name, inst.Slug, created.ID)
	if created.Status != "" {
		fmt.Fprintf(out, ", status %s", created.Status)
	}
	fmt.Fprintln(out, ").")
	// The SAME warning the HTTP route prints on every successful creation
	// (outbound.WarningTemplatePending) — two surfaces with the same
	// behavior, not two truths.
	fmt.Fprintln(out, outbound.WarningTemplatePending)
	return nil
}
