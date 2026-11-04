package host_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestAServiceServesGRPCAndItsOpsEndpoints(t *testing.T) {
	ran := make(chan struct{})
	closed := false
	grpcAddr, opsAddr := runnertest.Addr(t), runnertest.Addr(t)
	h := host.New(host.Options{
		Name:     "shop",
		Settings: host.Settings{GRPC: grpcAddr, Ops: opsAddr, ShutdownGrace: time.Second},
		Logger:   slog.New(slog.DiscardHandler),
		Registry: observability.NewRegistry(), // with the Go collectors, as every binary builds it
		Tasks:    []runner.Task{{Name: "worker", Run: func(ctx context.Context) error { close(ran); <-ctx.Done(); return nil }}},
	})
	testpb.RegisterTestServiceServer(h.GRPC, empty{})
	h.OnClose(func() { closed = true })

	stop := runnertest.Start(t, "http://"+opsAddr+"/readyz", h.Run)
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

	if err := stop(); err != nil {
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
