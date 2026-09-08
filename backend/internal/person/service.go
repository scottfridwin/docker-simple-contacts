package person

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

// store abstracts the persistence operations the service depends on.
type store interface {
	Create(ctx context.Context, p *Person) (*Person, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Person, error)
	GetDeletedByID(ctx context.Context, id uuid.UUID) (*Person, error)
	List(ctx context.Context, params ListParams) ([]Person, int, error)
	ListDeleted(ctx context.Context, params ListParams) ([]Person, int, error)
	Update(ctx context.Context, id uuid.UUID, p *Person) (*Person, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) error
	HardDelete(ctx context.Context, id uuid.UUID) error
	PurgeExpired(ctx context.Context, olderThan time.Duration) (int64, error)
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
		phoneNumbers = []string{}
	}
	customFields := in.CustomFields
	if customFields == nil {
		customFields = map[string]any{}
	}

	p := &Person{
		FirstName:    in.FirstName,
		MiddleNames:  middleNames,
		LastName:     in.LastName,
		DisplayName:  DeriveDisplayName(in.FirstName, middleNames, in.LastName),
		Nickname:     in.Nickname,
		Pronouns:     in.Pronouns,
		Birthdate:    in.Birthdate,
		PhoneNumbers: phoneNumbers,
		CustomFields: customFields,
	}
	created, err := s.repo.Create(ctx, p)
	if err == nil {
		s.notify(ctx, contactsync.ChangeKindCreated, created)
	}
	return created, nil, err
}

// Get returns a single Person by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Person, error) {
	return s.repo.GetByID(ctx, id)
}

// List returns a page of Persons and the total count.
func (s *Service) List(ctx context.Context, params ListParams) ([]Person, int, error) {
	return s.repo.List(ctx, params)
}

// ListDeleted returns soft-deleted Persons for the recycle bin.
func (s *Service) ListDeleted(ctx context.Context, params ListParams) ([]Person, int, error) {
	return s.repo.ListDeleted(ctx, params)
}

// Update validates and applies a patch to an existing Person.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Person, ValidationErrors, error) {
	if errs := ValidateUpdate(in); errs.HasErrors() {
		return nil, errs, nil
	}

	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	applyUpdate(current, in)
	updated, err := s.repo.Update(ctx, id, current)
	if err == nil {
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
func (s *Service) PurgeExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.repo.PurgeExpired(ctx, olderThan)
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
			current.PhoneNumbers = []string{}
		}
	}
	if in.CustomFieldsSet {
		if in.CustomFields != nil {
			current.CustomFields = in.CustomFields
		} else {
			current.CustomFields = map[string]any{}
		}
	}
	// Always re-derive display name from current name parts.
	current.DisplayName = DeriveDisplayName(current.FirstName, current.MiddleNames, current.LastName)
}

func (s *Service) notify(ctx context.Context, kind contactsync.ChangeKind, p *Person) {
	if s.notifier == nil || p == nil {
		return
	}
	if contactsync.IsSyncOrigin(ctx) {
		return
	}
	ownerID, _ := authn.UserID(ctx)
	var ownerPtr *uuid.UUID
	if ownerID != uuid.Nil {
		ownerPtr = &ownerID
	}
	_ = s.notifier.RecordChanged(ctx, contactsync.PersonChange{
		Kind:      kind,
		Snapshot:  p.Snapshot(ownerPtr),
		ChangedAt: time.Now(),
	})
}

func timePtr(t time.Time) *time.Time { return &t }
