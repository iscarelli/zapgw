package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHealthAnswersOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()

	routes(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
}

// The per-instance probe route has to be REGISTERED here. Without this
// the handler exists, its tests pass green, and
// /v1/instances/{slug}/health returns 404 in production — this project's
// favorite defect: nothing flags it.
func TestRoutesRegistersThePerInstanceProbe(t *testing.T) {
	var arrived bool
	health := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { arrived = true })

	req := httptest.NewRequest(http.MethodGet, "/v1/instances/lojinha/health", nil)
	rec := httptest.NewRecorder()
	routes(nil, nil, health, nil, nil, nil, nil, nil, nil, nil, nil, nil).ServeHTTP(rec, req)

	if !arrived {
		t.Fatalf("a requisicao nao chegou ao handler de saude (status = %d)", rec.Code)
	}
}

// The catalog route has to be REGISTERED here, for the same reason as the
// probe: the handler exists, its tests pass green, and /v1/templates
// returns 404 in production — indistinguishable from "this gateway
// version doesn't have this route", which is how the consumer draws the
// wrong conclusion and no one looks in the right place.
func TestRoutesRegistersTheTemplateCatalog(t *testing.T) {
	var methods []string
	templates := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
	})

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/v1/templates?instancia=lojinha", nil)
		rec := httptest.NewRecorder()
		routes(nil, nil, nil, templates, nil, nil, nil, nil, nil, nil, nil, nil).ServeHTTP(rec, req)
		if len(methods) == 0 || methods[len(methods)-1] != method {
			t.Fatalf("%s /v1/templates nao chegou ao handler (status = %d)", method, rec.Code)
		}
	}
}

// The media route has TWO patterns — upload (/v1/media) and download by
// id (/v1/media/) — and both have to be REGISTERED here, for the same
// reason as the probe and the catalog: the handler exists, its tests pass
// green, and the route returns 404 in production (or worse, redirects
// without going anywhere) — indistinguishable from "this gateway version
// doesn't have this route". The two patterns are exercised SEPARATELY
// because they cover different halves of the ServeMux: `/v1/media`
// without a slash is an EXACT pattern, `/v1/media/` with a slash is a
// SUBTREE pattern — removing one of the two `mux.Handle` calls leaves the
// other standing, and only a test that hits both paths catches both
// mutations.
func TestRoutesRegistersTheMediaRoutes(t *testing.T) {
	var paths []string
	media := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
	})

	for _, path := range []string{"/v1/media", "/v1/media/abc123"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		routes(nil, nil, nil, nil, media, nil, nil, nil, nil, nil, nil, nil).ServeHTTP(rec, req)
		if len(paths) == 0 || paths[len(paths)-1] != path {
			t.Fatalf("%s nao chegou ao handler de midia (status = %d)", path, rec.Code)
		}
	}
}

// The estado route (T-060) has to be REGISTERED here, for the same
// reason as the three above: the handler exists, its tests pass green,
// and /v1/estado returns 404 in production. The cost of this defect
// family has already been paid once in this project (T-014, the probe
// answering 404 on :8443 with the gateway correctly serving it), and the
// symptom sends people to investigate deploy and code when the problem is
// routing.
func TestRoutesRegistersThePerInstanceState(t *testing.T) {
	var arrived bool
	state := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { arrived = true })

	req := httptest.NewRequest(http.MethodGet, "/v1/estado?instancia=lojinha", nil)
	rec := httptest.NewRecorder()
	routes(nil, nil, nil, nil, nil, state, nil, nil, nil, nil, nil, nil).ServeHTTP(rec, req)

	if !arrived {
		t.Fatalf("a requisicao nao chegou ao handler de estado (status = %d)", rec.Code)
	}
}

// The leituras route (T-075) has to be REGISTERED here, for the same
// reason as the four above: without the mux.Handle, the handler exists,
// its tests pass green, and POST /v1/leituras returns 404 in production —
// and the consumer concludes "this gateway version doesn't have the route
// yet", which is the most expensive wrong conclusion possible, because it
// sends them to wait for something that is already live.
func TestRoutesRegistersTheReads(t *testing.T) {
	var arrived bool
	reads := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { arrived = true })

	req := httptest.NewRequest(http.MethodPost, "/v1/leituras", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	routes(nil, nil, nil, nil, nil, nil, reads, nil, nil, nil, nil, nil).ServeHTTP(rec, req)

	if !arrived {
		t.Fatalf("a requisicao nao chegou ao handler de leituras (status = %d)", rec.Code)
	}
}

// The cadastro route (T-079) has to be REGISTERED, and here the cost of
// forgetting is the biggest of all: whoever hits it is a THIRD PARTY,
// with no channel to ask. A 404 would tell them "this gateway has no
// cadastro by API" — and they would go ask someone for the values, which
// is exactly what the model exists to end.
func TestRoutesRegistersTheEnrollment(t *testing.T) {
	var arrived bool
	enrollment := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { arrived = true })

	req := httptest.NewRequest(http.MethodPost, "/v1/cadastro", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	routes(nil, nil, nil, nil, nil, nil, nil, enrollment, nil, nil, nil, nil).ServeHTTP(rec, req)

	if !arrived {
		t.Fatalf("a requisicao nao chegou ao handler de cadastro (status = %d)", rec.Code)
	}
}

// The fumaca and pausa routes (T-084) have to be REGISTERED, for the same
// reason as cadastro: without the mux.Handle, the handler exists, its
// tests pass green, and the route returns 404 in production — and for a
// THIRD-PARTY consumer, with no channel, that is a dead end, not a "try
// again later".
func TestRoutesRegistersTheSmokeTest(t *testing.T) {
	var arrived bool
	smoke := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { arrived = true })

	req := httptest.NewRequest(http.MethodPost, "/v1/fumaca", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	routes(nil, nil, nil, nil, nil, nil, nil, nil, smoke, nil, nil, nil).ServeHTTP(rec, req)

	if !arrived {
		t.Fatalf("a requisicao nao chegou ao handler de fumaca (status = %d)", rec.Code)
	}
}

func TestRoutesRegistersThePause(t *testing.T) {
	var arrived bool
	pause := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { arrived = true })

	req := httptest.NewRequest(http.MethodPost, "/v1/pausa", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	routes(nil, nil, nil, nil, nil, nil, nil, nil, nil, pause, nil, nil).ServeHTTP(rec, req)

	if !arrived {
		t.Fatalf("a requisicao nao chegou ao handler de pausa (status = %d)", rec.Code)
	}
}

// The bloqueio route (T-148) has to be REGISTERED, for the same reason as
// the rest: without the mux.Handle, the handler exists, its tests pass
// green, and POST/DELETE/GET /v1/bloqueios return 404 in production.
func TestRoutesRegistersTheBlock(t *testing.T) {
	var methods []string
	blocking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
	})

	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodGet} {
		req := httptest.NewRequest(method, "/v1/bloqueios", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		routes(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, blocking, nil).ServeHTTP(rec, req)
		if len(methods) == 0 || methods[len(methods)-1] != method {
			t.Fatalf("%s /v1/bloqueios nao chegou ao handler (status = %d)", method, rec.Code)
		}
	}
}

// The perfil route (T-155) has to be REGISTERED, for the same reason as
// the rest: without the mux.Handle, the handler exists, its tests pass
// green, and GET/POST /v1/perfil return 404 in production.
func TestRoutesRegistersTheProfile(t *testing.T) {
	var methods []string
	profile := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
	})

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/v1/perfil?instancia=lojinha", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		routes(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, profile).ServeHTTP(rec, req)
		if len(methods) == 0 || methods[len(methods)-1] != method {
			t.Fatalf("%s /v1/perfil nao chegou ao handler (status = %d)", method, rec.Code)
		}
	}
}

// /v1/health is deliberately NOT informative: it only proves the process
// is up. The channel's health (token revoked by the client) is a
// different route, in plan 2.
func TestHealthReturnsJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()

	routes(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, quero application/json", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo nao desserializa como JSON: %v (corpo = %q)", err, rec.Body.String())
	}
	// NON-REGRESSION (T-025): `ok` is this gateway's public guarantee. A
	// consumer that only reads `ok` must not break when the format gains
	// a new field (`versao`) — that is why the assertion looks at ONLY
	// this field, the way the least attentive consumer would look.
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf(`corpo["ok"] = %#v, quero true`, body["ok"])
	}
}

// (a) WITHOUT -ldflags injection, `versao` stays at the default
// "desenvolvimento" — never a plausible number like "0.0.0" (T-025): a
// made-up number is more dangerous than the declared unknown, because it
// is believable.
func TestHealthWithoutInjectionReturnsDevelopmentVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()

	routes(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).ServeHTTP(rec, req)

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if !body.OK {
		t.Fatalf("ok = %v, quero true", body.OK)
	}
	if body.Version != "desenvolvimento" {
		t.Fatalf(`versao = %q, quero "desenvolvimento"`, body.Version)
	}
}

// (a) the same, on the `zapgw versao` subcommand side.
func TestVersionCommandWithoutInjectionPrintsDevelopment(t *testing.T) {
	var out strings.Builder
	if err := dispatch([]string{"versao"}, &out, os.Getenv); err != nil {
		t.Fatalf("dispatch([]string{\"versao\"}, ...): %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "desenvolvimento" {
		t.Fatalf(`zapgw versao = %q, quero "desenvolvimento"`, got)
	}
}

// buildWithVersion compiles the REAL BINARY (not the test) with
// -ldflags "-X main.version=…" and returns the path to the executable.
//
// IT IS THE ONLY WAY to prove -ldflags injection works. A test that only
// checks `versao == "desenvolvimento"` (the two above) proves the
// DEFAULT, never the flag — T-025 itself explicitly requires the
// injection test to run a compilation with it, otherwise it proves the
// default again under a different name.
func buildWithVersion(t *testing.T, injectedVersion string) string {
	t.Helper()
	name := "zapgw-teste-versao"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)

	cmd := exec.Command("go", "build",
		"-ldflags", "-X main.version="+injectedVersion,
		"-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build -ldflags \"-X main.version=%s\": %v\n%s", injectedVersion, err, out)
	}
	return bin
}

// freeAddress returns a `127.0.0.1:port` address that is free AT THE
// MOMENT of the call, for the compiled binary to listen on. The window
// between closing this probing listener and the child process opening
// its own is short — it is the same idiom used across the rest of the Go
// ecosystem for "get me a free port".
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	address := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("fechar o listener de sondagem: %v", err)
	}
	return address
}

// startServerAndGetHealth starts `bin` (with NO argument at all — the
// server path, main.go) as a real process, points ZAPGW_ENDERECO at a
// free port, waits for /v1/health to answer 200, and returns the body.
// The process is killed at the end of the test.
func startServerAndGetHealth(t *testing.T, bin string) []byte {
	t.Helper()
	address := freeAddress(t)

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"ZAPGW_CHAVE_CIFRA="+testKey,
		"ZAPGW_BANCO="+filepath.Join(t.TempDir(), "zapgw.db"),
		"ZAPGW_ENDERECO="+address,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("iniciar %s: %v", bin, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(15 * time.Second)
	var lastError error
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + address + "/v1/health")
		if err != nil {
			lastError = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastError = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastError = fmt.Errorf("status %d: %s", resp.StatusCode, body)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return body
	}
	t.Fatalf("/v1/health em %s nao respondeu a tempo: %v\nstderr do processo:\n%s",
		address, lastError, stderr.String())
	return nil
}

// (b) WITH `-ldflags "-X main.version=9.9.9"`, BOTH paths that depend on
// the version return 9.9.9: `zapgw versao` and `GET /v1/health`. This is
// the proof T-025 explicitly asks for — the two above prove the default,
// this one proves the INJECTION.
func TestVersionInjectedByLdflagsPropagatesToBothPaths(t *testing.T) {
	const injectedVersion = "9.9.9"
	bin := buildWithVersion(t, injectedVersion)

	out, err := exec.Command(bin, "versao").Output()
	if err != nil {
		t.Fatalf("%s versao: %v", bin, err)
	}
	if got := strings.TrimSpace(string(out)); got != injectedVersion {
		t.Fatalf("%s versao = %q, quero %q", bin, got, injectedVersion)
	}

	body := startServerAndGetHealth(t, bin)
	var resp healthResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("corpo do /v1/health nao desserializa: %v (%s)", err, body)
	}
	if !resp.OK {
		t.Fatalf("ok = %v, quero true (corpo = %s)", resp.OK, body)
	}
	if resp.Version != injectedVersion {
		t.Fatalf("versao no /v1/health = %q, quero %q", resp.Version, injectedVersion)
	}
}

// T-115 (3): proves that startPeriodicPurge's recover() really lets
// the SECOND loop round happen after a panic in the first — the
// guarantee the function's comment promises ("without it, a panic...
// kills the goroutine... and the mechanism silently stops running for
// the rest of the process's life"). `go tool cover` showed this function
// at 0% before this test. It needed NO new field: `purgar` has always
// been injected by PARAMETER — the same discipline this file already
// requires of Watchdog/InstagramRenewer, except here it was already in
// place.
func TestStartPeriodicPurgeSurvivesAPanicAndContinuesOnTheNextTick(t *testing.T) {
	// The goroutine never stops (there is no "stop"), so it keeps ticking
	// after this test ends — that is why closing the channel only
	// happens on EXACTLY the second call, never again.
	var calls int
	secondRound := make(chan struct{})
	purge := func() (int, error) {
		calls++
		switch calls {
		case 1:
			panic("panico de teste — primeira volta")
		case 2:
			close(secondRound)
		}
		return 0, nil
	}

	startPeriodicPurge("teste-t115", time.Millisecond, purge)

	select {
	case <-secondRound:
		// the second round happened — the panic from the first did NOT kill the loop.
	case <-time.After(5 * time.Second):
		t.Fatal("a segunda volta do laco nunca aconteceu depois do panico — o recover nao protegeu a goroutine")
	}
}
