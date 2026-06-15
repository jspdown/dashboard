package pullrequest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryService_List(t *testing.T) {
	tests := []struct {
		name      string
		opts      ListOpts
		wantCount int
		wantIDs   []int
	}{
		{
			name:      "default returns all in fixture order",
			opts:      ListOpts{},
			wantCount: 13,
			wantIDs:   []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13},
		},
		{
			name:      "filter all is equivalent to default",
			opts:      ListOpts{Filter: "all"},
			wantCount: 13,
		},
		{
			name:      "filter needs review",
			opts:      ListOpts{Filter: "needs review"},
			wantCount: 4,
			wantIDs:   []int{1, 2, 3, 4},
		},
		{
			// Mock service uses stale_after_days=5, so chip and filter both read
			// "stale > 5d". Of ages [1,2,0,4,4,8,6,1,0,3,2,1,2], only 8 and 6
			// strictly exceed 5.
			name:      "filter stale > 5d",
			opts:      ListOpts{Filter: "stale > 5d"},
			wantCount: 2,
			wantIDs:   []int{6, 7},
		},
		{
			// The threshold is config-driven, so a string that doesn't match
			// the configured "stale > Nd" hits matchFilter's catch-all and
			// returns everything.
			name:      "filter stale with mismatched threshold falls back to all",
			opts:      ListOpts{Filter: "stale > 3d"},
			wantCount: 13,
		},
		{
			name:      "filter ci failing",
			opts:      ListOpts{Filter: "ci failing"},
			wantCount: 2,
			wantIDs:   []int{4, 5},
		},
		{
			name:      "unknown filter falls back to all",
			opts:      ListOpts{Filter: "nonsense"},
			wantCount: 13,
		},
	}

	svc := newMemoryService()
	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.List(ctx, tc.opts)
			require.NoError(t, err)
			require.Len(t, got, tc.wantCount)
			if tc.wantIDs != nil {
				ids := make([]int, len(got))
				for i, pr := range got {
					ids[i] = pr.ID
				}
				assert.Equal(t, tc.wantIDs, ids)
			}
		})
	}
}

func TestMemoryService_List_Sort(t *testing.T) {
	svc := newMemoryService()
	ctx := context.Background()

	t.Run("sort by age desc", func(t *testing.T) {
		got, err := svc.List(ctx, ListOpts{Sort: "age"})
		require.NoError(t, err)
		require.Len(t, got, 13)
		for i := 1; i < len(got); i++ {
			assert.GreaterOrEqual(t, got[i-1].Age, got[i].Age,
				"ages must be non-increasing: %d at %d, %d at %d", got[i-1].Age, i-1, got[i].Age, i)
		}
	})

	t.Run("sort by repo asc", func(t *testing.T) {
		got, err := svc.List(ctx, ListOpts{Sort: "repo"})
		require.NoError(t, err)
		require.Len(t, got, 13)
		for i := 1; i < len(got); i++ {
			assert.LessOrEqual(t, got[i-1].Repo, got[i].Repo)
		}
	})

	t.Run("sort by author asc", func(t *testing.T) {
		got, err := svc.List(ctx, ListOpts{Sort: "author"})
		require.NoError(t, err)
		require.Len(t, got, 13)
		for i := 1; i < len(got); i++ {
			assert.LessOrEqual(t, got[i-1].Author, got[i].Author)
		}
	})

	t.Run("sort priority preserves fixture order", func(t *testing.T) {
		got, err := svc.List(ctx, ListOpts{Sort: "priority"})
		require.NoError(t, err)
		require.Len(t, got, 13)
		for i, pr := range got {
			assert.Equal(t, i+1, pr.ID, "fixture order is by ID 1..13")
		}
	})
}

func TestMemoryService_List_DoesNotMutateFixtures(t *testing.T) {
	svc := newMemoryService()
	ctx := context.Background()

	got, err := svc.List(ctx, ListOpts{Sort: "age"})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	got[0].Title = "mutated"

	// Re-fetch and ensure the fixture / internal slice was not touched.
	again, err := svc.List(ctx, ListOpts{})
	require.NoError(t, err)
	for _, pr := range again {
		assert.NotEqual(t, "mutated", pr.Title)
	}
}
