package instance

import (
	"context"
	"fmt"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/world/internal/model"
	"finga/pkg/transient"
)

// pointsMsg asks to spend points or, with spend nil, to reset them; done hears the outcome once it applies.
type pointsMsg struct {
	id       kernel.CharacterID
	conn     string
	expected int // the unspent count the client saw
	spend    *gamedata.Stats
	done     chan error
}

func (pointsMsg) message() {}

// SpendPoints adds points to the player's stats out of their unspent ones, if expected is still that count, and returns once the new stats apply.
func (i *Instance) SpendPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points gamedata.Stats) error {
	m := pointsMsg{id: id, conn: conn, expected: expected, spend: &points, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// ResetPoints returns every spent point to unspent with the free reset, if expected is still the unspent count, and returns once the new stats apply.
func (i *Instance) ResetPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int) error {
	m := pointsMsg{id: id, conn: conn, expected: expected, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// settlePoints spends or resets on the tick, then re-derives, tells the player and saves at once.
func (i *Instance) settlePoints(m pointsMsg) {
	p, ok := i.players[m.id]
	if !ok || p.Connection != m.conn {
		m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
		return
	}
	var err error
	if m.spend != nil {
		p.Points, err = spend(p.Points, m.expected, *m.spend)
	} else {
		p.Points, err = reset(p.Points, m.expected, i.data.StatRules().PointsPerLevel*(p.Level-1))
	}
	if err != nil {
		m.done <- err
		return
	}
	i.restat(p)
	i.checkpoint(p)
	m.done <- nil
}

// spend moves points from unspent into the stats; none negative, at least one, out of the count the client saw.
func spend(pts model.Points, expected int, add gamedata.Stats) (model.Points, error) {
	switch {
	case add.STR < 0 || add.DEX < 0 || add.INT < 0 || add.AGI < 0 || add.VIT < 0 || add.Total() == 0:
		return pts, model.ErrBadPoints
	case expected != pts.Unspent:
		return pts, model.ErrPointsChanged
	case add.Total() > pts.Unspent:
		return pts, model.ErrNotEnoughPoints
	}
	pts.Spent = progression.AddStats(pts.Spent, add)
	pts.Unspent -= add.Total()
	return pts, nil
}

// reset uses the free reset: every point the levels earned is unspent again.
func reset(pts model.Points, expected, earned int) (model.Points, error) {
	switch {
	case pts.FreeResetUsed:
		return pts, model.ErrNoFreeReset
	case expected != pts.Unspent:
		return pts, model.ErrPointsChanged
	}
	return model.Points{Unspent: earned, FreeResetUsed: true}, nil
}
