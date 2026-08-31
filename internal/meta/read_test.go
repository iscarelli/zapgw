package meta

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMarkAsReadWithoutTypingSendsTheUsualBody is the NON-REGRESSION for
// T-147 (same discipline as T-137): `MarkAsRead` has already been in
// production since T-075, and the body WITHOUT `digitando` has to come out
// BYTE FOR BYTE identical to today's. The JSON below is FROZEN as a literal —
// the alphabetical order of the keys is decided by encoding/json itself
// (message_id, messaging_product, status), both for the old map[string]string
// and for today's map[string]any.
func TestMarkAsReadWithoutTypingSendsTheUsualBody(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	err := testClient(srv).MarkAsRead(context.Background(), "PNID1", "token", "wamid.TESTE-DIGITANDO", false)
	if err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}

	const want = `{"message_id":"wamid.TESTE-DIGITANDO","messaging_product":"whatsapp","status":"read"}`
	if receivedBody != want {
		t.Fatalf("corpo = %s\nquero  = %s\n(digitando=false NAO pode mudar o corpo de uma chamada ja em producao)",
			receivedBody, want)
	}
}

// TestMarkAsReadWithTypingAddsTheIndicator is the positive finding:
// digitando=true adds `typing_indicator.type == "text"` to the SAME body,
// without removing or swapping any of the usual three fields (source:
// developers.facebook.com/docs/whatsapp/cloud-api/typing-indicators, read
// 2026-08-20).
func TestMarkAsReadWithTypingAddsTheIndicator(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	err := testClient(srv).MarkAsRead(context.Background(), "PNID1", "token", "wamid.TESTE-DIGITANDO", true)
	if err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}

	const want = `{"message_id":"wamid.TESTE-DIGITANDO","messaging_product":"whatsapp","status":"read","typing_indicator":{"type":"text"}}`
	if receivedBody != want {
		t.Fatalf("corpo = %s\nquero  = %s", receivedBody, want)
	}
}
