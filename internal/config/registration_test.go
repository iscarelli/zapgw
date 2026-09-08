package config

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- T-079: the CONSUMER registers THEIR OWN Meta setup, and the 24h window -------------
//
// THE DIRECTION IS CONSUMER -> GATEWAY, AND IT'S A WRITE. No test here asks
// the gateway what the configuration is: what's proven is that it
// RECEIVES, encrypts and writes — and that nothing comes back out.

// instanceWithOnlyTheSlug is the instance as it's born in T-079's model: the
// owner supplies the slug and nothing else, because `waba_id`,
// `phone_number_id`, the number and the App secrets are the CONSUMER's data.
func instanceWithOnlyTheSlug(slug string) Instance {
	return Instance{Slug: slug, TimeoutMs: 5000}
}

func testRegistration() MetaRegistration {
	return MetaRegistration{
		WabaID:        "WABA-DO-CONSUMIDOR",
		PhoneNumberID: "PNID-DO-CONSUMIDOR",
		DisplayNumber: "5511999990000",
		AppSecret:     "app-secret-do-consumidor",
		SendToken:     "token-envio-do-consumidor",
		CallbackURL:   "https://painel.do-consumidor.example/webhooks/zapgw",
	}
}

// registeredByName becomes {"app_secret": true, ...} by asking the STORE
// which columns are encrypted — never by a hand-written list here.
func registeredByName(t *testing.T, r InstanceSummary) map[string]bool {
	t.Helper()
	m := map[string]bool{}
	for _, c := range r.Encrypted {
		m[c.Name] = c.Registered
	}
	return m
}

// An instance is born WITH JUST THE SLUG — and it's born INCOMPLETE on purpose.
//
// Until T-079 this was refused (T-074 required waba_id and phone_number_id
// at creation). What changed isn't the rigor: it's WHO has the values. See
// ValidateIdentification.
func TestCreateInstanceWithOnlyTheSlugWorksAndIsBornIncompleteAndPaused(t *testing.T) {
	s := testStore(t)

	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance with just the slug: %v — the owner does not have the consumer's Meta data", err)
	}

	r, err := s.SummarizeInstance("terceiro")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if r.WabaID != "" || r.PhoneNumberID != "" {
		t.Errorf("the instance was born with identification (waba=%q pnid=%q) — nobody supplied it", r.WabaID, r.PhoneNumberID)
	}
	if r.Active {
		t.Error("the instance was born ACTIVE — only the smoke test activates")
	}
	// The fields the CONSUMER registers are born unregistered, and the
	// read has to say so without decrypting anything.
	reg := registeredByName(t, r)
	for _, field := range []string{"app_secret", "token_envio", "callback_url"} {
		if reg[field] {
			t.Errorf("%s shows up as REGISTERED on an instance that only has a slug", field)
		}
	}
	// And the slug is still validated: it belongs to the owner and becomes a URL path.
	if err := s.CreateInstance(instanceWithOnlyTheSlug("Slug Invalido")); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("CreateInstance with a malformed slug = %v, want ErrInvalidSlug", err)
	}
}

// The round trip through encryption, on the REGISTRATION side: what the
// consumer sends has to reach sending in the clear, and the FILE encrypted.
func TestRegisterMetaWritesENCRYPTEDAndReturnsInTheClear(t *testing.T) {
	vault, err := NewVault(testKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "teste.db")
	s, err := OpenStore(path, vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if _, err := s.RegisterMeta("terceiro", testRegistration(), time.Now()); err != nil {
		t.Fatalf("RegisterMeta: %v", err)
	}

	i, err := s.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	want := map[string]string{
		"WabaID":        "WABA-DO-CONSUMIDOR",
		"PhoneNumberID": "PNID-DO-CONSUMIDOR",
		"DisplayNumber": "5511999990000",
		"AppSecret":     "app-secret-do-consumidor",
		"SendToken":     "token-envio-do-consumidor",
		"CallbackURL":   "https://painel.do-consumidor.example/webhooks/zapgw",
	}
	has := map[string]string{
		"WabaID": i.WabaID, "PhoneNumberID": i.PhoneNumberID, "DisplayNumber": i.DisplayNumber,
		"AppSecret": i.AppSecret, "SendToken": i.SendToken, "CallbackURL": i.CallbackURL,
	}
	for field, expected := range want {
		if has[field] != expected {
			t.Errorf("%s = %q, want %q", field, has[field], expected)
		}
	}
	// Registration does NOT write to verify_token or to segredo_entrega:
	// neither is in MetaRegistration. This instance was born without them
	// (just the slug), so what's proven here is that registration did NOT
	// INVENT any value — and that a consumer field didn't leak into a
	// neighboring column.
	if i.VerifyToken != "" || i.DeliverySecret != "" {
		t.Errorf("registration wrote to fields that aren't its own: verify_token=%q segredo_entrega=%q", i.VerifyToken, i.DeliverySecret)
	}
	_ = s.Close()

	// The database file goes into the nightly backup: none of this can be readable.
	raw := readFile(t, path)
	for _, secret := range []string{
		"app-secret-do-consumidor", "token-envio-do-consumidor",
		"https://painel.do-consumidor.example/webhooks/zapgw",
	} {
		if containsBytes(raw, secret) {
			t.Errorf("value %q shows up IN THE CLEAR in the database file", secret)
		}
	}
}

// 🔴 REGISTERING DOESN'T ACTIVATE. Registering proves nothing; SENDING proves.
//
// If registration activated it, a wrong credential would turn into an
// "active" instance that refuses everything — exactly the defect T-074
// found. And the guarantee is STRUCTURAL: the `ativo` column doesn't
// appear in RegisterMeta's UPDATE.
func TestRegisterMetaDoesNOTActivateTheInstance(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if _, err := s.RegisterMeta("terceiro", testRegistration(), time.Now()); err != nil {
		t.Fatalf("RegisterMeta: %v", err)
	}

	i, err := s.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Active {
		t.Fatal("the instance became ACTIVE after registration — registering proves nothing, only `zapgw fumaca` activates")
	}

	// And it also doesn't DEACTIVATE an instance that was already running:
	// a consumer rotating their own credential cannot bring their own
	// channel down as a side effect.
	if err := s.ActivateInstance("terceiro"); err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}
	if _, err := s.RegisterMeta("terceiro", testRegistration(), time.Now()); err != nil {
		t.Fatalf("RegisterMeta (re-registration): %v", err)
	}
	i, err = s.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if !i.Active {
		t.Fatal("the re-registration PAUSED an active instance — it does not write to the `ativo` column, in any direction")
	}
}

// THE WINDOW COUNTS FROM THE CONSUMER'S FIRST INSERT, NOT FROM THE
// INSTANCE'S CREATION. "I create the instance today, in 5 days the
// consumer inserts something, the count starts there."
//
// Without this, a slow consumer would lose the window BEFORE starting —
// and their first contact with the gateway would be an error they didn't cause.
func TestTheWindowOpensOnTheConsumersFIRSTInsertAndNotAtCreation(t *testing.T) {
	s := testStore(t)
	creation := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	if err := s.CreateInstanceAt(instanceWithOnlyTheSlug("terceiro"), creation); err != nil {
		t.Fatalf("CreateInstanceAt: %v", err)
	}

	// FIVE DAYS later — far past 24h, if they counted from creation.
	first := creation.Add(5 * 24 * time.Hour)
	window, err := s.RegisterMeta("terceiro", testRegistration(), first)
	if err != nil {
		t.Fatalf("RegisterMeta 5 days after creation: %v — the window is counting from CREATION", err)
	}
	if !window.OpenedAt.Equal(first) {
		t.Errorf("window.OpenedAt = %s, want %s (the consumer's first insert)", window.OpenedAt, first)
	}
	if want := first.Add(RegistrationWindow); !window.ClosesAt.Equal(want) {
		t.Errorf("window.ClosesAt = %s, want %s", window.ClosesAt, want)
	}
}

// AND THE WINDOW DOES NOT RESTART ON EVERY CHANGE. If it restarted,
// whoever touched it every day would keep it open forever, and the rule
// would become decorative.
//
// THREE INSTANTS ARE NEEDED to prove it: without the middle registration,
// "doesn't restart" is indistinguishable from "restarts" — both would
// refuse the one at 25h.
func TestTheWindowDoesNotRESTARTOnEveryRegistration(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	t0 := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)

	if _, err := s.RegisterMeta("terceiro", testRegistration(), t0); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	// 23h later: inside the window, and it's THIS ONE that would make the
	// clock restart if someone swapped `janela.OpenedAt` for `agora` in the UPDATE.
	secondOne := testRegistration()
	secondOne.DisplayNumber = "5511888880000"
	window, err := s.RegisterMeta("terceiro", secondOne, t0.Add(23*time.Hour))
	if err != nil {
		t.Fatalf("registration 23h later: %v — the window closed before 24h", err)
	}
	if !window.OpenedAt.Equal(t0) {
		t.Errorf("window.OpenedAt = %s after the second registration, want %s — the FIRST insert cannot move", window.OpenedAt, t0)
	}

	// 25h after the FIRST insert: closed. If the window had restarted on
	// the 23h registration, this one would pass.
	third := testRegistration()
	third.DisplayNumber = "5511777770000"
	if _, err := s.RegisterMeta("terceiro", third, t0.Add(25*time.Hour)); !errors.Is(err, ErrRegistrationWindowClosed) {
		t.Fatalf("registration 25h after the first insert = %v, want ErrRegistrationWindowClosed — the window RESTARTED on the change", err)
	}
}

// A closed window WRITES NOTHING — and the error says WHY and WHAT TO DO,
// because with no channel to ask, the error message IS the support.
func TestRegisterMetaAfterTheWindowWritesNOTHING(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	t0 := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	if _, err := s.RegisterMeta("terceiro", testRegistration(), t0); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	late := testRegistration()
	late.PhoneNumberID = "PNID-DE-OUTRA-CONTA"
	_, err := s.RegisterMeta("terceiro", late, t0.Add(RegistrationWindow+time.Second))
	if !errors.Is(err, ErrRegistrationWindowClosed) {
		t.Fatalf("err = %v, want ErrRegistrationWindowClosed", err)
	}
	// The error says WHEN the window opened and WHEN it closed: without
	// both instants, "closed" is a claim the consumer has no way to check.
	if !strings.Contains(err.Error(), t0.Format(time.RFC3339)) {
		t.Errorf("the error does not say when the window opened: %q", err.Error())
	}

	r, err := s.SummarizeInstance("terceiro")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if r.PhoneNumberID != "PNID-DO-CONSUMIDOR" {
		t.Errorf("phone_number_id = %q — the refused registration WROTE anyway", r.PhoneNumberID)
	}
}

// The rule holds in the SAME terms as creation: a clear-text callback, a
// CA bundle with no certificate, and an empty mandatory field are refused
// here too.
//
// WHY THE TEST EXISTS even though it's the SAME validation function: what's
// proven isn't the function, it's that it's WIRED UP on this path. This
// project's mother-of-all-traps is "the rule holds in one place and not in
// the next", and a registration that accepted `http://` would make the raw
// body's delivery cross the network readable — exactly what creation closes off.
func TestRegisterMetaRefusesWhatCreationAlsoRefuses(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*MetaRegistration)
		wantErr error
	}{
		{"external http callback", func(c *MetaRegistration) { c.CallbackURL = "http://painel.do-consumidor.example/hook" }, ErrInsecureCallback},
		{"bundle with no certificate", func(c *MetaRegistration) { c.CABundle = "this is not a PEM" }, ErrInvalidCABundle},
		{"no app_secret", func(c *MetaRegistration) { c.AppSecret = "" }, ErrIncompleteRegistration},
		{"no token_envio", func(c *MetaRegistration) { c.SendToken = "   " }, ErrIncompleteRegistration},
		{"no numero_exibido", func(c *MetaRegistration) { c.DisplayNumber = "" }, ErrIncompleteRegistration},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t)
			if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
				t.Fatalf("CreateInstance: %v", err)
			}
			reg := testRegistration()
			c.mutate(&reg)

			_, err := s.RegisterMeta("terceiro", reg, time.Now())

			if !errors.Is(err, c.wantErr) {
				t.Fatalf("RegisterMeta = %v, want %v", err, c.wantErr)
			}
			// THE VALUE CANNOT APPEAR IN THE ERROR: it goes to the gateway's
			// log and to the HTTP response, and two of these fields are secret.
			for _, value := range []string{"app-secret-do-consumidor", "token-envio-do-consumidor", "painel.do-consumidor.example"} {
				if strings.Contains(err.Error(), value) {
					t.Errorf("the error carries a value from the registration (%q): %q", value, err.Error())
				}
			}
			r, err := s.SummarizeInstance("terceiro")
			if err != nil {
				t.Fatalf("SummarizeInstance: %v", err)
			}
			if r.RegisteredAt != "" {
				t.Errorf("the window OPENED on a refused registration (cadastro_em = %q) — an invalid request would spend the 24h", r.RegisteredAt)
			}
		})
	}
}

// RE-REGISTERING IS THE ROTATION PATH ON THE CONSUMER'S SIDE: it REPLACES.
func TestRegisterMetaAcceptsReRegistrationByOverwriting(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	t0 := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	if _, err := s.RegisterMeta("terceiro", testRegistration(), t0); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	newValue := testRegistration()
	newValue.AppSecret = "app-secret-ROTACIONADO"
	newValue.SendToken = "token-envio-ROTACIONADO"
	// AN EMPTY CALLBACK on re-registration means an OUTBOUND-ONLY instance
	// — registration replaces, and an omitted field counts as empty.
	newValue.CallbackURL = ""
	if _, err := s.RegisterMeta("terceiro", newValue, t0.Add(time.Hour)); err != nil {
		t.Fatalf("re-registration: %v", err)
	}

	i, err := s.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.AppSecret != "app-secret-ROTACIONADO" || i.SendToken != "token-envio-ROTACIONADO" {
		t.Errorf("the re-registration did not overwrite the secrets: app_secret=%q token_envio=%q", i.AppSecret, i.SendToken)
	}
	if i.CallbackURL != "" {
		t.Errorf("callback_url = %q, want empty — registration REPLACES", i.CallbackURL)
	}
}

// REOPENING gives back the deadline and DOES NOT TOUCH the configuration:
// whoever registers stays the consumer. And the clock doesn't start on the
// reopening — it starts on their next insert, otherwise the window would
// consume itself while the owner and the consumer were still exchanging messages.
func TestReopeningTheWindowReturnsTheDeadlineWithoutTouchingTheConfiguration(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	t0 := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	if _, err := s.RegisterMeta("terceiro", testRegistration(), t0); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	afterTheWindow := t0.Add(30 * 24 * time.Hour)
	if _, err := s.RegisterMeta("terceiro", testRegistration(), afterTheWindow); !errors.Is(err, ErrRegistrationWindowClosed) {
		t.Fatalf("err = %v, want ErrRegistrationWindowClosed", err)
	}

	if err := s.ReopenRegistrationWindow("terceiro"); err != nil {
		t.Fatalf("ReopenRegistrationWindow: %v", err)
	}

	// THE CONFIGURATION STAYS THE SAME: reopening is an act on the DEADLINE.
	i, err := s.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.AppSecret != "app-secret-do-consumidor" || i.PhoneNumberID != "PNID-DO-CONSUMIDOR" {
		t.Errorf("reopening touched the configuration: app_secret=%q pnid=%q", i.AppSecret, i.PhoneNumberID)
	}

	// AND THE CLOCK ONLY STARTS WHEN THEY WRITE AGAIN — two days after the
	// reopening, registration still goes through.
	twoDaysLater := afterTheWindow.Add(48 * time.Hour)
	window, err := s.RegisterMeta("terceiro", testRegistration(), twoDaysLater)
	if err != nil {
		t.Fatalf("registration after reopening: %v — the reopening gave a deadline counted from ITSELF, not from the next insert", err)
	}
	if !window.OpenedAt.Equal(twoDaysLater) {
		t.Errorf("window.OpenedAt = %s, want %s", window.OpenedAt, twoDaysLater)
	}
	// And the new window closes 24h AFTER that insert, not before.
	if _, err := s.RegisterMeta("terceiro", testRegistration(), twoDaysLater.Add(2*time.Hour)); err != nil {
		t.Errorf("registration 2h after reopening and re-inserting: %v", err)
	}
}

// Reopening a slug that doesn't exist HAS to flag it: a silent success
// would send the owner away thinking they unlocked the consumer, who would stay locked out.
func TestReopeningTheWindowFlagsANonexistentSlug(t *testing.T) {
	s := testStore(t)
	if err := s.ReopenRegistrationWindow("nao-existe"); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("err = %v, want ErrInstanceNotFound", err)
	}
}

// AN UNREADABLE STAMP COUNTS AS CLOSED. The two possible wrong readings
// don't cost the same: treating it as "never inserted" would reopen the
// window on its own — precisely on the row someone edited by hand — and
// the rule would become decorative. Treating it as closed costs a
// message, and there is a command to unlock it.
func TestAWindowFromAnUnreadableStampCountsAsCLOSED(t *testing.T) {
	s := testStore(t)
	if err := s.CreateInstance(instanceWithOnlyTheSlug("terceiro")); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE instancia SET cadastro_em = 'yesterday afternoon' WHERE slug = 'terceiro'`); err != nil {
		t.Fatalf("corrupt the stamp: %v", err)
	}

	if _, err := s.RegisterMeta("terceiro", testRegistration(), time.Now()); !errors.Is(err, ErrRegistrationWindowClosed) {
		t.Fatalf("err = %v, want ErrRegistrationWindowClosed", err)
	}
}

// THE `CREATE TABLE IF NOT EXISTS` TRAP: a new column doesn't reach a
// database that already exists, and the IF NOT EXISTS hides that. Here the
// column is `cadastro_em`, and the symptom would be EVERY instance read
// failing with "no such column" after the update — the whole gateway
// would die on the first request.
func TestTheMigrationTakesRegisteredAtToThePreExistingDatabase(t *testing.T) {
	path := priorDatabase(t, schemaWithRequestHash, 2)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old database: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO instancia (slug, waba_id, phone_number_id, numero_exibido,
		    app_secret, verify_token, token_envio, callback_url, segredo_entrega,
		    timeout_ms, ativo)
		VALUES ('tenant-one','WABA1','PNID1','5532999990000','x','x','x','x','x',5000,1)`); err != nil {
		t.Fatalf("insert old instance: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old database: %v", err)
	}

	s, err := openAt(t, path)
	if err != nil {
		t.Fatalf("OpenStore on old database: %v", err)
	}
	if !columns(t, s, "instancia")["cadastro_em"] {
		t.Fatal("column cadastro_em did NOT reach the pre-existing database")
	}

	r, err := s.SummarizeInstance("tenant-one")
	if err != nil {
		t.Fatalf("SummarizeInstance after the migration: %v", err)
	}
	// EMPTY IS THE CORRECT ANSWER for the pre-existing instance: the
	// consumer never inserted anything on it (the owner typed everything
	// at creation), so its clock starts the first time they write. Unlike
	// carimbos_desde, here the absence is a real state and not a hole.
	if r.RegisteredAt != "" {
		t.Errorf("cadastro_em = %q on a pre-existing instance, want empty — the migration invented a first insert that never happened", r.RegisteredAt)
	}
	if !WindowFrom(r.RegisteredAt).IsOpen(time.Now()) {
		t.Error("the pre-existing instance's window was born CLOSED — it would never have a way to be registered by the consumer")
	}
}
