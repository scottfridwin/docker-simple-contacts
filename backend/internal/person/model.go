// Package person contains the Person domain model, validation, and storage.
package person

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

// Person is the single domain entity supported in v1.
type Person struct {
	ID           uuid.UUID      `json:"id"`
	FirstName    string         `json:"first_name"`
	MiddleNames  []string       `json:"middle_names"`
	LastName     string         `json:"last_name"`
	DisplayName  string         `json:"display_name"`
	Nickname     *string        `json:"nickname,omitempty"`
	Pronouns     *string        `json:"pronouns,omitempty"`
	Birthdate    *string        `json:"birthdate,omitempty"`
	PhoneNumbers []string       `json:"phone_numbers"`
	CustomFields map[string]any `json:"custom_fields"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    *time.Time     `json:"deleted_at,omitempty"`
}

// CreateInput is the payload accepted when creating a Person.
type CreateInput struct {
	FirstName    string         `json:"first_name"`
	MiddleNames  []string       `json:"middle_names"`
	LastName     string         `json:"last_name"`
	Nickname     *string        `json:"nickname"`
	Pronouns     *string        `json:"pronouns"`
	Birthdate    *string        `json:"birthdate"`
	PhoneNumbers []string       `json:"phone_numbers"`
	CustomFields map[string]any `json:"custom_fields"`
}

// UpdateInput is the payload accepted when patching a Person. Pointer fields and
// the presence flags distinguish "field omitted" from "field explicitly set".
type UpdateInput struct {
	FirstName    *string
	MiddleNames  *[]string
	LastName     *string
	Nickname     *string
	Pronouns     *string
	Birthdate    *string
	PhoneNumbers *[]string
	CustomFields map[string]any

	FirstNameSet    bool
	MiddleNamesSet  bool
	LastNameSet     bool
	NicknameSet     bool
	PronounsSet     bool
	BirthdateSet    bool
	PhoneNumbersSet bool
	CustomFieldsSet bool
}

// ListParams controls list filtering, sorting, and pagination.
type ListParams struct {
	Page      int
	PageSize  int
	SortField string
	SortDesc  bool
	FirstName string
	LastName  string
}

// DeriveDisplayName builds a display name from the name parts when the caller
// does not supply one. Middle names are joined in order.
func DeriveDisplayName(firstName string, middleNames []string, lastName string) string {
	parts := make([]string, 0, len(middleNames)+2)
	if firstName != "" {
		parts = append(parts, firstName)
	}
	for _, m := range middleNames {
		if strings.TrimSpace(m) != "" {
			parts = append(parts, m)
		}
	}
	if lastName != "" {
		parts = append(parts, lastName)
	}
	return strings.Join(parts, " ")
}

// Snapshot converts a Person into a provider-neutral sync snapshot.
func (p Person) Snapshot(ownerID *uuid.UUID) contactsync.PersonSnapshot {
	return contactsync.PersonSnapshot{
		ID:           p.ID,
		OwnerID:      ownerID,
		FirstName:    p.FirstName,
		MiddleNames:  append([]string(nil), p.MiddleNames...),
		LastName:     p.LastName,
		DisplayName:  p.DisplayName,
		Nickname:     p.Nickname,
		Pronouns:     p.Pronouns,
		Birthdate:    p.Birthdate,
		PhoneNumbers: append([]string(nil), p.PhoneNumbers...),
		CustomFields: cloneMap(p.CustomFields),
		DeletedAt:    p.DeletedAt,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
