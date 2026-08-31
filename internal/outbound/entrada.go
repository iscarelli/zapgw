// WHERE this gateway's ingress is served from — the `entrada` block of
// GET /v1/estado and the SAME lines from `zapgw estado` (T-120).
//
// 🔴 THE LIMIT THAT DECIDES THE ENTIRE DESIGN, and it is not a lack of
// effort: THIS GATEWAY CANNOT SAY WHETHER IT IS REACHABLE FROM OUTSIDE. A
// request that does not arrive leaves no trace at all in here — it is
// literally the defect docs/IMPLANTACAO.md documents in the section "Se o
// caminho público cair, os quatro monitores INTERNOS ficam MUDOS": the journal does not
// record what did not arrive, the subscription with Meta stays correct, the
// counters just stop, and /v1/health keeps answering `200`. On 2026-08-06
// the fixed-IP link fell for ~9 min with all four monitors GREEN, and it was
// the CONSUMER who gave the warning.
//
// THAT IS WHY THERE IS NO — AND THERE CANNOT COME TO BE — AN `alcancavel`
// FIELD. It would be exactly the blind monitor that answers OK, published in
// the contract and multiplied by every consumer who reads it. The one who
// answers "is Meta managing to deliver?" is the probe that measures FROM
// OUTSIDE (implanta/sonda-publica.sh and the T-119 Worker); no measurement
// taken in here replaces it.
//
// WHAT THIS FILE PUBLISHES, THEN, ARE TWO SMALLER AND HONEST THINGS, and the
// difference between them is the axis of the file:
//
//   - `via` — WHERE the ingress is published from. This is CONFIGURATION,
//     never measurement: the gateway does not discover this on its own,
//     someone wrote it in the machine's `env`.
//   - `conector` — whether the `cloudflared` that publishes this route has a
//     live connection. THIS ONE IS ACTUALLY MEASURED — and that is why it
//     distinguishes "I measured and it's bad" from "I couldn't measure". The
//     second answer is `desconhecido` with `falhando_desde`, NEVER a zero
//     that looks like a verdict.
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

// The TWO environment variables of this slice. They are read in `main` (and
// in `zapgw estado`), never by a package under internal/ on its own — the
// SAME discipline as config.CounterRetentionDays: the environment
// enters through a function that receives `getenv`, and the value flows down
// as a parameter.
const (
	// VarIngressVia says where the ingress is published from: `tunel` or
	// `encaminhamento_de_porta`.
	VarIngressVia = "ZAPGW_ENTRADA_VIA"
	// VarConnectorReady is the URL of the `/ready` of the connector that
	// publishes this route (the `cloudflared`). EMPTY is a legitimate state
	// — see ConnectorNotConfigured.
	VarConnectorReady = "ZAPGW_CONECTOR_READY"
)

// The possible ingress paths, plus the answer for "nobody told me".
//
// THEY ARE WORDS, not a `tunel: true/false` boolean: the day a third path
// shows up (a rented reverse proxy, a second tunnel), the boolean would force
// inventing a second field and the consumer to read both together to know
// one single thing.
const (
	// ViaTunnel: ingress arrives through an OUTBOUND tunnel (Cloudflare
	// Tunnel, the 2026-08-06 switchover). Does not depend on a fixed IP,
	// does not depend on port forwarding, and crosses CGNAT.
	ViaTunnel = "tunel"
	// ViaPortForwarding: ingress arrives via `:443` forwarded at the
	// edge router, with DNS pointing at the home IP.
	ViaPortForwarding = "encaminhamento_de_porta"
	// ViaUnknown: nobody configured VarIngressVia. It is the SAME word
	// the token watchdog uses for "we don't know" (VerdictUnknown),
	// and not a new vocabulary for the same idea.
	//
	// 🔴 WHY EMPTY DOES NOT FALL BACK TO `encaminhamento_de_porta`, which was
	// the truth until 2026-08-06: because it would be a plausible GUESS, and
	// a plausible guess is more dangerous than a declared unknown — the same
	// decision behind `var versao = "desenvolvimento"` in cmd/zapgw/main.go,
	// which refuses to be born "0.0.0". A convenience default here would
	// publish, to every consumer, a path that is not the live one today.
	ViaUnknown = VerdictUnknown
)

// IngressVia resolves `via` from the environment.
//
// THE TWO ANSWERS ARE DIFFERENT ON PURPOSE, and that is the point of this
// function:
//
//   - EMPTY (nobody configured it) returns ViaUnknown, no error.
//     Booting the binary is mandatory; publishing a guess is not. An error
//     here would make the first deployment of this version BRING DOWN the
//     gateway just because /etc/zapgw/env did not yet have the new line.
//   - A VALUE NOT ON THE LIST returns AN ERROR, and the caller does not come
//     up (log.Fatalf in `main`). `tunnel`, `túnel`, `porta`, and the like
//     are not "close enough": they would be published in the contract as if
//     they were checked configuration. Failing closed at STARTUP puts the
//     defect in front of whoever just edited the `env` — which is the only
//     time it is cheap.
func IngressVia(getenv func(string) string) (string, error) {
	if getenv == nil {
		return ViaUnknown, nil
	}
	switch v := getenv(VarIngressVia); v {
	case "":
		return ViaUnknown, nil
	case ViaTunnel, ViaPortForwarding:
		return v, nil
	default:
		return "", fmt.Errorf("zapgw: %s = %q nao e um caminho de entrada conhecido — use %q ou %q (vazio publica %q)",
			VarIngressVia, v, ViaTunnel, ViaPortForwarding, ViaUnknown)
	}
}

// ConnectorAddress reads the URL of the connector's `/ready`. Empty = not
// configured, and that is NOT an error: an installation entering via port
// forwarding has no connector at all to ask.
//
// HERE THE SPACE IS TRIMMED AND IN IngressVia IT IS NOT, and the asymmetry
// is deliberate. In `via` the value GOES TO THE CONTRACT and the vocabulary
// is closed: " tunel" is a fat-fingered typo that needs to scream. Here
// there is no vocabulary — a line break coming from a heredoc would just
// make every question fail, and the block would publish `desconhecido` with
// `falhando_desde` climbing: an ALARM THAT LIES, pointing at a connector
// that is actually up. A false alarm is the fastest way to train someone to
// ignore this block.
func ConnectorAddress(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	return strings.TrimSpace(getenv(VarConnectorReady))
}

// The THREE states of ConnectorInState.
//
// `observado`/`desconhecido` are the SAME words this package already uses
// for the SAME question ("is there a valid measurement right now?") —
// CertObserved (certificado_do_callback, numero_na_meta) and
// VerdictUnknown (token watchdog). A new vocabulary would force the
// consumer to learn a second table for the same idea.
const (
	// ConnectorObserved: the gateway ASKED and the connector ANSWERED. The
	// number is in `conexoes_prontas`, and it can be ZERO — zero is a
	// legitimate measurement ("the connector is up and has no tunnel
	// mounted"), not a measurement failure.
	//
	// 🔴 WHY `observado` AND NOT `ok`: `ok` would be a JUDGMENT, and a
	// judgment about `conexoes_prontas: 0` would come out wrong in both
	// directions. The gateway publishes what it measured and when it
	// measured it; whoever alarms is whoever reads it — the same rule
	// CertificateInState already follows by NOT having an "expired" state.
	ConnectorObserved = CertObserved
	// ConnectorUnknown: COULDN'T MEASURE — either there was never a
	// measurement, or the last attempt failed, or the last response aged
	// out. `falhando_desde` separates "never asked" (null) from "asking and
	// getting nothing back".
	ConnectorUnknown = VerdictUnknown
	// ConnectorNotConfigured: VarConnectorReady is empty — nobody told the
	// gateway who to ask.
	//
	// IT IS A NAMED STATE AND THE BLOCK STAYS IN THE JSON, never a field
	// that disappears. A field that disappears breaks a strict parser, and
	// this project already paid for that (the `token_instagram` of v0.37.x,
	// which came to always be present precisely because of this). Besides,
	// "not configured" is a different answer from "couldn't measure", and
	// the two send someone to look in different places.
	ConnectorNotConfigured = "nao_configurado"
)

// ConnectorInState is the `entrada.conector` block.
//
// EVERY FIELD IS A POINTER WITHOUT omitempty, by the rule this package
// already applies in MetaToken and CounterInState: an explicit `null` says
// "there isn't one", and the key ALWAYS exists.
type ConnectorInState struct {
	State string `json:"estado"`
	// ReadyConnections is the `readyConnections` of the cloudflared `/ready`.
	//
	// `null` WHEN `desconhecido`, ALWAYS — and this is the field the T-120
	// mandatory mutation protects. Repeating the last measured number here,
	// or writing `0` when the question got no answer, would turn "couldn't
	// measure" into "measured and it's bad" on whoever reads it — and the
	// two send the person looking in opposite places (the network up to the
	// connector, or the connector itself).
	ReadyConnections *int `json:"conexoes_prontas"`
	// MeasuredAt is the last time the connector ANSWERED — not the last
	// attempt. It keeps pointing at the last real response even after the
	// state degrades to `desconhecido`: it is what says how long the gateway
	// has not heard from the connector, information that zeroing it would
	// destroy. Same rule as MetaToken.MeasuredAt.
	MeasuredAt *string `json:"medido_em"`
	// FailingSince is the FIRST attempt of the CURRENT sequence of
	// failures — `null` when the last attempt answered, or when there was
	// never an attempt. Only the sequence's first failure writes: the
	// following ones cannot push the date forward, otherwise it would say
	// "failing for a minute" after an hour of failing (the defect
	// Watchdog.record already avoids).
	FailingSince *string `json:"falhando_desde"`
}

// IngressInState is the published `entrada` block.
//
// ⚠️ WHAT IT DOES NOT PROMISE, and the sentence has to travel along with it
// (it is in the contract, docs/CONTRATO-CONSUMIDOR.md): `via` and `conector`
// describe WHERE the ingress is published from and WHETHER THE CONNECTOR IS
// UP — they do NOT promise that Meta is managing to deliver. See the header
// of this file.
type IngressInState struct {
	Via       string           `json:"via"`
	Connector ConnectorInState `json:"conector"`
	// LastWebhookAt is the SAME timestamp as
	// `contadores.recebidas.ultimo_em`, not a second clock (T-120 was
	// explicit: reuse, don't create).
	//
	// WHY IT REPEATS HERE, then: because it is the only piece of data in
	// this response that lets the consumer conclude SILENCE on their own —
	// and "how long has nothing come in?" is the question they ask when they
	// suspect the public path, not when they are reading the counters table.
	// These are two fields with the SAME value, for the same reason (and
	// with the same care) as `dia`/`dia_utc`: they come from the SAME
	// reading, so there is no path by which they diverge.
	LastWebhookAt *string `json:"ultimo_webhook_em"`
}

// IngressSource is what BuildState needs to publish the `entrada` block.
//
// IT COMES IN AS A MANDATORY POSITIONAL PARAMETER, and this is the T-111
// lesson applied again: whoever assembles a state has to DECLARE where the
// ingress comes from. Omitting it does not compile. And the ZERO VALUE is
// the most honest one there is — `via: desconhecido` and `conector:
// nao_configurado` —, so a slip fails toward "we don't know", never toward
// an assertion.
type IngressSource struct {
	Via string
	// Connector CAN BE nil: the struct's zero value is a valid state
	// (nao_configurado), and ConnectorProbe.Read handles the nil receiver.
	//
	// A CONCRETE POINTER, not an interface, on purpose: a nil interface
	// stored in a struct field is the "typed nil" pitfall
	// StateHandler.renewer already had to work around by hand
	// (estado_handler.go). Here the problem does not exist because the type
	// never becomes an interface.
	Connector *ConnectorProbe
}

// inState translates the source into the published block. An empty `via`
// becomes ViaUnknown — never an empty string in the JSON, which the
// consumer would have to distinguish from absence to answer the same
// question.
func (f IngressSource) inState() IngressInState {
	via := f.Via
	if via == "" {
		via = ViaUnknown
	}
	return IngressInState{Via: via, Connector: f.Connector.Read()}
}

// connectorProbeInterval is how often the `/ready` is asked. The
// connector is LOCAL (another machine on the same network), so the question
// is cheap — but it does not happen on the consumer's read, for the SAME
// reason as the token watchdog (vigia.go): the consumer's dashboard cannot
// hang on a third party's latency, and measuring only when someone looks
// leaves the system blind precisely when it is quiet.
const connectorProbeInterval = time.Minute

// connectorMeasurementValidity is how long the last response keeps being
// presented as `observado`. Past that the block degrades to `desconhecido`,
// with `medido_em` intact.
//
// WHY IT EXISTS, and the argument is the same as verdictValidity: a cache
// that never expires is a lie with a timestamp. If the probe's goroutine
// dies, a frozen `observado` would paint "connector up" forever — which is
// exactly the blind monitor this task exists to not build.
//
// THREE TICKS, for the same reason as there: one missed tick is normal
// noise; three in a row is already a problem. SMALLER than the interval,
// every measurement would be born expired.
const connectorMeasurementValidity = 3 * connectorProbeInterval

// connectorProbeDeadline caps ONE question. Without it, a `/ready` that
// accepts the connection and never answers would hang the probe's goroutine
// forever — and the probe is ONE for the whole process.
const connectorProbeDeadline = 5 * time.Second

// readyBodyCap is the ceiling on the body read from `/ready`. The
// legitimate JSON is a bit over a hundred bytes; the ceiling exists so a
// wrongly pointed address (a `/` on some random server, a proxy that returns
// HTML) does not turn into memory.
const readyBodyCap = 64 << 10

// connectorMeasurement is the stored state. It does not leave this package:
// whoever reads from outside gets ConnectorInState, already with expiration
// applied.
type connectorMeasurement struct {
	connections int
	// measuredAt is the last time the connector ANSWERED — zero when it never
	// answered.
	measuredAt time.Time
	// failingSince is the FIRST attempt of the current sequence of
	// failures — zero when the last attempt answered.
	failingSince time.Time
}

// ConnectorProbe asks the connector's `/ready`, at its own pace, and stores
// the last response.
type ConnectorProbe struct {
	url    string
	client *http.Client

	interval time.Duration
	validity time.Duration
	// now is injectable only for testing — without it, proving that an
	// old measurement degrades would require sleeping three minutes. Same
	// role as the Watchdog's field of the same name.
	now func() time.Time

	// mu protects `m`: the timer's goroutine writes and every HTTP request
	// reads. This project already paid a Critical for shared state without a
	// lock in a handler (docs/ARMADILHAS.md, "Go / concorrência").
	mu sync.Mutex
	m  connectorMeasurement
}

// NewConnectorProbe assembles the probe INERT: it only starts measuring in
// Start (or in a standalone Measure, which is what `zapgw estado` does).
// An empty URL returns a probe that never talks to anyone and always reads
// `nao_configurado`.
func NewConnectorProbe(url string) *ConnectorProbe {
	return &ConnectorProbe{
		url: url,
		// Its own client, not http.DefaultClient: the default has no
		// deadline at all, and this probe's deadline is its own business.
		// NO TLS option is touched here — there is no mode to skip
		// certificate verification in this binary (CLAUDE.md), and a
		// `/ready` on `https` is verified like any other destination.
		client:   &http.Client{Timeout: connectorProbeDeadline},
		interval: connectorProbeInterval,
		validity: connectorMeasurementValidity,
		now:      time.Now,
	}
}

// Start starts the timer's goroutine: one tick RIGHT AWAY and one per
// interval, forever. Same mold (and same recover) as the token watchdog and
// the purges — without it, a panic kills the goroutine and the probe stops
// measuring SILENTLY for the rest of the process's life, leaving the last
// measurement frozen.
func (s *ConnectorProbe) Start() {
	if s == nil || s.url == "" {
		// Without an address there is nothing to measure, and a goroutine
		// waking up to do nothing would just be noise.
		return
	}
	go func() {
		for {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("zapgw: sonda do conector sofreu panico (recuperado): %v", rec)
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
// SAME reason as Watchdog.CheckInstance: the measurement lives in the
// SERVER process's memory, and a command-line process that just came to
// life would always read `desconhecido` — which on the screen of someone in
// an incident looks like a broken probe. Asking `/ready` is a pure READ, so
// the status command can do it without mutating anything (unlike the
// Instagram token renewer, which stays nil there for that reason).
func (s *ConnectorProbe) Measure(ctx context.Context) {
	if s == nil || s.url == "" {
		return
	}
	n, err := s.ask(ctx)
	s.record(n, err)
}

// ask makes ONE call to `/ready`.
//
// 🔴 THE BODY DECIDES, NOT THE STATUS, and this choice is the heart of the
// task.
//
// The case that governs it is `readyConnections: 0` — the tunnel fell —,
// which is the most important measurement this probe can bring. IT IS NOT
// CHECKED HERE what HTTP status cloudflared answers with in that case (the
// only response this session had in hand was a healthy `/ready`, with
// `"status":200` in the body), and the decision exists precisely so the
// question does not matter: if the body carries the field, there was a
// MEASUREMENT, whatever the status is.
//
// Tying this to `200` would be betting on an UNCHECKED third-party detail,
// and losing the tunnel's fall in the most expensive direction — it would
// turn into "couldn't measure", hiding the fall behind silence.
//
// FAILURE IS ONLY: couldn't reach it, couldn't read it, or the response did
// not carry the field.
func (s *ConnectorProbe) ask(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, connectorProbeDeadline)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return 0, fmt.Errorf("montar o pedido ao /ready do conector (%s): %w", s.url, err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("perguntar ao /ready do conector (%s): %w", s.url, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, readyBodyCap))
		_ = resp.Body.Close()
	}()

	// A pointer, not an int: it is what distinguishes "answered 0
	// connections" (measurement) from "answered something else, without the
	// field" (measurement failure). With a plain int, a proxy's HTML
	// silently decoded would turn into `0` — a made-up verdict, which is the
	// defect this task was written to not have.
	var body struct {
		ReadyConnections *int `json:"readyConnections"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, readyBodyCap)).Decode(&body); err != nil {
		return 0, fmt.Errorf("ler o /ready do conector (%s, HTTP %d): %w", s.url, resp.StatusCode, err)
	}
	if body.ReadyConnections == nil {
		return 0, fmt.Errorf("o /ready do conector (%s) respondeu HTTP %d sem o campo readyConnections",
			s.url, resp.StatusCode)
	}
	return *body.ReadyConnections, nil
}

// record applies the result of ONE attempt. Same asymmetry as
// Watchdog.record: an attempt that did not answer does NOT erase the last
// response — it only starts (or continues) the sequence of failures.
func (s *ConnectorProbe) record(n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if err != nil {
		if s.m.failingSince.IsZero() {
			s.m.failingSince = now
		}
		return
	}
	s.m.connections, s.m.measuredAt, s.m.failingSince = n, now, time.Time{}
}

// Read returns the `entrada.conector` block — ALWAYS from what has already
// been measured, never talking to the connector.
//
// A nil RECEIVER AND AN EMPTY URL are the SAME state (`nao_configurado`) on
// purpose: whoever assembles a State with no probe at all (a test, a command
// that did not build one) cannot receive a block that looks like a
// measurement.
func (s *ConnectorProbe) Read() ConnectorInState {
	if s == nil || s.url == "" {
		return ConnectorInState{State: ConnectorNotConfigured}
	}

	s.mu.Lock()
	m := s.m
	s.mu.Unlock()

	r := ConnectorInState{
		State:        ConnectorUnknown,
		MeasuredAt:   stamp(m.measuredAt),
		FailingSince: stamp(m.failingSince),
	}
	// ORDER MATTERS: an ongoing sequence of failures brings the state down
	// even if the last response is still within its validity. The opposite
	// would make the block say `observado` while `falhando_desde` screams
	// the opposite, two contradicting statements in the same response.
	switch {
	case !m.failingSince.IsZero(), m.measuredAt.IsZero(),
		s.now().Sub(m.measuredAt) > s.validity:
		return r
	}
	connections := m.connections
	r.State, r.ReadyConnections = ConnectorObserved, &connections
	return r
}
