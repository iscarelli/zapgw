// Tests for the `instagram_token` block of GET /v1/estado (T-098) — the
// main DELIVERABLE of this task, after the owner's decision on
// 2026-07-30 ("you don't need to alarm anything... the state has to tell the
// truth from the first failure").
package outbound

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// testInertWatchdog is a watchdog that never measures anything — BuildState
// requires one (does not accept nil, unlike the renewer), but no test in this
// file needs a TOKEN META verdict, only token_instagram.
func testInertWatchdog(store *config.Store) *Watchdog {
	return NewWatchdog(store, meta.NewClient(nil, "http://127.0.0.1:1"))
}

// --- not_applicable: WHATSAPP instance -----------------------------------

func TestStateInstagramTokenIsNotApplicableForWhatsapp(t *testing.T) {
	store, _ := storeWithConsumer(t) // creates "lojinha", tipo=whatsapp
	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, "lojinha", time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	ti := e.InstagramToken
	if ti.Verdict != VerdictIGTokenNotApplicable {
		t.Errorf("verdict = %q, want %q", ti.Verdict, VerdictIGTokenNotApplicable)
	}
	// ASSERTED ABSENCE: the verdict says "does not apply", and the date fields
	// stay null — never a number calculated over a deadline that doesn't exist.
	if ti.SetAt != nil || ti.ExpiresAt != nil || ti.DaysLeft != nil || ti.RenewedAt != nil || ti.FailingSince != nil {
		t.Errorf("date/renewal field filled on a not_applicable instance: %+v", ti)
	}
	if ti.Instruction != nil {
		t.Errorf("instruction present with no problem at all: %q", *ti.Instruction)
	}
}

// --- aguardando: valid token, threshold not yet reached ------------------

func TestStateInstagramTokenWaitingWhenItHasNotRenewedYet(t *testing.T) {
	setAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	store, slug := storeWithInstagram(t, setAt)
	now := setAt.Add(9 * 24 * time.Hour) // within the window, far from the threshold

	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, slug, now)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	ti := e.InstagramToken
	if ti.Verdict != VerdictIGTokenWaiting {
		t.Errorf("verdict = %q, want %q", ti.Verdict, VerdictIGTokenWaiting)
	}
	if ti.SetAt == nil || *ti.SetAt != setAt.Format(time.RFC3339) {
		t.Errorf("SetAt = %v, want %s", ti.SetAt, setAt.Format(time.RFC3339))
	}
	want := setAt.Add(InstagramTokenValidity).Format(time.RFC3339)
	if ti.ExpiresAt == nil || *ti.ExpiresAt != want {
		t.Errorf("ExpiresAt = %v, want %s", ti.ExpiresAt, want)
	}
	if ti.DaysLeft == nil || *ti.DaysLeft != 51 { // 60 - 9
		t.Errorf("DaysLeft = %v, want 51", ti.DaysLeft)
	}
	if ti.RenewedAt != nil {
		t.Errorf("RenewedAt = %v, want nil — the loop never renewed this token", ti.RenewedAt)
	}
	if ti.Instruction != nil {
		t.Errorf("instruction present with no problem at all: %q", *ti.Instruction)
	}
}

// --- ok: the loop has ALREADY renewed successfully at least once ------------------

func TestStateInstagramTokenOkAfterRenewingSuccessfully(t *testing.T) {
	store, slug := storeWithInstagram(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	renewedAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := store.RenewInstagramTokenAt(slug, "token-novo", renewedAt); err != nil {
		t.Fatalf("RenewInstagramTokenAt: %v", err)
	}
	now := renewedAt.Add(24 * time.Hour)

	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, slug, now)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	ti := e.InstagramToken
	if ti.Verdict != VerdictIGTokenOK {
		t.Errorf("verdict = %q, want %q — the loop already renewed successfully", ti.Verdict, VerdictIGTokenOK)
	}
	if ti.RenewedAt == nil || *ti.RenewedAt != renewedAt.Format(time.RFC3339) {
		t.Errorf("RenewedAt = %v, want %s", ti.RenewedAt, renewedAt.Format(time.RFC3339))
	}
	if ti.Instruction != nil {
		t.Errorf("instruction present with no problem at all: %q", *ti.Instruction)
	}
}

// --- falhando: honest FROM THE FIRST failure, and CARRIES THE INSTRUCTION -------
//
// Explicit request from the owner, 2026-07-30: "the failure state has to say
// what to do, not just that it's bad" — the consumer does NOT have the token
// in hand, and a verdict without an instruction would be a dead end.

func TestStateInstagramTokenFailingCarriesInstructionAndFailingSince(t *testing.T) {
	setAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	store, slug := storeWithInstagram(t, setAt)

	rv := NewInstagramRenewer(store, meta.NewClient(nil, "http://127.0.0.1:1"), "http://127.0.0.1:1")
	failingSince := setAt.Add(31 * 24 * time.Hour)
	rv.markFailure(slug, failingSince)

	now := failingSince.Add(2 * time.Hour) // failing for 2h - honest EVEN newly-started

	e, err := BuildState(store, testInertWatchdog(store), rv, IngressSource{}, nil, nil, testVersion, slug, now)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	ti := e.InstagramToken
	if ti.Verdict != VerdictIGTokenFailing {
		t.Errorf("verdict = %q, want %q", ti.Verdict, VerdictIGTokenFailing)
	}
	if ti.FailingSince == nil || *ti.FailingSince != failingSince.Format(time.RFC3339) {
		t.Errorf("FailingSince = %v, want %s", ti.FailingSince, failingSince.Format(time.RFC3339))
	}
	// THE CENTRAL ASSERTION OF THE OWNER'S REQUEST: the "falhando" LABEL is not
	// enough — the INSTRUCTION of what to do has to come with it, and it has to
	// say that the resolution is MANUAL (the consumer does not have the token
	// to fix it alone).
	if ti.Instruction == nil {
		t.Fatal("Instruction absent with a failing verdict — the consumer has no way to know what to do")
	}
	if !strings.Contains(*ti.Instruction, "MANUAL") {
		t.Errorf("Instruction = %q, expected to mention that the resolution is MANUAL", *ti.Instruction)
	}
	if *ti.Instruction != InstructionIGTokenFailing {
		t.Errorf("Instruction = %q, want the constant InstructionIGTokenFailing", *ti.Instruction)
	}
}

// --- expirado: past 60 days, also carries an instruction ---------------

func TestStateInstagramTokenExpiredCarriesInstruction(t *testing.T) {
	setAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	store, slug := storeWithInstagram(t, setAt)
	now := setAt.Add(61 * 24 * time.Hour) // past the 60 days

	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, slug, now)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	ti := e.InstagramToken
	if ti.Verdict != VerdictIGTokenExpired {
		t.Errorf("verdict = %q, want %q", ti.Verdict, VerdictIGTokenExpired)
	}
	if ti.DaysLeft == nil || *ti.DaysLeft >= 0 {
		t.Errorf("DaysLeft = %v, want negative (already past deadline)", ti.DaysLeft)
	}
	if ti.Instruction == nil {
		t.Fatal("Instruction absent with an expired verdict")
	}
	if !strings.Contains(strings.ToLower(*ti.Instruction), "manual") {
		t.Errorf("Instruction = %q, expected to mention MANUAL login on Meta", *ti.Instruction)
	}
	if *ti.Instruction != InstructionIGTokenExpired {
		t.Errorf("Instruction = %q, want the constant InstructionIGTokenExpired", *ti.Instruction)
	}
}

// --- expirado has PRECEDENCE over falhando -------------------------------
//
// An instance can be both "falhando" (the last attempt didn't work) and
// already "expirada" (age >= 60 days) at the same time — and the verdict has
// to pick ONE, and the strongest one: "expirado" changes what the person does
// (manual login, not "wait for the next attempt").
func TestStateInstagramTokenExpiredTakesPrecedenceOverFailing(t *testing.T) {
	setAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	store, slug := storeWithInstagram(t, setAt)

	rv := NewInstagramRenewer(store, meta.NewClient(nil, "http://127.0.0.1:1"), "http://127.0.0.1:1")
	rv.markFailure(slug, setAt.Add(58*24*time.Hour))

	now := setAt.Add(65 * 24 * time.Hour) // expired AND failing at the same time

	e, err := BuildState(store, testInertWatchdog(store), rv, IngressSource{}, nil, nil, testVersion, slug, now)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if e.InstagramToken.Verdict != VerdictIGTokenExpired {
		t.Errorf("verdict = %q, want %q (expired beats failing)", e.InstagramToken.Verdict, VerdictIGTokenExpired)
	}
}

// --- HTTP integration: the real JSON carries the block --------------------

func TestGETStateInstagramExposesInstagramTokenInTheJSON(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")

	renewer := NewInstagramRenewer(store, meta.NewClient(nil, "http://127.0.0.1:1"), "http://127.0.0.1:1")
	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), renewer,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	rec := askState(t, h, "token-do-a", "insta-loja")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	ti, ok := body["instagram_token"].(map[string]any)
	if !ok {
		t.Fatalf("instagram_token absent or of another type in the JSON: %v", body["instagram_token"])
	}
	if ti["verdict"] != VerdictIGTokenWaiting {
		t.Errorf("verdict = %v, want %q", ti["verdict"], VerdictIGTokenWaiting)
	}
	if ti["definido_em"] == nil {
		t.Error("definido_em absent — instance insta-loja has had a token since creation")
	}
	if ti["expires_at"] == nil {
		t.Error("expires_at absent")
	}
	if _, has := ti["days_left"]; !has {
		t.Error("days_left absent")
	}
}

// And the SAME endpoint, on a WHATSAPP instance, has to say not_applicable IN
// THE REAL JSON — not just in the Go struct.
func TestGETStateWhatsappInstagramTokenIsNotApplicableInTheJSON(t *testing.T) {
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	rec := askState(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	ti, ok := body["instagram_token"].(map[string]any)
	if !ok {
		t.Fatalf("instagram_token absent or of another type in the JSON: %v", body["instagram_token"])
	}
	if ti["verdict"] != VerdictIGTokenNotApplicable {
		t.Errorf("verdict = %v, want %q — instagram_token CANNOT vanish nor come zeroed on a whatsapp instance",
			ti["verdict"], VerdictIGTokenNotApplicable)
	}
}

// --- T-099: not_applicable in the REVERSE DIRECTION — WhatsApp blocks on an ------
// --- INSTAGRAM instance. It's the SAME mother pitfall that T-098 closed on one
// side (above) and left open on the other: measured in production (tenant-two-ig,
// v0.36.0, 2026-07-30 21:11), numero_na_meta said `never_observed` — the
// WRONG answer, because quality and message tier ARE WhatsApp concepts and
// will NEVER exist on an Instagram instance; the right answer is NotApplicable.

func TestStateNumberAtMetaIsNotApplicableForInstagram(t *testing.T) {
	store, slug := storeWithInstagram(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))

	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, slug, time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	n := e.NumberAtMeta
	if n.Quality.State != NotApplicable {
		t.Errorf("quality.state = %q, want %q", n.Quality.State, NotApplicable)
	}
	if n.MessageLimit.State != NotApplicable {
		t.Errorf("message_limit.state = %q, want %q", n.MessageLimit.State, NotApplicable)
	}
	// ASSERTED ABSENCE, like in token_instagram on a whatsapp instance: the
	// fields that only make sense with a real observation stay null.
	if n.Quality.Value != nil || n.Quality.ObservedAt != nil || n.Quality.Source != nil {
		t.Errorf("quality with a field filled on a not_applicable instance: %+v", n.Quality)
	}
	if n.MessageLimit.Value != nil || n.MessageLimit.ObservedAt != nil || n.MessageLimit.Source != nil {
		t.Errorf("message_limit with a field filled on a not_applicable instance: %+v", n.MessageLimit)
	}
	if n.CheckedAt != nil {
		t.Errorf("checked_at = %v, want nil — there is never an attempt to measure on an Instagram instance", n.CheckedAt)
	}
}

// THE FINDING FROM watchdog.go (T-099, required by the task before deciding by
// analogy): without the override in metaTokenInState, this test would prove
// that the consumer would see a PERMANENT `veredito: "recusado"` on a healthy
// Instagram instance — the watchdog RUNS for it (Check does not filter by
// type), but it measures by calling GET /{phone_number_id}, and Instagram
// never has a phone_number_id (ValidateInstanceType rejects the registration
// if it comes filled in). The call fails LOCALLY with ErrInvalidPhoneNumberID,
// with no network at all, and definitiveOutcome (watchdog.go) treats that error
// as a rejected credential.
func TestStateMetaTokenIsNotApplicableForInstagramEvenIfTheWatchdogMeasuredRefusedByMistake(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")
	watchdog := NewWatchdog(store, meta.NewClient(nil, "http://127.0.0.1:1"))

	watchdog.Check(context.Background())

	// PRE-CONDITION: proves the finding. Without it, the test below wouldn't
	// prove anything — we need the watchdog to have actually measured
	// "recusado" by mistake for the override to have something to fix.
	if read := watchdog.Read("insta-loja"); read.Verdict != VerdictRefused {
		t.Fatalf("finding's pre-condition: watchdog.Read = %q, want %q — without this the test does not exercise the override",
			read.Verdict, VerdictRefused)
	}

	e, err := BuildState(store, watchdog, nil, IngressSource{}, nil, nil, testVersion, "insta-loja", time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if e.MetaToken.Verdict != NotApplicable {
		t.Errorf("meta_token.verdict = %q, want %q — the finding from watchdog.go CANNOT leak to the consumer",
			e.MetaToken.Verdict, NotApplicable)
	}
	if e.MetaToken.MeasuredAt != nil || e.MetaToken.CheckedAt != nil || e.MetaToken.CheckFailingSince != nil {
		t.Errorf("meta_token with a timestamp filled on a not_applicable instance: %+v", e.MetaToken)
	}
}

// The SAME endpoint, in the real JSON — not just in the Go struct.
func TestGETStateInstagramNumberAtMetaAndMetaTokenAreNotApplicableInTheJSON(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")

	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	rec := askState(t, h, "token-do-a", "insta-loja")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		NumberAtMeta struct {
			Quality      map[string]any `json:"quality"`
			MessageLimit map[string]any `json:"message_limit"`
			CheckedAt    *string        `json:"checked_at"`
		} `json:"number_at_meta"`
		MetaToken struct {
			Verdict string `json:"verdict"`
		} `json:"meta_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	for name, block := range map[string]map[string]any{
		"quality": body.NumberAtMeta.Quality, "message_limit": body.NumberAtMeta.MessageLimit,
	} {
		if block["state"] != NotApplicable {
			t.Errorf("%s.state = %v, want %q", name, block["state"], NotApplicable)
		}
	}
	if body.NumberAtMeta.CheckedAt != nil {
		t.Errorf("number_at_meta.checked_at = %v, want null", *body.NumberAtMeta.CheckedAt)
	}
	if body.MetaToken.Verdict != NotApplicable {
		t.Errorf("meta_token.verdict = %q, want %q", body.MetaToken.Verdict, NotApplicable)
	}
}

// --- T-107: `tipo` and `ig_id` on /v1/estado — the same blindness as T-103, -----
// --- on the other surface. `IgID1` is the value storeWithInstagram /
// storeWithInstagramConsumer record for "insta-loja" (see both files).

func TestStatePublishesTypeAndIgIDWithValueForInstagram(t *testing.T) {
	store, slug := storeWithInstagram(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, slug, time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if e.Type != config.TypeInstagram {
		t.Errorf("tipo = %q, want %q", e.Type, config.TypeInstagram)
	}
	if e.IgID != "IGID1" {
		t.Errorf("ig_id = %q, want the registered value %q", e.IgID, "IGID1")
	}
}

func TestStatePublishesTypeAndIgIDAsNotApplicableForWhatsapp(t *testing.T) {
	store, _ := storeWithConsumer(t) // creates "lojinha", tipo=whatsapp
	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, "lojinha", time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if e.Type != config.TypeWhatsApp {
		t.Errorf("tipo = %q, want %q", e.Type, config.TypeWhatsApp)
	}
	if e.IgID != NotApplicable {
		t.Errorf("ig_id = %q, want %q — a whatsapp instance has no ig_id, and the absence has to be ASSERTED",
			e.IgID, NotApplicable)
	}
}

// The SAME, in the real JSON — not just in the Go struct (the same discipline
// the other blocks in this file already follow: see TestGETStateInstagramExposesInstagramTokenInTheJSON).
func TestGETStateInstagramExposesTypeAndIgIDInTheJSON(t *testing.T) {
	store, path := storeWithInstagramConsumer(t)
	activateInstance(t, path, "insta-loja")

	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	rec := askState(t, h, "token-do-a", "insta-loja")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	if body["kind"] != config.TypeInstagram {
		t.Errorf("tipo = %v, want %q", body["kind"], config.TypeInstagram)
	}
	if body["ig_id"] != "IGID1" {
		t.Errorf("ig_id = %v, want %q", body["ig_id"], "IGID1")
	}
}

func TestGETStateWhatsappExposesTypeAndIgIDAsNotApplicableInTheJSON(t *testing.T) {
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	rec := askState(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	if body["kind"] != config.TypeWhatsApp {
		t.Errorf("tipo = %v, want %q", body["kind"], config.TypeWhatsApp)
	}
	if body["ig_id"] != NotApplicable {
		t.Errorf("ig_id = %v, want %q — the field always has to come, never absent nor an empty string",
			body["ig_id"], NotApplicable)
	}
}

// --- (c) the VOCABULARY is the SAME in BOTH DIRECTIONS -------------------------
//
// Proof by CROSS-COMPARISON between the blocks, not by comparing each one
// against a literal written here: if someday someone touches numberAtMeta or
// metaTokenInState and swaps NotApplicable for a new term only in that spot
// ("indisponivel", "nao_aplicavel", even a "nao_se_aplica" with a different
// space), the cross-comparison below turns red EVEN IF the new term is
// reasonable on its own — the DIVERGENCE between the four findings is the
// defect this test exists to catch, exactly the shape of this project's
// mother pitfall ("the rule holds in one place and not in the next")
// applied to a single word.
func TestStateNotApplicableIsTheSameWordInBothSenses(t *testing.T) {
	// Direction 1 (T-098): instagram_token on a WHATSAPP instance.
	storeWA, _ := storeWithConsumer(t) // creates "lojinha", tipo=whatsapp
	eWA, err := BuildState(storeWA, testInertWatchdog(storeWA), nil, IngressSource{}, nil, nil, testVersion, "lojinha", time.Now())
	if err != nil {
		t.Fatalf("BuildState (whatsapp): %v", err)
	}

	// Direction 2 (T-099): number_at_meta and meta_token on an INSTAGRAM instance.
	storeIG, slugIG := storeWithInstagram(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	eIG, err := BuildState(storeIG, testInertWatchdog(storeIG), nil, IngressSource{}, nil, nil, testVersion, slugIG, time.Now())
	if err != nil {
		t.Fatalf("BuildState (instagram): %v", err)
	}

	findings := map[string]string{
		"instagram_token.verdict (whatsapp instance)":             eWA.InstagramToken.Verdict,
		"number_at_meta.quality.state (instagram instance)":       eIG.NumberAtMeta.Quality.State,
		"number_at_meta.message_limit.state (instagram instance)": eIG.NumberAtMeta.MessageLimit.State,
		"meta_token.verdict (instagram instance)":                 eIG.MetaToken.Verdict,
	}
	for label, value := range findings {
		if value != NotApplicable {
			t.Errorf("%s = %q, want %q — the TWO senses of the mother pitfall have to speak the SAME word "+
				"(compared against NotApplicable, the single source in internal/outbound/state.go)", label, value, NotApplicable)
		}
	}
	// AND THE FOUR AGAINST EACH OTHER, without depending on the constant: if
	// someday someone removes NotApplicable and writes loose literals, a test
	// that only compares against the constant could keep failing for the wrong
	// reason. This second round guarantees that the REAL FAILURE — divergence
	// between the blocks — is what makes the test flag it, even under that
	// hypothesis.
	firstOne := eWA.InstagramToken.Verdict
	for label, value := range findings {
		if value != firstOne {
			t.Errorf("%s = %q diverges from instagram_token.verdict (whatsapp) = %q — the two senses "+
				"of this pitfall have to use the SAME word between them", label, value, firstOne)
		}
	}
}
