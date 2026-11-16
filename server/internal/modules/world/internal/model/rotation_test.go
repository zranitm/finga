package model_test

import (
	"cmp"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/progression"
	"finga/modules/world/internal/model"
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

// planned is the skill points a character of class at level has, as the bots spend them: the promotion's skills up to what it asks, the rest in the main skill, the promotion's first before it and the class's first after.
func planned(d *gamedata.Data, class, future gamedata.Class, level int) map[string]int {
	left := (level - 1) * d.StatRules().SkillPointsPerLevel
	points := map[string]int{}
	for _, id := range slices.Sorted(maps.Keys(future.Promotion.Skills)) {
		n := min(left, future.Promotion.Skills[id])
		points[id] += n
		left -= n
	}
	main := slices.Sorted(maps.Keys(future.Promotion.Skills))[0]
	if class.ID != d.StartClass().ID {
		main = class.Skills[0]
	}
	points[main] += left
	return points
}

// rotationOf is a character's rotation against a mob, with every damage skill it has points in.
func rotationOf(d *gamedata.Data, me progression.Derived, c gamedata.Class, points map[string]int, m gamedata.Mob) rotation {
	length := model.TicksIn(c.Attack.Length, tickInterval)
	_, cancelAt := model.ActionPoints(length, c.Attack.Hit, c.Attack.Cancel)
	basic := act{damage: meanDamage(d, model.PlayerAttacker(me, c.Attack.Damage), m), length: length, cancelAt: cancelAt, cancelable: true}
	var skills []act
	for id, n := range points {
		s, _ := d.Skill(id)
		if n == 0 || s.Kind != gamedata.Active || s.Effect != gamedata.DamageEffect {
			continue
		}
		st := s.Stages[s.StageAt(n)]
		l := model.TicksIn(st.Length, tickInterval)
		_, at := model.ActionPoints(l, st.Hit, st.Cancel)
		skills = append(skills, act{
			damage: meanDamage(d, model.PlayerAttacker(me, st.Damage).Times(st.Power.At(n)), m),
			length: l, cancelAt: at, cancelable: st.Cancelable,
			cooldown: model.SkillCooldownTicks(st.Cooldown, me.CooldownSpeed, tickInterval),
			cost:     st.Energy,
		})
	}
	slices.SortFunc(skills, func(a, b act) int { return cmp.Compare(b.damage, a.damage) }) // the strongest first
	e := d.Energy()
	return rotation{basic: dps(basic, nil, e), skills: dps(basic, skills, e), chain: chain(basic, skills, e), skillShare: skillShare(&basic, skills, e)}
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
