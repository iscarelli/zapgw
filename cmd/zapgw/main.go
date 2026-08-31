// zapgw command: HTTP gateway for the WhatsApp Cloud API.
//
// This file only assembles: reads the environment, opens the store,
// registers routes, starts the server. No business rule lives here.
//
// WITH a command-line argument the binary is a provisioning tool (see
// provisionar.go and fumaca.go); WITH NO argument at all it starts the
// server, which is how systemd runs it — the ONLY exception is a person
// at a terminal, who gets the menu instead (menu.go, T-082). Scripts and
// systemd have no terminal, so nothing changes for them: see
// `shouldOpenMenu`.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/inbound"
	"github.com/iscarelli/zapgw/internal/meta"
	"github.com/iscarelli/zapgw/internal/outbound"
)

// versao is the binary's identity. IT IS NEVER READ FROM DISK AT RUNTIME
// — the VERSION file does not go to the CT, and a binary that read the
// version from a file would lie exactly when it matters (an old file next
// to a new binary — that is what happened on 2026-07-25 and opened
// T-025). Injection is via `-ldflags "-X main.version=…"` at build time
// (implanta/deploy.sh, sourced from the VERSION file).
//
// Without injection (a plain `go build ./...`, the way a dev runs it
// locally) the value stays "desenvolvimento" — NEVER a plausible number
// like "0.0.0": a made-up number is more dangerous than the declared
// unknown, because it is believable.
var version = "desenvolvimento"

// healthResponse is the body of `GET /v1/health`.
//
// `ok` is this gateway's public guarantee: it never stops existing and
// never stops being `true` when this handler responds — this endpoint's
// format only GROWS, so a consumer that only reads `ok` cannot break when
// a new field (like `versao`, T-025) is added.
type healthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"versao"`
}

func routes(inboundHandler, outboundHandler, healthHandler, templatesHandler, mediaHandler, stateHandler, readsHandler, enrollmentHandler, smokeHandler, pauseHandler, blockingHandler, profileHandler http.Handler) http.Handler {
	mux := http.NewServeMux()

	// NOT INFORMATIVE ABOUT THE CHANNEL, on purpose: this `200` says the
	// PROCESS is up and WHICH BINARY it is — and nothing more. It comes
	// out the same with every token revoked. Each channel's health (the
	// token Meta still accepts) is /v1/instances/{slug}/health, which
	// asks Meta and therefore costs a network call.
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{OK: true, Version: version})
	})

	if inboundHandler != nil {
		mux.Handle("/v1/inbound/", inboundHandler)
	}
	if outboundHandler != nil {
		mux.Handle("/v1/messages", outboundHandler)
	}
	if healthHandler != nil {
		// A LAN route, like /v1/messages: only /v1/inbound is published
		// to the internet. It consumes the instance's credential and
		// spends a Graph API call — publishing this would give anyone on
		// the internet a cheap way to burn someone else's quota.
		mux.Handle("/v1/instances/", healthHandler)
	}
	if templatesHandler != nil {
		// A LAN route, for the same reasons: the catalog describes the
		// tenant's business and every query spends a call on their Graph
		// API.
		mux.Handle("/v1/templates", templatesHandler)
	}
	if mediaHandler != nil {
		// A LAN route, like the others: whoever uploads media spends the
		// instance's quota and whoever downloads receives customer
		// CONTENT. Both patterns are necessary — `/v1/media` matches the
		// upload, `/v1/media/` matches the download by id.
		mux.Handle("/v1/media", mediaHandler)
		mux.Handle("/v1/media/", mediaHandler)
	}
	if stateHandler != nil {
		// A LAN route, like the others — and here the reason is
		// different: it spends no quota at all (it answers from the
		// database and the watcher's cache), but it DESCRIBES the
		// tenant's business TRAFFIC. Message volume, when the last one
		// arrived, and whether their credential is rejected are not
		// things for the internet. It is born internal by construction:
		// the :8443 router matches by EXCLUDING /v1/inbound
		// (docs/IMPLANTACAO.md).
		mux.Handle("/v1/estado", stateHandler)
	}
	if readsHandler != nil {
		// A LAN route, like /v1/messages and for the SAME reasons: it
		// consumes the instance's credential and spends a call on the
		// tenant's Graph API. Publishing this to the internet would give
		// anyone a cheap way to burn someone else's quota — and the
		// body's `wamid` carries the customer's phone number, which is
		// not something to send over the public port.
		mux.Handle("/v1/leituras", readsHandler)
	}
	if enrollmentHandler != nil {
		// A LAN route, like the others, and here the reason is the
		// strongest of all: it carries the consumer's `app_secret` and
		// `token_envio` through, in plain text in the body. It is a WRITE
		// — the consumer registering their Meta credentials (T-079,
		// docs/MODELO-DE-USO.md) — and returns no configuration at all.
		//
		// ⚠️ And it is the most expensive example of the question still
		// open in the model: a real third party needs to REACH this
		// route, and today :8443 matches by EXCLUDING /v1/inbound
		// (docs/IMPLANTACAO.md), meaning only the LAN reaches here. Until
		// the owner decides this, whoever registers is whoever has access
		// to the gateway's network.
		mux.Handle("/v1/cadastro", enrollmentHandler)
	}
	if smokeHandler != nil {
		// A LAN route, like the others — and here the reason is the same
		// as /v1/cadastro: it is the ONLY route in the gateway that SENDS
		// without the consumer having requested a send (T-084,
		// docs/MODELO-DE-USO.md, item 7). While only the LAN reaches
		// here, it is the owner who registers the consumer who will call
		// it — the same reach gap /v1/cadastro already had, until T-053.
		mux.Handle("/v1/fumaca", smokeHandler)
	}
	if pauseHandler != nil {
		// A LAN route, for the same reasons as the other instance
		// routes: it changes a tenant's channel active/paused state.
		mux.Handle("/v1/pausa", pauseHandler)
	}
	if blockingHandler != nil {
		// A LAN route, like the other instance routes (T-148): it
		// consumes the instance's credential and spends a call on the
		// tenant's Graph API, and the body carries a customer's phone
		// number — not something to send over the public port.
		mux.Handle("/v1/bloqueios", blockingHandler)
	}
	if profileHandler != nil {
		// A LAN route, like the other instance routes (T-155): it
		// consumes the instance's credential and spends a call on the
		// tenant's Graph API, and DESCRIBES their BUSINESS (address,
		// email, website) — not something to send over the public port.
		mux.Handle("/v1/perfil", profileHandler)
	}
	return mux
}

// openStore opens the database from the environment. It is the ONLY
// place that decides the default path and that requires the encryption
// key — the server and the subcommands all go through here, otherwise one
// of them would end up opening a different file, or opening without
// encryption.
//
// The key lives OUTSIDE the database. Without it nothing starts up:
// refusing loud and early is what prevents running "working" with no
// encryption at all.
func openStore(env environment) (*config.Store, error) {
	vault, err := config.NewVault(env("ZAPGW_CHAVE_CIFRA"))
	if err != nil {
		return nil, fmt.Errorf("zapgw: ZAPGW_CHAVE_CIFRA: %w", err)
	}
	path := env("ZAPGW_BANCO")
	if path == "" {
		path = "zapgw.db"
	}
	store, err := config.OpenStore(path, vault)
	if err != nil {
		return nil, fmt.Errorf("zapgw: abrir banco: %w", err)
	}
	return store, nil
}

// graphBase is the Graph API root, configurable so it can be pointed at
// a test server. The default is Meta's production.
func graphBase(env environment) string {
	if base := env("ZAPGW_GRAPH_BASE"); base != "" {
		return base
	}
	// VERIFIED on 2026-07-24 at
	// developers.facebook.com/docs/graph-api/guides/versioning, which
	// states in plain text that the Graph API's most recent version is
	// v25.0.
	//
	// NOT VERIFIED, and therefore not claimed here: whether v21.0 (the
	// one the plan came with) has already expired. That page only says
	// each version is maintained for AT LEAST two years after release,
	// and does not list which ones have expired.
	// /docs/graph-api/changelog, which would have that list, returned 404
	// on the same date.
	//
	// Confirm again before deploying to production if this commit is old.
	return "https://graph.facebook.com/v25.0"
}

// instagramRenewalBase is the root of the Instagram token renewal
// endpoint (T-098) — a DIFFERENT HOST from graphBase
// (graph.instagram.com, with no version prefix; see the comment on
// meta.DefaultInstagramRenewalBase). Configurable for the SAME reason as
// graphBase: pointing at a test server, never at the real Meta during
// the suite.
func instagramRenewalBase(env environment) string {
	if base := env("ZAPGW_INSTAGRAM_REFRESH_BASE"); base != "" {
		return base
	}
	return meta.DefaultInstagramRenewalBase
}

// versionCommand prints the binary's version and exits 0, without opening
// the database or touching the network. It is what whoever is inside the
// CT uses to answer "which binary is really running" without depending on
// HTTP or checking a sha256 by hand (T-025).
func versionCommand(out io.Writer) error {
	fmt.Fprintln(out, version)
	return nil
}

// startPeriodicPurge starts a goroutine that calls `purgar` on
// startup and then every `period`, forever — the SAME template for
// every TTL purge in this binary (T-035 asked to reuse the idempotency
// one, and a single function keeps the two from diverging on the next
// change).
//
// A purge failure does NOT bring the service down: it only makes the
// table grow, and growing slowly is better than refusing traffic. That
// holds for a RETURNED ERROR — for a PANIC it only holds because of the
// recover() below: without it, a panic inside the loop kills the
// goroutine and, with no one left watching that purge, the mechanism
// silently stops running for the rest of the process's life.
func startPeriodicPurge(name string, period time.Duration, purge func() (int, error)) {
	go func() {
		for {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("zapgw: purga de %s sofreu panico (recuperado): %v", name, rec)
					}
				}()
				if n, err := purge(); err != nil {
					log.Printf("zapgw: purga de %s falhou: %v", name, err)
				} else if n > 0 {
					log.Printf("zapgw: purga de %s removeu %d registro(s)", name, n)
				}
			}()
			time.Sleep(period)
		}
	}()
}

func main() {
	// WITH an argument, the binary is a command-line tool; WITH NO
	// argument at all, it starts the server — which is how it runs under
	// systemd.
	if len(os.Args) > 1 {
		if err := dispatch(os.Args[1:], os.Stdout, os.Getenv); err != nil {
			// A status != 0 is what a deploy script reads. Without it, a
			// failed provisioning would look like a success.
			log.Fatalf("%v", err)
		}
		return
	}

	// THE ONLY EXCEPTION to "no argument starts the server": someone is
	// at the terminal. The whole condition (and why it looks at BOTH
	// sides) is in `shouldOpenMenu` — with no TTY the menu does NOT open,
	// otherwise systemd and implanta/deploy.sh would sit waiting for a
	// choice no one is going to type.
	if shouldOpenMenu(os.Args[1:], os.Stdin, os.Stdout) {
		if err := menu(os.Stdin, os.Stdout, os.Getenv); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	// WHICH PATH INBOUND IS PUBLISHED THROUGH (T-120), resolved BEFORE
	// opening the database and BEFORE listening on any port: an unknown
	// value in ZAPGW_ENTRADA_VIA brings the startup down, and the cheap
	// place to discover that is in front of whoever just edited
	// /etc/zapgw/env — not three weeks later, in a contract field no one
	// checked. Empty is NOT an error: it publishes `desconhecido` (see
	// outbound.IngressVia for the two answers and why they differ).
	via, err := outbound.IngressVia(os.Getenv)
	if err != nil {
		log.Fatalf("%v", err)
	}

	// SENDING SINGLETON GUARD — resolved BEFORE opening the database and
	// BEFORE listening on any port, for the same reason as
	// ZAPGW_ENTRADA_VIA above: an unreadable value brings the startup
	// down in front of whoever just edited /etc/zapgw/env, and not three
	// weeks later, on failover day.
	//
	// It wraps ONLY /v1/messages. Receiving, health, state and
	// registration keep responding on a non-leader by design: what
	// cannot happen in duplicate is ACTING outward (talking to Meta) —
	// see the header of internal/outbound/lideranca.go.
	leadership, err := outbound.NewLeadership(os.Getenv)
	if err != nil {
		log.Fatalf("%v", err)
	}
	// Saying out loud which of the two modes is in effect. A disarmed
	// guard is correct on a single node, but "disarmed because someone
	// forgot the variable on the pair" is the defect that sends a
	// duplicate message — and both are invisible if no one prints them.
	if leadership.Armed() {
		log.Printf("zapgw: guarda de lideranca ARMADA (%s)", os.Getenv(outbound.VarLeadershipFile))
	} else {
		log.Printf("zapgw: guarda de lideranca DESARMADA — no unico; defina %s para armar", outbound.VarLeadershipFile)
	}

	store, err := openStore(os.Getenv)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer store.Close()

	// Idempotency TTL. It is what keeps the exception ("we kept
	// (consumer, key) -> id") from turning into history. Short on
	// purpose: a DELIVERY record, not a message record.
	ttl := 72 * time.Hour
	if v := os.Getenv("ZAPGW_TTL_IDEMPOTENCIA_HORAS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ttl = time.Duration(n) * time.Hour
		}
	}
	startPeriodicPurge("idempotencia", time.Hour, func() (int, error) {
		return store.PurgeIdempotency(time.Now().Add(-ttl))
	})

	// Instance counters TTL (T-035). 90 days by default: without a
	// purge the table grows forever in a binary meant to run for years —
	// the same reasoning as idempotency above, except here the record is
	// not a secret at all (just slug, day, key and n), so the deadline
	// can be much longer.
	//
	// RESOLVED ONCE, HERE, and passed down by parameter to the TWO
	// things that depend on it (T-081): the purge below is the ceiling of
	// the `serie_dias` window in GET /v1/estado. The environment is only
	// read in `main` — no package under internal/ reads an environment
	// variable —, and two resolutions of the same deadline would diverge
	// on the day someone changed the `env`, with the route accepting a
	// 30-day series over a database that keeps 15.
	counterDays := config.CounterRetentionDays(os.Getenv)
	counterTTL := time.Duration(counterDays) * 24 * time.Hour
	startPeriodicPurge("contadores", time.Hour, func() (int, error) {
		return store.PurgeCounters(time.Now().Add(-counterTTL))
	})

	counter := config.NewCounter(store)

	// TRANSIT log TTL (T-091). 30 days by default: the same discipline
	// as the counters retention above, but deliberately shorter — the log
	// keeps an HMAC of phone number and of wamid, and the older it gets
	// the wider the window in which an HMAC could someday be correlated
	// with the number through some other external path (a network
	// capture, an address-book leak).
	transitDays := config.TransitRetentionDays(os.Getenv)
	transitTTL := time.Duration(transitDays) * 24 * time.Hour
	startPeriodicPurge("transito", time.Hour, func() (int, error) {
		return store.PurgeTransit(time.Now().Add(-transitTTL))
	})
	transit := config.NewTransit(store)

	maxBytes := 1 << 20
	if v := os.Getenv("ZAPGW_MAX_CORPO_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxBytes = n
		}
	}

	address := os.Getenv("ZAPGW_ENDERECO")
	if address == "" {
		address = "127.0.0.1:8080"
	}

	// The certificate observer (T-064) is WRITTEN on delivery and READ
	// in GET /v1/estado — both sides through the same store, which is
	// what makes the observation survive a restart. It only renews when
	// there is a delivery (there is no probe), so losing it on every
	// deploy would leave a low-traffic instance saying "never observed"
	// for days.
	h := inbound.NewHandler(store, inbound.NewDeliverer(config.NewCertificateObserver(store)), maxBytes, counter, transit)

	authenticator := outbound.NewAuthenticator(store)
	metaClient := meta.NewClient(nil, graphBase(os.Getenv))

	// T-104: Instagram sending talks to graph.instagram.com, a
	// DIFFERENT host from the rest of the Graph API (metaClient.base).
	// instagramRenewalBase is the SAME resolution that used to feed
	// only the token renewer (below) — reused here instead of a second
	// environment read, which would diverge from the first the day
	// someone changed only one of the two.
	//
	// T-111: AllTypes — sending already serves both WhatsApp and
	// Instagram, handling the difference INTERNALLY since T-097 (see
	// outbound.Handler.enviar).
	out := outbound.NewHandlerWithInstagramBase(store, authenticator, metaClient, maxBytes, counter, transit,
		instagramRenewalBase(os.Getenv), outbound.AllTypes)
	// T-111: AllTypes — health serves BOTH types and handles the
	// difference INTERNALLY (nao_se_aplica for the type with no
	// equivalent credential check, without calling Meta; see
	// outbound.HealthHandler.health).
	health := outbound.NewHealthHandler(store, authenticator, metaClient, outbound.AllTypes)
	// T-111: WhatsAppOnly — the catalog uses inst.WabaID, a field exclusive
	// to WhatsApp; Instagram has no template in this slice (the same
	// restriction as sending, which only accepts "texto" for Instagram).
	// The SAME counter as sending, with its OWN KEY (T-173): the deletion
	// counts into `templates_apagados` and nothing else. See
	// internal/config/contador.go.
	templates := outbound.NewTemplatesHandler(store, authenticator, metaClient, maxBytes, counter,
		outbound.WhatsAppOnly)
	// T-111: WhatsAppOnly — upload and download use inst.PhoneNumberID;
	// Instagram's first slice (T-097) does not send media.
	media := outbound.NewMediaHandler(store, authenticator, metaClient, counter, outbound.WhatsAppOnly)
	// The SAME counter as sending, with its OWN KEY (T-075): marking as
	// read never adds to `enviadas`. See internal/config/contador.go.
	// T-111: WhatsAppOnly — marking as read uses inst.PhoneNumberID.
	reads := outbound.NewReadsHandler(store, authenticator, metaClient, maxBytes, counter, outbound.WhatsAppOnly)
	// The cadastro route does NOT receive metaClient, and the absence
	// is the guarantee (T-079): this route records what the consumer
	// sent and asks Meta nothing at all. What proves the credential
	// works is `zapgw fumaca`, and that is why registering does not
	// activate.
	// T-111: WhatsAppOnly — this route records waba_id/phone_number_id/
	// numero_exibido; Instagram has no cadastro by API in this slice
	// (see "Desenho preservado" in docs/TASKS.md).
	enrollment := outbound.NewRegistrationHandler(store, authenticator, maxBytes, counter, outbound.WhatsAppOnly)
	// POST /v1/fumaca and POST /v1/pausa (T-084): step 4 of the model
	// (docs/MODELO-DE-USO.md, "O fluxo, e quem faz cada passo" — the
	// consumer "Prova o canal (`fumaca`)") becomes executable by a
	// third-party consumer, with no shell on the gateway machine. fumaca
	// calls the SAME outbound.SmokeWithInstagramBase function that
	// `cmd/zapgw fumaca` calls (fumaca.go); pausa calls the SAME
	// store.PauseInstance that `zapgw instancia pausar` already calls.
	// T-104: the SAME Instagram base as the sending route, above — the
	// smoke test also sends SendInstagramMessage for real (step 3).
	// T-111: AllTypes — the smoke test already knows how to activate
	// both types, handling the difference INTERNALLY (fumaca.go). pausa
	// is AllTypes because it reads no type-specific field at all: it
	// only toggles `ativo`.
	smokeRoute := outbound.NewSmokeHandlerWithInstagramBase(store, authenticator, metaClient, counter, maxBytes,
		instagramRenewalBase(os.Getenv), outbound.AllTypes)
	pauseRoute := outbound.NewPauseHandler(store, authenticator, maxBytes, counter, outbound.AllTypes)
	// POST/DELETE/GET /v1/bloqueios (T-148): the Cloud API's user-blocking
	// endpoint. T-111: WhatsAppOnly — blocking uses inst.PhoneNumberID, a
	// field exclusive to WhatsApp; there is no Instagram equivalent in
	// this slice.
	blockingRoute := outbound.NewBlockHandler(store, authenticator, metaClient, maxBytes, counter, outbound.WhatsAppOnly)
	// GET/POST /v1/perfil (T-155): the whatsapp_business_profile. T-111:
	// WhatsAppOnly — profileNode (internal/outbound/perfil_handler.go)
	// uses inst.PhoneNumberID, a field exclusive to WhatsApp; there is no
	// documented Instagram-equivalent endpoint in this slice.
	profileRoute := outbound.NewProfileHandler(store, authenticator, metaClient, maxBytes, counter, outbound.WhatsAppOnly)

	// The token watcher (T-060) is the PRIMARY SENSOR for the verdict
	// GET /v1/estado publishes: it runs per ACTIVE instance, independent
	// of traffic and of anyone watching. Without Start, the route would
	// keep responding — always `desconhecido` —, and "desconhecido"
	// forever is exactly the blind panel this task exists to end. See
	// vigia.go.
	watchdog := outbound.NewWatchdog(store, metaClient)
	watchdog.Start()

	// The Instagram token renewer (T-098) is the PAIR of the paragraph
	// above, except for Instagram's fixed 60-day token expiration instead
	// of WhatsApp's acceptance verdict: it runs per tipo=instagram
	// instance, independent of traffic and of anyone watching. Without
	// Start, the token would expire in SILENCE — exactly the defect
	// T-098 exists to close. See renovador_instagram.go.
	igRenewer := outbound.NewInstagramRenewer(store, metaClient, instagramRenewalBase(os.Getenv))
	igRenewer.Start()

	// The connector probe (T-120) is the THIRD sensor with this same
	// template — its own timer, measurement in memory, consumer read
	// served from what has already been measured. It asks the `/ready`
	// of the `cloudflared` that publishes this route. Without
	// ZAPGW_CONECTOR_READY it is born inert and the block comes out
	// `nao_configurado`, which is the honest answer for an installation
	// with no tunnel.
	//
	// ⚠️ IT DOES NOT MEASURE, AND CANNOT START MEASURING, WHETHER THE
	// GATEWAY IS REACHABLE FROM OUTSIDE — only the public probe answers
	// that, from outside. See entrada.go.
	connector := outbound.NewConnectorProbe(outbound.ConnectorAddress(os.Getenv))
	connector.Start()

	// The external probe (T-121) is the FOURTH sensor with this same
	// template: it asks the ZAPGW_SONDA_EXTERNA_URL URL (the public
	// verdict of the probe that measures inbound access FROM OUTSIDE the
	// network — sonda-worker/), on its own cadence, and publishes what it
	// has already measured in `alcance_externo`. Without the variable it
	// is born inert and the block comes out `nao_configurado` — the same
	// honest answer `conector` gives for an installation with no tunnel.
	externalProbe := outbound.NewExternalProbe(outbound.ExternalProbeURL(os.Getenv))
	externalProbe.Start()

	// T-111: AllTypes — GET /v1/estado already publishes both types,
	// handling the difference INTERNALLY (estado.go).
	state := outbound.NewStateHandler(store, authenticator, watchdog, igRenewer,
		outbound.IngressSource{Via: via, Connector: connector}, externalProbe,
		// The SAME `lideranca` that wraps /v1/messages, on purpose: if
		// estado read from a different instance, the panel could say
		// "leader" while the sending guard said the opposite — two
		// answers for the same fact, and the divergence would only show
		// up on failover day.
		leadership,
		version, counterDays, counter, outbound.AllTypes)

	log.Printf("zapgw ouvindo em %s", address)
	if err := http.ListenAndServe(address, routes(h, leadership.Require(out), health, templates, media, state, reads, enrollment, smokeRoute, pauseRoute, blockingRoute, profileRoute)); err != nil {
		log.Fatalf("zapgw: servidor caiu: %v", err)
	}
}
