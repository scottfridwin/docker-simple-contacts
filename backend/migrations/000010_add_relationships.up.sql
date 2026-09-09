-- Person-to-person relationships (Parent/Child, Spouse, Sibling, Partner).
-- A single row is stored from the creating person's perspective
-- (person_id -> related_person_id/name); the reverse view (e.g. Child, for
-- the Parent relationship above) is computed at read time by inverting the
-- type, so there is exactly one row per relationship and no risk of the two
-- sides drifting out of sync.
--
-- related_person_id is nullable to support relationships synced from
-- providers (e.g. Google) that only store a free-text name with no link to
-- an actual contact record, and relationships the user typed a name for
-- without linking an existing contact.
CREATE TABLE person_relationships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID REFERENCES users(id),
    person_id UUID NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
    related_person_id UUID REFERENCES persons(id) ON DELETE CASCADE,
    related_person_name TEXT,
    type TEXT NOT NULL CHECK (type IN ('parent', 'child', 'spouse', 'sibling', 'partner')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (related_person_id IS NOT NULL OR related_person_name IS NOT NULL),
    CHECK (related_person_id IS NULL OR related_person_id <> person_id)
);

CREATE INDEX idx_person_relationships_person_id ON person_relationships (person_id);
CREATE INDEX idx_person_relationships_related_person_id ON person_relationships (related_person_id) WHERE related_person_id IS NOT NULL;

-- Prevent duplicate links of the same type between the same two linked
-- contacts. Unlinked (name-only) relationships are not constrained, since
-- there could legitimately be more than one unresolved "Jane Doe".
CREATE UNIQUE INDEX idx_person_relationships_unique_link ON person_relationships (person_id, related_person_id, type) WHERE related_person_id IS NOT NULL;
