// POST /v1/pausa — the consumer takes its OWN instance down (T-084).
//
// THE SAFE DIRECTION (fail-closed): unlike smoke test, it requires no proof
// at all — just the consumer->instance bond. GOING BACK requires a new smoke
// test: there is no other path to `ativo = 1` (internal/config/store.go:957,
// smoke.go), so pausing never reopens on its own.
//
// ONE PATH ONLY: this route calls store.PauseInstance directly, the SAME
// function that `zapgw instancia pausar` (cmd/zapgw/provision.go) already
// calls. There is no business logic to extract — PauseInstance is already
// the only path, from both sides.
package outbound

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
)

const pauseRoute = "POST /v1/pausa"

type PauseHandler struct {
	store       *config.Store
	auth        *Authenticator
	maxBytes    int
	throttleLog *logThrottle
	// counter is the old-name migration metric (T-205, config.CounterOldNameUsed)
	// — see the comment where it is recorded, below.
	counter *config.Counter
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go. It GATES nothing here: pausing
	// only changes the `ativo` field, which isn't specific to any type — the
	// field exists just so that building this handler is forced to declare
	// it, like every other one.
	types AcceptedTypes
}

// NewPauseHandler builds the route. `types` is AllTypes: pause()
// never reads a WhatsApp-specific or Instagram-specific field — see the
// comment on the `types` field, above.
//
// `counter` is POSITIONAL AND MANDATORY (T-205, same discipline as
// AcceptedTypes) — see the comment on NewRegistrationHandler for why an
// optional counter is the exact defect this task exists to close.
func NewPauseHandler(store *config.Store, auth *Authenticator, maxBytes int, counter *config.Counter, types AcceptedTypes) http.Handler {
	h := &PauseHandler{
		store: store, auth: auth, maxBytes: maxBytes, throttleLog: newLogThrottle(logSuppressionWindow),
		counter: counter,
		types:   types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(pauseRoute, h.pause)
	return mux
}

// PauseRequest is the body of POST /v1/pausa: just the instance.
type PauseRequest struct {
	Instance string `json:"instancia"`
}

var ErrPauseNoInstance = errors.New("campo `instancia` e obrigatorio")

// PauseResponse is the body of the 200.
type PauseResponse struct {
	Instance string `json:"instance"`
	State    string `json:"state"`
	Paused   bool   `json:"paused"`
}

func (h *PauseHandler) pause(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar em %s: %v", pauseRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, pauseRoute, "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanent", "corpo grande demais", 0)
			return
		}
		logRejection(h.throttleLog, pauseRoute, "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retryable", "corpo nao foi lido por inteiro", 0)
		return
	}

	// T-203 (step 2 of T-189): accept `instance` as an alias of `instancia`
	// (docs/MIGRACAO-CONTRATO-EN.md) — the only ENTRADA key this route has.
	translated, oldNames, ok := translateInputOrReject(
		w, h.throttleLog, pauseRoute, consumer.Name, raw, instanceOnlyAlias)
	if !ok {
		return
	}

	var p PauseRequest
	if err := json.Unmarshal(translated, &p); err != nil {
		logRejection(h.throttleLog, pauseRoute, "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanent", "corpo nao e JSON valido", 0)
		return
	}
	p.Instance = strings.TrimSpace(p.Instance)
	if p.Instance == "" {
		logRejection(h.throttleLog, pauseRoute, "", consumer.Name, ErrPauseNoInstance.Error())
		respondError(w, http.StatusBadRequest, "permanent", ErrPauseNoInstance.Error(), 0)
		return
	}
	// T-205 (the counter T-203 left unwired on this route): see the same
	// comment in registration_handler.go.
	if len(oldNames) > 0 {
		h.counter.Record(p.Instance, config.CounterOldNameUsed)
	}

	// THE BOND BEFORE ANYTHING ELSE — 403 before 404, like the sibling
	// routes.
	if !CanUse(consumer, p.Instance) {
		log.Printf("zapgw: consumidor %q pediu pausar a instancia %q, que nao e dele",
			consumer.Name, p.Instance)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	if err := h.store.PauseInstance(p.Instance); err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			log.Printf("zapgw: consumidor %q tem vinculo com a instancia %q, que NAO existe mais no banco",
				consumer.Name, p.Instance)
			respondError(w, http.StatusNotFound, "config", "esta instancia nao existe mais no gateway", 0)
			return
		}
		log.Printf("zapgw: erro de store ao pausar a instancia %q: %v", p.Instance, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(PauseResponse{
		Instance: p.Instance,
		State:    config.StateOf(config.InstanceSummary{Active: false}),
		Paused:   true,
	})
}
