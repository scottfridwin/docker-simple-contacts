//go:build integration

package person

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
