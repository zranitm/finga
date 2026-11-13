// Package app is the world service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/pkg/uuidv7"
	"finga/services/world/config"
	"finga/services/world/internal/application/announce"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/application/party"
	"finga/services/world/internal/application/world"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/handler"
	"finga/services/world/internal/metrics"
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

// actionsLastATick refuses a tick that outlasts an action: a basic attack or an active skill takes at least a tick, so its points fall inside it.
func actionsLastATick(data *gamedata.Data, hz int, tick time.Duration) error {
	for _, c := range data.Classes() {
		if c.Attack.Length < tick {
			return fmt.Errorf("tick_rate_hz: a %d Hz tick lasts %s, longer than class %s's %s attack; every action must last at least a tick", hz, tick, c.ID, c.Attack.Length)
		}
	}
	for _, s := range data.Skills() {
		for _, st := range s.Stages {
			if s.Kind == gamedata.Active && st.Length < tick {
				return fmt.Errorf("tick_rate_hz: a %d Hz tick lasts %s, longer than skill %s's %s at %s; every action must last at least a tick", hz, tick, s.ID, st.Length, st.Name)
			}
		}
	}
	return nil
}

// Build adds the world module to h from cfg.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	if err := wholeSteps(cfg.TickRateHz, data.Movement().Dt); err != nil {
		return err
	}
	if err := actionsLastATick(data, cfg.TickRateHz, cfg.TickInterval()); err != nil {
		return err
	}
	m, err := h.Module("world")
	if err != nil {
		return err
	}
	m.TraceRoots(loot.EntryPoint)
	log, rec := m.Logger, m.Panics
	meters, err := metrics.New(h.Registry(), world.DropReasons())
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	characterConn, err := h.Dial("character", cfg.Dependencies.Character)
	if err != nil {
		return err
	}
	itemConn, err := h.Dial("item", cfg.Dependencies.Item)
	if err != nil {
		return err
	}
	socialConn, err := h.Dial("social", cfg.Dependencies.Social)
	if err != nil {
		return err
	}
	characters := characterv1.NewCharacterServiceClient(characterConn)
	items := itemv1.NewItemServiceClient(itemConn)

	nodeID := uuidv7.NewString()
	social := clients.NewSocial(socialv1.NewSocialServiceClient(socialConn), rec)
	parties := party.NewCopy(party.Deps{Social: social, Logger: log}, party.Options{World: nodeID})
	notices := announce.New(announce.Deps{Social: social, Logger: log})
	equipment := clients.NewEquipment(items)
	itemCalls := itemcalls.NewPool(itemcalls.Deps{Observer: meters, Panics: rec}, itemcalls.Options{Workers: cfg.ItemCalls.Workers})
	maps := world.New(world.Deps{
		Data:         data,
		NewRoll:      newRoll,
		Observer:     meters,
		Items:        clients.NewLoot(items),
		Batches:      meters,
		Equipment:    equipment,
		HandIns:      equipment,
		Gifts:        equipment,
		Potions:      equipment,
		HandedIns:    equipment,
		Wardrobe:     equipment,
		ItemCalls:    itemCalls,
		Parties:      parties,
		Notices:      notices,
		Traces:       h.Traces,
		Saver:        clients.NewSaver(characters),
		Characters:   clients.NewCharacters(characters),
		Starter:      clients.NewStarter(items),
		Panics:       rec,
		Logger:       log,
		SaveObserver: meters,
	}, world.Tuning{TickInterval: cfg.TickInterval(), CheckpointInterval: cfg.CheckpointInterval, FlushTimeout: cfg.FlushTimeout, BatchMax: cfg.ItemCalls.BatchMax})
	m.Task("parties", parties.Run, host.Restartable)
	m.Task("announcements", notices.Run, host.Restartable)
	m.Task("world", maps.Run, host.Vital)
	m.Task("saves", maps.Save, host.Vital)
	m.Task("item calls", itemCalls.Run, host.Vital)
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	worldv1.RegisterWorldServiceServer(srv, handler.New(maps, handler.Options{BootID: nodeID, TickRateHz: cfg.TickRateHz}))
	log.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "mobs", len(data.Mobs()))
	return nil
}

// newRoll is a map's random source, seeded afresh: combat and drops are a game's chance, not a secret.
func newRoll() model.Roll {
	return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // game randomness
}
