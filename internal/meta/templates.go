// WABA's template catalog: PAGINATED reading, creation and deletion by name.
//
// THE TRAP THIS FILE EXISTS TO NOT REPEAT: this network's old gateway only
// returned the FIRST 25 templates, and a real system has 84. That's what
// took it out of production. The truncation gives no error at all — it
// returns a plausible, short list, the consumer concludes the template
// "doesn't exist", and the message simply never goes out.
//
// That's why the read follows `paging.next` UNTIL IT DISAPPEARS, and the
// page ceiling that prevents an infinite loop ERRORS when it's exceeded.
// Returning the partial list when the ceiling is exceeded would be
// reinventing the same trap with a different number.
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
	"strconv"
	"strings"
)

var (
	// ErrInvalidWabaID: the WABA identifier has a shape that cannot
	// safely become a URL segment.
	ErrInvalidWabaID = errors.New("meta: waba_id com forma invalida")

	// ErrIncompleteCatalog: pagination exceeded the page ceiling.
	//
	// It IS an error, never a partial list: a short list returned as if
	// it were complete is exactly the defect that took the old gateway
	// out of production.
	ErrIncompleteCatalog = errors.New("meta: o catalogo de templates nao coube no teto de paginas; a lista seria INCOMPLETA")

	// ErrPageFromAnotherOrigin: the response's `paging.next` points outside
	// the configured Graph API.
	ErrPageFromAnotherOrigin = errors.New("meta: paging.next aponta para outra origem")

	// ErrCatalogNotUnderstood: the page (or one of its items) doesn't
	// have a template's minimum shape.
	ErrCatalogNotUnderstood = errors.New("meta: pagina do catalogo de templates nao entendida")

	// ErrTemplateWithoutID: Meta answered the creation with 2xx but no
	// template id.
	ErrTemplateWithoutID = errors.New("meta: resposta 2xx sem id de template")

	// ErrDeletionNotConfirmed: Meta answered the deletion with 2xx but
	// did NOT say `success: true`.
	//
	// It is the SAME trap as ErrTemplateWithoutID, on the other verb: a status
	// < 400 does not prove the deletion happened. The documented body of
	// this edge is `{"success": bool}`, so `false` — and the absence of the
	// field, which `encoding/json` turns into the zero value without any
	// error (docs/ARMADILHAS.md, "Go / JSON") — have to come out as an
	// error. Reporting "deleted" over a body that did not say so is the
	// worst outcome available here: the consumer crosses the name off its
	// cleanup list and the template stays on the account, invisible until
	// somebody counts the catalog again.
	ErrDeletionNotConfirmed = errors.New("meta: resposta 2xx sem success:true ao apagar template")
)

const (
	// itemsPerPage is what we ask Meta for per page.
	itemsPerPage = 100

	// pageCap exists ONLY so the loop doesn't spin forever.
	pageCap = 50

	// catalogBodyCap is larger than other responses' because a page of
	// 100 templates with components is big.
	catalogBodyCap = 8 << 20
)

// Template is what the consumer receives per catalog item.
//
// `ID` was added in T-078 because RE-READING the catalog after a creation
// that ended without a response needs to answer with the template's id —
// the same id the creation would have returned. Without it, the gateway
// knows how to say "it exists" and doesn't know how to say "it's this one".
//
// It's `omitempty` and its ABSENCE is NOT an error, unlike `Name`: it
// wasn't verified against Meta's source that every catalog page carries
// `id`, and taking down the WHOLE catalog read for an unconfirmed field
// would trade a useful catalog for none — which is the expensive version of
// the same mistake. Whoever depends on the id handles the empty case
// explicitly (see respondAmbiguousOutcome in
// internal/outbound/templates_handler.go).
type Template struct {
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"nome"`
	Status     string          `json:"status"`
	Category   string          `json:"categoria"`
	Language   string          `json:"idioma"`
	Components json.RawMessage `json:"componentes,omitempty"`

	// Reason is Meta's `rejected_reason`, PASSED THROUGH AS IT CAME — the
	// SAME word and the SAME doctrine as TemplateStatus.Reason
	// (internal/meta/types.go:225), for the SAME fact: the literal string
	// "NONE" is the NORMAL value when there's no reason and passes
	// through intact; only the field's real ABSENCE disappears from the
	// JSON, via omitempty. "Meta said NONE" and "Meta didn't send the
	// field" are different facts (T-116, prompted by consumer-b: two
	// rejected-template attempts with no information at all about why).
	//
	// No empty check, unlike Name: same reason as ID above — it wasn't
	// verified against the source that EVERY catalog item carries
	// `rejected_reason`, and taking down the whole read for an
	// unconfirmed accessory field would trade a useful catalog for none.
	Reason string `json:"motivo,omitempty"`
}

// TemplateRequest is the creation, in this gateway's names.
type TemplateRequest struct {
	Name       string
	Category   string
	Language   string
	Components json.RawMessage
	// AllowCategoryChange is PASSED THROUGH VERBATIM to Meta's
	// `allow_category_change` — the gateway doesn't validate, interpret,
	// or translate it. A pointer, not `bool`, BY CONSTRUCTION (T-108):
	// `nil` is the only way for the field to not go into the request
	// body. With a plain `bool`, EVERY consumer that today doesn't send
	// the field would start sending `false` to Meta without having asked
	// for anything — it would change what the gateway says to Meta for
	// EVERYONE, silently. See CreateTemplate, where the `nil` decides
	// whether the key enters the map.
	AllowCategoryChange *bool
}

// CreatedTemplate is Meta's verdict on the creation.
type CreatedTemplate struct {
	ID       string
	Status   string
	Category string
}

// ListTemplates returns the WABA's WHOLE catalog, following
// `paging.next` until it disappears.
//
// `status` filters (e.g. "APPROVED") and is optional. It goes to Meta in
// the query AND is applied here again, on purpose: it wasn't verified
// against the source that it honors the parameter, and promising
// "APPROVED" while returning the whole catalog would make the consumer
// send a REJECTED template and take an error to the end customer's face.
// Filtering twice costs nothing; filtering zero times costs a message that
// never goes out.
//
// Returns `nil` TOGETHER with any error. A partial list + error would try
// the same tragedy as the 25: someone, someday, would use the list
// "because something came back".
func (c *Client) ListTemplates(ctx context.Context, wabaID, token, status string) ([]Template, error) {
	if !PhoneNumberIDValid(wabaID) {
		// THE SAME guard as sending, and the same function (not a copy):
		// url.JoinPath resolves `..` like path.Join, so an id with `../`
		// escapes the Graph API's version prefix and points to another
		// endpoint.
		return nil, ErrInvalidWabaID
	}
	target, err := url.JoinPath(c.base, wabaID, "message_templates")
	if err != nil {
		return nil, fmt.Errorf("meta: montar url: %w", err)
	}

	// Uppercase before sending: if Meta compares the status
	// case-sensitively, an "approved" coming from the consumer would come
	// back as an EMPTY catalog — which is this trap's short list in its
	// extreme form.
	status = strings.ToUpper(strings.TrimSpace(status))
	// Fields we want from the catalog. Without it, Meta returns a subset
	// (id, name, status, category, language) and omits accessory fields
	// like `rejected_reason` — which is what matters on a REJECTED
	// template.
	//
	// ⚠️ Keep in sync with what pageTemplates deserializes: if Meta
	// starts returning a new field we want, it has to enter here AND in
	// the deserialization `struct` (today: id, name, status, category,
	// language, components, rejected_reason).
	const catalogFields = "id,name,status,category,language,components,rejected_reason"

	q := url.Values{}
	q.Set("limit", strconv.Itoa(itemsPerPage))
	q.Set("fields", catalogFields)
	if status != "" {
		q.Set("status", status)
	}

	var all []Template
	next := target + "?" + q.Encode()
	for page := 0; next != ""; page++ {
		if page >= pageCap {
			// EXCEEDING THE CEILING IS AN ERROR. The alternative —
			// returning what's already there — is the 25-item truncation
			// with a different number, and this time with the
			// appearance of a deliberate decision.
			return nil, fmt.Errorf("%w (teto de %d paginas, %d templates lidos ate aqui)",
				ErrIncompleteCatalog, pageCap, len(all))
		}
		items, following, err := c.templatePage(ctx, next, token)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if following != "" {
			if err := sameGraphOrigin(c.base, following); err != nil {
				return nil, err
			}
		}
		next = following
	}

	if status == "" {
		return all, nil
	}
	kept := make([]Template, 0, len(all))
	for _, t := range all {
		if strings.EqualFold(t.Status, status) {
			kept = append(kept, t)
		}
	}
	return kept, nil
}

// templatePage fetches ONE page and returns the items and
// `paging.next`.
func (c *Client) templatePage(ctx context.Context, target, token string) ([]Template, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", fmt.Errorf("meta: montar requisicao: %w", err)
	}
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("meta: falha de transporte ao listar templates: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, catalogBodyCap))
	if err != nil {
		return nil, "", fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return nil, "", metaError
	}
	return pageTemplates(raw)
}

// pageTemplates deserializes one page.
//
// A MALFORMED ITEM IS AN ERROR, NEVER A SKIP: skipping is truncation with a
// different name, and the outcome is the same as the 25-item trap — a
// short, plausible list, with no error. (Unlike the webhook parser, where
// skipping the bad item PRESERVES the good ones from another tenant; here
// skipping DESTROYS the endpoint's only guarantee.)
func pageTemplates(raw []byte) ([]Template, string, error) {
	var envelope struct {
		Data   []json.RawMessage `json:"data"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, "", fmt.Errorf("%w: corpo nao entendido", ErrCatalogNotUnderstood)
	}

	items := make([]Template, 0, len(envelope.Data))
	for i, payload := range envelope.Data {
		var t struct {
			ID             string          `json:"id"`
			Name           string          `json:"name"`
			Status         string          `json:"status"`
			Category       string          `json:"category"`
			Language       string          `json:"language"`
			Components     json.RawMessage `json:"components"`
			RejectedReason string          `json:"rejected_reason"`
		}
		if err := json.Unmarshal(payload, &t); err != nil {
			return nil, "", fmt.Errorf("%w: item %d", ErrCatalogNotUnderstood, i)
		}
		// `null` and `{}` do NOT fail the Unmarshal (docs/ARMADILHAS.md,
		// "Go / JSON"): they become a zeroed struct. Without this check,
		// a null item would become a ghost template, with an empty name,
		// in the consumer's catalog.
		if strings.TrimSpace(t.Name) == "" {
			return nil, "", fmt.Errorf("%w: item %d sem nome", ErrCatalogNotUnderstood, i)
		}
		items = append(items, Template{
			// No empty check, unlike the name: see the comment on
			// Template's ID field.
			ID:       strings.TrimSpace(t.ID),
			Name:     strings.TrimSpace(t.Name),
			Status:   strings.TrimSpace(t.Status),
			Category: strings.TrimSpace(t.Category),
			Language: strings.TrimSpace(t.Language),
			// The components travel RAW: describing their schema would
			// be asserting a Meta shape this project hasn't verified
			// against the source, and rewriting them would break the
			// template the day it changes.
			Components: t.Components,
			// "NONE" passes through as it came — see Template.Reason's
			// comment.
			Reason: strings.TrimSpace(t.RejectedReason),
		})
	}
	return items, strings.TrimSpace(envelope.Paging.Next), nil
}

// CreateTemplate creates the template on the WABA and returns Meta's
// verdict.
func (c *Client) CreateTemplate(
	ctx context.Context, wabaID, token string, p TemplateRequest,
) (CreatedTemplate, error) {
	if !PhoneNumberIDValid(wabaID) {
		return CreatedTemplate{}, ErrInvalidWabaID
	}
	target, err := url.JoinPath(c.base, wabaID, "message_templates")
	if err != nil {
		return CreatedTemplate{}, fmt.Errorf("meta: montar url: %w", err)
	}

	body := map[string]any{
		"name":       p.Name,
		"category":   p.Category,
		"language":   p.Language,
		"components": p.Components,
	}
	// ONLY enters the map when the consumer ASKED for it: `nil` is "I
	// didn't send anything", and it's the only way for this request's
	// body to stay BYTE FOR BYTE identical to before T-108 when no one
	// uses the new field.
	if p.AllowCategoryChange != nil {
		body["allow_category_change"] = *p.AllowCategoryChange
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return CreatedTemplate{}, fmt.Errorf("meta: montar corpo: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return CreatedTemplate{}, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return CreatedTemplate{}, fmt.Errorf("meta: falha de transporte ao criar template: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	rawResponse, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return CreatedTemplate{}, fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, rawResponse); metaError != nil {
		return CreatedTemplate{}, metaError
	}

	// A `2xx` does NOT prove an id came with it — the same trap as
	// sending (ErrResponseWithoutID). Returning an empty id as success makes
	// the consumer store a record that LOOKS created.
	var r struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(rawResponse, &r); err != nil {
		return CreatedTemplate{}, fmt.Errorf("%w: corpo nao entendido", ErrTemplateWithoutID)
	}
	id := strings.TrimSpace(r.ID)
	if id == "" {
		return CreatedTemplate{}, ErrTemplateWithoutID
	}
	// The status comes from META, raw: inventing "PENDING" here would be
	// asserting what it didn't say. Whoever tells the consumer that a
	// freshly created template needs approval is the handler, with OUR
	// OWN text.
	return CreatedTemplate{
		ID:       id,
		Status:   strings.TrimSpace(r.Status),
		Category: strings.TrimSpace(r.Category),
	}, nil
}

// DeleteTemplate deletes, BY NAME, the WABA's template.
//
// `DELETE /{waba}/message_templates?name=<nome>`, and only that shape. Meta
// also accepts `hsm_id` (one template, one language) and a batch by
// `hsm_ids`; NEITHER is used here, on purpose. This gateway does ONE Meta
// action per call (CLAUDE.md, "o gateway faz UMA ação da Meta por chamada"):
// the loop over dozens of names belongs to the consumer, which is the side
// that knows the order to do it in and what to do when one of them fails.
//
// 🔴 DELETING BY NAME DELETES EVERY LANGUAGE — verbatim from the reference,
// "Deletes templates matching the name in all languages"
// (developers.facebook.com/docs/graph-api/reference/whats-app-business-account/message_templates/,
// read on 2026-08-28). Whoever calls this reports that to the consumer; the
// client does not soften it.
//
// The response is `{"success": bool}`, and anything that is not an explicit
// `true` comes out as ErrDeletionNotConfirmed — see the comment there.
func (c *Client) DeleteTemplate(ctx context.Context, wabaID, token, name string) error {
	if !PhoneNumberIDValid(wabaID) {
		// THE SAME guard, and the same function, as the two sisters above:
		// url.JoinPath resolves `..`, so an id with `../` escapes the Graph
		// API's version prefix and points at another endpoint.
		return ErrInvalidWabaID
	}
	target, err := url.JoinPath(c.base, wabaID, "message_templates")
	if err != nil {
		return fmt.Errorf("meta: montar url: %w", err)
	}
	q := url.Values{}
	q.Set("name", name)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("meta: montar requisicao: %w", err)
	}
	// The token goes in the HEADER, never in the URL — the same reason as
	// every other call in this package: a token in a query string leaks
	// into proxy, server, and CDN logs. The template NAME does travel in
	// the query, and that is fine: it is a technical identifier, not
	// customer content.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// errWithoutDetail because *url.Error carries the FULL URL, and the
		// full URL carries the waba_id.
		return fmt.Errorf("meta: falha de transporte ao apagar template: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return metaError
	}

	// A POINTER, not `bool`: `{}` and `null` deserialize with NO error into
	// a zeroed struct, so a plain `bool` would make "Meta did not send the
	// field" and "Meta said false" the same fact. Both are refusals here,
	// but they are different refusals, and the pointer is what keeps the
	// distinction available to whoever reads this later.
	var r struct {
		Success *bool `json:"success"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("%w: corpo nao entendido", ErrDeletionNotConfirmed)
	}
	if r.Success == nil || !*r.Success {
		return ErrDeletionNotConfirmed
	}
	return nil
}

// sameGraphOrigin rejects a `paging.next` that leaves the configured
// Graph API.
//
// The `next` comes from the response BODY. Following it blindly would send
// the instance's send token — in the Authorization header — to whatever
// host showed up there. The rejected URL does NOT go into the error
// message: it can carry a credential in the query, and this text goes up
// to the log.
func sameGraphOrigin(base, next string) error {
	b, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("meta: base da Graph API invalida: %w", errors.New("nao e URL"))
	}
	p, err := url.Parse(next)
	if err != nil {
		return ErrPageFromAnotherOrigin
	}
	if !strings.EqualFold(p.Scheme, b.Scheme) || !strings.EqualFold(p.Host, b.Host) {
		return ErrPageFromAnotherOrigin
	}
	return nil
}
