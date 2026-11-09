package application

import (
	"context"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/item/internal/model"
)

// Store is what the service sees of persistence.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	Inventory(ctx context.Context, owner kernel.CharacterID) (model.Inventory, error)
}

// UnitOfWork is everything bound to one transaction.
type UnitOfWork interface {
	Items() Repository
	Events() EventRepository
	Notices() Notices
	Idempotency() Idempotency
}

// Repository reads and writes the items table; owned methods put the owner in the WHERE clause.
type Repository interface {
	// LockInventory takes the owner's lock so two changes can't pick one empty slot, then locks its inventory and worn rows in ID order.
	LockInventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error)
	LockOwned(ctx context.Context, id kernel.ItemID, owner kernel.CharacterID) (model.Item, error)
	// RecordStarterGrant records that owner got its starter kit; false when it already had.
	RecordStarterGrant(ctx context.Context, owner kernel.CharacterID) (bool, error)
	Insert(ctx context.Context, it model.Item) error
	// AddRoll gives an existing item the roll it lacks.
	AddRoll(ctx context.Context, id kernel.ItemID, stats progression.ItemStats) error
	SetQuantity(ctx context.Context, id kernel.ItemID, quantity int) error
	// SetSlot puts an item in an inventory slot, taking it off if it was worn.
	SetSlot(ctx context.Context, id kernel.ItemID, tab gamedata.Tab, slot int) error
	SetEquipped(ctx context.Context, id kernel.ItemID, equipSlot string) error
	Destroy(ctx context.Context, id kernel.ItemID) error
	// NextRevision counts one more change to the owner's inventory and returns the count.
	NextRevision(ctx context.Context, owner kernel.CharacterID) (uint64, error)
}

// EventRepository appends to the item history.
type EventRepository interface {
	Append(ctx context.Context, e model.Event) error
}

// Notices tells other services about item changes, in the change's transaction.
type Notices interface {
	Send(ctx context.Context, n model.Notice) error
}

// Idempotency runs a command once per key and replays its Result; one key with another fingerprint is ErrKeyReused.
type Idempotency interface {
	Run(ctx context.Context, owner kernel.AccountID, scope, key string, fingerprint []byte, fn func() (Result, error)) (Result, error)
}

// Content is what the use cases need from game data.
type Content interface {
	Item(id string) (gamedata.Item, bool)
	Class(id kernel.Class) (gamedata.Class, bool)
	EquipSlots() []gamedata.EquipSlot
	EquipSlot(id string) (gamedata.EquipSlot, bool)
}
