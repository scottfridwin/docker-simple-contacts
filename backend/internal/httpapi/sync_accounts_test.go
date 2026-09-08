package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

type fakeSyncAccountStore struct {
	items map[uuid.UUID]*contactsync.Account
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

func (f *fakeSyncAccountStore) Create(_ context.Context, account *contactsync.Account) (*contactsync.Account, error) {
	cp := *account
	cp.ID = uuid.New()
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
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
	return NewRouter(logger, personSvc, personStore, newFakeSyncAccountStore(), []string{"http://localhost:5173"})
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
