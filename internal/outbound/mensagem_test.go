package outbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func textRequest() Request {
	return Request{Instance: "lojinha", To: "5511999990000", Type: "texto", Text: "oi"}
}

// A-5: RequestHash has to be DETERMINISTIC. Request is a struct (no map), so
// json.Marshal always serializes the fields in the same order — but the test
// proves this instead of just trusting a reading of the type: an unstable
// hash would turn every legitimate retry into a false 422 (ErrKeyWithDifferentRequest),
// which is worse than not having the guard.
func TestRequestHashIsStableAcross100Calls(t *testing.T) {
	p := Request{
		Instance: "lojinha", To: "5511999990000", Type: "botoes",
		Text: "confirma?", Buttons: []Button{{ID: "SIM", Title: "Sim"}, {ID: "NAO", Title: "Nao"}},
	}

	firstOne := RequestHash(p)
	if firstOne == "" {
		t.Fatal("RequestHash devolveu vazio")
	}
	for i := 0; i < 100; i++ {
		if got := RequestHash(p); got != firstOne {
			t.Fatalf("chamada %d: hash = %q, quero %q (o mesmo de sempre)", i, got, firstOne)
		}
	}
}

// DIFFERENT requests have to produce DIFFERENT hashes — otherwise the A-5
// guard would let distinct requests collide on the same key.
func TestRequestHashDistinguishesTheText(t *testing.T) {
	a := RequestHash(Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "lembrete"})
	b := RequestHash(Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "cobranca"})
	if a == b {
		t.Fatalf("pedidos com texto diferente produziram o MESMO hash: %q", a)
	}
}

// F4 — found in the adversarial re-review. mensagem.go called meta.Canonicalize(p.To)
// only to DECIDE whether a digit was left over, and threw the result away — so
// RequestHash hashed the RAW p.To while MetaBody sent the canonical one. Two
// spellings of the SAME number ("+55 11 99999-0000" and "5511999990000") produce
// the same message on the wire and DIFFERENT hashes: a legitimate retry whose
// formatting varied would get a false 422 (ErrKeyWithDifferentRequest), which is worse
// than not having the guard.
func TestRequestHashIsEqualForTwoSpellingsOfTheSamePhone(t *testing.T) {
	withAreaCode := Request{Instance: "lojinha", To: "+55 11 99999-0000", Type: "texto", Text: "oi"}
	canonical := Request{Instance: "lojinha", To: "5511999990000", Type: "texto", Text: "oi"}

	if err := withAreaCode.Validate(); err != nil {
		t.Fatalf("Validate (formatado): %v", err)
	}
	if err := canonical.Validate(); err != nil {
		t.Fatalf("Validate (canonico): %v", err)
	}

	a := RequestHash(withAreaCode)
	b := RequestHash(canonical)
	if a != b {
		t.Fatalf("hashes diferentes para o MESMO telefone em grafias diferentes: %q (formatado, To=%q) != %q (canonico, To=%q)",
			a, withAreaCode.To, b, canonical.To)
	}
}

func TestValidateAcceptsTheFourV1Types(t *testing.T) {
	cases := []Request{
		{Instance: "l", To: "5511999990000", Type: "texto", Text: "oi"},
		{Instance: "l", To: "5511999990000", Type: "template",
			Template: "lembrete", Language: "pt_BR"},
		{Instance: "l", To: "5511999990000", Type: "botoes", Text: "confirma?",
			Buttons: []Button{{ID: "SIM", Title: "Sim"}}},
		{Instance: "l", To: "5511999990000", Type: "cta_url", Text: "veja",
			ButtonTitle: "Abrir", ButtonURL: "https://exemplo.com"},
	}

	for _, p := range cases {
		if err := p.Validate(); err != nil {
			t.Errorf("tipo %q recusado: %v", p.Type, err)
		}
	}
}

func TestValidateRefusesUnknownType(t *testing.T) {
	// T-024 made "reacao" and "localizacao" VALID types — they no longer
	// belong here. "REACAO" and "Localizacao" are still invalid: `tipo` is
	// deliberately case-sensitive, and a consumer that spells it differently
	// has to get ErrUnknownType, never fall into a type by accident.
	for _, kind := range []string{"", "audio", "TEXTO", "REACAO", "Localizacao"} {
		p := textRequest()
		p.Type = kind
		if err := p.Validate(); !errors.Is(err, ErrUnknownType) {
			t.Errorf("tipo %q: erro = %v, quero ErrUnknownType", kind, err)
		}
	}
}

// THE REASON THE UNION IS DISCRIMINATED, and it's the whole reason this file
// exists. In the Cloud API `interactive.type` is a SINGLE value, and a reply
// button and a link button have INCOMPATIBLE `action` shapes. Without this
// guard, mixing the two would be a 400 from Meta discovered in production.
// With it, it's a schema error before the wire.
//
// A system on this network measured 84 templates in production and found 0
// mixed ones — which means the case has never appeared, not that it can't.
func TestValidateRefusesButtonsMixedWithCtaURL(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "botoes", Text: "?",
		Buttons:     []Button{{ID: "SIM", Title: "Sim"}},
		ButtonTitle: "Abrir", ButtonURL: "https://exemplo.com",
	}

	if err := p.Validate(); !errors.Is(err, ErrMixedButtons) {
		t.Fatalf("erro = %v, quero ErrMixedButtons", err)
	}
}

// PITFALL — cost paid on another project on this network.
// Meta ACCEPTS `context` on a template and responds 200, and the quote bubble
// NEVER renders. Apparent success hiding partial delivery: there's no error in
// the response, no error in the status webhook, and no endpoint to check
// against. The only evidence is the customer seeing a reply show up detached
// with no quote.
func TestValidateRefusesReplyToInTemplate(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "lembrete", Language: "pt_BR", ReplyTo: "wamid.ABC",
	}

	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) {
		t.Fatalf("erro = %v, quero ErrFieldForbidden", err)
	}
	if !strings.Contains(err.Error(), "responder_a") {
		t.Errorf("o erro nao diz QUAL campo: %v", err)
	}
}

func TestValidateAcceptsReplyToOnTheOtherTypes(t *testing.T) {
	for _, kind := range []string{"texto", "botoes", "cta_url"} {
		p := textRequest()
		p.Type = kind
		p.ReplyTo = "wamid.ABC"
		switch kind {
		case "botoes":
			p.Buttons = []Button{{ID: "SIM", Title: "Sim"}}
		case "cta_url":
			p.ButtonTitle, p.ButtonURL = "Abrir", "https://exemplo.com"
		}
		if err := p.Validate(); err != nil {
			t.Errorf("tipo %q recusou responder_a: %v", kind, err)
		}
	}
}

func TestValidateRequiresTheFieldsOfEachType(t *testing.T) {
	cases := []struct {
		name  string
		p     Request
		field string
	}{
		{"texto sem texto", Request{Instance: "l", To: "5511999990000", Type: "texto"}, "texto"},
		{"template sem nome", Request{Instance: "l", To: "5511999990000", Type: "template", Language: "pt_BR"}, "template"},
		{"template sem idioma", Request{Instance: "l", To: "5511999990000", Type: "template", Template: "x"}, "idioma"},
		{"botoes sem botao", Request{Instance: "l", To: "5511999990000", Type: "botoes", Text: "?"}, "botoes"},
		{"cta sem url", Request{Instance: "l", To: "5511999990000", Type: "cta_url", Text: "?", ButtonTitle: "Abrir"}, "botao_url"},
		{"sem instancia", Request{To: "5511999990000", Type: "texto", Text: "oi"}, "instancia"},
		{"sem para", Request{Instance: "l", Type: "texto", Text: "oi"}, "para"},
	}

	for _, c := range cases {
		err := c.p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("%s: erro = %v, quero ErrFieldRequired", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia o campo %q: %v", c.name, c.field, err)
		}
	}
}

// PITFALL — cost paid in production on another project on this network: sending
// a PDF as base64 FAILED SILENTLY. The Cloud API doesn't accept base64; only a
// public URL or media_id. Refusing with a NAMED error is what stops the next
// person from having to discover this with the customer on the other end.
func TestValidateRefusesBase64WithANamedError(t *testing.T) {
	p := textRequest()
	p.Text = "data:application/pdf;base64,JVBERi0xLjQK"

	err := p.Validate()
	if !errors.Is(err, ErrBase64) {
		t.Fatalf("erro = %v, quero ErrBase64", err)
	}
	if !strings.Contains(err.Error(), "media_id") {
		t.Errorf("o erro nao diz o que fazer no lugar: %v", err)
	}
}

func TestValidateDoesNotConfuseNormalTextWithBase64(t *testing.T) {
	// The guard can't refuse a legitimate message that happens to talk about data.
	for _, text := range []string{
		"manda o boleto em pdf",
		"data: 23/07",
		"base64 e um jeito de codificar",
		"https://exemplo.com/arquivo.pdf",
	} {
		p := textRequest()
		p.Text = text
		if err := p.Validate(); err != nil {
			t.Errorf("texto legitimo %q recusado: %v", text, err)
		}
	}
}

// CRITICAL found in the T5 review. The previous guard was case-sensitive and
// required the marker at position ZERO — so DATA:, Data:, and any text prefix
// before it sailed through silently, recreating the very silent failure it
// exists to prevent.
func TestValidateRefusesBase64InAnyCaseAndPosition(t *testing.T) {
	cases := []string{
		"data:application/pdf;base64,JVBERi0xLjQK",
		"DATA:application/pdf;base64,JVBERi0xLjQK",
		"Data:application/pdf;base64,JVBERi0xLjQK",
		"DaTa:APPLICATION/PDF;BASE64,JVBERi0xLjQK",
		"veja: data:application/pdf;base64,JVBERi0xLjQK",
		"segue o arquivo\ndata:image/png;base64,iVBORw0KGgo=",
		"  data:application/pdf;charset=utf-8;base64,JVBERi0xLjQK",
	}

	for _, text := range cases {
		p := textRequest()
		p.Text = text
		if err := p.Validate(); !errors.Is(err, ErrBase64) {
			t.Errorf("texto %.50q: erro = %v, quero ErrBase64", text, err)
		}
	}
}

func TestValidateDoesNotRefuseLegitimateTextStartingWithData(t *testing.T) {
	// The other side of the guard. Refusing a legitimate conversation costs
	// just as much as letting a broken send through — both failures land on
	// the end customer.
	cases := []string{
		"data: 23/07",
		"data:24/12 confirmada",
		"Data: amanha",
		"DATA: 15h",
		"data:hoje as 15h",
		"a data:sexta esta confirmada",
		"base64 e um jeito de codificar",
		"manda o boleto em pdf",
		"https://exemplo.com/arquivo.pdf",
	}

	for _, text := range cases {
		p := textRequest()
		p.Text = text
		if err := p.Validate(); err != nil {
			t.Errorf("texto legitimo %q recusado: %v", text, err)
		}
	}
}

// Found in the T6 review, but the gap is in validation: `Validate` checked
// PRESENCE (!= "") and not CONTENT. A responder_a of only spaces exists, is
// useless, and would turn into a context with a blank message_id — a quote for
// an id that doesn't exist. Same family as the blank id already fixed in the
// client.
func TestValidateTreatsAWhitespaceOnlyReplyToAsAbsent(t *testing.T) {
	for _, blank := range []string{"   ", "\t", "\n", " \t\n "} {
		p := textRequest()
		p.ReplyTo = blank
		if err := p.Validate(); err != nil {
			t.Errorf("responder_a %q deu erro: %v — devia ser tratado como ausente", blank, err)
		}
		if p.ReplyTo != "" {
			t.Errorf("responder_a %q nao foi normalizado, ficou %q", blank, p.ReplyTo)
		}
	}
}

func TestValidateNormalizesReplyToWithSpacesAtTheEnds(t *testing.T) {
	p := textRequest()
	p.ReplyTo = "  wamid.ABC  "

	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.ReplyTo != "wamid.ABC" {
		t.Fatalf("ReplyTo = %q — os espacos das pontas seriam propagados para a Meta", p.ReplyTo)
	}
}

func TestValidateRequiresIDAndTitleInEachButton(t *testing.T) {
	cases := []struct {
		name    string
		buttons []Button
		field   string
	}{
		{"id vazio", []Button{{ID: "", Title: "Sim"}}, "botoes[0].id"},
		{"id em branco", []Button{{ID: "   ", Title: "Sim"}}, "botoes[0].id"},
		{"titulo vazio", []Button{{ID: "SIM", Title: ""}}, "botoes[0].titulo"},
		{"titulo em branco", []Button{{ID: "SIM", Title: "\t"}}, "botoes[0].titulo"},
		{"segundo botao ruim", []Button{{ID: "SIM", Title: "Sim"}, {ID: "", Title: "Nao"}}, "botoes[1].id"},
	}

	for _, c := range cases {
		p := Request{Instance: "l", To: "5511999990000", Type: "botoes",
			Text: "confirma?", Buttons: c.buttons}

		err := p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("%s: erro = %v, quero ErrFieldRequired", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia %q: %v", c.name, c.field, err)
		}
	}
}

func TestValidateNormalizesTheButtonsItAccepts(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "botoes",
		Text: "confirma?", Buttons: []Button{{ID: "  SIM  ", Title: "  Sim  "}}}

	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Buttons[0].ID != "SIM" || p.Buttons[0].Title != "Sim" {
		t.Fatalf("botao = %+v — os espacos seriam propagados para a Meta", p.Buttons[0])
	}
}

// ---------------------------------------------------------------------------
// T-137 — `cabecalho_texto`/`rodape` in "botoes" and "cta_url"
// ---------------------------------------------------------------------------

// Presence isn't content: "  oi  " becomes "oi", and "   " counts as ABSENT
// (it can't turn into `"footer":{"text":""}`, the same pitfall already closed
// for `responder_a`, `botoes`, `botao_titulo` etc.).
func TestValidateTrimsHeaderTextAndFooter(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "botoes", Text: "confirma?",
		Buttons:    []Button{{ID: "SIM", Title: "Sim"}},
		HeaderText: "  oi  ",
		Footer:     "   ",
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.HeaderText != "oi" {
		t.Errorf("cabecalho_texto = %q, quero %q (espacos aparados)", p.HeaderText, "oi")
	}
	if p.Footer != "" {
		t.Errorf("rodape = %q, quero \"\" (so espacos conta como ausente)", p.Footer)
	}
}

// Same guarantee, in the cta_url type.
func TestValidateTrimsHeaderTextAndFooterInCtaURL(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "cta_url", Text: "veja",
		ButtonTitle: "Abrir",
		ButtonURL:   "https://exemplo.com",
		HeaderText:  "  novidade  ",
		Footer:      "\t\n",
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.HeaderText != "novidade" {
		t.Errorf("cabecalho_texto = %q, quero %q", p.HeaderText, "novidade")
	}
	if p.Footer != "" {
		t.Errorf("rodape = %q, quero \"\"", p.Footer)
	}
}

// 60 runes passes, 61 refuses — the limit is from the Cloud API, counted in
// RUNES (utf8.RuneCountInString), never in bytes: both fields accept emoji,
// and counting bytes would reject valid text.
func TestValidateRefusesLongHeaderTextAndFooter(t *testing.T) {
	sixty := strings.Repeat("a", 60)
	sixtyOne := strings.Repeat("a", 61)
	// Multi-byte emoji: proves the count is by rune, not by byte —
	// "👍" takes 4 bytes in UTF-8 and counts as 1 rune.
	sixtyWithEmoji := strings.Repeat("👍", 60)

	cases := []struct {
		name        string
		headerText  string
		footerField string
		want        error
	}{
		{"cabecalho com 60 runas passa", sixty, "", nil},
		{"cabecalho com 60 emojis passa", sixtyWithEmoji, "", nil},
		{"rodape com 60 runas passa", "", sixty, nil},
		{"cabecalho com 61 runas recusa", sixtyOne, "", ErrFieldTooLong},
		{"rodape com 61 runas recusa", "", sixtyOne, ErrFieldTooLong},
	}

	for _, c := range cases {
		p := Request{
			Instance: "l", To: "5511999990000", Type: "botoes", Text: "confirma?",
			Buttons:    []Button{{ID: "SIM", Title: "Sim"}},
			HeaderText: c.headerText,
			Footer:     c.footerField,
		}
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: erro = %v, quero %v", c.name, err, c.want)
		}
	}
}

// `cabecalho_texto`/`rodape` only have shape in "botoes" and "cta_url" (T-137).
// Accepting one of them on another type would silently drop it during assembly —
// the most expensive failure mode in this project. Same family of guard as
// `cabecalho`, `botoes_template`, `reacao`, and `localizacao`.
func TestValidateRefusesHeaderTextAndFooterOutsideButtonsAndCtaURL(t *testing.T) {
	base := []struct {
		name string
		p    Request
	}{
		{"texto", Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "oi"}},
		{"template", Request{Instance: "l", To: "5511999990000", Type: "template",
			Template: "t", Language: "pt_BR"}},
		{"midia", Request{Instance: "l", To: "5511999990000", Type: "midia",
			MediaID: "M1", Category: "imagem"}},
		{"reacao", Request{Instance: "l", To: "5511999990000", Type: "reacao",
			Reaction: &ReactionRequest{Target: "wamid.X", Emoji: emojiPtr("👍")}}},
		{"localizacao", Request{Instance: "l", To: "5511999990000", Type: "localizacao",
			Location: &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(1)}}},
	}

	for _, field := range []struct {
		name    string
		applies func(*Request, string)
	}{
		{"cabecalho_texto", func(p *Request, v string) { p.HeaderText = v }},
		{"rodape", func(p *Request, v string) { p.Footer = v }},
	} {
		for _, c := range base {
			t.Run(field.name+"/"+c.name, func(t *testing.T) {
				p := c.p
				field.applies(&p, "valor")
				err := p.Validate()
				if !errors.Is(err, ErrFieldForbidden) {
					t.Fatalf("erro = %v, quero ErrFieldForbidden", err)
				}
				if !strings.Contains(err.Error(), field.name) {
					t.Errorf("o erro nao nomeia %q: %v", field.name, err)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// T-139 — `botao_titulo` of `cta_url` without a cap (Meta refuses above 20 and
// doesn't say which field was wrong — see docs/ARMADILHAS.md)
// ---------------------------------------------------------------------------

// 20 runes passes, 21 refuses with ErrFieldTooLong naming botao_titulo — the
// same cap that already reached the customer without the button on
// 18/08/2026 (#131009, anonymous error). A case with an accent/emoji proves
// the count is by RUNE (utf8.RuneCountInString), never by byte.
func TestValidateRefusesLongCtaURLButtonTitle(t *testing.T) {
	twenty := strings.Repeat("a", 20)
	twentyOne := strings.Repeat("a", 21)
	// "áçã" + "íõü..." gives 20 accented runes, each multi-byte in UTF-8.
	twentyAccented := strings.Repeat("á", 20)
	// Multi-byte emoji: "👍" takes 4 bytes and counts as 1 rune.
	twentyWithEmoji := strings.Repeat("👍", 20)
	twentyOneWithEmoji := strings.Repeat("👍", 21)

	cases := []struct {
		name        string
		buttonTitle string
		want        error
	}{
		{"20 runas passa", twenty, nil},
		{"20 runas acentuadas passa", twentyAccented, nil},
		{"20 emojis passa", twentyWithEmoji, nil},
		{"21 runas recusa", twentyOne, ErrFieldTooLong},
		{"21 emojis recusa", twentyOneWithEmoji, ErrFieldTooLong},
	}

	for _, c := range cases {
		p := Request{
			Instance: "l", To: "5511999990000", Type: "cta_url", Text: "veja",
			ButtonTitle: c.buttonTitle,
			ButtonURL:   "https://exemplo.com",
		}
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: erro = %v, quero %v", c.name, err, c.want)
			continue
		}
		if !strings.Contains(err.Error(), "botao_titulo") {
			t.Errorf("%s: o erro nao nomeia botao_titulo: %v", c.name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// T-140 — cap of 20 runes on `botoes[].titulo` (quick reply) — MEASURED
// against real Meta production on 2026-08-20 (see quickReplyButtonTitleLimit
// in mensagem.go and docs/ARMADILHAS.md)
// ---------------------------------------------------------------------------

// 20 runes passes, 21 refuses with ErrFieldTooLong naming the INDEX of the
// wrong button in a list of several — and a title of 20 accented runes (more
// than 20 bytes) also passes, proving the count is by RUNE
// (utf8.RuneCountInString), not by byte.
func TestValidateRefusesLongQuickReplyButtonTitle(t *testing.T) {
	twenty := strings.Repeat("a", 20)
	twentyOne := strings.Repeat("a", 21)
	// "ç" is multi-byte in UTF-8: 20 runes here add up to 40 bytes.
	twentyAccented := strings.Repeat("ç", 20)

	cases := []struct {
		name    string
		buttons []Button
		want    error
		field   string
	}{
		{"20 runas passa", []Button{{ID: "A", Title: twenty}}, nil, ""},
		{"20 runas acentuadas / 40 bytes passa", []Button{{ID: "A", Title: twentyAccented}}, nil, ""},
		{"21 runas recusa", []Button{{ID: "A", Title: twentyOne}}, ErrFieldTooLong, "botoes[0].titulo"},
		{
			"terceiro botao de uma lista de tres recusa nomeando o indice certo",
			[]Button{
				{ID: "A", Title: "Sim"},
				{ID: "B", Title: "Nao"},
				{ID: "C", Title: twentyOne},
			},
			ErrFieldTooLong, "botoes[2].titulo",
		},
	}

	for _, c := range cases {
		p := Request{Instance: "l", To: "5511999990000", Type: "botoes",
			Text: "confirma?", Buttons: c.buttons}

		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: erro = %v, quero %v", c.name, err, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia %q: %v", c.name, c.field, err)
		}
	}
}

// ---------------------------------------------------------------------------
// T-143 — cap of 3 ITEMS in the `botoes[]` list (quick reply) — MEASURED in
// real Meta production on 2026-08-20, with v0.52.0 already live (see
// quickReplyButtonCountLimit in mensagem.go): four buttons sent
// from `tenant-one` came back with
// `detalhe_meta: "Invalid buttons count. Min allowed buttons: 1, Max allowed
// buttons: 3"`.
// ---------------------------------------------------------------------------

// 1 button still passes (non-regression of the floor already covered by the
// empty-list guard), 3 buttons passes (the exact cap), 4 is refused with
// ErrFieldTooLong naming how many came in and the maximum.
func TestValidateRefusesMoreThanThreeQuickReplyButtons(t *testing.T) {
	cases := []struct {
		name    string
		buttons []Button
		want    error
	}{
		{"1 botao passa", []Button{{ID: "A", Title: "Sim"}}, nil},
		{
			"3 botoes passa (teto exato)",
			[]Button{
				{ID: "A", Title: "Sim"},
				{ID: "B", Title: "Nao"},
				{ID: "C", Title: "Talvez"},
			},
			nil,
		},
		{
			"4 botoes recusa",
			[]Button{
				{ID: "A", Title: "Sim"},
				{ID: "B", Title: "Nao"},
				{ID: "C", Title: "Talvez"},
				{ID: "D", Title: "Depois"},
			},
			ErrFieldTooLong,
		},
	}

	for _, c := range cases {
		p := Request{Instance: "l", To: "5511999990000", Type: "botoes",
			Text: "confirma?", Buttons: c.buttons}

		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: erro = %v, quero %v", c.name, err, c.want)
			continue
		}
		if !strings.Contains(err.Error(), "4") || !strings.Contains(err.Error(), "3") {
			t.Errorf("%s: o erro nao cita quantos vieram e o maximo: %v", c.name, err)
		}
		// T-144: the QUANTITY guard can't cite "caracteres" — that's the
		// unit of the text sentinel, and citing it here sends whoever reads
		// it to measure title length instead of removing a button.
		if strings.Contains(err.Error(), "caracteres") {
			t.Errorf("%s: o erro da guarda de quantidade nao pode citar \"caracteres\": %v", c.name, err)
		}
	}
}

// Found in the final review. The "presence isn't content" rule had been applied
// to responder_a and to botoes, and NOT to the neighboring fields of the same
// function — the fourth occurrence of this pattern in this project.
func TestValidateRefusesBlankFields(t *testing.T) {
	cases := []struct {
		name  string
		p     Request
		field string
	}{
		{"texto em branco", Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "   "}, "texto"},
		{"template em branco", Request{Instance: "l", To: "5511999990000", Type: "template", Template: "  ", Language: "pt_BR"}, "template"},
		{"idioma em branco", Request{Instance: "l", To: "5511999990000", Type: "template", Template: "t", Language: "\t"}, "idioma"},
		{"botao_titulo em branco", Request{Instance: "l", To: "5511999990000", Type: "cta_url", Text: "x", ButtonTitle: "  ", ButtonURL: "https://e.com"}, "botao_titulo"},
		{"botao_url em branco", Request{Instance: "l", To: "5511999990000", Type: "cta_url", Text: "x", ButtonTitle: "Abrir", ButtonURL: " "}, "botao_url"},
		{"instancia em branco", Request{Instance: "  ", To: "5511999990000", Type: "texto", Text: "oi"}, "instancia"},
	}

	for _, c := range cases {
		err := c.p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("%s: erro = %v, quero ErrFieldRequired", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia %q: %v", c.name, c.field, err)
		}
	}
}

func TestValidateRefusesToWithoutADigit(t *testing.T) {
	// Canonicalization ERASES anything that isn't a digit. Without this guard,
	// "nao-e-telefone" would pass and reach Meta as to:"" — the recipient wiped
	// out silently.
	for _, to := range []string{"   ", "nao-e-telefone", "+++", "()-", "\t\n"} {
		p := Request{Instance: "l", To: to, Type: "texto", Text: "oi"}
		err := p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("para %q: erro = %v, quero ErrFieldRequired", to, err)
		}
	}
}

func TestValidateAcceptsAFormattedTo(t *testing.T) {
	// The other side: a legitimate number with formatting can't be refused.
	for _, to := range []string{"5511999990000", "+55 11 99999-0000", " 5511999990000 "} {
		p := Request{Instance: "l", To: to, Type: "texto", Text: "oi"}
		if err := p.Validate(); err != nil {
			t.Errorf("para legitimo %q recusado: %v", to, err)
		}
	}
}

func TestValidateNormalizesTheFieldsItAccepts(t *testing.T) {
	p := Request{Instance: "  lojinha  ", To: " 5511999990000 ", Type: "cta_url",
		Text: "  veja  ", ButtonTitle: "  Abrir  ", ButtonURL: "  https://e.com  "}

	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Instance != "lojinha" || p.Text != "veja" ||
		p.ButtonTitle != "Abrir" || p.ButtonURL != "https://e.com" {
		t.Fatalf("campos nao normalizados: %+v", p)
	}
}

func TestValidateReportsTheMixtureBeforeAMissingField(t *testing.T) {
	// The REAL cause is the mixing. Reporting "missing botao_titulo" makes
	// whoever reads it fill in the title instead of removing the buttons — the
	// error tells them to fix the wrong thing.
	cta := Request{Instance: "l", To: "5511999990000", Type: "cta_url",
		Buttons: []Button{{ID: "SIM", Title: "Sim"}}}
	if err := cta.Validate(); !errors.Is(err, ErrMixedButtons) {
		t.Errorf("cta_url com botoes: erro = %v, quero ErrMixedButtons", err)
	}

	buttons := Request{Instance: "l", To: "5511999990000", Type: "botoes",
		ButtonURL: "https://exemplo.com"}
	if err := buttons.Validate(); !errors.Is(err, ErrMixedButtons) {
		t.Errorf("botoes com botao_url: erro = %v, quero ErrMixedButtons", err)
	}
}

// ---------------------------------------------------------------------------
// type `midia` (T-016)
// ---------------------------------------------------------------------------

func mediaRequest() Request {
	return Request{
		Instance: "lojinha", To: "5511999990000", Type: "midia",
		Category: "imagem", MediaID: "MEDIA-123",
	}
}

func TestValidateAcceptsMediaInEveryCategory(t *testing.T) {
	for _, cat := range []string{"imagem", "video", "audio", "documento", "sticker"} {
		p := mediaRequest()
		p.Category = cat
		if err := p.Validate(); err != nil {
			t.Errorf("categoria %q recusada: %v", cat, err)
		}
	}
}

// WITHOUT media_id there's nothing to send. And the field exists because the
// alternative — bytes in the body — is exactly what fails silently (see
// ErrMediaBase64).
func TestValidateMediaRequiresMediaIDAndCategory(t *testing.T) {
	cases := []struct {
		name  string
		p     Request
		field string
	}{
		{"sem media_id", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "imagem"}, "media_id"},
		{"media_id so com espaco", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "imagem", MediaID: "   "}, "media_id"},
		{"sem categoria", Request{Instance: "l", To: "5511999990000", Type: "midia",
			MediaID: "MEDIA-123"}, "categoria"},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("%s: erro = %v, quero ErrFieldRequired", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia o campo %q: %v", c.name, c.field, err)
		}
	}
}

// Category is the ONLY thing that says the shape of the Graph API body. A
// made-up category has no shape at all, and sending it anyway would be
// asking for a 400 from Meta discovered in production.
func TestValidateMediaRefusesUnknownCategory(t *testing.T) {
	for _, bad := range []string{"audiozinho", "IMAGEM", "foto", "arquivo"} {
		p := mediaRequest()
		p.Category = bad
		if err := p.Validate(); !errors.Is(err, ErrUnknownCategory) {
			t.Errorf("categoria %q: erro = %v, quero ErrUnknownCategory", bad, err)
		}
	}
}

// A FIELD ACCEPTED THAT THE BODY DOESN'T CARRY WOULD DISAPPEAR SILENTLY. The
// audio body and the sticker body assembled by MetaBody have nowhere to put
// a caption, and only the document one has a file name — so accepting these
// fields there would make the consumer's text vanish with no error at all,
// which is the most expensive failure mode in this project. Who decides is the
// table in meta/media.go, read by BOTH sides.
func TestValidateMediaRefusesAFieldTheCategoryDoesNotCarry(t *testing.T) {
	cases := []struct {
		name  string
		p     Request
		field string
	}{
		{"legenda em audio", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "audio", MediaID: "M", Caption: "ouve isso"}, "legenda"},
		{"legenda em sticker", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "sticker", MediaID: "M", Caption: "hehe"}, "legenda"},
		{"nome_arquivo em imagem", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "imagem", MediaID: "M", Filename: "foto.png"}, "nome_arquivo"},
		{"nome_arquivo em audio", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "audio", MediaID: "M", Filename: "nota.ogg"}, "nome_arquivo"},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if !errors.Is(err, ErrFieldForbidden) {
			t.Errorf("%s: erro = %v, quero ErrFieldForbidden", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia o campo %q: %v", c.name, c.field, err)
		}
	}
}

func TestValidateMediaAcceptsTheFieldsTheCategoryCarries(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "midia",
		Category: "documento", MediaID: "M", Caption: "a nota fiscal", Filename: "nota.pdf"}
	if err := p.Validate(); err != nil {
		t.Fatalf("documento com legenda e nome_arquivo recusado: %v", err)
	}
}

// PITFALL, and the reason the POST /v1/media route exists: without it,
// whoever has bytes hosts a public URL just for Meta to fetch — and the send
// fails SILENTLY when it doesn't fetch it. Refusing base64 without saying
// WHERE TO GO instead just swaps the silent failure for a rude one, so the
// error cites the route.
func TestValidateMediaRefusesBase64WithANamedErrorThatCitesTheRoute(t *testing.T) {
	cases := []struct {
		name string
		p    Request
	}{
		{"no media_id", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "imagem", MediaID: "data:image/png;base64,iVBORw0KGgo="}},
		{"na legenda", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "imagem", MediaID: "M", Caption: "veja: DATA:image/png;BASE64,iVBORw0="}},
		{"no nome_arquivo", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "documento", MediaID: "M", Filename: "data:application/pdf;base64,JVBER"}},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if !errors.Is(err, ErrMediaBase64) {
			t.Errorf("%s: erro = %v, quero ErrMediaBase64", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), "POST /v1/media") {
			t.Errorf("%s: o erro nao diz para onde ir: %v", c.name, err)
		}
	}
}

// THE OTHER HALF OF THE SAME GUARD: too broad and it would break legitimate
// conversation. "data" is a common word in Portuguese, and a file name can
// contain either of the two halves without being base64.
func TestValidateMediaDoesNotConfuseLegitimateTextWithBase64(t *testing.T) {
	cases := []Request{
		{Instance: "l", To: "5511999990000", Type: "midia", Category: "imagem",
			MediaID: "M", Caption: "data: 23/07, confira o comprovante"},
		{Instance: "l", To: "5511999990000", Type: "midia", Category: "documento",
			MediaID: "M", Filename: "base64-explicado.pdf"},
		{Instance: "l", To: "5511999990000", Type: "midia", Category: "imagem",
			MediaID: "M", Caption: "o data center caiu"},
	}
	for _, p := range cases {
		if err := p.Validate(); err != nil {
			t.Errorf("pedido legitimo recusado (%q): %v", p.Caption+p.Filename, err)
		}
	}
}

func TestValidateMediaNormalizesTheFieldsItAccepts(t *testing.T) {
	p := Request{Instance: " lojinha ", To: "5511999990000", Type: "midia",
		Category: " documento ", MediaID: "  MEDIA-123  ", Caption: "  a nota  ",
		Filename: "  nota.pdf  "}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.MediaID != "MEDIA-123" || p.Category != "documento" ||
		p.Caption != "a nota" || p.Filename != "nota.pdf" {
		t.Errorf("campos nao aparados: %+v", p)
	}
}

// ---------------------------------------------------------------------------
// template components: header and URL button (T-021)
// ---------------------------------------------------------------------------

func templateRequest() Request {
	return Request{Instance: "lojinha", To: "5511999990000", Type: "template",
		Template: "venda_confirmada", Language: "pt_BR"}
}

// HASH NON-REGRESSION, and it isn't cosmetic: the request hash is WRITTEN
// into the idempotency store, with a 72h TTL. If it changes, every
// legitimate retry of an already-reserved request starts getting a false 422
// — exactly the outcome ARMADILHAS.md calls "worse than not having the
// guard". The new fields are `omitempty`, so a request that doesn't use them
// serializes the same as before; this literal was CAPTURED from the
// implementation prior to T-021.
func TestVariablesOnlyTemplateHashDidNotChangeWithTheNewFields(t *testing.T) {
	p := Request{
		Instance: "lojinha", To: "5511999990000", Type: "template",
		Template: "lembrete", Language: "pt_BR", Variables: []string{"Maria", "19h"},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	const ofToday = "f232a847bfe1b6cc16dc685a30d268d9f531d2a6f5231155909f564cbf9b39fe"
	if got := RequestHash(p); got != ofToday {
		t.Errorf("hash mudou: %q, quero %q (todo retry dentro do TTL de 72h viraria 422 falso)", got, ofToday)
	}
}

// RAW `components` IS REFUSED, not ignored.
//
// The handler deserializes with json.Unmarshal without DisallowUnknownFields:
// without a declared field to refuse it, the consumer's `components` would
// vanish silently and the message would go out WITHOUT the header — 200 in
// the response, partial delivery on the customer's phone. And accepting it
// would be even worse: the discriminated union exists to make what Meta
// rejects inexpressible, and a passthrough would hand that entire class of
// error back to production.
func TestValidateRefusesRawComponentsInAnyType(t *testing.T) {
	cases := []Request{
		{Instance: "l", To: "5511999990000", Type: "template", Template: "t", Language: "pt_BR"},
		{Instance: "l", To: "5511999990000", Type: "texto", Text: "oi"},
	}
	raw := []byte(`[{"type":"header","parameters":[{"type":"document",` +
		`"document":{"link":"https://exemplo.com/recibo.pdf"}}]}]`)

	for _, p := range cases {
		p.Components = raw
		err := p.Validate()
		if !errors.Is(err, ErrRawComponents) {
			t.Errorf("tipo %q: erro = %v, quero ErrRawComponents", p.Type, err)
			continue
		}
		// The error has to say WHAT TO USE INSTEAD, otherwise whoever reads
		// it only finds out they can't — and goes back to looking for a way
		// to pass it through.
		if !strings.Contains(err.Error(), "cabecalho") || !strings.Contains(err.Error(), "botoes_template") {
			t.Errorf("tipo %q: o erro nao aponta a saida: %v", p.Type, err)
		}
	}
}

func TestValidateAcceptsHeaderAndTemplateButtonsInTemplate(t *testing.T) {
	cases := []struct {
		name string
		p    Request
	}{
		{"cabecalho de texto", func() Request {
			p := templateRequest()
			p.Header = &TemplateHeader{Type: "texto", Text: "Pedido 4210"}
			return p
		}()},
		{"cabecalho de documento com nome", func() Request {
			p := templateRequest()
			p.Header = &TemplateHeader{Type: "documento", MediaID: "M1", Filename: "recibo.pdf"}
			return p
		}()},
		{"cabecalho de imagem", func() Request {
			p := templateRequest()
			p.Header = &TemplateHeader{Type: "imagem", MediaID: "M1"}
			return p
		}()},
		{"botao url", func() Request {
			p := templateRequest()
			p.TemplateButtons = []TemplateButtonUnion{{Index: 0, Type: "url", Text: "tok-abc"}}
			return p
		}()},
		{"os tres juntos", func() Request {
			p := templateRequest()
			p.Variables = []string{"Maria"}
			p.Header = &TemplateHeader{Type: "documento", MediaID: "M1"}
			p.TemplateButtons = []TemplateButtonUnion{
				{Index: 0, Type: "url", Text: "t"},
				{Index: 1, Type: "url", Text: "u"},
			}
			return p
		}()},
	}
	for _, c := range cases {
		if err := c.p.Validate(); err != nil {
			t.Errorf("%s: recusado: %v", c.name, err)
		}
	}
}

// The header's type decides the SHAPE of the parameter. A type this gateway
// doesn't know how to assemble has no shape at all, and sending it anyway
// would be a 400 from Meta discovered in production. `audio` and `sticker` are
// VALID media categories in the `midia` type and don't count here — which is
// exactly why the test cites them.
func TestValidateHeaderRefusesUnknownType(t *testing.T) {
	for _, bad := range []string{"", "audio", "sticker", "IMAGEM", "arquivo", "location"} {
		p := templateRequest()
		p.Header = &TemplateHeader{Type: bad, MediaID: "M1"}
		if err := p.Validate(); !errors.Is(err, ErrUnknownHeaderType) {
			t.Errorf("cabecalho.tipo %q: erro = %v, quero ErrUnknownHeaderType", bad, err)
		}
	}
}

func TestValidateHeaderRequiresTheFieldOfItsType(t *testing.T) {
	cases := []struct {
		name   string
		header TemplateHeader
		field  string
	}{
		{"texto sem texto", TemplateHeader{Type: "texto"}, "cabecalho.texto"},
		{"texto so com espaco", TemplateHeader{Type: "texto", Text: "   "}, "cabecalho.texto"},
		{"imagem sem media_id", TemplateHeader{Type: "imagem"}, "cabecalho.media_id"},
		{"documento com media_id so de espaco", TemplateHeader{Type: "documento", MediaID: "  "},
			"cabecalho.media_id"},
	}
	for _, c := range cases {
		hdr := c.header
		p := templateRequest()
		p.Header = &hdr
		err := p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("%s: erro = %v, quero ErrFieldRequired", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia %q: %v", c.name, c.field, err)
		}
	}
}

// A FIELD ACCEPTED THAT THE PARAMETER DOESN'T CARRY WOULD DISAPPEAR SILENTLY —
// the same rule (and the same table, meta.AcceptsFilename) that already
// applies in the `midia` type.
func TestValidateHeaderRefusesAFieldTheTypeDoesNotCarry(t *testing.T) {
	cases := []struct {
		name   string
		header TemplateHeader
		field  string
	}{
		{"media_id em cabecalho de texto",
			TemplateHeader{Type: "texto", Text: "oi", MediaID: "M1"}, "cabecalho.media_id"},
		{"nome_arquivo em cabecalho de texto",
			TemplateHeader{Type: "texto", Text: "oi", Filename: "x.pdf"}, "cabecalho.nome_arquivo"},
		{"texto em cabecalho de imagem",
			TemplateHeader{Type: "imagem", MediaID: "M1", Text: "oi"}, "cabecalho.texto"},
		{"nome_arquivo em cabecalho de imagem",
			TemplateHeader{Type: "imagem", MediaID: "M1", Filename: "foto.png"}, "cabecalho.nome_arquivo"},
		{"nome_arquivo em cabecalho de video",
			TemplateHeader{Type: "video", MediaID: "M1", Filename: "v.mp4"}, "cabecalho.nome_arquivo"},
	}
	for _, c := range cases {
		hdr := c.header
		p := templateRequest()
		p.Header = &hdr
		err := p.Validate()
		if !errors.Is(err, ErrFieldForbidden) {
			t.Errorf("%s: erro = %v, quero ErrFieldForbidden", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia %q: %v", c.name, c.field, err)
		}
	}
}

// THE PITFALL THIS GUARD EXISTS TO PREVENT, and it's the same one that made
// POST /v1/media exist: a raw URL in place of media_id makes META GO FETCH
// the file. When it doesn't fetch it — host unavailable, TLS, 404, robots —
// there's no error at all on our side: the template arrives without the
// document, or doesn't arrive. The valid shape of media_id
// (meta.MediaIDValid) is the SAME one the rest of the project uses, not a
// copy.
func TestValidateHeaderRefusesARawURLInPlaceOfTheMediaID(t *testing.T) {
	cases := []string{
		"https://exemplo.com/recibo.pdf",
		"http://10.0.0.19/recibo.pdf",
		"//exemplo.com/recibo.pdf",
		"exemplo.com/recibo.pdf",
	}
	for _, url := range cases {
		p := templateRequest()
		p.Header = &TemplateHeader{Type: "documento", MediaID: url}
		err := p.Validate()
		if !errors.Is(err, ErrInvalidMediaID) {
			t.Errorf("media_id %q: erro = %v, quero ErrInvalidMediaID", url, err)
			continue
		}
		if !strings.Contains(err.Error(), "POST /v1/media") {
			t.Errorf("media_id %q: o erro nao diz para onde ir: %v", url, err)
		}
	}
}

// THE OTHER HALF OF THE SAME GUARD: too broad and it would refuse a
// legitimate media_id. The accepted shape is that of meta.MediaIDValid —
// letter, digit, `_` and `-` —, and it doesn't assert that Meta's id is
// numeric.
func TestValidateHeaderAcceptsALegitimateMediaID(t *testing.T) {
	for _, id := range []string{"1234567890", "MEDIA-123", "abc_DEF-9"} {
		p := templateRequest()
		p.Header = &TemplateHeader{Type: "documento", MediaID: id}
		if err := p.Validate(); err != nil {
			t.Errorf("media_id legitimo %q recusado: %v", id, err)
		}
	}
}

func TestValidateHeaderRefusesBase64WithAnErrorThatCitesTheRoute(t *testing.T) {
	p := templateRequest()
	p.Header = &TemplateHeader{Type: "documento",
		MediaID: "data:application/pdf;base64,JVBERi0xLjQK"}
	err := p.Validate()
	if !errors.Is(err, ErrMediaBase64) {
		t.Fatalf("erro = %v, quero ErrMediaBase64", err)
	}
	if !strings.Contains(err.Error(), "POST /v1/media") {
		t.Errorf("o erro nao diz para onde ir: %v", err)
	}
}

// A negative index doesn't exist in the template, and sending it that way
// would only discover the problem in Meta's response — or worse, not
// discover it.
func TestValidateTemplateButtonRefusesNegativeIndex(t *testing.T) {
	p := templateRequest()
	p.TemplateButtons = []TemplateButtonUnion{{Index: -1, Type: "url", Text: "tok"}}
	if err := p.Validate(); !errors.Is(err, ErrButtonIndex) {
		t.Fatalf("erro = %v, quero ErrButtonIndex", err)
	}
}

// Header only exists in template. Accepting it on another type would
// DISCARD it with no error at all during body assembly — the same reason
// `legenda` on audio is refused. (The sibling case of `botoes_template`
// outside of template is in TestValidateRefusesTemplateButtonsOutsideTemplate.)
func TestValidateRefusesHeaderOutsideTemplate(t *testing.T) {
	cases := []struct {
		name  string
		p     Request
		field string
	}{
		{"cabecalho em texto", Request{Instance: "l", To: "5511999990000", Type: "texto",
			Text: "oi", Header: &TemplateHeader{Type: "texto", Text: "x"}}, "cabecalho"},
		{"cabecalho em cta_url", Request{Instance: "l", To: "5511999990000", Type: "cta_url",
			Text: "veja", ButtonTitle: "Abrir", ButtonURL: "https://e.com",
			Header: &TemplateHeader{Type: "texto", Text: "x"}}, "cabecalho"},
		{"cabecalho em midia", Request{Instance: "l", To: "5511999990000", Type: "midia",
			Category: "imagem", MediaID: "M",
			Header: &TemplateHeader{Type: "imagem", MediaID: "M"}}, "cabecalho"},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if !errors.Is(err, ErrFieldForbidden) {
			t.Errorf("%s: erro = %v, quero ErrFieldForbidden", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia %q: %v", c.name, c.field, err)
		}
	}
}

// An unknown type keeps being reported as an unknown type: telling someone
// to fix the `cabecalho` of a request whose real problem is the `tipo` would
// make the person fix the wrong thing — the same reason ErrMixedButtons
// comes before the required fields.
func TestValidateReportsUnknownTypeBeforeForbiddenHeader(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "carrossel",
		Header: &TemplateHeader{Type: "texto", Text: "x"}}
	if err := p.Validate(); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("erro = %v, quero ErrUnknownType", err)
	}
}

// Trim BEFORE deciding, and return the trimmed value — otherwise the spaces
// travel to Meta. Holds for the new fields the same way it already held for
// the old ones.
func TestValidateNormalizesTheNewTemplateFields(t *testing.T) {
	p := templateRequest()
	p.Header = &TemplateHeader{Type: " documento ", MediaID: "  MEDIA-1  ",
		Filename: "  recibo.pdf  "}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Header.Type != "documento" || p.Header.MediaID != "MEDIA-1" ||
		p.Header.Filename != "recibo.pdf" {
		t.Errorf("cabecalho nao aparado: %+v", *p.Header)
	}
}

// ---------------------------------------------------------------------------
// template buttons: discriminated union by type (T-044)
// ---------------------------------------------------------------------------

func TestValidateAcceptsURLTemplateButton(t *testing.T) {
	p := templateRequest()
	p.TemplateButtons = []TemplateButtonUnion{{Index: 0, Type: "url", Text: "BR123456789BR"}}
	if err := p.Validate(); err != nil {
		t.Fatalf("botao de template tipo url recusado: %v", err)
	}
}

func TestValidateAcceptsQuickReplyTemplateButton(t *testing.T) {
	p := templateRequest()
	p.TemplateButtons = []TemplateButtonUnion{{Index: 1, Type: "resposta_rapida", Payload: "confirma:41"}}
	if err := p.Validate(); err != nil {
		t.Fatalf("botao de template tipo resposta_rapida recusado: %v", err)
	}
}

// (f) a made-up type is a NAMED error — same family as ErrUnknownHeaderType.
func TestValidateTemplateButtonRefusesUnknownType(t *testing.T) {
	p := templateRequest()
	p.TemplateButtons = []TemplateButtonUnion{{Index: 0, Type: "call", Text: "x"}}
	err := p.Validate()
	if !errors.Is(err, ErrUnknownTemplateButtonType) {
		t.Fatalf("erro = %v, quero ErrUnknownTemplateButtonType", err)
	}
	if !strings.Contains(err.Error(), `"call"`) {
		t.Errorf("o erro nao cita o tipo recusado: %v", err)
	}
}

// (g) an EMPTY payload is a named error, NOT a block with no parameter: Meta
// would accept a quick_reply block without `payload` and the click would
// come back with no recognizable id at all — the SAME defect this task
// exists to close, just caused by us instead of by Meta.
func TestValidateQuickReplyTemplateButtonRequiresPayload(t *testing.T) {
	for _, payload := range []string{"", "   "} {
		p := templateRequest()
		p.TemplateButtons = []TemplateButtonUnion{{Index: 0, Type: "resposta_rapida", Payload: payload}}
		err := p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("payload %q: erro = %v, quero ErrFieldRequired", payload, err)
			continue
		}
		if !strings.Contains(err.Error(), "botoes_template[0].payload") {
			t.Errorf("payload %q: o erro nao nomeia o campo: %v", payload, err)
		}
	}
}

func TestValidateURLTemplateButtonRequiresText(t *testing.T) {
	for _, text := range []string{"", "   "} {
		p := templateRequest()
		p.TemplateButtons = []TemplateButtonUnion{{Index: 0, Type: "url", Text: text}}
		err := p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("texto %q: erro = %v, quero ErrFieldRequired", text, err)
			continue
		}
		if !strings.Contains(err.Error(), "botoes_template[0].texto") {
			t.Errorf("texto %q: o erro nao nomeia o campo: %v", text, err)
		}
	}
}

// A field from the OTHER type would vanish silently during assembly
// (templateComponents) — same family of guard as cabecalho.
func TestValidateTemplateButtonRefusesAFieldTheTypeDoesNotCarry(t *testing.T) {
	cases := []struct {
		name string
		b    TemplateButtonUnion
	}{
		{"payload em tipo url", TemplateButtonUnion{Index: 0, Type: "url", Text: "x", Payload: "y"}},
		{"texto em tipo resposta_rapida", TemplateButtonUnion{Index: 0, Type: "resposta_rapida", Payload: "confirma:41", Text: "x"}},
	}
	for _, c := range cases {
		p := templateRequest()
		p.TemplateButtons = []TemplateButtonUnion{c.b}
		if err := p.Validate(); !errors.Is(err, ErrFieldForbidden) {
			t.Errorf("%s: erro = %v, quero ErrFieldForbidden", c.name, err)
		}
	}
}

// (d) a repeated index WITHIN botoes_template — which since T-045 is the
// ONLY way to repeat an index, because there's no longer a second field.
func TestValidateTemplateButtonRefusesRepeatedIndexInsideTheField(t *testing.T) {
	p := templateRequest()
	p.TemplateButtons = []TemplateButtonUnion{
		{Index: 0, Type: "url", Text: "a"},
		{Index: 0, Type: "resposta_rapida", Payload: "b"},
	}
	err := p.Validate()
	if !errors.Is(err, ErrButtonIndex) {
		t.Fatalf("erro = %v, quero ErrButtonIndex", err)
	}
	// Which index repeated is what the consumer needs to find the button in
	// the catalog — without it they only know "some" index repeated.
	if !strings.Contains(err.Error(), "0") {
		t.Errorf("o erro nao diz qual indice repetiu: %v", err)
	}
}

// `botoes_template` only exists in template — same family of guard as
// `cabecalho`.
func TestValidateRefusesTemplateButtonsOutsideTemplate(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "oi",
		TemplateButtons: []TemplateButtonUnion{{Index: 0, Type: "url", Text: "x"}}}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) {
		t.Fatalf("erro = %v, quero ErrFieldForbidden", err)
	}
	if !strings.Contains(err.Error(), "botoes_template") {
		t.Errorf("o erro nao nomeia o campo: %v", err)
	}
}

// THE NAME COLLISION THAT MADE THE FIELD BE CALLED `botoes_template`, AND NOT
// `botoes` (see the comment on TemplateButtonUnion): `botoes`
// (Request.Buttons, {id,titulo} from a regular interactive message) sent on a
// TEMPLATE request has to be a NAMED error, never a silent discard — and the
// error has to point to the RIGHT name of the new field, so whoever confuses
// the two similar names knows where to go.
func TestValidateRefusesInteractiveButtonsInTemplateWithAnErrorPointingAtTemplateButtons(t *testing.T) {
	p := templateRequest()
	p.Buttons = []Button{{ID: "SIM", Title: "Sim"}}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) {
		t.Fatalf("erro = %v, quero ErrFieldForbidden", err)
	}
	if !strings.Contains(err.Error(), "botoes_template") {
		t.Errorf("o erro nao aponta o campo certo (botoes_template): %v", err)
	}
}

// AND THE OPPOSITE: botoes_template on a request of tipo:"botoes" (regular
// interactive message) is also a named error.
func TestValidateRefusesTemplateButtonsInTheInteractiveButtonsType(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "botoes", Text: "confirma?",
		Buttons:         []Button{{ID: "SIM", Title: "Sim"}},
		TemplateButtons: []TemplateButtonUnion{{Index: 0, Type: "url", Text: "x"}}}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) {
		t.Fatalf("erro = %v, quero ErrFieldForbidden", err)
	}
	if !strings.Contains(err.Error(), "botoes_template") {
		t.Errorf("o erro nao nomeia o campo: %v", err)
	}
}

// Trim BEFORE deciding — same rule as every field in this file.
func TestValidateNormalizesTemplateButton(t *testing.T) {
	p := templateRequest()
	p.TemplateButtons = []TemplateButtonUnion{{Index: 0, Type: "  url  ", Text: "  tok  "}}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.TemplateButtons[0].Type != "url" || p.TemplateButtons[0].Text != "tok" {
		t.Errorf("botoes_template[0] nao aparado: %+v", p.TemplateButtons[0])
	}
}

// ---------------------------------------------------------------------------
// removal of `botoes_url` (T-045)
// ---------------------------------------------------------------------------

// THE INVALID STATE T-045 EXISTS TO KILL: the same button declared twice, in
// two fields. Up to T-044 it was REFUSED by an index guard that crossed
// `botoes_url` and `botoes_template`; refused is not inexpressible, and this
// project uses a discriminated union precisely to not depend on a guard.
//
// THE TEST ASKS THE TYPE, not a hand-written list: how many Request fields
// carry a template-button parameter (a LIST whose item has `Index`)? The
// answer has to be ONE. If someone resurrects a second field — for
// compatibility, for haste, or for not knowing about this task —, this test
// goes red NAMING the new field, even if the index guard stays green. It's
// the T-048 lesson applied to the struct instead of to the database schema
// (docs/ARMADILHAS.md): every hand-written list needs, alongside it,
// something that asks the source.
//
// `botoes` (Request.Buttons, {id,titulo}) does NOT count and can't count: it's
// the body of a regular interactive message, not a template parameter — and
// that's why the criterion is the `Index` field, which only the
// template-button parameter has.
func TestRequestHasASINGLETemplateButtonParameterField(t *testing.T) {
	kind := reflect.TypeOf(Request{})
	var findings []string
	for i := 0; i < kind.NumField(); i++ {
		field := kind.Field(i)
		if field.Type.Kind() != reflect.Slice {
			continue
		}
		item := field.Type.Elem()
		if item.Kind() != reflect.Struct {
			continue
		}
		if _, has := item.FieldByName("Index"); has {
			findings = append(findings, field.Name+" ("+field.Tag.Get("json")+")")
		}
	}
	if len(findings) != 1 || !strings.HasPrefix(findings[0], "TemplateButtons") {
		t.Fatalf("campos de parametro de botao de template = %v; quero exatamente um "+
			"(TemplateButtons). Dois campos tornam EXPRIMIVEL o mesmo botao declarado "+
			"duas vezes, que e o estado que a T-045 removeu", findings)
	}
}

// (b) from the T-045 Verify: a request that STILL sends `botoes_url` gets a
// 400 with a NAMED error, and the error points to the successor AND shows
// the translation.
//
// THE TEST GOES IN THROUGH JSON, not by filling in the Go field, on purpose:
// the real path is json.Unmarshal in the handler, WITHOUT
// DisallowUnknownFields, and that's exactly where a removed field would turn
// into a silent discard. Filling in the struct would prove validation;
// going in through JSON proves DESERIALIZATION, which is where the defect
// would live.
func TestValidateRefusesURLButtonsWithANamedErrorThatCitesTheSuccessor(t *testing.T) {
	bodies := map[string]string{
		"em template": `{"instancia":"lojinha","para":"5511999990000","tipo":"template",` +
			`"template":"equipamento_enviado","idioma":"pt_BR",` +
			`"botoes_url":[{"indice":0,"texto":"BR123456789BR"}]}`,
		// It doesn't exist in ANY type — it's not "forbidden outside of
		// template", it's removed. Reporting it as a forbidden field would send
		// someone to fix the wrong thing (move the field into a template, which
		// also doesn't work).
		"em texto": `{"instancia":"lojinha","para":"5511999990000","tipo":"texto","texto":"oi",` +
			`"botoes_url":[{"indice":0,"texto":"BR123456789BR"}]}`,
	}
	for name, body := range bodies {
		var p Request
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatalf("%s: Unmarshal: %v", name, err)
		}
		err := p.Validate()
		if !errors.Is(err, ErrRemovedURLButtons) {
			t.Errorf("%s: erro = %v, quero ErrRemovedURLButtons (ignorar em silencio "+
				"mandaria o template SEM o botao, com 200 na resposta)", name, err)
			continue
		}
		// Naming the successor isn't enough: the translation is mechanical and
		// fits in the message, and without it the consumer opens the contract
		// in the middle of the incident.
		if !strings.Contains(err.Error(), "botoes_template") {
			t.Errorf("%s: o erro nao aponta o sucessor: %v", name, err)
		}
		if !strings.Contains(err.Error(), `"tipo":"url"`) {
			t.Errorf("%s: o erro nao mostra a traducao: %v", name, err)
		}
	}
}

// THE OTHER HALF OF THE SAME PROOF, and it's the reason the refusal exists:
// if `botoes_url` were ignored, the assembled body would go out WITH NO
// BUTTON AT ALL. This test freezes that fact — the request is valid in
// everything except the removed field, and `components` comes out ABSENT.
//
// In other words: the price of ignoring it isn't "a lost field", it's a
// template delivered incomplete to the end customer, with 200 in the
// response and a billed conversation burned. The price of refusing it is a
// deploy on the consumer's side.
func TestIgnoredURLButtonsWouldProduceATemplateWithoutAButton(t *testing.T) {
	p := Request{
		Instance: "lojinha", To: "5511999990000", Type: "template",
		Template: "equipamento_enviado", Language: "pt_BR",
		RemovedURLButtons: json.RawMessage(`[{"indice":0,"texto":"BR123456789BR"}]`),
	}
	tpl, _ := MetaBody(p)["template"].(map[string]any)
	if value, has := tpl["components"]; has {
		t.Fatalf("botoes_url produziu components: %#v — ele nao deve produzir NADA; "+
			"a defesa e a recusa em Validate, nao a montagem", value)
	}
}

// (d) from the T-045 Verify: the idempotency hash of whoever uses
// `botoes_template` does NOT change with the removal of `botoes_url`.
//
// This isn't cosmetic: the hash is WRITTEN into the idempotency store with a
// 72h TTL, and if it changes every legitimate retry within the window
// becomes a false 422. The literals were CAPTURED before the removal,
// running the T-044 code — if they had been generated afterward, they'd
// prove the new code agrees with itself, which is the opposite of what this
// test exists to say.
func TestHashOfWhoeverUsesTemplateButtonsDidNotChangeWithTheRemovalOfURLButtons(t *testing.T) {
	cases := []struct {
		name   string
		p      Request
		before string
	}{
		{"so botao de url", Request{
			Instance: "lojinha", To: "5511999990000", Type: "template",
			Template: "equipamento_enviado", Language: "pt_BR",
			TemplateButtons: []TemplateButtonUnion{{Index: 1, Type: "url", Text: "abc123"}},
		}, "f14dd6eb107e01fad46e224ca946d9119b78fcaec62a5afa418ff4c9ce56835f"},
		{"os dois tipos juntos", Request{
			Instance: "lojinha", To: "5511999990000", Type: "template",
			Template: "confirma_agendamento", Language: "pt_BR",
			TemplateButtons: []TemplateButtonUnion{
				{Index: 0, Type: "url", Text: "BR123"},
				{Index: 1, Type: "resposta_rapida", Payload: "confirma:41"},
			},
		}, "08309da488ca39a95cff61db0e2a72f54abe8b1c3393e57589b8b9cabbc402b5"},
	}
	for _, c := range cases {
		if err := c.p.Validate(); err != nil {
			t.Fatalf("%s: Validate: %v", c.name, err)
		}
		if got := RequestHash(c.p); got != c.before {
			t.Errorf("%s: hash mudou: %q, quero %q (todo retry dentro do TTL de 72h "+
				"viraria 422 falso)", c.name, got, c.before)
		}
	}
}

// ---------------------------------------------------------------------------
// type `reacao` (T-024)
// ---------------------------------------------------------------------------

// emojiPtr exists for the same reason as floatPtr, further below: Emoji is
// *string on purpose (see the comment on ReactionRequest), and a struct literal
// needs the address of a variable, not a raw string.
func emojiPtr(s string) *string { return &s }

func reactionRequest() Request {
	return Request{
		Instance: "lojinha", To: "5511999990000", Type: "reacao",
		Reaction: &ReactionRequest{Target: "wamid.ABC", Emoji: emojiPtr("\U0001F44D")},
	}
}

func TestValidateAcceptsReaction(t *testing.T) {
	p := reactionRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("reacao recusada: %v", err)
	}
}

func TestValidateReactionRequiresTheReactionField(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "reacao"}
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) {
		t.Fatalf("erro = %v, quero ErrFieldRequired", err)
	}
	if !strings.Contains(err.Error(), "reacao") {
		t.Errorf("o erro nao nomeia o campo: %v", err)
	}
}

// (d) from the T-024 Verify: a missing alvo is a NAMED error, never an
// empty message_id sent to Meta silently.
func TestValidateReactionRequiresTarget(t *testing.T) {
	for _, target := range []string{"", "   ", "\t"} {
		p := reactionRequest()
		p.Reaction.Target = target
		err := p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("alvo %q: erro = %v, quero ErrFieldRequired", target, err)
			continue
		}
		if !strings.Contains(err.Error(), "reacao.alvo") {
			t.Errorf("alvo %q: o erro nao nomeia reacao.alvo: %v", target, err)
		}
	}
}

// EMPTY `emoji` REMOVES THE REACTION (T-027) — source: experiment with a
// consumer-a device, 2026-07-26 10:15 -03 (see the comment on ReactionRequest).
// Blank space counts: trim to "" before deciding, same rule as the rest of
// the file.
func TestValidateReactionAcceptsEmptyEmojiAsRemoval(t *testing.T) {
	for _, emoji := range []string{"", "   ", "\t"} {
		p := reactionRequest()
		p.Reaction.Emoji = emojiPtr(emoji)
		if err := p.Validate(); err != nil {
			t.Errorf("emoji %q: recusado = %v, quero aceito (remocao)", emoji, err)
			continue
		}
		if p.Reaction.Emoji == nil || *p.Reaction.Emoji != "" {
			t.Errorf("emoji %q: apos Validate = %v, quero ponteiro para \"\"", emoji, p.Reaction.Emoji)
		}
	}
}

// ABSENT `emoji` (key not sent, Emoji == nil) STAYS a required-field error —
// it doesn't turn into removal. The distinction with the test above IS the
// contract: an empty string is a choice, a nil pointer is a key never sent.
// See the comment on ReactionRequest for the reason for the asymmetry with
// receiving.
func TestValidateReactionRequiresEmoji(t *testing.T) {
	p := reactionRequest()
	p.Reaction.Emoji = nil
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) {
		t.Fatalf("erro = %v, quero ErrFieldRequired", err)
	}
	if !strings.Contains(err.Error(), "reacao.emoji") {
		t.Errorf("o erro nao nomeia reacao.emoji: %v", err)
	}
}

func TestValidateReactionNormalizesTheFields(t *testing.T) {
	p := reactionRequest()
	p.Reaction.Target = "  wamid.ABC  "
	p.Reaction.Emoji = emojiPtr("  \U0001F44D  ")
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Reaction.Target != "wamid.ABC" || p.Reaction.Emoji == nil || *p.Reaction.Emoji != "\U0001F44D" {
		t.Errorf("reacao nao aparada: %+v", *p.Reaction)
	}
}

// `reacao` only has shape in the MetaBody of its own type. Accepting it
// on a different type would silently discard it during assembly — the most
// expensive failure mode in this project (same family of guard as
// cabecalho/botoes_url).
func TestValidateRefusesReactionOutsideTheReactionType(t *testing.T) {
	p := textRequest()
	p.Reaction = &ReactionRequest{Target: "wamid.ABC", Emoji: emojiPtr("\U0001F44D")}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) {
		t.Fatalf("erro = %v, quero ErrFieldForbidden", err)
	}
	if !strings.Contains(err.Error(), "reacao") {
		t.Errorf("o erro nao nomeia o campo: %v", err)
	}
}

// ---------------------------------------------------------------------------
// type `localizacao` (T-024)
// ---------------------------------------------------------------------------

func floatPtr(v float64) *float64 { return &v }

func locationRequest() Request {
	return Request{
		Instance: "lojinha", To: "5511999990000", Type: "localizacao",
		Location: &LocationRequest{Latitude: floatPtr(37.44), Longitude: floatPtr(-122.16)},
	}
}

func TestValidateAcceptsLocation(t *testing.T) {
	p := locationRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("localizacao recusada: %v", err)
	}
}

func TestValidateLocationRequiresTheLocationField(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "localizacao"}
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) {
		t.Fatalf("erro = %v, quero ErrFieldRequired", err)
	}
	if !strings.Contains(err.Error(), "localizacao") {
		t.Errorf("o erro nao nomeia o campo: %v", err)
	}
}

// Central pitfall of T-024 (docs/ARMADILHAS.md, "Validação"): 0 is a valid
// coordinate (the crossing of the Greenwich meridian with the equator).
// latitude/longitude NIL (absent) is an error; latitude/longitude ZERO is not.
func TestValidateLocationAcceptsZeroLatitudeAndLongitude(t *testing.T) {
	p := locationRequest()
	p.Location.Latitude = floatPtr(0)
	p.Location.Longitude = floatPtr(0)
	if err := p.Validate(); err != nil {
		t.Fatalf("latitude/longitude zero recusadas: %v", err)
	}
}

func TestValidateLocationRequiresLatitudeAndLongitude(t *testing.T) {
	cases := []struct {
		name  string
		p     func() Request
		field string
	}{
		{"sem latitude", func() Request {
			p := locationRequest()
			p.Location.Latitude = nil
			return p
		}, "localizacao.latitude"},
		{"sem longitude", func() Request {
			p := locationRequest()
			p.Location.Longitude = nil
			return p
		}, "localizacao.longitude"},
	}
	for _, c := range cases {
		p := c.p()
		err := p.Validate()
		if !errors.Is(err, ErrFieldRequired) {
			t.Errorf("%s: erro = %v, quero ErrFieldRequired", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: o erro nao nomeia %q: %v", c.name, c.field, err)
		}
	}
}

func TestValidateLocationNormalizesNameAndAddress(t *testing.T) {
	p := locationRequest()
	p.Location.Name = "  Cafe de Teste  "
	p.Location.Address = "  Rua de Teste, 101  "
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Location.Name != "Cafe de Teste" || p.Location.Address != "Rua de Teste, 101" {
		t.Errorf("nome/endereco nao aparados: %+v", *p.Location)
	}
}

// `localizacao` only has shape in the MetaBody of its own type. Same
// family of guard as cabecalho/botoes_url and reacao.
func TestValidateRefusesLocationOutsideTheLocationType(t *testing.T) {
	p := textRequest()
	p.Location = &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(1)}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) {
		t.Fatalf("erro = %v, quero ErrFieldForbidden", err)
	}
	if !strings.Contains(err.Error(), "localizacao") {
		t.Errorf("o erro nao nomeia o campo: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HASH NON-REGRESSION (T-024)
// ---------------------------------------------------------------------------

// The new fields (Reaction, Location) have to be omitempty, otherwise the
// hash of ANY request that doesn't use them would change — and that hash is
// WRITTEN into the idempotency store with a 72h TTL (docs/ARMADILHAS.md,
// "Field NOVO no `Request` sem `omitempty` muda o hash de TODO pedido antigo").
// The literal was captured from the implementation PRIOR to this task,
// before Reaction/Location existed on the Request type.
func TestTextHashDidNotChangeWithTheNewReactionAndLocationFields(t *testing.T) {
	p := textRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	const ofToday = "0a8d0873ee9078a51ac5d9f6391eede10b734515ee2ad02accb0c41ae641d281"
	if got := RequestHash(p); got != ofToday {
		t.Errorf("hash mudou: %q, quero %q (todo retry dentro do TTL de 72h viraria 422 falso)", got, ofToday)
	}
}

// The new fields (HeaderText, Footer, T-137) have to be omitempty for
// the SAME reason: without it, the hash of ANY botoes/cta_url request that
// doesn't use the fields would change, and the legitimate retry of an
// already-reserved request would get a false 422. The literals were
// captured from the implementation PRIOR to this task, before
// HeaderText/Footer existed on the Request type.
func TestButtonsAndCtaURLHashDidNotChangeWithTheNewHeaderAndFooterFields(t *testing.T) {
	pButtons := Request{
		Instance: "lojinha", To: "5511999990000", Type: "botoes", Text: "confirma?",
		Buttons: []Button{{ID: "SIM", Title: "Sim"}},
	}
	if err := pButtons.Validate(); err != nil {
		t.Fatalf("Validate botoes: %v", err)
	}
	const todaysButtonsHash = "61504f54daef797e4125eeaec2d048e9c513a86ee996656844d7ef4def616d21"
	if got := RequestHash(pButtons); got != todaysButtonsHash {
		t.Errorf("hash de botoes mudou: %q, quero %q", got, todaysButtonsHash)
	}

	pCta := Request{
		Instance: "lojinha", To: "5511999990000", Type: "cta_url", Text: "veja",
		ButtonTitle: "Abrir", ButtonURL: "https://exemplo.com",
	}
	if err := pCta.Validate(); err != nil {
		t.Fatalf("Validate cta_url: %v", err)
	}
	const todaysCtaHash = "580017af6ba3d197fea903274e5d8e051b63ba054cfc53a99548e1c8e5aa22cf"
	if got := RequestHash(pCta); got != todaysCtaHash {
		t.Errorf("hash de cta_url mudou: %q, quero %q", got, todaysCtaHash)
	}
}

// ---------------------------------------------------------------------------
// T-145 -- tipo:"lista", the LIST interactive message. The six caps
// (botao_titulo, secoes, secoes[].titulo, itens[].id, itens[].titulo,
// itens[].descricao) came from the official DOCUMENTATION
// (developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-list-messages,
// read on 2026-08-20) -- NOT from measurement against real production,
// unlike the button-quantity caps (T-140/T-143). See the comment on the
// limits in mensagem.go for why the provenance matters.
// ---------------------------------------------------------------------------

// baseListRequest is a minimal, valid tipo:"lista": one section, one item.
// Every cap test starts from it and changes ONLY the field under test, so
// the error can't come from another field by accident.
func baseListRequest() Request {
	return Request{
		Instance: "l", To: "5511999990000", Type: "lista", Text: "escolha uma opcao",
		ButtonTitle: "Ver opcoes",
		Sections: []ListSection{
			{Title: "Categoria 1", Items: []ListItem{
				{ID: "item-1", Title: "Item 1", Description: "descricao do item 1"},
			}},
		},
	}
}

// sectionsWithNItems assembles n sections, one item each, unique ids/titles per
// section -- used by the QUANTITY cap tests (sections and summed items),
// where the content of each section doesn't matter, only the count.
func sectionsWithNItems(n int) []ListSection {
	sections := make([]ListSection, n)
	for i := 0; i < n; i++ {
		sections[i] = ListSection{
			Title: fmt.Sprintf("Secao %d", i),
			Items: []ListItem{{ID: fmt.Sprintf("id-%d", i), Title: fmt.Sprintf("Item %d", i)}},
		}
	}
	return sections
}

func TestValidateAcceptsList(t *testing.T) {
	p := baseListRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

func TestValidateListRequiresText(t *testing.T) {
	p := baseListRequest()
	p.Text = ""
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "texto") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando texto", err)
	}
}

func TestValidateListRequiresSections(t *testing.T) {
	p := baseListRequest()
	p.Sections = nil
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "secoes") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando secoes", err)
	}
}

func TestValidateListRequiresAnItemInEachSection(t *testing.T) {
	p := baseListRequest()
	p.Sections[0].Items = nil
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) {
		t.Fatalf("erro = %v, quero ErrFieldRequired", err)
	}
	if !strings.Contains(err.Error(), "secoes[0].itens") {
		t.Errorf("o erro nao nomeia secoes[0].itens: %v", err)
	}
}

// The action button's label (botao_titulo) is REUSED from cta_url (T-145):
// same field, same 20-rune cap.
func TestValidateListRefusesLongButtonTitle(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"20 runas passa", strings.Repeat("a", 20), nil},
		{"21 runas recusa", strings.Repeat("a", 21), ErrFieldTooLong},
	}
	for _, c := range cases {
		p := baseListRequest()
		p.ButtonTitle = c.value
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "botao_titulo") {
			t.Errorf("%s: erro = %v, quero %v nomeando botao_titulo", c.name, err, c.want)
		}
	}
}

func TestValidateListRequiresButtonTitle(t *testing.T) {
	p := baseListRequest()
	p.ButtonTitle = ""
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "botao_titulo") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando botao_titulo", err)
	}
}

// Cap of 10 SECTIONS (documented, not measured).
func TestValidateListRefusesMoreThanTenSections(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want error
	}{
		{"10 secoes passa (teto exato)", 10, nil},
		{"11 secoes recusa", 11, ErrFieldTooLong},
	}
	for _, c := range cases {
		p := baseListRequest()
		p.Sections = sectionsWithNItems(c.n)
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "secoes") {
			t.Errorf("%s: erro = %v, quero %v nomeando secoes", c.name, err, c.want)
		}
	}
}

// Cap of 24 runes on secoes[].titulo.
func TestValidateListRefusesLongSectionTitle(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"24 runas passa", strings.Repeat("a", 24), nil},
		{"25 runas recusa", strings.Repeat("a", 25), ErrFieldTooLong},
	}
	for _, c := range cases {
		p := baseListRequest()
		p.Sections[0].Title = c.value
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "secoes[0].titulo") {
			t.Errorf("%s: erro = %v, quero %v nomeando secoes[0].titulo", c.name, err, c.want)
		}
	}
}

// Cap of 200 runes on itens[].id.
func TestValidateListRefusesLongItemID(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"200 runas passa", strings.Repeat("a", 200), nil},
		{"201 runas recusa", strings.Repeat("a", 201), ErrFieldTooLong},
	}
	for _, c := range cases {
		p := baseListRequest()
		p.Sections[0].Items[0].ID = c.value
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "secoes[0].itens[0].id") {
			t.Errorf("%s: erro = %v, quero %v nomeando secoes[0].itens[0].id", c.name, err, c.want)
		}
	}
}

// Cap of 24 runes on itens[].titulo. A second item at index 1 proves the
// error names the RIGHT index, following the T-140 pattern.
func TestValidateListRefusesLongItemTitle(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"24 runas passa", strings.Repeat("a", 24), nil},
		{"25 runas recusa", strings.Repeat("a", 25), ErrFieldTooLong},
	}
	for _, c := range cases {
		p := baseListRequest()
		p.Sections[0].Items = append(p.Sections[0].Items, ListItem{ID: "item-2", Title: c.value})
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "secoes[0].itens[1].titulo") {
			t.Errorf("%s: erro = %v, quero %v nomeando secoes[0].itens[1].titulo", c.name, err, c.want)
		}
	}
}

// Cap of 72 runes on itens[].descricao. Description is OPTIONAL -- empty never
// counts toward the cap (see TestValidateAcceptsList).
func TestValidateListRefusesLongItemDescription(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"72 runas passa", strings.Repeat("a", 72), nil},
		{"73 runas recusa", strings.Repeat("a", 73), ErrFieldTooLong},
	}
	for _, c := range cases {
		p := baseListRequest()
		p.Sections[0].Items[0].Description = c.value
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "secoes[0].itens[0].descricao") {
			t.Errorf("%s: erro = %v, quero %v nomeando secoes[0].itens[0].descricao", c.name, err, c.want)
		}
	}
}

// THE CAP THAT ISN'T PER SECTION: the SUMMED total of items across all
// sections is at most 10 -- 3 sections of 4 items (12 total, each section
// alone within any reasonable limit) is REFUSED. It's the easiest point in
// this task to implement wrong: a len(secao.Items) > 10 per section would
// pass this case and would only break as a 400 from Meta in production.
func TestValidateRefusesListWithSummedItemsAboveTheCap(t *testing.T) {
	threeSectionsOfFour := make([]ListSection, 3)
	for i := range threeSectionsOfFour {
		items := make([]ListItem, 4)
		for j := range items {
			items[j] = ListItem{ID: fmt.Sprintf("s%d-i%d", i, j), Title: fmt.Sprintf("Item %d.%d", i, j)}
		}
		threeSectionsOfFour[i] = ListSection{Title: fmt.Sprintf("Secao %d", i), Items: items}
	}

	p := baseListRequest()
	p.Sections = threeSectionsOfFour
	err := p.Validate()
	if !errors.Is(err, ErrFieldTooLong) {
		t.Fatalf("3 secoes de 4 itens (12 no total): erro = %v, quero ErrFieldTooLong", err)
	}
	if !strings.Contains(err.Error(), "12") || !strings.Contains(err.Error(), "10") {
		t.Errorf("o erro nao cita quantos vieram (12) e o maximo (10): %v", err)
	}

	// Non-regression: 10 summed items (the exact cap, split across two
	// sections of 5) keeps passing.
	tenSummed := []ListSection{
		{Title: "Secao 0", Items: make([]ListItem, 5)},
		{Title: "Secao 1", Items: make([]ListItem, 5)},
	}
	for _, s := range tenSummed {
		for j := range s.Items {
			s.Items[j] = ListItem{ID: fmt.Sprintf("%s-%d", s.Title, j), Title: fmt.Sprintf("Item %d", j)}
		}
	}
	p2 := baseListRequest()
	p2.Sections = tenSummed
	if err := p2.Validate(); err != nil {
		t.Errorf("10 itens somados (teto exato): Validate = %v, quero nil", err)
	}
}

// secoes only has shape in the "lista" branch -- sent on another type, it
// would be discarded silently during assembly without this guard.
func TestValidateRefusesSectionsOutsideList(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "oi",
		Sections: []ListSection{{Title: "S", Items: []ListItem{{ID: "1", Title: "I"}}}}}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "secoes") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando secoes", err)
	}
}

// botao_titulo is now reused by cta_url AND lista (T-145) -- sent on any
// OTHER type is still forbidden, same rule as always.
func TestValidateRefusesButtonTitleOutsideCtaURLAndList(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "oi",
		ButtonTitle: "Abrir"}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "botao_titulo") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando botao_titulo", err)
	}
}

// Lista accepts cabecalho_texto/rodape by the SAME rule as botoes/cta_url
// (T-137, extended by T-145).
func TestValidateListAcceptsHeaderTextAndFooter(t *testing.T) {
	p := baseListRequest()
	p.HeaderText = "Novidades"
	p.Footer = "Oferta valida hoje"
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

// Normalization: extra space in titulo/id/descricao is trimmed BEFORE
// assembly, same rule as the rest of the file -- otherwise the body would
// carry the space to Meta.
func TestValidateListNormalizesTheFields(t *testing.T) {
	p := baseListRequest()
	p.Sections[0].Title = "  Categoria 1  "
	p.Sections[0].Items[0].ID = "  item-1  "
	p.Sections[0].Items[0].Title = "  Item 1  "
	p.Sections[0].Items[0].Description = "  descricao  "
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
	if p.Sections[0].Title != "Categoria 1" || p.Sections[0].Items[0].ID != "item-1" ||
		p.Sections[0].Items[0].Title != "Item 1" || p.Sections[0].Items[0].Description != "descricao" {
		t.Errorf("secoes nao normalizadas: %+v", p.Sections[0])
	}
}

// ---------------------------------------------------------------------------
// type `contatos` (T-146)
// ---------------------------------------------------------------------------

func contactsRequest() Request {
	return Request{
		Instance: "l", To: "5511999990000", Type: "contatos",
		Contacts: []Contact{
			{Name: ContactName{FormattedName: "Joao Vendedor"}},
		},
	}
}

func TestValidateAcceptsContacts(t *testing.T) {
	p := contactsRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

// Complete card, with one item in each nested list -- checks that none of
// the ~25 optional fields is refused by mistake (validation only requires
// formatted_name and the birthday format, see the comment on
// validateContacts).
func TestValidateAcceptsAFullContact(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "contatos",
		Contacts: []Contact{{
			Name: ContactName{
				FormattedName: "Joao da Silva",
				FirstName:     "Joao", LastName: "Silva", MiddleName: "da",
				Prefix: "Sr.", Suffix: "Jr.",
			},
			Addresses: []ContactAddress{
				{Street: "Rua A, 100", City: "SP", State: "SP", Zip: "01000-000",
					Country: "Brasil", CountryCode: "BR", Type: "WORK"},
			},
			Birthday: "1990-05-20",
			Emails:   []ContactEmail{{Email: "joao@example.com", Type: "WORK"}},
			Org:      &ContactOrg{Company: "Acme", Department: "Vendas", Title: "Gerente"},
			Phones:   []ContactPhone{{Phone: "+55 11 99999-0000", Type: "CELL", WaID: "5511999990000"}},
			Urls:     []ContactURL{{URL: "https://example.com", Type: "WORK"}},
		}},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

func TestValidateContactsRequiresAtLeastOneCard(t *testing.T) {
	p := contactsRequest()
	p.Contacts = nil
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "contatos") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando contatos", err)
	}
}

// The ONLY required field on a card is name.formatted_name (source:
// official doc, read on 2026-08-20 -- see the comment on ContactName).
// The error has to name the card's INDEX, not just "formatted_name" -- it's
// the only way for the consumer to find which of several cards is
// incomplete.
func TestValidateContactsRequiresFormattedName(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "contatos",
		Contacts: []Contact{
			{Name: ContactName{FormattedName: "Primeiro"}},
			{Name: ContactName{FormattedName: "  "}},
		},
	}
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) {
		t.Fatalf("erro = %v, quero ErrFieldRequired", err)
	}
	if !strings.Contains(err.Error(), "contatos[1].name.formatted_name") {
		t.Errorf("o erro nao nomeia contatos[1].name.formatted_name: %v", err)
	}
}

// birthday is the ONLY field with a declared format in Meta's doc (T-146,
// item 5): when filled in it has to be YYYY-MM-DD. Empty stays optional.
func TestValidateContactsRefusesBirthdayWithInvalidFormat(t *testing.T) {
	for _, cases := range []string{"20-05-1990", "1990/05/20", "1990-13-01", "1990-02-30", "hoje"} {
		p := contactsRequest()
		p.Contacts[0].Birthday = cases
		err := p.Validate()
		if !errors.Is(err, ErrInvalidContactDate) {
			t.Errorf("birthday %q: erro = %v, quero ErrInvalidContactDate", cases, err)
			continue
		}
		if !strings.Contains(err.Error(), "contatos[0].birthday") {
			t.Errorf("birthday %q: o erro nao nomeia contatos[0].birthday: %v", cases, err)
		}
	}
}

func TestValidateContactsAcceptsValidBirthday(t *testing.T) {
	p := contactsRequest()
	p.Contacts[0].Birthday = "1990-05-20"
	if err := p.Validate(); err != nil {
		t.Fatalf("birthday valido recusado: %v", err)
	}
}

func TestValidateContactsAcceptsAbsentBirthday(t *testing.T) {
	p := contactsRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

// THERE IS NO MADE-UP CAP (T-146, item 4): not on the number of cards, not
// on the size of any sub-field. A card with a large number of phones
// (nothing the doc declares as a limit) has to pass.
func TestValidateContactsImposesNoCountCap(t *testing.T) {
	p := contactsRequest()
	for i := 0; i < 50; i++ {
		p.Contacts[0].Phones = append(p.Contacts[0].Phones, ContactPhone{Phone: fmt.Sprintf("+55119999%04d", i)})
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil (nenhum teto de phones e documentado)", err)
	}
}

func TestValidateContactsNormalizesFormattedNameAndBirthday(t *testing.T) {
	p := contactsRequest()
	p.Contacts[0].Name.FormattedName = "  Joao Vendedor  "
	p.Contacts[0].Birthday = "  1990-05-20  "
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Contacts[0].Name.FormattedName != "Joao Vendedor" || p.Contacts[0].Birthday != "1990-05-20" {
		t.Errorf("contato nao aparado: %+v", p.Contacts[0])
	}
}

// `contatos` only has shape in the MetaBody of its own type. Accepting
// it on a different type would discard it silently during assembly -- same
// family of guard as reacao/localizacao.
func TestValidateRefusesContactsOutsideTheContactsType(t *testing.T) {
	p := textRequest()
	p.Contacts = []Contact{{Name: ContactName{FormattedName: "Joao"}}}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "contatos") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando contatos", err)
	}
}

// HASH NON-REGRESSION: `Contacts` is omitempty, so the hash of a texto
// request that doesn't use the field has to stay the SAME as before this
// task existed -- same literal as
// TestTextHashDidNotChangeWithTheNewReactionAndLocationFields.
func TestTextHashDidNotChangeWithTheNewContactsField(t *testing.T) {
	p := textRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	const ofToday = "0a8d0873ee9078a51ac5d9f6391eede10b734515ee2ad02accb0c41ae641d281"
	if got := RequestHash(p); got != ofToday {
		t.Errorf("hash mudou: %q, quero %q (o campo Contacts novo tem de ser omitempty)", got, ofToday)
	}
}

// ---------------------------------------------------------------------------
// type `pedir_localizacao` (T-150)
// ---------------------------------------------------------------------------

func baseLocationAskRequest() Request {
	return Request{
		Instance: "l", To: "5511999990000", Type: "pedir_localizacao",
		Text: "Pode compartilhar sua localizacao?",
	}
}

func TestValidateAcceptsLocationRequest(t *testing.T) {
	p := baseLocationAskRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

func TestValidateLocationRequestRequiresText(t *testing.T) {
	p := baseLocationAskRequest()
	p.Text = ""
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "texto") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando texto", err)
	}
}

// `pedir_localizacao` HAS NEITHER cabecalho NOR rodape (T-150): the doc
// describes the object with THREE fields (type, body, action) and no
// others. The guard that refuses this is the SAME cross-cutting guard that
// already protects texto/midia/reacao/etc -- this type simply wasn't added
// to its exception list.
func TestValidateLocationRequestRefusesHeaderText(t *testing.T) {
	p := baseLocationAskRequest()
	p.HeaderText = "Ola"
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "cabecalho_texto") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando cabecalho_texto", err)
	}
}

func TestValidateLocationRequestRefusesFooter(t *testing.T) {
	p := baseLocationAskRequest()
	p.Footer = "Ate mais"
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "rodape") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando rodape", err)
	}
}

// THERE IS NO CAP on texto, even though Meta's doc declares 1024 characters
// for `body.text` (source in the comment on the "pedir_localizacao" case, in
// mensagem.go). It's the decision recorded in T-143: the gateway doesn't
// mirror Meta's limits table. This test FREEZES that decision -- if someone
// ever invents a cap here, this test fails.
func TestValidateAcceptsLocationRequestWithLongText(t *testing.T) {
	p := baseLocationAskRequest()
	p.Text = strings.Repeat("a", 2000)
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil (T-143: sem teto de texto)", err)
	}
}

// baseFlowRequest is the minimal valid request for `tipo:"flow"` (T-154):
// "navigate" (the default) WITH "tela" filled in, "id" (never together with
// "nome").
func baseFlowRequest() Request {
	return Request{
		Instance: "l", To: "5511999990000", Type: "flow", Text: "Preencha seus dados",
		ButtonTitle: "Agendar",
		Flow: &FlowRequest{
			ID:     "123456789",
			Token:  "agendamento-4471",
			Action: "navigate",
			Screen: "TELA_INICIAL",
		},
	}
}

func TestValidateAcceptsFlow(t *testing.T) {
	p := baseFlowRequest()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

// An absent `acao` normalizes to "navigate" (the third-party source's
// default) -- see the comment on FlowRequest in mensagem.go. With the
// default, `tela` stays required.
func TestValidateFlowAbsentActionNormalizesToNavigate(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.Action = ""
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
	if p.Flow.Action != "navigate" {
		t.Errorf("fluxo.acao = %q depois de Validate, quero \"navigate\" (o default)", p.Flow.Action)
	}
}

// `acao: "data_exchange"` does NOT require `tela` -- only "navigate" does,
// because the third-party source says the payload is only required in that
// case.
func TestValidateFlowDataExchangeWithoutScreenPasses(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.Action = "data_exchange"
	p.Flow.Screen = ""
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil (data_exchange nao exige tela)", err)
	}
}

// `acao: "navigate"` WITHOUT `tela` is refused -- the source says the
// payload is required in that case.
func TestValidateFlowNavigateWithoutScreenRefuses(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.Screen = ""
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "fluxo.tela") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando fluxo.tela", err)
	}
}

// `fluxo.id` and `fluxo.nome` together are refused -- mutually exclusive.
func TestValidateFlowRefusesIDAndNameTogether(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.Name = "meu-fluxo"
	err := p.Validate()
	if !errors.Is(err, ErrInvalidFlowIDName) {
		t.Fatalf("erro = %v, quero ErrInvalidFlowIDName", err)
	}
}

// `fluxo.id` and `fluxo.nome` both absent are refused -- at least one is
// required.
func TestValidateFlowRefusesIDAndNameBothAbsent(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.ID = ""
	err := p.Validate()
	if !errors.Is(err, ErrInvalidFlowIDName) {
		t.Fatalf("erro = %v, quero ErrInvalidFlowIDName", err)
	}
}

// `fluxo.nome` alone (without `id`) is also a valid request -- proves the
// exclusivity is really OR, not "only id works".
func TestValidateFlowAcceptsNameOnly(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.ID = ""
	p.Flow.Name = "meu-fluxo"
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
}

func TestValidateFlowRequiresToken(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.Token = ""
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "fluxo.token") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando fluxo.token", err)
	}
}

func TestValidateFlowRequires(t *testing.T) {
	p := baseFlowRequest()
	p.Flow = nil
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "fluxo") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando fluxo", err)
	}
}

func TestValidateFlowRefusesUnknownAction(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.Action = "voar"
	err := p.Validate()
	if !errors.Is(err, ErrUnknownFlowAction) || !strings.Contains(err.Error(), "voar") {
		t.Fatalf("erro = %v, quero ErrUnknownFlowAction citando %q", err, "voar")
	}
}

// `botao_titulo` (flow_cta) is REQUIRED in flow, same rule as cta_url and
// lista, and uses its OWN constant flowCtaLimit (T-149/T-154): 20 runes
// passes, 21 refuses.
func TestValidateFlowRefusesLongButtonTitle(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"20 runas passa", strings.Repeat("a", 20), nil},
		{"21 runas recusa", strings.Repeat("a", 21), ErrFieldTooLong},
	}
	for _, c := range cases {
		p := baseFlowRequest()
		p.ButtonTitle = c.value
		err := p.Validate()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, quero nil", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "botao_titulo") {
			t.Errorf("%s: erro = %v, quero %v nomeando botao_titulo", c.name, err, c.want)
		}
	}
}

func TestValidateFlowRequiresButtonTitle(t *testing.T) {
	p := baseFlowRequest()
	p.ButtonTitle = ""
	err := p.Validate()
	if !errors.Is(err, ErrFieldRequired) || !strings.Contains(err.Error(), "botao_titulo") {
		t.Fatalf("erro = %v, quero ErrFieldRequired nomeando botao_titulo", err)
	}
}

// Item 4 of T-154: `cabecalho_texto`/`rodape` are REFUSED in flow, and the
// reason is LACK OF CONFIRMATION -- there's no confirmed source that Meta
// accepts header/footer on this type. The guard is the SAME cross-cutting
// guard that already protects pedir_localizacao/midia/etc -- "flow" wasn't
// added to the list of types that accept them.
func TestValidateFlowRefusesHeaderText(t *testing.T) {
	p := baseFlowRequest()
	p.HeaderText = "Ola"
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "cabecalho_texto") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando cabecalho_texto", err)
	}
}

func TestValidateFlowRefusesFooter(t *testing.T) {
	p := baseFlowRequest()
	p.Footer = "Ate mais"
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "rodape") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando rodape", err)
	}
}

// `fluxo` only has shape in the MetaBody of its own type (T-154) -- sent
// on another type it would be discarded silently during assembly without
// this guard.
func TestValidateRefusesFlowOutsideTheFlowType(t *testing.T) {
	p := Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "oi",
		Flow: &FlowRequest{ID: "1", Token: "t", Action: "data_exchange"}}
	err := p.Validate()
	if !errors.Is(err, ErrFieldForbidden) || !strings.Contains(err.Error(), "fluxo") {
		t.Fatalf("erro = %v, quero ErrFieldForbidden nomeando fluxo", err)
	}
}

// Trim spaces: same rule as the rest of this file -- presence isn't
// content.
func TestValidateFlowNormalizesTheFields(t *testing.T) {
	p := baseFlowRequest()
	p.Flow.ID = "  123456789  "
	p.Flow.Token = "  agendamento-4471  "
	p.Flow.Screen = "  TELA_INICIAL  "
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, quero nil", err)
	}
	if p.Flow.ID != "123456789" {
		t.Errorf("fluxo.id = %q, quero aparado", p.Flow.ID)
	}
	if p.Flow.Token != "agendamento-4471" {
		t.Errorf("fluxo.token = %q, quero aparado", p.Flow.Token)
	}
	if p.Flow.Screen != "TELA_INICIAL" {
		t.Errorf("fluxo.tela = %q, quero aparado", p.Flow.Screen)
	}
}
