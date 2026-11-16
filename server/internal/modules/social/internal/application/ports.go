package application

import (
	"context"

	"finga/internal/kernel"
	"finga/modules/social/internal/model"
)

// PartyStore keeps who has been online, the parties and the pending invites.
type PartyStore interface {
	// InTx runs fn atomically: its writes land only if fn returns nil and no store call failed.
	InTx(ctx context.Context, fn func(tx PartyTx) error) error
}

// PartyTx reads and writes inside one InTx; a store failure is kept and returned by InTx.
type PartyTx interface {
	Member(id kernel.CharacterID) (model.Member, bool)
	PutMember(m model.Member)
	DeleteMember(id kernel.CharacterID)
	Members() []model.Member
	// PartyOf finds a party by any of its members.
	PartyOf(member kernel.CharacterID) (model.Party, bool)
	// PutParty saves p; a member it no longer lists no longer finds it.
	PutParty(p model.Party)
	DeleteParty(id model.PartyID)
	Parties() []model.Party
	Invite(id model.InviteID) (model.Invite, bool)
	PutInvite(inv model.Invite)
	DeleteInvite(id model.InviteID)
	Invites() []model.Invite
	// NextJoinOrder hands out increasing numbers for model.Member.JoinOrder.
	NextJoinOrder() uint64
}

// Notifier is told what members must hear, in the order the changes happened; it must not block.
type Notifier interface {
	// PartyChanged sends the new state to each member; p is nil for one who left.
	PartyChanged(members []kernel.CharacterID, p *model.Party)
	// Alone tells a member they are in no party, which is no change to any membership.
	Alone(id kernel.CharacterID)
	Invited(to kernel.CharacterID, inv model.Invite)
	// Reachable says where a member's deliveries go from now on: the gateway and connection they are online through.
	Reachable(id kernel.CharacterID, gateway, conn string)
	// Unreachable says a member's deliveries stop, if they still went to conn.
	Unreachable(id kernel.CharacterID, conn string)
}
