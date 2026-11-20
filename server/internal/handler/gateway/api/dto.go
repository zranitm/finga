package api

import (
	"time"

	model "finga/internal/model/gateway"
)

// Request and response bodies of api/openapi.yaml; a drift test keeps them in sync.

// RegisterRequest is POST /api/accounts.
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AccountResponse is an account, never with its password hash.
type AccountResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// LoginRequest is POST /api/sessions.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SessionResponse carries the opaque token, the only time it is visible.
type SessionResponse struct {
	Token string `json:"token"`
}

// CreateCharacterRequest is POST /api/characters; every new character is a Beginner, so it names no class.
type CreateCharacterRequest struct {
	Name string `json:"name"`
}

// StatsResponse are the five primary stats.
type StatsResponse struct {
	STR int `json:"str"`
	DEX int `json:"dex"`
	INT int `json:"int"`
	AGI int `json:"agi"`
	VIT int `json:"vit"`
}

// CharacterResponse is a character as stored.
type CharacterResponse struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Class string        `json:"class"`
	Level int           `json:"level"`
	Exp   int64         `json:"exp"`
	Stats StatsResponse `json:"stats"`
	HP    int           `json:"hp"`
	MapID string        `json:"map_id"`
	// SkillBar is the skill on each of the bar's eight keys, "" for none.
	SkillBar []string `json:"skill_bar"`
}

// SkillBarRequest is PUT /api/characters/{id}/skill-bar.
type SkillBarRequest struct {
	Slots []string `json:"slots"`
}

// SkillBarResponse is the skill bar as saved.
type SkillBarResponse struct {
	Slots []string `json:"slots"`
}

// CharactersResponse is GET /api/characters.
type CharactersResponse struct {
	Characters []CharacterResponse `json:"characters"`
}

// SettingsRequest is PUT /api/settings; a flag left out is false, the zoom 0, and keys left out are the defaults.
type SettingsRequest struct {
	AutoAssignPoints bool              `json:"auto_assign_points"`
	ChatPinned       bool              `json:"chat_pinned"`
	HideMinimap      bool              `json:"hide_minimap"`
	Zoom             float64           `json:"zoom"`
	Walk             bool              `json:"walk"`
	Keys             map[string]string `json:"keys"`
}

// WardrobeResponse is the account's pieces, oldest first.
type WardrobeResponse struct {
	Pieces []PieceResponse `json:"pieces"` // never null: [] for none
}

// PieceResponse is one wardrobe entry: the piece, what gave it, the item for an item, and when.
type PieceResponse struct {
	ID      string    `json:"id"`
	Cause   string    `json:"cause"`
	ItemID  string    `json:"item_id,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

// SettingsResponse is the account's settings.
type SettingsResponse struct {
	AutoAssignPoints bool              `json:"auto_assign_points"`
	ChatPinned       bool              `json:"chat_pinned"`
	HideMinimap      bool              `json:"hide_minimap"`
	Zoom             float64           `json:"zoom"`
	Walk             bool              `json:"walk"`
	Keys             map[string]string `json:"keys"` // never null: {} for the defaults
}

// ArenaBuildRequest is a character's arena build: eight keys, "" for an empty one, and the trait chosen for skills on it.
type ArenaBuildRequest struct {
	SkillBar []string          `json:"skill_bar"`
	Traits   map[string]string `json:"traits"`
}

// ArenaBuildResponse is an arena build.
type ArenaBuildResponse struct {
	SkillBar []string          `json:"skill_bar"`
	Traits   map[string]string `json:"traits"` // never null: {} for none
}

// PvpStandingResponse is a character in a rated mode; place is 0 until they're placed.
type PvpStandingResponse struct {
	Mode          string `json:"mode"`
	Shown         int    `json:"shown"`
	Rank          string `json:"rank"`
	PlacementLeft int    `json:"placement_left"`
	Wins          int    `json:"wins"`
	Losses        int    `json:"losses"`
	Draws         int    `json:"draws"`
	Place         int    `json:"place"`
}

// PvpProfileResponse is a character's standing in each rated mode, and their arena build.
type PvpProfileResponse struct {
	Standings []PvpStandingResponse `json:"standings"`
	Build     ArenaBuildResponse    `json:"build"`
}

// LeaderboardResponse is a rated mode's board, highest first.
type LeaderboardResponse struct {
	Mode string                   `json:"mode"`
	Rows []LeaderboardRowResponse `json:"rows"` // never null: [] for none
}

// LeaderboardRowResponse is a character on a board; a tie shares its place.
type LeaderboardRowResponse struct {
	Place       int    `json:"place"`
	CharacterID string `json:"character_id"`
	Name        string `json:"name"`
	Class       string `json:"class"`
	Shown       int    `json:"shown"`
	Rank        string `json:"rank"`
	Wins        int    `json:"wins"`
	Losses      int    `json:"losses"`
	Draws       int    `json:"draws"`
}

// ErrorResponse is every error body: a stable six-digit code, which the client words itself, and a message for developers.
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func toAccount(a model.Account) AccountResponse {
	return AccountResponse{ID: a.ID.String(), Username: a.Username, CreatedAt: a.CreatedAt}
}

func toCharacter(c model.Character) CharacterResponse {
	s := c.Stats
	return CharacterResponse{
		ID: c.ID.String(), Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp,
		Stats: StatsResponse{STR: s.STR, DEX: s.DEX, INT: s.INT, AGI: s.AGI, VIT: s.VIT},
		HP:    c.HP, MapID: c.MapID, SkillBar: c.SkillBar,
	}
}

func toCharacters(cs []model.Character) CharactersResponse {
	out := CharactersResponse{Characters: make([]CharacterResponse, 0, len(cs))}
	for _, c := range cs {
		out.Characters = append(out.Characters, toCharacter(c))
	}
	return out
}

func toWardrobe(pieces []model.Piece) WardrobeResponse {
	out := WardrobeResponse{Pieces: make([]PieceResponse, 0, len(pieces))}
	for _, p := range pieces {
		r := PieceResponse{ID: p.ID, Cause: p.Cause, AddedAt: p.AddedAt}
		if p.Item != nil {
			r.ItemID = p.Item.String()
		}
		out.Pieces = append(out.Pieces, r)
	}
	return out
}

func toSettings(s model.Settings) SettingsResponse {
	keys := s.Keys
	if keys == nil {
		keys = map[string]string{}
	}
	return SettingsResponse{AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned, HideMinimap: s.HideMinimap, Zoom: s.Zoom, Walk: s.Walk, Keys: keys}
}

func toArenaBuild(b model.ArenaBuild) ArenaBuildResponse {
	out := ArenaBuildResponse{SkillBar: b.Bar, Traits: b.Traits}
	if out.Traits == nil {
		out.Traits = map[string]string{}
	}
	return out
}

func toPvpProfile(p model.PvpProfile) PvpProfileResponse {
	out := PvpProfileResponse{Standings: make([]PvpStandingResponse, 0, len(p.Standings)), Build: toArenaBuild(p.Build)}
	for _, s := range p.Standings {
		out.Standings = append(out.Standings, PvpStandingResponse{Mode: s.Mode, Shown: s.Shown, Rank: s.Rank, PlacementLeft: s.PlacementLeft, Wins: s.Wins, Losses: s.Losses, Draws: s.Draws, Place: s.Place})
	}
	return out
}

func toLeaderboard(mode string, rows []model.PvpRow) LeaderboardResponse {
	out := LeaderboardResponse{Mode: mode, Rows: make([]LeaderboardRowResponse, 0, len(rows))}
	for _, r := range rows {
		out.Rows = append(out.Rows, LeaderboardRowResponse{Place: r.Place, CharacterID: r.Character.String(), Name: r.Name, Class: r.Class, Shown: r.Shown, Rank: r.Rank, Wins: r.Wins, Losses: r.Losses, Draws: r.Draws})
	}
	return out
}
