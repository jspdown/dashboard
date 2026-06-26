// Package dashboard wires the runtime dependencies (postgres pool, GitHub
// client, config) into a Dashboard http.Handler: API routes under /api, the
// static frontend from /web when configured, and a background poller for
// on-demand refresh.
//
// It's shared between cmd/dashboard's serve action and the e2e harness so both
// build the dashboard the same way. Tests just pass a different *gh.Client and
// let the harness inject the X-Forwarded-User header the auth middleware reads.
//
// Auth is the upstream proxy's job (oauth2-proxy + Traefik forwardauth in prod,
// a synthesised header in dev/e2e). The dashboard only trusts that header and
// injects auth.User into request context.
package dashboard

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	gh "github.com/google/go-github/v85/github"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jspdown/dashboard/api/pkg/auth"
	"github.com/jspdown/dashboard/api/pkg/buildinfo"
	dgithub "github.com/jspdown/dashboard/api/pkg/github"
	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// Deps is the injection surface for assembling a Dashboard. The caller opens
// the pool and runs migrations before calling New, and closes the pool on exit.
type Deps struct {
	// PollInterval is the default per-repo poll cadence.
	PollInterval time.Duration
	Pool         *pgxpool.Pool
	GitHubClient *gh.Client
	Logger       zerolog.Logger
	// WebDir, when non-empty, serves the built frontend bundle from this dir.
	WebDir string
}

// Dashboard is the http.Handler (a chi.Router with API + static routes) plus
// Poller, the background ticker. Call Poller.Run(ctx) to start polling, or let
// the /api/poll endpoint drive single ticks.
type Dashboard struct {
	handler http.Handler
	Poller  *dgithub.Poller
}

// ServeHTTP delegates to the internal chi router.
func (d *Dashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.handler.ServeHTTP(w, r)
}

// New builds a Dashboard from the injected dependencies.
func New(d Deps) *Dashboard {
	prStore := pullrequest.NewStore(d.Pool)
	userStore := pullrequest.NewUserStore(d.Pool)
	ingester := dgithub.NewIngester(d.Pool, prStore)
	poller := dgithub.NewPoller(d.GitHubClient, ingester, prStore, userStore, userStore, d.PollInterval, d.Logger)

	prService := pullrequest.NewPostgresService(prStore, userStore)
	prHandler := pullrequest.NewHandler(prService, poller, d.Logger)

	verifier := dgithub.NewVerifier(d.GitHubClient)
	settingsHandler := pullrequest.NewSettingsHandler(userStore, prService, verifier, poller, d.Logger)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.TrustedHeader)
		prHandler.Routes(r)
		settingsHandler.Routes(r)
		r.Get("/build", buildinfo.Handler())
		r.Get("/me", meHandler())
	})
	if d.WebDir != "" {
		mountStatic(r, d.WebDir)
	}

	return &Dashboard{handler: r, Poller: poller}
}

// meHandler returns the signed-in user from request context (populated by
// TrustedHeader from the proxy's X-Forwarded-* headers). The SPA fetches it
// once at boot for the TopBar avatar and search placeholder.
func meHandler() http.HandlerFunc {
	type body struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url,omitempty"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFrom(r.Context())
		if !ok {
			http.Error(w, "no user in context", http.StatusInternalServerError)
			return
		}
		raw, err := json.Marshal(body{Login: u.Login, AvatarURL: u.AvatarURL})
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

func mountStatic(r chi.Router, webDir string) {
	fs := http.FileServer(http.Dir(webDir))
	r.Handle("/", fs)
	r.Handle("/index.html", fs)
	r.Handle("/assets/*", fs)
	r.Handle("/favicon.svg", fs)

	indexPath := filepath.Join(webDir, "index.html")
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			http.NotFound(w, req)
			return
		}
		http.ServeFile(w, req, indexPath)
	})
}
