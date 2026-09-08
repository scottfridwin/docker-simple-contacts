package contactsync

import (
	"time"

	"github.com/google/uuid"
)

// Account stores the persisted configuration for a connected sync provider.
type Account struct {
	ID                   uuid.UUID
	OwnerID              *uuid.UUID
	Provider             string
	ProviderAccountID    string
	AccessToken          *string
	RefreshToken         *string
	ExpiresAt            *time.Time
	Scope                string
	SyncCursor           string
	SyncFrequencyMinutes int
	Status               string
	LastSyncedAt         *time.Time
	LastError            *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}
