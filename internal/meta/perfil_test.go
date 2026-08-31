package meta

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadProfileBuildsTheRightPathAndFields(t *testing.T) {
	var path, query, authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		query = r.URL.RawQuery
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"about":"Loja de teste","address":"Rua Um, 1",
			"description":"Descricao","email":"a@b.com",
			"profile_picture_url":"https://exemplo/x.jpg",
			"websites":["https://a.com","https://b.com"],"vertical":"RETAIL"}]}`))
	}))
	defer srv.Close()

	p, err := testClient(srv).ReadProfile(context.Background(), "PNID1", "token-envio")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}

	if path != "/PNID1/whatsapp_business_profile" {
		t.Fatalf("caminho = %q, quero /PNID1/whatsapp_business_profile", path)
	}
	if !strings.Contains(query, "fields=") {
		t.Fatalf("query = %q, esperava fields=", query)
	}
	if authorization != "Bearer token-envio" {
		t.Fatalf("Authorization = %q", authorization)
	}
	if p.About != "Loja de teste" || p.Address != "Rua Um, 1" || p.Description != "Descricao" ||
		p.Email != "a@b.com" || p.ProfilePictureURL != "https://exemplo/x.jpg" || p.Vertical != "RETAIL" {
		t.Fatalf("perfil = %+v", p)
	}
	if len(p.Websites) != 2 || p.Websites[0] != "https://a.com" || p.Websites[1] != "https://b.com" {
		t.Fatalf("websites = %v", p.Websites)
	}
}

// TestReadProfileWithAnEmptyDataReturnsAnEmptyProfileWithoutError: this is NOT a read
// error — it's an instance with no profile field filled in. See
// profileFromResponse's comment.
func TestReadProfileWithAnEmptyDataReturnsAnEmptyProfileWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	p, err := testClient(srv).ReadProfile(context.Background(), "PNID1", "token")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if p.About != "" || p.Address != "" || p.Description != "" || p.Email != "" ||
		p.ProfilePictureURL != "" || p.Vertical != "" || p.Websites != nil {
		t.Fatalf("perfil = %+v, quero zero-value", p)
	}
}

func TestReadProfileWithAnUnreadableBodyReturnsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`nao e json`))
	}))
	defer srv.Close()

	_, err := testClient(srv).ReadProfile(context.Background(), "PNID1", "token")
	if !errors.Is(err, ErrProfileNotUnderstood) {
		t.Fatalf("err = %v, quero ErrProfileNotUnderstood", err)
	}
}

func TestReadProfileWithAnInvalidNodeRefusesWithoutCallingMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("a Meta nao devia ser chamada com um node invalido")
	}))
	defer srv.Close()

	_, err := testClient(srv).ReadProfile(context.Background(), "../fora", "token")
	if !errors.Is(err, ErrInvalidProfileNode) {
		t.Fatalf("err = %v, quero ErrInvalidProfileNode", err)
	}
}

// TestWriteProfileSendsOnlyThePresentFields is the MOST IMPORTANT test in
// this file (T-155, item 4): a field ABSENT in ProfilePatch cannot appear in
// the body sent to Meta — sending "" would erase a value already there.
// Only `about` was filled in; the body has to carry ONLY
// `messaging_product` and `about`, never `description`, `address`, etc.
func TestWriteProfileSendsOnlyThePresentFields(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	about := "Nova descricao curta"
	err := testClient(srv).WriteProfile(context.Background(), "PNID1", "token", ProfilePatch{
		About: &about,
	})
	if err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	const want = `{"messaging_product":"whatsapp","about":"Nova descricao curta"}`
	if receivedBody != want {
		t.Fatalf("corpo = %s\nquero  = %s\n(um campo ausente em ProfilePatch NAO pode aparecer no corpo — "+
			"aparecer apagaria o valor que a Meta ja guarda)", receivedBody, want)
	}
}

// TestWriteProfileWithAnEmptyWebsitesListErases proves the other side of
// the same distinction: a NON-nil pointer to an EMPTY list is an explicit
// request to erase, and has to come out as `"websites":[]`, not be left out
// of the body.
func TestWriteProfileWithAnEmptyWebsitesListErases(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	empty := []string{}
	err := testClient(srv).WriteProfile(context.Background(), "PNID1", "token", ProfilePatch{
		Websites: &empty,
	})
	if err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}
	const want = `{"messaging_product":"whatsapp","websites":[]}`
	if receivedBody != want {
		t.Fatalf("corpo = %s\nquero  = %s", receivedBody, want)
	}
}

func TestWriteProfileWithEveryFieldBuildsTheCompleteBody(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	about, address, description, email, vertical, handle := "Sobre", "Endereco", "Descricao", "a@b.com", "RETAIL", "MEDIA-HANDLE-1"
	sites := []string{"https://a.com"}
	err := testClient(srv).WriteProfile(context.Background(), "PNID1", "token", ProfilePatch{
		About: &about, Address: &address, Description: &description, Email: &email,
		Websites: &sites, Vertical: &vertical, ProfilePictureHandle: &handle,
	})
	if err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}
	const want = `{"messaging_product":"whatsapp","about":"Sobre","address":"Endereco",` +
		`"description":"Descricao","email":"a@b.com","websites":["https://a.com"],` +
		`"vertical":"RETAIL","profile_picture_handle":"MEDIA-HANDLE-1"}`
	if receivedBody != want {
		t.Fatalf("corpo = %s\nquero  = %s", receivedBody, want)
	}
}

func TestWriteProfileWithAnInvalidNodeRefusesWithoutCallingMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("a Meta nao devia ser chamada com um node invalido")
	}))
	defer srv.Close()

	about := "x"
	err := testClient(srv).WriteProfile(context.Background(), "../fora", "token", ProfilePatch{About: &about})
	if !errors.Is(err, ErrInvalidProfileNode) {
		t.Fatalf("err = %v, quero ErrInvalidProfileNode", err)
	}
}

func TestWriteProfilePassesOnMetasClassifiedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid parameter","code":100}}`))
	}))
	defer srv.Close()

	about := "x"
	err := testClient(srv).WriteProfile(context.Background(), "PNID1", "token", ProfilePatch{About: &about})
	var me *MetaError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, quero *MetaError", err)
	}
	if me.MetaCode != 100 {
		t.Fatalf("MetaCode = %d, quero 100", me.MetaCode)
	}
}
