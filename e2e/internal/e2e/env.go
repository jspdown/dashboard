package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// bundlePath returns the absolute path to the built React bundle.
// DASHBOARD_WEB_DIR overrides it: the test-e2e and screenshots flake
// apps point it at the Nix-built web package so the harness serves the
// exact bundle the image ships. Without the override we resolve
// app/dist relative to this file's compile-time location, so a bare
// `go test` from any directory still finds a locally built bundle.
func bundlePath() string {
	if dir := os.Getenv("DASHBOARD_WEB_DIR"); dir != "" {
		return dir
	}
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "app", "dist"))
}

// bundleReady checks that the bundle's index.html exists. The test-e2e
// flake app points DASHBOARD_WEB_DIR at the Nix-built bundle so it's
// always present; this is a clear early failure for runs that bypass
// the app (e.g. `go test ./...` against an unbuilt app/dist).
func bundleReady() error {
	bundleOnce.Do(func() {
		indexPath := filepath.Join(bundlePath(), "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			bundleErr = fmt.Errorf("bundle not built: %w (run `make test-e2e`, which serves the Nix-built bundle)", err)
		}
	})
	return bundleErr
}

var (
	bundleOnce sync.Once
	bundleErr  error
)

// chromeAvailable reports whether chromedp can launch a browser here.
// We probe once per binary; repeated checks are O(1).
func chromeAvailable() error {
	chromeOnce.Do(func() {
		chromeErr = probeChrome()
	})
	return chromeErr
}

var (
	chromeOnce sync.Once
	chromeErr  error
)

func probeChrome() error {
	// Generous on purpose: a missing binary fails instantly via
	// exec.ErrNotFound, so the deadline only bites when Chrome exists
	// but starts slowly (cold caches, emulated CPU, parallel load).
	// Killing it then would turn a healthy environment into a spurious
	// skip.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, chromeAllocatorOpts()...)
	defer cancelAlloc()

	bctx, cancelB := chromedp.NewContext(allocCtx)
	defer cancelB()

	if err := chromedp.Run(bctx, chromedp.Navigate("about:blank")); err != nil {
		return wrapChromeErr(err)
	}
	return nil
}

// chromeAllocatorOpts is the canonical chromedp option set for the e2e
// harness. Defined once so the probe and the long-lived shared
// allocator agree on flags. We point chromedp at a Chrome path on disk
// when we find one, since its $PATH lookup misses /Applications apps on
// macOS.
func chromeAllocatorOpts() []chromedp.ExecAllocatorOption {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	if path := findChromeBinary(); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}
	return opts
}

// findChromeBinary returns the first Chrome/Chromium binary it can
// locate, preferring $CHROME_PATH (overridable in CI) over filesystem
// scans. Returns "" when nothing is found, in which case chromedp
// falls back to its built-in $PATH lookup and probably fails, which
// probeChrome reports as a Skip.
func findChromeBinary() string {
	if env := os.Getenv("CHROME_PATH"); env != "" {
		return env
	}
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func wrapChromeErr(err error) error {
	if err == nil {
		return nil
	}
	// Common case: Chrome/Chromium not on $PATH. Bubble up a hint so a
	// developer or agent knows what to install.
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("chrome binary not found on $PATH: %w", err)
	}
	return err
}
