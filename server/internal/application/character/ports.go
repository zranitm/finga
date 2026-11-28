package character

import (
	"context"

	model "finga/internal/model/character"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// Store reads on the pool and writes only through InTx.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	// Load returns ErrNotFound for another account's character, so ownership and existence look alike.
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
	// Find is the characters with ids, of any account; one that doesn't exist isn't there.
	Find(ctx context.Context, ids []kernel.CharacterID) ([]model.Character, error)
	// Quests is the character's quests, active and finished.
	Quests(ctx context.Context, id kernel.CharacterID) ([]model.Quest, error)
	// Skills is the character's skills with points.
	Skills(ctx context.Context, id kernel.CharacterID) ([]model.SkillPoints, error)
	// Timers is the character's skill timers as the last checkpoint left them.
	Timers(ctx context.Context, id kernel.CharacterID) ([]model.SkillTimer, error)
	// Waypoints is the waypoints the character has found.
	Waypoints(ctx context.Context, id kernel.CharacterID) ([]model.Waypoint, error)
}

// UnitOfWork is every repository bound to one transaction.
type UnitOfWork interface {
	Characters() Repository
}

// StarterItems grants starter items after the character commits; grants are idempotent.
type StarterItems interface {
	GrantStarter(ctx context.Context, account kernel.AccountID, character kernel.CharacterID) error
	Retryable(err error) bool
}

// Repository writes the characters table.
type Repository interface {
	// CountLocked counts the account's characters, holding the account's lock until the transaction ends.
	CountLocked(ctx context.Context, account kernel.AccountID) (int, error)
	// Insert returns the character as stored, with its updated_at.
	Insert(ctx context.Context, c model.Character) (model.Character, error)
	// SaveCheckpoint writes the character's row and its quests as listed, and deletes any other quest of it.
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu model.LevelUp) error
	// SaveWaypoints adds the waypoints the character has not found yet, each with when it was found, and changes none it has.
	SaveWaypoints(ctx context.Context, id kernel.CharacterID, found []model.Waypoint) error
	// SetSkillBar stores the account's character's skill bar; another account's character is ErrNotFound.
	SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error
}

// Content is what the use cases need from game data.
type Content interface {
	// StartClass is the class every new character starts as: the root of the tree.
	StartClass() gamedata.Class
	Class(id kernel.Class) (gamedata.Class, bool)
	StatRules() gamedata.StatRules
	StartTown() *gamedata.Map
	Skill(id string) (gamedata.Skill, bool)
}
