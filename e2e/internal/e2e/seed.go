package e2e

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

func seedViewer(ctx context.Context, pool *pgxpool.Pool, o options) error {
	store := pullrequest.NewUserStore(pool)

	for _, repo := range o.repos {
		if err := store.AddRepo(ctx, o.viewer, repo); err != nil {
			return fmt.Errorf("seed repo %s: %w", repo, err)
		}
	}

	for _, p := range o.profiles {
		overrides := make([]pullrequest.ReviewerOverride, len(p.ReviewerOverrides))
		for i, ov := range p.ReviewerOverrides {
			overrides[i] = pullrequest.ReviewerOverride{Label: ov.Label, Reviewers: ov.Reviewers}
		}

		name := p.Name
		if name == "" {
			name = "Rules"
		}
		stale := p.StaleAfterDays
		if stale == 0 {
			stale = pullrequest.DefaultStaleAfterDays
		}
		merged := p.RecentlyMergedDays
		if merged == 0 {
			merged = pullrequest.DefaultRecentlyMergedDays
		}

		profile := pullrequest.RuleProfile{
			Name:     name,
			AllRepos: p.AllRepos,
			Repos:    p.Repos,
			ReviewSettings: pullrequest.ReviewSettings{
				DefaultRequiredReviewers: p.DefaultRequiredReviewers,
				StaleAfterDays:           stale,
				RecentlyMergedDays:       merged,
				IgnoreLabels:             p.IgnoreLabels,
				BotAuthors:               p.BotAuthors,
				ReviewerOverrides:        overrides,
			},
		}
		if _, err := store.CreateProfile(ctx, o.viewer, profile); err != nil {
			return fmt.Errorf("seed profile %q: %w", name, err)
		}
	}
	return nil
}
