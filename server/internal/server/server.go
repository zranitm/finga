// Package server wires the game server and runs its HTTP listener: /api
// (REST), /ws (the gateway), /healthz, /readyz and /metrics, with the
// background tasks (the world's instances and the persistence worker) and
// graceful shutdown. Build constructs everything from configuration; New
// takes ready-made parts, for tests and for Build.
package server

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
)

const (
	readTimeout = 10 * time.Second
	readyPing   = 2 * time.Second
)

// Pinger is what readiness needs from the database.
type Pinger interface {
	Ping(ctx context.Context) error
}

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

// Deps are the handlers the server mounts and the tasks it runs.
type Deps struct {
	API     http.Handler // /api/
	Gateway http.Handler // /ws; nil mounts nothing
	Metrics http.Handler // /metrics
	DB      Pinger
	Tasks   []Task
}

// Server is the HTTP server, its probes, and its background tasks.
type Server struct {
	cfg     config.Server
	log     *slog.Logger
	deps    Deps
	mux     *http.ServeMux
	ready   atomic.Bool
	mu      sync.Mutex
	addr    string
	closers []func()
}

// New mounts the handlers. Run starts listening.
func New(opts Options, deps Deps) *Server {
	s := &Server{cfg: opts.Config, log: opts.Logger, deps: deps, mux: http.NewServeMux()}
	s.mux.Handle("/api/", deps.API)
	if deps.Gateway != nil {
		s.mux.Handle("GET /ws", deps.Gateway)
	}
	s.mux.Handle("GET /metrics", deps.Metrics)
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /readyz", s.readyz)
	return s
}

// Handler is the whole server as one handler, for tests through httptest.
func (s *Server) Handler() http.Handler { return s.mux }

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
	srv := &http.Server{Handler: s.mux, ReadHeaderTimeout: readTimeout}
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

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// readyz is 200 once the server is listening (game data loaded, everything
// wired) and the database answers a ping; 503 otherwise, with no detail.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"starting"}` + "\n"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readyPing)
	defer cancel()
	if err := s.deps.DB.Ping(ctx); err != nil {
		s.log.WarnContext(ctx, "readiness: database ping failed", "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"database unavailable"}` + "\n"))
		return
	}
	_, _ = w.Write([]byte(`{"status":"ready"}` + "\n"))
}
