package outbound

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	_ "modernc.org/sqlite" // driver for activateInstance — test only
)

const testCipherKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// testDisplayNumbers is the SINGLE SOURCE of the DisplayNumber for each
// test instance: storeWithConsumer registers by it, and the health probe
// test checks against it. Numbers are DISTINCT on purpose — two slugs with
// the same number would mask a probe answering for the wrong instance.
var testDisplayNumbers = map[string]string{
	"lojinha": "5532999990001",
	"clinica": "5532999990002",
}

// Returns the store AND the file path: Task 8 needs the path to activate the
// instance via direct SQL (the store does not expose activation — that
// belongs to the panel, in plan 4).
func storeWithConsumer(t *testing.T) (*config.Store, string) {
	t.Helper()
	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := config.OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for slug, number := range testDisplayNumbers {
		err := s.CreateInstance(config.Instance{
			Slug: slug, WabaID: "W-" + slug, PhoneNumberID: "P-" + slug,
			DisplayNumber: number,
			AppSecret:     "a", VerifyToken: "v", SendToken: "t-" + slug,
			CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 2000,
		})
		if err != nil {
			t.Fatalf("CreateInstance %q: %v", slug, err)
		}
	}
	if err := s.CreateConsumer("sistema-a", "token-do-a", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	return s, path
}

// activateInstance turns the instance on via direct SQL.
//
// Real activation comes from the panel (plan 4); the store does not expose it
// on purpose, because an instance is BORN PAUSED and only a smoke test should
// activate it. Here, in test, touching the column directly is acceptable — and
// it keeps the dependency visible instead of hidden behind a convenience
// method that someone would use in production.
func activateInstance(t *testing.T, path, slug string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco para ativar: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`UPDATE instancia SET ativo = 1 WHERE slug = ?`, slug); err != nil {
		t.Fatalf("ativar instancia %q: %v", slug, err)
	}
}

func TestTokenFromHeaderAcceptsBearer(t *testing.T) {
	cases := []struct{ header, want string }{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", "abc123"}, // Meta and clients vary the case
		{"BEARER abc123", "abc123"},
		{"Bearer  abc123 ", "abc123"}, // extra space does not change the token
		{"abc123", ""},                // without the scheme, it doesn't count
		{"Basic abc123", ""},
		{"", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
	}

	for _, c := range cases {
		if got := TokenFromHeader(c.header); got != c.want {
			t.Errorf("TokenFromHeader(%q) = %q, quero %q", c.header, got, c.want)
		}
	}
}

func TestAuthenticateAcceptsValidToken(t *testing.T) {
	store, _ := storeWithConsumer(t)
	a := NewAuthenticator(store)

	c, err := a.Authenticate("Bearer token-do-a")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if c.Name != "sistema-a" {
		t.Errorf("Name = %q", c.Name)
	}
}

func TestAuthenticateRefusesMissingAndInvalidToken(t *testing.T) {
	store, _ := storeWithConsumer(t)
	a := NewAuthenticator(store)

	if _, err := a.Authenticate(""); !errors.Is(err, ErrNoToken) {
		t.Errorf("header vazio: erro = %v, quero ErrNoToken", err)
	}
	if _, err := a.Authenticate("Bearer token-errado"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("token errado: erro = %v, quero ErrInvalidToken", err)
	}
}

// Project REQUIREMENT 3, turned into a test: sending on behalf of N
// businesses without confusing one with another. A token leaked from system A
// cannot send a message through system B's number — and the link is CHECKED,
// never assumed.
func TestCanUseBindsTheConsumerToHisOwnInstances(t *testing.T) {
	c := config.Consumer{Name: "sistema-a", Instances: []string{"lojinha"}}

	if !CanUse(c, "lojinha") {
		t.Error("o consumidor nao pode usar a propria instancia")
	}
	if CanUse(c, "clinica") {
		t.Fatal("o consumidor usou a instancia de OUTRO sistema")
	}
	if CanUse(c, "") {
		t.Error("slug vazio foi aceito")
	}
	if CanUse(config.Consumer{Name: "x"}, "lojinha") {
		t.Error("consumidor SEM instancia nenhuma usou uma instancia")
	}
}

func TestCanUseDoesNotMatchByPrefix(t *testing.T) {
	// PITFALL: comparing with strings.HasPrefix or Contains would let "lojinha"
	// authorize "lojinha-teste" — and slug is the instance's identity.
	c := config.Consumer{Name: "a", Instances: []string{"lojinha"}}

	for _, slug := range []string{"lojinha-teste", "lojinhaX", "loj", "LOJINHA"} {
		if CanUse(c, slug) {
			t.Errorf("slug %q foi aceito por parecido com lojinha", slug)
		}
	}
}
