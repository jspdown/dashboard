// Package auth carries the per-request user identity through request context.
//
// The dashboard runs no auth flow of its own. Production sits behind
// oauth2-proxy, which does the GitHub OAuth dance, gates on org membership, and
// forwards the identity as request headers. TrustedHeader (in middleware.go)
// reads those headers and injects auth.User; downstream reads it via UserFrom.
package auth

import "context"

// User identifies the human behind a request, populated by TrustedHeader from
// the upstream proxy's headers.
type User struct {
	Login     string
	AvatarURL string
}

type ctxKey int

const userKey ctxKey = 0

// WithUser returns a copy of ctx that carries u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFrom returns the authenticated user attached to ctx. ok is false when no
// user is present, which behind the auth middleware is a programming error
// (the middleware rejects unauthenticated requests upstream).
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey).(User)
	return u, ok
}
