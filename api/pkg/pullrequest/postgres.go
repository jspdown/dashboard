package pullrequest

import (
	"context"
	"errors"
	"time"

	"github.com/jspdown/dashboard/api/pkg/auth"
)

type PostgresService struct {
	store     *Store
	userStore *UserStore
}

func NewPostgresService(store *Store, userStore *UserStore) *PostgresService {
	return &PostgresService{store: store, userStore: userStore}
}

// errNoUserInContext means an authed route was reached without a User in
// context, i.e. the auth middleware isn't installed. Callers surface it as 500.
var errNoUserInContext = errors.New("pullrequest: no authenticated user in context")

// List builds the dashboard payload for the viewer. Repos and review rules are
// per-user: we load the viewer's observed repos and settings, snapshot only
// those repos, then run the viewer's rules to classify each PR. The viewer login
// comes from request context (set by the auth middleware).
func (s *PostgresService) List(ctx context.Context, opts ListOpts) ([]PullRequestView, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, errNoUserInContext
	}

	settings, err := s.userStore.GetSettings(ctx, u.Login)
	if err != nil {
		return nil, err
	}
	repos, err := s.userStore.ListRepos(ctx, u.Login)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return []PullRequestView{}, nil
	}

	snapshots, err := s.store.ListSnapshotsForUser(ctx, u.Login, repos, settings.RecentlyMergedDays)
	if err != nil {
		return nil, err
	}

	rules := NewRules(settings)
	now := time.Now()

	out := make([]PullRequestView, 0, len(snapshots))
	for _, snap := range snapshots {
		latest := LatestReviewsByReviewer(snap.Reviews)

		group := rules.ClassifyGroup(snap.PullRequest, u.Login, latest, snap.Labels, snap.ReviewRequests)
		if group == "" {
			continue
		}

		required, _ := rules.RequiredReviewers(snap.Labels)
		out = append(out, newPullRequestView(snap, group, u.Login, latest, required, now))
	}

	return s.filterAndSort(out, opts, settings.StaleAfterDays), nil
}

func (s *PostgresService) MarkViewed(ctx context.Context, githubID int64) error {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return errNoUserInContext
	}
	return s.store.MarkViewed(ctx, u.Login, githubID)
}

// RepoView is the per-repo row on the Repositories settings screen: its polling
// health, the user's open/needs-review counts, and the last poll time.
type RepoView struct {
	Repo     string     `json:"repo"`
	Health   string     `json:"health"`
	Open     int        `json:"open"`
	Needs    int        `json:"needs"`
	SyncedAt *time.Time `json:"synced_at,omitempty"`
	Error    string     `json:"error,omitempty"`
}

// Repo health values, mirrored by the frontend HealthDot.
const (
	RepoHealthOK       = "ok"
	RepoHealthChecking = "checking"
	RepoHealthError    = "error"
)

// RepoOverview returns one row per repo the viewer observes, with polling health
// and the viewer's open / needs-review counts for that repo. Backs the
// Repositories settings screen.
func (s *PostgresService) RepoOverview(ctx context.Context) ([]RepoView, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, errNoUserInContext
	}

	settings, err := s.userStore.GetSettings(ctx, u.Login)
	if err != nil {
		return nil, err
	}
	repos, err := s.userStore.ListRepos(ctx, u.Login)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return []RepoView{}, nil
	}

	statuses, err := s.userStore.RepoStatuses(ctx, repos)
	if err != nil {
		return nil, err
	}
	snapshots, err := s.store.ListSnapshotsForUser(ctx, u.Login, repos, settings.RecentlyMergedDays)
	if err != nil {
		return nil, err
	}

	rules := NewRules(settings)
	open := make(map[string]int, len(repos))
	needs := make(map[string]int, len(repos))
	for _, snap := range snapshots {
		if snap.Status == StatusOpen {
			open[snap.Repo]++
		}
		latest := LatestReviewsByReviewer(snap.Reviews)
		if rules.ClassifyGroup(snap.PullRequest, u.Login, latest, snap.Labels, snap.ReviewRequests) == GroupReview {
			needs[snap.Repo]++
		}
	}

	out := make([]RepoView, 0, len(repos))
	for _, repo := range repos {
		st := statuses[repo]
		view := RepoView{
			Repo:     repo,
			Health:   repoHealth(st),
			Open:     open[repo],
			Needs:    needs[repo],
			SyncedAt: repoSyncedAt(st),
		}
		if st.LastError != nil {
			view.Error = *st.LastError
		}
		out = append(out, view)
	}
	return out, nil
}

// repoHealth maps a repo's cursor row to a health state. No row (or no poll yet)
// means it was just added and hasn't been reached: "checking". A recorded error
// means the server lost access: "error". Otherwise it's polling: "ok".
func repoHealth(st RepoStatus) string {
	switch {
	case st.LastPolledAt == nil:
		return RepoHealthChecking
	case st.LastError != nil:
		return RepoHealthError
	default:
		return RepoHealthOK
	}
}

// repoSyncedAt prefers the last poll attempt (so a quiet repo still shows a
// fresh time) and falls back to the cursor's last advance.
func repoSyncedAt(st RepoStatus) *time.Time {
	if st.LastPolledAt != nil {
		return st.LastPolledAt
	}
	return st.LastSyncedAt
}

func (s *PostgresService) filterAndSort(prs []PullRequestView, opts ListOpts, staleAfterDays int) []PullRequestView {
	out := make([]PullRequestView, 0, len(prs))
	for _, pr := range prs {
		if !matchFilter(pr, opts.Filter, staleAfterDays) {
			continue
		}
		out = append(out, pr)
	}
	applySort(out, opts.Sort)
	return out
}
