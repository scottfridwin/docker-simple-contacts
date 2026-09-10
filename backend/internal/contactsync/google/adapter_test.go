package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func (s *stubPersonService) ListRelationships(context.Context, uuid.UUID) ([]person.RelationshipView, error) {
	s.t.Fatal("unexpected ListRelationships call")
	return nil, nil
}

func (s *stubPersonService) ListIncomingRelationships(context.Context, uuid.UUID) ([]person.RelationshipView, error) {
	s.t.Fatal("unexpected ListIncomingRelationships call")
	return nil, nil
}

func (s *stubPersonService) ReplaceRelationships(context.Context, uuid.UUID, []person.RelationshipInput) error {
	s.t.Fatal("unexpected ReplaceRelationships call")
	return nil
}

func (s *stubPersonService) FindByDisplayName(context.Context, string) ([]person.Person, error) {
	s.t.Fatal("unexpected FindByDisplayName call")
	return nil, nil
}

func (s *stubPersonService) FindByExactName(context.Context, string, string) ([]person.Person, error) {
	s.t.Fatal("unexpected FindByExactName call")
	return nil, nil
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
	adapter := &Adapter{people: &stubPersonService{t: t}, logger: slog.Default()}
	remote := contactsync.ProviderRecord{
		Record: contactsync.Record{
			ExternalID: "people/c6126766398320782690",
			Tombstone:  contactsync.Tombstone{Deleted: true},
			Fields:     map[string]contactsync.FieldState{},
		},
	}

	var pending []pendingRelationship
	if err := adapter.mergeRemoteRecord(context.Background(), uuid.New(), contactsync.AuthSession{}, remote, &pending); err != nil {
		t.Fatalf("mergeRemoteRecord: %v", err)
	}
}

// TestMergeRemoteRecordDefersRelationshipReconciliation guards the two-pass
// sync fix: relationship reconciliation for an updated contact must not run
// inline inside mergeRemoteRecord (which processes one remote record at a
// time and so may not have created a referenced contact yet) - it must be
// queued in pending and only run after every record in the pull has been
// created/updated, so relationships can resolve to contacts synced later in
// the same run.
// noopLinkStore is a no-op recordLinkStore fake for tests that exercise
// mergeRemoteRecord's update path, which opportunistically self-heals the
// remote-record link on every merge.
type noopLinkStore struct{}

func (noopLinkStore) Get(context.Context, uuid.UUID, uuid.UUID) (*contactsync.RecordLink, error) {
	return nil, errors.New("not found")
}

func (noopLinkStore) Upsert(context.Context, *contactsync.RecordLink) error {
	return nil
}

func TestMergeRemoteRecordDefersRelationshipReconciliation(t *testing.T) {
	localID := uuid.New()
	local := &person.Person{ID: localID, FirstName: "Ada", LastName: "Lovelace", UpdatedAt: time.Now().Add(-time.Hour)}
	svc := &relPersonService{stubPersonService: stubPersonService{t: t}, getResult: local}
	adapter := &Adapter{people: svc, links: noopLinkStore{}, logger: slog.Default()}

	remote := contactsync.ProviderRecord{
		Record: contactsync.Record{
			ExternalID: "people/c1",
			Fields: map[string]contactsync.FieldState{
				"first_name":         {IsSet: true, Value: "Ada", UpdatedAt: time.Now()},
				"last_name":          {IsSet: true, Value: "Lovelace", UpdatedAt: time.Now()},
				"local_id":           {IsSet: true, Value: localID.String()},
				"_google_updated_at": {IsSet: true, Value: time.Now().UTC().Format(time.RFC3339Nano)},
				"relations": {IsSet: true, Value: []contactsync.LabeledValue{
					{Label: string(person.RelationSpouse), Value: "Not Yet Synced"},
				}},
			},
		},
	}

	var pending []pendingRelationship
	if err := adapter.mergeRemoteRecord(context.Background(), uuid.New(), contactsync.AuthSession{}, remote, &pending); err != nil {
		t.Fatalf("mergeRemoteRecord: %v", err)
	}

	if svc.replacedInputs != nil {
		t.Fatalf("ReplaceRelationships called eagerly during merge, want deferred: %+v", svc.replacedInputs)
	}
	if len(pending) != 1 || pending[0].PersonID != localID {
		t.Fatalf("pending = %+v, want one entry for %v", pending, localID)
	}

	adapter.reconcileRelationships(context.Background(), pending[0].PersonID, pending[0].Fields)
	if svc.replacedPersonID != localID || len(svc.replacedInputs) != 1 {
		t.Fatalf("expected reconciliation to run once triggered, got personID=%v inputs=%+v", svc.replacedPersonID, svc.replacedInputs)
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

func TestMapGoogleRelationType(t *testing.T) {
	cases := map[string]person.RelationType{
		"parent": person.RelationParent, "Mother": person.RelationParent, "FATHER": person.RelationParent,
		"child": person.RelationChild, "son": person.RelationChild, "daughter": person.RelationChild,
		"spouse":  person.RelationSpouse,
		"sibling": person.RelationSibling, "brother": person.RelationSibling, "sister": person.RelationSibling,
		"partner": person.RelationPartner, "domesticPartner": person.RelationPartner,
	}
	for input, want := range cases {
		got, ok := mapGoogleRelationType(input)
		if !ok || got != want {
			t.Errorf("mapGoogleRelationType(%q) = (%q, %v), want (%q, true)", input, got, ok, want)
		}
	}
	for _, unsupported := range []string{"friend", "relative", "manager", "assistant", "referredBy", "colleague", ""} {
		if _, ok := mapGoogleRelationType(unsupported); ok {
			t.Errorf("mapGoogleRelationType(%q) unexpectedly matched", unsupported)
		}
	}
}

// TestToProviderRecordMapsRelations guards the import mapping: supported
// Google relation types are carried through as our fixed enum, and
// unsupported types (no custom-type support) are silently skipped.
func TestToProviderRecordMapsRelations(t *testing.T) {
	in := googlePerson{
		ResourceName: "people/c1",
		Names:        []googleName{{GivenName: "Scott", FamilyName: "Fridlund"}},
		Relations: []googleRelation{
			{Person: "Jane Doe", Type: "spouse"},
			{Person: "Some Friend", Type: "friend"},
			{Person: "", Type: "sibling"},
		},
	}
	fields := toProviderRecord(in).Record.Fields
	got := fieldLabeledValues(fields, "relations")
	if len(got) != 1 || got[0].Label != string(person.RelationSpouse) || got[0].Value != "Jane Doe" {
		t.Fatalf("relations = %+v, want just the spouse relation", got)
	}
}

// TestToGooglePersonMapsRelations guards the export mapping: our stored
// (type, resolved name) pairs become Google's {person, type} relation shape.
func TestToGooglePersonMapsRelations(t *testing.T) {
	record := contactsync.Record{
		Fields: map[string]contactsync.FieldState{
			"first_name": {IsSet: true, Value: "Scott"},
			"last_name":  {IsSet: true, Value: "Fridlund"},
			"relations": {IsSet: true, Value: []contactsync.LabeledValue{
				{Label: string(person.RelationSpouse), Value: "Jane Doe"},
			}},
		},
	}
	out := toGooglePerson(record, "etag")
	if len(out.Relations) != 1 || out.Relations[0].Person != "Jane Doe" || out.Relations[0].Type != "spouse" {
		t.Fatalf("Relations = %+v", out.Relations)
	}
}

// relPersonService is a minimal personService fake for testing
// reconcileRelationships/attachRelationsForExport in isolation.
type relPersonService struct {
	stubPersonService
	byName           map[string][]person.Person
	relationships    []person.RelationshipView
	incoming         []person.RelationshipView
	getResult        *person.Person
	replacedPersonID uuid.UUID
	replacedInputs   []person.RelationshipInput
}

func (s *relPersonService) Get(_ context.Context, id uuid.UUID) (*person.Person, error) {
	if s.getResult != nil && s.getResult.ID == id {
		return s.getResult, nil
	}
	return nil, person.ErrNotFound
}

func (s *relPersonService) Update(_ context.Context, id uuid.UUID, _ person.UpdateInput) (*person.Person, person.ValidationErrors, error) {
	updated := *s.getResult
	updated.UpdatedAt = time.Now()
	return &updated, nil, nil
}

func (s *relPersonService) FindByDisplayName(_ context.Context, name string) ([]person.Person, error) {
	return s.byName[name], nil
}

func (s *relPersonService) ReplaceRelationships(_ context.Context, personID uuid.UUID, desired []person.RelationshipInput) error {
	s.replacedPersonID = personID
	s.replacedInputs = desired
	return nil
}

func (s *relPersonService) ListRelationships(context.Context, uuid.UUID) ([]person.RelationshipView, error) {
	return s.relationships, nil
}

func (s *relPersonService) ListIncomingRelationships(context.Context, uuid.UUID) ([]person.RelationshipView, error) {
	return s.incoming, nil
}

// TestReconcileRelationshipsLinksUniqueNameMatch guards the agreed matching
// policy: link to an existing contact only when its display name matches
// exactly and unambiguously; otherwise keep the relationship name-only so
// the information from Google isn't lost.
func TestReconcileRelationshipsLinksUniqueNameMatch(t *testing.T) {
	janeID := uuid.New()
	personID := uuid.New()
	svc := &relPersonService{stubPersonService: stubPersonService{t: t}, byName: map[string][]person.Person{
		"Jane Doe":    {{ID: janeID, DisplayName: "Jane Doe"}},
		"Common Name": {{ID: uuid.New()}, {ID: uuid.New()}}, // ambiguous
	}}
	adapter := &Adapter{people: svc, logger: slog.Default()}

	fields := map[string]contactsync.FieldState{
		"relations": {IsSet: true, Value: []contactsync.LabeledValue{
			{Label: string(person.RelationSpouse), Value: "Jane Doe"},
			{Label: string(person.RelationSibling), Value: "Common Name"},
			{Label: string(person.RelationPartner), Value: "Nobody Matches"},
		}},
	}
	adapter.reconcileRelationships(context.Background(), personID, fields)

	if svc.replacedPersonID != personID {
		t.Fatalf("ReplaceRelationships called for %v, want %v", svc.replacedPersonID, personID)
	}
	if len(svc.replacedInputs) != 3 {
		t.Fatalf("replacedInputs = %+v, want 3 entries", svc.replacedInputs)
	}
	if svc.replacedInputs[0].RelatedPersonID == nil || *svc.replacedInputs[0].RelatedPersonID != janeID {
		t.Errorf("expected unique match to be linked, got %+v", svc.replacedInputs[0])
	}
	if svc.replacedInputs[1].RelatedPersonID != nil {
		t.Errorf("expected ambiguous match to stay unlinked, got %+v", svc.replacedInputs[1])
	}
	if svc.replacedInputs[2].RelatedPersonID != nil || svc.replacedInputs[2].RelatedPersonName == nil {
		t.Errorf("expected no-match relation to stay unlinked with a name, got %+v", svc.replacedInputs[2])
	}
}

func TestAttachRelationsForExport(t *testing.T) {
	relatedID := uuid.New()
	svc := &relPersonService{stubPersonService: stubPersonService{t: t}, relationships: []person.RelationshipView{
		{Type: person.RelationChild, RelatedPersonID: &relatedID, RelatedPersonName: "Kid Name"},
	}}
	adapter := &Adapter{people: svc, logger: slog.Default()}

	record := contactsync.Record{Fields: map[string]contactsync.FieldState{}}
	record = adapter.attachRelationsForExport(context.Background(), uuid.New(), time.Now(), record)

	got := fieldLabeledValues(record.Fields, "relations")
	if len(got) != 1 || got[0].Label != string(person.RelationChild) || got[0].Value != "Kid Name" {
		t.Fatalf("relations = %+v", got)
	}
}

// TestReconcileRelationshipsDedupesSymmetricGoogleRelations guards the
// dedup fix: Google can report the same relationship on both contacts (A
// says "child: B", B says "parent: A"), but that must collapse to exactly
// one row in our schema. Whichever side is reconciled second sees the first
// side's row as an incoming (other-owned) relationship and must skip
// re-adding its own copy.
func TestReconcileRelationshipsDedupesSymmetricGoogleRelations(t *testing.T) {
	personID := uuid.New()
	bID := uuid.New()
	svc := &relPersonService{
		stubPersonService: stubPersonService{t: t},
		byName:            map[string][]person.Person{"Person B": {{ID: bID, DisplayName: "Person B"}}},
		incoming: []person.RelationshipView{
			{Type: person.RelationChild, RelatedPersonID: &bID, RelatedPersonName: "Person B"},
		},
	}
	adapter := &Adapter{people: svc, logger: slog.Default()}

	fields := map[string]contactsync.FieldState{
		"relations": {IsSet: true, Value: []contactsync.LabeledValue{
			{Label: string(person.RelationChild), Value: "Person B"},
		}},
	}
	adapter.reconcileRelationships(context.Background(), personID, fields)

	if len(svc.replacedInputs) != 0 {
		t.Fatalf("replacedInputs = %+v, want none (already represented from the other side)", svc.replacedInputs)
	}
}

// TestDoRetriesOn429ThenSucceeds guards the sync-abort-on-quota-error bug:
// a single "Quota exceeded ... RESOURCE_EXHAUSTED" response from Google must
// not fail the whole sync run - it should be retried (honoring Retry-After
// when Google sends one, so the test doesn't have to sleep out a full
// backoff window).
func TestDoRetriesOn429ThenSucceeds(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(googlePerson{ResourceName: "people/1", ETag: "etag-ok"})
	}))
	defer server.Close()

	adapter := &Adapter{cfg: Config{PeopleBaseURL: server.URL}, http: http.DefaultClient, logger: slog.Default()}
	got, err := adapter.getContact(context.Background(), "token", "people/1")
	if err != nil {
		t.Fatalf("getContact after one 429 retry: %v", err)
	}
	if got.ETag != "etag-ok" {
		t.Fatalf("got = %+v", got)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one 429 then one success)", attempts)
	}
}

// TestDoGivesUpAfterMaxAttemptsOn429 guards against retrying forever: a
// persistently rate-limited request must eventually return the 429 error
// rather than looping indefinitely.
func TestDoGivesUpAfterMaxAttemptsOn429(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`))
	}))
	defer server.Close()

	adapter := &Adapter{cfg: Config{PeopleBaseURL: server.URL}, http: http.DefaultClient, logger: slog.Default()}
	_, err := adapter.getContact(context.Background(), "token", "people/1")
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("expected a final 429 apiError, got %v", err)
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d, want 5 (maxAttempts)", attempts)
	}
}

// TestUpsertRecordRetriesOnceOnETagConflict guards the "Request person.etag
// is different than the current person.etag" FAILED_PRECONDITION error: it
// means the contact changed on Google's side between our read and our
// update, and must be retried once with a freshly-read etag instead of
// failing the whole sync run.
func TestUpsertRecordRetriesOnceOnETagConflict(t *testing.T) {
	getCalls, patchCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCalls++
			etag := fmt.Sprintf("etag-%d", getCalls)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(googlePerson{ResourceName: "people/1", ETag: etag})
		case http.MethodPatch:
			patchCalls++
			if patchCalls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":400,"status":"FAILED_PRECONDITION","message":"Request person.etag is different than the current person.etag. Clear local cache and get the latest person."}}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(googlePerson{ResourceName: "people/1", ETag: "etag-final"})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	adapter := &Adapter{cfg: Config{PeopleBaseURL: server.URL}, http: http.DefaultClient, logger: slog.Default()}
	session := contactsync.AuthSession{AccessToken: "token"}
	record := contactsync.Record{ExternalID: "people/1", Fields: map[string]contactsync.FieldState{
		"first_name": {IsSet: true, Value: "Ada"},
	}}

	out, err := adapter.UpsertRecord(context.Background(), session, record)
	if err != nil {
		t.Fatalf("UpsertRecord: %v", err)
	}
	if out.Record.ExternalID != "people/1" {
		t.Fatalf("out = %+v", out)
	}
	if getCalls != 2 {
		t.Fatalf("getCalls = %d, want 2 (initial read + re-read after conflict)", getCalls)
	}
	if patchCalls != 2 {
		t.Fatalf("patchCalls = %d, want 2 (failed attempt + retry)", patchCalls)
	}
}
