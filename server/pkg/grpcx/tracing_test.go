package grpcx_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
)

func TestCallsCarryTheTrace(t *testing.T) {
	ctx := context.Background()
	rec := tracetest.NewSpanRecorder()
	// Roots this provider starts are never sampled, so only a sampled caller's spans are recorded.
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.NeverSample())), sdktrace.WithSpanProcessor(rec))
	tracing := grpcx.Tracing{Provider: tp}
	var mu sync.Mutex
	var seen trace.SpanContext
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: table, Logger: slog.New(slog.DiscardHandler), Tracing: &tracing})
	testpb.RegisterTestServiceServer(srv, &shop{
		err: func(ctx context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			seen = trace.SpanContextFromContext(ctx)
			return nil
		},
		stream: func(stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
	})
	c := testpb.NewTestServiceClient(grpctest.Conn(t, srv, grpcx.TraceCalls(tracing)))
	entry := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample())).Tracer("test")

	t.Run("a call under a sampled parent gets a client and a server span in its trace", func(t *testing.T) {
		ctx, parent := entry.Start(ctx, "shop.buy")
		defer parent.End()
		if _, err := c.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatal(err)
		}
		kinds := map[trace.SpanKind]bool{}
		for _, s := range rec.Ended() {
			if s.Name() == "grpc.testing.TestService/EmptyCall" && s.SpanContext().TraceID() == parent.SpanContext().TraceID() {
				kinds[s.SpanKind()] = true
			}
		}
		if !kinds[trace.SpanKindClient] || !kinds[trace.SpanKindServer] {
			t.Fatalf("spans in the trace %v, want a client and a server one", kinds)
		}
	})
	t.Run("a stream gets none", func(t *testing.T) {
		before := len(rec.Ended())
		ctx, parent := entry.Start(ctx, "shop.buy")
		defer parent.End()
		stream, err := c.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatal(err)
		}
		if got := rec.Ended()[before:]; len(got) != 0 {
			t.Fatalf("%d spans, the first %s", len(got), got[0].Name())
		}
	})
	t.Run("a root that is not sampled exports nothing but carries its trace ID", func(t *testing.T) {
		before := len(rec.Ended())
		ctx, root := tp.Tracer("test").Start(ctx, "shop.tick")
		defer root.End()
		if _, err := c.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if seen.TraceID() != root.SpanContext().TraceID() || seen.IsSampled() || len(rec.Ended()) != before {
			t.Fatalf("the server saw %v under %v, and %d spans were recorded", seen, root.SpanContext(), len(rec.Ended())-before)
		}
	})
}
