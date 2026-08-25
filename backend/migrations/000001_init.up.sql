CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE persons (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name    TEXT NOT NULL,
    middle_names  TEXT[] NOT NULL DEFAULT '{}',
    last_name     TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    custom_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

-- Default list ordering is display_name; index the common access paths.
CREATE INDEX idx_persons_display_name ON persons (display_name) WHERE deleted_at IS NULL;
CREATE INDEX idx_persons_last_name ON persons (last_name) WHERE deleted_at IS NULL;
CREATE INDEX idx_persons_deleted_at ON persons (deleted_at) WHERE deleted_at IS NOT NULL;
