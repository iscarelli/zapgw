// From the validated Request to the Graph API body.
//
// This function assumes Validate() has already passed — it does NOT revalidate. Separating the
// two is deliberate: validation says what is acceptable, this one says how it's written. If
// both knew the rules, they would diverge on the first change.
package outbound

import (
	"strconv"

	"github.com/iscarelli/zapgw/internal/meta"
)

// MetaBody assembles the JSON the Graph API expects.
//
// The phone is CANONICALIZED here: Meta does not guarantee the same spelling you
// registered, and sending without the 9th digit delivers to another number — or to
// none at all, silently.
func MetaBody(p Request) map[string]any {
	body := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                meta.Canonicalize(p.To),
	}

	// `context` only goes in when there's something to quote: Meta may refuse an empty
	// context, and sending the key with no value helps no one.
	if p.ReplyTo != "" {
		body["context"] = map[string]any{"message_id": p.ReplyTo}
	}

	switch p.Type {
	case "texto":
		body["type"] = "text"
		body["text"] = map[string]any{"body": p.Text}

	case "template":
		body["type"] = "template"
		tpl := map[string]any{
			"name":     p.Template,
			"language": map[string]any{"code": p.Language},
		}
		// `components` ABSENT is different from `components: []`. Meta refuses the
		// empty list in some templates, and the difference only shows up in a real send.
		if comps := templateComponents(p); len(comps) > 0 {
			tpl["components"] = comps
		}
		body["template"] = tpl

	case "botoes":
		buttons := make([]map[string]any, 0, len(p.Buttons))
		for _, b := range p.Buttons {
			buttons = append(buttons, map[string]any{
				"type":  "reply",
				"reply": map[string]any{"id": b.ID, "title": b.Title},
			})
		}
		body["type"] = "interactive"
		interactive := map[string]any{
			"type":   "button",
			"body":   map[string]any{"text": p.Text},
			"action": map[string]any{"buttons": buttons},
		}
		// Key ABSENT when empty — never `"header":null` nor
		// `{"text":""}` (same rule as the template's `components` and the
		// media's `caption`). T-137: FREE-TEXT header/footer,
		// optional, unapproved.
		if p.HeaderText != "" {
			interactive["header"] = map[string]any{"type": "text", "text": p.HeaderText}
		}
		if p.Footer != "" {
			interactive["footer"] = map[string]any{"text": p.Footer}
		}
		body["interactive"] = interactive

	case "midia":
		// The field name is THE SAME as `type` in the Cloud API:
		// {"type":"image","image":{…}}. The "imagem" -> "image" translation lives in a
		// single place (meta.GraphAPIType), also read by the validator — two
		// tables would diverge and the error would show up as a 400 from Meta in production.
		cat, _ := meta.KnownCategory(p.Category)
		kind := meta.GraphAPIType(cat)
		media := map[string]any{"id": p.MediaID}
		// Key ABSENT is different from an empty key (same rule as the template's
		// `components`). And both `Aceita…` are the SAME source that Validate
		// consults: without this, a field accepted there would silently vanish here.
		if p.Caption != "" && meta.AcceptsCaption(cat) {
			media["caption"] = p.Caption
		}
		if p.Filename != "" && meta.AcceptsFilename(cat) {
			media["filename"] = p.Filename
		}
		body["type"] = kind
		body[kind] = media

	case "cta_url":
		// The body WITHOUT header/footer is the format that was VERIFIED on a
		// real device in another project on this network — a 200 from Meta would not have
		// proved this, and this base remains FROZEN byte for byte (see
		// TestCtaURLBody). T-137 adds FREE-TEXT header/footer,
		// optional, unapproved, following the SAME key-absent-
		// when-empty rule as the rest of this file.
		body["type"] = "interactive"
		interactive := map[string]any{
			"type": "cta_url",
			"body": map[string]any{"text": p.Text},
			"action": map[string]any{
				"name": "cta_url",
				"parameters": map[string]any{
					"display_text": p.ButtonTitle,
					"url":          p.ButtonURL,
				},
			},
		}
		if p.HeaderText != "" {
			interactive["header"] = map[string]any{"type": "text", "text": p.HeaderText}
		}
		if p.Footer != "" {
			interactive["footer"] = map[string]any{"text": p.Footer}
		}
		body["interactive"] = interactive

	case "lista":
		// Field names checked against the official source
		// (developers.facebook.com/docs/whatsapp/cloud-api/messages/
		// interactive-list-messages, read on 2026-08-20): `sections[].title`,
		// `sections[].rows[]` with `id`, `title`, `description`. `description`
		// ABSENT when empty — never `""` — same key-absent-
		// when-empty rule as the rest of this file.
		sections := make([]map[string]any, 0, len(p.Sections))
		for _, s := range p.Sections {
			rows := make([]map[string]any, 0, len(s.Items))
			for _, it := range s.Items {
				row := map[string]any{"id": it.ID, "title": it.Title}
				if it.Description != "" {
					row["description"] = it.Description
				}
				rows = append(rows, row)
			}
			sections = append(sections, map[string]any{"title": s.Title, "rows": rows})
		}
		body["type"] = "interactive"
		interactive := map[string]any{
			"type": "list",
			"body": map[string]any{"text": p.Text},
			"action": map[string]any{
				"button":   p.ButtonTitle,
				"sections": sections,
			},
		}
		// Same path as T-137: key ABSENT when empty.
		if p.HeaderText != "" {
			interactive["header"] = map[string]any{"type": "text", "text": p.HeaderText}
		}
		if p.Footer != "" {
			interactive["footer"] = map[string]any{"text": p.Footer}
		}
		body["interactive"] = interactive

	case "pedir_localizacao":
		// WHOLE shape from the official source (developers.facebook.com/docs/
		// whatsapp/cloud-api/guides/send-messages/location-request-messages/,
		// read 2026-08-20): {"type":"location_request_message",
		// "body":{"text":…},"action":{"name":"send_location"}}. `send_location`
		// is CONSTANT — there's no consumer field for it (see Validate(),
		// case "pedir_localizacao", in message.go).
		body["type"] = "interactive"
		body["interactive"] = map[string]any{
			"type":   "location_request_message",
			"body":   map[string]any{"text": p.Text},
			"action": map[string]any{"name": "send_location"},
		}

	case "flow":
		// 🟢 SHAPE CONFIRMED AGAINST META (T-154, confirmed by T-156)
		// — see the FlowRequest comment in message.go for the whole
		// proof: a deliberately made-up `flow_id` came back with an error only
		// on that field (meta_code 131009), which proves Meta PARSED
		// the rest of the payload below without complaint. RENDERING
		// remains UNPROVEN — a Flow was never published on this
		// WABA — and the PARAMETERS still come from BSP documentation
		// (360dialog) and a third-party SDK (whatsapp-api-js), read on
		// 2026-08-20: Meta confirmed the COMBINATION passes her parser,
		// not that this source is official.
		//
		// Validate() (message.go, validateFlow) already guaranteed: Flow != nil;
		// Token != ""; ID XOR Name (never both, never neither); Action
		// normalized to "navigate" when empty, and Screen != "" when
		// Action == "navigate". This function does NOT revalidate — same rule as the
		// top of this file.
		f := p.Flow
		parameters := map[string]any{
			// `flow_message_version` is CONSTANT — there's no consumer
			// field for it, same pattern as `action.name` in
			// "pedir_localizacao" just above.
			"flow_message_version": flowMessageVersion,
			"flow_token":           f.Token,
			// `flow_cta`: the same `botao_titulo` that cta_url and lista use
			// (T-145/T-149/T-154) — see the validateButtonTitle comment.
			"flow_cta":    p.ButtonTitle,
			"flow_action": f.Action,
		}
		// `flow_id` XOR `flow_name` — Validate() already guaranteed that only one of
		// the two is filled, so the two `if`s below are mutually
		// exclusive by construction, not by luck.
		if f.ID != "" {
			parameters["flow_id"] = f.ID
		}
		if f.Name != "" {
			parameters["flow_name"] = f.Name
		}
		// `flow_action_payload`: key ABSENT when there's neither screen nor
		// data, same key-absent-when-empty rule as the rest of this
		// file. `screen` is only missing when Action == "data_exchange" and the
		// consumer didn't send `tela` (Validate() only requires `tela` on
		// "navigate").
		payload := map[string]any{}
		if f.Screen != "" {
			payload["screen"] = f.Screen
		}
		if len(f.Data) > 0 {
			payload["data"] = f.Data
		}
		if len(payload) > 0 {
			parameters["flow_action_payload"] = payload
		}
		body["type"] = "interactive"
		body["interactive"] = map[string]any{
			"type": "flow",
			"body": map[string]any{"text": p.Text},
			"action": map[string]any{
				"name":       "flow",
				"parameters": parameters,
			},
		}

	case "reacao":
		// Field names checked against the official source
		// (developers.facebook.com/docs/whatsapp/cloud-api/messages/reaction-messages,
		// read on 2026-07-26): {"type":"reaction","reaction":{"message_id":…,"emoji":…}}.
		// Validate() already guaranteed a non-empty Target and a non-nil Emoji (points to ""
		// on removal, to the value on addition — see the ReactionRequest comment).
		//
		// THE "emoji" KEY HAS TO GO ALWAYS, EVEN EMPTY — NEVER OMITTED. It was the
		// empty string that `consumer-a` proved, on a real device, removes the reaction
		// (T-027, 2026-07-26 10:15 -03). An `if *p.Reaction.Emoji != "" { ... }`
		// here would look like a harmless cleanup and would turn EVERY removal into a
		// request without that key — and Meta would still respond 200 with a new wamid the
		// SAME way, without removing anything: exactly the silent failure this
		// field exists to close.
		body["type"] = "reaction"
		body["reaction"] = map[string]any{
			"message_id": p.Reaction.Target,
			"emoji":      *p.Reaction.Emoji,
		}

	case "localizacao":
		// Field names checked against the official source
		// (developers.facebook.com/docs/whatsapp/cloud-api/messages/location-messages,
		// read on 2026-07-26): latitude/longitude are NUMBERS on the wire, and are ALWAYS
		// present — 0 is a valid coordinate (the crossing of the Greenwich
		// meridian with the equator). Validate() already guaranteed that both
		// pointers are not nil, so dereferencing here is safe; there is no
		// omission conditional for them, unlike Name/Address.
		body["type"] = "location"
		loc := map[string]any{
			"latitude":  *p.Location.Latitude,
			"longitude": *p.Location.Longitude,
		}
		if p.Location.Name != "" {
			loc["name"] = p.Location.Name
		}
		if p.Location.Address != "" {
			loc["address"] = p.Location.Address
		}
		body["location"] = loc

	case "contatos":
		// `type`/`contacts` checked against the official source
		// (developers.facebook.com/docs/whatsapp/cloud-api/messages/
		// contacts-messages, read 2026-08-20): {"type":"contacts","contacts":[…]}.
		// NO TRANSLATION INSIDE: the Contact fields already carry Meta's own
		// names in their own json tags (see the Contact type comment,
		// in message.go, for why) — there is no field-by-field assembly
		// here, unlike the other types in this switch, because there is nothing
		// to translate.
		body["type"] = "contacts"
		body["contacts"] = p.Contacts
	}

	return body
}

// templateComponents translates OUR fields (`cabecalho`, `variaveis`,
// `botoes_template`) into the Graph API's `components` blocks.
//
// WHY THE CONSUMER DOES NOT SEND `components` READY-MADE: the discriminated union
// exists to make what Meta rejects inexpressible, and a passthrough would
// hand that whole class of error back to production — the gateway would become a
// proxy that protects no one. See Request.Components, which exists only to be
// refused.
//
// RETURNS nil (not an empty list) when there is no parameter at all: the caller
// depends on this to not write the `components` key, and `components: []` is
// refused by Meta in some templates.
//
// THE BLOCK ORDER IS FIXED — header, body, button — and it's not a whim: the request's
// hash covers the request, but the assembled body is what goes on the wire, and an order
// that varied between calls would make every body comparison (here and in the
// tests) intermittent. A fixed order makes this impossible by construction.
func templateComponents(p Request) []map[string]any {
	var comps []map[string]any

	if p.Header != nil {
		comps = append(comps, map[string]any{
			"type":       "header",
			"parameters": []map[string]any{headerParameter(*p.Header)},
		})
	}

	if len(p.Variables) > 0 {
		params := make([]map[string]any, 0, len(p.Variables))
		for _, v := range p.Variables {
			params = append(params, map[string]any{"type": "text", "text": v})
		}
		comps = append(comps, map[string]any{"type": "body", "parameters": params})
	}

	// ONE BLOCK PER BUTTON, each with its OWN index: two parameters in the same
	// block would go to the same button, and the second token would be lost.
	//
	// A SINGLE LOOP since T-045. Until then there were two — `botoes_url` first,
	// `botoes_template` after —, and the order between them was fixed here by hand;
	// today the output order is simply the order of the list the consumer
	// sent, with nothing to combine.
	//
	// The `tipo` of EACH item has already been confirmed by Validate() as "url" or
	// "resposta_rapida" — there is no third case here, and that's why there is no
	// `default` in the switch below (MetaBody does not revalidate, see the comment at the
	// top of the file).
	for _, b := range p.TemplateButtons {
		switch b.Type {
		case "url":
			// THE FORMAT IS THE SAME that `botoes_url` produced until T-045, byte for
			// byte, and it remains a non-regression requirement: the field left,
			// the URL button did not. Frozen in
			// TestTemplateURLButtonBodyStaysByteForByteWhatT044Delivered.
			comps = append(comps, map[string]any{
				"type":     "button",
				"sub_type": "url",
				// The `index` travels as a STRING in the Graph API ("0", not 0). Sending a
				// number is the kind of difference that only shows up in a real send.
				"index":      strconv.Itoa(b.Index),
				"parameters": []map[string]any{{"type": "text", "text": b.Text}},
			})
		case "resposta_rapida":
			// The parameter IS "type":"payload", NEVER "type":"text" — both
			// are strings, and only the parameter's TYPE tells Meta that this is
			// a quick reply payload, not a URL suffix. Swapping this
			// silently would bring back the same defect this task
			// exists to close (Meta returns the button's TEXT, not the id).
			comps = append(comps, map[string]any{
				"type":       "button",
				"sub_type":   "quick_reply",
				"index":      strconv.Itoa(b.Index),
				"parameters": []map[string]any{{"type": "payload", "payload": b.Payload}},
			})
		}
	}

	return comps
}

// headerParameter assembles the `header` block's single parameter.
//
// THERE IS NO PATH FOR A RAW URL, and that absence is the guarantee: the media
// parameter only knows how to write `{"id": …}`. A raw URL would make Meta go FETCH the file,
// and that's the path that fails silently when she doesn't fetch it — the reason
// POST /v1/media exists. Validate already refuses a media_id shaped like a URL; here there's
// nowhere to put one.
func headerParameter(c TemplateHeader) map[string]any {
	if c.Type == "texto" {
		return map[string]any{"type": "text", "text": c.Text}
	}
	// The field name is THE SAME as `type` — {"type":"document","document":{…}} —
	// and the "documento" -> "document" translation lives in a single place
	// (meta.GraphAPIType), also read by the validator.
	cat, _ := meta.KnownCategory(c.Type)
	kind := meta.GraphAPIType(cat)
	media := map[string]any{"id": c.MediaID}
	// Key ABSENT is different from an empty key (same rule as `components`), and
	// whoever answers "carries it or not" is the SAME table Validate consults.
	if c.Filename != "" && meta.AcceptsFilename(cat) {
		media["filename"] = c.Filename
	}
	return map[string]any{"type": kind, kind: media}
}
