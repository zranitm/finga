package api

import (
	"time"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/application/character"
)

// The request and response bodies of api/openapi.yaml. A drift test keeps
// their JSON fields equal to the schemas there.

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

// CreateCharacterRequest is POST /api/characters.
type CreateCharacterRequest struct {
	Name  string `json:"name"`
	Class string `json:"class"`
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
	MP    int           `json:"mp"`
	MapID string        `json:"map_id"`
}

// CharactersResponse is GET /api/characters.
type CharactersResponse struct {
	Characters []CharacterResponse `json:"characters"`
}

// SettingsRequest is PUT /api/settings; SettingsResponse is what both
// settings endpoints return.
type SettingsRequest struct {
	ControlPreset string `json:"control_preset"`
}

// SettingsResponse is the account's settings.
type SettingsResponse struct {
	ControlPreset string `json:"control_preset"`
}

// ErrorResponse is every error body: a code the client can branch on and a
// message safe to show the player.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func toAccount(a *accountv1.Account) AccountResponse {
	return AccountResponse{ID: a.GetId(), Username: a.GetUsername(), CreatedAt: a.GetCreatedAt().AsTime()}
}

func toCharacter(c character.Character) CharacterResponse {
	return CharacterResponse{
		ID: c.ID.String(), Name: c.Name, Class: c.Class.String(), Level: c.Level, Exp: c.Exp,
		Stats: StatsResponse{STR: c.Stats.STR, DEX: c.Stats.DEX, INT: c.Stats.INT, AGI: c.Stats.AGI, VIT: c.Stats.VIT},
		HP:    c.HP, MP: c.MP, MapID: c.MapID,
	}
}

func toCharacters(cs []character.Character) CharactersResponse {
	out := CharactersResponse{Characters: make([]CharacterResponse, 0, len(cs))}
	for _, c := range cs {
		out.Characters = append(out.Characters, toCharacter(c))
	}
	return out
}

func toSettings(s *accountv1.Settings) SettingsResponse {
	return SettingsResponse{ControlPreset: s.GetControlPreset()}
}
