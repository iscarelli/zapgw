// GET, POST and DELETE /v1/templates — the catalog, WHOLE.
//
// WHY IT EXISTS THIS WAY: this network's old gateway returned only the first
// 25 templates of a WABA that had 84. That's what took it out of production.
// The truncation gave no error at all — it returned a plausible, short list,
// the consumer concluded the template "does not exist," and the message never
// went out. Because of that, here:
//
//   - reading paginates until `paging.next` disappears (internal/meta/templates.go);
//   - hitting the page-count ceiling is an ERROR, never a short list with `200`;
//   - the `status` filter is also applied on our side, so that "APPROVED"
//     means APPROVED even if Meta ignores the parameter.
//
// The guard order is the same as sending, and each one exists because the
// previous one is not enough:
//
//	authenticate -> (POST: validate the schema) -> check the binding to the
//	instance -> instance active? -> Meta
//
// The catalog DESCRIBES THE TENANT'S BUSINESS (campaign names, billing text),
// so the binding guard applies here for the same reason as sending.
//
// NO CACHE, on purpose: a template's status changes on Meta without warning
// the gateway, and a cached catalog would answer "APPROVED" about a template
// that just got rejected — the truncation trap wearing a different coat.
package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

// WarningTemplatePending travels on EVERY successful creation.
//
// Without it the consumer tries to use the template right away and gets an
// error from Meta — an error it cannot explain, because the creation
// responded with success.
//
// EXPORTED (T-036): `zapgw template criar` prints the SAME warning — two
// surfaces with the same behavior, not two truths.
const WarningTemplatePending = "template recem-criado NAO pode ser usado na hora: ele nasce PENDING e so " +
	"vale depois de aprovado pela Meta. Confira o status em GET /v1/templates?instancia=<slug> antes de enviar."

// WarningCreationConfirmedByReread travels along with the success the
// gateway RECONSTRUCTS after a creation that ended with no response from
// Meta (T-078).
//
// It is not cosmetic: the consumer needs to know that its call failed
// midway, because that's what explains the delay and what it will find in
// its own log. Hiding the scare would deliver a clean `201` over a path that
// was not clean.
const WarningCreationConfirmedByReread = "a criacao terminou SEM resposta da Meta, mas o gateway releu o " +
	"catalogo e ACHOU este template: ele FOI criado. NAO tente criar de novo com o mesmo nome."

// MessageInconclusiveOutcome is what the consumer reads when the creation
// ended with no response AND the catalog re-read did NOT find the template.
//
// THE WORD "INCONCLUSIVE" IS THE PAYLOAD OF THIS CONSTANT, and it is the
// result of a question that was ASKED OF THE SOURCE on 2026-07-28 (T-078)
// and came back unanswered:
//
//	Meta documents read-after-write for the RESPONSE OF THAT SAME POST edge —
//	"This endpoint supports read-after-write and will read the node to
//	which you POSTed"
//	(developers.facebook.com/docs/graph-api/reference/whats-app-business-account/message_templates/).
//	It does NOT document, on any page it was possible to read, that a
//	`GET /{waba}/message_templates` made AFTERWARD already contains the new
//	template — and it also does not document the opposite. NOT DOCUMENTED
//	EITHER WAY.
//
// And the guarantee that exists is precisely the one that does NOT apply
// here: it holds for the POST's response, which in this outcome is exactly
// what did not arrive. (For the same reason the doc's advice — save the
// `id` and query `GET /{id}` — does not apply: with no response, there is no
// id.)
//
// Without that guarantee, "didn't find it" is NOT "doesn't exist," and the
// error is ASYMMETRIC: saying "I don't know" costs the consumer one check;
// saying "it was not created" makes it repeat the creation — and name +
// language are UNIQUE per WABA, with the repeat coming back `code 100` /
// `subcode 2388024`
// (developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management),
// meaning the name becomes unusable.
const MessageInconclusiveOutcome = "a criacao do template terminou sem resposta da Meta; o gateway releu o " +
	"catalogo e NAO o encontrou, mas o desfecho e INCONCLUSIVO — nao esta provado que ele deixou de ser criado. " +
	"NAO repita a criacao com o mesmo nome as cegas: se ele existir, o nome fica queimado e a Meta nao o reaceita. " +
	"Consulte GET /v1/templates?instancia=<slug> daqui a alguns minutos antes de decidir."

// MessageUnknownOutcome is the outcome for when even the re-read
// failed — the gateway could not reach Meta through any path.
const MessageUnknownOutcome = "falha ao falar com a Meta; o template PODE ter sido criado e a releitura do " +
	"catalogo feita pelo gateway TAMBEM falhou. Confira o catalogo antes de tentar de novo."

// WarningCategoryChangedFormat travels APPENDED to the warning when the
// category Meta RECORDED differs from the one the consumer REQUESTED
// (T-108, field report from consumer-b on 2026-07-30: they submitted
// `instagram_continuar` as UTILITY and Meta recorded MARKETING — no error, no
// warning, and they only found out by re-reading the catalog afterward).
//
// Category decides BILLING (MARKETING and UTILITY have different prices) and
// whether the message needs opt-in — a silent swap is money and compliance,
// not aesthetics, and the rule in CLAUDE.md ("NINGUÉM fala direto com a Meta")
// takes away the consumer's chance to notice on its own: the one who has to
// report it is the gateway.
//
// It is a FORMAT (%q on both sides), not a fixed constant like the others in
// this file, because the value changes with every request — composed with
// fmt.Sprintf and concatenated to `aviso` the SAME way the code already
// composes it today (WarningTemplatePending + " " + WarningCreationConfirmedByReread):
// there is no second warning mechanism, just more text in the same field.
const WarningCategoryChangedFormat = "a categoria PEDIDA foi %q, mas a Meta GRAVOU %q. Isso NAO e erro — a Meta " +
	"pode recategorizar um template na propria criacao — e o gateway NAO desfaz a troca. Confira se o preco e a " +
	"exigencia de opt-in da categoria gravada ainda valem antes de usar este template."

// --- DELETION (T-173) ---

// The three outcomes of DELETE /v1/templates, and they are DISTINGUISHABLE on
// purpose. Answering `200 {}` to both of the successful ones was exactly what
// consumer-b asked us not to do (2026-08-28): it has 61 approved templates to
// take off the account, the cleanup will be interrupted and resumed, and a
// report that cannot separate "I deleted it now" from "it was already gone"
// is a report that cannot be closed.
//
// `inconclusivo` is NOT in this list because it is not a `200`: it is an
// ERROR response, with the SAME status (502) and the SAME class
// (`desconhecido`) the ambiguous creation already uses — see
// MessageInconclusiveDeletion.
const (
	OutcomeDeleted     = "apagado"
	OutcomeDidNotExist = "ja_nao_existia"
)

// StatusPendingDeletion is Meta's literal value, not a name of ours.
//
// 🔴 IT IS WHY "STILL IN THE CATALOG" IS NOT "NOT DELETED", and that is the
// whole reason this constant exists instead of an inline string. Verbatim
// from the source, read on 2026-08-28:
//
//	"If you delete a template that has been sent in a template message but
//	has yet to be delivered (for example, because the WhatsApp user's phone
//	is turned off), the template's status is set to PENDING_DELETION and
//	WhatsApp attempts delivery for 30 days."
//	https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management
//
// So a deletion Meta ACCEPTED can leave the name in the catalog. Reading that
// as doubt would put a template that was correctly deleted into the
// consumer's "I don't know" pile — on a 61-item cleanup, the pile is the
// product.
const StatusPendingDeletion = "PENDING_DELETION"

// WarningNameBurnedForThirtyDays travels on EVERY successful deletion —
// and ONLY on `apagado`, never on `ja_nao_existia`.
//
// THE 30 DAYS ARE META'S, WITH A SOURCE. Verbatim, read on 2026-08-28:
//
//	"If you delete an approved template, you cannot create a new template
//	with the same name for 30 days."
//	https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management
//
// WHY IT DOES NOT TRAVEL ON `ja_nao_existia`: there the gateway did not
// delete anything and does not know whether the name EVER existed. Telling a
// consumer that a name it never used is burned for 30 days would be the
// gateway inventing a restriction — and this warning's whole value is that
// it is a fact from the source.
const WarningNameBurnedForThirtyDays = "a exclusao apaga o template em TODOS os idiomas, e a Meta NAO aceita " +
	"criar um template com o MESMO nome por 30 dias. Se precisar recriar antes disso, escolha outro nome."

// WarningStillInCatalogPendingDelivery is APPENDED to the warning above
// when the deleted template is STILL in the catalog with StatusPendingDeletion.
//
// The consumer is going to open `GET /v1/templates` and see a template it
// just deleted. Without this line, the only reading available to it is "the
// deletion did not work" — and it would redo work that was already done, or
// open a ticket about a defect that does not exist.
const WarningStillInCatalogPendingDelivery = "este template CONTINUA aparecendo no catalogo com status " +
	StatusPendingDeletion + ": havia mensagem ja enviada e ainda nao entregue, e a Meta segue tentando " +
	"entrega-la por ate 30 dias. A exclusao FOI aceita; ele sai do catalogo sozinho."

// MessageInconclusiveDeletion is what the consumer reads when the deletion
// ended with NO response from Meta and the catalog re-read STILL shows the
// template under a status other than PENDING_DELETION.
//
// SAME WORD, SAME STATUS AND SAME REASONING AS THE AMBIGUOUS CREATION (see
// MessageInconclusiveOutcome): "I didn't see it happen" is not "it didn't
// happen", and the error is asymmetric — saying "not deleted" makes the
// consumer repeat a call that may have already worked.
const MessageInconclusiveDeletion = "a exclusao do template terminou sem resposta da Meta; o gateway releu o " +
	"catalogo e o template CONTINUA la, com um status que NAO e " + StatusPendingDeletion + ". O desfecho e " +
	"INCONCLUSIVO — nao esta provado que a exclusao deixou de acontecer. Consulte " +
	"GET /v1/templates?instancia=<slug> daqui a alguns minutos antes de decidir."

// MessageUnknownDeletion is the outcome for when even the re-read
// failed — the gateway could not reach Meta through any path.
const MessageUnknownDeletion = "falha ao falar com a Meta; o template PODE ter sido apagado e a releitura " +
	"do catalogo feita pelo gateway TAMBEM falhou. Confira o catalogo antes de tentar de novo."

// reTemplateName is the guard that makes a WILDCARD IMPOSSIBLE BY
// CONSTRUCTION, and that is the only reason it is this strict.
//
// 🔴 THE DELETION IS BY NAME AND HAS NO UNDO. If Meta ever accepted `*`, `%`
// or any other pattern in `name`, ONE call would take out a whole family of
// templates — and nothing in this gateway, or on the consumer's side, could
// put them back: a template is only recreated by hand, it is reapproved by
// Meta on Meta's schedule, and the name itself stays blocked for 30 days
// (see WarningNameBurnedForThirtyDays). It was NOT verified against the
// source that Meta refuses a pattern here — and a guarantee this expensive
// cannot rest on an unverified assumption about someone else's behavior.
//
// The character set is the one Meta documents for a template name (lowercase
// letters, digits and underscore), so nothing legitimate is refused by it.
// The refusal happens BEFORE any call to Meta, which is what makes it a
// guard and not a message.
var reTemplateName = regexp.MustCompile(`^[a-z0-9_]{1,512}$`)

// normalizeCategory decides when two spellings of category count as THE
// SAME category: trims spaces and ignores CASE. It exists so T-108's
// comparator (requested vs. recorded) does not flag a swap because of
// "utility" vs "UTILITY" — different spelling is not a different category.
//
// Used on BOTH sides of the comparison (the requested and the recorded), on
// BOTH success paths of this file (the normal creation, in criar, and the
// one reconstructed by the catalog re-read, in respondAmbiguousOutcome,
// T-101/T-078): a second normalization rule here would diverge from the
// first on the next change — this project's mother trap (docs/ARMADILHAS.md).
func normalizeCategory(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// RereadWaits are the SPACED-OUT pauses between SUCCESSIVE catalog
// re-reads, used only when the creation ended with no response from Meta AND
// the FIRST re-read (immediate, since T-078) did not find the template
// (T-101).
//
// WHY THEY EXIST: a field report from consumer-b (2026-07-30) showed the
// MOST LIKELY case of this path — the template WAS created and propagated
// into Meta's catalog in under a minute — coming out with the SAME FACE as
// the rare and expensive outcome (it was not created). A single immediate
// re-read does not give propagation time to happen; retrying, spaced out,
// resolves the common case without loosening the warning for the rare case —
// MessageInconclusiveOutcome stays EXACTLY the same.
//
// 2s / 5s / 10s, and NOT the consumer's original suggestion (2s / 5s / 15s):
// the sum enters into the "prazo com folga (30 s é confortável)" deadline the
// CONTRACT tells its client to use (docs/CONTRATO-CONSUMIDOR.md) — and that
// sum covers ONLY the pauses, without counting the original creation attempt
// or the network time of each re-read, which also consume that budget.
// 2+5+15=22s blows the slack; 2+5+10=17s leaves margin. See
// RereadWaitCap, the number that goes into the contract.
var RereadWaits = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

// RereadWaitCap is the SUM of RereadWaits: the maximum PURE
// wait (not counting network) this path can add to a request. It is the
// number the contract promises in writing, in seconds, for the consumer to
// size its client's timeout with FACT instead of estimate (item 2 of T-101).
var RereadWaitCap = sumOfDurations(RereadWaits)

func sumOfDurations(ds []time.Duration) time.Duration {
	var total time.Duration
	for _, d := range ds {
		total += d
	}
	return total
}

// waitReread is the pause between a re-read that did NOT find it and
// the next one. Package var, in the SAME pattern as creationClock
// (cmd/zapgw/provisionar.go): the test swaps the function for a spy that does
// NOT actually sleep — a test that slept the up-to-17s ceiling would stall the
// whole suite (docs/ARMADILHAS.md forbids a test that actually sleeps).
var waitReread = waitWithContext

// waitWithContext sleeps `d`, but stops RIGHT AWAY if `ctx` is canceled —
// select over time.NewTimer and ctx.Done(), never plain time.Sleep. If the
// consumer gives up on the request mid-wait, the gateway does not keep
// sleeping until the end for no purpose: the next call to Meta will fail
// fast because of the SAME canceled context, and the outcome becomes unknown
// (or inconclusive) without spending the seconds that were left.
func waitWithContext(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

type TemplatesHandler struct {
	store    *config.Store
	auth     *Authenticator
	client   *meta.Client
	maxBytes int
	// throttleLog suppresses repeated logging of a VALIDATION refusal (T-037)
	// — see logThrottle and logRejection in handler.go. It is not "state" in
	// the sense of the comment below (no catalog is cached here); it is only
	// the record of WHEN the last refusal of each (route, consumer) logged.
	throttleLog *logThrottle
	// counter counts the deletions (T-173). WITHOUT IT, A SERIES DELETION
	// IS INVISIBLE: `GET /v1/estado` would show a WABA losing dozens of
	// templates with nothing in the gateway saying it was us. The route
	// itself runs about once a year, so the counter is the only trace that
	// survives the log's retention.
	counter *config.Counter
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go.
	types AcceptedTypes
}

// NewTemplatesHandler assembles the three routes. It does NOT keep CATALOG
// state: there is no cache at all here, and that is how the absence of cache
// is proven by construction.
//
// `types` is WhatsAppOnly: list(), create() and deleteTemplate() use inst.WabaID, a
// field that only exists on config.TypeWhatsApp — empty on any Instagram
// instance (T-111). Instagram has no template in this slice (the same
// restriction sending already applies — see handler.go, "text type only").
func NewTemplatesHandler(
	store *config.Store, auth *Authenticator, client *meta.Client, maxBytes int,
	counter *config.Counter, types AcceptedTypes,
) http.Handler {
	h := &TemplatesHandler{
		store: store, auth: auth, client: client, maxBytes: maxBytes,
		throttleLog: newLogThrottle(logSuppressionWindow),
		counter:     counter,
		types:       types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/templates", h.list)
	mux.HandleFunc("POST /v1/templates", h.create)
	mux.HandleFunc("DELETE /v1/templates", h.deleteTemplate)
	return mux
}

type templatesResponse struct {
	Instance  string          `json:"instancia"`
	Total     int             `json:"total"`
	Templates []meta.Template `json:"templates"`
}

// CreateTemplateRequest is the body of POST /v1/templates, in the gateway's
// names.
//
// EXPORTED (T-036): the `zapgw template criar` command talks to the local
// store instead of building an HTTP call against this route, but it has to
// reject an invalid components file with the SAME rule as this route, not a
// copy of it — two rules diverge on the first change (docs/ARMADILHAS.md,
// this project's mother trap), a function called from both places does
// not.
type CreateTemplateRequest struct {
	Instance   string          `json:"instancia"`
	Name       string          `json:"nome"`
	Category   string          `json:"categoria"`
	Language   string          `json:"idioma"`
	Components json.RawMessage `json:"componentes"`
	// AllowCategoryChange is OPTIONAL and PASSED THROUGH VERBATIM to Meta
	// (T-108) — the gateway does not validate it, does not interpret it,
	// does not translate it; who decides the effect is Meta. Pointer, and
	// not `bool`, BY CONSTRUCTION: it is the only way to distinguish "the
	// consumer did not send the field" (nil, the gateway sends NOTHING to
	// Meta, byte for byte the same as before this task) from "the consumer
	// sent false" (Meta receives `false`). With a plain `bool` the two cases
	// would collapse into the same `false`, and EVERY consumer that today
	// does not use the field would start sending a value it never asked for.
	AllowCategoryChange *bool `json:"allow_category_change,omitempty"`
}

// templateCreatedResponse is the `201` of the creation.
//
// The `id` has been `omitempty` since T-078, and on the normal path this
// never fires: meta.CreateTemplate already rejects a `2xx` with no id
// (ErrTemplateWithoutID). It exists for the one path that can reach here with no
// id — the creation reconstructed by the catalog re-read, when the item
// found did not bring an `id`. There, sending `"id": ""` would give the
// consumer a value that LOOKS like an id to save; sending no field at all
// tells the truth, which is "it exists, and I don't know the id."
type templateCreatedResponse struct {
	ID       string `json:"id,omitempty"`
	Status   string `json:"status,omitempty"`
	Category string `json:"categoria,omitempty"`
	// RequestedCategory is the echo of the REQUEST's `categoria` field —
	// WITHOUT omitempty, unlike the fields above. `categoria` is a MANDATORY
	// field of the request (Validate rejects a request without it, lines
	// 365-366), so there is no "legitimately empty" case that would justify
	// hiding it: a field that only shows up on a bad day is a field the
	// consumer's parser learns about on a bad day (T-108). It exists for the
	// consumer to compare against `categoria` (what Meta RECORDED) without
	// depending on free text — see WarningCategoryChangedFormat.
	RequestedCategory string `json:"categoria_pedida"`
	Warning           string `json:"aviso"`
	// Rereads and WaitSeconds are only born != 0 on the T-101 path —
	// normal creation (no ambiguous outcome) never re-read the catalog, and
	// `omitempty` hides them on that path instead of showing a misleading
	// zero. The consumer needs to know HOW MANY TIMES the gateway re-read
	// the catalog and HOW LONG it waited between them to calibrate its own
	// timeout (item 5 of T-101).
	Rereads     int `json:"releituras,omitempty"`
	WaitSeconds int `json:"espera_segundos,omitempty"`
}

func (h *TemplatesHandler) list(w http.ResponseWriter, r *http.Request) {
	consumer, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("instancia"))
	if slug == "" {
		logRejection(h.throttleLog, "GET /v1/templates", "", consumer.Name,
			"parametro instancia e obrigatorio")
		respondError(w, http.StatusBadRequest, "permanente",
			"parametro instancia e obrigatorio", 0)
		return
	}
	inst, ok := h.instanceActive(w, consumer, slug, "GET /v1/templates")
	if !ok {
		return
	}

	// The deadline covers PAGINATION AS A WHOLE. A catalog that does not fit
	// in it becomes an error; never the partial list read until the deadline
	// blows up.
	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	catalog, err := h.client.ListTemplates(ctx, inst.WabaID, inst.SendToken,
		r.URL.Query().Get("status"))
	if err != nil {
		h.respondCatalogError(w, inst.Slug, err)
		return
	}
	if catalog == nil {
		// `[]`, never `null`: a `null` forces every consumer to handle two
		// different empties, and whoever forgets breaks on the first client
		// with no template at all.
		catalog = []meta.Template{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(templatesResponse{
		Instance:  inst.Slug,
		Total:     len(catalog),
		Templates: catalog,
	})
}

func (h *TemplatesHandler) create(w http.ResponseWriter, r *http.Request) {
	consumer, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			logRejection(h.throttleLog, "POST /v1/templates", "", consumer.Name, "corpo grande demais")
			respondError(w, http.StatusRequestEntityTooLarge, "permanente", "corpo grande demais", 0)
			return
		}
		// Same reasoning as sending: it's not "body too large," it's the
		// consumer's connection that dropped mid-upload. 400 because what
		// arrived incomplete was the REQUEST, and retryable because retrying
		// resolves it.
		logRejection(h.throttleLog, "POST /v1/templates", "", consumer.Name, "corpo nao foi lido por inteiro")
		respondError(w, http.StatusBadRequest, "retentavel", "corpo nao foi lido por inteiro", 0)
		return
	}

	// T-203 (step 2 of T-189): accept the English name of every ENTRADA key
	// this route has (docs/MIGRACAO-CONTRATO-EN.md), translated to the
	// canonical (Portuguese) form BEFORE unmarshaling.
	translated, oldNames, ok := translateEntradaOrReject(
		w, h.throttleLog, "POST /v1/templates", consumer.Name, raw, createTemplateAlias)
	if !ok {
		return
	}

	var p CreateTemplateRequest
	if err := json.Unmarshal(translated, &p); err != nil {
		logRejection(h.throttleLog, "POST /v1/templates", "", consumer.Name, "corpo nao e JSON valido")
		respondError(w, http.StatusBadRequest, "permanente", "corpo nao e JSON valido", 0)
		return
	}
	// Validate BEFORE talking to Meta: sending a request already known to be
	// broken spends the instance's quota and returns the consumer a message
	// that is not ours, about fields it did not even write under those
	// names.
	if err := p.Validate(); err != nil {
		// The message names the FIELD, never the value — same guarantee as
		// sending (handler.go), and the same one that makes it safe to log
		// `err.Error()` (T-037).
		logRejection(h.throttleLog, "POST /v1/templates", p.Instance, consumer.Name, err.Error())
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
		return
	}
	if len(oldNames) > 0 {
		h.counter.Record(p.Instance, config.CounterOldNameUsed)
	}

	inst, ok := h.instanceActive(w, consumer, p.Instance, "POST /v1/templates")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	created, err := h.client.CreateTemplate(ctx, inst.WabaID, inst.SendToken, meta.TemplateRequest{
		Name:                p.Name,
		Category:            p.Category,
		Language:            p.Language,
		Components:          p.Components,
		AllowCategoryChange: p.AllowCategoryChange,
	})
	if err != nil {
		h.respondCreationError(w, r, inst, p.Name, p.Language, p.Category, err)
		return
	}

	resp := templateCreatedResponse{
		ID:                created.ID,
		Status:            created.Status,
		Category:          created.Category,
		RequestedCategory: p.Category,
		// The warning is OURS and always goes out; the `status` above is
		// what META responded. Swapping the warning for a status read would
		// leave the consumer without instructions on the day it responds
		// with something else.
		Warning: WarningTemplatePending,
	}
	// T-108: Meta can RECORD a category different from the one REQUESTED,
	// with no error and no warning of its own — the gateway is the one that
	// has to report it.
	if normalizeCategory(p.Category) != normalizeCategory(created.Category) {
		resp.Warning += " " + fmt.Sprintf(WarningCategoryChangedFormat, p.Category, created.Category)
	}
	respondTemplateCreated(w, resp)
}

// templateEntry is ONE catalog line the deletion took out (or found
// still there). The consumer needs the language: Meta deletes BY NAME IN ALL
// LANGUAGES, so a name it thought was one template can be four.
type templateEntry struct {
	ID       string `json:"id,omitempty"`
	Language string `json:"idioma"`
	Category string `json:"categoria,omitempty"`
	Status   string `json:"status,omitempty"`
}

// templateDeletedResponse is the `200` of the deletion, in BOTH successful
// outcomes.
type templateDeletedResponse struct {
	Instance string `json:"instancia"`
	Name     string `json:"nome"`
	// Outcome is OutcomeDeleted or OutcomeDidNotExist — see the comment
	// on those constants for why the two cannot collapse into one `200 {}`.
	Outcome string `json:"desfecho"`
	// Entries is `[]`, never `null`, for the same reason as the catalog
	// list: a `null` forces every consumer to handle two different empties,
	// and `ja_nao_existia` is precisely the outcome where it is empty.
	Entries []templateEntry `json:"entradas"`
	Warning string          `json:"aviso,omitempty"`
	// Rereads and WaitSeconds are only born != 0 on the ambiguous
	// path (the deletion went out and no verdict came back, and the catalog
	// re-read is what reconstructed the outcome) — same fields, same
	// meaning and same `omitempty` as templateCreatedResponse.
	Rereads     int `json:"releituras,omitempty"`
	WaitSeconds int `json:"espera_segundos,omitempty"`
}

// apagar removes ONE template, BY NAME, from the instance's WABA.
//
// WHY IT EXISTS (T-173, 2026-08-28): consumer-b has 61 approved templates to
// take off the account, and until this route that was hand work in the
// WhatsApp Manager. Since NOBODY talks to Meta directly (CLAUDE.md), the
// absence of the route was what pushed the consumer back to the panel.
//
// The guards are the SAME ones, in the SAME order, as the two sisters:
// authenticate -> validate the parameters -> binding/existence/pause
// (instanceActive) -> type. Validating the parameters BEFORE instanceActive
// is what create() already does with the body, and it is what keeps a
// malformed request from spending a store read.
func (h *TemplatesHandler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	consumer, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("instancia"))
	if slug == "" {
		logRejection(h.throttleLog, "DELETE /v1/templates", "", consumer.Name,
			"parametro instancia e obrigatorio")
		respondError(w, http.StatusBadRequest, "permanente",
			"parametro instancia e obrigatorio", 0)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("nome"))
	if name == "" {
		logRejection(h.throttleLog, "DELETE /v1/templates", slug, consumer.Name,
			"parametro nome e obrigatorio")
		respondError(w, http.StatusBadRequest, "permanente",
			"parametro nome e obrigatorio", 0)
		return
	}
	if !reTemplateName.MatchString(name) {
		// The refusal names the RULE, never the value — same guarantee as
		// the other routes, and the same one that makes it safe to log.
		// See the comment on reTemplateName for why this is a guard and
		// not a nicety.
		logRejection(h.throttleLog, "DELETE /v1/templates", slug, consumer.Name,
			"parametro nome invalido")
		respondError(w, http.StatusBadRequest, "permanente",
			"parametro nome so aceita letras minusculas, digitos e _ (ate 512 caracteres); "+
				"curinga e caractere especial sao recusados aqui, antes de qualquer chamada a Meta, "+
				"porque a exclusao e por nome e nao tem desfazer", 0)
		return
	}

	inst, ok := h.instanceActive(w, consumer, slug, "DELETE /v1/templates")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	// READ THE CATALOG FIRST, and no status filter: this is what makes the
	// route IDEMPOTENT IN FACT, not just in name. A 61-template cleanup gets
	// interrupted and resumed, and the second pass over a name already gone
	// has to answer `ja_nao_existia` — not an error from Meta the consumer
	// has to interpret, and not a `200` indistinguishable from a real
	// deletion.
	//
	// It is also what fills `entradas`: after the DELETE the lines are gone,
	// so WHAT was deleted can only be told by whoever looked before.
	catalog, err := h.client.ListTemplates(ctx, inst.WabaID, inst.SendToken, "")
	if err != nil {
		h.respondCatalogError(w, inst.Slug, err)
		return
	}
	found := entriesWithName(catalog, name)
	if len(found) == 0 {
		// NO CALL TO META AT ALL. Deleting a name that is not there would
		// spend the instance's quota to receive an error about something
		// the consumer already got right.
		log.Printf("zapgw: exclusao de template na instancia %q: o nome %q NAO esta no catalogo — "+
			"nada foi pedido a Meta (desfecho %s)", inst.Slug, name, OutcomeDidNotExist)
		respondTemplateDeleted(w, templateDeletedResponse{
			Instance: inst.Slug,
			Name:     name,
			Outcome:  OutcomeDidNotExist,
			Entries:  []templateEntry{},
		})
		return
	}

	if err := h.client.DeleteTemplate(ctx, inst.WabaID, inst.SendToken, name); err != nil {
		h.respondDeletionError(w, r, inst, name, found, err)
		return
	}
	h.respondDeleted(w, inst, name, found, nil, 0, 0)
}

// respondDeleted is the ONE place that answers `apagado` — the direct path
// and the one reconstructed by the catalog re-read both come through here.
//
// Two copies would diverge on the first change, and the divergence would land
// exactly on the counter and the warning: this project's mother trap
// (docs/ARMADILHAS.md).
//
// `rest` is what the re-read still found under that name (nil on the
// direct path); it is only ever non-empty in the PENDING_DELETION case, which
// is the one the consumer has to be told about.
func (h *TemplatesHandler) respondDeleted(
	w http.ResponseWriter, inst config.Instance, name string,
	found []templateEntry, rest []templateEntry, attempts int, want time.Duration,
) {
	// The counter goes up on `apagado` ONLY — `ja_nao_existia` deleted
	// nothing, and counting it would make the number stop answering "how
	// many templates did this gateway take off the account?".
	h.counter.Record(inst.Slug, config.CounterTemplatesDeleted)

	// The template NAME goes into the log; the URL and the query do NOT.
	// The name is a technical identifier and is exactly what the next person
	// needs in order to search the catalog; the URL carries the waba_id and
	// travels next to the token.
	log.Printf("zapgw: template %q APAGADO na instancia %q — %d entrada(s) no catalogo antes: %s",
		name, inst.Slug, len(found), languagesOf(found))

	resp := templateDeletedResponse{
		Instance:    inst.Slug,
		Name:        name,
		Outcome:     OutcomeDeleted,
		Entries:     found,
		Warning:     WarningNameBurnedForThirtyDays,
		Rereads:     attempts,
		WaitSeconds: int(want.Seconds()),
	}
	if len(rest) > 0 {
		resp.Warning += " " + WarningStillInCatalogPendingDelivery
	}
	respondTemplateDeleted(w, resp)
}

func respondTemplateDeleted(w http.ResponseWriter, resp templateDeletedResponse) {
	if resp.Entries == nil {
		resp.Entries = []templateEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// entriesWithName collects EVERY catalog line with that name — never just the
// first.
//
// Verbatim from the reference (read on 2026-08-28): the deletion "Deletes
// templates matching the name in all languages". Returning one line would
// under-report what the call actually took out, and the consumer's cleanup
// report would be short by exactly the languages nobody looked at.
func entriesWithName(catalog []meta.Template, name string) []templateEntry {
	var found []templateEntry
	for i := range catalog {
		if !strings.EqualFold(strings.TrimSpace(catalog[i].Name), name) {
			continue
		}
		found = append(found, templateEntry{
			ID:       catalog[i].ID,
			Language: catalog[i].Language,
			Category: catalog[i].Category,
			Status:   catalog[i].Status,
		})
	}
	return found
}

// languagesOf is the log's short form of what was deleted. Language and status
// only — no component text, which is what goes to the tenant's end customer.
func languagesOf(entries []templateEntry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, e.Language+"/"+e.Status)
	}
	return strings.Join(parts, ", ")
}

// deletionAccepted decides whether what the catalog STILL shows under the
// deleted name counts as "Meta accepted the deletion".
//
// 🔴 THE PENDING_DELETION HALF IS THE POINT, and it is a correction to the
// obvious rule ("still there = didn't work"). Verbatim from the source, read
// on 2026-08-28:
//
//	"If you delete a template that has been sent in a template message but
//	has yet to be delivered (for example, because the WhatsApp user's phone
//	is turned off), the template's status is set to PENDING_DELETION and
//	WhatsApp attempts delivery for 30 days."
//	https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management
//
// So a deletion Meta ACCEPTED leaves the name visible. Reading that as doubt
// would report `inconclusivo` about a deletion that was right all along — and
// on a 61-name cleanup, the "I don't know" pile is what the consumer has to
// work through by hand afterward.
//
// ALL remaining lines have to be PENDING_DELETION, not just one: Meta deletes
// every language at once, so a single line under another status means the
// call did not do what it says on the tin, and that is doubt, not success.
func deletionAccepted(rest []templateEntry) bool {
	for _, e := range rest {
		if !strings.EqualFold(strings.TrimSpace(e.Status), StatusPendingDeletion) {
			return false
		}
	}
	return true
}

// respondDeletionError translates a DELETION failure — the same shape, and
// the same reasoning, as respondCreationError.
//
// The difference from reading is the same one creation has: the deletion MAY
// have happened on Meta's side. Calling it `retentavel` would send the
// consumer to retry blindly, and by the "NINGUÉM fala direto com a Meta" rule
// it has no second door to check through. Who checks is us, in
// respondAmbiguousDeletion.
func (h *TemplatesHandler) respondDeletionError(
	w http.ResponseWriter, r *http.Request, inst config.Instance,
	name string, found []templateEntry, err error,
) {
	switch {
	case errors.Is(err, meta.ErrInvalidWabaID):
		log.Printf("ALARME zapgw: waba_id invalido para a instancia %q — corrija o waba_id no store; "+
			"nenhuma exclusao de template desta instancia funciona ate la", inst.Slug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; "+
				"o pedido nao chegou a Meta e nao adianta repetir ate isso ser corrigido", 0)
	case errors.Is(err, meta.ErrDeletionNotConfirmed):
		// A `2xx` without `success: true` is AMBIGUOUS for the same reason
		// as transport, and falls into the SAME handling — exactly as
		// ErrTemplateWithoutID does on the creation. The asymmetry "the rule
		// holds in one branch and not in its neighbor" is this project's
		// mother trap.
		h.respondAmbiguousDeletion(w, r, inst, name, found, err)
	default:
		var me *meta.MetaError
		if errors.As(err, &me) {
			// Meta RESPONDED. There is no ambiguity to resolve, and
			// re-reading the catalog here would only spend the instance's
			// quota.
			//
			// This is also the branch that carries the refusals Meta
			// documents for this edge — a disabled template cannot be
			// deleted, for one. The gateway does NOT pre-validate status for
			// it: the consumer needs to read what Meta said, not our guess
			// at what it would say.
			if me.Class == meta.ClassConfig {
				log.Printf("ALARME zapgw: credencial da instancia %q recusada pela Meta ao apagar template", inst.Slug)
			}
			respondMetaError(w, statusForClass(me.Class), me)
			return
		}
		h.respondAmbiguousDeletion(w, r, inst, name, found, err)
	}
}

// respondAmbiguousDeletion handles the one class of outcome where the gateway
// does not know what happened on the other side: the deletion went out from
// here and no verdict came back (transport down, deadline exceeded, `2xx`
// without `success`).
//
// It is the SAME machinery as the ambiguous creation (T-078/T-101) —
// catalogReread, RereadWaits, respondErrorWithWait, the same
// `502`, the same `desconhecido` class and the same `releituras` /
// `espera_segundos` fields — with the QUESTION inverted: creation re-reads
// until it FINDS the template, deletion re-reads until the name stops being
// there under a live status.
//
// THE ACCEPTED COST, the same one written on respondAmbiguousOutcome: a
// request that lands here can take up to TWO instance deadlines plus
// RereadWaitCap.
func (h *TemplatesHandler) respondAmbiguousDeletion(
	w http.ResponseWriter, r *http.Request, inst config.Instance,
	name string, found []templateEntry, cause error,
) {
	// (1) THE REAL ERROR, ALWAYS, BEFORE anything else that might also fail.
	// It was the absence of exactly this line that made the 2026-07-28
	// creation outcome structurally undiagnosable (see respondAmbiguousOutcome).
	log.Printf("zapgw: exclusao de template terminou SEM VEREDITO da Meta na instancia %q (template %q): "+
		"%v — relendo o catalogo para descobrir se ela aconteceu", inst.Slug, name, cause)

	rest, attempts, want, err := h.spacedRereadsForDeletion(r.Context(), inst, name)
	switch {
	case err != nil:
		// BOTH FAILURES LOGGED, in one line: the deletion's cause above and
		// the reading's cause here.
		log.Printf("zapgw: a releitura do catalogo da instancia %q TAMBEM falhou apos a exclusao ambigua do "+
			"template %q (tentativa %d de releitura, %s de espera acumulada): %v — o desfecho continua desconhecido",
			inst.Slug, name, attempts, want, err)
		respondErrorWithWait(w, http.StatusBadGateway, string(meta.ClassUnknown),
			MessageUnknownDeletion, attempts, want)

	case deletionAccepted(rest):
		// NOT AN ERROR: the name is gone, or it is there under
		// PENDING_DELETION — which Meta documents as the ACCEPTED deletion of
		// a template with delivery still in flight. What failed was our
		// reading of the response.
		log.Printf("zapgw: a releitura do catalogo da instancia %q confirmou a exclusao do template %q na "+
			"tentativa %d (apos %s de espera): %d entrada(s) restante(s) [%s] — a exclusao TINHA acontecido",
			inst.Slug, name, attempts, want, len(rest), languagesOf(rest))
		h.respondDeleted(w, inst, name, found, rest, attempts, want)

	default:
		// STILL THERE, ALIVE, IN EVERY ATTEMPT. This does NOT authorize
		// saying "it was not deleted" — see MessageInconclusiveDeletion.
		log.Printf("zapgw: a releitura do catalogo da instancia %q ainda ACHA o template %q [%s] em %d "+
			"tentativas espacadas (%s de espera) — desfecho INCONCLUSIVO, e nao 'nao foi apagado'",
			inst.Slug, name, languagesOf(rest), attempts, want)
		respondErrorWithWait(w, http.StatusBadGateway, string(meta.ClassUnknown),
			MessageInconclusiveDeletion, attempts, want)
	}
}

func respondTemplateCreated(w http.ResponseWriter, resp templateCreatedResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// Validate trims and requires the four fields. Trims BEFORE deciding, and
// assigns the trimmed value — otherwise the spaces travel to Meta
// (docs/ARMADILHAS.md, "Validação": presence is not content).
//
// EXPORTED (T-036) so `zapgw template criar` calls the SAME function as this
// route — see the comment on CreateTemplateRequest.
func (p *CreateTemplateRequest) Validate() error {
	p.Instance = strings.TrimSpace(p.Instance)
	p.Name = strings.TrimSpace(p.Name)
	p.Category = strings.TrimSpace(p.Category)
	p.Language = strings.TrimSpace(p.Language)

	switch {
	case p.Instance == "":
		return errors.New("campo instancia e obrigatorio")
	case p.Name == "":
		return errors.New("campo nome e obrigatorio")
	case p.Category == "":
		return errors.New("campo categoria e obrigatorio")
	case p.Language == "":
		return errors.New("campo idioma e obrigatorio")
	}

	// `componentes` has to be an actual JSON LIST. `null` does NOT fail the
	// Unmarshal (docs/ARMADILHAS.md, "Go / JSON") and would travel to Meta as
	// a template with no body at all; `{}` the same, in a different shape.
	components := bytes.TrimSpace(p.Components)
	if len(components) == 0 || !bytes.HasPrefix(components, []byte("[")) {
		return errors.New("campo componentes e obrigatorio e tem de ser uma lista JSON")
	}
	p.Components = components
	return nil
}

// autenticar responds and returns false when the caller does not pass.
func (h *TemplatesHandler) authenticate(w http.ResponseWriter, r *http.Request) (config.Consumer, bool) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err == nil {
		return consumer, true
	}
	if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
		respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
		return config.Consumer{}, false
	}
	log.Printf("zapgw: erro de store ao autenticar em /v1/templates: %v", err)
	respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
	return config.Consumer{}, false
}

// instanceActive is the SAME three guards as sending, in the same order:
// binding, existence, pause. They live in one single function because both
// routes in this file need all three — two copies would diverge on the first
// change, which is this project's mother trap.
//
// `rota` is only for the refusal log (T-037) to know whether it was GET or
// POST /v1/templates that refused — both call this function.
func (h *TemplatesHandler) instanceActive(
	w http.ResponseWriter, consumer config.Consumer, slug, route string,
) (config.Instance, bool) {
	// BEFORE any call to Meta: without this, a token leaked from system A
	// would read B's catalog — which describes its business — and would
	// still spend the quota of an instance that isn't its own.
	if !CanUse(consumer, slug) {
		log.Printf("zapgw: consumidor %q pediu templates da instancia %q, que nao e dele",
			consumer.Name, slug)
		respondError(w, http.StatusForbidden, "config",
			"instancia nao autorizada para este consumidor", 0)
		return config.Instance{}, false
	}

	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			// 404 for instance YES (T-037): whoever got here has already
			// authenticated.
			logRejection(h.throttleLog, route, slug, consumer.Name, "instancia desconhecida")
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return config.Instance{}, false
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q em /v1/templates: %v", slug, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return config.Instance{}, false
	}
	if !inst.Active {
		// 503 like the rest of the gateway, and without spending a call to
		// Meta on a channel that cannot send anyway.
		respondError(w, http.StatusServiceUnavailable, "retentavel", "instancia pausada", 0)
		return config.Instance{}, false
	}
	// T-111: AFTER the binding check (403) and the existence check (404) —
	// NEVER before, otherwise this route turns into an oracle for "what type
	// is this slug" for someone who does not own it. checkType already
	// writes the 400/config response when it rejects.
	if !checkType(w, h.types, inst, "") {
		return config.Instance{}, false
	}
	return inst, true
}

// respondCatalogError translates a READ failure.
//
// No branch here returns a list: the error response carries no templates,
// and that is the whole point of this endpoint.
func (h *TemplatesHandler) respondCatalogError(w http.ResponseWriter, slug string, err error) {
	switch {
	case errors.Is(err, meta.ErrIncompleteCatalog):
		// NEEDS A HUMAN: while this lasts, this consumer CANNOT read this
		// instance's catalog. Either the WABA grew past the page ceiling
		// (raise `pageCap` in internal/meta/templates.go and redeploy)
		// or Meta's pagination is looping. What is NOT done is serving the
		// partial list: that is how the old gateway got taken out of
		// production.
		log.Printf("ALARME zapgw: catalogo de templates da instancia %q nao coube no teto de paginas — "+
			"nenhuma lista foi servida (parcial seria pior); confira a paginacao da Meta ou suba o teto: %v",
			slug, err)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"o catalogo desta instancia nao coube no limite de paginacao do gateway; "+
				"nenhuma lista parcial e devolvida de proposito — avise quem opera o gateway", 0)
	case errors.Is(err, meta.ErrInvalidWabaID):
		// The request never even left here, and no read of this instance
		// works until an admin fixes the registration.
		log.Printf("ALARME zapgw: waba_id invalido para a instancia %q — corrija o waba_id no store; "+
			"nenhuma consulta de template desta instancia funciona ate la", slug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; "+
				"o pedido nao chegou a Meta e nao adianta repetir ate isso ser corrigido", 0)
	case errors.Is(err, meta.ErrPageFromAnotherOrigin):
		log.Printf("ALARME zapgw: paginacao de templates da instancia %q apontou para fora da Graph API "+
			"configurada; a leitura foi abortada e o token NAO foi enviado ao destino estranho", slug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a paginacao da Meta apontou para um destino inesperado e a leitura foi abortada; "+
				"nenhuma lista parcial e devolvida — avise quem opera o gateway", 0)
	case errors.Is(err, meta.ErrCatalogNotUnderstood):
		respondError(w, http.StatusServiceUnavailable, string(meta.ClassRetryable),
			"a Meta respondeu um catalogo que o gateway nao entendeu; nenhuma lista parcial e devolvida", 0)
	default:
		var me *meta.MetaError
		if errors.As(err, &me) {
			if me.Class == meta.ClassConfig {
				log.Printf("ALARME zapgw: credencial da instancia %q recusada pela Meta ao ler templates", slug)
			}
			// T-153: respondMetaError (handler.go), not respondError —
			// this route has its own error body and it was THROUGH IT that
			// the consumer who triggered the task got a 503 without the new
			// fields. See the comment on respondMetaError.
			respondMetaError(w, statusForClass(me.Class), me)
			return
		}
		// Transport, deadline exceeded, reading the response. Unlike
		// creation, READING creates nothing on the other side: retrying is
		// safe, and that is why here it really is `retentavel`, not
		// `desconhecido`.
		respondError(w, http.StatusServiceUnavailable, string(meta.ClassRetryable),
			"nao foi possivel falar com a Meta para ler o catalogo; tente de novo", 0)
	}
}

// respondCreationError translates a WRITE failure.
//
// The difference from reading is in the no-response outcome: creating MAY
// have happened on Meta's side. Calling this `retentavel` would send the
// consumer to retry blindly — and until T-078 this branch told IT to check
// the catalog, something it can no longer do: by the "NINGUÉM fala direto
// com a Meta" rule (CLAUDE.md, 2026-07-28) it no longer has the second door.
// Who checks now is us, in respondAmbiguousOutcome.
func (h *TemplatesHandler) respondCreationError(
	w http.ResponseWriter, r *http.Request, inst config.Instance, name, language, requestedCategory string, err error,
) {
	switch {
	case errors.Is(err, meta.ErrInvalidWabaID):
		log.Printf("ALARME zapgw: waba_id invalido para a instancia %q — corrija o waba_id no store; "+
			"nenhuma criacao de template desta instancia funciona ate la", inst.Slug)
		respondError(w, http.StatusBadGateway, string(meta.ClassConfig),
			"a configuracao desta instancia no gateway esta invalida; "+
				"o pedido nao chegou a Meta e nao adianta repetir ate isso ser corrigido", 0)
	case errors.Is(err, meta.ErrTemplateWithoutID):
		// A `2xx` with no id is AMBIGUOUS for the same reason as transport,
		// and that's why it falls into the SAME handling: Meta may have
		// created it and just not said the id. Until T-078 this branch
		// answered directly, and T-078 did not leave it out on purpose —
		// the asymmetry "the rule applies in one branch and not in its
		// neighbor" is this project's mother trap (docs/ARMADILHAS.md).
		h.respondAmbiguousOutcome(w, r, inst, name, language, requestedCategory, err)
	default:
		var me *meta.MetaError
		if errors.As(err, &me) {
			// Meta RESPONDED. There is no ambiguity to resolve here, and
			// re-reading the catalog here would only spend the instance's
			// quota.
			if me.Class == meta.ClassConfig {
				log.Printf("ALARME zapgw: credencial da instancia %q recusada pela Meta ao criar template", inst.Slug)
			}
			// T-153: this is EXACTLY the branch the consumer who triggered
			// the task hit — a deterministic 503 (meta 2) on the CREATION of
			// a template. respondMetaError (handler.go) forwards
			// Detail, Subcode, Explanation, and Trace; respondError
			// (used up to here) only had class/code/message.
			respondMetaError(w, statusForClass(me.Class), me)
			return
		}
		h.respondAmbiguousOutcome(w, r, inst, name, language, requestedCategory, err)
	}
}

// respondAmbiguousOutcome handles the ONE class of outcome where the
// gateway does not know what happened on the other side: the creation went
// out from here and no verdict at all came back (transport down, deadline
// exceeded, `2xx` with no id).
//
// WHY IT EXISTS (T-078, 2026-07-28): that day a consumer created
// `pedido_avaliacao_v2` and got a `502 desconhecido` with the message "check
// the catalog." The template HAD been created, and it only found out because
// it still had direct access to the Graph API. That access has just been
// forbidden. Two defects came together and both die here:
//
//  1. the branch LOGGED NOTHING — measured on the production CT,
//     `journalctl -u zapgw | grep -ci template` came back ZERO for the whole
//     day, with the `502` having come out of it. It was the only class that
//     exists to say "I don't know what happened" and the only one that did
//     not store what happened, so that "was it a timeout or transport?" was a
//     STRUCTURALLY unanswerable question;
//  2. the response told the CONSUMER to check. Now the one who checks is the
//     gateway.
//
// THE ACCEPTED COST, written here so nobody discovers it with a stopwatch: a
// request that falls into this path can take up to TWO instance deadlines —
// the one from the creation that died and the one from the re-read. That is
// the price of the consumer getting a fact instead of a question.
func (h *TemplatesHandler) respondAmbiguousOutcome(
	w http.ResponseWriter, r *http.Request, inst config.Instance, name, language, requestedCategory string, cause error,
) {
	// (1) THE REAL ERROR, ALWAYS, BEFORE anything else that might also fail.
	//
	// The template's NAME and LANGUAGE go in, and the REQUEST BODY does not:
	// the body carries the `componentes`, which is text that goes to the
	// tenant's end customer. Name and language are technical identifiers,
	// and are exactly what the next person needs to search the catalog.
	log.Printf("zapgw: criacao de template terminou SEM VEREDITO da Meta na instancia %q "+
		"(template %q, idioma %q): %v — relendo o catalogo para descobrir se ela aconteceu",
		inst.Slug, name, language, cause)

	hit, attempts, want, err := h.spacedRereads(r.Context(), inst, name, language)
	switch {
	case err != nil:
		// BOTH FAILURES LOGGED. One single line, with the creation's cause
		// above and the reading's cause here, and the next occurrence stops
		// being undiagnosable.
		log.Printf("zapgw: a releitura do catalogo da instancia %q TAMBEM falhou apos a criacao ambigua "+
			"do template %q (tentativa %d de releitura, %s de espera acumulada): %v — o desfecho continua desconhecido",
			inst.Slug, name, attempts, want, err)
		respondErrorWithWait(w, http.StatusBadGateway, string(meta.ClassUnknown),
			MessageUnknownOutcome, attempts, want)

	case hit != nil:
		// NOT AN ERROR: the template exists. What failed was our reading of
		// the response, and the consumer gets the success the first call
		// would have returned.
		log.Printf("zapgw: a releitura do catalogo da instancia %q ACHOU o template %q (idioma %q, "+
			"status %q) na tentativa %d de releitura (apos %s de espera) — a criacao TINHA acontecido; "+
			"respondendo 201", inst.Slug, name, language, hit.Status, attempts, want)
		resp := templateCreatedResponse{
			ID:                hit.ID,
			Status:            hit.Status,
			Category:          hit.Category,
			RequestedCategory: requestedCategory,
			Warning:           WarningTemplatePending + " " + WarningCreationConfirmedByReread,
			Rereads:           attempts,
			WaitSeconds:       int(want.Seconds()),
		}
		// T-108: this is WHERE THE CONSUMER HAS THE LEAST CHANCE TO NOTICE
		// the swap — it is already reading a warning about the re-read, and
		// a second swap inside it goes unnoticed if it is not spelled out.
		if normalizeCategory(requestedCategory) != normalizeCategory(hit.Category) {
			resp.Warning += " " + fmt.Sprintf(WarningCategoryChangedFormat, requestedCategory, hit.Category)
		}
		respondTemplateCreated(w, resp)

	default:
		// DID NOT FIND IT IN ANY ATTEMPT. This does NOT authorize saying "it
		// was not created" — see MessageInconclusiveOutcome, which carries
		// the source and the why, and which COMES OUT IDENTICAL regardless
		// of how many attempts happened: the warning does not loosen, it
		// only gets rarer (T-101).
		log.Printf("zapgw: a releitura do catalogo da instancia %q NAO achou o template %q (idioma %q) em "+
			"%d tentativas espacadas (%s de espera) — desfecho INCONCLUSIVO, e nao 'nao foi criado': a Meta "+
			"nao documenta que o catalogo mostra na hora o que um POST acabou de criar",
			inst.Slug, name, language, attempts, want)
		respondErrorWithWait(w, http.StatusBadGateway, string(meta.ClassUnknown),
			MessageInconclusiveOutcome, attempts, want)
	}
}

// respondErrorWithWait is respondError (handler.go) plus how many
// re-reads happened and how long was spent waiting between them — ONLY the
// two ambiguous outcomes of respondAmbiguousOutcome use this (T-101, item
// 5).
//
// The `mensagem` stays the SAME text respondError would have received: the
// new fields are ADDITIVE, they never rewrite MessageInconclusiveOutcome
// or MessageUnknownOutcome — the warning does not loosen (see the
// comment on those constants).
func respondErrorWithWait(w http.ResponseWriter, status int, class, message string, attempts int, want time.Duration) {
	var r errorResponseWithReread
	r.Error.Class = class
	r.Error.Message = message
	r.Error.Rereads = attempts
	r.Error.WaitSeconds = int(want.Seconds())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(r)
}

// errorResponseWithReread has the SAME shape as errorResponse (handler.go),
// with two extra fields. It does not reuse its struct because errorResponse
// is shared by routes that NEVER re-read any catalog (sending, health,
// reading) — stamping `releituras`/`espera_segundos` always zero on them
// would be noise, not information.
type errorResponseWithReread struct {
	Error struct {
		Class       string `json:"classe"`
		MetaCode    int    `json:"codigo_meta,omitempty"`
		Message     string `json:"mensagem"`
		Rereads     int    `json:"releituras"`
		WaitSeconds int    `json:"espera_segundos"`
	} `json:"erro"`
}

// spacedRereads tries catalogReread MORE THAN ONCE, spaced out by
// RereadWaits, before returning to the caller the "didn't find it"
// that authorizes the inconclusive outcome (T-101).
//
// The FIRST attempt is IMMEDIATE, with no pause at all — the MOST LIKELY
// case (created and propagated in seconds) cannot be made slower because of
// the rare and expensive case. A pause only kicks in AFTER an attempt that
// did NOT find it and BEFORE the next one.
//
// Also returns how many attempts happened and how long was spent sleeping
// between them — the consumer needs to know there was a wait to calibrate
// its own timeout (item 5 of T-101).
//
// Stops the moment: (a) it finds the template, (b) an attempt returns an
// error — the re-read also failed, no point insisting against a transport
// that is not responding, the outcome is unknown —, or (c) RereadWaits
// runs out without finding it — the outcome is inconclusive.
func (h *TemplatesHandler) spacedRereads(
	ctx context.Context, inst config.Instance, name, language string,
) (hit *meta.Template, attempts int, want time.Duration, err error) {
	_, attempts, want, err = h.spacedRereadsOf(ctx, func(ctx context.Context) (bool, error) {
		var errInner error
		hit, errInner = h.catalogReread(ctx, inst, name, language)
		// FINDING IT is what settles the creation's question.
		return hit != nil, errInner
	})
	return hit, attempts, want, err
}

// spacedRereadsForDeletion is the deletion's half (T-173): it re-reads
// until the name STOPS being in the catalog under a live status.
//
// Returns what the LAST attempt still found under that name — which is what
// tells `apagado` (nothing, or only PENDING_DELETION) from `inconclusivo`
// (still there, alive). See deletionAccepted.
func (h *TemplatesHandler) spacedRereadsForDeletion(
	ctx context.Context, inst config.Instance, name string,
) (rest []templateEntry, attempts int, want time.Duration, err error) {
	_, attempts, want, err = h.spacedRereadsOf(ctx, func(ctx context.Context) (bool, error) {
		catalog, errInner := h.catalogForReread(ctx, inst)
		if errInner != nil {
			return false, errInner
		}
		rest = entriesWithName(catalog, name)
		return deletionAccepted(rest), nil
	})
	return rest, attempts, want, err
}

// spacedRereadsOf is the LOOP the two re-reads above share: try, and
// while the answer is not settled, pause by RereadWaits and try again.
//
// WHY IT IS ONE FUNCTION AND NOT TWO COPIES: the pause policy is a number the
// CONTRACT promises (RereadWaitCap, the consumer sizes its timeout
// by it). Two loops would let the creation's ceiling and the deletion's
// diverge on the first change, and the consumer would be given one number
// while a route obeyed another — this project's mother trap.
//
// The FIRST attempt is IMMEDIATE, with no pause at all: the MOST LIKELY case
// cannot be made slower because of the rare and expensive one. A pause only
// happens AFTER an attempt that did not settle and BEFORE the next one.
//
// It stops the moment: (a) `tentar` says the question is settled, (b) an
// attempt returns an error — no point insisting against a transport that is
// not answering —, or (c) RereadWaits runs out.
func (h *TemplatesHandler) spacedRereadsOf(
	ctx context.Context, attempt func(context.Context) (bool, error),
) (resolved bool, attempts int, want time.Duration, err error) {
	for i := 0; ; i++ {
		attempts++
		resolved, err = attempt(ctx)
		if err != nil || resolved {
			return resolved, attempts, want, err
		}
		if i >= len(RereadWaits) {
			return false, attempts, want, nil
		}
		pause := RereadWaits[i]
		waitReread(ctx, pause)
		want += pause
	}
}

// catalogReread looks for the template by the pair (name, language),
// which is its identity within a WABA.
//
// 🔴 IT IS A `GET`, AND ONLY A `GET`. No line here can turn into "try
// creating it again," not today and not on the day someone thinks "it would
// be more useful." Creation is NOT idempotent and name+language are unique
// per WABA: a second creation of a template that exists comes back with
// `code 100` / `subcode 2388024`
// (developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management)
// and the name is burned — the consumer loses the name it chose, forever,
// because of a "kindness" from the gateway. Who decides to retry is it, with
// the fact in hand. TestRereadDoesNOTCreateAgain guards this by counting the
// POSTs.
//
// Returns (nil, nil) when the read worked and the template was not there —
// which is "didn't find it," never "doesn't exist."
func (h *TemplatesHandler) catalogReread(
	ctx context.Context, inst config.Instance, name, language string,
) (*meta.Template, error) {
	catalog, err := h.catalogForReread(ctx, inst)
	if err != nil {
		return nil, err
	}
	for i := range catalog {
		if strings.EqualFold(catalog[i].Name, name) && strings.EqualFold(catalog[i].Language, language) {
			return &catalog[i], nil
		}
	}
	return nil, nil
}

// catalogForReread is the WHOLE catalog read the two re-reads (creation
// and deletion) do — the part that is identical between them, in one place.
//
// NEW DEADLINE, and derived from the CONSUMER's request, not from the context
// of the call that failed. That context may be dead from an expired deadline
// — which is one of the causes that bring someone here — and reusing it would
// make the re-read fail without even leaving the machine, turning every
// timeout into "unknown" again. This is the requirement written in CLAUDE.md:
// reading has to work INDEPENDENTLY of whether the write failed, because it's
// a different path.
//
// NO status filter, on purpose, and it matters for BOTH callers: the
// just-created template is born PENDING, and the just-deleted one can be
// PENDING_DELETION. Any filter would hide exactly the row that decides the
// outcome — "didn't find it" would become a lie instead of a doubt.
func (h *TemplatesHandler) catalogForReread(
	ctx context.Context, inst config.Instance,
) ([]meta.Template, error) {
	ctx, cancel := context.WithTimeout(ctx, InstanceDeadline(inst))
	defer cancel()
	return h.client.ListTemplates(ctx, inst.WabaID, inst.SendToken, "")
}
