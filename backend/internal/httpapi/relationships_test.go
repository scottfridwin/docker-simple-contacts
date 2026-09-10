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

	"github.com/scottfridlund/contacts/backend/internal/person"
)

func TestRelationshipEndpointsCreateListDelete(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "Parent"})
	b, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "B", LastName: "Child"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/relationships", map[string]any{
		"type":              "parent",
		"related_person_id": b.ID.String(),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created relationshipResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Type != "parent" || created.RelatedPersonName != "B Child" {
		t.Errorf("created = %+v", created)
	}

	// A's list shows Parent pointing at B.
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+a.ID.String()+"/relationships", nil)
	var aList struct {
		Data []relationshipResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &aList)
	if len(aList.Data) != 1 || aList.Data[0].Type != "parent" {
		t.Fatalf("A's relationships = %+v", aList.Data)
	}

	// B's list shows the computed inverse: Child pointing at A.
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+b.ID.String()+"/relationships", nil)
	var bList struct {
		Data []relationshipResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &bList)
	if len(bList.Data) != 1 || bList.Data[0].Type != "child" || bList.Data[0].RelatedPersonName != "A Parent" {
		t.Fatalf("B's relationships = %+v, want inverted child", bList.Data)
	}

	// Delete from B's (reverse) side removes it from both.
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+b.ID.String()+"/relationships/"+bList.Data[0].ID.String(), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+a.ID.String()+"/relationships", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &aList)
	if len(aList.Data) != 0 {
		t.Fatalf("expected relationship gone after delete, got %+v", aList.Data)
	}
}

func TestRelationshipEndpointRejectsInvalidType(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "One"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/relationships", map[string]any{
		"type":                "friend",
		"related_person_name": "Some Friend",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}
}

func TestRelationshipEndpointUnlinkedByName(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "One"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/relationships", map[string]any{
		"type":                "sibling",
		"related_person_name": "Unlinked Sibling",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created relationshipResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.RelatedPersonID != nil {
		t.Errorf("expected unlinked relationship to have nil related_person_id, got %v", *created.RelatedPersonID)
	}
	if created.RelatedPersonName != "Unlinked Sibling" {
		t.Errorf("RelatedPersonName = %q", created.RelatedPersonName)
	}
}

func TestRelationshipEndpoint404sForUnknownPerson(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodGet, "/api/v1/persons/"+uuidNil()+"/relationships", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func uuidNil() string {
	return "00000000-0000-0000-0000-000000000000"
}

func TestRelationshipEndpointsInvalidIDs(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "One"})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/persons/not-a-uuid/relationships"},
		{http.MethodPost, "/api/v1/persons/not-a-uuid/relationships"},
		{http.MethodDelete, "/api/v1/persons/not-a-uuid/relationships/" + uuidNil()},
		{http.MethodDelete, "/api/v1/persons/" + a.ID.String() + "/relationships/not-a-uuid"},
	} {
		rec := doJSON(t, h, tc.method, tc.path, map[string]any{"type": "sibling", "related_person_name": "X"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRelationshipEndpointCreateMalformedJSON(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "One"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/relationships", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestRelationshipEndpointCreateInvalidRelatedPersonID(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "One"})

	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+a.ID.String()+"/relationships", map[string]any{
		"type": "sibling", "related_person_id": "not-a-uuid",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestRelationshipEndpointCreate404sForUnknownPerson(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons/"+uuidNil()+"/relationships", map[string]any{
		"type": "sibling", "related_person_name": "X",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestRelationshipEndpointDelete404s(t *testing.T) {
	h, store := testRouter()
	a, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "One"})

	rec := doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+uuidNil()+"/relationships/"+uuidNil(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete for unknown person status = %d, want 404", rec.Code)
	}
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+a.ID.String()+"/relationships/"+uuidNil(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete for unknown relationship status = %d, want 404", rec.Code)
	}
}

// relationshipErrorStore lets the person exist (GetByID succeeds) but forces
// every relationship-specific store call to fail with a generic error, to
// exercise the internal_error branches.
type relationshipErrorStore struct {
	*fakeStore
	err error
}

func (s *relationshipErrorStore) CreateRelationship(context.Context, uuid.UUID, person.RelationshipInput) (*person.RelationshipView, error) {
	return nil, s.err
}
func (s *relationshipErrorStore) ListRelationships(context.Context, uuid.UUID) ([]person.RelationshipView, error) {
	return nil, s.err
}
func (s *relationshipErrorStore) DeleteRelationship(context.Context, uuid.UUID, uuid.UUID) error {
	return s.err
}

func TestRelationshipEndpointsReturnInternalErrors(t *testing.T) {
	store := &relationshipErrorStore{fakeStore: newFakeStore(), err: errors.New("database unavailable")}
	svc := person.NewService(store)
	h := NewRouter(slog.Default(), svc, store, nil, nil, nil)
	created, _, _ := svc.Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "One"})

	rec := doJSON(t, h, http.MethodGet, "/api/v1/persons/"+created.ID.String()+"/relationships", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("list status = %d, want 500", rec.Code)
	}
	rec = doJSON(t, h, http.MethodPost, "/api/v1/persons/"+created.ID.String()+"/relationships", map[string]any{"type": "sibling", "related_person_name": "X"})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("create status = %d, want 500", rec.Code)
	}
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+created.ID.String()+"/relationships/"+uuid.NewString(), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("delete status = %d, want 500", rec.Code)
	}
}
