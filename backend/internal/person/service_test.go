package person

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// memStore is an in-memory store implementation for unit tests.
type memStore struct {
	items map[uuid.UUID]*Person
}

func newMemStore() *memStore {
	return &memStore{items: make(map[uuid.UUID]*Person)}
}

func (m *memStore) Create(_ context.Context, p *Person) (*Person, error) {
	cp := *p
	cp.ID = uuid.New()
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	m.items[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (m *memStore) GetByID(_ context.Context, id uuid.UUID) (*Person, error) {
	p, ok := m.items[id]
	if !ok || p.DeletedAt != nil {
		return nil, ErrNotFound
	}
	out := *p
	return &out, nil
}

func (m *memStore) List(_ context.Context, _ ListParams) ([]Person, int, error) {
	out := make([]Person, 0, len(m.items))
	for _, p := range m.items {
		if p.DeletedAt == nil {
			out = append(out, *p)
		}
	}
	return out, len(out), nil
}

func (m *memStore) Update(_ context.Context, id uuid.UUID, p *Person) (*Person, error) {
	existing, ok := m.items[id]
	if !ok || existing.DeletedAt != nil {
		return nil, ErrNotFound
	}
	cp := *p
	cp.ID = id
	cp.UpdatedAt = time.Now()
	m.items[id] = &cp
	out := cp
	return &out, nil
}

func (m *memStore) SoftDelete(_ context.Context, id uuid.UUID) error {
	p, ok := m.items[id]
	if !ok || p.DeletedAt != nil {
		return ErrNotFound
	}
	now := time.Now()
	p.DeletedAt = &now
	return nil
}

func (m *memStore) PurgeExpired(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

func TestServiceCreateDerivesDisplayName(t *testing.T) {
	svc := NewService(newMemStore())
	p, verrs, err := svc.Create(context.Background(), CreateInput{
		FirstName:   "Scott",
		MiddleNames: []string{"A"},
		LastName:    "Fridlund",
	})
	if err != nil || verrs.HasErrors() {
		t.Fatalf("unexpected: err=%v verrs=%v", err, verrs)
	}
	if p.DisplayName != "Scott A Fridlund" {
		t.Errorf("DisplayName = %q, want derived", p.DisplayName)
	}
}

func TestServiceCreateValidationError(t *testing.T) {
	svc := NewService(newMemStore())
	_, verrs, err := svc.Create(context.Background(), CreateInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verrs.HasErrors() {
		t.Fatal("expected validation errors")
	}
}

func TestServiceUpdatePartial(t *testing.T) {
	svc := NewService(newMemStore())
	created, _, _ := svc.Create(context.Background(), CreateInput{FirstName: "A", LastName: "B"})

	newLast := "Changed"
	updated, verrs, err := svc.Update(context.Background(), created.ID, UpdateInput{
		LastName:    &newLast,
		LastNameSet: true,
	})
	if err != nil || verrs.HasErrors() {
		t.Fatalf("unexpected: err=%v verrs=%v", err, verrs)
	}
	if updated.LastName != "Changed" {
		t.Errorf("LastName = %q, want Changed", updated.LastName)
	}
	if updated.FirstName != "A" {
		t.Errorf("FirstName = %q, want unchanged A", updated.FirstName)
	}
}

func TestServiceUpdateNotFound(t *testing.T) {
	svc := NewService(newMemStore())
	name := "X"
	_, _, err := svc.Update(context.Background(), uuid.New(), UpdateInput{FirstName: &name, FirstNameSet: true})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestServiceDelete(t *testing.T) {
	svc := NewService(newMemStore())
	created, _, _ := svc.Create(context.Background(), CreateInput{FirstName: "A", LastName: "B"})
	if err := svc.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := svc.Get(context.Background(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestServiceListAndPurge(t *testing.T) {
	svc := NewService(newMemStore())
	_, _, _ = svc.Create(context.Background(), CreateInput{FirstName: "A", LastName: "B"})
	_, _, _ = svc.Create(context.Background(), CreateInput{FirstName: "C", LastName: "D"})

	items, total, err := svc.List(context.Background(), ListParams{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Errorf("total=%d len=%d, want 2", total, len(items))
	}

	if _, err := svc.PurgeExpired(context.Background(), time.Hour); err != nil {
		t.Errorf("purge: %v", err)
	}
}

func TestServiceUpdateNewOptionalFields(t *testing.T) {
	svc := NewService(newMemStore())
	created, _, _ := svc.Create(context.Background(), CreateInput{FirstName: "A", LastName: "B"})

	nick := "Ace"
	pro := "they/them"
	bd := "1990-06-15"
	nums := []string{"+1-555-0100"}
	updated, verrs, err := svc.Update(context.Background(), created.ID, UpdateInput{
		Nickname:        &nick,
		NicknameSet:     true,
		Pronouns:        &pro,
		PronounsSet:     true,
		Birthdate:       &bd,
		BirthdateSet:    true,
		PhoneNumbers:    &nums,
		PhoneNumbersSet: true,
	})
	if err != nil || verrs.HasErrors() {
		t.Fatalf("unexpected: err=%v verrs=%v", err, verrs)
	}
	if updated.Nickname == nil || *updated.Nickname != "Ace" {
		t.Errorf("Nickname = %v", updated.Nickname)
	}
	if updated.Pronouns == nil || *updated.Pronouns != "they/them" {
		t.Errorf("Pronouns = %v", updated.Pronouns)
	}
	if updated.Birthdate == nil || *updated.Birthdate != "1990-06-15" {
		t.Errorf("Birthdate = %v", updated.Birthdate)
	}
	if len(updated.PhoneNumbers) != 1 || updated.PhoneNumbers[0] != "+1-555-0100" {
		t.Errorf("PhoneNumbers = %v", updated.PhoneNumbers)
	}

	// Clearing optional fields should work.
	updated, _, _ = svc.Update(context.Background(), created.ID, UpdateInput{
		Nickname:        nil,
		NicknameSet:     true,
		PhoneNumbers:    &[]string{},
		PhoneNumbersSet: true,
	})
	if updated.Nickname != nil {
		t.Errorf("expected Nickname cleared, got %v", updated.Nickname)
	}
	if len(updated.PhoneNumbers) != 0 {
		t.Errorf("expected PhoneNumbers cleared, got %v", updated.PhoneNumbers)
	}
}

func TestServiceUpdateDisplayNameRederived(t *testing.T) {
	svc := NewService(newMemStore())
	created, _, _ := svc.Create(context.Background(), CreateInput{
		FirstName: "A", LastName: "B",
	})
	if created.DisplayName != "A B" {
		t.Fatalf("DisplayName = %q, want derived", created.DisplayName)
	}

	newFirst := "Alpha"
	updated, _, err := svc.Update(context.Background(), created.ID, UpdateInput{
		FirstName: &newFirst, FirstNameSet: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DisplayName != "Alpha B" {
		t.Errorf("DisplayName = %q, want re-derived Alpha B", updated.DisplayName)
	}
}

func TestServiceUpdateMiddleNamesAndCustomFields(t *testing.T) {
	svc := NewService(newMemStore())
	created, _, _ := svc.Create(context.Background(), CreateInput{
		FirstName: "A", LastName: "B",
		CustomFields: map[string]any{"k_one": "v"},
	})

	middles := []string{"M"}
	updated, _, err := svc.Update(context.Background(), created.ID, UpdateInput{
		MiddleNames: &middles, MiddleNamesSet: true,
		CustomFields: map[string]any{"k_two": float64(2)}, CustomFieldsSet: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.MiddleNames) != 1 || updated.MiddleNames[0] != "M" {
		t.Errorf("MiddleNames = %v", updated.MiddleNames)
	}
	if _, ok := updated.CustomFields["k_two"]; !ok {
		t.Errorf("CustomFields = %v, want k_two", updated.CustomFields)
	}
}
