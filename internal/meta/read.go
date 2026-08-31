// Marking a RECEIVED message as read — the blue check in the direction that
// was missing (T-075).
//
// The gateway always knew how to say that the CUSTOMER read what we sent: it's
// the `status: read` that arrives in the webhook and becomes an event
// (docs/CONTRATO-CONSUMIDOR.md). Telling the customer that WE read their
// message didn't exist — and the root cause isn't ours: with Baileys the
// marker went out by itself on receipt, and with the Cloud API it requires an
// explicit call. Every consumer migrating from Baileys hits this, and the
// symptom is the conversation staying at two gray checks forever while the
// operator is already working on the request.
package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// MarkAsRead tells Meta that message `wamid` was read by the business,
// and optionally turns on the "typing…" indicator in the same call.
//
// THE VERB WAS CHECKED AT THE SOURCE, and this isn't zeal: one of the two
// consumers' request cited `PUT /{phone-number-id}/messages`. Both official
// Meta pages, read on 2026-07-28, say POST — on the SAME path as sending:
//
//   - developers.facebook.com/docs/whatsapp/cloud-api/guides/mark-message-as-read
//   - developers.facebook.com/documentation/business-messaging/whatsapp/messages/mark-message-as-read
//
// Both show the same body: `{"messaging_product":"whatsapp",
// "status":"read","message_id":"wamid…"}`. Following the verb from the
// request would have produced a Graph API error that this project's suite
// CANNOT reach (it doesn't talk to Meta — see CLAUDE.md), so it would only
// have shown up against the real Meta.
//
// THE TYPING INDICATOR HAS NO ENDPOINT OF ITS OWN (T-147, owner's decision on
// 2026-08-20): the Cloud API fuses the two into the SAME POST, just adding the
// `typing_indicator` field to the read-receipt body (source:
// developers.facebook.com/docs/whatsapp/cloud-api/typing-indicators, read
// 2026-08-20):
//
//	{"messaging_product":"whatsapp","status":"read",
//	 "message_id":"<wamid>","typing_indicator":{"type":"text"}}
//
// Creating a separate call would make the gateway emit TWO POSTs for what
// Meta does in one.
//
// The `wamid` travels in the BODY, never in the URL. That's why it does NOT
// carry the guard that phone_number_id carries: PhoneNumberIDValid exists
// because url.JoinPath resolves `..` and a malformed id would escape the
// Graph API's version prefix. Whoever "unifies" the two validations one day:
// that's the difference, and it's deliberate.
//
// DOES NOT RETURN ANYTHING FROM THE SUCCESS BODY — just nil (marked) or the
// already-classified error. It's the same decision as CheckCredential, and
// here it's even cheaper: the doc shows `{"success": true}`, but there's no id
// to lose, so the defect that ErrResponseWithoutID names on send (a 2xx without an
// id becomes a record that LOOKS sent) has no equivalent in this call. Passing
// that body through would make the consumer depend on a Meta response schema
// that adds nothing to the HTTP status.
func (c *Client) MarkAsRead(ctx context.Context, phoneNumberID, token, wamid string, typing bool) error {
	if !PhoneNumberIDValid(phoneNumberID) {
		return ErrInvalidPhoneNumberID
	}

	// NON-REGRESSION: `MarkAsRead` is already in production (T-075). The
	// body WITHOUT `digitando` has to come out BYTE FOR BYTE identical to
	// today's — that's why the `typing_indicator` key only enters the map
	// when `digitando` is true, never as a present-but-empty field
	// (serializing a `map[string]any` with one key fewer produces a
	// different JSON, not a `null` field).
	body := map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        wamid,
	}
	if typing {
		body["typing_indicator"] = map[string]string{"type": "text"}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("meta: montar corpo da leitura: %w", err)
	}

	target, err := url.JoinPath(c.base, phoneNumberID, "messages")
	if err != nil {
		return fmt.Errorf("meta: montar url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// We do NOT interpolate the error: *url.Error carries the full URL,
		// and it carries the client's phone_number_id.
		return fmt.Errorf("meta: falha de transporte ao marcar como lida: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	rawResponse, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}

	// ClassifyResponse reads ONLY error.message and error.code — never the
	// rest of the body, which can echo client data.
	if metaError := ClassifyResponse(resp.StatusCode, rawResponse); metaError != nil {
		return metaError
	}
	return nil
}
