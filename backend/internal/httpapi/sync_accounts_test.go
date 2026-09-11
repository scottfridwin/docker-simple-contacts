package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

type fakeSyncAccountStore struct {
	items     map[uuid.UUID]*contactsync.Account
	createErr error
}

func newFakeSyncAccountStore() *fakeSyncAccountStore {
	return &fakeSyncAccountStore{items: make(map[uuid.UUID]*contactsync.Account)}
}

func (f *fakeSyncAccountStore) List(_ context.Context, _ int) ([]contactsync.Account, error) {
	out := make([]contactsync.Account, 0, len(f.items))
	for _, item := range f.items {
		out = append(out, *item)
	}
	return out, nil
}

func (f *fakeSyncAccountStore) Create(ctx context.Context, account *contactsync.Account) (*contactsync.Account, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	cp := *account
	cp.ID = uuid.New()
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	// Mirror the real repository: owner_id comes from the request context,
	// not the caller-supplied struct.
	if ownerID, ok := authn.UserID(ctx); ok {
		cp.OwnerID = &ownerID
	}
	if cp.Status == "" {
		cp.Status = "connected"
	}
	if cp.SyncFrequencyMinutes == 0 {
		cp.SyncFrequencyMinutes = 60
	}
	f.items[cp.ID] = &cp
	return &cp, nil
}

func (f *fakeSyncAccountStore) GetByID(_ context.Context, id uuid.UUID) (*contactsync.Account, error) {
	item, ok := f.items[id]
	if !ok {
		return nil, contactsync.ErrNotFound
	}
	cp := *item
	return &cp, nil
}

func (f *fakeSyncAccountStore) Update(_ context.Context, account *contactsync.Account) (*contactsync.Account, error) {
	if _, ok := f.items[account.ID]; !ok {
		return nil, contactsync.ErrNotFound
	}
	cp := *account
	cp.UpdatedAt = time.Now()
	f.items[cp.ID] = &cp
	return &cp, nil
}

func (f *fakeSyncAccountStore) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := f.items[id]; !ok {
		return contactsync.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func testSyncRouter() http.Handler {
	personStore := newFakeStore()
	personSvc := person.NewService(personStore)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(logger, personSvc, personStore, newFakeSyncAccountStore(), nil, []string{"http://localhost:5173"})
}

func TestCreateSyncAccount(t *testing.T) {
	h := testSyncRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts", map[string]any{
		"provider":               "google",
		"provider_account_id":    "abc123",
		"sync_frequency_minutes": 30,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var account contactsync.Account
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	if account.Provider != "google" || account.ProviderAccountID != "abc123" {
		t.Fatalf("unexpected account: %+v", account)
	}
}

// TestSyncAccountJSONWireFormat guards against a regression where Account had
// no json tags: Go-to-Go round-trip tests can't catch that, since encoding and
// decoding into the same untagged struct is symmetric either way. Frontend
// clients need exact snake_case keys, and OAuth tokens must never leak to the
// browser.
func TestSyncAccountJSONWireFormat(t *testing.T) {
	h := testSyncRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts", map[string]any{
		"provider":               "google",
		"provider_account_id":    "abc123",
		"access_token":           "secret-access-token",
		"refresh_token":          "secret-refresh-token",
		"sync_frequency_minutes": 30,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "provider", "provider_account_id", "sync_frequency_minutes", "status", "sync_cursor", "created_at", "updated_at"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("response missing expected snake_case key %q: %s", key, rec.Body.String())
		}
	}
	for _, key := range []string{"ID", "ProviderAccountID", "SyncFrequencyMinutes", "access_token", "refresh_token", "AccessToken", "RefreshToken"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("response leaked unexpected key %q: %s", key, rec.Body.String())
		}
	}
	if body := rec.Body.String(); bytes.Contains([]byte(body), []byte("secret-access-token")) ||
		bytes.Contains([]byte(body), []byte("secret-refresh-token")) {
		t.Fatalf("response leaked oauth token material: %s", body)
	}
}

func TestCreateSyncAccountValidationError(t *testing.T) {
	h := testSyncRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts", map[string]any{
		"provider": "",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestListGetUpdateDeleteSyncAccountFlow(t *testing.T) {
	h := testSyncRouter()

	create := doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts", map[string]any{
		"provider":            "google",
		"provider_account_id": "acc-1",
		"status":              "connected",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", create.Code, create.Body.String())
	}
	var created contactsync.Account
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	list := doJSON(t, h, http.MethodGet, "/api/v1/sync-accounts", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	var lr syncAccountListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Data) != 1 {
		t.Fatalf("list length = %d, want 1", len(lr.Data))
	}

	get := doJSON(t, h, http.MethodGet, "/api/v1/sync-accounts/"+created.ID.String(), nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d", get.Code)
	}
	var fetched contactsync.Account
	if err := json.Unmarshal(get.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.ProviderAccountID != "acc-1" {
		t.Fatalf("unexpected account id: %+v", fetched)
	}

	update := doJSON(t, h, http.MethodPatch, "/api/v1/sync-accounts/"+created.ID.String(), map[string]any{
		"status":                 "reconnect_required",
		"sync_frequency_minutes": 120,
	})
	if update.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", update.Code, update.Body.String())
	}
	var updated contactsync.Account
	if err := json.Unmarshal(update.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != "reconnect_required" || updated.SyncFrequencyMinutes != 120 {
		t.Fatalf("unexpected updated account: %+v", updated)
	}

	deleteResp := doJSON(t, h, http.MethodDelete, "/api/v1/sync-accounts/"+created.ID.String(), nil)
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteResp.Code)
	}

	getAfterDelete := doJSON(t, h, http.MethodGet, "/api/v1/sync-accounts/"+created.ID.String(), nil)
	if getAfterDelete.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want 404", getAfterDelete.Code)
	}
}

// TestSyncNowTriggersBackgroundSync guards the on-demand "Sync now" action:
// POSTing to /sync-accounts/{id}/sync must call Adapter.Sync for that
// account and return immediately (202), not block until the sync itself
// finishes - a large/rate-limited pull can run far longer than a browser or
// proxy is willing to wait on one request.
func TestSyncNowTriggersBackgroundSync(t *testing.T) {
	store := newFakeSyncAccountStore()
	created, err := store.Create(context.Background(), &contactsync.Account{
		Provider:          "google",
		ProviderAccountID: "subject-1",
	})
	if err != nil {
		t.Fatalf("seeding account: %v", err)
	}
	done := make(chan struct{}, 1)
	adapter := &fakeGoogleAdapter{syncDone: done}
	personStore := newFakeStore()
	personSvc := person.NewService(personStore)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewRouter(logger, personSvc, personStore, store, adapter, []string{"http://localhost:5173"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts/"+created.ID.String()+"/sync", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected Adapter.Sync to be called in the background")
	}
	if adapter.syncCalls != 1 {
		t.Fatalf("syncCalls = %d, want 1", adapter.syncCalls)
	}
}

func TestSyncNowNotFoundAndUnconfigured(t *testing.T) {
	store := newFakeSyncAccountStore()
	personStore := newFakeStore()
	personSvc := person.NewService(personStore)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// No adapter configured (Google sync disabled entirely).
	h := NewRouter(logger, personSvc, personStore, store, nil, []string{"http://localhost:5173"})
	rec := doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts/"+uuid.New().String()+"/sync", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when no adapter is configured", rec.Code)
	}

	// Adapter configured, but the account id doesn't exist.
	h = NewRouter(logger, personSvc, personStore, store, &fakeGoogleAdapter{}, []string{"http://localhost:5173"})
	rec = doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts/"+uuid.New().String()+"/sync", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown account", rec.Code)
	}
}

func TestSyncAccountInvalidID(t *testing.T) {
	h := testSyncRouter()
	for _, req := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/sync-accounts/not-a-uuid", nil},
		{http.MethodPatch, "/api/v1/sync-accounts/not-a-uuid", map[string]any{"status": "connected"}},
		{http.MethodDelete, "/api/v1/sync-accounts/not-a-uuid", nil},
	} {
		rec := doJSON(t, h, req.method, req.path, req.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s status = %d, want 400", req.method, req.path, rec.Code)
		}
	}
}

func TestSyncAccountUpdateRejectsUnknownField(t *testing.T) {
	h := testSyncRouter()
	created := doJSON(t, h, http.MethodPost, "/api/v1/sync-accounts", map[string]any{
		"provider":            "google",
		"provider_account_id": "acc-2",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d", created.Code)
	}
	var account contactsync.Account
	_ = json.Unmarshal(created.Body.Bytes(), &account)

	rec := doJSON(t, h, http.MethodPatch, "/api/v1/sync-accounts/"+account.ID.String(), map[string]any{
		"unknown": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSyncAccountNotFoundPaths(t *testing.T) {
	h := testSyncRouter()
	id := uuid.New().String()
	for _, req := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/sync-accounts/" + id, nil},
		{http.MethodPatch, "/api/v1/sync-accounts/" + id, map[string]any{"status": "connected"}},
		{http.MethodDelete, "/api/v1/sync-accounts/" + id, nil},
	} {
		rec := doJSON(t, h, req.method, req.path, req.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404", req.method, req.path, rec.Code)
		}
	}
}

func TestDecodeSyncAccountUpdateAllFields(t *testing.T) {
	body := `{
		"provider":"google",
		"provider_account_id":"p-1",
		"access_token":"a",
		"refresh_token":"r",
		"expires_at":"2026-01-02T03:04:05Z",
		"scope":"contacts.read",
		"sync_cursor":"c1",
		"sync_frequency_minutes":45,
		"status":"connected"
	}`
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(body))
	out, err := decodeSyncAccountUpdate(httptest.NewRecorder(), req)
	if err != nil {
		t.Fatalf("decodeSyncAccountUpdate: %v", err)
	}
	if !out.ProviderSet || out.Provider == nil || *out.Provider != "google" {
		t.Fatalf("provider not decoded: %+v", out)
	}
	if !out.ProviderAccountIDSet || out.ProviderAccountID == nil || *out.ProviderAccountID != "p-1" {
		t.Fatalf("provider_account_id not decoded: %+v", out)
	}
	if !out.AccessTokenSet || out.AccessToken == nil || *out.AccessToken != "a" {
		t.Fatalf("access_token not decoded: %+v", out)
	}
	if !out.RefreshTokenSet || out.RefreshToken == nil || *out.RefreshToken != "r" {
		t.Fatalf("refresh_token not decoded: %+v", out)
	}
	if !out.ExpiresAtSet || out.ExpiresAt == nil {
		t.Fatalf("expires_at not decoded: %+v", out)
	}
	if !out.ScopeSet || out.Scope == nil || *out.Scope != "contacts.read" {
		t.Fatalf("scope not decoded: %+v", out)
	}
	if !out.SyncCursorSet || out.SyncCursor == nil || *out.SyncCursor != "c1" {
		t.Fatalf("cursor not decoded: %+v", out)
	}
	if !out.SyncFrequencyMinutesSet || out.SyncFrequencyMinutes == nil || *out.SyncFrequencyMinutes != 45 {
		t.Fatalf("frequency not decoded: %+v", out)
	}
	if !out.StatusSet || out.Status == nil || *out.Status != "connected" {
		t.Fatalf("status not decoded: %+v", out)
	}
}

func TestDecodeSyncAccountUpdateRejectsInvalidBodies(t *testing.T) {
	for _, body := range []string{
		`{"unknown":true}`,
		`{"sync_frequency_minutes":"bad"}`,
		`{"expires_at":"bad-date"}`,
		`not json`,
	} {
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(body))
		if _, err := decodeSyncAccountUpdate(httptest.NewRecorder(), req); err == nil {
			t.Fatalf("expected decode error for body %s", body)
		}
	}
}

func TestApplySyncAccountPatch(t *testing.T) {
	provider := "google"
	providerID := "acc"
	access := "token-a"
	refresh := "token-r"
	expires := time.Now().Add(time.Hour).UTC()
	scope := "contacts"
	cursor := "c-1"
	frequency := 15
	status := "connected"

	current := &contactsync.Account{}
	applySyncAccountPatch(current, syncAccountUpdateInput{
		Provider:                &provider,
		ProviderSet:             true,
		ProviderAccountID:       &providerID,
		ProviderAccountIDSet:    true,
		AccessToken:             &access,
		AccessTokenSet:          true,
		RefreshToken:            &refresh,
		RefreshTokenSet:         true,
		ExpiresAt:               &expires,
		ExpiresAtSet:            true,
		Scope:                   &scope,
		ScopeSet:                true,
		SyncCursor:              &cursor,
		SyncCursorSet:           true,
		SyncFrequencyMinutes:    &frequency,
		SyncFrequencyMinutesSet: true,
		Status:                  &status,
		StatusSet:               true,
	})

	if current.Provider != "google" || current.ProviderAccountID != "acc" || current.SyncCursor != "c-1" {
		t.Fatalf("patch did not apply expected values: %+v", current)
	}
	if current.AccessToken == nil || *current.AccessToken != "token-a" || current.RefreshToken == nil || *current.RefreshToken != "token-r" {
		t.Fatalf("token fields not applied: %+v", current)
	}
	if current.ExpiresAt == nil || !current.ExpiresAt.Equal(expires) || current.Scope != "contacts" || current.SyncFrequencyMinutes != 15 || current.Status != "connected" {
		t.Fatalf("metadata fields not applied: %+v", current)
	}
}
