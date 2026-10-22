// Package item is the item module: item instances, the inventory, and the
// append-only item events (D2, D5). Every change is one transaction through
// the module's unit of work: the idempotency check first, then the rows,
// the events, and an outbox event, so a change can be retried safely and is
// never half-applied. It owns the items and item_events tables.
package item

import (
	"time"

	"github.com/google/uuid"

	"finga/internal/application/gamedata"
	"finga/internal/model"
	"finga/pkg/errs"
)

// The module's errors.
var (
	ErrNotFound      = errs.New(errs.NotFound, "item not found")
	ErrSlotTaken     = errs.New(errs.Conflict, "that slot is taken")
	ErrDifferentItem = errs.New(errs.Conflict, "that slot holds a different item")
	ErrInventoryFull = errs.New("inventory_full", "your inventory is full")
	ErrKeyReused     = errs.New("idempotency_key_reused", "that request was already made with other arguments")
	ErrBadQuantity   = errs.Invalid("quantity must be at least 1 and no more than the item holds")
	ErrBadSlot       = errs.Invalid("slot must be within the tab")
	ErrWrongTab      = errs.Invalid("an item stays in its own tab")
	ErrUnknownItem   = errs.Invalid("unknown item")
)

// SlotsPerTab is the inventory size of each tab (page 5).
const SlotsPerTab = 24

// Actor is who acts: the account scopes idempotency keys, the character
// owns the items.
type Actor struct {
	Account   model.AccountID
	Character model.CharacterID
}

// Item is an item instance in an inventory.
type Item struct {
	ID         model.ItemID
	TemplateID string
	Owner      model.CharacterID
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
	ItemID        model.ItemID
	Kind          EventKind
	QuantityDelta int
	FromOwner     *model.CharacterID
	ToOwner       *model.CharacterID
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
	ItemID model.ItemID
	Tab    gamedata.Tab
	Slot   int
}

// DiscardInput destroys a whole item or part of a stack.
type DiscardInput struct {
	Key      string
	ItemID   model.ItemID
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
