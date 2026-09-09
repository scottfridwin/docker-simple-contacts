package person

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

// memStore is an in-memory store implementation for unit tests.
type memStore struct {
	items         map[uuid.UUID]*Person
	relationships []storedRelationship
}

type storedRelationship struct {
	id              uuid.UUID
	personID        uuid.UUID
	relatedPersonID *uuid.UUID
	relatedName     *string
	relType         RelationType
}

type memNotifier struct {
	events []contactsync.PersonChange
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

func (m *memStore) GetDeletedByID(_ context.Context, id uuid.UUID) (*Person, error) {
	p, ok := m.items[id]
	if !ok || p.DeletedAt == nil {
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

func (m *memStore) ListDeleted(_ context.Context, _ ListParams) ([]Person, int, error) {
	out := make([]Person, 0, len(m.items))
	for _, p := range m.items {
		if p.DeletedAt != nil {
			out = append(out, *p)
		}
	}
	return out, len(out), nil
}

func (m *memStore) Restore(_ context.Context, id uuid.UUID) error {
	p, ok := m.items[id]
	if !ok || p.DeletedAt == nil {
		return ErrNotFound
	}
	p.DeletedAt = nil
	return nil
}

func (m *memStore) HardDelete(_ context.Context, id uuid.UUID) error {
	p, ok := m.items[id]
	if !ok || p.DeletedAt == nil {
		return ErrNotFound
	}
	delete(m.items, id)
	return nil
}

func (m *memStore) PurgeExpired(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

func (m *memStore) CreateRelationship(_ context.Context, personID uuid.UUID, in RelationshipInput) (*RelationshipView, error) {
	for _, existing := range m.relationships {
		if existing.personID == personID && in.RelatedPersonID != nil && existing.relatedPersonID != nil &&
			*existing.relatedPersonID == *in.RelatedPersonID && existing.relType == in.Type {
			return nil, ErrRelationshipExists
		}
	}
	if in.RelatedPersonID != nil {
		if _, ok := m.items[*in.RelatedPersonID]; !ok {
			return nil, ErrRelatedPersonNotFound
		}
	}
	id := uuid.New()
	m.relationships = append(m.relationships, storedRelationship{
		id: id, personID: personID, relatedPersonID: in.RelatedPersonID, relatedName: in.RelatedPersonName, relType: in.Type,
	})
	name := ""
	if in.RelatedPersonName != nil {
		name = *in.RelatedPersonName
	}
	if in.RelatedPersonID != nil {
		if p, ok := m.items[*in.RelatedPersonID]; ok {
			name = p.DisplayName
		}
	}
	return &RelationshipView{ID: id, Type: in.Type, RelatedPersonID: in.RelatedPersonID, RelatedPersonName: name}, nil
}

func (m *memStore) ListRelationships(_ context.Context, personID uuid.UUID) ([]RelationshipView, error) {
	var views []RelationshipView
	for _, rel := range m.relationships {
		switch {
		case rel.personID == personID:
			name := ""
			if rel.relatedName != nil {
				name = *rel.relatedName
			}
			if rel.relatedPersonID != nil {
				if p, ok := m.items[*rel.relatedPersonID]; ok {
					name = p.DisplayName
				}
			}
			views = append(views, RelationshipView{ID: rel.id, Type: rel.relType, RelatedPersonID: rel.relatedPersonID, RelatedPersonName: name})
		case rel.relatedPersonID != nil && *rel.relatedPersonID == personID:
			name := ""
			if p, ok := m.items[rel.personID]; ok {
				name = p.DisplayName
			}
			id := rel.personID
			views = append(views, RelationshipView{ID: rel.id, Type: rel.relType.Inverse(), RelatedPersonID: &id, RelatedPersonName: name})
		}
	}
	return views, nil
}

func (m *memStore) DeleteRelationship(_ context.Context, personID, relationshipID uuid.UUID) error {
	for i, rel := range m.relationships {
		if rel.id != relationshipID {
			continue
		}
		if rel.personID != personID && (rel.relatedPersonID == nil || *rel.relatedPersonID != personID) {
			continue
		}
		m.relationships = append(m.relationships[:i], m.relationships[i+1:]...)
		return nil
	}
	return ErrNotFound
}

func (m *memStore) ReplaceRelationships(_ context.Context, personID uuid.UUID, desired []RelationshipInput) error {
	kept := m.relationships[:0]
	for _, rel := range m.relationships {
		if rel.personID != personID {
			kept = append(kept, rel)
		}
	}
	m.relationships = kept
	for _, in := range desired {
		m.relationships = append(m.relationships, storedRelationship{
			id: uuid.New(), personID: personID, relatedPersonID: in.RelatedPersonID, relatedName: in.RelatedPersonName, relType: in.Type,
		})
	}
	return nil
}

func (m *memStore) FindByDisplayName(_ context.Context, name string) ([]Person, error) {
	var out []Person
	for _, p := range m.items {
		if p.DisplayName == name && p.DeletedAt == nil {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (n *memNotifier) RecordChanged(_ context.Context, change contactsync.PersonChange) error {
	n.events = append(n.events, change)
	return nil
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
	nums := []contactsync.LabeledValue{{Label: "mobile", Value: "+1-555-0100"}}
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
	if len(updated.PhoneNumbers) != 1 || updated.PhoneNumbers[0].Value != "+1-555-0100" {
		t.Errorf("PhoneNumbers = %v", updated.PhoneNumbers)
	}

	// Clearing optional fields should work.
	updated, _, _ = svc.Update(context.Background(), created.ID, UpdateInput{
		Nickname:        nil,
		NicknameSet:     true,
		PhoneNumbers:    &[]contactsync.LabeledValue{},
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

func TestServiceRecycleBinOperations(t *testing.T) {
	store := newMemStore()
	svc := NewService(store)
	p, _, err := svc.Create(context.Background(), CreateInput{FirstName: "A", LastName: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	deleted, total, err := svc.ListDeleted(context.Background(), ListParams{Page: 1, PageSize: 25})
	if err != nil || total != 1 || len(deleted) != 1 {
		t.Fatalf("unexpected deleted list: total=%d len=%d err=%v", total, len(deleted), err)
	}
	if err := svc.Restore(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.HardDelete(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
}

func TestServiceEmitsSyncNotifications(t *testing.T) {
	store := newMemStore()
	notifier := &memNotifier{}
	svc := NewService(store, notifier)
	created, _, err := svc.Create(context.Background(), CreateInput{FirstName: "A", LastName: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if len(notifier.events) != 1 || notifier.events[0].Kind != contactsync.ChangeKindCreated {
		t.Fatalf("expected created event, got %#v", notifier.events)
	}

	newLast := "C"
	if _, _, err := svc.Update(context.Background(), created.ID, UpdateInput{LastName: &newLast, LastNameSet: true}); err != nil {
		t.Fatal(err)
	}
	if len(notifier.events) != 2 || notifier.events[1].Kind != contactsync.ChangeKindUpdated {
		t.Fatalf("expected updated event, got %#v", notifier.events)
	}

	if err := svc.Delete(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if len(notifier.events) != 3 || notifier.events[2].Kind != contactsync.ChangeKindDeleted {
		t.Fatalf("expected deleted event, got %#v", notifier.events)
	}
}

func TestServiceSkipsNotificationsForSyncOrigin(t *testing.T) {
	store := newMemStore()
	notifier := &memNotifier{}
	svc := NewService(store, notifier)

	ctx := contactsync.WithSyncOrigin(context.Background())
	if _, _, err := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "B"}); err != nil {
		t.Fatal(err)
	}
	if len(notifier.events) != 0 {
		t.Fatalf("expected no events for sync-origin context, got %#v", notifier.events)
	}
}

// TestServiceRelationshipDirectionality guards the core relationship design:
// a single stored link (A is Parent of B) must be visible as the inverse
// (B's list shows Child, pointing at A) without a second stored row.
func TestServiceRelationshipDirectionality(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	a, _, err := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := svc.Create(ctx, CreateInput{FirstName: "B", LastName: "Child"})
	if err != nil {
		t.Fatal(err)
	}

	view, verrs, err := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationParent, RelatedPersonID: &b.ID})
	if err != nil || verrs.HasErrors() {
		t.Fatalf("CreateRelationship: view=%v verrs=%v err=%v", view, verrs, err)
	}
	if view.Type != RelationParent || view.RelatedPersonName != "B Child" {
		t.Fatalf("unexpected forward view: %+v", view)
	}

	aViews, err := svc.ListRelationships(ctx, a.ID)
	if err != nil || len(aViews) != 1 || aViews[0].Type != RelationParent || aViews[0].RelatedPersonName != "B Child" {
		t.Fatalf("A's relationships = %+v, err=%v", aViews, err)
	}

	bViews, err := svc.ListRelationships(ctx, b.ID)
	if err != nil || len(bViews) != 1 || bViews[0].Type != RelationChild || bViews[0].RelatedPersonName != "A Parent" {
		t.Fatalf("B's relationships = %+v, err=%v, want inverted Child pointing at A", bViews, err)
	}
	if bViews[0].RelatedPersonID == nil || *bViews[0].RelatedPersonID != a.ID {
		t.Fatalf("B's relationship related_person_id = %v, want %v", bViews[0].RelatedPersonID, a.ID)
	}

	if err := svc.DeleteRelationship(ctx, b.ID, bViews[0].ID); err != nil {
		t.Fatalf("DeleteRelationship from reverse side: %v", err)
	}
	if aViews, _ := svc.ListRelationships(ctx, a.ID); len(aViews) != 0 {
		t.Fatalf("expected relationship gone from both sides after delete, A still has %+v", aViews)
	}
}

func TestServiceRelationshipValidation(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "One"})

	if _, verrs, _ := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: "friend"}); !verrs.HasErrors() {
		t.Error("expected error for invalid relationship type")
	}
	if _, verrs, _ := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSpouse}); !verrs.HasErrors() {
		t.Error("expected error when neither related_person_id nor related_person_name is set")
	}
	if _, verrs, _ := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSpouse, RelatedPersonID: &a.ID}); !verrs.HasErrors() {
		t.Error("expected error for self-relationship")
	}
	missing := uuid.New()
	if _, verrs, err := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSpouse, RelatedPersonID: &missing}); err != nil || !verrs.HasErrors() {
		t.Errorf("expected validation error for missing related person, got verrs=%v err=%v", verrs, err)
	}

	name := "Unlinked Friend"
	if _, verrs, err := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSibling, RelatedPersonName: &name}); err != nil || verrs.HasErrors() {
		t.Fatalf("expected unlinked relationship to succeed, verrs=%v err=%v", verrs, err)
	}
}

func TestServiceRelationshipSymmetricTypesInvertToSameType(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "One"})
	b, _, _ := svc.Create(ctx, CreateInput{FirstName: "B", LastName: "Two"})

	if _, verrs, err := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSpouse, RelatedPersonID: &b.ID}); err != nil || verrs.HasErrors() {
		t.Fatalf("CreateRelationship: %v %v", verrs, err)
	}
	bViews, err := svc.ListRelationships(ctx, b.ID)
	if err != nil || len(bViews) != 1 || bViews[0].Type != RelationSpouse {
		t.Fatalf("expected symmetric Spouse relationship on B, got %+v, err=%v", bViews, err)
	}
}

func TestServiceReplaceRelationshipsAndFindByDisplayName(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "One"})
	b, _, _ := svc.Create(ctx, CreateInput{FirstName: "B", LastName: "Two"})

	name := "Unlinked"
	if err := svc.ReplaceRelationships(ctx, a.ID, []RelationshipInput{
		{Type: RelationSpouse, RelatedPersonID: &b.ID},
		{Type: RelationSibling, RelatedPersonName: &name},
	}); err != nil {
		t.Fatalf("ReplaceRelationships: %v", err)
	}
	views, err := svc.ListRelationships(ctx, a.ID)
	if err != nil || len(views) != 2 {
		t.Fatalf("ListRelationships after replace = %+v, err=%v", views, err)
	}

	matches, err := svc.FindByDisplayName(ctx, a.DisplayName)
	if err != nil || len(matches) != 1 || matches[0].ID != a.ID {
		t.Fatalf("FindByDisplayName = %+v, err=%v", matches, err)
	}
	if none, err := svc.FindByDisplayName(ctx, "Nobody Here"); err != nil || len(none) != 0 {
		t.Fatalf("FindByDisplayName for unknown name = %+v, err=%v", none, err)
	}
}

func TestServiceRelationshipInputBothIDAndNameRejected(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "One"})
	b, _, _ := svc.Create(ctx, CreateInput{FirstName: "B", LastName: "Two"})
	name := "Also Named"
	if _, verrs, _ := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSpouse, RelatedPersonID: &b.ID, RelatedPersonName: &name}); !verrs.HasErrors() {
		t.Error("expected error when both related_person_id and related_person_name are set")
	}
}

func TestServiceCreateRelationshipMapsDuplicateError(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "One"})
	b, _, _ := svc.Create(ctx, CreateInput{FirstName: "B", LastName: "Two"})
	if _, verrs, err := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSpouse, RelatedPersonID: &b.ID}); err != nil || verrs.HasErrors() {
		t.Fatalf("first create: verrs=%v err=%v", verrs, err)
	}
	_, verrs, err := svc.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSpouse, RelatedPersonID: &b.ID})
	if err != nil || !verrs.HasErrors() {
		t.Fatalf("expected a validation error for the duplicate relationship, got verrs=%v err=%v", verrs, err)
	}
}

func TestServiceUpdateRejectsInvalidInput(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	created, _, _ := svc.Create(ctx, CreateInput{FirstName: "A", LastName: "B"})
	empty := ""
	_, verrs, err := svc.Update(ctx, created.ID, UpdateInput{FirstName: &empty, FirstNameSet: true})
	if err != nil || !verrs.HasErrors() {
		t.Fatalf("expected validation error, got verrs=%v err=%v", verrs, err)
	}
}

// TestSnapshotClonesNonEmptyCustomFields guards the non-empty branch of
// cloneMap (the empty-map branch is already covered by other tests).
func TestSnapshotClonesNonEmptyCustomFields(t *testing.T) {
	p := Person{CustomFields: map[string]any{"a": "b"}}
	snap := p.Snapshot(nil)
	if snap.CustomFields["a"] != "b" {
		t.Fatalf("CustomFields = %v", snap.CustomFields)
	}
	snap.CustomFields["a"] = "mutated"
	if p.CustomFields["a"] != "b" {
		t.Fatal("Snapshot's custom_fields map must be a copy, not shared with the source Person")
	}
}

// TestServiceDeleteRestoreHardDeletePropagateNotFound guards the not-found
// error paths in Delete/Restore/HardDelete, which look up the record before
// mutating it.
func TestServiceDeleteRestoreHardDeletePropagateNotFound(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	missing := uuid.New()

	if err := svc.Delete(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(missing) = %v, want ErrNotFound", err)
	}
	if err := svc.Restore(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restore(missing) = %v, want ErrNotFound", err)
	}
	if err := svc.HardDelete(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("HardDelete(missing) = %v, want ErrNotFound", err)
	}
}
