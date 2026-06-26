// Package config loads the dashboard's server-level runtime configuration from
// a single YAML file. Per-user workflow settings (repos, review rules) live in
// the database, not here.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full on-disk config schema.
type Config struct {
	Poll PollConfig `yaml:"poll"`
}

// PollConfig holds the server-level polling settings shared by every repo.
type PollConfig struct {
	// Interval is the default per-repo poll cadence. Every observed repo is
	// polled on its own ticker at this interval, so a slow repo can't block a
	// fast one.
	Interval time.Duration `yaml:"interval"`
}

// The types below are a shared vocabulary for a repo subscription or review
// policy, used by the e2e harness to seed a viewer's state and by the poller for
// repo verification. They are not part of the on-disk Config.

// RepoConfig binds a repo slug to its polling cadence.
type RepoConfig struct {
	Repo     string        `yaml:"repo"`
	Interval time.Duration `yaml:"interval"`
}

// ReviewConfig describes how PRs are routed through a user's review queue.
type ReviewConfig struct {
	// DefaultRequiredReviewers defines how many reviewers a PR needs.
	DefaultRequiredReviewers int `yaml:"defaultRequiredReviewers"`
	// IgnoreLabels defines the labels that identify PRs we want to ignore.
	IgnoreLabels []string `yaml:"ignoreLabels"`
	// ReviewerOverrides defines per-label overrides for the required-reviewer
	// count. The first matching label wins; unmatched labels fall back to
	// DefaultRequiredReviewers.
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

// defaultPollInterval is the per-repo poll cadence used when the config omits it.
const defaultPollInterval = time.Minute

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
	if c.Poll.Interval == 0 {
		c.Poll.Interval = defaultPollInterval
	}
}

func (c *Config) validate() error {
	if c.Poll.Interval <= 0 {
		return errors.New("poll.interval must be > 0")
	}
	return nil
}
