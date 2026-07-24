package pullrequest

import (
	"context"
	"sort"
)

type Service interface {
	List(ctx context.Context, opts ListOpts) ([]PullRequestView, error)
	MarkViewed(ctx context.Context, githubID int64) error
}

// matchFilter applies the chip filter. Staleness is computed per PR against its
// own profile's window (PullRequestView.Stale), so the "stale" chip carries no
// global threshold.
func matchFilter(pr PullRequestView, filter string) bool {
	switch filter {
	case "", "all":
		return true
	case "needs review":
		return pr.Group == GroupReview
	case "stale":
		return pr.Stale
	case "ci failing":
		return pr.CI == CIFailing
	default:
		return true
	}
}

func applySort(prs []PullRequestView, key string) {
	switch key {
	case "age":
		sort.SliceStable(prs, func(i, j int) bool { return prs[i].Age > prs[j].Age })
	case "repo":
		sort.SliceStable(prs, func(i, j int) bool { return prs[i].Repo < prs[j].Repo })
	case "author":
		sort.SliceStable(prs, func(i, j int) bool { return prs[i].Author < prs[j].Author })
	}
}
