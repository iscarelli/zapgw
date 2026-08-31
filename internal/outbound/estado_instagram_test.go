// Tests for the `token_instagram` block of GET /v1/estado (T-098) — the
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

// --- nao_se_aplica: WHATSAPP instance -----------------------------------

func TestStateInstagramTokenIsNotApplicableForWhatsapp(t *testing.T) {
	store, _ := storeWithConsumer(t) // creates "lojinha", tipo=whatsapp
	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, "lojinha", time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	ti := e.InstagramToken
	if ti.Verdict != VerdictIGTokenNotApplicable {
		t.Errorf("veredito = %q, quero %q", ti.Verdict, VerdictIGTokenNotApplicable)
	}
	// ASSERTED ABSENCE: the verdict says "does not apply", and the date fields
	// stay null — never a number calculated over a deadline that doesn't exist.
	if ti.SetAt != nil || ti.ExpiresAt != nil || ti.DaysLeft != nil || ti.RenewedAt != nil || ti.FailingSince != nil {
		t.Errorf("campo de data/renovacao preenchido numa instancia nao_se_aplica: %+v", ti)
	}
	if ti.Instruction != nil {
		t.Errorf("instrucao presente sem problema nenhum: %q", *ti.Instruction)
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
		t.Errorf("veredito = %q, quero %q", ti.Verdict, VerdictIGTokenWaiting)
	}
	if ti.SetAt == nil || *ti.SetAt != setAt.Format(time.RFC3339) {
		t.Errorf("SetAt = %v, quero %s", ti.SetAt, setAt.Format(time.RFC3339))
	}
	want := setAt.Add(InstagramTokenValidity).Format(time.RFC3339)
	if ti.ExpiresAt == nil || *ti.ExpiresAt != want {
		t.Errorf("ExpiresAt = %v, quero %s", ti.ExpiresAt, want)
	}
	if ti.DaysLeft == nil || *ti.DaysLeft != 51 { // 60 - 9
		t.Errorf("DaysLeft = %v, quero 51", ti.DaysLeft)
	}
	if ti.RenewedAt != nil {
		t.Errorf("RenewedAt = %v, quero nil — o laco nunca renovou este token", ti.RenewedAt)
	}
	if ti.Instruction != nil {
		t.Errorf("instrucao presente sem problema nenhum: %q", *ti.Instruction)
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
		t.Errorf("veredito = %q, quero %q — o laco ja renovou com sucesso", ti.Verdict, VerdictIGTokenOK)
	}
	if ti.RenewedAt == nil || *ti.RenewedAt != renewedAt.Format(time.RFC3339) {
		t.Errorf("RenewedAt = %v, quero %s", ti.RenewedAt, renewedAt.Format(time.RFC3339))
	}
	if ti.Instruction != nil {
		t.Errorf("instrucao presente sem problema nenhum: %q", *ti.Instruction)
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
		t.Errorf("veredito = %q, quero %q", ti.Verdict, VerdictIGTokenFailing)
	}
	if ti.FailingSince == nil || *ti.FailingSince != failingSince.Format(time.RFC3339) {
		t.Errorf("FailingSince = %v, quero %s", ti.FailingSince, failingSince.Format(time.RFC3339))
	}
	// THE CENTRAL ASSERTION OF THE OWNER'S REQUEST: the "falhando" LABEL is not
	// enough — the INSTRUCTION of what to do has to come with it, and it has to
	// say that the resolution is MANUAL (the consumer does not have the token
	// to fix it alone).
	if ti.Instruction == nil {
		t.Fatal("Instruction ausente com veredito falhando — o consumidor nao tem como saber o que fazer")
	}
	if !strings.Contains(*ti.Instruction, "MANUAL") {
		t.Errorf("Instruction = %q, esperava mencionar que a resolucao e MANUAL", *ti.Instruction)
	}
	if *ti.Instruction != InstructionIGTokenFailing {
		t.Errorf("Instruction = %q, quero a constante InstructionIGTokenFailing", *ti.Instruction)
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
		t.Errorf("veredito = %q, quero %q", ti.Verdict, VerdictIGTokenExpired)
	}
	if ti.DaysLeft == nil || *ti.DaysLeft >= 0 {
		t.Errorf("DaysLeft = %v, quero negativo (ja passou do prazo)", ti.DaysLeft)
	}
	if ti.Instruction == nil {
		t.Fatal("Instruction ausente com veredito expirado")
	}
	if !strings.Contains(strings.ToLower(*ti.Instruction), "manual") {
		t.Errorf("Instruction = %q, esperava mencionar login MANUAL na Meta", *ti.Instruction)
	}
	if *ti.Instruction != InstructionIGTokenExpired {
		t.Errorf("Instruction = %q, quero a constante InstructionIGTokenExpired", *ti.Instruction)
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
		t.Errorf("veredito = %q, quero %q (expirado vence falhando)", e.InstagramToken.Verdict, VerdictIGTokenExpired)
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
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON invalido: %v\n%s", err, rec.Body.String())
	}
	ti, ok := body["token_instagram"].(map[string]any)
	if !ok {
		t.Fatalf("token_instagram ausente ou de outro tipo no JSON: %v", body["token_instagram"])
	}
	if ti["veredito"] != VerdictIGTokenWaiting {
		t.Errorf("veredito = %v, quero %q", ti["veredito"], VerdictIGTokenWaiting)
	}
	if ti["definido_em"] == nil {
		t.Error("definido_em ausente — a instancia insta-loja tem token desde a criacao")
	}
	if ti["expira_em"] == nil {
		t.Error("expira_em ausente")
	}
	if _, has := ti["dias_restantes"]; !has {
		t.Error("dias_restantes ausente")
	}
}

// And the SAME endpoint, on a WHATSAPP instance, has to say nao_se_aplica IN
// THE REAL JSON — not just in the Go struct.
func TestGETStateWhatsappInstagramTokenIsNotApplicableInTheJSON(t *testing.T) {
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	rec := askState(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON invalido: %v\n%s", err, rec.Body.String())
	}
	ti, ok := body["token_instagram"].(map[string]any)
	if !ok {
		t.Fatalf("token_instagram ausente ou de outro tipo no JSON: %v", body["token_instagram"])
	}
	if ti["veredito"] != VerdictIGTokenNotApplicable {
		t.Errorf("veredito = %v, quero %q — token_instagram NAO PODE sumir nem vir zerado numa instancia whatsapp",
			ti["veredito"], VerdictIGTokenNotApplicable)
	}
}

// --- T-099: nao_se_aplica in the REVERSE DIRECTION — WhatsApp blocks on an ------
// --- INSTAGRAM instance. It's the SAME mother pitfall that T-098 closed on one
// side (above) and left open on the other: measured in production (tenant-two-ig,
// v0.36.0, 2026-07-30 21:11), numero_na_meta said `nunca_observado` — the
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
		t.Errorf("qualidade.estado = %q, quero %q", n.Quality.State, NotApplicable)
	}
	if n.MessageLimit.State != NotApplicable {
		t.Errorf("limite_de_mensagens.estado = %q, quero %q", n.MessageLimit.State, NotApplicable)
	}
	// ASSERTED ABSENCE, like in token_instagram on a whatsapp instance: the
	// fields that only make sense with a real observation stay null.
	if n.Quality.Value != nil || n.Quality.ObservedAt != nil || n.Quality.Source != nil {
		t.Errorf("qualidade com campo preenchido numa instancia nao_se_aplica: %+v", n.Quality)
	}
	if n.MessageLimit.Value != nil || n.MessageLimit.ObservedAt != nil || n.MessageLimit.Source != nil {
		t.Errorf("limite_de_mensagens com campo preenchido numa instancia nao_se_aplica: %+v", n.MessageLimit)
	}
	if n.CheckedAt != nil {
		t.Errorf("conferido_em = %v, quero nil — nunca ha tentativa de medir numa instancia Instagram", n.CheckedAt)
	}
}

// THE FINDING FROM vigia.go (T-099, required by the task before deciding by
// analogy): without the override in metaTokenInState, this test would prove
// that the consumer would see a PERMANENT `veredito: "recusado"` on a healthy
// Instagram instance — the watchdog RUNS for it (Check does not filter by
// type), but it measures by calling GET /{phone_number_id}, and Instagram
// never has a phone_number_id (ValidateInstanceType rejects the registration
// if it comes filled in). The call fails LOCALLY with ErrInvalidPhoneNumberID,
// with no network at all, and definitiveOutcome (vigia.go) treats that error
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
		t.Fatalf("pre-condicao do achado: vigia.Read = %q, quero %q — sem isto o teste nao exercita o override",
			read.Verdict, VerdictRefused)
	}

	e, err := BuildState(store, watchdog, nil, IngressSource{}, nil, nil, testVersion, "insta-loja", time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if e.MetaToken.Verdict != NotApplicable {
		t.Errorf("token_meta.veredito = %q, quero %q — o achado do vigia.go NAO PODE vazar para o consumidor",
			e.MetaToken.Verdict, NotApplicable)
	}
	if e.MetaToken.MeasuredAt != nil || e.MetaToken.CheckedAt != nil || e.MetaToken.CheckFailingSince != nil {
		t.Errorf("token_meta com carimbo preenchido numa instancia nao_se_aplica: %+v", e.MetaToken)
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
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		NumberAtMeta struct {
			Quality      map[string]any `json:"qualidade"`
			MessageLimit map[string]any `json:"limite_de_mensagens"`
			CheckedAt    *string        `json:"conferido_em"`
		} `json:"numero_na_meta"`
		MetaToken struct {
			Verdict string `json:"veredito"`
		} `json:"token_meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON invalido: %v\n%s", err, rec.Body.String())
	}
	for name, block := range map[string]map[string]any{
		"qualidade": body.NumberAtMeta.Quality, "limite_de_mensagens": body.NumberAtMeta.MessageLimit,
	} {
		if block["estado"] != NotApplicable {
			t.Errorf("%s.estado = %v, quero %q", name, block["estado"], NotApplicable)
		}
	}
	if body.NumberAtMeta.CheckedAt != nil {
		t.Errorf("numero_na_meta.conferido_em = %v, quero null", *body.NumberAtMeta.CheckedAt)
	}
	if body.MetaToken.Verdict != NotApplicable {
		t.Errorf("token_meta.veredito = %q, quero %q", body.MetaToken.Verdict, NotApplicable)
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
		t.Errorf("tipo = %q, quero %q", e.Type, config.TypeInstagram)
	}
	if e.IgID != "IGID1" {
		t.Errorf("ig_id = %q, quero o valor cadastrado %q", e.IgID, "IGID1")
	}
}

func TestStatePublishesTypeAndIgIDAsNotApplicableForWhatsapp(t *testing.T) {
	store, _ := storeWithConsumer(t) // creates "lojinha", tipo=whatsapp
	e, err := BuildState(store, testInertWatchdog(store), nil, IngressSource{}, nil, nil, testVersion, "lojinha", time.Now())
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if e.Type != config.TypeWhatsApp {
		t.Errorf("tipo = %q, quero %q", e.Type, config.TypeWhatsApp)
	}
	if e.IgID != NotApplicable {
		t.Errorf("ig_id = %q, quero %q — instancia whatsapp nao tem ig_id, e a ausencia tem de ser AFIRMADA",
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
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON invalido: %v\n%s", err, rec.Body.String())
	}
	if body["tipo"] != config.TypeInstagram {
		t.Errorf("tipo = %v, quero %q", body["tipo"], config.TypeInstagram)
	}
	if body["ig_id"] != "IGID1" {
		t.Errorf("ig_id = %v, quero %q", body["ig_id"], "IGID1")
	}
}

func TestGETStateWhatsappExposesTypeAndIgIDAsNotApplicableInTheJSON(t *testing.T) {
	store, path := storeWithConsumer(t)
	activateInstance(t, path, "lojinha")

	h := NewStateHandler(store, NewAuthenticator(store), testInertWatchdog(store), nil,
		IngressSource{}, nil, nil, testVersion, config.DefaultRetentionDays, config.NewCounter(store), AllTypes)

	rec := askState(t, h, "token-do-a", "lojinha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON invalido: %v\n%s", err, rec.Body.String())
	}
	if body["tipo"] != config.TypeWhatsApp {
		t.Errorf("tipo = %v, quero %q", body["tipo"], config.TypeWhatsApp)
	}
	if body["ig_id"] != NotApplicable {
		t.Errorf("ig_id = %v, quero %q — o campo tem de vir SEMPRE, nunca ausente nem string vazia",
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
	// Direction 1 (T-098): token_instagram on a WHATSAPP instance.
	storeWA, _ := storeWithConsumer(t) // creates "lojinha", tipo=whatsapp
	eWA, err := BuildState(storeWA, testInertWatchdog(storeWA), nil, IngressSource{}, nil, nil, testVersion, "lojinha", time.Now())
	if err != nil {
		t.Fatalf("BuildState (whatsapp): %v", err)
	}

	// Direction 2 (T-099): numero_na_meta and token_meta on an INSTAGRAM instance.
	storeIG, slugIG := storeWithInstagram(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	eIG, err := BuildState(storeIG, testInertWatchdog(storeIG), nil, IngressSource{}, nil, nil, testVersion, slugIG, time.Now())
	if err != nil {
		t.Fatalf("BuildState (instagram): %v", err)
	}

	findings := map[string]string{
		"token_instagram.veredito (instancia whatsapp)":               eWA.InstagramToken.Verdict,
		"numero_na_meta.qualidade.estado (instancia instagram)":       eIG.NumberAtMeta.Quality.State,
		"numero_na_meta.limite_de_mensagens.estado (inst. instagram)": eIG.NumberAtMeta.MessageLimit.State,
		"token_meta.veredito (instancia instagram)":                   eIG.MetaToken.Verdict,
	}
	for label, value := range findings {
		if value != NotApplicable {
			t.Errorf("%s = %q, quero %q — os DOIS sentidos da armadilha-mae tem de falar a MESMA palavra "+
				"(comparado contra NotApplicable, a fonte unica de internal/outbound/estado.go)", label, value, NotApplicable)
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
			t.Errorf("%s = %q diverge de token_instagram.veredito (whatsapp) = %q — os dois sentidos "+
				"desta armadilha tem de usar a MESMA palavra entre si", label, value, firstOne)
		}
	}
}
