// Package app is the account service's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/pkg/retention"
	"finga/pkg/retention/retentionprom"
	"finga/pkg/uuidv7"
	"finga/services/account/config"
	"finga/services/account/internal/application"
	"finga/services/account/internal/handler"
	"finga/services/account/internal/password"
	"finga/services/account/internal/repository"
)

// Build adds the account module to h from cfg; it fails fast on an unreachable database.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	m, err := h.Module("account")
	if err != nil {
		return err
	}
	retentionMetrics, err := retentionprom.New(h.Registry(), "account")
	if err != nil {
		return err
	}
	pool, dbMetrics, err := m.Database(ctx)
	if err != nil {
		return err
	}
	streams := handler.NewStreams(uuidv7.NewString())
	accounts, err := application.NewService(application.Deps{
		Store:    repository.NewPostgres(pool, dbMetrics),
		Notifier: streams,
		Hasher:   password.NewHasher(hashCost(cfg.Password), cfg.Password.Concurrent, cfg.Password.Wait),
	})
	if err != nil {
		return err
	}
	ended := retention.Policy{Keep: cfg.Sessions.Keep, Every: cfg.Sessions.PruneEvery, Batch: cfg.Sessions.PruneBatch, Gap: cfg.Sessions.PruneGap}
	m.Task("session retention", func(ctx context.Context) error {
		return retention.Run(ctx, "sessions", ended, accounts.PruneSessions, retentionMetrics, m.Logger)
	}, host.Restartable)
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	accountv1.RegisterAccountServiceServer(srv, handler.New(accounts, streams))
	return nil
}

// hashCost is the configured Argon2id cost, with the library's salt and key lengths.
func hashCost(p config.Password) password.Params {
	return password.Params{Time: p.Time, Memory: p.MemoryKiB, Threads: p.Threads, SaltLen: password.Default.SaltLen, KeyLen: password.Default.KeyLen}
}
