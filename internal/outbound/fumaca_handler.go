// POST /v1/fumaca — the consumer proves the channel and, IF Meta accepts,
// the instance activates (T-084).
//
// WHY IT EXISTS: step 4 of the model (docs/MODELO-DE-USO.md, "O fluxo, e
// quem faz cada passo" — the consumer "Prova o canal (`fumaca`)") wasn't
// runnable by a THIRD-PARTY consumer: `zapgw fumaca` is a command line
// that opens the local database, and a third party has no shell on the
// gateway machine. Until this task, the OWNER did the proving, after the
// consumer GAVE NOTICE — and there was no channel for that notice.
//
// ONE PATH, TWO FACADES: this route has no business logic of its own. It
// authenticates, checks the link and calls SmokeWithInstagramBase()
// (fumaca.go) — the SAME function `cmd/zapgw fumaca` calls. Two copies would
// diverge, and the one that diverged would be the one nobody runs by hand.
//
// 🔴 `ativo = 1` REMAINS A CONSEQUENCE OF META HAVING ACCEPTED, NEVER OF
// SOMEONE HAVING ASKED. There is no branch on this route that activates
// without a successful send — if Meta refuses, the instance stays PAUSED
// and the response says why.
//
// AUTHENTICATION IS THE SAME as the other instance routes: the
// consumer->instance link decides here just the same, and someone else's
// instance gets the SAME 403 — BEFORE any 404, so this route doesn't become
// an oracle for "does this slug exist?".
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

const smokeRoute = "POST /v1/fumaca"

type SmokeHandler struct {
	store       *config.Store
	auth        *Authenticator
	client      *meta.Client
	counter     *config.Counter
	maxBytes    int
	throttleLog *logThrottle
	// baseInstagram — see the comment on
	// NewSmokeHandlerWithInstagramBase, below (T-104).
	baseInstagram string
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go. It GATES NOTHING here:
	// SmokeWithInstagramBase (fumaca.go) already handles both types
	// internally since T-097/T-098 — the field exists only so this handler's
	// construction is forced to declare it, like every other one.
	types AcceptedTypes
}

// NewSmokeHandler builds the `POST /v1/fumaca` route against
// Instagram's PRODUCTION HOST — a shortcut for
// NewSmokeHandlerWithInstagramBase, below (SAME pairing as
// config.NewCounter/NewCounterWithStore).
func NewSmokeHandler(store *config.Store, auth *Authenticator, client *meta.Client, counter *config.Counter, maxBytes int, types AcceptedTypes) http.Handler {
	return NewSmokeHandlerWithInstagramBase(store, auth, client, counter, maxBytes, meta.DefaultInstagramRenewalBase, types)
}

// NewSmokeHandlerWithInstagramBase is NewSmokeHandler, above, with the
// Instagram HOST INJECTABLE (T-104) — passed along to
// outbound.SmokeWithInstagramBase, the SAME function `cmd/zapgw fumaca`
// calls. Only in test or in the lab (T-071) does `baseInstagram` point
// elsewhere; production uses meta.DefaultInstagramRenewalBase (via
// NewSmokeHandler).
//
// `types` is AllTypes (T-111): the smoke test already knows how to
// activate both types, handling the difference INTERNALLY since
// T-097/T-098 — see the comment on the `types` field, above.
func NewSmokeHandlerWithInstagramBase(store *config.Store, auth *Authenticator, client *meta.Client, counter *config.Counter, maxBytes int, baseInstagram string, types AcceptedTypes) http.Handler {
	h := &SmokeHandler{
		store: store, auth: auth, client: client, counter: counter, maxBytes: maxBytes,
		baseInstagram: baseInstagram,
		throttleLog:   newLogThrottle(logSuppressionWindow),
		types:         types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(smokeRoute, h.smoke)
	return mux
}

// SmokeRequest is POST /v1/fumaca's body: the instance and the
// DESTINATION that will RECEIVE the test message, in E.164. No default on
// purpose — a default here would send a message to the wrong number, and a
// sent message can't be undone.
type SmokeRequest struct {
	Instance    string `json:"instancia"`
	Destination string `json:"destino"`
}

var (
	ErrSmokeRequestNoInstance    = errors.New("campo `instancia` e obrigatorio")
	ErrSmokeRequestNoDestination = errors.New("campo `destino` e obrigatorio e nao tem default — o teste de fumaca manda uma mensagem DE VERDADE")
)

func (p *SmokeRequest) validate() error {
	p.Instance = strings.TrimSpace(p.Instance)
	p.Destination = strings.TrimSpace(p.Destination)
	if p.Instance == "" {
		return ErrSmokeRequestNoInstance
	}
	if p.Destination == "" {
		return ErrSmokeRequestNoDestination
	}
	return nil
}

// SmokeResponse is the body of the 200.
type SmokeResponse struct {
	Instance string `json:"instancia"`
	// State and Paused are the SAME fact, via the SAME functions as GET
	// /v1/estado and POST /v1/cadastro (config.StateOf).
	State  string `json:"estado"`
	Paused bool   `json:"pausada"`
	// AlreadyActive: no message was sent on THIS call — the instance had
	// already been activated before. Without this field, an automated
	// consumer would have no way to distinguish "I just proved the channel"
	// from "it was already proved before".
	AlreadyActive bool `json:"ja_estava_ativa"`
	// WaMessageID only appears when THIS call sent the test message
	// (omitempty: left out when AlreadyActive is true).
	WaMessageID string `json:"wa_message_id,omitempty"`
	// ActiveSince is the timestamp of the send that activated the instance
	// (the same event that adds to config.CounterSent) — RFC3339, absent if
	// the instance never activated (shouldn't happen in a success response,
	// but omitempty avoids asserting a date that doesn't exist).
	ActiveSince string `json:"ativa_desde,omitempty"`
}

func (h *SmokeHandler) smoke(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar em %s: %v", smokeRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, smokeRoute, "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanente", "corpo grande demais", 0)
			return
		}
		logRejection(h.throttleLog, smokeRoute, "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retentavel", "corpo nao foi lido por inteiro", 0)
		return
	}

	var p SmokeRequest
	if err := json.Unmarshal(raw, &p); err != nil {
		logRejection(h.throttleLog, smokeRoute, "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanente", "corpo nao e JSON valido", 0)
		return
	}
	if err := p.validate(); err != nil {
		logRejection(h.throttleLog, smokeRoute, p.Instance, consumer.Name, err.Error())
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
		return
	}

	// THE LINK BEFORE ANYTHING ELSE, and before even saying whether the
	// instance exists: someone else's instance gets a 403 and never a 404,
	// otherwise this route becomes an oracle answering "does this slug
	// exist?" to whoever has any token at all.
	if !CanUse(consumer, p.Instance) {
		log.Printf("zapgw: consumidor %q pediu fumaca na instancia %q, que nao e dele",
			consumer.Name, p.Instance)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	// WithoutCancel for the SAME reason as the send (handler.go): if the
	// consumer drops the connection midway, the test message may have
	// already gone out to the recipient — aborting the call to Meta midway
	// would leave that undefined, and the smoke test key has no retry nor
	// idempotency to resolve the doubt afterward.
	ctx := context.WithoutCancel(r.Context())

	result, err := SmokeWithInstagramBase(ctx, h.store, h.client, h.counter, p.Instance, p.Destination, h.baseInstagram, nil)
	if err != nil {
		h.respondSmokeError(w, p.Instance, consumer.Name, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(h.smokeResponse(result))
}

// smokeResponse builds the 200's body. The activation timestamp comes
// from the SAME counter the smoke test's message uses (config.CounterSent,
// LastEventPerKey) — there's no dedicated "ativado_em" column in the
// store, and there doesn't need to be: the send that activates is the same
// event the counter already timestamps (fumaca.go, step 3 — "BEFORE STEP 4,
// not after").
func (h *SmokeHandler) smokeResponse(r SmokeResult) SmokeResponse {
	resp := SmokeResponse{
		Instance:      r.Instance.Slug,
		State:         config.StateOf(config.InstanceSummary{Active: r.Instance.Active}),
		Paused:        !r.Instance.Active,
		AlreadyActive: r.AlreadyActive,
		WaMessageID:   r.WaMessageID,
	}
	events, err := h.store.LastEventPerKey(r.Instance.Slug)
	if err != nil {
		// This doesn't fail the response: the instance is ALREADY active
		// (and the message, if there was one, already went out) — the
		// timestamp is enrichment, not the guarantee. A failure here just
		// logs.
		log.Printf("zapgw: erro de store ao ler o carimbo de ativacao da instancia %q: %v", r.Instance.Slug, err)
		return resp
	}
	if when, ok := events[config.CounterSent]; ok {
		resp.ActiveSince = when.Format(time.RFC3339)
	}
	return resp
}

// respondSmokeError translates SmokeWithInstagramBase()'s errors into
// the status the consumer receives.
//
// WITH NO CHANNEL TO ASK, THE ERROR MESSAGE IS THE SUPPORT: each branch
// says WHY it refused and whether the instance stays PAUSED
// (docs/MODELO-DE-USO.md).
func (h *SmokeHandler) respondSmokeError(w http.ResponseWriter, slug, consumer string, err error) {
	if errors.Is(err, config.ErrInstanceNotFound) {
		log.Printf("zapgw: consumidor %q tem vinculo com a instancia %q, que NAO existe mais no banco", consumer, slug)
		respondError(w, http.StatusNotFound, "config", "esta instancia nao existe mais no gateway", 0)
		return
	}

	if errors.Is(err, ErrSmokeActivationFailed) {
		// NEEDS A HUMAN: the test message WAS DELIVERED but the instance
		// stays PAUSED. Retrying would send ANOTHER real message — that's
		// why this is NOT the "retentavel" class.
		log.Printf("ALARME zapgw: fumaca da instancia %q enviou a mensagem de teste mas falhou ao ativar: %v", slug, err)
		respondError(w, http.StatusBadGateway, "config",
			"a mensagem de teste foi enviada e aceita pela Meta, mas o gateway falhou ao marcar a instancia como ativa;"+
				" NAO rode o fumaca de novo (isso mandaria outra mensagem real) — avise quem opera o gateway", 0)
		return
	}

	if errors.Is(err, ErrFieldRequired) {
		// The only way Request.Validate() can fail here, given that instance
		// and text are always filled in by SmokeWithInstagramBase(): the
		// `destino` had no digit left after canonicalization.
		logRejection(h.throttleLog, smokeRoute, slug, consumer, err.Error())
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
		return
	}

	if errors.Is(err, meta.ErrInvalidPhoneNumberID) {
		log.Printf("ALARME zapgw: phone_number_id invalido para a instancia %q ao rodar o fumaca — "+
			"corrija o phone_number_id no store; a instancia continua PAUSADA", slug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; a instancia continua PAUSADA", 0)
		return
	}

	if errors.Is(err, meta.ErrResponseWithoutID) {
		respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
			"a Meta respondeu sem id de mensagem; a instancia continua PAUSADA", 0)
		return
	}

	var me *meta.MetaError
	if errors.As(err, &me) {
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: credencial da instancia %q recusada pela Meta ao rodar o fumaca; continua PAUSADA", slug)
		}
		respondError(w, statusForClass(me.Class), string(me.Class),
			me.Message+"; a instancia continua PAUSADA", me.MetaCode)
		return
	}

	// Transport failure, deadline exceeded, or error reading the response:
	// Meta gave no usable answer. The instance stays PAUSED because
	// SmokeWithInstagramBase() only activates after a confirmed send.
	respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
		"falha ao falar com a Meta; a instancia continua PAUSADA", 0)
}
