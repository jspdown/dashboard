-- Rule profiles are the source of review rules, but the rows users saved under
-- the previous user_settings shape were never carried over. A user with no
-- profile resolves to the built-in defaults, which silently drops their ignore
-- labels, bot authors, and reviewer overrides. Give each such user an all-repos
-- catch-all profile holding what they had.
--
-- Users who already have a profile are left alone, so this is safe to re-run and
-- never overwrites a profile configured from the Settings screen. A database
-- created after user_settings left the schema has nothing to carry and skips.

DO $$
BEGIN
    IF to_regclass('public.user_settings') IS NULL
       OR to_regclass('public.user_reviewer_overrides') IS NULL THEN
        RETURN;
    END IF;

    WITH carried AS (
        INSERT INTO user_rule_profiles (
            user_login, name, all_repos, default_required_reviewers,
            stale_after_days, recently_merged_days, ignore_labels, bot_authors, position
        )
        SELECT s.user_login, 'Default', true, s.default_required_reviewers,
               s.stale_after_days, s.recently_merged_days, s.ignore_labels, s.bot_authors, 0
        FROM user_settings s
        WHERE NOT EXISTS (
            SELECT 1 FROM user_rule_profiles p WHERE p.user_login = s.user_login
        )
        RETURNING id, user_login
    )
    INSERT INTO user_rule_profile_reviewer_overrides (profile_id, label, reviewers)
    SELECT c.id, o.label, o.reviewers
    FROM carried c
    JOIN user_reviewer_overrides o ON o.user_login = c.user_login;
END $$;
