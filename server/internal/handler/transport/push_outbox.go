package transport

import (
	application "finga/internal/application/account"
	"finga/internal/shared/failure"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	model "finga/internal/model/account"
	"finga/pkg/wsconn"
)

// Observer counts the conversation's traffic.
type Observer interface {
	MessageIn(msgType string)
	MessageOut(msgType string)
	// WelcomeSent hears each Welcome's encoded size, kind login or rejoin.
	WelcomeSent(kind string, bytes int)
	SlowConsumer()
	// Unencoded hears a push no handler registered an encoder for, which is a bug; the push is dropped.
	Unencoded(pushType string)
}

// Outbox is one connection's writing side: the application.Outbox the gateway reaches the player through.
type Outbox struct {
	ws       wsconn.Conn
	obs      Observer
	log      *slog.Logger
	encoders *Encoders

	mu     sync.Mutex // each send and the end, one at a time, so nothing is queued after the end's Kicked
	ending bool       // the first End, Kick or Close wins; nothing is sent after it
}

var _ application.Outbox = (*Outbox)(nil)

// NewOutbox writes to ws and counts every message on obs; encoders turn what the application pushes into messages.
func NewOutbox(ws wsconn.Conn, obs Observer, log *slog.Logger, encoders *Encoders) *Outbox {
	return &Outbox{ws: ws, obs: obs, log: log, encoders: encoders}
}

// ID names the connection.
func (o *Outbox) ID() string { return o.ws.ID() }

// Welcome answers a Hello that joined, or follows a rejoin, which has no token.
func (o *Outbox) Welcome(w model.Welcome) {
	msg := o.encoders.welcomeOf(w)
	kind := "login"
	if w.Token == "" {
		kind = "rejoin"
	}
	o.obs.WelcomeSent(kind, proto.Size(msg))
	o.send(wsconn.Message{Data: msg, Written: w.Written})
}

// Deliver sends a push as its message.
func (o *Outbox) Deliver(d model.Delivery) {
	body, ok := o.encoders.body(d.Push)
	if !ok {
		name := reflect.TypeOf(d.Push).Name()
		o.obs.Unencoded(name)
		o.log.Error("a push has no encoder", "conn", o.ws.ID(), "push", name)
		return
	}
	o.send(wsconn.Message{Tick: d.Tick, Data: body})
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

// RefusalResponse is a command's refusal as the client reads it.
func RefusalResponse(p failure.Public) *clientv1.Response {
	return &clientv1.Response{Ok: false, Code: int32(p.Code)}
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
