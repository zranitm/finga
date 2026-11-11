// Package dbxprom is dbx's Prometheus observer: each transaction's time and retries, by the gRPC method it served.
package dbxprom

import (
	"context"
	"path"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"finga/pkg/dbx"
	"finga/pkg/observability"
)

// Transaction buckets cover a local Postgres (sub-millisecond) to a slow one.
var txBuckets = []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1}

// Metrics are a database service's transactions; it is its stores' dbx.Observer.
type Metrics struct {
	duration *prometheus.HistogramVec // by operation
	retries  *prometheus.CounterVec   // by operation
}

var _ dbx.Observer = (*Metrics)(nil)

// New registers the transaction metrics on reg; registering twice on one registry fails.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: observability.Namespace, Name: "db_tx_duration_seconds", Help: "Database transaction time by operation, retries included.", Buckets: txBuckets,
		}, []string{"operation"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "db_tx_retries_total", Help: "Transactions re-run after a deadlock, a serialization failure or a lock timeout.",
		}, []string{"operation"}),
	}
	return m, observability.Register(reg, m.duration, m.retries)
}

// TxDone records a transaction under the gRPC method it served, as Add for /finga.item.v1.ItemService/Add.
func (m *Metrics) TxDone(ctx context.Context, took time.Duration, runs int) {
	op := "other"
	if method, ok := grpc.Method(ctx); ok {
		op = path.Base(method)
	}
	m.duration.WithLabelValues(op).Observe(took.Seconds())
	if runs > 1 {
		m.retries.WithLabelValues(op).Add(float64(runs - 1))
	}
}
