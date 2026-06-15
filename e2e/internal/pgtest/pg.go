// Package pgtest hands out short-lived, isolated Postgres databases to
// e2e tests, sharing one container per test binary.
//
// The first Database call starts a testcontainer, migrates a "template"
// database, and marks it as a Postgres template. Later calls (including
// parallel ones) clone fresh databases via CREATE DATABASE ... TEMPLATE,
// roughly 10ms each instead of re-running migrations. Each test gets a
// unique name plus a t.Cleanup that drops it.
//
// Tests skip cleanly when Docker isn't reachable, so the suite stays
// usable on machines without a daemon.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jspdown/dashboard/api/migrations"
)

const (
	postgresImage = "postgres:17"
	templateDB    = "dashboard_template"
)

var (
	pool     *containerPool
	poolOnce sync.Once
	poolErr  error
)

type containerPool struct {
	container *tcpostgres.PostgresContainer
	adminDSN  string
	host      string
	port      string
}

// Database carves a fresh database off the shared container and returns
// its DSN; a t.Cleanup drops it. The caller opens a pool/conn on the
// DSN. The first call per binary starts the container and migrates the
// template (a few seconds); later calls clone in O(10ms). Skips the
// test (without failing) when Docker isn't reachable.
func Database(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dsn, cleanup, err := NewDatabase(ctx)
	if err != nil {
		// IsAvailable failures become a skip; everything else (e.g. a
		// CREATE DATABASE failure on a healthy container) is fatal: it
		// means the harness itself is broken.
		if errors.Is(err, errPgUnavailable) {
			t.Skipf("pgtest: skipping (Postgres unavailable): %v", err)
		}
		t.Fatalf("pgtest: create database: %v", err)
	}
	t.Cleanup(cleanup)
	return dsn
}

// NewDatabase carves a fresh database off the shared container and
// returns its DSN plus a cleanup func that drops it. This is the
// non-test entry point (dev-e2e); tests prefer Database, which threads
// cleanup through t.Cleanup.
//
// ctx bounds the CREATE DATABASE call; the returned cleanup uses its
// own short-timeout context so it runs even after ctx is canceled.
func NewDatabase(ctx context.Context) (dsn string, cleanup func(), err error) {
	poolOnce.Do(func() { pool, poolErr = bootstrap() })
	if poolErr != nil {
		return "", nil, fmt.Errorf("%w: %w", errPgUnavailable, poolErr)
	}

	name := "dashboard_test_" + randSuffix()
	if err := pool.execAdmin(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`, name, templateDB)); err != nil {
		return "", nil, fmt.Errorf("creating database %s: %w", name, err)
	}

	// Cleanup uses a fresh context, not the caller's: the caller's ctx
	// is usually canceled by the time cleanup runs (test returned,
	// dev-e2e signal received), but we still need to drop the database
	// to keep the container tidy across runs.
	cleanup = func() { //nolint:contextcheck // fresh context is intentional
		dropCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// DROP ... WITH (FORCE) terminates lingering connections
		// rather than failing on them. Best-effort: a leak doesn't
		// affect the next test (each gets a fresh database name).
		if err := pool.execAdmin(dropCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, name)); err != nil {
			fmt.Fprintf(os.Stderr, "pgtest: drop database %s: %v\n", name, err)
		}
	}
	return pool.dsn(name), cleanup, nil
}

// errPgUnavailable is returned when bootstrap fails (Docker missing
// or container fails to start). Test callers translate this into
// t.Skip; non-test callers surface it to the user.
var errPgUnavailable = errors.New("postgres testcontainer unavailable")

func bootstrap() (*containerPool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("starting postgres container: %w", err)
	}

	host, err := c.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("container host: %w", err)
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, fmt.Errorf("container port: %w", err)
	}

	pool := &containerPool{
		container: c,
		host:      host,
		port:      port.Port(),
	}
	pool.adminDSN = pool.dsn("postgres")

	if err := pool.buildTemplate(ctx); err != nil {
		_ = c.Terminate(context.Background())
		return nil, fmt.Errorf("building template database: %w", err)
	}

	return pool, nil
}

// buildTemplate creates the template database and migrates it once.
// Test databases clone from this template, so migrations run exactly
// once per binary no matter how many tests open a database.
//
// golang-migrate leaves background connections open after returning, so
// we forcibly close any stragglers before marking the template.
// Otherwise the first CREATE DATABASE ... TEMPLATE returns SQLSTATE
// 55006 ("source database is being accessed by other users").
func (p *containerPool) buildTemplate(ctx context.Context) error {
	if err := p.execAdmin(ctx, fmt.Sprintf(`CREATE DATABASE %s`, templateDB)); err != nil {
		return fmt.Errorf("creating template db: %w", err)
	}

	cfg, err := pgx.ParseConfig(p.dsn(templateDB))
	if err != nil {
		return fmt.Errorf("parsing template dsn: %w", err)
	}
	if err := migrations.MigrateUp(cfg); err != nil {
		return fmt.Errorf("migrating template: %w", err)
	}

	// Drain leftover sessions on the template, then disallow new ones.
	// Postgres requires the source database to have zero connections
	// when CREATE DATABASE ... TEMPLATE runs.
	if err := p.execAdmin(ctx, fmt.Sprintf(
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`,
		templateDB,
	)); err != nil {
		return fmt.Errorf("draining template sessions: %w", err)
	}
	if err := p.execAdmin(ctx, fmt.Sprintf(`ALTER DATABASE %s ALLOW_CONNECTIONS false`, templateDB)); err != nil {
		return fmt.Errorf("locking template: %w", err)
	}

	// Mark the template so CREATE DATABASE ... TEMPLATE is allowed for it.
	// Without this Postgres rejects clones from non-template databases unless
	// the caller is the owner.
	return p.execAdmin(ctx, fmt.Sprintf(`UPDATE pg_database SET datistemplate = true WHERE datname = '%s'`, templateDB))
}

func (p *containerPool) execAdmin(ctx context.Context, sql string) error {
	conn, err := pgx.Connect(ctx, p.adminDSN)
	if err != nil {
		return fmt.Errorf("connecting to admin db: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

func (p *containerPool) dsn(database string) string {
	return fmt.Sprintf("postgres://test:test@%s:%s/%s?sslmode=disable", p.host, p.port, database)
}

func randSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read on /dev/urandom virtually never fails; if it does fall
		// back to a timestamp so test setup doesn't crash.
		return strings.ReplaceAll(time.Now().UTC().Format("150405.000000"), ".", "")
	}
	return hex.EncodeToString(b[:])
}

// IsAvailable reports whether a Postgres testcontainer can be brought up
// in the current environment. Used by callers that want to skip a whole
// e2e harness setup (browser, bundle build, etc.) when Postgres isn't
// going to work anyway.
func IsAvailable() error {
	poolOnce.Do(func() { pool, poolErr = bootstrap() })
	return poolErr
}

// Cleanup terminates the shared container. Called from a TestMain after
// all tests in the package finish. Safe to call when bootstrap failed.
func Cleanup() {
	if pool == nil || pool.container == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.container.Terminate(ctx); err != nil && !errors.Is(err, context.Canceled) {
		// Best effort; container leaks are noisy but harmless in CI.
		fmt.Printf("pgtest: container terminate: %v\n", err)
	}
}
