// Isolation between tenants on the WAY OUT — ALL routes, at once (T-086).
//
// The question this file answers is not "does route X reject?", which each
// test file already answered on its own. It is that question's sibling, and
// it can only be asked in one place:
//
//	is there ANY route through which consumer B can read or touch A's instance?
//
// The difference matters because of this project's mother pitfall: the rule
// holds in one place and does not hold in the next. When each route proves
// its own 403 in its own file, the route that IS BORN without a test does not
// show up anywhere — that was exactly the case with `POST /v1/templates`,
// which shares the guard with its `GET` sibling and still never had its own
// test for someone else's instance.
//
// THAT IS WHY THE TABLE IS CHECKED AGAINST THE PACKAGE, and not just written
// by hand: TestIsolationTableCOVERSEveryRouteRegisteredInThePackage reads the
// `mux.HandleFunc` calls in this directory's files and goes red NAMING the
// route nobody covered. It is the same discipline as T-048 ("every hand-written
// list over a schema needs something that asks the schema"), with the schema
// swapped for the set of routes.
//
// EACH ROUTE'S TWO QUESTIONS, and the second is the one that did not exist:
//
//  1. SOMEONE ELSE'S instance (exists, and belongs to another consumer) -> 403;
//  2. NONEXISTENT instance -> 403 TOO, never 404.
//
// Without (2), the response status becomes an oracle for "does this slug
// exist?" for anyone with any token. The code already does it right in every
// route — `CanUse` runs before `FindInstance` in all eight —, but until
// this task that was a pattern kept by discipline, with no test that would go
// red if someone reversed the order in one of them.
package outbound

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// A's instance and B's come from storeWithConsumer (lojinha / clinica).
// sistema-a is already born linked to lojinha; here the SECOND consumer is
// born, which was what was missing for the question to make sense.
const (
	slugOfA     = "lojinha"
	slugOfB     = "clinica"
	tokenOfB    = "token-do-b"
	missingSlug = "nao-existe-em-lugar-nenhum"
)

// outboundRoute is a package route and the MINIMAL request that reaches its
// link guard. Minimal matters: an invalid body would stop earlier, at a 400,
// and the test would end up proving schema validation instead of isolation.
type outboundRoute struct {
	// rota is the EXACT string of the mux.HandleFunc that registers this route.
	// It is by it that the check against the package matches — changing the
	// registration pattern without changing it here leaves the coverage test
	// red, which is the point.
	route string
	build func(slug string) *http.Request
}

func newRequest(method, target, body string) *http.Request {
	return httptest.NewRequest(method, target, strings.NewReader(body))
}

func outboundRoutes() []outboundRoute {
	return []outboundRoute{
		{
			route: "POST /v1/messages",
			build: func(slug string) *http.Request {
				r := newRequest(http.MethodPost, "/v1/messages",
					`{"instancia":"`+slug+`","para":"5511900000001","tipo":"texto","texto":"oi"}`)
				r.Header.Set("Idempotency-Key", "k-isolamento-"+slug)
				return r
			},
		},
		{
			route: "POST /v1/leituras",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodPost, "/v1/leituras", readBody(slug, testWamid))
			},
		},
		{
			route: "GET /v1/templates",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodGet, "/v1/templates?instancia="+slug, "")
			},
		},
		{
			// The route the table exists to have caught: it shares
			// `instanceActive` with its GET sibling and still never had its
			// own test for someone else's instance. Sharing the function is
			// not the same thing as having the guarantee — the same lesson
			// from `fumaca`, which shared the body assembly but not the
			// instrumentation (T-054).
			route: "POST /v1/templates",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodPost, "/v1/templates",
					`{"instancia":"`+slug+`","nome":"promo","categoria":"MARKETING",`+
						`"idioma":"pt_BR","componentes":[{"type":"BODY","text":"oi"}]}`)
			},
		},
		{
			// 🔴 THE ONLY ROUTE HERE THAT DESTROYS SOMETHING ON META'S SIDE,
			// and it has no undo: a template comes back only by hand, gets
			// reapproved on Meta's schedule, and its name stays blocked for
			// 30 days. A leaked token reaching someone else's instance
			// through this route does not read their business — it deletes
			// it.
			//
			// It is also the route that runs about once a year, so this line
			// is the only thing that exercises it every day (T-173).
			route: "DELETE /v1/templates",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodDelete,
					"/v1/templates?instancia="+slug+"&nome=qualquer_um", "")
			},
		},
		{
			route: "POST /v1/media",
			build: func(slug string) *http.Request {
				// Empty body on purpose: the link guard runs BEFORE the multipart
				// is read, and a valid body would only add noise.
				return newRequest(http.MethodPost, "/v1/media?instancia="+slug, "")
			},
		},
		{
			route: "GET /v1/media/{id}",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodGet, "/v1/media/MEDIA-DO-OUTRO?instancia="+slug, "")
			},
		},
		{
			route: "GET /v1/instances/{slug}/health",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodGet, "/v1/instances/"+slug+"/health", "")
			},
		},
		{
			route: "GET /v1/estado",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodGet, "/v1/estado?instancia="+slug, "")
			},
		},
		{
			route: "POST /v1/cadastro",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodPost, "/v1/cadastro",
					`{"instancia":"`+slug+`","waba_id":"W-DE-B","phone_number_id":"P-DE-B",`+
						`"app_secret":"s","token_envio":"t"}`)
			},
		},
		{
			// 🔴 `POST /v1/fumaca` SENDS A REAL PAID MESSAGE when it reaches
			// the third step (fumaca.go). The link guard (CanUse) runs
			// BEFORE step 1 (the instance existing) and WAY before step 2
			// (talking to Meta) — this file's two rejection tests use
			// uncallableMeta, which fails the test if any thread
			// reaches Meta. If this guard is ever inverted, that fake server
			// is what flags it before a real message goes out.
			route: "POST /v1/fumaca",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodPost, "/v1/fumaca", smokeBody(slug, testSmokeDestination))
			},
		},
		{
			route: "POST /v1/pausa",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodPost, "/v1/pausa", `{"instancia":"`+slug+`"}`)
			},
		},
		{
			route: "POST /v1/bloqueios",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodPost, "/v1/bloqueios",
					`{"instancia":"`+slug+`","telefones":["5511999990000"]}`)
			},
		},
		{
			route: "DELETE /v1/bloqueios",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodDelete, "/v1/bloqueios",
					`{"instancia":"`+slug+`","telefones":["5511999990000"]}`)
			},
		},
		{
			route: "GET /v1/bloqueios",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodGet, "/v1/bloqueios?instancia="+slug, "")
			},
		},
		{
			route: "GET /v1/perfil",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodGet, "/v1/perfil?instancia="+slug, "")
			},
		},
		{
			route: "POST /v1/perfil",
			build: func(slug string) *http.Request {
				return newRequest(http.MethodPost, "/v1/perfil", `{"instancia":"`+slug+`","about":"x"}`)
			},
		},
	}
}

// allOutboundRoutes builds outboundRoutes()'s surfaces over the SAME store, with the two
// consumers linked, and hangs them on the same mux `main` uses — the same
// patterns as cmd/zapgw/main.go, because a different mux here would prove
// routing production does not have.
//
// META COMES AS A PARAMETER, not fixed: the REJECTION tests pass a server
// that FAILS THE TEST if it is called (rejection by link has to happen before
// any thread, or else a leaked token burns someone else's quota just by
// knocking on the door), and the positive control — which needs to reach the
// other side of the guard — passes one that answers with anything.
func allOutboundRoutes(t *testing.T, srv *httptest.Server) http.Handler {
	t.Helper()

	store, path := storeWithConsumer(t)
	if err := store.CreateConsumer("sistema-b", tokenOfB, []string{slugOfB}); err != nil {
		t.Fatalf("CreateConsumer sistema-b: %v", err)
	}
	// BOTH ACTIVE on purpose: with A's instance paused, every 403 in this file
	// could be coming from the pause guard, and the suite would go green with
	// `CanUse` turned off. It is the same pitfall the existing 403 tests
	// already record one by one.
	activateInstance(t, path, slugOfA)
	activateInstance(t, path, slugOfB)

	client := meta.NewClient(srv.Client(), srv.URL)
	auth := NewAuthenticator(store)
	counter := config.NewCounter(store)
	const maxBytes = 1 << 20

	mux := http.NewServeMux()
	mux.Handle("/v1/messages", NewHandler(store, auth, client, maxBytes, counter, config.NewTransit(store), AllTypes))
	mux.Handle("/v1/leituras", NewReadsHandler(store, auth, client, maxBytes, counter, WhatsAppOnly))
	mux.Handle("/v1/templates", NewTemplatesHandler(store, auth, client, maxBytes, counter, WhatsAppOnly))
	media := NewMediaHandler(store, auth, client, WhatsAppOnly)
	mux.Handle("/v1/media", media)
	mux.Handle("/v1/media/", media)
	mux.Handle("/v1/instances/", NewHealthHandler(store, auth, client, AllTypes))
	mux.Handle("/v1/estado", NewStateHandler(store, auth, NewWatchdog(store, client), nil,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, AllTypes))
	mux.Handle("/v1/cadastro", NewRegistrationHandler(store, auth, maxBytes, counter, WhatsAppOnly))
	mux.Handle("/v1/fumaca", NewSmokeHandler(store, auth, client, counter, maxBytes, AllTypes))
	mux.Handle("/v1/pausa", NewPauseHandler(store, auth, maxBytes, counter, AllTypes))
	mux.Handle("/v1/bloqueios", NewBlockHandler(store, auth, client, maxBytes, counter, WhatsAppOnly))
	mux.Handle("/v1/perfil", NewProfileHandler(store, auth, client, maxBytes, WhatsAppOnly))
	return mux
}

// anyResponseMeta exists ONLY for the positive control: it needs
// to get past the link guard, and what happens after it is not this file's
// subject.
func anyResponseMeta(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func askWithToken(t *testing.T, h http.Handler, req *http.Request, token string) *httptest.ResponseRecorder {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Consumer B, with THEIR OWN token, pointing at A's instance: 403 on ALL
// routes, including the pure-read ones.
//
// READING IS NOT LESS SERIOUS THAN WRITING, and that is why `GET /v1/estado`,
// `GET /v1/templates` and `GET /v1/media/{id}` are in the same table as
// sending: the catalog describes the other business's campaigns, the state
// describes their traffic volume, and the media IS customer content.
func TestNoOutboundRouteLetsBTouchAsInstance(t *testing.T) {
	h := allOutboundRoutes(t, uncallableMeta(t))

	for _, r := range outboundRoutes() {
		t.Run(r.route, func(t *testing.T) {
			rec := askWithToken(t, h, r.build(slugOfA), tokenOfB)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s com o token de B apontando para %q: status = %d, quero 403; corpo = %s",
					r.route, slugOfA, rec.Code, rec.Body.String())
			}
			// The rejection cannot tell the name, number, or anything about someone else's instance.
			if body := rec.Body.String(); strings.Contains(body, testDisplayNumbers[slugOfA]) {
				t.Errorf("%s vazou o numero exibido da instancia alheia na recusa: %s", r.route, body)
			}
		})
	}
}

// The SAME 403 for a slug that DOES NOT EXIST — and it is this half that
// stops the route from becoming an oracle.
//
// If a route answered 404 here and 403 above, anyone with a valid token could
// enumerate this gateway's slugs by comparing the two statuses. Instance
// names are business names.
func TestNoOutboundRouteSaysWhetherAFOREIGNSlugExists(t *testing.T) {
	h := allOutboundRoutes(t, uncallableMeta(t))

	for _, r := range outboundRoutes() {
		t.Run(r.route, func(t *testing.T) {
			foreign := askWithToken(t, h, r.build(slugOfA), tokenOfB)
			missing := askWithToken(t, h, r.build(missingSlug), tokenOfB)

			if missing.Code != http.StatusForbidden {
				t.Fatalf("%s com slug inexistente: status = %d, quero 403 — 404 aqui responde"+
					" \"este slug existe?\" a quem tem um token qualquer; corpo = %s",
					r.route, missing.Code, missing.Body.String())
			}
			if foreign.Code != missing.Code {
				t.Fatalf("%s distingue instancia alheia (%d) de inexistente (%d) — a diferenca E' o oraculo",
					r.route, foreign.Code, missing.Code)
			}
		})
	}
}

// The other side, and it exists for the same reason as the test that proves
// a MATCHING phone_number_id does not count as a discard: without this, an
// inverted guard (rejecting everything) would go green on the two tests above
// and would only show up when the consumer tried to use their OWN instance.
//
// We do not assert 2xx: each route follows different paths after the guard,
// and this test's Meta answers with a body that says nothing. What is
// required is only that the LINK rejection does not happen — no 403 response.
func TestOutboundRoutesDoNotRefuseBOnBsOWNInstance(t *testing.T) {
	h := allOutboundRoutes(t, anyResponseMeta(t))

	for _, r := range outboundRoutes() {
		t.Run(r.route, func(t *testing.T) {
			rec := askWithToken(t, h, r.build(slugOfB), tokenOfB)
			if rec.Code == http.StatusForbidden {
				t.Fatalf("%s recusou o consumidor na instancia DELE (403) — a guarda esta invertida;"+
					" corpo = %s", r.route, rec.Body.String())
			}
		})
	}
}

// The check that stops this table from rotting.
//
// It reads the `mux.HandleFunc(...)` calls in this package's NON-test files
// and requires every registered route to appear in the table. A new route
// without an entry in the table leaves this test red NAMING the route —
// which is the only way for the next `POST /v1/templates` not to repeat this
// one's history: being born sharing a sibling's guard and, because of that,
// appearing covered.
//
// The table's list stays explicit on purpose. A future route may legitimately
// NOT have an instance guard (`main`'s `GET /v1/health` does not, and should
// not); this check exists so the decision is MADE, not to make it on its own.
func TestIsolationTableCOVERSEveryRouteRegisteredInThePackage(t *testing.T) {
	inTable := map[string]bool{}
	for _, r := range outboundRoutes() {
		inTable[r.route] = true
	}

	registeredRoutes := routesRegisteredInPackage(t)
	if len(registeredRoutes) == 0 {
		t.Fatal("nenhuma rota encontrada no pacote — a conferencia parou de conferir," +
			" e uma conferencia que nao acha nada passa verde para sempre")
	}
	for _, route := range registeredRoutes {
		if !inTable[route] {
			t.Errorf("a rota %q e registrada neste pacote e NAO esta na tabela de isolamento:"+
				" ninguem prova que o consumidor B leva 403 nela", route)
		}
	}
	for route := range inTable {
		if !contains(registeredRoutes, route) {
			t.Errorf("a tabela de isolamento cobre %q, que nao e mais registrada no pacote —"+
				" a entrada esta provando uma rota que nao existe", route)
		}
	}
}

func contains(catalog []string, target string) bool {
	for _, s := range catalog {
		if s == target {
			return true
		}
	}
	return false
}

var (
	// The argument can be a literal ("GET /v1/estado") or a package constant
	// (registrationRoute) — both forms exist today.
	reRegistration = regexp.MustCompile(`mux\.HandleFunc\(\s*([^,]+),`)
	reConstant     = regexp.MustCompile(`(?m)^\s*(?:const\s+)?(\w+)\s*=\s*"([^"]+)"`)
)

// routesRegisteredInPackage asks THE CODE which routes exist, instead of
// trusting a hand-written list.
//
// It reads the package directory (`go test` runs with the cwd inside it).
// Test files are left out: a route assembled by a test is not a production
// surface.
func routesRegisteredInPackage(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ler o diretorio do pacote: %v", err)
	}

	sources := map[string]string{}
	constants := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("ler %s: %v", name, err)
		}
		sources[name] = string(b)
		for _, m := range reConstant.FindAllStringSubmatch(string(b), -1) {
			constants[m[1]] = m[2]
		}
	}

	var routes []string
	for name, source := range sources {
		for _, m := range reRegistration.FindAllStringSubmatch(source, -1) {
			arg := strings.TrimSpace(m[1])
			if value, err := strconv.Unquote(arg); err == nil {
				routes = append(routes, value)
				continue
			}
			value, ok := constants[arg]
			if !ok {
				// Fail loud: an argument this reader cannot resolve
				// would hide an entire route from the check.
				t.Fatalf("%s registra uma rota por %q, e esta conferencia nao sabe resolver esse valor —"+
					" ensine-a antes de seguir, senao a rota fica fora da tabela sem ninguem notar", name, arg)
			}
			routes = append(routes, value)
		}
	}
	return routes
}
