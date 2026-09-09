package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

type fakeGoogleAdapter struct {
	authRequest contactsync.AuthRequest
	authSession contactsync.AuthSession
	beginErr    error
	completeErr error
	syncErr     error
	syncCalls   int
	syncDone    chan struct{}
}

func (f *fakeGoogleAdapter) ProviderName() string { return "google" }

func (f *fakeGoogleAdapter) Capabilities() contactsync.Capabilities {
	return contactsync.Capabilities{}
}

func (f *fakeGoogleAdapter) BeginAuthorization(context.Context, string, string) (contactsync.AuthRequest, error) {
	if f.beginErr != nil {
		return contactsync.AuthRequest{}, f.beginErr
	}
	return f.authRequest, nil
}

func (f *fakeGoogleAdapter) CompleteAuthorization(context.Context, string) (contactsync.AuthSession, error) {
	if f.completeErr != nil {
		return contactsync.AuthSession{}, f.completeErr
	}
	return f.authSession, nil
}

func (f *fakeGoogleAdapter) RefreshAuthorization(context.Context, contactsync.AuthSession) (contactsync.AuthSession, error) {
	return contactsync.AuthSession{}, nil
}

func (f *fakeGoogleAdapter) ListChanges(context.Context, contactsync.AuthSession, string) (contactsync.ChangePage, error) {
	return contactsync.ChangePage{}, nil
}

func (f *fakeGoogleAdapter) FetchRecord(context.Context, contactsync.AuthSession, string) (contactsync.ProviderRecord, error) {
	return contactsync.ProviderRecord{}, nil
}

func (f *fakeGoogleAdapter) UpsertRecord(context.Context, contactsync.AuthSession, contactsync.Record) (contactsync.ProviderRecord, error) {
	return contactsync.ProviderRecord{}, nil
}

func (f *fakeGoogleAdapter) DeleteRecord(context.Context, contactsync.AuthSession, string) error {
	return nil
}

func (f *fakeGoogleAdapter) Sync(context.Context, contactsync.Account, contactsync.Job) error {
	f.syncCalls++
	if f.syncDone != nil {
		defer func() { f.syncDone <- struct{}{} }()
	}
	return f.syncErr
}

// awaitSync waits for the background sync goroutine triggered by the callback
// handler to finish, since the callback now responds before sync completes.
func awaitSync(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background sync to complete")
	}
}

func testSyncGoogleRouter(adapter contactsync.Adapter) http.Handler {
	personStore := newFakeStore()
	personSvc := person.NewService(personStore)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(logger, personSvc, personStore, newFakeSyncAccountStore(), adapter, []string{"http://localhost:5173"})
}

func TestGoogleOAuthBegin(t *testing.T) {
	adapter := &fakeGoogleAdapter{authRequest: contactsync.AuthRequest{AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth?state=abc", State: "abc"}}
	h := testSyncGoogleRouter(adapter)

	rec := doJSON(t, h, http.MethodGet, "/api/v1/sync/google/begin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected oauth state cookie")
	}
	var out googleBeginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.AuthorizationURL == "" || out.State == "" {
		t.Fatalf("unexpected begin payload: %+v", out)
	}
}

func TestGoogleOAuthCallbackInvalidState(t *testing.T) {
	adapter := &fakeGoogleAdapter{}
	h := testSyncGoogleRouter(adapter)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/google/callback?state=bad&code=ok", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleOAuthCallbackUpsertAndSync(t *testing.T) {
	adapter := &fakeGoogleAdapter{
		authRequest: contactsync.AuthRequest{AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth?state=abc", State: "abc"},
		authSession: contactsync.AuthSession{
			ProviderAccountID: "people/123",
			AccessToken:       "access-token",
			RefreshToken:      "refresh-token",
			ExpiresAt:         time.Now().Add(time.Hour).UTC(),
			Scope:             "https://www.googleapis.com/auth/contacts",
		},
		syncDone: make(chan struct{}, 1),
	}
	h := testSyncGoogleRouter(adapter)

	begin := doJSON(t, h, http.MethodGet, "/api/v1/sync/google/begin", nil)
	if begin.Code != http.StatusOK {
		t.Fatalf("begin status = %d body=%s", begin.Code, begin.Body.String())
	}
	cookies := begin.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected oauth cookie")
	}

	var beginPayload googleBeginResponse
	if err := json.Unmarshal(begin.Body.Bytes(), &beginPayload); err != nil {
		t.Fatal(err)
	}

	cbReq := httptest.NewRequest(http.MethodGet, "/api/v1/sync/google/callback?state="+beginPayload.State+"&code=valid", nil)
	cbReq.AddCookie(cookies[0])
	cbReq.Header.Set("Accept", "text/html")
	cbRec := httptest.NewRecorder()
	h.ServeHTTP(cbRec, cbReq)
	if cbRec.Code != http.StatusOK {
		t.Fatalf("callback status = %d body=%s", cbRec.Code, cbRec.Body.String())
	}
	if got := cbRec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q, want text/html", got)
	}
	if body := cbRec.Body.String(); !strings.Contains(body, "Authorization complete") {
		t.Fatalf("callback body missing completion message: %s", body)
	}
	awaitSync(t, adapter.syncDone)
	if adapter.syncCalls != 1 {
		t.Fatalf("sync calls = %d, want 1", adapter.syncCalls)
	}

	list := doJSON(t, h, http.MethodGet, "/api/v1/sync-accounts", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	var accounts syncAccountListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &accounts); err != nil {
		t.Fatal(err)
	}
	if len(accounts.Data) != 1 || accounts.Data[0].Provider != "google" {
		t.Fatalf("unexpected account list: %+v", accounts)
	}
}

// TestUpsertGoogleAccountDistinguishesAccounts guards the multi-account bug
// where connecting a second Google account overwrote the first one because
// matching was done by provider alone instead of provider + account subject.
func TestUpsertGoogleAccountDistinguishesAccounts(t *testing.T) {
	store := newFakeSyncAccountStore()
	h := &googleOAuthHandler{repo: store, adapter: &fakeGoogleAdapter{}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	first, err := h.upsertGoogleAccount(req, contactsync.AuthSession{
		ProviderAccountID: "sub-alice",
		DisplayName:       "alice@example.com",
		AccessToken:       "token-a",
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := h.upsertGoogleAccount(req, contactsync.AuthSession{
		ProviderAccountID: "sub-bob",
		DisplayName:       "bob@example.com",
		AccessToken:       "token-b",
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if first.ID == second.ID {
		t.Fatalf("expected two distinct accounts, got the same id for both: %s", first.ID)
	}
	accounts, err := store.List(req.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 stored accounts, got %d: %+v", len(accounts), accounts)
	}

	// Reconnecting the same Google account (same subject) must update the
	// existing row rather than creating a third one.
	reconnected, err := h.upsertGoogleAccount(req, contactsync.AuthSession{
		ProviderAccountID: "sub-alice",
		DisplayName:       "alice@example.com",
		AccessToken:       "token-a-refreshed",
	})
	if err != nil {
		t.Fatalf("reconnect upsert: %v", err)
	}
	if reconnected.ID != first.ID {
		t.Fatalf("expected reconnect to reuse account %s, got %s", first.ID, reconnected.ID)
	}
	accounts, err = store.List(req.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected still 2 stored accounts after reconnect, got %d", len(accounts))
	}
}

func TestGoogleOAuthCallbackReturnsJSONForApiClients(t *testing.T) {
	adapter := &fakeGoogleAdapter{
		authRequest: contactsync.AuthRequest{AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth?state=abc", State: "abc"},
		authSession: contactsync.AuthSession{
			ProviderAccountID: "people/123",
			AccessToken:       "access-token",
			Scope:             "https://www.googleapis.com/auth/contacts",
		},
	}
	h := testSyncGoogleRouter(adapter)

	begin := doJSON(t, h, http.MethodGet, "/api/v1/sync/google/begin", nil)
	var beginPayload googleBeginResponse
	_ = json.Unmarshal(begin.Body.Bytes(), &beginPayload)
	cookies := begin.Result().Cookies()
	cbReq := httptest.NewRequest(http.MethodGet, "/api/v1/sync/google/callback?state="+beginPayload.State+"&code=valid", nil)
	cbReq.AddCookie(cookies[0])
	cbReq.Header.Set("Accept", "application/json")
	cbRec := httptest.NewRecorder()
	h.ServeHTTP(cbRec, cbReq)
	if cbRec.Code != http.StatusOK {
		t.Fatalf("callback status = %d body=%s", cbRec.Code, cbRec.Body.String())
	}
	if got := cbRec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
	if !strings.Contains(cbRec.Body.String(), "people/123") {
		t.Fatalf("expected JSON response body, got %s", cbRec.Body.String())
	}
}

func TestGoogleOAuthCallbackSyncFailure(t *testing.T) {
	adapter := &fakeGoogleAdapter{
		authRequest: contactsync.AuthRequest{AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth?state=abc", State: "abc"},
		authSession: contactsync.AuthSession{ProviderAccountID: "people/123", AccessToken: "access-token"},
		syncErr:     context.DeadlineExceeded,
		syncDone:    make(chan struct{}, 1),
	}
	h := testSyncGoogleRouter(adapter)

	begin := doJSON(t, h, http.MethodGet, "/api/v1/sync/google/begin", nil)
	var beginPayload googleBeginResponse
	_ = json.Unmarshal(begin.Body.Bytes(), &beginPayload)
	cookies := begin.Result().Cookies()
	cbReq := httptest.NewRequest(http.MethodGet, "/api/v1/sync/google/callback?state="+beginPayload.State+"&code=valid", nil)
	cbReq.AddCookie(cookies[0])
	cbRec := httptest.NewRecorder()
	h.ServeHTTP(cbRec, cbReq)
	// The callback responds immediately once the account is connected; the
	// sync itself runs in the background so a downstream sync failure must
	// not fail the OAuth handshake response.
	if cbRec.Code != http.StatusOK {
		t.Fatalf("callback status = %d body=%s", cbRec.Code, cbRec.Body.String())
	}
	awaitSync(t, adapter.syncDone)
}
