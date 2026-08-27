-- Create relationships table for person-to-person connections
CREATE TABLE relationships (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id_1       UUID NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
    person_id_2       UUID NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
    relationship_type TEXT NOT NULL,
    label             TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ,
    
    -- Prevent duplicate relationships in both directions
    CONSTRAINT relationships_not_self CHECK (person_id_1 <> person_id_2),
    CONSTRAINT relationships_ordered CHECK (person_id_1 < person_id_2)
);

-- Indexes for common queries
CREATE INDEX idx_relationships_person_id_1 ON relationships (person_id_1) WHERE deleted_at IS NULL;
CREATE INDEX idx_relationships_person_id_2 ON relationships (person_id_2) WHERE deleted_at IS NULL;
CREATE INDEX idx_relationships_type ON relationships (relationship_type) WHERE deleted_at IS NULL;
CREATE INDEX idx_relationships_deleted_at ON relationships (deleted_at) WHERE deleted_at IS NOT NULL;
