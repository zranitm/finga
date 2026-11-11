// Package outboxprom is the outbox's Prometheus gauge: the rows not yet published, from a count it takes on a schedule.
package outboxprom

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability"
)

// Metrics are the outbox's: the rows a relay has yet to publish.
type Metrics struct {
	unpublished prometheus.Gauge
}

// New registers the outbox gauge on reg; registering twice on one registry fails.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{unpublished: prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: observability.Namespace, Name: "outbox_unpublished", Help: "Outbox rows not yet published: in Stage 1, with no relay, the unpublished TTL caps it; in Stage 2, the relay's lag.",
	})}
	return m, observability.Register(reg, m.unpublished)
}

// Track sets the gauge from count now and every interval until ctx ends; a failed count leaves the gauge as it was.
func (m *Metrics) Track(ctx context.Context, every time.Duration, count func(context.Context) (int64, error), log *slog.Logger) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		n, err := count(ctx)
		switch {
		case err == nil:
			m.unpublished.Set(float64(n))
		case ctx.Err() == nil:
			log.WarnContext(ctx, "outbox count failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
