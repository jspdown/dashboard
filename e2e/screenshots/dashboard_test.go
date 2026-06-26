// Package screenshots_test produces canonical UI captures of the
// dashboard. Every test seeds the e2e harness with a scenario, drives
// the chromedp tab into a target state, and writes a PNG via
// Browser.Screenshot.
//
// Output goes to $DASHBOARD_SCREENSHOT_DIR (set by `make screenshots
// OUT=...`) or a per-test t.TempDir() for ad-hoc runs. Add a new test
// per screenshot. Same authoring vocabulary as the e2e scenarios suite;
// shared scenarios live in internal/scenarios.
package screenshots_test

import (
	"testing"

	"github.com/jspdown/dashboard/e2e/internal/e2e"
	"github.com/jspdown/dashboard/e2e/internal/scenarios"
)

// demoOpts is the harness configuration the canonical screenshots
// run against. Mirrors the production review policy so the rendered
// dashboard looks like a real deployment.
func demoOpts(viewer string) []e2e.Option {
	repos := scenarios.DemoRepos()
	opts := make([]e2e.Option, 0, 2+len(repos))
	opts = append(opts,
		e2e.WithViewer(viewer),
		e2e.WithReview(e2e.ReviewConfig{
			DefaultRequiredReviewers: 2,
			IgnoreLabels:             []string{"area/webui"},
			ReviewerOverrides: []e2e.ReviewerOverride{
				{Label: "bot/light-review", Reviewers: 1},
			},
			BotAuthors: []string{"dependabot[bot]"},
		}),
	)
	for _, slug := range repos {
		opts = append(opts, e2e.WithRepo(slug))
	}
	return opts
}

// seedAndPoll seeds the demo scenario and polls every repo so the
// dashboard is fully populated before the screenshot is captured.
func seedAndPoll(t *testing.T, h *e2e.Harness, viewer string) {
	t.Helper()
	scenarios.Demo(h.Fake, viewer)
	for _, slug := range scenarios.DemoRepos() {
		h.Poll(slug)
	}
	h.Reload()
}

// TestScreenshot_DashboardOverview captures the default dashboard
// view: every group rendered, mix of authors / ages / CI states /
// stale badges. The "headline" screenshot for docs and the README.
func TestScreenshot_DashboardOverview(t *testing.T) {
	t.Parallel()
	const viewer = "alex"
	h := e2e.Start(t, demoOpts(viewer)...)
	seedAndPoll(t, h, viewer)
	h.Browser.Screenshot("dashboard-overview")
}

// TestScreenshot_AllGroupsExpanded captures the same scenario with
// every group expanded so the "Reviewed by me" rows (collapsed by
// default) are visible. Useful as a "show me everything" reference.
func TestScreenshot_AllGroupsExpanded(t *testing.T) {
	t.Parallel()
	const viewer = "alex"
	h := e2e.Start(t, demoOpts(viewer)...)
	seedAndPoll(t, h, viewer)
	h.Browser.ExpandAllGroups()
	h.Browser.Screenshot("dashboard-all-groups-expanded")
}

// TestScreenshot_GroupTooltips renders the per-group tooltip text as
// a fixed banner at the top of the page (chromedp doesn't capture
// native browser tooltips). The result is a one-stop reference for
// what each group means and which config knobs name each label.
func TestScreenshot_GroupTooltips(t *testing.T) {
	t.Parallel()
	const viewer = "alex"
	h := e2e.Start(t, demoOpts(viewer)...)
	seedAndPoll(t, h, viewer)
	h.Browser.RenderTooltipsOverlay()
	h.Browser.Screenshot("dashboard-group-tooltips")
}

