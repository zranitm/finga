// Package metrics sets up Prometheus metrics: the registry, its handler and the process-wide counters.
package metrics

import (
	"context"
	"errors"
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

// Shared registers c on reg, or returns the collector of c's family already there, so the modules of one process count in one family.
func Shared[C prometheus.Collector](reg prometheus.Registerer, c C) (C, error) {
	err := reg.Register(c)
	if already, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
		if existing, ok := already.ExistingCollector.(C); ok {
			return existing, nil
		}
	}
	return c, err
}

type operationKey struct{}

// WithOperation is ctx serving operation, which the metrics observed under it are labelled with.
func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey{}, operation)
}

// Operation is the operation ctx serves, or "other" outside one.
func Operation(ctx context.Context) string {
	if op, ok := ctx.Value(operationKey{}).(string); ok {
		return op
	}
	return "other"
}

// Namespace prefixes the game's own metrics; the libraries' (go_, process_) keep their standard names.
const Namespace = "finga"

// PanicCounter is one module's count in finga_panics_total, by flow; a panics.Recorder counts in it.
type PanicCounter struct {
	vec    *prometheus.CounterVec
	module string
}

// NewPanicCounter is module's count in finga_panics_total, which it registers on reg or shares with the modules already there; every flow starts at 0.
func NewPanicCounter(reg prometheus.Registerer, module string) (*PanicCounter, error) {
	vec, err := Shared(reg, prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Name: "panics_total", Help: "Panics recovered, by module and flow."}, []string{"module", "flow"}))
	if err != nil {
		return nil, fmt.Errorf("panics_total: %w", err)
	}
	for _, flow := range panics.Flows() {
		vec.WithLabelValues(module, flow)
	}
	return &PanicCounter{vec: vec, module: module}, nil
}

// Inc counts one panic in flow.
func (c *PanicCounter) Inc(flow string) { c.vec.WithLabelValues(c.module, flow).Inc() }

// TaskRestarts is finga_task_restarts_total by module and task, which a runner counts restartable tasks' restarts in.
type TaskRestarts struct{ vec *prometheus.CounterVec }

// NewTaskRestarts registers finga_task_restarts_total on reg, or shares the one already there.
func NewTaskRestarts(reg prometheus.Registerer) (*TaskRestarts, error) {
	vec, err := Shared(reg, prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Name: "task_restarts_total", Help: "Restarts of a restartable task after a panic, an error or an early end, by module and task."}, []string{"module", "task"}))
	if err != nil {
		return nil, fmt.Errorf("task_restarts_total: %w", err)
	}
	return &TaskRestarts{vec: vec}, nil
}

// Init shows module's task at 0 until its first restart.
func (c *TaskRestarts) Init(module, task string) { c.vec.WithLabelValues(module, task) }

// Inc counts one restart of module's task.
func (c *TaskRestarts) Inc(module, task string) { c.vec.WithLabelValues(module, task).Inc() }
