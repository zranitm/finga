package world_test

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"testing"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// PvP balance (docs/tech/pvp.md section 14): in a match every class fights with its kit, and pvp.yaml's scale.hp and scale.damage set how long a fight lasts.
const (
	downFastest, downSlowest = 12.0, 18.0 // seconds a class's rotation takes to down a still kit, each pair
	meanFastest, meanSlowest = 14.0, 16.0 // seconds, over every pair
	burstMost                = 0.4        // of a kit's HP: the most the longest control-into-chain burst takes
)

// kit is a class's match kit as the world derives it: its own stats at the kit's level, its worn items at the middle of their rolls, and every skill at its last stage; its max HP, and the factor on the hits it deals, by scale and its class's own.
type kit struct {
	class  gamedata.Class
	me     progression.Derived
	points map[string]int
	hp     float64
	damage float64
}

func kitOf(t *testing.T, d *gamedata.Data, id kernel.Class) kit {
	t.Helper()
	c, _ := d.Class(id)
	k, ok := d.Kit(id)
	if !ok {
		t.Fatalf("no kit for %s", id)
	}
	points := map[string]int{}
	var learned []progression.Learned
	for _, s := range d.Skills() {
		if c.Learnable(s) && !s.Granted {
			points[s.ID] = max(s.Stages[len(s.Stages)-1].At, 1)
			learned = append(learned, progression.Learned{Skill: s, Points: points[s.ID]})
		}
	}
	var worn []progression.WornItem
	for _, w := range k.Worn {
		it, _ := d.Item(w.Item)
		worn = append(worn, progression.WornItem{Item: it, Stats: progression.Rolled(it.Rolls, func(r gamedata.Range) int { return (r.Min + r.Max) / 2 })})
	}
	me := progression.SheetOf(d.StatRules(), c.ID, k.Level, k.Own, progression.Active(worn, k.Level, c, k.Own), progression.Passives(learned)).Derived
	scale := d.Pvp().Scale
	return kit{class: c, me: me, points: points, hp: math.Round(float64(me.MaxHP) * scale.HP * k.HP), damage: scale.Damage * k.Damage}
}

// kits are every class with a kit, by id.
func kits(t *testing.T, d *gamedata.Data) []kit {
	var ids []kernel.Class
	for id := range d.Pvp().Kits {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var out []kit
	for _, id := range ids {
		out = append(out, kitOf(t, d, id))
	}
	return out
}

// on is the kit as a rotation's target: a player in a match, whom no blow MISSes.
func (k kit) on() target { return target{Defender: model.PlayerDefender(k.me), sure: true} }

// downIn is how long a's rotation, chained as players fight on the default traits with each press a little late, takes to down a still b: b's HP over a's damage a second on b, both scaled.
func downIn(d *gamedata.Data, a, b kit) float64 {
	skills, onHit := keysOf(d, a.me, a.class, a.points, nil, b.on(), keysOnBar)
	return b.hp / ((chain(basicOf(d, a.me, a.class, b.on()), skills, d.Energy(), pressLate) + onHit) * a.damage)
}

// Time to down: each class's rotation downs a still kit of each class in 12 to 18 s, and in 14 to 16 s over all of them.
func TestPvpTimeToDown(t *testing.T) {
	d := gamedatatest.Shipped(t)
	all := kits(t, d)
	var sum float64
	for _, a := range all {
		for _, b := range all {
			s := downIn(d, a, b)
			sum += s
			t.Logf("%s downs %s (%v HP) in %.1f s", a.class.ID, b.class.ID, b.hp, s)
			if s < downFastest || s > downSlowest {
				t.Errorf("%s downs %s in %.1f s, want %v to %v", a.class.ID, b.class.ID, s, downFastest, downSlowest)
			}
		}
	}
	if mean := sum / float64(len(all)*len(all)); mean < meanFastest || mean > meanSlowest {
		t.Errorf("a down takes %.2f s on average, want %v to %v", mean, meanFastest, meanSlowest)
	}
}

// blow is an action of a burst: how long the control it lands holds the target from acting, whether a blow after wakes it, and when it can go.
type blow struct {
	act
	holds uint64
	wakes bool
	when  goes
}

// goes is when a blow can go in a burst, where its target is always held.
type goes int

const (
	anyTime   goes = iota // from its own key, or within a time of its root's hit as the rotation has it
	whileHeld             // a follow-up waiting on a control: never a burst's first blow
	opening               // a follow-up waiting on a far lock or a dodge: only a burst's first blow
)

// blowsOf are a's damage skills it can key or follow up into, on b, with no trait, the strongest first.
func blowsOf(d *gamedata.Data, a, b kit) []blow {
	var out []blow
	for _, id := range slices.Sorted(maps.Keys(a.points)) {
		if bl, ok := blowOf(d, a, b, id, ""); ok {
			out = append(out, bl)
		}
	}
	strongestFirst(out)
	return out
}

// withTrait is blows with id's blow taken with trait, the strongest first; blows is left as it was.
func withTrait(d *gamedata.Data, a, b kit, blows []blow, id, trait string) []blow {
	k := slices.IndexFunc(blows, func(bl blow) bool { return bl.id == id })
	if k < 0 {
		return blows
	}
	out := slices.Clone(blows)
	out[k], _ = blowOf(d, a, b, id, trait)
	strongestFirst(out)
	return out
}

func strongestFirst(blows []blow) {
	slices.SortFunc(blows, func(x, y blow) int { return cmp.Or(cmp.Compare(y.damage, x.damage), cmp.Compare(x.id, y.id)) })
}

// blowOf is a's skill id on b with trait; false when it isn't an active damage skill.
func blowOf(d *gamedata.Data, a, b kit, id, trait string) (blow, bool) {
	s, _ := d.Skill(id)
	if s.Kind != gamedata.Active || s.Effect != gamedata.DamageEffect {
		return blow{}, false
	}
	st := s.Spec(a.points[id], trait)
	bl := blow{act: actOf(d, a.me, st, a.points[id], b.on())}
	bl.id = id
	if def, ok := d.Status(st.Status.Status); ok && def.Group == gamedata.Hard && def.Blocks.Has(gamedata.BlocksAct) {
		bl.holds, bl.wakes = model.TicksIn(st.Status.Time, tickInterval), def.Wakes
		if def.ID == gamedata.Knockdown {
			bl.holds = model.TicksIn(d.MobRules().React.Down, tickInterval) // the down pose
		}
	}
	if f, root, ok := followOf(a.class, id); ok && !a.class.Keyable(id) {
		switch {
		case f.When.After > 0:
			bl.root, bl.after = root, model.TicksIn(f.When.After, tickInterval)
		case len(f.When.Controlled) > 0:
			bl.when = whileHeld
		default:
			bl.when = opening
		}
	}
	return bl, true
}

// followOf is the class's chain that skill follows in, and its root.
func followOf(c gamedata.Class, skill string) (gamedata.FollowUp, string, bool) {
	for _, ch := range c.Chains {
		if f, ok := c.FollowUp(ch.Root, skill); ok {
			return f, ch.Root, true
		}
	}
	return gamedata.FollowUp{}, "", false
}

// burst is what the control x and the chain after it deal before the target is free: from x's start, every skill ready and the energy full, chained as players fight, each control that lands adding its hold, and a blow on a sleeper waking it.
func burst(x blow, blows []blow, basic act, e gamedata.Energy) float64 {
	ready, hits := map[string]uint64{x.id: x.cooldown}, map[string]uint64{x.id: x.hitAt}
	energy, held, sleeping, dealt := e.Max-x.cost, x.hitAt+x.holds, x.wakes, x.damage
	cur, start := x, uint64(0)
	for {
		next, at := blow{act: basic}, start+cur.length
		if cur.id == "" {
			energy = min(energy+e.PerAttack, e.Max)
			for _, b := range blows {
				from := max(start+cur.cancelAt, ready[b.id])
				if b.when != opening && b.cost <= energy && ready[b.id] <= start+cur.length && b.open(from, hits) {
					next, at = b, from
					break
				}
			}
		} else if cur.cancelable {
			at = start + cur.cancelAt
		}
		hit := at + next.hitAt
		if hit >= held {
			return dealt
		}
		dealt += next.damage
		if next.id != "" {
			energy -= next.cost
			ready[next.id], hits[next.id] = at+next.cooldown, hit
		}
		switch {
		case next.holds > 0:
			held, sleeping = max(held, hit+next.holds), next.wakes
		case sleeping && next.damage > 0:
			held = hit + 1 // the blow wakes the sleeper
		}
		cur, start = next, at
	}
}

// The longest burst: a control that keeps its target from acting, and the chain it holds them for, takes at most 40% of any kit's HP.
func TestPvpBurst(t *testing.T) {
	d := gamedatatest.Shipped(t)
	all := kits(t, d)
	var worst float64
	for _, a := range all {
		for _, b := range all {
			basic := basicOf(d, a.me, a.class, b.on())
			plain := blowsOf(d, a, b) // each skill's damage is a mean of many rolls, so a trait retakes only its own skill's
			for _, id := range slices.Sorted(maps.Keys(a.points)) {
				s, _ := d.Skill(id)
				for _, trait := range append([]string{""}, traitIDs(s)...) {
					blows := plain
					if trait != "" {
						blows = withTrait(d, a, b, plain, id, trait)
					}
					k := slices.IndexFunc(blows, func(bl blow) bool { return bl.id == id })
					if k < 0 || blows[k].holds == 0 || blows[k].when == whileHeld {
						continue
					}
					share := burst(blows[k], blows, basic, d.Energy()) * a.damage / b.hp
					worst = max(worst, share)
					if share > burstMost {
						t.Errorf("%s's %s[%s] on %s takes %.0f%% of its HP, want at most %.0f%%", a.class.ID, id, trait, b.class.ID, share*100, burstMost*100)
					}
					if share > 0.25 {
						t.Logf("%s's %s[%s] on %s: %.0f%%", a.class.ID, id, trait, b.class.ID, share*100)
					}
				}
			}
		}
	}
	t.Logf("the longest burst takes %.0f%% of a kit's HP", worst*100)
}

func traitIDs(s gamedata.Skill) []string {
	var out []string
	for _, tr := range s.Traits {
		out = append(out, tr.ID)
	}
	return out
}

// The burst models made-up actions, so its rules are pinned apart from the data: attacks of 20 ticks cut at 6, a stun of 20 holding for 30, and a slam of 10 that knocks down for 20 more.
func TestBurstRules(t *testing.T) {
	basic := act{damage: 10, length: 20, cancelAt: 6, cancelable: true}
	stun := blow{id: "stun", damage: 20, length: 20, cancelAt: 6, cooldown: 100, holds: 30}
	slam := blow{id: "slam", damage: 50, length: 10, cancelAt: 6, cooldown: 100, holds: 20, when: whileHeld}
	sleep := stun
	sleep.wakes, sleep.holds = true, 80
	rush := blow{id: "rush", damage: 40, length: 20, cancelAt: 6, cooldown: 100, when: opening}
	energy := gamedata.Energy{Max: 10, PerAttack: 2}
	for _, c := range []struct {
		name  string
		x     blow
		blows []blow
		want  float64
	}{
		{"the stun, and the attack at its end, inside its 30 ticks", stun, nil, 30},
		{"the stun, an attack, the slam at its cancel point, holding to tick 46, and an attack at 36", stun, []blow{slam}, 90},
		{"a rush that waits on a far lock or a dodge only opens a burst", stun, []blow{slam, rush}, 90},
		{"a sleep ends at the first blow after it", sleep, []blow{slam}, 30},
	} {
		if got := burst(c.x, c.blows, basic, energy); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
