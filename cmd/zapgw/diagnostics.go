// `zapgw diagnostico` — answers, READ-ONLY, the Meta panel state questions
// that do NOT show up in traffic (T-109): whose token it is, whether the
// messaging permission was granted, and whether the account is subscribed
// to receive `messages` on the webhook.
//
// NOT THE SAME AS `fumaca`. `zapgw fumaca` SENDS a real message and is the
// ONLY way to activate an instance (smoke.go); this command sends
// nothing, does not change `ativo`, does not write to the database — it
// only asks Meta and prints the verdict.
//
// PORTED from `diag_instagram_meta.py` (donated by consumer-b on
// 2026-07-30, after a night-long hunt) — the reason this command exists is
// NOT convenience, it is SECRECY: that script required pasting the
// PRODUCTION TOKEN into a `.env` next to it to run. 🔴 HERE THE CREDENTIAL
// NEVER LEAVES THE VAULT: `--slug` looks up the instance in the store
// (which already decrypts `token_envio` in memory, like any other command
// in this binary), and nothing — not the whole token, not a prefix, not a
// suffix, not even the length — is PRINTED. Only the verdict of each
// question is printed to screen, in a format pastable into chat (see
// TestDiagnosticInstagramHealthyInstanceAnswersEveryQuestion, in
// diagnostics_test.go, which checks the entire output WITHOUT the token —
// the mutation that prints `inst.SendToken` leaves this test red).
//
// THE THREE TRAPS THE .py ALREADY PAID DEARLY TO LEARN, ported here:
//
//  1. `/me` and `entry[].id` are DIFFERENT id spaces — diverging is
//     EXPECTED, never a problem (see the comment on meta.InstagramAccount).
//  2. `debug_token` DOES NOT WORK for a token born from Instagram Login —
//     this command does not call it (see the header of
//     internal/meta/instagram_diagnostics.go). The permission is tested
//     BY USE: hitting the endpoint that requires it.
//  3. A DM from someone who doesn't follow the account lands in
//     "requests" — question 2 sweeps all four folders, not just the
//     default inbox. 🔴 T-113: this safeguard was NEVER exercised against
//     the case it claims to cover — until 2026-07-31 all FIVE calls
//     always returned the SAME number in production. See
//     meta.MeasuredFolderResult for the mechanism that closes the
//     doubt.
//
// WHAT THIS COMMAND CANNOT ANSWER, AND SAYS OUT LOUD: the tester role in
// the App is an APP-LEVEL question (it requires app_id and an
// administrator token the instance does not hold — only the app_secret,
// used to validate the webhook signature). Comes out as
// `nao_verificavel_daqui`, with the reason — NEVER disappearing from the
// output and NEVER coming out as "ok" by omission: a diagnostic that stays
// silent about what it doesn't know produces a false "all clear" at
// exactly the moment someone is hunting for a problem (T-109, Do item 4).
//
// INSTAGRAM ONLY, ON PURPOSE (T-109, item 7): it is the type that
// motivated the donation and where the panel questions hurt. There is no
// WhatsApp check here — it would be dead vocabulary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

const (
	verdictOK            = "  [ok]"
	verdictError         = "  [ERRO]"
	verdictWarning       = "  [!]"
	verdictNotVerifiable = "  [nao_verificavel_daqui]"
)

// instagramConversationFolders is the display ORDER of the extra folders —
// the SAME list meta.InstagramMessagingPermission sweeps, repeated here
// only so the printing iterates in a stable order (the ByFolder map
// guarantees no order at all).
var instagramConversationFolders = []string{"other", "page_done", "spam", "requests"}

// formatInstagramCount is the ONLY place that decides how a
// conversation count turns into text (T-112). `≥ N (primeira pagina)` when
// Meta signaled `paging.next` (there is more beyond what came back);
// `N conversa(s)` with no marker when not — then N is exact, not a floor.
// NEVER prints a bare `N` when Floor is true: presenting a floor as a total
// is exactly the T-112 defect.
func formatInstagramCount(c meta.ConversationCount) string {
	if c.Floor {
		return fmt.Sprintf("≥ %d conversa(s) (primeira pagina; pode haver mais)", c.N)
	}
	return fmt.Sprintf("%d conversa(s)", c.N)
}

// foldersWithSameNumber detects the symptom measured in production in
// T-112: the responses that came back (default inbox + folders that
// didn't fail) ALL carry the same N. With fewer than two data points the
// comparison says nothing (there is nothing to compare), so it returns
// false.
func foldersWithSameNumber(byFolder map[string]meta.ConversationCount) bool {
	if len(byFolder) < 2 {
		return false
	}
	reference, defined := 0, false
	for _, c := range byFolder {
		if !defined {
			reference, defined = c.N, true
			continue
		}
		if c.N != reference {
			return false
		}
	}
	return true
}

func diagnose(args []string, out io.Writer, env environment) error {
	fs := flag.NewFlagSet("diagnostico", flag.ContinueOnError)
	fs.SetOutput(out)
	slug := fs.String("slug", "", "instancia a diagnosticar. SOMENTE LEITURA. OBRIGATORIO")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug e obrigatorio")
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	inst, err := store.FindInstance(who)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			return fmt.Errorf("zapgw: instancia %q nao existe (use `zapgw instancia listar` para ver os slugs): %w", who, err)
		}
		return fmt.Errorf("zapgw: buscar instancia %q: %w", who, err)
	}

	// "" reads as TypeWhatsApp — the same normalization
	// config.ValidateInstanceType already applies on write (every row
	// written before T-097 has the column empty). INSTAGRAM ONLY in this
	// slice (T-109, item 7): do not invent a WhatsApp check for symmetry.
	kind := inst.Type
	if kind == "" {
		kind = config.TypeWhatsApp
	}
	if kind != config.TypeInstagram {
		return fmt.Errorf("zapgw: diagnostico ainda so cobre instancias --tipo instagram (T-109, item 7) —"+
			" %q e do tipo %q", who, kind)
	}

	// The SAME client and SAME host resolution smoke.go and main.go use
	// for Instagram (instagramRenewalBase) — graph.instagram.com, a
	// DIFFERENT host from the rest of the Graph API (graphBase),
	// injectable to point at a fake server in the test.
	client := meta.NewClient(&http.Client{}, graphBase(env))
	base := instagramRenewalBase(env)

	// ZAPGW_DIAGNOSTIC_PROBE_FOLDER (old name ZAPGW_DIAGNOSTICO_SONDAR_FOLDER
	// — T-214; any non-empty value) turns on the probe from item 1 of T-113
	// — the "exercisable without recompiling" mechanism the task asked for,
	// so the operator on the CT can run it without needing a new binary.
	// Off by default: it is ONE extra request that only matters while
	// MeasuredFolderResult is still FolderUnknown.
	probeRaw, probeOldUsed := config.EnvOrOld(env, envDiagnosticProbeFolderNew, envDiagnosticProbeFolderOld)
	probeFlag := strings.TrimSpace(probeRaw) != ""
	config.WarnOldEnvVar(probeOldUsed && probeFlag, envDiagnosticProbeFolderOld, envDiagnosticProbeFolderNew)

	return diagnoseInstagram(context.Background(), client, base, inst, out, probeFlag)
}

// diagnoseInstagram runs the three VERIFIABLE questions (account,
// permission, webhook subscription) plus the ONE that isn't (tester role),
// and prints the verdict. NEVER prints `inst.SendToken` nor any piece of
// it — only what Meta returned about the ACCOUNT (id, username, type) and
// about the STATE (permission granted or not, subscribed or not).
func diagnoseInstagram(ctx context.Context, client *meta.Client, base string, inst config.Instance, out io.Writer, probeInvalidFolder bool) error {
	fmt.Fprintf(out, "diagnostico do instagram · instancia %q (ig_id %q)\n\n", inst.Slug, inst.IgID)

	var problems []string

	// 1) de quem e este token.
	fmt.Fprintln(out, "1) a conta do token")
	account, err := client.InstagramTokenAccount(ctx, base, inst.SendToken)
	if err != nil {
		fmt.Fprintf(out, "%s nao deu para ler a conta — %v\n", verdictError, err)
		problems = append(problems, "o token nao respondeu; pode estar expirado ou ser de outro App")
	} else {
		accountType := account.AccountType
		if accountType == "" {
			accountType = "(nao informado)"
		}
		fmt.Fprintf(out, "%s @%s · id %s · tipo %s\n", verdictOK, account.Username, account.ID, accountType)
		if account.AccountType != "" && account.AccountType != "BUSINESS" && account.AccountType != "MEDIA_CREATOR" {
			problems = append(problems, fmt.Sprintf("a conta e %s, nao profissional", account.AccountType))
		}
		// 🔴 DO NOT flag divergence — it is EXPECTED. See the comment on
		// meta.InstagramAccount and the Why of T-109 (4 events discarded
		// when this comparison was done backwards, recording the App-scope
		// id instead of entry[].id).
		if inst.IgID != "" && account.ID != "" && account.ID != inst.IgID {
			fmt.Fprintf(out, "%s id do token (escopo do App): %s\n", verdictWarning, account.ID)
			fmt.Fprintf(out, "      ig_id desta instancia (entry[].id do webhook): %s\n", inst.IgID)
			fmt.Fprintln(out, "      divergir e NORMAL — sao espacos de id diferentes. quem vale para o")
			fmt.Fprintln(out, "      roteamento e o entry[].id do webhook, que e o ig_id gravado na instancia.")
		}
	}

	// 2) the messaging permission, tested BY USE — never via debug_token.
	fmt.Fprintln(out, "\n2) a permissao de mensagens — testada pelo uso")
	permission, err := client.InstagramMessagingPermission(ctx, base, inst.SendToken)
	if err != nil {
		var metaError *meta.MetaError
		if errors.As(err, &metaError) && metaError.Class == meta.ClassConfig {
			fmt.Fprintf(out, "%s recusado por PERMISSAO/CREDENCIAL — %v\n", verdictError, err)
			problems = append(problems, "o token nao tem `instagram_business_manage_messages` concedida: gere o "+
				"token de novo e, na tela de autorizacao, confirme que a permissao de mensagens aparece")
		} else {
			fmt.Fprintf(out, "%s nao deu para listar conversas — %v\n", verdictWarning, err)
			fmt.Fprintln(out, "      (nao conclui nada sobre a permissao: o erro nao e de permissao)")
		}
	} else {
		fmt.Fprintf(out, "%s permissao CONCEDIDA (o endpoint de conversas respondeu)\n", verdictOK)
		fmt.Fprintf(out, "      conversas na caixa padrao: %s\n", formatInstagramCount(permission.ByFolder[""]))
		for _, folder := range instagramConversationFolders {
			c, has := permission.ByFolder[folder]
			if !has {
				continue // folder failed (best effort) — no number, no line
			}
			fmt.Fprintf(out, "%s pasta %q: %s\n", verdictOK, folder, formatInstagramCount(c))
		}
		// T-113/T-114: this warning's text is PARAMETERIZED by
		// meta.MeasuredFolderResult. With FolderIgnored the sweep of
		// the extra folders DOESN'T EVEN RUN
		// (meta.InstagramMessagingPermission stops calling the four
		// folders as soon as the value becomes FolderIgnored) — only the
		// default-inbox data remains, and there is nothing left to COMPARE
		// live. That is why this branch is UNCONDITIONAL: the claim comes
		// from the MEASUREMENT (T-114), not from a comparison between
		// folders that, with FolderIgnored, would never have the two data
		// points needed to happen — gating it behind foldersWithSameNumber
		// (which requires at least two folders answered) would make the
		// warning NEVER appear, the hedge this task exists to close.
		if meta.MeasuredFolderResult == meta.FolderIgnored {
			fmt.Fprintln(out, verdictOK+" a segregacao por pasta NAO E OBSERVAVEL por esta API (medido, T-113/T-114):")
			fmt.Fprintln(out, "      o parametro `folder` de /me/conversations e ignorado com token de Instagram")
			fmt.Fprintln(out, "      Login. Nao use estes numeros para dizer em que gaveta uma DM esta.")
		} else if foldersWithSameNumber(permission.ByFolder) {
			switch meta.MeasuredFolderResult {
			case meta.FolderHonored:
				fmt.Fprintln(out, verdictWarning+" todas as pastas que responderam trouxeram o MESMO numero. O filtro")
				fmt.Fprintln(out, "      `folder` EXISTE (medido, T-113) — a causa mais provavel e' o TETO DE PAGINA")
				fmt.Fprintln(out, "      mascarando a diferenca real entre pastas.")
			default:
				fmt.Fprintln(out, verdictWarning+" todas as pastas que responderam trouxeram o MESMO numero — o filtro")
				fmt.Fprintln(out, "      `folder` pode nao estar sendo aplicado por este endpoint. NAO conclua daqui")
				fmt.Fprintln(out, "      em que gaveta a DM esta (medicao em producao ainda pendente — ver T-113;")
				fmt.Fprintln(out, "      rode com ZAPGW_DIAGNOSTIC_PROBE_FOLDER=1 para medir).")
			}
		}
		if permission.TotalConversations == 0 {
			fmt.Fprintln(out, "      nenhuma conversa em pasta nenhuma — isso reforça que a Meta nao esta")
			fmt.Fprintln(out, "      expondo trafego desta conta ao App (ou ainda nao chegou DM nenhuma).")
		}
	}

	// 3) the account's webhook subscription.
	fmt.Fprintln(out, "\n3) a inscricao de webhook da conta")
	fields, err := client.InstagramWebhookSubscription(ctx, base, inst.SendToken)
	if err != nil {
		fmt.Fprintf(out, "%s nao deu para ler a inscricao — %v\n", verdictError, err)
	} else {
		label := strings.Join(fields, ", ")
		if label == "" {
			label = "(nenhum campo inscrito)"
		}
		fmt.Fprintf(out, "%s inscricoes: %s\n", verdictOK, label)
		if !slices.Contains(fields, "messages") {
			problems = append(problems, "a conta nao esta inscrita no campo `messages`")
		}
	}

	// 4) tester role in the App — STRUCTURALLY unverifiable with what the
	// instance holds (T-109, Do item 4): it requires app_id and an
	// ADMINISTRATOR token for the App, and the instance only has the
	// app_secret (used only to validate the webhook signature). Makes NO
	// call at all — the reason does not depend on the network.
	fmt.Fprintln(out, "\n4) o papel de testador no App")
	fmt.Fprintf(out, "%s este gateway nao guarda o app_id nem um token administrador do App para esta "+
		"instancia — so o app_secret, usado para validar a assinatura do webhook. confira manualmente no "+
		"painel da Meta (App > Instagram > Funcoes > Testadores do Instagram).\n", verdictNotVerifiable)

	// 5) the probe from item 1 of T-113 — ONLY runs when requested
	// (ZAPGW_DIAGNOSTIC_PROBE_FOLDER, old name ZAPGW_DIAGNOSTICO_SONDAR_FOLDER
	// — T-214; read in `diagnose` and passed here as `probeInvalidFolder`).
	// It is the MEASUREMENT that closes the question: a folder Meta never
	// documented proves, by itself, which of the two hypotheses holds
	// (meta.FolderFilterResult).
	if probeInvalidFolder {
		fmt.Fprintln(out, "\n5) sonda do parametro `folder` — medicao pedida (T-113, ZAPGW_DIAGNOSTIC_PROBE_FOLDER)")
		probe, err := client.ProbeInvalidInstagramFolder(ctx, base, inst.SendToken)
		if err != nil {
			fmt.Fprintf(out, "%s a Meta RECUSOU um folder invalido — %v\n", verdictOK, err)
			fmt.Fprintln(out, "      isso PROVA que o filtro de pasta foi RESPEITADO: o parametro `folder`")
			fmt.Fprintln(out, "      EXISTE e e' aplicado. Cole esta linha inteira no relatorio da T-113.")
		} else {
			fmt.Fprintf(out, "%s a Meta ACEITOU o folder invalido e devolveu %s\n", verdictWarning, formatInstagramCount(probe))
			fmt.Fprintln(out, "      compare com \"conversas na caixa padrao\" no item 2, acima: numero IGUAL PROVA")
			fmt.Fprintln(out, "      que o filtro de pasta foi IGNORADO: o parametro `folder` nao e' aplicado.")
			fmt.Fprintln(out, "      Cole as duas linhas no relatorio da T-113.")
		}
	}

	fmt.Fprintln(out, "\n"+strings.Repeat("=", 70))
	if len(problems) > 0 {
		fmt.Fprintln(out, "o que esta faltando:")
		for _, p := range problems {
			fmt.Fprintf(out, "  · %s\n", p)
		}
	} else {
		fmt.Fprintln(out, "tudo o que da para ver DAQUI esta em ordem — o item 4 continua manual (ver acima).")
	}
	fmt.Fprintln(out, strings.Repeat("=", 70))

	return nil
}
