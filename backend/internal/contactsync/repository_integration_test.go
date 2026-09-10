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
