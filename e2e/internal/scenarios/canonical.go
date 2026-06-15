// Package scenarios holds reusable seed data for the e2e harness, one
// function per "demo state" the dashboard ships with. The screenshot
// suite and the dev-e2e binary share them so both render the same data.
//
// Aim new scenarios at every dashboard group and most badge/state
// variants (stale ages, CI mixes, draft vs open, review verdicts,
// merge-readiness) so the resulting screenshot is visually rich.
package scenarios

import (
	"time"

	"github.com/jspdown/dashboard/e2e/internal/githubtest"
)

// Demo seeds a representative dashboard state across multiple repos
// for the named viewer. Polling the configured repos afterwards fills
// every dashboard group:
//
//   - "Needs my review": PRs from teammates needing the viewer's review
//   - "My open PRs": viewer-authored open PRs with assorted ages and CI
//   - "Reviewed by me": PRs the viewer already approved or blocked
//   - "Renovate": dependabot-authored dependency bumps
//   - "Recently merged": viewer-authored PRs merged in the freshness window
//
// The mix mirrors jspdown's real dashboard so screenshots feel
// realistic, with no real PR titles or author handles.
func Demo(s *githubtest.Server, viewer string) {
	now := s.Anchor()

	// platform/api-gateway: heaviest review queue
	apigw := s.Repo("platform/api-gateway")
	apigw.PR(4821).
		Title("Add tracing to auth middleware").
		Author("carol").
		Open().
		Age(1*24*time.Hour).
		Reviewers(viewer).
		AddReview("marco", "approved", -6*time.Hour) // 1/2, one more LGTM needed
	apigw.PR(4799).
		Title("Add rate-limit headers to public API").
		Author("sarah").
		Open().
		Age(4*24*time.Hour).
		Reviewers(viewer).
		AddCheck("ci/build", "completed", "failure", -2*time.Hour).
		AddReview("carol", "approved", -3*time.Hour) // approved but CI failing, blocked
	apigw.PR(4830).
		Title("Bump pg-driver to 12.1.0 (security)").
		Author("dependabot[bot]").
		Open().
		Age(2*time.Hour).
		AddCheck("ci/build", "in_progress", "", 0)
	apigw.PR(4801).
		Title("Bump go to 1.23.4").
		Author(viewer).
		Merged(-2*24*time.Hour, viewer)

	// platform/auth: viewer's main repo, lots of "mine"
	auth := s.Repo("platform/auth")
	auth.PR(1102).
		Title("Refactor session store to use Redis cluster").
		Author("marco").
		Open().
		Age(2 * 24 * time.Hour).
		Reviewers(viewer)
	auth.PR(1234).
		Title("Migrate auth to OIDC").
		Author(viewer).
		Open().
		Age(4*24*time.Hour).
		AddCheck("ci/build", "completed", "failure", -1*time.Hour).
		AddReview("marco", "approved", -20*time.Hour).
		AddReview("sarah", "approved", -18*time.Hour) // 2/2 but CI failing, blocked
	auth.PR(1241).
		Title("Drop unused feature flag: legacy_signin").
		Author(viewer).
		Open().
		Age(2*time.Hour).
		AddCheck("ci/build", "in_progress", "", 0).
		AddReview("carol", "approved", -1*time.Hour).
		AddReview("marco", "approved", -1*time.Hour) // 2/2, waiting on CI
	auth.PR(1198).
		Title("Tighten CSRF check on /admin endpoints").
		Author("marco").
		Open().
		Age(3*24*time.Hour).
		AddReview(viewer, "approved", -1*24*time.Hour).
		AddReview("sarah", "approved", -20*time.Hour) // 2/2, ready to merge
	auth.PR(1230).
		Title("Fix flake in auth_test.py").
		Author(viewer).
		Merged(-1*24*time.Hour, viewer)

	// platform/jobs: RFC and a reviewed PR
	jobs := s.Repo("platform/jobs")
	jobs.PR(88).
		Title("RFC: background job retry semantics").
		Author(viewer).
		Open().
		Age(8*24*time.Hour).                            // stale
		AddReview("priya", "approved", -2*24*time.Hour) // 1/2, one more LGTM needed
	jobs.PR(90).
		Title("Switch jobs queue to streams API").
		Author("priya").
		Open().
		Age(2*24*time.Hour).
		AddReview(viewer, "changes_requested", -3*time.Hour)

	// platform/db: older spike from the viewer
	db := s.Repo("platform/db")
	db.PR(412).
		Title("Spike: per-tenant query budgets").
		Author(viewer).
		Open().
		Age(6 * 24 * time.Hour) // stale, almost the threshold

	// platform/observability: quick win from the viewer
	obs := s.Repo("platform/observability")
	obs.PR(901).
		Title("Tidy log redaction rules").
		Author(viewer).
		Open().
		Age(1*24*time.Hour).
		AddReview("marco", "approved", -12*time.Hour).
		AddReview("carol", "approved", -10*time.Hour) // 2/2, ready to merge

	_ = now // anchor side-effect; kept for future scenarios that need it
}

// DemoRepos returns the slugs Demo seeds, in poll order. Lets callers
// wire poll-config without duplicating the list across the harness
// Option list and the seed function.
func DemoRepos() []string {
	return []string{
		"platform/api-gateway",
		"platform/auth",
		"platform/jobs",
		"platform/db",
		"platform/observability",
	}
}
