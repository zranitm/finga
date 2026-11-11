package receive

import (
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"golang.org/x/time/rate"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
)

// A protocol message without a route fails here rather than answering NOT_IMPLEMENTED in a game.
func TestEveryClientMessageHasARoute(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	reg := session.NewRegistry(session.Deps{Logger: log, Traces: noop.NewTracerProvider()}, session.Options{})
	h := New(send.New(send.Deps{Logger: log, Registry: reg}, send.Options{}), Options{Logger: log, Traces: noop.NewTracerProvider()})
	var unrouted []string
	h.router.NoRoute(func(c *msgContext) { unrouted = append(unrouted, c.Message.Type) })
	// Past Hello, with no payload and a queue nobody drains, so no handler runs.
	body := (&clientv1.ClientMessage{}).ProtoReflect().Descriptor().Oneofs().ByName("body").Fields()
	in := &Inbox{helloSeen: true, session: reg.Open("c-1", nil), limiters: map[string]*rate.Limiter{}, queue: make(chan func(), body.Len())}
	for i := range body.Len() {
		h.Dispatch(in, wsconn.Message{Type: string(body.Get(i).Message().Name())})
	}
	if len(unrouted) > 0 {
		t.Fatalf("no route for %v", unrouted)
	}
}
