package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

const googleStateCookie = "contacts_google_sync_state"

type googleOAuthHandler struct {
	repo    syncAccountStore
	adapter contactsync.Adapter
}

type googleBeginResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
}

func (h *googleOAuthHandler) begin(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.adapter == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "google sync adapter not configured", nil)
		return
	}
	state, err := randomStateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to initialize google oauth state", nil)
		return
	}
	redirectURI := strings.TrimSpace(r.URL.Query().Get("redirect_uri"))
	authRequest, err := h.adapter.BeginAuthorization(r.Context(), redirectURI, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	setOAuthStateCookie(w, state)
	writeJSON(w, http.StatusOK, googleBeginResponse{AuthorizationURL: authRequest.AuthorizationURL, State: state})
}

func (h *googleOAuthHandler) callback(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.adapter == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "google sync adapter not configured", nil)
		return
	}
	cookie, err := r.Cookie(googleStateCookie)
	if err != nil || !secureStateEqual(cookie.Value, r.URL.Query().Get("state")) {
		clearOAuthStateCookie(w)
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid oauth state", nil)
		return
	}
	clearOAuthStateCookie(w)

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "oauth callback code is required", nil)
		return
	}
	session, err := h.adapter.CompleteAuthorization(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	account, err := h.upsertGoogleAccount(r, session)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to store google sync account", nil)
		return
	}
	if err := h.adapter.Sync(r.Context(), *account, contactsync.Job{}); err != nil {
		writeError(w, http.StatusBadGateway, "sync_failed", err.Error(), nil)
		return
	}
	refreshed, err := h.repo.GetByID(r.Context(), account.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, account)
		return
	}
	writeJSON(w, http.StatusOK, refreshed)
}

func (h *googleOAuthHandler) upsertGoogleAccount(r *http.Request, session contactsync.AuthSession) (*contactsync.Account, error) {
	if strings.TrimSpace(session.AccessToken) == "" {
		return nil, errors.New("google access token is missing")
	}
	accounts, err := h.repo.List(r.Context(), 100)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if strings.EqualFold(accounts[i].Provider, "google") {
			accounts[i].ProviderAccountID = session.ProviderAccountID
			accounts[i].AccessToken = stringPtr(session.AccessToken)
			accounts[i].RefreshToken = stringPtr(session.RefreshToken)
			if !session.ExpiresAt.IsZero() {
				expiresAt := session.ExpiresAt.UTC()
				accounts[i].ExpiresAt = &expiresAt
			}
			accounts[i].Scope = session.Scope
			accounts[i].Status = "connected"
			accounts[i].LastError = nil
			return h.repo.Update(r.Context(), &accounts[i])
		}
	}
	account := &contactsync.Account{
		Provider:          "google",
		ProviderAccountID: session.ProviderAccountID,
		AccessToken:       stringPtr(session.AccessToken),
		RefreshToken:      stringPtr(session.RefreshToken),
		Scope:             session.Scope,
		Status:            "connected",
	}
	if !session.ExpiresAt.IsZero() {
		expiresAt := session.ExpiresAt.UTC()
		account.ExpiresAt = &expiresAt
	}
	return h.repo.Create(r.Context(), account)
}

func randomStateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func setOAuthStateCookie(w http.ResponseWriter, state string) {
	//nolint:gosec // state is non-sensitive nonce and is intentionally cookie-stored for CSRF check
	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((10 * time.Minute).Seconds()),
	})
}

func clearOAuthStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func secureStateEqual(expected, actual string) bool {
	if expected == "" || actual == "" {
		return false
	}
	if len(expected) != len(actual) {
		return false
	}
	var diff byte
	for i := range expected {
		diff |= expected[i] ^ actual[i]
	}
	return diff == 0
}

func stringPtr(value string) *string {
	v := value
	return &v
}
