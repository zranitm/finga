// Package host runs one process and the modules in it: its listeners, the ops endpoints, background tasks and graceful shutdown.
package host

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"finga/internal/facade"
	"finga/internal/gamedata"
	"finga/internal/gamedata/load"
	"finga/pkg/dbx"
	"finga/pkg/dbx/dbxprom"
	"finga/pkg/grpcx"
	"finga/pkg/observability/metrics"
	"finga/pkg/observability/tracing"
	"finga/pkg/panics"
	"finga/pkg/runner"
)

// TraceFlushTimeout bounds the export of the spans left at Close, inside the margin Compose leaves after shutdown_grace.
const TraceFlushTimeout = 2 * time.Second

// The listener names, as the logs show them.
const (
	grpcListener = "grpc"
	opsListener  = "ops"
)

// Options describe one process.
type Options struct {
	Name     string // the process, as its own logs and its traces name it
	Settings Settings
	Logger   *slog.Logger
	Registry *prometheus.Registry // served on /metrics
}

// Host is one process, carrying one module or many.
type Host struct {
	// Traces mints every span's IDs and exports the sampled ones; Close flushes it.
	Traces trace.TracerProvider

	opts      Options
	logger    *slog.Logger
	restarts  *metrics.TaskRestarts
	roots     []func(name string) bool
	metrics   *grpcprom.ServerMetrics
	clients   *grpcprom.ClientMetrics
	tracing   grpcx.Tracing
	grpc      *grpc.Server
	listeners []runner.Listener
	tasks     []runner.Task
	drains    []func(ctx context.Context)
	health    *grpchealth.Server
	drainGRPC func()
	data      *gamedata.Data
	pool      *pgxpool.Pool
	run       *runner.Runner
	closers   []func()
}

// Kind is what a task's failure does; every task says which.
type Kind = runner.Kind

const (
	// Vital tasks own live game state: their failure drains the process and it exits non-zero.
	Vital = runner.Vital
	// Restartable tasks keep no live state of their own: a failure restarts the task with backoff.
	Restartable = runner.Restartable
)

// New builds the process; add its modules, then Run.
func New(opts Options) (*Host, error) {
	h := &Host{opts: opts, logger: opts.Logger.With("service", opts.Name), clients: grpcx.NewClientMetrics()}
	restarts, err := metrics.NewTaskRestarts(opts.Registry)
	if err != nil {
		return nil, err
	}
	h.restarts = restarts
	if err := opts.Registry.Register(h.clients); err != nil {
		return nil, err
	}
	traces, err := tracing.New(context.Background(), opts.Settings.Tracing, tracing.Options{Service: opts.Name, Version: buildVersion, Root: h.isRoot, Logger: h.logger})
	if err != nil {
		return nil, err
	}
	h.Traces = traces
	h.tracing = grpcx.Tracing{Provider: traces}
	h.OnClose(func() { // first in, so it runs last: the spans the rest end are flushed too
		ctx, cancel := context.WithTimeout(context.Background(), TraceFlushTimeout)
		defer cancel()
		_ = traces.Shutdown(ctx)
	})
	return h, nil
}

// isRoot reports whether a root span named name, one of a module's entry points, starts a sampled trace.
func (h *Host) isRoot(name string) bool {
	for _, root := range h.roots {
		if root(name) {
			return true
		}
	}
	return false
}

// Registry is where modules register their own metrics; it is served on /metrics.
func (h *Host) Registry() prometheus.Registerer { return h.opts.Registry }

// GameData is the game data in Settings.DataDir, loaded at the first call and shared by every module after.
func (h *Host) GameData() (*gamedata.Data, error) {
	if h.data == nil {
		data, err := load.Dir(h.opts.Settings.DataDir)
		if err != nil {
			return nil, err
		}
		h.data = data
	}
	return h.data, nil
}

// Module is one module's part of the process: its logs, panics, tasks and transactions carry its name.
type Module struct {
	// Logger is the process's, with the module's name.
	Logger *slog.Logger
	// Panics records the module's recovered panics and counts them under its name.
	Panics *panics.Recorder

	name string
	h    *Host
}

// Module is the part of the process named name; a module asks for it once.
func (h *Host) Module(name string) (*Module, error) {
	count, err := metrics.NewPanicCounter(h.opts.Registry, name)
	if err != nil {
		return nil, err
	}
	log := h.opts.Logger.With("module", name)
	return &Module{Logger: log, Panics: panics.NewRecorder(log, count), name: name, h: h}, nil
}

// Task adds a background job that runs until the process stops; kind says what its failure does.
func (m *Module) Task(name string, run func(ctx context.Context) error, kind Kind) {
	if kind == Restartable {
		m.h.restarts.Init(m.name, name)
	}
	m.h.tasks = append(m.h.tasks, runner.Task{Name: name, Module: m.name, Kind: kind, Run: run, Panics: m.Panics})
}

// Calls traces, counts and times the module's facade calls and recovers their panics; errors maps its model's errors to the kernel's.
func (m *Module) Calls(errors facade.Errors) (*facade.Calls, error) {
	return facade.New(facade.Options{Module: m.name, Traces: m.h.Traces, Registry: m.h.Registry(), Panics: m.Panics, Errors: errors})
}

// Registry is where the module registers its own metrics, served on /metrics with every other module's.
func (m *Module) Registry() prometheus.Registerer { return m.h.Registry() }

// Traces is the process's tracer provider, for the module's own spans beyond its calls.
func (m *Module) Traces() trace.TracerProvider { return m.h.Traces }

// TraceRoots adds the module's entry points: a root span whose name roots reports starts a sampled trace.
func (m *Module) TraceRoots(roots func(name string) bool) { m.h.roots = append(m.h.roots, roots) }

// Database is the process's pool, opened from Settings.Database at the first call and closed by Close, and the module's transaction metrics.
func (m *Module) Database(ctx context.Context) (*pgxpool.Pool, *dbxprom.Metrics, error) {
	observer, err := dbxprom.New(m.h.opts.Registry, m.name)
	if err != nil {
		return nil, nil, err
	}
	if m.h.pool == nil {
		pool, err := dbx.NewPool(ctx, m.h.opts.Settings.Database)
		if err != nil {
			return nil, nil, err
		}
		m.h.pool = pool
		m.h.OnClose(pool.Close)
	}
	return m.h.pool, observer, nil
}

// ServeGRPC is the server the module registers its handlers on, at Settings.GRPC; its errors answer by errors, and its panics are the module's. One module per process serves gRPC.
func (m *Module) ServeGRPC(errors grpcx.ErrorTable) (*grpc.Server, error) {
	h := m.h
	switch {
	case h.opts.Settings.GRPC == "":
		return nil, fmt.Errorf("module %s: the process has no gRPC address", m.name)
	case h.grpc != nil:
		return nil, fmt.Errorf("module %s: the process already serves gRPC", m.name)
	}
	h.metrics = grpcx.NewServerMetrics()
	if err := h.opts.Registry.Register(h.metrics); err != nil {
		return nil, err
	}
	drainCtx, drained := context.WithCancel(context.Background())
	h.grpc = grpcx.NewServer(grpcx.ServerOptions{Errors: errors, Logger: h.logger, Panics: m.Panics, Metrics: h.metrics, DrainCtx: drainCtx, Tracing: &h.tracing})
	h.health = grpchealth.NewServer()
	healthpb.RegisterHealthServer(h.grpc, health{Server: h.health, drainCtx: drainCtx})
	h.listeners = append(h.listeners, runner.Listener{Name: grpcListener, Addr: h.opts.Settings.GRPC, Server: runner.GRPC(h.grpc)})
	// Health turns NOT_SERVING, which every watcher hears before its stream ends; subscriptions, which never end on their own, end so GracefulStop waits only for calls.
	h.drainGRPC = func() {
		h.health.Shutdown()
		drained()
	}
	return h.grpc, nil
}

// Dial connects to the named service at addr; its calls are counted on /metrics and traced, and Close closes it.
func (h *Host) Dial(name, addr string) (*grpc.ClientConn, error) {
	conn, err := grpcx.Dial(addr, append(grpcx.ClientMetrics(h.clients), grpcx.TraceCalls(h.tracing))...)
	if err != nil {
		return nil, fmt.Errorf("%s service: %w", name, err)
	}
	h.OnClose(func() { _ = conn.Close() })
	return conn, nil
}

// Serve adds a listener that serves handler over HTTP, as the gateway serves its players.
func (h *Host) Serve(name, addr string, handler http.Handler) {
	h.listeners = append(h.listeners, runner.Listener{Name: name, Addr: addr, Server: runner.HTTP(handler)})
}

// OnDrain adds a step that runs once readiness drops, before the listeners stop.
func (h *Host) OnDrain(drain func(ctx context.Context)) { h.drains = append(h.drains, drain) }

// Run serves until ctx ends, then shuts down gracefully; each gRPC service reports SERVING and its metrics start at 0.
func (h *Host) Run(ctx context.Context) error {
	if h.grpc != nil {
		h.metrics.InitializeMetrics(h.grpc)
		for service := range h.grpc.GetServiceInfo() {
			h.health.SetServingStatus(service, healthpb.HealthCheckResponse_SERVING)
		}
	}
	ops := runner.Probes(func() bool { return h.run.Ready() }, metrics.Handler(h.opts.Registry))
	listeners := append(slices.Clone(h.listeners), runner.Listener{Name: opsListener, Addr: h.opts.Settings.Ops, Server: runner.HTTP(ops)})
	h.run = runner.New(runner.Options{Logger: h.logger, ShutdownGrace: h.opts.Settings.ShutdownGrace, Drain: h.drain, Restarts: h.restarts}, listeners, h.tasks)
	return h.run.Run(ctx)
}

// drain runs the service's own steps, then the gRPC server's, whose streams end last.
func (h *Host) drain(ctx context.Context) {
	for _, d := range h.drains {
		d(ctx)
	}
	if h.drainGRPC != nil {
		h.drainGRPC()
	}
}

// OnClose adds something Close releases (a pool, a client connection).
func (h *Host) OnClose(f func()) { h.closers = append(h.closers, f) }

// Close releases what the composition root opened, last first.
func (h *Host) Close() {
	for _, closer := range slices.Backward(h.closers) {
		closer()
	}
}
