package model

import "finga/internal/kernel"

// Player is the character a connection plays, as the gateway keeps it to
// introduce the player to its party.
type Player struct {
	ID    kernel.CharacterID
	Name  string
	Level int
	HP    int
	MaxHP int
}

// Presence is where a connected player is: the character and its map,
// which every world command carries.
type Presence struct {
	Character kernel.CharacterID
	MapID     string
}

// Actor is who acts on items: the account scopes idempotency keys, the
// character owns the items.
type Actor struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
}

// Limit is a per-message-type token bucket (page 4): a rate and a burst.
type Limit struct {
	PerSecond float64
	Burst     int
}

// RateLimits are page 4's per-account limits, by message type. ChatSend
// is missing on purpose: the social service limits it (1 per second, burst
// 3) and answers chat_rate_limited, which a silent drop here would hide.
func RateLimits() map[string]Limit {
	l := map[string]Limit{
		"Input":  {30, 30}, // one per tick is 20 a second; the headroom absorbs a burst after a stall
		"Attack": {10, 10},
		"PickUp": {10, 10},
	}
	for _, t := range []string{"PartyInvite", "PartyRespond", "PartyLeave", "PartyKick", "PartyPromote"} {
		l[t] = Limit{2, 2}
	}
	for _, t := range []string{"InventoryMove", "InventoryDiscard"} {
		l[t] = Limit{5, 5}
	}
	return l
}
