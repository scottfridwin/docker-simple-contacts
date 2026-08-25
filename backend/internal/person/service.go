package person

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// store abstracts the persistence operations the service depends on.
type store interface {
	Create(ctx context.Context, p *Person) (*Person, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Person, error)
	List(ctx context.Context, params ListParams) ([]Person, int, error)
	Update(ctx context.Context, id uuid.UUID, p *Person) (*Person, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
	PurgeExpired(ctx context.Context, olderThan time.Duration) (int64, error)
}

// Service holds the Person business logic.
type Service struct {
	repo store
}

// NewService constructs a Service.
func NewService(repo store) *Service {
	return &Service{repo: repo}
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
	customFields := in.CustomFields
	if customFields == nil {
		customFields = map[string]any{}
	}

	p := &Person{
		FirstName:    in.FirstName,
		MiddleNames:  middleNames,
		LastName:     in.LastName,
		DisplayName:  resolveDisplayName(in.DisplayName, in.FirstName, middleNames, in.LastName),
		CustomFields: customFields,
	}
	created, err := s.repo.Create(ctx, p)
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
	return updated, nil, err
}

// Delete soft-deletes a Person.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.SoftDelete(ctx, id)
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
	if in.CustomFieldsSet {
		if in.CustomFields != nil {
			current.CustomFields = in.CustomFields
		} else {
			current.CustomFields = map[string]any{}
		}
	}
	// Display name: explicit non-empty value wins; an explicit empty string
	// re-derives from the current name parts.
	if in.DisplayNameSet {
		if in.DisplayName != nil && *in.DisplayName != "" {
			current.DisplayName = *in.DisplayName
		} else {
			current.DisplayName = DeriveDisplayName(current.FirstName, current.MiddleNames, current.LastName)
		}
	}
}

func resolveDisplayName(explicit *string, firstName string, middleNames []string, lastName string) string {
	if explicit != nil && *explicit != "" {
		return *explicit
	}
	return DeriveDisplayName(firstName, middleNames, lastName)
}
