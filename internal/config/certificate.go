// The consumer's certificate validity — what the DELIVERY has already seen,
// kept so someone can act before the delivery breaks (T-064).
//
// WHY OBSERVE, AND NOT PROBE. The gateway already speaks https to the
// consumer's callback on every delivery, and the handshake already brings
// its chain: the `NotAfter` is right there, for free. A periodic probe
// (opening a connection just to look at the certificate) would be a second
// network path, with a second way to fail, to answer what the first already
// answers. That's why this file has NO timer and opens NO connection at
// all: it only keeps what the delivery saw.
//
// THE PRICE OF THIS CHOICE, and it's the reason the `observado_em` stamp
// exists and travels along in the contract: the information ages when
// traffic stops. An instance that has never delivered HAS NO observation —
// and that's a NAMED state, never a made-up date (see CertificateObservation).
//
// THE SAME RULE AS THE COUNTER APPLIES HERE, for the same reason: writing
// is tracking, and tracking can never bring the delivery down. That's why
// CertificateObserver.Record RETURNS NOTHING — the guarantee lives
// in the SIGNATURE, not in the discipline of whoever calls it
// (docs/ARMADILHAS.md, "Um método que PODE devolver erro convida alguém
// a, um dia, tratar esse erro como fatal").
package config

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// CertificateObservation is what the gateway SAW of an instance's callback
// certificate, in the last delivery where a handshake happened.
//
// THE TWO FIELDS GO TOGETHER OR THEY'RE WORTHLESS. `ExpiresAt` alone doesn't
// say whether it's current information or three weeks old — and a
// certificate observed three weeks ago may have already been renewed (or
// already expired). Whoever reads it decides with both in hand.
//
// ZERO = NEVER OBSERVED, in both fields at once: there is no "observed but
// don't know when" state, because there is no path that writes one without
// the other (RecordCallbackCertificate writes both in the same UPSERT).
type CertificateObservation struct {
	// ExpiresAt is the NotAfter of the LEAF certificate the consumer presented.
	ExpiresAt time.Time
	// ObservedAt is when that handshake happened.
	ObservedAt time.Time
}

// Observed tells whether there is an observation. `false` means "we have
// never delivered to this consumer with a complete handshake", which is
// legitimate information — not an error.
func (o CertificateObservation) Observed() bool {
	return !o.ObservedAt.IsZero()
}

// RecordCallbackCertificate writes (or overwrites) an instance's observation.
//
// ALWAYS OVERWRITES, without comparing against what was there: the latest
// observation is the only one that describes NOW. A renewed certificate has
// a later NotAfter, a certificate swapped by mistake may have an earlier
// one — in both cases what the gateway just saw is what counts, and a
// "only accept a later date" rule would hide precisely the swap that
// matters.
//
// DON'T CALL THIS DIRECTLY FROM THE DELIVERY PATH: that's what
// CertificateObserver (below) exists for, wrapping the call with the
// "error only logs" guarantee. This method returns a real error because
// its caller (the serialized writer) needs to know if there is something
// to log.
func (s *Store) RecordCallbackCertificate(slug string, expiresAt, observedAt time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO certificado_do_callback (slug, expira_em, observado_em) VALUES (?, ?, ?)
		ON CONFLICT (slug) DO UPDATE SET
		  expira_em = excluded.expira_em, observado_em = excluded.observado_em`,
		slug, stampOf(expiresAt), stampOf(observedAt))
	if err != nil {
		return fmt.Errorf("config: registrar certificado do callback: %w", err)
	}
	return nil
}

// CallbackCertificate reads an instance's observation. An instance with
// no row gets back the ZEROED CertificateObservation and a nil error —
// "never observed" is an answer, not a failure.
//
// A stamp that fails to decode IS AN ERROR, never a zeroed observation: the
// column is written by ONE function, in ONE format (stampOf), so an
// unreadable value there means someone edited the database by hand.
// Returning "never observed" in that case would turn a tampered database
// into plausible silence — the most expensive failure shape in this
// project.
func (s *Store) CallbackCertificate(slug string) (CertificateObservation, error) {
	var expires, observed string
	err := s.db.QueryRow(
		`SELECT expira_em, observado_em FROM certificado_do_callback WHERE slug = ?`, slug,
	).Scan(&expires, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		return CertificateObservation{}, nil
	}
	if err != nil {
		return CertificateObservation{}, fmt.Errorf("config: ler certificado do callback: %w", err)
	}

	expiresAt, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return CertificateObservation{}, fmt.Errorf(
			"config: expira_em do certificado (slug=%q) nao e RFC3339: %w", slug, err)
	}
	observedAt, err := time.Parse(time.RFC3339, observed)
	if err != nil {
		return CertificateObservation{}, fmt.Errorf(
			"config: observado_em do certificado (slug=%q) nao e RFC3339: %w", slug, err)
	}
	return CertificateObservation{ExpiresAt: expiresAt.UTC(), ObservedAt: observedAt.UTC()}, nil
}

// ObserverStore is what CertificateObserver needs from the Store.
// EXPORTED for the same reason as CounterStore: a test in ANOTHER package
// (internal/inbound) uses an implementation that ALWAYS fails to prove, on
// a real delivery, that a write failure never reaches the delivery's outcome.
type ObserverStore interface {
	RecordCallbackCertificate(slug string, expiresAt, observedAt time.Time) error
}

// CertificateObserver is the SINGLE WRITER of certificate observations.
//
// Same design as Counter, for the same two reasons: the mutex serializes
// the write (the gateway delivers across several goroutines over the SAME
// deliverer — the project already paid a Critical for shared mutable state,
// see docs/ARMADILHAS.md, "Go / concorrência"), and Record returns
// nothing, so there is no error that a caller could, one day, treat as
// fatal in the middle of a delivery.
type CertificateObserver struct {
	mu    sync.Mutex
	store ObserverStore
}

// NewCertificateObserver is the PRODUCTION constructor.
func NewCertificateObserver(store *Store) *CertificateObserver {
	return &CertificateObserver{store: store}
}

// NewCertificateObserverWithStore accepts any ObserverStore. It
// exists for testing — including so a test in another package can prove
// that a write that ALWAYS fails doesn't change the delivery's outcome.
func NewCertificateObserverWithStore(store ObserverStore) *CertificateObserver {
	return &CertificateObserver{store: store}
}

// Record keeps what the delivery just saw.
//
// NIL RECEIVER IS A DELIBERATE NO-OP: delivery tests that have nothing to
// do with certificates pass nil, and none of them can panic because of a
// tracking subsystem. In production, cmd/zapgw/main.go is what assembles
// it, and a missing observer there doesn't produce a WRONG result — it
// produces `never observed` on the status route, which is a NAMED and
// visible state.
func (o *CertificateObserver) Record(slug string, expiresAt, observedAt time.Time) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.store.RecordCallbackCertificate(slug, expiresAt, observedAt); err != nil {
		log.Printf("zapgw: falha ao gravar a validade do certificado do callback (slug=%q): %v", slug, err)
	}
}
