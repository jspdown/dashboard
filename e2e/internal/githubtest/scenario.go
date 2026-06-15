// Package githubtest provides a fake GitHub server for end-to-end tests.
//
// Scenarios are authored in the dashboard's vocabulary (PRs, labels,
// reviews, checks); one translator renders them as REST or GraphQL, so a
// GitHub schema change has a single place to land. Safe for concurrent
// use: tests can mutate scenarios while the poller fetches.
package githubtest

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// PR describes a pull request in dashboard-visible terms. Fields match
// what the ingester reads, not GitHub's full schema; add one only when
// something downstream needs it.
type PR struct {
	Number    int
	Title     string
	Author    string
	Status    string // "open" | "closed" | "merged"
	Draft     bool
	HeadSHA   string
	Body      string
	Additions int
	Deletions int
	Comments  int

	// Timestamps. A zero value falls back to the scenario anchor. Use
	// AddReview etc. with negative offsets for relative authoring.
	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  time.Time
	MergedAt  time.Time
	MergedBy  string

	Labels    []string
	Reviewers []string // pending review requests
	Reviews   []Review
	Checks    []Check
}

// Review is one submitted review on a PR.
type Review struct {
	ID          int64
	Reviewer    string
	Verdict     string // "approved" | "changes_requested" | "commented" | "dismissed"
	SubmittedAt time.Time
	Body        string
}

// Check is one check run on a PR's head commit.
type Check struct {
	ID          int64
	Name        string
	Status      string // "completed" | "in_progress" | "queued"
	Conclusion  string // "success" | "failure" | "cancelled" | "neutral" | "skipped" | "timed_out" | "action_required" | ""
	CompletedAt time.Time
}

// Server is the test fixture: a fluent builder plus an httptest.Server
// (in server.go). Build scenarios with Repo / PR, then point the
// dashboard's GitHub client at Server.URL. Builder methods are safe for
// concurrent use; a mid-flight mutation shows up on the next fetch.
type Server struct {
	t *testing.T

	mu          sync.RWMutex
	anchor      time.Time
	tick        int // logical clock; each mutation bumps the affected PR's UpdatedAt
	repos       map[string]*repoState
	currentUser FakeUser
	orgMembers  map[string]map[string]struct{}
	httpx       *httpServer // populated by server.go
}

// touch bumps the logical clock and stamps a fresh UpdatedAt so the
// cursor-based poller sees the PR as changed next tick. Caller holds s.mu.
func (s *Server) touch(pr *PR) {
	s.tick++
	pr.UpdatedAt = s.anchor.Add(time.Duration(s.tick) * time.Millisecond)
}

type repoState struct {
	owner string
	name  string
	prs   map[int]*PR
}

// New starts a fake GitHub server bound to t and closed via t.Cleanup;
// don't call Close yourself. The anchor defaults to now (relative offsets
// in builder calls are measured from it); pin it with WithAnchor for
// fully deterministic tests.
func New(t *testing.T, opts ...Option) *Server {
	t.Helper()
	s := newDetached(opts...)
	s.BindT(t)
	t.Cleanup(s.Close)
	return s
}

// NewDetached starts a fake GitHub server whose lifecycle the caller
// manages via Close. Used by the dev-e2e binary; tests should prefer New.
func NewDetached(opts ...Option) *Server {
	return newDetached(opts...)
}

func newDetached(opts ...Option) *Server {
	s := &Server{
		anchor:      time.Now().UTC(),
		repos:       make(map[string]*repoState),
		currentUser: defaultFakeUser,
		orgMembers:  make(map[string]map[string]struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.startHTTP()
	return s
}

// Close tears down the underlying httptest.Server. Safe to call more than
// once. Tests rely on t.Cleanup; non-test callers must call it themselves.
func (s *Server) Close() {
	s.close()
}

// BindT attaches a testing.T to a detached Server so builder failures and
// unhandled-route warnings go through the test logger instead of stderr.
// The harness calls it when promoting a Boot()'d Stack to a Start()'d Harness.
func (s *Server) BindT(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t = t
}

// fatalf fails the test in test mode and panics in detached mode (so the
// dev binary crashes loudly). Don't recover from it.
func (s *Server) fatalf(format string, args ...any) {
	if s.t != nil {
		s.t.Helper()
		s.fatalf(format, args...)
		return
	}
	panic(fmt.Sprintf(format, args...))
}

// logf routes to t.Logf in tests and stderr in detached mode.
func (s *Server) logf(format string, args ...any) {
	if s.t != nil {
		s.t.Logf(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// Option configures a Server at construction time.
type Option func(*Server)

// WithAnchor pins the scenario's anchor time. All relative timestamps
// in builder calls are computed from this.
func WithAnchor(t time.Time) Option {
	return func(s *Server) { s.anchor = t.UTC() }
}

// URL returns the base URL the dashboard's GitHub client should be
// pointed at. Both REST (/repos/...) and GraphQL (/graphql) traffic
// route through this URL.
func (s *Server) URL() string {
	return s.httpx.url()
}

// Anchor returns the scenario's anchor time. Useful for asserting on
// timestamps later or for computing absolute times outside the builder.
func (s *Server) Anchor() time.Time {
	return s.anchor
}

// Repo returns a builder for the named repo, registering it on first use
// and returning the existing one afterward so tests can interleave repo
// declaration with PR mutation.
func (s *Server) Repo(slug string) *RepoBuilder {
	owner, name, ok := splitSlug(slug)
	if !ok {
		s.fatalf("githubtest: invalid repo slug %q (expected owner/name)", slug)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, found := s.repos[slug]
	if !found {
		r = &repoState{owner: owner, name: name, prs: map[int]*PR{}}
		s.repos[slug] = r
	}
	return &RepoBuilder{s: s, repo: r}
}

// PR returns a builder for an existing PR, to mutate state between poll
// ticks. Fails the test if it doesn't exist (use Repo(slug).PR to create).
func (s *Server) PR(slug string, number int) *PRBuilder {
	s.mu.RLock()
	r, ok := s.repos[slug]
	s.mu.RUnlock()
	if !ok {
		s.fatalf("githubtest: PR(%q, %d): repo not declared yet", slug, number)
	}
	s.mu.RLock()
	pr, ok := r.prs[number]
	s.mu.RUnlock()
	if !ok {
		s.fatalf("githubtest: PR(%q, %d): PR not declared yet", slug, number)
	}
	return &PRBuilder{s: s, repo: r, pr: pr}
}

// RepoBuilder configures a single repo. Returned by Server.Repo.
type RepoBuilder struct {
	s    *Server
	repo *repoState
}

// PR registers (or returns) a PR in this repo. Unset fields default:
// Title "Test PR #N", Author "octocat", Status "open", HeadSHA from the number.
func (rb *RepoBuilder) PR(number int) *PRBuilder {
	rb.s.mu.Lock()
	defer rb.s.mu.Unlock()
	pr, ok := rb.repo.prs[number]
	if !ok {
		pr = &PR{
			Number:    number,
			Title:     fmt.Sprintf("Test PR #%d", number),
			Author:    "octocat",
			Status:    "open",
			HeadSHA:   fmt.Sprintf("sha-%s-%d", rb.repo.name, number),
			CreatedAt: rb.s.anchor.Add(-1 * time.Hour),
		}
		rb.repo.prs[number] = pr
		rb.s.touch(pr)
	}
	return &PRBuilder{s: rb.s, repo: rb.repo, pr: pr}
}

// PRBuilder mutates a single PR. Returned by RepoBuilder.PR or Server.PR.
// Methods chain; Done() ends the chain and returns the parent Server.
type PRBuilder struct {
	s    *Server
	repo *repoState
	pr   *PR
}

// Title sets the PR title.
func (pb *PRBuilder) Title(t string) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Title = t
	pb.s.touch(pb.pr)
	return pb
}

// Author sets the PR author login.
func (pb *PRBuilder) Author(login string) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Author = login
	pb.s.touch(pb.pr)
	return pb
}

// Open marks the PR as open (not draft).
func (pb *PRBuilder) Open() *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Status = "open"
	pb.pr.Draft = false
	pb.s.touch(pb.pr)
	return pb
}

// Draft marks the PR as open and draft.
func (pb *PRBuilder) Draft() *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Status = "open"
	pb.pr.Draft = true
	pb.s.touch(pb.pr)
	return pb
}

// Merged marks the PR as merged at the given offset from the scenario
// anchor (use a negative offset for "in the past").
func (pb *PRBuilder) Merged(offset time.Duration, mergedBy string) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Status = "merged"
	pb.pr.MergedAt = pb.s.anchor.Add(offset)
	pb.pr.ClosedAt = pb.pr.MergedAt
	pb.pr.MergedBy = mergedBy
	pb.s.touch(pb.pr)
	return pb
}

// Closed marks the PR as closed (not merged) at the given offset.
func (pb *PRBuilder) Closed(offset time.Duration) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Status = "closed"
	pb.pr.ClosedAt = pb.s.anchor.Add(offset)
	pb.s.touch(pb.pr)
	return pb
}

// Age sets CreatedAt to anchor minus age so the PR's reported age matches
// the requested duration. Handy for stale-threshold scenarios.
func (pb *PRBuilder) Age(age time.Duration) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.CreatedAt = pb.s.anchor.Add(-age)
	pb.s.touch(pb.pr)
	return pb
}

// Labels replaces the PR's label set with the given names.
func (pb *PRBuilder) Labels(names ...string) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Labels = append([]string(nil), names...)
	pb.s.touch(pb.pr)
	return pb
}

// AddLabel adds a single label, preserving existing ones.
func (pb *PRBuilder) AddLabel(name string) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	if slices.Contains(pb.pr.Labels, name) {
		return pb
	}
	pb.pr.Labels = append(pb.pr.Labels, name)
	pb.s.touch(pb.pr)
	return pb
}

// RemoveLabel removes a single label, no-op if it wasn't present.
func (pb *PRBuilder) RemoveLabel(name string) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	out := pb.pr.Labels[:0]
	for _, l := range pb.pr.Labels {
		if l != name {
			out = append(out, l)
		}
	}
	pb.pr.Labels = out
	pb.s.touch(pb.pr)
	return pb
}

// Reviewers replaces the pending review-request set.
func (pb *PRBuilder) Reviewers(logins ...string) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	pb.pr.Reviewers = append([]string(nil), logins...)
	pb.s.touch(pb.pr)
	return pb
}

// AddReview appends a submitted review. Offset is relative to the
// scenario anchor (negative = past).
func (pb *PRBuilder) AddReview(reviewer, verdict string, offset time.Duration) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	id := int64(len(pb.pr.Reviews) + 1)
	for _, r := range pb.pr.Reviews {
		if r.ID >= id {
			id = r.ID + 1
		}
	}
	pb.pr.Reviews = append(pb.pr.Reviews, Review{
		ID:          id,
		Reviewer:    reviewer,
		Verdict:     verdict,
		SubmittedAt: pb.s.anchor.Add(offset),
	})
	pb.s.touch(pb.pr)
	return pb
}

// AddCheck appends a check run on the PR's head SHA. Offset is relative
// to the scenario anchor (negative = past).
func (pb *PRBuilder) AddCheck(name, status, conclusion string, offset time.Duration) *PRBuilder {
	pb.s.mu.Lock()
	defer pb.s.mu.Unlock()
	id := int64(len(pb.pr.Checks) + 1)
	for _, c := range pb.pr.Checks {
		if c.ID >= id {
			id = c.ID + 1
		}
	}
	pb.pr.Checks = append(pb.pr.Checks, Check{
		ID:          id,
		Name:        name,
		Status:      status,
		Conclusion:  conclusion,
		CompletedAt: pb.s.anchor.Add(offset),
	})
	pb.s.touch(pb.pr)
	return pb
}

// Done returns the parent Server for further work; useful when chaining
// across multiple repos in one statement.
func (pb *PRBuilder) Done() *Server { return pb.s }

func splitSlug(slug string) (owner, name string, ok bool) {
	owner, name, ok = strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}
