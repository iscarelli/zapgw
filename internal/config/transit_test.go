package config

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/meta"
)

// TestSearchTransitFindsTheSameRowWithAnySpellingOfThePhoneNumber is T-094's
// Verify (a) — owner's decision, 2026-07-30 ("you can put the number in,
// it's not a secret"), which reverted, only for the phone number and the
// wamid, T-091's HMAC design (see the header of
// internal/config/transit.go). The FOUR SPELLINGS of the same subscriber
// — with/without "55", with/without the ninth digit, with/without
// formatting — have to find the SAME row: it's the gain that motivated the
// decision, because with HMAC only the EXACT form that generated the hash
// found it, and getting the spelling wrong returned "nothing found",
// indistinguishable from "this person never sent anything".
func TestSearchTransitFindsTheSameRowWithAnySpellingOfThePhoneNumber(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	// Writes in the CANONICAL form, already in the CLEAR — exactly what
	// internal/inbound/handler.go writes into TransitRecord.Counterparty
	// via Event.FromCanonical.
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound,
		Counterparty: meta.Canonicalize("5532999990000"), Type: string(meta.EventTypeMessage), Correlation: "c1",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	spellings := []string{
		"(32) 99999-0000", // as an operator would type it
		"32999990000",     // without the "55"
		"553299990000",    // with "55", without the ninth digit
		"5532999990000",   // canonical
	}
	for _, g := range spellings {
		lastEight := meta.LastEightDigits(g)
		found, err := s.SearchTransit("lojinha", lastEight, time.Time{})
		if err != nil {
			t.Fatalf("SearchTransit(%q): %v", g, err)
		}
		if len(found) != 1 {
			t.Fatalf("grafia %q (ultimosOito=%q): achadas = %d, quero 1", g, lastEight, len(found))
		}
		if found[0].Direction != DirectionInbound || found[0].Type != string(meta.EventTypeMessage) {
			t.Fatalf("grafia %q: linha achada = %+v, nao bate com o que foi gravado", g, found[0])
		}
	}
}

// TestSearchTransitWithEmptyLastEightDoesNotFindAccountWebhooks: an invalid
// phone number (fewer than 8 digits) produces
// meta.LastEightDigits("") = "", and that must NOT match
// `contraparte = ”` on ACCOUNT webhook rows (which never have a
// counterpart) — without this guard, searching for an invalid number
// would return every account webhook of the instance as if they were "matches".
func TestSearchTransitWithEmptyLastEightDoesNotFindAccountWebhooks(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound,
		Type: "status_de_template", Correlation: "c-conta",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	found, err := s.SearchTransit("lojinha", "", time.Time{})
	if err != nil {
		t.Fatalf("SearchTransit(\"\"): %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("achadas = %d, quero 0 — ultimosOito vazio nao pode achar linha sem contraparte", len(found))
	}
}

// TestSearchTransitBringsTheWamidWhenItExistsAndEmptyWithoutIt is T-128's Verify
// (a): the `wamid` column has existed in the database since T-094, but
// until now neither TransitLine nor the searches exposed it — the
// runbook for the two `ALARME ... PRECISA DE GENTE` in
// internal/outbound/handler.go tells the operator to write down the
// wa_message_id by hand without saying where to get it from. Writes TWO
// rows with the SAME counterpart (one inbound, without a wamid — Meta
// doesn't send an id on a received message; one outbound, with a wamid)
// and requires the exact value on the one that has it and empty ("") on
// the one that doesn't — a test that only checked "didn't break" would
// pass with the column always empty, exactly the defect this task exists
// to close.
func TestSearchTransitBringsTheWamidWhenItExistsAndEmptyWithoutIt(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	const number = "5532999990000"
	const wamid = "wamid.HBgNNTUzMjk5OTk5MDAwMBUCABIYFjNFQjBEO"
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound,
		Counterparty: meta.Canonicalize(number), Type: string(meta.EventTypeMessage), Correlation: "c-entrada",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit (entrada, sem wamid): %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionOutbound,
		Counterparty: meta.Canonicalize(number), Wamid: wamid, Type: "texto", Correlation: "c-saida", Outcome: "enviado",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit (saida, com wamid): %v", err)
	}

	lastEight := meta.LastEightDigits(number)
	found, err := s.SearchTransit("lojinha", lastEight, time.Time{})
	if err != nil {
		t.Fatalf("SearchTransit: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("achadas = %d, quero 2", len(found))
	}

	// SearchTransit returns from the MOST RECENT to the OLDEST — the
	// outbound one (written last) comes first.
	outboundRow, inboundRow := found[0], found[1]
	if outboundRow.Direction != DirectionOutbound || outboundRow.Wamid != wamid {
		t.Fatalf("linha de saida = %+v, queria Wamid = %q", outboundRow, wamid)
	}
	if inboundRow.Direction != DirectionInbound || inboundRow.Wamid != "" {
		t.Fatalf("linha de entrada = %+v, queria Wamid = \"\"", inboundRow)
	}
}

// TestHMACCorrelationOfEmptyIsEmpty: the SAME rule as always — an empty key
// returns an empty HMAC, never the HMAC of "".
func TestHMACCorrelationOfEmptyIsEmpty(t *testing.T) {
	s := testStore(t)
	if got := s.HMACCorrelation(""); got != "" {
		t.Fatalf("HMACCorrelation(\"\") = %q, quero vazio", got)
	}
}

// TestHMACCorrelationIsDeterministicAndDoesNotTakeThePlainValueBack is the
// POST-REVIEW FIX: the consumer's Idempotency-Key is free text from an
// EXTERNAL origin, and the `correlacao` column on the OUTBOUND side can
// only store the HMAC — never the value. This test proves both halves:
// the HMAC is the same for the same key (otherwise the --chave search
// would never find anything), and SearchTransitByCorrelation only finds
// by the HMAC, never by the key in the clear.
func TestHMACCorrelationIsDeterministicAndDoesNotTakeThePlainValueBack(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	const key = "pedido-5532999990000" // the way a reasonable consumer would choose it
	a := s.HMACCorrelation(key)
	b := s.HMACCorrelation(key)
	if a != b {
		t.Fatalf("HMACCorrelation(x) duas vezes deu %q e %q — nao e deterministico", a, b)
	}
	if a == key {
		t.Fatal("HMACCorrelation devolveu o proprio valor em claro")
	}

	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionOutbound,
		Correlation: a, Type: "texto", Outcome: "enviado",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	// A search by the HMAC (what the CLI does) finds the row.
	found, err := s.SearchTransitByCorrelation("lojinha", a, time.Time{})
	if err != nil {
		t.Fatalf("SearchTransitByCorrelation: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("achadas por HMAC = %d, quero 1", len(found))
	}

	// A search by the key IN THE CLEAR finds nothing — the column doesn't store the value.
	nothing, err := s.SearchTransitByCorrelation("lojinha", key, time.Time{})
	if err != nil {
		t.Fatalf("SearchTransitByCorrelation (valor em claro): %v", err)
	}
	if len(nothing) != 0 {
		t.Fatalf("achadas pela chave EM CLARO = %d, quero 0 — a coluna nao pode guardar o valor cru", len(nothing))
	}
}

// TestWriteTransitRefusesAnUnknownDirection: closed vocabulary, the same
// discipline as IncrementCounter (counter.go) — a value outside
// DirectionInbound/DirectionOutbound is an error, not a silent write.
func TestWriteTransitRefusesAnUnknownDirection(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	err := s.WriteTransit(TransitRecord{Slug: "lojinha", Direction: "lateral"}, time.Now())
	if err == nil {
		t.Fatal("WriteTransit aceitou uma direcao fora do vocabulario")
	}
}

// TestPurgeTransitDeletesOnlyWhatIsPastTheTTL is T-091's Verify (d): the SAME
// mold as TestPurgarContadoresApagaSoOQueVenceu / PurgeIdempotency —
// writes an OLD row and a NEW one, purges with a deadline only the old one
// crosses, and requires that ONLY the old one disappear.
func TestPurgeTransitDeletesOnlyWhatIsPastTheTTL(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	now := time.Now()
	oldOne := now.Add(-40 * 24 * time.Hour)
	newOne := now.Add(-1 * time.Hour)

	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound, Correlation: "velha",
	}, oldOne); err != nil {
		t.Fatalf("WriteTransit (velha): %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound, Correlation: "nova",
	}, newOne); err != nil {
		t.Fatalf("WriteTransit (nova): %v", err)
	}

	n, err := s.PurgeTransit(now.Add(-30 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("PurgeTransit: %v", err)
	}
	if n != 1 {
		t.Fatalf("PurgeTransit apagou %d linha(s), quero 1 (so a velha)", n)
	}

	var remainingOnes int
	if err := s.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "lojinha").Scan(&remainingOnes); err != nil {
		t.Fatalf("contar transito restante: %v", err)
	}
	if remainingOnes != 1 {
		t.Fatalf("restaram %d linha(s), quero 1 (a nova)", remainingOnes)
	}
	var remainingCorrelation string
	if err := s.DB().QueryRow(`SELECT correlacao FROM transito WHERE slug = ?`, "lojinha").Scan(&remainingCorrelation); err != nil {
		t.Fatalf("ler correlacao restante: %v", err)
	}
	if remainingCorrelation != "nova" {
		t.Fatalf("a linha restante e %q, quero a %q — a purga apagou a linha errada", remainingCorrelation, "nova")
	}
}

// testInstanceWithSlug is testInstance() with a different slug —
// used by T-096's tests, which need TWO instances to prove a cleanup (by
// instance or by phone number) only touches what it should.
func testInstanceWithSlug(slug string) Instance {
	i := testInstance()
	i.Slug = slug
	i.WabaID = "WABA-" + slug
	i.PhoneNumberID = "PNID-" + slug
	return i
}

// TestNumbersForLastEightReturnsAllTheDistinctOnes is the foundation of
// `zapgw log clear --telefone`'s ambiguity guard (T-096): two SYNTHETIC
// numbers with a different area code sharing the same last eight digits
// have to come back BOTH, not just one — it's what lets the caller count and refuse.
func TestNumbersForLastEightReturnsAllTheDistinctOnes(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	const numberA = "5511900000001" // synthetic — area code 11
	const numberB = "5532900000001" // synthetic — area code 32, SAME last 8 digits as A
	if meta.LastEightDigits(numberA) != meta.LastEightDigits(numberB) {
		t.Fatalf("fixture errada: A e B deveriam compartilhar os ultimos 8 digitos (A=%q B=%q)",
			meta.LastEightDigits(numberA), meta.LastEightDigits(numberB))
	}

	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound, Counterparty: numberA, Type: "mensagem", Correlation: "a",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit A: %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound, Counterparty: numberB, Type: "mensagem", Correlation: "b",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit B: %v", err)
	}

	numbers, err := s.NumbersForLastEight(meta.LastEightDigits(numberA))
	if err != nil {
		t.Fatalf("NumbersForLastEight: %v", err)
	}
	if len(numbers) != 2 {
		t.Fatalf("numeros = %v, quero os DOIS numeros distintos (A e B)", numbers)
	}
}

// TestNumbersForLastEightWithEmptyFindsNothing is the same reasoning as
// TestSearchTransitWithEmptyLastEightDoesNotFindAccountWebhooks, above: an
// empty `lastEight` must never match `contraparte = ”`.
func TestNumbersForLastEightWithEmptyFindsNothing(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound, Type: "status_de_template", Correlation: "c-conta",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	numbers, err := s.NumbersForLastEight("")
	if err != nil {
		t.Fatalf("NumbersForLastEight(\"\"): %v", err)
	}
	if len(numbers) != 0 {
		t.Fatalf("numeros = %v, queria vazio", numbers)
	}
}

// TestClearInstanceTransitDeletesOnlyItsOwn is T-096's Verify (a) at
// the store layer: deleting ONE instance's log cannot touch ANY row of another.
func TestClearInstanceTransitDeletesOnlyItsOwn(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance lojinha: %v", err)
	}
	if err := s.CreateInstance(testInstanceWithSlug("outra")); err != nil {
		t.Fatalf("CreateInstance outra: %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound, Type: "mensagem", Correlation: "l1",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit lojinha: %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "outra", Direction: DirectionInbound, Type: "mensagem", Correlation: "o1",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit outra: %v", err)
	}

	n, err := s.ClearInstanceTransit("lojinha")
	if err != nil {
		t.Fatalf("ClearInstanceTransit: %v", err)
	}
	if n != 1 {
		t.Fatalf("linhas apagadas = %d, quero 1", n)
	}

	var remainingLojinha, remainingOther int
	if err := s.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "lojinha").Scan(&remainingLojinha); err != nil {
		t.Fatalf("contar lojinha: %v", err)
	}
	if err := s.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "outra").Scan(&remainingOther); err != nil {
		t.Fatalf("contar outra: %v", err)
	}
	if remainingLojinha != 0 {
		t.Fatalf("lojinha deveria estar vazia, restaram %d", remainingLojinha)
	}
	if remainingOther != 1 {
		t.Fatalf("outra NAO deveria ter sido tocada, restaram %d (quero 1)", remainingOther)
	}
}

// TestClearTransitByPhoneDeletesAcrossAllInstancesAndCountsPerSlug is
// T-096's Verify (c) at the store layer: a single number deletes across
// ALL instances it appeared in, and returns the count PER instance.
func TestClearTransitByPhoneDeletesAcrossAllInstancesAndCountsPerSlug(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance lojinha: %v", err)
	}
	if err := s.CreateInstance(testInstanceWithSlug("outra")); err != nil {
		t.Fatalf("CreateInstance outra: %v", err)
	}

	const target = "5511900000002"    // synthetic
	const notTarget = "5532900000009" // synthetic, last 8 digits DIFFERENT from the target

	for _, corr := range []string{"alvo-1", "alvo-2"} {
		if err := s.WriteTransit(TransitRecord{
			Slug: "lojinha", Direction: DirectionInbound, Counterparty: target, Type: "mensagem", Correlation: corr,
		}, time.Now()); err != nil {
			t.Fatalf("WriteTransit alvo lojinha (%s): %v", corr, err)
		}
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "outra", Direction: DirectionInbound, Counterparty: target, Type: "mensagem", Correlation: "alvo-3",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit alvo outra: %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound, Counterparty: notTarget, Type: "mensagem", Correlation: "preservada",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit nao-alvo: %v", err)
	}

	deleted, err := s.ClearTransitByPhone(target)
	if err != nil {
		t.Fatalf("ClearTransitByPhone: %v", err)
	}
	bySlug := map[string]int64{}
	for _, a := range deleted {
		bySlug[a.Slug] = a.Rows
	}
	if bySlug["lojinha"] != 2 {
		t.Fatalf("lojinha: apagadas = %d, quero 2", bySlug["lojinha"])
	}
	if bySlug["outra"] != 1 {
		t.Fatalf("outra: apagadas = %d, quero 1", bySlug["outra"])
	}

	var remainingTarget, remainingPreserved int
	if err := s.DB().QueryRow(`SELECT count(*) FROM transito WHERE contraparte = ?`, target).Scan(&remainingTarget); err != nil {
		t.Fatalf("contar alvo: %v", err)
	}
	if err := s.DB().QueryRow(`SELECT count(*) FROM transito WHERE contraparte = ?`, notTarget).Scan(&remainingPreserved); err != nil {
		t.Fatalf("contar nao-alvo: %v", err)
	}
	if remainingTarget != 0 {
		t.Fatalf("restaram %d linha(s) do alvo, quero 0", remainingTarget)
	}
	if remainingPreserved != 1 {
		t.Fatalf("a linha do numero NAO-alvo foi tocada: restaram %d, quero 1", remainingPreserved)
	}
}

// TestHighestLogRowidGoesBackToZeroWhenTheTableBecomesEmpty is the foundation of
// T-096's item 5 fix: after deleting EVERYTHING, the highest rowid has to
// be 0 — it's the value that makes the follow (runLog,
// cmd/zapgw/log.go) roll back the cursor so the next row, even reusing a
// low rowid, gets seen.
func TestHighestLogRowidGoesBackToZeroWhenTheTableBecomesEmpty(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.WriteTransit(TransitRecord{
			Slug: "lojinha", Direction: DirectionInbound, Type: "mensagem", Correlation: fmt.Sprintf("c%d", i),
		}, time.Now()); err != nil {
			t.Fatalf("WriteTransit %d: %v", i, err)
		}
	}

	before, err := s.HighestLogRowid("")
	if err != nil {
		t.Fatalf("HighestLogRowid antes: %v", err)
	}
	if before < 3 {
		t.Fatalf("HighestLogRowid antes = %d, esperava pelo menos 3", before)
	}

	if _, err := s.ClearInstanceTransit("lojinha"); err != nil {
		t.Fatalf("ClearInstanceTransit: %v", err)
	}

	after, err := s.HighestLogRowid("")
	if err != nil {
		t.Fatalf("HighestLogRowid depois: %v", err)
	}
	if after != 0 {
		t.Fatalf("HighestLogRowid depois de apagar tudo = %d, quero 0", after)
	}
}

// TestTransitRecordNeverPropagatesAnError: the SAME guarantee as Counter
// (counter_test.go) — Record returns nothing, so the only possible
// proof is that the method does NOT PANIC and that the
// always-fails store was actually called.
type alwaysFailingTransitStore struct{ calls int }

func (f *alwaysFailingTransitStore) WriteTransit(TransitRecord, time.Time) error {
	f.calls++
	return errTestTransit
}

var errTestTransit = errors.New("falha de teste")

func TestTransitRecordNeverPropagatesAnError(t *testing.T) {
	fake := &alwaysFailingTransitStore{}
	tr := NewTransitWithStore(fake)
	tr.Record(TransitRecord{Slug: "lojinha", Direction: DirectionInbound})
	if fake.calls != 1 {
		t.Fatalf("chamadas = %d, quero 1 — Register nao chamou o store", fake.calls)
	}
}

// TestClearTransitByPhoneUnderConcurrencyTheCountMatchesWhatActuallyDisappeared
// (T-131) proves the comment on ClearTransitByPhone (transit.go,
// "COUNTS BEFORE DELETING, in the SAME transaction: counting afterward
// would count zero, and the per-instance count is the only proof, on
// screen, that the right thing was deleted"): while the cleanup runs,
// OTHER goroutines insert transit rows for the SAME phone number at the same time.
//
// THE INVARIANT doesn't depend on knowing WHEN the cleanup ran relative to
// the inserts — it's conservation: everything that was created, minus
// what's left, has to match what the function said it deleted. If the
// counting SELECT ran separately from the DELETE (outside the same
// transaction), a concurrent insert could land in the gap between the
// two: it wouldn't enter the count, but the DELETE (which matches by
// `contraparte`, with no time filter) would delete it anyway — the screen
// would lie with a number smaller than what actually disappeared.
func TestClearTransitByPhoneUnderConcurrencyTheCountMatchesWhatActuallyDisappeared(t *testing.T) {
	s := testStore(t)

	const rounds = 30
	const preExisting = 5
	const insertedInTheRace = 20
	const target = "5511900000099" // sintetico

	for round := 0; round < rounds; round++ {
		slug := fmt.Sprintf("corrida-limpeza-%d", round)
		if err := s.CreateInstance(testInstanceWithSlug(slug)); err != nil {
			t.Fatalf("rodada %d: CreateInstance: %v", round, err)
		}

		// Some rows already exist BEFORE the race starts, so the cleanup
		// has something to count even if it wins very early.
		for i := 0; i < preExisting; i++ {
			if err := s.WriteTransit(TransitRecord{
				Slug: slug, Direction: DirectionInbound, Counterparty: target,
				Type: "mensagem", Correlation: fmt.Sprintf("pre-%d-%d", round, i),
			}, time.Now()); err != nil {
				t.Fatalf("rodada %d: WriteTransit pre-existente: %v", round, err)
			}
		}

		var wg sync.WaitGroup
		wg.Add(insertedInTheRace + 1)
		for i := 0; i < insertedInTheRace; i++ {
			go func(n int) {
				defer wg.Done()
				err := retryOnBusy(t, func() error {
					return s.WriteTransit(TransitRecord{
						Slug: slug, Direction: DirectionInbound, Counterparty: target,
						Type: "mensagem", Correlation: fmt.Sprintf("corrida-%d-%d", round, n),
					}, time.Now())
				})
				if err != nil {
					t.Errorf("rodada %d: WriteTransit concorrente: %v", round, err)
				}
			}(i)
		}
		var deleted []TransitRowsDeleted
		var errCleanup error
		go func() {
			defer wg.Done()
			errCleanup = retryOnBusy(t, func() error {
				var err error
				deleted, err = s.ClearTransitByPhone(target)
				return err
			})
		}()
		wg.Wait()

		if errCleanup != nil {
			t.Fatalf("rodada %d: ClearTransitByPhone: %v", round, errCleanup)
		}
		var reported int64
		for _, a := range deleted {
			reported += a.Rows
		}

		var remaining int64
		if err := s.DB().QueryRow(`SELECT count(*) FROM transito WHERE contraparte = ?`, target).Scan(&remaining); err != nil {
			t.Fatalf("rodada %d: contar restantes: %v", round, err)
		}

		totalCreated := int64(preExisting + insertedInTheRace)
		actuallyRemoved := totalCreated - remaining
		if reported != actuallyRemoved {
			t.Fatalf("rodada %d: ClearTransitByPhone reportou %d apagadas, mas sumiram %d de fato "+
				"(criadas=%d, restam=%d) — a contagem nao bate com o que sumiu",
				round, reported, actuallyRemoved, totalCreated, remaining)
		}

		// The next round uses a different slug, but the phone number is
		// the SAME — clean up what's left so it doesn't accumulate
		// between rounds and distort the next round's count.
		if err := retryOnBusy(t, func() error {
			_, err := s.ClearTransitByPhone(target)
			return err
		}); err != nil {
			t.Fatalf("rodada %d: limpeza de saneamento: %v", round, err)
		}
	}
}
