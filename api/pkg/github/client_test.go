package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_RejectsEmptyToken(t *testing.T) {
	cases := []string{"", "   \n  "}
	for _, token := range cases {
		client, user, err := NewClient(context.Background(), token)
		assert.Nil(t, client)
		assert.Nil(t, user)
		assert.ErrorContains(t, err, "missing Github token")
	}
}

func TestVerifyRepos_AllAccessible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/repos/acme/gadget", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	repos := []RepoConfig{{Repo: "acme/widget"}, {Repo: "acme/gadget"}}
	accessible, errs := VerifyRepos(context.Background(), newTestClient(t, server), repos)
	assert.Equal(t, repos, accessible)
	assert.Empty(t, errs)
}

func TestVerifyRepos_DropsInaccessibleAndReports404Specially(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/repos/acme/missing", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	repos := []RepoConfig{{Repo: "acme/widget"}, {Repo: "acme/missing"}}
	accessible, errs := VerifyRepos(context.Background(), newTestClient(t, server), repos)
	assert.Equal(t, []RepoConfig{{Repo: "acme/widget"}}, accessible)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "acme/missing: not found or token lacks access")
}

func TestVerifyRepos_AllInaccessible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/missing-one", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/repos/acme/missing-two", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	repos := []RepoConfig{{Repo: "acme/missing-one"}, {Repo: "acme/missing-two"}}
	accessible, errs := VerifyRepos(context.Background(), newTestClient(t, server), repos)
	assert.Empty(t, accessible)
	require.Len(t, errs, 2)
	assert.Contains(t, errs[0].Error(), "acme/missing-one: not found or token lacks access")
	assert.Contains(t, errs[1].Error(), "acme/missing-two: not found or token lacks access")
}
