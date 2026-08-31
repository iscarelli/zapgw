package outbound

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

func testPause(t *testing.T) (http.Handler, *config.Store) {
	t.Helper()
	store, _ := storeWithConsumer(t)
	h := NewPauseHandler(store, NewAuthenticator(store), 1<<20, AllTypes)
	return h, store
}

func pauseBody(slug string) string {
	return `{"instancia":"` + slug + `"}`
}

func askPause(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/pausa", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPauseRouteDeactivatesTheInstance(t *testing.T) {
	h, store := testPause(t)
	if err := store.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance (preparo do teste): %v", err)
	}

	rec := askPause(t, h, "token-do-a", pauseBody("lojinha"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	var resp PauseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
	}
	if resp.State != "pausada" || !resp.Paused {
		t.Errorf("estado = %q pausada = %t, quero pausada/true", resp.State, resp.Paused)
	}

	i, err := store.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Active {
		t.Fatal("a instancia continua ATIVA depois da pausa")
	}
}

// Verify (d): pausing and then running smoke test again activates the
// instance — the ONLY way back that exists.
func TestPauseRouteFollowedBySmokeActivatesAgain(t *testing.T) {
	g := workingSmokeGraph(t)
	store, _ := storeWithConsumer(t)
	if err := store.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance (preparo do teste): %v", err)
	}

	pause := NewPauseHandler(store, NewAuthenticator(store), 1<<20, AllTypes)
	if rec := askPause(t, pause, "token-do-a", pauseBody("lojinha")); rec.Code != http.StatusOK {
		t.Fatalf("pausar: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	i, err := store.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance apos pausar: %v", err)
	}
	if i.Active {
		t.Fatal("a instancia continua ATIVA depois da pausa")
	}

	smoke := NewSmokeHandler(store, NewAuthenticator(store),
		meta.NewClient(g.srv.Client(), g.srv.URL), config.NewCounter(store), 1<<20, AllTypes)
	if rec := askSmoke(t, smoke, "token-do-a", smokeBody("lojinha", testSmokeDestination)); rec.Code != http.StatusOK {
		t.Fatalf("fumaca depois da pausa: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	i, err = store.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance apos fumaca: %v", err)
	}
	if !i.Active {
		t.Fatal("a instancia continua PAUSADA depois de um fumaca que passou")
	}
}

// --- The guards ---------------------------------------------------------------

func TestPauseRouteRefusesInstanceNotOwnedByConsumerBefore404(t *testing.T) {
	h, _ := testPause(t)

	rec := askPause(t, h, "token-do-a", pauseBody("clinica"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, quero 403", rec.Code)
	}
}

func TestPauseRouteRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	h, _ := testPause(t)

	if rec := askPause(t, h, "", pauseBody("lojinha")); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
	if rec := askPause(t, h, "token-errado", pauseBody("lojinha")); rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status = %d, quero 401", rec.Code)
	}
}

func TestPauseRouteRefusesIncompleteBody(t *testing.T) {
	h, _ := testPause(t)

	cases := []struct{ name, body string }{
		{"sem instancia", `{}`},
		{"instancia so com espaco", `{"instancia":"   "}`},
		{"nao e JSON", `nao sou json`},
		{"corpo vazio", ``},
	}
	for _, c := range cases {
		rec := askPause(t, h, "token-do-a", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quero 400 (corpo = %s)", c.name, rec.Code, rec.Body.String())
		}
	}
}

func TestPauseRouteUnknownInstanceGives404(t *testing.T) {
	store, path := storeWithConsumer(t)
	if err := store.CreateConsumer("sistema-c", "token-do-c", []string{"clinica"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco para apagar a instancia: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM instancia WHERE slug = 'clinica'`); err != nil {
		t.Fatalf("apagar instancia clinica: %v", err)
	}

	h := NewPauseHandler(store, NewAuthenticator(store), 1<<20, AllTypes)
	rec := askPause(t, h, "token-do-c", pauseBody("clinica"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quero 404 (corpo = %s)", rec.Code, rec.Body.String())
	}
}
