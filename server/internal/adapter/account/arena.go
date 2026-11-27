package account

import (
	"context"

	"finga/internal/application/pvp"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// ArenaModule is the pvp module as the REST API calls it: profiles, arena builds and boards.
type ArenaModule interface {
	Profile(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) (pvp.Profile, error)
	SetBuild(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, bar []string, traits map[string]string) error
	Leaderboard(ctx context.Context, mode string) ([]pvp.Row, error)
}

// Arena is the application's pvp profiles, builds and boards.
type Arena struct{ p ArenaModule }

// NewArena adapts p.
func NewArena(p ArenaModule) Arena { return Arena{p: p} }

// Profile is one of the account's characters' standing in each rated mode, and their arena build.
func (a Arena) Profile(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) (model.PvpProfile, error) {
	pr, err := a.p.Profile(ctx, account, id)
	if err != nil {
		return model.PvpProfile{}, wrap(err)
	}
	out := model.PvpProfile{Standings: make([]model.PvpStanding, 0, len(pr.Standings)), Build: model.ArenaBuild{Bar: pr.Build.Bar, Traits: pr.Build.Traits}}
	for _, s := range pr.Standings {
		out.Standings = append(out.Standings, model.PvpStanding(s))
	}
	return out, nil
}

// SetBuild saves the arena build of one of the account's characters.
func (a Arena) SetBuild(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, b model.ArenaBuild) error {
	return wrap(a.p.SetBuild(ctx, account, id, b.Bar, b.Traits))
}

// Leaderboard is a rated mode's board.
func (a Arena) Leaderboard(ctx context.Context, mode string) ([]model.PvpRow, error) {
	rows, err := a.p.Leaderboard(ctx, mode)
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]model.PvpRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.PvpRow{Place: r.Place, Character: r.Character, Name: r.Name, Class: string(r.Class), Shown: r.Shown, Rank: r.Rank, Wins: r.Wins, Losses: r.Losses, Draws: r.Draws})
	}
	return out, nil
}
