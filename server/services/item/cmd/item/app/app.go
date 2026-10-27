// Package app is the item service's composition root (Low-level design,
// page 10): game data, the pool, the item module, and its gRPC handler on
// a host. It is a package of its own, next to the main, because the
// end-to-end tests build the service exactly as production does.
package app

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/config"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/item"
)

// Build wires the item service from configuration. It fails fast on bad
// game data or an unreachable database.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	data, err := gamedata.Load(cfg.World.DataDir)
	if err != nil {
		return nil, err
	}
	pool, err := dbx.NewPool(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return nil, err
	}
	items := item.NewService(item.NewPostgres(pool), data, item.Options{})
	h := host.New(host.Options{
		Name:          "item",
		Endpoint:      cfg.Services.Item,
		ShutdownGrace: cfg.Server.ShutdownGrace,
		Logger:        log,
		Registry:      reg,
		Errors:        handler.Errors,
	})
	itemv1.RegisterItemServiceServer(h.GRPC, handler.New(items))
	h.OnClose(pool.Close)
	return h, nil
}
