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
		t.Fatalf("criar %s: %v", name, err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE idempotencia (
		consumidor TEXT NOT NULL, chave TEXT NOT NULL,
		wa_message_id TEXT NOT NULL DEFAULT '', criado_em INTEGER NOT NULL,
		PRIMARY KEY (consumidor, chave))`); err != nil {
		t.Fatalf("criar tabela: %v", err)
	}
	for _, l := range lines {
		if _, err := db.Exec(`INSERT INTO idempotencia VALUES (?,?,?,?)`, l[0], l[1], l[2], l[3]); err != nil {
			t.Fatalf("inserir: %v", err)
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
		t.Fatal("sem --antigo tinha de dar erro")
	}
	if !strings.Contains(err.Error(), "supervisor guarda") {
		t.Errorf("a mensagem tem de dizer de onde vem o arquivo antigo; veio: %v", err)
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
		t.Fatal("com perda o comando tem de sair com ERRO — quem roda isto num script precisa de status != 0")
	}
	if !strings.Contains(err.Error(), "PRECISA DE GENTE") {
		t.Errorf("o erro tem de usar a MESMA marca dos alarmes do handler; veio: %v", err)
	}

	text := out.String()
	for _, required := range []string{
		"CONFIRMADAS PERDIDAS", "k-saiu", "wamid.ABC",
		"EM ABERTO PERDIDAS", "k-duvida",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("a saida nao traz %q:\n%s", required, text)
		}
	}
	// 🔴 The separation is the point: the key without a wamid must NOT
	// appear as confirmed, otherwise the report treats doubt as certainty.
	confirmed := text[strings.Index(text, "CONFIRMADAS PERDIDAS"):strings.Index(text, "EM ABERTO PERDIDAS")]
	if strings.Contains(confirmed, "k-duvida") {
		t.Error("k-duvida (sem wamid) apareceu entre as CONFIRMADAS — isso e tratar 'nao sei' como 'sei'")
	}
}

func TestLostCommandWithoutLossExitsClean(t *testing.T) {
	equal := [][4]any{{"lojinha", "k1", "wamid.A", 10}}
	old := rawDatabase(t, "antigo.db", equal)
	current := rawDatabase(t, "atual.db", equal)

	var out bytes.Buffer
	if err := lostCommand([]string{"--antigo", old, "--atual", current}, &out, func(string) string { return "" }); err != nil {
		t.Fatalf("sem perda o comando tem de sair 0; veio: %v", err)
	}
	if !strings.Contains(out.String(), "NADA EM RISCO") {
		t.Errorf("faltou o veredito explicito:\n%s", out.String())
	}
}

// An empty old database cannot pass as "nothing lost" without a caveat.
func TestLostCommandWarnsWhenTheOldOneIsEmpty(t *testing.T) {
	old := rawDatabase(t, "antigo.db", nil)
	current := rawDatabase(t, "atual.db", [][4]any{{"lojinha", "k1", "wamid.A", 10}})

	var out bytes.Buffer
	if err := lostCommand([]string{"--antigo", old, "--atual", current}, &out, func(string) string { return "" }); err != nil {
		t.Fatalf("nao devia dar erro: %v", err)
	}
	if !strings.Contains(out.String(), "ATENCAO") {
		t.Errorf("antigo vazio tem de vir com ressalva — pode ser o arquivo errado:\n%s", out.String())
	}
}
