// Package app is the world service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"

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
	"finga/pkg/observability"
	"finga/pkg/panics"
	"finga/pkg/runner"
	"finga/pkg/uuidv7"
	"finga/services/world/config"
	"finga/services/world/internal/application"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/handler"
	"finga/services/world/internal/model"
)

// Build wires the world service from cfg.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	panicCount, err := observability.NewPanicCounter(reg)
	if err != nil {
		return nil, err
	}
	rec := panics.NewRecorder(log, panicCount)
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

	nodeID := uuidv7.NewString()
	parties := clients.NewPartyCopy(socialv1.NewSocialServiceClient(socialConn), nodeID, log, rec)
	maps := application.New(data, application.Tuning{TickInterval: cfg.TickInterval(), CheckpointInterval: cfg.CheckpointInterval}, application.WorldDeps{
		NewRoll:    newRoll,
		Observer:   metrics,
		Items:      clients.NewPickups(items),
		Workers:    application.NewWorkers(cfg.Economy.Workers, metrics, rec),
		Parties:    parties,
		Saver:      clients.NewSaver(characters),
		Characters: clients.NewCharacters(characters),
		Starter:    clients.NewStarter(items),
		Panics:     rec,
		Logger:     log,
	})
	h := host.New(host.Options{
		Name:     "world",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Panics:   rec,
		Errors:   handler.Errors,
		Tasks: []runner.Task{
			{Name: "parties", Run: parties.Run},
			{Name: "world", Run: maps.Run},
		},
	})
	worldv1.RegisterWorldServiceServer(h.GRPC, handler.New(maps, handler.Options{BootID: nodeID, Logger: log}))
	h.OnClose(closeAll)
	log.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "mobs", len(data.Mobs()))
	return h, nil
}

// newRoll is a map's random source, seeded afresh: combat and drops are a game's chance, not a secret.
func newRoll() model.Roll {
	return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // game randomness
}
