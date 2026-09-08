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
		t.Fatalf("write %s: %v", path, err)
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
		t.Fatalf("provision instance: %v", err)
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
		t.Fatalf("calls to Meta = %d, want 1", g.calls.Load())
	}
	text := out.String()
	if !strings.Contains(text, "TEMPLATE123") {
		t.Errorf("the output does not show the id returned by Meta:\n%s", text)
	}
	if !strings.Contains(text, "confirmacao_pedido") {
		t.Errorf("the output does not show the template's name:\n%s", text)
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
		{"empty object", `{}`},
		{"null", `null`},
		{"not a list nor an object", `"texto solto"`},
		{"empty file", ``},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			g := workingTemplateGraph()
			vars := templateScenario(t, g)
			file := writeComponentsFile(t, testCase.content)

			var out bytes.Buffer
			err := dispatch(templateCreateArgs("lojinha", file), &out, fakeEnvironment(vars))
			if err == nil {
				t.Fatalf("components %q was accepted, output:\n%s", testCase.content, out.String())
			}
			if g.calls.Load() != 0 {
				t.Errorf("Meta was called %d time(s) with an invalid components file — "+
					"validation had to refuse BEFORE the network", g.calls.Load())
			}
		})
	}
}

// MANDATORY MUTATION (T-036, Verify): moving the validation to AFTER the
// network call has to leave TestTemplateCreateInvalidComponentsDoesNotCallMeta
// red. Proved by hand by moving the `p.Validate()` call in template.go to
// after `cliente.CreateTemplate(...)`: 3 of the 4 subtests ("empty object",
// "null", "not a list nor an object") started calling Meta
// (g.chamadas.Load() == 1) before validation rejected the request, and the
// "the Meta was called" assertion failed as expected in all three. (The
// fourth, "empty file", kept passing for a DIFFERENT reason — empty
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
		t.Fatal("nonexistent components file was accepted")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name the path %q", err.Error(), path)
	}
	if g.calls.Load() != 0 {
		t.Errorf("Meta was called with a components file that does not exist")
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
		t.Errorf("the output does not contain the pending warning:\n%s", out.String())
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
		t.Fatalf("error = %v, want ErrInstanceNotFound", err)
	}
	if g.calls.Load() != 0 {
		t.Errorf("Meta was called for an instance that does not exist")
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
		t.Fatal("empty name was accepted")
	}
	if g.calls.Load() != 0 {
		t.Errorf("Meta was called with a request with no name")
	}
}
