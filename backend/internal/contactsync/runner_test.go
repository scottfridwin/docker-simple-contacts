package contactsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeAccountStore struct {
	accounts []Account
	due      []Account
	err      error
}

func (f *fakeAccountStore) List(context.Context, int) ([]Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]Account, len(f.accounts))
	copy(out, f.accounts)
	return out, nil
}

func (f *fakeAccountStore) ListDue(context.Context, time.Time) ([]Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]Account(nil), f.due...), nil
}

type fakeQueue struct {
	jobs    []Job
	done    []uuid.UUID
	failed  map[uuid.UUID]string
	listErr error
	doneErr error
	failErr error
}

func (f *fakeQueue) ListPending(context.Context, int) ([]Job, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]Job(nil), f.jobs...), nil
}

func (f *fakeQueue) MarkDone(_ context.Context, id uuid.UUID) error {
	if f.doneErr != nil {
		return f.doneErr
	}
	f.done = append(f.done, id)
	return nil
}

func (f *fakeQueue) MarkFailed(_ context.Context, id uuid.UUID, reason string) error {
	if f.failErr != nil {
		return f.failErr
	}
	if f.failed == nil {
		f.failed = make(map[uuid.UUID]string)
	}
	f.failed[id] = reason
	return nil
}

type fakeProcessor struct {
	processed []string
	err       error
}

func (f *fakeProcessor) Process(_ context.Context, account Account, job Job) error {
	if f.err != nil {
		return f.err
	}
	f.processed = append(f.processed, account.Provider+":"+job.ID.String())
	return nil
}

func TestRunnerRunOnceProcessesJobsForAccounts(t *testing.T) {
	ownerID := uuid.New()
	job := Job{ID: uuid.New(), OwnerID: &ownerID, PersonID: uuid.New(), Status: JobStatusPending}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{{Provider: "google"}, {Provider: "apple"}}},
		&fakeQueue{jobs: []Job{job}},
		&fakeProcessor{},
	)
	count, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if count != 1 {
		t.Fatalf("processed = %d, want 1", count)
	}
	q := runner.jobs.(*fakeQueue)
	if len(q.done) != 1 || q.done[0] != job.ID {
		t.Fatalf("expected job marked done, got %#v", q.done)
	}
}

func TestRunnerRunOnceMarksFailures(t *testing.T) {
	job := Job{ID: uuid.New(), Status: JobStatusPending}
	proc := &fakeProcessor{err: errors.New("adapter failed")}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{{Provider: "google"}}},
		&fakeQueue{jobs: []Job{job}},
		proc,
	)
	if _, err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	q := runner.jobs.(*fakeQueue)
	if got := q.failed[job.ID]; got != "adapter failed" {
		t.Fatalf("failed reason = %q, want adapter failed", got)
	}
}

// TestRunnerRunDueAccountsReconcilesOnSchedule guards periodic reconciliation:
// without this, a connected account would only ever pull remote changes once
// (right after connecting) and never again unless a local edit happened to
// enqueue a job.
func TestRunnerRunDueAccountsReconcilesOnSchedule(t *testing.T) {
	now := time.Now()
	neverSynced := Account{ID: uuid.New(), Provider: "google", SyncFrequencyMinutes: 5}
	staleSynced := Account{ID: uuid.New(), Provider: "google", SyncFrequencyMinutes: 5, LastSyncedAt: timePtr(now.Add(-10 * time.Minute))}
	recentlySynced := Account{ID: uuid.New(), Provider: "google", SyncFrequencyMinutes: 30, LastSyncedAt: timePtr(now.Add(-1 * time.Minute))}

	proc := &fakeProcessor{}
	runner := NewRunner(
		&fakeAccountStore{due: []Account{neverSynced, staleSynced, recentlySynced}},
		&fakeQueue{},
		proc,
	)
	runner.runDueAccounts(context.Background())

	if len(proc.processed) != 2 {
		t.Fatalf("expected 2 accounts processed (never-synced + stale), got %d: %#v", len(proc.processed), proc.processed)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
