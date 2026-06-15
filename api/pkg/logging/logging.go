package logging

import (
	"io"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

func New(level string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}

	var out io.Writer = os.Stderr
	if isatty.IsTerminal(os.Stderr.Fd()) {
		out = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	}

	return zerolog.New(out).Level(lvl).With().Timestamp().Logger()
}
