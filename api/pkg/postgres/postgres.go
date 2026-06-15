package postgres

import (
	"context"
	"fmt"

	pgxlog "github.com/jackc/pgx-zerolog"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/rs/zerolog/log"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres connection string: %w", err)
	}

	config.ConnConfig.Tracer = &tracelog.TraceLog{
		Logger:   pgxlog.NewLogger(log.Logger),
		LogLevel: tracelog.LogLevelError,
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}
	return pool, nil
}
