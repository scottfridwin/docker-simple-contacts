import type { Person, Relationship, RelationshipType } from '../types';

/**
 * Get the human-readable label for a relationship from the perspective of viewingPersonId.
 * 
 * Semantic: relationship_type describes the relationship FROM person_id_1 TO person_id_2.
 * - "parent" means person_id_1 is the parent of person_id_2
 * - When viewing from person_id_1's perspective: "X is my child"
 * - When viewing from person_id_2's perspective: "X is my parent"
 */
export function getRelationshipLabel(
  rel: Relationship,
  viewingPersonId: string,
  otherPerson: Person,
): string {
  const isViewing_1 = viewingPersonId === rel.person_id_1;
  const isViewing_2 = viewingPersonId === rel.person_id_2;

  if (!isViewing_1 && !isViewing_2) {
    return `Relationship with ${otherPerson.display_name}`;
  }

  const type = rel.relationship_type;
  const otherName = otherPerson.display_name;

  // Directional relationships (different meaning based on perspective)
  if (type === 'parent') {
    return isViewing_1 ? `${otherName} is my child` : `${otherName} is my parent`;
  }
  if (type === 'child') {
    return isViewing_1 ? `${otherName} is my parent` : `${otherName} is my child`;
  }

  // Symmetric relationships (same meaning from both perspectives)
  if (type === 'spouse') {
    return `${otherName} is my spouse`;
  }
  if (type === 'sibling') {
    return `${otherName} is my sibling`;
  }
  if (type === 'colleague') {
    return `${otherName} is my colleague`;
  }
  if (type === 'friend') {
    return `${otherName} is my friend`;
  }
  if (type === 'other') {
    return rel.label || `Related to ${otherName}`;
  }

  return `Relationship with ${otherName}`;
}

/**
 * Determine the correct direction for storing a relationship.
 * Normalizes directional types and ensures person_id_1 < person_id_2.
 * 
 * Example:
 * - User wants: "I am parent of Bob"
 * - Input: myId, type="parent", theirId
 * - Output: { person_id_1: min(myId, theirId), person_id_2: max(...), relationship_type: "parent" or "child" }
 */
export function normalizeRelationshipDirection(
  myPersonId: string,
  otherPersonId: string,
  desiredType: RelationshipType,
): { person_id_1: string; person_id_2: string; relationship_type: RelationshipType } {
  const [id1, id2] = [myPersonId, otherPersonId].sort();
  const iAmId1 = myPersonId === id1;

  // For directional types, flip the type if I'm person_id_2
  if (desiredType === 'parent') {
    return {
      person_id_1: id1,
      person_id_2: id2,
      relationship_type: iAmId1 ? 'parent' : 'child',
    };
  }

  if (desiredType === 'child') {
    return {
      person_id_1: id1,
      person_id_2: id2,
      relationship_type: iAmId1 ? 'child' : 'parent',
    };
  }

  // For symmetric relationships, type stays the same
  return {
    person_id_1: id1,
    person_id_2: id2,
    relationship_type: desiredType,
  };
}
