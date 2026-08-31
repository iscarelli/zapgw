package meta

import "testing"

// TRAP — REAL cost, and the most expensive of the "silent failure" family in
// this network. In a production E2E on 2026-07-20 00:23 the customer tapped a
// button, the webhook recorded the message and answered 200 OK, the `==`
// between phone numbers matched nothing, and NOTHING was sent back. From the
// outside it was indistinguishable from success.
// Cause: Meta does NOT guarantee the same spelling you registered — the same
// subscriber coexists with and without the 9th digit.
func TestCanonicalizeInsertsTheNinthDigitWhenItIsMissing(t *testing.T) {
	cases := []struct{ input, want string }{
		{"551199990000", "5511999990000"},  // cell phone without the 9 -> gains it
		{"5511999990000", "5511999990000"}, // already canonical -> untouched
		{"5511987654321", "5511987654321"},
		{"551187654321", "5511987654321"},
	}

	for _, c := range cases {
		if got := Canonicalize(c.input); got != c.want {
			t.Errorf("Canonicalize(%q) = %q, quero %q", c.input, got, c.want)
		}
	}
}

// THE GUARD that separates the fix from the damage. A LANDLINE also has 12
// digits (55 + area code + 8), and the landline subscriber starts at 2-5. A
// bare `if len == 12 { insert 9 }` would produce numbers that DO NOT EXIST
// for every landline in the country.
func TestCanonicalizeDoesNotTouchALandline(t *testing.T) {
	cases := []string{
		"553234567890", // landline, starts with 3
		"551123456789", // landline, starts with 2
		"553245678901", // landline, starts with 4
		"551155555555", // landline, starts with 5
	}

	for _, input := range cases {
		if got := Canonicalize(input); got != input {
			t.Errorf("Canonicalize(%q) = %q — fixo nao pode ganhar o 9", input, got)
		}
	}
}

func TestCanonicalizeStripsFormatting(t *testing.T) {
	cases := []struct{ input, want string }{
		{"+55 11 99999-0000", "5511999990000"}, // already canonical, just cleans
		{"+55 (32) 3456-7890", "553234567890"}, // formatted LANDLINE: cleans and does NOT gain the 9
		{"+55 11 9999-0000", "5511999990000"},  // formatted cell phone WITHOUT the 9: cleans AND canonicalizes
	}

	for _, c := range cases {
		if got := Canonicalize(c.input); got != c.want {
			t.Errorf("Canonicalize(%q) = %q, quero %q", c.input, got, c.want)
		}
	}
}

func TestCanonicalizeDoesNotInventForAForeignOrEmptyNumber(t *testing.T) {
	// Outside the Brazilian 12-digit case, the function only cleans and
	// returns. Inventing a digit for a country code we don't know is worse
	// than doing nothing.
	cases := []struct{ input, want string }{
		{"", ""},
		{"12025550123", "12025550123"}, // US, 11 digits
		{"5532", "5532"},
		{"abc", ""},
	}

	for _, c := range cases {
		if got := Canonicalize(c.input); got != c.want {
			t.Errorf("Canonicalize(%q) = %q, quero %q", c.input, got, c.want)
		}
	}
}

// TestLastEightDigitsIgnores55DDDAndTheNinthDigit is the Verify (a) of T-094 at
// the source: the FOUR spellings of the same subscriber — with/without "55",
// with/without the ninth digit, with/without formatting — have to produce the
// SAME eight digits. And it's the reason the owner accepted the switch:
// Canonicalize alone does NOT resolve the two spellings without "55" (they never
// reach 12 digits with the "55" prefix, so the ninth-digit insertion never
// fires).
func TestLastEightDigitsIgnores55DDDAndTheNinthDigit(t *testing.T) {
	const want = "99990000"
	spellings := []string{
		"(32) 99999-0000", // as an operator would type it
		"32999990000",     // without the "55"
		"553299990000",    // with "55", without the ninth
		"5532999990000",   // canonical
	}
	for _, g := range spellings {
		if got := LastEightDigits(g); got != want {
			t.Errorf("LastEightDigits(%q) = %q, quero %q", g, got, want)
		}
	}
}

// TestLastEightDigitsOfAShortNumberReturnsEmpty: returning a prefix shorter
// than eight digits would match ANY number ending in it — the same care that
// HMACContraparte("") had before this task, just without the HMAC.
func TestLastEightDigitsOfAShortNumberReturnsEmpty(t *testing.T) {
	cases := []string{"", "1234567", "abc"}
	for _, c := range cases {
		if got := LastEightDigits(c); got != "" {
			t.Errorf("LastEightDigits(%q) = %q, quero vazio", c, got)
		}
	}
}
