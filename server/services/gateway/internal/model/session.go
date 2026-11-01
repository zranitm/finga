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
