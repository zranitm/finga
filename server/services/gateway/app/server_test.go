package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"finga/pkg/runner/runnertest"
	"finga/services/gateway/app"
	"finga/services/gateway/config"
)

func newServer(addr string, deps app.Deps) *app.Server {
	deps.Metrics = http.NotFoundHandler()
	return app.New(app.Options{
		HTTP:          config.HTTP{Addr: addr},
		ShutdownGrace: 2 * time.Second,
		Logger:        slog.New(slog.DiscardHandler),
	}, deps)
}

// Readiness checks no dependency, so a shared outage cannot pull every instance out of rotation at once.
func TestReadinessFollowsTheProcess(t *testing.T) {
	addr := runnertest.Addr(t)
	stop := runnertest.Start(t, "http://"+addr+"/readyz", newServer(addr, app.Deps{}).Run)

	for _, path := range []string{"/readyz", "/healthz"} {
		t.Run(path+" answers on the real listener", func(t *testing.T) {
			status, body := runnertest.Get(t, "http://"+addr+path)
			if status != http.StatusOK || (path == "/readyz" && !strings.Contains(body, "ready")) {
				t.Fatalf("status %d body %s", status, body)
			}
		})
	}

	if err := stop(); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if res, err := http.Get("http://" + addr + "/healthz"); err == nil {
		res.Body.Close()
		t.Fatal("the listener still answers after shutdown")
	}
}

// The drain runs after readiness turns 503, so the load balancer stops sending new connections first.
func TestTheDrainRunsAfterReadinessTurnsOff(t *testing.T) {
	addr := runnertest.Addr(t)
	drained := make(chan int, 1)
	srv := newServer(addr, app.Deps{Drain: func(context.Context) {
		res, err := http.Get("http://" + addr + "/readyz") // the listener still serves while draining
		if err != nil {
			drained <- 0
			return
		}
		res.Body.Close()
		drained <- res.StatusCode
	}})
	stop := runnertest.Start(t, "http://"+addr+"/readyz", srv.Run)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if code := <-drained; code != http.StatusServiceUnavailable {
		t.Fatalf("readyz answered %d while draining, want 503", code)
	}
}
