package pullrequest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveProfile(t *testing.T) {
	catchAll := RuleProfile{
		ID:       1,
		Name:     "Everything",
		AllRepos: true,
		ReviewSettings: ReviewSettings{
			DefaultRequiredReviewers: 2,
			StaleAfterDays:           5,
			RecentlyMergedDays:       7,
			IgnoreLabels:             []string{"wip"},
		},
	}
	specific := RuleProfile{
		ID:    2,
		Name:  "Strict",
		Repos: []string{"acme/widget"},
		ReviewSettings: ReviewSettings{
			DefaultRequiredReviewers: 4,
			StaleAfterDays:           3,
			RecentlyMergedDays:       14,
			IgnoreLabels:             []string{"flaky"},
		},
	}
	profiles := []RuleProfile{catchAll, specific}

	t.Run("specific profile wins for a repo it lists", func(t *testing.T) {
		got := ResolveProfile(profiles, "acme/widget")
		assert.Equal(t, 4, got.DefaultRequiredReviewers)
		assert.Equal(t, 3, got.StaleAfterDays)
		assert.Equal(t, []string{"flaky"}, got.IgnoreLabels)
	})

	t.Run("catch-all applies to a repo no specific profile lists", func(t *testing.T) {
		got := ResolveProfile(profiles, "acme/other")
		assert.Equal(t, 2, got.DefaultRequiredReviewers)
		assert.Equal(t, []string{"wip"}, got.IgnoreLabels)
	})

	t.Run("built-in defaults apply with no catch-all", func(t *testing.T) {
		got := ResolveProfile([]RuleProfile{specific}, "acme/other")
		assert.Equal(t, DefaultRequiredReviewers, got.DefaultRequiredReviewers)
		assert.Equal(t, DefaultStaleAfterDays, got.StaleAfterDays)
		assert.Equal(t, DefaultRecentlyMergedDays, got.RecentlyMergedDays)
		assert.Empty(t, got.IgnoreLabels)
	})

	t.Run("no profiles at all yields defaults", func(t *testing.T) {
		got := ResolveProfile(nil, "acme/widget")
		assert.Equal(t, DefaultReviewSettings(), got)
	})

	t.Run("specific profile wins even over a catch-all that lists later", func(t *testing.T) {
		// Order shouldn't matter: a specific match beats the catch-all.
		got := ResolveProfile([]RuleProfile{specific, catchAll}, "acme/widget")
		assert.Equal(t, 4, got.DefaultRequiredReviewers)
	})
}

func TestMaxRecentlyMergedWindow(t *testing.T) {
	t.Run("floors at the built-in default", func(t *testing.T) {
		got := MaxRecentlyMergedWindow([]RuleProfile{
			{ReviewSettings: ReviewSettings{RecentlyMergedDays: 3}},
		})
		assert.Equal(t, DefaultRecentlyMergedDays, got)
	})

	t.Run("returns the widest window", func(t *testing.T) {
		got := MaxRecentlyMergedWindow([]RuleProfile{
			{ReviewSettings: ReviewSettings{RecentlyMergedDays: 14}},
			{ReviewSettings: ReviewSettings{RecentlyMergedDays: 30}},
		})
		assert.Equal(t, 30, got)
	})

	t.Run("no profiles yields the default", func(t *testing.T) {
		assert.Equal(t, DefaultRecentlyMergedDays, MaxRecentlyMergedWindow(nil))
	})
}
