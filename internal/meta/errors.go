// Graph API error taxonomy, in THREE classes the consumer understands
// without knowing a single Meta code.
//
// THE CLASSIFICATION IS, FIRST, STRUCTURAL: it comes from the HTTP STATUS
// (classOfStatus). AFTER that, an OUR OWN table of throttling codes
// (retryableCodesByNature, below) gets a second chance — but it can
// only PROMOTE a status that fell into the default (ClassPermanent) to
// ClassRetryable. It never demotes: ClassConfig (401/403) and
// ClassRetryable already decided by status (5xx, 429, 408, 425) come out
// untouched, even if they carry a code from the table. See the table's
// comment for why this second chance exists, and classOfStatus for why it
// never demotes.
//
// Every number in the table was checked against the official source (T-142)
// — it's not a magic constant we made up.
package meta

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type ErrorClass string

const (
	// ClassRetryable: retrying later might work.
	ClassRetryable ErrorClass = "retentavel"
	// ClassPermanent: retrying repeats the same error. Give up.
	ClassPermanent ErrorClass = "permanente"
	// ClassConfig: credential or permission. Only a person fixes it.
	ClassConfig ErrorClass = "config"
	// ClassUnknown: the gateway doesn't know whether Meta created the
	// message. Resending can duplicate it — check whether it already
	// arrived before deciding.
	//
	// NEVER produced by ClassifyResponse: this function only runs when
	// Meta HAS RESPONDED, and a response (even an error one) is exactly the
	// case where we know what it decided. Whoever produces
	// ClassUnknown is the caller (internal/outbound/handler.go), for
	// the outcomes where the call didn't end in any response at all —
	// transport, timeout, reading the response, a 2xx without an id. If
	// you're looking for this class's branch in classOfStatus and not
	// finding it: that's why — no branch is missing.
	ClassUnknown ErrorClass = "desconhecido"
)

// MetaError is what the consumer receives when Meta refuses.
type MetaError struct {
	Class    ErrorClass
	MetaCode int
	Message  string
	// Detail is the RAW text of `error.error_data.details` — the ONLY key
	// read from inside `error_data` (see ClassifyResponse's comment for
	// why the rest of the envelope stays out). Truncated at
	// detailRuneCap RUNES. Empty when Meta didn't send
	// `error_data.details`, or when `error_data` isn't an object, or
	// doesn't have that key — it NEVER panics because of this.
	Detail string
	// Subcode is `error.error_subcode` — disambiguates generic codes like
	// Meta's `2` ("unexpected error, try again"). Zero when Meta didn't
	// send the field, indistinguishable from a real zero subcode (same
	// tolerance MetaCode already has).
	Subcode int
	// Explanation is `error.error_user_msg` — text Meta WROTE TO BE SHOWN to
	// a human, unlike Detail (which can echo the payload that was sent).
	// When `error.error_user_title` also comes in, Explanation becomes
	// "title: msg". Truncated at detailRuneCap RUNES, for the same
	// reason as Detail: third-party string doesn't come in without a
	// limit. Empty when Meta didn't send either field.
	Explanation string
	// Trace is `error.fbtrace_id` — the ONLY identifier Meta support
	// accepts to open a ticket about a specific call, and it does NOT come
	// back after this call. It is NOT a secret: it can go up to the log and
	// into the response to the consumer without restriction (unlike
	// Detail).
	Trace string
}

func (e *MetaError) Error() string {
	return fmt.Sprintf("meta: %s (codigo %d): %s", e.Class, e.MetaCode, e.Message)
}

// ClassifyResponse returns nil for success, or the classified error.
//
// NEVER panics, for any body: Meta can return an empty body, proxy HTML, or
// JSON without the `error` field. None of that can erase the
// classification, which is already decided by the status.
func ClassifyResponse(status int, body []byte) *MetaError {
	if status >= 200 && status < 300 {
		return nil
	}

	e := &MetaError{Class: classOfStatus(status), Message: http.StatusText(status)}
	if e.Message == "" {
		e.Message = fmt.Sprintf("status %d", status)
	}

	// Each field is read ON ITS OWN. A single Unmarshal over the envelope
	// fails ATOMICALLY: one field with an unexpected type zeroes the whole
	// struct and takes the useful message down with it. It's the same
	// shape of defect that already cost this project's webhook parser a
	// Critical — there, one malformed item erased the whole batch.
	//
	// Only SEVEN keys are read: `message`, `code`, `error_subcode`,
	// `error_user_title`, `error_user_msg`, and `fbtrace_id` directly from
	// `error`, and — inside `error_data`, and ONLY there — `details`. The
	// rest of the body stays out on purpose: Meta's `error_data` can echo
	// the payload that was sent, which carries a phone number and message
	// text, and that text goes up to the log and into the response to the
	// consumer (T-141). The four new keys (T-153) don't carry that risk —
	// they're fields Meta itself writes for diagnostics or to be SHOWN to a
	// human, never an echo of the payload sent. `error_data` IS
	// deserialized, but only into a map[string]json.RawMessage (the same
	// treatment `error` gets above) — never into a struct that would
	// capture every key present: only the `details` key comes out of that
	// map. It's not certain that Meta still sends `error_data.details`
	// through today's path — the evidence that it sends it (T-141) came
	// through ANOTHER transport, on 2026-07-18; whether it arrives here is
	// an open question until measured in production.
	var envelope struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if raw, has := envelope.Error["message"]; has {
			var m string
			if json.Unmarshal(raw, &m) == nil && m != "" {
				e.Message = m
			}
		}
		if raw, has := envelope.Error["code"]; has {
			e.MetaCode = tolerantInt(raw)
			// Second chance, never second judge: only promotes when the
			// status fell into the default (ClassPermanent).
			// ClassConfig and ClassRetryable already decided by the
			// status stay as they are — see retryableCodesByNature's
			// comment for why.
			if e.Class == ClassPermanent && retryableCodesByNature[e.MetaCode] {
				e.Class = ClassRetryable
			}
		}
		if raw, has := envelope.Error["error_data"]; has {
			var errorData map[string]json.RawMessage
			if json.Unmarshal(raw, &errorData) == nil {
				if rawDetails, has := errorData["details"]; has {
					var d string
					if json.Unmarshal(rawDetails, &d) == nil && d != "" {
						e.Detail = truncateDetail(d)
					}
				}
			}
		}
		// T-153, item 1: the four keys below are read the SAME way as the
		// two above — each on its own, one's failure doesn't erase the
		// others.
		if raw, has := envelope.Error["error_subcode"]; has {
			e.Subcode = tolerantInt(raw)
		}
		if raw, has := envelope.Error["fbtrace_id"]; has {
			var trace string
			if json.Unmarshal(raw, &trace) == nil && trace != "" {
				e.Trace = trace
			}
		}
		// error_user_title and error_user_msg form Explanation TOGETHER, but
		// keep being read separately: a malformed title cannot erase the
		// message, and the absence of a title is the normal case (not
		// every Meta error response brings both).
		var title string
		if raw, has := envelope.Error["error_user_title"]; has {
			if json.Unmarshal(raw, &title) != nil {
				title = ""
			}
		}
		if raw, has := envelope.Error["error_user_msg"]; has {
			var msg string
			if json.Unmarshal(raw, &msg) == nil && msg != "" {
				if title != "" {
					e.Explanation = truncateDetail(title + ": " + msg)
				} else {
					e.Explanation = truncateDetail(msg)
				}
			}
		}
	}
	return e
}

// detailRuneCap is the ceiling in RUNES (not bytes) that MetaError.Detail
// and MetaError.Explanation can each carry. Both are third-party (Meta) text:
// Detail can echo a piece of the payload the consumer sent; Explanation is
// text Meta wrote to be shown, but without a ceiling neither would grow
// unbounded inside our response.
const detailRuneCap = 500

// truncateDetail cuts `s` to at most detailRuneCap RUNES, adding the
// suffix " …[truncado]" when it cuts. The cut is by RUNE, not by byte:
// cutting in the middle of a multibyte character would produce invalid
// UTF-8.
func truncateDetail(s string) string {
	r := []rune(s)
	if len(r) <= detailRuneCap {
		return s
	}
	return string(r[:detailRuneCap]) + " …[truncado]"
}

// tolerantInt reads an integer that can arrive as a number or as a
// string.
//
// This is NOT an assertion about what Meta does — I didn't check that
// against the source. It's cheap tolerance: the cost of accepting both
// forms is three lines, and the cost of not accepting is silently losing
// the code.
func tolerantInt(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 0
}

// retryableCodesByNature are Meta error codes that signal throttling
// (the Graph API's rate-limit family). Verified against the "Throttling
// errors" section of
// developers.facebook.com/docs/whatsapp/cloud-api/support/error-codes on
// 2026-08-20.
//
// WHY THE CODE IS THE KEY, AND NOT THE STATUS: Meta does NOT DOCUMENT which
// HTTP status these codes arrive with — the official page lists the
// throttling family without a status column, and the equivalent section of
// the Marketing Messages API shows an error of the SAME SHAPE arriving as
// 400 Bad Request. If classOfStatus were the only word, a throttling error
// wrapped in a 400 would fall into the default (ClassPermanent): the
// consumer would stop retrying exactly when waiting and retrying was the
// solution (see T-142's Why). The code doesn't have that ambiguity —
// 130429 means the same thing in any status envelope, so it serves as a key
// where the status doesn't.
//
// The list is OUR OWN and CONSERVATIVE: only a code whose nature is "wait
// and try again" per the official documentation gets in. A throttling code
// Meta invents tomorrow falls into ClassPermanent until someone adds it
// here — this table is not exhaustive, and the gateway doesn't promise it
// is.
var retryableCodesByNature = map[int]bool{
	130429: true, // Rate limit hit
	131056: true, // (Business Account, Consumer Account) pair rate limit hit
	133016: true, // (throttling)
	131048: true, // Spam rate limit hit
	80007:  true, // Rate limit issues
}

func classOfStatus(status int) ErrorClass {
	switch {
	case status == http.StatusTooManyRequests:
		// Rate limit: waiting and retrying IS the solution.
		return ClassRetryable
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		// Wrong, expired, or scopeless token. Resending doesn't fix it; a
		// person does.
		return ClassConfig
	case status >= 500 && status < 600:
		// The problem is Meta's.
		return ClassRetryable
	case status == http.StatusRequestTimeout, status == http.StatusTooEarly:
		// 408 and 425 are, by HTTP's own definition, "try again" — not an
		// error in the request. Leaving them in the default would make the
		// consumer give up on something recoverable.
		return ClassRetryable
	default:
		// Remaining 4xx, and anything out of range. Resending repeats the
		// error.
		return ClassPermanent
	}
}
