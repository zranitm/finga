// Package facade is what every module's facade shares: each call's span, metrics and panic recovery, and the table from the module's errors to the kernel's.
package facade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/kernel"
	"finga/pkg/observability/metrics"
	"finga/pkg/panics"
	"finga/pkg/transient"
)

// Row maps one of a module's model errors to the kernel error its callers see.
type Row struct {
	Model  error
	Kernel error
}

// Errors is a module's error table.
type Errors []Row

// kernel wraps err in its row's kernel error, keeping err as the cause; an error without a row passes as it is.
func (t Errors) kernel(err error) error {
	for _, r := range t {
		if errors.Is(err, r.Model) {
			return fmt.Errorf("%w: %w", r.Kernel, err)
		}
	}
	return err
}

// The codes a call ends with, besides a rule's reason.
const (
	codeOK          = "ok"
	codeUnavailable = "unavailable"
	codeCanceled    = "canceled"
	codeInternal    = "internal"
)

// codeOf is the code err ends a call with: ok, the reason of the rule it breaks, unavailable, canceled when the caller gave up, or internal.
func codeOf(err error) string {
	if err == nil {
		return codeOK
	}
	if reason, ok := kernel.ReasonOf(err); ok {
		return reason
	}
	switch {
	case errors.Is(err, transient.ErrUnavailable):
		return codeUnavailable
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return codeCanceled
	}
	return codeInternal
}

// Options describe one module's calls.
type Options struct {
	Module   string
	Traces   trace.TracerProvider
	Registry prometheus.Registerer
	Panics   *panics.Recorder // the module's, so a panic in a call counts under it
	Errors   Errors
}

// Calls traces, counts and times a module's calls, and recovers their panics.
type Calls struct {
	opts     Options
	tracer   trace.Tracer
	total    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// callBuckets are gRPC's, from a sub-millisecond world command to a slow database call, in seconds, so the panels compare.
var callBuckets = []float64{.0002, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}

// New is opts.Module's calls; its metric families are shared with every other module's on the registry.
func New(opts Options) (*Calls, error) {
	total, err := metrics.Shared(opts.Registry, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metrics.Namespace, Name: "module_calls_total", Help: "Calls to a module's facade, by module, method and code: ok, the rule's reason, unavailable, canceled or internal.",
	}, []string{"module", "method", "code"}))
	if err != nil {
		return nil, err
	}
	duration, err := metrics.Shared(opts.Registry, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metrics.Namespace, Name: "module_call_duration_seconds", Help: "Time a module's facade takes to answer a call, by module and method.", Buckets: callBuckets,
	}, []string{"module", "method"}))
	if err != nil {
		return nil, err
	}
	return &Calls{opts: opts, tracer: opts.Traces.Tracer("finga/internal/facade"), total: total, duration: duration}, nil
}

// Call is one call under way.
type Call struct {
	calls  *Calls
	ctx    context.Context
	method string
	span   trace.Span
	start  time.Time
}

// Start begins a call to method under a span named module.Method; the method defers the call's End.
func (c *Calls) Start(ctx context.Context, method string) (context.Context, *Call) {
	ctx, span := c.tracer.Start(ctx, c.opts.Module+"."+method, trace.WithAttributes(attribute.String("module", c.opts.Module)))
	return ctx, &Call{calls: c, ctx: ctx, method: method, span: span, start: time.Now()}
}

// End ends the call with the method's error, deferred so it recovers a panic: a panic becomes kernel.ErrInternal, a model error its kernel error.
func (c *Call) End(err *error) {
	if p := recover(); p != nil {
		e := panics.Of(p)
		c.calls.opts.Panics.Record(c.ctx, panics.FlowCall, e, "method", c.method)
		*err = fmt.Errorf("%w: %w", kernel.ErrInternal, e)
	}
	*err = c.calls.opts.Errors.kernel(*err)
	code := codeOf(*err)
	c.calls.total.WithLabelValues(c.calls.opts.Module, c.method, code).Inc()
	c.calls.duration.WithLabelValues(c.calls.opts.Module, c.method).Observe(time.Since(c.start).Seconds())
	c.span.SetAttributes(attribute.String("code", code))
	if code == codeInternal || code == codeUnavailable {
		c.span.SetStatus(otelcodes.Error, code) // a rule or a caller giving up is the call working as it should
	}
	c.span.End()
}
