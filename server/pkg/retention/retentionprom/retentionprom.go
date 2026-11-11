// Package retentionprom is retention's Prometheus observer: the rows each task deleted, by store.
package retentionprom

import (
	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics"
	"finga/pkg/retention"
)

// Metrics are the rows each retention task deleted; it is their retention.Observer.
type Metrics struct {
	deleted *prometheus.CounterVec // by store: a table, or social's in-memory members
}

var _ retention.Observer = (*Metrics)(nil)

// New registers the deleted-rows counter on reg; registering twice on one registry fails.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{deleted: prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metrics.Namespace, Name: "retention_deleted_total", Help: "Rows a retention task deleted once kept their time, by store: a table, or social's in-memory members.",
	}, []string{"store"})}
	return m, metrics.Register(reg, m.deleted)
}

// Deleted counts n rows deleted from store; a pass that deleted none still shows the store, at 0.
func (m *Metrics) Deleted(store string, n int64) {
	m.deleted.WithLabelValues(store).Add(float64(n))
}
