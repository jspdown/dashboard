package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v85/github"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// Verifier checks that the server PAT can reach a repo before the poller starts
// servicing it. It backs the add-repo flow and implements
// pullrequest.RepoVerifier.
type Verifier struct {
	client *gh.Client
}

func NewVerifier(client *gh.Client) *Verifier {
	return &Verifier{client: client}
}

// Verify returns nil if the repo is reachable, pullrequest.ErrRepoInaccessible
// if it doesn't exist or the token lacks access, and the wrapped error for any
// other failure (so a transient GitHub outage isn't reported to the user as
// "no access").
func (v *Verifier) Verify(ctx context.Context, slug string) error {
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("%w: %q is not in owner/name form", pullrequest.ErrRepoInaccessible, slug)
	}

	if _, _, err := v.client.Repositories.Get(ctx, owner, name); err != nil {
		var ghErr *gh.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
			return pullrequest.ErrRepoInaccessible
		}

		return fmt.Errorf("verifying %s: %w", slug, err)
	}

	return nil
}
