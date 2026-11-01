// Package ws serves the gateway's WebSocket at /ws, routing client messages to the Gateway's use cases.
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
	commandTimeout      = 5 * time.Second
)

// Observer counts the conversation's traffic.
type Observer interface {
	MessageIn(msgType string)
	MessageOut(msgType string)
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
}

// Handler serves /ws.
type Handler struct {
	gw           *application.Gateway
	subprotocols []wsconn.Subprotocol
	opts         Options
	router       *wsrouter.Router[*client]

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
	h := &Handler{gw: gw, subprotocols: protocol.Subprotocols(), opts: opts, clients: map[string]*client{}}
	h.router = h.routes()
	return h
}

// ServeHTTP upgrades the request and runs the connection until it closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.gw.Draining() {
		http.Error(w, "server restarting", http.StatusServiceUnavailable)
		return
	}
	wsconn.Serve(w, r, h, wsconn.Options{Subprotocols: h.subprotocols, AllowedOrigins: h.opts.Origins})
}

// OnOpen starts a connection's command goroutine and its gateway session.
func (h *Handler) OnOpen(c wsconn.Conn) {
	cl := newClient(h, c)
	h.mu.Lock()
	h.clients[c.ID()] = cl
	h.mu.Unlock()
	go cl.commands()
	cl.session = h.gw.Open(cl)
}

// OnMessage routes a message on the read loop; anything that calls a service is queued.
func (h *Handler) OnMessage(c wsconn.Conn, m wsconn.Message) {
	h.opts.Observer.MessageIn(m.Type)
	if cl := h.client(c.ID()); cl != nil {
		h.router.Dispatch(cl, wsrouter.Message{Type: m.Type, ReqID: m.ReqID, Seq: m.Seq, Data: m.Data})
	}
}

// OnClose cancels and drains the connection's commands before ending its session.
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

type noopObserver struct{}

// MessageIn does nothing.
func (noopObserver) MessageIn(string) {}

// MessageOut does nothing.
func (noopObserver) MessageOut(string) {}

// SlowConsumer does nothing.
func (noopObserver) SlowConsumer() {}
