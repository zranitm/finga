package tracing_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/propagation"

	"finga/pkg/tracing"
)

func TestOnlyTheEntryPointsStartASampledTrace(t *testing.T) {
	ctx := context.Background()
	tp, err := tracing.New(ctx, tracing.Config{}, tracing.Options{Service: "shop", Root: func(name string) bool { return name == "shop.buy" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tp.Shutdown(ctx) })
	tracer := tp.Tracer("test")

	buyCtx, buy := tracer.Start(ctx, "shop.buy")
	defer buy.End()
	_, other := tracer.Start(ctx, "shop.tick")
	defer other.End()
	_, child := tracer.Start(buyCtx, "item.add")
	defer child.End()
	tests := []struct {
		name    string
		sampled bool
		got     bool
	}{
		{"an entry point is sampled", true, buy.SpanContext().IsSampled()},
		{"any other root is not", false, other.SpanContext().IsSampled()},
		{"a child follows its parent", true, child.SpanContext().IsSampled() && child.SpanContext().TraceID() == buy.SpanContext().TraceID()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.sampled {
				t.Fatalf("sampled %v, want %v", tt.got, tt.sampled)
			}
		})
	}
	t.Run("a root that is not sampled still has its IDs, for the logs", func(t *testing.T) {
		if !other.SpanContext().TraceID().IsValid() || !other.SpanContext().SpanID().IsValid() {
			t.Fatalf("span context %+v", other.SpanContext())
		}
	})
	t.Run("the context crosses a process boundary", func(t *testing.T) {
		carrier := propagation.MapCarrier{}
		tracing.Propagator().Inject(buyCtx, carrier)
		_, remote := tracer.Start(tracing.Propagator().Extract(ctx, carrier), "item.add")
		defer remote.End()
		if remote.SpanContext().TraceID() != buy.SpanContext().TraceID() || !remote.SpanContext().IsSampled() {
			t.Fatalf("carried %v into %+v", carrier, remote.SpanContext())
		}
	})
}

// A configured collector gets the sampled spans, flushed at Shutdown; an unreachable one fails no call.
func TestAnEndpointExportsWithoutBlocking(t *testing.T) {
	ctx := context.Background()
	tp, err := tracing.New(ctx, tracing.Config{OTLPEndpoint: "127.0.0.1:1"}, tracing.Options{Service: "shop", Root: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	_, span := tp.Tracer("test").Start(ctx, "shop.buy")
	span.End()
	if !span.SpanContext().IsSampled() {
		t.Fatal("the entry point was not sampled")
	}
	shutdown, cancel := context.WithTimeout(ctx, 0)
	defer cancel()
	_ = tp.Shutdown(shutdown) // the collector is not there; Shutdown gives up when its context ends
}
