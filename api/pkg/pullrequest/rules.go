package pullrequest

import (
	"slices"
	"sort"
)

// Rules holds one user's workflow knobs for classifying PRs and deciding how
// many reviewers each needs. It's built per request from the viewer's settings.
type Rules struct {
	settings UserSettings
}

// NewRules binds a user's settings to the rules engine. Rules is cheap to
// construct, so the service builds a fresh one per request from the viewer's
// stored (or default) settings.
func NewRules(settings UserSettings) *Rules {
	return &Rules{settings: settings}
}

// RequiredReviewers returns how many distinct reviewers a PR needs before
// it leaves "Needs my review", plus an ignored flag for PRs to drop
// entirely. They're separate because "zero required" and "drop it" differ.
// First matching ReviewerOverride wins; ignore labels beat overrides.
func (r *Rules) RequiredReviewers(labels []string) (count int, ignored bool) {
	for _, l := range r.settings.IgnoreLabels {
		if slices.Contains(labels, l) {
			return 0, true
		}
	}
	for _, o := range r.settings.ReviewerOverrides {
		if slices.Contains(labels, o.Label) {
			return o.Reviewers, false
		}
	}
	return r.settings.DefaultRequiredReviewers, false
}

// LatestReviewsByReviewer keeps the most recent review per reviewer. On a
// tie (same SubmittedAt) the first one seen wins, so pass reviews in
// insertion order if that matters.
func LatestReviewsByReviewer(reviews []Review) map[string]Review {
	out := make(map[string]Review, len(reviews))
	for _, r := range reviews {
		if existing, ok := out[r.Reviewer]; ok && !r.SubmittedAt.After(existing.SubmittedAt) {
			continue
		}
		out[r.Reviewer] = r
	}
	return out
}

// RollupCI summarizes check runs into one status plus completed/total counts:
//
//   - no checks              → CINone
//   - any conclusion=failure → CIFailing (wins over pending)
//   - any run not completed  → CIPending
//   - otherwise              → CIPassing
func RollupCI(checks []CheckRun) (status string, done, total int) {
	if len(checks) == 0 {
		return CINone, 0, 0
	}
	total = len(checks)
	anyFailed := false
	anyPending := false
	for _, c := range checks {
		if c.RunStatus == "completed" {
			done++
		} else {
			anyPending = true
		}
		if c.Conclusion != nil && *c.Conclusion == "failure" {
			anyFailed = true
		}
	}
	switch {
	case anyFailed:
		status = CIFailing
	case anyPending:
		status = CIPending
	default:
		status = CIPassing
	}
	return status, done, total
}

// ComputeActivity diffs a PR snapshot against the viewer's last recorded
// view and returns what's new. A nil View means never opened: Unread with
// zero deltas (no baseline to subtract). Comment deltas clamp at zero so a
// deletion that drops the count below the snapshot won't go negative.
func ComputeActivity(snap PullRequestSnapshot) Activity {
	if snap.View == nil {
		return Activity{Unread: true}
	}
	a := Activity{
		NewCommits:  snap.HeadSHA != snap.View.HeadSHAAtView,
		NewComments: snap.Comments - snap.View.CommentsCountAtView,
	}
	if a.NewComments < 0 {
		a.NewComments = 0
	}
	for _, r := range snap.Reviews {
		if r.SubmittedAt.After(snap.View.ViewedAt) {
			a.NewReviews++
		}
	}
	a.Unread = a.NewCommits || a.NewComments > 0 || a.NewReviews > 0
	return a
}

// BlockingUsers returns the set of users currently keeping a PR from
// merging, pending review requests plus reviewers whose latest verdict
// is "changes_requested". Sorted for deterministic JSON output.
func BlockingUsers(reviewRequests []string, latest map[string]Review) []string {
	seen := make(map[string]struct{}, len(reviewRequests)+len(latest))
	for _, r := range reviewRequests {
		seen[r] = struct{}{}
	}
	for _, r := range latest {
		if r.Verdict == VerdictChangesRequested {
			seen[r.Reviewer] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for u := range seen {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// ApprovalCount returns how many reviewers currently approve the PR. Pass
// the deduplicated map from LatestReviewsByReviewer so each reviewer counts
// once, on their latest verdict.
func ApprovalCount(latest map[string]Review) int {
	n := 0
	for _, r := range latest {
		if r.Verdict == VerdictApproved {
			n++
		}
	}
	return n
}

// MergeReadiness verdicts whether a PR can merge under our review policy
// (the configured approval count, which GitHub branch protection doesn't
// enforce for us). Precedence, highest first:
//
//   - draft                              → MergeDraft
//   - changes requested, or CI failing   → MergeBlocked
//   - fewer approvals than required      → MergeNeedsApproval
//   - CI still running                   → MergeCIPending
//   - otherwise                          → MergeReady
//
// ci comes from RollupCI, latest from LatestReviewsByReviewer, required
// from RequiredReviewers.
func MergeReadiness(draft bool, approvals, required int, ci string, latest map[string]Review) string {
	if draft {
		return MergeDraft
	}
	changesRequested := false
	for _, r := range latest {
		if r.Verdict == VerdictChangesRequested {
			changesRequested = true
			break
		}
	}
	if changesRequested || ci == CIFailing {
		return MergeBlocked
	}
	if approvals < required {
		return MergeNeedsApproval
	}
	if ci == CIPending {
		return MergeCIPending
	}
	return MergeReady
}

// ClassifyGroup picks the dashboard group a PR belongs to for the viewer,
// "" if it shouldn't appear anywhere. Branch order matters, keep it:
//
//  1. status=merged → GroupMerged
//  2. author=viewer → GroupMine
//  3. draft → ""
//  4. ignore label → ""
//  5. viewer already reviewed and not re-requested → GroupReviewed
//  6. distinct reviewers < required → GroupRenovate if author is a configured
//     bot, else GroupReview
//  7. otherwise → ""
//
// A re-request (or a "commented" review that doesn't clear the request, see
// ingest.handleReview) keeps the PR in GroupReview instead of the
// collapsed-by-default Reviewed group.
func (r *Rules) ClassifyGroup(pr PullRequest, viewer string, latest map[string]Review, labels []string, reviewRequests []string) string {
	if pr.Status == StatusMerged {
		return GroupMerged
	}
	if pr.Author == viewer {
		return GroupMine
	}
	if pr.Draft {
		return ""
	}
	required, ignored := r.RequiredReviewers(labels)
	if ignored {
		return ""
	}
	if _, viewerReviewed := latest[viewer]; viewerReviewed {
		if !slices.Contains(reviewRequests, viewer) {
			return GroupReviewed
		}
	}
	if len(latest) < required {
		if slices.Contains(r.settings.BotAuthors, pr.Author) {
			return GroupRenovate
		}
		return GroupReview
	}
	return ""
}
