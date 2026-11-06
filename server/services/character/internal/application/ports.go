package application

import (
	"context"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/character/internal/model"
)

// Store reads on the pool and writes only through InTx.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	// Load returns ErrNotFound for another account's character, so ownership and existence look alike.
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
}

// UnitOfWork is every repository bound to one transaction.
type UnitOfWork interface {
	Characters() Repository
}

// StarterItems grants starter items after the character commits; grants are idempotent.
type StarterItems interface {
	GrantStarter(ctx context.Context, account kernel.AccountID, character kernel.CharacterID, class kernel.Class) error
	Retryable(err error) bool
}

// Repository writes the characters table.
type Repository interface {
	// CountLocked counts the account's characters, holding the account's lock until the transaction ends.
	CountLocked(ctx context.Context, account kernel.AccountID) (int, error)
	// Insert returns the character as stored, with its updated_at.
	Insert(ctx context.Context, c model.Character) (model.Character, error)
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu model.LevelUp) error
}

// Content is what the use cases need from game data.
type Content interface {
	Class(id kernel.Class) (gamedata.Class, bool)
	Formulas(id kernel.Class) (gamedata.Formulas, bool)
	Town() *gamedata.Map
}
