package e2e_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/e2e/internal/e2e"
)

// Each test calls t.Parallel(); the harness gives every test its own
// fake GitHub server, fresh Postgres database, and browser tab, so
// there's no shared mutable state between them.

// TestIgnoreLabelHidesPRFromReviewQueue exercises the ignore_labels
// path: a PR carrying area/webui must not appear in "Needs my review",
// and removing the label re-surfaces it on the next poll.
func TestIgnoreLabelHidesPRFromReviewQueue(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("alice"),
		e2e.WithRepo("acme/widget"),
		e2e.WithReview(e2e.ReviewConfig{
			DefaultRequiredReviewers: 2,
			IgnoreLabels:             []string{"area/webui"},
		}),
	)

	h.Fake.Repo("acme/widget").
		PR(42).
		Title("Migrate auth to OIDC").
		Author("bob").
		Open().
		Labels("area/webui")

	h.Poll("acme/widget")
	h.Reload()

	assert.Empty(t, h.Browser.PRsInGroup("review"),
		"PR with ignore_labels label must not enter the review queue")
	assert.Equal(t, 0, h.Browser.GroupCount("review"))

	// Drop the label, re-poll, and confirm the PR appears.
	h.Fake.PR("acme/widget", 42).RemoveLabel("area/webui")
	h.Poll("acme/widget")
	h.Reload()

	assert.Equal(t, []int{42}, h.Browser.PRsInGroup("review"),
		"removing the ignore label must surface the PR on the next poll")
}

// TestReviewerOverrideBypassesDefaultCount exercises the
// reviewer_overrides path: a label-matched PR needs fewer reviewers
// than the default, so a single approval moves it out of the queue.
func TestReviewerOverrideBypassesDefaultCount(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("alice"),
		e2e.WithRepo("acme/widget"),
		e2e.WithReview(e2e.ReviewConfig{
			DefaultRequiredReviewers: 2,
			ReviewerOverrides: []e2e.ReviewerOverride{
				{Label: "bot/light-review", Reviewers: 1},
			},
		}),
	)

	h.Fake.Repo("acme/widget").
		PR(7).
		Title("Bump dependency").
		Author("renovate-bot").
		Open().
		Labels("bot/light-review").
		AddReview("carol", "approved", -10*time.Minute)

	h.Poll("acme/widget")
	h.Reload()

	// One approval is enough under the override, so the PR exits
	// the review queue.
	assert.Empty(t, h.Browser.PRsInGroup("review"),
		"override-met PR (1 reviewer needed, 1 approved) must not show in review queue")

	// Tooltip on the review group surfaces the override clause from
	// the config so adopters can see what's enforced.
	tip := h.Browser.GroupTooltip("review")
	require.NotEmpty(t, tip, "review group tooltip must be config-driven")
	assert.Contains(t, tip, "bot/light-review",
		"tooltip should name the configured override label")
}

// TestBotAuthorRoutesToRenovateGroup exercises the bot_authors path:
// PRs from a configured bot land in the dedicated "Renovate" group
// instead of crowding the review queue, which is the whole reason
// bot_authors exists.
func TestBotAuthorRoutesToRenovateGroup(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("alice"),
		e2e.WithRepo("acme/widget"),
		e2e.WithReview(e2e.ReviewConfig{
			DefaultRequiredReviewers: 2,
			BotAuthors:               []string{"renovate-bot[bot]"},
		}),
	)

	h.Fake.Repo("acme/widget").
		PR(99).Title("chore(deps): bump go-github").Author("renovate-bot[bot]").Open()
	h.Fake.Repo("acme/widget").
		PR(100).Title("Add login").Author("carol").Open()

	h.Poll("acme/widget")
	h.Reload()

	assert.Equal(t, []int{99}, h.Browser.PRsInGroup("renovate"),
		"PR authored by a configured bot must land in the renovate group")
	assert.Equal(t, []int{100}, h.Browser.PRsInGroup("review"),
		"PRs from regular authors keep going to the review queue")
}

// TestRepositoriesScreenReflectsPollHealthAndCounts exercises the per-user
// RepoOverview path end-to-end: after a poll, the Repositories settings screen
// must show the observed repo as healthy with the viewer's open and
// needs-review counts, the numbers PostgresService.RepoOverview computes.
func TestRepositoriesScreenReflectsPollHealthAndCounts(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("alice"),
		e2e.WithRepo("acme/widget"),
		e2e.WithReview(e2e.ReviewConfig{DefaultRequiredReviewers: 2}),
	)

	// One open PR needing alice's review, one open PR she authored.
	h.Fake.Repo("acme/widget").
		PR(11).Title("Needs review").Author("bob").Open()
	h.Fake.Repo("acme/widget").
		PR(12).Title("Mine").Author("alice").Open()

	h.Poll("acme/widget")

	h.Browser.GotoSettings("/settings/repos")
	rows := h.Browser.SettingsRepoRows()

	require.Contains(t, rows, "acme/widget", "observed repo must render a row")
	row := rows["acme/widget"]
	assert.Equal(t, "polling", row.Health, "a polled repo with no error reads as healthy")
	assert.Contains(t, row.Stats, "2 open", "both open PRs are counted")
	assert.Contains(t, row.Stats, "1 need you", "the un-reviewed PR by another author needs the viewer")
}

// TestStaleAfterDaysFlowsThroughChip exercises the freshness path: the
// stale-filter chip text must reflect the configured stale_after_days,
// and the search placeholder must reflect the configured viewer login.
// Both are surfaces a fresh adopter notices first.
func TestStaleAfterDaysFlowsThroughChip(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("priya"),
		e2e.WithRepo("acme/widget"),
		e2e.WithFreshness(e2e.FreshnessConfig{
			StaleAfterDays:     10,
			RecentlyMergedDays: 14,
		}),
	)

	h.Fake.Repo("acme/widget").
		PR(1).Title("Old PR").Author("bob").Open().Age(20 * 24 * time.Hour)

	h.Poll("acme/widget")
	h.Reload()

	chips := h.Browser.FilterChips()
	require.NotEmpty(t, chips, "chip row must render")
	assert.Contains(t, chips, "stale > 10d",
		"chip text must reflect configured stale_after_days, got chips=%v", chips)

	assert.Equal(t,
		"filter… repo:auth ci:failing author:priya",
		h.Browser.SearchPlaceholder(),
		"placeholder must reflect configured viewer login")

	// The "Recently merged" tooltip must also reflect the configured
	// window so adopters with a different freshness see the right text.
	mergedTip := h.Browser.GroupTooltip("merged")
	assert.Contains(t, mergedTip, "14 days",
		"merged-group tooltip must reflect recently_merged_days")
}
