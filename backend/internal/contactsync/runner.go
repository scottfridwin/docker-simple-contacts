package contactsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

type accountStore interface {
	List(context.Context, int) ([]Account, error)
	ListDue(context.Context, time.Time) ([]Account, error)
	ListLinkedToPerson(context.Context, uuid.UUID) ([]Account, error)
}

type jobQueue interface {
	ListPending(context.Context, int) ([]Job, error)
	MarkDone(context.Context, uuid.UUID) error
	MarkFailed(context.Context, uuid.UUID, string) error
}

// Processor applies one queued job to one connected sync account.
type Processor interface {
	Process(context.Context, Account, Job) error
}

// Runner drains queued sync jobs and dispatches them to connected accounts.
type Runner struct {
	accounts  accountStore
	jobs      jobQueue
	processor Processor
	batchSize int
	logger    *slog.Logger
}

// NewRunner constructs a Runner. A nil logger falls back to slog.Default().
func NewRunner(accounts accountStore, jobs jobQueue, processor Processor, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{accounts: accounts, jobs: jobs, processor: processor, batchSize: 50, logger: logger}
}

// RunOnce processes one batch of pending jobs.
func (r *Runner) RunOnce(ctx context.Context) (int, error) {
	if r == nil || r.jobs == nil || r.accounts == nil || r.processor == nil {
		return 0, nil
	}
	jobs, err := r.jobs.ListPending(ctx, r.batchSize)
	if err != nil {
		return 0, fmt.Errorf("loading pending sync jobs: %w", err)
	}
	processed := 0
	for _, job := range jobs {
		if err := r.runJob(ctx, job); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// RunLoop keeps executing sync jobs until the context is canceled. A job or
// account failure is logged but never stops the loop - previously any single
// processing error caused RunLoop to return, which permanently killed all
// background sync (both job-triggered and periodic) until the app restarted.
func (r *Runner) RunLoop(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		r.tick(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// tick runs one iteration of job processing and due-account reconciliation,
// recovering from any panic so this goroutine (and therefore the whole
// process, since nothing else supervises it) never dies from an unexpected
// panic deep in a provider adapter.
func (r *Runner) tick(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("recovered from panic in sync loop", "panic", rec)
		}
	}()
	if _, err := r.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Error("sync job processing failed", "error", err)
	}
	r.runDueAccounts(ctx)
}

// runDueAccounts reconciles connected accounts whose configured sync
// frequency has elapsed, independent of any queued local-change job. Without
// this, a connected account only ever pulls remote changes once (right after
// connecting) and never again unless a local contact happens to change.
func (r *Runner) runDueAccounts(ctx context.Context) {
	if r == nil || r.accounts == nil || r.processor == nil {
		return
	}
	now := time.Now()
	accounts, err := r.accounts.ListDue(ctx, now)
	if err != nil {
		r.logger.Error("listing due sync accounts failed", "error", err)
		return
	}
	for _, account := range accounts {
		freq := time.Duration(account.SyncFrequencyMinutes) * time.Minute
		if freq <= 0 {
			freq = 5 * time.Minute
		}
		if account.LastSyncedAt != nil && now.Sub(*account.LastSyncedAt) < freq {
			continue
		}
		accountCtx := ctx
		if account.OwnerID != nil && *account.OwnerID != uuid.Nil {
			accountCtx = authn.WithUserID(ctx, *account.OwnerID)
		}
		r.logger.Info("reconciling due sync account", "account_id", account.ID, "provider", account.Provider)
		if err := r.safeProcess(accountCtx, account, Job{}); err != nil {
			r.logger.Error("periodic sync failed", "account_id", account.ID, "provider", account.Provider, "error", err)
		}
	}
}

// safeProcess invokes the processor, recovering from any panic and
// converting it into a regular error. A single malformed record or
// unexpected provider response must never crash the whole sync loop.
func (r *Runner) safeProcess(ctx context.Context, account Account, job Job) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic processing sync for account %s: %v", account.ID, rec)
		}
	}()
	return r.processor.Process(ctx, account, job)
}

func (r *Runner) runJob(ctx context.Context, job Job) error {
	ownerCtx := ctx
	if job.OwnerID != nil && *job.OwnerID != uuid.Nil {
		ownerCtx = authn.WithUserID(ctx, *job.OwnerID)
	}
	accounts, err := r.accounts.List(ownerCtx, r.batchSize)
	if err != nil {
		return fmt.Errorf("loading sync accounts for job %s: %w", job.ID, err)
	}
	// Also fan out to every account already mirroring this record under a
	// different owner (e.g. a contact shared with another account, which
	// syncs it to its own separate Google connection), not just the
	// accounts owned by whoever triggered this change.
	linked, err := r.accounts.ListLinkedToPerson(ctx, job.PersonID)
	if err != nil {
		return fmt.Errorf("loading linked sync accounts for job %s: %w", job.ID, err)
	}
	accounts = mergeAccountsByID(accounts, linked)
	if len(accounts) == 0 {
		if err := r.jobs.MarkDone(ownerCtx, job.ID); err != nil {
			return fmt.Errorf("marking job %s done: %w", job.ID, err)
		}
		return nil
	}
	for _, account := range accounts {
		// Each account must be processed under its own owner's context (not
		// necessarily the triggering job's owner), so owner-scoped lookups
		// inside the provider adapter resolve against the right account.
		acctCtx := ownerCtx
		if account.OwnerID != nil && *account.OwnerID != uuid.Nil {
			acctCtx = authn.WithUserID(ctx, *account.OwnerID)
		}
		if err := r.safeProcess(acctCtx, account, job); err != nil {
			if markErr := r.jobs.MarkFailed(ownerCtx, job.ID, err.Error()); markErr != nil {
				return fmt.Errorf("marking job %s failed: %w", job.ID, markErr)
			}
			return fmt.Errorf("processing job %s for provider %s: %w", job.ID, account.Provider, err)
		}
	}
	if err := r.jobs.MarkDone(ownerCtx, job.ID); err != nil {
		return fmt.Errorf("marking job %s done: %w", job.ID, err)
	}
	return nil
}

// mergeAccountsByID unions two account slices, deduplicated by ID, preserving
// the order accounts are first seen.
func mergeAccountsByID(primary, extra []Account) []Account {
	out := make([]Account, 0, len(primary)+len(extra))
	seen := make(map[uuid.UUID]bool, len(primary)+len(extra))
	for _, a := range primary {
		if !seen[a.ID] {
			seen[a.ID] = true
			out = append(out, a)
		}
	}
	for _, a := range extra {
		if !seen[a.ID] {
			seen[a.ID] = true
			out = append(out, a)
		}
	}
	return out
}

// NoopProcessor is a placeholder processor used until provider adapters are
// connected.
type NoopProcessor struct{}

// Process satisfies Processor.
func (NoopProcessor) Process(context.Context, Account, Job) error { return nil }
