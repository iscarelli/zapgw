// Tests for the send singleton guard (leadership.go).
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
	"bytes"
	"encoding/json"
	"fmt"
	"log"
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
		t.Fatalf("create lease file: %v", err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("age lease file: %v", err)
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
		t.Fatal("without ZAPGW_LIDERANCA_ARQUIVO the guard has to stay DISARMED — it is the single-node install")
	}
	if ok, reason := l.Holder(); !ok {
		t.Fatalf("a disarmed guard has to answer titular=true; got false (%s)", reason)
	}

	var called bool
	internal := markingHandler(&called)
	// Disarmed, Require returns the handler ITSELF: there is nothing to check,
	// and wrapping it would only add work on the critical path.
	if got := l.Require(internal); fmt.Sprintf("%p", got) != fmt.Sprintf("%p", internal) {
		t.Error("disarmed, Require should return the original handler without wrapping")
	}

	w := httptest.NewRecorder()
	l.Require(internal).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if !called {
		t.Fatal("a disarmed guard blocked the send — this would break every single-node install")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, wanted 200", w.Code)
	}
}

func TestLeadershipWithFreshLeaseLetsTheSendThrough(t *testing.T) {
	path := leaseFile(t, 1*time.Second)
	l := &Leadership{file: path, validity: 15 * time.Second}

	if ok, reason := l.Holder(); !ok {
		t.Fatalf("a 1s lease with a 15s limit has to hold; refused: %s", reason)
	}

	var called bool
	w := httptest.NewRecorder()
	l.Require(markingHandler(&called)).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if !called || w.Code != http.StatusOK {
		t.Fatalf("the legitimate holder was blocked: called=%v status=%d", called, w.Code)
	}
}

// 🔴 The case that justifies the whole file: a stale lease does NOT send.
func TestLeadershipWithSTALELeaseRefusesAsRetryable(t *testing.T) {
	path := leaseFile(t, 90*time.Second)
	l := &Leadership{file: path, validity: 15 * time.Second, logf: func(string, ...any) {}}

	ok, reason := l.Holder()
	if ok {
		t.Fatal("a 90s lease with a 15s limit CANNOT hold — this is the standby that came up and was never promoted")
	}
	if reason == "" {
		t.Error("the refusal has to say WHY; an empty reason makes someone restart the service looking for a defect that does not exist")
	}

	var called bool
	w := httptest.NewRecorder()
	l.Require(markingHandler(&called)).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if called {
		t.Fatal("the send handler was called without leadership — this is exactly the duplicate message the guard exists to prevent")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, wanted 503: the consumer needs to RETRY, not give up", w.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not error JSON: %v", err)
	}
	if body.Error.Class != "retryable" {
		t.Errorf("class = %q, wanted \"retryable\" — a 4xx would make the consumer GIVE UP on a message that only needed another destination", body.Error.Class)
	}
}

// 🔴 Fail closed: not being able to VERIFY is not "everything is fine".
func TestLeadershipWithoutFileRefusesInsteadOfAssumingAllIsWell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	l := &Leadership{file: path, validity: 15 * time.Second, logf: func(string, ...any) {}}

	ok, reason := l.Holder()
	if ok {
		t.Fatal("a MISSING lease file has to refuse — 'nao consegui verificar' can never turn into 'can send'")
	}
	if reason == "" {
		t.Error("the refusal for absence has to say which path was missing")
	}

	var called bool
	w := httptest.NewRecorder()
	l.Require(markingHandler(&called)).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if called || w.Code != http.StatusServiceUnavailable {
		t.Fatalf("absence of a lease let it through: called=%v status=%d", called, w.Code)
	}
}

func TestNewLeadershipRefusesToStartWithUnreadableOrNonPositiveValidity(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"text that is not a duration", "quinze segundos"},
		{"number without a unit", "15"},
		{"zero", "0s"},
		{"negative", "-5s"},
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
				t.Fatalf("%s = %q had to BRING DOWN startup; an unreadable value turning into a silent default "+
					"only shows up on failover day",
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
		t.Fatal("an ARMED guard without ZAPGW_LIDERANCA_VALIDADE had to BRING DOWN startup: any default is a guess about someone else's TTL, and the wrong guess overlaps two holders")
	}
	for _, required := range []string{"V + A < T", "duplicada"} {
		if !strings.Contains(err.Error(), required) {
			t.Errorf("the message has to carry %q — whoever arms it needs the formula, not a \"missing variable\"; got: %v", required, err)
		}
	}
}

// Disarmed does NOT require validity: a single-node install cannot be forced
// to configure a pair that doesn't exist.
func TestNewLeadershipDisarmedDoesNotRequireValidity(t *testing.T) {
	l, err := NewLeadership(func(string) string { return "" })
	if err != nil {
		t.Fatalf("disarmed cannot require validity: %v", err)
	}
	if l.Armed() {
		t.Fatal("without a file the guard has to stay disarmed")
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
		t.Fatal("with a configured file the guard has to be ARMED")
	}
	if l.file != "/run/zapgw/lider" {
		t.Errorf("file = %q, wanted no whitespace at the ends", l.file)
	}
	if l.validity != 7*time.Second {
		t.Errorf("validity = %v, wanted 7s", l.validity)
	}
}

// TestNewLeadershipAcceptsTheNewNamesAndTheyWin is T-214's Verify for
// ZAPGW_LEADERSHIP_FILE/ZAPGW_LIDERANCA_ARQUIVO and
// ZAPGW_LEADERSHIP_VALIDITY/ZAPGW_LIDERANCA_VALIDADE — each pair resolved
// INDEPENDENTLY (one can come from the old name while the other comes from
// the new one).
func TestNewLeadershipAcceptsTheNewNamesAndTheyWin(t *testing.T) {
	cases := []struct {
		name         string
		vars         map[string]string
		wantFile     string
		wantValidity time.Duration
	}{
		{
			"both new",
			map[string]string{VarLeadershipFileNew: "/run/novo/lider", VarLeadershipValidityNew: "9s"},
			"/run/novo/lider", 9 * time.Second,
		},
		{
			"both old",
			map[string]string{VarLeadershipFile: "/run/velho/lider", VarLeadershipValidity: "9s"},
			"/run/velho/lider", 9 * time.Second,
		},
		{
			"new file, old validity: each wins on its own",
			map[string]string{VarLeadershipFileNew: "/run/novo/lider", VarLeadershipValidity: "9s"},
			"/run/novo/lider", 9 * time.Second,
		},
		{
			"both present in each pair: the NEW one wins on both",
			map[string]string{
				VarLeadershipFileNew: "/run/novo/lider", VarLeadershipFile: "/run/velho/lider",
				VarLeadershipValidityNew: "9s", VarLeadershipValidity: "99s",
			},
			"/run/novo/lider", 9 * time.Second,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l, err := NewLeadership(func(k string) string { return c.vars[k] })
			if err != nil {
				t.Fatalf("NewLeadership: %v", err)
			}
			if l.file != c.wantFile {
				t.Errorf("file = %q, want %q", l.file, c.wantFile)
			}
			if l.validity != c.wantValidity {
				t.Errorf("validity = %v, want %v", l.validity, c.wantValidity)
			}
		})
	}
}

// TestNewLeadershipWarnsOnlyWhenOldNamesWin is T-214 Do item 3, checked
// independently for each of the two variables.
func TestNewLeadershipWarnsOnlyWhenOldNamesWin(t *testing.T) {
	cases := []struct {
		name                           string
		vars                           map[string]string
		wantFileWarn, wantValidityWarn bool
	}{
		{
			"both new: silent",
			map[string]string{VarLeadershipFileNew: "/run/lider", VarLeadershipValidityNew: "9s"},
			false, false,
		},
		{
			"both old: both warn",
			map[string]string{VarLeadershipFile: "/run/lider", VarLeadershipValidity: "9s"},
			true, true,
		},
		{
			"only the file is old",
			map[string]string{VarLeadershipFile: "/run/lider", VarLeadershipValidityNew: "9s"},
			true, false,
		},
	}
	original := log.Writer()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			if _, err := NewLeadership(func(k string) string { return c.vars[k] }); err != nil {
				log.SetOutput(original)
				t.Fatalf("NewLeadership: %v", err)
			}
			log.SetOutput(original)
			text := buf.String()
			fileWarned := strings.Contains(text, VarLeadershipFile) && strings.Contains(text, "obsoleta")
			validityWarned := strings.Contains(text, VarLeadershipValidity) && strings.Contains(text, "obsoleta")
			if fileWarned != c.wantFileWarn {
				t.Errorf("file warning = %v (log: %q), want %v", fileWarned, text, c.wantFileWarn)
			}
			if validityWarned != c.wantValidityWarn {
				t.Errorf("validity warning = %v (log: %q), want %v", validityWarned, text, c.wantValidityWarn)
			}
		})
	}
}

func TestLeadershipSuppressesRepeatedRefusalLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
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
		t.Fatalf("20 refusals produced %d log lines; wanted 1 — under lost leadership EVERY request refuses, and a repeated log hides the rest of the journal", rows)
	}
}

// Guard against a concurrency defect: the suite's `-race` needs to exercise
// Require in parallel, which is how it actually runs (an HTTP handler).
func TestLeadershipIsSafeUnderConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
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
