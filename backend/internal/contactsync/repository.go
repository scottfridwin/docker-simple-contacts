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
