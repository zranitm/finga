// Package app is the character service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
	"finga/pkg/panics"
	"finga/services/character/config"
	"finga/services/character/internal/application"
	"finga/services/character/internal/clients"
	"finga/services/character/internal/handler"
	"finga/services/character/internal/repository"
)

// Build wires the character service from cfg.
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
	pool, err := dbx.NewPool(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	itemConn, err := grpcx.Dial(cfg.Dependencies.Item)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("item service: %w", err)
	}
	characters := application.NewService(repository.NewPostgres(pool), data, application.Options{
		Starter: clients.NewStarter(itemv1.NewItemServiceClient(itemConn)),
		Logger:  log,
	})
	h := host.New(host.Options{
		Name:     "character",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Panics:   rec,
		Errors:   handler.Errors,
	})
	characterv1.RegisterCharacterServiceServer(h.GRPC, handler.New(characters))
	h.OnClose(pool.Close)
	h.OnClose(func() { _ = itemConn.Close() })
	return h, nil
}
