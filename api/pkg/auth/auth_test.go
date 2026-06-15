package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserFrom(t *testing.T) {
	t.Run("returns false when no user is in context", func(t *testing.T) {
		_, ok := UserFrom(context.Background())
		assert.False(t, ok)
	})

	t.Run("round-trips through WithUser", func(t *testing.T) {
		want := User{Login: "alice", AvatarURL: "https://example/a.png"}
		ctx := WithUser(context.Background(), want)

		got, ok := UserFrom(ctx)
		assert.True(t, ok)
		assert.Equal(t, want, got)
	})

	t.Run("each context has its own user", func(t *testing.T) {
		base := context.Background()
		a := WithUser(base, User{Login: "alice"})
		b := WithUser(base, User{Login: "bob"})

		ua, _ := UserFrom(a)
		ub, _ := UserFrom(b)
		assert.Equal(t, "alice", ua.Login)
		assert.Equal(t, "bob", ub.Login)
	})
}
