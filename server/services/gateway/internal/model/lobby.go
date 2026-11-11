package model

import (
	"time"

	"finga/internal/kernel"
)

// Account is an account as the REST API shows it, never with its password.
type Account struct {
	ID        kernel.AccountID
	Username  string
	CreatedAt time.Time
}

// Settings are an account's settings.
type Settings struct {
	ControlPreset    string
	AutoAssignPoints bool
	ChatPinned       bool
}

// Stats are a character's five primary stats.
type Stats struct {
	STR, DEX, INT, AGI, VIT int
}

// Character is a character as the REST API lists it.
type Character struct {
	ID    kernel.CharacterID
	Name  string
	Class string
	Level int
	Exp   int64
	Stats Stats
	HP    int
	MP    int
	MapID string
	// The skill on each of keys 1 to 6, "" for none.
	SkillBar []string
}
