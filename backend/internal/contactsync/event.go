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
	PhoneNumbers []string       `json:"phone_numbers"`
	CustomFields map[string]any `json:"custom_fields"`
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
