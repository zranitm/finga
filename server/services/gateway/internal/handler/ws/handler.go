// Package ws serves the gateway's WebSocket at /ws: the handshake and each connection's life.
// Package receive handles what a client sends, package push what it is sent.
package ws

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"
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

// Options tune the handler. Zero values take the defaults.
type Options struct {
	// Origins are browser origins allowed besides the page's own.
	Origins      []string
	HelloTimeout time.Duration
	CommandQueue int
	Clock        func() time.Time
	Observer     Observer
	Logger       *slog.Logger
	// Panics records a panic in a message's chain or a connection's callbacks; nil logs it to Logger.
	Panics *panics.Recorder
}

// Handler serves /ws.
type Handler struct {
	gw           *send.Gateway
	subprotocols []wsconn.Subprotocol
	opts         Options
	receive      *receive.Handler

	mu          sync.Mutex
	connections map[string]connection
}

// connection is one socket's receiving side and its session.
type connection struct {
	in      *receive.Inbox
	session *session.Session
}

// New serves gw's connections.
func New(gw *send.Gateway, opts Options) *Handler {
	if opts.Observer == nil {
		opts.Observer = noopObserver{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Panics == nil {
		opts.Panics = panics.NewRecorder(opts.Logger, nil)
	}
	return &Handler{
		gw: gw, subprotocols: protocol.Subprotocols(), opts: opts, connections: map[string]connection{},
		receive: receive.New(gw, receive.Options{HelloTimeout: opts.HelloTimeout, CommandQueue: opts.CommandQueue, Clock: opts.Clock, Logger: opts.Logger, Panics: opts.Panics}),
	}
}

// ServeHTTP upgrades the request and runs the connection until it closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.gw.Draining() {
		http.Error(w, "server restarting", http.StatusServiceUnavailable)
		return
	}
	wsconn.Serve(w, r, h, wsconn.Options{Subprotocols: h.subprotocols, AllowedOrigins: h.opts.Origins, Panics: h.opts.Panics})
}

// OnOpen opens the connection's session, written to through its outbox, and starts its receiving side.
func (h *Handler) OnOpen(c wsconn.Conn) {
	out := push.NewOutbox(c, h.opts.Observer)
	s := h.gw.Open(c.ID(), out)
	conn := connection{in: h.receive.Open(out, s), session: s}
	h.mu.Lock()
	h.connections[c.ID()] = conn
	h.mu.Unlock()
}

// OnMessage hands a message to the receiving side, on the read loop.
func (h *Handler) OnMessage(c wsconn.Conn, m wsconn.Message) {
	h.opts.Observer.MessageIn(m.Type)
	h.mu.Lock()
	conn, ok := h.connections[c.ID()]
	h.mu.Unlock()
	if ok {
		h.receive.Dispatch(conn.in, m)
	}
}

// OnClose stops the connection's commands before ending its session.
func (h *Handler) OnClose(c wsconn.Conn, err error) {
	h.mu.Lock()
	conn, ok := h.connections[c.ID()]
	delete(h.connections, c.ID())
	h.mu.Unlock()
	if !ok {
		return
	}
	conn.in.Stop()
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

type noopObserver struct{}

// MessageIn does nothing.
func (noopObserver) MessageIn(string) {}

// MessageOut does nothing.
func (noopObserver) MessageOut(string) {}

// SlowConsumer does nothing.
func (noopObserver) SlowConsumer() {}
