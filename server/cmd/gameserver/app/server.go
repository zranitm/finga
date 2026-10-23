// Package app is the game server's composition root and its process. Build
// wires every module from configuration, the one place that knows them all;
// Server runs the HTTP listener on the router from internal/handler, with
// the background tasks (the world's instances, the persistence worker, the
// session sweeper) and graceful shutdown. New takes ready-made parts, for
// tests and for Build.
//
// Each binary wires its own dependencies next to its main. This one lives in
// a package of its own, not in main, because Go cannot import a main package
// and the end-to-end tests build the server exactly as production does.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"finga/internal/config"
	"finga/internal/handler"
)

const readTimeout = 10 * time.Second

// Task is a background job that runs beside the listener and stops when
// the server's context ends.
type Task struct {
	Name string
	Run  func(ctx context.Context) error
}

// Options are the listener's settings.
type Options struct {
	Config config.Server
	Logger *slog.Logger
}

// Deps are the handlers the router mounts and the tasks the server runs.
type Deps struct {
	API     http.Handler // /api/
	Gateway http.Handler // /ws; nil mounts nothing
	Metrics http.Handler // /metrics
	Tasks   []Task
}

// Server is the HTTP listener, its readiness, and its background tasks.
type Server struct {
	cfg     config.Server
	log     *slog.Logger
	deps    Deps
	handler http.Handler
	ready   atomic.Bool
	mu      sync.Mutex
	addr    string
	closers []func()
}

// New builds the router over the handlers, with readiness from this
// server's state. Run starts listening.
func New(opts Options, deps Deps) *Server {
	s := &Server{cfg: opts.Config, log: opts.Logger, deps: deps}
	s.handler = handler.NewRouter(handler.Routes{
		API:     deps.API,
		Gateway: deps.Gateway,
		Metrics: deps.Metrics,
		Ready:   s.ready.Load,
	})
	return s
}

// Handler is the whole server as one handler, for tests through httptest.
func (s *Server) Handler() http.Handler { return s.handler }

// Addr is the listening address once Run has bound it; empty before.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// RunTasks starts the background tasks and returns when they have all
// stopped; they stop when ctx ends. Run calls it; tests that serve the
// handler through httptest call it themselves.
func (s *Server) RunTasks(ctx context.Context) {
	var wg sync.WaitGroup
	for _, t := range s.deps.Tasks {
		wg.Add(1)
		go func(t Task) {
			defer wg.Done()
			if err := t.Run(ctx); err != nil {
				s.log.ErrorContext(ctx, "task stopped with an error", "task", t.Name, "err", err)
			}
		}(t)
	}
	wg.Wait()
}

// Run listens on the configured address and runs the tasks until ctx is
// done, then shuts down gracefully within the grace period: no new
// connections, in-flight requests finish, tasks flush. It returns nil
// after a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.addr = ln.Addr().String()
	s.mu.Unlock()
	srv := &http.Server{Handler: s.handler, ReadHeaderTimeout: readTimeout}
	taskCtx, stopTasks := context.WithCancel(ctx)
	tasksDone := make(chan struct{})
	go func() { defer close(tasksDone); s.RunTasks(taskCtx) }()
	s.ready.Store(true)
	s.log.InfoContext(ctx, "listening", "addr", s.addr)

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		s.ready.Store(false)
		stopTasks()
		<-tasksDone
		return err
	case <-ctx.Done():
	}
	s.ready.Store(false)
	s.log.InfoContext(ctx, "shutting down", "grace", s.cfg.ShutdownGrace)
	graceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownGrace)
	defer cancel()
	shutdownErr := srv.Shutdown(graceCtx)
	stopTasks()
	<-tasksDone
	if shutdownErr != nil {
		return shutdownErr
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close releases what Build opened (the pool). Safe after Run.
func (s *Server) Close() {
	for _, c := range s.closers {
		c()
	}
}
