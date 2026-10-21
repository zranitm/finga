package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/application/account"
	"finga/internal/application/character"
	"finga/internal/application/gamedata"
	"finga/internal/config"
	"finga/internal/handler/api"
	"finga/internal/telemetry"
	"finga/pkg/dbx"
	"finga/pkg/observability"
)

// BuildOptions is everything Build needs.
type BuildOptions struct {
	Config   config.Config
	Logger   *slog.Logger
	Registry *prometheus.Registry
}

// Build wires the game server as production runs it: game data, the
// database pool, each module's store and service with options filled from
// configuration, the REST handler, metrics, and the server. It fails fast
// on bad data or an unreachable database.
func Build(ctx context.Context, opts BuildOptions) (*Server, error) {
	cfg := opts.Config
	data, err := gamedata.Load(cfg.World.DataDir)
	if err != nil {
		return nil, err
	}
	pool, err := dbx.NewPool(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return nil, err
	}
	if _, err := telemetry.NewMetrics(opts.Registry); err != nil {
		pool.Close()
		return nil, fmt.Errorf("metrics: %w", err)
	}

	accounts := account.NewService(account.NewPostgres(pool), account.Options{})
	characters := character.NewService(character.NewPostgres(pool), data, character.Options{})

	restAPI := api.NewHandler(api.Dependencies{
		Accounts:    accounts,
		Characters:  characters,
		Sessions:    accounts,
		Logger:      opts.Logger,
		CORSOrigins: cfg.Server.CORSOrigins,
	})
	s := New(Options{Config: cfg.Server, Logger: opts.Logger}, Deps{
		API:     restAPI,
		Metrics: observability.MetricsHandler(opts.Registry),
		DB:      pool,
	})
	s.closers = append(s.closers, pool.Close)
	opts.Logger.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "items", len(data.Items()), "mobs", len(data.Mobs()))
	return s, nil
}
