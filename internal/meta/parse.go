// WhatsApp Cloud API webhook parser.
//
// HARD CONTRACT: this function NEVER panics and is NEVER a precondition for
// delivery. The caller delivers the raw body to the consumer even when
// parsing fails — parsing is enrichment. Without this, a bug here would
// discard events from ALL consumers at once, with a 200, and Meta never
// redelivers.
package meta

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ErrBodyNotObject: the body is valid JSON but isn't an object.
//
// TRAP: `null`, `42`, `[]`, `"text"`, and `true` are SYNTACTICALLY VALID
// JSON. In Go, json.Unmarshal of "null" into a map leaves the map nil and
// does NOT return an error — the following code, which assumes data,
// carries on thinking it has some.
var ErrBodyNotObject = errors.New("meta: corpo nao e um objeto JSON")

// ErrPartialParse: the body was read, but part of it couldn't be
// interpreted.
//
// Returned TOGETHER with the events that succeeded — never in their place.
// The caller delivers the events we have, the raw body, and the warning
// that something was left behind. Discarding the whole batch because of a
// malformed item would erase an unrelated account's message: Meta batches
// `entry` from different accounts in the same call.
var ErrPartialParse = errors.New("meta: parte do payload nao pode ser lida")

// THE BOUNDARY STRUCTS (T-068). envelopeMeta, entryMeta, changeMeta,
// valueMeta, statusFromMeta, templateStatusMeta, templateCategoryMeta,
// numberQualityMeta, and accountAlertMeta follow the SAME rule as
// messageMeta: EVERY field is json.RawMessage, no exceptions, and none is
// interpreted at Unmarshal time. A json.Unmarshal whose fields are all
// RawMessage has no way to fail over any JSON object.
//
// WHY THESE, AND NOT JUST THE MESSAGE (T-062 shielded the leaf and stopped
// there). The radius of a failing Unmarshal GROWS as you climb the tree,
// and it was measured with ParseWebhook before this task, one field
// swapped at a time, with a good message + a good sibling in the same
// batch:
//
//	valueMeta.metadata / .contacts        -> len(evs) = 0  (the WHOLE change:
//	                                         that client's messages AND status)
//	changeMeta.field                      -> len(evs) = 0  (the whole change)
//	entryMeta.id (waba_id)                -> the entry disappears; OTHER entries' siblings survive
//	statusFromMeta.id/status/timestamp/...    -> the status disappears; its sibling survives
//	templateStatusMeta.event/reason/...   -> len(evs) = 0  (the template event)
//
// `valueMeta` is the worst of the five and is worse than the defect T-062
// had just fixed: a `"contacts":"x"` Meta sends in a new format erased a
// client's WHOLE message batch, silently, with a 200 answered to Meta —
// docs/ARMADILHAS.md's Critical #1 under another name.
//
// THE LISTS ARE ALSO RawMessage, and not []json.RawMessage: a
// `"changes":"x"` or a `"messages":"x"` would make the SLICE's Unmarshal
// fail, and with it the struct that contains it. It's the same reason
// already written in messageMeta.Errors — per-ITEM isolation doesn't
// protect against the WHOLE LIST having the wrong type. Each one is read
// by messageBlock[[]json.RawMessage].
//
// WHAT'S LEFT FOR THE NEXT PERSON:
// TestBoundaryStructsIsolateEveryFieldByConstruction (parse_test.go) walks
// all of them by reflection and turns RED, naming the struct and field, the
// day someone hangs a concrete type here. THAT TEST'S LIST IS HAND-WRITTEN
// and therefore rots by OMISSION: a new boundary struct no one adds there
// is checked by no one. New struct here, new line there.
type envelopeMeta struct {
	Entry json.RawMessage `json:"entry"`
}

type entryMeta struct {
	// ID is the waba_id. It's a ROUTING KEY, not decoration: guard 5b in
	// internal/inbound/handler.go compares this value against the
	// instance's, and it's the ONLY key an ACCOUNT webhook carries (an
	// account webhook has no metadata.phone_number_id — see
	// templateStatusMeta).
	//
	// That's why an UNREADABLE waba_id doesn't become "let it through": it
	// is read as "" and the guard treats "" as a non-match. See
	// AccountWabaIDsInPayload for the whole decision and why.
	ID json.RawMessage `json:"id"`

	// Time is the BATCH's timestamp (unix seconds), and it's the only time
	// a template status webhook has: its `value` carries no timestamp of
	// its own (T-043, checked against the 21 captured samples). See
	// templateStatusEvent for what's done with it — and why it enters
	// the event's KEY.
	//
	// It was the FIRST field in this struct to become json.RawMessage
	// (T-043), two plans before the rule applied to its siblings `id` and
	// `changes` — this project's classic asymmetry, and the reason this
	// task exists.
	Time json.RawMessage `json:"time"`

	Changes json.RawMessage `json:"changes"`
}

// fieldTemplateStatus is the `field` of the template approval/rejection
// webhook.
//
// ENUMERATING THE NAME HERE IS CORRECT, and it's the OPPOSITE of what
// AccountWabaIDsInPayload does on purpose (see its comment, further
// below). There, an enumerated name would be a GUARD locked to today's
// vocabulary: a new account field the list didn't cite would pass without
// any isolation check. Here the name doesn't guard anything — it says "I
// know how to read THIS format". The other account fields
// (message_template_quality_update, template_category_update,
// account_update, ...) have a `value` with OTHER keys; interpreting them
// with this parser would produce an invented event, which is worse than
// none. An account field other than this one still only arrives in the
// `cru`, as before.
const fieldTemplateStatus = "message_template_status_update"

// fieldTemplateCategory is the `field` of the category RECLASSIFICATION
// webhook (T-057, 2026-07-28). Enumerating the name here is correct for
// the same reason written above: it doesn't guard anything, it says "I
// know how to read THIS format".
//
// IT'S THE NEIGHBOR THAT WAS LEFT OUT, and the asymmetry cost a whole
// protection: T-043 modeled the field right next to it and read the
// category from there, with no one asking which event Meta dedicates to a
// category CHANGE. See TemplateCategory (types.go) for what this event
// gives that the other doesn't.
const fieldTemplateCategory = "template_category_update"

// fieldNumberQuality and fieldAccountAlert are the other two ACCOUNT
// webhooks this gateway knows how to read (T-058, 2026-07-28), under the
// same rule: the name enumerated here doesn't guard anything, it just says
// "I know how to read THIS format".
//
// THERE ARE TWO, NOT THE TEN THE APP RECEIVES. The choice is written in
// docs/META-CAMPOS-DE-WEBHOOK.md and in docs/CONTRATO-CONSUMIDOR.md: a
// field with no interested consumer becomes dead vocabulary in the
// envelope, and the envelope only GROWS — adding later is free, removing
// later is a breaking change. The rest still arrive with `cru` and
// `"eventos": []`, which is correct, documented behavior, never a parse
// failure.
const (
	fieldNumberQuality = "phone_number_quality_update"
	fieldAccountAlert  = "account_alerts"
)

type changeMeta struct {
	Field json.RawMessage `json:"field"`
	Value json.RawMessage `json:"value"`
}

type valueMeta struct {
	Metadata json.RawMessage `json:"metadata"`
	Contacts json.RawMessage `json:"contacts"`
	Messages json.RawMessage `json:"messages"`
	Statuses json.RawMessage `json:"statuses"`
}

// metadataMeta and contactMeta were ANONYMOUS structs inside valueMeta
// until T-068. They got a name for the same reason as textMeta and
// friends (messageBlock is generic and needs a type), and the fields
// INSIDE them are PLAIN under docs/ARMADILHAS.md's DEPTH rule: the OUTER
// field is what isolates.
//
// A `"metadata":{"phone_number_id":42}` costs the whole "metadata" block
// and nothing more — the change's messages arrive, without a
// phone_number_id. And a `"contacts":[{"profile":"x","wa_id":"..."}]`
// costs THAT contact (that message's customer name), never the message or
// its sibling contacts, because each contacts[] item is deserialized on
// its own.
type metadataMeta struct {
	PhoneNumberID string `json:"phone_number_id"`
}

type contactMeta struct {
	WaID    string `json:"wa_id"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}

// messageMeta is a message's ISOLATION BOUNDARY: EVERY field is
// json.RawMessage, no exceptions, and none of them is interpreted here.
//
// WHY ALL OF THEM, AND NOT JUST THE ONES THAT ALREADY CAUSED TROUBLE
// (T-062, 2026-07-28). T-043 shielded entry.time; T-061 shielded context
// and voice. In both, the defense was applied TO THE FIELD someone was
// touching, and the siblings stayed plain. Measured with ParseWebhook
// before this task, an unexpected-type value in ANY field of this struct —
// including `"text":"oi"`, the most common type of all — made the WHOLE
// message's json.Unmarshal fail: it became `ignorados++` and DISAPPEARED
// from the `eventos` list, the list the contract tells the consumer to
// dedup and act on. The `cru` was still delivered with `parse_error`, but
// whoever follows the contract never saw the message. No ALARM and no
// counter.
//
// THIS TASK'S CHOICE WAS NOT "shield the five missing fields" — it was
// moving the BOUNDARY. A json.Unmarshal whose fields are all
// json.RawMessage has no way to fail over any JSON object: there's no
// type RawMessage rejects. From here on, the only thing that erases a
// message from the list is it not having an `id` — and that's a decision,
// not an accident (without an id there's no dedup key; see
// messageEvent).
//
// A NEW FIELD IS BORN PROTECTED, AND THAT'S ENFORCED BY A TEST, not by
// discipline: TestMessageMetaIsolatesEveryFieldByConstruction (parse_test.go)
// walks this struct by reflection and turns RED the day someone hangs a
// non-json.RawMessage field here, naming the field. That was exactly the
// step missing in the three earlier rounds — each fixed the field of the
// moment and left the class open.
//
// Each block is read by messageBlock (end of this file), which
// degrades the BLOCK when the format isn't what we know how to read. The
// cost is observable and is documented in docs/CONTRATO-CONSUMIDOR.md,
// "Mudanças que quebram".
type messageMeta struct {
	// Identity. They're strings on Meta's side, and are read with
	// messageBlock[string] — a STRICT read on purpose: a `"id":42` does
	// NOT become the wamid "42" (inventing a wamid makes the consumer
	// reply to a message that doesn't exist), it becomes "" and the
	// message falls into the id guard. Timestamp is the exception, and
	// that's why it goes through textFromNumberOrString: a number or text
	// give the same instant, same tolerance as entryMeta.Time.
	From      json.RawMessage `json:"from"`
	ID        json.RawMessage `json:"id"`
	Timestamp json.RawMessage `json:"timestamp"`
	Type      json.RawMessage `json:"type"`

	Text        json.RawMessage `json:"text"`
	Button      json.RawMessage `json:"button"`
	Interactive json.RawMessage `json:"interactive"`

	Audio    json.RawMessage `json:"audio"`
	Image    json.RawMessage `json:"image"`
	Video    json.RawMessage `json:"video"`
	Document json.RawMessage `json:"document"`
	Sticker  json.RawMessage `json:"sticker"`

	Reaction json.RawMessage `json:"reaction"`
	Location json.RawMessage `json:"location"`

	// Errors is the RAW list, not []json.RawMessage: an `"errors":"x"`
	// would make the SLICE's Unmarshal fail, and with it the whole
	// message — the per-ITEM isolation T-028 brought (each errors[] item
	// deserialized separately) doesn't protect against the WHOLE LIST
	// having the wrong type. Both levels exist: the list degrades here,
	// the item degrades in messageEvent.
	//
	// Meta sends this when the message's TYPE is "unsupported" (code
	// 131051, "Message type unknown", confirmed at
	// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/,
	// read on 2026-07-26): it received something the Cloud API doesn't
	// know how to represent. Reuses the SAME metaError type statusFromMeta.Errors
	// uses (the item's format is identical); what changes is the meaning,
	// documented in Event.Error (types.go).
	Errors json.RawMessage `json:"errors"`

	// Context is the "context" Meta sends when this message QUOTES
	// another (the user replied by holding the bubble) AND/OR when it was
	// FORWARDED (T-059) — the two cases are independent: a forwarded
	// message that doesn't quote anyone arrives with "context" WITHOUT
	// "id".
	//
	// It was the FIRST field in this struct to become json.RawMessage
	// (T-061), one plan before the rule applied to its siblings. The
	// comment that used to be here became the whole type's doctrine,
	// above.
	Context json.RawMessage `json:"context"`
}

// textMeta, buttonMeta, and interactiveMeta were ANONYMOUS structs inside
// messageMeta until T-062. They got a name because messageBlock is
// generic and needs a type to instantiate — and, as a bonus, because an
// anonymous struct can't be cited in a comment or in a test.
//
// The fields INSIDE them are plain, under docs/ARMADILHAS.md's DEPTH rule
// ("Go / JSON"): the OUTER field is what isolates. A
// `"interactive":{"type":"button_reply","button_reply":42}` costs the
// whole "interactive" block and nothing more.
type textMeta struct {
	Body string `json:"body"`
}

type buttonMeta struct {
	Payload string `json:"payload"`
	Text    string `json:"text"`
}

type interactiveMeta struct {
	Type        string `json:"type"`
	ButtonReply struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"button_reply"`
	ListReply struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"list_reply"`
}

// contextMeta is the RAW format of Meta's "context" — it dies here inside.
// Three of the four fields it can carry are modeled: "id" (T-032, becomes
// Event.ReplyTo) and "forwarded"/"frequently_forwarded" (T-059, become
// Event.Forwarded and Event.FrequentlyForwarded). Left out, on
// purpose: "from" (the BUSINESS's number — see Event.ReplyTo in
// types.go) and "referred_product" (no one asked, and the envelope only
// grows).
type contextMeta struct {
	ID string `json:"id"`

	// A PLAIN bool, and not json.RawMessage — and since T-061 this is the
	// DEPTH rule fulfilled, not an accepted risk: the OUTER field is what
	// isolates (messageMeta.Context, json.RawMessage), so an
	// unexpected-type value in here (e.g. "true" in quotes) has no more
	// neighbor to take down — it costs the "context" block and nothing
	// else. It's the same decision as errorDataMeta and
	// pricingMeta.Billable, for the same question (docs/ARMADILHAS.md, "Go
	// / JSON"): a sibling of fields that already work asks for
	// RawMessage; nested INSIDE something that already isolates stays
	// plain.
	//
	// See Event.Forwarded (types.go) for why they're plain bools and
	// not *bool like Voice.
	Forwarded           bool `json:"forwarded"`
	FrequentlyForwarded bool `json:"frequently_forwarded"`
}

type mediaMeta struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	Caption  string `json:"caption"`
	Filename string `json:"filename"`
	// Voice is json.RawMessage, read by tolerantBool (end of this file) —
	// the *bool with three states (true / false / absent) still exists,
	// just at the DESTINATION, Event.Voice. See Event.Voice's comment for
	// the meaning of each state.
	//
	// WHY NOT *bool HERE (T-061): when this line was written, the media
	// blocks (Audio, Image, Video, Document, Sticker) were SIBLINGS of
	// from/id/type in messageMeta and went through no isolation at all,
	// so a `"voice": "true"` brought down the WHOLE MESSAGE's Unmarshal —
	// the same defect, the same fix, and the same task as Context, above.
	// This field has carried the fragile format since plan 1, and was
	// fixed together on purpose: leaving one of the two for later would
	// be the asymmetry docs/ARMADILHAS.md calls the mother trap.
	//
	// SINCE T-062 THE OUTER BLOCK ALREADY ISOLATES — and under
	// docs/ARMADILHAS.md's DEPTH rule this would be a candidate to go
	// back to being `*bool`. DO NOT revert it, and the reason has
	// changed: with `*bool`, a `"voice":"sim"` would make the WHOLE MEDIA
	// block unreadable, and the audio would arrive without a midia_id and
	// without the mime carrying `codecs=opus` — the message would
	// survive, the attachment wouldn't. RawMessage here doesn't buy
	// isolation against the siblings (the outer field already gives
	// that): it buys the loss stopping AT THIS FIELD. Proven by
	// TestParseWebhookAVoiceOfTheWrongTypeDeliversTheAudioWithVoiceAbsent, which
	// requires MediaID and the mime to stay intact.
	Voice json.RawMessage `json:"voice"`
}

// reactionMeta and locationMeta are Meta's RAW format — they die here
// inside. The consumer never sees "message_id" or "name"/"address": they
// see Target, Name, Address, in our own vocabulary (types.go).
type reactionMeta struct {
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
}

type locationMeta struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
}

// statusFromMeta is messageMeta's DIRECT SIBLING, in the SAME loop — and it
// was left out of the two earlier rounds because no one looked sideways
// (T-068). Every field is json.RawMessage for the same reason, with one
// reading difference worth writing down: Timestamp goes through
// textFromNumberOrString and the others don't.
//
// WHY THE TIMESTAMP IS TOLERANT AND THE REST IS STRICT: a
// `"timestamp":1769000000` (a number, the form Meta already uses in
// entry.time) gives the SAME instant as "1769000000" — there's nothing to
// invent. But `"id":42` CANNOT become the wamid "42": inventing a wamid
// makes the consumer reply to a message that doesn't exist. Same
// decision, same words, as messageMeta.
type statusMeta struct {
	ID          json.RawMessage `json:"id"`
	Status      json.RawMessage `json:"status"`
	Timestamp   json.RawMessage `json:"timestamp"`
	RecipientID json.RawMessage `json:"recipient_id"`
	// Errors is the RAW list, and each item is deserialized separately:
	// the same reason as the rest of the file (per-level isolation) and
	// the same reason messageMeta.Errors is RawMessage and not
	// []json.RawMessage. An error item whose shape doesn't match (e.g.
	// "code" arriving as a string) CANNOT bring down id/status/timestamp
	// of the whole status just because Meta added a new format inside
	// errors[] — the consumer would lose the EVENT, not just the reason;
	// and an `"errors":"x"` cannot bring down the LIST.
	Errors json.RawMessage `json:"errors"`

	// Pricing stays as RawMessage, the SAME reason as Errors, above
	// (per-field isolation): a "pricing" of the WRONG TYPE (e.g. a
	// string instead of an object) cannot bring down id/status/timestamp
	// of the whole status just because Meta added a new field — this
	// file's SAME mother trap (docs/ARMADILHAS.md), now applied to a
	// field that didn't even exist when the rule was written for
	// errors[] (T-028).
	Pricing json.RawMessage `json:"pricing"`
}

// pricingMeta is the RAW format of "pricing" in the status webhook — see
// Billing in types.go for what survives from it and the reason Billable
// is a pointer.
//
// NOT a plain struct like errorDataMeta: there, the nested object
// (error_data) was already ISOLATED by being inside errors[]
// ([]json.RawMessage) — a malformed field inside it only brings down the
// errors[0] item, never the whole status. "pricing" sits directly in
// statusFromMeta, one level up; without RawMessage here, a wrong-typed
// "pricing" would break the WHOLE status's Unmarshal.
type pricingMeta struct {
	Billable *bool  `json:"billable"`
	Category string `json:"category"`
}

// metaError is the RAW format of an errors[] item in the status webhook —
// see StatusError in types.go for what survives from it and the format's
// source.
//
// ErrorData is a PLAIN struct, not a pointer — the same reason
// StatusError.Details isn't *string (see types.go): an absent
// "error_data" and an "error_data" present without "details" both produce
// Details == "", and both answers are the SAME thing for statusEvent
// (Details stays empty in both cases). Distinguishing the two would have
// no consumer. It's confirmed — not assumed — that this is safe:
// `json.Unmarshal` of an ABSENT field, of `error_data: null`, and of
// `error_data: {}` over a plain struct are all three a no-op (the
// `encoding/json` package documents that null over any type that isn't a
// pointer/interface/map/slice has no effect and produces no error); only
// an `error_data` of the WRONG TYPE (e.g. a string) would fail the whole
// item's Unmarshal — and that case is already covered by the existing
// guard in statusEvent (malformed item -> e.Error stays nil, see
// TestParseWebhookInAStatusErrorAMalformedItemDoesNotBringDownTheEvent).
type metaError struct {
	Code      int           `json:"code"`
	Title     string        `json:"title"`
	ErrorData errorDataMeta `json:"error_data"`
}

type errorDataMeta struct {
	Details string `json:"details"`
}

// templateStatusMeta is the RAW format of `value` from
// message_template_status_update — it dies here inside, like reactionMeta
// and pricingMeta. The consumer sees TemplateStatus (types.go), in our
// own vocabulary.
//
// Shape confirmed by a REAL CAPTURE (21 samples consumer-a kept from
// before the migration, delivered on 2026-07-26), not just by the doc:
//
//	{"field":"message_template_status_update",
//	 "value":{"event":"APPROVED","message_template_id":1384121316897444,
//	          "message_template_name":"aguardando_peca_v2",
//	          "message_template_language":"pt_BR",
//	          "reason":"NONE","message_template_category":"UTILITY"}}
//
// Two facts from the capture that were NOT deducible: `reason` comes as
// the STRING "NONE" when there's no reason (not absent, not null — see
// TemplateStatus.Reason), and there's no `metadata` or
// `phone_number_id` anywhere in the payload, which confirms with data the
// gap T-038 closed by reading code.
//
// IT IS READ FROM THE RAW `value`, and not from a typed field in
// changeMeta — which is what the changeDeTemplateMeta struct did until
// T-068, rereading the WHOLE `change` just to grab the `value` in another
// format. That second read's reason still holds and is now free: hanging
// `event`, `reason`, and friends off valueMeta would put TEMPLATE fields
// inside the struct that reads MESSAGES, and a `reason` of a new shape in
// a message payload would bring down the WHOLE `value`. With
// changeMeta.Value raw, the two `value` shapes are read from the SAME
// block, each into its own struct, with no extra Unmarshal.
type templateStatusMeta struct {
	Event json.RawMessage `json:"event"`

	// TemplateID only needs to become TEXT (it enters the event's key,
	// see templateStatusEvent) and the capture shows a 16-digit
	// number: modeling it as an int would risk the day Meta sends it in
	// quotes — the same cheap tolerance as tolerantInt (errors.go),
	// for the same reason (costs three lines; not having it costs the
	// event).
	TemplateID json.RawMessage `json:"message_template_id"`

	Name     json.RawMessage `json:"message_template_name"`
	Language json.RawMessage `json:"message_template_language"`
	Category json.RawMessage `json:"message_template_category"`
	Reason   json.RawMessage `json:"reason"`
}

// templateCategoryMeta is the RAW format of `value` from
// `template_category_update` (T-057) — it dies here inside, like
// templateStatusMeta. The consumer sees TemplateCategory (types.go), in
// our own vocabulary.
//
// IT IS A BOUNDARY STRUCT: every field is json.RawMessage, no exceptions,
// under the T-068 rule written in the other six's comment. Born this way
// instead of gaining the defense in some future round — which is exactly
// what docs/ARMADILHAS.md records as "Go / JSON"'s most expensive entry:
// the same defense applied three times, field by field, before someone
// treats the class.
//
// Shape originally DERIVED FROM THE DOC — the sample Meta's dashboard
// *Test* button shows (2026-07-28), transcribed in
// docs/META-CAMPOS-DE-WEBHOOK.md:
//
//	{"field":"template_category_update",
//	 "value":{"message_template_id":12345678,
//	          "message_template_name":"my_message_template",
//	          "message_template_language":"en-US",
//	          "previous_category":"MARKETING","new_category":"UTILITY",
//	          "correct_category":"MARKETING","category_appeal_status":"ELIGIBLE"}}
//
// REAL CAPTURES EXIST SINCE 2026-08-28 (T-174), ceded by consumer
// `consumer-b`, and they REPLACED the derived fixture (T-051 records why
// living with both is worse):
// testdata/corpus/categoria_de_template_{rebaixamento,restauracao,sem_anterior}.json.
//
// WHAT THE CAPTURES CHANGED ABOUT THIS STRUCT, and it is only knowledge —
// not a single field moved: real traffic brought NEITHER `correct_category`
// NOR `category_appeal_status` in any of the three, and one of the three
// came WITHOUT `previous_category`. All three keep being modelled and keep
// degrading to empty, which is what the boundary-struct rule already
// required; the difference is that the degradation of `previous_category`
// is now an OBSERVED case instead of a project decision written blind.
// See templateCategoryEvent, below, and the three tests named
// TestCategoriaDeTemplate* in corpus_test.go.
type templateCategoryMeta struct {
	// TemplateID becomes TEXT via textFromNumberOrString, the same reason
	// (and the same 16-digit risk) as templateStatusMeta.TemplateID: the
	// dashboard sample sends it as a NUMBER.
	TemplateID json.RawMessage `json:"message_template_id"`

	Name     json.RawMessage `json:"message_template_name"`
	Language json.RawMessage `json:"message_template_language"`

	Previous json.RawMessage `json:"previous_category"`
	New      json.RawMessage `json:"new_category"`
	Correct  json.RawMessage `json:"correct_category"`

	AppealStatus json.RawMessage `json:"category_appeal_status"`
}

// numberQualityMeta and accountAlertMeta are the RAW format of
// `value` from `phone_number_quality_update` and `account_alerts` (T-058,
// 2026-07-28) — they die here inside. The consumer sees NumberQuality
// and AccountAlert (types.go), in our own vocabulary.
//
// THESE ARE BOUNDARY STRUCTS: every field is json.RawMessage, no
// exceptions, under the T-068 rule. Born this way, with the corresponding
// line already in TestBoundaryStructsIsolateEveryFieldByConstruction — it's
// the step missing from the three rounds docs/ARMADILHAS.md records as
// "Go / JSON"'s most expensive entry.
//
// Shape DERIVED FROM THE DOC, not from a capture, like
// templateCategoryMeta's: they're the samples the dashboard's *Test*
// button shows (2026-07-28), frozen in testdata/corpus/ and transcribed in
// docs/META-CAMPOS-DE-WEBHOOK.md.
//
//	{"field":"phone_number_quality_update",
//	 "value":{"display_phone_number":"16505551111","event":"ONBOARDING",
//	          "current_limit":"TIER_250","old_limit":"TIER_NOT_SET",
//	          "max_daily_conversations_per_business":"TIER_250"}}
//
//	{"field":"account_alerts",
//	 "value":{"entity_type":"WABA","entity_id":123456,
//	          "alert_severity":"INFORMATIONAL","alert_status":"NONE",
//	          "alert_type":"OBA_APPROVED","alert_description":"..."}}
type numberQualityMeta struct {
	DisplayNumber json.RawMessage `json:"display_phone_number"`
	Event         json.RawMessage `json:"event"`

	// The three limits are read as TEXT and passed through literally —
	// see NumberQuality (types.go) for why "TIER_250" doesn't become
	// 250.
	CurrentLimit  json.RawMessage `json:"current_limit"`
	PreviousLimit json.RawMessage `json:"old_limit"`
	MaxDailyLimit json.RawMessage `json:"max_daily_conversations_per_business"`
}

type accountAlertMeta struct {
	EntityType json.RawMessage `json:"entity_type"`

	// EntityID becomes TEXT via textFromNumberOrString: the sample
	// sends it as a NUMBER, and it enters the event's key. Same reason
	// (and the same risk of not fitting in an int32) as
	// templateStatusMeta.TemplateID.
	EntityID json.RawMessage `json:"entity_id"`

	Severity    json.RawMessage `json:"alert_severity"`
	State       json.RawMessage `json:"alert_status"`
	Type        json.RawMessage `json:"alert_type"`
	Description json.RawMessage `json:"alert_description"`
}

// ParseWebhook converts Meta's raw body into typed events.
//
// ISOLATION AT EVERY LEVEL. Each `entry`, each `change`, and each item is
// deserialized on its own: a malformed one is counted and skipped, and its
// siblings keep going. A single Unmarshal covering the whole tree would let
// a wrong-typed field erase the whole batch — including valid messages from
// accounts that have nothing to do with the problem.
//
// SINCE T-068 THIS APPLIES AT THE FIELD LEVEL, and not just the item: the
// boundary structs (see their comment, above) are all json.RawMessage, so a
// `"contacts":"x"` degrades the BLOCK instead of bringing down the
// `change`. What still discards something is a written decision, never a
// type accident: a missing `id` (message, status, template) and the
// reaction/location guards.
//
// `ignorados` IS COUNTED WHEN AN EVENT STOPS EXISTING, never when a block
// INSIDE a delivered event is lost — a rule inherited from T-062 and the
// reason an unreadable `metadata` doesn't produce ErrPartialParse while an
// unreadable `field` does (without `field` there's no way to know which
// event we failed to model).
//
// Returns an error when it CANNOT understand the whole body
// (ErrBodyNotObject) or when part of it was left behind (ErrPartialParse,
// alongside the events that succeeded). An error here doesn't authorize
// discarding anything: the caller delivers the raw body regardless.
func ParseWebhook(payload []byte) ([]Event, error) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(payload, &generic); err != nil {
		return nil, err
	}
	if generic == nil {
		// json.Unmarshal("null", &map) does NOT error and leaves the map nil.
		return nil, ErrBodyNotObject
	}

	// NIL HERE IS LEGITIMATE, and it doesn't reach the wire that way: when
	// there's nothing to enrich (an unmodeled account webhook, a body
	// without messages/statuses) this slice comes back nil, and a nil
	// slice would serialize as `null`. Who normalizes to `[]` is the
	// envelope assembly (internal/inbound/deliver.go, T-067) — a single
	// place, because normalizing in every caller means enumerating call
	// sites. Don't "fix" this here thinking the consumer receives `null`.
	var evs []Event
	ignored := 0

	duringWalk := forEachChange(payload, func(ent entryRead, ch changeRead) {
		// Template status (T-043): its own `field`, `value` in another
		// format — and no `messages`/`statuses` inside. Exits before the
		// rest because nothing below applies to it.
		if ch.Field == fieldTemplateStatus {
			ev, ok := templateStatusEvent(ch.Value, ent.WabaID, ent.When)
			if !ok {
				ignored++ // counted, never silently discarded
				return
			}
			evs = append(evs, ev)
			return
		}

		// Category reclassification (T-057): another `field`, another
		// `value` format, and also no `messages`/`statuses` inside.
		if ch.Field == fieldTemplateCategory {
			ev, ok := templateCategoryEvent(ch.Value, ent.WabaID, ent.When)
			if !ok {
				ignored++ // counted, never silently discarded
				return
			}
			evs = append(evs, ev)
			return
		}

		// Number quota/quality and account alert (T-058): the same shape
		// as the two template ones — its own `field`, `value` in another
		// format, no `messages`/`statuses` inside.
		if ch.Field == fieldNumberQuality {
			ev, ok := numberQualityEvent(ch.Value, ent.WabaID, ent.When)
			if !ok {
				ignored++ // counted, never silently discarded
				return
			}
			evs = append(evs, ev)
			return
		}
		if ch.Field == fieldAccountAlert {
			ev, ok := accountAlertEvent(ch.Value, ent.WabaID, ent.When)
			if !ok {
				ignored++ // counted, never silently discarded
				return
			}
			evs = append(evs, ev)
			return
		}

		// A `value` that isn't a JSON object costs the change — that's
		// what it describes — and nothing beyond it: the sibling changes
		// and the other entries keep getting read.
		v, valueState := messageBlock[valueMeta](ch.Value)
		if valueState == blockUnreadable {
			ignored++
			return
		}

		// metadata and contacts do NOT count as ignored when they
		// degrade, and the rule is the same as T-062's: it's counted
		// when an EVENT stops existing, never when a block INSIDE a
		// delivered event is lost. An unreadable "metadata" costs those
		// messages' phone_number_id; an unreadable contact costs that
		// message's customer name. The messages arrive in both cases.
		//
		// CONSEQUENCE ON GUARD 5a, and it's deliberate: without a
		// readable phone_number_id, the events come out with
		// PhoneNumberID == "" and the addressing guard
		// (internal/inbound/handler.go) lets them through, because it
		// only compares when there's something to compare. This is NOT
		// what's done with an unreadable waba_id (see
		// AccountWabaIDsInPayload), and the difference has a reason:
		// "metadata" legitimately DOES NOT EXIST in an account webhook,
		// so "" there is the routine case; `entry.id` comes in every
		// payload Meta sends, and "" there means something went wrong.
		metadata, _ := messageBlock[metadataMeta](v.Metadata)
		contacts, _ := messageBlock[[]json.RawMessage](v.Contacts)
		nameByWaID := map[string]string{}
		for _, rawContact := range contacts {
			c, state := messageBlock[contactMeta](rawContact)
			if state != blockRead {
				continue
			}
			nameByWaID[c.WaID] = c.Profile.Name
		}

		// One `for` per message, no try wrapping the whole loop: an event
		// we don't know how to read CANNOT discard the ones after it.
		//
		// Since T-062 this Unmarshal only fails if `bruta` isn't a JSON
		// object — messageMeta is all json.RawMessage, and RawMessage
		// rejects no type. The guards that discard a message (missing
		// id, targetless reaction, absent location) moved: they live in
		// messageEvent, which is who reads the blocks, so the
		// decision and the read can't diverge.
		messages, messagesState := messageBlock[[]json.RawMessage](v.Messages)
		if messagesState == blockUnreadable {
			ignored++ // the unreadable LIST loses its events — this counts
		}
		for _, raw := range messages {
			var m messageMeta
			if err := json.Unmarshal(raw, &m); err != nil {
				ignored++
				continue
			}
			ev, ok := messageEvent(m, ent.WabaID, metadata.PhoneNumberID, nameByWaID)
			if !ok {
				ignored++ // counted, never silently discarded
				continue
			}
			evs = append(evs, ev)
		}

		// `statuses` is read EVEN WITH `messages` unreadable, and vice
		// versa: they're two sibling fields, and until T-068 both died
		// together because one's slice brought down the whole `value`.
		status, statusState := messageBlock[[]json.RawMessage](v.Statuses)
		if statusState == blockUnreadable {
			ignored++
		}
		for _, raw := range status {
			var s statusMeta
			if err := json.Unmarshal(raw, &s); err != nil {
				ignored++ // counted, never silently discarded — see T5
				continue
			}
			ev, ok := statusEvent(s, ent.WabaID, metadata.PhoneNumberID)
			if !ok {
				ignored++
				continue
			}
			evs = append(evs, ev)
		}
	})
	// In two lines, and not `ignorados += forEachChange(...)`: the closure
	// above increments `ignorados` DURING the call, and Go's spec only
	// guarantees lexical order between FUNCTION CALLS — reading the
	// `ignorados` operand in an `x += f()` has no guaranteed order relative
	// to `f()`. Writing it this way isn't style: it's not depending on
	// unspecified order.
	ignored += duringWalk

	if ignored > 0 {
		return evs, fmt.Errorf("%w: %d item(ns) ignorado(s)", ErrPartialParse, ignored)
	}
	return evs, nil
}

// entryRead is what survives from an `entry` after the degraded read.
type entryRead struct {
	WabaID string // "" when `id` didn't come OR couldn't be read — see AccountWabaIDsInPayload
	When   int64  // entry.time in unix seconds; 0 when it didn't come
}

// changeRead is what survives from a `change`.
type changeRead struct {
	Field string          // "" when `field` didn't come or couldn't be read
	Value json.RawMessage // the RAW `value`: each reader interprets it into its own struct
}

// forEachChange walks entry[] and changes[] of the raw body and calls
// `visita` once per change. Returns how many items were left behind during
// the walk.
//
// ONE WALK FOR THE PAYLOAD'S TWO READERS (ParseWebhook and
// AccountWabaIDsInPayload), and that stopped being a matter of taste in
// T-068. While the upper levels were concrete-typed structs, the two
// functions could repeat three lines of `json.Unmarshal` with no risk. With
// DEGRADED reading at every level, two copies would diverge — and the
// divergence here would be exactly between what the parser DELIVERS and
// what the isolation guard CHECKS, which is docs/ARMADILHAS.md's mother
// trap in its most expensive shape.
func forEachChange(payload []byte, visit func(ent entryRead, ch changeRead)) int {
	var env envelopeMeta
	if err := json.Unmarshal(payload, &env); err != nil {
		// UNREACHABLE today: envelopeMeta is all json.RawMessage, and the
		// caller has already guaranteed the body is a JSON object.
		// Returns 1 and not 0 because the alternative is a path that
		// swallows the WHOLE payload without counting — found by the
		// mutation that turned `Entry` back into []json.RawMessage: with
		// it, `{"entry":"x"}` started coming out as `nil, nil`, i.e.
		// "no events, all fine", when before this task it came out with
		// an error.
		return 1
	}

	ignored := 0
	entries, entriesState := messageBlock[[]json.RawMessage](env.Entry)
	if entriesState == blockUnreadable {
		ignored++
	}
	for _, rawEntry := range entries {
		var entry entryMeta
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			ignored++ // an entry[] item that isn't a JSON object
			continue
		}
		// STRICT read of the waba_id, same as messageMeta.ID: `"id":42`
		// doesn't become the waba_id "42". Absent and unreadable both
		// give "" — and here, unlike what holds inside a message, the two
		// are the SAME answer on purpose: "there's no way to prove whose
		// entry this is".
		waba, _ := messageBlock[string](entry.ID)
		ent := entryRead{WabaID: waba, When: toUnix(textFromNumberOrString(entry.Time))}

		changes, changesState := messageBlock[[]json.RawMessage](entry.Changes)
		if changesState == blockUnreadable {
			ignored++
		}
		for _, rawChange := range changes {
			var ch changeMeta
			if err := json.Unmarshal(rawChange, &ch); err != nil {
				ignored++
				continue
			}
			field, fieldState := messageBlock[string](ch.Field)
			if fieldState == blockUnreadable {
				// COUNTS, and the reason is that this is the field by
				// which the change is CLASSIFIED: without it there's no
				// way to know whether that `value` was a template
				// webhook we failed to model. The change still gets
				// read as if it were "messages" (best effort: if there
				// are messages in there, they arrive), but the
				// envelope's `parse_error` has to say something
				// couldn't be classified — it's the only channel we
				// have to not lose this silently.
				ignored++
			}
			visit(ent, changeRead{Field: field, Value: ch.Value})
		}
	}
	return ignored
}

// AccountWabaIDsInPayload returns the waba_id (entry[].id) of every
// ACCOUNT CHANGE in the payload — any `change` whose `field` is NOT
// "messages".
//
// WHY "field != messages" AND NOT A LIST OF NAMES: Meta documents the
// account fields that don't accept a URL override as a closed list
// (message_template_status_update, message_template_quality_update,
// message_template_components_update, template_category_update,
// account_update, account_review_update, account_alerts — confirmed at
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/override/,
// read on 2026-07-26: "Template webhooks (...) and account-level webhooks
// (...) do not support callback overrides" and are always delivered to the
// "app's default callback URL"). Enumerating those names here would lock
// them to today — Meta adds a new field without notice
// (docs/ARMADILHAS.md), and a future account field the list does NOT cite
// would pass through this guard with no check at all. "messages" is the
// ONLY field this gateway knows how to route by phone_number_id
// (messageEvent/statusEvent, above); anything else falls into the
// waba_id fallback, which is the ONLY key every account change carries.
//
// entry[].id IS THE WABA ID: confirmed on two official Meta pages, read on
// 2026-07-26 — the parameter table at
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
// describes `<WHATSAPP_BUSINESS_ACCOUNT_ID>` (entry.id) as "WhatsApp
// Business Account ID.", and the example at
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/message_template_status_update/
// shows the SAME `"id"` field at the `entry` level, outside
// `changes`/`value`.
//
// It exists SEPARATE from ParseWebhook, not as one more return value from
// it, because an account change produces NO Event at all — the gateway
// still doesn't model its content (T-038 is about ROUTING, not about
// enriching the account payload) — and without this function the handler's
// guard (internal/inbound/handler.go, step 5) would sweep zero events and
// never see these changes go by: that was exactly the gap that let another
// WABA's raw body be delivered to the path's slug's consumer.
//
// PER-ITEM ISOLATION, the same reason as the rest of this file: a
// malformed `entry` or `change` is ignored here — already counted by
// ParseWebhook, which runs over the SAME raw body —, never bringing down
// its neighbors. The walk is the SAME as ParseWebhook's (forEachChange),
// and not a copy: see its comment.
//
// AN UNREADABLE WABA_ID RETURNS "" AND THAT IS T-068's DECISION, not a
// side effect. Both exits were open: discard the `entry` with its own
// counter, or make the guard treat unreadable as a non-match. **The first
// is a defense that only looks like one**, and the reason is written in
// step 5b itself: the guard rejects the WHOLE BATCH because the RAW body
// travels along with delivery — discarding only the entry would remove
// its EVENTS and still deliver that account's content, inside the `cru`,
// to the path's slug's consumer.
//
// So "" enters the list and the guard treats it as a non-match, rejecting
// it. What's lost and what's gained, stated in full:
//
//   - an ACCOUNT webhook whose `entry.id` Meta sent in a new shape is
//     lost. It comes out with an ALARM in the journal and a counter
//     (conta_descartada), so it's not a silent loss — it's an ANNOUNCED
//     loss, with someone notified;
//   - recording data from an account we don't know is ours into a third
//     party's database is avoided. Wrong routing gets fixed by
//     repointing; data recorded in another consumer's database doesn't
//     undo itself.
//
// ABSENT AND UNREADABLE ARE THE SAME ANSWER HERE, on purpose, and it's the
// same exception tolerantBool documents: T-062's distinction exists to
// decide whether we DISCARD consumer content, and this isn't that kind of
// question. The question here is "can we PROVE this webhook belongs to
// this instance?", and both an absent `id` and an unreadable one answer
// no.
func AccountWabaIDsInPayload(payload []byte) []string {
	var ids []string
	forEachChange(payload, func(ent entryRead, ch changeRead) {
		// An unreadable `field` falls RIGHT HERE ("" != "messages") and is
		// treated as an account change — the conservative side on
		// purpose: a change we don't know how to classify has to go
		// through the waba_id check, never escape it.
		if ch.Field != "messages" {
			ids = append(ids, ent.WabaID)
		}
	})
	return ids
}

// messageEvent builds the message event from the RAW blocks, reading
// each one on its own (T-062). Returns ok == false when the message
// cannot become an event — the caller counts it as ignored, never
// silently discards it.
//
// THE THREE REJECTIONS ARE DELIBERATE AND LIVE HERE, and not in the loop,
// because all three depend on blocks only this function reads.
// Enumerating the same condition in two places is the defect shape
// docs/ARMADILHAS.md calls the mother trap.
//
// "UNREADABLE BLOCK" IS NOT "ABSENT BLOCK", and the difference decides
// whether the customer's message arrives (T-062). Both produce the same
// envelope — the block doesn't appear —, but they say different things
// about WHO failed:
//
//   - ABSENT (or `null`) is Meta asserting there's no such block. A
//     `type:"reaction"` with no target, or a `type:"location"` with no
//     object, is a payload that doesn't add up: it's still
//     `ignorados++`, like before this task;
//   - UNREADABLE is OUR parser not knowing how to read what came in — and
//     it can very well be a NEW format, not a broken payload (Meta adds
//     and changes fields without notice; see docs/ARMADILHAS.md). Erasing
//     the message because we don't understand one of its blocks is
//     charging the consumer for our own lag, and the outcome is this
//     project's most expensive: permanent loss, with a 200 answered to
//     Meta, no alarm, and no counter.
func messageEvent(m messageMeta, wabaID, phoneNumberID string, nameByWaID map[string]string) (Event, bool) {
	// STRICT read: `"id":42` doesn't become "42" — see messageMeta.
	id, _ := messageBlock[string](m.ID)
	// `null`, `{}`, and a wrong-typed id all give "" here. Without this
	// guard the event would come out with ID == "msg:", colliding in the
	// consumer's dedup with ANY other message with no id in the same
	// batch. Delivering an event that can't be deduplicated is passive,
	// not active: the raw body still reaches the consumer regardless.
	if id == "" {
		return Event{}, false
	}
	from, _ := messageBlock[string](m.From)
	kind, _ := messageBlock[string](m.Type)

	// A reaction WITHOUT a target (message_id) is malformed: Meta always
	// sends this field on a real reaction (removed or not). WITHOUT an
	// emoji, on the other hand, it's legitimate — see reactionMeta and
	// Reaction in types.go.
	reaction, reactionState := messageBlock[reactionMeta](m.Reaction)
	if kind == "reaction" && reactionState != blockUnreadable && reaction.MessageID == "" {
		return Event{}, false
	}
	// A location without the "location" object is malformed: unlike the
	// reaction, Meta has no legitimate case of sending type "location"
	// without the object.
	loc, locState := messageBlock[locationMeta](m.Location)
	if kind == "location" && locState == blockAbsent {
		return Event{}, false
	}

	// ctx can come out zeroed for three reasons the consumer sees the
	// same way: "context" didn't come, a "context" came without these
	// fields, or a "context" came that couldn't be read (T-061).
	ctx, _ := messageBlock[contextMeta](m.Context)
	text, _ := messageBlock[textMeta](m.Text)

	e := Event{
		Type:          EventTypeMessage,
		ID:            "msg:" + id,
		PhoneNumberID: phoneNumberID,
		WabaID:        wabaID,
		Timestamp:     toUnix(textFromNumberOrString(m.Timestamp)),
		WaMessageID:   id,
		SubType:       kind,
		FromRaw:       from,
		FromCanonical: Canonicalize(from),
		ContactName:   nameByWaID[from],
		Text:          text.Body,
		ReplyTo:       ctx.ID, // "" when there's no context or context has no id — omitempty handles the rest
		// T-059: false when there's no context, when there's context
		// without these fields, or when Meta sent false — the three
		// cases are the SAME answer for the consumer, and omitempty
		// erases all three from the envelope. See Event.Forwarded, in
		// types.go.
		Forwarded:           ctx.Forwarded,
		FrequentlyForwarded: ctx.FrequentlyForwarded,
	}

	switch kind {
	case "button":
		// A reply to a TEMPLATE button — the only path possible OUTSIDE
		// the 24h window. Does NOT arrive as interactive.button_reply.
		button, _ := messageBlock[buttonMeta](m.Button)
		e.ButtonPayload = button.Payload
		e.ButtonText = button.Text
		if e.Text == "" {
			e.Text = button.Text
		}
	case "interactive":
		inter, _ := messageBlock[interactiveMeta](m.Interactive)
		switch inter.Type {
		case "button_reply":
			e.ButtonPayload = inter.ButtonReply.ID
			e.ButtonText = inter.ButtonReply.Title
		case "list_reply":
			e.ButtonPayload = inter.ListReply.ID
			e.ButtonText = inter.ListReply.Title
		}
		if e.Text == "" {
			e.Text = e.ButtonText
		}
	case "reaction":
		// Only when the block was READ: unreadable reaches here on
		// purpose (see the guard, above) and comes out without `reacao`
		// — never with an invented Reaction from bytes we don't
		// understand.
		if reactionState == blockRead {
			e.Reaction = &Reaction{
				Emoji:  reaction.Emoji, // "" == removed, see Reaction in types.go
				Target: reaction.MessageID,
			}
		}
	case "location":
		// Same criterion as the reaction. Lat/lon 0 is a VALID coordinate
		// (see Location in types.go), so "block read" is the only
		// proof the zeros came from Meta and not from our zeroed struct.
		if locState == blockRead {
			e.Location = &Location{
				Latitude:  loc.Latitude,
				Longitude: loc.Longitude,
				Name:      loc.Name,
				Address:   loc.Address,
			}
		}
	}

	for _, raw := range []json.RawMessage{m.Audio, m.Image, m.Video, m.Document, m.Sticker} {
		mid, state := messageBlock[mediaMeta](raw)
		if state != blockRead || mid.ID == "" {
			continue
		}
		e.MediaID = mid.ID
		e.MediaMimePayload = mid.MimeType // RAW, with parameter. See types.go.
		e.Caption = mid.Caption
		e.Filename = mid.Filename
		e.Voice = tolerantBool(mid.Voice) // nil when "voice" didn't come, came null, or came unreadable
		break
	}

	// errors[] appears when Meta doesn't know how to represent what it
	// received (the observed case is SubType "unsupported") — see
	// messageMeta.Errors's comment, above, and Event.Error's, in
	// types.go, for the meaning difference from the status event's
	// "error". THE SAME isolation guard as statusEvent: a malformed
	// errors[0] only loses the reason, never the whole event (id,
	// sub_tipo, etc. stay intact). Since T-062 the LIST also degrades (an
	// `"errors":"x"` used to cost the whole message) — see
	// messageMeta.Errors.
	errs, _ := messageBlock[[]json.RawMessage](m.Errors)
	if len(errs) > 0 {
		var er metaError
		if json.Unmarshal(errs[0], &er) == nil {
			e.Error = &StatusError{Code: er.Code, Message: er.Title, Details: er.ErrorData.Details}
		}
	}

	return e, true
}

// statusEvent builds the status event from the RAW fields, reading
// each one on its own (T-068). Returns ok == false when the status
// cannot become an event — the caller counts it as ignored, never
// silently discards it.
//
// THE KEY IS COMPOSITE — status:{wa_message_id}:{status} — because sent,
// delivered, and read arrive with the SAME wa_message_id. A simple key
// would discard two of the three in the consumer's dedup.
//
// THE `id` GUARD LIVES HERE, and not in ParseWebhook's loop, for the same
// reason that moved messageEvent's guards in T-062: whoever decides
// has to be whoever READS, or the decision and the read diverge. Without
// the wamid the key would become "status::sent", colliding with any other
// status with no id in the same batch.
//
// The guard against REGRESSION (read arriving before delivered) is the
// consumer's job, not this one's: the gateway keeps no state and doesn't
// know what already went by. That's why Timestamp goes along — it's what
// the consumer uses to order them.
func statusEvent(s statusMeta, wabaID, phoneNumberID string) (Event, bool) {
	// STRICT read (`"id":42` doesn't become the wamid "42") — see
	// statusFromMeta.
	id, _ := messageBlock[string](s.ID)
	if id == "" {
		return Event{}, false
	}
	// Unreadable `status` and `recipient_id` degrade to "", which is
	// exactly what an ABSENT field already produced before this task: the
	// event comes out with the field empty instead of disappearing.
	// Losing the recipient is cheaper than losing the event; and the
	// consumer sees the difference, because the envelope's `parse_error`
	// flags nothing here — the event arrived whole from the dedup point
	// of view, which is the promise the key carries.
	state, _ := messageBlock[string](s.Status)
	recipient, _ := messageBlock[string](s.RecipientID)

	e := Event{
		Type:          EventTypeStatus,
		ID:            "status:" + id + ":" + state,
		PhoneNumberID: phoneNumberID,
		WabaID:        wabaID,
		// Tolerant of number or text, unlike its siblings — see statusFromMeta.
		Timestamp:   toUnix(textFromNumberOrString(s.Timestamp)),
		WaMessageID: id,
		Status:      state,
		ToRaw:       recipient,
		ToCanonical: Canonicalize(recipient),
	}

	// errors[] is a LIST — Meta can send more than one item. The gateway
	// keeps only the FIRST (documented in CONTRATO-CONSUMIDOR.md): it's
	// what's enough to warn the operator, and there is, as of today, no
	// observed case of conflicting items that would justify exposing a
	// whole list.
	//
	// If the first item can't be interpreted (an unexpected type in a
	// field), e.Error stays nil and the REST of the event (id, status,
	// timestamp, recipient) stays intact — losing the reason is better
	// than losing the whole event over a new format inside errors[].
	errs, _ := messageBlock[[]json.RawMessage](s.Errors)
	if len(errs) > 0 {
		var er metaError
		if json.Unmarshal(errs[0], &er) == nil {
			// error_data (T-029) is NESTED and might not exist in the
			// item — its absence cannot bring down Code/Message,
			// filled in the SAME assignment. See metaError.ErrorData's
			// comment for why a nil-check isn't needed here.
			e.Error = &StatusError{Code: er.Code, Message: er.Title, Details: er.ErrorData.Details}
		}
	}

	// pricing (T-041): the same isolation as errors[], above. If the
	// item can't be interpreted (wrong type), e.Billing stays nil and
	// the REST of the event stays intact — losing the category is better
	// than losing the whole event over a new field.
	//
	// "pricing": null and "pricing": {} also CANNOT become an empty
	// Billing: json.Unmarshal of null/{} into a PLAIN struct
	// (pricingMeta, no pointer) is a no-op (same rule as errorDataMeta,
	// above), so p stays zeroed (Category == "" && Billable == nil) —
	// it's this same zeroed state we use to decide there's NO billing to
	// report, treating the three cases (absent, null, {}) identically:
	// real absence.
	if len(s.Pricing) > 0 {
		var p pricingMeta
		if json.Unmarshal(s.Pricing, &p) == nil && (p.Category != "" || p.Billable != nil) {
			e.Billing = &Billing{Category: p.Category, Billable: p.Billable}
		}
	}

	return e, true
}

// templateStatusEvent builds the template approval/rejection event
// from the RAW `value` (T-043; started receiving `value` instead of the
// whole `change` in T-068, when changeMeta.Value became json.RawMessage).
// Returns ok == false when the value cannot become an event — the caller
// counts it as ignored, never silently discards it.
//
// THE KEY INCLUDES THE TIME, AND THE OBVIOUS ANSWER IS WRONG. For a
// MESSAGE status the key is status:{wamid}:{status} and that's enough,
// because sent/delivered/read are DISTINCT states: the same pair never
// repeats. Not here: the SAME template can be APPROVED more than once —
// approved, edited, back to pending, approved again — and a key
// template_status:{id}:{event} would make the SECOND approval get
// deduplicated by the consumer and DISAPPEAR. It's the exact defect the
// composite key exists to prevent, just turned inside out.
//
// The time comes from `entry.time` (unix seconds) because this webhook's
// `value` has NO timestamp of its own — checked against the 21 captured
// samples. It also goes into Event.Timestamp, which is how the consumer
// orders them.
//
// WHEN `entry.time` DOESN'T COME (0), the event still goes out, with ":0"
// in the key. It's not an oversight: without the time we lose the ability
// to distinguish two approvals of the SAME template — but discarding the
// event would lose both, and losing an event is this project's most
// expensive outcome (Meta doesn't redeliver after a 200). DIFFERENT
// template ids stay distinct in that case.
//
// PhoneNumberID stays empty on purpose: this webhook carries no
// metadata.phone_number_id (see templateStatusMeta, above). WabaID is the
// only routing key — it's what the handler already uses to decide whether
// the webhook belongs to this instance (internal/inbound/handler.go, step
// 5b, T-038).
func templateStatusEvent(rawValue json.RawMessage, wabaID string, when int64) (Event, bool) {
	t, state := messageBlock[templateStatusMeta](rawValue)
	if state != blockRead {
		return Event{}, false
	}

	// The same guard as m.ID == "" on messages, for the same reason:
	// without the template's id and without the `event`, the key would
	// become "template_status:::<time>" and collide with ANY other
	// equally empty change from the same batch. Delivering an event that
	// can't be deduplicated is passive, not active: the raw body still
	// reaches the consumer regardless.
	//
	// The OTHER fields degrade on their own (T-068): a `"reason":42`
	// costs the reason and nothing more — until this task it cost the
	// whole event, and with it the category reclassification warning
	// T-043 exists to give.
	id := textFromNumberOrString(t.TemplateID)
	event, _ := messageBlock[string](t.Event)
	if id == "" || event == "" {
		return Event{}, false
	}
	name, _ := messageBlock[string](t.Name)
	language, _ := messageBlock[string](t.Language)
	category, _ := messageBlock[string](t.Category)
	reason, _ := messageBlock[string](t.Reason)

	return Event{
		Type:      EventTypeTemplateStatus,
		ID:        "template_status:" + id + ":" + event + ":" + strconv.FormatInt(when, 10),
		WabaID:    wabaID,
		Timestamp: when,
		Template: &TemplateStatus{
			Name:     name,
			Language: language,
			Category: category,
			State:    event,
			Reason:   reason, // "NONE" passes through as it came — see TemplateStatus.Reason
		},
	}, true
}

// templateCategoryEvent builds the category RECLASSIFICATION event
// from the RAW `value` (T-057, 2026-07-28). Returns ok == false when the
// value cannot become an event — the caller counts it as ignored, never
// silently discards it.
//
// THE KEY CARRIES THE TRANSITION AND THE TIME, and neither is decoration.
// The lesson is the same one templateStatusEvent documents, made
// worse: there the SAME template can be APPROVED more than once; here it
// can go and COME BACK from a category — UTILITY -> MARKETING today,
// MARKETING -> UTILITY next week, UTILITY -> MARKETING again. A key
// template_categoria:{id} would deduplicate everything after the first;
// a key template_categoria:{id}:{previous}:{new} would deduplicate the
// THIRD against the FIRST, which are the same transition at different
// instants — and the third is precisely the one that reopens the appeal
// window. Only the time separates the two.
//
// The transition enters ALONGSIDE the time, and not in its place, because
// `entry.time` is in SECONDS: two changes to the same template in the
// same second are unlikely and not impossible, and without the
// transition in the key they'd collide. With it, they only collide if
// they're also the same transition — the moment dedup IS the right
// behavior.
//
// WHEN `entry.time` DOESN'T COME (0), the event still goes out, with ":0"
// in the key — the same decision as templateStatusEvent, same
// reason: losing the event is this project's most expensive outcome, and
// Meta doesn't redeliver after a 200.
//
// PhoneNumberID stays empty: an account webhook carries no
// metadata.phone_number_id. WabaID is the only routing key (guard 5b).
func templateCategoryEvent(rawValue json.RawMessage, wabaID string, when int64) (Event, bool) {
	t, state := messageBlock[templateCategoryMeta](rawValue)
	if state != blockRead {
		return Event{}, false
	}

	id := textFromNumberOrString(t.TemplateID)
	newCategory, _ := messageBlock[string](t.New)
	// THE SAME guard as templateStatusEvent, with the equivalent
	// field: there it's `message_template_id` + `event`, here it's
	// `message_template_id` + `new_category`. Without both the key would
	// become "template_categoria::::0" and collide with ANY other
	// equally empty change from the same batch, and an event that can't
	// be deduplicated is passive, not active — the raw body still
	// reaches the consumer regardless.
	//
	// `new_category` is the field chosen (and not `previous_category`)
	// because it's the FACT: a reclassification event that doesn't say
	// where the template went warns of nothing. The full direction is
	// better, and its absence alone doesn't erase the event — see
	// PreviousCategory, below, which degrades.
	if id == "" || newCategory == "" {
		return Event{}, false
	}

	// The rest degrade on their own (the boundary structs' rule): a
	// `"previous_category":42` costs the direction and nothing more —
	// the warning that the category changed, and the appeal window,
	// still arrive.
	//
	// AND THE ABSENT `previous_category` IS NO LONGER HYPOTHETICAL: one
	// of the 18 events consumer `consumer-b` had stored arrived without
	// the field, and it is frozen in
	// testdata/corpus/categoria_de_template_sem_anterior.json (T-174,
	// 2026-08-28). Until then this degradation was a decision with no
	// observation behind it. The empty piece stays IN THE MIDDLE of the
	// key, between the two colons — dropping it would collapse this
	// key's shape onto a different one.
	name, _ := messageBlock[string](t.Name)
	language, _ := messageBlock[string](t.Language)
	previous, _ := messageBlock[string](t.Previous)
	correct, _ := messageBlock[string](t.Correct)
	appeal, _ := messageBlock[string](t.AppealStatus)

	return Event{
		Type:      EventTypeTemplateCategory,
		ID:        "template_categoria:" + id + ":" + previous + ":" + newCategory + ":" + strconv.FormatInt(when, 10),
		WabaID:    wabaID,
		Timestamp: when,
		TemplateCategory: &TemplateCategory{
			Name:             name,
			Language:         language,
			PreviousCategory: previous,
			NewCategory:      newCategory,
			CorrectCategory:  correct,
			AppealStatus:     appeal, // "ELIGIBLE" passes through as it came — never a boolean
		},
	}, true
}

// keyDistinguishesSomething says whether an event's key has any piece left over
// capable of separating it from a neighbor in the SAME batch.
//
// EXISTS BECAUSE TWO ACCOUNT WEBHOOKS HAVE NO ID AT ALL (T-058), not
// because the rule changed. The rule is the same as the four earlier
// events' — "an event that can't be deduplicated doesn't become an event;
// the raw body reaches the consumer regardless" —, just that message,
// status, and the two template ones express it by naming the field that
// carries the id, and `phone_number_quality_update` and `account_alerts`
// have no such field to name.
//
// ELECTING A "REQUIRED" FIELD HERE WOULD BE WORSE, and the alert's case
// shows why: the natural candidate would be `alert_type`, and rejecting an
// alert without it would throw away precisely an alert that arrived with
// only `alert_severity` filled in — the severity is the field that makes
// this event worth having (see AccountAlert, types.go). The honest
// criterion is the key's own: if NOTHING distinguishes it, it collides
// with any other equally empty change in the batch and the consumer's
// dedup would erase both. If any piece is left over, it distinguishes.
//
// THE TIME DOESN'T COUNT as a piece, on purpose: it belongs to the BATCH,
// so two empty changes in the same payload would have the same time. A
// time alone separates nothing.
func keyDistinguishesSomething(pieces ...string) bool {
	for _, p := range pieces {
		if p != "" {
			return true
		}
	}
	return false
}

// numberQualityEvent builds the number QUOTA/quality event from the
// RAW `value` (T-058, 2026-07-28). Returns ok == false when the value
// cannot become an event — the caller counts it as ignored, never
// silently discards it.
//
// THE KEY CARRIES THE LIMIT TRANSITION, and not just the `event`, from the
// lesson already paid twice in this file (templateStatusEvent and
// templateCategoryEvent): `event` values REPEAT over a number's
// life — FLAGGED, UNFLAGGED, FLAGGED again — and a key carrying only those
// would make the second occurrence get deduplicated by the consumer.
// `old_limit` -> `current_limit` gives the other axis: a downgrade and a
// promotion between the same tiers are opposite facts.
//
// `display_phone_number` enters the key because a WABA can have more than
// one number: without it, two numbers flagged in the same second would
// collide. It's a LABEL, never routing — whoever routes an account
// webhook is the waba_id (guard 5b), and this payload has no
// `metadata.phone_number_id` at all.
func numberQualityEvent(rawValue json.RawMessage, wabaID string, when int64) (Event, bool) {
	q, state := messageBlock[numberQualityMeta](rawValue)
	if state != blockRead {
		return Event{}, false
	}

	number, _ := messageBlock[string](q.DisplayNumber)
	event, _ := messageBlock[string](q.Event)
	current, _ := messageBlock[string](q.CurrentLimit)
	previous, _ := messageBlock[string](q.PreviousLimit)
	max, _ := messageBlock[string](q.MaxDailyLimit)

	if !keyDistinguishesSomething(number, event, previous, current) {
		return Event{}, false
	}

	return Event{
		Type:      EventTypeNumberQuality,
		ID:        "qualidade_do_numero:" + number + ":" + event + ":" + previous + ":" + current + ":" + strconv.FormatInt(when, 10),
		WabaID:    wabaID,
		Timestamp: when,
		NumberQuality: &NumberQuality{
			DisplayNumber: number,
			State:         event,
			CurrentLimit:  current,  // "TIER_250" passes through as it came — never 250
			PreviousLimit: previous, // same
			MaxDailyLimit: max,
		},
	}, true
}

// accountAlertEvent builds the alert event from the RAW `value`
// (T-058, 2026-07-28). Returns ok == false when the value cannot become an
// event — the caller counts it as ignored, never silently discards it.
//
// THE KEY CARRIES THE SEVERITY AND THE STATE, and not just the alert
// type: the SAME `alert_type` can arrive twice with a different severity
// (an escalating problem) or with a different `alert_status` (a problem
// that gets resolved). With a key
// alerta_de_conta:{entity}:{type}:{time}, the ESCALATION warning would be
// deduplicated against the original warning — which is the only one of
// the two that demands action.
//
// THE TIME closes the key for the usual reason: the same alert, with the
// same severity, can happen again weeks later.
func accountAlertEvent(rawValue json.RawMessage, wabaID string, when int64) (Event, bool) {
	a, state := messageBlock[accountAlertMeta](rawValue)
	if state != blockRead {
		return Event{}, false
	}

	// entity_id comes as a NUMBER in the sample, so it's
	// textFromNumberOrString and not a strict read — unlike a wamid, there's
	// no risk here of "inventing an address": this id doesn't address
	// anything, it only identifies.
	entityID := textFromNumberOrString(a.EntityID)
	entityType, _ := messageBlock[string](a.EntityType)
	kind, _ := messageBlock[string](a.Type)
	severity, _ := messageBlock[string](a.Severity)
	alertState, _ := messageBlock[string](a.State)
	description, _ := messageBlock[string](a.Description)

	if !keyDistinguishesSomething(entityID, kind, severity, alertState) {
		return Event{}, false
	}

	return Event{
		Type:      EventTypeAccountAlert,
		ID:        "alerta_de_conta:" + entityID + ":" + kind + ":" + severity + ":" + alertState + ":" + strconv.FormatInt(when, 10),
		WabaID:    wabaID,
		Timestamp: when,
		AccountAlert: &AccountAlert{
			EntityType:  entityType,
			EntityID:    entityID,
			Type:        kind,
			Severity:    severity,
			State:       alertState,
			Description: description,
		},
	}, true
}

// textFromNumberOrString reads, as TEXT, a value that can arrive as a
// number or in quotes — and returns "" for absent, null, or any other
// shape.
//
// A sibling of tolerantInt (errors.go) and for the same reason: it's
// not an assertion about what Meta does, it's cheap tolerance. The
// difference is the destination — here the value becomes TEXT (a piece of
// a dedup key, or toUnix's input), so there's no numeric conversion to
// get wrong and no integer to overflow.
func textFromNumberOrString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s // also covers `null`: no-op, s stays ""
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// blockState says what happened to a block inside a message.
//
// THERE ARE THREE, NOT TWO, and the third is the reason this type exists
// (T-062): "didn't come" and "came and couldn't be read" produce the same
// envelope (the block doesn't appear) and are NOT the same fact. See
// messageEvent for what each one decides — in short: absent is Meta
// asserting there's no block, unreadable is our parser not understanding
// what came in, and only the first authorizes discarding the message.
type blockState int

const (
	blockAbsent     blockState = iota // didn't come, or came as `null`
	blockUnreadable                   // came, and the format isn't what we know how to read
	blockRead
)

// messageBlock reads a RAW block from inside messages[] and returns
// what survives from it.
//
// It's the generalization of T-061's contextoDaMensagem — the SAME
// function, with the type opened up —, not a second mechanism next to it.
// Until T-062 there was one reader per protected field (contextoDaMensagem,
// tolerantBool) and none for the remaining thirteen fields; a helper per
// field is exactly what makes the next round shield only the field of the
// moment.
//
// DISCARDS THE WHOLE BLOCK instead of making use of what encoding/json had
// already filled in before the error (the package keeps decoding the other
// fields and only returns the UnmarshalTypeError at the end). It's not
// line economy: it's the SAME shape statusEvent already uses for
// errors[0] and for pricing — "if the block couldn't be read, the block
// doesn't exist" —, and a rule that holds in one place and not its
// neighbor is the defect docs/ARMADILHAS.md records as this project's
// mother trap.
//
// The Unmarshal is done onto a **T and not onto a T because it's the only
// way to separate `null` from an empty object: `null` over a plain struct
// is a no-op (doesn't error and changes nothing), and without that
// distinction a `"location":null` would become a Location at 0,0 — a
// valid coordinate, in the middle of the Atlantic.
//
// The `p == nil` check IS NOT JUST SEMANTICS: without it, `*p` on a
// `null` is a nil dereference, and this file's top comment promises this
// parser NEVER panics. Found by running the mutation that removes it — it
// didn't leave a red test, it brought down the whole suite with a panic.
func messageBlock[T any](raw json.RawMessage) (T, blockState) {
	var zero T
	if len(raw) == 0 {
		return zero, blockAbsent
	}
	var p *T
	if err := json.Unmarshal(raw, &p); err != nil {
		return zero, blockUnreadable
	}
	if p == nil {
		return zero, blockAbsent // `null`
	}
	return *p, blockRead
}

// tolerantBool reads a bool that might not come, might come as `null`,
// or might come with an unexpected TYPE — and returns nil in all three
// cases, which is Event.Voice's "I don't know".
//
// It does NOT copy tolerantInt's tolerance (errors.go), which accepts
// the same value in quotes, and the difference is the COST OF GETTING IT
// WRONG: there the value is an error code; here it decides whether the
// audio is a PLAYABLE voice note or a plain attachment, and choosing
// wrong turns a voice note into an attachment with no error anywhere (the
// two-mimes trap, 2026-07-20, docs/ARMADILHAS.md). Faced with a shape no
// one has observed, "I don't know" is the right answer — and the *bool
// exists exactly so it can say that.
//
// STILL EXISTS after T-062, instead of the caller using
// messageBlock[bool] directly, because the *bool IS the TRANSLATION of
// the three states into the envelope's vocabulary: here, and only here,
// "absent" and "unreadable" become the same answer on purpose. Whoever
// reads the call in messageEvent sees the name of what's happening.
func tolerantBool(raw json.RawMessage) *bool {
	b, state := messageBlock[bool](raw)
	if state != blockRead {
		return nil
	}
	return &b
}

func toUnix(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
