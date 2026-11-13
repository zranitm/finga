package model_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"finga/internal/gamedata"
	"finga/internal/gamedata/load"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/world/internal/model"
)

const (
	realData     = "../../../../data"
	tickInterval = 50 * time.Millisecond // the world's 20 ticks a second
	attacks      = 20000                 // resolved each way per fight
)

// region is a band of hunting maps, from the lowest mob level of its first map, and what an on-level character meets there.
type region struct {
	name       string
	from       int        // a map is in the last region whose from is at most its min_level
	hits       [2]float64 // a normal mob's landed hits to kill at the reference pace; a faster class needs more, a slower one fewer
	survives   [2]int     // a ranged class's mob hits lived through, the least of them a melee class's; 0 as the top is no top
	playerHits float64    // the least share of every class's attacks that land, when below the class's own floor
	life       float64    // the least kills a melee class manages on a normal mob before dying with no potion
}

// Forest and cave are only logged: they are the Beginner's, and skills retune them.
var regions = []region{
	{name: "A", from: 15, hits: [2]float64{2.5, 3.5}, survives: [2]int{10, 12}, life: 12},
	{name: "B", from: 21, hits: [2]float64{3.5, 4.5}, survives: [2]int{8, 10}, life: 8},
	{name: "C", from: 26, hits: [2]float64{4.5, 5.5}, survives: [2]int{6, 8}, life: 5.5},
	{name: "D", from: 31, hits: [2]float64{6, 8}, survives: [2]int{4, 0}, playerHits: 0.70, life: 3},
}

// fighter is what a class a Beginner is promoted to must manage against a mob of its level.
type fighter struct {
	hitFloor float64    // the least share of its attacks that land
	hitBy    [2]float64 // the share of a mob's attacks that land on it
}

var fighters = map[kernel.Class]fighter{
	"warrior":  {hitFloor: 0.75, hitBy: [2]float64{0.85, 0.95}}, // its accuracy barely grows, so a mob's evasion must stay near it
	"thief":    {hitFloor: 0.80, hitBy: [2]float64{0.55, 0.70}}, // built on AGI, it dodges clearly more than the rest, never most attacks
	"bowman":   {hitFloor: 0.80, hitBy: [2]float64{0.85, 0.95}},
	"magician": {hitFloor: 0.75, hitBy: [2]float64{0.85, 0.95}},
}

const (
	refPace        = 1.125 // seconds a landed hit takes the warrior, 900 ms at 80% landing, which the hits bands are set at
	promotionLevel = 10    // below it a character is a Beginner
	killsPerLevel  = 20    // a normal mob's EXP is about a twentieth of its level's
	bandStars      = 5     // ★6 and ★7 are rare finds, not what a character can expect to wear
)

func regionOf(m *gamedata.Map) (region, bool) {
	var r region
	found := false
	for _, rg := range regions {
		if m.MinLevel >= rg.from {
			r, found = rg, true
		}
	}
	return r, found
}

// fight is how an on-level character and a mob fare against each other, the character standing in its reach all along and never dodging.
type fight struct {
	hitsToKill     float64 // landed hits
	hitRate        float64 // the character's attacks that land
	hitsSurvived   int     // the mob's landed hits the character lives through, at their mean over its attacks
	landRate       float64 // the mob's attacks that land
	hitDamage      float64 // a landed hit's damage, the mean over its attacks
	hitsPerSecond  float64 // the mob's hits that land
	taken          float64 // damage a second
	secondsPerKill float64 // attacking, misses included, at the character's attack pace
	killsPerLife   float64 // kills before dying with no potion, for a melee class
}

// character is a class at level, every level's points spent by split and wearing, in every slot, the newest band gear it can use at its middle roll.
func character(d *gamedata.Data, items []gamedata.Item, c gamedata.Class, split gamedata.Stats, level int) progression.Derived {
	own := d.StartClass().BaseStats
	for range level - 1 {
		own = progression.AddStats(own, split)
	}
	var worn []progression.WornItem
	for _, s := range d.EquipSlots() {
		var best *gamedata.Item
		for k := range items {
			it := &items[k]
			if it.Tab != gamedata.Equip || it.Slot != s.Kind || it.Stars > bandStars || progression.Meets(*it, level, c, own) != progression.NothingUnmet {
				continue
			}
			if best == nil || it.MinLevel > best.MinLevel {
				best = it
			}
		}
		if best != nil {
			mid := progression.Rolled(best.Rolls, func(r gamedata.Range) int { return (r.Min + r.Max) / 2 })
			worn = append(worn, progression.WornItem{Item: *best, Stats: mid})
		}
	}
	return progression.DeriveStats(d.StatRules(), c.ID, level, own, progression.Active(worn, level, c, own))
}

// fightOf resolves many attacks each way with the world's own combat, from a fixed seed.
func fightOf(d *gamedata.Data, me progression.Derived, c gamedata.Class, m gamedata.Mob) fight {
	r := rand.New(rand.NewPCG(1, 2))
	hit := d.StatRules().Hit
	var landed, dealt float64
	for range attacks {
		if h := model.Resolve(model.PlayerAttacker(me, c.Attack.Damage), model.MobDefender(m), hit, r); !h.Miss {
			landed++
			dealt += float64(h.Damage)
		}
	}
	f := fight{hitsToKill: float64(m.HP) / (dealt / landed), hitRate: landed / attacks}
	rates := cadence(m, d.MobRules().HitGuard)
	for i, a := range m.Attacks {
		var lands, taken float64
		for range attacks {
			if h := model.Resolve(model.MobAttacker(m, a.Power), model.PlayerDefender(me), hit, r); !h.Miss {
				lands++
				taken += float64(h.Damage)
			}
		}
		share := float64(len(m.Attacks))
		f.landRate += lands / attacks / share
		f.hitDamage += taken / lands / share
		f.hitsPerSecond += rates[i] * lands / attacks
		f.taken += rates[i] * taken / attacks
	}
	f.hitsSurvived = int(math.Ceil(float64(me.MaxHP)/f.hitDamage)) - 1
	pace := time.Duration(model.AttackLengthTicks(c.Attack.Length, me.AttackSpeed, tickInterval)) * tickInterval
	f.secondsPerKill = f.hitsToKill / f.hitRate * pace.Seconds()
	f.killsPerLife = float64(f.hitsSurvived+1) / (f.secondsPerKill * f.hitsPerSecond)
	return f
}

// cadence is how often a second each of a mob's attacks reaches a player who stays in reach.
func cadence(m gamedata.Mob, guard time.Duration) []float64 {
	const over = time.Hour
	counts := make([]float64, len(m.Attacks))
	ready := make([]time.Duration, len(m.Attacks))
	var at, guarded time.Duration
	next := 0
	for at < over {
		pick := -1
		for k := range m.Attacks {
			if i := (next + k) % len(m.Attacks); ready[i] <= at {
				pick = i
				break
			}
		}
		if pick < 0 {
			at = slices.Min(ready)
			continue
		}
		a := m.Attacks[pick]
		ready[pick] = at + a.Cooldown
		if lands := at + a.Windup + a.Travel; lands >= guarded {
			counts[pick]++
			guarded = lands + guard
		}
		next = (pick + 1) % len(m.Attacks)
		at += a.Busy()
	}
	for i := range counts {
		counts[i] /= over.Seconds()
	}
	return counts
}

// realItemList is every item in data/items.yaml.
func realItemList(t *testing.T, d *gamedata.Data) []gamedata.Item {
	t.Helper()
	raw, err := os.ReadFile(realData + "/items.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Items []struct {
			ID string `yaml:"id"`
		} `yaml:"items"`
	}
	if err := yaml.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	out := make([]gamedata.Item, 0, len(f.Items))
	for _, it := range f.Items {
		item, _ := d.Item(it.ID)
		out = append(out, item)
	}
	return out
}

// mapMobs are the mob types a map spawns, its normal ones first.
func mapMobs(d *gamedata.Data, m *gamedata.Map) (normal, elite []gamedata.Mob) {
	seen := map[string]bool{}
	for _, s := range m.Spawns {
		if seen[s.Mob] {
			continue
		}
		seen[s.Mob] = true
		mob, _ := d.Mob(s.Mob)
		if mob.Elite {
			elite = append(elite, mob)
		} else {
			normal = append(normal, mob)
		}
	}
	return normal, elite
}

// player is who meets a mob: a class a Beginner is promoted to, or a Beginner spending its points by the split of the class it means to become.
type player struct {
	class  gamedata.Class
	future gamedata.Class // the class it is, or means to become
	split  gamedata.Stats
	name   string
}

// players are who meets a mob of level: Beginners below the promotion level, the classes they are promoted to from it.
func players(d *gamedata.Data, level int) []player {
	var out []player
	for _, c := range d.Classes() {
		if c.Parent != d.StartClass().ID {
			continue
		}
		if level < promotionLevel {
			out = append(out, player{class: d.StartClass(), future: c, split: c.AutoAssign, name: "beg>" + string(c.ID)[:3]})
		} else {
			out = append(out, player{class: c, future: c, split: c.AutoAssign, name: string(c.ID)})
		}
	}
	return out
}

// Every hunting map's mobs against each on-level class, held to their region's targets from A on; the table is logged for tuning.
func TestBalance(t *testing.T) {
	d, err := load.Dir(realData)
	if err != nil {
		t.Fatal(err)
	}
	items := realItemList(t, d)
	for _, m := range d.Maps() {
		if m.Kind != gamedata.Hunting {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			t.Parallel()
			balanceOf(t, d, items, m)
		})
	}
}

// balanceOf is TestBalance on one map.
func balanceOf(t *testing.T, d *gamedata.Data, items []gamedata.Item, m *gamedata.Map) {
	rg, asserted := regionOf(m)
	normals, elites := mapMobs(d, m)
	for _, mob := range append(normals, elites...) {
		level := min(mob.Level, d.LevelCap())
		for _, p := range players(d, level) {
			c := p.class
			me := character(d, items, c, p.split, level)
			f := fightOf(d, me, c, mob)
			kills := math.NaN() // at the cap there is no next level
			if exp, ok := d.ExpToNext(level); ok {
				kills = float64(exp) / float64(max(mob.Exp, 1))
			}
			pace := "at the cap"
			if !math.IsNaN(kills) {
				pace = fmt.Sprintf("kills/level %4.0f  attacking/level %5.0fs", kills, kills*f.secondsPerKill)
			}
			life := "     "
			if c.Attack.Kind == gamedata.Melee {
				life = fmt.Sprintf("%5.1f", f.killsPerLife)
			}
			r := rotationOf(d, me, c, planned(d, c, p.future, level), mob)
			// Logged for the user's playtest, not held to the bands: how fast skills on cooldown kill, in hits at the reference pace, and their share of the damage.
			withSkills := float64(mob.HP) / r.skills / refPace
			// What a player who never dodges takes against a touch every hit guard.
			touches := f.taken / (f.landRate * f.hitDamage / d.MobRules().HitGuard.Seconds())
			t.Logf("%-17s %-16s L%-2d %-8s hits %4.1f (%4.1f at the reference pace, %4.1f with skills, %3.0f%% of it by skills)  hit %3.0f%%  survives %3d  hit by %3.0f%%, %3.0f%% of touches' damage  kills/life %s  dps %5.1f, %5.1f with skills, chained %+3.0f%%  %s",
				m.ID, mob.ID, mob.Level, p.name, f.hitsToKill, f.secondsPerKill/refPace, withSkills, 100*r.skillShare, 100*f.hitRate, f.hitsSurvived, 100*f.landRate, 100*touches, life, r.basic, r.skills, 100*r.gain(), pace)
			if r.gain() > maxChainGain {
				t.Errorf("%s %s vs %s: a perfect chain adds %.0f%% to its skills on cooldown, want at most %.0f%%", m.ID, mob.ID, p.name, 100*r.gain(), 100*maxChainGain)
			}
			if !asserted {
				continue
			}
			check(t, m, rg, mob, c, f, kills)
			if mob.Elite && len(normals) > 0 {
				normal := normals[0]
				n := fightOf(d, character(d, items, c, p.split, min(normal.Level, d.LevelCap())), c, normal)
				checkElite(t, m, mob, normal, c, f, n)
			}
		}
	}
}

func check(t *testing.T, m *gamedata.Map, rg region, mob gamedata.Mob, c gamedata.Class, f fight, kills float64) {
	t.Helper()
	where := m.ID + " " + mob.ID + " vs " + string(c.ID)
	// Every class kills about as fast: the time a kill takes, misses and pace included, in hits at the reference pace.
	if paced := f.secondsPerKill / refPace; !mob.Elite && (paced < rg.hits[0] || paced > rg.hits[1]) {
		t.Errorf("%s: %.1f hits to kill at the reference pace (%.1f landed), want %.1f to %.1f in region %s", where, paced, f.hitsToKill, rg.hits[0], rg.hits[1], rg.name)
	}
	if !mob.Elite && !math.IsNaN(kills) && math.Abs(kills-killsPerLevel) > 5 {
		t.Errorf("%s: %.0f kills a level, want about %d", where, kills, killsPerLevel)
	}
	want, ok := fighters[c.ID]
	if !ok {
		t.Fatalf("%s: no targets for the class", where)
	}
	floor := want.hitFloor
	if rg.playerHits > 0 {
		floor = min(floor, rg.playerHits)
	}
	if f.hitRate < floor {
		t.Errorf("%s: %.0f%% of attacks land, want at least %.0f%%", where, 100*f.hitRate, 100*floor)
	}
	if f.landRate < want.hitBy[0] || f.landRate > want.hitBy[1] {
		t.Errorf("%s: %.0f%% of its attacks land, want %.0f%% to %.0f%%", where, 100*f.landRate, 100*want.hitBy[0], 100*want.hitBy[1])
	}
	// A melee class stands in reach, so it may outlast the band's top; a ranged one keeps its distance. Against magic, classes differ by design, the Magician most of all.
	top := rg.survives[1]
	if c.Attack.Kind == gamedata.Melee || mob.Damage == gamedata.Magic {
		top = 0
	}
	if f.hitsSurvived < rg.survives[0] || top > 0 && f.hitsSurvived > top {
		t.Errorf("%s: survives %d hits, want %d to %d in region %s", where, f.hitsSurvived, rg.survives[0], top, rg.name)
	}
	// A melee class takes hits for every kill, so what it lives through must last it a run of kills.
	if c.Attack.Kind == gamedata.Melee && !mob.Elite && f.killsPerLife < rg.life {
		t.Errorf("%s: %.1f kills a life, want at least %.1f in region %s", where, f.killsPerLife, rg.life, rg.name)
	}
}

// An elite takes 2 to 3 times its map's normal mob's hits and gives 2 to 3 times its EXP, and drops the gear they share about twice as often.
func checkElite(t *testing.T, m *gamedata.Map, elite, normal gamedata.Mob, c gamedata.Class, e, n fight) {
	t.Helper()
	where := m.ID + " " + elite.ID + " vs " + string(c.ID)
	if ratio := e.hitsToKill / n.hitsToKill; ratio < 2 || ratio > 3 {
		t.Errorf("%s: %.1f times the hits of %s, want 2 to 3", where, ratio, normal.ID)
	}
	if ratio := float64(elite.Exp) / float64(max(normal.Exp, 1)); ratio < 2 || ratio > 3 {
		t.Errorf("%s: %.1f times the EXP of %s, want about 2.5", where, ratio, normal.ID)
	}
	for _, ed := range elite.Drops {
		for _, nd := range normal.Drops {
			if ed.Item != nd.Item {
				continue
			}
			if ratio := ed.Chance / nd.Chance; ratio < 1.5 || ratio > 2.5 {
				t.Errorf("%s: drops %s at %.1f times %s's chance, want about 2", where, ed.Item, ratio, normal.ID)
			}
		}
	}
}

// A field boss against one on-level player of every class, at its map's top level: the bosses' targets, and its EXP against its map's top normal mob's.
var (
	bossMinutes = [2]float64{2.5, 3.5} // a kill alone at the class's pace, misses included; a Beginner's is only logged, as its splits' differ by half again
	bossExp     = [2]float64{35, 45}   // times its map's top normal mob's
)

const bossPotions = 30 // the most of its band's potion a melee class standing in reach all along drinks

// Each map's field boss against one on-level player of every class, held to the bosses' targets; the table is logged for tuning.
func TestFieldBosses(t *testing.T) {
	d, err := load.Dir(realData)
	if err != nil {
		t.Fatal(err)
	}
	items := realItemList(t, d)
	for _, m := range d.Maps() {
		if m.Boss == nil {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			t.Parallel()
			bossOf(t, d, items, m)
		})
	}
}

// bossOf is TestFieldBosses on one map.
func bossOf(t *testing.T, d *gamedata.Data, items []gamedata.Item, m *gamedata.Map) {
	boss, _ := d.Mob(m.Boss.Mob)
	top := topNormal(d, m)
	if ratio := float64(boss.Exp) / float64(top.Exp); ratio < bossExp[0] || ratio > bossExp[1] {
		t.Errorf("%s: %.0f times the EXP of %s, want %.0f to %.0f", boss.ID, ratio, top.ID, bossExp[0], bossExp[1])
	}
	potion := bandPotion(t, d, m)
	level := min(m.MaxLevel, d.LevelCap())
	for _, p := range players(d, level) {
		c := p.class
		me := character(d, items, c, p.split, level)
		f := fightOf(d, me, c, boss)
		minutes := f.secondsPerKill / 60
		potions := max(0, f.secondsPerKill*f.taken-float64(me.MaxHP)) / float64(potion.HealHP)
		r := rotationOf(d, me, c, planned(d, c, p.future, level), boss)
		t.Logf("%-17s %-16s L%-2d %-8s %4.1f min (%3.1f with skills)  hit %3.0f%%  hit by %3.0f%%, %4.1f a second, for %4.0f of %4d HP  %3.0f %s standing in reach",
			m.ID, boss.ID, boss.Level, p.name, minutes, float64(boss.HP)/r.skills/60, 100*f.hitRate, 100*f.landRate, f.hitsPerSecond, f.hitDamage, me.MaxHP, potions, potion.ID)
		if level >= promotionLevel && (minutes < bossMinutes[0] || minutes > bossMinutes[1]) {
			t.Errorf("%s vs %s: %.1f minutes alone, want %.1f to %.1f", boss.ID, p.name, minutes, bossMinutes[0], bossMinutes[1])
		}
		if c.Attack.Kind == gamedata.Melee && potions > bossPotions {
			t.Errorf("%s vs %s: %.0f %s standing in reach, want at most %d", boss.ID, p.name, potions, potion.ID, bossPotions)
		}
	}
}

// topNormal is the map's normal mob of the top level, the one a boss is measured against.
func topNormal(d *gamedata.Data, m *gamedata.Map) gamedata.Mob {
	normals, _ := mapMobs(d, m)
	top := normals[0]
	for _, n := range normals[1:] {
		if n.Level > top.Level {
			top = n
		}
	}
	return top
}

// bandPotion is the HP potion its map's normal mobs drop, the top one's first: its band's.
func bandPotion(t *testing.T, d *gamedata.Data, m *gamedata.Map) gamedata.Item {
	t.Helper()
	normals, _ := mapMobs(d, m)
	top := topNormal(d, m)
	for _, mob := range append([]gamedata.Mob{top}, normals...) {
		for _, dr := range mob.Drops {
			if it, _ := d.Item(dr.Item); it.HealHP > 0 {
				return it
			}
		}
	}
	t.Fatalf("%s: no normal mob drops an HP potion", m.ID)
	return gamedata.Item{}
}
