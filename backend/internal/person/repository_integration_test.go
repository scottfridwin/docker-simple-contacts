//go:build integration

package person

import (
	"context"
	"errors"
	"fmt"
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

// TestRepositoryRelationships exercises the real UNION-based
// forward/reverse query, the unique-linked-relationship constraint, cascade
// deletion when the related person is hard-deleted, and cross-owner
// isolation against a real Postgres instance (the in-memory fakes used by
// unit tests can't catch SQL-level bugs).
func TestRepositoryRelationships(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()
	owner := createTestUser(t, ctx, pool, "rel-owner")
	other := createTestUser(t, ctx, pool, "rel-other")
	ownerCtx := authn.WithUserID(ctx, owner)
	otherCtx := authn.WithUserID(ctx, other)

	a, err := repo.Create(ownerCtx, &Person{FirstName: "A", LastName: "Parent", DisplayName: "A Parent", CustomFields: map[string]any{}})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := repo.Create(ownerCtx, &Person{FirstName: "B", LastName: "Child", DisplayName: "B Child", CustomFields: map[string]any{}})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id IN ($1, $2)", a.ID, b.ID)
	})

	view, err := repo.CreateRelationship(ownerCtx, a.ID, RelationshipInput{Type: RelationParent, RelatedPersonID: &b.ID})
	if err != nil {
		t.Fatalf("CreateRelationship: %v", err)
	}
	if view.Type != RelationParent || view.RelatedPersonName != "B Child" {
		t.Fatalf("unexpected view: %+v", view)
	}

	// Duplicate linked relationship of the same type is rejected.
	if _, err := repo.CreateRelationship(ownerCtx, a.ID, RelationshipInput{Type: RelationParent, RelatedPersonID: &b.ID}); !errors.Is(err, ErrRelationshipExists) {
		t.Fatalf("expected ErrRelationshipExists, got %v", err)
	}

	// B's list shows the computed inverse via the real UNION query.
	bViews, err := repo.ListRelationships(ownerCtx, b.ID)
	if err != nil {
		t.Fatalf("ListRelationships(B): %v", err)
	}
	if len(bViews) != 1 || bViews[0].Type != RelationChild || bViews[0].RelatedPersonName != "A Parent" {
		t.Fatalf("B's relationships = %+v", bViews)
	}

	// ListIncomingRelationships(B) returns the same computed-inverse row (used
	// by the Google sync adapter to dedupe symmetric relationships), but A's
	// own copy is empty since A owns the row rather than receiving it.
	bIncoming, err := repo.ListIncomingRelationships(ownerCtx, b.ID)
	if err != nil {
		t.Fatalf("ListIncomingRelationships(B): %v", err)
	}
	if len(bIncoming) != 1 || bIncoming[0].Type != RelationChild || bIncoming[0].RelatedPersonID == nil || *bIncoming[0].RelatedPersonID != a.ID {
		t.Fatalf("B's incoming relationships = %+v", bIncoming)
	}
	aIncoming, err := repo.ListIncomingRelationships(ownerCtx, a.ID)
	if err != nil {
		t.Fatalf("ListIncomingRelationships(A): %v", err)
	}
	if len(aIncoming) != 0 {
		t.Fatalf("expected no incoming relationships for A (it owns the row), got %+v", aIncoming)
	}

	// A different owner must not see this relationship at all.
	otherViews, err := repo.ListRelationships(otherCtx, a.ID)
	if err != nil {
		t.Fatalf("ListRelationships (other owner): %v", err)
	}
	if len(otherViews) != 0 {
		t.Fatalf("expected no relationships visible to a different owner, got %+v", otherViews)
	}

	// Hard-deleting the related person cascades and removes the relationship.
	if err := repo.SoftDelete(ownerCtx, b.ID); err != nil {
		t.Fatalf("soft delete B: %v", err)
	}
	if err := repo.HardDelete(ownerCtx, b.ID); err != nil {
		t.Fatalf("hard delete B: %v", err)
	}
	aViews, err := repo.ListRelationships(ownerCtx, a.ID)
	if err != nil {
		t.Fatalf("ListRelationships(A) after related person hard-deleted: %v", err)
	}
	if len(aViews) != 0 {
		t.Fatalf("expected relationship to cascade-delete with the related person, got %+v", aViews)
	}
}

// TestRepositoryRelationshipCap guards the unbounded-growth fix: once a
// Person has MaxRelationshipsPerPerson relationships recorded from its own
// perspective, one more must be rejected.
func TestRepositoryRelationshipCap(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	a, err := repo.Create(ctx, &Person{FirstName: "Cap", LastName: "Owner", DisplayName: "Cap Owner", CustomFields: map[string]any{}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id = $1", a.ID) })

	for i := 0; i < MaxRelationshipsPerPerson; i++ {
		name := fmt.Sprintf("Friend %d", i)
		if _, err := repo.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSibling, RelatedPersonName: &name}); err != nil {
			t.Fatalf("CreateRelationship #%d: %v", i, err)
		}
	}
	overflow := "One Too Many"
	if _, err := repo.CreateRelationship(ctx, a.ID, RelationshipInput{Type: RelationSibling, RelatedPersonName: &overflow}); !errors.Is(err, ErrTooManyRelationships) {
		t.Fatalf("relationship past the cap = %v, want ErrTooManyRelationships", err)
	}
}

// TestRepositorySharing is the security-critical guard for cross-account
// contact sharing: a share grants exactly the recipient view+edit access to
// the base Person record, an unrelated third account must never see it, and
// deletion/relationship-management/re-sharing stay owner-only even for the
// recipient. Run against a real Postgres instance since access control bugs
// here would be a real data-exposure vulnerability the in-memory unit-test
// fakes can't catch.
func TestRepositorySharing(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	owner := createTestUser(t, ctx, pool, "share-owner")
	recipient := createTestUser(t, ctx, pool, "share-recipient")
	stranger := createTestUser(t, ctx, pool, "share-stranger")
	ownerCtx := authn.WithUserID(ctx, owner)
	recipientCtx := authn.WithUserID(ctx, recipient)
	strangerCtx := authn.WithUserID(ctx, stranger)

	created, err := repo.Create(ownerCtx, &Person{FirstName: "Shared", LastName: "Contact", DisplayName: "Shared Contact", CustomFields: map[string]any{}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id = $1", created.ID) })

	// Sharing with yourself, a nonexistent email, or the same recipient
	// twice must all be rejected.
	if _, err := repo.CreateShare(ownerCtx, created.ID, "share-owner@example.com"); !errors.Is(err, ErrCannotShareWithSelf) {
		t.Fatalf("self-share = %v, want ErrCannotShareWithSelf", err)
	}
	if _, err := repo.CreateShare(ownerCtx, created.ID, "nobody@example.com"); !errors.Is(err, ErrShareUserNotFound) {
		t.Fatalf("unknown-email share = %v, want ErrShareUserNotFound", err)
	}
	share, err := repo.CreateShare(ownerCtx, created.ID, "SHARE-RECIPIENT@example.com")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	if share.SharedWithUserID != recipient {
		t.Fatalf("share resolved to %v, want recipient %v", share.SharedWithUserID, recipient)
	}
	if _, err := repo.CreateShare(ownerCtx, created.ID, "share-recipient@example.com"); !errors.Is(err, ErrShareExists) {
		t.Fatalf("duplicate share = %v, want ErrShareExists", err)
	}

	// Recipient can view and edit the shared Person via the accessible path.
	viaRecipient, err := repo.GetAccessible(recipientCtx, created.ID)
	if err != nil {
		t.Fatalf("recipient GetAccessible: %v", err)
	}
	if viaRecipient.IsOwner {
		t.Error("expected IsOwner=false for the recipient")
	}
	if viaRecipient.OwnerDisplayName == nil || *viaRecipient.OwnerDisplayName != "share-owner" {
		t.Errorf("OwnerDisplayName = %v, want share-owner", viaRecipient.OwnerDisplayName)
	}
	viaRecipient.Notes = func() *string { s := "edited by recipient"; return &s }()
	updated, err := repo.Update(recipientCtx, created.ID, viaRecipient)
	if err != nil {
		t.Fatalf("recipient Update: %v", err)
	}
	if updated.Notes == nil || *updated.Notes != "edited by recipient" {
		t.Fatalf("expected recipient's edit to persist, got %+v", updated.Notes)
	}

	// The recipient's edit shows up for the owner too (single shared record).
	viaOwner, err := repo.GetAccessible(ownerCtx, created.ID)
	if err != nil {
		t.Fatalf("owner GetAccessible: %v", err)
	}
	if !viaOwner.IsOwner {
		t.Error("expected IsOwner=true for the owner")
	}
	if viaOwner.Notes == nil || *viaOwner.Notes != "edited by recipient" {
		t.Fatalf("owner should see the recipient's edit, got %+v", viaOwner.Notes)
	}

	// The recipient sees it merged into their own list.
	recipientList, _, err := repo.List(recipientCtx, ListParams{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("recipient List: %v", err)
	}
	found := false
	for _, p := range recipientList {
		if p.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the shared Person to appear in the recipient's list")
	}

	// An unrelated third account must never see it via either path.
	if _, err := repo.GetAccessible(strangerCtx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger GetAccessible = %v, want ErrNotFound", err)
	}
	strangerList, _, err := repo.List(strangerCtx, ListParams{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("stranger List: %v", err)
	}
	for _, p := range strangerList {
		if p.ID == created.ID {
			t.Fatal("stranger can see a Person shared with someone else")
		}
	}

	// Deletion, relationship management, and re-sharing remain owner-only
	// even for the recipient - GetByID (the strict path those use) must
	// reject the recipient.
	if _, err := repo.GetByID(recipientCtx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recipient strict GetByID = %v, want ErrNotFound (shared access must not extend to relationships/delete)", err)
	}
	if err := repo.SoftDelete(recipientCtx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recipient SoftDelete = %v, want ErrNotFound", err)
	}
	// repo.CreateShare itself has no owner check (that's enforced one layer
	// up, by Service.CreateShare's GetByID pre-check) - exercise the real
	// Service on top of this same repo to confirm the recipient is
	// rejected end-to-end.
	svc := NewService(repo)
	if _, _, err := svc.CreateShare(recipientCtx, created.ID, "share-stranger@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recipient re-share via Service = %v, want ErrNotFound", err)
	}

	// Revoking the share removes access again.
	if err := repo.DeleteShare(ownerCtx, created.ID, share.ID); err != nil {
		t.Fatalf("DeleteShare: %v", err)
	}
	if _, err := repo.GetAccessible(recipientCtx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recipient GetAccessible after revoke = %v, want ErrNotFound", err)
	}
}

// TestRepositoryLeaveShare guards the recipient-initiated self-unshare path
// against a real database: the recipient can remove their own access
// without the owner acting, an unrelated account cannot remove someone
// else's share, and the owner's own strict access is untouched.
func TestRepositoryLeaveShare(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	owner := createTestUser(t, ctx, pool, "leave-owner")
	recipient := createTestUser(t, ctx, pool, "leave-recipient")
	stranger := createTestUser(t, ctx, pool, "leave-stranger")
	ownerCtx := authn.WithUserID(ctx, owner)
	recipientCtx := authn.WithUserID(ctx, recipient)
	strangerCtx := authn.WithUserID(ctx, stranger)

	created, err := repo.Create(ownerCtx, &Person{FirstName: "Leave", LastName: "Test", DisplayName: "Leave Test", CustomFields: map[string]any{}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id = $1", created.ID) })

	if _, err := repo.CreateShare(ownerCtx, created.ID, "leave-recipient@example.com"); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	if err := repo.DeleteShareByRecipient(strangerCtx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unrelated account DeleteShareByRecipient = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetAccessible(recipientCtx, created.ID); err != nil {
		t.Fatalf("recipient should still have access: %v", err)
	}

	if err := repo.DeleteShareByRecipient(recipientCtx, created.ID); err != nil {
		t.Fatalf("DeleteShareByRecipient: %v", err)
	}
	if _, err := repo.GetAccessible(recipientCtx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recipient GetAccessible after leaving = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetAccessible(ownerCtx, created.ID); err != nil {
		t.Fatalf("owner should be unaffected: %v", err)
	}
}

// TestRepositoryShareCap guards the unbounded-growth fix: once a Person is
// shared with MaxSharesPerPerson accounts, one more must be rejected.
func TestRepositoryShareCap(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	owner := createTestUser(t, ctx, pool, "cap-owner")
	ownerCtx := authn.WithUserID(ctx, owner)
	created, err := repo.Create(ownerCtx, &Person{FirstName: "Cap", LastName: "Test", DisplayName: "Cap Test", CustomFields: map[string]any{}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM persons WHERE id = $1", created.ID) })

	for i := 0; i < MaxSharesPerPerson; i++ {
		subject := fmt.Sprintf("cap-recipient-%d", i)
		createTestUser(t, ctx, pool, subject)
		if _, err := repo.CreateShare(ownerCtx, created.ID, subject+"@example.com"); err != nil {
			t.Fatalf("CreateShare #%d: %v", i, err)
		}
	}
	createTestUser(t, ctx, pool, "cap-recipient-overflow")
	if _, err := repo.CreateShare(ownerCtx, created.ID, "cap-recipient-overflow@example.com"); !errors.Is(err, ErrTooManyShares) {
		t.Fatalf("share past the cap = %v, want ErrTooManyShares", err)
	}
}
