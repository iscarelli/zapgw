// The body of POST /v1/messages: a discriminated union by `tipo`.
//
// WHY A DISCRIMINATED UNION, and not one route per type: in the Cloud API
// `interactive.type` is ONE single value, and reply button and link button have
// INCOMPATIBLE `action` shapes. With the type discriminating, "buttons mixed
// with cta_url" stops being a 400 from Meta discovered in production and
// becomes a schema error, here, before touching the network.
//
// Validation exists to make INEXPRESSIBLE what Meta would reject — or,
// worse, what it ACCEPTS and does not deliver (see ErrFieldForbidden).
package outbound

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iscarelli/zapgw/internal/meta"
)

var (
	ErrUnknownType    = errors.New("outbound: tipo de mensagem desconhecido")
	ErrFieldRequired  = errors.New("outbound: campo obrigatorio ausente")
	ErrFieldForbidden = errors.New("outbound: campo proibido para este tipo")
	ErrMixedButtons   = errors.New("outbound: botao de resposta e botao de link nao convivem")
	ErrBase64         = errors.New("outbound: base64 nao e aceito")

	// ErrUnknownCategory: `tipo: "midia"` without a category that exists
	// in the meta/media.go table.
	ErrUnknownCategory = errors.New("outbound: categoria de midia desconhecida")

	// ErrMediaBase64 is a sibling of ErrBase64 and is NOT the same: here the
	// fix is different. In text, base64 is content in the wrong place; in
	// media fields, whoever sends bytes where a `media_id` should go needs to
	// know a route exists that returns that id. Both use the SAME detector
	// (containsBase64) — two detections would diverge on the first change —,
	// and separating the sentinel lets the consumer tell the two cases apart.
	ErrMediaBase64 = errors.New("outbound: base64 nao e aceito em campo de midia")

	// ErrRawComponents: the consumer sent `components` in the Graph API
	// format for the gateway to pass through. See the comment on the
	// Request.Components field for why this is REFUSED and not ignored.
	ErrRawComponents = errors.New("outbound: components cru da Graph API nao e aceito")

	// ErrRemovedURLButtons: the consumer sent `botoes_url`, a field REMOVED
	// on 2026-07-28 (T-045). See the comment on the Request.RemovedURLButtons
	// field for why this is REFUSED and not ignored — the short answer is
	// that ignoring it would send the template WITHOUT THE BUTTON with a 200
	// in the response, and the consumer would find out on the customer's
	// phone.
	ErrRemovedURLButtons = errors.New("outbound: botoes_url foi removido; use botoes_template")

	// ErrUnknownHeaderType: `cabecalho.tipo` outside the list this
	// gateway knows how to assemble.
	ErrUnknownHeaderType = errors.New("outbound: tipo de cabecalho de template desconhecido")

	// ErrInvalidMediaID: the value in place of a media_id does not have the
	// shape of a media_id — typically a raw URL. See validateHeader.
	ErrInvalidMediaID = errors.New("outbound: media_id com forma invalida")

	// ErrButtonIndex: negative or repeated template button index.
	//
	// UNTIL T-045 this guard had to CROSS two fields (`botoes_url` and
	// `botoes_template`), because both wrote into the SAME index space of
	// the SAME template. With `botoes_url` removed, the index space went
	// back to being STRUCTURAL — one list, one space — and the guard no
	// longer depends on anyone remembering to check the two together.
	ErrButtonIndex = errors.New("outbound: indice de botao de template invalido")

	// ErrUnknownTemplateButtonType: `botoes_template[i].tipo` outside
	// "url" | "resposta_rapida" (T-044).
	ErrUnknownTemplateButtonType = errors.New("outbound: tipo de botao de template desconhecido")

	// ErrFieldTooLong: `cabecalho_texto` or `rodape` above the character limit
	// the Cloud API accepts in those blocks (T-137), or `botao_titulo` of
	// `cta_url` above the measured ceiling (T-139), or `botoes` (list of
	// quick-reply buttons) above the ITEM ceiling the Cloud API accepts
	// (T-143) — the same sentinel even though it's a LIST size and not a
	// string one, because the nature of the defect is the same: field beyond
	// the limit Meta accepts.
	//
	// THE SENTINEL TEXT DOES NOT NAME THE UNIT (T-144): with three uses in
	// runes and one in items, "above the character limit" lied to whoever
	// hit the item usage — the message told them to measure text length
	// when the real defect was too many buttons. Whoever calls it is who
	// knows the unit, and that's why every `fmt.Errorf` that uses this
	// sentinel has to complete the sentence with it ("... (N runes, maximum
	// M)" or "... (N items, maximum M)") — never leave just "above the
	// limit" without saying what was exceeded.
	ErrFieldTooLong = errors.New("outbound: campo acima do limite")

	// ErrInvalidContactDate: `contatos[i].birthday` filled in but outside
	// the `YYYY-MM-DD` shape — the ONLY `contatos` field with a format
	// declared in Meta's doc (T-146). See validateContacts.
	ErrInvalidContactDate = errors.New("outbound: data de nascimento do contato com formato invalido")

	// ErrInvalidFlowIDName: `fluxo.id` and `fluxo.nome` of `tipo:"flow"`
	// (T-154) are MUTUALLY EXCLUSIVE and at least one is required — the
	// third-party source (see the comment on FlowRequest) says "one or the
	// other, never both". Sending both together or neither of the two falls
	// here. See validateFlow.
	ErrInvalidFlowIDName = errors.New("outbound: fluxo.id e fluxo.nome sao mutuamente exclusivos, um dos dois e obrigatorio")

	// ErrUnknownFlowAction: `fluxo.acao` of `tipo:"flow"` (T-154) outside
	// "navigate" | "data_exchange" — the ONLY TWO values the third-party
	// source describes. See validateFlow.
	ErrUnknownFlowAction = errors.New("outbound: fluxo.acao desconhecida")
)

// safeRejectionMessage returns the text of a Validate() error READY FOR
// LOG (T-037).
//
// THE GENERAL RULE, valid for almost every error in this file, is "name the
// field, never the value" — and that's why it's safe to log err.Error()
// directly in most cases. FOUR sentinels ARE THE EXCEPTION: ErrUnknownType,
// ErrUnknownCategory, ErrUnknownHeaderType and
// ErrUnknownTemplateButtonType (T-044) deliberately quote the value the
// consumer sent (`%q`) to help identify the error itself — and that value can
// be ANY string the consumer wrote into the `tipo` or `categoria` field, with
// no relation whatsoever to a real message type. Going into the HTTP RESPONSE
// this is safe (the same consumer who sent the value is reading it back);
// going into the GATEWAY LOG it would be a fresh copy of text they wrote,
// recorded in a place they don't control — the same family as Critical #2 of
// this project (`%v` of a `*url.Error` leaking the `callback_url`,
// docs/ARMADILHAS.md).
//
// FOUND while writing the T-037 log, reviewing every Validate() message
// against the code (not against the doc): nothing had reason to revise these
// three messages until a LOG consumer showed up.
func safeRejectionMessage(err error) string {
	switch {
	case errors.Is(err, ErrUnknownType):
		return "tipo de mensagem desconhecido"
	case errors.Is(err, ErrUnknownCategory):
		return "categoria de midia desconhecida"
	case errors.Is(err, ErrUnknownHeaderType):
		return "tipo de cabecalho de template desconhecido"
	case errors.Is(err, ErrUnknownTemplateButtonType):
		return "tipo de botao de template desconhecido"
	default:
		return err.Error()
	}
}

// Button is a quick-reply button.
type Button struct {
	ID    string `json:"id"`
	Title string `json:"titulo"`
}

// TemplateHeader is the parameter of a template's `header` block.
//
// `Type` discriminates, and that's why the sibling fields are mutually
// exclusive: a text header carries `Text` and nothing else; a media header
// carries `MediaID` (and `Filename`, only in document). Accepting both
// together would silently discard one of them during assembly — the most
// expensive failure shape in this project.
//
// THE VOCABULARY OF `Type` IS DELIBERATELY THE SAME AS `Request.Category`
// ("imagem", "video", "documento"), and the translation to Meta's name still
// lives in one single place (meta.GraphAPIType). A second vocabulary here
// would diverge from that one on the first change — this project's mother
// trap.
//
// THERE IS NO URL: the media header is by `media_id`, always. A raw URL
// makes Meta go FETCH the file, and that is exactly the path that fails
// silently when it doesn't fetch — the reason POST /v1/media exists.
type TemplateHeader struct {
	// Type: "texto" | "imagem" | "video" | "documento".
	Type string `json:"tipo"`
	// Text: only in `tipo: "texto"`.
	Text string `json:"texto,omitempty"`
	// MediaID: only in media types; comes from POST /v1/media.
	MediaID string `json:"media_id,omitempty"`
	// Filename: only in `tipo: "documento"`.
	Filename string `json:"nome_arquivo,omitempty"`
}

// TemplateButtonUnion is the dynamic parameter of ONE template button: a
// discriminated union by `tipo`, in the SAME pattern as TemplateHeader —
// because Meta also models a template button that way, a `button` block with
// `sub_type` discriminating.
//
// `Index` is positional and belongs to the TEMPLATE, not to this list: it
// says which button — in the order they were declared in Meta — the
// parameter belongs to. Sending the wrong index gives no error at all: the
// token goes to the wrong button, and the customer clicks and lands in the
// wrong place.
//
// ITS LIST (`botoes_template`) IS THE ONLY TEMPLATE BUTTON PARAMETER FIELD,
// and that's the result of T-045. Until then there also existed
// `botoes_url` (`BotaoDeTemplate`), which said the same thing that
// `tipo:"url"` says here — two ways to declare the SAME button, and
// therefore an EXPRESSIBLE invalid state: the same index in both fields. It
// was refused by a cross-guard, and a guard is not the same as
// inexpressible — see validateTemplateButtons.
//
// "url" -> sub_type:"url", parameter {"type":"text","text":…} — the URL
// button, which is what `botoes_url` produced (same block shape, same
// `sub_type`).
//
// "resposta_rapida" -> sub_type:"quick_reply", parameter
// {"type":"payload","payload":…} — the path that was missing (T-044):
// without it Meta returns the button's TEXT on click, not an id the
// consumer can recognize.
//
// `Text` only exists in "url"; `Payload` only exists in "resposta_rapida" —
// the two are mutually exclusive, same reason as TemplateHeader:
// accepting the wrong field in the wrong type would silently discard it
// during assembly.
//
// `Payload` IS OPAQUE and may carry the consumer's internal id (e.g.:
// "confirma:41"). It NEVER goes into a log (same rule as T-037: the error
// message names the field, never the value) — no validation message in this
// file echoes Payload.
//
// WHY IT'S NOT CALLED `botoes` IN THE JSON, even though the Cloud API and
// this task's original request call it that: `Request.Buttons []Button`
// already uses the json tag "botoes" for ANOTHER thing — the quick-reply
// buttons of `tipo:"botoes"` (a plain interactive message, WITHOUT a
// template; see docs/CONTRATO-CONSUMIDOR.md). Confirmed by experiment
// before writing this field: two Go structs with the SAME json tag at the
// SAME level of a struct are silently ignored, both by json.Marshal and by
// json.Unmarshal — no error, both fields stay empty. Reusing "botoes" here
// would erase BOTH features at once, with no signal whatsoever: this
// project's own mother trap ("the rule applies here, doesn't apply there"),
// this time inside `encoding/json`. That's why the field is called
// `botoes_template` — see docs/ARMADILHAS.md.
type TemplateButtonUnion struct {
	Index int `json:"indice"`
	// Type: "url" | "resposta_rapida".
	Type string `json:"tipo"`
	// Text: only in `tipo: "url"`.
	Text string `json:"texto,omitempty"`
	// Payload: only in `tipo: "resposta_rapida"`. Opaque — see the type's comment.
	Payload string `json:"payload,omitempty"`
}

// ListItem is a row (`row`) inside a ListSection, the body of
// `tipo: "lista"` (T-145).
//
// `Description` is OPTIONAL and ABSENT when empty — NEVER `""` — same
// absent-key-when-empty rule as the rest of this file (see `header`/`footer`
// in MetaBody): Meta only accepts the `description` key when there's
// content, and sending an empty string is a different way of sending
// absence that no one has a reason to choose.
type ListItem struct {
	ID          string `json:"id"`
	Title       string `json:"titulo"`
	Description string `json:"descricao,omitempty"`
}

// ListSection is a group of ListItem inside `tipo: "lista"` (T-145).
//
// The Cloud API requires AT LEAST ONE section with AT LEAST ONE item — see
// validateList — and the 10-ITEM CEILING IS SUMMED ACROSS ALL SECTIONS, not
// per section: a message with 3 sections of 4 items each (12 total) is
// REFUSED even though no single section goes over 10. It's the easiest
// point of this task to implement wrong, because when wrong it only shows up
// as a 400 from Meta in production — see TestValidateRefusesListWithSummedItemsAboveTheCap.
type ListSection struct {
	Title string     `json:"titulo"`
	Items []ListItem `json:"itens"`
}

// ReactionRequest is the parameter of `tipo: "reacao"`: applying OR REMOVING an
// emoji from a message received earlier.
//
// THE VOCABULARY IS THE SAME as the inbound side (internal/meta/types.go,
// type Reaction, fixed in T-023): `alvo`/`emoji` mean the same thing in both
// directions — using different names for the same thing is the beginning of
// two vocabularies.
//
// EMPTY `emoji` REMOVES THE REACTION (T-027). The source is NOT a page —
// the official doc (developers.facebook.com/docs/whatsapp/cloud-api/messages/
// reaction-messages, read on 2026-07-26) lists `<EMOJI>` as "Required" and
// describes no removal at all. The source is consumer-a, who ran the
// EXPERIMENT WITH A DEVICE on 2026-07-26 (10:15 -03), with the owner watching
// the screen: two sends via the direct Graph API, same body, only the emoji
// changing —
//
//	(1) emoji: "\U0001F44D" -> the owner confirmed: the reaction APPEARED on the device.
//	(2) emoji: ""           -> Meta responded 200 with a NEW wa_message_id,
//	                           and the owner confirmed: the reaction DISAPPEARED from the device.
//
// THE DETAIL THAT HAS TO SURVIVE ANY REFACTOR OF THIS FIELD: the `200` with
// a new wamid came out THE SAME in both sends. If the reaction had NOT
// disappeared in the second one, the response would have been exactly the
// same — Meta proves it ACCEPTED the request, never that the EFFECT
// happened. The only possible witness was the customer's device (see
// docs/ARMADILHAS.md, "Sucesso da API não é sucesso do efeito").
//
// ABSENT `emoji` (key not sent in the JSON) STAYS a required-field ERROR —
// it does NOT become removal. This is the OPPOSITE of RECEIVING (where Meta
// itself OMITS the key to say "I removed it"), and the asymmetry is
// deliberate: on send, whoever assembles the request is a PROGRAM belonging
// to the consumer, and "forgot to send the field" is the common programming
// error — absence of a key is indistinguishable from an oversight. An empty
// string is a choice someone typed on purpose. That's why Emoji is
// `*string`, not `string`: it's the pointer (nil vs. points-to-"") that
// tells the two apart — the same technique as Latitude/Longitude in
// LocationRequest, right below.
type ReactionRequest struct {
	Target string `json:"alvo"` // wamid of the reacted-to message

	// Emoji: nil = key absent (ErrFieldRequired); points to "" =
	// removes the reaction; points to a value = adds/replaces the reaction.
	Emoji *string `json:"emoji"`
}

// LocationRequest is the parameter of `tipo: "localizacao"`: sharing a
// point with the recipient. Vocabulary identical to the inbound side
// (internal/meta/types.go, type Location).
//
// Latitude/Longitude are deliberately POINTERS, and have NO omitempty: 0 is
// a valid coordinate (the intersection of the Greenwich meridian and the
// equator), and a plain float64 with omitempty would silently erase that
// coordinate from the body. The pointer is what tells "didn't send" (nil,
// validation error) apart from "sent zero" (accepted) — the same trap
// recorded in docs/ARMADILHAS.md for the encrypted column and for the twin
// inbound field.
type LocationRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Name      string   `json:"nome,omitempty"`
	Address   string   `json:"endereco,omitempty"`
}

// Contact is ONE card of `tipo: "contatos"` (T-146) — the Cloud API's
// contact card, the only one of the eleven `type` values that was missing
// from this gateway.
//
// 🔴 DELIBERATE DESIGN DECISION THAT GOES AGAINST THIS REPO'S CONVENTION:
// inside the card the fields use META'S OWN NAMES, in English
// (`name.formatted_name`, `phones[].phone`, `org.company`…), and not a
// Portuguese translation like the rest of this file does (`para`, `texto`,
// `botoes`…). REASON: there are ~25 nested fields of a standard structure
// (vCard). A translation table with 25 entries is a PERMANENT maintenance
// cost that diverges on the first new field Meta adds — and the owner's
// order, recorded in T-146, was fidelity to the official API here. What
// stays in Portuguese is ONLY the top-level key (`contatos` in Request, see
// below), like the rest of the request.
//
// THIS IS NOT THE SAME RAW PASSTHROUGH THAT T-043 REFUSED IN `components`
// (see Request.Components) — the difference is structural, not a matter of
// taste:
//
//   - `components` was a DISCRIMINATED UNION (header | body | button, each
//     block with its OWN shape and combinations Meta REJECTS — reply
//     button mixed with cta_url, for example). Passing that raw JSON
//     through would return to production a whole class of error that this
//     file's validation exists to make inexpressible BEFORE touching the
//     network.
//   - `Contact` is A SINGLE REQUIRED FIELD (`name.formatted_name`) and NO
//     UNION: there is no combination of sub-fields that Meta rejects for
//     being together, there is no "pick the type and only that type's
//     fields are valid". It's just a form with independent fields, most of
//     them optional. There is no invalid state to make inexpressible here —
//     just one field to require. A name translation would add no safety
//     that the English vocabulary doesn't already give; it would just
//     create a second list bound to diverge from Meta on the first new
//     field.
//
// See validateContacts() for the ONE required field (formatted_name) and the
// ONE format check (birthday), and the comment on Request.Contacts for why NO
// ceiling is invented here (T-146, item 4 — Meta's page declares no limit
// for these fields, and T-143 already recorded "mirroring a limit no one
// read" as a decision NOT to make).
type Contact struct {
	Name      ContactName      `json:"name"`
	Addresses []ContactAddress `json:"addresses,omitempty"`
	// Birthday: "YYYY-MM-DD" — the ONLY Contact field with a format
	// declared in Meta's doc. See validateContacts.
	Birthday string         `json:"birthday,omitempty"`
	Emails   []ContactEmail `json:"emails,omitempty"`
	Org      *ContactOrg    `json:"org,omitempty"`
	Phones   []ContactPhone `json:"phones,omitempty"`
	Urls     []ContactURL   `json:"urls,omitempty"`
}

// ContactName is Contact's `name` block. `FormattedName` is the ONLY
// required field of every `tipo: "contatos"` (source:
// developers.facebook.com/docs/whatsapp/cloud-api/messages/contacts-messages,
// read 2026-08-20 — "Contact's formatted name. This will appear in the
// message alongside the profile arrow button."). Everything else is optional.
type ContactName struct {
	FormattedName string `json:"formatted_name"`
	FirstName     string `json:"first_name,omitempty"`
	LastName      string `json:"last_name,omitempty"`
	MiddleName    string `json:"middle_name,omitempty"`
	Prefix        string `json:"prefix,omitempty"`
	Suffix        string `json:"suffix,omitempty"`
}

// ContactAddress is an item of `addresses[]`. All fields are optional in
// Meta; none has a declared shape or ceiling, so none is checked here (see
// the comment on Contact).
type ContactAddress struct {
	Street      string `json:"street,omitempty"`
	City        string `json:"city,omitempty"`
	State       string `json:"state,omitempty"`
	Zip         string `json:"zip,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	Type        string `json:"type,omitempty"`
}

// ContactEmail is an item of `emails[]`.
type ContactEmail struct {
	Email string `json:"email,omitempty"`
	Type  string `json:"type,omitempty"`
}

// ContactOrg is Contact's `org` block.
type ContactOrg struct {
	Company    string `json:"company,omitempty"`
	Department string `json:"department,omitempty"`
	Title      string `json:"title,omitempty"`
}

// ContactPhone is an item of `phones[]`.
type ContactPhone struct {
	Phone string `json:"phone,omitempty"`
	Type  string `json:"type,omitempty"`
	WaID  string `json:"wa_id,omitempty"`
}

// ContactURL is an item of `urls[]`.
type ContactURL struct {
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

// FlowRequest is the parameter of `tipo: "flow"` (T-154): opens a WhatsApp
// Flow (a native form inside WhatsApp) for the recipient to fill out.
//
// 🟢 SHAPE CONFIRMED AGAINST META'S PARSER ON 2026-08-20 (T-156) — but read
// carefully what this does and does not cover, because this project has
// already conflated the two things three times TODAY ALONE. We sent a
// `tipo:"flow"` with a DELIBERATELY MADE-UP `flow_id`, on both branches of
// `acao` (`navigate` with `tela`, and `data_exchange` without). On both, Meta
// responded:
//
//	400  codigo_meta 131009
//	detalhe_meta: Parameter "flow_id" is invalid. Please check if the flow
//	              associated to this id belongs to your WhatsApp Business
//	              Account, and it's in a valid state.
//
// In other words: it PARSED the entire payload and only stopped at the one
// field that was deliberately false. `flow_message_version`, `flow_token`,
// `flow_cta`, `flow_action` and `flow_action_payload` (with `screen` and
// `data`) MADE IT THROUGH its parser — if any of them had been wrong or had
// a swapped name, it would have complained about it before reaching
// `flow_id`.
//
// 🔴 WHAT THIS DOES NOT PROVE: RENDERING. There was never a published Flow
// on this WABA, so no screen ever got to open on the recipient's side — "Meta
// accepted the payload" and "the Flow rendered on the client" are DIFFERENT
// proofs, and only the first one happened. Don't write "proven" without
// saying which of the two.
//
// And the PARAMETERS still come from elsewhere: the structure of the fields
// below (names, requiredness, id/nome XOR) came from BSP DOCUMENTATION
// (360dialog) and a THIRD-PARTY SDK (whatsapp-api-js), read on 2026-08-20 —
// Meta has not confirmed that this source is official, only that the
// COMBINATION we assembled from it passes its parser. These are two things
// this comment must not merge: provenance of the PARAMETERS (third-hand, as
// always) and confirmation of the SHAPE (now real) — see validateFlow and
// the "flow" case of MetaBody (corpo.go).
//
// `ID` and `Name` are MUTUALLY EXCLUSIVE — the third-party source says "one
// or the other, never both" — and at least one is required (validateFlow).
// `Token` is REQUIRED: it's the identifier the CONSUMER generates to match
// the Flow's response (which arrives via webhook as `interactive.nfm_reply`
// — see the comment on the "flow" case in Validate()) with what they were
// doing when they opened the Flow. Without it the response comes back and
// no one knows whose it is.
type FlowRequest struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"nome,omitempty"`
	Token string `json:"token"`
	// Action: "navigate" | "data_exchange". Empty normalizes to "navigate"
	// in validateFlow (the third-party source's default).
	Action string `json:"acao,omitempty"`
	// Screen: only REQUIRED when Action == "navigate" — the source says the
	// payload is required in that case.
	Screen string `json:"tela,omitempty"`
	// Data: optional, passed through raw to `flow_action_payload.data`.
	Data map[string]any `json:"dados,omitempty"`
}

// Request is the body of POST /v1/messages.
//
// `To` IS CANONICALIZED IN Validate() FOR EVERY INSTANCE (see Validate(),
// below) — including Instagram, where it carries an IGSID, not a phone
// number. This is deliberate and NOT A BUG: Validate() runs BEFORE the
// handler knows the instance's TYPE (the header of handler.go explains why
// the order is that), and changing its behavior per type would break this
// task's (T-097) non-regression guarantee for WhatsApp. The handler
// (internal/outbound/handler.go) resolves this by capturing `rawTo` — the
// ORIGINAL value, only trimmed — BEFORE calling Validate(), and it's
// `rawTo` (never `p.To`) that goes to Meta and to the transit log when
// the instance is Instagram. `p.To` keeps feeding RequestHash (idempotency
// only needs a STABLE fingerprint of the request, not a semantically
// correct IGSID) and the non-empty validation below.
type Request struct {
	Instance string `json:"instancia"`
	To       string `json:"para"`
	Type     string `json:"tipo"`
	ReplyTo  string `json:"responder_a,omitempty"`

	// texto, botoes, cta_url, lista, pedir_localizacao
	Text string `json:"texto,omitempty"`

	// template
	Template  string   `json:"template,omitempty"`
	Language  string   `json:"idioma,omitempty"`
	Variables []string `json:"variaveis,omitempty"`
	// Header and TemplateButtons are the other `components` blocks the
	// Graph API accepts; `Variables` keeps being the `body` block, with its
	// usual meaning.
	Header *TemplateHeader `json:"cabecalho,omitempty"`

	// RemovedURLButtons exists ONLY TO BE REFUSED — same technique as
	// Components, right below, and for the same reason.
	//
	// `botoes_url` was the template URL-button field, succeeded by
	// TemplateButtons in T-044 and REMOVED in T-045 (2026-07-28), after
	// BOTH consumers confirmed in writing that they no longer use it.
	//
	// WHY A FIELD INSTEAD OF SIMPLY DELETING IT: the handler deserializes
	// with json.Unmarshal without DisallowUnknownFields. Without this
	// field, a `botoes_url` that still arrived would be SILENTLY discarded
	// — the consumer would get a 200 and the template would go out WITHOUT
	// THE BUTTON, with the discovery happening on the customer's phone. It
	// is json.RawMessage, and not the old list, on purpose: the gateway
	// doesn't need to know how to read the content to refuse it, and
	// keeping the old type around would keep alive the shape T-045 exists
	// to erase.
	//
	// THERE IS NO WAY for this field to produce any button at all: no path
	// reads it outside the refusal in Validate, and MetaBody does not
	// mention it.
	RemovedURLButtons json.RawMessage `json:"botoes_url,omitempty"`

	// TemplateButtons is the ONLY template button parameter field (T-045):
	// a discriminated union by `tipo`, see the comment on
	// TemplateButtonUnion for each type's shape and for why the field
	// isn't named `botoes`.
	//
	// ONE LIST, ONE INDEX SPACE. Until T-045 the space was shared with
	// `botoes_url`, and the repeated-index guard had to cross the two
	// fields by hand; today it's structural, with nothing to remember.
	TemplateButtons []TemplateButtonUnion `json:"botoes_template,omitempty"`

	// Components exists to be REFUSED, and is not a wasted field.
	//
	// Without it, a `"components": [...]` in the body would be silently
	// ignored by the handler's json.Unmarshal (it doesn't use
	// DisallowUnknownFields): the consumer would send the header, get a
	// 200, and the message would go out WITHOUT the header — apparent
	// success hiding partial delivery, the outcome this entire file exists
	// to prevent.
	//
	// And passthrough is not an alternative: the discriminated union exists
	// to make INEXPRESSIBLE what Meta rejects, and passing raw
	// `components` through would return that entire class of error to
	// production — the gateway would turn into a proxy that protects no
	// one. Whoever needs a header or a button uses `cabecalho` and
	// `botoes_template`, which this gateway assembles.
	Components json.RawMessage `json:"components,omitempty"`

	// botoes
	Buttons []Button `json:"botoes,omitempty"`

	// cta_url, lista — the action button's label. REUSED by BOTH since
	// T-145: same concept (the button's label), same ceiling — see the
	// comment on validateButtonTitle for why there is NO separate
	// `lista_botao` (a field decision by the CONSUMER, which T-149 does
	// not undo). ButtonURL is still exclusive to cta_url: lista has no URL,
	// its button only opens the list.
	ButtonTitle string `json:"botao_titulo,omitempty"`
	ButtonURL   string `json:"botao_url,omitempty"`

	// lista (T-145). `omitempty` is REQUIRED for the SAME reason as Reaction
	// and Location right below: without it, every request that doesn't
	// use lista would gain `"secoes":null` in the JSON RequestHash signs,
	// changing the idempotency hash of whoever does NOT use the field —
	// a legitimate retry of an already-reserved request would get a false
	// 422.
	Sections []ListSection `json:"secoes,omitempty"`

	// botoes, cta_url — FREE-TEXT header/footer of the Cloud API's
	// `interactive` object (T-137): accepted without approval, within the
	// 24 h window.
	//
	// `omitempty` on BOTH is REQUIRED, and it's not style: without it,
	// every request that doesn't use the fields would start carrying
	// `"cabecalho_texto":""` in the JSON RequestHash signs, the idempotency
	// hash of WHOEVER DOESN'T USE THE FIELD would change, and a legitimate
	// retry of an already-reserved request would get a false 422 — the
	// same trap already noted in Reaction and Location, and in
	// docs/ARMADILHAS.md.
	//
	// It does NOT reuse the name `cabecalho`: that field is the TEMPLATE
	// header (TemplateHeader), with a different shape (media_id,
	// nome_arquivo, and a single parameter fixed at registration). The
	// `_texto` suffix leaves room for a future `cabecalho_midia` without
	// renaming anything.
	//
	// In TEMPLATE this does NOT exist and will NOT exist: the template
	// header accepts 1 parameter FIXED AT REGISTRATION, and the template
	// footer is FIXED at registration and accepts NO parameter at all
	// (checked at developers.facebook.com/docs/whatsapp/business-management-api/
	// message-templates/components, read on 2026-08-20) — see the
	// forbidden-field guard in Validate().
	HeaderText string `json:"cabecalho_texto,omitempty"`
	Footer     string `json:"rodape,omitempty"`

	// midia
	MediaID  string `json:"media_id,omitempty"`
	Category string `json:"categoria,omitempty"`
	Caption  string `json:"legenda,omitempty"`
	Filename string `json:"nome_arquivo,omitempty"`

	// reacao. omitempty is REQUIRED: without it, every request that
	// doesn't use reaction would gain "reacao":null in the JSON RequestHash
	// hashes, and the idempotency hash of WHOEVER DOESN'T USE THE FIELD
	// would change — a legitimate retry of an already-reserved request
	// would get a false 422 (see the RequestHash trap in
	// docs/ARMADILHAS.md).
	Reaction *ReactionRequest `json:"reacao,omitempty"`

	// localizacao. Same reason for the omitempty above.
	Location *LocationRequest `json:"localizacao,omitempty"`

	// contatos (T-146): one or more contact cards. `omitempty` is
	// REQUIRED for the SAME reason as Reaction/Location/Sections right
	// above — without it, "contatos":null would enter the JSON of every
	// request that doesn't use the field, changing the idempotency hash
	// of whoever never sent `contatos`.
	//
	// See the comment on Contact for WHY the inside of the card uses
	// META's field names (English) instead of the translation the rest of
	// this file uses — and for why this is NOT the same passthrough T-043
	// refused in Components (field above, in this same struct).
	Contacts []Contact `json:"contatos,omitempty"`

	// fluxo (T-154): the body of `tipo:"flow"`. `omitempty` is REQUIRED
	// for the SAME reason as Reaction/Location/Sections/Contacts above —
	// without it, "fluxo":null would enter the JSON of every request that
	// doesn't use the field, changing the idempotency hash of whoever
	// never sent `fluxo`.
	//
	// See the comment on FlowRequest: the SHAPE is confirmed against Meta
	// (T-156); the THIRD-HAND provenance still applies only to the
	// PARAMETERS, and RENDERING remains unproven.
	Flow *FlowRequest `json:"fluxo,omitempty"`
}

// Validate refuses everything Meta would reject — and also what it ACCEPTS
// without delivering, which is worse because it produces no error at all.
func (p *Request) Validate() error {
	// Trim BEFORE any decision. Presence is not content: a field of just
	// spaces exists, is useless, and would reach Meta just like that — from
	// `responder_a` (which would become a `context` with a blank
	// message_id) to the neighboring fields of this same function (`para`,
	// `texto`, `template`, `idioma`, `botao_titulo`, `botao_url`), which had
	// the same gap.
	//
	// The normalization happens HERE and not in the body assembly because
	// assembly deliberately does not revalidate: if both knew the rule,
	// they would diverge on the first change.
	p.Instance = strings.TrimSpace(p.Instance)
	p.To = strings.TrimSpace(p.To)
	p.Text = strings.TrimSpace(p.Text)
	p.Template = strings.TrimSpace(p.Template)
	p.Language = strings.TrimSpace(p.Language)
	p.ButtonTitle = strings.TrimSpace(p.ButtonTitle)
	p.ButtonURL = strings.TrimSpace(p.ButtonURL)
	p.ReplyTo = strings.TrimSpace(p.ReplyTo)
	p.MediaID = strings.TrimSpace(p.MediaID)
	p.Category = strings.TrimSpace(p.Category)
	p.Caption = strings.TrimSpace(p.Caption)
	p.Filename = strings.TrimSpace(p.Filename)

	if p.Instance == "" {
		return fmt.Errorf("%w: instancia", ErrFieldRequired)
	}
	if p.To == "" {
		return fmt.Errorf("%w: para", ErrFieldRequired)
	}
	// `para` becomes the CANONICALIZED value here, not merely checked
	// against it: RequestHash (further below) hashes p.To after Validate,
	// and MetaBody sends the canonicalized value when sending. If we only
	// checked here and discarded the result, the two spellings of the SAME
	// phone number ("+55 11 99999-0000" and "5511999990000") would produce
	// the same message on the wire and DIFFERENT hashes — a legitimate retry
	// whose formatting changed would get a false 422. Canonicalizing here is
	// idempotent (see meta.Canonicalize), so MetaBody canonicalizing again
	// stays correct.
	p.To = meta.Canonicalize(p.To)
	if p.To == "" {
		return fmt.Errorf("%w: para (nao sobrou nenhum digito)", ErrFieldRequired)
	}

	// The refusal of raw `components` comes BEFORE the switch because it
	// doesn't depend on the type: passing through the Graph API format
	// isn't valid in any of them. And it's a REFUSAL and not silence on
	// purpose — see the comment on the field.
	if len(p.Components) > 0 {
		return fmt.Errorf("%w: monte o header em `cabecalho` e os parametros de botao em "+
			"`botoes_template`, que este gateway traduz", ErrRawComponents)
	}

	// `botoes_url` WAS REMOVED (T-045), and the refusal lives here — next
	// to the raw `components` one and for the SAME reason — because it
	// doesn't depend on the type: the field doesn't exist in any type, and
	// reporting it as "forbidden in text" would send someone to fix the
	// wrong thing.
	//
	// REFUSING AND NOT IGNORING is the decision T-045 had to make, and the
	// reason is the asymmetry of the two possible errors. Silently ignored,
	// the request becomes a 200 and the template goes out WITHOUT THE
	// BUTTON: the consumer has no way to know, and whoever finds out is
	// their customer, on their phone, clicking an incomplete template — and
	// it still burned a billed conversation. Refused, they get a 400
	// immediately, with the new field's name and the translation ready. The
	// first costs a wrong delivery and trust in the number on screen; the
	// second costs a deploy. *When the two possible errors have prices of
	// different orders of magnitude, the doubt resolves toward the cheap
	// side* (docs/ARMADILHAS.md).
	//
	// THE MESSAGE CARRIES THE FULL TRANSLATION on purpose: "use
	// botoes_template" alone sends the consumer to open the contract in the
	// middle of an incident. The translation is mechanical and fits in one
	// line, so it goes in the line.
	if len(p.RemovedURLButtons) > 0 {
		return fmt.Errorf(`%w: traduza cada item {"indice":N,"texto":X} para `+
			`{"indice":N,"tipo":"url","texto":X} em `+"`botoes_template`", ErrRemovedURLButtons)
	}

	switch p.Type {
	case "texto":
		if p.Text == "" {
			return fmt.Errorf("%w: texto", ErrFieldRequired)
		}
	case "template":
		if p.Template == "" {
			return fmt.Errorf("%w: template", ErrFieldRequired)
		}
		if p.Language == "" {
			return fmt.Errorf("%w: idioma", ErrFieldRequired)
		}
		// Meta ACCEPTS `context` in template and responds 200 — and the
		// quote bubble NEVER renders. Apparent success hiding partial
		// delivery, with no error in the response nor in the status
		// webhook. We refuse here because no later layer would be able to
		// detect it.
		if p.ReplyTo != "" {
			return fmt.Errorf("%w: responder_a em template (a Meta aceita e a citacao nunca aparece)", ErrFieldForbidden)
		}
		if err := p.validateHeader(); err != nil {
			return err
		}
		if err := p.validateTemplateButtons(); err != nil {
			return err
		}
	case "botoes":
		// The mix-up comes BEFORE the required fields: the real cause is
		// the mix-up, and reporting "missing botao_titulo" makes whoever
		// reads it fill in the title instead of removing the buttons — the
		// error would send them to fix the wrong thing.
		if p.ButtonURL != "" || p.ButtonTitle != "" {
			return ErrMixedButtons
		}
		if p.Text == "" {
			return fmt.Errorf("%w: texto", ErrFieldRequired)
		}
		if len(p.Buttons) == 0 {
			return fmt.Errorf("%w: botoes", ErrFieldRequired)
		}
		if n := len(p.Buttons); n > quickReplyButtonCountLimit {
			return fmt.Errorf("%w: botoes (%d itens, maximo %d)",
				ErrFieldTooLong, n, quickReplyButtonCountLimit)
		}
		for i := range p.Buttons {
			p.Buttons[i].ID = strings.TrimSpace(p.Buttons[i].ID)
			p.Buttons[i].Title = strings.TrimSpace(p.Buttons[i].Title)
			if p.Buttons[i].ID == "" {
				return fmt.Errorf("%w: botoes[%d].id", ErrFieldRequired, i)
			}
			if p.Buttons[i].Title == "" {
				return fmt.Errorf("%w: botoes[%d].titulo", ErrFieldRequired, i)
			}
			if n := utf8.RuneCountInString(p.Buttons[i].Title); n > quickReplyButtonTitleLimit {
				return fmt.Errorf("%w: botoes[%d].titulo (%d runas, maximo %d)",
					ErrFieldTooLong, i, n, quickReplyButtonTitleLimit)
			}
		}
		if err := p.validateHeaderTextAndFooter(); err != nil {
			return err
		}
	case "cta_url":
		if len(p.Buttons) > 0 {
			return ErrMixedButtons
		}
		if p.Text == "" {
			return fmt.Errorf("%w: texto", ErrFieldRequired)
		}
		if err := p.validateButtonTitle(); err != nil {
			return err
		}
		if p.ButtonURL == "" {
			return fmt.Errorf("%w: botao_url", ErrFieldRequired)
		}
		if err := p.validateHeaderTextAndFooter(); err != nil {
			return err
		}
	case "lista":
		if p.Text == "" {
			return fmt.Errorf("%w: texto", ErrFieldRequired)
		}
		if err := p.validateButtonTitle(); err != nil {
			return err
		}
		if err := p.validateList(); err != nil {
			return err
		}
		if err := p.validateHeaderTextAndFooter(); err != nil {
			return err
		}
	case "pedir_localizacao":
		// `location_request_message` (T-150). The whole shape is THREE
		// fields — `type`, `body.text`, `action.name` — and no other
		// (source: developers.facebook.com/docs/whatsapp/cloud-api/guides/
		// send-messages/location-request-messages/, read 2026-08-20).
		// `action.name` is CONSTANT ("send_location"), not a consumer
		// field: it doesn't exist down here, it only appears fixed in
		// MetaBody.
		if p.Text == "" {
			return fmt.Errorf("%w: texto", ErrFieldRequired)
		}
		// 🔴 NO CEILING HERE, ON PURPOSE — same decision as T-143. Meta's
		// doc declares 1024 characters for `body.text`; it was READ and
		// DELIBERATELY NOT mirrored: this gateway does not duplicate
		// Meta's limit table, and since T-141 the consumer receives the
		// field and the number in `detalhe_meta` when it refuses. A
		// ceiling on input is reserved for when the failure would be
		// SILENT or Meta's error wouldn't serve as a diagnostic — that's
		// not the case here. If someone ever tries to "fix" this by adding
		// a limiteTexto..., read T-143 first: the test
		// TestValidateAcceptsLocationRequestWithLongText (2000 runes)
		// exists to fail exactly that change.
		//
		// This type ALSO HAS NO cabecalho_texto/rodape/botao_titulo/
		// secoes — the doc describes only the THREE fields above. No new
		// guard line is needed: the cross-guards for cabecalho_texto/rodape
		// (further below, after the switch) and for botao_titulo/secoes
		// already refuse BY OMISSION — "pedir_localizacao" is not on their
		// allowed-type lists, so any of those fields automatically falls
		// into ErrFieldForbidden.
	case "midia":
		if p.MediaID == "" {
			return fmt.Errorf("%w: media_id (obtenha um em POST /v1/media)", ErrFieldRequired)
		}
		if p.Category == "" {
			return fmt.Errorf("%w: categoria", ErrFieldRequired)
		}
		// The category decides the body's SHAPE (meta.GraphAPIType) and
		// which fields it carries. A category the table doesn't know has
		// no shape at all, and sending it anyway would be a 400 from Meta
		// discovered in production.
		cat, known := meta.KnownCategory(p.Category)
		if !known {
			return fmt.Errorf("%w: %q", ErrUnknownCategory, p.Category)
		}
		// The refusal below exists because SILENCE would be worse:
		// MetaBody has nowhere to put a caption in audio/sticker nor a
		// filename outside document, so accepting the field would discard
		// it with no error at all. Whoever answers "carries it or not" is
		// the SAME table the assembler consults — two rules would diverge,
		// and the divergence would be invisible.
		if p.Caption != "" && !meta.AcceptsCaption(cat) {
			return fmt.Errorf("%w: legenda em %s (o corpo desta categoria nao a carrega, "+
				"e ela sumiria sem erro)", ErrFieldForbidden, cat)
		}
		if p.Filename != "" && !meta.AcceptsFilename(cat) {
			return fmt.Errorf("%w: nome_arquivo em %s (so documento tem nome de arquivo, "+
				"e ele sumiria sem erro)", ErrFieldForbidden, cat)
		}
	case "reacao":
		if err := p.validateReaction(); err != nil {
			return err
		}
	case "localizacao":
		if err := p.validateLocation(); err != nil {
			return err
		}
	case "contatos":
		if err := p.validateContacts(); err != nil {
			return err
		}
	case "flow":
		// tipo:"flow" (T-154) — opens a WhatsApp Flow. The SHAPE is
		// confirmed against Meta's parser (T-156); RENDERING is not —
		// see the comment on FlowRequest, above, BEFORE changing anything
		// here.
		if err := p.validateFlow(); err != nil {
			return err
		}
		// `botao_titulo` (the label of the button that opens the Flow,
		// `flow_cta` in the Cloud API) is REUSED from the same consumer
		// field that cta_url and lista use — same reason as T-145: don't
		// create a separate `flow_botao` just because the inner field has
		// a different name in Meta. validateFlow already called
		// validateButtonTitle(), which uses its OWN constant (flowCtaLimit)
		// for this type: flow_cta is a DIFFERENT Cloud API field than
		// `display_text` (cta_url) and `action.button` (lista), and T-149
		// set the rule of not sharing a constant between different fields
		// just because they coincide in value today.
		//
		// `cabecalho_texto`/`rodape`: NOT ACCEPTED here, and the refusal is
		// due to LACK OF CONFIRMATION — NOT because Meta forbids it. The
		// third-party sources disagree on whether Flow supports
		// header/footer, and none of them was confirmed in this reading.
		// Refusing now is ADDITIVE later, if someone ever confirms Meta
		// accepts it; accepting now and being wrong would be a contract
		// BREAK later. The guard that refuses both is the generic one
		// right after this switch — "flow" deliberately did NOT go into
		// the list of types that accept them.
	default:
		return fmt.Errorf("%w: %q", ErrUnknownType, p.Type)
	}

	// AFTER the switch, and not before: here the type is already known to
	// be valid. A `cabecalho` in a MADE-UP type has to come out as
	// ErrUnknownType — the real problem is the type, and sending
	// someone to fix the header would make them fix the wrong thing (the
	// same reason ErrMixedButtons comes before the required fields).
	//
	// The refusal exists because SILENCE would be worse: MetaBody only
	// assembles `components` in the template branch, so a header sent in
	// `texto` would be discarded with no error at all.
	if p.Type != "template" {
		if p.Header != nil {
			return fmt.Errorf("%w: cabecalho em %s (so template tem components, "+
				"e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
		}
		if len(p.TemplateButtons) > 0 {
			return fmt.Errorf("%w: botoes_template em %s (so template tem components, "+
				"e eles sumiriam sem erro)", ErrFieldForbidden, p.Type)
		}
	}

	// `cabecalho_texto`/`rodape` (FREE-TEXT header/footer of the
	// `interactive` object, T-137) only has a shape in MetaBody in the
	// "botoes", "cta_url" and "lista" branches (T-145 extended the reuse to
	// lista). Accepting one of them in a different type would silently
	// discard it during assembly — this project's most expensive failure
	// shape. The message for `tipo:"template"` sends them to the right
	// place: the template header is `cabecalho` (a different shape), and
	// the template footer is FIXED at registration and accepts no
	// parameter at all.
	if p.Type != "botoes" && p.Type != "cta_url" && p.Type != "lista" {
		if p.HeaderText != "" {
			if p.Type == "template" {
				return fmt.Errorf("%w: cabecalho_texto em template (o header de template "+
					"e `cabecalho`, com outra forma; use-o no lugar)", ErrFieldForbidden)
			}
			return fmt.Errorf("%w: cabecalho_texto em %s (so botoes, cta_url e lista tem "+
				"header de texto livre, e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
		}
		if p.Footer != "" {
			if p.Type == "template" {
				return fmt.Errorf("%w: rodape em template (o rodape de template e fixo no "+
					"cadastro e nao aceita parametro nenhum)", ErrFieldForbidden)
			}
			return fmt.Errorf("%w: rodape em %s (so botoes, cta_url e lista tem footer "+
				"de texto livre, e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
		}
	}

	// `botao_titulo` (the action button's label) only has a shape in
	// MetaBody in the "cta_url", "lista" and "flow" branches (T-145,
	// extended by T-154: the SAME consumer field, a field that stays A
	// SINGLE ONE — see the comment on validateButtonTitle). In "botoes" the
	// field is already refused EARLIER, inside the switch
	// (ErrMixedButtons) — this guard only reaches the types that get
	// this far without having blocked the field before.
	if p.Type != "cta_url" && p.Type != "lista" && p.Type != "flow" && p.ButtonTitle != "" {
		return fmt.Errorf("%w: botao_titulo em %s (so cta_url, lista e flow tem botao de acao, "+
			"e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
	}

	// `secoes` only has a shape in MetaBody in the "lista" branch
	// (T-145). Accepting it in another type would silently discard it
	// during assembly.
	if p.Type != "lista" && len(p.Sections) > 0 {
		return fmt.Errorf("%w: secoes em %s (so lista tem secoes, "+
			"e elas sumiriam sem erro)", ErrFieldForbidden, p.Type)
	}

	// `botoes` (Request.Buttons, {id,titulo}) and `botoes_template` (T-044,
	// {indice,tipo,texto|payload}) ARE TWO DIFFERENT THINGS that only
	// ended up looking alike by name after T-044 — the first is the whole
	// body of a plain interactive message (tipo:"botoes"), the second is a
	// TEMPLATE parameter (tipo:"template"). Without this guard, `botoes`
	// sent in a template request would be silently discarded during
	// assembly (MetaBody only reads p.Buttons in the "botoes" branch):
	// the consumer would confuse the two similar names and the template
	// would go out without the button, with a 200 in the response.
	// `cta_url` is left out of the "template only" case below because it
	// already has its OWN, more specific guard (ErrMixedButtons, in
	// the switch above).
	if p.Type != "botoes" && p.Type != "cta_url" && len(p.Buttons) > 0 {
		return fmt.Errorf("%w: botoes em %s (use botoes_template para botao de "+
			"TEMPLATE, ou tipo:\"botoes\" para mensagem interativa comum)", ErrFieldForbidden, p.Type)
	}

	// Same guard family: `reacao` and `localizacao` only have a shape in
	// their own type's MetaBody. Accepting one of them in a different
	// type would silently discard it during assembly — this project's
	// most expensive failure shape.
	if p.Type != "reacao" && p.Reaction != nil {
		return fmt.Errorf("%w: reacao em %s (so reacao usa esse campo, "+
			"e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
	}
	if p.Type != "localizacao" && p.Location != nil {
		return fmt.Errorf("%w: localizacao em %s (so localizacao usa esse campo, "+
			"e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
	}
	// `contatos` only has a shape in its own type's MetaBody (T-146).
	// Same guard family as reacao/localizacao right above.
	if p.Type != "contatos" && len(p.Contacts) > 0 {
		return fmt.Errorf("%w: contatos em %s (so contatos usa esse campo, "+
			"e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
	}
	// `fluxo` only has a shape in its own type's MetaBody (T-154). Same
	// guard family as reacao/localizacao/contatos right above.
	if p.Type != "flow" && p.Flow != nil {
		return fmt.Errorf("%w: fluxo em %s (so flow usa esse campo, "+
			"e ele sumiria sem erro)", ErrFieldForbidden, p.Type)
	}

	return p.refuseBase64()
}

// mediaHeaders are the categories THIS gateway knows how to assemble as
// a template header. The list is OURS and conservative, the same way the
// mime one in meta/media.go is: it refuses what we don't know instead of
// sending it and hoping. `audio` and `sticker` are valid categories in the
// `midia` type and are not here — we don't know their header shape, and
// making it up would be asking for a 400 from Meta discovered in
// production.
var mediaHeaders = map[meta.Category]bool{
	meta.CategoryImage:    true,
	meta.CategoryVideo:    true,
	meta.CategoryDocument: true,
}

// validateHeader refuses everything assembly would silently discard.
//
// The header's `tipo` discriminates, and the sibling fields are mutually
// exclusive: each of them only exists in the body of ONE of the types, so
// accepting the field in the wrong type would erase it with no error at
// all — this project's most expensive failure shape.
func (p *Request) validateHeader() error {
	c := p.Header
	if c == nil {
		return nil
	}
	// Trim BEFORE deciding, and keep the trimmed value: presence is not
	// content, and a field of just spaces would reach Meta just like that.
	c.Type = strings.TrimSpace(c.Type)
	c.Text = strings.TrimSpace(c.Text)
	c.MediaID = strings.TrimSpace(c.MediaID)
	c.Filename = strings.TrimSpace(c.Filename)

	if c.Type == "texto" {
		if c.MediaID != "" {
			return fmt.Errorf("%w: cabecalho.media_id em cabecalho de texto", ErrFieldForbidden)
		}
		if c.Filename != "" {
			return fmt.Errorf("%w: cabecalho.nome_arquivo em cabecalho de texto", ErrFieldForbidden)
		}
		if c.Text == "" {
			return fmt.Errorf("%w: cabecalho.texto", ErrFieldRequired)
		}
		return nil
	}

	// The vocabulary is the SAME as `categoria`, read from the SAME table:
	// a second name list would diverge from it on the first change.
	cat, known := meta.KnownCategory(c.Type)
	if !known || !mediaHeaders[cat] {
		return fmt.Errorf("%w: %q (use texto, imagem, video ou documento)",
			ErrUnknownHeaderType, c.Type)
	}
	if c.Text != "" {
		return fmt.Errorf("%w: cabecalho.texto em cabecalho de %s (so o cabecalho de "+
			"texto o carrega, e ele sumiria sem erro)", ErrFieldForbidden, cat)
	}
	if c.MediaID == "" {
		return fmt.Errorf("%w: cabecalho.media_id (obtenha um em POST /v1/media)", ErrFieldRequired)
	}
	// Order matters: base64 has its OWN fix, more informative than "invalid
	// shape", and the two cases would fall into the same refusal if the
	// shape were checked first.
	if containsBase64(c.MediaID) {
		return fmt.Errorf("%w: cabecalho.media_id; suba os bytes em POST /v1/media e "+
			"mande o media_id que ele devolve", ErrMediaBase64)
	}
	// THE MEDIA HEADER IS BY media_id, NEVER BY RAW URL — and this line is
	// the guard. A raw URL makes META GO FETCH the file, and when it
	// doesn't fetch (host down, TLS, 404) there's no error at all on our
	// side: the template arrives without the document. It's the same
	// silent failure that made POST /v1/media exist. The valid shape comes
	// from meta.MediaIDValid, which is the SAME rule used to assemble a
	// URL with the id — not a copy of it.
	if !meta.MediaIDValid(c.MediaID) {
		return fmt.Errorf("%w: cabecalho.media_id (URL crua nao serve: a Meta iria BUSCAR o "+
			"arquivo e falha calado quando nao busca; suba os bytes em POST /v1/media)",
			ErrInvalidMediaID)
	}
	// THE SAME table the assembler consults (meta/media.go): only document
	// has a filename, so accepting it in the others would silently discard
	// it.
	if c.Filename != "" && !meta.AcceptsFilename(cat) {
		return fmt.Errorf("%w: cabecalho.nome_arquivo em %s (so documento tem nome de "+
			"arquivo, e ele sumiria sem erro)", ErrFieldForbidden, cat)
	}
	return nil
}

// validateTemplateButtons checks `botoes_template`, the ONLY template
// button parameter field since T-045.
//
// The index belongs to the TEMPLATE, not to this list: it says which button
// declared in Meta the parameter belongs to. Getting the index wrong
// produces no error at all — the token goes to the wrong button and the
// customer lands in the wrong place —, so the things the gateway CAN check
// on its own (negative index and repeated index) are checked.
//
// `vistos` IS A SINGLE ONE AND THAT IS STRUCTURAL TODAY, not discipline.
// Until T-045 there was a second field (`botoes_url`) writing into the SAME
// index space, and the guard had to cross the two by hand:
// `botoes_url[indice:0]` + `botoes_template[indice:0]` was the SAME button
// declared twice, Meta would silently discard one of the two parameters,
// and the consumer didn't choose which. With a single field, that state
// stopped being EXPRESSIBLE — and inexpressible is stronger than refused,
// because it doesn't depend on anyone remembering the guard on the day
// someone touches this (this project's mother trap, docs/ARMADILHAS.md).
func (p *Request) validateTemplateButtons() error {
	seen := make(map[int]bool, len(p.TemplateButtons))

	for i := range p.TemplateButtons {
		b := &p.TemplateButtons[i]
		b.Type = strings.TrimSpace(b.Type)
		b.Text = strings.TrimSpace(b.Text)
		b.Payload = strings.TrimSpace(b.Payload)

		if b.Index < 0 {
			return fmt.Errorf("%w: botoes_template[%d].indice = %d (o indice e a posicao do botao "+
				"no template, comecando em 0)", ErrButtonIndex, i, b.Index)
		}
		// TWO PARAMETERS FOR THE SAME BUTTON has no defined outcome: one of
		// the two gets discarded, and it's not us who chooses which.
		// Refusing is the only way for the consumer to know which token
		// they would lose.
		if seen[b.Index] {
			return fmt.Errorf("%w: dois parametros para o botao de indice %d", ErrButtonIndex, b.Index)
		}
		seen[b.Index] = true

		switch b.Type {
		case "url":
			// `payload` would silently vanish during assembly: only
			// resposta_rapida carries it (templateComponents).
			if b.Payload != "" {
				return fmt.Errorf("%w: botoes_template[%d].payload em tipo url (so resposta_rapida "+
					"o carrega, e ele sumiria sem erro)", ErrFieldForbidden, i)
			}
			if b.Text == "" {
				return fmt.Errorf("%w: botoes_template[%d].texto", ErrFieldRequired, i)
			}
		case "resposta_rapida":
			// `texto` would silently vanish during assembly: only url
			// carries it.
			if b.Text != "" {
				return fmt.Errorf("%w: botoes_template[%d].texto em tipo resposta_rapida (so url "+
					"o carrega, e ele sumiria sem erro)", ErrFieldForbidden, i)
			}
			// EMPTY `payload` is a named error, not a parameter-less block:
			// Meta would accept a quick_reply block without `payload` and
			// the click would come back with no recognizable id at all —
			// the SAME defect this task exists to close, only caused by us
			// instead of by Meta.
			if b.Payload == "" {
				return fmt.Errorf("%w: botoes_template[%d].payload", ErrFieldRequired, i)
			}
		default:
			return fmt.Errorf("%w: %q (use url ou resposta_rapida)", ErrUnknownTemplateButtonType, b.Type)
		}
	}

	return nil
}

// validateReaction checks `reacao`. See the comment on ReactionRequest for the
// source of the removal (experiment with a device, consumer-a, 2026-07-26)
// and for why ABSENT `emoji` stays ErrFieldRequired instead of becoming a
// removal — only the empty string removes.
func (p *Request) validateReaction() error {
	if p.Reaction == nil {
		return fmt.Errorf("%w: reacao", ErrFieldRequired)
	}
	// Trim BEFORE deciding, same rule as the rest of this file.
	p.Reaction.Target = strings.TrimSpace(p.Reaction.Target)

	if p.Reaction.Target == "" {
		return fmt.Errorf("%w: reacao.alvo", ErrFieldRequired)
	}
	if p.Reaction.Emoji == nil {
		return fmt.Errorf("%w: reacao.emoji (mande \"\" para remover a reacao, "+
			"nao omita a chave)", ErrFieldRequired)
	}
	// The pointer is reassigned to the TRIMMED value — not merely checked —
	// for the same reason as the rest of this file: the body assembled
	// next (MetaBody) does not revalidate, and an untrimmed " " emoji
	// would reach Meta just like that, when the intent was to remove.
	trimmed := strings.TrimSpace(*p.Reaction.Emoji)
	p.Reaction.Emoji = &trimmed
	// aparado == "" is a valid REMOVAL (see the comment on ReactionRequest) —
	// there is deliberately no empty check here.
	return nil
}

// validateLocation checks `localizacao`. See the comment on
// LocationRequest for why Latitude/Longitude are pointers.
func (p *Request) validateLocation() error {
	if p.Location == nil {
		return fmt.Errorf("%w: localizacao", ErrFieldRequired)
	}
	l := p.Location
	// Name/Address are optional free text; Latitude/Longitude are NOT
	// trimmed because they aren't strings — they're pointers to a number,
	// and their requiredness is the nil check below, not content.
	l.Name = strings.TrimSpace(l.Name)
	l.Address = strings.TrimSpace(l.Address)

	if l.Latitude == nil {
		return fmt.Errorf("%w: localizacao.latitude", ErrFieldRequired)
	}
	if l.Longitude == nil {
		return fmt.Errorf("%w: localizacao.longitude", ErrFieldRequired)
	}
	return nil
}

// contactDateLayout is the ONLY date shape `contatos[i].birthday` accepts
// (T-146) — confirmed in the official doc (developers.facebook.com/docs/
// whatsapp/cloud-api/messages/contacts-messages, read 2026-08-20).
// time.Parse with this layout is enough as validation: besides requiring
// the exact "YYYY-MM-DD" shape (rejects "2026-1-1", extra text, a year with
// too many or too few digits), it already refuses an invalid calendar date
// ("2026-13-01", "2026-02-30") without needing a separate regex.
const contactDateLayout = "2006-01-02"

// validateContacts checks `contatos`, the body of `tipo:"contatos"` (T-146).
//
// ONLY TWO THINGS ARE CHECKED HERE, on purpose — see the comment on the
// Request.Contacts field and item 4 of T-146: (1) `contatos` cannot be
// empty; (2) each card needs `name.formatted_name`, the ONLY required field
// the doc declares; (3) `birthday`, when filled in, needs the YYYY-MM-DD
// shape, the ONLY field with a declared format. NO OTHER CEILING IS
// INVENTED: Meta's page declares no size or quantity limit for the other
// ~20 fields, and T-143 already recorded "mirroring a limit no one read" as
// a decision NOT to make — the consumer receives the right field and number
// in `detalhe_meta` (T-141) if Meta refuses something this gateway didn't
// check.
//
// Errors name the PATH (`contatos[i].name.formatted_name`), in the same
// pattern as T-140/T-145 — "formatted_name" alone doesn't say which of the
// several cards was wrong.
func (p *Request) validateContacts() error {
	if len(p.Contacts) == 0 {
		return fmt.Errorf("%w: contatos", ErrFieldRequired)
	}
	for i := range p.Contacts {
		c := &p.Contacts[i]
		// Trim BEFORE deciding, same rule as the rest of this file — only
		// on the two fields this function actually checks.
		c.Name.FormattedName = strings.TrimSpace(c.Name.FormattedName)
		c.Birthday = strings.TrimSpace(c.Birthday)

		if c.Name.FormattedName == "" {
			return fmt.Errorf("%w: contatos[%d].name.formatted_name", ErrFieldRequired, i)
		}
		if c.Birthday != "" {
			if _, err := time.Parse(contactDateLayout, c.Birthday); err != nil {
				return fmt.Errorf("%w: contatos[%d].birthday (use YYYY-MM-DD)", ErrInvalidContactDate, i)
			}
		}
	}
	return nil
}

// headerTextAndFooterLimit is the character ceiling (RUNES, not bytes —
// both fields accept emoji) the Cloud API accepts in the FREE-TEXT
// header/footer of an interactive message (botoes, cta_url).
//
// The FOOTER has "Maximum 60 characters" confirmed in the official source
// (developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-
// reply-buttons-messages, read on 2026-08-20). The HEADER's is the value
// from the Cloud API's interactive-message-object reference, but that was
// NOT reconfirmed in this specific reading — it's stated here rather than
// turned into invented certainty.
const headerTextAndFooterLimit = 60

// ctaURLDisplayTextLimit is the character ceiling (RUNES, not bytes — the
// label accepts accents and emoji) of the `display_text` of a
// `tipo:"cta_url"` button (T-139).
//
// 🔴 The official source IS SILENT: the Cloud API's messages reference
// (developers.facebook.com/docs/whatsapp/cloud-api/reference/messages, read
// on 2026-08-20) DECLARES NO limit at all for `display_text`. The 20 came
// from a MEASUREMENT on a third party's device, done by consumer-b on
// 18/08/2026, by manual bisection (17 passed, 21 failed, then 19 and 20
// confirmed the exact ceiling) after Meta refused a send with
// `(#131009) Parameter value is not valid` — an error that does NOT name
// which parameter. See docs/ARMADILHAS.md.
//
// Do not treat as documented: if Meta changes this limit, only a new
// on-device measurement fixes it.
//
// A constant of ITS OWN, not shared with listButtonLimit below (T-149):
// same value today, but they are two DIFFERENT Cloud API fields
// (`display_text` of cta_url x `action.button` of lista) with different
// provenances — one measured, the other documented —, and Meta can change
// one without the other. This does NOT split the CONSUMER field:
// `botao_titulo` stays A SINGLE ONE in the JSON, reused by both types — see
// the comment on validateButtonTitle, which is what chooses which of these
// two constants to use.
const ctaURLDisplayTextLimit = 20

// listButtonLimit is the character ceiling (RUNES, not bytes) of the
// `action.button` of `tipo:"lista"` (T-145).
//
// DOCUMENTED: the official list-messages reference
// (developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-
// list-messages, read on 2026-08-20) declares "Maximum length: 20
// characters" for the button. It's a documented source, not a measured one
// — not yet confirmed against real production.
//
// A constant of ITS OWN, not shared with ctaURLDisplayTextLimit above
// (T-149) — same reason as that comment: two different Cloud API fields,
// same value today by coincidence, different provenances.
const listButtonLimit = 20

// flowCtaLimit is the character ceiling (RUNES, not bytes) of the
// `flow_cta` of `tipo:"flow"` (T-154) — the label of the button that opens
// the Flow.
//
// 🔴 PROVENANCE: THE WEAKEST OF THIS FILE'S THREE BUTTON-CEILING CONSTANTS.
// quickReplyButtonTitleLimit and ctaURLDisplayTextLimit came from
// MEASUREMENT (the second one third-party, the first one ours);
// listButtonLimit came from OFFICIAL DOC. This value came from BSP
// DOCUMENTATION (360dialog) and a THIRD-PARTY SDK (whatsapp-api-js), read
// on 2026-08-20 — never confirmed against Meta, never measured, never read
// in the official doc (which was not reached for Flows — see the comment
// on FlowRequest, in mensagem.go). Only a real call against Meta fixes this
// value if it is wrong.
//
// A constant of ITS OWN, not shared with ctaURLDisplayTextLimit nor with
// listButtonLimit (same rule as T-149, applied to a THIRD field this
// time): `flow_cta` is a different Cloud API field than `display_text`
// (cta_url) and `action.button` (lista), it coincides in value with both
// today, and Meta can change any of the three without changing the others.
const flowCtaLimit = 20

// flowMessageVersion is the FIXED value of `flow_message_version` in the
// body of `tipo:"flow"` (T-154, see MetaBody in corpo.go) — NOT A
// CONSUMER FIELD, same pattern as `action.name: "send_location"` in T-150.
// The third-party source (see the comment on FlowRequest) uses "3" in
// every example read on 2026-08-20; there's no field for it here because
// there's no decision at all for the consumer to make about this.
const flowMessageVersion = "3"

// quickReplyButtonTitleLimit is the character ceiling (RUNES, not
// bytes) of the `titulo` of each item in `botoes[]` of a `tipo:"botoes"`
// message (the `reply.title` of the Cloud API's quick-reply button).
//
// A constant of ITS OWN, not shared with ctaURLDisplayTextLimit above:
// same value today, but they are two different Cloud API fields
// (`reply.title` of botoes[] x `display_text` of cta_url) and Meta can
// change one without the other.
//
// 🔴 MEASURED BY US, against Meta's REAL PRODUCTION, on 2026-08-20, on the
// `tenant-one` instance, with messages actually sent (not a third-party
// measurement like the ctaURLDisplayTextLimit one above):
//
//	20 runes / 20 bytes            -> 200
//	21 runes                       -> 400 (#131009) Parameter value is not valid
//	25 runes                       -> 400 same
//	20 runes / 40 bytes (20 "ç")   -> 200
//
// The last line is the proof that the count is by RUNE, not by byte: 40
// bytes passed because they were 20 characters. The error is the SAME
// (#131009) anonymous one from cta_url's botao_titulo — it doesn't say
// which field was wrong. See docs/ARMADILHAS.md.
//
// Do not treat this number as documented: if Meta changes the limit, only
// a new on-device measurement fixes this value.
const quickReplyButtonTitleLimit = 20

// quickReplyButtonCountLimit is the ITEM ceiling (not character)
// of the `botoes[]` list of a `tipo:"botoes"` message.
//
// 🔴 MEASURED IN PRODUCTION, against the real Meta, on 2026-08-20, on
// v0.52.0 already live, with four buttons actually sent by the `tenant-one`
// instance:
//
//	HTTP 400
//	codigo_meta  131009
//	mensagem     (#131009) Parameter value is not valid
//	detalhe_meta Invalid buttons count. Min allowed buttons: 1, Max allowed buttons: 3
//
// This `detalhe_meta` is T-141's pass-through working for the first time
// against the real Meta — and it was what revealed this defect. The limit
// came READ straight from the error's own text, not bisected like the
// character ceiling above: it's the demonstration that the pass-through
// pays for itself.
//
// 🔴 LIMIT OF THIS APPROACH (a project decision, not an oversight): the
// gateway will NOT mirror Meta's entire limit table. A mirror of a
// third-party table ages and starts lying — Meta changes the number and
// this constant is silently wrong until the next measurement. And since
// T-141 the consumer ALREADY receives the right field and number in
// `detalhe_meta` when Meta refuses, so a mirror here adds no information
// at all in most cases, just one more place to go stale.
//
// Validation ON INPUT (like this one) is reserved for cases where (a) the
// failure would be SILENT without it, or (b) the error Meta would return
// doesn't serve as a diagnostic. The button count falls into that
// exception because it's a STRUCTURAL constant of the block the gateway
// itself assembles (the `interactive.action.buttons` array of the JSON
// that goes out from here) — not because "every limit must be mirrored".
// Do not treat this number as documented: if Meta changes the ceiling,
// only a new measurement fixes this value.
const quickReplyButtonCountLimit = 3

// validateHeaderTextAndFooter trims and limits `cabecalho_texto`/`rodape`,
// valid only in "botoes" and "cta_url" (T-137). Called from both branches
// of the switch in Validate(), never duplicated: if the two copies knew the
// rule, they would diverge on the first change.
//
// A field that ends up empty AFTER the trim counts as ABSENT — same rule as
// the rest of this file —, so the size cutoff only runs when the trimmed
// value isn't empty.
func (p *Request) validateHeaderTextAndFooter() error {
	p.HeaderText = strings.TrimSpace(p.HeaderText)
	p.Footer = strings.TrimSpace(p.Footer)

	if p.HeaderText != "" {
		if n := utf8.RuneCountInString(p.HeaderText); n > headerTextAndFooterLimit {
			return fmt.Errorf("%w: cabecalho_texto (%d runas, maximo %d)",
				ErrFieldTooLong, n, headerTextAndFooterLimit)
		}
	}
	if p.Footer != "" {
		if n := utf8.RuneCountInString(p.Footer); n > headerTextAndFooterLimit {
			return fmt.Errorf("%w: rodape (%d runas, maximo %d)",
				ErrFieldTooLong, n, headerTextAndFooterLimit)
		}
	}
	return nil
}

// validateButtonTitle checks `botao_titulo`, a CONSUMER field REUSED by
// "cta_url" and "lista" since T-145, and by "flow" since T-154 — A SINGLE
// field in the JSON, a project decision T-149 does NOT undo: inventing a
// separate `lista_botao`/`flow_botao` would create two names for a single
// thing, the same duplication this project already paid dearly for in
// `botoes` x `botoes_template`. Called from all three branches of the
// switch in Validate(), never duplicated: if the copies knew the rule, they
// would diverge on the first change.
//
// Inside, however, the CEILING used depends on the type (T-149, extended by
// T-154): ctaURLDisplayTextLimit, listButtonLimit and flowCtaLimit are
// constants OF THEIR OWN because they mirror THREE DIFFERENT Cloud API
// fields (`display_text` of cta_url, `action.button` of lista, `flow_cta`
// of flow) that today have the SAME value by coincidence and different
// provenances — see the comment on each constant. Separating the internal
// constant doesn't split the consumer field: it's this SAME function, just
// choosing which of the three to use.
func (p *Request) validateButtonTitle() error {
	if p.ButtonTitle == "" {
		return fmt.Errorf("%w: botao_titulo", ErrFieldRequired)
	}
	limit := ctaURLDisplayTextLimit
	switch p.Type {
	case "lista":
		limit = listButtonLimit
	case "flow":
		limit = flowCtaLimit
	}
	if n := utf8.RuneCountInString(p.ButtonTitle); n > limit {
		return fmt.Errorf("%w: botao_titulo (%d runas, maximo %d)",
			ErrFieldTooLong, n, limit)
	}
	return nil
}

// validateFlow checks `fluxo`, the body of `tipo:"flow"` (T-154). See the
// comment on FlowRequest: the SHAPE is confirmed against Meta (T-156),
// RENDERING is not, and the PARAMETERS remain third-hand — read it before
// changing any guard here.
func (p *Request) validateFlow() error {
	if p.Flow == nil {
		return fmt.Errorf("%w: fluxo", ErrFieldRequired)
	}
	f := p.Flow
	// Trim BEFORE deciding, same rule as the rest of this file.
	f.ID = strings.TrimSpace(f.ID)
	f.Name = strings.TrimSpace(f.Name)
	f.Token = strings.TrimSpace(f.Token)
	f.Action = strings.TrimSpace(f.Action)
	f.Screen = strings.TrimSpace(f.Screen)

	// `id` and `nome` are MUTUALLY EXCLUSIVE and at least one is required —
	// the third-party source says "one or the other, never both" (see the
	// comment on FlowRequest). Refuses both together AND both absent,
	// naming both fields in both messages: whoever reads it knows which of
	// the two cases hit.
	if f.ID != "" && f.Name != "" {
		return fmt.Errorf("%w: fluxo.id e fluxo.nome juntos (mande so um dos dois)", ErrInvalidFlowIDName)
	}
	if f.ID == "" && f.Name == "" {
		return fmt.Errorf("%w: fluxo.id ou fluxo.nome (um dos dois e obrigatorio)", ErrInvalidFlowIDName)
	}

	// `token` is the identifier that matches the Flow's response with what
	// the consumer was doing (see the comment on FlowRequest). Without it
	// the response comes back and no one knows whose it is.
	if f.Token == "" {
		return fmt.Errorf("%w: fluxo.token", ErrFieldRequired)
	}

	// Empty normalizes to the default "navigate" (third-party source) — the
	// same technique as p.Reaction.Emoji and other fields in this file that
	// normalize the pointer/value BEFORE assembly (MetaBody, which does
	// not revalidate) has to decide again.
	if f.Action == "" {
		f.Action = "navigate"
	}
	switch f.Action {
	case "navigate":
		// The third-party source says the payload is required in this
		// case — refuse here instead of letting Meta refuse without
		// naming the field.
		if f.Screen == "" {
			return fmt.Errorf("%w: fluxo.tela (obrigatoria quando fluxo.acao e \"navigate\")",
				ErrFieldRequired)
		}
	case "data_exchange":
		// `tela` is OPTIONAL in this branch — there's no requiredness guard.
	default:
		return fmt.Errorf("%w: %q (use navigate ou data_exchange)", ErrUnknownFlowAction, f.Action)
	}

	return p.validateButtonTitle()
}

// The ceilings of `secoes`/`itens` of `tipo:"lista"` (T-145).
//
// 🔴 PROVENANCE: DIFFERENT FROM THAT OF THE T-140/T-143 CEILINGS RIGHT
// ABOVE. Those came from MEASUREMENT against real production Meta — the
// number only exists because someone sent for real and Meta returned a 400
// stating the ceiling. THESE SIX came from OFFICIAL DOCUMENTATION
// (developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-
// list-messages, read on 2026-08-20), NOT from measurement against
// production — better provenance than the button ceilings (Meta declares
// these explicitly, unlike cta_url's `display_text`), but still not
// confirmed by a real send. Mixing the two provenances in the explanation
// would be false doc: if Meta changes one of these numbers without
// updating the doc, only a measurement corrects it.
const (
	listSectionsLimit        = 10
	listSectionTitleLimit    = 24
	listItemIDLimit          = 200
	listItemTitleLimit       = 24
	listItemDescriptionLimit = 72

	// totalListItemsLimit: the ceiling is NOT PER SECTION, it's
	// SUMMED across ALL sections of a request — 3 sections of 4 items each
	// (12 total) is REFUSED even though no single section goes over 10.
	// It's the easiest point of this task to implement wrong: wrong, it
	// only shows up as a 400 from Meta in production, never in a test that
	// checks section by section.
	totalListItemsLimit = 10
)

// validateList checks `secoes`, the body of `tipo:"lista"` (T-145). Errors
// name the PATH (`secoes[i].itens[j].campo`), in the T-140 pattern —
// "titulo" alone doesn't say which of the several titles was wrong.
func (p *Request) validateList() error {
	if len(p.Sections) == 0 {
		return fmt.Errorf("%w: secoes", ErrFieldRequired)
	}
	if n := len(p.Sections); n > listSectionsLimit {
		return fmt.Errorf("%w: secoes (%d secoes, maximo %d)", ErrFieldTooLong, n, listSectionsLimit)
	}

	// THE SUMMED TOTAL, NOT PER SECTION — see the comment on
	// totalListItemsLimit. Accumulated through the loop and checked
	// ONCE at the end, after every section/item has already been validated
	// individually.
	totalItems := 0

	for i := range p.Sections {
		s := &p.Sections[i]
		s.Title = strings.TrimSpace(s.Title)
		if s.Title == "" {
			return fmt.Errorf("%w: secoes[%d].titulo", ErrFieldRequired, i)
		}
		if n := utf8.RuneCountInString(s.Title); n > listSectionTitleLimit {
			return fmt.Errorf("%w: secoes[%d].titulo (%d runas, maximo %d)",
				ErrFieldTooLong, i, n, listSectionTitleLimit)
		}
		// A section without an item has no shape in Meta (`rows` would be
		// empty) — refused here as a required field, not as a ceiling:
		// it's not a number from the doc, it's the same structural logic
		// as "botoes" (Request.Buttons) requiring at least one item.
		if len(s.Items) == 0 {
			return fmt.Errorf("%w: secoes[%d].itens", ErrFieldRequired, i)
		}

		for j := range s.Items {
			it := &s.Items[j]
			it.ID = strings.TrimSpace(it.ID)
			it.Title = strings.TrimSpace(it.Title)
			it.Description = strings.TrimSpace(it.Description)

			if it.ID == "" {
				return fmt.Errorf("%w: secoes[%d].itens[%d].id", ErrFieldRequired, i, j)
			}
			if n := utf8.RuneCountInString(it.ID); n > listItemIDLimit {
				return fmt.Errorf("%w: secoes[%d].itens[%d].id (%d runas, maximo %d)",
					ErrFieldTooLong, i, j, n, listItemIDLimit)
			}
			if it.Title == "" {
				return fmt.Errorf("%w: secoes[%d].itens[%d].titulo", ErrFieldRequired, i, j)
			}
			if n := utf8.RuneCountInString(it.Title); n > listItemTitleLimit {
				return fmt.Errorf("%w: secoes[%d].itens[%d].titulo (%d runas, maximo %d)",
					ErrFieldTooLong, i, j, n, listItemTitleLimit)
			}
			if it.Description != "" {
				if n := utf8.RuneCountInString(it.Description); n > listItemDescriptionLimit {
					return fmt.Errorf("%w: secoes[%d].itens[%d].descricao (%d runas, maximo %d)",
						ErrFieldTooLong, i, j, n, listItemDescriptionLimit)
				}
			}
			totalItems++
		}
	}

	if totalItems > totalListItemsLimit {
		return fmt.Errorf("%w: secoes[*].itens (%d itens somados, maximo %d)",
			ErrFieldTooLong, totalItems, totalListItemsLimit)
	}

	return nil
}

// RequestHash identifies the REQUEST (not the key) to bind idempotency to
// it — see config.ErrKeyWithDifferentRequest.
//
// Call it ONLY AFTER p.Validate(): Validate trims the fields, and a hash taken
// before it would make " 5511..." and "5511..." — the SAME request — collide
// as different requests, turning into a false 422 on the first legitimate
// retry.
//
// DETERMINISTIC by construction: Request is a struct (not a map), so
// json.Marshal always serializes the fields in the same declaration order —
// there's no map key whose iteration order varies between calls.
func RequestHash(p Request) string {
	// The Marshal error is discarded, not ignored by oversight: Request has
	// no field of a type that encoding/json rejects (chan, func, complex),
	// so json.Marshal here never fails. The requested signature returns just
	// a string; forcing a second return value just for an error this type
	// can't produce would replace clarity with ceremony.
	//
	// THE ONLY TWO DOORS through which a Marshal error could enter are the
	// json.RawMessage fields (`Components` and `RemovedURLButtons`): invalid
	// raw bytes would make them fail. Both are closed by the rule at the top
	// of this function — Validate REJECTS both fields when they aren't
	// empty, and RequestHash is only called after it (handler.go:268). If one
	// day a RawMessage becomes ACCEPTED, this premise falls with it.
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// refuseBase64 prevents a send that would fail SILENTLY.
//
// The Cloud API does not accept base64 content — only a public URL or a
// media_id. A system on this network sent a PDF in base64 and the send
// failed with no visible error until someone went looking. The error needs
// to SAY what to do instead.
//
// THE GUARD HAS TO GET BOTH SIDES RIGHT, and each half of the condition
// exists for a different reason:
//
//   - `data:` alone would reject "data: 23/07" — a date spelled out in
//     Portuguese ("data" = "date");
//   - `;base64,` alone would reject "base64 is a way to encode";
//   - the search is CASE-INSENSITIVE because the data: scheme is
//     case-insensitive by definition (RFC 3986/2397): DATA:, Data:, and
//     data: are the same URI;
//   - the search is at ANY POSITION because nothing guarantees the payload
//     occupies the whole field from the start — "here's the pdf: data:...;
//     base64,..." is as broken a send as one that starts at position zero.
//
// A guard broader than needed breaks legitimate conversation; a narrower one
// lets through the send that fails silently. Both failures land on the end
// customer.
func (p *Request) refuseBase64() error {
	if containsBase64(p.Text) {
		return fmt.Errorf("%w: use POST /v1/media para obter um media_id, ou uma URL publica", ErrBase64)
	}
	// The media fields use the SAME detector, not a copy of it: the guard
	// has already been fixed three times (docs/ARMADILHAS.md, "Validação"),
	// and a second detection would inherit the wrong version from one of
	// those rounds. media_id is included here because it's exactly where
	// someone pastes the bytes thinking the gateway will resolve it — and
	// the error needs to say where a real id comes from, or the silent
	// failure just turns into a rude one.
	//
	// A slice, not a map: a map's iteration order varies between runs, and
	// the SAME request would produce different error messages from one call
	// to the next — a flaky test today, and someone "fixing" the test
	// tomorrow.
	for _, field := range []struct{ name, value string }{
		{"media_id", p.MediaID},
		{"legenda", p.Caption},
		{"nome_arquivo", p.Filename},
	} {
		if containsBase64(field.value) {
			return fmt.Errorf("%w: %s; suba os bytes em POST /v1/media e mande o media_id que ele devolve",
				ErrMediaBase64, field.name)
		}
	}
	return nil
}

// containsBase64 is the detection, in one place. See refuseBase64 for why
// each half of the condition exists.
func containsBase64(s string) bool {
	lower := strings.ToLower(s)
	start := strings.Index(lower, "data:")
	return start >= 0 && strings.Contains(lower[start:], ";base64,")
}
