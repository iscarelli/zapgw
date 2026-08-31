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
		t.Fatalf("criar %s: %v", name, err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`CREATE TABLE idempotencia (
		consumidor    TEXT NOT NULL,
		chave         TEXT NOT NULL,
		wa_message_id TEXT NOT NULL DEFAULT '',
		criado_em     INTEGER NOT NULL,
		PRIMARY KEY (consumidor, chave)
	)`); err != nil {
		t.Fatalf("criar tabela em %s: %v", name, err)
	}
	for _, l := range rows {
		if _, err := db.Exec(`INSERT INTO idempotencia (consumidor, chave, wa_message_id, criado_em) VALUES (?,?,?,?)`,
			l.Consumer, l.Key, l.Wamid, l.CreatedAt); err != nil {
			t.Fatalf("inserir em %s: %v", name, err)
		}
	}
	return path
}

func fileSum(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler %s: %v", path, err)
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
		t.Fatalf("Confirmed = %+v; queria so k-saiu — e' a que CHEGOU a Meta e o restaurado esqueceu", c.Confirmed)
	}
	if len(c.Open) != 1 || c.Open[0].Key != "k-duvida" {
		t.Fatalf("Open = %+v; queria so k-duvida", c.Open)
	}
	if c.ReadInOld != 3 || c.ReadInCurrent != 2 {
		t.Errorf("lidas = (%d, %d); queria (3, 2) — o tamanho do que foi comparado tem de sair no relatorio",
			c.ReadInOld, c.ReadInCurrent)
	}
	if !c.Lost() {
		t.Error("Perdeu() = false com uma confirmada e uma em aberto")
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
		t.Fatalf("o banco de teste tinha de nascer em user_version=0 (como um banco antigo); veio %d", beforeVersion)
	}

	if _, err := CompareFailover(old, current); err != nil {
		t.Fatalf("CompareFailover: %v", err)
	}

	if after := fileSum(t, old); after != beforeFile {
		t.Error("o BANCO ANTIGO MUDOU depois da pericia — a ferramenta destruiu a evidencia que veio periciar")
	}
	if after := schemaVersion(t, old); after != beforeVersion {
		t.Errorf("user_version foi de %d para %d: rodou MIGRACAO no banco antigo", beforeVersion, after)
	}
}

func schemaVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		t.Fatalf("abrir %s: %v", path, err)
	}
	defer func() { _ = db.Close() }()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("ler user_version de %s: %v", path, err)
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
		t.Fatalf("achou perda onde os dois bancos sao iguais: %+v", c)
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
		t.Error("antigo vazio nao pode produzir perda")
	}
	if c.ReadInOld != 0 {
		t.Errorf("ReadInOld = %d, queria 0 — e' esse numero que deixa quem le desconfiar do arquivo errado", c.ReadInOld)
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
			t.Fatalf("saiu fora de ordem em %d: %d depois de %d",
				i, first.Confirmed[i].CreatedAt, first.Confirmed[i-1].CreatedAt)
		}
	}
	second, err := CompareFailover(old, current)
	if err != nil {
		t.Fatalf("CompareFailover (2a): %v", err)
	}
	for i := range first.Confirmed {
		if first.Confirmed[i] != second.Confirmed[i] {
			t.Fatalf("duas execucoes deram ordens diferentes na posicao %d", i)
		}
	}
}
