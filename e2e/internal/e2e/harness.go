// Package e2e wires the dashboard's full backend (fake GitHub, real Postgres,
// in-process api, bundled frontend) into a Harness for parallel-safe end-to-end
// tests driven through headless Chrome via chromedp. Each test gets its own
// fake, database, api server, and browser tab; the expensive bits (Postgres
// container, Chrome process, frontend bundle) are shared per test binary in
// TestMain.
//
// Production puts an upstream proxy (oauth2-proxy + Traefik forwardauth) in
// front for auth; the harness fakes that by stamping X-Forwarded-User on every
// request reaching the dashboard handler. The real proxy path is covered
// separately in e2e/internal/oauth2proxytest.
//
// Boot returns a Stack without the testing.T scaffolding, so the harness also
// works as a library for the dev-e2e command and the screenshots suite.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Harness is one isolated dashboard stack scoped to a single test. It embeds
// *Stack (fake server, anchor time, poller) and adds an httptest.Server (URL)
// and a chromedp Browser on top.
type Harness struct {
	*Stack
	t *testing.T

	URL     string
	Browser *Browser

	apiSrv *httptest.Server
}

// Start brings up an isolated stack (fake GitHub, fresh Postgres, in-process
// api with the configured policy, and a chromedp tab pointing at it) and tears
// itself down via t.Cleanup, so callers never call Close. Skips the test if
// Postgres or Chrome isn't reachable.
func Start(t *testing.T, opts ...Option) *Harness {
	t.Helper()

	if err := chromeAvailable(); err != nil {
		t.Skipf("e2e: skipping (Chrome unavailable: %v)", err)
	}
	if err := bundleReady(); err != nil {
		t.Fatalf("e2e: frontend bundle not ready: %v", err)
	}

	ctx := context.Background()
	stack, err := Boot(ctx, opts...)
	if err != nil {
		if strings.Contains(err.Error(), "postgres unavailable") {
			t.Skipf("e2e: %v", err)
		}
		t.Fatalf("e2e: boot stack: %v", err)
	}

	stack.Fake.BindT(t)

	apiSrv := httptest.NewServer(stack.Handler)
	stack.addCloser(apiSrv.Close)

	browser := newBrowser(t, apiSrv.URL, stack.Viewer)

	t.Cleanup(stack.Close)
	return &Harness{
		Stack:   stack,
		t:       t,
		URL:     apiSrv.URL,
		Browser: browser,
		apiSrv:  apiSrv,
	}
}

// Poll triggers a synchronous poll tick for the repo via POST
// /api/poll/{owner}/{repo}, the same path a "force refresh" button would take,
// and blocks until the ingester has written through to Postgres. It shadows
// Stack.Poll deliberately: tests want the full HTTP path, while Stack.Poll
// calls the poller directly for seeding.
func (h *Harness) Poll(repoSlug string) {
	h.t.Helper()
	owner, name, ok := strings.Cut(repoSlug, "/")
	if !ok || owner == "" || name == "" {
		h.t.Fatalf("e2e: Poll(%q): expected owner/name", repoSlug)
	}
	target := fmt.Sprintf("%s/api/poll/%s/%s", h.URL, owner, name)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, target, nil)
	if err != nil {
		h.t.Fatalf("e2e: build poll request: %v", err)
	}
	// The outer middleware injects X-Forwarded-User from the viewer for us.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("e2e: poll %s: %v", repoSlug, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		h.t.Fatalf("e2e: poll %s: HTTP %d", repoSlug, resp.StatusCode)
	}
}

// Reload reloads the SPA so the browser refetches /api data. Call it after
// Poll when the rendered DOM needs to reflect the new state.
func (h *Harness) Reload() {
	h.t.Helper()
	h.Browser.Reload()
}

