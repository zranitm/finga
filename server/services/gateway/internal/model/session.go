package model

import (
	"time"

	"finga/internal/kernel"
)

// Player is the character a connection plays, as its party sees it.
type Player struct {
	ID    kernel.CharacterID
	Name  string
	Level int
	HP    int
	MaxHP int
}

// Presence is a connected player: its character and the connection it joined through; every world command carries it.
type Presence struct {
	Character  kernel.CharacterID
	Connection string
}

// Actor is who acts on items: the account scopes idempotency keys, the character owns the items.
type Actor struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
}

// EndReason is why the gateway ends a player's session; the handler decides what the client sees.
type EndReason string

// The reasons a session ends.
const (
	EndLoggedInElsewhere EndReason = "logged_in_elsewhere"
	EndSessionExpired    EndReason = "session_expired"
	EndServerRestart     EndReason = "server_restart"
	EndDeliveryBacklog   EndReason = "delivery_backlog" // a join held more deliveries than it may
	EndRejoinFailed      EndReason = "rejoin_failed"
	EndTickRateChanged   EndReason = "tick_rate_changed" // a rejoin found the world at another rate, which the client learns from a new Welcome
)

// Welcome is what a joined player starts from; Token replaces the one Hello sent.
type Welcome struct {
	Token             string
	Character         CharacterView
	Inventory         []ItemView
	Equipment         []EquipSlot
	InventoryRevision uint64
	MapID             string
	ServerTime        time.Time
	TickRateHz        int
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

// ItemEquip wears an Equip-tab item; EquipSlot is needed for rings only.
type ItemEquip struct {
	IdempotencyKey string
	Item           kernel.ItemID
	EquipSlot      string
}

// PointsSpend adds points to the stats, out of the unspent ones, if their count is still ExpectedUnspent.
type PointsSpend struct {
	ExpectedUnspent int
	Points          Stats
}

// ItemUnequip puts a worn item back in the Equip tab, in ToSlot or, when nil, the first free slot.
type ItemUnequip struct {
	IdempotencyKey string
	EquipSlot      string
	ToSlot         *int
}

// ItemDiscard destroys some or all of a stack.
type ItemDiscard struct {
	IdempotencyKey string
	Item           kernel.ItemID
	Quantity       int
}
