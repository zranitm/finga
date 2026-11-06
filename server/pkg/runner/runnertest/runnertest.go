// Package runnertest runs a process in a test as its binary runs: on ports the kernel picks,
// which the test reads from the "listening" records the runner logs once it is ready.
package runnertest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	readyWithin = 10 * time.Second
	pollEvery   = 20 * time.Millisecond
)

// client closes every connection after its request: a spare one it dialed would hold a shutdown for 5 s.
var client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}

// Log is a logger that keeps the address each listener logged it listens on, and drops everything else.
type Log struct {
	mu     sync.Mutex
	addrs  map[string]string
	logged chan struct{} // closed and replaced on each listening record
}

// NewLog makes an empty Log.
func NewLog() *Log { return &Log{addrs: map[string]string{}, logged: make(chan struct{})} }

// Logger logs into l; hand it to the composition root under test.
func (l *Log) Logger() *slog.Logger { return slog.New(l.Handler()) }

// Handler is l as a slog.Handler, to combine with another.
func (l *Log) Handler() slog.Handler { return listening{l} }

func (l *Log) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	clear(l.addrs)
}

// bound is the addresses of names once all of them are logged, with a channel that closes on the next record.
func (l *Log) bound(names []string) (map[string]string, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	addrs := map[string]string{}
	for _, name := range names {
		addr, ok := l.addrs[name]
		if !ok {
			return nil, l.logged
		}
		addrs[name] = addr
	}
	return addrs, l.logged
}

type listening struct{ log *Log }

// Enabled takes every level: the runner logs "listening" at info.
func (listening) Enabled(context.Context, slog.Level) bool { return true }

// Handle keeps a "listening" record's address under its listener's name.
func (h listening) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "listening" {
		return nil
	}
	var name, addr string
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "listener":
			name = a.Value.String()
		case "addr":
			addr = a.Value.String()
		}
		return true
	})
	h.log.mu.Lock()
	defer h.log.mu.Unlock()
	h.log.addrs[name] = addr
	close(h.log.logged)
	h.log.logged = make(chan struct{})
	return nil
}

// WithAttrs ignores attrs: a "listening" record carries its own.
func (h listening) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup ignores the group, for the same reason.
func (h listening) WithGroup(string) slog.Handler { return h }

// Process is a run Start began.
type Process struct {
	addrs map[string]string
	stop  func() error
}

// Addr is where the named listener listens.
func (p *Process) Addr(listener string) string { return p.addrs[listener] }

// Stop cancels the run and returns its error; cleanup calls it if the test does not.
func (p *Process) Stop() error { return p.stop() }

// Start calls run, which logs into log, until each of listeners has logged where it listens;
// the test fails if run returns first.
func Start(t testing.TB, log *Log, run func(ctx context.Context) error, listeners ...string) *Process {
	t.Helper()
	log.reset()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	p := &Process{stop: sync.OnceValue(func() error { cancel(); return <-done })}
	t.Cleanup(func() { _ = p.Stop() })
	deadline := time.After(readyWithin)
	for {
		addrs, logged := log.bound(listeners)
		if addrs != nil {
			p.addrs = addrs
			return p
		}
		select {
		case <-logged:
		case err := <-done:
			done <- err
			t.Fatalf("stopped before %v listened: %v", listeners, err)
		case <-deadline:
			t.Fatalf("%v did not listen within %s", listeners, readyWithin)
		}
	}
}

// GetUntil fetches url until its body holds want, as a background task's work lands after Start; past readyWithin the test fails.
func GetUntil(t testing.TB, url, want string) string {
	t.Helper()
	deadline := time.Now().Add(readyWithin)
	for {
		_, body := Get(t, url)
		if strings.Contains(body, want) {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not show %s within %s:\n%s", url, want, readyWithin, body)
		}
		time.Sleep(pollEvery)
	}
}

// Get fetches url on a connection it closes, so a process started here shuts down within its grace.
func Get(t testing.TB, url string) (status int, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}
