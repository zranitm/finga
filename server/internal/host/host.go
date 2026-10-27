// Package host runs one internal service the way every Finga service runs
// (Low-level design, page 10): a gRPC server on its listen address, built
// by pkg/grpcx with the service's error table, and /healthz, /readyz and
// /metrics on its ops address, with background tasks, readiness and
// graceful shutdown on pkg/runner. A service's composition root makes a
// Host, registers its gRPC handlers on GRPC, and runs it.
package host

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"finga/internal/config"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
	"finga/pkg/runner"
)

// The listener names, for Addr.
const (
	GRPC = "grpc"
	Ops  = "ops"
)

// Options describe one service.
type Options struct {
	Name          string          // for logs
	Endpoint      config.Endpoint // Listen and Ops are used; Addr is for callers
	ShutdownGrace time.Duration
	Logger        *slog.Logger
	Registry      *prometheus.Registry // served on /metrics
	Errors        grpcx.ErrorTable     // the service's errors and their reasons
	Tasks         []runner.Task
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
	h := &Host{GRPC: grpcx.NewServer(grpcx.ServerOptions{Errors: opts.Errors, Logger: logger})}
	ops := runner.Probes(func() bool { return h.run.Ready() }, observability.MetricsHandler(opts.Registry))
	h.run = runner.New(runner.Options{Logger: logger, ShutdownGrace: opts.ShutdownGrace}, []runner.Listener{
		{Name: GRPC, Addr: opts.Endpoint.Listen, Server: runner.GRPC(h.GRPC)},
		{Name: Ops, Addr: opts.Endpoint.Ops, Server: runner.HTTP(ops)},
	}, opts.Tasks)
	return h
}

// Run serves until ctx ends, then shuts down gracefully.
func (h *Host) Run(ctx context.Context) error { return h.run.Run(ctx) }

// Ready reports whether the service is serving.
func (h *Host) Ready() bool { return h.run.Ready() }

// Addr is a listener's bound address (GRPC or Ops) once serving.
func (h *Host) Addr(listener string) string { return h.run.Addr(listener) }

// OnClose adds something Close releases (a pool, a client connection).
func (h *Host) OnClose(f func()) { h.closers = append(h.closers, f) }

// Close releases what the composition root opened, last first.
func (h *Host) Close() {
	for i := len(h.closers) - 1; i >= 0; i-- {
		h.closers[i]()
	}
}
