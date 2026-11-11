package send

import (
	"context"

	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// SpendPoints adds points through the world, which answers once the new stats apply; StatsChanged comes on its stream, before or after the answer.
func (g *Gateway) SpendPoints(ctx context.Context, s *session.Session, in model.PointsSpend) error {
	return g.deps.World.SpendPoints(ctx, s.Presence(), in)
}

// ResetPoints returns every spent point to unspent with the free reset, through the world.
func (g *Gateway) ResetPoints(ctx context.Context, s *session.Session, expectedUnspent int) error {
	return g.deps.World.ResetPoints(ctx, s.Presence(), expectedUnspent)
}
