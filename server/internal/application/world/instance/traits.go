package instance

import (
	"context"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// traitMsg asks to choose one of a skill's traits; done hears the outcome once it applies.
type traitMsg struct {
	id           kernel.CharacterID
	conn         string
	skill, trait string
	done         chan error
}

func (traitMsg) message() {}

// ChooseTrait sets the trait the player's skill is used with, and returns once the new sheet applies.
func (i *Instance) ChooseTrait(ctx context.Context, id kernel.CharacterID, conn, skill, trait string) error {
	m := traitMsg{id: id, conn: conn, skill: skill, trait: trait, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// chooseTrait refuses a dead player, a skill without points, a trait it doesn't have, an extra not open yet and a player in combat, in that order; otherwise it sets the trait, tells the player and saves at once.
func (i *Instance) chooseTrait(m traitMsg) {
	p, err := i.alive(m.id, m.conn)
	if err != nil {
		m.done <- err
		return
	}
	s, ok := i.data.Skill(m.skill)
	points := s.PointsIn(p.Skills.Points[m.skill])
	k, has := s.Trait(m.trait)
	switch {
	case !ok || points == 0 || !p.class.Learnable(s):
		err = model.ErrSkillNotLearned
	case !has:
		err = model.ErrTraitUnknown
	case k == 1 && !s.TraitOpen(points):
		err = model.ErrTraitLocked
	case i.inCombat(p):
		err = model.ErrInCombat
	}
	if err != nil || p.Skills.Traits[m.skill] == traitID(s, k) {
		m.done <- err
		return
	}
	if p.Skills.Traits == nil {
		p.Skills.Traits = map[string]string{}
	}
	if k == 0 {
		delete(p.Skills.Traits, m.skill)
	} else {
		p.Skills.Traits[m.skill] = m.trait
	}
	i.restat(p)
	i.checkpoint(p)
	m.done <- nil
}

// traitID is how the skills keep trait k of s: the default as "", which isn't listed.
func traitID(s gamedata.Skill, k int) string {
	if k == 0 {
		return ""
	}
	return s.Traits[k].ID
}

// inCombat is whether p fought within combat's length. A town has no fights, and a join starts out of combat, so a player there is never in it.
func (i *Instance) inCombat(p *player) bool { return i.tick < p.combatUntil }

// stampFights puts both sides of each of this tick's Damage events in combat, as they go out.
func (i *Instance) stampFights() {
	until := i.tick + i.ticksFor(i.data.Combat())
	for _, e := range i.events {
		d, ok := e.(model.Damage)
		if !ok {
			continue
		}
		for _, side := range []model.EntityRef{d.Source, d.Target} {
			if p := i.players[side.Player]; !side.IsMob() && p != nil {
				p.combatUntil = until
			}
		}
	}
}
