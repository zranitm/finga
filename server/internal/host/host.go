// Package host runs one internal service: gRPC, the ops endpoints, background tasks and graceful shutdown.
package host

import (
	"context"
	"log/slog"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"finga/pkg/grpcx"
	"finga/pkg/observability"
	"finga/pkg/panics"
	"finga/pkg/runner"
)

// The listener names, as the logs show them.
const (
	grpcListener = "grpc"
	opsListener  = "ops"
)

// Options describe one service.
type Options struct {
	Name     string
	Settings Settings
	Logger   *slog.Logger
	Registry *prometheus.Registry // served on /metrics
	Errors   grpcx.ErrorTable
	Tasks    []runner.Task
	// Panics records the process's recovered panics; nil logs them to Logger.
	Panics *panics.Recorder
}

// Host is one service's process.
type Host struct {
	// GRPC is the server to register the service's handlers on.
	GRPC    *grpc.Server
	run     *runner.Runner
	closers []func()
}

// New builds the process; register handlers on GRPC, then Run.
func New(opts Options) *Host {
	logger := opts.Logger.With("service", opts.Name)
	if opts.Panics == nil {
		opts.Panics = panics.NewRecorder(opts.Logger, nil)
	}
	draining := make(chan struct{})
	h := &Host{GRPC: grpcx.NewServer(grpcx.ServerOptions{Errors: opts.Errors, Logger: logger, Panics: opts.Panics, Draining: draining})}
	ops := runner.Probes(func() bool { return h.run.Ready() }, observability.MetricsHandler(opts.Registry))
	// Subscriptions never end on their own; the drain ends them so GracefulStop waits only for calls.
	drain := func(context.Context) { close(draining) }
	h.run = runner.New(runner.Options{Logger: logger, ShutdownGrace: opts.Settings.ShutdownGrace, Panics: opts.Panics, Drain: drain}, []runner.Listener{
		{Name: grpcListener, Addr: opts.Settings.GRPC, Server: runner.GRPC(h.GRPC)},
		{Name: opsListener, Addr: opts.Settings.Ops, Server: runner.HTTP(ops)},
	}, opts.Tasks)
	return h
}

// Run serves until ctx ends, then shuts down gracefully.
func (h *Host) Run(ctx context.Context) error { return h.run.Run(ctx) }

// OnClose adds something Close releases (a pool, a client connection).
func (h *Host) OnClose(f func()) { h.closers = append(h.closers, f) }

// Close releases what the composition root opened, last first.
func (h *Host) Close() {
	for _, closer := range slices.Backward(h.closers) {
		closer()
	}
}
