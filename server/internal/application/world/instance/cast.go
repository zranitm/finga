package instance

import (
	"cmp"
	"context"
	"maps"
	"math"
	"slices"
	"time"

	"finga/internal/application/world/mobs"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// castSlack is how early a skill may come before its cooldown ends or its action may start and still be used, so a client timer a tick ahead isn't refused; its cooldown and action count from the end, not the early use.
const castSlack = 150 * time.Millisecond

// skillMsg asks to use a skill; done hears the outcome once the tick resolves it.
type skillMsg struct {
	id    kernel.CharacterID
	conn  string
	in    model.SkillUse // the locked target is hit first when the shape reaches it; a moving skill's way is as the client sent it
	seq   uint32         // the input it was pressed during
	cost  int            // the energy it spends, set as it waits for its turn
	trait string         // the trait it was pressed with, set as it waits, so a change meanwhile can't alter what the client drew
	done  chan error
}

// wayTolerance is how far off unit length a way's vector may be, for the client's rounding.
const wayTolerance = 1e-6

// moveMark is a skill's move waiting for the first input stepped past the one its press came during, plus its delay, as the client starts it.
type moveMark struct {
	after  uint32
	way    sim.Way
	lock   uint64            // a blink at the locked mob's: the mob, which the world aims it behind as the frame the player saw left it
	reach  float64           // and the skill's distance, within which the mob must stand
	kind   gamedata.MoveKind // none for a break-free that only ends the hold
	steps  int               // a dash's or leap's sub-steps
	mobs   sim.Mobs
	breaks bool // a break-free's: it ends the hold first
}

// start lands a blink on s at once, or starts a dash or leap.
func (m moveMark) start(s sim.State, g sim.Geometry, p sim.Params) sim.State {
	if m.kind == gamedata.BlinkMove {
		return sim.Blink(s, m.way, g, p)
	}
	return sim.Dash(s, m.way, m.steps, m.mobs, m.kind == gamedata.LeapMove, p)
}

// pastRing is how far past the locked mob's far contact ring a blink at it aims, so it lands clear of the body behind it however the sums round.
const pastRing = 0.05

// behind is a blink's way from s to pastRing beyond b's far side, b the locked mob's body, when it stands within reach of s; else way, as the client sent it. A foe within reach may put the landing past it.
func behind(s sim.State, b sim.Blocker, radius, reach float64, way sim.Way) sim.Way {
	dx, dz := b.X-s.X, b.Z-s.Z
	d := math.Hypot(dx, dz)
	if d == 0 || d > reach {
		return way
	}
	return sim.Way{X: dx / d, Z: dz / d, Length: d + b.Radius + radius + pastRing}
}

// mobsOf is how mv meets mobs' bodies.
func mobsOf(mv gamedata.Move) sim.Mobs {
	switch {
	case mv.Through:
		return sim.ThroughMobs
	case mv.Stop:
		return sim.StopAtMob
	}
	return sim.SlideOnMobs
}

// dodging is whether a skill's dodge runs at tick.
func (p *player) dodging(tick uint64) bool { return p.dodgeFrom <= tick && tick < p.dodgeUntil }

// edgeSlack lets a blow along a block's edge count as in front: the aim comes as a float32, a ten-millionth of a radian off, and a 180° arc's edge is a dot product of 0.
const edgeSlack = 1e-6

// guards is whether p's block stops a blow landing along (dx, dz) at tick: its guard runs, and the blow comes from the front, the other way, within the arc round its aim.
func (p *player) guards(tick uint64, dx, dz, front float64) bool {
	return p.blockFrom <= tick && tick < p.blockUntil && -(p.blockX*dx+p.blockZ*dz) >= front-edgeSlack
}

func (skillMsg) message() {}

// buff is a buff skill's bonus on a player, in the units items use, until a tick, and its shield's pool left.
type buff struct {
	until  uint64
	bonus  progression.ItemStats
	points int    // what it was used with, which a checkpoint keeps to give the bonus again
	trait  string // as points: the trait it was used with
	shield int
	from   kernel.CharacterID // who used it, for the sheet
}

// UseSkill uses one of the player's active skills, pressed during input seq, and returns once the tick resolves it.
func (i *Instance) UseSkill(ctx context.Context, id kernel.CharacterID, conn string, in model.SkillUse, seq uint32) error {
	m := skillMsg{id: id, conn: conn, in: in, seq: seq, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// castSkills takes this tick's skill uses, refusing at once those that can't go, and uses each waiting one once its player's next skill may start: at a basic attack's cancel point, or as the last skill ends. Running before the attacks, it goes ahead of a held attack.
func (i *Instance) castSkills() {
	for _, m := range i.casts {
		u, err := i.usable(m, i.traitNow(m))
		if err == nil {
			err = i.chainReady(u, m.in.Root) // judged at the press, not as its turn comes
		}
		switch {
		case err != nil:
			m.done <- err
		case u.p.waitingCast != nil:
			m.done <- model.ErrSkillWaiting // one waits for the turn at a time
		default:
			waiting := m
			waiting.seq, waiting.cost, waiting.trait = u.p.pressedDuring(m.seq), u.spec.Energy, u.trait
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
		u, err := i.usable(*m, m.trait) // refuses one whose cooldown ends past the slack
		if err == nil {
			i.cast(u, max(start, u.ready, i.tick)) // the action counts from when both the last action and the cooldown allow it
		}
		m.done <- err
	}
}

// use is a skill use that can go: who, the skill, the stage its points make, and what its trait makes of it.
type use struct {
	p      *player
	s      gamedata.Skill
	points int
	stage  int
	trait  string          // the extra's id once it's open and chosen, else "" for the default
	spec   gamedata.Stage  // the stage with the trait, which the use resolves with
	ready  uint64          // the tick its last cooldown ends
	seq    uint32          // the input it was pressed during
	aim    float32         // the yaw it aims along
	target model.EntityRef // the lock it hits first when its shape reaches it
	way    sim.Way         // a moving skill's, its length held to the skill's distance
}

// traitNow is the trait m's player has chosen for its skill now.
func (i *Instance) traitNow(m skillMsg) string {
	if p := i.players[m.id]; p != nil {
		return p.Skills.Traits[m.in.Skill]
	}
	return ""
}

// usable is the use m asks for with trait, or why it can't go: dead, not learned, passive, damaging or a status in a town, held from acting or silenced, cooling down, short of energy, a moving skill's way not a unit vector and a length, or a heal with no one hurt to heal. A waiting use asks again at its start, as a death or the energy can change meanwhile, with the trait it was pressed with.
func (i *Instance) usable(m skillMsg, trait string) (use, error) {
	p, err := i.alive(m.id, m.conn)
	if err != nil {
		return use{}, err
	}
	if !i.fighting() {
		return use{}, model.ErrMatchNotStarted
	}
	s, ok := i.data.Skill(m.in.Skill)
	points := s.PointsIn(p.Skills.Points[m.in.Skill])
	switch {
	case !ok || points == 0 || !p.class.Learnable(s):
		return use{}, model.ErrSkillNotLearned
	case s.Kind == gamedata.Passive:
		return use{}, model.ErrSkillPassive
	case (s.Effect == gamedata.DamageEffect || s.Effect == gamedata.StatusEffect) && i.m.TownAt(p.state.X, p.state.Z):
		return use{}, model.ErrNotInTown
	case s.BreaksControl && !p.statuses.Hard(): // no slack: a hold that just ended leaves nothing to break
		return use{}, model.ErrNotControlled
	case !s.BreaksControl && p.statuses.Blocks(gamedata.BlocksAct):
		return use{}, model.ErrCantAct
	case !s.BreaksControl && p.statuses.Blocks(gamedata.BlocksSkills):
		return use{}, model.ErrSilenced
	}
	if k, ok := s.Trait(trait); !ok || k == 0 || !s.TraitOpen(points) {
		trait = ""
	}
	u := use{p: p, s: s, points: points, stage: s.StageAt(points), trait: trait, spec: s.Spec(points, trait), ready: p.skillReady[m.in.Skill], seq: m.seq, aim: m.in.Aim, target: m.in.Target}
	st := u.spec
	switch {
	case u.ready > i.tick+i.ticksFor(castSlack):
		return use{}, model.ErrSkillCoolingDown
	case p.Energy < st.Energy:
		return use{}, model.ErrNotEnoughEnergy
	}
	if st.Move != (gamedata.Move{}) {
		w := m.in.Way
		if !(w.Length >= 0) || !(math.Abs(math.Hypot(w.X, w.Z)-1) <= wayTolerance) {
			return use{}, model.ErrBadSkillWay
		}
		u.way = sim.Way{X: w.X, Z: w.Z, Length: min(w.Length, st.Move.Distance)}
	}
	if s.Effect == gamedata.HealEffect && len(i.hurtInReach(p, st)) == 0 {
		return use{}, model.ErrHPFull
	}
	return u, nil
}

// cast spends the use's energy, starts its cooldown and its action at start, and lands its effect at the action's hit.
func (i *Instance) cast(u use, start uint64) {
	p, s, st := u.p, u.s, u.spec
	p.Energy -= st.Energy
	p.spendAhead(u.seq, st.Energy)
	p.skillReady[s.ID] = start + model.SkillCooldownTicks(st.Cooldown, p.derived.CooldownSpeed, i.tuning.TickInterval)
	p.aimAt(u.aim)
	i.events = append(i.events, model.SkillUsed{Tick: i.tick, Caster: p.ID, Skill: s.ID, Stage: u.stage, Trait: u.trait, Aim: u.aim, Target: u.target})
	length, hit := model.TicksIn(st.Length, i.tuning.TickInterval), i.effect(u)
	i.seenStarting(p, start, length, st.Timing, st.Cancelable, s.Effect == gamedata.DamageEffect || s.Effect == gamedata.StatusEffect, st.Shape, u.target, st.Move)
	i.begin(p, skillAction, start, length, st.Timing, st.Cancelable, hit)
	hitAt, _ := model.ActionPoints(length, st.Hit, st.Cancel)
	p.hitSeqs[s.ID] = uint64(u.seq) + hitAt
	if h := st.Hits; h.Count > 1 { // the repeats, each from where the player stands and aims then
		for k := 1; k < h.Count; k++ {
			p.windUps = append(p.windUps, windUp{at: start + hitAt + i.ticksFor(time.Duration(k)*h.Every), land: hit})
		}
	}
	if mv := st.Move; mv != (gamedata.Move{}) || s.BreaksControl {
		after := u.seq
		if mv.Delay > 0 { // ticksFor holds any time to a tick at least
			after += uint32(i.ticksFor(mv.Delay)) //nolint:gosec // a delay's ticks are few
		}
		p.moveMark = &moveMark{after: after, way: u.way, kind: mv.Kind, steps: int(i.ticksFor(mv.Time)) * i.subSteps, mobs: mobsOf(mv), breaks: s.BreaksControl} //nolint:gosec // a move's ticks are few
		if mv.Kind == gamedata.BlinkMove && mv.Toward == gamedata.TowardLock && u.target.Player.IsZero() {
			p.moveMark.lock, p.moveMark.reach = u.target.Mob, mv.Distance
		}
	}
	if st.Dodge > 0 {
		p.dodgeFrom, p.dodgeUntil = start, start+i.ticksFor(st.Dodge)
		p.changed = true // the Delta carries a dodge from now
	}
	if st.Block > 0 { // it faces the aim at the press and doesn't turn as the player moves
		p.blockFrom, p.blockUntil, p.blockGave = start, start+i.ticksFor(st.Block), false
		p.blockX, p.blockZ = model.HeadingOf(u.aim)
	}
}

// effect lands the use with the power it starts with, from where the caster stands as it lands: a heal finds the hurt then, and a buff lasts from then.
func (i *Instance) effect(u use) func() {
	p, s, st := u.p, u.s, u.spec
	power := st.Power.At(u.points)
	switch s.Effect {
	case gamedata.DamageEffect:
		attacker := model.PlayerAttacker(p.derived, st.Damage).Times(power)
		return func() {
			i.strike(p, u.target, mobs.Strike{Shape: st.Shape, Most: st.Targets, Power: attacker, Push: st.Push}, st.Damage, st.Status.At(u.points))
		}
	case gamedata.HealEffect:
		amount := int(math.Round(p.derived.MagicAttack * power))
		return func() {
			for _, t := range i.hurtInReach(p, st) {
				t.HP = min(t.HP+amount, t.derived.MaxHP)
				t.changed = true
			}
		}
	case gamedata.BuffEffect:
		bonus := progression.BonusAt(st.Bonus, u.points)
		return func() {
			pool := 0
			if st.Shield != (gamedata.Shield{}) {
				pool = int(math.Round(p.derived.MagicAttack * st.Shield.Power.At(u.points)))
			}
			for _, t := range i.buffed(p, st) {
				t.buffs[s.ID] = buff{until: i.tick + i.ticksFor(st.Duration), bonus: bonus, points: u.points, trait: u.trait, shield: pool, from: p.ID}
				i.restat(t)
			}
		}
	case gamedata.StatusEffect:
		return func() { i.afflict(p, st, u.points, u.target) }
	default: // a move skill's move starts with its action, its hit adding nothing; usable refuses a passive
		return func() {}
	}
}

// buffed are who a buff reaches: the caster, and with reach_party each ally within its radius in 3D, as a heal measures it.
func (i *Instance) buffed(p *player, st gamedata.Stage) []*player {
	if !st.ReachParty {
		return []*player{p}
	}
	out := []*player{p}
	middle := p.state.Y + i.params.Height/2
	for _, a := range i.allies(p) {
		if model.Gap(p.state.X, middle, p.state.Z, i.bodyOf(a)) <= st.Shape.Radius {
			out = append(out, a)
		}
	}
	return out
}

// shielded is what of a mob's damage gets past p's shields, which take it first, in their skills' order.
func (p *player) shielded(damage int) int {
	for _, id := range slices.Sorted(maps.Keys(p.buffs)) {
		b := p.buffs[id]
		took := min(b.shield, damage)
		b.shield, damage = b.shield-took, damage-took
		p.buffs[id] = b
	}
	return damage
}

// shield is what's left of p's shields.
func (p *player) shield() int {
	sum := 0
	for _, b := range p.buffs {
		sum += b.shield
	}
	return sum
}

// hurtInReach are the hurt a heal reaches, up to its targets: the caster, then the nearest allies within its radius in 3D.
func (i *Instance) hurtInReach(p *player, st gamedata.Stage) []*player {
	hurt := func(t *player) bool { return t.HP < t.derived.MaxHP }
	var reached []*player
	if hurt(p) {
		reached = append(reached, p)
	}
	type ally struct {
		p   *player
		gap float64
	}
	var allies []ally
	middle := p.state.Y + i.params.Height/2
	for _, a := range i.allies(p) {
		if gap := model.Gap(p.state.X, middle, p.state.Z, i.bodyOf(a)); hurt(a) && gap <= st.Shape.Radius {
			allies = append(allies, ally{a, gap})
		}
	}
	slices.SortStableFunc(allies, func(x, y ally) int { return cmp.Compare(x.gap, y.gap) })
	for _, a := range allies[:min(len(allies), max(0, st.Targets-len(reached)))] {
		reached = append(reached, a.p)
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
			t.Buffs[s] = model.BuffTimer{Until: now.Add(time.Duration(b.until-i.tick) * i.tuning.TickInterval), Points: b.points, Trait: b.trait, Shield: b.shield, From: b.from}
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
		bonus, ok := buffBonus(i.data, s, b.Points, b.Trait)
		if left := b.Until.Sub(now); ok && left > 0 {
			p.buffs[s] = buff{until: i.tick + i.ticksFor(left), bonus: bonus, points: b.Points, trait: b.Trait, shield: b.Shield, from: b.From}
		}
	}
	p.Timers = model.SkillTimers{}
}

// BuffsOf is the bonus of the buffs in t still running at now, each at the points and trait it was used with.
func BuffsOf(data Content, t model.SkillTimers, now time.Time) progression.ItemStats {
	var sum progression.ItemStats
	for s, b := range t.Buffs {
		if bonus, ok := buffBonus(data, s, b.Points, b.Trait); ok && b.Until.After(now) {
			sum = sum.Plus(bonus)
		}
	}
	return sum
}

// BuffViewsOf are the buffs in t still running at now, by skill, as self's sheet lists them: self's own name no giver.
func BuffViewsOf(t model.SkillTimers, self kernel.CharacterID, now time.Time) []model.BuffView {
	var out []model.BuffView
	for _, s := range slices.Sorted(maps.Keys(t.Buffs)) {
		b := t.Buffs[s]
		if left := b.Until.Sub(now); left > 0 {
			v := model.BuffView{Skill: s, Left: left, Points: b.Points, From: b.From}
			if b.From == self {
				v.From = kernel.CharacterID{}
			}
			out = append(out, v)
		}
	}
	return out
}

// buffBonus is the bonus a buff skill gives at points with trait, from its stage then.
func buffBonus(data Content, skill string, points int, trait string) (progression.ItemStats, bool) {
	s, ok := data.Skill(skill)
	if !ok {
		return progression.ItemStats{}, false
	}
	return progression.BonusAt(s.Spec(points, trait).Bonus, points), true
}

// buffed is the sum of the player's buffs.
func (p *player) buffed() progression.ItemStats {
	var sum progression.ItemStats
	for _, b := range p.buffs {
		sum = sum.Plus(b.bonus)
	}
	return sum
}
