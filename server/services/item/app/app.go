// Package app is the item service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/observability"
	"finga/pkg/panics"
	"finga/services/item/config"
	"finga/services/item/internal/application"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/repository"
)

// Build wires the item service; it fails fast on bad game data or an unreachable database.
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
	pool, err := dbx.NewPool(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return nil, err
	}
	items := application.NewService(repository.NewPostgres(pool), data)
	h := host.New(host.Options{
		Name:     "item",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Panics:   rec,
		Errors:   handler.Errors,
	})
	itemv1.RegisterItemServiceServer(h.GRPC, handler.New(items))
	h.OnClose(pool.Close)
	return h, nil
}
