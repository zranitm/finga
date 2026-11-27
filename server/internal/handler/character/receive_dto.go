package character

import (
	model "finga/internal/model/gateway"
)

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
type CharacterResponse struct { //nolint:revive // named as its schema in api/openapi.yaml, which the drift test holds it to
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
