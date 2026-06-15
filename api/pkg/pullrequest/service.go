package pullrequest

import (
	"context"
	"fmt"
	"sort"
)

type Service interface {
	List(ctx context.Context, opts ListOpts) ([]PullRequestView, error)
	MarkViewed(ctx context.Context, githubID int64) error
}

// matchFilter applies the chip filter. The "stale > Nd" chip uses the
// configured threshold, so we rebuild the same string here to match it exactly
// rather than parsing it back out.
func matchFilter(pr PullRequestView, filter string, staleAfterDays int) bool {
	switch filter {
	case "", "all":
		return true
	case "needs review":
		return pr.Group == GroupReview
	case fmt.Sprintf("stale > %dd", staleAfterDays):
		return pr.Age > staleAfterDays
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
