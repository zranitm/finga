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
	"finga/services/world/internal/model"
)

// maxChainGain is the most a perfect chain of cancels may add to a rotation that never cancels, leaving room around the product's "up to about 20%".
const maxChainGain = 0.22

// horizon is how long a rotation runs, in ticks: ten minutes.
const horizon = 10 * 60 * 20

// act is an action a rotation takes: its expected damage, its length and cancel point in ticks, and for a skill its cooldown.
type act struct {
	damage           float64
	length, cancelAt uint64
	cooldown         uint64
	cancelable       bool
}

// rotation is damage a second three ways: basic attacks alone, skills on cooldown one action at a time, and the same actions as a perfect chain, every cancel allowed taken at its point; and the share of the second way's damage its skills deal.
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
		})
	}
	slices.SortFunc(skills, func(a, b act) int { return cmp.Compare(b.damage, a.damage) }) // the strongest first
	return rotation{basic: dps(basic, nil, false), skills: dps(basic, skills, false), chain: dps(basic, skills, true), skillShare: skillShare(&basic, skills)}
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

// rotate is skills on cooldown one action at a time for the horizon: as each action ends, the strongest skill ready, else a basic attack. MP is taken as enough.
func rotate(basic *act, skills []act) []*act {
	ready := make([]uint64, len(skills))
	var out []*act
	for t := uint64(0); t < horizon; t += out[len(out)-1].length {
		next := basic
		for i := range skills {
			if ready[i] <= t {
				next, ready[i] = &skills[i], t+skills[i].cooldown
				break
			}
		}
		out = append(out, next)
	}
	return out
}

// dps is the rotation's damage a second. Chained, each action that the next cuts short ends at its cancel point: a skill cuts any attack short, and an attack a cancelable skill. A skill still waits for its cooldown.
func dps(basic act, skills []act, chain bool) float64 {
	seq := rotate(&basic, skills)
	ready := map[*act]uint64{}
	var t, dealt float64
	var start uint64
	for k, a := range seq {
		start = max(start, ready[a])
		ready[a] = start + a.cooldown
		dealt += a.damage
		end := start + a.length
		if k+1 < len(seq) && chain && cuts(a, seq[k+1], &basic) {
			end = start + a.cancelAt
		}
		t = float64(start + a.length) // the time the rotation takes runs to the last action's end
		start = end
	}
	return dealt / (t * tickInterval.Seconds())
}

// skillShare is the share of the rotation's damage its skills deal.
func skillShare(basic *act, skills []act) float64 {
	var all, bySkills float64
	for _, a := range rotate(basic, skills) {
		all += a.damage
		if a != basic {
			bySkills += a.damage
		}
	}
	return bySkills / all
}

// cuts is whether next may cut a short: only the other kind, and an attack only a cancelable skill.
func cuts(a, next, basic *act) bool {
	return (a == basic) != (next == basic) && (a == basic || a.cancelable)
}

// The rotation model on made-up actions, so its rules are pinned apart from the data: an attack of 20 ticks and a Strike of 20 on a 30 tick cooldown, each with its cancel point at 17.
func TestRotation(t *testing.T) {
	basic := act{damage: 10, length: 20, cancelAt: 17, cancelable: true}
	strike := act{damage: 30, length: 20, cancelAt: 17, cooldown: 30}
	cancelable := strike
	cancelable.cancelable = true
	for _, tc := range []struct {
		name   string
		skills []act
		chain  bool
		want   float64
	}{
		{"basic attacks alone: one a second", nil, false, 10},
		{"chained, an attack never cuts an attack short", nil, true, 10},
		{"Strike on its cooldown: Strike and an attack every 40 ticks", []act{strike}, false, 40 / 2.0},
		{"chained, Strike cuts the attack short at 17: every 37 ticks", []act{strike}, true, 40 / 1.85},
		{"a cancelable Strike is cut short at 17 as well: every 34 ticks", []act{cancelable}, true, 40 / 1.7},
		{"two skills back to back: neither cuts the other short", []act{cancelable, {damage: 30, length: 20, cancelAt: 17, cooldown: 20, cancelable: true}}, true, 30},
	} {
		if got := dps(basic, tc.skills, tc.chain); math.Abs(got-tc.want) > tc.want*0.005 {
			t.Errorf("%s: %.2f a second, want %.2f", tc.name, got, tc.want)
		}
	}
}
