// Registration of the number and two-step verification PIN — POST
// <phone-number-id>/register, <phone-number-id>/deregister, and POST
// directly on <phone-number-id> with {"pin":...} (T-151, owner's decision,
// 2026-08-20: "only in provisioning").
//
// PROVISIONING ONLY, NEVER AN HTTP ROUTE: the three calls touch the number
// that is already LIVE on Meta — registering requires the two-step PIN,
// deregistering takes the number offline, and changing the PIN changes the
// key Meta uses to recognize the number's human operator. None of these is a
// messaging operation, and the registration endpoint (POST /v1/cadastro)
// isn't even reachable from outside the network (owner's decision,
// 2026-07-31) — exposing this over HTTP would open more surface than
// registration already opens today. Whoever calls these functions is the
// operations CLI (cmd/zapgw/provision.go), never a handler.
//
// 🔴 THE PIN OPERATION IS ONE-WAY. The Cloud API has NO endpoint to DISABLE
// two-step verification once registered — Meta's own doc says "Setting up
// two-factor authentication is a requirement to use the Cloud API." Once
// Register has accepted, the only way to change that is the WhatsApp
// Manager, outside this API. Whoever uses Register needs to know this
// BEFORE calling, not after — there is no "undo" here, nor anywhere else in
// this gateway.
//
// Sources read on 2026-08-20:
//
//	POST /{phone-number-id}/register     {"messaging_product":"whatsapp","pin":"<6 digits>"}
//	POST /{phone-number-id}/deregister   {"messaging_product":"whatsapp"} -> {"success":true}
//	POST /{phone-number-id}               {"pin":"<6 digits>"}   (sets/changes the PIN)
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
)

// ErrInvalidPin: the PIN doesn't have the shape Meta documents — exactly 6
// ASCII digits. Checked HERE, before any network call, so as not to spend a
// call (and a Meta error that might echo the rejected format) on a defect
// that can be caught without leaving the machine.
var ErrInvalidPin = errors.New("meta: pin invalido — a Meta exige exatamente 6 digitos")

// PinValid accepts ONLY 6 ASCII digits.
//
// This is NOT an attempt to guess a rule Meta didn't document: the source
// cited at the top of this file shows "<6 digits>" on all three endpoints,
// and that's the only verified form. A PIN that passes here can still be
// rejected by Meta for another reason (e.g. too repetitive a PIN) — this
// function only catches the format error, cheaper and faster than a round
// trip over the network.
func PinValid(pin string) bool {
	if len(pin) != 6 {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Register turns on two-step verification for the number with `pin`.
//
// THE PIN GOES IN THE BODY, NEVER IN THE URL: a query string leaks into
// proxy, server, and CDN logs. And it never enters an error returned from
// here — this function's errors (ErrInvalidPin, ErrInvalidPhoneNumberID,
// *MetaError) do not interpolate the PIN value.
func (c *Client) Register(ctx context.Context, phoneNumberID, token, pin string) error {
	if !PinValid(pin) {
		return ErrInvalidPin
	}
	return c.postRegistration(ctx, phoneNumberID, "register", map[string]any{
		"messaging_product": "whatsapp",
		"pin":               pin,
	}, token, "registrar")
}

// Deregister takes the number offline at Meta — after it, no message goes
// in or out through this phone_number_id until a new Register. The DECISION
// to require confirmation belongs to the caller (cmd/zapgw/provision.go,
// the same pattern as `instancia remover`); this client only talks to the
// Graph API.
func (c *Client) Deregister(ctx context.Context, phoneNumberID, token string) error {
	return c.postRegistration(ctx, phoneNumberID, "deregister", map[string]any{
		"messaging_product": "whatsapp",
	}, token, "desregistrar")
}

// SetPin changes the two-step verification PIN of a number that's
// ALREADY registered — POST directly on /{phone-number-id}, with no suffix
// (the SAME root that ObserveNumber reads, just as a POST here). Same
// caveat as Register: the PIN never goes into the URL or into an error.
func (c *Client) SetPin(ctx context.Context, phoneNumberID, token, pin string) error {
	if !PinValid(pin) {
		return ErrInvalidPin
	}
	if !PhoneNumberIDValid(phoneNumberID) {
		return ErrInvalidPhoneNumberID
	}
	target, err := url.JoinPath(c.base, phoneNumberID)
	if err != nil {
		return fmt.Errorf("meta: montar url: %w", err)
	}
	body, err := json.Marshal(map[string]any{"pin": pin})
	if err != nil {
		return fmt.Errorf("meta: montar corpo do pin: %w", err)
	}
	return c.sendRegistration(ctx, target, body, token, "trocar o pin")
}

// postRegistration is the shared body of Register and Deregister — same URL
// root (phone_number_id + suffix), only the body and the error label change.
// Two copies would diverge at the first suffix change — this project's
// mother trap.
func (c *Client) postRegistration(
	ctx context.Context, phoneNumberID, suffix string, bodyMap map[string]any, token, label string,
) error {
	if !PhoneNumberIDValid(phoneNumberID) {
		return ErrInvalidPhoneNumberID
	}
	target, err := url.JoinPath(c.base, phoneNumberID, suffix)
	if err != nil {
		return fmt.Errorf("meta: montar url: %w", err)
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("meta: montar corpo do %s: %w", label, err)
	}
	return c.sendRegistration(ctx, target, body, token, label)
}

// sendRegistration is the raw POST shared by the three calls in this file: it
// builds the request, sends it, classifies the response. It DOES NOT return
// anything from the success body (just nil or the already-classified error)
// — the same decision as CheckCredential: passing that body through would
// assert a Meta response schema that was only read in the doc, never
// measured in production.
func (c *Client) sendRegistration(ctx context.Context, target string, body []byte, token, label string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// We do NOT interpolate the error: *url.Error carries the full URL
		// (with the phone_number_id), but never the body — the PIN doesn't
		// leak through here.
		return fmt.Errorf("meta: falha de transporte ao %s: %w", label, errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return metaError
	}
	return nil
}
