// Package character is the character module: creation, loading, stats,
// EXP and levels. The rules (derived stats, EXP awards, the death penalty)
// are plain functions over values, so the world can run them inside a tick;
// the service persists checkpoints and level-ups from worker goroutines.
// It owns the characters table.
package character

import (
	"time"

	"finga/internal/application/gamedata"
	"finga/internal/model"
	"finga/pkg/errs"
)

// The module's errors.
var (
	ErrNotFound         = errs.New(errs.NotFound, "character not found")
	ErrNameTaken        = errs.New(errs.Conflict, "that name is taken")
	ErrBadName          = errs.Invalid("name must be 3 to 12 letters and digits, starting with a letter")
	ErrClassUnavailable = errs.Invalid("that class is not available")
)

// Character is a character as stored.
type Character struct {
	ID            model.CharacterID
	AccountID     model.AccountID
	Name          string
	Class         model.Class
	Level         int
	Exp           int64 // within the current level
	Stats         gamedata.Stats
	UnspentPoints int
	HP            int
	MP            int
	MapID         string
	X, Y          float64 // feet centre, map pixels
	UpdatedAt     time.Time
}

// Progress is a level and the EXP within it.
type Progress struct {
	Level int
	Exp   int64
}

// Derived are the stats computed from class, primary stats, level and
// equipment (page 5). HP and MP are whole points; the rest stay fractional
// for the damage formula. Rates are fractions.
type Derived struct {
	MaxHP       int
	MaxMP       int
	AttackPower float64
	Accuracy    float64
	Evasion     float64
	Defense     float64
	CritRate    float64
	CritDamage  float64
	Penetration float64
}

// Equipment is what worn items add. Stage 1 characters wear nothing, so
// the world passes the zero value; step 12's starter items are inventory.
type Equipment struct {
	WeaponAttack float64
	ArmorDefense float64
	Penetration  float64
}

// CreateInput is a new character.
type CreateInput struct {
	Name  string
	Class model.Class
}

// Checkpoint is the non-economic state saved every 30 seconds (TD-02) and
// on map change and logout.
type Checkpoint struct {
	ID    model.CharacterID
	MapID string
	X, Y  float64
	HP    int
	MP    int
	Exp   int64
	At    time.Time
}

// LevelUp is saved right away when a character gains a level.
type LevelUp struct {
	ID    model.CharacterID
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MP    int
	At    time.Time
}
