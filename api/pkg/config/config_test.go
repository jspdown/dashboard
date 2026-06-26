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
	t.Run("poll interval round-trips", func(t *testing.T) {
		path := writeFile(t, `
poll:
  interval: 2m
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, 2*time.Minute, cfg.Poll.Interval)
	})

	t.Run("default fills omitted interval", func(t *testing.T) {
		path := writeFile(t, `{}`)
		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, time.Minute, cfg.Poll.Interval)
	})

	t.Run("non-positive interval is rejected", func(t *testing.T) {
		path := writeFile(t, `
poll:
  interval: -1s
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "poll.interval must be at least 1m")
	})

	t.Run("sub-minute interval is rejected", func(t *testing.T) {
		path := writeFile(t, `
poll:
  interval: 30s
`)
		_, err := Load(path)
		assert.ErrorContains(t, err, "poll.interval must be at least 1m")
	})

	t.Run("malformed yaml is rejected", func(t *testing.T) {
		path := writeFile(t, "poll: [unclosed")
		_, err := Load(path)
		assert.ErrorContains(t, err, "invalid yaml")
	})

	t.Run("missing file is rejected", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
		assert.ErrorContains(t, err, "reading config file")
	})
}
