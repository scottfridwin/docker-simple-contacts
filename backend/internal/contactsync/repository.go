package contactsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

// ErrNotFound is returned when a sync account does not exist.
var ErrNotFound = errors.New("sync account not found")

// Repository persists sync account configuration and state.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository constructs a Repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new sync account.
func (r *Repository) Create(ctx context.Context, account *Account) (*Account, error) {
	if account.SyncFrequencyMinutes <= 0 {
		account.SyncFrequencyMinutes = 5
	}
	if account.Status == "" {
		account.Status = "connected"
	}
	ownerID, owned := authn.UserID(ctx)
	const qOwned = `
		INSERT INTO sync_accounts (owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error, created_at, updated_at`
	const qLegacy = `
		INSERT INTO sync_accounts (provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error, created_at, updated_at`
	var row pgx.Row
	if owned {
		row = r.pool.QueryRow(ctx, qOwned, ownerID, account.Provider, account.ProviderAccountID, account.DisplayName, account.AccessToken, account.RefreshToken, account.ExpiresAt, account.Scope, account.SyncCursor, account.SyncFrequencyMinutes, account.Status, account.LastSyncedAt, account.LastError)
	} else {
		row = r.pool.QueryRow(ctx, qLegacy, account.Provider, account.ProviderAccountID, account.DisplayName, account.AccessToken, account.RefreshToken, account.ExpiresAt, account.Scope, account.SyncCursor, account.SyncFrequencyMinutes, account.Status, account.LastSyncedAt, account.LastError)
	}
	return scanAccount(row)
}

// GetByID fetches a sync account by ID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Account, error) {
	q := `
		SELECT id, owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error, created_at, updated_at
		FROM sync_accounts
		WHERE id = $1`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q = strings.Replace(q, "WHERE id = $1", "WHERE id = $1 AND owner_id = $2", 1)
		args = append(args, ownerID)
	}
	account, err := scanAccount(r.pool.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return account, err
}

// Update persists the current sync state for an account.
func (r *Repository) Update(ctx context.Context, account *Account) (*Account, error) {
	q := `
		UPDATE sync_accounts
		SET provider = $2, provider_account_id = $3, display_name = $4, access_token = $5, refresh_token = $6, expires_at = $7,
		    scope = $8, sync_cursor = $9, sync_frequency_minutes = $10, status = $11, last_synced_at = $12, last_error = $13,
		    updated_at = now()
		WHERE id = $1`
	args := []any{
		account.ID, account.Provider, account.ProviderAccountID, account.DisplayName, account.AccessToken, account.RefreshToken, account.ExpiresAt,
		account.Scope, account.SyncCursor, account.SyncFrequencyMinutes, account.Status, account.LastSyncedAt, account.LastError,
	}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $14`
		args = append(args, ownerID)
	}
	q += `
		RETURNING id, owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error, created_at, updated_at`
	return scanAccount(r.pool.QueryRow(ctx, q, args...))
}

// UpdateSyncState persists only the fields a running sync itself manages -
// OAuth tokens, sync cursor, status, and last-synced/last-error - never
// provider, display_name, or sync_frequency_minutes. A single Sync() call
// can hold its own in-memory Account snapshot for a very long time (a
// large/rate-limited pull can run for hours); if it wrote every column back
// via the full Update above, it would silently clobber a user-editable
// setting (e.g. sync_frequency_minutes) changed via PATCH /sync-accounts
// while that sync was still in flight, reverting it back to whatever the
// stale snapshot had at the top of the run.
func (r *Repository) UpdateSyncState(ctx context.Context, account *Account) (*Account, error) {
	q := `
		UPDATE sync_accounts
		SET access_token = $2, refresh_token = $3, expires_at = $4, scope = $5,
		    sync_cursor = $6, status = $7, last_synced_at = $8, last_error = $9,
		    updated_at = now()
		WHERE id = $1`
	args := []any{
		account.ID, account.AccessToken, account.RefreshToken, account.ExpiresAt,
		account.Scope, account.SyncCursor, account.Status, account.LastSyncedAt, account.LastError,
	}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $10`
		args = append(args, ownerID)
	}
	q += `
		RETURNING id, owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error, created_at, updated_at`
	return scanAccount(r.pool.QueryRow(ctx, q, args...))
}

// List returns sync accounts for the current account scope.
func (r *Repository) List(ctx context.Context, limit int) ([]Account, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `
		SELECT id, owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error, created_at, updated_at
		FROM sync_accounts`
	args := []any{}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` WHERE owner_id = $1`
		args = append(args, ownerID)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ` + fmt.Sprint(limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing sync accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0, limit)
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		accounts = append(accounts, *account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sync accounts: %w", err)
	}
	return accounts, nil
}

// Delete removes a sync account.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	q := `DELETE FROM sync_accounts WHERE id = $1`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $2`
		args = append(args, ownerID)
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("deleting sync account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListDue returns sync accounts that are ready for periodic reconciliation.
func (r *Repository) ListDue(ctx context.Context, before time.Time) ([]Account, error) {
	const q = `
		SELECT id, owner_id, provider, provider_account_id, display_name, access_token, refresh_token, expires_at, scope, sync_cursor, sync_frequency_minutes, status, last_synced_at, last_error, created_at, updated_at
		FROM sync_accounts
		WHERE status = 'connected' AND (last_synced_at IS NULL OR last_synced_at < $1)
		ORDER BY last_synced_at NULLS FIRST, id ASC`
	rows, err := r.pool.Query(ctx, q, before)
	if err != nil {
		return nil, fmt.Errorf("listing sync accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		accounts = append(accounts, *account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sync accounts: %w", err)
	}
	return accounts, nil
}

// ListLinkedToPerson returns every sync account (across every owner) that
// already has a remote record mapped for personID, so a local change can
// fan out to every mirror of that record - e.g. a contact shared between two
// accounts, each syncing it to their own separate Google account. Deliberately
// unscoped by the caller's authn context, mirroring ListDue.
func (r *Repository) ListLinkedToPerson(ctx context.Context, personID uuid.UUID) ([]Account, error) {
	const q = `
		SELECT a.id, a.owner_id, a.provider, a.provider_account_id, a.display_name, a.access_token, a.refresh_token,
		       a.expires_at, a.scope, a.sync_cursor, a.sync_frequency_minutes, a.status, a.last_synced_at, a.last_error,
		       a.created_at, a.updated_at
		FROM sync_accounts a
		JOIN sync_record_links l ON l.sync_account_id = a.id
		WHERE l.person_id = $1`
	rows, err := r.pool.Query(ctx, q, personID)
	if err != nil {
		return nil, fmt.Errorf("listing sync accounts linked to person: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		accounts = append(accounts, *account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sync accounts linked to person: %w", err)
	}
	return accounts, nil
}

// ListSharedWithAccountsForPerson returns every sync account owned by an
// account personID has been shared with (view+edit recipients), regardless
// of whether that account has ever linked/synced this record before. This
// lets a local edit to a shared contact fan out to a recipient's own
// connected accounts even on the very first time - see ListLinkedToPerson for
// the complementary "already syncing this record" case. Deliberately
// unscoped by the caller's authn context, mirroring ListLinkedToPerson.
func (r *Repository) ListSharedWithAccountsForPerson(ctx context.Context, personID uuid.UUID) ([]Account, error) {
	const q = `
		SELECT a.id, a.owner_id, a.provider, a.provider_account_id, a.display_name, a.access_token, a.refresh_token,
		       a.expires_at, a.scope, a.sync_cursor, a.sync_frequency_minutes, a.status, a.last_synced_at, a.last_error,
		       a.created_at, a.updated_at
		FROM sync_accounts a
		JOIN person_shares ps ON ps.shared_with_user_id = a.owner_id
		WHERE ps.person_id = $1`
	rows, err := r.pool.Query(ctx, q, personID)
	if err != nil {
		return nil, fmt.Errorf("listing sync accounts shared with person: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		accounts = append(accounts, *account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sync accounts shared with person: %w", err)
	}
	return accounts, nil
}

type accountScanner interface {
	Scan(dest ...any) error
}

func scanAccount(s accountScanner) (*Account, error) {
	var account Account
	if err := s.Scan(
		&account.ID, &account.OwnerID, &account.Provider, &account.ProviderAccountID, &account.DisplayName, &account.AccessToken,
		&account.RefreshToken, &account.ExpiresAt, &account.Scope, &account.SyncCursor, &account.SyncFrequencyMinutes,
		&account.Status, &account.LastSyncedAt, &account.LastError, &account.CreatedAt, &account.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &account, nil
}
