package e2e

import (
	"time"

	apicfg "github.com/jspdown/dashboard/api/pkg/config"
)

// Option configures the harness. Use the With* helpers; options stays
// unexported so tests can't reach fields the harness owns.
type Option func(*options)

type options struct {
	viewer string
	repos  []apicfg.RepoConfig
	review apicfg.ReviewConfig
	fresh  apicfg.FreshnessConfig
	anchor time.Time
}

func defaultOptions() options {
	return options{
		viewer: "alex",
		repos:  []apicfg.RepoConfig{{Repo: "acme/widget", Interval: time.Minute}},
		review: apicfg.ReviewConfig{DefaultRequiredReviewers: 2},
		fresh: apicfg.FreshnessConfig{
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

// WithRepos sets the polled repos. Each repo must appear once with a positive
// interval. Defaults to a single "acme/widget" polled every minute.
func WithRepos(repos ...apicfg.RepoConfig) Option {
	return func(o *options) { o.repos = repos }
}

// WithRepo is a one-line shortcut for adding a single repo with a
// 1-minute interval. Repeated calls accumulate.
func WithRepo(slug string) Option {
	return func(o *options) {
		// Replace the default placeholder repo on first call; append after.
		if len(o.repos) == 1 && o.repos[0].Repo == "acme/widget" {
			o.repos = []apicfg.RepoConfig{{Repo: slug, Interval: time.Minute}}
			return
		}
		o.repos = append(o.repos, apicfg.RepoConfig{Repo: slug, Interval: time.Minute})
	}
}

// WithReview overrides the default review policy ({DefaultRequiredReviewers: 2}
// with no ignore labels, overrides, or bot authors).
func WithReview(rc apicfg.ReviewConfig) Option {
	return func(o *options) { o.review = rc }
}

// WithFreshness overrides the default freshness windows
// (5d stale / 7d recently merged).
func WithFreshness(fc apicfg.FreshnessConfig) Option {
	return func(o *options) { o.fresh = fc }
}

// WithAnchor pins the scenario time. Relative offsets in the Fake builder
// (PR.Age, AddReview, etc.) are computed against it. Defaults to
// time.Now().UTC() at Start.
func WithAnchor(t time.Time) Option {
	return func(o *options) { o.anchor = t.UTC() }
}
