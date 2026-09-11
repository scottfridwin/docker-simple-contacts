package contactsync

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

type fakeAccountStore struct {
	accounts []Account
	due      []Account
	linked   []Account
	shared   []Account
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

func (f *fakeAccountStore) ListLinkedToPerson(context.Context, uuid.UUID) ([]Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]Account(nil), f.linked...), nil
}

func (f *fakeAccountStore) ListSharedWithAccountsForPerson(context.Context, uuid.UUID) ([]Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]Account(nil), f.shared...), nil
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
	processed     []string
	contextOwners []uuid.UUID
	calls         int
	err           error
	panics        bool
	failFor       map[uuid.UUID]error
	failForJob    map[uuid.UUID]error
}

func (f *fakeProcessor) Process(ctx context.Context, account Account, job Job) error {
	f.calls++
	if f.panics {
		panic("simulated processor panic")
	}
	if err, ok := f.failFor[account.ID]; ok {
		return err
	}
	if err, ok := f.failForJob[job.ID]; ok {
		return err
	}
	if f.err != nil {
		return f.err
	}
	owner, _ := authn.UserID(ctx)
	f.contextOwners = append(f.contextOwners, owner)
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
		nil,
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

// TestRunnerRunJobFansOutToLinkedAccountsOfOtherOwners guards the sharing
// scenario: a change made under one owner's account (e.g. the owner of a
// shared contact) must still reach every other account already mirroring
// that same person (e.g. a recipient's own separately-connected Google
// account), each processed under its own owner's context.
func TestRunnerRunJobFansOutToLinkedAccountsOfOtherOwners(t *testing.T) {
	ownerA := uuid.New()
	ownerB := uuid.New()
	personID := uuid.New()
	accountA := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerA}
	accountB := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerB}
	job := Job{ID: uuid.New(), OwnerID: &ownerA, PersonID: personID, Status: JobStatusPending}
	proc := &fakeProcessor{}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{accountA}, linked: []Account{accountA, accountB}},
		&fakeQueue{jobs: []Job{job}},
		proc,
		nil,
	)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if proc.calls != 2 {
		t.Fatalf("processor calls = %d, want 2 (deduped across owner + linked accounts)", proc.calls)
	}
	if len(proc.contextOwners) != 2 || proc.contextOwners[0] != ownerA || proc.contextOwners[1] != ownerB {
		t.Fatalf("contextOwners = %#v, want [%s, %s]", proc.contextOwners, ownerA, ownerB)
	}
}

// TestRunnerRunJobFansOutToShareRecipientAccountsNeverLinkedBefore guards the
// propagation-on-edit feature: editing a Person already shared with another
// account must reach that recipient's own connected accounts even the very
// first time, before ListLinkedToPerson would ever know about them (unlike
// the already-mirroring case above, which only guards accounts that have
// already synced this record at least once).
func TestRunnerRunJobFansOutToShareRecipientAccountsNeverLinkedBefore(t *testing.T) {
	ownerA := uuid.New()
	ownerB := uuid.New()
	personID := uuid.New()
	accountA := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerA}
	accountB := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerB}
	job := Job{ID: uuid.New(), OwnerID: &ownerA, PersonID: personID, Status: JobStatusPending}
	proc := &fakeProcessor{}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{accountA}, shared: []Account{accountB}},
		&fakeQueue{jobs: []Job{job}},
		proc,
		nil,
	)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if proc.calls != 2 {
		t.Fatalf("processor calls = %d, want 2 (owner account + share recipient's account)", proc.calls)
	}
	if len(proc.contextOwners) != 2 || proc.contextOwners[0] != ownerA || proc.contextOwners[1] != ownerB {
		t.Fatalf("contextOwners = %#v, want [%s, %s]", proc.contextOwners, ownerA, ownerB)
	}
}

// TestRunnerRunJobExcludesOriginAccount guards the propagation-vs-ping-pong
// fix: a change pulled in from one sync account (Job.OriginAccountID) must
// still reach every OTHER linked/shared account, but must never be pushed
// straight back to the account it just came from - doing so would look like
// a newer remote change on that account's next pull and loop forever.
func TestRunnerRunJobExcludesOriginAccount(t *testing.T) {
	ownerA := uuid.New()
	ownerB := uuid.New()
	personID := uuid.New()
	accountA := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerA}
	accountB := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerB}
	job := Job{ID: uuid.New(), OwnerID: &ownerA, PersonID: personID, Status: JobStatusPending, OriginAccountID: &accountA.ID}
	proc := &fakeProcessor{}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{accountA}, linked: []Account{accountA, accountB}},
		&fakeQueue{jobs: []Job{job}},
		proc,
		nil,
	)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if proc.calls != 1 {
		t.Fatalf("processor calls = %d, want 1 (only account B, account A excluded as the origin)", proc.calls)
	}
	if len(proc.contextOwners) != 1 || proc.contextOwners[0] != ownerB {
		t.Fatalf("contextOwners = %#v, want [%s]", proc.contextOwners, ownerB)
	}
}

func TestRunnerRunOnceMarksFailures(t *testing.T) {
	job := Job{ID: uuid.New(), Status: JobStatusPending}
	proc := &fakeProcessor{err: errors.New("adapter failed")}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{{Provider: "google"}}},
		&fakeQueue{jobs: []Job{job}},
		proc,
		nil,
	)
	if _, err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	q := runner.jobs.(*fakeQueue)
	if got := q.failed[job.ID]; !strings.Contains(got, "adapter failed") {
		t.Fatalf("failed reason = %q, want it to contain adapter failed", got)
	}
}

// TestRunnerRunJobContinuesAfterOneAccountFails guards best-effort fan-out:
// one account failing must not stop the job from being attempted against
// every other account in the fan-out list (e.g. other share recipients who
// had nothing to do with the failure) - it used to return on the very first
// failing account, silently skipping the rest for this attempt.
func TestRunnerRunJobContinuesAfterOneAccountFails(t *testing.T) {
	ownerA := uuid.New()
	ownerB := uuid.New()
	personID := uuid.New()
	accountA := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerA}
	accountB := Account{ID: uuid.New(), Provider: "google", OwnerID: &ownerB}
	job := Job{ID: uuid.New(), OwnerID: &ownerA, PersonID: personID, Status: JobStatusPending}
	proc := &fakeProcessor{failFor: map[uuid.UUID]error{accountA.ID: errors.New("account A broken")}}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{accountA}, linked: []Account{accountA, accountB}},
		&fakeQueue{jobs: []Job{job}},
		proc,
		nil,
	)
	if _, err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error since account A failed")
	}
	if proc.calls != 2 {
		t.Fatalf("processor calls = %d, want 2 (both accounts attempted despite A failing)", proc.calls)
	}
	if len(proc.processed) != 1 || proc.processed[0] != "google:"+job.ID.String() {
		t.Fatalf("expected account B to still be processed successfully, got %#v", proc.processed)
	}
	q := runner.jobs.(*fakeQueue)
	if !strings.Contains(q.failed[job.ID], "account A broken") {
		t.Fatalf("failed reason = %q, want it to contain account A broken", q.failed[job.ID])
	}
}

// TestRunnerRunOnceContinuesAfterOneJobFails guards the same best-effort
// principle at the batch level: one job failing must not prevent other,
// unrelated pending jobs in the same batch from being attempted.
func TestRunnerRunOnceContinuesAfterOneJobFails(t *testing.T) {
	account := Account{ID: uuid.New(), Provider: "google"}
	brokenJob := Job{ID: uuid.New(), PersonID: uuid.New(), Status: JobStatusPending}
	okJob := Job{ID: uuid.New(), PersonID: uuid.New(), Status: JobStatusPending}
	proc := &fakeProcessor{failForJob: map[uuid.UUID]error{brokenJob.ID: errors.New("broken")}}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{account}},
		&fakeQueue{jobs: []Job{brokenJob, okJob}},
		proc,
		nil,
	)
	processed, err := runner.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected a combined error since brokenJob failed")
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1 (okJob still succeeds despite brokenJob failing)", processed)
	}
	q := runner.jobs.(*fakeQueue)
	if len(q.done) != 1 || q.done[0] != okJob.ID {
		t.Fatalf("expected only okJob marked done, got %#v", q.done)
	}
	if _, failed := q.failed[brokenJob.ID]; !failed {
		t.Fatalf("expected brokenJob marked failed, got %#v", q.failed)
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
		nil,
	)
	runner.runDueAccounts(context.Background())

	if len(proc.processed) != 2 {
		t.Fatalf("expected 2 accounts processed (never-synced + stale), got %d: %#v", len(proc.processed), proc.processed)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// TestRunDueAccountsSurvivesProcessorPanic guards against one account's
// malformed data (e.g. an unexpected provider response deep in an adapter)
// taking down reconciliation for every other due account in the same tick.
func TestRunDueAccountsSurvivesProcessorPanic(t *testing.T) {
	panicking := Account{ID: uuid.New(), Provider: "google", SyncFrequencyMinutes: 5}
	fine := Account{ID: uuid.New(), Provider: "google", SyncFrequencyMinutes: 5}

	proc := &fakeProcessor{panics: true}
	runner := NewRunner(
		&fakeAccountStore{due: []Account{panicking, fine}},
		&fakeQueue{},
		proc,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	runner.runDueAccounts(context.Background())

	if proc.calls != 2 {
		t.Fatalf("expected both accounts to be attempted despite the panic, got %d calls", proc.calls)
	}
}

// TestRunLoopSurvivesProcessorPanic guards the same failure mode at the
// RunLoop level: an unrecovered panic in a goroutine crashes the whole
// process (unlike an HTTP handler panic, which net/http contains per
// request), so background sync must never let one bad record kill the app.
func TestRunLoopSurvivesProcessorPanic(t *testing.T) {
	job := Job{ID: uuid.New(), Status: JobStatusPending}
	proc := &fakeProcessor{panics: true}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{{Provider: "google"}}},
		&fakeQueue{jobs: []Job{job}},
		proc,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	if err := runner.RunLoop(ctx, 20*time.Millisecond); err != nil {
		t.Fatalf("RunLoop returned error, want nil (loop must survive panics): %v", err)
	}
	if proc.calls < 2 {
		t.Fatalf("expected the loop to keep ticking after a panic, got %d attempts", proc.calls)
	}
}

// TestRunLoopSurvivesPersistentFailures guards a critical resilience bug:
// RunLoop used to return (and its caller's goroutine would exit) the moment
// any single job failed to process, permanently stopping ALL future
// background sync - including the periodic due-account reconciliation -
// until the process was restarted. It must now log and keep ticking instead.
func TestRunLoopSurvivesPersistentFailures(t *testing.T) {
	job := Job{ID: uuid.New(), Status: JobStatusPending}
	proc := &fakeProcessor{err: errors.New("boom")}
	runner := NewRunner(
		&fakeAccountStore{accounts: []Account{{Provider: "google"}}},
		&fakeQueue{jobs: []Job{job}},
		proc,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	if err := runner.RunLoop(ctx, 20*time.Millisecond); err != nil {
		t.Fatalf("RunLoop returned error, want nil (loop must survive failures): %v", err)
	}
	if proc.calls < 2 {
		t.Fatalf("expected the loop to keep retrying after failures, got %d attempts", proc.calls)
	}
}
