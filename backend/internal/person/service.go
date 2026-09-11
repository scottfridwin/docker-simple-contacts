package person

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

// store abstracts the persistence operations the service depends on.
type store interface {
	Create(ctx context.Context, p *Person) (*Person, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Person, error)
	GetAccessible(ctx context.Context, id uuid.UUID) (*Person, error)
	GetDeletedByID(ctx context.Context, id uuid.UUID) (*Person, error)
	List(ctx context.Context, params ListParams) ([]Person, int, error)
	ListDeleted(ctx context.Context, params ListParams) ([]Person, int, error)
	Update(ctx context.Context, id uuid.UUID, p *Person) (*Person, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) error
	HardDelete(ctx context.Context, id uuid.UUID) error
	PurgeExpired(ctx context.Context, olderThan time.Duration) ([]Person, error)
	CreateRelationship(ctx context.Context, personID uuid.UUID, in RelationshipInput) (*RelationshipView, error)
	ListRelationships(ctx context.Context, personID uuid.UUID) ([]RelationshipView, error)
	ListIncomingRelationships(ctx context.Context, personID uuid.UUID) ([]RelationshipView, error)
	DeleteRelationship(ctx context.Context, personID, relationshipID uuid.UUID) error
	ReplaceRelationships(ctx context.Context, personID uuid.UUID, desired []RelationshipInput) error
	FindByDisplayName(ctx context.Context, name string) ([]Person, error)
	FindByExactName(ctx context.Context, firstName, lastName string) ([]Person, error)
	CreateShare(ctx context.Context, personID uuid.UUID, email string) (*Share, error)
	ListShares(ctx context.Context, personID uuid.UUID) ([]Share, error)
	DeleteShare(ctx context.Context, personID, shareID uuid.UUID) error
	DeleteShareByRecipient(ctx context.Context, personID uuid.UUID) error
}

// syncNotifier is implemented by the sync engine to enqueue follow-up work.
type syncNotifier interface {
	RecordChanged(context.Context, contactsync.PersonChange) error
}

// Service holds the Person business logic.
type Service struct {
	repo     store
	notifier syncNotifier
}

// NewService constructs a Service.
func NewService(repo store, notifier ...syncNotifier) *Service {
	svc := &Service{repo: repo}
	if len(notifier) > 0 {
		svc.notifier = notifier[0]
	}
	return svc
}

// Create validates the input, derives the display name when absent, and stores
// the Person.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Person, ValidationErrors, error) {
	if errs := ValidateCreate(in); errs.HasErrors() {
		return nil, errs, nil
	}

	middleNames := in.MiddleNames
	if middleNames == nil {
		middleNames = []string{}
	}
	phoneNumbers := in.PhoneNumbers
	if phoneNumbers == nil {
		phoneNumbers = []contactsync.LabeledValue{}
	}
	emails := in.Emails
	if emails == nil {
		emails = []contactsync.LabeledValue{}
	}
	addresses := in.Addresses
	if addresses == nil {
		addresses = []contactsync.Address{}
	}
	customFields := in.CustomFields
	if customFields == nil {
		customFields = map[string]any{}
	}
	labels := in.Labels
	if labels == nil {
		labels = []string{}
	}

	p := &Person{
		FirstName:    in.FirstName,
		MiddleNames:  middleNames,
		LastName:     in.LastName,
		DisplayName:  DeriveDisplayName(in.FirstName, middleNames, in.LastName),
		Nickname:     in.Nickname,
		Pronouns:     in.Pronouns,
		Birthdate:    in.Birthdate,
		Emails:       emails,
		PhoneNumbers: phoneNumbers,
		Addresses:    addresses,
		Organization: in.Organization,
		Notes:        in.Notes,
		CustomFields: customFields,
		Labels:       labels,
		IsFavorite:   in.IsFavorite,
	}
	created, err := s.repo.Create(ctx, p)
	if err == nil {
		s.notify(ctx, contactsync.ChangeKindCreated, created)
	}
	return created, nil, err
}

// Get returns a single Person by ID, whether owned or shared with the
// current account.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Person, error) {
	return s.repo.GetAccessible(ctx, id)
}

// List returns a page of Persons and the total count.
func (s *Service) List(ctx context.Context, params ListParams) ([]Person, int, error) {
	return s.repo.List(ctx, params)
}

// ListDeleted returns soft-deleted Persons for the recycle bin.
func (s *Service) ListDeleted(ctx context.Context, params ListParams) ([]Person, int, error) {
	return s.repo.ListDeleted(ctx, params)
}

// Update validates and applies a patch to an existing Person. Allowed for
// the owner or anyone the Person has been shared with (view+edit access).
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Person, ValidationErrors, error) {
	if errs := ValidateUpdate(in); errs.HasErrors() {
		return nil, errs, nil
	}

	current, err := s.repo.GetAccessible(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	before := *current
	applyUpdate(current, in)
	if reflect.DeepEqual(before, *current) {
		// A no-op edit (every field already matches) must not bump
		// updated_at or raise a sync job. Without this, a remote-triggered
		// merge whose data already matches what's stored locally (e.g. one
		// linked account's own earlier export bouncing back on the other
		// account's next pull) would keep resetting updated_at and
		// notifying sync every single time - an infinite ping-pong between
		// two linked/shared accounts that never converges, since each
		// side's harmless re-application looks like a fresh edit to the
		// other.
		return current, nil, nil
	}

	updated, err := s.repo.Update(ctx, id, current)
	if err == nil {
		// Ownership doesn't change from an edit; carry it over rather than
		// re-querying it.
		updated.IsOwner = current.IsOwner
		updated.OwnerDisplayName = current.OwnerDisplayName
		s.notify(ctx, contactsync.ChangeKindUpdated, updated)
	}
	return updated, nil, err
}

// Delete soft-deletes a Person.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		return err
	}
	current.DeletedAt = timePtr(time.Now())
	s.notify(ctx, contactsync.ChangeKindDeleted, current)
	return nil
}

// Restore makes a soft-deleted Person visible again.
func (s *Service) Restore(ctx context.Context, id uuid.UUID) error {
	deleted, err := s.repo.GetDeletedByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Restore(ctx, id); err != nil {
		return err
	}
	deleted.DeletedAt = nil
	s.notify(ctx, contactsync.ChangeKindRestored, deleted)
	return nil
}

// HardDelete permanently removes a soft-deleted Person.
func (s *Service) HardDelete(ctx context.Context, id uuid.UUID) error {
	deleted, err := s.repo.GetDeletedByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.HardDelete(ctx, id); err != nil {
		return err
	}
	s.notify(ctx, contactsync.ChangeKindHardDeleted, deleted)
	return nil
}

// PurgeExpired removes soft-deleted records older than the retention window.
// Each purged record is notified the same as an explicit HardDelete, so the
// corresponding remote contact (if any) is deleted on every synced account
// too - otherwise a purged contact's stale contacts_local_id tag would make
// it look "missing locally" on the next pull and get silently recreated.
func (s *Service) PurgeExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	purged, err := s.repo.PurgeExpired(ctx, olderThan)
	if err != nil {
		return 0, err
	}
	for i := range purged {
		s.notify(ctx, contactsync.ChangeKindHardDeleted, &purged[i])
	}
	return int64(len(purged)), nil
}

// CreateRelationship validates and stores a relationship from personID's
// perspective, first confirming personID itself exists (and is owned by the
// caller).
func (s *Service) CreateRelationship(ctx context.Context, personID uuid.UUID, in RelationshipInput) (*RelationshipView, ValidationErrors, error) {
	if _, err := s.repo.GetByID(ctx, personID); err != nil {
		return nil, nil, err
	}
	if errs := ValidateRelationshipInput(personID, in); errs.HasErrors() {
		return nil, errs, nil
	}
	view, err := s.repo.CreateRelationship(ctx, personID, in)
	if errors.Is(err, ErrRelatedPersonNotFound) {
		return nil, ValidationErrors{{Field: "related_person_id", Message: "related person not found"}}, nil
	}
	if errors.Is(err, ErrRelationshipExists) {
		return nil, ValidationErrors{{Field: "type", Message: "this relationship already exists"}}, nil
	}
	if errors.Is(err, ErrTooManyRelationships) {
		return nil, ValidationErrors{{Field: "type", Message: ErrTooManyRelationships.Error()}}, nil
	}
	return view, nil, err
}

// ListRelationships returns every relationship involving personID.
func (s *Service) ListRelationships(ctx context.Context, personID uuid.UUID) ([]RelationshipView, error) {
	if _, err := s.repo.GetByID(ctx, personID); err != nil {
		return nil, err
	}
	return s.repo.ListRelationships(ctx, personID)
}

// ListIncomingRelationships returns only the relationships someone else
// created that name personID as the related person. Used by sync adapters
// to deduplicate relationships reported symmetrically by an external
// provider.
func (s *Service) ListIncomingRelationships(ctx context.Context, personID uuid.UUID) ([]RelationshipView, error) {
	return s.repo.ListIncomingRelationships(ctx, personID)
}

// DeleteRelationship removes a relationship visible from personID's side.
func (s *Service) DeleteRelationship(ctx context.Context, personID, relationshipID uuid.UUID) error {
	return s.repo.DeleteRelationship(ctx, personID, relationshipID)
}

// ReplaceRelationships replaces every relationship personID created with the
// given set. Used by sync adapters to reconcile relationships pulled from an
// external provider on each sync.
func (s *Service) ReplaceRelationships(ctx context.Context, personID uuid.UUID, desired []RelationshipInput) error {
	return s.repo.ReplaceRelationships(ctx, personID, desired)
}

// FindByDisplayName returns every owner-scoped Person with an exact
// display_name match. Used by sync adapters to resolve a provider-supplied
// relationship name to a local contact when possible.
func (s *Service) FindByDisplayName(ctx context.Context, name string) ([]Person, error) {
	return s.repo.FindByDisplayName(ctx, name)
}

// FindByExactName returns every owner-scoped Person whose first and last
// name match exactly. Used by sync adapters to resolve a newly-seen
// provider contact to an existing local contact before creating a
// duplicate.
func (s *Service) FindByExactName(ctx context.Context, firstName, lastName string) ([]Person, error) {
	return s.repo.FindByExactName(ctx, firstName, lastName)
}

// CreateShare grants another account (looked up by exact email match)
// view+edit access to personID. Only the owner may share a Person -
// re-sharing a Person already shared with you is not permitted.
func (s *Service) CreateShare(ctx context.Context, personID uuid.UUID, email string) (*Share, ValidationErrors, error) {
	if _, err := s.repo.GetByID(ctx, personID); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(email) == "" {
		return nil, ValidationErrors{{Field: "email", Message: "email is required"}}, nil
	}
	if !emailPattern.MatchString(strings.TrimSpace(email)) {
		return nil, ValidationErrors{{Field: "email", Message: "must be a valid email address"}}, nil
	}
	share, err := s.repo.CreateShare(ctx, personID, email)
	if errors.Is(err, ErrShareUserNotFound) {
		return nil, ValidationErrors{{Field: "email", Message: "no account found for that email - ask them to log in to Contacts at least once first"}}, nil
	}
	if errors.Is(err, ErrShareExists) {
		return nil, ValidationErrors{{Field: "email", Message: "already shared with this person"}}, nil
	}
	if errors.Is(err, ErrCannotShareWithSelf) {
		return nil, ValidationErrors{{Field: "email", Message: "cannot share a person with yourself"}}, nil
	}
	if errors.Is(err, ErrTooManyShares) {
		return nil, ValidationErrors{{Field: "email", Message: ErrTooManyShares.Error()}}, nil
	}
	return share, nil, err
}

// ListShares returns everyone personID is currently shared with. Owner-only.
func (s *Service) ListShares(ctx context.Context, personID uuid.UUID) ([]Share, error) {
	if _, err := s.repo.GetByID(ctx, personID); err != nil {
		return nil, err
	}
	return s.repo.ListShares(ctx, personID)
}

// DeleteShare revokes a share. Owner-only.
func (s *Service) DeleteShare(ctx context.Context, personID, shareID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, personID); err != nil {
		return err
	}
	return s.repo.DeleteShare(ctx, personID, shareID)
}

// LeaveShare lets a recipient remove their own access to a Person shared
// with them, without needing the owner to revoke it. Deliberately uses
// GetAccessible (not the strict owner-only GetByID), since the caller here
// is expected to be the recipient, not the owner.
func (s *Service) LeaveShare(ctx context.Context, personID uuid.UUID) error {
	if _, err := s.repo.GetAccessible(ctx, personID); err != nil {
		return err
	}
	return s.repo.DeleteShareByRecipient(ctx, personID)
}

func applyUpdate(current *Person, in UpdateInput) {
	if in.FirstNameSet && in.FirstName != nil {
		current.FirstName = *in.FirstName
	}
	if in.LastNameSet && in.LastName != nil {
		current.LastName = *in.LastName
	}
	if in.MiddleNamesSet {
		if in.MiddleNames != nil {
			current.MiddleNames = *in.MiddleNames
		} else {
			current.MiddleNames = []string{}
		}
	}
	if in.NicknameSet {
		current.Nickname = in.Nickname
	}
	if in.PronounsSet {
		current.Pronouns = in.Pronouns
	}
	if in.BirthdateSet {
		current.Birthdate = in.Birthdate
	}
	if in.PhoneNumbersSet {
		if in.PhoneNumbers != nil {
			current.PhoneNumbers = *in.PhoneNumbers
		} else {
			current.PhoneNumbers = []contactsync.LabeledValue{}
		}
	}
	if in.EmailsSet {
		if in.Emails != nil {
			current.Emails = *in.Emails
		} else {
			current.Emails = []contactsync.LabeledValue{}
		}
	}
	if in.AddressesSet {
		if in.Addresses != nil {
			current.Addresses = *in.Addresses
		} else {
			current.Addresses = []contactsync.Address{}
		}
	}
	if in.OrganizationSet {
		current.Organization = in.Organization
	}
	if in.NotesSet {
		current.Notes = in.Notes
	}
	if in.CustomFieldsSet {
		if in.CustomFields != nil {
			current.CustomFields = in.CustomFields
		} else {
			current.CustomFields = map[string]any{}
		}
	}
	if in.LabelsSet {
		if in.Labels != nil {
			current.Labels = *in.Labels
		} else {
			current.Labels = []string{}
		}
	}
	if in.IsFavoriteSet && in.IsFavorite != nil {
		current.IsFavorite = *in.IsFavorite
	}
	// Always re-derive display name from current name parts.
	current.DisplayName = DeriveDisplayName(current.FirstName, current.MiddleNames, current.LastName)
}

func (s *Service) notify(ctx context.Context, kind contactsync.ChangeKind, p *Person) {
	if s.notifier == nil || p == nil {
		return
	}
	ownerID, ok := authn.UserID(ctx)
	if !ok && p.OwnerID != nil {
		// Background/system-initiated changes (e.g. the retention purge
		// loop) run without an authenticated actor in ctx. Fall back to the
		// Person's own owner so the resulting job still fans out to at
		// least the owner's accounts, instead of an unscoped Job.OwnerID
		// making the account lookup fall through to every connected
		// account system-wide.
		ownerID = *p.OwnerID
		ok = true
	}
	var ownerPtr *uuid.UUID
	if ok && ownerID != uuid.Nil {
		ownerPtr = &ownerID
	}
	change := contactsync.PersonChange{
		Kind:      kind,
		Snapshot:  p.Snapshot(ownerPtr),
		ChangedAt: time.Now(),
	}
	// A change pulled in from a sync account must still propagate onward to
	// any OTHER linked/shared accounts (e.g. a contact edited directly in
	// G1 should still reach G2) - it must not be dropped entirely just
	// because it originated from sync reconciliation. It only needs to
	// exclude the account it came from, which Runner.runJob does using
	// OriginAccountID, so the change doesn't get pushed straight back to
	// where it just came from and loop forever.
	if originID, ok := contactsync.SyncOriginAccountID(ctx); ok {
		change.OriginAccountID = &originID
	}
	_ = s.notifier.RecordChanged(ctx, change)
}

func timePtr(t time.Time) *time.Time { return &t }
