package e2e

import (
	"time"

	apicfg "github.com/jspdown/dashboard/api/pkg/config"
)

// Option configures the harness. Use the With* helpers; options stays
// unexported so tests can't reach fields the harness owns.
type Option func(*options)

// ReviewConfig is the review policy seeded for the viewer.
type ReviewConfig struct {
	DefaultRequiredReviewers int
	IgnoreLabels             []string
	ReviewerOverrides        []ReviewerOverride
	BotAuthors               []string
}

// ReviewerOverride pairs a PR label with a non-default reviewer count.
type ReviewerOverride struct {
	Label     string
	Reviewers int
}

// FreshnessConfig is the freshness windows seeded for the viewer.
type FreshnessConfig struct {
	StaleAfterDays     int
	RecentlyMergedDays int
}

type options struct {
	viewer string
	repos  []string
	review ReviewConfig
	fresh  FreshnessConfig
	anchor time.Time
}

func defaultOptions() options {
	return options{
		viewer: "alex",
		repos:  []string{"acme/widget"},
		review: ReviewConfig{DefaultRequiredReviewers: 2},
		fresh: FreshnessConfig{
			StaleAfterDays:     5,
			RecentlyMergedDays: 7,
		},
		anchor: time.Now().UTC(),
	}
}

// toAppConfig builds the server-level config the harness hands to the dashboard
// package. Repos and review rules are per-user, seeded into the database for the
// viewer (see seedViewer), so the config only carries the poll cadence.
func (o options) toAppConfig() *apicfg.Config {
	return &apicfg.Config{Poll: apicfg.PollConfig{Interval: time.Minute}}
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

// WithReview overrides the default review policy ({DefaultRequiredReviewers: 2}
// with no ignore labels, overrides, or bot authors).
func WithReview(rc ReviewConfig) Option {
	return func(o *options) { o.review = rc }
}

// WithFreshness overrides the default freshness windows
// (5d stale / 7d recently merged).
func WithFreshness(fc FreshnessConfig) Option {
	return func(o *options) { o.fresh = fc }
}

// WithAnchor pins the scenario time. Relative offsets in the Fake builder
// (PR.Age, AddReview, etc.) are computed against it. Defaults to
// time.Now().UTC() at Start.
func WithAnchor(t time.Time) Option {
	return func(o *options) { o.anchor = t.UTC() }
}
