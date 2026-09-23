package meta

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// T-255: each of the three calls asks for EXACTLY ONE field — combining
// health_status and primary_funding_id into one WABA call (T-254's original
// shape) meant a refusal on either field erased the other; splitting them
// means a refusal on one never blinds the caller to the other.
func TestObserveWABAHealthAsksOnlyForHealthStatus(t *testing.T) {
	c, urls, authorizations := respondingGraph(t, http.StatusOK, `{"health_status":{"can_send_message":"AVAILABLE","entities":[]}}`)

	if _, err := c.ObserveWABAHealth(context.Background(), "WABAID1", "token-secreto"); err != nil {
		t.Fatalf("ObserveWABAHealth: %v", err)
	}
	url := (*urls)[0]
	if !strings.Contains(url, "health_status") {
		t.Errorf("the URL %q does not request health_status", url)
	}
	if strings.Contains(url, "primary_funding_id") {
		t.Errorf("the URL %q requests primary_funding_id — that is now ObserveWABAFunding's own call (T-255)", url)
	}
	if strings.Contains(url, "token-secreto") {
		t.Errorf("the token leaked into the URL: %q", url)
	}
	if (*authorizations)[0] != "Bearer token-secreto" {
		t.Errorf("Authorization = %q", (*authorizations)[0])
	}
}

func TestObservePhoneHealthAsksOnlyForHealthStatus(t *testing.T) {
	c, urls, _ := respondingGraph(t, http.StatusOK, `{"health_status":{"can_send_message":"AVAILABLE","entities":[]}}`)

	if _, err := c.ObservePhoneHealth(context.Background(), "PNID1", "t"); err != nil {
		t.Fatalf("ObservePhoneHealth: %v", err)
	}
	url := (*urls)[0]
	if !strings.Contains(url, "health_status") {
		t.Errorf("the URL %q does not request health_status", url)
	}
	if strings.Contains(url, "primary_funding_id") {
		t.Errorf("the URL %q requests primary_funding_id — the phone number node does not have it, "+
			"and asking risks a 400 that erases health_status along with it", url)
	}
}

// ObserveWABAFunding is the split-out third call (T-255): it asks the WABA
// node for primary_funding_id ONLY, never bundled with health_status.
func TestObserveWABAFundingAsksOnlyForPrimaryFundingID(t *testing.T) {
	c, urls, authorizations := respondingGraph(t, http.StatusOK, `{"primary_funding_id":"1234567890123"}`)

	if _, err := c.ObserveWABAFunding(context.Background(), "WABAID1", "token-secreto"); err != nil {
		t.Fatalf("ObserveWABAFunding: %v", err)
	}
	url := (*urls)[0]
	if !strings.Contains(url, "primary_funding_id") {
		t.Errorf("the URL %q does not request primary_funding_id", url)
	}
	if strings.Contains(url, "health_status") {
		t.Errorf("the URL %q requests health_status — that is ObserveWABAHealth's own call (T-255)", url)
	}
	if strings.Contains(url, "token-secreto") {
		t.Errorf("the token leaked into the URL: %q", url)
	}
	if (*authorizations)[0] != "Bearer token-secreto" {
		t.Errorf("Authorization = %q", (*authorizations)[0])
	}
}

// The full shape the Do section of T-254 describes, checked against a
// REALISTIC body: top-level can_send_message, an entity with its OWN
// can_send_message, an error with all three of its fields, and
// additional_info. Every value survives LITERAL, same discipline as
// NumberObservation. T-255 split this call away from primary_funding_id —
// see TestAccountHealthFundingKeepsLiteralValue below for that field.
func TestAccountHealthKeepsLiteralValues(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK, `{
		"health_status": {
			"can_send_message": "LIMITED",
			"entities": [
				{
					"entity_type": "WABA",
					"id": "the-real-waba-id-must-not-leak",
					"can_send_message": "LIMITED",
					"errors": [
						{"error_code": 139012, "error_description": "quality dropped",
						 "possible_solution": "wait for quality to recover"}
					],
					"additional_info": ["informational note"]
				}
			]
		}
	}`)

	obs, err := c.ObserveWABAHealth(context.Background(), "WABAID1", "t")
	if err != nil {
		t.Fatalf("ObserveWABAHealth: %v", err)
	}
	if obs.CanSendMessage != "LIMITED" {
		t.Errorf("CanSendMessage = %q, want the LITERAL %q", obs.CanSendMessage, "LIMITED")
	}
	if obs.HasFundingID {
		t.Error("HasFundingID = true, want false — this call never requests primary_funding_id (T-255)")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Entities = %d, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.EntityType != "WABA" {
		t.Errorf("EntityType = %q, want %q", e.EntityType, "WABA")
	}
	if e.CanSendMessage != "LIMITED" {
		t.Errorf("Entity CanSendMessage = %q, want %q", e.CanSendMessage, "LIMITED")
	}
	if len(e.Errors) != 1 {
		t.Fatalf("Errors = %d, want 1", len(e.Errors))
	}
	if e.Errors[0].Code != 139012 || e.Errors[0].Description != "quality dropped" ||
		e.Errors[0].PossibleSolution != "wait for quality to recover" {
		t.Errorf("Errors[0] = %+v, want the LITERAL Meta fields", e.Errors[0])
	}
	if len(e.AdditionalInfo) != 1 || e.AdditionalInfo[0] != "informational note" {
		t.Errorf("AdditionalInfo = %v", e.AdditionalInfo)
	}
}

// ObserveWABAFunding's own literal check (T-255): the split-out call reads
// primary_funding_id correctly and does NOT invent a health_status it was
// never asked for.
func TestAccountHealthFundingKeepsLiteralValue(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK, `{"primary_funding_id":"1234567890123"}`)

	obs, err := c.ObserveWABAFunding(context.Background(), "WABAID1", "t")
	if err != nil {
		t.Fatalf("ObserveWABAFunding: %v", err)
	}
	if !obs.HasFundingID {
		t.Error("HasFundingID = false, want true — primary_funding_id came back non-empty")
	}
	if obs.CanSendMessage != "" || len(obs.Entities) != 0 {
		t.Errorf("obs = %+v, want CanSendMessage/Entities empty — this call never asked for health_status", obs)
	}
}

// primary_funding_id ABSENT is the same "no payment method on file" as
// primary_funding_id being an empty string — both read as HasFundingID ==
// false, never true by omission.
func TestAccountHealthFundingIDAbsentOrEmptyIsFalse(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"primary_funding_id":""}`,
	} {
		c, _, _ := respondingGraph(t, http.StatusOK, body)
		obs, err := c.ObserveWABAFunding(context.Background(), "WABAID1", "t")
		if err != nil {
			t.Fatalf("ObserveWABAFunding: %v", err)
		}
		if obs.HasFundingID {
			t.Errorf("body %q: HasFundingID = true, want false", body)
		}
	}
}

// A single field inside ONE entity coming in an unexpected type loses ONLY
// that field, never the whole entity and never the OTHER entity — same
// discipline as ClassifyResponse and numberObservation: an atomic Unmarshal
// over the whole nested shape would fail (and lose everything) on Meta
// sending one unexpected type anywhere in it.
func TestAccountHealthLosesONLYTheUnreadableField(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK, `{
		"health_status": {
			"can_send_message": "AVAILABLE",
			"entities": [
				{"entity_type": {"this": "is not a string"}, "can_send_message": "AVAILABLE"},
				{"entity_type": "PHONE_NUMBER", "can_send_message": "AVAILABLE"}
			]
		}
	}`)

	obs, err := c.ObserveWABAHealth(context.Background(), "WABAID1", "t")
	if err != nil {
		t.Fatalf("ObserveWABAHealth: %v", err)
	}
	if obs.CanSendMessage != "AVAILABLE" {
		t.Errorf("CanSendMessage = %q, want %q — the top-level field must survive a bad entity", obs.CanSendMessage, "AVAILABLE")
	}
	if len(obs.Entities) != 2 {
		t.Fatalf("Entities = %d, want 2 — a field that cannot be read becomes absence, never a dropped entity", len(obs.Entities))
	}
	if obs.Entities[0].EntityType != "" {
		t.Errorf("Entities[0].EntityType = %q, want \"\" — the unreadable field must become absence, never invention", obs.Entities[0].EntityType)
	}
	if obs.Entities[1].EntityType != "PHONE_NUMBER" {
		t.Errorf("Entities[1].EntityType = %q, want %q", obs.Entities[1].EntityType, "PHONE_NUMBER")
	}
}

// An entity item that is not even a JSON OBJECT (a bare string, here) is
// dropped entirely — this is the one shape that genuinely cannot be
// unmarshaled into the entity struct, unlike a single wrong-typed field
// inside an otherwise valid object (the case above).
func TestAccountHealthDropsAnEntityThatIsNotAnObjectAtAll(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK, `{
		"health_status": {
			"can_send_message": "AVAILABLE",
			"entities": [
				"not-an-object-at-all",
				{"entity_type": "PHONE_NUMBER", "can_send_message": "AVAILABLE"}
			]
		}
	}`)

	obs, err := c.ObserveWABAHealth(context.Background(), "WABAID1", "t")
	if err != nil {
		t.Fatalf("ObserveWABAHealth: %v", err)
	}
	if len(obs.Entities) != 1 || obs.Entities[0].EntityType != "PHONE_NUMBER" {
		t.Errorf("Entities = %+v, want only the readable PHONE_NUMBER entity", obs.Entities)
	}
}

// A 2xx response WITHOUT health_status is an EMPTY observation (CanSendMessage
// == ""), never an invented value — the CALLER (the outbound handler)
// decides that "" never becomes AVAILABLE by omission.
func TestAccountHealthWithoutHealthStatusIsEmpty(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusOK, `{"id":"WABAID1"}`)

	obs, err := c.ObserveWABAHealth(context.Background(), "WABAID1", "t")
	if err != nil {
		t.Fatalf("ObserveWABAHealth: %v", err)
	}
	if obs.CanSendMessage != "" || len(obs.Entities) != 0 || obs.HasFundingID {
		t.Errorf("obs = %+v, want the zero value", obs)
	}
}

// The error comes back ALREADY CLASSIFIED, same as CheckCredential and
// ObserveNumber.
func TestAccountHealthReturnsAClassifiedError(t *testing.T) {
	c, _, _ := respondingGraph(t, http.StatusUnauthorized,
		`{"error":{"message":"Invalid OAuth access token","code":190}}`)

	_, err := c.ObserveWABAHealth(context.Background(), "WABAID1", "t")
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want *MetaError", err)
	}
	if me.Class != ClassConfig {
		t.Errorf("class = %q, want %q", me.Class, ClassConfig)
	}
}

// THE SAME guard as sending, checking, and observing the number: an id with
// `../` would escape the Graph API's version prefix.
func TestObserveWABAHealthRefusesAnInvalidID(t *testing.T) {
	c, urls, _ := respondingGraph(t, http.StatusOK, `{}`)

	_, err := c.ObserveWABAHealth(context.Background(), "../../me", "t")
	if !errors.Is(err, ErrInvalidPhoneNumberID) {
		t.Fatalf("err = %v, want ErrInvalidPhoneNumberID", err)
	}
	if len(*urls) != 0 {
		t.Errorf("the invalid id reached the network: %v", *urls)
	}
}
