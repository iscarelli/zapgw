// SINGLETON guard for sending (layer 3 of the high-availability design).
//
// THE PROBLEM, and it is not theoretical: in the active-passive pair, two
// instances alive at the same time mean the SAME message arriving twice on
// the client's phone, and Meta's 250/day quota burning double. It is not
// degradation, it is damage — and the most likely case is not even split
// brain: it is the standby that came up whole, serves pages, answers the
// health check, and was never promoted.
//
// THE RULE, copied from the design: the right to act does NOT come from
// having come up. It comes from a locally verifiable condition, checked ON
// EVERY send.
//
// WHY A FILE, and not a query to etcd from here: the outside supervisor is
// the one who contends for the lease (it already does `litestream restore`
// and brings the gateway up). If the gateway spoke etcd, it would gain a
// network dependency on the most critical path that exists here — and the
// whole design rests on the gateway NOT knowing that Litestream and etcd
// exist, because that is what makes step 1 cheap to undo. A file whose mtime
// the supervisor renews while it holds the lease delivers the same guarantee
// with a local `stat`.
//
// FAIL CLOSED, and the three doors close on the same side:
//   - file absent           -> DOES NOT send
//   - file too old           -> DOES NOT send
//   - error while checking   -> DOES NOT send
//
// The third is the one that matters to write down: "could not verify" is NOT
// "everything is fine". The two turn into silence if no one separates them,
// and here the second one sends a duplicate message to a real client. It is
// the same discipline this project applies to a blind monitor.
package outbound

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

const (
	// VarLeadershipFile points to the lease file that the supervisor renews
	// while THIS machine holds the leadership.
	//
	// EMPTY DISARMS THE GUARD, on purpose: it is the single-node install,
	// which is what runs today and what runs on any development machine.
	// Disarmed, the behavior is identical to before this guard existed.
	//
	// The obvious risk of this is someone deploying the pair and forgetting
	// the variable, and the guard ending up disarmed on BOTH nodes — which is
	// exactly the duplicate message it exists to prevent. Today that is
	// covered by ONE line in the journal, at startup (see cmd/zapgw/main.go,
	// "leadership guard ARMED/DISARMED").
	//
	// ONE LINE IN THE JOURNAL WOULD NOT BE ENOUGH — it only shows up for
	// whoever reads that machine's journal that day. That's why the guard's
	// state ALSO ships in GET /v1/estado and in `zapgw estado`, which is what
	// the panel and whoever operates it read (T-135, `lideranca` block — see
	// LeadershipInState below).
	//
	// This is the OLD (Portuguese) name. T-214 (2026-08-31) added
	// VarLeadershipFileNew as the English pair — this constant stays,
	// unchanged and still read, because it is the ONLY name an
	// already-deployed /etc/zapgw/env has; see NewLeadership.
	VarLeadershipFile = "ZAPGW_LIDERANCA_ARQUIVO"
	// VarLeadershipFileNew is the English name of VarLeadershipFile (T-214).
	// The NEW name wins when both are set — see config.EnvOrOld.
	VarLeadershipFileNew = "ZAPGW_LEADERSHIP_FILE"

	// VarLeadershipValidity is the maximum age of the lease file.
	//
	// REQUIRED when the guard is armed, and it HAS NO DEFAULT. That is
	// deliberate, and the reason is that the safe value depends on something
	// this process has no way of knowing.
	//
	// Calling `V` this validity, `R` the interval at which the supervisor
	// renews, `A` the time until one of our actions becomes VISIBLE to the
	// successor, and `T` the lease's TTL in the consensus store (etcd), the
	// two conditions are:
	//
	//	R < V        otherwise a scheduler delay turns into a pointless send refusal
	//	V + A < T    otherwise the two nodes think they are the titular AT THE SAME TIME
	//
	// The second is the one that matters, and it's the one this file got
	// wrong: the first version carried a 15 s default "as a starting number".
	// With a T of 15 s measured by the team that assembles the pair,
	// 15 + 1 = 16 > 15 — meaning the DEFAULT allowed overlap, which is exactly
	// the duplicate message on the client's phone this guard exists to
	// prevent.
	// *Fixed on 2026-08-18, the same day, via consumer review.*
	//
	// 🔴 And the fix was NOT swapping 15 for a smaller number: `T` is
	// configuration of an etcd this process doesn't even know about, so ANY
	// default is a guess about someone else's configuration — and the wrong
	// guess fails silently, allowing two titulars. Without a default, whoever
	// arms it is forced to do the math, and the error message carries the
	// formula.
	//
	// `A` is NOT the HTTP timeout. It's the time until the decision becomes
	// visible to the successor: here, the replication lag (Litestream's RPO,
	// measured at 1.00 s), because `ReserveIdempotency` commits locally
	// before sending and the successor only sees that after the replica
	// arrives.
	//
	// This is the OLD (Portuguese) name; VarLeadershipValidityNew (T-214) is
	// the English pair — see NewLeadership.
	VarLeadershipValidity = "ZAPGW_LIDERANCA_VALIDADE"
	// VarLeadershipValidityNew is the English name of VarLeadershipValidity
	// (T-214).
	VarLeadershipValidityNew = "ZAPGW_LEADERSHIP_VALIDITY"

	// refusalLogInterval keeps a lost leadership from turning into a
	// flood in the journal: under refusal EVERY request refuses, and the
	// repeated log hides the rest instead of informing.
	refusalLogInterval = 30 * time.Second
)

// Leadership answers just one question: can this machine ACT right now?
//
// The nil value is usable and answers "yes" — it's the disarmed guard. This
// exists so that a test that doesn't care about leadership (most of them)
// doesn't need to build anything.
type Leadership struct {
	// file empty = disarmed guard.
	file     string
	validity time.Duration

	// logf is injectable so the test can observe the suppression without
	// depending on the global log.
	logf func(format string, args ...any)

	mu        sync.Mutex
	lastLogAt time.Time
}

// NewLeadership reads the environment configuration.
//
// Returns an error ONLY when the validity comes written and unreadable — and
// in that case the gateway does NOT come up. This is deliberate, and it's the
// same criterion as IngressVia: a value nobody can interpret has to bring down
// the startup in front of whoever just edited the `env`, not turn into a
// silent default that only shows up on failover day.
// T-214: accepts VarLeadershipFileNew/VarLeadershipValidityNew in addition to
// the old names (new wins if both are set, independently for each of the
// two), and logs once per variable (config.WarnOldEnvVar) when the value
// that armed the guard came from an OLD name.
func NewLeadership(getenv func(string) string) (*Leadership, error) {
	if getenv == nil {
		return &Leadership{}, nil
	}
	fileRaw, fileOld := config.EnvOrOld(getenv, VarLeadershipFileNew, VarLeadershipFile)
	file := strings.TrimSpace(fileRaw)
	validityRaw, validityOld := config.EnvOrOld(getenv, VarLeadershipValidityNew, VarLeadershipValidity)
	raw := strings.TrimSpace(validityRaw)

	// The variable NAME to cite in an error — whichever spelling actually
	// carried the value, old or new — so the message points at the exact
	// line the operator needs to fix.
	fileName, validityName := VarLeadershipFileNew, VarLeadershipValidityNew
	if fileOld {
		fileName = VarLeadershipFile
	}
	if validityOld {
		validityName = VarLeadershipValidity
	}

	// Disarmed: the validity is not used, so it's neither required nor
	// validated. A single-node install cannot be forced to configure a pair.
	if file == "" {
		return &Leadership{}, nil
	}

	if raw == "" {
		return nil, fmt.Errorf(
			"zapgw: %s esta definida mas %s nao — a guarda de lideranca NAO tem default, de proposito.\n"+
				"  O valor seguro depende do TTL da concessao no etcd, que este processo nao conhece.\n"+
				"  Escolha V atendendo as DUAS: R < V  e  V + A < T\n"+
				"    R = intervalo em que o supervisor renova o arquivo\n"+
				"    A = tempo ate uma acao ficar VISIVEL ao sucessor (aqui, o atraso da replicacao)\n"+
				"    T = TTL da concessao no etcd\n"+
				"  Violar a segunda faz os dois nos se acharem titulares AO MESMO TEMPO — mensagem duplicada.",
			fileName, validityName)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return nil, fmt.Errorf("zapgw: %s = %q nao e uma duracao valida (ex.: %q)",
			validityName, raw, "8s")
	}
	if d <= 0 {
		return nil, fmt.Errorf("zapgw: %s = %q tem de ser positivo — duracao zero ou negativa recusaria TODO envio",
			validityName, raw)
	}
	config.WarnOldEnvVar(fileOld, VarLeadershipFile, VarLeadershipFileNew)
	config.WarnOldEnvVar(validityOld, VarLeadershipValidity, VarLeadershipValidityNew)
	return &Leadership{file: file, validity: d}, nil
}

// Armed says whether the guard is configured. Disarmed is not an error: it's
// the single-node install.
func (l *Leadership) Armed() bool {
	return l != nil && l.file != ""
}

// Holder answers whether this machine can act, and WHY not when it can't.
//
// The reason is not decoration: it goes into the log and into the response to
// the consumer. "I did not send" without saying why is the kind of refusal
// that makes someone restart the service at 3 a.m. looking for a defect that
// doesn't exist.
func (l *Leadership) Holder() (bool, string) {
	if !l.Armed() {
		return true, ""
	}
	info, err := os.Stat(l.file)
	if err != nil {
		// Absent and unreadable fall TOGETHER here, and both refuse. The
		// distinction between "the supervisor hasn't created it yet" and "I
		// can't read it" doesn't change the decision — in both cases this
		// machine did NOT PROVE it is the titular, and the only safe answer
		// is not to act.
		return false, fmt.Sprintf("nao consegui verificar a concessao de lideranca em %s: %v", l.file, err)
	}
	age := time.Since(info.ModTime())
	if age > l.validity {
		return false, fmt.Sprintf("a concessao de lideranca em %s esta velha (%s, limite %s) — esta maquina nao e o titular",
			l.file, age.Truncate(time.Millisecond), l.validity)
	}
	return true, ""
}

// LeadershipInState is the `lideranca` block of GET /v1/estado.
//
// WHY IT EXISTS, and the hole it closes is the worst one in this mechanism:
// deploying the pair and FORGETTING the variable on BOTH nodes. The guard
// ends up disarmed on both sides, both send, and the symptom is a duplicate
// message on the client's device. Until this task, the only signal for that
// was ONE line in the journal, at startup — visible only to whoever reads
// that machine's journal that day.
//
// It answers TWO questions, and merging them into a single boolean would
// lose the one that matters: "does a guard exist in this install?" and "is
// this machine the titular right now?".
type LeadershipInState struct {
	// Armed is the first question: is there a guard configured here?
	Armed bool `json:"armada"`
	// State uses the SAME vocabulary this package already uses for "is
	// there a valid measurement right now?" (CertObserved / NotApplicable).
	// New vocabulary would force the consumer to learn a second table for
	// the same idea.
	State string `json:"state"`
	// Holder is `null` when the guard is disarmed — NEVER `true`, and never
	// an absent field. `true` would make a single node look like it won an
	// election that never happened; absent would make the consumer have to
	// guess.
	Holder *bool `json:"titular"`
	// Reason only comes when Holder is false. It distinguishes "the lease is
	// stale" from "could not verify the lease" — both refuse the send (and
	// that's why they don't change `titular`), but only the second means the
	// machine is BLIND, and whoever operates it needs to know which of the
	// two it is.
	Reason *string `json:"reason"`
}

// inState builds the block. A nil receiver is a DISARMED guard — a caller
// that doesn't build it publishes "there is no guard here", never a made-up
// claim about leadership.
func (l *Leadership) inState() LeadershipInState {
	if !l.Armed() {
		return LeadershipInState{Armed: false, State: NotApplicable}
	}
	holder, reason := l.Holder()
	b := LeadershipInState{Armed: true, State: CertObserved, Holder: &holder}
	if !holder {
		m := reason
		b.Reason = &m
	}
	return b
}

// Require wraps a handler and refuses when this machine is not the titular.
//
// 503 + "retryable" is the right pair, and it's not an aesthetic choice: the
// consumer already retries on this class by contract, and by the time it
// retries the VIP will already have migrated to whoever actually holds the
// lease. A 4xx would make it GIVE UP on a message that just needed a
// different destination.
func (l *Leadership) Require(next http.Handler) http.Handler {
	if !l.Armed() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, reason := l.Holder(); !ok {
			l.logLeadershipRefusal(reason)
			respondError(w, http.StatusServiceUnavailable, "retryable",
				"esta instancia do gateway nao detem a lideranca do par e por isso nao envia; "+
					"repita — quem detem a concessao atende", 0)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logLeadershipRefusal emits at most one line per refusalLogInterval.
func (l *Leadership) logLeadershipRefusal(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if !l.lastLogAt.IsZero() && now.Sub(l.lastLogAt) < refusalLogInterval {
		return
	}
	l.lastLogAt = now
	write := l.logf
	if write == nil {
		write = log.Printf
	}
	write("zapgw: envio RECUSADO por lideranca: %s", reason)
}
