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

// A drain that uses all it may still leaves the listeners part of the grace for the requests in flight.
func TestASlowDrainLeavesTheListenersPartOfTheGrace(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("done"))
	})
	drained := make(chan struct{})
	log := runnertest.NewLog()
	r := newRunner(t, runner.Options{Logger: log.Logger(), Drain: func(ctx context.Context) {
		<-ctx.Done() // a peer that never closes
		close(drained)
	}}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(slow)}})
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
	stopped := make(chan error, 1)
	go func() { stopped <- stop() }()
	<-drained
	time.Sleep(100 * time.Millisecond) // the request outlives the drain
	close(release)
	if body := <-got; body != "done" {
		t.Fatalf("in-flight request: %s", body)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("Run returned %v", err)
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
	}, runner.Task{Name: "never", Kind: runner.Vital, Run: func(context.Context) error { ran.Store(true); return nil }})
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
		runner.Task{Name: "ticker", Kind: runner.Vital, Run: func(ctx context.Context) error { <-ctx.Done(); close(stopped); return nil }})
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

// A request stuck past the grace is cut off, so it can't keep the process running; Run names the listener it cut.
func TestARequestPastTheGraceIsCutOff(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	stuck := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	})
	log := runnertest.NewLog()
	r := newRunner(t, runner.Options{Logger: log.Logger(), ShutdownGrace: 50 * time.Millisecond}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(stuck)}})
	addr, stop := serve(t, log, r, "http")
	go func() {
		if res, err := http.Get("http://" + addr + "/stuck"); err == nil {
			res.Body.Close()
		}
	}()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- stop() }()
	select {
	case err := <-stopped:
		if !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "http: ") {
			t.Fatalf("Run returned %v; want the http listener's deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a request stuck past the grace kept the server running")
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
	}, runner.Task{Name: "ticker", Kind: runner.Vital, Run: func(ctx context.Context) error { <-ctx.Done(); taskStopped.Store(true); return nil }})
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
		runner.Task{Name: "steady", Kind: runner.Vital, Run: func(ctx context.Context) error { <-ctx.Done(); close(steadyStopped); return nil }},
		runner.Task{Name: "broken", Kind: runner.Vital, Run: func(context.Context) error { panic("bad state") }},
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

// A task that ends before shutdown, with or without an error, leaves a dead loop behind a ready process.
func TestATaskThatEndsEarlyStopsTheProcess(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{{"nil", nil}, {"an error", errors.New("stream gone")}} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRunner(t, runner.Options{}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
				runner.Task{Name: "loop", Kind: runner.Vital, Run: func(context.Context) error { return tt.err }})
			done := make(chan error, 1)
			go func() { done <- r.Run(context.Background()) }()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "loop") || (tt.err != nil && !errors.Is(err, tt.err)) {
					t.Fatalf("Run returned %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Run kept serving after its task ended")
			}
		})
	}
}

// A vital task that ends because another failed, as world's saves end when a map panics, doesn't hide that failure, whichever reports first.
func TestAFailureOutranksATaskThatEndedOnIt(t *testing.T) {
	stopping, draining := make(chan struct{}), make(chan struct{})
	r := newRunner(t, runner.Options{Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil), Drain: func(context.Context) { close(draining) }},
		[]runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "saves", Kind: runner.Vital, Run: func(context.Context) error { <-stopping; return nil }},
		runner.Task{Name: "world", Kind: runner.Vital, Run: func(context.Context) error {
			close(stopping)
			<-draining // the saves' end is reported: the process is shutting down
			panic("map broke")
		}},
	)
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background()) }()
	select {
	case err := <-done:
		if e, ok := errors.AsType[*panics.Error](err); !ok || e.Value != "map broke" || !strings.Contains(err.Error(), "task world") {
			t.Fatalf("Run returned %v; want the map's panic", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept serving after its tasks ended")
	}
}

// A panic while the process shuts down is still its exit error.
func TestAPanicDuringShutdownIsReturned(t *testing.T) {
	r := newRunner(t, runner.Options{Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil)},
		[]runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "flusher", Kind: runner.Vital, Run: func(ctx context.Context) error { <-ctx.Done(); panic("flush failed") }})
	stop := start(t, r)
	if err := stop(); err == nil || !strings.Contains(err.Error(), "flush failed") {
		t.Fatalf("Run returned %v", err)
	}
}

// restarts is a runner.Restarts the task goroutines can share.
type restarts struct {
	mu sync.Mutex
	n  map[[2]string]int
}

func (c *restarts) Inc(module, task string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[[2]string]int{}
	}
	c.n[[2]string{module, task}]++
}

func (c *restarts) get(module, task string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[[2]string{module, task}]
}

// A restartable task that fails before shutdown, by a panic, an error or an early end, runs again; the process stays ready.
func TestARestartableTaskRestartsAfterAFailure(t *testing.T) {
	for _, tt := range []struct {
		name string
		fail func() error
	}{
		{"panic", func() error { panic("bad state") }},
		{"error", func() error { return errors.New("stream gone") }},
		{"early end", func() error { return nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs lockedBuffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			own, shared := &tally{}, &tally{}
			counted := &restarts{}
			var runs atomic.Int32
			again := make(chan struct{})
			r := newRunner(t, runner.Options{Logger: log, Panics: panics.NewRecorder(log, shared), Restarts: counted},
				[]runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
				runner.Task{Name: "pump", Module: "gateway", Kind: runner.Restartable, Panics: panics.NewRecorder(log, own), Run: func(ctx context.Context) error {
					if runs.Add(1) == 1 {
						return tt.fail()
					}
					close(again)
					<-ctx.Done()
					return nil
				}})
			stop := start(t, r)
			select {
			case <-again:
			case <-time.After(5 * time.Second):
				t.Fatal("the task never ran again")
			}
			if !r.Ready() || counted.get("gateway", "pump") != 1 || !strings.Contains(logs.String(), `"module":"gateway","task":"pump"`) {
				t.Fatalf("ready %v, restarts %d, log %q", r.Ready(), counted.get("gateway", "pump"), logs.String())
			}
			if want := map[bool]int{true: 1}[tt.name == "panic"]; own.get(panics.FlowTask) != want || shared.get(panics.FlowTask) != 0 {
				t.Fatalf("the task's own recorder counted %d panics, the runner's %d; want %d and 0", own.get(panics.FlowTask), shared.get(panics.FlowTask), want)
			}
			if err := stop(); err != nil {
				t.Fatalf("Run returned %v", err)
			}
			if runs.Load() != 2 || counted.get("gateway", "pump") != 1 {
				t.Fatalf("%d runs, %d restarts after shutdown", runs.Load(), counted.get("gateway", "pump"))
			}
		})
	}
}

// Each failure in a row waits twice as long before the restart as the one before, from 100 ms.
func TestARestartableTaskWaitsLongerAfterEachFailureInARow(t *testing.T) {
	var mu sync.Mutex
	var began []time.Time
	r := newRunner(t, runner.Options{}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "loop", Kind: runner.Restartable, Run: func(ctx context.Context) error {
			mu.Lock()
			began = append(began, time.Now())
			n := len(began)
			mu.Unlock()
			if n < 3 {
				return errors.New("down")
			}
			<-ctx.Done()
			return nil
		}})
	stop := start(t, r)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(began) == 3 })
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if first, second := began[1].Sub(began[0]), began[2].Sub(began[1]); first < 100*time.Millisecond || second < 200*time.Millisecond {
		t.Fatalf("restarted after %s, then %s", first, second)
	}
}

// A task's panic during shutdown is the exit error, restartable or not.
func TestARestartableTasksPanicDuringShutdownIsReturned(t *testing.T) {
	r := newRunner(t, runner.Options{Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil)},
		[]runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "sweeper", Kind: runner.Restartable, Run: func(ctx context.Context) error { <-ctx.Done(); panic("sweep failed") }})
	stop := start(t, r)
	if err := stop(); err == nil || !strings.Contains(err.Error(), "sweep failed") {
		t.Fatalf("Run returned %v", err)
	}
}

// Every task says whether its failure restarts it or stops the process.
func TestATaskWithoutAKindIsRefused(t *testing.T) {
	var ran atomic.Bool
	r := newRunner(t, runner.Options{}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "unsure", Run: func(context.Context) error { ran.Store(true); return nil }})
	if err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "unsure") || ran.Load() {
		t.Fatalf("Run returned %v, the task ran: %v", err, ran.Load())
	}
}

// A restart counts as it begins: a shutdown during the wait before it counts none.
func TestAShutdownDuringTheWaitCountsNoRestart(t *testing.T) {
	counted := &restarts{}
	var runs atomic.Int32
	failed := make(chan struct{})
	r := newRunner(t, runner.Options{Restarts: counted}, []runner.Listener{{Name: "http", Addr: "127.0.0.1:0", Server: runner.HTTP(http.NotFoundHandler())}},
		runner.Task{Name: "loop", Kind: runner.Restartable, Run: func(context.Context) error {
			if runs.Add(1) == 1 {
				close(failed)
			}
			return errors.New("down")
		}})
	stop := start(t, r)
	<-failed
	if err := stop(); err != nil { // well within the 100 ms wait
		t.Fatal(err)
	}
	if runs.Load() != 1 || counted.get("", "loop") != 0 {
		t.Fatalf("%d runs, %d restarts counted; want 1 and none", runs.Load(), counted.get("", "loop"))
	}
}
