// Package app wires and runs the gateway; it is not main so the end-to-end tests can import it.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"finga/pkg/panics"
	"finga/pkg/runner"
	"finga/services/gateway/config"
	"finga/services/gateway/internal/handler"
)

// Task is a background job that stops when the server's context ends.
type Task = runner.Task

// Options are the listener's settings.
type Options struct {
	HTTP          config.HTTP
	ShutdownGrace time.Duration
	Logger        *slog.Logger
	Panics        *panics.Recorder // a task's panic, which shuts the gateway down; nil logs it
}

// Deps are the handlers the router mounts and the tasks the server runs.
type Deps struct {
	API     func(e *gin.Engine) // nil mounts nothing
	Gateway http.Handler        // /ws; nil mounts nothing
	Metrics http.Handler        // /metrics
	Tasks   []Task
	// Drain runs on shutdown after readiness turns 503, before the listener closes; nil skips it.
	Drain func(ctx context.Context)
}

const listener = "http"

// Server is the gateway's process: one HTTP listener and the background tasks.
type Server struct {
	run     *runner.Runner
	handler http.Handler
	closers []func()
}

// New builds the router over the handlers; Run starts listening.
func New(opts Options, deps Deps) *Server {
	s := &Server{}
	s.handler = handler.NewRouter(handler.Routes{
		API:     deps.API,
		Gateway: deps.Gateway,
		Metrics: deps.Metrics,
		Ready:   func() bool { return s.run.Ready() },
	})
	s.run = runner.New(runner.Options{Logger: opts.Logger, ShutdownGrace: opts.ShutdownGrace, Drain: deps.Drain, Panics: opts.Panics},
		[]runner.Listener{{Name: listener, Addr: opts.HTTP.Addr, Server: runner.HTTP(s.handler)}}, deps.Tasks)
	return s
}

// Handler is the whole server as one handler, for tests through httptest.
func (s *Server) Handler() http.Handler { return s.handler }

// Addr is the listening address once Run has bound it; empty before.
func (s *Server) Addr() string { return s.run.Addr(listener) }

// RunTasks runs the background tasks until ctx ends; tests serving through httptest call it directly.
func (s *Server) RunTasks(ctx context.Context) { s.run.RunTasks(ctx) }

// Run listens and runs the tasks until ctx is done, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error { return s.run.Run(ctx) }

// Close releases what Build opened. Safe after Run.
func (s *Server) Close() {
	for _, c := range s.closers {
		c()
	}
}
