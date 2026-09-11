-- Sharing a Person record with another account. The owner grants
-- view+edit access to a specific other user (looked up by exact email
-- match, so the recipient must already have logged in at least once).
-- Deletion, restoring, hard-deleting, re-sharing, and relationship
-- management on a shared Person remain owner-only; only the base Person
-- record (GET single/list, PATCH) is accessible to a share recipient.
CREATE TABLE person_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id UUID NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
    shared_with_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (person_id, shared_with_user_id)
);

CREATE INDEX idx_person_shares_person_id ON person_shares (person_id);
CREATE INDEX idx_person_shares_shared_with_user_id ON person_shares (shared_with_user_id);
