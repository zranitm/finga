// Package app is the account service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/uuidv7"
	"finga/services/account/config"
	"finga/services/account/internal/application"
	"finga/services/account/internal/handler"
	"finga/services/account/internal/repository"
)

// Build wires the account service; it fails fast on an unreachable database.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	pool, err := dbx.NewPool(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return nil, err
	}
	var srv *handler.Server // built below; the hooks only fire once requests arrive
	accounts := application.NewService(repository.NewPostgres(pool), application.Options{
		OnSessionRevoked: func(ctx context.Context, id uuid.UUID, reason string) { srv.SessionRevoked(ctx, id, reason) },
		OnAccountRevoked: func(ctx context.Context, id uuid.UUID, reason string) { srv.AccountRevoked(ctx, id, reason) },
	})
	srv = handler.New(accounts, uuidv7.NewString())
	h := host.New(host.Options{
		Name:     "account",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Errors:   handler.Errors,
	})
	accountv1.RegisterAccountServiceServer(h.GRPC, srv)
	h.OnClose(pool.Close)
	return h, nil
}
