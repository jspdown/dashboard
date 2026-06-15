package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	gh "github.com/google/go-github/v85/github"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// rollupQuery asks for every open PR's check rollup state, 50 per page (one
// page is usually enough for us). The rollup combines check runs and legacy
// commit statuses, so it's authoritative even when CI doesn't use the Checks API.
const rollupQuery = `query($owner:String!,$name:String!,$cursor:String){
  repository(owner:$owner,name:$name){
    pullRequests(first:50,after:$cursor,states:OPEN,orderBy:{field:UPDATED_AT,direction:DESC}){
      pageInfo{hasNextPage,endCursor}
      nodes{
        number
        headRefOid
        commits(last:1){nodes{commit{statusCheckRollup{state}}}}
      }
    }
  }
}`

// checkRollup is one PR's GitHub-side rollup snapshot.
type checkRollup struct {
	Number      int
	HeadRefOID  string
	RollupState string // SUCCESS|FAILURE|ERROR|PENDING|EXPECTED|"" (no rollup)
}

// listCheckRollups pages over every open PR in (owner, name) and returns the
// rollup state GitHub computes from check runs and commit statuses. The poller
// uses it to catch CI changes that don't bump pr.updated_at.
func (p *Poller) listCheckRollups(ctx context.Context, owner, name string) ([]checkRollup, error) {
	var (
		out    []checkRollup
		cursor *string
	)
	for {
		page, next, err := p.fetchCheckRollupsPage(ctx, owner, name, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if next == nil {
			return out, nil
		}
		cursor = next
	}
}

func (p *Poller) fetchCheckRollupsPage(ctx context.Context, owner, name string, cursor *string) ([]checkRollup, *string, error) {
	body, err := json.Marshal(map[string]any{
		"query": rollupQuery,
		"variables": map[string]any{
			"owner":  owner,
			"name":   name,
			"cursor": cursor,
		},
	})
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphQLEndpoint(p.client), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Client().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, nil, fmt.Errorf("graphql returned %d: %s", resp.StatusCode, string(snippet))
	}

	var payload struct {
		Data struct {
			Repository struct {
				PullRequests struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Number     int    `json:"number"`
						HeadRefOID string `json:"headRefOid"`
						Commits    struct {
							Nodes []struct {
								Commit struct {
									StatusCheckRollup *struct {
										State string `json:"state"`
									} `json:"statusCheckRollup"`
								} `json:"commit"`
							} `json:"nodes"`
						} `json:"commits"`
					} `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, nil, fmt.Errorf("decoding graphql response: %w", err)
	}
	if len(payload.Errors) > 0 {
		return nil, nil, fmt.Errorf("graphql error: %s", payload.Errors[0].Message)
	}

	page := make([]checkRollup, 0, len(payload.Data.Repository.PullRequests.Nodes))
	for _, n := range payload.Data.Repository.PullRequests.Nodes {
		var state string
		if len(n.Commits.Nodes) > 0 && n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
			state = n.Commits.Nodes[0].Commit.StatusCheckRollup.State
		}
		page = append(page, checkRollup{
			Number:      n.Number,
			HeadRefOID:  n.HeadRefOID,
			RollupState: state,
		})
	}

	if !payload.Data.Repository.PullRequests.PageInfo.HasNextPage {
		return page, nil, nil
	}
	end := payload.Data.Repository.PullRequests.PageInfo.EndCursor
	return page, &end, nil
}

// graphQLEndpoint derives the GraphQL POST URL from the REST client's BaseURL,
// so tests that swap BaseURL for an httptest.Server route GraphQL there too.
func graphQLEndpoint(client *gh.Client) string {
	u := *client.BaseURL
	u.Path = "/graphql"
	u.RawQuery = ""
	return u.String()
}

// mapRollup translates GitHub's StatusState enum into our CI rollup vocabulary.
// PENDING and EXPECTED both mean "in flight", FAILURE and ERROR both mean
// "failing", and "" means no checks yet (statusCheckRollup is null).
func mapRollup(state string) string {
	switch state {
	case "SUCCESS":
		return pullrequest.CIPassing
	case "FAILURE", "ERROR":
		return pullrequest.CIFailing
	case "PENDING", "EXPECTED":
		return pullrequest.CIPending
	default:
		return pullrequest.CINone
	}
}
