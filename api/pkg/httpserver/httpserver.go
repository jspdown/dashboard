package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

type Server struct {
	addr   string
	logger zerolog.Logger
	mount  func(chi.Router)
}

func New(addr string, logger zerolog.Logger, mount func(chi.Router)) *Server {
	return &Server{addr: addr, logger: logger, mount: mount}
}

func (s *Server) Run(ctx context.Context) error {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(RequestLogger(s.logger))
	s.mount(r)

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info().Str("addr", s.addr).Msg("Listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info().Msg("Shutting down")
		// Use a fresh context: ctx is already cancelled, but Shutdown needs a
		// live deadline to drain in-flight requests.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}
