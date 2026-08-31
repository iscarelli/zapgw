package outbound

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// alwaysFailingOutboundTransitStore is the double of config.TransitStore
// that fails on EVERY call — it proves, against the REAL sending handler,
// that a failure writing the transit log never changes the status returned
// to the consumer (T-091, Verify (c)). Same role as alwaysFailingOutboundCounter
// just above in this test file.
type alwaysFailingOutboundTransitStore struct{ calls int }

func (f *alwaysFailingOutboundTransitStore) WriteTransit(config.TransitRecord, time.Time) error {
	f.calls++
	return errors.New("alwaysFailingOutboundTransitStore: falha proposital de teste")
}

// TestOutboundTransitDoesNotStoreTheIdempotencyKeyInTheClear is the FIX after review
// of Verify (b): the first version wrote the RAW `Idempotency-Key` into the
// `correlacao` column on the OUTBOUND side — free-form text from an
// EXTERNAL origin, exactly what the field list in
// internal/config/transito.go forbids. This test sends a SENTINEL
// Idempotency-Key (with a "phone number" embedded, to look like a real
// value a reasonable consumer would choose) and sweeps EVERY column of
// EVERY row written, like TestTransitoNaoGuardaConteudo already does on the
// inbound side (internal/inbound/transito_test.go).
//
// STILL HOLDS AFTER T-094 (2026-07-30) WITHOUT CHANGING A LINE: the owner's
// decision was about the PHONE NUMBER, not about the Idempotency-Key — it's
// still HMAC (Store.HMACCorrelation), and that's why the sentinel embedded
// inside it (the "phone number" in the middle of the string, deliberately
// resembling real data) must still NEVER appear in plaintext in any column.
func TestOutboundTransitDoesNotStoreTheIdempotencyKeyInTheClear(t *testing.T) {
	const sentinelKey = "SENTINELA-NAO-PODE-APARECER-5532999990000"

	srv := acceptingMeta("wamid.SENTINELA")
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20,
		config.NewCounter(store), config.NewTransit(store), AllTypes)

	rec := ask(t, h, "token-do-a", sentinelKey, textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s — quero 200", rec.Code, rec.Body.String())
	}

	rows, err := store.DB().Query(`SELECT * FROM transito WHERE slug = 'lojinha'`)
	if err != nil {
		t.Fatalf("consultar transito: %v", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("listar colunas: %v", err)
	}

	nRows := 0
	for rows.Next() {
		nRows++
		// GENERIC Scan, like the sibling test on the inbound side: sweeps
		// ANY column present in the table today, not just the ones this
		// test expects.
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("ler linha de transito: %v", err)
		}
		for i, v := range values {
			text := fmt.Sprintf("%v", v)
			if strings.Contains(text, sentinelKey) {
				t.Fatalf("coluna %q da linha de transito contem a Idempotency-Key em claro — vazou para o log de transito",
					columns[i])
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar transito: %v", err)
	}
	if nRows == 0 {
		t.Fatal("nenhuma linha de transito foi gravada — o teste nao exercitou o caminho que ele prova")
	}
}

// TestHandlerFalhaDeTransitoNaoMudaOStatusDoEnvio is T-091's Verify (c) on
// the OUTBOUND side: on the SUCCESS path (Meta accepted the send), a
// failure writing the transit log cannot prevent the 200 nor the
// wa_message_id from reaching the consumer.
func TestHandlerTransitFailureDoesNotChangeTheStatusOfASuccessfulSend(t *testing.T) {
	srv := acceptingMeta("wamid.OK")
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	fake := &alwaysFailingOutboundTransitStore{}
	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20,
		config.NewCounter(store), config.NewTransitWithStore(fake), AllTypes)

	rec := ask(t, h, "token-do-a", "k-transito-falha-sucesso", textBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s — falha do TRANSITO nao pode mudar a resposta do ENVIO",
			rec.Code, rec.Body.String())
	}
	if fake.calls == 0 {
		t.Fatal("o transito que sempre erra nunca foi chamado — o teste nao exercitou o caminho que ele prova")
	}
}

// TestHandlerTransitFailureDoesNotChangeTheStatusOfAFailedSend is the SAME
// Verify (c), on the path where Meta REFUSES the send — to prove that the
// error branch also writes AFTER responding, and not before.
func TestHandlerTransitFailureDoesNotChangeTheStatusOfAFailedSend(t *testing.T) {
	// Meta's 401 -> class "config" -> 502 for the consumer (same table
	// as TestHandlerTranslatesTheErrorClassIntoAStatus).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"x","code":1}}`))
	}))
	defer srv.Close()
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	fake := &alwaysFailingOutboundTransitStore{}
	h := NewHandler(store, NewAuthenticator(store), meta.NewClient(srv.Client(), srv.URL), 1<<20,
		config.NewCounter(store), config.NewTransitWithStore(fake), AllTypes)

	rec := ask(t, h, "token-do-a", "k-transito-falha-erro", textBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, corpo = %s — quero 502 (credencial recusada), inalterado pela falha do TRANSITO",
			rec.Code, rec.Body.String())
	}
	if fake.calls == 0 {
		t.Fatal("o transito que sempre erra nunca foi chamado — o teste nao exercitou o caminho que ele prova")
	}
}
