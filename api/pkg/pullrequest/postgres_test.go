package pullrequest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRepoHealth(t *testing.T) {
	polled := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	synced := time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)
	boom := "boom"

	tests := []struct {
		name string
		st   RepoStatus
		want string
	}{
		{
			name: "never polled is checking",
			st:   RepoStatus{LastSyncedAt: &synced},
			want: RepoHealthChecking,
		},
		{
			name: "last poll errored is error",
			st:   RepoStatus{LastPolledAt: &polled, LastError: &boom},
			want: RepoHealthError,
		},
		{
			name: "polled without error is ok",
			st:   RepoStatus{LastPolledAt: &polled, LastSyncedAt: &synced},
			want: RepoHealthOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, repoHealth(tt.st))
		})
	}
}

func TestRepoSyncedAt(t *testing.T) {
	polled := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	synced := time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)

	t.Run("prefers last poll attempt over cursor advance", func(t *testing.T) {
		got := repoSyncedAt(RepoStatus{LastPolledAt: &polled, LastSyncedAt: &synced})
		assert.Equal(t, &polled, got)
	})

	t.Run("falls back to cursor advance when never polled", func(t *testing.T) {
		got := repoSyncedAt(RepoStatus{LastSyncedAt: &synced})
		assert.Equal(t, &synced, got)
	})

	t.Run("nil when neither is set", func(t *testing.T) {
		assert.Nil(t, repoSyncedAt(RepoStatus{}))
	})
}
