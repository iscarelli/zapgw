package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/meta"
)

// withFolderResultForTest swaps meta.MeasuredFolderResult for
// the duration of the test and restores the production value in Cleanup
// — the SAME save/restore pattern as comResultadoDoFolder in
// internal/meta/diagnostico_instagram_test.go, repeated here because this
// package (main) cannot import a test helper from another package. Used
// by tests that need the sweep of the four folders to run (T-112), a
// behavior that stopped being PRODUCTION's after T-114 measured and
// swapped MeasuredFolderResult to FolderIgnored.
func withFolderResultForTest(t *testing.T, value meta.FolderFilterResult) {
	t.Helper()
	original := meta.MeasuredFolderResult
	meta.MeasuredFolderResult = value
	t.Cleanup(func() { meta.MeasuredFolderResult = original })
}

// fakeInstagramGraph is a fake Instagram Graph API with the three
// routes `zapgw diagnostico` uses (GET /me, GET /me/conversations, GET
// /me/subscribed_apps). Path-based, unlike fakeGraph (fumaca_test.go),
// which only distinguishes GET from POST — the diagnostic hits THREE
// different GET paths in the same round, and each needs its own
// response.
type fakeInstagramGraph struct {
	meStatus int
	meBody   string

	// conversationsBody and conversationsError are indexed by FOLDER ("" =
	// default inbox, no `folder` in the query). A folder in conversationsError
	// responds with that status; otherwise it responds 200 with
	// conversationsBody[folder].
	conversationsBody  map[string]string
	conversationsError map[string]int

	subStatus int
	subBody   string

	mu    sync.Mutex
	paths []string
}

func (g *fakeInstagramGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The token goes in the header, never in the URL — if it stops
		// going there, the diagnostic stops proving what it exists to
		// prove.
		if r.Header.Get("Authorization") == "" {
			t.Errorf("chamada sem Authorization em %s %s", r.Method, r.URL.Path)
		}
		g.mu.Lock()
		g.paths = append(g.paths, r.URL.Path+"?"+r.URL.RawQuery)
		g.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me":
			w.WriteHeader(g.meStatus)
			_, _ = w.Write([]byte(g.meBody))
		case "/me/conversations":
			folder := r.URL.Query().Get("folder")
			if status, has := g.conversationsError[folder]; has {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"nao tem permissao para ler conversas","code":10}}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(g.conversationsBody[folder]))
		case "/me/subscribed_apps":
			w.WriteHeader(g.subStatus)
			_, _ = w.Write([]byte(g.subBody))
		default:
			t.Errorf("caminho inesperado no diagnostico: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// workingInstagramGraph builds a HEALTHY fixture: a BUSINESS account,
// token id EQUAL to the instance's ig_id (no divergence to report),
// permission granted with a conversation in two folders, and subscribed
// to the `messages` field.
func workingInstagramGraph(igID string) *fakeInstagramGraph {
	return &fakeInstagramGraph{
		meStatus: http.StatusOK,
		meBody:   `{"id":"` + igID + `","username":"lojinha_ig","account_type":"BUSINESS"}`,
		conversationsBody: map[string]string{
			"":          `{"data":[{"id":"c1"},{"id":"c2"}]}`,
			"other":     `{"data":[]}`,
			"page_done": `{"data":[]}`,
			"spam":      `{"data":[]}`,
			"requests":  `{"data":[{"id":"c3"}]}`,
		},
		conversationsError: map[string]int{},
		subStatus:          http.StatusOK,
		subBody:            `{"data":[{"subscribed_fields":["messages"]}]}`,
	}
}

// diagnosticScenario provisions a --tipo instagram instance and points
// the Instagram host resolution (ZAPGW_INSTAGRAM_REFRESH_BASE) at the
// fake — the SAME variable instagramRenewalBase(env) reads, and the
// SAME one SendInstagramMessage/RenewInstagramToken already use in
// this package's tests.
func diagnosticScenario(t *testing.T, slug, igID string, g *fakeInstagramGraph) map[string]string {
	t.Helper()
	vars := instagramProvisionedForRotation(t, slug, igID)
	vars["ZAPGW_INSTAGRAM_REFRESH_BASE"] = g.server(t).URL
	return vars
}

func diagnosticArgs(slug string) []string {
	return []string{"diagnostico", "--slug", slug}
}

// TestDiagnosticInstagramHealthyInstanceAnswersEveryQuestion is case
// (a) of T-109's Verify: a healthy Instagram instance -> every question
// comes out with a verdict, and the ENTIRE output contains no token.
func TestDiagnosticInstagramHealthyInstanceAnswersEveryQuestion(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_SAUDAVEL")
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_SAUDAVEL", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	for _, mark := range []string{"1) a conta do token", "2) a permissao de mensagens", "3) a inscricao de webhook", "4) o papel de testador"} {
		if !strings.Contains(text, mark) {
			t.Errorf("a saida nao tem a pergunta %q:\n%s", mark, text)
		}
	}
	// THE ENTIRE /me BODY HAS TO BE READ, not just id/username: a
	// json.Unmarshal missing the `json:"account_type"` tag on the
	// AccountType field (a real bug, caught by this test during T-109's
	// implementation) read the correct id and username and stayed silent
	// about the account type, always falling back to "(nao informado)"
	// even with Meta answering BUSINESS.
	if !strings.Contains(text, "tipo BUSINESS") {
		t.Errorf("a saida nao leu o account_type devolvido pela Meta (tipo BUSINESS):\n%s", text)
	}
	if strings.Contains(text, "(nao informado)") {
		t.Errorf("o tipo da conta saiu como \"nao informado\" apesar de a Meta ter respondido account_type:\n%s", text)
	}
	if !strings.Contains(text, "tudo o que da para ver DAQUI esta em ordem") {
		t.Errorf("instancia saudavel nao terminou com o veredito de \"em ordem\":\n%s", text)
	}
	if !strings.Contains(text, "nao_verificavel_daqui") {
		t.Errorf("a saida nao marca o item 4 como nao_verificavel_daqui:\n%s", text)
	}
	if strings.Contains(text, oldSend) {
		t.Errorf("O TOKEN APARECEU NA SAIDA DO DIAGNOSTICO:\n%s", text)
	}
}

// TestDiagnosticInstagramMissingPermissionDoesNotUseDebugToken is case (b):
// Meta rejects /me/conversations with a permission error (401/403 ->
// meta.ClassConfig) -> the verdict says the permission is missing, and
// the output NEVER mentions debug_token — this command does not call it
// (T-109, item 3b: it rejects an Instagram Login token on both hosts and
// is worthless).
func TestDiagnosticInstagramMissingPermissionDoesNotUseDebugToken(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_SEM_PERMISSAO")
	g.conversationsError[""] = http.StatusForbidden
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_SEM_PERMISSAO", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "recusado por PERMISSAO") {
		t.Errorf("a saida nao diz que a permissao foi recusada:\n%s", text)
	}
	if !strings.Contains(text, "instagram_business_manage_messages") {
		t.Errorf("a saida nao nomeia a permissao que falta:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "debug_token") {
		t.Errorf("a saida menciona debug_token — este comando NAO pode reintroduzi-lo (T-109, item 3b):\n%s", text)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, path := range g.paths {
		if strings.Contains(path, "debug_token") {
			t.Errorf("o comando BATEU em debug_token: %s", path)
		}
	}
}

// TestDiagnosticInstagramDivergentButExpectedIDIsNotAProblem is case (c):
// the `id` `/me` returns (App scope) diverges from the `ig_id` recorded
// on the instance (the webhook's `entry[].id`) -> the output STATES this
// is expected and does NOT go into the problem list. Confusing the two
// has already cost 4 discarded events in production (docs/TASKS.md,
// T-109, Why) — if this test goes red, the defect is back.
func TestDiagnosticInstagramDivergentButExpectedIDIsNotAProblem(t *testing.T) {
	g := workingInstagramGraph("IGID_DO_ESCOPO_DO_APP")
	// ig_id RECORDED on the instance is DIFFERENT from the id /me returns.
	vars := diagnosticScenario(t, "insta-loja", "IGID_ENTRY_DO_WEBHOOK", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "NORMAL") {
		t.Errorf("a saida nao diz que a divergencia de id e NORMAL:\n%s", text)
	}
	if !strings.Contains(text, "IGID_DO_ESCOPO_DO_APP") || !strings.Contains(text, "IGID_ENTRY_DO_WEBHOOK") {
		t.Errorf("a saida nao mostra os DOIS ids divergentes:\n%s", text)
	}
	// THE STRONG PROOF: with everything else healthy, the divergence
	// alone must NOT push the command to the "missing" verdict — it is
	// informational.
	if strings.Contains(text, "o que esta faltando") {
		t.Errorf("a divergencia de id ESPERADA foi tratada como problema:\n%s", text)
	}
	if !strings.Contains(text, "tudo o que da para ver DAQUI esta em ordem") {
		t.Errorf("instancia saudavel (so com id divergente, que e esperado) nao fechou em ordem:\n%s", text)
	}
}

// TestDiagnosticInstagramTesterRoleComesOutNotVerifiable is case (d): a
// check the gateway structurally CANNOT do (tester role in the App
// requires app_id and an administrator token the instance does not hold)
// comes out as `nao_verificavel_daqui` WITH A REASON — it never
// disappears from the output, it never becomes "ok" by omission.
func TestDiagnosticInstagramTesterRoleComesOutNotVerifiable(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_QUALQUER")
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_QUALQUER", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "4) o papel de testador") {
		t.Fatalf("a pergunta do papel de testador SUMIU da saida:\n%s", text)
	}
	if !strings.Contains(text, "nao_verificavel_daqui") {
		t.Errorf("o item 4 nao saiu como nao_verificavel_daqui:\n%s", text)
	}
	if strings.Contains(text, "[ok] este gateway") {
		t.Errorf("o item 4 saiu como \"ok\" por omissao — ele tem de dizer que NAO sabe:\n%s", text)
	}
	// The reason has to be in the SAME line/block, pastable into chat —
	// never just the label with no explanation.
	if !strings.Contains(text, "app_id") {
		t.Errorf("a saida nao diz O MOTIVO de o item 4 nao ser verificavel:\n%s", text)
	}
}

// TestDiagnosticInstagramCountWithoutFloorComesOutExact is case (a) of T-112's
// Verify: a folder with FEWER items than the page ceiling (no
// `paging.next` in the body) comes out with the EXACT number, with no `≥`
// floor marker.
//
// Forces FolderUnknown (withFolderResultForTest): this test
// proves the per-folder FORMATTING (T-112), which only prints a row when
// the folder sweep runs — and since T-114 the PRODUCTION value
// (FolderIgnored) stops the sweep before the first extra folder. Without
// forcing it, the "requests" folder would never be queried and the
// assertion below would go red for a reason that has nothing to do with
// what this test proves.
func TestDiagnosticInstagramCountWithoutFloorComesOutExact(t *testing.T) {
	withFolderResultForTest(t, meta.FolderUnknown)
	g := workingInstagramGraph("IGID_SINTETICO_CONTAGEM_EXATA")
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_CONTAGEM_EXATA", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "conversas na caixa padrao: 2 conversa(s)") {
		t.Errorf("a caixa padrao (2 itens, sem paging.next) nao saiu com numero exato:\n%s", text)
	}
	if !strings.Contains(text, `pasta "requests": 1 conversa(s)`) {
		t.Errorf("a pasta requests (1 item, sem paging.next) nao saiu com numero exato:\n%s", text)
	}
	if strings.Contains(text, "≥") {
		t.Errorf("uma pagina SEM paging.next nao pode sair marcada como piso (≥):\n%s", text)
	}
}

// TestDiagnosticInstagramCountWithFloorShowsPlusSign is case (b) of
// T-112's Verify: Meta returns `paging.next` filled in on the default
// inbox — this is a FLOOR ("at least N"), NEVER the total, and the output
// has to say so.
//
// The MANDATORY MUTATION of Verify (going back to printing raw
// `len(data)`) leaves this test red: without the floor marker, "2
// conversa(s)" would hit the NOT-contains assertion "conversas na caixa
// padrao: 2 conversa(s)" below, because the exact expected format requires
// the `≥` prefix.
func TestDiagnosticInstagramCountWithFloorShowsPlusSign(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_CONTAGEM_PISO")
	g.conversationsBody[""] = `{"data":[{"id":"c1"},{"id":"c2"}],"paging":{"next":"https://graph.instagram.com/me/conversations?after=X"}}`
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_CONTAGEM_PISO", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "conversas na caixa padrao: ≥ 2 conversa(s)") {
		t.Errorf("a caixa padrao com paging.next nao saiu marcada como piso:\n%s", text)
	}
	if !strings.Contains(text, "primeira pagina") {
		t.Errorf("a saida nao explica que o numero e' so a primeira pagina:\n%s", text)
	}
	// The negative proof: the raw number must NEVER appear as if it were
	// the total (without the `≥` on the same line as the default inbox).
	if strings.Contains(text, "conversas na caixa padrao: 2 conversa(s)\n") {
		t.Errorf("o piso saiu impresso como se fosse o total (sem o sinal ≥):\n%s", text)
	}
}

// TestDiagnosticInstagramAllFoldersEqualWarns is case (c) of T-112's
// Verify — the REAL production MEASUREMENT (v0.42.0, 2026-07-31): the
// five calls (default inbox + four folders) returned exactly the same
// number. The output has to say so, instead of listing five identical
// [ok] lines as if they were five independent measurements.
//
// Forces FolderUnknown: this test proves the text of the AMBIGUOUS
// branch (`default` in cmd/zapgw/diagnostico.go) — the warning that
// existed BEFORE T-114 proved the folder is ignored. Since T-114 the
// PRODUCTION value is FolderIgnored, which has its own text and
// mechanism (TestDiagnosticInstagramPerFolderSegregationNotObservable,
// below); without forcing it here, the folder sweep would not even run
// and this test's assertions (which depend on the live comparison between
// folders) would go red for a reason that already has its own test.
func TestDiagnosticInstagramAllFoldersEqualWarns(t *testing.T) {
	withFolderResultForTest(t, meta.FolderUnknown)
	g := workingInstagramGraph("IGID_SINTETICO_PASTAS_IGUAIS")
	bodyOfThree := `{"data":[{"id":"a"},{"id":"b"},{"id":"c"}]}`
	g.conversationsBody = map[string]string{
		"":          bodyOfThree,
		"other":     bodyOfThree,
		"page_done": bodyOfThree,
		"spam":      bodyOfThree,
		"requests":  bodyOfThree,
	}
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_PASTAS_IGUAIS", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "MESMO numero") {
		t.Errorf("a saida nao avisa que todas as pastas deram o mesmo numero:\n%s", text)
	}
	if !strings.Contains(text, "folder") {
		t.Errorf("o aviso nao nomeia o parametro `folder` como suspeito:\n%s", text)
	}
	if !strings.Contains(text, "NAO conclua") {
		t.Errorf("o aviso nao instrui a nao concluir em que gaveta a DM esta:\n%s", text)
	}
}

// TestDiagnosticInstagramPerFolderSegregationNotObservable is item (1) of
// T-114's Verify: with meta.MeasuredFolderResult at the PRODUCTION
// value (FolderIgnored, measured on 2026-07-31 15:54 -03) the
// diagnostic's output has to STATE, in plain words, that folder
// segregation is not observable through this API — never again the old
// conditional hedge ("may not be applied"). Does NOT use
// withFolderResultForTest: it is the only test in this file that
// must run with the production value exactly as it stands in the code,
// because it is exactly that configuration this test proves.
func TestDiagnosticInstagramPerFolderSegregationNotObservable(t *testing.T) {
	if meta.MeasuredFolderResult != meta.FolderIgnored {
		t.Fatalf("meta.MeasuredFolderResult = %v — este teste prova o comportamento de PRODUCAO "+
			"(o filtro de pasta e' ignorado, T-114); se o valor mudou de proposito, ajuste este teste junto",
			meta.MeasuredFolderResult)
	}

	g := workingInstagramGraph("IGID_SINTETICO_SEGREGACAO_NAO_OBSERVAVEL")
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_SEGREGACAO_NAO_OBSERVAVEL", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "NAO E OBSERVAVEL por esta API") {
		t.Errorf("a saida nao AFIRMA que a segregacao por pasta nao e observavel:\n%s", text)
	}
	if strings.Contains(text, "pode nao estar sendo aplicado") {
		t.Errorf("a saida ainda usa o texto CONDICIONAL antigo, que a T-114 devia ter fechado:\n%s", text)
	}

	// NEGATIVE PROOF: none of the four extra folders was queried — the
	// sweep stops as soon as MeasuredFolderResult is FolderIgnored
	// (meta.InstagramMessagingPermission). No call, no "pasta ..." line.
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, path := range g.paths {
		if strings.Contains(path, "folder=") {
			t.Errorf("uma chamada usou `folder` apesar de o filtro de pasta ja estar provado ignorado — a varredura nao parou: %s", path)
		}
	}
}

// TestDiagnosticInstagramPermissionVerdictDoesNotChangeWithFloor is case (d)
// of T-112's Verify: even with paging.next filled in (a floor) on the
// default inbox, the permission verdict stays "CONCEDIDA" — it never
// depended on the number, only on the endpoint having answered (T-112, Do
// item 4).
func TestDiagnosticInstagramPermissionVerdictDoesNotChangeWithFloor(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_VEREDICTO_PISO")
	g.conversationsBody[""] = `{"data":[{"id":"c1"},{"id":"c2"}],"paging":{"next":"https://graph.instagram.com/me/conversations?after=X"}}`
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_VEREDICTO_PISO", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "permissao CONCEDIDA (o endpoint de conversas respondeu)") {
		t.Errorf("o veredito de permissao mudou so por causa do piso:\n%s", text)
	}
}

// testInvalidFolder MIRRORS `instagramProbeInvalidFolder`
// (internal/meta/diagnostico_instagram.go) — unexported, so the `main`
// package's test repeats it here. If it changes value in the `meta`
// package, these tests go red (the query the fake server receives no
// longer matches), which is the correct signal that the duplication went
// stale.
const testInvalidFolder = "zzz-nao-existe-t113"

// TestDiagnosticInstagramWithoutEnvVarDoesNotProbeInvalidFolder is case (a) of
// T-113's Verify: without `ZAPGW_DIAGNOSTICO_SONDAR_FOLDER`, item 5 does
// not appear in the output and NO request uses the invalid folder — the
// probe is a MEASUREMENT, not part of the normal diagnostic (extra
// network cost only when requested).
func TestDiagnosticInstagramWithoutEnvVarDoesNotProbeInvalidFolder(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_SEM_SONDA")
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_SEM_SONDA", g)

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if strings.Contains(text, "5) sonda") {
		t.Errorf("o item 5 apareceu sem ZAPGW_DIAGNOSTICO_SONDAR_FOLDER:\n%s", text)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, path := range g.paths {
		if strings.Contains(path, testInvalidFolder) {
			t.Errorf("o comando bateu no folder invalido SEM a env var pedir: %s", path)
		}
	}
}

// TestDiagnosticInstagramInvalidFolderProbeAcceptedSaysIgnored is case
// (b): with the probe ON, the (fake) Meta ACCEPTS the invalid folder and
// returns the SAME list as the default inbox — the output has to SAY,
// in plain words, that the folder filter was ignored, and instruct
// pasting it into T-113's report (item 1 of Do). T-183: the guard
// matches the BEHAVIOUR sentence, never the name of the Go constant —
// a text that names a symbol goes stale on the next rename and no
// compiler notices.
func TestDiagnosticInstagramInvalidFolderProbeAcceptedSaysIgnored(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_SONDA_ACEITA")
	// The default inbox (workingInstagramGraph) has 2 items — the
	// same body for the invalid folder is what proves the "ignored"
	// hypothesis.
	g.conversationsBody[testInvalidFolder] = g.conversationsBody[""]
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_SONDA_ACEITA", g)
	vars["ZAPGW_DIAGNOSTICO_SONDAR_FOLDER"] = "1"

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "5) sonda do parametro `folder`") {
		t.Fatalf("o item 5 nao apareceu com a sonda ligada:\n%s", text)
	}
	if !strings.Contains(text, "ACEITOU") {
		t.Errorf("a saida nao diz que a Meta aceitou o folder invalido:\n%s", text)
	}
	if !strings.Contains(text, "o filtro de pasta foi IGNORADO") {
		t.Errorf("a saida nao DIZ que o filtro de pasta foi ignorado para o operador colar no relatorio:\n%s", text)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	found := false
	for _, path := range g.paths {
		if strings.Contains(path, testInvalidFolder) {
			found = true
		}
	}
	if !found {
		t.Errorf("nenhuma chamada usou o folder invalido apesar da sonda ligada: %v", g.paths)
	}
}

// TestDiagnosticInstagramInvalidFolderProbeRefusedSaysHonored is
// case (c): the (fake) Meta REJECTS the invalid folder — the output has
// to SAY, in plain words, that the folder filter was honoured. Same
// T-183 rule as the case above: the guard matches the behaviour
// sentence, not the name of the Go constant.
func TestDiagnosticInstagramInvalidFolderProbeRefusedSaysHonored(t *testing.T) {
	g := workingInstagramGraph("IGID_SINTETICO_SONDA_RECUSA")
	g.conversationsError[testInvalidFolder] = http.StatusBadRequest
	vars := diagnosticScenario(t, "insta-loja", "IGID_SINTETICO_SONDA_RECUSA", g)
	vars["ZAPGW_DIAGNOSTICO_SONDAR_FOLDER"] = "1"

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-loja"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	text := out.String()

	if !strings.Contains(text, "5) sonda do parametro `folder`") {
		t.Fatalf("o item 5 nao apareceu com a sonda ligada:\n%s", text)
	}
	if !strings.Contains(text, "RECUSOU") {
		t.Errorf("a saida nao diz que a Meta recusou o folder invalido:\n%s", text)
	}
	if !strings.Contains(text, "o filtro de pasta foi RESPEITADO") {
		t.Errorf("a saida nao DIZ que o filtro de pasta foi respeitado para o operador colar no relatorio:\n%s", text)
	}
}

// TestDiagnosticRefusesWhatsAppInstance is the guard for item 7 of
// T-109: this slice only covers --tipo instagram. A WhatsApp instance has
// to be REJECTED, never silently ignored or treated as an empty
// Instagram.
func TestDiagnosticRefusesWhatsAppInstance(t *testing.T) {
	vars := testEnvironment(t)
	var out bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provisionar instancia: %v", err)
	}
	out.Reset()

	err := dispatch(diagnosticArgs("lojinha"), &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("o diagnostico aceitou uma instancia --tipo whatsapp")
	}
	if !strings.Contains(err.Error(), "instagram") {
		t.Errorf("o erro nao explica que so instagram e coberto: %v", err)
	}
}

// TestDiagnosticAbortsWhenTheInstanceDoesNotExist mirrors
// TestSmokeAbortsWhenTheInstanceDoesNotExist: a nonexistent slug must not
// hit the Graph API at all.
func TestDiagnosticAbortsWhenTheInstanceDoesNotExist(t *testing.T) {
	vars := testEnvironment(t)
	var out bytes.Buffer
	err := dispatch(diagnosticArgs("nao-existe"), &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("o comando aceitou um slug inexistente")
	}
}
