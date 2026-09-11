ALTER TABLE sync_jobs
    ADD COLUMN origin_account_id UUID REFERENCES sync_accounts(id) ON DELETE SET NULL;
