package person

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/authn"
)

// ErrRelationshipExists is returned when a linked relationship of the same
// type already exists between the same two people.
var ErrRelationshipExists = errors.New("relationship already exists")

// ErrRelatedPersonNotFound is returned when related_person_id does not
// resolve to an existing, owner-scoped Person.
var ErrRelatedPersonNotFound = errors.New("related person not found")

// CreateRelationship stores a new relationship from personID's perspective.
func (r *Repository) CreateRelationship(ctx context.Context, personID uuid.UUID, in RelationshipInput) (*RelationshipView, error) {
	if in.RelatedPersonID != nil {
		if _, err := r.GetByID(ctx, *in.RelatedPersonID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrRelatedPersonNotFound
			}
			return nil, fmt.Errorf("looking up related person: %w", err)
		}
	}

	ownerID, owned := authn.UserID(ctx)
	var ownerArg any
	if owned {
		ownerArg = ownerID
	}

	const q = `
		INSERT INTO person_relationships (owner_id, person_id, related_person_id, related_person_name, type)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`
	var id uuid.UUID
	var createdAt time.Time
	err := r.pool.QueryRow(ctx, q, ownerArg, personID, in.RelatedPersonID, in.RelatedPersonName, string(in.Type)).Scan(&id, &createdAt)
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return nil, ErrRelationshipExists
		}
		return nil, fmt.Errorf("creating relationship: %w", err)
	}

	name := ""
	if in.RelatedPersonName != nil {
		name = *in.RelatedPersonName
	}
	if in.RelatedPersonID != nil {
		related, err := r.GetByID(ctx, *in.RelatedPersonID)
		if err == nil {
			name = related.DisplayName
		}
	}
	return &RelationshipView{
		ID:                id,
		Type:              in.Type,
		RelatedPersonID:   in.RelatedPersonID,
		RelatedPersonName: name,
		CreatedAt:         createdAt,
	}, nil
}

// ListRelationships returns every relationship involving personID, from
// personID's perspective: rows it created directly, plus rows created by
// another person that name personID as the related person (with the type
// inverted, e.g. someone else's "Parent" link becomes this person's "Child").
func (r *Repository) ListRelationships(ctx context.Context, personID uuid.UUID) ([]RelationshipView, error) {
	ownerID, owned := authn.UserID(ctx)
	var ownerArg any
	if owned {
		ownerArg = ownerID
	}

	const q = `
		SELECT r.id, r.type, false AS is_reverse, r.related_person_id, r.related_person_name,
		       rp.display_name, (rp.deleted_at IS NOT NULL), r.created_at
		FROM person_relationships r
		LEFT JOIN persons rp ON rp.id = r.related_person_id
		WHERE r.person_id = $1 AND ($2::uuid IS NULL OR r.owner_id = $2)

		UNION ALL

		SELECT r.id, r.type, true AS is_reverse, r.person_id, NULL,
		       p.display_name, (p.deleted_at IS NOT NULL), r.created_at
		FROM person_relationships r
		JOIN persons p ON p.id = r.person_id
		WHERE r.related_person_id = $1 AND ($2::uuid IS NULL OR r.owner_id = $2)

		ORDER BY created_at ASC`

	rows, err := r.pool.Query(ctx, q, personID, ownerArg)
	if err != nil {
		return nil, fmt.Errorf("listing relationships: %w", err)
	}
	defer rows.Close()

	views := make([]RelationshipView, 0)
	for rows.Next() {
		var (
			id                uuid.UUID
			relType           string
			isReverse         bool
			relatedPersonID   *uuid.UUID
			relatedPersonName *string
			resolvedName      *string
			resolvedDeleted   bool
			createdAt         time.Time
		)
		if err := rows.Scan(&id, &relType, &isReverse, &relatedPersonID, &relatedPersonName, &resolvedName, &resolvedDeleted, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		t := RelationType(relType)
		if isReverse {
			t = t.Inverse()
		}
		name := ""
		switch {
		case resolvedName != nil:
			name = *resolvedName
		case relatedPersonName != nil:
			name = *relatedPersonName
		}
		views = append(views, RelationshipView{
			ID:                   id,
			Type:                 t,
			RelatedPersonID:      relatedPersonID,
			RelatedPersonName:    name,
			RelatedPersonDeleted: resolvedDeleted,
			CreatedAt:            createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating relationships: %w", err)
	}
	return views, nil
}

// ListIncomingRelationships returns only the relationships someone else
// created that name personID as the related person (the computed reverse
// view), i.e. rows personID does not own. Sync adapters use this to detect
// when a relationship is already represented from the other person's side
// before adding a redundant duplicate of their own.
func (r *Repository) ListIncomingRelationships(ctx context.Context, personID uuid.UUID) ([]RelationshipView, error) {
	ownerID, owned := authn.UserID(ctx)
	var ownerArg any
	if owned {
		ownerArg = ownerID
	}

	const q = `
		SELECT r.id, r.type, r.person_id, p.display_name, (p.deleted_at IS NOT NULL), r.created_at
		FROM person_relationships r
		JOIN persons p ON p.id = r.person_id
		WHERE r.related_person_id = $1 AND ($2::uuid IS NULL OR r.owner_id = $2)
		ORDER BY r.created_at ASC`

	rows, err := r.pool.Query(ctx, q, personID, ownerArg)
	if err != nil {
		return nil, fmt.Errorf("listing incoming relationships: %w", err)
	}
	defer rows.Close()

	views := make([]RelationshipView, 0)
	for rows.Next() {
		var (
			id          uuid.UUID
			relType     string
			ownerPerson uuid.UUID
			displayName string
			deleted     bool
			createdAt   time.Time
		)
		if err := rows.Scan(&id, &relType, &ownerPerson, &displayName, &deleted, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning incoming relationship: %w", err)
		}
		views = append(views, RelationshipView{
			ID:                   id,
			Type:                 RelationType(relType).Inverse(),
			RelatedPersonID:      &ownerPerson,
			RelatedPersonName:    displayName,
			RelatedPersonDeleted: deleted,
			CreatedAt:            createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating incoming relationships: %w", err)
	}
	return views, nil
}

// DeleteRelationship removes a relationship by id, scoped to the current
// owner. personID must be either the row's person_id or related_person_id,
// so a relationship can be deleted from either side's page.
func (r *Repository) DeleteRelationship(ctx context.Context, personID, relationshipID uuid.UUID) error {
	q := `DELETE FROM person_relationships WHERE id = $1 AND (person_id = $2 OR related_person_id = $2)`
	args := []any{relationshipID, personID}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $3`
		args = append(args, ownerID)
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("deleting relationship: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceRelationships replaces every relationship personID created (i.e.
// rows where person_id = personID) with the given set, used to reconcile
// relationships pulled from an external provider on each sync. Relationships
// created by OTHER people that reference personID (the computed reverse
// view) are left untouched, since this person doesn't own those rows.
func (r *Repository) ReplaceRelationships(ctx context.Context, personID uuid.UUID, desired []RelationshipInput) error {
	ownerID, owned := authn.UserID(ctx)
	var ownerArg any
	if owned {
		ownerArg = ownerID
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning relationship replace transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM person_relationships WHERE person_id = $1`, personID); err != nil {
		return fmt.Errorf("clearing existing relationships: %w", err)
	}
	for _, in := range desired {
		if !in.Type.Valid() {
			continue
		}
		if in.RelatedPersonID != nil && *in.RelatedPersonID == personID {
			continue
		}
		const q = `
			INSERT INTO person_relationships (owner_id, person_id, related_person_id, related_person_name, type)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`
		if _, err := tx.Exec(ctx, q, ownerArg, personID, in.RelatedPersonID, in.RelatedPersonName, string(in.Type)); err != nil {
			return fmt.Errorf("inserting relationship: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing relationship replace: %w", err)
	}
	return nil
}

// FindByDisplayName returns every non-deleted, owner-scoped Person with an
// exact display_name match, used to resolve provider-supplied relationship
// names (e.g. Google's free-text relation "person" field) to a local
// contact when possible.
func (r *Repository) FindByDisplayName(ctx context.Context, name string) ([]Person, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	q := `
		SELECT id, first_name, middle_names, last_name, display_name, nickname, pronouns, birthdate, emails, phone_numbers, addresses, organization, notes, custom_fields, is_favorite,
		       created_at, updated_at, deleted_at
		FROM persons
		WHERE display_name = $1 AND deleted_at IS NULL`
	args := []any{name}
	if ownerID, ok := authn.UserID(ctx); ok {
		q += ` AND owner_id = $2`
		args = append(args, ownerID)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("finding persons by display name: %w", err)
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating persons by display name: %w", err)
	}
	return out, nil
}
