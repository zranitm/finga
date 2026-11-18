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
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// horizon is how long a rotation runs, in ticks: ten minutes.
const horizon = 10 * 60 * 20

// keysOnBar is how many keys a skill bar has, the character module's SkillBarKeys.
const keysOnBar = 6

// act is an action a rotation takes: its expected damage, its length, hit and cancel point in ticks, and for a skill its id, cooldown and energy, and for a follow-up its root and the ticks after the root's hit it may go within.
type act struct {
	id                      string
	damage                  float64
	length, hitAt, cancelAt uint64
	cooldown                uint64
	cost                    int
	cancelable              bool
	root                    string
	after                   uint64
}

// open is whether the act may go at t: a root any time, a follow-up within its window from its root's last hit, as hits holds them.
func (a *act) open(t uint64, hits map[string]uint64) bool {
	if a.root == "" {
		return true
	}
	hit, ok := hits[a.root]
	return ok && hit <= t && t <= hit+a.after
}

// rotation is damage a second three ways: basic attacks alone, skills on cooldown one action at a time, and a chain as players fight; the share of the second way's damage its skills deal; and the second way again with every extra trait (C20 §8).
type rotation struct{ basic, skills, chain, skillShare, extras float64 }

func (r rotation) gain() float64 { return r.chain/r.skills - 1 }

// planned is the skill points a character of class at level has, as a player building toward the promotion spends them: the promotion's skills up to what it asks, the rest in the main skill, the promotion's first before it and the class's first after.
// The bots also put a point in each skill their level opens; a rotation that spent energy on those would count less than one action at a time, so the logged one leaves them out.
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

// rotationOf is a character's rotation against a mob, with the skills on its six keys and the damage over time its buffs' hits inflict, on the default traits and on every extra.
func rotationOf(d *gamedata.Data, me progression.Derived, c gamedata.Class, points map[string]int, m gamedata.Mob) rotation {
	length := model.TicksIn(c.Attack.Length, tickInterval)
	hitAt, cancelAt := model.ActionPoints(length, c.Attack.Hit, c.Attack.Cancel)
	basic := act{damage: meanDamage(d, model.PlayerAttacker(me, c.Attack.Damage), m), length: length, hitAt: hitAt, cancelAt: cancelAt, cancelable: true}
	skills, onHit := keysOf(d, me, c, points, nil, m, keysOnBar)
	extras, extrasOnHit := keysOf(d, me, c, points, extrasOf(d, points), m, keysOnBar)
	e := d.Energy()
	return rotation{basic: dps(basic, nil, e), skills: dps(basic, skills, e) + onHit, chain: chain(basic, skills, e) + onHit, skillShare: skillShare(&basic, skills, e), extras: dps(basic, extras, e) + extrasOnHit}
}

// extrasOf chooses the extra trait of each skill with points that has one; Spec keeps the default until it opens.
func extrasOf(d *gamedata.Data, points map[string]int) map[string]string {
	out := map[string]string{}
	for id := range points {
		if s, _ := d.Skill(id); len(s.Traits) == 2 {
			out[id] = s.Traits[1].ID
		}
	}
	return out
}

// keysOf are the damage skills a rotation carries on n keys, each on its trait, the strongest first, and what the keys' buffs add a second by their hits' damage over time.
// A key holds a skill with points the class lets stand on a key, and the follow-ups right after it (C18); a far lock, a control or a dodge isn't modelled, so a follow-up waiting on one isn't counted.
// The n keys whose strongest use deals the most are carried, a buff's use counted as its damage over time while it lasts.
func keysOf(d *gamedata.Data, me progression.Derived, c gamedata.Class, points map[string]int, traits map[string]string, m gamedata.Mob, n int) (skills []act, onHit float64) {
	type key struct {
		root         string
		acts         []act
		onHit, worth float64
	}
	var keys []key
	for root := range points {
		if s, _ := d.Skill(root); points[root] == 0 || s.Kind != gamedata.Active || !c.Keyable(root) {
			continue
		}
		k := key{root: root}
		add := func(id, from string, after time.Duration) {
			s, _ := d.Skill(id)
			p := points[id]
			if p == 0 {
				return
			}
			st := s.Spec(p, traits[id])
			switch {
			case s.Effect == gamedata.DamageEffect:
				a := actOf(d, me, st, p, m)
				a.id, a.root, a.after = id, from, model.TicksIn(after, tickInterval)
				k.acts = append(k.acts, a)
				k.worth = max(k.worth, a.damage)
			case s.Effect == gamedata.BuffEffect && st.OnHit.Status != "":
				dps := onHitDPS(d, me, c, st, p, m)
				k.onHit += dps
				k.worth = max(k.worth, dps*max(st.Cooldown, st.Duration).Seconds())
			}
		}
		add(root, "", 0)
		for _, ch := range c.Chains {
			for _, f := range ch.Follow {
				if ch.Root == root && f.When.After > 0 {
					add(f.Skill, root, f.When.After)
				}
			}
		}
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b key) int { return cmp.Or(cmp.Compare(b.worth, a.worth), cmp.Compare(a.root, b.root)) })
	for _, k := range keys[:min(len(keys), n)] {
		skills = append(skills, k.acts...)
		onHit += k.onHit
	}
	slices.SortFunc(skills, func(a, b act) int { return cmp.Or(cmp.Compare(b.damage, a.damage), cmp.Compare(a.id, b.id)) }) // the strongest first
	return skills, onHit
}

// actOf is a damage skill's action at n points: each of its hits, and the damage over time it inflicts, ticking until it runs out or the skill can be used again to restart it.
func actOf(d *gamedata.Data, me progression.Derived, st gamedata.Stage, n int, m gamedata.Mob) act {
	l := model.TicksIn(st.Length, tickInterval)
	hitAt, at := model.ActionPoints(l, st.Hit, st.Cancel)
	attacker := model.PlayerAttacker(me, st.Damage)
	damage := meanDamage(d, attacker.Times(st.Power.At(n)), m) * float64(max(st.Hits.Count, 1))
	if def, ok := d.Status(st.Status.Status); ok && def.Group == gamedata.Dot {
		cooldown := time.Duration(model.SkillCooldownTicks(st.Cooldown, me.CooldownSpeed, tickInterval)) * tickInterval
		ticks := min(st.Status.Time, max(cooldown, st.Length)) / def.Every
		damage += float64(ticks) * float64(model.Steady(attacker.Times(st.Status.Power.At(n)), model.MobDefender(m)))
	}
	return act{
		damage: damage, length: l, hitAt: hitAt, cancelAt: at, cancelable: st.Cancelable,
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

// rotate is skills on cooldown one action at a time for the horizon, from no energy: as each action ends, the strongest skill ready and open that the energy covers, else a basic attack, which reaches the mob and gains energy.
func rotate(basic *act, skills []act, e gamedata.Energy) []*act {
	ready := make([]uint64, len(skills))
	hits := map[string]uint64{}
	var out []*act
	energy := 0
	for t := uint64(0); t < horizon; t += out[len(out)-1].length {
		next := basic
		for i := range skills {
			if ready[i] <= t && skills[i].cost <= energy && skills[i].open(t, hits) {
				next, ready[i] = &skills[i], t+skills[i].cooldown
				break
			}
		}
		if next == basic {
			energy = min(energy+e.PerAttack, e.Max)
		} else {
			energy -= next.cost
			hits[next.id] = t + next.hitAt
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

// chain is damage a second as players fight, from no energy. After an attack, the strongest skill it leaves paid for, ready by its end and open as it goes, goes at the attack's cancel point or as it comes ready, whichever is later; else an attack follows at the end. After a skill an attack follows, cutting it at its cancel point if it is cancelable. The time runs to the last action's end.
func chain(basic act, skills []act, e gamedata.Energy) float64 {
	ready := make([]uint64, len(skills))
	hits := map[string]uint64{}
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
				from := max(start+a.cancelAt, ready[i])
				if skills[i].cost <= energy && ready[i] <= end && skills[i].open(from, hits) {
					next, at = &skills[i], from
					energy -= next.cost
					ready[i] = at + next.cooldown
					hits[next.id] = at + next.hitAt
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
	// A root of 30 hitting at 4, and its follow-up of 50 open for 30 ticks from the root's hit, or for 10 when late, each on a 100 tick cooldown.
	root := act{id: "root", damage: 30, length: 20, hitAt: 4, cancelAt: 6, cooldown: 100}
	follow := act{id: "follow", damage: 50, length: 20, hitAt: 4, cancelAt: 6, cooldown: 100, root: "root", after: 30}
	late := follow
	late.after = 10
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
		{"one at a time, a follow-up goes as its root ends, 16 ticks past the root's hit: the root, the follow-up and three attacks every 100 ticks", dps, []act{follow, root}, 110 / 5.0},
		{"one at a time, a follow-up whose window shut before its root ends never goes: the root and four attacks every 100 ticks", dps, []act{late, root}, 70 / 5.0},
		{"chained, a follow-up goes at the next attack's cancel point, 22 ticks past the root's hit: the root, an attack, the follow-up and three attacks every 100 ticks", chain, []act{follow, root}, 120 / 5.0},
		{"chained, a follow-up whose window shut by the attack's cancel point never goes", chain, []act{late, root}, 70 / 5.0},
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

// C18: a rotation carries six keys, each a skill its class lets stand on a key with its follow-ups right after it, those waiting on a far lock, a control or a dodge left out; the six whose strongest use deals the most.
func TestARotationCarriesSixKeys(t *testing.T) {
	d := gamedatatest.Shipped(t)
	items := realItemList(t, d)
	mob, _ := d.Mob("green_slime")
	ids := func(acts []act) []string {
		var out []string
		for _, a := range acts {
			out = append(out, a.id)
		}
		slices.Sort(out)
		return out
	}
	// A point in each skill the class's line teaches, as the bots put them, and the rest as planned.
	everySkill := func(c gamedata.Class) map[string]int {
		points := planned(d, c, c, 30)
		for _, line := range c.Line {
			teacher, _ := d.Class(line)
			for _, id := range teacher.Skills {
				points[id] = max(points[id], 1)
			}
		}
		return points
	}
	carried := func(id kernel.Class, n int) ([]act, float64) {
		c, _ := d.Class(id)
		return keysOf(d, character(d, items, c, c.AutoAssign, 30), c, everySkill(c), nil, mob, n)
	}
	// Charge and Ground Slam wait on a far or controlled lock, and Lunge on a dodge; Iron Body follows War Cry but deals nothing.
	if got, _ := carried("warrior", keysOnBar); !slices.Equal(ids(got), []string{"slash_blast", "spark", "strike", "throw"}) {
		t.Errorf("a Warrior carries %v", ids(got))
	}
	// Flurry follows Double Stab within 2 s; Shadow Step waits on a far lock or a dodge. Poison Blade's key adds its poison.
	thief, poison := carried("thief", keysOnBar)
	if i := slices.IndexFunc(thief, func(a act) bool { return a.id == "flurry" }); i < 0 || thief[i].root != "double_stab" || thief[i].after != 40 || slices.Contains(ids(thief), "shadow_step") || poison == 0 {
		t.Errorf("a Thief carries %+v and %v of poison, want Flurry 40 ticks after Double Stab's hit, no Shadow Step, and Poison Blade's poison", thief, poison)
	}
	// Seven damage keys, Arrow Rain waiting on a control: the weakest goes.
	six, _ := carried("bowman", keysOnBar)
	seven, _ := carried("bowman", 99)
	if len(six) != 6 || len(seven) != 7 || slices.Contains(ids(seven), "arrow_rain") {
		t.Fatalf("a Bowman carries %v of %v, want six of seven, no Arrow Rain", ids(six), ids(seven))
	}
	if weakest := seven[len(seven)-1]; slices.ContainsFunc(six, func(a act) bool { return a.id == weakest.id }) {
		t.Errorf("a Bowman carries %v, want %s, the weakest of %v, left off", ids(six), weakest.id, ids(seven))
	}
	// Meteor waits on a control.
	if got, _ := carried("magician", keysOnBar); slices.Contains(ids(got), "meteor") || !slices.Contains(ids(got), "frost_nova") {
		t.Errorf("a Magician carries %v, want Frost Nova and no Meteor", ids(got))
	}
}

// C20 §8: the harness runs each class on every extra too: Numbing Edge's weaken in place of Venom's poison adds no damage over time, and Concussion's Crushing Blow comes round every 7 s, not 4.5.
func TestARotationOnEveryExtra(t *testing.T) {
	d := gamedatatest.Shipped(t)
	items := realItemList(t, d)
	mob, _ := d.Mob("green_slime")
	thief, _ := d.Class("thief")
	me := character(d, items, thief, thief.AutoAssign, 30)
	points := map[string]int{"poison_blade": 10, "strike": 25, "double_stab": 10}
	strikeOf := func(acts []act) act {
		return acts[slices.IndexFunc(acts, func(a act) bool { return a.id == "strike" })]
	}
	defaults, venom := keysOf(d, me, thief, points, nil, mob, keysOnBar)
	extras, numbing := keysOf(d, me, thief, points, extrasOf(d, points), mob, keysOnBar)
	ticks := func(cd time.Duration) uint64 { return model.SkillCooldownTicks(cd, me.CooldownSpeed, tickInterval) }
	if venom == 0 || numbing != 0 {
		t.Errorf("poison a second %v on Venom, %v on Numbing Edge; want some, then none", venom, numbing)
	}
	if got, want := strikeOf(defaults).cooldown, ticks(4500*time.Millisecond); got != want {
		t.Errorf("Swift's Crushing Blow cools for %d ticks, want %d", got, want)
	}
	if got, want := strikeOf(extras).cooldown, ticks(7*time.Second); got != want {
		t.Errorf("Concussion's Crushing Blow cools for %d ticks, want %d", got, want)
	}
}
