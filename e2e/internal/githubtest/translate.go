package githubtest

// translate.go is the only file that knows GitHub's wire format. A
// changed response shape or a newly-read dashboard field lands here;
// scenario.go and tests stay in dashboard-domain vocabulary.
//
// The CI lint check `grep -l "node_id"` scoped to internal/githubtest
// should report exactly this one file. Anything else means the wire
// format leaked elsewhere.

import (
	"fmt"
	"sort"
	"time"
)

// jsonUser is the minimal GitHub user shape go-github reads via
// pr.GetUser().GetLogin() and friends. AvatarURL is set on the fake's
// authenticated-user endpoint; PR-embedded users leave it empty (the
// dashboard doesn't read it from there).
type jsonUser struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// jsonLabel matches GitHub's PR label shape.
type jsonLabel struct {
	Name string `json:"name"`
}

// jsonRef is the head/base ref shape: pr.GetHead().GetSHA() reads sha.
type jsonRef struct {
	SHA   string   `json:"sha"`
	Ref   string   `json:"ref"`
	Label string   `json:"label"`
	User  jsonUser `json:"user"`
}

// jsonPR is the field set the dashboard's ingester actually reads,
// per pkg/github/ingest.go's pullRequestRow. Any field below maps
// to a Get*() call there; new fields land when ingestion grows.
type jsonPR struct {
	ID                 int64       `json:"id"`
	NodeID             string      `json:"node_id"`
	Number             int         `json:"number"`
	Title              string      `json:"title"`
	State              string      `json:"state"` // "open" | "closed"
	Draft              bool        `json:"draft"`
	User               jsonUser    `json:"user"`
	Body               string      `json:"body"`
	Additions          int         `json:"additions"`
	Deletions          int         `json:"deletions"`
	Comments           int         `json:"comments"`
	ReviewComments     int         `json:"review_comments"`
	Labels             []jsonLabel `json:"labels"`
	RequestedReviewers []jsonUser  `json:"requested_reviewers"`
	Head               jsonRef     `json:"head"`
	Base               jsonRef     `json:"base"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
	ClosedAt           *time.Time  `json:"closed_at"`
	MergedAt           *time.Time  `json:"merged_at"`
	MergedBy           *jsonUser   `json:"merged_by"`
}

// jsonReview is the submitted review shape go-github exposes via
// review.GetID(), GetUser(), GetState(), GetSubmittedAt(), GetBody().
// State is uppercase from REST; pkg/github/ingest.go's
// normalizeReviewState lowercases it on read.
type jsonReview struct {
	ID          int64     `json:"id"`
	NodeID      string    `json:"node_id"`
	User        jsonUser  `json:"user"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// jsonCheckRunsList wraps a check-run page; go-github decodes both the
// total_count and the runs array.
type jsonCheckRunsList struct {
	TotalCount int            `json:"total_count"`
	CheckRuns  []jsonCheckRun `json:"check_runs"`
}

// jsonCheckRun is the field set the ingester reads via cr.GetID(),
// GetName(), GetStatus(), GetConclusion(), GetCompletedAt(),
// GetHeadSHA().
type jsonCheckRun struct {
	ID          int64      `json:"id"`
	HeadSHA     string     `json:"head_sha"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  *string    `json:"conclusion"`
	CompletedAt *time.Time `json:"completed_at"`
}

// jsonRepo is the minimal repo shape VerifyRepos asserts on
// (Repositories.Get is a presence check; the dashboard doesn't read
// any field of the response payload beyond the absence of an HTTP
// error).
type jsonRepo struct {
	ID       int64    `json:"id"`
	NodeID   string   `json:"node_id"`
	Name     string   `json:"name"`
	FullName string   `json:"full_name"`
	Owner    jsonUser `json:"owner"`
	Private  bool     `json:"private"`
}

// jsonRateLimit is what /rate_limit returns. The auth watcher only
// looks at .resources.core.{limit,remaining,reset}.
type jsonRateLimit struct {
	Resources struct {
		Core struct {
			Limit     int   `json:"limit"`
			Remaining int   `json:"remaining"`
			Reset     int64 `json:"reset"`
			Used      int   `json:"used"`
		} `json:"core"`
	} `json:"resources"`
}

// snapshotRepoState turns a repoState into the wire shapes the
// dashboard's GitHub client expects. All access to PR/Review/Check
// fields lives in this file; callers get ready-to-marshal JSON structs.
// The Server holds a read lock during this call.

func (s *Server) prJSON(pr *PR) jsonPR {
	updatedAt := pr.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = s.anchor
	}
	createdAt := pr.CreatedAt
	if createdAt.IsZero() {
		createdAt = s.anchor.Add(-time.Hour)
	}
	state := "open"
	if pr.Status == "closed" || pr.Status == "merged" {
		state = "closed"
	}

	out := jsonPR{
		ID:                 stableID("pr", pr.Number),
		NodeID:             fmt.Sprintf("PR_kw%d", pr.Number),
		Number:             pr.Number,
		Title:              pr.Title,
		State:              state,
		Draft:              pr.Draft,
		User:               userJSON(pr.Author),
		Body:               pr.Body,
		Additions:          pr.Additions,
		Deletions:          pr.Deletions,
		Comments:           pr.Comments,
		ReviewComments:     0,
		Labels:             make([]jsonLabel, 0, len(pr.Labels)),
		RequestedReviewers: make([]jsonUser, 0, len(pr.Reviewers)),
		Head: jsonRef{
			SHA:  pr.HeadSHA,
			Ref:  fmt.Sprintf("topic/pr-%d", pr.Number),
			User: userJSON(pr.Author),
		},
		Base: jsonRef{
			SHA:  "0000000000000000000000000000000000000000",
			Ref:  "main",
			User: userJSON("octocat"),
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	for _, l := range pr.Labels {
		out.Labels = append(out.Labels, jsonLabel{Name: l})
	}
	for _, r := range pr.Reviewers {
		out.RequestedReviewers = append(out.RequestedReviewers, userJSON(r))
	}
	if !pr.ClosedAt.IsZero() {
		t := pr.ClosedAt
		out.ClosedAt = &t
	}
	if !pr.MergedAt.IsZero() {
		t := pr.MergedAt
		out.MergedAt = &t
		if pr.MergedBy != "" {
			u := userJSON(pr.MergedBy)
			out.MergedBy = &u
		}
	}
	return out
}

// listPRsJSON returns PRs filtered by state (matching the GitHub
// /pulls endpoint's state= query parameter), sorted by updated_at
// descending to match the dashboard's request shape (sort=updated,
// direction=desc).
func (s *Server) listPRsJSON(slug, state string) []jsonPR {
	r, ok := s.repos[slug]
	if !ok {
		return nil
	}
	out := make([]jsonPR, 0, len(r.prs))
	for _, pr := range r.prs {
		if !matchesPRState(pr, state) {
			continue
		}
		out = append(out, s.prJSON(pr))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func matchesPRState(pr *PR, state string) bool {
	switch state {
	case "open":
		return pr.Status == "open"
	case "closed":
		return pr.Status == "closed" || pr.Status == "merged"
	case "all", "":
		return true
	default:
		return false
	}
}

func (s *Server) reviewsJSON(pr *PR) []jsonReview {
	out := make([]jsonReview, 0, len(pr.Reviews))
	for _, r := range pr.Reviews {
		out = append(out, jsonReview{
			ID:          subID("review", pr.Number, int(r.ID)),
			NodeID:      fmt.Sprintf("PRR_kw%d_%d", pr.Number, r.ID),
			User:        userJSON(r.Reviewer),
			State:       upperVerdict(r.Verdict),
			Body:        r.Body,
			SubmittedAt: r.SubmittedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SubmittedAt.Before(out[j].SubmittedAt)
	})
	return out
}

func (s *Server) checksJSON(pr *PR) jsonCheckRunsList {
	runs := make([]jsonCheckRun, 0, len(pr.Checks))
	for _, c := range pr.Checks {
		var concl *string
		if c.Conclusion != "" {
			cc := c.Conclusion
			concl = &cc
		}
		var done *time.Time
		if !c.CompletedAt.IsZero() {
			d := c.CompletedAt
			done = &d
		}
		runs = append(runs, jsonCheckRun{
			ID:          subID("check", pr.Number, int(c.ID)),
			HeadSHA:     pr.HeadSHA,
			Name:        c.Name,
			Status:      c.Status,
			Conclusion:  concl,
			CompletedAt: done,
		})
	}
	return jsonCheckRunsList{
		TotalCount: len(runs),
		CheckRuns:  runs,
	}
}

func (s *Server) repoJSON(slug string) (jsonRepo, bool) {
	r, ok := s.repos[slug]
	if !ok {
		return jsonRepo{}, false
	}
	return jsonRepo{
		ID:       stableID("repo", len(slug)),
		NodeID:   "R_kw" + slug,
		Name:     r.name,
		FullName: slug,
		Owner:    userJSON(r.owner),
	}, true
}

func userJSON(login string) jsonUser {
	return jsonUser{
		Login: login,
		ID:    stableID("user", len(login)),
		Type:  "User",
	}
}

func upperVerdict(v string) string {
	switch v {
	case "approved":
		return "APPROVED"
	case "changes_requested":
		return "CHANGES_REQUESTED"
	case "commented":
		return "COMMENTED"
	case "dismissed":
		return "DISMISSED"
	default:
		return v
	}
}

// subID derives a deterministic, globally-unique ID for a per-PR
// sub-resource (a review or check run). Their local counters restart at
// 1 per PR, so stableID("review", localID) alone would mint the same ID
// for two PRs' first reviews, and since the ingester keys those rows by
// ID a later-polled PR's review would clobber an earlier one. Folding in
// pr.Number disambiguates them, matching the globally-unique PR-number
// assumption already baked into stableID("pr", pr.Number).
func subID(kind string, prNumber, local int) int64 {
	return stableID(kind, prNumber*1000+local)
}

// stableID gives JSON responses deterministic IDs derived from the
// kind+number tuple. Real GitHub IDs are opaque integers; ours are
// reproducible so test failures point at the same row across runs.
func stableID(kind string, n int) int64 {
	var seed int64
	for _, b := range []byte(kind) {
		seed = seed*131 + int64(b)
	}
	return seed*1000 + int64(n)
}

// graphQLRollupResponse builds the rollup-query response for one repo,
// matching pkg/github/graphql.go's expected shape. The dashboard pages
// 50 at a time; we return everything in one page since test scenarios
// never approach that count.
type graphQLRollupResponse struct {
	Data struct {
		Repository struct {
			PullRequests struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []graphQLRollupNode `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
}

type graphQLRollupNode struct {
	Number     int    `json:"number"`
	HeadRefOID string `json:"headRefOid"`
	Commits    struct {
		Nodes []graphQLCommitNode `json:"nodes"`
	} `json:"commits"`
}

type graphQLCommitNode struct {
	Commit struct {
		StatusCheckRollup *graphQLRollupState `json:"statusCheckRollup"`
	} `json:"commit"`
}

type graphQLRollupState struct {
	State string `json:"state"`
}

func (s *Server) graphQLRollup(slug string) graphQLRollupResponse {
	var resp graphQLRollupResponse
	r, ok := s.repos[slug]
	if !ok {
		return resp
	}
	for _, pr := range r.prs {
		if pr.Status != "open" {
			continue
		}
		node := graphQLRollupNode{
			Number:     pr.Number,
			HeadRefOID: pr.HeadSHA,
		}
		if state := rollupState(pr.Checks); state != "" {
			commit := graphQLCommitNode{}
			commit.Commit.StatusCheckRollup = &graphQLRollupState{State: state}
			node.Commits.Nodes = append(node.Commits.Nodes, commit)
		}
		resp.Data.Repository.PullRequests.Nodes = append(resp.Data.Repository.PullRequests.Nodes, node)
	}
	return resp
}

// rollupState mimics GitHub's combined check + commit-status rollup
// over our domain Check list. Failures dominate, then pending, then
// success. Empty → no rollup yet.
func rollupState(checks []Check) string {
	if len(checks) == 0 {
		return ""
	}
	anyFail, anyPending := false, false
	for _, c := range checks {
		if c.Status != "completed" {
			anyPending = true
			continue
		}
		switch c.Conclusion {
		case "failure", "cancelled", "timed_out", "action_required":
			anyFail = true
		case "":
			anyPending = true
		}
	}
	switch {
	case anyFail:
		return "FAILURE"
	case anyPending:
		return "PENDING"
	default:
		return "SUCCESS"
	}
}
