package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/urfave/cli/v3"

	"github.com/jspdown/dashboard/api/migrations"
	"github.com/jspdown/dashboard/api/pkg/config"
	"github.com/jspdown/dashboard/api/pkg/dashboard"
	gh "github.com/jspdown/dashboard/api/pkg/github"
	"github.com/jspdown/dashboard/api/pkg/httpserver"
	"github.com/jspdown/dashboard/api/pkg/logging"
	"github.com/jspdown/dashboard/api/pkg/postgres"
)

func main() {
	os.Exit(run())
}

func run() int {
	cmd := &cli.Command{
		Name:  "dashboard",
		Usage: "Dashboard API",
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "start the HTTP API",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "addr",
						Value:   ":8080",
						Usage:   "address the HTTP server listens on",
						Sources: cli.EnvVars("DASHBOARD_API_ADDR"),
					},
					&cli.StringFlag{
						Name:    "log-level",
						Value:   "info",
						Usage:   "log level (trace, debug, info, warn, error)",
						Sources: cli.EnvVars("DASHBOARD_API_LOG_LEVEL"),
					},
					&cli.StringFlag{
						Name:     "database-url",
						Usage:    "postgres connection string",
						Sources:  cli.EnvVars("DASHBOARD_DATABASE_URL"),
						Required: true,
					},
					&cli.StringFlag{
						Name:     "github-token",
						Usage:    "GitHub personal access token",
						Sources:  cli.EnvVars("DASHBOARD_GITHUB_TOKEN"),
						Required: true,
					},
					&cli.StringFlag{
						Name:     "config",
						Usage:    "path to the YAML configuration file",
						Sources:  cli.EnvVars("DASHBOARD_CONFIG"),
						Required: true,
					},
					&cli.StringFlag{
						Name:    "web-dir",
						Value:   "/web",
						Usage:   "directory containing the built frontend",
						Sources: cli.EnvVars("DASHBOARD_WEB_DIR"),
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return serve(ctx, buildConfig(cmd))
				},
			},
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cmd.Run(ctx, os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// Config holds the serve command's runtime settings, sourced from CLI
// flags and their backing environment variables.
type Config struct {
	Addr        string
	LogLevel    string
	DatabaseURL string
	GitHubToken string
	ConfigPath  string
	WebDir      string
}

// buildConfig collects the serve flag values into a Config.
func buildConfig(cmd *cli.Command) Config {
	return Config{
		Addr:        cmd.String("addr"),
		LogLevel:    cmd.String("log-level"),
		DatabaseURL: cmd.String("database-url"),
		GitHubToken: cmd.String("github-token"),
		ConfigPath:  cmd.String("config"),
		WebDir:      cmd.String("web-dir"),
	}
}

func serve(ctx context.Context, conf Config) error {
	logger := logging.New(conf.LogLevel)

	cfg, err := config.Load(conf.ConfigPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	connConfig, err := pgx.ParseConfig(conf.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parsing database url: %w", err)
	}
	if err = migrations.MigrateUp(connConfig); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	logger.Info().Msg("Migrations applied")

	pool, err := postgres.NewPool(ctx, conf.DatabaseURL)
	if err != nil {
		return fmt.Errorf("creating postgres pool: %w", err)
	}
	defer pool.Close()

	ghClient, ghUser, err := gh.NewClient(ctx, conf.GitHubToken)
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}
	logger.Info().Str("login", ghUser.GetLogin()).Msg("GitHub token authenticated")

	// Repos are per-user now: the poller discovers the union of everyone's
	// subscriptions from the database and verifies access lazily as it polls.
	app := dashboard.New(dashboard.Deps{
		Config:       cfg,
		Pool:         pool,
		GitHubClient: ghClient,
		Logger:       logger,
		WebDir:       conf.WebDir,
	})

	go app.Poller.Run(ctx)

	srv := httpserver.New(conf.Addr, logger, func(r chi.Router) {
		r.Mount("/", app)
	})
	return srv.Run(ctx)
}
