// GET /v1/instances/{slug}/account-health — is this ACCOUNT still fit to
// send, beyond the token? (T-254)
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

// accountHealthErrorResponse is one item of an entity's `errors` — Meta's
// own `error_code`/`error_description`/`possible_solution`, renamed to the
// short keys this route uses.
type accountHealthErrorResponse struct {
	Code             int    `json:"code"`
	Description      string `json:"description"`
	PossibleSolution string `json:"possible_solution"`
}

// accountHealthEntityResponse is one item of `entities` — the UNION of the
// WABA call and the phone number call, WITHOUT the `id` key Meta sends: a
// third party's Meta id never leaves this route (see
// internal/meta/account_health.go, AccountHealthEntity).
type accountHealthEntityResponse struct {
	EntityType     string                       `json:"entity_type"`
	CanSendMessage string                       `json:"can_send_message"`
	Errors         []accountHealthErrorResponse `json:"errors"`
	AdditionalInfo []string                     `json:"additional_info"`
}

// accountHealthResponse is the body of the `200`.
//
// HasPaymentMethod is INFERRED from `primary_funding_id` coming back
// non-empty on the WABA call — it has NEVER been measured against a real
// account WITHOUT a payment method (docs/CONTRATO-CONSUMIDOR.md says so in
// the section this struct documents). The VALUE of primary_funding_id
// itself never reaches this struct: internal/meta/account_health.go parses
// it straight into a bool and discards it.
//
// CheckedAt is the instant THIS call talked to Meta — same role as
// healthResponse.VerifiedAt, and for the same reason: without cache it is
// always "now", and a consumer or proxy that caches this response cannot
// present it as fresh, because the age is written into it.
type accountHealthResponse struct {
	CanSendMessage   string                        `json:"can_send_message"`
	HasPaymentMethod bool                          `json:"has_payment_method"`
	Entities         []accountHealthEntityResponse `json:"entities"`
	CheckedAt        string                        `json:"checked_at"`
}

// accountHealthNotApplicableResponse is the `200` a non-WhatsApp instance
// gets — same shape idea as health_handler.go's Verdict field, kept as its
// own tiny struct because accountHealthResponse's fields (can_send_message,
// has_payment_method, entities) have no meaning to answer for a type that
// was never asked.
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
		// healthy — and we do not spend two calls to Meta for a channel
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
	// health_handler.go. It covers BOTH calls below, not one each: a
	// consumer that set a short timeout on the instance did so for the
	// whole probe, not for half of it.
	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	waba, err := h.client.ObserveWABAHealth(ctx, inst.WabaID, inst.SendToken)
	if err != nil {
		respondUnhealthy(w, err)
		return
	}
	if !recognizedCanSendMessage(waba.CanSendMessage) {
		respondUnrecognizedAccountHealth(w)
		return
	}

	phone, err := h.client.ObservePhoneHealth(ctx, inst.PhoneNumberID, inst.SendToken)
	if err != nil {
		respondUnhealthy(w, err)
		return
	}
	if !recognizedCanSendMessage(phone.CanSendMessage) {
		respondUnrecognizedAccountHealth(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(accountHealthResponse{
		CanSendMessage:   worstCanSendMessage(waba.CanSendMessage, phone.CanSendMessage),
		HasPaymentMethod: waba.HasFundingID,
		Entities:         accountHealthEntities(waba.Entities, phone.Entities),
		CheckedAt:        time.Now().UTC().Format(time.RFC3339),
	})
}

// respondUnrecognizedAccountHealth is the branch that keeps a `200` from
// EVER meaning AVAILABLE by omission: a response without `health_status`, or
// with a `can_send_message` outside the three literals Meta documents, is
// exactly as uninformative as no response at all — and gets the SAME class
// (`unknown`) respondUnhealthy already uses for "Meta didn't answer what we
// asked".
func respondUnrecognizedAccountHealth(w http.ResponseWriter) {
	respondError(w, http.StatusServiceUnavailable, string(meta.ClassUnknown),
		"resposta da Meta sem health_status reconhecivel", 0)
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

// worstCanSendMessage returns the WORSE of the WABA's and the phone
// number's own `can_send_message` — the account is only as fit to send as
// its most restricted half.
func worstCanSendMessage(waba, phone string) string {
	if canSendMessageRank[waba] >= canSendMessageRank[phone] {
		return waba
	}
	return phone
}

// accountHealthEntities is the UNION of both calls' entities, in order
// (WABA first, phone number second) — Meta's `id` never crosses into the
// response type (accountHealthEntityResponse has no field for it).
func accountHealthEntities(lists ...[]meta.AccountHealthEntity) []accountHealthEntityResponse {
	out := make([]accountHealthEntityResponse, 0)
	for _, list := range lists {
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
	}
	return out
}
