package host_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"finga/internal/host"
	"finga/pkg/grpcx"
	"finga/pkg/observability/metrics"
	"finga/pkg/observability/tracing"
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

func newHost(t *testing.T, opts host.Options) *host.Host {
	t.Helper()
	h, err := host.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// newPublic is a service that serves no gRPC but players over HTTP, as the gateway does.
func newPublic(t *testing.T, log *runnertest.Log) *host.Host {
	t.Helper()
	h := newHost(t, host.Options{Name: "edge", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: 2 * time.Second}, Logger: log.Logger(), Registry: metrics.NewRegistry()})
	h.Serve("http", "127.0.0.1:0", http.NotFoundHandler())
	return h
}

func TestAServiceServesGRPCAndItsOpsEndpoints(t *testing.T) {
	ran := make(chan struct{})
	closed := false
	log := runnertest.NewLog()
	h := newHost(t, host.Options{
		Name:     "shop",
		Settings: host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: time.Second},
		Logger:   log.Logger(),
		Registry: metrics.NewRegistry(), // with the Go collectors, as every binary builds it
	})
	h.Task("worker", func(ctx context.Context) error { close(ran); <-ctx.Done(); return nil })
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
	t.Run("each registered service has its own status", func(t *testing.T) {
		conn, err := grpcx.Dial(grpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		res, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: "grpc.testing.TestService"})
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
	var dialed *grpc.ClientConn
	t.Run("a dependency dialed through the host is counted from the caller's side", func(t *testing.T) {
		conn, err := h.Dial("shop", grpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		dialed = conn
		if _, err := testpb.NewTestServiceClient(conn).EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatal(err)
		}
		_, body := runnertest.Get(t, "http://"+opsAddr+"/metrics")
		if want := `grpc_client_handled_total{grpc_code="OK",grpc_method="EmptyCall",grpc_service="grpc.testing.TestService",grpc_type="unary"} 1`; !strings.Contains(body, want) {
			t.Fatalf("/metrics lacks %s", want)
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
	if dialed == nil || dialed.GetState() != connectivity.Shutdown {
		t.Fatal("Close left the dialed connection open")
	}
	if res, err := http.Get("http://" + opsAddr + "/readyz"); err == nil {
		res.Body.Close()
		t.Fatal("ops still answers after shutdown")
	}
}

// A subscriber's stream never ends on its own; shutdown ends it rather than wait out the grace.
func TestAnOpenStreamDoesNotHoldTheShutdown(t *testing.T) {
	log := runnertest.NewLog()
	h := newHost(t, host.Options{
		Name:     "shop",
		Settings: host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: 30 * time.Second},
		Logger:   log.Logger(),
		Registry: metrics.NewRegistry(),
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

func TestAServiceWithoutGRPCServesItsHTTPListenerAndOps(t *testing.T) {
	log := runnertest.NewLog()
	h := newPublic(t, log)
	if h.GRPC != nil {
		t.Fatal("a gRPC server for a service that serves none")
	}
	p := runnertest.Start(t, log, h.Run, "http", "ops")
	// Readiness checks no dependency, so a shared outage cannot pull every instance out of rotation at once.
	for _, path := range []string{"/readyz", "/healthz"} {
		if status, body := runnertest.Get(t, "http://"+p.Addr("ops")+path); status != http.StatusOK || (path == "/readyz" && !strings.Contains(body, "ready")) {
			t.Errorf("ops %s: status %d body %s", path, status, body)
		}
	}
	// Players reach only the public listener, so the probes and the metrics are not on it.
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		if status, _ := runnertest.Get(t, "http://"+p.Addr("http")+path); status != http.StatusNotFound {
			t.Errorf("the public listener answered %s with %d", path, status)
		}
	}
	if status, body := runnertest.Get(t, "http://"+p.Addr("ops")+"/metrics"); status != http.StatusOK || !strings.Contains(body, "finga_panics_total") {
		t.Errorf("ops /metrics: status %d, the panic counter missing", status)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	if res, err := http.Get("http://" + p.Addr("ops") + "/healthz"); err == nil {
		res.Body.Close()
		t.Fatal("the listener still answers after shutdown")
	}
}

// A drain step runs after readiness turns 503, so the load balancer stops sending new connections first.
func TestADrainStepRunsAfterReadinessTurnsOff(t *testing.T) {
	log := runnertest.NewLog()
	h := newPublic(t, log)
	var addr string // set before the drain runs
	drained := make(chan int, 1)
	h.OnDrain(func(context.Context) {
		res, err := http.Get("http://" + addr + "/readyz") // the listener still serves while draining
		if err != nil {
			drained <- 0
			return
		}
		res.Body.Close()
		drained <- res.StatusCode
	})
	p := runnertest.Start(t, log, h.Run, "http", "ops")
	addr = p.Addr("ops")
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	if code := <-drained; code != http.StatusServiceUnavailable {
		t.Fatalf("readyz answered %d while draining, want 503", code)
	}
}

// The host's tracer provider samples the service's entry points, and Close flushes and ends it.
func TestTheHostTracesItsEntryPointsUntilClose(t *testing.T) {
	h := newHost(t, host.Options{Name: "shop", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: time.Second}, Logger: runnertest.NewLog().Logger(), Registry: metrics.NewRegistry(),
		Roots: func(name string) bool { return name == "shop.buy" }})
	tracer := h.Traces.Tracer("test")
	_, buy := tracer.Start(context.Background(), "shop.buy")
	_, tick := tracer.Start(context.Background(), "shop.tick")
	if !buy.IsRecording() || tick.IsRecording() || !tick.SpanContext().TraceID().IsValid() {
		t.Fatalf("an entry point recording %v, another root recording %v with IDs %v", buy.IsRecording(), tick.IsRecording(), tick.SpanContext())
	}
	buy.End()
	tick.End()
	h.Close()
	if _, after := h.Traces.Tracer("test").Start(context.Background(), "shop.buy"); after.IsRecording() {
		t.Fatal("still recording after Close")
	}
}

// Compose stops a service shutdown_grace plus 5 s after asking; the last flush must leave room in that margin.
func TestCloseGivesUpOnASilentCollectorWithinTwoSeconds(t *testing.T) {
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	h := newHost(t, host.Options{Name: "shop", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: time.Second, Tracing: tracing.Config{OTLPEndpoint: silent.Addr().String()}},
		Logger: runnertest.NewLog().Logger(), Registry: metrics.NewRegistry(), Roots: func(name string) bool { return name == "shop.buy" }})
	_, buy := h.Traces.Tracer("test").Start(context.Background(), "shop.buy")
	buy.End()
	start := time.Now()
	h.Close()
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Close took %v with a silent collector", took)
	}
}
