package outbound

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// T-258: `cabecalho_midia` -- the media header of interactive "botoes" and
// "cta_url" messages.

func headerMediaButtons(h *TemplateHeader) Request {
	return Request{
		Instance: "l", To: "5511999990000", Type: "botoes", Text: "confirma?",
		Buttons:     []Button{{ID: "SIM", Title: "Sim"}},
		HeaderMedia: h,
	}
}

func headerMediaCta(h *TemplateHeader) Request {
	return Request{
		Instance: "l", To: "5511999990000", Type: "cta_url", Text: "veja",
		ButtonTitle: "Abrir", ButtonURL: "https://exemplo.com/g",
		HeaderMedia: h,
	}
}

// The header block must match, key by key, the media header example in
// Meta's interactive reply buttons page (read 2026-10-22):
// "header": {"type": "image", "image": {"id": "<MEDIA_ID>"}}.
func TestInteractiveMediaHeaderMatchesMetaShape(t *testing.T) {
	for _, c := range []struct {
		name string
		req  Request
	}{
		{"botoes", headerMediaButtons(&TemplateHeader{Type: "imagem", MediaID: "1234567890"})},
		{"cta_url", headerMediaCta(&TemplateHeader{Type: "imagem", MediaID: "1234567890"})},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.req.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			inter, _ := MetaBody(c.req)["interactive"].(map[string]any)
			got, _ := json.Marshal(inter["header"])
			const want = `{"image":{"id":"1234567890"},"type":"image"}`
			if string(got) != want {
				t.Errorf("header = %s, want %s", got, want)
			}
		})
	}
}

func TestInteractiveMediaHeaderVideoAndDocument(t *testing.T) {
	for _, mk := range []func(*TemplateHeader) Request{headerMediaButtons, headerMediaCta} {
		v := mk(&TemplateHeader{Type: "video", MediaID: "V1"})
		if err := v.Validate(); err != nil {
			t.Fatalf("video: %v", err)
		}
		got, _ := json.Marshal(MetaBody(v)["interactive"].(map[string]any)["header"])
		if string(got) != `{"type":"video","video":{"id":"V1"}}` {
			t.Errorf("video header = %s", got)
		}

		d := mk(&TemplateHeader{Type: "documento", MediaID: "D1", Filename: "menu.pdf"})
		if err := d.Validate(); err != nil {
			t.Fatalf("document: %v", err)
		}
		got, _ = json.Marshal(MetaBody(d)["interactive"].(map[string]any)["header"])
		if string(got) != `{"document":{"filename":"menu.pdf","id":"D1"},"type":"document"}` {
			t.Errorf("document header = %s", got)
		}

		nf := mk(&TemplateHeader{Type: "documento", MediaID: "D1"})
		if err := nf.Validate(); err != nil {
			t.Fatalf("document without filename: %v", err)
		}
		got, _ = json.Marshal(MetaBody(nf)["interactive"].(map[string]any)["header"])
		if strings.Contains(string(got), "filename") {
			t.Errorf("filename key present while empty: %s", got)
		}
	}
}

// Without the field the body has no header key at all, and the idempotency
// hash input does not mention it.
func TestInteractiveWithoutMediaHeaderIsUnchanged(t *testing.T) {
	for _, p := range []Request{headerMediaButtons(nil), headerMediaCta(nil)} {
		if err := p.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		inter, _ := MetaBody(p)["interactive"].(map[string]any)
		if _, has := inter["header"]; has {
			t.Errorf("header present without the field: %v", inter["header"])
		}
		raw, _ := json.Marshal(p)
		if strings.Contains(string(raw), "cabecalho_midia") {
			t.Errorf("cabecalho_midia leaked into the canonical JSON: %s", raw)
		}
	}
}

func TestInteractiveMediaHeaderRefusals(t *testing.T) {
	good := &TemplateHeader{Type: "imagem", MediaID: "I1"}
	cases := []struct {
		name    string
		req     Request
		want    error
		mention string
	}{
		{"texto", headerMediaButtons(&TemplateHeader{Type: "texto", Text: "oi"}),
			ErrFieldForbidden, "cabecalho_texto"},
		{"cta texto", headerMediaCta(&TemplateHeader{Type: "texto", Text: "oi"}),
			ErrFieldForbidden, "cabecalho_texto"},
		{"tipo desconhecido", headerMediaButtons(&TemplateHeader{Type: "audio", MediaID: "A1"}),
			ErrUnknownHeaderType, ""},
		{"sem media_id", headerMediaButtons(&TemplateHeader{Type: "imagem"}),
			ErrFieldRequired, "cabecalho_midia.media_id"},
		{"URL no media_id", headerMediaCta(&TemplateHeader{Type: "imagem", MediaID: "https://exemplo.com/a.png"}),
			ErrInvalidMediaID, "cabecalho_midia.media_id"},
		{"nome_arquivo em imagem", headerMediaButtons(&TemplateHeader{Type: "imagem", MediaID: "I1", Filename: "a.png"}),
			ErrFieldForbidden, "nome_arquivo"},
		{"junto com cabecalho_texto", func() Request {
			r := headerMediaButtons(good)
			r.HeaderText = "titulo"
			return r
		}(), ErrFieldForbidden, "cabecalho_texto"},
		{"lista", Request{
			Instance: "l", To: "5511999990000", Type: "lista", Text: "t", ButtonTitle: "Ver",
			Sections:    []ListSection{{Title: "s", Items: []ListItem{{ID: "1", Title: "um"}}}},
			HeaderMedia: good,
		}, ErrFieldForbidden, "cabecalho_midia"},
		{"midia", Request{Instance: "l", To: "5511999990000", Type: "midia",
			MediaID: "M1", Category: "imagem", HeaderMedia: good},
			ErrFieldForbidden, "cabecalho_midia"},
		{"template", Request{Instance: "l", To: "5511999990000", Type: "template",
			Template: "t", Language: "pt_BR", HeaderMedia: good},
			ErrFieldForbidden, "cabecalho_midia"},
		{"texto livre", Request{Instance: "l", To: "5511999990000", Type: "texto",
			Text: "oi", HeaderMedia: good},
			ErrFieldForbidden, "cabecalho_midia"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if c.mention != "" && !strings.Contains(err.Error(), c.mention) {
				t.Errorf("error does not mention %q: %v", c.mention, err)
			}
		})
	}
}

// English spelling: `header_media` with `kind`/`file_name` translates to the
// Portuguese request, same body and same hash.
func TestInputAcceptsEnglishHeaderMediaKeys(t *testing.T) {
	const pt = `{"instancia":"lojinha","para":"5511999990000","tipo":"botoes","texto":"x",` +
		`"botoes":[{"id":"a","titulo":"A"}],"cabecalho_midia":{"tipo":"documento","media_id":"D1","nome_arquivo":"m.pdf"}}`
	const en = `{"instance":"lojinha","to":"5511999990000","kind":"buttons","text":"x",` +
		`"buttons":[{"id":"a","title":"A"}],"header_media":{"kind":"documento","media_id":"D1","file_name":"m.pdf"}}`

	decode := func(raw string) (Request, []string) {
		out, old, err := translateRequestBody([]byte(raw))
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		var r Request
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if err := r.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		return r, old
	}
	rpt, oldPT := decode(pt)
	ren, oldEN := decode(en)

	bpt, _ := json.Marshal(MetaBody(rpt))
	ben, _ := json.Marshal(MetaBody(ren))
	if string(bpt) != string(ben) {
		t.Errorf("different bodies:\nPT: %s\nEN: %s", bpt, ben)
	}
	if !strings.Contains(string(ben), `"document":{"filename":"m.pdf","id":"D1"}`) {
		t.Errorf("document header missing: %s", ben)
	}
	if RequestHash(rpt) != RequestHash(ren) {
		t.Error("PT and EN spellings hash differently")
	}
	if len(oldPT) == 0 || len(oldEN) != 0 {
		t.Errorf("old-name counter: PT=%v EN=%v", oldPT, oldEN)
	}
}
