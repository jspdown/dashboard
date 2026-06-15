// Package config loads the dashboard's runtime configuration from a
// single YAML file.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RepoConfig binds a repo slug to its polling cadence.
type RepoConfig struct {
	Repo     string        `yaml:"repo"`
	Interval time.Duration `yaml:"interval"`
}

// Config is the full on-disk config schema.
type Config struct {
	Repos     []RepoConfig    `yaml:"repos"`
	Review    ReviewConfig    `yaml:"review"`
	Freshness FreshnessConfig `yaml:"freshness"`
}

// ReviewConfig describes how PRs are routed through the review queue.
type ReviewConfig struct {
	// DefaultRequiredReviewers defines how much reviewers we need per PR.
	DefaultRequiredReviewers int `yaml:"defaultRequiredReviewers"`
	// IgnoreLabels defines the labels that identify PRs we want to ignore.
	IgnoreLabels []string `yaml:"ignoreLabels"`
	// ReviewerOverrides defines per-label overrides for the required-reviewer count.
	// The first matching label wins; unmatched labels fall back to DefaultRequiredReviewers.
	ReviewerOverrides []ReviewerOverride `yaml:"reviewerOverrides"`
	// BotAuthors defines who are the bots.
	BotAuthors []string `yaml:"botAuthors"`
}

// ReviewerOverride pairs a PR label with a non-default reviewer count.
type ReviewerOverride struct {
	Label     string `json:"label"     yaml:"label"`
	Reviewers int    `json:"reviewers" yaml:"reviewers"`
}

// FreshnessConfig defines the criteria of stale PRs.
type FreshnessConfig struct {
	StaleAfterDays     int `yaml:"staleAfterDays"`
	RecentlyMergedDays int `yaml:"recentlyMergedDays"`
}

// Load reads, parses, defaults, and validates the YAML config at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	var cfg Config
	if err = yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid yaml: %w", err)
	}

	cfg.applyDefaults()

	if err = cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Review.DefaultRequiredReviewers == 0 {
		c.Review.DefaultRequiredReviewers = 2
	}
	if c.Freshness.StaleAfterDays == 0 {
		c.Freshness.StaleAfterDays = 5
	}
	if c.Freshness.RecentlyMergedDays == 0 {
		c.Freshness.RecentlyMergedDays = 7
	}
}

func (c *Config) validate() error {
	if len(c.Repos) == 0 {
		return errors.New("at least one repo is required")
	}

	seen := make(map[string]struct{}, len(c.Repos))
	for i, r := range c.Repos {
		owner, name, ok := strings.Cut(r.Repo, "/")
		if !ok || owner == "" || name == "" {
			return fmt.Errorf("repos[%d]: %q is not in owner/name form", i, r.Repo)
		}
		if r.Interval <= 0 {
			return fmt.Errorf("repos[%d] (%s): interval must be > 0", i, r.Repo)
		}
		if _, dup := seen[r.Repo]; dup {
			return fmt.Errorf("repos: %s appears more than once", r.Repo)
		}
		seen[r.Repo] = struct{}{}
	}

	if c.Review.DefaultRequiredReviewers < 0 {
		return errors.New("review.defaultRequiredReviewers must be >= 0")
	}
	for i, o := range c.Review.ReviewerOverrides {
		if strings.TrimSpace(o.Label) == "" {
			return fmt.Errorf("review.reviewerOverrides[%d]: label is required", i)
		}
		if o.Reviewers < 0 {
			return fmt.Errorf("review.reviewerOverrides[%d] (%s): reviewers must be >= 0", i, o.Label)
		}
	}

	if c.Freshness.StaleAfterDays <= 0 {
		return errors.New("freshness.staleAfterDays must be > 0")
	}
	if c.Freshness.RecentlyMergedDays <= 0 {
		return errors.New("freshness.recentlyMergedDays must be > 0")
	}
	return nil
}
