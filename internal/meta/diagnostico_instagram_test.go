// Tests for MeasuredFolderResult and ProbeInvalidInstagramFolder
// (T-113) — the SINGLE DECISION POINT T-113 introduced and the MEASUREMENT
// item 1 requires. The "healthy diagnostic" tests stay in
// cmd/zapgw/diagnostico_test.go, which checks the formatted OUTPUT; here
// the concern is the REQUEST: how many go out and with which `folder`.
package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// conversationCounter counts, per requested `folder` (key "" for the
// default inbox), how many times /me/conversations was called — it's the
// way to prove request counts without interpreting the formatted output.
type conversationCounter struct {
	mu       sync.Mutex
	byFolder map[string]int
}

func (c *conversationCounter) server(t *testing.T) *httptest.Server {
	t.Helper()
	c.byFolder = map[string]int{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/conversations" {
			t.Errorf("caminho inesperado: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		folder := r.URL.Query().Get("folder")
		c.mu.Lock()
		c.byFolder[folder]++
		c.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"c1"}]}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func (c *conversationCounter) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.byFolder {
		n += v
	}
	return n
}

// withFolderResult swaps MeasuredFolderResult for the duration of
// the test and restores the original value in Cleanup — the very
// save/restore MeasuredFolderResult's comment promises: the variable is
// only exported for this, never for mutation in production (that's still
// code edit + recompile, one line).
func withFolderResult(t *testing.T, value FolderFilterResult) {
	t.Helper()
	original := MeasuredFolderResult
	MeasuredFolderResult = value
	t.Cleanup(func() { MeasuredFolderResult = original })
}

// TestInstagramMessagingPermissionSweepsTheFourFoldersWhenUnknown is
// the BEFORE-the-measurement behavior (T-113/T-114): with
// FolderUnknown, the sweep makes all FIVE calls — the default inbox
// plus the four folders. T-114 (2026-07-31) MEASURED the answer and
// switched the PRODUCTION value to FolderIgnored (see
// MeasuredFolderResult, diagnostico_instagram.go) — this test still
// exists, now with explicit save/restore via withFolderResult, to
// prove that the OLD behavior (the full sweep) stays correct for whoever
// is still on FolderUnknown; it stopped being a read of the
// production value because that value has already changed.
func TestInstagramMessagingPermissionSweepsTheFourFoldersWhenUnknown(t *testing.T) {
	withFolderResult(t, FolderUnknown)

	g := &conversationCounter{}
	srv := g.server(t)

	_, err := testClient(srv).InstagramMessagingPermission(context.Background(), srv.URL, "token")
	if err != nil {
		t.Fatalf("InstagramMessagingPermission: %v", err)
	}

	if got := g.total(); got != 5 {
		t.Errorf("total de chamadas a /me/conversations = %d, quero 5 (caixa padrao + 4 pastas)", got)
	}
	for _, folder := range []string{"", "other", "page_done", "spam", "requests"} {
		if g.byFolder[folder] != 1 {
			t.Errorf("folder %q chamado %d vez(es), quero exatamente 1", folder, g.byFolder[folder])
		}
	}
}

// TestInstagramMessagingPermissionStopsSweepingWhenFolderIgnored proves
// the OTHER side of the decision point (T-113, Do item 2): when
// MeasuredFolderResult = FolderIgnored, the four-extra-folder sweep
// STOPS — only the default inbox is queried. Without this test,
// InstagramMessagingPermission's `if` would only be exercised on the day
// someone changed the value in production — too late to catch an inverted
// `if`.
func TestInstagramMessagingPermissionStopsSweepingWhenFolderIgnored(t *testing.T) {
	withFolderResult(t, FolderIgnored)

	g := &conversationCounter{}
	srv := g.server(t)

	_, err := testClient(srv).InstagramMessagingPermission(context.Background(), srv.URL, "token")
	if err != nil {
		t.Fatalf("InstagramMessagingPermission: %v", err)
	}

	if got := g.total(); got != 1 {
		t.Errorf("total de chamadas a /me/conversations = %d, quero 1 (so a caixa padrao) com FolderIgnored", got)
	}
	if g.byFolder[""] != 1 {
		t.Errorf("a caixa padrao nao foi chamada com FolderIgnored: %v", g.byFolder)
	}
	for _, folder := range []string{"other", "page_done", "spam", "requests"} {
		if n := g.byFolder[folder]; n != 0 {
			t.Errorf("pasta %q foi chamada %d vez(es) com FolderIgnored — a varredura tinha de ter parado", folder, n)
		}
	}
}

// TestInstagramMessagingPermissionFolderHonoredAloneDoesNotChangeTheSweep
// proves the half of MeasuredFolderResult's comment that's easy to
// forget: FolderHonored ALONE isn't enough to fix the sweep (it would
// still need pagination/a small limit, which is future work) — it only
// changes the warning's TEXT (cmd/zapgw/diagnostico.go). The five calls
// keep going out the way they do today.
func TestInstagramMessagingPermissionFolderHonoredAloneDoesNotChangeTheSweep(t *testing.T) {
	withFolderResult(t, FolderHonored)

	g := &conversationCounter{}
	srv := g.server(t)

	_, err := testClient(srv).InstagramMessagingPermission(context.Background(), srv.URL, "token")
	if err != nil {
		t.Fatalf("InstagramMessagingPermission: %v", err)
	}

	if got := g.total(); got != 5 {
		t.Errorf("total de chamadas a /me/conversations = %d, quero 5 (FolderHonored sozinho nao reduz a varredura)", got)
	}
}

// TestProbeInvalidInstagramFolderUsesAFolderMetaNeverDocumented is
// T-113's item 1: the probe has to send a `folder` outside Meta's
// vocabulary (other/page_done/spam/requests) — otherwise it would measure
// the wrong question.
func TestProbeInvalidInstagramFolderUsesAFolderMetaNeverDocumented(t *testing.T) {
	var receivedFolder string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedFolder = r.URL.Query().Get("folder")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"c1"},{"id":"c2"}]}`))
	}))
	defer srv.Close()

	probe, err := testClient(srv).ProbeInvalidInstagramFolder(context.Background(), srv.URL, "token")
	if err != nil {
		t.Fatalf("ProbeInvalidInstagramFolder: %v", err)
	}
	for _, folder := range []string{"", "other", "page_done", "spam", "requests"} {
		if receivedFolder == folder {
			t.Errorf("a sonda usou %q — um folder DOCUMENTADO pela Meta, nao mede a hipotese do item 1", receivedFolder)
		}
	}
	if receivedFolder == "" {
		t.Error("a sonda nao mandou `folder` nenhum")
	}
	if probe.N != 2 {
		t.Errorf("N = %d, quero 2 (o corpo de teste tem 2 itens)", probe.N)
	}
}

// TestProbeInvalidInstagramFolderReturnsTheErrorWhenMetaRefuses is the
// OTHER possible outcome of item 1: if Meta rejects the invalid folder,
// the probe returns the classified error — it's the signal for
// FolderHonored.
func TestProbeInvalidInstagramFolderReturnsTheErrorWhenMetaRefuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Unsupported get request. Invalid folder","code":100}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).ProbeInvalidInstagramFolder(context.Background(), srv.URL, "token")
	if err == nil {
		t.Fatal("a sonda nao devolveu erro apesar de a Meta ter recusado o folder invalido")
	}
}
