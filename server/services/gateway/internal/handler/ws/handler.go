// Package ws serves the gateway's WebSocket at /ws: the handshake and each connection's life.
// Package receive handles what a client sends, package push what it is sent.
package ws

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"finga/internal/protocol"
	"finga/pkg/panics"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/handler/ws/push"
	"finga/services/gateway/internal/handler/ws/receive"
)

// Observer counts the conversation's traffic.
type Observer interface {
	push.Observer
	MessageIn(msgType string)
	SlowConsumer()
}

// Options tune the handler.
type Options struct {
	// Origins are browser origins allowed besides the page's own.
	Origins      []string
	HelloTimeout time.Duration
	CommandQueue int
	Observer     Observer
	Logger       *slog.Logger
	// Panics records a panic in a message's chain or a connection's callbacks.
	Panics *panics.Recorder
}

// Handler serves /ws.
type Handler struct {
	gw           *send.Gateway
	subprotocols []wsconn.Subprotocol
	opts         Options
	receive      *receive.Handler
}

// Connection is one socket's receiving side and its session: the state wsconn hands back to each callback.
type Connection struct {
	in      *receive.Inbox
	session *session.Session
}

// New serves gw's connections.
func New(gw *send.Gateway, opts Options) *Handler {
	return &Handler{
		gw: gw, subprotocols: protocol.Subprotocols(), opts: opts,
		receive: receive.New(gw, receive.Options{HelloTimeout: opts.HelloTimeout, CommandQueue: opts.CommandQueue, Logger: opts.Logger, Panics: opts.Panics}),
	}
}

// ServeHTTP upgrades the request and runs the connection until it closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.gw.Draining() {
		http.Error(w, "server restarting", http.StatusServiceUnavailable)
		return
	}
	wsconn.Serve[Connection](w, r, h, wsconn.Options{Subprotocols: h.subprotocols, AllowedOrigins: h.opts.Origins, Panics: h.opts.Panics})
}

// OnOpen opens the connection's session, written to through its outbox, and starts its receiving side.
func (h *Handler) OnOpen(c wsconn.Conn) Connection {
	out := push.NewOutbox(c, h.opts.Observer)
	s := h.gw.Open(c.ID(), out)
	return Connection{in: h.receive.Open(out, s), session: s}
}

// OnMessage hands a message to the receiving side, on the read loop.
func (h *Handler) OnMessage(_ wsconn.Conn, conn Connection, m wsconn.Message) {
	h.opts.Observer.MessageIn(m.Type)
	h.receive.Dispatch(conn.in, m)
}

// OnClose stops the connection's commands before ending its session; a push that could not be encoded
// is a bug, so it is logged as one.
func (h *Handler) OnClose(c wsconn.Conn, conn Connection, err error) {
	if conn.in == nil {
		return // OnOpen panicked
	}
	conn.in.Stop()
	if errors.Is(err, wsconn.ErrEncode) {
		h.opts.Logger.Error("a message could not be encoded", "conn", c.ID(), "err", err)
	}
	if ce, ok := errors.AsType[*wsconn.CloseError](err); ok && ce.Code == wsconn.CloseSlowConsumer {
		h.opts.Observer.SlowConsumer()
	}
	h.gw.Closed(conn.session, closeReason(err))
}

func closeReason(err error) string {
	if err == nil {
		return "peer closed"
	}
	return err.Error()
}
