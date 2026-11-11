// Package app is the character service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata/load"
	"finga/internal/host"
	"finga/services/character/config"
	"finga/services/character/internal/application"
	"finga/services/character/internal/clients"
	"finga/services/character/internal/handler"
	"finga/services/character/internal/repository"
)

// Build wires the character service from cfg.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	data, err := load.Dir(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	h, err := host.New(host.Options{Name: "character", Settings: cfg.Host(), Logger: log, Registry: reg, Errors: handler.Errors})
	if err != nil {
		return nil, err
	}
	pool, dbMetrics, err := h.Database(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	itemConn, err := h.Dial("item", cfg.Dependencies.Item)
	if err != nil {
		h.Close()
		return nil, err
	}
	characters := application.NewService(application.Deps{
		Store:   repository.NewPostgres(pool, dbMetrics),
		Content: data,
		Starter: clients.NewStarter(itemv1.NewItemServiceClient(itemConn)),
		Logger:  log,
	})
	characterv1.RegisterCharacterServiceServer(h.GRPC, handler.New(characters))
	return h, nil
}
