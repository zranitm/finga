// Package model is the character service's model: the stored character, its saves and errors.
package model

import (
	"errors"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// The service's errors.
var (
	ErrNotFound         = errors.New("character: not found")
	ErrNameTaken        = errors.New("character: name taken")
	ErrBadName          = errors.New("character: bad name")
	ErrClassUnavailable = errors.New("character: class not available")
	ErrSlotsFull        = errors.New("character: account has no free character slot")
)

// Character is a character as stored.
type Character struct {
	ID            kernel.CharacterID
	AccountID     kernel.AccountID
	Name          string
	Class         kernel.Class
	Level         int
	Exp           int64          // within the current level
	Points        gamedata.Stats // spent in each stat; the own stats are the class's starting stats plus these
	UnspentPoints int
	FreeResetUsed bool
	HP            int
	MP            int
	MapID         string
	X, Y          float64 // feet centre, map pixels
	UpdatedAt     time.Time
}

// Checkpoint is the non-economic state saved periodically, on map change and logout.
type Checkpoint struct {
	ID            kernel.CharacterID
	MapID         string
	X, Y          float64
	HP            int
	MP            int
	Level         int
	Exp           int64
	Points        gamedata.Stats
	UnspentPoints int
	FreeResetUsed bool
}

// LevelUp is saved right away when a character gains a level.
type LevelUp struct {
	ID     kernel.CharacterID
	Level  int
	Exp    int64
	Points gamedata.Stats
	HP     int
	MP     int
}
