// GET /v1/instances/{slug}/instagram-users/{igsid} — who is this IGSID?
// (T-269)
//
// WHY IT EXISTS: the Instagram DM webhook carries only `sender.id` (the
// IGSID), never the @. A consumer that wants to fill a customer record needs
// the username (and the display name) of whoever wrote, and this route asks
// Meta for them with that instance's token.
//
// Meta only answers once the user consented, which Meta records when the
// user SENDS A MESSAGE to the account; a comment does not grant it. Without
// consent the Meta refusal is passed through to the consumer (code, subcode,
// message, trace), so the consent case is distinguishable by `meta_code` and
// Meta's own message. Meta's page states no rate limit for this call, and the
// gateway invents none: it adds no cache and no log per call, same reasoning
// as health_handler.go.
//
// GUARD ORDER (the same as every instance route):
//
//	authenticate -> bond with the instance (403) -> instance exists (404)
//	-> instance active (503) -> instance type (400) -> igsid shape (400) -> Meta
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

type InstagramUserHandler struct {
	store  *config.Store
	auth   *Authenticator
	client *meta.Client
	// baseInstagram is the root of the graph.instagram.com host, injectable
	// like the one on Handler (handler.go) so a test points it at a fake.
	baseInstagram string
	types         AcceptedTypes
}

// NewInstagramUserHandler builds the route against the real Instagram host.
// `types` is positional and mandatory (T-111).
func NewInstagramUserHandler(store *config.Store, auth *Authenticator, client *meta.Client, types AcceptedTypes) http.Handler {
	return NewInstagramUserHandlerWithBase(store, auth, client, meta.DefaultInstagramRenewalBase, types)
}

// NewInstagramUserHandlerWithBase is NewInstagramUserHandler with the host
// root injectable — production passes the same value the Instagram send uses.
func NewInstagramUserHandlerWithBase(store *config.Store, auth *Authenticator, client *meta.Client, baseInstagram string, types AcceptedTypes) http.Handler {
	h := &InstagramUserHandler{store: store, auth: auth, client: client, baseInstagram: baseInstagram, types: types}
	mux := http.NewServeMux()
	// A string LITERAL: the isolation table reads this registration by text.
	mux.HandleFunc("GET /v1/instances/{slug}/instagram-users/{igsid}", h.lookup)
	return mux
}

// instagramUserResponse is the body of the `200`. Name is null when Meta had
// none.
type instagramUserResponse struct {
	IGSID    string  `json:"igsid"`
	Username string  `json:"username"`
	Name     *string `json:"name"`
}

func (h *InstagramUserHandler) lookup(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "missing or invalid token", 0)
			return
		}
		log.Printf("zapgw: store error while authenticating on the instagram-users route: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
		return
	}

	slug := r.PathValue("slug")
	if !CanUse(consumer, slug) {
		respondError(w, http.StatusForbidden, "config", "instance not authorized for this consumer", 0)
		return
	}

	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			respondError(w, http.StatusNotFound, "config", "unknown instance", 0)
			return
		}
		log.Printf("zapgw: store error while looking up instance %q on the instagram-users route: %v", slug, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
		return
	}
	if !inst.Active {
		respondError(w, http.StatusServiceUnavailable, "retryable", "instance paused", 0)
		return
	}
	// AFTER the bond and the existence check, never before — otherwise this
	// route becomes an oracle of "what type is this slug".
	if !checkType(w, h.types, inst, "") {
		return
	}

	igsid := r.PathValue("igsid")
	if !meta.IGSIDValid(igsid) {
		// Refused here, before any call to Meta.
		respondError(w, http.StatusBadRequest, "permanent", "igsid must be digits only", 0)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	profile, err := h.client.InstagramUserProfile(ctx, h.baseInstagram, igsid, inst.SendToken)
	if err != nil {
		respondInstagramUserError(w, inst.Slug, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(instagramUserResponse{
		IGSID:    igsid,
		Username: profile.Username,
		Name:     profile.Name,
	})
}

// respondInstagramUserError translates the failure of the call to Meta, with
// the same mapping POST /v1/messages uses for a Meta refusal
// (respondMetaError + statusForClass).
func respondInstagramUserError(w http.ResponseWriter, slug string, err error) {
	var me *meta.MetaError
	switch {
	case errors.As(err, &me):
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: Meta refused the credential of instance %q when reading an Instagram user profile", slug)
		}
		respondMetaError(w, statusForClass(me.Class), me)
	case errors.Is(err, meta.ErrInstagramProfileNotUnderstood):
		respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
			"Meta answered about the Instagram user with something the gateway did not understand", 0)
	default:
		// Transport, deadline exceeded, reading the response.
		respondError(w, http.StatusServiceUnavailable, string(meta.ClassRetryable),
			"could not talk to Meta about the Instagram user; try again", 0)
	}
}
