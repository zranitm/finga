// Package outboxprom is the outbox's Prometheus gauge: the rows not yet published, from a count it takes on a schedule.
package outboxprom

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics"
)

// Metrics are one module's outbox: the rows a relay has yet to publish.
type Metrics struct {
	unpublished prometheus.Gauge
}

// New is module's outbox gauge, whose family it registers on reg or shares with the modules already there.
func New(reg prometheus.Registerer, module string) (*Metrics, error) {
	vec, err := metrics.Shared(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metrics.Namespace, Name: "outbox_unpublished", Help: "Outbox rows not yet published, by module: in Stage 1, with no relay, the unpublished TTL caps it; in Stage 2, the relay's lag.",
	}, []string{"module"}))
	if err != nil {
		return nil, err
	}
	return &Metrics{unpublished: vec.WithLabelValues(module)}, nil
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
