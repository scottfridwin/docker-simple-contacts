CREATE TABLE sync_jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     UUID REFERENCES users(id),
    person_id    UUID NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    snapshot     JSONB NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    processed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sync_jobs_owner_id ON sync_jobs (owner_id);
CREATE INDEX idx_sync_jobs_person_id ON sync_jobs (person_id);
CREATE INDEX idx_sync_jobs_status ON sync_jobs (status);
