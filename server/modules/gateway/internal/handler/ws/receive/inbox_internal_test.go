package receive

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/modules/gateway/internal/application/send"
	"finga/modules/gateway/internal/application/session"
	"finga/modules/gateway/internal/handler/ws/push"
	"finga/pkg/panics"
	"finga/pkg/wsconn"
)

// closedConn is a connection that keeps the code it was closed with.
type closedConn struct {
	mu   sync.Mutex
	code int
}

func (*closedConn) ID() string                { return "c-1" }
func (*closedConn) Send(wsconn.Message) error { return nil }
func (c *closedConn) Close(code int, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.code == 0 {
		c.code = code
	}
}

func (c *closedConn) closedWith() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code
}

// panicTally counts recorded panics by flow.
type panicTally struct {
	mu sync.Mutex
	n  map[string]int
}

func (t *panicTally) Inc(flow string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.n[flow]++
}

// A panic on the command goroutine outside any chain is recorded and closes that player's socket, so they reconnect; the process goes on.
func TestAPanicOnTheCommandGoroutineClosesTheSocket(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	count := &panicTally{n: map[string]int{}}
	reg := session.NewRegistry(session.Deps{Logger: log, Traces: noop.NewTracerProvider()}, session.Options{})
	h := New(send.New(send.Deps{Logger: log, Registry: reg}, send.Options{}), Options{Logger: log, Traces: noop.NewTracerProvider(), Panics: panics.NewRecorder(log, count), HelloTimeout: time.Hour, CommandQueue: 4})
	conn := &closedConn{}
	in := h.Open(push.NewOutbox(conn, nil, push.NewFrames()), reg.Open("c-1", nil))
	in.queue <- func() { panic("bad state") }
	select {
	case <-in.drained:
	case <-time.After(5 * time.Second):
		t.Fatal("the command goroutine never ended")
	}
	in.Stop()
	recorded := func() int {
		count.mu.Lock()
		defer count.mu.Unlock()
		return count.n[panics.FlowWS]
	}
	for deadline := time.Now().Add(5 * time.Second); recorded() == 0 && time.Now().Before(deadline); time.Sleep(time.Millisecond) { // recorded once the goroutine unwinds past drained
	}
	if conn.closedWith() != wsconn.CloseInternal || recorded() != 1 {
		t.Fatalf("closed with %d, %d panics recorded; want %d and 1", conn.closedWith(), recorded(), wsconn.CloseInternal)
	}
}
