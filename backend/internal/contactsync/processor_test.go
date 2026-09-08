package contactsync

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeAdapter struct {
	provider string
	err      error
}

func (a *fakeAdapter) ProviderName() string       { return a.provider }
func (a *fakeAdapter) Capabilities() Capabilities { return Capabilities{} }
func (a *fakeAdapter) BeginAuthorization(context.Context, string, string) (AuthRequest, error) {
	return AuthRequest{}, nil
}
func (a *fakeAdapter) CompleteAuthorization(context.Context, string) (AuthSession, error) {
	return AuthSession{}, nil
}
func (a *fakeAdapter) RefreshAuthorization(context.Context, AuthSession) (AuthSession, error) {
	return AuthSession{}, nil
}
func (a *fakeAdapter) ListChanges(context.Context, AuthSession, string) (ChangePage, error) {
	return ChangePage{}, nil
}
func (a *fakeAdapter) FetchRecord(context.Context, AuthSession, string) (ProviderRecord, error) {
	return ProviderRecord{}, nil
}
func (a *fakeAdapter) UpsertRecord(context.Context, AuthSession, Record) (ProviderRecord, error) {
	return ProviderRecord{}, nil
}
func (a *fakeAdapter) DeleteRecord(context.Context, AuthSession, string) error { return nil }
func (a *fakeAdapter) Sync(context.Context, Account, Job) error                { return a.err }

func TestDispatchProcessorRoutesByProvider(t *testing.T) {
	registry := AdapterRegistry{}
	registry.Register(&fakeAdapter{provider: "google"})
	processor := NewDispatchProcessor(registry)
	jobID := uuid.New()
	if err := processor.Process(context.Background(), Account{Provider: "google"}, Job{ID: jobID}); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestDispatchProcessorMissingProvider(t *testing.T) {
	processor := NewDispatchProcessor(nil)
	err := processor.Process(context.Background(), Account{Provider: "apple"}, Job{ID: uuid.New()})
	if err == nil {
		t.Fatal("expected missing provider error")
	}
	if !strings.Contains(err.Error(), "no adapter registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}
