package grpcx_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

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
		// The server span ends once the answer is written, so it can land after the client has the answer.
		kinds := map[trace.SpanKind]bool{}
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			for _, s := range inTrace(rec, parent.SpanContext().TraceID()) {
				if s.Name() == "grpc.testing.TestService/EmptyCall" {
					kinds[s.SpanKind()] = true
				}
			}
			if kinds[trace.SpanKindClient] && kinds[trace.SpanKindServer] {
				break
			}
		}
		if !kinds[trace.SpanKindClient] || !kinds[trace.SpanKindServer] {
			t.Fatalf("spans in the trace %v, want a client and a server one", kinds)
		}
	})
	t.Run("a stream gets none", func(t *testing.T) {
		ctx, parent := entry.Start(ctx, "shop.buy")
		defer parent.End()
		stream, err := c.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); !errors.Is(err, io.EOF) { // the whole call, so any span it made has ended
			t.Fatalf("after the one message: %v, want EOF", err)
		}
		if got := inTrace(rec, parent.SpanContext().TraceID()); len(got) != 0 {
			t.Fatalf("%d spans, the first %s", len(got), got[0].Name())
		}
	})
	t.Run("a root that is not sampled exports nothing but carries its trace ID", func(t *testing.T) {
		ctx, root := tp.Tracer("test").Start(ctx, "shop.tick")
		defer root.End()
		if _, err := c.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if got := inTrace(rec, root.SpanContext().TraceID()); seen.TraceID() != root.SpanContext().TraceID() || seen.IsSampled() || len(got) != 0 {
			t.Fatalf("the server saw %v under %v, and %d spans were recorded", seen, root.SpanContext(), len(got))
		}
	})
}

// inTrace is the ended spans of one trace: each subtest has its own, so a late span of another can't count in it.
func inTrace(rec *tracetest.SpanRecorder, id trace.TraceID) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.SpanContext().TraceID() == id {
			out = append(out, s)
		}
	}
	return out
}
