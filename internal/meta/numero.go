// What the Graph API says about the NUMBER: quality and messaging limit
// (T-080).
//
// WHY THIS EXISTS, AND IT'S DEBT, NOT A FEATURE: until 2026-07-28 the
// consumer read these two values DIRECTLY from the Graph API, with their own
// token. The owner's rule — *nobody talks directly to Meta* (CLAUDE.md) —
// closed that path, and there was no replacement: their screen started
// saying "waiting for a gateway route". Closing the hatch before the door
// exists leaves someone locked outside, and this is the door.
//
// # THE SOURCE, CHECKED AGAINST META ON 2026-07-28 — AND THE CONSUMER WAS HALF WRONG
//
// They cited `GET /{waba-id}/phone_numbers` with the `messaging_limit_tier`
// field. Checked against Meta's doc, and both halves need a correction:
//
//   - THE FIELD `messaging_limit_tier` IS DEPRECATED. The messaging limits
//     page (developers.facebook.com/documentation/business-messaging/whatsapp/
//     messaging-limits, read on 2026-07-28) says, verbatim: "The
//     `messaging_limit_tier` field, which used to return a business phone
//     number's messaging limit, has been deprecated. Request the
//     `whatsapp_business_manager_messaging_limit` field instead." The SAME
//     page shows the call `https://graph.facebook.com/v25.0/{phone-number-id}
//     ?fields=whatsapp_business_manager_messaging_limit` and the response
//     `{"whatsapp_business_manager_messaging_limit": "TIER_250", "id": "..."}`.
//     That's why THIS is the name requested here — implementing what the
//     consumer cited would have been born deprecated on day 1.
//   - THE ENDPOINT DOESN'T HAVE TO BE THE WABA'S. `GET /{phone-number-id}`
//     answers both fields, and it's the call the watchdog ALREADY makes per
//     active instance (CheckCredential). Using the WABA's would open a
//     second network path, with a second way to fail, to answer what the
//     first already answers — and it would still return the list of ALL
//     numbers on the account, data about other numbers this gateway has no
//     use receiving.
//
// The lesson is worth recording because it nearly cost us on the very same
// day: two consumers requested `PUT` to mark a message as read, and Meta's
// doc said `POST`. **Consumer assertion is input, never fact.**
//
// # THE `fields=` IS NECESSARY FOR ONE OF THE TWO, AND ONLY ONE
//
// `quality_rating` comes in the STANDARD body of `GET /{phone-number-id}`
// (Meta's phone numbers reference page lists it among the fields the read
// returns without asking). `whatsapp_business_manager_messaging_limit` does
// NOT: the doc itself says "request the field". That's why this call exists
// separately from CheckCredential instead of just reading its body.
//
// # WHAT THIS FUNCTION DOES NOT DO: TRANSLATE
//
// "TIER_250" doesn't become 250, and "GREEN" doesn't become a number or a
// boolean. It's the same rule (and the same reason) as NumberQuality in
// types.go: translating requires a table of our own, and it breaks the day
// Meta invents a new value — breaking in the worst way, returning a
// plausible number for something no one checked.
package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// numberFields is the list requested in `fields=`.
//
// IT'S SHORT ON PURPOSE: every extra field is a name the Graph API can
// reject (a `fields=` with a nonexistent field answers 400/code 100, not 200
// with the field missing), and one such rejection takes down the read of the
// OTHER fields with it. Requesting `throughput`, `status`, or `name_status`
// "while we're at it" would put at risk the two fields someone actually
// reads.
const numberFields = "quality_rating,whatsapp_business_manager_messaging_limit"

// NumberObservation is what the Graph API answered about the number, RAW.
//
// An empty field means "Meta didn't send this field" — never "the value is
// empty" and never an invented value. Whoever writes it only writes what
// came in (see config.UpdateNumberAtMeta): a missing field CANNOT erase a
// previous observation that was good.
type NumberObservation struct {
	// Quality is `quality_rating` — "GREEN", "YELLOW", "RED", "UNKNOWN"...
	// LITERAL, always. The gateway does not order these words and does not
	// derive "bad" from any of them: ordering a third party's vocabulary
	// requires knowing the whole list, and no one here knows it (same
	// decision as AccountAlert.Severity).
	Quality string
	// Limit is `whatsapp_business_manager_messaging_limit` — "TIER_250",
	// "TIER_1K"... LITERAL, always. See the top of this file for why it is
	// NOT the `messaging_limit_tier` the consumer cited.
	Limit string
}

// Empty says that Meta answered without EITHER of the two fields. The
// caller has nothing to write — and writing empty over what was already
// there would trade a good observation for nothing.
func (o NumberObservation) Empty() bool { return o.Quality == "" && o.Limit == "" }

// ObserveNumber asks the Graph API for this number's quality and messaging
// limit.
//
// THE ERROR COMES BACK ALREADY CLASSIFIED (ClassifyResponse), same as
// CheckCredential — but the caller CANNOT treat every error from here as
// a credential verdict. See Watchdog.checkOne: a rejected `fields=` answers
// 400 (permanent class), which is indistinguishable, in this taxonomy, from
// "Meta rejected it for good." Letting that become `recusado` would paint a
// perfectly good token red just because we asked for one extra field.
//
// The token goes in the HEADER, never in the URL: a token in a query string
// leaks into proxy, server, and CDN logs. The `fields=` goes in the query,
// which is the only place the Graph API accepts it — and it isn't a secret.
func (c *Client) ObserveNumber(ctx context.Context, phoneNumberID, token string) (NumberObservation, error) {
	// THE SAME guard as sending and checking, through the same function
	// (not a copy): url.JoinPath resolves `..` like path.Join, so an id
	// with `../` would escape the Graph API's version prefix and point to
	// another endpoint.
	if !PhoneNumberIDValid(phoneNumberID) {
		return NumberObservation{}, ErrInvalidPhoneNumberID
	}
	target, err := url.JoinPath(c.base, phoneNumberID)
	if err != nil {
		return NumberObservation{}, fmt.Errorf("meta: montar url: %w", err)
	}
	target += "?" + url.Values{"fields": {numberFields}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return NumberObservation{}, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return NumberObservation{}, fmt.Errorf("meta: falha de transporte ao observar o numero: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return NumberObservation{}, fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return NumberObservation{}, metaError
	}
	return numberObservation(raw), nil
}

// numberObservation reads the two fields with the SAME tolerance as the
// webhook parser: a missing field, `null`, or an unexpected type become ""
// instead of taking down the read of the other field.
//
// WHY json.RawMessage AND NOT `string` DIRECTLY: with `string`, a single
// field arriving as a number (Meta already sends `entity_id` that way in
// account_alerts) would make the json.Unmarshal of the WHOLE ENVELOPE fail,
// and both fields would be lost together. This file prefers to lose one
// over losing both.
func numberObservation(raw []byte) NumberObservation {
	var envelope struct {
		Quality json.RawMessage `json:"quality_rating"`
		Limit   json.RawMessage `json:"whatsapp_business_manager_messaging_limit"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return NumberObservation{}
	}
	quality, _ := messageBlock[string](envelope.Quality)
	limit, _ := messageBlock[string](envelope.Limit)
	return NumberObservation{Quality: quality, Limit: limit}
}
