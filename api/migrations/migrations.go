package migrations

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxdriver "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

//go:embed *.sql
var migrationFS embed.FS

func MigrateUp(config *pgx.ConnConfig) error {
	migrator, stop, err := buildMigrator(config)
	if err != nil {
		return fmt.Errorf("building migrator: %w", err)
	}
	defer func() { _ = stop() }()

	if err = migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running up migrations: %w", err)
	}

	return nil
}

func MigrateDown(config *pgx.ConnConfig, steps int) error {
	migrator, stop, err := buildMigrator(config)
	if err != nil {
		return fmt.Errorf("building migrator: %w", err)
	}
	defer func() { _ = stop() }()

	if err = migrator.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running down migrations: %w", err)
	}

	return nil
}

func buildMigrator(config *pgx.ConnConfig) (*migrate.Migrate, func() error, error) {
	source, err := iofs.New(migrationFS, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("reading migrations: %w", err)
	}
	closeSource := func() error { return source.Close() }

	db := stdlib.OpenDB(*config)
	closeAll := func() error {
		return errors.Join(closeSource(), db.Close())
	}

	driver, err := pgxdriver.WithInstance(db, &pgxdriver.Config{MigrationsTable: "migrations"})
	if err != nil {
		_ = closeAll()
		return nil, nil, fmt.Errorf("creating driver: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		_ = closeAll()
		return nil, nil, fmt.Errorf("creating migrator: %w", err)
	}

	migrator.Log = &logger{logger: log.Logger}
	return migrator, closeAll, nil
}

type logger struct {
	logger zerolog.Logger
}

func (l *logger) Printf(format string, v ...any) {
	l.logger.Info().Msgf(format, v...)
}

func (l *logger) Verbose() bool {
	return l.logger.GetLevel() <= zerolog.DebugLevel
}
