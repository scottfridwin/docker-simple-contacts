package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/user"
)

const (
	sessionCookie = "contacts_session"
	stateCookie   = "contacts_oidc_state"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	SessionKey   []byte
}

type Provider struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	users    userStore
	key      []byte
}

type userStore interface {
	Upsert(context.Context, string, string, string) (*user.User, error)
}

func New(ctx context.Context, cfg Config, users userStore) (*Provider, error) {
	if len(cfg.SessionKey) < 32 {
		return nil, errors.New("SESSION_SECRET must be at least 32 bytes")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC provider: %w", err)
	}
	return &Provider{
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		users:    users,
		key:      cfg.SessionKey,
	}, nil
}

func (p *Provider) Routes(r interface {
	Get(string, http.HandlerFunc)
	Post(string, http.HandlerFunc)
}) {
	r.Get("/auth/login", p.login)
	r.Get("/auth/callback", p.callback)
	r.Post("/auth/logout", p.logout)
}

func (p *Provider) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := p.readSession(r)
		if !ok {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(authn.WithUserID(r.Context(), id)))
	})
}

func (p *Provider) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "failed to create login state", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		http.Error(w, "failed to create login nonce", http.StatusInternalServerError)
		return
	}
	setCookie(w, stateCookie, state, true, 10*time.Minute)
	setCookie(w, "contacts_oidc_nonce", nonce, true, 10*time.Minute)
	url := p.oauth.AuthCodeURL(state, oidc.Nonce(nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

func (p *Provider) callback(w http.ResponseWriter, r *http.Request) {
	stateCookieValue, err := r.Cookie(stateCookie)
	if err != nil || !hmac.Equal([]byte(stateCookieValue.Value), []byte(r.URL.Query().Get("state"))) {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	nonceCookie, err := r.Cookie("contacts_oidc_nonce")
	if err != nil {
		http.Error(w, "invalid login nonce", http.StatusBadRequest)
		return
	}
	token, err := p.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "login response missing ID token", http.StatusUnauthorized)
		return
	}
	idToken, err := p.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "invalid ID token", http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != nonceCookie.Value {
		http.Error(w, "invalid login nonce", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Subject   string `json:"sub"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		Preferred string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "invalid identity claims", http.StatusUnauthorized)
		return
	}
	if claims.Subject == "" {
		http.Error(w, "identity is missing subject", http.StatusUnauthorized)
		return
	}
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.Preferred
	}
	u, err := p.users.Upsert(r.Context(), claims.Subject, claims.Email, displayName)
	if err != nil {
		http.Error(w, "failed to save account", http.StatusInternalServerError)
		return
	}
	setCookie(w, sessionCookie, p.signSession(u.ID), true, 24*time.Hour)
	clearCookie(w, stateCookie)
	clearCookie(w, "contacts_oidc_nonce")
	http.Redirect(w, r, "/", http.StatusFound)
}

func (p *Provider) logout(w http.ResponseWriter, _ *http.Request) {
	clearCookie(w, sessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Provider) signSession(id uuid.UUID) string {
	exp := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf("%s.%d", id, exp)
	mac := hmac.New(sha256.New, p.key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "." + hex.EncodeToString(mac.Sum(nil))))
}

func (p *Provider) readSession(r *http.Request) (uuid.UUID, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return uuid.Nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return uuid.Nil, false
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(parts[0])
	exp, errExp := strconv.ParseInt(parts[1], 10, 64)
	if errExp != nil || err != nil || time.Now().Unix() >= exp {
		return uuid.Nil, false
	}
	mac := hmac.New(sha256.New, p.key)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected, err := hex.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, mac.Sum(nil)) {
		return uuid.Nil, false
	}
	return id, true
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func setCookie(w http.ResponseWriter, name, value string, httpOnly bool, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: httpOnly, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds())})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "unauthorized", "message": "authentication required"}})
}
