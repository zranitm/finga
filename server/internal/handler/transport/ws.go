package transport

import (
	"errors"
	application "finga/internal/application/account"
	"log/slog"
	"net/http"
	"time"

	"finga/internal/shared/protocol"
	"finga/pkg/panics"
	"finga/pkg/wsconn"

	"go.opentelemetry.io/otel/trace"
)

// ServerOptions tune the WebSocket server.
type ServerOptions struct {
	// Origins are browser origins allowed besides the page's own.
	Origins      []string
	HelloTimeout time.Duration
	CommandQueue int
	Observer     Observer
	Logger       *slog.Logger
	// Panics records a panic in a message's chain or a connection's callbacks.
	Panics *panics.Recorder
	// Traces starts each command's trace.
	Traces trace.TracerProvider
}

// Server serves /ws.
type Server struct {
	gw           *application.Commands
	subprotocols []wsconn.Subprotocol
	opts         ServerOptions
	receive      *Receiver
	encoders     *Encoders
}

// Connection is one socket's receiving side and its session: the state wsconn hands back to each callback.
type Connection struct {
	in      *Inbox
	session *application.Connection
}

// NewServer serves gw's connections: each message is routed as routes say, and each push is written as encoders say; it fails if two routes take a message, or the Welcome has no encoder.
func NewServer(gw *application.Commands, opts ServerOptions, encoders *Encoders, routes ...Routes) (*Server, error) {
	if err := encoders.Complete(); err != nil {
		return nil, err
	}
	receive, err := NewReceiver(gw, ReceiveOptions{HelloTimeout: opts.HelloTimeout, CommandQueue: opts.CommandQueue, Logger: opts.Logger, Panics: opts.Panics, Traces: opts.Traces}, routes...)
	if err != nil {
		return nil, err
	}
	return &Server{gw: gw, subprotocols: protocol.Subprotocols(), opts: opts, encoders: encoders, receive: receive}, nil
}

// ServeHTTP upgrades the request and runs the connection until it closes.
func (h *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.gw.Draining() {
		http.Error(w, "server restarting", http.StatusServiceUnavailable)
		return
	}
	wsconn.Serve(w, r, h, wsconn.Options{Subprotocols: h.subprotocols, AllowedOrigins: h.opts.Origins, Panics: h.opts.Panics})
}

// OnOpen opens the connection's session, written to through its outbox, and starts its receiving side.
func (h *Server) OnOpen(c wsconn.Conn) Connection {
	out := NewOutbox(c, h.opts.Observer, h.opts.Logger, h.encoders)
	s := h.gw.Open(c.ID(), out)
	return Connection{in: h.receive.Open(out, s), session: s}
}

// OnMessage hands a message to the receiving side, on the read loop.
func (h *Server) OnMessage(_ wsconn.Conn, conn Connection, m wsconn.Message) {
	h.opts.Observer.MessageIn(m.Type)
	h.receive.Dispatch(conn.in, m)
}

// OnClose stops the connection's commands before ending its session; a push that failed to encode is logged as a bug.
func (h *Server) OnClose(c wsconn.Conn, conn Connection, err error) {
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
	h.gw.Closed(conn.session)
	h.opts.Logger.Info("ws closed", append([]any{"conn", c.ID()}, closeAttrs(err)...)...)
}

// closeAttrs says which side ended a connection, with its close frame's code and reason; a drop has no frame, so it logs the code browsers report.
func closeAttrs(err error) []any {
	if ce, ok := errors.AsType[*wsconn.CloseError](err); ok {
		attrs := []any{"end", "server", "code", ce.Code, "reason", ce.Reason}
		if ce.Err != nil {
			attrs = append(attrs, "err", ce.Err)
		}
		return attrs
	}
	if pc, ok := errors.AsType[*wsconn.PeerClose](err); ok {
		return []any{"end", "client", "code", pc.Code, "reason", pc.Reason}
	}
	return []any{"end", "dropped", "code", wsconn.CloseDropped, "err", err}
}
