// Package app is the item service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/telemetry"
	"finga/pkg/dbx"
	"finga/pkg/idempotency"
	"finga/pkg/observability"
	"finga/pkg/outbox"
	"finga/pkg/panics"
	"finga/pkg/retention"
	"finga/pkg/runner"
	"finga/services/item/config"
	"finga/services/item/internal/application"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/repository"
)

// outboxCountEvery is how often the outbox_unpublished gauge is counted again.
const outboxCountEvery = 15 * time.Second

// Build wires the item service; it fails fast on bad game data or an unreachable database.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	panicCount, err := observability.NewPanicCounter(reg)
	if err != nil {
		return nil, err
	}
	rec := panics.NewRecorder(log, panicCount)
	dbMetrics, err := telemetry.NewDBMetrics(reg)
	if err != nil {
		return nil, err
	}
	outboxMetrics, err := telemetry.NewOutboxMetrics(reg)
	if err != nil {
		return nil, err
	}
	retentionMetrics, err := telemetry.NewRetentionMetrics(reg)
	if err != nil {
		return nil, err
	}
	data, err := gamedata.Load(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	pool, err := dbx.NewPool(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	items := application.NewService(repository.NewPostgres(pool, dbMetrics), data)
	countOutbox := func(ctx context.Context) (int64, error) { return outbox.Unpublished(ctx, pool) }
	pruneKeys := func(ctx context.Context, before time.Time, limit int) (int64, error) {
		return idempotency.Prune(ctx, pool, before, limit)
	}
	keys := retention.Policy{Keep: cfg.IdempotencyKeys.Keep, Every: cfg.IdempotencyKeys.PruneEvery, Batch: cfg.IdempotencyKeys.PruneBatch}
	h := host.New(host.Options{
		Name:     "item",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Panics:   rec,
		Errors:   handler.Errors,
		Tasks: []runner.Task{
			{Name: "outbox gauge", Run: func(ctx context.Context) error {
				return outboxMetrics.Track(ctx, outboxCountEvery, countOutbox, log)
			}},
			{Name: "idempotency key retention", Run: func(ctx context.Context) error {
				return retention.Run(ctx, "idempotency_keys", keys, pruneKeys, retentionMetrics, log)
			}},
		},
	})
	itemv1.RegisterItemServiceServer(h.GRPC, handler.New(items))
	h.OnClose(pool.Close)
	return h, nil
}
