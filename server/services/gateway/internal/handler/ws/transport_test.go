package ws_test

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
)

// pipes is a listener whose connections are net.Pipe ends: a gateway on it runs inside a synctest bubble,
// where a read on a real socket would not count as blocked.
type pipes struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (l *pipes) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipes) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *pipes) Addr() net.Addr { return &net.TCPAddr{} }

// dial hands the server one end of a new pipe and returns the other.
func (l *pipes) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// serve runs h on an in-memory listener until the test ends; the client reaches it at any ws:// URL.
func serve(t *testing.T, h http.Handler) *http.Client {
	t.Helper()
	ln := &pipes{conns: make(chan net.Conn), done: make(chan struct{})}
	srv := &http.Server{Handler: h}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ln) }()
	tr := &http.Transport{DialContext: ln.dial}
	t.Cleanup(func() {
		tr.CloseIdleConnections()
		_ = srv.Close()
		<-done
	})
	return &http.Client{Transport: tr}
}
