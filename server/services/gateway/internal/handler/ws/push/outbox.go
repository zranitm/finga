// Package push writes everything one connection is sent: the application's Welcome, pushes and ends,
// and the replies the receiving side chooses.
package push

import (
	"strings"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

// Observer counts the messages sent.
type Observer interface {
	MessageOut(msgType string)
}

// Outbox is one connection's writing side, and its session's application.Outbox.
type Outbox struct {
	ws  wsconn.Conn
	obs Observer
}

var _ application.Outbox = (*Outbox)(nil)

// NewOutbox writes to ws and counts every message on obs.
func NewOutbox(ws wsconn.Conn, obs Observer) *Outbox {
	return &Outbox{ws: ws, obs: obs}
}

// ID names the connection.
func (o *Outbox) ID() string { return o.ws.ID() }

// Welcome answers a Hello that joined.
func (o *Outbox) Welcome(w model.Welcome) {
	o.send(wsconn.Message{Data: &clientv1.Welcome{
		Token: w.Token, Character: w.Character, Inventory: w.Inventory, Party: w.Party, MapId: w.MapID, ServerTime: w.ServerTime.UnixMilli(),
	}})
}

// Deliver sends a push as its message.
func (o *Outbox) Deliver(d model.Delivery) {
	o.send(wsconn.Message{Tick: d.Tick, Data: message(d.Push)})
}

// End kicks with the protocol's reason where it has one; any other end closes as internal, and the client reconnects.
func (o *Outbox) End(reason model.EndReason) {
	switch reason {
	case model.EndLoggedInElsewhere:
		o.Kick(clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE)
	case model.EndSessionExpired:
		o.Kick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
	case model.EndServerRestart:
		o.Kick(clientv1.KickReason_KICK_REASON_SERVER_RESTART)
	default:
		o.ws.Close(wsconn.CloseInternal, string(reason))
	}
}

// Reply answers a client message; reqID is empty when nobody waits for the answer.
func (o *Outbox) Reply(reqID string, data proto.Message) {
	o.send(wsconn.Message{ReqID: reqID, Data: data})
}

// Kick sends Kicked with the reason, then closes with the reason's short name, like logged_in_elsewhere.
func (o *Outbox) Kick(reason clientv1.KickReason) {
	o.send(wsconn.Message{Data: &clientv1.Kicked{Reason: reason}})
	o.ws.Close(wsconn.CloseKicked, strings.ToLower(strings.TrimPrefix(reason.String(), "KICK_REASON_")))
}

// Close closes the connection without a message.
func (o *Outbox) Close(code int, reason string) { o.ws.Close(code, reason) }

// ErrorResponse is a failure as the client reads it, for a command or a push.
func ErrorResponse(p model.Public) *clientv1.Response {
	return &clientv1.Response{Ok: false, Code: p.Code, Message: p.Message}
}

// send enqueues a message, typed by its data's name when unset; a full queue closes the connection.
func (o *Outbox) send(m wsconn.Message) {
	if data, ok := m.Data.(proto.Message); ok && m.Type == "" {
		m.Type = string(data.ProtoReflect().Descriptor().Name())
	}
	o.obs.MessageOut(m.Type)
	_ = o.ws.Send(m)
}
