ALTER TABLE persons ADD COLUMN is_favorite BOOLEAN NOT NULL DEFAULT false;

-- Speeds up "give me all favorites" queries used for the always-visible
-- favorites section, without bloating the index with non-favorite rows.
CREATE INDEX idx_persons_is_favorite ON persons (is_favorite) WHERE deleted_at IS NULL AND is_favorite = true;
