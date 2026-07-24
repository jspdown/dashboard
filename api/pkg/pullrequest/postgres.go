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

	repos, err := s.userStore.ListRepos(ctx, u.Login)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return []PullRequestView{}, nil
	}

	profiles, err := s.userStore.ListProfiles(ctx, u.Login)
	if err != nil {
		return nil, err
	}

	// Fetch one snapshot set wide enough for every repo's merged window, then
	// narrow each merged PR to its own profile's window below.
	snapshots, err := s.store.ListSnapshotsForUser(ctx, u.Login, repos, MaxRecentlyMergedWindow(profiles))
	if err != nil {
		return nil, err
	}

	// Rules are per-repo: each repo resolves to one profile (specific, else the
	// all-repos catch-all, else built-in defaults). Cache the resolved settings
	// and engine by repo so we build each once per request rather than per PR.
	type repoRules struct {
		settings ReviewSettings
		rules    *Rules
	}
	byRepo := make(map[string]repoRules, len(repos))
	rulesFor := func(repo string) repoRules {
		if rr, ok := byRepo[repo]; ok {
			return rr
		}
		settings := ResolveProfile(profiles, repo)
		rr := repoRules{settings: settings, rules: NewRules(settings)}
		byRepo[repo] = rr
		return rr
	}
	now := time.Now()

	out := make([]PullRequestView, 0, len(snapshots))
	for _, snap := range snapshots {
		latest := LatestReviewsByReviewer(snap.Reviews)

		rr := rulesFor(snap.Repo)
		group := rr.rules.ClassifyGroup(snap.PullRequest, u.Login, latest, snap.Labels, snap.ReviewRequests)
		if group == "" {
			continue
		}
		// The snapshot set used the widest merged window; drop merged PRs older
		// than this repo's own window.
		if group == GroupMerged && snap.MergedAt != nil && Age(now, *snap.MergedAt) >= rr.settings.RecentlyMergedDays {
			continue
		}

		required, _ := rr.rules.RequiredReviewers(snap.Labels)
		out = append(out, newPullRequestView(snap, group, u.Login, latest, required, rr.settings.StaleAfterDays, now))
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

// RepoView is the per-repo row on the Repositories settings screen: its polling
// health, the last poll time, and the rule profile that applies to it.
type RepoView struct {
	Repo     string     `json:"repo"`
	Health   string     `json:"health"`
	SyncedAt *time.Time `json:"synced_at,omitempty"`
	Error    string     `json:"error,omitempty"`
	Profile  string     `json:"profile,omitempty"`
}

// Repo health values, mirrored by the frontend HealthDot.
const (
	RepoHealthOK       = "ok"
	RepoHealthChecking = "checking"
	RepoHealthError    = "error"
)

// RepoOverview returns one row per repo the viewer observes, with polling health
// and the last poll time. Backs the Repositories settings screen.
func (s *PostgresService) RepoOverview(ctx context.Context) ([]RepoView, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, errNoUserInContext
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

	profiles, err := s.userStore.ListProfiles(ctx, u.Login)
	if err != nil {
		return nil, err
	}

	out := make([]RepoView, 0, len(repos))
	for _, repo := range repos {
		st := statuses[repo]
		view := RepoView{
			Repo:     repo,
			Health:   repoHealth(st),
			SyncedAt: repoSyncedAt(st),
		}
		if p := matchProfile(profiles, repo); p != nil {
			view.Profile = p.Name
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

func (s *PostgresService) filterAndSort(prs []PullRequestView, opts ListOpts) []PullRequestView {
	out := make([]PullRequestView, 0, len(prs))
	for _, pr := range prs {
		if !matchFilter(pr, opts.Filter) {
			continue
		}
		out = append(out, pr)
	}
	applySort(out, opts.Sort)
	return out
}
