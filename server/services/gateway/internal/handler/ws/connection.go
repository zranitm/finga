package ws

import (
	"context"
	"strings"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

// connection is one socket's handler state, and its session's application.Outbox.
type connection struct {
	h          *Handler
	ws         wsconn.Conn
	session    *application.Session
	helloTimer *time.Timer

	// Owned by the read loop.
	helloSeen bool
	limiters  map[string]*rate.Limiter

	// One goroutine runs queued chains in order; OnClose cancels ctx and waits for drained.
	queue   chan func()
	ctx     context.Context
	cancel  context.CancelFunc
	drained chan struct{}
	joined  bool // owned by the command goroutine
}

var _ application.Outbox = (*connection)(nil)

func newConnection(h *Handler, ws wsconn.Conn) *connection {
	c := &connection{h: h, ws: ws, limiters: map[string]*rate.Limiter{}, queue: make(chan func(), h.opts.CommandQueue), drained: make(chan struct{})}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.helloTimer = time.AfterFunc(h.opts.HelloTimeout, func() { ws.Close(wsconn.CloseKicked, "no hello") })
	return c
}

func (c *connection) commands() {
	defer close(c.drained)
	for {
		select {
		case <-c.ctx.Done():
			return
		case rest := <-c.queue:
			if c.ctx.Err() != nil {
				return
			}
			rest()
		}
	}
}

// allow takes a token from the connection's bucket for msgType; limits are per connection.
func (c *connection) allow(msgType string, l limit) bool {
	b, ok := c.limiters[msgType]
	if !ok {
		b = rate.NewLimiter(rate.Limit(l.perSecond), l.burst)
		c.limiters[msgType] = b
	}
	return b.AllowN(c.h.opts.Clock(), 1)
}

// Welcome answers a Hello that joined.
func (c *connection) Welcome(w model.Welcome) {
	c.send(wsconn.Message{Data: &clientv1.Welcome{
		Token: w.Token, Character: w.Character, Inventory: w.Inventory, Party: w.Party, MapId: w.MapID, ServerTime: w.ServerTime.UnixMilli(),
	}})
}

// Deliver sends a push as its message.
func (c *connection) Deliver(d model.Delivery) {
	c.send(wsconn.Message{Tick: d.Tick, Data: message(d.Push)})
}

// End kicks with the protocol's reason where it has one; any other end closes as internal, and the client reconnects.
func (c *connection) End(reason model.EndReason) {
	switch reason {
	case model.EndLoggedInElsewhere:
		c.kick(clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE)
	case model.EndSessionExpired:
		c.kick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
	case model.EndServerRestart:
		c.kick(clientv1.KickReason_KICK_REASON_SERVER_RESTART)
	default:
		c.ws.Close(wsconn.CloseInternal, string(reason))
	}
}

// kick sends Kicked with the reason, then closes with the reason's short name, like logged_in_elsewhere.
func (c *connection) kick(reason clientv1.KickReason) {
	c.send(wsconn.Message{Data: &clientv1.Kicked{Reason: reason}})
	c.ws.Close(wsconn.CloseKicked, strings.ToLower(strings.TrimPrefix(reason.String(), "KICK_REASON_")))
}

func (c *connection) reply(reqID string, data proto.Message) {
	c.send(wsconn.Message{ReqID: reqID, Data: data})
}

// send enqueues a message, typed by its data's name when unset; a full queue closes the connection.
func (c *connection) send(m wsconn.Message) {
	if data, ok := m.Data.(proto.Message); ok && m.Type == "" {
		m.Type = string(data.ProtoReflect().Descriptor().Name())
	}
	c.h.opts.Observer.MessageOut(m.Type)
	_ = c.ws.Send(m)
}
