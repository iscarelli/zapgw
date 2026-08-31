package meta

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// readCorpus reads a REAL payload (or, when flagged in the README, derived
// from Meta's published documentation) from testdata/corpus. corpusDir
// comes from corpus_test.go, same package.
func readCorpus(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpusDir, name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	return b
}

func TestParseWebhookReadsATextMessage(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"display_phone_number":"5532999990000","phone_number_id":"PNID1"},
	    "contacts":[{"profile":{"name":"Maria"},"wa_id":"551199990000"}],
	    "messages":[{"from":"551199990000","id":"wamid.AAA","timestamp":"1769000000",
	                 "type":"text","text":{"body":"quero reservar mesa"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}

	e := evs[0]
	if e.Type != EventTypeMessage {
		t.Errorf("Type = %q, quero %q", e.Type, EventTypeMessage)
	}
	if e.WaMessageID != "wamid.AAA" {
		t.Errorf("WaMessageID = %q", e.WaMessageID)
	}
	if e.PhoneNumberID != "PNID1" {
		t.Errorf("PhoneNumberID = %q", e.PhoneNumberID)
	}
	if e.WabaID != "WABA1" {
		t.Errorf("WabaID = %q", e.WabaID)
	}
	if e.Text != "quero reservar mesa" {
		t.Errorf("Text = %q", e.Text)
	}
	if e.ID != "msg:wamid.AAA" {
		t.Errorf("ID = %q, quero msg:wamid.AAA", e.ID)
	}
	// The TWO forms of the phone number, and that's what makes the
	// consumer's `==` correct by construction in any language.
	if e.FromRaw != "551199990000" {
		t.Errorf("FromRaw = %q, quero o valor EXATO que a Meta mandou", e.FromRaw)
	}
	if e.FromCanonical != "5511999990000" {
		t.Errorf("FromCanonical = %q, quero 5511999990000", e.FromCanonical)
	}
	if e.ContactName != "Maria" {
		t.Errorf("ContactName = %q", e.ContactName)
	}
}

// TRAP — REAL cost: a whole confirmation loop (customer taps
// Confirm/Cancel, the schedule updates itself) was built on this network
// with 11 green tests over a payload the system was INCAPABLE of
// producing. Reason: send_interactive only works INSIDE the 24h window; a
// day-before reminder goes OUTSIDE it, so only via TEMPLATE with
// quick-reply — and the reply to that button does NOT arrive as
// interactive.button_reply. It arrives as type "button".
func TestParseWebhookReadsATemplateButtonAsTypeButton(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "messages":[{"from":"5511999990000","id":"wamid.BBB","timestamp":"1769000001",
	                 "type":"button","button":{"payload":"CONFIRMAR_123","text":"Confirmar"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].ButtonPayload != "CONFIRMAR_123" {
		t.Errorf("ButtonPayload = %q, quero CONFIRMAR_123", evs[0].ButtonPayload)
	}
	if evs[0].ButtonText != "Confirmar" {
		t.Errorf("ButtonText = %q, quero Confirmar", evs[0].ButtonText)
	}
}

func TestParseWebhookReadsAnInteractiveButton(t *testing.T) {
	// The OTHER form, which only exists INSIDE the 24h window. The two
	// coexist, and that's why the consumer receives a single field:
	// ButtonPayload.
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "messages":[{"from":"5511999990000","id":"wamid.CCC","timestamp":"1769000002",
	                 "type":"interactive","interactive":{"type":"button_reply",
	                 "button_reply":{"id":"CANCELAR_123","title":"Cancelar"}}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if evs[0].ButtonPayload != "CANCELAR_123" {
		t.Errorf("ButtonPayload = %q, quero CANCELAR_123", evs[0].ButtonPayload)
	}
	if evs[0].ButtonText != "Cancelar" {
		t.Errorf("ButtonText = %q, quero Cancelar", evs[0].ButtonText)
	}
}

func TestParseWebhookReadsMediaWithThePayloadsMime(t *testing.T) {
	// The PAYLOAD's mime carries "codecs=opus", which is what makes
	// WhatsApp render a playable VOICE NOTE. GET /{media_id} returns a
	// bare "audio/ogg". The gateway reports both and does NOT normalize
	// either one — normalizing would destroy exactly what needs to be
	// preserved. Cost already paid in production.
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "messages":[{"from":"5511999990000","id":"wamid.DDD","timestamp":"1769000003",
	                 "type":"audio","audio":{"id":"MEDIA1","mime_type":"audio/ogg; codecs=opus","voice":true}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if evs[0].MediaID != "MEDIA1" {
		t.Errorf("MediaID = %q", evs[0].MediaID)
	}
	if evs[0].MediaMimePayload != "audio/ogg; codecs=opus" {
		t.Errorf("MediaMimePayload = %q — o parametro codecs NAO pode ser cortado", evs[0].MediaMimePayload)
	}
}

// TRAP — cost: a Critical in this network's previous library.
// json.loads("null") does NOT raise in Python; in Go, json.Unmarshal of
// "null" into a map leaves the map NIL and does NOT return an error. The
// mechanism changes, the outcome to avoid is the same: carrying on thinking
// there's data. It has to become a NAMED error, and the caller delivers the
// raw body regardless (spec invariant 2).
func TestParseWebhookRefusesABodyThatIsNotAnObject(t *testing.T) {
	cases := []string{`null`, `42`, `[]`, `"texto"`, `true`, ``, `{`}

	for _, c := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panico com corpo %q: %v", c, r)
				}
			}()
			evs, err := ParseWebhook([]byte(c))
			if err == nil {
				t.Fatalf("corpo %q devia dar erro, veio nil (evs=%v)", c, evs)
			}
			if len(evs) != 0 {
				t.Fatalf("corpo %q devolveu %d eventos, quero 0", c, len(evs))
			}
		}()
	}
	// The `null` case has its own error, so the log can say WHAT came in.
	_, err := ParseWebhook([]byte(`null`))
	if !errors.Is(err, ErrBodyNotObject) {
		t.Fatalf("erro para null = %v, quero ErrBodyNotObject", err)
	}
}

func TestParseWebhookIgnoresAnUnknownEventWithoutBringingDownTheOthers(t *testing.T) {
	// An event we don't know how to read CANNOT discard the ones after
	// it. It was a Critical in the previous library: the try wrapped the
	// whole loop, and one bad event discarded all the following ones,
	// with a 200.
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "messages":[
	      {"from":"5511999990000","id":"wamid.EEE","timestamp":"1769000004","type":"tipo_que_nao_existe"},
	      {"from":"5511999990000","id":"wamid.FFF","timestamp":"1769000005","type":"text","text":{"body":"oi"}}
	    ]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("len(evs) = %d, quero 2 — o desconhecido tambem chega, so sem campos ricos", len(evs))
	}
	if evs[1].Text != "oi" {
		t.Errorf("a mensagem DEPOIS do evento desconhecido se perdeu: %+v", evs[1])
	}
}

// CRITICAL — found in the T5 review, proven by a test before the fix.
// Meta batches `entry` from DIFFERENT accounts in the same call. Before the
// fix, a single Unmarshal covered entry+changes+value, and a wrong-typed
// field ANYWHERE in the tree erased the whole batch: account B's valid
// message vanished because of account A's malformed payload. It's this
// network's mother trap — the fix had reached the message level and hadn't
// reached the level above.
func TestParseWebhookIsolatesAMalformedEntryFromItsSiblings(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[
	  {"id":"WABA_RUIM","changes":[{"field":"messages","value":{
	     "metadata":{"phone_number_id":"PNID_RUIM"},
	     "messages":{}}}]},
	  {"id":"WABA_BOA","changes":[{"field":"messages","value":{
	     "metadata":{"phone_number_id":"PNID_BOA"},
	     "messages":[{"from":"5511999990000","id":"wamid.BOA","timestamp":"1769000000",
	                  "type":"text","text":{"body":"sobrevivi"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)

	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — a mensagem da conta BOA foi descartada junto com a RUIM", len(evs))
	}
	if evs[0].Text != "sobrevivi" {
		t.Errorf("Text = %q", evs[0].Text)
	}
	if evs[0].WabaID != "WABA_BOA" {
		t.Errorf("WabaID = %q", evs[0].WabaID)
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Errorf("err = %v, quero ErrPartialParse — o item ignorado tem de ser SINALIZADO, nao sumir calado", err)
	}
}

func TestParseWebhookIsolatesAMalformedChangeFromItsSiblings(t *testing.T) {
	// The same defect one level down: two `changes` in the SAME entry.
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":"isto devia ser um objeto"},
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.BOA","timestamp":"1769000000",
	                "type":"text","text":{"body":"sobrevivi"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)

	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — a change boa foi descartada com a ruim", len(evs))
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Errorf("err = %v, quero ErrPartialParse", err)
	}
}

func TestParseWebhookWithNoIgnoredItemReturnsNoError(t *testing.T) {
	// The counterpart: a whole good payload cannot alarm by mistake.
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.A","timestamp":"1769000000",
	                "type":"text","text":{"body":"oi"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("err = %v, quero nil", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
}

// TRAP — cost: a lost or duplicated status.
// The SAME message arrives sent -> delivered -> read with the SAME
// wa_message_id. Deduplicating by id alone discards two of the three. The
// key is COMPOSITE.
func TestParseWebhookUsesACompositeKeyInTheStatus(t *testing.T) {
	build := func(status string) []byte {
		return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
		  {"field":"messages","value":{
		    "metadata":{"phone_number_id":"PNID1"},
		    "statuses":[{"id":"wamid.XYZ","status":"` + status + `",
		                 "timestamp":"1769000010","recipient_id":"551199990000"}]}}]}]}`)
	}

	seenIDs := map[string]bool{}
	for _, s := range []string{"sent", "delivered", "read"} {
		evs, err := ParseWebhook(build(s))
		if err != nil {
			t.Fatalf("erro inesperado em %q: %v", s, err)
		}
		if len(evs) != 1 {
			t.Fatalf("len(evs) = %d para %q, quero 1", len(evs), s)
		}
		e := evs[0]
		if e.Type != EventTypeStatus {
			t.Errorf("Type = %q, quero %q", e.Type, EventTypeStatus)
		}
		if e.Status != s {
			t.Errorf("Status = %q, quero %q", e.Status, s)
		}
		want := "status:wamid.XYZ:" + s
		if e.ID != want {
			t.Errorf("ID = %q, quero %q", e.ID, want)
		}
		if seenIDs[e.ID] {
			t.Fatalf("ID %q repetido — a chave composta nao esta distinguindo os status", e.ID)
		}
		seenIDs[e.ID] = true
	}
	if len(seenIDs) != 3 {
		t.Fatalf("ids distintos = %d, quero 3", len(seenIDs))
	}
}

func TestParseWebhookGivesBothFormsOfTheRecipientInTheStatus(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.XYZ","status":"delivered",
	                 "timestamp":"1769000010","recipient_id":"551199990000"}]}}]}]}`)

	evs, _ := ParseWebhook(payload)
	if evs[0].ToRaw != "551199990000" {
		t.Errorf("ToRaw = %q, quero o valor EXATO da Meta", evs[0].ToRaw)
	}
	if evs[0].ToCanonical != "5511999990000" {
		t.Errorf("ToCanonical = %q, quero 5511999990000", evs[0].ToCanonical)
	}
}

func TestParseWebhookReadsAMessageAndAStatusInTheSamePayload(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "messages":[{"from":"5511999990000","id":"wamid.M","timestamp":"1769000020",
	                 "type":"text","text":{"body":"oi"}}],
	    "statuses":[{"id":"wamid.S","status":"read","timestamp":"1769000021",
	                 "recipient_id":"5511999990000"}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("len(evs) = %d, quero 2", len(evs))
	}
}

// Minor found in the T5 review. A `null` or `{}` message does NOT make the
// Unmarshal fail (Go treats it as a no-op on a non-pointer struct), so it
// became an empty Event with ID == "msg:". Two of them collided in the
// consumer's dedup — and types.go's comment PROMISES a unique id for
// dedup. A promise and code disagreeing is how a bug hides on this
// network.
func TestParseWebhookEmitsNoEventWithoutMetasId(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[
	     null,
	     {},
	     {"from":"5511999990000","id":"wamid.BOA","timestamp":"1769000000",
	      "type":"text","text":{"body":"sobrevivi"}}
	   ]}}]}]}`)

	evs, err := ParseWebhook(payload)

	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — mensagem sem id nao pode virar evento", len(evs))
	}
	if evs[0].ID != "msg:wamid.BOA" {
		t.Errorf("ID = %q", evs[0].ID)
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Errorf("err = %v, quero ErrPartialParse — o descarte tem de ser SINALIZADO", err)
	}
}

func TestParseWebhookEmitsNoStatusWithoutMetasId(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "statuses":[
	     {"status":"sent","timestamp":"1769000000","recipient_id":"5511999990000"},
	     {"id":"wamid.S","status":"delivered","timestamp":"1769000001","recipient_id":"5511999990000"}
	   ]}}]}]}`)

	evs, err := ParseWebhook(payload)

	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — status sem id daria a chave composta \"status::sent\"", len(evs))
	}
	if evs[0].ID != "status:wamid.S:delivered" {
		t.Errorf("ID = %q", evs[0].ID)
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Errorf("err = %v, quero ErrPartialParse", err)
	}
}

// --- T-023: reaction, location, voice, caption/file name ---

// T-028: Meta sends a failure's reason inside errors[], in the status
// webhook itself — before this task the gateway only read
// id/status/timestamp and the reason was lost. Shape checked at
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
// (read on 2026-07-26): see StatusError in types.go for the full quote.
//
// status_failed.json became a real CAPTURE in T-033 (consumer-a,
// 2026-07-26): it's the real failure from 2026-07-20 (OS LR-00014, code
// 131026), the same one that prompted the operator warning that gave rise
// to this whole task — before it was derived from the doc's generic
// example (code 131049).
func TestParseWebhookAFailedStatusWithAnErrorProducesCodeAndMessage(t *testing.T) {
	payload := readCorpus(t, "status_failed.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Status != "failed" {
		t.Fatalf("Status = %q, quero failed", e.Status)
	}
	if e.Error == nil {
		t.Fatal("Error == nil — status failed com errors[] tem de produzir StatusError")
	}
	if e.Error.Code != 131026 {
		t.Errorf("Error.Code = %d, quero 131026", e.Error.Code)
	}
	if e.Error.Message != "Message undeliverable" {
		t.Errorf("Error.Message = %q", e.Error.Message)
	}
}

// T-029: errors[0].error_data.details adds the ONLY part of the message
// that doesn't repeat the title — see StatusError.Details's comment in
// types.go. Case (a) of the task's Verify.
func TestParseWebhookAFailedStatusWithErrorDataProducesDetails(t *testing.T) {
	payload := readCorpus(t, "status_failed.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Error == nil {
		t.Fatal("Error == nil — status failed com errors[] tem de produzir StatusError")
	}
	want := "Message Undeliverable."
	if e.Error.Details != want {
		t.Errorf("Error.Details = %q, quero %q", e.Error.Details, want)
	}
}

// T-029, case (b) of the Verify: error_data is a NESTED object and might
// not exist in the item. Its absence CANNOT bring down Code/Message,
// which already worked before this task — and the task's mandatory
// mutation targets exactly this test (zeroing the whole error when
// error_data is missing has to leave it red).
func TestParseWebhookAFailedStatusWithoutErrorDataDoesNotBringDownCodeAndMessage(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.F","status":"failed",
	                 "timestamp":"1769000033","recipient_id":"551199990000",
	                 "errors":[{"code":131049,"title":"titulo sem error_data"}]}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Error == nil {
		t.Fatal("Error == nil — ausencia de error_data nao pode derrubar codigo/mensagem")
	}
	if e.Error.Code != 131049 || e.Error.Message != "titulo sem error_data" {
		t.Errorf("Error = %+v, quero Code/Message intactos mesmo sem error_data", e.Error)
	}
	if e.Error.Details != "" {
		t.Errorf("Details = %q, quero vazio — error_data nao veio no payload", e.Error.Details)
	}
}

// T-029, case (c) of the Verify: error_data present but without the
// "details" key. Same guarantee as case (b), different JSON shape.
func TestParseWebhookAFailedStatusWithErrorDataButNoDetailsDoesNotBreak(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.F","status":"failed",
	                 "timestamp":"1769000034","recipient_id":"551199990000",
	                 "errors":[{"code":131049,"title":"titulo","error_data":{}}]}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Error == nil {
		t.Fatal("Error == nil")
	}
	if e.Error.Code != 131049 || e.Error.Message != "titulo" {
		t.Errorf("Error = %+v, quero Code/Message intactos", e.Error)
	}
	if e.Error.Details != "" {
		t.Errorf("Details = %q, quero vazio — error_data sem \"details\"", e.Error.Details)
	}
}

// TRAP this test exists to prevent: if Error became a plain field (an int
// with no pointer/omitempty) instead of a *StatusError, a "failed" WITHOUT
// errors[] in the payload would produce code 0 and an empty message — a
// FALSE error, indistinguishable from a real one (Meta doesn't use code
// 0). Absence has to be absence.
func TestParseWebhookAFailedStatusWithoutErrorsDoesNotInventCodeZero(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.F","status":"failed",
	                 "timestamp":"1769000030","recipient_id":"551199990000"}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Error != nil {
		t.Errorf("Error = %+v, quero nil — failed sem errors[] nao pode inventar motivo", evs[0].Error)
	}
}

// errors[] is a LIST. Meta can send more than one item; the gateway keeps
// only the FIRST, and that choice is documented (not a silent discard) in
// CONTRATO-CONSUMIDOR.md.
func TestParseWebhookAStatusWithTwoErrorsKeepsOnlyTheFirst(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.F","status":"failed",
	                 "timestamp":"1769000031","recipient_id":"551199990000",
	                 "errors":[
	                   {"code":131049,"title":"primeiro erro"},
	                   {"code":470,"title":"segundo erro"}
	                 ]}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Error == nil {
		t.Fatal("Error == nil")
	}
	if evs[0].Error.Code != 131049 || evs[0].Error.Message != "primeiro erro" {
		t.Errorf("Error = %+v, quero o PRIMEIRO item (131049, \"primeiro erro\")", evs[0].Error)
	}
}

// An errors[] item with an unexpected shape (here, "code" arriving as a
// string) cannot bring down the WHOLE EVENT — losing the reason is
// acceptable, losing id/status/timestamp isn't. Contrast with
// TestParseWebhookEmitsNoStatusWithoutMetasId: there the id, required for
// dedup, is missing; here the id and the other fields are intact, only
// the error sub-object can't be read.
func TestParseWebhookInAStatusErrorAMalformedItemDoesNotBringDownTheEvent(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.F","status":"failed",
	                 "timestamp":"1769000032","recipient_id":"551199990000",
	                 "errors":[{"code":"nao-e-numero","title":"x"}]}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v — item de erro malformado nao pode virar ErrPartialParse do EVENTO", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — o status sobrevive ao erro malformado", len(evs))
	}
	e := evs[0]
	if e.ID != "status:wamid.F:failed" || e.Status != "failed" || e.ToRaw != "551199990000" {
		t.Errorf("evento base corrompido pelo item de erro malformado: %+v", e)
	}
	if e.Error != nil {
		t.Errorf("Error = %+v, quero nil — o item nao pode ser interpretado", e.Error)
	}
}

// --- T-041: "pricing" in the status webhook becomes "cobranca" in the envelope ---
//
// Requested by consumer-a (2026-07-26): 145 of the 148 status events they
// have recorded carry "pricing", and editing a template can make Meta
// reclassify UTILITY -> MARKETING (changes price and sending rules) —
// without this field, that would only show up on the invoice, weeks
// later. See Billing in types.go for the full quote and the reason only
// Category/Billable are included.

func TestParseWebhookAStatusWithPricingProducesBilling(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.P1","status":"read",
	                 "timestamp":"1769000050","recipient_id":"551199990000",
	                 "pricing":{"billable":true,"pricing_model":"PMP","category":"utility","type":"regular"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Billing == nil {
		t.Fatal("Billing == nil — status com pricing tem de produzir Billing")
	}
	if e.Billing.Category != "utility" {
		t.Errorf("Category = %q, quero utility", e.Billing.Category)
	}
	if e.Billing.Billable == nil || !*e.Billing.Billable {
		t.Errorf("Billable = %v, quero um *bool apontando para true", e.Billing.Billable)
	}
}

// The task's MANDATORY MUTATION: if Billable became a bool instead of a
// *bool, "billable": false and an absent "billable" would produce the SAME
// zero value (false) — indistinguishable from each other, when the
// difference here is about MONEY ("Meta said it doesn't charge" versus
// "Meta said nothing"). This test goes RED with that mutation: it
// requires a *bool pointing to false, not just a "not-true".
func TestParseWebhookStatusPricingBillableFalseDiffersFromAbsent(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.P2","status":"sent",
	                 "timestamp":"1769000051","recipient_id":"551199990000",
	                 "pricing":{"billable":false,"pricing_model":"CBP","category":"service","type":"regular"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Billing == nil {
		t.Fatal("Billing == nil — pricing veio, so billable e false")
	}
	if e.Billing.Billable == nil {
		t.Fatal("Billable == nil, quero um *bool apontando para false (explicito, nao ausente)")
	}
	if *e.Billing.Billable != false {
		t.Errorf("Billable = %v, quero false", *e.Billing.Billable)
	}
}

// The counterpart of the mutation above: a status WITHOUT "pricing" in the
// payload cannot invent any Billing — not even with Billable pointing to
// false by default. Absence is absence.
func TestParseWebhookAStatusWithoutPricingGetsNoBilling(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.P3","status":"sent",
	                 "timestamp":"1769000052","recipient_id":"551199990000"}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Billing != nil {
		t.Errorf("Billing = %+v, quero nil — status sem pricing nao pode inventar cobranca", evs[0].Billing)
	}
}

// "pricing": null has to behave like absence, not like a zeroed Billing —
// the same reason as errorDataMeta (json.Unmarshal of null over a PLAIN
// struct is a no-op, not an error; see pricingMeta's comment in parse.go).
func TestParseWebhookAStatusWithNullPricingGetsNoBilling(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.P4","status":"sent",
	                 "timestamp":"1769000053","recipient_id":"551199990000",
	                 "pricing":null}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Billing != nil {
		t.Errorf("Billing = %+v, quero nil — pricing:null e ausencia, nao uma cobranca zerada", evs[0].Billing)
	}
}

// A "pricing" of the WRONG TYPE (a string instead of an object) cannot
// bring down the WHOLE EVENT — losing the billing is acceptable, losing
// id/status/timestamp isn't. THE SAME family as
// TestParseWebhookInAStatusErrorAMalformedItemDoesNotBringDownTheEvent, above, now
// for this task's new field.
func TestParseWebhookAMalformedStatusPricingDoesNotBringDownTheEvent(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "statuses":[{"id":"wamid.P5","status":"sent",
	                 "timestamp":"1769000054","recipient_id":"551199990000",
	                 "pricing":"nao-e-um-objeto"}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v — pricing malformado nao pode virar ErrPartialParse do EVENTO", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — o status sobrevive ao pricing malformado", len(evs))
	}
	e := evs[0]
	if e.ID != "status:wamid.P5:sent" || e.Status != "sent" || e.ToRaw != "551199990000" {
		t.Errorf("evento base corrompido pelo pricing malformado: %+v", e)
	}
	if e.Billing != nil {
		t.Errorf("Billing = %+v, quero nil — o pricing nao pode ser interpretado", e.Billing)
	}
}

// --- T-033: errors[] INSIDE messages[] (sub_tipo "unsupported") ---
//
// Meta sends this when it receives something the Cloud API doesn't know
// how to represent (e.g. a new message type, or an edit —
// "unsupported":{"type":"edit"} in the official example). Before this
// task, `messageMeta` had no `Errors` field, so the event came out with
// just sub_tipo "unsupported", an id, and nothing else — indistinguishable
// from "empty message" to the consumer. Payload checked at
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/
// (read on 2026-07-26):
//
//	"messages":[{"from":"16505551234","id":"wamid…","timestamp":"1750090702",
//	             "errors":[{"code":131051,"title":"Message type unknown",
//	                        "message":"Message type unknown",
//	                        "error_data":{"details":"Message type is currently not supported."}}],
//	             "type":"unsupported","unsupported":{"type":"edit"}}]
//
// The "unsupported":{"type":"edit"} field is NOT modeled (out of this
// task's scope — no one asked, and the envelope only grows by explicit
// decision).
//
// Case (a) of the task's Verify.
func TestParseWebhookAnUnsupportedMessageWithAnErrorProducesCodeAndMessage(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.UNSUPPORTED1","timestamp":"1769000040",
	                "type":"unsupported",
	                "errors":[{"code":131051,"title":"Message type unknown","message":"Message type unknown",
	                           "error_data":{"details":"Message type is currently not supported."}}],
	                "unsupported":{"type":"edit"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.SubType != "unsupported" {
		t.Fatalf("SubType = %q, quero unsupported", e.SubType)
	}
	if e.Error == nil {
		t.Fatal("Error == nil — mensagem unsupported com errors[] tem de produzir StatusError")
	}
	if e.Error.Code != 131051 {
		t.Errorf("Error.Code = %d, quero 131051", e.Error.Code)
	}
	if e.Error.Message != "Message type unknown" {
		t.Errorf("Error.Message = %q, quero \"Message type unknown\"", e.Error.Message)
	}
	if e.Error.Details != "Message type is currently not supported." {
		t.Errorf("Error.Details = %q", e.Error.Details)
	}
}

// Case (b) of the Verify: a NORMAL message still has no `erro` — real
// omitempty, not a zeroed struct visible in the JSON. Complements (without
// replacing) TestParseWebhookDoesNotRegressTheCurrent16Fields, which already
// locks "erro" into the list of fields that CANNOT appear on a plain text
// message.
func TestParseWebhookANormalMessageGetsNoError(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.NORMAL1","timestamp":"1769000041",
	                "type":"text","text":{"body":"oi"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Error != nil {
		t.Errorf("Error = %+v, quero nil — mensagem sem errors[] nao pode ganhar motivo", evs[0].Error)
	}
	b, err := json.Marshal(evs[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "erro") {
		t.Errorf("\"erro\" apareceu no JSON de uma mensagem sem errors[]: %s", b)
	}
}

// Case (c) of the Verify: unsupported WITHOUT errors[] cannot invent code
// zero — the same trap as the status side
// (TestParseWebhookAFailedStatusWithoutErrorsDoesNotInventCodeZero).
func TestParseWebhookAnUnsupportedMessageWithoutErrorsDoesNotInventCodeZero(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.UNSUPPORTED2","timestamp":"1769000042",
	                "type":"unsupported"}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Error != nil {
		t.Errorf("Error = %+v, quero nil — unsupported sem errors[] nao pode inventar motivo", evs[0].Error)
	}
}

// An errors[0] of unexpected shape ("code" as a string) on the MESSAGE
// cannot bring down the whole event — the same guarantee that already
// exists on the status side
// (TestParseWebhookInAStatusErrorAMalformedItemDoesNotBringDownTheEvent), now proven
// at the other entry point of the same mechanism.
func TestParseWebhookInAMessageErrorAMalformedItemDoesNotBringDownTheEvent(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.UNSUPPORTED3","timestamp":"1769000043",
	                "type":"unsupported","errors":[{"code":"nao-e-numero","title":"x"}]}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v — item de erro malformado nao pode virar ErrPartialParse do EVENTO", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — a mensagem sobrevive ao erro malformado", len(evs))
	}
	e := evs[0]
	if e.ID != "msg:wamid.UNSUPPORTED3" || e.SubType != "unsupported" {
		t.Errorf("evento base corrompido pelo item de erro malformado: %+v", e)
	}
	if e.Error != nil {
		t.Errorf("Error = %+v, quero nil — o item nao pode ser interpretado", e.Error)
	}
}

func TestParseWebhookReadsAReaction(t *testing.T) {
	payload := readCorpus(t, "reacao.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Reaction == nil {
		t.Fatal("Reaction == nil")
	}
	// "❤️" (U+2764 U+FE0F, with variation selector) is consumer-a's real
	// capture (2026-07-26) — see docs/ARMADILHAS.md. A single-codepoint
	// emoji wouldn't prove the variation selector survives the parser.
	if e.Reaction.Emoji != "❤️" {
		t.Errorf("Emoji = %q, quero ❤️", e.Reaction.Emoji)
	}
	if e.Reaction.Target != "wamid.TESTE001" {
		t.Errorf("Target = %q, quero wamid.TESTE001", e.Reaction.Target)
	}
}

// Meta's official doc
// (developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/reaction/,
// read on 2026-07-26) says "When an end user removes a reaction emoji, a
// webhook without the 'emoji' field will be sent." In other words, an
// ABSENT emoji in the payload is a LEGITIMATE signal (removal), not a
// malformed payload. Treating it as a counted parse error would silently
// erase that signal from the consumer — exactly the data-loss family
// T-023 exists to close. Confirmed by a REAL CAPTURE in T-026 (consumer-a,
// 2026-07-26): reacted and undid the same reaction 20s later, and the
// second event arrived without the "emoji" key — not "", not null: the
// key doesn't exist. See docs/ARMADILHAS.md.
func TestParseWebhookAReactionWithoutAnEmojiIsAValidRemovalNotAParseError(t *testing.T) {
	payload := readCorpus(t, "reacao_removida.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado (reacao sem emoji NAO e malformada): %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Reaction == nil {
		t.Fatal("Reaction == nil — remocao tambem e um evento, so sem emoji")
	}
	if e.Reaction.Emoji != "" {
		t.Errorf("Emoji = %q, quero vazio (remocao)", e.Reaction.Emoji)
	}
	if e.Reaction.Target != "wamid.TESTE001" {
		t.Errorf("Target = %q, quero wamid.TESTE001", e.Reaction.Target)
	}
}

// The target (message_id) does NOT have the same escape hatch: Meta
// always sends it, both on an added reaction and on a removed one.
// Without it the item is malformed.
func TestParseWebhookAReactionWithoutATargetIsACountedParseError(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[
	     {"from":"5511999990000","id":"wamid.RUIM","timestamp":"1769000000","type":"reaction","reaction":{"emoji":"👍"}},
	     {"from":"5511999990000","id":"wamid.BOA","timestamp":"1769000001","type":"text","text":{"body":"oi"}}
	   ]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — reacao sem alvo (message_id) e malformada, e a mensagem irma sobrevive", len(evs))
	}
	if evs[0].Reaction != nil {
		t.Errorf("a mensagem que sobreviveu nao devia ter Reaction: %+v", evs[0].Reaction)
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Errorf("err = %v, quero ErrPartialParse", err)
	}
}

// localizacao.json is consumer-a's real capture (2026-07-26), with
// coordinates rounded on purpose (masking, not capture noise). The real
// case is the BARE pin: WITHOUT a name/address — the old fixture, derived
// from the doc, had both and tested the rare case. See docs/ARMADILHAS.md.
func TestParseWebhookReadsALocation(t *testing.T) {
	payload := readCorpus(t, "localizacao.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Location == nil {
		t.Fatal("Location == nil")
	}
	if e.Location.Latitude != -21.229 {
		t.Errorf("Latitude = %v", e.Location.Latitude)
	}
	if e.Location.Longitude != -43.7892 {
		t.Errorf("Longitude = %v", e.Location.Longitude)
	}
	if e.Location.Name != "" {
		t.Errorf("Name = %q, quero vazio — a captura real nao trouxe nome", e.Location.Name)
	}
	if e.Location.Address != "" {
		t.Errorf("Address = %q, quero vazio — a captura real nao trouxe endereco", e.Location.Address)
	}
}

// Meta ALSO accepts a location with name/address (the "venue" pin,
// officially documented) — it's just not the common case observed in
// T-026's capture. This payload is SYNTHETIC (not part of the corpus,
// values invented on purpose) and exists just so the
// name/address-present path doesn't go untested after localizacao.json
// became the bare pin.
func TestParseWebhookReadsALocationWithNameAndAddress(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA_TESTE","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID_TESTE"},
	   "messages":[{"from":"5511999990000","id":"wamid.TESTESINT","timestamp":"1769000012",
	     "type":"location","location":{"latitude":1.5,"longitude":2.5,
	       "name":"Estabelecimento Sintetico","address":"Endereco Sintetico, 1"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Location == nil {
		t.Fatal("Location == nil")
	}
	if e.Location.Name != "Estabelecimento Sintetico" {
		t.Errorf("Name = %q", e.Location.Name)
	}
	if e.Location.Address != "Endereco Sintetico, 1" {
		t.Errorf("Address = %q", e.Location.Address)
	}
}

// THE GUARD IS THE ZERO DEFAULT ON A NUMERIC FIELD: 0 is a valid
// coordinate (the intersection of the Greenwich meridian and the
// equator). `omitempty` on a float64 erases zero as if it were absent —
// the same trap recorded in docs/ARMADILHAS.md for latitude/longitude on
// the SEND direction (T-024). This test proves the same guarantee on the
// RECEIVE direction, at the type level: direct marshal, without going
// through the parser.
func TestLocationSerializesZeroLatitudeLongitudeWithoutOmitting(t *testing.T) {
	l := Location{Latitude: 0, Longitude: 0}

	b, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := got["latitude"]; !ok {
		t.Fatal("latitude 0 sumiu do JSON — omitempty apagaria o meridiano de Greenwich")
	}
	if _, ok := got["longitude"]; !ok {
		t.Fatal("longitude 0 sumiu do JSON — omitempty apagaria o equador")
	}
}

func TestParseWebhookALocationWithoutAnObjectIsACountedParseError(t *testing.T) {
	// Unlike the reaction, Meta has no legitimate case of sending type
	// "location" without the "location" object — there's no "removal"
	// of a location. This item is genuinely malformed.
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[
	     {"from":"5511999990000","id":"wamid.RUIM","timestamp":"1769000000","type":"location"},
	     {"from":"5511999990000","id":"wamid.BOA","timestamp":"1769000001","type":"text","text":{"body":"oi"}}
	   ]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — localizacao sem o objeto e malformada, e a mensagem irma sobrevive", len(evs))
	}
	if evs[0].Location != nil {
		t.Errorf("a mensagem que sobreviveu nao devia ter Location: %+v", evs[0].Location)
	}
	if !errors.Is(err, ErrPartialParse) {
		t.Errorf("err = %v, quero ErrPartialParse", err)
	}
}

// TestParseWebhookVoiceTrue uses the SAME corpus as the two-mimes trap
// (2026-07-20): the audio already had "voice":true there, only the Voice
// field didn't exist on Event yet.
func TestParseWebhookVoiceTrue(t *testing.T) {
	payload := readCorpus(t, "audio_nota_de_voz.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Voice == nil || !*evs[0].Voice {
		t.Fatalf("Voice = %v, quero um *bool apontando para true", evs[0].Voice)
	}
}

// The INPUT half of the two-mimes trap (2026-07-20, docs/ARMADILHAS.md):
// an audio WITHOUT the "voice" field in Meta's payload cannot become
// "voz: false" by default. If it did, it would be indistinguishable from
// an EXPLICIT "voice: false" (a plain audio attachment) — and the
// consumer would lose the only way to know absence means "I don't know".
func TestParseWebhookAnAbsentVoiceDoesNotBecomeFalseByDefault(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.AUDIO_SEM_VOICE","timestamp":"1769000000",
	                "type":"audio","audio":{"id":"MEDIA_X","mime_type":"audio/ogg"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Voice != nil {
		t.Fatalf("Voice = %v, quero nil (ausente) — audio sem campo \"voice\" NAO pode virar false por default", *evs[0].Voice)
	}
}

// The counterpart: an EXPLICIT "voice": false (a plain audio attachment)
// has to come out DIFFERENT from absent — present and false, not omitted.
func TestParseWebhookAnExplicitFalseVoiceDiffersFromAbsent(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.AUDIO_ANEXO","timestamp":"1769000000",
	                "type":"audio","audio":{"id":"MEDIA_Y","mime_type":"audio/mpeg","voice":false}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Voice == nil {
		t.Fatal("Voice == nil, quero um *bool apontando para false (explicito, nao ausente)")
	}
	if *evs[0].Voice != false {
		t.Errorf("Voice = %v, quero false", *evs[0].Voice)
	}
}

func TestParseWebhookReadsCaptionAndFilename(t *testing.T) {
	payload := readCorpus(t, "documento_com_legenda.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Caption != "PDF teste" {
		t.Errorf("Caption = %q, quero \"PDF teste\"", evs[0].Caption)
	}
	if evs[0].Filename != "515642-9741-manual-forno-gourmet-grill-rev-43.pdf" {
		t.Errorf("Filename = %q, quero o nome real (longo, com hifens e numeros)", evs[0].Filename)
	}
}

// TestParseWebhookDoesNotRegressTheCurrent16Fields freezes the JSON
// serialization of the 16 fields that existed BEFORE T-023, and proves
// the five new fields (Reaction, Voice, Caption, Filename, Location)
// are ADDITIVE: a plain text message, which uses none of them, gains no
// new key in the JSON. It's omitempty that makes the envelope's public
// guarantee ("only grows, never changes what already exists") really
// hold.
func TestParseWebhookDoesNotRegressTheCurrent16Fields(t *testing.T) {
	payload := readCorpus(t, "mensagem_texto.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}

	b, err := json.Marshal(evs[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	want := map[string]any{
		"kind":            "message",
		"id":              "msg:wamid.TESTE001",
		"phone_number_id": "PNID_TESTE",
		"waba_id":         "WABA_TESTE",
		"timestamp":       float64(1769000000),
		"wa_message_id":   "wamid.TESTE001",
		"sub_kind":        "text",
		"from_raw":        "551199990000",
		"from_canonical":  "5511999990000",
		"contact_name":    "Fulana de Teste",
		"text":            "Teste",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("campo %q = %v, quero %v", k, got[k], v)
		}
	}

	// No omitted field (new or old) can leak when it doesn't apply — this
	// is what proves omitempty stays airtight.
	mustNotAppear := []string{
		"reaction", "voice", "caption", "file_name", "location",
		"button_payload", "button_text", "media_id", "media_mime_payload",
		"status", "to_raw", "to_canonical", "error", "reply_to",
	}
	for _, field := range mustNotAppear {
		if _, exists := got[field]; exists {
			t.Errorf("campo %q apareceu no JSON de uma mensagem de texto simples — omitempty quebrado", field)
		}
	}
	if len(got) != len(want) {
		t.Errorf("total de chaves no JSON = %d, quero %d — algum campo extra vazando", len(got), len(want))
	}
}

// TestParseWebhookAnUnknownFieldDoesNotBringDownTheParse is T-031's main value.
// Until now, the corpus only contained fields we already knew — so it
// could NEVER fail over a new field, which is exactly the scenario it
// exists for. Meta already sends "from_user_id" (on the message) and
// "user_id" (on each contact) in real traffic (consumer-a's capture,
// 2026-07-26, consumer-a-STATUS.local.md) without them appearing in the
// doc's classic examples. Today this breaks nothing by ACCIDENT of
// encoding/json (an unknown field is silently ignored by a struct without
// DisallowUnknownFields) — this test turns the accident into a guarantee:
// it goes red if the parser ever starts strictly validating fields.
//
// The GENERIC field ("campo_que_a_meta_ainda_nao_criou") covers the case
// that hasn't happened yet: any new key Meta might add, not just the two
// we've already observed.
//
// And the opposite guarantee, which the task itself requires: neither of
// the two personal-identity fields (user_id, from_user_id) can leak into
// the envelope — no one asked, and the envelope only grows by explicit
// decision, never by encoding/json's reflex.
func TestParseWebhookAnUnknownFieldDoesNotBringDownTheParse(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{
	    "metadata":{"phone_number_id":"PNID1"},
	    "contacts":[{"profile":{"name":"Maria"},"wa_id":"551199990000","user_id":"BR.10000000000000000"}],
	    "messages":[{"from":"551199990000","from_user_id":"BR.10000000000000000",
	                 "id":"wamid.GGG","timestamp":"1769000006","type":"text",
	                 "text":{"body":"oi"},
	                 "campo_que_a_meta_ainda_nao_criou":{"qualquer":"coisa"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v — campo desconhecido nao pode virar erro de parse", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	e := evs[0]
	if e.Text != "oi" {
		t.Errorf("Text = %q — o campo conhecido ao lado do desconhecido se perdeu", e.Text)
	}
	if e.FromRaw != "551199990000" || e.FromCanonical != "5511999990000" {
		t.Errorf("FromRaw=%q FromCanonical=%q", e.FromRaw, e.FromCanonical)
	}
	if e.ContactName != "Maria" {
		t.Errorf("ContactName = %q — o casamento por wa_id quebrou junto com o campo novo", e.ContactName)
	}

	// Do NOT add user_id/from_user_id to the envelope (T-031's decision):
	// it's personal-identity data, no one asked for it, and adding it
	// later is free — removing it later is a breaking change.
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, forbidden := range []string{"user_id", "from_user_id", "BR.", "campo_que_a_meta_ainda_nao_criou"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("envelope vazou %q: %s", forbidden, b)
		}
	}
}

// TestParseWebhookATemplateButtonCaptureHasPayloadEqualToText documents an
// observed fact, not a decision: in the real template quick-reply
// (consumer-a's capture, 2026-07-26), button.payload and button.text came
// EQUAL ("Falar com a gente"). That's why this fixture ALONE doesn't prove
// the parser reads the right field — swapping Payload for Text in the
// code would produce the SAME result here. See
// TestParseWebhookASyntheticTemplateButtonDistinguishesPayloadFromText, which
// is the test that catches that swap.
func TestParseWebhookATemplateButtonCaptureHasPayloadEqualToText(t *testing.T) {
	payload := readCorpus(t, "botao_de_template.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].ButtonPayload != "Falar com a gente" {
		t.Errorf("ButtonPayload = %q", evs[0].ButtonPayload)
	}
	if evs[0].ButtonPayload != evs[0].ButtonText {
		t.Errorf("ButtonPayload (%q) != ButtonText (%q) — a captura real tem os dois IGUAIS; "+
			"se isto mudou, o corpus_test.go precisa ser revisto", evs[0].ButtonPayload, evs[0].ButtonText)
	}
}

// TestParseWebhookASyntheticTemplateButtonDistinguishesPayloadFromText is the
// synthetic sibling the real capture can't be: with DIFFERENT payload and
// text, a swapped field read (m.Button.Text instead of m.Button.Payload)
// turns RED here and only here — that's why this fixture exists. It's the
// same family as the "leak test whose fixture erased the branch that
// would leak" in docs/ARMADILHAS.md: a comfortable fixture (payload ==
// text) would hide exactly the bug this test exists to catch.
// --- T-032: responder_a (Meta's context.id) ---

// TestParseWebhookReadsReplyTo is case (a) of T-032's Verify: a message
// with context.id produces responder_a. resposta_a_mensagem.json is
// consumer-a's real capture (2026-07-26): the owner replied quoting an
// earlier message, and the raw body brought context.from (the business's
// number) and context.id (the quoted message's wamid) — BOTH present and
// DIFFERENT, which makes this fixture also prove the mandatory mutation
// (see the next test).
func TestParseWebhookReadsReplyTo(t *testing.T) {
	payload := readCorpus(t, "resposta_a_mensagem.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].ReplyTo != "wamid.TESTE001" {
		t.Errorf("ReplyTo = %q, quero wamid.TESTE001 (o id do CONTEXT, nao o from)", evs[0].ReplyTo)
	}
}

// MANDATORY MUTATION of T-032, recorded in a test (not just in a manual
// experiment): context.from ("5532999990000") and context.id
// ("wamid.TESTE001") are TWO DIFFERENT VALUES in the same fixture — if the
// parser read m.Context.From instead of m.Context.ID, ReplyTo would
// come out "5532999990000" and this test goes red, because it compares
// the exact VALUE, not just the field's presence.
func TestParseWebhookReplyToReadsTheContextsIdNotItsFrom(t *testing.T) {
	payload := readCorpus(t, "resposta_a_mensagem.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].ReplyTo == "5532999990000" {
		t.Fatalf("ReplyTo = %q — leu context.FROM (o numero do negocio), nao context.id", evs[0].ReplyTo)
	}
	if evs[0].ReplyTo != "wamid.TESTE001" {
		t.Errorf("ReplyTo = %q, quero wamid.TESTE001", evs[0].ReplyTo)
	}
}

// Case (b) of the Verify: a message WITHOUT context doesn't gain the
// field — real omitempty, not an empty string visible in the JSON.
func TestParseWebhookWithoutContextGetsNoReplyTo(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.SEMCONTEXT","timestamp":"1769000000",
	                "type":"text","text":{"body":"oi"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].ReplyTo != "" {
		t.Errorf("ReplyTo = %q, quero vazio — mensagem sem context nao pode ganhar o campo", evs[0].ReplyTo)
	}

	b, err := json.Marshal(evs[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "responder_a") {
		t.Errorf("responder_a apareceu no JSON sem context: %s", b)
	}
}

// Case (c) of the Verify: context present but without "id" doesn't
// invent a value — comes out the same as the no-context-at-all case
// (there's no behavior difference to protect between the two, the same
// reason contextMeta isn't a pointer).
func TestParseWebhookAContextWithoutAnIdDoesNotInventReplyTo(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.CONTEXTSEMID","timestamp":"1769000000",
	                "type":"text","context":{"from":"5532999990000"},"text":{"body":"oi"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].ReplyTo != "" {
		t.Errorf("ReplyTo = %q, quero vazio — context sem id nao pode inventar valor", evs[0].ReplyTo)
	}
}

// T-059, the case that prompted the task: a CHAIN MESSAGE. When the
// message was passed along many times, Meta marks BOTH fields —
// encaminhada and encaminhada_muitas_vezes —, and it's the second one
// that lets the consumer NOT fire a business flow on top of a chain
// message.
//
// SYNTHETIC payload (no real capture exists; see Event.Forwarded in
// types.go). It's the corpus fixture's sibling: there the two fields come
// DIFFERENT from each other (true/false), here both come true — and it's
// the two cases together that prove each field is read from its own
// place. One alone wouldn't be enough: with both true, swapping the read
// of one for the other would pass green.
func TestParseWebhookAChainMessageMarksBothForwardingFields(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.CORRENTE1","timestamp":"1769000000",
	                "type":"text","context":{"forwarded":true,"frequently_forwarded":true},
	                "text":{"body":"REPASSE PARA 10 PESSOAS"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if !evs[0].Forwarded {
		t.Errorf("Forwarded = false, quero true")
	}
	if !evs[0].FrequentlyForwarded {
		t.Errorf("FrequentlyForwarded = false, quero true — e o sinal de corrente, a razao desta tarefa existir")
	}
}

// A message that was NOT forwarded gains no key at all in the envelope —
// not even `"encaminhada": false`. It's the same criterion as every field
// here (omitempty), and it's what backs the plain-bool decision: absent
// and false are the SAME answer, so the envelope only needs to carry the
// assertion.
//
// This test uses a message WITH context (a reply), on purpose: without
// that it would only prove "no context means no field", which is the
// easy case. The case that matters is context PRESENT, with an "id", and
// without the forwarding fields — which is how every normal reply
// arrives.
func TestParseWebhookANonForwardedReplyGetsNoForwardingKeys(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.RESPOSTA1","timestamp":"1769000000",
	                "type":"text","context":{"from":"5532999990000","id":"wamid.TESTE001"},
	                "text":{"body":"Recebido"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Forwarded || evs[0].FrequentlyForwarded {
		t.Errorf("Forwarded=%v FrequentlyForwarded=%v, quero os dois false",
			evs[0].Forwarded, evs[0].FrequentlyForwarded)
	}
	if evs[0].ReplyTo != "wamid.TESTE001" {
		t.Errorf("ReplyTo = %q — os campos novos nao podem atrapalhar a leitura do id", evs[0].ReplyTo)
	}
	b, err := json.Marshal(evs[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "encaminhada") {
		t.Errorf("\"encaminhada\" apareceu no JSON de uma mensagem nao encaminhada: %s", b)
	}
}

// --- T-061: an unexpected type in "context" degrades the BLOCK, never the message ---

// The test T-061 exists to make green, and it's not only worth it for
// the broken message: the BATCH has two messages, and before this task
// the first one (wrong-typed context) became `ignorados++` and VANISHED —
// with a 200 to Meta, which therefore never redelivers. The second one
// proves, in the same Unmarshal, that `responder_a` is still read when the
// type is right: the tolerance cannot mean "stopped reading the field".
//
// MANDATORY MUTATION (done and reverted before the commit): reverting
// messageMeta.Context to `contextMeta` (a plain struct) leaves this test
// RED — len(evs) drops to 1 and err becomes ErrPartialParse.
func TestParseWebhookAContextOfTheWrongTypeDeliversTheMessageWithoutCountingIgnored(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.CTXQUEBRADO","timestamp":"1769000000",
	                "type":"text","context":"wamid.TESTE001","text":{"body":"Recebido"}},
	               {"from":"5511999990000","id":"wamid.CTXOK","timestamp":"1769000001",
	                "type":"text","context":{"from":"5532999990000","id":"wamid.TESTE001"},
	                "text":{"body":"Tambem recebido"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if errors.Is(err, ErrPartialParse) {
		t.Fatalf("err = %v — a mensagem foi contada como ignorada; era exatamente isso que a fazia sumir", err)
	}
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("len(evs) = %d, quero 2 — a mensagem com context ilegivel tem de ser ENTREGUE", len(evs))
	}
	if evs[0].Text != "Recebido" {
		t.Errorf("Text = %q — a mensagem tem de chegar inteira, so sem o bloco", evs[0].Text)
	}
	if evs[0].ReplyTo != "" {
		t.Errorf("ReplyTo = %q, quero vazio — o bloco ilegivel nao pode ser adivinhado", evs[0].ReplyTo)
	}
	if evs[0].Forwarded || evs[0].FrequentlyForwarded {
		t.Errorf("Forwarded=%v FrequentlyForwarded=%v, quero os dois false",
			evs[0].Forwarded, evs[0].FrequentlyForwarded)
	}
	if evs[1].ReplyTo != "wamid.TESTE001" {
		t.Errorf("ReplyTo = %q na mensagem de tipo CERTO — tolerar o ilegivel nao pode virar parar de ler",
			evs[1].ReplyTo)
	}
}

// The other side of the same block: "context" is an object, but a field
// INSIDE it has an unexpected type ("id" a number, "forwarded" a string).
// The sibling file doesn't cover this case — there the Unmarshal doesn't
// even enter the block.
//
// The WHOLE block degrades, including the fields that were readable: it's
// the same shape statusEvent already uses for errors[0] and pricing
// (see messageBlock). "42" doesn't become the wamid "42" on purpose —
// inventing a wamid would make the consumer reply to a message that
// doesn't exist.
func TestParseWebhookAFieldOfTheWrongTypeInsideContextDoesNotBringDownTheMessage(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.CTXCAMPO","timestamp":"1769000000",
	                "type":"text","context":{"id":42,"forwarded":"sim"},
	                "text":{"body":"Recebido"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Text != "Recebido" {
		t.Errorf("Text = %q", evs[0].Text)
	}
	if evs[0].ReplyTo != "" {
		t.Errorf("ReplyTo = %q, quero vazio — 42 nao e um wamid", evs[0].ReplyTo)
	}
	if evs[0].Forwarded {
		t.Errorf("Forwarded = true — \"sim\" nao e um booleano e nao pode virar um")
	}
}

// THE CHOICE this test locks in: the block degrades WHOLE, even when
// encoding/json had already read a good field before hitting the bad one
// (it keeps decoding and only returns the UnmarshalTypeError at the end).
// Here "id" is valid and only "forwarded" is impossible — making use of
// the id would give one more `responder_a`, and the answer is still no.
//
// Two reasons, and the second is the deciding one: (1) it's the SAME
// shape statusEvent uses for errors[0] and pricing, and asymmetry
// between two places that solve the same problem is this project's
// mother defect (docs/ARMADILHAS.md); (2) "unreadable block = absent
// block" is a sentence that stays true when "context"'s fourth field gets
// modeled, while "make use of whatever comes through" changes meaning
// with every new field — and the consumer has no way to know which
// version is running.
func TestParseWebhookAnUnreadableContextDiscardsTheWHOLEBlockNotJustTheBadField(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.CTXMEIOBOM","timestamp":"1769000000",
	                "type":"text","context":{"id":"wamid.TESTE001","forwarded":"sim"},
	                "text":{"body":"Recebido"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Text != "Recebido" {
		t.Errorf("Text = %q — a mensagem continua sendo o que nao se perde", evs[0].Text)
	}
	if evs[0].ReplyTo != "" {
		t.Errorf("ReplyTo = %q, quero vazio — o bloco degrada inteiro, nao campo a campo", evs[0].ReplyTo)
	}
}

// "context"'s twin in the SAME defect, and it's older: "voice" has carried
// the fragile format since plan 1. A `"voice":"sim"` used to bring down the
// Unmarshal of the WHOLE message — not just the audio block.
//
// MANDATORY MUTATION: reverting mediaMeta.Voice to `*bool` leaves this
// test red (len(evs) = 0 and err = ErrPartialParse).
func TestParseWebhookAVoiceOfTheWrongTypeDeliversTheAudioWithVoiceAbsent(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.VOICEQUEBRADO","timestamp":"1769000000",
	                "type":"audio","audio":{"id":"MEDIA_Z","mime_type":"audio/ogg; codecs=opus",
	                                        "voice":"sim"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if errors.Is(err, ErrPartialParse) {
		t.Fatalf("err = %v — o audio foi contado como ignorado por causa de UM campo", err)
	}
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].MediaID != "MEDIA_Z" {
		t.Errorf("MediaID = %q — o resto do bloco de midia tem de sobreviver", evs[0].MediaID)
	}
	if evs[0].MediaMimePayload != "audio/ogg; codecs=opus" {
		t.Errorf("MediaMimePayload = %q — o parametro codecs se perdeu", evs[0].MediaMimePayload)
	}
	if evs[0].Voice != nil {
		t.Errorf("Voice = %v, quero nil — \"sim\" nao e booleano, e \"nao sei\" e a unica resposta honesta", *evs[0].Voice)
	}
}

// "voice": null has to come out EQUAL to an absent "voice" (nil), never
// false. The path changed in T-061 (the *bool became a json.RawMessage
// read by tolerantBool) and this is the case where a naive Unmarshal
// would get it wrong: `null` over a bool doesn't error and leaves false,
// which is a value with its own meaning in the contract ("a plain audio
// attachment").
func TestParseWebhookANullVoiceDoesNotBecomeFalse(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.VOICENULL","timestamp":"1769000000",
	                "type":"audio","audio":{"id":"MEDIA_W","mime_type":"audio/ogg","voice":null}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].Voice != nil {
		t.Fatalf("Voice = %v, quero nil — null e ausencia, nao \"voice: false\"", *evs[0].Voice)
	}
}

// --- T-062: the CLASS, not the five fields -----------------------------------

// The guard that makes a NEW field born protected, and it's the step
// missing from the three earlier rounds (T-043 entry.time, T-061
// context+voice, T-062 the family). Each shielded the field of the
// moment and left the siblings plain; none left behind anything that
// would go RED when the next field arrived.
//
// This test walks messageMeta by reflection and requires json.RawMessage
// on EVERY field. The day someone hangs an `Order *orderMeta` there, it
// fails naming the field — before the payload exists, before any consumer
// loses a message. It's the cheapest test in this task and the only one
// that covers what hasn't been written yet.
func TestMessageMetaIsolatesEveryFieldByConstruction(t *testing.T) {
	kind := reflect.TypeOf(messageMeta{})
	if kind.NumField() == 0 {
		t.Fatal("messageMeta sem campos — a guarda nao verificou NADA")
	}
	payload := reflect.TypeOf(json.RawMessage(nil))
	for i := 0; i < kind.NumField(); i++ {
		field := kind.Field(i)
		if field.Type != payload {
			t.Errorf("messageMeta.%s e %s, quero json.RawMessage — um campo que o "+
				"encoding/json possa RECUSAR aqui derruba o Unmarshal da mensagem "+
				"INTEIRA, e ela some de `eventos` (ver o comentario do tipo)",
				field.Name, field.Type)
		}
	}
}

// --- T-068: the same guard, at the levels ABOVE the message -------------------

// The SIBLINGS of the test above, and the reason they exist is in
// docs/ARMADILHAS.md's table: T-062 made the MESSAGE struct the boundary
// and stopped there, leaving five structs one level up with concrete
// types — where the radius of a failing Unmarshal isn't a message, it's
// the batch.
//
// Covers messageMeta again ON PURPOSE, alongside the other six: the
// overlap with TestMessageMetaIsolatesEveryFieldByConstruction costs
// microseconds and guarantees that deleting one of the two tests doesn't
// open up the whole class.
//
// MANDATORY MUTATION (done and reverted before the commit): reverting ANY
// field of ANY of these structs to a concrete type leaves this test red,
// naming struct AND field, before the payload exists.
func TestBoundaryStructsIsolateEveryFieldByConstruction(t *testing.T) {
	boundary := []any{
		envelopeMeta{},
		entryMeta{},
		changeMeta{},
		valueMeta{},
		messageMeta{},
		statusMeta{},
		templateStatusMeta{},
		templateCategoryMeta{}, // T-057
		numberQualityMeta{},    // T-058
		accountAlertMeta{},     // T-058
	}
	payload := reflect.TypeOf(json.RawMessage(nil))
	for _, sample := range boundary {
		kind := reflect.TypeOf(sample)
		t.Run(kind.Name(), func(t *testing.T) {
			if kind.NumField() == 0 {
				t.Fatalf("%s sem campos — a guarda nao verificou NADA", kind.Name())
			}
			for i := 0; i < kind.NumField(); i++ {
				field := kind.Field(i)
				if field.Type != payload {
					t.Errorf("%s.%s e %s, quero json.RawMessage — um campo que o "+
						"encoding/json possa RECUSAR aqui derruba o Unmarshal de %s "+
						"INTEIRO, e com ele tudo que estava dentro (ver o comentario "+
						"das structs de fronteira, em parse.go)",
						kind.Name(), field.Name, field.Type, kind.Name())
				}
			}
		})
	}
}

// twoEntryBatch: a COMPLETE payload — two entries, contacts, metadata,
// messages, and statuses — used by the all-levels sweep, below. The
// message from the SECOND entry is the witness: it has to arrive no
// matter what happens to the first.
const twoEntryBatch = `{"object":"whatsapp_business_account","entry":[
  {"id":"WABA_TESTE","time":1769000000,"changes":[
    {"field":"messages","value":{"messaging_product":"whatsapp",
     "metadata":{"display_phone_number":"5532999990000","phone_number_id":"PNID_TESTE"},
     "contacts":[{"profile":{"name":"Fulana"},"wa_id":"551199990000"}],
     "messages":[{"from":"551199990000","id":"wamid.VARRE1","timestamp":"1769000000","type":"text","text":{"body":"oi"}}],
     "statuses":[{"id":"wamid.VARRE2","status":"delivered","timestamp":"1769000001","recipient_id":"551199990000","pricing":{"billable":true,"category":"utility"}}]}}]},
  {"id":"WABA_TESTE","time":1769000002,"changes":[
    {"field":"messages","value":{"messaging_product":"whatsapp",
     "metadata":{"phone_number_id":"PNID_TESTE"},
     "messages":[{"from":"551199990000","id":"wamid.TESTEMUNHA","timestamp":"1769000002","type":"text","text":{"body":"Testemunha"}}]}}]}]}`

// The CLASS sweep at the upper levels, and it enumerates no struct at
// all: it walks the WHOLE payload by reflecting over the JSON — every
// key, of every object, of every level, of every list item — and swaps
// one at a time.
//
// The sentence it locks in is the strongest this parser can promise: NO
// field, at NO level, with an unexpected shape, can silence the batch.
// The witness sits in the SECOND entry, which is how Meta batches
// different accounts in the same call — and it's exactly the message
// docs/ARMADILHAS.md's Critical #1 describes as lost.
//
// WHY SWEEP THE PAYLOAD AND NOT A LIST OF STRUCTS (docs/ARMADILHAS.md,
// "toda lista escrita à mão sobre o esquema precisa de algo que
// pergunte ao esquema"): the boundary-structs list in the test above is
// hand-written and rots the day someone models a new level. This sweep
// asks the PAYLOAD, so a new level that shows up in a fixture enters on
// its own.
func TestParseWebhookNoFieldAtAnyLevelSilencesTheBatch(t *testing.T) {
	var payload any
	if err := json.Unmarshal([]byte(twoEntryBatch), &payload); err != nil {
		t.Fatalf("o proprio lote de teste nao e' JSON valido: %v", err)
	}

	// `"x"` covers "a string came where an object/list/bool/number is
	// expected"; `["x"]` covers the fields that are ALREADY a string, for
	// which `"x"` would be a valid shape and would prove nothing. The
	// same two from T-062's sweep.
	mutants := []string{`"x"`, `["x"]`}

	paths := jsonPaths(payload, "")
	if len(paths) == 0 {
		t.Fatal("nenhum caminho para mutar — a guarda nao verificou NADA")
	}
	// TWO EXCEPTIONS, and both are "you swapped the container that HOLDS
	// the witness", not "the parser silenced the batch":
	//   - "entry[1]..." is the witness itself;
	//   - "entry" is the LIST that contains it — swapping the root for
	//     `"x"` leaves no payload standing at all. That case has its own
	//     test right below (TestParseWebhookAnEntryOfTheWrongTypeDoesNotPanicAndCountsIgnored),
	//     because what's required of it is different: don't panic, and
	//     COUNT it.
	seen := 0
	for _, path := range paths {
		if path == "entry" || strings.HasPrefix(path, "entry[1]") {
			continue
		}
		for _, mutant := range mutants {
			seen++
			corrupted := withPathSwapped(t, twoEntryBatch, path, mutant)
			evs, err := ParseWebhook([]byte(corrupted))
			_ = err // ErrPartialParse e' legitimo aqui: perder um item e' o preco; calar o lote nao e'
			found := false
			for _, e := range evs {
				if e.WaMessageID == "wamid.TESTEMUNHA" && e.Text == "Testemunha" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s=%s: a testemunha do OUTRO entry sumiu (len(evs)=%d, err=%v)",
					path, mutant, len(evs), err)
			}
		}
	}
	if seen == 0 {
		t.Fatal("nenhuma mutacao foi aplicada — a guarda nao verificou NADA")
	}
	t.Logf("%d mutacoes aplicadas em %d caminhos do payload", seen, len(paths))
}

// The third case measured in valueMeta (docs/ARMADILHAS.md): an
// unexpected-type `contacts[].profile` gave len(evs) = 0 — ONE contact's
// profile erased the batch.
//
// It didn't become a corpus fixture because what it proves isn't the
// boundary (that's contacts_de_tipo_errado_sintetico.json): it's
// docs/ARMADILHAS.md's DEPTH rule one level below it. contactMeta is a
// PLAIN struct on purpose — the LIST is what isolates, and each item is
// deserialized separately —, so the price of an unreadable `profile` is
// THAT contact, and the OTHER customer's name in the same batch still
// arrives.
func TestParseWebhookAnUnreadableProfileCostsOnlyThatContact(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA_TESTE","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID_TESTE"},
	   "contacts":[{"profile":"Fulana","wa_id":"551199990000"},
	               {"profile":{"name":"Sicrana"},"wa_id":"553284630011"}],
	   "messages":[{"from":"551199990000","id":"wamid.SEMNOME","timestamp":"1769000000","type":"text","text":{"body":"oi"}},
	               {"from":"553284630011","id":"wamid.COMNOME","timestamp":"1769000001","type":"text","text":{"body":"oi"}}]}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil || len(evs) != 2 {
		t.Fatalf("err=%v len=%d, quero nil e 2 — um perfil ilegivel nao pode apagar mensagem nenhuma", err, len(evs))
	}
	if evs[0].ContactName != "" {
		t.Errorf("ContactName = %q, quero vazio — \"Fulana\" esta legivel a olho nu no bloco e ainda assim nao se adivinha", evs[0].ContactName)
	}
	if evs[1].ContactName != "Sicrana" {
		t.Errorf("ContactName da irma = %q, quero Sicrana — o contato ilegivel levou junto o vizinho", evs[1].ContactName)
	}
}

// The tree's root: an unexpected-type `"entry"` leaves nothing standing,
// and that's why it's the sweep's only exception, above. What's required
// here is what's still left: don't panic (the contract at the top of
// parse.go) and COUNT what was left behind, so the envelope's
// `parse_error` tells the consumer the gateway didn't understand the
// body — the `cru` still gets delivered by the caller
// (internal/inbound/handler.go).
//
// Until T-068 this case returned encoding/json's raw UnmarshalTypeError,
// which also reached the consumer. The difference is that now
// `envelopeMeta` follows the same rule as the other six boundary
// structs, and the day someone models `"object"` there with a concrete
// type won't be a special day.
func TestParseWebhookAnEntryOfTheWrongTypeDoesNotPanicAndCountsIgnored(t *testing.T) {
	for _, payload := range []string{
		`{"object":"whatsapp_business_account","entry":"x"}`,
		`{"object":"whatsapp_business_account","entry":42}`,
		`{"object":"whatsapp_business_account","entry":{"id":"WABA_TESTE"}}`,
	} {
		evs, err := ParseWebhook([]byte(payload))
		if !errors.Is(err, ErrPartialParse) {
			t.Errorf("%s: err = %v, quero ErrPartialParse", payload, err)
		}
		if len(evs) != 0 {
			t.Errorf("%s: len(evs) = %d, quero 0", payload, len(evs))
		}
	}
}

// jsonPaths returns the path of EVERY key of EVERY object in the
// document, at any depth, in the format
// "entry[0].changes[0].value.metadata".
func jsonPaths(cur any, prefix string) []string {
	var paths []string
	switch v := cur.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			paths = append(paths, path)
			paths = append(paths, jsonPaths(v[key], path)...)
		}
	case []any:
		for i, item := range v {
			paths = append(paths, jsonPaths(item, fmt.Sprintf("%s[%d]", prefix, i))...)
		}
	}
	return paths
}

// withPathSwapped returns the document with ONE path replaced by the
// given raw value — the rest as it was.
func withPathSwapped(t *testing.T, document, path, value string) string {
	t.Helper()
	var root any
	if err := json.Unmarshal([]byte(document), &root); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	var raw any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		t.Fatalf("Unmarshal do mutante %q: %v", value, err)
	}

	cur := root
	parts := strings.Split(path, ".")
	for i, part := range parts {
		name, indexes := part, []int{}
		for {
			openIdx := strings.LastIndex(name, "[")
			if openIdx < 0 || !strings.HasSuffix(name, "]") {
				break
			}
			var n int
			if _, err := fmt.Sscanf(name[openIdx:], "[%d]", &n); err != nil {
				t.Fatalf("indice ilegivel em %q", part)
			}
			indexes = append([]int{n}, indexes...)
			name = name[:openIdx]
		}
		object, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("caminho %q: %q nao e' objeto", path, strings.Join(parts[:i], "."))
		}
		if i == len(parts)-1 && len(indexes) == 0 {
			object[name] = raw
			break
		}
		cur = object[name]
		for _, n := range indexes {
			list, ok := cur.([]any)
			if !ok || n >= len(list) {
				t.Fatalf("caminho %q: indice %d fora de uma lista", path, n)
			}
			cur = list[n]
		}
	}

	b, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(b)
}

// messagesOfEachType: one GOOD message of each type the parser models,
// used by the class test, below. All of them carry `context`, because
// it's a sibling of every other block and T-061 proved a forgotten
// sibling costs the message.
var messagesOfEachType = map[string]string{
	"text":        `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"text","text":{"body":"oi"},"context":{"id":"wamid.TESTE001"}}`,
	"button":      `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"button","button":{"payload":"P9","text":"Falar"},"context":{"id":"wamid.TESTE001"}}`,
	"interactive": `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"interactive","interactive":{"type":"button_reply","button_reply":{"id":"confirmar","title":"Confirmar"}},"context":{"id":"wamid.TESTE001"}}`,
	"audio":       `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"audio","audio":{"id":"MEDIA_A","mime_type":"audio/ogg; codecs=opus","voice":true},"context":{"id":"wamid.TESTE001"}}`,
	"image":       `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"image","image":{"id":"MEDIA_I","mime_type":"image/jpeg","caption":"foto"},"context":{"id":"wamid.TESTE001"}}`,
	"video":       `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"video","video":{"id":"MEDIA_V","mime_type":"video/mp4"},"context":{"id":"wamid.TESTE001"}}`,
	"document":    `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"document","document":{"id":"MEDIA_D","mime_type":"application/pdf","filename":"n.pdf"},"context":{"id":"wamid.TESTE001"}}`,
	"sticker":     `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"sticker","sticker":{"id":"MEDIA_S","mime_type":"image/webp"},"context":{"id":"wamid.TESTE001"}}`,
	"reaction":    `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"reaction","reaction":{"message_id":"wamid.TESTE001","emoji":"❤️"},"context":{"id":"wamid.TESTE001"}}`,
	"location":    `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"location","location":{"latitude":-21.229,"longitude":-43.7892},"context":{"id":"wamid.TESTE001"}}`,
	"unsupported": `{"from":"551199990000","id":"wamid.MUT","timestamp":"1769000000","type":"unsupported","errors":[{"code":131051,"title":"Message type unknown"}],"context":{"id":"wamid.TESTE001"}}`,
}

// batchOfTwo wraps two raw messages in this corpus's standard envelope.
func batchOfTwo(a, b string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA_TESTE","changes":[` +
		`{"field":"messages","value":{"metadata":{"phone_number_id":"PNID_TESTE"},` +
		`"contacts":[{"profile":{"name":"Fulana"},"wa_id":"551199990000"}],` +
		`"messages":[` + a + `,` + b + `]}}]}]}`)
}

const healthySibling = `{"from":"551199990000","id":"wamid.IRMA","timestamp":"1769000099","type":"text","text":{"body":"Irma sa"}}`

// The CLASS test, and it doesn't enumerate the five fields T-062 found:
// it enumerates the KEYS THE PAYLOAD ITSELF has, type by type. A new key
// in an example message enters the sweep on its own — that's the
// difference between fixing the class and fixing a list.
//
// The sentence it locks in: NO field of a message, with an unexpected
// shape, can erase the message from the `eventos` list; and the SIBLINGS
// in the same batch arrive intact. Measured before the task with
// ParseWebhook: ANY field of messageMeta brought down the message,
// `len(evs)` dropped, and ErrPartialParse came back.
//
// The ONLY exception is `id`, and it's a decision — without an id there's
// no dedup key, and an event with ID "msg:" would collide with any other
// one with no id in the same batch. This is covered separately, right
// below, so that "the rule has an exception" is written instead of
// implied.
//
// MANDATORY MUTATION (done and reverted before the commit): reverting ANY
// field of messageMeta to a concrete type — `Text` for the anonymous
// struct, or `Reaction` for *reactionMeta — leaves this test red with
// `len(evs) = 1` for the corresponding type, and also leaves
// TestMessageMetaIsolatesEveryFieldByConstruction red, naming the field.
func TestParseWebhookNoFieldOfTheWrongTypeErasesTheMessageNorItsSiblings(t *testing.T) {
	// `"x"` covers "a string came where an object/bool/array is
	// expected"; `["x"]` covers the fields that are ALREADY strings
	// (from/id/type/timestamp), for which `"x"` would be a valid shape
	// and would prove nothing.
	mutants := []string{`"x"`, `["x"]`}

	kinds := make([]string, 0, len(messagesOfEachType))
	for kind := range messagesOfEachType {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(messagesOfEachType[kind]), &fields); err != nil {
				t.Fatalf("fixture do tipo %s nao e' objeto JSON: %v", kind, err)
			}
			keys := make([]string, 0, len(fields))
			for key := range fields {
				if key != "id" { // ver o teste da excecao, abaixo
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			if len(keys) == 0 {
				t.Fatal("nenhuma chave para mutar — a guarda nao verificou NADA")
			}

			for _, key := range keys {
				for _, mutant := range mutants {
					broken := withFieldSwapped(t, fields, key, mutant)
					evs, err := ParseWebhook(batchOfTwo(broken, healthySibling))
					if err != nil {
						t.Errorf("%s/%s=%s: err = %v — a mensagem foi contada como ignorada, "+
							"que e' exatamente o que a fazia sumir", kind, key, mutant, err)
						continue
					}
					if len(evs) != 2 {
						t.Errorf("%s/%s=%s: len(evs) = %d, quero 2", kind, key, mutant, len(evs))
						continue
					}
					if evs[0].WaMessageID != "wamid.MUT" {
						t.Errorf("%s/%s=%s: evs[0].WaMessageID = %q, quero wamid.MUT",
							kind, key, mutant, evs[0].WaMessageID)
					}
					if evs[1].WaMessageID != "wamid.IRMA" || evs[1].Text != "Irma sa" {
						t.Errorf("%s/%s=%s: a irma sa chegou como %q/%q",
							kind, key, mutant, evs[1].WaMessageID, evs[1].Text)
					}
				}
			}
		})
	}
}

// withFieldSwapped returns the message with ONE key replaced by the given
// raw value — the rest byte for byte as it was.
func withFieldSwapped(t *testing.T, fields map[string]json.RawMessage, key, value string) string {
	t.Helper()
	dup := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		dup[k] = v
	}
	dup[key] = json.RawMessage(value)
	b, err := json.Marshal(dup)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(b)
}

// THE EXCEPTION to the rule above, written instead of implied: an
// unexpected-type `id` still erases the message, and it's still
// `ignorados++`.
//
// It's not an oversight or a forgotten field: without the wamid there's
// no dedup key, and the event would come out with ID "msg:", colliding
// with any other message with no id in the same batch — the consumer
// would keep one and throw the others away thinking they were repeats.
// Delivering an event that can't be deduplicated is passive; the `cru`
// still reaches the consumer regardless, with `parse_error`.
//
// And `42` does NOT become the wamid "42" (a STRICT read, unlike
// entry.time): inventing a wamid would make the consumer reply to a
// message that doesn't exist.
func TestParseWebhookAnIdOfTheWrongTypeStillErasesTheMessage(t *testing.T) {
	broken := `{"from":"551199990000","id":42,"timestamp":"1769000000","type":"text","text":{"body":"oi"}}`

	evs, err := ParseWebhook(batchOfTwo(broken, healthySibling))
	if !errors.Is(err, ErrPartialParse) {
		t.Fatalf("err = %v, quero ErrPartialParse — a mensagem sem id tem de ser CONTADA", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1 — so a irma sa", len(evs))
	}
	if evs[0].WaMessageID != "wamid.IRMA" {
		t.Errorf("WaMessageID = %q, quero wamid.IRMA", evs[0].WaMessageID)
	}
}

// The other side of the ABSENT-block vs. UNREADABLE-block distinction,
// and it's what prevents T-062's tolerance from becoming "the parser
// accepts anything": the reaction and location guards still apply when
// META says there's no block. `null` is absence spelled out, not an
// unknown format — that's why it's still `ignorados++`.
//
// MUTATION (done and reverted before the commit): swapping the `return
// blockAbsent` on messageBlock's `p == nil` branch for `blockRead`
// leaves the `location` sub-test RED — `"location":null` becomes a
// Location at 0,0, a valid coordinate in the middle of the Atlantic —,
// and takes TestParseWebhookANullVoiceDoesNotBecomeFalse down with it (`voz:
// false` instead of absent).
//
// The `reaction` sub-test does NOT fall with that mutation, and the
// reason is worth writing: a zeroed reactionMeta has MessageID == "", and
// the "reaction without a target" guard catches the case regardless. It
// exists because it proves the SAME sentence through the opposite path —
// and it falls with the mutation that reverts messageMeta.Reaction to a
// concrete type.
func TestParseWebhookANullReactionOrLocationIsStillAbsence(t *testing.T) {
	cases := map[string]string{
		"reaction": `{"from":"551199990000","id":"wamid.NULO","timestamp":"1769000000","type":"reaction","reaction":null}`,
		"location": `{"from":"551199990000","id":"wamid.NULO","timestamp":"1769000000","type":"location","location":null}`,
	}
	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			evs, err := ParseWebhook(batchOfTwo(broken, healthySibling))
			if !errors.Is(err, ErrPartialParse) {
				t.Fatalf("err = %v, quero ErrPartialParse — null e' a Meta dizendo que nao ha bloco", err)
			}
			if len(evs) != 1 || evs[0].WaMessageID != "wamid.IRMA" {
				t.Fatalf("len(evs) = %d, evs[0] = %q — quero so a irma sa", len(evs), evs[0].WaMessageID)
			}
		})
	}
}

func TestParseWebhookASyntheticTemplateButtonDistinguishesPayloadFromText(t *testing.T) {
	payload := readCorpus(t, "botao_de_template_sintetico.json")

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("len(evs) = %d, quero 1", len(evs))
	}
	if evs[0].ButtonPayload != "PAYLOAD_INTERNO_9F3" {
		t.Errorf("ButtonPayload = %q, quero PAYLOAD_INTERNO_9F3", evs[0].ButtonPayload)
	}
	if evs[0].ButtonText != "Falar com a gente" {
		t.Errorf("ButtonText = %q, quero \"Falar com a gente\"", evs[0].ButtonText)
	}
	if evs[0].ButtonPayload == evs[0].ButtonText {
		t.Fatalf("ButtonPayload == ButtonText — o fixture sintetico perdeu a razao de existir " +
			"se os dois valores coincidirem")
	}
}

// --- AccountWabaIDsInPayload (T-038, 2026-07-26) ----------------------------
//
// Fixture based on
// developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/message_template_status_update/
// (read on 2026-07-26) — a real ACCOUNT webhook, with no
// metadata.phone_number_id at all.

// Verify: a `change` with field != "messages" returns entry[].id's waba_id.
func TestAccountWabaIDsInPayloadReadsAnAccountChange(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA_TESTE","time":1751247548,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"APPROVED","message_template_id":1689556908129832,
	    "message_template_name":"lembrete","message_template_language":"pt_BR"}}]}]}`)

	ids := AccountWabaIDsInPayload(payload)
	if len(ids) != 1 {
		t.Fatalf("len(ids) = %d, quero 1", len(ids))
	}
	if ids[0] != "WABA_TESTE" {
		t.Errorf("ids[0] = %q, quero WABA_TESTE", ids[0])
	}
}

// Verify: a `change` with field == "messages" (message OR status) does NOT
// count as an account change — it's exactly this distinction that lets
// the handler avoid touching the message path to close account routing.
func TestAccountWabaIDsInPayloadIgnoresAMessage(t *testing.T) {
	ids := AccountWabaIDsInPayload(testAccountPayload())
	if len(ids) != 0 {
		t.Fatalf("ids = %v, quero nenhum — o change e' field:\"messages\"", ids)
	}
}

// Verify: a batch with BOTH changes (normal message + account webhook)
// returns only the ACCOUNT change's waba_id — a message doesn't
// contaminate the list.
func TestAccountWabaIDsInPayloadMixesMessageAndAccount(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[
	  {"id":"WABA1","changes":[
	    {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	     "messages":[{"from":"5511999990000","id":"wamid.A","timestamp":"1","type":"text","text":{"body":"oi"}}]}}]},
	  {"id":"WABA_OUTRA","changes":[
	    {"field":"message_template_status_update","value":{"event":"APPROVED"}}]}
	]}`)

	ids := AccountWabaIDsInPayload(payload)
	if len(ids) != 1 {
		t.Fatalf("len(ids) = %d, quero 1 — so a mudanca de CONTA conta", len(ids))
	}
	if ids[0] != "WABA_OUTRA" {
		t.Errorf("ids[0] = %q, quero WABA_OUTRA", ids[0])
	}
}

// Verify: a body that isn't a JSON object (the same trap as ParseWebhook's,
// docs/ARMADILHAS.md) returns nil, never panics.
func TestAccountWabaIDsInPayloadAnInvalidBodyDoesNotPanic(t *testing.T) {
	for _, c := range []string{`null`, `42`, `[]`, `"texto"`, `true`, `{`} {
		if ids := AccountWabaIDsInPayload([]byte(c)); ids != nil {
			t.Errorf("corpo %q: ids = %v, quero nil", c, ids)
		}
	}
}

func testAccountPayload() []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.A","timestamp":"1769000000",
	                "type":"text","text":{"body":"oi"}}]}}]}]}`)
}

// --- Template status (T-043, 2026-07-26) ----------------------------------

// templateStatusPayload builds the template status webhook with the
// ENTRY's time as a variable — it's what decides the event's key.
func templateStatusPayload(stamp, event string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":` + stamp + `,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"` + event + `","message_template_id":1384121316897444,
	    "message_template_name":"aguardando_peca_v2","message_template_language":"pt_BR",
	    "reason":"NONE","message_template_category":"UTILITY"}}]}]}`)
}

// Case (b) of the Verify, and the ONLY test that justifies the key
// decision.
//
// The SAME template can be APPROVED more than once: approved, edited,
// back to pending, approved again. With the "obvious" key
// (template_status:{id}:{event}) the SECOND approval would have an id
// IDENTICAL to the first and the consumer would throw it out in dedup —
// the event would vanish with no signal at all, the most expensive way
// possible in this project.
//
// The task's MANDATORY MUTATION: removing ":" + time from the key in
// templateStatusEvent (internal/meta/parse.go) leaves this test RED,
// and only this one.
func TestParseWebhookTemplateStatusTwoApprovalsAtDifferentInstantsHaveDifferentIds(t *testing.T) {
	first, err := ParseWebhook(templateStatusPayload("1769000020", "APPROVED"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	second, err := ParseWebhook(templateStatusPayload("1769999999", "APPROVED"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("len = %d e %d, quero 1 e 1", len(first), len(second))
	}
	if first[0].ID == second[0].ID {
		t.Fatalf("os dois ids sao %q — a segunda aprovacao do MESMO template seria "+
			"deduplicada e sumiria no consumidor", first[0].ID)
	}
	// The other side of the same coin: repeating the SAME event at the
	// SAME instant (Meta's legitimate redelivery, or a malicious resend)
	// has to give the SAME id — otherwise the key stops being useful for
	// dedup.
	duplicate, err := ParseWebhook(templateStatusPayload("1769000020", "APPROVED"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if duplicate[0].ID != first[0].ID {
		t.Errorf("reentrega do mesmo evento deu id %q, quero %q — a chave tem de ser DETERMINISTICA",
			duplicate[0].ID, first[0].ID)
	}
}

// An ABSENT `reason` cannot become "NONE", and "NONE" cannot become
// absent: they're two different facts about what Meta said. Case (c) of
// the Verify, from both sides.
func TestParseWebhookTemplateStatusReasonNONEAndAbsenceAreDifferentThings(t *testing.T) {
	withNONE, err := ParseWebhook(templateStatusPayload("1769000020", "APPROVED"))
	if err != nil || len(withNONE) != 1 {
		t.Fatalf("err=%v len=%d", err, len(withNONE))
	}
	if withNONE[0].Template.Reason != "NONE" {
		t.Errorf("Reason = %q, quero a string NONE — a Meta a manda literalmente",
			withNONE[0].Template.Reason)
	}

	withoutReason := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":1769000020,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"REJECTED","message_template_id":1384121316897444,
	    "message_template_name":"aguardando_peca_v2","message_template_language":"pt_BR",
	    "message_template_category":"UTILITY"}}]}]}`)
	evs, err := ParseWebhook(withoutReason)
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d — reason ausente nao e payload malformado", err, len(evs))
	}
	if evs[0].Template.Reason != "" {
		t.Errorf("Reason = %q, quero vazio — a Meta nao mandou o campo", evs[0].Template.Reason)
	}
	b, err := json.Marshal(evs[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "motivo") {
		t.Errorf("a chave \"motivo\" apareceu num evento sem reason: %s", b)
	}
}

// Without `message_template_id` or without `event` the key would collide
// with any other equally empty change — the same guard (and the same
// reason) as messages' m.ID == "". Counted as ignored, never silently
// discarded.
func TestParseWebhookTemplateStatusWithoutAnIdOrWithoutAnEventCountsAsIgnored(t *testing.T) {
	cases := map[string]string{
		"sem id":     `{"event":"APPROVED","message_template_name":"x"}`,
		"sem event":  `{"message_template_id":1384121316897444,"message_template_name":"x"}`,
		"id null":    `{"event":"APPROVED","message_template_id":null}`,
		"value nulo": `null`,
	}
	for name, value := range cases {
		payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":1769000020,
		  "changes":[{"field":"message_template_status_update","value":` + value + `}]}]}`)

		evs, err := ParseWebhook(payload)
		if len(evs) != 0 {
			t.Errorf("%s: len(evs) = %d, quero 0", name, len(evs))
		}
		if !errors.Is(err, ErrPartialParse) {
			t.Errorf("%s: err = %v, quero ErrPartialParse — descartar em silencio e o que nao pode", name, err)
		}
	}
}

// Case (d) of the Verify — non-regression: message and status stay
// IDENTICAL when a template webhook arrives in the SAME batch. Meta
// batches `entry` from different accounts in the same call, so this is
// the realistic case, not the exotic one.
func TestParseWebhookTemplateStatusTouchesNeitherMessageNorStatus(t *testing.T) {
	onlyMessageAndStatus := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	   "messages":[{"from":"5511999990000","id":"wamid.M1","timestamp":"1769000000",
	                "type":"text","text":{"body":"oi"}}],
	   "statuses":[{"id":"wamid.S1","status":"delivered","timestamp":"1769000001",
	                "recipient_id":"551199990000"}]}}]}]}`)
	withTemplateToo := []byte(`{"object":"whatsapp_business_account","entry":[
	  {"id":"WABA1","changes":[
	   {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
	    "messages":[{"from":"5511999990000","id":"wamid.M1","timestamp":"1769000000",
	                 "type":"text","text":{"body":"oi"}}],
	    "statuses":[{"id":"wamid.S1","status":"delivered","timestamp":"1769000001",
	                 "recipient_id":"551199990000"}]}}]},
	  {"id":"WABA1","time":1769000020,"changes":[
	   {"field":"message_template_status_update","value":{
	     "event":"APPROVED","message_template_id":1384121316897444,
	     "message_template_name":"aguardando_peca_v2","message_template_language":"pt_BR",
	     "reason":"NONE","message_template_category":"UTILITY"}}]}]}`)

	before, err := ParseWebhook(onlyMessageAndStatus)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	after, err := ParseWebhook(withTemplateToo)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("len(antes) = %d, quero 2 (mensagem + status)", len(before))
	}
	if len(after) != 3 {
		t.Fatalf("len(depois) = %d, quero 3 (mensagem + status + template)", len(after))
	}
	for i := range before {
		a, _ := json.Marshal(before[i])
		d, _ := json.Marshal(after[i])
		if string(a) != string(d) {
			t.Errorf("evento %d mudou byte a byte com o template no lote:\nantes:  %s\ndepois: %s", i, a, d)
		}
	}
	if after[2].Type != EventTypeTemplateStatus {
		t.Errorf("o terceiro evento e %q, quero template_status", after[2].Type)
	}
}

// An account field the gateway does NOT model still doesn't become an
// event — its `value` has other keys, and interpreting it with a parser
// that isn't its own would produce an invented event. It still reaches
// the consumer through the `cru`, with `eventos: []`.
//
// 🔴 THIS TEST USED `template_category_update` AS ITS EXAMPLE, and the
// example's choice was T-057's defect written as a test: the field
// elected to illustrate "we don't read this" was precisely the event
// DEDICATED to the category change, which T-043 had gone to fetch from
// its neighbor. The test passed green while asserting, in plain terms,
// that the gateway wasn't listening on the right channel. Swapped for
// `account_review_update`, which still has no model by written decision
// (see docs/CONTRATO-CONSUMIDOR.md, "Webhook de CONTA").
func TestParseWebhookAnotherAccountFieldStaysOnlyInTheRawBody(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":1769000020,"changes":[
	  {"field":"account_review_update","value":{"decision":"APPROVED"}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("erro inesperado: %v — campo de conta desconhecido nao e parse parcial", err)
	}
	if len(evs) != 0 {
		t.Fatalf("len(evs) = %d, quero 0 — campo de conta sem modelo nao vira evento", len(evs))
	}
}

// --- Category reclassification (T-057, 2026-07-28) ------------------------

// templateCategoryPayload builds the `template_category_update`
// webhook with the ENTRY's time and the TRANSITION as variables — these
// are the two that decide the key.
func templateCategoryPayload(stamp, previous, newCategory string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":` + stamp + `,"changes":[
	  {"field":"template_category_update","value":{
	    "message_template_id":12345678,"message_template_name":"my_message_template",
	    "message_template_language":"en-US","previous_category":"` + previous + `",
	    "new_category":"` + newCategory + `","correct_category":"` + previous + `",
	    "category_appeal_status":"ELIGIBLE"}}]}]}`)
}

// The ONLY test that justifies the key decision, and it has three legs
// because the key has three pieces that can fall alone.
//
// A template can GO AND COME BACK from a category: UTILITY -> MARKETING
// today, MARKETING -> UTILITY next week, UTILITY -> MARKETING again.
// These are three distinct facts, and the third is the one that reopens
// the appeal window.
//
//   - without the TRANSITION in the key, going and coming back (same
//     template, same hypothetical second) collide;
//   - without the TIME in the key, the third transition collides with
//     the FIRST — they're the same transition at different instants, and
//     the consumer would throw away the one they can still contest;
//   - and the key has to stay DETERMINISTIC, or Meta's legitimate
//     redelivery (up to 36h) becomes a new event and the consumer's
//     dedup stops working.
func TestParseWebhookTemplateCategoryTwoTransitionsHaveDifferentIds(t *testing.T) {
	idOf := func(payload []byte) string {
		t.Helper()
		evs, err := ParseWebhook(payload)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if len(evs) != 1 {
			t.Fatalf("len(evs) = %d, quero 1", len(evs))
		}
		return evs[0].ID
	}

	there := idOf(templateCategoryPayload("1769000070", "UTILITY", "MARKETING"))
	back := idOf(templateCategoryPayload("1769000070", "MARKETING", "UTILITY"))
	if there == back {
		t.Errorf("ida e volta tem o mesmo id %q — sem a TRANSICAO na chave, encarecer e baratear viram o mesmo evento", there)
	}

	again := idOf(templateCategoryPayload("1769999999", "UTILITY", "MARKETING"))
	if again == there {
		t.Errorf("as duas UTILITY->MARKETING tem o mesmo id %q — sem o TEMPO na chave, a segunda reclassificacao "+
			"seria deduplicada e o consumidor nunca saberia que ha uma janela de recurso aberta", there)
	}

	redelivery := idOf(templateCategoryPayload("1769000070", "UTILITY", "MARKETING"))
	if redelivery != there {
		t.Errorf("reentrega do mesmo evento deu id %q, quero %q — a chave tem de ser DETERMINISTICA",
			redelivery, there)
	}
}

// Without `message_template_id` or without `new_category` the key would
// collide with any other equally empty change in the same batch — the
// same guard (and the same reason) as the template status's
// `message_template_id` + `event`. Counted as ignored, never silently
// discarded.
func TestParseWebhookTemplateCategoryWithoutAnIdOrWithoutANewCategoryCountsAsIgnored(t *testing.T) {
	cases := map[string]string{
		"sem id":            `{"new_category":"MARKETING","previous_category":"UTILITY"}`,
		"sem new_category":  `{"message_template_id":12345678,"previous_category":"UTILITY"}`,
		"id null":           `{"new_category":"MARKETING","message_template_id":null}`,
		"new_category null": `{"message_template_id":12345678,"new_category":null}`,
		"value nulo":        `null`,
	}
	for name, value := range cases {
		payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":1769000070,
		  "changes":[{"field":"template_category_update","value":` + value + `}]}]}`)

		evs, err := ParseWebhook(payload)
		if len(evs) != 0 {
			t.Errorf("%s: len(evs) = %d, quero 0", name, len(evs))
		}
		if !errors.Is(err, ErrPartialParse) {
			t.Errorf("%s: err = %v, quero ErrPartialParse — descartar em silencio e o que nao pode", name, err)
		}
	}
}

// The boundary-structs rule applied to this `value`: an unexpected-type
// field costs THAT field, never the event. The warning that the category
// changed — and the appeal window — arrive even with the direction half
// missing.
func TestParseWebhookTemplateCategoryAnUnreadableFieldDoesNotBringDownTheEvent(t *testing.T) {
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":1769000070,"changes":[
	  {"field":"template_category_update","value":{
	    "message_template_id":12345678,"message_template_name":{"a":1},
	    "previous_category":42,"new_category":"MARKETING",
	    "category_appeal_status":"ELIGIBLE"}}]}]}`)

	evs, err := ParseWebhook(payload)
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d, quero nil e 1 — campo ilegivel nao pode apagar o aviso de reclassificacao", err, len(evs))
	}
	c := evs[0].TemplateCategory
	if c == nil {
		t.Fatal("TemplateCategory == nil")
	}
	if c.NewCategory != "MARKETING" || c.AppealStatus != "ELIGIBLE" {
		t.Errorf("NewCategory=%q AppealStatus=%q — o que decide tem de sobreviver",
			c.NewCategory, c.AppealStatus)
	}
	// 42 does NOT become "42": a block that couldn't be read doesn't exist.
	if c.PreviousCategory != "" || c.Name != "" {
		t.Errorf("PreviousCategory=%q Name=%q, quero os dois vazios — bloco ilegivel nao se adivinha",
			c.PreviousCategory, c.Name)
	}
	if evs[0].ID != "template_categoria:12345678::MARKETING:1769000070" {
		t.Errorf("ID = %q — a chave sai com o pedaco ilegivel vazio, e nao inventado", evs[0].ID)
	}
}

// The TWO template events coexist and are NOT confused: a
// `template_category_update` cannot come out with the `template` block
// (which talks about approval state), and a
// `message_template_status_update` cannot come out with
// `template_categoria`. They're different vocabularies about the same
// object, and gluing one onto the other would make the consumer read
// "estado" off an event that isn't about state at all.
func TestParseWebhookTheTwoTemplateEventsDoNotMix(t *testing.T) {
	category, err := ParseWebhook(templateCategoryPayload("1769000070", "UTILITY", "MARKETING"))
	if err != nil || len(category) != 1 {
		t.Fatalf("err=%v len=%d", err, len(category))
	}
	if category[0].Template != nil {
		t.Errorf("Template = %+v num evento de categoria, quero nil", category[0].Template)
	}
	b, err := json.Marshal(category[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), `"template":`) {
		t.Errorf("a chave \"template\" apareceu num evento de categoria: %s", b)
	}

	status, err := ParseWebhook(templateStatusPayload("1769000020", "APPROVED"))
	if err != nil || len(status) != 1 {
		t.Fatalf("err=%v len=%d", err, len(status))
	}
	if status[0].TemplateCategory != nil {
		t.Errorf("TemplateCategory = %+v num evento de status, quero nil", status[0].TemplateCategory)
	}
	b, err = json.Marshal(status[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "template_categoria") {
		t.Errorf("a chave \"template_categoria\" apareceu num evento de status: %s", b)
	}
}

// The isolation guard (T-038) reads the RAW body, not the event list — so
// producing the event cannot have erased this webhook's waba_id check.
// The same proof the template status already had, on the new field.
func TestAccountWabaIDsInPayloadStillSeesATemplateCategoryThatBecameAnEvent(t *testing.T) {
	payload := templateCategoryPayload("1769000070", "UTILITY", "MARKETING")

	evs, err := ParseWebhook(payload)
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d, quero nil e 1", err, len(evs))
	}
	ids := AccountWabaIDsInPayload(payload)
	if len(ids) != 1 || ids[0] != "WABA1" {
		t.Fatalf("ids = %v, quero [WABA1] — a guarda de isolamento da T-038 parou de ver este webhook", ids)
	}
}

// --- Number quota/quality and account alert (T-058, 2026-07-28) ----------

func accountPayloadWithValue(field, value, stamp string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":` + stamp + `,
	  "changes":[{"field":"` + field + `","value":` + value + `}]}]}`)
}

// The same lesson that has already cost two keys in this file, on the
// third and fourth surfaces: a number's `event` values (FLAGGED,
// UNFLAGGED, FLAGGED again) and an account's `alert_type` values REPEAT
// over their lifetime. A key carrying only those would deduplicate the
// second occurrence.
func TestParseWebhookQualityAndAlertRepeatValuesAndNeedTheTimeInTheKey(t *testing.T) {
	idOf := func(payload []byte) string {
		t.Helper()
		evs, err := ParseWebhook(payload)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if len(evs) != 1 {
			t.Fatalf("len(evs) = %d, quero 1", len(evs))
		}
		return evs[0].ID
	}

	const flagged = `{"display_phone_number":"5532999990000","event":"FLAGGED",
	  "old_limit":"TIER_1K","current_limit":"TIER_50"}`
	first := idOf(accountPayloadWithValue(fieldNumberQuality, flagged, "1769000080"))
	second := idOf(accountPayloadWithValue(fieldNumberQuality, flagged, "1769999999"))
	if first == second {
		t.Errorf("dois FLAGGED do mesmo numero tem o mesmo id %q — o segundo rebaixamento seria "+
			"deduplicado e ninguem saberia que a cota caiu de novo", first)
	}
	if duplicate := idOf(accountPayloadWithValue(fieldNumberQuality, flagged, "1769000080")); duplicate != first {
		t.Errorf("reentrega deu id %q, quero %q — a chave tem de ser DETERMINISTICA", duplicate, first)
	}

	// The other half, and it's the argument for the severity in the key:
	// the SAME alert ESCALATING in severity is a new fact, not a repeat.
	const informational = `{"entity_type":"WABA","entity_id":123456,"alert_type":"OBA_APPROVED",
	  "alert_severity":"INFORMATIONAL","alert_status":"NONE"}`
	const severe = `{"entity_type":"WABA","entity_id":123456,"alert_type":"OBA_APPROVED",
	  "alert_severity":"CRITICAL","alert_status":"NONE"}`
	light := idOf(accountPayloadWithValue(fieldAccountAlert, informational, "1769000084"))
	heavy := idOf(accountPayloadWithValue(fieldAccountAlert, severe, "1769000084"))
	if light == heavy {
		t.Errorf("o alerta informativo e o CRITICAL tem o mesmo id %q — a escalada seria deduplicada "+
			"contra o aviso original, e ela e a unica das duas que exige acao", light)
	}
}

// The guard for the two ID-LESS events: it only rejects when NOTHING in
// the key distinguishes it. Rejecting for lack of an elected field
// (`alert_type`, for example) would throw away an alert that arrived with
// only severity — which is the field that makes the event worth having.
// See keyDistinguishesSomething, in parse.go.
func TestParseWebhookQualityAndAlertAreOnlyRefusedWhenNothingDistinguishes(t *testing.T) {
	refused := map[string][2]string{
		"qualidade sem nada":  {fieldNumberQuality, `{"max_daily_conversations_per_business":"TIER_250"}`},
		"qualidade vazia":     {fieldNumberQuality, `{}`},
		"qualidade nula":      {fieldNumberQuality, `null`},
		"qualidade nao-bloco": {fieldNumberQuality, `"TIER_250"`},
		"alerta so descricao": {fieldAccountAlert, `{"alert_description":"texto livre"}`},
		"alerta vazio":        {fieldAccountAlert, `{}`},
		"alerta nulo":         {fieldAccountAlert, `null`},
	}
	for name, tc := range refused {
		evs, err := ParseWebhook(accountPayloadWithValue(tc[0], tc[1], "1769000080"))
		if len(evs) != 0 {
			t.Errorf("%s: len(evs) = %d, quero 0", name, len(evs))
		}
		if !errors.Is(err, ErrPartialParse) {
			t.Errorf("%s: err = %v, quero ErrPartialParse — descartar em silencio e o que nao pode", name, err)
		}
	}

	// And the other side, which is what the guard exists to NOT do: one
	// piece alone already distinguishes it, and the event goes out.
	accepted := map[string][2]string{
		"alerta so com severidade": {fieldAccountAlert, `{"alert_severity":"CRITICAL"}`},
		"qualidade so com evento":  {fieldNumberQuality, `{"event":"FLAGGED"}`},
		"qualidade so com limite":  {fieldNumberQuality, `{"current_limit":"TIER_50"}`},
	}
	for name, tc := range accepted {
		evs, err := ParseWebhook(accountPayloadWithValue(tc[0], tc[1], "1769000080"))
		if err != nil {
			t.Errorf("%s: err = %v, quero nil", name, err)
		}
		if len(evs) != 1 {
			t.Fatalf("%s: len(evs) = %d, quero 1 — recusar isto perderia o unico campo que decide", name, len(evs))
		}
	}
}

// The boundary-structs rule on the two new `value`s: an unexpected-type
// field costs THAT field, never the event.
func TestParseWebhookQualityAndAlertAnUnreadableFieldDoesNotBringDownTheEvent(t *testing.T) {
	evs, err := ParseWebhook(accountPayloadWithValue(fieldNumberQuality,
		`{"display_phone_number":{"a":1},"event":"FLAGGED","old_limit":42,"current_limit":"TIER_50"}`,
		"1769000080"))
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d, quero nil e 1 — campo ilegivel nao pode apagar o aviso de rebaixamento", err, len(evs))
	}
	q := evs[0].NumberQuality
	if q == nil {
		t.Fatal("NumberQuality == nil")
	}
	if q.State != "FLAGGED" || q.CurrentLimit != "TIER_50" {
		t.Errorf("State=%q CurrentLimit=%q — o que decide tem de sobreviver", q.State, q.CurrentLimit)
	}
	// 42 does NOT become "42" in a limit: an invented tier is worse than
	// an absent tier, because whoever reads it believes it.
	if q.PreviousLimit != "" || q.DisplayNumber != "" {
		t.Errorf("PreviousLimit=%q DisplayNumber=%q, quero os dois vazios", q.PreviousLimit, q.DisplayNumber)
	}

	evs, err = ParseWebhook(accountPayloadWithValue(fieldAccountAlert,
		`{"entity_type":["WABA"],"entity_id":123456,"alert_severity":"CRITICAL","alert_description":{"x":1}}`,
		"1769000084"))
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d, quero nil e 1", err, len(evs))
	}
	a := evs[0].AccountAlert
	if a == nil {
		t.Fatal("AccountAlert == nil")
	}
	if a.Severity != "CRITICAL" || a.EntityID != "123456" {
		t.Errorf("Severity=%q EntityID=%q — o que decide tem de sobreviver", a.Severity, a.EntityID)
	}
	if a.EntityType != "" || a.Description != "" {
		t.Errorf("EntityType=%q Description=%q, quero os dois vazios", a.EntityType, a.Description)
	}
}

// The FOUR account events coexist without mixing: each one carries ITS
// OWN block and none of the other three. A consumer that read
// `template.categoria` on an alert event would be reading garbage — and
// the only defense against that is the block simply not existing.
func TestParseWebhookTheFourAccountEventsDoNotMix(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		block   string // a chave JSON que TEM de aparecer
	}{
		{"template_status", templateStatusPayload("1769000020", "APPROVED"), `"template":`},
		{"template_categoria", templateCategoryPayload("1769000070", "UTILITY", "MARKETING"), `"template_category":`},
		{"qualidade", accountPayloadWithValue(fieldNumberQuality,
			`{"event":"FLAGGED","current_limit":"TIER_50"}`, "1769000080"), `"number_quality":`},
		{"alerta", accountPayloadWithValue(fieldAccountAlert,
			`{"alert_type":"OBA_APPROVED","alert_severity":"CRITICAL"}`, "1769000084"), `"account_alert":`},
	}
	blocks := []string{`"template":`, `"template_category":`, `"number_quality":`, `"account_alert":`}

	for _, c := range cases {
		evs, err := ParseWebhook(c.payload)
		if err != nil || len(evs) != 1 {
			t.Fatalf("%s: err=%v len=%d", c.name, err, len(evs))
		}
		b, err := json.Marshal(evs[0])
		if err != nil {
			t.Fatalf("%s: Marshal: %v", c.name, err)
		}
		output := string(b)
		if !strings.Contains(output, c.block) {
			t.Errorf("%s: o bloco %s nao apareceu: %s", c.name, c.block, output)
		}
		for _, another := range blocks {
			if another == c.block {
				continue
			}
			if strings.Contains(output, another) {
				t.Errorf("%s: o bloco %s vazou para um evento que nao e dele: %s", c.name, another, output)
			}
		}
	}
}

// The isolation guard (T-038) reads the RAW body: producing an event for
// these two fields cannot have erased their waba_id check.
func TestAccountWabaIDsInPayloadStillSeesQualityAndAlert(t *testing.T) {
	for name, payload := range map[string][]byte{
		"qualidade": accountPayloadWithValue(fieldNumberQuality,
			`{"event":"FLAGGED","current_limit":"TIER_50"}`, "1769000080"),
		"alerta": accountPayloadWithValue(fieldAccountAlert,
			`{"alert_type":"OBA_APPROVED","alert_severity":"CRITICAL"}`, "1769000084"),
	} {
		evs, err := ParseWebhook(payload)
		if err != nil || len(evs) != 1 {
			t.Fatalf("%s: err=%v len=%d, quero nil e 1", name, err, len(evs))
		}
		ids := AccountWabaIDsInPayload(payload)
		if len(ids) != 1 || ids[0] != "WABA1" {
			t.Fatalf("%s: ids = %v, quero [WABA1] — a guarda de isolamento parou de ver este webhook", name, ids)
		}
	}
}

// Case (d) of the Verify, the other half: the non-matching waba_id
// discard (T-038) still holds for THIS SAME webhook, which now also
// becomes an event. The two things are deliberately independent — the
// isolation guard reads the raw body, not the event list —, and this
// test is what proves producing the event didn't erase the guard.
func TestAccountWabaIDsInPayloadStillSeesATemplateStatusThatBecameAnEvent(t *testing.T) {
	payload := templateStatusPayload("1769000020", "APPROVED")

	evs, err := ParseWebhook(payload)
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d, quero nil e 1", err, len(evs))
	}
	ids := AccountWabaIDsInPayload(payload)
	if len(ids) != 1 || ids[0] != "WABA1" {
		t.Fatalf("ids = %v, quero [WABA1] — a guarda de isolamento da T-038 parou de ver este webhook", ids)
	}
}

// entry.time with an unexpected TYPE cannot bring down the whole entry —
// that's why entryMeta.Time is json.RawMessage. Without it, the day Meta
// sends the timestamp in quotes (or as an object) would erase ALL the
// messages in that batch, from every consumer, and nothing would flag it.
//
// MUTATION: swapping `Time json.RawMessage` for `Time int64` in parse.go
// leaves the two cases below red.
func TestParseWebhookAnEntryTimeOfUnexpectedTypeDoesNotBringDownTheBatch(t *testing.T) {
	for _, stamp := range []string{`"1769000020"`, `{"quando":1769000020}`} {
		payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":` + stamp + `,"changes":[
		  {"field":"messages","value":{"metadata":{"phone_number_id":"PNID1"},
		   "messages":[{"from":"5511999990000","id":"wamid.SOBREVIVE","timestamp":"1769000000",
		                "type":"text","text":{"body":"oi"}}]}}]}]}`)

		evs, err := ParseWebhook(payload)
		if err != nil {
			t.Errorf("time=%s: erro inesperado: %v", stamp, err)
		}
		if len(evs) != 1 || evs[0].ID != "msg:wamid.SOBREVIVE" {
			t.Fatalf("time=%s: a mensagem do lote se perdeu por causa do carimbo do entry: %+v", stamp, evs)
		}
	}
}

// `time` in quotes still becomes the right timestamp (cheap tolerance, a
// sibling of tolerantInt in errors.go); a `time` that's neither a
// number nor number-text becomes 0 — and the event still goes out,
// because losing the event is worse than losing the distinction between
// two approvals (see templateStatusEvent).
func TestParseWebhookTemplateStatusToleratesTheTime(t *testing.T) {
	quoted, err := ParseWebhook(templateStatusPayload(`"1769000020"`, "APPROVED"))
	if err != nil || len(quoted) != 1 {
		t.Fatalf("err=%v len=%d", err, len(quoted))
	}
	if quoted[0].Timestamp != 1769000020 {
		t.Errorf("Timestamp = %d, quero 1769000020 — o carimbo entre aspas se perdeu", quoted[0].Timestamp)
	}

	withoutTime := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"APPROVED","message_template_id":1384121316897444}}]}]}`)
	evs, err := ParseWebhook(withoutTime)
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d — entry sem time nao pode APAGAR o evento", err, len(evs))
	}
	if evs[0].ID != "template_status:1384121316897444:APPROVED:0" {
		t.Errorf("ID = %q, quero o sufixo :0", evs[0].ID)
	}
}

// The template id can arrive as a number (the observed one) or in
// quotes; in both cases the key has to be the SAME, or the same event
// delivered twice in different shapes would escape the consumer's dedup.
func TestParseWebhookTemplateStatusAnIdAsNumberOrTextGivesTheSameKey(t *testing.T) {
	quoted := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","time":1769000020,"changes":[
	  {"field":"message_template_status_update","value":{
	    "event":"APPROVED","message_template_id":"1384121316897444",
	    "message_template_name":"aguardando_peca_v2","message_template_language":"pt_BR",
	    "reason":"NONE","message_template_category":"UTILITY"}}]}]}`)

	number, err := ParseWebhook(templateStatusPayload("1769000020", "APPROVED"))
	if err != nil || len(number) != 1 {
		t.Fatalf("err=%v len=%d", err, len(number))
	}
	text, err := ParseWebhook(quoted)
	if err != nil || len(text) != 1 {
		t.Fatalf("err=%v len=%d", err, len(text))
	}
	if number[0].ID != text[0].ID {
		t.Errorf("ids diferentes para o mesmo evento: %q e %q", number[0].ID, text[0].ID)
	}
}

// Envelope non-regression: a template event does NOT gain a message or
// status field, and a message does NOT gain "template". This is what
// makes the public guarantee ("the envelope only grows, never changes
// what already exists") hold for this new type too — a sibling of
// TestParseWebhookDoesNotRegressTheCurrent16Fields.
func TestParseWebhookTemplateStatusLeaksNoMessageFieldNorTheOtherWayAround(t *testing.T) {
	evs, err := ParseWebhook(templateStatusPayload("1769000020", "APPROVED"))
	if err != nil || len(evs) != 1 {
		t.Fatalf("err=%v len=%d", err, len(evs))
	}
	b, err := json.Marshal(evs[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := map[string]any{
		"kind":      "template_status",
		"id":        "template_status:1384121316897444:APPROVED:1769000020",
		"waba_id":   "WABA1",
		"timestamp": float64(1769000020),
		"template": map[string]any{
			"name":     "aguardando_peca_v2",
			"language": "pt_BR",
			"category": "UTILITY",
			"state":    "APPROVED",
			"reason":   "NONE",
		},
	}
	if len(got) != len(want) {
		t.Errorf("o evento de template tem %d chaves, quero %d: %s", len(got), len(want), b)
	}
	for k, v := range want {
		if !reflect.DeepEqual(got[k], v) {
			t.Errorf("%s = %#v, quero %#v", k, got[k], v)
		}
	}

	// And the other direction: a plain text message doesn't gain "template".
	msgs, err := ParseWebhook(readCorpus(t, "mensagem_texto.json"))
	if err != nil || len(msgs) != 1 {
		t.Fatalf("err=%v len=%d", err, len(msgs))
	}
	m, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(m), "template") {
		t.Errorf("\"template\" apareceu numa mensagem de texto: %s", m)
	}
}
