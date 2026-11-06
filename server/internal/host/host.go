// Package host runs one service: its gRPC or public HTTP listener, the ops endpoints, background tasks and graceful shutdown.
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

	"finga/internal/telemetry"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
	"finga/pkg/panics"
	"finga/pkg/runner"
	"finga/pkg/tracing"
)

// traceFlushTimeout bounds the export of the spans left at Close.
const traceFlushTimeout = 5 * time.Second

// The listener names, as the logs show them.
const (
	grpcListener = "grpc"
	opsListener  = "ops"
)

// Options describe one service.
type Options struct {
	Name     string
	Settings Settings // with GRPC set, the host serves gRPC there
	Logger   *slog.Logger
	Registry *prometheus.Registry // served on /metrics
	Errors   grpcx.ErrorTable     // what the gRPC handlers' errors answer
	// Roots reports whether a root span of this name, one of the service's entry points, starts a sampled trace.
	Roots func(name string) bool
}

// Host is one service's process.
type Host struct {
	// GRPC is the server to register the service's handlers on; nil for a service that serves no gRPC.
	GRPC *grpc.Server
	// Panics records the process's recovered panics and counts them on /metrics.
	Panics *panics.Recorder
	// Traces mints every span's IDs and exports the sampled ones; Close flushes it.
	Traces trace.TracerProvider

	opts      Options
	logger    *slog.Logger
	metrics   *grpcprom.ServerMetrics
	clients   *grpcprom.ClientMetrics
	tracing   grpcx.Tracing
	listeners []runner.Listener
	tasks     []runner.Task
	drains    []func(ctx context.Context)
	health    *grpchealth.Server
	drainGRPC func()
	run       *runner.Runner
	closers   []func()
}

// New builds the process; add its handlers, listeners and tasks, then Run.
func New(opts Options) (*Host, error) {
	panicCount, err := observability.NewPanicCounter(opts.Registry)
	if err != nil {
		return nil, err
	}
	h := &Host{Panics: panics.NewRecorder(opts.Logger, panicCount), opts: opts, logger: opts.Logger.With("service", opts.Name), clients: grpcx.NewClientMetrics()}
	if err := opts.Registry.Register(h.clients); err != nil {
		return nil, err
	}
	traces, err := tracing.New(context.Background(), opts.Settings.Tracing, tracing.Options{Service: opts.Name, Root: opts.Roots})
	if err != nil {
		return nil, err
	}
	h.Traces = traces
	h.tracing = grpcx.Tracing{Provider: traces}
	h.OnClose(func() { // first in, so it runs last: the spans the rest end are flushed too
		ctx, cancel := context.WithTimeout(context.Background(), traceFlushTimeout)
		defer cancel()
		_ = traces.Shutdown(ctx)
	})
	if opts.Settings.GRPC != "" {
		h.serveGRPC()
	}
	return h, nil
}

func (h *Host) serveGRPC() {
	h.metrics = grpcx.NewServerMetrics()
	h.opts.Registry.MustRegister(h.metrics) // a fresh registry per process: only a second Host on it fails
	draining := make(chan struct{})
	h.GRPC = grpcx.NewServer(grpcx.ServerOptions{Errors: h.opts.Errors, Logger: h.logger, Panics: h.Panics, Metrics: h.metrics, Draining: draining, Tracing: &h.tracing})
	h.health = grpchealth.NewServer()
	healthpb.RegisterHealthServer(h.GRPC, health{Server: h.health, draining: draining})
	h.listeners = append(h.listeners, runner.Listener{Name: grpcListener, Addr: h.opts.Settings.GRPC, Server: runner.GRPC(h.GRPC)})
	// Health turns NOT_SERVING, which every watcher hears before its stream ends; subscriptions, which never end on their own, end so GracefulStop waits only for calls.
	h.drainGRPC = func() {
		h.health.Shutdown()
		close(draining)
	}
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

// Task adds a background job that runs until the process stops; its failure stops the process.
func (h *Host) Task(name string, run func(ctx context.Context) error) {
	h.tasks = append(h.tasks, runner.Task{Name: name, Run: run})
}

// OnDrain adds a step that runs once readiness drops, before the listeners stop.
func (h *Host) OnDrain(drain func(ctx context.Context)) { h.drains = append(h.drains, drain) }

// Database opens the pool cfg names, which Close closes, with the metrics its repository reports to.
func (h *Host) Database(ctx context.Context, cfg dbx.Config) (*pgxpool.Pool, *telemetry.DBMetrics, error) {
	metrics, err := telemetry.NewDBMetrics(h.opts.Registry)
	if err != nil {
		return nil, nil, err
	}
	pool, err := dbx.NewPool(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	h.OnClose(pool.Close)
	return pool, metrics, nil
}

// Run serves until ctx ends, then shuts down gracefully; each gRPC service reports SERVING and its metrics start at 0.
func (h *Host) Run(ctx context.Context) error {
	if h.GRPC != nil {
		h.metrics.InitializeMetrics(h.GRPC)
		for service := range h.GRPC.GetServiceInfo() {
			h.health.SetServingStatus(service, healthpb.HealthCheckResponse_SERVING)
		}
	}
	ops := runner.Probes(func() bool { return h.run.Ready() }, observability.MetricsHandler(h.opts.Registry))
	listeners := append(slices.Clone(h.listeners), runner.Listener{Name: opsListener, Addr: h.opts.Settings.Ops, Server: runner.HTTP(ops)})
	h.run = runner.New(runner.Options{Logger: h.logger, ShutdownGrace: h.opts.Settings.ShutdownGrace, Panics: h.Panics, Drain: h.drain}, listeners, h.tasks)
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
