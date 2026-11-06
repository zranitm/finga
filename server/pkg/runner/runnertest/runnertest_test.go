package runnertest_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"finga/pkg/runner"
	"finga/pkg/runner/runnertest"
)

// A task's work shows on an endpoint a little after the process is up; GetUntil waits for it.
func TestGetUntilWaitsForTheBodyToShowIt(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "calls %d", calls.Add(1))
	}))
	defer srv.Close()
	if body := runnertest.GetUntil(t, srv.URL, "calls 3"); body != "calls 3" {
		t.Fatalf("body %q", body)
	}
}

// Start reads the address from the record the runner logs, so the test listens where the runner bound.
func TestStartFindsTheAddressTheRunnerLogs(t *testing.T) {
	log := runnertest.NewLog()
	var r *runner.Runner
	ops := runner.HTTP(runner.Probes(func() bool { return r.Ready() }, http.NotFoundHandler()))
	r = runner.New(runner.Options{Logger: log.Logger(), ShutdownGrace: time.Second}, []runner.Listener{{Name: "ops", Addr: "127.0.0.1:0", Server: ops}}, nil)
	p := runnertest.Start(t, log, r.Run, "ops")
	if status, body := runnertest.Get(t, "http://"+p.Addr("ops")+"/readyz"); status != http.StatusOK {
		t.Fatalf("readyz %d %s", status, body)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}
