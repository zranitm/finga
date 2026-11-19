package world

import (
	"time"

	"github.com/google/uuid"

	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// LootKind is what became of a pouch's items.
type LootKind int

// What became of a pouch's items.
const (
	LootTaken LootKind = iota + 1 // in the taker's inventory
	LootLeft                      // still in the pouch when its life ended
)

// LootItem is an item a Loot names.
type LootItem struct {
	Template string
	Quantity int
}

// Loot tells a player what became of a pouch's items: their own taken or left, or a party member's taken.
type Loot struct {
	Tick      uint64
	Kind      LootKind
	MapID     string // pouch IDs are the map's own
	Pouch     uint64
	Owner     kernel.CharacterID
	OwnerName string
	Items     []LootItem
}

func (Loot) event() {}

// PouchKind is what a pouch is: a kill's, or a boss's box for one player.
type PouchKind int

// The pouch kinds.
const (
	PouchSmall PouchKind = iota + 1 // a kill's
	PouchBox                        // a boss's, one per eligible player
)

// PouchItem is an item in a pouch as its owner sees it; ID is its own within the pouch.
type PouchItem struct {
	ID       uint32
	Template string
	Quantity int
	Stats    *progression.ItemStats // equipment's roll, nil for anything else
}

// PouchView is a pouch as its owner alone sees it, gone after GoneIn.
type PouchView struct {
	ID      uint64
	Kind    PouchKind
	X, Y, Z float64
	Items   []PouchItem
	GoneIn  time.Duration
}

// Pouches are the changes to a player's own pouches in a frame: those new or changed, and those gone.
type Pouches struct {
	Changed []PouchView
	Gone    []uint64
}

// Pick is one item a take asks for.
type Pick struct {
	Pouch uint64
	Item  uint32
}

// PickAnswer is what a pick came to: taken when Err is nil, else refused with why.
type PickAnswer struct {
	Pick
	Err error
}

// Grant is an item for the item module to add; Key is its idempotency key, the same on every try.
type Grant struct {
	Key      uuid.UUID
	Template string
	Quantity int
	Stats    *progression.ItemStats // equipment's roll, nil for anything else
}

// GrantCall is one call to the item module: items for one owner, each granted under its own key.
type GrantCall struct {
	Account   kernel.AccountID // scopes the keys
	Character kernel.CharacterID
	Drops     []Grant
}

// GrantKind is how the item module answered one drop of a call.
type GrantKind int

// How the item module answered a drop.
const (
	GrantAdded    GrantKind = iota + 1 // added by this call
	GrantReplayed                      // added by an earlier call, whose reply it answers
	GrantNoRoom                        // refused: the inventory is full
	GrantRefused                       // refused by another rule, which no retry passes
)

// GrantOutcome is one drop's answer.
type GrantOutcome struct {
	Kind GrantKind
	// Replayed is a replayed drop's stored reply: the slots its grant changed, at the revision it took; empty when the item module kept none.
	Replayed Inventory
	Reason   string // why a refused drop was refused
}

// Granted is a call's answer: each drop's outcome in the call's order, and the slots the drops it added changed, at the call's revision.
type Granted struct {
	Outcomes []GrantOutcome
	Added    Inventory
}
