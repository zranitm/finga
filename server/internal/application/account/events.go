package account

import (
	"context"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
)

// sessionsQueue bounds the session ends queued for a slow node before its subscription is closed.
const sessionsQueue = 1024

// Event is what a node hears on its sessions stream: a SessionRevoked or a Revoked, each after it commits.
//
//sumtype:decl
type Event interface{ sessionEvent() }

// SessionRevoked is one ended session: the server closes its connection.
type SessionRevoked struct {
	Session kernel.SessionID
	Reason  RevokeReason
}

// Revoked is every session of an account ended at once.
type Revoked struct {
	Account kernel.AccountID
	Reason  RevokeReason
}

func (SessionRevoked) sessionEvent() {}
func (Revoked) sessionEvent()        {}

// RevokeReason is why sessions ended, which the player is told.
type RevokeReason int

// The reasons a session ends.
const (
	RevokeUnspecified RevokeReason = iota // the zero: a reason the contract has no name for
	RevokeTokenReuse                      // a rotated-out token came back: someone else may hold it
	RevokeLogout
	RevokeNewLogin // the account logged in elsewhere
)

var revokeReasons = map[string]RevokeReason{
	ReasonTokenReuse: RevokeTokenReuse,
	ReasonLogout:     RevokeLogout,
	ReasonNewLogin:   RevokeNewLogin,
}

// Subscribe opens a node's sessions stream from now on; a newer one under the same ID closes it, as a full queue does.
func (m *Module) Subscribe(node string) *hub.Sub[Event] { return m.sessions.Subscribe(node) }

// notifier tells every node of ended sessions; it never blocks.
type notifier struct{ sessions *hub.Hub[Event] }

var _ Notifier = notifier{}

// SessionRevoked tells every node one session ended.
func (n notifier) SessionRevoked(_ context.Context, id kernel.SessionID, reason string) {
	n.sessions.Broadcast(SessionRevoked{Session: id, Reason: revokeReasons[reason]})
}

// AccountRevoked tells every node all of an account's sessions ended.
func (n notifier) AccountRevoked(_ context.Context, id kernel.AccountID, reason string) {
	n.sessions.Broadcast(Revoked{Account: id, Reason: revokeReasons[reason]})
}
