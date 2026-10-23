package character

import (
	"context"

	"finga/internal/application/gamedata"
	"finga/internal/model"
)

// Store is what the service sees of persistence: plain reads on the pool,
// and InTx for anything that writes.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	List(ctx context.Context, account model.AccountID) ([]Character, error)
	// Load returns the character only if it belongs to the account;
	// otherwise ErrNotFound, so ownership and existence look the same.
	Load(ctx context.Context, id model.CharacterID, account model.AccountID) (Character, error)
}

// UnitOfWork is every repository bound to one transaction, plus the item
// module's starter rules bound to the same transaction.
type UnitOfWork interface {
	Characters() Repository
	Starter() StarterItems
}

// StarterItems grants a new character its class's starter items inside
// character creation's transaction. The item module implements it;
// internal/server binds the two without either importing the other.
type StarterItems interface {
	GrantStarter(ctx context.Context, owner model.CharacterID, class model.Class) error
}

// Repository writes the characters table.
type Repository interface {
	Insert(ctx context.Context, c Character) error
	SaveCheckpoint(ctx context.Context, cp Checkpoint) error
	SaveLevelUp(ctx context.Context, lu LevelUp) error
}

// Content is what the module needs from game data.
type Content interface {
	Class(id model.Class) (gamedata.Class, bool)
	Formulas(id model.Class) (gamedata.Formulas, bool)
	Town() *gamedata.Map
}

// ExpCurve is what the EXP rules need from game data.
type ExpCurve interface {
	ExpToNext(level int) (int64, bool)
	LevelCap() int
}
