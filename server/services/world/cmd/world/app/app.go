// Package app is the world service's composition root (Low-level design,
// page 10): game data, the tick metrics, clients for the character, item
// and social services, the world with its adapters over them, and its gRPC
// handler on a host, with the instances, the persistence worker and the
// party copy as the host's tasks. It is a package of its own, next to the
// main, because the end-to-end tests build the service exactly as
// production does.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/telemetry"
	"finga/pkg/grpcx"
	"finga/pkg/runner"
	"finga/services/world/config"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/handler"
	"finga/services/world/internal/world"
)

// Build wires the world service from configuration. It needs no database:
// characters are saved through the character service and items through the
// item service, from workers, never inside a tick. It fails fast on bad game
// data; the services it calls are dialled lazily.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	data, err := gamedata.Load(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	metrics, err := telemetry.NewMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	var conns []*grpc.ClientConn
	closeAll := func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}
	dial := func(name, addr string) (*grpc.ClientConn, error) {
		c, err := grpcx.Dial(addr)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("%s service: %w", name, err)
		}
		conns = append(conns, c)
		return c, nil
	}
	characterConn, err := dial("character", cfg.Dependencies.Character)
	if err != nil {
		return nil, err
	}
	itemConn, err := dial("item", cfg.Dependencies.Item)
	if err != nil {
		return nil, err
	}
	socialConn, err := dial("social", cfg.Dependencies.Social)
	if err != nil {
		return nil, err
	}
	characters := characterv1.NewCharacterServiceClient(characterConn)
	items := itemv1.NewItemServiceClient(itemConn)

	nodeID := uuid.NewString()
	parties := clients.NewPartyCopy(socialv1.NewSocialServiceClient(socialConn), nodeID, log)
	maps := world.New(data, clients.NewSaver(characters), log, world.Options{
		TickInterval:       cfg.TickInterval(),
		CheckpointInterval: cfg.CheckpointInterval,
		Observer:           metrics,
		Items:              clients.NewPickups(items),
		Workers:            world.NewWorkers(cfg.Economy.Workers, metrics),
		Parties:            parties,
		Characters:         clients.NewCharacters(characters),
		Starter:            clients.NewStarter(items),
	})
	h := host.New(host.Options{
		Name:     "world",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Errors:   handler.Errors,
		Tasks: []runner.Task{
			{Name: "parties", Run: parties.Run},
			{Name: "world", Run: maps.Run},
			{Name: "persistence", Run: func(ctx context.Context) error { maps.RunPersistence(ctx); return nil }},
		},
	})
	worldv1.RegisterWorldServiceServer(h.GRPC, handler.New(maps, handler.Options{BootID: nodeID, Logger: log}))
	h.OnClose(closeAll)
	log.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "mobs", len(data.Mobs()))
	return h, nil
}
