package account

import (
	"context"

	model "finga/internal/model/account"
)

// SpendPoints adds points through the world, which answers once the new stats apply; StatsChanged comes on its stream, before or after the answer.
func (g *Commands) SpendPoints(ctx context.Context, s *Connection, in model.PointsSpend) error {
	return g.deps.World.SpendPoints(ctx, s.Presence(), in)
}

// SpendSkillPoints invests skill points through the world, which answers once the new sheet applies; StatsChanged comes on its stream, before or after the answer.
func (g *Commands) SpendSkillPoints(ctx context.Context, s *Connection, in model.SkillPointsSpend) error {
	return g.deps.World.SpendSkillPoints(ctx, s.Presence(), in)
}

// ChooseTrait sets a skill's trait through the world, which answers once the new sheet applies; StatsChanged comes on its stream, before or after the answer.
func (g *Commands) ChooseTrait(ctx context.Context, s *Connection, in model.TraitChoice) error {
	return g.deps.World.ChooseTrait(ctx, s.Presence(), in)
}

// UseSkill uses an active skill through the world, which answers once its tick resolved it; SkillUsed, Damage and StatsChanged come on its stream, before or after the answer.
func (g *Commands) UseSkill(ctx context.Context, s *Connection, in model.SkillUse, seq uint32) error {
	return g.deps.World.UseSkill(ctx, s.Presence(), in, seq)
}

// ResetPoints returns every spent point to unspent with the free reset, through the world.
func (g *Commands) ResetPoints(ctx context.Context, s *Connection, expectedUnspent int) error {
	return g.deps.World.ResetPoints(ctx, s.Presence(), expectedUnspent)
}
