package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

func TestTransitCommandRequiresInstanceAndOneOfTheTwoIndexes(t *testing.T) {
	vars := testEnvironment(t)
	env := fakeEnvironment(vars)

	cases := [][]string{
		{"transito"},
		{"transito", "--telefone", "5511999990000"},
		{"transito", "--instancia", "lojinha"},
		// BOTH together are also rejected: they are different indexes, and
		// accepting both would force deciding which prevails.
		{"transito", "--instancia", "lojinha", "--telefone", "5511999990000", "--chave", "k1"},
	}
	for _, args := range cases {
		var out bytes.Buffer
		if err := dispatch(args, &out, env); err == nil {
			t.Errorf("args=%v: nenhum erro, quero recusa por falta de flag obrigatoria (ou pelas duas juntas)", args)
		}
	}
}

// TestTransitCommandFindsTheMessageAndDoesNOTLeakThePhone is the CLI end-to-end:
// a row recorded with the counterparty in PLAIN TEXT (T-094) shows up in
// the search under ANY spelling of the SAME number (via
// meta.LastEightDigits), and the `zapgw transito` screen still does not
// print the phone number — not because it is a secret (the owner's
// decision, 2026-07-30: it is not), but because whoever calls `--telefone`
// already has the number IN HAND, and echoing it back adds no information
// (`zapgw log`, which has NO number in hand at all, is the one that got the
// `contraparte` column — see log_test.go).
func TestTransitCommandFindsTheMessageAndDoesNOTLeakThePhone(t *testing.T) {
	vars := testEnvironment(t)
	env := fakeEnvironment(vars)

	store := storeFromEnvironment(t, vars)
	if err := store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "a", VerifyToken: "v", SendToken: "t",
		CallbackURL: "https://consumidor.interno/webhooks/zapgw", DeliverySecret: "s", TimeoutMs: 100,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	const numberWithoutNinth = "551199990000" // 12 digits
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Counterparty: meta.Canonicalize(numberWithoutNinth), Type: "mensagem", Correlation: "c1", Outcome: "consumidor guardou (200)",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Fechar: %v", err)
	}

	// Search with the CANONICAL form (with the ninth digit) — the operator
	// typing the number the way they know it, not the way Meta recorded
	// it.
	var out bytes.Buffer
	if err := dispatch([]string{"transito", "--instancia", "lojinha", "--telefone", "5511999990000"},
		&out, env); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "mensagem") || !strings.Contains(text, "entrada") ||
		!strings.Contains(text, "consumidor guardou (200)") {
		t.Fatalf("saida nao trouxe a linha esperada:\n%s", text)
	}
	if strings.Contains(text, numberWithoutNinth) || strings.Contains(text, "5511999990000") {
		t.Fatalf("a saida de `zapgw transito` vazou o telefone:\n%s", text)
	}
}

// TestTransitCommandFindsTheSendByKeyAndDoesNOTLeakTheIdempotencyKey is the
// post-review FIX: the consumer's Idempotency-Key has to find the row via
// the HMAC, and the plain value (the key itself, sentinel included) must
// never appear on screen.
func TestTransitCommandFindsTheSendByKeyAndDoesNOTLeakTheIdempotencyKey(t *testing.T) {
	vars := testEnvironment(t)
	env := fakeEnvironment(vars)

	store := storeFromEnvironment(t, vars)
	if err := store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "a", VerifyToken: "v", SendToken: "t",
		CallbackURL: "https://consumidor.interno/webhooks/zapgw", DeliverySecret: "s", TimeoutMs: 100,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	const sentinelKey = "SENTINELA-NAO-PODE-APARECER-5532999990000"
	writeHMAC := store.HMACCorrelation(sentinelKey)
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionOutbound,
		Counterparty: meta.Canonicalize("5511999990000"),
		Type:         "texto", Correlation: writeHMAC, Outcome: "enviado",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Fechar: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"transito", "--instancia", "lojinha", "--chave", sentinelKey},
		&out, env); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "saida") || !strings.Contains(text, "texto") || !strings.Contains(text, "enviado") {
		t.Fatalf("saida nao trouxe a linha esperada:\n%s", text)
	}
	if strings.Contains(text, sentinelKey) {
		t.Fatalf("a saida vazou a Idempotency-Key em claro:\n%s", text)
	}
	if strings.Contains(text, writeHMAC) {
		t.Fatalf("a saida vazou o HMAC da chave:\n%s", text)
	}
}

// TestTransitCommandPrintsTheWamidOrTheDash is Verify (a) of T-128: the
// `zapgw transito` screen gets the `wamid` column — the missing piece for
// the two `ALARME ... PRECISA DE GENTE` in internal/outbound/handler.go,
// which tell you to record the wa_message_id by hand without saying where
// to get it from. Records TWO rows in the SAME search — one outbound WITH a
// wamid, one inbound WITHOUT — and requires the exact value on the first
// and "—" (never empty) on the second, the SAME treatment cmd/zapgw/log.go
// already gives an absent counterparty. A test that only checked "the
// output didn't crash" would pass with the column always empty, which is
// exactly the defect this task closes.
func TestTransitCommandPrintsTheWamidOrTheDash(t *testing.T) {
	vars := testEnvironment(t)
	env := fakeEnvironment(vars)

	store := storeFromEnvironment(t, vars)
	if err := store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "a", VerifyToken: "v", SendToken: "t",
		CallbackURL: "https://consumidor.interno/webhooks/zapgw", DeliverySecret: "s", TimeoutMs: 100,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	const number = "5511999990000"
	// The wamid below STILL carried a real third party's phone number in
	// base64 (area code 32, ending in ...10) even after `numero`, in the
	// line above, had already been swapped for the synthetic one — caught
	// by the T-162 gate (internal/config/phones_allowlist_test.go),
	// which decodes the payload before deciding. Rewritten to match
	// `numero`: same "HBgN" prefix and same metadata suffix as the
	// original value, only the phone-number segment was swapped. (Full
	// number deliberately NOT written here — see the owner's rule about
	// not adding new occurrences of the real number, CLAUDE.md.)
	const wamid = "wamid.HBgNNTUxMTk5OTk5MDAwMAUCABIYFjNFQjBEO"
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionInbound,
		Counterparty: meta.Canonicalize(number), Type: "mensagem", Correlation: "c-entrada", Outcome: "consumidor guardou (200)",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit (entrada, sem wamid): %v", err)
	}
	if err := store.WriteTransit(config.TransitRecord{
		Slug: "lojinha", Direction: config.DirectionOutbound,
		Counterparty: meta.Canonicalize(number), Wamid: wamid, Type: "texto", Correlation: "c-saida", Outcome: "enviado",
	}, time.Now()); err != nil {
		t.Fatalf("WriteTransit (saida, com wamid): %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Fechar: %v", err)
	}

	var outBuf bytes.Buffer
	if err := dispatch([]string{"transito", "--instancia", "lojinha", "--telefone", number},
		&outBuf, env); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	text := outBuf.String()
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("saida tem menos de 3 linhas (cabecalho + 2 achadas):\n%s", text)
	}
	// SearchTransit returns from MOST RECENT to OLDEST: the outbound row
	// (recorded last) comes right after the header.
	outboundLine, inboundLine := lines[1], lines[2]
	if !strings.Contains(outboundLine, wamid) {
		t.Fatalf("linha de saida nao trouxe o wamid:\n%s", outboundLine)
	}
	if !strings.Contains(inboundLine, "—") {
		t.Fatalf("linha de entrada (sem wamid) nao trouxe o travessao:\n%s", inboundLine)
	}
	if strings.Contains(inboundLine, wamid) {
		t.Fatalf("linha de entrada trouxe o wamid da linha de saida:\n%s", inboundLine)
	}
}

func TestTransitCommandWithoutHitsIsNotAnError(t *testing.T) {
	vars := testEnvironment(t)
	env := fakeEnvironment(vars)

	store := storeFromEnvironment(t, vars)
	if err := store.CreateInstance(config.Instance{
		Slug: "lojinha", WabaID: "WABA1", PhoneNumberID: "PNID1",
		AppSecret: "a", VerifyToken: "v", SendToken: "t",
		CallbackURL: "https://consumidor.interno/webhooks/zapgw", DeliverySecret: "s", TimeoutMs: 100,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Fechar: %v", err)
	}

	var out bytes.Buffer
	if err := dispatch([]string{"transito", "--instancia", "lojinha", "--telefone", "5511999990000"},
		&out, env); err != nil {
		t.Fatalf("dispatch: %v — buscar num numero que nunca falou nao e erro", err)
	}
	if !strings.Contains(out.String(), "nada encontrado") {
		t.Fatalf("saida = %q, queria dizer que nada foi encontrado", out.String())
	}
}
