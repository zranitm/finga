package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	"finga/internal/application/world"
	"finga/internal/config"
	"finga/internal/gamedata"
	"finga/internal/handler/api"
	"finga/internal/handler/ws"
	"finga/internal/telemetry"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
)

// BuildOptions is everything Build needs.
type BuildOptions struct {
	Config   config.Config
	Logger   *slog.Logger
	Registry *prometheus.Registry
}

// Build wires the game server as production runs it: game data, a client
// for each extracted service (page 10: account, character, item and social
// so far), the world with its adapters over them, the REST handler and the
// gateway, metrics, and the server. It needs no database: every table
// belongs to a service. It fails fast on bad data; a service it dials is
// reached lazily, and a call fails while it is down.
func Build(ctx context.Context, opts BuildOptions) (*Server, error) {
	cfg := opts.Config
	data, err := gamedata.Load(cfg.World.DataDir)
	if err != nil {
		return nil, err
	}
	metrics, err := telemetry.NewMetrics(opts.Registry)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	conns, err := dialAll(cfg.Services)
	if err != nil {
		return nil, err
	}
	items := itemv1.NewItemServiceClient(conns.item)
	accounts := accountv1.NewAccountServiceClient(conns.account)
	social := socialv1.NewSocialServiceClient(conns.social)
	characters := characterv1.NewCharacterServiceClient(conns.character)

	parties := newPartyCopy(social, uuid.NewString(), opts.Logger)
	maps := world.New(data, saver{characters}, opts.Logger, world.Options{
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
	s.closers = append(s.closers, conns.close)
	opts.Logger.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "items", len(data.Items()), "mobs", len(data.Mobs()))
	return s, nil
}

// worldAdapter narrows *world.World to the gateway's World interface.
type worldAdapter struct{ w *world.World }

// Instance finds a map actor as a gateway MapInstance.
func (a worldAdapter) Instance(mapID string) (ws.MapInstance, error) {
	return a.w.Instance(mapID)
}

// conns are the game server's connections to the extracted services.
type conns struct {
	account, character, item, social *grpc.ClientConn
}

// dialAll makes a connection to each service; nothing connects until the
// first call.
func dialAll(s config.Services) (conns, error) {
	var c conns
	for _, d := range []struct {
		name string
		addr string
		conn **grpc.ClientConn
	}{
		{"account", s.Account.Addr, &c.account},
		{"character", s.Character.Addr, &c.character},
		{"item", s.Item.Addr, &c.item},
		{"social", s.Social.Addr, &c.social},
	} {
		conn, err := grpcx.Dial(d.addr)
		if err != nil {
			c.close()
			return conns{}, fmt.Errorf("%s service: %w", d.name, err)
		}
		*d.conn = conn
	}
	return c, nil
}

func (c conns) close() {
	for _, conn := range []*grpc.ClientConn{c.account, c.character, c.item, c.social} {
		if conn != nil {
			_ = conn.Close()
		}
	}
}
