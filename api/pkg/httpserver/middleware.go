package httpserver

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

func RequestLogger(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			status := ww.Status()
			level := zerolog.InfoLevel
			switch {
			case status >= 500:
				level = zerolog.ErrorLevel
			case status >= 400:
				level = zerolog.WarnLevel
			}

			logger.WithLevel(level).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", status).
				Int64("duration_ms", time.Since(start).Milliseconds()).
				Str("request_id", middleware.GetReqID(r.Context())).
				Msg("HTTP request")
		})
	}
}
