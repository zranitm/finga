package model

import "finga/internal/kernel"

// PartyEvent is what the social service's party stream tells the world, in order.
//
//sumtype:decl
type PartyEvent interface{ partyEvent() }

// PartiesSubscribed starts a subscription: the memberships that follow are the whole table.
type PartiesSubscribed struct{}

// PartiesLost is a subscription that broke; another follows.
type PartiesLost struct{ Err error }

// Membership gives each of Characters its party's members; none means they have no party.
type Membership struct {
	Characters []kernel.CharacterID
	Members    []kernel.CharacterID
}

func (PartiesSubscribed) partyEvent() {}
func (PartiesLost) partyEvent()       {}
func (Membership) partyEvent()        {}

// MemberUpdate is what a map saw of a party member, for the social service.
type MemberUpdate struct {
	ID    kernel.CharacterID
	MapID string
	Level int
	HP    int
	MaxHP int
}
