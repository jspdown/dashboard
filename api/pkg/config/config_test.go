package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoad(t *testing.T) {
	t.Run("full config round-trips", func(t *testing.T) {
		path := writeFile(t, `
repos:
  - repo: acme/widget
    interval: 2m
  - repo: acme/gadget
    interval: 10m
review:
  defaultRequiredReviewers: 2
  ignoreLabels:
    - area/webui
  reviewerOverrides:
    - label: bot/light-review
      reviewers: 1
  botAuthors:
    - renovate-with-github-actions[bot]
freshness:
  staleAfterDays: 5
  recentlyMergedDays: 7
`)
		cfg, err := Load(path)
		require.NoError(t, err)

		require.Len(t, cfg.Repos, 2)
		assert.Equal(t, "acme/widget", cfg.Repos[0].Repo)
		assert.Equal(t, 2*time.Minute, cfg.Repos[0].Interval)
		assert.Equal(t, 2, cfg.Review.DefaultRequiredReviewers)
		assert.Equal(t, []string{"area/webui"}, cfg.Review.IgnoreLabels)
		assert.Equal(t, []ReviewerOverride{{Label: "bot/light-review", Reviewers: 1}}, cfg.Review.ReviewerOverrides)
		assert.Equal(t, []string{"renovate-with-github-actions[bot]"}, cfg.Review.BotAuthors)
		assert.Equal(t, 5, cfg.Freshness.StaleAfterDays)
		assert.Equal(t, 7, cfg.Freshness.RecentlyMergedDays)
	})

	t.Run("defaults fill omitted fields", func(t *testing.T) {
		path := writeFile(t, `
repos:
  - { repo: a/b, interval: 1m }
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Review.DefaultRequiredReviewers)
		assert.Equal(t, 5, cfg.Freshness.StaleAfterDays)
		assert.Equal(t, 7, cfg.Freshness.RecentlyMergedDays)
	})

	t.Run("empty repos list is rejected", func(t *testing.T) {
		path := writeFile(t, `repos: []
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "at least one repo is required")
	})

	t.Run("malformed repo slug is rejected", func(t *testing.T) {
		path := writeFile(t, `
repos:
  - { repo: nopath, interval: 1m }
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "owner/name form")
	})

	t.Run("non-positive interval is rejected", func(t *testing.T) {
		path := writeFile(t, `
repos:
  - { repo: a/b, interval: 0s }
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "interval must be > 0")
	})

	t.Run("duplicate repo is rejected", func(t *testing.T) {
		path := writeFile(t, `
repos:
  - { repo: a/b, interval: 1m }
  - { repo: a/b, interval: 5m }
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "appears more than once")
	})

	t.Run("override without label is rejected", func(t *testing.T) {
		path := writeFile(t, `
repos:
  - { repo: a/b, interval: 1m }
review:
  reviewerOverrides:
    - { reviewers: 1 }
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "label is required")
	})

	t.Run("negative reviewers is rejected", func(t *testing.T) {
		path := writeFile(t, `
repos:
  - { repo: a/b, interval: 1m }
review:
  reviewerOverrides:
    - { label: x, reviewers: -1 }
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "reviewers must be >= 0")
	})

	t.Run("malformed yaml is rejected", func(t *testing.T) {
		path := writeFile(t, "repos: [unclosed")
		_, err := Load(path)
		assert.ErrorContains(t, err, "invalid yaml")
	})

	t.Run("missing file is rejected", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
		assert.ErrorContains(t, err, "reading config file")
	})
}
