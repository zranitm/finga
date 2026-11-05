package application

import (
	"context"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/outbox"
	"finga/services/item/internal/model"
)

// Store is what the service sees of persistence.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	Inventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error)
}

// UnitOfWork is everything bound to one transaction.
type UnitOfWork interface {
	Items() Repository
	Events() EventRepository
	Outbox() outbox.Writer
	Idempotency() Idempotency
}

// Repository reads and writes the items table; owned methods put the owner in the WHERE clause.
type Repository interface {
	// LockInventory takes the owner's lock, which orders that owner's changes (two would pick the same empty
	// slot), then locks the rows in ID order and returns them by tab and slot.
	LockInventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error)
	LockOwned(ctx context.Context, id kernel.ItemID, owner kernel.CharacterID) (model.Item, error)
	Insert(ctx context.Context, it model.Item) error
	SetQuantity(ctx context.Context, id kernel.ItemID, quantity int) error
	SetSlot(ctx context.Context, id kernel.ItemID, tab gamedata.Tab, slot int) error
	Destroy(ctx context.Context, id kernel.ItemID) error
}

// EventRepository appends to the item history.
type EventRepository interface {
	Append(ctx context.Context, e model.Event) error
}

// Idempotency runs a command once per key; the same key with another request is ErrKeyReused.
type Idempotency interface {
	Run(ctx context.Context, owner kernel.AccountID, scope, key string, request any, fn func() (Result, error)) (Result, error)
}

// Content is what the use cases need from game data.
type Content interface {
	Item(id string) (gamedata.Item, bool)
	Class(id kernel.Class) (gamedata.Class, bool)
}
