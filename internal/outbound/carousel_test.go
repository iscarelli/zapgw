package outbound

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// T-259: `tipo: "carrossel"` -- the free-form interactive media carousel.

func carouselQR(n int) Request {
	cards := make([]TemplateCard, n)
	for i := range cards {
		cards[i] = TemplateCard{
			Header:  &TemplateHeader{Type: "imagem", MediaID: "IMG" + string(rune('A'+i))},
			Text:    "cartao",
			Buttons: []Button{{ID: "ID" + string(rune('A'+i)), Title: "Quero"}},
		}
	}
	return Request{Instance: "l", To: "5511999990000", Type: "carrossel",
		Text: "veja as fotos", Cards: cards}
}

func carouselURL(n int) Request {
	p := carouselQR(n)
	for i := range p.Cards {
		p.Cards[i].Buttons = nil
		p.Cards[i].ButtonTitle = "Abrir"
		p.Cards[i].ButtonURL = "https://exemplo.com/g"
	}
	return p
}

// Frozen from Meta's interactive media carousel page (read 2026-10-22):
// interactive.type "carousel", body.text, action.cards[] with card_index,
// type "cta_url" (also on quick-reply cards, as the page has it), header,
// optional body, and action = {buttons:[{type:"quick_reply",quick_reply:
// {id,title}}]} or {name:"cta_url",parameters:{display_text,url}}.
// The header carries `id` (not the page's `link`): POST /v1/media is the only
// path, same as every other media header here.
func TestInteractiveCarouselBodyMatchesMetaShape(t *testing.T) {
	qr := carouselQR(2)
	if err := qr.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got, _ := json.Marshal(MetaBody(qr))
	const wantQR = `{"interactive":{"action":{"cards":[` +
		`{"action":{"buttons":[{"quick_reply":{"id":"IDA","title":"Quero"},"type":"quick_reply"}]},` +
		`"body":{"text":"cartao"},"card_index":0,"header":{"image":{"id":"IMGA"},"type":"image"},"type":"cta_url"},` +
		`{"action":{"buttons":[{"quick_reply":{"id":"IDB","title":"Quero"},"type":"quick_reply"}]},` +
		`"body":{"text":"cartao"},"card_index":1,"header":{"image":{"id":"IMGB"},"type":"image"},"type":"cta_url"}]},` +
		`"body":{"text":"veja as fotos"},"type":"carousel"},` +
		`"messaging_product":"whatsapp","recipient_type":"individual","to":"5511999990000","type":"interactive"}`
	if string(got) != wantQR {
		t.Errorf("quick-reply carousel mismatch:\ngot:  %s\nwant: %s", got, wantQR)
	}

	u := carouselURL(2)
	if err := u.Validate(); err != nil {
		t.Fatalf("Validate url: %v", err)
	}
	inter := MetaBody(u)["interactive"].(map[string]any)
	cards, _ := json.Marshal(inter["action"].(map[string]any)["cards"].([]map[string]any)[0])
	const wantURL = `{"action":{"name":"cta_url","parameters":{"display_text":"Abrir","url":"https://exemplo.com/g"}},` +
		`"body":{"text":"cartao"},"card_index":0,"header":{"image":{"id":"IMGA"},"type":"image"},"type":"cta_url"}`
	if string(cards) != wantURL {
		t.Errorf("cta_url card mismatch:\ngot:  %s\nwant: %s", cards, wantURL)
	}
}

// T-260: Meta answers `sent` and never delivers a card without body text, so
// the gateway refuses it, naming the card. Blank counts as empty.
func TestCarouselCardRequiresText(t *testing.T) {
	for _, text := range []string{"", "   ", "\t\n"} {
		for name, mk := range map[string]func(int) Request{"quick-reply": carouselQR, "url": carouselURL} {
			p := mk(3)
			p.Cards[1].Text = text
			err := p.Validate()
			if !errors.Is(err, ErrFieldRequired) {
				t.Errorf("%s card with text %q: %v, want ErrFieldRequired", name, text, err)
				continue
			}
			if !strings.Contains(err.Error(), "cartoes[1].texto") {
				t.Errorf("%s card with text %q: %v, want it to name cartoes[1].texto", name, text, err)
			}
		}
	}
}

func TestCarouselValidAndLimits(t *testing.T) {
	for _, n := range []int{2, 10} {
		p := carouselQR(n)
		if err := p.Validate(); err != nil {
			t.Errorf("%d cards: %v", n, err)
		}
	}
	p := carouselQR(1)
	if err := p.Validate(); !errors.Is(err, ErrFieldRequired) {
		t.Errorf("1 card: %v, want ErrFieldRequired", err)
	}
	p = carouselQR(11)
	if err := p.Validate(); !errors.Is(err, ErrFieldTooLong) {
		t.Errorf("11 cards: %v, want ErrFieldTooLong", err)
	}
	p = carouselQR(2)
	p.Cards = nil
	if err := p.Validate(); !errors.Is(err, ErrFieldRequired) {
		t.Errorf("no cards: %v, want ErrFieldRequired", err)
	}
	p = carouselQR(2)
	p.Text = ""
	if err := p.Validate(); !errors.Is(err, ErrFieldRequired) {
		t.Errorf("no texto: %v, want ErrFieldRequired", err)
	}
}

func TestCarouselCardRules(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Request)
		want error
	}{
		{"no header", func(p *Request) { p.Cards[1].Header = nil }, ErrFieldRequired},
		{"text header", func(p *Request) { p.Cards[1].Header = &TemplateHeader{Type: "texto", Text: "x"} }, ErrUnknownHeaderType},
		{"document header", func(p *Request) { p.Cards[1].Header = &TemplateHeader{Type: "documento", MediaID: "D"} }, ErrUnknownHeaderType},
		{"mixed header types", func(p *Request) { p.Cards[1].Header = &TemplateHeader{Type: "video", MediaID: "V"} }, ErrCardsMismatch},
		{"url as header", func(p *Request) { p.Cards[0].Header.MediaID = "https://x.com/a.jpg" }, ErrInvalidMediaID},
		{"no button", func(p *Request) { p.Cards[1].Buttons = nil }, ErrFieldRequired},
		{"both kinds", func(p *Request) { p.Cards[0].ButtonTitle, p.Cards[0].ButtonURL = "A", "https://x.com" }, ErrMixedButtons},
		{"url without title", func(p *Request) {
			p.Cards[0].Buttons, p.Cards[0].ButtonURL = nil, "https://x.com"
		}, ErrFieldRequired},
		{"different kind across cards", func(p *Request) {
			p.Cards[1].Buttons, p.Cards[1].ButtonTitle, p.Cards[1].ButtonURL = nil, "A", "https://x.com"
		}, ErrCardsMismatch},
		{"different count across cards", func(p *Request) {
			p.Cards[1].Buttons = append(p.Cards[1].Buttons, Button{ID: "X", Title: "Y"})
		}, ErrCardsMismatch},
		{"too many buttons", func(p *Request) {
			for _, c := range []*TemplateCard{&p.Cards[0], &p.Cards[1]} {
				c.Buttons = []Button{{ID: "1", Title: "a"}, {ID: "2", Title: "b"}, {ID: "3", Title: "c"}, {ID: "4", Title: "d"}}
			}
		}, ErrFieldTooLong},
		{"empty button id", func(p *Request) { p.Cards[0].Buttons[0].ID = " " }, ErrFieldRequired},
		{"long button title", func(p *Request) {
			p.Cards[0].Buttons[0].Title = "012345678901234567890"
		}, ErrFieldTooLong},
		{"variaveis in carousel", func(p *Request) { p.Cards[0].Variables = []string{"a"} }, ErrFieldForbidden},
		{"botoes_template in carousel", func(p *Request) {
			p.Cards[0].TemplateButtons = []TemplateButtonUnion{{Index: 0, Type: "url", Text: "x"}}
		}, ErrFieldForbidden},
		{"top-level cabecalho_texto", func(p *Request) { p.HeaderText = "x" }, ErrFieldForbidden},
		{"top-level cabecalho_midia", func(p *Request) { p.HeaderMedia = &TemplateHeader{Type: "imagem", MediaID: "M"} }, ErrFieldForbidden},
		{"top-level rodape", func(p *Request) { p.Footer = "x" }, ErrFieldForbidden},
		{"top-level botoes", func(p *Request) { p.Buttons = []Button{{ID: "a", Title: "b"}} }, ErrFieldForbidden},
		{"top-level botao_url", func(p *Request) { p.ButtonURL = "https://x.com" }, ErrFieldForbidden},
		{"top-level botao_titulo", func(p *Request) { p.ButtonTitle = "x" }, ErrFieldForbidden},
		{"top-level cabecalho", func(p *Request) { p.Header = &TemplateHeader{Type: "imagem", MediaID: "M"} }, ErrFieldForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := carouselQR(2)
			c.mut(&p)
			if err := p.Validate(); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

// The card fields of a free-form carousel are 400 in a template card, and
// `cartoes` stays 400 in every other type.
func TestCarouselCardFieldsRefusedInTemplateAndCartoesElsewhere(t *testing.T) {
	for _, mut := range []func(*TemplateCard){
		func(c *TemplateCard) { c.Text = "x" },
		func(c *TemplateCard) { c.Buttons = []Button{{ID: "a", Title: "b"}} },
		func(c *TemplateCard) { c.ButtonTitle = "x" },
		func(c *TemplateCard) { c.ButtonURL = "https://x.com" },
	} {
		cards := []TemplateCard{
			{Header: &TemplateHeader{Type: "imagem", MediaID: "A"}},
			{Header: &TemplateHeader{Type: "imagem", MediaID: "B"}},
		}
		mut(&cards[0])
		p := Request{Instance: "l", To: "5511999990000", Type: "template",
			Template: "t", Language: "pt_BR", Cards: cards}
		if err := p.Validate(); !errors.Is(err, ErrFieldForbidden) {
			t.Errorf("template card new field: %v, want ErrFieldForbidden", err)
		}
	}
	p := Request{Instance: "l", To: "5511999990000", Type: "texto", Text: "x",
		Cards: carouselQR(2).Cards}
	if err := p.Validate(); !errors.Is(err, ErrFieldForbidden) {
		t.Errorf("cartoes in texto: %v, want ErrFieldForbidden", err)
	}
}

// English aliases: `kind: "carousel"`, `cards[].text/buttons/button_title/
// button_url`, with the `buttons[].title` item alias.
func TestCarouselEnglishAliases(t *testing.T) {
	in := `{"instance":"l","to":"5511999990000","kind":"carousel","text":"hi","cards":[` +
		`{"header":{"kind":"image","media_id":"A"},"text":"c","buttons":[{"id":"1","title":"Go"}]},` +
		`{"header":{"kind":"image","media_id":"B"},"text":"c","buttons":[{"id":"2","title":"Go"}]}]}`
	out, old, err := translateRequestBody([]byte(in))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(old) != 0 {
		t.Logf("old names: %v", old)
	}
	var p Request
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.Type != "carrossel" || len(p.Cards) != 2 || p.Cards[0].Text != "c" ||
		p.Cards[0].Buttons[0].Title != "Go" {
		t.Fatalf("translation incomplete: %+v", p)
	}
}
