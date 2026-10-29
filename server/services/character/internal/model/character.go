// Package model is the character service's model (Low-level design, page
// 11): a character as stored, the checkpoint and level-up it saves, and
// the errors its use cases return. The progression rules it follows
// (derived stats, EXP awards, the death penalty) are the shared kernel's
// internal/progression, so the world runs them inside a tick.
package model

import (
	"errors"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// The service's errors. Each names one failure; the service's handler sends
// it as a reason, and the gateway gives it a code, a status and the words a
// player sees (its errmap).
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
	Exp           int64 // within the current level
	Stats         gamedata.Stats
	UnspentPoints int
	HP            int
	MP            int
	MapID         string
	X, Y          float64 // feet centre, map pixels
	UpdatedAt     time.Time
}

// Checkpoint is the non-economic state saved every 30 seconds (TD-02) and
// on map change and logout.
type Checkpoint struct {
	ID    kernel.CharacterID
	MapID string
	X, Y  float64
	HP    int
	MP    int
	Exp   int64
	At    time.Time
}

// LevelUp is saved right away when a character gains a level.
type LevelUp struct {
	ID    kernel.CharacterID
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MP    int
	At    time.Time
}
