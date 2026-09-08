package contactsync

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeAccountStore struct {
	accounts []Account
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

type fakeQueue struct {
	jobs      []Job
	done      []uuid.UUID
	failed    map[uuid.UUID]string
	listErr   error
	doneErr   error
	failErr   error
	processed map[uuid.UUID]bool
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
