DROP INDEX IF EXISTS idx_persons_is_favorite;
ALTER TABLE persons DROP COLUMN IF EXISTS is_favorite;
