package contactsync

import (
	"time"

	"github.com/google/uuid"
)

// Account stores the persisted configuration for a connected sync provider.
type Account struct {
	ID                   uuid.UUID  `json:"id"`
	OwnerID              *uuid.UUID `json:"owner_id,omitempty"`
	Provider             string     `json:"provider"`
	ProviderAccountID    string     `json:"provider_account_id"`
	AccessToken          *string    `json:"-"`
	RefreshToken         *string    `json:"-"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	Scope                string     `json:"scope"`
	SyncCursor           string     `json:"sync_cursor"`
	SyncFrequencyMinutes int        `json:"sync_frequency_minutes"`
	Status               string     `json:"status"`
	LastSyncedAt         *time.Time `json:"last_synced_at,omitempty"`
	LastError            *string    `json:"last_error,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}
