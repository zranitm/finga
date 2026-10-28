package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	accountv1 "finga/api/gen/account/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	"finga/internal/application/character"
	"finga/internal/application/world"
	"finga/internal/config"
	"finga/internal/gamedata"
	"finga/internal/handler/api"
	"finga/internal/handler/ws"
	"finga/internal/telemetry"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
)

// BuildOptions is everything Build needs.
type BuildOptions struct {
	Config   config.Config
	Logger   *slog.Logger
	Registry *prometheus.Registry
}

// Build wires the game server as production runs it: game data, the
// database pool, a client for each extracted service (page 10: the item
// service so far), each remaining module's store and service with options
// filled from configuration, the world, the REST handler and the gateway,
// metrics, and the server. It fails fast on bad data or an unreachable
// database; a service it dials is reached lazily, and a call fails while it
// is down.
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
	metrics, err := telemetry.NewMetrics(opts.Registry)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("metrics: %w", err)
	}

	itemConn, err := grpcx.Dial(cfg.Services.Item.Addr)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("item service: %w", err)
	}
	items := itemv1.NewItemServiceClient(itemConn)
	accountConn, err := grpcx.Dial(cfg.Services.Account.Addr)
	if err != nil {
		pool.Close()
		_ = itemConn.Close()
		return nil, fmt.Errorf("account service: %w", err)
	}
	accounts := accountv1.NewAccountServiceClient(accountConn)
	socialConn, err := grpcx.Dial(cfg.Services.Social.Addr)
	if err != nil {
		pool.Close()
		_ = itemConn.Close()
		_ = accountConn.Close()
		return nil, fmt.Errorf("social service: %w", err)
	}
	social := socialv1.NewSocialServiceClient(socialConn)

	characters := character.NewService(character.NewPostgres(pool), data, character.Options{Starter: starter{items}, Logger: opts.Logger})
	parties := newPartyCopy(social, uuid.NewString(), opts.Logger)
	maps := world.New(data, characters, opts.Logger, world.Options{
		TickInterval:       cfg.World.TickInterval(),
		CheckpointInterval: cfg.World.CheckpointInterval,
		Observer:           metrics,
		Items:              pickups{items},
		Workers:            world.NewWorkers(cfg.Economy.Workers, metrics),
		Parties:            parties,
	})

	restAPI := api.NewHandler(api.Dependencies{
		Accounts:    accounts,
		Characters:  characters,
		Sessions:    guard{accounts},
		Logger:      opts.Logger,
		CORSOrigins: cfg.Server.CORSOrigins,
	})
	gateway := ws.New(ws.Dependencies{
		ID:          uuid.NewString(),
		Sessions:    guard{accounts},
		Watcher:     accounts,
		Characters:  characters,
		Items:       items,
		Social:      social,
		World:       worldAdapter{maps},
		Logger:      opts.Logger,
		Observer:    metrics,
		CORSOrigins: cfg.Server.CORSOrigins,
	})
	s := New(Options{Config: cfg.Server, Logger: opts.Logger}, Deps{
		API:     restAPI,
		Gateway: gateway,
		Metrics: observability.MetricsHandler(opts.Registry),
		Tasks: []Task{
			{Name: "parties", Run: parties.Run},
			{Name: "world", Run: maps.Run},
			{Name: "persistence", Run: func(ctx context.Context) error { maps.RunPersistence(ctx); return nil }},
			{Name: "sessions", Run: gateway.Run},
		},
	})
	s.closers = append(s.closers, pool.Close, func() { _ = itemConn.Close() }, func() { _ = accountConn.Close() }, func() { _ = socialConn.Close() })
	opts.Logger.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "items", len(data.Items()), "mobs", len(data.Mobs()))
	return s, nil
}

// worldAdapter narrows *world.World to the gateway's World interface.
type worldAdapter struct{ w *world.World }

// Instance finds a map actor as a gateway MapInstance.
func (a worldAdapter) Instance(mapID string) (ws.MapInstance, error) {
	return a.w.Instance(mapID)
}
