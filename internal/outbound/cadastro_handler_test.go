package outbound

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// --- POST /v1/cadastro: the CONSUMER writing THEIR Meta (T-079) --------------
//
// THE DIRECTION IS CONSUMER -> GATEWAY. No test here asks for configuration
// back: what is proven is that the gateway RECEIVES, stores encrypted, and
// returns nothing of what it received.

// testRegistration sets up the store in the state T-079 leaves a new instance
// in: created WITH ONLY THE SLUG, paused, and with a consumer linked.
//
// `de-outro` exists for the 403: without a second instance in the database,
// "someone else's instance gets 403" is indistinguishable from "nonexistent
// instance gets 403".
func testRegistration(t *testing.T) (http.Handler, *config.Store, func() time.Time, *time.Time) {
	t.Helper()
	vault, err := config.NewVault(testCipherKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	store, err := config.OpenStore(filepath.Join(t.TempDir(), "t.db"), vault)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, slug := range []string{"terceiro", "de-outro"} {
		// ONLY THE SLUG, and the two SHARED secrets the owner draws at
		// creation (T-052) — they do not pass through this route, and they
		// are here so the test can also prove they do not COME OUT through it.
		if err := store.CreateInstance(config.Instance{
			Slug:           slug,
			VerifyToken:    testEncryptedValue["verify_token"],
			DeliverySecret: testEncryptedValue["segredo_entrega"],
			TimeoutMs:      2000,
		}); err != nil {
			t.Fatalf("CreateInstance %q: %v", slug, err)
		}
	}
	if err := store.CreateConsumer("sistema-a", "token-do-a", []string{"terceiro"}); err != nil {
		t.Fatalf("CreateConsumer: %v", err)
	}

	clock := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	h := newRegistrationHandlerAt(store, NewAuthenticator(store), 1<<20, func() time.Time { return now() }, WhatsAppOnly)
	return h, store, now, &clock
}

// testEncryptedValue gives ONE distinct value per encrypted column — and
// that is what makes the "nothing encrypted comes out of here" assertion
// able to NAME the leaked field.
//
// THE LIST IS WRITTEN BY HAND ON PURPOSE, and it has a guard: the test asks
// the STORE which columns are encrypted and fails naming whichever one is
// not here. This is the T-048 lesson applied to a map instead of a table —
// every hand-written list over the schema needs something that asks the
// schema.
// THE VALUES SHARE NO PREFIX, and that is what makes the failure NAME a
// single field: with a "SENTINELA-" common to all of them, a leak truncated
// to the first 10 characters would flag all four at once and whoever went to
// fix it would not know which branch produced the leak (measured: the first
// version of this test did exactly that).
var testEncryptedValue = map[string]string{
	"app_secret":      "as3f1c9d-sentinela-do-app-secret",
	"token_envio":     "te8b2e77-sentinela-do-token-envio",
	"callback_url":    "https://cb2d9a41-sentinela.example/hook",
	"segredo_entrega": "se55ac10-sentinela-da-entrega",
	"verify_token":    "vt9911fe-sentinela-do-verify",
	// bundle_ca is filled in at test time (it needs to be a real PEM,
	// otherwise validation refuses it before it gets anywhere near the response).
	"bundle_ca": "",
}

// testCA returns the PEM of a self-signed CA certificate.
func testCA(t *testing.T, name string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	mold := x509.Certificate{
		SerialNumber:          big.NewInt(7),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &mold, &mold, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("criar certificado: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
}

func registrationBody(slug string, fields map[string]string) string {
	body := map[string]string{
		"instancia":       slug,
		"waba_id":         "WABA-DO-CONSUMIDOR",
		"phone_number_id": "PNID-DO-CONSUMIDOR",
		"numero_exibido":  "5511999990000",
		"app_secret":      testEncryptedValue["app_secret"],
		"token_envio":     testEncryptedValue["token_envio"],
		"callback_url":    testEncryptedValue["callback_url"],
	}
	for k, v := range fields {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func register(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/cadastro", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- The happy path --------------------------------------------------------------

func TestRegistrationWritesTheConsumerMetaAndSaysWhichFieldsWereRegistered(t *testing.T) {
	h, store, _, _ := testRegistration(t)

	rec := register(t, h, "token-do-a", registrationBody("terceiro", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200. corpo: %s", rec.Code, rec.Body.String())
	}
	var resp RegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta nao e JSON: %v (%s)", err, rec.Body.String())
	}

	// What was STORED, read from the database in the clear: it is the only
	// proof that the write arrived whole and in the right fields.
	i, err := store.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.WabaID != "WABA-DO-CONSUMIDOR" || i.PhoneNumberID != "PNID-DO-CONSUMIDOR" {
		t.Errorf("identificacao nao chegou: waba=%q pnid=%q", i.WabaID, i.PhoneNumberID)
	}
	if i.AppSecret != testEncryptedValue["app_secret"] || i.SendToken != testEncryptedValue["token_envio"] {
		t.Errorf("credenciais nao chegaram: app_secret=%q token_envio=%q", i.AppSecret, i.SendToken)
	}

	// The response says WHETHER each field is registered, and the list comes
	// from the STORE.
	registered := map[string]bool{}
	for _, c := range resp.Encrypted {
		registered[c.Field] = c.Registered
	}
	for _, field := range []string{"app_secret", "token_envio", "callback_url"} {
		if !registered[field] {
			t.Errorf("a resposta diz que %s NAO esta cadastrado, e ele acabou de ser gravado", field)
		}
	}
	if registered["bundle_ca"] {
		t.Error("bundle_ca aparece como cadastrado, e nenhum foi mandado")
	}
	if !resp.RegistrationWindow.Open {
		t.Error("a janela saiu FECHADA no primeiro cadastro")
	}
}

// 🔴 THE MANDATORY MUTATION OF T-079: making the route return any encrypted
// field — WHOLE, TRUNCATED, or as a HASH — has to turn this red, NAMING the
// field.
//
// HOW IT FINDS A NEW FIELD WITHOUT ITS OWN LIST: the encrypted columns come
// from config.InstanceSummary.Encrypted, which is where the store declares
// them. If a seventh encrypted column shows up and nobody puts a test value
// for it here, this test fails NAMING the column — instead of passing green
// over a field it does not know to look for (the T-048 lesson).
//
// AND WHY THE SEARCH IS THIS ONE, and not a `strings.Contains(corpo,
// segredo)`: a truncated prefix does NOT contain the secret (it is CONTAINED
// in it), and a hash does not look like the value. The three forms are
// checked against EVERY string in the JSON, at any depth.
func TestRegistrationReturnsNoENCRYPTEDField(t *testing.T) {
	testEncryptedValue["bundle_ca"] = testCA(t, "ca-do-consumidor")
	h, store, _, _ := testRegistration(t)

	rec := register(t, h, "token-do-a", registrationBody("terceiro", map[string]string{
		"bundle_ca": testEncryptedValue["bundle_ca"],
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200. corpo: %s", rec.Code, rec.Body.String())
	}

	// ASKS THE STORE which columns are encrypted.
	r, err := store.SummarizeInstance("terceiro")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if len(r.Encrypted) == 0 {
		t.Fatal("o store nao declarou coluna cifrada nenhuma — este teste nao verificaria nada")
	}

	texts := jsonStrings(t, rec.Body.Bytes())
	for _, c := range r.Encrypted {
		value, has := testEncryptedValue[c.Name]
		if !has || value == "" {
			t.Fatalf("a coluna cifrada %q e NOVA e este teste nao sabe que valor procurar por ela —"+
				" ponha um em testEncryptedValue, senao a assercao 'nada cifrado sai daqui' passa verde sem olhar para ela", c.Name)
		}
		sum := sha256.Sum256([]byte(value))
		hash := hex.EncodeToString(sum[:])
		for path, text := range texts {
			switch {
			case strings.Contains(text, value):
				t.Errorf("o campo cifrado %q saiu INTEIRO na resposta, em %s", c.Name, path)
			// TRUNCATED is a PREFIX or SUFFIX of the value, and not "any
			// chunk": a loose `Contains(valor, texto)` would match a field's
			// NAME against its own test value and flag a leak where there is
			// none. The 6-character cutoff removes short words from the
			// response ("ativa", "sim") without letting a `valor[:8]`
			// through, which is the form a "preview for review" would take.
			case len(text) >= 6 && (strings.HasPrefix(value, text) || strings.HasSuffix(value, text)):
				t.Errorf("o campo cifrado %q saiu TRUNCADO na resposta, em %s: %q", c.Name, path, text)
			case len(text) >= 16 && strings.Contains(value, text):
				t.Errorf("o campo cifrado %q saiu em PEDACO na resposta, em %s: %q", c.Name, path, text)
			case strings.Contains(text, hash) || strings.Contains(text, hash[:16]):
				t.Errorf("o campo cifrado %q saiu em HASH na resposta, em %s", c.Name, path)
			}
		}
	}
}

// jsonStrings returns every string in the document, with the path to it —
// the path is what makes the failure SAY where the leak came out, instead of
// just that it came out.
func jsonStrings(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("resposta nao e JSON: %v", err)
	}
	found := map[string]string{}
	var advance func(prefix string, v any)
	advance = func(prefix string, v any) {
		switch no := v.(type) {
		case string:
			found[prefix] = no
		case map[string]any:
			for k, child := range no {
				advance(prefix+"."+k, child)
			}
		case []any:
			for n, child := range no {
				advance(prefix+"["+strconv.Itoa(n)+"]", child)
			}
		}
	}
	advance("$", doc)
	if len(found) == 0 {
		t.Fatal("a resposta nao tem string nenhuma — este teste nao verificaria nada")
	}
	return found
}

// 🔴 THE SECOND MANDATORY MUTATION: making registration activate the instance
// has to turn this red. Registering proves nothing; SENDING proves.
func TestRegistrationDoesNOTActivateTheInstance(t *testing.T) {
	h, store, _, _ := testRegistration(t)

	rec := register(t, h, "token-do-a", registrationBody("terceiro", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200. corpo: %s", rec.Code, rec.Body.String())
	}

	i, err := store.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.Active {
		t.Fatal("a instancia ficou ATIVA depois do cadastro — so o `zapgw fumaca` ativa")
	}

	// And the RESPONSE has to say so, otherwise the consumer walks away
	// thinking the channel is live and finds out from the 503 of the first send.
	var resp RegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta nao e JSON: %v", err)
	}
	if !resp.Paused || resp.State != "pausada" {
		t.Errorf("a resposta diz estado=%q pausada=%v numa instancia que continua parada", resp.State, resp.Paused)
	}
	if !strings.Contains(strings.ToLower(resp.NextStep), "pausada") {
		t.Errorf("o proximo_passo nao avisa que a instancia continua pausada: %q", resp.NextStep)
	}
}

// --- Access guards -----------------------------------------------------------

func TestRegistrationWithoutTokenIs401AndNeverWritesAnything(t *testing.T) {
	h, store, _, _ := testRegistration(t)

	rec := register(t, h, "", registrationBody("terceiro", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, quero 401", rec.Code)
	}
	r, err := store.SummarizeInstance("terceiro")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if r.PhoneNumberID != "" {
		t.Errorf("gravou sem credencial nenhuma: pnid=%q", r.PhoneNumberID)
	}
}

// The route EXISTS — 401, not 404. A 404 here would send the consumer
// looking for a URL error when the problem is the token, and they have no
// channel to ask.
func TestRegistrationWithoutTokenDoesNotAnswer404(t *testing.T) {
	h, _, _, _ := testRegistration(t)
	rec := register(t, h, "", registrationBody("terceiro", nil))
	if rec.Code == http.StatusNotFound {
		t.Fatal("a rota respondeu 404 sem credencial — ela existe, e o que falta e o token")
	}
}

func TestRegistrationOfFOREIGNInstanceIs403AndDoesNotTouchIt(t *testing.T) {
	h, store, _, _ := testRegistration(t)

	rec := register(t, h, "token-do-a", registrationBody("de-outro", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quero 403. corpo: %s", rec.Code, rec.Body.String())
	}
	r, err := store.SummarizeInstance("de-outro")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if r.PhoneNumberID != "" || r.RegisteredAt != "" {
		t.Errorf("a instancia alheia foi tocada: pnid=%q cadastro_em=%q", r.PhoneNumberID, r.RegisteredAt)
	}
}

// --- The 24h window, via the route ----------------------------------------------

// After the window the POST is refused with an error that says WHY and WHAT
// TO DO — with no channel to ask, the error message IS the support.
func TestRegistrationAfterTheWindowIs409AndSaysWhatToDo(t *testing.T) {
	h, store, _, clock := testRegistration(t)

	if rec := register(t, h, "token-do-a", registrationBody("terceiro", nil)); rec.Code != http.StatusOK {
		t.Fatalf("primeiro cadastro: status %d (%s)", rec.Code, rec.Body.String())
	}
	*clock = clock.Add(config.RegistrationWindow + time.Minute)

	rec := register(t, h, "token-do-a", registrationBody("terceiro", map[string]string{
		"phone_number_id": "PNID-DE-OUTRA-CONTA",
	}))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, quero 409. corpo: %s", rec.Code, rec.Body.String())
	}
	body := strings.ToLower(rec.Body.String())
	// WHY it closed, WHAT TO DO, and the NAME of the command that unlocks it.
	// Without all three, the consumer is stuck in a dead end.
	for _, required := range []string{"fechada", "primeira", "reabrir-cadastro"} {
		if !strings.Contains(body, required) {
			t.Errorf("a mensagem de erro nao diz %q — ela e o unico suporte que este consumidor tem:\n%s", required, rec.Body.String())
		}
	}
	// And NOTHING was stored: the previous configuration still stands.
	r, err := store.SummarizeInstance("terceiro")
	if err != nil {
		t.Fatalf("SummarizeInstance: %v", err)
	}
	if r.PhoneNumberID != "PNID-DO-CONSUMIDOR" {
		t.Errorf("phone_number_id = %q — o cadastro recusado gravou mesmo assim", r.PhoneNumberID)
	}
}

// The window counts from the FIRST insertion, not the instance's creation:
// an instance created 5 days ago accepts the first registration without
// complaint.
func TestRegistrationOpensTheWindowOnFirstInsertNotOnCreation(t *testing.T) {
	h, _, _, clock := testRegistration(t)
	*clock = clock.Add(5 * 24 * time.Hour)

	rec := register(t, h, "token-do-a", registrationBody("terceiro", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 — a janela esta contando da CRIACAO. corpo: %s", rec.Code, rec.Body.String())
	}
	var resp RegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta nao e JSON: %v", err)
	}
	if want := clock.Format(time.RFC3339); resp.RegistrationWindow.FirstInsertAt != want {
		t.Errorf("primeira_insercao_em = %q, quero %q", resp.RegistrationWindow.FirstInsertAt, want)
	}
}

// --- Validation --------------------------------------------------------------------

// The validation error NAMES the field and NEVER carries the value: two of
// this body's fields are secret, and the response also goes to the
// consumer's log.
func TestRegistrationRefusesMissingFieldWithoutEchoingValue(t *testing.T) {
	cases := map[string]struct {
		changes map[string]string
		field   string
	}{
		"sem waba_id":         {map[string]string{"waba_id": ""}, "waba_id"},
		"sem phone_number_id": {map[string]string{"phone_number_id": ""}, "phone_number_id"},
		"sem app_secret":      {map[string]string{"app_secret": ""}, "app_secret"},
		"sem token_envio":     {map[string]string{"token_envio": "  "}, "token_envio"},
		"sem numero_exibido":  {map[string]string{"numero_exibido": ""}, "numero_exibido"},
		"callback em claro":   {map[string]string{"callback_url": "http://painel.example/hook"}, "https"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, store, _, _ := testRegistration(t)

			rec := register(t, h, "token-do-a", registrationBody("terceiro", c.changes))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, quero 400. corpo: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.field) {
				t.Errorf("o erro nao nomeia %q: %s", c.field, rec.Body.String())
			}
			for _, value := range []string{testEncryptedValue["app_secret"], testEncryptedValue["token_envio"]} {
				if strings.Contains(rec.Body.String(), value) {
					t.Errorf("o erro ecoa um segredo do corpo: %s", rec.Body.String())
				}
			}
			// An invalid request does NOT spend the window: whoever gets the
			// body wrong on the first attempt would lose 24h over a typo.
			r, err := store.SummarizeInstance("terceiro")
			if err != nil {
				t.Fatalf("SummarizeInstance: %v", err)
			}
			if r.RegisteredAt != "" {
				t.Errorf("a janela abriu num cadastro recusado: cadastro_em = %q", r.RegisteredAt)
			}
		})
	}
}

func TestRegistrationWithoutInstanceInTheBodyIs400(t *testing.T) {
	h, _, _, _ := testRegistration(t)
	rec := register(t, h, "token-do-a", `{"waba_id":"W"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "instancia") {
		t.Errorf("o erro nao nomeia o campo que falta: %s", rec.Body.String())
	}
}

func TestRegistrationWithNonJSONBodyDoesNotEchoTheBody(t *testing.T) {
	h, _, _, _ := testRegistration(t)
	// The broken body carries a secret: the encoding/json error quotes the
	// chunk that did not match, and that is why it cannot go into the response.
	rec := register(t, h, "token-do-a", `{"instancia":"terceiro","app_secret":"`+testEncryptedValue["app_secret"])
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), testEncryptedValue["app_secret"]) {
		t.Fatalf("a resposta ecoou o corpo, com o segredo dentro: %s", rec.Body.String())
	}
}

// RE-REGISTRATION IS THE ROTATION PATH ON THE CONSUMER'S SIDE — and it replaces.
func TestRegistrationAcceptsREREGISTRATIONOverwriting(t *testing.T) {
	h, store, _, clock := testRegistration(t)

	if rec := register(t, h, "token-do-a", registrationBody("terceiro", nil)); rec.Code != http.StatusOK {
		t.Fatalf("primeiro cadastro: status %d (%s)", rec.Code, rec.Body.String())
	}
	*clock = clock.Add(time.Hour)
	rec := register(t, h, "token-do-a", registrationBody("terceiro", map[string]string{
		"token_envio": "token-envio-ROTACIONADO",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("recadastro: status %d (%s)", rec.Code, rec.Body.String())
	}

	i, err := store.FindInstance("terceiro")
	if err != nil {
		t.Fatalf("FindInstance: %v", err)
	}
	if i.SendToken != "token-envio-ROTACIONADO" {
		t.Errorf("token_envio = %q — o recadastro nao sobrescreveu", i.SendToken)
	}
}
