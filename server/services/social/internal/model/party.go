// Package model holds the social service's parties, invites, chat lines and errors.
package model

import (
	"errors"
	"time"

	"finga/internal/kernel"
)

// Defaults.
const (
	DefaultMaxMembers = 4
	DefaultInviteTTL  = 30 * time.Second
)

// Member is a party member as the others see them, fed by the gateway and the world.
type Member struct {
	ID     kernel.CharacterID
	Name   string
	Level  int
	HP     int
	MaxHP  int
	MapID  string
	Online bool
	// JoinOrder is lowest for the longest-standing member, who leads when the leader goes.
	JoinOrder uint64
}

// Party is one party: the leader and the members in join order.
type Party struct {
	ID      string
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

// The party errors; the handler sends each as a contract reason.
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
