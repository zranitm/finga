package instance

import (
	"context"
	"maps"
	"math"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/world/internal/model"
)

// castSlack is how early a skill may come before its cooldown ends and still be used, so a client timer a tick ahead isn't refused; the next cooldown counts from the end, not the early use.
const castSlack = 150 * time.Millisecond

// skillMsg asks to use a skill; done hears the outcome once the tick resolves it.
type skillMsg struct {
	id    kernel.CharacterID
	conn  string
	skill string
	done  chan error
}

func (skillMsg) message() {}

// buff is a buff skill's bonus on a player, in the units items use, until a tick.
type buff struct {
	until uint64
	bonus progression.ItemStats
}

// UseSkill uses one of the player's active skills and returns once the tick resolves it.
func (i *Instance) UseSkill(ctx context.Context, id kernel.CharacterID, conn, skill string) error {
	m := skillMsg{id: id, conn: conn, skill: skill, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// castSkills resolves this tick's skill uses in the order they came, after movement, as attacks are.
func (i *Instance) castSkills() {
	for _, m := range i.casts {
		m.done <- i.cast(m)
	}
	clear(i.casts)
	i.casts = i.casts[:0]
}

// cast uses the skill, or says why it can't: dead, not learned, passive, damaging in a town, cooling down, short of MP, or a heal with no one hurt to heal.
func (i *Instance) cast(m skillMsg) error {
	p, err := i.alive(m.id, m.conn)
	if err != nil {
		return err
	}
	s, ok := i.data.Skill(m.skill)
	points := p.Skills.Points[m.skill]
	switch {
	case !ok || points == 0 || !p.class.Learnable(s):
		return model.ErrSkillNotLearned
	case s.Kind == gamedata.Passive:
		return model.ErrSkillPassive
	case s.Effect == gamedata.DamageEffect && i.m.Kind == gamedata.Town:
		return model.ErrNotInTown
	}
	ready := p.skillReady[m.skill]
	if ready > i.tick+i.ticksFor(castSlack) {
		return model.ErrSkillCoolingDown
	}
	stage := s.StageAt(points)
	st := s.Stages[stage]
	cost := st.MPAt(points)
	if p.MP < cost {
		return model.ErrNotEnoughMP
	}
	var healed []*player
	if s.Effect == gamedata.HealEffect {
		if healed = i.hurtInReach(p, st); len(healed) == 0 {
			return model.ErrHPFull
		}
	}
	p.MP -= cost
	if p.skillReady == nil {
		p.skillReady = map[string]uint64{}
	}
	p.skillReady[m.skill] = max(ready, i.tick) + model.SkillCooldownTicks(st.Cooldown, p.derived.CooldownSpeed, i.tuning.TickInterval)
	i.events = append(i.events, model.SkillUsed{Tick: i.tick, Caster: p.ID, Skill: s.ID, Stage: stage, Facing: p.state.Facing})
	power := st.Power.At(points)
	switch s.Effect {
	case gamedata.DamageEffect:
		i.strike(p, st.Shape, st.Targets, model.PlayerAttacker(p.derived, st.Damage).Times(power))
	case gamedata.HealEffect:
		amount := int(math.Round(p.derived.MagicAttack * power))
		for _, t := range healed {
			t.HP = min(t.HP+amount, t.derived.MaxHP)
			t.changed = true
		}
	case gamedata.BuffEffect:
		if p.buffs == nil {
			p.buffs = map[string]buff{}
		}
		p.buffs[s.ID] = buff{until: i.tick + i.ticksFor(st.Duration), bonus: progression.BonusAt(st.Bonus, points)}
		i.restat(p) // its StatsChanged carries the MP spent
		return nil
	}
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
	return nil
}

// hurtInReach are those a heal reaches below full HP: the caster, and the party members alive on the map in its shape, up to its targets in all, nearest first.
func (i *Instance) hurtInReach(p *player, st gamedata.Stage) []*player {
	allies := i.present(p, i.deps.Parties.Party(p.ID))[1:]
	boxes := make([]model.Target, len(allies))
	for k, a := range allies {
		boxes[k] = model.Target{Box: i.playerBox(a)}
	}
	reached := []*player{p}
	for _, k := range model.Reached(p.state.X, p.state.Y, p.state.Facing, i.params.Height, st.Shape, boxes, st.Targets-1) {
		reached = append(reached, allies[k])
	}
	return slices.DeleteFunc(reached, func(t *player) bool { return t.HP >= t.derived.MaxHP })
}

// expireBuffs ends the buffs whose time is up and re-derives the stats without them.
func (i *Instance) expireBuffs() {
	for _, id := range i.order {
		p := i.players[id]
		n := len(p.buffs)
		maps.DeleteFunc(p.buffs, func(_ string, b buff) bool { return b.until <= i.tick })
		if len(p.buffs) < n {
			i.restat(p)
		}
	}
}

// buffed is the sum of the player's buffs.
func (p *player) buffed() progression.ItemStats {
	var sum progression.ItemStats
	for _, b := range p.buffs {
		sum = sum.Plus(b.bonus)
	}
	return sum
}
