package contactsync

import (
	"context"
	"fmt"
	"strings"
)

// AdapterRegistry resolves provider names to concrete adapters.
type AdapterRegistry map[string]Adapter

// Register adds or replaces an adapter under its provider name.
func (r AdapterRegistry) Register(adapter Adapter) {
	if r == nil || adapter == nil {
		return
	}
	r[strings.ToLower(strings.TrimSpace(adapter.ProviderName()))] = adapter
}

// Lookup resolves a provider name to an adapter.
func (r AdapterRegistry) Lookup(provider string) (Adapter, bool) {
	if len(r) == 0 {
		return nil, false
	}
	adapter, ok := r[strings.ToLower(strings.TrimSpace(provider))]
	return adapter, ok
}

// DispatchProcessor routes jobs to provider-specific adapters.
type DispatchProcessor struct {
	registry AdapterRegistry
}

// NewDispatchProcessor constructs a provider-dispatch processor.
func NewDispatchProcessor(registry AdapterRegistry) *DispatchProcessor {
	if registry == nil {
		registry = AdapterRegistry{}
	}
	return &DispatchProcessor{registry: registry}
}

// Process sends the queued job to the matching provider adapter.
func (p *DispatchProcessor) Process(ctx context.Context, account Account, job Job) error {
	adapter, ok := p.registry.Lookup(account.Provider)
	if !ok {
		return fmt.Errorf("no adapter registered for provider %q", account.Provider)
	}
	if err := adapter.Sync(ctx, account, job); err != nil {
		return fmt.Errorf("provider %s sync failed: %w", adapter.ProviderName(), err)
	}
	return nil
}
