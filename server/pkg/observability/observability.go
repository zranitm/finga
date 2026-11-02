// Package observability sets up Prometheus metrics: the registry, its handler and the process-wide counters.
package observability

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry is a fresh registry with the Go runtime and process collectors.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

// MetricsHandler serves reg in the Prometheus text format.
func MetricsHandler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
}

// PanicCounter is panics_total by flow, which a panics.Recorder counts in.
type PanicCounter struct{ vec *prometheus.CounterVec }

// NewPanicCounter registers panics_total in reg.
func NewPanicCounter(reg prometheus.Registerer) (*PanicCounter, error) {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "panics_total", Help: "Panics recovered, by flow."}, []string{"flow"})
	if err := reg.Register(vec); err != nil {
		return nil, fmt.Errorf("panics_total: %w", err)
	}
	return &PanicCounter{vec: vec}, nil
}

// Inc counts one panic in flow.
func (c *PanicCounter) Inc(flow string) { c.vec.WithLabelValues(flow).Inc() }
