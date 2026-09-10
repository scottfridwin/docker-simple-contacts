package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

func TestShareEndpointsCreateListDelete(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "Owner"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/shares", map[string]any{
		"email": "Friend@Example.com",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created shareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Email != "friend@example.com" {
		t.Errorf("created = %+v", created)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+a.ID.String()+"/shares", nil)
	var list struct {
		Data []shareResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data) != 1 || list.Data[0].Email != "friend@example.com" {
		t.Fatalf("shares list = %+v", list.Data)
	}

	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+a.ID.String()+"/shares/"+created.ID.String(), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+a.ID.String()+"/shares", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data) != 0 {
		t.Fatalf("expected no shares after delete, got %+v", list.Data)
	}
}

func TestShareEndpointsValidationAndNotFound(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "Owner"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/shares", map[string]any{
		"email": "notfound@example.com",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create with unknown email status = %d, body=%s", rec.Code, rec.Body.String())
	}

	missingID := "00000000-0000-0000-0000-000000000000"
	rec = doJSON(t, h, http.MethodPost, "/api/v1/persons/"+missingID+"/shares", map[string]any{"email": "friend@example.com"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("create for unknown person status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+missingID+"/shares", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("list for unknown person status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+missingID+"/shares/"+missingID, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete for unknown person status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestShareEndpointsInvalidIDs(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "Owner"})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/persons/not-a-uuid/shares"},
		{http.MethodPost, "/api/v1/persons/not-a-uuid/shares"},
		{http.MethodDelete, "/api/v1/persons/not-a-uuid/shares/" + uuid.NewString()},
		{http.MethodDelete, "/api/v1/persons/" + a.ID.String() + "/shares/not-a-uuid"},
		{http.MethodDelete, "/api/v1/persons/not-a-uuid/shares/mine"},
	} {
		rec := doJSON(t, h, tc.method, tc.path, map[string]any{"email": "friend@example.com"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", tc.method, tc.path, rec.Code)
		}
	}
}

func TestShareEndpointCreateMalformedJSON(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "Owner"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/shares", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// shareErrorStore lets the person exist (GetByID succeeds) but forces every
// share-specific store call to fail with a generic error, to exercise the
// internal_error branch that runs after the not-found/validation checks.
type shareErrorStore struct {
	*fakeStore
	err error
}

func (s *shareErrorStore) CreateShare(context.Context, uuid.UUID, string) (*person.Share, error) {
	return nil, s.err
}
func (s *shareErrorStore) ListShares(context.Context, uuid.UUID) ([]person.Share, error) {
	return nil, s.err
}
func (s *shareErrorStore) DeleteShare(context.Context, uuid.UUID, uuid.UUID) error {
	return s.err
}
func (s *shareErrorStore) DeleteShareByRecipient(context.Context, uuid.UUID) error {
	return s.err
}

func TestShareEndpointsReturnInternalErrors(t *testing.T) {
	store := &shareErrorStore{fakeStore: newFakeStore(), err: errors.New("database unavailable")}
	svc := person.NewService(store)
	h := NewRouter(slog.Default(), svc, store, nil, nil, nil)
	created, _, _ := svc.Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "Owner"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+created.ID.String()+"/shares", map[string]any{"email": "friend@example.com"})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("create status = %d, want 500", rec.Code)
	}
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+created.ID.String()+"/shares", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("list status = %d, want 500", rec.Code)
	}
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+created.ID.String()+"/shares/"+uuid.NewString(), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("delete status = %d, want 500", rec.Code)
	}
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+created.ID.String()+"/shares/mine", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("leave status = %d, want 500", rec.Code)
	}
}

// TestShareEndpointLeave guards the recipient self-unshare route: the
// recipient (identified via the authenticated context, not the owner) can
// remove their own access, a second attempt 404s, and an unrelated account
// can't remove someone else's share.
func TestShareEndpointLeave(t *testing.T) {
	h, store := testRouter()
	svc := person.NewService(store)
	a, _, _ := svc.Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "Owner"})
	created, _, err := svc.CreateShare(context.Background(), a.ID, "friend@example.com")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	leavePath := "/api/v1/persons/" + a.ID.String() + "/shares/mine"

	// An unrelated account (no share of its own) gets 404.
	req := httptest.NewRequest(http.MethodDelete, leavePath, nil)
	req = req.WithContext(authn.WithUserID(req.Context(), uuid.New()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unrelated account leave status = %d, want 404", rec.Code)
	}

	// The actual recipient can leave.
	req = httptest.NewRequest(http.MethodDelete, leavePath, nil)
	req = req.WithContext(authn.WithUserID(req.Context(), created.SharedWithUserID))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("recipient leave status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Leaving again (no longer shared) 404s.
	req = httptest.NewRequest(http.MethodDelete, leavePath, nil)
	req = req.WithContext(authn.WithUserID(req.Context(), created.SharedWithUserID))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("repeat leave status = %d, want 404", rec.Code)
	}
}
