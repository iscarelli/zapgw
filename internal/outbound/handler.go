// POST /v1/messages — the send.
//
// ORDER matters and each step exists because the previous one is not enough:
//
//	authenticate -> read the body -> validate the schema -> check the link with
//	the instance -> instance active? -> reserve idempotency -> send -> confirm
//
// Validating the schema BEFORE checking the link is deliberate: a body error
// belongs to the consumer and they should know about it, even if they are also
// asking for an instance that is not theirs. But the call TO META only happens
// after both guards.
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

// defaultTimeout covers ONLY the instance provisioned without TimeoutMs (field <= 0).
//
// It is NOT a deadline recommendation — I did not check Meta's source for what
// a good value would be. It is only the ceiling that avoids calling the Graph
// API with NO limit at all when the provisioning data is missing or came
// invalid: without it, a destination that hangs holds the goroutine (and the
// idempotency slot) forever.
const defaultTimeout = 30 * time.Second

// statusForClass translates Meta's error class into the HTTP status the
// consumer receives.
//
// ONE function, used by every route that talks to Meta: two tables would
// diverge on the first change, and the consumer would have to learn which
// route follows which — which is this project's mother pitfall.
//
// `config` becomes 502, not 401: the problem is with the credential THE
// GATEWAY holds, not the consumer's token. Returning 401 would send them to
// check their own token, which is correct — and the defect would stay hidden
// in the wrong place.
func statusForClass(c meta.ErrorClass) int {
	switch c {
	case meta.ClassRetryable:
		return http.StatusServiceUnavailable
	case meta.ClassPermanent:
		return http.StatusBadRequest
	default: // config and unknown
		return http.StatusBadGateway
	}
}

// InstanceDeadline is the deadline for the CALL to Meta, chosen by the instance.
//
// Without it, meta.NewClient(nil, …) falls back to http.DefaultClient, which
// has no timeout at all, and an instance whose destination hangs holds the
// goroutine indefinitely. ONE function for the three HTTP routes and for the
// `zapgw template criar` command (T-036), for the same reason as
// statusForClass: exported so the command-line command reuses the SAME
// account instead of a copy that would diverge on the first change.
func InstanceDeadline(inst config.Instance) time.Duration {
	if inst.TimeoutMs <= 0 {
		return defaultTimeout
	}
	return time.Duration(inst.TimeoutMs) * time.Millisecond
}

// logSuppressionWindow is the window for the rejection log throttle (T-037).
//
// THE NUMBER IS OURS — it does not come from any external source. It exists
// only so a consumer looping with the SAME invalid request does not turn into
// one log line per request; short enough not to hide for too long a consumer
// that changes reason in the middle of the loop.
const logSuppressionWindow = time.Minute

// logThrottle decides whether a rejection can log NOW or was suppressed by
// a recent repetition of the SAME key (T-037).
//
// The FIRST occurrence of a key ALWAYS logs — unlike the inbound's
// contadorRejeicoes (internal/inbound/handler.go), which only alarms on the
// N-th rejection within the window, here the first is the OPERATOR'S ONLY
// CHANCE to know the reason right away: postponing this until the N-th
// attempt is too late to be useful (and that was exactly the defect T-037
// exists to close — see docs/TASKS.md). Only SUBSEQUENT repetitions of the
// SAME key, within the window, are left out.
//
// ONLY THE KEY (route+consumer) goes into the map — never the request body
// nor any field value. The map grows at most with the number of routes times
// the number of registered consumers, both bounded; never with arbitrary data
// from whoever calls (that is why the key does NOT include the error message
// nor the instance slug, which in a send request has not yet been confirmed
// against the store at the moment of the schema failure).
type logThrottle struct {
	mu     sync.Mutex
	window time.Duration
	// now is injectable only for testing — without it, proving the window
	// expiring would require actually sleeping.
	now  func() time.Time
	last map[string]time.Time
}

func newLogThrottle(window time.Duration) *logThrottle {
	return &logThrottle{window: window, now: time.Now, last: map[string]time.Time{}}
}

// permitir returns true the first time `key` appears, or after the window
// since the last time it was allowed has already passed; false while it is
// still valid.
func (t *logThrottle) allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if last, ok := t.last[key]; ok && now.Sub(last) < t.window {
		return false
	}
	t.last[key] = now
	return true
}

// logRejection records a 4xx rejection (NEVER 401) from the send, media and
// templates routes — T-037.
//
// WHAT MAKES THIS LOG POSSIBLE WITHOUT VIOLATING THIS PROJECT'S SECRETS RULE:
// this package's validation error message is OURS and, AS A RULE, does not
// carry request data — it names the FIELD, never the value (see
// Request.Validate() and the sibling validations in templates_handler.go and
// media_handler.go). The rule has known exceptions that are already handled —
// see safeRejectionMessage in mensagem.go, which exists exactly for the
// Validate() messages that quote the rejected value (ErrUnknownType and
// friends): whoever calls logRejection for a Request.Validate() rejection must
// pass safeRejectionMessage(err), never raw err.Error(). IF the general rule
// stops holding in a new case — someone "improves" an error message to quote
// the rejected value, without going through safeRejectionMessage —, this log
// starts leaking consumer data without anything here catching it. That is why
// the premise stays written HERE, at the point that uses it, and not only in
// the head of whoever wrote the validation.
//
// NEVER call this with the request body, a field value, or the
// Idempotency-Key (it can carry an entity id) — only with the slug (can be ""
// when the request failed before the gateway knew which instance was asked
// for), the consumer, and the message (which names the field, never the value).
func logRejection(throttle *logThrottle, route, slug, consumer, message string) {
	if !throttle.allow(route + "|" + consumer) {
		return
	}
	log.Printf("zapgw: %s recusou consumidor=%q instancia=%q: %s", route, consumer, slug, message)
}

type Handler struct {
	store    *config.Store
	auth     *Authenticator
	client   *meta.Client
	maxBytes int
	// counter is the SERIALIZED writer of instance counters (T-035).
	// Register returns no error at all — see internal/config/contador.go —
	// so nothing here can propagate a counting failure to the response already
	// written to the consumer.
	counter *config.Counter
	// transito is the writer of the TRANSIT log (T-091): "did this message pass
	// through here?", without storing content or a phone number in the clear.
	// NIL is safe (recordTransit checks). Register returns no error at all
	// (config.Transit), for the SAME reason as counter.
	transit *config.Transit
	// throttleLog suppresses repeated logging of a VALIDATION rejection (T-037) —
	// see logThrottle and logRejection.
	throttleLog *logThrottle
	// baseInstagram is the root of the graph.instagram.com host used by EVERY
	// send from an Instagram instance (T-104) — see the comment on NewHandler,
	// below, and the one on meta.DefaultInstagramRenewalBase.
	baseInstagram string
	// types declares which instance types this route serves (T-111) — see the
	// comment on AcceptedTypes in types.go. It GATES NOTHING here: send()
	// already handles both types internally since T-097 (see `inst.Type ==
	// config.TypeInstagram` below) — the field exists only so this handler's
	// construction is forced to declare, like every other one.
	types AcceptedTypes
}

// NewHandler builds the send handler against Instagram's PRODUCTION HOST —
// a shortcut for NewHandlerWithInstagramBase, below, for whoever does not
// need to point Instagram at a fake server (SAME pair as
// config.NewCounter/NewCounterWithStore).
func NewHandler(store *config.Store, auth *Authenticator, client *meta.Client, maxBytes int, counter *config.Counter, transit *config.Transit, types AcceptedTypes) http.Handler {
	return NewHandlerWithInstagramBase(store, auth, client, maxBytes, counter, transit, meta.DefaultInstagramRenewalBase, types)
}

// NewHandlerWithInstagramBase is NewHandler, above, with Instagram's HOST
// (graph.instagram.com) INJECTABLE — T-104. `baseInstagram` is used, NEVER
// `client`'s `c.base`, in every call to SendInstagramMessage: this
// handler's two WhatsApp/Instagram fields talk to DIFFERENT Graph API hosts,
// and a single *meta.Client does not hold two `base`s — the SAME pattern as
// RenewInstagramToken and outbound.SmokeWithInstagramBase, which already
// receive Instagram's base as a PARAMETER instead of embedding a second copy
// of the host.
//
// `types` is AllTypes (T-111): the send already serves WhatsApp and
// Instagram, handling the difference INTERNALLY since T-097 — see the
// comment on the `types` field, above.
func NewHandlerWithInstagramBase(store *config.Store, auth *Authenticator, client *meta.Client, maxBytes int, counter *config.Counter, transit *config.Transit, baseInstagram string, types AcceptedTypes) http.Handler {
	h := &Handler{
		store: store, auth: auth, client: client, maxBytes: maxBytes, counter: counter, transit: transit,
		baseInstagram: baseInstagram,
		throttleLog:   newLogThrottle(logSuppressionWindow),
		types:         types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", h.send)
	return mux
}

// contar records 1 in the counter (slug, key) — safe no-op if the handler
// does not have a Counter (e.g. a test that does not care about counting).
func (h *Handler) count(slug, key string) {
	if h.counter != nil {
		h.counter.Record(slug, key)
	}
}

// recordTransit writes the TRANSIT log (T-091) for ONE send attempt —
// safe no-op if the handler does not have a Transit.
//
// `key` (the consumer's Idempotency-Key) plays here the role `correlacao`
// plays in inbound: it is the id that ties this line to ONE call to
// POST /v1/messages, and it is the only request identifier the CONSUMER also
// knows — which helps cross-reference a report from them with this line.
//
// 🔴 BUT `key` NEVER GOES RAW INTO THE `correlacao` COLUMN, even after
// T-094: the `Idempotency-Key` is free-form text of EXTERNAL ORIGIN — the
// consumer chooses the content, and nothing stops them from putting an order
// id or a customer name there. The owner decided about the PHONE NUMBER
// (`canonicalTo`/`wamid` below, in the CLEAR since T-094), not about this
// field — Store.HMACCorrelation remains the way to index `key` without
// storing the value: whoever knows the key computes the same HMAC and finds
// the line; whoever only has the database, does not. See the comment on
// config.TransitRecord.Correlation for the full form.
func (h *Handler) recordTransit(slug, kind, key, canonicalTo, wamid, outcome string) {
	if h.transit == nil {
		return
	}
	h.transit.Record(config.TransitRecord{
		Slug:         slug,
		Direction:    config.DirectionOutbound,
		Counterparty: canonicalTo,
		Wamid:        wamid,
		Type:         kind,
		Correlation:  h.store.HMACCorrelation(key),
		Outcome:      outcome,
	})
}

// sendErrorClass classifies a send error in the SAME vocabulary that
// respondSendError uses to choose the HTTP status — reused here ONLY for
// the transit log outcome, never to decide what to answer the consumer (that
// decision remains solely respondSendError's). A short text from a CLOSED
// vocabulary, never err.Error(): a *meta.MetaError message might one day quote
// request data, and the transit log cannot become a second place that leaks it.
func sendErrorClass(err error) string {
	if errors.Is(err, meta.ErrInvalidPhoneNumberID) {
		return string(meta.ClassConfig)
	}
	var me *meta.MetaError
	if errors.As(err, &me) {
		return string(me.Class)
	}
	return string(meta.ClassUnknown)
}

type errorResponse struct {
	Error struct {
		Class    string `json:"classe"`
		MetaCode int    `json:"codigo_meta,omitempty"`
		Message  string `json:"mensagem"`
		// MetaDetail is T-141: RAW passthrough of Meta's error.error_data.details
		// (see meta.MetaError.Detail) — a field SEPARATE from Message,
		// NEVER concatenated into it: whoever today matches on `mensagem` must
		// not break, and the consumer needs to be able to handle the two
		// things separately. `omitempty` so the error body stays IDENTICAL,
		// byte for byte, to how it was before this task on every path that
		// has no detail (non-regression).
		MetaDetail string `json:"detalhe_meta,omitempty"`
		// The three fields below are T-153, and SEPARATE from each other and from
		// Message for the SAME reason as MetaDetail: none goes concatenated,
		// and all have `omitempty` so the body stays IDENTICAL when Meta does
		// not send the source fields. See meta.MetaError for what each one is
		// and where it comes from.
		MetaSubcode int `json:"subcodigo_meta,omitempty"`
		// MetaExplanation is meta.MetaError.Explanation: text that Meta WROTE TO BE
		// SHOWN — it is NOT stable across versions of their API (see
		// docs/CONTRATO-CONSUMIDOR.md).
		MetaExplanation string `json:"explicacao_meta,omitempty"`
		// MetaTrace is meta.MetaError.Trace (the fbtrace_id) — the ONLY
		// identifier Meta support accepts, and it does NOT come back after
		// this call. It is NOT a secret.
		MetaTrace string `json:"rastro_meta,omitempty"`
	} `json:"erro"`
}

// respondError is a FUNCTION, not a method: the health probe
// (saude_handler.go) answers with the SAME error format, and the consumer
// should not have to learn two. A second error writer in the package would
// diverge from the first on the first change.
//
// This package has dozens of callers of respondError outside this file
// (templates_handler.go, media_handler.go, cadastro_handler.go, …) none of
// which have Meta detail to pass — changing respondError's signature would
// force touching all of them just to always pass "". Instead, respondError
// keeps the SAME signature as always and delegates to
// respondErrorWithDetail (below) with an empty detail. Whoever has a whole
// *meta.MetaError to pass on (send() in T-141, and the two templates routes
// in T-153) calls respondMetaError, below, directly.
func respondError(w http.ResponseWriter, status int, class, message string, code int) {
	respondErrorWithDetail(w, status, class, message, code, "")
}

// respondErrorWithDetail is respondError, above, with `detalhe_meta`
// (T-141) also writable. See the comment on respondError for why the two
// functions exist instead of just one.
func respondErrorWithDetail(w http.ResponseWriter, status int, class, message string, code int, detail string) {
	var r errorResponse
	r.Error.Class = class
	r.Error.Message = message
	r.Error.MetaCode = code
	r.Error.MetaDetail = detail

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(r)
}

// respondMetaError is the SAME errorResponse body, but writing ALL the
// fields a *meta.MetaError can carry — Detail (T-141), Subcode, Explanation
// and Trace (T-153) — at once, instead of requiring a new parameter in
// respondErrorWithDetail every time Meta gains a field. It exists because
// two different routes (send, in handler.go, and the two in
// templates_handler.go) reach the same point — "I have a complete
// *meta.MetaError, I need to pass it all along" — and T-153 was born exactly
// from ONE of those routes having been left behind (task item 4: the
// consumer got the 503 through the templates route, not the send one).
func respondMetaError(w http.ResponseWriter, status int, me *meta.MetaError) {
	var r errorResponse
	r.Error.Class = string(me.Class)
	r.Error.Message = me.Message
	r.Error.MetaCode = me.MetaCode
	r.Error.MetaDetail = me.Detail
	r.Error.MetaSubcode = me.Subcode
	r.Error.MetaExplanation = me.Explanation
	r.Error.MetaTrace = me.Trace

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(r)
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		// Without the key there is no way to stop a retry from becoming two
		// messages on the customer's phone. Requiring it is cheaper than
		// explaining the duplicate.
		logRejection(h.throttleLog, "POST /v1/messages", "", consumer.Name,
			"header Idempotency-Key e obrigatorio")
		respondError(w, http.StatusBadRequest, "permanente", "header Idempotency-Key e obrigatorio", 0)
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, "POST /v1/messages", "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanente", "corpo grande demais", 0)
			return
		}
		// Any other error from ReadRaw is NOT "body too large" — it is the
		// consumer's connection dropping mid-upload (raw io.ReadAll). 400
		// because what arrived incomplete was the REQUEST (the gateway is up;
		// 503 would lie by saying the problem is ours), and retryable because
		// repeating fixes it — saying "permanent" here would send the consumer
		// to give up on something a new POST would fix.
		logRejection(h.throttleLog, "POST /v1/messages", "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retentavel", "corpo nao foi lido por inteiro", 0)
		return
	}

	// T-203 (step 2 of T-189): accept the English name of every ENTRADA key
	// this route has, translated to the CANONICAL (Portuguese) form BEFORE
	// unmarshaling — so p.Validate(), RequestHash and every downstream
	// consumer of Request see the SAME struct regardless of which language
	// the consumer wrote the body in. Must run before json.Unmarshal below:
	// see the header comment on entrada_apelidos.go for why the ORDER is
	// the whole point (idempotency has to hash the canonical form, or the
	// same message written in PT and in EN would send twice).
	translated, oldNames, err := translateRequestBody(raw)
	if err != nil {
		logRejection(h.throttleLog, "POST /v1/messages", "", consumer.Name, err.Error())
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
		return
	}

	var p Request
	if err := json.Unmarshal(translated, &p); err != nil {
		logRejection(h.throttleLog, "POST /v1/messages", "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanente", "corpo nao e JSON valido", 0)
		return
	}
	// T-097: captured BEFORE p.Validate() mutates p.To. For an Instagram
	// instance, `para` is an IGSID — not a phone number —, and Validate() still
	// runs meta.Canonicalize(p.To) for EVERY request (WhatsApp non-regression:
	// see the comment on Validate() in mensagem.go). An IGSID that happened to
	// have 12 digits starting with "55" would come out of Canonicalize with a
	// digit INSERTED — a corrupted address, silently. rawTo is the exact
	// value the consumer sent (only trimmed), and it is THAT ONE that goes to
	// Meta and to the transit log when the instance is Instagram — never p.To.
	rawTo := strings.TrimSpace(p.To)
	if err := p.Validate(); err != nil {
		// The schema error message is OURS and does not carry request data —
		// it names the field, never the value (with three exceptions that quote
		// the rejected value to guide the consumer; safeRejectionMessage
		// swaps them for a fixed text before logging — see mensagem.go). The
		// response body to the CONSUMER still uses the raw err.Error(): whoever
		// sent the wrong value already knows it, only the log must not repeat it.
		logRejection(h.throttleLog, "POST /v1/messages", p.Instance, consumer.Name, safeRejectionMessage(err))
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
		return
	}
	// AFTER Validate, never before: Validate trims the fields, and a hash over the
	// raw request would make " 5511..." and "5511..." — the SAME request —
	// collide as different requests.
	requestHash := RequestHash(p)

	// T-203 Do item 6: the number that authorizes step 4 (flipping OUTPUT
	// to English) is "how many requests still use the old contract",
	// measured — not assumed. Counted once per request, not once per old
	// key, so a request using three old names does not read as three times
	// the traffic of one using a single old name.
	if len(oldNames) > 0 {
		h.count(p.Instance, config.CounterOldNameUsed)
	}

	if !CanUse(consumer, p.Instance) {
		log.Printf("zapgw: consumidor %q pediu a instancia %q, que nao e dele",
			consumer.Name, p.Instance)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	inst, err := h.store.FindInstance(p.Instance)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			// instance 404 YES (T-037): whoever got here already authenticated —
			// it is someone's wrong config, which is worth investigating, unlike
			// 401 (scan noise).
			logRejection(h.throttleLog, "POST /v1/messages", p.Instance, consumer.Name, "instancia desconhecida")
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q: %v", p.Instance, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}
	if !inst.Active {
		log.Printf("zapgw: instancia %q esta pausada e recebeu pedido de envio", inst.Slug)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "instancia pausada", 0)
		return
	}
	// T-097: first Instagram slice — ONLY tipo:"texto". No template (Instagram
	// does not have one), no media, button, reaction or location (not
	// implemented yet). p.Validate() has already accepted the generic request
	// schema; this is the restriction SPECIFIC TO THE INSTANCE TYPE, which can
	// only be known after fetching `inst` — that is why it lives here, and not
	// in Validate() (mensagem.go), which does not know the instance type (it
	// runs BEFORE CanUse/FindInstance, on purpose — see this file's
	// header).
	if inst.Type == config.TypeInstagram && p.Type != "texto" {
		logRejection(h.throttleLog, "POST /v1/messages", inst.Slug, consumer.Name,
			"instancia Instagram so aceita tipo texto nesta fase")
		respondError(w, http.StatusBadRequest, "permanente",
			"esta instancia e Instagram; nesta fase o gateway so envia tipo \"texto\" — "+
				"sem template, midia, botao, reacao ou localizacao", 0)
		return
	}
	// counterpartyForLog is the value that goes to Meta (Instagram) and to the
	// transit log — p.To (canonicalized) for WhatsApp, exactly as before this
	// task; rawTo (the intact IGSID) for Instagram. See the comment on
	// rawTo, above.
	counterpartyForLog := p.To
	if inst.Type == config.TypeInstagram {
		counterpartyForLog = rawTo
	}

	alreadySent, reserved, err := h.store.ReserveIdempotency(consumer.Name, key, requestHash)
	if err != nil {
		if errors.Is(err, config.ErrKeyWithDifferentRequest) {
			// NOT 409: 409 says "wait and try again", and trying again
			// with this key for THIS request will never work — the request
			// has to change key. And the contract recommends the entity's id
			// as the key, and the same entity sends several messages (reminder,
			// billing, apology): without this 422, the second one gets the
			// FIRST one's id and never goes out — a silent and costly failure.
			respondError(w, http.StatusUnprocessableEntity, "permanente",
				"esta chave de idempotencia ja foi usada para outro pedido", 0)
			return
		}
		log.Printf("zapgw: erro de store na idempotencia: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}
	if alreadySent != "" {
		// "" for message_status: the idempotency record only keeps the id, and
		// replaying an old send has no new status to report — the same
		// silent treatment a send without the field already gets.
		h.respondOK(w, alreadySent, "") // already sent; return the SAME id
		return
	}
	if !reserved {
		// Another send with this key is in progress. 409 is the honest response:
		// we did not send, and there is no id to return yet.
		respondError(w, http.StatusConflict, "retentavel", "envio com esta chave em andamento", 0)
		return
	}

	// Deadline for the CALL, not for the received HTTP request: without this,
	// meta.NewClient(nil, …) falls back to http.DefaultClient, which has no
	// timeout at all, and an instance whose destination hangs holds the
	// goroutine (and the idempotency slot) indefinitely.
	deadline := InstanceDeadline(inst)
	// context.WithoutCancel FIRST, before WithTimeout: who decides how long to
	// wait for Meta is the INSTANCE (TimeoutMs), not the consumer's client
	// timeout. Without letting go of r.Context()'s cancellation, a typical HTTP
	// client timeout (3-5s, the same order of magnitude as the default
	// TimeoutMs) would abort the call to Meta MID-FLIGHT when the consumer
	// gives up on the request — and the key, already reserved, becomes an
	// unknown outcome and stays held for up to 72h. The impatient consumer
	// would strangle their own send and then not even be able to resend.
	// WithoutCancel preserves the context's VALUES (tracing) and discards only
	// the cancellation; the `defer cancel()` below is still mandatory —
	// without a PARENT cancellation, it is what prevents the WithTimeout
	// timer from leaking.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), deadline)
	defer cancel()

	// T-097: the instance's TYPE decides which Graph API call goes out — not a
	// second handler. Both return the SAME meta.SendResponse, and the rest of
	// this method (idempotency, counter, transit, response to the consumer)
	// is identical for both types from here on.
	var resp meta.SendResponse
	if inst.Type == config.TypeInstagram {
		resp, err = h.client.SendInstagramMessage(ctx, h.baseInstagram, inst.IgID, inst.SendToken, rawTo, p.Text)
	} else {
		resp, err = h.client.SendMessage(ctx, inst.PhoneNumberID, inst.SendToken, MetaBody(p))
	}
	if err != nil {
		// THE ASYMMETRY: we only release when we KNOW the message was not
		// created on Meta's side.
		//
		//   - *meta.MetaError (it answered with an error status) or
		//     ErrInvalidPhoneNumberID (the request never left here): a
		//     KNOWN-NEGATIVE outcome. Releasing is correct, the retry has to
		//     be able to actually send.
		//   - any other error (transport, reading the response,
		//     ErrResponseWithoutID, deadline exceeded): the message MAY have gone
		//     out. Releasing here would let a legitimate retry create a
		//     SECOND message on a real customer's phone — the damage
		//     idempotency exists entirely to prevent. Better the key stays
		//     held (the retry gets 409 until TTL purge or until someone
		//     investigates) than risk the duplicate.
		var me *meta.MetaError
		knownNegativeOutcome := errors.As(err, &me) || errors.Is(err, meta.ErrInvalidPhoneNumberID)
		if knownNegativeOutcome {
			if errRelease := h.store.ReleaseIdempotency(consumer.Name, key); errRelease != nil {
				// NEEDS A HUMAN: Meta rejected (message was NOT created),
				// but the DELETE that would return the key failed — without
				// manual action the consumer stays stuck at 409 even though
				// they have the right to resend. Release the key
				// (consumidor, key) by hand or wait for the TTL purge.
				log.Printf("ALARME zapgw: idempotencia nao liberada apos falha conhecida-negativa "+
					"(consumidor=%q chave=%q) — libere a mao ou aguarde a purga por TTL: %v",
					consumer.Name, key, errRelease)
			}
		} else {
			// NEEDS A HUMAN, but not NOW by default: the key stays held on
			// purpose until the TTL purge. Only turn into manual action if the
			// consumer complains of a persistent 409 BEFORE the TTL expires —
			// in that case, check whether the message went out (look for the
			// recipient in Meta's manager) before releasing, otherwise you
			// risk duplicating.
			log.Printf("ALARME zapgw: chave de idempotencia RETIDA por desfecho desconhecido "+
				"(a mensagem pode ter saido; consumidor=%q chave=%q): %v", consumer.Name, key, err)
		}
		h.respondSendError(w, inst.Slug, err)
		// T-035: count ONLY AFTER the response to the consumer has been written.
		// Register returns no error at all (internal/config/contador.go) — a
		// counting failure only logs, it never changes what was already
		// answered.
		h.count(inst.Slug, config.CounterSendFailures)
		// T-091: SAME place and SAME rule — after the response is written, through
		// a path that cannot return an error (config.Transit.Register).
		// No wamid: Meta did not return any id.
		h.recordTransit(inst.Slug, p.Type, key, counterpartyForLog, "", sendErrorClass(err))
		return
	}

	if err := h.store.ConfirmIdempotency(consumer.Name, key, resp.ID); err != nil {
		// The message WENT OUT. We do not invent a failure for the consumer — but
		// the idempotency record was left without an id, and a retry of it would
		// get 409 instead of the id. NEEDS A HUMAN: record the wa_message_id
		// below by hand (or warn the consumer out of band) before they try
		// again thinking it never sent.
		log.Printf("ALARME zapgw: mensagem %q enviada mas idempotencia nao confirmada "+
			"(consumidor=%q chave=%q): %v", resp.ID, consumer.Name, key, err)
	}
	h.respondOK(w, resp.ID, resp.MessageStatus)
	// T-035: count ONLY AFTER the response has been written — same guarantee
	// as the error branch above.
	h.count(inst.Slug, config.CounterSent)
	// T-091: same, SAME place and SAME rule.
	h.recordTransit(inst.Slug, p.Type, key, counterpartyForLog, resp.ID, "enviado")
}

// okResponse is the success body of POST /v1/messages.
//
// `MessageStatus` has `omitempty` on purpose: the field only appears when
// `respondOK` decides it matters (see there) — absence from Meta and
// "accepted" do not enter the JSON, which keeps the body IDENTICAL, byte for
// byte, to what it was before this task in both cases. A consumer that only
// reads `wa_message_id` (every consumer today) sees NO difference at all.
type okResponse struct {
	WaMessageID   string `json:"wa_message_id"`
	MessageStatus string `json:"message_status,omitempty"`
}

// respondOK builds the send success body.
//
// `status` is the RAW message_status Meta reported ("" when it did not send
// the field — see meta.SendResponse). It only enters the body when it is
// DIFFERENT from "accepted": accepted and absence are the SAME outcome from
// the point of view of whoever reads the response (Meta accepted the request
// normally), and that is why both cases produce the SAME body as today — the
// non-regression this task requires. Any OTHER value
// (held_for_quality_assessment, paused, or a future value Meta might send
// that this code does not yet know) is the case where `200` does NOT mean
// "it will arrive": that is why it stays visible in the body and alarms in
// the log, instead of disappearing behind the same old `200`. See
// docs/CONTRATO-CONSUMIDOR.md.
func (h *Handler) respondOK(w http.ResponseWriter, id, status string) {
	r := okResponse{WaMessageID: id}
	if status != "" && status != "accepted" {
		r.MessageStatus = status
		log.Printf("ALARME zapgw: a Meta aceitou o envio (wa_message_id=%q) mas message_status = %q, "+
			"nao \"accepted\" — o 200 nao garante que a mensagem chegue; ver docs/CONTRATO-CONSUMIDOR.md",
			id, status)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(r)
}

// respondSendError translates the error class into the status the
// consumer receives (the table lives in statusForClass) and adds what only
// applies to SENDING: what to do with the idempotency key.
func (h *Handler) respondSendError(w http.ResponseWriter, instanceSlug string, err error) {
	// ErrInvalidPhoneNumberID comes BEFORE errors.As(&em): it is not an
	// unknown outcome, and Meta was not even called (see PhoneNumberIDValid
	// in internal/meta/client.go), so "check whether the message arrived"
	// would send the operator looking for something that never existed. And
	// the key was already released three lines above (the caller's
	// knownNegativeOutcome), so "do not resend" would also be a lie.
	// ALARM because every instance with this defect becomes unable to send
	// until an admin fixes the phone_number_id in the store — "needs a
	// human", not "wait for the retry".
	if errors.Is(err, meta.ErrInvalidPhoneNumberID) {
		log.Printf("ALARME zapgw: phone_number_id invalido para a instancia %q — "+
			"corrija o phone_number_id no store; nenhum envio desta instancia funciona ate la",
			instanceSlug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; "+
				"o pedido nao foi enviado a Meta e nao adianta reenviar ate isso ser corrigido", 0)
		return
	}

	var me *meta.MetaError
	if errors.As(err, &me) {
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: credencial da instancia recusada pela Meta")
		}
		// T-141/T-153: em.Detail, em.Subcode, em.Explanation and em.Trace go
		// into the response body to the CONSUMER, as separate fields — NEVER
		// into the transit log. The log is persistent and read by whoever
		// operates it; the response goes only to whoever sent the payload and
		// therefore already has its content. rastro_meta (fbtrace_id) is the
		// only one of the four that does NOT carry request data — but it stays
		// out of the log ANYWAY, on purpose: the transit log has a fixed
		// column shape (T-091), and deciding to give it a new column is
		// another task, not this one. See recordTransit/sendErrorClass,
		// called below in the caller (h.send): they use only the error's
		// CLASS, never any of em's four fields.
		respondMetaError(w, statusForClass(me.Class), me)
		return
	}

	// The two branches below are the UNKNOWN outcome: the call did not end in
	// a response that says what Meta decided (see the asymmetry above), and
	// the corresponding key was left HELD, not released. Answering 503
	// retryable here would lie in both halves: 503 says "the gateway is
	// unavailable", but it is up; and it says "try again", but trying again
	// with THIS key can only give 409, because it was not released. 502 tells
	// the truth: the upstream (Meta) did not give a usable response.
	if errors.Is(err, meta.ErrResponseWithoutID) {
		respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
			"a Meta respondeu sem id de mensagem; nao reenvie com esta chave, "+
				"confira se a mensagem chegou e so entao decida", 0)
		return
	}
	// default: transport failure, deadline exceeded, or error reading the
	// response. Same reasoning as the branch above — the call did not produce
	// a response from Meta, so there is no way to know whether the message was
	// created.
	respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
		"falha ao falar com a Meta; nao reenvie com esta chave, "+
			"confira se a mensagem chegou e so entao decida", 0)
}
