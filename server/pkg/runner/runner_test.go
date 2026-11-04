package runner_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	"finga/pkg/grpcx"
	"finga/pkg/panics"
	"finga/pkg/runner"
	"finga/pkg/runner/runnertest"
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

// serve runs r, which logs into log, and returns where its listener named name listens.
func serve(t *testing.T, log *runnertest.Log, r *runner.Runner, name string) (addr string, stop func() error) {
	t.Helper()
	p := runnertest.Start(t, log, r.Run, name)
	return p.Addr(name), p.Stop
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

func TestReadinessFollowsTheProcess(t *testing.T) {
	var r *runner.Runner
	ops := runner.Probes(func() bool { return r.Ready() }, http.NotFoundHandler())
	log := runnertest.NewLog()
	r = newRunner(t, runner.Options{Logger: log.Logger()}, []runner.Listener{{Name: "ops", Addr: "127.0.0.1:0", Server: runner.HTTP(ops)}})

	if r.Ready() {
		t.Fatal("ready before Run")
	}
	addr, stop := serve(t, log, r, "ops")
	res, err := http.Get("http://" + addr + "/readyz")
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
	log := runnertest.NewLog()
	r := newRunner(t, runner.Options{Logger: log.Logger()}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(slow)}})
	addr, stop := serve(t, log, r, "http")

	got := make(chan string, 1)
	go func() {
		res, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		got <- string(body)
	}()
	<-entered
	runDone := make(chan error, 1)
	go func() { runDone <- stop() }()             // shutdown starts while the request is in flight
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
	if res, err := http.Get("http://" + addr + "/"); err == nil {
		res.Body.Close()
		t.Fatal("connected after shutdown")
	}
}

func TestRunFailsOnABusyAddress(t *testing.T) {
	log := runnertest.NewLog()
	first := newRunner(t, runner.Options{Logger: log.Logger()}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}})
	busy, _ := serve(t, log, first, "http")
	ran := atomic.Bool{}
	second := newRunner(t, runner.Options{}, []runner.Listener{
		{Name: "free", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())},
		{Name: "busy", Addr: busy, Server: runner.HTTP(http.NotFoundHandler())},
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
	var addr string // set before the drain runs
	log := runnertest.NewLog()
	r = newRunner(t, runner.Options{Logger: log.Logger(), Drain: func(context.Context) {
		readyDuringDrain.Store(r.Ready())
		res, err := http.Get("http://" + addr + "/")
		if err == nil {
			res.Body.Close()
			listeningDuringDrain.Store(true)
		}
	}}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}})
	addr, stop := serve(t, log, r, "http")
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
	log := runnertest.NewLog()
	r := newRunner(t, runner.Options{Logger: log.Logger()}, []runner.Listener{{Name: "grpc", Addr: "127.0.0.1:0", Server: runner.GRPC(srv)}})
	addr, stop := serve(t, log, r, "grpc")
	conn, err := grpcx.Dial(addr)
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
	log := runnertest.NewLog()
	r := newRunner(t, runner.Options{Logger: log.Logger(), ShutdownGrace: 50 * time.Millisecond}, []runner.Listener{{Name: "grpc", Addr: "127.0.0.1:0", Server: runner.GRPC(srv)}})
	addr, stop := serve(t, log, r, "grpc")
	conn, err := grpcx.Dial(addr, grpc.WithDefaultCallOptions(grpc.WaitForReady(false)))
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

// failing serves until told to fail, then returns err; it counts its shutdowns.
type failing struct {
	fail     chan struct{}
	err      error
	shutdown atomic.Int32
}

func (f *failing) Serve(net.Listener) error { <-f.fail; return f.err }
func (f *failing) Shutdown(context.Context) error {
	f.shutdown.Add(1)
	return nil
}

func TestAListenerThatFailsStopsTheRunner(t *testing.T) {
	broken := &failing{fail: make(chan struct{}), err: errors.New("accept: too many open files")}
	var drained, taskStopped atomic.Bool
	r := newRunner(t, runner.Options{Drain: func(context.Context) { drained.Store(true) }}, []runner.Listener{
		{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())},
		{Name: "broken", Addr: "127.0.0.1:0", Server: broken},
	}, runner.Task{Name: "ticker", Run: func(ctx context.Context) error { <-ctx.Done(); taskStopped.Store(true); return nil }})
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background()) }()
	waitFor(t, r.Ready)
	close(broken.fail)
	select {
	case err := <-done:
		if !errors.Is(err, broken.err) || !strings.Contains(err.Error(), "broken") {
			t.Fatalf("Run = %v, want the broken listener's error, named", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept running after a listener failed")
	}
	if r.Ready() || drained.Load() || !taskStopped.Load() || broken.shutdown.Load() != 1 {
		t.Fatalf("ready %v, drained %v, task stopped %v, shutdowns %d: want not ready, no drain, the task stopped, every listener shut down",
			r.Ready(), drained.Load(), taskStopped.Load(), broken.shutdown.Load())
	}
}

func TestAnHTTPServersOwnErrorsReachTheLogger(t *testing.T) {
	var logs lockedBuffer
	panicky := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("handler without its own recovery") })
	log := runnertest.NewLog()
	r := newRunner(t, runner.Options{Logger: slog.New(slog.NewMultiHandler(log.Handler(), slog.NewJSONHandler(&logs, nil)))},
		[]runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(panicky)}})
	addr, _ := serve(t, log, r, "http")
	if res, err := http.Get("http://" + addr); err == nil {
		res.Body.Close()
	}
	waitFor(t, func() bool { return strings.Contains(logs.String(), "handler without its own recovery") })
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("log %q", logs.String())
	}
}

// lockedBuffer is a log destination the runner's goroutines can share.
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

// tally is a panics.Counter the runner's goroutines can share.
type tally struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *tally) Inc(flow string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[flow]++
}

func (c *tally) get(flow string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[flow]
}

func TestAPanickingTaskStopsTheProcessGracefully(t *testing.T) {
	var logs lockedBuffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	count := &tally{}
	steadyStopped := make(chan struct{})
	var drained atomic.Bool
	r := newRunner(t, runner.Options{Logger: log, Panics: panics.NewRecorder(log, count), Drain: func(context.Context) { drained.Store(true) }},
		[]runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "steady", Run: func(ctx context.Context) error { <-ctx.Done(); close(steadyStopped); return nil }},
		runner.Task{Name: "broken", Run: func(context.Context) error { panic("bad state") }},
	)
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background()) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept serving after a task panicked")
	}
	if e, ok := errors.AsType[*panics.Error](err); !ok || e.Value != "bad state" || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("Run returned %v", err)
	}
	select {
	case <-steadyStopped:
	default:
		t.Fatal("the other task was still running when Run returned")
	}
	if !drained.Load() {
		t.Fatal("the shutdown skipped Drain")
	}
	if count.get(panics.FlowTask) != 1 || !strings.Contains(logs.String(), `"task":"broken"`) || !strings.Contains(logs.String(), `"panic":"bad state"`) {
		t.Fatalf("count %d, log %q", count.get(panics.FlowTask), logs.String())
	}
}
