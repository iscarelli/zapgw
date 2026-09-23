// GET /v1/instances/{slug}/account-health — is this ACCOUNT still fit to
// send, beyond the token? (T-254, split into three independent Meta queries
// by T-255)
//
// WHY IT EXISTS: on 2026-09-21 the Graph API refused a template send with
// error 131042 (the WABA has no payment method on file), and the consumer
// only found out through a real customer's failed send. The sibling probe
// (health_handler.go) only proves the TOKEN is still accepted — it never
// asks whether the ACCOUNT is fit to send at all. This route asks Meta, with
// that instance's token, BEFORE the consumer's own customer pays the price
// of finding out the hard way; the consumer's stated plan is to poll it
// every 15-30 minutes.
//
// SAME GUARD ORDER as health, and for the SAME reasons written there:
//
//	authenticate -> check the bond with the instance -> instance active? -> Meta
//
// NO CACHE, and NO LOG PER CALL, same reasoning as health_handler.go: the
// frequency belongs to whoever calls, and a repeated alarm trains the
// operator to ignore it (docs/ARMADILHAS.md).
//
// WHY THREE CALLS, NOT TWO: T-254 shipped combining `health_status` and
// `primary_funding_id` into ONE call to the WABA. The first real call
// (2026-09-22, through a consumer) came back `503` with Meta error code
// `10` ("requires... Business Solution Provider"), and there was no way to
// tell which of the two fields Meta actually refused — a single combined
// call means a refusal on EITHER field blinds the whole route. T-255 splits
// it into `phone_health_status`, `waba_health_status`, and `waba_funding`:
// only `phone_health_status` failing still means "no signal at all" (`503`,
// same as before); the other two failing independently degrades the `200`
// instead of erasing it — their failure lands in the `unavailable` array
// (see accountHealthResponse) instead of taking the whole response down.
//
// WHY ENTITIES ARE DEDUPLICATED BY entity_type (T-256): the second real call
// (consumer, v0.71.0, 2026-09-23 02:13 UTC) measured that
// phone_health_status's own `entities` already carries the WABA/BUSINESS/APP
// chain, not just PHONE_NUMBER — so concatenating it with
// waba_health_status's entities repeats those three, and the copies can
// DISAGREE (the same APP entity came back with Meta error 138025 in one copy
// and without it in the other). mergeEntitiesByType folds same-type entities
// into one, keeping the WORSE can_send_message and the UNION of errors and
// additional_info (see its own comment for the exact rule).
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

type AccountHealthHandler struct {
	store  *config.Store
	auth   *Authenticator
	client *meta.Client
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go.
	types AcceptedTypes
}

// NewAccountHealthHandler builds the probe. Like HealthHandler, it keeps NO
// mutable state at all — that is how the absence of cache is proven by
// construction.
//
// `types` is AllTypes (T-111), and — same as health_handler.go — this is NOT
// a loophole: accountHealth() handles the difference INTERNALLY, never with
// 400 (refusing would break the consumer's loop-based watching; this is a
// READ route). Meta's health_status has no documented equivalent for
// Instagram, so a non-WhatsApp instance answers NotApplicable WITHOUT
// calling Meta — the same decision T-104 already measured for the sibling
// probe.
func NewAccountHealthHandler(store *config.Store, auth *Authenticator, client *meta.Client, types AcceptedTypes) http.Handler {
	h := &AccountHealthHandler{store: store, auth: auth, client: client, types: types}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/instances/{slug}/account-health", h.accountHealth)
	return mux
}

// The three query names T-255 introduced — they travel in the response
// itself (accountHealthUnavailableEntry.Query, and the "<query>: " prefix on
// a 503's message), so a caller reading a degraded 200 or a refused 503
// knows WHICH of the three Graph calls is behind it, without having to
// reverse-engineer it from the message text.
const (
	queryPhoneHealthStatus = "phone_health_status"
	queryWABAHealthStatus  = "waba_health_status"
	queryWABAFunding       = "waba_funding"
)

// accountHealthErrorResponse is one item of an entity's `errors` — Meta's
// own `error_code`/`error_description`/`possible_solution`, renamed to the
// short keys this route uses.
type accountHealthErrorResponse struct {
	Code             int    `json:"code"`
	Description      string `json:"description"`
	PossibleSolution string `json:"possible_solution"`
}

// accountHealthEntityResponse is one item of `entities` — the UNION of the
// calls that answered, WITHOUT the `id` key Meta sends: a third party's Meta
// id never leaves this route (see internal/meta/account_health.go,
// AccountHealthEntity).
type accountHealthEntityResponse struct {
	EntityType     string                       `json:"entity_type"`
	CanSendMessage string                       `json:"can_send_message"`
	Errors         []accountHealthErrorResponse `json:"errors"`
	AdditionalInfo []string                     `json:"additional_info"`
}

// accountHealthUnavailableEntry is one item of `unavailable` — a query that
// failed WITHOUT taking the whole `200` down with it (T-255). Same hygiene
// as respondUnhealthy's body: never the raw Meta response, never the token.
type accountHealthUnavailableEntry struct {
	// Query is one of the three names above — WHICH call this failure came
	// from.
	Query    string `json:"query"`
	Class    string `json:"class"`
	MetaCode int    `json:"meta_code"`
	Message  string `json:"message"`
}

// accountHealthResponse is the body of the `200`.
//
// PaymentMethod replaces T-254's original boolean field for this same
// question (T-255): a bool could not tell "no payment method on file" apart
// from "we could not even ask" — and the two need different reactions from
// whoever reads this. It is one of three literals:
//
//   - "present" — waba_funding answered and primary_funding_id came back
//     non-empty.
//   - "absent"  — waba_funding answered WITHOUT the field, or with it empty.
//     NEVER used when waba_funding itself failed — that is "unavailable".
//   - "unavailable" — the waba_funding query failed; the failure itself is
//     in `unavailable`, with Query == "waba_funding".
//
// The VALUE of primary_funding_id itself never reaches this struct:
// internal/meta/account_health.go parses it straight into a bool and
// discards it.
//
// Unavailable lists the queries (waba_health_status, waba_funding) that
// failed WITHOUT taking the response down — omitted entirely when empty, so
// a fully healthy account's response looks exactly like T-254's did.
// phone_health_status never appears here: it failing is ALWAYS a `503`
// instead (see the file header), because without it there is no signal at
// all.
//
// CheckedAt is the instant THIS call talked to Meta — same role as
// healthResponse.VerifiedAt, and for the same reason: without cache it is
// always "now", and a consumer or proxy that caches this response cannot
// present it as fresh, because the age is written into it.
type accountHealthResponse struct {
	CanSendMessage string                          `json:"can_send_message"`
	PaymentMethod  string                          `json:"payment_method"`
	Entities       []accountHealthEntityResponse   `json:"entities"`
	Unavailable    []accountHealthUnavailableEntry `json:"unavailable,omitempty"`
	CheckedAt      string                          `json:"checked_at"`
}

// accountHealthNotApplicableResponse is the `200` a non-WhatsApp instance
// gets — same shape idea as health_handler.go's Verdict field, kept as its
// own tiny struct because accountHealthResponse's fields (can_send_message,
// payment_method, entities) have no meaning to answer for a type that was
// never asked.
type accountHealthNotApplicableResponse struct {
	Verdict   string `json:"verdict"`
	CheckedAt string `json:"checked_at"`
}

func (h *AccountHealthHandler) accountHealth(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		// The only log in this file: database down isn't probe noise, and
		// without this line the gateway would go silent about its own
		// failure — same reasoning as health_handler.go.
		log.Printf("zapgw: store error while authenticating on the account-health probe: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}

	slug := r.PathValue("slug")
	// THE SAME guard as sending and as health: querying an instance's
	// account health is speaking about another system's business, and it
	// runs BEFORE any call to Meta.
	if !CanUse(consumer, slug) {
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return
		}
		log.Printf("zapgw: store error while looking up instance %q on the account-health probe: %v", slug, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}
	if !inst.Active {
		// 503 like health: a paused instance does not send, so it is not
		// healthy — and we do not spend three calls to Meta for a channel
		// that cannot send anyway.
		respondError(w, http.StatusServiceUnavailable, "retryable", "instancia pausada", 0)
		return
	}

	// T-111: AllTypes — this route serves the TWO known types and handles
	// the difference INTERNALLY (never with 400). `health_status` has no
	// documented equivalent on graph.instagram.com (same absence T-104
	// measured for CheckCredential), so a non-WhatsApp instance short
	// circuits here WITHOUT calling Meta.
	if knownType(inst.Type) != config.TypeWhatsApp {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(accountHealthNotApplicableResponse{
			Verdict:   NotApplicable,
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	// The CALL's deadline, chosen by the instance — same reasoning as
	// health_handler.go. It covers ALL THREE calls below, not one each: a
	// consumer that set a short timeout on the instance did so for the
	// whole probe, not for a third of it.
	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	// (1) phone_health_status. Failing here is the ONLY failure that stays
	// a 503: without it there is no signal at all (see the file header).
	// The message is prefixed with the query name so a 503 says WHO was
	// refused, instead of forcing the reader to guess from the text alone.
	phone, err := h.client.ObservePhoneHealth(ctx, inst.PhoneNumberID, inst.SendToken)
	if err != nil {
		respondUnhealthyForQuery(w, queryPhoneHealthStatus, err)
		return
	}
	if !recognizedCanSendMessage(phone.CanSendMessage) {
		respondUnrecognizedAccountHealth(w, queryPhoneHealthStatus)
		return
	}

	canSend := phone.CanSendMessage
	entities := entityResponses(phone.Entities)
	var unavailable []accountHealthUnavailableEntry

	// (2) waba_health_status. Failing — Meta error OR an unrecognized
	// answer — degrades the response instead of erasing it: it lands in
	// `unavailable`, and can_send_message/entities come from
	// phone_health_status alone.
	waba, err := h.client.ObserveWABAHealth(ctx, inst.WabaID, inst.SendToken)
	switch {
	case err != nil:
		unavailable = append(unavailable, unavailableEntryFromError(queryWABAHealthStatus, err))
	case !recognizedCanSendMessage(waba.CanSendMessage):
		unavailable = append(unavailable, unavailableEntryUnrecognized(queryWABAHealthStatus))
	default:
		canSend = worstCanSendMessage(canSend, waba.CanSendMessage)
		entities = append(entities, entityResponses(waba.Entities)...)
	}

	// (3) waba_funding. Failing — same as (2) — degrades PaymentMethod to
	// "unavailable" instead of erasing the response; it NEVER reads as
	// "absent" (see accountHealthResponse's comment on why the two must not
	// collapse).
	paymentMethod := "absent"
	funding, err := h.client.ObserveWABAFunding(ctx, inst.WabaID, inst.SendToken)
	switch {
	case err != nil:
		unavailable = append(unavailable, unavailableEntryFromError(queryWABAFunding, err))
		paymentMethod = "unavailable"
	case funding.HasFundingID:
		paymentMethod = "present"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(accountHealthResponse{
		CanSendMessage: canSend,
		PaymentMethod:  paymentMethod,
		Entities:       mergeEntitiesByType(entities),
		Unavailable:    unavailable,
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
	})
}

// mergeEntitiesByType folds `entities` down to ONE entry per entity_type
// (T-256) — phone_health_status and waba_health_status can both answer with
// the WABA/BUSINESS/APP chain (measured 2026-09-23), so concatenating their
// entities repeats those three, and the repeats can disagree with each
// other. Order is the order of FIRST appearance, same discipline as the
// merge functions below:
//
//   - can_send_message: the WORSE of the copies (worstCanSendMessage — same
//     rank table the top-level field already uses, so a value outside the
//     three recognized literals follows the SAME existing rule: it ranks as
//     if it were AVAILABLE, never masking a real BLOCKED/LIMITED).
//   - errors: the union, keeping the FIRST occurrence of each `code` — a
//     code repeated across copies (the common case: the same Meta error on
//     both) survives once, not once per copy.
//   - additional_info: the union, without repeating the same string.
func mergeEntitiesByType(list []accountHealthEntityResponse) []accountHealthEntityResponse {
	merged := make([]accountHealthEntityResponse, 0, len(list))
	index := make(map[string]int, len(list))
	for _, e := range list {
		if i, ok := index[e.EntityType]; ok {
			existing := &merged[i]
			existing.CanSendMessage = worstCanSendMessage(existing.CanSendMessage, e.CanSendMessage)
			existing.Errors = mergeErrorsByCode(existing.Errors, e.Errors)
			existing.AdditionalInfo = mergeStrings(existing.AdditionalInfo, e.AdditionalInfo)
			continue
		}
		index[e.EntityType] = len(merged)
		merged = append(merged, e)
	}
	return merged
}

// mergeErrorsByCode unions two entities' `errors`, keeping the FIRST
// occurrence of each `code` — see mergeEntitiesByType's comment for why a
// repeated code (the same Meta error surviving into both copies of an
// entity) must not become two identical entries.
func mergeErrorsByCode(a, b []accountHealthErrorResponse) []accountHealthErrorResponse {
	seen := make(map[int]bool, len(a)+len(b))
	out := make([]accountHealthErrorResponse, 0, len(a)+len(b))
	for _, list := range [][]accountHealthErrorResponse{a, b} {
		for _, er := range list {
			if seen[er.Code] {
				continue
			}
			seen[er.Code] = true
			out = append(out, er)
		}
	}
	return out
}

// mergeStrings unions two string slices without repeating a value — used for
// `additional_info`, which (unlike errors) has no code to key on, so the
// string itself is the identity.
func mergeStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// respondUnhealthyForQuery is respondUnhealthy (health_handler.go), with the
// query's name prefixed onto the message — the ONLY place in this route a
// Graph failure still becomes a `503` (phone_health_status; see the file
// header for why the other two queries degrade the `200` instead).
func respondUnhealthyForQuery(w http.ResponseWriter, query string, err error) {
	class, message, code := classifyHealthProbeError(err)
	respondError(w, http.StatusServiceUnavailable, string(class), query+": "+message, code)
}

// unavailableEntryFromError turns a Graph failure on waba_health_status or
// waba_funding into an `unavailable` entry instead of a `503` — the SAME
// classification respondUnhealthy uses, just carried in the array instead of
// in the top-level error body.
func unavailableEntryFromError(query string, err error) accountHealthUnavailableEntry {
	class, message, code := classifyHealthProbeError(err)
	return accountHealthUnavailableEntry{
		Query:    query,
		Class:    string(class),
		MetaCode: code,
		Message:  message,
	}
}

// unrecognizedHealthStatusMessage is the text both
// respondUnrecognizedAccountHealth and unavailableEntryUnrecognized use for
// a 2xx response that came back WITHOUT a health_status this route trusts.
const unrecognizedHealthStatusMessage = "resposta da Meta sem health_status reconhecivel"

// unavailableEntryUnrecognized is unavailableEntryFromError's sibling for
// the "Meta answered 200 but not with a health_status we recognize" case —
// there is no *meta.MetaError to classify here, so the class is `unknown`
// directly, same as respondUnrecognizedAccountHealth uses for
// phone_health_status.
func unavailableEntryUnrecognized(query string) accountHealthUnavailableEntry {
	return accountHealthUnavailableEntry{
		Query:   query,
		Class:   string(meta.ClassUnknown),
		Message: unrecognizedHealthStatusMessage,
	}
}

// respondUnrecognizedAccountHealth is the branch that keeps a `200` from
// EVER meaning AVAILABLE by omission: a response without `health_status`, or
// with a `can_send_message` outside the three literals Meta documents, is
// exactly as uninformative as no response at all — and gets the SAME class
// (`unknown`) respondUnhealthy already uses for "Meta didn't answer what we
// asked". Only reached for phone_health_status (query): the other two
// queries route the same condition through unavailableEntryUnrecognized
// instead, because they degrade the 200 rather than replacing it.
func respondUnrecognizedAccountHealth(w http.ResponseWriter, query string) {
	respondError(w, http.StatusServiceUnavailable, string(meta.ClassUnknown),
		query+": "+unrecognizedHealthStatusMessage, 0)
}

// recognizedCanSendMessage is the CLOSED vocabulary this route trusts for
// `can_send_message`. Anything else — absent, misspelled, or a future value
// Meta adds — is NOT accepted as a verdict: see respondUnrecognizedAccountHealth.
func recognizedCanSendMessage(v string) bool {
	switch v {
	case "AVAILABLE", "LIMITED", "BLOCKED":
		return true
	default:
		return false
	}
}

// canSendMessageRank orders the three recognized literals from best to
// worst. It is only ever consulted AFTER recognizedCanSendMessage confirmed
// both inputs are one of the three — an unrecognized value never reaches
// worstCanSendMessage.
var canSendMessageRank = map[string]int{
	"AVAILABLE": 0,
	"LIMITED":   1,
	"BLOCKED":   2,
}

// worstCanSendMessage returns the WORSE of two of the route's own
// can_send_message values — the account is only as fit to send as its most
// restricted half. Only ever called with values recognizedCanSendMessage
// already accepted.
func worstCanSendMessage(a, b string) string {
	if canSendMessageRank[a] >= canSendMessageRank[b] {
		return a
	}
	return b
}

// entityResponses converts ONE query's entities into the response shape —
// Meta's `id` never crosses into it (accountHealthEntityResponse has no
// field for it). The caller concatenates the results of the queries that
// actually answered (T-255: a query that failed contributes NO entities,
// its failure goes into `unavailable` instead).
func entityResponses(list []meta.AccountHealthEntity) []accountHealthEntityResponse {
	out := make([]accountHealthEntityResponse, 0, len(list))
	for _, e := range list {
		errs := make([]accountHealthErrorResponse, 0, len(e.Errors))
		for _, er := range e.Errors {
			errs = append(errs, accountHealthErrorResponse{
				Code:             er.Code,
				Description:      er.Description,
				PossibleSolution: er.PossibleSolution,
			})
		}
		info := make([]string, 0, len(e.AdditionalInfo))
		info = append(info, e.AdditionalInfo...)
		out = append(out, accountHealthEntityResponse{
			EntityType:     e.EntityType,
			CanSendMessage: e.CanSendMessage,
			Errors:         errs,
			AdditionalInfo: info,
		})
	}
	return out
}
