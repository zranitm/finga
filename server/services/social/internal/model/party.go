// Package model is the social service's model (Low-level design, page 11):
// parties, their members and invites, with page 5's limits; chat channels,
// lines and the rule that cleans them; and the errors the use cases
// return. It needs nothing but data.
package model

import (
	"errors"
	"time"

	"finga/internal/kernel"
)

// Defaults (page 5).
const (
	DefaultMaxMembers = 4
	DefaultInviteTTL  = 30 * time.Second
)

// Member is a party member as the others see them. The service learns
// these from the gateway (online, offline) and the world (map, level, HP).
type Member struct {
	ID     kernel.CharacterID
	Name   string
	Level  int
	HP     int
	MaxHP  int
	MapID  string
	Online bool
	// JoinOrder is set by the service when the member joins a party: the
	// longest-standing member has the lowest, and leads when the leader goes.
	JoinOrder uint64
}

// Party is one party: the leader and the members in join order.
type Party struct {
	Leader  kernel.CharacterID
	Members []Member
}

// Invite is a pending invitation.
type Invite struct {
	ID       string
	From     kernel.CharacterID
	FromName string
	To       kernel.CharacterID
	Expires  time.Time
}

// The party errors the commands return. Each names one failure; the service's
// handler sends it as a reason, and the gateway gives it a code and the
// words a player sees (its errmap).
var (
	ErrNotLeader     = errors.New("party: not the leader")
	ErrFull          = errors.New("party: full")
	ErrInParty       = errors.New("party: already in a party")
	ErrNoParty       = errors.New("party: not in a party")
	ErrNotMember     = errors.New("party: not a member")
	ErrNotHere       = errors.New("party: invitee not on the map")
	ErrAlreadyAsked  = errors.New("party: already invited")
	ErrInviteExpired = errors.New("party: invite expired")
	ErrSelf          = errors.New("party: invited self")
)
