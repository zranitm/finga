package item

import (
	"context"

	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/pkg/outbox"
)

// Store is what the service sees of persistence.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	// Inventory lists the owner's items, by tab and slot, on the pool.
	Inventory(ctx context.Context, owner model.CharacterID) ([]Item, error)
}

// UnitOfWork is everything bound to one transaction.
type UnitOfWork interface {
	Items() Repository
	Events() EventRepository
	Outbox() outbox.Writer
	Idempotency() Idempotency
}

// Repository reads and writes the items table on one connection. Ownership
// is decided by the service and made atomic here: owned methods put the
// owner in the WHERE clause.
type Repository interface {
	// LockInventory locks the owner's inventory rows in ID order and returns
	// them by tab and slot.
	LockInventory(ctx context.Context, owner model.CharacterID) ([]Item, error)
	LockOwned(ctx context.Context, id model.ItemID, owner model.CharacterID) (Item, error)
	Insert(ctx context.Context, it Item) error
	SetQuantity(ctx context.Context, id model.ItemID, quantity int) error
	SetSlot(ctx context.Context, id model.ItemID, tab gamedata.Tab, slot int) error
	Destroy(ctx context.Context, id model.ItemID) error
}

// EventRepository appends to the item history.
type EventRepository interface {
	Append(ctx context.Context, e Event) error
}

// Idempotency runs a command once per key: it hashes the request, stores
// the result, and replays it for the same key and request. The same key
// with another request is ErrKeyReused.
type Idempotency interface {
	Run(ctx context.Context, owner model.AccountID, scope, key string, request any, fn func() (Result, error)) (Result, error)
}

// Content is what the module needs from game data.
type Content interface {
	Item(id string) (gamedata.Item, bool)
	Class(id model.Class) (gamedata.Class, bool)
}
