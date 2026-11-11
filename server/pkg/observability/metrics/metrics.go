// Package metrics sets up Prometheus metrics: the registry, its handler and the process-wide counters.
package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"finga/pkg/panics"
)

// NewRegistry is a fresh registry with the Go runtime and process collectors.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

// Handler serves reg in the Prometheus text format.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
}

// Register registers each of cs on reg, stopping at the first that fails, as a second registration on one registry does.
func Register(reg prometheus.Registerer, cs ...prometheus.Collector) error {
	for _, c := range cs {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// Namespace prefixes the game's own metrics; the libraries' (grpc_, go_, process_) keep their standard names.
const Namespace = "finga"

// PanicCounter is finga_panics_total by flow, which a panics.Recorder counts in.
type PanicCounter struct{ vec *prometheus.CounterVec }

// NewPanicCounter registers finga_panics_total in reg, every flow at 0.
func NewPanicCounter(reg prometheus.Registerer) (*PanicCounter, error) {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Name: "panics_total", Help: "Panics recovered, by flow."}, []string{"flow"})
	if err := reg.Register(vec); err != nil {
		return nil, fmt.Errorf("panics_total: %w", err)
	}
	for _, flow := range panics.Flows() {
		vec.WithLabelValues(flow)
	}
	return &PanicCounter{vec: vec}, nil
}

// Inc counts one panic in flow.
func (c *PanicCounter) Inc(flow string) { c.vec.WithLabelValues(flow).Inc() }
