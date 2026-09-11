package contactsync

import (
	"context"
	"time"
)

// Capabilities describes what a provider adapter can do.
type Capabilities struct {
	SupportsTwoWaySync      bool
	SupportsDeletes         bool
	SupportsIncrementalSync bool
	SupportsPartialMerge    bool
}

// AuthSession stores the provider-managed credential state for a sync account.
type AuthSession struct {
	ProviderAccountID string
	DisplayName       string
	AccessToken       string
	RefreshToken      string
	ExpiresAt         time.Time
	Scope             string
}

// AuthRequest captures the state needed to finish an authorization flow.
type AuthRequest struct {
	AuthorizationURL string
	State            string
}

// FieldState tracks a single normalized field and the last time it changed.
type FieldState struct {
	IsSet     bool
	Value     any
	UpdatedAt time.Time
}

// Tombstone tracks record-level deletion state.
type Tombstone struct {
	Deleted   bool
	UpdatedAt time.Time
}

// Record is the provider-neutral contact representation used by the sync core.
type Record struct {
	ExternalID string
	Tombstone  Tombstone
	Fields     map[string]FieldState
}

// ProviderRecord is the payload exchanged with provider adapters.
type ProviderRecord struct {
	Record Record
	ETag   string
	Cursor string
}

// ChangePage is a page of remote changes from a provider adapter.
type ChangePage struct {
	Records    []ProviderRecord
	NextCursor string
	HasMore    bool
}

// Adapter is the provider-specific contract used by the sync core.
type Adapter interface {
	ProviderName() string
	Capabilities() Capabilities
	BeginAuthorization(ctx context.Context, redirectURI string, state string) (AuthRequest, error)
	CompleteAuthorization(ctx context.Context, code string) (AuthSession, error)
	RefreshAuthorization(ctx context.Context, session AuthSession) (AuthSession, error)
	ListChanges(ctx context.Context, session AuthSession, cursor string) (ChangePage, error)
	FetchRecord(ctx context.Context, session AuthSession, remoteID string) (ProviderRecord, error)
	UpsertRecord(ctx context.Context, session AuthSession, record Record) (ProviderRecord, error)
	DeleteRecord(ctx context.Context, session AuthSession, remoteID string) error
	Sync(ctx context.Context, account Account, job Job) error
}
