// Package app is the world service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"

	"github.com/prometheus/client_golang/prometheus"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata/load"
	"finga/internal/host"
	"finga/internal/telemetry"
	"finga/pkg/uuidv7"
	"finga/services/world/config"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/application/party"
	"finga/services/world/internal/application/world"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/handler"
	"finga/services/world/internal/model"
)

// wholeSteps refuses a rate whose tick splits a movement step, as the client predicts in whole steps; the rate, unlike the interval, is exact.
func wholeSteps(hz int, dt float64) error {
	steps := 1 / (float64(hz) * dt)
	if steps < 1 || math.Abs(steps-math.Round(steps)) > 1e-9 {
		return fmt.Errorf("tick_rate_hz: a %d Hz tick holds %.3g movement steps of %.4gs; it must hold a whole number", hz, steps, dt)
	}
	return nil
}

// Build wires the world service from cfg.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	data, err := load.Dir(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if err := wholeSteps(cfg.TickRateHz, data.Movement().Dt); err != nil {
		return nil, err
	}
	h, err := host.New(host.Options{Name: "world", Settings: cfg.Host(), Logger: log, Registry: reg, Errors: handler.Errors, Roots: loot.EntryPoint})
	if err != nil {
		return nil, err
	}
	rec := h.Panics
	metrics, err := telemetry.NewWorldMetrics(reg, world.DropReasons())
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	characterConn, err := h.Dial("character", cfg.Dependencies.Character)
	if err != nil {
		h.Close()
		return nil, err
	}
	itemConn, err := h.Dial("item", cfg.Dependencies.Item)
	if err != nil {
		h.Close()
		return nil, err
	}
	socialConn, err := h.Dial("social", cfg.Dependencies.Social)
	if err != nil {
		h.Close()
		return nil, err
	}
	characters := characterv1.NewCharacterServiceClient(characterConn)
	items := itemv1.NewItemServiceClient(itemConn)

	nodeID := uuidv7.NewString()
	parties := party.NewCopy(party.Deps{Social: clients.NewSocial(socialv1.NewSocialServiceClient(socialConn), rec), Logger: log}, party.Options{World: nodeID})
	equipment := clients.NewEquipment(items)
	maps := world.New(world.Deps{
		Data:         data,
		NewRoll:      newRoll,
		Observer:     metrics,
		Items:        clients.NewLoot(items),
		Batches:      metrics,
		Equipment:    equipment,
		HandIns:      equipment,
		Potions:      equipment,
		HandedIns:    equipment,
		Wardrobe:     equipment,
		ItemCalls:    itemcalls.NewPool(itemcalls.Deps{Observer: metrics, Panics: rec}, itemcalls.Options{Workers: cfg.ItemCalls.Workers}),
		Parties:      parties,
		Traces:       h.Traces,
		Saver:        clients.NewSaver(characters),
		Characters:   clients.NewCharacters(characters),
		Starter:      clients.NewStarter(items),
		Panics:       rec,
		Logger:       log,
		SaveObserver: metrics,
	}, world.Tuning{TickInterval: cfg.TickInterval(), CheckpointInterval: cfg.CheckpointInterval, FlushTimeout: cfg.FlushTimeout, BatchMax: cfg.ItemCalls.BatchMax})
	h.Task("parties", parties.Run)
	h.Task("world", maps.Run)
	worldv1.RegisterWorldServiceServer(h.GRPC, handler.New(maps, handler.Options{BootID: nodeID, TickRateHz: cfg.TickRateHz}))
	log.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "mobs", len(data.Mobs()))
	return h, nil
}

// newRoll is a map's random source, seeded afresh: combat and drops are a game's chance, not a secret.
func newRoll() model.Roll {
	return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // game randomness
}
