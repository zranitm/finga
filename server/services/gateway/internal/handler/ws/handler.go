// Package ws is the gateway's WebSocket handler (Low-level design, pages 4
// and 11), the counterpart of the REST handler in handler/api: it upgrades
// /ws, runs each connection's read and write loops on pkg/wsconn with the
// client protocol's codec, and routes every message a client sends through
// its chain (routes.go) to the Gateway's use cases, turning each message
// into a use case's arguments and its result into the reply. It owns what
// only the conversation has: the handshake, the per-message rate limits,
// each connection's command queue, and request IDs. It also implements the
// application's Conn for each connection, putting what the use cases tell
// the player into protocol messages. While the gateway drains, it refuses
// new connections with 503.
package ws

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/pkg/wsrouter"
	"finga/services/gateway/internal/application"
)

const (
	defaultHelloTimeout = 5 * time.Second
	defaultCommandQueue = 64
	// commandTimeout bounds a command's service calls.
	commandTimeout = 5 * time.Second
)

// Observer counts the conversation's traffic; internal/telemetry
// implements it.
type Observer interface {
	MessageIn(msgType string)
	MessageOut(msgType string)
	SlowConsumer()
}

// Options tune the handler. Zero values take the defaults.
type Options struct {
	// Origins are the browser origins allowed besides the page's own
	// (empty: same origin only).
	Origins []string
	// HelloTimeout is how long a connection has to send Hello; 5 s by
	// default.
	HelloTimeout time.Duration
	// CommandQueue bounds each connection's command queue; 64 by default.
	CommandQueue int
	Clock        func() time.Time
	Observer     Observer
	Logger       *slog.Logger
}

// Handler serves /ws.
type Handler struct {
	gw     *application.Gateway
	codec  wsconn.Codec
	opts   Options
	router *wsrouter.Router[*client] // every message a client may send (routes.go)

	mu      sync.Mutex
	clients map[string]*client
}

// New serves gw's connections.
func New(gw *application.Gateway, opts Options) *Handler {
	if opts.HelloTimeout <= 0 {
		opts.HelloTimeout = defaultHelloTimeout
	}
	if opts.CommandQueue <= 0 {
		opts.CommandQueue = defaultCommandQueue
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Observer == nil {
		opts.Observer = noopObserver{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	h := &Handler{gw: gw, codec: protocol.NewCodec(), opts: opts, clients: map[string]*client{}}
	h.router = h.routes()
	return h
}

// ServeHTTP upgrades the request and runs the connection until it closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.gw.Draining() {
		http.Error(w, "server restarting", http.StatusServiceUnavailable)
		return
	}
	wsconn.Serve(w, r, h, wsconn.Options{Codec: h.codec, AllowedOrigins: h.opts.Origins})
}

// OnOpen starts a connection: its command goroutine, the Hello timer, and
// its session with the gateway.
func (h *Handler) OnOpen(c wsconn.Conn) {
	cl := newClient(h, c)
	h.mu.Lock()
	h.clients[c.ID()] = cl
	h.mu.Unlock()
	go cl.commands()
	cl.session = h.gw.Open(cl)
}

// OnMessage routes a decoded message, on the read loop: the checks that
// run here, then the connection's command queue for the rest, so the read
// loop never waits on a service.
func (h *Handler) OnMessage(c wsconn.Conn, m wsconn.Message) {
	h.opts.Observer.MessageIn(m.Type)
	if cl := h.client(c.ID()); cl != nil {
		h.router.Dispatch(cl, wsrouter.Message{Type: m.Type, ReqID: m.ReqID, Seq: m.Seq, Data: m.Data})
	}
}

// OnClose ends a connection: it cancels the command in flight and waits
// for the command goroutine, then ends the session, so no command runs
// once the player has left.
func (h *Handler) OnClose(c wsconn.Conn, err error) {
	h.mu.Lock()
	cl := h.clients[c.ID()]
	delete(h.clients, c.ID())
	h.mu.Unlock()
	if cl == nil {
		return
	}
	cl.helloTimer.Stop()
	cl.cancel()
	<-cl.drained
	if ce, ok := errors.AsType[*wsconn.CloseError](err); ok && ce.Code == wsconn.CloseSlowConsumer {
		h.opts.Observer.SlowConsumer()
	}
	h.gw.Closed(cl.session, closeReason(err))
}

func (h *Handler) client(id string) *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clients[id]
}

func closeReason(err error) string {
	if err == nil {
		return "peer closed"
	}
	return err.Error()
}

// noopObserver is the Observer when none is given.
type noopObserver struct{}

// MessageIn does nothing.
func (noopObserver) MessageIn(string) {}

// MessageOut does nothing.
func (noopObserver) MessageOut(string) {}

// SlowConsumer does nothing.
func (noopObserver) SlowConsumer() {}
