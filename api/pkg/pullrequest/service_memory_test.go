package pullrequest

import "context"

// memoryService is a test-only Service serving the canned fixtures from
// fixtures_test.go. It backs the table-driven service_test.go that pins
// matchFilter/applySort against known expected IDs. Production has only
// PostgresService.
type memoryService struct {
	prs            []PullRequestView
	staleAfterDays int
}

// memoryStaleAfterDays is the in-memory service's staleness threshold. Matches
// the production default.
const memoryStaleAfterDays = 5

func newMemoryService() *memoryService {
	prs := make([]PullRequestView, len(fixtures))
	copy(prs, fixtures)
	return &memoryService{prs: prs, staleAfterDays: memoryStaleAfterDays}
}

func (s *memoryService) List(_ context.Context, opts ListOpts) ([]PullRequestView, error) {
	out := make([]PullRequestView, 0, len(s.prs))
	for _, pr := range s.prs {
		if !matchFilter(pr, opts.Filter, s.staleAfterDays) {
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
