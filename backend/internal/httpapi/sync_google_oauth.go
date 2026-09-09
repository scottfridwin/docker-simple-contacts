package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

const googleStateCookie = "contacts_google_sync_state"

// backgroundSyncTimeout bounds the async post-OAuth contact sync so a stuck
// provider call can't run forever.
const backgroundSyncTimeout = 10 * time.Minute

type googleOAuthHandler struct {
	repo    syncAccountStore
	adapter contactsync.Adapter
	logger  *slog.Logger
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

	// The initial contact sync can take much longer than a browser/proxy is
	// willing to wait on this request (observed as a Cloudflare 502), so run it
	// in the background and respond as soon as the account is connected. The
	// frontend polls/refreshes sync accounts to pick up the eventual result.
	h.runBackgroundSync(*account)

	if wantsHTML(r) {
		writeGoogleCallbackHTML(w, account)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (h *googleOAuthHandler) runBackgroundSync(account contactsync.Account) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), backgroundSyncTimeout)
		defer cancel()
		if err := h.adapter.Sync(ctx, account, contactsync.Job{}); err != nil && h.logger != nil {
			h.logger.Error("background google sync failed", "account_id", account.ID, "error", err)
		}
	}()
}

func (h *googleOAuthHandler) upsertGoogleAccount(r *http.Request, session contactsync.AuthSession) (*contactsync.Account, error) {
	if strings.TrimSpace(session.AccessToken) == "" {
		return nil, errors.New("google access token is missing")
	}
	if strings.TrimSpace(session.ProviderAccountID) == "" {
		return nil, errors.New("google account identifier is missing")
	}
	accounts, err := h.repo.List(r.Context(), 100)
	if err != nil {
		return nil, err
	}
	var displayName *string
	if session.DisplayName != "" {
		displayName = stringPtr(session.DisplayName)
	}
	// Match on the specific Google account (provider + subject), not just the
	// provider, so connecting a different Google account adds a new row
	// instead of overwriting whichever account happened to be connected.
	for i := range accounts {
		if strings.EqualFold(accounts[i].Provider, "google") &&
			accounts[i].ProviderAccountID == session.ProviderAccountID {
			if displayName != nil {
				accounts[i].DisplayName = displayName
			}
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
		DisplayName:       displayName,
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

func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return true
	}
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}

func writeGoogleCallbackHTML(w http.ResponseWriter, account *contactsync.Account) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(account)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>Google sync complete</title>
	<style>
		body { font-family: system-ui, -apple-system, Segoe UI, sans-serif; margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f9fafb; color: #111827; }
		main { max-width: 28rem; padding: 2rem; text-align: center; background: white; border: 1px solid #e5e7eb; border-radius: 1rem; box-shadow: 0 1rem 2.5rem rgba(15, 23, 42, 0.08); }
		p { line-height: 1.5; color: #374151; }
		.muted { color: #6b7280; font-size: 0.92rem; }
		button { font: inherit; padding: 0.7rem 1rem; border: 0; border-radius: 0.6rem; background: #1e3a8a; color: white; cursor: pointer; }
	</style>
</head>
<body>
	<main>
		<h1>Authorization complete</h1>
		<p>Your Google account is connected. Contacts are syncing in the background and will
		appear shortly.</p>
		<p class="muted">You can close this window and return to the app.</p>
		<button type="button" onclick="window.opener && window.opener.postMessage({type: 'google-sync-complete', account: %s}, window.location.origin); window.close();">Close window</button>
	</main>
	<script>
		(function () {
			try {
				if (window.opener && !window.opener.closed) {
					window.opener.postMessage({ type: 'google-sync-complete', account: %s }, window.location.origin);
				}
			} catch (e) {}
			window.setTimeout(function () {
				window.close();
			}, 800);
		}());
	</script>
</body>
</html>`, payload, payload)
}
