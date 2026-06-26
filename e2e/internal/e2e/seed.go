package e2e

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedViewer writes the viewer's repo subscriptions and review rules straight
// into the database, the same rows the Repositories and Review rules settings
// screens write in production. It lets the existing WithRepo/WithReview/
// WithFreshness option vocabulary keep working now that this state is per-user.
func seedViewer(ctx context.Context, pool *pgxpool.Pool, o options) error {
	for _, r := range o.repos {
		if _, err := pool.Exec(ctx,
			`INSERT INTO user_repos (user_login, repo) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			o.viewer, r.Repo); err != nil {
			return fmt.Errorf("seed repo %s: %w", r.Repo, err)
		}
	}

	ignore := o.review.IgnoreLabels
	if ignore == nil {
		ignore = []string{}
	}
	bots := o.review.BotAuthors
	if bots == nil {
		bots = []string{}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO user_settings (
    user_login, default_required_reviewers, stale_after_days, recently_merged_days,
    ignore_labels, bot_authors
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_login) DO UPDATE SET
    default_required_reviewers = EXCLUDED.default_required_reviewers,
    stale_after_days           = EXCLUDED.stale_after_days,
    recently_merged_days       = EXCLUDED.recently_merged_days,
    ignore_labels              = EXCLUDED.ignore_labels,
    bot_authors                = EXCLUDED.bot_authors`,
		o.viewer, o.review.DefaultRequiredReviewers, o.fresh.StaleAfterDays, o.fresh.RecentlyMergedDays,
		ignore, bots); err != nil {
		return fmt.Errorf("seed settings: %w", err)
	}

	for _, ov := range o.review.ReviewerOverrides {
		if _, err := pool.Exec(ctx,
			`INSERT INTO user_reviewer_overrides (user_login, label, reviewers) VALUES ($1, $2, $3)
			 ON CONFLICT (user_login, label) DO UPDATE SET reviewers = EXCLUDED.reviewers`,
			o.viewer, ov.Label, ov.Reviewers); err != nil {
			return fmt.Errorf("seed override %s: %w", ov.Label, err)
		}
	}
	return nil
}
