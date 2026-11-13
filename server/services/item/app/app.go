// Package app is the item service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"time"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/host"
	"finga/pkg/idempotency/idempotencypg"
	"finga/pkg/outbox/outboxpg"
	"finga/pkg/outbox/outboxprom"
	"finga/pkg/retention"
	"finga/pkg/retention/retentionprom"
	"finga/services/item/config"
	"finga/services/item/internal/application"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/repository"
)

// outboxCountEvery is how often the finga_outbox_unpublished gauge is counted again.
const outboxCountEvery = 15 * time.Second

// Build adds the item module to h from cfg; it fails fast on bad game data or an unreachable database.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("item")
	if err != nil {
		return err
	}
	log := m.Logger
	outboxMetrics, err := outboxprom.New(h.Registry(), "item")
	if err != nil {
		return err
	}
	retentionMetrics, err := retentionprom.New(h.Registry(), "item")
	if err != nil {
		return err
	}
	pool, dbMetrics, err := m.Database(ctx)
	if err != nil {
		return err
	}
	items := application.NewService(application.Deps{Store: repository.NewPostgres(pool, dbMetrics), Content: data, Logger: log})
	m.Task("outbox gauge", func(ctx context.Context) error {
		return outboxMetrics.Track(ctx, outboxCountEvery, func(ctx context.Context) (int64, error) { return outboxpg.Unpublished(ctx, pool) }, log)
	}, host.Restartable)
	keys := retention.Policy{Keep: cfg.IdempotencyKeys.Keep, Every: cfg.IdempotencyKeys.PruneEvery, Batch: cfg.IdempotencyKeys.PruneBatch, Gap: cfg.IdempotencyKeys.PruneGap}
	pruneKeys := func(ctx context.Context, before time.Time, limit int) (int64, error) {
		return idempotencypg.Prune(ctx, pool, before, limit)
	}
	m.Task("idempotency key retention", func(ctx context.Context) error {
		return retention.Run(ctx, "idempotency_keys", keys, pruneKeys, retentionMetrics, log)
	}, host.Restartable)
	outboxRows := func(keep time.Duration) retention.Policy {
		return retention.Policy{Keep: keep, Every: cfg.Outbox.PruneEvery, Batch: cfg.Outbox.PruneBatch, Gap: cfg.Outbox.PruneGap}
	}
	m.Task("outbox published retention", func(ctx context.Context) error {
		prune := func(ctx context.Context, before time.Time, limit int) (int64, error) {
			return outboxpg.PrunePublished(ctx, pool, before, limit)
		}
		return retention.Run(ctx, "outbox", outboxRows(cfg.Outbox.PublishedKeep), prune, retentionMetrics, log.With("rows", "published"))
	}, host.Restartable)
	if cfg.Outbox.UnpublishedKeep > 0 { // Stage 1 only: with Stage 2's relay it would delete events not yet sent
		m.Task("outbox unpublished retention", func(ctx context.Context) error {
			prune := func(ctx context.Context, before time.Time, limit int) (int64, error) {
				return outboxpg.PruneUnpublished(ctx, pool, before, limit)
			}
			return retention.Run(ctx, "outbox", outboxRows(cfg.Outbox.UnpublishedKeep), prune, retentionMetrics, log.With("rows", "unpublished"))
		}, host.Restartable)
	}
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	itemv1.RegisterItemServiceServer(srv, handler.New(items))
	return nil
}
