// Business profile in the Graph API: `GET`/`POST /{node}/whatsapp_business_profile`
// — what the customer sees when tapping the business name inside WhatsApp
// (T-155).
//
// 🔴 THE NODE IS NOT CONFIRMED AGAINST THE SOURCE, AND THAT'S WRITTEN OUT
// LOUD: the references consulted for this task DISAGREE on whether this
// endpoint hangs off `PHONE_NUMBER_ID` or `WABA_ID` — the wrong node
// returns `404`. This file treats the node as a plain parameter (`node`),
// exactly like `phoneNumberID` in client.go/media.go; WHO CHOOSES which
// instance field becomes `node` is a SINGLE function in
// internal/outbound/perfil_handler.go (profileNode) — never this file,
// which knows nothing about config.Instance. This is deliberate: if the
// choice turns out to be `WABA_ID`, the swap is ONE line in that function,
// not a sweep through this package. See its comment for the state of the
// doubt and for who's going to measure it against production.
//
// THE ACCEPTED FIELDS, and the CEILINGS Meta documents (third-party
// reference, NOT rechecked here — see ProfilePatch's comment): about (139
// char.), description (512 char.), address, email, websites (up to 2),
// vertical, profile_picture_handle. THIS GATEWAY DOES NOT MIRROR THE
// CEILINGS (T-143's decision, same as this task): Meta rejects and explains
// (`explicacao_meta`/`rastro_meta`, T-153) — duplicating the number here
// would only create a second source that would diverge from Meta the day it
// changes its own limit.
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

var (
	// ErrInvalidProfileNode: the identifier passed as `node` has a shape
	// that cannot safely become a URL segment — the SAME rule as
	// PhoneNumberIDValid (client.go), reused and not copied: it's about
	// the id's SHAPE (preventing `../` from escaping the Graph API's
	// version prefix), which applies equally to phone_number_id and to
	// waba_id.
	ErrInvalidProfileNode = errors.New("meta: identificador do perfil com forma invalida")

	// ErrProfileNotUnderstood: the GET response doesn't have the minimum
	// expected shape (missing `data` envelope, or the item isn't a JSON
	// object).
	ErrProfileNotUnderstood = errors.New("meta: perfil do negocio nao entendido")
)

// profileFields is the GET's `fields=`. Without it Meta can return a
// subset — the same trap as catalogFields in templates.go.
//
// ⚠️ Keep in sync with what profileFromResponse deserializes.
const profileFields = "about,address,description,email,profile_picture_url,websites,vertical"

// Profile is what the GET returns, FIELD BY FIELD as Meta sent it — no
// missing field is invented (see profileFromResponse). "Absent" and "came back
// empty" are different facts this type doesn't distinguish by construction
// (a zeroed `string` covers both); whoever needs the fine distinction is
// the WRITE side (ProfilePatch, below), not the read.
type Profile struct {
	About             string   `json:"about,omitempty"`
	Address           string   `json:"address,omitempty"`
	Description       string   `json:"description,omitempty"`
	Email             string   `json:"email,omitempty"`
	ProfilePictureURL string   `json:"profile_picture_url,omitempty"`
	Websites          []string `json:"websites,omitempty"`
	Vertical          string   `json:"vertical,omitempty"`
}

// ProfilePatch is the write body, in META'S OWN NAMES (not translated — the
// same decision as TemplateRequest.AllowCategoryChange in templates.go: a
// field passed through verbatim doesn't get a name of its own).
//
// A POINTER, ON PURPOSE — the SAME doctrine as config.Rotation
// (internal/config/store.go, "a pointer distinguishes DON'T TOUCH from
// REPLACE WITH THIS"): `nil` is "the consumer didn't send this field", and
// the gateway does NOT send the key to Meta — whatever value is already
// there survives. Swapping `*string` for `string` one day would ALWAYS send
// the key (even `""`), and a POST with only `about` would ERASE
// `description`/`address`/etc. — it's the trap this task exists to avoid
// repeating (see TestEscreverPerfilNaoApagaCampoAusente in
// perfil_test.go).
//
// `Websites` is `*[]string`: a NON-nil pointer to an EMPTY list is a
// legitimate request ("erase the sites"), different from `nil` ("don't
// touch") — the same distinction, one level down.
type ProfilePatch struct {
	About       *string   `json:"about,omitempty"`
	Address     *string   `json:"address,omitempty"`
	Description *string   `json:"description,omitempty"`
	Email       *string   `json:"email,omitempty"`
	Websites    *[]string `json:"websites,omitempty"`
	Vertical    *string   `json:"vertical,omitempty"`
	// ProfilePictureHandle is the `id` of a media file ALREADY UPLOADED via
	// POST /v1/media (the same media_id internal/outbound/media_handler.go
	// returns) — Meta swaps the profile picture for that upload's content.
	// This gateway doesn't validate the category here: whoever rejects a
	// handle that isn't an image is Meta itself.
	ProfilePictureHandle *string `json:"profile_picture_handle,omitempty"`
}

// ReadProfile reads `node`'s whatsapp_business_profile.
func (c *Client) ReadProfile(ctx context.Context, node, token string) (Profile, error) {
	if !PhoneNumberIDValid(node) {
		return Profile{}, ErrInvalidProfileNode
	}
	target, err := url.JoinPath(c.base, node, "whatsapp_business_profile")
	if err != nil {
		return Profile{}, fmt.Errorf("meta: montar url: %w", err)
	}
	q := url.Values{}
	q.Set("fields", profileFields)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"?"+q.Encode(), nil)
	if err != nil {
		return Profile{}, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return Profile{}, fmt.Errorf("meta: falha de transporte ao ler o perfil: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return Profile{}, fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return Profile{}, metaError
	}
	return profileFromResponse(raw)
}

// profileFromResponse deserializes the `{"data":[{...}]}` envelope documented
// for this endpoint.
//
// FIELD BY FIELD, NEVER A SINGLE UNMARSHAL STRAIGHT INTO THE STRUCT: a
// field with an unexpected type would zero the whole struct and take the
// profile down with it — the same trap already recorded in media.go
// (DescribeMedia) and templates.go (pageTemplates).
func profileFromResponse(raw []byte) (Profile, error) {
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Profile{}, fmt.Errorf("%w: corpo nao entendido", ErrProfileNotUnderstood)
	}
	// NOT VERIFIED AGAINST THE SOURCE that `data` always carries exactly
	// one item — only that it's the envelope's documented format. An empty
	// `data` returns Profile{} WITHOUT AN ERROR: an instance with no profile
	// field filled in is still a valid (empty) profile, not a read
	// failure.
	if len(envelope.Data) == 0 {
		return Profile{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data[0], &fields); err != nil || fields == nil {
		return Profile{}, fmt.Errorf("%w: item nao e um objeto JSON", ErrProfileNotUnderstood)
	}
	websites, err := textList(fields["websites"])
	if err != nil {
		return Profile{}, fmt.Errorf("%w: campo websites nao e uma lista", ErrProfileNotUnderstood)
	}
	return Profile{
		About:             textOf(fields["about"]),
		Address:           textOf(fields["address"]),
		Description:       textOf(fields["description"]),
		Email:             textOf(fields["email"]),
		ProfilePictureURL: textOf(fields["profile_picture_url"]),
		Websites:          websites,
		Vertical:          textOf(fields["vertical"]),
	}, nil
}

// textList deserializes an optional field that should be a list of
// strings. A missing field (empty `raw`) returns (nil, nil) — it's not an
// error, it's "Meta didn't send this field".
func textList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// WriteProfile sends ONLY the fields present in `p` — see ProfilePatch's
// comment: absent is "don't touch", never "erase".
//
// THE BODY IS BUILT BY EMBEDDING THE STRUCT, not by a map written by hand
// field by field (unlike CreateTemplate, in templates.go): each pointer's
// `omitempty` in ProfilePatch already does, by itself, the same thing a list
// of `if campo != nil { corpo[...] = *campo }` would do, and a hand-written
// list is one more copy of the field list to forget to keep in sync the day
// Meta gains a new field.
func (c *Client) WriteProfile(ctx context.Context, node, token string, p ProfilePatch) error {
	if !PhoneNumberIDValid(node) {
		return ErrInvalidProfileNode
	}

	bodyForMeta := struct {
		MessagingProduct string `json:"messaging_product"`
		ProfilePatch
	}{MessagingProduct: "whatsapp", ProfilePatch: p}
	raw, err := json.Marshal(bodyForMeta)
	if err != nil {
		return fmt.Errorf("meta: montar corpo do perfil: %w", err)
	}

	target, err := url.JoinPath(c.base, node, "whatsapp_business_profile")
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
		return fmt.Errorf("meta: falha de transporte ao escrever o perfil: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	rawResponse, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, rawResponse); metaError != nil {
		return metaError
	}
	// WE DO NOT REQUIRE `success:true` in the body — the same decision as
	// MarkAsRead (leitura.go): the documentation shows this field, but
	// there's no id at all to lose, and rejecting a 2xx without it would
	// reproduce the "2xx without an id" trap (ErrUploadWithoutID) over a field
	// that might not even always come.
	return nil
}
