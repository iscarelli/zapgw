package meta

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const corpusDir = "../../testdata/corpus"

// What each corpus file has to produce.
var corpusExpectations = map[string]func(*testing.T, []Event, error){
	"mensagem_texto.json": func(t *testing.T, evs []Event, err error) {
		// consumer-a's real capture (2026-07-26, T-031): "from_user_id" (on
		// the message) and "contacts[].user_id" (format "BR.<digits>")
		// arrived over the wire and are NOT in the doc's classic examples.
		// err==nil and len==1 prove an unknown field doesn't bring down the
		// parse — but today that's an accident of encoding/json, not a
		// proven decision; see
		// TestParseWebhookAnUnknownFieldDoesNotBringDownTheParse for the
		// explicit guarantee, including that neither field leaks into the
		// envelope.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		if evs[0].Text != "Teste" {
			t.Errorf("Text = %q", evs[0].Text)
		}
		if evs[0].FromRaw != "551199990000" || evs[0].FromCanonical != "5511999990000" {
			t.Errorf("FromRaw=%q FromCanonical=%q — BOTH forms are mandatory",
				evs[0].FromRaw, evs[0].FromCanonical)
		}
	},
	"botao_de_template.json": func(t *testing.T, evs []Event, err error) {
		// consumer-a's real capture (2026-07-26, T-031): a real TEMPLATE
		// quick-reply, tapped on the device. payload and text came EQUAL
		// ("Falar com a gente") — which is exactly why this fixture ALONE
		// doesn't prove the parser reads the right field (swapped payload
		// and text would produce the SAME result here). The proof for
		// that is botao_de_template_sintetico.json, below.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].ButtonPayload != "Falar com a gente" {
			t.Errorf("ButtonPayload = %q — type \"button\" is not being read", evs[0].ButtonPayload)
		}
		if evs[0].ButtonText != "Falar com a gente" {
			t.Errorf("ButtonText = %q", evs[0].ButtonText)
		}
		// T-032: this fixture has carried "context" since T-031 (from and
		// id are DIFFERENT: "5532999990000" versus "wamid.TESTE013"),
		// without ever having been read. Independent proof of the same
		// guarantee as resposta_a_mensagem.json — reading context.from
		// instead of context.id here would also leave this test red.
		if evs[0].ReplyTo != "wamid.TESTE013" {
			t.Errorf("ReplyTo = %q, want wamid.TESTE013 (the context's id, not the from)", evs[0].ReplyTo)
		}
	},
	"resposta_a_mensagem.json": func(t *testing.T, evs []Event, err error) {
		// consumer-a's real capture (2026-07-26, T-032): a text message
		// replying to (quoting) another. context.from ("5532999990000")
		// and context.id ("wamid.TESTE001") are DIFFERENT on purpose —
		// that's what makes this fixture prove the task's mandatory
		// mutation (reading the wrong field swaps the value, not just
		// its presence).
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		if evs[0].ReplyTo != "wamid.TESTE001" {
			t.Errorf("ReplyTo = %q, want wamid.TESTE001 (the context's id, not the from)", evs[0].ReplyTo)
		}
		if evs[0].Text != "Recebido" {
			t.Errorf("Text = %q, want \"Recebido\"", evs[0].Text)
		}
	},
	"mensagem_encaminhada_sintetica.json": func(t *testing.T, evs []Event, err error) {
		// SYNTHETIC on purpose (T-059, 2026-07-28): no real capture of
		// these fields exists — `grep -rl forwarded testdata/corpus/`
		// found nothing before this file. The two fields come with
		// DIFFERENT values FROM EACH OTHER (forwarded:true,
		// frequently_forwarded:false) for the SAME reason
		// botao_de_template_sintetico.json exists: with both equal,
		// reading one field in place of the other would pass GREEN.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		if !evs[0].Forwarded {
			t.Errorf("Forwarded = false, want true — context.forwarded is not being read")
		}
		if evs[0].FrequentlyForwarded {
			t.Errorf("FrequentlyForwarded = true, want false — the two fields were swapped in the read")
		}
		// This fixture has "context" WITHOUT "id": forwarding isn't
		// quoting. If the two cases were glued together, a forwarded
		// message would gain an empty responder_a or, worse, the
		// consumer would conclude it's a reply to something.
		if evs[0].ReplyTo != "" {
			t.Errorf("ReplyTo = %q, want empty — forwarding is not quoting", evs[0].ReplyTo)
		}
	},
	// --- T-061: an unexpected type degrades the BLOCK, never the message ---
	//
	// The three files below are the corpus's only ones MALFORMED on
	// purpose. The assertion that matters in all of them is the same,
	// and it's the one that was inverted before T-061: `err == nil &&
	// len(evs) == 1`. An `err != nil` here is not a test detail — it's
	// the `ignorados++` that made the customer's message vanish, with a
	// 200 to Meta and no delivery to the consumer.
	"context_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// "context" came as a STRING where an OBJECT is expected.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1 — an unreadable context must NOT take the message down", err, len(evs))
		}
		// The message arrives WHOLE: what's lost is the block, not the rest.
		if evs[0].Text != "Recebido" {
			t.Errorf("Text = %q, want \"Recebido\" — the message has to survive the unreadable context", evs[0].Text)
		}
		if evs[0].WaMessageID != "wamid.TESTE018" {
			t.Errorf("WaMessageID = %q", evs[0].WaMessageID)
		}
		if evs[0].ReplyTo != "" {
			t.Errorf("ReplyTo = %q, want empty — the block cannot be guessed at", evs[0].ReplyTo)
		}
		if evs[0].Forwarded || evs[0].FrequentlyForwarded {
			t.Errorf("Forwarded=%v FrequentlyForwarded=%v, want both false",
				evs[0].Forwarded, evs[0].FrequentlyForwarded)
		}
	},
	"context_com_campo_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// "context" is an object, but INSIDE it "id" came as a NUMBER
		// where a string is expected and "forwarded" came as a STRING
		// where a bool is expected. This is the case the sibling file
		// does NOT cover: there the block isn't even entered.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1 — a field unreadable INSIDE context also does not take the message down", err, len(evs))
		}
		if evs[0].Text != "Recebido" {
			t.Errorf("Text = %q, want \"Recebido\"", evs[0].Text)
		}
		if evs[0].WaMessageID != "wamid.TESTE019" {
			t.Errorf("WaMessageID = %q", evs[0].WaMessageID)
		}
		// 42 CANNOT become "42": textFromNumberOrString exists for dedup
		// keys, not for a wamid — inventing a wamid from a number would
		// make the consumer reply to a message that doesn't exist.
		if evs[0].ReplyTo != "" {
			t.Errorf("ReplyTo = %q, want empty", evs[0].ReplyTo)
		}
		if evs[0].Forwarded {
			t.Errorf("Forwarded = true — \"sim\" is not a boolean and must not become one")
		}
	},
	"audio_voice_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// "context"'s twin in the SAME defect, since plan 1: "voice"
		// came as a STRING where a bool is expected, inside a media
		// block that goes through no isolation at all.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1 — an unreadable \"voice\" must NOT take the whole audio down", err, len(evs))
		}
		// What matters about the audio survives — including the mime
		// WITH its parameter, which is what makes the voice note exist
		// on the other side.
		if evs[0].MediaID != "MEDIA_TESTE9" {
			t.Errorf("MediaID = %q, want MEDIA_TESTE9", evs[0].MediaID)
		}
		if evs[0].MediaMimePayload != "audio/ogg; codecs=opus" {
			t.Errorf("MediaMimePayload = %q — the codecs parameter was lost", evs[0].MediaMimePayload)
		}
		// nil, not false: "I don't know" is the only honest answer, and
		// it's what stops the consumer from resending a voice note as a
		// plain attachment.
		if evs[0].Voice != nil {
			t.Errorf("Voice = %v, want nil — \"sim\" is not a boolean and must not become false", *evs[0].Voice)
		}
	},
	// --- T-062: the WHOLE FAMILY, one file per message TYPE ---
	//
	// The five files below have TWO messages each, and the second is
	// what T-061's three files didn't have: the HEALTHY SIBLING, from
	// the same batch. The assertion that holds in all of them is
	// `len(evs) == 2` — the unexpectedly-shaped message degrades and
	// arrives, and its sibling arrives intact. Before T-062, measured
	// with ParseWebhook, each of these five gave `len(evs) == 1` plus
	// ErrPartialParse: the broken message VANISHED from `eventos`.
	//
	// One file per type, and not a single batch with all five: a red
	// test has to say WHICH type broke (the same reason T-061's three
	// files exist).
	"texto_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// `"text":"oi"` — a string where an object is expected. It's the
		// scary case: text is the most common type of all, and an
		// unexpectedly-shaped text used to erase the system's most
		// banal message.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2 — the broken message AND the sibling have to arrive", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE021" {
			t.Errorf("WaMessageID = %q, want wamid.TESTE021 — the message with unreadable text vanished", evs[0].WaMessageID)
		}
		if evs[0].Text != "" {
			t.Errorf("Text = %q, want empty — the unreadable block cannot be guessed at", evs[0].Text)
		}
		// What survives is what identifies the message: without it the
		// delivered event would be good for nothing.
		if evs[0].SubType != "text" || evs[0].FromRaw != "551199990000" || evs[0].Timestamp != 1769000040 {
			t.Errorf("SubType=%q FromRaw=%q Timestamp=%d — the message's identity has to survive",
				evs[0].SubType, evs[0].FromRaw, evs[0].Timestamp)
		}
		if evs[1].Text != "Irma sa" {
			t.Errorf("the sibling's Text = %q — tolerating the unreadable must not turn into stopping reading", evs[1].Text)
		}
	},
	"audio_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// `"audio":"MEDIA_TESTE10"` — the WHOLE media block with the
		// wrong type, one level above the "voice" T-061 closed.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE023" {
			t.Errorf("WaMessageID = %q, want wamid.TESTE023", evs[0].WaMessageID)
		}
		// The id is NOT mined out of the unreadable block: "MEDIA_TESTE10"
		// sits right there, readable in plain sight, and still doesn't
		// become midia_id — a block that couldn't be read doesn't exist
		// (see messageBlock).
		if evs[0].MediaID != "" {
			t.Errorf("MediaID = %q, want empty — the block degrades whole, not field by field", evs[0].MediaID)
		}
		if evs[1].MediaID != "MEDIA_TESTE11" || evs[1].MediaMimePayload != "audio/ogg; codecs=opus" {
			t.Errorf("sibling: MediaID=%q Mime=%q", evs[1].MediaID, evs[1].MediaMimePayload)
		}
		if evs[1].Voice == nil || !*evs[1].Voice {
			t.Errorf("the sibling's Voice = %v, want true", evs[1].Voice)
		}
	},
	"interativo_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE025" {
			t.Errorf("WaMessageID = %q, want wamid.TESTE025", evs[0].WaMessageID)
		}
		if evs[0].ButtonPayload != "" || evs[0].ButtonText != "" {
			t.Errorf("ButtonPayload=%q ButtonText=%q, want both empty",
				evs[0].ButtonPayload, evs[0].ButtonText)
		}
		if evs[1].ButtonPayload != "confirmar" || evs[1].ButtonText != "Confirmar" {
			t.Errorf("sibling: ButtonPayload=%q ButtonText=%q", evs[1].ButtonPayload, evs[1].ButtonText)
		}
	},
	"reacao_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// The case that forced the distinction between an ABSENT block
		// and an UNREADABLE block (see messageEvent): the "reaction
		// without a target is malformed" guard still holds — and is
		// still proven by
		// TestParseWebhookAReactionWithoutATargetIsACountedParseError —, but it
		// cannot reach a block only WE failed to read.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2 — an unreadable reaction is not an absent reaction", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE027" {
			t.Errorf("WaMessageID = %q, want wamid.TESTE027", evs[0].WaMessageID)
		}
		// "wamid.TESTE001" is written in the block and does NOT become
		// the target: guessing the target would make the consumer
		// attribute the reaction to the wrong message.
		if evs[0].Reaction != nil {
			t.Errorf("Reaction = %+v, want nil — a reaction is not invented from unreadable bytes", evs[0].Reaction)
		}
		if evs[1].Reaction == nil || evs[1].Reaction.Target != "wamid.TESTE001" {
			t.Errorf("sibling: Reaction = %+v", evs[1].Reaction)
		}
	},
	"botao_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE029" {
			t.Errorf("WaMessageID = %q, want wamid.TESTE029", evs[0].WaMessageID)
		}
		if evs[0].ButtonPayload != "" || evs[0].Text != "" {
			t.Errorf("ButtonPayload=%q Text=%q, want both empty", evs[0].ButtonPayload, evs[0].Text)
		}
		// payload and text DIFFERENT in the sibling, for the same reason
		// botao_de_template_sintetico.json exists.
		if evs[1].ButtonPayload != "PAYLOAD_INTERNO_7C1" || evs[1].ButtonText != "Falar com a gente" {
			t.Errorf("sibling: ButtonPayload=%q ButtonText=%q", evs[1].ButtonPayload, evs[1].ButtonText)
		}
	},
	// --- T-068: the LEVELS ABOVE the message, one file per struct ---
	//
	// Synthetic on purpose (2026-07-28): no capture of any of these
	// formats exists, and there was no way there could be — these are
	// shapes Meta hasn't sent yet. What exists is the MEASUREMENT of the
	// damage, done with ParseWebhook before the task and recorded in
	// docs/ARMADILHAS.md: with the field swapped, each of these files
	// gave a SMALLER `len(evs)` and ErrPartialParse, and what vanished
	// wasn't a block — it was a whole customer's batch.
	//
	// The healthy sibling comes in the SAME batch in all of them, like
	// T-062's five: it's what separates "the parser tolerated it" from
	// "the parser stopped reading".
	"metadata_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// `"metadata":"PNID_TESTE"` — a string where an object is
		// expected. Measured before T-068: len(evs) = 0. `metadata`
		// sits at a level (valueMeta) that carries ALL the messages and
		// ALL the statuses of that change, so an unexpectedly-shaped
		// phone_number_id erased the customer's whole batch — messages
		// and statuses together.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2 (the message AND the status of the same change)", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE031" || evs[0].Text != "Mensagem sa" {
			t.Errorf("evs[0] = %q/%q — the change's message vanished", evs[0].WaMessageID, evs[0].Text)
		}
		if evs[1].Type != EventTypeStatus || evs[1].WaMessageID != "wamid.TESTE032" {
			t.Errorf("evs[1] = %q/%q — the status of the SAME change vanished", evs[1].Type, evs[1].WaMessageID)
		}
		// The block degrades whole: "PNID_TESTE" sits right there,
		// readable in plain sight, and still doesn't become
		// phone_number_id (the same rule as
		// audio_de_tipo_errado_sintetico.json).
		for i, e := range evs {
			if e.PhoneNumberID != "" {
				t.Errorf("evs[%d].PhoneNumberID = %q, want empty — a block that couldn't be read doesn't exist", i, e.PhoneNumberID)
			}
		}
		// contacts is still read: what degrades is the block, not its sibling.
		if evs[0].ContactName != "Fulana de Teste" {
			t.Errorf("ContactName = %q — the unreadable metadata took contacts down with it", evs[0].ContactName)
		}
	},
	"contacts_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// `"contacts":"Fulana de Teste"` — the most expensive of the
		// five measured cases, and the reason this task exists: a
		// contacts of a new shape erased a customer's WHOLE message
		// batch, silently, with a 200 answered to Meta. It's
		// docs/ARMADILHAS.md's Critical #1 under another name.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2 — unreadable contacts must not erase any message", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE033" || evs[0].Text != "Mensagem sa" {
			t.Errorf("evs[0] = %q/%q", evs[0].WaMessageID, evs[0].Text)
		}
		if evs[1].WaMessageID != "wamid.TESTE034" {
			t.Errorf("evs[1] = %q — the same change's status vanished", evs[1].WaMessageID)
		}
		// The envelope loses the PROFILE, not the message — the task's
		// sentence, turned into an assertion.
		if evs[0].ContactName != "" {
			t.Errorf("ContactName = %q, want empty — the name of an unreadable block isn't guessed at", evs[0].ContactName)
		}
		if evs[0].PhoneNumberID != "PNID_TESTE" {
			t.Errorf("PhoneNumberID = %q — the unreadable contacts took metadata down with it", evs[0].PhoneNumberID)
		}
	},
	"field_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// `"field":42` on the FIRST change; the second is healthy.
		// Measured before T-068: len(evs) = 0 — the whole change died.
		//
		// IT'S THE ONLY ONE OF THE SIX THAT STILL RETURNS
		// ErrPartialParse, on purpose: `field` is the field by which the
		// change is CLASSIFIED, and without it there's no way to know
		// whether that `value` was a template webhook we failed to
		// model. The messages arrive (best effort), and `parse_error`
		// says something couldn't be classified.
		if !errors.Is(err, ErrPartialParse) {
			t.Fatalf("err = %v, want ErrPartialParse — an unreadable `field` has to be COUNTED", err)
		}
		if len(evs) != 2 {
			t.Fatalf("len(evs) = %d, want 2 — the messages of BOTH changes have to arrive", len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE035" {
			t.Errorf("evs[0] = %q — the message of the change without a readable field vanished", evs[0].WaMessageID)
		}
		if evs[1].WaMessageID != "wamid.TESTE036" || evs[1].Text != "Irma sa" {
			t.Errorf("evs[1] = %q/%q", evs[1].WaMessageID, evs[1].Text)
		}
	},
	"entry_id_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// `"id":42` on the FIRST entry (the waba_id), with a SECOND
		// healthy entry — which is how Meta batches different accounts
		// in the same call. Measured before T-068: the whole entry
		// vanished.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2 — the entry with an unreadable waba_id also delivers", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE037" {
			t.Errorf("evs[0] = %q — the message of the entry without a readable waba_id vanished", evs[0].WaMessageID)
		}
		// "" is what the handler's guard 5b treats as a NON-MATCH
		// (T-068). The event exists; who decides whether it leaves this
		// house is the handler, with an ALARM and a counter — see
		// TestHandlerRecusaWebhookDeContaComWabaIDIlegivel.
		if evs[0].WabaID != "" {
			t.Errorf("evs[0].WabaID = %q, want empty — 42 does not become the waba_id \"42\"", evs[0].WabaID)
		}
		if evs[1].WaMessageID != "wamid.TESTE038" || evs[1].WabaID != "WABA_TESTE" {
			t.Errorf("evs[1] = %q/%q — the sibling from ANOTHER entry has to arrive intact",
				evs[1].WaMessageID, evs[1].WabaID)
		}
	},
	"status_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// messageMeta's direct sibling, in the SAME loop, and the most
		// embarrassing of the five: an unexpectedly-shaped `status`,
		// `recipient_id`, and `timestamp` erased the whole status event.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2", err, len(evs))
		}
		if evs[0].WaMessageID != "wamid.TESTE039" {
			t.Errorf("evs[0] = %q — the degraded status vanished", evs[0].WaMessageID)
		}
		if evs[0].Status != "" || evs[0].ToRaw != "" || evs[0].ToCanonical != "" {
			t.Errorf("Status=%q ToRaw=%q ToCanonical=%q, want all three empty — 42 does not become \"42\"",
				evs[0].Status, evs[0].ToRaw, evs[0].ToCanonical)
		}
		// The timestamp is the tolerant EXCEPTION (see statusFromMeta): a
		// number and text give the same instant, so it survives.
		if evs[0].Timestamp != 1769000058 {
			t.Errorf("Timestamp = %d, want 1769000058 — a number and text give the same instant", evs[0].Timestamp)
		}
		if evs[1].WaMessageID != "wamid.TESTE040" || evs[1].Status != "delivered" {
			t.Errorf("evs[1] = %q/%q — the sibling status has to arrive intact", evs[1].WaMessageID, evs[1].Status)
		}
	},
	"template_de_tipo_errado_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// `"message_template_name":42` and `"reason":{...}` — the second
		// change is healthy. Measured before T-068: len(evs) = 0, i.e.
		// the reclassification warning T-043 exists to give vanished
		// because of a field that doesn't even enter the decision.
		if err != nil || len(evs) != 2 {
			t.Fatalf("err=%v len=%d, want nil and 2", err, len(evs))
		}
		if evs[0].Template == nil {
			t.Fatal("evs[0].Template == nil — the degraded template event vanished")
		}
		// What decides (state and category) survives; what degraded is
		// just what came in unreadable.
		if evs[0].Template.State != "REJECTED" || evs[0].Template.Category != "MARKETING" {
			t.Errorf("State=%q Category=%q — what makes this event worth having has to survive",
				evs[0].Template.State, evs[0].Template.Category)
		}
		if evs[0].Template.Name != "" || evs[0].Template.Reason != "" {
			t.Errorf("Name=%q Reason=%q, want both empty — an unreadable block isn't guessed at",
				evs[0].Template.Name, evs[0].Template.Reason)
		}
		if evs[0].ID != "template_status:9900000000000001:REJECTED:1769000060" {
			t.Errorf("ID = %q — the key has to survive whole", evs[0].ID)
		}
		if evs[1].Template == nil || evs[1].Template.Name != "irma_sa_v1" || evs[1].Template.Reason != "NONE" {
			t.Errorf("evs[1].Template = %+v — the healthy sibling of the same batch", evs[1].Template)
		}
	},
	"botao_de_template_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// Synthetic, on purpose (T-031): payload and text DIFFERENT —
		// "PAYLOAD_INTERNO_9F3" versus "Falar com a gente". The real
		// capture (botao_de_template.json) has both equal, so it alone
		// doesn't catch a swapped field read (see the comment further up
		// and the "A leak test passes GREEN when the fixture erases the
		// branch that would leak" family in docs/ARMADILHAS.md). This fixture is what
		// makes the mutation "swap m.Button.Payload for m.Button.Text"
		// turn RED.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].ButtonPayload != "PAYLOAD_INTERNO_9F3" {
			t.Errorf("ButtonPayload = %q, want PAYLOAD_INTERNO_9F3", evs[0].ButtonPayload)
		}
		if evs[0].ButtonText != "Falar com a gente" {
			t.Errorf("ButtonText = %q, want \"Falar com a gente\"", evs[0].ButtonText)
		}
	},
	"botao_interativo.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		// consumer-a's real capture (2026-07-26, T-033): button_reply.id
		// ("confirmar") and button_reply.title ("Confirmar") come
		// DIFFERENT — they only differ in capitalization, but enough to
		// distinguish a swapped field read on its own (Go's string
		// comparison is case-sensitive). That's why this fixture does
		// NOT need a synthetic sibling like
		// botao_de_template_sintetico.json needed.
		if evs[0].ButtonPayload != "confirmar" {
			t.Errorf("ButtonPayload = %q, want \"confirmar\" (the id, not the title)", evs[0].ButtonPayload)
		}
		if evs[0].ButtonText != "Confirmar" {
			t.Errorf("ButtonText = %q, want \"Confirmar\"", evs[0].ButtonText)
		}
		if evs[0].ButtonPayload == evs[0].ButtonText {
			t.Fatalf("ButtonPayload == ButtonText — they lost the ability to catch a swapped field read")
		}
	},
	"audio_nota_de_voz.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].MediaMimePayload != "audio/ogg; codecs=opus" {
			t.Errorf("MediaMimePayload = %q — the codecs parameter was cut, and it's what makes the voice note exist",
				evs[0].MediaMimePayload)
		}
	},
	// --- T-069: REAL `sent` and `delivered`, measured by consumer-a ---
	//
	// The three files below are a CAPTURE (2026-07-28, a corpus of 267
	// raw consumer-a payloads) and replace the doc-derived
	// `status_delivered.json` that used to live here — they don't
	// coexist with it, for the reason written in
	// testdata/corpus/README.md: two fixtures for the same status, one
	// real and one invented, is an invitation to test against the wrong
	// one.
	//
	// MASKED, and CONSISTENTLY across the three: the same real number
	// always becomes 553288888888, the same user_id always becomes
	// BR.20000000000000000, and the wamid the `sent` WITH pricing and the
	// `delivered` shared in the capture becomes the same wamid.TESTE042
	// in both. Without that consistency the correlation test below
	// (TestCorpusSentAndDeliveredOfTheSameWamidHaveTheSameTimestamp) would stop
	// making sense. The TIMESTAMPS are Meta's real ones, on purpose: they
	// identify no one and are the fact the capture proved.
	"status_sent_sem_pricing.json": func(t *testing.T, evs []Event, err error) {
		// The finding a hand-written fixture would never have: `pricing`
		// is OPTIONAL on `sent` — 4 of 53 raw `sent` came without the
		// block (~7.5%). A `sent` without billing is NOT an error, it's
		// the normal case; whoever counts by billing category (T-063)
		// needs to know this BEFORE writing the counter, not after.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		if evs[0].ID != "status:wamid.TESTE041:sent" {
			t.Errorf("ID = %q — the composite key is mandatory", evs[0].ID)
		}
		if evs[0].Status != "sent" {
			t.Errorf("Status = %q, want sent", evs[0].Status)
		}
		// The task's assertion: absent, never zeroed. This is the one
		// that turns RED if someone makes the parse require `pricing`.
		if evs[0].Billing != nil {
			t.Errorf("Billing = %+v, want nil — `pricing` is optional on sent, and this is the real case without it", evs[0].Billing)
		}
		if evs[0].Error != nil {
			t.Errorf("Error = %+v, want nil — sent has no errors[]", evs[0].Error)
		}
		// `recipient_user_id` (and `contacts[].user_id`) are keys
		// statusFromMeta/contactMeta do NOT model and that arrived over the
		// real wire, in 152 of 225 raw payloads. err==nil and len==1
		// here PROVE an unknown key doesn't bring down the status
		// parse — which until this task was an assumption. That they
		// also don't LEAK into the envelope is in
		// TestCorpusARealStatusDoesNotLeakRecipientUserIDIntoTheEnvelope,
		// below.
		if evs[0].ToRaw != "553288888888" || evs[0].ToCanonical != "5532988888888" {
			t.Errorf("ToRaw=%q ToCanonical=%q — BOTH forms are mandatory",
				evs[0].ToRaw, evs[0].ToCanonical)
		}
		if evs[0].Timestamp != 1785073298 {
			t.Errorf("Timestamp = %d, want 1785073298 (Meta's real timestamp)", evs[0].Timestamp)
		}
	},
	"status_sent_com_pricing.json": func(t *testing.T, evs []Event, err error) {
		// The common shape: 49 of 53 raw `sent` came WITH `pricing`.
		// It's this file's pairing with the one above that makes
		// "optional" a proven assertion instead of a sentence in the
		// README.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		if evs[0].ID != "status:wamid.TESTE042:sent" {
			t.Errorf("ID = %q", evs[0].ID)
		}
		if evs[0].Billing == nil {
			t.Fatal("Billing == nil — status com pricing tem de produzir Billing")
		}
		// `service` and `billable:false` are this capture's REAL values,
		// and both differ from the only billing fixture that existed
		// (status_read_com_cobranca.json: `utility` and `billable:true`)
		// — swapping the read of one field for the other no longer
		// slips by unnoticed.
		if evs[0].Billing.Category != "service" {
			t.Errorf("Category = %q, want service", evs[0].Billing.Category)
		}
		if evs[0].Billing.Billable == nil {
			t.Fatal("Billable == nil — Meta said `billable:false`, and absent is NOT the same information")
		}
		if *evs[0].Billing.Billable {
			t.Errorf("Billable = true, want false — `billable:false` must not become true")
		}
		if evs[0].Timestamp != 1785072102 {
			t.Errorf("Timestamp = %d, want 1785072102", evs[0].Timestamp)
		}
	},
	"status_delivered.json": func(t *testing.T, evs []Event, err error) {
		// A REAL CAPTURE since T-069 (before it was doc-derived — the
		// only such status fixture). The real `delivered` from
		// consumer-a's corpus comes WITH `pricing` (49 of 49), no
		// `conversation` block, and with the SAME wamid and the SAME
		// timestamp as the `sent` above.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].ID != "status:wamid.TESTE042:delivered" {
			t.Errorf("ID = %q — the composite key is mandatory", evs[0].ID)
		}
		// Non-regression (T-028): a delivered status doesn't gain Error
		// just because the type now exists.
		if evs[0].Error != nil {
			t.Errorf("Error = %+v, want nil — delivered has no errors[]", evs[0].Error)
		}
		// The "no pricing -> absent Billing" non-regression this file
		// carried until T-041 MOVED ADDRESS, and didn't vanish: it now
		// lives in status_sent_sem_pricing.json and in
		// status_failed.json, both backed by a capture. Here Meta sent
		// `pricing`, and it has to arrive.
		if evs[0].Billing == nil {
			t.Fatal("Billing == nil — o delivered real veio COM pricing")
		}
		if evs[0].Billing.Category != "service" {
			t.Errorf("Category = %q, want service", evs[0].Billing.Category)
		}
		if evs[0].Timestamp != 1785072102 {
			t.Errorf("Timestamp = %d, want 1785072102", evs[0].Timestamp)
		}
	},
	"status_failed.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].Status != "failed" {
			t.Errorf("Status = %q, want failed", evs[0].Status)
		}
		if evs[0].Error == nil {
			t.Fatal("Error == nil — status failed com errors[] tem de produzir StatusError")
		}
		// consumer-a's real capture (2026-07-26, T-033): the real
		// failure from 2026-07-20 (OS LR-00014) that prompted this
		// whole task — before it was derived from the doc's generic
		// example (code 131049).
		if evs[0].Error.Code != 131026 {
			t.Errorf("Error.Code = %d, want 131026", evs[0].Error.Code)
		}
		if evs[0].Error.Message != "Message undeliverable" {
			t.Errorf("Error.Message = %q", evs[0].Error.Message)
		}
		// T-029: errors[0].error_data.details.
		if evs[0].Error.Details != "Message Undeliverable." {
			t.Errorf("Error.Details = %q", evs[0].Error.Details)
		}
		// Non-regression (T-041): this fixture has no "pricing" in the
		// payload — the message failed, Meta doesn't charge for it, and
		// Billing stays absent.
		if evs[0].Billing != nil {
			t.Errorf("Billing = %+v, want nil — this status has no pricing", evs[0].Billing)
		}
	},
	"status_read_com_cobranca.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		// consumer-a's real capture (2026-07-26, T-041), pasted into the
		// bilateral channel (consumer-a-STATUS.local.md, gitignored): 145
		// of the 148 status events they have recorded carry "pricing";
		// this is the exact format they pasted, wrapped in this
		// corpus's standard envelope.
		if evs[0].Billing == nil {
			t.Fatal("Billing == nil — status com pricing tem de produzir Billing")
		}
		if evs[0].Billing.Category != "utility" {
			t.Errorf("Category = %q, want utility", evs[0].Billing.Category)
		}
		if evs[0].Billing.Billable == nil || !*evs[0].Billing.Billable {
			t.Errorf("Billable = %v, want a *bool pointing to true", evs[0].Billing.Billable)
		}
	},
	"status_de_template.json": func(t *testing.T, evs []Event, err error) {
		// consumer-a's real (partial) capture (2026-07-26, T-043): the
		// `change` is literal, one of the 21 samples they'd kept on
		// disk since before the migration; the `entry` (id/time) is
		// this corpus's standard envelope, because what was delivered
		// didn't include that level. See testdata/corpus/README.md.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1 — the template webhook HAS to become an event", err, len(evs))
		}
		e := evs[0]
		if e.Type != EventTypeTemplateStatus {
			t.Errorf("Type = %q, want template_status", e.Type)
		}
		if e.Template == nil {
			t.Fatal("Template == nil — an event with no content is good for nothing")
		}
		if e.Template.Category != "UTILITY" {
			t.Errorf("Category = %q, want UTILITY — it's the field that makes this event worth having",
				e.Template.Category)
		}
		if e.Template.State != "APPROVED" {
			t.Errorf("State = %q, want APPROVED", e.Template.State)
		}
		// "NONE" is Meta's NORMAL value when there's no reason —
		// translating to empty would erase the difference between
		// "Meta said NONE" and "Meta didn't send the field".
		if e.Template.Reason != "NONE" {
			t.Errorf("Reason = %q, want the string NONE as it came", e.Template.Reason)
		}
		if e.Template.Name != "aguardando_peca_v2" || e.Template.Language != "pt_BR" {
			t.Errorf("Name=%q Language=%q", e.Template.Name, e.Template.Language)
		}
		// The time comes from the entry (the `value` has no timestamp
		// of its own) and enters the key — see templateStatusEvent.
		if e.Timestamp != 1769000020 {
			t.Errorf("Timestamp = %d, want 1769000020 (the entry.time)", e.Timestamp)
		}
		if e.ID != "template_status:1384121316897444:APPROVED:1769000020" {
			t.Errorf("ID = %q — the key with the time is mandatory", e.ID)
		}
		// A template webhook has no metadata.phone_number_id: the only
		// routing key is the waba.
		if e.PhoneNumberID != "" {
			t.Errorf("PhoneNumberID = %q, want empty — this webhook does not carry that field", e.PhoneNumberID)
		}
		if e.WabaID != "WABA_TESTE" {
			t.Errorf("WabaID = %q", e.WabaID)
		}
	},
	// --- T-057/T-174: `template_category_update`, the event DEDICATED to the change ---
	//
	// FOUR files: three REAL captures (T-174, 2026-08-28) plus the
	// synthetic sibling. The derived-from-the-doc one is GONE — it was
	// replaced, not kept alongside, which is the corpus's standing rule
	// (testdata/corpus/README.md) and the reason the same asymmetry cost
	// money on `status_delivered.json`.
	//
	// The synthetic one survives the replacement, and NOT out of habit:
	// it is the only file where the four category fields differ from
	// each other, so it is the only one that turns red if someone reads
	// `correct_category` in place of `previous_category`. The three
	// captures cannot do that job — real traffic brought NEITHER
	// `correct_category` NOR `category_appeal_status` in any of them.
	"categoria_de_template_rebaixamento.json": func(t *testing.T, evs []Event, err error) {
		// REAL CAPTURE, ceded by consumer `consumer-b` on 2026-08-28
		// through the channel, whole and unreformatted; the only edit at
		// origin was the `waba_id` becoming WABA_TESTE. `entry.time`
		// 1787252135 = 2026-08-20 18:55:35 UTC (15:55:35 -03).
		//
		// The EXPENSIVE direction, and the one that was never observed
		// before this file: UTILITY -> MARKETING raises the price of
		// every send in the family.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1 — the category webhook HAS to become an event", err, len(evs))
		}
		e := evs[0]
		if e.Type != EventTypeTemplateCategory {
			t.Errorf("Type = %q, want template_categoria", e.Type)
		}
		if e.TemplateCategory == nil {
			t.Fatal("TemplateCategory == nil — an event with no content is good for nothing")
		}
		c := e.TemplateCategory
		// The DIRECTION is what this event has that
		// `message_template_status_update` doesn't. If it's lost, what's
		// left is what T-043 already gave.
		if c.PreviousCategory != "UTILITY" || c.NewCategory != "MARKETING" {
			t.Errorf("previous=%q new=%q, want UTILITY -> MARKETING — the direction is what this event adds",
				c.PreviousCategory, c.NewCategory)
		}
		if c.Name != "instrucoes_download_app_v6" || c.Language != "pt_BR" {
			t.Errorf("Name=%q Language=%q", c.Name, c.Language)
		}
		// THE FINDING OF THE CAPTURE, and no one invents it reading the
		// doc: real traffic brought NEITHER field. The dashboard's
		// sample has both, which is exactly why testing only against it
		// proves agreement with the documentation and not with Meta.
		// They stay modelled (the synthetic sibling exercises them) and
		// they degrade to empty here — losing them must not cost the
		// event.
		if c.CorrectCategory != "" || c.AppealStatus != "" {
			t.Errorf("correct=%q appeal=%q, want BOTH empty — the real capture did not bring either one",
				c.CorrectCategory, c.AppealStatus)
		}
		// The same time-carrying key as template_status, for the
		// WORSENED reason templateCategoryEvent documents: a
		// template can go and COME BACK from a category, and without
		// the time the third transition collides with the first.
		if e.ID != "template_categoria:1563912508540305:UTILITY:MARKETING:1787252135" {
			t.Errorf("ID = %q — the key with the transition AND time is mandatory", e.ID)
		}
		if e.Timestamp != 1787252135 {
			t.Errorf("Timestamp = %d, want 1787252135 (o entry.time)", e.Timestamp)
		}
		// Account webhook: the only routing key is the waba (guard 5b).
		if e.PhoneNumberID != "" {
			t.Errorf("PhoneNumberID = %q, want empty — this webhook does not carry that field", e.PhoneNumberID)
		}
		if e.WabaID != "WABA_TESTE" {
			t.Errorf("WabaID = %q", e.WabaID)
		}
	},
	"categoria_de_template_restauracao.json": func(t *testing.T, evs []Event, err error) {
		// REAL CAPTURE (same origin and same day as the file above).
		// `entry.time` 1787305767 = 2026-08-21 09:49:27 UTC (06:49:27
		// -03), ~14,9 h after the demotion — the SAME
		// `message_template_id`, coming back.
		//
		// This file's value is not this test: it's the PAIR. See
		// TestTemplateCategoryTheRealPairThereAndBackHasDifferentKeys.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		e := evs[0]
		c := e.TemplateCategory
		if c == nil {
			t.Fatal("TemplateCategory == nil")
		}
		if c.PreviousCategory != "MARKETING" || c.NewCategory != "UTILITY" {
			t.Errorf("previous=%q new=%q, want MARKETING -> UTILITY (the way back)",
				c.PreviousCategory, c.NewCategory)
		}
		if c.Name != "instrucoes_download_app_v6" || c.Language != "pt_BR" {
			t.Errorf("Name=%q Language=%q", c.Name, c.Language)
		}
		if c.CorrectCategory != "" || c.AppealStatus != "" {
			t.Errorf("correct=%q appeal=%q, want BOTH empty — Meta did not send them even on the restoration",
				c.CorrectCategory, c.AppealStatus)
		}
		if e.ID != "template_categoria:1563912508540305:MARKETING:UTILITY:1787305767" {
			t.Errorf("ID = %q", e.ID)
		}
		if e.Timestamp != 1787305767 {
			t.Errorf("Timestamp = %d, want 1787305767", e.Timestamp)
		}
		if e.WabaID != "WABA_TESTE" {
			t.Errorf("WabaID = %q", e.WabaID)
		}
	},
	"categoria_de_template_sem_anterior.json": func(t *testing.T, evs []Event, err error) {
		// REAL CAPTURE, and the one that changes what we KNOW instead of
		// what we test: it arrives WITHOUT `previous_category`. One in
		// the 18 events the consumer had stored; the other seventeen
		// carry the field.
		//
		// Until this file, `PreviousCategory` degrading (and not
		// killing the event) was a project decision written in
		// parse.go's comment with no observation behind it. Now there
		// is one. The assertion below is what keeps it: the event is
		// PRODUCED, the direction is half-known, and the key still
		// distinguishes — see
		// TestTemplateCategoryWithoutPreviousCategoryStillBecomesAnEvent.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1 — the ABSENCE of the direction must not erase the event", err, len(evs))
		}
		e := evs[0]
		c := e.TemplateCategory
		if c == nil {
			t.Fatal("TemplateCategory == nil")
		}
		if c.PreviousCategory != "" {
			t.Errorf("PreviousCategory = %q, want empty — the payload does not carry previous_category", c.PreviousCategory)
		}
		if c.NewCategory != "MARKETING" {
			t.Errorf("NewCategory = %q, want MARKETING — it's the FACT, and it sustains the event on its own", c.NewCategory)
		}
		if c.Name != "teste_sonda_503_20ago" || c.Language != "pt_BR" {
			t.Errorf("Name=%q Language=%q", c.Name, c.Language)
		}
		// The empty field stays IN THE MIDDLE of the key. Dropping it
		// would collapse this key's shape onto a different one, and two
		// events would start colliding by construction.
		if e.ID != "template_categoria:3503097836538248::MARKETING:1787244576" {
			t.Errorf("ID = %q — the empty field sits in the MIDDLE of the key, between the colons", e.ID)
		}
		if e.Timestamp != 1787244576 {
			t.Errorf("Timestamp = %d, want 1787244576", e.Timestamp)
		}
		if e.WabaID != "WABA_TESTE" {
			t.Errorf("WabaID = %q", e.WabaID)
		}
	},
	"categoria_de_template_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// SYNTHETIC (T-057): the FOUR category fields come with values
		// DIFFERENT from each other — previous UTILITY, new MARKETING,
		// correct AUTHENTICATION —, which the dashboard's sample doesn't
		// have (there previous == correct). It's this file that turns
		// red if someone swaps the read of `previous_category` for
		// `correct_category`.
		//
		// The direction here is the EXPENSIVE one: UTILITY -> MARKETING
		// raises the price of every send, and it's exactly the case
		// T-043 exists to warn about and didn't cover.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		c := evs[0].TemplateCategory
		if c == nil {
			t.Fatal("TemplateCategory == nil")
		}
		if c.PreviousCategory != "UTILITY" {
			t.Errorf("PreviousCategory = %q, want UTILITY (the previous, not the correct)", c.PreviousCategory)
		}
		if c.NewCategory != "MARKETING" {
			t.Errorf("NewCategory = %q, want MARKETING", c.NewCategory)
		}
		if c.CorrectCategory != "AUTHENTICATION" {
			t.Errorf("CorrectCategory = %q, want AUTHENTICATION (the correct, not the previous)", c.CorrectCategory)
		}
		if c.PreviousCategory == c.CorrectCategory {
			t.Fatal("PreviousCategory == CorrectCategory — this fixture lost its ability to catch a swapped field read, which is the only reason it exists")
		}
		// "NOT_ELIGIBLE" exists in this fixture to prove the field is
		// TEXT: a derived boolean ("can it be appealed?") would have to
		// decide today what to do with a value that only shows up
		// tomorrow.
		if c.AppealStatus != "NOT_ELIGIBLE" {
			t.Errorf("AppealStatus = %q, want NOT_ELIGIBLE as it came", c.AppealStatus)
		}
		// A 16-digit message_template_id, like status_de_template.json's:
		// doesn't fit in an int32, which is why it's read as TEXT.
		if evs[0].ID != "template_categoria:9900000000000002:UTILITY:MARKETING:1769000072" {
			t.Errorf("ID = %q", evs[0].ID)
		}
	},
	// --- T-058: the other two account webhooks that have a consumer ---
	//
	// `phone_number_quality_update` (quota/quality) and `account_alerts`
	// (severity). The rest of the account fields the App receives still
	// have no model, and that's WRITTEN into the contract — see
	// TestParseWebhookAnotherAccountFieldStaysOnlyInTheRawBody.
	"qualidade_do_numero_derivado_da_doc.json": func(t *testing.T, evs []Event, err error) {
		// DERIVED FROM THE DOC: the dashboard's *Test* button sample
		// (2026-07-28), frozen byte for byte. `16505551111` is Meta's own
		// fictitious number, preserved from the sample — it's no one's
		// real number.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		e := evs[0]
		if e.Type != EventTypeNumberQuality {
			t.Errorf("Type = %q, want qualidade_do_numero", e.Type)
		}
		q := e.NumberQuality
		if q == nil {
			t.Fatal("NumberQuality == nil")
		}
		// LITERAL. "TIER_250" doesn't become 250, and "TIER_NOT_SET"
		// doesn't become 0 or empty: inventing meaning for an unverified
		// value is worse than passing it through (see NumberQuality,
		// types.go).
		if q.CurrentLimit != "TIER_250" || q.PreviousLimit != "TIER_NOT_SET" {
			t.Errorf("CurrentLimit=%q PreviousLimit=%q, want the literal TIER_250 and TIER_NOT_SET",
				q.CurrentLimit, q.PreviousLimit)
		}
		if q.State != "ONBOARDING" {
			t.Errorf("State = %q, want ONBOARDING", q.State)
		}
		if q.DisplayNumber != "16505551111" {
			t.Errorf("DisplayNumber = %q", q.DisplayNumber)
		}
		if q.MaxDailyLimit != "TIER_250" {
			t.Errorf("MaxDailyLimit = %q", q.MaxDailyLimit)
		}
		if e.ID != "qualidade_do_numero:16505551111:ONBOARDING:TIER_NOT_SET:TIER_250:1769000080" {
			t.Errorf("ID = %q — the key carries the limit TRANSITION and the time", e.ID)
		}
		// Account webhook: no phone_number_id. `display_phone_number` is
		// a label, and cannot become a routing key.
		if e.PhoneNumberID != "" {
			t.Errorf("PhoneNumberID = %q, want empty — display_phone_number is NOT phone_number_id", e.PhoneNumberID)
		}
		if e.WabaID != "WABA_TESTE" {
			t.Errorf("WabaID = %q", e.WabaID)
		}
	},
	"qualidade_do_numero_sintetico.json": func(t *testing.T, evs []Event, err error) {
		// SYNTHETIC (T-058): in the dashboard's sample `current_limit`
		// and `max_daily_conversations_per_business` come with the SAME
		// value ("TIER_250"), so there swapping the read of one for the
		// other passes GREEN. Here the THREE limits differ from each
		// other.
		//
		// And the direction is the EXPENSIVE one: TIER_1K -> TIER_50 is
		// a DOWNGRADE, which is the case this event exists to warn
		// about. The sample freezes an ONBOARDING, which is the one
		// transition that worries no one.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		q := evs[0].NumberQuality
		if q == nil {
			t.Fatal("NumberQuality == nil")
		}
		if q.CurrentLimit != "TIER_50" {
			t.Errorf("CurrentLimit = %q, want TIER_50 (the current, not the max_daily)", q.CurrentLimit)
		}
		if q.PreviousLimit != "TIER_1K" {
			t.Errorf("PreviousLimit = %q, want TIER_1K", q.PreviousLimit)
		}
		if q.MaxDailyLimit != "TIER_10K" {
			t.Errorf("MaxDailyLimit = %q, want TIER_10K (the max_daily, not the current)", q.MaxDailyLimit)
		}
		if q.CurrentLimit == q.MaxDailyLimit {
			t.Fatal("CurrentLimit == MaxDailyLimit — this fixture lost its ability to catch a swapped field read, which is the only reason it exists")
		}
		if q.State != "FLAGGED" {
			t.Errorf("State = %q, want FLAGGED", q.State)
		}
	},
	"alerta_de_conta_derivado_da_doc.json": func(t *testing.T, evs []Event, err error) {
		// DERIVED FROM THE DOC: the dashboard's *Test* button sample
		// (2026-07-28). Has NO synthetic sibling, and the absence is a
		// decision: the four fields that enter the key already come with
		// DIFFERENT values from each other in the sample (`WABA`,
		// `123456`, `INFORMATIONAL`, `NONE`, `OBA_APPROVED`), so it
		// alone catches a swapped field read. Adding a synthetic one
		// "for symmetry" with the quality one would be ceremony with no
		// guarantee — the same decision (and the same question) as
		// botao_interativo.json.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		e := evs[0]
		if e.Type != EventTypeAccountAlert {
			t.Errorf("Type = %q, want alerta_de_conta", e.Type)
		}
		a := e.AccountAlert
		if a == nil {
			t.Fatal("AccountAlert == nil")
		}
		// The field that justifies the type existing: without severity,
		// a serious alert and a routine notice arrive identical.
		if a.Severity != "INFORMATIONAL" {
			t.Errorf("Severity = %q, want INFORMATIONAL as it came", a.Severity)
		}
		if a.Type != "OBA_APPROVED" {
			t.Errorf("Type = %q, want OBA_APPROVED (the alert_type, not the alert_status)", a.Type)
		}
		if a.State != "NONE" {
			t.Errorf("State = %q, want the string NONE as it came", a.State)
		}
		if a.EntityType != "WABA" {
			t.Errorf("EntityType = %q", a.EntityType)
		}
		// entity_id comes as a NUMBER in the payload and comes out as
		// TEXT — never an int, for the same reason as the 16-digit
		// message_template_id.
		if a.EntityID != "123456" {
			t.Errorf("EntityID = %q, want \"123456\" (text, and Meta sent a number)", a.EntityID)
		}
		if a.Description != "Sample alert description, informational in nature with no status" {
			t.Errorf("Description = %q", a.Description)
		}
		if e.ID != "alerta_de_conta:123456:OBA_APPROVED:INFORMATIONAL:NONE:1769000084" {
			t.Errorf("ID = %q — the key carries severity and state, otherwise an ESCALATION gets deduplicated against the original alert", e.ID)
		}
		if e.PhoneNumberID != "" {
			t.Errorf("PhoneNumberID = %q, want empty", e.PhoneNumberID)
		}
		if e.WabaID != "WABA_TESTE" {
			t.Errorf("WabaID = %q", e.WabaID)
		}
	},
	"corpo_null.json": func(t *testing.T, evs []Event, err error) {
		if err == nil {
			t.Fatal("null body passed without an error")
		}
		if len(evs) != 0 {
			t.Fatalf("len(evs) = %d, want 0", len(evs))
		}
	},
	"reacao.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d, want nil and 1", err, len(evs))
		}
		if evs[0].Reaction == nil {
			t.Fatal("Reaction == nil")
		}
		// "❤️" is TWO codepoints (U+2764 HEAVY BLACK HEART + U+FE0F
		// VARIATION SELECTOR-16) — consumer-a's real capture
		// (2026-07-26). A SINGLE-codepoint emoji (the "👍" this file
		// had before, doc-derived) doesn't exercise the variation
		// selector path; see docs/ARMADILHAS.md.
		if evs[0].Reaction.Emoji != "❤️" {
			t.Errorf("Emoji = %q, want ❤️ (with variation selector)", evs[0].Reaction.Emoji)
		}
		if len([]rune(evs[0].Reaction.Emoji)) != 2 {
			t.Errorf("Emoji has %d rune(s), want 2 — the variation selector was lost",
				len([]rune(evs[0].Reaction.Emoji)))
		}
		if evs[0].Reaction.Target != "wamid.TESTE001" {
			t.Errorf("Target = %q", evs[0].Reaction.Target)
		}
	},
	"reacao_removida.json": func(t *testing.T, evs []Event, err error) {
		// An emoji ABSENT from Meta's payload is REMOVAL, not
		// malformation — it cannot become ErrPartialParse.
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d — a reaction without an emoji is removal, not an error", err, len(evs))
		}
		if evs[0].Reaction == nil {
			t.Fatal("Reaction == nil — remocao tambem e um evento")
		}
		if evs[0].Reaction.Emoji != "" {
			t.Errorf("Emoji = %q, want empty (removal)", evs[0].Reaction.Emoji)
		}
		if evs[0].Reaction.Target != "wamid.TESTE001" {
			t.Errorf("Target = %q", evs[0].Reaction.Target)
		}
	},
	"localizacao.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].Location == nil {
			t.Fatal("Location == nil")
		}
		// Coordinates rounded on purpose by consumer-a before pasting
		// into the channel (real capture, 2026-07-26) — not the doc's.
		if evs[0].Location.Latitude != -21.229 {
			t.Errorf("Latitude = %v", evs[0].Location.Latitude)
		}
		if evs[0].Location.Longitude != -43.7892 {
			t.Errorf("Longitude = %v", evs[0].Location.Longitude)
		}
		// The bare pin (WITHOUT name/address) is the common case in the
		// real capture — the doc-derived fixture carried both and
		// tested the rare case. See docs/ARMADILHAS.md.
		if evs[0].Location.Name != "" {
			t.Errorf("Name = %q, want empty — Meta did not send a name in this capture", evs[0].Location.Name)
		}
		if evs[0].Location.Address != "" {
			t.Errorf("Address = %q, want empty — Meta did not send an address in this capture", evs[0].Location.Address)
		}
	},
	"documento_com_legenda.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		// consumer-a's real capture (2026-07-26): caption and filename
		// both come, side by side, and the filename is the customer's
		// REAL file name (long, with hyphens and numbers) — not a short
		// generic one like "nota.pdf". The doc-derived fixture that
		// existed before didn't exercise a long name. See
		// docs/ARMADILHAS.md.
		if evs[0].Caption != "PDF teste" {
			t.Errorf("Caption = %q, want \"PDF teste\"", evs[0].Caption)
		}
		if evs[0].Filename != "515642-9741-manual-forno-gourmet-grill-rev-43.pdf" {
			t.Errorf("Filename = %q", evs[0].Filename)
		}
	},
	"imagem.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].MediaMimePayload != "image/jpeg" {
			t.Errorf("MediaMimePayload = %q, want image/jpeg", evs[0].MediaMimePayload)
		}
		if evs[0].MediaID != "MEDIA_TESTE3" {
			t.Errorf("MediaID = %q", evs[0].MediaID)
		}
	},
	"video.json": func(t *testing.T, evs []Event, err error) {
		if err != nil || len(evs) != 1 {
			t.Fatalf("err=%v len=%d", err, len(evs))
		}
		if evs[0].MediaMimePayload != "video/mp4" {
			t.Errorf("MediaMimePayload = %q, want video/mp4", evs[0].MediaMimePayload)
		}
		if evs[0].MediaID != "MEDIA_TESTE4" {
			t.Errorf("MediaID = %q", evs[0].MediaID)
		}
	},
}

func TestTheWholeCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(corpusDir, "*.json"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	// TRAP — cost: on this network, an architectural guard swept paths
	// relative to the cwd and, run from another directory, found ZERO
	// files and passed GREEN without scanning anything. Every guard needs
	// to prove it checked something.
	if len(files) == 0 {
		t.Fatal("no file in the corpus — the guard checked NOTHING")
	}

	for _, path := range files {
		name := filepath.Base(path)
		check, hasTest := corpusExpectations[name]
		if !hasTest {
			t.Errorf("%s is in the corpus and no test consumes it", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			payload, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			evs, errParse := ParseWebhook(payload)
			check(t, evs, errParse)
		})
	}

	if len(files) != len(corpusExpectations) {
		t.Errorf("files=%d expected=%d — the table and the directory diverged",
			len(files), len(corpusExpectations))
	}
}

// corpusEvents reads a corpus file and returns what ParseWebhook
// produced. It exists because the two tests below compare files AGAINST
// EACH OTHER, and TestTheWholeCorpus's table can only look at one file at a
// time.
func corpusEvents(t *testing.T, name string) []Event {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(corpusDir, name))
	if err != nil {
		t.Fatalf("ReadFile %s: %v", name, err)
	}
	evs, err := ParseWebhook(payload)
	if err != nil {
		t.Fatalf("ParseWebhook %s: %v", name, err)
	}
	if len(evs) != 1 {
		t.Fatalf("%s: len(evs) = %d, want 1", name, len(evs))
	}
	return evs
}

// T-069's capture's most valuable finding, and the only one no one
// invents on their own: the `sent` and the `delivered` of the SAME wamid
// arrived with the SAME timestamp from Meta (1785072102 in both, measured
// by consumer-a on 2026-07-28).
//
// The consequence belongs to the CONSUMER, not to us: whoever builds a
// history ordering by the envelope's `timestamp` does NOT separate the two
// states — the sender's clock says the same instant for both. Only the
// ARRIVAL order separates them. That's why the warning is in
// docs/CONTRATO-CONSUMIDOR.md, and not just here.
//
// What separates the two events is the KEY (`status:{wamid}:{status}`),
// and this test asserts both things at once: equal timestamps, different
// ids. If someone "simplifies" the key to `status:{wamid}` — as was
// already attempted on the template event, see the contract — `delivered`
// starts colliding with `sent` in the consumer's dedup and one of the two
// vanishes.
func TestCorpusSentAndDeliveredOfTheSameWamidHaveTheSameTimestamp(t *testing.T) {
	sent := corpusEvents(t, "status_sent_com_pricing.json")[0]
	delivered := corpusEvents(t, "status_delivered.json")[0]

	if sent.WaMessageID != delivered.WaMessageID {
		t.Fatalf("wamid sent=%q delivered=%q — both fixtures have to be the SAME send, otherwise this test proves nothing",
			sent.WaMessageID, delivered.WaMessageID)
	}
	if sent.Timestamp != delivered.Timestamp {
		t.Errorf("Timestamp sent=%d delivered=%d — the real capture brought BOTH with %d; a fixture that makes them differ hides the consumer's problem",
			sent.Timestamp, delivered.Timestamp, 1785072102)
	}
	if sent.ID == delivered.ID {
		t.Errorf("ID sent == ID delivered == %q — the key HAS to include the status, otherwise a dedup by id throws away one of the two states",
			sent.ID)
	}
	if sent.Status != "sent" || delivered.Status != "delivered" {
		t.Errorf("Status sent=%q delivered=%q", sent.Status, delivered.Status)
	}
}

// `recipient_user_id` and `contacts[].user_id` arrive over the real wire
// (152 of 225 raw payloads, consumer-a's measurement). The parser does NOT
// model them, and that's a decision: the envelope only grows, and a field
// no one asked for becomes contract forever.
//
// The double guarantee this test locks in: they don't bring down the
// parse (corpusEvents's `err`) AND they don't leak into the JSON the
// consumer receives. The second isn't theory — the same pair of fields
// already had this proof on the MESSAGE side
// (TestParseWebhookAnUnknownFieldDoesNotBringDownTheParse); the STATUS side
// went without it until a capture existed, which is the usual asymmetry.
func TestCorpusARealStatusDoesNotLeakRecipientUserIDIntoTheEnvelope(t *testing.T) {
	for _, name := range []string{
		"status_sent_sem_pricing.json",
		"status_sent_com_pricing.json",
		"status_delivered.json",
	} {
		ev := corpusEvents(t, name)[0]
		output, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", name, err)
		}
		for _, forbidden := range []string{"user_id", "BR.20000000000000000"} {
			if strings.Contains(string(output), forbidden) {
				t.Errorf("%s: the envelope carries %q — the consumer reads OUR vocabulary, not Meta's:\n%s",
					name, forbidden, output)
			}
		}
	}
}

// T-174, and it is the proof the dashboard's sample could never give:
// the SAME `message_template_id` goes to MARKETING and COMES BACK,
// fourteen hours later, in real traffic.
//
// templateCategoryEvent's comment (parse.go) decided this key's
// shape on that scenario — the transition entering ALONGSIDE the time —
// and until 2026-08-28 the scenario had never been observed. Now the two
// halves are frozen side by side, and this test is what keeps a
// "simplification" of the key from arriving unnoticed: with
// template_categoria:{id} the two events collide and the consumer's dedup
// erases the return, which is precisely the one that says the money went
// back to normal.
//
// The two captures were ceded by consumer `consumer-b` on 2026-08-28.
func TestTemplateCategoryTheRealPairThereAndBackHasDifferentKeys(t *testing.T) {
	downgrade := corpusEvents(t, "categoria_de_template_rebaixamento.json")[0]
	restore := corpusEvents(t, "categoria_de_template_restauracao.json")[0]

	// Without this the test proves nothing: two DIFFERENT templates
	// would trivially have different keys.
	if downgrade.TemplateCategory == nil || restore.TemplateCategory == nil {
		t.Fatal("TemplateCategory == nil em um dos dois")
	}
	if downgrade.TemplateCategory.Name != restore.TemplateCategory.Name {
		t.Fatalf("different names (%q vs %q) — both fixtures HAVE to be the same template, otherwise this test proves nothing",
			downgrade.TemplateCategory.Name, restore.TemplateCategory.Name)
	}
	// Deliberately WITHOUT the surrounding colons: this guard is about
	// the two fixtures being the same template, not about the key's
	// shape. With ":id:" a change to the key's shape would trip this
	// Fatalf and hide the collision assertion below — which is the one
	// that has to speak.
	if !strings.Contains(downgrade.ID, "1563912508540305") || !strings.Contains(restore.ID, "1563912508540305") {
		t.Fatalf("message_template_id different between the two: there=%q back=%q — the pair lost its identity",
			downgrade.ID, restore.ID)
	}

	if downgrade.ID == restore.ID {
		t.Errorf("ID there == ID back == %q — the key HAS to separate the two transitions, otherwise the consumer's dedup erases the way back",
			downgrade.ID)
	}
	if downgrade.Timestamp == restore.Timestamp {
		t.Errorf("Timestamp equal on both (%d) — the real capture brought 1787252135 and 1787305767",
			downgrade.Timestamp)
	}
	// The direction is inverted between the two, and that's the whole
	// point of the event: one raises the price, the other lowers it.
	if downgrade.TemplateCategory.NewCategory != "MARKETING" ||
		restore.TemplateCategory.NewCategory != "UTILITY" {
		t.Errorf("new there=%q back=%q, want MARKETING and UTILITY — both directions",
			downgrade.TemplateCategory.NewCategory, restore.TemplateCategory.NewCategory)
	}
}

// T-174: `previous_category` CAN BE ABSENT, and this is the observation
// that was missing.
//
// templateCategoryEvent (parse.go) requires only
// `message_template_id` + `new_category`, and lets the direction degrade.
// That was written as a project decision on 2026-07-28 with NO capture
// showing the field missing — the kind of sentence this project treats as
// unproven until traffic says otherwise. The consumer `consumer-b`
// found one in 18 stored events (2026-08-28) and handed over the raw
// body.
//
// What this test pins down is that the event SURVIVES the absence. The
// opposite outcome is the one that costs: a reclassification to MARKETING
// that never reaches the consumer is a family sending at the higher price
// with nobody's clock started.
func TestTemplateCategoryWithoutPreviousCategoryStillBecomesAnEvent(t *testing.T) {
	ev := corpusEvents(t, "categoria_de_template_sem_anterior.json")[0]

	if ev.Type != EventTypeTemplateCategory {
		t.Fatalf("Type = %q, want template_categoria", ev.Type)
	}
	c := ev.TemplateCategory
	if c == nil {
		t.Fatal("TemplateCategory == nil — the event came out with no content")
	}
	if c.PreviousCategory != "" {
		t.Errorf("PreviousCategory = %q, want empty — this payload does NOT carry previous_category, and the fixture lost its reason to exist if it does",
			c.PreviousCategory)
	}
	if c.NewCategory != "MARKETING" {
		t.Errorf("NewCategory = %q, want MARKETING", c.NewCategory)
	}
	// The key keeps the empty slot between the two colons. It still
	// distinguishes: the id and the time are there, and a second event
	// of the same template in the same second would have to be the same
	// transition to collide — which is when dedup is the right answer.
	if ev.ID != "template_categoria:3503097836538248::MARKETING:1787244576" {
		t.Errorf("ID = %q — the key has to keep the empty field in the middle", ev.ID)
	}
	// And it does not collide with the pair above, which is the same
	// guarantee looked at from the other side.
	for _, neighbor := range []string{
		"categoria_de_template_rebaixamento.json",
		"categoria_de_template_restauracao.json",
		"categoria_de_template_sintetico.json",
	} {
		if another := corpusEvents(t, neighbor)[0]; another.ID == ev.ID {
			t.Errorf("the key without a direction collided with %s (%q)", neighbor, ev.ID)
		}
	}
}

// T-174, and this one is a claim NO single fixture can make: in the
// THREE real captures Meta sent NEITHER `correct_category` NOR
// `category_appeal_status`.
//
// The dashboard's sample has both, and the contract's table describes
// both as what makes this event worth consuming ("without it nobody
// knows an appeal window exists"). That description came from the
// documentation. Real traffic, so far, does not corroborate it — which
// is the whole reason a derived fixture is not a capture.
//
// THIS TEST IS NOT A GUARD AGAINST THE FIELDS: they stay modelled, and
// `categoria_de_template_sintetico.json` keeps exercising them. It is a
// dated MEASUREMENT — three captures, one account, 2026-08-28 — that
// turns red the day a capture with either field is frozen here. When
// that day comes, the right move is to update this test and the
// contract's table together, not to delete the assertion.
func TestTemplateCategoryNoRealCaptureBroughtAppealNorCorrectCategory(t *testing.T) {
	captures := []string{
		"categoria_de_template_rebaixamento.json",
		"categoria_de_template_restauracao.json",
		"categoria_de_template_sem_anterior.json",
	}
	seenIDs := make(map[string]string, len(captures))
	for _, name := range captures {
		ev := corpusEvents(t, name)[0]
		c := ev.TemplateCategory
		if c == nil {
			t.Fatalf("%s: TemplateCategory == nil", name)
		}
		if c.CorrectCategory != "" || c.AppealStatus != "" {
			t.Errorf("%s: correct=%q appeal=%q — if Meta started sending them, UPDATE the measurement and the contract table together, do not delete the assertion",
				name, c.CorrectCategory, c.AppealStatus)
		}
		// Three captures, three keys: the corpus would be lying if two
		// of them deduplicated into one.
		if before, duplicate := seenIDs[ev.ID]; duplicate {
			t.Errorf("%s and %s produced the SAME key %q", before, name, ev.ID)
		}
		seenIDs[ev.ID] = name
	}
	if len(seenIDs) != len(captures) {
		t.Errorf("distinct keys = %d, want %d", len(seenIDs), len(captures))
	}
}
