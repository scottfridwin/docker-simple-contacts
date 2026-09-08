package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

func TestMiddlewareAcceptsSignedSession(t *testing.T) {
	p := &Provider{key: []byte("01234567890123456789012345678901")}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/persons", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: p.signSession(uuid.New())})
	rec := httptest.NewRecorder()
	p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authn.UserID(r.Context()); !ok {
			t.Error("expected authenticated user in request context")
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestMiddlewareRejectsUnsignedAPISession(t *testing.T) {
	p := &Provider{key: []byte("01234567890123456789012345678901")}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/persons", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "invalid"})
	rec := httptest.NewRecorder()
	p.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unauthenticated request reached handler")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
