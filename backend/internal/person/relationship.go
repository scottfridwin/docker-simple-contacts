package person

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// RelationType is one of the fixed relationship kinds supported in v1. There
// is no custom-type support; the enum can grow in a future migration if
// needed.
type RelationType string

const (
	RelationParent  RelationType = "parent"
	RelationChild   RelationType = "child"
	RelationSpouse  RelationType = "spouse"
	RelationSibling RelationType = "sibling"
	RelationPartner RelationType = "partner"
)

// inverseRelationType maps a relationship type to how it reads from the
// other person's side: Parent/Child are a directional pair, the rest are
// symmetric (self-inverse).
var inverseRelationType = map[RelationType]RelationType{
	RelationParent:  RelationChild,
	RelationChild:   RelationParent,
	RelationSpouse:  RelationSpouse,
	RelationSibling: RelationSibling,
	RelationPartner: RelationPartner,
}

// Valid reports whether t is one of the supported relationship types.
func (t RelationType) Valid() bool {
	_, ok := inverseRelationType[t]
	return ok
}

// Inverse returns how this relationship type reads from the related
// person's perspective (e.g. Parent's inverse is Child).
func (t RelationType) Inverse() RelationType {
	return inverseRelationType[t]
}

// RelationshipInput is the payload accepted when creating a relationship.
// Exactly one of RelatedPersonID/RelatedPersonName must be set: a linked
// relationship points at an existing Person (its display name is always
// resolved live, never cached); an unlinked relationship is just a name,
// e.g. imported from a provider that doesn't link records, or entered by
// hand for someone not in this account's contacts.
type RelationshipInput struct {
	Type              RelationType
	RelatedPersonID   *uuid.UUID
	RelatedPersonName *string
}

// RelationshipView is a relationship as seen from one specific person's
// side, already resolved (type inverted if this is the computed reverse of
// a relationship someone else created, and the related person's current
// display name looked up live when linked).
type RelationshipView struct {
	ID                   uuid.UUID
	Type                 RelationType
	RelatedPersonID      *uuid.UUID
	RelatedPersonName    string
	RelatedPersonDeleted bool
	CreatedAt            time.Time
}

// ValidateRelationshipInput checks a relationship payload before it reaches
// the repository. personID is the person the relationship is being created
// from, so a self-relationship can be rejected early.
func ValidateRelationshipInput(personID uuid.UUID, in RelationshipInput) ValidationErrors {
	var errs ValidationErrors
	if !in.Type.Valid() {
		errs = append(errs, ValidationError{Field: "type", Message: "must be one of: parent, child, spouse, sibling, partner"})
	}

	hasID := in.RelatedPersonID != nil
	hasName := in.RelatedPersonName != nil && strings.TrimSpace(*in.RelatedPersonName) != ""
	switch {
	case hasID && hasName:
		errs = append(errs, ValidationError{Field: "related_person_name", Message: "must not be set when related_person_id is set"})
	case !hasID && !hasName:
		errs = append(errs, ValidationError{Field: "related_person_id", Message: "either related_person_id or related_person_name is required"})
	case hasID && *in.RelatedPersonID == personID:
		errs = append(errs, ValidationError{Field: "related_person_id", Message: "a person cannot be related to themselves"})
	}
	if hasName && len(*in.RelatedPersonName) > MaxNameLength {
		errs = append(errs, ValidationError{Field: "related_person_name", Message: "must be at most 255 characters"})
	}
	return errs
}
