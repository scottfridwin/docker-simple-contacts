package google

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

// TestPersonToRecordUsesProvidedExternalID guards the multi-account collision
// fix: the remote resource id must come from the caller (looked up per sync
// account) instead of a single shared Person.custom_fields value, since one
// Person can be linked to different remote records on different accounts.
func TestPersonToRecordUsesProvidedExternalID(t *testing.T) {
	p := person.Person{
		ID:        uuid.New(),
		FirstName: "Ada",
		LastName:  "Lovelace",
		UpdatedAt: time.Now(),
	}

	forAccountA := personToRecord(p, "people/account-a-contact")
	forAccountB := personToRecord(p, "people/account-b-contact")

	if forAccountA.ExternalID != "people/account-a-contact" {
		t.Fatalf("account A external id = %q", forAccountA.ExternalID)
	}
	if forAccountB.ExternalID != "people/account-b-contact" {
		t.Fatalf("account B external id = %q", forAccountB.ExternalID)
	}
	if forAccountA.ExternalID == forAccountB.ExternalID {
		t.Fatal("expected different accounts to resolve different external ids for the same person")
	}
}

// TestRemoteToLocalDoesNotWriteCustomFields ensures newly imported contacts no
// longer stash the remote resource name in Person.custom_fields, since that
// single shared field can't hold a separate id per connected account.
func TestRemoteToLocalDoesNotWriteCustomFields(t *testing.T) {
	record := contactsync.ProviderRecord{
		Record: contactsync.Record{
			ExternalID: "people/abc123",
			Fields: map[string]contactsync.FieldState{
				"first_name": {IsSet: true, Value: "Grace"},
				"last_name":  {IsSet: true, Value: "Hopper"},
			},
		},
	}

	create, localID := remoteToLocal(record)

	if localID != nil {
		t.Fatalf("expected no local id mapping, got %v", *localID)
	}
	if len(create.CustomFields) != 0 {
		t.Fatalf("expected no custom fields written, got %+v", create.CustomFields)
	}
}
