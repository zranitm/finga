package host_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"finga/internal/config"
	"finga/internal/host"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
	"finga/pkg/runner"
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
	h := host.New(host.Options{
		Name:          "shop",
		Endpoint:      config.Endpoint{Listen: "127.0.0.1:0", Addr: "unused", Ops: "127.0.0.1:0"},
		ShutdownGrace: time.Second,
		Logger:        slog.New(slog.DiscardHandler),
		Registry:      observability.NewRegistry(), // with the Go collectors, as every binary builds it
		Tasks:         []runner.Task{{Name: "worker", Run: func(ctx context.Context) error { close(ran); <-ctx.Done(); return nil }}},
	})
	testpb.RegisterTestServiceServer(h.GRPC, empty{})
	h.OnClose(func() { closed = true })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for !h.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("never ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-ran

	t.Run("the gRPC listener serves the registered service", func(t *testing.T) {
		conn, err := grpcx.Dial(h.Addr(host.GRPC))
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
			res, err := http.Get("http://" + h.Addr(host.Ops) + path)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusOK || (path == "/metrics" && !strings.Contains(string(body), "go_goroutines")) {
				t.Fatalf("%s: %d %s", path, res.StatusCode, body)
			}
		})
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.Close()
	if !closed || h.Ready() {
		t.Fatalf("closed %v, ready %v after shutdown", closed, h.Ready())
	}
}
