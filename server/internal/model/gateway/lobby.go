package gateway

import (
	"time"

	"finga/internal/shared/kernel"
)

// Piece is a wardrobe entry: a piece the account collected, what gave it, and when.
type Piece struct {
	ID      string
	Cause   string         // item; rank, quest and feat come with their sources
	Item    *kernel.ItemID // the item added, for an item alone
	AddedAt time.Time
}

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

// ArenaBuild is a character's arena build: eight keys, "" for an empty one, and the trait chosen for skills on it.
type ArenaBuild struct {
	Bar    []string
	Traits map[string]string
}

// PvpStanding is a character in a rated mode: their shown rating and rank, the placement matches left, their record, and their place on the board, 0 before they're placed.
type PvpStanding struct {
	Mode                string
	Shown               int
	Rank                string
	PlacementLeft       int
	Wins, Losses, Draws int
	Place               int
}

// PvpProfile is a character's standing in each rated mode, and their arena build.
type PvpProfile struct {
	Standings []PvpStanding
	Build     ArenaBuild
}

// PvpRow is a character on a mode's board, a tie sharing its place.
type PvpRow struct {
	Place               int
	Character           kernel.CharacterID
	Name                string
	Class               string
	Shown               int
	Rank                string
	Wins, Losses, Draws int
}
