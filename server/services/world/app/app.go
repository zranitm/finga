// Package app is the world service's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/host"
	"finga/pkg/uuidv7"
	"finga/services/world"
	"finga/services/world/config"
	"finga/services/world/internal/handler"
)

// Build adds the world module to h from cfg and serves its facade over gRPC.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("world")
	if err != nil {
		return err
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
	bootID := uuidv7.NewString()
	maps, err := world.New(ctx, world.Deps{Module: m, Config: cfg, Data: data, ID: bootID, Remote: world.Remote{
		Character: characterv1.NewCharacterServiceClient(characterConn),
		Item:      itemv1.NewItemServiceClient(itemConn),
		Social:    socialv1.NewSocialServiceClient(socialConn),
	}})
	if err != nil {
		return err
	}
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	worldv1.RegisterWorldServiceServer(srv, handler.New(maps, handler.Options{BootID: bootID, TickRateHz: cfg.TickRateHz}))
	m.Logger.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "mobs", len(data.Mobs()))
	return nil
}
