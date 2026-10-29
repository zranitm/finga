package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finga/services/gateway/cmd/gateway/app"
	"finga/services/gateway/config"
)

func newServer(t *testing.T, api http.Handler) *app.Server {
	t.Helper()
	return app.New(app.Options{
		HTTP:          config.HTTP{Addr: "127.0.0.1:0"},
		ShutdownGrace: 2 * time.Second,
		Logger:        slog.New(slog.DiscardHandler),
	}, app.Deps{API: api, Metrics: http.NotFoundHandler()})
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// Readiness answers one question: can this process take traffic now? It is
// ready from the moment it listens until shutdown starts, and it checks no
// dependency: the database is shared by every instance, so failing on it
// would pull them all out of rotation at once (Build already refuses to
// start without it). The router (internal/handler) serves the probes and
// pkg/runner drives readiness (its tests cover in-flight requests on
// shutdown and a busy address); this test checks the two are wired.
func TestReadinessFollowsTheProcess(t *testing.T) {
	srv := newServer(t, http.NotFoundHandler())
	h := srv.Handler()

	t.Run("readyz is 503 before the server runs", func(t *testing.T) {
		if rec := get(t, h, "/readyz"); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", rec.Code)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	waitFor(t, func() bool { return srv.Addr() != "" })

	t.Run("readyz is 200 once listening", func(t *testing.T) {
		if rec := get(t, h, "/readyz"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ready") {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("the real listener answers", func(t *testing.T) {
		res, err := http.Get("http://" + srv.Addr() + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d", res.StatusCode)
		}
	})

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned %v", err)
	}
	t.Run("readyz is 503 again after shutdown", func(t *testing.T) {
		if rec := get(t, h, "/readyz"); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// On shutdown the drain runs once readiness has turned 503, so a load
// balancer has stopped sending new connections before the gateway tells
// the ones it holds to go, and before the listener closes.
func TestTheDrainRunsAfterReadinessTurnsOff(t *testing.T) {
	var srv *app.Server
	drained := make(chan int, 1)
	srv = app.New(app.Options{
		HTTP:          config.HTTP{Addr: "127.0.0.1:0"},
		ShutdownGrace: 2 * time.Second,
		Logger:        slog.New(slog.DiscardHandler),
	}, app.Deps{API: http.NotFoundHandler(), Metrics: http.NotFoundHandler(), Drain: func(context.Context) {
		res, err := http.Get("http://" + srv.Addr() + "/readyz") // the listener still serves while draining
		if err != nil {
			drained <- 0
			return
		}
		res.Body.Close()
		drained <- res.StatusCode
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	waitFor(t, func() bool { return srv.Addr() != "" })
	cancel()
	if code := <-drained; code != http.StatusServiceUnavailable {
		t.Fatalf("readyz answered %d while draining, want 503", code)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
