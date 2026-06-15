package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// fakeStaleSource records calls and returns canned numbers and snapshots.
type fakeStaleSource struct {
	numbers   []int
	err       error
	snapshots map[int]pullrequest.PullRequestSnapshot
	snapErr   error

	mu    sync.Mutex
	calls []struct {
		repo    string
		version int
	}
	snapshotCalls []struct {
		repo   string
		number int
	}
}

func (f *fakeStaleSource) ListStaleIngestNumbers(_ context.Context, repo string, currentVersion int) ([]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		repo    string
		version int
	}{repo, currentVersion})
	return f.numbers, f.err
}

func (f *fakeStaleSource) GetSnapshotByNumber(_ context.Context, repo string, number int) (pullrequest.PullRequestSnapshot, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshotCalls = append(f.snapshotCalls, struct {
		repo   string
		number int
	}{repo, number})
	if f.snapErr != nil {
		return pullrequest.PullRequestSnapshot{}, false, f.snapErr
	}
	snap, ok := f.snapshots[number]
	return snap, ok, nil
}

// recordingApplier counts ingester invocations so backfill tests can assert
// the apply path was driven without standing up a real Ingester.
type recordingApplier struct {
	mu     sync.Mutex
	events []any
}

func (r *recordingApplier) Apply(_ context.Context, event any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

// pollerWithBuf builds a Poller wired to the test server with its logger
// writing into buf, ready for backfillStale to drive.
func pollerWithBuf(t *testing.T, server *httptest.Server, buf *bytes.Buffer, prs staleSource, apl applier) *Poller {
	t.Helper()
	return &Poller{
		client:   newTestClient(t, server),
		ingester: apl,
		prs:      prs,
		logger:   zerolog.New(buf).Level(zerolog.TraceLevel),
	}
}

func decodeLogs(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var m map[string]any
		require.NoError(t, dec.Decode(&m))
		out = append(out, m)
	}
	return out
}

// fullPRJSON returns the JSON body for a single PR fetch (the "full" shape
// after Get). Number is the only field the rest of the apply pipeline reads
// for downstream calls (reviews and check-runs).
func fullPRJSON(number int, headSHA string) map[string]any {
	return map[string]any{
		"id":         int64(1000 + number),
		"node_id":    fmt.Sprintf("PR_%d", number),
		"number":     number,
		"state":      "open",
		"title":      fmt.Sprintf("pr %d", number),
		"draft":      false,
		"user":       map[string]any{"login": "alice"},
		"head":       map[string]any{"sha": headSHA},
		"base":       map[string]any{"sha": "base"},
		"created_at": time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"updated_at": time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
}

func TestPoller_backfillStale_RefetchesStaleNumbers(t *testing.T) {
	mux := http.NewServeMux()
	var fetched sync.Map
	mux.HandleFunc("/repos/acme/widget/pulls/", func(w http.ResponseWriter, r *http.Request) {
		// Path looks like /repos/acme/widget/pulls/{n} or /pulls/{n}/reviews.
		// All inner endpoints respond with empty arrays except the PR itself.
		switch r.URL.Path {
		case "/repos/acme/widget/pulls/42":
			fetched.Store(42, true)
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(fullPRJSON(42, "sha42")))
		case "/repos/acme/widget/pulls/99":
			fetched.Store(99, true)
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(fullPRJSON(99, "sha99")))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}
	})
	mux.HandleFunc("/repos/acme/widget/commits/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stale := &fakeStaleSource{numbers: []int{42, 99}}
	apl := &recordingApplier{}
	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, stale, apl)
	p.backfillStale(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"))

	_, ok42 := fetched.Load(42)
	_, ok99 := fetched.Load(99)
	assert.True(t, ok42, "PR 42 was fetched")
	assert.True(t, ok99, "PR 99 was fetched")
	require.Len(t, stale.calls, 1)
	assert.Equal(t, "acme/widget", stale.calls[0].repo)
	assert.Equal(t, IngestVersion, stale.calls[0].version)
	assert.Len(t, apl.events, 2, "ingester saw one PullRequestEvent per stale PR")

	logs := decodeLogs(t, &buf)
	require.NotEmpty(t, logs)
	last := logs[len(logs)-1]
	assert.Equal(t, "info", last["level"])
	assert.EqualValues(t, 2, last["stale_refetched"])
	assert.EqualValues(t, 0, last["stale_failed"])
	assert.EqualValues(t, 0, last["stale_remaining"])
}

func TestPoller_backfillStale_HonorsBudget(t *testing.T) {
	mux := http.NewServeMux()
	var fetched sync.Map
	mux.HandleFunc("/repos/acme/widget/pulls/", func(w http.ResponseWriter, r *http.Request) {
		var n int
		// Path is /repos/acme/widget/pulls/{n} for the Get and /pulls/{n}/reviews for reviews.
		_, err := fmt.Sscanf(r.URL.Path, "/repos/acme/widget/pulls/%d", &n)
		if err == nil && r.URL.Path == fmt.Sprintf("/repos/acme/widget/pulls/%d", n) {
			fetched.Store(n, true)
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(fullPRJSON(n, fmt.Sprintf("sha%d", n))))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("/repos/acme/widget/commits/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	// More numbers than the per-tick budget; only the first staleBudgetPerTick
	// should be fetched in one call.
	numbers := make([]int, staleBudgetPerTick+3)
	for i := range numbers {
		numbers[i] = 100 + i
	}

	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, &fakeStaleSource{numbers: numbers}, &recordingApplier{})
	p.backfillStale(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"))

	got := 0
	fetched.Range(func(_, _ any) bool { got++; return true })
	assert.Equal(t, staleBudgetPerTick, got, "exactly the budget number of PRs were fetched")

	logs := decodeLogs(t, &buf)
	require.NotEmpty(t, logs)
	last := logs[len(logs)-1]
	assert.EqualValues(t, staleBudgetPerTick, last["stale_refetched"])
	assert.EqualValues(t, 3, last["stale_remaining"], "remaining count surfaces the unfinished work")
}

func TestPoller_backfillStale_LogsFailuresWithoutPropagating(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/pulls/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/widget/pulls/7":
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(fullPRJSON(7, "sha7")))
		case "/repos/acme/widget/pulls/8":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}
	})
	mux.HandleFunc("/repos/acme/widget/commits/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, &fakeStaleSource{numbers: []int{7, 8}}, &recordingApplier{})
	p.backfillStale(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"))

	logs := decodeLogs(t, &buf)
	require.NotEmpty(t, logs)
	last := logs[len(logs)-1]
	assert.Equal(t, "info", last["level"])
	assert.EqualValues(t, 1, last["stale_refetched"])
	assert.EqualValues(t, 1, last["stale_failed"])
}

// rollupResp produces a single-page GraphQL handler matching what
// listCheckRollups expects, used by refreshDriftedRollups tests.
func rollupResp(rollups []checkRollup) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		nodes := make([]map[string]any, 0, len(rollups))
		for _, r := range rollups {
			rollup := any(nil)
			if r.RollupState != "" {
				rollup = map[string]any{"state": r.RollupState}
			}
			nodes = append(nodes, map[string]any{
				"number":     r.Number,
				"headRefOid": r.HeadRefOID,
				"commits": map[string]any{"nodes": []map[string]any{
					{"commit": map[string]any{"statusCheckRollup": rollup}},
				}},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"repository": map[string]any{"pullRequests": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes":    nodes,
			}},
		}})
	}
}

// snapshotWithCheck builds a minimal snapshot whose RollupCI maps to
// the given internal CI state (passing/pending/failing/none).
func snapshotWithCheck(headSHA, ci string) pullrequest.PullRequestSnapshot {
	snap := pullrequest.PullRequestSnapshot{
		PullRequest: pullrequest.PullRequest{HeadSHA: headSHA, Status: pullrequest.StatusOpen},
	}
	switch ci {
	case pullrequest.CIPassing:
		conc := "success"
		snap.CheckRuns = []pullrequest.CheckRun{{Name: "ci", RunStatus: "completed", Conclusion: &conc}}
	case pullrequest.CIFailing:
		conc := "failure"
		snap.CheckRuns = []pullrequest.CheckRun{{Name: "ci", RunStatus: "completed", Conclusion: &conc}}
	case pullrequest.CIPending:
		snap.CheckRuns = []pullrequest.CheckRun{{Name: "ci", RunStatus: "in_progress"}}
	case pullrequest.CINone:
		// no checks
	}
	return snap
}

func TestPoller_refreshDriftedRollups_RefetchesOnlyDivergentPRs(t *testing.T) {
	var fetched sync.Map
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", rollupResp([]checkRollup{
		{Number: 1, HeadRefOID: "sha1", RollupState: "SUCCESS"},     // matches DB → skip
		{Number: 2, HeadRefOID: "sha2", RollupState: "SUCCESS"},     // DB pending → refresh
		{Number: 3, HeadRefOID: "sha3", RollupState: "FAILURE"},     // DB success → refresh
		{Number: 4, HeadRefOID: "sha4-new", RollupState: "SUCCESS"}, // SHA mismatch → refresh
	}))
	mux.HandleFunc("/repos/acme/widget/pulls/", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/repos/acme/widget/pulls/%d", &n); err == nil && r.URL.Path == fmt.Sprintf("/repos/acme/widget/pulls/%d", n) {
			fetched.Store(n, true)
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(fullPRJSON(n, fmt.Sprintf("sha%d", n))))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("/repos/acme/widget/commits/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stale := &fakeStaleSource{snapshots: map[int]pullrequest.PullRequestSnapshot{
		1: snapshotWithCheck("sha1", pullrequest.CIPassing),
		2: snapshotWithCheck("sha2", pullrequest.CIPending),
		3: snapshotWithCheck("sha3", pullrequest.CIPassing),
		4: snapshotWithCheck("sha4-old", pullrequest.CIPassing),
	}}
	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, stale, &recordingApplier{})
	p.refreshDriftedRollups(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"), nil)

	_, ok1 := fetched.Load(1)
	_, ok2 := fetched.Load(2)
	_, ok3 := fetched.Load(3)
	_, ok4 := fetched.Load(4)
	assert.False(t, ok1, "matching rollup is not refetched")
	assert.True(t, ok2, "state mismatch is refetched")
	assert.True(t, ok3, "state mismatch is refetched")
	assert.True(t, ok4, "head SHA mismatch is refetched")

	logs := decodeLogs(t, &buf)
	require.NotEmpty(t, logs)
	last := logs[len(logs)-1]
	assert.Equal(t, "info", last["level"])
	assert.EqualValues(t, 4, last["rollup_checked"])
	assert.EqualValues(t, 3, last["rollup_refreshed"])
	assert.EqualValues(t, 0, last["rollup_failed"])
	assert.EqualValues(t, 0, last["rollup_status_only"])
}

func TestPoller_refreshDriftedRollups_HonorsSkipFromCursorBatch(t *testing.T) {
	var fetched sync.Map
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", rollupResp([]checkRollup{
		{Number: 7, HeadRefOID: "sha7", RollupState: "FAILURE"},
		{Number: 8, HeadRefOID: "sha8", RollupState: "FAILURE"},
	}))
	mux.HandleFunc("/repos/acme/widget/pulls/", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/repos/acme/widget/pulls/%d", &n); err == nil && r.URL.Path == fmt.Sprintf("/repos/acme/widget/pulls/%d", n) {
			fetched.Store(n, true)
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(fullPRJSON(n, fmt.Sprintf("sha%d", n))))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("/repos/acme/widget/commits/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stale := &fakeStaleSource{snapshots: map[int]pullrequest.PullRequestSnapshot{
		7: snapshotWithCheck("sha7", pullrequest.CIPassing),
		8: snapshotWithCheck("sha8", pullrequest.CIPassing),
	}}
	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, stale, &recordingApplier{})
	skip := map[int]struct{}{7: {}}
	p.refreshDriftedRollups(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"), skip)

	_, ok7 := fetched.Load(7)
	_, ok8 := fetched.Load(8)
	assert.False(t, ok7, "PR already handled by the cursor batch is not refetched")
	assert.True(t, ok8, "untouched PR is refetched on divergence")

	logs := decodeLogs(t, &buf)
	last := logs[len(logs)-1]
	assert.EqualValues(t, 1, last["rollup_checked"])
	assert.EqualValues(t, 1, last["rollup_refreshed"])
}

func TestPoller_refreshDriftedRollups_StatusOnlyRepoIsSkipped(t *testing.T) {
	var fetched sync.Map
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", rollupResp([]checkRollup{
		// DB has no check runs, GitHub reports SUCCESS, likely a repo
		// using only legacy commit statuses, which we don't ingest.
		{Number: 5, HeadRefOID: "sha5", RollupState: "SUCCESS"},
	}))
	mux.HandleFunc("/repos/acme/widget/pulls/", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/repos/acme/widget/pulls/%d", &n); err == nil && r.URL.Path == fmt.Sprintf("/repos/acme/widget/pulls/%d", n) {
			fetched.Store(n, true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stale := &fakeStaleSource{snapshots: map[int]pullrequest.PullRequestSnapshot{
		5: snapshotWithCheck("sha5", pullrequest.CINone), // no check runs at all
	}}
	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, stale, &recordingApplier{})
	p.refreshDriftedRollups(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"), nil)

	_, ok := fetched.Load(5)
	assert.False(t, ok, "status-only repo skips refetch to avoid endless churn")

	logs := decodeLogs(t, &buf)
	last := logs[len(logs)-1]
	assert.EqualValues(t, 1, last["rollup_status_only"])
	assert.EqualValues(t, 0, last["rollup_refreshed"])
}

func TestPoller_refreshDriftedRollups_GraphQLErrorIsLoggedNotPropagated(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, &fakeStaleSource{}, &recordingApplier{})
	p.refreshDriftedRollups(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"), nil)

	logs := decodeLogs(t, &buf)
	require.NotEmpty(t, logs)
	assert.Contains(t, logs[len(logs)-1]["message"], "Rollup drift fetch failed")
}

func TestPoller_backfillStale_EmptyListNoFetch(t *testing.T) {
	called := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(http.ResponseWriter, *http.Request) {
		called++
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	p := pollerWithBuf(t, server, &buf, &fakeStaleSource{numbers: nil}, &recordingApplier{})
	p.backfillStale(context.Background(), RepoConfig{Repo: "acme/widget"}, "acme", "widget", repoFromSlug("acme/widget"))

	assert.Equal(t, 0, called, "no GitHub requests when stale list is empty")
	logs := decodeLogs(t, &buf)
	require.NotEmpty(t, logs)
	last := logs[len(logs)-1]
	assert.Equal(t, "debug", last["level"], "empty list logs at debug, not info")
}
