ALTER TABLE sync_accounts ADD COLUMN display_name TEXT;

CREATE TABLE sync_record_links (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sync_account_id   UUID NOT NULL REFERENCES sync_accounts(id) ON DELETE CASCADE,
    person_id         UUID NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
    remote_id         TEXT NOT NULL,
    remote_updated_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (sync_account_id, person_id),
    UNIQUE (sync_account_id, remote_id)
);

CREATE INDEX idx_sync_record_links_person_id ON sync_record_links (person_id);
