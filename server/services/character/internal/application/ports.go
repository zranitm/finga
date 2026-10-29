package application

import (
	"context"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/character/internal/model"
)

// Store is what the service sees of persistence: plain reads on the pool,
// and InTx for anything that writes.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	// Load returns the character only if it belongs to the account;
	// otherwise ErrNotFound, so ownership and existence look the same.
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
}

// UnitOfWork is every repository bound to one transaction.
type UnitOfWork interface {
	Characters() Repository
}

// StarterItems grants a new character its class's starter items, after the
// character has committed: no transaction spans two services (page 10).
// The item service implements it, through an adapter over its client; a
// grant is idempotent, keyed by the character ID, so retrying it is safe.
type StarterItems interface {
	GrantStarter(ctx context.Context, account kernel.AccountID, character kernel.CharacterID, class kernel.Class) error
	// Retryable reports whether a failed grant may succeed when tried again:
	// a transient failure, not a rule it ran into.
	Retryable(err error) bool
}

// Repository writes the characters table.
type Repository interface {
	Insert(ctx context.Context, c model.Character) error
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu model.LevelUp) error
}

// Content is what the use cases need from game data.
type Content interface {
	Class(id kernel.Class) (gamedata.Class, bool)
	Formulas(id kernel.Class) (gamedata.Formulas, bool)
	Town() *gamedata.Map
}
