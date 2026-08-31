// The public webhook: POST /v1/inbound/{slug} and the verification GET.
//
// THE ORDER BELOW IS MANDATORY AND FAIL-CLOSED:
//
//	path -> app_secret -> verify signature -> ONLY THEN trust the body
//
// There is no shortcut. There's no way to know whose body it is before being
// able to trust it, and to trust it you need to know which secret to use.
// That's why the instance comes from the PATH, never from the body.
//
// The Host header plays NO part at all: that's what lets the same gateway
// serve each client's own domain with no new code, and what makes a forged
// Host change no destination at all.
package inbound

import (
	"crypto/hmac"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

// Threshold and window for the over-the-cap-body alarm (ReadRaw's 413).
//
// WHY THIS ALARMS: a 413 does NOT fix itself. Meta redelivers the SAME body,
// it blows past the SAME cap, and within 36h it gives up — a permanent loss,
// which by mirror.go's criterion (ALARM = needs a person) is the most
// expensive case. The fix is human: raise ZAPGW_MAX_CORPO_BYTES and restart,
// or trim the payload at the source. No redelivery does this on its own.
//
// WHY IT DOESN'T ALARM ON THE FIRST ONE: while Meta's redelivery window is
// open, the isolated event still has a chance, and alarming on what can fix
// itself trains whoever operates the system to ignore the alarm
// (docs/ARMADILHAS.md). Three within the same hour, on the same instance, is
// no longer an isolated event: it's either the wrong cap or a source sending
// past it.
//
// THE NUMBERS ARE OURS, NOT META'S. Meta doesn't publish the interval or the
// count of its redeliveries (docs/ARMADILHAS.md), so a window calibrated
// "by its redelivery behavior" would be a made-up number. 1h is the
// compromise: short enough for the count to mean "this is happening right
// now" — yesterday's rejections don't add up with today's — and long enough
// not to reset between two events spaced apart.
const (
	largeBodyThreshold = 3
	largeBodyWindow    = time.Hour
)

type Handler struct {
	store      *config.Store
	deliverer  *Deliverer
	maxBytes   int
	rejections *rejectionCounter
	// counter is the SERIALIZED writer for instance counters (T-035).
	// Register returns no error at all — see internal/config/contador.go —
	// so nothing here can propagate a counting failure into the response to
	// Meta.
	counter *config.Counter
	// number is the writer for the NUMBER observation (T-080): the
	// `phone_number_quality_update` webhook carries `current_limit`, which is
	// the second source for the tier — the other one is the watchdog's
	// measurement. Register returns no error at all
	// (config.NumberObserver), for the SAME reason as the counter:
	// nothing here can propagate a tracking failure into the response to
	// Meta.
	number *config.NumberObserver
	// transit is the writer for the TRANSIT log (T-091): "did this message
	// pass through here?", with no content and no plaintext phone number
	// stored. NIL is safe (recordTransit checks) — there is a test that
	// builds a Handler without it. Register returns no error at all
	// (config.Transit), for the SAME reason as the counter.
	transit *config.Transit
	// warnedCategories holds which UNKNOWN billing categories have already
	// produced an ALARM on this instance, so the warning fires once per
	// process instead of once per message (T-063, see
	// internal/inbound/cobranca.go).
	warnedCategories *unknownCategoryWarning
	// now is injectable only for tests, like the Watchdog's own `now` —
	// without it, proving the tie-break rule between webhook and measurement
	// would require sleeping on the clock.
	now func() time.Time
	// seq is atomic because http.Server serves every request in its own
	// goroutine over the SAME handler. A raw `seq++` here is a data race —
	// proven with -race — and no sequential test reveals it.
	seq atomic.Int64
}

func NewHandler(store *config.Store, deliverer *Deliverer, maxBytes int, counter *config.Counter, transit *config.Transit) http.Handler {
	_, mux := newHandler(store, deliverer, maxBytes, counter, transit, time.Now)
	return mux
}

// newHandler ALSO returns the *Handler, and it exists so a test can stop the
// clock (the same role as the Watchdog's `now` field). Production never swaps
// it — NewHandler above passes time.Now and there is no path outside this
// package that passes anything else.
func newHandler(
	store *config.Store, deliverer *Deliverer, maxBytes int,
	counter *config.Counter, transit *config.Transit, now func() time.Time,
) (*Handler, http.Handler) {
	h := &Handler{
		store: store, deliverer: deliverer, maxBytes: maxBytes, counter: counter, transit: transit,
		number:           config.NewNumberObserver(store),
		now:              now,
		rejections:       newRejectionCounter(largeBodyThreshold, largeBodyWindow),
		warnedCategories: newUnknownCategoryWarning(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/inbound/{slug}", h.receive)
	mux.HandleFunc("GET /v1/inbound/{slug}", h.verify)
	return h, mux
}

// verify answers the webhook's signature challenge (GET), which uses the
// verify_token — NOT the app_secret.
//
// TRAP: the verify_token is only used HERE, never on the POST. Changing the
// value without re-registering it with Meta breaks NO traffic at all:
// everything keeps working normally for weeks, until someone re-saves the
// callback URL in Meta's dashboard and the rejection shows up disconnected
// from the old change.
func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			http.Error(w, "instancia desconhecida", http.StatusNotFound)
			return
		}
		// Same distinction as receive: the database being down is not a
		// mistyped slug. A 404 here would send the operator hunting for a
		// typo in the slug when the problem is infrastructure.
		log.Printf("zapgw: erro de store no slug %q: %v", slug, err)
		http.Error(w, "indisponivel", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	// hmac.Equal instead of !=, for the same reason as the app_secret:
	// constant-time comparison. Low practical consequence here — the
	// verify_token isn't a high-value secret and the endpoint isn't on the
	// hot path — but the fix is one line and keeps the project consistent.
	if q.Get("hub.mode") != "subscribe" || !hmac.Equal([]byte(q.Get("hub.verify_token")), []byte(inst.VerifyToken)) {
		http.Error(w, "verificacao recusada", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(q.Get("hub.challenge")))
}

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	// 1. PATH -> instance. Before reading a single byte of the body.
	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			// Without this line, a stale slug in Meta's dashboard is a mute
			// symptom: "messages stopped arriving," with no error anywhere,
			// for up to 36h.
			log.Printf("zapgw: slug desconhecido %q recebeu trafego", slug)
			http.Error(w, "instancia desconhecida", http.StatusNotFound)
			return
		}
		// Database down: transient. Meta redelivers.
		log.Printf("zapgw: erro de store no slug %q: %v", slug, err)
		http.Error(w, "indisponivel", http.StatusServiceUnavailable)
		return
	}

	// A PAUSED instance does not process traffic. It is born paused on
	// purpose (config.CreateInstance writes ativo=0 fixed), and without this
	// guard the guarantee would be inert: a freshly created instance would
	// receive and deliver production messages before any smoke test.
	//
	// 503, not 200: paused means "not ready yet," not "discard." The 503
	// keeps Meta's redelivery window open, so activating the instance
	// within it recovers the messages. Deliberately no ALARME prefix: the
	// activation is already an expected step in provisioning (plan 4), not
	// a reaction to this log — no one needs to act JUST BECAUSE this event
	// arrived.
	if !inst.Active {
		log.Printf("zapgw: instancia %q esta pausada e recebeu trafego", slug)
		http.Error(w, "instancia pausada", http.StatusServiceUnavailable)
		return
	}

	// 2. Raw body, with a cap.
	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			// The ISOLATED event does not alarm: we respond 413 and Meta
			// redelivers within the 36h window. REPETITION alarms, because
			// the redelivery brings the same body and gets the same 413 —
			// by the end of the window the message is lost for good and
			// only a person can fix it.
			//
			// The trace below still fires on EVERY rejection: this block
			// ADDS the alarm case, it does not replace the log that already
			// existed (docs/ARMADILHAS.md: a fix that swaps the condition
			// instead of adding a case already erased the alarm that
			// mattered in this file).
			n, alarm := h.rejections.record(slug)
			if alarm {
				log.Printf("ALARME zapgw: instancia %q recusou %d corpos acima do teto de %d bytes em ate %s;"+
					" o reenvio da Meta traz o MESMO corpo e leva 413 de novo, entao a mensagem se perde em definitivo quando ela desistir."+
					" ACAO: subir ZAPGW_MAX_CORPO_BYTES e reiniciar o servico, ou cortar o payload na origem",
					slug, n, h.maxBytes, largeBodyWindow)
			} else {
				log.Printf("zapgw: corpo acima do teto na instancia %q (%d na janela de %s)", slug, n, largeBodyWindow)
			}
			http.Error(w, "corpo grande demais", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "erro de leitura", http.StatusServiceUnavailable)
		return
	}

	// 3. Signature, over the RAW BYTES.
	if !meta.SignatureValid(raw, r.Header.Get("X-Hub-Signature-256"), inst.AppSecret) {
		// We don't log the body: it carries personal data.
		log.Printf("zapgw: assinatura invalida na instancia %q", slug)
		http.Error(w, "assinatura invalida", http.StatusForbidden)
		return
	}

	// 4. ONLY NOW is the body trustworthy. The parse is ENRICHMENT: if it
	//    fails, the raw body goes out anyway.
	//
	// THE ONLY NEW BRANCH FROM T-097: the instance's TYPE decides which
	// PARSER reads the same raw body — not a second entry path (path ->
	// app_secret -> signature already happened identically for both types,
	// above). A `whatsapp` instance (the default, including Type == "")
	// goes through the SAME meta.ParseWebhook as always, byte for byte —
	// that is this task's no-regression guarantee.
	var parseErr string
	var evs []meta.Event
	if inst.Type == config.TypeInstagram {
		evs, err = meta.ParseInstagramWebhook(raw)
	} else {
		evs, err = meta.ParseWebhook(raw)
	}
	if err != nil {
		parseErr = err.Error()
		// NO ALARME prefix: the handler continues and delivers the raw body
		// anyway (INVARIANT 2 of deliver.go). The consumer receives the
		// event the same way; there is nothing a person needs to do RIGHT
		// NOW because of this particular parse.
		//
		// T-110: T-106 split two sentinels in internal/meta
		// (ErrPartialParse = genuinely unreadable, ErrUnmodeledItems =
		// read fine, legitimate, this slice just doesn't model it), but up
		// to this point the journal called both of them "parse failed" —
		// the failure monitor fired on normal Instagram traffic (measured
		// in production on v0.40.0).
		//
		// THE ORDER OF THE IF MATTERS AND IS MANDATORY: ErrPartialParse has
		// to be checked FIRST. T-106 composes the two sentinels with
		// errors.Join when the SAME batch has one unreadable item and one
		// unmodeled item — checking ErrUnmodeledItems first (with an
		// else) would make that mixed batch fall into the informational
		// branch, and the unreadable one would vanish from the journal: the
		// task would have made worse the very thing it came to fix.
		switch {
		case errors.Is(err, meta.ErrPartialParse):
			// Part of the payload could not be read — deserves human
			// attention, even when the same error also carries legitimate,
			// unmodeled items (errors.Is sees both sides of the Join).
			log.Printf("zapgw: parse falhou na instancia %q: %v", slug, err)
		case errors.Is(err, meta.ErrUnmodeledItems):
			// Only LEGITIMATE items that this slice doesn't model: this is
			// not a failure at all, and calling it "failed"/"error" is
			// exactly what fired the monitor with nothing wrong having
			// happened.
			log.Printf("zapgw: instancia %q: %v", slug, err)
		default:
			// Defensive: no parse error today falls here (ParseWebhook only
			// produces ErrPartialParse; ParseInstagramWebhook only produces
			// the two sentinels above, isolated or composed), but a future
			// error that is neither of the two falls back to the usual
			// behavior instead of staying silent.
			log.Printf("zapgw: parse falhou na instancia %q: %v", slug, err)
		}
	}

	// 5. Tenant isolation guard — TWO checks, one per webhook FORMAT,
	// because message/status and ACCOUNT webhooks do not carry the SAME
	// routing key (T-038, 2026-07-26).
	//
	// 5a. message/status: the body's phone_number_id has to be the path
	//     instance's own. The guard rejects the WHOLE BATCH when it finds
	//     an event whose phone_number_id is NON-EMPTY and different from
	//     the path instance's. This block is the ORIGINAL one, byte for
	//     byte — it did not change in this task, and that is the
	//     no-regression guarantee: it's the path that carries real
	//     production traffic right now.
	//
	// 5b. an ACCOUNT webhook (template status, number quality, alerts) has
	//     NO phone_number_id at all — Meta doesn't support URL override for
	//     them and they always arrive at the main endpoint (confirmed at
	//     developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/override/,
	//     read on 2026-07-26). The ONLY routing key left is the waba_id
	//     from entry[].id — confirmed as "WhatsApp Business Account ID" in
	//     the parameter table at
	//     developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
	//     and in the example at
	//     developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/message_template_status_update/
	//     (both read on 2026-07-26; see meta.AccountWabaIDsInPayload). We
	//     compare it against the PATH instance's waba_id, for the SAME
	//     reason as 5a — and the comment at the top of this file: the
	//     instance comes from the path, never from the body. A waba_id that
	//     doesn't match does NOT reroute the delivery to another instance —
	//     it only rejects this one.
	//
	// WITHOUT 5b (found by the consumer on 2026-07-26): an account webhook
	// arrives at the main endpoint, 5a sweeps ZERO events (an account
	// change produces no Event — the gateway doesn't model its content),
	// nothing gets rejected, and the RAW BODY is delivered to the consumer
	// of the slug that's in the path even though it belongs to another
	// WABA. It isn't just misrouting: the consumer checks
	// envelope["instancia"] against THEIR OWN slug, the guard passes
	// (WE were the ones who put the path's slug in the envelope), and they
	// WRITE TO THEIR DATABASE, PERMANENTLY, the raw body of an event that
	// may belong to another tenant. Data written into someone else's
	// database doesn't undo itself — unlike misrouting, which fixes itself
	// by repointing.
	//
	// WHAT THE TWO GUARDS TOGETHER DON'T COVER, and this remains a known
	// gap, not a guarantee: a parse that failed entirely (the body isn't a
	// JSON object — 5a and 5b find no entry at all) and an events/changes
	// list that's empty for any other reason. In both cases there is
	// nothing to compare; neither one was the trigger for this task.
	//
	// WHY REJECT THE WHOLE BATCH instead of filtering by event/change: the
	// RAW body travels along in the delivery. Filtering and still sending
	// the raw body would give one instance's consumer another instance's
	// content.
	//
	// OPERATIONAL CONSTRAINT, and it is SIMPLER than this comment used to
	// claim up to 2026-07-26. Each consumer uses their OWN App on Meta
	// (owner's decision, 2026-07-26) and the Callback URL is PER APP — so
	// each instance already receives on ITS OWN path (/v1/inbound/{slug})
	// by construction, with no one configuring any override at all. INPUT
	// isolation is the URL path plus that instance's app_secret, which
	// belongs to a different App: a signature valid for one is not valid
	// for the other (exercised with live traffic in T-042, 2026-07-26 —
	// 200 on one's path, 403 with the SAME bytes and the SAME signature on
	// the other's path).
	//
	// WEBHOOK OVERRIDE (per WABA, per number) is the path for whoever one
	// day puts two numbers under the SAME App — and only for that case. The
	// sentence that used to be here cited the override as a generic
	// operational constraint, which sent the reader chasing after a Meta
	// feature they don't need to separate two consumers.
	//
	// ACCOUNT webhooks still have no override at all: they always arrive at
	// the App's main endpoint, and that's exactly why 5b can only compare
	// waba_id, never phone_number_id.
	//
	// 200, not 5xx, in both cases: redelivering would repeat the same
	// mismatch for 36h. The fix is configuration, and a person does it —
	// hence ALARME in both.
	//
	// BOTH COUNT, and each under ITS OWN key (T-047, 2026-07-26):
	// config.CounterNumberDiscarded in 5a, config.CounterAccountDiscarded in 5b.
	// Before T-047, 5a counted NOTHING — deliberately triggered with live
	// traffic in T-042, the alarm came out correctly in the journal and the
	// instance kept showing `recebidas 1` and `conta_descartada 0`, meaning
	// the isolation rejection was invisible in `zapgw estado` and the only
	// trace was a journal line, which docs/ARMADILHAS.md records that no
	// one reads out of habit. Why there are TWO keys and not one: see the
	// comment on CounterNumberDiscarded in internal/config/contador.go.
	//
	// NEITHER OF THE TWO COUNTS `recebidas`, AND THAT IS DELIBERATE — the
	// question was asked in T-047 and the answer is NO. In this handler
	// `recebidas` means "webhook that made it through to DELIVERY" (and
	// that's why it's incremented further down, next to CounterKeys,
	// and not at the top of the method): EVERY early exit — 404 for the
	// slug, 503 for paused, 413 for a large body, 403 for the signature, 5a
	// and 5b — is left out. Making only 5a and 5b start counting would
	// create a NEW asymmetry among early exits, which is the exact shape of
	// this project's mother-trap, and it would also retroactively change
	// the meaning of a number that has already been read in production.
	// Whoever one day wants a true denominator ("how many webhooks did Meta
	// hit here?") adds their OWN key, counted at the top of the method —
	// they don't redefine this one.
	if inst.Type == config.TypeInstagram {
		// 5-instagram. The ONLY routing key an Instagram payload carries is
		// `entry[].id` (the IGID) — there is no phone_number_id and no
		// waba_id, so there is no 5a/5b to distinguish: every `entry` in
		// the batch has to belong to this instance. SAME discipline as 5b:
		// "" (missing, or unreadable) is treated as NOT MATCHING, never as
		// "matches by default" — see meta.IgIDsInPayload.
		for _, igID := range meta.IgIDsInPayload(raw) {
			if igID == "" || igID != inst.IgID {
				if igID == "" {
					log.Printf("ALARME zapgw: instancia %q recebeu webhook de Instagram sem entry[].id legivel; "+
						"nao da para provar que e dela (%q), entao o lote foi recusado", slug, inst.IgID)
				} else {
					log.Printf("ALARME zapgw: instancia %q recebeu webhook de Instagram do entry[].id %q, que nao e o dela (%q)",
						slug, igID, inst.IgID)
				}
				// 200: redelivering would repeat the same mismatch for 36h.
				// The fix is a person.
				w.WriteHeader(http.StatusOK)
				// Reuses CounterAccountDiscarded (not a new key): the question
				// this key answers — "was there an isolation rejection at
				// the ACCOUNT/ENTRY level, with no finer message-level key
				// to separate it?" — is EXACTLY the same one WhatsApp's 5b
				// answers. See the comment on CounterNumberDiscarded
				// (internal/config/contador.go) for why there are two
				// DIFFERENT keys when the question CHANGES — here it
				// doesn't.
				if h.counter != nil {
					h.counter.Record(slug, config.CounterAccountDiscarded)
				}
				return
			}
		}
	} else {
		// 5. Tenant isolation guard — TWO checks, one per webhook FORMAT,
		// because message/status and ACCOUNT webhooks do not carry the SAME
		// routing key (T-038, 2026-07-26).
		//
		// 5a. message/status: the body's phone_number_id has to be the path
		//     instance's own. The guard rejects the WHOLE BATCH when it
		//     finds an event whose phone_number_id is NON-EMPTY and
		//     different from the path instance's. This block is the
		//     ORIGINAL one, byte for byte — it did not change in this task,
		//     and that is the no-regression guarantee: it's the path that
		//     carries real production traffic right now.
		//
		// 5b. an ACCOUNT webhook (template status, number quality, alerts)
		//     has NO phone_number_id at all — Meta doesn't support URL
		//     override for them and they always arrive at the main
		//     endpoint (confirmed at
		//     developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/override/,
		//     read on 2026-07-26). The ONLY routing key left is the
		//     waba_id from entry[].id — confirmed as "WhatsApp Business
		//     Account ID" in the parameter table at
		//     developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
		//     and in the example at
		//     developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/message_template_status_update/
		//     (both read on 2026-07-26; see meta.AccountWabaIDsInPayload).
		//     We compare it against the PATH instance's waba_id, for the
		//     SAME reason as 5a — and the comment at the top of this file:
		//     the instance comes from the path, never from the body. A
		//     waba_id that doesn't match does NOT reroute the delivery to
		//     another instance — it only rejects this one.
		//
		// WITHOUT 5b (found by the consumer on 2026-07-26): an account
		// webhook arrives at the main endpoint, 5a sweeps ZERO events (an
		// account change produces no Event — the gateway doesn't model
		// its content), nothing gets rejected, and the RAW BODY is
		// delivered to the consumer of the slug that's in the path even
		// though it belongs to another WABA. It isn't just misrouting: the
		// consumer checks envelope["instancia"] against THEIR OWN slug,
		// the guard passes (WE were the ones who put the path's slug in
		// the envelope), and they WRITE TO THEIR DATABASE, PERMANENTLY,
		// the raw body of an event that may belong to another tenant. Data
		// written into someone else's database doesn't undo itself —
		// unlike misrouting, which fixes itself by repointing.
		//
		// WHAT THE TWO GUARDS TOGETHER DON'T COVER, and this remains a
		// known gap, not a guarantee: a parse that failed entirely (the
		// body isn't a JSON object — 5a and 5b find no entry at all) and
		// an events/changes list that's empty for any other reason. In
		// both cases there is nothing to compare; neither one was the
		// trigger for this task.
		//
		// WHY REJECT THE WHOLE BATCH instead of filtering by event/change:
		// the RAW body travels along in the delivery. Filtering and still
		// sending the raw body would give one instance's consumer another
		// instance's content.
		//
		// OPERATIONAL CONSTRAINT, and it is SIMPLER than this comment used
		// to claim up to 2026-07-26. Each consumer uses their OWN App on
		// Meta (owner's decision, 2026-07-26) and the Callback URL is PER
		// APP — so each instance already receives on ITS OWN path
		// (/v1/inbound/{slug}) by construction, with no one configuring
		// any override at all. INPUT isolation is the URL path plus that
		// instance's app_secret, which belongs to a different App: a
		// signature valid for one is not valid for the other (exercised
		// with live traffic in T-042, 2026-07-26 — 200 on one's path, 403
		// with the SAME bytes and the SAME signature on the other's path).
		//
		// WEBHOOK OVERRIDE (per WABA, per number) is the path for whoever
		// one day puts two numbers under the SAME App — and only for that
		// case. The sentence that used to be here cited the override as a
		// generic operational constraint, which sent the reader chasing
		// after a Meta feature they don't need to separate two consumers.
		//
		// ACCOUNT webhooks still have no override at all: they always
		// arrive at the App's main endpoint, and that's exactly why 5b can
		// only compare waba_id, never phone_number_id.
		//
		// 200, not 5xx, in both cases: redelivering would repeat the same
		// mismatch for 36h. The fix is configuration, and a person does
		// it — hence ALARME in both.
		//
		// BOTH COUNT, and each under ITS OWN key (T-047, 2026-07-26):
		// config.CounterNumberDiscarded in 5a, config.CounterAccountDiscarded in
		// 5b. Before T-047, 5a counted NOTHING — deliberately triggered
		// with live traffic in T-042, the alarm came out correctly in the
		// journal and the instance kept showing `recebidas 1` and
		// `conta_descartada 0`, meaning the isolation rejection was
		// invisible in `zapgw estado` and the only trace was a journal
		// line, which docs/ARMADILHAS.md records that no one reads out of
		// habit. Why there are TWO keys and not one: see the comment on
		// CounterNumberDiscarded in internal/config/contador.go.
		//
		// NEITHER OF THE TWO COUNTS `recebidas`, AND THAT IS DELIBERATE —
		// the question was asked in T-047 and the answer is NO. In this
		// handler `recebidas` means "webhook that made it through to
		// DELIVERY" (and that's why it's incremented further down, next
		// to CounterKeys, and not at the top of the method): EVERY
		// early exit — 404 for the slug, 503 for paused, 413 for a large
		// body, 403 for the signature, 5a and 5b — is left out. Making
		// only 5a and 5b start counting would create a NEW asymmetry
		// among early exits, which is the exact shape of this project's
		// mother-trap, and it would also retroactively change the meaning
		// of a number that has already been read in production. Whoever
		// one day wants a true denominator ("how many webhooks did Meta
		// hit here?") adds their OWN key, counted at the top of the
		// method — they don't redefine this one.
		for _, e := range evs {
			if e.PhoneNumberID != "" && e.PhoneNumberID != inst.PhoneNumberID {
				log.Printf("ALARME zapgw: instancia %q recebeu phone_number_id %q, que nao e dela",
					slug, e.PhoneNumberID)
				// 200: redelivering would repeat the same failure for 36h.
				// The fix is a person.
				w.WriteHeader(http.StatusOK)
				// T-035: count ONLY AFTER w.WriteHeader — see the comment
				// on h.counter further below in this method.
				if h.counter != nil {
					h.counter.Record(slug, config.CounterNumberDiscarded)
				}
				return
			}
		}
		// AN UNREADABLE WABA_ID DOES NOT PASS (T-068).
		// meta.AccountWabaIDsInPayload returns "" when the account
		// webhook's `entry.id` either didn't come at all OR can't be read
		// (Meta sending a format this parser doesn't know), and the
		// comparison below treats "" as not-matching — because this
		// guard's question is "can we PROVE this webhook belongs to this
		// instance?", and with no readable waba_id the answer is no. The
		// `waba == ""` is written EXPLICITLY instead of being trusted to
		// the inequality check: an instance with an empty waba_id in the
		// database would make "" match "", and the guard would pass
		// silently, which is exactly how a guarantee turns into decoration.
		//
		// The alternative — discard only the unreadable `entry` and
		// deliver the rest — was rejected for the reason written above in
		// this same comment: the RAW body travels along in the delivery,
		// so filtering events does NOT stop that account's content from
		// reaching the consumer. See meta.AccountWabaIDsInPayload.
		for _, waba := range meta.AccountWabaIDsInPayload(raw) {
			if waba == "" || waba != inst.WabaID {
				if waba == "" {
					log.Printf("ALARME zapgw: instancia %q recebeu webhook de CONTA sem waba_id legivel; "+
						"nao da para provar que e dela (%q), entao o lote foi recusado", slug, inst.WabaID)
				} else {
					log.Printf("ALARME zapgw: instancia %q recebeu webhook de CONTA da waba_id %q, que nao e a dela (%q)",
						slug, waba, inst.WabaID)
				}
				// 200: redelivering would repeat the same mismatch for
				// 36h. The fix is a person.
				w.WriteHeader(http.StatusOK)
				// T-035: count ONLY AFTER w.WriteHeader — see the comment
				// on h.counter further below in this method.
				if h.counter != nil {
					h.counter.Record(slug, config.CounterAccountDiscarded)
				}
				return
			}
		}
	}

	correlation := h.nextCorrelation()

	// 6. Delivery, and mirror the verdict.
	status, deliveryErr := h.deliverer.Deliver(
		r.Context(), inst, raw, evs, parseErr, correlation, r.Header.Get("X-Hub-Signature-256"))
	v := ConsumerVerdict(status, deliveryErr)
	// The two halves log for DIFFERENT reasons, and that's why neither
	// replaces the other:
	//
	//   v.Alarm                              -> someone NEEDS to act. It's
	//     the only way the permanent-loss alarm gets out: on the STATUS
	//     axis, v.Alarm only ever comes together with StatusForMeta 200,
	//     so IF the condition below were just "non-2xx," that case would
	//     never log — that was exactly the defect that erased the
	//     permanent-loss alarm. (On the ERROR axis there's an alarm with
	//     504 — the certificate failure —, which the same condition already
	//     covers.)
	//   StatusForMeta outside 2xx            -> the trace matters: it's
	//     the case where "where did the message stop?" has an answer worth
	//     recording, even when no one needs to act right now (Meta
	//     redelivers on its own).
	//
	// Without the OR, a 200-with-Alarm (Meta NEVER redelivers) would stay
	// silent.
	if v.Alarm || v.StatusForMeta < 200 || v.StatusForMeta >= 300 {
		prefix := "zapgw"
		if v.Alarm {
			prefix = "ALARME zapgw"
		}
		log.Printf("%s: instancia=%q correlacao=%s %s", prefix, slug, correlation, v.Reason)
	}

	w.WriteHeader(v.StatusForMeta)

	// T-035: count ONLY AFTER the response to Meta has been written, never
	// before. w.WriteHeader already fixed the status on the line above;
	// nothing that happens from here on can change what Meta received.
	// Register (config.Counter) returns no error at all by signature — a
	// counting failure only leaves a log behind, and that is the guarantee
	// this task's CLAUDE.md requires: a counter that brings delivery down
	// is infinitely worse than a missing counter.
	if h.counter != nil {
		h.counter.Record(slug, config.CounterReceived)
		for _, key := range CounterKeys(status, deliveryErr, v) {
			h.counter.Record(slug, key)
		}
	}

	// T-063: billing by category, in the same place and under the same
	// rule — AFTER the response to Meta has been written. The EXTRA
	// condition (only count when that response was 2xx) lives inside
	// countBilling, with the why: a non-2xx is a request for redelivery,
	// and a redelivery counted again would inflate a number that turns
	// into money on the consumer's dashboard.
	h.countBilling(slug, evs, v)

	// T-080: the same place and the same rule as the counter — AFTER the
	// response to Meta has been written, and through a path that cannot
	// return an error.
	h.recordNumberLimit(slug, evs)

	// T-091: TRANSIT log, in the SAME place and under the SAME rule as the
	// counters above — AFTER the response to Meta has been written,
	// through a path that cannot return an error (config.Transit.Register).
	h.recordTransit(slug, evs, correlation, v.Reason)
}

// recordTransit writes the TRANSIT log (T-091) for the received batch.
//
// ONE LINE PER MODELED EVENT — not one per webhook — because a POST can
// carry more than one event (more than one sender, more than one status)
// and each one has its OWN counterpart: a single line per webhook could
// only index ONE phone number, hiding the others from an HMAC search. All
// the lines from the same POST share `correlation` and `outcome`, which
// describe the whole batch, not the event.
//
// WHEN THE BATCH CARRIED NO MODELED EVENT AT ALL (parse failed, or there
// was only a field this gateway doesn't model), it writes ONE line with
// `tipo` and the counterpart/wamid empty — without this, a webhook that
// arrived and was answered would leave no trace at all in this log, and
// "something arrived, correlation X, here's the outcome" is still useful
// information for whoever investigates.
//
// `correlation` GOES OUT RAW HERE, and NOT in outbound
// (internal/outbound/handler.go): this value is the OPAQUE id that
// `h.nextCorrelation()` itself GENERATES — no one from outside chooses
// its content, so storing it in plaintext leaks nothing, and it's the SAME
// value that shows up in the journal next to the verdict (`v.Reason`),
// serving to cross-reference the two sources of the same event. In
// outbound, `correlation` is the CONSUMER's `Idempotency-Key` — free-form
// text of external origin — and that's why it goes out as an HMAC
// (config.Store.HMACCorrelation), never in plaintext. See the comment on
// config.TransitRecord.Correlation.
func (h *Handler) recordTransit(slug string, evs []meta.Event, correlation, outcome string) {
	if h.transit == nil {
		return
	}
	if len(evs) == 0 {
		h.transit.Record(config.TransitRecord{
			Slug: slug, Direction: config.DirectionInbound,
			Correlation: correlation, Outcome: outcome,
		})
		return
	}
	for _, e := range evs {
		counterpart, wamid := counterpartAndWamidOfEvent(e)
		h.transit.Record(config.TransitRecord{
			Slug:         slug,
			Direction:    config.DirectionInbound,
			Counterparty: counterpart,
			Wamid:        wamid,
			Type:         string(e.Type),
			Correlation:  correlation,
			Outcome:      outcome,
		})
	}
}

// counterpartAndWamidOfEvent returns the ALREADY CANONICAL phone number of
// the counterpart and the wamid of ONE event — the only two values the
// transit log stores in PLAINTEXT since T-094
// (config.TransitRecord.Counterparty/Wamid).
//
// ONLY message and status HAVE a counterpart: ACCOUNT webhooks (template
// status, category, number quality, alert) don't talk about a conversation
// with a specific number, and that's why they return both empty —
// config.WriteTransit writes "" as "no counterpart," it never invents a
// value.
func counterpartAndWamidOfEvent(e meta.Event) (counterpart, wamid string) {
	switch e.Type {
	case meta.EventTypeMessage:
		return e.FromCanonical, e.WaMessageID
	case meta.EventTypeStatus:
		return e.ToCanonical, e.WaMessageID
	default:
		return "", ""
	}
}

// recordNumberLimit stores the tier the
// `phone_number_quality_update` webhook pushed (T-080).
//
// WHY `current_limit` AND NOT `max_daily_conversations_per_business`: the
// first one is the number's MESSAGE LIMIT — the same quantity the watchdog's
// measurement reads from `whatsapp_business_manager_messaging_limit` — and
// the second is a different number, one that in Meta's dashboard sample
// happens to carry the SAME value (docs/META-CAMPOS-DE-WEBHOOK.md notes that
// this is exactly why the corpus has a synthetic fixture with the three
// different). Storing the wrong one under the right one's name would be
// exactly the kind of mix-up that fixture exists to catch.
//
// `old_limit` is NOT stored, and the omission is deliberate: this block
// answers "which tier is the number in RIGHT NOW." The DIRECTION of the
// change already travels whole in the `qualidade_do_numero` event the
// consumer receives — repeating it here would create a second copy to drift
// apart.
//
// THE TIMESTAMP IS OUR OWN CLOCK, not Meta's `entry.time`. The why
// (comparing two clocks no one synchronized would silently decide which
// source wins) lives in internal/config/numero.go, which is where the
// tie-break rule lives.
func (h *Handler) recordNumberLimit(slug string, evs []meta.Event) {
	for _, e := range evs {
		if e.Type != meta.EventTypeNumberQuality || e.NumberQuality == nil {
			continue
		}
		if e.NumberQuality.CurrentLimit == "" {
			continue
		}
		h.number.Record(slug, config.NumberUpdate{
			// "TIER_250" goes out as it arrived — never 250. Same rule as T-058.
			Limit:  e.NumberQuality.CurrentLimit,
			Source: config.SourceWebhook,
			When:   h.now(),
		})
	}
}

// rejectionCounter counts rejections per instance within a window, in
// memory, per process.
//
// THE PROJECT'S BOUNDARY, and it is not crossed here: this holds
// CONFIGURATION, never a MESSAGE. This counter stores one integer and one
// instant per slug — it does not store the rejected body, nor a piece of it,
// nor a hash of it. Storing the body "to diagnose later" would be a
// redelivery queue, and a queue belongs to the consumer.
//
// AND THAT'S WHY IT DOESN'T NEED TO SURVIVE A RESTART: losing the count on a
// restart costs, at most, a delayed alarm; persisting it would bring in
// state this gateway exists specifically not to have.
//
// THE MAP DOES NOT GROW WITHOUT LIMIT: only a slug that exists in the store
// and is active reaches this point — the handler checks both BEFORE reading
// the body (receive, steps 1 and 2) —, so the keys are limited to registered
// instances, never to whatever an attacker types into the URL.
type rejectionCounter struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	// now is injectable because the alternative, in a window test, would
	// be sleeping for an hour.
	now     func() time.Time
	perSlug map[string]*countingWindow
}

type countingWindow struct {
	n     int
	start time.Time
}

func newRejectionCounter(threshold int, window time.Duration) *rejectionCounter {
	return &rejectionCounter{
		threshold: threshold,
		window:    window,
		now:       time.Now,
		perSlug:   map[string]*countingWindow{},
	}
}

// record counts one rejection for the instance and returns how many there
// have already been in the current window and whether THIS one was the one
// that crossed the threshold.
//
// WHY A MUTEX AND NOT atomic: these are three fields that only make sense
// together (the count, the window's start, and the decision to alarm).
// http.Server serves every request in its own goroutine over the SAME
// handler, and an unsynchronized counter here has already cost a Critical in
// this file (docs/ARMADILHAS.md).
//
// WHY `==` AND NOT `>=`: with `>=`, every subsequent rejection within the
// same window would alarm again — a burst would turn into hundreds of ALARME
// lines, and a repeated alarm trains whoever operates it to ignore it. This
// way at most ONE alarm goes out per instance per window; if the condition
// persists, it comes back in the next window, which is the interval in
// which the information is new.
func (c *rejectionCounter) record(slug string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	j := c.perSlug[slug]
	if j == nil || now.Sub(j.start) >= c.window {
		j = &countingWindow{start: now}
		c.perSlug[slug] = j
	}
	j.n++
	return j.n, j.n == c.threshold
}

func (h *Handler) nextCorrelation() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" +
		strconv.FormatInt(h.seq.Add(1), 36)
}
