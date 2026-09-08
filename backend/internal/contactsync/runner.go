package contactsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

type accountStore interface {
	List(context.Context, int) ([]Account, error)
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
}

// NewRunner constructs a Runner.
func NewRunner(accounts accountStore, jobs jobQueue, processor Processor) *Runner {
	return &Runner{accounts: accounts, jobs: jobs, processor: processor, batchSize: 50}
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

// RunLoop keeps executing sync jobs until the context is canceled.
func (r *Runner) RunLoop(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if _, err := r.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runner) runJob(ctx context.Context, job Job) error {
	accountCtx := ctx
	if job.OwnerID != nil && *job.OwnerID != uuid.Nil {
		accountCtx = authn.WithUserID(ctx, *job.OwnerID)
	}
	accounts, err := r.accounts.List(accountCtx, r.batchSize)
	if err != nil {
		return fmt.Errorf("loading sync accounts for job %s: %w", job.ID, err)
	}
	if len(accounts) == 0 {
		if err := r.jobs.MarkDone(accountCtx, job.ID); err != nil {
			return fmt.Errorf("marking job %s done: %w", job.ID, err)
		}
		return nil
	}
	for _, account := range accounts {
		if err := r.processor.Process(accountCtx, account, job); err != nil {
			if markErr := r.jobs.MarkFailed(accountCtx, job.ID, err.Error()); markErr != nil {
				return fmt.Errorf("marking job %s failed: %w", job.ID, markErr)
			}
			return fmt.Errorf("processing job %s for provider %s: %w", job.ID, account.Provider, err)
		}
	}
	if err := r.jobs.MarkDone(accountCtx, job.ID); err != nil {
		return fmt.Errorf("marking job %s done: %w", job.ID, err)
	}
	return nil
}

// NoopProcessor is a placeholder processor used until provider adapters are
// connected.
type NoopProcessor struct{}

// Process satisfies Processor.
func (NoopProcessor) Process(context.Context, Account, Job) error { return nil }
