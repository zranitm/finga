// Package app is the account service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/internal/telemetry"
	"finga/pkg/auth/password"
	"finga/pkg/retention"
	"finga/pkg/uuidv7"
	"finga/services/account/config"
	"finga/services/account/internal/application"
	"finga/services/account/internal/handler"
	"finga/services/account/internal/repository"
)

// Build wires the account service; it fails fast on an unreachable database.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	h, err := host.New(host.Options{Name: "account", Settings: cfg.Host(), Logger: log, Registry: reg, Errors: handler.Errors})
	if err != nil {
		return nil, err
	}
	retentionMetrics, err := telemetry.NewRetentionMetrics(reg)
	if err != nil {
		return nil, err
	}
	pool, dbMetrics, err := h.Database(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	streams := handler.NewStreams(uuidv7.NewString())
	accounts := application.NewService(application.Deps{
		Store:    repository.NewPostgres(pool, dbMetrics),
		Notifier: streams,
		Hasher:   password.NewHasher(hashCost(cfg.Password), cfg.Password.Concurrent, cfg.Password.Wait),
	})
	ended := retention.Policy{Keep: cfg.Sessions.Keep, Every: cfg.Sessions.PruneEvery, Batch: cfg.Sessions.PruneBatch}
	h.Task("session retention", func(ctx context.Context) error {
		return retention.Run(ctx, "sessions", ended, accounts.PruneSessions, retentionMetrics, log)
	})
	accountv1.RegisterAccountServiceServer(h.GRPC, handler.New(accounts, streams))
	return h, nil
}

// hashCost is the configured Argon2id cost, with the library's salt and key lengths.
func hashCost(p config.Password) password.Params {
	return password.Params{Time: p.Time, Memory: p.MemoryKiB, Threads: p.Threads, SaltLen: password.Default.SaltLen, KeyLen: password.Default.KeyLen}
}
