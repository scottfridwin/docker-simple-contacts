package person

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

type scanStub struct {
	values []any
	err    error
}

func (s scanStub) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i, value := range s.values {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = value.(uuid.UUID)
		case *string:
			*d = value.(string)
		case *[]string:
			*d = value.([]string)
		case *[]contactsync.LabeledValue:
			*d = value.([]contactsync.LabeledValue)
		case *[]contactsync.Address:
			*d = value.([]contactsync.Address)
		case **contactsync.Organization:
			*d = value.(*contactsync.Organization)
		case **string:
			*d = value.(*string)
		case *map[string]any:
			*d = value.(map[string]any)
		case *bool:
			*d = value.(bool)
		case *time.Time:
			*d = value.(time.Time)
		case **time.Time:
			*d = value.(*time.Time)
		case **uuid.UUID:
			*d = value.(*uuid.UUID)
		}
	}
	return nil
}

func TestScanPersonNormalizesNilCollections(t *testing.T) {
	now := time.Now()
	p, err := scanPerson(scanStub{values: []any{
		uuid.New(), "First", []string(nil), "Last", "First Last",
		(*string)(nil), (*string)(nil), (*string)(nil),
		[]contactsync.LabeledValue(nil), []contactsync.LabeledValue(nil), []contactsync.Address(nil),
		(*contactsync.Organization)(nil), (*string)(nil), map[string]any(nil), false, []string(nil),
		now, now, (*time.Time)(nil),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.MiddleNames) != 0 || len(p.PhoneNumbers) != 0 || len(p.CustomFields) != 0 || len(p.Labels) != 0 {
		t.Fatalf("nil collections were not normalized: %+v", p)
	}
}

func TestScanPersonReturnsScanError(t *testing.T) {
	want := errors.New("scan failed")
	if _, err := scanPerson(scanStub{err: want}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}

}

func TestScanPersonPreservesValues(t *testing.T) {
	now := time.Now()
	nickname := "N"
	deleted := now.Add(time.Hour)
	p, err := scanPerson(scanStub{values: []any{
		uuid.New(), "First", []string{"M"}, "Last", "First M Last",
		&nickname, (*string)(nil), &nickname,
		[]contactsync.LabeledValue{{Value: "a@example.com"}}, []contactsync.LabeledValue{{Value: "555"}}, []contactsync.Address{{City: "Springfield"}},
		&contactsync.Organization{Name: "Acme"}, &nickname, map[string]any{"x": "y"}, true, []string{"Family"},
		now, now, &deleted,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.MiddleNames) != 1 || len(p.PhoneNumbers) != 1 || p.CustomFields["x"] != "y" || p.DeletedAt == nil || !p.IsFavorite || len(p.Labels) != 1 {
		t.Fatalf("values were not preserved: %+v", p)
	}
}

func TestScanPersonAccessible(t *testing.T) {
	now := time.Now()
	ownerID := uuid.New()
	ownerName := "Bob"
	p, err := scanPersonAccessible(scanStub{values: []any{
		uuid.New(), "First", []string(nil), "Last", "First Last",
		(*string)(nil), (*string)(nil), (*string)(nil),
		[]contactsync.LabeledValue(nil), []contactsync.LabeledValue(nil), []contactsync.Address(nil),
		(*contactsync.Organization)(nil), (*string)(nil), map[string]any(nil), false, []string(nil),
		now, now, (*time.Time)(nil),
		&ownerID, &ownerName,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if p.OwnerID == nil || *p.OwnerID != ownerID {
		t.Errorf("OwnerID = %v, want %v", p.OwnerID, ownerID)
	}
	if p.OwnerDisplayName == nil || *p.OwnerDisplayName != ownerName {
		t.Errorf("OwnerDisplayName = %v, want %v", p.OwnerDisplayName, ownerName)
	}
}

func TestScanPersonAccessibleReturnsScanError(t *testing.T) {
	want := errors.New("scan failed")
	if _, err := scanPersonAccessible(scanStub{err: want}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

// TestApplyOwnership guards the three branches: the current viewer owns the
// Person, the current viewer only has shared access, and there's no
// authenticated viewer at all (single-tenant/legacy mode).
func TestApplyOwnership(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	name := "Bob"

	owned := &Person{OwnerID: &ownerID, OwnerDisplayName: &name}
	applyOwnership(owned, ownerID, true)
	if !owned.IsOwner || owned.OwnerDisplayName != nil {
		t.Errorf("owner case: IsOwner=%v OwnerDisplayName=%v", owned.IsOwner, owned.OwnerDisplayName)
	}

	shared := &Person{OwnerID: &otherID, OwnerDisplayName: &name}
	applyOwnership(shared, ownerID, true)
	if shared.IsOwner || shared.OwnerDisplayName == nil {
		t.Errorf("shared case: IsOwner=%v OwnerDisplayName=%v", shared.IsOwner, shared.OwnerDisplayName)
	}

	noViewer := &Person{OwnerID: &otherID, OwnerDisplayName: &name}
	applyOwnership(noViewer, uuid.Nil, false)
	if !noViewer.IsOwner || noViewer.OwnerDisplayName != nil {
		t.Errorf("no-viewer case: IsOwner=%v OwnerDisplayName=%v", noViewer.IsOwner, noViewer.OwnerDisplayName)
	}
}

func TestBuildOrderBy(t *testing.T) {
	cases := []struct {
		field string
		desc  bool
		want  string
	}{
		{"last_name", false, "last_name ASC, first_name ASC, id ASC"},
		{"last_name", true, "last_name DESC, first_name DESC, id ASC"},
		{"first_name", false, "first_name ASC, id ASC"},
		{"display_name", true, "display_name DESC, id ASC"},
		{"created_at", false, "created_at ASC, id ASC"},
		{"updated_at", true, "updated_at DESC, id ASC"},
		{"unknown_field", false, "last_name ASC, first_name ASC, id ASC"},
	}
	for _, tc := range cases {
		if got := buildOrderBy(tc.field, tc.desc); got != tc.want {
			t.Errorf("buildOrderBy(%q, %v) = %q, want %q", tc.field, tc.desc, got, tc.want)
		}
	}
}
