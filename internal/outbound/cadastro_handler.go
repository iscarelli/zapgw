// POST /v1/cadastro — the CONSUMER registers THEIR OWN Meta (T-079).
//
// 🔴 THE DIRECTION IS CONSUMER -> GATEWAY, AND THIS IS A WRITE. This route does not
// return any configuration: the gateway does not self-describe here, it RECEIVES. The
// whole design is in docs/MODELO-DE-USO.md, which wins over this file if the
// two diverge.
//
// WHY IT EXISTS: the gateway's target audience is THIRD-PARTY developers, with their
// own Meta account, out of the owner's reach and with no channel to ask. Until this
// task, `zapgw provisionar instancia` required the OWNER to know `waba_id`,
// `phone_number_id`, number, and the App's secrets — data that belongs to the consumer,
// which the owner does not have and should not have. The model decided is the
// opposite: the owner creates the instance with only the SLUG (which is theirs because
// it's immutable and becomes a URL path), hands over the minimum for the consumer to
// talk to the gateway, and the consumer registers their own Meta from their own panel.
//
// AUTHENTICATION IS THE SAME as POST /v1/messages, and this is a decision: the
// consumer->instance link decides here the same way, and someone else's instance gets
// the SAME 403. A second auth model would be this project's mother pitfall in another outfit.
//
// 🔴 WHAT THIS ROUTE COSTS, and the consumer HAS to know: with it, the consumer
// token stops being "send a message" and becomes "reconfigure the
// instance" — whoever steals it restores the credentials and points the instance to
// their own Meta. This is an accepted consequence of the model, and what limits it is not
// permission but TIME: the config.RegistrationWindow window, counted from the FIRST insertion.
// After it, a stolen token goes back to being worth only "send a message".
package outbound

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
)

const registrationRoute = "POST /v1/cadastro"

type RegistrationHandler struct {
	store    *config.Store
	auth     *Authenticator
	maxBytes int
	// now is injectable for TESTING: the 24h window is only feasible if the test
	// can move the clock. In production it's always time.Now.
	now func() time.Time
	// throttleLog suppresses repeated VALIDATION-refusal logging (T-037) — see
	// logThrottle and logRejection in handler.go.
	throttleLog *logThrottle
	// types declares which instance types this route serves (T-111) — see
	// the AcceptedTypes comment in types.go.
	types AcceptedTypes
}

// NewRegistrationHandler assembles the route. It does NOT receive *meta.Client, and the
// absence is information: this route does not talk to Meta. It writes what the consumer
// sent; what proves the credential works is `zapgw fumaca`, and that's why
// registering does NOT activate.
//
// `types` is WhatsAppOnly: this route writes waba_id/phone_number_id/
// numero_exibido, fields exclusive to config.TypeWhatsApp (T-111). An
// Instagram instance has no registration via API in this slice — see the
// "Desenho preservado" in docs/TASKS.md for the day it gets one.
func NewRegistrationHandler(store *config.Store, auth *Authenticator, maxBytes int, types AcceptedTypes) http.Handler {
	return newRegistrationHandlerAt(store, auth, maxBytes, time.Now, types)
}

func newRegistrationHandlerAt(store *config.Store, auth *Authenticator, maxBytes int, now func() time.Time, types AcceptedTypes) http.Handler {
	h := &RegistrationHandler{
		store: store, auth: auth, maxBytes: maxBytes, now: now,
		throttleLog: newLogThrottle(logSuppressionWindow),
		types:       types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(registrationRoute, h.register)
	return mux
}

// RegistrationRequest is the body of POST /v1/cadastro.
//
// THE FIELD NAMES ARE THE COLUMN NAMES, and that's not a coincidence: this is how
// `instancia mostrar` saying `app_secret=nao` and the field the consumer
// needs to send have the SAME name, with nobody translating between two screens.
//
// REPLACES, DOES NOT PATCH: registration writes the WHOLE set, and an omitted
// field counts as empty. This is deliberate — a partial patch would force the consumer to
// know what's already written, and the one thing the gateway never returns is
// precisely that (see config.MetaRegistration). The response's `cifrados` exists
// so they can CHECK what stuck, at that same instant, without guessing.
type RegistrationRequest struct {
	Instance      string `json:"instancia"`
	WabaID        string `json:"waba_id"`
	PhoneNumberID string `json:"phone_number_id"`
	DisplayNumber string `json:"numero_exibido"`
	AppSecret     string `json:"app_secret"`
	SendToken     string `json:"token_envio"`
	CallbackURL   string `json:"callback_url"`
	BundleCA      string `json:"bundle_ca"`
}

// ErrRegistrationNoInstance names the FIELD, never the value — it propagates to the
// refusal log (T-037), and this body carries two secrets.
var ErrRegistrationNoInstance = errors.New("campo `instancia` e obrigatorio")

// FieldInRegistration is what the response says about an encrypted field: the name and WHETHER it
// is registered. NEVER the value, not even truncated, not even hashed — see the header of
// registrationResponse.
type FieldInRegistration struct {
	Field      string `json:"campo"`
	Registered bool   `json:"cadastrado"`
}

// WindowInRegistration is the deadline the consumer still has, in TWO instants.
//
// RELATIVE DEADLINE DOES NOT GO IN ("3h left"): it ages inside the response and
// lies to any panel that stores it. Whoever reads it subtracts from their own clock.
type WindowInRegistration struct {
	Open bool `json:"aberta"`
	// FirstInsertAt is what started the clock — the two halves of the
	// rule come from it: it is not the instance's creation date, and it does NOT
	// change when the consumer registers again.
	FirstInsertAt string `json:"primeira_insercao_em"`
	ClosesAt      string `json:"fecha_em"`
}

// RegistrationResponse is the 200 body.
//
// 🔴 NO ENCRYPTED FIELD LEAVES FROM HERE — not whole, not truncated, not hashed.
// This rule has held since T-020 (a secret goes in and does not come back) and this route
// makes no exception, even though it's the only one where the consumer just sent the values
// and "return it to check" would seem useful: an endpoint that returns a credential
// turns a leaked consumer token into theft of their Meta account. What they
// need to check is whether the field ARRIVED, and that's what `cifrados` answers.
//
// The format ONLY GROWS, like the one for /v1/health and GET /v1/estado.
type RegistrationResponse struct {
	Instance string `json:"instancia"`
	// State and Paused are the SAME fact, from the SAME functions as GET /v1/estado
	// (config.StateOf). They are here because the consumer's next
	// question is always "so, does it work now?" — and the answer is no.
	State              string                `json:"estado"`
	Paused             bool                  `json:"pausada"`
	RegistrationWindow WindowInRegistration  `json:"janela_de_cadastro"`
	Encrypted          []FieldInRegistration `json:"cifrados"`
	// NextStep is the support this gateway has: with no channel to ask, the
	// right response has to say what to do next.
	NextStep string `json:"proximo_passo"`
}

// register runs the guards in the SAME order as sending — authenticate -> read the body ->
// validate the schema -> check the link -> write —, with ONE deliberate absence:
// there is NO "active instance" guard. Every instance that reaches here is paused
// by construction (it's born that way and only the smoke test activates it), so requiring
// active would make the route impossible to use for exactly what it exists for.
func (h *RegistrationHandler) register(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar em %s: %v", registrationRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, registrationRoute, "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanente", "corpo grande demais", 0)
			return
		}
		logRejection(h.throttleLog, registrationRoute, "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retentavel", "corpo nao foi lido por inteiro", 0)
		return
	}

	var p RegistrationRequest
	if err := json.Unmarshal(raw, &p); err != nil {
		// THE json ERROR DOES NOT GO into the response or the log: it quotes the piece of
		// the body that didn't match, and this body carries app_secret and token_envio.
		logRejection(h.throttleLog, registrationRoute, "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanente", "corpo nao e JSON valido", 0)
		return
	}
	if p.Instance == "" {
		logRejection(h.throttleLog, registrationRoute, "", consumer.Name, ErrRegistrationNoInstance.Error())
		respondError(w, http.StatusBadRequest, "permanente", ErrRegistrationNoInstance.Error(), 0)
		return
	}

	// THE LINK BEFORE ANYTHING ELSE, and also before saying whether the
	// instance exists: someone else's instance gets 403 and never 404, otherwise this route
	// becomes an oracle that answers "does this slug exist?" to anyone with any
	// token.
	if !CanUse(consumer, p.Instance) {
		log.Printf("zapgw: consumidor %q pediu cadastrar a instancia %q, que nao e dele",
			consumer.Name, p.Instance)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	// T-111: AFTER the link (403) — NEVER before, otherwise this route becomes an
	// oracle of "what type is this slug" for whoever doesn't own it. It needs
	// its own read (the other routes already had `inst` at this point of the
	// flow; this one doesn't, because it writes directly via store.RegisterMeta) —
	// checkType already writes the 400/config when it refuses.
	inst, err := h.store.FindInstance(p.Instance)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			log.Printf("zapgw: consumidor %q tem vinculo com a instancia %q, que NAO existe mais no banco",
				consumer.Name, p.Instance)
			respondError(w, http.StatusNotFound, "config",
				"esta instancia nao existe mais no gateway; fale com quem te entregou o slug", 0)
			return
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q em %s: %v", p.Instance, registrationRoute, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}
	if !checkType(w, h.types, inst, "instancia de Instagram e configurada por quem opera o gateway") {
		return
	}

	window, err := h.store.RegisterMeta(p.Instance, config.MetaRegistration{
		WabaID:        p.WabaID,
		PhoneNumberID: p.PhoneNumberID,
		DisplayNumber: p.DisplayNumber,
		AppSecret:     p.AppSecret,
		SendToken:     p.SendToken,
		CallbackURL:   p.CallbackURL,
		CABundle:      p.BundleCA,
	}, h.now())
	if err != nil {
		h.respondRegistrationError(w, p.Instance, consumer.Name, window, err)
		return
	}

	// SummarizeInstance, and NOT FindInstance: the summary answers "which fields
	// are registered?" WITHOUT decrypting anything. Fetching would put all six secrets in
	// the clear in this handler's memory with no use for them — and the only way
	// to guarantee that nothing encrypted leaves from here is to never have the value in hand.
	summary, err := h.store.SummarizeInstance(p.Instance)
	if err != nil {
		log.Printf("zapgw: cadastro da instancia %q GRAVOU e a leitura do resumo falhou: %v", p.Instance, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel",
			"o cadastro foi gravado, mas o gateway nao conseguiu ler o estado dela para te responder;"+
				" repetir o cadastro e seguro (ele substitui pelo mesmo valor)", 0)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(registrationResponse(summary, window, h.now()))
}

// registrationResponse assembles the 200 body from the SUMMARY — the same source as
// `zapgw instancia mostrar`.
//
// THE LIST OF ENCRYPTED FIELDS IS NOT WRITTEN HERE: it comes from
// config.InstanceSummary.Encrypted, which is where the store says which columns are
// encrypted. A list of its own on this surface would fall behind on the day the
// seventh encrypted column arrived, and the consumer would check a smaller set
// than what they sent with nothing flagging it (the lesson from T-048: every list written
// by hand over the schema needs something that asks the schema).
func registrationResponse(r config.InstanceSummary, j config.Window, now time.Time) RegistrationResponse {
	resp := RegistrationResponse{
		Instance: r.Slug,
		State:    config.StateOf(r),
		Paused:   !r.Active,
		RegistrationWindow: WindowInRegistration{
			Open:          j.IsOpen(now),
			FirstInsertAt: j.OpenedAt.UTC().Format(time.RFC3339),
			ClosesAt:      j.ClosesAt.UTC().Format(time.RFC3339),
		},
		// The instance remains PAUSED, and saying so is half the work of this
		// response: registering proves nothing, SENDING proves. If registering
		// activated it, a wrong credential would turn into an "active" instance that refuses
		// everything — the defect T-074 found.
		//
		// UNTIL T-084 this text said "notify whoever gave you the slug" —
		// there was no channel at all for that notice, and the consumer would stay
		// stuck waiting for someone to act for them. Now there's a route: call it
		// yourself.
		NextStep: "esta instancia continua PAUSADA: enquanto isso, o webhook responde 503 e o envio tambem." +
			" So um envio bem-sucedido a ativa: chame POST /v1/fumaca com esta instancia e o destino que deve" +
			" RECEBER a mensagem de teste — se a Meta aceitar, a instancia ativa sozinha.",
	}
	for _, c := range r.Encrypted {
		resp.Encrypted = append(resp.Encrypted, FieldInRegistration{Field: c.Name, Registered: c.Registered})
	}
	return resp
}

// respondRegistrationError translates the store's errors.
//
// WITH NO CHANNEL TO ASK, THE ERROR MESSAGE IS THE SUPPORT: each branch says WHY
// it refused and WHAT TO DO. An ambiguous terminal error here is a dead end for
// someone the owner cannot reach (docs/MODELO-DE-USO.md).
func (h *RegistrationHandler) respondRegistrationError(w http.ResponseWriter, slug, consumer string, window config.Window, err error) {
	switch {
	case errors.Is(err, config.ErrRegistrationWindowClosed):
		// 409, not 403: 403 is the response for "this instance isn't yours", and using
		// the same code for two different reasons would make the consumer
		// check the token when the problem is the clock. The log goes out WITHOUT
		// throttling: it's a rare event per instance, and it's what the owner looks for
		// when the consumer notifies him.
		log.Printf("zapgw: consumidor %q tentou cadastrar a instancia %q com a janela FECHADA (abriu em %s): %v",
			consumer, slug, window.OpenedAt.UTC().Format(time.RFC3339), err)
		respondError(w, http.StatusConflict, "config",
			"a janela de cadastro desta instancia esta FECHADA e nada foi gravado."+
				" Ela dura "+config.RegistrationWindow.String()+" contados da PRIMEIRA vez que voce cadastrou algo aqui"+
				" (nao da criacao da instancia, e ela NAO reinicia a cada mudanca)."+
				" O que fazer: peca a quem te entregou o slug para reabrir a janela —"+
				" e o comando `zapgw instancia reabrir-cadastro`, do lado do gateway."+
				" A configuracao que ja estava gravada continua valendo; nada foi perdido.", 0)
	case errors.Is(err, config.ErrInstanceNotFound):
		log.Printf("zapgw: consumidor %q tem vinculo com a instancia %q, que NAO existe mais no banco", consumer, slug)
		respondError(w, http.StatusNotFound, "config",
			"esta instancia nao existe mais no gateway; fale com quem te entregou o slug", 0)
	case errors.Is(err, config.ErrIncompleteIdentification),
		errors.Is(err, config.ErrIncompleteRegistration),
		errors.Is(err, config.ErrInsecureCallback),
		errors.Is(err, config.ErrInvalidCABundle):
		// The store's message NAMES the field and never carries the value — that's
		// why it can go whole into the response and the log. See
		// config.ValidateMetaRegistration.
		logRejection(h.throttleLog, registrationRoute, slug, consumer, err.Error())
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
	default:
		log.Printf("zapgw: erro de store ao cadastrar a instancia %q: %v", slug, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
	}
}
