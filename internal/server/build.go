package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/application/account"
	"finga/internal/application/character"
	"finga/internal/application/chat"
	"finga/internal/application/gamedata"
	"finga/internal/application/item"
	"finga/internal/application/party"
	"finga/internal/application/world"
	"finga/internal/config"
	"finga/internal/handler/api"
	"finga/internal/handler/ws"
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
// configuration, the world, the REST handler and the gateway, metrics, and
// the server. It fails fast on bad data or an unreachable database.
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

	accounts := account.NewService(account.NewPostgres(pool), account.Options{})
	itemStore := item.NewPostgres(pool)
	items := item.NewService(itemStore, data, item.Options{})
	characters := character.NewService(character.NewPostgres(pool, func(tx pgx.Tx) character.StarterItems {
		return items.Within(itemStore.UnitOfWork(tx))
	}), data, character.Options{})
	parties := party.NewService(party.Options{})
	chatSvc := chat.NewService(parties, chat.Options{})
	maps := world.New(data, characters, opts.Logger, world.Options{
		TickInterval:       cfg.World.TickInterval(),
		CheckpointInterval: cfg.World.CheckpointInterval,
		Observer:           metrics,
		Items:              items,
		Workers:            world.NewWorkers(cfg.Economy.Workers, metrics),
		Parties:            parties,
	})

	restAPI := api.NewHandler(api.Dependencies{
		Accounts:    accounts,
		Characters:  characters,
		Sessions:    accounts,
		Logger:      opts.Logger,
		CORSOrigins: cfg.Server.CORSOrigins,
	})
	gateway := ws.New(ws.Dependencies{
		Sessions:    accounts,
		Characters:  characters,
		Items:       items,
		Chat:        chatSvc,
		Parties:     parties,
		World:       worldAdapter{maps},
		Logger:      opts.Logger,
		Observer:    metrics,
		CORSOrigins: cfg.Server.CORSOrigins,
	})
	parties.Notify(gateway) // PartyState and invites go out through the gateway
	s := New(Options{Config: cfg.Server, Logger: opts.Logger}, Deps{
		API:     restAPI,
		Gateway: gateway,
		Metrics: observability.MetricsHandler(opts.Registry),
		DB:      pool,
		Tasks: []Task{
			{Name: "world", Run: maps.Run},
			{Name: "persistence", Run: func(ctx context.Context) error { maps.RunPersistence(ctx); return nil }},
		},
	})
	s.closers = append(s.closers, pool.Close)
	opts.Logger.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "items", len(data.Items()), "mobs", len(data.Mobs()))
	return s, nil
}

// worldAdapter narrows *world.World to the gateway's World interface.
type worldAdapter struct{ w *world.World }

// Instance finds a map actor as a gateway MapInstance.
func (a worldAdapter) Instance(mapID string) (ws.MapInstance, error) {
	return a.w.Instance(mapID)
}
