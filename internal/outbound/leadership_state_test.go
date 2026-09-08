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
	for name, guard := range map[string]*Leadership{"nil": l, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			b := guard.inState()
			if b.Armed {
				t.Error("armada = true without a configured file")
			}
			if b.State != NotApplicable {
				t.Errorf("state = %q, wanted %q", b.State, NotApplicable)
			}
			// 🔴 The point: `titular` has to be NULL, never true. A `true`
			// would make a single node look like it won an election that
			// never happened — and a dashboard summing up "holders" would
			// count an installation with no peer.
			if b.Holder != nil {
				t.Errorf("titular = %v, wanted null: a disarmed guard did not win any election", *b.Holder)
			}
			if b.Reason != nil {
				t.Errorf("reason = %q, wanted null when there is no refusal", *b.Reason)
			}
		})
	}
}

func TestLeadershipInStateArmedWithFreshLeaseSaysHolder(t *testing.T) {
	l := &Leadership{file: leaseFile(t, 1*time.Second), validity: 15 * time.Second}
	b := l.inState()

	if !b.Armed {
		t.Error("armada = false with a configured file")
	}
	if b.State != CertObserved {
		t.Errorf("state = %q, wanted %q — there was a real measurement", b.State, CertObserved)
	}
	if b.Holder == nil || !*b.Holder {
		t.Fatalf("titular = %v, wanted true", b.Holder)
	}
	if b.Reason != nil {
		t.Errorf("reason = %q, but there was no refusal to explain", *b.Reason)
	}
}

func TestLeadershipInStateArmedWithStaleLeaseSaysWhyNot(t *testing.T) {
	l := &Leadership{file: leaseFile(t, 90*time.Second), validity: 15 * time.Second}
	b := l.inState()

	if !b.Armed {
		t.Error("armada = false with a configured file")
	}
	if b.Holder == nil || *b.Holder {
		t.Fatalf("titular = %v, wanted false: the lease is stale", b.Holder)
	}
	if b.Reason == nil || *b.Reason == "" {
		t.Fatal("empty reason — whoever operates it needs to know WHY this node is not sending, otherwise they restart the service looking for a defect that does not exist")
	}
}

// A refusal from FAILING TO VERIFY has to be distinguishable from a refusal
// from a stale grant. Both refuse the send (and that's why `titular` is
// false in both), but only the first means the machine is BLIND.
func TestLeadershipInStateDistinguishesBlindFromNotHolder(t *testing.T) {
	old := (&Leadership{file: leaseFile(t, 90*time.Second), validity: 15 * time.Second}).inState()
	blind := (&Leadership{file: filepath.Join(t.TempDir(), "does-not-exist"), validity: 15 * time.Second}).inState()

	if old.Reason == nil || blind.Reason == nil {
		t.Fatal("both refusals have to carry a reason")
	}
	if *old.Reason == *blind.Reason {
		t.Errorf("the two reasons are identical (%q) — 'nao consegui verificar' and 'nao sou o titular' become the same thing, and the first one is the one that says the machine is blind", *old.Reason)
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
	for _, field := range []string{"armada", "state", "titular", "reason"} {
		if _, has := m[field]; !has {
			t.Errorf("field %q vanished from the JSON — an ABSENT field forces the consumer to guess; a null one it can read", field)
		}
	}
	if m["titular"] != nil {
		t.Errorf("titular = %v in the JSON, wanted null with a disarmed guard", m["titular"])
	}
	if m["armada"] != false {
		t.Errorf("armada = %v, wanted false", m["armada"])
	}
}

// Querying the state must NOT interfere with the guard, and vice versa:
// /v1/estado is a dashboard route and can be called in a loop.
func TestLeadershipInStateDoesNotInterfereWithTheGuard(t *testing.T) {
	path := leaseFile(t, 1*time.Second)
	l := &Leadership{file: path, validity: 15 * time.Second}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	for i := 0; i < 50; i++ {
		_ = l.inState()
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("querying the state TOUCHED the lease file — the dashboard would be renewing leadership, which is exactly the false holder the guard exists to block")
	}
	if ok, reason := l.Holder(); !ok {
		t.Errorf("after 50 state queries the guard started refusing: %s", reason)
	}
}
