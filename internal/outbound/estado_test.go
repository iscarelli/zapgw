// Tests of the state's PRESENTATION — what the screen says about the same
// data the route serializes. The content itself is tested by
// estado_handler_test.go (through the route) and by cmd/zapgw/estado_test.go
// (through the command).
package outbound

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// withPrintClock stops the presentation clock at the requested
// instant and restores it to normal at the end of the test. Without this,
// "is the distance correct?" would be a question about the machine's clock.
func withPrintClock(t *testing.T, when time.Time) {
	t.Helper()
	previous := printClock
	printClock = func() time.Time { return when }
	t.Cleanup(func() { printClock = previous })
}

// rowValue returns the value of the line labeled `label`. It fails
// loudly if the line doesn't exist: a test that compares empty string with
// empty string passes green over a screen that printed nothing.
func rowValue(t *testing.T, rows []StateRow, label string) string {
	t.Helper()
	for _, l := range rows {
		if l.Label == label {
			return l.Value
		}
	}
	t.Fatalf("a linha %q nao saiu na apresentacao: %+v", label, rows)
	return ""
}

// T-072'S RULE: an OBSERVATION timestamp never comes out in the future.
//
// Each timestamp's distance is measured against the now of the PRINTING, not
// against `gerado_em`. `zapgw estado` measures the token on the Graph API
// AFTER stamping `gerado_em` (the watchdog's cache lives in the server's
// process), so `medido_em` is legitimately later than the snapshot — and the
// screen used to print "in 1s" about a fact that had already happened, a
// number that GROWS with how slow Meta is, which is exactly when someone is
// reading this screen.
//
// THE ASSERTION IS ABOUT THE PRINTED TEXT, not about the calculated
// duration, because it was the TEXT that misled the reader.
func TestStateRowsMeasureTheDistanceAgainstThePrintNowNotAgainstGeneratedAt(t *testing.T) {
	portrait := time.Date(2026, 7, 28, 18, 22, 31, 0, time.UTC)
	// The screen is printed 5s after the snapshot: that's the time the
	// CLI spent talking to the Graph API before printing.
	withPrintClock(t, portrait.Add(5*time.Second))

	e := State{
		Instance:    "lojinha",
		State:       "ativa",
		Version:     testVersion,
		GeneratedAt: portrait.Format(time.RFC3339),
		MetaToken: MetaToken{
			Verdict: VerdictOK,
			// Both of the token's timestamps happen AFTER the snapshot — it's
			// the real case, not a made-up scenario.
			MeasuredAt: stamp(portrait.Add(1 * time.Second)),
			CheckedAt:  stamp(portrait.Add(2 * time.Second)),
		},
		CallbackCertificate: CertificateInState{
			State: CertObserved,
			// 54 days and one hour: the "+1h" just moves it off the exact day
			// boundary, so the test is about the reference clock, not
			// rounding.
			ExpiresAt:  stamp(portrait.Add(54*24*time.Hour + time.Hour)),
			ObservedAt: stamp(portrait.Add(3 * time.Second)),
		},
	}

	rows := StateRows(e)

	// Every OBSERVATION timestamp comes out in the past, with the distance
	// counted from the printing.
	for _, c := range []struct{ label, want string }{
		{"gerado_em", "(ha 5s)"},
		{"medido_em", "(ha 4s)"},
		{"conferido_em", "(ha 3s)"},
		{"observado_em", "(ha 2s)"},
	} {
		v := rowValue(t, rows, c.label)
		if strings.Contains(v, "daqui a") {
			t.Errorf("%s = %q — carimbo de OBSERVACAO anunciado como futuro; o referencial e que esta errado",
				c.label, v)
		}
		if !strings.HasSuffix(v, c.want) {
			t.Errorf("%s = %q, quero terminando em %q", c.label, v, c.want)
		}
	}

	// And the counter-test, which is what prevents the wrong fix ("if it
	// comes out future, print 0s ago"): a genuinely future timestamp KEEPS
	// coming out as future.
	if v := rowValue(t, rows, "expira_em"); !strings.HasSuffix(v, "(daqui a 54d)") {
		t.Errorf("expira_em = %q, quero terminando em \"(daqui a 54d)\" — o certificado vence mesmo no futuro", v)
	}
}

// T-070 (1): every item of `serie_7_dias` carries both `dia` AND `dia_utc`,
// with the SAME value.
//
// THE ASSERTION IS ABOUT THE RESPONSE'S BYTES, not about the struct: it's
// the field name on the wire that the consumer reads, and a wrong JSON tag
// doesn't show up in any read done through the struct — this project has
// already paid that bill with `eventos: null`, an envelope the whole suite
// produced and nobody ever looked at serialized.
func TestSeries7DaysBringsDayAndDayUTCWithTheSameValue(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	rec := askState(t, h, "token-do-a", "lojinha")
	if rec.Code != 200 {
		t.Fatalf("status = %d, quero 200; corpo = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	today := time.Now().UTC().Format("2006-01-02")
	for _, key := range []string{`"dia":"` + today + `"`, `"dia_utc":"` + today + `"`} {
		if !strings.Contains(body, key) {
			t.Errorf("a resposta nao traz %s. corpo:\n%s", key, body)
		}
	}

	// And both hold for ALL seven days, pair by pair — a `dia_utc` only on
	// today would be worse than none, because the consumer would migrate and
	// lose the other six with no error at all.
	var r struct {
		Series []struct {
			Day    string `json:"dia"`
			DayUTC string `json:"dia_utc"`
		} `json:"serie_7_dias"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("corpo nao desserializa: %v", err)
	}
	if len(r.Series) != 7 {
		t.Fatalf("serie_7_dias tem %d entradas, quero 7", len(r.Series))
	}
	for i, d := range r.Series {
		if d.DayUTC == "" {
			t.Errorf("serie_7_dias[%d] veio sem `dia_utc` — o nome novo tem de valer para a serie inteira", i)
		}
		if d.Day != d.DayUTC {
			t.Errorf("serie_7_dias[%d]: dia = %q e dia_utc = %q — os dois sao o MESMO dado", i, d.Day, d.DayUTC)
		}
	}
}

// T-070 (2): `carimbos_desde` travels at the top of the response, with a
// real value.
//
// It answers "since when does this instance record timestamps?" — without
// it, `ultimo_em: null` stays ambiguous between "never happened" and
// "happened before the timestamp existed", and every consumer hardcodes the
// v0.23.0 date.
func TestStatePublishesStampsSinceAsARealDate(t *testing.T) {
	m := tokenAcceptingMeta()
	h, _, _ := testState(t, m, "lojinha")

	rec := askState(t, h, "token-do-a", "lojinha")
	r := readState(t, rec)

	if r.StampsSince == "" {
		t.Fatalf("carimbos_desde veio vazio — o campo existe para nao deixar o consumidor adivinhar. corpo:\n%s",
			rec.Body.String())
	}
	if _, err := time.Parse(time.RFC3339, r.StampsSince); err != nil {
		t.Errorf("carimbos_desde = %q, que nao e RFC3339: %v", r.StampsSince, err)
	}
	if !strings.Contains(rec.Body.String(), `"carimbos_desde":"`+r.StampsSince+`"`) {
		t.Errorf("o campo nao saiu com o nome `carimbos_desde` nos BYTES. corpo:\n%s", rec.Body.String())
	}
}

// THE SAME rule on the counters TABLE, which doesn't go through reflection:
// a key's `ultimo_em` is an observation timestamp just like the ones above,
// and the two halves of the same screen cannot measure distance against
// different reference clocks.
func TestReadableStampMeasuresAgainstThePrintNow(t *testing.T) {
	printed := time.Date(2026, 7, 28, 18, 22, 36, 0, time.UTC)
	withPrintClock(t, printed)

	c := stamp(printed.Add(-90 * time.Second))
	if want, has := "(ha 1min)", ReadableStamp(c); !strings.HasSuffix(has, want) {
		t.Errorf("ReadableStamp = %q, quero terminando em %q", has, want)
	}
	if v := ReadableStamp(nil); v != NoValue {
		t.Errorf("ReadableStamp(nil) = %q, quero %q", v, NoValue)
	}
}
