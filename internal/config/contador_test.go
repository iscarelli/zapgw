package config

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestIncrementCounterAddsUpOnTheSameDay(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)

	if err := s.IncrementCounter("lojinha", CounterReceived, now); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}
	if err := s.IncrementCounter("lojinha", CounterReceived, now.Add(time.Hour)); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	m, err := s.CountersBetween("lojinha", now, now)
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[CounterReceived] != 2 {
		t.Fatalf("recebidas = %d, quero 2 — duas chamadas no MESMO dia tem de somar na mesma linha", m[CounterReceived])
	}
}

func TestIncrementCounterRefusesAKeyOutsideTheVocabulary(t *testing.T) {
	// The vocabulary is CLOSED on purpose (T-035): an unreviewed new key
	// is exactly the metric nobody is going to look at.
	s := testStore(t)
	err := s.IncrementCounter("lojinha", "metrica_inventada", time.Now())
	if !errors.Is(err, ErrUnknownCounterKey) {
		t.Fatalf("erro = %v, quero ErrUnknownCounterKey", err)
	}
}

func TestCountersBetweenSeparatesPerInstance(t *testing.T) {
	// ONE instance's counter cannot leak into another one's summary — the
	// same tenant-isolation reasoning the rest of the project requires.
	s := testStore(t)
	now := time.Now()

	_ = s.IncrementCounter("lojinha", CounterDelivered, now)
	_ = s.IncrementCounter("lojinha", CounterDelivered, now)
	_ = s.IncrementCounter("outra", CounterDelivered, now)

	m, err := s.CountersBetween("lojinha", now, now)
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[CounterDelivered] != 2 {
		t.Fatalf("entregues de lojinha = %d, quero 2 — contagem de OUTRA instancia vazou", m[CounterDelivered])
	}
}

func TestCountersBetweenSumsSeveralDaysInThePeriod(t *testing.T) {
	s := testStore(t)
	today := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	_ = s.IncrementCounter("lojinha", CounterReceived, today)
	_ = s.IncrementCounter("lojinha", CounterReceived, today.AddDate(0, 0, -3))
	_ = s.IncrementCounter("lojinha", CounterReceived, today.AddDate(0, 0, -8)) // outside the 7-day window

	m, err := s.CountersBetween("lojinha", today.AddDate(0, 0, -7), today)
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[CounterReceived] != 2 {
		t.Fatalf("recebidas nos ultimos 7 dias = %d, quero 2 (o evento de 8 dias atras fica DE FORA)", m[CounterReceived])
	}
}

func TestCountersBetweenWithNoEventReturnsAnEmptyMapWithNoError(t *testing.T) {
	// T-035's Verify (f): an instance with no traffic cannot turn into an error.
	s := testStore(t)
	m, err := s.CountersBetween("lojinha-sem-trafego", time.Now().AddDate(0, 0, -7), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("mapa = %v, quero vazio", m)
	}
	if m[CounterReceived] != 0 {
		t.Fatalf("m[CounterReceived] = %d, quero 0 (chave ausente lida como zero)", m[CounterReceived])
	}
}

// Verify (e): the purge deletes what's past the age and does NOT delete the rest.
func TestPurgeCountersDeletesOnlyWhatIsPastTheAge(t *testing.T) {
	s := testStore(t)
	today := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	oldStamp := today.AddDate(0, 0, -100)

	_ = s.IncrementCounter("lojinha", CounterReceived, oldStamp)
	_ = s.IncrementCounter("lojinha", CounterReceived, today)

	n, err := s.PurgeCounters(today.AddDate(0, 0, -90))
	if err != nil {
		t.Fatalf("PurgeCounters: %v", err)
	}
	if n != 1 {
		t.Fatalf("purgou %d linha(s), quero 1", n)
	}

	m, err := s.CountersBetween("lojinha", oldStamp, today)
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[CounterReceived] != 1 {
		t.Fatalf("recebidas apos purga = %d, quero 1 (o registro RECENTE tem de sobreviver)", m[CounterReceived])
	}
}

func TestPurgeCountersDoesNotDeleteARecentRecord(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	_ = s.IncrementCounter("lojinha", CounterReceived, now)

	n, err := s.PurgeCounters(now.AddDate(0, 0, -90))
	if err != nil {
		t.Fatalf("PurgeCounters: %v", err)
	}
	if n != 0 {
		t.Fatalf("purgou %d registro(s) recente(s), quero 0", n)
	}
}

// docs/ARMADILHAS.md, "Go / concorrência": every mutable state touched by
// concurrent goroutines needs a concurrent test run under -race, otherwise
// -race is theater. Counter.Record is called by the HTTP handler,
// which http.Server serves across goroutines over the SAME handler.
func TestCounterRecordUnderConcurrencyAddsUpRight(t *testing.T) {
	s := testStore(t)
	c := NewCounter(s)

	const goroutines = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c.Record("lojinha", CounterReceived)
		}()
	}
	wg.Wait()

	m, err := s.CountersBetween("lojinha", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("CountersBetween: %v", err)
	}
	if m[CounterReceived] != goroutines {
		t.Fatalf("recebidas = %d, quero %d — contagem perdida sob concorrencia", m[CounterReceived], goroutines)
	}
}

// Record RETURNS NO ERROR AT ALL, by signature — even when the written
// key is outside the closed vocabulary (the only way Record can fail in
// practice, since the write itself has no other plausible error path on a
// freshly opened store). The proof that the failure does NOT propagate
// lives in the handler tests (internal/inbound, internal/outbound): this
// test here only proves the call doesn't panic or hang.
func TestCounterRecordWithAnInvalidKeyOnlyLogsAndMovesOn(t *testing.T) {
	s := testStore(t)
	c := NewCounter(s)

	c.Record("lojinha", "chave-que-nao-existe-no-vocabulario")
	// Getting here, without a panic, is the proof: Record has no way to propagate it.
}

// --- Last event stamp (T-060) ---------------------------------------

// The stamp is what undoes the stalled-counter ambiguity: "stalled from
// failure" and "stalled because nobody wrote" are the SAME number, and
// only AGE separates the two. This test requires both halves — that it
// exists and that it tracks the LAST event, not the first.
func TestLastEventPerKeyKeepsTheMostRecentEvent(t *testing.T) {
	s := testStore(t)
	firstOne := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	last := firstOne.Add(3 * time.Hour)

	if err := s.IncrementCounter("lojinha", CounterReceived, firstOne); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}
	if err := s.IncrementCounter("lojinha", CounterReceived, last); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	m, err := s.LastEventPerKey("lojinha")
	if err != nil {
		t.Fatalf("LastEventPerKey: %v", err)
	}
	if !m[CounterReceived].Equal(last) {
		t.Errorf("carimbo de recebidas = %v, quero %v (o carimbo e do ULTIMO evento)", m[CounterReceived], last)
	}
}

// The stamp crosses the DAY boundary: the counter's two rows are different
// (the primary key includes the day), and a MAX() done inside a single row
// would return the wrong row's stamp.
func TestLastEventPerKeyCrossesTheDayBoundary(t *testing.T) {
	s := testStore(t)
	yesterday := time.Date(2026, 7, 25, 23, 59, 0, 0, time.UTC)
	today := time.Date(2026, 7, 26, 0, 1, 0, 0, time.UTC)

	if err := s.IncrementCounter("lojinha", CounterDelivered, today); err != nil {
		t.Fatalf("IncrementCounter (hoje): %v", err)
	}
	if err := s.IncrementCounter("lojinha", CounterDelivered, yesterday); err != nil {
		t.Fatalf("IncrementCounter (ontem): %v", err)
	}

	m, err := s.LastEventPerKey("lojinha")
	if err != nil {
		t.Fatalf("LastEventPerKey: %v", err)
	}
	if !m[CounterDelivered].Equal(today) {
		t.Errorf("carimbo de entregues = %v, quero %v — o MAX vale entre DIAS, nao so dentro de um",
			m[CounterDelivered], today)
	}
}

// A key with no event at all does NOT appear in the map: absence is
// "never". A zeroed time.Time in its place would be a year-1 stamp —
// plausible-looking for someone who only glances at the field, and a
// dashboard would print "last received: 01/01/0001" as if it were a real measurement.
func TestLastEventPerKeyInventsNoStampForAKeyWithNoEvent(t *testing.T) {
	s := testStore(t)
	if err := s.IncrementCounter("lojinha", CounterReceived, time.Now()); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	m, err := s.LastEventPerKey("lojinha")
	if err != nil {
		t.Fatalf("LastEventPerKey: %v", err)
	}
	if _, has := m[CounterSent]; has {
		t.Errorf("chave %q ganhou carimbo sem nunca ter sido contada: %v", CounterSent, m[CounterSent])
	}
}

// The stamp has NO window, unlike the counters: cutting at 7 days would
// make an event from 20 days ago look IDENTICAL to "never happened" —
// exactly the ambiguity it exists to undo.
func TestLastEventPerKeyHasNoSevenDayWindow(t *testing.T) {
	s := testStore(t)
	oldTime := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, -20)
	if err := s.IncrementCounter("lojinha", CounterReceived, oldTime); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	m, err := s.LastEventPerKey("lojinha")
	if err != nil {
		t.Fatalf("LastEventPerKey: %v", err)
	}
	if !m[CounterReceived].Equal(oldTime) {
		t.Errorf("carimbo de 20 dias atras = %v, quero %v — a janela de 7 dias nao pode corta-lo",
			m[CounterReceived], oldTime)
	}
}

func TestLastEventPerKeySeparatesPerInstance(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	if err := s.IncrementCounter("outra", CounterReceived, now); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}

	m, err := s.LastEventPerKey("lojinha")
	if err != nil {
		t.Fatalf("LastEventPerKey: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("a instancia \"lojinha\" herdou carimbo de outra: %v", m)
	}
}

// SummarizeCounters is THE SINGLE SOURCE of the numbers that come out of
// the gateway: `zapgw estado` (cmd/zapgw/estado.go) and GET /v1/estado
// (internal/outbound/estado_handler.go) read from here, and T-060 requires
// both to return the SAME numbers for the same instance at the same instant.
func TestSummarizeCountersJoinsTheTwoWindowsAndTheStamp(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.IncrementCounter("lojinha", CounterReceived, now); err != nil {
		t.Fatalf("IncrementCounter (hoje): %v", err)
	}
	if err := s.IncrementCounter("lojinha", CounterReceived, now.AddDate(0, 0, -3)); err != nil {
		t.Fatalf("IncrementCounter (3 dias atras): %v", err)
	}
	if err := s.IncrementCounter("lojinha", CounterReceived, now.AddDate(0, 0, -10)); err != nil {
		t.Fatalf("IncrementCounter (10 dias atras): %v", err)
	}

	r, err := s.SummarizeCounters("lojinha", now)
	if err != nil {
		t.Fatalf("SummarizeCounters: %v", err)
	}
	if r.Today[CounterReceived] != 1 {
		t.Errorf("hoje = %d, quero 1", r.Today[CounterReceived])
	}
	if r.Last7Days[CounterReceived] != 2 {
		t.Errorf("ultimos 7 dias = %d, quero 2 — o evento de 10 dias atras fica de FORA",
			r.Last7Days[CounterReceived])
	}
	if !r.LastEvent[CounterReceived].Equal(now) {
		t.Errorf("carimbo = %v, quero %v", r.LastEvent[CounterReceived], now)
	}
	if len(r.Series) != ShortSeriesDays || len(r.ShortSeries) != ShortSeriesDays {
		t.Errorf("sem janela pedida: Series tem %d e ShortSeries tem %d entradas, quero %d nas duas",
			len(r.Series), len(r.ShortSeries), ShortSeriesDays)
	}
}

// --- T-081: the series window -------------------------------------------------

// 🔴 THE MEASUREMENT THAT CAME BEFORE THE CODE (T-081's step 1): the 30
// days are ALREADY IN THE DATABASE, and this task did no new storage.
//
// What this test proves, and each half matters:
//
//  1. an event from 29 days ago SURVIVES a purge run with the production
//     deadline — the DefaultRetentionDays-day retention the contract
//     asserts is true, and not stale doc;
//  2. it SHOWS UP in a 30-day series, on its own day. The 7-day cut was
//     never about storage: it was the interval SummarizeCounters asked the
//     database for.
//
// If both hold, building a custom aggregation to "keep 30 days" would have
// been work on data that already existed — and once redundant storage is
// written, nobody undoes it.
func TestTheThirtyDaySeriesComesFromDataAlreadyInTheDatabase(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	ago := []int{29, 20, 8, 0}
	for _, d := range ago {
		if err := s.IncrementCounter("lojinha", CounterSent, now.AddDate(0, 0, -d)); err != nil {
			t.Fatalf("IncrementCounter(-%dd): %v", d, err)
		}
	}

	// The PRODUCTION PURGE, with the production deadline — not a test
	// deadline chosen to make the data fit.
	if _, err := s.PurgeCounters(now.AddDate(0, 0, -DefaultRetentionDays)); err != nil {
		t.Fatalf("PurgeCounters: %v", err)
	}

	r, err := s.SummarizeCountersWithSeries("lojinha", now, 30)
	if err != nil {
		t.Fatalf("SummarizeCountersWithSeries: %v", err)
	}
	if len(r.Series) != 30 {
		t.Fatalf("a serie tem %d entradas, quero 30", len(r.Series))
	}
	if want := dayOf(now.AddDate(0, 0, -29)); r.Series[0].Day != want {
		t.Errorf("o dia mais velho da serie e %q, quero %q (do mais velho para o mais novo)", r.Series[0].Day, want)
	}
	inTheSeries := map[string]int{}
	for _, d := range r.Series {
		inTheSeries[d.Day] = d.N[CounterSent]
	}
	for _, d := range ago {
		day := dayOf(now.AddDate(0, 0, -d))
		if inTheSeries[day] != 1 {
			t.Errorf("o evento de %d dia(s) atras (%s) conta %d na serie de 30 dias, quero 1",
				d, day, inTheSeries[day])
		}
	}
}

// THE WINDOW'S CEILING IS THE RETENTION, and this test ties the two
// numbers together: the OLDEST day of the largest possible series has to
// survive the purge.
//
// Without this tie, the ceiling would be a number chosen by eye — and the
// day someone touched the purge's math (the DELETE's `dia <`, the `-1` in
// the enumeration) the maximum series would start on an already-deleted
// day, returning zero wearing the face of measurement right on the
// chart's first bar.
func TestTheOldestDayOfTheMaximumWindowSurvivesThePurge(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	oldestOfTheWindow := now.AddDate(0, 0, -(DefaultRetentionDays - 1))
	if err := s.IncrementCounter("lojinha", CounterReceived, oldestOfTheWindow); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}
	// And one day PAST the deadline, which has to disappear: without it,
	// a DELETE that deleted nothing would pass as "the data survived".
	if err := s.IncrementCounter("lojinha", CounterReceived, now.AddDate(0, 0, -(DefaultRetentionDays+1))); err != nil {
		t.Fatalf("IncrementCounter (alem do prazo): %v", err)
	}

	n, err := s.PurgeCounters(now.AddDate(0, 0, -DefaultRetentionDays))
	if err != nil {
		t.Fatalf("PurgeCounters: %v", err)
	}
	if n != 1 {
		t.Fatalf("a purga apagou %d linha(s), quero 1 (so a que passou do prazo)", n)
	}

	r, err := s.SummarizeCountersWithSeries("lojinha", now, DefaultRetentionDays)
	if err != nil {
		t.Fatalf("SummarizeCountersWithSeries: %v", err)
	}
	if len(r.Series) != DefaultRetentionDays {
		t.Fatalf("a serie tem %d entradas, quero %d", len(r.Series), DefaultRetentionDays)
	}
	if firstOne := r.Series[0]; firstOne.Day != dayOf(oldestOfTheWindow) || firstOne.N[CounterReceived] != 1 {
		t.Errorf("o dia mais velho da janela maxima e %q com %d evento(s), quero %q com 1 — o teto passou do que a purga guarda",
			firstOne.Day, firstOne.N[CounterReceived], dayOf(oldestOfTheWindow))
	}
}

// `ShortSeries` is the SUFFIX of `Series`, coming out of the SAME read: same
// days, same numbers. Two independent queries could disagree (all it
// takes is midnight falling between them) and the consumer would see
// `serie_7_dias` and `serie_diaria` counting the same day differently.
func TestShortSeriesIsTheSevenDaySuffixOfTheRequestedWindow(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	for _, d := range []int{0, 2, 6, 9, 25} {
		if err := s.IncrementCounter("lojinha", CounterDelivered, now.AddDate(0, 0, -d)); err != nil {
			t.Fatalf("IncrementCounter(-%dd): %v", d, err)
		}
	}

	r, err := s.SummarizeCountersWithSeries("lojinha", now, 30)
	if err != nil {
		t.Fatalf("SummarizeCountersWithSeries: %v", err)
	}
	if len(r.ShortSeries) != ShortSeriesDays {
		t.Fatalf("ShortSeries tem %d entradas com janela de 30, quero %d", len(r.ShortSeries), ShortSeriesDays)
	}
	suffix := r.Series[len(r.Series)-ShortSeriesDays:]
	for i, d := range r.ShortSeries {
		if d.Day != suffix[i].Day || d.N[CounterDelivered] != suffix[i].N[CounterDelivered] {
			t.Errorf("ShortSeries[%d] = (%s, %d) e o sufixo de Series = (%s, %d) — as duas saem da MESMA leitura",
				i, d.Day, d.N[CounterDelivered], suffix[i].Day, suffix[i].N[CounterDelivered])
		}
	}
	// The sum of the 7 still matches Last7Days: the events from 9 and
	// 25 days ago are left out of both.
	var sum int
	for _, d := range r.ShortSeries {
		sum += d.N[CounterDelivered]
	}
	if sum != r.Last7Days[CounterDelivered] {
		t.Errorf("soma da ShortSeries = %d, Last7Days = %d — as duas contas tem de bater",
			sum, r.Last7Days[CounterDelivered])
	}
	// And a window SMALLER than 7 doesn't shrink the short series: it's a living contract.
	shortR, err := s.SummarizeCountersWithSeries("lojinha", now, 3)
	if err != nil {
		t.Fatalf("SummarizeCountersWithSeries(3): %v", err)
	}
	if len(shortR.Series) != 3 || len(shortR.ShortSeries) != ShortSeriesDays {
		t.Errorf("janela de 3 dias: Series tem %d e ShortSeries tem %d, quero 3 e %d",
			len(shortR.Series), len(shortR.ShortSeries), ShortSeriesDays)
	}
}

// The retention deadline is resolved in ONE place (the purge in
// cmd/zapgw/main.go and GET /v1/estado's series ceiling both read from
// here), and an invalid value falls back to the default instead of
// bringing the server down on startup — the behavior the purge already had
// before this function existed.
func TestCounterRetentionDaysReadsTheEnvironment(t *testing.T) {
	env := func(value string) func(string) string {
		return func(k string) string {
			if k == CounterRetentionEnvVar {
				return value
			}
			return ""
		}
	}
	for _, c := range []struct {
		value string
		want  int
	}{
		{"", DefaultRetentionDays},
		{"15", 15},
		{"365", 365},
		{"abc", DefaultRetentionDays},
		{"0", DefaultRetentionDays},
		{"-7", DefaultRetentionDays},
	} {
		if has := CounterRetentionDays(env(c.value)); has != c.want {
			t.Errorf("%s=%q -> %d dias, quero %d", CounterRetentionEnvVar, c.value, has, c.want)
		}
	}
	if has := CounterRetentionDays(nil); has != DefaultRetentionDays {
		t.Errorf("sem ambiente nenhum -> %d dias, quero %d", has, DefaultRetentionDays)
	}
}
