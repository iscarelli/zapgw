// The matrix: translates the consumer's verdict into what Meta hears.
//
// THE RULE THAT GOVERNS EVERYTHING: a 200 to Meta is IRREVERSIBLE. It
// redelivers for up to 36h if it doesn't receive a 2xx, and STOPS FOREVER if
// it receives a 200. So the rule is not "never return 500" — it is:
//
//	answer 200 when a redelivery would NOT fix it,
//	answer non-2xx when a redelivery WOULD fix it.
//
// And the 36h are NOT a safety net for a long outage: they cover a restart
// that takes seconds. On the STATUS axis, every row with Alarm=true is one
// where we respond 200, meaning one where Meta NEVER redelivers again: if the
// alarm doesn't fire, the loss is permanent and no one finds out. On the
// ERROR axis there is one alarm that responds 504 — the certificate failure —
// and the next paragraph explains why it is not an exception to the
// criterion.
//
// ALARM = NEEDS A PERSON. Permanent loss (we respond 2xx to Meta, which NEVER
// redelivers again) is ONE CASE of this criterion — the most expensive one,
// because only a person can recover what was lost — but it is not the
// definition of it: any situation where only a person can resolve it also
// alarms, even with no loss involved at all. A case where Meta will still
// redeliver (and so no one needs to act right now) does NOT alarm: alarming
// on what fixes itself trains whoever operates the system to ignore the
// alarm, and then the alarm that matters disappears into the noise along
// with it.
//
// A consumer down for a long time is NOT covered by this signal: when Meta's
// redeliveries expire, it simply stops, and the gateway never even finds out.
// What covers that is the per-instance probe, on its own plan — it is not
// here, and this absence is deliberate, not an oversight.
package inbound

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"

	"github.com/iscarelli/zapgw/internal/config"
)

// Verdict is what we decide to answer Meta, and why.
type Verdict struct {
	StatusForMeta int
	Alarm         bool
	Reason        string
}

// ConsumerVerdict translates the delivery result (HTTP status returned by
// the consumer, or an error if it didn't even respond) into the response we
// send Meta.
func ConsumerVerdict(status int, err error) Verdict {
	if err != nil {
		// TLS is its OWN case, not a consumer-is-down under a different name.
		//
		// The difference that justifies the alarm: a consumer outage fixes
		// itself (it comes back, Meta redelivers within the window); an
		// expired, self-signed, or unknown-CA certificate does NOT fix
		// itself — every Meta redelivery redoes the SAME handshake and hits
		// the SAME rejection, until it gives up. Then the loss is permanent
		// and no one was warned.
		//
		// The status stays 504 (not 200) on purpose: the open redelivery
		// window is exactly what gives someone time to renew the
		// certificate or register the CA, and have the messages still get
		// through. An alarm alongside 504 doesn't contradict this file's
		// criterion — it IS the criterion: ALARM = needs a person.
		//
		// NO THRESHOLD, unlike the handler's 413: there, an isolated event
		// still had a chance on its own, and only repetition turned into
		// loss. Here the FIRST occurrence already needs a person, and
		// delaying the warning would trade noise for silence in the one
		// case where silence costs a message.
		if errors.Is(err, config.ErrInvalidCABundle) {
			return Verdict{
				StatusForMeta: http.StatusGatewayTimeout,
				Alarm:         true,
				Reason: "the CA bundle registered on this instance carries no certificate at all," +
					" so NO delivery from it goes out — and no Meta redelivery fixes this." +
					" ACTION: re-register the instance with a valid PEM --bundle-ca, or with no bundle if the consumer uses a public CA",
			}
		}
		if isCertificateFailure(err) {
			return Verdict{
				StatusForMeta: http.StatusGatewayTimeout,
				Alarm:         true,
				Reason: "the consumer's CERTIFICATE was refused in TLS (expired, self-signed, wrong hostname," +
					" or issued by a CA this gateway doesn't know). This does NOT fix itself:" +
					" every Meta redelivery hits the same refusal, and once it gives up the message is lost for good." +
					" ACTION: renew/fix the consumer's certificate, or register its CA on the instance (--bundle-ca)." +
					" Turning off verification is NOT an option in this gateway",
			}
		}
		// Down, timeout, DNS: redelivering fixes it IF it comes back in
		// time. Does not alarm: Meta will still redeliver, so it isn't a
		// permanent loss.
		return Verdict{
			StatusForMeta: http.StatusGatewayTimeout,
			Alarm:         false,
			Reason:        errorReason(err),
		}
	}

	switch {
	case status >= 200 && status < 300:
		return Verdict{
			StatusForMeta: http.StatusOK,
			Reason:        fmt.Sprintf("consumer stored it (%d)", status),
		}

	case status >= 500 && status < 600:
		return Verdict{
			StatusForMeta: http.StatusBadGateway,
			Reason:        fmt.Sprintf("consumer failed transiently (%d); Meta will redeliver", status),
		}

	case status >= 400:
		// They understood and refused. Redelivering repeats the same
		// failure for 36h.
		return Verdict{
			StatusForMeta: http.StatusOK,
			Alarm:         true,
			Reason:        fmt.Sprintf("consumer REFUSED (%d); event lost for good", status),
		}

	default:
		// 1xx/3xx on a callback is a misconfiguration, not something transitory.
		return Verdict{
			StatusForMeta: http.StatusOK,
			Alarm:         true,
			Reason:        fmt.Sprintf("consumer returned an unexpected status (%d)", status),
		}
	}
}

// CounterKeys returns the counter keys (config.Cont*, T-035) that this
// delivery outcome must increment.
//
// ONE FUNCTION ONLY, fed by the SAME `status`/`err` that decide the verdict,
// plus the already-computed verdict — never a second classification that
// could drift from ConsumerVerdict's. It's this project's mother-trap:
// the same rule holding in one place and not the next. The test
// TestCounterKeysMatchConsumerVerdictBoundaries sweeps the whole
// 100..599 range and proves the two agree at the boundaries.
//
// WHY A TRANSIENT FAILURE (consumer 5xx) COUNTS NOTHING AT ALL: the closed
// counter vocabulary (T-035) has no key for this, and forcing this event
// inside "recusadas_pelo_consumidor" would confuse whoever operates it — Meta
// is still going to redeliver on its own, no one needs to act now.
func CounterKeys(status int, err error, v Verdict) []string {
	var keys []string
	if err == nil {
		switch {
		case status >= 200 && status < 300:
			keys = append(keys, config.CounterDelivered)
		case status >= 500 && status < 600:
			// Transient: no key (see comment above).
		case status >= 400:
			keys = append(keys, config.CounterRefusedByConsumer)
		}
	}
	// v.Alarm && StatusForMeta 200 is, by mirror.go's own definition, the
	// PERMANENT LOSS (Meta never redelivers again) — the only axis that
	// decides this key, and the SAME one the handler already uses to log
	// with the ALARME prefix.
	if v.Alarm && v.StatusForMeta == http.StatusOK {
		keys = append(keys, config.CounterDefinitiveLossAlarm)
	}
	return keys
}

// isCertificateFailure reports whether the delivery died in certificate
// verification.
//
// The question is put to the ERROR, with errors.As, and never to its text:
// a library's error message changes between Go versions and between
// operating systems (on Windows verification goes through the platform's own
// verifier), and a substring guard would silently turn into a false negative
// — the exact shape of failure this project chases down.
//
// tls.CertificateVerificationError is the wrapper crypto/tls puts around the
// x509 error; the three x509 types are listed because a path that verifies
// the certificate outside the handshake doesn't go through that wrapper.
func isCertificateFailure(err error) bool {
	var verification *tls.CertificateVerificationError
	if errors.As(err, &verification) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError // includes EXPIRED
	var wrongHostname x509.HostnameError
	return errors.As(err, &unknownAuthority) ||
		errors.As(err, &invalidCertificate) ||
		errors.As(err, &wrongHostname)
}

// errorReason describes the transport failure WITHOUT interpolating the raw
// error.
//
// WHY NOT `%v` ON THE ERROR: a transport error in Go is typically a
// *url.Error, and its Error() carries the request's FULL URL — host, path
// and query string. The CallbackURL is encrypted at rest precisely so that a
// stolen backup doesn't reveal the consumers' topology; printing it to the
// log would make the encryption decorative. What comes out here is the
// CATEGORY of the failure; WHO failed the caller identifies by the instance
// slug, which is not a secret.
func errorReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "consumer did not respond in time"
	case errors.Is(err, context.Canceled):
		return "delivery canceled before the consumer's response"
	default:
		return "consumer unreachable"
	}
}
