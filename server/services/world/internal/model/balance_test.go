package model_test

import (
	"math"
	"math/rand/v2"
	"os"
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
	hits       [2]float64 // a normal mob's landed hits to kill, either class
	survives   [2]int     // the bowman's touches lived through; 0 as the top is no top
	playerHits float64    // the least share of either class's attacks that land, when below the class's own floor
}

// Forest and cave are only logged: classes and skills retune them.
var regions = []region{
	{name: "A", from: 15, hits: [2]float64{2.5, 3.5}, survives: [2]int{10, 12}},
	{name: "B", from: 21, hits: [2]float64{3.5, 4.5}, survives: [2]int{8, 10}},
	{name: "C", from: 26, hits: [2]float64{4.5, 5.5}, survives: [2]int{6, 8}},
	{name: "D", from: 31, hits: [2]float64{6, 8}, survives: [2]int{4, 0}, playerHits: 0.70},
}

const (
	warriorHitFloor = 0.75 // the warrior's accuracy barely grows, so a mob's evasion must stay near it
	bowmanHitFloor  = 0.80
	killsPerLevel   = 30 // a normal mob's EXP is about a thirtieth of its level's
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

// fight is how an on-level character and a mob fare against each other.
type fight struct {
	hitsToKill      float64 // landed hits
	hitRate         float64 // the character's attacks that land
	touchesSurvived int     // the mob's landed touches the character lives through
	touchRate       float64 // the mob's touches that land
	secondsPerKill  float64 // attacking, misses included, at the character's attack pace
}

// character is a class at level, its points spent by its class's split and wearing, in every slot, the newest gear it can use at its middle roll.
func character(d *gamedata.Data, items []gamedata.Item, c gamedata.Class, level int) progression.Derived {
	own := c.BaseStats
	for range level - 1 {
		own = progression.AddStats(own, c.AutoAssign)
	}
	var worn []progression.WornItem
	for _, s := range d.EquipSlots() {
		var best *gamedata.Item
		for k := range items {
			it := &items[k]
			if it.Tab != gamedata.Equip || it.Slot != s.Kind || progression.Meets(*it, level, c.ID, own) != progression.NothingUnmet {
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
	return progression.DeriveStats(d.StatRules(), c.ID, level, own, progression.Active(worn, level, c.ID, own))
}

// fightOf resolves many attacks each way with the world's own combat, from a fixed seed.
func fightOf(d *gamedata.Data, me progression.Derived, c gamedata.Class, m gamedata.Mob) fight {
	r := rand.New(rand.NewPCG(1, 2))
	rules := d.StatRules()
	limits := model.HitChance{Min: rules.MinHit, Max: rules.MaxHit}
	var landed, dealt float64
	for range attacks {
		if h := model.Resolve(model.PlayerAttacker(me, c.Attack.Damage), model.MobDefender(m), limits, r); !h.Miss {
			landed++
			dealt += float64(h.Damage)
		}
	}
	var touches, taken float64
	for range attacks {
		if h := model.Resolve(model.MobAttacker(m), model.PlayerDefender(me), limits, r); !h.Miss {
			touches++
			taken += float64(h.Damage)
		}
	}
	f := fight{
		hitsToKill:      float64(m.HP) / (dealt / landed),
		hitRate:         landed / attacks,
		touchesSurvived: int(math.Ceil(float64(me.MaxHP)/(taken/touches))) - 1,
		touchRate:       touches / attacks,
	}
	pace := time.Duration(model.AttackCooldownTicks(c.Attack.Cooldown, me.AttackSpeed, tickInterval)) * tickInterval
	f.secondsPerKill = f.hitsToKill / f.hitRate * pace.Seconds()
	return f
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

// Every hunting map's mobs against an on-level warrior and bowman, held to their region's targets from A on; the table is logged for tuning.
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
		rg, asserted := regionOf(m)
		normals, elites := mapMobs(d, m)
		for _, mob := range append(normals, elites...) {
			for _, c := range d.Classes() {
				level := min(mob.Level, d.LevelCap())
				me := character(d, items, c, level)
				f := fightOf(d, me, c, mob)
				exp, _ := d.ExpToNext(level)
				kills := float64(exp) / float64(max(mob.Exp, 1))
				t.Logf("%-7s %-18s L%-2d %-7s hits %4.1f  hit %3.0f%%  survives %3d  touched %3.0f%%  kills/level %4.0f  attacking/level %5.0fs",
					m.ID, mob.ID, mob.Level, c.ID, f.hitsToKill, 100*f.hitRate, f.touchesSurvived, 100*f.touchRate, kills, kills*f.secondsPerKill)
				if !asserted {
					continue
				}
				check(t, m, rg, mob, c, f, kills)
				if mob.Elite && len(normals) > 0 {
					normal := normals[0]
					n := fightOf(d, character(d, items, c, min(normal.Level, d.LevelCap())), c, normal)
					checkElite(t, m, mob, normal, c, f, n)
				}
			}
		}
	}
}

func check(t *testing.T, m *gamedata.Map, rg region, mob gamedata.Mob, c gamedata.Class, f fight, kills float64) {
	t.Helper()
	where := m.ID + " " + mob.ID + " vs " + string(c.ID)
	if !mob.Elite && (f.hitsToKill < rg.hits[0] || f.hitsToKill > rg.hits[1]) {
		t.Errorf("%s: %.1f hits to kill, want %.1f to %.1f in region %s", where, f.hitsToKill, rg.hits[0], rg.hits[1], rg.name)
	}
	if !mob.Elite && math.Abs(kills-killsPerLevel) > 5 {
		t.Errorf("%s: %.0f kills a level, want about %d", where, kills, killsPerLevel)
	}
	floor := warriorHitFloor
	if c.ID == kernel.Bowman {
		floor = bowmanHitFloor
	}
	if rg.playerHits > 0 {
		floor = min(floor, rg.playerHits)
	}
	if f.hitRate < floor {
		t.Errorf("%s: %.0f%% of attacks land, want at least %.0f%%", where, 100*f.hitRate, 100*floor)
	}
	switch c.ID {
	case kernel.Bowman:
		if f.touchesSurvived < rg.survives[0] || rg.survives[1] > 0 && f.touchesSurvived > rg.survives[1] {
			t.Errorf("%s: survives %d touches, want %d to %d in region %s", where, f.touchesSurvived, rg.survives[0], rg.survives[1], rg.name)
		}
		if f.touchRate < 0.65 || f.touchRate > 0.85 {
			t.Errorf("%s: %.0f%% of touches land, want 65%% to 85%%", where, 100*f.touchRate)
		}
	case kernel.Warrior:
		if f.touchesSurvived < rg.survives[0] {
			t.Errorf("%s: survives %d touches, want at least the bowman's %d", where, f.touchesSurvived, rg.survives[0])
		}
		if f.touchRate < 0.85 {
			t.Errorf("%s: %.0f%% of touches land, want at least 85%%", where, 100*f.touchRate)
		}
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
