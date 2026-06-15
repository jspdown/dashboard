package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v85/github"
)

func NewClient(ctx context.Context, token string) (*gh.Client, *gh.User, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil, errors.New("missing Github token")
	}

	client := gh.NewClient(nil).WithAuthToken(token)

	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("validating github token: %w", err)
	}

	return client, user, nil
}

// VerifyRepos probes each repo with Repositories.Get and splits them into
// accessible and not. A 404 becomes "not found or token lacks access"; other
// errors are wrapped as-is. The caller decides whether to fail or carry on with
// the accessible subset.
func VerifyRepos(ctx context.Context, client *gh.Client, repos []RepoConfig) (accessible []RepoConfig, errs []error) {
	for _, r := range repos {
		owner, name, ok := strings.Cut(r.Repo, "/")
		if !ok {
			errs = append(errs, fmt.Errorf("%s: invalid repo, expected owner/name", r.Repo))
			continue
		}

		if _, _, err := client.Repositories.Get(ctx, owner, name); err != nil {
			var ghErr *gh.ErrorResponse
			if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
				errs = append(errs, fmt.Errorf("%s: not found or token lacks access", r.Repo))
				continue
			}
			errs = append(errs, fmt.Errorf("%s: %w", r.Repo, err))
			continue
		}

		accessible = append(accessible, r)
	}

	return accessible, errs
}
