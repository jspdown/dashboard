ALTER TABLE repo_sync_cursors DROP COLUMN IF EXISTS last_error;
ALTER TABLE repo_sync_cursors DROP COLUMN IF EXISTS last_polled_at;

DROP TABLE IF EXISTS user_repos;
