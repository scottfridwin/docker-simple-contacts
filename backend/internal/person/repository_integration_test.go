//go:build integration

package person

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/contactsync"
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

func TestRepositoryCRUD(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	created, err := repo.Create(ctx, &Person{
		FirstName:    "Integration",
		MiddleNames:  []string{"Q"},
		LastName:     "Tester",
		DisplayName:  "Integration Q Tester",
		PhoneNumbers: []contactsync.LabeledValue{{Label: "mobile", Value: "+1-555-0100"}},
		CustomFields: map[string]any{"blood_type": "O+", "age": float64(30)},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id = $1", created.ID) })

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.FirstName != "Integration" || len(got.MiddleNames) != 1 {
		t.Errorf("unexpected record: %+v", got)
	}

	got.LastName = "Updated"
	updated, err := repo.Update(ctx, created.ID, got)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.LastName != "Updated" {
		t.Errorf("LastName = %q", updated.LastName)
	}

	list, total, err := repo.List(ctx, ListParams{Page: 1, PageSize: 25, SortField: "display_name", SortDesc: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total < 1 || len(list) < 1 {
		t.Errorf("expected at least one record, got total=%d", total)
	}

	if err := repo.SoftDelete(ctx, created.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, created.ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after soft delete, got %v", err)
	}

	// Purge with a zero window should remove the just-deleted record.
	if _, err := repo.PurgeExpired(ctx, -time.Hour); err != nil {
		t.Fatalf("purge: %v", err)
	}
}

// TestRepositoryDeletedRecordsAreOwnerScoped guards against cross-tenant
// access to the recycle bin: one user's soft-deleted contacts must not be
// listable, restorable, or permanently-deletable by another user.
func TestRepositoryDeletedRecordsAreOwnerScoped(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	ownerA := createTestUser(t, ctx, pool, "owner-a")
	ownerB := createTestUser(t, ctx, pool, "owner-b")
	ctxA := authn.WithUserID(ctx, ownerA)
	ctxB := authn.WithUserID(ctx, ownerB)

	created, err := repo.Create(ctxA, &Person{FirstName: "Owner", LastName: "A", CustomFields: map[string]any{}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id = $1", created.ID) })

	if err := repo.SoftDelete(ctxA, created.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// Owner B must not see owner A's deleted contact in the recycle bin.
	deletedForB, _, err := repo.ListDeleted(ctxB, ListParams{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list deleted (owner B): %v", err)
	}
	for _, p := range deletedForB {
		if p.ID == created.ID {
			t.Fatal("owner B can see owner A's deleted contact")
		}
	}

	// Owner A must see it.
	deletedForA, _, err := repo.ListDeleted(ctxA, ListParams{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list deleted (owner A): %v", err)
	}
	found := false
	for _, p := range deletedForA {
		if p.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("owner A cannot see their own deleted contact")
	}

	// Owner B must not be able to restore or hard-delete owner A's contact.
	if err := repo.Restore(ctxB, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner B restore = %v, want ErrNotFound", err)
	}
	if err := repo.HardDelete(ctxB, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner B hard delete = %v, want ErrNotFound", err)
	}

	// Owner A can restore and then hard-delete their own contact.
	if err := repo.Restore(ctxA, created.ID); err != nil {
		t.Fatalf("owner A restore: %v", err)
	}
	if err := repo.SoftDelete(ctxA, created.ID); err != nil {
		t.Fatalf("re-soft-delete: %v", err)
	}
	if err := repo.HardDelete(ctxA, created.ID); err != nil {
		t.Fatalf("owner A hard delete: %v", err)
	}
}

func createTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, subject string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx, `INSERT INTO users (subject, email, display_name) VALUES ($1, $1 || '@example.com', $1) RETURNING id`, subject).Scan(&id)
	if err != nil {
		t.Fatalf("creating test user %q: %v", subject, err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", id) })
	return id
}
