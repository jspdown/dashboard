package pullrequest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/jspdown/dashboard/api/pkg/auth"
)

// SettingsStore stores a user's repositories and rule profiles.
type SettingsStore interface {
	ListRepos(ctx context.Context, userLogin string) ([]string, error)
	HasRepo(ctx context.Context, userLogin, repo string) (bool, error)
	AddRepo(ctx context.Context, userLogin, repo string) error
	RemoveRepo(ctx context.Context, userLogin, repo string) error
	Suggestions(ctx context.Context, userLogin string) ([]RepoSuggestion, error)
	ListProfiles(ctx context.Context, userLogin string) ([]RuleProfile, error)
	CreateProfile(ctx context.Context, userLogin string, p RuleProfile) (RuleProfile, error)
	UpdateProfile(ctx context.Context, userLogin string, p RuleProfile) error
	DeleteProfile(ctx context.Context, userLogin string, id int64) error
}

// RepoOverviewer gives an overview of the repositories.
type RepoOverviewer interface {
	RepoOverview(ctx context.Context) ([]RepoView, error)
}

// RepoVerifier verifies that we can use the given repository.
type RepoVerifier interface {
	Verify(ctx context.Context, slug string) error
}

// RepoPoller lets the handler poke the poller after a subscription has changed, so the
// new repo starts polling without waiting for the next reconciliation tick.
type RepoPoller interface {
	RunOnce(ctx context.Context, slug string) error
	Nudge()
}

// SettingsHandler serves the per-user Repositories and Review rules screens.
type SettingsHandler struct {
	store    SettingsStore
	overview RepoOverviewer
	verifier RepoVerifier
	poller   RepoPoller
	logger   zerolog.Logger
}

func NewSettingsHandler(store SettingsStore, overview RepoOverviewer, verifier RepoVerifier, poller RepoPoller, logger zerolog.Logger) *SettingsHandler {
	return &SettingsHandler{store: store, overview: overview, verifier: verifier, poller: poller, logger: logger}
}

func (h *SettingsHandler) Routes(r chi.Router) {
	r.Get("/settings/repos", h.listRepos)
	r.Post("/settings/repos", h.addRepo)
	r.Get("/settings/repos/suggestions", h.suggestions)
	r.Delete("/settings/repos/{owner}/{repo}", h.removeRepo)
	r.Post("/settings/repos/{owner}/{repo}/recheck", h.recheckRepo)
	r.Get("/settings/profiles", h.listProfiles)
	r.Post("/settings/profiles", h.createProfile)
	r.Put("/settings/profiles/{id}", h.updateProfile)
	r.Delete("/settings/profiles/{id}", h.deleteProfile)
}

func (h *SettingsHandler) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := h.overview.RepoOverview(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("Repo overview")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, repos)
}

func (h *SettingsHandler) suggestions(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	suggs, err := h.store.Suggestions(r.Context(), login)
	if err != nil {
		h.logger.Error().Err(err).Msg("Repo suggestions")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, suggs)
}

type addRepoRequest struct {
	Repo string `json:"repo"`
}

func (h *SettingsHandler) addRepo(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body addRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	slug, err := normalizeRepoSlug(body.Repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	exists, err := h.store.HasRepo(r.Context(), login, slug)
	if err != nil {
		h.logger.Error().Err(err).Msg("Check repo")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, "already observing "+slug, http.StatusConflict)
		return
	}

	if err := h.verifier.Verify(r.Context(), slug); err != nil {
		if errors.Is(err, ErrRepoInaccessible) {
			http.Error(w, "Repository not found or you do not have access. Check the spelling of owner/repo.", http.StatusUnprocessableEntity)
			return
		}
		h.logger.Error().Err(err).Str("repo", slug).Msg("Verify repo")
		http.Error(w, "could not verify repository access", http.StatusBadGateway)
		return
	}

	if err := h.store.AddRepo(r.Context(), login, slug); err != nil {
		h.logger.Error().Err(err).Str("repo", slug).Msg("Add repo")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Nudge the poller so the repo starts polling now rather than at the next
	// reconcile tick, matching the UI's "PRs appear within a few seconds".
	h.poller.Nudge()

	writeJSON(w, h.logger, http.StatusCreated, addRepoRequest{Repo: slug})
}

func (h *SettingsHandler) removeRepo(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	slug := h.repoSlug(r)
	if err := h.store.RemoveRepo(r.Context(), login, slug); err != nil {
		h.logger.Error().Err(err).Str("repo", slug).Msg("Remove repo")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// The repo may now have no subscribers; let the poller reconcile it away.
	h.poller.Nudge()
	w.WriteHeader(http.StatusNoContent)
}

func (h *SettingsHandler) recheckRepo(w http.ResponseWriter, r *http.Request) {
	slug := h.repoSlug(r)
	if err := h.verifier.Verify(r.Context(), slug); err != nil {
		if errors.Is(err, ErrRepoInaccessible) {
			http.Error(w, "still no access to "+slug, http.StatusUnprocessableEntity)
			return
		}
		h.logger.Error().Err(err).Str("repo", slug).Msg("Recheck repo")
		http.Error(w, "could not verify repository access", http.StatusBadGateway)
		return
	}
	h.poller.Nudge()
	w.WriteHeader(http.StatusNoContent)
}

// ProfileView is the wire shape of a rule profile: a named, self-contained
// review policy scoped to a set of repos or, when all_repos is set, every
// observed repo no specific profile claims.
type ProfileView struct {
	ID                       int64              `json:"id"`
	Name                     string             `json:"name"`
	AllRepos                 bool               `json:"all_repos"`
	Repos                    []string           `json:"repos"`
	DefaultRequiredReviewers int                `json:"default_required_reviewers"`
	StaleAfterDays           int                `json:"stale_after_days"`
	RecentlyMergedDays       int                `json:"recently_merged_days"`
	IgnoreLabels             []string           `json:"ignore_labels"`
	BotAuthors               []string           `json:"bot_authors"`
	ReviewerOverrides        []ReviewerOverride `json:"reviewer_overrides"`
}

func profileView(p RuleProfile) ProfileView {
	return ProfileView{
		ID:                       p.ID,
		Name:                     p.Name,
		AllRepos:                 p.AllRepos,
		Repos:                    nonNil(p.Repos),
		DefaultRequiredReviewers: p.DefaultRequiredReviewers,
		StaleAfterDays:           p.StaleAfterDays,
		RecentlyMergedDays:       p.RecentlyMergedDays,
		IgnoreLabels:             nonNil(p.IgnoreLabels),
		BotAuthors:               nonNil(p.BotAuthors),
		ReviewerOverrides:        nonNilOverrides(p.ReviewerOverrides),
	}
}

// profileViewToProfile validates a submitted profile and converts it to a
// RuleProfile. Out-of-range values are rejected rather than clamped. An
// all-repos profile carries no explicit repo list.
func profileViewToProfile(v ProfileView) (RuleProfile, error) {
	name := strings.TrimSpace(v.Name)
	if name == "" {
		return RuleProfile{}, errors.New("profile name is required")
	}
	if v.DefaultRequiredReviewers < 0 || v.DefaultRequiredReviewers > MaxRequiredReviewers {
		return RuleProfile{}, errors.New("default_required_reviewers out of range")
	}
	if v.StaleAfterDays < MinDays || v.StaleAfterDays > MaxStaleAfterDays {
		return RuleProfile{}, errors.New("stale_after_days out of range")
	}
	if v.RecentlyMergedDays < MinDays || v.RecentlyMergedDays > MaxRecentlyMergedDays {
		return RuleProfile{}, errors.New("recently_merged_days out of range")
	}

	overrides, err := validateReviewerOverrides(v.ReviewerOverrides)
	if err != nil {
		return RuleProfile{}, err
	}

	var repos []string
	if !v.AllRepos {
		repos, err = validateRepos(v.Repos)
		if err != nil {
			return RuleProfile{}, err
		}
	}

	return RuleProfile{
		ID:       v.ID,
		Name:     name,
		AllRepos: v.AllRepos,
		Repos:    repos,
		ReviewSettings: ReviewSettings{
			DefaultRequiredReviewers: v.DefaultRequiredReviewers,
			StaleAfterDays:           v.StaleAfterDays,
			RecentlyMergedDays:       v.RecentlyMergedDays,
			IgnoreLabels:             cleanStrings(v.IgnoreLabels),
			BotAuthors:               cleanStrings(v.BotAuthors),
			ReviewerOverrides:        overrides,
		},
	}, nil
}

// validateRepos normalizes each owner/name slug and dedupes, preserving order.
func validateRepos(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		slug, err := normalizeRepoSlug(raw)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out, nil
}

func (h *SettingsHandler) listProfiles(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	profiles, err := h.store.ListProfiles(r.Context(), login)
	if err != nil {
		h.logger.Error().Err(err).Msg("List profiles")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]ProfileView, len(profiles))
	for i, p := range profiles {
		out[i] = profileView(p)
	}
	writeJSON(w, h.logger, http.StatusOK, out)
}

func (h *SettingsHandler) createProfile(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body ProfileView
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	profile, err := profileViewToProfile(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	created, err := h.store.CreateProfile(r.Context(), login, profile)
	if err != nil {
		h.writeProfileError(w, err, "Create profile")
		return
	}
	writeJSON(w, h.logger, http.StatusCreated, profileView(created))
}

func (h *SettingsHandler) updateProfile(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid profile id", http.StatusBadRequest)
		return
	}
	var body ProfileView
	if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	profile, err := profileViewToProfile(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	profile.ID = id
	if err := h.store.UpdateProfile(r.Context(), login, profile); err != nil {
		h.writeProfileError(w, err, "Update profile")
		return
	}
	writeJSON(w, h.logger, http.StatusOK, profileView(profile))
}

func (h *SettingsHandler) deleteProfile(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid profile id", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteProfile(r.Context(), login, id); err != nil {
		h.writeProfileError(w, err, "Delete profile")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeProfileError maps the profile store's sentinel errors to client statuses
// and logs anything unexpected as a 500.
func (h *SettingsHandler) writeProfileError(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, ErrProfileNotFound):
		http.Error(w, "profile not found", http.StatusNotFound)
	case errors.Is(err, ErrDuplicateCatchAll):
		http.Error(w, "an all-repositories profile already exists", http.StatusConflict)
	case errors.Is(err, ErrRepoProfileConflict):
		http.Error(w, "a repository is already in another profile", http.StatusConflict)
	default:
		h.logger.Error().Err(err).Msg(msg)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// validateReviewerOverrides trims labels, rejects blanks and out-of-range
// counts, and dedupes by label preserving order.
func validateReviewerOverrides(in []ReviewerOverride) ([]ReviewerOverride, error) {
	out := make([]ReviewerOverride, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, o := range in {
		label := strings.TrimSpace(o.Label)
		if label == "" {
			return nil, errors.New("reviewer override label is required")
		}
		if o.Reviewers < 0 || o.Reviewers > MaxRequiredReviewers {
			return nil, errors.New("reviewer override count out of range")
		}
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, ReviewerOverride{Label: label, Reviewers: o.Reviewers})
	}
	return out, nil
}

func (h *SettingsHandler) repoSlug(r *http.Request) string {
	return chi.URLParam(r, "owner") + "/" + chi.URLParam(r, "repo")
}

// nonNilOverrides returns an empty slice for nil so JSON encodes "[]".
func nonNilOverrides(in []ReviewerOverride) []ReviewerOverride {
	if in == nil {
		return []ReviewerOverride{}
	}
	return in
}

// normalizeRepoSlug trims and validates an owner/name slug.
func normalizeRepoSlug(raw string) (string, error) {
	slug := strings.TrimSpace(raw)
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" {
		return "", errors.New("repo must be in owner/name form")
	}
	return slug, nil
}

// cleanStrings trims, drops empties, and dedupes while preserving order.
func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// viewer pulls the authenticated login from context, writing a 500 if the auth
// middleware isn't installed (which would be a wiring bug, not a client error).
func (h *SettingsHandler) viewer(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		http.Error(w, "no user in context", http.StatusInternalServerError)
		return "", false
	}
	return u.Login, true
}

func writeJSON(w http.ResponseWriter, logger zerolog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error().Err(err).Msg("Encode response")
	}
}
