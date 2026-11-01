package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finga/services/gateway/app"
	"finga/services/gateway/config"
)

func newServer(t *testing.T) *app.Server {
	t.Helper()
	return app.New(app.Options{
		HTTP:          config.HTTP{Addr: "127.0.0.1:0"},
		ShutdownGrace: 2 * time.Second,
		Logger:        slog.New(slog.DiscardHandler),
	}, app.Deps{Metrics: http.NotFoundHandler()})
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// Readiness checks no dependency, so a shared outage cannot pull every instance out of rotation at once.
func TestReadinessFollowsTheProcess(t *testing.T) {
	srv := newServer(t)
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

// The drain runs after readiness turns 503, so the load balancer stops sending new connections first.
func TestTheDrainRunsAfterReadinessTurnsOff(t *testing.T) {
	var srv *app.Server
	drained := make(chan int, 1)
	srv = app.New(app.Options{
		HTTP:          config.HTTP{Addr: "127.0.0.1:0"},
		ShutdownGrace: 2 * time.Second,
		Logger:        slog.New(slog.DiscardHandler),
	}, app.Deps{Metrics: http.NotFoundHandler(), Drain: func(context.Context) {
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
