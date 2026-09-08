package meta

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// THE CLASSIFICATION IS, FIRST, STRUCTURAL — derived from the HTTP STATUS.
// The cases below use code 131047 (outside T-142's throttling table) or a
// `{}` body with no `code` at all, so none of them triggers the second
// chance by code: what's proven here is the pure status mapping.
//
// This project's rule forbids a magic constant with an unverified Meta
// number. The throttling table (retryableCodesByNature) is no
// exception: every number was checked against the official source (T-142),
// and even so it only PROMOTES — it never decides on its own nor demotes
// what the status already decided (see
// TestClassificarCodigoDeThrottlingPromoveApenasOPermanente below).
func TestClassifyUsesTheHTTPStatusAsTheBase(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorClass
		why    string
	}{
		{http.StatusInternalServerError, ClassRetryable, "5xx from Meta: the problem is theirs, retrying resolves it"},
		{http.StatusBadGateway, ClassRetryable, "idem"},
		{http.StatusServiceUnavailable, ClassRetryable, "idem"},
		{http.StatusGatewayTimeout, ClassRetryable, "idem"},
		{http.StatusTooManyRequests, ClassRetryable, "rate limit: waiting and retrying IS the solution"},
		{http.StatusUnauthorized, ClassConfig, "wrong or expired token: only a person fixes it"},
		{http.StatusForbidden, ClassConfig, "no permission: only a person fixes it"},
		{http.StatusBadRequest, ClassPermanent, "wrong body: resending repeats the same error"},
		{http.StatusNotFound, ClassPermanent, "nonexistent resource"},
		{http.StatusUnprocessableEntity, ClassPermanent, "idem"},
	}

	for _, c := range cases {
		e := ClassifyResponse(c.status, []byte(`{}`))
		if e == nil {
			t.Fatalf("status %d did not produce an error", c.status)
		}
		if e.Class != c.want {
			t.Errorf("status %d -> %q, want %q (%s)", c.status, e.Class, c.want, c.why)
		}
	}
}

func TestClassifyDoesNotInventAnErrorForSuccess(t *testing.T) {
	for _, status := range []int{200, 201, 202, 204} {
		if e := ClassifyResponse(status, []byte(`{}`)); e != nil {
			t.Errorf("status %d produced error %v", status, e)
		}
	}
}

func TestClassifyExtractsMetasCodeWithoutDependingOnIt(t *testing.T) {
	// The code TRAVELS to the consumer (they can have their own rule), but
	// it does NOT decide the class — deciding by a number not verified
	// against the source is exactly what the project rule forbids.
	body := []byte(`{"error":{"message":"algo","type":"OAuthException","code":131047}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e == nil {
		t.Fatal("did not produce an error")
	}
	if e.MetaCode != 131047 {
		t.Errorf("MetaCode = %d, want 131047", e.MetaCode)
	}
	if e.Class != ClassPermanent {
		t.Errorf("Class = %q — the class comes from status 400, not the code", e.Class)
	}
}

func TestClassifyWithstandsABodyThatDoesNotHelp(t *testing.T) {
	// Meta can return an empty body, proxy HTML, or JSON without `error`.
	// None of that can bring the gateway down or erase the classification.
	cases := [][]byte{
		nil, {}, []byte(`not json`), []byte(`null`), []byte(`[]`),
		[]byte(`{"error":"texto em vez de objeto"}`),
		[]byte(`{"error":{"code":"131047"}}`), // code as a STRING
		[]byte(`<html>502 Bad Gateway</html>`),
	}

	for _, body := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic with body %q: %v", body, r)
				}
			}()
			e := ClassifyResponse(http.StatusBadGateway, body)
			if e == nil {
				t.Fatalf("body %q erased the classification", body)
			}
			if e.Class != ClassRetryable {
				t.Errorf("body %q changed the class to %q", body, e.Class)
			}
		}()
	}
}

func TestMetaErrorDoesNotLeakTheBodyIntoTheMessage(t *testing.T) {
	// Meta's error body can echo the payload sent — which carries a phone
	// number and message text. The error message goes up to the log and
	// into the response to the consumer; neither one is a place for
	// personal data.
	body := []byte(`{"error":{"message":"Invalid parameter","code":100,` +
		`"error_data":{"details":"to=5511999990000 body=segredo do cliente"}}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	text := e.Error()

	for _, forbidden := range []string{"5511999990000", "segredo do cliente", "error_data"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("the error message leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "Invalid parameter") {
		t.Errorf("Meta's useful message was lost: %s", text)
	}
}

func TestAnUnknownClassNeverBecomesRetryable(t *testing.T) {
	// 1xx/3xx in a Graph API response is an anomaly, not a transient
	// failure. Treating it as retryable would produce endless resending of
	// something that's never going to work.
	for _, status := range []int{100, 301, 302, 399} {
		e := ClassifyResponse(status, []byte(`{}`))
		if e == nil {
			t.Fatalf("status %d did not produce an error", status)
		}
		if e.Class == ClassRetryable {
			t.Errorf("status %d classified as retryable", status)
		}
	}
}

// MINOR found in the T3 review, and it's the parser's Critical lesson in a
// new place: json.Unmarshal fails ATOMICALLY, so a field with an unexpected
// type zeroed the whole envelope and took the useful message down with it —
// which is exactly what a human reads to diagnose.
func TestClassifyKeepsTheMessageWhenTheCodeIsMalformed(t *testing.T) {
	cases := []string{
		`{"error":{"message":"seria util","code":"131047"}}`,
		`{"error":{"message":"seria util","code":null}}`,
		`{"error":{"message":"seria util","code":{"aninhado":1}}}`,
		`{"error":{"message":"seria util","code":[1,2]}}`,
	}

	for _, body := range cases {
		e := ClassifyResponse(http.StatusBadRequest, []byte(body))
		if e == nil {
			t.Fatalf("body %s did not produce an error", body)
		}
		if e.Message != "seria util" {
			t.Errorf("body %s: Message = %q — the bad field took the good one down with it", body, e.Message)
		}
	}
}

func TestClassifyReadsTheCodeAsANumberOrAsAString(t *testing.T) {
	number := ClassifyResponse(http.StatusBadRequest, []byte(`{"error":{"code":131047}}`))
	text := ClassifyResponse(http.StatusBadRequest, []byte(`{"error":{"code":"131047"}}`))

	if number.MetaCode != 131047 {
		t.Errorf("code as a number = %d", number.MetaCode)
	}
	if text.MetaCode != 131047 {
		t.Errorf("code as a string = %d", text.MetaCode)
	}
}

func TestClassifyKeepsTheCodeWhenTheMessageIsMalformed(t *testing.T) {
	// The symmetry of the first test: the bad field on the other side also
	// cannot take the good one down.
	e := ClassifyResponse(http.StatusBadRequest, []byte(`{"error":{"message":{"x":1},"code":100}}`))

	if e.MetaCode != 100 {
		t.Errorf("MetaCode = %d — the malformed message took the code down with it", e.MetaCode)
	}
}

func TestClassifyTreatsTimeoutAndTooEarlyAsRetryable(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly} {
		e := ClassifyResponse(status, []byte(`{}`))
		if e.Class != ClassRetryable {
			t.Errorf("status %d -> %q, want retryable — they are 'try again' by HTTP's own definition",
				status, e.Class)
		}
	}
}

// T-142: the heart of the task. Meta doesn't document which status the
// throttling family arrives with — the Marketing Messages API shows an error
// of the same shape as 400. If the code didn't promote, a throttling error
// wrapped in a 400 would fall into the default (ClassPermanent) and the
// consumer would stop retrying.
func TestClassifyAThrottlingCodePromotesPermanentToRetryable(t *testing.T) {
	body := []byte(`{"error":{"message":"limite de taxa","code":130429}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e == nil {
		t.Fatal("did not produce an error")
	}
	if e.Class != ClassRetryable {
		t.Errorf("status 400 + code 130429 -> %q, want ClassRetryable", e.Class)
	}
}

// A code OUTSIDE the throttling table doesn't promote anything: the default
// stays ClassPermanent. The table is conservative by design.
func TestClassifyAnUnknownCodeDoesNotPromote(t *testing.T) {
	body := []byte(`{"error":{"message":"algo","code":999999}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e == nil {
		t.Fatal("did not produce an error")
	}
	if e.Class != ClassPermanent {
		t.Errorf("status 400 + unknown code -> %q, want ClassPermanent", e.Class)
	}
}

// The promotion is ONLY UPWARD: a 401 with a throttling code stays
// ClassConfig. The table is a second chance for the default, not a second
// judge that overrides what the status has already decided with confidence.
func TestClassifyAThrottlingCodeDoesNotDowngradeClassConfig(t *testing.T) {
	body := []byte(`{"error":{"message":"token invalido","code":130429}}`)

	e := ClassifyResponse(http.StatusUnauthorized, body)
	if e == nil {
		t.Fatal("did not produce an error")
	}
	if e.Class != ClassConfig {
		t.Errorf("status 401 + code 130429 -> %q, want ClassConfig (the code does NOT downgrade)", e.Class)
	}
}

// The five codes in the table, one by one, all with status 400: all five
// have to promote to ClassRetryable.
func TestClassifyTheFiveThrottlingCodesPromote(t *testing.T) {
	for code := range retryableCodesByNature {
		body := []byte(fmt.Sprintf(`{"error":{"message":"limite","code":%d}}`, code))

		e := ClassifyResponse(http.StatusBadRequest, body)
		if e == nil {
			t.Fatalf("code %d: did not produce an error", code)
		}
		if e.Class != ClassRetryable {
			t.Errorf("code %d: status 400 -> %q, want ClassRetryable", code, e.Class)
		}
	}
}

// Non-regression: a body without `code` still classifies by status alone,
// like before this task.
func TestClassifyWithoutACodeUsesOnlyTheStatus(t *testing.T) {
	e := ClassifyResponse(http.StatusBadRequest, []byte(`{"error":{"message":"sem codigo"}}`))
	if e == nil {
		t.Fatal("did not produce an error")
	}
	if e.Class != ClassPermanent {
		t.Errorf("status 400 without a code -> %q, want ClassPermanent", e.Class)
	}
}

// T-141: today the gateway ONLY reads message and code from Meta's error.
// error_data can name the field and the ceiling it rejected (see
// docs/ARMADILHAS.md, the case of code 131009 found through another
// transport on 2026-07-18) — this test proves that Detail is filled when
// error_data.details comes in the SYNTHETIC body below. It does NOT prove
// that Meta still sends this field through today's path; that's a
// production measurement, outside this suite's reach.
func TestClassifyFillsDetailWhenErrorDataDetailsComes(t *testing.T) {
	body := []byte(`{"error":{"message":"Parameter value is not valid","code":131009,` +
		`"error_data":{"details":"Button title length invalid. Min length: 1, Max length: 20"}}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e == nil {
		t.Fatal("did not produce an error")
	}
	want := "Button title length invalid. Min length: 1, Max length: 20"
	if e.Detail != want {
		t.Errorf("Detail = %q, want %q", e.Detail, want)
	}
	// The message and the code still come through, intact — the new field
	// cannot take down the two that already existed.
	if e.Message != "Parameter value is not valid" {
		t.Errorf("Message = %q — reading error_data took the message down with it", e.Message)
	}
	if e.MetaCode != 131009 {
		t.Errorf("MetaCode = %d — reading error_data took the code down with it", e.MetaCode)
	}
}

// The absence of error_data is TODAY's case (non-regression): Detail stays
// empty, and Message/MetaCode keep working exactly as before this task.
func TestClassifyWithoutErrorDataLeavesDetailEmpty(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid parameter","code":100}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e.Detail != "" {
		t.Errorf("Detail = %q, want empty (body without error_data)", e.Detail)
	}
	if e.Message != "Invalid parameter" || e.MetaCode != 100 {
		t.Errorf("Message/MetaCode changed without error_data: %q / %d", e.Message, e.MetaCode)
	}
}

// error_data present without `details`, `error_data: null`, and
// `error_data` of the WRONG type (string) — all three have to return an
// empty Detail WITHOUT bringing down Message or MetaCode. It's the SAME
// discipline as the test above for message/code, applied to the new field.
func TestClassifyToleratesMalformedErrorData(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no details", `{"error":{"message":"algo","code":1,"error_data":{"blocked_reason":"x"}}}`},
		{"error_data null", `{"error":{"message":"algo","code":1,"error_data":null}}`},
		{"error_data string", `{"error":{"message":"algo","code":1,"error_data":"nao e objeto"}}`},
	}

	for _, c := range cases {
		e := ClassifyResponse(http.StatusBadRequest, []byte(c.body))
		if e == nil {
			t.Fatalf("%s: did not produce an error", c.name)
		}
		if e.Detail != "" {
			t.Errorf("%s: Detail = %q, want empty", c.name, e.Detail)
		}
		if e.Message != "algo" {
			t.Errorf("%s: Message = %q — malformed error_data took the message down with it", c.name, e.Message)
		}
		if e.MetaCode != 1 {
			t.Errorf("%s: MetaCode = %d — malformed error_data took the code down with it", c.name, e.MetaCode)
		}
	}
}

// Ceiling of 500 RUNES (not bytes), with the truncation suffix — an
// unbounded error_data.details cannot grow without limit inside our
// response.
func TestClassifyTruncatesDetailAt500Runes(t *testing.T) {
	// "á" is 1 rune / 2 bytes: guarantees the count is by rune, not by byte.
	long := strings.Repeat("á", 600)
	body := []byte(`{"error":{"message":"algo","code":1,"error_data":{"details":"` + long + `"}}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)

	suffix := " …[truncado]"
	if !strings.HasSuffix(e.Detail, suffix) {
		t.Fatalf("Detail does not end with the truncation suffix: %q", e.Detail)
	}
	truncatedBody := strings.TrimSuffix(e.Detail, suffix)
	if runes := len([]rune(truncatedBody)); runes != 500 {
		t.Errorf("the part before the suffix has %d runes, want 500", runes)
	}
}

func TestClassifyDoesNotTruncateADetailWithinTheCap(t *testing.T) {
	body := []byte(`{"error":{"message":"algo","code":1,"error_data":{"details":"curto"}}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e.Detail != "curto" {
		t.Errorf("Detail = %q, want %q with no truncation", e.Detail, "curto")
	}
}

// T-153: the heart of the task. The consumer hit a deterministic 503 (meta
// 2) WITHOUT error_data and asked, in writing, for Meta's RAW response
// beyond message/code. This test proves that the four new fields —
// error_subcode, error_user_title+error_user_msg (become Explanation), and
// fbtrace_id — are read from a SYNTHETIC body in the Graph API's documented
// format, and that message/code stay intact.
func TestClassifyReadsSubcodeExplanationAndTrace(t *testing.T) {
	body := []byte(`{"error":{"message":"An unknown error has occurred","type":"OAuthException","code":2,` +
		`"error_subcode":2494055,` +
		`"error_user_title":"Erro temporario",` +
		`"error_user_msg":"Tente novamente em alguns instantes",` +
		`"fbtrace_id":"AbCdEfGhIjKlMnOp"}}`)

	e := ClassifyResponse(http.StatusServiceUnavailable, body)
	if e == nil {
		t.Fatal("did not produce an error")
	}
	if e.Subcode != 2494055 {
		t.Errorf("Subcode = %d, want 2494055", e.Subcode)
	}
	if e.Trace != "AbCdEfGhIjKlMnOp" {
		t.Errorf("Trace = %q, want %q", e.Trace, "AbCdEfGhIjKlMnOp")
	}
	want := "Erro temporario: Tente novamente em alguns instantes"
	if e.Explanation != want {
		t.Errorf("Explanation = %q, want %q", e.Explanation, want)
	}
	if e.Message != "An unknown error has occurred" {
		t.Errorf("Message = %q — the new fields took the message down with them", e.Message)
	}
	if e.MetaCode != 2 {
		t.Errorf("MetaCode = %d — the new fields took the code down with them", e.MetaCode)
	}
}

// Without error_user_title, Explanation is JUST error_user_msg — the
// "titulo:" prefix only appears when there's a real title.
func TestClassifyAnExplanationWithoutATitleGetsNoPrefix(t *testing.T) {
	body := []byte(`{"error":{"message":"algo","code":1,"error_user_msg":"tente de novo mais tarde"}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e.Explanation != "tente de novo mais tarde" {
		t.Errorf("Explanation = %q, want it without a prefix", e.Explanation)
	}
}

// Non-regression: without any of the four new keys, Subcode/Trace/
// Explanation stay at the zero value, and Message/MetaCode stay as they
// were before this task.
func TestClassifyWithoutTheNewFieldsLeavesTheThreeEmpty(t *testing.T) {
	body := []byte(`{"error":{"message":"parametro invalido","code":100}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e.Subcode != 0 {
		t.Errorf("Subcode = %d, want 0", e.Subcode)
	}
	if e.Explanation != "" {
		t.Errorf("Explanation = %q, want empty", e.Explanation)
	}
	if e.Trace != "" {
		t.Errorf("Trace = %q, want empty", e.Trace)
	}
	if e.Message != "parametro invalido" || e.MetaCode != 100 {
		t.Errorf("Message/MetaCode changed without the new fields: %q / %d", e.Message, e.MetaCode)
	}
}

// error_subcode as a STRING (the same tolerance code already has, via
// tolerantInt) has to work the same way.
func TestClassifyReadsSubcodeAsAString(t *testing.T) {
	body := []byte(`{"error":{"message":"algo","code":1,"error_subcode":"2494055"}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)
	if e.Subcode != 2494055 {
		t.Errorf("Subcode = %d, want 2494055 (read as a string)", e.Subcode)
	}
}

// Each of the four new fields, malformed, CANNOT erase the others or
// message/code — the SAME discipline the error_data test already proves for
// Detail (T-141), applied to T-153's fields.
func TestClassifyToleratesMalformedNewFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"nested error_subcode", `{"error":{"message":"algo","code":1,"error_subcode":{"x":1}}}`},
		{"nested error_user_title", `{"error":{"message":"algo","code":1,"error_user_title":{"x":1},"error_user_msg":"msg"}}`},
		{"nested error_user_msg", `{"error":{"message":"algo","code":1,"error_user_msg":{"x":1}}}`},
		{"nested fbtrace_id", `{"error":{"message":"algo","code":1,"fbtrace_id":{"x":1}}}`},
	}

	for _, c := range cases {
		e := ClassifyResponse(http.StatusBadRequest, []byte(c.body))
		if e == nil {
			t.Fatalf("%s: did not produce an error", c.name)
		}
		if e.Message != "algo" {
			t.Errorf("%s: Message = %q — the malformed new field took the message down with it", c.name, e.Message)
		}
		if e.MetaCode != 1 {
			t.Errorf("%s: MetaCode = %d — the malformed new field took the code down with it", c.name, e.MetaCode)
		}
	}
	// The nested-title case still has to let error_user_msg through — it's
	// the proof that a bad title doesn't erase the good message.
	e := ClassifyResponse(http.StatusBadRequest,
		[]byte(`{"error":{"message":"algo","code":1,"error_user_title":{"x":1},"error_user_msg":"msg boa"}}`))
	if e.Explanation != "msg boa" {
		t.Errorf("Explanation = %q, want %q — a malformed title must not erase the good message", e.Explanation, "msg boa")
	}
}

// Explanation truncates at 500 RUNES, at the SAME ceiling as Detail —
// third-party string doesn't come in without a limit.
func TestClassifyTruncatesExplanationAt500Runes(t *testing.T) {
	long := strings.Repeat("á", 600)
	body := []byte(`{"error":{"message":"algo","code":1,"error_user_msg":"` + long + `"}}`)

	e := ClassifyResponse(http.StatusBadRequest, body)

	suffix := " …[truncado]"
	if !strings.HasSuffix(e.Explanation, suffix) {
		t.Fatalf("Explanation does not end with the truncation suffix: %q", e.Explanation)
	}
	truncatedBody := strings.TrimSuffix(e.Explanation, suffix)
	if runes := len([]rune(truncatedBody)); runes != 500 {
		t.Errorf("the part before the suffix has %d runes, want 500", runes)
	}
}
