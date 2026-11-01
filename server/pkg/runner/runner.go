// Package runner runs a process's listeners and background tasks with readiness and graceful shutdown.
// Shutdown order: readiness drops, Drain runs, listeners stop within the grace period, then tasks stop.
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

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// Server is what a listener serves.
type Server interface {
	// Serve accepts on ln until Shutdown; it returns nil once shut down.
	Serve(ln net.Listener) error
	// Shutdown stops accepting and waits for in-flight work until ctx ends, then stops the rest.
	Shutdown(ctx context.Context) error
}

// Listener is one address and what it serves.
type Listener struct {
	Name   string
	Addr   string // host:port; port 0 picks a free one
	Server Server
}

// Task is a background job that runs until the runner's context ends.
type Task struct {
	Name string
	Run  func(ctx context.Context) error
}

// Options tune the runner.
type Options struct {
	Logger        *slog.Logger
	ShutdownGrace time.Duration
	// Drain runs after readiness drops and before the listeners stop; nil skips it.
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

// Addr is a listener's bound address once Run serves; it turns non-empty together with Ready.
func (r *Runner) Addr(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addrs[name]
}

// RunTasks runs the background tasks until ctx ends and they all return; Run calls it.
func (r *Runner) RunTasks(ctx context.Context) {
	var wg sync.WaitGroup
	for _, t := range r.tasks {
		wg.Go(func() {
			if err := t.Run(ctx); err != nil {
				r.opts.Logger.ErrorContext(ctx, "task stopped with an error", "task", t.Name, "err", err)
			}
		})
	}
	wg.Wait()
}

// Run binds all listeners or none, serves until ctx ends or a listener fails, then shuts down.
func (r *Runner) Run(ctx context.Context) error {
	lns, err := r.bind()
	if err != nil {
		return err
	}
	taskCtx, stopTasks := context.WithCancel(context.WithoutCancel(ctx))
	var tasks sync.WaitGroup
	tasks.Go(func() { r.RunTasks(taskCtx) })

	serving, servingCtx := errgroup.WithContext(ctx)
	for i, ln := range lns {
		l := r.listeners[i]
		serving.Go(func() error {
			if err := l.Server.Serve(ln); err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
			return nil
		})
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

	<-servingCtx.Done()
	failed := ctx.Err() == nil // the serving context ended on its own: a listener failed
	r.ready.Store(false)
	r.opts.Logger.InfoContext(ctx, "shutting down", "grace", r.opts.ShutdownGrace)
	graceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.opts.ShutdownGrace)
	defer cancel()
	if r.opts.Drain != nil && !failed {
		r.opts.Drain(graceCtx)
	}
	var shutdown errgroup.Group
	for _, l := range r.listeners {
		shutdown.Go(func() error {
			if err := l.Server.Shutdown(graceCtx); err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
			return nil
		})
	}
	shutdownErr := shutdown.Wait()
	servingErr := serving.Wait()
	stopTasks()
	tasks.Wait()
	if servingErr != nil {
		return servingErr
	}
	return shutdownErr
}

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

// Shutdown waits for in-flight requests until ctx ends, then closes the rest.
func (s *httpServer) Shutdown(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		_ = s.srv.Close()
		return err
	}
	return nil
}

// GRPC serves srv; Shutdown forces Stop at the deadline, as long-lived streams never end on their own.
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
