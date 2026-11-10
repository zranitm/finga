// Package push writes everything one connection is sent: Welcome, pushes, ends, and the replies receive chooses.
package push

import (
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// Observer counts the messages sent.
type Observer interface {
	MessageOut(msgType string)
}

// Outbox is one connection's writing side: the session.Outbox the gateway reaches the player through.
type Outbox struct {
	ws     wsconn.Conn
	obs    Observer
	frames *Frames

	mu     sync.Mutex // each send and the end, one at a time, so nothing is queued after the end's Kicked
	ending bool       // the first End, Kick or Close wins; nothing is sent after it
}

var _ session.Outbox = (*Outbox)(nil)

// NewOutbox writes to ws and counts every message on obs; frames shares each frame's encoding with the other outboxes.
func NewOutbox(ws wsconn.Conn, obs Observer, frames *Frames) *Outbox {
	return &Outbox{ws: ws, obs: obs, frames: frames}
}

// ID names the connection.
func (o *Outbox) ID() string { return o.ws.ID() }

// Welcome answers a Hello that joined.
func (o *Outbox) Welcome(w model.Welcome) {
	o.send(wsconn.Message{Data: &clientv1.Welcome{
		Token: w.Token, Character: characterView(w.Character), Inventory: itemViews(w.Inventory), Equipment: equipment(w.Equipment), InventoryRevision: w.InventoryRevision, MapId: w.MapID,
		ServerTime: w.ServerTime.UnixMilli(), TickRateHz: uint32(w.TickRateHz), Quests: questStates(w.Quests),
	}})
}

// Deliver sends a push as its message.
func (o *Outbox) Deliver(d model.Delivery) {
	o.send(wsconn.Message{Tick: d.Tick, Data: o.body(d.Push)})
}

// End kicks with the protocol's reason where it has one; any other end closes as internal, and the client reconnects.
func (o *Outbox) End(reason model.EndReason) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.end() {
		return
	}
	switch reason {
	case model.EndLoggedInElsewhere:
		o.kick(clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE)
	case model.EndSessionExpired:
		o.kick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
	case model.EndServerRestart:
		o.kick(clientv1.KickReason_KICK_REASON_SERVER_RESTART)
	default:
		o.ws.Close(wsconn.CloseInternal, string(reason))
	}
}

// kick sends Kicked, then closes with the reason's short name. Called with mu held.
func (o *Outbox) kick(reason clientv1.KickReason) {
	o.write(wsconn.Message{Data: &clientv1.Kicked{Reason: reason}})
	o.ws.Close(wsconn.CloseKicked, strings.ToLower(strings.TrimPrefix(reason.String(), "KICK_REASON_")))
}

// end marks the connection ended and reports whether it was the first end. Called with mu held.
func (o *Outbox) end() (first bool) {
	first = !o.ending
	o.ending = true
	return first
}

// Reply answers a client message; reqID is empty when nobody waits for the answer.
func (o *Outbox) Reply(reqID string, data proto.Message) {
	o.send(wsconn.Message{ReqID: reqID, Data: data})
}

// Kick sends Kicked with the reason, then closes with the reason's short name, like logged_in_elsewhere.
func (o *Outbox) Kick(reason clientv1.KickReason) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.end() {
		o.kick(reason)
	}
}

// Close closes the connection without a message.
func (o *Outbox) Close(code int, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.end() {
		o.ws.Close(code, reason)
	}
}

// ErrorResponse is a failure as the client reads it, for a command or a push.
func ErrorResponse(p model.Public) *clientv1.Response {
	return &clientv1.Response{Ok: false, Code: p.Code, Message: p.Message}
}

// send enqueues a message, unless the connection ended.
func (o *Outbox) send(m wsconn.Message) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.ending {
		o.write(m)
	}
}

// write enqueues a message, typed by its data's name when unset; a full queue closes the connection. Called with mu held.
func (o *Outbox) write(m wsconn.Message) {
	if m.Type == "" {
		switch data := m.Data.(type) {
		case proto.Message:
			m.Type = string(data.ProtoReflect().Descriptor().Name())
		case wsconn.Spliced:
			m.Type = string(data.Own.ProtoReflect().Descriptor().Name())
		}
	}
	o.obs.MessageOut(m.Type)
	_ = o.ws.Send(m) // a full queue or a closed connection ends in OnClose, which reports it
}
