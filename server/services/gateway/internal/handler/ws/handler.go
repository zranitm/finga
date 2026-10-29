// Package ws is the gateway's WebSocket handler (Low-level design, pages 4
// and 11): it upgrades /ws, runs each connection's read and write loops on
// pkg/wsconn with the client protocol's codec, and hands the connection's
// open, decoded messages and close to the application's Gateway. While
// the gateway drains, it refuses new connections with 503.
package ws

import (
	"errors"
	"net/http"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

// Handler serves /ws.
type Handler struct {
	gw      *application.Gateway
	codec   wsconn.Codec
	origins []string
}

// New serves gw's connections; origins are the browser origins allowed
// besides the page's own (empty: same origin only).
func New(gw *application.Gateway, origins []string) *Handler {
	return &Handler{gw: gw, codec: protocol.NewCodec(), origins: origins}
}

// ServeHTTP upgrades the request and runs the connection until it closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.gw.Draining() {
		http.Error(w, "server restarting", http.StatusServiceUnavailable)
		return
	}
	wsconn.Serve(w, r, h, wsconn.Options{Codec: h.codec, AllowedOrigins: h.origins})
}

// OnOpen hands a new connection to the gateway.
func (h *Handler) OnOpen(c wsconn.Conn) { h.gw.Open(conn{c}) }

// OnMessage hands a decoded message to the gateway, on the read loop.
func (h *Handler) OnMessage(c wsconn.Conn, m wsconn.Message) {
	h.gw.Receive(conn{c}, model.Message{Type: m.Type, Seq: m.Seq, ReqID: m.ReqID, Tick: m.Tick, Data: m.Data})
}

// OnClose tells the gateway the connection is gone, and whether it fell
// too far behind.
func (h *Handler) OnClose(c wsconn.Conn, err error) {
	var ce *wsconn.CloseError
	slow := errors.As(err, &ce) && ce.Code == wsconn.CloseSlowConsumer
	h.gw.Closed(conn{c}, slow, closeReason(err))
}

func closeReason(err error) string {
	if err == nil {
		return "peer closed"
	}
	return err.Error()
}

// conn is a wsconn connection as the application's Conn.
type conn struct{ c wsconn.Conn }

// ID names the connection.
func (c conn) ID() string { return c.c.ID() }

// Send enqueues; a full queue closes the connection, and OnClose follows.
func (c conn) Send(m model.Message) {
	_ = c.c.Send(wsconn.Message{Type: m.Type, Seq: m.Seq, ReqID: m.ReqID, Tick: m.Tick, Data: m.Data})
}

// Close ends the connection with the WebSocket close code for code.
func (c conn) Close(code model.CloseCode, reason string) { c.c.Close(closeCodes[code], reason) }

// closeCodes are the gateway's reasons to close as WebSocket close codes.
var closeCodes = map[model.CloseCode]int{
	model.CloseKicked:   wsconn.CloseKicked,
	model.CloseInternal: wsconn.CloseInternal,
}
