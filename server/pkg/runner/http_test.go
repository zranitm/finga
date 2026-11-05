package runner_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/runner"
)

// oneConn is a listener that accepts conn, then nothing until it closes.
type oneConn struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newOneConn(conn net.Conn) *oneConn {
	l := &oneConn{conns: make(chan net.Conn, 1), done: make(chan struct{})}
	l.conns <- conn
	return l
}

func (l *oneConn) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *oneConn) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *oneConn) Addr() net.Addr { return &net.TCPAddr{} }

// A kept-alive connection that sends no other request is closed after two minutes, not held forever.
func TestAnIdleConnectionIsClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		srv := runner.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
		go func() { _ = srv.Serve(newOneConn(server)) }()
		defer func() { _ = srv.Shutdown(t.Context()) }()

		go func() { _, _ = fmt.Fprint(client, "GET / HTTP/1.1\r\nHost: finga\r\n\r\n") }()
		r := bufio.NewReader(client)
		res, err := http.ReadResponse(r, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.Close {
			t.Fatal("the server closed after one request")
		}
		start := time.Now()
		if _, err := r.ReadByte(); err != io.EOF {
			t.Fatalf("read %v, want the connection closed", err)
		}
		if idle := time.Since(start); idle != 2*time.Minute {
			t.Errorf("closed after %v idle, want 2m", idle)
		}
	})
}
