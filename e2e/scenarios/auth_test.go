package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/e2e/internal/e2e"
)

// auth_test.go covers the dashboard under the trusted-header auth
// model. The dashboard doesn't run the OAuth dance itself; the harness
// simulates oauth2-proxy by stamping X-Forwarded-User on every request
// reaching the api. These tests assert read state is scoped per user
// and that the TopBar renders the avatar UI from /api/me.
//
// Unit coverage of the middleware lives in
// pkg/auth/middleware_test.go; the real-oauth2-proxy flow lives in
// auth_integration_test.go.

func drainAndClose(t *testing.T, body io.ReadCloser) {
	t.Helper()
	_, _ = io.Copy(io.Discard, body)
	if err := body.Close(); err != nil {
		t.Logf("auth_test: close body: %v", err)
	}
}

func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// TestMarkViewed_scoped_per_user is the core data-isolation check: the
// view row from mark-viewed is keyed to the logged-in user, not global.
// The PR is viewer-authored so it lands in "mine" where Unread matters.
func TestMarkViewed_scoped_per_user(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t, e2e.WithViewer("alice"), e2e.WithRepo("acme/widget"))
	pool := openPool(t, h.DSN)

	h.Fake.Repo("acme/widget").PR(1).Author("alice").Open()
	h.Poll("acme/widget")

	before := fetchPRs(t, h)
	require.Len(t, before, 1)
	assert.True(t, before[0].Unread, "newly-ingested PR must be unread for the author until they open it")

	markReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		h.URL+"/api/prs/"+prIDFor("acme/widget", 1)+"/viewed", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(markReq)
	require.NoError(t, err)
	drainAndClose(t, resp.Body)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	after := fetchPRs(t, h)
	require.Len(t, after, 1)
	assert.False(t, after[0].Unread, "after mark-viewed, PR must not be unread")

	var rows int
	err = pool.QueryRow(context.Background(),
		"SELECT count(*) FROM pull_request_views WHERE user_login = $1", "alice").Scan(&rows)
	require.NoError(t, err)
	assert.Equal(t, 1, rows, "exactly one view row, scoped to alice")
}

// TestNewUser_seesAllUnread is the wall-of-unread baseline: a fresh
// user with no view rows sees every "mine"/"reviewed" PR as unread.
// Review-queue PRs don't get the Unread flag (the UX only flags PRs the
// viewer has already engaged with).
func TestNewUser_seesAllUnread(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t, e2e.WithViewer("alice"), e2e.WithRepo("acme/widget"))

	h.Fake.Repo("acme/widget").PR(10).Author("alice").Open()
	h.Fake.Repo("acme/widget").PR(11).Author("alice").Open()
	h.Fake.Repo("acme/widget").PR(12).Author("alice").Open()
	h.Poll("acme/widget")

	prs := fetchPRs(t, h)
	require.Len(t, prs, 3)
	for _, pr := range prs {
		assert.True(t, pr.Unread, "PR %d should be unread for a brand-new user", pr.Num)
	}
}

// TestUserMenu_avatarVisible is a chromedp UI check: after the
// harness injects X-Forwarded-User and the SPA fetches /api/me, the
// TopBar renders the user-menu avatar trigger.
func TestUserMenu_avatarVisible(t *testing.T) {
	t.Parallel()
	h := e2e.Start(t, e2e.WithViewer("alice"))

	trigger := h.Browser.QueryAttribute(`button[aria-haspopup="true"]`, "aria-expanded")
	assert.Equal(t, "false", trigger)
}

// prIDFor returns the GitHub ID the fake server assigns to a PR
// (mirrors githubtest.stableID for "pr"). Used to build the
// POST /api/prs/{id}/viewed URL without exposing the fake's wire
// helper.
func prIDFor(_ string, number int) string {
	const seedKind = "pr"
	var seed int64
	for _, b := range []byte(seedKind) {
		seed = seed*131 + int64(b)
	}
	return strconv.FormatInt(seed*1000+int64(number), 10)
}

type prListItem struct {
	Num    int  `json:"num"`
	Unread bool `json:"unread"`
}

func fetchPRs(t *testing.T, h *e2e.Harness) []prListItem {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, h.URL+"/api/prs", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer drainAndClose(t, resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out []prListItem
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}
