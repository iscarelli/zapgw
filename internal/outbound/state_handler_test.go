package outbound

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

const testVersion = "9.9.9-teste"

// testState returns the handler, the store (for the test to write to the
// counter) and the watchdog (for the test to measure the token whenever it
// wants).
//
// It receives WHICH instances are active, not a boolean, for the same reason
// as the probe: each test needs the guard it targets to be the FIRST to speak.
func testState(t *testing.T, m *fakeHealthMeta, active ...string) (http.Handler, *config.Store, *Watchdog) {
	t.Helper()
	return testStateWithRetention(t, m, config.DefaultRetentionDays, active...)
}

// testStateWithRetention is the same handler with the series ceiling chosen
// — which in production comes from the environment, resolved once in `main`.
// It exists because the T-081 rejection has to cite the deadline IN EFFECT at
// this installation, and a test that only exercised the default of 90 would
// not distinguish "cites the deadline" from "prints a compiled constant".
func testStateWithRetention(t *testing.T, m *fakeHealthMeta, retentionDays int, active ...string) (http.Handler, *config.Store, *Watchdog) {
	t.Helper()
	store, path := storeWithConsumer(t)
	for _, slug := range active {
		activateInstance(t, path, slug)
	}
	srv := m.server(t)
	watchdog := NewWatchdog(store, meta.NewClient(srv.Client(), srv.URL))
	// nil in place of the Instagram renewer: no test in this file has an
	// Instagram instance, and BuildStateWithSeries treats nil as "no known
	// failure" (see IGRenewalFailureReader, state.go).
	return NewStateHandler(store, NewAuthenticator(store), watchdog, nil, IngressSource{}, nil, nil, testVersion, retentionDays, config.NewCounter(store), AllTypes), store, watchdog
}

func askState(t *testing.T, h http.Handler, token, slug string) *httptest.ResponseRecorder {
	t.Helper()
	return askStateWithWindow(t, h, token, slug, "")
}

// askStateWithWindow appends the RAW `?series_days=` (string, not int) on
// purpose: the consumer sends text, and "abc", "0" and "-3" are requests that
// really exist and need a named response.
//
// T-208: this general-purpose helper uses the NEW (English) spelling of
// both query parameters — `instance`/`series_days`, not `instancia`/
// `serie_dias` — on purpose. GET /v1/estado now records
// config.CounterOldNameUsed when the OLD spelling is used (it's an
// ENTRADA-QUERY point like any other, section 9.2), and this helper backs
// the vast majority of this package's state-reading tests, which have
// nothing to do with that migration. Using the OLD spelling here would
// make nearly every test in this file silently increment that counter as
// a side effect of merely reading state — exactly the kind of noise
// TestStateWithoutTrafficAnswersZerosNotError exists to catch. Tests that
// DO want to exercise the alias use the old spelling explicitly — see
// input_aliases_test.go's ENTRADA-QUERY cases.
func askStateWithWindow(t *testing.T, h http.Handler, token, slug, seriesDays string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/v1/estado"
	if slug != "" {
		target += "?instance=" + slug
	}
	if seriesDays != "" {
		if slug == "" {
			target += "?"
		} else {
			target += "&"
		}
		target += "series_days=" + url.QueryEscape(seriesDays)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// testDay is ONE series day seen from the consumer's point of view. It is
// a single type because the TWO series have exactly the same shape — and a
// second type here would keep the test green on the day only one of them
// changed.
type testDay struct {
	Day string `json:"day"` // OBSOLETE since T-070, see DayInState
	// DayUTC is the name that states the timezone, and that is why it
	// survives being copied outside the contract.
	DayUTC   string         `json:"day_utc"`
	Counters map[string]int `json:"counters"`
}

// testStateResponse is a DELIBERATE copy of the format, written from the
// consumer's point of view — if someone renames a field in respostaEstado,
// this test turns red instead of the consumer finding out in production.
type testStateResponse struct {
	Instance    string `json:"instance"`
	State       string `json:"state"`
	Paused      bool   `json:"paused"`
	Version     string `json:"version"`
	GeneratedAt string `json:"generated_at"`
	// StampsSince: the age of the INSTRUMENT, without which `last_at: null`
	// remains ambiguous (T-070).
	StampsSince string `json:"stamps_since"`
	Counters    map[string]struct {
		Today     int     `json:"hoje"`
		Last7Days int     `json:"last_7_days"`
		LastAt    *string `json:"last_at"`
	} `json:"counters"`
	Series7Days []testDay `json:"last_7_days_series"`
	// DailySeries is the REQUESTED window (T-081). It is a separate field, not
	// the one above grown, because `serie_7_dias` is a live contract for two
	// consumers.
	DailySeries []testDay `json:"daily_series"`
	MetaToken   struct {
		Verdict           string  `json:"verdict"`
		MeasuredAt        *string `json:"measured_at"`
		CheckedAt         *string `json:"checked_at"`
		CheckFailingSince *string `json:"check_failing_since"`
	} `json:"meta_token"`
	CallbackCertificate struct {
		State      string  `json:"state"`
		ExpiresAt  *string `json:"expires_at"`
		ObservedAt *string `json:"observed_at"`
	} `json:"callback_certificate"`
}

func readState(t *testing.T, rec *httptest.ResponseRecorder) testStateResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var r testStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("body does not deserialize: %v (body = %q)", err, rec.Body.String())
	}
	return r
}

// THE T-060 CENTRAL GUARANTEE: EVERY key of the closed vocabulary appears in
// the response, even zeroed.
//
// This test walks config.KeysInDisplayOrder — the vocabulary of TRUTH,
// not a list written in this file (which would be the second copy that T-039
// paid to eliminate). And it is the automated half of the task's MANDATORY
// MUTATION: adding a new key to the vocabulary makes the test require it in
// the response WITHOUT anyone touching state_handler.go.
func TestStateShowsEveryKeyOfTheVocabulary(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha")

	now := time.Now()
	// Each key a DIFFERENT number of times (its position in the list + 1), so
	// that no read can "get it right by coincidence" reading another key.
	for i, key := range config.KeysInDisplayOrder {
		for n := 0; n <= i; n++ {
			if err := store.IncrementCounter("lojinha", key, now); err != nil {
				t.Fatalf("IncrementCounter(%q): %v", key, err)
			}
		}
	}

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if len(r.Counters) != len(config.KeysInDisplayOrder) {
		t.Errorf("the response has %d keys, the vocabulary has %d",
			len(r.Counters), len(config.KeysInDisplayOrder))
	}
	for i, key := range config.KeysInDisplayOrder {
		c, has := r.Counters[key]
		if !has {
			t.Errorf("key %q of the vocabulary did NOT appear in the response", key)
			continue
		}
		if c.Today != i+1 || c.Last7Days != i+1 {
			t.Errorf("key %q = (hoje=%d, 7dias=%d), want (%d, %d)", key, c.Today, c.Last7Days, i+1, i+1)
		}
	}
}

// An instance with no traffic at all answers 200 with zeros — never an error
// and never an empty response. It is the normal case of a freshly provisioned
// instance, and a panel that got 404 there would conclude the instance does
// not exist.
func TestStateWithoutTrafficAnswersZerosNotError(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	for _, key := range config.KeysInDisplayOrder {
		c := r.Counters[key]
		if c.Today != 0 || c.Last7Days != 0 {
			t.Errorf("key %q = (hoje=%d, 7dias=%d), want (0,0)", key, c.Today, c.Last7Days)
		}
		if c.LastAt != nil {
			t.Errorf("key %q has timestamp %q without it ever having happened — `null` is the right answer",
				key, *c.LastAt)
		}
	}
}

// THE TIMESTAMP is the route's most valuable item: a stalled counter is
// ambiguous between "it failed" and "nobody wrote", and only age tells the
// two apart.
func TestStateStampsTheLastEventPerKey(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha")

	when := time.Now().UTC().Truncate(time.Second)
	if err := store.IncrementCounter("lojinha", config.CounterReceived, when); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	c := r.Counters[config.CounterReceived]
	if c.LastAt == nil {
		t.Fatalf("received with no timestamp after a recorded event")
	}
	if want := when.Format(time.RFC3339); *c.LastAt != want {
		t.Errorf("last_at = %q, want %q", *c.LastAt, want)
	}
	// And the NEIGHBORING key remains null: a timestamp that leaked into every
	// key would answer "yes" to any question and would be useless for alarming.
	if v := r.Counters[config.CounterSent].LastAt; v != nil {
		t.Errorf("sent gained timestamp %q without it ever having happened", *v)
	}
}

// The daily series ALWAYS has 7 entries, in order, with the empty days
// present and zeroed: a variable-size series would make the consumer's chart
// change shape depending on traffic, and a zeroed day would disappear instead
// of showing up as zero.
func TestStateHasSevenDaySeriesWithEmptyDaysZeroed(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha")

	now := time.Now().UTC()
	if err := store.IncrementCounter("lojinha", config.CounterReceived, now.AddDate(0, 0, -3)); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if len(r.Series7Days) != 7 {
		t.Fatalf("last_7_days_series has %d entries, want 7", len(r.Series7Days))
	}
	// From OLDEST to newest, no gap: consecutive days.
	for i, d := range r.Series7Days {
		want := now.AddDate(0, 0, i-6).Format("2006-01-02")
		if d.Day != want {
			t.Errorf("last_7_days_series[%d].day = %q, want %q (oldest to newest)", i, d.Day, want)
		}
		// EVERY key of the vocabulary on EVERY day — the same single source.
		for _, key := range config.KeysInDisplayOrder {
			if _, has := d.Counters[key]; !has {
				t.Errorf("last_7_days_series[%d] (%s) does not have the vocabulary key %q", i, d.Day, key)
			}
		}
	}
	// The event from 3 days ago is on its own day, and only there.
	eventDay := now.AddDate(0, 0, -3).Format("2006-01-02")
	var sum int
	for _, d := range r.Series7Days {
		n := d.Counters[config.CounterReceived]
		sum += n
		if d.Day == eventDay && n != 1 {
			t.Errorf("day %s: received = %d, want 1", d.Day, n)
		}
		if d.Day != eventDay && n != 0 {
			t.Errorf("day %s: received = %d, want 0", d.Day, n)
		}
	}
	// The series SUMS to the total: if the two counts diverge, one of the two
	// is lying and the consumer has no way to know which.
	if want := r.Counters[config.CounterReceived].Last7Days; sum != want {
		t.Errorf("sum of the series = %d, last_7_days = %d — both counts have to match", sum, want)
	}
}

// `pausada` exists because a paused instance answers 503, volume goes to
// zero and the timestamp ages — INDISTINGUISHABLE from "nobody wrote". Without
// this field the consumer's alarm would accuse an outage when the cause is a
// deliberate pause.
func TestStateSaysWhetherTheInstanceIsPaused(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m) // none activated: instance is born paused

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if !r.Paused {
		t.Errorf("paused = false on an instance that was never activated")
	}
	// The word is the SAME as `zapgw estado` and `zapgw instancia listar`
	// (config.StateOf): two spellings would make a `grep pausada` lie.
	if r.State != "pausada" {
		t.Errorf("state = %q, want %q", r.State, "pausada")
	}

	hActive, _, _ := testState(t, m, "lojinha")
	rActive := readState(t, askState(t, hActive, "token-do-a", "lojinha"))
	if rActive.Paused || rActive.State != "ativa" {
		t.Errorf("ACTIVE instance answered (paused=%v, state=%q)", rActive.Paused, rActive.State)
	}
}

func TestStateBringsTheBinaryVersion(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	if r := readState(t, askState(t, h, "token-do-a", "lojinha")); r.Version != testVersion {
		t.Errorf("version = %q, want %q", r.Version, testVersion)
	}
}

// THE READ NEVER TALKS TO META. The consumer paints the panel at their own
// frequency; the one measuring is the timer, at ours. A panel that called
// Meta on every load would be hostage to its latency and uptime, and "Meta is
// down" would turn into "gateway has a problem" on the consumer's screen.
func TestStateAnswersFromCacheWithoutCallingMeta(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, watchdog := testState(t, m, "lojinha")

	// Before any tick: `desconhecido`, and ZERO calls.
	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if r.MetaToken.Verdict != VerdictUnknown {
		t.Errorf("verdict without measurement = %q, want %q", r.MetaToken.Verdict, VerdictUnknown)
	}
	if n := m.gets.Load(); n != 0 {
		t.Fatalf("the read talked to Meta %d time(s) — it has to answer ONLY from cache", n)
	}

	// The timer measures once; ten reads afterward still call no one.
	watchdog.Check(context.Background())
	for range 10 {
		if r := readState(t, askState(t, h, "token-do-a", "lojinha")); r.MetaToken.Verdict != VerdictOK {
			t.Fatalf("verdict = %q, want %q after a watchdog tick", r.MetaToken.Verdict, VerdictOK)
		}
	}
	if n := m.gets.Load(); n != 1 {
		t.Errorf("Graph API calls = %d, want 1 (ONLY the watchdog's tick; the read never calls)", n)
	}
}

// THE SAME guard as POST /v1/messages: system A's token does not read system
// B's business traffic. Message volume, when the last one arrived, and
// whether the credential is rejected describe the tenant's business.
//
// "clinica" stays ACTIVE on purpose: if it were paused, this test would go
// green with the link guard turned off — and the 403 would never have been
// proven.
func TestStateRefusesInstanceNotOwnedByConsumer(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha", "clinica")
	if err := store.IncrementCounter("clinica", config.CounterReceived, time.Now()); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	rec := askState(t, h, "token-do-a", "clinica")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	// And the rejection cannot leak ANYTHING about someone else's instance.
	if body := rec.Body.String(); len(body) > 0 {
		var e errorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("error body does not deserialize: %v (body = %q)", err, body)
		}
		if e.Error.Class != "config" {
			t.Errorf("class = %q, want \"config\" (the same as sending)", e.Error.Class)
		}
	}
}

func TestStateRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	if rec := askState(t, h, "", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	if rec := askState(t, h, "token-errado", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", rec.Code)
	}
}

// Without the `instancia` parameter the response is 400, not the 403 that
// CanUse("") would give for free: "you cannot see this instance" would send
// the consumer to check their own link, which is correct — and the defect
// would stay hidden in the wrong place.
func TestStateWithoutInstanceParameterAnswers400(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	rec := askState(t, h, "token-do-a", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

// --- T-081: the series window -------------------------------------------------

// THE REQUEST THAT ORIGINATED THE TASK: 30 days. The consumer's cost panel
// chart was fed by the WABA's `analytics`, directly on the Graph, and the
// owner's rule closed that path — this route is the replacement.
//
// The test writes an event at 25 days, which is OUTSIDE the 7-day window and
// INSIDE the 30-day one: it is what distinguishes "the series got bigger" from
// "the series got bigger and brings the data only it reaches".
func TestStateDeliversTheRequestedThirtyDayWindow(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha")

	now := time.Now().UTC()
	if err := store.IncrementCounter("lojinha", config.CounterSent, now.AddDate(0, 0, -25)); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	r := readState(t, askStateWithWindow(t, h, "token-do-a", "lojinha", "30"))
	if len(r.DailySeries) != 30 {
		t.Fatalf("daily_series has %d entries, want 30", len(r.DailySeries))
	}
	// From OLDEST to newest, no gap — the same shape as the short series.
	for i, d := range r.DailySeries {
		want := now.AddDate(0, 0, i-29).Format("2006-01-02")
		if d.DayUTC != want {
			t.Fatalf("daily_series[%d].day_utc = %q, want %q", i, d.DayUTC, want)
		}
		if d.Day != d.DayUTC {
			t.Errorf("daily_series[%d]: day = %q and day_utc = %q — both are the SAME data", i, d.Day, d.DayUTC)
		}
		for _, key := range config.KeysInDisplayOrder {
			if _, has := d.Counters[key]; !has {
				t.Errorf("daily_series[%d] (%s) does not have the vocabulary key %q", i, d.DayUTC, key)
			}
		}
	}
	// THE DATA FROM 25 DAYS AGO IS THERE — and this is the finding from step
	// (1) of the task: it never needed new storage, only the route to allow the
	// request.
	eventDay := now.AddDate(0, 0, -25).Format("2006-01-02")
	var sum int
	for _, d := range r.DailySeries {
		n := d.Counters[config.CounterSent]
		sum += n
		if d.DayUTC == eventDay && n != 1 {
			t.Errorf("day %s: sent = %d, want 1", d.DayUTC, n)
		}
	}
	if sum != 1 {
		t.Errorf("sent in the 30-day series = %d, want 1", sum)
	}
	// And it remains OUTSIDE the 7-day window, which did not change meaning.
	if v := r.Counters[config.CounterSent].Last7Days; v != 0 {
		t.Errorf("last_7_days = %d for an event from 25 days ago — the short window cannot have grown along with it", v)
	}
}

// 🔴 `serie_7_dias` DOES NOT CHANGE SHAPE when the requested window is
// different — and it is the exact SUFFIX of the long series, day by day and
// number by number.
//
// THE ASSERTION IS ABOUT THE TWO TOGETHER, not just about the size: two
// series read in separate queries could disagree (midnight falling between
// them is enough) and the consumer would see two charts counting the same day
// in different ways — with nothing flagging which one is right.
func TestSeries7DaysStillHas7EntriesAndIsTheSuffixOfTheDailySeries(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha")

	now := time.Now().UTC()
	for _, ago := range []int{0, 3, 12, 25} {
		if err := store.IncrementCounter("lojinha", config.CounterReceived, now.AddDate(0, 0, -ago)); err != nil {
			t.Fatalf("IncrementCounter(-%dd): %v", ago, err)
		}
	}

	r := readState(t, askStateWithWindow(t, h, "token-do-a", "lojinha", "30"))
	if len(r.Series7Days) != config.ShortSeriesDays {
		t.Fatalf("last_7_days_series has %d entries with serie_dias=30, want %d — it is live contract for two consumers",
			len(r.Series7Days), config.ShortSeriesDays)
	}
	suffix := r.DailySeries[len(r.DailySeries)-config.ShortSeriesDays:]
	for i, d := range r.Series7Days {
		if d.DayUTC != suffix[i].DayUTC {
			t.Fatalf("last_7_days_series[%d] is day %q, but the daily_series suffix is %q — both come from the SAME read",
				i, d.DayUTC, suffix[i].DayUTC)
		}
		for _, key := range config.KeysInDisplayOrder {
			if d.Counters[key] != suffix[i].Counters[key] {
				t.Errorf("day %s, key %q: last_7_days_series says %d and daily_series says %d — two counts of the same day",
					d.DayUTC, key, d.Counters[key], suffix[i].Counters[key])
			}
		}
	}
	// And the sum of the 7 still matches `ultimos_7_dias`, which is the
	// contract's promise that a new window could have broken without anyone
	// noticing.
	var sum int
	for _, d := range r.Series7Days {
		sum += d.Counters[config.CounterReceived]
	}
	if want := r.Counters[config.CounterReceived].Last7Days; sum != want {
		t.Errorf("sum of last_7_days_series = %d, last_7_days = %d — both counts have to match", sum, want)
	}
}

// Without `?serie_dias=`, the response is the usual one: 7 entries in BOTH
// series. The consumer that existed before T-081 cannot receive thirteen
// times more data because of a default that changed under them.
func TestStateWithoutRequestedWindowDeliversSevenDaysInBothSeries(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if len(r.Series7Days) != config.ShortSeriesDays || len(r.DailySeries) != config.ShortSeriesDays {
		t.Fatalf("missing serie_dias: last_7_days_series has %d and daily_series has %d entries, want %d in both",
			len(r.Series7Days), len(r.DailySeries), config.ShortSeriesDays)
	}
}

// 🔴 T-081's MANDATORY MUTATION, and the assertion is about the WARNING, not
// about the size: a window larger than the retention is REJECTED, and the
// rejection cites the deadline IN EFFECT at this installation.
//
// Returning the shortened series (or 120 entries with 30 days of zero) would
// be the same defect as the truncated templates catalog, which this project
// treats as an error and never as `200`: the days the purge already erased
// would come back zeroed, indistinguishable from "there was no traffic",
// exactly in the part of the chart nobody checks.
//
// BOTH RETENTIONS ARE EXERCISED on purpose. With only the default of 90, the
// test would not distinguish "cites the deadline in effect" from "prints a
// compiled constant" — and an installation with
// `ZAPGW_TTL_CONTADORES_DIAS=15` would receive a message lying about its own
// database.
func TestStateRefusesWindowLargerThanRetentionSayingTheTermInForce(t *testing.T) {
	m := tokenAcceptingMeta()

	for _, c := range []struct{ retention, request int }{
		{config.DefaultRetentionDays, config.DefaultRetentionDays + 1},
		{15, 30}, // installation that shortened retention: 30 days no longer fit
	} {
		h, _, _ := testStateWithRetention(t, m, c.retention, "lojinha")
		rec := askStateWithWindow(t, h, "token-do-a", "lojinha", strconv.Itoa(c.request))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("retention=%d, serie_dias=%d: status = %d, want 400 — a silently short series is worse than an error; body = %s",
				c.retention, c.request, rec.Code, rec.Body.String())
		}
		var e errorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("error body does not deserialize: %v (body = %q)", err, rec.Body.String())
		}
		if e.Error.Class != "permanent" {
			t.Errorf("class = %q, want \"permanent\" — retrying the same request will not work", e.Error.Class)
		}
		if !strings.Contains(e.Error.Message, strconv.Itoa(c.retention)) {
			t.Errorf("the message does not say the deadline in effect (%d days): %q — without the number, whoever reads it does not know what to ask for",
				c.retention, e.Error.Message)
		}
	}
}

// A window that is not a number, zero or negative also gets a named `400` —
// and not a silent default. A `serie_dias=abc` that turned into 7 days would
// deliver a one-week chart to whoever asked for something else, and the
// consumer would read their own error as our data.
func TestStateRefusesWindowThatIsNotANumberOfDays(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	for _, raw := range []string{"abc", "0", "-3", "7,5", "30d"} {
		rec := askStateWithWindow(t, h, "token-do-a", "lojinha", raw)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("serie_dias=%q: status = %d, want 400; body = %s", raw, rec.Code, rec.Body.String())
		}
	}
}

// The window rejection comes AFTER the link guard: the message cites the
// installation's retention deadline, and whoever cannot even read the
// instance cannot learn anything about it through a parameter error.
func TestStateChecksTheBindingBeforeTheWindow(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testStateWithRetention(t, m, config.DefaultRetentionDays, "lojinha", "clinica")

	rec := askStateWithWindow(t, h, "token-do-a", "clinica", "9999")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (the link decides before the parameter); body = %s", rec.Code, rec.Body.String())
	}
}

// --- T-064: the consumer's certificate validity --------------------------

// "NEVER OBSERVED" IS A NAMED STATE, and this test is the reason it exists:
// an instance that never delivered has no observed certificate, and a
// consumer that only got `expira_em: null` could read it as "expired" and
// alarm on everything from day one. That is exactly what T-060 paid for with
// `ultimo_em: null` — it works from then on, and starts with a false
// positive.
func TestStateWithoutDeliverySaysNeverObservedAndInventsNoDate(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if r.CallbackCertificate.State != CertNeverObserved {
		t.Errorf("state = %q, want %q", r.CallbackCertificate.State, CertNeverObserved)
	}
	if v := r.CallbackCertificate.ExpiresAt; v != nil {
		t.Errorf("expires_at = %q on an instance that never delivered — a made-up date is worse than an empty field", *v)
	}
	if v := r.CallbackCertificate.ObservedAt; v != nil {
		t.Errorf("observed_at = %q without any observation having happened", *v)
	}
}

// With observation, BOTH timestamps appear — and the state word changes. The
// date alone does not say whether it is information from now or from three
// weeks ago.
func TestStatePublishesBothCertificateStamps(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha")

	expires := time.Now().Add(45 * 24 * time.Hour).UTC().Truncate(time.Second)
	observed := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	if err := store.RecordCallbackCertificate("lojinha", expires, observed); err != nil {
		t.Fatalf("RecordCallbackCertificate: %v", err)
	}

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if r.CallbackCertificate.State != CertObserved {
		t.Fatalf("state = %q, want %q", r.CallbackCertificate.State, CertObserved)
	}
	if r.CallbackCertificate.ExpiresAt == nil || r.CallbackCertificate.ObservedAt == nil {
		t.Fatalf("state %q with a null timestamp: %+v — the two travel together or are worthless",
			CertObserved, r.CallbackCertificate)
	}
	if want := expires.Format(time.RFC3339); *r.CallbackCertificate.ExpiresAt != want {
		t.Errorf("expires_at = %q, want %q", *r.CallbackCertificate.ExpiresAt, want)
	}
	if want := observed.Format(time.RFC3339); *r.CallbackCertificate.ObservedAt != want {
		t.Errorf("observed_at = %q, want %q — the instant of DELIVERY, not the current one",
			*r.CallbackCertificate.ObservedAt, want)
	}
	// And `gerado_em` must NOT have turned into the observation timestamp: a
	// certificate seen two hours ago would stay eternally "just observed" and
	// the field would stop aging, which is the one useful thing it does.
	if *r.CallbackCertificate.ObservedAt == r.GeneratedAt {
		t.Errorf("observed_at = generated_at (%q) — the timestamp is being read off the response's clock", r.GeneratedAt)
	}
}

// The observation is PER INSTANCE. One that leaked between slugs would make
// consumer B's alarm look at consumer A's certificate.
func TestStateDoesNotMixTheCertificateOfAnotherInstance(t *testing.T) {
	m := tokenAcceptingMeta()
	h, store, _ := testState(t, m, "lojinha", "clinica")
	if err := store.RecordCallbackCertificate("clinica",
		time.Now().Add(24*time.Hour), time.Now()); err != nil {
		t.Fatalf("RecordCallbackCertificate: %v", err)
	}

	r := readState(t, askState(t, h, "token-do-a", "lojinha"))
	if r.CallbackCertificate.State != CertNeverObserved {
		t.Errorf("lojinha answered %q; the recorded observation was clinica's (%+v)",
			r.CallbackCertificate.State, r.CallbackCertificate)
	}
}

// Every handler in this project serves each request in a goroutine over the
// SAME handler; without a concurrent test, -race is theater
// (docs/ARMADILHAS.md, "Go / concurrency"). Here the watchdog WRITES at the same
// time, which is exactly what happens in production when the timer ticks with
// a panel open.
func TestStateConcurrentWithTheWatchdogMeasuring(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, watchdog := testState(t, m, "lojinha")

	var wg sync.WaitGroup
	codes := make([]int, 30)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = askState(t, h, "token-do-a", "lojinha").Code
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		watchdog.Check(context.Background())
	}()
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("read %d: status = %d, want 200", i, c)
		}
	}
}
