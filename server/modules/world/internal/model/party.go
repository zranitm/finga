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

// MemberUpdate is what a map saw of a party member, for the social service.
type MemberUpdate struct {
	ID    kernel.CharacterID
	MapID string
	Class kernel.Class
	Level int
	HP    int
	MaxHP int
}
