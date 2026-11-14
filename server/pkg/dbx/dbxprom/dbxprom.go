// Package dbxprom is dbx's Prometheus observer: each transaction's time and retries, by module and the facade call it served.
package dbxprom

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/dbx"
	"finga/pkg/observability/metrics"
)

// Transaction buckets cover a local Postgres (sub-millisecond) to a slow one.
var txBuckets = []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1}

// Metrics are one module's transactions; it is its stores' dbx.Observer.
type Metrics struct {
	duration *prometheus.HistogramVec // by module and operation
	retries  *prometheus.CounterVec   // by module and operation
	module   string
}

var _ dbx.Observer = (*Metrics)(nil)

// New is module's transaction metrics, whose families it registers on reg or shares with the modules already there.
func New(reg prometheus.Registerer, module string) (*Metrics, error) {
	duration, err := metrics.Shared(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metrics.Namespace, Name: "db_tx_duration_seconds", Help: "Database transaction time by module and operation, retries included.", Buckets: txBuckets,
	}, []string{"module", "operation"}))
	if err != nil {
		return nil, err
	}
	retries, err := metrics.Shared(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metrics.Namespace, Name: "db_tx_retries_total", Help: "Transactions re-run after a deadlock, a serialization failure or a lock timeout, by module and operation.",
	}, []string{"module", "operation"}))
	if err != nil {
		return nil, err
	}
	return &Metrics{duration: duration, retries: retries, module: module}, nil
}

// TxDone records a transaction under the facade call it served, as AddMany for item's AddMany, or other outside a call.
func (m *Metrics) TxDone(ctx context.Context, took time.Duration, runs int) {
	op := metrics.Operation(ctx)
	m.duration.WithLabelValues(m.module, op).Observe(took.Seconds())
	if runs > 1 {
		m.retries.WithLabelValues(m.module, op).Add(float64(runs - 1))
	}
}
