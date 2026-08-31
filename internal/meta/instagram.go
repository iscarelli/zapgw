// Instagram — the first slice (T-097): receiving and sending TEXT DMs.
//
// Source, checked against developers.facebook.com on 2026-07-30
// (docs/TASKS.md, T-097's `Source:` block, and revalidated by this task's
// implementer on the two pages below, same date):
//
//   - sending: POST /<IG_ID>/messages, body
//     {"recipient":{"id":IGSID},"message":{"text":"..."}}, success response
//     {"recipient_id":"IGSID","message_id":"..."} — a DIFFERENT FORMAT from
//     WhatsApp's ({"messages":[{"id":"..."}]}, client.go), and that's why
//     this file does NOT reuse sendResponse.
//     (developers.facebook.com/docs/messenger-platform/instagram/features/send-message,
//     read on 2026-07-30).
//   - received-message webhook:
//     {"object":"instagram","entry":[{"id":"IGID","time":...,
//     "messaging":[{"sender":{"id":"IGSID"},"recipient":{"id":"IGID"},
//     "timestamp":...,"message":{"mid":"...","text":"..."}}]}]}
//     — checked against TWO independent pages, with the SAME example:
//     developers.facebook.com/docs/instagram-platform/webhooks/examples/ and
//     developers.facebook.com/docs/messenger-platform/instagram/features/webhook/
//     (both read on 2026-07-30).
//   - signature: THE SAME generic Graph API Webhooks infrastructure that
//     WhatsApp already uses — "We sign all Event Notification payloads with a
//     SHA256 signature and include the signature in the request's
//     X-Hub-Signature-256 header, preceded with sha256="
//     (developers.facebook.com/docs/instagram-platform/webhooks, read on
//     2026-07-30). THAT'S WHY INBOUND DIDN'T GAIN A SECOND VERIFICATION PATH:
//     meta.SignatureValid (signature.go) already covers it — the app_secret
//     and the algorithm are the SAME, only the body it covers changes shape.
//     Instagram's doc, like WhatsApp's, does NOT explicitly state "over the
//     raw bytes" (neither one does — see docs/ARMADILHAS.md, "Assinatura" has
//     no entry about this because it was never a problem: both point to the
//     SAME Webhooks product shared between WhatsApp, Instagram, Messenger,
//     and Pages, and the gateway has verified over the raw bytes for
//     WhatsApp since plan 1, without incident).
//     ⚠️ NOT CHECKED: whether Meta requires App Review/advanced access for an
//     App to act on behalf of third-party accounts (the same caveat the task
//     already records in docs/TASKS.md) — doesn't block this slice, but
//     don't assert that it doesn't require it.
package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrResponseWithoutMessageID: Meta answered 2xx to sending an Instagram DM but
// didn't send a message_id.
//
// THE SAME trap (and the same RETRYABLE class, see docs/ARMADILHAS.md,
// "Meta / WhatsApp Cloud API": "Um `200` da Meta NÃO prova que veio id") as
// ErrResponseWithoutID (client.go) — its OWN sentinel because the FIELD
// is different (`messages[0].id` on WhatsApp; `message_id` at the root, on
// Instagram), and an `errors.Is` that confused the two would hide which API
// answered wrong.
var ErrResponseWithoutMessageID = errors.New("meta: resposta 2xx do Instagram sem message_id")

// ErrUnmodeledItems: T-106. The `messaging[]` item was READ successfully
// — it's a LEGITIMATE Meta payload — but this slice (T-097, text messages
// only) doesn't model it as an Event: a read receipt, a delivery receipt,
// a postback, a story reaction, or a message with an attachment (`mid`
// without `text`).
//
// NEVER the same thing as ErrPartialParse. That one says "I couldn't read
// this"; this one says "I read this, and it's not what this slice
// understands". Before this separation the two fell into the SAME
// `ignorados`, and the SAME sentence ("parte do payload nao pode ser lida")
// went out for both — including for the item the gateway had just
// identified with certainty (see instagramMessageEvent's comment and
// docs/TASKS.md, T-106, which MEASURED this in production: two "parse
// failed" batches per response sent, triggering the failure monitor on
// perfectly normal traffic).
var ErrUnmodeledItems = errors.New("meta: item legitimo que esta fatia do instagram nao modela")

// instagramEnvelopeMeta and instagramEntryMeta are the RAW format of the
// top of an Instagram webhook. `Entry`/`Messaging` are json.RawMessage so
// that a malformed item (an `entry` that isn't an object, a `messaging`
// item whose type doesn't match) only loses THAT item — its siblings keep
// getting read.
//
// DOES NOT REPLICATE THE PER-FIELD ISOLATION DEPTH that
// internal/meta/parse.go has for WhatsApp (json.RawMessage on EVERY inner
// field): that defense was born from three real incidents across dozens of
// tasks (docs/ARMADILHAS.md, "Go / JSON") — this is Instagram's FIRST
// slice, with no history of its own, and copying the defense's shape
// without having the reason for it would be ceremony. If a field here ever
// causes the same problem, the fix is the SAME (isolate that field) — not a
// reason to rewrite everything today.
type instagramEnvelopeMeta struct {
	Entry json.RawMessage `json:"entry"`
}

type instagramEntryMeta struct {
	// ID is `entry[].id` — the IGID (Instagram professional account id)
	// that received the event. It's a ROUTING KEY: IgIDsInPayload, below,
	// is what inbound's addressing guard checks against the instance's
	// ig_id from the path — the same discipline as WhatsApp's waba_id
	// (parse.go, AccountWabaIDsInPayload).
	ID        json.RawMessage `json:"id"`
	Messaging json.RawMessage `json:"messaging"`
}

// messagingItemMeta is the RAW format of ONE `messaging[]` item.
//
// Instagram sends, in the SAME `messaging` array, things that aren't text
// messages — reaction, read receipt, postback, story reply — and the echo
// of the business's own reply. This slice ONLY models the case of a present
// `message.text` (non-echo); the others reach this far and do NOT become an
// Event, but for THREE different reasons that instagramMessageEvent
// distinguishes (T-106, after measuring in production that mixing the
// three lied about the most common one): unreadable (`itemUnreadable`, counts
// toward ErrPartialParse), legitimate-but-not-modeled (`itemUnmodeled`,
// counts toward ErrUnmodeledItems), and echo (`itemEcho`, silent —
// doesn't count anywhere). The raw body still goes to the consumer the same
// way in all three cases (see the top of parse.go, "parsing is enrichment,
// never a precondition for delivery").
type messagingItemMeta struct {
	Sender    json.RawMessage `json:"sender"`
	Timestamp json.RawMessage `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type instagramParticipantMeta struct {
	ID string `json:"id"`
}

type instagramMessageBodyMeta struct {
	Mid  string `json:"mid"`
	Text string `json:"text"`
	// IsEcho: T-105. Source, checked against developers.facebook.com on
	// 2026-07-31, on TWO independent pages with the SAME example (same
	// discipline as this file's header):
	//   developers.facebook.com/docs/messenger-platform/instagram/features/webhook/
	//   ("is_echo flag will be present to indicate that the message is sent
	//   from the Instagram account itself", with example
	//   {"message":{"mid":"...","text":"...","is_echo":true}})
	//   developers.facebook.com/docs/instagram-platform/webhooks/examples/
	//   ("is_echo set to true is included when the message was sent by your
	//   app user")
	// See instagramMessageEvent, below, for why this matters.
	IsEcho bool `json:"is_echo"`
}

// ParseInstagramWebhook converts the raw body of an Instagram webhook
// (`"object":"instagram"`) into typed events, in the SAME vocabulary
// WhatsApp uses for messages (EventTypeMessage, internal/meta/types.go) — a
// consumer on an Instagram instance doesn't need to learn a new event type:
// they already know, from the SLUG they configured, which product this is.
//
// HARD CONTRACT EQUAL TO ParseWebhook's: NEVER panics, and is NEVER a
// precondition for delivery — the caller (internal/inbound/handler.go)
// delivers the raw body even when this function returns an error or a
// partial list.
func ParseInstagramWebhook(payload []byte) ([]Event, error) {
	var env instagramEnvelopeMeta
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, err
	}

	var evs []Event
	unreadable := 0 // ErrPartialParse — the ONLY category where "parte do payload nao pode ser lida" is true
	unmodeled := 0  // ErrUnmodeledItems — read, legitimate, this slice doesn't model it

	entries, entriesState := messageBlock[[]json.RawMessage](env.Entry)
	if entriesState == blockUnreadable {
		unreadable++
	}
	for _, rawEntry := range entries {
		var ent instagramEntryMeta
		if err := json.Unmarshal(rawEntry, &ent); err != nil {
			unreadable++ // an entry[] item that isn't a JSON object
			continue
		}
		// STRICT read of the id, the same rule as WhatsApp's waba_id
		// (parse.go): a `"id":42` doesn't become the IGID "42".
		igID, _ := messageBlock[string](ent.ID)

		items, itemsState := messageBlock[[]json.RawMessage](ent.Messaging)
		if itemsState == blockUnreadable {
			unreadable++
		}
		for _, rawItem := range items {
			var m messagingItemMeta
			if err := json.Unmarshal(rawItem, &m); err != nil {
				unreadable++
				continue
			}
			ev, result := instagramMessageEvent(m, igID)
			switch result {
			case itemModeled:
				evs = append(evs, ev)
			case itemUnreadable:
				unreadable++ // counted, never silently discarded
			case itemUnmodeled:
				unmodeled++ // counted, never silently discarded
			case itemEcho:
				// T-106: SILENT on purpose — see
				// instagramMessageEvent's comment. Increments
				// nothing.
			}
		}
	}

	return evs, instagramParseErrors(unreadable, unmodeled)
}

// instagramParseErrors builds ParseInstagramWebhook's final error from
// the TWO counts T-106 separated (unreadable vs. legitimate-and-not-
// modeled — the echo, a third category, never reaches here: it's silent).
//
// CHOSEN SHAPE (the task leaves the shape open — two distinct errors, or
// one error with two fields): TWO DISTINCT SENTINELS, joined with
// errors.Join when both counts are positive in the SAME batch, instead of
// an error struct with two ints. Reason: ErrPartialParse already exists and
// is already checked with errors.Is in several places (corpus_test.go,
// parse_test.go); the ONLY consumer today of ParseInstagramWebhook's error
// (internal/inbound/handler.go, the "parse failed" log) only wants the
// TEXT, never a concrete type. A new struct would force it, and every
// future caller, to learn a type just to read a count the text already
// carries; two sentinels solve that AND still respond to errors.Is/errors.As
// if a future caller needs to distinguish the two causes programmatically.
func instagramParseErrors(unreadable, unmodeled int) error {
	var parts []error
	if unreadable > 0 {
		parts = append(parts, fmt.Errorf("%w: %d item(ns) ignorado(s)", ErrPartialParse, unreadable))
	}
	if unmodeled > 0 {
		parts = append(parts, fmt.Errorf("%w: %d item(ns)", ErrUnmodeledItems, unmodeled))
	}
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return parts[0]
	default:
		return errors.Join(parts...)
	}
}

// messagingItemResult classifies ONE `messaging[]` item that didn't
// become an Event — T-106. Before this task instagramMessageEvent
// returned just a bool (`ok == false`), and ParseInstagramWebhook counted
// EVERY rejection as `ignorados`, producing the SAME sentence ("parte do
// payload nao pode ser lida") for four different reasons — only the first
// (JSON that's really unreadable) was true of it. MEASURED in production
// (docs/TASKS.md, T-106): starting from the echo fix (T-105), EVERY reply
// the business sends generates an item that fell into this bucket, and the
// failure monitor was firing on perfectly normal traffic.
type messagingItemResult int

const (
	itemModeled    messagingItemResult = iota // became an Event
	itemUnreadable                            // the item's JSON doesn't match the expected format — ErrPartialParse
	itemUnmodeled                             // read, legitimate, this slice doesn't model it — ErrUnmodeledItems
	itemEcho                                  // recognized and deliberately left out of the batch — SILENT
)

// instagramMessageEvent builds the event from ONE `messaging[]` item.
// See messagingItemResult for what each rejection means (this slice
// only models non-echo TEXT messages — see the messagingItemMeta type's
// comment).
func instagramMessageEvent(m messagingItemMeta, igID string) (Event, messagingItemResult) {
	msg, state := messageBlock[instagramMessageBodyMeta](m.Message)
	switch state {
	case blockUnreadable:
		// The `message` field CAME, but in a format WE DON'T KNOW HOW TO
		// READ — the ONLY one of this function's rejections where "can't
		// be read" is true.
		return Event{}, itemUnreadable
	case blockAbsent:
		// No `message` block AT ALL: read receipt, delivery receipt,
		// postback, story reaction — Meta sends all of these in the SAME
		// `messaging[]` array, and this slice (T-097) only models text.
		// It WAS READ; it just doesn't become an Event. NEVER "can't be
		// read".
		return Event{}, itemUnmodeled
	}
	// blockRead from here on.

	// T-105: an echo (the message the BUSINESS ITSELF sent, echoed back
	// by Meta) NEVER becomes a modeled event — it comes out of the
	// `eventos` batch, and the `cru` still goes whole to the consumer
	// (parsing is enrichment, never a precondition for delivery, see the
	// top of ParseInstagramWebhook and of this file). Isolated BEFORE
	// `mid`/`texto` because it's the strongest reason to reject, and by
	// construction an echo HAS both filled in — without this check first
	// it would pass the guards below with no trouble and become an
	// Event, which was exactly the defect measured in production: an
	// echo's counterpart is the business's OWN IGID (`sender.id` == the
	// account that sent it, see the Source in the
	// instagramMessageBodyMeta type), the consumer read this as "a
	// customer message arrived" and replied to its own echo — Meta
	// refuses to send to itself, and the send failed with `permanente`
	// on EVERY reply the business sends.
	//
	// T-106: the echo is SILENT (itemEcho), NOT "not modeled" — this
	// task's decision. It's different from a read receipt: it's not a
	// type this gateway doesn't understand yet, it's a type the gateway
	// recognizes PERFECTLY and discards ON PURPOSE because the consumer
	// already knows what it itself sent. Counting this as "not modeled"
	// would flag a new type worth attention, when in fact it's the most
	// common and best-understood case of all — that mixing is what T-106
	// measured as two "parse failed" warnings per reply sent, in
	// production.
	//
	// NOT A NEW EVENT TYPE ("echo", "mirror"...): T-105's own decision —
	// that would be new contract surface for the day someone proves they
	// need it.
	if msg.IsEcho {
		return Event{}, itemEcho
	}

	mid := strings.TrimSpace(msg.Mid)
	// Without `mid` there's no possible dedup key — the same guard
	// messageEvent (parse.go) applies to WhatsApp's wamid. It WAS
	// READ, and the block exists: NOT-MODELED, never unreadable.
	if mid == "" {
		return Event{}, itemUnmodeled
	}
	text := strings.TrimSpace(msg.Text)
	// WITHOUT TEXT, WE DON'T MODEL IT: a `message` with an attachment
	// (image, audio, story reply) arrives with `mid` but no `text` —
	// inventing an empty-text event would assert that the customer wrote
	// nothing, when in fact they sent something this slice doesn't read.
	// The item stays only in the raw body. It WAS READ: NOT-MODELED,
	// never unreadable.
	if text == "" {
		return Event{}, itemUnmodeled
	}

	sender, _ := messageBlock[instagramParticipantMeta](m.Sender)
	from := strings.TrimSpace(sender.ID)

	return Event{
		Type: EventTypeMessage,
		ID:   "msg:" + mid,
		// NO PhoneNumberID/WabaID, ON PURPOSE: Instagram doesn't have
		// either, and inbound's addressing guard compares IgIDsInPayload
		// against inst.IgID DIRECTLY, without going through Event.
		// Leaving both fields empty is the same reading a WhatsApp
		// account webhook already produces when the field doesn't exist
		// in that format (see Event's comment in types.go).
		Timestamp: toUnix(textFromNumberOrString(m.Timestamp)),
		// WaMessageID carries the `mid` — a deliberate reuse of the SAME
		// field WhatsApp uses for the wamid: both are "the identifier
		// Meta gave this message", and a consumer with instances of
		// both types doesn't need to learn a field name per product
		// (the same reasoning that decided to keep `wa_message_id` in
		// POST /v1/messages's response — see docs/CONTRATO-CONSUMIDOR.md,
		// Instagram section).
		WaMessageID: mid,
		SubType:     "text",
		// FromRaw == FromCanonical: the IGSID does NOT go through Canonicalize
		// (see Event.FromCanonical's comment, types.go) — the two fields
		// coexist only so the consumer doesn't need to know, field by
		// field, which product generated the event.
		FromRaw:       from,
		FromCanonical: from,
		Text:          text,
	}, itemModeled
}

// IgIDsInPayload returns the `entry[].id` of EVERY entry in an Instagram
// payload — it's what inbound's addressing guard
// (internal/inbound/handler.go) uses to check that the batch belongs to
// this instance, BEFORE trusting any modeled event.
//
// "" ENTERS THE LIST when the id didn't come, came as `null`, or can't be
// read — the same decision (it's the SAME question) as
// AccountWabaIDsInPayload (parse.go): "can we PROVE this entry belongs to
// this instance?" — UNREADABLE answers no, just as much as ABSENT, and the
// handler's guard treats both as a non-match (never as "" == "" for an
// instance with an empty ig_id, which ValidateInstanceType already
// prevents from existing).
//
// NO PANIC for a body that isn't the expected object: json.Unmarshal
// failing returns an empty list, and the caller (the handler) only enters
// this path AFTER confirming the body is a JSON object (step 2,
// ReadRaw+ParseInstagramWebhook already ran first) — but the function
// doesn't trust that and doesn't panic regardless.
func IgIDsInPayload(payload []byte) []string {
	var env instagramEnvelopeMeta
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil
	}
	entries, _ := messageBlock[[]json.RawMessage](env.Entry)
	ids := make([]string, 0, len(entries))
	for _, rawEntry := range entries {
		var ent instagramEntryMeta
		if err := json.Unmarshal(rawEntry, &ent); err != nil {
			ids = append(ids, "")
			continue
		}
		id, _ := messageBlock[string](ent.ID)
		ids = append(ids, id)
	}
	return ids
}

// SendInstagramMessage sends a TEXT DM and returns the message_id.
//
// POST /<IG_ID>/messages, body
// {"recipient":{"id":...},"message":{"text":...}} — see the Source at the
// top of this file. The token goes in the HEADER, never in the URL (same
// rule as SendMessage, client.go): Meta's example page shows
// `?access_token=`, but Authorization: Bearer is the Graph API's standard
// mechanism (the same infrastructure WhatsApp uses) and it's the only one
// this gateway uses on any call, so as not to open a second way for a token
// to leak through a proxy/CDN log.
//
// 🔴 T-104: `base` is a PARAMETER, NEVER `c.base` — the SAME discipline as
// RenewInstagramToken, below in this file, and for the SAME reason: this
// endpoint lives on graph.instagram.com, a HOST DIFFERENT from the rest of
// the Graph API that `c.base` points to (typically
// graph.facebook.com/vNN.N). Until this task, this function used `c.base`,
// and that's how tenant-two-ig's first real activation died at smoke-test
// step 2 with "Invalid OAuth access token - Cannot parse access token": an
// Instagram Login token is **not parseable** on the wrong host.
// RenewInstagramToken's comment already warned about this — the lesson
// just hadn't crossed over to this neighboring function. MEASURED, not
// deduced: `POST https://graph.instagram.com/{ig_id}/messages` with this
// SAME body returned `200` against the real Meta on 2026-07-31 (see
// docs/TASKS.md, T-104's `Source:` block) — only the host was wrong, never
// the body, the Bearer, or the id.
func (c *Client) SendInstagramMessage(
	ctx context.Context, base, igID, token, recipientIGSID, text string,
) (SendResponse, error) {
	// THE SAME shape check WhatsApp's send uses (PhoneNumberIDValid,
	// client.go): url.JoinPath resolves `..` like path.Join, and the
	// ig_id comes from the store the same way the phone_number_id does —
	// provisioned by an admin, but implicit trust is what no one
	// rechecks the day the data's origin changes. The function's NAME is
	// WhatsApp's; the RULE (only letters, digits, `_`, `-`) isn't
	// specific to any phone number — reusing it avoids a second copy of
	// the same rule diverging at the first change.
	if !PhoneNumberIDValid(igID) {
		return SendResponse{}, ErrInvalidPhoneNumberID
	}

	body := map[string]any{
		"recipient": map[string]any{"id": recipientIGSID},
		"message":   map[string]any{"text": text},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return SendResponse{}, fmt.Errorf("meta: montar corpo: %w", err)
	}

	// `base`, NEVER `c.base` — see the function's comment, above.
	target, err := url.JoinPath(base, igID, "messages")
	if err != nil {
		return SendResponse{}, fmt.Errorf("meta: montar url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return SendResponse{}, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// We do NOT interpolate the error: *url.Error carries the full
		// URL, which here carries the ig_id. Same rule as SendMessage.
		return SendResponse{}, fmt.Errorf("meta: falha de transporte ao enviar (instagram): %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	rawResponse, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return SendResponse{}, fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}

	if metaError := ClassifyResponse(resp.StatusCode, rawResponse); metaError != nil {
		return SendResponse{}, metaError
	}
	return instagramSendResponse(rawResponse)
}

// instagramSendResponse extracts `message_id` from the body's ROOT — a
// format DIFFERENT from WhatsApp's (`messages[0].id`, sendResponse in
// client.go). See the Source at the top of this file:
// {"recipient_id":"IGSID","message_id":"..."}.
func instagramSendResponse(raw []byte) (SendResponse, error) {
	var envelope struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return SendResponse{}, fmt.Errorf("%w: corpo nao entendido", ErrResponseWithoutMessageID)
	}
	// Trim BEFORE deciding, same rule as sendResponse: an id made of
	// only spaces is as useless as an empty one.
	id := strings.TrimSpace(envelope.MessageID)
	if id == "" {
		return SendResponse{}, ErrResponseWithoutMessageID
	}
	return SendResponse{ID: id}, nil
}

// 🔴 T-104: THERE IS NO CheckCredential FOR INSTAGRAM, AND THE ABSENCE IS
// DELIBERATE.
//
// The smoke test's step 2 (internal/outbound/smoke.go) exists to catch a
// revoked token WITHOUT sending a message to a real number — on WhatsApp
// that's `GET /{phone_number_id}` (CheckCredential, client.go). This
// file has NO sibling function for Instagram because there is no
// MEASURED equivalent call: T-104's Source only measured `POST
// /{ig_id}/messages` against the real Meta, and the technique consumer-b's
// hunt left on record (see the top of this file) is that `debug_token`
// REJECTS an Instagram Login token on BOTH hosts — meaning the only
// measured way, so far, to confirm an Instagram
// permission/token is valid is to HIT the endpoint that requires it.
// Inventing a `GET /{ig_id}` "by analogy" with WhatsApp would be a call
// that CAN lie (accept or reject for a reason that isn't the token), and
// this project prefers skipping a check to making one that deceives — see
// the decision in internal/outbound/smoke.go, where step 2 is SKIPPED for
// Instagram instances, documenting why. Meta still confirms the token: only
// at step 3, when the real test send goes out.
//
// --- T-098: renewing Instagram's long-lived token --------------------
//
// Source, checked DIRECTLY against
// developers.facebook.com/docs/instagram-platform/reference/refresh_access_token/
// on 2026-07-30 (the same date as T-098's `Source:` block in docs/TASKS.md,
// which brought the endpoint and the preconditions but not the body's
// format — this comment closes that gap with the endpoint's own reference
// page, not with trained memory):
//
//	Request:  GET https://graph.instagram.com/refresh_access_token
//	          ?grant_type=ig_refresh_token&access_token=<CURRENT_TOKEN>
//	Response: {"access_token":"<NEW>","token_type":"bearer","expires_in":<seconds>}
//
// `expires_in` is NOT read: this gateway already knows the validity from
// its own source (60 days, T-098's `Source:` block) and it's the same
// constant that DECIDES when to renew (InstagramTokenValidity,
// internal/outbound/instagram_renewer.go) — using ONE response's
// `expires_in` to redefine the validity would create a SECOND source of the
// same truth, and the two would diverge the day Meta changes the number
// without notice. `token_definido_em` (when THIS function returns success)
// is this gateway's only clock for the token's deadline.

// DefaultInstagramRenewalBase is the PRODUCTION root of the WHOLE
// graph.instagram.com host — a HOST DIFFERENT from the rest of the Graph
// API this client talks to (`c.base`, typically graph.facebook.com/vNN.N):
// renewal (below) and sending a DM (SendInstagramMessage, above) BOTH
// live on graph.instagram.com, WITHOUT a version prefix. That's why both
// functions receive the base as a PARAMETER instead of using `c.base` —
// confusing the two hosts would point the call at a URL Meta doesn't
// serve (that was EXACTLY T-104's defect: SendInstagramMessage used
// `c.base` until that task).
//
// 🔴 The NAME stayed "Renovacao" for HISTORICAL reasons (T-098 created it
// just for RenewInstagramToken), but the VALUE is generic — the host's
// root, not one endpoint's. T-104 reused this SAME constant for
// SendInstagramMessage instead of creating a second one with a
// different name: a new constant, with the SAME value, would be a third
// way of saying "graph.instagram.com" — this project's mother trap
// (docs/ARMADILHAS.md) applied to a URL. Renaming it would require
// touching every reference (main.go, instagram_renewer.go, the tests,
// the docs) for aesthetics alone; it wasn't done.
//
// EXPORTED and INJECTABLE for the SAME reason as graphBase
// (cmd/zapgw/main.go): no package under internal/ reads an environment
// variable, and the test has to be able to point at a fake server — NEVER
// at the real Meta.
const DefaultInstagramRenewalBase = "https://graph.instagram.com"

// ErrRenewalWithoutAccessToken: Meta answered the renewal with 2xx but didn't
// send an `access_token` — the SAME trap and the SAME class (RETRYABLE —
// see docs/ARMADILHAS.md, "Meta / WhatsApp Cloud API": "Um `200` da Meta
// NÃO prova que veio id") as this package's two siblings,
// ErrResponseWithoutID (client.go) and ErrResponseWithoutMessageID (above). Its OWN
// sentinel because the FIELD is different once again (`access_token` at the
// root, here).
var ErrRenewalWithoutAccessToken = errors.New("meta: resposta 2xx da renovacao do instagram sem access_token")

// RenewInstagramToken requests a new long-lived token starting from a
// token that's STILL valid. It does NOT decide WHEN to renew nor validate
// Meta's preconditions (24h old, not expired) — that's the CALLER's job
// (internal/outbound/instagram_renewer.go); this function only talks to
// Meta and returns the new token, or the classified error.
//
// 🔴 THE TOKEN GOES IN THE QUERY STRING, NOT IN THE HEADER — and this is a
// deliberate EXCEPTION to this package's general rule ("the token goes in
// the header, never in the URL", see SendInstagramMessage above and
// SendMessage in client.go). The difference isn't an oversight: on the
// other endpoints the `?access_token=` is just a SHORTCUT the example page
// shows alongside the preferred form (Authorization: Bearer), and this
// gateway always chooses the second. Here there IS NO second documented
// form — the reference page cited above only shows `access_token` as a
// REQUIRED QUERY PARAMETER, and the value isn't just any READ credential:
// it's the very token being renewed, required by the endpoint's mechanism.
// This means that, for this call alone, the current token passes through
// any proxy/CDN between this gateway and graph.instagram.com inside the
// URL — mitigated by TLS in transit (this gateway has no path without TLS,
// CLAUDE.md), but not by logs on Meta's side, which this gateway doesn't
// control. Recorded here, and in this task's report, for the owner to
// decide whether to accept it — it wasn't hidden.
func (c *Client) RenewInstagramToken(ctx context.Context, renewalBase, currentToken string) (string, error) {
	target, err := url.Parse(strings.TrimRight(renewalBase, "/") + "/refresh_access_token")
	if err != nil {
		return "", fmt.Errorf("meta: montar url de renovacao: %w", err)
	}
	target.RawQuery = url.Values{
		"grant_type":   {"ig_refresh_token"},
		"access_token": {currentToken},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", fmt.Errorf("meta: montar requisicao de renovacao: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// We do NOT interpolate the error: *url.Error carries the full
		// URL, which here carries the CURRENT token in the query string
		// (see the comment above). Same rule as
		// SendMessage/SendInstagramMessage.
		return "", fmt.Errorf("meta: falha de transporte ao renovar o token do instagram: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return "", fmt.Errorf("meta: ler resposta da renovacao: %w", errWithoutDetail(err))
	}

	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return "", metaError
	}

	var envelope struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("%w: corpo nao entendido", ErrRenewalWithoutAccessToken)
	}
	// Trim BEFORE deciding, same rule as this package's two send
	// responses: a token made of only spaces is as useless as an empty
	// one, and stored as if it were valid it would erase the GOOD token
	// that was still in the store.
	newToken := strings.TrimSpace(envelope.AccessToken)
	if newToken == "" {
		return "", ErrRenewalWithoutAccessToken
	}
	return newToken, nil
}
