package runner_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	"finga/pkg/grpcx"
	"finga/pkg/runner"
)

func newRunner(t *testing.T, opts runner.Options, ls []runner.Listener, tasks ...runner.Task) *runner.Runner {
	t.Helper()
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.ShutdownGrace == 0 {
		opts.ShutdownGrace = 2 * time.Second
	}
	return runner.New(opts, ls, tasks)
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func start(t *testing.T, r *runner.Runner) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	waitFor(t, r.Ready)
	return func() error { cancel(); return <-done }
}

func TestProbes(t *testing.T) {
	ready := atomic.Bool{}
	h := runner.Probes(ready.Load, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("metrics")) }))
	tests := []struct {
		name   string
		path   string
		ready  bool
		status int
		body   string
	}{
		{"healthz while the process runs", "/healthz", false, http.StatusOK, `{"status":"ok"}`},
		{"readyz before ready", "/readyz", false, http.StatusServiceUnavailable, `{"status":"not ready"}`},
		{"readyz once ready", "/readyz", true, http.StatusOK, `{"status":"ready"}`},
		{"metrics", "/metrics", true, http.StatusOK, "metrics"},
		{"anything else", "/nope", true, http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready.Store(tt.ready)
			rec := get(t, h, tt.path)
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.body) {
				t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
			}
		})
	}
}

// Readiness answers one question: can this process take traffic now? It is
// ready from the moment it listens until shutdown starts.
func TestReadinessFollowsTheProcess(t *testing.T) {
	var r *runner.Runner
	ops := runner.Probes(func() bool { return r.Ready() }, http.NotFoundHandler())
	r = newRunner(t, runner.Options{}, []runner.Listener{{Name: "ops", Addr: "127.0.0.1:0", Server: runner.HTTP(ops)}})

	if r.Ready() || r.Addr("ops") != "" {
		t.Fatal("ready or bound before Run")
	}
	// An address is published together with readiness: whoever sees one
	// sees the other (the gateway's readiness test waited on the
	// address and once caught readyz still 503).
	ctxEarly, cancelEarly := context.WithCancel(context.Background())
	early := newRunner(t, runner.Options{}, []runner.Listener{{Name: "ops", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}})
	earlyDone := make(chan error, 1)
	go func() { earlyDone <- early.Run(ctxEarly) }()
	waitFor(t, func() bool { return early.Addr("ops") != "" })
	if !early.Ready() {
		t.Fatal("an address was published before readiness")
	}
	cancelEarly()
	if err := <-earlyDone; err != nil {
		t.Fatal(err)
	}
	stop := start(t, r)
	res, err := http.Get("http://" + r.Addr("ops") + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("readyz %d while listening", res.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if r.Ready() {
		t.Fatal("still ready after shutdown")
	}
}

func TestShutdownLetsInFlightRequestsFinish(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("done"))
	})
	r := newRunner(t, runner.Options{}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(slow)}})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- r.Run(ctx) }()
	waitFor(t, r.Ready)

	got := make(chan string, 1)
	go func() {
		res, err := http.Get("http://" + r.Addr("http") + "/slow")
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		got <- string(body)
	}()
	<-entered
	cancel()                                      // shutdown starts while the request is in flight
	waitFor(t, func() bool { return !r.Ready() }) // readiness drops first
	select {
	case err := <-runDone:
		t.Fatalf("Run returned %v before the in-flight request finished", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if body := <-got; body != "done" {
		t.Fatalf("in-flight request: %s", body)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if res, err := http.Get("http://" + r.Addr("http") + "/"); err == nil {
		res.Body.Close()
		t.Fatal("connected after shutdown")
	}
}

func TestRunFailsOnABusyAddress(t *testing.T) {
	first := newRunner(t, runner.Options{}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}})
	stop := start(t, first)
	defer func() { _ = stop() }()
	ran := atomic.Bool{}
	second := newRunner(t, runner.Options{}, []runner.Listener{
		{Name: "free", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())},
		{Name: "busy", Addr: first.Addr("http"), Server: runner.HTTP(http.NotFoundHandler())},
	}, runner.Task{Name: "never", Run: func(context.Context) error { ran.Store(true); return nil }})
	if err := second.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("second Run on a busy address: %v", err)
	}
	if ran.Load() || second.Ready() {
		t.Fatal("tasks ran or the runner became ready although a listener failed")
	}
}

func TestTasksStopWithTheRunner(t *testing.T) {
	stopped := make(chan struct{})
	r := newRunner(t, runner.Options{}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "ticker", Run: func(ctx context.Context) error { <-ctx.Done(); close(stopped); return nil }})
	stop := start(t, r)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("the task was still running when Run returned")
	}
}

func TestDrainRunsAfterReadinessDropsAndBeforeTheListenersClose(t *testing.T) {
	var r *runner.Runner
	var readyDuringDrain, listeningDuringDrain atomic.Bool
	r = newRunner(t, runner.Options{Drain: func(context.Context) {
		readyDuringDrain.Store(r.Ready())
		res, err := http.Get("http://" + r.Addr("http") + "/")
		if err == nil {
			res.Body.Close()
			listeningDuringDrain.Store(true)
		}
	}}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}})
	stop := start(t, r)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if readyDuringDrain.Load() || !listeningDuringDrain.Load() {
		t.Fatalf("during drain: ready %v, listening %v; want not ready and still listening", readyDuringDrain.Load(), listeningDuringDrain.Load())
	}
}

type blocking struct {
	testpb.UnimplementedTestServiceServer
	entered, release chan struct{}
}

// EmptyCall waits for release, or for the server to cancel the call.
func (b *blocking) EmptyCall(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
	close(b.entered)
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &testpb.Empty{}, nil
}

func TestAGRPCServerStopsGracefully(t *testing.T) {
	b := &blocking{entered: make(chan struct{}), release: make(chan struct{})}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	testpb.RegisterTestServiceServer(srv, b)
	r := newRunner(t, runner.Options{}, []runner.Listener{{Name: "grpc", Addr: "127.0.0.1:0", Server: runner.GRPC(srv)}})
	stop := start(t, r)
	conn, err := grpcx.Dial(r.Addr("grpc"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	callDone := make(chan error, 1)
	go func() {
		_, err := testpb.NewTestServiceClient(conn).EmptyCall(context.Background(), &testpb.Empty{})
		callDone <- err
	}()
	<-b.entered
	stopped := make(chan error, 1)
	go func() { stopped <- stop() }()
	select {
	case err := <-stopped:
		t.Fatalf("stopped (%v) before the call in flight finished", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(b.release)
	if err := <-callDone; err != nil {
		t.Fatalf("the call in flight failed: %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestAGRPCServerPastTheGraceIsStopped(t *testing.T) {
	b := &blocking{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(b.release)
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	testpb.RegisterTestServiceServer(srv, b)
	r := newRunner(t, runner.Options{ShutdownGrace: 50 * time.Millisecond}, []runner.Listener{{Name: "grpc", Addr: "127.0.0.1:0", Server: runner.GRPC(srv)}})
	stop := start(t, r)
	conn, err := grpcx.Dial(r.Addr("grpc"), grpc.WithDefaultCallOptions(grpc.WaitForReady(false)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() { _, _ = testpb.NewTestServiceClient(conn).EmptyCall(context.Background(), &testpb.Empty{}) }()
	<-b.entered
	stopped := make(chan error, 1)
	go func() { stopped <- stop() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a call stuck past the grace kept the server running")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
