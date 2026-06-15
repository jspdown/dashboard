package pullrequest

// PullRequestView is the dashboard-facing shape of a PR. Some fields are
// pre-formatted (Changes, Checks, Merged, Age) so the frontend renders them
// as-is. JSON tags pin the wire shape.
type PullRequestView struct {
	ID       int      `json:"id"`
	Group    string   `json:"group"`
	Title    string   `json:"title"`
	Repo     string   `json:"repo"`
	Num      int      `json:"num"`
	Author   string   `json:"author"`
	Age      int      `json:"age"`
	CI       string   `json:"ci"`
	Changes  string   `json:"changes"`
	Checks   string   `json:"checks"`
	Comments int      `json:"comments"`
	Blocking []string `json:"blocking"`
	// Approvals / RequiredApprovals give review progress against the configured
	// policy ("2 LGTM") for the "n/m approved" fraction. MergeState is that
	// policy's merge-readiness verdict (see MergeReadiness). All three are
	// omitted for merged PRs.
	Approvals         int          `json:"approvals"`
	RequiredApprovals int          `json:"required_approvals"`
	MergeState        string       `json:"merge_state,omitempty"`
	Merged            string       `json:"merged,omitempty"`
	Unread            bool         `json:"unread"`
	NewActivity       *NewActivity `json:"new_activity,omitempty"`
	// ViewerVerdict is the viewer's latest review verdict, set only for the
	// "reviewed" group so the frontend can sort and tint rows by it.
	ViewerVerdict string `json:"viewer_verdict,omitempty"`
}

// NewActivity is the per-event breakdown shown on the row's unread hover. Set
// only when the viewer has a recorded view (so the deltas have a baseline) and
// only on groups that show the unread indicator.
type NewActivity struct {
	NewCommits  bool `json:"new_commits,omitempty"`
	NewComments int  `json:"new_comments,omitempty"`
	NewReviews  int  `json:"new_reviews,omitempty"`
}

type ListOpts struct {
	Filter string
	Sort   string
}
