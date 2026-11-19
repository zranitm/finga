package gateway

import (
	"time"

	"finga/internal/shared/kernel"
)

// Account is an account as the REST API shows it, never with its password.
type Account struct {
	ID        kernel.AccountID
	Username  string
	CreatedAt time.Time
}

// Settings are an account's settings.
type Settings struct {
	AutoAssignPoints bool
	ChatPinned       bool
	HideMinimap      bool
	Zoom             float64
	Walk             bool
	Keys             map[string]string // the key bindings that differ from the client's defaults
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
	MapID string
	// The skill on each of the bar's eight keys, "" for none.
	SkillBar []string
}
