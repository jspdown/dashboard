package pullrequest

import (
	"fmt"
	"time"
)

// Presentation helpers: they shape domain values into the strings the
// dashboard renders, so the rules in rules.go stay free of formatting.

// newPullRequestView assembles the view shape from a snapshot, group, and
// latest-review map. It calls the view-bound rules (RollupCI, BlockingUsers,
// ComputeActivity) and the formatting helpers (ChecksLabel, Age,
// HumanizeMerged) so the composition layer stays formatting-free.
func newPullRequestView(snap PullRequestSnapshot, group, viewer string, latest map[string]Review, required, staleAfterDays int, now time.Time) PullRequestView {
	ciStatus, done, total := RollupCI(snap.CheckRuns)

	view := PullRequestView{
		ID:       int(snap.GithubID),
		Group:    group,
		Title:    snap.Title,
		Repo:     snap.Repo,
		Num:      snap.Number,
		Author:   snap.Author,
		Changes:  fmt.Sprintf("+%d -%d", snap.Additions, snap.Deletions),
		Comments: snap.Comments,
		Blocking: BlockingUsers(snap.ReviewRequests, latest),
		CI:       ciStatus,
		Checks:   ChecksLabel(done, total),
	}
	// Review progress and merge-readiness only make sense while a PR is
	// still open; a merged PR's "n/m approved" would be noise.
	if group != GroupMerged {
		view.Approvals = ApprovalCount(latest)
		view.RequiredApprovals = required
		view.MergeState = MergeReadiness(snap.Draft, view.Approvals, required, ciStatus, latest)
	}
	if group == GroupMerged && snap.MergedAt != nil {
		view.Age = Age(now, *snap.MergedAt)
		view.Merged = HumanizeMerged(now, *snap.MergedAt)
	} else {
		view.Age = Age(now, snap.CreatedAt)
		view.Stale = view.Age >= staleAfterDays
	}
	if group == GroupMine || group == GroupReviewed {
		applyActivity(&view, ComputeActivity(snap), snap.View != nil)
	}
	if group == GroupReviewed {
		view.ViewerVerdict = viewerVerdict(snap.Reviews, viewer)
	}
	return view
}

// viewerVerdict picks the verdict to show for the viewer in the "Reviewed
// by me" group: the latest of their reviews whose state isn't "commented".
// On GitHub a "commented" review is just a comment in a review envelope and
// doesn't change the prior approved/changes_requested verdict, so it must
// not shadow the real one here.
func viewerVerdict(reviews []Review, viewer string) string {
	var latest Review
	for _, r := range reviews {
		if r.Reviewer != viewer || r.Verdict == VerdictCommented {
			continue
		}
		if !r.SubmittedAt.After(latest.SubmittedAt) {
			continue
		}
		latest = r
	}
	return latest.Verdict
}

// applyActivity copies ComputeActivity's result onto the view. The hover
// breakdown is attached only when the viewer has a recorded baseline;
// without one the deltas are all-zero and meaningless, so the frontend
// falls back to a "Never viewed" tooltip.
func applyActivity(view *PullRequestView, a Activity, hasBaseline bool) {
	view.Unread = a.Unread
	if !hasBaseline {
		return
	}
	view.NewActivity = &NewActivity{
		NewCommits:  a.NewCommits,
		NewComments: a.NewComments,
		NewReviews:  a.NewReviews,
	}
}

// ChecksLabel formats RollupCI's done/total as the string the UI shows
// in the checks column.
func ChecksLabel(done, total int) string {
	if total == 0 {
		return "—"
	}
	return fmt.Sprintf("%d/%d", done, total)
}

// Age returns the integer number of days between t and now, clamped at
// zero so future timestamps don't produce negatives.
func Age(now, t time.Time) int {
	d := now.Sub(t)
	if d < 0 {
		return 0
	}
	return int(d / (24 * time.Hour))
}

// HumanizeMerged renders a merged-at timestamp as "today" / "yesterday"
// / "Nd ago" for the recently-merged section.
func HumanizeMerged(now, mergedAt time.Time) string {
	days := Age(now, mergedAt)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%dd ago", days)
	}
}
