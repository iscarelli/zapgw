// GET /v1/instances/{slug}/health — the probe that is INFORMATIONAL.
//
// WHY IT EXISTS: `/v1/health` happily responds `200` with ALL tokens
// revoked — it only proves the process is standing. A token revoked by the
// client on Meta is the failure that dies silently: nothing in the gateway
// changes, no log appears, and the first to find out is the end customer who
// didn't receive the message. This endpoint asks Meta, with THAT instance's
// token, whether it still accepts it.
//
// The order of the guards is the same as sending, and each one exists
// because the previous one isn't enough:
//
//	authenticate -> check the bond with the instance -> instance active? -> Meta
//
// NO CACHE, and this isn't forgotten savings: a cached probe lies for
// exactly the duration of the cache, which is precisely when it matters
// most — the client revokes the token and the gateway keeps saying
// `ok:true` until the entry expires, which is the silent failure this
// endpoint exists to expose. Whoever calls a probe chooses the frequency;
// caching a result here steals that decision from whoever holds it.
//
// AND IT DOES NOT LOG NOR ALARM per call, on purpose. The frequency belongs
// to whoever queries: a monitor hitting it every 30 seconds with a dead
// token would fill the log with identical lines, and a repeated alarm trains
// the operator to ignore it (docs/ARMADILHAS.md). Whoever alarms about health
// is the monitor reading this response — the probe only answers the truth,
// now.
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

type HealthHandler struct {
	store  *config.Store
	auth   *Authenticator
	client *meta.Client
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go.
	types AcceptedTypes
}

// NewHealthHandler builds the probe. It does NOT keep state: there is no
// mutable field here at all, and that's how the absence of cache is proven
// by construction.
//
// `types` is AllTypes (T-111), and THIS IS NOT A LOOPHOLE: health()
// HANDLES THE DIFFERENCE INTERNALLY (see the switch there), never refuses
// with 400 — refusing would break the consumer's loop-based watching (this
// is a READ route). For types without a credential-confirmation endpoint
// equivalent to WhatsApp's GET /{phone_number_id} — Instagram, and any
// future type that hasn't gotten its own check yet —, the credential block
// comes out as NotApplicable, WITHOUT calling Meta. T-104 MEASURED that
// graph.instagram.com has no such equivalent; don't invent one by analogy.
func NewHealthHandler(store *config.Store, auth *Authenticator, client *meta.Client, types AcceptedTypes) http.Handler {
	h := &HealthHandler{store: store, auth: auth, client: client, types: types}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/instances/{slug}/health", h.health)
	return mux
}

// healthResponse is the body of the `200`.
//
// DisplayNumber comes from the STORE, never from Meta's success body:
// passing that body through would assert a response schema that wasn't
// verified at the source, and would still risk returning fields to the
// consumer that no one reviewed. It's here for the operator to confirm
// which number this slug speaks for.
//
// VerifiedAt is the instant THIS call talked to Meta. Without cache it's
// always "now" — and that's exactly why it travels: a dashboard or proxy
// that goes on to cache this response can't present it as fresh, because the
// age is written into it.
type healthResponse struct {
	OK            bool   `json:"ok"`
	DisplayNumber string `json:"numero_exibido,omitempty"`
	VerifiedAt    string `json:"verificado_em"`
	// Verdict only appears (omitempty) when the instance's TYPE has no
	// endpoint on Meta equivalent to GET /{phone_number_id} that confirms
	// the token WITHOUT sending a message (T-104 MEASURED that this doesn't
	// exist on graph.instagram.com; don't invent one by analogy — T-111). It
	// holds NotApplicable, and `ok` stays `true`: the gateway detected no
	// problem because it DIDN'T ASK — never "asked and everything's fine". A
	// consumer that only reads `ok` (every consumer today) sees no
	// difference at all on the path that already worked (WhatsApp).
	Verdict string `json:"veredito,omitempty"`
}

func (h *HealthHandler) health(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		// The only log in this file: database down isn't probe noise, and
		// without this line the gateway would go silent about its own failure.
		log.Printf("zapgw: erro de store ao autenticar no probe de saude: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}

	slug := r.PathValue("slug")
	// THE SAME guard as sending: querying an instance's health is speaking
	// about another system's business. It comes BEFORE any call to Meta —
	// without this, a leaked token from system A would spend system B's
	// instance quota and discover which slugs exist from the response status.
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
		log.Printf("zapgw: erro de store ao buscar instancia %q no probe de saude: %v", slug, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}
	if !inst.Active {
		// 503 like the rest of the gateway: a paused instance doesn't send, so
		// it isn't healthy — saying `ok:true` here because "the token is
		// valid" would be answering a different question. And we don't spend
		// a call to Meta for a channel that can't send anyway. HOLDS FOR
		// BOTH TYPES: pause is operational state, independent of type.
		respondError(w, http.StatusServiceUnavailable, "retentavel", "instancia pausada", 0)
		return
	}

	// T-111: AllTypes — health serves the TWO known types and handles
	// the difference INTERNALLY (never with 400: refusing would break the
	// consumer's loop-based watching — a READ route doesn't refuse).
	// `knownType` normalizes "" to WhatsApp; any OTHER value (Instagram,
	// or a future type that hasn't gotten its own check yet) falls into this
	// branch — without calling Meta, because the field the call below uses
	// (inst.PhoneNumberID) only exists on config.TypeWhatsApp, and calling
	// with it empty is exactly the defect T-111 exists to close.
	if knownType(inst.Type) != config.TypeWhatsApp {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{
			OK:         true,
			VerifiedAt: time.Now().UTC().Format(time.RFC3339),
			Verdict:    NotApplicable,
		})
		return
	}

	// The CALL's deadline, chosen by the instance — without this, an HTTP
	// client with no timeout (meta.NewClient(nil, …) falls back to
	// http.DefaultClient) would leave the probe hanging forever against a
	// destination that stalls.
	//
	// Unlike sending, here the consumer's context is NOT released with
	// WithoutCancel: there's no reserved idempotency key to protect, and
	// whoever gave up waiting for the probe's response has no interest at
	// all in the call continuing.
	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	if err := h.client.CheckCredential(ctx, inst.PhoneNumberID, inst.SendToken); err != nil {
		respondUnhealthy(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		OK:            true,
		DisplayNumber: inst.DisplayNumber,
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}

// respondUnhealthy translates the failure into `503` + the class that
// says what to do.
//
// EVERY outcome that isn't "Meta accepted the token" is `503`, not the
// sending status table: whoever queries a probe asks ONE question — is this
// channel fit to send right now? —, and a `502` here versus a `503` there
// forces the monitor to learn the entire taxonomy to decide "red or green".
// The class travels in the body, like the rest of the gateway, and it's the
// class that says whether the fix is waiting (`retentavel`) or calling a
// human (`config`).
//
// The message NEVER carries the token nor Meta's raw body: `em.Message`
// comes from ClassifyResponse, which reads only `error.message` and
// `error.code` — the rest of the body stays out because Meta's `error_data`
// can echo the payload that was sent, with phone number and message text.
func respondUnhealthy(w http.ResponseWriter, err error) {
	class := meta.ClassUnknown
	// Default: the call didn't end in a response from Meta (transport,
	// deadline exceeded, reading). We don't know whether the token is
	// valid — and saying "retentavel" would assert that we know.
	message := "nao foi possivel falar com a Meta; a saude deste canal nao pode ser confirmada agora"
	code := 0

	var me *meta.MetaError
	switch {
	case errors.Is(err, meta.ErrInvalidPhoneNumberID):
		// The request didn't even leave here: the registered phone_number_id
		// doesn't safely become a URL segment. Only a human can fix it, and
		// no sending from this instance works until then.
		class = meta.ClassConfig
		message = "o phone_number_id cadastrado para esta instancia tem forma invalida; " +
			"o probe nem chegou a falar com a Meta"
	case errors.As(err, &me):
		class = me.Class
		message = me.Message
		code = me.MetaCode
	}
	respondError(w, http.StatusServiceUnavailable, string(class), message, code)
}
