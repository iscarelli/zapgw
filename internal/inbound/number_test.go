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
		t.Fatalf("abrir banco para ativar: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE instancia SET ativo = 1 WHERE slug = ?`, "lojinha"); err != nil {
		t.Fatalf("ativar instancia: %v", err)
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
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
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
		t.Errorf("limite = %q, quero o LITERAL TIER_50 (o `current_limit`, nao o `max_daily`)", n.Limit.Value)
	}
	if n.Limit.Source != config.SourceWebhook {
		t.Errorf("fonte = %q, quero %q", n.Limit.Source, config.SourceWebhook)
	}
	if !n.Limit.ObservedAt.Equal(afterWebhook) {
		t.Errorf("observado_em = %v, quero %v (o NOSSO relogio, nao o `time` da Meta)",
			n.Limit.ObservedAt, afterWebhook)
	}
	// A webhook isn't a check: we asked nothing, it just arrived. If it
	// stamped `conferido_em`, "the measurement is healthy" would show
	// green on a gateway that lost read access to the Graph API.
	if !n.CheckedAt.IsZero() {
		t.Errorf("conferido_em = %v, quero zero — webhook nao e' medicao", n.CheckedAt)
	}
	// Quality remains unobserved: this webhook carries NO rating at all
	// (it carries an `event`), and inventing one would assert a
	// translation the Meta source doesn't support.
	if n.Quality.Observed() {
		t.Errorf("qualidade = %+v, quero nao-observada", n.Quality)
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
		t.Fatalf("semear a medicao: %v", err)
	}

	deliverQuality(t, h, qualityPayload("TIER_50"))

	n := numberOf(t, store)
	if n.Limit.Value != "TIER_50" || n.Limit.Source != config.SourceWebhook {
		t.Errorf("limite = (%q, %q), quero (TIER_50, %q) — o aviso EMPURRADO de rebaixamento e' o "+
			"unico que chega antes de o envio comecar a falhar por limite",
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
		t.Fatalf("semear a medicao: %v", err)
	}

	deliverQuality(t, h, qualityPayload("TIER_50"))

	if n := numberOf(t, store); n.Limit.Value != "TIER_1K" {
		t.Errorf("limite = %q, quero TIER_1K — observacao atrasada nao desfaz uma mais nova", n.Limit.Value)
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
		t.Errorf("gravou o tier de outra WABA: %+v", n.Limit)
	}
}
