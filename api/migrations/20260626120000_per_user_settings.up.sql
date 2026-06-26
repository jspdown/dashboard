-- Per-user repositories and review rules. Repos and review policy used to live
-- in the YAML config and applied globally; they now live in the database, scoped
-- to each viewer. The server still polls with one PAT, but the set of polled
-- repos is the union of every user's subscriptions, and each user sees only
-- their own repos filtered by their own rules.

-- user_repos: which repos a user observes. The poller services the distinct
-- union across all users; List filters each viewer to their own subset.
CREATE TABLE user_repos (
    user_login TEXT        NOT NULL,
    repo       TEXT        NOT NULL,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_login, repo)
);

CREATE INDEX user_repos_repo_idx ON user_repos (repo);

-- user_settings: a user's review-rule defaults. Rows are created lazily on the
-- first save; a missing row means the built-in defaults apply.
CREATE TABLE user_settings (
    user_login                 TEXT        PRIMARY KEY,
    default_required_reviewers INT         NOT NULL DEFAULT 2,
    stale_after_days           INT         NOT NULL DEFAULT 5,
    recently_merged_days       INT         NOT NULL DEFAULT 7,
    ignore_labels              TEXT[]      NOT NULL DEFAULT '{}',
    bot_authors                TEXT[]      NOT NULL DEFAULT '{}',
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- user_reviewer_overrides: per-user, per-label required-reviewer counts. The
-- first matching label wins; unmatched labels fall back to the user's default.
CREATE TABLE user_reviewer_overrides (
    user_login TEXT NOT NULL,
    label      TEXT NOT NULL,
    reviewers  INT  NOT NULL,
    PRIMARY KEY (user_login, label)
);

-- Per-repo polling health, surfaced on the Repositories settings screen.
-- last_synced_at already tracks the cursor; these record the last poll attempt
-- (so a quiet repo still shows a fresh "last sync") and its error, if any.
ALTER TABLE repo_sync_cursors ADD COLUMN last_polled_at TIMESTAMPTZ;
ALTER TABLE repo_sync_cursors ADD COLUMN last_error     TEXT;
