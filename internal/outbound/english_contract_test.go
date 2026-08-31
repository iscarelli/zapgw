// Código: internal/meta/types.go, internal/meta/templates.go,
// internal/meta/errors.go, internal/outbound/*.go, internal/inbound/deliver.go,
// internal/config/counter.go, docs/MIGRACAO-CONTRATO-EN.md,
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
// internal/outbound/state.go/ingress.go/external_probe.go/watchdog.go and
// internal/config/counter.go). Never hand-typed from memory: memory is
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

// "tipo" is NOT in the list above, on purpose — but unlike before T-210,
// that is no longer because the general sweep can't see it: it's because
// "tipo" gets its OWN path-scoped check (forbiddenKeyExceptions, below),
// which is strictly stronger than a flat word ban.
//
// The reason a plain entry in forbiddenOutputTokens can't do this job:
// `Event.Type` was `tipo` and T-209 renamed it to `kind` (row 81 of the
// migration table) — but `AccountAlert` (a field NESTED inside an
// account-alert Event) has its OWN `Type` field, tagged `tipo,omitempty`,
// that the table does NOT cover and T-209 left untouched on purpose
// (docs/MIGRACAO-CONTRATO-EN.md never lists it). forbiddenOutputTokens is
// checked with a flat substring search over the WHOLE marshaled blob, with
// no notion of WHERE a key sits — so a plain `"tipo"` entry there cannot
// tell "Event's own top-level key regressed" apart from "AccountAlert's
// legitimate nested field", and T-210 measured what that forces: either the
// word is excluded entirely (T-209's choice — the exclusion silently also
// waives the top-level key, which is the bug T-210 exists to fix) or it is
// included and fails on every single run, on a field the table says to
// leave alone.
//
// walkForbiddenKeys (below) fixes this by walking each instance's PARSED
// structure instead of its flat text, so it always knows the exact parent
// path of every key it visits. That lets the exception be as narrow as the
// real one: "tipo" is allowed ONLY at path account_alert.tipo — anywhere
// else, including Event's own top-level key, it fails. Proven by mutation
// in the T-210 report: reverting internal/meta/types.go:413 from
// `json:"kind"` back to `json:"tipo"` now fails
// TestOutputContractHasNoPortugueseKeyOrValue directly, not just the
// narrower regression test below.
//
// TestEventTypeKeyIsKindNotTipoAtTheTopLevel stays as a second, independent
// check of the same fact — cheap, and it fails with a much more specific
// message ("the event still has a top-level tipo key") than the general
// sweep's generic one.
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

// keyException is a single, EXACT, path-scoped waiver for a key that would
// otherwise be forbidden — see walkForbiddenKeys.
type keyException struct {
	path []string // parent object keys, root to the key's immediate parent
	key  string
}

// forbiddenKeyExceptions is deliberately short: every entry here is a key
// T-209 left in Portuguese ON PURPOSE, at an EXACT nesting path, because
// something ELSE at a different path (or a different key entirely) already
// carries the English name the table renamed. Adding an entry here waives
// that one path only — it does not waive the key name everywhere, which is
// the mistake that made the general sweep blind to Event.Type (T-210).
var forbiddenKeyExceptions = []keyException{
	// AccountAlert.Type (internal/meta/types.go, tagged `tipo,omitempty`)
	// is a DIFFERENT field from Event.Type (tagged `kind`, row 81 of the
	// migration table) — docs/MIGRACAO-CONTRATO-EN.md never lists it, and
	// T-209 left it untouched. Scoped to this exact path so a "tipo" key
	// appearing anywhere ELSE — in particular Event's own top-level key —
	// is still forbidden.
	{path: []string{"account_alert"}, key: "tipo"},
}

// walkForbiddenKeys parses one instance's marshaled JSON and visits every
// object key with its full parent path, failing on "tipo" unless the exact
// path+key pair is listed in forbiddenKeyExceptions. This is what makes the
// sweep able to tell "Event's own top-level key regressed to tipo" apart
// from "AccountAlert's legitimate nested tipo field" — a flat substring
// search over the whole blob (forbiddenOutputTokens, below) cannot make
// that distinction, so "tipo" is checked here instead of there.
//
// Only "tipo" is checked this way today because it's the only forbidden
// word with a real, deliberate exception; every other word in
// forbiddenOutputTokens has none, so a flat search is exact for them and
// doesn't need path-awareness.
func walkForbiddenKeys(t *testing.T, label string, v any, path []string) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			if k == "tipo" {
				exempt := false
				for _, ex := range forbiddenKeyExceptions {
					if ex.key == k && len(ex.path) == len(path) {
						same := true
						for i := range ex.path {
							if ex.path[i] != path[i] {
								same = false
								break
							}
						}
						if same {
							exempt = true
							break
						}
					}
				}
				if !exempt {
					t.Errorf("%s: a chave \"tipo\" aparece em %s — regressao do Event.Type (deveria ser \"kind\") ou de outro campo que a T-209 deveria ter renomeado",
						label, strings.Join(append(append([]string{}, path...), k), "."))
				}
			}
			walkForbiddenKeys(t, label, child, append(append([]string{}, path...), k))
		}
	case []any:
		for _, item := range val {
			walkForbiddenKeys(t, label, item, path)
		}
	}
}

// TestOutputContractHasNoPortugueseKeyOrValue is the T-209 GATE, and it's a
// SWEEP, never a sample: it serializes one MAXIMAL instance of every
// SAIDA-EVENTO event type and every SAIDA-RESPOSTA body this gateway writes
// to the wire, then fails if any of the 119 Portuguese-side words the
// migration table (docs/MIGRACAO-CONTRATO-EN.md) renamed away from still
// shows up — as a KEY or as a VALUE, since both are checked by the same
// quoted-token search — PLUS a path-scoped structural check for "tipo"
// (walkForbiddenKeys), the one word a flat search can't safely include
// (see the comment on TestEventTypeKeyIsKindNotTipoAtTheTopLevel and the
// T-210 report for why).
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

		var parsed any
		if err := json.Unmarshal(b, &parsed); err != nil {
			t.Fatalf("%s: Unmarshal (structural check): %v", label, err)
		}
		walkForbiddenKeys(t, label, parsed, nil)
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
