package person

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RelationshipRepository provides persistence for Relationship records.
type RelationshipRepository struct {
	pool *pgxpool.Pool
}

// NewRelationshipRepository constructs a RelationshipRepository.
func NewRelationshipRepository(pool *pgxpool.Pool) *RelationshipRepository {
	return &RelationshipRepository{pool: pool}
}

// ErrRelationshipNotFound is returned when a Relationship does not exist.
var ErrRelationshipNotFound = errors.New("relationship not found")

// CreateRelationship inserts a new Relationship and returns the stored record.
// Automatically ensures person_id_1 < person_id_2 for storage consistency.
func (r *RelationshipRepository) CreateRelationship(ctx context.Context, rel *Relationship) (*Relationship, error) {
	// Normalize IDs so person_id_1 is always less than person_id_2
	if rel.PersonID1.String() > rel.PersonID2.String() {
		rel.PersonID1, rel.PersonID2 = rel.PersonID2, rel.PersonID1
	}

	const q = `
		INSERT INTO relationships (person_id_1, person_id_2, relationship_type, label)
		VALUES ($1, $2, $3, $4)
		RETURNING id, person_id_1, person_id_2, relationship_type, label, created_at, updated_at, deleted_at`

	row := r.pool.QueryRow(ctx, q, rel.PersonID1, rel.PersonID2, rel.RelationshipType, rel.Label)
	return scanRelationship(row)
}

// GetRelationship returns a single non-deleted Relationship by ID.
func (r *RelationshipRepository) GetRelationship(ctx context.Context, id uuid.UUID) (*Relationship, error) {
	const q = `
		SELECT id, person_id_1, person_id_2, relationship_type, label, created_at, updated_at, deleted_at
		FROM relationships
		WHERE id = $1 AND deleted_at IS NULL`

	rel, err := scanRelationship(r.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRelationshipNotFound
	}
	return rel, err
}

// ListRelationshipsForPerson returns all non-deleted relationships for a person (as either ID).
func (r *RelationshipRepository) ListRelationshipsForPerson(ctx context.Context, personID uuid.UUID) ([]Relationship, error) {
	const q = `
		SELECT id, person_id_1, person_id_2, relationship_type, label, created_at, updated_at, deleted_at
		FROM relationships
		WHERE (person_id_1 = $1 OR person_id_2 = $1) AND deleted_at IS NULL
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, q, personID)
	if err != nil {
		return nil, fmt.Errorf("listing relationships: %w", err)
	}
	defer rows.Close()

	var rels []Relationship
	for rows.Next() {
		rel, scanErr := scanRelationship(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		rels = append(rels, *rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating relationships: %w", err)
	}
	return rels, nil
}

// UpdateRelationship applies a patch to an existing Relationship.
func (r *RelationshipRepository) UpdateRelationship(ctx context.Context, id uuid.UUID, rel *Relationship) (*Relationship, error) {
	const q = `
		UPDATE relationships
		SET relationship_type = $2, label = $3, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, person_id_1, person_id_2, relationship_type, label, created_at, updated_at, deleted_at`

	updated, err := scanRelationship(r.pool.QueryRow(ctx, q, id, rel.RelationshipType, rel.Label))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRelationshipNotFound
	}
	return updated, err
}

// SoftDeleteRelationship marks a Relationship as deleted.
func (r *RelationshipRepository) SoftDeleteRelationship(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE relationships SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("soft deleting relationship: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRelationshipNotFound
	}
	return nil
}

// PurgeExpiredRelationships permanently removes relationships soft-deleted before the cutoff.
func (r *RelationshipRepository) PurgeExpiredRelationships(ctx context.Context, olderThan time.Duration) (int64, error) {
	const q = `DELETE FROM relationships WHERE deleted_at IS NOT NULL AND deleted_at < $1`
	cutoff := time.Now().Add(-olderThan)
	tag, err := r.pool.Exec(ctx, q, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purging expired relationships: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanRelationship(s scanner) (*Relationship, error) {
	var rel Relationship
	if err := s.Scan(
		&rel.ID, &rel.PersonID1, &rel.PersonID2, &rel.RelationshipType,
		&rel.Label, &rel.CreatedAt, &rel.UpdatedAt, &rel.DeletedAt,
	); err != nil {
		return nil, err
	}
	return &rel, nil
}

// RelationshipService handles Relationship business logic.
type RelationshipService struct {
	repo      *RelationshipRepository
	personRepo *Repository
}

// NewRelationshipService constructs a RelationshipService.
func NewRelationshipService(repo *RelationshipRepository, personRepo *Repository) *RelationshipService {
	return &RelationshipService{repo: repo, personRepo: personRepo}
}

// CreateRelationship validates and creates a relationship between two persons.
func (s *RelationshipService) CreateRelationship(ctx context.Context, personID1, personID2 uuid.UUID, relType string, label *string) (*Relationship, ValidationErrors, error) {
	// Validate that both persons exist
	if _, err := s.personRepo.GetByID(ctx, personID1); err != nil {
		return nil, nil, ErrNotFound
	}
	if _, err := s.personRepo.GetByID(ctx, personID2); err != nil {
		return nil, nil, ErrNotFound
	}

	// Validate relationship type and other constraints
	validTypes := map[string]bool{
		"spouse":    true,
		"parent":    true,
		"child":     true,
		"sibling":   true,
		"colleague": true,
		"friend":    true,
		"other":     true,
	}
	if !validTypes[relType] {
		errs := ValidationErrors{{Field: "relationship_type", Message: "must be a valid relationship type"}}
		return nil, errs, nil
	}
	if personID1 == personID2 {
		errs := ValidationErrors{{Field: "relationship", Message: "cannot create a relationship with the same person"}}
		return nil, errs, nil
	}

	rel := &Relationship{
		PersonID1:        personID1,
		PersonID2:        personID2,
		RelationshipType: relType,
		Label:            label,
	}
	created, err := s.repo.CreateRelationship(ctx, rel)
	return created, nil, err
}

// GetRelationship retrieves a relationship by ID.
func (s *RelationshipService) GetRelationship(ctx context.Context, id uuid.UUID) (*Relationship, error) {
	return s.repo.GetRelationship(ctx, id)
}

// ListRelationshipsForPerson lists all relationships involving a person.
func (s *RelationshipService) ListRelationshipsForPerson(ctx context.Context, personID uuid.UUID) ([]Relationship, error) {
	// Verify person exists
	if _, err := s.personRepo.GetByID(ctx, personID); err != nil {
		return nil, ErrNotFound
	}
	return s.repo.ListRelationshipsForPerson(ctx, personID)
}

// UpdateRelationship validates and updates a relationship.
func (s *RelationshipService) UpdateRelationship(ctx context.Context, id uuid.UUID, relType string, label *string) (*Relationship, ValidationErrors, error) {
	validTypes := map[string]bool{
		"spouse":    true,
		"parent":    true,
		"child":     true,
		"sibling":   true,
		"colleague": true,
		"friend":    true,
		"other":     true,
	}
	if !validTypes[relType] {
		errs := ValidationErrors{{Field: "relationship_type", Message: "must be a valid relationship type"}}
		return nil, errs, nil
	}

	rel := &Relationship{
		RelationshipType: relType,
		Label:            label,
	}
	updated, err := s.repo.UpdateRelationship(ctx, id, rel)
	return updated, nil, err
}

// DeleteRelationship soft-deletes a relationship.
func (s *RelationshipService) DeleteRelationship(ctx context.Context, id uuid.UUID) error {
	return s.repo.SoftDeleteRelationship(ctx, id)
}

// PurgeExpired removes soft-deleted relationships.
func (s *RelationshipService) PurgeExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.repo.PurgeExpiredRelationships(ctx, olderThan)
}
