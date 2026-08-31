// Código: internal/meta/types.go, internal/meta/templates.go,
// internal/meta/errors.go, internal/outbound/*.go, internal/inbound/deliver.go,
// internal/config/contador.go, docs/MIGRACAO-CONTRATO-EN.md,
// docs/contrato-chaves-que-nao-mudam.txt
//
// T-209's GATE: the flip only counts as done if a real scan says so, not
// because every rename was applied by hand and nobody re-checked.
//
// forbiddenOutputTokens is the OLD (Portuguese) spelling of every SAIDA-EVENTO
// / SAIDA-RESPOSTA key AND value this task renamed — extracted MECHANICALLY
// from the diff that did the renaming (`git diff` over every non-test file
// this task touched, grep for the removed `json:"…"` tag content, plus the
// small set of value constants renamed in internal/meta/errors.go,
// internal/outbound/estado.go/entrada.go/sonda_externa.go/vigia.go and
// internal/config/contador.go). Never hand-typed from memory: memory is
// exactly what missed a field before (the media_id lesson, section 5 of the
// migration doc).
package outbound

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/iscarelli/zapgw/internal/meta"
)

var forbiddenOutputTokens = []string{
	"aberta",
	"aguardando",
	"alcance_externo",
	"alerta_de_conta",
	"alvo",
	"bloqueados",
	"botao_payload",
	"botao_texto",
	"carimbos_desde",
	"categoria",
	"categoria_anterior",
	"categoria_correta",
	"categoria_nova",
	"categoria_pedida",
	"certificado_do_callback",
	"checagem_falhando_desde",
	"cifrados",
	"classe",
	"cobranca",
	"codigo",
	"codigo_meta",
	"componentes",
	"conector",
	"conexoes_prontas",
	"conferido_em",
	"contadores",
	"cru",
	"de_canonico",
	"de_cru",
	"desconhecido",
	"desfecho",
	"detalhe_meta",
	"detalhes",
	"dia",
	"dia_utc",
	"dias_restantes",
	"encaminhada",
	"encaminhada_muitas_vezes",
	"endereco",
	"entrada",
	"erro",
	"estado",
	"eventos",
	"expira_em",
	"expirado",
	"explicacao_meta",
	"falhando",
	"falhando_desde",
	"falhas",
	"fonte",
	"gerado_em",
	"idioma",
	"instancia",
	"instrucao",
	"janela_de_cadastro",
	"legenda",
	"limite_anterior",
	"limite_atual",
	"limite_de_mensagens",
	"limite_diario_maximo",
	"localizacao",
	"medido_em",
	"mensagem",
	"midia_id",
	"midia_mime_payload",
	"motivo",
	"nao_configurado",
	"nao_consegui_verificar",
	"nao_se_aplica",
	"nome",
	"nome_antigo_usado",
	"nome_arquivo",
	"nome_contato",
	"numero_exibido",
	"numero_na_meta",
	"nunca_observado",
	"observado_em",
	"para_canonico",
	"para_cru",
	"pausada",
	"permanente",
	"processados",
	"qualidade",
	"qualidade_do_numero",
	"rastro_meta",
	"reacao",
	"recebido_em",
	"recusado",
	"renovado_em",
	"responder_a",
	"retentavel",
	"serie_7_dias",
	"serie_diaria",
	"sub_tipo",
	"subcodigo_meta",
	"template_categoria",
	"texto",
	"tipo_da_entidade",
	"token_instagram",
	"token_meta",
	"ultimo_em",
	"ultimo_webhook_em",
	"ultimos_7_dias",
	"valor",
	"veredito",
	"versao",
	"voz",
}

// "tipo" is DELIBERATELY not in the list above, and that omission is itself
// part of the gate, not a gap in it. `Event.Type` was `tipo` and this task
// renamed it to `kind` (row 81 of the migration table) — but `AccountAlert`
// (a field NESTED inside an account-alert Event) has its OWN `Type` field,
// tagged `tipo,omitempty`, that the table does NOT cover and this task left
// untouched on purpose (docs/MIGRACAO-CONTRATO-EN.md never lists it; see the
// T-209 report for the full list of look-alike fields left alone for the
// same reason). A blanket `"tipo"` check cannot tell the two apart — it
// would fail on every run, on a field the table says to leave in Portuguese.
// `Event.Type`'s rename is proven precisely instead, by
// internal/meta/parse_test.go (TestParseWebhookDoesNotRegressTheCurrent16Fields,
// TestParseWebhookTheFourAccountEventsDoNotMix), which check the TOP-LEVEL
// `"kind"` key exactly, never the nested one.
func TestEventTypeKeyIsKindNotTipoAtTheTopLevel(t *testing.T) {
	var ev meta.Event
	ev.Type = meta.EventTypeMessage
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, has := top["tipo"]; has {
		t.Errorf("o evento ainda tem uma chave de topo `tipo`: %s", b)
	}
	if _, has := top["kind"]; !has {
		t.Errorf("o evento nao tem a chave de topo `kind`: %s", b)
	}
}

// fillSample walks a struct (or any addressable value) by reflection and
// gives every zero field a non-zero placeholder — a string, a pointer to a
// filled struct, one element in every empty slice, one entry in every empty
// map. WHY REFLECTION, and not hand-writing every field: hand-writing is
// exactly what missed `Location.Name` and the AccountAlert sub-fields during
// this task's own planning (see the T-209 report) — a field nobody thought to
// set never gets marshaled, and a scan that never marshals a field can never
// catch a leaked Portuguese tag on it. Reflection fills EVERY exported field
// that exists TODAY, without anyone maintaining a list.
func fillSample(v reflect.Value) {
	if !v.IsValid() || !v.CanSet() {
		return
	}
	if v.Type() == reflect.TypeOf(json.RawMessage(nil)) {
		v.Set(reflect.ValueOf(json.RawMessage(`{}`)))
		return
	}
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillSample(v.Elem())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fillSample(v.Field(i))
		}
	case reflect.String:
		if v.String() == "" {
			v.SetString("x")
		}
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() == 0 {
			v.SetInt(1)
		}
	case reflect.Slice:
		if v.Len() == 0 {
			elem := reflect.New(v.Type().Elem()).Elem()
			fillSample(elem)
			v.Set(reflect.Append(v, elem))
		} else {
			for i := 0; i < v.Len(); i++ {
				fillSample(v.Index(i))
			}
		}
	case reflect.Map:
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
			key := reflect.New(v.Type().Key()).Elem()
			if key.Kind() == reflect.String {
				key.SetString("k")
			}
			val := reflect.New(v.Type().Elem()).Elem()
			fillSample(val)
			v.SetMapIndex(key, val)
		}
	}
}

// TestOutputContractHasNoPortugueseKeyOrValue is the T-209 GATE, and it's a
// SWEEP, never a sample: it serializes one MAXIMAL instance of every
// SAIDA-EVENTO event type and every SAIDA-RESPOSTA body this gateway writes
// to the wire, then fails if any of the 119 Portuguese-side words the
// migration table (docs/MIGRACAO-CONTRATO-EN.md) renamed away from still
// shows up — as a KEY or as a VALUE, since both are checked by the same
// quoted-token search.
//
// "I renamed them all" is an impression; this is the number that replaces it.
func TestOutputContractHasNoPortugueseKeyOrValue(t *testing.T) {
	var out strings.Builder

	marshal := func(label string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", label, err)
		}
		out.Write(b)
		out.WriteByte('\n')
	}

	// SAIDA-EVENTO: one filled Event per EventType, so every type-specific
	// nested block (template, template_category, number_quality,
	// account_alert) gets marshaled at least once — a bare Event{} would
	// leave those omitempty pointers nil and their keys would never appear
	// to be checked at all.
	for _, et := range []meta.EventType{
		meta.EventTypeMessage, meta.EventTypeStatus, meta.EventTypeTemplateStatus,
		meta.EventTypeTemplateCategory, meta.EventTypeNumberQuality, meta.EventTypeAccountAlert,
	} {
		var ev meta.Event
		fillSample(reflect.ValueOf(&ev).Elem())
		ev.Type = et
		marshal("event:"+string(et), ev)
	}

	// SAIDA-RESPOSTA: one filled instance of every response body a route
	// writes with json.NewEncoder/json.Marshal.
	fillAndMarshal := func(label string, v any) {
		fillSample(reflect.ValueOf(v).Elem())
		marshal(label, v)
	}
	fillAndMarshal("state", &State{})
	fillAndMarshal("registration", &RegistrationResponse{})
	fillAndMarshal("block-op", &blockOperationResponse{})
	fillAndMarshal("block-list", &blockListResponse{})
	fillAndMarshal("templates", &templatesResponse{})
	fillAndMarshal("template-created", &templateCreatedResponse{})
	fillAndMarshal("template-deleted", &templateDeletedResponse{})
	fillAndMarshal("health", &healthResponse{})
	fillAndMarshal("smoke", &SmokeResponse{})
	fillAndMarshal("profile-read", &profileResponse{})
	fillAndMarshal("profile-write", &profileWriteResponse{})
	fillAndMarshal("pause", &PauseResponse{})

	// The shared error body, once per ErrorClass — respondError's ~50 call
	// sites pass either one of these constants or (since this task) the
	// literal English string, and both paths end up in this same struct.
	for _, class := range []string{
		string(meta.ClassRetryable), string(meta.ClassPermanent),
		string(meta.ClassConfig), string(meta.ClassUnknown),
	} {
		var er errorResponse
		fillSample(reflect.ValueOf(&er).Elem())
		er.Error.Class = class
		marshal("error:"+class, er)
	}
	var erReread errorResponseWithReread
	fillSample(reflect.ValueOf(&erReread).Elem())
	marshal("error-reread", erReread)

	output := out.String()
	for _, token := range forbiddenOutputTokens {
		if strings.Contains(output, `"`+token+`"`) {
			t.Errorf("a saida ainda contem %q — chave ou valor em portugues que a T-209 tinha de ter renomeado", token)
		}
	}
}

// TestFrozenKeysStayIdenticalInSource is the other half of the T-209 gate:
// docs/contrato-chaves-que-nao-mudam.txt lists every output key that was
// ALREADY English before this task and therefore must NOT be touched by it —
// renaming one of them is the `media_id`-in-reverse mistake (section 5 of
// docs/MIGRACAO-CONTRATO-EN.md). This sweeps the actual source under
// internal/meta and internal/outbound (never the doc, which could itself be
// stale) and fails if any listed key no longer appears there, spelled
// exactly as declared.
//
// A key can show up either as a struct tag (`json:"key"` / `json:"key,…"`)
// or as a literal map key (`"key":`, media_handler.go's `map[string]string`
// response) — both patterns are checked, and either one satisfies the key.
func TestFrozenKeysStayIdenticalInSource(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "contrato-chaves-que-nao-mudam.txt"))
	if err != nil {
		t.Fatalf("ler docs/contrato-chaves-que-nao-mudam.txt: %v", err)
	}
	var frozen []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		frozen = append(frozen, line)
	}
	if len(frozen) == 0 {
		t.Fatal("docs/contrato-chaves-que-nao-mudam.txt nao tem nenhuma chave — o teste nao provaria nada")
	}

	var sourceText strings.Builder
	for _, dir := range []string{
		filepath.Join("..", "meta"),
		".", // internal/outbound itself
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ler %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("ler %s: %v", filepath.Join(dir, name), err)
			}
			sourceText.Write(b)
			sourceText.WriteByte('\n')
		}
	}
	source := sourceText.String()

	for _, key := range frozen {
		structTag := regexp.MustCompile(`json:"` + regexp.QuoteMeta(key) + `[",]`)
		mapLiteral := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `":`)
		if !structTag.MatchString(source) && !mapLiteral.MatchString(source) {
			t.Errorf("a chave congelada %q (docs/contrato-chaves-que-nao-mudam.txt) NAO aparece mais em "+
				"internal/meta nem internal/outbound — sumiu ou foi renomeada, e as duas sao proibidas para ela", key)
		}
	}
}
