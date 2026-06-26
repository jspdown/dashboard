package pullrequest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/jspdown/dashboard/api/pkg/auth"
)

// SettingsStore is the persistence the settings handler needs. *UserStore
// satisfies it; tests stub it.
type SettingsStore interface {
	ListRepos(ctx context.Context, userLogin string) ([]string, error)
	HasRepo(ctx context.Context, userLogin, repo string) (bool, error)
	AddRepo(ctx context.Context, userLogin, repo string) error
	RemoveRepo(ctx context.Context, userLogin, repo string) error
	Suggestions(ctx context.Context, userLogin string) ([]RepoSuggestion, error)
	GetSettings(ctx context.Context, userLogin string) (UserSettings, error)
	SaveSettings(ctx context.Context, userLogin string, us UserSettings) error
}

// RepoOverviewer computes the per-repo rows (health + counts) for the
// Repositories screen. *PostgresService satisfies it.
type RepoOverviewer interface {
	RepoOverview(ctx context.Context) ([]RepoView, error)
}

// RepoVerifier confirms the server PAT can reach a repo before we start polling
// it. github.Verifier satisfies it; it returns ErrRepoInaccessible on a miss.
type RepoVerifier interface {
	Verify(ctx context.Context, slug string) error
}

// RepoPoller lets the handler poke the poller after a subscription change so the
// new repo starts polling (and its first PRs land) without waiting for the next
// reconcile tick. *github.Poller satisfies it.
type RepoPoller interface {
	RunOnce(ctx context.Context, slug string) error
	Nudge()
}

// SettingsHandler serves the per-user Repositories and Review rules screens plus
// the /config endpoint the dashboard reads at boot.
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
	r.Get("/config", h.config)
	r.Get("/settings/repos", h.listRepos)
	r.Post("/settings/repos", h.addRepo)
	r.Get("/settings/repos/suggestions", h.suggestions)
	r.Delete("/settings/repos/{owner}/{repo}", h.removeRepo)
	r.Post("/settings/repos/{owner}/{repo}/recheck", h.recheckRepo)
	r.Get("/settings/rules", h.getRules)
	r.Put("/settings/rules", h.putRules)
}

// UIConfig is the per-user runtime config the dashboard reads at boot for group
// descriptions, stale badges, and the merged-window tooltip.
type UIConfig struct {
	StaleAfterDays     int            `json:"stale_after_days"`
	RecentlyMergedDays int            `json:"recently_merged_days"`
	Review             UIReviewConfig `json:"review"`
}

type UIReviewConfig struct {
	DefaultRequiredReviewers int                `json:"default_required_reviewers"`
	IgnoreLabels             []string           `json:"ignore_labels"`
	ReviewerOverrides        []ReviewerOverride `json:"reviewer_overrides"`
}

func uiConfigFromSettings(us UserSettings) UIConfig {
	return UIConfig{
		StaleAfterDays:     us.StaleAfterDays,
		RecentlyMergedDays: us.RecentlyMergedDays,
		Review: UIReviewConfig{
			DefaultRequiredReviewers: us.DefaultRequiredReviewers,
			IgnoreLabels:             nonNil(us.IgnoreLabels),
			ReviewerOverrides:        us.ReviewerOverrides,
		},
	}
}

func (h *SettingsHandler) config(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	settings, err := h.store.GetSettings(r.Context(), login)
	if err != nil {
		h.logger.Error().Err(err).Msg("Get settings for config")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, uiConfigFromSettings(settings))
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
	slug := chi.URLParam(r, "owner") + "/" + chi.URLParam(r, "repo")
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
	slug := chi.URLParam(r, "owner") + "/" + chi.URLParam(r, "repo")
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

// RulesView is the wire shape of the Review rules screen: the editable knobs.
type RulesView struct {
	DefaultRequiredReviewers int                `json:"default_required_reviewers"`
	StaleAfterDays           int                `json:"stale_after_days"`
	RecentlyMergedDays       int                `json:"recently_merged_days"`
	IgnoreLabels             []string           `json:"ignore_labels"`
	BotAuthors               []string           `json:"bot_authors"`
	ReviewerOverrides        []ReviewerOverride `json:"reviewer_overrides"`
}

func rulesViewFromSettings(us UserSettings) RulesView {
	return RulesView{
		DefaultRequiredReviewers: us.DefaultRequiredReviewers,
		StaleAfterDays:           us.StaleAfterDays,
		RecentlyMergedDays:       us.RecentlyMergedDays,
		IgnoreLabels:             nonNil(us.IgnoreLabels),
		BotAuthors:               nonNil(us.BotAuthors),
		ReviewerOverrides:        us.ReviewerOverrides,
	}
}

func (h *SettingsHandler) getRules(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	settings, err := h.store.GetSettings(r.Context(), login)
	if err != nil {
		h.logger.Error().Err(err).Msg("Get rules")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, rulesViewFromSettings(settings))
}

func (h *SettingsHandler) putRules(w http.ResponseWriter, r *http.Request) {
	login, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body RulesView
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	settings, err := rulesViewToSettings(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.SaveSettings(r.Context(), login, settings); err != nil {
		h.logger.Error().Err(err).Msg("Save rules")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, rulesViewFromSettings(settings))
}

// rulesViewToSettings validates the submitted rules and converts them to a
// UserSettings. It clamps nothing silently: out-of-range values are rejected so
// the client and server agree on what was saved.
func rulesViewToSettings(v RulesView) (UserSettings, error) {
	if v.DefaultRequiredReviewers < 0 || v.DefaultRequiredReviewers > MaxRequiredReviewers {
		return UserSettings{}, errors.New("default_required_reviewers out of range")
	}
	if v.StaleAfterDays < MinDays || v.StaleAfterDays > MaxStaleAfterDays {
		return UserSettings{}, errors.New("stale_after_days out of range")
	}
	if v.RecentlyMergedDays < MinDays || v.RecentlyMergedDays > MaxRecentlyMergedDays {
		return UserSettings{}, errors.New("recently_merged_days out of range")
	}

	overrides := make([]ReviewerOverride, 0, len(v.ReviewerOverrides))
	seen := make(map[string]struct{}, len(v.ReviewerOverrides))
	for _, o := range v.ReviewerOverrides {
		label := strings.TrimSpace(o.Label)
		if label == "" {
			return UserSettings{}, errors.New("reviewer override label is required")
		}
		if o.Reviewers < 0 || o.Reviewers > MaxRequiredReviewers {
			return UserSettings{}, errors.New("reviewer override count out of range")
		}
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		overrides = append(overrides, ReviewerOverride{Label: label, Reviewers: o.Reviewers})
	}

	return UserSettings{
		DefaultRequiredReviewers: v.DefaultRequiredReviewers,
		StaleAfterDays:           v.StaleAfterDays,
		RecentlyMergedDays:       v.RecentlyMergedDays,
		IgnoreLabels:             cleanStrings(v.IgnoreLabels),
		BotAuthors:               cleanStrings(v.BotAuthors),
		ReviewerOverrides:        overrides,
	}, nil
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
