package grpcx

import (
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"finga/pkg/tracing"
)

// Tracing gives calls trace context and spans, as Provider samples; not streams, whose spans would last hours.
type Tracing struct {
	Provider trace.TracerProvider
}

// TraceCalls is the Dial option that traces a connection's calls.
func TraceCalls(t Tracing) grpc.DialOption {
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler(t.options()...))
}

func (t Tracing) serverOption() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler(t.options()...))
}

// options keep the metrics on Prometheus: OpenTelemetry carries traces only.
func (t Tracing) options() []otelgrpc.Option {
	return []otelgrpc.Option{
		otelgrpc.WithTracerProvider(t.Provider),
		otelgrpc.WithPropagators(tracing.Propagator()),
		otelgrpc.WithMeterProvider(noop.NewMeterProvider()),
		otelgrpc.WithFilter(unary),
	}
}

// unary looks the method up in the registered descriptors; one not found is traced.
func unary(info *stats.RPCTagInfo) bool {
	name := protoreflect.FullName(strings.ReplaceAll(strings.TrimPrefix(info.FullMethodName, "/"), "/", "."))
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	m, ok := d.(protoreflect.MethodDescriptor)
	return err != nil || !ok || !m.IsStreamingClient() && !m.IsStreamingServer()
}
