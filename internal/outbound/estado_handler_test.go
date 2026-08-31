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
	// failure" (see IGRenewalFailureReader, estado.go).
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
// entrada_apelidos_test.go's ENTRADA-QUERY cases.
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
	Day string `json:"dia"` // OBSOLETE since T-070, see DayInState
	// DayUTC is the name that states the timezone, and that is why it
	// survives being copied outside the contract.
	DayUTC   string         `json:"dia_utc"`
	Counters map[string]int `json:"contadores"`
}

// testStateResponse is a DELIBERATE copy of the format, written from the
// consumer's point of view — if someone renames a field in respostaEstado,
// this test turns red instead of the consumer finding out in production.
type testStateResponse struct {
	Instance    string `json:"instancia"`
	State       string `json:"estado"`
	Paused      bool   `json:"pausada"`
	Version     string `json:"versao"`
	GeneratedAt string `json:"gerado_em"`
	// StampsSince: the age of the INSTRUMENT, without which `ultimo_em: null`
	// remains ambiguous (T-070).
	StampsSince string `json:"carimbos_desde"`
	Counters    map[string]struct {
		Today     int     `json:"hoje"`
		Last7Days int     `json:"ultimos_7_dias"`
		LastAt    *string `json:"ultimo_em"`
	} `json:"contadores"`
	Series7Days []testDay `json:"serie_7_dias"`
	// DailySeries is the REQUESTED window (T-081). It is a separate field, not
	// the one above grown, because `serie_7_dias` is a live contract for two
	// consumers.
	DailySeries []testDay `json:"serie_diaria"`
	MetaToken   struct {
		Verdict           string  `json:"veredito"`
		MeasuredAt        *string `json:"medido_em"`
		CheckedAt         *string `json:"conferido_em"`
		CheckFailingSince *string `json:"checagem_falhando_desde"`
	} `json:"token_meta"`
	CallbackCertificate struct {
		State      string  `json:"estado"`
		ExpiresAt  *string `json:"expira_em"`
		ObservedAt *string `json:"observado_em"`
	} `json:"certificado_do_callback"`
}

func readState(t *testing.T, rec *httptest.ResponseRecorder) testStateResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	var r testStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("corpo nao desserializa: %v (corpo = %q)", err, rec.Body.String())
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
// the response WITHOUT anyone touching estado_handler.go.
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
		t.Errorf("a resposta tem %d chaves, o vocabulario tem %d",
			len(r.Counters), len(config.KeysInDisplayOrder))
	}
	for i, key := range config.KeysInDisplayOrder {
		c, has := r.Counters[key]
		if !has {
			t.Errorf("chave %q do vocabulario NAO apareceu na resposta", key)
			continue
		}
		if c.Today != i+1 || c.Last7Days != i+1 {
			t.Errorf("chave %q = (hoje=%d, 7dias=%d), quero (%d, %d)", key, c.Today, c.Last7Days, i+1, i+1)
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
			t.Errorf("chave %q = (hoje=%d, 7dias=%d), quero (0,0)", key, c.Today, c.Last7Days)
		}
		if c.LastAt != nil {
			t.Errorf("chave %q tem carimbo %q sem nunca ter acontecido — `null` e a resposta certa",
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
		t.Fatalf("recebidas sem carimbo depois de um evento gravado")
	}
	if want := when.Format(time.RFC3339); *c.LastAt != want {
		t.Errorf("ultimo_em = %q, quero %q", *c.LastAt, want)
	}
	// And the NEIGHBORING key remains null: a timestamp that leaked into every
	// key would answer "yes" to any question and would be useless for alarming.
	if v := r.Counters[config.CounterSent].LastAt; v != nil {
		t.Errorf("enviadas ganhou carimbo %q sem nunca ter acontecido", *v)
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
		t.Fatalf("serie_7_dias tem %d entradas, quero 7", len(r.Series7Days))
	}
	// From OLDEST to newest, no gap: consecutive days.
	for i, d := range r.Series7Days {
		want := now.AddDate(0, 0, i-6).Format("2006-01-02")
		if d.Day != want {
			t.Errorf("serie_7_dias[%d].dia = %q, quero %q (do mais velho para o mais novo)", i, d.Day, want)
		}
		// EVERY key of the vocabulary on EVERY day — the same single source.
		for _, key := range config.KeysInDisplayOrder {
			if _, has := d.Counters[key]; !has {
				t.Errorf("serie_7_dias[%d] (%s) nao tem a chave %q do vocabulario", i, d.Day, key)
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
			t.Errorf("dia %s: recebidas = %d, quero 1", d.Day, n)
		}
		if d.Day != eventDay && n != 0 {
			t.Errorf("dia %s: recebidas = %d, quero 0", d.Day, n)
		}
	}
	// The series SUMS to the total: if the two counts diverge, one of the two
	// is lying and the consumer has no way to know which.
	if want := r.Counters[config.CounterReceived].Last7Days; sum != want {
		t.Errorf("soma da serie = %d, ultimos_7_dias = %d — as duas contas tem de bater", sum, want)
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
		t.Errorf("pausada = false numa instancia que nunca foi ativada")
	}
	// The word is the SAME as `zapgw estado` and `zapgw instancia listar`
	// (config.StateOf): two spellings would make a `grep pausada` lie.
	if r.State != "pausada" {
		t.Errorf("estado = %q, quero %q", r.State, "pausada")
	}

	hActive, _, _ := testState(t, m, "lojinha")
	rActive := readState(t, askState(t, hActive, "token-do-a", "lojinha"))
	if rActive.Paused || rActive.State != "ativa" {
		t.Errorf("instancia ATIVA respondeu (pausada=%v, estado=%q)", rActive.Paused, rActive.State)
	}
}

func TestStateBringsTheBinaryVersion(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	if r := readState(t, askState(t, h, "token-do-a", "lojinha")); r.Version != testVersion {
		t.Errorf("versao = %q, quero %q", r.Version, testVersion)
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
		t.Errorf("veredito sem medicao = %q, quero %q", r.MetaToken.Verdict, VerdictUnknown)
	}
	if n := m.gets.Load(); n != 0 {
		t.Fatalf("a leitura falou %d vez(es) com a Meta — ela tem de responder SO do cache", n)
	}

	// The timer measures once; ten reads afterward still call no one.
	watchdog.Check(context.Background())
	for range 10 {
		if r := readState(t, askState(t, h, "token-do-a", "lojinha")); r.MetaToken.Verdict != VerdictOK {
			t.Fatalf("veredito = %q, quero %q depois de um tique da vigia", r.MetaToken.Verdict, VerdictOK)
		}
	}
	if n := m.gets.Load(); n != 1 {
		t.Errorf("chamadas a Graph API = %d, quero 1 (SO o tique da vigia; a leitura nunca chama)", n)
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
		t.Fatalf("status = %d, quero 403; corpo = %s", rec.Code, rec.Body.String())
	}
	// And the rejection cannot leak ANYTHING about someone else's instance.
	if body := rec.Body.String(); len(body) > 0 {
		var e errorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("corpo de erro nao desserializa: %v (corpo = %q)", err, body)
		}
		if e.Error.Class != "config" {
			t.Errorf("classe = %q, quero \"config\" (a mesma do envio)", e.Error.Class)
		}
	}
}

func TestStateRefusesWithoutTokenAndWithInvalidToken(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	if rec := askState(t, h, "", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401", rec.Code)
	}
	if rec := askState(t, h, "token-errado", "lojinha"); rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status = %d, quero 401", rec.Code)
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
		t.Fatalf("status = %d, quero 400; corpo = %s", rec.Code, rec.Body.String())
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
		t.Fatalf("serie_diaria tem %d entradas, quero 30", len(r.DailySeries))
	}
	// From OLDEST to newest, no gap — the same shape as the short series.
	for i, d := range r.DailySeries {
		want := now.AddDate(0, 0, i-29).Format("2006-01-02")
		if d.DayUTC != want {
			t.Fatalf("serie_diaria[%d].dia_utc = %q, quero %q", i, d.DayUTC, want)
		}
		if d.Day != d.DayUTC {
			t.Errorf("serie_diaria[%d]: dia = %q e dia_utc = %q — os dois sao o MESMO dado", i, d.Day, d.DayUTC)
		}
		for _, key := range config.KeysInDisplayOrder {
			if _, has := d.Counters[key]; !has {
				t.Errorf("serie_diaria[%d] (%s) nao tem a chave %q do vocabulario", i, d.DayUTC, key)
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
			t.Errorf("dia %s: enviadas = %d, quero 1", d.DayUTC, n)
		}
	}
	if sum != 1 {
		t.Errorf("enviadas na serie de 30 dias = %d, quero 1", sum)
	}
	// And it remains OUTSIDE the 7-day window, which did not change meaning.
	if v := r.Counters[config.CounterSent].Last7Days; v != 0 {
		t.Errorf("ultimos_7_dias = %d para um evento de 25 dias atras — a janela curta nao pode ter crescido junto", v)
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
		t.Fatalf("serie_7_dias tem %d entradas com serie_dias=30, quero %d — ela e contrato vivo de dois consumidores",
			len(r.Series7Days), config.ShortSeriesDays)
	}
	suffix := r.DailySeries[len(r.DailySeries)-config.ShortSeriesDays:]
	for i, d := range r.Series7Days {
		if d.DayUTC != suffix[i].DayUTC {
			t.Fatalf("serie_7_dias[%d] e o dia %q, mas o sufixo da serie_diaria e %q — as duas saem da MESMA leitura",
				i, d.DayUTC, suffix[i].DayUTC)
		}
		for _, key := range config.KeysInDisplayOrder {
			if d.Counters[key] != suffix[i].Counters[key] {
				t.Errorf("dia %s, chave %q: serie_7_dias diz %d e serie_diaria diz %d — duas contas do mesmo dia",
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
		t.Errorf("soma da serie_7_dias = %d, ultimos_7_dias = %d — as duas contas tem de bater", sum, want)
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
		t.Fatalf("sem serie_dias: serie_7_dias tem %d e serie_diaria tem %d entradas, quero %d nas duas",
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
			t.Fatalf("retencao=%d, serie_dias=%d: status = %d, quero 400 — serie curta em silencio e pior que erro; corpo = %s",
				c.retention, c.request, rec.Code, rec.Body.String())
		}
		var e errorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("corpo de erro nao desserializa: %v (corpo = %q)", err, rec.Body.String())
		}
		if e.Error.Class != "permanente" {
			t.Errorf("classe = %q, quero \"permanente\" — repetir o mesmo pedido nao vai funcionar", e.Error.Class)
		}
		if !strings.Contains(e.Error.Message, strconv.Itoa(c.retention)) {
			t.Errorf("a mensagem nao diz o prazo em vigor (%d dias): %q — sem o numero, quem le nao sabe o que pedir",
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
			t.Errorf("serie_dias=%q: status = %d, quero 400; corpo = %s", raw, rec.Code, rec.Body.String())
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
		t.Fatalf("status = %d, quero 403 (o vinculo decide antes do parametro); corpo = %s", rec.Code, rec.Body.String())
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
		t.Errorf("estado = %q, quero %q", r.CallbackCertificate.State, CertNeverObserved)
	}
	if v := r.CallbackCertificate.ExpiresAt; v != nil {
		t.Errorf("expira_em = %q numa instancia que nunca entregou — data inventada e pior que campo vazio", *v)
	}
	if v := r.CallbackCertificate.ObservedAt; v != nil {
		t.Errorf("observado_em = %q sem nenhuma observacao ter acontecido", *v)
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
		t.Fatalf("estado = %q, quero %q", r.CallbackCertificate.State, CertObserved)
	}
	if r.CallbackCertificate.ExpiresAt == nil || r.CallbackCertificate.ObservedAt == nil {
		t.Fatalf("estado %q com carimbo nulo: %+v — os dois andam juntos ou nao valem nada",
			CertObserved, r.CallbackCertificate)
	}
	if want := expires.Format(time.RFC3339); *r.CallbackCertificate.ExpiresAt != want {
		t.Errorf("expira_em = %q, quero %q", *r.CallbackCertificate.ExpiresAt, want)
	}
	if want := observed.Format(time.RFC3339); *r.CallbackCertificate.ObservedAt != want {
		t.Errorf("observado_em = %q, quero %q — o instante da ENTREGA, nao o de agora",
			*r.CallbackCertificate.ObservedAt, want)
	}
	// And `gerado_em` must NOT have turned into the observation timestamp: a
	// certificate seen two hours ago would stay eternally "just observed" and
	// the field would stop aging, which is the one useful thing it does.
	if *r.CallbackCertificate.ObservedAt == r.GeneratedAt {
		t.Errorf("observado_em = gerado_em (%q) — o carimbo esta sendo lido do relogio da resposta", r.GeneratedAt)
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
		t.Errorf("lojinha respondeu %q; a observacao gravada era da clinica (%+v)",
			r.CallbackCertificate.State, r.CallbackCertificate)
	}
}

// Every handler in this project serves each request in a goroutine over the
// SAME handler; without a concurrent test, -race is theater
// (docs/ARMADILHAS.md, "Go / concorrência"). Here the watchdog WRITES at the same
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
			t.Fatalf("leitura %d: status = %d, quero 200", i, c)
		}
	}
}
