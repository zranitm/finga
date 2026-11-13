// Package app is the character service's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/host"
	"finga/services/character"
	"finga/services/character/config"
	"finga/services/character/internal/clients"
	"finga/services/character/internal/handler"
)

// Build adds the character module to h from cfg and serves its facade over gRPC; it fails fast on an unreachable database.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("character")
	if err != nil {
		return err
	}
	itemConn, err := h.Dial("item", cfg.Dependencies.Item)
	if err != nil {
		return err
	}
	characters, err := character.New(ctx, character.Deps{Module: m, Data: data, Starter: clients.NewStarter(itemv1.NewItemServiceClient(itemConn))})
	if err != nil {
		return err
	}
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	characterv1.RegisterCharacterServiceServer(srv, handler.New(characters))
	return nil
}
