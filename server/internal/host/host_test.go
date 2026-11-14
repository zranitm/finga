package host_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/observability/metrics"
	"finga/pkg/observability/metrics/promtest"
	"finga/pkg/observability/tracing"
	"finga/pkg/panics"
	"finga/pkg/runner/runnertest"
)

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

// newPublic is a process that serves players over HTTP, as the game does.
func newPublic(t *testing.T, log *runnertest.Log) *host.Host {
	t.Helper()
	h := newHost(t, host.Options{Name: "edge", Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: 2 * time.Second}, Logger: log.Logger(), Registry: metrics.NewRegistry()})
	h.Serve("http", "127.0.0.1:0", http.NotFoundHandler())
	return h
}

// A process runs its modules' tasks and serves its ops endpoints until it stops, then releases what it opened, last first.
func TestAProcessRunsItsTasksAndClosesWhatItOpened(t *testing.T) {
	ran := make(chan struct{})
	var closed []string
	log := runnertest.NewLog()
	h := newHost(t, host.Options{
		Name:     "shop",
		Settings: host.Settings{Ops: "127.0.0.1:0", ShutdownGrace: time.Second},
		Logger:   log.Logger(),
		Registry: metrics.NewRegistry(), // with the Go collectors, as every binary builds it
	})
	module(t, h, "shop").Task("worker", func(ctx context.Context) error { close(ran); <-ctx.Done(); return nil }, host.Vital)
	h.OnClose(func() { closed = append(closed, "pool") })
	h.OnClose(func() { closed = append(closed, "cache") })

	p := runnertest.Start(t, log, h.Run, "ops")
	opsAddr := p.Addr("ops")
	<-ran
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
	if want := []string{"cache", "pool"}; !slices.Equal(closed, want) {
		t.Fatalf("Close released %v; want %v", closed, want)
	}
	if res, err := http.Get("http://" + opsAddr + "/readyz"); err == nil {
		res.Body.Close()
		t.Fatal("ops still answers after shutdown")
	}
}

func TestAProcessServesItsHTTPListenerAndOps(t *testing.T) {
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
	calls, err := shop.Calls(nil)
	if err != nil {
		t.Fatal(err)
	}
	sell := func(ctx context.Context) (err error) {
		_, call := calls.Start(ctx, "Sell")
		defer call.End(&err)
		panic("till jammed")
	}
	if err := sell(context.Background()); !errors.Is(err, kernel.ErrInternal) {
		t.Fatalf("a panicking call returned %v", err)
	}
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
		{"finga_panics_total", []string{"module", "shop", "flow", "call"}, 1},
		{"finga_module_calls_total", []string{"module", "shop", "method", "Sell", "code", "internal"}, 1},
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
