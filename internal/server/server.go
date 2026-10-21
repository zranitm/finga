// Package server wires the game server and runs its HTTP listener: /api
// (REST), /ws (the gateway, from step 10), /healthz, /readyz and /metrics,
// with graceful shutdown. Build constructs everything from configuration;
// New takes ready-made parts, for tests and for Build.
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

// Options are the listener's settings.
type Options struct {
	Config config.Server
	Logger *slog.Logger
}

// Deps are the handlers the server mounts.
type Deps struct {
	API     http.Handler // /api/
	Metrics http.Handler // /metrics
	DB      Pinger
}

// Server is the HTTP server and its probes.
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

// Run listens on the configured address until ctx is done, then shuts down
// gracefully within the grace period: no new connections, in-flight
// requests finish. It returns nil after a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.addr = ln.Addr().String()
	s.mu.Unlock()
	srv := &http.Server{Handler: s.mux, ReadHeaderTimeout: readTimeout}
	s.ready.Store(true)
	s.log.InfoContext(ctx, "listening", "addr", s.addr)

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		s.ready.Store(false)
		return err
	case <-ctx.Done():
	}
	s.ready.Store(false)
	s.log.InfoContext(ctx, "shutting down", "grace", s.cfg.ShutdownGrace)
	graceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(graceCtx); err != nil {
		return err
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
