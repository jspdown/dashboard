package auth

import (
	"encoding/json"
	"net/http"
)

const (
	// HeaderForwardedUser is the header oauth2-proxy sets (with
	// --pass-user-headers=true) to identify the authenticated user. Production
	// has oauth2-proxy add it; e2e tests inject it directly.
	HeaderForwardedUser = "X-Forwarded-User"

	// HeaderForwardedAvatar optionally carries the user's avatar URL, set by
	// oauth2-proxy with --set-xauthrequest. Absent is fine: UserMenu falls back
	// to a letter.
	HeaderForwardedAvatar = "X-Forwarded-Avatar"
)

// TrustedHeader is the dashboard's only auth middleware. It reads the user from
// the X-Forwarded-User header set by the upstream proxy and injects auth.User
// into the request context. Requests without the header get a 401; oauth2-proxy
// turns that into a sign-in redirect for top-level navigations (its --api-route
// flag keeps XHR 401s as 401s).
//
// The dashboard never validates the proxy's session itself; it relies on the
// deployment topology to route every request through the proxy. In
// docker-compose that's Traefik forwardauth pointed at oauth2-proxy; in e2e the
// harness binds httptest to loopback and injects the header.
func TrustedHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login := r.Header.Get(HeaderForwardedUser)
		if login == "" {
			respondUnauthorized(w)
			return
		}
		ctx := WithUser(r.Context(), User{
			Login:     login,
			AvatarURL: r.Header.Get(HeaderForwardedAvatar),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type unauthorizedBody struct {
	Error string `json:"error"`
}

func respondUnauthorized(w http.ResponseWriter) {
	body, err := json.Marshal(unauthorizedBody{Error: "unauthorized"})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(body)
}
