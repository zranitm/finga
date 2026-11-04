package host_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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
	h.Close()
}
