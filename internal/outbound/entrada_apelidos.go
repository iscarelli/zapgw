// entrada_apelidos.go — T-203 (step 2 of T-189, docs/MIGRACAO-CONTRATO-EN.md)
// for KEYS, and T-207 (the same step 2, docs/MIGRACAO-CONTRATO-EN.md
// section 8) for VALUES.
//
// The gateway starts ACCEPTING the English name of every ENTRADA-direction
// key in the migration table, in ADDITION to the Portuguese one it already
// accepted — never instead of it. T-207 does the same for the three
// ENTRADA value vocabularies (section 8.1, 8.3, 8.5 — see the block
// comment above requestTypeValueAlias, further down this file). Output is
// untouched: a consumer who has not moved a single field or value keeps
// getting exactly the response they get today (see the file header of
// docs/MIGRACAO-CONTRATO-EN.md — step 4 is the one that flips output, and
// it is MAJOR, and it is not this one).
//
// THE ALIAS IS POSITIONAL, NEVER GLOBAL (T-203 Do item 2). Each dictionary
// below applies at EXACTLY the nesting level the migration table names for
// that key's ENTRADA occurrence — never a generic "walk the whole JSON tree
// and rename anything recognized". A generic walker would also rewrite
// `name`/`type` INSIDE `contatos[i]` (Meta's own vCard vocabulary, kept in
// English on purpose — see the comment on Contact in mensagem.go) or inside
// `components`/`botoes_url` (raw pass-through, meant to be REFUSED unread,
// never interpreted). See docs/ARMADILHAS.md, this project's mother trap.
//
// ORDER OF OPERATIONS, and it is the whole point of this file: translation
// runs BEFORE json.Unmarshal into the request struct and BEFORE
// RequestHash. The idempotency hash — and every validation error — has to
// see the CANONICAL (Portuguese) form; hashing the raw body would make the
// SAME request, written once in Portuguese and once in English, produce two
// different hashes, and the SAME message would go out twice to the customer
// (T-203 Do item 5 — the single most expensive defect this task exists to
// prevent).
package outbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrConflictingAlias: the consumer sent the SAME field under both its
// Portuguese and its English name in the SAME request. This gateway does
// not pick a winner for them — "the last one wins" is invisible today and
// becomes a production defect discovered six months from now, when nobody
// remembers which spelling was supposed to be authoritative.
var ErrConflictingAlias = errors.New("outbound: mesma chave em portugues e em ingles no mesmo pedido")

// translateAliasesInPlace renames, at ONE level of an already-decoded JSON
// object, every English alias in `dict` (english -> portuguese) to its
// canonical Portuguese key — the one every validator and struct tag in this
// package already expects. Every key NOT in `dict` is left byte-for-byte
// alone: that is what makes this safe to call on an object that also
// carries fields this migration step does not cover, or a nested block
// that is Meta's own vocabulary passed through untouched.
//
// Returns, for the old-name counter (T-203 Do item 6), the canonical
// (Portuguese) keys that were present in their OLD spelling — i.e. this
// particular field has not been moved to English by whoever sent the
// request.
func translateAliasesInPlace(m map[string]json.RawMessage, dict map[string]string) (oldNames []string, err error) {
	for en, pt := range dict {
		enVal, hasEN := m[en]
		_, hasPT := m[pt]
		switch {
		case hasEN && hasPT:
			return nil, fmt.Errorf("%w: %q e %q", ErrConflictingAlias, pt, en)
		case hasEN:
			m[pt] = enVal
			delete(m, en)
		case hasPT:
			oldNames = append(oldNames, pt)
		}
	}
	return oldNames, nil
}

// translateSubObject applies translateAliasesInPlace to ONE nested JSON
// object — a `json.RawMessage` that is itself `{...}` — and re-encodes it.
//
// A `raw` that is not a JSON object (absent, `null`, or malformed) comes
// back UNCHANGED and with no error: this function's only job is to
// translate keys it can parse as an object. Shape validation stays the
// job of the struct's own json.Unmarshal and Validate(), which see EXACTLY
// the error they would have seen if this step had not run at all.
func translateSubObject(raw json.RawMessage, dict map[string]string) (json.RawMessage, []string, error) {
	if len(raw) == 0 {
		return raw, nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return raw, nil, nil
	}
	oldNames, err := translateAliasesInPlace(m, dict)
	if err != nil {
		return nil, nil, err
	}
	out, merr := json.Marshal(m)
	if merr != nil {
		return raw, nil, nil
	}
	return out, oldNames, nil
}

// translateTopLevel is translateSubObject applied to a WHOLE request body,
// for the routes whose ENTRADA keys are all at the top level (no nested
// object needs its own dictionary). Same fallback rule: a body that is not
// a JSON object comes back unchanged, and the real json.Unmarshal into the
// request struct is what reports "corpo nao e JSON valido" — this function
// never produces that message itself, so it never needs to quote (and
// possibly leak) a fragment of a body that, on some of these routes,
// carries secrets (app_secret, token_envio).
func translateTopLevel(raw []byte, dict map[string]string) ([]byte, []string, error) {
	return translateSubObject(json.RawMessage(raw), dict)
}

// --- POST /v1/messages (mensagem.go, Request) ---

// requestAliasAtTopLevel: Request's ENTRADA fields, from
// docs/MIGRACAO-CONTRATO-EN.md. Same-spelling pairs (`template`, `payload`)
// are deliberately ABSENT — there is nothing to alias, the consumer already
// wrote the final name. `media_id` is likewise absent: it is already
// English and listed in docs/contrato-chaves-que-nao-mudam.txt (Do item 7)
// — aliasing it would be renaming the destination backwards.
var requestAliasAtTopLevel = map[string]string{
	"instance":         "instancia",
	"to":               "para",
	"kind":             "tipo",
	"reply_to":         "responder_a",
	"text":             "texto",
	"language":         "idioma",
	"variables":        "variaveis",
	"header":           "cabecalho",
	"header_text":      "cabecalho_texto",
	"template_buttons": "botoes_template",
	"buttons":          "botoes",
	"button_title":     "botao_titulo",
	"button_url":       "botao_url",
	"footer":           "rodape",
	"category":         "categoria",
	"caption":          "legenda",
	"file_name":        "nome_arquivo",
	"reaction":         "reacao",
	"location":         "localizacao",
	// url_buttons/botoes_url: the field REFUSED on purpose since T-045 (see
	// Request.RemovedURLButtons). Aliased like any other ENTRADA row so the
	// refusal fires the same way regardless of which spelling arrives —
	// there is no special case here, only the normal translation.
	"url_buttons": "botoes_url",
}

// templateHeaderAlias: TemplateHeader's fields (the `cabecalho` object),
// one nesting level below Request.
var templateHeaderAlias = map[string]string{
	"kind":      "tipo",
	"text":      "texto",
	"file_name": "nome_arquivo",
}

// templateButtonAlias: TemplateButtonUnion's fields, applied to EACH item
// of `botoes_template`. `payload` is absent on purpose — already the same
// spelling in both languages. `index` -> `indice` was added by T-208
// (docs/MIGRACAO-CONTRATO-EN.md section 9.1, row 2 —
// docs/INVENTARIO-VALORES.md section 2, item 2): it was missing from the
// migration table itself, not merely from this dict, which is exactly the
// blind spot T-208 exists to close — a key with no published pair is
// invisible to the old-name counter (Do item 6), not merely unaliased.
var templateButtonAlias = map[string]string{
	"kind":  "tipo",
	"text":  "texto",
	"index": "indice",
}

// reactionAlias: ReactionRequest's fields (the `reacao` object). `emoji`
// is absent — same spelling in both languages.
var reactionAlias = map[string]string{
	"target": "alvo",
}

// locationAlias: LocationRequest's fields (the `localizacao` object).
// `latitude`/`longitude` are absent — already English, and not in the
// migration table as ENTRADA-aliasable (they are the same word in both
// languages, nothing to alias).
var locationAlias = map[string]string{
	"address": "endereco",
	"name":    "nome",
}

// flowAlias: FlowRequest's fields (the `fluxo` object). Only `nome` is in
// the migration table as ENTRADA; `id`, `token`, `acao`, `tela` and `dados`
// are NOT — Do item 7 forbids inventing an alias for a key the table does
// not name, even though `fluxo` itself sits one level up.
var flowAlias = map[string]string{
	"name": "nome",
}

// buttonAlias: Button's fields, applied to EACH item of `botoes` — the
// PLAIN quick-reply buttons of `tipo:"botoes"` (mensagem.go's Button
// struct), NOT `botoes_template` (TemplateButtonUnion, a different type,
// aliased above) and NOT `botao_titulo` (Request.ButtonTitle, a THIRD,
// unrelated field — the single-button title of `tipo:"cta_url"`/`"lista"`/
// `"flow"`, already in requestAliasAtTopLevel).
//
// Added by T-208 (docs/MIGRACAO-CONTRATO-EN.md section 9.1, row 1 —
// docs/INVENTARIO-VALORES.md section 2, item 1): `titulo` had NO published
// pair before T-208, and a key the migration table doesn't name is
// invisible to config.CounterOldNameUsed, not merely unaliased (Do item
// 6) — this is the exact scenario `consumer-b` measured against production:
// they sent `titulo` (this field) and the counter never moved. `id` is
// absent — already the same spelling in both languages.
var buttonAlias = map[string]string{
	"title": "titulo",
}

// translateButtonItem applies buttonAlias to ONE item of `botoes` — same
// fallback rule as translateSubObject: an item that is not a JSON object
// comes back unchanged, and the real json.Unmarshal into Button is what
// reports the shape error.
func translateButtonItem(raw json.RawMessage) (json.RawMessage, []string, error) {
	return translateSubObject(raw, buttonAlias)
}

// translateRequestBody is T-203 Do items 1+2 for POST /v1/messages: it
// accepts the English name of every ENTRADA key Request (and its nested
// blocks) has, at the EXACT position that key occupies, and returns the
// translated body plus which canonical (Portuguese) keys arrived in their
// OLD spelling — for the old-name counter (Do item 6).
//
// MUST be called BEFORE json.Unmarshal into Request and BEFORE
// RequestHash — see this file's header comment for why.
func translateRequestBody(raw []byte) ([]byte, []string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		// Not a JSON object at all — the real json.Unmarshal into Request,
		// right after this call, reports "corpo nao e JSON valido" itself.
		return raw, nil, nil
	}

	var oldNames []string

	top, err := translateAliasesInPlace(m, requestAliasAtTopLevel)
	if err != nil {
		return nil, nil, err
	}
	oldNames = append(oldNames, top...)

	// T-207: value aliases run AFTER the key alias above — so they read
	// "tipo"/"categoria" under their canonical name regardless of which
	// spelling the consumer used for the KEY — and BEFORE json.Unmarshal /
	// RequestHash below, same ordering requirement and same reason as the
	// key alias: hashing before translation would make {"tipo":"texto"}
	// and {"tipo":"text"} — the SAME request — produce two different
	// hashes, and the same message would go out twice to the customer
	// (see this file's header comment).
	if old := translateValueAliasInPlace(m, "tipo", requestTypeValueAlias); old != "" {
		oldNames = append(oldNames, old)
	}
	if old := translateValueAliasInPlace(m, "categoria", requestCategoryValueAlias); old != "" {
		oldNames = append(oldNames, old)
	}

	nested := []struct {
		key  string
		dict map[string]string
	}{
		{"cabecalho", templateHeaderAlias},
		{"reacao", reactionAlias},
		{"localizacao", locationAlias},
		{"fluxo", flowAlias},
	}
	for _, n := range nested {
		sub, ok := m[n.key]
		if !ok {
			continue
		}
		translated, old, err := translateSubObject(sub, n.dict)
		if err != nil {
			return nil, nil, err
		}
		m[n.key] = translated
		oldNames = append(oldNames, old...)
	}

	// botoes is a LIST of Button (T-208) — each item gets its own
	// translation, positionally, never the list's key itself (that one is
	// already handled by requestAliasAtTopLevel above, "buttons"->"botoes").
	// NOT the same field as botoes_template, below — see buttonAlias's
	// comment for why the two must never share a dict.
	if sub, ok := m["botoes"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(sub, &arr); err == nil {
			for i := range arr {
				translated, old, err := translateButtonItem(arr[i])
				if err != nil {
					return nil, nil, err
				}
				arr[i] = translated
				oldNames = append(oldNames, old...)
			}
			if out, merr := json.Marshal(arr); merr == nil {
				m["botoes"] = out
			}
		}
	}

	// botoes_template is a LIST of TemplateButtonUnion — each item gets its
	// own translation, positionally, never the list's key itself (that one
	// is already handled by requestAliasAtTopLevel above).
	// translateTemplateButtonItem (T-207) does BOTH the key alias
	// (templateButtonAlias) and the value alias of "tipo"
	// (templateButtonTypeValueAlias, section 8.3) for each item, in that
	// order — see its own comment for why the order matters.
	if sub, ok := m["botoes_template"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(sub, &arr); err == nil {
			for i := range arr {
				translated, old, err := translateTemplateButtonItem(arr[i])
				if err != nil {
					return nil, nil, err
				}
				arr[i] = translated
				oldNames = append(oldNames, old...)
			}
			if out, merr := json.Marshal(arr); merr == nil {
				m["botoes_template"] = out
			}
		}
	}

	out, merr := json.Marshal(m)
	if merr != nil {
		return raw, nil, nil
	}
	return out, oldNames, nil
}

// --- The other entrada-decoding routes: flat, top-level only. ---

// createTemplateAlias: CreateTemplateRequest's fields (POST /v1/templates,
// templates_handler.go).
var createTemplateAlias = map[string]string{
	"instance":   "instancia",
	"name":       "nome",
	"category":   "categoria",
	"language":   "idioma",
	"components": "componentes",
}

// registrationAlias: RegistrationRequest's fields (POST /v1/cadastro,
// cadastro_handler.go). `waba_id`, `phone_number_id`, `app_secret`,
// `callback_url` and `bundle_ca` are absent — already English, and not in
// the migration table as ENTRADA-aliasable Portuguese keys.
var registrationAlias = map[string]string{
	"instance":       "instancia",
	"display_number": "numero_exibido",
	"send_token":     "token_envio",
}

// instanceOnlyAlias: POST /v1/pausa, POST /v1/leituras and POST /v1/fumaca
// only have ONE ENTRADA key from the migration table — `instancia` itself.
// `wamid`/`digitando` (leituras) and `destino` (fumaca) are NOT in the
// table (Do item 7: never invent an alias for a key that is not there).
// POST/DELETE /v1/bloqueios is NOT in this list any more — see blockAlias,
// below: it moved out the day `telefones` got a published pair (T-208).
var instanceOnlyAlias = map[string]string{
	"instance": "instancia",
}

// blockAlias: BlockRequest's fields (POST/DELETE /v1/bloqueios,
// bloqueio_handler.go). Added by T-208 (docs/MIGRACAO-CONTRATO-EN.md
// section 9.1, row 3 — docs/INVENTARIO-VALORES.md section 2, item 3):
// `telefones` had NO published pair before T-208, and — same reasoning as
// buttonAlias above — a key the migration table doesn't name is invisible
// to config.CounterOldNameUsed, not merely unaliased (Do item 6). This
// route used to share instanceOnlyAlias with pausa/leituras/fumaca; it no
// longer does, because it now has a SECOND aliasable key those three
// don't have.
var blockAlias = map[string]string{
	"instance": "instancia",
	"phones":   "telefones",
}

// --- T-207 (step 2 of T-189, for VALUES): docs/MIGRACAO-CONTRATO-EN.md
// section 8. Only the three ENTRADA value vocabularies get an alias here —
// sections 8.2, 8.6, 8.7, 8.8-8.10 and 8.11 are SAIDA and are not touched
// (Do item 1): accepting an output value on input means nothing and would
// only be surface added by mistake.
//
// `tipo` IS FOUR VOCABULARIES SHARING ONE JSON KEY (Do item 2), same trap
// as the key alias above: requestTypeValueAlias, templateButtonTypeValueAlias
// and (the fourth, `tipo` of TemplateHeader/section 8.4-adjacent, already
// English and out of section 8 entirely) are kept in SEPARATE dicts, each
// scoped to the ONE object the migration table names for it. A value from
// one dict means nothing in another object — `media` at the top level and
// `quick_reply` inside a template button do not share a vocabulary just
// because both keys are spelled "tipo".
//
// `*`-marked values in docs/MIGRACAO-CONTRATO-EN.md section 8 (the same
// word in both languages) are deliberately ABSENT from every dict below:
// there is nothing to alias, and an absent entry can never be misread by
// the counter as either "old" or "new" spelling — see
// translateValueAliasInPlace.
//
// A value has NO conflict case (Do item 3): a field carries exactly one
// value, so there is nothing analogous to ErrConflictingAlias for values —
// don't invent one.

// requestTypeValueAlias: `Request.Type` ("tipo" at the top level of
// POST /v1/messages), section 8.1. `template`, `cta_url` and `flow` are
// absent — same word in both languages, nothing to alias.
var requestTypeValueAlias = map[string]string{
	"text":             "texto",
	"buttons":          "botoes",
	"list":             "lista",
	"request_location": "pedir_localizacao",
	"reaction":         "reacao",
	"location":         "localizacao",
	"contacts":         "contatos",
	"media":            "midia",
}

// requestCategoryValueAlias: `Request.Category` ("categoria" at the top
// level of POST /v1/messages), section 8.5. `video`, `audio` and `sticker`
// are absent — same word in both languages, nothing to alias.
var requestCategoryValueAlias = map[string]string{
	"image":    "imagem",
	"document": "documento",
}

// templateButtonTypeValueAlias: `TemplateButtonUnion.Type` ("tipo" inside
// EACH item of `botoes_template`), section 8.3. `url` is absent — same
// word in both languages, nothing to alias. Deliberately its OWN dict, not
// merged with requestTypeValueAlias — see the block comment above.
var templateButtonTypeValueAlias = map[string]string{
	"quick_reply": "resposta_rapida",
}

// translateValueAliasInPlace looks at ONE field's value inside an already
// key-canonicalized JSON object `m`. If the value is the ENGLISH spelling
// present in `dict` (english -> portuguese), it is rewritten in place to
// the canonical Portuguese spelling the rest of this package already
// expects, and "" comes back — this is the NEW spelling, nothing to count.
// If the value is already the OLD (Portuguese) spelling of a word that HAS
// an English alternative in `dict`, the marker "valor:<field>=<value>"
// comes back for the old-name counter (Do item 5) — the "valor:" prefix is
// what lets a reader of the counter's raw markers tell a value apart from
// a key (which are bare canonical field names, see translateAliasesInPlace)
// without needing a second counter. Any other value — a "same word in both
// languages" value that is absent from `dict` on purpose, or a value this
// vocabulary does not recognize at all — passes through untouched:
// recognizing what is a VALID value stays Validate()'s job, never this
// function's (Do item 4 — the alias adds spellings, it never loosens
// validation).
func translateValueAliasInPlace(m map[string]json.RawMessage, field string, dict map[string]string) (oldName string) {
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		// Not a JSON string at all (wrong shape) — the real json.Unmarshal
		// into the request struct, right after this call, reports that
		// error itself.
		return ""
	}
	if pt, hasEN := dict[v]; hasEN {
		out, err := json.Marshal(pt)
		if err != nil {
			return ""
		}
		m[field] = out
		return ""
	}
	for _, pt := range dict {
		if v == pt {
			return "valor:" + field + "=" + v
		}
	}
	return ""
}

// translateTemplateButtonItem applies BOTH the key alias
// (templateButtonAlias) and the value alias (templateButtonTypeValueAlias,
// on "tipo") to ONE item of `botoes_template` — key first, always, so the
// value step finds the field under its canonical name regardless of which
// spelling the consumer used for the key itself. Same fallback rule as
// translateSubObject: an item that is not a JSON object comes back
// unchanged, and the real json.Unmarshal into TemplateButtonUnion is what
// reports the shape error.
func translateTemplateButtonItem(raw json.RawMessage) (json.RawMessage, []string, error) {
	if len(raw) == 0 {
		return raw, nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return raw, nil, nil
	}
	oldNames, err := translateAliasesInPlace(m, templateButtonAlias)
	if err != nil {
		return nil, nil, err
	}
	if old := translateValueAliasInPlace(m, "tipo", templateButtonTypeValueAlias); old != "" {
		oldNames = append(oldNames, old)
	}
	out, merr := json.Marshal(m)
	if merr != nil {
		return raw, nil, nil
	}
	return out, oldNames, nil
}

// translateEntradaOrReject runs translateTopLevel(raw, dict) and, on a
// conflicting alias, writes the 400 and logs it — the SAME boilerplate
// every entrada-decoding handler in this package already repeats for
// "corpo grande demais" and "corpo nao e JSON valido". ok is false only
// when this function has already written the response; the caller must
// return immediately without writing anything else.
func translateEntradaOrReject(
	w http.ResponseWriter, throttle *logThrottle, route, consumerName string,
	raw []byte, dict map[string]string,
) (translated []byte, oldNames []string, ok bool) {
	translated, oldNames, err := translateTopLevel(raw, dict)
	if err != nil {
		logRejection(throttle, route, "", consumerName, err.Error())
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
		return nil, nil, false
	}
	return translated, oldNames, true
}

// --- T-208: ENTRADA-QUERY, a query parameter's alias -------------------
//
// docs/MIGRACAO-CONTRATO-EN.md section 9.2 / docs/INVENTARIO-VALORES.md
// section 3. A query parameter is NEVER a JSON key — r.URL.Query() already
// returns "" for whatever wasn't sent, so none of the machinery above
// (which exists to tell "absent" apart from "empty JSON value") applies
// here. Same PRINCIPLE as the body (accept both, count the old one), a
// DIFFERENT point in the code (docs/TASKS.md, T-208 Do item 2 and 5).
//
// queryAlias prefers the NEW (English) name and falls back to the OLD
// (Portuguese) one only when the new one is absent or blank — the same
// "novo or velho" idiom `consumer-b` already uses on their own read side
// (docs/TASKS.md, T-189). It does NOT reject a request that carries BOTH
// names at once (unlike translateAliasesInPlace's ErrConflictingAlias for
// a body key): the query string is config for THIS call, never message
// content that goes out twice on a hash collision, and Do item 2 asks only
// for "accept both, count the old one" — inventing a second conflict rule
// with no request behind it is exactly what docs/ARMADILHAS.md's mother
// trap warns against (a mechanism nobody asked for, that only a NEW kind of
// bug can grow up in).
func queryAlias(q url.Values, en, pt string) (value string, oldNameUsed bool) {
	if v := strings.TrimSpace(q.Get(en)); v != "" {
		return v, false
	}
	if v := strings.TrimSpace(q.Get(pt)); v != "" {
		return v, true
	}
	return "", false
}

// queryAliasRaw is queryAlias WITHOUT the trim — for the one ENTRADA-QUERY
// pair whose value must go up EXACTLY AS IT ARRIVED
// (`mime_do_payload`/`payload_mime`, media_handler.go's download(): "the
// gateway checks, it never rewrites"). Trimming here would be exactly the
// silent rewrite that comment forbids — and would also turn a
// whitespace-only value into an EMPTY one, skipping wellFormedMime's 400
// instead of triggering it.
func queryAliasRaw(q url.Values, en, pt string) (value string, oldNameUsed bool) {
	if v := q.Get(en); v != "" {
		return v, false
	}
	if v := q.Get(pt); v != "" {
		return v, true
	}
	return "", false
}
