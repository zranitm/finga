package host_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"finga/internal/host"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
	"finga/pkg/runner"
	"finga/pkg/runner/runnertest"
)

type empty struct {
	testpb.UnimplementedTestServiceServer
}

func (empty) EmptyCall(context.Context, *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}

// StreamingOutputCall is a subscription: one message, then nothing until the stream ends.
func (empty) StreamingOutputCall(_ *testpb.StreamingOutputCallRequest, stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
	if err := stream.Send(&testpb.StreamingOutputCallResponse{}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func TestAServiceServesGRPCAndItsOpsEndpoints(t *testing.T) {
	ran := make(chan struct{})
	closed := false
	log := runnertest.NewLog()
	h := host.New(host.Options{
		Name:     "shop",
		Settings: host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: time.Second},
		Logger:   log.Logger(),
		Registry: observability.NewRegistry(), // with the Go collectors, as every binary builds it
		Tasks:    []runner.Task{{Name: "worker", Run: func(ctx context.Context) error { close(ran); <-ctx.Done(); return nil }}},
	})
	testpb.RegisterTestServiceServer(h.GRPC, empty{})
	h.OnClose(func() { closed = true })

	p := runnertest.Start(t, log, h.Run, "grpc", "ops")
	grpcAddr, opsAddr := p.Addr("grpc"), p.Addr("ops")
	<-ran
	ctx := context.Background()

	t.Run("the gRPC listener serves the registered service", func(t *testing.T) {
		conn, err := grpcx.Dial(grpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := testpb.NewTestServiceClient(conn).EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatal(err)
		}
		if _, err := testpb.NewTestServiceClient(conn).UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.Unimplemented {
			t.Fatalf("an unimplemented call: %v", err)
		}
	})
	t.Run("the gRPC health service answers SERVING", func(t *testing.T) {
		conn, err := grpcx.Dial(grpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		res, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
		if err != nil || res.GetStatus() != healthpb.HealthCheckResponse_SERVING {
			t.Fatalf("health %v %v", res, err)
		}
	})
	t.Run("every call is counted and timed by method and code, and every method starts at 0", func(t *testing.T) {
		_, body := runnertest.Get(t, "http://"+opsAddr+"/metrics")
		for _, want := range []string{
			`grpc_server_handled_total{grpc_code="OK",grpc_method="EmptyCall",grpc_service="grpc.testing.TestService",grpc_type="unary"} 1`,
			`grpc_server_handled_total{grpc_code="Unimplemented",grpc_method="UnaryCall",grpc_service="grpc.testing.TestService",grpc_type="unary"} 1`,
			`grpc_server_handling_seconds_count{grpc_method="EmptyCall",grpc_service="grpc.testing.TestService",grpc_type="unary"} 1`,
			`grpc_server_started_total{grpc_method="FullDuplexCall",grpc_service="grpc.testing.TestService",grpc_type="bidi_stream"} 0`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("/metrics lacks %s", want)
			}
		}
	})
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		t.Run("ops "+path, func(t *testing.T) {
			status, body := runnertest.Get(t, "http://"+opsAddr+path)
			if status != http.StatusOK || (path == "/metrics" && !strings.Contains(body, "go_goroutines")) {
				t.Fatalf("%s: %d %s", path, status, body)
			}
		})
	}

	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	h.Close()
	if !closed {
		t.Fatal("Close released nothing")
	}
	if res, err := http.Get("http://" + opsAddr + "/readyz"); err == nil {
		res.Body.Close()
		t.Fatal("ops still answers after shutdown")
	}
}

// A subscriber's stream never ends on its own; shutdown ends it rather than wait out the grace.
func TestAnOpenStreamDoesNotHoldTheShutdown(t *testing.T) {
	log := runnertest.NewLog()
	h := host.New(host.Options{
		Name:     "shop",
		Settings: host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: 30 * time.Second},
		Logger:   log.Logger(),
		Registry: observability.NewRegistry(),
	})
	testpb.RegisterTestServiceServer(h.GRPC, empty{})
	p := runnertest.Start(t, log, h.Run, "grpc", "ops")
	conn, err := grpcx.Dial(p.Addr("grpc"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := testpb.NewTestServiceClient(conn).StreamingOutputCall(context.Background(), &testpb.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	watch, err := healthpb.NewHealthClient(conn).Watch(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := watch.Recv(); err != nil || res.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health %v %v", res, err)
	}
	start := time.Now()
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("shutdown took %s with a stream open", took)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.Unavailable {
		t.Fatalf("the subscriber got %v", err)
	}
	if res, err := watch.Recv(); err != nil || res.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("health at shutdown %v %v", res, err)
	}
	h.Close()
}
