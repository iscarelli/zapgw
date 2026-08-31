// Tests for the `lideranca` block of GET /v1/estado (T-135).
//
// The defect these tests exist to prevent is the indicator LYING — and the
// lie that costs the most has a direction: saying "armed" when it isn't, or
// saying "holder" on a node that isn't. Both make whoever operates it
// believe they're protected, which is worse than having no indicator at
// all.
package outbound

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLeadershipInStateDisarmedInventsNoHolder(t *testing.T) {
	var l *Leadership // nil: the caller that didn't build anything
	for name, guard := range map[string]*Leadership{"nil": l, "vazia": {}} {
		t.Run(name, func(t *testing.T) {
			b := guard.inState()
			if b.Armed {
				t.Error("armada = true sem arquivo configurado")
			}
			if b.State != NotApplicable {
				t.Errorf("estado = %q, queria %q", b.State, NotApplicable)
			}
			// 🔴 The point: `titular` has to be NULL, never true. A `true`
			// would make a single node look like it won an election that
			// never happened — and a dashboard summing up "holders" would
			// count an installation with no peer.
			if b.Holder != nil {
				t.Errorf("titular = %v, queria null: guarda desarmada nao venceu eleicao nenhuma", *b.Holder)
			}
			if b.Reason != nil {
				t.Errorf("motivo = %q, queria null quando nao ha recusa", *b.Reason)
			}
		})
	}
}

func TestLeadershipInStateArmedWithFreshLeaseSaysHolder(t *testing.T) {
	l := &Leadership{file: leaseFile(t, 1*time.Second), validity: 15 * time.Second}
	b := l.inState()

	if !b.Armed {
		t.Error("armada = false com arquivo configurado")
	}
	if b.State != CertObserved {
		t.Errorf("estado = %q, queria %q — houve medicao de verdade", b.State, CertObserved)
	}
	if b.Holder == nil || !*b.Holder {
		t.Fatalf("titular = %v, queria true", b.Holder)
	}
	if b.Reason != nil {
		t.Errorf("motivo = %q, mas nao houve recusa para explicar", *b.Reason)
	}
}

func TestLeadershipInStateArmedWithStaleLeaseSaysWhyNot(t *testing.T) {
	l := &Leadership{file: leaseFile(t, 90*time.Second), validity: 15 * time.Second}
	b := l.inState()

	if !b.Armed {
		t.Error("armada = false com arquivo configurado")
	}
	if b.Holder == nil || *b.Holder {
		t.Fatalf("titular = %v, queria false: a concessao esta velha", b.Holder)
	}
	if b.Reason == nil || *b.Reason == "" {
		t.Fatal("motivo vazio — quem opera precisa saber POR QUE este no nao esta enviando, senao reinicia o servico procurando defeito que nao existe")
	}
}

// A refusal from FAILING TO VERIFY has to be distinguishable from a refusal
// from a stale grant. Both refuse the send (and that's why `titular` is
// false in both), but only the first means the machine is BLIND.
func TestLeadershipInStateDistinguishesBlindFromNotHolder(t *testing.T) {
	old := (&Leadership{file: leaseFile(t, 90*time.Second), validity: 15 * time.Second}).inState()
	blind := (&Leadership{file: filepath.Join(t.TempDir(), "nao-existe"), validity: 15 * time.Second}).inState()

	if old.Reason == nil || blind.Reason == nil {
		t.Fatal("as duas recusas tem de trazer motivo")
	}
	if *old.Reason == *blind.Reason {
		t.Errorf("os dois motivos sao identicos (%q) — 'nao consegui verificar' e 'nao sou o titular' viram a mesma coisa, e a primeira e a que diz que a maquina esta cega", *old.Reason)
	}
}

// The contract is JSON: the test has to look at the NAMES and the NULLS the
// consumer receives, not just the Go fields.
func TestLeadershipInStateSerializesWithTheContractNames(t *testing.T) {
	raw, err := json.Marshal((&Leadership{}).inState())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"armada", "estado", "titular", "motivo"} {
		if _, has := m[field]; !has {
			t.Errorf("o campo %q sumiu do JSON — campo AUSENTE obriga o consumidor a adivinhar; nulo ele consegue ler", field)
		}
	}
	if m["titular"] != nil {
		t.Errorf("titular = %v no JSON, queria null com a guarda desarmada", m["titular"])
	}
	if m["armada"] != false {
		t.Errorf("armada = %v, queria false", m["armada"])
	}
}

// Querying the state must NOT interfere with the guard, and vice versa:
// /v1/estado is a dashboard route and can be called in a loop.
func TestLeadershipInStateDoesNotInterfereWithTheGuard(t *testing.T) {
	path := leaseFile(t, 1*time.Second)
	l := &Leadership{file: path, validity: 15 * time.Second}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat antes: %v", err)
	}
	for i := 0; i < 50; i++ {
		_ = l.inState()
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat depois: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("consultar o estado MEXEU no arquivo de concessao — o painel estaria renovando a lideranca, que e' exatamente o titular falso que a guarda existe para barrar")
	}
	if ok, reason := l.Holder(); !ok {
		t.Errorf("depois de 50 consultas ao estado a guarda passou a recusar: %s", reason)
	}
}
