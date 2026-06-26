package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

func TestVerifier_Verify_Reachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"full_name":"acme/widget"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	v := NewVerifier(newTestClient(t, server))
	require.NoError(t, v.Verify(context.Background(), "acme/widget"))
}

func TestVerifier_Verify_NotFoundIsInaccessible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/ghost", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	v := NewVerifier(newTestClient(t, server))
	err := v.Verify(context.Background(), "acme/ghost")
	assert.ErrorIs(t, err, pullrequest.ErrRepoInaccessible,
		"a 404 must map to ErrRepoInaccessible so the UI shows a 422")
}

func TestVerifier_Verify_OtherErrorIsWrappedNotInaccessible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	v := NewVerifier(newTestClient(t, server))
	err := v.Verify(context.Background(), "acme/widget")
	require.Error(t, err)
	assert.NotErrorIs(t, err, pullrequest.ErrRepoInaccessible,
		"a transient outage must not be reported as no-access (UI shows 502, not 422)")
}

func TestVerifier_Verify_MalformedSlug(t *testing.T) {
	// A bad slug must be rejected before any HTTP call: a hit means the
	// verifier let a malformed slug through to GitHub.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("verifier hit GitHub for a malformed slug")
	}))
	defer server.Close()

	v := NewVerifier(newTestClient(t, server))
	for _, slug := range []string{"noslash", "acme/", "/widget", ""} {
		err := v.Verify(context.Background(), slug)
		assert.ErrorIs(t, err, pullrequest.ErrRepoInaccessible, "slug %q", slug)
	}
}
