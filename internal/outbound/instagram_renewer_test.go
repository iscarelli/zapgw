// Tests for T-098 (renewal of the Instagram long-lived token).
//
// ALL SERVERS ARE LOCAL httptest.NewServer — the SAME technique
// instagram_test.go (T-097) already uses. The gateway NEVER talks to the
// real Meta in this file (hard rule from CLAUDE.md).
package outbound

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// storeWithInstagram creates an INSTAGRAM instance "insta-loja" with the
// token set AT `setAt` — which lets each test control the token's AGE
// without waiting real days (CreateInstanceAt, not CreateInstance, is what
// gives that control).
func storeWithInstagram(t *testing.T, setAt time.Time) (*config.Store, string) {
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

	if err := s.CreateInstanceAt(config.Instance{
		Slug: "insta-loja", Type: config.TypeInstagram, IgID: "IGID1",
		AppSecret: "a", VerifyToken: "v", SendToken: "token-antigo",
		CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 2000,
	}, setAt); err != nil {
		t.Fatalf("CreateInstanceAt (instagram): %v", err)
	}
	return s, "insta-loja"
}

// renewalGraph is a fake Graph API that only understands
// GET /refresh_access_token — the ONLY path RenewInstagramToken calls.
type renewalGraph struct {
	mu       sync.Mutex
	calls    int
	newToken string
	refuse   bool
}

func (g *renewalGraph) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.calls++
		g.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if g.refuse {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"grafo-de-teste: token com menos de 24h ou invalido","code":190}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"` + g.newToken + `","token_type":"bearer","expires_in":5184000}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (g *renewalGraph) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func testRenewer(store *config.Store, srv *httptest.Server) *InstagramRenewer {
	return NewInstagramRenewer(store, meta.NewClient(srv.Client(), srv.URL), srv.URL)
}

// captureLog redirects the `log` package to a buffer and returns a
// function that restores the default destination — SAME pattern as
// handler_test.go (logStdout).
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(logStdout) })
	return &buf
}

// --- Verify (a): less than 30 days left -> renews, writes the new token and the new deadline

func TestRenewerRenewsWhenFewerThan30DaysAreLeft(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	setAt := now.Add(-31 * 24 * time.Hour) // age 31d -> 29d left, < 30
	store, slug := storeWithInstagram(t, setAt)

	g := &renewalGraph{newToken: "token-novo-da-meta"}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }

	rv.Check(context.Background())

	if n := g.count(); n != 1 {
		t.Fatalf("Meta was called %d time(s), want exactly 1", n)
	}
	inst, err := store.FindInstance(slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if inst.SendToken != "token-novo-da-meta" {
		t.Errorf("SendToken = %q, want the new token", inst.SendToken)
	}
	if want := now.Format(time.RFC3339); inst.TokenSetAt != want {
		t.Errorf("TokenSetAt = %q, want %q (deadline restarted from now)", inst.TokenSetAt, want)
	}
	r, err := store.SummarizeInstance(slug)
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if want := now.Format(time.RFC3339); r.TokenRenewedAt != want {
		t.Errorf("TokenRenewedAt = %q, want %q", r.TokenRenewedAt, want)
	}
	if failure := rv.FailingSince(slug); !failure.IsZero() {
		t.Errorf("FailingSince = %v, want zero (the renewal succeeded)", failure)
	}
}

// --- Verify (b): more than 30 days left -> does NOT call Meta

func TestRenewerDoesNotCallMetaWhenMoreThan30DaysAreLeft(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	setAt := now.Add(-10 * 24 * time.Hour) // age 10d -> 50d left
	store, slug := storeWithInstagram(t, setAt)

	g := &renewalGraph{newToken: "nao-deveria-aparecer-em-lugar-nenhum"}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }

	rv.Check(context.Background())

	if n := g.count(); n != 0 {
		t.Fatalf("Meta was called %d time(s), want 0 — 50 days are left, well above the %d threshold",
			n, DaysToRenewIGToken)
	}
	inst, err := store.FindInstance(slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if inst.SendToken != "token-antigo" {
		t.Errorf("SendToken changed to %q without Meta having been called", inst.SendToken)
	}
}

// --- Verify (c): token with less than 24h -> skipped, no call and no alarm.
//
// FIRST in ISOLATION, as a pure function — it's the ONLY way to truly
// exercise the 24h guard: with DaysToRenewIGToken = 30, every token
// with less than 24h ALSO has less than 30 days, so an end-to-end-only test
// would never prove the 24h guard exists (the 30-day one would already be
// enough for the same observable result). See the comment on
// decideIGTokenRenewal.
//
// 🔴 ALL DURATIONS IN THIS TABLE ARE HAND-WRITTEN LITERALS — none derives
// from DaysToRenewIGToken, MinAgeToRenewIGToken, or
// InstagramTokenValidity. A test that computed the expected value from the
// SAME constant it's supposed to watch proves nothing (docs/ARMADILHAS.md,
// "Testes", found in T-095: zeroing the constant would leave this test
// green).
func TestDecideIGTokenRenewal(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		want igTokenRenewalDecision
	}{
		{"2 hours: too young for Meta to accept", 2 * time.Hour, decisionTokenTooYoung},
		{"23h59: still too young (boundary, from below)", 23*time.Hour + 59*time.Minute, decisionTokenTooYoung},
		{"exactly 24h: already allowed, but not yet time", 24 * time.Hour, decisionWait},
		{"10 days: waiting", 10 * 24 * time.Hour, decisionWait},
		{"29 days and 23h: still waiting (boundary, from below)", 29*24*time.Hour + 23*time.Hour, decisionWait},
		{"exactly 30 days: renews (boundary, from above)", 30 * 24 * time.Hour, decisionRenew},
		{"59 days: renews", 59 * 24 * time.Hour, decisionRenew},
		{"exactly 60 days: expired (boundary)", 60 * 24 * time.Hour, decisionExpired},
		{"90 days: expired", 90 * 24 * time.Hour, decisionExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decideIGTokenRenewal(c.age); got != c.want {
				t.Errorf("decideIGTokenRenewal(%s) = %v, want %v", c.age, got, c.want)
			}
		})
	}
}

// AND THEN end-to-end, confirming that the real loop uses the decision:
func TestRenewerSkipsTokenYoungerThan24hWithoutCallingOrAlarming(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	setAt := now.Add(-2 * time.Hour) // freshly-born token
	store, slug := storeWithInstagram(t, setAt)

	g := &renewalGraph{newToken: "nao-deveria-aparecer"}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }
	logBuf := captureLog(t)

	rv.Check(context.Background())

	if n := g.count(); n != 0 {
		t.Fatalf("Meta was called %d time(s), want 0 — the token is only 2h old", n)
	}
	if strings.Contains(logBuf.String(), "ALARME") {
		t.Errorf("log contains ALARME with no reason:\n%s", logBuf.String())
	}
	inst, err := store.FindInstance(slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if inst.SendToken != "token-antigo" {
		t.Errorf("SendToken changed to %q", inst.SendToken)
	}
}

// --- Verify (d) 🔴: Meta returns a new token but the WRITE fails ->
// the gateway keeps the old one, does NOT mark it as renewed, and an
// ALARME goes out.
//
// THE FAILURE IS INJECTED via `rv.persist` (see the field's comment in
// instagram_renewer.go) instead of corrupting the test database: this
// way it's possible to prove, AT THE SAME TIME, that (1) Meta WAS called
// and returned a new token, and (2) that token NEVER touched the database —
// the two halves of Verify (d).
func TestRenewerWhenTheWriteFailsKeepsTheOldTokenAndDoesNotMarkRenewed(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	setAt := now.Add(-31 * 24 * time.Hour) // 29 days left
	store, slug := storeWithInstagram(t, setAt)

	g := &renewalGraph{newToken: "token-novo-da-meta"}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }

	errWrite := errors.New("simulado: disco cheio")
	var persistenceRequest bool
	rv.persist = func(slug, newToken string, now time.Time) error {
		persistenceRequest = true
		if newToken != "token-novo-da-meta" {
			t.Errorf("persist received newToken = %q, want what Meta returned", newToken)
		}
		return errWrite
	}
	logBuf := captureLog(t)

	rv.Check(context.Background())

	if !persistenceRequest {
		t.Fatal("the write was never attempted — the test would have no way to prove guarantee (d)")
	}
	if n := g.count(); n != 1 {
		t.Fatalf("Meta was called %d time(s), want exactly 1", n)
	}
	// (1) the gateway keeps the OLD TOKEN — read from the real database,
	// which rv.persist (injected) never touched.
	inst, err := store.FindInstance(slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if inst.SendToken != "token-antigo" {
		t.Errorf("SendToken = %q — the gateway should NOT have swapped the token while the write failed", inst.SendToken)
	}
	// (2) does NOT mark it as renewed: FailingSince has to be filled in,
	// not zero (zero is what clearFailure writes on SUCCESS).
	if failure := rv.FailingSince(slug); failure.IsZero() {
		t.Error("FailingSince = zero — the loop treated a failed write as a success")
	}
	// (3) an ALARME comes out.
	if !strings.Contains(logBuf.String(), "ALARME") {
		t.Fatalf("log without ALARME:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "GRAVACAO falhou") {
		t.Errorf("log does not mention the write having failed:\n%s", logBuf.String())
	}
}

// --- Verify (e): Meta refuses the renewal -> ALARME with the remaining days in the text

func TestRenewerWhenMetaRefusesAlarmsWithDaysLeftInTheText(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	setAt := now.Add(-31 * 24 * time.Hour) // 29 days left
	store, slug := storeWithInstagram(t, setAt)

	g := &renewalGraph{refuse: true}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }
	logBuf := captureLog(t)

	rv.Check(context.Background())

	if !strings.Contains(logBuf.String(), "ALARME") {
		t.Fatalf("log without ALARME:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "29 dia") {
		t.Errorf("log does not cite the remaining days (29):\n%s", logBuf.String())
	}
	inst, err := store.FindInstance(slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if inst.SendToken != "token-antigo" {
		t.Errorf("SendToken = %q — Meta refused, nothing should have changed", inst.SendToken)
	}
	if failure := rv.FailingSince(slug); failure.IsZero() {
		t.Error("FailingSince = zero — Meta's refusal should have marked the failure")
	}
}

// --- Verify (f): a tipo=whatsapp instance NEVER enters this loop

func TestRenewerNeverRunsForAWhatsappInstance(t *testing.T) {
	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "t.db")
	store, err := config.OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	// This instance's token is 31 days old — INSIDE the window in which an
	// INSTAGRAM instance would be renewed (29 days would be left, < 30).
	// It's ON PURPOSE: if the loop confused the type, this would NOT fall
	// into the "expired" branch (which only alarms and doesn't call Meta)
	// — it would fall straight into the "renew" branch, and Meta WOULD be
	// called for real. A 90-day age (which would only alarm) would let
	// this specific mutation go unnoticed.
	if err := store.CreateInstanceAt(config.Instance{
		Slug: "lojinha", WabaID: "W1", PhoneNumberID: "P1", DisplayNumber: "5532999990000",
		AppSecret: "a", VerifyToken: "v", SendToken: "token-whatsapp",
		CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 2000,
	}, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("CreateInstanceAt (whatsapp): %v", err)
	}

	g := &renewalGraph{newToken: "nao-deveria-acontecer"}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }
	logBuf := captureLog(t)

	rv.Check(context.Background())

	if n := g.count(); n != 0 {
		t.Fatalf("Meta was called %d time(s) because of a WHATSAPP instance, want 0", n)
	}
	if strings.Contains(logBuf.String(), "ALARME") {
		t.Errorf("a whatsapp instance generated an ALARME from the instagram renewer:\n%s", logBuf.String())
	}
	inst, err := store.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if inst.SendToken != "token-whatsapp" {
		t.Errorf("SendToken changed to %q", inst.SendToken)
	}
}

// SECOND LAYER of (f): calling checkOne DIRECTLY (bypassing Check's
// filter) proves the DEFENSE-IN-DEPTH guard inside it — the same discipline
// as Store.RenewInstagramTokenAt (`AND tipo = ?`). Without this test,
// removing Check's filter and keeping only checkOne's would pass
// unnoticed by TestRenewerNeverRunsForAWhatsappInstance (which only
// exercises Check).
func TestRenewerCheckOneRefusesWhatsappInstanceOutright(t *testing.T) {
	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "t.db")
	store, err := config.OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	// SAME age (31 days) and SAME reason as TestRenewerNeverRunsForAWhatsappInstance:
	// inside the "renew" window, so that the removed guard turns into a
	// real call to Meta, not just an alarm.
	if err := store.CreateInstanceAt(config.Instance{
		Slug: "lojinha", WabaID: "W1", PhoneNumberID: "P1", DisplayNumber: "5532999990000",
		AppSecret: "a", VerifyToken: "v", SendToken: "token-whatsapp",
		CallbackURL: "http://127.0.0.1:1", DeliverySecret: "s", TimeoutMs: 2000,
	}, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("CreateInstanceAt (whatsapp): %v", err)
	}

	g := &renewalGraph{newToken: "nao-deveria-acontecer"}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }

	rv.checkOne(context.Background(), "lojinha")

	if n := g.count(); n != 0 {
		t.Fatalf("checkOne called Meta %d time(s) directly for a WHATSAPP instance, want 0", n)
	}
}

// --- A PAUSED instance keeps entering the loop (decision documented on
// Check: a token's expiry is NOT reversible the way pausing is).

func TestRenewerChecksInstagramInstanceEvenWhenPaused(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	setAt := now.Add(-31 * 24 * time.Hour)
	store, slug := storeWithInstagram(t, setAt)
	// storeWithInstagram creates the instance PAUSED (CreateInstance never
	// activates — only `zapgw fumaca` activates). No need to pause it
	// explicitly.

	g := &renewalGraph{newToken: "token-novo-mesmo-pausada"}
	rv := testRenewer(store, g.server(t))
	rv.now = func() time.Time { return now }

	rv.Check(context.Background())

	if n := g.count(); n != 1 {
		t.Fatalf("Meta was called %d time(s) for the PAUSED instance, want exactly 1", n)
	}
	inst, err := store.FindInstance(slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if inst.SendToken != "token-novo-mesmo-pausada" {
		t.Errorf("SendToken = %q, want the new token — pausing cannot block renewal", inst.SendToken)
	}
}

// T-115 (3): SAME proof as TestWatchdogStartSurvivesAPanicAndContinuesOnTheNextTick
// (watchdog_test.go), now for the renewer — Start's recover lets the SECOND
// round of the loop happen after a panic in the first. `go tool cover`
// showed Start at 0% before this test.
func TestRenewerStartSurvivesAPanicAndContinuesOnTheNextTick(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	store, _ := storeWithInstagram(t, now)
	rv := NewInstagramRenewer(store, meta.NewClient(http.DefaultClient, "http://127.0.0.1:1"), "http://127.0.0.1:1")
	rv.interval = time.Millisecond

	// Start's goroutine never stops: SAME caveat as Watchdog's sibling test
	// — the channel can only be closed ONCE, on exactly the second call.
	var calls int
	secondRound := make(chan struct{})
	rv.work = func(ctx context.Context) {
		calls++
		switch calls {
		case 1:
			panic("test panic — first round")
		case 2:
			close(secondRound)
		}
	}

	rv.Start()

	select {
	case <-secondRound:
		// the second round happened — the first one's panic did NOT kill the loop.
	case <-time.After(5 * time.Second):
		t.Fatal("the loop's second round never happened after the panic — recover did not protect the goroutine")
	}
}
