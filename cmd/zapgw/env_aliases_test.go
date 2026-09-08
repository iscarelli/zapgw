// Tests for T-214 (CAMADA 4): the ZAPGW_* variables and the CLI verbs
// accept both their old (Portuguese) and new (English) spelling, the new
// one wins when both are set, and using the old one is logged once.
package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- databasePath (shared by openStore and `zapgw perdidas`) --------------

func TestDatabasePathAcceptsBothNamesNewWins(t *testing.T) {
	cases := []struct {
		name        string
		vars        map[string]string
		wantPath    string
		wantOldUsed bool
	}{
		{"no variable at all: default", map[string]string{}, "zapgw.db", false},
		{"only the new one", map[string]string{envDatabaseNew: "novo.db"}, "novo.db", false},
		{"only the old one", map[string]string{envDatabaseOld: "velho.db"}, "velho.db", true},
		{"both: the NEW one wins", map[string]string{
			envDatabaseNew: "novo.db", envDatabaseOld: "velho.db",
		}, "novo.db", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, oldUsed := databasePath(fakeEnvironment(c.vars))
			if path != c.wantPath {
				t.Errorf("path = %q, want %q", path, c.wantPath)
			}
			if oldUsed != c.wantOldUsed {
				t.Errorf("oldNameUsed = %v, want %v", oldUsed, c.wantOldUsed)
			}
		})
	}
}

// --- openStore: ZAPGW_DATABASE/ZAPGW_BANCO and ZAPGW_ENCRYPTION_KEY/ZAPGW_CHAVE_CIFRA ---

func TestOpenStoreAcceptsTheNewDatabaseNameAndItWins(t *testing.T) {
	pathNew := filepath.Join(t.TempDir(), "novo.db")
	pathOld := filepath.Join(t.TempDir(), "velho.db")
	vars := map[string]string{
		envEncryptionKeyNew: testKey,
		envDatabaseNew:      pathNew,
		envDatabaseOld:      pathOld,
	}
	store, err := openStore(fakeEnvironment(vars))
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	_ = store.Close()

	if _, err := os.Stat(pathNew); err != nil {
		t.Errorf("the database from the NEW variable was not created: %v", err)
	}
	if _, err := os.Stat(pathOld); err == nil {
		t.Errorf("the database from the OLD variable was created — the NEW one should have won")
	}
}

func TestOpenStoreAcceptsTheNewEncryptionKeyNameAndItWins(t *testing.T) {
	vars := map[string]string{
		envEncryptionKeyNew: testKey,
		envEncryptionKeyOld: "chave-velha-invalida-de-proposito",
		envDatabaseNew:      filepath.Join(t.TempDir(), "zapgw.db"),
	}
	// If the OLD (invalid) key had won, NewVault would refuse it and
	// openStore would return an error — succeeding here IS the proof the
	// NEW (valid) key won.
	store, err := openStore(fakeEnvironment(vars))
	if err != nil {
		t.Fatalf("the valid NEW key should have won over the invalid old one: %v", err)
	}
	_ = store.Close()
}

func TestOpenStoreWarnsOnlyWhenOldNamesWin(t *testing.T) {
	cases := []struct {
		name         string
		vars         map[string]string
		wantKeyWarn  bool
		wantPathWarn bool
	}{
		{
			"both new: silent",
			map[string]string{envEncryptionKeyNew: testKey, envDatabaseNew: filepath.Join(t.TempDir(), "a.db")},
			false, false,
		},
		{
			"both old: both warn",
			map[string]string{envEncryptionKeyOld: testKey, envDatabaseOld: filepath.Join(t.TempDir(), "b.db")},
			true, true,
		},
	}
	original := log.Writer()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			store, err := openStore(fakeEnvironment(c.vars))
			log.SetOutput(original)
			if err != nil {
				t.Fatalf("openStore: %v", err)
			}
			_ = store.Close()
			text := buf.String()
			keyWarned := strings.Contains(text, envEncryptionKeyOld) && strings.Contains(text, "deprecated")
			pathWarned := strings.Contains(text, envDatabaseOld) && strings.Contains(text, "deprecated")
			if keyWarned != c.wantKeyWarn {
				t.Errorf("key warning = %v (log: %q), want %v", keyWarned, text, c.wantKeyWarn)
			}
			if pathWarned != c.wantPathWarn {
				t.Errorf("database warning = %v (log: %q), want %v", pathWarned, text, c.wantPathWarn)
			}
		})
	}
}

// --- CLI verbs: fumaca/smoke, instancia/instance, consumidor/consumer, estado/state ---

// TestDispatchAcceptsEnglishVerbsSilently is T-214's Verify for the four CLI
// verbs the task names: the OLD (Portuguese) verb still runs and prints the
// T-214 notice; the NEW (English) verb runs identically and stays silent.
// The underlying subcommand is free to error afterward (no --slug, no
// instance) — warnOldVerb writes BEFORE that, so the notice is there either
// way.
func TestDispatchAcceptsEnglishVerbsSilently(t *testing.T) {
	env := fakeEnvironment(testEnvironment(t))
	cases := []struct{ oldVerb, newVerb string }{
		{"fumaca", "smoke"},
		{"instancia", "instance"},
		{"consumidor", "consumer"},
		{"estado", "state"},
	}
	for _, c := range cases {
		t.Run(c.oldVerb+"/"+c.newVerb, func(t *testing.T) {
			var outOld bytes.Buffer
			_ = dispatch([]string{c.oldVerb}, &outOld, env)
			if !strings.Contains(outOld.String(), "deprecated") || !strings.Contains(outOld.String(), c.newVerb) {
				t.Errorf("%q did not warn to use %q: %s", c.oldVerb, c.newVerb, outOld.String())
			}

			var outNew bytes.Buffer
			_ = dispatch([]string{c.newVerb}, &outNew, env)
			if strings.Contains(outNew.String(), "deprecated") {
				t.Errorf("%q (the NEW verb) warned needlessly: %s", c.newVerb, outNew.String())
			}
		})
	}
}

// --- provisionar/rotacionar: ZAPGW_SEND_TOKEN/ZAPGW_TOKEN_ENVIO and ZAPGW_DELIVERY_SECRET/ZAPGW_SEGREDO_ENTREGA ---

func TestCreateInstanceAcceptsTheNewSecretNamesAndTheyWin(t *testing.T) {
	vars := testEnvironment(t)
	vars["ZAPGW_APP_SECRET"] = "app-secret-de-teste"
	vars["ZAPGW_VERIFY_TOKEN"] = "verify-token-de-teste"
	vars[envSendTokenNew] = "token-envio-NOVO"
	vars[envSendTokenOld] = "token-envio-VELHO-nao-pode-vencer"
	vars[envDeliverySecretNew] = "entrega-NOVA"
	vars[envDeliverySecretOld] = "entrega-VELHA-nao-pode-vencer"

	var out bytes.Buffer
	if err := dispatch(instanceArgs("tenant-create-precedencia"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	i := instanceFromEnvironment(t, vars, "tenant-create-precedencia")
	if i.SendToken != "token-envio-NOVO" {
		t.Errorf("SendToken = %q, want the value of the NEW variable", i.SendToken)
	}
	if i.DeliverySecret != "entrega-NOVA" {
		t.Errorf("DeliverySecret = %q, want the value of the NEW variable", i.DeliverySecret)
	}
}

func TestRotateInstanceAcceptsTheNewSecretNamesAndTheyWin(t *testing.T) {
	vars := provisionedForRotation(t, "tenant-rotate-precedencia")
	vars[envSendTokenNew] = "token-envio-NOVO"
	vars[envSendTokenOld] = "token-envio-VELHO-nao-pode-vencer"
	vars[envDeliverySecretNew] = "entrega-NOVA"
	vars[envDeliverySecretOld] = "entrega-VELHA-nao-pode-vencer"

	var out bytes.Buffer
	if err := dispatch([]string{"instancia", "rotacionar", "--slug", "tenant-rotate-precedencia"},
		&out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	i := instanceFromEnvironment(t, vars, "tenant-rotate-precedencia")
	if i.SendToken != "token-envio-NOVO" {
		t.Errorf("SendToken = %q, want the value of the NEW variable", i.SendToken)
	}
	if i.DeliverySecret != "entrega-NOVA" {
		t.Errorf("DeliverySecret = %q, want the value of the NEW variable", i.DeliverySecret)
	}
}

// TestInstagramCreationAcceptsSendTokenNewName is T-114's missing-credential
// guard (provision.go), now also accepting envSendTokenNew alone.
func TestInstagramCreationAcceptsSendTokenNewName(t *testing.T) {
	vars := testEnvironment(t)
	vars["ZAPGW_APP_SECRET"] = "app-secret-de-teste"
	vars[envSendTokenNew] = "token-envio-de-teste"

	var out bytes.Buffer
	err := dispatch(instagramInstanceArgs("insta-alias-envio", "IGID_ALIAS_ENVIO"), &out, fakeEnvironment(vars))
	if err != nil {
		t.Fatalf("--tipo instagram with %s (new name) was REFUSED: %v\n%s", envSendTokenNew, err, out.String())
	}
}

// --- ZAPGW_PUBLIC_URL/ZAPGW_URL_PUBLICA (webhookURL, enrollmentURL) --------

func TestWebhookURLAcceptsTheNewNameAndItWins(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"only the new one", map[string]string{envPublicURLNew: "https://novo.example"}, "https://novo.example/v1/inbound/slug"},
		{"only the old one", map[string]string{envPublicURLOld: "https://velho.example"}, "https://velho.example/v1/inbound/slug"},
		{"both: the NEW one wins", map[string]string{
			envPublicURLNew: "https://novo.example", envPublicURLOld: "https://velho.example",
		}, "https://novo.example/v1/inbound/slug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := webhookURL(fakeEnvironment(c.vars), "slug"); got != c.want {
				t.Errorf("webhookURL = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEnrollmentURLAcceptsTheNewNameAndItWins(t *testing.T) {
	got := enrollmentURL(fakeEnvironment(map[string]string{
		envPublicURLNew: "https://novo.example", envPublicURLOld: "https://velho.example",
	}))
	want := "https://novo.example/v1/cadastro"
	if got != want {
		t.Errorf("enrollmentURL = %q, want %q", got, want)
	}
}

func TestWebhookURLWarnsOnlyWhenOldNameWins(t *testing.T) {
	cases := []struct {
		name     string
		vars     map[string]string
		wantWarn bool
	}{
		{"only the old one: warns", map[string]string{envPublicURLOld: "https://velho.example"}, true},
		{"only the new one: stays silent", map[string]string{envPublicURLNew: "https://novo.example"}, false},
		{"none: stays silent", map[string]string{}, false},
	}
	original := log.Writer()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			webhookURL(fakeEnvironment(c.vars), "slug")
			log.SetOutput(original)
			warned := strings.Contains(buf.String(), envPublicURLOld) && strings.Contains(buf.String(), "deprecated")
			if warned != c.wantWarn {
				t.Errorf("warning = %v (log: %q), want %v", warned, buf.String(), c.wantWarn)
			}
		})
	}
}

// --- ZAPGW_DIAGNOSTIC_PROBE_FOLDER/ZAPGW_DIAGNOSTICO_SONDAR_FOLDER ---------

func TestDiagnosticProbeFolderAcceptsTheNewNameAndItWins(t *testing.T) {
	g := workingInstagramGraph("IGID_ALIAS_SONDA")
	g.conversationsBody[testInvalidFolder] = g.conversationsBody[""]
	vars := diagnosticScenario(t, "insta-alias-sonda", "IGID_ALIAS_SONDA", g)
	vars[envDiagnosticProbeFolderNew] = "1"

	var out bytes.Buffer
	if err := dispatch(diagnosticArgs("insta-alias-sonda"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "5) `folder` parameter probe") {
		t.Fatalf("the probe did NOT turn on with the NEW variable (%s):\n%s", envDiagnosticProbeFolderNew, out.String())
	}
}

func TestDiagnosticProbeFolderWarnsOnlyWhenOldNameWins(t *testing.T) {
	g := workingInstagramGraph("IGID_ALIAS_SONDA_AVISO")
	g.conversationsBody[testInvalidFolder] = g.conversationsBody[""]

	cases := []struct {
		name     string
		set      func(vars map[string]string)
		wantWarn bool
	}{
		{"only the old one: warns", func(v map[string]string) { v[envDiagnosticProbeFolderOld] = "1" }, true},
		{"only the new one: stays silent", func(v map[string]string) { v[envDiagnosticProbeFolderNew] = "1" }, false},
	}
	original := log.Writer()
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slug := fmt.Sprintf("insta-alias-sonda-aviso-%d", i)
			vars := diagnosticScenario(t, slug, "IGID_ALIAS_SONDA_AVISO", g)
			c.set(vars)

			var out bytes.Buffer
			var buf bytes.Buffer
			log.SetOutput(&buf)
			err := dispatch(diagnosticArgs(slug), &out, fakeEnvironment(vars))
			log.SetOutput(original)
			if err != nil {
				t.Fatalf("dispatch: %v\n%s", err, out.String())
			}
			warned := strings.Contains(buf.String(), envDiagnosticProbeFolderOld) && strings.Contains(buf.String(), "deprecated")
			if warned != c.wantWarn {
				t.Errorf("warning = %v (log: %q), want %v", warned, buf.String(), c.wantWarn)
			}
		})
	}
}

// --- Real process boot: the server's actual startup log --------------------

// bootAndCaptureStderr starts `bin` with NO argument (the server path) and
// exactly the env vars in `vars` (plus the ones the OS already carries),
// waits for /v1/health to answer 200, kills the process and returns
// everything written to stderr. Killing BEFORE reading is what makes the
// read race-free: exec.Cmd copies a non-*os.File Stderr through a pipe on a
// background goroutine, and Wait() only returns after that goroutine is
// done — the same guarantee startServerAndGetHealth relies on, just
// exercised after Wait instead of skipped.
func bootAndCaptureStderr(t *testing.T, bin string, vars map[string]string) string {
	t.Helper()
	address, ok := vars["ZAPGW_ENDERECO"]
	if !ok {
		address = vars["ZAPGW_ADDRESS"]
	}
	if address == "" {
		t.Fatal("bootAndCaptureStderr: vars needs ZAPGW_ENDERECO or ZAPGW_ADDRESS")
	}

	env := os.Environ()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	cmd := exec.Command(bin)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var lastError error
	healthy := false
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + address + "/v1/health")
		if err != nil {
			lastError = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			healthy = true
			break
		}
		lastError = fmt.Errorf("status %d", resp.StatusCode)
		time.Sleep(50 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if !healthy {
		t.Fatalf("/v1/health at %s did not answer in time: %v\nprocess stderr:\n%s",
			address, lastError, stderr.String())
	}
	return stderr.String()
}

// TestServerStartupWarnsOnOldNamesAndStaysSilentOnNewNames is T-214's
// end-to-end Verify: the REAL binary, booted twice — once with every
// server-time ZAPGW_* variable in its OLD (Portuguese) spelling, once with
// every one in its NEW (English) spelling — proves the startup log prints
// the T-214 notice for each old name used, and prints NOTHING extra when
// every variable is already migrated.
func TestServerStartupWarnsOnOldNamesAndStaysSilentOnNewNames(t *testing.T) {
	bin := buildWithVersion(t, "0.0.0-t214-teste")

	oldNames := []string{
		"ZAPGW_BANCO", "ZAPGW_CHAVE_CIFRA", "ZAPGW_ENDERECO", "ZAPGW_MAX_CORPO_BYTES",
		"ZAPGW_TTL_IDEMPOTENCIA_HORAS", "ZAPGW_TTL_CONTADORES_DIAS", "ZAPGW_TTL_TRANSITO_DIAS",
		"ZAPGW_ENTRADA_VIA", "ZAPGW_CONECTOR_READY", "ZAPGW_LIDERANCA_ARQUIVO",
		"ZAPGW_LIDERANCA_VALIDADE", "ZAPGW_SONDA_EXTERNA_URL",
	}
	oldVars := map[string]string{
		"ZAPGW_CHAVE_CIFRA":            testKey,
		"ZAPGW_BANCO":                  filepath.Join(t.TempDir(), "old.db"),
		"ZAPGW_ENDERECO":               freeAddress(t),
		"ZAPGW_MAX_CORPO_BYTES":        "2097152",
		"ZAPGW_TTL_IDEMPOTENCIA_HORAS": "48",
		"ZAPGW_TTL_CONTADORES_DIAS":    "60",
		"ZAPGW_TTL_TRANSITO_DIAS":      "20",
		"ZAPGW_ENTRADA_VIA":            "tunel",
		"ZAPGW_CONECTOR_READY":         "http://127.0.0.1:9/ready",
		"ZAPGW_LIDERANCA_ARQUIVO":      filepath.Join(t.TempDir(), "lider"),
		"ZAPGW_LIDERANCA_VALIDADE":     "8s",
		"ZAPGW_SONDA_EXTERNA_URL":      "http://127.0.0.1:9/status",
	}
	oldStderr := bootAndCaptureStderr(t, bin, oldVars)
	for _, name := range oldNames {
		if !strings.Contains(oldStderr, name) || !strings.Contains(oldStderr, "deprecated") {
			t.Errorf("startup with old names did NOT warn about %s:\nstderr:\n%s", name, oldStderr)
		}
	}

	newVars := map[string]string{
		"ZAPGW_ENCRYPTION_KEY":        testKey,
		"ZAPGW_DATABASE":              filepath.Join(t.TempDir(), "new.db"),
		"ZAPGW_ADDRESS":               freeAddress(t),
		"ZAPGW_MAX_BODY_BYTES":        "2097152",
		"ZAPGW_TTL_IDEMPOTENCY_HOURS": "48",
		"ZAPGW_TTL_COUNTERS_DAYS":     "60",
		"ZAPGW_TTL_TRANSIT_DAYS":      "20",
		"ZAPGW_INGRESS_VIA":           "tunel",
		"ZAPGW_CONNECTOR_READY":       "http://127.0.0.1:9/ready",
		"ZAPGW_LEADERSHIP_FILE":       filepath.Join(t.TempDir(), "lider"),
		"ZAPGW_LEADERSHIP_VALIDITY":   "8s",
		"ZAPGW_EXTERNAL_PROBE_URL":    "http://127.0.0.1:9/status",
	}
	newStderr := bootAndCaptureStderr(t, bin, newVars)
	if strings.Contains(newStderr, "deprecated") {
		t.Errorf("startup with ALL NEW names printed an unwarranted T-214 notice:\nstderr:\n%s", newStderr)
	}
}
