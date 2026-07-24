package e2e

import (
	"time"
)

// Option configures the harness. Use the With* helpers; options stays
// unexported so tests can't reach fields the harness owns.
type Option func(*options)

// Profile is a rule profile seeded for the viewer: a self-contained review
// policy scoped to a list of repos or, when AllRepos is set, every observed repo
// no specific profile claims. Zero StaleAfterDays / RecentlyMergedDays seed the
// built-in defaults (see seedViewer).
type Profile struct {
	Name                     string
	AllRepos                 bool
	Repos                    []string
	DefaultRequiredReviewers int
	StaleAfterDays           int
	RecentlyMergedDays       int
	IgnoreLabels             []string
	BotAuthors               []string
	ReviewerOverrides        []ReviewerOverride
}

// ReviewerOverride pairs a PR label with a non-default reviewer count.
type ReviewerOverride struct {
	Label     string
	Reviewers int
}

type options struct {
	viewer   string
	repos    []string
	profiles []Profile
	anchor   time.Time
}

func defaultOptions() options {
	return options{
		viewer: "alex",
		repos:  []string{"acme/widget"},
		anchor: time.Now().UTC(),
	}
}

// WithViewer sets the viewer login (default "alex"). The harness injects it as
// X-Forwarded-User so the auth middleware sees an authenticated user.
func WithViewer(login string) Option {
	return func(o *options) { o.viewer = login }
}

// WithRepo subscribes the viewer to a repo. Repeated calls accumulate.
// Defaults to a single "acme/widget".
func WithRepo(slug string) Option {
	return func(o *options) {
		// Replace the default placeholder repo on first call; append after.
		if len(o.repos) == 1 && o.repos[0] == "acme/widget" {
			o.repos = []string{slug}
			return
		}
		o.repos = append(o.repos, slug)
	}
}

// WithProfile seeds a rule profile for the viewer. Repeated calls accumulate.
// With no profile, repos fall back to the built-in defaults (2 reviewers,
// 5d stale, 7d recently merged), matching an unconfigured user.
func WithProfile(p Profile) Option {
	return func(o *options) { o.profiles = append(o.profiles, p) }
}

// WithAnchor pins the scenario time. Relative offsets in the Fake builder
// (PR.Age, AddReview, etc.) are computed against it. Defaults to
// time.Now().UTC() at Start.
func WithAnchor(t time.Time) Option {
	return func(o *options) { o.anchor = t.UTC() }
}
