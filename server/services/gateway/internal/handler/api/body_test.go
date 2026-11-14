package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/services/account"
	"finga/services/gateway/internal/handler/api"
	"finga/services/gateway/internal/model"
)

// pipeListener hands the server one end of each net.Pipe, so a real http.Server runs in a synctest bubble.
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }

// serve runs h on a real server; the returned dial opens a connection to it.
func serve(t *testing.T, h http.Handler) (dial func() net.Conn) {
	ln := newPipeListener()
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return func() net.Conn {
		client, server := net.Pipe()
		ln.conns <- server
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
}

const signUp = `{"username":"alice","password":"correct horse"}`

// A client that stops sending its body is answered when the body's time runs out, not held for as long as it likes.
func TestABodyThatStallsTimesOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness()
		conn := serve(t, h.handler)()
		start := time.Now()
		go func() {
			_, _ = fmt.Fprintf(conn, "POST /api/accounts HTTP/1.1\r\nHost: finga\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(signUp), signUp[:10])
		}()
		res, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body api.ErrorResponse
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusRequestTimeout || body.Code != model.BodyTimeout.Code {
			t.Fatalf("status %d code %d, want 408 %s", res.StatusCode, body.Code, model.BodyTimeout.Name)
		}
		if waited := time.Since(start); waited != 10*time.Second {
			t.Errorf("answered after %v, want 10s", waited)
		}
		if _, err := io.Copy(io.Discard, conn); err != nil || !res.Close {
			t.Errorf("the connection is kept for another request (%v)", err)
		}
	})
}

// Once the body is in, net/http lifts the deadline for its own read past it, so a slow use case is not cut off.
func TestTheBodyDeadlineDoesNotEndTheRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness()
		h.accounts.register = func(ctx context.Context, c account.Credentials) (account.Account, error) {
			select {
			case <-time.After(time.Minute):
			case <-ctx.Done():
				return account.Account{}, ctx.Err()
			}
			return account.Account{ID: accountID, Username: c.Username, CreatedAt: t0}, nil
		}
		conn := serve(t, h.handler)()
		go func() {
			_, _ = fmt.Fprintf(conn, "POST /api/accounts HTTP/1.1\r\nHost: finga\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(signUp), signUp)
		}()
		res, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(res.Body)
			t.Fatalf("status %d body %s, want 201", res.StatusCode, b)
		}
	})
}
