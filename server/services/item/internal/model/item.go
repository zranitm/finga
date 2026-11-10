// Package model holds the item service's items, inventory slots, item events and errors.
package model

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

// The service's errors; the handler sends each as a contract reason.
var (
	ErrNotFound       = errors.New("item: not found")
	ErrSlotTaken      = errors.New("item: slot taken")
	ErrDifferentItem  = errors.New("item: slot holds a different item")
	ErrStackFull      = errors.New("item: the stack in that slot is full")
	ErrInventoryFull  = errors.New("item: inventory full")
	ErrKeyReused      = errors.New("item: idempotency key reused with other arguments")
	ErrBadKey         = errors.New("item: idempotency key is not a UUID")
	ErrBadCause       = errors.New("item: unknown cause type")
	ErrBadQuantity    = errors.New("item: bad quantity")
	ErrBadSlot        = errors.New("item: slot outside the tab")
	ErrWrongTab       = errors.New("item: item moved to another tab")
	ErrUnknownItem    = errors.New("item: unknown item template")
	ErrBadRoll        = errors.New("item: equipment needs a roll within its template's ranges, and nothing else takes one")
	ErrItemWorn       = errors.New("item: the item is worn; unequip it first")
	ErrLevelTooLow    = errors.New("item: the character's level is below the item's")
	ErrWrongClass     = errors.New("item: the item is for another class")
	ErrWrongEquipSlot = errors.New("item: the item doesn't go in that equip slot")
	ErrNotEquipment   = errors.New("item: only equipment can be worn")
	ErrNothingWorn    = errors.New("item: nothing is worn in that equip slot")
	ErrBadBatch       = errors.New("item: a batch holds no drops, or more than one call takes")
)

// SlotsPerTab is the inventory size of each tab.
const SlotsPerTab = 24

// Actor is who acts: the account scopes idempotency keys, the character owns the items.
type Actor struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
}

// Inventory is a character's items, those in its tabs then those it wears, as they stood at Revision, the count of their changes.
type Inventory struct {
	Revision uint64
	Items    []Item
}

// Item is an item instance in an inventory slot, or worn in an equip slot.
type Item struct {
	ID         kernel.ItemID
	TemplateID string
	Owner      kernel.CharacterID
	Tab        gamedata.Tab
	Slot       int
	Quantity   int
	Stats      *progression.ItemStats // what it rolled; nil for anything but equipment
	EquipSlot  string                 // where it is worn; empty in the inventory, where Tab and Slot say where it is
}

// Worn reports whether the item is worn.
func (it Item) Worn() bool { return it.EquipSlot != "" }

// CauseType says what caused an item change.
type CauseType string

// The cause types.
const (
	CauseDrop    CauseType = "drop"
	CausePlayer  CauseType = "player"
	CauseStarter CauseType = "starter"
)

// Cause is the origin of a change: a drop, a player action, or character creation.
type Cause struct {
	Type CauseType
	ID   uuid.UUID // optional: the drop or request
}

// EventKind is an item event's kind.
type EventKind string

// The event kinds.
const (
	EventCreated    EventKind = "created"
	EventStacked    EventKind = "stacked"
	EventSplit      EventKind = "split"
	EventMoved      EventKind = "moved"
	EventDiscarded  EventKind = "discarded"
	EventEquipped   EventKind = "equipped"
	EventUnequipped EventKind = "unequipped"
)

// Event is one line of an item's history.
type Event struct {
	ItemID        kernel.ItemID
	Kind          EventKind
	QuantityDelta int
	FromOwner     *kernel.CharacterID
	ToOwner       *kernel.CharacterID
	Cause         Cause
	OccurredAt    time.Time
}

// Slot is one changed inventory slot: Item is nil when it emptied.
type Slot struct {
	Tab  gamedata.Tab
	Slot int
	Item *Item
}

// WornSlot is one changed equip slot: Item is nil when it emptied.
type WornSlot struct {
	Slot string
	Item *Item
}

// Grant is a starter item for a class (classes.yaml).
type Grant = gamedata.Grant

// SortInventory orders an inventory by tab then slot, the worn items last by equip slot.
func SortInventory(inv []Item) {
	sort.Slice(inv, func(a, b int) bool {
		x, y := inv[a], inv[b]
		switch {
		case x.Worn() != y.Worn():
			return y.Worn()
		case x.Worn():
			return x.EquipSlot < y.EquipSlot
		case x.Tab != y.Tab:
			return x.Tab < y.Tab
		}
		return x.Slot < y.Slot
	})
}
