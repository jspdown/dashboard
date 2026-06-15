// Command dev-e2e brings up the same harness the e2e tests use, but
// long-lived: a fake GitHub server pre-seeded with the demo scenario, a
// fresh Postgres database (testcontainer), and the in-process api wired
// against both. Vite proxies to it via
// DASHBOARD_API_URL=http://127.0.0.1:18080 so frontend iteration
// renders against the real api with realistic config-driven data.
//
// Production sits behind oauth2-proxy + Traefik forwardauth, which
// inject X-Forwarded-User on every request. The harness mirrors that by
// stamping the header inside Stack.Handler, so dev-e2e needn't run
// oauth2-proxy locally. Sign-in is implicit; iterate on UI with no
// login dance.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jspdown/dashboard/e2e/internal/e2e"
	"github.com/jspdown/dashboard/e2e/internal/scenarios"
)

const defaultViewer = "alex"

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "address to listen on (loopback recommended)")
	viewer := flag.String("viewer", defaultViewer, "GitHub login the dashboard treats as the viewer")
	flag.Parse()

	if err := run(*addr, *viewer); err != nil {
		fmt.Fprintf(os.Stderr, "dev-e2e: %v\n", err)
		os.Exit(1)
	}
}

func run(addr, viewer string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opts := append([]e2e.Option{e2e.WithViewer(viewer)}, repoOpts()...)
	stack, err := e2e.Boot(ctx, opts...)
	if err != nil {
		return fmt.Errorf("boot stack: %w", err)
	}
	defer stack.Close()

	scenarios.Demo(stack.Fake, viewer)

	for _, slug := range scenarios.DemoRepos() {
		if err := stack.Poll(ctx, slug); err != nil {
			return fmt.Errorf("seed poll %s: %w", slug, err)
		}
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           stack.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// Shutdown deliberately uses a fresh context: ctx is already
		// canceled by the time we get here, but we still want to give
		// in-flight requests a brief grace period.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx) //nolint:contextcheck // fresh context is intentional, see comment above
	}()

	fmt.Fprintf(os.Stderr, "dev-e2e: listening on http://%s (viewer=%s)\n", addr, viewer)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func repoOpts() []e2e.Option {
	repos := scenarios.DemoRepos()
	out := make([]e2e.Option, 0, len(repos))
	for _, slug := range repos {
		out = append(out, e2e.WithRepo(slug))
	}
	return out
}
