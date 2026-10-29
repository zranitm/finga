// Command migrate applies every service's pending goose migrations to the
// configured database, each set with its own version table, then exits
// (Low-level design, page 10). It reads its own configuration,
// cmd/migrate/config/config.yaml: the database and the log level.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"finga/cmd/migrate/app"
	"finga/cmd/migrate/config"
	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/dbx"
	"finga/pkg/observability"
)

const connTimeout = 30 * time.Second

func main() { os.Exit(run()) }

func run() int {
	// Configuration first: there is no logger until the level is known, so
	// configuration errors go to stderr. The loader never echoes secrets.
	// Overrides come from the environment only; whoever starts the process
	// sets it (Compose under make up, make run exporting server/.env).
	cfg, err := configload.Load[config.Config](configload.Options{Path: configload.ResolvePath(config.Dir), EnvPrefix: host.EnvPrefix, Strict: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate: invalid configuration:", err)
		return 1
	}
	level, err := observability.ParseLevel(cfg.LogLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}
	log := observability.NewLogger(os.Stdout, level)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, connTimeout)
	defer cancel()
	pool, err := dbx.NewPool(connectCtx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		log.Error("database connection failed", "err", err)
		return 1
	}
	defer pool.Close()

	applied, err := app.Apply(ctx, pool)
	if err != nil {
		log.Error("migration failed", "err", err)
		return 1
	}
	for _, s := range app.Sets() {
		log.Info("migrations applied", "service", s.Service, "table", s.Table, "count", applied[s.Service])
	}
	return 0
}
