// POST /v1/bloqueios, DELETE /v1/bloqueios and GET /v1/bloqueios — block,
// unblock, and list who is blocked, directly on the Cloud API (T-148,
// owner's decision on 2026-08-20, fidelity to the official API).
//
// The Cloud API has its own block endpoint and the gateway did not reach it:
// a consumer who needed to stop receiving from an abusive number had no path,
// and under this house's rule ("NINGUÉM fala direto com a Meta", CLAUDE.md)
// they also could not go around it.
//
// 🔴 BLOCKING IS REACTIVE, NOT PREVENTIVE — and this is not OUR rule, it is
// Meta's: you can only block whoever sent a message in the last 24 h, and you
// cannot block another business account (source in the header of
// internal/meta/block.go). The gateway does NOT check this window on its
// own — Meta decides, PER NUMBER, and the way to know is the next paragraph.
//
// 🔴 PARTIAL SUCCESS IS THE HARD CASE: Meta returns `200` in the envelope
// with an error PER NUMBER inside it (the 24h restriction above is the most
// common example of an error that shows up this way). That is why this route
// NEVER returns a plain `200` — every POST/DELETE call comes back with
// `processados` AND `falhas`, even when `falhas` is empty. A `200` without
// this distinction would make the consumer record "blocked" for someone who
// was not.
//
// AUTHENTICATION IS THE SAME as the other instance routes: the
// consumer->instance link decides here the same way, and someone else's
// instance gets the SAME 403 — there is no second authorization model.
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

const blockRoute = "/v1/bloqueios"

// phonesPerBlockLimit is the ceiling DECLARED by Meta PER CALL
// (T-148). The 64,000 ceiling is ACCOUNT STATE, not the request's — this
// file does not mirror it: Meta is the one who knows the total, and its
// error arrives via `detalhe_meta` (T-141) when it is exceeded.
const phonesPerBlockLimit = 1000

type BlockHandler struct {
	store    *config.Store
	auth     *Authenticator
	client   *meta.Client
	maxBytes int
	// throttleLog suppresses repeated logging of VALIDATION refusal (T-037) —
	// see logThrottle and logRejection in handler.go.
	throttleLog *logThrottle
	// counter is the old-name migration metric (T-205, config.CounterOldNameUsed)
	// — see the comment where it is recorded, below.
	counter *config.Counter
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go.
	types AcceptedTypes
}

// NewBlockHandler builds the three routes. `types` is WhatsAppOnly:
// blocking uses inst.PhoneNumberID, a field that only exists in
// config.TypeWhatsApp — empty in any Instagram instance (T-111/T-097). User
// blocking is exclusive to the WhatsApp Cloud API; there is no Instagram
// equivalent.
//
// `counter` is POSITIONAL AND MANDATORY (T-205, same discipline as
// AcceptedTypes) — see the comment on NewRegistrationHandler
// (registration_handler.go) for why an optional counter is the exact defect
// this task exists to close.
func NewBlockHandler(
	store *config.Store, auth *Authenticator, client *meta.Client, maxBytes int, counter *config.Counter, types AcceptedTypes,
) http.Handler {
	h := &BlockHandler{
		store: store, auth: auth, client: client, maxBytes: maxBytes,
		throttleLog: newLogThrottle(logSuppressionWindow),
		counter:     counter,
		types:       types,
	}
	mux := http.NewServeMux()
	// Literals, not a concatenation of blockRoute: the isolation table
	// (isolation_test.go) reads these three arguments BY TEXT and only
	// resolves a string literal or an integer-valued constant — an expression
	// like "POST "+X would leave it blind to this route, hiding it from the
	// 403 check.
	mux.HandleFunc("POST /v1/bloqueios", h.block)
	mux.HandleFunc("DELETE /v1/bloqueios", h.unblock)
	mux.HandleFunc("GET /v1/bloqueios", h.list)
	return mux
}

// BlockRequest is the body of POST/DELETE /v1/bloqueios.
type BlockRequest struct {
	Instance string   `json:"instancia"`
	Phones   []string `json:"telefones"`
}

var (
	// ErrBlockNoInstance and ErrBlockNoPhones name the FIELD,
	// never the value — same discipline as ErrReadNoInstance
	// (reads_handler.go), so the refusal log (T-037) can pass through
	// err.Error() without risk.
	ErrBlockNoInstance = errors.New("campo `instancia` e obrigatorio")
	ErrBlockNoPhones   = errors.New("campo `telefones` e obrigatorio e nao pode ser vazio")
)

// Validate trims and requires `instancia`, requires at least one
// `telefones[]`, refuses above the ceiling (ErrFieldTooLong, T-148 item 4), and
// CANONICALIZES each phone with meta.Canonicalize — the SAME rule as sending
// (message.go): sending without the ninth digit would silently block
// ANOTHER number.
func (p *BlockRequest) Validate() error {
	p.Instance = strings.TrimSpace(p.Instance)
	if p.Instance == "" {
		return ErrBlockNoInstance
	}
	if len(p.Phones) == 0 {
		return ErrBlockNoPhones
	}
	if n := len(p.Phones); n > phonesPerBlockLimit {
		return fmt.Errorf("%w: telefones (%d telefones, maximo %d por chamada)",
			ErrFieldTooLong, n, phonesPerBlockLimit)
	}
	for i, t := range p.Phones {
		c := meta.Canonicalize(t)
		if c == "" {
			return fmt.Errorf("%w: telefones[%d] (nao sobrou nenhum digito)", ErrFieldRequired, i)
		}
		p.Phones[i] = c
	}
	return nil
}

// blockItemResponse is ONE number PROCESSED successfully (blocked or
// unblocked, per the response's `operacao`).
type blockItemResponse struct {
	Phone string `json:"telefone"`
	WaID  string `json:"wa_id,omitempty"`
}

// blockFailureResponse is ONE number REFUSED by Meta INSIDE the same `200`
// — the heart of T-148 (item 3). Same three detail fields as the whole-call
// error (`erro.mensagem`/`erro.codigo_meta`/`erro.detalhe_meta`), just per
// item.
type blockFailureResponse struct {
	Phone      string `json:"telefone"`
	WaID       string `json:"wa_id,omitempty"`
	MetaCode   int    `json:"meta_code,omitempty"`
	Message    string `json:"message"`
	MetaDetail string `json:"meta_detail,omitempty"`
}

// blockOperationResponse is the `200` of POST/DELETE /v1/bloqueios —
// ALWAYS per number. `Operation` ("bloquear"/"desbloquear") exists so the
// consumer never has to guess which call generated this response just from
// the format.
type blockOperationResponse struct {
	Instance  string                 `json:"instance"`
	Operation string                 `json:"operacao"`
	Processed []blockItemResponse    `json:"processed"`
	Failures  []blockFailureResponse `json:"failures"`
}

// blockListItem is ONE number in the block list (GET) — only the
// `wa_id`: Meta does not return the phone number in the clear in this
// listing.
type blockListItem struct {
	WaID string `json:"wa_id"`
}

// blockListResponse is the `200` of GET /v1/bloqueios.
type blockListResponse struct {
	Instance     string          `json:"instance"`
	Total        int             `json:"total"`
	Blocked      []blockListItem `json:"blocked"`
	CursorBefore string          `json:"cursor_antes,omitempty"`
	CursorAfter  string          `json:"cursor_depois,omitempty"`
}

func (h *BlockHandler) block(w http.ResponseWriter, r *http.Request) {
	h.process(w, r, "POST "+blockRoute, "bloquear", h.client.BlockUsers)
}

func (h *BlockHandler) unblock(w http.ResponseWriter, r *http.Request) {
	h.process(w, r, "DELETE "+blockRoute, "desbloquear", h.client.UnblockUsers)
}

// process is the SHARED BODY of block and unblock — the ONLY
// difference between the two routes is WHICH Meta client method to call and
// the response's `operacao` label. Two copies would diverge on the first
// change (this project's mother pitfall, docs/ARMADILHAS.md).
//
// THE ORDER OF GUARDS is the SAME as the neighboring routes (leituras,
// templates): authenticate -> read the body -> validate the schema -> check
// the link -> instance active? -> type accepted? -> Meta.
func (h *BlockHandler) process(
	w http.ResponseWriter, r *http.Request, route, operation string,
	call func(ctx context.Context, phoneNumberID, token string, phones []string) (meta.BlockResult, error),
) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar em %s: %v", route, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, route, "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanent", "corpo grande demais", 0)
			return
		}
		// Same reading as the other routes: what arrived incomplete was the
		// REQUEST, and it's retryable because repeating resolves it.
		logRejection(h.throttleLog, route, "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retryable", "corpo nao foi lido por inteiro", 0)
		return
	}

	// T-203 (step 2 of T-189): accept `instance` as an alias of `instancia`.
	// T-208: `telefones` also has a published pair now (`phones`) — see
	// blockAlias's comment in input_aliases.go for why this route no
	// longer shares instanceOnlyAlias with pausa/leituras/fumaca.
	translated, oldNames, ok := translateInputOrReject(
		w, h.throttleLog, route, consumer.Name, raw, blockAlias)
	if !ok {
		return
	}

	var p BlockRequest
	if err := json.Unmarshal(translated, &p); err != nil {
		logRejection(h.throttleLog, route, "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanent", "corpo nao e JSON valido", 0)
		return
	}
	if err := p.Validate(); err != nil {
		logRejection(h.throttleLog, route, p.Instance, consumer.Name, err.Error())
		respondError(w, http.StatusBadRequest, "permanent", err.Error(), 0)
		return
	}
	// T-205 (the counter T-203 left unwired on this route): see the same
	// comment in registration_handler.go. `process` serves BOTH block and
	// unblock (h.block/h.unblock above) — the counter has to live here,
	// not in either caller, or one of the two verbs would silently stop
	// counting.
	if len(oldNames) > 0 {
		h.counter.Record(p.Instance, config.CounterOldNameUsed)
	}

	if !CanUse(consumer, p.Instance) {
		log.Printf("zapgw: consumidor %q pediu %s na instancia %q, que nao e dele",
			consumer.Name, operation, p.Instance)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	inst, err := h.store.FindInstance(p.Instance)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			logRejection(h.throttleLog, route, p.Instance, consumer.Name, "instancia desconhecida")
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q em %s: %v", p.Instance, route, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}
	if !inst.Active {
		respondError(w, http.StatusServiceUnavailable, "retryable", "instancia pausada", 0)
		return
	}
	// T-111: AFTER the link (403) and the existence (404) — NEVER before,
	// otherwise this route becomes an oracle of "what type is this slug" for
	// whoever does not own it. checkType already writes the 400/config
	// when it refuses.
	if !checkType(w, h.types, inst, "") {
		return
	}

	// CALL deadline, chosen by the instance — WithoutCancel for the same
	// reason as sending and /v1/leituras: the instance decides how long to
	// wait for Meta, not the consumer's client timeout.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), InstanceDeadline(inst))
	defer cancel()

	result, err := call(ctx, inst.PhoneNumberID, inst.SendToken, p.Phones)
	if err != nil {
		h.respondBlockError(w, inst.Slug, err)
		return
	}

	respondBlockResult(w, inst.Slug, operation, result)
}

// respondBlockResult is the `200` PER NUMBER — never a plain
// success. See the header of this file.
func respondBlockResult(w http.ResponseWriter, slug, operation string, result meta.BlockResult) {
	processed := make([]blockItemResponse, 0, len(result.Succeeded))
	for _, s := range result.Succeeded {
		processed = append(processed, blockItemResponse{Phone: s.Phone, WaID: s.WaID})
	}
	failures := make([]blockFailureResponse, 0, len(result.Failed))
	for _, f := range result.Failed {
		failures = append(failures, blockFailureResponse{
			Phone: f.Phone, WaID: f.WaID,
			MetaCode: f.MetaCode, Message: f.Message, MetaDetail: f.Detail,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(blockOperationResponse{
		Instance:  slug,
		Operation: operation,
		Processed: processed,
		Failures:  failures,
	})
}

// respondBlockError translates the failure of the WHOLE CALL (never
// that of a number inside it — that becomes `falhas[]`, not an HTTP error).
func (h *BlockHandler) respondBlockError(w http.ResponseWriter, instanceSlug string, err error) {
	if errors.Is(err, meta.ErrInvalidPhoneNumberID) {
		log.Printf("ALARME zapgw: phone_number_id invalido para a instancia %q — "+
			"corrija o phone_number_id no store; nenhum bloqueio desta instancia funciona ate la",
			instanceSlug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; "+
				"o pedido nao foi enviado a Meta e nao adianta repetir ate isso ser corrigido", 0)
		return
	}

	var me *meta.MetaError
	if errors.As(err, &me) {
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: credencial da instancia %q recusada pela Meta ao falar com block_users", instanceSlug)
		}
		respondErrorWithDetail(w, statusForClass(me.Class), string(me.Class), me.Message, me.MetaCode, me.Detail)
		return
	}

	// Transport, deadline exceeded, or unreadable response: NO number was
	// processed — unlike partial success, which only exists INSIDE a 200.
	// Blocking/unblocking creates no message and generates no charge:
	// repeating is safe, same reading as /v1/leituras.
	respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
		"falha ao falar com a Meta; a operacao pode nao ter acontecido para nenhum numero — "+
			"repetir e seguro (bloquear/desbloquear nao tem efeito colateral por si so)", 0)
}

const listBlocksRoute = "GET " + blockRoute

func (h *BlockHandler) list(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar em %s: %v", listBlocksRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}

	// T-208: `instancia`/`instance` is ENTRADA-QUERY here, not a body key —
	// queryAlias (input_aliases.go) is the SAME "novo or velho" idiom,
	// applied at the point query strings are actually read.
	slug, oldInstanceParam := queryAlias(r.URL.Query(), "instance", "instancia")
	if slug == "" {
		logRejection(h.throttleLog, listBlocksRoute, "", consumer.Name, "parametro instancia e obrigatorio")
		respondError(w, http.StatusBadRequest, "permanent", "parametro instancia e obrigatorio", 0)
		return
	}

	if !CanUse(consumer, slug) {
		log.Printf("zapgw: consumidor %q pediu lista de bloqueios da instancia %q, que nao e dele",
			consumer.Name, slug)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			logRejection(h.throttleLog, listBlocksRoute, slug, consumer.Name, "instancia desconhecida")
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q em %s: %v", slug, listBlocksRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "indisponivel", 0)
		return
	}
	if !inst.Active {
		respondError(w, http.StatusServiceUnavailable, "retryable", "instancia pausada", 0)
		return
	}
	if !checkType(w, h.types, inst, "") {
		return
	}
	// T-208: recorded only after every guard above accepted the request —
	// the same "count what the gateway actually served" moment as
	// GET /v1/estado and GET /v1/perfil (see their comments).
	if oldInstanceParam {
		h.counter.Record(inst.Slug, config.CounterOldNameUsed)
	}

	limit := 0
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, errConv := strconv.Atoi(v)
		if errConv != nil || n <= 0 {
			logRejection(h.throttleLog, listBlocksRoute, slug, consumer.Name, "parametro limit invalido")
			respondError(w, http.StatusBadRequest, "permanent", "parametro limit tem de ser um inteiro positivo", 0)
			return
		}
		limit = n
	}
	after := strings.TrimSpace(r.URL.Query().Get("after"))
	before := strings.TrimSpace(r.URL.Query().Get("before"))

	// READ: no WithoutCancel, same pattern as GET /v1/templates — there is
	// no write to protect from a client that gives up midway.
	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	page, err := h.client.ListBlocks(ctx, inst.PhoneNumberID, inst.SendToken, limit, after, before)
	if err != nil {
		h.respondBlockError(w, inst.Slug, err)
		return
	}

	items := make([]blockListItem, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, blockListItem{WaID: it.WaID})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(blockListResponse{
		Instance:     inst.Slug,
		Total:        len(items),
		Blocked:      items,
		CursorBefore: page.CursorBefore,
		CursorAfter:  page.CursorAfter,
	})
}
