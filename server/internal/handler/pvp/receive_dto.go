package pvp

import (
	model "finga/internal/model/gateway"
)

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
type PvpStandingResponse struct { //nolint:revive // named as its schema in api/openapi.yaml, which the drift test holds it to
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
type PvpProfileResponse struct { //nolint:revive // named as its schema in api/openapi.yaml, which the drift test holds it to
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
