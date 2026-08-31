// Fumaca — the ONLY path that runs the smoke test's four steps and, IF Meta
// accepts the send, activates the instance (T-084).
//
// ONE PATH, TWO FACADES: `cmd/zapgw fumaca` (command line, for whoever has
// shell access on the gateway machine) and `POST /v1/fumaca`
// (fumaca_handler.go, for the third-party consumer who does NOT) call this
// SAME function. Two copies would diverge, and the one that diverged would
// be the one nobody runs by hand — docs/MODELO-DE-USO.md, item 7.
//
// THE ORDER OF THE STEPS IS THE GUARANTEE, and each one exists because the
// previous one isn't enough:
//
//	(1) the instance exists          — without this, the following steps
//	                                    would be talking about a mistyped
//	                                    slug;
//	(2) the Graph API accepts the token — GET /{phone_number_id}. It's the
//	                                    ONLY way to catch a token the client
//	                                    revoked without sending a message to
//	                                    a real number.
//	                                    🔴 T-104: SKIPPED for an Instagram
//	                                    instance — there is, MEASURED, no
//	                                    equivalent call on that host (see
//	                                    step 2 below, in the function body,
//	                                    for the source);
//	(3) a message goes OUT, with an id — a 200 from Meta doesn't prove an id
//	                                    came back;
//	(4) only THEN, activate.
//
// It aborts on the FIRST step that fails, and failing on any of them leaves
// the instance PAUSED. THERE IS NO FORCE FLAG: a path to `ativo = 1` that
// doesn't go through here undoes the whole guarantee — ActivateInstance
// (internal/config/store.go:957) is the ONLY path to `ativo = 1` in this
// project, and this function is its ONLY caller outside of tests.
//
// INSTANCE ALREADY ACTIVE: this function sends NO message at all, and
// returns AlreadyActive. Without this guard, `POST /v1/fumaca` becomes a way
// to burn paid messages in a loop — it's the ONLY route in the gateway that
// sends without the consumer having asked for a send. And resending over an
// already-active instance proves nothing the first proof hadn't already
// proven: to force a new proof, pause first (`POST /v1/pausa`) — coming back
// requires a new smoke test, on purpose.
package outbound

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// SmokeText is what shows up on the receiver's phone. It identifies
// itself in its own words: whoever receives this is a real person, and an
// anonymous "test" message scares people or turns into a spam report.
const SmokeText = "zapgw: teste de fumaca do canal. " +
	"Se voce recebeu esta mensagem, o envio esta funcionando. Nao e preciso responder."

var (
	// ErrSmokeNoSlug: no instance was specified.
	ErrSmokeNoSlug = errors.New("outbound: slug e obrigatorio")
	// ErrSmokeNoDestination: NO DEFAULT, on purpose — a default here would
	// send a message to the wrong number, and a sent message can't be undone.
	ErrSmokeNoDestination = errors.New("outbound: destino e obrigatorio e nao tem default — o teste de fumaca manda uma mensagem DE VERDADE")
	// ErrSmokeActivationFailed: the test message WENT OUT (Meta accepted it,
	// with an id) but the ActivateInstance that should have come right after
	// failed. NEEDS A HUMAN: the instance stays PAUSED despite a real message
	// having been delivered, and running the smoke test again would send
	// ANOTHER real message — that's why the HTTP facade doesn't treat this as
	// "just try again".
	ErrSmokeActivationFailed = errors.New("outbound: a mensagem de teste foi enviada, mas o gateway falhou ao ativar a instancia")
)

// SmokeResult is what both facades need to report the outcome —
// neither rebuilds it on its own.
type SmokeResult struct {
	Instance      config.Instance
	AlreadyActive bool
	WaMessageID   string
}

// identificationField says, only for the PROGRESS LINE (step 1/4), which
// name to use for the identifier that steps 2 and 3 will use — it decides
// nothing, it just keeps the message from calling an ig_id a
// "phone_number_id".
func identificationField(kind string) string {
	if kind == config.TypeInstagram {
		return "ig_id"
	}
	return "phone_number_id"
}

// SmokeWithInstagramBase runs the four steps. See the file's header for
// the whole guarantee.
//
// `baseInstagram` is the root of the graph.instagram.com host used by EVERY
// Instagram call in this function (T-104) — production uses
// meta.DefaultInstagramRenewalBase (via Fumaca(), above); a fake server only
// in test or in the lab (T-071), NEVER the real Meta outside of production.
// SAME pattern as RenewInstagramToken (internal/meta/instagram.go) and
// InstagramRenewer.renewalBase (renovador_instagram.go) — receiving the
// base by PARAMETER, never embedding a second copy of the host.
//
// `report`, when not nil, receives a progress line after each successful
// step (or skipped one — see step 2). It NEVER decides anything — it exists
// only so the command-line facade can report on a network call that can take
// seconds. The HTTP route passes nil.
func SmokeWithInstagramBase(ctx context.Context, store *config.Store, client *meta.Client, counter *config.Counter, slug, destination, baseInstagram string, report func(string)) (SmokeResult, error) {
	if report == nil {
		report = func(string) {}
	}

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return SmokeResult{}, ErrSmokeNoSlug
	}
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return SmokeResult{}, ErrSmokeNoDestination
	}

	// --- step 1: the instance exists ----------------------------------------
	inst, err := store.FindInstance(slug)
	if err != nil {
		return SmokeResult{}, fmt.Errorf("passo 1/4 (a instancia existe): %w", err)
	}
	// T-097: the identifier that shows up in the line changes with the
	// TYPE — showing an empty phone_number_id on an Instagram instance (or
	// the reverse) isn't an error at all, but it would confuse whoever is
	// reading the progress.
	identifier := inst.PhoneNumberID
	if inst.Type == config.TypeInstagram {
		identifier = inst.IgID
	}
	report(fmt.Sprintf("passo 1/4 OK: instancia %q encontrada (%s %s, ativo=%t).",
		inst.Slug, identificationField(inst.Type), identifier, inst.Active))

	// ALREADY ACTIVE: doesn't send a message. See the file's header.
	if inst.Active {
		return SmokeResult{Instance: inst, AlreadyActive: true}, nil
	}

	deadline := InstanceDeadline(inst)

	// --- step 2: the Graph API accepts the token --------------------------------
	// It's the ONLY way to catch a token the client revoked without sending a
	// message to a real number. It runs BEFORE the send on purpose: without
	// this, every run of the smoke test over a dead token would still try to
	// send a real message.
	//
	// 🔴 T-104: SKIPPED FOR INSTAGRAM, ON PURPOSE — it's not a regression,
	// it's a measured decision. meta.CheckCredential does `GET /{id}` on
	// graph.facebook.com; for Instagram there is, MEASURED, no equivalent
	// call on graph.instagram.com (see the comment above
	// instagramSendResponse, internal/meta/instagram.go — the technique
	// `consumer-b` recorded that `debug_token` REFUSES the Instagram Login
	// token on both hosts). Forcing a `GET /{ig_id}` "by analogy" with
	// WhatsApp would be a question that CAN LIE — accepting or refusing for
	// a reason that isn't the token —, and this project would rather skip a
	// check than run one without a source (CLAUDE.md, "Doc errado é pior
	// que doc nenhum" — the same holds for a check). Meta STILL confirms the
	// token: only in step 3, when the test send actually goes out — if the
	// token is revoked, Meta refuses there, with the SAME error class
	// (`config`), just without the extra guarantee of aborting BEFORE
	// spending the send attempt.
	if inst.Type == config.TypeInstagram {
		report("passo 2/4 PULADO (instagram): nao ha, medida, uma chamada que confirme o token " +
			"sem enviar mensagem neste host — a Meta confirma no passo 3.")
	} else {
		// meta.CheckCredential does `GET /{id}` with the token — the
		// SAME question the health probe (saude_handler.go) and the watchdog
		// (vigia.go) already ask. Reusing it avoids a second copy of the
		// same GET call diverging on the first change.
		ctxToken, cancelToken := context.WithTimeout(ctx, deadline)
		defer cancelToken()
		if err := client.CheckCredential(ctxToken, identifier, inst.SendToken); err != nil {
			return SmokeResult{}, fmt.Errorf("passo 2/4 (a Graph API aceita o token de envio): %w", err)
		}
		report("passo 2/4 OK: a Graph API aceitou o token de envio.")
	}

	// --- step 3: a message goes OUT, with an id ----------------------------------
	var resp meta.SendResponse
	if inst.Type == config.TypeInstagram {
		// 🔴 NO WINDOW PRE-CHECK: on Instagram you can only reply WITHIN the
		// 24h window (7 days with the human_agent tag) — there's no way to
		// "start a conversation" with a template, the way WhatsApp allows.
		// This slice does NOT try to guess whether the window is open before
		// sending: it tries the REAL send (the SAME production path,
		// SendInstagramMessage) and lets Meta decide — exactly the same
		// discipline as WhatsApp's step 3, just without the template
		// shortcut. If Meta refuses because the window is closed, the error
		// below says so out loud alongside what Meta answered, because here
		// a dead end is worse than on WhatsApp — there's no "just send a
		// template" to try next.
		ctxSend, cancelSend := context.WithTimeout(ctx, deadline)
		defer cancelSend()
		resp, err = client.SendInstagramMessage(ctxSend, baseInstagram, inst.IgID, inst.SendToken, destination, SmokeText)
		if err != nil {
			counter.Record(inst.Slug, config.CounterSendFailures)
			return SmokeResult{}, fmt.Errorf("passo 3/4 (enviar a mensagem de teste ao IGSID %s — "+
				"no Instagram isto SO funciona se ele tiver mandado mensagem para esta conta nas ultimas 24h "+
				"(7 dias com a tag human_agent); se a causa for essa, a Meta recusa e o erro abaixo vem dela, "+
				"nao de credencial): %w", destination, err)
		}
	} else {
		// Builds the request through the SAME path as the production send
		// (Validate + MetaBody). A separate path here would prove the
		// wrong path: the smoke test would pass and POST /v1/messages would
		// stay broken.
		p := Request{Instance: inst.Slug, To: destination, Type: "texto", Text: SmokeText}
		if err := p.Validate(); err != nil {
			// NO counter here, and that's what the production send also
			// does: a validation refusal happens BEFORE any byte goes out to
			// Meta, so there was no send and no SEND failure to count.
			return SmokeResult{}, fmt.Errorf("passo 3/4 (mensagem de teste): %w", err)
		}

		ctxSend, cancelSend := context.WithTimeout(ctx, deadline)
		defer cancelSend()
		resp, err = client.SendMessage(ctxSend, inst.PhoneNumberID, inst.SendToken, MetaBody(p))
		if err != nil {
			counter.Record(inst.Slug, config.CounterSendFailures)
			return SmokeResult{}, fmt.Errorf("passo 3/4 (enviar a mensagem de teste): %w", err)
		}
	}
	// SendMessage already refuses an empty id (meta.ErrResponseWithoutID).
	// The check here is cheap and covers the day that guarantee moves
	// somewhere else: activating over an empty id would activate a channel
	// that never proved it delivers.
	if strings.TrimSpace(resp.ID) == "" {
		counter.Record(inst.Slug, config.CounterSendFailures)
		return SmokeResult{}, fmt.Errorf("passo 3/4: %w", meta.ErrResponseWithoutID)
	}
	// BEFORE STEP 4, not after: `enviadas` is a fact of step 3. If the
	// ActivateInstance below fails, the message went OUT all the same and
	// the counter has to say 1 — hanging the count on step 4's success would
	// reopen the bug T-054 closed, just on a worse day.
	counter.Record(inst.Slug, config.CounterSent)
	report(fmt.Sprintf("passo 3/4 OK: mensagem enviada para %s (wa_message_id %s).", destination, resp.ID))

	// --- step 4: ONLY THEN, activate -------------------------------------------
	if err := store.ActivateInstance(inst.Slug); err != nil {
		return SmokeResult{}, fmt.Errorf("passo 4/4 (ativar a instancia): %w: %w", ErrSmokeActivationFailed, err)
	}
	inst.Active = true
	report(fmt.Sprintf("passo 4/4 OK: instancia %q ATIVADA.", inst.Slug))

	return SmokeResult{Instance: inst, WaMessageID: resp.ID}, nil
}
