package outbound

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func asJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return m
}

// The phone number is CANONICALIZED on output. Meta does not guarantee the
// same spelling you registered, and sending to 551199990000 when the
// subscriber is 5511999990000 delivers to another number — or to none.
func TestBodyCanonicalizesTheOutgoingPhone(t *testing.T) {
	c := MetaBody(Request{Instance: "l", To: "551199990000", Type: "texto", Text: "oi"})

	if c["to"] != "5511999990000" {
		t.Fatalf("to = %v, quero 5511999990000 (canonizado)", c["to"])
	}
}

func TestTextBody(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "texto", Text: "oi"}))

	if c["messaging_product"] != "whatsapp" {
		t.Errorf("messaging_product = %v", c["messaging_product"])
	}
	if c["type"] != "text" {
		t.Errorf("type = %v, quero text", c["type"])
	}
	text, _ := c["text"].(map[string]any)
	if text["body"] != "oi" {
		t.Errorf("text.body = %v", text["body"])
	}
}

func TestTemplateBodyWithVariables(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "lembrete", Language: "pt_BR", Variables: []string{"Maria", "19h"}}))

	if c["type"] != "template" {
		t.Fatalf("type = %v", c["type"])
	}
	tpl, _ := c["template"].(map[string]any)
	if tpl["name"] != "lembrete" {
		t.Errorf("template.name = %v", tpl["name"])
	}
	language, _ := tpl["language"].(map[string]any)
	if language["code"] != "pt_BR" {
		t.Errorf("template.language.code = %v", language["code"])
	}

	comps, _ := tpl["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components = %v, quero 1 (body)", comps)
	}
	body, _ := comps[0].(map[string]any)
	params, _ := body["parameters"].([]any)
	if len(params) != 2 {
		t.Fatalf("parameters = %v, quero 2", params)
	}
	p0, _ := params[0].(map[string]any)
	if p0["type"] != "text" || p0["text"] != "Maria" {
		t.Errorf("parameters[0] = %v", p0)
	}
}

func TestTemplateBodyWithoutVariablesSendsNoComponents(t *testing.T) {
	// Meta refuses an empty `components: []` in some templates. Absent is
	// different from empty, and the difference only shows up on the real send.
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "aviso", Language: "pt_BR"}))

	tpl, _ := c["template"].(map[string]any)
	if _, has := tpl["components"]; has {
		t.Fatalf("template.components presente sem variaveis: %v", tpl["components"])
	}
}

// ---------------------------------------------------------------------------
// template components: header and URL button (T-021)
// ---------------------------------------------------------------------------

// NO REGRESSION, BYTE FOR BYTE. There is an instance in production sending a
// template with `variaveis`; any change to the body assembled for THIS
// request breaks a client. The comparison is against the exact string (and
// not field by field) on purpose: a NEW field appearing in the body — an
// extra key, `components` in the wrong order — would go unnoticed by any
// selective assertion.
//
// The literal below was CAPTURED from the implementation that predates this
// task, not hand-written from what the body "should" be.
func TestTemplateBodyWithVariablesOnlyIsIdenticalToTodays(t *testing.T) {
	p := Request{
		Instance: "lojinha", To: "5511999990000", Type: "template",
		Template: "lembrete", Language: "pt_BR", Variables: []string{"Maria", "19h"},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	raw, err := json.Marshal(MetaBody(p))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	const ofToday = `{"messaging_product":"whatsapp","recipient_type":"individual",` +
		`"template":{"components":[{"parameters":[{"text":"Maria","type":"text"},` +
		`{"text":"19h","type":"text"}],"type":"body"}],"language":{"code":"pt_BR"},` +
		`"name":"lembrete"},"to":"5511999990000","type":"template"}`

	if string(raw) != ofToday {
		t.Errorf("o corpo mudou para um template so com variaveis\nagora: %s\nhoje:  %s", raw, ofToday)
	}
}

// The DOCUMENT `header` block — the case of consumer-a's PDF receipt. The
// parameter carries the media_id, never a URL: a raw URL makes Meta go fetch
// the file, and that's the path that fails silently when it doesn't fetch it.
func TestTemplateBodyWithDocumentHeader(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "venda_confirmada", Language: "pt_BR",
		Header: &TemplateHeader{
			Type: "documento", MediaID: "MEDIA-9", Filename: "recibo.pdf"},
	}))

	tpl, _ := c["template"].(map[string]any)
	comps, _ := tpl["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components = %v, quero 1 (header)", comps)
	}
	header, _ := comps[0].(map[string]any)
	if header["type"] != "header" {
		t.Fatalf("components[0].type = %v, quero header", header["type"])
	}
	params, _ := header["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("header.parameters = %v, quero 1", params)
	}
	p0, _ := params[0].(map[string]any)
	// The field's name is the SAME as the `type` — {"type":"document","document":{…}} —,
	// and the "documento" -> "document" translation comes from meta.GraphAPIType.
	if p0["type"] != "document" {
		t.Fatalf("parameters[0].type = %v, quero document", p0["type"])
	}
	doc, _ := p0["document"].(map[string]any)
	if doc["id"] != "MEDIA-9" {
		t.Errorf("document.id = %v, quero MEDIA-9", doc["id"])
	}
	if doc["filename"] != "recibo.pdf" {
		t.Errorf("document.filename = %v, quero recibo.pdf", doc["filename"])
	}
	// No link/url: there is no field for that, and none can come to exist.
	if _, has := doc["link"]; has {
		t.Errorf("document.link presente — o header e por media_id: %v", doc)
	}
}

func TestTemplateBodyWithHeaderOfEachType(t *testing.T) {
	cases := []struct {
		header    TemplateHeader
		graphType string
		want      map[string]any // nil for the text header
	}{
		{TemplateHeader{Type: "texto", Text: "Pedido 4210"}, "text", nil},
		{TemplateHeader{Type: "imagem", MediaID: "M1"}, "image", map[string]any{"id": "M1"}},
		{TemplateHeader{Type: "video", MediaID: "M2"}, "video", map[string]any{"id": "M2"}},
		{TemplateHeader{Type: "documento", MediaID: "M3"}, "document", map[string]any{"id": "M3"}},
	}

	for _, tc := range cases {
		hdr := tc.header
		c := asJSON(t, MetaBody(Request{
			Instance: "l", To: "5511999990000", Type: "template",
			Template: "t", Language: "pt_BR", Header: &hdr}))

		tpl, _ := c["template"].(map[string]any)
		comps, _ := tpl["components"].([]any)
		if len(comps) != 1 {
			t.Errorf("%s: components = %v, quero 1", hdr.Type, comps)
			continue
		}
		header, _ := comps[0].(map[string]any)
		params, _ := header["parameters"].([]any)
		p0, _ := params[0].(map[string]any)
		if p0["type"] != tc.graphType {
			t.Errorf("%s: parameters[0].type = %v, quero %q", hdr.Type, p0["type"], tc.graphType)
			continue
		}
		if tc.want == nil {
			if p0["text"] != "Pedido 4210" {
				t.Errorf("%s: parameters[0].text = %v", hdr.Type, p0["text"])
			}
			continue
		}
		media, _ := p0[tc.graphType].(map[string]any)
		if len(media) != len(tc.want) {
			t.Errorf("%s: %v, quero exatamente %v", hdr.Type, media, tc.want)
		}
		for key, value := range tc.want {
			if media[key] != value {
				t.Errorf("%s: %s.%s = %v, quero %v", hdr.Type, tc.graphType, key, media[key], value)
			}
		}
	}
}

// `filename` ABSENT is different from `filename: ""` — the same rule as the
// media's `caption` and `components` itself.
func TestTemplateBodyOmitsFilenameWhenThereIsNone(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "t", Language: "pt_BR",
		Header: &TemplateHeader{Type: "documento", MediaID: "M"},
	}))

	tpl, _ := c["template"].(map[string]any)
	comps, _ := tpl["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components = %v, quero 1 (header)", comps)
	}
	header, _ := comps[0].(map[string]any)
	params, _ := header["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("header.parameters = %v, quero 1", params)
	}
	p0, _ := params[0].(map[string]any)
	doc, _ := p0["document"].(map[string]any)
	if _, has := doc["filename"]; has {
		t.Errorf("filename presente sem nome_arquivo: %v", doc)
	}
}

// THE URL BUTTON IS POSITIONAL, and `index` says WHICH template button the
// parameter belongs to. Swapping the index produces no error at all: the
// token goes to the wrong button and the client lands in the wrong place.
// That's why the test requires the value, and requires it to be a STRING —
// which is how the Graph API expects it.
func TestTemplateBodyWithURLButton(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "equipamento_enviado", Language: "pt_BR",
		TemplateButtons: []TemplateButtonUnion{{Index: 1, Type: "url", Text: "abc123"}},
	}))

	tpl, _ := c["template"].(map[string]any)
	comps, _ := tpl["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components = %v, quero 1 (button)", comps)
	}
	button, _ := comps[0].(map[string]any)
	if button["type"] != "button" {
		t.Errorf("components[0].type = %v, quero button", button["type"])
	}
	if button["sub_type"] != "url" {
		t.Errorf("components[0].sub_type = %v, quero url", button["sub_type"])
	}
	if button["index"] != "1" {
		t.Errorf("components[0].index = %#v, quero a STRING \"1\"", button["index"])
	}
	params, _ := button["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("parameters = %v, quero 1", params)
	}
	p0, _ := params[0].(map[string]any)
	if p0["type"] != "text" || p0["text"] != "abc123" {
		t.Errorf("parameters[0] = %v", p0)
	}
}

// Each button becomes its own block, with ITS OWN index — two parameters in
// the same block would go to the same button.
func TestTemplateBodyWithTwoURLButtonsPreservesEachIndex(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "pacote_entregue", Language: "pt_BR",
		TemplateButtons: []TemplateButtonUnion{
			{Index: 0, Type: "url", Text: "rastreio-1"},
			{Index: 2, Type: "url", Text: "portal-9"},
		},
	}))

	tpl, _ := c["template"].(map[string]any)
	comps, _ := tpl["components"].([]any)
	if len(comps) != 2 {
		t.Fatalf("components = %v, quero 2 blocos de botao", comps)
	}
	want := []struct{ index, text string }{{"0", "rastreio-1"}, {"2", "portal-9"}}
	for i, q := range want {
		block, _ := comps[i].(map[string]any)
		if block["index"] != q.index {
			t.Errorf("components[%d].index = %v, quero %q", i, block["index"], q.index)
		}
		params, _ := block["parameters"].([]any)
		if len(params) != 1 {
			t.Errorf("components[%d].parameters = %v, quero 1", i, params)
			continue
		}
		p0, _ := params[0].(map[string]any)
		if p0["text"] != q.text {
			t.Errorf("components[%d].parameters[0].text = %v, quero %q", i, p0["text"], q.text)
		}
	}
}

// THE BLOCK ORDER IS FIXED — header, body, button. It is not whimsy: the
// assembled body goes into the request's hash (RequestHash), and an order that
// varied would make the SAME request produce different hashes; a legitimate
// retry would get a false 422. Fixing the order here makes that impossible by
// construction.
func TestTemplateBodyBuildsInTheOrderHeaderBodyButton(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "orcamento_disponivel", Language: "pt_BR",
		Variables:       []string{"Maria"},
		Header:          &TemplateHeader{Type: "documento", MediaID: "M", Filename: "orc.pdf"},
		TemplateButtons: []TemplateButtonUnion{{Index: 0, Type: "url", Text: "tok"}},
	}))

	tpl, _ := c["template"].(map[string]any)
	comps, _ := tpl["components"].([]any)
	if len(comps) != 3 {
		t.Fatalf("components = %v, quero 3", comps)
	}
	want := []string{"header", "body", "button"}
	for i, q := range want {
		block, _ := comps[i].(map[string]any)
		if block["type"] != q {
			t.Errorf("components[%d].type = %v, quero %q", i, block["type"], q)
		}
	}
}

// (d) of T-021's Verify, in its strongest form: NO parameter from any of the
// three blocks. `components` has to remain ABSENT, never `[]` — Meta refuses
// the empty list in some templates, and the difference only shows up on the
// real send.
func TestTemplateBodyWithNoParameterAtAllSendsNoComponents(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "aviso", Language: "pt_BR"}))

	tpl, _ := c["template"].(map[string]any)
	value, has := tpl["components"]
	if has {
		t.Fatalf("template.components presente sem parametro nenhum: %#v (ausente != [])", value)
	}
}

// ---------------------------------------------------------------------------
// template buttons: type-discriminated union (T-044)
// ---------------------------------------------------------------------------

// (a) type:"resposta_rapida" produces sub_type:"quick_reply" with parameter
// {"type":"payload","payload":…} — AND THE CHECK IS ON THE PARAMETER'S TYPE,
// not just the payload's presence: "type":"text" and "type":"payload" are two
// equally-present strings, and a bug that swapped one for the other would
// still leave the payload there, only Meta would read it as a URL suffix, not
// as a quick reply id — the SAME defect T-044 exists to close (Meta returns
// the button's TEXT on click, not a recognizable id).
func TestTemplateBodyWithQuickReplyButton(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "confirma_agendamento", Language: "pt_BR",
		TemplateButtons: []TemplateButtonUnion{{Index: 0, Type: "resposta_rapida", Payload: "confirma:41"}},
	}))

	tpl, _ := c["template"].(map[string]any)
	comps, _ := tpl["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components = %v, quero 1 (button)", comps)
	}
	button, _ := comps[0].(map[string]any)
	if button["type"] != "button" {
		t.Errorf("components[0].type = %v, quero button", button["type"])
	}
	if button["sub_type"] != "quick_reply" {
		t.Errorf("components[0].sub_type = %v, quero quick_reply", button["sub_type"])
	}
	if button["index"] != "0" {
		t.Errorf("components[0].index = %#v, quero a STRING \"0\"", button["index"])
	}
	params, _ := button["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("parameters = %v, quero 1", params)
	}
	p0, _ := params[0].(map[string]any)
	// The parameter's TYPE is checked BEFORE the value, on purpose: if the
	// order were inverted, a bug that swapped "type":"payload" for
	// "type":"text" would still pass the value check (the payload itself is
	// right, only the discriminator is wrong) — and that is exactly the bug
	// T-044's mandatory mutation (i) proves.
	if p0["type"] != "payload" {
		t.Fatalf("parameters[0].type = %v, quero payload (NAO text)", p0["type"])
	}
	if p0["payload"] != "confirma:41" {
		t.Errorf("parameters[0].payload = %v, quero confirma:41", p0["payload"])
	}
}

// (b) T-045 NO REGRESSION: type:"url" keeps producing, byte for byte, the
// same block `botoes_url` used to produce — the FIELD is gone, the BUTTON is not.
//
// UNTIL T-045 this comparison was against the body assembled by `botoes_url`
// in the SAME test, on purpose: with both forms alive, comparing one against
// the other survived any format change without updating two places. With the
// old field removed, that pair stopped existing and the only honest way to
// keep the guarantee is a FROZEN LITERAL — captured from T-044's
// implementation, before the removal, and not regenerated from the new code
// (regenerating would prove the code agrees with itself, which is the
// opposite of no-regression).
//
// The literal is the JSON with keys in alphabetical order because
// json.Marshal sorts map keys — it's not this test's choice, and it doesn't
// change between runs.
func TestTemplateURLButtonBodyStaysByteForByteWhatT044Delivered(t *testing.T) {
	const fromT044 = `[{"index":"1","parameters":[{"text":"abc123","type":"text"}],` +
		`"sub_type":"url","type":"button"}]`

	body := MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "equipamento_enviado", Language: "pt_BR",
		TemplateButtons: []TemplateButtonUnion{{Index: 1, Type: "url", Text: "abc123"}},
	})
	tpl, _ := body["template"].(map[string]any)
	block, err := json.Marshal(tpl["components"])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(block) != fromT044 {
		t.Errorf("o bloco de botao de URL mudou com a remocao de botoes_url:\nagora: %s\nT-044: %s",
			block, fromT044)
	}
}

// The union's TWO types in the SAME request, each with ITS OWN index and ITS
// OWN sub_type — the case that used to require both FIELDS and today fits in
// a single list.
func TestTemplateBodyWithBothButtonTypesTogether(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "template",
		Template: "pacote_entregue", Language: "pt_BR",
		TemplateButtons: []TemplateButtonUnion{
			{Index: 0, Type: "url", Text: "rastreio-1"},
			{Index: 1, Type: "resposta_rapida", Payload: "confirma:41"},
		},
	}))

	tpl, _ := c["template"].(map[string]any)
	comps, _ := tpl["components"].([]any)
	if len(comps) != 2 {
		t.Fatalf("components = %v, quero 2 blocos de botao", comps)
	}
	b0, _ := comps[0].(map[string]any)
	if b0["sub_type"] != "url" || b0["index"] != "0" {
		t.Errorf("components[0] = %v, quero sub_type:url index:0", b0)
	}
	b1, _ := comps[1].(map[string]any)
	if b1["sub_type"] != "quick_reply" || b1["index"] != "1" {
		t.Errorf("components[1] = %v, quero sub_type:quick_reply index:1", b1)
	}
}

func TestButtonsBody(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "botoes", Text: "confirma?",
		Buttons: []Button{{ID: "SIM", Title: "Sim"}, {ID: "NAO", Title: "Nao"}}}))

	if c["type"] != "interactive" {
		t.Fatalf("type = %v", c["type"])
	}
	inter, _ := c["interactive"].(map[string]any)
	if inter["type"] != "button" {
		t.Errorf("interactive.type = %v, quero button", inter["type"])
	}
	action, _ := inter["action"].(map[string]any)
	buttons, _ := action["buttons"].([]any)
	if len(buttons) != 2 {
		t.Fatalf("buttons = %v, quero 2", buttons)
	}
	b0, _ := buttons[0].(map[string]any)
	if b0["type"] != "reply" {
		t.Errorf("buttons[0].type = %v, quero reply", b0["type"])
	}
	reply, _ := b0["reply"].(map[string]any)
	if reply["id"] != "SIM" || reply["title"] != "Sim" {
		t.Errorf("buttons[0].reply = %v", reply)
	}
	// NO REGRESSION (T-137): without cabecalho_texto/rodape in the request,
	// the body remains WITHOUT header/footer.
	if _, has := inter["header"]; has {
		t.Error("interactive.header presente sem cabecalho_texto no pedido")
	}
	if _, has := inter["footer"]; has {
		t.Error("interactive.footer presente sem rodape no pedido")
	}
}

func TestCtaURLBody(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "cta_url", Text: "veja as fotos",
		ButtonTitle: "Abrir", ButtonURL: "https://exemplo.com/g"}))

	inter, _ := c["interactive"].(map[string]any)
	if inter["type"] != "cta_url" {
		t.Fatalf("interactive.type = %v, quero cta_url", inter["type"])
	}
	action, _ := inter["action"].(map[string]any)
	params, _ := action["parameters"].(map[string]any)
	if params["display_text"] != "Abrir" || params["url"] != "https://exemplo.com/g" {
		t.Errorf("action.parameters = %v", params)
	}
	// NO REGRESSION (T-137): without cabecalho_texto/rodape in the request,
	// the body remains WITHOUT header/footer — the format that was verified
	// on a real device in another project of this network. A 200 from Meta
	// would not have proven this.
	if _, has := inter["header"]; has {
		t.Error("interactive.header presente — o formato verificado nao tem header")
	}
	if _, has := inter["footer"]; has {
		t.Error("interactive.footer presente — o formato verificado nao tem footer")
	}
}

// T-137: `cabecalho_texto`/`rodape` are FREE-TEXT header/footer of the
// `interactive` object, accepted in "botoes" and "cta_url". Key ABSENT when
// empty, present in the right shape when filled — the same rule as the
// template's `components` and the media's `caption` (comment at the top of
// this file).
func TestButtonsBodyWithHeaderTextAndFooter(t *testing.T) {
	cases := []struct {
		name        string
		headerText  string
		footerField string
		wantHeader  bool
		wantFooter  bool
	}{
		{"so cabecalho", "Confirma o pedido?", "", true, false},
		{"so rodape", "", "Responda em ate 24h", false, true},
		{"os dois", "Confirma o pedido?", "Responda em ate 24h", true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Request{
				Instance: "l", To: "5511999990000", Type: "botoes", Text: "confirma?",
				Buttons:    []Button{{ID: "SIM", Title: "Sim"}},
				HeaderText: c.headerText,
				Footer:     c.footerField,
			}
			body := asJSON(t, MetaBody(p))
			inter, _ := body["interactive"].(map[string]any)

			header, hasHeader := inter["header"].(map[string]any)
			if hasHeader != c.wantHeader {
				t.Fatalf("header presente = %v, quero %v", hasHeader, c.wantHeader)
			}
			if c.wantHeader {
				if header["type"] != "text" || header["text"] != c.headerText {
					t.Errorf("header = %v, quero type:text text:%q", header, c.headerText)
				}
			}

			footer, hasFooter := inter["footer"].(map[string]any)
			if hasFooter != c.wantFooter {
				t.Fatalf("footer presente = %v, quero %v", hasFooter, c.wantFooter)
			}
			if c.wantFooter {
				if footer["text"] != c.footerField {
					t.Errorf("footer = %v, quero text:%q", footer, c.footerField)
				}
			}
		})
	}
}

// Same guarantee as TestButtonsBodyWithHeaderTextAndFooter, on the
// "cta_url" type — the two interactive types that accept free-text
// header/footer (T-137).
func TestCtaURLBodyWithHeaderTextAndFooter(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "cta_url", Text: "veja as fotos",
		ButtonTitle: "Abrir",
		ButtonURL:   "https://exemplo.com/g",
		HeaderText:  "Novidades",
		Footer:      "Oferta valida hoje",
	}
	body := asJSON(t, MetaBody(p))
	inter, _ := body["interactive"].(map[string]any)

	header, _ := inter["header"].(map[string]any)
	if header["type"] != "text" || header["text"] != "Novidades" {
		t.Errorf("header = %v, quero type:text text:Novidades", header)
	}
	footer, _ := inter["footer"].(map[string]any)
	if footer["text"] != "Oferta valida hoje" {
		t.Errorf("footer = %v, quero text:Oferta valida hoje", footer)
	}
}

// T-145: type:"lista". Field names checked against the official source
// (developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-
// list-messages, read on 2026-08-20): sections[].title, sections[].rows[]
// with id/title/description.
func TestListBody(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "lista", Text: "escolha uma opcao",
		ButtonTitle: "Ver opcoes",
		Sections: []ListSection{
			{Title: "Bebidas", Items: []ListItem{
				{ID: "cafe", Title: "Cafe", Description: "Cafe coado"},
				{ID: "cha", Title: "Cha"},
			}},
			{Title: "Comidas", Items: []ListItem{
				{ID: "bolo", Title: "Bolo", Description: "Bolo de cenoura"},
			}},
		},
	}
	body := asJSON(t, MetaBody(p))

	if body["type"] != "interactive" {
		t.Fatalf("type = %v, quero interactive", body["type"])
	}
	inter, _ := body["interactive"].(map[string]any)
	if inter["type"] != "list" {
		t.Fatalf("interactive.type = %v, quero list", inter["type"])
	}
	interBody, _ := inter["body"].(map[string]any)
	if interBody["text"] != "escolha uma opcao" {
		t.Errorf("body.text = %v", interBody["text"])
	}

	action, _ := inter["action"].(map[string]any)
	if action["button"] != "Ver opcoes" {
		t.Errorf("action.button = %v, quero \"Ver opcoes\"", action["button"])
	}
	sections, _ := action["sections"].([]any)
	if len(sections) != 2 {
		t.Fatalf("action.sections tem %d itens, quero 2", len(sections))
	}

	s0, _ := sections[0].(map[string]any)
	if s0["title"] != "Bebidas" {
		t.Errorf("sections[0].title = %v, quero Bebidas", s0["title"])
	}
	rows0, _ := s0["rows"].([]any)
	if len(rows0) != 2 {
		t.Fatalf("sections[0].rows tem %d itens, quero 2", len(rows0))
	}
	l0, _ := rows0[0].(map[string]any)
	if l0["id"] != "cafe" || l0["title"] != "Cafe" || l0["description"] != "Cafe coado" {
		t.Errorf("sections[0].rows[0] = %v", l0)
	}
	// `description` ABSENT when empty -- never "". The "cha" row has no
	// description in the request.
	l1, _ := rows0[1].(map[string]any)
	if l1["id"] != "cha" || l1["title"] != "Cha" {
		t.Errorf("sections[0].rows[1] = %v", l1)
	}
	if _, has := l1["description"]; has {
		t.Error("sections[0].rows[1].description presente -- deveria estar AUSENTE (item sem descricao)")
	}

	s1, _ := sections[1].(map[string]any)
	if s1["title"] != "Comidas" {
		t.Errorf("sections[1].title = %v, quero Comidas", s1["title"])
	}

	// NO REGRESSION: without cabecalho_texto/rodape in the request, the body
	// remains WITHOUT header/footer -- the same key-absent-when-empty rule
	// as the rest of this file.
	if _, has := inter["header"]; has {
		t.Error("interactive.header presente -- pedido nao mandou cabecalho_texto")
	}
	if _, has := inter["footer"]; has {
		t.Error("interactive.footer presente -- pedido nao mandou rodape")
	}
}

// Same guarantee as TestButtonsBodyWithHeaderTextAndFooter /
// TestCtaURLBodyWithHeaderTextAndFooter, on the "lista" type -- the third
// interactive type that accepts free-text header/footer since T-145.
func TestListBodyWithHeaderTextAndFooter(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "lista", Text: "escolha uma opcao",
		ButtonTitle: "Ver opcoes",
		Sections:    []ListSection{{Title: "S", Items: []ListItem{{ID: "1", Title: "Item"}}}},
		HeaderText:  "Novidades",
		Footer:      "Oferta valida hoje",
	}
	body := asJSON(t, MetaBody(p))
	inter, _ := body["interactive"].(map[string]any)

	header, _ := inter["header"].(map[string]any)
	if header["type"] != "text" || header["text"] != "Novidades" {
		t.Errorf("header = %v, quero type:text text:Novidades", header)
	}
	footer, _ := inter["footer"].(map[string]any)
	if footer["text"] != "Oferta valida hoje" {
		t.Errorf("footer = %v, quero text:Oferta valida hoje", footer)
	}
}

func TestBodyCarriesReplyToWhenThereIsOne(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "texto", Text: "oi",
		ReplyTo: "wamid.ABC"}))

	ctx, _ := c["context"].(map[string]any)
	if ctx["message_id"] != "wamid.ABC" {
		t.Fatalf("context.message_id = %v", ctx["message_id"])
	}
}

func TestBodyOmitsContextWhenThereIsNone(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "texto", Text: "oi"}))

	if _, has := c["context"]; has {
		t.Fatal("context presente sem responder_a — a Meta pode recusar context vazio")
	}
}

// THE MEDIA BODY HAS ONE SHAPE PER CATEGORY, and the field name is the SAME
// as the `type`: {"type":"image","image":{...}}. Getting this pair wrong
// produces a 400 from Meta discovered in production — and the
// "imagem" -> "image" translation lives in a single place
// (meta.GraphAPIType), also read by whoever validates.
func TestMediaBodyPerCategory(t *testing.T) {
	cases := []struct {
		category  string
		graphType string
		caption   string
		name      string
		wantKey   map[string]any
	}{
		{"imagem", "image", "olha isso", "", map[string]any{"id": "M1", "caption": "olha isso"}},
		{"video", "video", "o video", "", map[string]any{"id": "M1", "caption": "o video"}},
		{"audio", "audio", "", "", map[string]any{"id": "M1"}},
		{"sticker", "sticker", "", "", map[string]any{"id": "M1"}},
		{"documento", "document", "a nota", "nota.pdf",
			map[string]any{"id": "M1", "caption": "a nota", "filename": "nota.pdf"}},
	}

	for _, c := range cases {
		body := asJSON(t, MetaBody(Request{
			Instance: "l", To: "5511999990000", Type: "midia", Category: c.category,
			MediaID: "M1", Caption: c.caption, Filename: c.name}))

		if body["type"] != c.graphType {
			t.Errorf("%s: type = %v, quero %q", c.category, body["type"], c.graphType)
			continue
		}
		media, _ := body[c.graphType].(map[string]any)
		if len(media) != len(c.wantKey) {
			t.Errorf("%s: corpo = %v, quero exatamente %v", c.category, media, c.wantKey)
		}
		for key, value := range c.wantKey {
			if media[key] != value {
				t.Errorf("%s: %s.%s = %v, quero %v", c.category, c.graphType, key, media[key], value)
			}
		}
	}
}

// `caption` ABSENT is different from `caption: ""`. Sending the empty key
// helps no one and is one more field for Meta to refuse — the same rule as
// the template's `components`.
func TestMediaBodyOmitsEmptyCaptionAndEmptyFilename(t *testing.T) {
	body := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "midia",
		Category: "documento", MediaID: "M1"}))

	doc, _ := body["document"].(map[string]any)
	if _, has := doc["caption"]; has {
		t.Errorf("caption presente sem legenda: %v", doc)
	}
	if _, has := doc["filename"]; has {
		t.Errorf("filename presente sem nome_arquivo: %v", doc)
	}
}

// ---------------------------------------------------------------------------
// `reacao` type (T-024)
// ---------------------------------------------------------------------------

// Shape checked against the official source (developers.facebook.com/docs/
// whatsapp/cloud-api/messages/reaction-messages, read on 2026-07-26):
// {"type":"reaction","reaction":{"message_id":…,"emoji":…}}.
func TestReactionBody(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "reacao",
		Reaction: &ReactionRequest{Target: "wamid.ABC", Emoji: emojiPtr("\U0001F44D")}}))

	if c["type"] != "reaction" {
		t.Fatalf("type = %v, quero reaction", c["type"])
	}
	reaction, _ := c["reaction"].(map[string]any)
	if reaction["message_id"] != "wamid.ABC" {
		t.Errorf("reaction.message_id = %v, quero wamid.ABC", reaction["message_id"])
	}
	if reaction["emoji"] != "\U0001F44D" {
		t.Errorf("reaction.emoji = %v, quero \U0001F44D", reaction["emoji"])
	}
}

// T-027: `emoji: ""` REMOVES the reaction (device experiment, consumer-a,
// 2026-07-26 10:15 -03 — see the comment on ReactionRequest). The "emoji" key
// HAS TO APPEAR in the body even when empty: it's the key's presence with an
// empty string that Meta reads as removal. If `omitempty` (or an equivalent
// conditional) dropped the key here, Meta would respond 200 the same way
// without removing anything — the mutation this test exists to catch.
func TestReactionBodyWithEmptyEmojiSendsTheKeyEmpty(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "reacao",
		Reaction: &ReactionRequest{Target: "wamid.ABC", Emoji: emojiPtr("")}}))

	reaction, _ := c["reaction"].(map[string]any)
	emoji, present := reaction["emoji"]
	if !present {
		t.Fatalf("chave \"emoji\" ausente do corpo — a remocao vira pedido sem efeito, "+
			"e a Meta responde 200 do mesmo jeito sem remover nada: %v", reaction)
	}
	if emoji != "" {
		t.Errorf("reaction.emoji = %v, quero \"\" (remocao)", emoji)
	}
}

// ---------------------------------------------------------------------------
// `localizacao` type (T-024)
// ---------------------------------------------------------------------------

// Shape checked against the official source (developers.facebook.com/docs/
// whatsapp/cloud-api/messages/location-messages, read on 2026-07-26):
// latitude and longitude are a NUMBER on the wire (not a string), and
// name/address are optional.
func TestLocationBody(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "localizacao",
		Location: &LocationRequest{
			Latitude: floatPtr(37.44221496582), Longitude: floatPtr(-122.16165924072),
			Name: "Cafe de Teste", Address: "Rua de Teste, 101",
		}}))

	if c["type"] != "location" {
		t.Fatalf("type = %v, quero location", c["type"])
	}
	loc, _ := c["location"].(map[string]any)
	// NUMBER, not string: json.Unmarshal returns float64 for a JSON number
	// and string for a JSON string — the TYPE assertion (not just the value)
	// is what proves the serialization didn't turn into text.
	lat, isNumber := loc["latitude"].(float64)
	if !isNumber {
		t.Fatalf("location.latitude = %#v (%T), quero float64 (numero JSON, nao string)", loc["latitude"], loc["latitude"])
	}
	if lat != 37.44221496582 {
		t.Errorf("location.latitude = %v, quero 37.44221496582", lat)
	}
	lon, isNumber := loc["longitude"].(float64)
	if !isNumber {
		t.Fatalf("location.longitude = %#v (%T), quero float64 (numero JSON, nao string)", loc["longitude"], loc["longitude"])
	}
	if lon != -122.16165924072 {
		t.Errorf("location.longitude = %v, quero -122.16165924072", lon)
	}
	if loc["name"] != "Cafe de Teste" {
		t.Errorf("location.name = %v, quero Cafe de Teste", loc["name"])
	}
	if loc["address"] != "Rua de Teste, 101" {
		t.Errorf("location.address = %v, quero Rua de Teste, 101", loc["address"])
	}
}

// T-024's CENTRAL PITFALL: 0 is a valid coordinate (the crossing of the
// Greenwich meridian with the equator), and it HAS to come out in the body —
// it cannot be omitted for looking "empty". The assertion checks the key's
// PRESENCE, not just the value: an omitempty on a numeric field would drop
// the whole key, and `loc["latitude"] == nil` would have the same symptom as
// "the key doesn't exist".
func TestLocationBodyDoesNotOmitZeroLatitudeAndLongitude(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "localizacao",
		Location: &LocationRequest{Latitude: floatPtr(0), Longitude: floatPtr(0)}}))

	loc, _ := c["location"].(map[string]any)
	latitude, has := loc["latitude"]
	if !has {
		t.Fatal("location.latitude AUSENTE para latitude=0 — apagaria o equador em silencio")
	}
	if latitude != float64(0) {
		t.Errorf("location.latitude = %v, quero 0", latitude)
	}
	longitude, has := loc["longitude"]
	if !has {
		t.Fatal("location.longitude AUSENTE para longitude=0 — apagaria o meridiano de Greenwich em silencio")
	}
	if longitude != float64(0) {
		t.Errorf("location.longitude = %v, quero 0", longitude)
	}
}

// name/address ARE optional in Meta itself: an absent key is different from
// an empty key, the same rule as `caption`/`filename`/`components`.
func TestLocationBodyOmitsNameAndAddressWhenAbsent(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "localizacao",
		Location: &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(2)}}))

	loc, _ := c["location"].(map[string]any)
	if _, has := loc["name"]; has {
		t.Errorf("location.name presente sem nome: %v", loc)
	}
	if _, has := loc["address"]; has {
		t.Errorf("location.address presente sem endereco: %v", loc)
	}
}

// ---------------------------------------------------------------------------
// `contatos` type (T-146)
// ---------------------------------------------------------------------------

// Shape checked against the official source (developers.facebook.com/docs/
// whatsapp/cloud-api/messages/contacts-messages, read 2026-08-20):
// {"type":"contacts", "contacts":[…]}. NO TRANSLATION inside the card, on
// purpose -- see the comment on the Contact type in mensagem.go -- so this
// assertion checks META's OWN names (`name`, `formatted_name`, `phones`,
// `phone`…), not a translation.
func TestContactsBody(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "contatos",
		Contacts: []Contact{{
			Name:   ContactName{FormattedName: "Joao Vendedor", FirstName: "Joao"},
			Phones: []ContactPhone{{Phone: "+55 11 99999-0000", Type: "CELL", WaID: "5511999990000"}},
			Emails: []ContactEmail{{Email: "joao@example.com", Type: "WORK"}},
			Org:    &ContactOrg{Company: "Acme"},
		}},
	}))

	if c["type"] != "contacts" {
		t.Fatalf("type = %v, quero contacts", c["type"])
	}
	contacts, isList := c["contacts"].([]any)
	if !isList || len(contacts) != 1 {
		t.Fatalf("contacts = %v, quero uma lista com 1 item", c["contacts"])
	}
	c0, _ := contacts[0].(map[string]any)

	name, _ := c0["name"].(map[string]any)
	if name["formatted_name"] != "Joao Vendedor" || name["first_name"] != "Joao" {
		t.Errorf("name = %v", name)
	}
	// last_name ABSENT when empty -- never "" -- the same rule as the rest of
	// this file.
	if _, has := name["last_name"]; has {
		t.Errorf("name.last_name presente sem valor: %v", name)
	}

	phones, _ := c0["phones"].([]any)
	p0, _ := phones[0].(map[string]any)
	if p0["phone"] != "+55 11 99999-0000" || p0["type"] != "CELL" || p0["wa_id"] != "5511999990000" {
		t.Errorf("phones[0] = %v", p0)
	}

	emails, _ := c0["emails"].([]any)
	e0, _ := emails[0].(map[string]any)
	if e0["email"] != "joao@example.com" || e0["type"] != "WORK" {
		t.Errorf("emails[0] = %v", e0)
	}

	org, _ := c0["org"].(map[string]any)
	if org["company"] != "Acme" {
		t.Errorf("org = %v", org)
	}
}

// Card with only the required field: none of the optional keys (addresses,
// birthday, emails, org, phones, urls) can appear -- key ABSENT when empty,
// the same rule as the rest of this file.
func TestContactsBodyOmitsAbsentOptionalFields(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "contatos",
		Contacts: []Contact{{Name: ContactName{FormattedName: "Joao"}}},
	}))
	contacts, _ := c["contacts"].([]any)
	c0, _ := contacts[0].(map[string]any)

	for _, key := range []string{"addresses", "birthday", "emails", "org", "phones", "urls"} {
		if _, has := c0[key]; has {
			t.Errorf("contacts[0].%s presente sem valor: %v", key, c0)
		}
	}
}

func TestContactsBodyWithMoreThanOneCard(t *testing.T) {
	c := asJSON(t, MetaBody(Request{
		Instance: "l", To: "5511999990000", Type: "contatos",
		Contacts: []Contact{
			{Name: ContactName{FormattedName: "Primeiro"}},
			{Name: ContactName{FormattedName: "Segundo"}},
		},
	}))
	contacts, _ := c["contacts"].([]any)
	if len(contacts) != 2 {
		t.Fatalf("contacts tem %d itens, quero 2", len(contacts))
	}
}

// T-150: type:"pedir_localizacao". The WHOLE shape from the official source
// (developers.facebook.com/docs/whatsapp/cloud-api/guides/send-messages/
// location-request-messages/, read 2026-08-20) -- THREE fields and no
// others: {"type":"location_request_message","body":{"text":...},
// "action":{"name":"send_location"}}. Compared by WHOLE STRUCTURAL EQUALITY
// (reflect.DeepEqual), not field by field: the shape is small enough to fit
// in a literal, and it's the most direct way to freeze "this is all there is,
// nothing more" -- no header, no footer, no extra field in action.
func TestLocationRequestBody(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "pedir_localizacao",
		Text: "Pode compartilhar sua localizacao?",
	}
	body := asJSON(t, MetaBody(p))

	if body["type"] != "interactive" {
		t.Fatalf("type = %v, quero interactive", body["type"])
	}
	inter, _ := body["interactive"].(map[string]any)

	want := map[string]any{
		"type":   "location_request_message",
		"body":   map[string]any{"text": "Pode compartilhar sua localizacao?"},
		"action": map[string]any{"name": "send_location"},
	}
	if !reflect.DeepEqual(inter, want) {
		t.Fatalf("interactive = %#v, quero %#v (a forma tem de ser EXATAMENTE os tres campos da doc)",
			inter, want)
	}
}

// THERE IS NO CEILING on the text at assembly time either -- the same
// decision from T-143, frozen on the VALIDATION side in
// TestValidateAcceptsLocationRequestWithLongText (mensagem_test.go). Here it
// checks that the long text crosses the assembly intact, without truncating.
func TestLocationRequestBodyWithLongTextDoesNotTruncate(t *testing.T) {
	long := strings.Repeat("a", 2000)
	p := Request{Instance: "l", To: "5511999990000", Type: "pedir_localizacao", Text: long}
	body := asJSON(t, MetaBody(p))
	inter, _ := body["interactive"].(map[string]any)
	interBody, _ := inter["body"].(map[string]any)
	if interBody["text"] != long {
		t.Errorf("body.text truncado: %d caracteres, quero %d", len(interBody["text"].(string)), len(long))
	}
}

// T-154: type:"flow", `fluxo.id` + `fluxo.acao:"navigate"` (with `tela`).
// The WHOLE shape is THIRD-HAND -- see the comment on the "flow" case in
// corpo.go and the comment on FlowRequest in mensagem.go. Compared by WHOLE
// STRUCTURAL EQUALITY (reflect.DeepEqual), the same pattern as
// TestLocationRequestBody -- freezes "this is all there is, nothing more".
func TestFlowBodyWithIDAndNavigate(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "flow", Text: "Preencha seus dados",
		ButtonTitle: "Agendar",
		Flow: &FlowRequest{
			ID:     "123456789",
			Token:  "agendamento-4471",
			Action: "navigate",
			Screen: "TELA_INICIAL",
		},
	}
	body := asJSON(t, MetaBody(p))

	if body["type"] != "interactive" {
		t.Fatalf("type = %v, quero interactive", body["type"])
	}
	inter, _ := body["interactive"].(map[string]any)

	want := map[string]any{
		"type": "flow",
		"body": map[string]any{"text": "Preencha seus dados"},
		"action": map[string]any{
			"name": "flow",
			"parameters": map[string]any{
				"flow_message_version": "3",
				"flow_token":           "agendamento-4471",
				"flow_cta":             "Agendar",
				"flow_action":          "navigate",
				"flow_id":              "123456789",
				"flow_action_payload":  map[string]any{"screen": "TELA_INICIAL"},
			},
		},
	}
	if !reflect.DeepEqual(inter, want) {
		t.Fatalf("interactive = %#v, quero %#v", inter, want)
	}
}

// `fluxo.nome` (without `id`) becomes `flow_name`, never `flow_id` -- the two
// are mutually exclusive by construction (Validate() already guaranteed that
// before MetaBody runs).
func TestFlowBodyWithNameBecomesFlowName(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "flow", Text: "Preencha seus dados",
		ButtonTitle: "Agendar",
		Flow: &FlowRequest{
			Name:   "fluxo-de-agendamento",
			Token:  "agendamento-4471",
			Action: "navigate",
			Screen: "TELA_INICIAL",
		},
	}
	body := asJSON(t, MetaBody(p))
	inter, _ := body["interactive"].(map[string]any)
	action, _ := inter["action"].(map[string]any)
	params, _ := action["parameters"].(map[string]any)

	if params["flow_name"] != "fluxo-de-agendamento" {
		t.Errorf("flow_name = %v, quero fluxo-de-agendamento", params["flow_name"])
	}
	if _, has := params["flow_id"]; has {
		t.Error("flow_id presente -- o pedido mandou fluxo.nome, nao fluxo.id")
	}
}

// `acao:"data_exchange"` without `tela` or `dados`: the whole
// `flow_action_payload` stays ABSENT -- key-absent-when-empty, the same rule
// as the rest of this file (header/footer of botoes/cta_url/lista,
// description of ListItem).
func TestFlowBodyDataExchangeWithoutPayloadOmitsTheKey(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "flow", Text: "Preencha seus dados",
		ButtonTitle: "Agendar",
		Flow: &FlowRequest{
			ID:     "123456789",
			Token:  "agendamento-4471",
			Action: "data_exchange",
		},
	}
	body := asJSON(t, MetaBody(p))
	inter, _ := body["interactive"].(map[string]any)
	action, _ := inter["action"].(map[string]any)
	params, _ := action["parameters"].(map[string]any)

	if _, has := params["flow_action_payload"]; has {
		t.Errorf("flow_action_payload presente (%v) -- fluxo sem tela nem dados deveria omitir a chave",
			params["flow_action_payload"])
	}
}

// `fluxo.dados` becomes `flow_action_payload.data`, together with `screen`
// when both come filled in.
func TestFlowBodyWithDataAndScreen(t *testing.T) {
	p := Request{
		Instance: "l", To: "5511999990000", Type: "flow", Text: "Preencha seus dados",
		ButtonTitle: "Agendar",
		Flow: &FlowRequest{
			ID:     "123456789",
			Token:  "agendamento-4471",
			Action: "data_exchange",
			Screen: "TELA_DOIS",
			Data:   map[string]any{"nome_cliente": "Maria"},
		},
	}
	body := asJSON(t, MetaBody(p))
	inter, _ := body["interactive"].(map[string]any)
	action, _ := inter["action"].(map[string]any)
	params, _ := action["parameters"].(map[string]any)
	payload, _ := params["flow_action_payload"].(map[string]any)

	if payload["screen"] != "TELA_DOIS" {
		t.Errorf("flow_action_payload.screen = %v, quero TELA_DOIS", payload["screen"])
	}
	data, _ := payload["data"].(map[string]any)
	if data["nome_cliente"] != "Maria" {
		t.Errorf("flow_action_payload.data.nome_cliente = %v, quero Maria", data["nome_cliente"])
	}
}
