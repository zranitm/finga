package model

import (
	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/internal/progression"
)

// LootKind is what became of a drop.
type LootKind int

// What became of a drop.
const (
	LootCollected LootKind = iota + 1 // in its owner's inventory
	LootWaiting                       // on the ground until its owner has room
	LootLost                          // gone at the end of its life, never collected
)

// Loot tells a player what became of a drop: their own, or a party member's they saw collected.
type Loot struct {
	Tick      uint64
	Kind      LootKind
	MapID     string // drop IDs are the map's own
	DropID    uint64
	Owner     kernel.CharacterID
	OwnerName string
	Template  string
	Quantity  int
}

func (Loot) event() {}

// Grant is a drop for the item service to add; Key is its idempotency key, the same on every try.
type Grant struct {
	Key      uuid.UUID
	Template string
	Quantity int
	Stats    *progression.ItemStats // equipment's roll, nil for anything else
}

// GrantCall is one call to the item service: drops for one owner, each granted under its own key.
type GrantCall struct {
	Account   kernel.AccountID // scopes the keys
	Character kernel.CharacterID
	Drops     []Grant
}

// GrantKind is how the item service answered one drop of a call.
type GrantKind int

// How the item service answered a drop.
const (
	GrantAdded    GrantKind = iota + 1 // added by this call
	GrantReplayed                      // added by an earlier call, whose reply it answers
	GrantNoRoom                        // refused: the inventory is full
	GrantRefused                       // refused by another rule, which no retry passes
)

// GrantOutcome is one drop's answer.
type GrantOutcome struct {
	Kind GrantKind
	// Replayed is a replayed drop's stored reply: the slots its grant changed, at the revision it took; empty when the item service kept none.
	Replayed Inventory
	Reason   string // why a refused drop was refused
}

// Granted is a call's answer: each drop's outcome in the call's order, and the slots the drops it added changed, at the call's revision.
type Granted struct {
	Outcomes []GrantOutcome
	Added    Inventory
}
