package person

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a Person does not exist or is soft-deleted.
var ErrNotFound = errors.New("person not found")

// Repository provides persistence for Person records backed by PostgreSQL.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository constructs a Repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

var allowedSortFields = map[string]string{
	"display_name": "display_name",
	"first_name":   "first_name",
	"last_name":    "last_name",
	"created_at":   "created_at",
	"updated_at":   "updated_at",
}

// Create inserts a new Person and returns the stored record.
func (r *Repository) Create(ctx context.Context, p *Person) (*Person, error) {
	const q = `
		INSERT INTO persons (first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, phone_numbers, custom_fields)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, phone_numbers, custom_fields,
		          created_at, updated_at, deleted_at`
	row := r.pool.QueryRow(ctx, q,
		p.FirstName, p.MiddleNames, p.LastName, p.DisplayName, p.Nickname, p.Pronouns, p.Birthdate, p.PhoneNumbers, p.CustomFields,
	)
	return scanPerson(row)
}

// GetByID returns a single non-deleted Person by ID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Person, error) {
	const q = `
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, phone_numbers, custom_fields,
		       created_at, updated_at, deleted_at
		FROM persons
		WHERE id = $1 AND deleted_at IS NULL`
	person, err := scanPerson(r.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return person, err
}

// List returns a page of non-deleted Persons and the total matching count.
func (r *Repository) List(ctx context.Context, params ListParams) ([]Person, int, error) {
	where := []string{"deleted_at IS NULL"}
	args := []any{}
	idx := 1

	if params.FirstName != "" {
		where = append(where, fmt.Sprintf("first_name ILIKE $%d", idx))
		args = append(args, "%"+params.FirstName+"%")
		idx++
	}
	if params.LastName != "" {
		where = append(where, fmt.Sprintf("last_name ILIKE $%d", idx))
		args = append(args, "%"+params.LastName+"%")
		idx++
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	countQ := "SELECT COUNT(*) FROM persons WHERE " + whereClause
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting persons: %w", err)
	}

	sortColumn := allowedSortFields[params.SortField]
	if sortColumn == "" {
		sortColumn = "display_name"
	}
	direction := "ASC"
	if params.SortDesc {
		direction = "DESC"
	}

	limit := params.PageSize
	offset := (params.Page - 1) * params.PageSize

	// Secondary sort on id keeps ordering deterministic across pages.
	listQ := fmt.Sprintf(`
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, phone_numbers, custom_fields,
		       created_at, updated_at, deleted_at
		FROM persons
		WHERE %s
		ORDER BY %s %s, id ASC
		LIMIT $%d OFFSET $%d`, whereClause, sortColumn, direction, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing persons: %w", err)
	}
	defer rows.Close()

	persons := make([]Person, 0, limit)
	for rows.Next() {
		p, scanErr := scanPerson(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		persons = append(persons, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating persons: %w", err)
	}
	return persons, total, nil
}

// Update applies a patch to an existing Person and returns the updated record.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, p *Person) (*Person, error) {
	const q = `
		UPDATE persons
		SET first_name = $2, middle_names = $3, last_name = $4,
		    display_name = $5, nickname = $6, pronouns = $7, birthdate = $8,
		    phone_numbers = $9, custom_fields = $10, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, phone_numbers, custom_fields,
		          created_at, updated_at, deleted_at`
	person, err := scanPerson(r.pool.QueryRow(ctx, q,
		id, p.FirstName, p.MiddleNames, p.LastName, p.DisplayName, p.Nickname, p.Pronouns, p.Birthdate, p.PhoneNumbers, p.CustomFields,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return person, err
}

// SoftDelete marks a Person as deleted (recycle bin). Returns ErrNotFound when
// the record does not exist or is already deleted.
func (r *Repository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE persons SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("soft deleting person: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpired permanently removes records soft-deleted before the cutoff. It
// returns the number of purged rows.
func (r *Repository) PurgeExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	const q = `DELETE FROM persons WHERE deleted_at IS NOT NULL AND deleted_at < $1`
	cutoff := time.Now().Add(-olderThan)
	tag, err := r.pool.Exec(ctx, q, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purging expired persons: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Ping verifies database connectivity for readiness checks.
func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPerson(s scanner) (*Person, error) {
	var p Person
	if err := s.Scan(
		&p.ID, &p.FirstName, &p.MiddleNames, &p.LastName, &p.DisplayName,
		&p.Nickname, &p.Pronouns, &p.Birthdate, &p.PhoneNumbers,
		&p.CustomFields, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
	); err != nil {
		return nil, err
	}
	if p.MiddleNames == nil {
		p.MiddleNames = []string{}
	}
	if p.PhoneNumbers == nil {
		p.PhoneNumbers = []string{}
	}
	if p.CustomFields == nil {
		p.CustomFields = map[string]any{}
	}
	return &p, nil
}
