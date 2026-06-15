package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/e2e/internal/e2e"
	"github.com/jspdown/dashboard/e2e/internal/githubtest"
	"github.com/jspdown/dashboard/e2e/internal/oauth2proxytest"
)

// TestOAuth2ProxyIntegration_SignsInAndReachesDashboard wires the
// production auth topology end-to-end: a real oauth2-proxy subprocess
// in front of the dashboard's bare handler, with the in-process fake
// GitHub server standing in for github.com. It drives the OAuth dance
// over HTTP and confirms a signed-in user reaches /api/me with the
// right identity.
//
// Skipped when oauth2-proxy isn't on PATH. CI installs it; local runs
// without the binary just don't exercise this path.
//
// The rest of the e2e suite bypasses oauth2-proxy entirely by stamping
// X-Forwarded-User server-side, which is faster and covers every
// dashboard-side concern. This test is a smoke check on the wiring
// (oauth2-proxy flags, header names, --api-route behavior) so a
// docker-compose drift can't silently break production.
func TestOAuth2ProxyIntegration_SignsInAndReachesDashboard(t *testing.T) {
	if err := oauth2proxytest.IsAvailable(); err != nil {
		t.Skipf("integration: %v", err)
	}
	t.Parallel()

	const (
		viewer = "alice"
		org    = "test-org"
	)

	ctx := context.Background()
	stack, err := e2e.Boot(ctx, e2e.WithViewer(viewer))
	require.NoError(t, err)
	t.Cleanup(stack.Close)
	stack.Fake.BindT(t)

	// Seed the fake: alice is the user GitHub will hand back from
	// /authorize, and a member of the gated org.
	stack.Fake.AsUser(githubtest.FakeUser{
		Login:     viewer,
		ID:        42,
		AvatarURL: "https://avatars.githubusercontent.com/u/42",
	})
	stack.Fake.AddOrgMember(org, viewer)

	// The bare dashboard handler, no X-Forwarded-User injection.
	// oauth2-proxy stamps the header in this test, like in production.
	apiSrv := httptest.NewServer(stack.RawHandler)
	t.Cleanup(apiSrv.Close)

	proxy := oauth2proxytest.Start(t, oauth2proxytest.Options{
		UpstreamURL: apiSrv.URL,
		FakeURL:     stack.Fake.URL(),
		AllowedOrg:  org,
	})

	// Walk the OAuth dance with a cookie jar. Without one,
	// oauth2-proxy's state and session cookies don't survive between
	// hops and the callback fails with "state mismatch".
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	// Per-request timeout keeps the test failing fast, naming the hung
	// hop, instead of burning the binary's 10-minute deadline on one Do.
	client := &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Unauthenticated GET / makes oauth2-proxy 302 to /oauth2/sign_in.
	current := proxy.URL + "/"
	var finalStatus int
	for hop := range 10 {
		t.Logf("hop %d: %s", hop, current)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoErrorf(t, err, "hop %d (%s)", hop, current)
		require.NoError(t, resp.Body.Close())
		if resp.StatusCode != http.StatusFound {
			finalStatus = resp.StatusCode
			break
		}
		loc, err := resp.Location()
		require.NoError(t, err)
		// External hops (the fake GitHub) come back as full URLs;
		// internal oauth2-proxy hops come back as paths, so resolve
		// relative against the current URL.
		if !strings.HasPrefix(loc.String(), "http") {
			current = strings.TrimRight(current, "/") + loc.String()
		} else {
			current = loc.String()
		}
	}
	assert.Equal(t, http.StatusOK, finalStatus, "OAuth dance should terminate at a 200 on /")

	// Now /api/me should return alice's identity: oauth2-proxy sets
	// X-Forwarded-User on the upstream request, the TrustedHeader
	// middleware reads it, and /api/me echoes it back.
	meReq, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL+"/api/me", nil)
	require.NoError(t, err)
	meResp, err := client.Do(meReq)
	require.NoError(t, err)
	defer func() { _ = meResp.Body.Close() }()

	require.Equal(t, http.StatusOK, meResp.StatusCode)
	var me struct {
		Login string `json:"login"`
	}
	require.NoError(t, json.NewDecoder(meResp.Body).Decode(&me))
	assert.Equal(t, viewer, me.Login, "oauth2-proxy must propagate the signed-in user to /api/me")
}
