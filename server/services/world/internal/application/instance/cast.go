package instance

import (
	"context"
	"maps"
	"math"
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

// castSkills takes this tick's skill uses, refusing at once those that can't go, and uses each waiting one once its player's action turn comes. A skill takes the turn of the next basic attack, so one used during the attack's cooldown goes as it ends; running before the attacks, it goes ahead of a held attack.
func (i *Instance) castSkills() {
	for _, m := range i.casts {
		u, err := i.usable(m)
		switch {
		case err != nil:
			m.done <- err
		case u.p.waitingCast != nil:
			m.done <- model.ErrSkillCoolingDown // one waits for the turn at a time
		default:
			waiting := m
			u.p.waitingCast = &waiting
		}
	}
	clear(i.casts)
	i.casts = i.casts[:0]
	for _, id := range i.order {
		p := i.players[id]
		m := p.waitingCast
		if m == nil || p.cooldownUntil > i.tick+i.ticksFor(castSlack) {
			continue
		}
		p.waitingCast = nil
		u, err := i.usable(*m)
		if err == nil {
			i.cast(u)
		}
		m.done <- err
	}
}

// use is a skill use that can go: who, the skill, and the stage its points make.
type use struct {
	p      *player
	s      gamedata.Skill
	points int
	stage  int
	ready  uint64    // the tick its last cooldown ends
	healed []*player // a heal's targets
}

// usable is the use m asks for, or why it can't go: dead, not learned, passive, damaging in a town, cooling down, short of MP, or a heal with no one hurt to heal.
func (i *Instance) usable(m skillMsg) (use, error) {
	p, err := i.alive(m.id, m.conn)
	if err != nil {
		return use{}, err
	}
	s, ok := i.data.Skill(m.skill)
	points := p.Skills.Points[m.skill]
	switch {
	case !ok || points == 0 || !p.class.Learnable(s):
		return use{}, model.ErrSkillNotLearned
	case s.Kind == gamedata.Passive:
		return use{}, model.ErrSkillPassive
	case s.Effect == gamedata.DamageEffect && i.m.Kind == gamedata.Town:
		return use{}, model.ErrNotInTown
	}
	u := use{p: p, s: s, points: points, stage: s.StageAt(points), ready: p.skillReady[m.skill]}
	st := s.Stages[u.stage]
	switch {
	case u.ready > i.tick+i.ticksFor(castSlack):
		return use{}, model.ErrSkillCoolingDown
	case p.MP < st.MPAt(points):
		return use{}, model.ErrNotEnoughMP
	}
	if s.Effect == gamedata.HealEffect {
		if u.healed = i.hurtInReach(p, st); len(u.healed) == 0 {
			return use{}, model.ErrHPFull
		}
	}
	return u, nil
}

// cast spends the use's MP, starts its cooldown and the next attack's, and lands its effect.
func (i *Instance) cast(u use) {
	p, s, st := u.p, u.s, u.s.Stages[u.stage]
	p.MP -= st.MPAt(u.points)
	if p.skillReady == nil {
		p.skillReady = map[string]uint64{}
	}
	p.skillReady[s.ID] = max(u.ready, i.tick) + model.SkillCooldownTicks(st.Cooldown, p.derived.CooldownSpeed, i.tuning.TickInterval)
	p.cooldownUntil = max(p.cooldownUntil, i.tick) + model.AttackCooldownTicks(p.class.Attack.Cooldown, p.derived.AttackSpeed, i.tuning.TickInterval)
	i.events = append(i.events, model.SkillUsed{Tick: i.tick, Caster: p.ID, Skill: s.ID, Stage: u.stage, Facing: p.state.Facing})
	power := st.Power.At(u.points)
	switch s.Effect {
	case gamedata.DamageEffect:
		i.strike(p, st.Shape, st.Targets, model.PlayerAttacker(p.derived, st.Damage).Times(power))
	case gamedata.HealEffect:
		amount := int(math.Round(p.derived.MagicAttack * power))
		for _, t := range u.healed {
			t.HP = min(t.HP+amount, t.derived.MaxHP)
			t.changed = true
		}
	case gamedata.BuffEffect:
		if p.buffs == nil {
			p.buffs = map[string]buff{}
		}
		p.buffs[s.ID] = buff{until: i.tick + i.ticksFor(st.Duration), bonus: progression.BonusAt(st.Bonus, u.points)}
		i.restat(p) // its StatsChanged carries the MP spent
		return
	}
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
}

// hurtInReach are those below full HP a heal reaches, up to its targets: the caster if hurt, then the nearest hurt party members alive on the map in its shape. Those at full HP take no place.
func (i *Instance) hurtInReach(p *player, st gamedata.Stage) []*player {
	hurt := func(t *player) bool { return t.HP < t.derived.MaxHP }
	var reached, allies []*player
	if hurt(p) {
		reached = append(reached, p)
	}
	var boxes []model.Target
	for _, a := range i.present(p, i.deps.Parties.Party(p.ID))[1:] {
		if hurt(a) {
			allies = append(allies, a)
			boxes = append(boxes, model.Target{Box: i.playerBox(a)})
		}
	}
	for _, k := range model.Reached(p.state.X, p.state.Y, p.state.Facing, i.params.Height, st.Shape, boxes, st.Targets-len(reached)) {
		reached = append(reached, allies[k])
	}
	return reached
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
