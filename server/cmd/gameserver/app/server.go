// Package app is the game server's composition root and its process. Build
// wires the gateway and the REST handler over a client for each service;
// Server runs the HTTP listener on the router from internal/handler, with
// the background tasks (the streams the gateway follows and its session
// sweeper) and graceful shutdown, on pkg/runner. New takes ready-made
// parts, for tests and for Build.
//
// Each binary wires its own dependencies next to its main. This one lives in
// a package of its own, not in main, because Go cannot import a main package
// and the end-to-end tests build the server exactly as production does.
package app

import (
	"context"
	"log/slog"
	"net/http"

	"finga/internal/config"
	"finga/internal/handler"
	"finga/pkg/runner"
)

// Task is a background job that runs beside the listener and stops when
// the server's context ends.
type Task = runner.Task

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

// listener is the one listener's name in the runner.
const listener = "http"

// Server is the game server's process: one HTTP listener for the router,
// and the background tasks, on pkg/runner (readiness, graceful shutdown).
type Server struct {
	run     *runner.Runner
	handler http.Handler
	closers []func()
}

// New builds the router over the handlers, with readiness from this
// server's runner. Run starts listening.
func New(opts Options, deps Deps) *Server {
	s := &Server{}
	s.handler = handler.NewRouter(handler.Routes{
		API:     deps.API,
		Gateway: deps.Gateway,
		Metrics: deps.Metrics,
		Ready:   func() bool { return s.run.Ready() },
	})
	s.run = runner.New(runner.Options{Logger: opts.Logger, ShutdownGrace: opts.Config.ShutdownGrace},
		[]runner.Listener{{Name: listener, Addr: opts.Config.Addr, Server: runner.HTTP(s.handler)}}, deps.Tasks)
	return s
}

// Handler is the whole server as one handler, for tests through httptest.
func (s *Server) Handler() http.Handler { return s.handler }

// Addr is the listening address once Run has bound it; empty before.
func (s *Server) Addr() string { return s.run.Addr(listener) }

// RunTasks starts the background tasks and returns when they have all
// stopped; they stop when ctx ends. Run calls it; tests that serve the
// handler through httptest call it themselves.
func (s *Server) RunTasks(ctx context.Context) { s.run.RunTasks(ctx) }

// Run listens on the configured address and runs the tasks until ctx is
// done, then shuts down gracefully within the grace period: readiness
// drops, no new connections, in-flight requests finish, tasks flush. It
// returns nil after a clean shutdown.
func (s *Server) Run(ctx context.Context) error { return s.run.Run(ctx) }

// Close releases what Build opened (the pool). Safe after Run.
func (s *Server) Close() {
	for _, c := range s.closers {
		c()
	}
}
