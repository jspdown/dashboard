package screenshots_test

import (
	"testing"

	"github.com/jspdown/dashboard/e2e/internal/e2e"
)

// TestScreenshot_SettingsRepos captures the per-user Repositories settings
// screen: the add-repo bar, the observed-repo list with health and PR counts,
// and the team suggestions. Reuses the demo scenario so the rows are populated.
func TestScreenshot_SettingsRepos(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t, demoOpts()...)
	seedAndPoll(t, h)
	h.Browser.GotoSettings("/settings/repos")
	h.Browser.Screenshot("settings-repositories")
}

// TestScreenshot_SettingsRules captures the per-user Review rules screen:
// required reviewers with per-label overrides, ignore labels, bot authors, and
// the freshness windows, all seeded from the demo review policy.
func TestScreenshot_SettingsRules(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t, demoOpts()...)
	seedAndPoll(t, h)
	h.Browser.GotoSettings("/settings/rules")
	h.Browser.Screenshot("settings-review-rules")
}
