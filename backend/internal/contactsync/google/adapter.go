package google

import (
	"context"
	"errors"
	"fmt"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

// Adapter is the first provider target for the sync framework.
type Adapter struct{}

// NewAdapter constructs a Google adapter placeholder.
func NewAdapter() *Adapter {
	return &Adapter{}
}

// ProviderName returns the provider key used by the sync registry.
func (a *Adapter) ProviderName() string { return "google" }

// Capabilities reports the initial Google sync capabilities.
func (a *Adapter) Capabilities() contactsync.Capabilities {
	return contactsync.Capabilities{
		SupportsTwoWaySync:      true,
		SupportsDeletes:         true,
		SupportsIncrementalSync: true,
		SupportsPartialMerge:    true,
	}
}

// BeginAuthorization is not implemented yet.
func (a *Adapter) BeginAuthorization(context.Context, string, string) (contactsync.AuthRequest, error) {
	return contactsync.AuthRequest{}, errors.New("google sync not implemented yet")
}

// CompleteAuthorization is not implemented yet.
func (a *Adapter) CompleteAuthorization(context.Context, string) (contactsync.AuthSession, error) {
	return contactsync.AuthSession{}, errors.New("google sync not implemented yet")
}

// RefreshAuthorization is not implemented yet.
func (a *Adapter) RefreshAuthorization(context.Context, contactsync.AuthSession) (contactsync.AuthSession, error) {
	return contactsync.AuthSession{}, errors.New("google sync not implemented yet")
}

// ListChanges is not implemented yet.
func (a *Adapter) ListChanges(context.Context, contactsync.AuthSession, string) (contactsync.ChangePage, error) {
	return contactsync.ChangePage{}, errors.New("google sync not implemented yet")
}

// FetchRecord is not implemented yet.
func (a *Adapter) FetchRecord(context.Context, contactsync.AuthSession, string) (contactsync.ProviderRecord, error) {
	return contactsync.ProviderRecord{}, errors.New("google sync not implemented yet")
}

// UpsertRecord is not implemented yet.
func (a *Adapter) UpsertRecord(context.Context, contactsync.AuthSession, contactsync.Record) (contactsync.ProviderRecord, error) {
	return contactsync.ProviderRecord{}, errors.New("google sync not implemented yet")
}

// DeleteRecord is not implemented yet.
func (a *Adapter) DeleteRecord(context.Context, contactsync.AuthSession, string) error {
	return errors.New("google sync not implemented yet")
}

// Sync is not implemented yet.
func (a *Adapter) Sync(context.Context, contactsync.Account, contactsync.Job) error {
	return fmt.Errorf("google sync not implemented yet")
}
