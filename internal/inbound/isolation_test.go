// Tenant isolation with TWO tenants in the database (T-086).
//
// WHY A NEW FILE, and not more tests in handler_test.go: the isolation
// tests that already existed there prove the GUARD — a payload with a
// non-matching phone_number_id/waba_id is rejected, and the right counter
// goes up. They prove this with ONE instance in the database, so the
// question they CANNOT answer is the one this file exists to answer:
//
//	does tenant A's content reach tenant B's CONSUMER?
//
// With a single instance, "wasn't delivered" is the only observable thing;
// with two, each with ITS OWN consumer, it's possible to look at the safe
// instead of the door — who received bytes, and whether those bytes are the
// other tenant's.
//
// THE DIFFERENCE IS WHAT THE MUTATION MEASURES. Removing the
// phone_number_id check (step 5a) or the waba_id one (5b) from
// internal/inbound/handler.go makes this file's tests go red NAMING the
// leak: B's consumer received an envelope whose decoded `cru` carries A's
// payload. A test that only looked at the HTTP status would say "expected
// 200, got 200" and pass.
//
// NO INSTANCE HERE IS A REAL CONSUMER'S — the slugs, the ids and the
// secrets are synthetic, and so is the phone number in the payloads
// (CLAUDE.md, the hard rule about the phone number in the repository). It
// was promised in writing to the consumer that the proof would use a test
// instance.
package inbound

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
)

// The two instances in the test database. The values are DISTINCT across
// every axis on purpose: slug, waba_id, phone_number_id and app_secret. Two
// fields sharing the same value would let a swapped read pass green — the
// same trap docs/ARMADILHAS.md records about a fixture with identical
// neighboring fields.
const (
	slugA = "inquilino-a"
	slugB = "inquilino-b"

	wabaA = "WABA-DO-A"
	wabaB = "WABA-DO-B"

	pnidA = "PNID-DO-A"
	pnidB = "PNID-DO-B"

	secretA = "app-secret-do-a"
	secretB = "app-secret-do-b"

	// markerOfA shows up INSIDE every one of tenant A's payloads and nowhere
	// else. It's what the leak search in the delivered body looks for: the
	// envelope carries `cru` in base64 (deliver.go), so searching for the
	// marker in the raw POST body without decoding it would find zero even
	// with the leak actually happening — a search unable to find what it's
	// looking for has already cost this project (docs/ARMADILHAS.md,
	// "eu medi a forma do invólucro e concluí sobre a existência do
	// conteúdo").
	markerOfA = "SO-DO-INQUILINO-A"
)

// spyConsumer stores the bodies it received, so the test can ask "what
// arrived here?" instead of just "did anything arrive?".
type spyConsumer struct {
	srv *httptest.Server

	mu     sync.Mutex
	bodies [][]byte
}

func newSpyConsumer(t *testing.T) *spyConsumer {
	t.Helper()
	c := &spyConsumer{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *spyConsumer) receivedBodies() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.bodies...)
}

// leak looks for tenant A's marker in the bodies this consumer
// received, decoding the envelope's `cru`.
//
// Returns a description of what it found, or "" if nothing matched. The
// SAME function serves the negative tests and the positive control — and
// that's deliberate: absence only counts as proof after the method has
// been exercised against a known positive (docs/ARMADILHAS.md).
func (c *spyConsumer) leak(marker string) string {
	for i, body := range c.receivedBodies() {
		var env Envelope
		if err := json.Unmarshal(body, &env); err != nil {
			// A body that isn't an envelope is still a delivery: report it as is.
			if strings.Contains(string(body), marker) {
				return "corpo " + strconv.Itoa(i) + " (nao desserializa como envelope) contem " + marker
			}
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(env.Raw)
		if err == nil && strings.Contains(string(raw), marker) {
			return "envelope com instancia=" + env.Instance + " cujo `cru` decodificado contem " + marker
		}
		if strings.Contains(string(body), marker) {
			return "envelope com instancia=" + env.Instance + " cujo corpo contem " + marker
		}
	}
	return ""
}

// twoTenants builds the database with BOTH instances active, each
// delivering to ITS OWN consumer.
func twoTenants(t *testing.T) (http.Handler, string, *spyConsumer, *spyConsumer) {
	t.Helper()

	consumerA := newSpyConsumer(t)
	consumerB := newSpyConsumer(t)

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

	create := func(slug, waba, pnid, secret, callback string) {
		t.Helper()
		err := store.CreateInstance(config.Instance{
			Slug: slug, WabaID: waba, PhoneNumberID: pnid,
			AppSecret: secret, VerifyToken: "vt-" + slug, SendToken: "te-" + slug,
			CallbackURL: callback, DeliverySecret: "se-" + slug, TimeoutMs: 2000,
		})
		if err != nil {
			t.Fatalf("CreateInstance %q: %v", slug, err)
		}
	}
	create(slugA, wabaA, pnidA, secretA, consumerA.srv.URL)
	create(slugB, wabaB, pnidB, secretB, consumerB.srv.URL)

	h := NewHandler(store, NewDeliverer(nil), 1<<20, config.NewCounter(store), config.NewTransit(store))
	activateInstanceInFile(t, path, slugA)
	activateInstanceInFile(t, path, slugB)
	return h, path, consumerA, consumerB
}

// messageFromA is a legitimate MESSAGE webhook for tenant A: both ids are
// its own, and the text carries the marker. It's the payload A's Meta App
// produces — nothing in it is forged for the test beyond the content.
//
// The phone number is SYNTHETIC (CLAUDE.md).
func messageFromA() []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"` + wabaA + `","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"` + pnidA + `"},
	   "messages":[{"from":"5511900000001","id":"wamid.` + markerOfA + `","timestamp":"1769000000",
	                "type":"text","text":{"body":"consulta da cliente do ` + markerOfA + `"}}]}}]}]}`)
}

// accountOfA is an ACCOUNT webhook for tenant A. It has NO phone_number_id —
// Meta doesn't send one in this format —, so the only routing key is
// `entry[].id`, and the one that has to reject it is guard 5b.
func accountOfA() []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"` + wabaA + `","time":1751247548,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"APPROVED","message_template_id":1689556908129832,
	    "message_template_name":"campanha-` + markerOfA + `","message_template_language":"pt_BR",
	    "reason":"NONE","message_template_category":"MARKETING"}}]}]}`)
}

func deliverTo(t *testing.T, h http.Handler, slug string, raw []byte, secret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbound/"+slug, strings.NewReader(string(raw)))
	req.Header.Set("X-Hub-Signature-256", signAsMeta(raw, secret))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// requireNothingLeaked is the assertion this task exists to create: not "got a
// 403," but "A's content reached no one."
func requireNothingLeaked(t *testing.T, a, b *spyConsumer) {
	t.Helper()
	if v := b.leak(markerOfA); v != "" {
		t.Fatalf("VAZAMENTO ENTRE INQUILINOS: o consumidor de %q recebeu conteudo de %q — %s",
			slugB, slugA, v)
	}
	if n := len(b.receivedBodies()); n != 0 {
		t.Fatalf("o consumidor de %q recebeu %d entrega(s) que nao deveriam existir", slugB, n)
	}
	if n := len(a.receivedBodies()); n != 0 {
		t.Fatalf("o consumidor de %q recebeu %d entrega(s) — o gateway REAPONTOU o lote em vez de recusar", slugA, n)
	}
}

// POSITIVE CONTROL, and it comes FIRST on purpose.
//
// Without it, the three tests below would prove "nothing reached B's
// consumer" with an apparatus unable to see any delivery at all — and
// absence measured by a search that finds nothing is the error
// docs/ARMADILHAS.md records as the most expensive of this family. Here the
// SAME `leak` function the others use to require "" has to find the
// marker.
func TestPositiveControlPayloadOfAReachesConsumerOfA(t *testing.T) {
	h, _, a, b := twoTenants(t)

	raw := messageFromA()
	if rec := deliverTo(t, h, slugA, raw, secretA); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 no caminho da PROPRIA instancia; corpo = %s", rec.Code, rec.Body.String())
	}

	if v := a.leak(markerOfA); v == "" {
		t.Fatal("o consumidor de A NAO recebeu o payload de A — o aparato deste arquivo nao enxerga entrega," +
			" e por isso nenhum dos testes de ausencia abaixo valeria como prova")
	}
	if n := len(b.receivedBodies()); n != 0 {
		t.Fatalf("o consumidor de %q recebeu %d entrega(s) de um lote que era do %q", slugB, n, slugA)
	}
}

// AXIS 0 — the signature, which is what actually separates tenants when
// each consumer has its own App (today's design). A's payload delivered to
// B's path, signed with A's OWN app_secret, doesn't even reach guards
// 5a/5b: step 3 rejects it with 403 because the path's secret is a
// different one.
//
// This test does NOT go red with the 5a/5b mutations, and that's
// information, not a gap: it measures a different defense. The two below
// are the ones that measure the guards.
func TestPayloadOfAOnPathOfBSignedBYADoesNotPassTheSignature(t *testing.T) {
	h, path, a, b := twoTenants(t)

	rec := deliverTo(t, h, slugB, messageFromA(), secretA)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quero 403 — assinatura de outro App nao vale neste caminho; corpo = %s",
			rec.Code, rec.Body.String())
	}
	requireNothingLeaked(t, a, b)
	// No counter moves: the rejection happens before any counting.
	for _, key := range []string{config.CounterNumberDiscarded, config.CounterAccountDiscarded, config.CounterReceived} {
		if n := directCount(t, path, slugB, key); n != 0 {
			t.Errorf("%s de %q = %d, quero 0", key, slugB, n)
		}
	}
}

// AXIS 1 — phone_number_id (guard 5a).
//
// THE REAL SCENARIO this test models, and why the signature is B's: two
// numbers under the SAME App, or a repointed Callback URL. In these cases
// Meta signs with the secret of the App that serves the path — B's — and
// the body is still A's. The signature checks out, step 3 passes, and the
// ONLY thing standing between A's content and B's consumer is guard 5a.
//
// MANDATORY MUTATION (T-086, done and reverted before the commit): removing
// the phone_number_id check from step 5a makes this test go red on
// `VAZAMENTO ENTRE INQUILINOS`, citing the envelope with
// `instancia=inquilino-b` whose decoded `cru` carries A's marker.
func TestMessageFromAOnPathOfBDoesNotLEAKThroughPhoneNumberID(t *testing.T) {
	h, path, a, b := twoTenants(t)

	rec := deliverTo(t, h, slugB, messageFromA(), secretB)

	// 200 is this rejection's contract: redelivering would repeat the same
	// configuration mismatch for 36h, and the fix is a person.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	requireNothingLeaked(t, a, b)

	if n := directCount(t, path, slugB, config.CounterNumberDiscarded); n != 1 {
		t.Errorf("numero_descartado de %q = %d, quero 1 — a recusa tem de ser visivel em `zapgw estado`", slugB, n)
	}
	// The NEIGHBORING axis's key stays put: whoever reads the table needs
	// to know WHICH guard rejected it, otherwise they end up checking both
	// places every time.
	if n := directCount(t, path, slugB, config.CounterAccountDiscarded); n != 0 {
		t.Errorf("conta_descartada de %q = %d, quero 0 — quem recusou foi a guarda 5a", slugB, n)
	}
	// And tenant A cannot be charged for a webhook that never even went
	// through its own path: a counter on the wrong instance is the
	// numeric version of the same leak.
	for _, key := range []string{config.CounterNumberDiscarded, config.CounterAccountDiscarded, config.CounterReceived} {
		if n := directCount(t, path, slugA, key); n != 0 {
			t.Errorf("%s de %q = %d, quero 0 — nada aconteceu no caminho do %q", key, slugA, n, slugA)
		}
	}
}

// AXIS 2 — waba_id (guard 5b), with B's signature for the SAME reason as axis 1.
//
// An ACCOUNT webhook has no phone_number_id at all, so guard 5a sweeps zero
// events and rejects nothing: without 5b, the raw body of an account that
// isn't B's reaches B's consumer with `instancia` saying it is — and the
// consumer WRITES it, because the only guard it has checks the envelope's
// slug against its own.
//
// MANDATORY MUTATION (T-086, done and reverted before the commit): removing
// the waba_id check from step 5b makes this test go red on `VAZAMENTO
// ENTRE INQUILINOS`.
func TestAccountWebhookOfAOnPathOfBDoesNotLEAKThroughWabaID(t *testing.T) {
	h, path, a, b := twoTenants(t)

	rec := deliverTo(t, h, slugB, accountOfA(), secretB)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	requireNothingLeaked(t, a, b)

	if n := directCount(t, path, slugB, config.CounterAccountDiscarded); n != 1 {
		t.Errorf("conta_descartada de %q = %d, quero 1", slugB, n)
	}
	if n := directCount(t, path, slugB, config.CounterNumberDiscarded); n != 0 {
		t.Errorf("numero_descartado de %q = %d, quero 0 — quem recusou foi a guarda 5b", slugB, n)
	}
	for _, key := range []string{config.CounterNumberDiscarded, config.CounterAccountDiscarded, config.CounterReceived} {
		if n := directCount(t, path, slugA, key); n != 0 {
			t.Errorf("%s de %q = %d, quero 0 — nada aconteceu no caminho do %q", key, slugA, n, slugA)
		}
	}
}
