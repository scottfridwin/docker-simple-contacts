package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

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
