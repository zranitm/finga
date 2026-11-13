package outboxprom_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics/promtest"
	"finga/pkg/outbox/outboxprom"
)

func TestTheOutboxGaugeFollowsItsCount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m, err := outboxprom.New(reg, "item")
		if err != nil {
			t.Fatal(err)
		}
		counts := []int64{12, -1, 30}
		count := func(context.Context) (int64, error) {
			n := counts[0]
			if len(counts) > 1 {
				counts = counts[1:]
			}
			if n < 0 {
				return 0, errors.New("database down")
			}
			return n, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error)
		go func() { done <- m.Track(ctx, time.Minute, count, slog.New(slog.DiscardHandler)) }()
		synctest.Wait()
		if got := promtest.Sample(t, reg, "finga_outbox_unpublished", "module", "item"); got != 12 {
			t.Fatalf("finga_outbox_unpublished = %v, want 12", got)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := promtest.Sample(t, reg, "finga_outbox_unpublished", "module", "item"); got != 12 {
			t.Fatalf("after a failed count %v, want the last good one", got)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := promtest.Sample(t, reg, "finga_outbox_unpublished", "module", "item"); got != 30 {
			t.Fatalf("finga_outbox_unpublished = %v, want 30", got)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
