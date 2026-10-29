// Package model is the item service's model (Low-level design, page 11):
// item instances, the inventory's slots and order, the append-only item
// events and their outbox payloads (D2, D5), and the errors its use cases
// return. It needs nothing but data.
package model

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// The service's errors. Each names one failure; the service's handler sends
// it as a reason, and the gateway gives it a code, a status and the words a
// player sees (its errmap).
var (
	ErrNotFound      = errors.New("item: not found")
	ErrSlotTaken     = errors.New("item: slot taken")
	ErrDifferentItem = errors.New("item: slot holds a different item")
	ErrInventoryFull = errors.New("item: inventory full")
	ErrKeyReused     = errors.New("item: idempotency key reused with other arguments")
	ErrBadQuantity   = errors.New("item: bad quantity")
	ErrBadSlot       = errors.New("item: slot outside the tab")
	ErrWrongTab      = errors.New("item: item moved to another tab")
	ErrUnknownItem   = errors.New("item: unknown item template")
)

// rules are the errors above: a player ran into a rule, so retrying the
// same request cannot help. Anything else (a database failure) may pass.
var rules = []error{
	ErrNotFound, ErrSlotTaken, ErrDifferentItem, ErrInventoryFull, ErrKeyReused,
	ErrBadQuantity, ErrBadSlot, ErrWrongTab, ErrUnknownItem,
}

// IsRule reports whether err is one of the service's rule errors, as opposed
// to a transient failure worth one retry.
func IsRule(err error) bool {
	for _, rule := range rules {
		if errors.Is(err, rule) {
			return true
		}
	}
	return false
}

// SlotsPerTab is the inventory size of each tab (page 5).
const SlotsPerTab = 24

// Actor is who acts: the account scopes idempotency keys, the character
// owns the items.
type Actor struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
}

// Item is an item instance in an inventory.
type Item struct {
	ID         kernel.ItemID
	TemplateID string
	Owner      kernel.CharacterID
	Tab        gamedata.Tab
	Slot       int
	Quantity   int
	UpdatedAt  time.Time
}

// CauseType says what caused an item change.
type CauseType string

// The cause types (page 2).
const (
	CauseDrop    CauseType = "drop"
	CausePlayer  CauseType = "player"
	CauseStarter CauseType = "starter"
)

// Cause is the origin of a change: a drop (its ID), a player action, or
// character creation.
type Cause struct {
	Type CauseType
	ID   uuid.UUID // optional: the drop or request
}

// EventKind is an item event's kind.
type EventKind string

// The event kinds used in Stage 1 (page 2 lists the market ones too).
const (
	EventCreated   EventKind = "created"
	EventStacked   EventKind = "stacked"
	EventSplit     EventKind = "split"
	EventMoved     EventKind = "moved"
	EventDiscarded EventKind = "discarded"
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
	Tab  gamedata.Tab `json:"tab"`
	Slot int          `json:"slot"`
	Item *Item        `json:"item,omitempty"`
}

// Grant is a starter item for a class (classes.yaml).
type Grant = gamedata.Grant

// SortInventory orders an inventory by tab then slot, the order it is
// listed in.
func SortInventory(inv []Item) {
	sort.Slice(inv, func(a, b int) bool {
		if inv[a].Tab != inv[b].Tab {
			return inv[a].Tab < inv[b].Tab
		}
		return inv[a].Slot < inv[b].Slot
	})
}
