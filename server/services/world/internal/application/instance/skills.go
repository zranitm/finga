package instance

import (
	"context"
	"fmt"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/services/world/internal/model"
)

// skillPointsMsg asks to invest skill points; done hears the outcome once it applies.
type skillPointsMsg struct {
	id       kernel.CharacterID
	conn     string
	expected int // the unspent count the client saw
	points   map[string]int
	done     chan error
}

func (skillPointsMsg) message() {}

// SpendSkillPoints invests the player's unspent skill points in skills their class can learn, if expected is still that count, and returns once the new sheet applies.
func (i *Instance) SpendSkillPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points map[string]int) error {
	m := skillPointsMsg{id: id, conn: conn, expected: expected, points: points, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// settleSkillPoints invests on the tick, then re-derives, since passives add to the stats, tells the player and saves at once.
func (i *Instance) settleSkillPoints(m skillPointsMsg) {
	p, ok := i.players[m.id]
	if !ok || p.Connection != m.conn {
		m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
		return
	}
	learnable := func(id string) bool {
		s, ok := i.data.Skill(id)
		return ok && p.class.Learnable(s)
	}
	skills, err := invest(p.Skills, m.expected, m.points, learnable)
	if err != nil {
		m.done <- err
		return
	}
	p.Skills = skills
	i.restat(p)
	i.checkpoint(p)
	m.done <- nil
}

// invest moves skill points from unspent into skills; none negative, at least one, each skill learnable, out of the count the client saw.
func invest(sk model.Skills, expected int, add map[string]int, learnable func(string) bool) (model.Skills, error) {
	total := 0
	for id, n := range add {
		switch {
		case n < 0:
			return sk, model.ErrBadSkillPoints
		case n > 0 && !learnable(id):
			return sk, model.ErrSkillNotLearnable
		}
		total += n
	}
	switch {
	case total == 0:
		return sk, model.ErrBadSkillPoints
	case expected != sk.Unspent:
		return sk, model.ErrSkillPointsChanged
	case total > sk.Unspent:
		return sk, model.ErrNotEnoughSkillPoints
	}
	out := sk.Clone()
	if out.Points == nil {
		out.Points = map[string]int{}
	}
	for id, n := range add {
		if n > 0 {
			out.Points[id] += n
		}
	}
	out.Unspent -= total
	return out, nil
}

// learned are the skills with points that the class can learn, for the stats their passives give.
func learned(data Content, class gamedata.Class, skills model.Skills) []progression.Learned {
	out := make([]progression.Learned, 0, len(skills.Points))
	for id, points := range skills.Points {
		if s, ok := data.Skill(id); ok && class.Learnable(s) {
			out = append(out, progression.Learned{Skill: s, Points: points})
		}
	}
	return out
}
