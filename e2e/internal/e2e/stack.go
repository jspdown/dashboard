package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	gh "github.com/google/go-github/v85/github"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jspdown/dashboard/api/pkg/auth"
	"github.com/jspdown/dashboard/api/pkg/dashboard"
	"github.com/jspdown/dashboard/api/pkg/postgres"

	"github.com/jspdown/dashboard/e2e/internal/githubtest"
	"github.com/jspdown/dashboard/e2e/internal/pgtest"
)

// Stack is the harness core without testing scaffolding: a fake GitHub server,
// a fresh Postgres database, and a fully-wired dashboard app. Test callers
// prefer Start (it wraps Stack with a chromedp Browser via t.Cleanup); non-test
// callers like dev-e2e call Boot and manage the lifecycle via Stack.Close.
type Stack struct {
	// Fake is the in-process GitHub server. Authoring scenarios against it is
	// the entry point for tests and dev-e2e alike.
	Fake *githubtest.Server
	// Anchor is the scenario time; relative offsets in builder calls are
	// computed from it.
	Anchor time.Time
	// Handler is the chi router (/api routes plus the static bundle from
	// WebDir), already wrapped with the X-Forwarded-User injector. Callers wrap
	// it in their own server (httptest for tests, ListenAndServe for dev-e2e).
	Handler http.Handler
	// RawHandler is the dashboard handler without the X-Forwarded-User
	// injection. The oauth2-proxy integration test uses this so the proxy
	// stamps the header itself, mirroring production.
	RawHandler http.Handler
	// DSN is the Postgres connection string the api was wired against, handy
	// for tests that peek at the database directly.
	DSN string
	// Viewer is the login the harness impersonates by injecting
	// X-Forwarded-User on every request, so tests can assert on per-user state
	// without a real OAuth flow.
	Viewer string

	options options
	app     *dashboard.Dashboard
	pool    *pgxpool.Pool
	closers []func()
}

// Boot brings up the harness core (Postgres, fake GitHub, in-process api
// wiring). The Stack is ready to serve immediately; the caller wraps
// Stack.Handler in its own server. Any mid-boot failure cleans up partial
// state before returning the error.
func Boot(ctx context.Context, opts ...Option) (*Stack, error) {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}

	if err := pgtest.IsAvailable(); err != nil {
		return nil, fmt.Errorf("postgres unavailable: %w", err)
	}

	dsn, dropDB, err := pgtest.NewDatabase(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire database: %w", err)
	}
	closers := []func(){dropDB}

	rollback := func(wrapped error) error {
		for _, c := range slices.Backward(closers) {
			c()
		}
		return wrapped
	}

	fake := githubtest.NewDetached(githubtest.WithAnchor(o.anchor))
	closers = append(closers, fake.Close)

	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		return nil, rollback(fmt.Errorf("postgres pool: %w", err))
	}
	closers = append(closers, pool.Close)

	cfg := o.toAppConfig()

	ghClient := gh.NewClient(nil).WithAuthToken("fake-token")
	fakeBase, err := url.Parse(fake.URL())
	if err != nil {
		return nil, rollback(fmt.Errorf("parse fake url: %w", err))
	}
	ghClient.BaseURL = fakeBase
	ghClient.UploadURL = fakeBase

	app := dashboard.New(dashboard.Deps{
		Config:       cfg,
		Pool:         pool,
		GitHubClient: ghClient,
		PollRepos:    cfg.Repos,
		Logger:       zerolog.Nop(),
		WebDir:       bundlePath(),
	})

	return &Stack{
		Fake:       fake,
		Anchor:     fake.Anchor(),
		Handler:    injectAuthHeader(o.viewer, app),
		RawHandler: app,
		DSN:        dsn,
		Viewer:     o.viewer,
		options:    o,
		app:        app,
		pool:       pool,
		closers:    closers,
	}, nil
}

// injectAuthHeader wraps next so every request carries the viewer in
// X-Forwarded-User. Production gets that header from oauth2-proxy; the harness
// stamps it server-side instead, so chromedp's tab, Harness.Poll's client, and
// direct test requests all look authenticated without anyone touching headers.
func injectAuthHeader(viewer string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set, not Add: the harness's identity should win, and an inbound value
		// is suspicious anyway (the dashboard is on loopback in tests).
		r.Header.Set(auth.HeaderForwardedUser, viewer)
		next.ServeHTTP(w, r)
	})
}

// Close releases every resource the Stack holds, in LIFO order. Safe to call
// more than once; later calls are no-ops.
func (s *Stack) Close() {
	for _, c := range slices.Backward(s.closers) {
		c()
	}
	s.closers = nil
}

// Poll runs a synchronous poll tick for the repo via the in-process
// *github.Poller (no HTTP round-trip). Used by dev-e2e and the screenshot suite
// to seed state; end-to-end tests should prefer Harness.Poll, which goes
// through the HTTP endpoint the webui uses.
func (s *Stack) Poll(ctx context.Context, repoSlug string) error {
	if s.app == nil || s.app.Poller == nil {
		return errors.New("stack not booted: no poller")
	}
	return s.app.Poller.RunOnce(ctx, repoSlug)
}

// addCloser registers another teardown step in the LIFO list. Start uses it to
// fold httptest.Server and chromedp tab cleanup into the same chain.
func (s *Stack) addCloser(fn func()) {
	s.closers = append(s.closers, fn)
}
