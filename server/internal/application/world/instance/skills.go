package instance

import (
	"context"
	"fmt"
	"maps"
	"slices"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/transient"
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
	open := func(id string) error {
		s, ok := i.data.Skill(id)
		switch {
		case !ok || !p.class.Learnable(s):
			return model.ErrSkillNotLearnable
		case p.Level < s.Level:
			return model.ErrSkillLocked
		}
		return nil
	}
	skills, err := invest(p.Skills, m.expected, m.points, open)
	if err != nil {
		m.done <- err
		return
	}
	p.Skills = skills
	i.restat(p)
	i.checkpoint(p)
	m.done <- nil
}

// invest moves skill points from unspent into skills; none negative, at least one, each skill open to the character, out of the count the client saw.
func invest(sk model.Skills, expected int, add map[string]int, open func(string) error) (model.Skills, error) {
	total := 0
	for _, id := range slices.Sorted(maps.Keys(add)) { // the same refusal however the map is ordered
		n := add[id]
		if n < 0 {
			return sk, model.ErrBadSkillPoints
		}
		if n > 0 {
			if err := open(id); err != nil {
				return sk, err
			}
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
