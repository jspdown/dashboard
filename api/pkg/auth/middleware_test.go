package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrustedHeader(t *testing.T) {
	t.Run("injects user from X-Forwarded-User", func(t *testing.T) {
		var captured User
		var captureOK bool
		ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured, captureOK = UserFrom(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/prs", nil)
		req.Header.Set(HeaderForwardedUser, "alice")
		req.Header.Set(HeaderForwardedAvatar, "https://avatars.githubusercontent.com/u/1?v=4")

		TrustedHeader(ok).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, captureOK, "user must be in context")
		assert.Equal(t, "alice", captured.Login)
		assert.Equal(t, "https://avatars.githubusercontent.com/u/1?v=4", captured.AvatarURL)
	})

	t.Run("avatar header is optional", func(t *testing.T) {
		var captured User
		ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured, _ = UserFrom(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/prs", nil)
		req.Header.Set(HeaderForwardedUser, "bob")

		TrustedHeader(ok).ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "bob", captured.Login)
		assert.Empty(t, captured.AvatarURL)
	})

	t.Run("missing header rejects with 401 JSON", func(t *testing.T) {
		called := false
		next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/prs", nil)
		TrustedHeader(next).ServeHTTP(rec, req)

		assert.False(t, called, "downstream handler must not run on missing header")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "unauthorized", body["error"])
	})

	t.Run("empty header value is treated as missing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/prs", nil)
		req.Header.Set(HeaderForwardedUser, "")
		TrustedHeader(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}
