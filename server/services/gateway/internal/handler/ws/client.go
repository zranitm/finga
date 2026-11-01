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

// client is one connection's handler state, and its application.Conn.
type client struct {
	h          *Handler
	conn       wsconn.Conn
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

var _ application.Conn = (*client)(nil)

func newClient(h *Handler, c wsconn.Conn) *client {
	cl := &client{h: h, conn: c, limiters: map[string]*rate.Limiter{}, queue: make(chan func(), h.opts.CommandQueue), drained: make(chan struct{})}
	cl.ctx, cl.cancel = context.WithCancel(context.Background())
	cl.helloTimer = time.AfterFunc(h.opts.HelloTimeout, func() { c.Close(wsconn.CloseKicked, "no hello") })
	return cl
}

func (c *client) commands() {
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
func (c *client) allow(msgType string, l limit) bool {
	b, ok := c.limiters[msgType]
	if !ok {
		b = rate.NewLimiter(rate.Limit(l.perSecond), l.burst)
		c.limiters[msgType] = b
	}
	return b.AllowN(c.h.opts.Clock(), 1)
}

// ID names the connection.
func (c *client) ID() string { return c.conn.ID() }

// Welcome answers a Hello that joined.
func (c *client) Welcome(w model.Welcome) {
	c.send(wsconn.Message{Data: &clientv1.Welcome{
		Token: w.Token, Character: w.Character, Inventory: w.Inventory, Party: w.Party, MapId: w.MapID, ServerTime: w.ServerTime.UnixMilli(),
	}})
}

// MapChange tells the player a rejoin put them on another map.
func (c *client) MapChange(mapID string, x, y float64) {
	c.send(wsconn.Message{Data: &clientv1.MapChange{MapId: mapID, X: x, Y: y}})
}

// PartyGone tells the player they have no party.
func (c *client) PartyGone() { c.send(wsconn.Message{Data: &clientv1.PartyState{}}) }

// Deliver forwards a service's push.
func (c *client) Deliver(d model.Delivery) {
	c.send(wsconn.Message{Type: d.Type, Tick: d.Tick, Data: d.Data})
}

// Kick sends Kicked with the reason, then closes with the reason's short name, like logged_in_elsewhere.
func (c *client) Kick(reason clientv1.KickReason) {
	c.send(wsconn.Message{Data: &clientv1.Kicked{Reason: reason}})
	c.conn.Close(wsconn.CloseKicked, strings.ToLower(strings.TrimPrefix(reason.String(), "KICK_REASON_")))
}

// Close ends the connection as internal: the client reconnects.
func (c *client) Close(reason string) { c.conn.Close(wsconn.CloseInternal, reason) }

func (c *client) reply(reqID string, data proto.Message) {
	c.send(wsconn.Message{ReqID: reqID, Data: data})
}

// send enqueues a message, typed by its data's name when unset; a full queue closes the connection.
func (c *client) send(m wsconn.Message) {
	if data, ok := m.Data.(proto.Message); ok && m.Type == "" {
		m.Type = string(data.ProtoReflect().Descriptor().Name())
	}
	c.h.opts.Observer.MessageOut(m.Type)
	_ = c.conn.Send(m)
}
