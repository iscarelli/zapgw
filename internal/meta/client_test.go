package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(srv *httptest.Server) *Client {
	return NewClient(srv.Client(), srv.URL)
}

func TestSendMessageReturnsMetasID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.ENVIADO"}]}`))
	}))
	defer srv.Close()

	resp, err := testClient(srv).SendMessage(
		context.Background(), "PNID1", "token", map[string]any{"to": "5511999990000"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.ID != "wamid.ENVIADO" {
		t.Fatalf("id = %q, want wamid.ENVIADO", resp.ID)
	}
}

// TRAP — cost paid by this network's other project.
// A Meta `200` does NOT prove an id came with it. The same response can
// bring {"messages":[]}, {}, or an id of the wrong type — all valid JSON
// with a status < 400, passing straight through the whole client without
// touching error classification. Returning "" as success is the worst of
// outcomes: the consumer records wa_message_id="" in the database, the
// record LOOKS sent, and the defect only shows up far from its origin, with
// no pointer back.
func TestSendMessageRefuses200WithoutAnID(t *testing.T) {
	cases := []string{
		`{"messages":[]}`,
		`{}`,
		`{"messages":[{}]}`,
		`{"messages":[{"id":""}]}`,
		`{"messages":[{"id":123}]}`,
		`{"messages":"nao e lista"}`,
		`null`,
		``,
		`not json`,
	}

	for _, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))

		resp, err := testClient(srv).SendMessage(
			context.Background(), "PNID1", "token", map[string]any{})
		srv.Close()

		if err == nil {
			t.Errorf("body %q returned SUCCESS with id %q", body, resp.ID)
			continue
		}
		if !errors.Is(err, ErrResponseWithoutID) {
			t.Errorf("body %q: err = %v, want ErrResponseWithoutID", body, err)
		}
		if resp.ID != "" {
			t.Errorf("body %q returned id %q together with an error", body, resp.ID)
		}
	}
}

func TestSendMessageClassifiesMetasError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid parameter","code":100}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).SendMessage(
		context.Background(), "PNID1", "token", map[string]any{})

	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want *MetaError", err)
	}
	if me.Class != ClassPermanent {
		t.Errorf("Class = %q, want permanent", me.Class)
	}
	if me.MetaCode != 100 {
		t.Errorf("MetaCode = %d, want 100", me.MetaCode)
	}
}

func TestSendMessageSendsTheTokenInTheHeaderAndNeverInTheURL(t *testing.T) {
	// A token in a query string leaks into proxy, server, and CDN logs.
	var authorization, requestedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		requestedURL = r.URL.String()
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.X"}]}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).SendMessage(
		context.Background(), "PNID1", "token-secreto", map[string]any{})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if authorization != "Bearer token-secreto" {
		t.Errorf("Authorization = %q", authorization)
	}
	if strings.Contains(requestedURL, "token-secreto") {
		t.Fatalf("the token showed up in the URL: %s", requestedURL)
	}
	if !strings.Contains(requestedURL, "PNID1") {
		t.Errorf("the URL doesn't carry the phone_number_id: %s", requestedURL)
	}
}

func TestSendMessageHonorsTheContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := testClient(srv).SendMessage(ctx, "PNID1", "t", map[string]any{}); err == nil {
		t.Fatal("canceled context did not interrupt the call")
	}
}

// IMPORTANT found in the T4 review. The guard caught the EMPTY id and let
// the BLANK id through — and an id made of only spaces is as useless as an
// empty one. The consumer would record a wa_message_id that's good for
// nothing, and the record would look sent.
func TestSendMessageRefusesABlankID(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"id":"   "}]}`,
		`{"messages":[{"id":"\t"}]}`,
		`{"messages":[{"id":"\n"}]}`,
		`{"messages":[{"id":" \t\n "}]}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))

		resp, err := testClient(srv).SendMessage(
			context.Background(), "PNID1", "token", map[string]any{})
		srv.Close()

		if err == nil {
			t.Errorf("body %q returned SUCCESS with id %q", body, resp.ID)
		}
		if resp.ID != "" {
			t.Errorf("body %q returned id %q together with an error", body, resp.ID)
		}
	}
}

func TestSendMessageTrimsTheIDBeforeReturning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"id":"  wamid.COM_ESPACO  "}]}`))
	}))
	defer srv.Close()

	resp, err := testClient(srv).SendMessage(
		context.Background(), "PNID1", "token", map[string]any{})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.ID != "wamid.COM_ESPACO" {
		t.Fatalf("id = %q — the leading/trailing spaces were propagated", resp.ID)
	}
}

// The review noted that the existing tests don't discriminate against an
// implementation that searched for the first `id` ANYWHERE in the JSON. The
// typed struct already prevents that; this test makes the guarantee
// explicit instead of accidental.
func TestSendMessageOnlyAcceptsTheIDInMessagesZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(
			`{"contacts":[{"id":"nao-e-esse"}],"messages":[{}],"meta":{"id":"nem-esse"}}`))
	}))
	defer srv.Close()

	resp, err := testClient(srv).SendMessage(
		context.Background(), "PNID1", "token", map[string]any{})

	if err == nil {
		t.Fatalf("accepted an id that wasn't in messages[0]: %q", resp.ID)
	}
	if resp.ID != "" {
		t.Fatalf("id = %q", resp.ID)
	}
}

func TestSendMessageRefusesAPhoneNumberIDThatWouldEscape(t *testing.T) {
	// url.JoinPath resolves `..`, so a dirty id escapes the version prefix
	// and points to another Graph API endpoint.
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.X"}]}`))
	}))
	defer srv.Close()

	for _, dirty := range []string{
		"../outro", "../../etc/passwd", "a/b", "", "PNID?x=1", "PNID#frag", "PNID 1", "PNID%2F",
	} {
		_, err := testClient(srv).SendMessage(
			context.Background(), dirty, "token", map[string]any{})
		if !errors.Is(err, ErrInvalidPhoneNumberID) {
			t.Errorf("id %q: err = %v, want ErrInvalidPhoneNumberID", dirty, err)
		}
	}
	if called {
		t.Fatal("the client CALLED the network with an invalid phone_number_id")
	}
}

func TestSendMessageAcceptsANormalPhoneNumberID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.X"}]}`))
	}))
	defer srv.Close()

	for _, good := range []string{"123456789012345", "PNID_1", "pnid-abc"} {
		if _, err := testClient(srv).SendMessage(
			context.Background(), good, "token", map[string]any{}); err != nil {
			t.Errorf("valid id %q refused: %v", good, err)
		}
	}
}

// T-034. Meta only sends message_status on a template under pacing (see the
// source cited in sendResponse, internal/meta/client.go); for the rest of
// the traffic the field comes back ABSENT, and that's the normal case, not
// a read failure. ABSENCE != "accepted": treating the two the same would be
// asserting a value Meta never sent.
//
// MANDATORY MUTATION (see docs/TASKS.md, T-034): swapping
// `MessageStatus: envelope.Messages[0].MessageStatus` for an `if` that
// turns into "accepted" when the field comes empty leaves this test RED —
// proven manually before the commit, without leaving the mutant code in the
// repository.
func TestSendMessageAnAbsentMessageStatusStaysEmptyNotAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.SEM-STATUS"}]}`)) // no message_status
	}))
	defer srv.Close()

	resp, err := testClient(srv).SendMessage(
		context.Background(), "PNID1", "token", map[string]any{})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.MessageStatus != "" {
		t.Fatalf("MessageStatus = %q, want \"\" (absent) — not an invented \"accepted\"", resp.MessageStatus)
	}
}

// The case where Meta SENDS the field: the three documented values (see
// the source in sendResponse) arrive RAW, with no translation — not into
// Portuguese, and not into this package's error classes
// (retryable/permanent/config).
func TestSendMessageReturnsTheRawMessageStatus(t *testing.T) {
	for _, status := range []string{"accepted", "held_for_quality_assessment", "paused"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.X","message_status":"` + status + `"}]}`))
		}))

		resp, err := testClient(srv).SendMessage(
			context.Background(), "PNID1", "token", map[string]any{})
		srv.Close()

		if err != nil {
			t.Fatalf("status %q: SendMessage: %v", status, err)
		}
		if resp.MessageStatus != status {
			t.Errorf("status %q: MessageStatus = %q, want the raw value back", status, resp.MessageStatus)
		}
	}
}
