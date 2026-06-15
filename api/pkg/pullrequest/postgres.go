package pullrequest

import (
	"context"
	"errors"
	"time"

	"github.com/jspdown/dashboard/api/pkg/auth"
	"github.com/jspdown/dashboard/api/pkg/config"
)

type PostgresService struct {
	store          *Store
	rules          *Rules
	staleAfterDays int
}

func NewPostgresService(store *Store, freshness config.FreshnessConfig, rules *Rules) *PostgresService {
	return &PostgresService{
		store:          store,
		rules:          rules,
		staleAfterDays: freshness.StaleAfterDays,
	}
}

// errNoUserInContext means an authed route was reached without a User in
// context, i.e. the auth middleware isn't installed. Callers surface it as 500.
var errNoUserInContext = errors.New("pullrequest: no authenticated user in context")

// List builds the dashboard payload from one atomic snapshot query. The store
// returns each PR with its collections collated; we run the domain rules to
// classify each one and hand wire-shape assembly to newPullRequestView. The
// viewer login comes from request context (set by the auth middleware).
func (s *PostgresService) List(ctx context.Context, opts ListOpts) ([]PullRequestView, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, errNoUserInContext
	}

	snapshots, err := s.store.ListSnapshotsForUser(ctx, u.Login)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	out := make([]PullRequestView, 0, len(snapshots))
	for _, snap := range snapshots {
		latest := LatestReviewsByReviewer(snap.Reviews)
		group := s.rules.ClassifyGroup(snap.PullRequest, u.Login, latest, snap.Labels, snap.ReviewRequests)
		if group == "" {
			continue
		}
		required, _ := s.rules.RequiredReviewers(snap.Labels)
		out = append(out, newPullRequestView(snap, group, u.Login, latest, required, now))
	}

	return s.filterAndSort(out, opts), nil
}

func (s *PostgresService) MarkViewed(ctx context.Context, githubID int64) error {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return errNoUserInContext
	}
	return s.store.MarkViewed(ctx, u.Login, githubID)
}

func (s *PostgresService) filterAndSort(prs []PullRequestView, opts ListOpts) []PullRequestView {
	out := make([]PullRequestView, 0, len(prs))
	for _, pr := range prs {
		if !matchFilter(pr, opts.Filter, s.staleAfterDays) {
			continue
		}
		out = append(out, pr)
	}
	applySort(out, opts.Sort)
	return out
}
