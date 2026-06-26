package config

import (
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

// defaultPollInterval is the per-repo poll cadence used when the config omits it.
const defaultPollInterval = time.Minute

// minPollInterval is the lowest poll cadence we accept, to keep the poller from
// burning through GitHub's rate limit.
const minPollInterval = time.Minute

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
	if c.Poll.Interval < minPollInterval {
		return fmt.Errorf("poll.interval must be at least %s", minPollInterval)
	}
	return nil
}
