// Package app is the account service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/pkg/auth/password"
	"finga/pkg/dbx"
	"finga/pkg/observability"
	"finga/pkg/panics"
	"finga/pkg/uuidv7"
	"finga/services/account/config"
	"finga/services/account/internal/application"
	"finga/services/account/internal/handler"
	"finga/services/account/internal/repository"
)

// Build wires the account service; it fails fast on an unreachable database.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	panicCount, err := observability.NewPanicCounter(reg)
	if err != nil {
		return nil, err
	}
	rec := panics.NewRecorder(log, panicCount)
	pool, err := dbx.NewPool(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	streams := handler.NewStreams(uuidv7.NewString())
	accounts := application.NewService(repository.NewPostgres(pool), streams, application.Options{
		Hasher: password.NewHasher(hashCost(cfg.Password), cfg.Password.Concurrent, cfg.Password.Wait),
	})
	srv := handler.New(accounts, streams)
	h := host.New(host.Options{
		Name:     "account",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Panics:   rec,
		Errors:   handler.Errors,
	})
	accountv1.RegisterAccountServiceServer(h.GRPC, srv)
	h.OnClose(pool.Close)
	return h, nil
}

// hashCost is the configured Argon2id cost, with the library's salt and key lengths.
func hashCost(p config.Password) password.Params {
	return password.Params{Time: p.Time, Memory: p.MemoryKiB, Threads: p.Threads, SaltLen: password.Default.SaltLen, KeyLen: password.Default.KeyLen}
}
