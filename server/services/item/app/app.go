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
	"finga/pkg/idempotency/idempotencypg"
	"finga/pkg/outbox/outboxpg"
	"finga/pkg/retention"
	"finga/services/item/config"
	"finga/services/item/internal/application"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/repository"
)

// outboxCountEvery is how often the finga_outbox_unpublished gauge is counted again.
const outboxCountEvery = 15 * time.Second

// Build wires the item service; it fails fast on bad game data or an unreachable database.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	data, err := gamedata.Load(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	h, err := host.New(host.Options{Name: "item", Settings: cfg.Host(), Logger: log, Registry: reg, Errors: handler.Errors})
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
	pool, dbMetrics, err := h.Database(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	items := application.NewService(application.Deps{Store: repository.NewPostgres(pool, dbMetrics), Content: data})
	h.Task("outbox gauge", func(ctx context.Context) error {
		return outboxMetrics.Track(ctx, outboxCountEvery, func(ctx context.Context) (int64, error) { return outboxpg.Unpublished(ctx, pool) }, log)
	})
	keys := retention.Policy{Keep: cfg.IdempotencyKeys.Keep, Every: cfg.IdempotencyKeys.PruneEvery, Batch: cfg.IdempotencyKeys.PruneBatch}
	pruneKeys := func(ctx context.Context, before time.Time, limit int) (int64, error) {
		return idempotencypg.Prune(ctx, pool, before, limit)
	}
	h.Task("idempotency key retention", func(ctx context.Context) error {
		return retention.Run(ctx, "idempotency_keys", keys, pruneKeys, retentionMetrics, log)
	})
	itemv1.RegisterItemServiceServer(h.GRPC, handler.New(items))
	return h, nil
}
