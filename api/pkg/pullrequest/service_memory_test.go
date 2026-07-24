package pullrequest

import "context"

// memoryService is a test-only Service serving the canned fixtures from
// fixtures_test.go. It backs the table-driven service_test.go that pins
// matchFilter/applySort against known expected IDs. Production has only
// PostgresService.
type memoryService struct {
	prs []PullRequestView
}

func newMemoryService() *memoryService {
	prs := make([]PullRequestView, len(fixtures))
	copy(prs, fixtures)
	return &memoryService{prs: prs}
}

func (s *memoryService) List(_ context.Context, opts ListOpts) ([]PullRequestView, error) {
	out := make([]PullRequestView, 0, len(s.prs))
	for _, pr := range s.prs {
		if !matchFilter(pr, opts.Filter) {
			continue
		}
		out = append(out, pr)
	}
	applySort(out, opts.Sort)
	return out, nil
}

func (s *memoryService) MarkViewed(_ context.Context, githubID int64) error {
	for i := range s.prs {
		if s.prs[i].ID == int(githubID) {
			s.prs[i].Unread = false
			s.prs[i].NewActivity = nil
		}
	}
	return nil
}
