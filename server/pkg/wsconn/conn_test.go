package wsconn_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"go.uber.org/goleak"

	"finga/pkg/wsconn"
)

// recorder is a Handler that remembers what happened.
type recorder struct {
	mu       sync.Mutex
	opened   int
	closed   int
	closeErr error
	messages []wsconn.Message
	onOpen   func(wsconn.Conn)
	onMsg    func(wsconn.Conn, wsconn.Message)
	done     chan struct{} // closed on OnClose
}

func newRecorder() *recorder { return &recorder{done: make(chan struct{})} }

func (r *recorder) OnOpen(c wsconn.Conn) {
	r.mu.Lock()
	r.opened++
	r.mu.Unlock()
	if r.onOpen != nil {
		r.onOpen(c)
	}
}

func (r *recorder) OnMessage(c wsconn.Conn, m wsconn.Message) {
	r.mu.Lock()
	r.messages = append(r.messages, m)
	r.mu.Unlock()
	if r.onMsg != nil {
		r.onMsg(c, m)
	}
}

func (r *recorder) OnClose(_ wsconn.Conn, err error) {
	r.mu.Lock()
	r.closed++
	r.closeErr = err
	n := r.closed
	r.mu.Unlock()
	if n == 1 {
		close(r.done)
	}
}

func (r *recorder) waitClosed(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnClose was never called")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed != 1 {
		t.Fatalf("OnClose called %d times", r.closed)
	}
	return r.closeErr
}

type server struct {
	ts  *httptest.Server
	rec *recorder
}

func start(t *testing.T, rec *recorder, opts wsconn.Options) *server {
	t.Helper()
	if opts.Codec == nil {
		opts.Codec = newCodec()
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsconn.Serve(w, r, rec, opts)
	}))
	t.Cleanup(ts.Close)
	return &server{ts: ts, rec: rec}
}

func (s *server) wsURL() string { return "ws" + strings.TrimPrefix(s.ts.URL, "http") }

func dial(t *testing.T, s *server, origin string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts := &websocket.DialOptions{}
	if origin != "" {
		opts.HTTPHeader = http.Header{"Origin": []string{origin}}
	}
	c, _, err := websocket.Dial(ctx, s.wsURL(), opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func closeStatus(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err == nil {
			continue // drain whatever the server sent before closing
		}
		return websocket.CloseStatus(err)
	}
}

func TestOriginCheck(t *testing.T) {
	checkLeaks(t)
	tests := []struct {
		name    string
		allowed []string
		origin  string
		wantOK  bool
	}{
		{"no Origin header is fine (not a browser)", nil, "", true},
		{"with no allowed origins, a foreign origin is refused", nil, "http://localhost:5173", false},
		{"an allowed origin is accepted", []string{"http://localhost:5173"}, "http://localhost:5173", true},
		{"another origin is refused", []string{"http://localhost:5173"}, "http://evil.example", false},
		{"the scheme matters", []string{"https://play.example"}, "http://play.example", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder()
			s := start(t, rec, wsconn.Options{AllowedOrigins: tt.allowed})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			opts := &websocket.DialOptions{}
			if tt.origin != "" {
				opts.HTTPHeader = http.Header{"Origin": []string{tt.origin}}
			}
			c, res, err := websocket.Dial(ctx, s.wsURL(), opts)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("dial: %v", err)
				}
				c.Close(websocket.StatusNormalClosure, "")
				_ = rec.waitClosed(t)
				return
			}
			if err == nil {
				_ = c.CloseNow()
				t.Fatal("dial succeeded")
			}
			if res == nil || res.StatusCode != http.StatusForbidden {
				t.Fatalf("response %v, want 403", res)
			}
			if rec.opened != 0 {
				t.Fatal("OnOpen ran for a refused upgrade")
			}
		})
	}
}

func TestEcho(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	rec.onMsg = func(c wsconn.Conn, m wsconn.Message) {
		m.Tick = 42
		if err := c.Send(m); err != nil {
			t.Errorf("send: %v", err)
		}
	}
	s := start(t, rec, wsconn.Options{})
	c := dial(t, s, "")
	ctx := context.Background()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"t":"Ping","seq":3,"req":"r-1","d":{"client_time":7}}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if canon(t, data) != canon(t, []byte(`{"t":"Ping","seq":3,"req":"r-1","tick":42,"d":{"client_time":7}}`)) {
		t.Fatalf("echoed %s", data)
	}
	c.Close(websocket.StatusNormalClosure, "bye")
	if err := rec.waitClosed(t); err != nil {
		t.Fatalf("a normal closure reported %v", err)
	}
	if rec.opened != 1 || len(rec.messages) != 1 || rec.messages[0].Data != (ping{ClientTime: 7}) {
		t.Fatalf("recorder %+v", rec)
	}
}

func TestBadFramesCloseTheConnection(t *testing.T) {
	checkLeaks(t)
	tests := []struct {
		name     string
		frame    []byte
		opts     wsconn.Options
		wantCode websocket.StatusCode
	}{
		{"an unknown message type", []byte(`{"t":"Teleport","d":{}}`), wsconn.Options{}, websocket.StatusCode(wsconn.CloseBadMessage)},
		{"a malformed frame", []byte(`{"t":`), wsconn.Options{}, websocket.StatusCode(wsconn.CloseBadMessage)},
		{"a frame over the read limit", []byte(`{"t":"Ping","d":{"client_time":` + strings.Repeat("1", 2000) + `}}`), wsconn.Options{ReadLimit: 1024}, websocket.StatusMessageTooBig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder()
			s := start(t, rec, tt.opts)
			c := dial(t, s, "")
			if err := c.Write(context.Background(), websocket.MessageText, tt.frame); err != nil {
				t.Fatal(err)
			}
			if got := closeStatus(t, c); got != tt.wantCode {
				t.Fatalf("close status %d, want %d", got, tt.wantCode)
			}
			err := rec.waitClosed(t)
			var ce *wsconn.CloseError
			if tt.wantCode == websocket.StatusCode(wsconn.CloseBadMessage) && (!errors.As(err, &ce) || ce.Code != wsconn.CloseBadMessage) {
				t.Fatalf("OnClose got %v, want CloseError %d", err, wsconn.CloseBadMessage)
			}
			if len(rec.messages) != 0 {
				t.Fatal("a bad frame reached OnMessage")
			}
		})
	}
}

func TestSlowConsumerIsDropped(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	// The sender reports Send's first error, or nil if every send fitted.
	// OnClose can run before the sender sees its error, so the test waits
	// for this rather than reading a shared value at close time.
	sendErr := make(chan error, 1)
	big := blob{Data: strings.Repeat("x", 8<<10)}
	rec.onOpen = func(c wsconn.Conn) {
		go func() {
			// A client that never reads: the socket buffers fill, then the
			// queue, then Send fails and the connection is closed.
			for range 4000 {
				if err := c.Send(wsconn.Message{Type: "Blob", Data: big}); err != nil {
					sendErr <- err
					return
				}
			}
			sendErr <- nil
		}()
	}
	s := start(t, rec, wsconn.Options{SendQueue: 8})
	c := dial(t, s, "")
	defer c.CloseNow() //nolint:errcheck // test teardown
	err := rec.waitClosed(t)
	var ce *wsconn.CloseError
	if !errors.As(err, &ce) || ce.Code != wsconn.CloseSlowConsumer {
		t.Fatalf("OnClose got %v, want CloseError %d", err, wsconn.CloseSlowConsumer)
	}
	select {
	case got := <-sendErr:
		if !errors.Is(got, wsconn.ErrQueueFull) {
			t.Fatalf("Send returned %v, want ErrQueueFull", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the sender never finished")
	}
}

func TestMissedPongClosesTheConnection(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	s := start(t, rec, wsconn.Options{PingInterval: 30 * time.Millisecond, PongTimeout: 50 * time.Millisecond})
	c := dial(t, s, "")
	defer c.CloseNow() //nolint:errcheck // test teardown
	// The client never reads, so it never answers pings.
	err := rec.waitClosed(t)
	var ce *wsconn.CloseError
	if !errors.As(err, &ce) || ce.Code != wsconn.CloseTimeout {
		t.Fatalf("OnClose got %v, want CloseError %d", err, wsconn.CloseTimeout)
	}
}

func TestPongKeepsAResponsiveClient(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	s := start(t, rec, wsconn.Options{PingInterval: 20 * time.Millisecond, PongTimeout: 200 * time.Millisecond})
	c := dial(t, s, "")
	ctx, cancel := context.WithCancel(context.Background())
	readCtx := c.CloseRead(ctx) // reading answers pings
	time.Sleep(150 * time.Millisecond)
	select {
	case <-rec.done:
		t.Fatalf("closed although the client answered pings: %v", rec.closeErr)
	default:
	}
	cancel()
	<-readCtx.Done()
	_ = c.CloseNow()
	_ = rec.waitClosed(t)
}

func TestServerSideClose(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	rec.onMsg = func(c wsconn.Conn, _ wsconn.Message) { c.Close(wsconn.CloseKicked, "logged in elsewhere") }
	s := start(t, rec, wsconn.Options{})
	c := dial(t, s, "")
	if err := c.Write(context.Background(), websocket.MessageText, []byte(`{"t":"Attack"}`)); err != nil {
		t.Fatal(err)
	}
	if got := closeStatus(t, c); got != websocket.StatusCode(wsconn.CloseKicked) {
		t.Fatalf("close status %d", got)
	}
	err := rec.waitClosed(t)
	var ce *wsconn.CloseError
	if !errors.As(err, &ce) || ce.Code != wsconn.CloseKicked || ce.Reason != "logged in elsewhere" {
		t.Fatalf("OnClose got %v", err)
	}
	t.Run("Send after Close fails and Close twice is harmless", func(t *testing.T) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		// The conn handed to OnClose is the same one; use the last message's conn via a fresh server instead.
	})
}

func TestSendAfterCloseFails(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	var after atomic.Value
	rec.onMsg = func(c wsconn.Conn, _ wsconn.Message) {
		c.Close(wsconn.CloseKicked, "x")
		c.Close(wsconn.CloseKicked, "again") // harmless
		after.Store(c.Send(wsconn.Message{Type: "Attack"}))
	}
	s := start(t, rec, wsconn.Options{})
	c := dial(t, s, "")
	if err := c.Write(context.Background(), websocket.MessageText, []byte(`{"t":"Attack"}`)); err != nil {
		t.Fatal(err)
	}
	closeStatus(t, c)
	_ = rec.waitClosed(t)
	if got, _ := after.Load().(error); !errors.Is(got, wsconn.ErrClosed) {
		t.Fatalf("Send after Close = %v, want ErrClosed", got)
	}
}

func TestConnIDsAreDistinct(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	var mu sync.Mutex
	ids := map[string]bool{}
	rec.onOpen = func(c wsconn.Conn) { mu.Lock(); ids[c.ID()] = true; mu.Unlock() }
	s := start(t, rec, wsconn.Options{})
	var conns []*websocket.Conn
	for range 3 {
		conns = append(conns, dial(t, s, ""))
	}
	for _, c := range conns {
		c.Close(websocket.StatusNormalClosure, "")
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 3 {
		t.Fatalf("%d distinct IDs for 3 connections", len(ids))
	}
	for id := range ids {
		if id == "" {
			t.Fatal("empty ID")
		}
	}
}

func TestNoHelloTimeoutIsTheHandlersJob(t *testing.T) {
	// wsconn has no idea what Hello is; this documents that the gateway
	// (step 10) owns that timer. Nothing to assert beyond the API shape.
	var _ wsconn.Handler = newRecorder()
}

// checkLeaks verifies no goroutine outlives the test. It registers first,
// so it runs last, after the httptest server's own cleanup has closed it.
func checkLeaks(t *testing.T) {
	t.Helper()
	opt := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, opt) })
}
