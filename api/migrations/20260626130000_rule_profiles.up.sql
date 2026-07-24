-- Rule profiles: a user's review rules as named, self-contained policies. Each
-- profile targets either an explicit set of repos or, when all_repos is set,
-- every observed repo no specific profile claims. List resolves one profile per
-- repo (specific wins, else the all-repos catch-all, else built-in defaults) and
-- classifies that repo's PRs with it. There is no inheritance between profiles.

CREATE TABLE user_rule_profiles (
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

CREATE INDEX user_rule_profiles_user_idx ON user_rule_profiles (user_login);

-- At most one all-repos catch-all per user.
CREATE UNIQUE INDEX user_rule_profiles_all_repos_uniq
    ON user_rule_profiles (user_login) WHERE all_repos;

-- user_rule_profile_repos: the repos a specific profile targets. The
-- (user_login, repo) unique constraint enforces one specific profile per repo.
CREATE TABLE user_rule_profile_repos (
    profile_id BIGINT NOT NULL REFERENCES user_rule_profiles (id) ON DELETE CASCADE,
    user_login TEXT   NOT NULL,
    repo       TEXT   NOT NULL,
    PRIMARY KEY (profile_id, repo),
    UNIQUE (user_login, repo)
);

-- user_rule_profile_reviewer_overrides: per-profile, per-label required-reviewer
-- counts. The first matching label wins; unmatched labels fall back to the
-- profile's default_required_reviewers.
CREATE TABLE user_rule_profile_reviewer_overrides (
    profile_id BIGINT NOT NULL REFERENCES user_rule_profiles (id) ON DELETE CASCADE,
    label      TEXT   NOT NULL,
    reviewers  INT    NOT NULL,
    PRIMARY KEY (profile_id, label)
);
