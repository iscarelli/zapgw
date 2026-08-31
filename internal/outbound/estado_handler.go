// GET /v1/estado?instancia={slug} — the numbers the gateway promises, for
// whoever already has a dashboard (T-060).
//
// WHY IT EXISTS, and it isn't visualization: until now these numbers only
// existed for whoever went into the CT and ran `zapgw estado`. At
// `consumer-b`'s cutover (2026-07-28) the ONLY observation available was the
// operator checking a counter by hand over SSH. The real gain is ALARM: the
// consumers already have a notification system, and with readable counters
// `alarme_perda_definitiva > 0` becomes an automatic alert in a system that
// already knows how to alert — instead of a number that only appears if
// someone goes and looks. That's the difference between observability and
// discipline.
//
// THE DECISION NOT TO HAVE A DASHBOARD STILL HOLDS: this doesn't reverse it,
// it completes it. The gateway publishes the number; whoever already has the
// web is the one who designs it.
//
// IT DOES NOT BUILD THE STATE, it only publishes it: outbound.BuildState
// (estado.go) builds it, the SAME function `zapgw estado` uses. The split is
// from T-065: until then, the state was built right here and the CLI command
// showed only the counter table — four blocks the consumer saw and the
// operator didn't. This file handles what belongs ONLY to the route:
// authentication, the consumer->instance link, the HTTP statuses and the
// JSON.
//
// FOUR THINGS THIS ROUTE DOES NOT DO, and each one is a decision, not an
// oversight:
//
//  1. It has NO key list of its own. The state iterates
//     config.KeysInDisplayOrder, the single source (T-039) — a new key
//     appears here without ANYONE touching this file. A second list was
//     exactly the defect T-039 paid to fix.
//  2. It does NOT talk to Meta. The token verdict comes from the watchdog's
//     cache (vigia.go), which measures at its own pace. A dashboard that
//     called Meta on every load would be stuck to its latency and its
//     uptime, and "Meta is down" would turn into "the gateway has a problem"
//     on the consumer's screen.
//  3. It is NOT Prometheus, and does not accept an arbitrary date filter.
//     The audience is the CONSUMER, not a collector: a scraping format
//     invites infrastructure this project doesn't have, and a surface that
//     grows without a consumer asking is a dead surface that reads as
//     current. The ONLY time parameter is `serie_dias` (T-081) — an integer
//     of days counted backward from today, with a ceiling. There is no
//     arbitrary range, no start date, and the difference isn't taste: a
//     single-size, bounded window has predictable cost and doesn't let
//     anyone request a slice the database doesn't have.
//  4. It does NOT talk to the external probe (T-121). The `alcance_externo`
//     block comes from outbound.ExternalProbe's cache (sonda_externa.go),
//     which measures at its own pace — for the SAME reason as item 2, just
//     with a FOURTH service in the middle (gateway, Meta, and now the
//     probe) instead of a third.
package outbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
)

type StateHandler struct {
	store    *config.Store
	auth     *Authenticator
	watchdog *Watchdog
	// renewer is the Instagram token renewer (T-098). CAN BE nil (a test
	// that doesn't use Instagram doesn't need to build it) — the conversion
	// to IGRenewalFailureReader, further below in state(), handles this
	// by hand to avoid the pitfall of a nil POINTER turning into a non-nil
	// INTERFACE (Go: a nil *InstagramRenewer, assigned directly to an
	// interface variable, makes `interface != nil` answer TRUE).
	renewer *InstagramRenewer
	// version is the binary's identity, injected via -ldflags in cmd/zapgw
	// (`var version`). It travels by PARAMETER, and isn't read here, because
	// the one that receives it from the linker is package main — and a value
	// read from a file would lie exactly when it matters (an old file next to
	// a new binary, T-025's defect).
	version string
	// retentionDays is the CEILING of the `serie_dias` window, and it is the
	// SAME number the counter purge uses (T-081).
	//
	// IT TRAVELS BY PARAMETER, resolved from the environment a single time in
	// cmd/zapgw/main.go: no package under internal/ reads an environment
	// variable, and — more importantly — resolving the deadline here again
	// would create a SECOND number for the same fact. The two would diverge
	// on the day someone changed the `env`, and the divergence would show up
	// as this route accepting 30 days over a database that only keeps 15,
	// returning 15 days of zero wearing a measurement's face.
	retentionDays int
	// lideranca is the send singleton guard, so the state can PUBLISH
	// whether it exists and who holds it. Nil-safe: nil publishes "unarmed".
	leadership *Leadership
	// entrada is WHERE this gateway's ingress is published (T-120). It comes
	// resolved from the environment a single time in `main`, for the SAME
	// reason as retentionDays above: no package under internal/ reads an
	// environment variable on its own, and two resolutions of the same fact
	// would diverge.
	//
	// THE ZERO VALUE IS HONEST (`via: desconhecido`, `conector:
	// nao_configurado`), so a caller that forgets it publishes "we don't
	// know" — never a wrong assertion about the ingress path.
	ingress IngressSource
	// reach is the READER of the external probe's verdict (T-121) — the
	// SAME discipline as `entrada` above: resolved from the environment a
	// single time in `main`, and nil-safe (ExternalProbe.Read handles a nil
	// receiver), so a caller that doesn't build it publishes
	// `nao_configurado`, never a made-up measurement.
	reach *ExternalProbe
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go. It GATES NOTHING here:
	// BuildStateWithSeries (estado.go) already handles both types internally
	// since T-097/T-107 — the field exists only so this handler's
	// construction is forced to declare it, like every other one.
	types AcceptedTypes
}

// NewStateHandler builds the route. `types` is AllTypes: GET
// /v1/estado already publishes both types, handling the difference
// INTERNALLY (State.Type, NotApplicable in the blocks that don't apply) —
// see the comment on the `types` field, above.
func NewStateHandler(store *config.Store, auth *Authenticator, watchdog *Watchdog, renewer *InstagramRenewer, ingress IngressSource, reach *ExternalProbe, leadership *Leadership, version string, retentionDays int, types AcceptedTypes) http.Handler {
	h := &StateHandler{
		store: store, auth: auth, watchdog: watchdog, renewer: renewer, ingress: ingress, reach: reach, leadership: leadership, version: version,
		retentionDays: retentionDays, types: types,
	}
	if h.retentionDays < 1 {
		// A caller that didn't resolve the deadline (or resolved it to zero)
		// gets the default instead of a zero-day ceiling, which would refuse
		// EVERY state read. Falling back to the default is the safe side of
		// the error: at most it accepts a window larger than the
		// installation keeps, and the response's own `carimbos_desde` still
		// disambiguates that.
		h.retentionDays = config.DefaultRetentionDays
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/estado", h.state)
	return mux
}

// seriesWindow reads `?serie_dias=` and decides how many days the series
// will have.
//
// ABSENT = ShortSeriesDays, and not "the maximum window": the consumer who
// asked for nothing is the one that already existed before T-081, and
// growing their response thirteenfold without them asking would be a
// contract change disguised as a default.
//
// 🔴 A WINDOW LARGER THAN THE RETENTION IS AN ERROR, NEVER A SILENTLY
// SHORTENED SERIES. This is this task's whole point, and the rule already
// belongs to this house: a truncated template catalog is also an error and
// never a `200`. Returning 120 entries when the database keeps 90 doesn't
// deliver 120 days — it delivers 30 days of zero indistinguishable from "no
// traffic", exactly in the part of the chart nobody checks. The message
// states the ceiling IN EFFECT on this installation (not the compiled
// default), so whoever is on the other side can fix the request without
// opening a ticket.
func seriesWindow(raw string, retentionDays int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return config.ShortSeriesDays, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 {
		return 0, fmt.Errorf("`serie_dias` = %q: tem de ser um inteiro de 1 a %d",
			raw, retentionDays)
	}
	if days > retentionDays {
		return 0, fmt.Errorf(
			"`serie_dias` = %d, mas este gateway guarda contador por %d dias — a serie mais longa possivel tem %d entradas, e as mais velhas de uma janela maior sairiam zeradas sem terem sido medidas",
			days, retentionDays, retentionDays)
	}
	return days, nil
}

func (h *StateHandler) state(w http.ResponseWriter, r *http.Request) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return
		}
		log.Printf("zapgw: erro de store ao autenticar em GET /v1/estado: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}

	slug := strings.TrimSpace(r.URL.Query().Get("instancia"))
	if slug == "" {
		// 400, and not the 403 CanUse("") would hand out for free: "you
		// cannot see this instance" would send the consumer to check their
		// link, which is fine — the defect would stay hidden in the wrong
		// place.
		respondError(w, http.StatusBadRequest, "permanente",
			"parametro de consulta `instancia` e obrigatorio", 0)
		return
	}
	// THE SAME guard as POST /v1/messages, and not a new model: the
	// consumer->instance link decides, and asking for someone else's
	// instance gets the SAME 403 as sending. It comes BEFORE any database
	// read — without this, a token leaked from system A would find out from
	// the response status which slugs exist, and would read business B's
	// traffic volume.
	if !CanUse(consumer, slug) {
		log.Printf("zapgw: consumidor %q pediu o estado da instancia %q, que nao e dele",
			consumer.Name, slug)
		respondError(w, http.StatusForbidden, "config", "instancia nao autorizada para este consumidor", 0)
		return
	}

	// The series window is checked AFTER the link guard, on purpose: the
	// refusal message cites this installation's retention deadline, and
	// retention deadline is an operational detail. Whoever can't even read
	// the instance learns nothing about it from a parameter error.
	days, err := seriesWindow(r.URL.Query().Get("serie_dias"), h.retentionDays)
	if err != nil {
		respondError(w, http.StatusBadRequest, "permanente", err.Error(), 0)
		return
	}

	// One call, and the body is the entire State serialized — no field
	// list in here. A new field in State (estado.go) comes out in this
	// response AND on the `zapgw estado` screen without anyone editing
	// either surface, which is T-065's guarantee.
	// h.renewer is *InstagramRenewer (a CONCRETE pointer); it only becomes
	// an interface here, and ONLY when it isn't nil — passing the nil
	// pointer straight through would create a NON-nil interface internally
	// (Go's "typed nil"), and BuildStateWithSeries would call FailingSince
	// on a nil receiver.
	var renewer IGRenewalFailureReader
	if h.renewer != nil {
		renewer = h.renewer
	}
	e, err := BuildStateWithSeries(h.store, h.watchdog, renewer, h.ingress, h.reach, h.leadership, h.version, slug, time.Now(), days)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return
		}
		// 503, and not 200 with the state half-built: a read failure returned
		// as a zeroed state would be a lie wearing a fact's face, and it
		// would land exactly on the value the consumer uses to NOT alarm.
		log.Printf("zapgw: erro ao montar o estado em GET /v1/estado: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(e)
}
