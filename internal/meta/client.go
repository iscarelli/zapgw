// Client for Meta's Graph API.
//
// The gateway is the ONLY thing on the network that speaks this protocol; on
// the outside there's only the zapgw contract. That's why Meta's quirks die
// here.
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

// ErrResponseWithoutID: Meta answered 2xx but didn't send a message id.
//
// TRAP, and it has already cost this network's another project: a Meta
// `200` does NOT prove an id came with it. The same response can bring
// {"messages":[]}, {}, or an id of the wrong type — all valid JSON with a
// status < 400. Returning "" as success is the worst possible outcome: the
// consumer records wa_message_id="" in the database, the record LOOKS sent,
// and the defect shows up far from its origin.
//
// Classified as RETRYABLE: Meta responded, but didn't answer what was
// agreed — the same treatment given to a malformed body from it.
var ErrResponseWithoutID = errors.New("meta: resposta 2xx sem id de mensagem")

// ErrInvalidPhoneNumberID: the identifier has a shape that cannot safely
// become a URL segment.
var ErrInvalidPhoneNumberID = errors.New("meta: phone_number_id com forma invalida")

const responseBodyCap = 1 << 20 // 1 MiB: the Graph API's response is small

type Client struct {
	http *http.Client
	base string
}

// NewClient builds the client. `base` is the Graph API root (e.g.
// "https://graph.facebook.com/v21.0"), injectable for testing.
func NewClient(client *http.Client, base string) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{http: client, base: base}
}

// SendResponse is what Meta returned for an ACCEPTED send: the message id
// and the pacing status it reported, raw.
type SendResponse struct {
	ID string
	// MessageStatus is the RAW value of messages[0].message_status. ""
	// means Meta did NOT send the field — and that's the EXPECTED case for
	// most of this traffic (see the source cited in sendResponse), not a
	// read failure. Absence and "accepted" are NOT the same thing: ""
	// never becomes "accepted" on its own, because asserting a value Meta
	// didn't send would be inventing data it never confirmed.
	//
	// It is NOT translated, nor mapped to ErrorClass (retryable/permanent/
	// config): the source doesn't support that translation (see
	// sendResponse), and mapping without a source is worse than passing
	// the raw value through — see docs/ARMADILHAS.md, "Duas fontes
	// independentes que descem do mesmo nada" and T-024's sibling case.
	MessageStatus string
}

// SendMessage sends the already-built body and returns the
// wa_message_id (and the message_status, when Meta sends it).
//
// The token goes in the HEADER, never in the URL: a token in a query string
// leaks into proxy, server, and CDN logs.
func (c *Client) SendMessage(
	ctx context.Context, phoneNumberID, token string, body map[string]any,
) (SendResponse, error) {
	if !PhoneNumberIDValid(phoneNumberID) {
		return SendResponse{}, ErrInvalidPhoneNumberID
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return SendResponse{}, fmt.Errorf("meta: montar corpo: %w", err)
	}

	target, err := url.JoinPath(c.base, phoneNumberID, "messages")
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
		// Transport failure. We do NOT interpolate the error: *url.Error
		// carries the full request URL, and it carries the
		// phone_number_id.
		return SendResponse{}, fmt.Errorf("meta: falha de transporte ao enviar: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	rawResponse, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return SendResponse{}, fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}

	if metaError := ClassifyResponse(resp.StatusCode, rawResponse); metaError != nil {
		return SendResponse{}, metaError
	}
	return sendResponse(rawResponse)
}

// CheckCredential asks the Graph API whether this instance's token is
// still valid.
//
// `GET /{phone_number_id}` doesn't create anything on the other side, and
// it's the ONLY way to catch a token the client revoked at Meta WITHOUT
// sending a message to a real number. A revoked token is the failure that
// dies silently: nothing in the gateway changes, and the first to know
// would be the end customer who didn't receive anything.
//
// DOES NOT return ANYTHING from the success body — just nil (accepted) or
// the already-classified error. Passing that body through would assert a
// Meta response schema that was never checked against the source, and the
// consumer would start depending on it; whoever needs to describe the
// number reads the store, which is our own data.
//
// It is step 2 of the smoke test (cmd/zapgw/fumaca.go) AND the body of the
// per-instance probe (internal/outbound/saude_handler.go). A copy of the
// call in each place would be this project's mother trap — the same true
// sentence in one place and not the next, diverging at the first change.
func (c *Client) CheckCredential(ctx context.Context, phoneNumberID, token string) error {
	// THE SAME guard as sending, and the same function (not a copy):
	// url.JoinPath resolves `..` like path.Join, so an id with `../` would
	// escape the Graph API's version prefix and point to another endpoint.
	if !PhoneNumberIDValid(phoneNumberID) {
		return ErrInvalidPhoneNumberID
	}
	target, err := url.JoinPath(c.base, phoneNumberID)
	if err != nil {
		return fmt.Errorf("meta: montar url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("meta: montar requisicao: %w", err)
	}
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("meta: falha de transporte ao conferir a credencial: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	// ClassifyResponse reads ONLY error.message and error.code — never
	// the rest of the body, which can echo client data.
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return metaError
	}
	return nil
}

// sendResponse extracts messages[0].id (requiring it to exist and be a
// non-empty string — any other shape becomes ErrResponseWithoutID) and, from
// the SAME item, messages[0].message_status — raw, with no translation at
// all.
//
// SOURCE, both read on 2026-07-26:
//
//   - developers.facebook.com/documentation/business-messaging/whatsapp/messages/send-messages
//     — the "Response contents" table describes the field's placeholder like
//     this: "The message_status property is only included in responses when
//     sending a template message that uses a template that is being paced."
//     In other words: (i) the field does NOT come in every response — only
//     when the request is for a TEMPLATE and that template is under pacing.
//     For the rest of this gateway's traffic (text, media, interactive,
//     reaction, location, and even a template outside of pacing) the field
//     comes back absent, and that's the EXPECTED case, not a read failure —
//     hence SendResponse.MessageStatus staying "" in those cases, never an
//     invented value.
//   - developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-pacing
//     — about held_for_quality_assessment: "If the feedback is positive...
//     the held messages will be released and sent normally" but "If the
//     feedback is negative... Each held message will be dropped." In other
//     words, the question "will it be delivered later or can it never be"
//     has BOTH answers — the outcome is only decided later, and that's why
//     this code cannot treat held_for_quality_assessment as equivalent to
//     accepted.
//
// WITHOUT A SOURCE, and therefore deliberately NOT decided here: the two
// official pages DISAGREE about what "paused" means. The API reference page
// (developers.facebook.com/docs/whatsapp/cloud-api/reference/messages)
// lists "paused" as one of the THREE values of message_status itself
// ("paused: The message delivery has been paused" — reading: about the
// MESSAGE). The template-pacing page above describes "paused" as the
// TEMPLATE's OWN `status` ("The template's status will be set to PAUSED" —
// reading: about the TEMPLATE, a field DIFFERENT from message_status.
// There's no way to know, from the doc alone, which of the two the Graph
// API actually implements — that's why this code just passes the raw
// message_status value through, whatever it is, and doesn't invent which
// of the two readings is correct.
func sendResponse(raw []byte) (SendResponse, error) {
	var envelope struct {
		Messages []struct {
			ID            string `json:"id"`
			MessageStatus string `json:"message_status"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return SendResponse{}, fmt.Errorf("%w: corpo nao entendido", ErrResponseWithoutID)
	}
	if len(envelope.Messages) == 0 {
		return SendResponse{}, ErrResponseWithoutID
	}
	// Trim BEFORE deciding: an id made of only spaces is as useless as an
	// empty one, and passing it through as success is this function's
	// worst possible outcome — the consumer records a wa_message_id that's
	// good for nothing and the record LOOKS sent.
	id := strings.TrimSpace(envelope.Messages[0].ID)
	if id == "" {
		return SendResponse{}, ErrResponseWithoutID
	}
	return SendResponse{ID: id, MessageStatus: envelope.Messages[0].MessageStatus}, nil
}

// PhoneNumberIDValid accepts only letters, digits, `_`, and `-`.
//
// WHY VALIDATE SOMETHING THAT COMES FROM INSIDE: `url.JoinPath` resolves
// `..` like path.Join, so an id with `../` ESCAPES the Graph API's version
// prefix and points to another endpoint. Today the value comes from the
// store, provisioned by an admin — but that's implicit trust, and implicit
// trust is what no one remembers to recheck the day the data's origin
// changes.
//
// EXPORTED because there is a SECOND place that builds a URL with this id:
// step 2 of the smoke test (cmd/zapgw/fumaca.go), which does GET
// /{phone_number_id}. A copy of the rule there would be exactly this
// project's mother trap — the same true sentence in one place and not the
// next, diverging at the first change.
//
// The rule is deliberately conservative: it does not assert that Meta's id
// is numeric (I didn't check that against the source), it just rejects
// whatever cannot safely become a path segment.
func PhoneNumberIDValid(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		acceptable := (r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			r == '_' || r == '-'
		if !acceptable {
			return false
		}
	}
	return true
}

// errWithoutDetail swaps the error for a marker of its CLASS, without the
// text.
//
// A transport error in Go is typically *url.Error, and its Error() carries
// the FULL URL — which here carries the client's phone_number_id. This text
// goes up to the log. Same decision (and same reason) as inbound's
// `motivoDoErro`.
func errWithoutDetail(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("prazo esgotado")
	case errors.Is(err, context.Canceled):
		return errors.New("cancelado")
	default:
		return errors.New("inalcancavel")
	}
}
