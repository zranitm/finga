// Package pvp adapts the modules the pvp module calls: the world's matches.
package pvp

import (
	"context"

	"finga/internal/application/world"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// WorldModule is the world module as the pvp module calls it.
type WorldModule interface {
	CanFight(ctx context.Context, id kernel.CharacterID) error
	OpenMatch(ctx context.Context, spec world.MatchSpec) error
	FollowResults(ctx context.Context, deliver func(world.MatchResult)) error
}

// World is the world module as the matcher's port.
type World struct{ w WorldModule }

// NewWorld adapts w.
func NewWorld(w WorldModule) World { return World{w: w} }

// CanFight is nil when the character could go into a match now.
func (w World) CanFight(ctx context.Context, id kernel.CharacterID) error {
	return w.w.CanFight(ctx, id)
}

// OpenMatch opens spec's match.
func (w World) OpenMatch(ctx context.Context, spec model.Spec) error {
	out := world.MatchSpec{ID: spec.ID, Mode: spec.Mode}
	for k, side := range spec.Sides {
		for _, p := range side {
			f := world.Fighter{Character: p.Character}
			if b := p.Bot; b != nil {
				f.Bot = &world.Bot{ID: b.ID, Name: b.Name, Class: b.Class, Tier: b.Tier, Bar: b.Bar}
			}
			out.Sides[k] = append(out.Sides[k], f)
		}
	}
	return w.w.OpenMatch(ctx, out)
}

// FollowResults hands deliver each match's outcome until ctx ends.
func (w World) FollowResults(ctx context.Context, deliver func(model.Result)) error {
	return w.w.FollowResults(ctx, func(r world.MatchResult) {
		out := model.Result{Match: r.Match, Mode: r.Mode, Started: r.Started, Ended: r.Ended, Winner: r.Winner, Wins: r.Wins}
		for k, side := range r.Sides {
			for _, f := range side {
				out.Sides[k] = append(out.Sides[k], model.Outcome(f))
			}
		}
		deliver(out)
	})
}
