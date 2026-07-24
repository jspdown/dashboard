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
		e2e.WithProfile(e2e.Profile{
			Name:                     "All repos",
			AllRepos:                 true,
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
		e2e.WithProfile(e2e.Profile{
			Name:                     "All repos",
			AllRepos:                 true,
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
		e2e.WithProfile(e2e.Profile{
			Name:                     "All repos",
			AllRepos:                 true,
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

// TestSpecificProfileOverridesCatchAll exercises profile resolution end-to-end:
// an all-repositories profile needs one reviewer, but a specific profile for one
// repo needs two. A same-shaped PR (one approval) clears the review queue in the
// catch-all repo yet stays in it for the repo the specific profile claims.
func TestSpecificProfileOverridesCatchAll(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("alice"),
		e2e.WithRepo("acme/strict"),
		e2e.WithRepo("acme/relaxed"),
		e2e.WithProfile(e2e.Profile{Name: "Baseline", AllRepos: true, DefaultRequiredReviewers: 1}),
		e2e.WithProfile(e2e.Profile{Name: "Strict", Repos: []string{"acme/strict"}, DefaultRequiredReviewers: 2}),
	)

	// One approval from carol on a same-shaped PR in each repo.
	h.Fake.Repo("acme/strict").
		PR(1).Title("Tighten auth").Author("bob").Open().
		AddReview("carol", "approved", -10*time.Minute)
	h.Fake.Repo("acme/relaxed").
		PR(2).Title("Tweak copy").Author("bob").Open().
		AddReview("carol", "approved", -10*time.Minute)

	h.Poll("acme/strict")
	h.Poll("acme/relaxed")
	h.Reload()

	// Under the catch-all (1) the single approval clears review; the specific
	// profile on acme/strict needs 2, so that PR stays put.
	assert.Equal(t, []int{1}, h.Browser.PRsInGroup("review"),
		"specific profile (needs 2, has 1 approval) must keep only its repo's PR in review")
}

// TestRepositoriesScreenReflectsPollHealth exercises the per-user RepoOverview
// path end-to-end: after a poll, the Repositories settings screen must show the
// observed repo as healthy, the health PostgresService.RepoOverview computes.
func TestRepositoriesScreenReflectsPollHealth(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("alice"),
		e2e.WithRepo("acme/widget"),
	)

	h.Fake.Repo("acme/widget").
		PR(11).Title("Needs review").Author("bob").Open()

	h.Poll("acme/widget")

	h.Browser.GotoSettings("/settings/repos")
	rows := h.Browser.SettingsRepoRows()

	require.Contains(t, rows, "acme/widget", "observed repo must render a row")
	assert.Equal(t, "polling", rows["acme/widget"].Health, "a polled repo with no error reads as healthy")
}

// TestProfileStaleWindowFlagsRow exercises the per-profile freshness path: a PR
// older than its profile's stale window is flagged stale server-side, the badge
// reaches its row, and the search placeholder reflects the configured viewer.
func TestProfileStaleWindowFlagsRow(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t,
		e2e.WithViewer("priya"),
		e2e.WithRepo("acme/widget"),
		e2e.WithProfile(e2e.Profile{
			Name:                     "All repos",
			AllRepos:                 true,
			DefaultRequiredReviewers: 2,
			StaleAfterDays:           10,
			RecentlyMergedDays:       14,
		}),
	)

	h.Fake.Repo("acme/widget").
		PR(1).Title("Old PR").Author("bob").Open().Age(20 * 24 * time.Hour)

	h.Poll("acme/widget")
	h.Reload()

	assert.Contains(t, h.Browser.StalePRs(), 1,
		"a PR older than the profile's 10d stale window must show a stale badge")

	chips := h.Browser.FilterChips()
	require.Contains(t, chips, "stale", "the stale filter chip must render")

	assert.Equal(t,
		"filter… repo:auth ci:failing author:priya",
		h.Browser.SearchPlaceholder(),
		"placeholder must reflect configured viewer login")
}
