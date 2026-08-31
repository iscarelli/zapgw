// Delivery of the event to the consumer.
//
// THREE GUARANTEES, and all three are the reason this file exists separately:
//
//  1. THE RAW BODY ALWAYS GOES, alongside the events. The consumer stores the
//     raw body BEFORE looking at the events; if the parse failed, it still has
//     everything.
//  2. THE DELIVERY IS SIGNED with a secret PER INSTANCE. Without this, whoever
//     discovers the callback URL injects a fake event — and an event turns
//     into an action on the consumer's side.
//  3. THE CONSUMER'S CERTIFICATE IS VERIFIED, with no escape hatch. The
//     signature in item 2 proves integrity, not confidentiality: the raw body
//     carries personal data, and that's why callback_url has to be https
//     (config.ValidateCallbackURL). Requiring the scheme without verifying the
//     certificate would be theater — the URL would keep saying https and
//     there would be no guarantee behind it at all. A consumer with its own
//     CA registers THEIR bundle on the instance; there is no way, and there
//     must never be a way, to accept an invalid certificate.
package inbound

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// Envelope is the body of the POST to the consumer.
type Envelope struct {
	Instance   string       `json:"instancia"`
	ReceivedAt string       `json:"recebido_em"`
	Raw        string       `json:"cru"` // base64 of the EXACT bytes from Meta
	Events     []meta.Event `json:"eventos"`
	ParseError string       `json:"parse_error"`
}

type Deliverer struct {
	now func() time.Time

	// observer holds the certificate validity that the delivery's handshake
	// already carries (T-064). It is for TRACKING ONLY: it decides nothing
	// here and cannot change the delivery's outcome — Register returns no
	// error, and a nil receiver is a no-op (delivery tests that don't deal
	// with certificates pass nil).
	observer *config.CertificateObserver

	// clients holds one http.Client per TRUST ANCHOR (key: the hash of the
	// instance's CA bundle; "" -> the system's CA store).
	//
	// One client per bundle, and not a single shared pool, is what keeps the
	// anchor registered by one client valid ONLY for its own consumer: with a
	// shared pool, the CA that tenant A registered would end up validating
	// tenant B's consumer certificate without anyone having asked for that.
	//
	// And it's one client per bundle, not one per DELIVERY, because a new
	// client on every call would throw away the connection and redo the
	// handshake every time.
	//
	// The map does not grow without limit: the key is the bundle of
	// registered instances, never something that arrives from outside.
	mu      sync.Mutex
	clients map[string]*http.Client
}

// NewDeliverer builds the deliverer. It does NOT receive an http.Client, and
// that is the rule, not an oversight: a client injected from outside is
// exactly where the escape hatch would get in (it would only take one
// tls.Config without verification in the wrong place), and turning off
// certificate verification produces no error at all — it just removes a
// protection, silently. See CLAUDE.md, "TLS — não existe modo desligado", and the
// guard in deliver_test.go.
//
// THE OBSERVER COMES IN AS A REQUIRED PARAMETER (nil for whoever doesn't care)
// and not through a second constructor: a `NewDeliverer()` with no observer
// sitting next to a `NovoEntregadorComObservador()` would be a production path
// where the capture simply doesn't happen, and no one would know — the
// mother-trap of this project (docs/ARMADILHAS.md). With a single
// constructor, whoever assembles it is forced to say what they want.
func NewDeliverer(observer *config.CertificateObserver) *Deliverer {
	return &Deliverer{now: time.Now, clients: map[string]*http.Client{}, observer: observer}
}

// clientForInstance returns the HTTP client for that trust anchor.
//
// The verification is ALWAYS strict: there is no path in this file that
// accepts an invalid certificate. A registered bundle loosens nothing — it
// SWAPS the anchor (the consumer's own CA in place of the system store), and
// the chain, the validity and the hostname keep being checked the same way.
func (e *Deliverer) clientForInstance(bundle string) (*http.Client, error) {
	key := sha256.Sum256([]byte(bundle))

	e.mu.Lock()
	defer e.mu.Unlock()
	if c, ok := e.clients[string(key[:])]; ok {
		return c, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if bundle != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(bundle)) {
			// Without this the instance would silently fall back to the
			// system store, which is exactly the consumer the own-CA feature
			// exists to cover.
			return nil, fmt.Errorf("inbound: entrega: %w", config.ErrInvalidCABundle)
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	c := &http.Client{Transport: transport}
	e.clients[string(key[:])] = c
	return c, nil
}

const deliverySignaturePrefix = "sha256="

// signatureSeparator separates the timestamp from the body inside the signed
// message.
//
// It exists so that the boundary between the two is UNAMBIGUOUS. Without a
// separator, (ts="1769000000", body="X") and (ts="176900000", body="0X")
// would produce exactly the same signed bytes — and therefore the same
// signature. A dot solves it because the timestamp is only decimal digits: no
// other (ts, body) pair can produce the same concatenation.
const signatureSeparator = "."

// signedMessage assembles the bytes the HMAC covers: timestamp + "." + body.
//
// This function is the definition of the format, and there is only one for
// both sides (signing and verifying). Two copies of the same computation
// drift apart the day one of them changes, and the drift shows up as
// "invalid signature" with no clue why.
func signedMessage(timestamp string, body []byte) []byte {
	m := make([]byte, 0, len(timestamp)+len(signatureSeparator)+len(body))
	m = append(m, timestamp...)
	m = append(m, signatureSeparator...)
	return append(m, body...)
}

// SignDelivery returns the value of X-Zapgw-Signature.
//
// The signature covers THE TIMESTAMP AND THE BODY, in this order, separated
// by a dot:
//
//	X-Zapgw-Signature = "sha256=" + hex(HMAC_SHA256(secret, timestamp + "." + body))
//
// where `timestamp` are the exact ASCII bytes of X-Zapgw-Timestamp (unix in
// seconds, decimal) and `body` are the exact bytes of the POST body.
//
// WHY THE TIMESTAMP ENTERS THE COMPUTATION (T-022): until 2026-07-26 only the
// body was signed, and the timestamp traveled in a header OUTSIDE the
// signature. Whoever captured a delivery could resend it with a new
// timestamp and the signature would still be valid — the tolerance window
// that the contract tells the consumer to implement protected nothing, and
// they were exposed while thinking they weren't. A doc that promises a
// guarantee that doesn't exist is worse than no doc at all.
//
// It remains deliberately SIMPLE and ours: the consumer doesn't need to learn
// any Meta peculiarity to verify it. The peculiarities die at the gateway.
func SignDelivery(timestamp string, body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(signedMessage(timestamp, body))
	return deliverySignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// DeliverySignatureValid redoes the computation from SignDelivery and
// compares.
//
// The gateway SIGNS, it does not verify — whoever verifies is the consumer,
// in their own language. This function exists anyway for two reasons, and
// neither is decorative: it is the EXECUTABLE definition of the format
// published in docs/CONTRATO-CONSUMIDOR.md (doc and code cannot drift apart
// if there is only one computation), and it is what lets the T-022 test prove
// the anti-replay behavior the way the consumer will actually live it —
// taking a real delivery and swapping only the timestamp.
//
// Hard contract, same as meta.SignatureValid: NEVER panics, for any header
// value.
func DeliverySignatureValid(timestamp string, body []byte, header, secret string) bool {
	// A missing timestamp is a rejection, not "sign without it": with no
	// instant there is nothing to compare against the tolerance window, and
	// a delivery counted as valid with no freshness check is exactly the
	// hole T-022 closed.
	if timestamp == "" {
		return false
	}
	if !strings.HasPrefix(header, deliverySignaturePrefix) {
		return false
	}

	// hex.DecodeString is what rejects a non-ASCII header: a byte outside
	// [0-9a-fA-F] becomes an error, never a panic.
	expected, err := hex.DecodeString(header[len(deliverySignaturePrefix):])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(signedMessage(timestamp, body))

	// hmac.Equal, never ==: constant-time comparison. And the comparison is
	// deliberately over the DECODED BYTES — with []byte, swapping in == doesn't
	// even compile.
	return hmac.Equal(expected, mac.Sum(nil))
}

// Deliver sends the envelope to the consumer and returns THEIR STATUS,
// untranslated. Who translates it is mirror.go, and only at the boundary
// with Meta.
func (e *Deliverer) Deliver(
	ctx context.Context,
	inst config.Instance,
	raw []byte,
	evs []meta.Event,
	parseErr string,
	correlation string,
	metaSignature string,
) (int, error) {
	// Before anything else: with no usable trust anchor there is no possible
	// delivery, and the reason has to be exactly that, not "consumer
	// unreachable."
	client, err := e.clientForInstance(inst.CABundle)
	if err != nil {
		return 0, err
	}

	// ONE clock read for the whole delivery. Since T-022 the timestamp enters
	// the signature computation, so reading the clock twice would make the
	// header and the signature drift apart whenever the second rolled over
	// in between: one delivery in every N rejected by the consumer as
	// "invalid signature," with nothing here flagging it. A rare,
	// irreproducible failure — this project's preferred flavor of damage.
	now := e.now()
	timestamp := strconv.FormatInt(now.Unix(), 10)

	// `eventos` ALWAYS COMES OUT AS AN ARRAY, NEVER `null` (T-067, 2026-07-28).
	//
	// `evs` arrives nil as ROUTINE, not as an exception: meta.ParseWebhook
	// returns `var evs []Event` with no append at all when there is nothing
	// to enrich (an ACCOUNT webhook not modeled — the App is subscribed to
	// ten fields and the gateway models two —, a body with no
	// messages/statuses, a parse that failed entirely), and the handler
	// passes it through untouched (internal/inbound/handler.go:194). A nil
	// slice in Go serializes as `null`, not as `[]`.
	//
	// WHY THIS IS A DEFECT AND `parse_error` IS NOT, and this is the general
	// rule: the empty value of `parse_error` is `""`, falsy in every
	// language, and that's why the field is fine as it is. The empty value
	// of `eventos` used to be `null` in a type the consumer ITERATES over —
	// `for ev in envelope["eventos"]` blows up with
	// `TypeError: 'NoneType' object is not iterable` in Python, and
	// `eventos == []` never matches. A field whose empty value is already
	// falsy needs no fix; a field whose empty value is `null` in an
	// iterable type does.
	//
	// HERE, AND NOT AT EVERY CALLER: this is the one point every envelope
	// passes through. Normalizing on the caller's side means enumerating
	// call sites, which is the exact shape of this project's mother-trap
	// (docs/ARMADILHAS.md) — the next delivery path would be born sending
	// `null` all over again.
	if evs == nil {
		evs = []meta.Event{}
	}

	env := Envelope{
		Instance:   inst.Slug,
		ReceivedAt: now.UTC().Format(time.RFC3339),
		Raw:        base64.StdEncoding.EncodeToString(raw),
		Events:     evs,
		ParseError: parseErr,
	}

	body, err := json.Marshal(env)
	if err != nil {
		return 0, fmt.Errorf("inbound: montar envelope: %w", err)
	}

	deadline := time.Duration(inst.TimeoutMs) * time.Millisecond
	if deadline <= 0 {
		deadline = 5 * time.Second // OUR default. Meta doesn't publish theirs.
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("inbound: montar requisicao: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// THE SAME `timestamp` in both headers: what goes into X-Zapgw-Timestamp
	// is exactly what entered the signature computation.
	req.Header.Set("X-Zapgw-Signature", SignDelivery(timestamp, body, inst.DeliverySecret))
	req.Header.Set("X-Zapgw-Timestamp", timestamp)
	req.Header.Set("X-Zapgw-Correlation-Id", correlation)
	// Meta's ORIGINAL signature, passed through byte for byte. The gateway
	// has already verified it; it travels along so that a consumer who WANTS
	// end-to-end proof can redo the computation with the app_secret. Without
	// this, the proof chain dies here, and the consumer receives the raw body
	// with no way to demonstrate it came from Meta.
	if metaSignature != "" {
		req.Header.Set("X-Hub-Signature-256", metaSignature)
	}
	if len(evs) > 0 {
		// DETERMINISTIC id of the first event: it's what the consumer
		// deduplicates by. A legitimate Meta redelivery and a malicious
		// resend land on the same id, and therefore in the same dedup.
		req.Header.Set("X-Zapgw-Event-Id", evs[0].ID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err // consumer down or slow: mirror.go decides what Meta hears
	}
	defer resp.Body.Close()
	e.observeCertificate(inst.Slug, resp, now)
	_, _ = io.Copy(io.Discard, resp.Body) // drain so the connection can be reused

	return resp.StatusCode, nil
}

// observeCertificate stores the `NotAfter` of the certificate the consumer
// just presented (T-064).
//
// FOR FREE, AND ON PURPOSE: the connection already exists and the handshake
// has already happened and been VERIFIED (clientForInstance). There is no
// probe, no extra connection and no timer — the date comes from the same
// delivery that was going to happen anyway. If this ever turns into an active
// check, it will be a different network path with a different way to fail,
// and the decision has to be made again.
//
// THE CERTIFICATE IS THE LEAF (PeerCertificates[0], the first one by
// crypto/tls's own definition). It's the one the consumer controls and
// renews — the one that expires every 90 days on a Let's Encrypt/Google Trust
// Services setup and takes delivery down when automatic renewal fails
// silently, which is the case this task exists to cover. The links higher up
// the chain expire in years and belong to the CA; attributing them to the
// consumer would make the date point at someone who can't act on it.
//
// ONLY WHEN THERE WAS A HANDSHAKE: resp.TLS is nil over plain http (which
// config.ValidateCallbackURL already rejects at registration time, but
// delivery tests use it). And when the handshake FAILS — expired
// certificate, a chain that doesn't close — this doesn't get reached at all:
// `client.Do` already returned an error up above. The previous observation
// is NOT erased in that case, and that is deliberate: whoever reads it sees
// the last known date with an `observado_em` that ages, which is more
// information than a zeroed field.
//
// THE HTTP OUTCOME DOES NOT MATTER. A consumer that responds 500 presented
// the certificate the same way; the observation is about the certificate,
// not about what their application answered afterward.
func (e *Deliverer) observeCertificate(slug string, resp *http.Response, now time.Time) {
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return
	}
	// THE SAME `now` from the delivery, and not a second clock read: it's
	// the instant this handshake happened, and two reads would give the
	// timestamp a precision it doesn't have (see the note on the single
	// read, at the start of Deliver).
	e.observer.Record(slug, resp.TLS.PeerCertificates[0].NotAfter, now)
}
