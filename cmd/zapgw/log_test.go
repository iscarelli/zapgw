package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// syncBuffer is a bytes.Buffer safe for WRITING from one goroutine (the
// follow) and READING from another (the test, polling until the expected
// row appears) — without this both sides race over the same
// bytes.Buffer and `-race` flags it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func createTestLogInstance(t *testing.T, store *config.Store, slug string) {
	t.Helper()
	if err := store.CreateInstance(config.Instance{
		Slug: slug, WabaID: "WABA-" + slug, PhoneNumberID: "PNID-" + slug,
		AppSecret: "a", VerifyToken: "v", SendToken: "t",
		CallbackURL: "https://consumidor.interno/webhooks/zapgw", DeliverySecret: "s", TimeoutMs: 100,
	}); err != nil {
		t.Fatalf("CreateInstance(%q): %v", slug, err)
	}
}

// (a) with 250 rows recorded, `zapgw log` prints the LAST 200, and the
// LAST one printed is the most RECENT — chronological order, oldest
// first, the way a log is read.
func TestLogLast200InChronologicalOrder(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 250; i++ {
		if err := store.WriteTransit(config.TransitRecord{
			Slug: "lojinha", Direction: config.DirectionInbound,
			Type: "mensagem", Correlation: fmt.Sprintf("c-%03d", i),
			Outcome: fmt.Sprintf("linha-%03d", i),
		}, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("WriteTransit %d: %v", i, err)
		}
	}

	var out bytes.Buffer
	// context ALREADY CANCELED: we only want the initial dump, without
	// entering the follow — test (e), further below, is what proves the
	// cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runLog(ctx, store, "", defaultLogRows, &out, time.Hour); err != nil {
		t.Fatalf("runLog: %v", err)
	}

	text := out.String()
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("saida vazia")
	}
	data := lines[1:] // the first line is the header
	if len(data) != 200 {
		t.Fatalf("esperava 200 linhas de dado, vieram %d:\n%s", len(data), text)
	}
	// 250 rows (indices 0..249): the last 200 are 50..249. The oldest of
	// them (linha-050) has to come FIRST, the most recent (linha-249)
	// LAST.
	if !strings.Contains(data[0], "linha-050") {
		t.Fatalf("primeira linha impressa deveria ser a mais antiga das ultimas 200 (linha-050):\n%s", data[0])
	}
	if !strings.Contains(data[len(data)-1], "linha-249") {
		t.Fatalf("ultima linha impressa deveria ser a mais recente (linha-249):\n%s", data[len(data)-1])
	}
	if strings.Contains(text, "linha-049") {
		t.Fatalf("linha-049 nao deveria aparecer — esta fora das ultimas 200:\n%s", text)
	}
}

// (b) 🔴 the follow must not lose a row when two arrive in the SAME
// second. This is the test that distinguishes a cursor by rowid (correct)
// from a cursor by timestamp (T-093 forbids it): with a cursor by
// timestamp, the SECOND row of the pair would never satisfy "carimbo >
// ultimo", because the two timestamps are IDENTICAL.
func TestLogFollowDoesNotLoseARowInTheSameSecond(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- runLog(ctx, store, "", defaultLogRows, out, 5*time.Millisecond)
	}()

	// gives the follow time to enter the loop before recording, otherwise
	// both rows could come out through the INITIAL DUMP instead of the
	// follow — which would prove nothing about the cursor.
	time.Sleep(30 * time.Millisecond)

	sameInstant := time.Now()
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "c-a", Outcome: "linha-A-mesmo-segundo",
	}, sameInstant); err != nil {
		t.Fatalf("WriteTransit A: %v", err)
	}
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "c-b", Outcome: "linha-B-mesmo-segundo",
	}, sameInstant); err != nil {
		t.Fatalf("WriteTransit B: %v", err)
	}

	deadline := time.After(2 * time.Second)
waitLoop:
	for {
		text := out.String()
		if strings.Contains(text, "linha-A-mesmo-segundo") && strings.Contains(text, "linha-B-mesmo-segundo") {
			break waitLoop
		}
		select {
		case <-deadline:
			t.Fatalf("follow nao pegou as duas linhas do mesmo segundo em 2s (perda silenciosa):\n%s", text)
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runLog: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLog nao retornou apos o cancelamento — o teste pendurou")
	}
}

// (c) --n and --instancia work, and NO flag is required — the shape the
// owner required, 2026-07-30 ("I just want a zapgw log"). Tests only the
// PARSING (parseLogFlags), without opening the store nor entering the
// follow, which only ends with Ctrl-C.
func TestLogCommandOptionalFlagsWork(t *testing.T) {
	var out bytes.Buffer

	// parseLogFlags receives the arguments AFTER the subcommand's
	// name — the same convention as dispatch (`logCommand(args[1:],
	// ...)`) and every `comando*` in this package.
	n, slug, keepGoing, err := parseLogFlags(nil, &out)
	if err != nil || !keepGoing {
		t.Fatalf("sem flag nenhuma deveria funcionar: seguir=%v err=%v", keepGoing, err)
	}
	if n != defaultLogRows {
		t.Fatalf("--n default = %d, queria %d", n, defaultLogRows)
	}
	if slug != "" {
		t.Fatalf("--instancia default deveria ser vazio, veio %q", slug)
	}

	n, slug, keepGoing, err = parseLogFlags([]string{"--n", "5", "--instancia", "lojinha"}, &out)
	if err != nil || !keepGoing {
		t.Fatalf("--n e --instancia deveriam ser aceitos: seguir=%v err=%v", keepGoing, err)
	}
	if n != 5 {
		t.Fatalf("--n = %d, queria 5", n)
	}
	if slug != "lojinha" {
		t.Fatalf("--instancia = %q, queria \"lojinha\"", slug)
	}

	if _, _, _, err := parseLogFlags([]string{"--n", "0"}, &out); err == nil {
		t.Fatal("--n 0 deveria ser recusado (nao ha 'ultimas 0 linhas')")
	}
}

// (c), the half that exercises the BEHAVIOR: --instancia filters and --n
// limits when `runLog` actually runs against the store.
func TestRunLogHonorsInstanceAndN(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")
	createTestLogInstance(t, store, "outra")

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		if err := store.WriteTransit(config.TransitRecord{
			Slug: "lojinha", Direction: config.DirectionInbound,
			Type: "mensagem", Correlation: fmt.Sprintf("lj-%d", i), Outcome: fmt.Sprintf("lojinha-%d", i),
		}, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("WriteTransit lojinha %d: %v", i, err)
		}
		if err := store.WriteTransit(config.TransitRecord{
			Slug: "outra", Direction: config.DirectionInbound,
			Type: "mensagem", Correlation: fmt.Sprintf("ot-%d", i), Outcome: fmt.Sprintf("outra-%d", i),
		}, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("WriteTransit outra %d: %v", i, err)
		}
	}

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runLog(ctx, store, "lojinha", 2, &out, time.Hour); err != nil {
		t.Fatalf("runLog: %v", err)
	}

	text := out.String()
	if strings.Contains(text, "outra-") {
		t.Fatalf("--instancia lojinha vazou linha de outra instancia:\n%s", text)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	data := lines[1:]
	if len(data) != 2 {
		t.Fatalf("--n 2 deveria limitar a 2 linhas, vieram %d:\n%s", len(data), text)
	}
	if !strings.Contains(text, "lojinha-3") || !strings.Contains(text, "lojinha-4") {
		t.Fatalf("--n 2 deveria trazer as DUAS ULTIMAS (lojinha-3, lojinha-4):\n%s", text)
	}
}

// (d) 🔴 THE FLIP: until 2026-07-30 this test
// (TestLogNaoVazaTelefoneNemHMAC) required the OPPOSITE — that neither
// HMAC nor phone number appear in the output. Minutes after seeing that
// guarantee, the owner decided the opposite: "you can put the number in,
// it's not a secret" (T-094). `zapgw log` STARTS SHOWING the counterparty
// in PLAIN TEXT — this is exactly the gain that motivated the task: a
// log that only said "something passed at 21:20" with no word on who
// only answered half of the question that started T-091 (identifying who
// sent a specific message).
func TestLogShowsTheCounterpartInTheClear(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	const sentinelNumber = "5532999990000"
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Counterparty: sentinelNumber, Type: "mensagem", Correlation: "c1", Outcome: "consumidor guardou (200)",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runLog(ctx, store, "", defaultLogRows, &out, time.Hour); err != nil {
		t.Fatalf("runLog: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "mensagem") || !strings.Contains(text, "consumidor guardou (200)") {
		t.Fatalf("saida nao trouxe a linha esperada:\n%s", text)
	}
	if !strings.Contains(text, sentinelNumber) {
		t.Fatalf("a saida NAO mostrou o telefone — decisao do dono (T-094) foi mostrar:\n%s", text)
	}
}

// (d), the second half: a row recorded BEFORE T-094 has an EMPTY
// counterparty (HMAC is one-way, there is no way to recover the phone
// number from the hash T-091 recorded) — and this is NOT A BUG. `zapgw
// log` prints "—" on that row, never an empty value that looks like "no
// sender", and the table stays well-formed (same number of columns on
// both rows).
func TestLogShowsDashForOldRowWithoutCounterpart(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	// Simulates a row recorded BEFORE T-094: an empty Counterparty, the way
	// every v0.32.0 row stays after the migration (HMAC does not
	// reverse).
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "c-antiga", Outcome: "consumidor guardou (200)",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runLog(ctx, store, "", defaultLogRows, &out, time.Hour); err != nil {
		t.Fatalf("runLog: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("esperava cabecalho + 1 linha de dado, vieram %d:\n%s", len(lines), out.String())
	}
	// THE TABLE DID NOT BREAK: the rest of the row (tipo, desfecho) stays
	// intact next to the "—" — an empty column must not have dragged or
	// cut the following columns.
	if !strings.Contains(lines[1], "mensagem") || !strings.Contains(lines[1], "consumidor guardou (200)") {
		t.Fatalf("linha antiga: o resto dos campos nao sobreviveu ao lado da contraparte vazia:\n%s", lines[1])
	}
	if !strings.Contains(lines[1], "—") {
		t.Fatalf("linha antiga sem contraparte deveria imprimir \"—\", veio:\n%s", lines[1])
	}
}

// (f) 🔴 T-095, THE TEST THAT DISTINGUISHES fixed width from tabwriter —
// it needs to go RED with tabwriter, otherwise it proves nothing. The
// tabwriter aligns by BATCH (only what it saw before the Flush): the
// header is a batch on its own, the initial block is another batch, and
// each follow round is a THIRD batch — in practice, a single row. Each
// batch computes the "instancia" column's width from what it itself
// contains: the header sees the word "instancia" (9 letters); the
// initial block and the follow see the slug "lojinha" (7 letters) — THEIR
// OWN batches, written at different moments, with no visibility into each
// other. The result with tabwriter: the initial block and the follow
// agree with each other (same slug, same batch-type), but both DIVERGE
// from the header. It is exactly this pattern — initial matches follow,
// neither matches the header — that this test captures by comparing
// COLUMN INDICES, never the whole string (comparing the whole string
// would not catch the defect, because each row's VALUES are different by
// nature).
func TestLogFixedAlignmentBetweenInitialBlockAndFollow(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	// a row ALREADY RECORDED — comes out through the INITIAL BLOCK, the
	// first batch after the header.
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "c-inicial", Outcome: "linha-do-bloco-inicial",
	}, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("WriteTransit inicial: %v", err)
	}

	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runLog(ctx, store, "", defaultLogRows, out, 5*time.Millisecond)
	}()

	// waits for the INITIAL BLOCK to appear before recording the follow's
	// row — otherwise both could land in the same batch and the test
	// would prove nothing about DIFFERENT batches.
	deadline := time.After(2 * time.Second)
	for !strings.Contains(out.String(), "linha-do-bloco-inicial") {
		select {
		case <-deadline:
			t.Fatalf("bloco inicial nao apareceu em 2s:\n%s", out.String())
		case <-time.After(5 * time.Millisecond):
		}
	}

	// records the row that comes out through the FOLLOW — a batch
	// SEPARATE from the initial one, which is exactly where the tabwriter
	// (aligns by batch) would misalign.
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "c-follow", Outcome: "linha-do-bloco-follow",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit follow: %v", err)
	}

	deadline = time.After(2 * time.Second)
	for !strings.Contains(out.String(), "linha-do-bloco-follow") {
		select {
		case <-deadline:
			t.Fatalf("linha do follow nao apareceu em 2s:\n%s", out.String())
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runLog: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLog nao retornou apos o cancelamento — o teste pendurou")
	}

	text := out.String()
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("esperava cabecalho + bloco inicial + follow, vieram %d linhas:\n%s", len(lines), text)
	}
	header := lines[0]

	var initialLine, followLine string
	for _, l := range lines[1:] {
		switch {
		case strings.Contains(l, "linha-do-bloco-inicial"):
			initialLine = l
		case strings.Contains(l, "linha-do-bloco-follow"):
			followLine = l
		}
	}
	if initialLine == "" || followLine == "" {
		t.Fatalf("nao encontrei as duas linhas esperadas:\n%s", text)
	}

	headerOffset := strings.Index(header, "instancia")
	initialOffset := strings.Index(initialLine, "lojinha")
	followOffset := strings.Index(followLine, "lojinha")
	if headerOffset < 0 || initialOffset < 0 || followOffset < 0 {
		t.Fatalf("nao encontrei a coluna instancia numa das linhas:\ncabecalho=%q\ninicial=%q\nfollow=%q",
			header, initialLine, followLine)
	}
	if initialOffset != headerOffset || followOffset != headerOffset {
		t.Fatalf("colunas desalinhadas (isto e o que o tabwriter fazia): cabecalho comeca \"instancia\" em %d, "+
			"bloco inicial comeca o slug em %d, follow comeca o slug em %d\ncabecalho=%q\ninicial=%q\nfollow=%q",
			headerOffset, initialOffset, followOffset, header, initialLine, followLine)
	}
}

// (g) a 30-character slug is not truncated — the ENTIRE slug appears in
// the output, even being wider than the `instancia` column (24).
// Truncating would lose the identity of what is being read; the row just
// comes out wider.
func TestLogWideSlugIsNotTruncated(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)

	// 30 characters: "slug-" (5) + 25 "z" = 30. Lowercase, digits and
	// hyphen, no hyphen at the tip — within the shape config.ValidateSlug
	// requires (3 to 40 characters).
	wideSlug := "slug-" + strings.Repeat("z", 25)
	if len(wideSlug) != 30 {
		t.Fatalf("wideSlug tem %d caracteres, queria 30", len(wideSlug))
	}
	createTestLogInstance(t, store, wideSlug)

	if err := store.WriteTransit(config.TransitRecord{
		Slug: wideSlug, Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "c-largo", Outcome: "linha-do-slug-largo",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runLog(ctx, store, "", defaultLogRows, &out, time.Hour); err != nil {
		t.Fatalf("runLog: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, wideSlug) {
		t.Fatalf("o slug de 30 caracteres foi truncado, saida:\n%s", text)
	}
}

// (h) with every value fitting inside the column's width, the header
// and the body have EXACTLY the same column positions, for all six
// fields — not just "instancia", which test (f) already covers.
func TestLogHeaderMatchesBodyWhenValuesFit(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	// SYNTHETIC number (CLAUDE.md's hard rule: a new fixture uses a number
	// that is not the owner's real phone number) — "all zero" pattern
	// after the area code, recognizable as fake, with the 13 digits a
	// Brazilian E.164 uses.
	const syntheticCounterpart = "5511900000000"
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Counterparty: syntheticCounterpart, Type: "mensagem", Correlation: "c-cabe",
		Outcome: "consumidor guardou (200)",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runLog(ctx, store, "", defaultLogRows, &out, time.Hour); err != nil {
		t.Fatalf("runLog: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("esperava cabecalho + 1 linha, vieram %d:\n%s", len(lines), out.String())
	}
	header, body := lines[0], lines[1]

	cases := []struct {
		column string
		label  string
		value  string
	}{
		{"instancia", "instancia", "lojinha"},
		{"contraparte", "contraparte", syntheticCounterpart},
		{"direcao", "direcao", "entrada"},
		{"tipo", "tipo", "mensagem"},
		{"desfecho", "desfecho", "consumidor guardou (200)"},
	}
	for _, c := range cases {
		headerOff := strings.Index(header, c.label)
		if headerOff < 0 {
			t.Fatalf("rotulo %q nao encontrado no cabecalho: %q", c.label, header)
		}
		bodyOff := strings.Index(body, c.value)
		if bodyOff < 0 {
			t.Fatalf("valor %q nao encontrado no corpo: %q", c.value, body)
		}
		if bodyOff != headerOff {
			t.Fatalf("coluna %s desalinhada: cabecalho em %d, corpo em %d\ncabecalho=%q\ncorpo=%q",
				c.column, headerOff, bodyOff, header, body)
		}
	}
}

// (i) 🔴 T-095, THE OWNER'S ADJUSTMENT: the timestamp (RFC3339, always
// 20 characters) already showed that a column with no explicit SEPARATOR
// glues to the next one whenever the value fills the entire width — this
// task's first version only gave the field a width, no separator, and
// the timestamp glued to the instance on every row. The correct fix is
// not to bump the width by 1 (that just defers the same problem to the
// day another field also fills the column — `tipo` with "template" or a
// 13-digit `contraparte` already come close). This test proves that,
// even with `tipo` and `contraparte` FILLING THE ENTIRE WIDTH of their
// own column, the 2 separator spaces are still there before the next
// field starts.
func TestLogSeparatorSurvivesWhenValueFillsTheColumn(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	// "tipo" with EXACTLY logTypeWidth (10) characters, and
	// "contraparte" with EXACTLY logCounterpartWidth (15) — both fields
	// with NO slack at all for their own value, the same scenario the
	// timestamp (always 20/20) already exposed before this adjustment.
	// exactType and exactCounterpart use DIFFERENT alphabets (letter vs.
	// digit) on purpose: one cannot be a substring of the other,
	// otherwise strings.Index finds the wrong occurrence (the one inside
	// the other field) and the test lies about which column it is
	// measuring.
	const exactType = "zzzzzzzzzz"             // 10 characters
	const exactCounterpart = "999888777666555" // 15 characters (synthetic — not a real E.164, just fills the column)
	if len(exactType) != logTypeWidth {
		t.Fatalf("exactType tem %d caracteres, queria %d (logTypeWidth)", len(exactType), logTypeWidth)
	}
	if len(exactCounterpart) != logCounterpartWidth {
		t.Fatalf("exactCounterpart tem %d caracteres, queria %d (logCounterpartWidth)", len(exactCounterpart), logCounterpartWidth)
	}

	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Counterparty: exactCounterpart, Type: exactType, Correlation: "c-exato",
		Outcome: "linha-com-colunas-cheias",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runLog(ctx, store, "", defaultLogRows, &out, time.Hour); err != nil {
		t.Fatalf("runLog: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("esperava cabecalho + 1 linha, vieram %d:\n%s", len(lines), out.String())
	}
	header, body := lines[0], lines[1]

	// expectedSeparator is a LITERAL, on purpose — it does not reference
	// logColumnSeparator. If the production constant became "" (or
	// anything smaller than 2 spaces), strings.HasPrefix(x,
	// logColumnSeparator) would accept ANYTHING as a prefix of "" and the
	// test would pass even with the column glued — tested by hand by
	// zeroing the constant before writing this version. The literal is
	// what keeps the test flagging it when the implementation regresses.
	const expectedSeparator = "  "

	// the header also has to have the 2 spaces there — same constant,
	// same format (logRowFormat), so "contraparte" (11 letters) and
	// "direcao" have to be separated by >= 2 spaces already in the
	// header.
	counterpartHeaderOff := strings.Index(header, "contraparte")
	if counterpartHeaderOff < 0 {
		t.Fatalf("rotulo \"contraparte\" nao encontrado no cabecalho: %q", header)
	}
	afterLabel := header[counterpartHeaderOff+len("contraparte"):]
	if !strings.HasPrefix(afterLabel, expectedSeparator) {
		t.Fatalf("cabecalho: rotulo \"contraparte\" nao tem os 2 espacos de separador antes do proximo: %q", header)
	}

	// in the BODY: after "tipo" (exactly 10 characters), the SEPARATOR (2
	// spaces) has to come before "desfecho" starts — it must not glue.
	typeOff := strings.Index(body, exactType)
	if typeOff < 0 {
		t.Fatalf("tipo exato nao encontrado no corpo: %q", body)
	}
	afterType := body[typeOff+len(exactType):]
	if !strings.HasPrefix(afterType, expectedSeparator) {
		t.Fatalf("tipo com largura exata (%d) colou na coluna seguinte, sem os 2 espacos de separador: %q",
			logTypeWidth, body)
	}

	// after "contraparte" (exactly 15 characters), the SEPARATOR has to
	// come before "direcao" starts.
	counterpartOff := strings.Index(body, exactCounterpart)
	if counterpartOff < 0 {
		t.Fatalf("contraparte exata nao encontrada no corpo: %q", body)
	}
	afterCounterpart := body[counterpartOff+len(exactCounterpart):]
	if !strings.HasPrefix(afterCounterpart, expectedSeparator) {
		t.Fatalf("contraparte com largura exata (%d) colou na coluna seguinte, sem os 2 espacos de separador: %q",
			logCounterpartWidth, body)
	}
}

// (e) the follow loop ends when the context is canceled (simulated
// Ctrl-C) and returns nil — no panic, no error. With a TIMEOUT: if the
// loop doesn't end, the test fails instead of hanging the entire suite.
func TestRunLogEndsWhenTheContextIsCancelled(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- runLog(ctx, store, "", defaultLogRows, &out, 5*time.Millisecond)
	}()

	time.Sleep(20 * time.Millisecond) // lets the follow enter the loop
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runLog apos Ctrl-C simulado devolveu erro (queria nil): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLog nao retornou apos o contexto ser cancelado — o teste pendurou")
	}
}

// --- T-096: `zapgw log clear` --------------------------------------------

// Verify (a): `--instancia` with no `--confirmo` rejects and deletes
// nothing; with the WRONG slug in `--confirmo`, same; with the RIGHT
// slug, deletes only that instance's and does not touch the others. THE
// SAME pattern as `instancia remover` (provision_test.go).
func TestLogClearInstanceRequiresConfirmEqualToTheSlug(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")
	createTestLogInstance(t, store, "outra")

	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound, Type: "mensagem", Correlation: "c1", Outcome: "linha-lojinha",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit lojinha: %v", err)
	}
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "outra", Direction: config.DirectionInbound, Type: "mensagem", Correlation: "c2", Outcome: "linha-outra",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit outra: %v", err)
	}

	var out bytes.Buffer
	if err := logCommand([]string{"clear", "--instancia", "lojinha"}, &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("sem --confirmo deveria RECUSAR")
	}
	out.Reset()
	if err := logCommand([]string{"clear", "--instancia", "lojinha", "--confirmo", "outra"}, &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("--confirmo com o slug ERRADO deveria RECUSAR")
	}

	check := storeFromEnvironment(t, vars)
	var remainingLojinha, remainingOther int
	if err := check.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "lojinha").Scan(&remainingLojinha); err != nil {
		t.Fatalf("contar lojinha: %v", err)
	}
	if err := check.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "outra").Scan(&remainingOther); err != nil {
		t.Fatalf("contar outra: %v", err)
	}
	if remainingLojinha != 1 || remainingOther != 1 {
		t.Fatalf("as duas recusas nao deveriam ter apagado nada: lojinha=%d outra=%d (quero 1 e 1)", remainingLojinha, remainingOther)
	}

	out.Reset()
	if err := logCommand([]string{"clear", "--instancia", "lojinha", "--confirmo", "lojinha"}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("clear com --confirmo certo: %v", err)
	}

	check2 := storeFromEnvironment(t, vars)
	var remainingLojinha2, remainingOther2 int
	if err := check2.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "lojinha").Scan(&remainingLojinha2); err != nil {
		t.Fatalf("contar lojinha (2): %v", err)
	}
	if err := check2.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "outra").Scan(&remainingOther2); err != nil {
		t.Fatalf("contar outra (2): %v", err)
	}
	if remainingLojinha2 != 0 {
		t.Fatalf("lojinha deveria estar vazia depois do clear, restaram %d", remainingLojinha2)
	}
	if remainingOther2 != 1 {
		t.Fatalf("outra NAO deveria ter sido tocada, restaram %d (quero 1)", remainingOther2)
	}
}

// Verify (item 3 of T-114): `--telefone` with no `--confirmo` rejects and
// deletes nothing; with a DIVERGENT `--confirmo`, same; with an IDENTICAL
// `--confirmo`, deletes as usual. THE SAME pattern as
// TestLogClearInstanceRequiresConfirmEqualToTheSlug, above — until
// T-114 this half required no `--confirmo` at all, the inconsistency
// this task closes.
func TestLogClearPhoneRequiresConfirmEqualToThePhone(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	const targetNumber = "5511900000077" // synthetic
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound, Counterparty: targetNumber,
		Type: "mensagem", Correlation: "c1", Outcome: "linha-alvo",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	var out bytes.Buffer
	if err := logCommand([]string{"clear", "--telefone", targetNumber}, &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("sem --confirmo deveria RECUSAR")
	}
	out.Reset()
	if err := logCommand([]string{"clear", "--telefone", targetNumber, "--confirmo", "5511900000000"}, &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("--confirmo com o numero ERRADO deveria RECUSAR")
	}

	check := storeFromEnvironment(t, vars)
	var remaining int
	if err := check.DB().QueryRow(`SELECT count(*) FROM transito WHERE contraparte = ?`, targetNumber).Scan(&remaining); err != nil {
		t.Fatalf("contar: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("as duas recusas nao deveriam ter apagado nada, restaram %d (quero 1)", remaining)
	}

	out.Reset()
	if err := logCommand([]string{"clear", "--telefone", targetNumber, "--confirmo", targetNumber}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("clear com --confirmo certo: %v", err)
	}

	check2 := storeFromEnvironment(t, vars)
	var remaining2 int
	if err := check2.DB().QueryRow(`SELECT count(*) FROM transito WHERE contraparte = ?`, targetNumber).Scan(&remaining2); err != nil {
		t.Fatalf("contar (2): %v", err)
	}
	if remaining2 != 0 {
		t.Fatalf("deveria estar vazio depois do clear, restaram %d", remaining2)
	}
}

// Verify (b): 🔴 two SYNTHETIC numbers with the SAME last eight digits
// and a different area code — `clear --telefone` has to REJECT, list
// both, and NO row can be deleted. This is the test that stops the
// command from deleting someone else's history; without the ambiguity
// guard from item 3 of the task, this test goes RED (the command would
// delete both rows).
func TestLogClearPhoneRefusesWhenTwoNumbersMatchOnTheLastEight(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	const numberA = "5511900000001" // synthetic — area code 11
	const numberB = "5532900000001" // synthetic — area code 32, SAME last 8 digits as A
	if meta.LastEightDigits(numberA) != meta.LastEightDigits(numberB) {
		t.Fatalf("fixture errada: A e B deveriam compartilhar os ultimos 8 digitos (A=%q B=%q)",
			meta.LastEightDigits(numberA), meta.LastEightDigits(numberB))
	}

	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound, Counterparty: numberA, Type: "mensagem", Correlation: "a", Outcome: "linha-A",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit A: %v", err)
	}
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound, Counterparty: numberB, Type: "mensagem", Correlation: "b", Outcome: "linha-B",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit B: %v", err)
	}

	var out bytes.Buffer
	// asks for the cleanup using A's spelling — it would match the same
	// with B's, because the last eight digits are the same. --confirmo
	// goes along (T-114): this test proves the AMBIGUITY guard, not the
	// --confirmo one — without the right --confirmo the command would
	// reject for a different reason before reaching the last-eight-digits
	// comparison.
	err := logCommand([]string{"clear", "--telefone", numberA, "--confirmo", numberA}, &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("clear --telefone deveria RECUSAR quando dois numeros distintos batem — apagaria o historico de outra pessoa")
	}
	if !strings.Contains(err.Error(), numberA) || !strings.Contains(err.Error(), numberB) {
		t.Fatalf("a recusa deveria LISTAR os dois numeros que bateram, veio: %v", err)
	}

	check := storeFromEnvironment(t, vars)
	var leftovers int
	if err := check.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "lojinha").Scan(&leftovers); err != nil {
		t.Fatalf("contar transito: %v", err)
	}
	if leftovers != 2 {
		t.Fatalf("a recusa deveria deixar as DUAS linhas intactas, restaram %d", leftovers)
	}
}

// Verify (c): `--telefone` with a UNIQUE target deletes its rows in ALL
// instances and reports the count PER instance — without touching rows
// from another number.
func TestLogClearPhoneDeletesInAllInstancesAndCountsPerInstance(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")
	createTestLogInstance(t, store, "outra")

	const targetNumber = "5511900000002" // synthetic
	const otherNumber = "5532900000009"  // synthetic, last 8 digits DIFFERENT from the target

	for i, corr := range []string{"alvo-1", "alvo-2"} {
		if err := store.WriteTransit(config.TransitRecord{
			Slug: "lojinha", Direction: config.DirectionInbound,
			Counterparty: targetNumber, Type: "mensagem", Correlation: corr, Outcome: fmt.Sprintf("linha-alvo-%d", i),
		}, time.Now()); err != nil {
			t.Fatalf("WriteTransit alvo lojinha %d: %v", i, err)
		}
	}
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "outra", Direction: config.DirectionInbound,
		Counterparty: targetNumber, Type: "mensagem", Correlation: "alvo-3", Outcome: "linha-alvo-outra",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit alvo outra: %v", err)
	}
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Counterparty: otherNumber, Type: "mensagem", Correlation: "nao-alvo", Outcome: "linha-preservada",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit nao-alvo: %v", err)
	}

	var out bytes.Buffer
	if err := logCommand([]string{"clear", "--telefone", targetNumber, "--confirmo", targetNumber}, &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("clear --telefone: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "lojinha") || !strings.Contains(text, "outra") {
		t.Fatalf("saida deveria informar a contagem POR INSTANCIA: %s", text)
	}

	check := storeFromEnvironment(t, vars)
	var remainingTarget, remainingOtherOne int
	if err := check.DB().QueryRow(`SELECT count(*) FROM transito WHERE contraparte = ?`, targetNumber).Scan(&remainingTarget); err != nil {
		t.Fatalf("contar alvo: %v", err)
	}
	if err := check.DB().QueryRow(`SELECT count(*) FROM transito WHERE contraparte = ?`, otherNumber).Scan(&remainingOtherOne); err != nil {
		t.Fatalf("contar nao-alvo: %v", err)
	}
	if remainingTarget != 0 {
		t.Fatalf("as 3 linhas do alvo deveriam ter sumido, restaram %d", remainingTarget)
	}
	if remainingOtherOne != 1 {
		t.Fatalf("a linha do OUTRO numero nao deveria ter sido tocada, restaram %d", remainingOtherOne)
	}
}

// Verify (d): neither of the two, or both together — a clear error,
// nothing deleted.
func TestLogClearCommandRequiresExactlyOneOfTheTwo(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound, Type: "mensagem", Correlation: "c", Outcome: "linha",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}

	var out bytes.Buffer
	if err := logCommand([]string{"clear"}, &out, fakeEnvironment(vars)); err == nil {
		t.Fatal("nenhum dos dois deveria ser erro")
	}
	out.Reset()
	if err := logCommand([]string{"clear", "--instancia", "lojinha", "--telefone", "5511900000000", "--confirmo", "lojinha"},
		&out, fakeEnvironment(vars)); err == nil {
		t.Fatal("os dois juntos deveria ser erro")
	}

	check := storeFromEnvironment(t, vars)
	var leftovers int
	if err := check.DB().QueryRow(`SELECT count(*) FROM transito WHERE slug = ?`, "lojinha").Scan(&leftovers); err != nil {
		t.Fatalf("contar transito: %v", err)
	}
	if leftovers != 1 {
		t.Fatalf("nada deveria ter sido apagado, restaram %d", leftovers)
	}
}

// Verify (e): 🔴 THE FOLLOW SURVIVES THE CLEAR. With a `zapgw log`
// following, deletes everything (the highest rowid disappears — the
// `transito` table does not declare AUTOINCREMENT, so SQLite REUSES
// rowids), records a NEW row (which is born with a low, reused rowid),
// and proves it APPEARS. Without the fix from item 5 of the task
// (runLog resets the cursor when HighestLogRowid < lastRowid), this
// test goes RED: the follow filters `rowid > old-cursor` forever and the
// new row never appears.
func TestLogFollowSurvivesLogClear(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	// initial rows so the maximum rowid starts HIGH — this way, after the
	// clear and a new insert, the reused rowid ends up LESS THAN OR EQUAL
	// to the cursor the follow had already seen.
	for i := 0; i < 5; i++ {
		if err := store.WriteTransit(config.TransitRecord{
			Slug: "lojinha", Direction: config.DirectionInbound,
			Type: "mensagem", Correlation: fmt.Sprintf("antes-%d", i), Outcome: fmt.Sprintf("antes-%d", i),
		}, time.Now()); err != nil {
			t.Fatalf("WriteTransit antes %d: %v", i, err)
		}
	}

	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runLog(ctx, store, "", defaultLogRows, out, 5*time.Millisecond)
	}()

	// waits for the initial block to appear — the follow already has
	// cursor = rowid 5 before the clear.
	deadline := time.After(2 * time.Second)
	for !strings.Contains(out.String(), "antes-4") {
		select {
		case <-deadline:
			t.Fatalf("bloco inicial nao apareceu em 2s:\n%s", out.String())
		case <-time.After(5 * time.Millisecond):
		}
	}

	// deletes EVERYTHING — the highest rowid (5) disappears, and the next
	// insert starts counting from zero again (table with no
	// AUTOINCREMENT).
	if _, err := store.ClearInstanceTransit("lojinha"); err != nil {
		t.Fatalf("ClearInstanceTransit: %v", err)
	}

	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "depois", Outcome: "linha-depois-do-clear",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit depois: %v", err)
	}

	deadline = time.After(2 * time.Second)
	for !strings.Contains(out.String(), "linha-depois-do-clear") {
		select {
		case <-deadline:
			t.Fatalf("follow ficou CEGO apos o clear — linha-depois-do-clear nao apareceu em 2s:\n%s", out.String())
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runLog: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLog nao retornou apos o cancelamento — o teste pendurou")
	}
}

// Complement to Verify (e), requested by the owner after reviewing the
// fix: `largest-1` (instead of `largest`) has an ACCEPTED side effect — it
// can REPEAT a row already shown (see the reset comment in runLog,
// cmd/zapgw/log.go). This test pins down what MATTERS — no NEW row is
// lost after a PARTIAL clear that removes precisely the most recent
// rows — without locking onto an exact repetition count (the fix's own
// comment already documents the scenario and the price).
//
// Scenario: 7 rows of one number, THEN 3 rows of another number (which
// end up with the HIGHEST rowids). The follow sees all 10. A `clear
// --telefone` (via ClearTransitByPhone) deletes only the 3 most
// recent — the table's maximum rowid DROPS from 10 to 7, without the
// table becoming empty (the original T-096 only tested the "delete
// everything" case). A new row recorded afterward has to appear.
func TestLogFollowDoesNotLoseANewRowAfterPartialClearThatRemovesTheMostRecent(t *testing.T) {
	vars := testEnvironment(t)
	store := storeFromEnvironment(t, vars)
	createTestLogInstance(t, store, "lojinha")

	const numberA = "5511900000030"      // synthetic — ends up with rowids 1..7, PRESERVED by the clear
	const targetNumber = "5511900000040" // synthetic — ends up with rowids 8..10, the ones the clear removes

	for i := 0; i < 7; i++ {
		if err := store.WriteTransit(config.TransitRecord{
			Slug: "lojinha", Direction: config.DirectionInbound, Counterparty: numberA,
			Type: "mensagem", Correlation: fmt.Sprintf("a-%d", i), Outcome: fmt.Sprintf("preservada-%d", i),
		}, time.Now()); err != nil {
			t.Fatalf("WriteTransit preservada %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := store.WriteTransit(config.TransitRecord{
			Slug: "lojinha", Direction: config.DirectionInbound, Counterparty: targetNumber,
			Type: "mensagem", Correlation: fmt.Sprintf("alvo-%d", i), Outcome: fmt.Sprintf("alvo-%d", i),
		}, time.Now()); err != nil {
			t.Fatalf("WriteTransit alvo %d: %v", i, err)
		}
	}

	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runLog(ctx, store, "", defaultLogRows, out, 5*time.Millisecond)
	}()

	// waits for the complete initial block (all 10 rows) — the follow
	// already has cursor = rowid 10 before the partial clear.
	deadline := time.After(2 * time.Second)
	for !strings.Contains(out.String(), "alvo-2") {
		select {
		case <-deadline:
			t.Fatalf("bloco inicial nao apareceu em 2s:\n%s", out.String())
		case <-time.After(5 * time.Millisecond):
		}
	}

	// PARTIAL clear: only the 3 rows of the target number (rowids 8,9,10)
	// disappear — the 7 rows of numberA (rowids 1..7) stay in the table.
	// The maximum rowid drops from 10 to 7, BUT the table does not become
	// empty.
	deleted, err := store.ClearTransitByPhone(targetNumber)
	if err != nil {
		t.Fatalf("ClearTransitByPhone: %v", err)
	}
	var totalDeleted int64
	for _, a := range deleted {
		totalDeleted += a.Rows
	}
	if totalDeleted != 3 {
		t.Fatalf("fixture errada: deveria ter apagado 3 linhas do alvo, apagou %d", totalDeleted)
	}

	// the NEW row is born with rowid 8 (current max 7 + 1) — LESS than
	// the old cursor (10). Without the fix from item 5, the follow would
	// filter `rowid > 10` forever and this row would never appear.
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Type: "mensagem", Correlation: "nova", Outcome: "linha-nova-apos-clear-parcial",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit nova: %v", err)
	}

	deadline = time.After(2 * time.Second)
	for !strings.Contains(out.String(), "linha-nova-apos-clear-parcial") {
		select {
		case <-deadline:
			t.Fatalf("follow perdeu a linha NOVA apos um clear PARCIAL — nao apareceu em 2s:\n%s", out.String())
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runLog: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLog nao retornou apos o cancelamento — o teste pendurou")
	}
}
