package github

import (
	"context"
	"errors"
	"fmt"
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
