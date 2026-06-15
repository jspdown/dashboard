package pullrequest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/api/pkg/config"
)

// testUIConfig is the config the handler advertises in tests. None of
// the handler tests inspect the values, but the constructor needs one.
func testUIConfig() UIConfig {
	return UIConfig{
		StaleAfterDays:     5,
		RecentlyMergedDays: 7,
		Review: UIReviewConfig{
			DefaultRequiredReviewers: 2,
			IgnoreLabels:             []string{"area/webui"},
			ReviewerOverrides: []config.ReviewerOverride{
				{Label: "bot/light-review", Reviewers: 1},
			},
		},
	}
}

type fakeService struct {
	listCalls       []ListOpts
	listResult      []PullRequestView
	listErr         error
	markViewedCalls []int64
	markViewedErr   error
}

func (f *fakeService) List(_ context.Context, opts ListOpts) ([]PullRequestView, error) {
	f.listCalls = append(f.listCalls, opts)
	return f.listResult, f.listErr
}

func (f *fakeService) MarkViewed(_ context.Context, githubID int64) error {
	f.markViewedCalls = append(f.markViewedCalls, githubID)
	return f.markViewedErr
}

type fakePoller struct {
	calls []string
	err   error
}

func (f *fakePoller) RunOnce(_ context.Context, repoSlug string) error {
	f.calls = append(f.calls, repoSlug)
	return f.err
}

func newRouter(svc Service) http.Handler {
	return newRouterWith(svc, &fakePoller{})
}

func newRouterWith(svc Service, poller PollTrigger) http.Handler {
	r := chi.NewRouter()
	NewHandler(svc, testUIConfig(), poller, zerolog.Nop()).Routes(r)
	return r
}

func TestHandler_Config(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got UIConfig
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, testUIConfig(), got)
}

func TestHandler_Poll(t *testing.T) {
	t.Run("triggers poller and returns 204", func(t *testing.T) {
		poller := &fakePoller{}
		req := httptest.NewRequest(http.MethodPost, "/poll/acme/widget", nil)
		rec := httptest.NewRecorder()
		newRouterWith(&fakeService{}, poller).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Equal(t, []string{"acme/widget"}, poller.calls)
	})

	t.Run("repo not configured surfaces as 404", func(t *testing.T) {
		poller := &fakePoller{err: errors.New(`repo "ghost/repo" is not in the poller's configured repos`)}
		req := httptest.NewRequest(http.MethodPost, "/poll/ghost/repo", nil)
		rec := httptest.NewRecorder()
		newRouterWith(&fakeService{}, poller).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("poller error surfaces as 500", func(t *testing.T) {
		poller := &fakePoller{err: errors.New("github went sideways")}
		req := httptest.NewRequest(http.MethodPost, "/poll/acme/widget", nil)
		rec := httptest.NewRecorder()
		newRouterWith(&fakeService{}, poller).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("nil poller responds 503", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/poll/acme/widget", nil)
		rec := httptest.NewRecorder()
		newRouterWith(&fakeService{}, nil).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}

func TestHandler_List(t *testing.T) {
	canned := []PullRequestView{
		{ID: 99, Group: "review", Title: "stub", Repo: "x/y", Num: 1, Author: "a", CI: "failing", Blocking: []string{}},
	}
	svc := &fakeService{listResult: canned}

	req := httptest.NewRequest(http.MethodGet, "/prs?filter=ci%20failing&sort=age", nil)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got []PullRequestView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, canned, got)

	require.Len(t, svc.listCalls, 1)
	assert.Equal(t, ListOpts{Filter: "ci failing", Sort: "age"}, svc.listCalls[0])
}

func TestHandler_List_NoQueryParams(t *testing.T) {
	svc := &fakeService{listResult: []PullRequestView{}}

	req := httptest.NewRequest(http.MethodGet, "/prs", nil)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, svc.listCalls, 1)
	assert.Equal(t, ListOpts{}, svc.listCalls[0])
}

func TestHandler_List_ServiceError(t *testing.T) {
	svc := &fakeService{listErr: errors.New("boom")}

	req := httptest.NewRequest(http.MethodGet, "/prs", nil)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandler_MarkViewed(t *testing.T) {
	svc := &fakeService{}

	req := httptest.NewRequest(http.MethodPost, "/prs/4242/viewed", nil)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, []int64{4242}, svc.markViewedCalls)
}

func TestHandler_MarkViewed_BadID(t *testing.T) {
	svc := &fakeService{}

	req := httptest.NewRequest(http.MethodPost, "/prs/not-a-number/viewed", nil)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, svc.markViewedCalls)
}

func TestHandler_MarkViewed_ServiceError(t *testing.T) {
	svc := &fakeService{markViewedErr: errors.New("boom")}

	req := httptest.NewRequest(http.MethodPost, "/prs/1/viewed", nil)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, []int64{1}, svc.markViewedCalls)
}
