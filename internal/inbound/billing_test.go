package inbound

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// inboundCorpusDir points at the SAME corpus that internal/meta exercises.
//
// WHY A HANDLER TEST READS THE CORPUS: the two claims that decide this task
// are about REAL payloads — "the same wamid arrives in `sent` and in
// `delivered`, both with pricing" and "4 of 53 `sent` came without pricing."
// A payload typed here would only prove the counter agrees with what I
// wrote; the corpus proves it agrees with what Meta SENT.
const inboundCorpusDir = "../../testdata/corpus"

func corpus(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(inboundCorpusDir, name))
	if err != nil {
		t.Fatalf("ler corpus %q: %v", name, err)
	}
	return b
}

// eventsOfPayload runs the REAL parser over the payload — this is what lets
// BillingKeys be exercised without HTTP while, at the same time,
// avoiding inventing a meta.Event by hand (an Event built in the test
// would only prove the function agrees with what I wrote, not with what the
// parser produces).
func eventsOfPayload(t *testing.T, raw []byte) []meta.Event {
	t.Helper()
	evs, err := meta.ParseWebhook(raw)
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	return evs
}

// syntheticCorpusStatus builds a status webhook in the SAME envelope as
// the capture files (WABA_TESTE/PNID_TESTE), with the `pricing` block chosen
// by the test.
//
// It exists because the real capture doesn't cover everything the counter
// needs to distinguish: the corpus only has `sent` with
// `service`/`billable:false`. The other three categories and the
// `billable:true` case on a `sent` were never observed HERE, and inventing a
// corpus file for them would claim Meta sent something no one ever saw
// (testdata/corpus/README.md: a derived fixture must not look like a
// capture). Synthetic, inside the test, tells the truth about its own
// origin.
func syntheticCorpusStatus(status, wamid, pricing string) []byte {
	block := ""
	if pricing != "" {
		block = `,"pricing":` + pricing
	}
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA_TESTE","changes":[
	  {"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"PNID_TESTE"},
	   "statuses":[{"id":"` + wamid + `","status":"` + status + `","timestamp":"1785072102",` +
		`"recipient_id":"553288888888"` + block + `}]}}]}]}`)
}

// deliverToCorpusHandler spins up a consumer that answers with
// `consumerStatus`, sends the signed payload to the "lojinha" instance
// (identified with the corpus's own ids) and returns the database path so
// the counts can be read.
func deliverToCorpusHandler(t *testing.T, raw []byte, consumerStatus int) string {
	t.Helper()
	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(consumerStatus)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithIdentity(t, consumer.URL, 1<<20, "WABA_TESTE", "PNID_TESTE")
	activateInstanceInFile(t, path, "lojinha")

	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	h.ServeHTTP(httptest.NewRecorder(), req)
	return path
}

// vocabularyBillingKeys is the subset of
// config.KeysInDisplayOrder that talks about billing, derived by
// PREFIX and never written by hand.
//
// It's the same rule T-048 left written down: every list built over a
// vocabulary needs, alongside it, something that asks the vocabulary
// itself. A new billing key only goes into internal/config/counter.go and
// is automatically required by "and ONLY it"; a list here would silently
// go stale, which is exactly the defect T-039 fixed one surface up.
func vocabularyBillingKeys() []string {
	var keys []string
	for _, key := range config.KeysInDisplayOrder {
		if strings.HasPrefix(key, "cobranca_") {
			keys = append(keys, key)
		}
	}
	return keys
}

// requireBilling checks ALL billing keys: the ones the case expects and,
// implicitly, the ones it expects to stay at zero. It's the "and ONLY it"
// from T-063's Verify — without it, a counter that incremented two columns
// per event would pass green.
func requireBilling(t *testing.T, path string, want map[string]int) {
	t.Helper()
	for _, key := range vocabularyBillingKeys() {
		if n := directCount(t, path, "lojinha", key); n != want[key] {
			t.Errorf("%s = %d, quero %d", key, n, want[key])
		}
	}
}

// The heart of T-063: the category Meta billed turns into a counter, and
// ONLY the right key moves.
//
// THE FIRST TWO LINES ARE A REAL CAPTURE and carry the mandatory mutation:
// the corpus's `sent` came as `service` + `billable:false` and the `read`
// came as `utility` + `billable:true`. They're the two fields with values
// that DIFFER, and that's why swapping the reading of one for the other
// doesn't go unnoticed — a counter that decided `cobranca_cobravel` by
// looking at the category (instead of `billable`) would mark the `service`
// line as billed.
func TestCountsBillingCategoryAndOnlyTheRightKey(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want map[string]int
	}{
		{
			// CAPTURE (consumer-a, T-069): `service` with `billable:false`.
			// The category counts; the billable column does NOT — Meta said
			// it doesn't charge, and absence of billing cannot turn into
			// billing.
			name: "sent real, service, nao cobravel",
			raw:  corpus(t, "status_sent_com_pricing.json"),
			want: map[string]int{config.CounterBillingService: 1},
		},
		{
			// CAPTURE (consumer-a, T-069): 4 of 53 real `sent` carry no
			// `pricing`. It is not an error; it's ~7.5% of the volume, and
			// without its own key it would silently vanish from the
			// measurement.
			name: "sent real sem pricing vai para ausente",
			raw:  corpus(t, "status_sent_sem_pricing.json"),
			want: map[string]int{config.CounterBillingAbsent: 1},
		},
		{
			name: "marketing cobravel",
			raw:  syntheticCorpusStatus("sent", "wamid.SINT01", `{"billable":true,"pricing_model":"PMP","category":"marketing","type":"regular"}`),
			want: map[string]int{config.CounterBillingMarketing: 1, config.CounterBillingBillable: 1},
		},
		{
			name: "utility cobravel",
			raw:  syntheticCorpusStatus("sent", "wamid.SINT02", `{"billable":true,"pricing_model":"PMP","category":"utility","type":"regular"}`),
			want: map[string]int{config.CounterBillingUtility: 1, config.CounterBillingBillable: 1},
		},
		{
			name: "authentication cobravel",
			raw:  syntheticCorpusStatus("sent", "wamid.SINT03", `{"billable":true,"pricing_model":"PMP","category":"authentication","type":"regular"}`),
			want: map[string]int{config.CounterBillingAuthentication: 1, config.CounterBillingBillable: 1},
		},
		{
			// A MISSING `billable` is not `false`, and much less `true`:
			// the category counts, the billable column doesn't. Without
			// this case, a counter that treated absence as billed would
			// pass green.
			name: "marketing sem billable nao conta cobravel",
			raw:  syntheticCorpusStatus("sent", "wamid.SINT04", `{"pricing_model":"PMP","category":"marketing","type":"regular"}`),
			want: map[string]int{config.CounterBillingMarketing: 1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := deliverToCorpusHandler(t, c.raw, http.StatusOK)
			requireBilling(t, path, c.want)
		})
	}
}

// 🔴 The test that stops the invoice from being multiplied by three.
//
// The SAME send shows up in `sent`, `delivered`, and `read`, and all three
// can carry `pricing` — this corpus's `status_sent_com_pricing.json` and
// `status_delivered.json` are the capture of the SAME wamid, with the SAME
// category. If any status besides `sent` counted, the number the consumer
// multiplies by the rate would come out up to 3x higher than reality, and
// it would look like measurement.
func TestOnlySentCountsBilling(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		// Capture: same wamid and same category as the `sent` in the table above.
		{"delivered real do MESMO wamid do sent", corpus(t, "status_delivered.json")},
		// Capture: `read` carries its own `pricing` (`utility`, `billable:true`).
		{"read real com pricing", corpus(t, "status_read_com_cobranca.json")},
		// `failed` has no pricing — a cheap non-regression check, because a
		// counter that counted "every status" would send it to
		// `cobranca_ausente`.
		{"failed real", corpus(t, "status_failed.json")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := deliverToCorpusHandler(t, c.raw, http.StatusOK)
			requireBilling(t, path, map[string]int{})
			// Non-regression: the event ARRIVED (it wasn't rejected by any
			// guard) — otherwise this test would pass green for the wrong
			// reason.
			if n := directCount(t, path, "lojinha", config.CounterReceived); n != 1 {
				t.Errorf("recebidas = %d, quero 1 — o lote tem de ter chegado ate a entrega", n)
			}
		})
	}
}

// Unknown category: counts in `cobranca_outra` AND the literal value comes
// out in the log. Silently discarding it would mean losing money without
// knowing; creating a new key from the received value would let Meta choose
// the keys on our dashboard.
func TestUnknownCategoryGoesToOtherAndLogsTheValue(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	raw := syntheticCorpusStatus("sent", "wamid.SINT05",
		`{"billable":true,"pricing_model":"PMP","category":"engajamento_novo","type":"regular"}`)
	path := deliverToCorpusHandler(t, raw, http.StatusOK)

	requireBilling(t, path, map[string]int{
		config.CounterBillingOther:    1,
		config.CounterBillingBillable: 1,
	})

	output := buf.String()
	if !strings.Contains(output, "engajamento_novo") {
		t.Errorf("o log nao traz o valor literal da categoria — e ele que diz QUAL chave criar: %s", output)
	}
	if !strings.Contains(output, "ALARME") {
		t.Errorf("o aviso saiu sem ALARME — so gente acrescenta a chave nova: %s", output)
	}
}

// The warning fires ONCE per (slug, category) per process: a new category
// would generate one line per MESSAGE, and a repeated alarm trains whoever
// operates it to ignore it. The counter keeps counting all of them.
func TestUnknownCategoryWarnsOnceButAlwaysCounts(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithIdentity(t, consumer.URL, 1<<20, "WABA_TESTE", "PNID_TESTE")
	activateInstanceInFile(t, path, "lojinha")

	for i, wamid := range []string{"wamid.SINT06", "wamid.SINT07", "wamid.SINT08"} {
		raw := syntheticCorpusStatus("sent", wamid,
			`{"billable":true,"pricing_model":"PMP","category":"engajamento_novo","type":"regular"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
		req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("evento %d: status = %d, quero 200", i, rec.Code)
		}
	}

	if n := strings.Count(buf.String(), "engajamento_novo"); n != 1 {
		t.Errorf("o aviso saiu %d vezes, quero 1 — alarme repetido vira ruido e some junto com o que importa", n)
	}
	requireBilling(t, path, map[string]int{
		config.CounterBillingOther:    3,
		config.CounterBillingBillable: 3,
	})
}

// The unknown-category warning is SHARED STATE between goroutines:
// http.Server serves each request in its own goroutine over the SAME
// handler, and this project has already had a Critical exactly because of
// this (`seq++`, docs/ARMADILHAS.md). A sequential test doesn't reveal a
// race — this one runs under `-race` with N goroutines hitting the same
// instance, and requires the same outcome: ONE warning, N counts.
func TestUnknownCategoryWarningUnderConcurrency(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	log.SetOutput(lockingWriter{&buf, &mu})
	defer log.SetOutput(os.Stderr)

	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, path := testHandlerAndDBWithIdentity(t, consumer.URL, 1<<20, "WABA_TESTE", "PNID_TESTE")
	activateInstanceInFile(t, path, "lojinha")

	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw := syntheticCorpusStatus("sent", "wamid.CONC"+strconv.Itoa(i),
				`{"billable":true,"pricing_model":"PMP","category":"engajamento_novo","type":"regular"}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
			req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
			h.ServeHTTP(httptest.NewRecorder(), req)
		}(i)
	}
	wg.Wait()

	mu.Lock()
	times := strings.Count(buf.String(), "engajamento_novo")
	mu.Unlock()
	if times != 1 {
		t.Errorf("o aviso saiu %d vezes sob concorrencia, quero 1", times)
	}
	requireBilling(t, path, map[string]int{
		config.CounterBillingOther:    n,
		config.CounterBillingBillable: n,
	})
}

// lockingWriter serializes writes to the test buffer. The `log` package
// already serializes ITS OWN calls, but the test READING the buffer races
// against them — and `-race` would flag that as the test's own race,
// hiding the one that actually matters.
type lockingWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (e lockingWriter) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buf.Write(p)
}

// A NON-2xx response to Meta is a request for REDELIVERY — and Meta
// redelivers (5 attempts in 9 s have already been measured in our own
// access log). Counting before accepting would add up the same `sent` once
// per attempt, and the error would show up precisely during a consumer
// incident.
func TestBillingDoesNotCountWhenMetaWillRedeliver(t *testing.T) {
	// Consumer 500 -> verdict 502 for Meta (internal/inbound/mirror.go).
	path := deliverToCorpusHandler(t, corpus(t, "status_sent_com_pricing.json"), http.StatusInternalServerError)
	requireBilling(t, path, map[string]int{})
}

// The other side: a consumer that REFUSES (4xx) makes Meta hear 200 — it
// never redelivers again, and the billing happened the same way anyway.
// Counting here is mandatory, otherwise an instance with a refusing
// consumer would have a real cost and zero measurement.
func TestBillingCountsWhenConsumerRefusesAndMetaHears200(t *testing.T) {
	path := deliverToCorpusHandler(t, corpus(t, "status_sent_com_pricing.json"), http.StatusBadRequest)
	requireBilling(t, path, map[string]int{config.CounterBillingService: 1})
}

// T-035's guarantee applied to this count: a failure writing the counter
// does not change what Meta already heard. It lives in Register's
// SIGNATURE (it returns nothing), not in the caller's discipline — this
// test is what proves the new path is also covered by it.
func TestBillingCounterFailureDoesNotChangeStatusReturnedToMeta(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	h, broken := handlerWithAlwaysFailingCounter(t, consumer.URL)

	// That helper's own instance ids (WABA1/PNID1), not the corpus's: what
	// this test proves is Register's SIGNATURE, not the payload format.
	raw := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},"statuses":[
	    {"id":"wamid.Q1","status":"sent","timestamp":"1785072102","recipient_id":"553288888888",
	     "pricing":{"billable":true,"pricing_model":"PMP","category":"marketing","type":"regular"}}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/lojinha", strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, "app-secret-de-teste"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, quero 200 — contador quebrado nao pode mudar o que a Meta ouviu", rec.Code)
	}
	if broken.calls.Load() == 0 {
		t.Error("o contador nem foi chamado — o teste passaria verde pelo motivo errado")
	}
}

// The pure function, exercised without HTTP: a batch with SEVERAL statuses
// counts each `sent` once, and ignores the other states of the SAME send.
func TestBillingKeysOfABatchWithSeveralStatuses(t *testing.T) {
	evs := eventsOfPayload(t, []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA_TESTE","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID_TESTE"},"statuses":[
	    {"id":"wamid.L1","status":"sent","timestamp":"1785072102","recipient_id":"553288888888",
	     "pricing":{"billable":true,"pricing_model":"PMP","category":"marketing","type":"regular"}},
	    {"id":"wamid.L1","status":"delivered","timestamp":"1785072102","recipient_id":"553288888888",
	     "pricing":{"billable":true,"pricing_model":"PMP","category":"marketing","type":"regular"}},
	    {"id":"wamid.L2","status":"sent","timestamp":"1785072103","recipient_id":"553288888888",
	     "pricing":{"billable":false,"pricing_model":"PMP","category":"service","type":"free_customer_service"}},
	    {"id":"wamid.L3","status":"sent","timestamp":"1785072104","recipient_id":"553288888888"}
	  ]}}]}]}`))

	keys, unknown := BillingKeys(evs)
	want := map[string]int{
		config.CounterBillingMarketing: 1,
		config.CounterBillingBillable:  1,
		config.CounterBillingService:   1,
		config.CounterBillingAbsent:    1,
	}
	has := map[string]int{}
	for _, c := range keys {
		has[c]++
	}
	for key, n := range want {
		if has[key] != n {
			t.Errorf("%s = %d, quero %d (chaves: %v)", key, has[key], n, keys)
		}
	}
	if len(keys) != 4 {
		t.Errorf("len(chaves) = %d, quero 4 — o `delivered` do mesmo wamid nao pode contar: %v", len(keys), keys)
	}
	if len(unknown) != 0 {
		t.Errorf("desconhecidas = %v, quero vazio", unknown)
	}
}

// Every key BillingKeys can return MUST be in the closed vocabulary —
// otherwise IncrementCounter rejects it, only logs, and the count
// vanishes with nothing flagging it (mutation (4) of T-054 measured exactly
// this behavior).
func TestEveryBillingKeyIsInTheClosedVocabulary(t *testing.T) {
	cases := [][]byte{
		corpus(t, "status_sent_com_pricing.json"),
		corpus(t, "status_sent_sem_pricing.json"),
		syntheticCorpusStatus("sent", "wamid.V1", `{"billable":true,"category":"marketing"}`),
		syntheticCorpusStatus("sent", "wamid.V2", `{"billable":true,"category":"utility"}`),
		syntheticCorpusStatus("sent", "wamid.V3", `{"billable":true,"category":"authentication"}`),
		syntheticCorpusStatus("sent", "wamid.V4", `{"billable":true,"category":"nao_existe_ainda"}`),
	}
	inVocabulary := map[string]bool{}
	for _, key := range config.KeysInDisplayOrder {
		inVocabulary[key] = true
	}
	for _, raw := range cases {
		keys, _ := BillingKeys(eventsOfPayload(t, raw))
		for _, key := range keys {
			if !inVocabulary[key] {
				t.Errorf("chave %q nao esta em config.KeysInDisplayOrder — ela seria recusada e a contagem sumiria", key)
			}
		}
	}
}
