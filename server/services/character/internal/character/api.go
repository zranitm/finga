// Package character is the character module: creation, loading, stats,
// EXP and levels. The rules (derived stats, EXP awards, the death penalty)
// are plain functions over values, so the world can run them inside a tick;
// the service persists checkpoints and level-ups from worker goroutines.
// It owns the characters table.
package character

import (
	"errors"
	"time"

	"finga/internal/gamedata"
	"finga/internal/model"
)

// The module's errors. Each names one failure; the handlers give it a
// code, a status and the words a player sees (internal/handler/errmap).
var (
	ErrNotFound         = errors.New("character: not found")
	ErrNameTaken        = errors.New("character: name taken")
	ErrBadName          = errors.New("character: bad name")
	ErrClassUnavailable = errors.New("character: class not available")
	ErrSlotsFull        = errors.New("character: account has no free character slot")
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
