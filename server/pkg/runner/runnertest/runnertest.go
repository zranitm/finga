// Package runnertest runs a process in a test as its binary would: on loopback ports, until it answers /readyz.
package runnertest

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

const (
	readyWithin = 10 * time.Second
	pollEvery   = 5 * time.Millisecond
)

// Addr is a free loopback address to listen on: the kernel picks the port and the probe releases it.
func Addr(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// Start runs run until readyURL answers 200, failing the test if run returns first.
// stop cancels run and returns its error; cleanup calls it if the test does not.
func Start(t testing.TB, readyURL string, run func(ctx context.Context) error) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	stop = sync.OnceValue(func() error { cancel(); return <-done })
	t.Cleanup(func() { _ = stop() })
	for deadline := time.Now().Add(readyWithin); !ready(readyURL); {
		select {
		case err := <-done:
			done <- err
			t.Fatalf("stopped before %s answered: %v", readyURL, err)
		case <-time.After(pollEvery):
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s not ready within %s", readyURL, readyWithin)
		}
	}
	return stop
}

func ready(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK
}
