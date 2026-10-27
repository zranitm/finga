// Package runner runs one process: its listeners (HTTP or gRPC) and its
// background tasks, with readiness and graceful shutdown. Readiness is
// local: ready from the moment every listener is bound until shutdown
// starts, checking no dependency. On shutdown, readiness drops first, then
// an optional drain runs while the listeners still serve, then the
// listeners stop accepting and let what is in flight finish within the
// grace period, then the tasks stop.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
)

// Server is what a listener serves.
type Server interface {
	// Serve accepts on ln until Shutdown; it returns nil once shut down.
	Serve(ln net.Listener) error
	// Shutdown stops accepting and waits for the work in flight, until ctx
	// ends; then it stops whatever is left.
	Shutdown(ctx context.Context) error
}

// Listener is one address and what it serves.
type Listener struct {
	Name   string // for logs and Addr
	Addr   string // host:port; port 0 picks a free one
	Server Server
}

// Task is a background job that runs beside the listeners and stops when
// the runner's context ends.
type Task struct {
	Name string
	Run  func(ctx context.Context) error
}

// Options tune the runner.
type Options struct {
	Logger        *slog.Logger
	ShutdownGrace time.Duration
	// Drain runs once readiness has dropped and before the listeners stop,
	// with the grace period's context: the gateway tells its connections
	// to go here. nil skips it.
	Drain func(ctx context.Context)
}

// Runner is one process.
type Runner struct {
	opts      Options
	listeners []Listener
	tasks     []Task
	ready     atomic.Bool

	mu    sync.Mutex
	addrs map[string]string
}

// New makes a runner; Run starts it.
func New(opts Options, listeners []Listener, tasks []Task) *Runner {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Runner{opts: opts, listeners: listeners, tasks: tasks, addrs: map[string]string{}}
}

// Ready reports whether the process can take traffic now.
func (r *Runner) Ready() bool { return r.ready.Load() }

// Addr is a listener's bound address once Run serves; empty before. It
// turns non-empty together with Ready, so a caller that sees an address
// also sees the process ready.
func (r *Runner) Addr(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addrs[name]
}

// RunTasks runs the background tasks until ctx ends and they have all
// returned. Run calls it; a test serving a handler through httptest calls
// it itself.
func (r *Runner) RunTasks(ctx context.Context) {
	var wg sync.WaitGroup
	for _, t := range r.tasks {
		wg.Add(1)
		go func(t Task) {
			defer wg.Done()
			if err := t.Run(ctx); err != nil {
				r.opts.Logger.ErrorContext(ctx, "task stopped with an error", "task", t.Name, "err", err)
			}
		}(t)
	}
	wg.Wait()
}

// Run binds every listener (failing, with nothing started, if one cannot
// bind), serves them and runs the tasks until ctx ends or a listener
// fails, then shuts down. It returns nil after a clean shutdown.
func (r *Runner) Run(ctx context.Context) error {
	lns, err := r.bind()
	if err != nil {
		return err
	}
	taskCtx, stopTasks := context.WithCancel(context.WithoutCancel(ctx))
	tasksDone := make(chan struct{})
	go func() { defer close(tasksDone); r.RunTasks(taskCtx) }()

	served := make(chan error, len(lns))
	for i, ln := range lns {
		go func(l Listener, ln net.Listener) {
			if err := l.Server.Serve(ln); err != nil {
				served <- fmt.Errorf("%s: %w", l.Name, err)
				return
			}
			served <- nil
		}(r.listeners[i], ln)
	}
	r.mu.Lock()
	for i, l := range r.listeners {
		r.addrs[l.Name] = lns[i].Addr().String()
	}
	r.ready.Store(true)
	r.mu.Unlock()
	for _, l := range r.listeners {
		r.opts.Logger.InfoContext(ctx, "listening", "listener", l.Name, "addr", r.Addr(l.Name))
	}

	var failure error
	select {
	case failure = <-served:
	case <-ctx.Done():
	}
	r.ready.Store(false)
	r.opts.Logger.InfoContext(ctx, "shutting down", "grace", r.opts.ShutdownGrace)
	graceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.opts.ShutdownGrace)
	defer cancel()
	if r.opts.Drain != nil && failure == nil {
		r.opts.Drain(graceCtx)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(r.listeners))
	for _, l := range r.listeners {
		wg.Add(1)
		go func(l Listener) {
			defer wg.Done()
			if err := l.Server.Shutdown(graceCtx); err != nil {
				errs <- fmt.Errorf("%s: %w", l.Name, err)
			}
		}(l)
	}
	wg.Wait()
	close(errs)
	stopTasks()
	<-tasksDone
	if failure != nil {
		return failure
	}
	for err := range errs {
		return err
	}
	return nil
}

// bind listens on every address, or on none.
func (r *Runner) bind() ([]net.Listener, error) {
	lns := make([]net.Listener, 0, len(r.listeners))
	for _, l := range r.listeners {
		ln, err := net.Listen("tcp", l.Addr)
		if err != nil {
			for _, open := range lns {
				_ = open.Close()
			}
			return nil, fmt.Errorf("%s: %w", l.Name, err)
		}
		lns = append(lns, ln)
	}
	return lns, nil
}

const readHeaderTimeout = 10 * time.Second

// HTTP serves h.
func HTTP(h http.Handler) Server {
	return &httpServer{srv: &http.Server{Handler: h, ReadHeaderTimeout: readHeaderTimeout}}
}

type httpServer struct{ srv *http.Server }

// Serve serves HTTP on ln until Shutdown.
func (s *httpServer) Serve(ln net.Listener) error {
	if err := s.srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown closes the listener and waits for requests in flight until ctx
// ends, then closes what is left.
func (s *httpServer) Shutdown(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		_ = s.srv.Close()
		return err
	}
	return nil
}

// GRPC serves srv. Shutdown stops it gracefully, and stops it outright
// when the grace period ends first (a stream to a gateway never ends on
// its own).
func GRPC(srv *grpc.Server) Server { return &grpcServer{srv: srv} }

type grpcServer struct{ srv *grpc.Server }

// Serve serves gRPC on ln until Shutdown.
func (s *grpcServer) Serve(ln net.Listener) error {
	if err := s.srv.Serve(ln); !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	return nil
}

// Shutdown stops gracefully, or outright once ctx ends.
func (s *grpcServer) Shutdown(ctx context.Context) error {
	stopped := make(chan struct{})
	go func() { s.srv.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-ctx.Done():
		s.srv.Stop()
		<-stopped
	}
	return nil
}
