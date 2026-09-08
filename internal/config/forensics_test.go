// Tests for the failover forensics (forensics.go).
//
// The case this file exists to not lose is
// TestCompareFailoverDoesNotTouchTheOldDatabase: the forensics tool running
// OpenStore would migrate the only surviving copy of the evidence,
// silently. The other tests protect the separation between "I know it went
// out" and "I don't know", which is what keeps the report from treating
// doubt as certainty.
package config

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// idempotencyDatabase creates a RAW SQLite — just the `idempotencia` table
// and `user_version = 0`, which is what an old database looks like. It
// deliberately doesn't go through OpenStore: if it did, it would already
// be born migrated and the non-modification test would lose its point.
func idempotencyDatabase(t *testing.T, name string, rows ...SendAtRisk) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`CREATE TABLE idempotencia (
		consumidor    TEXT NOT NULL,
		chave         TEXT NOT NULL,
		wa_message_id TEXT NOT NULL DEFAULT '',
		criado_em     INTEGER NOT NULL,
		PRIMARY KEY (consumidor, chave)
	)`); err != nil {
		t.Fatalf("create table in %s: %v", name, err)
	}
	for _, l := range rows {
		if _, err := db.Exec(`INSERT INTO idempotencia (consumidor, chave, wa_message_id, criado_em) VALUES (?,?,?,?)`,
			l.Consumer, l.Key, l.Wamid, l.CreatedAt); err != nil {
			t.Fatalf("insert into %s: %v", name, err)
		}
	}
	return path
}

func fileSum(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestCompareFailoverSeparatesConfirmedFromOpen(t *testing.T) {
	old := idempotencyDatabase(t, "antigo.db",
		SendAtRisk{Consumer: "lojinha", Key: "k-saiu", Wamid: "wamid.ABC", CreatedAt: 100},
		SendAtRisk{Consumer: "lojinha", Key: "k-duvida", Wamid: "", CreatedAt: 200},
		SendAtRisk{Consumer: "lojinha", Key: "k-replicou", Wamid: "wamid.XYZ", CreatedAt: 50},
	)
	current := idempotencyDatabase(t, "atual.db",
		SendAtRisk{Consumer: "lojinha", Key: "k-replicou", Wamid: "wamid.XYZ", CreatedAt: 50},
		// Work the SUCCESSOR did after taking over: exists only here, and
		// isn't a loss at all.
		SendAtRisk{Consumer: "lojinha", Key: "k-depois", Wamid: "wamid.NOVO", CreatedAt: 999},
	)

	c, err := CompareFailover(old, current)
	if err != nil {
		t.Fatalf("CompareFailover: %v", err)
	}

	if len(c.Confirmed) != 1 || c.Confirmed[0].Key != "k-saiu" {
		t.Fatalf("Confirmed = %+v; wanted only k-saiu — it's the one that REACHED Meta and the restored one forgot", c.Confirmed)
	}
	if len(c.Open) != 1 || c.Open[0].Key != "k-duvida" {
		t.Fatalf("Open = %+v; wanted only k-duvida", c.Open)
	}
	if c.ReadInOld != 3 || c.ReadInCurrent != 2 {
		t.Errorf("read = (%d, %d); wanted (3, 2) — the size of what was compared has to show up in the report",
			c.ReadInOld, c.ReadInCurrent)
	}
	if !c.Lost() {
		t.Error("Lost() = false with one confirmed and one open")
	}
}

// 🔴 The test that guards the decision to NOT use OpenStore.
//
// OpenStore runs migrations. Pointed at the old database, it would ALTER
// it — the forensics tool silently destroying the only surviving copy of
// the evidence it came to examine. This test goes red if someone
// "simplifies" openReadOnly into OpenStore.
func TestCompareFailoverDoesNotTouchTheOldDatabase(t *testing.T) {
	old := idempotencyDatabase(t, "antigo.db",
		SendAtRisk{Consumer: "lojinha", Key: "k1", Wamid: "wamid.A", CreatedAt: 10},
	)
	current := idempotencyDatabase(t, "atual.db")

	beforeFile := fileSum(t, old)
	beforeVersion := schemaVersion(t, old)
	if beforeVersion != 0 {
		t.Fatalf("the test database had to be born at user_version=0 (like an old database); came out %d", beforeVersion)
	}

	if _, err := CompareFailover(old, current); err != nil {
		t.Fatalf("CompareFailover: %v", err)
	}

	if after := fileSum(t, old); after != beforeFile {
		t.Error("the OLD DATABASE CHANGED after the forensics run — the tool destroyed the evidence it came to examine")
	}
	if after := schemaVersion(t, old); after != beforeVersion {
		t.Errorf("user_version went from %d to %d: it ran a MIGRATION on the old database", beforeVersion, after)
	}
}

func schemaVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = db.Close() }()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("read user_version from %s: %v", path, err)
	}
	return v
}

func TestCompareFailoverWithNoLossInventsNothing(t *testing.T) {
	equal := []SendAtRisk{
		{Consumer: "lojinha", Key: "k1", Wamid: "wamid.A", CreatedAt: 10},
		{Consumer: "lojinha", Key: "k2", Wamid: "", CreatedAt: 20},
	}
	old := idempotencyDatabase(t, "antigo.db", equal...)
	current := idempotencyDatabase(t, "atual.db", equal...)

	c, err := CompareFailover(old, current)
	if err != nil {
		t.Fatalf("CompareFailover: %v", err)
	}
	if c.Lost() {
		t.Fatalf("found a loss where the two databases are equal: %+v", c)
	}
}

// An empty old database is NOT "nothing was lost" — it's a comparison that
// didn't compare anything. Whoever reads it can tell the difference because
// ReadInOld is in the report.
func TestCompareFailoverExposesThatTheOldOneWasEmpty(t *testing.T) {
	old := idempotencyDatabase(t, "antigo.db")
	current := idempotencyDatabase(t, "atual.db",
		SendAtRisk{Consumer: "lojinha", Key: "k1", Wamid: "wamid.A", CreatedAt: 10},
	)
	c, err := CompareFailover(old, current)
	if err != nil {
		t.Fatalf("CompareFailover: %v", err)
	}
	if c.Lost() {
		t.Error("an empty old database cannot produce a loss")
	}
	if c.ReadInOld != 0 {
		t.Errorf("ReadInOld = %d, wanted 0 — it's this number that lets the reader suspect the wrong file", c.ReadInOld)
	}
}

// A Go map iterates in random order: without the sort, two runs would give
// different reports for the SAME pair of databases, and whoever is
// reconstructing an incident's timeline would have no way to trust what
// they read.
func TestCompareFailoverHasAStableOrderAndByTime(t *testing.T) {
	var rows []SendAtRisk
	for i := 0; i < 25; i++ {
		rows = append(rows, SendAtRisk{
			Consumer:  "lojinha",
			Key:       fmt.Sprintf("k%02d", i),
			Wamid:     fmt.Sprintf("wamid.%02d", i),
			CreatedAt: int64(1000 - i), // out of order on purpose
		})
	}
	old := idempotencyDatabase(t, "antigo.db", rows...)
	current := idempotencyDatabase(t, "atual.db")

	first, err := CompareFailover(old, current)
	if err != nil {
		t.Fatalf("CompareFailover: %v", err)
	}
	for i := 1; i < len(first.Confirmed); i++ {
		if first.Confirmed[i-1].CreatedAt > first.Confirmed[i].CreatedAt {
			t.Fatalf("came out of order at %d: %d after %d",
				i, first.Confirmed[i].CreatedAt, first.Confirmed[i-1].CreatedAt)
		}
	}
	second, err := CompareFailover(old, current)
	if err != nil {
		t.Fatalf("CompareFailover (2nd): %v", err)
	}
	for i := range first.Confirmed {
		if first.Confirmed[i] != second.Confirmed[i] {
			t.Fatalf("two runs gave different orders at position %d", i)
		}
	}
}
