package outbound

import "testing"

// TestSmokeTextHasPortugueseAccents pins the text a real person reads on
// their phone: dropping the accents is a defect, not a style choice.
func TestSmokeTextHasPortugueseAccents(t *testing.T) {
	const want = "zapgw: teste de fumaça do canal. Se você recebeu esta mensagem, o envio está funcionando. Não é preciso responder."
	if SmokeText != want {
		t.Fatalf("SmokeText = %q, want %q", SmokeText, want)
	}
}
