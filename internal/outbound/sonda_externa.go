// THE VERDICT OF THE EXTERNAL PROBE, brought inside `GET /v1/estado`
// (`alcance_externo`, T-121).
//
// WHY THIS EXISTS: `consumer-b` reaches the gateway through the INTERNAL
// door, and for them "the gateway responds" and "the public entrance is up"
// are the SAME question until the day they stop being — which is exactly
// what happened on 2026-08-06, when the tunnel fell for ~9 min with the
// internal service perfect and the four monitors of the time (journal, Meta
// subscription, channel, health) ALL green. The probe that measures this
// FROM OUTSIDE already exists (`sonda-worker/`, T-119) and already publishes
// a public, unauthenticated verdict (see docs/CONTRATO-CONSUMIDOR.md,
// section "Duas perguntas diferentes"). Owner's request, 2026-08-07: *"isn't
// the ideal for you to query the probe and return that information? That
// way they only talk to you."* — the same discipline as CLAUDE.md §
// "NINGUÉM fala direto com a Meta", applied here to a third party that
// isn't Meta but that the consumer would have had to learn to query on
// their own.
//
// 🔴 WHAT THIS FILE DOES NOT DO, and it's a decision, not an oversight: it
// does NOT replace the public probe URL that the contract already
// documents. The case where the probe matters most — the GATEWAY GONE
// SILENT — is exactly the case where asking the gateway returns nothing. A
// status that shares the failure domain of what it monitors isn't status.
// The two things coexist: this field is convenience when the gateway
// responds; the direct URL is the one that survives our own outage. See the
// same sentence in entrada.go about `conector`.
//
// THE MOLD IS THE SAME AS vigia.go AND entrada.go (ConnectorProbe), and for
// the SAME reason written there: neither measure at request time (the
// handler would get stuck on the latency and uptime of a FOURTH service —
// the gateway, Meta, and now the probe), nor derive it from traffic
// (traffic silence is indistinguishable from a stopped probe). That's why
// there's a background TIMER with cache, and the GET /v1/estado handler
// (estado_handler.go) only reads memory.
//
// THE WORD `nao_consegui_verificar` IS DELIBERATE, and does NOT reuse
// VerdictUnknown/ConnectorUnknown ("desconhecido") that the rest
// of this package uses for the SAME structural idea ("no valid measurement
// right now"). Here the consumer will AUTOMATE AN ALARM on top of this
// specific field to decide whether to page someone (T-121, explicit
// request) — and the task flagged as a CENTRAL POINT that this reading
// failing must NEVER turn into `down` (the verdict the probe uses for "the
// entrance is down") nor into silence (field absent). An unambiguous name,
// that doesn't resemble "down" nor the generic "desconhecido" used in other
// blocks, is the defense against the consumer coding
// `if alcance_externo != "observado" { TOTAL_OUTAGE }` and confusing "I
// couldn't ask" with "you are down".
package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// VarExternalProbeURL is the ONLY environment variable of this slice — read
// in `main` (and in `zapgw estado`), never on its own inside internal/, by
// the SAME discipline as VarConnectorReady (entrada.go): the environment
// enters through a function that receives `getenv`, and the value flows
// down as a parameter.
//
// ABSENT IS A LEGITIMATE STATE, not an error: an installation that doesn't
// yet have the public probe running (or whose owner hasn't passed the URL
// yet) boots normally and publishes `nao_configurado` — the SAME pattern as
// `conector` in T-120.
const VarExternalProbeURL = "ZAPGW_SONDA_EXTERNA_URL"

// ExternalProbeURL reads the probe URL from the environment. Empty = not
// configured, and that is NOT an error — see the comment on
// VarExternalProbeURL.
//
// THE SPACE IS TRIMMED, as in ConnectorAddress and for the SAME reason:
// there's no closed vocabulary here (unlike IngressVia) for a heredoc line
// break to complain against — without the trim, it would just make every
// read fail and the block would publish `nao_consegui_verificar` forever,
// pointing at a probe that was actually never asked correctly.
func ExternalProbeURL(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	return strings.TrimSpace(getenv(VarExternalProbeURL))
}

// The THREE states of ExternalReachInState.
const (
	// ReachStateObserved: the gateway ASKED the external probe and it
	// RESPONDED with readable JSON. Its verdict (literal, "up"/"down" or
	// whatever the probe sends) is in `veredito`.
	//
	// SAME WORD as CertObserved (certificado_do_callback, numero_na_meta):
	// the question is the same — "is there a valid measurement right now?"
	// — and a new vocabulary just for this block would force the consumer
	// to learn a second table for the same idea.
	ReachStateObserved = CertObserved
	// ReachStateNotConfigured: VarExternalProbeURL is empty — nobody
	// told the gateway who to ask. SAME word and SAME reason as
	// ConnectorNotConfigured (entrada.go): "not configured" is a different
	// answer from "couldn't measure", and the two point to different
	// things to check (a missing `env` vs. a broken network or probe).
	ReachStateNotConfigured = ConnectorNotConfigured
	// ReachStateCouldNotVerify: URL configured, but the LAST
	// attempt did not produce a trustworthy reading — no response, no
	// readable JSON, or the expected field missing. Its OWN word, and the
	// reason is in the file header: this field can never turn into `down`
	// nor into silence, and reusing "desconhecido" would risk the
	// consumer treating this response as the SAME thing as
	// `token_meta.veredito` or `entrada.conector.estado`, when the
	// question they're automating on top of it is a different one.
	ReachStateCouldNotVerify = "nao_consegui_verificar"
)

// SourceExternalProbe is the literal published in `alcance_externo.fonte` when
// `estado == observado`.
//
// TODAY THERE'S ONLY ONE MECHANISM (asking the URL from
// VarExternalProbeURL), and the field might look like ceremony because of
// that — but it follows the SAME shape as ObservedValue.Source
// (numero_na_meta), which exists for the day a SECOND source shows up (for
// example, the probe itself pushing the verdict instead of the gateway
// having to ask) without forcing the consumer to reinterpret a contract
// it's already published.
const SourceExternalProbe = "sonda_externa"

// ExternalReachInState is the `alcance_externo` block (T-121).
//
// THE SHAPE IS THE SAME AS ObservedValue (numero_na_meta) — state, the
// measured value, when and from where —, just with the names this specific
// field calls for (`veredito` instead of `valor`, `medido_em` instead of
// `observado_em`, to match the vocabulary MetaToken and ConnectorInState
// already use for "the last time a real response arrived").
//
// EVERY FIELD BESIDES `estado` IS A POINTER WITHOUT omitempty, by the same
// rule as this whole package: explicit `null` says "there is none", and the
// key is ALWAYS present — a field that disappears from the JSON would force
// the consumer to distinguish "absent" from "null" to answer the same
// question (and this project already paid for that: `token_instagram` in
// v0.37.x).
type ExternalReachInState struct {
	State string `json:"estado"`
	// Verdict is the literal the external probe answered (today, "up" or
	// "down" — see docs/CONTRATO-CONSUMIDOR.md) — WITHOUT TRANSLATION, for
	// the same reason as NumberAtMeta.MessageLimit: translating would
	// hide a new word the probe might use tomorrow, returning something
	// plausible for a value nobody checked. `null` whenever `estado` !=
	// observado.
	Verdict *string `json:"veredito"`
	// MeasuredAt is the last time the external probe actually RESPONDED —
	// not the last ATTEMPT. It keeps pointing to that last real response
	// even after the state degrades to nao_consegui_verificar: it's what
	// says how long the gateway hasn't heard from the probe, information
	// that zeroing it would destroy. Same rule as MetaToken.MeasuredAt and
	// ConnectorInState.MeasuredAt.
	MeasuredAt *string `json:"medido_em"`
	// Source is SourceExternalProbe when `estado == observado`, `null` in the
	// other two states — see the comment on SourceExternalProbe.
	Source *string `json:"fonte"`
}

// externalProbeInterval is how often the configured URL is asked.
//
// ~60s, as the task asks: the external probe measures every 5 min
// (docs/CONTRATO-CONSUMIDOR.md), so reading faster than that brings no new
// information — it just spends one extra HTTP call per minute against a
// third party that isn't ours.
const externalProbeInterval = 60 * time.Second

// externalMeasurementValidity is for how long the last response keeps being
// presented as `observado`. Past that (or with an attempt in progress
// failing) the block degrades to `nao_consegui_verificar`, with `medido_em`
// intact.
//
// THREE TICKS, the SAME relation to the interval that vigia.go and
// entrada.go already use: one missed tick is normal noise; three in a row
// failing is already a problem. LOWER than the interval, and every
// measurement would be born expired.
const externalMeasurementValidity = 3 * externalProbeInterval

// externalProbeDeadline bounds ONE question. Without it, an external probe
// that accepts the connection and never answers would hold the goroutine
// forever — and it's UNIQUE for the whole process (T-121, Verify: "the
// handler does no network I/O" depends on this deadline living in the
// READER, not in the handler, which does no I/O at all).
const externalProbeDeadline = 5 * time.Second

// externalProbeBodyCap is the read cap on the body. The documented
// JSON (`{"status":"up"}`) is a handful of bytes; the cap exists so that a
// URL pointed at the wrong thing (an HTML page, a misconfigured proxy)
// doesn't turn into memory — same reason as readyBodyCap in
// entrada.go.
const externalProbeBodyCap = 4 << 10

// externalReachMeasurement is the stored state. It doesn't leave this package:
// whoever reads it from outside gets ExternalReachInState, already with
// expiration applied.
type externalReachMeasurement struct {
	verdict string
	// measuredAt is the last time the external probe RESPONDED — zero when
	// it never responded.
	measuredAt time.Time
	// failingSince is the FIRST attempt of the current run of failures —
	// zero when the last attempt responded.
	failingSince time.Time
}

// ExternalProbe asks the URL configured in ZAPGW_SONDA_EXTERNA_URL, at its
// own pace, and stores the last response — the BACKGROUND READER that T-121
// requires (mold of vigia.go/entrada.go).
type ExternalProbe struct {
	url    string
	client *http.Client

	interval time.Duration
	validity time.Duration
	// now is injectable only for testing — without it, proving that an
	// old measurement degrades would require sleeping three minutes. Same
	// role as the field of the same name in Watchdog and in ConnectorProbe.
	now func() time.Time

	// mu protects `m`: the timer goroutine writes, and every HTTP request
	// to GET /v1/estado reads. This project already paid a Critical for
	// shared state with no lock in a handler (docs/ARMADILHAS.md, "Go /
	// concorrência").
	mu sync.Mutex
	m  externalReachMeasurement
}

// NewExternalProbe builds the probe INERT: it only starts measuring in
// Start (or in a standalone Measure, which is what `zapgw estado` does).
// An empty URL returns a probe that never talks to anyone and always reads
// `nao_configurado`.
func NewExternalProbe(url string) *ExternalProbe {
	return &ExternalProbe{
		url: url,
		// Own client, not http.DefaultClient: the default has no deadline
		// at all. NO TLS option is touched here — there's no mode to skip
		// certificate verification in this binary (CLAUDE.md), and a probe
		// on `https` is verified like any other destination.
		client:   &http.Client{Timeout: externalProbeDeadline},
		interval: externalProbeInterval,
		validity: externalMeasurementValidity,
		now:      time.Now,
	}
}

// Start brings up the timer goroutine: one tick RIGHT AWAY and then one
// per interval, forever. Same mold (and same recover) as the token watchdog,
// the connector probe, and the purges — without it, a panic kills the
// goroutine and the probe stops measuring IN SILENCE for the rest of the
// process's life, leaving the last measurement frozen.
func (s *ExternalProbe) Start() {
	if s == nil || s.url == "" {
		// With no address there's nothing to measure, and a goroutine
		// waking up to do nothing would just be noise.
		return
	}
	go func() {
		for {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("zapgw: sonda externa sofreu panico (recuperado): %v", rec)
					}
				}()
				s.Measure(context.Background())
			}()
			time.Sleep(s.interval)
		}
	}()
}

// Measure runs ONE tick.
//
// WHO NEEDS IT STANDALONE IS `zapgw estado` (cmd/zapgw/estado.go), for the
// SAME reason as ConnectorProbe.Measure: the measurement lives in the
// SERVER process's memory, and a command-line process that just started
// would always read `nao_consegui_verificar` — which, on the screen of
// someone in an incident, looks like the probe itself is broken. Asking is
// pure READ, so the status command can do it without mutating anything.
func (s *ExternalProbe) Measure(ctx context.Context) {
	if s == nil || s.url == "" {
		return
	}
	v, err := s.ask(ctx)
	s.record(v, err)
}

// ask makes ONE call to the external probe and returns the literal
// `status`.
//
// FAILURE IS ONLY: couldn't reach it, couldn't read the JSON, or the
// response didn't carry the `status` field. NONE of these three turns into
// "down" — who decides what the ABSENCE of a reading means is Read(),
// publishing ReachStateCouldNotVerify, never this method inventing
// a verdict.
func (s *ExternalProbe) ask(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, externalProbeDeadline)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return "", fmt.Errorf("montar o pedido a sonda externa (%s): %w", s.url, err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("perguntar a sonda externa (%s): %w", s.url, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, externalProbeBodyCap))
		_ = resp.Body.Close()
	}()

	// Pointer, not a plain string: it's what distinguishes "answered with
	// an empty status" (which would already be odd) from "answered
	// something else, without the field" — same discipline as
	// `ReadyConnections *int` in entrada.go.
	var body struct {
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, externalProbeBodyCap)).Decode(&body); err != nil {
		return "", fmt.Errorf("ler a sonda externa (%s, HTTP %d): %w", s.url, resp.StatusCode, err)
	}
	if body.Status == nil || strings.TrimSpace(*body.Status) == "" {
		return "", fmt.Errorf("a sonda externa (%s) respondeu HTTP %d sem o campo `status`", s.url, resp.StatusCode)
	}
	return *body.Status, nil
}

// record applies the result of ONE attempt. Same asymmetry as
// Watchdog.record and ConnectorProbe.record: an attempt that didn't
// respond does NOT erase the last response — it only starts (or continues)
// the failure run, and only the FIRST failure of the run writes
// `failingSince`.
func (s *ExternalProbe) record(verdict string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if err != nil {
		if s.m.failingSince.IsZero() {
			s.m.failingSince = now
		}
		return
	}
	s.m.verdict, s.m.measuredAt, s.m.failingSince = verdict, now, time.Time{}
}

// Read returns the `alcance_externo` block — ALWAYS from what's already been
// measured, never talking to the external probe. It's what guarantees the
// T-121 Verify: the GET /v1/estado handler (estado_handler.go) calls this
// and only this, and this method does no I/O at all.
//
// NIL RECEIVER AND EMPTY URL are the SAME state (`nao_configurado`) on
// purpose: whoever builds an State with no probe at all (a test, a
// command that didn't build one) can't receive a block that looks like a
// measurement.
func (s *ExternalProbe) Read() ExternalReachInState {
	if s == nil || s.url == "" {
		return ExternalReachInState{State: ReachStateNotConfigured}
	}

	s.mu.Lock()
	m := s.m
	s.mu.Unlock()

	r := ExternalReachInState{
		State:      ReachStateCouldNotVerify,
		MeasuredAt: stamp(m.measuredAt),
	}
	// THE ORDER MATTERS, as in ConnectorProbe.Read: a run of failures in
	// progress takes down the state even if the last good response is
	// still within validity. The opposite would make the block say
	// `observado` while the next attempt has already been failing for
	// minutes.
	switch {
	case !m.failingSince.IsZero(), m.measuredAt.IsZero(), s.now().Sub(m.measuredAt) > s.validity:
		return r
	}
	verdict := m.verdict
	source := SourceExternalProbe
	r.State, r.Verdict, r.Source = ReachStateObserved, &verdict, &source
	return r
}
