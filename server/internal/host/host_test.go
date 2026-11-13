package host_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
	"finga/pkg/observability/metrics/promtest"
	"finga/pkg/observability/tracing"
	"finga/pkg/panics"
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

// module is the host's part named name.
func module(t *testing.T, h *host.Host, name string) *host.Module {
	t.Helper()
	m, err := h.Module(name)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// serveGRPC has the module named name serve the test service on h's gRPC address.
func serveGRPC(t *testing.T, h *host.Host, name string) *host.Module {
	t.Helper()
	m := module(t, h, name)
	srv, err := m.ServeGRPC(nil)
	if err != nil {
		t.Fatal(err)
	}
	testpb.RegisterTestServiceServer(srv, empty{})
	return m
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
	serveGRPC(t, h, "shop").Task("worker", func(ctx context.Context) error { close(ran); <-ctx.Done(); return nil }, host.Vital)
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
	serveGRPC(t, h, "shop")
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
	module(t, h, "edge")
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

// The host's tracer provider samples every module's entry points, and Close flushes and ends it.
func TestTheHostTracesItsEntryPointsUntilClose(t *testing.T) {
	h := newHost(t, host.Options{Name: "mall", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: time.Second}, Logger: runnertest.NewLog().Logger(), Registry: metrics.NewRegistry()})
	module(t, h, "shop").TraceRoots(func(name string) bool { return name == "shop.buy" })
	module(t, h, "edge").TraceRoots(func(name string) bool { return name == "edge.enter" })
	_, enter := h.Traces.Tracer("test").Start(context.Background(), "edge.enter")
	if !enter.IsRecording() {
		t.Fatal("a second module's entry point is not sampled")
	}
	enter.End()
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
		Logger: runnertest.NewLog().Logger(), Registry: metrics.NewRegistry()})
	module(t, h, "shop").TraceRoots(func(name string) bool { return name == "shop.buy" })
	_, buy := h.Traces.Tracer("test").Start(context.Background(), "shop.buy")
	buy.End()
	start := time.Now()
	h.Close()
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Close took %v with a silent collector", took)
	}
}

// Modules share the process: one registry, each one's panics and restarts counted under its name.
func TestModulesShareTheProcessAndCountUnderTheirNames(t *testing.T) {
	var logs lockedBuffer
	reg := metrics.NewRegistry()
	h := newHost(t, host.Options{Name: "mall", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: time.Second}, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Registry: reg})
	shop, edge := module(t, h, "shop"), module(t, h, "edge")
	restarted := make(chan struct{})
	var runs atomic.Int32
	shop.Task("restock", func(ctx context.Context) error {
		if runs.Add(1) == 1 {
			panic("empty shelf")
		}
		close(restarted)
		<-ctx.Done()
		return nil
	}, host.Restartable)
	edge.Task("door", func(ctx context.Context) error { <-ctx.Done(); return nil }, host.Restartable)
	edge.Panics.Do(context.Background(), panics.FlowHTTP, func() { panic("bad request") })
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	<-restarted
	for _, tt := range []struct {
		metric string
		labels []string
		want   float64
	}{
		{"finga_panics_total", []string{"module", "shop", "flow", "task"}, 1},
		{"finga_panics_total", []string{"module", "edge", "flow", "http"}, 1},
		{"finga_panics_total", []string{"module", "edge", "flow", "task"}, 0},
		{"finga_task_restarts_total", []string{"module", "shop", "task", "restock"}, 1},
		{"finga_task_restarts_total", []string{"module", "edge", "task", "door"}, 0},
	} {
		if got := promtest.Sample(t, reg, tt.metric, tt.labels...); got != tt.want {
			t.Errorf("%s%v = %v, want %v", tt.metric, tt.labels, got, tt.want)
		}
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"module":"shop","flow":"task","panic":"empty shelf"`) {
		t.Fatalf("the restock panic was not logged under its module: %s", logs.String())
	}
}

// lockedBuffer is a log the process's goroutines can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// One module per process serves gRPC, on the process's gRPC address.
func TestOneModuleServesGRPC(t *testing.T) {
	h := newHost(t, host.Options{Name: "shop", Settings: host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: time.Second}, Logger: runnertest.NewLog().Logger(), Registry: metrics.NewRegistry()})
	serveGRPC(t, h, "shop")
	if _, err := module(t, h, "edge").ServeGRPC(nil); err == nil {
		t.Fatal("a second module served gRPC in the same process")
	}
	public := newPublic(t, runnertest.NewLog())
	if _, err := module(t, public, "edge").ServeGRPC(nil); err == nil {
		t.Fatal("a module served gRPC in a process with no gRPC address")
	}
}

// The game data loads once, and every module that asks after shares that copy.
func TestGameDataLoadsOnce(t *testing.T) {
	h := newHost(t, host.Options{Name: "mall", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: time.Second, DataDir: "../../data"}, Logger: runnertest.NewLog().Logger(), Registry: metrics.NewRegistry()})
	first, err := h.GameData()
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.GameData()
	if err != nil || second != first {
		t.Fatalf("the second call got %p, %v; want the first copy %p", second, err, first)
	}
	missing := newHost(t, host.Options{Name: "mall", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: time.Second, DataDir: t.TempDir()}, Logger: runnertest.NewLog().Logger(), Registry: metrics.NewRegistry()})
	if _, err := missing.GameData(); err == nil {
		t.Fatal("game data loaded from an empty directory")
	}
}
