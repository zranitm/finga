package model

import (
	"time"

	"finga/internal/kernel"
	"finga/internal/protocol"
)

// Player is the character a connection plays, as its party sees it.
type Player struct {
	ID    kernel.CharacterID
	Name  string
	Level int
	HP    int
	MaxHP int
}

// Presence is where a connected player is; every world command carries it.
type Presence struct {
	Character kernel.CharacterID
	MapID     string
}

// Actor is who acts on items: the account scopes idempotency keys, the character owns the items.
type Actor struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
}

// Welcome is what a joined player starts from; Token replaces the one Hello sent.
type Welcome struct {
	Token      string
	Character  protocol.CharacterView
	Inventory  []protocol.ItemView
	Party      *protocol.PartyView
	MapID      string
	ServerTime time.Time
}

// Keys are the keys a player holds.
type Keys struct {
	Left, Right, Up, Down, Jump bool
}

// ItemMove moves an item to a slot of a tab.
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
