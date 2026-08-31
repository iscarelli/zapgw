// Count by BILLING CATEGORY (T-063, 2026-07-28).
//
// WHAT THIS FILE EXISTS TO DO: the consumer already projects cost by
// multiplying volume by rate, with the volume ESTIMATED on their side. Meta
// sends, in every status webhook, under which category it billed that
// delivery (`pricing.category`/`billable`, which the parser translates to
// meta.Billing). Counting this here swaps the projection for MEASUREMENT —
// the owner's rule applied to money: "a number the gateway promises has to
// come from the gateway."
//
// 🔴 THE DECISION THAT MAKES THE NUMBER MEAN SOMETHING: ONLY `sent` COUNTS.
//
// The same wamid appears in `sent`, `delivered`, and `read`, and all three can
// carry `pricing`. This is NOT a guess: in the corpus, status_sent_com_pricing.json
// and status_delivered.json are the capture of the SAME send (same wamid, same
// timestamp, same `service` category), and status_read_com_cobranca.json is a
// `read` with its own `pricing`. Counting every status that carries a billing
// block would multiply the measured invoice by up to THREE, and an inflated
// number is worse than none: it looks like measurement and replaces the
// estimate that was at least honest about being an estimate. The comment on
// CounterReadsMarked (internal/config/counter.go) had already flagged this
// risk one level up — here it is resolved, not inherited.
//
// WHY `sent` AND NOT `delivered`, which in the sample never comes without
// `pricing`: `sent` is the only state that EVERY billed message passes
// through. A message can be sent and never delivered (device off for days) —
// Meta already charged for it, and a counter anchored on `delivered` would
// miss it. The price of this choice is measured and has its own key: 4 of 53
// raw `sent` came WITHOUT `pricing` (~7.5%, consumer-a, T-069), and those fall
// into config.CounterBillingAbsent instead of vanishing.
//
// WHAT THIS NUMBER IS NOT, and the contract says so to the consumer: it counts
// ACCEPTED `sent` WEBHOOKS, not unique messages. The gateway keeps no
// per-message state (docs/ARMADILHAS.md and the comment on rejectionCounter:
// this is where configuration lives, never a message), so a Meta redelivery
// after a 200 would count again — the same caveat that `recebidas` and
// `entregues` already carry. The common redelivery case (we answered 5xx) is
// covered: counting only happens when the response to Meta was 2xx, see
// BillingKeys in use in the handler.
package inbound

import (
	"log"
	"sync"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// billingStatus is the only message state that feeds the billing count.
// See the comment at the top of this file for why it has to be just ONE.
//
// The value is literal and comes from Meta, like every status:
// internal/meta/parse.go passes `statuses[].status` through exactly as it
// arrived, with no vocabulary of ours in between.
const billingStatus = "sent"

// keyByBillingCategory matches the LITERAL value of `pricing.category`
// against the key from the closed vocabulary.
//
// THE FOUR ARE THE OBSERVED ONES, and the comparison is EXACT — no
// lowercasing, no trimming, no synonym table. If Meta starts sending
// "MARKETING" in uppercase, the event falls into `cobranca_outra` WITH THE
// VALUE IN THE LOG, which is precisely the path designed for "Meta changed
// something": the count doesn't disappear, it moves column, and the log says
// which column to create. Normalizing the case here would be a disguised
// translation table — it would hide the change instead of flagging it, and
// this project has already paid the price for asserting about Meta without
// measuring.
var keyByBillingCategory = map[string]string{
	"marketing":      config.CounterBillingMarketing,
	"utility":        config.CounterBillingUtility,
	"authentication": config.CounterBillingAuthentication,
	"service":        config.CounterBillingService,
}

// BillingKeys returns the counter keys that this batch of events must
// increment, and the `pricing.category` values it was NOT able to classify.
//
// PURE FUNCTION, deliberately: who logs and who writes is the handler, after
// the response to Meta has already been written. This way the decision
// ("which key?") is testable without HTTP, without a database and without a
// clock, and no path here can delay the response.
//
// An unknown category comes out in BOTH lists: it counts in
// config.CounterBillingOther and the literal value travels to the log. The
// counter says HOW MANY, the log says WHICH — kept separate because a
// vocabulary that grew with the received value would let Meta choose the keys
// on our dashboard (see CounterBillingOther, in internal/config/counter.go).
func BillingKeys(evs []meta.Event) (keys []string, unknownCategories []string) {
	for _, e := range evs {
		if e.Type != meta.EventTypeStatus || e.Status != billingStatus {
			continue
		}
		if e.Billing == nil {
			// `sent` without `pricing` is the NORMAL case (~7.5% of measured
			// traffic), not an error — and it vanishes from the measurement
			// if it has no key of its own.
			keys = append(keys, config.CounterBillingAbsent)
			continue
		}
		key, known := keyByBillingCategory[e.Billing.Category]
		if !known {
			// Includes the case where `pricing` is present without
			// `category` (the parser accepts this when `billable` came
			// alone): "I don't know how to classify this" is exactly what
			// `cobranca_outra` answers, and the log comes out with the
			// empty string, which is the true information.
			key = config.CounterBillingOther
			unknownCategories = append(unknownCategories, e.Billing.Category)
		}
		keys = append(keys, key)
		// ONLY `true`. Absent and `false` are not the same thing (that's why
		// Billable is *bool), but neither one is "Meta charged."
		if e.Billing.Billable != nil && *e.Billing.Billable {
			keys = append(keys, config.CounterBillingBillable)
		}
	}
	return keys, unknownCategories
}

// countBilling writes the batch's billing keys and warns about an unknown
// category.
//
// CALLED ONLY AFTER w.WriteHeader AND ONLY WHEN THE RESPONSE TO META WAS 2xx —
// the two conditions are deliberate and different:
//
//   - AFTER: T-035 rule. config.Counter.Register does not return an error by
//     signature, so counting can never bring delivery down.
//   - ONLY ON 2xx: a non-2xx is a request for REDELIVERY, and Meta redelivers.
//     Counting before accepting would add up the same `sent` once per
//     attempt — five attempts in 9 s have already been measured in our own
//     access log —, and the error would show up precisely during a consumer
//     incident, exactly when the number is watched most closely. `recebidas`
//     accepts this noise because it counts traffic; here the number is money.
func (h *Handler) countBilling(slug string, evs []meta.Event, v Verdict) {
	if v.StatusForMeta < 200 || v.StatusForMeta >= 300 {
		return
	}
	keys, unknown := BillingKeys(evs)
	if h.counter != nil {
		for _, key := range keys {
			h.counter.Record(slug, key)
		}
	}
	for _, category := range unknown {
		h.warnedCategories.warn(slug, category)
	}
}

// unknownCategoryWarning logs ONCE per (slug, category) per process.
//
// WHY ALARM: a category this vocabulary doesn't know is money falling into
// `cobranca_outra`, and only a person can fix it — someone needs to add the
// key. It's the mirror.go criterion ("ALARM = needs a person"), applied to a
// case where no one dies today but the measurement stops adding up.
//
// WHY ONLY ONCE: without this, a new category would generate one ALARM line
// per MESSAGE. A repeated alarm trains whoever operates it to ignore it, and
// then the alarm that matters disappears into the noise along with it — the
// same reason for `==` (and not `>=`) in rejectionCounter, right below in
// this package. The counter already answers "how many times"; the log exists
// to answer "which value", and that answer doesn't change on repetition.
//
// THE MAP DOES NOT GROW WITHOUT LIMIT: the category comes from a payload
// whose SIGNATURE has already been checked (step 3 of the handler), so who
// chooses these values is Meta, never whoever types into the URL. And losing
// the map on a restart is cheap on purpose — the warning comes back, which is
// the correct behavior for whoever just brought the service up and wants to
// know what's happening right now.
type unknownCategoryWarning struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newUnknownCategoryWarning() *unknownCategoryWarning {
	return &unknownCategoryWarning{seen: map[string]bool{}}
}

func (a *unknownCategoryWarning) warn(slug, category string) {
	a.mu.Lock()
	key := slug + "\x00" + category
	first := !a.seen[key]
	a.seen[key] = true
	a.mu.Unlock()
	if !first {
		return
	}
	log.Printf("ALARME zapgw: instancia %q recebeu cobranca da Meta na categoria %q,"+
		" que nao esta no vocabulario de contadores — os eventos dela estao caindo em %q"+
		" e a medicao de custo por categoria fica incompleta enquanto isso."+
		" ACAO: acrescentar a chave em internal/config/counter.go (ela aparece sozinha em"+
		" `zapgw estado` e em GET /v1/estado). Este aviso sai UMA vez por categoria por processo",
		slug, category, config.CounterBillingOther)
}
