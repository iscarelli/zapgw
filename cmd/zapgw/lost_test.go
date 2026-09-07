package main

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func rawDatabase(t *testing.T, name string, lines [][4]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE idempotencia (
		consumidor TEXT NOT NULL, chave TEXT NOT NULL,
		wa_message_id TEXT NOT NULL DEFAULT '', criado_em INTEGER NOT NULL,
		PRIMARY KEY (consumidor, chave))`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, l := range lines {
		if _, err := db.Exec(`INSERT INTO idempotencia VALUES (?,?,?,?)`, l[0], l[1], l[2], l[3]); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	return path
}

// Without --antigo there is no possible post-mortem, and the message has
// to TEACH where that file comes from — otherwise whoever is in the middle
// of an incident finds out too late that no one kept the copy.
func TestLostCommandRequiresTheOldOneAndExplainsWhereItComesFrom(t *testing.T) {
	var out bytes.Buffer
	err := lostCommand(nil, &out, func(string) string { return "" })
	if err == nil {
		t.Fatal("without --antigo it had to error out")
	}
	if !strings.Contains(err.Error(), "supervisor keeps") {
		t.Errorf("the message has to say where the old file comes from; got: %v", err)
	}
}

func TestLostCommandSeparatesTheTwoListsAndFailsWhenThereIsLoss(t *testing.T) {
	old := rawDatabase(t, "antigo.db", [][4]any{
		{"lojinha", "k-saiu", "wamid.ABC", 100},
		{"lojinha", "k-duvida", "", 200},
	})
	current := rawDatabase(t, "atual.db", nil)

	var out bytes.Buffer
	err := lostCommand([]string{"--antigo", old, "--atual", current}, &out, func(string) string { return "" })
	if err == nil {
		t.Fatal("with a loss the command has to exit with an ERROR — whoever runs this in a script needs status != 0")
	}
	if !strings.Contains(err.Error(), "PRECISA DE GENTE") {
		t.Errorf("the error has to use the SAME marker as the handler's alarms; got: %v", err)
	}

	text := out.String()
	for _, required := range []string{
		"CONFIRMED LOST", "k-saiu", "wamid.ABC",
		"OPEN LOST", "k-duvida",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("the output is missing %q:\n%s", required, text)
		}
	}
	// 🔴 The separation is the point: the key without a wamid must NOT
	// appear as confirmed, otherwise the report treats doubt as certainty.
	confirmed := text[strings.Index(text, "CONFIRMED LOST"):strings.Index(text, "OPEN LOST")]
	if strings.Contains(confirmed, "k-duvida") {
		t.Error("k-duvida (without a wamid) appeared among the CONFIRMED ones — that is treating 'don't know' as 'know'")
	}
}

func TestLostCommandWithoutLossExitsClean(t *testing.T) {
	equal := [][4]any{{"lojinha", "k1", "wamid.A", 10}}
	old := rawDatabase(t, "antigo.db", equal)
	current := rawDatabase(t, "atual.db", equal)

	var out bytes.Buffer
	if err := lostCommand([]string{"--antigo", old, "--atual", current}, &out, func(string) string { return "" }); err != nil {
		t.Fatalf("without a loss the command has to exit 0; got: %v", err)
	}
	if !strings.Contains(out.String(), "NOTHING AT RISK") {
		t.Errorf("the explicit verdict is missing:\n%s", out.String())
	}
}

// An empty old database cannot pass as "nothing lost" without a caveat.
func TestLostCommandWarnsWhenTheOldOneIsEmpty(t *testing.T) {
	old := rawDatabase(t, "antigo.db", nil)
	current := rawDatabase(t, "atual.db", [][4]any{{"lojinha", "k1", "wamid.A", 10}})

	var out bytes.Buffer
	if err := lostCommand([]string{"--antigo", old, "--atual", current}, &out, func(string) string { return "" }); err != nil {
		t.Fatalf("should not have errored: %v", err)
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Errorf("an empty old database has to come with a caveat — it may be the wrong file:\n%s", out.String())
	}
}
