// Package tracing sets up OpenTelemetry traces: IDs everywhere, sampled only at entry points, sent over OTLP if set.
package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"finga/pkg/uuidv7"
)

// Config is a binary's tracing section.
type Config struct {
	// OTLPEndpoint is the host:port of an OTLP gRPC collector; empty exports nothing.
	OTLPEndpoint string `mapstructure:"otlp_endpoint" validate:"omitempty,host_port"`
}

// Options name the process and its entry points.
type Options struct {
	Service string
	Version string // the build's; empty leaves it out
	// Root reports whether a root span of this name starts a sampled trace; nil samples none.
	Root func(name string) bool
	// Logger takes OpenTelemetry's own errors, such as failed exports, one per errorGap; the handler is process-wide, and nil keeps the default.
	Logger *slog.Logger
}

// errorGap spaces OpenTelemetry's error lines: with the collector down, every batch fails.
const errorGap = time.Minute

// New is the process's tracer provider; Shutdown flushes what it has not exported yet.
func New(ctx context.Context, cfg Config, opts Options) (*sdktrace.TracerProvider, error) {
	root := opts.Root
	if root == nil {
		root = func(string) bool { return false }
	}
	if opts.Logger != nil {
		sometimes := &rate.Sometimes{First: 1, Interval: errorGap}
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			sometimes.Do(func() { opts.Logger.WarnContext(context.Background(), "opentelemetry error", "err", err) })
		}))
	}
	res, err := processResource(opts)
	if err != nil {
		return nil, err
	}
	all := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.ParentBased(entryPoints(root))),
		sdktrace.WithResource(res),
	}
	if cfg.OTLPEndpoint != "" {
		exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint), otlptracegrpc.WithInsecure())
		if err != nil {
			return nil, fmt.Errorf("tracing: exporter: %w", err)
		}
		all = append(all, sdktrace.WithBatcher(exporter))
	}
	return sdktrace.NewTracerProvider(all...), nil
}

// processResource is the SDK's default resource, OTEL_RESOURCE_ATTRIBUTES included, under the service, its version and this process.
func processResource(opts Options) (*resource.Resource, error) {
	own := []attribute.KeyValue{semconv.ServiceName(opts.Service), semconv.ServiceInstanceID(uuidv7.NewString())}
	if opts.Version != "" {
		own = append(own, semconv.ServiceVersion(opts.Version))
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(own...))
	if err != nil {
		return nil, fmt.Errorf("tracing: resource: %w", err)
	}
	return res, nil
}

// Propagator carries the trace context and baggage between processes.
func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// entryPoints samples a root span its predicate names; the IDs of any other are minted all the same.
type entryPoints func(name string) bool

// ShouldSample samples the span when it is an entry point; ParentBased leaves the children to their parent.
func (e entryPoints) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	decision := sdktrace.Drop
	if e(p.Name) {
		decision = sdktrace.RecordAndSample
	}
	return sdktrace.SamplingResult{Decision: decision, Tracestate: trace.SpanContextFromContext(p.ParentContext).TraceState()}
}

// Description names the sampler.
func (entryPoints) Description() string { return "EntryPoints" }
