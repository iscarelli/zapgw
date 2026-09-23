// What the Graph API says about the ACCOUNT's ability to send, beyond the
// token (T-254).
//
// WHY THIS EXISTS: on 2026-09-21 the Meta Graph API refused a template send
// with error 131042 ("the WABA has no payment method on file"), and the
// consumer only found out through a real customer's failed send.
// CheckCredential (client.go) and ObserveNumber (number.go) both already ask
// Meta about THIS instance — but neither one answers "is the account itself
// fit to send". CheckCredential only proves the TOKEN is still accepted;
// ObserveNumber only reads the number's quality and messaging limit tier.
// This file is the third, narrower question: Meta's own verdict on whether
// sending would even be attempted.
//
// TWO SEPARATE CALLS, because the two fields live on TWO DIFFERENT Graph
// nodes: `health_status` exists on BOTH the WABA and the phone number, but
// `primary_funding_id` (whether a payment method is on file) only exists on
// the WABA. Asking the WABA for BOTH and the number for only `health_status`
// keeps every request field one that node actually answers — a `fields=`
// naming something a node does not have answers 400, taking the WHOLE
// response down with it (same lesson as numberFields in number.go).
package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// accountHealthFields is what ObservePhoneHealth requests: the phone number
// node does not have `primary_funding_id` — asking for it there would risk
// a 400 that erases `health_status` along with it.
const accountHealthFields = "health_status"

// accountHealthFieldsWithFunding is what ObserveWABAHealth requests — the
// WABA node is the ONLY one of the two with a funding source at all.
const accountHealthFieldsWithFunding = "health_status,primary_funding_id"

// AccountHealthEntity is one item of `health_status.entities`, as Meta sends
// it — WITHOUT the `id` key Meta includes on every entity. T-254 decided
// that a third party's Meta id never leaves this gateway on this route: the
// caller already knows which id it asked about (it chose the URL), and
// parsing the id again here buys nothing while adding a value that could
// leak into a log or a response nobody reviewed.
type AccountHealthEntity struct {
	// EntityType is `entity_type` — LITERAL, always ("WABA",
	// "PHONE_NUMBER", or anything else Meta sends). Not translated, same
	// discipline as NumberObservation.Quality: ordering or renaming a third
	// party's vocabulary requires knowing the whole list, and nobody here
	// does.
	EntityType string
	// CanSendMessage is THIS entity's own `can_send_message` — LITERAL,
	// always. "" means Meta did not send it for this entity.
	CanSendMessage string
	// Errors is `errors` — empty (not nil-vs-empty distinguished) when Meta
	// sent none.
	Errors []AccountHealthEntityError
	// AdditionalInfo is `additional_info`, as RAW strings. Meta documents it
	// as free-form diagnostic text, not a closed vocabulary — there is
	// nothing here to translate.
	AdditionalInfo []string
}

// AccountHealthEntityError is one item of an entity's `errors` list.
type AccountHealthEntityError struct {
	// Code is `error_code` — kept numeric with the SAME tolerance as
	// MetaError.MetaCode (tolerantInt): this API already sends codes as
	// either a number or a numeric string elsewhere.
	Code int
	// Description is `error_description` — Meta's own text.
	Description string
	// PossibleSolution is `possible_solution` — Meta's own suggested fix,
	// written to be shown to a human (same class as MetaError.Explanation).
	PossibleSolution string
}

// AccountHealthObservation is what ONE call (the WABA's or the phone
// number's) answered.
type AccountHealthObservation struct {
	// CanSendMessage is the TOP of health_status —
	// `health_status.can_send_message`. LITERAL, always. "" means Meta
	// answered without it (or without `health_status` at all) — the CALLER
	// decides what that means (internal/outbound/account_health_handler.go
	// never lets it become AVAILABLE by omission).
	CanSendMessage string
	// Entities is `health_status.entities`.
	Entities []AccountHealthEntity
	// HasFundingID reports whether `primary_funding_id` came back
	// NON-EMPTY. Only ObserveWABAHealth can ever set this true —
	// ObservePhoneHealth never requests the field, so it is always false
	// there. 🔴 THE VALUE of primary_funding_id is NEVER kept anywhere past
	// this parse: it is read straight into this bool and discarded — it
	// does not survive in this struct, in a log, or in an error.
	HasFundingID bool
}

// ObserveWABAHealth asks the WABA node for `health_status` AND
// `primary_funding_id` — the ONLY of the two calls that can answer whether a
// payment method is on file.
//
// The token goes in the HEADER, never in the URL, same reason as
// CheckCredential and ObserveNumber.
func (c *Client) ObserveWABAHealth(ctx context.Context, wabaID, token string) (AccountHealthObservation, error) {
	return c.observeAccountHealth(ctx, wabaID, token, accountHealthFieldsWithFunding)
}

// ObservePhoneHealth asks the phone number node for `health_status` only —
// see accountHealthFields for why `primary_funding_id` is never requested
// here.
func (c *Client) ObservePhoneHealth(ctx context.Context, phoneNumberID, token string) (AccountHealthObservation, error) {
	return c.observeAccountHealth(ctx, phoneNumberID, token, accountHealthFields)
}

// observeAccountHealth is the ONE function both exported calls above go
// through — a second copy of the URL-building and error-classification
// would be this project's mother trap (see the comment on CheckCredential).
func (c *Client) observeAccountHealth(ctx context.Context, id, token, fields string) (AccountHealthObservation, error) {
	// THE SAME guard as sending, checking, and observing the number,
	// through the SAME function (not a copy): url.JoinPath resolves `..`
	// like path.Join, so an id with `../` would escape the Graph API's
	// version prefix and point to another endpoint. PhoneNumberIDValid's
	// name predates this call, but its rule (letters, digits, `_`, `-`) is
	// not specific to a phone number — it is what makes ANY Meta id safe as
	// a URL segment, and a waba_id has the same shape.
	if !PhoneNumberIDValid(id) {
		return AccountHealthObservation{}, ErrInvalidPhoneNumberID
	}
	target, err := url.JoinPath(c.base, id)
	if err != nil {
		return AccountHealthObservation{}, fmt.Errorf("meta: build url: %w", err)
	}
	target += "?" + url.Values{"fields": {fields}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return AccountHealthObservation{}, fmt.Errorf("meta: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return AccountHealthObservation{}, fmt.Errorf("meta: transport failure while observing account health: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return AccountHealthObservation{}, fmt.Errorf("meta: read response: %w", errWithoutDetail(err))
	}
	// ClassifyResponse reads ONLY error.message and error.code (plus the
	// four T-153 keys) — never the rest of the body, which can echo client
	// data.
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return AccountHealthObservation{}, metaError
	}
	return accountHealthObservation(raw), nil
}

// accountHealthObservation reads the envelope with the SAME per-field
// tolerance as numberObservation and ClassifyResponse: one malformed field,
// or one malformed entity, or one malformed error inside an entity, loses
// ONLY itself — never the rest of the response. A single atomic Unmarshal
// over the whole nested shape would fail the WHOLE call on Meta sending one
// unexpected type anywhere in it, which is exactly the defect this
// project's webhook parser already paid a Critical for once
// (docs/ARMADILHAS.md).
func accountHealthObservation(raw []byte) AccountHealthObservation {
	var envelope struct {
		HealthStatus json.RawMessage `json:"health_status"`
		FundingID    json.RawMessage `json:"primary_funding_id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return AccountHealthObservation{}
	}

	var obs AccountHealthObservation
	if funding, _ := messageBlock[string](envelope.FundingID); strings.TrimSpace(funding) != "" {
		obs.HasFundingID = true
	}
	// The VALUE of `funding` above is discarded here, on purpose: it never
	// gets assigned to any field of AccountHealthObservation. See
	// HasFundingID's comment.

	if len(envelope.HealthStatus) == 0 {
		return obs
	}
	var status struct {
		CanSendMessage json.RawMessage   `json:"can_send_message"`
		Entities       []json.RawMessage `json:"entities"`
	}
	if json.Unmarshal(envelope.HealthStatus, &status) != nil {
		return obs
	}
	obs.CanSendMessage, _ = messageBlock[string](status.CanSendMessage)

	for _, rawEntity := range status.Entities {
		var e struct {
			EntityType     json.RawMessage   `json:"entity_type"`
			CanSendMessage json.RawMessage   `json:"can_send_message"`
			AdditionalInfo []json.RawMessage `json:"additional_info"`
			Errors         []json.RawMessage `json:"errors"`
		}
		if json.Unmarshal(rawEntity, &e) != nil {
			// ONE malformed entity does not take the others down with it.
			continue
		}
		entity := AccountHealthEntity{}
		entity.EntityType, _ = messageBlock[string](e.EntityType)
		entity.CanSendMessage, _ = messageBlock[string](e.CanSendMessage)
		for _, rawInfo := range e.AdditionalInfo {
			if s, state := messageBlock[string](rawInfo); state == blockRead {
				entity.AdditionalInfo = append(entity.AdditionalInfo, s)
			}
		}
		for _, rawErr := range e.Errors {
			var er struct {
				Code             json.RawMessage `json:"error_code"`
				Description      json.RawMessage `json:"error_description"`
				PossibleSolution json.RawMessage `json:"possible_solution"`
			}
			if json.Unmarshal(rawErr, &er) != nil {
				continue
			}
			description, _ := messageBlock[string](er.Description)
			solution, _ := messageBlock[string](er.PossibleSolution)
			entity.Errors = append(entity.Errors, AccountHealthEntityError{
				Code:             tolerantInt(er.Code),
				Description:      description,
				PossibleSolution: solution,
			})
		}
		obs.Entities = append(obs.Entities, entity)
	}
	return obs
}
