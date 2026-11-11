// Package model holds the social service's parties, invites, chat lines and errors.
package model

import (
	"errors"
	"time"

	"finga/internal/kernel"
)

// The party rules.
const (
	MaxMembers = 4
	InviteTTL  = 30 * time.Second
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
	// Connection is the one the member is online through; an Offline naming another is stale.
	Connection string
	// Gateway is the one the member is online through, where their deliveries go.
	Gateway string
	// JoinSeq is the world's sequence of the join behind Connection; an Online with a lower one is stale.
	JoinSeq uint64
	// JoinOrder is lowest for the longest-standing member, who leads when the leader goes.
	JoinOrder uint64
	// OfflineAt is when the member last went offline; zero while online.
	OfflineAt time.Time
	// ReportedAt is when a world last reported the member's map, level and HP.
	ReportedAt time.Time
}

// Party is one party: the leader and the members in join order.
type Party struct {
	ID      PartyID
	Leader  kernel.CharacterID
	Members []Member
}

// Invite is a pending invitation.
type Invite struct {
	ID       InviteID
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
	ErrTargetInParty = errors.New("party: the invitee is already in a party")
	ErrNoParty       = errors.New("party: not in a party")
	ErrNotMember     = errors.New("party: not a member")
	ErrNotHere       = errors.New("party: not online on the map")
	ErrAlreadyAsked  = errors.New("party: already invited")
	ErrInviteExpired = errors.New("party: invite expired")
	ErrSelf          = errors.New("party: invited self")
)
