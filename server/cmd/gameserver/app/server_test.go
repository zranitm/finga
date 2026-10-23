package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finga/cmd/gameserver/app"
	"finga/internal/config"
)

func newServer(t *testing.T, api http.Handler) *app.Server {
	t.Helper()
	return app.New(app.Options{
		Config: config.Server{Addr: "127.0.0.1:0", ShutdownGrace: 2 * time.Second},
		Logger: slog.New(slog.DiscardHandler),
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
// start without it). The router (internal/handler) serves the probes; these
// tests check that the process drives them.
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

func TestShutdownLetsInFlightRequestsFinish(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})
	srv := newServer(t, api)
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()
	waitFor(t, func() bool { return srv.Addr() != "" })

	type result struct {
		status int
		body   string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		res, err := http.Get("http://" + srv.Addr() + "/api/slow")
		if err != nil {
			got <- result{err: err}
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		got <- result{status: res.StatusCode, body: string(body)}
	}()
	<-entered
	cancel() // shutdown starts while the request is in flight
	// Draining: readiness drops first, so a load balancer stops sending new
	// traffic while the in-flight request finishes.
	waitFor(t, func() bool { return get(t, srv.Handler(), "/readyz").Code == http.StatusServiceUnavailable })
	select {
	case err := <-runDone:
		t.Fatalf("Run returned %v before the in-flight request finished", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	r := <-got
	if r.err != nil || r.status != http.StatusOK || r.body != "done" {
		t.Fatalf("in-flight request: %+v", r)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run returned %v", err)
	}
	t.Run("a new connection after shutdown is refused", func(t *testing.T) {
		res, err := http.Get("http://" + srv.Addr() + "/healthz")
		if err == nil {
			res.Body.Close()
			t.Fatal("connected after shutdown")
		}
	})
}

func TestRunFailsOnABusyAddress(t *testing.T) {
	first := newServer(t, http.NotFoundHandler())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = first.Run(ctx) }()
	waitFor(t, func() bool { return first.Addr() != "" })
	second := app.New(app.Options{
		Config: config.Server{Addr: first.Addr(), ShutdownGrace: time.Second},
		Logger: slog.New(slog.DiscardHandler),
	}, app.Deps{API: http.NotFoundHandler(), Metrics: http.NotFoundHandler()})
	if err := second.Run(ctx); err == nil {
		t.Fatal("second Run on the same address succeeded")
	}
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
