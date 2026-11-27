package item

import (
	"context"

	model "finga/internal/model/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// Store is what the service sees of persistence.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	Inventory(ctx context.Context, owner kernel.CharacterID) (model.Inventory, error)
	// HandedIn finds the owner's hand-ins of the completions asked, leaving out those it has none of.
	HandedIn(ctx context.Context, owner kernel.CharacterID, asked []QuestCompletion) ([]HandIn, error)
	// Pieces lists the account's wardrobe, oldest first.
	Pieces(ctx context.Context, account kernel.AccountID) ([]model.Piece, error)
	// Look is the character's look, the zero Look when it never set one.
	Look(ctx context.Context, character kernel.CharacterID) (model.Look, error)
}

// UnitOfWork is everything bound to one transaction.
type UnitOfWork interface {
	Items() Repository
	Events() EventRepository
	Notices() Notices
	Idempotency() Idempotency
	HandIns() HandIns
	Wardrobe() Wardrobe
	// Savepoint runs fn; when fn fails, everything it wrote is undone and the transaction carries on.
	Savepoint(ctx context.Context, fn func() error) error
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

// Idempotency runs a command once per key and replays its Changes; one key with another fingerprint is ErrKeyReused.
type Idempotency interface {
	Run(ctx context.Context, owner kernel.AccountID, scope, key string, fingerprint []byte, fn func() (Changes, error)) (Changes, error)
	// Lock takes the lock Run takes for key, until the transaction ends.
	Lock(ctx context.Context, owner kernel.AccountID, scope, key string) error
}

// HandIns keeps each completion of a quest handed in, for good.
type HandIns interface {
	// Claim takes the owner's inventory lock, so a second hand-in of a completion waits for the first to commit, then finds the completion's record if there is one.
	Claim(ctx context.Context, owner kernel.CharacterID, c QuestCompletion) (HandIn, bool, error)
	Record(ctx context.Context, owner kernel.CharacterID, h HandIn) error
}

// Wardrobe keeps each account's pieces and each character's look.
type Wardrobe interface {
	// Add puts piece in the account's wardrobe, from item; false when it has it.
	Add(ctx context.Context, account kernel.AccountID, piece string, item kernel.ItemID) (bool, error)
	// Grant puts g's piece in the account's wardrobe for its cause; false when it has it.
	Grant(ctx context.Context, account kernel.AccountID, g model.PieceGrant) (bool, error)
	// Collected is which of pieces the account has.
	Collected(ctx context.Context, account kernel.AccountID, pieces []string) ([]string, error)
	// LockLook reads the character's look for a change in this transaction, the zero Look when it has none.
	LockLook(ctx context.Context, character kernel.CharacterID) (model.Look, error)
	// SetLook stores the character's look, one revision on, and returns it.
	SetLook(ctx context.Context, character kernel.CharacterID, look model.Look) (model.Look, error)
}

// Content is what the use cases need from game data.
type Content interface {
	Item(id string) (gamedata.Item, bool)
	Class(id kernel.Class) (gamedata.Class, bool)
	StartClass() gamedata.Class
	EquipSlots() []gamedata.EquipSlot
	EquipSlot(id string) (gamedata.EquipSlot, bool)
	Piece(id string) (gamedata.Piece, bool)
	PieceIn(item string) (gamedata.Piece, bool)
}
