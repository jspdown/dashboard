package pullrequest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/api/pkg/auth"
)

type fakeSettingsStore struct {
	repos       []string
	has         bool
	suggestions []RepoSuggestion
	profiles    []RuleProfile

	createErr error
	updateErr error
	deleteErr error

	added   []string
	removed []string
	created *RuleProfile
	updated *RuleProfile
	deleted []int64
}

func (f *fakeSettingsStore) ListRepos(context.Context, string) ([]string, error) { return f.repos, nil }
func (f *fakeSettingsStore) HasRepo(context.Context, string, string) (bool, error) {
	return f.has, nil
}
func (f *fakeSettingsStore) AddRepo(_ context.Context, _ string, repo string) error {
	f.added = append(f.added, repo)
	return nil
}
func (f *fakeSettingsStore) RemoveRepo(_ context.Context, _ string, repo string) error {
	f.removed = append(f.removed, repo)
	return nil
}
func (f *fakeSettingsStore) Suggestions(context.Context, string) ([]RepoSuggestion, error) {
	return f.suggestions, nil
}
func (f *fakeSettingsStore) ListProfiles(context.Context, string) ([]RuleProfile, error) {
	return f.profiles, nil
}
func (f *fakeSettingsStore) CreateProfile(_ context.Context, _ string, p RuleProfile) (RuleProfile, error) {
	if f.createErr != nil {
		return RuleProfile{}, f.createErr
	}
	f.created = &p
	p.ID = 1
	return p, nil
}
func (f *fakeSettingsStore) UpdateProfile(_ context.Context, _ string, p RuleProfile) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = &p
	return nil
}
func (f *fakeSettingsStore) DeleteProfile(_ context.Context, _ string, id int64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeOverview struct{ repos []RepoView }

func (f *fakeOverview) RepoOverview(context.Context) ([]RepoView, error) { return f.repos, nil }

type fakeVerifier struct{ err error }

func (f *fakeVerifier) Verify(context.Context, string) error { return f.err }

type fakeRepoPoller struct {
	nudges int
	runs   []string
}

func (f *fakeRepoPoller) RunOnce(_ context.Context, slug string) error {
	f.runs = append(f.runs, slug)
	return nil
}
func (f *fakeRepoPoller) Nudge() { f.nudges++ }

func newSettingsRouter(h *SettingsHandler) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), auth.User{Login: "alex"})))
		})
	})
	h.Routes(r)
	return r
}

func newSettingsHandler(store SettingsStore, overview RepoOverviewer, verifier RepoVerifier, poller RepoPoller) *SettingsHandler {
	return NewSettingsHandler(store, overview, verifier, poller, zerolog.Nop())
}

func TestSettings_ListRepos(t *testing.T) {
	overview := &fakeOverview{repos: []RepoView{{Repo: "acme/widget", Health: RepoHealthOK, Profile: "Strict"}}}
	h := newSettingsHandler(&fakeSettingsStore{}, overview, &fakeVerifier{}, &fakeRepoPoller{})

	rec := httptest.NewRecorder()
	newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/repos", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var got []RepoView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "acme/widget", got[0].Repo)
	assert.Equal(t, RepoHealthOK, got[0].Health)
	assert.Equal(t, "Strict", got[0].Profile)
}

func TestSettings_AddRepo(t *testing.T) {
	t.Run("success verifies, stores, and nudges the poller", func(t *testing.T) {
		store := &fakeSettingsStore{}
		poller := &fakeRepoPoller{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, poller)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos", strings.NewReader(`{"repo":"acme/widget"}`))
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, []string{"acme/widget"}, store.added)
		assert.Equal(t, 1, poller.nudges)
	})

	t.Run("malformed slug is rejected", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos", strings.NewReader(`{"repo":"nopath"}`))
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, store.added)
	})

	t.Run("already observing returns 409", func(t *testing.T) {
		store := &fakeSettingsStore{has: true}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos", strings.NewReader(`{"repo":"acme/widget"}`))
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Empty(t, store.added)
	})

	t.Run("inaccessible repo returns 422 and is not stored", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{err: ErrRepoInaccessible}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos", strings.NewReader(`{"repo":"acme/ghost"}`))
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Empty(t, store.added)
	})

	t.Run("verifier outage returns 502", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{err: errors.New("github down")}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos", strings.NewReader(`{"repo":"acme/widget"}`))
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadGateway, rec.Code)
		assert.Empty(t, store.added)
	})
}

func TestSettings_RemoveRepo(t *testing.T) {
	store := &fakeSettingsStore{}
	poller := &fakeRepoPoller{}
	h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, poller)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/settings/repos/acme/widget", nil)
	newSettingsRouter(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, []string{"acme/widget"}, store.removed)
	assert.Equal(t, 1, poller.nudges)
}

func TestSettings_ListProfiles(t *testing.T) {
	store := &fakeSettingsStore{profiles: []RuleProfile{
		{ID: 7, Name: "Strict", Repos: []string{"acme/widget"}, ReviewSettings: ReviewSettings{
			DefaultRequiredReviewers: 3, StaleAfterDays: 4, RecentlyMergedDays: 9,
		}},
	}}
	h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

	rec := httptest.NewRecorder()
	newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/profiles", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var got []ProfileView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, int64(7), got[0].ID)
	assert.Equal(t, "Strict", got[0].Name)
	assert.Equal(t, []string{"acme/widget"}, got[0].Repos)
	assert.Equal(t, 3, got[0].DefaultRequiredReviewers)
	// nil slices encode as [] for the client.
	assert.Equal(t, []ReviewerOverride{}, got[0].ReviewerOverrides)
	assert.Equal(t, []string{}, got[0].BotAuthors)
}

func TestSettings_CreateProfile(t *testing.T) {
	const valid = `{"name":"Strict","all_repos":false,"repos":["acme/widget","acme/widget"],
		"default_required_reviewers":3,"stale_after_days":4,"recently_merged_days":9,
		"ignore_labels":["wip","wip"," "],"bot_authors":["renovate[bot]"],
		"reviewer_overrides":[{"label":"hotfix","reviewers":1}]}`

	t.Run("valid profile is cleaned, saved, and echoed with an id", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/profiles", strings.NewReader(valid)))

		require.Equal(t, http.StatusCreated, rec.Code)
		require.NotNil(t, store.created)
		// Duplicate repos and blank/duplicate labels are cleaned before persisting.
		assert.Equal(t, []string{"acme/widget"}, store.created.Repos)
		assert.Equal(t, []string{"wip"}, store.created.IgnoreLabels)
		assert.Equal(t, []ReviewerOverride{{Label: "hotfix", Reviewers: 1}}, store.created.ReviewerOverrides)

		var got ProfileView
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, int64(1), got.ID)
	})

	t.Run("all-repos profile drops any submitted repo list", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		body := `{"name":"Everything","all_repos":true,"repos":["acme/widget"],
			"default_required_reviewers":2,"stale_after_days":5,"recently_merged_days":7}`
		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/profiles", strings.NewReader(body)))

		require.Equal(t, http.StatusCreated, rec.Code)
		require.NotNil(t, store.created)
		assert.True(t, store.created.AllRepos)
		assert.Empty(t, store.created.Repos)
	})

	t.Run("missing name is rejected", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		body := `{"name":"  ","default_required_reviewers":2,"stale_after_days":5,"recently_merged_days":7}`
		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/profiles", strings.NewReader(body)))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Nil(t, store.created)
	})

	t.Run("out-of-range stale window is rejected", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		body := `{"name":"X","default_required_reviewers":2,"stale_after_days":999,"recently_merged_days":7}`
		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/profiles", strings.NewReader(body)))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Nil(t, store.created)
	})

	t.Run("malformed repo slug is rejected", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		body := `{"name":"X","repos":["nopath"],"default_required_reviewers":2,"stale_after_days":5,"recently_merged_days":7}`
		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/profiles", strings.NewReader(body)))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Nil(t, store.created)
	})

	t.Run("duplicate catch-all surfaces as 409", func(t *testing.T) {
		store := &fakeSettingsStore{createErr: ErrDuplicateCatchAll}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		body := `{"name":"Everything","all_repos":true,"default_required_reviewers":2,"stale_after_days":5,"recently_merged_days":7}`
		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/profiles", strings.NewReader(body)))

		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("repo already in another profile surfaces as 409", func(t *testing.T) {
		store := &fakeSettingsStore{createErr: ErrRepoProfileConflict}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		body := `{"name":"Strict","repos":["acme/widget"],"default_required_reviewers":2,"stale_after_days":5,"recently_merged_days":7}`
		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/profiles", strings.NewReader(body)))

		assert.Equal(t, http.StatusConflict, rec.Code)
	})
}

func TestSettings_UpdateProfile(t *testing.T) {
	const valid = `{"name":"Strict","repos":["acme/widget"],
		"default_required_reviewers":3,"stale_after_days":4,"recently_merged_days":9}`

	t.Run("valid update is saved against the path id", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings/profiles/42", strings.NewReader(valid)))

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, store.updated)
		assert.Equal(t, int64(42), store.updated.ID)
		assert.Equal(t, "Strict", store.updated.Name)
	})

	t.Run("non-numeric id is rejected", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings/profiles/abc", strings.NewReader(valid)))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Nil(t, store.updated)
	})

	t.Run("unknown profile returns 404", func(t *testing.T) {
		store := &fakeSettingsStore{updateErr: ErrProfileNotFound}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings/profiles/99", strings.NewReader(valid)))

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("repo conflict returns 409", func(t *testing.T) {
		store := &fakeSettingsStore{updateErr: ErrRepoProfileConflict}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/settings/profiles/99", strings.NewReader(valid)))

		assert.Equal(t, http.StatusConflict, rec.Code)
	})
}

func TestSettings_DeleteProfile(t *testing.T) {
	t.Run("known profile is deleted", func(t *testing.T) {
		store := &fakeSettingsStore{}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/settings/profiles/5", nil))

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Equal(t, []int64{5}, store.deleted)
	})

	t.Run("unknown profile returns 404", func(t *testing.T) {
		store := &fakeSettingsStore{deleteErr: ErrProfileNotFound}
		h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

		rec := httptest.NewRecorder()
		newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/settings/profiles/5", nil))

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestSettings_RecheckRepo(t *testing.T) {
	t.Run("regained access nudges the poller and returns 204", func(t *testing.T) {
		poller := &fakeRepoPoller{}
		h := newSettingsHandler(&fakeSettingsStore{}, &fakeOverview{}, &fakeVerifier{}, poller)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos/acme/widget/recheck", nil)
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Equal(t, 1, poller.nudges)
	})

	t.Run("still inaccessible returns 422 and does not nudge", func(t *testing.T) {
		poller := &fakeRepoPoller{}
		h := newSettingsHandler(&fakeSettingsStore{}, &fakeOverview{}, &fakeVerifier{err: ErrRepoInaccessible}, poller)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos/acme/widget/recheck", nil)
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, 0, poller.nudges)
	})

	t.Run("verifier outage returns 502 and does not nudge", func(t *testing.T) {
		poller := &fakeRepoPoller{}
		h := newSettingsHandler(&fakeSettingsStore{}, &fakeOverview{}, &fakeVerifier{err: errors.New("github down")}, poller)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/repos/acme/widget/recheck", nil)
		newSettingsRouter(h).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadGateway, rec.Code)
		assert.Equal(t, 0, poller.nudges)
	})
}

func TestSettings_Suggestions(t *testing.T) {
	store := &fakeSettingsStore{suggestions: []RepoSuggestion{{Repo: "acme/gadget", Observers: []string{"lou"}}}}
	h := newSettingsHandler(store, &fakeOverview{}, &fakeVerifier{}, &fakeRepoPoller{})

	rec := httptest.NewRecorder()
	newSettingsRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/repos/suggestions", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var got []RepoSuggestion
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "acme/gadget", got[0].Repo)
}
