package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/person"
)

// fakeStore is an in-memory person store for endpoint tests.
type fakeStore struct {
	items         map[uuid.UUID]*person.Person
	relationships []fakeRelationship
}

type fakeRelationship struct {
	id              uuid.UUID
	personID        uuid.UUID
	relatedPersonID *uuid.UUID
	relatedName     *string
	relType         person.RelationType
}

type errorStore struct {
	*fakeStore
	err error
}

func (s *errorStore) GetByID(context.Context, uuid.UUID) (*person.Person, error) {
	return nil, s.err
}
func (s *errorStore) List(context.Context, person.ListParams) ([]person.Person, int, error) {
	return nil, 0, s.err
}
func (s *errorStore) ListDeleted(context.Context, person.ListParams) ([]person.Person, int, error) {
	return nil, 0, s.err
}
func (s *errorStore) GetDeletedByID(context.Context, uuid.UUID) (*person.Person, error) {
	return nil, s.err
}
func (s *errorStore) Update(context.Context, uuid.UUID, *person.Person) (*person.Person, error) {
	return nil, s.err
}
func (s *errorStore) SoftDelete(context.Context, uuid.UUID) error { return s.err }
func (s *errorStore) Restore(context.Context, uuid.UUID) error    { return s.err }
func (s *errorStore) HardDelete(context.Context, uuid.UUID) error { return s.err }

func newFakeStore() *fakeStore {
	return &fakeStore{items: make(map[uuid.UUID]*person.Person)}
}

func (f *fakeStore) Create(_ context.Context, p *person.Person) (*person.Person, error) {
	cp := *p
	cp.ID = uuid.New()
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	f.items[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakeStore) GetByID(_ context.Context, id uuid.UUID) (*person.Person, error) {
	p, ok := f.items[id]
	if !ok || p.DeletedAt != nil {
		return nil, person.ErrNotFound
	}
	out := *p
	return &out, nil
}

func (f *fakeStore) GetDeletedByID(_ context.Context, id uuid.UUID) (*person.Person, error) {
	p, ok := f.items[id]
	if !ok || p.DeletedAt == nil {
		return nil, person.ErrNotFound
	}
	out := *p
	return &out, nil
}

func (f *fakeStore) List(_ context.Context, _ person.ListParams) ([]person.Person, int, error) {
	out := make([]person.Person, 0, len(f.items))
	for _, p := range f.items {
		if p.DeletedAt == nil {
			out = append(out, *p)
		}

	}
	return out, len(out), nil
}

func (f *fakeStore) ListDeleted(_ context.Context, _ person.ListParams) ([]person.Person, int, error) {
	out := make([]person.Person, 0, len(f.items))
	for _, p := range f.items {
		if p.DeletedAt != nil {
			out = append(out, *p)
		}
	}
	return out, len(out), nil
}

func (f *fakeStore) Update(_ context.Context, id uuid.UUID, p *person.Person) (*person.Person, error) {
	existing, ok := f.items[id]
	if !ok || existing.DeletedAt != nil {
		return nil, person.ErrNotFound
	}
	cp := *p
	cp.ID = id
	cp.UpdatedAt = time.Now()
	f.items[id] = &cp
	out := cp
	return &out, nil
}

func (f *fakeStore) SoftDelete(_ context.Context, id uuid.UUID) error {
	p, ok := f.items[id]
	if !ok || p.DeletedAt != nil {
		return person.ErrNotFound
	}

	now := time.Now()
	p.DeletedAt = &now
	return nil
}

func (f *fakeStore) Restore(_ context.Context, id uuid.UUID) error {
	p, ok := f.items[id]
	if !ok || p.DeletedAt == nil {
		return person.ErrNotFound
	}
	p.DeletedAt = nil
	return nil
}

func (f *fakeStore) HardDelete(_ context.Context, id uuid.UUID) error {
	p, ok := f.items[id]
	if !ok || p.DeletedAt == nil {
		return person.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func (f *fakeStore) PurgeExpired(_ context.Context, _ time.Duration) (int64, error) { return 0, nil }

func (f *fakeStore) Ping(_ context.Context) error { return nil }

func (f *fakeStore) CreateRelationship(_ context.Context, personID uuid.UUID, in person.RelationshipInput) (*person.RelationshipView, error) {
	if in.RelatedPersonID != nil {
		if _, ok := f.items[*in.RelatedPersonID]; !ok {
			return nil, person.ErrRelatedPersonNotFound
		}
	}
	for _, existing := range f.relationships {
		if existing.personID == personID && in.RelatedPersonID != nil && existing.relatedPersonID != nil &&
			*existing.relatedPersonID == *in.RelatedPersonID && existing.relType == in.Type {
			return nil, person.ErrRelationshipExists
		}
	}
	id := uuid.New()
	f.relationships = append(f.relationships, fakeRelationship{
		id: id, personID: personID, relatedPersonID: in.RelatedPersonID, relatedName: in.RelatedPersonName, relType: in.Type,
	})
	name := ""
	if in.RelatedPersonName != nil {
		name = *in.RelatedPersonName
	}
	if in.RelatedPersonID != nil {
		if p, ok := f.items[*in.RelatedPersonID]; ok {
			name = p.DisplayName
		}
	}
	return &person.RelationshipView{ID: id, Type: in.Type, RelatedPersonID: in.RelatedPersonID, RelatedPersonName: name}, nil
}

func (f *fakeStore) ListRelationships(_ context.Context, personID uuid.UUID) ([]person.RelationshipView, error) {
	var views []person.RelationshipView
	for _, rel := range f.relationships {
		switch {
		case rel.personID == personID:
			name := ""
			if rel.relatedName != nil {
				name = *rel.relatedName
			}
			if rel.relatedPersonID != nil {
				if p, ok := f.items[*rel.relatedPersonID]; ok {
					name = p.DisplayName
				}
			}
			views = append(views, person.RelationshipView{ID: rel.id, Type: rel.relType, RelatedPersonID: rel.relatedPersonID, RelatedPersonName: name})
		case rel.relatedPersonID != nil && *rel.relatedPersonID == personID:
			name := ""
			if p, ok := f.items[rel.personID]; ok {
				name = p.DisplayName
			}
			id := rel.personID
			views = append(views, person.RelationshipView{ID: rel.id, Type: rel.relType.Inverse(), RelatedPersonID: &id, RelatedPersonName: name})
		}
	}
	return views, nil
}

func (f *fakeStore) ListIncomingRelationships(_ context.Context, personID uuid.UUID) ([]person.RelationshipView, error) {
	var views []person.RelationshipView
	for _, rel := range f.relationships {
		if rel.relatedPersonID == nil || *rel.relatedPersonID != personID {
			continue
		}
		name := ""
		if p, ok := f.items[rel.personID]; ok {
			name = p.DisplayName
		}
		id := rel.personID
		views = append(views, person.RelationshipView{ID: rel.id, Type: rel.relType.Inverse(), RelatedPersonID: &id, RelatedPersonName: name})
	}
	return views, nil
}

func (f *fakeStore) DeleteRelationship(_ context.Context, personID, relationshipID uuid.UUID) error {
	for i, rel := range f.relationships {
		if rel.id != relationshipID {
			continue
		}
		if rel.personID != personID && (rel.relatedPersonID == nil || *rel.relatedPersonID != personID) {
			continue
		}
		f.relationships = append(f.relationships[:i], f.relationships[i+1:]...)
		return nil
	}
	return person.ErrNotFound
}

func (f *fakeStore) ReplaceRelationships(_ context.Context, personID uuid.UUID, desired []person.RelationshipInput) error {
	kept := f.relationships[:0]
	for _, rel := range f.relationships {
		if rel.personID != personID {
			kept = append(kept, rel)
		}
	}
	f.relationships = kept
	for _, in := range desired {
		f.relationships = append(f.relationships, fakeRelationship{
			id: uuid.New(), personID: personID, relatedPersonID: in.RelatedPersonID, relatedName: in.RelatedPersonName, relType: in.Type,
		})
	}
	return nil
}

func (f *fakeStore) FindByDisplayName(_ context.Context, name string) ([]person.Person, error) {
	var out []person.Person
	for _, p := range f.items {
		if p.DisplayName == name && p.DeletedAt == nil {
			out = append(out, *p)
		}
	}
	return out, nil
}

func testRouter() (http.Handler, *fakeStore) {
	store := newFakeStore()
	svc := person.NewService(store)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(logger, svc, store, nil, nil, []string{"http://localhost:5173"}), store
}

func doJSON(t *testing.T, h http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthAndReady(t *testing.T) {
	h, _ := testRouter()
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := doJSON(t, h, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}

		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Error("missing security response headers")
		}
	}

}

func TestCreatePersonSuccess(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{
		"first_name":    "Scott",
		"last_name":     "Fridlund",
		"custom_fields": map[string]any{"blood_type": "O+"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	var p person.Person
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.DisplayName != "Scott Fridlund" {
		t.Errorf("DisplayName = %q", p.DisplayName)
	}
}

func TestCreatePersonValidationError(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{"first_name": ""})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestCreatePersonUnknownField(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{
		"first_name": "A", "last_name": "B", "bogus": "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGetPersonNotFound(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodGet, "/api/v1/persons/"+uuid.NewString(), nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetPersonInvalidID(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodGet, "/api/v1/persons/not-a-uuid", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestFullCRUDFlow(t *testing.T) {
	h, _ := testRouter()

	// Create
	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{
		"first_name": "Jane", "last_name": "Doe",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}

	var created person.Person
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// List
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}

	var list listResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Total != 1 {
		t.Errorf("Total = %d, want 1", list.Total)
	}

	// Patch
	rec = doJSON(t, h, http.MethodPatch, "/api/v1/persons/"+created.ID.String(), map[string]any{
		"last_name": "Smith",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var patched person.Person
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if patched.LastName != "Smith" {
		t.Errorf("LastName = %q, want Smith", patched.LastName)
	}

	// Delete
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+created.ID.String(), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}

	// Get after delete -> 404
	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/"+created.ID.String(), nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rec.Code)
	}
}

func TestPatchNewFields(t *testing.T) {
	h, store := testRouter()
	p, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "B"})

	rec := doJSON(t, h, http.MethodPatch, "/api/v1/persons/"+p.ID.String(), map[string]any{
		"nickname":      "Ace",
		"pronouns":      "they/them",
		"birthdate":     "1990-06-15",
		"phone_numbers": []map[string]string{{"label": "mobile", "value": "+1-555-0100"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated person.Person
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Nickname == nil || *updated.Nickname != "Ace" {
		t.Errorf("Nickname = %v", updated.Nickname)
	}
	if len(updated.PhoneNumbers) != 1 {
		t.Errorf("PhoneNumbers = %v", updated.PhoneNumbers)
	}

	// Wrong type for phone_numbers should return 400.
	rec = doJSON(t, h, http.MethodPatch, "/api/v1/persons/"+p.ID.String(), map[string]any{
		"phone_numbers": "not-an-array",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPatchInvalidCustomField(t *testing.T) {
	h, store := testRouter()
	p, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "B"})
	rec := doJSON(t, h, http.MethodPatch, "/api/v1/persons/"+p.ID.String(), map[string]any{
		"custom_fields": map[string]any{"Bad-Key": "x"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestPatchUnknownFieldRejected(t *testing.T) {
	h, store := testRouter()
	p, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "B"})
	rec := doJSON(t, h, http.MethodPatch, "/api/v1/persons/"+p.ID.String(), map[string]any{"bogus": 1})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPatchWrongType(t *testing.T) {
	h, store := testRouter()
	p, _, _ := person.NewService(store).Create(context.Background(), person.CreateInput{FirstName: "A", LastName: "B"})
	rec := doJSON(t, h, http.MethodPatch, "/api/v1/persons/"+p.ID.String(), map[string]any{"first_name": 123})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPatchNotFound(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodPatch, "/api/v1/persons/"+uuid.NewString(), map[string]any{"last_name": "X"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteNotFound(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+uuid.NewString(), nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestListWithParams(t *testing.T) {
	h, _ := testRouter()
	_ = doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{"first_name": "Alice", "last_name": "Zed"})
	_ = doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{"first_name": "Bob", "last_name": "Young"})

	rec := doJSON(t, h, http.MethodGet, "/api/v1/persons?page=1&page_size=200&sort=first_name&order=asc&first_name=Ali", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var list listResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list.PageSize != 100 {
		t.Errorf("PageSize = %d, want capped at 100", list.PageSize)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodGet, "/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	rec = doJSON(t, h, http.MethodPut, "/api/v1/persons/"+uuid.NewString(), nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestCreateMalformedJSON(t *testing.T) {
	h, _ := testRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/persons", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}

}

func TestRecycleBinFlow(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{"first_name": "Recycle", "last_name": "Bin"})
	var created person.Person
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	_ = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+created.ID.String(), nil)

	rec = doJSON(t, h, http.MethodGet, "/api/v1/persons/deleted", nil)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("Recycle")) {
		t.Fatalf("deleted list status/body = %d/%s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodPost, "/api/v1/persons/"+created.ID.String()+"/restore", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("restore status = %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+created.ID.String()+"/permanent", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("permanent delete active person status = %d", rec.Code)
	}
}

func TestDecodeUpdateFields(t *testing.T) {
	cases := []string{
		`{"first_name":"A"}`, `{"last_name":"B"}`, `{"middle_names":["M"]}`,
		`{"nickname":"N"}`, `{"pronouns":"they"}`, `{"birthdate":"2020-01-01"}`,
		`{"phone_numbers":[{"label":"mobile","value":"555"}]}`,
		`{"emails":[{"label":"home","value":"a@example.com"}]}`,
		`{"addresses":[{"label":"home","city":"Springfield"}]}`,
		`{"organization":{"name":"Acme","title":"Engineer"}}`, `{"organization":null}`,
		`{"notes":"hello"}`, `{"custom_fields":{"x":"y"}}`, `{"is_favorite":true}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(body))
		if _, err := decodeUpdate(httptest.NewRecorder(), req); err != nil {
			t.Errorf("decodeUpdate(%s): %v", body, err)
		}
	}
}

func TestDecodeUpdateRejectsMalformedFields(t *testing.T) {
	cases := []string{
		`{"first_name":1}`, `{"last_name":1}`, `{"middle_names":"x"}`,
		`{"nickname":1}`, `{"pronouns":1}`, `{"birthdate":1}`,
		`{"phone_numbers":"x"}`, `{"phone_numbers":["555"]}`,
		`{"emails":"x"}`, `{"addresses":"x"}`, `{"organization":"x"}`, `{"notes":1}`,
		`{"custom_fields":"x"}`, `{"is_favorite":"x"}`, `{"unknown":true}`,
	}

	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(body))
		if _, err := decodeUpdate(httptest.NewRecorder(), req); err == nil {
			t.Errorf("decodeUpdate(%s) unexpectedly succeeded", body)
		}
	}

}

// TestParseListParamsDefaultsToLastNameAscending guards the default sort
// fix: the contact list should default to "Last Name, First Name" ascending
// instead of display_name descending.
func TestParseListParamsDefaultsToLastNameAscending(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/persons", nil)
	params := parseListParams(req)
	if params.SortField != "last_name" {
		t.Errorf("SortField = %q, want last_name", params.SortField)
	}
	if params.SortDesc {
		t.Error("SortDesc = true, want ascending by default")
	}
}

func TestParseListParamsParsesFavoriteFilter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/persons?favorite=true", nil)
	params := parseListParams(req)
	if params.Favorite == nil || !*params.Favorite {
		t.Errorf("Favorite = %v, want true", params.Favorite)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/persons", nil)
	params = parseListParams(req)
	if params.Favorite != nil {
		t.Errorf("Favorite = %v, want nil when not specified", params.Favorite)
	}
}

func TestDecodeJSONRejectsTrailingPayload(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"first_name":"A"}{"last_name":"B"}`))
	if err := decodeJSON(httptest.NewRecorder(), req, &map[string]string{}); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
}

func TestRecycleBinMissingPerson(t *testing.T) {
	h, _ := testRouter()
	id := uuid.NewString()
	for _, methodPath := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/persons/" + id + "/restore"},
		{http.MethodDelete, "/api/v1/persons/" + id + "/permanent"},
	} {
		rec := doJSON(t, h, methodPath.method, methodPath.path, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", methodPath.method, methodPath.path, rec.Code)
		}
	}
}

func TestPermanentDeleteDeletedPerson(t *testing.T) {
	h, _ := testRouter()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/persons", map[string]any{"first_name": "A", "last_name": "B"})
	var p person.Person
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	_ = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+p.ID.String(), nil)
	rec = doJSON(t, h, http.MethodDelete, "/api/v1/persons/"+p.ID.String()+"/permanent", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

}

func TestHandlersReturnInternalErrors(t *testing.T) {
	store := &errorStore{fakeStore: newFakeStore(), err: errors.New("database unavailable")}
	svc := person.NewService(store)
	h := NewRouter(slog.Default(), svc, store, nil, nil, nil)
	id := uuid.NewString()
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/persons/" + id, nil},
		{http.MethodGet, "/api/v1/persons", nil},
		{http.MethodGet, "/api/v1/persons/deleted", nil},
		{http.MethodPatch, "/api/v1/persons/" + id, map[string]any{"nickname": "N"}},
		{http.MethodDelete, "/api/v1/persons/" + id, nil},
		{http.MethodPost, "/api/v1/persons/" + id + "/restore", nil},
		{http.MethodDelete, "/api/v1/persons/" + id + "/permanent", nil},
	} {
		rec := doJSON(t, h, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s = %d, want 500", tc.method, tc.path, rec.Code)
		}
	}
}
