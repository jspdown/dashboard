-- Per-user repositories, scoped to each viewer. The server polls with one PAT
-- over the union of every user's subscriptions, and each user sees only their
-- own repos. Review rules live in rule profiles (see 20260626130000).

-- user_repos: which repos a user observes. The poller services the distinct
-- union across all users; List filters each viewer to their own subset.
CREATE TABLE user_repos (
    user_login TEXT        NOT NULL,
    repo       TEXT        NOT NULL,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_login, repo)
);

CREATE INDEX user_repos_repo_idx ON user_repos (repo);

-- Per-repo polling health, surfaced on the Repositories settings screen.
-- last_synced_at already tracks the cursor; these record the last poll attempt
-- (so a quiet repo still shows a fresh "last sync") and its error, if any.
ALTER TABLE repo_sync_cursors ADD COLUMN last_polled_at TIMESTAMPTZ;
ALTER TABLE repo_sync_cursors ADD COLUMN last_error     TEXT;
