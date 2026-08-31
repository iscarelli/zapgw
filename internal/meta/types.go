package meta

// EventType: the ONE dialect the consumer receives. The gateway is the only
// thing on the network that sees Meta's shape; on the outside there is only
// this.
type EventType string

const (
	EventTypeMessage        EventType = "message"
	EventTypeStatus         EventType = "status"
	EventTypeTemplateStatus EventType = "template_status"

	// EventTypeTemplateCategory is the `template_category_update` webhook
	// (T-057, 2026-07-28) — Meta announcing it RECLASSIFIED a template's
	// category. NOT the same as EventTypeTemplateStatus: that one is the
	// approval/rejection event, which carries the category as an
	// ATTRIBUTE of the new state; this one is the event DEDICATED to the
	// CHANGE, and it's the only one that gives the direction
	// (`previous_` -> `new_`) and the appeal window. See
	// TemplateCategory.
	EventTypeTemplateCategory EventType = "template_category"

	// EventTypeNumberQuality is the `phone_number_quality_update` webhook
	// (T-058, 2026-07-28) — the daily QUOTA and the number's quality. See
	// NumberQuality.
	EventTypeNumberQuality EventType = "number_quality"

	// EventTypeAccountAlert is the `account_alerts` webhook (T-058,
	// 2026-07-28) — an account problem warning, WITH SEVERITY. See
	// AccountAlert.
	EventTypeAccountAlert EventType = "account_alert"
)

// Reaction: the emoji and target of a message reaction (a message with
// m.Type == "reaction" on Meta's side).
//
// The emoji's ABSENCE from the envelope's JSON (the "emoji" key doesn't
// appear) means the user REMOVED the reaction — not malformation. It's
// Meta's own vocabulary on their side: confirmed at
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/reaction/
// (read on 2026-07-26), which says "When an end user removes a reaction
// emoji, a webhook without the 'emoji' field will be sent." Counting that
// case as a parse error would erase a legitimate signal — the same family
// of data loss this task exists to close. The target (the reacted-to
// message's message_id) is ALWAYS required: without it the item is
// malformed and becomes a counted parse error, never an Event (see
// ParseWebhook).
//
// CORROBORATED BY A REAL CAPTURE, not just by doc (T-026, consumer-a,
// 2026-07-26): the owner reacted and undid the SAME reaction 20s later, and
// the second event arrived without the "emoji" key — not "", not null: the
// key doesn't exist (testdata/corpus/reacao_removida.json). Independent
// corroboration that already existed: consumer-a's
// `_processar_reacao`, in production since 2026-07-20, already treated the
// absence of "emoji" as removal before any conversation between the two
// projects (backend/app/whatsapp/inbound.py, there). The same capture also
// confirmed, by direct observation (not deduction), that the reaction event
// arrives with its OWN "id" at the message level, different from the
// "message_id" inside "reaction" (the target) — checked against
// messageEvent, in parse.go: Event.ID uses m.ID (the event), Target uses
// m.Reaction.MessageID (the target). The two are NEVER the same value in
// the real capture. And the observed emoji ("❤️") is TWO codepoints
// (U+2764 + U+FE0F, variation selector) — testdata/corpus/reacao.json uses
// this exact emoji because a single-codepoint fixture wouldn't exercise
// this path.
type Reaction struct {
	Emoji  string `json:"emoji,omitempty"` // absent = reaction REMOVED
	Target string `json:"target"`          // wamid of the reacted-to message
}

// Location: a point shared by the contact (a message with m.Type ==
// "location" on Meta's side).
//
// Latitude/Longitude have NO omitempty: 0 is a valid coordinate (the
// intersection of the Greenwich meridian and the equator), and omitting a
// zero would silently erase that point from the map. Name/Address are
// optional on Meta's own side.
//
// CORROBORATED BY A REAL CAPTURE (T-026, consumer-a, 2026-07-26): the bare
// pin — WITHOUT "name" and WITHOUT "address" — is the case OBSERVED as
// common (testdata/corpus/localizacao.json); the earlier fixture, derived
// from the doc, carried both and tested the rare case (the doc shows an
// example of a venue pin, which Meta also accepts, but isn't what most
// users send when sharing location through the app).
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
}

// StatusError: the reason for an `errors[]` Meta sent — inside `statuses[]`
// (Status == "failed") OR inside `messages[]` (sub_tipo "unsupported",
// T-033, 2026-07-26). The SAME type serves both places because the
// errors[] item's format is identical in both (code/title/
// error_data.details); what changes is the MEANING, and that difference
// lives in Event.Error (further below) and in the contract, not here: in a
// status event, "error" means "delivery failed"; in a message event, it
// means "Meta received something and the Cloud API doesn't know how to
// represent it" — Meta did NOT fail to deliver anything, it delivered and
// didn't know how to decode the content (the observed case is code 131051,
// "Message type unknown").
//
// Only filled when Meta sent `errors[]` with at least one interpretable
// item. ABSENCE (Event.Error == nil) is different from an StatusError with
// Code 0 and an empty Message — that second shape is never produced by
// the parser; if it were, a "failed" (or an "unsupported") with no
// errors[] in the payload would be indistinguishable from a code-0 error,
// which Meta never uses.
//
// Format confirmed at
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
// (read on 2026-07-26; also checked via the mirrored URL
// developers.facebook.com/docs/whatsapp/cloud-api/webhooks/reference/messages/status/,
// same date, same example):
//
//	"errors":[{"code":131049,
//	           "title":"This message was not delivered to maintain healthy ecosystem engagement.",
//	           "message":"This message was not delivered to maintain healthy ecosystem engagement.",
//	           "error_data":{"details":"In order to maintain a healthy ecosystem engagement, ..."},
//	           "href":"/documentation/business-messaging/whatsapp/support/error-codes"}]
//
// The gateway keeps Code (from `code`), Message (from `title`), and
// Details (from `error_data.details`) — the minimum needed to translate,
// group, and explain to the operator without reimplementing a code table
// that rots the day Meta adds a new one (the same reason no translation
// table exists here: see internal/meta/errors.go). `message` (identical to
// `title` in both examples checked) and `href` are not modeled: `title`
// already says what `message` would say, and `href` has no consumer
// (T-029, 2026-07-26, consumer-a's field-by-field answer checking their own
// translator, `backend/app/telegram/notifier.py:55-69`).
// `error_data.details` is different from both: it's the ONLY part of the
// message that ADDS information instead of repeating the title — without
// it, the warning to the operator is poor exactly in the cases where the
// generic code explains nothing, which are the cases where the message is
// needed most.
//
// `errors[]` is a LIST: Meta can send more than one item. The gateway keeps
// only the FIRST — see statusEvent, in parse.go, and the status event
// section in docs/CONTRATO-CONSUMIDOR.md.
type StatusError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`

	// Details comes from errors[0].error_data.details — a NESTED object
	// that might not exist (Meta only sends error_data for some codes).
	// Its absence cannot bring down Code/Message, which already
	// worked before this task — see statusEvent, in parse.go.
	//
	// A string with `omitempty`, NOT *string: unlike Code/Message
	// (which live inside a *StatusError, where the WHOLE STRUCT's pointer
	// already resolves "did it come or not"), here there's no second
	// shape with its own meaning to distinguish from "absent". "" and
	// "didn't come" are the SAME answer for whoever asks "did a detail
	// come?" — unlike Voice (*bool, in the Event type) and StatusError
	// itself (*StatusError), where the zero value (false, an empty
	// struct) has a different meaning from absent. A pointer here would
	// be ceremony with no guarantee.
	Details string `json:"details,omitempty"`
}

// Billing: under which category Meta billed this delivery — OUR OWN
// vocabulary, not "pricing" (the field Meta sends in the status webhook):
// Meta's format dies in parse.go, like the rest of the envelope (T-041,
// 2026-07-26, requested by consumer-a, who checked before asking that
// "pricing" didn't exist in our contract or in the code).
//
// WHY IT MATTERS, AND IT ISN'T ACCOUNTING TRIVIA: editing a template can
// make Meta reclassify UTILITY -> MARKETING, which changes price and
// sending rules. Without this field, the reclassification only shows up on
// the INVOICE, weeks later; with it, it shows up on the FIRST message
// delivered after the change — Meta saying, on EVERY delivery, under which
// category it billed.
//
// Only Category and Billable are modeled — the MINIMUM that decides
// anything. pricing_model and type (the other two fields Meta sends inside
// "pricing") stay out until someone says what they'd do with them: the
// envelope only grows, so adding later is free, removing later is a
// breaking change.
type Billing struct {
	Category string `json:"category"`

	// Billable is *bool, NOT bool: the SAME rule as "voz" (Event.Voice,
	// above), with a BIGGER consequence — here the difference is about
	// MONEY, not just voice-note-becomes-attachment. "Meta said it
	// doesn't charge" (false) and "Meta said nothing" (absent) are not
	// the same information, and inventing false as a default would hide
	// exactly the case where no one knows.
	Billable *bool `json:"cobravel,omitempty"`
}

// TemplateStatus: what changed about an account TEMPLATE — the content
// of the `message_template_status_update` webhook translated into this
// project's vocabulary (T-043, 2026-07-26). Only filled when Event.Type ==
// EventTypeTemplateStatus.
//
// THE TYPE'S NAME IS DELIBERATELY NOT `Template`: `meta.Template`
// (internal/meta/templates.go:63) already exists and means SOMETHING ELSE
// — a CATALOG item read from the Graph API. Two meanings for the same name
// is the trap docs/ARMADILHAS.md records under "Go / JSON" (the `botoes`
// case): the same name for two different things is worse than two names
// for the same thing, because no one notices. The JSON key is "template"
// (Event.Template) because on the consumer's side there's no catalog to
// confuse it with — the conflict is ours, the package's, and it dies here.
//
// WHY THIS MATTERS, AND IT ISN'T COMPLETENESS: `message_template_category`
// comes IN THE EVENT. The UTILITY -> MARKETING reclassification (which
// changes price and sending rules) shows up here BEFORE any message is
// sent — an earlier signal than the status's `pricing` (Billing, above),
// which only arrives AFTER a delivery. The two complement each other: this
// one warns that it changed, that one confirms what was billed.
//
// THE VALUES ARE NOT TRANSLATED — "APPROVED", "REJECTED", "UTILITY",
// "MARKETING" come out as Meta sent them, for the same reason as
// Billing.Category and Event.Status: a translation table of our own
// rots the day Meta adds a new value, and the unknown value would come out
// as "" or as a guess. What dies in parse.go is the FORMAT (field names,
// nesting, timestamp), not the third party's value vocabulary.
type TemplateStatus struct {
	// Name and Language are what IDENTIFIES a template on SEND
	// (Request.Template + Request.Language, internal/outbound/message.go) —
	// that's why they're the pair that lets the consumer connect this
	// warning to what they send. Meta's `message_template_id` is NOT
	// modeled: it identifies the template in the dashboard and in the
	// Graph API, not in sending, and no one asked for it — it only
	// appears inside Event.ID, where it's needed for the dedup key.
	// Same rule as Billing: the envelope only grows, so adding later is
	// free and removing later is a breaking change.
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`

	// Category is `message_template_category` — UTILITY, MARKETING,
	// AUTHENTICATION. It's the field that gives this task the priority
	// it has (see the type's comment, above).
	Category string `json:"category,omitempty"`

	// State is Meta's `event`: APPROVED, REJECTED, PENDING, PAUSED,
	// DISABLED... NOT called "status" so it doesn't collide, in the mind
	// of whoever reads the envelope, with Event.Status
	// (sent/delivered/read/failed), which talks about a MESSAGE, not a
	// template.
	State string `json:"state"`

	// Reason is Meta's `reason`, PASSED THROUGH AS IT CAME — including
	// the literal string "NONE", which is the NORMAL value when there's
	// no reason (observed in the 21 samples consumer-a captured,
	// 2026-07-26). We do NOT translate "NONE" to empty: "Meta said NONE"
	// and "Meta didn't send the field" are different facts, and the
	// second can show up on an event type we haven't seen yet. With
	// omitempty, only the field's real ABSENCE disappears from the JSON.
	Reason string `json:"reason,omitempty"`
}

// TemplateCategory: Meta RECLASSIFIED a template's category — the
// content of the `template_category_update` webhook translated into this
// project's vocabulary (T-057, 2026-07-28). Only filled when Event.Type ==
// EventTypeTemplateCategory.
//
// WHY IT EXISTS ALONGSIDE TemplateStatus, AND NOT INSIDE IT. T-043
// delivered the reclassification warning by reading
// `message_template_category` from `message_template_status_update` —
// which is the APPROVAL/REJECTION event, with the category as an attribute
// of the new state. It works when the category change comes together with
// a re-approval, and it does NOT work when Meta reclassifies an already
// approved template without touching its state: in that case
// `status_update` doesn't arrive, and the whole protection stays silent
// until the invoice. This is the event DEDICATED to the change, and it
// gives two things the other doesn't:
//
//   - the DIRECTION (PreviousCategory -> NewCategory). `status_update`
//     says "today it's MARKETING" and doesn't say where it came from; a
//     price increase and a price decrease become indistinguishable
//     without state kept on the consumer's side;
//   - the APPEAL WINDOW (AppealStatus). A reclassification is
//     CONTESTABLE, and without receiving this event no one knows there's
//     a deadline to contest it.
//
// The two events coexist on purpose: a template can be reclassified AND
// re-approved, and in that case both arrive. Deduplicating one against the
// other would erase a fact.
//
// THE VALUES ARE NOT TRANSLATED, same rule as TemplateStatus:
// "MARKETING", "UTILITY", and "ELIGIBLE" come out as Meta sent them. In
// particular, AppealStatus does NOT become a boolean — "can it be
// appealed?" looks like a yes/no question, and Meta answers with a
// vocabulary no one here has enumerated; a boolean derived from it would
// force the gateway to decide, today, what to do with a value that only
// shows up tomorrow.
type TemplateCategory struct {
	// Name and Language are the pair that identifies the template on SEND
	// (Request.Template + Request.Language), same reason as TemplateStatus.
	// Meta's `message_template_id` still stays out of the envelope and
	// inside Event.ID, where it's needed for the dedup key.
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`

	// PreviousCategory and NewCategory are `previous_category` and
	// `new_category`. NewCategory has NO omitempty: it IS the event's
	// fact, and an event without it doesn't even get to exist (see
	// templateCategoryEvent, in parse.go) — letting the key
	// disappear from the JSON would force the consumer to distinguish
	// "didn't come" from "came empty" for a case the parser already
	// prevents.
	PreviousCategory string `json:"previous_category,omitempty"`
	NewCategory      string `json:"new_category"`

	// CorrectCategory is `correct_category`: which category Meta
	// considers correct for this template. In the dashboard's example it
	// comes EQUAL to PreviousCategory and DIFFERENT from the new one,
	// which only makes sense alongside AppealStatus — it's what
	// grounds the appeal. Passed through as it came.
	CorrectCategory string `json:"correct_category,omitempty"`

	// AppealStatus is `category_appeal_status` — "ELIGIBLE" in the
	// dashboard's example. It's the field with money inside: it says the
	// reclassification can be contested, and without it the consumer
	// finds out about the change without finding out it can be reversed.
	AppealStatus string `json:"status_do_recurso,omitempty"`
}

// NumberQuality: the daily QUOTA and the number's quality — the content
// of the `phone_number_quality_update` webhook translated into this
// project's vocabulary (T-058, 2026-07-28). Only filled when Event.Type ==
// EventTypeNumberQuality.
//
// WHY IT MATTERS, AND IT ISN'T TELEMETRY: this is the only channel through
// which a tier DOWNGRADE arrives before it hurts. Without it, the first
// news that the quota dropped is sending starting to fail on the limit — a
// symptom that points to the wrong place (everyone will look at the
// gateway, the token, and the network) and that only shows up after
// messages have already been rejected.
//
// THE LIMITS ARE TEXT, NEVER A NUMBER, and that's a decision, not laziness.
// "TIER_250" doesn't become 250. Converting requires a translation table of
// our own, and it breaks the day Meta invents a new tier — breaking in the
// worst way: returning a plausible number for a value no one verified.
// Passing through the literal is the only answer that stays true as their
// vocabulary grows. Same rule as TemplateStatus and Billing.Category.
type NumberQuality struct {
	// DisplayNumber is `display_phone_number` — the BUSINESS's number, in
	// the form Meta displays it. It's what says WHICH number the warning
	// is about, and that's why it enters the event's key. It does NOT go
	// through Canonicalize: the gateway isn't addressing anyone here, it's
	// passing through a label.
	DisplayNumber string `json:"display_number,omitempty"`

	// State is Meta's `event`: ONBOARDING, FLAGGED, UNFLAGGED... the
	// SAME name (and same reason) as TemplateStatus.State — not
	// called "status" so it doesn't collide, in the mind of whoever
	// reads the envelope, with Event.Status (sent/delivered/read/
	// failed), which talks about a MESSAGE.
	State string `json:"state,omitempty"`

	// CurrentLimit and PreviousLimit come from `current_limit` and
	// `old_limit`. These are the two that give the DIRECTION — it's the
	// direction that separates "the account matured" from "the account
	// was downgraded", and `current_limit` alone doesn't give it.
	CurrentLimit  string `json:"current_limit,omitempty"`
	PreviousLimit string `json:"previous_limit,omitempty"`

	// MaxDailyLimit is `max_daily_conversations_per_business`. In
	// the dashboard sample it comes EQUAL to CurrentLimit, and that's why
	// the corpus has a synthetic fixture with all three different:
	// without it, swapping the reading of one for the other would pass
	// green (see testdata/corpus/README.md).
	MaxDailyLimit string `json:"max_daily_limit,omitempty"`
}

// AccountAlert: Meta warning about an account problem, WITH SEVERITY —
// the content of the `account_alerts` webhook translated into this
// project's vocabulary (T-058, 2026-07-28). Only filled when Event.Type ==
// EventTypeAccountAlert.
//
// THE FIELD THAT JUSTIFIES THE TYPE IS Severity. The dashboard sample
// brings "INFORMATIONAL", and the fact that a level called "informational"
// exists implies there are levels ABOVE it — those are the ones that
// decide something. Without modeling it, a serious alert and a routine
// notice arrive identical: a raw line no one reads.
//
// EVERYTHING IS PASSED THROUGH AS IT CAME. There's no notion here of
// "serious" or "mild" derived from the severity, and the absence is
// deliberate: ordering a third party's vocabulary requires knowing the
// whole list, and no one here knows it. Who decides what's serious is the
// consumer, who has the business context — the gateway delivers the label
// intact so that decision is possible.
type AccountAlert struct {
	// EntityType and EntityID come from `entity_type` and
	// `entity_id`: what the alert is ABOUT. EntityID is TEXT because
	// Meta sends it as a NUMBER in the sample — the same tolerance (and
	// the same risk of not fitting in an int32) as message_template_id.
	EntityType string `json:"entity_kind,omitempty"`
	EntityID   string `json:"id_da_entidade,omitempty"`

	// Type is `alert_type` (e.g. "OBA_APPROVED") — what happened.
	// Severity is `alert_severity`; State is `alert_status`.
	Type     string `json:"tipo,omitempty"`
	Severity string `json:"severidade,omitempty"`
	State    string `json:"state,omitempty"`

	// Description is `alert_description`: Meta's free text, in English.
	// It's the only field in this event that is NOT a closed vocabulary,
	// and that's why it's the only one no consumer should write a rule
	// against — matching a third party's free-text string is the
	// fastest way to build an alarm that dies the day Meta rewrites the
	// sentence.
	Description string `json:"descricao,omitempty"`
}

// Event is an already-typed occurrence, delivered to the consumer INSIDE
// the envelope, alongside the raw body. It never replaces the raw body: it
// is enrichment.
type Event struct {
	Type EventType `json:"kind"`

	// ID is DETERMINISTIC, derived from the event's own content (type
	// prefix + identifier Meta assigned, e.g. "msg:"+wa_message_id) —
	// never random. It's what makes a legitimate Meta redelivery and a
	// malicious resend fall into the same consumer dedup.
	ID string `json:"id"`

	// Routing keys. PhoneNumberID for message/status; WabaID for
	// template status and account webhooks, which do NOT support
	// override on Meta and therefore always arrive at the main endpoint.
	PhoneNumberID string `json:"phone_number_id,omitempty"`
	WabaID        string `json:"waba_id,omitempty"`

	Timestamp int64 `json:"timestamp,omitempty"`

	// --- message ---
	WaMessageID string `json:"wa_message_id,omitempty"`
	SubType     string `json:"sub_kind,omitempty"` // text, button, interactive, audio, image...
	// FromRaw is the EXACT value Meta sent; FromCanonical has gone through
	// Canonicalize. Both exist because Meta doesn't guarantee the same
	// spelling you registered.
	//
	// ON AN INSTAGRAM EVENT (T-097, instagram.go): the counterpart is an
	// IGSID, not a phone number, and an IGSID does NOT go through
	// Canonicalize — inserting a "9th digit" into an id that happens to
	// have the SHAPE of a 12-digit Brazilian phone number would corrupt
	// the address. FromCanonical == FromRaw always on an Instagram event; the
	// two fields coexist only so the consumer doesn't need to know,
	// field by field, which product generated the event.
	FromRaw       string `json:"from_raw,omitempty"`
	FromCanonical string `json:"from_canonical,omitempty"`
	ContactName   string `json:"contact_name,omitempty"`
	Text          string `json:"text,omitempty"`

	// ReplyTo is the QUOTED message's wamid, when this message is a
	// reply (the user replied by holding the bubble) — comes from
	// messages[].context.id on Meta's side. THE SAME NAME as the
	// equivalent field on SEND (Request.ReplyTo,
	// internal/outbound/message.go:158): sending and receiving with
	// different names for the same thing would be the start of two
	// vocabularies (reason written in T-024, and it holds here too). The
	// referent is identical in both directions — the quoted message's
	// wamid; only the verb mood changes (replying vs. having been
	// replied to).
	//
	// context.from (the BUSINESS's number, not the customer's) does NOT
	// enter — a boundary decision from T-032 (requested by consumer-a,
	// 2026-07-26, real capture in hand): a field that looks like "from
	// whom" and is "to whom" is an invitation to a bug, and no one asked
	// for it. `referred_product` (the third field "context" can carry)
	// stays out for the original reason: no one asked, and the envelope
	// only grows — adding later is free, removing later is a breaking
	// change.
	//
	// `forwarded` and `frequently_forwarded` STAYED OUT until 2026-07-28
	// for the same reason, and the reason fell: someone asked (the
	// owner, T-059). See Forwarded / FrequentlyForwarded, right
	// below.
	//
	// A THIRD path to absence, since T-061, and it's OURS, not Meta's: a
	// "context" block the parser can't read (unexpected type) is
	// discarded WHOLE, and the message is delivered without this field —
	// before, the WHOLE message was lost. See messageBlock (parse.go,
	// called contextoDaMensagem until T-062) and
	// docs/CONTRATO-CONSUMIDOR.md, "Mudanças que quebram".
	ReplyTo string `json:"reply_to,omitempty"`

	// Forwarded and FrequentlyForwarded come from
	// messages[].context.forwarded and .frequently_forwarded — the other
	// two "context" fields this gateway models, alongside ReplyTo
	// (T-059, 2026-07-28, requested by the owner).
	//
	// WHY THEY WERE ADDED, AND THE ARGUMENT IS BEHAVIOR, NOT
	// COMPLETENESS: FrequentlyForwarded is WhatsApp's CHAIN-MESSAGE
	// signal. A consumer that auto-replies — scheduling detection,
	// autoresponder, relay — today treats a chain message as if the
	// customer had written it; it's this field that allows NOT firing a
	// business flow on top of it. Forwarded alone is a weaker signal,
	// but it's real context (a customer passing along a reference photo,
	// a price table, a screenshot of a conversation).
	//
	// A PLAIN BOOL, NOT *bool — THE OPPOSITE DECISION FROM Voice (further
	// below), and it's deliberate, not an oversight. The question that
	// decides is NOT "can Meta omit the field?" (it can, in both cases)
	// — it's "what BEHAVIOR DIFFERENCE does the third state buy?"
	// (docs/ARMADILHAS.md, "Go / JSON", the entry about the pointer that
	// bought nothing):
	//
	//   - on Voice the third state buys a lot: "Meta said this is NOT a
	//     voice note" (false) means resend as a plain attachment, and
	//     "Meta said nothing" (absent) means DON'T decide — deciding
	//     wrong turns a voice note into a file attachment, with no error
	//     anywhere (the two-mimes trap, 2026-07-20). They're THREE
	//     distinct actions for whoever resends;
	//   - here there is no third action. "It wasn't forwarded" and "Meta
	//     didn't say it was" lead the consumer to the SAME place: treat
	//     the message as written by the person. These fields are a
	//     SIGNAL — they only matter when they assert something —, not
	//     an attribute every message has. A pointer here would force
	//     every consumer to write `x != nil && *x` to reach exactly
	//     where `x` already reaches.
	//
	// WHAT THIS COMMENT DELIBERATELY DOES NOT ASSERT: when Meta sends
	// the field. There's no real capture of these fields in the corpus
	// (as of 2026-07-28, `grep -rl forwarded testdata/corpus/` found
	// nothing beyond this task's SYNTHETIC fixture), and Meta's public
	// doc, searched on that date, no longer has a page describing
	// `context`'s fields. The decision above doesn't depend on that: it
	// rests on the CONSUMER's behavior, which is what we know, and it
	// still holds even if Meta sends an explicit `false`.
	//
	// With omitempty, false DISAPPEARS from the envelope — consistent
	// with the reading above (absent and false are the SAME answer) and
	// necessary for the promise that a normal message doesn't gain a new
	// key (see TestParseWebhookDoesNotRegressTheCurrent16Fields).
	Forwarded           bool `json:"forwarded,omitempty"`
	FrequentlyForwarded bool `json:"frequently_forwarded,omitempty"`

	// Button: filled by both type "button" (a reply to a TEMPLATE, outside
	// the 24h window) and interactive.button_reply (INSIDE the window).
	// The consumer sees a single field; the difference is Meta's and it
	// dies here.
	ButtonPayload string `json:"button_payload,omitempty"`
	ButtonText    string `json:"button_text,omitempty"`

	// Reaction is only filled when SubType == "reaction". See the Reaction
	// type for the rule about an absent emoji == removal.
	Reaction *Reaction `json:"reaction,omitempty"`

	MediaID string `json:"media_id,omitempty"`
	// MediaMimePayload comes RAW, with parameter (e.g. "audio/ogg;
	// codecs=opus"). DO NOT normalize: it's the codecs=opus that makes
	// the voice note exist.
	MediaMimePayload string `json:"media_mime_payload,omitempty"`

	// Voice distinguishes a PLAYABLE voice note from a plain audio
	// attachment — the INPUT HALF of the two-mimes trap (2026-07-20,
	// docs/ARMADILHAS.md): without this field, whoever resends the audio
	// (contract obligation 5) has no way to know it needed to declare
	// "codecs=opus" back, and the voice note becomes a file attachment
	// on the consumer's side, with no error anywhere.
	//
	// *bool, NEVER bool: Meta only sends "voice" on audio messages, and
	// not always. An audio message WITHOUT the "voice" field in the
	// payload has to come out with Voice ABSENT (nil, omitted from the
	// JSON) — never "voz: false" by default, which would be
	// indistinguishable from an EXPLICIT "voice: false" from Meta (a
	// plain audio attachment, stated outright).
	//
	// Since T-061 a "voice" of an unexpected TYPE also comes out
	// ABSENT, instead of bringing down the whole message: "I don't know"
	// is the only honest answer, and it's so it can say that that this
	// field is a pointer. See tolerantBool (parse.go).
	//
	// The three states (true/false/absent) are confirmed by TWO
	// independent sides: consumer-a already treated `bool | None` with
	// `is` in production since 2026-07-20
	// (backend/app/whatsapp/inbound.py:236, before any conversation
	// between the two projects), and T-026's real capture (2026-07-26)
	// actually brought "voice": true in the payload
	// (testdata/corpus/audio_nota_de_voz.json).
	Voice *bool `json:"voice,omitempty"`

	// Caption and Filename come from image/video/document.caption and
	// document.filename (Meta only sends filename for document). They
	// only apply to the MediaID accompanying the same event.
	Caption  string `json:"caption,omitempty"`
	Filename string `json:"file_name,omitempty"`

	// --- status ---
	Status      string `json:"status,omitempty"` // sent, delivered, read, failed
	ToRaw       string `json:"to_raw,omitempty"`
	ToCanonical string `json:"to_canonical,omitempty"`
	// Error appears in TWO places in the envelope, with a DIFFERENT
	// MEANING in each — the same field, not two similarly-named fields,
	// because what Meta sends in errors[] has the same format in both:
	//   - in a STATUS event (Type == "status", here on this side):
	//     "delivery failed" (Status == "failed").
	//   - in a MESSAGE event (Type == "message", see SubType above):
	//     "Meta received something the Cloud API doesn't know how to
	//     represent" (SubType == "unsupported", T-033, 2026-07-26) — NOT
	//     a delivery failure, and treating both cases as the same fact
	//     would mislead the consumer about what happened.
	// See StatusError for the absence-vs-zero rule, and what stays out on
	// purpose.
	Error *StatusError `json:"error,omitempty"`

	// Billing only appears when Meta sent "pricing" in the status
	// (T-041, 2026-07-26) — 3 of the 148 status events consumer-a
	// recorded do NOT carry "pricing"; absence is absence, never zero
	// (see Billing, above).
	Billing *Billing `json:"pricing,omitempty"`

	// --- location (SubType == "location") ---
	Location *Location `json:"location,omitempty"`

	// --- template status (Type == EventTypeTemplateStatus) ---
	//
	// Only appears on a template event. A template event has NO
	// PhoneNumberID: the `message_template_status_update` webhook
	// carries no `metadata.phone_number_id` at all (confirmed with data
	// across the 21 captured samples, T-043, beyond the doc reading that
	// founded T-038) — its ONLY routing key is WabaID.
	Template *TemplateStatus `json:"template,omitempty"`

	// --- category reclassification (Type == EventTypeTemplateCategory) ---
	//
	// Same routing rule as Template above: an account webhook carries no
	// metadata.phone_number_id, so WabaID is the only key. A SEPARATE
	// field from Template, and not a Template with more fields, because
	// the two events can arrive for the SAME template on the same day
	// saying different things — merging the vocabularies would make a
	// consumer read "estado" off an event that isn't about state at all.
	TemplateCategory *TemplateCategory `json:"template_category,omitempty"`

	// --- number quota and quality (Type == EventTypeNumberQuality) ---
	//
	// An account webhook, like the two template ones: no PhoneNumberID,
	// routed only by WabaID. The `display_phone_number` it carries is a
	// LABEL (goes into NumberQuality.DisplayNumber), not a routing
	// key — guard 5a compares `metadata.phone_number_id`, which this
	// payload doesn't have.
	NumberQuality *NumberQuality `json:"number_quality,omitempty"`

	// --- account alert (Type == EventTypeAccountAlert) ---
	AccountAlert *AccountAlert `json:"account_alert,omitempty"`
}
