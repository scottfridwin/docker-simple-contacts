package person

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

// ErrShareExists is returned when a Person is already shared with the
// requested recipient.
var ErrShareExists = errors.New("already shared with this person")

// ErrShareUserNotFound is returned when no account exists for the
// requested recipient email (they must have logged in at least once).
var ErrShareUserNotFound = errors.New("no account found for that email")

// ErrCannotShareWithSelf is returned when the owner tries to share a
// Person with their own account.
var ErrCannotShareWithSelf = errors.New("cannot share a person with yourself")

// ErrTooManyShares is returned when a Person is already shared with the
// maximum number of accounts.
var ErrTooManyShares = errors.New("this contact has reached the maximum number of shares")

// MaxSharesPerPerson caps how many accounts a single Person can be shared
// with, preventing unbounded growth of the person_shares table.
const MaxSharesPerPerson = 50

// Share grants another account view+edit access to a Person the
// current account owns. Deletion, re-sharing, and relationship management
// remain owner-only - sharing only extends to the base Person record.
type Share struct {
	ID                    uuid.UUID
	PersonID              uuid.UUID
	SharedWithUserID      uuid.UUID
	SharedWithEmail       string
	SharedWithDisplayName string
	CreatedAt             time.Time
}

// CreateShare grants access to personID to the account with the given
// (exact-match) email. The recipient must already have an account (i.e.
// have logged in at least once via Authentik) - there is no invite-by-email
// flow for accounts that don't exist yet.
func (r *Repository) CreateShare(ctx context.Context, personID uuid.UUID, email string) (*Share, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, ErrShareUserNotFound
	}

	var recipientID uuid.UUID
	var displayName string
	err := r.pool.QueryRow(ctx, `SELECT id, display_name FROM users WHERE lower(email) = $1`, email).Scan(&recipientID, &displayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrShareUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("looking up user by email: %w", err)
	}
	if ownerID, ok := authn.UserID(ctx); ok && recipientID == ownerID {
		return nil, ErrCannotShareWithSelf
	}

	var shareCount int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM person_shares WHERE person_id = $1`, personID).Scan(&shareCount); err != nil {
		return nil, fmt.Errorf("counting existing shares: %w", err)
	}
	if shareCount >= MaxSharesPerPerson {
		return nil, ErrTooManyShares
	}

	const q = `
		INSERT INTO person_shares (person_id, shared_with_user_id)
		VALUES ($1, $2)
		RETURNING id, created_at`
	var id uuid.UUID
	var createdAt time.Time
	if err := r.pool.QueryRow(ctx, q, personID, recipientID).Scan(&id, &createdAt); err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return nil, ErrShareExists
		}
		return nil, fmt.Errorf("creating share: %w", err)
	}
	return &Share{
		ID: id, PersonID: personID, SharedWithUserID: recipientID,
		SharedWithEmail: email, SharedWithDisplayName: displayName, CreatedAt: createdAt,
	}, nil
}

// ListShares returns everyone personID is currently shared with.
func (r *Repository) ListShares(ctx context.Context, personID uuid.UUID) ([]Share, error) {
	const q = `
		SELECT ps.id, ps.person_id, ps.shared_with_user_id, u.email, u.display_name, ps.created_at
		FROM person_shares ps
		JOIN users u ON u.id = ps.shared_with_user_id
		WHERE ps.person_id = $1
		ORDER BY ps.created_at ASC`
	rows, err := r.pool.Query(ctx, q, personID)
	if err != nil {
		return nil, fmt.Errorf("listing shares: %w", err)
	}
	defer rows.Close()

	shares := make([]Share, 0)
	for rows.Next() {
		var s Share
		if err := rows.Scan(&s.ID, &s.PersonID, &s.SharedWithUserID, &s.SharedWithEmail, &s.SharedWithDisplayName, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning share: %w", err)
		}
		shares = append(shares, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating shares: %w", err)
	}
	return shares, nil
}

// DeleteShare revokes a previously granted share.
func (r *Repository) DeleteShare(ctx context.Context, personID, shareID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM person_shares WHERE id = $1 AND person_id = $2`, shareID, personID)
	if err != nil {
		return fmt.Errorf("deleting share: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteShareByRecipient lets the currently authenticated account remove its
// own access to a Person shared with it, without needing the owner to act.
// Requires an authenticated context; returns ErrNotFound if there's no such
// share (including when there's no authenticated user at all).
func (r *Repository) DeleteShareByRecipient(ctx context.Context, personID uuid.UUID) error {
	recipientID, ok := authn.UserID(ctx)
	if !ok {
		return ErrNotFound
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM person_shares WHERE person_id = $1 AND shared_with_user_id = $2`, personID, recipientID)
	if err != nil {
		return fmt.Errorf("deleting share: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
