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
		{"nenhuma variavel: default", map[string]string{}, "zapgw.db", false},
		{"so a nova", map[string]string{envDatabaseNew: "novo.db"}, "novo.db", false},
		{"so a velha", map[string]string{envDatabaseOld: "velho.db"}, "velho.db", true},
		{"as duas: a NOVA vence", map[string]string{
			envDatabaseNew: "novo.db", envDatabaseOld: "velho.db",
		}, "novo.db", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, oldUsed := databasePath(fakeEnvironment(c.vars))
			if path != c.wantPath {
				t.Errorf("path = %q, quero %q", path, c.wantPath)
			}
			if oldUsed != c.wantOldUsed {
				t.Errorf("oldNameUsed = %v, quero %v", oldUsed, c.wantOldUsed)
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
		t.Errorf("o banco da variavel NOVA nao foi criado: %v", err)
	}
	if _, err := os.Stat(pathOld); err == nil {
		t.Errorf("o banco da variavel VELHA foi criado — a NOVA devia ter vencido")
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
		t.Fatalf("a chave NOVA valida devia ter vencido a velha invalida: %v", err)
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
			"as duas novas: caladas",
			map[string]string{envEncryptionKeyNew: testKey, envDatabaseNew: filepath.Join(t.TempDir(), "a.db")},
			false, false,
		},
		{
			"as duas velhas: avisam as duas",
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
			keyWarned := strings.Contains(text, envEncryptionKeyOld) && strings.Contains(text, "obsoleta")
			pathWarned := strings.Contains(text, envDatabaseOld) && strings.Contains(text, "obsoleta")
			if keyWarned != c.wantKeyWarn {
				t.Errorf("aviso da chave = %v (log: %q), quero %v", keyWarned, text, c.wantKeyWarn)
			}
			if pathWarned != c.wantPathWarn {
				t.Errorf("aviso do banco = %v (log: %q), quero %v", pathWarned, text, c.wantPathWarn)
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
			if !strings.Contains(outOld.String(), "obsoleto") || !strings.Contains(outOld.String(), c.newVerb) {
				t.Errorf("%q nao avisou para usar %q: %s", c.oldVerb, c.newVerb, outOld.String())
			}

			var outNew bytes.Buffer
			_ = dispatch([]string{c.newVerb}, &outNew, env)
			if strings.Contains(outNew.String(), "obsoleto") {
				t.Errorf("%q (o verbo NOVO) avisou sem precisar: %s", c.newVerb, outNew.String())
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
		t.Errorf("SendToken = %q, quero o valor da variavel NOVA", i.SendToken)
	}
	if i.DeliverySecret != "entrega-NOVA" {
		t.Errorf("DeliverySecret = %q, quero o valor da variavel NOVA", i.DeliverySecret)
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
		t.Errorf("SendToken = %q, quero o valor da variavel NOVA", i.SendToken)
	}
	if i.DeliverySecret != "entrega-NOVA" {
		t.Errorf("DeliverySecret = %q, quero o valor da variavel NOVA", i.DeliverySecret)
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
		t.Fatalf("--tipo instagram com %s (nome novo) foi RECUSADA: %v\n%s", envSendTokenNew, err, out.String())
	}
}

// --- ZAPGW_PUBLIC_URL/ZAPGW_URL_PUBLICA (webhookURL, enrollmentURL) --------

func TestWebhookURLAcceptsTheNewNameAndItWins(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"so a nova", map[string]string{envPublicURLNew: "https://novo.example"}, "https://novo.example/v1/inbound/slug"},
		{"so a velha", map[string]string{envPublicURLOld: "https://velho.example"}, "https://velho.example/v1/inbound/slug"},
		{"as duas: a NOVA vence", map[string]string{
			envPublicURLNew: "https://novo.example", envPublicURLOld: "https://velho.example",
		}, "https://novo.example/v1/inbound/slug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := webhookURL(fakeEnvironment(c.vars), "slug"); got != c.want {
				t.Errorf("webhookURL = %q, quero %q", got, c.want)
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
		t.Errorf("enrollmentURL = %q, quero %q", got, want)
	}
}

func TestWebhookURLWarnsOnlyWhenOldNameWins(t *testing.T) {
	cases := []struct {
		name     string
		vars     map[string]string
		wantWarn bool
	}{
		{"so a velha: avisa", map[string]string{envPublicURLOld: "https://velho.example"}, true},
		{"so a nova: fica calado", map[string]string{envPublicURLNew: "https://novo.example"}, false},
		{"nenhuma: fica calado", map[string]string{}, false},
	}
	original := log.Writer()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			webhookURL(fakeEnvironment(c.vars), "slug")
			log.SetOutput(original)
			warned := strings.Contains(buf.String(), envPublicURLOld) && strings.Contains(buf.String(), "obsoleta")
			if warned != c.wantWarn {
				t.Errorf("aviso = %v (log: %q), quero %v", warned, buf.String(), c.wantWarn)
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
	if !strings.Contains(out.String(), "5) sonda do parametro `folder`") {
		t.Fatalf("a sonda NAO ligou com a variavel NOVA (%s):\n%s", envDiagnosticProbeFolderNew, out.String())
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
		{"so a velha: avisa", func(v map[string]string) { v[envDiagnosticProbeFolderOld] = "1" }, true},
		{"so a nova: fica calado", func(v map[string]string) { v[envDiagnosticProbeFolderNew] = "1" }, false},
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
			warned := strings.Contains(buf.String(), envDiagnosticProbeFolderOld) && strings.Contains(buf.String(), "obsoleta")
			if warned != c.wantWarn {
				t.Errorf("aviso = %v (log: %q), quero %v", warned, buf.String(), c.wantWarn)
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
		t.Fatal("bootAndCaptureStderr: vars precisa de ZAPGW_ENDERECO ou ZAPGW_ADDRESS")
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
		t.Fatalf("iniciar %s: %v", bin, err)
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
		t.Fatalf("/v1/health em %s nao respondeu a tempo: %v\nstderr do processo:\n%s",
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
		if !strings.Contains(oldStderr, name) || !strings.Contains(oldStderr, "obsoleta") {
			t.Errorf("arranque com nomes velhos NAO avisou sobre %s:\nstderr:\n%s", name, oldStderr)
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
	if strings.Contains(newStderr, "obsoleta") {
		t.Errorf("arranque com TODOS os nomes NOVOS imprimiu aviso T-214 indevido:\nstderr:\n%s", newStderr)
	}
}
