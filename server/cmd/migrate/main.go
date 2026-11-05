// Command migrate applies every service's pending goose migrations, then exits.
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
	"finga/pkg/logger"
)

const connTimeout = 30 * time.Second

func main() { os.Exit(run()) }

func run() int {
	// No logger until its section is read, so configuration errors go to stderr.
	cfg, err := configload.Load[config.Config](configload.Options{Path: configload.ResolvePath(config.Dir), EnvPrefix: host.EnvPrefix, Strict: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate: invalid configuration:", err)
		return 1
	}
	log, err := logger.New(os.Stdout, cfg.Log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, connTimeout)
	defer cancel()
	pool, err := dbx.NewPool(connectCtx, cfg.Database)
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
