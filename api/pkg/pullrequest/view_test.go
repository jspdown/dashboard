package pullrequest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChecksLabel(t *testing.T) {
	assert.Equal(t, "—", ChecksLabel(0, 0))
	assert.Equal(t, "3/5", ChecksLabel(3, 5))
	assert.Equal(t, "0/2", ChecksLabel(0, 2))
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		t    time.Time
		want int
	}{
		{"same instant", now, 0},
		{"one day ago", now.Add(-24 * time.Hour), 1},
		{"three days ago", now.Add(-3 * 24 * time.Hour), 3},
		{"future clamps to zero", now.Add(2 * time.Hour), 0},
		{"23h59m ago is still zero days", now.Add(-23*time.Hour - 59*time.Minute), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Age(now, tc.t))
		})
	}
}

func TestViewerVerdict(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		reviews []Review
		want    string
	}{
		{
			name: "later commented does not shadow earlier approved",
			reviews: []Review{
				{Reviewer: "alex", Verdict: VerdictApproved, SubmittedAt: t1},
				{Reviewer: "alex", Verdict: VerdictCommented, SubmittedAt: t3},
			},
			want: VerdictApproved,
		},
		{
			name: "later commented does not shadow earlier changes_requested",
			reviews: []Review{
				{Reviewer: "alex", Verdict: VerdictChangesRequested, SubmittedAt: t1},
				{Reviewer: "alex", Verdict: VerdictCommented, SubmittedAt: t3},
			},
			want: VerdictChangesRequested,
		},
		{
			name: "later approved overwrites earlier changes_requested",
			reviews: []Review{
				{Reviewer: "alex", Verdict: VerdictChangesRequested, SubmittedAt: t1},
				{Reviewer: "alex", Verdict: VerdictCommented, SubmittedAt: t2},
				{Reviewer: "alex", Verdict: VerdictApproved, SubmittedAt: t3},
			},
			want: VerdictApproved,
		},
		{
			name: "dismissed wins when newest non-comment",
			reviews: []Review{
				{Reviewer: "alex", Verdict: VerdictApproved, SubmittedAt: t1},
				{Reviewer: "alex", Verdict: VerdictDismissed, SubmittedAt: t2},
				{Reviewer: "alex", Verdict: VerdictCommented, SubmittedAt: t3},
			},
			want: VerdictDismissed,
		},
		{
			name: "only commented yields empty verdict",
			reviews: []Review{
				{Reviewer: "alex", Verdict: VerdictCommented, SubmittedAt: t1},
				{Reviewer: "alex", Verdict: VerdictCommented, SubmittedAt: t2},
			},
			want: "",
		},
		{
			name: "ignores other reviewers",
			reviews: []Review{
				{Reviewer: "bob", Verdict: VerdictApproved, SubmittedAt: t3},
				{Reviewer: "alex", Verdict: VerdictChangesRequested, SubmittedAt: t1},
			},
			want: VerdictChangesRequested,
		},
		{
			name:    "no reviews yields empty",
			reviews: nil,
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, viewerVerdict(tc.reviews, "alex"))
		})
	}
}

func TestHumanizeMerged(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"today", now.Add(-2 * time.Hour), "today"},
		{"yesterday", now.Add(-24 * time.Hour), "yesterday"},
		{"3 days ago", now.Add(-3 * 24 * time.Hour), "3d ago"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, HumanizeMerged(now, tc.t))
		})
	}
}
