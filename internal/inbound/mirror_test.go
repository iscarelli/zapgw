package inbound

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
)

// THE RULE THAT IS NOT NEGOTIABLE: a 200 to Meta is IRREVERSIBLE. It
// redelivers for up to 36h if it doesn't receive a 2xx, and STOPS FOREVER
// if it receives a 200. So: 200 when a redelivery would NOT fix it,
// non-2xx when it would.
//
// The original ADR's first wording for this network said "never 500" and
// CREATED the bug the ADR existed to prevent.
func TestVerdictMirrorsConsumerSuccess(t *testing.T) {
	for _, status := range []int{200, 201, 202, 204} {
		v := ConsumerVerdict(status, nil)
		if v.StatusForMeta != http.StatusOK {
			t.Errorf("consumidor %d -> Meta %d, quero 200", status, v.StatusForMeta)
		}
		if v.Alarm {
			t.Errorf("consumidor %d nao devia alarmar", status)
		}
	}
}

func TestVerdictPropagatesTransientFailureSoMetaRedelivers(t *testing.T) {
	// A consumer with 5xx: their database went down, redelivering FIXES
	// it. Meta needs to receive a non-2xx to try again.
	for _, status := range []int{500, 502, 503, 504} {
		v := ConsumerVerdict(status, nil)
		if v.StatusForMeta/100 == 2 {
			t.Errorf("consumidor %d -> Meta %d — 2xx aqui PERDE a mensagem para sempre",
				status, v.StatusForMeta)
		}
		if v.StatusForMeta != http.StatusBadGateway {
			t.Errorf("consumidor %d -> Meta %d, quero 502", status, v.StatusForMeta)
		}
	}
}

func TestVerdictDoesNotTellMetaToRedeliverWhatTheConsumerRefused(t *testing.T) {
	// A consumer with 4xx: they UNDERSTOOD and refused. Redelivering
	// repeats the same failure for 36h — the same shape of defect as
	// treating a permanent error as transient. Responds 200 and ALARMS
	// LOUDLY, because the loss is permanent.
	for _, status := range []int{400, 401, 403, 404, 409, 422} {
		v := ConsumerVerdict(status, nil)
		if v.StatusForMeta != http.StatusOK {
			t.Errorf("consumidor %d -> Meta %d, quero 200", status, v.StatusForMeta)
		}
		if !v.Alarm {
			t.Errorf("consumidor %d tem de ALARMAR — a Meta nunca mais reenvia", status)
		}
	}
}

func TestVerdictTreatsConsumerOutageAsTransient(t *testing.T) {
	// A refused connection or a timeout: redelivering fixes it, IF it
	// comes back in time.
	v := ConsumerVerdict(0, errors.New("dial tcp: connection refused"))
	if v.StatusForMeta != http.StatusGatewayTimeout {
		t.Fatalf("StatusForMeta = %d, quero 504", v.StatusForMeta)
	}
	if v.StatusForMeta/100 == 2 {
		t.Fatal("2xx com consumidor fora do ar perde a mensagem para sempre")
	}
	// IMPORTANT 1 from the T10 review: Meta WILL redeliver here (504), so
	// this is not a permanent loss. Alarming here trains whoever operates
	// it to ignore the alarm, and then the alarm that matters (a real
	// permanent loss) disappears into the noise along with it.
	if v.Alarm {
		t.Error("consumidor fora do ar e transitorio (a Meta reenvia) — nao pode alarmar")
	}
}

// CRITICAL found in the T10 review, proven with a callback containing a
// query string. A transport error in Go is *url.Error, and its Error()
// carries the WHOLE URL. Reason goes to the handler's log; the CallbackURL
// is encrypted at rest so that a stolen backup doesn't reveal the
// consumers' topology. Leaking it into the log would make that encryption
// decorative.
func TestVerdictDoesNotLeakTheCallbackURLInTheReason(t *testing.T) {
	err := &url.Error{
		Op:  "Post",
		URL: "https://consumidor.interno:8443/webhooks/zapgw?token=SEGREDO-QUE-NAO-PODE-VAZAR",
		Err: errors.New("dial tcp 10.0.0.99:8443: connect: connection refused"),
	}

	v := ConsumerVerdict(0, err)

	for _, forbidden := range []string{
		"SEGREDO-QUE-NAO-PODE-VAZAR", "token=", "consumidor.interno", "10.0.0.99", "8443",
	} {
		if strings.Contains(v.Reason, forbidden) {
			t.Errorf("Reason vazou %q — texto completo: %s", forbidden, v.Reason)
		}
	}
	if v.Reason == "" {
		t.Error("Reason vazio — sem ele, 'onde a mensagem parou' vira investigacao")
	}
}

func TestVerdictDistinguishesTimeoutFromUnreachable(t *testing.T) {
	// Different categories: an expired deadline is a different thing from
	// a refused connection. Without this distinction the operator doesn't
	// know whether to raise the timeout or bring the app back up.
	timeout := ConsumerVerdict(0, fmt.Errorf("post: %w", context.DeadlineExceeded))
	refused := ConsumerVerdict(0, errors.New("connection refused"))

	if timeout.Reason == refused.Reason {
		t.Fatalf("timeout e recusa dao o mesmo Reason (%q)", timeout.Reason)
	}
}

// The default branch (1xx/3xx on a callback) responds 200 — PERMANENT
// LOSS — and therefore HAS to alarm. Without this test, a regression that
// forgot the Alarm here would pass verify silently.
func TestVerdictAlarmsOn1xxAnd3xxRanges(t *testing.T) {
	for _, status := range []int{100, 101, 102, 300, 301, 302, 307, 308, 399} {
		v := ConsumerVerdict(status, nil)
		if v.StatusForMeta != http.StatusOK {
			t.Errorf("status %d -> Meta %d, quero 200", status, v.StatusForMeta)
		}
		if !v.Alarm {
			t.Errorf("status %d responde 200 e NAO alarma — perda definitiva e silenciosa", status)
		}
		if v.Reason == "" {
			t.Errorf("status %d deu Reason vazio", status)
		}
	}
}

// The STATUS AXIS rule (err == nil), swept across the whole range: here the
// alarm exists if and only if we respond 2xx to Meta (permanent loss). This
// test is the executable rule — if someone changes a branch and forgets the
// alarm, or alarms where Meta still redelivers, it breaks.
//
// ON THE ERROR AXIS the equivalence does NOT hold, and that isn't
// inconsistency: the criterion is "needs a person" (mirror.go:15-21), and
// permanent loss is only its most expensive case. A certificate failure
// responds 504 — Meta still redelivers — and it alarms anyway, because no
// redelivery fixes a certificate.
func TestVerdictAlarmsIfAndOnlyIfTheLossIsDefinitive(t *testing.T) {
	for status := 100; status <= 599; status++ {
		v := ConsumerVerdict(status, nil)
		definitive := v.StatusForMeta >= 200 && v.StatusForMeta < 300
		realSuccess := status >= 200 && status < 300

		switch {
		case realSuccess && v.Alarm:
			t.Errorf("status %d: consumidor guardou, nao pode alarmar", status)
		case definitive && !realSuccess && !v.Alarm:
			t.Errorf("status %d: respondemos %d a Meta (definitiva) e NAO alarmamos", status, v.StatusForMeta)
		case !definitive && v.Alarm:
			t.Errorf("status %d: a Meta vai reenviar (%d) e mesmo assim alarmamos", status, v.StatusForMeta)
		}
		if v.Reason == "" {
			t.Errorf("status %d: Reason vazio", status)
		}
	}
}

func TestVerdictDoesNotTreatGarbageAsTransient(t *testing.T) {
	// A status outside the valid HTTP range isn't "transient 5xx": it's the
	// consumer returning garbage. Treating it as transient gives 36h of
	// identical redelivery and a loss at the end, with no signal at all.
	for _, status := range []int{600, 700, 999} {
		v := ConsumerVerdict(status, nil)
		if v.StatusForMeta != http.StatusOK {
			t.Errorf("status %d -> Meta %d, quero 200", status, v.StatusForMeta)
		}
		if !v.Alarm {
			t.Errorf("status %d nao alarmou — perda definitiva e silenciosa", status)
		}
	}
}

// THE PAIR: a certificate failure alarms and states the ACTION; a consumer
// being down does not alarm. A guard too broad here turns every consumer
// outage into an ALARM (and trains people to ignore it), and one too
// narrow lets an expired certificate die silently. Both failures only show
// up in production.
func TestCertificateVerdictAlarmsAndStatesTheActionWithoutConfusingItWithConsumerOutage(t *testing.T) {
	// The error arrives wrapped in *url.Error via net/http, same as on the wire.
	certErr := &url.Error{
		Op:  "Post",
		URL: "https://consumidor.interno/webhooks/zapgw",
		Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
	}

	v := ConsumerVerdict(0, certErr)
	if !v.Alarm {
		t.Error("certificado invalido nao alarmou — a Meta desiste em 36h e a perda vira definitiva sem sinal")
	}
	if v.StatusForMeta != http.StatusGatewayTimeout {
		t.Errorf("StatusForMeta = %d, quero 504 — manter a janela de reenvio aberta e o que da tempo de alguem consertar", v.StatusForMeta)
	}
	if !strings.Contains(v.Reason, "ACAO:") {
		t.Errorf("Reason = %q — ALARME sem acao e so um susto", v.Reason)
	}
	for _, forbidden := range []string{"consumidor.interno", "webhooks"} {
		if strings.Contains(v.Reason, forbidden) {
			t.Errorf("Reason vazou %q — texto: %s", forbidden, v.Reason)
		}
	}

	// The other side: a consumer outage stays WITHOUT an alarm.
	outage := &url.Error{
		Op:  "Post",
		URL: "https://consumidor.interno/webhooks/zapgw",
		Err: errors.New("dial tcp 10.0.0.99:443: connect: connection refused"),
	}
	if ConsumerVerdict(0, outage).Alarm {
		t.Error("consumidor fora do ar virou alarme — a Meta reenvia, e alarme por queda treina quem opera a ignorar o alarme")
	}
}

// DELIBERATE COUPLING (T-035): this function exists so that a change to
// ConsumerVerdict's thresholds without updating CounterKeys
// breaks the suite, instead of the two drifting apart silently — the same
// lesson from the mother-trap ("where else should this rule hold, and
// does it?") applied to a pair of functions in the SAME file.
func TestCounterKeysMatchConsumerVerdictBoundaries(t *testing.T) {
	for status := 100; status <= 599; status++ {
		v := ConsumerVerdict(status, nil)
		keys := CounterKeys(status, nil, v)

		wantDelivered := status >= 200 && status < 300
		wantRefused := status >= 400 && status < 500
		wantAlarm := v.Alarm && v.StatusForMeta == http.StatusOK

		hasDelivered := containsKey(keys, config.CounterDelivered)
		hasRefused := containsKey(keys, config.CounterRefusedByConsumer)
		hasAlarm := containsKey(keys, config.CounterDefinitiveLossAlarm)

		if hasDelivered != wantDelivered {
			t.Errorf("status %d: entregues=%v, quero %v (chaves=%v)", status, hasDelivered, wantDelivered, keys)
		}
		if hasRefused != wantRefused {
			t.Errorf("status %d: recusadas_pelo_consumidor=%v, quero %v (chaves=%v)", status, hasRefused, wantRefused, keys)
		}
		if hasAlarm != wantAlarm {
			t.Errorf("status %d: alarme_perda_definitiva=%v, quero %v (chaves=%v)", status, hasAlarm, wantAlarm, keys)
		}
		// A transient 5xx has no key at all in the closed vocabulary: it
		// cannot count as delivered NOR as rejected.
		if status >= 500 && status < 600 {
			if hasDelivered || hasRefused {
				t.Errorf("status %d (transitorio): chaves=%v — 5xx nao pode contar como entrega nem recusa", status, keys)
			}
		}
	}
}

// The ERROR AXIS (transport, no status): neither of the two
// consumer-status keys applies (there was no HTTP response from them), but
// the permanent-loss alarm is still governed by the SAME condition.
func TestCounterKeysOnErrorAxisOnlyCountAlarmWhenApplicable(t *testing.T) {
	certErr := &url.Error{
		Op: "Post", URL: "https://consumidor.interno/webhooks/zapgw",
		Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
	}
	v := ConsumerVerdict(0, certErr)
	keys := CounterKeys(0, certErr, v)
	// A certificate failure alarms but responds 504 (Meta still
	// redelivers) — it is NOT a permanent loss, so no key from this
	// vocabulary applies.
	if len(keys) != 0 {
		t.Errorf("falha de certificado (504, alarme sem perda definitiva): chaves=%v, quero nenhuma", keys)
	}

	outage := errors.New("dial tcp: connection refused")
	vOutage := ConsumerVerdict(0, outage)
	if keys := CounterKeys(0, outage, vOutage); len(keys) != 0 {
		t.Errorf("consumidor fora do ar: chaves=%v, quero nenhuma (a Meta reenvia sozinha)", keys)
	}
}

func containsKey(keys []string, target string) bool {
	for _, c := range keys {
		if c == target {
			return true
		}
	}
	return false
}

func TestVerdictAlwaysExplainsTheReason(t *testing.T) {
	// Requirement 4 of the spec: "where did the message stop?" in one
	// question. A verdict with no reason turns into an investigation.
	cases := []struct {
		status int
		err    error
	}{{200, nil}, {500, nil}, {404, nil}, {0, errors.New("timeout")}}

	for _, c := range cases {
		if v := ConsumerVerdict(c.status, c.err); v.Reason == "" {
			t.Errorf("status=%d err=%v deu Reason vazio", c.status, c.err)
		}
	}
}
