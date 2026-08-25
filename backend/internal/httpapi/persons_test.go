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

	"github.com/scottfridlund/contacts/backend/internal/person"
)

// fakeStore is an in-memory person store for endpoint tests.
type fakeStore struct {
	items map[uuid.UUID]*person.Person
}

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

func (f *fakeStore) List(_ context.Context, _ person.ListParams) ([]person.Person, int, error) {
	out := make([]person.Person, 0, len(f.items))
	for _, p := range f.items {
		if p.DeletedAt == nil {
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

func (f *fakeStore) PurgeExpired(_ context.Context, _ time.Duration) (int64, error) { return 0, nil }

func (f *fakeStore) Ping(_ context.Context) error { return nil }

func testRouter() (http.Handler, *fakeStore) {
	store := newFakeStore()
	svc := person.NewService(store)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(logger, svc, store, []string{"http://localhost:5173"}), store
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
