// Package retention deletes rows kept past their time, a batch at a time, on a schedule.
package retention

import (
	"context"
	"log/slog"
	"time"
)

// Policy is how long rows are kept, how often the older ones are deleted, how many per statement, and the pause between statements.
type Policy struct {
	Keep  time.Duration
	Every time.Duration
	Batch int           // so no one statement holds its locks long
	Gap   time.Duration // so other writers get the table between a pass's batches
}

// Delete removes at most limit rows from before before, and reports how many it removed.
type Delete func(ctx context.Context, before time.Time, limit int) (int64, error)

// Observer hears how many rows each pass deleted.
type Observer interface {
	Deleted(table string, n int64)
}

// Run deletes table's rows older than p.Keep now and every p.Every until ctx ends; a failed pass is logged and the next tries again.
func Run(ctx context.Context, table string, p Policy, del Delete, obs Observer, log *slog.Logger) error {
	t := time.NewTicker(p.Every)
	defer t.Stop()
	for {
		start := time.Now()
		n, err := pass(ctx, p, del)
		obs.Deleted(table, n)
		switch {
		case err != nil && ctx.Err() == nil:
			log.WarnContext(ctx, "retention pass failed", "table", table, "deleted", n, "err", err)
		case n > 0:
			log.InfoContext(ctx, "expired rows deleted", "table", table, "deleted", n, "duration_ms", time.Since(start).Milliseconds())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// pass deletes a batch at a time until one comes back short.
func pass(ctx context.Context, p Policy, del Delete) (int64, error) {
	before := time.Now().Add(-p.Keep)
	var total int64
	for {
		n, err := del(ctx, before, p.Batch)
		total += n
		if err != nil || n < int64(p.Batch) {
			return total, err
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(p.Gap):
		}
	}
}
