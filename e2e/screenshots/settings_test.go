package screenshots_test

import (
	"testing"

	"github.com/jspdown/dashboard/e2e/internal/e2e"
)

// TestScreenshot_SettingsRepos captures the per-user Repositories settings
// screen: the add-repo bar, the observed-repo list with PR counts and last-sync
// status, and the team suggestions. Reuses the demo scenario so the rows are
// populated.
func TestScreenshot_SettingsRepos(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t, demoOpts()...)
	seedAndPoll(t, h)
	h.Browser.GotoSettings("/settings/repos")
	h.Browser.Screenshot("settings-repositories")
}

// TestScreenshot_SettingsRules captures the per-user Review rules screen: the
// list of rule profiles. The demo "Team defaults" all-repos profile sits
// alongside a repo-scoped "Critical infra" profile, so the capture shows both
// scope modes (the all-repos toggle and the repo picker) plus required
// reviewers, per-label overrides, ignore labels, bot authors, and freshness.
func TestScreenshot_SettingsRules(t *testing.T) {
	t.Parallel()
	opts := append(demoOpts(), e2e.WithProfile(e2e.Profile{
		Name:                     "Critical infra",
		Repos:                    []string{"platform/api-gateway"},
		DefaultRequiredReviewers: 3,
		IgnoreLabels:             []string{"wip"},
		BotAuthors:               []string{"renovate[bot]"},
		ReviewerOverrides:        []e2e.ReviewerOverride{{Label: "area/security", Reviewers: 4}},
		StaleAfterDays:           3,
		RecentlyMergedDays:       14,
	}))
	h := e2e.Start(t, opts...)
	seedAndPoll(t, h)
	h.Browser.GotoSettings("/settings/rules")
	h.Browser.WaitVisible(".profile-card")
	h.Browser.Screenshot("settings-review-rules")
}
