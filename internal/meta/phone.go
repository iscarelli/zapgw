package meta

import "strings"

// Canonicalize returns the number with only digits and, when it's a Brazilian
// cell phone written without the 9th digit, inserts the 9.
//
// WHY THIS EXISTS: Meta does NOT guarantee the same spelling you registered.
// The same subscriber arrives as 5511999990000 (13) and 551199990000 (12).
// Comparing the two forms with `==` fails silently — it has already cost a
// silent E2E failure in production.
//
// THE GUARD IS THE FIFTH DIGIT. A LANDLINE also has 12 digits (55 + area code
// + 8), and the landline subscriber starts at 2-5; the cell phone starts at
// 6-9. Inserting the 9 into every 12-digit number would produce nonexistent
// numbers for every landline in the country.
//
// Outside the Brazilian 12-digit case the function only cleans: inventing a
// digit for a country code we don't know is worse than doing nothing.
func Canonicalize(number string) string {
	var b strings.Builder
	for _, r := range number {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()

	if len(d) != 12 || !strings.HasPrefix(d, "55") {
		return d
	}
	if c := d[4]; c < '6' || c > '9' {
		return d // landline (2-5) — never gains the 9
	}
	return d[:4] + "9" + d[4:]
}

// LastEightDigits extracts the LAST EIGHT digits of a number, in ANY
// spelling — owner's decision, 2026-07-30 ("you can put the number in, it's
// not a secret"), which switched the transit-log search index from an HMAC of
// the CANONICAL number (T-091) to the last eight digits in the CLEAR (T-094).
//
// WHY THE LAST EIGHT, AND NOT Canonicalize: Canonicalize only knows how to insert
// the ninth digit when the number ALREADY HAS 12 digits with the "55" prefix
// — it doesn't help "32999990000" (without the "55") or "(32) 99999-0000"
// (formatted, without the "55") converge to the same form as
// "5532999990000". THE LAST EIGHT DIGITS SURVIVE both variations (with/without
// "55", with/without the ninth) because neither the country code, the area
// code, nor the ninth digit enters the last eight — only the subscriber's own
// number does (docs/ARMADILHAS.md, "Telefone brasileiro"). The four spellings
// below all end in "99990000":
//
//	(32) 99999-0000  · 32999990000  · 553299990000  · 5532999990000
//
// A number with fewer than eight digits returns "" — there's nothing to
// match, and returning a shorter prefix would match ANY number that ended in
// it (the same care as HMACContraparte(""), now without HMAC).
func LastEightDigits(number string) string {
	var b strings.Builder
	for _, r := range number {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 8 {
		return ""
	}
	return d[len(d)-8:]
}
