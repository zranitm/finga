package world_test

import (
	"cmp"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/progression"
)

// horizon is how long a rotation runs, in ticks: ten minutes.
const horizon = 10 * 60 * 20

// act is an action a rotation takes: its expected damage, its length and cancel point in ticks, and for a skill its cooldown and energy.
type act struct {
	damage           float64
	length, cancelAt uint64
	cooldown         uint64
	cost             int
	cancelable       bool
}

// rotation is damage a second three ways: basic attacks alone, skills on cooldown one action at a time, and a chain as players fight; and the share of the second way's damage its skills deal.
type rotation struct{ basic, skills, chain, skillShare float64 }

func (r rotation) gain() float64 { return r.chain/r.skills - 1 }

// planned is the skill points a character of class at level has, as the bots spend them: the promotion's skills up to what it asks, a point in each skill the class's line teaches at the level, the root's first, the rest in the main skill, the promotion's first before it and the class's first after.
func planned(d *gamedata.Data, class, future gamedata.Class, level int) map[string]int {
	left := (level - 1) * d.StatRules().SkillPointsPerLevel
	points := map[string]int{}
	for _, id := range slices.Sorted(maps.Keys(future.Promotion.Skills)) {
		n := min(left, future.Promotion.Skills[id])
		points[id] += n
		left -= n
	}
	for _, line := range slices.Backward(class.Line) {
		teacher, _ := d.Class(line)
		for _, id := range teacher.Skills {
			if s, _ := d.Skill(id); left > 0 && s.Level <= level && points[id] == 0 {
				points[id], left = 1, left-1
			}
		}
	}
	main := slices.Sorted(maps.Keys(future.Promotion.Skills))[0]
	if class.ID != d.StartClass().ID {
		main = class.Skills[0]
	}
	points[main] += left
	return points
}

// rotationOf is a character's rotation against a mob, with every damage skill it has points in, and the damage over time its buffs' hits inflict.
func rotationOf(d *gamedata.Data, me progression.Derived, c gamedata.Class, points map[string]int, m gamedata.Mob) rotation {
	length := model.TicksIn(c.Attack.Length, tickInterval)
	_, cancelAt := model.ActionPoints(length, c.Attack.Hit, c.Attack.Cancel)
	basic := act{damage: meanDamage(d, model.PlayerAttacker(me, c.Attack.Damage), m), length: length, cancelAt: cancelAt, cancelable: true}
	var skills []act
	var onHit float64
	for id, n := range points {
		s, _ := d.Skill(id)
		if n == 0 || s.Kind != gamedata.Active {
			continue
		}
		st := s.Stages[s.StageAt(n)]
		if s.Effect == gamedata.BuffEffect && st.OnHit.Status != "" {
			onHit += onHitDPS(d, me, c, st, n, m)
		}
		if s.Effect == gamedata.DamageEffect {
			skills = append(skills, actOf(d, me, st, n, m))
		}
	}
	slices.SortFunc(skills, func(a, b act) int { return cmp.Compare(b.damage, a.damage) }) // the strongest first
	e := d.Energy()
	return rotation{basic: dps(basic, nil, e), skills: dps(basic, skills, e) + onHit, chain: chain(basic, skills, e) + onHit, skillShare: skillShare(&basic, skills, e)}
}

// actOf is a damage skill's action at n points: each of its hits, and the damage over time it inflicts, ticking until it runs out or the skill can be used again to restart it.
func actOf(d *gamedata.Data, me progression.Derived, st gamedata.Stage, n int, m gamedata.Mob) act {
	l := model.TicksIn(st.Length, tickInterval)
	_, at := model.ActionPoints(l, st.Hit, st.Cancel)
	attacker := model.PlayerAttacker(me, st.Damage)
	damage := meanDamage(d, attacker.Times(st.Power.At(n)), m) * float64(max(st.Hits.Count, 1))
	if def, ok := d.Status(st.Status.Status); ok && def.Group == gamedata.Dot {
		cooldown := time.Duration(model.SkillCooldownTicks(st.Cooldown, me.CooldownSpeed, tickInterval)) * tickInterval
		ticks := min(st.Status.Time, max(cooldown, st.Length)) / def.Every
		damage += float64(ticks) * float64(model.Steady(attacker.Times(st.Status.Power.At(n)), model.MobDefender(m)))
	}
	return act{
		damage: damage, length: l, cancelAt: at, cancelable: st.Cancelable,
		cooldown: model.SkillCooldownTicks(st.Cooldown, me.CooldownSpeed, tickInterval),
		cost:     st.Energy,
	}
}

// onHitDPS is what a buff's damage over time adds a second while its user keeps hitting: each hit restarts it, so it ticks on its beat for the buff's share of its cooldown, with the class's damage type.
func onHitDPS(d *gamedata.Data, me progression.Derived, c gamedata.Class, st gamedata.Stage, n int, m gamedata.Mob) float64 {
	def, ok := d.Status(st.OnHit.Status)
	if !ok || def.Group != gamedata.Dot {
		return 0
	}
	tick := float64(model.Steady(model.PlayerAttacker(me, c.Attack.Damage).Times(st.OnHit.Power.At(n)), model.MobDefender(m)))
	uptime := min(1, st.Duration.Seconds()/max(st.Cooldown.Seconds(), st.Duration.Seconds()))
	return tick / def.Every.Seconds() * uptime
}

// meanDamage is an action's damage on the mob, misses included, from a fixed seed.
func meanDamage(d *gamedata.Data, a model.Attacker, m gamedata.Mob) float64 {
	r := rand.New(rand.NewPCG(3, 4))
	var sum float64
	for range attacks {
		sum += float64(model.Resolve(a, model.MobDefender(m), d.StatRules().Hit, r).Damage)
	}
	return sum / attacks
}

// rotate is skills on cooldown one action at a time for the horizon, from no energy: as each action ends, the strongest skill ready that the energy covers, else a basic attack, which reaches the mob and gains energy.
func rotate(basic *act, skills []act, e gamedata.Energy) []*act {
	ready := make([]uint64, len(skills))
	var out []*act
	energy := 0
	for t := uint64(0); t < horizon; t += out[len(out)-1].length {
		next := basic
		for i := range skills {
			if ready[i] <= t && skills[i].cost <= energy {
				next, ready[i] = &skills[i], t+skills[i].cooldown
				break
			}
		}
		if next == basic {
			energy = min(energy+e.PerAttack, e.Max)
		} else {
			energy -= next.cost
		}
		out = append(out, next)
	}
	return out
}

// dps is the rotation's damage a second, each action running to its end.
func dps(basic act, skills []act, e gamedata.Energy) float64 {
	var dealt float64
	var t uint64
	for _, a := range rotate(&basic, skills, e) {
		dealt += a.damage
		t += a.length
	}
	return dealt / (float64(t) * tickInterval.Seconds())
}

// chain is damage a second as players fight, from no energy. After an attack, the strongest skill it leaves paid for and ready by its end goes, at the attack's cancel point or as it comes ready, whichever is later; else an attack follows at the end. After a skill an attack follows, cutting it at its cancel point if it is cancelable. The time runs to the last action's end.
func chain(basic act, skills []act, e gamedata.Energy) float64 {
	ready := make([]uint64, len(skills))
	var dealt float64
	var start uint64
	energy := 0
	for a := &basic; ; {
		dealt += a.damage
		end := start + a.length
		next, at := &basic, end
		switch {
		case a == &basic:
			energy = min(energy+e.PerAttack, e.Max)
			for i := range skills {
				if skills[i].cost <= energy && ready[i] <= end {
					next, at = &skills[i], max(start+a.cancelAt, ready[i])
					energy -= next.cost
					ready[i] = at + next.cooldown
					break
				}
			}
		case a.cancelable:
			at = start + a.cancelAt
		}
		if at >= horizon {
			return dealt / (float64(end) * tickInterval.Seconds())
		}
		a, start = next, at
	}
}

// skillShare is the share of the rotation's damage its skills deal.
func skillShare(basic *act, skills []act, e gamedata.Energy) float64 {
	var all, bySkills float64
	for _, a := range rotate(basic, skills, e) {
		all += a.damage
		if a != basic {
			bySkills += a.damage
		}
	}
	return bySkills / all
}

// The rotation models on made-up actions, so their rules are pinned apart from the data: an attack of 20 ticks and a Strike of 20 on a 30 tick cooldown, each with its cancel point at 6.
func TestRotation(t *testing.T) {
	basic := act{damage: 10, length: 20, cancelAt: 6, cancelable: true}
	strike := act{damage: 30, length: 20, cancelAt: 6, cooldown: 30}
	cancelable := strike
	cancelable.cancelable = true
	costly := strike
	costly.cost = 3
	paced := cancelable
	paced.cost, paced.cooldown = 3, 0
	energy := gamedata.Energy{Max: 10, PerAttack: 2}
	for _, tc := range []struct {
		name string
		dps  func(act, []act, gamedata.Energy) float64
		acts []act
		want float64
	}{
		{"basic attacks alone: one a second", dps, nil, 10},
		{"chained, an attack never cuts an attack short", chain, nil, 10},
		{"Strike on its cooldown, one at a time: Strike and an attack every 40 ticks", dps, []act{strike}, 40 / 2.0},
		{"Strike costing 3 at 2 an attack, one at a time: two Strikes every three attacks, 90 every 100 ticks", dps, []act{costly}, 90 / 5.0},
		{"chained, Strike ready mid-attack goes as it comes ready, past the attack's cancel point, and runs to its end: an attack and Strike every 30 ticks", chain, []act{strike}, 40 / 1.5},
		{"chained, an attack cuts a cancelable Strike at its cancel point and fills the time saved, Strike cutting the second at its cancel point: Strike and two attacks every 32 ticks", chain, []act{cancelable}, 50 / 1.6},
		{"chained, a Strike costing 3 with no cooldown goes as soon as the attacks pay for it: two Strikes and three attacks every 44 ticks", chain, []act{paced}, 90 / 2.2},
	} {
		if got := tc.dps(basic, tc.acts, energy); math.Abs(got-tc.want) > tc.want*0.005 {
			t.Errorf("%s: %.2f a second, want %.2f", tc.name, got, tc.want)
		}
	}
}

// C17: an action counts each of its hits and the damage over time it inflicts, ticking until it runs out or the skill can restart it; a buff's on-hit poison counts for its share of its cooldown.
func TestTheRotationCountsRepeatsAndDamageOverTime(t *testing.T) {
	d := gamedatatest.Shipped(t)
	items := realItemList(t, d)
	thief, _ := d.Class("thief")
	me := character(d, items, thief, thief.AutoAssign, 20)
	mob, _ := d.Mob("green_slime")
	stage := func(id string, n int) gamedata.Stage {
		s, _ := d.Skill(id)
		return s.Stages[s.StageAt(n)]
	}

	flurry := stage("flurry", 1)
	once := flurry
	once.Hits = gamedata.Hits{}
	if got, want := actOf(d, me, flurry, 1, mob).damage, 5*actOf(d, me, once, 1, mob).damage; got != want || want == 0 {
		t.Errorf("Flurry's action deals %v, want its five hits' %v", got, want)
	}

	stab := stage("double_stab", 10) // Bleeding Stab: bleed 4 s every 1 s, on a 3.4 s cooldown
	plain := stab
	plain.Status = gamedata.SkillInflict{}
	tick := float64(model.Steady(model.PlayerAttacker(me, stab.Damage).Times(stab.Status.Power.At(10)), model.MobDefender(mob)))
	if got, want := actOf(d, me, stab, 10, mob).damage-actOf(d, me, plain, 10, mob).damage, 3*tick; got != want || tick == 0 {
		t.Errorf("Bleeding Stab's bleed adds %v, want the 3 ticks its 3.4 s cooldown lets run, %v", got, want)
	}

	blade := stage("poison_blade", 1) // poison every 1 s while a 20 s buff lasts, on a 45 s cooldown
	poison := float64(model.Steady(model.PlayerAttacker(me, thief.Attack.Damage).Times(blade.OnHit.Power.At(1)), model.MobDefender(mob)))
	if got, want := onHitDPS(d, me, thief, blade, 1, mob), poison*20/45; math.Abs(got-want) > 1e-9 || poison == 0 {
		t.Errorf("Poison Blade adds %v a second, want a tick a second for 20 of every 45, %v", got, want)
	}
}
