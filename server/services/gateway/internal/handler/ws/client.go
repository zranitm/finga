package ws

import (
	"context"
	"reflect"
	"time"

	"golang.org/x/time/rate"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

// client is one connection as the handler keeps it: the socket, the
// gateway's session for it, and the conversation's own state. It is the
// application's Conn for the connection.
type client struct {
	h          *Handler
	conn       wsconn.Conn
	session    *application.Session
	helloTimer *time.Timer

	// The read loop's own: the handshake, and a bucket per message type.
	helloSeen bool
	limiters  map[string]*rate.Limiter

	// The command queue: the read loop puts the rest of each message's
	// chain on it, and one goroutine runs them in order with ctx, which
	// OnClose cancels, closing drained when it stops.
	queue   chan func()
	ctx     context.Context
	cancel  context.CancelFunc
	drained chan struct{}
	joined  bool // the command goroutine's own: this connection's Hello joined
}

var _ application.Conn = (*client)(nil)

func newClient(h *Handler, c wsconn.Conn) *client {
	cl := &client{h: h, conn: c, limiters: map[string]*rate.Limiter{}, queue: make(chan func(), h.opts.CommandQueue), drained: make(chan struct{})}
	cl.ctx, cl.cancel = context.WithCancel(context.Background())
	// A connection that stays silent is closed.
	cl.helloTimer = time.AfterFunc(h.opts.HelloTimeout, func() { c.Close(wsconn.CloseKicked, "no hello") })
	return cl
}

// commands runs the connection's queue in order, one message's chain at a
// time, until the connection closes.
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

// allow takes one token from the connection's bucket for a message type (a
// golang.org/x/time/rate limiter, created full), so limits are per
// connection, and so per account.
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
	inventory := w.Inventory
	if inventory == nil {
		inventory = []protocol.ItemView{} // the client reads an array
	}
	c.send(wsconn.Message{Data: protocol.Welcome{
		Token: w.Token, Character: w.Character, Inventory: inventory, Party: w.Party, MapID: w.MapID, ServerTime: w.ServerTime.UnixMilli(),
	}})
}

// MapChange tells the player a rejoin put them on another map.
func (c *client) MapChange(mapID string, x, y float64) {
	c.send(wsconn.Message{Data: protocol.MapChange{MapID: mapID, X: x, Y: y}})
}

// PartyGone tells the player they have no party.
func (c *client) PartyGone() { c.send(wsconn.Message{Data: protocol.PartyState{}}) }

// Deliver forwards a service's push.
func (c *client) Deliver(d model.Delivery) {
	c.send(wsconn.Message{Type: d.Type, Tick: d.Tick, Data: d.Data})
}

// Kick sends Kicked with the reason, then closes.
func (c *client) Kick(reason protocol.KickReason) {
	c.send(wsconn.Message{Data: protocol.Kicked{Reason: reason}})
	c.conn.Close(wsconn.CloseKicked, string(reason))
}

// Close ends the connection as internal: the client reconnects.
func (c *client) Close(reason string) { c.conn.Close(wsconn.CloseInternal, reason) }

// reply answers a message: a Response or a follow-up.
func (c *client) reply(reqID string, data any) {
	c.send(wsconn.Message{ReqID: reqID, Data: data})
}

// send enqueues a message; a full queue closes the connection, and
// OnClose follows.
func (c *client) send(m wsconn.Message) {
	if m.Type == "" {
		m.Type = typeName(m.Data)
	}
	c.h.opts.Observer.MessageOut(m.Type)
	_ = c.conn.Send(m)
}

func typeName(v any) string {
	if v == nil {
		return ""
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}
