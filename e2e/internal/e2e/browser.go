package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/jspdown/dashboard/api/pkg/auth"
)

// Browser wraps a chromedp tab scoped to one test. Methods fail the test
// on error so callers don't thread a context through their assertions.
// One Chrome process is shared across the binary; each Browser gets its
// own tab, so parallel tests don't see each other's DOM or storage.
//
// Note: wrapping the tab context in context.WithTimeout makes chromedp
// detach from the tab when the cancel fires, so we use the tab context
// directly and rely on the Go test -timeout to bound the run.
type Browser struct {
	t       *testing.T
	tabCtx  context.Context
	cancel  context.CancelFunc
	baseURL string
}

func newBrowser(t *testing.T, baseURL, viewer string) *Browser {
	t.Helper()
	parent := sharedAlloc(t)

	tabCtx, cancel := chromedp.NewContext(parent)
	t.Cleanup(cancel)

	b := &Browser{t: t, tabCtx: tabCtx, cancel: cancel, baseURL: baseURL}

	// Set a viewport before the first navigation so headless Chrome
	// doesn't fall back to 800x600. Width matches the screenshot suite;
	// height is just a floor. Also pin X-Forwarded-User on every request
	// like oauth2-proxy does, so the identity holds even for requests the
	// server middleware can't intercept (e.g. preflights).
	if err := chromedp.Run(tabCtx,
		chromedp.EmulateViewport(1280, 800),
		network.SetExtraHTTPHeaders(network.Headers{auth.HeaderForwardedUser: viewer}),
	); err != nil {
		t.Fatalf("e2e: initial tab setup: %v", err)
	}

	b.Goto("/")
	return b
}

// Goto navigates to a path on the harness and waits for the initial
// /api/prs fetch to finish, signaled by data-prs-loaded="true". We wait
// on that marker rather than [data-group-id] because the SPA mounts the
// group containers before the PR data arrives. Pass "/" for the root.
func (b *Browser) Goto(path string) {
	b.t.Helper()
	url := b.baseURL + path
	if err := chromedp.Run(b.tabCtx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`[data-prs-loaded="true"]`, chromedp.ByQuery),
	); err != nil {
		b.dumpForFailure("goto "+url, err)
	}
}

// Reload does a full page reload and waits for the SPA to remount and
// refetch /api/prs. Two races to defeat: Reload + WaitVisible can match
// the old DOM still attached during unload, so we stamp a nonce and Poll
// until it's gone before trusting the new document; and the group
// containers mount before /api/prs returns, so we wait on the
// data-prs-loaded="true" marker rather than [data-group-id].
func (b *Browser) Reload() {
	b.t.Helper()
	nonce := fmt.Sprintf("reload-%d", time.Now().UnixNano())
	stampJS := fmt.Sprintf(`document.documentElement.setAttribute('data-reload-nonce', %q); true`, nonce)
	waitForUnloadJS := fmt.Sprintf(
		`document.documentElement.getAttribute('data-reload-nonce') !== %q`, nonce)
	var stamped bool
	if err := chromedp.Run(b.tabCtx,
		chromedp.Evaluate(stampJS, &stamped),
		chromedp.Reload(),
		chromedp.Poll(waitForUnloadJS, nil),
		chromedp.WaitVisible(`[data-prs-loaded="true"]`, chromedp.ByQuery),
	); err != nil {
		b.dumpForFailure("reload", err)
	}
}

// dumpForFailure logs the current tab state (url, readyState, body) to
// help diagnose chromedp errors. The failure path for Goto and Reload.
func (b *Browser) dumpForFailure(op string, cause error) {
	b.t.Helper()
	var (
		url        string
		readyState string
		bodyHTML   string
	)
	if err := chromedp.Run(b.tabCtx,
		chromedp.Location(&url),
		chromedp.Evaluate(`document.readyState`, &readyState),
		chromedp.OuterHTML(`body`, &bodyHTML, chromedp.ByQuery),
	); err != nil {
		b.t.Logf("e2e: dump-for-failure itself errored: %v", err)
	}
	b.t.Logf("e2e: %s failure (cause: %v)", op, cause)
	b.t.Logf("e2e: tab url=%s readyState=%s", url, readyState)
	if len(bodyHTML) > 4000 {
		bodyHTML = bodyHTML[:4000] + "...(truncated)"
	}
	b.t.Logf("e2e: body HTML: %s", bodyHTML)
	b.t.Fatalf("e2e: %s failed", op)
}

// PRsInGroup returns the PR numbers rendered in the named group (e.g.
// "review", "mine", "merged"), or empty when it's empty or collapsed.
func (b *Browser) PRsInGroup(groupID string) []int {
	b.t.Helper()
	var raw []string
	sel := fmt.Sprintf(`[data-group-id=%q] [data-pr-num]`, groupID)
	js := fmt.Sprintf(`Array.from(document.querySelectorAll(%q)).map(el => el.getAttribute('data-pr-num'))`, sel)
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &raw)); err != nil {
		b.t.Fatalf("e2e: PRsInGroup(%s): %v", groupID, err)
	}
	out := make([]int, 0, len(raw))
	for _, s := range raw {
		var n int
		_, _ = fmt.Sscanf(s, "%d", &n)
		out = append(out, n)
	}
	return out
}

// GroupCount reports the count rendered in a group's header (the
// number to the right of the label, sourced from the API's PR list).
func (b *Browser) GroupCount(groupID string) int {
	b.t.Helper()
	var n int
	js := fmt.Sprintf(`(() => {
		const head = document.querySelector('[data-group-head=%q]');
		if (!head) return -1;
		const counts = head.querySelectorAll('[class*="gCount"]');
		// First span is the total; there can be more for split summaries.
		if (counts.length === 0) return -1;
		return parseInt(counts[0].textContent.trim(), 10);
	})()`, groupID)
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &n)); err != nil {
		b.t.Fatalf("e2e: GroupCount(%s): %v", groupID, err)
	}
	return n
}

// GroupTooltip returns the title attribute on the group header span,
// which holds config-driven descriptive text. Lets tests assert that
// ignore_labels and reviewer_overrides reach the rendered UI.
func (b *Browser) GroupTooltip(groupID string) string {
	b.t.Helper()
	var s string
	js := fmt.Sprintf(`(() => {
		const head = document.querySelector('[data-group-head=%q]');
		if (!head) return '';
		const span = head.querySelector('span[title]');
		return span ? span.getAttribute('title') : '';
	})()`, groupID)
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &s)); err != nil {
		b.t.Fatalf("e2e: GroupTooltip(%s): %v", groupID, err)
	}
	return s
}

// FilterChips returns the quick-filter chip labels in render order. Use
// to assert that "stale > Nd" reflects the configured stale_after_days.
func (b *Browser) FilterChips() []string {
	b.t.Helper()
	var out []string
	js := `Array.from(document.querySelectorAll('button.chip')).map(el => el.textContent.trim())`
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &out)); err != nil {
		b.t.Fatalf("e2e: FilterChips: %v", err)
	}
	return out
}

// QueryAttribute returns an attribute value on the first element matching
// selector, or "" when nothing matches.
func (b *Browser) QueryAttribute(selector, attr string) string {
	b.t.Helper()
	var s string
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return '';
		return el.getAttribute(%q) ?? '';
	})()`, selector, attr)
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &s)); err != nil {
		b.t.Fatalf("e2e: QueryAttribute(%q, %q): %v", selector, attr, err)
	}
	return s
}

// SearchPlaceholder returns the placeholder on the dashboard search
// input. Use to assert that author:{viewer} is config-driven.
func (b *Browser) SearchPlaceholder() string {
	b.t.Helper()
	var s string
	js := `document.querySelector('input[placeholder]')?.getAttribute('placeholder') ?? ''`
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &s)); err != nil {
		b.t.Fatalf("e2e: SearchPlaceholder: %v", err)
	}
	return s
}

// Click clicks the first element matching selector. Screenshot scenarios
// use it to drive the dashboard into a state (collapse a group, etc.).
func (b *Browser) Click(selector string) {
	b.t.Helper()
	if err := chromedp.Run(b.tabCtx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
		b.t.Fatalf("e2e: Click(%q): %v", selector, err)
	}
}

// ExpandAllGroups clears the "collapsed groups" localStorage entry so
// every group renders expanded after the next reload. Used for
// "show everything" screenshots; settling it at page load beats firing
// clicks and waiting on a React state cascade before capture.
func (b *Browser) ExpandAllGroups() {
	b.t.Helper()
	const js = `(() => { localStorage.setItem('dashboard:collapsedGroups', '[]'); return true; })()`
	var ok bool
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &ok)); err != nil {
		b.t.Fatalf("e2e: ExpandAllGroups: %v", err)
	}
	b.Reload()
}

// RenderTooltipsOverlay injects a fixed banner listing each group
// header's title attribute. Native tooltips don't render in headless
// screenshots, so this surfaces them for documentation captures. Returns
// once the overlay is in the DOM, ready for the next Screenshot.
func (b *Browser) RenderTooltipsOverlay() {
	b.t.Helper()
	const js = `(() => {
	const headers = Array.from(document.querySelectorAll('button[aria-expanded] span[title]'));
	const overlay = document.createElement('div');
	overlay.id = 'e2e-tooltip-overlay';
	const css = [
		'position:fixed', 'top:0', 'left:0', 'right:0',
		'background:#0a0a0a', 'color:#e2e8f0',
		'padding:12px 16px',
		'font-family:Geist Mono, ui-monospace, monospace',
		'font-size:11px', 'z-index:9999',
		'border-bottom:2px solid #2dd4bf', 'line-height:1.6',
	].join(';');
	overlay.style.cssText = css;
	overlay.innerHTML = '<strong style="color:#2dd4bf">Group descriptions:</strong><br>' +
		headers.map(h => '&bull; <strong>' + h.textContent.trim() + '</strong>: ' + h.getAttribute('title')).join('<br>');
	document.body.prepend(overlay);
	return true;
})()`
	var done bool
	if err := chromedp.Run(b.tabCtx, chromedp.Evaluate(js, &done)); err != nil {
		b.t.Fatalf("e2e: RenderTooltipsOverlay: %v", err)
	}
}

// SetViewport changes the emulated viewport size. Call it before
// Screenshot when you need a specific frame; otherwise Screenshot lets
// the page content size the captured area.
func (b *Browser) SetViewport(width, height int) {
	b.t.Helper()
	if err := chromedp.Run(b.tabCtx, chromedp.EmulateViewport(int64(width), int64(height))); err != nil {
		b.t.Fatalf("e2e: SetViewport(%d,%d): %v", width, height, err)
	}
}

// Screenshot writes a full-page PNG of the current tab under name (no
// .png needed, the helper adds it) in the directory from screenshotDir.
//
// The dashboard's height:100vh layout clips the capture to the viewport,
// so we inject a CSS override first to let the document grow to its
// content and give FullScreenshot a crop-free result. The override stays
// on the tab, but per-test tabs keep it from bleeding across tests.
func (b *Browser) Screenshot(name string) {
	b.t.Helper()
	if !strings.HasSuffix(name, ".png") {
		name += ".png"
	}
	dir := screenshotDir(b.t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.t.Fatalf("e2e: Screenshot mkdir: %v", err)
	}
	path := filepath.Join(dir, name)

	// Loosen the nested 100vh / overflow:hidden flex containers that clip
	// captureBeyondViewport so the page sizes to its content. Idempotent.
	const sizingCSS = `(() => {
	const style = document.createElement('style');
	style.id = 'e2e-screenshot-sizing';
	style.textContent =
		'html, body, #root, [class*="app"], [class*="main"], [class*="prdash"] {' +
		'  height: auto !important;' +
		'  min-height: 0 !important;' +
		'  max-height: none !important;' +
		'  overflow: visible !important;' +
		'  flex: none !important;' +
		'}';
	document.head.appendChild(style);
	return true;
})()`

	var ok bool
	var buf []byte
	if err := chromedp.Run(b.tabCtx,
		chromedp.Evaluate(sizingCSS, &ok),
		chromedp.FullScreenshot(&buf, 100),
	); err != nil {
		b.t.Fatalf("e2e: Screenshot capture: %v", err)
	}
	// 0644: these are checked in or attached to PRs, so world-readable is
	// fine. gosec flags it anyway.
	if err := os.WriteFile(path, buf, 0o644); err != nil { //nolint:gosec
		b.t.Fatalf("e2e: Screenshot write %s: %v", path, err)
	}
	b.t.Logf("e2e: wrote %s", path)
}

// screenshotDir resolves the output directory: DASHBOARD_SCREENSHOT_DIR
// if set (so CI and per-PR runs can point anywhere), else a per-test
// temp dir.
func screenshotDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("DASHBOARD_SCREENSHOT_DIR"); d != "" {
		return d
	}
	return filepath.Join(t.TempDir(), "screenshots")
}

// Shared chromedp allocator: one Chrome process per test binary, started
// lazily. Each test gets its own tab context above.

var (
	allocOnce sync.Once
	allocCtx  context.Context
	allocCanc context.CancelFunc
	allocErr  error
)

// sharedAlloc returns the chromedp allocator shared by every test in this
// binary. Each test makes its own tab from it but reuses the one Chrome
// process, since boot is the expensive part (~1s). Chrome starts lazily
// on the first action; we don't warm it up here because canceling the
// only tab tears Chrome down with it. Cleanup is stopShared in TestMain.
func sharedAlloc(t *testing.T) context.Context {
	t.Helper()
	allocOnce.Do(func() {
		ctx, cancelExec := chromedp.NewExecAllocator(context.Background(), chromeAllocatorOpts()...)
		allocCtx = ctx
		allocCanc = cancelExec
	})
	if allocErr != nil {
		t.Skipf("e2e: shared Chrome alloc failed: %v", allocErr)
	}
	return allocCtx
}

// stopShared tears down the shared Chrome process, called from TestMain
// after all tests finish.
func stopShared() {
	if allocCanc != nil {
		allocCanc()
	}
}
