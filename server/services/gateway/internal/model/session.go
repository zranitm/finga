package model

import (
	"time"

	"finga/internal/kernel"
	"finga/internal/protocol"
)

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

// Welcome is what a player who joined starts from: the token to use from
// now on (every Hello rotates it), the character, its inventory and its
// party (nil: none), the map, and the server's clock.
type Welcome struct {
	Token      string
	Character  protocol.CharacterView
	Inventory  []protocol.ItemView
	Party      *protocol.PartyView
	MapID      string
	ServerTime time.Time
}

// Keys are the keys a player holds, which an input carries.
type Keys struct {
	Left, Right, Up, Down, Jump bool
}

// ItemMove moves an item to a slot of a tab. The idempotency key makes a
// retried command safe.
type ItemMove struct {
	IdempotencyKey string
	Item           kernel.ItemID
	Tab            string
	Slot           int
}

// ItemDiscard destroys some or all of a stack.
type ItemDiscard struct {
	IdempotencyKey string
	Item           kernel.ItemID
	Quantity       int
}
