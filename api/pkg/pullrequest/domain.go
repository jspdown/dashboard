package pullrequest

import "time"

// PullRequest is the domain entity the rules in rules.go reason about: the
// facts about a PR (author, status, draft, head commit, age) with no
// persistence or presentation concerns. Persistence types (db tags) live in
// store.go; view/wire types (json tags) in pullrequest.go.
type PullRequest struct {
	GithubID  int64
	Repo      string
	Number    int
	Title     string
	Author    string
	Status    string
	Draft     bool
	Additions int
	Deletions int
	Comments  int
	HeadSHA   string
	CreatedAt time.Time
	MergedAt  *time.Time
}

// Review is the latest known state of a single reviewer on a PR.
type Review struct {
	Reviewer    string
	Verdict     string
	SubmittedAt time.Time
}

// CheckRun is one CI check tied to a (repo, head_sha) pair.
type CheckRun struct {
	Name        string
	RunStatus   string
	Conclusion  *string
	CompletedAt *time.Time
}

// PullRequestSnapshot bundles a PR with the related collections the rules
// reason about. The store builds it from one atomic query so every
// collection reflects the same database snapshot.
type PullRequestSnapshot struct {
	PullRequest
	Reviews        []Review
	Labels         []string
	ReviewRequests []string
	CheckRuns      []CheckRun
	// View is the viewer's last recorded look at this PR, nil if never
	// viewed. ComputeActivity diffs the snapshot against this baseline.
	View *ViewState
}

// ViewState is the recorded baseline from the last time the viewer
// opened (or explicitly dismissed) a PR. It is the input to
// ComputeActivity along with the current PullRequestSnapshot.
type ViewState struct {
	ViewedAt            time.Time
	CommentsCountAtView int
	HeadSHAAtView       string
}

// Activity describes what changed on a PR since the viewer's last view.
// Unread is true whenever there's anything new, including the never-viewed
// case where the per-event counts stay zero.
type Activity struct {
	Unread      bool
	NewCommits  bool
	NewComments int
	NewReviews  int
}

// Pull-request lifecycle states. Matches the values stored in
// pull_requests.status and the values produced by the GitHub adapter
// in pkg/github/ingest.go.
const (
	StatusOpen   = "open"
	StatusMerged = "merged"
	StatusClosed = "closed"
)

// Review verdicts. Matches the canonical values stored in
// pull_request_reviews.verdict (see normalizeReviewState in
// pkg/github/ingest.go).
const (
	VerdictApproved         = "approved"
	VerdictChangesRequested = "changes_requested"
	VerdictDismissed        = "dismissed"
	VerdictCommented        = "commented"
)

// CI status values returned by RollupCI. They match the strings the
// frontend has been consuming, so the JSON wire shape is preserved.
const (
	CINone    = "none"
	CIFailing = "failing"
	CIPending = "pending"
	CIPassing = "passing"
)

// Group values returned by ClassifyGroup. Empty string means the PR is
// not surfaced to the viewer at all.
const (
	GroupReview   = "review"
	GroupRenovate = "renovate"
	GroupMine     = "mine"
	GroupReviewed = "reviewed"
	GroupMerged   = "merged"
)

// Merge-readiness states returned by MergeReadiness. They answer "can this
// PR merge under our review policy" (the configured approval count, which
// GitHub branch protection doesn't enforce here). Empty string means the
// question doesn't apply, e.g. an already-merged PR.
const (
	MergeReady         = "ready"
	MergeNeedsApproval = "needs_approval"
	MergeCIPending     = "ci_pending"
	MergeBlocked       = "blocked"
	MergeDraft         = "draft"
)
