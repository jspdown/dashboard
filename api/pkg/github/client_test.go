package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient_RejectsEmptyToken(t *testing.T) {
	cases := []string{"", "   \n  "}
	for _, token := range cases {
		client, user, err := NewClient(context.Background(), token)
		assert.Nil(t, client)
		assert.Nil(t, user)
		assert.ErrorContains(t, err, "missing")
	}
}
