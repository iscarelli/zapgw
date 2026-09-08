package inbound

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// The two tie-break instants, EXPLICIT and far apart: a test that wrote
// both sources at the same instant wouldn't distinguish "the most recent
// wins" from "the last one to write wins," which are different rules.
var (
	beforeWebhook = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	afterWebhook  = time.Date(2026, 7, 28, 12, 30, 0, 0, time.UTC)
)

// qualityHandler builds the handler with a STOPPED CLOCK, plus the
// store, so the test can place a measurement before or after the webhook.
func qualityHandler(t *testing.T, now time.Time) (http.Handler, *config.Store) {
	t.Helper()
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(consumer.Close)

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

	err = store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "app-secret-de-teste", VerifyToken: "vt", SendToken: "te",
		CallbackURL: consumer.URL, DeliverySecret: "se", TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database to activate: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE instancia SET ativo = 1 WHERE slug = ?`, "lojinha"); err != nil {
		t.Fatalf("activate instance: %v", err)
	}

	_, mux := newHandler(store, NewDeliverer(nil), 1<<20, config.NewCounter(store), config.NewTransit(store),
		func() time.Time { return now })
	return mux, store
}

// qualityPayload builds the `phone_number_quality_update` webhook with a
// DOWNGRADE — the direction that matters (TIER_1K -> TIER_50). The
// `entry[].id` is the instance's own waba_id, otherwise guard 5b rejects
// the batch and the test would prove something else.
func qualityPayload(currentLimit string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":1769000080,` +
		`"changes":[{"field":"phone_number_quality_update","value":{` +
		`"display_phone_number":"16505551111","event":"FLAGGED",` +
		`"old_limit":"TIER_1K","current_limit":"` + currentLimit + `",` +
		`"max_daily_conversations_per_business":"TIER_10K"}}]}]}`)
}

func deliverQuality(t *testing.T, h http.Handler, raw []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func numberOf(t *testing.T, s *config.Store) config.NumberAtMeta {
	t.Helper()
	n, err := s.NumberAtMeta("lojinha")
	if err != nil {
		t.Fatalf("NumberAtMeta: %v", err)
	}
	return n
}

// The webhook is the SECOND source for the tier, and it stores the
// `current_limit` LITERALLY.
//
// READS `current_limit`, NOT `max_daily_conversations_per_business`: this
// test's payload carries both with DIFFERENT values on purpose, because in
// Meta's dashboard sample they come out equal and swapping one for the
// other would pass green (docs/META-CAMPOS-DE-WEBHOOK.md records exactly
// this measurement).
func TestQualityWebhookRecordsTheLimitWithSourceWEBHOOK(t *testing.T) {
	h, store := qualityHandler(t, afterWebhook)

	deliverQuality(t, h, qualityPayload("TIER_50"))

	n := numberOf(t, store)
	if n.Limit.Value != "TIER_50" {
		t.Errorf("limit = %q, want the LITERAL TIER_50 (the `current_limit`, not `max_daily`)", n.Limit.Value)
	}
	if n.Limit.Source != config.SourceWebhook {
		t.Errorf("source = %q, want %q", n.Limit.Source, config.SourceWebhook)
	}
	if !n.Limit.ObservedAt.Equal(afterWebhook) {
		t.Errorf("observado_em = %v, want %v (OUR OWN clock, not Meta's `time`)",
			n.Limit.ObservedAt, afterWebhook)
	}
	// A webhook isn't a check: we asked nothing, it just arrived. If it
	// stamped `conferido_em`, "the measurement is healthy" would show
	// green on a gateway that lost read access to the Graph API.
	if !n.CheckedAt.IsZero() {
		t.Errorf("conferido_em = %v, want zero — a webhook is not a measurement", n.CheckedAt)
	}
	// Quality remains unobserved: this webhook carries NO rating at all
	// (it carries an `event`), and inventing one would assert a
	// translation the Meta source doesn't support.
	if n.Quality.Observed() {
		t.Errorf("quality = %+v, want unobserved", n.Quality)
	}
}

// 🔴 T-080's MANDATORY MUTATION (the second one), now through the REAL
// path: two sources, two instants. The webhook arrived AFTER the
// measurement and wins.
func TestWebhookOVERWRITESOLDERMeasurement(t *testing.T) {
	h, store := qualityHandler(t, afterWebhook)
	if err := store.UpdateNumberAtMeta("lojinha", config.NumberUpdate{
		Limit: "TIER_1K", Source: config.SourceMeasurement, When: beforeWebhook,
	}); err != nil {
		t.Fatalf("seed the measurement: %v", err)
	}

	deliverQuality(t, h, qualityPayload("TIER_50"))

	n := numberOf(t, store)
	if n.Limit.Value != "TIER_50" || n.Limit.Source != config.SourceWebhook {
		t.Errorf("limit = (%q, %q), want (TIER_50, %q) — the PUSHED downgrade notice is the "+
			"only one that arrives before sending starts failing on the limit",
			n.Limit.Value, n.Limit.Source, config.SourceWebhook)
	}
}

// And the symmetric case, through the same path: a DELAYED webhook (a Meta
// redelivery, up to 36h) does not regress a newer measurement.
func TestLATEWebhookDoesNotRegressNEWERMeasurement(t *testing.T) {
	h, store := qualityHandler(t, beforeWebhook)
	if err := store.UpdateNumberAtMeta("lojinha", config.NumberUpdate{
		Limit: "TIER_1K", Source: config.SourceMeasurement, When: afterWebhook,
	}); err != nil {
		t.Fatalf("seed the measurement: %v", err)
	}

	deliverQuality(t, h, qualityPayload("TIER_50"))

	if n := numberOf(t, store); n.Limit.Value != "TIER_1K" {
		t.Errorf("limit = %q, want TIER_1K — a delayed observation does not undo a newer one", n.Limit.Value)
	}
}

// An account webhook from ANOTHER WABA dies at guard 5b and CANNOT store
// anything — tenant isolation applies to this block the same way it
// applies to the rest. Without this assertion, the write could have been
// placed before the guard and one tenant's tier would show up on another's
// dashboard.
func TestWebhookFromANOTHERWabaDoesNotRecordTheNumber(t *testing.T) {
	h, store := qualityHandler(t, afterWebhook)

	raw := []byte(strings.Replace(string(qualityPayload("TIER_50")), `"id":"WABA1"`, `"id":"WABA-DE-OUTRO"`, 1))
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if n := numberOf(t, store); n.Limit.Observed() {
		t.Errorf("recorded another WABA's tier: %+v", n.Limit)
	}
}
