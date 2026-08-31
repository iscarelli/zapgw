package meta

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestBlockUsersBuildsTheRightBody checks the exact body of POST
// <phone_number_id>/block_users, in the shape from the source cited at the
// top of block.go.
func TestBlockUsersBuildsTheRightBody(t *testing.T) {
	var receivedMethod, receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","block_users":{"added_users":[
			{"input":"5511999990000","wa_id":"5511999990000"}]}}`))
	}))
	defer srv.Close()

	result, err := testClient(srv).BlockUsers(context.Background(), "PNID1", "token", []string{"5511999990000"})
	if err != nil {
		t.Fatalf("BlockUsers: %v", err)
	}
	if receivedMethod != http.MethodPost {
		t.Fatalf("metodo = %s, quero POST", receivedMethod)
	}
	const want = `{"block_users":[{"user":"5511999990000"}],"messaging_product":"whatsapp"}`
	if receivedBody != want {
		t.Fatalf("corpo = %s\nquero  = %s", receivedBody, want)
	}
	if len(result.Succeeded) != 1 || result.Succeeded[0].Phone != "5511999990000" || result.Succeeded[0].WaID != "5511999990000" {
		t.Fatalf("Succeeded = %+v", result.Succeeded)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Failed = %+v, quero nenhuma", result.Failed)
	}
}

// TestUnblockUsersUsesDeleteWithTheSameBody is the mirror: SAME body,
// DELETE verb — the difference between BlockUsers and
// UnblockUsers is ONLY the HTTP method.
func TestUnblockUsersUsesDeleteWithTheSameBody(t *testing.T) {
	var receivedMethod, receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","block_users":{"removed_users":[
			{"input":"5511999990000","wa_id":"5511999990000"}]}}`))
	}))
	defer srv.Close()

	result, err := testClient(srv).UnblockUsers(context.Background(), "PNID1", "token", []string{"5511999990000"})
	if err != nil {
		t.Fatalf("UnblockUsers: %v", err)
	}
	if receivedMethod != http.MethodDelete {
		t.Fatalf("metodo = %s, quero DELETE", receivedMethod)
	}
	const want = `{"block_users":[{"user":"5511999990000"}],"messaging_product":"whatsapp"}`
	if receivedBody != want {
		t.Fatalf("corpo = %s\nquero  = %s", receivedBody, want)
	}
	if len(result.Succeeded) != 1 || result.Succeeded[0].WaID != "5511999990000" {
		t.Fatalf("Succeeded = %+v", result.Succeeded)
	}
}

// TestBlockUsersPartialSuccessComesPerNumber IS THE MOST IMPORTANT TEST
// IN THIS FILE (T-148, item 3): Meta can answer `200` at the ENVELOPE and
// still reject some numbers INSIDE it. A client that collapsed this into
// "it worked" (because the STATUS was 200) would make the consumer record
// "blocked" for someone who wasn't — the silent failure this entire project
// exists to prevent.
func TestBlockUsersPartialSuccessComesPerNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // the ENVELOPE is 200, despite item 2's failure
		_, _ = w.Write([]byte(`{
			"messaging_product":"whatsapp",
			"block_users":{
				"added_users":[{"input":"5511999990000","wa_id":"5511999990000"}],
				"failed_users":[{"input":"5511999990001","wa_id":"5511999990001","errors":[
					{"message":"nao mandou mensagem nas ultimas 24h","code":139001,
					 "error_data":{"details":"janela de 24h fechada"}}
				]}]
			}
		}`))
	}))
	defer srv.Close()

	result, err := testClient(srv).BlockUsers(context.Background(), "PNID1", "token",
		[]string{"5511999990000", "5511999990001"})
	if err != nil {
		t.Fatalf("BlockUsers nao pode devolver erro para um 200 com sucesso parcial: %v", err)
	}
	if len(result.Succeeded) != 1 || result.Succeeded[0].Phone != "5511999990000" {
		t.Fatalf("Succeeded = %+v, quero exatamente 5511999990000", result.Succeeded)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("Failed = %+v, quero exatamente uma", result.Failed)
	}
	f := result.Failed[0]
	if f.Phone != "5511999990001" || f.MetaCode != 139001 ||
		f.Message != "nao mandou mensagem nas ultimas 24h" || f.Detail != "janela de 24h fechada" {
		t.Fatalf("Failed[0] = %+v", f)
	}
}

// TestBlockUsersAnErrorEnvelopeReturnsAMetaError checks the OTHER case:
// the WHOLE call fails (4xx/5xx at the envelope) — here NO number was
// processed, and the difference from partial success has to stay visible:
// the caller gets an error, not an empty BlockResult.
func TestBlockUsersAnErrorEnvelopeReturnsAMetaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"token invalido","code":190}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv).BlockUsers(context.Background(), "PNID1", "token", []string{"5511999990000"})
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, quero *MetaError", err)
	}
	if me.Class != ClassConfig || me.MetaCode != 190 {
		t.Fatalf("MetaError = %+v", me)
	}
}

// TestListBlocksPassesOnTheCursors checks that limit/after/before reach
// Meta's query and that the response's cursors come back to the caller.
func TestListBlocksPassesOnTheCursors(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("metodo = %s, quero GET", r.Method)
		}
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[{"messaging_product":"whatsapp","wa_id":"5511999990000"}],
			"paging":{"cursors":{"after":"CURSOR_DEPOIS","before":"CURSOR_ANTES"}}
		}`))
	}))
	defer srv.Close()

	page, err := testClient(srv).ListBlocks(context.Background(), "PNID1", "token", 50, "APOS", "ANTES")
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	q, err := url.ParseQuery(receivedQuery)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", receivedQuery, err)
	}
	if q.Get("limit") != "50" || q.Get("after") != "APOS" || q.Get("before") != "ANTES" {
		t.Fatalf("query = %s", receivedQuery)
	}
	if len(page.Items) != 1 || page.Items[0].WaID != "5511999990000" {
		t.Fatalf("Items = %+v", page.Items)
	}
	if page.CursorAfter != "CURSOR_DEPOIS" || page.CursorBefore != "CURSOR_ANTES" {
		t.Fatalf("cursores = depois=%q antes=%q", page.CursorAfter, page.CursorBefore)
	}
}

// TestListBlocksWithoutParametersSendsNoQuery: limit<=0 and empty
// after/before don't become any parameter at all — Meta decides the
// default.
func TestListBlocksWithoutParametersSendsNoQuery(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"paging":{"cursors":{}}}`))
	}))
	defer srv.Close()

	if _, err := testClient(srv).ListBlocks(context.Background(), "PNID1", "token", 0, "", ""); err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if receivedQuery != "" {
		t.Fatalf("query = %q, quero vazia", receivedQuery)
	}
}
