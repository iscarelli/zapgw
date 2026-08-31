// `grafo-falso` — a FAKE Graph API, for the lab.
//
// WHY IT EXISTS (T-071): a LAB instance has no number at Meta, and the only path
// to `ativo = 1` is `zapgw fumaca`, which demands a successful send. Up to here,
// the whole lab (create -> activate -> exercise -> delete) had a hole in the
// middle, and docs/IMPLANTACAO.md prescribed closing that hole with
// `UPDATE instancia SET ativo = 1` typed by hand into the PRODUCTION database.
//
// THE TASK GAVE TWO WAYS OUT, and this is the one that was chosen: instead of
// opening a SECOND DOOR to `ativo = 1` (an `instancia ativar --sem-prova`), the
// lab points `fumaca` at ANOTHER ENDPOINT. The proof requirement stays whole —
// `fumaca` only activates after a send that returned an id — and the lab
// exercises the ENTIRE production path, which is what a lab is there to do. The
// full why is in cmd/zapgw/smoke.go, next to the guarantee it protects.
//
// THIS BINARY DOES NOT GO TO PRODUCTION, and not by convention:
// `implanta/deploy.sh` builds `./cmd/zapgw`, only it. Nothing here changes the
// gateway binary — there is no "test mode" flag anywhere in it.
//
// IT CANNOT PRODUCE A FALSE POSITIVE, and that is what makes it acceptable: what
// reads the answer is the PRODUCTION code (internal/meta), which demands a
// non-empty `messages[0].id`. If this server answers crooked, `fumaca` FAILS and
// the instance stays paused. What it proves is the gateway's plumbing (CLI,
// store, counter, activation); what it does NOT prove — a token accepted by
// Meta, a message arriving on a handset — remains provable only against the real
// Meta, with `fumaca` without ZAPGW_GRAPH_BASE.
//
// LOOPBACK ONLY, on purpose: a fake Graph API reachable from the LAN is a cheap
// way for someone to point a real gateway at it without noticing.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// labAddress is fixed at 127.0.0.1: only the port is choosable. See
// the header — the absence of an option to listen on 0.0.0.0 is the guarantee.
const labAddress = "127.0.0.1"

// bodyCap caps how much of the POST body is read. It only serves to find out
// whether that POST is a send or a read receipt (see postToMessages) — no byte
// beyond that is used.
const bodyCap = 1 << 20

// Modes of --falha-de-template. They are the THREE outcomes T-078 implemented in
// the gateway, and they exist here so that each one can be exercised with the
// real binary, in the lab, instead of only in a unit test.
const (
	// failTemplateNone: creation answers normally.
	failTemplateNone = ""
	// failTemplateCreated reproduces 2026-07-28 in full: the template IS CREATED
	// and the connection dies before the answer. The `GET` that follows shows the
	// template, and the gateway has to answer `201`.
	failTemplateCreated = "criado"
	// failTemplateNotCreated: the connection dies and the template does NOT come
	// to exist. The gateway has to answer INCONCLUSIVO — never "it was not
	// created".
	failTemplateNotCreated = "nao-criado"
	// failTemplateCatalogToo: the POST dies and the `GET` too. It is the only
	// outcome in which `502 desconhecido` is still right.
	failTemplateCatalogToo = "catalogo-tambem"
)

type fakeGraph struct {
	refuseToken bool
	refuseSend  bool
	// refuseNumberFields reproduces the only way for the T-080 read to break
	// with nothing wrong with the token: the Graph API answering 400 to a
	// `fields=` whose name it no longer knows. It is the outcome the gateway HAS
	// to survive without painting the token red — see Watchdog.checkOne.
	refuseNumberFields bool
	// templateFailure is one of the failTemplate* above.
	templateFailure string

	// catalog is the template state of the fake WABA. It exists because the
	// outcome this fake needs to reproduce is precisely "the POST did not answer
	// AND the template exists" — without state, the `GET` that follows would have
	// nothing to show and the lab would prove the wrong case.
	mu      sync.Mutex
	catalog []map[string]any

	sent atomic.Int64
	// reads counts read receipts SEPARATELY from sends, for the same reason the
	// gateway counts them under a key of their own (config.CounterReadsMarked,
	// T-075): marking as read is not a send, and adding the two together here
	// would make the lab agree with a gateway that added them up wrong.
	reads atomic.Int64
}

func main() {
	port := flag.Int("porta", 9090, "porta em 127.0.0.1 (o endereco NAO e escolhivel: so loopback)")
	refuseToken := flag.Bool("recusar-token", false, "responder 401 no GET /{phone_number_id} — o passo 2 do fumaca aborta e NENHUMA mensagem e tentada")
	refuseSend := flag.Bool("recusar-envio", false, "responder 400 no POST /{phone_number_id}/messages — o fumaca falha e a instancia CONTINUA PAUSADA")
	refuseNumberFields := flag.Bool("recusar-campos-do-numero", false,
		"responder 400/code 100 ao GET /{phone_number_id}?fields=... (campo que a Graph nao conhece), "+
			"mantendo o GET limpo em 200: o veredito do token tem de continuar `ok` e so a qualidade/limite "+
			"do numero param de atualizar")
	templateFailure := flag.String("falha-de-template", failTemplateNone,
		"derruba a conexao do POST /{waba_id}/message_templates SEM resposta, para exercitar os tres desfechos "+
			"da T-078: `criado` (o template existe e o gateway deve responder 201), `nao-criado` (o gateway deve "+
			"responder INCONCLUSIVO) ou `catalogo-tambem` (o GET tambem cai e o 502 desconhecido continua certo)")
	flag.Parse()

	switch *templateFailure {
	case failTemplateNone, failTemplateCreated, failTemplateNotCreated, failTemplateCatalogToo:
	default:
		// Refuse here, rather than ignore: a mistyped value that got ignored would
		// make the lab run the HAPPY path while whoever is operating expects the
		// failure path — and the resulting "it passed" would be a lie.
		log.Printf("grafo-falso: --falha-de-template=%q nao existe (conheco: %q, %q, %q, %q)",
			*templateFailure, failTemplateNone, failTemplateCreated,
			failTemplateNotCreated, failTemplateCatalogToo)
		os.Exit(2)
	}

	g := &fakeGraph{
		refuseToken: *refuseToken, refuseSend: *refuseSend,
		refuseNumberFields: *refuseNumberFields,
		templateFailure:    *templateFailure,
	}
	address := fmt.Sprintf("%s:%d", labAddress, *port)

	// SAY OUT LOUD WHAT THIS IS. Whoever stumbles on this process on a machine has
	// to find out in one line that it is not Meta — a silent server on 9090 is
	// exactly the kind of thing someone assumes is production.
	log.SetFlags(log.Ltime)
	log.Printf("grafo-falso: Graph API DE MENTIRA em http://%s — nao e a Meta, nao entrega mensagem a ninguem", address)
	log.Printf("grafo-falso: aponte o laboratorio com  ZAPGW_GRAPH_BASE=http://%s", address)
	if g.refuseToken {
		log.Printf("grafo-falso: --recusar-token LIGADO: o GET responde 401 (token revogado)")
	}
	if g.refuseSend {
		log.Printf("grafo-falso: --recusar-envio LIGADO: o POST responde 400 (envio recusado)")
	}
	if g.refuseNumberFields {
		log.Printf("grafo-falso: --recusar-campos-do-numero LIGADO: o GET com `fields=` responde 400; " +
			"o GET limpo continua 200")
	}
	if g.templateFailure != failTemplateNone {
		log.Printf("grafo-falso: --falha-de-template=%s LIGADO: o POST de template MORRE sem resposta "+
			"(falha de transporte, como em 2026-07-28)", g.templateFailure)
	}

	if err := http.ListenAndServe(address, g.routes()); err != nil {
		log.Printf("grafo-falso: servidor caiu: %v", err)
		os.Exit(1)
	}
}

// routes serves the calls the lab needs to make, and nothing more: the two of the
// smoke test (check the credential and send / mark as read) and, since T-078, the
// two of the TEMPLATE CATALOG — create and list.
//
// An unknown path answers 404 with Meta's error shape, instead of a generous
// 200: a fake that accepts everything hides the call the gateway makes to the
// wrong place, which is precisely the defect that only shows up against the real
// Meta (where it costs dearly).
func (g *fakeGraph) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("grafo-falso: %s %s", r.Method, r.URL.Path)

		// The token goes in the HEADER. Without this refusal, a day on which the
		// gateway stopped sending Authorization would go unnoticed in the lab and
		// would only show up against Meta.
		if r.Header.Get("Authorization") == "" {
			g.writeError(w, http.StatusUnauthorized, 190, "grafo-falso: requisicao sem Authorization")
			return
		}

		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") {
			g.postToMessages(w, r)
			return
		}
		// BEFORE the generic GET below: without this guard, a `GET
		// /{waba}/message_templates` would fall into checkCredential and return
		// `{"id": ...}` — a body without `data`, which the gateway would refuse as
		// a catalog it did not understand. The lab would prove the wrong error.
		if strings.HasSuffix(r.URL.Path, "/message_templates") {
			switch r.Method {
			case http.MethodPost:
				g.createTemplate(w, r)
				return
			case http.MethodGet:
				g.listTemplates(w)
				return
			}
		}
		if r.Method == http.MethodGet {
			g.checkCredential(w, r)
			return
		}
		g.writeError(w, http.StatusNotFound, 100, "grafo-falso: rota que a Graph API nao tem: "+r.Method+" "+r.URL.Path)
	})
	return mux
}

func (g *fakeGraph) checkCredential(w http.ResponseWriter, r *http.Request) {
	if g.refuseToken {
		g.writeError(w, http.StatusUnauthorized, 190, "Invalid OAuth access token")
		return
	}

	// THE GRAPH API TREATS `fields=` AS ANOTHER QUESTION, and so does the fake:
	// with `fields=` it returns ONLY what was asked for (T-080). A fake that
	// ignored the parameter and always returned the same body would hide the day
	// the gateway asked for the wrong field — and that defect would only show up
	// against the real Meta, where it costs dearly.
	fields := r.URL.Query().Get("fields")
	if fields == "" {
		// The body does NOT matter to the gateway: meta.CheckCredential reads
		// nothing from the success response, only the status. It comes out looking
		// like Meta's for whoever is watching with curl, and nothing else depends
		// on that.
		respond(w, http.StatusOK, map[string]any{
			"id":                   strings.TrimPrefix(r.URL.Path, "/"),
			"display_phone_number": "+55 00 00000-0000",
		})
		return
	}

	if g.refuseNumberFields {
		// META'S EXACT SHAPE OF REFUSAL for a field it does not know: 400 with
		// `code` 100. It is the 400 (not the 401) that makes this case dangerous —
		// in the gateway's taxonomy it is class PERMANENTE, indistinguishable from
		// "Meta refused for good", and that is why the watcher reconfirms with the
		// clean GET before declaring `recusado`.
		g.writeError(w, http.StatusBadRequest, 100,
			"grafo-falso: (#100) Tried accessing nonexisting field on node type WhatsAppBusinessPhoneNumber")
		return
	}

	// ONLY THE FIELDS ASKED FOR, one by one. Always returning both would make a
	// gateway that asked for just one of them look right.
	body := map[string]any{"id": strings.TrimPrefix(r.URL.Path, "/")}
	for _, field := range strings.Split(fields, ",") {
		switch strings.TrimSpace(field) {
		case "quality_rating":
			body["quality_rating"] = "GREEN"
		case "whatsapp_business_manager_messaging_limit":
			// LITERAL, as Meta sends it. The lab exists to exercise the production
			// path, and production NEVER converts this to 250.
			body["whatsapp_business_manager_messaging_limit"] = "TIER_250"
		}
	}
	respond(w, http.StatusOK, body)
}

// postToMessages separates the TWO calls that share this path on the real Graph
// API: the SEND and the READ RECEIPT (T-075). What tells them apart is the BODY
// (`"status": "read"`), not the verb nor the path — both are
// `POST /{phone_number_id}/messages`, checked against Meta's docs on 2026-07-28
// (the URLs are in internal/meta/read.go).
//
// A fake that answered "send" to both would return `messages[].id` to a read
// receipt — data Meta NEVER sends there — and the lab would start hiding exactly
// the defect it exists to expose.
func (g *fakeGraph) postToMessages(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyCap))
	if err != nil {
		g.writeError(w, http.StatusBadRequest, 100, "grafo-falso: corpo do POST ilegivel")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	// A body unreadable as JSON falls into the SEND, which is the one with the
	// most demanding validation on the gateway side (it requires `messages[0].id`
	// back).
	_ = json.Unmarshal(raw, &body)
	if body.Status == "read" {
		g.markAsRead(w)
		return
	}
	g.send(w)
}

// markAsRead answers what Meta's docs show, and nothing beyond: `{"success":
// true}`. No `messages`, no id.
//
// `--recusar-envio` does NOT reach this path, on purpose: that flag exists to
// prove that a refused send leaves the instance PAUSED (see the header and
// cmd/zapgw/smoke.go), and no step of the lab marks a read. If one day someone
// needs to exercise the refusal here, the new flag is its own — reusing that one
// would make a log line say "envio recusado" for something that is not a send.
func (g *fakeGraph) markAsRead(w http.ResponseWriter) {
	g.reads.Add(1)
	respond(w, http.StatusOK, map[string]any{"success": true})
}

func (g *fakeGraph) send(w http.ResponseWriter) {
	if g.refuseSend {
		// THE SAME error shape as Meta's (error.message + error.code): what
		// classifies is internal/meta.ClassifyResponse, and a body in another
		// shape would prove an error path production does not have.
		g.writeError(w, http.StatusBadRequest, 131000, "grafo-falso: envio recusado a pedido (--recusar-envio)")
		return
	}
	// A UNIQUE id per send. A fixed id would pass just the same today, but it
	// would mask anything that came to depend on distinct ids — and the lab exists
	// to exercise the path, not to please it.
	n := g.sent.Add(1)
	respond(w, http.StatusOK, map[string]any{
		"messaging_product": "whatsapp",
		"messages":          []map[string]any{{"id": fmt.Sprintf("wamid.LABORATORIO-%d", n)}},
	})
}

// createTemplate serves `POST /{waba_id}/message_templates`.
//
// WITH --falha-de-template IT DIES WITHOUT ANSWERING, and that is what makes it
// useful: `panic(http.ErrAbortHandler)` makes the server close the connection
// midway, which is exactly what the gateway saw on 2026-07-28. A `500` would be
// an ANSWER, and an answer is something Meta classifies — the `desconhecido`
// outcome is born precisely from its ABSENCE, and a fake that answered an error
// would exercise another branch.
//
// THE ORDER MATTERS and it is the real world's: in `criado` mode, the template
// enters the catalog BEFORE the connection drops. That is how
// `pedido_avaliacao_v2` came to exist without anyone knowing.
func (g *fakeGraph) createTemplate(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyCap))
	if err != nil {
		g.writeError(w, http.StatusBadRequest, 100, "grafo-falso: corpo do POST ilegivel")
		return
	}
	var p struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		g.writeError(w, http.StatusBadRequest, 100, "grafo-falso: criacao de template sem `name`")
		return
	}

	didCreate := g.templateFailure == failTemplateNone || g.templateFailure == failTemplateCreated
	if didCreate {
		g.mu.Lock()
		// There is NO duplicate-name guard here, and the omission is deliberate:
		// Meta has one (`code 100` / `subcode 2388024`), but simulating it would
		// make the fake HELP the gateway never repeat the creation. The guarantee
		// that it does not repeat has to come from its own code, and it is proven
		// in TestRereadDoesNOTCreateAgain (internal/outbound).
		g.catalog = append(g.catalog, map[string]any{
			"id":         fmt.Sprintf("LABORATORIO-%d", len(g.catalog)+1),
			"name":       p.Name,
			"status":     "PENDING",
			"category":   p.Category,
			"language":   p.Language,
			"components": []any{},
		})
		g.mu.Unlock()
	}

	if g.templateFailure != failTemplateNone {
		log.Printf("grafo-falso: derrubando a conexao do POST de template SEM resposta (--falha-de-template=%s)",
			g.templateFailure)
		panic(http.ErrAbortHandler)
	}

	g.mu.Lock()
	created := g.catalog[len(g.catalog)-1]
	g.mu.Unlock()
	respond(w, http.StatusOK, map[string]any{
		"id": created["id"], "status": created["status"], "category": created["category"],
	})
}

// listTemplates serves `GET /{waba_id}/message_templates`.
//
// NO PAGINATION (no `paging.next`): the gateway already has a pagination test of
// its own, and a fake that paginated would have to get the shape of `next` right
// so as not to turn into an error for another reason. What this path exists to
// prove is the RE-READ after the ambiguous creation.
func (g *fakeGraph) listTemplates(w http.ResponseWriter) {
	if g.templateFailure == failTemplateCatalogToo {
		log.Printf("grafo-falso: derrubando tambem o GET do catalogo (--falha-de-template=%s)", g.templateFailure)
		panic(http.ErrAbortHandler)
	}
	g.mu.Lock()
	items := append([]map[string]any(nil), g.catalog...)
	g.mu.Unlock()
	if items == nil {
		// `[]`, never `null`: the gateway handles both, but a fake that sent `null`
		// would hide the day it stops handling it.
		items = []map[string]any{}
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (g *fakeGraph) writeError(w http.ResponseWriter, status, code int, message string) {
	respond(w, status, map[string]any{
		"error": map[string]any{"message": message, "code": code},
	})
}

func respond(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
