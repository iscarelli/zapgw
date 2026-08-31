// POST /v1/leituras — telling the client that WE read their message (T-075).
//
// WHY ITS OWN ROUTE, AND NOT AN EIGHTH `tipo` OF POST /v1/messages. The seven
// send types have `para`, have content, and return `wa_message_id`. Marking
// as read has NONE of the three: the target is a message and not a person,
// there is no body, and no message is born. Stuffing this into the send envelope
// would turn three contract fields into "optional depending on type" — the
// exact ambiguity that already cost dearly in the `botoes`/`botoes_template` pair
// (docs/ARMADILHAS.md). The argument is `consumer-a`'s, and they are right.
//
// AUTHENTICATION IS THE SAME, and this too is a decision: the
// consumer->instance link from POST /v1/messages decides here the same way, and someone
// else's instance gets the SAME 403. A second auth model would be this project's
// mother pitfall in another outfit.
//
// ONE `wamid` PER CALL, NO LIST — decided in T-075 and already communicated to
// consumers in writing (2026-07-28 16:52). The argument does not depend on what
// Meta does, but on PARTIAL FAILURE: with a list, 5 of 13 failing would force the
// gateway to invent a response for "partial success", and every possible response
// is bad (200 lying, 500 telling it to repeat what already worked, or a body with a
// per-item result, which is a new API inside the route). With
// one target per call, partial failure does not exist as a concept. Accepting a list
// later is additive; removing the list later would be a break.
//
// There is NO `Idempotency-Key` HERE, and the absence is deliberate — see mark().
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

const readsRoute = "POST /v1/leituras"

type ReadsHandler struct {
	store    *config.Store
	auth     *Authenticator
	client   *meta.Client
	maxBytes int
	// counter counts with its OWN KEY (config.CounterReadsMarked), never
	// config.CounterSent — see the key's comment in
	// internal/config/counter.go. Registering returns no error at all, so
	// nothing here can propagate a counting failure into the response already written.
	counter *config.Counter
	// throttleLog suppresses repeated VALIDATION-refusal logging (T-037) — see
	// logThrottle and logRejection in handler.go.
	throttleLog *logThrottle
	// types declares which instance types this route serves (T-111) — see
	// the AcceptedTypes comment in types.go.
	types AcceptedTypes
}

// NewReadsHandler assembles the route. `types` is WhatsAppOnly: marking as read
// calls meta.Client.MarkAsRead with inst.PhoneNumberID, a field that only
// exists in config.TypeWhatsApp — empty in any Instagram instance
// (T-111). There is no Instagram equivalent in this slice.
func NewReadsHandler(store *config.Store, auth *Authenticator, client *meta.Client, maxBytes int, counter *config.Counter, types AcceptedTypes) http.Handler {
	h := &ReadsHandler{
		store: store, auth: auth, client: client, maxBytes: maxBytes, counter: counter,
		throttleLog: newLogThrottle(logSuppressionWindow),
		types:       types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(readsRoute, h.mark)
	return mux
}

// contar registers 1 in the counter (slug, key) — a safe no-op if the handler doesn't
// have a Counter (e.g. a test that doesn't care about counting).
func (h *ReadsHandler) count(slug, key string) {
	if h.counter != nil {
		h.counter.Record(slug, key)
	}
}

// ReadRequest is the body of POST /v1/leituras.
//
// THREE FIELDS: the instance (which decides the link and the credential), the `wamid`
// of the RECEIVED message, and the OPTIONAL `digitando` (T-147). There is no `para` — the
// target is a message, and Meta discovers the conversation through it.
//
// `Typing` did NOT get its own route (`/v1/digitando`) because the Cloud API has no
// endpoint of its own for it: it merges the "typing…" indicator into the
// SAME POST as the read receipt (see the MarkAsRead comment in
// internal/meta/read.go). A separate route would make the gateway issue TWO
// POSTs for what Meta does in one, and would force the consumer to choose between
// "mark read" and "mark read and typing" when the difference doesn't exist on
// the other side.
type ReadRequest struct {
	Instance string `json:"instancia"`
	Wamid    string `json:"wamid"`
	Typing   bool   `json:"digitando,omitempty"`
}

var (
	// ErrReadNoInstance and ErrReadNoWamid name the FIELD, never the
	// value: they propagate to the refusal log (T-037), and the `wamid` carries the
	// recipient's phone number inside it (docs/ARMADILHAS.md).
	ErrReadNoInstance = errors.New("campo `instancia` e obrigatorio")
	ErrReadNoWamid    = errors.New("campo `wamid` e obrigatorio")
)

// Validate trims both fields and requires both.
//
// It does NOT validate the SHAPE of the wamid. The gateway does not know the grammar of
// that identifier (there's no Meta source that fixes it), and refusing on a guess of
// shape would make the gateway reject a legitimate wamid on the day Meta changes the
// format — while accepting an invalid one costs only a 400 coming from her, with her
// code attached.
func (p *ReadRequest) Validate() error {
	p.Instance = strings.TrimSpace(p.Instance)
	p.Wamid = strings.TrimSpace(p.Wamid)
	if p.Instance == "" {
		return ErrReadNoInstance
	}
	if p.Wamid == "" {
		return ErrReadNoWamid
	}
	return nil
}

// marcar runs the guards in the SAME order as sending — authenticate -> read the body ->
// validate the schema -> check the link -> instance active? -> call Meta —
// minus one: THERE IS NO IDEMPOTENCY HERE.
//
// 🔴 WHY WE DO NOT REQUIRE `Idempotency-Key`, and why this needs to be
// written and not just absent: marking the same message as read twice is HARMLESS —
// no message is born, there is no billing, and the final state is the same. The key
// exists on send because a retry becomes TWO messages on a real customer's phone;
// here there's no possible duplicate to prevent. Requiring it would hand both
// consumers an "already marked" control that the operation doesn't need —
// and the operator opens the same conversation ten times a day. Both explicitly asked
// to send without keeping that state.
//
// If someday someone wants to add the control "just in case": it only has
// cost (new state in the consumer, a key held for an unknown outcome,
// a 409 to unblock) and buys no defect that actually exists.
func (h *ReadsHandler) mark(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar em %s: %v", readsRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, readsRoute, "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanent", "corpo grande demais", 0)
			return
		}
		// Same reading as the send: the consumer's connection dropped midway. 400
		// because what arrived incomplete was the REQUEST, and `retentavel` because
		// retrying resolves it.
		logRejection(h.throttleLog, readsRoute, "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retryable", "corpo nao foi lido por inteiro", 0)
		return
	}

	// T-203 (step 2 of T-189): accept the English name of every ENTRADA key
	// this route has (docs/MIGRACAO-CONTRATO-EN.md), translated to the
	// canonical (Portuguese) form BEFORE unmarshaling.
	translated, oldNames, ok := translateInputOrReject(
		w, h.throttleLog, readsRoute, consumer.Name, raw, instanceOnlyAlias)
	if !ok {
		return
	}

	var p ReadRequest
	if err := json.Unmarshal(translated, &p); err != nil {
		logRejection(h.throttleLog, readsRoute, "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanent", "corpo nao e JSON valido", 0)
		return
	}
	if err := p.Validate(); err != nil {
		// The message names the FIELD, never the value — and here this is more than
		// discipline: the `wamid` carries the recipient's phone number encoded
		// inside it, so logging the refused value would leak personal data
		// into the journal (docs/ARMADILHAS.md).
		logRejection(h.throttleLog, readsRoute, p.Instance, consumer.Name, err.Error())
		respondError(w, http.StatusBadRequest, "permanent", err.Error(), 0)
		return
	}
	if len(oldNames) > 0 {
		h.count(p.Instance, config.CounterOldNameUsed)
	}

	if !CanUse(consumer, p.Instance) {
		log.Printf("zapgw: consumidor %q pediu marcar leitura na instancia %q, que nao e dele",
			consumer.Name, p.Instance)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	inst, err := h.store.FindInstance(p.Instance)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			logRejection(h.throttleLog, readsRoute, p.Instance, consumer.Name, "instancia desconhecida")
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q em %s: %v", p.Instance, readsRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}
	if !inst.Active {
		respondError(w, http.StatusServiceUnavailable, "retryable", "instancia pausada", 0)
		return
	}
	// T-111: AFTER the link (403) and existence (404) — NEVER before,
	// otherwise this route becomes an oracle of "what type is this slug" for whoever isn't
	// its owner. checkType already writes the 400/config when it refuses.
	if !checkType(w, h.types, inst, "") {
		return
	}

	// CALL deadline, chosen by the instance — the SAME account as the send
	// (InstanceDeadline), and WithoutCancel for the same reason: who decides how long
	// to wait for Meta is the instance, not the consumer's client timeout.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), InstanceDeadline(inst))
	defer cancel()

	if err := h.client.MarkAsRead(ctx, inst.PhoneNumberID, inst.SendToken, p.Wamid, p.Typing); err != nil {
		h.respondReadError(w, inst.Slug, err)
		// T-035: count ONLY AFTER the response has been written.
		h.count(inst.Slug, config.CounterReadFailures)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// WITHOUT `wa_message_id`: no message was born, and inventing an id to
	// "keep the shape" of the send would be lying in the contract to save a line
	// of doc. `ok` is the same field the consumer already reads in /v1/health and in
	// the per-instance probe.
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	// T-035: after the response, and with its OWN KEY. If this line registered
	// config.CounterSent, both consumers' cost projection would start
	// counting conversations that never existed — see CounterReadsMarked.
	h.count(inst.Slug, config.CounterReadsMarked)
}

// respondReadError uses the SAME taxonomy as the send (statusForClass), with
// ONE difference that only applies here: the UNKNOWN outcome tells it to retry.
//
// On send, "I don't know whether Meta created the message" forces not resending, because
// a blind retry duplicates a message on a customer's phone. Marking as read
// has no possible duplicate, so the honest response is the opposite: retry.
func (h *ReadsHandler) respondReadError(w http.ResponseWriter, instanceSlug string, err error) {
	if errors.Is(err, meta.ErrInvalidPhoneNumberID) {
		// ALARM for the same reason as the send: every instance with this defect
		// becomes unable to talk to Meta until an admin corrects the
		// phone_number_id in the store. "Needs a person", not "wait for the retry".
		log.Printf("ALARME zapgw: phone_number_id invalido para a instancia %q — "+
			"corrija o phone_number_id no store; nenhuma chamada a Meta desta instancia funciona ate la",
			instanceSlug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; "+
				"o pedido nao foi enviado a Meta e nao adianta repetir ate isso ser corrigido", 0)
		return
	}

	var me *meta.MetaError
	if errors.As(err, &me) {
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: credencial da instancia %q recusada pela Meta ao marcar leitura", instanceSlug)
		}
		respondError(w, statusForClass(me.Class), string(me.Class), me.Message, me.MetaCode)
		return
	}

	// Transport, deadline exceeded, or error reading the response: Meta gave no
	// usable response. 502 `desconhecido` tells the truth about the upstream —
	// but, unlike the send, retrying here is safe and is the fix.
	respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
		"falha ao falar com a Meta; a marcacao pode nao ter acontecido — "+
			"repetir e seguro (marcar duas vezes nao tem efeito colateral)", 0)
}
