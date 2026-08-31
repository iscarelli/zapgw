// Instagram diagnostics (T-109) — the READ calls that answer the state
// questions from the Meta dashboard that don't show up in traffic: whose
// token it is, whether the messaging permission was granted, and whether
// the account is subscribed to receive `messages` in the webhook.
//
// PORTED from `diag_instagram_meta.py` (repo root, donated by consumer-b on
// 2026-07-30) into the gateway — the reason is NOT comfort, it's SECRECY:
// the script required pasting the production token into a `.env` next to it
// to run. Here the credential never leaves the vault
// (cmd/zapgw/diagnostico.go reads it from the store by slug); these
// functions only receive the token already in memory, call Meta, and return
// structured data — they NEVER print anything, the command that calls them
// decides what becomes screen output.
//
// READ ONLY: no function in this file writes anything, to Meta or to the
// gateway. THERE IS NO FOURTH FUNCTION FOR `debug_token` — owner's decision
// (docs/TASKS.md, T-109, item 3b): that endpoint REJECTS a token born from
// Instagram login on both hosts (graph.instagram.com and
// graph.facebook.com), with different errors for each app id/secret
// combination — see the .py's header for the full account. Asking doesn't
// work; USING works — that's why DiagnosticoPermissaoInstagram hits the
// endpoint the permission actually gates, instead of inspecting the token.
package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// InstagramAccount is the response of `GET /me` on the Instagram host: whose
// token it is.
//
// 🔴 THE ID HERE IS THE ID IN THE APP'S SCOPE, NOT THE WEBHOOK's
// `entry[].id` — they are TWO DIFFERENT id spaces, and confusing them has
// already cost this project (docs/TASKS.md, T-109, Why: 4 events discarded
// when the App-scoped id was recorded in place of entry[].id). Whoever
// assembles the verdict (cmd/zapgw/diagnostico.go) has to say, in big
// letters, that DIVERGING from the instance's ig_id is EXPECTED — never
// flag the divergence as a problem.
type InstagramAccount struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	AccountType string `json:"account_type"`
}

// InstagramTokenAccount answers "whose token is this" — `GET /me`, fields
// id/username/account_type. It's the SAME call as step 1 of
// diag_instagram_meta.py.
func (c *Client) InstagramTokenAccount(ctx context.Context, base, token string) (InstagramAccount, error) {
	raw, err := c.readInstagramGraph(ctx, base, "me",
		url.Values{"fields": {"id,username,account_type"}}, token)
	if err != nil {
		return InstagramAccount{}, err
	}
	var account InstagramAccount
	if err := json.Unmarshal(raw, &account); err != nil {
		return InstagramAccount{}, fmt.Errorf("meta: corpo de /me (instagram) nao entendido: %w", err)
	}
	return account, nil
}

// diagnosticConversationCap is the `limit` requested from
// `/me/conversations` — T-112.
//
// 🔴 WHAT THIS IS NOT: full pagination (see the pattern in ListTemplates,
// templates.go, which follows `paging.next` until it disappears).
// DELIBERATE decision, not an oversight: this command is a WORKBENCH tool —
// the operator is in front of the screen, running it manually, and a folder
// can have hundreds of conversations. Paginating all FIVE calls (default
// inbox + four folders) to the end would multiply the wait by however many
// pages each one has, for a number the permission verdict (what this
// command exists to prove) doesn't need. A single page, larger than the
// Graph API's default of 25, is enough for the question that matters: "do
// the numbers DIFFER between folders?"
//
// 100 is 4x the default — headroom so that an account with a few dozen
// conversations per folder comes out with an EXACT number (no floor), while
// still being a single call per folder.
const diagnosticConversationCap = 100

// ConversationCount is the result of ONE call to `/me/conversations`.
//
// Floor is true when the response brought a NON-empty `paging.next` — there
// ARE MORE items beyond the N that came in this single page, and N is a
// FLOOR ("at least N"), NEVER the total. Floor is false when `paging.next`
// came back empty: N is the EXACT count (the Graph API has nothing left to
// paginate for this folder/token/query).
//
// WHY `paging.next`, AND NOT `N == diagnosticConversationCap`: comparing
// against the REQUESTED limit would assume Meta always honors the query's
// `limit` — if it ever answers a smaller page than requested even when
// there's more (a pagination behavior this package hasn't verified against
// the source for this endpoint), that comparison would say "exact" for a
// number that's still a floor. `paging.next` is the signal META ITSELF
// gives for "there's more", and it's the same signal templates.go already
// uses for the same question.
type ConversationCount struct {
	N     int
	Floor bool
}

// FolderFilterResult is the MEASURED answer to the question T-113
// asked: does the Graph API actually apply the `folder` parameter of
// `/me/conversations` with an Instagram Login token, or does it ignore it?
// Until 2026-07-31 the FIVE calls (default inbox + four folders) ALWAYS
// returned the SAME number in production, with TWO different page ceilings
// (25 in v0.42.0, 50 in v0.42.1 with `limit=100` requested) — which was
// indistinguishable from "ignored" while the page ceiling could ALSO be
// masking the difference. `ProbeInvalidInstagramFolder`, below, separated
// the two hypotheses with a `folder` Meta never documented: an error would
// prove the filter exists; the same list as the default inbox would prove
// it's ignored. ANSWERED on 2026-07-31 15:54 -03 (T-114): Meta accepted the
// invalid folder and returned the same list — it IGNORES the parameter. See
// `MeasuredFolderResult`, below.
type FolderFilterResult int

const (
	// FolderUnknown was the production value until T-114's
	// measurement (2026-07-31): the question had been formulated and the
	// measurement mechanism existed (ProbeInvalidInstagramFolder), but
	// no one had exercised the real Meta with it yet. While this value
	// held, the four-folder sweep always ran and the warning
	// `cmd/zapgw/diagnostico.go` prints stayed CONDITIONAL, never
	// affirmative. It still exists for the tests that prove the
	// BEFORE-the-measurement behavior (save/restore via
	// `withFolderResult`) and for the day another endpoint needs the
	// same open question.
	FolderUnknown FolderFilterResult = iota
	// FolderIgnored: a `folder=<invalid value>` returned the SAME list
	// as the default inbox instead of an error — proof the parameter
	// filters nothing on this endpoint, with this token type.
	FolderIgnored
	// FolderHonored: Meta REJECTED the invalid folder — the filter
	// EXISTS, and the identical number measured in earlier rounds was
	// the PAGE CEILING (25, then 50) masking the real difference between
	// folders.
	FolderHonored
)

// MeasuredFolderResult is the SINGLE DECISION POINT of this feature
// (T-113, docs/TASKS.md, Do item 2) — changing the BEHAVIOR of the folder
// sweep and of the warning `cmd/zapgw/diagnostico.go` prints means changing
// THIS LINE, and only it, to the value the measurement confirms:
//
//   - FolderIgnored → InstagramMessagingPermission stops sweeping the
//     four extra folders on its own (the `if` right below already checks
//     this value): four requests that inform nothing are WORSE than
//     absence — they produce four `[ok]`s the operator reads as four
//     measurements. The warning in diagnostico.go becomes an ASSERTION:
//     per-folder segregation is not observable through this API.
//   - FolderHonored → the sweep continues, BUT that alone is NOT
//     enough: `countInstagramConversations` still reads a SINGLE page with a
//     high `limit` — for the question "does this drawer have ANY
//     conversation?" to be reliable, the four folders need to be
//     PAGINATED to the end OR a small `limit` requested and only
//     item presence/absence used. This is NOT a one-line change: it
//     requires rewriting `countInstagramConversations`, and it's left for
//     whoever confirms this hypothesis.
//
// 🔴 It's a `var`, NOT a `const`, and the reason is NARROW: exported as a
// `var` so that this package's tests
// (TestPermissaoDeMensagensInstagramParaDeVarrerSoQuandoDesconhecido,
// diagnostico_instagram_test.go) can prove TODAY, with save/restore, that
// the `if` this value guards actually does stop sweeping the four folders
// once it becomes FolderIgnored — without that, the mechanism could only
// be tested ON THE DAY the real measurement happened, too late to catch a
// wrong `if`. OUTSIDE OF TESTS it has ONE value per build: the day of the
// measurement changes this line and recompiles, it's never written at
// runtime.
//
// ✅ MEASURED IN PRODUCTION (T-114, 2026-07-31 15:54 -03, `v0.42.2`,
// `zapgw diagnostico --slug <instagram-ativa>` with
// `ZAPGW_DIAGNOSTICO_SONDAR_FOLDER=1`, via `ProbeInvalidInstagramFolder`):
// a `folder=zzz-nao-exists-t113` — a value Meta has NEVER documented — was
// ACCEPTED and returned the SAME `≥ 50` conversation(s) (first page) as the
// default inbox. Meta didn't reject the invalid folder; it IGNORED it. It's
// this line, and only it, that records the proof — without it the next
// person redoes the probe or, worse, "fixes" the sweep back
// (docs/ARMADILHAS.md, "Virou o padrão" NÃO é "virou obrigatório")
// is the mistake this measurement exists to not repeat).
var MeasuredFolderResult = FolderIgnored

// instagramProbeInvalidFolder is the value used by item 1 of T-113's Do:
// a `folder` that does NOT exist in Meta's documented vocabulary (`other`,
// `page_done`, `spam`, `requests`). If the filter really works, Meta
// rejects it; if it ignores the parameter, it returns the SAME list as the
// default inbox.
const instagramProbeInvalidFolder = "zzz-nao-existe-t113"

// ProbeInvalidInstagramFolder is THE MEASUREMENT for T-113's item 1:
// sends `folder=zzz-nao-exists-t113` (never documented by Meta) to
// `/me/conversations` and returns exactly what it answered, WITHOUT
// interpreting — whoever reads the result (`cmd/zapgw/diagnostico.go`) is
// who decides the text that goes to the screen:
//
//   - error (Meta REJECTED the invalid folder) → proves the filter EXISTS
//     (FolderHonored).
//   - no error (Meta accepted it and returned data) → the CALLER needs to
//     compare the returned `N` against the default inbox's: an EQUAL
//     number proves the parameter is IGNORED (FolderIgnored); this
//     function alone doesn't make that comparison because it doesn't have
//     the default inbox's count in hand.
//
// This call is MEASUREMENT, not part of the normal diagnostic — it only
// runs when explicitly requested (`ZAPGW_DIAGNOSTICO_SONDAR_FOLDER`),
// because spending one more request on every `zapgw diagnostico` helps no
// one once `MeasuredFolderResult` stops being FolderUnknown.
func (c *Client) ProbeInvalidInstagramFolder(ctx context.Context, base, token string) (ConversationCount, error) {
	return c.countInstagramConversations(ctx, base, token, instagramProbeInvalidFolder)
}

// InstagramMessagingPermission tests the
// `instagram_business_manage_messages` permission BY USE — it hits `GET
// /me/conversations` (the default inbox) and, while `MeasuredFolderResult`
// hasn't proven the `folder` parameter is ignored (T-113), also each of the
// four folders Meta uses to segregate conversations (`other`, `page_done`,
// `spam`, `requests`). Returns nil when the endpoint responds; returns the
// classified error (ClassifyResponse) when Meta rejects — a
// PERMISSION/CREDENTIAL REJECTION comes with ClassConfig (401/403), and
// that's how the caller distinguishes "missing permission" from "Meta is
// down" without interpreting a message string (T-109, MEASURED: the old
// script compared error text — "permission", "scope", "(#10)", "(#200)" —
// and that list is ALWAYS incomplete; the structural classification this
// package already has for the rest of the gateway, ClassifyResponse,
// solves this with no list at all).
//
// 🔴 DOES NOT CALL `debug_token`. See the top of the file.
//
// The INTENT of the four-folder sweep was to distinguish "no DM exists"
// from "it's in another drawer" (a DM from someone who does NOT follow the
// account falls into `requests`, and the default inbox does NOT bring it).
// 🔴 THIS NEVER WORKED, AND IT'S NOW PROVEN (T-114, 2026-07-31): the same
// protection was documented in THREE places — the donated `.py`, T-109's
// comment (this paragraph's earlier version), and here — and none of the
// three had been exercised against the case it claimed to cover. The
// measurement showed the premise was false from the start: Meta IGNORES
// `folder`, so the sweep never segregated anything — it just repeated the
// same default inbox four times. See `MeasuredFolderResult`, above, for
// the proof, and docs/ARMADILHAS.md, section "Meta / WhatsApp Cloud API",
// for the cost.
func (c *Client) InstagramMessagingPermission(ctx context.Context, base, token string) (InstagramPermission, error) {
	defaultInbox, err := c.countInstagramConversations(ctx, base, token, "")
	if err != nil {
		return InstagramPermission{}, err
	}
	p := InstagramPermission{ByFolder: map[string]ConversationCount{"": defaultInbox}}
	sum := defaultInbox.N
	sumIsFloor := defaultInbox.Floor
	// The extra folders are BEST EFFORT, and ONLY RUN while it isn't
	// PROVEN that the `folder` parameter is ignored (T-113,
	// MeasuredFolderResult above) — four requests that inform nothing
	// are WORSE than absence (docs/TASKS.md, T-113, Do item 2). One
	// folder failing (Meta sometimes rejects `folder=spam` in isolation,
	// with no bearing on the permission) cannot erase the verdict the
	// default inbox already gave.
	if MeasuredFolderResult != FolderIgnored {
		for _, folder := range []string{"other", "page_done", "spam", "requests"} {
			n, errFolder := c.countInstagramConversations(ctx, base, token, folder)
			if errFolder != nil {
				continue
			}
			p.ByFolder[folder] = n
			sum += n.N
			sumIsFloor = sumIsFloor || n.Floor
		}
	}
	p.TotalConversations = sum
	p.TotalIsFloor = sumIsFloor
	return p, nil
}

// InstagramPermission is the result of InstagramMessagingPermission — the
// permission is GRANTED when the function returns (InstagramPermission,
// nil); when it returns an error, it's the caller who reads the ErrorClass
// to decide whether the reason was permission or something else.
type InstagramPermission struct {
	// TotalConversations is the SUM of the N from each folder that answered —
	// not the real total when TotalIsFloor is true (any folder added in
	// had `paging.next`, so the sum is also a floor).
	TotalConversations int
	TotalIsFloor       bool
	ByFolder           map[string]ConversationCount // key "" is the default inbox (no `folder`)
}

func (c *Client) countInstagramConversations(ctx context.Context, base, token, folder string) (ConversationCount, error) {
	q := url.Values{"limit": {strconv.Itoa(diagnosticConversationCap)}}
	if folder != "" {
		q.Set("folder", folder)
	}
	raw, err := c.readInstagramGraph(ctx, base, "me/conversations", q, token)
	if err != nil {
		return ConversationCount{}, err
	}
	var envelope struct {
		Data   []json.RawMessage `json:"data"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ConversationCount{}, fmt.Errorf("meta: corpo de /me/conversations (instagram) nao entendido: %w", err)
	}
	return ConversationCount{
		N:     len(envelope.Data),
		Floor: strings.TrimSpace(envelope.Paging.Next) != "",
	}, nil
}

// InstagramWebhookSubscription returns the fields (`subscribed_fields`) the
// account has subscribed for the App this token belongs to — `GET
// /me/subscribed_apps`. It's the SAME call as step 3 of
// diag_instagram_meta.py.
func (c *Client) InstagramWebhookSubscription(ctx context.Context, base, token string) ([]string, error) {
	raw, err := c.readInstagramGraph(ctx, base, "me/subscribed_apps", nil, token)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data []struct {
			SubscribedFields []string `json:"subscribed_fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("meta: corpo de /me/subscribed_apps (instagram) nao entendido: %w", err)
	}
	var fields []string
	for _, item := range envelope.Data {
		fields = append(fields, item.SubscribedFields...)
	}
	return fields, nil
}

// readInstagramGraph is the GET common to the three functions above: builds
// the url over `base` (NEVER `c.base` — the SAME discipline as
// SendInstagramMessage and RenewInstagramToken in this package,
// because `base` here is always graph.instagram.com, a host DIFFERENT from
// the rest of the Graph API), puts the token in the HEADER (never in the
// URL — the same rule as the rest of the package), and classifies the
// response with ClassifyResponse, the SAME function every other path in
// this package uses.
func (c *Client) readInstagramGraph(ctx context.Context, base, path string, query url.Values, token string) ([]byte, error) {
	target, err := url.JoinPath(base, path)
	if err != nil {
		return nil, fmt.Errorf("meta: montar url de diagnostico: %w", err)
	}
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("meta: montar requisicao de diagnostico: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// We do NOT interpolate the error: *url.Error carries the full
		// URL. Here it doesn't carry the token (it goes in the header),
		// but it can carry `caminho` — the same caution as the rest of
		// the package.
		return nil, fmt.Errorf("meta: falha de transporte no diagnostico: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return nil, fmt.Errorf("meta: ler resposta do diagnostico: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return nil, metaError
	}
	return raw, nil
}
