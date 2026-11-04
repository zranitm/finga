package wsconn_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"go.uber.org/goleak"
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/panics"
	"finga/pkg/wsconn"
)

type recorder struct {
	mu       sync.Mutex
	opened   int
	closed   int
	closeErr error
	messages []wsconn.Message
	onOpen   func(wsconn.Conn)
	onMsg    func(wsconn.Conn, wsconn.Message)
	onClose  func()
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
	if r.onClose != nil {
		r.onClose()
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

// textProtocol is the subprotocol the tests' clients ask for: text frames, so a test can write them by hand.
const textProtocol = "finga.v1.json"

var textOnly = []wsconn.Subprotocol{{Name: textProtocol, Codec: serverCodec(wsconn.JSON)}}

func start(t *testing.T, rec *recorder, opts wsconn.Options) *server {
	t.Helper()
	if opts.Subprotocols == nil {
		opts.Subprotocols = textOnly
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
	opts := &websocket.DialOptions{Subprotocols: []string{textProtocol}}
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
			opts := &websocket.DialOptions{Subprotocols: []string{textProtocol}}
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
		pong := &clientv1.Pong{ClientTime: m.Data.(*clientv1.Ping).GetClientTime(), ServerTime: 9}
		if err := c.Send(wsconn.Message{ReqID: m.ReqID, Tick: 42, Data: pong}); err != nil {
			t.Errorf("send: %v", err)
		}
	}
	s := start(t, rec, wsconn.Options{})
	c := dial(t, s, "")
	ctx := context.Background()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"seq":3,"req":"r-1","ping":{"client_time":7}}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if canon(t, data) != canon(t, []byte(`{"req":"r-1","tick":42,"pong":{"client_time":"7","server_time":"9"}}`)) {
		t.Fatalf("echoed %s", data)
	}
	c.Close(websocket.StatusNormalClosure, "bye")
	if err := rec.waitClosed(t); err != nil {
		t.Fatalf("a normal closure reported %v", err)
	}
	if rec.opened != 1 || len(rec.messages) != 1 || rec.messages[0].Seq != 3 || !proto.Equal(rec.messages[0].Data.(proto.Message), &clientv1.Ping{ClientTime: 7}) {
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
		{"an unknown message type", []byte(`{"teleport":{}}`), wsconn.Options{}, websocket.StatusCode(wsconn.CloseBadMessage)},
		{"a malformed frame", []byte(`{"ping":`), wsconn.Options{}, websocket.StatusCode(wsconn.CloseBadMessage)},
		{"a frame over the read limit", []byte(`{"chat_send":{"text":"` + strings.Repeat("x", 2000) + `"}}`), wsconn.Options{ReadLimit: 1024}, websocket.StatusMessageTooBig},
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
	// OnClose can run before the sender sees Send's error, so wait on this channel instead.
	sendErr := make(chan error, 1)
	big := &clientv1.ChatMessage{Text: strings.Repeat("x", 8<<10)}
	rec.onOpen = func(c wsconn.Conn) {
		go func() {
			// The client never reads: socket buffers fill, then the queue, then Send fails.
			for range 4000 {
				if err := c.Send(wsconn.Message{Data: big}); err != nil {
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
	if err := c.Write(context.Background(), websocket.MessageText, []byte(`{"attack":{}}`)); err != nil {
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
}

// gatedCodec encodes only once its gate opens.
type gatedCodec struct {
	wsconn.Codec
	gate chan struct{}
}

func (g gatedCodec) Encode(m wsconn.Message) ([]byte, error) {
	<-g.gate
	return g.Codec.Encode(m)
}

// The world's stream sends every player's frames from one goroutine, so a Send must not wait on encoding.
func TestSendLeavesEncodingToTheWriter(t *testing.T) {
	checkLeaks(t)
	gate := make(chan struct{})
	rec := newRecorder()
	sent := make(chan error, 1)
	rec.onOpen = func(c wsconn.Conn) {
		go func() { sent <- c.Send(wsconn.Message{Data: &clientv1.Pong{ServerTime: 7}}) }()
	}
	s := start(t, rec, wsconn.Options{Subprotocols: []wsconn.Subprotocol{{Name: textProtocol, Codec: gatedCodec{Codec: serverCodec(wsconn.JSON), gate: gate}}}})
	c := dial(t, s, "")
	defer c.CloseNow() //nolint:errcheck // test teardown
	select {
	case err := <-sent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send waited for the codec")
	}
	close(gate)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil || !strings.Contains(string(data), `"server_time":"7"`) {
		t.Fatalf("read %s, %v", data, err)
	}
}

func TestAMessageTheCodecCannotEncodeClosesTheConnection(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	rec.onOpen = func(c wsconn.Conn) {
		_ = c.Send(wsconn.Message{Data: &clientv1.Ping{}}) // a client message: the server's envelope has no field for it
	}
	s := start(t, rec, wsconn.Options{})
	c := dial(t, s, "")
	defer c.CloseNow() //nolint:errcheck // test teardown
	if got := closeStatus(t, c); got != websocket.StatusInternalError {
		t.Fatalf("closed with %v, want 1011", got)
	}
	var ce *wsconn.CloseError
	if err := rec.waitClosed(t); !errors.As(err, &ce) || ce.Code != wsconn.CloseInternal || ce.Reason != "encode failed" {
		t.Fatalf("OnClose got %v", err)
	}
}

func TestSendAfterCloseFails(t *testing.T) {
	checkLeaks(t)
	rec := newRecorder()
	var after atomic.Value
	rec.onMsg = func(c wsconn.Conn, _ wsconn.Message) {
		c.Close(wsconn.CloseKicked, "x")
		c.Close(wsconn.CloseKicked, "again")
		after.Store(c.Send(wsconn.Message{Data: &clientv1.Pong{}}))
	}
	s := start(t, rec, wsconn.Options{})
	c := dial(t, s, "")
	if err := c.Write(context.Background(), websocket.MessageText, []byte(`{"attack":{}}`)); err != nil {
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
	// wsconn knows no Hello; the handler owns that timer.
	var _ wsconn.Handler = newRecorder()
}

// checkLeaks must be registered first so it runs after the server's cleanup.
func checkLeaks(t *testing.T) {
	t.Helper()
	opt := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, opt) })
}

func TestSubprotocols(t *testing.T) {
	checkLeaks(t)
	opts := wsconn.Options{Subprotocols: []wsconn.Subprotocol{
		{Name: "finga.v1.proto", Codec: serverCodec(wsconn.Binary)},
		{Name: "finga.v1.json", Codec: serverCodec(wsconn.JSON)},
	}}
	echo := func(c wsconn.Conn, m wsconn.Message) {
		if err := c.Send(wsconn.Message{ReqID: m.ReqID, Data: &clientv1.Pong{ClientTime: m.Data.(*clientv1.Ping).GetClientTime()}}); err != nil {
			t.Errorf("send: %v", err)
		}
	}
	tests := []struct {
		name       string
		ask        []string
		wantChosen string
		wantFrame  websocket.MessageType
	}{
		{"the client's only choice", []string{"finga.v1.json"}, "finga.v1.json", websocket.MessageText},
		{"the server's preference among the client's", []string{"finga.v1.json", "finga.v1.proto"}, "finga.v1.proto", websocket.MessageBinary},
		{"names match regardless of case", []string{"FINGA.v1.JSON"}, "FINGA.v1.JSON", websocket.MessageText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder()
			rec.onMsg = echo
			s := start(t, rec, opts)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, s.wsURL(), &websocket.DialOptions{Subprotocols: tt.ask})
			if err != nil {
				t.Fatal(err)
			}
			if c.Subprotocol() != tt.wantChosen {
				t.Fatalf("negotiated %q, want %q", c.Subprotocol(), tt.wantChosen)
			}
			codec := clientCodec(wsconn.JSON)
			if tt.wantFrame == websocket.MessageBinary {
				codec = clientCodec(wsconn.Binary)
			}
			b, err := codec.Encode(wsconn.Message{ReqID: "r-1", Data: &clientv1.Ping{ClientTime: 7}})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Write(ctx, tt.wantFrame, b); err != nil {
				t.Fatal(err)
			}
			typ, data, err := c.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got, err := codec.Decode(data)
			if typ != tt.wantFrame || err != nil || got.ReqID != "r-1" || got.Data.(*clientv1.Pong).GetClientTime() != 7 {
				t.Fatalf("frame %v %+v %v", typ, got, err)
			}
			c.Close(websocket.StatusNormalClosure, "")
			_ = rec.waitClosed(t)
		})
	}
	for _, ask := range [][]string{nil, {"finga.v0.json"}} {
		t.Run(fmt.Sprintf("a client asking for %v is closed before OnOpen", ask), func(t *testing.T) {
			rec := newRecorder()
			s := start(t, rec, opts)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, s.wsURL(), &websocket.DialOptions{Subprotocols: ask})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = c.Read(ctx)
			if websocket.CloseStatus(err) != websocket.StatusCode(wsconn.CloseNoSubprotocol) || !strings.Contains(err.Error(), "finga.v1.proto, finga.v1.json") {
				t.Fatalf("read %v, want close %d naming the subprotocols", err, wsconn.CloseNoSubprotocol)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.opened != 0 {
				t.Fatal("OnOpen ran")
			}
		})
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAPanicInACallbackClosesTheConnectionAndIsRecorded(t *testing.T) {
	checkLeaks(t)
	boom := func() { panic("boom") }
	tests := []struct {
		name  string
		setup func(*recorder)
		send  bool
	}{
		{"in OnOpen", func(r *recorder) { r.onOpen = func(wsconn.Conn) { boom() } }, false},
		{"in OnMessage", func(r *recorder) { r.onMsg = func(wsconn.Conn, wsconn.Message) { boom() } }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs lockedBuffer
			rec := newRecorder()
			tt.setup(rec)
			s := start(t, rec, wsconn.Options{Panics: panics.NewRecorder(slog.New(slog.NewJSONHandler(&logs, nil)), nil)})
			c := dial(t, s, "")
			if tt.send {
				if err := c.Write(context.Background(), websocket.MessageText, []byte(`{"ping":{"client_time":1}}`)); err != nil {
					t.Fatal(err)
				}
			}
			if got := closeStatus(t, c); got != websocket.StatusInternalError {
				t.Fatalf("close status %d, want 1011", got)
			}
			err := rec.waitClosed(t)
			if ce, ok := errors.AsType[*wsconn.CloseError](err); !ok || ce.Code != wsconn.CloseInternal {
				t.Fatalf("OnClose got %v, want CloseError %d", err, wsconn.CloseInternal)
			}
			for _, want := range []string{`"msg":"panic recovered"`, `"flow":"ws"`, `"panic":"boom"`, `"conn":`} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("log %q lacks %s", logs.String(), want)
				}
			}
		})
	}
}

func TestAPanicInOnCloseIsRecorded(t *testing.T) {
	checkLeaks(t)
	var logs lockedBuffer
	rec := newRecorder()
	rec.onClose = func() { panic("late boom") }
	s := start(t, rec, wsconn.Options{Panics: panics.NewRecorder(slog.New(slog.NewJSONHandler(&logs, nil)), nil)})
	c := dial(t, s, "")
	_ = c.Close(websocket.StatusNormalClosure, "bye")
	_ = rec.waitClosed(t)
	eventually(t, func() bool { return strings.Contains(logs.String(), "late boom") })
}

func TestWithoutARecorderThePanicReachesTheHTTPServerAfterOnClose(t *testing.T) {
	var serverLog lockedBuffer
	rec := newRecorder()
	rec.onOpen = func(wsconn.Conn) { panic("boom") }
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsconn.Serve(w, r, rec, wsconn.Options{Subprotocols: textOnly})
	}))
	ts.Config.ErrorLog = log.New(&serverLog, "", 0)
	ts.Start()
	t.Cleanup(ts.Close)
	c := dial(t, &server{ts: ts, rec: rec}, "")
	if got := closeStatus(t, c); got != websocket.StatusInternalError {
		t.Fatalf("close status %d, want 1011", got)
	}
	_ = rec.waitClosed(t)
	eventually(t, func() bool { return strings.Contains(serverLog.String(), "boom") })
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 5 s")
		}
		time.Sleep(time.Millisecond)
	}
}
