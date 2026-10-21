package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"finga/internal/config"
	"finga/internal/server"
)

type pinger struct {
	mu  sync.Mutex
	err error
}

func (p *pinger) Ping(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *pinger) set(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func newServer(t *testing.T, api http.Handler, db *pinger) *server.Server {
	t.Helper()
	return server.New(server.Options{
		Config: config.Server{Addr: "127.0.0.1:0", ShutdownGrace: 2 * time.Second},
		Logger: slog.New(slog.DiscardHandler),
	}, server.Deps{API: api, DB: db, Metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("# metrics\n")) })})
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestProbes(t *testing.T) {
	db := &pinger{}
	srv := newServer(t, http.NotFoundHandler(), db)
	h := srv.Handler()

	t.Run("healthz is always 200", func(t *testing.T) {
		if rec := get(t, h, "/healthz"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("readyz is 503 before the server runs", func(t *testing.T) {
		if rec := get(t, h, "/readyz"); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", rec.Code)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	waitFor(t, func() bool { return srv.Addr() != "" })

	t.Run("readyz is 200 once running with the database answering", func(t *testing.T) {
		if rec := get(t, h, "/readyz"); rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("readyz is 503 when the database does not answer", func(t *testing.T) {
		db.set(errors.New("down"))
		defer db.set(nil)
		rec := get(t, h, "/readyz")
		if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "down") {
			t.Fatalf("status %d body %s (database error text must not leak)", rec.Code, rec.Body.String())
		}
	})
	t.Run("metrics is served", func(t *testing.T) {
		if rec := get(t, h, "/metrics"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "# metrics") {
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
	srv := newServer(t, api, &pinger{})
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
	first := newServer(t, http.NotFoundHandler(), &pinger{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = first.Run(ctx) }()
	waitFor(t, func() bool { return first.Addr() != "" })
	second := server.New(server.Options{
		Config: config.Server{Addr: first.Addr(), ShutdownGrace: time.Second},
		Logger: slog.New(slog.DiscardHandler),
	}, server.Deps{API: http.NotFoundHandler(), DB: &pinger{}, Metrics: http.NotFoundHandler()})
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
