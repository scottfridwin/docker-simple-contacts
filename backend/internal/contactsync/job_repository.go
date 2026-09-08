package contactsync

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

// JobRepository persists per-record sync jobs.
type JobRepository struct {
	pool *pgxpool.Pool
}

// NewJobRepository constructs a JobRepository.
func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{pool: pool}
}

// Create inserts a new job.
func (r *JobRepository) Create(ctx context.Context, job *Job) (*Job, error) {
	if job.Status == "" {
		job.Status = JobStatusPending
	}
	snapshot, err := json.Marshal(job.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshalling sync snapshot: %w", err)
	}
	ownerID, owned := authn.UserID(ctx)
	const qOwned = `
		INSERT INTO sync_jobs (owner_id, person_id, kind, snapshot, status, attempts, last_error, processed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, owner_id, person_id, kind, snapshot, status, attempts, last_error, processed_at, created_at, updated_at`
	const qLegacy = `
		INSERT INTO sync_jobs (person_id, kind, snapshot, status, attempts, last_error, processed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, owner_id, person_id, kind, snapshot, status, attempts, last_error, processed_at, created_at, updated_at`
	var row pgx.Row
	if owned {
		row = r.pool.QueryRow(ctx, qOwned, ownerID, job.PersonID, string(job.Kind), snapshot, job.Status, job.Attempts, job.LastError, job.ProcessedAt)
	} else {
		row = r.pool.QueryRow(ctx, qLegacy, job.PersonID, string(job.Kind), snapshot, job.Status, job.Attempts, job.LastError, job.ProcessedAt)
	}
	return scanJob(row)
}

// ListByOwner returns jobs in descending creation order, scoped to the active account.
func (r *JobRepository) ListByOwner(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `
		SELECT id, owner_id, person_id, kind, snapshot, status, attempts, last_error, processed_at, created_at, updated_at
		FROM sync_jobs`
	args := []any{}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` WHERE owner_id = $1`
		args = append(args, ownerID)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ` + strconv.Itoa(limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing sync jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0, limit)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sync jobs: %w", err)
	}
	return jobs, nil
}

// ListPending returns pending jobs in descending creation order.
func (r *JobRepository) ListPending(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `
		SELECT id, owner_id, person_id, kind, snapshot, status, attempts, last_error, processed_at, created_at, updated_at
		FROM sync_jobs
		WHERE status = $1`
	args := []any{JobStatusPending}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $2`
		args = append(args, ownerID)
	}
	q += ` ORDER BY created_at ASC, id ASC LIMIT ` + strconv.Itoa(limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing pending sync jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0, limit)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating pending sync jobs: %w", err)
	}
	return jobs, nil
}

// Delete removes a sync job by ID.
func (r *JobRepository) Delete(ctx context.Context, id uuid.UUID) error {
	q := `DELETE FROM sync_jobs WHERE id = $1`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $2`
		args = append(args, ownerID)
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("deleting sync job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountPendingForPerson counts pending jobs for one record.
func (r *JobRepository) CountPendingForPerson(ctx context.Context, ownerID *uuid.UUID, personID uuid.UUID) (int64, error) {
	q := `SELECT COUNT(*) FROM sync_jobs WHERE person_id = $1 AND status = $2`
	args := []any{personID, JobStatusPending}
	if ownerID != nil {
		q += ` AND owner_id = $3`
		args = append(args, *ownerID)
	}
	var count int64
	if err := r.pool.QueryRow(ctx, q, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting pending sync jobs: %w", err)
	}
	return count, nil
}

// MarkDone records a successful sync job completion.
func (r *JobRepository) MarkDone(ctx context.Context, id uuid.UUID) error {
	return r.updateStatus(ctx, id, JobStatusDone, nil, true)
}

// MarkFailed records a failed sync job attempt.
func (r *JobRepository) MarkFailed(ctx context.Context, id uuid.UUID, reason string) error {
	return r.updateStatus(ctx, id, JobStatusFailed, &reason, false)
}

func (r *JobRepository) updateStatus(ctx context.Context, id uuid.UUID, status string, reason *string, processed bool) error {
	var processedAt any
	if processed {
		processedAt = time.Now()
	}
	q := `
		UPDATE sync_jobs
		SET status = $2, last_error = $3, attempts = attempts + 1, processed_at = $4, updated_at = now()
		WHERE id = $1`
	args := []any{id, status, reason, processedAt}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $5`
		args = append(args, ownerID)
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("updating sync job status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(s jobScanner) (*Job, error) {
	var job Job
	var kind string
	var snapshotRaw []byte
	if err := s.Scan(
		&job.ID, &job.OwnerID, &job.PersonID, &kind, &snapshotRaw, &job.Status,
		&job.Attempts, &job.LastError, &job.ProcessedAt, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		return nil, err
	}
	job.Kind = ChangeKind(kind)
	if len(snapshotRaw) > 0 {
		if err := json.Unmarshal(snapshotRaw, &job.Snapshot); err != nil {
			return nil, fmt.Errorf("decoding sync snapshot: %w", err)
		}
	}
	return &job, nil
}
