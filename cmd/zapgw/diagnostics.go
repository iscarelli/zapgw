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
// `not_verifiable_here`, with the reason — NEVER disappearing from the
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
	verdictError         = "  [ERROR]"
	verdictWarning       = "  [!]"
	verdictNotVerifiable = "  [not_verifiable_here]"
)

// instagramConversationFolders is the display ORDER of the extra folders —
// the SAME list meta.InstagramMessagingPermission sweeps, repeated here
// only so the printing iterates in a stable order (the ByFolder map
// guarantees no order at all).
var instagramConversationFolders = []string{"other", "page_done", "spam", "requests"}

// formatInstagramCount is the ONLY place that decides how a
// conversation count turns into text (T-112). `≥ N (first page)` when
// Meta signaled `paging.next` (there is more beyond what came back);
// `N conversation(s)` with no marker when not — then N is exact, not a floor.
// NEVER prints a bare `N` when Floor is true: presenting a floor as a total
// is exactly the T-112 defect.
func formatInstagramCount(c meta.ConversationCount) string {
	if c.Floor {
		return fmt.Sprintf("≥ %d conversation(s) (first page; there may be more)", c.N)
	}
	return fmt.Sprintf("%d conversation(s)", c.N)
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
	slug := fs.String("slug", "", "instance to diagnose. READ-ONLY. REQUIRED")
	if keepGoing, err := parseFlags(fs, args); err != nil || !keepGoing {
		return err
	}

	who := strings.TrimSpace(*slug)
	if who == "" {
		return errors.New("zapgw: --slug is required")
	}

	store, err := openStore(env)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	inst, err := store.FindInstance(who)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			return fmt.Errorf("zapgw: instance %q does not exist (use `zapgw instancia listar` to see the slugs): %w", who, err)
		}
		return fmt.Errorf("zapgw: look up instance %q: %w", who, err)
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
		return fmt.Errorf("zapgw: diagnostico still only covers --tipo instagram instances (T-109, item 7) —"+
			" %q is of type %q", who, kind)
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
	fmt.Fprintf(out, "instagram diagnostic · instance %q (ig_id %q)\n\n", inst.Slug, inst.IgID)

	var problems []string

	// 1) whose token this is.
	fmt.Fprintln(out, "1) the token's account")
	account, err := client.InstagramTokenAccount(ctx, base, inst.SendToken)
	if err != nil {
		fmt.Fprintf(out, "%s could not read the account — %v\n", verdictError, err)
		problems = append(problems, "the token did not respond; it may be expired or from another App")
	} else {
		accountType := account.AccountType
		if accountType == "" {
			accountType = "(not informed)"
		}
		fmt.Fprintf(out, "%s @%s · id %s · type %s\n", verdictOK, account.Username, account.ID, accountType)
		if account.AccountType != "" && account.AccountType != "BUSINESS" && account.AccountType != "MEDIA_CREATOR" {
			problems = append(problems, fmt.Sprintf("the account is %s, not professional", account.AccountType))
		}
		// 🔴 DO NOT flag divergence — it is EXPECTED. See the comment on
		// meta.InstagramAccount and the Why of T-109 (4 events discarded
		// when this comparison was done backwards, recording the App-scope
		// id instead of entry[].id).
		if inst.IgID != "" && account.ID != "" && account.ID != inst.IgID {
			fmt.Fprintf(out, "%s token id (App scope): %s\n", verdictWarning, account.ID)
			fmt.Fprintf(out, "      this instance's ig_id (webhook entry[].id): %s\n", inst.IgID)
			fmt.Fprintln(out, "      diverging is NORMAL — they are different id spaces. what matters for")
			fmt.Fprintln(out, "      routing is the webhook's entry[].id, which is the ig_id recorded on the instance.")
		}
	}

	// 2) the messaging permission, tested BY USE — never via debug_token.
	fmt.Fprintln(out, "\n2) the messaging permission — tested by use")
	permission, err := client.InstagramMessagingPermission(ctx, base, inst.SendToken)
	if err != nil {
		var metaError *meta.MetaError
		if errors.As(err, &metaError) && metaError.Class == meta.ClassConfig {
			fmt.Fprintf(out, "%s refused by PERMISSION/CREDENTIAL — %v\n", verdictError, err)
			problems = append(problems, "the token was not granted `instagram_business_manage_messages`: generate the "+
				"token again and, on the authorization screen, confirm the messaging permission appears")
		} else {
			fmt.Fprintf(out, "%s could not list conversations — %v\n", verdictWarning, err)
			fmt.Fprintln(out, "      (this concludes nothing about the permission: the error is not a permission error)")
		}
	} else {
		fmt.Fprintf(out, "%s permission GRANTED (the conversations endpoint responded)\n", verdictOK)
		fmt.Fprintf(out, "      conversations in the default inbox: %s\n", formatInstagramCount(permission.ByFolder[""]))
		for _, folder := range instagramConversationFolders {
			c, has := permission.ByFolder[folder]
			if !has {
				continue // folder failed (best effort) — no number, no line
			}
			fmt.Fprintf(out, "%s folder %q: %s\n", verdictOK, folder, formatInstagramCount(c))
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
			fmt.Fprintln(out, verdictOK+" per-folder segregation is NOT OBSERVABLE through this API (measured, T-113/T-114):")
			fmt.Fprintln(out, "      the `folder` parameter of /me/conversations is ignored with an Instagram")
			fmt.Fprintln(out, "      Login token. Do not use these numbers to say which drawer a DM is in.")
		} else if foldersWithSameNumber(permission.ByFolder) {
			switch meta.MeasuredFolderResult {
			case meta.FolderHonored:
				fmt.Fprintln(out, verdictWarning+" every folder that answered came back with the SAME number. The")
				fmt.Fprintln(out, "      `folder` filter EXISTS (measured, T-113) — the most likely cause is the PAGE CEILING")
				fmt.Fprintln(out, "      masking the real difference between folders.")
			default:
				fmt.Fprintln(out, verdictWarning+" every folder that answered came back with the SAME number — the")
				fmt.Fprintln(out, "      `folder` filter may not be applied by this endpoint. DO NOT conclude from this")
				fmt.Fprintln(out, "      which drawer the DM is in (measurement in production still pending — see T-113;")
				fmt.Fprintln(out, "      run with ZAPGW_DIAGNOSTIC_PROBE_FOLDER=1 to measure).")
			}
		}
		if permission.TotalConversations == 0 {
			fmt.Fprintln(out, "      no conversation in any folder — this reinforces that Meta is not")
			fmt.Fprintln(out, "      exposing this account's traffic to the App (or no DM has arrived yet).")
		}
	}

	// 3) the account's webhook subscription.
	fmt.Fprintln(out, "\n3) the account's webhook subscription")
	fields, err := client.InstagramWebhookSubscription(ctx, base, inst.SendToken)
	if err != nil {
		fmt.Fprintf(out, "%s could not read the subscription — %v\n", verdictError, err)
	} else {
		label := strings.Join(fields, ", ")
		if label == "" {
			label = "(no field subscribed)"
		}
		fmt.Fprintf(out, "%s subscriptions: %s\n", verdictOK, label)
		if !slices.Contains(fields, "messages") {
			problems = append(problems, "the account is not subscribed to the `messages` field")
		}
	}

	// 4) tester role in the App — STRUCTURALLY unverifiable with what the
	// instance holds (T-109, Do item 4): it requires app_id and an
	// ADMINISTRATOR token for the App, and the instance only has the
	// app_secret (used only to validate the webhook signature). Makes NO
	// call at all — the reason does not depend on the network.
	fmt.Fprintln(out, "\n4) the tester role in the App")
	fmt.Fprintf(out, "%s this gateway does not hold the app_id nor an administrator token for the App for this "+
		"instance — only the app_secret, used to validate the webhook signature. check manually in the "+
		"Meta panel (App > Instagram > Roles > Instagram Testers).\n", verdictNotVerifiable)

	// 5) the probe from item 1 of T-113 — ONLY runs when requested
	// (ZAPGW_DIAGNOSTIC_PROBE_FOLDER, old name ZAPGW_DIAGNOSTICO_SONDAR_FOLDER
	// — T-214; read in `diagnose` and passed here as `probeInvalidFolder`).
	// It is the MEASUREMENT that closes the question: a folder Meta never
	// documented proves, by itself, which of the two hypotheses holds
	// (meta.FolderFilterResult).
	if probeInvalidFolder {
		fmt.Fprintln(out, "\n5) `folder` parameter probe — measurement requested (T-113, ZAPGW_DIAGNOSTIC_PROBE_FOLDER)")
		probe, err := client.ProbeInvalidInstagramFolder(ctx, base, inst.SendToken)
		if err != nil {
			fmt.Fprintf(out, "%s Meta REFUSED an invalid folder — %v\n", verdictOK, err)
			fmt.Fprintln(out, "      this PROVES the folder filter was HONORED: the `folder` parameter")
			fmt.Fprintln(out, "      EXISTS and is applied. Paste this whole line into T-113's report.")
		} else {
			fmt.Fprintf(out, "%s Meta ACCEPTED the invalid folder and returned %s\n", verdictWarning, formatInstagramCount(probe))
			fmt.Fprintln(out, "      compare with \"conversations in the default inbox\" in item 2, above: an EQUAL number PROVES")
			fmt.Fprintln(out, "      that the folder filter was IGNORED: the `folder` parameter is not applied.")
			fmt.Fprintln(out, "      Paste both lines into T-113's report.")
		}
	}

	fmt.Fprintln(out, "\n"+strings.Repeat("=", 70))
	if len(problems) > 0 {
		fmt.Fprintln(out, "what is missing:")
		for _, p := range problems {
			fmt.Fprintf(out, "  · %s\n", p)
		}
	} else {
		fmt.Fprintln(out, "everything that can be seen FROM HERE is in order — item 4 remains manual (see above).")
	}
	fmt.Fprintln(out, strings.Repeat("=", 70))

	return nil
}
