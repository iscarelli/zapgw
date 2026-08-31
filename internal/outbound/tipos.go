// AcceptedTypes — each route DECLARES, at handler CONSTRUCTION, which
// instance types (config.TypeWhatsApp, config.TypeInstagram) it knows how to
// serve (T-111).
//
// WHY THIS EXISTS: until this task, only four files in internal/outbound/
// knew about config.TypeInstagram (estado.go, fumaca.go, handler.go,
// renovador_instagram.go). FIVE ROUTES used a WhatsApp field (PhoneNumberID,
// WabaID) on ANY instance — empty on an Instagram instance —, and stayed
// that way in SILENCE when T-097 added the type: none of them had to decide
// anything, because there was no decision to make. The real defect wasn't
// the five: it was that a NEW TYPE INHERITS EVERYTHING by omission. This
// declaration forces the decision to EXIST.
//
// 🔴 IT IS A MANDATORY POSITIONAL PARAMETER on the `NewHandler…` calls,
// NEVER an optional field on an options struct nor `variadic`: only that way
// does OMITTING it fail to COMPILE. If this ever becomes optional, the whole
// task loses its point — it goes back to the ad-hoc checks from before, just
// with more ceremony.
//
// 🔴 DO NOT TURN THIS INTO net/http MIDDLEWARE, and the reason is
// TECHNICAL, not aesthetic: the instance identifier is NOT uniformly in the
// URL — it comes in the JSON BODY on seven routes, in the QUERY on
// GET /v1/estado, and in the PATH on health/inbound. A middleware would have
// to read the body (today read ONCE, with a byte cap, by httpx.ReadRaw) and
// reinject it, changing every handler anyway. The declaration lives at
// CONSTRUCTION; the check runs at the ONE point where `inst` is already
// known — inside each handler, after the binding check. If someone
// "improves" this into middleware, the problem comes back.
package outbound

import (
	"fmt"
	"net/http"

	"github.com/iscarelli/zapgw/internal/config"
)

// AcceptedTypes is the CLOSED vocabulary of possible declarations.
type AcceptedTypes int

const (
	// WhatsAppOnly rejects an Instagram instance (400, class "config") — see
	// checkType, below.
	WhatsAppOnly AcceptedTypes = iota
	// InstagramOnly is the mirror of WhatsAppOnly — STILL UNUSED: no route today
	// serves ONLY Instagram. It exists for the day one does, so that handler
	// also has to DECLARE instead of inheriting by omission.
	InstagramOnly
	// AllTypes declares that the handler serves BOTH known types and
	// handles the difference INTERNALLY. IT IS NOT AN ESCAPE HATCH: it is an
	// AFFIRMATION that someone looked. Every construction site that passes
	// AllTypes carries, alongside it, a comment saying WHY (see
	// cmd/zapgw/main.go) — the same discipline this project already demands
	// of another recorded decision (docs/ARMADILHAS.md).
	AllTypes
)

// knownType normalizes `tipo` to what it ACTUALLY means: "" and
// config.TypeWhatsApp are the SAME type — every row written before T-097
// has an empty `tipo` column (see the comment on config.Instance.Type).
//
// DO NOT CONFUSE WITH "accepts anything": a type that is neither of the TWO
// known ones (a future third type) does not normalize to whatsapp here — it
// only starts matching something on the day someone TEACHES this function
// about it. That is what makes the T-111 "third-type proof" work: without
// this selective normalization, an unknown type would fall into the `else`
// branch of an `if tipo == TypeInstagram` by accident.
func knownType(kind string) string {
	if kind == "" {
		return config.TypeWhatsApp
	}
	return kind
}

// aceita reports whether `tipo` is among the types `t` declares it accepts.
//
// THE COMPARISON IS BY EXPLICIT INCLUSION, NEVER BY EXCLUSION — the same
// discipline as config.ValidateInstanceType (internal/config/store.go):
// WhatsAppOnly accepts `tipo == TypeWhatsApp`, never `tipo != TypeInstagram`.
// A future THIRD type cannot fall into any branch by omission — not even
// AllTypes, which only accepts the TWO types this file knows about
// TODAY. Until someone teaches this function about a new type, NO
// declaration accepts it.
func (t AcceptedTypes) accepts(kind string) bool {
	kind = knownType(kind)
	switch t {
	case WhatsAppOnly:
		return kind == config.TypeWhatsApp
	case InstagramOnly:
		return kind == config.TypeInstagram
	case AllTypes:
		return kind == config.TypeWhatsApp || kind == config.TypeInstagram
	default:
		// AcceptedTypes is only ever born from one of the three constants
		// above — reaching here means a value nobody declared, and
		// rejecting (never accepting by omission) is the same discipline as
		// the rest of this function.
		return false
	}
}

// checkType is the ONLY function that REJECTS by instance type — called
// by the WRITE routes (item 4 of T-111): a single function, never five
// copies that would diverge on the first change (docs/ARMADILHAS.md, this
// project's mother-trap).
//
// 🔴 RUNS AFTER authentication and the binding check, NEVER BEFORE —
// mandatory order: token -> binding (403) -> type. An instance that belongs
// to someone else or does not exist still returns 403/404, never this 400:
// otherwise the route turns into an oracle answering "does this slug
// exist?" and "what type is it?" for someone who does not own it.
//
// Already writes the 400/config response when it rejects; returns true when
// the type is accepted (the caller proceeds without doing anything else).
//
// `orientacao` is what to do INSTEAD of this route — can be "" when there
// is no alternative to point to.
func checkType(w http.ResponseWriter, types AcceptedTypes, inst config.Instance, guidance string) bool {
	if types.accepts(inst.Type) {
		return true
	}
	msg := fmt.Sprintf("esta rota nao se aplica a instancias do tipo %q", knownType(inst.Type))
	if guidance != "" {
		msg += ". " + guidance
	}
	respondError(w, http.StatusBadRequest, "config", msg, 0)
	return false
}
