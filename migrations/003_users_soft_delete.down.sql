DROP INDEX IF EXISTS users_deleted_idx;
ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
