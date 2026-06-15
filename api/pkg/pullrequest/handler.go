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

	"github.com/jspdown/dashboard/api/pkg/config"
)

// PollTrigger is the slice of the GitHub poller the handler needs to drive
// a single on-demand poll. *github.Poller satisfies it; tests can stub it.
type PollTrigger interface {
	RunOnce(ctx context.Context, repoSlug string) error
}

// UIConfig is the slice of runtime config the frontend needs for group
// descriptions, stale badges, and the search placeholder. Connection-level
// fields (repos, GitHub token, bot authors) stay server-side; viewer
// identity comes from /api/auth/status, not here.
type UIConfig struct {
	StaleAfterDays     int            `json:"stale_after_days"`
	RecentlyMergedDays int            `json:"recently_merged_days"`
	Review             UIReviewConfig `json:"review"`
}

type UIReviewConfig struct {
	DefaultRequiredReviewers int                       `json:"default_required_reviewers"`
	IgnoreLabels             []string                  `json:"ignore_labels"`
	ReviewerOverrides        []config.ReviewerOverride `json:"reviewer_overrides"`
}

// UIConfigFrom strips the on-disk config down to what the frontend can see.
func UIConfigFrom(cfg *config.Config) UIConfig {
	return UIConfig{
		StaleAfterDays:     cfg.Freshness.StaleAfterDays,
		RecentlyMergedDays: cfg.Freshness.RecentlyMergedDays,
		Review: UIReviewConfig{
			DefaultRequiredReviewers: cfg.Review.DefaultRequiredReviewers,
			IgnoreLabels:             append([]string(nil), cfg.Review.IgnoreLabels...),
			ReviewerOverrides:        append([]config.ReviewerOverride(nil), cfg.Review.ReviewerOverrides...),
		},
	}
}

type Handler struct {
	svc      Service
	uiConfig UIConfig
	poller   PollTrigger
	logger   zerolog.Logger
}

func NewHandler(svc Service, uiConfig UIConfig, poller PollTrigger, logger zerolog.Logger) *Handler {
	return &Handler{svc: svc, uiConfig: uiConfig, poller: poller, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/prs", h.list)
	r.Post("/prs/{id}/viewed", h.markViewed)
	r.Get("/config", h.config)
	r.Post("/poll/{owner}/{repo}", h.poll)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	opts := ListOpts{
		Filter: r.URL.Query().Get("filter"),
		Sort:   r.URL.Query().Get("sort"),
	}
	prs, err := h.svc.List(r.Context(), opts)
	if err != nil {
		h.logger.Error().Err(err).Msg("List pull requests")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, http.StatusOK, prs)
}

func (h *Handler) markViewed(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.svc.MarkViewed(r.Context(), id); err != nil {
		h.logger.Error().Err(err).Int64("pr_github_id", id).Msg("Mark pr viewed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) config(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, h.uiConfig)
}

// errRepoNotConfigured is the sentinel the poller returns for a repo that
// isn't on its configured list. We surface it as a 404 (not a 500) so
// callers can tell "wrong repo" from "GitHub or the DB is down".
var errRepoNotConfigured = errors.New("repo not configured for polling")

// poll triggers one synchronous poll tick for the owner/repo path param.
// Used by the e2e harness for deterministic driving, and meant to back a
// future "force refresh" button. Always-on: the dashboard sits behind
// tailnet TLS so there's no abuse surface, and poll cost is bounded by
// GitHub's rate limit.
func (h *Handler) poll(w http.ResponseWriter, r *http.Request) {
	if h.poller == nil {
		http.Error(w, "polling not available", http.StatusServiceUnavailable)
		return
	}
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "repo")
	if owner == "" || name == "" {
		http.Error(w, "missing owner or repo", http.StatusBadRequest)
		return
	}
	slug := owner + "/" + name
	if err := h.poller.RunOnce(r.Context(), slug); err != nil {
		if isRepoNotConfigured(err) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		h.logger.Error().Err(err).Str("repo", slug).Msg("On-demand poll failed")
		http.Error(w, "poll failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// isRepoNotConfigured matches the error *github.Poller emits for an
// unconfigured repo. Substring match so this layer doesn't import github.
func isRepoNotConfigured(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errRepoNotConfigured) ||
		strings.Contains(err.Error(), "is not in the poller's configured repos")
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error().Err(err).Msg("Encode response")
	}
}
