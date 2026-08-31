package config

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	vault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	s, err := OpenStore(filepath.Join(t.TempDir(), "teste.db"), vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testInstance() Instance {
	return Instance{
		Slug:           "lojinha",
		WabaID:         "WABA1",
		PhoneNumberID:  "PNID1",
		DisplayNumber:  "5532999990000",
		AppSecret:      "app-secret-de-teste",
		VerifyToken:    "verify-token-de-teste",
		SendToken:      "token-envio-de-teste",
		CallbackURL:    "https://consumidor.interno/webhooks/zapgw",
		DeliverySecret: "segredo-entrega-de-teste",
		TimeoutMs:      5000,
		Active:         false,
	}
}

func TestStoreKeepsAndReturnsAnInstance(t *testing.T) {
	s := testStore(t)

	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	got, err := s.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if got.AppSecret != "app-secret-de-teste" {
		t.Errorf("AppSecret = %q — a ida e volta pela cifra quebrou", got.AppSecret)
	}
	if got.VerifyToken != "verify-token-de-teste" {
		t.Errorf("VerifyToken = %q", got.VerifyToken)
	}
	if got.SendToken != "token-envio-de-teste" {
		t.Errorf("SendToken = %q", got.SendToken)
	}
	if got.CallbackURL != "https://consumidor.interno/webhooks/zapgw" {
		t.Errorf("CallbackURL = %q", got.CallbackURL)
	}
	if got.DeliverySecret != "segredo-entrega-de-teste" {
		t.Errorf("DeliverySecret = %q", got.DeliverySecret)
	}
	if got.PhoneNumberID != "PNID1" {
		t.Errorf("PhoneNumberID = %q", got.PhoneNumberID)
	}
}

func TestStoreKeepsTheCredentialENCRYPTEDInTheFile(t *testing.T) {
	// The SQLite file goes into the nightly backup. If the credential were
	// in the clear, the backup would start carrying N businesses' tokens readable.
	vault, _ := NewVault(testKey)
	path := filepath.Join(t.TempDir(), "teste.db")
	s, err := OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	_ = s.Close()

	raw := readFile(t, path)
	for _, secret := range []string{
		"app-secret-de-teste", "verify-token-de-teste",
		"token-envio-de-teste", "segredo-entrega-de-teste",
		"https://consumidor.interno/webhooks/zapgw",
	} {
		if containsBytes(raw, secret) {
			t.Errorf("o segredo %q aparece EM CLARO no arquivo do banco", secret)
		}
	}
}

func TestStoreFlagsANonexistentInstance(t *testing.T) {
	s := testStore(t)

	if _, err := s.FindInstance("nao-existe"); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("erro = %v, quero ErrInstanceNotFound", err)
	}
}

func TestStoreFindsByPhoneNumberIDAndByWabaID(t *testing.T) {
	// Two routing keys: phone_number_id for message/status, waba_id for
	// template status and account webhooks (which don't support an
	// override on Meta and always arrive at the main endpoint).
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	byPNID, err := s.InstanceByPhoneNumberID("PNID1")
	if err != nil {
		t.Fatalf("InstanceByPhoneNumberID: %v", err)
	}
	if byPNID.Slug != "lojinha" {
		t.Errorf("Slug = %q", byPNID.Slug)
	}

	byWaba, err := s.InstanceByWabaID("WABA1")
	if err != nil {
		t.Fatalf("InstanceByWabaID: %v", err)
	}
	if byWaba.Slug != "lojinha" {
		t.Errorf("Slug = %q", byWaba.Slug)
	}
}

func TestStoreRefusesARepeatedSlug(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	if err := s.CreateInstance(testInstance()); err == nil {
		t.Fatal("slug repetido foi aceito — o slug e a URL do webhook, tem de ser unico")
	}
}

// --- Boundary validation: slug and callback_url -----------------------------
//
// Both live in CreateInstance, and not only in the subcommand, because the
// subcommand is just the FIRST creation path: the next one (an
// administration endpoint, a seed) would be born without them, which is
// this project's mother-of-all-traps ("the rule holds in one place and not
// in the next").
//
// And creation is the ONLY chance: the slug is IMMUTABLE after creation —
// there is no editing path — and the damage doesn't show up here, it shows
// up when /v1/inbound/{slug} is pasted into Meta and verification fails,
// with META's error message pointing at the wrong place.

func countInstances(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT count(*) FROM instancia`).Scan(&n); err != nil {
		t.Fatalf("contar instancias: %v", err)
	}
	return n
}

func TestCreateInstanceRefusesASlugOutsideTheShape(t *testing.T) {
	cases := []struct {
		name string
		slug string
	}{
		// The slash turns into /v1/inbound/loja/racer: the route doesn't
		// match, or matches another one. The `?` cuts off the path, so the
		// slug that arrives isn't what was typed. Uppercase creates a
		// different route, and whoever pasted it into Meta has no way to
		// suspect it. Percent-encoding turns back into a slash once decoded
		// on the path.
		{"barra", "loja/racer"},
		{"espaco", "loja racer"},
		{"maiuscula", "Lojinha"},
		{"hifen no comeco", "-lojinha"},
		{"hifen no fim", "lojinha-"},
		{"vazio", ""},
		{"so espacos", "   "},
		{"curto demais", "ab"},
		{"comprido demais", strings.Repeat("a", 41)},
		{"query string", "loja?x=1"},
		{"acento", "lojinha-café"},
		{"ponto", "lojinha.racer"},
		{"underscore", "lojinha_racer"},
		{"percent encoding", "loja%2Fracer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			i := testInstance()
			i.Slug = c.slug

			err := s.CreateInstance(i)

			if !errors.Is(err, ErrInvalidSlug) {
				t.Fatalf("CreateInstance(%q) = %v, quero ErrInvalidSlug", c.slug, err)
			}
			// The message has to SAY which rule broke: a bare "invalid
			// slug" makes whoever typed it guess among six rules.
			if err.Error() == ErrInvalidSlug.Error() {
				t.Errorf("o erro nao diz qual regra quebrou: %q", err.Error())
			}
			if n := countInstances(t, s); n != 0 {
				t.Errorf("%d instancia(s) gravada(s) apesar da recusa — a validacao correu DEPOIS do insert", n)
			}
		})
	}
}

func TestCreateInstanceAcceptsTheProductionSlugAndAnEmptyCallback(t *testing.T) {
	// The instance already in PRODUCTION: a slug with a hyphen in the
	// middle and an EMPTY callback_url — an outbound-only instance, its
	// webhook still lives on Evolution. Empty is not delivery in the
	// clear, it's absence of delivery. If the rule refused either of the
	// two, the rule would be wrong, not the instance.
	s := testStore(t)
	i := testInstance()
	i.Slug = "tenant-one"
	i.CallbackURL = ""

	if err := s.CreateInstance(i); err != nil {
		t.Fatalf("CreateInstance: %v — a validacao invalidou a instancia que roda hoje", err)
	}

	got, err := s.FindInstance("tenant-one")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if got.CallbackURL != "" {
		t.Errorf("CallbackURL = %q, quero vazia", got.CallbackURL)
	}
}

func TestCreateInstanceRefusesACallbackOutsideHTTPS(t *testing.T) {
	// Delivery to the consumer carries the message's RAW BODY — personal
	// data. It's signed by HMAC, but a signature proves INTEGRITY, not
	// confidentiality: over http:// the body crosses the LAN in the clear.
	// `marca` is the piece that IDENTIFIES the consumer and therefore
	// cannot appear in the error. Searching for the whole URL wouldn't
	// work: the message itself teaches the right shape ("has to be
	// https://"), so the `https://` case would match itself and the
	// assertion would turn into a false positive.
	cases := []struct {
		name string
		url  string
		mark string
	}{
		{"http externo", "http://consumidor.interno/webhooks/zapgw", "consumidor.interno"},
		{"http para IP da LAN", "http://10.0.0.19:9000/hook", "10.0.0.19"},
		// The following two are the "a guard has TWO sides" trap: a
		// HasPrefix("http://127.0.0.1") would accept both, and the local
		// test exception would become a door to the internet.
		{"host que so COMECA com 127.0.0.1", "http://127.0.0.1.consumidor.example/hook", "consumidor.example"},
		{"loopback no userinfo, host de fora", "http://127.0.0.1@consumidor.example/hook", "consumidor.example"},
		{"https no meio, nao no esquema", "http://consumidor.example/https://x", "consumidor.example"},
		{"esquema que nao e http", "ftp://consumidor.interno/hook", "consumidor.interno"},
		{"https sem host", "https://", ""},
		{"sem esquema nenhum", "consumidor.interno/hook", "consumidor.interno"},
		{"so espacos", "   ", ""},
		{"loopback vizinho, fora da excecao escrita", "http://127.0.0.2:9000", "127.0.0.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			i := testInstance()
			i.CallbackURL = c.url

			err := s.CreateInstance(i)

			if !errors.Is(err, ErrInsecureCallback) {
				t.Fatalf("CreateInstance com callback %q = %v, quero ErrInsecureCallback", c.url, err)
			}
			// The callback URL is encrypted at rest precisely to not reveal
			// the consumers' topology; the error goes to the log and the
			// terminal, so it must NOT carry the URL back
			// (docs/ARMADILHAS.md, "Erros e log").
			if c.mark != "" && strings.Contains(err.Error(), c.mark) {
				t.Errorf("o erro revela o destino da callback (%q): %q", c.mark, err.Error())
			}
			if n := countInstances(t, s); n != 0 {
				t.Errorf("%d instancia(s) gravada(s) apesar da recusa", n)
			}
		})
	}
}

func TestCreateInstanceAcceptsHTTPSCallbackAndTheTestLoopback(t *testing.T) {
	// The http:// exception is EXPLICIT and narrow: only 127.0.0.1 and
	// localhost, for local testing. Written as a pair with the refusal
	// table above — a broader guard breaks legitimate use, a narrower one
	// lets clear-text sending through.
	cases := []struct {
		name string
		url  string
	}{
		{"https", "https://consumidor.interno/webhooks/zapgw"},
		{"https com porta e query", "https://consumidor.interno:8443/hook?x=1"},
		{"vazia", ""},
		{"loopback de teste", "http://127.0.0.1:9000"},
		{"localhost de teste", "http://localhost:9000/hook"},
		{"esquema em maiuscula", "HTTPS://consumidor.interno/hook"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			i := testInstance()
			i.CallbackURL = c.url

			if err := s.CreateInstance(i); err != nil {
				t.Fatalf("CreateInstance com callback %q: %v", c.url, err)
			}

			got, err := s.FindInstance(i.Slug)
			if err != nil {
				t.Fatalf("FindInstance: %v", err)
			}
			if got.CallbackURL != c.url {
				t.Errorf("CallbackURL = %q, quero %q", got.CallbackURL, c.url)
			}
		})
	}
}

// --- Boundary validation: waba_id and phone_number_id (T-074, moved in T-079)
//
// WHY THIS EXISTS, and the test was born from a MUTATION THAT PASSED
// GREEN: removing the `waba == ""` from guard 5b
// (internal/inbound/handler.go) in T-068 left nothing red. That guard's
// real defense wasn't the comparison — it was the fact that every instance
// had waba_id filled in, and that was true of TODAY'S PATH (at the time
// `zapgw provisionar instancia` required the flags), not of the store. A
// future seed or administration endpoint would be born without that check.
//
// T-068's test (TestHandlerRecusaWabaIDIlegivelAindaQueAInstanciaNaoTenhaWabaID)
// proves the guard treats "" as unmatched; it does NOT stop an instance
// from ending up without a waba_id. They're different things, and this is
// the second one.
//
// 🔴 T-079 MOVED THIS TEST FROM CREATION TO REGISTRATION, AND THAT IS THE
// TASK AND NOT A RELAXATION. In the decided model (docs/MODELO-DE-USO.md)
// those two values belong to the CONSUMER: the instance is born with just
// the slug, and RegisterMeta is what writes them. Requiring them at
// creation became impossible to fulfill — the owner doesn't have them. The
// requirement moved PLACES, and what it prevents remains the same: writing
// half an identification.
func TestRegisterMetaRefusesEmptyIdentification(t *testing.T) {
	cases := []struct {
		name  string
		waba  string
		pnid  string
		field string // the field the error has to NAME
	}{
		{"sem waba_id", "", "PNID1", "waba_id"},
		{"sem phone_number_id", "WABA1", "", "phone_number_id"},
		{"sem os dois", "", "", "waba_id"},
		// Blank doesn't count as filled in: "   " is what's left over from
		// a field the consumer's panel sent empty, and it would never
		// match any waba_id.
		{"waba_id so de espacos", "   ", "PNID1", "waba_id"},
		{"phone_number_id so de espacos", "WABA1", "\t\n ", "phone_number_id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			if err := s.CreateInstance(instanceWithOnlyTheSlug("lojinha")); err != nil {
				t.Fatalf("CreateInstance: %v", err)
			}
			reg := testRegistration()
			reg.WabaID = c.waba
			reg.PhoneNumberID = c.pnid

			_, err := s.RegisterMeta("lojinha", reg, time.Now())

			if !errors.Is(err, ErrIncompleteIdentification) {
				t.Fatalf("RegisterMeta(waba=%q, pnid=%q) = %v, quero ErrIncompleteIdentification", c.waba, c.pnid, err)
			}
			// The error has to say WHICH of the two is missing: a bare
			// "incomplete identification" makes whoever typed it choose
			// between two fields.
			if !strings.Contains(err.Error(), c.field) {
				t.Errorf("o erro nao nomeia o campo que falta (%q): %q", c.field, err.Error())
			}
			// NOTHING was written: the validation ran BEFORE the UPDATE. If
			// it ran after, the instance would end up with half an
			// identification — the one state that's useless.
			r, err := s.SummarizeInstance("lojinha")
			if err != nil {
				t.Fatalf("SummarizeInstance: %v", err)
			}
			if r.WabaID != "" || r.PhoneNumberID != "" {
				t.Errorf("gravou identificacao apesar da recusa: waba=%q pnid=%q", r.WabaID, r.PhoneNumberID)
			}
			if r.RegisteredAt != "" {
				t.Errorf("a janela ABRIU num cadastro recusado (cadastro_em = %q) — um pedido invalido gastaria as 24h", r.RegisteredAt)
			}
		})
	}
}

func TestStoreIsBornWithTheInstancePaused(t *testing.T) {
	// An instance is born PAUSED. Only the smoke test
	// (cmd/zapgw/smoke.go) activates it — otherwise a misconfigured
	// instance enters production "working".
	s := testStore(t)
	i := testInstance()
	i.Active = true // even asking for active...
	if err := s.CreateInstance(i); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	got, _ := s.FindInstance("lojinha")
	if got.Active {
		t.Fatal("instancia nasceu ATIVA — tem de nascer pausada")
	}
}

func TestActivateInstanceTurnsTheInstanceOn(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	if err := s.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}

	got, err := s.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if !got.Active {
		t.Fatal("Ativo = false depois de ActivateInstance — o teste de fumaca nao teria como ligar a instancia")
	}
}

func TestActivateInstanceFlagsANonexistentSlug(t *testing.T) {
	// SILENT success is the expensive failure mode here: the smoke test
	// would print "instance activated" over a mistyped slug, and whoever
	// operated it would leave thinking they turned the channel on. The
	// real instance stays paused and the defect only shows up on the first
	// real send.
	s := testStore(t)

	err := s.ActivateInstance("slug-que-nao-existe")
	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("erro = %v, quero ErrInstanceNotFound", err)
	}
}

func TestPauseInstanceTurnsTheInstanceOff(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}

	if err := s.PauseInstance("lojinha"); err != nil {
		t.Fatalf("PauseInstance: %v", err)
	}

	got, err := s.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if got.Active {
		t.Fatal("Ativo = true depois de PauseInstance — nao ha como tirar do ar um canal quebrado")
	}
}

func TestPauseInstanceFlagsANonexistentSlug(t *testing.T) {
	// Same reason as ActivateInstance: pausing a wrong slug "successfully"
	// leaves running exactly the instance someone wanted to take offline.
	s := testStore(t)

	err := s.PauseInstance("slug-que-nao-existe")
	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("erro = %v, quero ErrInstanceNotFound", err)
	}
}

// --- Rotation of secrets for an instance that already exists ----------------------
//
// WHY THIS EXISTS: until T-017 the store had CreateInstance,
// ActivateInstance and PauseInstance — and nothing else. The five
// encrypted fields of an existing instance had NO write path, and spec
// §7.3 described the app_secret rotation procedure ("swap it on the
// gateway first, then on Meta") as if it were executable.
//
// THE NORMAL CASE IS A PARTIAL ROTATION: swap ONLY the app_secret.
// Whatever doesn't come in stays INTACT — silently zeroing the rest would
// erase the token_envio of an instance that is actually sending right now.

// --- T-048: remove instance ------------------------------------------------

// populateInstance puts one row into each table that belongs to the
// instance, so the removal has something to delete in ALL of them.
// Without this, a `DELETE` that missed a table would pass green deleting
// zero rows out of zero rows.
func populateInstance(t *testing.T, s *Store, slug string) {
	t.Helper()
	if err := s.IncrementCounter(slug, CounterReceived, time.Now()); err != nil {
		t.Fatalf("IncrementCounter: %v", err)
	}
	if err := s.RecordCallbackCertificate(slug, time.Now().Add(30*24*time.Hour), time.Now()); err != nil {
		t.Fatalf("RecordCallbackCertificate: %v", err)
	}
	if err := s.UpdateNumberAtMeta(slug, NumberUpdate{
		Quality: "GREEN", Limit: "TIER_250", Source: SourceMeasurement, When: time.Now(),
	}); err != nil {
		t.Fatalf("UpdateNumberAtMeta: %v", err)
	}
	if err := s.CreateConsumer("consumidor-de-"+slug, "token-de-"+slug, []string{slug}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	if err := s.WriteTransit(TransitRecord{
		Slug: slug, Direction: DirectionInbound, Correlation: "correlacao-de-" + slug,
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}
}

// rowsOf counts the rows of a table that belong to a slug.
func rowsOf(t *testing.T, s *Store, table, slug string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(
		`SELECT count(*) FROM `+table+` WHERE slug = ?`, slug).Scan(&n); err != nil {
		t.Fatalf("contar %s de %q: %v", table, slug, err)
	}
	return n
}

func TestRemoveInstanceDeletesEVERYTHINGOfItsAndNOTHINGThatIsNot(t *testing.T) {
	// TWO instances in the database: this is the test that matters. A
	// `DELETE` without the `WHERE slug` deletes the neighboring consumer's
	// instance, and with only ONE in the database it would pass green —
	// exactly the mistake this task exists to take out of people's hands.
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	neighbor := testInstance()
	neighbor.Slug = "clinica"
	neighbor.PhoneNumberID = "PNID2"
	neighbor.WabaID = "WABA2"
	if err := s.CreateInstance(neighbor); err != nil {
		t.Fatalf("CreateInstance vizinha: %v", err)
	}
	populateInstance(t, s, "lojinha")
	populateInstance(t, s, "clinica")

	deleted, err := s.RemoveInstance("lojinha")
	if err != nil {
		t.Fatalf("RemoveInstance: %v", err)
	}

	for _, table := range tablesWithSlug {
		if n := rowsOf(t, s, table, "lojinha"); n != 0 {
			t.Errorf("sobraram %d linha(s) em %s da instancia removida", n, table)
		}
		if n := rowsOf(t, s, table, "clinica"); n != 1 {
			t.Errorf("a instancia VIZINHA perdeu linha em %s: n = %d, quero 1", table, n)
		}
	}
	if _, err := s.FindInstance("lojinha"); !errors.Is(err, ErrInstanceNotFound) {
		t.Errorf("FindInstance depois de remover: err = %v", err)
	}
	if _, err := s.FindInstance("clinica"); err != nil {
		t.Errorf("a instancia vizinha sumiu junto: %v", err)
	}
	// The report has to match what happened: it's what goes to the
	// screen, and it's the only chance for someone to notice they deleted
	// the wrong instance.
	if len(deleted) != len(tablesWithSlug) {
		t.Fatalf("relatorio com %d tabelas, quero %d", len(deleted), len(tablesWithSlug))
	}
	for _, a := range deleted {
		if a.Rows != 1 {
			t.Errorf("%s: relatou %d linha(s), quero 1", a.Table, a.Rows)
		}
	}
}

// THE GUARD AGAINST THE NEXT ORPHANED TABLE.
//
// T-048 was written listing two tables; a day later T-064 created
// `certificado_do_callback`, and the memorized list was already wrong
// before anyone implemented it. This test asks the DATABASE which tables
// have a `slug` column and requires every one to be in `tablesWithSlug` —
// so table number five is born with a red test, instead of being born
// silently orphaned.
func TestRemoveInstanceCOVERSEveryTableWithASlugColumn(t *testing.T) {
	s := testStore(t)

	rows, err := s.DB().Query(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("listar tabelas: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("ler nome de tabela: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar tabelas: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("nenhuma tabela no banco de teste — o teste passaria sem verificar nada")
	}

	covered := map[string]bool{}
	for _, t := range tablesWithSlug {
		covered[t] = true
	}
	var withSlug int
	for _, table := range tables {
		var hasSlug int
		if err := s.DB().QueryRow(
			`SELECT count(*) FROM pragma_table_info(?) WHERE name = 'slug'`, table).Scan(&hasSlug); err != nil {
			t.Fatalf("colunas de %s: %v", table, err)
		}
		if hasSlug == 0 {
			continue
		}
		withSlug++
		if !covered[table] {
			t.Errorf("a tabela %q tem coluna `slug` e NAO esta em tablesWithSlug — "+
				"remover uma instancia deixaria linha orfa nela, invisivel ate alguem reusar o slug", table)
		}
	}
	if withSlug != len(tablesWithSlug) {
		t.Errorf("o banco tem %d tabela(s) com coluna `slug` e a lista tem %d — "+
			"uma entrada da lista nao corresponde a tabela nenhuma", withSlug, len(tablesWithSlug))
	}
}

func TestRemoveInstanceREFUSESAnACTIVEInstance(t *testing.T) {
	// Pausing is reversible; removing is not. Whoever really wants to
	// remove pauses first, and the pause gives a chance to look at what
	// stops working.
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	populateInstance(t, s, "lojinha")
	if err := s.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}

	if _, err := s.RemoveInstance("lojinha"); !errors.Is(err, ErrInstanceActive) {
		t.Fatalf("err = %v, quero ErrInstanceActive", err)
	}
	// The refusal cannot have deleted anything along the way.
	for _, table := range tablesWithSlug {
		if n := rowsOf(t, s, table, "lojinha"); n != 1 {
			t.Errorf("a recusa apagou linha em %s: n = %d, quero 1", table, n)
		}
	}
}

func TestRemoveInstanceFlagsANonexistentSlug(t *testing.T) {
	s := testStore(t)
	if _, err := s.RemoveInstance("nao-existe"); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("err = %v, quero ErrInstanceNotFound", err)
	}
}

func TestRemovingAPausedInstanceWorksAfterPausing(t *testing.T) {
	// The path the operator will actually walk: activate -> pause -> remove.
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.ActivateInstance("lojinha"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}
	if err := s.PauseInstance("lojinha"); err != nil {
		t.Fatalf("PauseInstance: %v", err)
	}
	if _, err := s.RemoveInstance("lojinha"); err != nil {
		t.Fatalf("RemoveInstance depois de pausar: %v", err)
	}
}

func ptr(v string) *string { return &v }

// instanceLikeTheProductionOne is this project's real instance at the time of
// T-017: `tenant-one`, ACTIVE and actually sending, with a REAL
// token_envio, RANDOM app_secret and verify_token (deliberate — a real
// value in there would suggest inbound is armed when it isn't) and an
// EMPTY callback_url. It's the one rotation exists to fix
// (docs/ARMAR-INBOUND.md, step 3), so it's the one that serves as the
// guinea pig here.
func instanceLikeTheProductionOne(t *testing.T, s *Store) Instance {
	t.Helper()
	i := testInstance()
	i.Slug = "tenant-one"
	i.AppSecret = "aleatorio-que-a-meta-NAO-conhece"
	i.VerifyToken = "aleatorio-que-a-meta-NAO-conhece-tambem"
	i.SendToken = "token-envio-REAL-que-esta-enviando-agora"
	i.DeliverySecret = "segredo-entrega-de-teste"
	i.CallbackURL = ""
	if err := s.CreateInstance(i); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	// It's ACTIVE in production. The factory needs to reproduce that: a
	// rotation that accidentally paused the instance would go unnoticed on
	// an instance that was already born paused.
	if err := s.ActivateInstance(i.Slug); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}
	i.Active = true
	return i
}

// T-017's central test: swapping ONLY the app_secret cannot touch anything
// else. The field that hurts most is token_envio — erasing it would
// silently take offline the only channel that actually sends today.
func TestRotatingSwapsOnlyTheAppSecretAndLeavesTheRestINTACT(t *testing.T) {
	s := testStore(t)
	before := instanceLikeTheProductionOne(t, s)

	if err := s.RotateInstance(before.Slug, Rotation{
		AppSecret: ptr("app-secret-REAL-do-painel-da-meta"),
	}); err != nil {
		t.Fatalf("RotateInstance: %v", err)
	}

	got, err := s.FindInstance(before.Slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if got.AppSecret != "app-secret-REAL-do-painel-da-meta" {
		t.Errorf("AppSecret = %q — a troca nao chegou ao banco", got.AppSecret)
	}
	untouched := []struct {
		field string
		got   string
		want  string
	}{
		{"VerifyToken", got.VerifyToken, before.VerifyToken},
		{"SendToken", got.SendToken, before.SendToken},
		{"DeliverySecret", got.DeliverySecret, before.DeliverySecret},
		{"CallbackURL", got.CallbackURL, before.CallbackURL},
		{"BundleCA", got.CABundle, before.CABundle},
		{"Slug", got.Slug, before.Slug},
		{"WabaID", got.WabaID, before.WabaID},
		{"PhoneNumberID", got.PhoneNumberID, before.PhoneNumberID},
		{"DisplayNumber", got.DisplayNumber, before.DisplayNumber},
	}
	for _, c := range untouched {
		if c.got != c.want {
			t.Errorf("%s = %q depois de rotacionar SO o app_secret, quero %q intacto", c.field, c.got, c.want)
		}
	}
	if got.TimeoutMs != before.TimeoutMs {
		t.Errorf("TimeoutMs = %d, quero %d intacto", got.TimeoutMs, before.TimeoutMs)
	}
	if !got.Active {
		t.Error("a instancia foi PAUSADA pela rotacao — o canal sairia do ar sozinho")
	}
}

func TestRotatingSwapsTheFiveFieldsAndReturnsInTheClear(t *testing.T) {
	// The other side of the test above: when all five come in, all five
	// swap. A guard that only knew how to leave things intact would be a
	// guard that doesn't rotate.
	s := testStore(t)
	before := instanceLikeTheProductionOne(t, s)

	newOnes := Rotation{
		AppSecret:      ptr("novo-app-secret"),
		VerifyToken:    ptr("novo-verify-token"),
		SendToken:      ptr("novo-token-envio"),
		DeliverySecret: ptr("novo-segredo-entrega"),
		CallbackURL:    ptr("https://novo-consumidor.interno/hook"),
	}
	if err := s.RotateInstance(before.Slug, newOnes); err != nil {
		t.Fatalf("RotateInstance: %v", err)
	}

	got, err := s.FindInstance(before.Slug)
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	// Field by field, with the NAME alongside: two parallel lists are how
	// a positional swap between credentials goes unnoticed
	// (docs/ARMADILHAS.md).
	check := []struct {
		field string
		got   string
		want  string
	}{
		{"AppSecret", got.AppSecret, *newOnes.AppSecret},
		{"VerifyToken", got.VerifyToken, *newOnes.VerifyToken},
		{"SendToken", got.SendToken, *newOnes.SendToken},
		{"DeliverySecret", got.DeliverySecret, *newOnes.DeliverySecret},
		{"CallbackURL", got.CallbackURL, *newOnes.CallbackURL},
	}
	for _, c := range check {
		if c.got != c.want {
			t.Errorf("%s = %q, quero %q", c.field, c.got, c.want)
		}
	}
}

// The rotated value has to go in ENCRYPTED, like the provisioned one. If
// the rotation wrote it in the clear, the database file — which goes into
// the nightly backup — would start carrying Meta's real app_secret
// readable, and nothing would flag it: reading it back would work just the same.
func TestRotatingWritesTheNEWValueENCRYPTEDInTheFile(t *testing.T) {
	vault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "teste.db")
	s, err := OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	i := instanceLikeTheProductionOne(t, s)
	if err := s.RotateInstance(i.Slug, Rotation{
		AppSecret:   ptr("app-secret-REAL-nao-pode-aparecer-em-claro"),
		CallbackURL: ptr("https://novo-consumidor.interno/hook"),
	}); err != nil {
		t.Fatalf("RotateInstance: %v", err)
	}
	_ = s.Close()

	raw := readFile(t, path)
	for _, secret := range []string{
		"app-secret-REAL-nao-pode-aparecer-em-claro",
		"https://novo-consumidor.interno/hook",
	} {
		if containsBytes(raw, secret) {
			t.Errorf("o valor rotacionado %q aparece EM CLARO no arquivo do banco", secret)
		}
	}
}

// THE SAME validation as creation, on the new path. This project's
// mother-of-all-traps is "the rule holds in one place and not in the
// next": a rotation that accepted an external http:// would make the raw
// body's delivery — personal data — cross the network readable, through a
// path creation closes off.
func TestRotatingRefusesACallbackOutsideHTTPS(t *testing.T) {
	cases := []struct {
		name string
		url  string
		mark string
	}{
		{"http externo", "http://consumidor.externo/webhooks/zapgw", "consumidor.externo"},
		{"http para IP da LAN", "http://10.0.0.19:9000/hook", "10.0.0.19"},
		{"host que so COMECA com 127.0.0.1", "http://127.0.0.1.consumidor.example/hook", "consumidor.example"},
		{"loopback no userinfo, host de fora", "http://127.0.0.1@consumidor.example/hook", "consumidor.example"},
		{"esquema que nao e http", "ftp://consumidor.externo/hook", "consumidor.externo"},
		{"https sem host", "https://", ""},
		{"so espacos", "   ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			// With a VALID https callback at the start: this way it's
			// possible to prove the refusal left nothing half-written.
			before := testInstance()
			if err := s.CreateInstance(before); err != nil {
				t.Fatalf("CreateInstance: %v", err)
			}

			err := s.RotateInstance(before.Slug, Rotation{
				AppSecret:   ptr("app-secret-novo"),
				CallbackURL: ptr(c.url),
			})

			if !errors.Is(err, ErrInsecureCallback) {
				t.Fatalf("RotateInstance com callback %q = %v, quero ErrInsecureCallback", c.url, err)
			}
			// The URL is encrypted at rest precisely to not reveal the
			// consumers' topology; the error goes to the log and the terminal.
			if c.mark != "" && strings.Contains(err.Error(), c.mark) {
				t.Errorf("o erro revela o destino da callback (%q): %q", c.mark, err.Error())
			}
			got, err := s.FindInstance(before.Slug)
			if err != nil {
				t.Fatalf("FindInstance: %v", err)
			}
			if got.CallbackURL != before.CallbackURL {
				t.Errorf("CallbackURL = %q apesar da recusa, quero %q", got.CallbackURL, before.CallbackURL)
			}
			if got.AppSecret != before.AppSecret {
				t.Error("o app_secret foi trocado apesar de a rotacao ter sido RECUSADA — a validacao correu depois do UPDATE")
			}
		})
	}
}

func TestRotatingAcceptsHTTPSCallbackAndTheTestLoopback(t *testing.T) {
	// The other side of the guard, written as a pair: narrower, it would
	// break legitimate rotation; broader, it would let clear-text sending
	// through. And the EMPTY callback remains valid — it's the way to turn
	// the instance back to "outbound only".
	cases := []struct {
		name string
		url  string
	}{
		{"https", "https://consumidor.interno/webhooks/zapgw"},
		{"https com porta e query", "https://consumidor.interno:8443/hook?x=1"},
		{"vazia (instancia so de saida)", ""},
		{"loopback de teste", "http://127.0.0.1:9000"},
		{"localhost de teste", "http://localhost:9000/hook"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			before := testInstance()
			if err := s.CreateInstance(before); err != nil {
				t.Fatalf("CreateInstance: %v", err)
			}

			if err := s.RotateInstance(before.Slug, Rotation{CallbackURL: ptr(c.url)}); err != nil {
				t.Fatalf("RotateInstance com callback %q: %v", c.url, err)
			}

			got, err := s.FindInstance(before.Slug)
			if err != nil {
				t.Fatalf("FindInstance: %v", err)
			}
			if got.CallbackURL != c.url {
				t.Errorf("CallbackURL = %q, quero %q", got.CallbackURL, c.url)
			}
		})
	}
}

// NEVER SILENT SUCCESS. An UPDATE that matches no row is NOT an error to
// SQLite: without checking RowsAffected, `zapgw instancia rotacionar`
// would print "swapped" over a mistyped slug, whoever operated it would
// leave thinking the real app_secret was on the gateway, and the real
// instance would keep the random one — the defect would only show up once
// Meta started delivering, far from the cause. Same trap that has already
// cost this project in setActive.
func TestRotatingFlagsANonexistentSlug(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	err := s.RotateInstance("slug-que-nao-existe", Rotation{AppSecret: ptr("x")})

	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("erro = %v, quero ErrInstanceNotFound", err)
	}
}

// A rotation with no field at all is a request that means nothing — and
// the dangerous outcome would be the silent one: printing "rotated"
// without having swapped anything (typical of someone who forgot to
// export the variable in the shell).
func TestRotatingWithNoFieldAtAllFlagsInsteadOfFakingSuccess(t *testing.T) {
	s := testStore(t)
	before := instanceLikeTheProductionOne(t, s)

	err := s.RotateInstance(before.Slug, Rotation{})

	if !errors.Is(err, ErrEmptyRotation) {
		t.Fatalf("erro = %v, quero ErrEmptyRotation", err)
	}
	got, _ := s.FindInstance(before.Slug)
	if got.AppSecret != before.AppSecret || got.SendToken != before.SendToken {
		t.Error("a rotacao vazia mexeu em campo")
	}
}

// ISOLATION BETWEEN TENANTS: an UPDATE without a WHERE (or with the wrong
// WHERE) would swap the app_secret of ALL instances — and the partial
// rotation test would pass green, because its own instance would also have
// been swapped. Only a second tenant in the database catches this.
func TestRotatingDoesNotTouchANOTHERInstance(t *testing.T) {
	s := testStore(t)
	target := instanceLikeTheProductionOne(t, s)

	neighbor := testInstance()
	neighbor.Slug = "clinica"
	neighbor.PhoneNumberID = "PNID2"
	neighbor.WabaID = "WABA2"
	neighbor.AppSecret = "app-secret-da-clinica"
	neighbor.SendToken = "token-envio-da-clinica"
	if err := s.CreateInstance(neighbor); err != nil {
		t.Fatalf("CreateInstance vizinha: %v", err)
	}

	if err := s.RotateInstance(target.Slug, Rotation{
		AppSecret: ptr("app-secret-so-da-lojinha"),
		SendToken: ptr("token-envio-so-da-lojinha"),
	}); err != nil {
		t.Fatalf("RotateInstance: %v", err)
	}

	got, err := s.FindInstance(neighbor.Slug)
	if err != nil {
		t.Fatalf("FindInstance vizinha: %v", err)
	}
	if got.AppSecret != neighbor.AppSecret {
		t.Errorf("o app_secret da OUTRA instancia virou %q — a rotacao vazou entre inquilinos", got.AppSecret)
	}
	if got.SendToken != neighbor.SendToken {
		t.Errorf("o token_envio da OUTRA instancia virou %q — o canal dela sairia do ar", got.SendToken)
	}
}

// --- List and show: seeing without opening the SQLite ---------------------------
//
// The operational read DECRYPTS NOTHING. It answers "is this field
// registered?", never "what is the value" — decrypting only to throw it
// away would open a window with the secret in the clear in the memory of a
// command that just wanted to count instances.
//
// The side effect is what matters most in an incident, and that's why it
// has its own test: listing keeps working with the WRONG encryption key,
// precisely the moment seeing the system's state matters most.

// ciphertextsOf turns the six fields into a name->registered map, checking
// along the way that there are SIX of them and no repeats. Without this
// check, a summary that returned four fields would pass green on every
// test that only queries the map.
func ciphertextsOf(t *testing.T, r InstanceSummary) map[string]bool {
	t.Helper()
	wantAll := []string{"app_secret", "verify_token", "token_envio", "callback_url", "segredo_entrega", "bundle_ca"}
	if len(r.Encrypted) != len(wantAll) {
		t.Fatalf("%q: %d campos cifrados, quero %d: %+v", r.Slug, len(r.Encrypted), len(wantAll), r.Encrypted)
	}
	m := map[string]bool{}
	for _, c := range r.Encrypted {
		if _, repeated := m[c.Name]; repeated {
			t.Fatalf("%q: o campo %q aparece duas vezes: %+v", r.Slug, c.Name, r.Encrypted)
		}
		m[c.Name] = c.Registered
	}
	for _, name := range wantAll {
		if _, has := m[name]; !has {
			t.Fatalf("%q: falta o campo %q no resumo: %+v", r.Slug, name, r.Encrypted)
		}
	}
	return m
}

// THREE tenants, only one active. A listing that returned just the first
// row, or that swapped active for paused, is worse than not existing:
// whoever operates it would stop seeing precisely the instance that's
// offline, and conclude everything is fine.
func TestListInstancesReturnsALLOfThemWithTheRightState(t *testing.T) {
	s := testStore(t)
	cases := []struct {
		slug   string
		active bool
		pnid   string
		waba   string
		number string
	}{
		{"tenant-one", true, "PNID-LOJA", "WABA-LOJA", "5532999990001"},
		{"clinica", false, "PNID-CLIN", "WABA-CLIN", "5532999990002"},
		{"padaria", false, "PNID-PADA", "WABA-PADA", "5532999990003"},
	}
	for _, c := range cases {
		i := testInstance()
		i.Slug, i.PhoneNumberID, i.WabaID, i.DisplayNumber = c.slug, c.pnid, c.waba, c.number
		i.TimeoutMs = 5000
		if err := s.CreateInstance(i); err != nil {
			t.Fatalf("CreateInstance(%q): %v", c.slug, err)
		}
		if c.active {
			if err := s.ActivateInstance(c.slug); err != nil {
				t.Fatalf("ActivateInstance(%q): %v", c.slug, err)
			}
		}
	}

	list, err := s.ListInstances()
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(list) != len(cases) {
		t.Fatalf("%d instancia(s) na lista, quero %d: %+v", len(list), len(cases), list)
	}
	// Order by slug: without a defined order, the same list comes out
	// different on every call and "what changed since yesterday?" stops
	// having an answer.
	if list[0].Slug != "clinica" || list[1].Slug != "padaria" || list[2].Slug != "tenant-one" {
		t.Errorf("a lista nao veio ordenada por slug: %q, %q, %q", list[0].Slug, list[1].Slug, list[2].Slug)
	}

	bySlug := map[string]InstanceSummary{}
	for _, r := range list {
		bySlug[r.Slug] = r
	}
	for _, c := range cases {
		r, found := bySlug[c.slug]
		if !found {
			t.Fatalf("a instancia %q nao apareceu na lista", c.slug)
		}
		if r.Active != c.active {
			t.Errorf("%q: Ativo = %v, quero %v — estado invertido manda quem opera mexer na instancia errada", c.slug, r.Active, c.active)
		}
		// The identifiers too: they aren't secret, and they're what lets
		// the row be matched to Meta's panel. Swapped between columns,
		// they send the operator looking for another tenant's number.
		if r.PhoneNumberID != c.pnid || r.WabaID != c.waba || r.DisplayNumber != c.number {
			t.Errorf("%q: pnid=%q waba=%q numero=%q, quero %q/%q/%q",
				c.slug, r.PhoneNumberID, r.WabaID, r.DisplayNumber, c.pnid, c.waba, c.number)
		}
		if r.TimeoutMs != 5000 {
			t.Errorf("%q: TimeoutMs = %d, quero 5000", c.slug, r.TimeoutMs)
		}
	}
}

func TestListInstancesOnAnEmptyDatabaseIsNotAnError(t *testing.T) {
	// A freshly created database is the first-provisioning case. An error
	// here would make someone just starting out think the command is broken.
	list, err := testStore(t).ListInstances()
	if err != nil {
		t.Fatalf("ListInstances em banco vazio: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("%d instancia(s) num banco vazio: %+v", len(list), list)
	}
}

// The question the summary answers is ONLY "is it registered?", and both
// answers have to be correct — including the NO. Empty is a legitimate
// state: an empty callback_url is the outbound-only instance (the one
// running in production today) and an empty bundle_ca is the system's CA
// store. Marking both as registered would send the operator looking for a
// defect that isn't there.
func TestListInstancesSaysWhoIsRegisteredInBothDirections(t *testing.T) {
	s := testStore(t)

	// The real instance: four secrets registered, callback and bundle empty.
	asInProduction := testInstance()
	asInProduction.Slug = "tenant-one"
	asInProduction.CallbackURL = ""
	asInProduction.CABundle = ""
	if err := s.CreateInstance(asInProduction); err != nil {
		t.Fatalf("CreateInstance(tenant-one): %v", err)
	}
	// And the other extreme: all six filled in.
	complete := testInstance()
	complete.Slug = "clinica"
	complete.PhoneNumberID = "PNID2"
	complete.WabaID = "WABA2"
	complete.CallbackURL = "https://consumidor.interno/webhooks/zapgw"
	complete.CABundle = testCA(t, "ca-do-consumidor-interno")
	if err := s.CreateInstance(complete); err != nil {
		t.Fatalf("CreateInstance(clinica): %v", err)
	}

	list, err := s.ListInstances()
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("%d instancia(s), quero 2", len(list))
	}
	bySlug := map[string]InstanceSummary{list[0].Slug: list[0], list[1].Slug: list[1]}

	want := map[string]map[string]bool{
		"tenant-one": {
			"app_secret": true, "verify_token": true, "token_envio": true,
			"segredo_entrega": true, "callback_url": false, "bundle_ca": false,
		},
		"clinica": {
			"app_secret": true, "verify_token": true, "token_envio": true,
			"segredo_entrega": true, "callback_url": true, "bundle_ca": true,
		},
	}
	for slug, expected := range want {
		got := ciphertextsOf(t, bySlug[slug])
		for field, isRegistered := range expected {
			if got[field] != isRegistered {
				t.Errorf("%s.%s: cadastrado = %v, quero %v", slug, field, got[field], isRegistered)
			}
		}
	}
}

// The SAME scenario, but on the database that EXISTS: `tenant-one` was
// written by a v0.4.0 binary, so its callback_url is the ciphertext of ""
// (CreateInstance encrypts all six, including the empty ones) and bundle_ca
// is the literally empty string the ALTER TABLE's DEFAULT left behind.
// These are TWO forms of "not registered", and a summary that only knew
// how to recognize one of them would show the single instance in
// production with one more field than it actually has.
func TestListInstancesRecognizesBothFormsOfEmptyInTheMigratedDatabase(t *testing.T) {
	path := priorDatabase(t, schemaWithRequestHash, 2)

	vault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	encrypt := func(plaintext string) string {
		t.Helper()
		c, err := vault.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("cifrar: %v", err)
		}
		return c
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco antigo: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO instancia (slug, waba_id, phone_number_id, numero_exibido,
		    app_secret, verify_token, token_envio, callback_url, segredo_entrega,
		    timeout_ms, ativo)
		VALUES ('tenant-one','WABA1','PNID1','5532999990000',?,?,?,?,?,5000,1)`,
		encrypt("app-secret-de-teste"), encrypt("verify-token-de-teste"),
		encrypt("token-envio-de-teste"), encrypt(""), encrypt("segredo-entrega-de-teste")); err != nil {
		t.Fatalf("inserir instancia antiga: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("fechar banco antigo: %v", err)
	}

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore sobre banco de v0.4.0: %v", err)
	}

	// The fixture only counts if both forms are really there: without
	// this check, the test could be asserting "not registered" over two
	// literally empty columns and never exercise the ciphertext of "".
	var callback, bundle string
	if err := s.DB().QueryRow(
		`SELECT callback_url, bundle_ca FROM instancia WHERE slug = 'tenant-one'`).Scan(&callback, &bundle); err != nil {
		t.Fatalf("ler as colunas cruas: %v", err)
	}
	if callback == "" {
		t.Fatal("a fixture gravou callback_url literalmente vazia — o caso do cifrado de \"\" nao seria exercitado")
	}
	if bundle != "" {
		t.Fatalf("a fixture nao produziu o DEFAULT '' do ALTER TABLE em bundle_ca (%q)", bundle)
	}

	list, err := s.ListInstances()
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("%d instancia(s), quero 1", len(list))
	}
	got := ciphertextsOf(t, list[0])
	for field, isRegistered := range map[string]bool{
		"app_secret": true, "verify_token": true, "token_envio": true,
		"segredo_entrega": true, "callback_url": false, "bundle_ca": false,
	} {
		if got[field] != isRegistered {
			t.Errorf("%s: cadastrado = %v, quero %v", field, got[field], isRegistered)
		}
	}
	if !list[0].Active {
		t.Error("a instancia apareceu PAUSADA — ela esta ativa no banco")
	}
}

// THE TEST THAT PROVES THERE IS NO DECRYPTION. With the wrong key there is
// no implementation that decrypts and still answers correctly: either it
// doesn't decrypt, or this test goes red.
//
// And it's the scenario that matters most: a wrong key is exactly the
// incident in which someone needs to see how many instances exist and
// which ones are up.
func TestListInstancesWorksWithTheWRONGENCRYPTIONKEY(t *testing.T) {
	const otherKey = "00000000000000000000000000000000000000000000000000000000000000ff"

	path := filepath.Join(t.TempDir(), "teste.db")
	rightVault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	rightStore, err := OpenStore(path, rightVault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	i := testInstance()
	i.Slug = "tenant-one"
	i.CallbackURL = "" // the outbound-only instance running today
	if err := rightStore.CreateInstance(i); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := rightStore.ActivateInstance(i.Slug); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}
	if err := rightStore.Close(); err != nil {
		t.Fatalf("Fechar: %v", err)
	}

	wrongVault, err := NewVault(otherKey)
	if err != nil {
		t.Fatalf("NewVault(outra): %v", err)
	}
	s, err := OpenStore(path, wrongVault)
	if err != nil {
		t.Fatalf("OpenStore com a outra chave: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// THE KEY REALLY NEEDS TO BE WRONG. Without this check the test would
	// pass green with both keys equal, claiming to have proven something
	// it never even exercised — the trap shape documented in docs/ARMADILHAS.md.
	if _, err := s.FindInstance("tenant-one"); err == nil {
		t.Fatal("FindInstance funcionou com a outra chave — a fixture nao trocou a chave de verdade")
	}

	list, err := s.ListInstances()
	if err != nil {
		t.Fatalf("ListInstances com a chave errada: %v — quem opera fica cego justamente no incidente", err)
	}
	if len(list) != 1 || list[0].Slug != "tenant-one" {
		t.Fatalf("lista = %+v, quero a tenant-one", list)
	}
	if !list[0].Active {
		t.Error("a instancia apareceu pausada com a chave errada — o estado nao depende da cifra")
	}
	got := ciphertextsOf(t, list[0])
	for field, isRegistered := range map[string]bool{
		"app_secret": true, "verify_token": true, "token_envio": true,
		"segredo_entrega": true, "callback_url": false, "bundle_ca": false,
	} {
		if got[field] != isRegistered {
			t.Errorf("%s: cadastrado = %v com a chave errada, quero %v", field, got[field], isRegistered)
		}
	}

	// And showing too: both reads answer the same question, and one that
	// decrypted would only break on the command nobody tested.
	if _, err := s.SummarizeInstance("tenant-one"); err != nil {
		t.Errorf("SummarizeInstance com a chave errada: %v", err)
	}
}

// Encrypting at rest and then returning the value in a summary makes the
// encryption decorative. And the CIPHERTEXT can't come out either: it's
// the material that only lacks the key to open, and a summary pasted into
// chat would hand it over whole.
func TestListInstancesReturnsNoSECRETAtAll(t *testing.T) {
	s := testStore(t)
	i := testInstance()
	i.Slug = "tenant-one"
	i.CABundle = testCA(t, "ca-do-consumidor-interno")
	if err := s.CreateInstance(i); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	list, err := s.ListInstances()
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	summary, err := s.SummarizeInstance("tenant-one")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	// The guard only counts if both reads returned the instance: over an
	// empty list, "no secret shows up" is trivially true and verifies nothing.
	if len(list) != 1 || summary.Slug != "tenant-one" {
		t.Fatalf("a leitura nao devolveu a instancia (lista=%+v, resumo=%+v) — a assercao de vazamento nao verificaria nada", list, summary)
	}
	// %+v ALONGSIDE %#v: %#v escapes the line break, and the CA bundle is
	// multiline — searching only the escaped form wouldn't find the whole
	// PEM printed raw. It's the same trap as a test that searches for the
	// input instead of the secret: the assertion runs and doesn't reach
	// what it's aiming at.
	everything := fmt.Sprintf("%+v %#v %+v %#v", list, list, summary, summary)

	// The values IN THE CLEAR.
	for name, plaintext := range map[string]string{
		"app_secret":      i.AppSecret,
		"verify_token":    i.VerifyToken,
		"token_envio":     i.SendToken,
		"segredo_entrega": i.DeliverySecret,
		"callback_url":    "consumidor.interno",
		"bundle_ca":       i.CABundle,
	} {
		if strings.Contains(everything, plaintext) {
			t.Errorf("o valor em claro de %s esta no resumo", name)
		}
	}

	// And the CIPHERTEXTS, read raw from the database — what a "just
	// return the column" implementation would produce, and it wouldn't be
	// flagged by any of the assertions above.
	var encrypted [6]string
	if err := s.DB().QueryRow(`
		SELECT app_secret, verify_token, token_envio, callback_url, segredo_entrega, bundle_ca
		  FROM instancia WHERE slug = 'tenant-one'`).Scan(
		&encrypted[0], &encrypted[1], &encrypted[2], &encrypted[3], &encrypted[4], &encrypted[5]); err != nil {
		t.Fatalf("ler as colunas cruas: %v", err)
	}
	for n, ciphertext := range encrypted {
		if ciphertext == "" {
			t.Fatalf("a coluna %d esta vazia no banco — a assercao procuraria a string vazia e nao verificaria nada", n)
		}
		if strings.Contains(everything, ciphertext) {
			t.Errorf("o CIFRADO da coluna %d esta no resumo — falta so a chave para abri-lo", n)
		}
	}
}

// NEVER SILENT SUCCESS: a zeroed summary returned for a mistyped slug would
// make the operator conclude the instance exists and is paused, when it
// doesn't exist.
func TestShowInstanceFlagsANonexistentSlug(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	got, err := s.SummarizeInstance("nao-existe")
	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("erro = %v, quero ErrInstanceNotFound", err)
	}
	if got.Slug != "" {
		t.Errorf("devolveu um resumo (%+v) junto com a recusa", got)
	}
}

// Both reads answer the SAME question and therefore have to give the SAME
// answer: two queries with their own columns diverge the day one of them
// changes, and the symptom would be `listar` and `mostrar` disagreeing
// about the same instance — this project's mother-of-all-traps.
func TestShowInstanceSaysTheSAMEAsTheListing(t *testing.T) {
	s := testStore(t)
	i := instanceLikeTheProductionOne(t, s)

	fromTheList, err := s.ListInstances()
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	one, err := s.SummarizeInstance(i.Slug)
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if len(fromTheList) != 1 {
		t.Fatalf("%d instancia(s) na lista, quero 1", len(fromTheList))
	}
	if fmt.Sprintf("%+v", fromTheList[0]) != fmt.Sprintf("%+v", one) {
		t.Errorf("listar e mostrar discordam sobre %q:\nlistar:  %+v\nmostrar: %+v", i.Slug, fromTheList[0], one)
	}
}

func TestStoreKeepsTheTokenAsAHashNeverInTheClear(t *testing.T) {
	// The consumer's token is NOT encrypted, it's HASHED: there is no use
	// case that needs it back. Encryption stores for reading later; a hash
	// proves without storing. If the file leaks, an encrypted token still
	// only depends on the key; a hash doesn't come back.
	vault, _ := NewVault(testKey)
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.CreateConsumer("consumer-a", "token-secreto-do-consumidor", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}
	_ = s.Close()

	if containsBytes(readFile(t, path), "token-secreto-do-consumidor") {
		t.Fatal("o token do consumidor aparece EM CLARO no arquivo do banco")
	}
}

func TestStoreFindsAConsumerByToken(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.CreateConsumer("consumer-a", "token-a", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}

	c, err := s.ConsumerByToken("token-a")
	if err != nil {
		t.Fatalf("ConsumerByToken: %v", err)
	}
	if c.Name != "consumer-a" {
		t.Errorf("Name = %q", c.Name)
	}
	if len(c.Instances) != 1 || c.Instances[0] != "lojinha" {
		t.Errorf("Instances = %v, quero [lojinha]", c.Instances)
	}
}

func TestStoreRefusesAnUnknownToken(t *testing.T) {
	s := testStore(t)

	if _, err := s.ConsumerByToken("token-que-nao-existe"); !errors.Is(err, ErrConsumerNotFound) {
		t.Fatalf("erro = %v, quero ErrConsumerNotFound", err)
	}
}

func TestStoreDoesNotConfuseConsumers(t *testing.T) {
	// TRAP this test prevents: if the consumidor->instancias link were read
	// by NAME instead of by token, or if two consumers shared a row,
	// system A's token would send a message through system B's number.
	// It's the project's requirement 3: send on behalf of N businesses
	// WITHOUT confusing one with another.
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	other := testInstance()
	other.Slug = "clinica"
	other.PhoneNumberID = "PNID2"
	other.WabaID = "WABA2"
	if err := s.CreateInstance(other); err != nil {
		t.Fatalf("CreateInstance outra: %v", err)
	}

	if err := s.CreateConsumer("sistema-a", "token-a", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer a: %v", err)
	}
	if err := s.CreateConsumer("sistema-b", "token-b", []string{"clinica"}); err != nil {
		t.Fatalf("CreateConsumer b: %v", err)
	}

	a, _ := s.ConsumerByToken("token-a")
	b, _ := s.ConsumerByToken("token-b")

	if len(a.Instances) != 1 || a.Instances[0] != "lojinha" {
		t.Errorf("A.Instances = %v", a.Instances)
	}
	if len(b.Instances) != 1 || b.Instances[0] != "clinica" {
		t.Errorf("B.Instances = %v", b.Instances)
	}
}

func TestStoreRefusesAConsumerWithARepeatedToken(t *testing.T) {
	s := testStore(t)
	_ = s.CreateInstance(testInstance())
	if err := s.CreateConsumer("a", "mesmo-token", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}

	if err := s.CreateConsumer("b", "mesmo-token", []string{"lojinha"}); err == nil {
		t.Fatal("dois consumidores com o mesmo token foram aceitos — o token E a identidade")
	}
}

// --- T-055: consumer token rotation -----------------------------------

func TestRotateConsumerKillsTheOldTokenAndSAVESTheLinks(t *testing.T) {
	// Both sides in the same assertion, because only both together mean
	// "rotation": the old one has to DIE (otherwise there was no
	// revocation) and the links have to STAY (otherwise the consumer's
	// next call turns into a 403 nobody connects to yesterday's rotation).
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.CreateConsumer("consumer-b", "token-vazado", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}

	if err := s.RotateConsumer("consumer-b", "token-novo"); err != nil {
		t.Fatalf("RotateConsumer: %v", err)
	}

	if _, err := s.ConsumerByToken("token-vazado"); !errors.Is(err, ErrConsumerNotFound) {
		t.Errorf("o token antigo ainda vale (err = %v) — a rotacao nao revogou nada", err)
	}
	c, err := s.ConsumerByToken("token-novo")
	if err != nil {
		t.Fatalf("o token novo nao autentica: %v", err)
	}
	if c.Name != "consumer-b" {
		t.Errorf("Name = %q, quero consumer-b", c.Name)
	}
	if len(c.Instances) != 1 || c.Instances[0] != "lojinha" {
		t.Errorf("Instances = %v, quero [lojinha] — o vinculo nao sobreviveu a rotacao", c.Instances)
	}
}

func TestRotateConsumerDoesNotTouchTheOTHERConsumer(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := s.CreateConsumer("sistema-a", "token-a", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer a: %v", err)
	}
	if err := s.CreateConsumer("sistema-b", "token-b", []string{"lojinha"}); err != nil {
		t.Fatalf("CreateConsumer b: %v", err)
	}

	if err := s.RotateConsumer("sistema-a", "token-a-novo"); err != nil {
		t.Fatalf("RotateConsumer: %v", err)
	}
	// An UPDATE without a WHERE would swap both — and the symptom would be
	// a consumer who asked for nothing losing access.
	if _, err := s.ConsumerByToken("token-b"); err != nil {
		t.Errorf("o token do OUTRO consumidor parou de valer: %v", err)
	}
}

func TestRotateConsumerFlagsANonexistentName(t *testing.T) {
	s := testStore(t)
	if err := s.RotateConsumer("nao-existe", "token"); !errors.Is(err, ErrConsumerNotFound) {
		t.Fatalf("err = %v, quero ErrConsumerNotFound", err)
	}
}

func TestListConsumersJoinsTheLinksAndShowsWhoHasNone(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	other := testInstance()
	other.Slug = "clinica"
	other.PhoneNumberID = "PNID2"
	other.WabaID = "WABA2"
	if err := s.CreateInstance(other); err != nil {
		t.Fatalf("CreateInstance outra: %v", err)
	}
	if err := s.CreateConsumer("sistema-a", "token-a", []string{"lojinha", "clinica"}); err != nil {
		t.Fatalf("CreateConsumer a: %v", err)
	}
	// With NO link at all: the case a plain JOIN would hide, and it's
	// exactly the one that gets 403 on everything.
	if _, err := s.DB().Exec(`INSERT INTO consumidor (nome, token_hash) VALUES ('orfao','h')`); err != nil {
		t.Fatalf("inserir orfao: %v", err)
	}

	list, err := s.ListConsumers()
	if err != nil {
		t.Fatalf("ListConsumers: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(lista) = %d, quero 2 (%v)", len(list), list)
	}
	// Stable order by name: "orfao" < "sistema-a".
	if list[0].Name != "orfao" || len(list[0].Instances) != 0 {
		t.Errorf("lista[0] = %+v, quero orfao sem vinculo", list[0])
	}
	if list[1].Name != "sistema-a" ||
		len(list[1].Instances) != 2 ||
		list[1].Instances[0] != "clinica" || list[1].Instances[1] != "lojinha" {
		t.Errorf("lista[1] = %+v, quero sistema-a com [clinica lojinha]", list[1])
	}
}

func TestHashTokenIsDeterministicAndHides(t *testing.T) {
	h1 := HashToken("abc")
	h2 := HashToken("abc")
	if h1 != h2 {
		t.Fatal("HashToken nao e deterministico — a busca por token nunca acharia")
	}
	if h1 == HashToken("abd") {
		t.Fatal("tokens diferentes deram o mesmo hash")
	}
	if strings.Contains(h1, "abc") {
		t.Fatal("o token aparece dentro do proprio hash")
	}
}

// CRITICAL found in the T1 review of plan 2.
// SQLite does NOT enforce foreign keys without PRAGMA foreign_keys = ON,
// and the schema declared REFERENCES without turning the pragma on. The
// clauses were DECORATIVE: a slug typo when provisioning would turn into
// an orphan link authorizing an instance nobody registered — no error, no log.
func TestTheForeignKeyPragmaIsOn(t *testing.T) {
	// This test asks the database, instead of trusting the DSN is right.
	// If the DSN's shape doesn't work for the driver, it fails LOUDLY
	// here, and not silently in the guard that depends on it.
	s := testStore(t)

	var on int
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("consultar o pragma: %v", err)
	}
	if on != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, quero 1 — as clausulas REFERENCES sao decorativas assim", on)
	}
}

func TestStoreRefusesAConsumerLinkedToANonexistentInstance(t *testing.T) {
	s := testStore(t)

	err := s.CreateConsumer("fantasma", "token-x", []string{"slug-que-nao-existe"})
	if err == nil {
		t.Fatal("vinculo orfao aceito — autorizaria uma instancia que ninguem cadastrou")
	}

	// And the consumer cannot have been left behind: the transaction undoes everything.
	if _, err := s.ConsumerByToken("token-x"); !errors.Is(err, ErrConsumerNotFound) {
		t.Fatalf("o consumidor sobrou apos a falha do vinculo: %v", err)
	}
}

func TestStoreRefusesABatchWithAnInvalidSlugInTheMiddle(t *testing.T) {
	// One good slug and one bad one in the SAME call: either everything
	// goes in, or nothing does. Half a link is worse than none — the
	// consumer would look provisioned.
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	err := s.CreateConsumer("meio", "token-y", []string{"lojinha", "nao-existe"})
	if err == nil {
		t.Fatal("lote com slug invalido foi aceito")
	}
	if _, err := s.ConsumerByToken("token-y"); !errors.Is(err, ErrConsumerNotFound) {
		t.Fatalf("o consumidor sobrou apos a falha parcial: %v", err)
	}
}

// The project's NAMED EXCEPTION: the gateway keeps (consumidor, chave) ->
// id. It's not a message — there is no content, recipient, or
// conversation. Without this, the Idempotency-Key the contract promises is
// FAKE, and a retry duplicates a message on a real customer's phone.
func TestIdempotencyBlocksASecondSend(t *testing.T) {
	s := testStore(t)

	alreadySent, reserved, err := s.ReserveIdempotency("sistema-a", "chave-1", "")
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if !reserved {
		t.Fatal("a primeira reserva nao foi concedida")
	}
	if alreadySent != "" {
		t.Fatalf("alreadySent = %q na primeira vez", alreadySent)
	}

	if err := s.ConfirmIdempotency("sistema-a", "chave-1", "wamid.PRIMEIRO"); err != nil {
		t.Fatalf("ConfirmIdempotency: %v", err)
	}

	alreadySent, reserved, err = s.ReserveIdempotency("sistema-a", "chave-1", "")
	if err != nil {
		t.Fatalf("ReserveIdempotency (2a): %v", err)
	}
	if reserved {
		t.Fatal("a segunda reserva foi concedida — a mensagem sairia DUAS vezes")
	}
	if alreadySent != "wamid.PRIMEIRO" {
		t.Fatalf("alreadySent = %q, quero o id do primeiro envio", alreadySent)
	}
}

func TestIdempotencyDoesNotConfuseConsumers(t *testing.T) {
	// The key is CHOSEN by the consumer. Two systems can choose the same
	// one without knowing it; if the key were global, one's send would
	// vanish because of the other.
	s := testStore(t)

	_, _, _ = s.ReserveIdempotency("sistema-a", "mesma-chave", "")
	_ = s.ConfirmIdempotency("sistema-a", "mesma-chave", "wamid.DO-A")

	alreadySent, reserved, err := s.ReserveIdempotency("sistema-b", "mesma-chave", "")
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if !reserved {
		t.Fatalf("o sistema B foi bloqueado pela chave do A (alreadySent=%q)", alreadySent)
	}
}

func TestIdempotencyReleasesWhenTheSendFails(t *testing.T) {
	// If Meta refused it, the key HAS to work again — otherwise the
	// consumer loses the message forever for having tried once.
	s := testStore(t)

	_, _, _ = s.ReserveIdempotency("sistema-a", "chave-1", "")
	if err := s.ReleaseIdempotency("sistema-a", "chave-1"); err != nil {
		t.Fatalf("ReleaseIdempotency: %v", err)
	}

	_, reserved, err := s.ReserveIdempotency("sistema-a", "chave-1", "")
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if !reserved {
		t.Fatal("a chave nao voltou a valer depois da falha")
	}
}

func TestIdempotencySecondCallWhileTheFirstIsRunning(t *testing.T) {
	// Reserved but not confirmed = send IN PROGRESS. The second call can't
	// reserve (would duplicate) nor receive an id (doesn't exist yet).
	s := testStore(t)

	_, _, _ = s.ReserveIdempotency("sistema-a", "chave-1", "")

	alreadySent, reserved, err := s.ReserveIdempotency("sistema-a", "chave-1", "")
	if err != nil {
		t.Fatalf("ReserveIdempotency: %v", err)
	}
	if reserved {
		t.Fatal("reservou duas vezes com um envio em andamento")
	}
	if alreadySent != "" {
		t.Fatalf("alreadySent = %q, mas o envio ainda nem terminou", alreadySent)
	}
}

func TestIdempotencyIsPurged(t *testing.T) {
	// A short TTL: it's a DELIVERY record, not history. Without purging,
	// the table grows forever and becomes the "store the message" the
	// project forbids.
	s := testStore(t)

	_, _, _ = s.ReserveIdempotency("sistema-a", "antiga", "")
	_ = s.ConfirmIdempotency("sistema-a", "antiga", "wamid.X")

	n, err := s.PurgeIdempotency(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("PurgeIdempotency: %v", err)
	}
	if n != 1 {
		t.Fatalf("purgou %d, quero 1", n)
	}

	_, reserved, _ := s.ReserveIdempotency("sistema-a", "antiga", "")
	if !reserved {
		t.Fatal("a chave purgada continua bloqueando")
	}
}

func TestThePurgeDoesNotDeleteARecentRecord(t *testing.T) {
	// The purge has to be SAFE: deleting a record still within the TTL
	// would turn a legitimate retry into a second message on the
	// customer's phone — exactly what idempotency exists to prevent.
	s := testStore(t)

	_, _, _ = s.ReserveIdempotency("sistema-a", "recente", "")
	_ = s.ConfirmIdempotency("sistema-a", "recente", "wamid.X")

	n, err := s.PurgeIdempotency(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("PurgeIdempotency: %v", err)
	}
	if n != 0 {
		t.Fatalf("purgou %d registros recentes, quero 0", n)
	}

	id, reserved, _ := s.ReserveIdempotency("sistema-a", "recente", "")
	if reserved {
		t.Fatal("o registro recente sumiu — um retry duplicaria a mensagem")
	}
	if id != "wamid.X" {
		t.Fatalf("id = %q", id)
	}
}

func TestIdempotencyKeepsNoMessageContent(t *testing.T) {
	// The EXCEPTION has a limit, and this test is the limit. If someone
	// one day adds the message body here "to resend later", that's the
	// QUEUE — and the queue belongs to the consumer.
	vault, _ := NewVault(testKey)
	path := filepath.Join(t.TempDir(), "t.db")
	s, _ := OpenStore(path, vault)
	_, _, _ = s.ReserveIdempotency("sistema-a", "chave-1", "")
	_ = s.ConfirmIdempotency("sistema-a", "chave-1", "wamid.X")
	_ = s.Close()

	raw := readFile(t, path)
	for _, column := range []string{"texto", "corpo", "payload", "mensagem", "para", "telefone"} {
		if containsBytes(raw, "idempotencia") && containsBytes(raw, column) {
			t.Errorf("a tabela de idempotencia parece guardar %q — isso e mensagem, nao entrega", column)
		}
	}
}

// THE CONTRACT TURNED INTO A TEST. ReserveIdempotency promises THREE
// outcomes, and none of them is "database error". A review ran 60
// concurrent goroutines and found a FOURTH: 58 came back with
// SQLITE_BUSY, because the DSN didn't configure busy_timeout and SQLite
// failed right away instead of waiting for the lock.
//
// This showed up exactly in the scenario idempotency exists to handle: a
// burst of simultaneous retries.
func TestIdempotencyUnderConcurrencyOnlyGivesTheThreeOutcomes(t *testing.T) {
	s := testStore(t)

	const goroutines = 60
	var wg sync.WaitGroup
	reservations := make([]bool, goroutines)
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_, reserved, err := s.ReserveIdempotency("sistema-a", "chave-disputada", "")
			reservations[n], errs[n] = reserved, err
		}(i)
	}
	wg.Wait()

	withError, withOwnership := 0, 0
	for i := range errs {
		if errs[i] != nil {
			withError++
			t.Errorf("goroutine %d voltou com erro: %v", i, errs[i])
		}
		if reservations[i] {
			withOwnership++
		}
	}

	if withError > 0 {
		t.Fatalf("%d de %d chamadas voltaram com erro de banco — o contrato promete tres desfechos, nenhum deles e erro",
			withError, goroutines)
	}
	if withOwnership != 1 {
		t.Fatalf("%d goroutines reservaram, quero exatamente 1 — mais de uma faria a mensagem sair mais de uma vez",
			withOwnership)
	}
}

func TestIdempotencyUnderConcurrencyOnDifferentKeys(t *testing.T) {
	// The other side: distinct keys shouldn't contend over anything with
	// each other. If busy_timeout were masking total serialization, this
	// would be slow but would still pass — what matters here is that ALL
	// of them reserve.
	s := testStore(t)

	const goroutines = 60
	var wg sync.WaitGroup
	reservations := make([]bool, goroutines)
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_, reserved, err := s.ReserveIdempotency("sistema-a", fmt.Sprintf("chave-%d", n), "")
			reservations[n], errs[n] = reserved, err
		}(i)
	}
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Errorf("goroutine %d voltou com erro: %v", i, errs[i])
		}
		if !reservations[i] {
			t.Errorf("goroutine %d nao reservou a propria chave, exclusiva dela", i)
		}
	}
}

func TestThePragmasAreAllOn(t *testing.T) {
	// Asks the database instead of trusting the DSN is right. If the
	// DSN's shape doesn't work for the driver, this fails LOUDLY here, and
	// not silently in the guarantee that depends on it.
	s := testStore(t)

	var fk int
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("consultar foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, quero 1", fk)
	}

	var busy int
	if err := s.DB().QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatalf("consultar busy_timeout: %v", err)
	}
	if busy <= 0 {
		t.Errorf("busy_timeout = %d, quero > 0 — sem ele o SQLite falha na hora em vez de esperar", busy)
	}

	var mode string
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("consultar journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, quero wal", mode)
	}
}

// Found in the final review, proven by test: same key + DIFFERENT request
// returned 200 with the first send's id, and the second message never went
// out. The consumer would record "sent" and have no way to detect it.
func TestIdempotencyRefusesAKeyReusedWithADifferentRequest(t *testing.T) {
	s := testStore(t)

	_, reserved, err := s.ReserveIdempotency("sistema-a", "pedido-12345", "hash-da-primeira")
	if err != nil || !reserved {
		t.Fatalf("primeira reserva: reservou=%v err=%v", reserved, err)
	}
	if err := s.ConfirmIdempotency("sistema-a", "pedido-12345", "wamid.PRIMEIRA"); err != nil {
		t.Fatalf("Confirmar: %v", err)
	}

	id, reserved, err := s.ReserveIdempotency("sistema-a", "pedido-12345", "hash-da-SEGUNDA")
	if !errors.Is(err, ErrKeyWithDifferentRequest) {
		t.Fatalf("erro = %v, quero ErrKeyWithDifferentRequest", err)
	}
	if reserved {
		t.Error("reservou com pedido diferente")
	}
	if id != "" {
		t.Errorf("devolveu id %q de OUTRA mensagem", id)
	}
}

func TestIdempotencyAcceptsARetryOfTheSameRequest(t *testing.T) {
	// The other side: a legitimate retry (same request, same key) has to
	// keep returning the same id, without sending again. Refusing here
	// would break the whole feature.
	s := testStore(t)

	_, _, _ = s.ReserveIdempotency("sistema-a", "k1", "mesmo-hash")
	_ = s.ConfirmIdempotency("sistema-a", "k1", "wamid.X")

	id, reserved, err := s.ReserveIdempotency("sistema-a", "k1", "mesmo-hash")
	if err != nil {
		t.Fatalf("retry legitimo recusado: %v", err)
	}
	if reserved {
		t.Error("reservou de novo em vez de devolver o id")
	}
	if id != "wamid.X" {
		t.Errorf("id = %q, quero wamid.X", id)
	}
}

// F5 — found in the adversarial re-review. The comparison had a
// "compatibility" branch (storedHash != "" && storedHash != requestHash)
// for a record predating the column's existence. At the time that state
// was unreachable; with the migration mechanism it became REAL —
// migration 2 adds hash_pedido with an empty DEFAULT to already-reserved
// rows —, which makes this test more necessary, not less. The effect of
// the removed branch would be treating ANY requestHash as equal to empty: a
// genuinely different request would slip past the guard
// ErrKeyWithDifferentRequest exists to enforce, and the second message would
// never go out.
func TestIdempotencyRefusesADifferentRequestEvenWithAnEmptyStoredHash(t *testing.T) {
	s := testStore(t)

	_, _, _ = s.ReserveIdempotency("sistema-a", "k1", "")
	_ = s.ConfirmIdempotency("sistema-a", "k1", "wamid.PRIMEIRA")

	id, reserved, err := s.ReserveIdempotency("sistema-a", "k1", "hash-de-um-pedido-diferente")
	if !errors.Is(err, ErrKeyWithDifferentRequest) {
		t.Fatalf("erro = %v, quero ErrKeyWithDifferentRequest", err)
	}
	if reserved {
		t.Error("reservou com pedido diferente so porque o hash gravado era vazio")
	}
	if id != "" {
		t.Errorf("devolveu id %q de OUTRA mensagem", id)
	}
}

func TestConfirmIdempotencyFlagsANonexistentKey(t *testing.T) {
	// The handler has an ALARM that describes exactly this scenario and
	// never fired, because an UPDATE with no match returned no error.
	s := testStore(t)

	err := s.ConfirmIdempotency("sistema-a", "nunca-reservada", "wamid.X")
	if !errors.Is(err, ErrIdempotencyVanished) {
		t.Fatalf("erro = %v, quero ErrIdempotencyVanished", err)
	}
}

// --- Schema migration -----------------------------------------------------

// schemaBeforeRequestHash is the schema as it was BEFORE the hash_pedido
// column, hand-written. A database created by an older binary has exactly
// this shape; the migration needs to work over it.
//
// It's hand-written on purpose: deriving it from the source's `esquema`
// would make the test track future changes and stop testing the old
// version — which is the only thing it exists to test.
const schemaBeforeRequestHash = `
CREATE TABLE IF NOT EXISTS instancia (
  slug             TEXT PRIMARY KEY,
  waba_id          TEXT NOT NULL,
  phone_number_id  TEXT NOT NULL,
  numero_exibido   TEXT NOT NULL DEFAULT '',
  app_secret       TEXT NOT NULL,
  verify_token     TEXT NOT NULL,
  token_envio      TEXT NOT NULL,
  callback_url     TEXT NOT NULL,
  segredo_entrega  TEXT NOT NULL,
  timeout_ms       INTEGER NOT NULL DEFAULT 5000,
  ativo            INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_instancia_pnid ON instancia(phone_number_id);
CREATE INDEX IF NOT EXISTS idx_instancia_waba ON instancia(waba_id);
CREATE TABLE IF NOT EXISTS consumidor (
  nome       TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS consumidor_instancia (
  consumidor TEXT NOT NULL REFERENCES consumidor(nome),
  slug       TEXT NOT NULL REFERENCES instancia(slug),
  PRIMARY KEY (consumidor, slug)
);
CREATE INDEX IF NOT EXISTS idx_consumidor_token ON consumidor(token_hash);
CREATE TABLE IF NOT EXISTS idempotencia (
  consumidor      TEXT NOT NULL,
  chave           TEXT NOT NULL,
  wa_message_id   TEXT NOT NULL DEFAULT '',
  criado_em       INTEGER NOT NULL,
  PRIMARY KEY (consumidor, chave)
);
CREATE INDEX IF NOT EXISTS idx_idempotencia_idade ON idempotencia(criado_em);
`

// schemaWithRequestHash is the schema of a v0.3.0 database: the column is
// already there (it was born inside the CREATE TABLE), but user_version
// was never written because the migration mechanism didn't exist. It's the
// shape of EVERY database in production at the moment this mechanism ships.
const schemaWithRequestHash = schemaBeforeRequestHash + `
ALTER TABLE idempotencia ADD COLUMN hash_pedido TEXT NOT NULL DEFAULT '';
`

// priorDatabase creates a SQLite file with the requested DDL and
// user_version, WITHOUT going through OpenStore — it's the only way to
// simulate a database that already existed.
func priorDatabase(t *testing.T, ddl string, userVersion int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "antigo.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco antigo: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("criar esquema antigo: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", userVersion)); err != nil {
		t.Fatalf("gravar user_version: %v", err)
	}
	return path
}

func columns(t *testing.T, s *Store, table string) map[string]bool {
	t.Helper()
	rows, err := s.DB().Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("ler coluna: %v", err)
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar colunas: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("a tabela %q nao tem coluna nenhuma — a consulta nao verificou nada", table)
	}
	return names
}

func openAt(t *testing.T, path string) (*Store, error) {
	t.Helper()
	vault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	s, err := OpenStore(path, vault)
	if s != nil {
		t.Cleanup(func() { _ = s.Close() })
	}
	return s, err
}

// The trap this test prevents: a new column inside CREATE TABLE IF NOT
// EXISTS does NOT reach a database that already exists, and the IF NOT
// EXISTS hides that. Startup passes clean and EVERY send starts returning
// 503 with "table idempotencia has no column named hash_pedido" — total
// failure, visible only on the first send.
func TestOpenStoreMigratesADatabaseFromThePreviousVersionAndSendingWorks(t *testing.T) {
	path := priorDatabase(t, schemaBeforeRequestHash, 0)

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore sobre banco da versao anterior: %v", err)
	}

	if !columns(t, s, "idempotencia")["hash_pedido"] {
		t.Fatal("a coluna hash_pedido NAO chegou ao banco que ja existia — todo envio devolveria 503")
	}

	// And a send needs to work end to end over the migrated database:
	// reserve, confirm, and the different-request guard in force.
	_, reserved, err := s.ReserveIdempotency("sistema-a", "k1", "hash-do-pedido")
	if err != nil || !reserved {
		t.Fatalf("reservar no banco migrado: reservou=%v err=%v", reserved, err)
	}
	if err := s.ConfirmIdempotency("sistema-a", "k1", "wamid.X"); err != nil {
		t.Fatalf("confirmar no banco migrado: %v", err)
	}
	id, reserved, err := s.ReserveIdempotency("sistema-a", "k1", "hash-do-pedido")
	if err != nil || reserved || id != "wamid.X" {
		t.Fatalf("retry legitimo: id=%q reservou=%v err=%v", id, reserved, err)
	}
	if _, _, err := s.ReserveIdempotency("sistema-a", "k1", "outro-hash"); !errors.Is(err, ErrKeyWithDifferentRequest) {
		t.Fatalf("erro = %v, quero ErrKeyWithDifferentRequest — a guarda nao veio junto com a coluna", err)
	}
}

// The other side, and the real case of every v0.3.0 database: the column
// ALREADY exists and user_version is still 0. A migration that blindly ran
// ALTER TABLE would break startup with "duplicate column name" — and the
// production database is exactly this one.
func TestOpenStoreDoesNotBreakOnADatabaseThatAlreadyHasTheColumnWithoutAVersion(t *testing.T) {
	path := priorDatabase(t, schemaWithRequestHash, 0)

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore sobre banco que ja tem a coluna: %v", err)
	}
	if !columns(t, s, "idempotencia")["hash_pedido"] {
		t.Fatal("a coluna hash_pedido sumiu")
	}
}

// A database written by a NEWER binary cannot be opened by this one.
// Starting up like that would give no error right away: the old binary
// would write with the old rules over what the new one writes, and the
// damage would show up far away.
func TestOpenStoreRefusesASchemaFromTheFuture(t *testing.T) {
	path := priorDatabase(t, schemaWithRequestHash, len(migrations)+1)

	s, err := openAt(t, path)
	if !errors.Is(err, ErrSchemaFromTheFuture) {
		t.Fatalf("erro = %v, quero ErrSchemaFromTheFuture", err)
	}
	if s != nil {
		t.Error("devolveu um Store utilizavel apesar de recusar o esquema")
	}
}

func TestANewDatabaseIsBornAtTheCurrentVersionAndReopeningDoesNotMigrateAgain(t *testing.T) {
	// Reopening is the common case (every service restart). The second
	// open cannot fail nor touch the version: a migration that isn't
	// idempotent breaks the restart, which is precisely when nobody is watching.
	path := filepath.Join(t.TempDir(), "novo.db")

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("primeira abertura: %v", err)
	}
	if v := schemaVersionRead(t, s); v != len(migrations) {
		t.Fatalf("user_version = %d apos criar, quero %d", v, len(migrations))
	}
	if !columns(t, s, "idempotencia")["hash_pedido"] {
		t.Fatal("banco novo nasceu sem hash_pedido")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Fechar: %v", err)
	}

	s2, err := openAt(t, path)
	if err != nil {
		t.Fatalf("reabertura: %v", err)
	}
	if v := schemaVersionRead(t, s2); v != len(migrations) {
		t.Fatalf("user_version = %d apos reabrir, quero %d", v, len(migrations))
	}
}

// A HALF-migrated schema is the worst possible outcome: startup passes and
// the failure shows up on the first write, in production. This test
// proves the transaction with a migration that fails on purpose — the
// previous step cannot survive, and the version has to stay the old one,
// so the next startup tries again.
func TestAMigrationThatFailsLeavesNoHalfSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meio.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir: %v", err)
	}
	defer func() { _ = db.Close() }()

	boom := errors.New("boom")
	steps := []migration{
		{"cria a tabela marca", func(ctx context.Context, c *sql.Conn) error {
			_, err := c.ExecContext(ctx, `CREATE TABLE marca (x INTEGER)`)
			return err
		}},
		{"falha de proposito", func(context.Context, *sql.Conn) error { return boom }},
	}

	if err := migrate(context.Background(), db, steps); !errors.Is(err, boom) {
		t.Fatalf("erro = %v, quero o erro da migracao que falhou", err)
	}

	var howMany int
	if err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='marca'`).Scan(&howMany); err != nil {
		t.Fatalf("consultar sqlite_master: %v", err)
	}
	if howMany != 0 {
		t.Error("a tabela criada pela migracao 1 sobreviveu a falha da 2 — o banco ficou pela metade")
	}

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("ler user_version: %v", err)
	}
	if version != 0 {
		t.Errorf("user_version = %d apos falhar, quero 0 — o banco se diria migrado sem estar", version)
	}
}

func schemaVersionRead(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("ler user_version: %v", err)
	}
	return v
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return b
}

func containsBytes(content []byte, needle string) bool {
	return bytes.Contains(content, []byte(needle))
}

// --- Per-instance CA bundle ----------------------------------------------
//
// THE NARROW EXIT from the "TLS — não existe modo desligado" rule (CLAUDE.md): a
// consumer with their own CA registers THEIR CA on this instance, and
// delivery keeps VERIFYING — only the trust anchor changes. There is no
// boolean.

// testCA returns the PEM of a self-signed CA certificate.
func testCA(t *testing.T, name string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(7),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("criar certificado: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestStoreKeepsAndReturnsTheCABundle(t *testing.T) {
	s := testStore(t)
	ca := testCA(t, "ca-do-consumidor-interno")

	i := testInstance()
	i.CABundle = ca
	if err := s.CreateInstance(i); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	got, err := s.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if got.CABundle != ca {
		t.Errorf("BundleCA nao sobreviveu a ida e volta pela cifra:\n%q", got.CABundle)
	}
}

// The bundle goes into the ENCRYPTED set for the same reason as
// callback_url: it isn't a secret in the strict sense (the server's
// certificate travels in the clear on every handshake), but it says WHO
// the consumer of that instance is — topology a stolen backup file must
// not reveal.
func TestStoreKeepsTheCABundleENCRYPTEDInTheFile(t *testing.T) {
	vault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "teste.db")
	s, err := OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	ca := testCA(t, "ca-do-consumidor-interno")
	i := testInstance()
	i.CABundle = ca
	if err := s.CreateInstance(i); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	_ = s.Close()

	raw := readFile(t, path)
	// The whole PEM AND a piece from its middle: a partial leak (just the
	// base64 body, without the delimiters) reveals the same topology.
	rows := strings.Split(strings.TrimSpace(ca), "\n")
	for _, needle := range []string{ca, rows[1]} {
		if containsBytes(raw, needle) {
			t.Errorf("o bundle de CA aparece EM CLARO no arquivo do banco (%q)", needle)
		}
	}
}

// An unreadable bundle cannot be accepted at registration: accepted, it
// would only show up on the FIRST delivery — and then the whole instance
// goes mute without creation having flagged anything.
func TestCreateInstanceRefusesACABundleWithNoCertificate(t *testing.T) {
	cases := map[string]string{
		"texto solto":              "isto nao e um PEM",
		"PEM sem certificado":      "-----BEGIN CERTIFICATE-----\nnao sou base64 de um cert\n-----END CERTIFICATE-----\n",
		"bloco de outro tipo":      "-----BEGIN PRIVATE KEY-----\nMHcCAQEEIA==\n-----END PRIVATE KEY-----\n",
		"so espaco em branco":      "   \n\t ",
		"cabecalho sem o corpo":    "-----BEGIN CERTIFICATE-----\n",
		"base64 que nao e um X509": "-----BEGIN CERTIFICATE-----\naGVsbG8gbXVuZG8=\n-----END CERTIFICATE-----\n",
	}
	for name, bundle := range cases {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			i := testInstance()
			i.CABundle = bundle
			if err := s.CreateInstance(i); !errors.Is(err, ErrInvalidCABundle) {
				t.Fatalf("erro = %v, quero ErrInvalidCABundle", err)
			}
			if n := countInstances(t, s); n != 0 {
				t.Errorf("%d instancia(s) criada(s) apesar da recusa", n)
			}
		})
	}
}

// An EMPTY bundle remains valid, and it's not a relaxation: without a
// bundle the system's CA store applies, which is the normal case (a
// consumer with a public CA certificate). It's the case for EVERY instance
// that exists today.
func TestCreateInstanceAcceptsAnInstanceWithNoCABundle(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(testInstance()); err != nil {
		t.Fatalf("CreateInstance sem bundle: %v", err)
	}
	got, err := s.FindInstance("lojinha")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if got.CABundle != "" {
		t.Errorf("BundleCA = %q, quero vazio", got.CABundle)
	}
}

// THIS MIGRATION'S TRAP, and it brings down the instance running in
// production TODAY: the other sensitive columns store CIPHERTEXT, and an
// ALTER TABLE's DEFAULT is the empty string — which is not a valid
// ciphertext. If the read blindly decrypted the new column, EVERY instance
// that already existed would start returning "config: decifrar", and the
// whole gateway (webhook and sending) would die on the first request after
// the update.
func TestOpenStoreMigratesAPreExistingInstanceWithNoCABundle(t *testing.T) {
	// A v0.4.0 database: schema with hash_pedido, user_version 2.
	path := priorDatabase(t, schemaWithRequestHash, 2)

	vault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	encrypt := func(plaintext string) string {
		t.Helper()
		c, err := vault.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("cifrar: %v", err)
		}
		return c
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco antigo: %v", err)
	}
	// The real instance: EMPTY callback_url (outbound only), like the one running today.
	if _, err := db.Exec(`
		INSERT INTO instancia (slug, waba_id, phone_number_id, numero_exibido,
		    app_secret, verify_token, token_envio, callback_url, segredo_entrega,
		    timeout_ms, ativo)
		VALUES ('tenant-one','WABA1','PNID1','5532999990000',?,?,?,?,?,5000,1)`,
		encrypt("app-secret-de-teste"), encrypt("verify-token-de-teste"),
		encrypt("token-envio-de-teste"), encrypt(""), encrypt("segredo-entrega-de-teste")); err != nil {
		t.Fatalf("inserir instancia antiga: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("fechar banco antigo: %v", err)
	}

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore sobre banco de v0.4.0: %v", err)
	}
	if !columns(t, s, "instancia")["bundle_ca"] {
		t.Fatal("a coluna bundle_ca NAO chegou ao banco que ja existia")
	}

	got, err := s.FindInstance("tenant-one")
	if err != nil {
		t.Fatalf("FindInstance depois da migracao: %v — a instancia que roda em producao parou de ser legivel", err)
	}
	if got.CABundle != "" {
		t.Errorf("BundleCA = %q, quero vazio", got.CABundle)
	}
	if got.AppSecret != "app-secret-de-teste" || got.DeliverySecret != "segredo-entrega-de-teste" {
		t.Errorf("os segredos da instancia antiga nao voltaram inteiros: %+v", got)
	}
	if got.CallbackURL != "" {
		t.Errorf("CallbackURL = %q, quero vazia", got.CallbackURL)
	}
	if !got.Active {
		t.Error("a instancia voltou PAUSADA depois da migracao — o canal sairia do ar sozinho")
	}
}

// T-070, the MIGRATION half: an instance that ALREADY EXISTED when the
// column was born has to come out of the migration with `carimbos_desde` filled in.
//
// THE ALTER TABLE'S ” DEFAULT IS NOT AN ANSWER: it isn't any date, and
// would reach the consumer as an empty field in a place where it expects
// an instant — the same defect shape this field exists to close
// (`ultimo_em: null` ambiguous between "never happened" and "happened
// before there was a stamp"). That's why the migration does the ALTER
// **and** the UPDATE.
func TestTheMigrationFillsStampsSinceOnAPreExistingInstance(t *testing.T) {
	// Truncated to the second because the stamp is too: without this the
	// comparison would lose by milliseconds and the test would fail from
	// rounding, not from a defect.
	beforeTheMigration := time.Now().UTC().Truncate(time.Second)

	// A v0.4.0 database, like the one running in production: schema with
	// hash_pedido, user_version 2. All the following migrations run.
	path := priorDatabase(t, schemaWithRequestHash, 2)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco antigo: %v", err)
	}
	// Arbitrary values in the encrypted columns: this test reads through
	// SummarizeInstance, which decrypts NOTHING — what it asserts is about
	// the new column.
	if _, err := db.Exec(`
		INSERT INTO instancia (slug, waba_id, phone_number_id, numero_exibido,
		    app_secret, verify_token, token_envio, callback_url, segredo_entrega,
		    timeout_ms, ativo)
		VALUES ('tenant-one','WABA1','PNID1','5532999990000','x','x','x','x','x',5000,1)`); err != nil {
		t.Fatalf("inserir instancia antiga: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("fechar banco antigo: %v", err)
	}

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore sobre banco de v0.4.0: %v", err)
	}
	r, err := s.SummarizeInstance("tenant-one")
	if err != nil {
		t.Fatalf("SummarizeInstance depois da migracao: %v", err)
	}
	if r.StampsSince == "" {
		t.Fatal("carimbos_desde ficou VAZIO na instancia pre-existente — o ALTER TABLE rodou e o UPDATE nao")
	}
	when, err := time.Parse(time.RFC3339, r.StampsSince)
	if err != nil {
		t.Fatalf("carimbos_desde = %q, que nao e RFC3339: %v", r.StampsSince, err)
	}
	// It has to be the instant the MIGRATION ran. A compiled date (the
	// v0.23.0 one, for example) would pass "isn't empty" and would lie on
	// every instance — and lie in the dangerous direction, claiming stamp
	// coverage that never happened.
	if when.Before(beforeTheMigration) {
		t.Errorf("carimbos_desde = %q, anterior ao instante em que a migracao rodou (%s) — parece constante compilada",
			r.StampsSince, beforeTheMigration.Format(time.RFC3339))
	}
}

// T-098: the SAME test as TestTheMigrationFillsStampsSinceOnAPreExistingInstance,
// now for token_definido_em — and token_renovado_em has to stay EMPTY,
// because "" is the CORRECT answer for "the automatic loop never renewed
// this token" on every row that existed before this task.
func TestTheMigrationFillsTokenSetAtAndKeepsTokenRenewedAtEmpty(t *testing.T) {
	beforeTheMigration := time.Now().UTC().Truncate(time.Second)

	path := priorDatabase(t, schemaWithRequestHash, 2)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("abrir banco antigo: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO instancia (slug, waba_id, phone_number_id, numero_exibido,
		    app_secret, verify_token, token_envio, callback_url, segredo_entrega,
		    timeout_ms, ativo)
		VALUES ('tenant-one','WABA1','PNID1','5532999990000','x','x','x','x','x',5000,1)`); err != nil {
		t.Fatalf("inserir instancia antiga: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("fechar banco antigo: %v", err)
	}

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore sobre banco de v0.4.0: %v", err)
	}
	r, err := s.SummarizeInstance("tenant-one")
	if err != nil {
		t.Fatalf("SummarizeInstance depois da migracao: %v", err)
	}
	if r.TokenSetAt == "" {
		t.Fatal("token_definido_em ficou VAZIO na instancia pre-existente — o ALTER TABLE rodou e o UPDATE nao")
	}
	when, err := time.Parse(time.RFC3339, r.TokenSetAt)
	if err != nil {
		t.Fatalf("token_definido_em = %q, que nao e RFC3339: %v", r.TokenSetAt, err)
	}
	if when.Before(beforeTheMigration) {
		t.Errorf("token_definido_em = %q, anterior ao instante em que a migracao rodou (%s) — parece constante compilada",
			r.TokenSetAt, beforeTheMigration.Format(time.RFC3339))
	}
	if r.TokenRenewedAt != "" {
		t.Errorf("token_renovado_em = %q, quero \"\" — o laco automatico nunca renovou este token", r.TokenRenewedAt)
	}
}

// T-070, the DATA PER INSTANCE half — and this is the test the task's
// mandatory mutation targets.
//
// TWO instances created at DIFFERENT instants, because with only one the
// test doesn't distinguish "one value per instance" from "an equal
// constant for all", which is exactly the defect. And the instants come in
// as a parameter (CreateInstanceAt) because the stamp has no fractional
// second: two creations with time.Now() would land on the same text and
// the test would go back to proving nothing.
func TestStampsSinceIsPerInstanceNotAConstant(t *testing.T) {
	s := testStore(t)

	birthOfTheFirst := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	birthOfTheSecond := birthOfTheFirst.Add(72 * time.Hour)

	first := testInstance()
	if err := s.CreateInstanceAt(first, birthOfTheFirst); err != nil {
		t.Fatalf("CreateInstanceAt(primeira): %v", err)
	}
	second := testInstance()
	second.Slug = "clinica"
	second.PhoneNumberID = "PNID2"
	second.WabaID = "WABA2"
	if err := s.CreateInstanceAt(second, birthOfTheSecond); err != nil {
		t.Fatalf("CreateInstanceAt(segunda): %v", err)
	}

	rp, err := s.SummarizeInstance(first.Slug)
	if err != nil {
		t.Fatalf("SummarizeInstance(%q): %v", first.Slug, err)
	}
	rs, err := s.SummarizeInstance(second.Slug)
	if err != nil {
		t.Fatalf("SummarizeInstance(%q): %v", second.Slug, err)
	}

	if want := birthOfTheFirst.Format(time.RFC3339); rp.StampsSince != want {
		t.Errorf("%s.carimbos_desde = %q, quero %q", first.Slug, rp.StampsSince, want)
	}
	if want := birthOfTheSecond.Format(time.RFC3339); rs.StampsSince != want {
		t.Errorf("%s.carimbos_desde = %q, quero %q", second.Slug, rs.StampsSince, want)
	}
	if rp.StampsSince == rs.StampsSince {
		t.Errorf("as duas instancias respondem %q — isto e uma constante global, nao um dado por instancia",
			rp.StampsSince)
	}
}

// migrationIndex finds, BY NAME, a migration's position in the list —
// never by a hand-typed position.
//
// T-097: `migracoes[:len(migracoes)-1]` (what this test had before) only
// worked while T-094's migration was the LAST one in the list — and
// "a new schema goes in as a NEW migration at the end of the list" (header
// of "Schema and migrations", store.go) guarantees it STOPS being the last
// one on the very next task that adds a column to ANY table, Instagram or
// not. It's the same lesson from docs/ARMADILHAS.md ("toda lista escrita
// à mão sobre o esquema precisa de algo que pergunte ao esquema"),
// applied to the ORDER of the migration list instead of to
// column names. `t.Fatalf` and not a silent index -1: a name that
// disappears from the list (a renamed migration, which should never happen
// — see the same header) has to bring the test down SHOUTING which name
// was missing.
func migrationIndex(t *testing.T, name string) int {
	t.Helper()
	for i, m := range migrations {
		if m.name == name {
			return i
		}
	}
	t.Fatalf("migracao %q nao existe na lista — o nome mudou?", name)
	return -1
}

// TestTheTransitMigrationCounterpartyAndWamidInTheClearReplaceTheHMAC proves T-094's
// migration (2026-07-30, owner's decision: "you can put the number in,
// it's not a secret") against a database IN THE REAL STATE OF PRODUCTION —
// v0.32.0 already had the `transito` table with hmac_contraparte/hmac_wamid
// (T-091), and that is exactly the shape this migration needs to accept.
//
// APPLIES ALL THE PREVIOUS MIGRATIONS FOR REAL (up to T-094's, WITHOUT
// including it — found BY NAME, see migrationIndex), instead of hand-
// writing the DDL like the others in this block: a migration that has
// already shipped never gets edited (see the "Schema and migrations"
// header), so reusing the previous migrations' code to build the "v0.32.0
// database" is safe — they are frozen history, they won't change under
// this test.
func TestTheTransitMigrationCounterpartyAndWamidInTheClearReplaceTheHMAC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "antes-t094.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("abrir banco antigo: %v", err)
	}
	previous := migrations[:migrationIndex(t, "transito.contraparte-e-wamid-em-claro")]
	if err := migrate(context.Background(), db, previous); err != nil {
		t.Fatalf("aplicar migracoes anteriores a T-094: %v", err)
	}
	// A REAL row from v0.32.0: HMAC in the two old columns, as T-091 wrote them.
	if _, err := db.Exec(`
		INSERT INTO transito (slug, direcao, hmac_contraparte, hmac_wamid, tipo, correlacao, carimbo, desfecho)
		VALUES ('lojinha','entrada','hash-do-telefone','hash-do-wamid','mensagem','c-de-producao',1769000000,'consumidor guardou (200)')`); err != nil {
		t.Fatalf("inserir linha de v0.32.0: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("fechar banco antigo: %v", err)
	}

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore sobre banco de v0.32.0: %v", err)
	}

	cols := columns(t, s, "transito")
	if cols["hmac_contraparte"] || cols["hmac_wamid"] {
		t.Fatalf("as colunas HMAC continuam no esquema depois da migracao: %+v", cols)
	}
	if !cols["contraparte"] || !cols["wamid"] {
		t.Fatalf("as colunas em claro nao chegaram: %+v", cols)
	}

	// The OLD ROW survives, but contraparte/wamid stay EMPTY FOREVER —
	// HMAC is one-way, there is no way to recover the phone number.
	var counterparty, wamid, outcome string
	if err := s.DB().QueryRow(`SELECT contraparte, wamid, desfecho FROM transito WHERE slug = 'lojinha'`).
		Scan(&counterparty, &wamid, &outcome); err != nil {
		t.Fatalf("ler a linha antiga depois da migracao: %v", err)
	}
	if counterparty != "" || wamid != "" {
		t.Fatalf("contraparte=%q wamid=%q — HMAC e' de mao unica, a linha antiga tem de ficar VAZIA, nao inventada",
			counterparty, wamid)
	}
	if outcome != "consumidor guardou (200)" {
		t.Fatalf("desfecho = %q — a migracao nao pode perder o resto da linha antiga", outcome)
	}

	// And the database stays WRITABLE and SEARCHABLE after the migration: a
	// NEW row writes the counterpart in the clear and the search by the
	// last eight digits finds it.
	if err := s.WriteTransit(TransitRecord{
		Slug: "lojinha", Direction: DirectionInbound,
		Counterparty: "5511999990000", Type: "mensagem", Correlation: "c-novo",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit depois da migracao: %v", err)
	}
	found, err := s.SearchTransit("lojinha", "99990000", time.Time{})
	if err != nil {
		t.Fatalf("SearchTransit depois da migracao: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("achadas = %d, quero 1 — a linha nova nao foi achada pelos ultimos 8 digitos", len(found))
	}
}

// --- T-131: the race guards that only existed in comments -------------
//
// The three prove it by EFFECT, following the mold of
// TestIdempotencyUnderConcurrencyOnlyGivesTheThreeOutcomes (above) and
// TestCounterRecordUnderConcurrencyAddsUpRight (counter_test.go):
// goroutines contending, and at the end an INVARIANT is checked — never a
// specific expected sequence, because the real order of two goroutines is
// never guaranteed.

// retryOnBusy calls f() again ONLY when the error is the TRANSIENT
// contention SQLite returns under WAL when a transaction that read and is
// about to write loses the race to ANOTHER writer midway
// (SQLITE_BUSY / SQLITE_BUSY_SNAPSHOT — "database is locked"). That is NOT
// what this section's guards prove: it's the driver saying "my own
// transaction can no longer proceed, try again from scratch" — the same
// thing a real consumer would do by hitting the command again. A DOMAIN
// error (ErrInstanceActive, ErrRegistrationWindowClosed,
// ErrInstanceNotFound etc.) never matches the `strings.Contains`
// below and comes back right away, without a retry — exactly the outcome
// each test's invariant examines.
//
// 🔴 THIS HELPER HAS ALREADY FAILED, and the two causes became T-136. Until
// 2026-08-19 it (a) matched the error by TEXT and (b) retried WITH NO WAIT
// AT ALL. Measured: TestLimparTransitoPorTelefoneSobConcorrencia... failed
// 3 in 12 isolated runs — 25%. Fifty attempts in a tight loop burn through
// in microseconds while 20 goroutines write: they all lose, and the
// "retry" never gets to be a retry.
//
// The fix has two halves, and the second is the one that was missing:
//
//	match by CODE — the driver's error text changes between versions, and
//	matching by text is the trap this project already documents elsewhere;
//	WAIT between attempts, with backoff and jitter — without a wait there's
//	nothing for the contention to pass, and without jitter the goroutines
//	resynchronize and collide again all together.
//
// ⚠️ THIS RETRY BELONGS TO THE TEST AND DOES NOT EXIST IN PRODUCTION. T-133
// evaluated putting it in production and REFUSED, with the reason written
// in docs/ARMADILHAS.md. Here it's legitimate because the test doesn't
// exist to prove the absence of contention: it exists to prove the
// INVARIANT (the count matches what disappeared). Don't copy this pattern
// into production code without reading that decision.
func retryOnBusy(t *testing.T, f func() error) error {
	t.Helper()
	wait := 200 * time.Microsecond
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		err = f()
		if err == nil || !sqliteLockError(err) {
			return err
		}
		// Jitter of up to 50%: without it, goroutines that backed off
		// together come back together and collide again at the same instant.
		//
		// THE CEILING DOESN'T CHANGE THE COST, and this was measured rather
		// than assumed: 20ms gave 11.9s per round, 5ms gave 11.9s. Spinning
		// without sleeping before backing off was also TRIED and didn't pay
		// off (11.7s) — left out, because complexity that doesn't show up
		// in the measurement is complexity for nothing. The ~12s are
		// INHERENT to this test's contention (30 rounds x 21 goroutines);
		// whoever wants to shorten it should touch the race's size, not the backoff.
		time.Sleep(wait + time.Duration(mrand.Int63n(int64(wait/2)+1)))
		if wait < 5*time.Millisecond {
			wait *= 2
		}
	}
	return err
}

// sqliteLockError tells whether the error is SQLite write contention,
// MATCHING BY CODE — never by text.
//
// 5 = SQLITE_BUSY. 517 = SQLITE_BUSY_SNAPSHOT, the specific case of a
// transaction that had already READ and lost the race to become a writer;
// busy_timeout does NOT cover that path (docs/ARMADILHAS.md, section
// SQLite). Both are the same family for retry purposes: whoever lost has
// to restart the whole transaction.
func sqliteLockError(err error) bool {
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Code() == 5 || e.Code() == 517
}

// TestRemoveInstanceUnderConcurrencyNeverDeletesAnInstanceThatWasActive proves
// the comment on RemoveInstance (above, "The check is done INSIDE the
// transaction, otherwise between 'is it paused?' and the DELETE a `fumaca`
// from another session could fit in and activate the instance"): the
// instance starts PAUSED, and ActivateInstance and RemoveInstance contend
// over the same instance at the same time.
//
// THE INVARIANT: the two can never both succeed (err == nil) IN THE SAME
// round. If ActivateInstance won, the instance became active and
// RemoveInstance has to return ErrInstanceActive (the check, done inside
// the SAME transaction as the DELETE, has to see the `ativo = 1` that was
// just committed). If RemoveInstance won (deleted an instance that was
// really PAUSED at the moment of the DELETE), the following ActivateInstance
// no longer finds the row and has to return ErrInstanceNotFound. Both
// succeeding together is only possible if the DELETE ran after the
// instance was already active — exactly the deletion of an active instance
// the guard exists to prevent.
func TestRemoveInstanceUnderConcurrencyNeverDeletesAnInstanceThatWasActive(t *testing.T) {
	s := testStore(t)

	const rounds = 300
	for round := 0; round < rounds; round++ {
		slug := fmt.Sprintf("corrida-remocao-%d", round)
		if err := s.CreateInstance(testInstanceWithSlug(slug)); err != nil {
			t.Fatalf("rodada %d: CreateInstance: %v", round, err)
		}
		// Born PAUSED (testInstanceWithSlug doesn't activate it) — it's
		// the comment's scenario: the "is it paused?" check needs to
		// survive a concurrent activation.

		var wg sync.WaitGroup
		var errActivate, errRemove error
		wg.Add(2)
		go func() {
			defer wg.Done()
			errActivate = retryOnBusy(t, func() error { return s.ActivateInstance(slug) })
		}()
		go func() {
			defer wg.Done()
			errRemove = retryOnBusy(t, func() error {
				_, err := s.RemoveInstance(slug)
				return err
			})
		}()
		wg.Wait()

		if errActivate == nil && errRemove == nil {
			t.Fatalf("rodada %d: ActivateInstance e RemoveInstance sucederam OS DOIS — "+
				"a instancia foi apagada tendo estado ativa no meio do caminho", round)
		}
	}
}

// TestRegisterMetaUnderConcurrencyAtTheWindowBoundaryNeverWritesOutsideTheWinningWindow
// proves the comment on RegisterMeta (above, "between a loose 'is it still
// open?' and the UPDATE, another request from the same consumer could fit
// in, and both would write the first insert with different instants"): N
// concurrent calls contend to be the FIRST insert on the same instance,
// half with an early `agora` and half with an `agora` ten days later — that
// is, half of them can only be legitimate if the OTHER half never "won" the
// race for the first insert.
//
// THE INVARIANT (what the comment promises, in practice): once the dust
// settles, there is a SINGLE winning window written to cadastro_em — each
// call's read and write have to have happened atomically, inside the SAME
// transaction, otherwise a call could read an already-SUPERSEDED
// cadastro_em (from before the winner committed) and write over it with
// ITS OWN `agora`, which may be outside the window that actually won.
// Therefore: EVERY call that succeeds (writes) has to have its `agora`
// INSIDE the window that ended up written — never outside it.
func TestRegisterMetaUnderConcurrencyAtTheWindowBoundaryNeverWritesOutsideTheWinningWindow(t *testing.T) {
	s := testStore(t)

	const rounds = 40
	const goroutinesPerRound = 10
	t0 := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)

	for round := 0; round < rounds; round++ {
		slug := fmt.Sprintf("corrida-cadastro-%d", round)
		if err := s.CreateInstance(instanceWithOnlyTheSlug(slug)); err != nil {
			t.Fatalf("rodada %d: CreateInstance: %v", round, err)
		}

		nowOf := make([]time.Time, goroutinesPerRound)
		for i := range nowOf {
			// Half act as if they were the "real" first insert (t0), half
			// as if arriving ten days later — the two can only succeed
			// together if each respects the window that actually won the race.
			if i%2 == 0 {
				nowOf[i] = t0
			} else {
				nowOf[i] = t0.Add(10 * 24 * time.Hour)
			}
		}

		var wg sync.WaitGroup
		success := make([]bool, goroutinesPerRound)
		wg.Add(goroutinesPerRound)
		for i := 0; i < goroutinesPerRound; i++ {
			go func(n int) {
				defer wg.Done()
				err := retryOnBusy(t, func() error {
					_, err := s.RegisterMeta(slug, testRegistration(), nowOf[n])
					return err
				})
				success[n] = err == nil
			}(i)
		}
		wg.Wait()

		r, err := s.SummarizeInstance(slug)
		if err != nil {
			t.Fatalf("rodada %d: SummarizeInstance: %v", round, err)
		}
		if r.RegisteredAt == "" {
			t.Fatalf("rodada %d: nenhuma chamada abriu a janela — pelo menos uma tinha de suceder", round)
		}
		winner := WindowFrom(r.RegisteredAt)

		for i := 0; i < goroutinesPerRound; i++ {
			if !success[i] {
				continue
			}
			if !winner.IsOpen(nowOf[i]) {
				t.Fatalf("rodada %d: goroutine %d gravou com agora=%s, fora da janela vencedora [%s, %s) — "+
					"a janela fechou e mesmo assim uma chamada gravou",
					round, i, nowOf[i], winner.OpenedAt, winner.ClosesAt)
			}
		}
	}
}
