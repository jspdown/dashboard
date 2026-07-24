-- 20260626130000 was rewritten in place after it had already been applied, so
-- databases that recorded that version never received the rule profile tables.
-- Create them here. Idempotent: a database that got them from 20260626130000
-- passes through unchanged.
--
-- The tables rule profiles supersede (user_settings, user_reviewer_overrides,
-- user_repo_settings, user_repo_reviewer_overrides) still hold each user's
-- previous review rules, so they are left in place. Drop them in a later
-- migration once those rules have been carried into a profile.

CREATE TABLE IF NOT EXISTS user_rule_profiles (
    id                         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_login                 TEXT        NOT NULL,
    name                       TEXT        NOT NULL,
    all_repos                  BOOLEAN     NOT NULL DEFAULT false,
    default_required_reviewers INT         NOT NULL DEFAULT 2,
    stale_after_days           INT         NOT NULL DEFAULT 5,
    recently_merged_days       INT         NOT NULL DEFAULT 7,
    ignore_labels              TEXT[]      NOT NULL DEFAULT '{}',
    bot_authors                TEXT[]      NOT NULL DEFAULT '{}',
    position                   INT         NOT NULL DEFAULT 0,
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS user_rule_profiles_user_idx ON user_rule_profiles (user_login);

-- At most one all-repos catch-all per user.
CREATE UNIQUE INDEX IF NOT EXISTS user_rule_profiles_all_repos_uniq
    ON user_rule_profiles (user_login) WHERE all_repos;

-- user_rule_profile_repos: the repos a specific profile targets. The
-- (user_login, repo) unique constraint enforces one specific profile per repo.
CREATE TABLE IF NOT EXISTS user_rule_profile_repos (
    profile_id BIGINT NOT NULL REFERENCES user_rule_profiles (id) ON DELETE CASCADE,
    user_login TEXT   NOT NULL,
    repo       TEXT   NOT NULL,
    PRIMARY KEY (profile_id, repo),
    UNIQUE (user_login, repo)
);

-- user_rule_profile_reviewer_overrides: per-profile, per-label required-reviewer
-- counts. The first matching label wins; unmatched labels fall back to the
-- profile's default_required_reviewers.
CREATE TABLE IF NOT EXISTS user_rule_profile_reviewer_overrides (
    profile_id BIGINT NOT NULL REFERENCES user_rule_profiles (id) ON DELETE CASCADE,
    label      TEXT   NOT NULL,
    reviewers  INT    NOT NULL,
    PRIMARY KEY (profile_id, label)
);
