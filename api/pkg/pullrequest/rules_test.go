package pullrequest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// testRules matches the original Traefik review policy: 2 reviewers by default,
// area/webui ignored, bot/light-review needs 1, renovate-with-github-actions[bot]
// the only routed bot author. Used by every rules_test case.
func testRules() *Rules {
	return NewRules(UserSettings{
		DefaultRequiredReviewers: 2,
		IgnoreLabels:             []string{"area/webui"},
		ReviewerOverrides: []ReviewerOverride{
			{Label: "bot/light-review", Reviewers: 1},
		},
		BotAuthors: []string{"renovate-with-github-actions[bot]"},
	})
}

func TestRequiredReviewers(t *testing.T) {
	tests := []struct {
		name        string
		labels      []string
		wantCount   int
		wantIgnored bool
	}{
		{"no labels", nil, 2, false},
		{"unrelated label", []string{"kind/bug"}, 2, false},
		{"area/webui", []string{"area/webui"}, 0, true},
		{"bot/light-review", []string{"bot/light-review"}, 1, false},
		{"both labels — area/webui wins", []string{"bot/light-review", "area/webui"}, 0, true},
		{"both labels — order independent", []string{"area/webui", "bot/light-review"}, 0, true},
	}
	r := testRules()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCount, gotIgnored := r.RequiredReviewers(tc.labels)
			assert.Equal(t, tc.wantCount, gotCount)
			assert.Equal(t, tc.wantIgnored, gotIgnored)
		})
	}
}

func TestRequiredReviewers_emptyConfig(t *testing.T) {
	// Bare settings: no overrides, no ignore labels, default count is the
	// zero value. Sanity check that rules don't read state outside the settings.
	r := NewRules(UserSettings{})
	count, ignored := r.RequiredReviewers([]string{"area/webui"})
	assert.Equal(t, 0, count)
	assert.False(t, ignored)
}

func TestRequiredReviewers_multipleOverrides(t *testing.T) {
	r := NewRules(UserSettings{
		DefaultRequiredReviewers: 3,
		ReviewerOverrides: []ReviewerOverride{
			{Label: "tiny", Reviewers: 1},
			{Label: "small", Reviewers: 2},
		},
	})
	t.Run("first matching override wins", func(t *testing.T) {
		count, _ := r.RequiredReviewers([]string{"small", "tiny"})
		assert.Equal(t, 1, count)
	})
	t.Run("non-matching label falls back to default", func(t *testing.T) {
		count, _ := r.RequiredReviewers([]string{"unrelated"})
		assert.Equal(t, 3, count)
	})
}

func TestLatestReviewsByReviewer(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)

	t.Run("newer review wins", func(t *testing.T) {
		got := LatestReviewsByReviewer([]Review{
			{Reviewer: "alex", Verdict: "commented", SubmittedAt: t1},
			{Reviewer: "alex", Verdict: "approved", SubmittedAt: t3},
			{Reviewer: "alex", Verdict: "changes_requested", SubmittedAt: t2},
		})
		assert.Len(t, got, 1)
		assert.Equal(t, "approved", got["alex"].Verdict)
	})

	t.Run("distinct reviewers", func(t *testing.T) {
		got := LatestReviewsByReviewer([]Review{
			{Reviewer: "alex", Verdict: "approved", SubmittedAt: t1},
			{Reviewer: "carol", Verdict: "commented", SubmittedAt: t2},
		})
		assert.Len(t, got, 2)
		assert.Equal(t, "approved", got["alex"].Verdict)
		assert.Equal(t, "commented", got["carol"].Verdict)
	})

	t.Run("tie keeps first-seen", func(t *testing.T) {
		got := LatestReviewsByReviewer([]Review{
			{Reviewer: "alex", Verdict: "commented", SubmittedAt: t1},
			{Reviewer: "alex", Verdict: "approved", SubmittedAt: t1},
		})
		assert.Equal(t, "commented", got["alex"].Verdict)
	})
}

func TestRollupCI(t *testing.T) {
	failure := "failure"
	success := "success"

	tests := []struct {
		name       string
		checks     []CheckRun
		wantStatus string
		wantDone   int
		wantTotal  int
	}{
		{
			name:       "no checks",
			checks:     nil,
			wantStatus: CINone,
			wantDone:   0,
			wantTotal:  0,
		},
		{
			name: "all passing",
			checks: []CheckRun{
				{RunStatus: "completed", Conclusion: &success},
				{RunStatus: "completed", Conclusion: &success},
			},
			wantStatus: CIPassing,
			wantDone:   2,
			wantTotal:  2,
		},
		{
			name: "any failure wins over pending",
			checks: []CheckRun{
				{RunStatus: "completed", Conclusion: &failure},
				{RunStatus: "in_progress"},
			},
			wantStatus: CIFailing,
			wantDone:   1,
			wantTotal:  2,
		},
		{
			name: "pending without failure",
			checks: []CheckRun{
				{RunStatus: "completed", Conclusion: &success},
				{RunStatus: "in_progress"},
			},
			wantStatus: CIPending,
			wantDone:   1,
			wantTotal:  2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotDone, gotTotal := RollupCI(tc.checks)
			assert.Equal(t, tc.wantStatus, gotStatus)
			assert.Equal(t, tc.wantDone, gotDone)
			assert.Equal(t, tc.wantTotal, gotTotal)
		})
	}
}

func TestBlockingUsers(t *testing.T) {
	t.Run("union of requests and changes_requested reviewers, sorted", func(t *testing.T) {
		latest := map[string]Review{
			"carol": {Reviewer: "carol", Verdict: "changes_requested"},
			"dana":  {Reviewer: "dana", Verdict: "approved"},
			"erin":  {Reviewer: "erin", Verdict: "changes_requested"},
		}
		got := BlockingUsers([]string{"bob", "carol"}, latest)
		assert.Equal(t, []string{"bob", "carol", "erin"}, got)
	})

	t.Run("ignores non-changes_requested verdicts", func(t *testing.T) {
		latest := map[string]Review{
			"alex": {Reviewer: "alex", Verdict: "approved"},
			"sam":  {Reviewer: "sam", Verdict: "commented"},
		}
		got := BlockingUsers(nil, latest)
		assert.Empty(t, got)
	})

	t.Run("empty inputs", func(t *testing.T) {
		got := BlockingUsers(nil, nil)
		assert.Empty(t, got)
	})
}

func TestApprovalCount(t *testing.T) {
	t.Run("counts only approved verdicts, deduplicated by reviewer", func(t *testing.T) {
		latest := map[string]Review{
			"alex":  {Reviewer: "alex", Verdict: VerdictApproved},
			"bob":   {Reviewer: "bob", Verdict: VerdictApproved},
			"carol": {Reviewer: "carol", Verdict: VerdictChangesRequested},
			"dana":  {Reviewer: "dana", Verdict: VerdictCommented},
		}
		assert.Equal(t, 2, ApprovalCount(latest))
	})

	t.Run("empty map is zero", func(t *testing.T) {
		assert.Equal(t, 0, ApprovalCount(nil))
	})
}

func TestReviewerCount(t *testing.T) {
	t.Run("counts approvals and changes_requested, ignores comments and dismissals", func(t *testing.T) {
		latest := map[string]Review{
			"alex":  {Reviewer: "alex", Verdict: VerdictApproved},
			"bob":   {Reviewer: "bob", Verdict: VerdictChangesRequested},
			"carol": {Reviewer: "carol", Verdict: VerdictCommented},
			"dana":  {Reviewer: "dana", Verdict: VerdictDismissed},
		}
		assert.Equal(t, 2, ReviewerCount(latest))
	})

	t.Run("empty map is zero", func(t *testing.T) {
		assert.Equal(t, 0, ReviewerCount(nil))
	})
}

func TestMergeReadiness(t *testing.T) {
	// approvedBy builds a latest-review map of n distinct approvers.
	approvedBy := func(n int) map[string]Review {
		m := make(map[string]Review, n)
		for i := range n {
			login := string(rune('a' + i))
			m[login] = Review{Reviewer: login, Verdict: VerdictApproved}
		}
		return m
	}

	tests := []struct {
		name     string
		draft    bool
		required int
		ci       string
		latest   map[string]Review
		want     string
	}{
		{
			name:  "draft wins over everything",
			draft: true, required: 2, ci: CIPassing,
			latest: approvedBy(2),
			want:   MergeDraft,
		},
		{
			name:     "changes requested blocks even with enough approvals",
			required: 2, ci: CIPassing,
			latest: map[string]Review{
				"a": {Reviewer: "a", Verdict: VerdictApproved},
				"b": {Reviewer: "b", Verdict: VerdictChangesRequested},
			},
			want: MergeBlocked,
		},
		{
			name:     "failing CI blocks",
			required: 2, ci: CIFailing,
			latest: approvedBy(2),
			want:   MergeBlocked,
		},
		{
			name:     "fewer approvals than required needs approval",
			required: 2, ci: CIPassing,
			latest: approvedBy(1),
			want:   MergeNeedsApproval,
		},
		{
			name:     "enough approvals but CI still running",
			required: 2, ci: CIPending,
			latest: approvedBy(2),
			want:   MergeCIPending,
		},
		{
			name:     "enough approvals and CI green is ready",
			required: 2, ci: CIPassing,
			latest: approvedBy(2),
			want:   MergeReady,
		},
		{
			name:     "no CI configured is ready once approved",
			required: 1, ci: CINone,
			latest: approvedBy(1),
			want:   MergeReady,
		},
		{
			name:     "zero required is ready with no reviews",
			required: 0, ci: CINone,
			latest: nil,
			want:   MergeReady,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeReadiness(tc.draft, ApprovalCount(tc.latest), tc.required, tc.ci, tc.latest)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestComputeActivity(t *testing.T) {
	viewedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	before := viewedAt.Add(-time.Hour)
	after := viewedAt.Add(time.Hour)

	baseSnap := func() PullRequestSnapshot {
		return PullRequestSnapshot{
			PullRequest: PullRequest{HeadSHA: "sha-1", Comments: 3},
			View: &ViewState{
				ViewedAt:            viewedAt,
				CommentsCountAtView: 3,
				HeadSHAAtView:       "sha-1",
			},
		}
	}

	tests := []struct {
		name string
		snap PullRequestSnapshot
		want Activity
	}{
		{
			name: "never viewed → unread, no breakdown",
			snap: PullRequestSnapshot{PullRequest: PullRequest{HeadSHA: "sha-1", Comments: 3}},
			want: Activity{Unread: true},
		},
		{
			name: "viewed and nothing changed",
			snap: baseSnap(),
			want: Activity{},
		},
		{
			name: "new commit only",
			snap: func() PullRequestSnapshot {
				s := baseSnap()
				s.HeadSHA = "sha-2"
				return s
			}(),
			want: Activity{Unread: true, NewCommits: true},
		},
		{
			name: "new comments only",
			snap: func() PullRequestSnapshot {
				s := baseSnap()
				s.Comments = 5
				return s
			}(),
			want: Activity{Unread: true, NewComments: 2},
		},
		{
			name: "deleted comments clamp to zero",
			snap: func() PullRequestSnapshot {
				s := baseSnap()
				s.Comments = 1
				return s
			}(),
			want: Activity{},
		},
		{
			name: "new reviews only — submitted after view counts, before view doesn't",
			snap: func() PullRequestSnapshot {
				s := baseSnap()
				s.Reviews = []Review{
					{Reviewer: "bob", SubmittedAt: before},
					{Reviewer: "carol", SubmittedAt: after},
					{Reviewer: "dana", SubmittedAt: after.Add(time.Minute)},
				}
				return s
			}(),
			want: Activity{Unread: true, NewReviews: 2},
		},
		{
			name: "review at exactly viewed_at does not count",
			snap: func() PullRequestSnapshot {
				s := baseSnap()
				s.Reviews = []Review{{Reviewer: "bob", SubmittedAt: viewedAt}}
				return s
			}(),
			want: Activity{},
		},
		{
			name: "mix of all three",
			snap: func() PullRequestSnapshot {
				s := baseSnap()
				s.HeadSHA = "sha-2"
				s.Comments = 4
				s.Reviews = []Review{{Reviewer: "bob", SubmittedAt: after}}
				return s
			}(),
			want: Activity{Unread: true, NewCommits: true, NewComments: 1, NewReviews: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ComputeActivity(tc.snap))
		})
	}
}

func TestClassifyGroup(t *testing.T) {
	approved := func(name string) Review {
		return Review{Reviewer: name, Verdict: "approved", SubmittedAt: time.Now()}
	}
	openPR := PullRequest{Author: "carol", Status: "open"}
	mergedPR := PullRequest{Author: "carol", Status: "merged"}
	draftPR := PullRequest{Author: "carol", Status: "open", Draft: true}
	mine := PullRequest{Author: "alex", Status: "open"}

	tests := []struct {
		name           string
		pr             PullRequest
		viewer         string
		latest         map[string]Review
		labels         []string
		reviewRequests []string
		want           string
	}{
		{
			name: "merged wins over everything",
			pr:   mergedPR,
			want: GroupMerged,
		},
		{
			name: "merged wins over draft",
			pr:   PullRequest{Author: "carol", Status: "merged", Draft: true},
			want: GroupMerged,
		},
		{
			name:   "viewer-author → mine",
			pr:     mine,
			viewer: "alex",
			want:   GroupMine,
		},
		{
			name:   "draft hides PR",
			pr:     draftPR,
			viewer: "alex",
			want:   "",
		},
		{
			name:   "area/webui hides PR",
			pr:     openPR,
			viewer: "alex",
			labels: []string{"area/webui"},
			want:   "",
		},
		{
			name:   "viewer already reviewed → reviewed",
			pr:     openPR,
			viewer: "alex",
			latest: map[string]Review{"alex": approved("alex")},
			want:   GroupReviewed,
		},
		{
			name:           "viewer reviewed but re-requested → review",
			pr:             openPR,
			viewer:         "alex",
			latest:         map[string]Review{"alex": approved("alex")},
			reviewRequests: []string{"alex"},
			want:           GroupReview,
		},
		{
			name:           "viewer only commented and still in review requests → review",
			pr:             openPR,
			viewer:         "alex",
			latest:         map[string]Review{"alex": {Reviewer: "alex", Verdict: "commented", SubmittedAt: time.Now()}},
			reviewRequests: []string{"alex"},
			want:           GroupReview,
		},
		{
			name:   "area/webui hides PR even when viewer reviewed",
			pr:     openPR,
			viewer: "alex",
			labels: []string{"area/webui"},
			latest: map[string]Review{"alex": approved("alex")},
			want:   "",
		},
		{
			name:   "default policy with 0 reviewers → review",
			pr:     openPR,
			viewer: "alex",
			want:   GroupReview,
		},
		{
			name:   "default policy with 1 reviewer → review",
			pr:     openPR,
			viewer: "alex",
			latest: map[string]Review{"bob": approved("bob")},
			want:   GroupReview,
		},
		{
			name:   "default policy met (2 reviewers) → hidden",
			pr:     openPR,
			viewer: "alex",
			latest: map[string]Review{
				"bob":   approved("bob"),
				"carol": approved("carol"),
			},
			want: "",
		},
		{
			name:   "bot/light-review with 0 → review",
			pr:     openPR,
			viewer: "alex",
			labels: []string{"bot/light-review"},
			want:   GroupReview,
		},
		{
			name:   "bot/light-review met with 1 → hidden",
			pr:     openPR,
			viewer: "alex",
			labels: []string{"bot/light-review"},
			latest: map[string]Review{"bob": approved("bob")},
			want:   "",
		},
		{
			name:   "changes_requested counts as a reviewer",
			pr:     openPR,
			viewer: "alex",
			latest: map[string]Review{
				"bob":   approved("bob"),
				"carol": {Reviewer: "carol", Verdict: "changes_requested", SubmittedAt: time.Now()},
			},
			want: "",
		},
		{
			name:   "comment-only review doesn't satisfy the reviewer requirement",
			pr:     openPR,
			viewer: "alex",
			latest: map[string]Review{
				"bob":   {Reviewer: "bob", Verdict: "commented", SubmittedAt: time.Now()},
				"carol": {Reviewer: "carol", Verdict: "changes_requested", SubmittedAt: time.Now()},
			},
			want: GroupReview,
		},
		{
			name:   "comment-only review doesn't meet a 1-reviewer policy",
			pr:     openPR,
			viewer: "alex",
			labels: []string{"bot/light-review"},
			latest: map[string]Review{"bob": {Reviewer: "bob", Verdict: "commented", SubmittedAt: time.Now()}},
			want:   GroupReview,
		},
		{
			name:   "dismissed review doesn't count toward the requirement",
			pr:     openPR,
			viewer: "alex",
			labels: []string{"bot/light-review"},
			latest: map[string]Review{"bob": {Reviewer: "bob", Verdict: "dismissed", SubmittedAt: time.Now()}},
			want:   GroupReview,
		},
		{
			name:   "renovate bot routes to renovate group",
			pr:     PullRequest{Author: "renovate-with-github-actions[bot]", Status: "open"},
			viewer: "alex",
			want:   GroupRenovate,
		},
		{
			name:   "renovate bot routes to reviewed when viewer already reviewed",
			pr:     PullRequest{Author: "renovate-with-github-actions[bot]", Status: "open"},
			viewer: "alex",
			latest: map[string]Review{"alex": approved("alex")},
			want:   GroupReviewed,
		},
		{
			name:           "renovate bot back to renovate when viewer re-requested",
			pr:             PullRequest{Author: "renovate-with-github-actions[bot]", Status: "open"},
			viewer:         "alex",
			latest:         map[string]Review{"alex": approved("alex")},
			reviewRequests: []string{"alex"},
			want:           GroupRenovate,
		},
		{
			name:   "renovate bot still hidden on draft",
			pr:     PullRequest{Author: "renovate-with-github-actions[bot]", Status: "open", Draft: true},
			viewer: "alex",
			want:   "",
		},
		{
			name:   "renovate bot merged still wins as merged",
			pr:     PullRequest{Author: "renovate-with-github-actions[bot]", Status: "merged"},
			viewer: "alex",
			want:   GroupMerged,
		},
	}
	r := testRules()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := r.ClassifyGroup(tc.pr, tc.viewer, tc.latest, tc.labels, tc.reviewRequests)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyGroup_multipleBotAuthors(t *testing.T) {
	r := NewRules(UserSettings{
		DefaultRequiredReviewers: 2,
		BotAuthors:               []string{"botA[bot]", "botB[bot]"},
	})
	for _, author := range []string{"botA[bot]", "botB[bot]"} {
		t.Run(author, func(t *testing.T) {
			got := r.ClassifyGroup(
				PullRequest{Author: author, Status: "open"},
				"alex",
				nil, nil, nil,
			)
			assert.Equal(t, GroupRenovate, got)
		})
	}
}
