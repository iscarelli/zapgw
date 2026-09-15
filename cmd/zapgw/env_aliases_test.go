// Tests for T-244: the old (Portuguese) ZAPGW_* env-var names are RETIRED
// — an old name set at startup (or at command time) is REFUSED, naming the
// new (English) one, and its value is NEVER read. (Five of the CLI verbs
// were a separate, still-open decision at the time this file was written —
// T-220 removed those, and T-245 later removed the rest -- "estado" and
// the eight sub-verbs -- the same way; see TestDispatchRefusesRemovedTopLevelVerbs
// and TestDispatchRefusesRemovedSubVerbs in provision_test.go.)
package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

// --- databasePath (shared by openStore and `zapgw perdidas`) --------------

// TestDatabasePathRefusesOldName is T-244's Verify: (a) only the new name
// -> read; (b) only the old one -> refused, naming the new name, value not
// read (the default does not kick in either); (c) both -> refused too.
func TestDatabasePathRefusesOldName(t *testing.T) {
	cases := []struct {
		name    string
		vars    map[string]string
		want    string
		wantErr bool
	}{
		{"no variable at all: default", map[string]string{}, "zapgw.db", false},
		{"(a) only the new one -> read", map[string]string{envDatabaseNew: "novo.db"}, "novo.db", false},
		{"(b) only the old one -> refused", map[string]string{envDatabaseOld: "velho.db"}, "", true},
		{"(c) both -> refused too", map[string]string{
			envDatabaseNew: "novo.db", envDatabaseOld: "velho.db",
		}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, err := databasePath(fakeEnvironment(c.vars))
			if c.wantErr {
				if err == nil {
					t.Fatalf("databasePath = (%q, nil), want a refusal naming %s", path, envDatabaseNew)
				}
				if !errors.Is(err, config.ErrObsoleteEnvVar) {
					t.Errorf("error does not wrap config.ErrObsoleteEnvVar: %v", err)
				}
				if !strings.Contains(err.Error(), envDatabaseNew) {
					t.Errorf("the refusal does not name %s: %v", envDatabaseNew, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("databasePath: %v", err)
			}
			if path != c.want {
				t.Errorf("path = %q, want %q", path, c.want)
			}
		})
	}
}

// TestOpenStoreRefusesOldEncryptionKeyName is T-244's mandatory case (b) for
// the encryption key: ZAPGW_CHAVE_CIFRA alone has to REFUSE startup, naming
// ZAPGW_ENCRYPTION_KEY, and the value must NOT be read — the failure mode
// this task exists to close is the gateway opening an EMPTY database under a
// DIFFERENT key, in silence.
func TestOpenStoreRefusesOldEncryptionKeyName(t *testing.T) {
	vars := map[string]string{
		envEncryptionKeyOld: testKey,
		envDatabaseNew:      filepath.Join(t.TempDir(), "zapgw.db"),
	}
	_, err := openStore(fakeEnvironment(vars))
	if err == nil {
		t.Fatal("openStore ACCEPTED the old encryption-key name — this would open an empty database under a different key, in silence")
	}
	if !errors.Is(err, config.ErrObsoleteEnvVar) {
		t.Errorf("error does not wrap config.ErrObsoleteEnvVar: %v", err)
	}
	if !strings.Contains(err.Error(), "ZAPGW_ENCRYPTION_KEY") {
		t.Errorf("the refusal does not name ZAPGW_ENCRYPTION_KEY: %v", err)
	}
}

// --- openStore: ZAPGW_DATABASE/ZAPGW_BANCO and ZAPGW_ENCRYPTION_KEY/ZAPGW_CHAVE_CIFRA ---

func TestOpenStoreAcceptsTheNewNames(t *testing.T) {
	pathNew := filepath.Join(t.TempDir(), "novo.db")
	vars := map[string]string{
		envEncryptionKeyNew: testKey,
		envDatabaseNew:      pathNew,
	}
	store, err := openStore(fakeEnvironment(vars))
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	_ = store.Close()

	if _, err := os.Stat(pathNew); err != nil {
		t.Errorf("the database from the NEW variable was not created: %v", err)
	}
}

// TestOpenStoreRefusesOldDatabaseName is case (b) for the database path
// pair, exercised through openStore (not just databasePath) — the same
// refusal has to reach the caller that actually opens the file.
func TestOpenStoreRefusesOldDatabaseName(t *testing.T) {
	vars := map[string]string{
		envEncryptionKeyNew: testKey,
		envDatabaseOld:      filepath.Join(t.TempDir(), "velho.db"),
	}
	_, err := openStore(fakeEnvironment(vars))
	if err == nil {
		t.Fatal("openStore ACCEPTED the old database-path name")
	}
	if !strings.Contains(err.Error(), envDatabaseNew) {
		t.Errorf("the refusal does not name %s: %v", envDatabaseNew, err)
	}
}

// --- CLI verbs: estado/state ---
// T-245 retired "estado" the same way T-220 retired the other four
// top-level pairs this test used to cover (fumaca, instancia, consumidor,
// and now estado) — the Portuguese spelling REFUSES, naming the English
// one, instead of dispatching with a deprecation notice. See
// TestDispatchRefusesRemovedTopLevelVerbs in provision_test.go.

// --- provision/rotate: ZAPGW_SEND_TOKEN/ZAPGW_TOKEN_ENVIO and ZAPGW_DELIVERY_SECRET/ZAPGW_SEGREDO_ENTREGA ---

func TestCreateInstanceAcceptsTheNewSecretNames(t *testing.T) {
	vars := testEnvironment(t)
	vars["ZAPGW_APP_SECRET"] = "app-secret-de-teste"
	vars["ZAPGW_VERIFY_TOKEN"] = "verify-token-de-teste"
	vars[envSendTokenNew] = "token-envio-NOVO"
	vars[envDeliverySecretNew] = "entrega-NOVA"

	var out bytes.Buffer
	if err := dispatch(instanceArgs("tenant-create-novo"), &out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	i := instanceFromEnvironment(t, vars, "tenant-create-novo")
	if i.SendToken != "token-envio-NOVO" {
		t.Errorf("SendToken = %q, want the value of the NEW variable", i.SendToken)
	}
	if i.DeliverySecret != "entrega-NOVA" {
		t.Errorf("DeliverySecret = %q, want the value of the NEW variable", i.DeliverySecret)
	}
}

// TestCreateInstanceRefusesOldSecretNames is T-244's case (b)/(c) for
// envSendTokenOld/envDeliverySecretOld: either one being set REFUSES
// creation, naming the corresponding new name.
func TestCreateInstanceRefusesOldSecretNames(t *testing.T) {
	cases := []struct {
		name string
		set  func(vars map[string]string)
		want string
	}{
		{"(b) only the old send token", func(v map[string]string) {
			v[envSendTokenOld] = "token-envio-VELHO"
		}, envSendTokenNew},
		{"(c) both send token names", func(v map[string]string) {
			v[envSendTokenNew] = "token-envio-NOVO"
			v[envSendTokenOld] = "token-envio-VELHO"
		}, envSendTokenNew},
		{"(b) only the old delivery secret", func(v map[string]string) {
			v["ZAPGW_APP_SECRET"] = "app-secret-de-teste"
			v[envSendTokenNew] = "token-envio-de-teste"
			v[envDeliverySecretOld] = "entrega-VELHA"
		}, envDeliverySecretNew},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vars := testEnvironment(t)
			c.set(vars)
			var out bytes.Buffer
			err := dispatch(instanceArgs(fmt.Sprintf("tenant-refusa-velho-%d", i)), &out, fakeEnvironment(vars))
			if err == nil {
				t.Fatal("the creation was ACCEPTED with an old secret name set")
			}
			if !errors.Is(err, config.ErrObsoleteEnvVar) {
				t.Errorf("error does not wrap config.ErrObsoleteEnvVar: %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the refusal does not name %s: %v", c.want, err)
			}
		})
	}
}

func TestRotateInstanceAcceptsTheNewSecretNames(t *testing.T) {
	vars := provisionedForRotation(t, "tenant-rotate-novo")
	vars[envSendTokenNew] = "token-envio-NOVO"
	vars[envDeliverySecretNew] = "entrega-NOVA"

	var out bytes.Buffer
	if err := dispatch([]string{"instance", "rotate", "--slug", "tenant-rotate-novo"},
		&out, fakeEnvironment(vars)); err != nil {
		t.Fatalf("dispatch: %v\n%s", err, out.String())
	}
	i := instanceFromEnvironment(t, vars, "tenant-rotate-novo")
	if i.SendToken != "token-envio-NOVO" {
		t.Errorf("SendToken = %q, want the value of the NEW variable", i.SendToken)
	}
	if i.DeliverySecret != "entrega-NOVA" {
		t.Errorf("DeliverySecret = %q, want the value of the NEW variable", i.DeliverySecret)
	}
}

// TestRotateInstanceRefusesOldSecretNames mirrors
// TestCreateInstanceRefusesOldSecretNames for `instance rotate`.
func TestRotateInstanceRefusesOldSecretNames(t *testing.T) {
	vars := provisionedForRotation(t, "tenant-rotate-refusa")
	vars[envSendTokenOld] = "token-envio-VELHO"

	var out bytes.Buffer
	err := dispatch([]string{"instance", "rotate", "--slug", "tenant-rotate-refusa"},
		&out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("the rotation was ACCEPTED with the old send-token name set")
	}
	if !errors.Is(err, config.ErrObsoleteEnvVar) {
		t.Errorf("error does not wrap config.ErrObsoleteEnvVar: %v", err)
	}
	if !strings.Contains(err.Error(), envSendTokenNew) {
		t.Errorf("the refusal does not name %s: %v", envSendTokenNew, err)
	}
}

// TestInstagramCreationAcceptsSendTokenNewName is T-114's missing-credential
// guard (provision.go), reading ONLY envSendTokenNew now (T-244).
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

// TestInstagramCreationRefusesSendTokenOldName is T-244's case (b) for the
// same guard: the old name being set REFUSES with config.ErrObsoleteEnvVar,
// never with the "missing credential" message.
func TestInstagramCreationRefusesSendTokenOldName(t *testing.T) {
	vars := testEnvironment(t)
	vars["ZAPGW_APP_SECRET"] = "app-secret-de-teste"
	vars[envSendTokenOld] = "token-envio-de-teste"

	var out bytes.Buffer
	err := dispatch(instagramInstanceArgs("insta-alias-envio-recusa", "IGID_ALIAS_ENVIO_RECUSA"), &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("the creation was ACCEPTED with the old send-token name set")
	}
	if !errors.Is(err, config.ErrObsoleteEnvVar) {
		t.Errorf("error does not wrap config.ErrObsoleteEnvVar: %v", err)
	}
	if !strings.Contains(err.Error(), envSendTokenNew) {
		t.Errorf("the refusal does not name %s: %v", envSendTokenNew, err)
	}
}

// --- ZAPGW_PUBLIC_URL/ZAPGW_URL_PUBLICA (webhookURL, enrollmentURL) --------

// TestWebhookURLRefusesOldName is T-244's Verify: (a) only the new name ->
// read; (b) only the old one -> refused, naming the new name; (c) both ->
// refused too.
func TestWebhookURLRefusesOldName(t *testing.T) {
	cases := []struct {
		name    string
		vars    map[string]string
		want    string
		wantErr bool
	}{
		{"(a) only the new one -> read", map[string]string{envPublicURLNew: "https://novo.example"}, "https://novo.example/v1/inbound/slug", false},
		{"(b) only the old one -> refused", map[string]string{envPublicURLOld: "https://velho.example"}, "", true},
		{"(c) both -> refused too", map[string]string{
			envPublicURLNew: "https://novo.example", envPublicURLOld: "https://velho.example",
		}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := webhookURL(fakeEnvironment(c.vars), "slug")
			if c.wantErr {
				if err == nil {
					t.Fatalf("webhookURL = (%q, nil), want a refusal naming %s", got, envPublicURLNew)
				}
				if !errors.Is(err, config.ErrObsoleteEnvVar) {
					t.Errorf("error does not wrap config.ErrObsoleteEnvVar: %v", err)
				}
				if !strings.Contains(err.Error(), envPublicURLNew) {
					t.Errorf("the refusal does not name %s: %v", envPublicURLNew, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("webhookURL: %v", err)
			}
			if got != c.want {
				t.Errorf("webhookURL = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEnrollmentURLRefusesOldName(t *testing.T) {
	got, err := enrollmentURL(fakeEnvironment(map[string]string{envPublicURLNew: "https://novo.example"}))
	if err != nil {
		t.Fatalf("enrollmentURL: %v", err)
	}
	if want := "https://novo.example/v1/cadastro"; got != want {
		t.Errorf("enrollmentURL = %q, want %q", got, want)
	}

	_, err = enrollmentURL(fakeEnvironment(map[string]string{envPublicURLOld: "https://velho.example"}))
	if err == nil {
		t.Fatal("enrollmentURL accepted the old name")
	}
	if !strings.Contains(err.Error(), envPublicURLNew) {
		t.Errorf("the refusal does not name %s: %v", envPublicURLNew, err)
	}
}

// --- ZAPGW_DIAGNOSTIC_PROBE_FOLDER/ZAPGW_DIAGNOSTICO_SONDAR_FOLDER ---------

func TestDiagnosticProbeFolderAcceptsTheNewName(t *testing.T) {
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

// TestDiagnosticProbeFolderRefusesOldName is T-244's case (b): the old
// name being set REFUSES the whole `diagnostico` command.
func TestDiagnosticProbeFolderRefusesOldName(t *testing.T) {
	g := workingInstagramGraph("IGID_ALIAS_SONDA_RECUSA")
	g.conversationsBody[testInvalidFolder] = g.conversationsBody[""]
	vars := diagnosticScenario(t, "insta-alias-sonda-recusa", "IGID_ALIAS_SONDA_RECUSA", g)
	vars[envDiagnosticProbeFolderOld] = "1"

	var out bytes.Buffer
	err := dispatch(diagnosticArgs("insta-alias-sonda-recusa"), &out, fakeEnvironment(vars))
	if err == nil {
		t.Fatal("dispatch accepted the old diagnostic-probe-folder name")
	}
	if !errors.Is(err, config.ErrObsoleteEnvVar) {
		t.Errorf("error does not wrap config.ErrObsoleteEnvVar: %v", err)
	}
	if !strings.Contains(err.Error(), envDiagnosticProbeFolderNew) {
		t.Errorf("the refusal does not name %s: %v", envDiagnosticProbeFolderNew, err)
	}
}

// --- Real process boot: the server's actual startup log --------------------

// bootAndCaptureStderr starts `bin` with NO argument (the server path) and
// exactly the env vars in `vars` (plus the ones the OS already carries),
// waits up to `timeout` for /v1/health to answer 200 OR for the process to
// exit, kills the process if it is still running and returns everything
// written to stderr plus whether it ever became healthy. Killing BEFORE
// reading is what makes the read race-free: exec.Cmd copies a non-*os.File
// Stderr through a pipe on a background goroutine, and Wait() only returns
// after that goroutine is done.
func bootAndCaptureStderr(t *testing.T, bin string, vars map[string]string, timeout time.Duration) (stderr string, healthy bool) {
	t.Helper()
	address := vars["ZAPGW_ADDRESS"]
	if address == "" {
		t.Fatal("bootAndCaptureStderr: vars needs ZAPGW_ADDRESS")
	}

	env := os.Environ()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	cmd := exec.Command(bin)
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return buf.String(), false
		default:
		}
		resp, err := http.Get("http://" + address + "/v1/health")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			_ = cmd.Process.Kill()
			<-exited
			return buf.String(), true
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	<-exited
	return buf.String(), false
}

// TestServerStartupRefusesOldNamesAndBootsSilentlyOnNewNames is T-244's
// end-to-end Verify: the REAL binary, booted with every server-time
// ZAPGW_* variable in its OLD (Portuguese) spelling, NEVER becomes healthy
// and its stderr names the new variable to use for each one; booted with
// every variable in its NEW (English) spelling, it boots and stays silent
// (no "no longer read" / "deprecated" text at all).
func TestServerStartupRefusesOldNamesAndBootsSilentlyOnNewNames(t *testing.T) {
	bin := buildWithVersion(t, "0.0.0-t244-teste")

	// A complete set of GOOD values, entirely under the NEW names — this
	// is also exercised standalone, below, as the silent-boot case.
	goodNewVars := map[string]string{
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
	oldToNew := map[string]string{
		"ZAPGW_BANCO":                  "ZAPGW_DATABASE",
		"ZAPGW_CHAVE_CIFRA":            "ZAPGW_ENCRYPTION_KEY",
		"ZAPGW_MAX_CORPO_BYTES":        "ZAPGW_MAX_BODY_BYTES",
		"ZAPGW_TTL_IDEMPOTENCIA_HORAS": "ZAPGW_TTL_IDEMPOTENCY_HOURS",
		"ZAPGW_TTL_CONTADORES_DIAS":    "ZAPGW_TTL_COUNTERS_DAYS",
		"ZAPGW_TTL_TRANSITO_DIAS":      "ZAPGW_TTL_TRANSIT_DAYS",
		"ZAPGW_ENTRADA_VIA":            "ZAPGW_INGRESS_VIA",
		"ZAPGW_CONECTOR_READY":         "ZAPGW_CONNECTOR_READY",
		"ZAPGW_LIDERANCA_ARQUIVO":      "ZAPGW_LEADERSHIP_FILE",
		"ZAPGW_LIDERANCA_VALIDADE":     "ZAPGW_LEADERSHIP_VALIDITY",
		"ZAPGW_SONDA_EXTERNA_URL":      "ZAPGW_EXTERNAL_PROBE_URL",
	}

	// One OLD name at a time: setting several together would only prove
	// the FIRST one checked refuses, not that each pair still refuses on
	// its own. Every OTHER variable keeps its GOOD value under the NEW
	// name — the address needs its own free port per sub-test, and the
	// leadership pair needs the FILE variable present for the VALIDITY
	// one to even be looked at (a disarmed guard never reads validity).
	for oldName, newName := range oldToNew {
		t.Run(oldName, func(t *testing.T) {
			vars := map[string]string{}
			for k, v := range goodNewVars {
				vars[k] = v
			}
			vars["ZAPGW_ADDRESS"] = freeAddress(t)
			if oldName == "ZAPGW_LIDERANCA_VALIDADE" {
				vars["ZAPGW_LEADERSHIP_FILE"] = filepath.Join(t.TempDir(), "lider")
			}
			value := vars[newName]
			delete(vars, newName)
			vars[oldName] = value

			stderr, healthy := bootAndCaptureStderr(t, bin, vars, 5*time.Second)
			if healthy {
				t.Fatalf("the server booted HEALTHY with the old name %s set — it must refuse to start.\nstderr:\n%s", oldName, stderr)
			}
			if !strings.Contains(stderr, newName) {
				t.Errorf("startup with %s set did not name %s in stderr:\n%s", oldName, newName, stderr)
			}
		})
	}

	silentVars := map[string]string{}
	for k, v := range goodNewVars {
		silentVars[k] = v
	}
	silentVars["ZAPGW_ADDRESS"] = freeAddress(t)
	newStderr, healthy := bootAndCaptureStderr(t, bin, silentVars, 15*time.Second)
	if !healthy {
		t.Fatalf("the server did NOT boot healthy with every variable in its NEW name:\nstderr:\n%s", newStderr)
	}
	for _, forbidden := range []string{"no longer read", "deprecated", "ErrObsoleteEnvVar"} {
		if strings.Contains(newStderr, forbidden) {
			t.Errorf("startup with ALL NEW names printed an unwarranted notice (%q):\nstderr:\n%s", forbidden, newStderr)
		}
	}
}
