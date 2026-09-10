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

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/contactsync"
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

// allowedSortFields maps a UI-facing sort key to the SQL columns used to
// order by, in priority order. "last_name" sorts by last name then first
// name so ties break predictably (the default: "Last Name, First Name").
var allowedSortFields = map[string][]string{
	"display_name": {"display_name"},
	"first_name":   {"first_name"},
	"last_name":    {"last_name", "first_name"},
	"created_at":   {"created_at"},
	"updated_at":   {"updated_at"},
}

// buildOrderBy resolves a sort key to a full ORDER BY clause (without the
// "ORDER BY" keyword), applying the requested direction to every column and
// always breaking remaining ties on id for stable pagination.
func buildOrderBy(sortField string, desc bool) string {
	cols := allowedSortFields[sortField]
	if len(cols) == 0 {
		cols = allowedSortFields["last_name"]
	}
	direction := "ASC"
	if desc {
		direction = "DESC"
	}
	parts := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		parts = append(parts, c+" "+direction)
	}
	parts = append(parts, "id ASC")
	return strings.Join(parts, ", ")
}

// Create inserts a new Person and returns the stored record.
func (r *Repository) Create(ctx context.Context, p *Person) (*Person, error) {
	normalizePersonSlices(p)
	ownerID, owned := authn.UserID(ctx)
	const qOwned = `
		INSERT INTO persons (owner_id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		          created_at, updated_at, deleted_at`
	const qLegacy = `
		INSERT INTO persons (first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		          created_at, updated_at, deleted_at`
	var row pgx.Row
	if owned {
		row = r.pool.QueryRow(ctx, qOwned, ownerID, p.FirstName, p.MiddleNames, p.LastName, p.DisplayName, p.Nickname, p.Pronouns, p.Birthdate, p.Emails, p.PhoneNumbers, p.Addresses, p.Organization, p.Notes, p.CustomFields, p.IsFavorite)
	} else {
		row = r.pool.QueryRow(ctx, qLegacy, p.FirstName, p.MiddleNames, p.LastName, p.DisplayName, p.Nickname, p.Pronouns, p.Birthdate, p.Emails, p.PhoneNumbers, p.Addresses, p.Organization, p.Notes, p.CustomFields, p.IsFavorite)
	}
	created, err := scanPerson(row)
	if err != nil {
		return nil, err
	}
	// A freshly created Person is always owned by its creator.
	created.IsOwner = true
	return created, nil
}

// GetByID returns a single non-deleted Person by ID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Person, error) {
	q := `
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		       created_at, updated_at, deleted_at
		FROM persons
		WHERE id = $1 AND deleted_at IS NULL`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q = strings.Replace(q, "WHERE id = $1", "WHERE id = $1 AND owner_id = $2", 1)
		args = append(args, ownerID)
	}
	person, err := scanPerson(r.pool.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return person, err
}

// GetAccessible returns a single non-deleted Person the current account
// either owns or has been granted a share for. When accessed via a share,
// IsOwner is false and OwnerDisplayName is populated so the UI can show
// "Shared by <name>". With no authenticated account (single-tenant/legacy
// mode), it behaves exactly like GetByID.
func (r *Repository) GetAccessible(ctx context.Context, id uuid.UUID) (*Person, error) {
	viewerID, ok := authn.UserID(ctx)
	if !ok {
		return r.GetByID(ctx, id)
	}
	const q = `
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		       created_at, updated_at, deleted_at, owner_id,
		       (SELECT display_name FROM users WHERE id = persons.owner_id) AS owner_display_name
		FROM persons
		WHERE id = $1 AND deleted_at IS NULL
		  AND (owner_id = $2 OR EXISTS (SELECT 1 FROM person_shares ps WHERE ps.person_id = persons.id AND ps.shared_with_user_id = $2))`
	person, err := scanPersonAccessible(r.pool.QueryRow(ctx, q, id, viewerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	applyOwnership(person, viewerID, true)
	return person, nil
}

// GetDeletedByID returns a soft-deleted Person by ID.
func (r *Repository) GetDeletedByID(ctx context.Context, id uuid.UUID) (*Person, error) {
	q := `
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		       created_at, updated_at, deleted_at
		FROM persons
		WHERE id = $1 AND deleted_at IS NOT NULL`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q = strings.Replace(q, "WHERE id = $1", "WHERE id = $1 AND owner_id = $2", 1)
		args = append(args, ownerID)
	}
	person, err := scanPerson(r.pool.QueryRow(ctx, q, args...))
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
	viewerID, viewerOK := authn.UserID(ctx)
	if viewerOK {
		where = append(where, fmt.Sprintf("(owner_id = $%d OR EXISTS (SELECT 1 FROM person_shares ps WHERE ps.person_id = persons.id AND ps.shared_with_user_id = $%d))", idx, idx))
		args = append(args, viewerID)
		idx++
	}

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

	if params.Favorite != nil {
		where = append(where, fmt.Sprintf("is_favorite = $%d", idx))
		args = append(args, *params.Favorite)
		idx++
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	countQ := "SELECT COUNT(*) FROM persons WHERE " + whereClause
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting persons: %w", err)
	}

	orderBy := buildOrderBy(params.SortField, params.SortDesc)

	limit := params.PageSize
	offset := (params.Page - 1) * params.PageSize

	// Secondary sort on id keeps ordering deterministic across pages.
	listQ := fmt.Sprintf(`
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		       created_at, updated_at, deleted_at, owner_id,
		       (SELECT display_name FROM users WHERE id = persons.owner_id) AS owner_display_name
		FROM persons
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, whereClause, orderBy, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing persons: %w", err)
	}
	defer rows.Close()

	persons := make([]Person, 0, limit)
	for rows.Next() {
		p, scanErr := scanPersonAccessible(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		applyOwnership(p, viewerID, viewerOK)
		persons = append(persons, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating persons: %w", err)
	}
	return persons, total, nil
}

// ListDeleted returns a page of soft-deleted Persons.
func (r *Repository) ListDeleted(ctx context.Context, params ListParams) ([]Person, int, error) {
	where := []string{"deleted_at IS NOT NULL"}
	args := []any{}
	idx := 1
	if ownerID, ok := authn.UserID(ctx); ok {
		where = append(where, fmt.Sprintf("owner_id = $%d", idx))
		args = append(args, ownerID)
		idx++
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	countQ := "SELECT COUNT(*) FROM persons WHERE " + whereClause
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting deleted persons: %w", err)
	}
	orderBy := buildOrderBy(params.SortField, params.SortDesc)
	pageSize := params.PageSize
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 100 {
		pageSize = 100
	}
	q := fmt.Sprintf(`
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		       created_at, updated_at, deleted_at
		FROM persons WHERE %s
		ORDER BY %s LIMIT $%d OFFSET $%d`, whereClause, orderBy, idx, idx+1)
	args = append(args, pageSize, (params.Page-1)*pageSize)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing deleted persons: %w", err)
	}
	defer rows.Close()
	persons := make([]Person, 0, 100)
	for rows.Next() {
		p, scanErr := scanPerson(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		persons = append(persons, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating deleted persons: %w", err)
	}
	return persons, total, nil
}

// Update applies a patch to an existing Person and returns the updated record.
// The WHERE clause allows either the owner or a share recipient (view+edit
// access) to persist changes.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, p *Person) (*Person, error) {
	normalizePersonSlices(p)
	q := `
		UPDATE persons
		SET first_name = $2, middle_names = $3, last_name = $4,
		    display_name = $5, nickname = $6, pronouns = $7, birthdate = $8,
		    emails = $9, phone_numbers = $10, addresses = $11, organization = $12, notes = $13, custom_fields = $14, is_favorite = $15, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		          created_at, updated_at, deleted_at`
	args := []any{id, p.FirstName, p.MiddleNames, p.LastName, p.DisplayName, p.Nickname, p.Pronouns, p.Birthdate, p.Emails, p.PhoneNumbers, p.Addresses, p.Organization, p.Notes, p.CustomFields, p.IsFavorite}
	if ownerID, ok := authn.UserID(ctx); ok {
		q = strings.Replace(q, "WHERE id = $1", "WHERE id = $1 AND (owner_id = $16 OR EXISTS (SELECT 1 FROM person_shares ps WHERE ps.person_id = persons.id AND ps.shared_with_user_id = $16))", 1)
		args = append(args, ownerID)
	}
	person, err := scanPerson(r.pool.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return person, err
}

// SoftDelete marks a Person as deleted (recycle bin). Returns ErrNotFound when
// the record does not exist or is already deleted.
func (r *Repository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	q := `UPDATE persons SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $2`
		args = append(args, ownerID)
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("soft deleting person: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore makes a soft-deleted Person active again.
func (r *Repository) Restore(ctx context.Context, id uuid.UUID) error {
	q := `UPDATE persons SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $2`
		args = append(args, ownerID)
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("restoring person: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// HardDelete permanently removes a soft-deleted Person.
func (r *Repository) HardDelete(ctx context.Context, id uuid.UUID) error {
	q := `DELETE FROM persons WHERE id = $1 AND deleted_at IS NOT NULL`
	args := []any{id}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $2`
		args = append(args, ownerID)
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("permanently deleting person: %w", err)
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
		&p.Nickname, &p.Pronouns, &p.Birthdate, &p.Emails, &p.PhoneNumbers, &p.Addresses, &p.Organization, &p.Notes,
		&p.CustomFields, &p.IsFavorite, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
	); err != nil {
		return nil, err
	}
	normalizePersonSlices(&p)
	if p.CustomFields == nil {
		p.CustomFields = map[string]any{}
	}
	return &p, nil
}

// scanPersonAccessible scans a row that additionally carries owner_id and a
// computed owner_display_name column (see GetAccessible/List).
func scanPersonAccessible(s scanner) (*Person, error) {
	var p Person
	var ownerDisplayName *string
	if err := s.Scan(
		&p.ID, &p.FirstName, &p.MiddleNames, &p.LastName, &p.DisplayName,
		&p.Nickname, &p.Pronouns, &p.Birthdate, &p.Emails, &p.PhoneNumbers, &p.Addresses, &p.Organization, &p.Notes,
		&p.CustomFields, &p.IsFavorite, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
		&p.OwnerID, &ownerDisplayName,
	); err != nil {
		return nil, err
	}
	normalizePersonSlices(&p)
	if p.CustomFields == nil {
		p.CustomFields = map[string]any{}
	}
	p.OwnerDisplayName = ownerDisplayName
	return &p, nil
}

// applyOwnership sets IsOwner based on the current viewer, clearing
// OwnerDisplayName when the viewer is the owner (it's only meaningful for
// shared access).
func applyOwnership(p *Person, viewerID uuid.UUID, viewerOK bool) {
	if !viewerOK {
		p.IsOwner = true
		p.OwnerDisplayName = nil
		return
	}
	p.IsOwner = p.OwnerID == nil || *p.OwnerID == viewerID
	if p.IsOwner {
		p.OwnerDisplayName = nil
	}
}

// normalizePersonSlices ensures nil slices become empty slices, since the
// JSONB columns backing these fields are NOT NULL.
func normalizePersonSlices(p *Person) {
	if p.MiddleNames == nil {
		p.MiddleNames = []string{}
	}
	if p.Emails == nil {
		p.Emails = []contactsync.LabeledValue{}
	}
	if p.PhoneNumbers == nil {
		p.PhoneNumbers = []contactsync.LabeledValue{}
	}
	if p.Addresses == nil {
		p.Addresses = []contactsync.Address{}
	}
}
