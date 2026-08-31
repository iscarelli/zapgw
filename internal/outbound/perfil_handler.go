// GET/POST /v1/perfil — the `whatsapp_business_profile` (T-155): what the
// customer sees when tapping the business name, inside WhatsApp. `GET` reads,
// `POST` writes.
//
// THE ORDER OF THE GUARDS is the same as the other instance routes, and each
// one exists because the previous one isn't enough:
//
//	authenticate -> (POST: read the body) -> check the bond with the instance ->
//	instance active? -> type accepted? -> Meta
//
// NO CACHE, on purpose, for the SAME reason as the template catalog
// (templates_handler.go): the profile can change on Meta's side without
// notifying the gateway, and a cached read would answer with an address or
// phone number that's no longer current.
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

// profileNode is THE ONLY function that decides which instance field becomes
// the `node` of `whatsapp_business_profile` in the Graph API (T-155, item 2
// of the task) — no other point in this package or in internal/meta builds
// that node.
//
// ✅ CONFIRMED AGAINST PRODUCTION (T-157, 2026-08-20, `v0.59.0` in the air):
// `GET /v1/perfil?instancia=tenant-one` returned `200` with the real,
// complete profile (address, description, email, websites, profile_picture_url,
// vertical). `PhoneNumberID` is the right node — if it were `WabaID`, Meta
// would have returned `404` and the gateway would have passed the error on.
//
// THE CHOICE WAS BORN FROM A DOUBT, and it's worth recording where from: the
// third-party references consulted in T-155 DIVERGED — one cited
// `WABA_ID/whatsapp_business_profile`, another `PHONE_NUMBER_ID/…` — and the
// gateway bet on `PhoneNumberID` because it's the node it already uses for
// `/messages`, `/media`, and `/leituras`. It wasn't obvious: it was measured.
//
// This function stays the SINGLE POINT of substitution even with the doubt
// resolved — the reason for existing wasn't only the uncertainty, it's that
// Meta could change the node in the future. If that happens, the fix is ONE
// LINE here: swap in `inst.WabaID`, which the instance ALSO keeps. No other
// part of the gateway needs to change.
func profileNode(inst config.Instance) string {
	return inst.PhoneNumberID
}

type ProfileHandler struct {
	store    *config.Store
	auth     *Authenticator
	client   *meta.Client
	maxBytes int
	// throttleLog suppresses repeated logging of VALIDATION refusal (T-037) —
	// see logThrottle and logRejection in handler.go.
	throttleLog *logThrottle
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go.
	types AcceptedTypes
	// counter is the old-name migration metric (T-208,
	// config.CounterOldNameUsed). GET /v1/perfil's `instancia`/`instance` is
	// ENTRADA-QUERY: a query parameter is never a JSON key, so it never went
	// through translateAliasesInPlace, published pair or not, before T-208.
	// POSITIONAL AND MANDATORY, same discipline as the other counter-carrying
	// handlers — see the comment on BlockHandler.counter.
	counter *config.Counter
}

// NewProfileHandler builds the two routes. It does NOT keep PROFILE state: no
// cache, as the package comment explains.
//
// `types` is WhatsAppOnly: read() and write() use profileNode(inst), which
// today returns inst.PhoneNumberID — a field that only exists on
// config.TypeWhatsApp, empty on any Instagram instance (T-111). The business
// profile is exclusive to the WhatsApp Cloud API; there is no documented
// equivalent endpoint for Instagram in this slice.
func NewProfileHandler(store *config.Store, auth *Authenticator, client *meta.Client, maxBytes int, counter *config.Counter, types AcceptedTypes) http.Handler {
	h := &ProfileHandler{
		store: store, auth: auth, client: client, maxBytes: maxBytes,
		throttleLog: newLogThrottle(logSuppressionWindow),
		counter:     counter,
		types:       types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/perfil", h.read)
	mux.HandleFunc("POST /v1/perfil", h.write)
	return mux
}

// profileResponse is the body of the 200 for GET /v1/perfil. The format ONLY
// GROWS, like the rest of this gateway — meta.Profile is embedded so that a
// new field Meta starts returning shows up here without requiring a second
// field list that would diverge from the first (this project's mother
// trap).
type profileResponse struct {
	Instance string `json:"instance"`
	meta.Profile
}

// ProfileRequest is the body of POST /v1/perfil. meta.ProfilePatch is embedded
// for the SAME reason as profileResponse: a new field that Meta accepts goes
// into a single list.
type ProfileRequest struct {
	Instance string `json:"instancia"`
	meta.ProfilePatch
}

// profileWriteResponse is the body of the 200 for POST /v1/perfil. It echoes
// what WAS SENT (Saved), not a re-read from Meta: WriteProfile has no
// useful success body to pass through (see the function's comment, in
// internal/meta/perfil.go — the docs show only `{"success":true}`), and an
// immediate re-read would cost a second call with no documented
// read-after-write guarantee for this endpoint.
type profileWriteResponse struct {
	Instance string            `json:"instance"`
	Saved    meta.ProfilePatch `json:"gravado"`
}

func (h *ProfileHandler) read(w http.ResponseWriter, r *http.Request) {
	consumer, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	// T-208: `instancia`/`instance` is ENTRADA-QUERY here — see queryAlias's
	// comment in entrada_apelidos.go.
	slug, oldInstanceParam := queryAlias(r.URL.Query(), "instance", "instancia")
	if slug == "" {
		logRejection(h.throttleLog, "GET /v1/perfil", "", consumer.Name, "parametro instancia e obrigatorio")
		respondError(w, http.StatusBadRequest, "permanent", "parametro instancia e obrigatorio", 0)
		return
	}
	inst, ok := h.instanceActive(w, consumer, slug, "GET /v1/perfil")
	if !ok {
		return
	}
	if oldInstanceParam {
		h.counter.Record(inst.Slug, config.CounterOldNameUsed)
	}

	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	profile, err := h.client.ReadProfile(ctx, profileNode(inst), inst.SendToken)
	if err != nil {
		h.respondProfileError(w, inst.Slug, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// WHAT META RETURNED, WITHOUT INVENTING A MISSING FIELD (item 4 of the task):
	// profile already comes with just what it sent (see profileFromResponse in
	// internal/meta/perfil.go) — this handler adds nothing and zeroes nothing.
	_ = json.NewEncoder(w).Encode(profileResponse{Instance: inst.Slug, Profile: profile})
}

func (h *ProfileHandler) write(w http.ResponseWriter, r *http.Request) {
	consumer, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, "POST /v1/perfil", "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanent", "corpo grande demais", 0)
			return
		}
		logRejection(h.throttleLog, "POST /v1/perfil", "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retryable", "corpo nao foi lido por inteiro", 0)
		return
	}

	var p ProfileRequest
	if err := json.Unmarshal(raw, &p); err != nil {
		logRejection(h.throttleLog, "POST /v1/perfil", "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanent", "corpo nao e JSON valido", 0)
		return
	}
	p.Instance = strings.TrimSpace(p.Instance)
	if p.Instance == "" {
		logRejection(h.throttleLog, "POST /v1/perfil", "", consumer.Name, "campo instancia e obrigatorio")
		respondError(w, http.StatusBadRequest, "permanent", "campo instancia e obrigatorio", 0)
		return
	}

	inst, ok := h.instanceActive(w, consumer, p.Instance, "POST /v1/perfil")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	// ONLY THE FIELDS PRESENT IN p.ProfilePatch TRAVEL (item 4 of the task —
	// absent != empty): json.Unmarshal already leaves nil every pointer
	// whose field didn't come in the body, and WriteProfile
	// (internal/meta/perfil.go) omits from the JSON to Meta every key whose
	// pointer is nil. No conversion happens here in between.
	if err := h.client.WriteProfile(ctx, profileNode(inst), inst.SendToken, p.ProfilePatch); err != nil {
		h.respondProfileError(w, inst.Slug, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(profileWriteResponse{Instance: inst.Slug, Saved: p.ProfilePatch})
}

// autenticar responds and returns false when the caller doesn't pass.
func (h *ProfileHandler) authenticate(w http.ResponseWriter, r *http.Request) (config.Consumer, bool) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err == nil {
		return consumer, true
	}
	if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
		respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
		return config.Consumer{}, false
	}
	log.Printf("zapgw: erro de store ao autenticar em /v1/perfil: %v", err)
	respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
	return config.Consumer{}, false
}

// instanceActive is the SAME three guards from the other instance routes
// (bond, existence, pause), plus the type (T-111) — see the twin comment in
// templates_handler.go: two copies would diverge at the first change, and
// that's why BOTH routes in this file call this function, instead of each
// having its own.
func (h *ProfileHandler) instanceActive(
	w http.ResponseWriter, consumer config.Consumer, slug, route string,
) (config.Instance, bool) {
	// BEFORE any call to Meta: without this, a leaked token from system A
	// would read (or write!) B's profile — which describes B's business.
	if !CanUse(consumer, slug) {
		log.Printf("zapgw: consumidor %q pediu o perfil da instancia %q, que nao e dele",
			consumer.Name, slug)
		respondError(w, http.StatusForbidden, "config",
			"instancia nao autorizada para este consumidor", 0)
		return config.Instance{}, false
	}

	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			// 404 for instance, YES (T-037): whoever got here already authenticated.
			logRejection(h.throttleLog, route, slug, consumer.Name, "instancia desconhecida")
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return config.Instance{}, false
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q em %s: %v", slug, route, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return config.Instance{}, false
	}
	if !inst.Active {
		respondError(w, http.StatusServiceUnavailable, "retryable", "instancia pausada", 0)
		return config.Instance{}, false
	}
	// T-111: AFTER the bond (403) and the existence (404) — NEVER before,
	// or this route becomes an oracle of "what type is this slug" for
	// whoever isn't its owner. checkType already writes the 400/config
	// when it refuses.
	if !checkType(w, h.types, inst, "") {
		return config.Instance{}, false
	}
	return inst, true
}

// respondProfileError translates the failure of the call to Meta — used by
// BOTH routes, read() and write(): both reach the same set of possible
// outcomes (ReadProfile and WriteProfile return the SAME error types).
func (h *ProfileHandler) respondProfileError(w http.ResponseWriter, slug string, err error) {
	switch {
	case errors.Is(err, meta.ErrInvalidProfileNode):
		// NEEDS A HUMAN: the identifier that profileNode chose for this
		// instance has an invalid shape — no read/write of this instance's
		// profile works until an admin fixes the registration.
		log.Printf("ALARME zapgw: identificador de perfil invalido para a instancia %q — "+
			"corrija o cadastro; nenhuma leitura/escrita de perfil desta instancia funciona ate la", slug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; "+
				"o pedido nao chegou a Meta e nao adianta repetir ate isso ser corrigido", 0)
	case errors.Is(err, meta.ErrProfileNotUnderstood):
		respondError(w, http.StatusServiceUnavailable, string(meta.ClassRetryable),
			"a Meta respondeu sobre o perfil algo que o gateway nao entendeu; tente de novo", 0)
	default:
		var me *meta.MetaError
		if errors.As(err, &me) {
			if me.Class == meta.ClassConfig {
				log.Printf("ALARME zapgw: credencial da instancia %q recusada pela Meta ao falar do perfil", slug)
			}
			// T-153: respondMetaError (handler.go) passes through Detail,
			// Subcode, Explanation, and Trace — the same discipline as the
			// other routes that talk to Meta.
			respondMetaError(w, statusForClass(me.Class), me)
			return
		}
		// Transport, deadline exceeded, reading the response.
		respondError(w, http.StatusServiceUnavailable, string(meta.ClassRetryable),
			"nao foi possivel falar com a Meta sobre o perfil; tente de novo", 0)
	}
}
