package world

import "finga/internal/shared/kernel"

// PartyEvent is what the social module's party stream tells the world, in order.
//
//sumtype:decl
type PartyEvent interface{ partyEvent() }

// PartiesSubscribed starts a subscription: the memberships that follow are the whole table.
type PartiesSubscribed struct{}

// PartiesLost is a subscription that broke; another follows.
type PartiesLost struct{ Err error }

// Membership gives each of Characters its party; none means they have no party.
type Membership struct {
	Characters []kernel.CharacterID
	Party      Party
}

// Party is a party as the world knows it: its members in join order, under an ID that stays as they come and go.
type Party struct {
	ID      string
	Members []kernel.CharacterID
}

func (PartiesSubscribed) partyEvent() {}
func (PartiesLost) partyEvent()       {}
func (Membership) partyEvent()        {}

// PartySpots is where the player's party members on the map stand that the player doesn't see, once a second on a map cut by cells; empty once all are in sight again.
type PartySpots struct {
	Tick  uint64
	Spots []PartySpot
}

// PartySpot is a party member where they stand.
type PartySpot struct {
	ID   kernel.CharacterID
	X, Z float64
}

func (PartySpots) event() {}

// MemberUpdate is what a map saw of a party member, for the social module.
type MemberUpdate struct {
	ID    kernel.CharacterID
	MapID string
	Class kernel.Class
	Level int
	HP    int
	MaxHP int
}
