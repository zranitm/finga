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

// castSlack is how early a skill may come before its cooldown ends or its action may start and still be used, so a client timer a tick ahead isn't refused; its cooldown and action count from the end, not the early use.
const castSlack = 150 * time.Millisecond

// skillMsg asks to use a skill; done hears the outcome once the tick resolves it.
type skillMsg struct {
	id    kernel.CharacterID
	conn  string
	skill string
	seq   uint32 // the input it was pressed during
	done  chan error
}

func (skillMsg) message() {}

// buff is a buff skill's bonus on a player, in the units items use, until a tick.
type buff struct {
	until  uint64
	bonus  progression.ItemStats
	points int // what it was used with, which a checkpoint keeps to give the bonus again
}

// UseSkill uses one of the player's active skills, pressed during input seq, and returns once the tick resolves it.
func (i *Instance) UseSkill(ctx context.Context, id kernel.CharacterID, conn, skill string, seq uint32) error {
	m := skillMsg{id: id, conn: conn, skill: skill, seq: seq, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// castSkills takes this tick's skill uses, refusing at once those that can't go, and uses each waiting one once its player's next skill may start: at a basic attack's cancel point, or as the last skill ends. Running before the attacks, it goes ahead of a held attack.
func (i *Instance) castSkills() {
	for _, m := range i.casts {
		u, err := i.usable(m)
		switch {
		case err != nil:
			m.done <- err
		case u.p.waitingCast != nil:
			m.done <- model.ErrSkillWaiting // one waits for the turn at a time
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
		start := p.action.nextStart(skillAction)
		if m == nil || start > i.tick+i.ticksFor(castSlack) {
			continue
		}
		p.waitingCast = nil
		u, err := i.usable(*m) // refuses one whose cooldown ends past the slack
		if err == nil {
			i.cast(u, max(start, u.ready, i.tick)) // the action counts from when both the last action and the cooldown allow it
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
	ready  uint64 // the tick its last cooldown ends
	seq    uint32 // the input it was pressed during
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
	u := use{p: p, s: s, points: points, stage: s.StageAt(points), ready: p.skillReady[m.skill], seq: m.seq}
	st := s.Stages[u.stage]
	switch {
	case u.ready > i.tick+i.ticksFor(castSlack):
		return use{}, model.ErrSkillCoolingDown
	case p.MP < st.MPAt(points):
		return use{}, model.ErrNotEnoughMP
	}
	if s.Effect == gamedata.HealEffect && len(i.hurtInReach(p, st)) == 0 {
		return use{}, model.ErrHPFull
	}
	return u, nil
}

// cast spends the use's MP, starts its cooldown and its action at start, casting from the input after its press, and lands its effect at the action's hit.
func (i *Instance) cast(u use, start uint64) {
	p, s, st := u.p, u.s, u.s.Stages[u.stage]
	p.MP -= st.MPAt(u.points)
	p.skillReady[s.ID] = start + model.SkillCooldownTicks(st.Cooldown, p.derived.CooldownSpeed, i.tuning.TickInterval)
	i.events = append(i.events, model.SkillUsed{Tick: i.tick, Caster: p.ID, Skill: s.ID, Stage: u.stage, Facing: p.state.Facing})
	length := model.TicksIn(st.Length, i.tuning.TickInterval)
	p.mark(u.seq, int(length)*i.subSteps) //nolint:gosec // lengths are small
	if i.begin(p, skillAction, start, length, st.Timing, st.Cancelable, i.effect(u)) && s.Effect == gamedata.BuffEffect {
		return // its restat's StatsChanged carries the MP spent
	}
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
}

// effect lands the use with the power it starts with, from where the caster stands as it lands: a heal finds the hurt then, and a buff lasts from then.
func (i *Instance) effect(u use) func() {
	p, s, st := u.p, u.s, u.s.Stages[u.stage]
	power := st.Power.At(u.points)
	switch s.Effect {
	case gamedata.DamageEffect:
		attacker := model.PlayerAttacker(p.derived, st.Damage).Times(power)
		return func() { i.strike(p, st.Shape, st.Targets, attacker) }
	case gamedata.HealEffect:
		amount := int(math.Round(p.derived.MagicAttack * power))
		return func() {
			for _, t := range i.hurtInReach(p, st) {
				t.HP = min(t.HP+amount, t.derived.MaxHP)
				t.changed = true
			}
		}
	default: // a buff: usable refuses a passive
		bonus := progression.BonusAt(st.Bonus, u.points)
		return func() {
			p.buffs[s.ID] = buff{until: i.tick + i.ticksFor(st.Duration), bonus: bonus, points: u.points}
			i.restat(p)
		}
	}
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

// timersOf is when p's running cooldowns and buffs end, taking this tick as now on the wall clock, for a checkpoint or the map p moves to.
func (i *Instance) timersOf(p *player, now time.Time) model.SkillTimers {
	t := model.SkillTimers{Cooldowns: map[string]time.Time{}, Buffs: map[string]model.BuffTimer{}}
	for s, ready := range p.skillReady {
		if ready > i.tick {
			t.Cooldowns[s] = now.Add(time.Duration(ready-i.tick) * i.tuning.TickInterval)
		}
	}
	for s, b := range p.buffs {
		if b.until > i.tick {
			t.Buffs[s] = model.BuffTimer{Until: now.Add(time.Duration(b.until-i.tick) * i.tuning.TickInterval), Points: b.points}
		}
	}
	return t
}

// startTimers counts on, from this map's tick, the cooldowns and buffs p joined with that are still running at now; a buff of a skill the data no longer has ends.
func (i *Instance) startTimers(p *player, now time.Time) {
	p.skillReady, p.buffs = map[string]uint64{}, map[string]buff{}
	for s, until := range p.Timers.Cooldowns {
		if left := until.Sub(now); left > 0 {
			p.skillReady[s] = i.tick + i.ticksFor(left)
		}
	}
	for s, b := range p.Timers.Buffs {
		bonus, ok := buffBonus(i.data, s, b.Points)
		if left := b.Until.Sub(now); ok && left > 0 {
			p.buffs[s] = buff{until: i.tick + i.ticksFor(left), bonus: bonus, points: b.Points}
		}
	}
	p.Timers = model.SkillTimers{}
}

// BuffsOf is the bonus of the buffs in t still running at now, each at the points it was used with.
func BuffsOf(data Content, t model.SkillTimers, now time.Time) progression.ItemStats {
	var sum progression.ItemStats
	for s, b := range t.Buffs {
		if bonus, ok := buffBonus(data, s, b.Points); ok && b.Until.After(now) {
			sum = sum.Plus(bonus)
		}
	}
	return sum
}

// buffBonus is the bonus a buff skill gives at points, from its stage then.
func buffBonus(data Content, skill string, points int) (progression.ItemStats, bool) {
	s, ok := data.Skill(skill)
	if !ok {
		return progression.ItemStats{}, false
	}
	return progression.BonusAt(s.Stages[s.StageAt(points)].Bonus, points), true
}

// buffed is the sum of the player's buffs.
func (p *player) buffed() progression.ItemStats {
	var sum progression.ItemStats
	for _, b := range p.buffs {
		sum = sum.Plus(b.bonus)
	}
	return sum
}
