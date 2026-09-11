package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

// TestDiscoverOIDCProviderRetriesTransientFailures guards a startup-race
// robustness fix: Authentik (or any OIDC provider) being briefly unreachable
// when the app starts must not permanently stop the whole app - including
// unrelated features like Person CRUD and Google sync - from starting.
func TestDiscoverOIDCProviderRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
		})
	}))
	defer srv.Close()

	cfg := oidcRetryConfig{initialBackoff: time.Millisecond, maxBackoff: 5 * time.Millisecond, maxWait: time.Second}
	provider, err := discoverOIDCProvider(context.Background(), srv.URL, slog.Default(), cfg)
	if err != nil {
		t.Fatalf("discoverOIDCProvider: %v", err)
	}
	if provider == nil {
		t.Fatal("expected a non-nil provider")
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestDiscoverOIDCProviderGivesUpAfterDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := oidcRetryConfig{initialBackoff: time.Millisecond, maxBackoff: 5 * time.Millisecond, maxWait: 50 * time.Millisecond}
	if _, err := discoverOIDCProvider(context.Background(), srv.URL, slog.Default(), cfg); err == nil {
		t.Fatal("expected error after exhausting retry budget")
	}
}
