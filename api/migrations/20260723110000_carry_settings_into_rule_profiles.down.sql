-- Drop only the catch-all profiles that still mirror the user_settings row they
-- came from. A profile edited since is no longer a copy and is kept. Reviewer
-- overrides cascade with the profile.

DO $$
BEGIN
    IF to_regclass('public.user_settings') IS NULL THEN
        RETURN;
    END IF;

    DELETE FROM user_rule_profiles p
    USING user_settings s
    WHERE p.user_login = s.user_login
      AND p.all_repos
      AND p.name = 'Default'
      AND p.default_required_reviewers = s.default_required_reviewers
      AND p.stale_after_days = s.stale_after_days
      AND p.recently_merged_days = s.recently_merged_days
      AND p.ignore_labels = s.ignore_labels
      AND p.bot_authors = s.bot_authors;
END $$;
