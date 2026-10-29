// Package item is the item module: item instances, the inventory, and the
// append-only item events (D2, D5). Every change is one transaction through
// the module's unit of work: the idempotency check first, then the rows,
// the events, and an outbox event, so a change can be retried safely and is
// never half-applied. It owns the items and item_events tables.
package item

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// The module's errors. Each names one failure; the service's handler sends
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

// IsRule reports whether err is one of the module's rule errors, as opposed
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

// AddInput puts a quantity of a template into the inventory: the first
// stack with room, then the first free slot.
type AddInput struct {
	Key        string // idempotency key
	TemplateID string
	Quantity   int
	Cause      Cause
}

// MoveInput moves an item to a slot of its tab, or merges it into a stack
// of the same item there.
type MoveInput struct {
	Key    string
	ItemID kernel.ItemID
	Tab    gamedata.Tab
	Slot   int
}

// DiscardInput destroys a whole item or part of a stack.
type DiscardInput struct {
	Key      string
	ItemID   kernel.ItemID
	Quantity int
}

// Slot is one changed inventory slot: Item is nil when it emptied.
type Slot struct {
	Tab  gamedata.Tab `json:"tab"`
	Slot int          `json:"slot"`
	Item *Item        `json:"item,omitempty"`
}

// Result is what a command changed; it is stored for idempotent replays.
type Result struct {
	Changed []Slot `json:"changed"`
}

// Grant is a starter item for a class (classes.yaml).
type Grant = gamedata.Grant
