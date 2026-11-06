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

	"finga/pkg/panics"
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
	Logger *slog.Logger
	// ShutdownGrace bounds the whole shutdown: Drain may take half of it, and the listeners have what is left.
	ShutdownGrace time.Duration
	// Drain runs after readiness drops and before the listeners stop; nil skips it.
	Drain func(ctx context.Context)
	// Panics records a task's panic, which then shuts the process down; nil logs it to Logger.
	Panics *panics.Recorder
}

// Runner is one process.
type Runner struct {
	opts      Options
	listeners []Listener
	tasks     []Task
	ready     atomic.Bool
}

// New makes a runner; Run starts it.
func New(opts Options, listeners []Listener, tasks []Task) *Runner {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Panics == nil {
		opts.Panics = panics.NewRecorder(opts.Logger, nil)
	}
	for _, l := range listeners {
		if s, ok := l.Server.(*httpServer); ok && s.srv.ErrorLog == nil {
			s.srv.ErrorLog = slog.NewLogLogger(opts.Logger.Handler(), slog.LevelError) // a panic no handler recovered, among others
		}
	}
	return &Runner{opts: opts, listeners: listeners, tasks: tasks}
}

// Ready reports whether the process can take traffic now.
func (r *Runner) Ready() bool { return r.ready.Load() }

// runTasks runs the background tasks until ctx ends and they all return. A task's panic, or a task that
// returns before ctx ends, goes to fatal: the process would otherwise stay ready with a dead loop.
func (r *Runner) runTasks(ctx context.Context, fatal func(error)) {
	var wg sync.WaitGroup
	for _, t := range r.tasks {
		wg.Go(func() {
			err := run(ctx, t)
			switch e, ok := errors.AsType[*panics.Error](err); {
			case ok:
				r.opts.Panics.Record(ctx, panics.FlowTask, e, "task", t.Name, "err", err.Error())
				fatal(fmt.Errorf("task %s: %w", t.Name, err))
			case ctx.Err() == nil:
				fatal(fmt.Errorf("task %s: %w", t.Name, errors.Join(errTaskEnded, err)))
			case err != nil:
				r.opts.Logger.ErrorContext(ctx, "task stopped with an error", "task", t.Name, "err", err)
			}
		})
	}
	wg.Wait()
}

var errTaskEnded = errors.New("stopped before shutdown")

func run(ctx context.Context, t Task) (err error) {
	defer panics.Capture(&err)
	return t.Run(ctx)
}

// drainShare is the part of the grace Drain may take, one in drainShare, so a slow drain never cuts the listeners' requests in flight.
const drainShare = 2

// Run binds all listeners or none, serves until ctx ends or a listener fails, then shuts down.
func (r *Runner) Run(ctx context.Context) error {
	lns, err := r.bind()
	if err != nil {
		return err
	}
	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	var (
		once     sync.Once
		fatalErr error // read after tasks.Wait
	)
	fatal := func(err error) {
		once.Do(func() { fatalErr = err })
		stop(err)
	}
	taskCtx, stopTasks := context.WithCancel(context.WithoutCancel(ctx))
	var tasks sync.WaitGroup
	tasks.Go(func() { r.runTasks(taskCtx, fatal) })

	serving, servingCtx := errgroup.WithContext(runCtx)
	for i, ln := range lns {
		l := r.listeners[i]
		serving.Go(func() error {
			if err := l.Server.Serve(ln); err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
			return nil
		})
	}
	r.ready.Store(true)
	for i, l := range r.listeners {
		r.opts.Logger.InfoContext(ctx, "listening", "listener", l.Name, "addr", lns[i].Addr().String())
	}

	<-servingCtx.Done()
	failed := runCtx.Err() == nil // the serving context ended on its own: a listener failed
	r.ready.Store(false)
	r.opts.Logger.InfoContext(ctx, "shutting down", "grace", r.opts.ShutdownGrace)
	graceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.opts.ShutdownGrace)
	defer cancel()
	if r.opts.Drain != nil && !failed {
		drainCtx, drained := context.WithTimeout(graceCtx, r.opts.ShutdownGrace/drainShare)
		r.opts.Drain(drainCtx)
		drained()
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
	if fatalErr != nil {
		return fatalErr
	}
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

// No whole-request read or write timeout: either would end a WebSocket; a handler sets its own deadlines.
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 2 * time.Minute // a kept-alive connection between requests
)

// HTTP serves h.
func HTTP(h http.Handler) Server {
	return &httpServer{srv: &http.Server{Handler: h, ReadHeaderTimeout: readHeaderTimeout, IdleTimeout: idleTimeout}}
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
