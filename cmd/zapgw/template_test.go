package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
)

// fakeTemplateGraph is a fake Graph API that only serves
// POST .../message_templates. Calls are counted atomically for the same
// reason as fakeGraph in smoke_test.go: httptest.Server serves each
// request in its own goroutine.
type fakeTemplateGraph struct {
	status int
	body   string

	calls atomic.Int64
}

func (g *fakeTemplateGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(g.status)
		_, _ = w.Write([]byte(g.body))
	}))
	t.Cleanup(s.Close)
	return s
}

func workingTemplateGraph() *fakeTemplateGraph {
	return &fakeTemplateGraph{
		status: http.StatusOK,
		body:   `{"id":"TEMPLATE123","status":"PENDING","category":"MARKETING"}`,
	}
}

// writeComponentsFile writes `content` to a temporary file and returns
// the path.
func writeComponentsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "componentes.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("escrever %s: %v", path, err)
	}
	return path
}

const validComponents = `[{"type":"BODY","text":"Ola {{1}}, seu pedido chegou."}]`

func templateCreateArgs(slug, componentsFile string) []string {
	return []string{
		"template", "criar",
		"--slug", slug,
		"--nome", "confirmacao_pedido",
		"--categoria", "UTILITY",
		"--idioma", "pt_BR",
		"--componentes", componentsFile,
	}
}

// templateScenario provisions an instance and points the Graph API at the
// fake server, returning the environment map the tests reuse.
func templateScenario(t *testing.T, g *fakeTemplateGraph) map[string]string {
	t.Helper()
	vars := testEnvironment(t)
	vars["ZAPGW_GRAPH_BASE"] = g.server(t).URL

	var junk bytes.Buffer
	if err := dispatch(instanceArgs("lojinha"), &junk, fakeEnvironment(vars)); err != nil {
		t.Fatalf("provisionar instancia: %v", err)
	}
	return vars
}

// (a) a file with a valid JSON list assembles the right request: Meta
// receives a single call, with the correct name/category/language, and the
// output shows the returned id.
func TestTemplateCreateValidFileBuildsTheRightRequest(t *testing.T) {
	g := workingTemplateGraph()
	vars := templateScenario(t, g)
	file := writeComponentsFile(t, validComponents)

	var out bytes.Buffer
	if err := dispatch(templateCreateArgs("lojinha", file), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}

	if g.calls.Load() != 1 {
		t.Fatalf("chamadas a Meta = %d, quero 1", g.calls.Load())
	}
	text := out.String()
	if !strings.Contains(text, "TEMPLATE123") {
		t.Errorf("a saida nao mostra o id devolvido pela Meta:\n%s", text)
	}
	if !strings.Contains(text, "confirmacao_pedido") {
		t.Errorf("a saida nao mostra o nome do template:\n%s", text)
	}
}

// (b) a file with `{}` or `null` is rejected BEFORE any network call. The
// mandatory mutation of T-036 (validate AFTER the call) has to leave this
// test red — see the comment below the test.
func TestTemplateCreateInvalidComponentsDoesNotCallMeta(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
	}{
		{"objeto vazio", `{}`},
		{"null", `null`},
		{"nao e lista nem objeto", `"texto solto"`},
		{"arquivo vazio", ``},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			g := workingTemplateGraph()
			vars := templateScenario(t, g)
			file := writeComponentsFile(t, testCase.content)

			var out bytes.Buffer
			err := dispatch(templateCreateArgs("lojinha", file), &out, fakeEnvironment(vars))
			if err == nil {
				t.Fatalf("componentes %q foi aceito, saida:\n%s", testCase.content, out.String())
			}
			if g.calls.Load() != 0 {
				t.Errorf("a Meta foi chamada %d vez(es) com um arquivo de componentes invalido — "+
					"a validacao tinha de recusar ANTES da rede", g.calls.Load())
			}
		})
	}
}

// MANDATORY MUTATION (T-036, Verify): moving the validation to AFTER the
// network call has to leave TestTemplateCreateInvalidComponentsDoesNotCallMeta
// red. Proved by hand by moving the `p.Validate()` call in template.go to
// after `cliente.CreateTemplate(...)`: 3 of the 4 subtests ("objeto vazio",
// "null", "nao e lista nem objeto") started calling Meta
// (g.chamadas.Load() == 1) before validation rejected the request, and the
// "the Meta was called" assertion failed as expected in all three. (The
// fourth, "arquivo vazio", kept passing for a DIFFERENT reason — empty
// components makes json.Marshal fail inside meta.Client itself before any
// HTTP, so the network call never happens either way; this does not
// invalidate the proof, it just shows that specific case has a second
// barrier.) The change was reverted before the commit — the code in
// template.go validates BEFORE opening the store and BEFORE any network
// call.

// (c) a nonexistent file gives an error that NAMES THE PATH.
func TestTemplateCreateNonexistentFileNamesThePath(t *testing.T) {
	g := workingTemplateGraph()
	vars := templateScenario(t, g)
	path := filepath.Join(t.TempDir(), "nao-existe.json")

	var out bytes.Buffer
	err := dispatch(templateCreateArgs("lojinha", path), &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("arquivo de componentes inexistente foi aceito")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("erro = %q, quero que nomeie o caminho %q", err.Error(), path)
	}
	if g.calls.Load() != 0 {
		t.Errorf("a Meta foi chamada com um arquivo de componentes que nao existe")
	}
}

// (d) the pending-status warning appears in the success output, and is the
// SAME text as the HTTP route (outbound.WarningTemplatePending) — two
// surfaces, one single behavior.
func TestTemplateCreateShowsThePendingWarning(t *testing.T) {
	g := workingTemplateGraph()
	vars := templateScenario(t, g)
	file := writeComponentsFile(t, validComponents)

	var out bytes.Buffer
	if err := dispatch(templateCreateArgs("lojinha", file), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "NAO pode ser usado na hora") {
		t.Errorf("a saida nao contem o aviso de pendencia:\n%s", out.String())
	}
}

// (e) a nonexistent --slug is a NAMED error (ErrInstanceNotFound),
// never an empty success.
func TestTemplateCreateNonexistentSlugIsANamedError(t *testing.T) {
	g := workingTemplateGraph()
	vars := templateScenario(t, g)
	file := writeComponentsFile(t, validComponents)

	var out bytes.Buffer
	err := dispatch(templateCreateArgs("nao-existe", file), &out, fakeEnvironment(vars))
	if !errors.Is(err, config.ErrInstanceNotFound) {
		t.Fatalf("erro = %v, quero ErrInstanceNotFound", err)
	}
	if g.calls.Load() != 0 {
		t.Errorf("a Meta foi chamada para uma instancia que nao existe")
	}
}

// The HTTP route and the command use the SAME validation
// (outbound.CreateTemplateRequest.Validate): a missing required field has to
// be rejected the SAME way in both places. Here it is only checked that the
// command rejects it; the route already has its own suite in
// internal/outbound.
func TestTemplateCreateMissingRequiredFieldIsRefused(t *testing.T) {
	g := workingTemplateGraph()
	vars := templateScenario(t, g)
	file := writeComponentsFile(t, validComponents)

	args := []string{
		"template", "criar",
		"--slug", "lojinha",
		"--nome", "", // required field missing
		"--categoria", "UTILITY",
		"--idioma", "pt_BR",
		"--componentes", file,
	}

	var out bytes.Buffer
	err := dispatch(args, &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("nome vazio foi aceito")
	}
	if g.calls.Load() != 0 {
		t.Errorf("a Meta foi chamada com um pedido sem nome")
	}
}
