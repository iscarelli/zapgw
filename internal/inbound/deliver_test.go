package inbound

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

func testInst(callback string) config.Instance {
	return config.Instance{
		Slug:           "lojinha",
		CallbackURL:    callback,
		DeliverySecret: "segredo-entrega-de-teste",
		TimeoutMs:      2000,
	}
}

func TestDeliverSendsTheRawAndTheEventsTogether(t *testing.T) {
	// The consumer stores the RAW body before looking at the events. That's
	// what makes silent loss impossible on their side, in any language.
	var received Envelope
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	raw := []byte(`{"object":"whatsapp_business_account"}`)
	evs := []meta.Event{{Type: meta.EventTypeMessage, ID: "msg:wamid.A", Text: "oi"}}

	status, err := NewDeliverer(nil).
		Deliver(context.Background(), testInst(srv.URL), raw, evs, "", "corr-1", "")
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, quero 200", status)
	}
	if received.Raw != base64.StdEncoding.EncodeToString(raw) {
		t.Errorf("Raw = %q — nao veio o corpo exato em base64", received.Raw)
	}
	if len(received.Events) != 1 || received.Events[0].Text != "oi" {
		t.Errorf("Events = %+v", received.Events)
	}
	if received.Instance != "lojinha" {
		t.Errorf("Instance = %q", received.Instance)
	}
}

// capturedDelivery is what the consumer sees arrive: the EXACT bytes of the
// body and the two headers verification uses. A struct because the
// signature stopped being a function of the body alone — whoever verifies
// needs all three together.
type capturedDelivery struct {
	body      []byte
	timestamp string
	signature string
}

// captureDelivery spins up a consumer that only stores what arrived and
// answers 200.
func captureDelivery(t *testing.T, raw []byte, evs []meta.Event) capturedDelivery {
	t.Helper()
	var captured capturedDelivery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.body, _ = io.ReadAll(r.Body)
		captured.timestamp = r.Header.Get("X-Zapgw-Timestamp")
		captured.signature = r.Header.Get("X-Zapgw-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := NewDeliverer(nil).
		Deliver(context.Background(), testInst(srv.URL), raw, evs, "", "corr-1", ""); err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	return captured
}

func TestDeliverSignsWithInstanceSecret(t *testing.T) {
	captured := captureDelivery(t, []byte(`{}`), nil)

	want := SignDelivery(captured.timestamp, captured.body, "segredo-entrega-de-teste")
	if captured.signature != want {
		t.Fatalf("X-Zapgw-Signature = %q, quero %q", captured.signature, want)
	}
	if captured.signature == "" {
		t.Fatal("entrega sem assinatura — quem descobrir a URL de callback injeta evento falso")
	}
}

// TestDeliverSignsTheTimestampTogetherWithTheBody IS the T-022 test, and it's
// worth more than every other test in this file combined: it's the
// difference between the anti-replay the contract promises and what the
// code actually delivers.
//
// While SignDelivery only signed the body, X-Zapgw-Timestamp traveled
// outside the signature: whoever captured a delivery could resend it with a
// new timestamp and the signature would still be valid. The "tolerance"
// window the contract tells the consumer to implement protected nothing —
// worse, they would mark the item as resolved and stay exposed while
// thinking they weren't.
//
// Assertion (2) is the whole task: ONLY the timestamp is changed, byte for
// byte identical otherwise, and verification HAS to fail.
func TestDeliverSignsTheTimestampTogetherWithTheBody(t *testing.T) {
	const secret = "segredo-entrega-de-teste"
	captured := captureDelivery(t, []byte(`{"object":"whatsapp_business_account"}`),
		[]meta.Event{{Type: meta.EventTypeMessage, ID: "msg:wamid.A", Text: "oi"}})

	// (1) the delivery as it arrived has to verify — otherwise (2) would pass for free.
	if !DeliverySignatureValid(captured.timestamp, captured.body, captured.signature, secret) {
		t.Fatalf("a entrega legitima nao verificou (ts=%q, assinatura=%q)", captured.timestamp, captured.signature)
	}

	// (2) the replay: same body, same signature, timestamp advanced by 1h
	// to fit inside the consumer's tolerance window.
	n, err := strconv.ParseInt(captured.timestamp, 10, 64)
	if err != nil {
		t.Fatalf("X-Zapgw-Timestamp = %q nao e um unix em segundos: %v", captured.timestamp, err)
	}
	fresh := strconv.FormatInt(n+3600, 10)
	if DeliverySignatureValid(fresh, captured.body, captured.signature, secret) {
		t.Fatal("a assinatura continuou valida com o timestamp trocado — o replay passa " +
			"e a tolerancia do consumidor nao protege de nada")
	}
}

// TestDeliverSignsTheSAMEInstantThatGoesInTheHeader exists because, since
// T-022, the timestamp stopped being decoration: it enters the signature
// computation. If the header and the signature are calculated from TWO
// clock reads, they drift apart whenever the second rolls over in between —
// and the symptom is one delivery in every N that the consumer rejects as
// "invalid signature," with nothing here flagging it. A rare, intermittent,
// irreproducible failure.
//
// This test's clock ADVANCES one second on every read: any path that reads
// it twice fails deterministically.
func TestDeliverSignsTheSAMEInstantThatGoesInTheHeader(t *testing.T) {
	const secret = "segredo-entrega-de-teste"
	var captured capturedDelivery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.body, _ = io.ReadAll(r.Body)
		captured.timestamp = r.Header.Get("X-Zapgw-Timestamp")
		captured.signature = r.Header.Get("X-Zapgw-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	e := NewDeliverer(nil)
	var reads int64
	base := time.Unix(1769000000, 0)
	e.now = func() time.Time {
		reads++
		return base.Add(time.Duration(reads) * time.Second)
	}

	if _, err := e.Deliver(context.Background(), testInst(srv.URL), []byte(`{}`), nil, "", "c", ""); err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if !DeliverySignatureValid(captured.timestamp, captured.body, captured.signature, secret) {
		t.Fatalf("o header e a assinatura usaram instantes diferentes (%d leituras do relogio): "+
			"o consumidor recusaria esta entrega", reads)
	}
}

// TestSignatureSeparatesTimestampFromBodyWithoutAmbiguity pins down the
// format's SEPARATOR, and it isn't a decorative test: with raw
// concatenation, two different (timestamp, body) pairs produce the SAME
// signed bytes, and therefore the same signature. Here, "1769000000"+"0x"
// and "17690000000"+"x" produce the same text.
//
// Today this wouldn't be exploitable in the gateway — the body is always a
// JSON object, and therefore always starts with `{`, never with a digit.
// But that defense lives in ANOTHER place (the Envelope's json.Marshal) and
// no one reimplementing the computation in Python or TypeScript, reading
// only the contract, would know it exists. The dot makes the boundary
// unambiguous by construction, and this test is what stops a "simplified"
// reimplementation from dropping it.
func TestSignatureSeparatesTimestampFromBodyWithoutAmbiguity(t *testing.T) {
	const secret = "segredo-entrega-de-teste"
	a := SignDelivery("1769000000", []byte("0x"), secret)
	b := SignDelivery("17690000000", []byte("x"), secret)
	if a == b {
		t.Fatalf("dois pares (timestamp, corpo) diferentes deram a MESMA assinatura (%s) — "+
			"sem separador a fronteira entre os dois e ambigua", a)
	}
}

// TestFrozenSignatureVector checks the implementation against the
// fixture versioned at testdata/assinatura-entrega.json.
//
// WHY A VECTOR, and not just the formula in prose: the gateway will be
// consumed by systems in OTHER languages (the first one is Python). Whoever
// reimplements the computation from a paragraph gets the concatenation, the
// encoding, or the escaping wrong — and the failure mode is "invalid
// signature" with no diagnosis at all: there's no way to tell which of the
// three. With the vector, there is: either the implementation reproduces
// that hex from those three values, or the problem is theirs.
//
// And the vector is only worth anything while it stays TRUE, and that's
// why it's a test and not a piece of documentation: if the formula changes
// by accident, this goes red.
//
// WHAT IT DOES NOT PIN DOWN: the envelope's schema. The fixture's `corpo`
// is an OPAQUE byte string — the test never builds an Envelope — so a new
// field in the envelope doesn't break this test, which is exactly what's
// wanted: it exists to pin down the COMPUTATION, not the format.
func TestFrozenSignatureVector(t *testing.T) {
	rawBody, err := os.ReadFile(filepath.Join("testdata", "assinatura-entrega.json"))
	if err != nil {
		t.Fatalf("ler o vetor: %v", err)
	}
	var v struct {
		Secret    string `json:"segredo_entrega"`
		Timestamp string `json:"timestamp"`
		Body      string `json:"corpo"`
		Expected  string `json:"assinatura_esperada"`
	}
	if err := json.Unmarshal(rawBody, &v); err != nil {
		t.Fatalf("decodificar o vetor: %v", err)
	}

	// The vector only does its job if it carries both reimplementation
	// traps. A vector reduced to ASCII-only by some "cleanup" would pass
	// green on a broken implementation — so losing these properties is a
	// failure, not a detail. (Same rule as the directory-walking guard:
	// verify that there is something to verify.)
	if !strings.ContainsRune(v.Body, '\\') {
		t.Error("o corpo do vetor perdeu a barra invertida — o escape de JSON deixa de ser exercitado")
	}
	nonASCII := false
	for _, r := range v.Body {
		if r > 127 {
			nonASCII = true
			break
		}
	}
	if !nonASCII {
		t.Error("o corpo do vetor perdeu o caractere nao-ASCII — a codificacao UTF-8 deixa de ser exercitada")
	}
	if v.Secret == "" || v.Timestamp == "" || v.Expected == "" {
		t.Fatalf("vetor incompleto: %+v", v)
	}

	if got := SignDelivery(v.Timestamp, []byte(v.Body), v.Secret); got != v.Expected {
		t.Fatalf("SignDelivery = %s\nvetor congelado = %s\n"+
			"a formula mudou: ou o vetor esta desatualizado, ou a mudanca quebra todo consumidor", got, v.Expected)
	}
	if !DeliverySignatureValid(v.Timestamp, []byte(v.Body), v.Expected, v.Secret) {
		t.Fatal("o verificador recusou a assinatura do proprio vetor — assinar e verificar discordam")
	}
}

func TestDeliverySignatureRejectsWhatItMustNotAccept(t *testing.T) {
	// Hard contract, same as meta.SignatureValid: NEVER panics, for any
	// header value coming from outside.
	const secret = "segredo-entrega-de-teste"
	captured := captureDelivery(t, []byte(`{}`), nil)

	cases := []struct {
		name      string
		timestamp string
		body      []byte
		signature string
	}{
		{"timestamp ausente", "", captured.body, captured.signature},
		// The case above isn't enough, and the mutation proved it: remove
		// the explicit rejection of an empty timestamp, and it stays red by
		// accident anyway — an attacker who just DELETES the header can't
		// redo the HMAC without the secret, so the computation doesn't
		// close either way. What actually exercises the guard is a
		// COHERENT sender, one that omits the timestamp and signs without
		// it: without the rejection, that verifies as valid and produces a
		// "verified" delivery with no instant at all to check against the
		// tolerance window — the T-022 hole coming back through another door.
		{"timestamp ausente, e assinado sem ele", "", captured.body, SignDelivery("", captured.body, secret)},
		{"corpo trocado", captured.timestamp, []byte(`{"injetado":true}`), captured.signature},
		{"segredo de outra instancia", captured.timestamp, captured.body, SignDelivery(captured.timestamp, captured.body, "outro-segredo")},
		{"assinatura ausente", captured.timestamp, captured.body, ""},
		{"sem o prefixo sha256=", captured.timestamp, captured.body, strings.TrimPrefix(captured.signature, "sha256=")},
		{"hex invalido", captured.timestamp, captured.body, "sha256=nao-e-hex"},
		{"hex de tamanho errado", captured.timestamp, captured.body, "sha256=abcd"},
		{"header nao-ASCII", captured.timestamp, captured.body, "sha256=çãé"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if DeliverySignatureValid(c.timestamp, c.body, c.signature, secret) {
				t.Errorf("verificou o que nao devia (ts=%q, assinatura=%q)", c.timestamp, c.signature)
			}
		})
	}
}

func TestDeliverSendsTracingAndAntiReplayHeaders(t *testing.T) {
	var h http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	evs := []meta.Event{{Type: meta.EventTypeMessage, ID: "msg:wamid.A"}}
	_, err := NewDeliverer(nil).
		Deliver(context.Background(), testInst(srv.URL), []byte(`{}`), evs, "", "corr-42", "")
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}

	if h.Get("X-Zapgw-Timestamp") == "" {
		t.Error("falta X-Zapgw-Timestamp — sem ele nao ha janela anti-replay")
	}
	if got := h.Get("X-Zapgw-Event-Id"); got != "msg:wamid.A" {
		t.Errorf("X-Zapgw-Event-Id = %q, quero msg:wamid.A", got)
	}
	if got := h.Get("X-Zapgw-Correlation-Id"); got != "corr-42" {
		t.Errorf("X-Zapgw-Correlation-Id = %q, quero corr-42", got)
	}
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", h.Get("Content-Type"))
	}
}

func TestDeliverReturnsConsumerStatusUntranslated(t *testing.T) {
	// The gateway MIRRORS. Translating here would break Meta's
	// 200-vs-non-2xx rule as it crosses the network boundary.
	for _, want := range []int{200, 202, 400, 409, 500, 503} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(want)
		}))

		got, err := NewDeliverer(nil).
			Deliver(context.Background(), testInst(srv.URL), []byte(`{}`), nil, "", "c", "")
		srv.Close()

		if err != nil {
			t.Fatalf("Entregar: %v", err)
		}
		if got != want {
			t.Errorf("status = %d, quero %d", got, want)
		}
	}
}

func TestDeliverPassesThroughMetasOriginalSignature(t *testing.T) {
	// A consumer who wants end-to-end proof needs Meta's signature. Without
	// passing it along, the proof chain dies at the gateway and they
	// receive the raw body with no way to demonstrate its origin.
	var receivedSignature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-Hub-Signature-256")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := NewDeliverer(nil).
		Deliver(context.Background(), testInst(srv.URL), []byte(`{}`), nil, "", "c", "sha256=abcdef")
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if receivedSignature != "sha256=abcdef" {
		t.Fatalf("X-Hub-Signature-256 = %q, quero sha256=abcdef", receivedSignature)
	}
}

func TestDeliverOmitsTheSignatureWhenThereIsNone(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["X-Hub-Signature-256"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, _ = NewDeliverer(nil).
		Deliver(context.Background(), testInst(srv.URL), []byte(`{}`), nil, "", "c", "")

	if present {
		t.Fatal("header presente e vazio — melhor omitir que mentir que existe")
	}
}

func TestDeliverCarriesParseErrorWithoutFailingToSendTheRaw(t *testing.T) {
	// INVARIANT 2 of the spec. Without this, a parse bug in the gateway
	// discards events for ALL consumers at once, with a 200.
	var received Envelope
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := NewDeliverer(nil).
		Deliver(context.Background(), testInst(srv.URL), []byte(`null`), nil, "corpo nao e objeto", "c", "")
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if received.ParseError != "corpo nao e objeto" {
		t.Errorf("ParseError = %q", received.ParseError)
	}
	if received.Raw != base64.StdEncoding.EncodeToString([]byte(`null`)) {
		t.Errorf("Raw = %q — o cru TEM de ir mesmo com o parse falhando", received.Raw)
	}
}

// The wire has to say `"eventos":[]` when there is no event — NEVER `null` (T-067).
//
// THE ASSERTION IS ABOUT THE BYTES, AND THAT IS THE POINT OF THE TEST, not
// pedantry. The Envelope struct already said "empty" while the wire said
// `null`: `len(Events)` is 0 in both cases, and `json.Unmarshal` of `null`
// into a slice also returns an empty slice. That's why the WHOLE suite
// stayed green with the defect live from plan 1 (2026-07-23) until
// 2026-07-28 — including TestHandlerDeliversTheRawEvenWithTheParseFailing
// (handler_test.go), which produced this exact envelope, read the
// serialized body, and only asserted
// `strings.Contains(..., "parse_error")`: it had `"eventos":null` in hand
// and never looked. Any test that decodes the JSON back into Go repeats the
// mistake.
//
// The case is not rare: it's every unmodeled account webhook, which has
// been routine traffic since 2026-07-28 (the App is subscribed to ten
// fields).
func TestEnvelopeWithNoEventGoesOutAsEmptyArrayOnTheWireNeverNull(t *testing.T) {
	captured := captureDelivery(t, []byte(`{"object":"whatsapp_business_account"}`), nil)

	if bytes.Contains(captured.body, []byte(`"eventos":null`)) {
		t.Errorf("o fio manda `\"eventos\":null`, que estoura `for ev in envelope[\"eventos\"]`"+
			" em Python e nunca casa com `eventos == []`; corpo: %s", captured.body)
	}
	if !bytes.Contains(captured.body, []byte(`"eventos":[]`)) {
		t.Errorf("o fio nao traz `\"eventos\":[]`; corpo: %s", captured.body)
	}

	// `parse_error` in the SAME envelope, and it's CORRECT as it is: with
	// no error, the field is the empty string — falsy in every language —,
	// never `null` and never absent (no omitempty). This assertion lives
	// here on purpose, right next to the one above: it's the pair that
	// defines the T-067 rule — a field whose empty value is already falsy
	// doesn't get touched; a field whose empty value is `null` in a type
	// that gets ITERATED over does.
	if !bytes.Contains(captured.body, []byte(`"parse_error":""`)) {
		t.Errorf("`parse_error` deixou de sair como `\"\"` no fio; corpo: %s", captured.body)
	}
}

// --- Delivery TLS: strict, no escape hatch ----------------------------------
//
// THE PROJECT RULE (CLAUDE.md, "TLS — não existe modo desligado"): no path in
// this gateway may have an option to skip certificate verification. Not a
// flag, not an environment variable, not a config field, not
// "development-only."
//
// The reason isn't purism: turning off verification produces NO error at
// all — it just removes a protection. So the option gets flipped on once to
// unblock a demo and stays on forever, silently, and the https://
// requirement on callback_url (config.ValidateCallbackURL) turns into
// theater: the scheme keeps saying https and there's no guarantee behind it
// anymore.
//
// The legitimate case that makes someone want the escape hatch — a consumer
// with their own CA — has its own narrow way out: a CA bundle PER INSTANCE,
// which is still verification, just with a different trust anchor.

// certificatePEM returns the certificate in the format the instance's
// bundle is stored in.
func certificatePEM(t *testing.T, c *x509.Certificate) string {
	t.Helper()
	if c == nil {
		t.Fatal("servidor de teste sem certificado — o teste nao verificaria nada")
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))
}

// selfSignedCertificate forges a NEW certificate on every call, valid for
// 127.0.0.1 within the requested window.
//
// WHY NOT USE httptest'S OWN CERTIFICATE: it is the SAME for every server
// (the package embeds a fixed pair) and it's valid until 2084. With it,
// "instance A's CA is not valid for instance B" would pass green without
// proving anything — the two consumers would literally have the same
// certificate —, and validity checking couldn't be tested at all. That's
// exactly how this test failed on its first run, and that's why the
// certificate is forged here.
func selfSignedCertificate(t *testing.T, name string, from, until time.Time) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             from,
		NotAfter:              until,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed: it is its own anchor
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("criar certificado: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{derBytes}, PrivateKey: key}
}

func tlsServerWith(t *testing.T, cert tls.Certificate) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	// A rejected handshake is the EXPECTED OUTCOME in most of these tests;
	// without silencing it, the server prints one line per attempt and the
	// green output turns into a wall of errors no one reads.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// testTLSServer spins up an https consumer with a SELF-SIGNED
// certificate — the certificate no public CA covers, which is the internal
// consumer's case. Each call gets ITS OWN certificate.
func testTLSServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	return tlsServerWith(t, selfSignedCertificate(t, name,
		time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour)))
}

// expiredTLSServer spins up a consumer whose certificate EXPIRED
// yesterday: it is up and responding, and the delivery still has to be
// rejected.
func expiredTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return tlsServerWith(t, selfSignedCertificate(t, "consumidor-com-cert-vencido",
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)))
}

func instWithBundle(callback, bundle string) config.Instance {
	i := testInst(callback)
	i.CABundle = bundle
	return i
}

// (a) THE GUARD. A certificate no trust anchor covers = delivery FAILS. If
// someone adds a path that accepts an invalid certificate (crypto/tls's
// escape hatch, a custom verifier that returns nil, a client injected from
// outside), this test goes RED.
func TestDeliveryFailsOnCertificateNoAnchorCovers(t *testing.T) {
	srv := testTLSServer(t, "consumidor")

	status, err := NewDeliverer(nil).
		Deliver(context.Background(), testInst(srv.URL), []byte(`{}`), nil, "", "c", "")

	if err == nil {
		t.Fatalf("a entrega PASSOU com certificado autoassinado (status %d) — a verificacao de TLS esta desligada em algum lugar", status)
	}
	if status != 0 {
		t.Errorf("status = %d — entrega que falhou no TLS nao pode devolver status de consumidor", status)
	}

	// The classification is checked against the REAL handshake error,
	// never against a synthetic one: it's the only way to prove
	// mirror.go's taxonomy matches what the platform actually returns.
	v := ConsumerVerdict(status, err)
	if !v.Alarm {
		t.Error("falha de certificado NAO alarmou — certificado errado ou vencido nao se conserta sozinho, e a Meta desiste em 36h")
	}
	if !strings.Contains(strings.ToLower(v.Reason), "certificado") {
		t.Errorf("Reason = %q — tem de dizer que o problema e CERTIFICADO, senao a acao humana e a errada (levantar o app do consumidor em vez de renovar o certificado)", v.Reason)
	}
	if v.StatusForMeta/100 == 2 {
		t.Errorf("StatusForMeta = %d — 2xx aqui joga a mensagem fora antes de qualquer chance de conserto", v.StatusForMeta)
	}
	// Reason goes to the log; the callback_url is encrypted at rest
	// precisely so that a stolen backup doesn't reveal the consumers'
	// topology.
	for _, forbidden := range []string{strings.TrimPrefix(srv.URL, "https://"), srv.Listener.Addr().String()} {
		if strings.Contains(v.Reason, forbidden) {
			t.Errorf("Reason vazou %q — texto: %s", forbidden, v.Reason)
		}
	}
}

// (b) THE NARROW WAY OUT. The same delivery passes when that consumer's CA
// is registered ON THE INSTANCE. It's still verification: only the trust
// anchor changes.
func TestDeliveryAcceptsCertificateFromCARegisteredOnInstance(t *testing.T) {
	srv := testTLSServer(t, "consumidor")
	inst := instWithBundle(srv.URL, certificatePEM(t, srv.Certificate()))

	status, err := NewDeliverer(nil).
		Deliver(context.Background(), inst, []byte(`{}`), nil, "", "c", "")
	if err != nil {
		t.Fatalf("Entregar com a CA da instancia cadastrada: %v — sem esta saida o consumidor com CA propria nao tem caminho legitimo, e e dai que nasce a pressao pela escotilha", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, quero 200", status)
	}
}

// (c) The bundle is only valid for the instance that registered it. A
// global pool would accumulate trust anchors across tenants: the CA client
// A registered would end up valid for client B's consumer, with no one
// asking for that.
func TestCertificateAcceptedOnOneInstanceIsNotValidOnAnother(t *testing.T) {
	srvA := testTLSServer(t, "consumidor-da-instancia-a")
	srvB := testTLSServer(t, "consumidor-da-instancia-b")
	e := NewDeliverer(nil)

	// Instance A delivers to its own consumer: passes.
	instA := instWithBundle(srvA.URL, certificatePEM(t, srvA.Certificate()))
	if _, err := e.Deliver(context.Background(), instA, []byte(`{}`), nil, "", "c", ""); err != nil {
		t.Fatalf("instancia A no consumidor dela: %v", err)
	}

	// Instance B, with A's CA, delivering to B's consumer: FAILS.
	instB := instWithBundle(srvB.URL, certificatePEM(t, srvA.Certificate()))
	if _, err := e.Deliver(context.Background(), instB, []byte(`{}`), nil, "", "c", ""); err == nil {
		t.Fatal("a CA da instancia A validou o certificado do consumidor de B — as ancoras de confianca estao vazando entre inquilinos")
	}

	// And instance B, with ITS OWN CA, has to pass. Without this half the
	// test would accept a deliverer that only keeps the FIRST anchor and
	// uses it for everyone: both assertions above would pass by accident
	// (B's rejection would come from the wrong anchor, not from the rule),
	// and the defect would show up when the second instance with its own
	// CA got registered — in production.
	instBCorrect := instWithBundle(srvB.URL, certificatePEM(t, srvB.Certificate()))
	if _, err := e.Deliver(context.Background(), instBCorrect, []byte(`{}`), nil, "", "c", ""); err != nil {
		t.Fatalf("instancia B com a CA dela: %v — cada instancia tem de usar a ancora DELA, nao a que chegou primeiro", err)
	}

	// And A's path keeps working afterward: B's rejection cannot have been
	// "the cache broke everything."
	if _, err := e.Deliver(context.Background(), instA, []byte(`{}`), nil, "", "c", ""); err != nil {
		t.Fatalf("instancia A parou de funcionar depois da recusa de B: %v", err)
	}

	// THE LAST ASSERTION, and it's the one that catches a pool that
	// ACCUMULATES: now that both CAs have already passed through here,
	// instance B with A's CA still has to keep failing. In a shared pool it
	// would pass — because B's certificate would already be in there,
	// planted by the OTHER instance. It's deliberately the same call as
	// above, repeated at the end: the answer changes depending on what
	// happened in between, and that's exactly what's being tested.
	if _, err := e.Deliver(context.Background(), instB, []byte(`{}`), nil, "", "c", ""); err == nil {
		t.Fatal("depois que as duas CAs foram usadas, a instancia B passou a validar com a CA de A — as ancoras estao se acumulando num pool comum")
	}
}

// An EXPIRED certificate is its own case: the consumer is up, it responds,
// and the delivery still has to be rejected. It's the case that most looks
// like "everything's fine" to an outside observer.
func TestDeliveryFailsOnExpiredCertificateEvenWithCARegistered(t *testing.T) {
	srv := expiredTLSServer(t)
	// The anchor itself is the expired certificate: if validity weren't
	// checked, the chain would close and the delivery would pass.
	inst := instWithBundle(srv.URL, certificatePEM(t, srv.Certificate()))

	_, err := NewDeliverer(nil).
		Deliver(context.Background(), inst, []byte(`{}`), nil, "", "c", "")
	if err == nil {
		t.Fatal("a entrega PASSOU com certificado vencido — a validade nao esta sendo conferida")
	}
	if v := ConsumerVerdict(0, err); !v.Alarm {
		t.Errorf("certificado vencido nao alarmou (Reason=%q) — ninguem seria avisado ate a Meta desistir", v.Reason)
	}
}

// A registered but unreadable bundle: NO delivery from that instance goes
// out, and that needs a person. Staying silent here produces the same
// symptom as a consumer being down — with the difference that this one
// doesn't come back on its own.
func TestDeliveryFlagsInstanceBundleWithNoCertificateAtAll(t *testing.T) {
	srv := testTLSServer(t, "consumidor")
	inst := instWithBundle(srv.URL, "-----BEGIN CERTIFICATE-----\nnao sou um certificado\n-----END CERTIFICATE-----\n")

	_, err := NewDeliverer(nil).
		Deliver(context.Background(), inst, []byte(`{}`), nil, "", "c", "")
	if !errors.Is(err, config.ErrInvalidCABundle) {
		t.Fatalf("erro = %v, quero ErrInvalidCABundle", err)
	}
	if v := ConsumerVerdict(0, err); !v.Alarm {
		t.Error("bundle invalido nao alarmou — a instancia fica muda para sempre e ninguem fica sabendo")
	}
}

// The Deliverer keeps one client per bundle, and http.Server serves every
// request in its own goroutine over the SAME handler. Mutable state in that
// shape has already cost this project a Critical (docs/ARMADILHAS.md);
// without a concurrent test, -race has nothing to detect.
//
// THE CERTIFICATE OBSERVER (T-064) GOES IN HERE on purpose: it's the second
// piece of shared mutable state on this path, written by every delivery
// that completes a handshake. Without it in this list, -race would have
// nothing to detect in the new write — which is literally the trap
// recorded in docs/ARMADILHAS.md, "Go / concorrência".
func TestConcurrentDeliveryDoesNotMixInstanceCertificates(t *testing.T) {
	srvA := testTLSServer(t, "consumidor-da-instancia-a")
	srvB := testTLSServer(t, "consumidor-da-instancia-b")
	e, spy := delivererWithSpy()

	instA := instWithBundle(srvA.URL, certificatePEM(t, srvA.Certificate()))
	instB := instWithBundle(srvB.URL, certificatePEM(t, srvB.Certificate()))
	instWrong := instWithBundle(srvB.URL, certificatePEM(t, srvA.Certificate()))

	var wg sync.WaitGroup
	asExpected := make(chan bool, 60)
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); asExpected <- delivered(e, instA) }()
		go func() { defer wg.Done(); asExpected <- delivered(e, instB) }()
		go func() { defer wg.Done(); asExpected <- !delivered(e, instWrong) }()
	}
	wg.Wait()
	close(asExpected)

	n := 0
	for ok := range asExpected {
		n++
		if !ok {
			t.Fatal("entrega concorrente deu o desfecho errado — ou a instancia certa falhou, ou a errada passou")
		}
	}
	if n != 60 {
		t.Fatalf("conferi %d entregas, esperava 60", n)
	}

	// Only the 40 deliveries that COMPLETED a handshake turned into an
	// observation — the 20 from the instance with the wrong anchor never
	// got to see any trustworthy certificate.
	if obs := spy.all(); len(obs) != 40 {
		t.Errorf("observacoes = %d, quero 40 (20 de A + 20 de B; as 20 recusadas nao observam)", len(obs))
	}
}

// --- T-064: the consumer's certificate validity -----------------------------
//
// The task was born from a FALSE claim: "the gateway already verifies the
// certificate on delivery, the date is already in hand." The first half
// was true (the tests above prove the verification); the second wasn't —
// no one looked at resp.TLS.PeerCertificates, and no date existed anywhere
// at all.

// capturedObservation is what the observer received, from the point of
// view of whoever is going to READ the state route.
type capturedObservation struct {
	slug       string
	expiresAt  time.Time
	observedAt time.Time
}

// spyObserver stores the observations instead of writing them. It has
// a mutex because the concurrent delivery test uses it — without it,
// -race would find the spy's own race and hide the one that matters.
type spyObserver struct {
	mu   sync.Mutex
	made []capturedObservation
}

func (o *spyObserver) RecordCallbackCertificate(slug string, expiresAt, observedAt time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.made = append(o.made, capturedObservation{slug: slug, expiresAt: expiresAt, observedAt: observedAt})
	return nil
}

func (o *spyObserver) all() []capturedObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]capturedObservation(nil), o.made...)
}

// delivererWithSpy returns the deliverer and the spy already wired together.
func delivererWithSpy() (*Deliverer, *spyObserver) {
	spy := &spyObserver{}
	return NewDeliverer(config.NewCertificateObserverWithStore(spy)), spy
}

// THE TASK'S TEST: a real delivery, with a real handshake, populates the
// certificate's date AND the instant it was seen.
//
// THE MANDATORY MUTATION: deleting the call to e.observeCertificate (or the
// read of resp.TLS.PeerCertificates inside it) makes this test go red on
// "no observation" — not on some other field.
//
// The date is checked against the REAL certificate the server presented
// (srv.Certificate().NotAfter), and not against a constant written here:
// it's the only way to prove the value came from the PEER'S OWN CHAIN, and
// not from any other plausible place.
func TestDeliveryObservesConsumerCertificateValidity(t *testing.T) {
	expires := time.Now().Add(45 * 24 * time.Hour)
	srv := tlsServerWith(t, selfSignedCertificate(t, "consumidor", time.Now().Add(-time.Hour), expires))
	inst := instWithBundle(srv.URL, certificatePEM(t, srv.Certificate()))

	e, spy := delivererWithSpy()
	// Fixed clock: the observation's timestamp is the instant OF THE
	// DELIVERY, and a time.Now() read again inside would pass this test by
	// accident.
	when := time.Unix(1769000000, 0).UTC()
	e.now = func() time.Time { return when }

	status, err := e.Deliver(context.Background(), inst, []byte(`{}`), nil, "", "c", "")
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, quero 200", status)
	}

	obs := spy.all()
	if len(obs) != 1 {
		t.Fatalf("observacoes = %d, quero 1 — a entrega nao capturou o certificado do consumidor", len(obs))
	}
	if obs[0].slug != "lojinha" {
		t.Errorf("slug = %q, quero %q — observacao gravada na instancia errada le o certificado do consumidor alheio",
			obs[0].slug, "lojinha")
	}
	if want := srv.Certificate().NotAfter; !obs[0].expiresAt.Equal(want) {
		t.Errorf("expiresAt = %v, quero %v (o NotAfter do certificado que o consumidor apresentou)",
			obs[0].expiresAt, want)
	}
	// And the date can't be a disguised zero value: it's the forged
	// certificate's own date.
	if want := expires.UTC().Truncate(time.Second); !obs[0].expiresAt.UTC().Equal(want) {
		t.Errorf("expiresAt = %v, quero %v", obs[0].expiresAt.UTC(), want)
	}
	if !obs[0].observedAt.Equal(when) {
		t.Errorf("observedAt = %v, quero %v — o carimbo e o instante da entrega, nao um segundo time.Now()",
			obs[0].observedAt, when)
	}
}

// A REJECTED handshake observes nothing, and that is the opposite of an
// oversight: there is no trustworthy certificate to observe, and storing
// what we just rejected would make the state route publish, as "observed,"
// a chain the gateway doesn't accept. The PREVIOUS observation stays in
// place and ages — which is more information than a zeroed field.
func TestDeliveryWithRefusedCertificateObservesNothing(t *testing.T) {
	srv := testTLSServer(t, "consumidor") // no bundle: no anchor covers it
	e, spy := delivererWithSpy()

	if _, err := e.Deliver(context.Background(), testInst(srv.URL), []byte(`{}`), nil, "", "c", ""); err == nil {
		t.Fatal("a entrega passou com certificado que nenhuma ancora cobre")
	}
	if obs := spy.all(); len(obs) != 0 {
		t.Errorf("observacoes = %d, quero 0 — certificado recusado nao vira observacao (%+v)", len(obs), obs)
	}
}

// THE HTTP OUTCOME DOES NOT MATTER for the observation. A consumer that
// responds 500 presented the certificate the same way, and it's precisely
// during the week their application is having trouble that no one wants to
// lose sight of the certificate's date.
func TestDeliveryObservesCertificateEvenWhenConsumerAnswers500(t *testing.T) {
	cert := selfSignedCertificate(t, "consumidor", time.Now().Add(-time.Hour), time.Now().Add(10*24*time.Hour))
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	inst := instWithBundle(srv.URL, certificatePEM(t, srv.Certificate()))
	e, spy := delivererWithSpy()

	status, err := e.Deliver(context.Background(), inst, []byte(`{}`), nil, "", "c", "")
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, quero 500", status)
	}
	if obs := spy.all(); len(obs) != 1 {
		t.Errorf("observacoes = %d, quero 1 — o certificado foi apresentado, o 500 e da aplicacao dele", len(obs))
	}
}

// failingObservationStore writes nothing and always returns an error.
type failingObservationStore struct{}

func (failingObservationStore) RecordCallbackCertificate(string, time.Time, time.Time) error {
	return errors.New("banco fora do ar")
}

// THE SAME RULE AS THE COUNTER (T-035): tracking never brings delivery
// down. If Register ever starts returning an error, this project's
// dominant pattern (`if err != nil { return }`) would turn a full database
// into a lost delivery — and Meta would hear the result of that.
func TestFailureToObserveCertificateDoesNotChangeDeliveryOutcome(t *testing.T) {
	srv := testTLSServer(t, "consumidor")
	inst := instWithBundle(srv.URL, certificatePEM(t, srv.Certificate()))

	e := NewDeliverer(config.NewCertificateObserverWithStore(failingObservationStore{}))
	status, err := e.Deliver(context.Background(), inst, []byte(`{}`), nil, "", "c", "")
	if err != nil {
		t.Fatalf("Entregar: %v — a falha ao gravar a observacao vazou para o desfecho da entrega", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, quero 200", status)
	}
}

func delivered(e *Deliverer, inst config.Instance) bool {
	_, err := e.Deliver(context.Background(), inst, []byte(`{}`), nil, "", "c", "")
	return err == nil
}

// THE SOURCE-LEVEL GUARD, and it covers BOTH directions of the rule: the
// gateway talking to the Graph API and the gateway delivering to the
// consumer. The behavioral tests above only cover delivery; this sweep
// catches the escape hatch in any file in the repository, including a
// test — because "only in the test" is exactly how the option gets in
// before it turns into production. This is the test CLAUDE.md promises in
// the "TLS — não existe modo desligado" section (T-160).
//
// The needle is assembled by concatenation on purpose: written out whole,
// it would show up in this very file and the sweep would flag itself.
//
// Fails CLOSED: any error locating the module root or reading the tree
// becomes a t.Fatalf, never a silent "found nothing."
func TestNoSourceInTheRepoTurnsOffTLSVerification(t *testing.T) {
	needle := "Insecure" + "SkipVerify"

	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("localizar a raiz do modulo (falha fechada): %v", err)
	}
	seenIDs := map[string]bool{}
	var findings []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("ler %s: %w", path, err)
		}
		if d.IsDir() {
			// .git never has real .go files. .claude/ holds worktrees from
			// other implementer agents — whole copies of the repo that
			// aren't this commit (same reason as the .gitignore entry and
			// "gofmt -l cmd internal" instead of "gofmt -l .", see
			// CLAUDE.md). Any other hidden directory also isn't code from
			// this repo.
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativizar %s: %w", path, err)
		}
		relative = filepath.ToSlash(relative)
		seenIDs[relative] = true

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("ler %s: %w", path, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			if strings.Contains(line, needle) {
				findings = append(findings, fmt.Sprintf("%s:%d", relative, i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrer o repo (falha fechada): %v", err)
	}
	if len(findings) > 0 {
		t.Fatalf("%s encontrado em %s: desligar a verificacao de certificado nao gera erro nenhum, so remove uma protecao —"+
			" por isso a opcao nao pode existir (CLAUDE.md, \"TLS — nao existe modo desligado\")."+
			" Consumer com CA propria usa o bundle de CA por instancia, que continua sendo verificacao",
			needle, strings.Join(findings, ", "))
	}

	// Guard against the worst outcome of a sweep: passing GREEN without
	// having looked at anything (docs/ARMADILHAS.md, "Testes"). The two
	// required files are one for each direction of the rule.
	for _, required := range []string{
		"internal/inbound/deliver.go", // delivery to the consumer
		"internal/meta/client.go",     // conversation with the Graph API
	} {
		if !seenIDs[required] {
			t.Fatalf("a varredura nao alcancou %s — ela passou sem verificar o que existe para verificar (%d arquivos vistos)",
				required, len(seenIDs))
		}
	}
}

// moduleRoot locates the Go module root (the directory with go.mod) by
// walking up from this test file's own directory, instead of assuming a
// fixed depth (`../..`) that would silently break if the package ever moved.
func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) nao retornou o caminho deste arquivo")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod nao encontrado subindo a partir de %s", filepath.Dir(file))
		}
		dir = parent
	}
}
