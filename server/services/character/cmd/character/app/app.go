// Package app is the character service's composition root (Low-level
// design, page 10): game data, the pool, the item client for starter items,
// the character module, and its gRPC handler on a host. It is a package of
// its own, next to the main, because the end-to-end tests build the service
// exactly as production does.
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
	"finga/services/character/config"
	"finga/services/character/internal/application"
	"finga/services/character/internal/clients"
	"finga/services/character/internal/handler"
	"finga/services/character/internal/repository"
)

// Build wires the character service from configuration. It fails fast on
// bad game data or an unreachable database; the item service is dialled
// lazily, and creation retries a grant it cannot reach.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	data, err := gamedata.Load(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	pool, err := dbx.NewPool(ctx, cfg.Database.URL, cfg.Database.MaxConns)
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
		Errors:   handler.Errors,
	})
	characterv1.RegisterCharacterServiceServer(h.GRPC, handler.New(characters))
	h.OnClose(pool.Close)
	h.OnClose(func() { _ = itemConn.Close() })
	return h, nil
}
