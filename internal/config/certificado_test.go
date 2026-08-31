package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ABSENCE IS "NEVER OBSERVED", NOT AN ERROR. A freshly provisioned instance
// has never delivered, and the status route needs to return 200 saying so —
// an error here would turn into a 503 on a perfectly healthy instance.
func TestCallbackCertificateWithNoObservationIsNotAnError(t *testing.T) {
	s := testStore(t)

	o, err := s.CallbackCertificate("lojinha")
	if err != nil {
		t.Fatalf("CallbackCertificate numa instancia sem observacao: %v", err)
	}
	if o.Observed() {
		t.Errorf("Observed() = true sem nenhuma entrega ter acontecido (%+v)", o)
	}
	if !o.ExpiresAt.IsZero() || !o.ObservedAt.IsZero() {
		t.Errorf("observacao inventada do nada: %+v", o)
	}
}

// BOTH STAMPS COME BACK, and they come back in UTC. `expira_em` without
// `observado_em` is useless: there is no way to know if it's current
// information or three weeks old.
func TestCallbackCertificateKeepsBothStamps(t *testing.T) {
	s := testStore(t)

	expires := time.Now().Add(45 * 24 * time.Hour).UTC().Truncate(time.Second)
	observed := time.Now().UTC().Truncate(time.Second)
	if err := s.RecordCallbackCertificate("lojinha", expires, observed); err != nil {
		t.Fatalf("RecordCallbackCertificate: %v", err)
	}

	o, err := s.CallbackCertificate("lojinha")
	if err != nil {
		t.Fatalf("CallbackCertificate: %v", err)
	}
	if !o.Observed() {
		t.Fatal("Observed() = false depois de uma observacao gravada")
	}
	if !o.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, quero %v", o.ExpiresAt, expires)
	}
	if !o.ObservedAt.Equal(observed) {
		t.Errorf("ObservedAt = %v, quero %v", o.ObservedAt, observed)
	}

	// And the NEIGHBORING instance still has no observation: an observation
	// that leaked between instances would answer "yes" for any slug, and
	// consumer B's alarm would read consumer A's certificate.
	other, err := s.CallbackCertificate("clinica")
	if err != nil {
		t.Fatalf("CallbackCertificate(clinica): %v", err)
	}
	if other.Observed() {
		t.Errorf("a observacao de lojinha apareceu em clinica: %+v", other)
	}
}

// THE LATEST OBSERVATION IS THE ONE THAT COUNTS, including when the new date
// is EARLIER. A certificate swapped by mistake (or rolled back) has an
// earlier NotAfter, and a "only accept a later date" rule would hide
// precisely the swap that matters.
func TestCallbackCertificateOverwritesWithTheMostRecentObservation(t *testing.T) {
	s := testStore(t)

	oldStamp := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := s.RecordCallbackCertificate("lojinha", oldStamp, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("primeira observacao: %v", err)
	}

	smaller := time.Now().Add(2 * 24 * time.Hour).UTC().Truncate(time.Second)
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.RecordCallbackCertificate("lojinha", smaller, now); err != nil {
		t.Fatalf("segunda observacao: %v", err)
	}

	o, err := s.CallbackCertificate("lojinha")
	if err != nil {
		t.Fatalf("CallbackCertificate: %v", err)
	}
	if !o.ExpiresAt.Equal(smaller) {
		t.Errorf("ExpiresAt = %v, quero %v — a observacao mais recente e a que descreve o agora", o.ExpiresAt, smaller)
	}
	if !o.ObservedAt.Equal(now) {
		t.Errorf("ObservedAt = %v, quero %v", o.ObservedAt, now)
	}
}

// An unreadable stamp IS AN ERROR, never "never observed". The column is
// written by ONE function, in ONE format: a strange value there means the
// database was edited by hand, and returning "never observed" would turn
// that into plausible silence — precisely the state the consumer uses to
// NOT alarm.
func TestCallbackCertificateWithAnUnreadableStampIsAnError(t *testing.T) {
	s := testStore(t)

	if _, err := s.DB().Exec(
		`INSERT INTO certificado_do_callback (slug, expira_em, observado_em) VALUES (?, ?, ?)`,
		"lojinha", "ontem de manha", "2026-07-28T12:00:00Z"); err != nil {
		t.Fatalf("gravar carimbo ilegivel: %v", err)
	}

	o, err := s.CallbackCertificate("lojinha")
	if err == nil {
		t.Fatalf("carimbo ilegivel passou como observacao valida: %+v", o)
	}
	if !strings.Contains(err.Error(), "expira_em") {
		t.Errorf("erro = %v — tem de dizer QUAL carimbo nao decodifica", err)
	}
}

// alwaysFailingStore is an ObserverStore that never writes. It exists to
// prove that a write failure doesn't turn into a panic or an error for the
// caller — the same guarantee the Counter gives, and for the same reason:
// tracking can never bring delivery down.
type alwaysFailingStore struct{ calls int }

func (s *alwaysFailingStore) RecordCallbackCertificate(string, time.Time, time.Time) error {
	s.calls++
	return errors.New("banco fora do ar")
}

func TestTheObserverDoesNotPropagateAWriteFailure(t *testing.T) {
	fake := &alwaysFailingStore{}
	o := NewCertificateObserverWithStore(fake)

	// If Record returned an error, this wouldn't even compile — and that's the defense.
	o.Record("lojinha", time.Now(), time.Now())

	if fake.calls != 1 {
		t.Fatalf("chamadas = %d, quero 1", fake.calls)
	}
}

// A nil Observador is a no-op: no delivery test, and no path that doesn't
// care about certificates, can blow up because of a tracking subsystem.
func TestANilObserverDoesNotPanic(t *testing.T) {
	var o *CertificateObserver
	o.Record("lojinha", time.Now(), time.Now())
}
