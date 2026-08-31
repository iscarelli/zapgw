// Tests for the send singleton guard (lideranca.go).
//
// What these tests exist to prevent is ONE defect, and it is expensive: the
// gateway sending when it is NOT the titular of the pair. The symptom does
// not show up here — it shows up on a client's phone, with the message
// repeated, and nothing in the log saying there were two instances alive.
//
// That's why most of these cases test the REFUSAL, not the permission: it's
// the side where getting it wrong costs money and trust.
package outbound

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// leaseFile creates a lease file with the requested age.
func leaseFile(t *testing.T, age time.Duration) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lider")
	if err := os.WriteFile(path, []byte("ok\n"), 0o600); err != nil {
		t.Fatalf("criar arquivo de concessao: %v", err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("envelhecer arquivo de concessao: %v", err)
	}
	return path
}

// markingHandler returns a handler that records that it was called.
func markingHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestLeadershipDisarmedLetsThroughAndMatchesThePreviousBehavior(t *testing.T) {
	l, err := NewLeadership(func(string) string { return "" })
	if err != nil {
		t.Fatalf("NewLeadership: %v", err)
	}
	if l.Armed() {
		t.Fatal("sem ZAPGW_LIDERANCA_ARQUIVO a guarda tem de ficar DESARMADA — e' a instalacao de no unico")
	}
	if ok, reason := l.Holder(); !ok {
		t.Fatalf("guarda desarmada tem de responder titular=true; veio false (%s)", reason)
	}

	var called bool
	internal := markingHandler(&called)
	// Disarmed, Require returns the handler ITSELF: there is nothing to check,
	// and wrapping it would only add work on the critical path.
	if got := l.Require(internal); fmt.Sprintf("%p", got) != fmt.Sprintf("%p", internal) {
		t.Error("desarmada, Require deveria devolver o handler original sem embrulho")
	}

	w := httptest.NewRecorder()
	l.Require(internal).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if !called {
		t.Fatal("guarda desarmada barrou o envio — isso quebraria toda instalacao de no unico")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, queria 200", w.Code)
	}
}

func TestLeadershipWithFreshLeaseLetsTheSendThrough(t *testing.T) {
	path := leaseFile(t, 1*time.Second)
	l := &Leadership{file: path, validity: 15 * time.Second}

	if ok, reason := l.Holder(); !ok {
		t.Fatalf("concessao de 1s com limite de 15s tem de valer; recusou: %s", reason)
	}

	var called bool
	w := httptest.NewRecorder()
	l.Require(markingHandler(&called)).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if !called || w.Code != http.StatusOK {
		t.Fatalf("titular legitimo foi barrado: chamou=%v status=%d", called, w.Code)
	}
}

// 🔴 The case that justifies the whole file: a stale lease does NOT send.
func TestLeadershipWithSTALELeaseRefusesAsRetryable(t *testing.T) {
	path := leaseFile(t, 90*time.Second)
	l := &Leadership{file: path, validity: 15 * time.Second, logf: func(string, ...any) {}}

	ok, reason := l.Holder()
	if ok {
		t.Fatal("concessao de 90s com limite de 15s NAO pode valer — este e o standby que subiu e nunca foi promovido")
	}
	if reason == "" {
		t.Error("a recusa tem de dizer POR QUE; motivo vazio faz alguem reiniciar o servico procurando defeito que nao existe")
	}

	var called bool
	w := httptest.NewRecorder()
	l.Require(markingHandler(&called)).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if called {
		t.Fatal("o handler de envio foi chamado sem lideranca — e' exatamente a mensagem duplicada que a guarda existe para impedir")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, queria 503: o consumidor precisa REPETIR, nao desistir", w.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo nao e JSON de erro: %v", err)
	}
	if body.Error.Class != "retentavel" {
		t.Errorf("classe = %q, queria \"retentavel\" — um 4xx faria o consumidor DESISTIR de uma mensagem que so' precisava de outro destino", body.Error.Class)
	}
}

// 🔴 Fail closed: not being able to VERIFY is not "everything is fine".
func TestLeadershipWithoutFileRefusesInsteadOfAssumingAllIsWell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nao-existe")
	l := &Leadership{file: path, validity: 15 * time.Second, logf: func(string, ...any) {}}

	ok, reason := l.Holder()
	if ok {
		t.Fatal("arquivo de concessao AUSENTE tem de recusar — 'nao consegui verificar' nunca pode virar 'pode enviar'")
	}
	if reason == "" {
		t.Error("a recusa por ausencia tem de dizer o caminho que faltou")
	}

	var called bool
	w := httptest.NewRecorder()
	l.Require(markingHandler(&called)).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if called || w.Code != http.StatusServiceUnavailable {
		t.Fatalf("ausencia de concessao deixou passar: chamou=%v status=%d", called, w.Code)
	}
}

func TestNewLeadershipRefusesToStartWithUnreadableOrNonPositiveValidity(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"texto que nao e duracao", "quinze segundos"},
		{"numero sem unidade", "15"},
		{"zero", "0s"},
		{"negativa", "-5s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewLeadership(func(k string) string {
				switch k {
				case VarLeadershipFile:
					return "/run/zapgw/lider"
				case VarLeadershipValidity:
					return c.value
				}
				return ""
			})
			if err == nil {
				t.Fatalf("%s = %q tinha de DERRUBAR a subida; valor ilegivel virando padrao silencioso so' aparece no dia do failover",
					VarLeadershipValidity, c.value)
			}
		})
	}
}

// 🔴 The defect the consumer found on the SAME day: a validity default is a
// guess about etcd's TTL, which this process doesn't know — and the wrong
// guess allows TWO titulars. Now arming without a validity does not come up.
func TestNewLeadershipRefusesToStartArmedWithoutValidity(t *testing.T) {
	_, err := NewLeadership(func(k string) string {
		if k == VarLeadershipFile {
			return "/run/zapgw/lider"
		}
		return ""
	})
	if err == nil {
		t.Fatal("guarda ARMADA sem ZAPGW_LIDERANCA_VALIDADE tinha de DERRUBAR a subida: qualquer default e um chute sobre o TTL alheio, e o chute errado sobrepoe dois titulares")
	}
	for _, required := range []string{"V + A < T", "duplicada"} {
		if !strings.Contains(err.Error(), required) {
			t.Errorf("a mensagem tem de carregar %q — quem arma precisa da formula, nao de um \"faltou variavel\"; veio: %v", required, err)
		}
	}
}

// Disarmed does NOT require validity: a single-node install cannot be forced
// to configure a pair that doesn't exist.
func TestNewLeadershipDisarmedDoesNotRequireValidity(t *testing.T) {
	l, err := NewLeadership(func(string) string { return "" })
	if err != nil {
		t.Fatalf("desarmada nao pode exigir validade: %v", err)
	}
	if l.Armed() {
		t.Fatal("sem arquivo a guarda tem de ficar desarmada")
	}
}

func TestNewLeadershipReadsFileAndValidity(t *testing.T) {
	l, err := NewLeadership(func(k string) string {
		switch k {
		case VarLeadershipFile:
			return "  /run/zapgw/lider  " // heredoc whitespace must not break it
		case VarLeadershipValidity:
			return "7s"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("NewLeadership: %v", err)
	}
	if !l.Armed() {
		t.Fatal("com arquivo configurado a guarda tem de ficar ARMADA")
	}
	if l.file != "/run/zapgw/lider" {
		t.Errorf("arquivo = %q, queria sem espaco nas pontas", l.file)
	}
	if l.validity != 7*time.Second {
		t.Errorf("validade = %v, queria 7s", l.validity)
	}
}

func TestLeadershipSuppressesRepeatedRefusalLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nao-existe")
	var rows int
	var mu sync.Mutex
	l := &Leadership{
		file:     path,
		validity: 15 * time.Second,
		logf: func(string, ...any) {
			mu.Lock()
			rows++
			mu.Unlock()
		},
	}

	stored := l.Require(markingHandler(new(bool)))
	for i := 0; i < 20; i++ {
		stored.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", nil))
	}

	mu.Lock()
	defer mu.Unlock()
	if rows != 1 {
		t.Fatalf("20 recusas produziram %d linhas de log; queria 1 — sob lideranca perdida TODA requisicao recusa, e o log repetido esconde o resto do journal", rows)
	}
}

// Guard against a concurrency defect: the suite's `-race` needs to exercise
// Require in parallel, which is how it actually runs (an HTTP handler).
func TestLeadershipIsSafeUnderConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nao-existe")
	l := &Leadership{file: path, validity: 15 * time.Second, logf: func(string, ...any) {}}
	stored := l.Require(markingHandler(new(bool)))

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", nil))
		}()
	}
	wg.Wait()
}
