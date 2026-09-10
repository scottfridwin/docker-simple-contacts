package contactsync

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ChangeKind identifies the mutation that occurred for a Person record.
type ChangeKind string

const (
	ChangeKindCreated     ChangeKind = "created"
	ChangeKindUpdated     ChangeKind = "updated"
	ChangeKindDeleted     ChangeKind = "deleted"
	ChangeKindRestored    ChangeKind = "restored"
	ChangeKindHardDeleted ChangeKind = "hard_deleted"
)

// LabeledValue is a labeled scalar value, used for multi-value fields like
// emails and phone numbers (e.g. label "Home", value "example@example.com").
type LabeledValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Address is one labeled postal address.
type Address struct {
	Label      string `json:"label"`
	Street     string `json:"street"`
	City       string `json:"city"`
	Region     string `json:"region"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

// Organization describes a Person's employer/role.
type Organization struct {
	Name       string `json:"name,omitempty"`
	Title      string `json:"title,omitempty"`
	Department string `json:"department,omitempty"`
}

// PersonSnapshot is the provider-neutral shape used by the sync core.
type PersonSnapshot struct {
	ID           uuid.UUID      `json:"id"`
	OwnerID      *uuid.UUID     `json:"owner_id,omitempty"`
	FirstName    string         `json:"first_name"`
	MiddleNames  []string       `json:"middle_names"`
	LastName     string         `json:"last_name"`
	DisplayName  string         `json:"display_name"`
	Nickname     *string        `json:"nickname,omitempty"`
	Pronouns     *string        `json:"pronouns,omitempty"`
	Birthdate    *string        `json:"birthdate,omitempty"`
	Emails       []LabeledValue `json:"emails"`
	PhoneNumbers []LabeledValue `json:"phone_numbers"`
	Addresses    []Address      `json:"addresses"`
	Organization *Organization  `json:"organization,omitempty"`
	Notes        *string        `json:"notes,omitempty"`
	CustomFields map[string]any `json:"custom_fields"`
	Labels       []string       `json:"labels"`
	IsFavorite   bool           `json:"is_favorite"`
	DeletedAt    *time.Time     `json:"deleted_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// PersonChange is emitted when a Person record changes locally.
type PersonChange struct {
	Kind      ChangeKind
	Snapshot  PersonSnapshot
	ChangedAt time.Time
}

// Notifier receives local person mutations so a sync engine can enqueue work.
type Notifier interface {
	RecordChanged(context.Context, PersonChange) error
}
