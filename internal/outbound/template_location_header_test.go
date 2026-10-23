package outbound

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// T-261: `cabecalho.tipo: "localizacao"` in a template send. The wire shape is
// {"type":"header","parameters":[{"type":"location","location":{...}}]}.

func locationHeaderRequest(h *TemplateHeader) Request {
	r := templateRequest()
	r.Header = h
	return r
}

// (a) the exact component, key for key, so a renamed or extra key fails.
func TestTemplateLocationHeaderBodyIsExact(t *testing.T) {
	r := locationHeaderRequest(&TemplateHeader{Type: "localizacao", Location: &LocationRequest{
		Latitude: floatPtr(-23.5505), Longitude: floatPtr(-46.6333),
		Name: "Loja Centro", Address: "Rua Exemplo, 100",
	}})
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	raw, err := json.Marshal(templateComponents(r))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	const want = `[{"parameters":[{"location":{"address":"Rua Exemplo, 100","latitude":-23.5505,` +
		`"longitude":-46.6333,"name":"Loja Centro"},"type":"location"}],"type":"header"}]`
	if string(raw) != want {
		t.Errorf("components\n got: %s\nwant: %s", raw, want)
	}
}

// name/address are ABSENT, not empty, when not sent.
func TestTemplateLocationHeaderOmitsEmptyNameAndAddress(t *testing.T) {
	r := locationHeaderRequest(&TemplateHeader{Type: "localizacao", Location: &LocationRequest{
		Latitude: floatPtr(1), Longitude: floatPtr(2)}})
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	raw, _ := json.Marshal(templateComponents(r))
	if !strings.Contains(string(raw), `"type":"location"`) {
		t.Fatalf("not a location parameter: %s", raw)
	}
	if strings.Contains(string(raw), `"name"`) || strings.Contains(string(raw), `"address"`) {
		t.Errorf("empty name/address must be absent: %s", raw)
	}
}

// (b) 0 is a valid coordinate and must survive into the body.
func TestTemplateLocationHeaderAcceptsZeroCoordinates(t *testing.T) {
	r := locationHeaderRequest(&TemplateHeader{Type: "localizacao", Location: &LocationRequest{
		Latitude: floatPtr(0), Longitude: floatPtr(0)}})
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	raw, _ := json.Marshal(templateComponents(r))
	if !strings.Contains(string(raw), `"latitude":0`) || !strings.Contains(string(raw), `"longitude":0`) {
		t.Errorf("zero coordinates missing from the body: %s", raw)
	}
}

// (c) missing coordinates / missing object / out of range: 400 naming the field.
func TestTemplateLocationHeaderRefusals(t *testing.T) {
	cases := []struct {
		name string
		h    *TemplateHeader
		want error
		sub  string
	}{
		{"latitude absent", &TemplateHeader{Type: "localizacao", Location: &LocationRequest{
			Longitude: floatPtr(1)}}, ErrFieldRequired, "cabecalho.localizacao.latitude"},
		{"longitude absent", &TemplateHeader{Type: "localizacao", Location: &LocationRequest{
			Latitude: floatPtr(1)}}, ErrFieldRequired, "cabecalho.localizacao.longitude"},
		{"localizacao absent", &TemplateHeader{Type: "localizacao"},
			ErrFieldRequired, "cabecalho.localizacao"},
		{"latitude out of range", &TemplateHeader{Type: "localizacao", Location: &LocationRequest{
			Latitude: floatPtr(90.5), Longitude: floatPtr(1)}}, ErrInvalidCoordinate, "cabecalho.localizacao.latitude"},
		{"longitude out of range", &TemplateHeader{Type: "localizacao", Location: &LocationRequest{
			Latitude: floatPtr(1), Longitude: floatPtr(-180.1)}}, ErrInvalidCoordinate, "cabecalho.localizacao.longitude"},
		// (d) a media_id (or text, or file name) beside the location would vanish.
		{"media_id with localizacao", &TemplateHeader{Type: "localizacao", MediaID: "M1",
			Location: &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(1)}},
			ErrFieldForbidden, "cabecalho.media_id"},
		{"texto with localizacao", &TemplateHeader{Type: "localizacao", Text: "oi",
			Location: &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(1)}},
			ErrFieldForbidden, "cabecalho.texto"},
		{"nome_arquivo with localizacao", &TemplateHeader{Type: "localizacao", Filename: "a.pdf",
			Location: &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(1)}},
			ErrFieldForbidden, "cabecalho.nome_arquivo"},
		// the reverse: localizacao in a header of another type would vanish.
		{"localizacao in texto header", &TemplateHeader{Type: "texto", Text: "oi",
			Location: &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(1)}},
			ErrFieldForbidden, "cabecalho.localizacao"},
		{"localizacao in imagem header", &TemplateHeader{Type: "imagem", MediaID: "I1",
			Location: &LocationRequest{Latitude: floatPtr(1), Longitude: floatPtr(1)}},
			ErrFieldForbidden, "cabecalho.localizacao"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			r := locationHeaderRequest(c.h)
			err := r.Validate()
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !strings.Contains(err.Error(), c.sub) {
				t.Errorf("message %q does not name %q", err.Error(), c.sub)
			}
		})
	}
}

// (e) Meta accepts only image/video in a carousel card, in both carousels.
func TestLocationHeaderIsRefusedInsideCarouselCards(t *testing.T) {
	loc := &TemplateHeader{Type: "localizacao", Location: &LocationRequest{
		Latitude: floatPtr(1), Longitude: floatPtr(1)}}

	card := carouselCard("A")
	card.Header = loc
	tpl := carouselRequest(carouselCard("B"), card)
	err := tpl.Validate()
	if !errors.Is(err, ErrUnknownHeaderType) || !strings.Contains(err.Error(), "cartoes[1].cabecalho") {
		t.Errorf("template carousel: err = %v, want ErrUnknownHeaderType naming cartoes[1].cabecalho", err)
	}
}

// The interactive media header (`cabecalho_midia`) is not a template: location
// there would be sent to Meta in a place it does not exist.
func TestLocationHeaderIsRefusedInInteractiveMediaHeader(t *testing.T) {
	r := headerMediaButtons(&TemplateHeader{Type: "localizacao", Location: &LocationRequest{
		Latitude: floatPtr(1), Longitude: floatPtr(1)}})
	if err := r.Validate(); !errors.Is(err, ErrUnknownHeaderType) {
		t.Errorf("err = %v, want ErrUnknownHeaderType", err)
	}
}

// (f) Template CREATION already passes `componentes` through raw: a
// HEADER/LOCATION component reaches Meta's body intact.
func TestCreateTemplatePassesLocationHeaderComponentRaw(t *testing.T) {
	m := metaWithCatalogOf()
	h := testTemplatesHandler(t, m, "lojinha")

	rec := createTemplate(t, h, "token-do-a", `{"instancia":"lojinha","nome":"endereco_loja",`+
		`"categoria":"UTILITY","idioma":"pt_BR","componentes":[`+
		`{"type":"HEADER","format":"LOCATION"},{"type":"BODY","text":"Estamos aqui"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	_, bodies := m.seen()
	if len(bodies) != 1 {
		t.Fatalf("bodies sent to Meta = %d, want 1", len(bodies))
	}
	if !strings.Contains(bodies[0], `{"type":"HEADER","format":"LOCATION"}`) {
		t.Errorf("the HEADER/LOCATION component did not reach Meta intact: %s", bodies[0])
	}
}
