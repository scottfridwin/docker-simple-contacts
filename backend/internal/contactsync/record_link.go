package contactsync

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLinkNotFound is returned when no remote mapping exists for a person on a
// given sync account.
var ErrLinkNotFound = errors.New("sync record link not found")

// RecordLink maps one local Person to its remote record on one sync account,
// so the same Person can be linked to multiple accounts (even multiple
// accounts of the same provider) without colliding on a single shared field.
type RecordLink struct {
	ID              uuid.UUID
	SyncAccountID   uuid.UUID
	PersonID        uuid.UUID
	RemoteID        string
	RemoteUpdatedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// RecordLinkRepository persists per-account remote record mappings.
type RecordLinkRepository struct {
	pool *pgxpool.Pool
}

// NewRecordLinkRepository constructs a RecordLinkRepository.
func NewRecordLinkRepository(pool *pgxpool.Pool) *RecordLinkRepository {
	return &RecordLinkRepository{pool: pool}
}

// Get returns the remote mapping for a person on a specific sync account.
func (r *RecordLinkRepository) Get(ctx context.Context, syncAccountID, personID uuid.UUID) (*RecordLink, error) {
	const q = `
		SELECT id, sync_account_id, person_id, remote_id, remote_updated_at, created_at, updated_at
		FROM sync_record_links
		WHERE sync_account_id = $1 AND person_id = $2`
	link, err := scanRecordLink(r.pool.QueryRow(ctx, q, syncAccountID, personID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	return link, err
}

// Upsert creates or updates the remote mapping for a person on a sync account.
func (r *RecordLinkRepository) Upsert(ctx context.Context, link *RecordLink) error {
	const q = `
		INSERT INTO sync_record_links (sync_account_id, person_id, remote_id, remote_updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (sync_account_id, person_id)
		DO UPDATE SET remote_id = EXCLUDED.remote_id, remote_updated_at = EXCLUDED.remote_updated_at, updated_at = now()
		RETURNING id, created_at, updated_at`
	return r.pool.QueryRow(ctx, q, link.SyncAccountID, link.PersonID, link.RemoteID, link.RemoteUpdatedAt).
		Scan(&link.ID, &link.CreatedAt, &link.UpdatedAt)
}

func scanRecordLink(row pgx.Row) (*RecordLink, error) {
	var link RecordLink
	if err := row.Scan(&link.ID, &link.SyncAccountID, &link.PersonID, &link.RemoteID, &link.RemoteUpdatedAt, &link.CreatedAt, &link.UpdatedAt); err != nil {
		return nil, err
	}
	return &link, nil
}
