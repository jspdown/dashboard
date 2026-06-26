package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v85/github"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient returns a *gh.Client whose BaseURL points at the given test
// server. This is the canonical pattern from go-github's own tests.
func newTestClient(t *testing.T, server *httptest.Server) *gh.Client {
	t.Helper()
	base, err := url.Parse(server.URL + "/")
	require.NoError(t, err)
	client := gh.NewClient(nil)
	client.BaseURL = base
	client.UploadURL = base
	return client
}

func newPoller(client *gh.Client) *Poller {
	return &Poller{client: client}
}

// makePR returns a minimal *gh.PullRequest with Number and UpdatedAt set,
// which is enough for listPullRequests to slice on.
func makePR(num int, updated time.Time) *gh.PullRequest {
	return &gh.PullRequest{
		Number:    gh.Ptr(num),
		UpdatedAt: &gh.Timestamp{Time: updated},
	}
}

// pageHandler serves a single page of PR JSON, adding a Link header to the next
// page when more remain.
func pageHandler(t *testing.T, pages [][]*gh.PullRequest) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if v := r.URL.Query().Get("page"); v != "" {
			p, err := strconv.Atoi(v)
			assert.NoError(t, err)
			page = p
		}
		idx := page - 1
		assert.GreaterOrEqual(t, idx, 0)
		assert.Less(t, idx, len(pages), "unexpected page %d requested", page)

		if page < len(pages) {
			next := *r.URL
			q := next.Query()
			q.Set("page", strconv.Itoa(page+1))
			next.RawQuery = q.Encode()
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next.String()))
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(pages[idx]))
	}
}

func TestPoller_listPullRequests_NoCursor(t *testing.T) {
	prs := []*gh.PullRequest{
		makePR(3, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)),
		makePR(2, time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)),
		makePR(1, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/pulls", pageHandler(t, [][]*gh.PullRequest{prs}))
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	got, newest, err := p.listPullRequests(context.Background(), "acme", "widget", "open", time.Time{})
	require.NoError(t, err)
	require.Len(t, got, 3, "all PRs returned when cursor is zero")
	assert.Equal(t, prs[0].UpdatedAt.Time, newest, "newestSeen tracks the most recent updated_at")
}

func TestPoller_listPullRequests_StopsAtCursor(t *testing.T) {
	cursor := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	prs := []*gh.PullRequest{
		makePR(3, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)),  // newer than cursor, included
		makePR(2, time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)), // newer than cursor, included
		makePR(1, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)),  // older, break
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/pulls", pageHandler(t, [][]*gh.PullRequest{prs}))
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	got, newest, err := p.listPullRequests(context.Background(), "acme", "widget", "all", cursor)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 3, got[0].GetNumber())
	assert.Equal(t, 2, got[1].GetNumber())
	assert.Equal(t, prs[0].UpdatedAt.Time, newest)
}

func TestPoller_listPullRequests_Paginates(t *testing.T) {
	page1 := []*gh.PullRequest{
		makePR(5, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)),
		makePR(4, time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)),
	}
	page2 := []*gh.PullRequest{
		makePR(3, time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)),
		makePR(2, time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/pulls", pageHandler(t, [][]*gh.PullRequest{page1, page2}))
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	got, newest, err := p.listPullRequests(context.Background(), "acme", "widget", "open", time.Time{})
	require.NoError(t, err)
	require.Len(t, got, 4, "follows the next-page link until exhausted")
	assert.Equal(t, page1[0].UpdatedAt.Time, newest)
}

func TestPoller_listPullRequests_StopsAcrossPages(t *testing.T) {
	cursor := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	page1 := []*gh.PullRequest{
		makePR(5, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)),
		makePR(4, time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)),
	}
	// First entry on page 2 is older than the cursor; pagination should stop
	// without touching any later page.
	page2 := []*gh.PullRequest{
		makePR(3, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/pulls", pageHandler(t, [][]*gh.PullRequest{page1, page2}))
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	got, newest, err := p.listPullRequests(context.Background(), "acme", "widget", "all", cursor)
	require.NoError(t, err)
	require.Len(t, got, 2, "stops once a PR older than cursor is seen on page 2")
	assert.Equal(t, page1[0].UpdatedAt.Time, newest)
}

func TestPoller_Nudge_CoalescesAndNeverBlocks(t *testing.T) {
	p := &Poller{nudge: make(chan struct{}, 1)}

	// Three nudges with no reconcile draining them. None may block; they
	// coalesce into a single pending signal.
	p.Nudge()
	p.Nudge()
	p.Nudge()

	select {
	case <-p.nudge:
	default:
		t.Fatal("expected one pending nudge after Nudge()")
	}
	select {
	case <-p.nudge:
		t.Fatal("nudges must coalesce into a single pending signal")
	default:
	}
}

// fakeRepoSource is a mutable, concurrency-safe DistinctRepos source so a test
// can change the observed set while the reconcile loop runs.
type fakeRepoSource struct {
	mu    sync.Mutex
	repos []string
}

func (f *fakeRepoSource) DistinctRepos(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.repos...), nil
}

func (f *fakeRepoSource) set(repos []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos = repos
}

// countingCursorStore records one tick per repo via RecordPoll, which pollRepo
// fires on every poll attempt (success or failure). It lets the reconcile test
// observe which repos are actively polling without a real Postgres.
type countingCursorStore struct {
	mu    sync.Mutex
	polls map[string]int
}

func (c *countingCursorStore) LoadCursor(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}
func (c *countingCursorStore) SaveCursor(context.Context, string, time.Time) error { return nil }
func (c *countingCursorStore) RecordPoll(_ context.Context, repo string, _ *string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.polls[repo]++
	return nil
}
func (c *countingCursorStore) count(repo string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.polls[repo]
}

type noopApplier struct{}

func (noopApplier) Apply(context.Context, any) error { return nil }

// TestPoller_Run_ReconcilesRepoSet drives the reconcile loop: a repo starts
// polling when observed, a newly-added repo starts on a Nudge, and a removed
// repo's ticker stops. The fake GitHub server 404s every request, so pollRepo
// fails fast at the list call but still records the attempt, which is all the
// test needs to see a repo's ticker firing.
func TestPoller_Run_ReconcilesRepoSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	src := &fakeRepoSource{repos: []string{"acme/widget"}}
	cur := &countingCursorStore{polls: map[string]int{}}

	p := &Poller{
		client:   newTestClient(t, server),
		ingester: noopApplier{},
		repos:    src,
		cursors:  cur,
		interval: 20 * time.Millisecond,
		nudge:    make(chan struct{}, 1),
		logger:   zerolog.Nop(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	require.Eventually(t, func() bool { return cur.count("acme/widget") >= 2 },
		2*time.Second, 10*time.Millisecond, "observed repo must start polling")

	// Add a repo and nudge: it must start without waiting a full reconcile tick.
	src.set([]string{"acme/widget", "acme/gadget"})
	p.Nudge()
	require.Eventually(t, func() bool { return cur.count("acme/gadget") >= 2 },
		2*time.Second, 10*time.Millisecond, "added repo must start polling after a nudge")

	// Remove widget and nudge. recordPoll is skipped once its context is
	// cancelled, so the count freezes the moment its ticker stops.
	src.set([]string{"acme/gadget"})
	p.Nudge()
	time.Sleep(150 * time.Millisecond) // let the cancel land and any in-flight poll settle
	frozen := cur.count("acme/widget")
	gadgetBefore := cur.count("acme/gadget")
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, frozen, cur.count("acme/widget"), "removed repo must stop polling")
	assert.Greater(t, cur.count("acme/gadget"), gadgetBefore, "remaining repo keeps polling")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestRepoFromSlug(t *testing.T) {
	r := repoFromSlug("acme/widget")
	assert.Equal(t, "acme/widget", r.GetFullName())
	assert.Equal(t, "widget", r.GetName())
	assert.Equal(t, "acme", r.GetOwner().GetLogin())
}

// captureLog runs fn against a trace-level logger writing to a buffer, then
// decodes the single JSON record it produced. Only for tests that emit exactly
// one entry.
func captureLog(t *testing.T, fn func(zerolog.Logger)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.TraceLevel)
	fn(logger)

	var out map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out),
		"expected single JSON log line, got %q", buf.String())
	return out
}

// fakeRateLimitError builds a *gh.RateLimitError suitable for asserting on,
// with just enough *http.Response to make .Error() not panic.
func fakeRateLimitError(t *testing.T, limit, remaining int, reset time.Time) *gh.RateLimitError {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/acme/widget/pulls", nil)
	require.NoError(t, err)
	return &gh.RateLimitError{
		Rate:     gh.Rate{Limit: limit, Remaining: remaining, Reset: gh.Timestamp{Time: reset}},
		Response: &http.Response{StatusCode: http.StatusForbidden, Request: req},
		Message:  "rate limit exceeded",
	}
}

func TestLogAPIError_AnonymousLimitFlagsAuthFailure(t *testing.T) {
	reset := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	rlErr := fakeRateLimitError(t, 60, 0, reset)
	wrapped := fmt.Errorf("listing pull requests: %w", rlErr)

	out := captureLog(t, func(logger zerolog.Logger) {
		logAPIError(logger, wrapped, "poll failed")
	})

	assert.Equal(t, "error", out["level"])
	assert.Equal(t, "anonymous", out["auth_state"])
	assert.EqualValues(t, 60, out["rate_limit"])
	assert.EqualValues(t, 0, out["rate_remaining"])
	assert.Contains(t, out["message"], "treating requests as anonymous")
	assert.Contains(t, out["message"], "DASHBOARD_GITHUB_TOKEN")
}

func TestLogAPIError_NormalRateLimitIsWarn(t *testing.T) {
	reset := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)
	rlErr := fakeRateLimitError(t, 5000, 0, reset)

	out := captureLog(t, func(logger zerolog.Logger) {
		logAPIError(logger, rlErr, "poll failed")
	})

	assert.Equal(t, "warn", out["level"])
	assert.EqualValues(t, 5000, out["rate_limit"])
	_, hasAuth := out["auth_state"]
	assert.False(t, hasAuth, "auth_state should only appear for anonymous (limit==60) errors")
	assert.Equal(t, "poll failed", out["message"])
}

func TestLogAPIError_SecondaryRateLimitIsWarn(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/acme/widget/pulls", nil)
	require.NoError(t, err)
	arl := &gh.AbuseRateLimitError{
		Response: &http.Response{StatusCode: http.StatusForbidden, Request: req},
		Message:  "secondary rate limit",
	}

	out := captureLog(t, func(logger zerolog.Logger) {
		logAPIError(logger, arl, "poll failed")
	})

	assert.Equal(t, "warn", out["level"])
	assert.Equal(t, "secondary_rate_limit", out["kind"])
}

func TestLogAPIError_GenericGitHubErrorAttachesStatus(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/acme/widget", nil)
	require.NoError(t, err)
	ghErr := &gh.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusUnauthorized, Request: req},
		Message:  "Bad credentials",
	}

	out := captureLog(t, func(logger zerolog.Logger) {
		logAPIError(logger, ghErr, "poll failed")
	})

	assert.Equal(t, "error", out["level"])
	assert.EqualValues(t, http.StatusUnauthorized, out["status_code"])
}

func TestLogAPIError_GenericErrorIsError(t *testing.T) {
	out := captureLog(t, func(logger zerolog.Logger) {
		logAPIError(logger, errors.New("connection reset"), "poll failed")
	})

	assert.Equal(t, "error", out["level"])
	assert.Equal(t, "poll failed", out["message"])
}

// rateLimitHandler serves /rate_limit responses with the given core limits.
func rateLimitHandler(t *testing.T, limit, remaining int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rate_limit", r.URL.Path)
		body := map[string]any{
			"resources": map[string]any{
				"core": map[string]any{
					"limit":     limit,
					"remaining": remaining,
					"reset":     time.Now().Add(time.Hour).Unix(),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}
}

func TestPoller_checkAuth_LimitOf60FlagsAnonymous(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rate_limit", rateLimitHandler(t, 60, 12))
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	p := &Poller{
		client: newTestClient(t, server),
		logger: zerolog.New(&buf).Level(zerolog.TraceLevel),
	}
	p.checkAuth(context.Background())

	var out map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out),
		"expected single JSON log line, got %q", buf.String())

	assert.Equal(t, "error", out["level"])
	assert.Equal(t, "anonymous", out["auth_state"])
	assert.EqualValues(t, 60, out["core_limit"])
	assert.Contains(t, out["message"], "treating requests as anonymous")
}

func TestPoller_checkAuth_AuthenticatedLimitLogsDebug(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rate_limit", rateLimitHandler(t, 5000, 4998))
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	p := &Poller{
		client: newTestClient(t, server),
		logger: zerolog.New(&buf).Level(zerolog.TraceLevel),
	}
	p.checkAuth(context.Background())

	var out map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out),
		"expected single JSON log line, got %q", buf.String())

	assert.Equal(t, "debug", out["level"])
	assert.Equal(t, "authenticated", out["auth_state"])
	assert.EqualValues(t, 5000, out["core_limit"])
}
