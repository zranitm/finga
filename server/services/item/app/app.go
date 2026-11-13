// Package app is the item service's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/host"
	"finga/services/item"
	"finga/services/item/config"
	"finga/services/item/internal/handler"
)

// Build adds the item module to h from cfg and serves its facade over gRPC; it fails fast on bad game data or an unreachable database.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("item")
	if err != nil {
		return err
	}
	items, err := item.New(ctx, item.Deps{Module: m, Config: cfg, Data: data})
	if err != nil {
		return err
	}
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	itemv1.RegisterItemServiceServer(srv, handler.New(items))
	return nil
}
