package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// rollupNode is the test-side shape for a single PR in the GraphQL response.
type rollupNode struct {
	number      int
	headRefOID  string
	rollupState string // "" means no statusCheckRollup
}

// rollupHandler serves a GraphQL response for the rollup query. pages holds the
// nodes per page; cursors[i] is the endCursor for page i (the client's cursor
// into page i+1).
func rollupHandler(t *testing.T, pages [][]rollupNode, cursors []string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/graphql", r.URL.Path)

		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		raw, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(raw, &body))
		assert.Contains(t, body.Query, "statusCheckRollup")

		// Resolve which page is being asked for from the cursor variable.
		var page int
		if c := body.Variables["cursor"]; c != nil {
			cs, ok := c.(string)
			assert.True(t, ok, "cursor must be string when set, got %T", c)
			for i, prev := range cursors {
				if prev == cs {
					page = i + 1
					break
				}
			}
		}
		assert.Less(t, page, len(pages), "unexpected page %d requested", page)

		nodes := make([]map[string]any, 0, len(pages[page]))
		for _, n := range pages[page] {
			commits := map[string]any{"nodes": []map[string]any{
				{"commit": map[string]any{"statusCheckRollup": nil}},
			}}
			if n.rollupState != "" {
				commits = map[string]any{"nodes": []map[string]any{
					{"commit": map[string]any{"statusCheckRollup": map[string]any{"state": n.rollupState}}},
				}}
			}
			nodes = append(nodes, map[string]any{
				"number":     n.number,
				"headRefOid": n.headRefOID,
				"commits":    commits,
			})
		}

		hasNext := page+1 < len(pages)
		end := ""
		if hasNext {
			end = cursors[page]
		}

		resp := map[string]any{"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": end},
					"nodes":    nodes,
				},
			},
		}}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}
}

func TestPoller_listCheckRollups_DecodesAllStates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", rollupHandler(t, [][]rollupNode{{
		{number: 1, headRefOID: "sha1", rollupState: "SUCCESS"},
		{number: 2, headRefOID: "sha2", rollupState: "FAILURE"},
		{number: 3, headRefOID: "sha3", rollupState: "PENDING"},
		{number: 4, headRefOID: "sha4", rollupState: ""}, // null statusCheckRollup
	}}, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	got, err := p.listCheckRollups(context.Background(), "acme", "widget")
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, "SUCCESS", got[0].RollupState)
	assert.Equal(t, "sha1", got[0].HeadRefOID)
	assert.Equal(t, "FAILURE", got[1].RollupState)
	assert.Equal(t, "PENDING", got[2].RollupState)
	assert.Empty(t, got[3].RollupState, "null statusCheckRollup decodes as empty state")
}

func TestPoller_listCheckRollups_FollowsCursorAcrossPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", rollupHandler(t,
		[][]rollupNode{
			{{number: 10, headRefOID: "a", rollupState: "SUCCESS"}},
			{{number: 11, headRefOID: "b", rollupState: "PENDING"}},
		},
		[]string{"cursor-page-1"},
	))
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	got, err := p.listCheckRollups(context.Background(), "acme", "widget")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 10, got[0].Number)
	assert.Equal(t, 11, got[1].Number)
}

func TestPoller_listCheckRollups_SurfacesGraphQLErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Could not resolve to a Repository with the name 'acme/widget'."}]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	_, err := p.listCheckRollups(context.Background(), "acme", "widget")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not resolve to a Repository")
}

func TestPoller_listCheckRollups_NonOKStatusReturnsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newPoller(newTestClient(t, server))
	_, err := p.listCheckRollups(context.Background(), "acme", "widget")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestPoller_listCheckRollups_PassesAuthHeader(t *testing.T) {
	var seenAuth string
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { seenAuth = r.Header.Get("Authorization") })
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequests":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server).WithAuthToken("test-token")
	p := newPoller(client)
	_, err := p.listCheckRollups(context.Background(), "acme", "widget")
	require.NoError(t, err)
	assert.Equal(t, "Bearer test-token", seenAuth, "go-github's auth transport propagates to the GraphQL request")
}

func TestMapRollup(t *testing.T) {
	cases := map[string]string{
		"SUCCESS":  pullrequest.CIPassing,
		"FAILURE":  pullrequest.CIFailing,
		"ERROR":    pullrequest.CIFailing,
		"PENDING":  pullrequest.CIPending,
		"EXPECTED": pullrequest.CIPending,
		"":         pullrequest.CINone,
		"unknown":  pullrequest.CINone,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, mapRollup(in))
		})
	}
}
