// Package retentionprom is retention's Prometheus observer: the rows each task deleted, by module and store.
package retentionprom

import (
	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics"
	"finga/pkg/retention"
)

// Metrics are the rows one module's retention tasks deleted; it is their retention.Observer.
type Metrics struct {
	deleted *prometheus.CounterVec // by module and store: a table, or social's in-memory members
	module  string
}

var _ retention.Observer = (*Metrics)(nil)

// New is module's deleted-rows count, whose family it registers on reg or shares with the modules already there.
func New(reg prometheus.Registerer, module string) (*Metrics, error) {
	deleted, err := metrics.Shared(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metrics.Namespace, Name: "retention_deleted_total", Help: "Rows a retention task deleted once kept their time, by module and store: a table, or social's in-memory members.",
	}, []string{"module", "store"}))
	if err != nil {
		return nil, err
	}
	return &Metrics{deleted: deleted, module: module}, nil
}

// Deleted counts n rows deleted from store; a pass that deleted none still shows the store, at 0.
func (m *Metrics) Deleted(store string, n int64) {
	m.deleted.WithLabelValues(m.module, store).Add(float64(n))
}
