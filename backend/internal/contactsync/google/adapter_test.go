package google

import (
	"context"
	"errors"
	"log/slog"
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

// TestFieldStringsNeverReturnsNilForEmptyValue guards a real bug: a Google
// contact with no middle name/phone numbers has an IsSet=true but empty
// []string field. append([]string(nil), values...) stays nil for an empty
// slice, and that nil later flows into person.UpdateInput.MiddleNames /
// PhoneNumbers, which hit a NOT NULL Postgres column and fail the update
// ("null value in column \"middle_names\" ... violates not-null constraint").
func TestFieldStringsNeverReturnsNilForEmptyValue(t *testing.T) {
	fields := map[string]contactsync.FieldState{
		"middle_names": {IsSet: true, Value: []string{}},
	}
	got := fieldStrings(fields, "middle_names")
	if got == nil {
		t.Fatal("fieldStrings returned nil for an empty-but-set field, want non-nil []string{}")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}

// TestRemoteToLocalProducesUpdatableMiddleNames exercises the same bug at the
// remoteToLocal boundary used directly by mergeRemoteRecord's local update.
func TestRemoteToLocalProducesUpdatableMiddleNames(t *testing.T) {
	record := contactsync.ProviderRecord{
		Record: contactsync.Record{
			ExternalID: "people/abc123",
			Fields: map[string]contactsync.FieldState{
				"first_name":    {IsSet: true, Value: "Grace"},
				"last_name":     {IsSet: true, Value: "Hopper"},
				"middle_names":  {IsSet: true, Value: []string{}},
				"phone_numbers": {IsSet: true, Value: []string{}},
			},
		},
	}

	create, _ := remoteToLocal(record)

	if create.MiddleNames == nil {
		t.Fatal("MiddleNames is nil, would violate the persons.middle_names NOT NULL constraint")
	}
	if create.PhoneNumbers == nil {
		t.Fatal("PhoneNumbers is nil, would violate the persons.phone_numbers NOT NULL constraint")
	}
}

// TestToGooglePersonMapsNewContactFields ensures emails, addresses,
// organization, notes, nickname, and birthdate all round-trip into the
// Google People API payload shape.
func TestToGooglePersonMapsNewContactFields(t *testing.T) {
	notes := "Met at a conference."
	nickname := "Ace"
	birthdate := "1989-04-19"
	record := contactsync.Record{
		Fields: map[string]contactsync.FieldState{
			"first_name": {IsSet: true, Value: "Scott"},
			"last_name":  {IsSet: true, Value: "Fridlund"},
			"emails": {IsSet: true, Value: []contactsync.LabeledValue{
				{Label: "work", Value: "scott@example.com"},
			}},
			"phone_numbers": {IsSet: true, Value: []contactsync.LabeledValue{
				{Label: "mobile", Value: "+1-555-0100"},
			}},
			"addresses": {IsSet: true, Value: []contactsync.Address{
				{Label: "home", Street: "1 Main St", City: "Springfield", Region: "IL", PostalCode: "62701", Country: "US"},
			}},
			"organization": {IsSet: true, Value: &contactsync.Organization{Name: "Acme", Title: "Engineer"}},
			"notes":        {IsSet: true, Value: notes},
			"nickname":     {IsSet: true, Value: nickname},
			"birthdate":    {IsSet: true, Value: birthdate},
		},
	}

	out := toGooglePerson(record, "etag")
	if len(out.EmailAddresses) != 1 || out.EmailAddresses[0].Value != "scott@example.com" || out.EmailAddresses[0].Type != "work" {
		t.Fatalf("EmailAddresses = %+v", out.EmailAddresses)
	}
	if len(out.PhoneNumbers) != 1 || out.PhoneNumbers[0].Value != "+1-555-0100" || out.PhoneNumbers[0].Type != "mobile" {
		t.Fatalf("PhoneNumbers = %+v", out.PhoneNumbers)
	}
	if len(out.Addresses) != 1 || out.Addresses[0].City != "Springfield" {
		t.Fatalf("Addresses = %+v", out.Addresses)
	}
	if len(out.Organizations) != 1 || out.Organizations[0].Name != "Acme" {
		t.Fatalf("Organizations = %+v", out.Organizations)
	}
	if len(out.Biographies) != 1 || out.Biographies[0].Value != notes {
		t.Fatalf("Biographies = %+v", out.Biographies)
	}
	if len(out.Nicknames) != 1 || out.Nicknames[0].Value != nickname {
		t.Fatalf("Nicknames = %+v", out.Nicknames)
	}
	if len(out.Birthdays) != 1 || out.Birthdays[0].Date == nil || out.Birthdays[0].Date.Year != 1989 || out.Birthdays[0].Date.Month != 4 || out.Birthdays[0].Date.Day != 19 {
		t.Fatalf("Birthdays = %+v", out.Birthdays)
	}
}

// TestToProviderRecordMapsNewContactFields is the inverse of the above: it
// verifies a Google API response is parsed back into the expected field
// values.
func TestToProviderRecordMapsNewContactFields(t *testing.T) {
	in := googlePerson{
		ResourceName: "people/c1",
		Names:        []googleName{{GivenName: "Scott", FamilyName: "Fridlund"}},
		Nicknames:    []googleNickname{{Value: "Ace"}},
		EmailAddresses: []googleEmailAddress{
			{Value: "scott@example.com", Type: "work"},
		},
		PhoneNumbers: []googlePhoneNumber{
			{Value: "+1-555-0100", Type: "mobile"},
		},
		Addresses: []googleAddress{
			{StreetAddress: "1 Main St", City: "Springfield", Region: "IL", PostalCode: "62701", Country: "US", Type: "home"},
		},
		Organizations: []googleOrganization{{Name: "Acme", Title: "Engineer"}},
		Biographies:   []googleBiography{{Value: "Met at a conference."}},
		Birthdays:     []googleBirthday{{Date: &googleDate{Year: 1989, Month: 4, Day: 19}}},
	}

	got := toProviderRecord(in)
	fields := got.Record.Fields

	if v := fieldLabeledValues(fields, "emails"); len(v) != 1 || v[0].Value != "scott@example.com" || v[0].Label != "work" {
		t.Fatalf("emails = %+v", v)
	}
	if v := fieldLabeledValues(fields, "phone_numbers"); len(v) != 1 || v[0].Value != "+1-555-0100" || v[0].Label != "mobile" {
		t.Fatalf("phone_numbers = %+v", v)
	}
	if v := fieldAddresses(fields, "addresses"); len(v) != 1 || v[0].City != "Springfield" {
		t.Fatalf("addresses = %+v", v)
	}
	if org := fieldOrganization(fields, "organization"); org == nil || org.Name != "Acme" {
		t.Fatalf("organization = %+v", org)
	}
	if v := fieldString(fields, "notes"); v != "Met at a conference." {
		t.Fatalf("notes = %q", v)
	}
	if v := fieldString(fields, "nickname"); v != "Ace" {
		t.Fatalf("nickname = %q", v)
	}
	if v := fieldString(fields, "birthdate"); v != "1989-04-19" {
		t.Fatalf("birthdate = %q", v)
	}
}

// TestToProviderRecordDedupesExactDuplicates guards a real production
// finding: a Google contact can carry genuine duplicate entries within a
// single source (observed live: the same mobile number, home email, and home
// address each listed twice on one contact). Google is the source of truth
// for that duplication, but we don't want to keep re-importing exact
// duplicates verbatim on every sync.
func TestToProviderRecordDedupesExactDuplicates(t *testing.T) {
	in := googlePerson{
		ResourceName: "people/c1",
		Names:        []googleName{{GivenName: "Scott", FamilyName: "Fridlund"}},
		EmailAddresses: []googleEmailAddress{
			{Value: "scott@example.com", Type: "home"},
			{Value: "work@example.com", Type: "work"},
			{Value: "scott@example.com", Type: "home"},
		},
		PhoneNumbers: []googlePhoneNumber{
			{Value: "+1-555-0100", Type: "mobile"},
			{Value: "+1-555-0100", Type: "mobile"},
		},
		Addresses: []googleAddress{
			{StreetAddress: "1 Main St", City: "Springfield", Type: "home"},
			{StreetAddress: "1 Main St", City: "Springfield", Type: "home"},
		},
	}

	fields := toProviderRecord(in).Record.Fields

	if v := fieldLabeledValues(fields, "emails"); len(v) != 2 {
		t.Fatalf("emails = %+v, want 2 (1 deduped home + 1 work)", v)
	}
	if v := fieldLabeledValues(fields, "phone_numbers"); len(v) != 1 {
		t.Fatalf("phone_numbers = %+v, want 1 deduped entry", v)
	}
	if v := fieldAddresses(fields, "addresses"); len(v) != 1 {
		t.Fatalf("addresses = %+v, want 1 deduped entry", v)
	}
}

// stubPersonService fails the test if any mutating method is called, so it
// can assert a code path is a pure no-op.
type stubPersonService struct {
	t *testing.T
}

func (s *stubPersonService) Get(context.Context, uuid.UUID) (*person.Person, error) {
	s.t.Fatal("unexpected Get call")
	return nil, nil
}

func (s *stubPersonService) Create(context.Context, person.CreateInput) (*person.Person, person.ValidationErrors, error) {
	s.t.Fatal("unexpected Create call")
	return nil, nil, nil
}

func (s *stubPersonService) Update(context.Context, uuid.UUID, person.UpdateInput) (*person.Person, person.ValidationErrors, error) {
	s.t.Fatal("unexpected Update call")
	return nil, nil, nil
}

func (s *stubPersonService) Delete(context.Context, uuid.UUID) error {
	s.t.Fatal("unexpected Delete call")
	return nil
}

func (s *stubPersonService) List(context.Context, person.ListParams) ([]person.Person, int, error) {
	s.t.Fatal("unexpected List call")
	return nil, 0, nil
}

// TestMergeRemoteRecordSkipsUnknownTombstone guards a real production
// incident: a Google contact deleted before we ever linked it arrives as a
// tombstone (Metadata.Deleted=true) with no local_id field, so remoteToLocal
// returns a nil local id. The old code treated "no local id" as "brand new
// contact" unconditionally, creating a local Person from the empty tombstone
// and then pushing it back to Google using the already-deleted resourceName,
// which 404s ("Requested entity was not found") and aborts the entire sync
// run for the account (observed live: account flipped to
// status=reconnect_required after every periodic sync).
func TestMergeRemoteRecordSkipsUnknownTombstone(t *testing.T) {
	adapter := &Adapter{people: &stubPersonService{t: t}}
	remote := contactsync.ProviderRecord{
		Record: contactsync.Record{
			ExternalID: "people/c6126766398320782690",
			Tombstone:  contactsync.Tombstone{Deleted: true},
			Fields:     map[string]contactsync.FieldState{},
		},
	}

	if err := adapter.mergeRemoteRecord(context.Background(), uuid.New(), contactsync.AuthSession{}, remote); err != nil {
		t.Fatalf("mergeRemoteRecord: %v", err)
	}
}

type fakeAccountStore struct{}

func (f *fakeAccountStore) Update(_ context.Context, a *contactsync.Account) (*contactsync.Account, error) {
	return a, nil
}

// TestMarkAccountFailedOnlyReconnectsOnAuthFailures guards against forcing
// users to re-authenticate for transient or record-specific sync failures
// (rate limits, a single bad record, Google 5xx, network blips): only
// genuinely missing/invalid/revoked OAuth credentials should ever set
// status=reconnect_required.
func TestMarkAccountFailedOnlyReconnectsOnAuthFailures(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus string
	}{
		{"generic error", errors.New("boom"), "error"},
		{"not found", &apiError{Status: 404, Body: "not found"}, "error"},
		{"server error", &apiError{Status: 500, Body: "oops"}, "error"},
		{"unauthorized", &apiError{Status: 401, Body: "invalid credentials"}, "reconnect_required"},
		{"missing token", &reauthRequiredError{errors.New("sync account is missing google access token")}, "reconnect_required"},
		{"refresh failed", &reauthRequiredError{errors.New("google oauth refresh failed: invalid_grant")}, "reconnect_required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &Adapter{accounts: &fakeAccountStore{}, logger: slog.Default()}
			account := &contactsync.Account{ID: uuid.New()}

			if err := adapter.markAccountFailed(context.Background(), account, tc.err); !errors.Is(err, tc.err) {
				t.Fatalf("markAccountFailed returned %v, want %v", err, tc.err)
			}
			if account.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", account.Status, tc.wantStatus)
			}
			if account.LastError == nil || *account.LastError != tc.err.Error() {
				t.Errorf("LastError = %v, want %q", account.LastError, tc.err.Error())
			}
		})
	}
}
