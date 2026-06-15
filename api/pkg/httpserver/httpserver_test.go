package httpserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestServer_RunAndShutdown(t *testing.T) {
	port := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)

	srv := New(addr, zerolog.Nop(), func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "pong")
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/ping")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 20*time.Millisecond, "server should accept requests")

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within 2s")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
