// Package oauth2proxytest runs oauth2-proxy as a subprocess for the
// integration test that exercises the production auth topology
// end-to-end: oauth2-proxy in front of the dashboard, with a fake
// GitHub server in place of GitHub. Tests that don't need the full
// wiring should use the e2e harness directly; it stamps
// X-Forwarded-User server-side without the subprocess overhead.
package oauth2proxytest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Binary is the executable name we look for on PATH.
const Binary = "oauth2-proxy"

// IsAvailable reports whether the oauth2-proxy binary is on PATH.
// Tests use this to t.Skip when running in environments without it.
func IsAvailable() error {
	if _, err := exec.LookPath(Binary); err != nil {
		return fmt.Errorf("oauth2-proxy not on PATH: %w", err)
	}
	return nil
}

// Proxy is a running oauth2-proxy subprocess.
type Proxy struct {
	URL    string
	cancel context.CancelFunc
	done   chan struct{}
}

// Options configures the spawned oauth2-proxy. UpstreamURL is the
// dashboard's bare handler (no auth injection), FakeURL is the
// in-process fake GitHub server, AllowedOrg gates org membership.
type Options struct {
	UpstreamURL string
	FakeURL     string
	AllowedOrg  string
}

// Start spawns oauth2-proxy on a random loopback port and waits for
// it to accept connections; a t.Cleanup kills it on teardown. The
// returned Proxy's URL is the address tests drive requests at. Skips
// the test (t.Skipf) when oauth2-proxy isn't on PATH.
func Start(t *testing.T, opts Options) *Proxy {
	t.Helper()
	if err := IsAvailable(); err != nil {
		t.Skipf("oauth2proxytest: %v", err)
	}

	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatalf("oauth2proxytest: pick free port: %v", err)
	}

	cookieSecret := make([]byte, 16)
	if _, err := rand.Read(cookieSecret); err != nil {
		t.Fatalf("oauth2proxytest: generate cookie secret: %v", err)
	}

	// oauth2-proxy's GitHub provider treats validate-url's path as the
	// API base and appends /user, /user/orgs, /user/emails. So
	// validate-url is the fake server root (no /user suffix) and
	// profile-url is the full /user URL.
	fakeRoot := trimSlash(opts.FakeURL)
	args := []string{
		"--provider=github",
		"--github-org=" + opts.AllowedOrg,
		"--client-id=test-client",
		"--client-secret=test-secret",
		"--cookie-secret=" + hex.EncodeToString(cookieSecret),
		"--cookie-secure=false",
		"--cookie-samesite=lax",
		"--http-address=127.0.0.1:" + strconv.Itoa(port),
		"--reverse-proxy=true",
		"--pass-user-headers=true",
		"--api-route=^/api",
		"--skip-provider-button=true",
		"--scope=read:org",
		"--email-domain=*",
		"--upstream=" + opts.UpstreamURL,
		"--login-url=" + fakeRoot + "/login/oauth/authorize",
		"--redeem-url=" + fakeRoot + "/login/oauth/access_token",
		"--validate-url=" + fakeRoot + "/",
		"--profile-url=" + fakeRoot + "/user",
		"--whitelist-domain=127.0.0.1:" + strconv.Itoa(port),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, Binary, args...)
	// Pipe oauth2-proxy stdout/stderr to both t.Log (for failure
	// reporting) and os.Stderr (so output survives a hang/panic, since
	// t.Log only flushes on FAIL and the timeout panic can lose
	// buffered messages).
	cmd.Stdout = io.MultiWriter(testLogWriter{t: t, prefix: "oauth2-proxy stdout"}, os.Stderr)
	cmd.Stderr = io.MultiWriter(testLogWriter{t: t, prefix: "oauth2-proxy stderr"}, os.Stderr)

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("oauth2proxytest: start: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForReady(proxyURL+"/ping", 10*time.Second); err != nil {
		cancel()
		<-done
		t.Fatalf("oauth2proxytest: wait for ready: %v", err)
	}

	p := &Proxy{URL: proxyURL, cancel: cancel, done: done}
	t.Cleanup(p.Stop)
	return p
}

// Stop cancels the subprocess and waits for it to exit.
func (p *Proxy) Stop() {
	if p == nil {
		return
	}
	p.cancel()
	<-p.done
}

// freeLoopbackPort asks the kernel for an unused port on 127.0.0.1 by
// binding, capturing the port, and closing. There's a small race
// before the subprocess re-binds, same as httptest does internally.
func freeLoopbackPort() (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	addr, _ := l.Addr().(*net.TCPAddr)
	if err := l.Close(); err != nil {
		return 0, err
	}
	if addr == nil {
		return 0, errors.New("listener returned a non-TCP address")
	}
	return addr.Port, nil
}

// waitForReady polls url until it returns any HTTP response or the
// deadline elapses. oauth2-proxy's /ping endpoint returns 200 once
// it's accepting connections.
func waitForReady(pingURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 250 * time.Millisecond}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, pingURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("oauth2-proxy did not become ready in time")
}

func trimSlash(s string) string { return strings.TrimRight(s, "/") }

// testLogWriter routes subprocess stdout/stderr into t.Log so a
// failed test surfaces oauth2-proxy's diagnostics in the test output.
type testLogWriter struct {
	t      *testing.T
	prefix string
}

func (w testLogWriter) Write(p []byte) (int, error) {
	for line := range strings.SplitSeq(strings.TrimRight(string(p), "\n"), "\n") {
		if line == "" {
			continue
		}
		w.t.Logf("%s: %s", w.prefix, line)
	}
	return len(p), nil
}

// ResolveURL turns a relative path against the proxy's URL into an
// absolute URL. Convenience for tests building request URLs.
func (p *Proxy) ResolveURL(path string) string {
	u, _ := url.Parse(p.URL)
	rel, _ := url.Parse(path)
	return u.ResolveReference(rel).String()
}
