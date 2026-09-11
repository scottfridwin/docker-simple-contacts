//go:build integration

package contactsync

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

// These tests run only with `-tags=integration` and require TEST_DATABASE_URL
// pointing at a migrated PostgreSQL instance.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration tests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createSyncTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, subject string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx, `INSERT INTO users (subject, email, display_name) VALUES ($1, $1 || '@example.com', $1) RETURNING id`, subject).Scan(&id)
	if err != nil {
		t.Fatalf("creating test user %q: %v", subject, err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", id) })
	return id
}

func createSyncTestPerson(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx, `INSERT INTO persons (owner_id, first_name, last_name, display_name) VALUES ($1, 'Link', 'Test', 'Link Test') RETURNING id`, ownerID).Scan(&id)
	if err != nil {
		t.Fatalf("creating test person: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id = $1", id) })
	return id
}

// TestRepositoryListLinkedToPerson guards the cross-owner sync fan-out
// feature: a shared contact synced to two different accounts' own Google
// connections must resolve to *both* sync accounts regardless of which
// owner each was created under, and must not include unrelated accounts.
func TestRepositoryListLinkedToPerson(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	links := NewRecordLinkRepository(pool)
	ctx := context.Background()

	ownerA := createSyncTestUser(t, ctx, pool, "link-owner-a")
	ownerB := createSyncTestUser(t, ctx, pool, "link-owner-b")
	personID := createSyncTestPerson(t, ctx, pool, ownerA)
	unrelatedPersonID := createSyncTestPerson(t, ctx, pool, ownerA)

	accountA, err := repo.Create(authn.WithUserID(ctx, ownerA), &Account{Provider: "google", ProviderAccountID: "link-google-a"})
	if err != nil {
		t.Fatalf("create account A: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sync_accounts WHERE id = $1", accountA.ID) })

	accountB, err := repo.Create(authn.WithUserID(ctx, ownerB), &Account{Provider: "google", ProviderAccountID: "link-google-b"})
	if err != nil {
		t.Fatalf("create account B: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sync_accounts WHERE id = $1", accountB.ID) })

	unrelatedAccount, err := repo.Create(authn.WithUserID(ctx, ownerA), &Account{Provider: "google", ProviderAccountID: "link-google-unrelated"})
	if err != nil {
		t.Fatalf("create unrelated account: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sync_accounts WHERE id = $1", unrelatedAccount.ID) })

	if err := links.Upsert(ctx, &RecordLink{SyncAccountID: accountA.ID, PersonID: personID, RemoteID: "remote-a"}); err != nil {
		t.Fatalf("link account A: %v", err)
	}
	if err := links.Upsert(ctx, &RecordLink{SyncAccountID: accountB.ID, PersonID: personID, RemoteID: "remote-b"}); err != nil {
		t.Fatalf("link account B: %v", err)
	}
	if err := links.Upsert(ctx, &RecordLink{SyncAccountID: unrelatedAccount.ID, PersonID: unrelatedPersonID, RemoteID: "remote-unrelated"}); err != nil {
		t.Fatalf("link unrelated account: %v", err)
	}

	got, err := repo.ListLinkedToPerson(ctx, personID)
	if err != nil {
		t.Fatalf("ListLinkedToPerson: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListLinkedToPerson returned %d accounts, want 2: %+v", len(got), got)
	}
	seen := map[uuid.UUID]bool{}
	for _, a := range got {
		seen[a.ID] = true
	}
	if !seen[accountA.ID] || !seen[accountB.ID] {
		t.Fatalf("expected both accountA and accountB, got %+v", got)
	}
	if seen[unrelatedAccount.ID] {
		t.Fatalf("unrelated account leaked into results: %+v", got)
	}
}

// TestRepositoryListSharedWithAccountsForPerson guards the edit-propagation
// feature: editing a Person shared with another account must be able to
// reach that recipient's own connected accounts even before any of them
// have ever linked/synced this particular record - ListLinkedToPerson alone
// can't see them yet in that case.
func TestRepositoryListSharedWithAccountsForPerson(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	owner := createSyncTestUser(t, ctx, pool, "share-owner")
	recipient := createSyncTestUser(t, ctx, pool, "share-recipient")
	stranger := createSyncTestUser(t, ctx, pool, "share-stranger")
	personID := createSyncTestPerson(t, ctx, pool, owner)

	recipientAccount, err := repo.Create(authn.WithUserID(ctx, recipient), &Account{Provider: "google", ProviderAccountID: "share-google-recipient"})
	if err != nil {
		t.Fatalf("create recipient account: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sync_accounts WHERE id = $1", recipientAccount.ID) })

	strangerAccount, err := repo.Create(authn.WithUserID(ctx, stranger), &Account{Provider: "google", ProviderAccountID: "share-google-stranger"})
	if err != nil {
		t.Fatalf("create stranger account: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sync_accounts WHERE id = $1", strangerAccount.ID) })

	if _, err := pool.Exec(ctx, `INSERT INTO person_shares (person_id, shared_with_user_id) VALUES ($1, $2)`, personID, recipient); err != nil {
		t.Fatalf("creating share: %v", err)
	}

	got, err := repo.ListSharedWithAccountsForPerson(ctx, personID)
	if err != nil {
		t.Fatalf("ListSharedWithAccountsForPerson: %v", err)
	}
	if len(got) != 1 || got[0].ID != recipientAccount.ID {
		t.Fatalf("ListSharedWithAccountsForPerson = %+v, want only recipient's account %v", got, recipientAccount.ID)
	}
}

// TestJobRepositoryListPendingRetriesFailedJobsWithBackoff guards the retry
// mechanism: a failed job must not be retried immediately (respecting its
// exponential backoff window), must be retried once that window elapses, and
// must stop being retried once MaxJobAttempts is reached (a dead letter).
func TestJobRepositoryListPendingRetriesFailedJobsWithBackoff(t *testing.T) {
	pool := newTestPool(t)
	jobs := NewJobRepository(pool)
	ctx := context.Background()
	owner := createSyncTestUser(t, ctx, pool, "job-backoff-owner")
	personID := createSyncTestPerson(t, ctx, pool, owner)

	job, err := jobs.Create(ctx, &Job{PersonID: personID, Kind: ChangeKindUpdated})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sync_jobs WHERE id = $1", job.ID) })

	if err := jobs.MarkFailed(ctx, job.ID, "boom"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	pending, err := jobs.ListPending(ctx, 50)
	if err != nil {
		t.Fatalf("list pending (fresh failure): %v", err)
	}
	for _, p := range pending {
		if p.ID == job.ID {
			t.Fatalf("job retried before its backoff window elapsed: %+v", p)
		}
	}

	// Backdate updated_at past the 1-attempt (2 minute) backoff window.
	if _, err := pool.Exec(ctx, "UPDATE sync_jobs SET updated_at = now() - interval '10 minutes' WHERE id = $1", job.ID); err != nil {
		t.Fatalf("backdating job: %v", err)
	}
	pending, err = jobs.ListPending(ctx, 50)
	if err != nil {
		t.Fatalf("list pending (after backoff): %v", err)
	}
	found := false
	for _, p := range pending {
		if p.ID == job.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected job to be retried after its backoff window elapsed, got %+v", pending)
	}

	// Exhaust attempts: once at MaxJobAttempts, it must never be retried again.
	if _, err := pool.Exec(ctx, "UPDATE sync_jobs SET attempts = $2, updated_at = now() - interval '1000 minutes' WHERE id = $1", job.ID, MaxJobAttempts); err != nil {
		t.Fatalf("exhausting attempts: %v", err)
	}
	pending, err = jobs.ListPending(ctx, 50)
	if err != nil {
		t.Fatalf("list pending (exhausted): %v", err)
	}
	for _, p := range pending {
		if p.ID == job.ID {
			t.Fatalf("dead-lettered job was retried past MaxJobAttempts: %+v", p)
		}
	}
}

// TestUpdateSyncStateDoesNotClobberUserSettings guards a real production
// incident: a user changed sync_frequency_minutes via PATCH /sync-accounts
// while a long-running Sync() was still in flight for that account (a
// large/rate-limited pull can run for hours, holding its own in-memory
// Account snapshot from before the user's change the whole time). When
// that in-flight run later persisted its own sync state (token refresh,
// or the final "sync finished" update), the old blanket Update wrote back
// its own stale sync_frequency_minutes/display_name too, silently
// reverting the user's change. UpdateSyncState must never touch those
// user-editable fields, only sync execution state.
func TestUpdateSyncStateDoesNotClobberUserSettings(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	name := "Original Name"
	created, err := repo.Create(ctx, &Account{
		Provider:             "google",
		ProviderAccountID:    "sync-state-test-" + uuid.NewString(),
		DisplayName:          &name,
		SyncFrequencyMinutes: 5,
		Status:               "connected",
	})
	if err != nil {
		t.Fatalf("creating account: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM sync_accounts WHERE id = $1", created.ID) })

	// Simulate a long-running Sync() call that loaded its own snapshot
	// before the user's settings change below.
	stale := *created

	// The user changes their settings via the API while that sync is
	// still (conceptually) in flight.
	newName := "User Renamed This"
	created.DisplayName = &newName
	created.SyncFrequencyMinutes = 60
	if _, err := repo.Update(ctx, created); err != nil {
		t.Fatalf("simulating user PATCH: %v", err)
	}

	// The stale in-flight sync now persists its own sync-execution state
	// using its old snapshot.
	token := "refreshed-token"
	stale.AccessToken = &token
	if _, err := repo.UpdateSyncState(ctx, &stale); err != nil {
		t.Fatalf("UpdateSyncState: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.SyncFrequencyMinutes != 60 {
		t.Errorf("SyncFrequencyMinutes = %d, want 60 (must survive the stale sync's UpdateSyncState call)", got.SyncFrequencyMinutes)
	}
	if got.DisplayName == nil || *got.DisplayName != newName {
		t.Errorf("DisplayName = %v, want %q (must survive the stale sync's UpdateSyncState call)", got.DisplayName, newName)
	}
	if got.AccessToken == nil || *got.AccessToken != token {
		t.Errorf("AccessToken = %v, want %q (UpdateSyncState must still persist its own fields)", got.AccessToken, token)
	}
}
