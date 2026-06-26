package e2e

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// seedViewer writes the viewer's repo subscriptions and review rules the same
// way production does, through pullrequest.UserStore, so the test seed can't
// drift from the app's persistence. It backs the WithRepo/WithReview/WithFreshness
// test options.
func seedViewer(ctx context.Context, pool *pgxpool.Pool, o options) error {
	store := pullrequest.NewUserStore(pool)

	for _, repo := range o.repos {
		if err := store.AddRepo(ctx, o.viewer, repo); err != nil {
			return fmt.Errorf("seed repo %s: %w", repo, err)
		}
	}

	overrides := make([]pullrequest.ReviewerOverride, len(o.review.ReviewerOverrides))
	for i, ov := range o.review.ReviewerOverrides {
		overrides[i] = pullrequest.ReviewerOverride{Label: ov.Label, Reviewers: ov.Reviewers}
	}

	settings := pullrequest.UserSettings{
		DefaultRequiredReviewers: o.review.DefaultRequiredReviewers,
		StaleAfterDays:           o.fresh.StaleAfterDays,
		RecentlyMergedDays:       o.fresh.RecentlyMergedDays,
		IgnoreLabels:             o.review.IgnoreLabels,
		BotAuthors:               o.review.BotAuthors,
		ReviewerOverrides:        overrides,
	}
	if err := store.SaveSettings(ctx, o.viewer, settings); err != nil {
		return fmt.Errorf("seed settings: %w", err)
	}
	return nil
}
