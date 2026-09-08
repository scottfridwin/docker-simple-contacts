CREATE TABLE sync_accounts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id             UUID REFERENCES users(id),
    provider             TEXT NOT NULL,
    provider_account_id  TEXT NOT NULL,
    access_token         TEXT,
    refresh_token        TEXT,
    expires_at           TIMESTAMPTZ,
    scope                TEXT NOT NULL DEFAULT '',
    sync_cursor          TEXT NOT NULL DEFAULT '',
    sync_frequency_minutes INTEGER NOT NULL DEFAULT 60,
    status               TEXT NOT NULL DEFAULT 'connected',
    last_synced_at       TIMESTAMPTZ,
    last_error           TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_account_id)
);

CREATE INDEX idx_sync_accounts_owner_id ON sync_accounts (owner_id);
CREATE INDEX idx_sync_accounts_status ON sync_accounts (status);
CREATE INDEX idx_sync_accounts_last_synced_at ON sync_accounts (last_synced_at);
